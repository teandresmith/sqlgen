package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// E2E MCP surface (MCP.md §7.4). Boots the compiled
// `sqlgen mcp serve` binary against this example's generated manifest and
// drives the full surface over raw newline-delimited JSON-RPC 2.0: the 11
// tools, the 4 resources, the 3 prompts, dialect-aware show_sql, the §5.3
// IsError typo envelope, and the §5.3 resource protocol error.
//
// The client is deliberately raw (no MCP SDK): this example module depends
// only on the root runtime module, and the hand-rolled wire client doubles as
// an SDK-independent check of the JSON-RPC framing — including the
// protocol-level -32003 + data.suggestions shape on resource reads, which the
// go-sdk client library collapses.
const (
	mcpWantDialect     = "postgres"
	mcpWantPlaceholder = "$1"
)

// buildSqlgenBinary compiles the CLI module once per test binary. The repo
// workspace must be active for the CLI's sibling module deps to resolve, so
// GOWORK is pointed at the repo's go.work explicitly — make test-examples
// runs this process with GOWORK=off (needed for the example's own replace
// directive), which would otherwise break the CLI build.
var buildSqlgenBinary = sync.OnceValues(func() (string, error) {
	goWork, err := filepath.Abs("../../../../../../go.work")
	if err != nil {
		return "", fmt.Errorf("resolving go.work path: %w", err)
	}
	dir, err := os.MkdirTemp("", "sqlgen-mcp-e2e")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "sqlgen")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "../../../.." // the cmd/sqlgen module root
	build.Env = append(os.Environ(), "GOWORK="+goWork)
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building sqlgen: %w\n%s", err, out)
	}
	return bin, nil
})

// mcpClient is a minimal newline-delimited JSON-RPC 2.0 client over the
// server subprocess's stdio.
type mcpClient struct {
	t      *testing.T
	stdin  io.WriteCloser
	reader *bufio.Reader
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	nextID int
}

// startMCPServer spawns `sqlgen mcp serve` against this example's manifest
// and completes the initialize handshake.
func startMCPServer(t *testing.T) *mcpClient {
	t.Helper()
	bin, err := buildSqlgenBinary()
	if err != nil {
		t.Fatalf("building sqlgen binary: %v", err)
	}

	cmd := exec.Command(bin, "mcp", "serve", "--manifest", manifestJSONPath, "--no-watch") //nolint:gosec // test-built binary
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sqlgen mcp serve: %v", err)
	}

	c := &mcpClient{t: t, stdin: stdin, reader: bufio.NewReader(stdout), cmd: cmd, stderr: &stderr}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})

	initRes, rpcErr := c.request("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "sqlgen-e2e-raw", "version": "0.0.0"},
	})
	if rpcErr != nil {
		t.Fatalf("initialize: %v", rpcErr)
	}
	caps := asMap(t, initRes["capabilities"], "initialize capabilities")
	for _, want := range []string{"tools", "resources", "prompts"} {
		if _, ok := caps[want]; !ok {
			t.Fatalf("initialize capabilities lack %q: %v", want, caps)
		}
	}
	c.notify("notifications/initialized", map[string]any{})
	return c
}

// rpcError is a protocol-level JSON-RPC error response.
type rpcError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message) }

// send writes one JSON-RPC message.
func (c *mcpClient) send(msg map[string]any) {
	c.t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("marshaling request: %v", err)
	}
	if _, err := c.stdin.Write(append(raw, '\n')); err != nil {
		c.t.Fatalf("writing request: %v\nserver stderr:\n%s", err, c.stderr.String())
	}
}

// notify sends a JSON-RPC notification (no id, no response).
func (c *mcpClient) notify(method string, params map[string]any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// request sends a JSON-RPC request and reads messages until the matching
// response arrives, skipping any interleaved notifications.
func (c *mcpClient) request(method string, params map[string]any) (map[string]any, *rpcError) {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.t.Fatalf("reading response to %s: %v\nserver stderr:\n%s", method, err, c.stderr.String())
		}
		var resp struct {
			ID     *int            `json:"id"`
			Result map[string]any  `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			c.t.Fatalf("decoding response to %s: %v\n%s", method, err, line)
		}
		if resp.ID == nil || *resp.ID != id {
			continue // a notification or an unrelated message
		}
		if len(resp.Error) > 0 {
			var rpcErr rpcError
			if err := json.Unmarshal(resp.Error, &rpcErr); err != nil {
				c.t.Fatalf("decoding error response to %s: %v\n%s", method, err, line)
			}
			return nil, &rpcErr
		}
		return resp.Result, nil
	}
}

// callTool invokes a tool expecting success and returns its structured output.
func (c *mcpClient) callTool(name string, args map[string]any) map[string]any {
	c.t.Helper()
	res, rpcErr := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	if rpcErr != nil {
		c.t.Fatalf("tools/call %s: %v", name, rpcErr)
	}
	if res["isError"] == true {
		c.t.Fatalf("tools/call %s(%v) returned isError result: %v", name, args, res)
	}
	return asMap(c.t, res["structuredContent"], name+" structuredContent")
}

// callToolExpectError invokes a tool expecting an IsError result and returns
// its structured {code, message, suggestions} envelope.
func (c *mcpClient) callToolExpectError(name string, args map[string]any) map[string]any {
	c.t.Helper()
	res, rpcErr := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	if rpcErr != nil {
		c.t.Fatalf("tools/call %s: %v", name, rpcErr)
	}
	if res["isError"] != true {
		c.t.Fatalf("tools/call %s(%v) succeeded, want isError result: %v", name, args, res)
	}
	return asMap(c.t, res["structuredContent"], name+" error envelope")
}

func asMap(t *testing.T, v any, desc string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object: %v", desc, v, v)
	}
	return m
}

func asSlice(t *testing.T, v any, desc string) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array: %v", desc, v, v)
	}
	return s
}

// TestMCP_FullSurface drives the 11 tools, 4 resources, and 3 prompts against
// this example's manifest, with dialect-aware show_sql assertions.
func TestMCP_FullSurface(t *testing.T) {
	c := startMCPServer(t)

	// tools/list — exactly the 11 sqlgen_* tools.
	toolsRes, rpcErr := c.request("tools/list", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("tools/list: %v", rpcErr)
	}
	tools := asSlice(t, toolsRes["tools"], "tools/list tools")
	if len(tools) != 11 {
		t.Fatalf("tools/list returned %d tools, want 11", len(tools))
	}
	for _, tool := range tools {
		name, _ := asMap(t, tool, "tool entry")["name"].(string)
		if !strings.HasPrefix(name, "sqlgen_") {
			t.Errorf("tool %q lacks the sqlgen_ prefix", name)
		}
	}

	// list_entities — pick assertion targets dynamically from this example's
	// own schema so the leg tracks schema evolution.
	listOut := c.callTool("sqlgen_list_entities", map[string]any{})
	entities := asSlice(t, listOut["entities"], "list_entities entities")
	if len(entities) == 0 {
		t.Fatal("list_entities returned no entities")
	}

	// Find an entity with at least one relationship for the graph tools.
	var relEntity, relName, relTarget, relEntityTable string
	for _, e := range entities {
		name, _ := asMap(t, e, "entity summary")["name"].(string)
		full := c.callTool("sqlgen_get_entity", map[string]any{"name": name})
		rels, _ := full["relationships"].([]any)
		if len(rels) == 0 {
			continue
		}
		rel := asMap(t, rels[0], "relationship")
		relEntity = name
		relName, _ = rel["name"].(string)
		relTarget, _ = rel["target_entity"].(string)
		relEntityTable, _ = full["table"].(string)
		break
	}
	if relEntity == "" {
		t.Fatal("no entity with relationships found — example schema should have FK edges")
	}

	relOut := c.callTool("sqlgen_describe_relationship", map[string]any{"entity": relEntity, "relationship": relName})
	if relOut["target_entity"] != relTarget || relOut["kind"] == "" {
		t.Errorf("describe_relationship(%s, %s) = %v, want kind set and target %q", relEntity, relName, relOut, relTarget)
	}

	pathOut := c.callTool("sqlgen_find_join_path", map[string]any{"from": relEntity, "to": relTarget})
	if paths := asSlice(t, pathOut["paths"], "find_join_path paths"); len(paths) == 0 {
		t.Errorf("find_join_path(%s, %s) found no path, want at least the direct edge", relEntity, relTarget)
	}

	refOut := c.callTool("sqlgen_find_referencing", map[string]any{"table": relEntityTable})
	if _, ok := refOut["references"]; !ok {
		t.Errorf("find_referencing(%s) output lacks references field: %v", relEntityTable, refOut)
	}

	// find_method + dialect-aware show_sql. "Get" is the by-PK read on every
	// generated client (PRD §9.1) — the manifest names the real method, so the
	// name an agent finds here is the name that compiles.
	findOut := c.callTool("sqlgen_find_method", map[string]any{"query": "Get"})
	matches := asSlice(t, findOut["matches"], "find_method matches")
	if len(matches) == 0 {
		t.Fatal("find_method(Get) returned no matches")
	}
	sqlEntity, _ := asMap(t, matches[0], "method match")["entity"].(string)
	sqlOut := c.callTool("sqlgen_show_sql", map[string]any{"entity": sqlEntity, "method": "Get"})
	sqlBody, _ := sqlOut["sql"].(string)
	if sqlOut["dialect"] != mcpWantDialect {
		t.Errorf("show_sql(%s, Get).dialect = %v, want %q", sqlEntity, sqlOut["dialect"], mcpWantDialect)
	}
	if !strings.Contains(sqlBody, mcpWantPlaceholder) {
		t.Errorf("show_sql(%s, Get).sql = %q, want the %s placeholder %q", sqlEntity, sqlBody, mcpWantDialect, mcpWantPlaceholder)
	}

	// Meta + diagnostics.
	c.callTool("sqlgen_get_conventions", map[string]any{})
	exampleOut := c.callTool("sqlgen_get_example", map[string]any{"entity": relEntity})
	if exampleOut["entity"] != relEntity {
		t.Errorf("get_example(%s).entity = %v", relEntity, exampleOut["entity"])
	}
	healthOut := c.callTool("sqlgen_health", map[string]any{})
	if healthOut["ok"] != true || healthOut["schema_version"] == "" {
		t.Errorf("health = %v, want ok true + schema_version set", healthOut)
	}
	validateOut := c.callTool("sqlgen_validate_manifest", map[string]any{})
	if validateOut["valid"] != true {
		t.Errorf("validate_manifest = %v, want valid true", validateOut)
	}

	// §5.3 typo contract (tool side): a one-character typo yields an IsError
	// result whose envelope carries -32003 and a suggestion for the real name.
	typo := relEntity[:len(relEntity)-1] + "X"
	envelope := c.callToolExpectError("sqlgen_get_entity", map[string]any{"name": typo})
	if code, _ := envelope["code"].(float64); code != -32003 {
		t.Errorf("get_entity(%s) envelope code = %v, want -32003", typo, envelope["code"])
	}
	suggestions := asSlice(t, envelope["suggestions"], "typo suggestions")
	if !containsString(suggestions, relEntity) {
		t.Errorf("get_entity(%s) suggestions = %v, want %q suggested", typo, suggestions, relEntity)
	}

	// resources/list (3 fixed) + templates/list (the entity template).
	resList, rpcErr := c.request("resources/list", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("resources/list: %v", rpcErr)
	}
	if got := len(asSlice(t, resList["resources"], "resources")); got != 3 {
		t.Errorf("resources/list returned %d, want 3", got)
	}
	tmplList, rpcErr := c.request("resources/templates/list", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("resources/templates/list: %v", rpcErr)
	}
	if got := len(asSlice(t, tmplList["resourceTemplates"], "resourceTemplates")); got != 1 {
		t.Errorf("resources/templates/list returned %d, want 1", got)
	}

	// resources/read — all four URIs return application/json bodies.
	for _, uri := range []string{"sqlgen://manifest", "sqlgen://conventions", "sqlgen://config", "sqlgen://entity/" + relEntity} {
		res, rpcErr := c.request("resources/read", map[string]any{"uri": uri})
		if rpcErr != nil {
			t.Fatalf("resources/read %s: %v", uri, rpcErr)
		}
		contents := asSlice(t, res["contents"], uri+" contents")
		if len(contents) != 1 {
			t.Fatalf("resources/read %s returned %d contents, want 1", uri, len(contents))
		}
		body := asMap(t, contents[0], uri+" content")
		if body["mimeType"] != "application/json" {
			t.Errorf("resources/read %s mimeType = %v, want application/json", uri, body["mimeType"])
		}
		text, _ := body["text"].(string)
		if !json.Valid([]byte(text)) {
			t.Errorf("resources/read %s body is not valid JSON", uri)
		}
		if uri == "sqlgen://manifest" && !strings.Contains(text, `"schema_version"`) {
			t.Errorf("sqlgen://manifest body lacks schema_version")
		}
	}

	// §5.3 resource contract (protocol side): unknown entity resource is a
	// genuine JSON-RPC error carrying -32003 + data.suggestions on the wire —
	// the raw-client check the SDK client collapses.
	_, resErr := c.request("resources/read", map[string]any{"uri": "sqlgen://entity/" + typo})
	if resErr == nil {
		t.Fatalf("resources/read sqlgen://entity/%s succeeded, want -32003 protocol error", typo)
	}
	if resErr.Code != -32003 {
		t.Errorf("resources/read unknown entity error code = %d, want -32003", resErr.Code)
	}
	if resErr.Data == nil {
		t.Errorf("resources/read unknown entity error carries no data: %v", resErr)
	} else if wireSuggestions, ok := resErr.Data["suggestions"].([]any); !ok || !containsString(wireSuggestions, relEntity) {
		t.Errorf("resources/read unknown entity data.suggestions = %v, want %q suggested", resErr.Data["suggestions"], relEntity)
	}

	// prompts/list + prompts/get — the 3 templates expand.
	promptsRes, rpcErr := c.request("prompts/list", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("prompts/list: %v", rpcErr)
	}
	if got := len(asSlice(t, promptsRes["prompts"], "prompts")); got != 3 {
		t.Errorf("prompts/list returned %d, want 3", got)
	}
	promptArgs := map[string]map[string]string{
		"sqlgen-write-query":            {"entity": relEntity, "goal": "list recent records"},
		"sqlgen-add-relationship-usage": {"entity": relEntity, "relationship": relName},
		"sqlgen-debug-not-found":        {"entity": relEntity},
	}
	for name, args := range promptArgs {
		res, rpcErr := c.request("prompts/get", map[string]any{"name": name, "arguments": args})
		if rpcErr != nil {
			t.Fatalf("prompts/get %s: %v", name, rpcErr)
		}
		if got := len(asSlice(t, res["messages"], name+" messages")); got == 0 {
			t.Errorf("prompts/get %s returned no messages", name)
		}
	}

	// Shutdown: closing stdin ends the stdio session; the server must exit
	// cleanly (exit code 0) rather than hang or crash.
	_ = c.stdin.Close()
	if err := c.cmd.Wait(); err != nil {
		t.Errorf("server did not exit cleanly after stdin close: %v\nstderr:\n%s", err, c.stderr.String())
	}
}

func containsString(items []any, want string) bool {
	for _, it := range items {
		if s, ok := it.(string); ok && s == want {
			return true
		}
	}
	return false
}
