// Package integration boots the compiled `sqlgen mcp serve` binary as a
// subprocess and drives the full MCP lifecycle against it over both
// transports (MCP.md §7.2). It parallels the gqlgen-subprocess integration
// tests' pattern: the binary is built once in TestMain and shared by all tests.
package integration

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sqlgenBin is the compiled CLI binary path, built once in TestMain.
var sqlgenBin string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	tmp, err := os.MkdirTemp("", "sqlgen-mcp-integration")
	if err != nil {
		panic(err)
	}
	sqlgenBin = filepath.Join(tmp, "sqlgen")

	// Build the CLI module (the directory two levels up). The repo workspace
	// must be active for the sibling runtime/parser modules to resolve, so
	// this intentionally inherits the environment (do not set GOWORK=off).
	build := exec.Command("go", "build", "-o", sqlgenBin, ".") //nolint:gosec // test binary path under MkdirTemp
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("building sqlgen binary: %v\n%s", err, out))
	}

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// tempManifest copies the package fixture manifest into a fresh directory so
// watch-mode tests can mutate it without touching testdata.
func tempManifest(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../testdata/manifest_gen.json")
	if err != nil {
		t.Fatalf("reading fixture manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest_gen.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil { //nolint:gosec // path is a t.TempDir file
		t.Fatalf("writing temp manifest: %v", err)
	}
	return path
}

// renameEntity returns the manifest bytes with the ActiveUser view renamed —
// a schema-valid mutation whose effect is visible through list_entities.
func renameEntity(t *testing.T, path, from, to string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading manifest for mutation: %v", err)
	}
	old := fmt.Sprintf("%q: %q", "name", from)
	mutated := strings.Replace(string(raw), old, fmt.Sprintf("%q: %q", "name", to), 1)
	if mutated == string(raw) {
		t.Fatalf("mutation %q -> %q did not change the manifest", from, to)
	}
	return []byte(mutated)
}

// atomicReplace mimics a `sqlgen generate` regen: write a temp file next to
// the target, then rename over it.
func atomicReplace(t *testing.T, path string, content []byte) {
	t.Helper()
	tmp := path + ".regen"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		t.Fatalf("writing regen temp file: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("renaming regen file over manifest: %v", err)
	}
}

// toolNames projects a tools/list result to its sorted tool names.
func toolNames(res *mcp.ListToolsResult) []string {
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// wantTools is the full §4.1 surface: the 11 read-only tools.
var wantTools = []string{
	"sqlgen_describe_relationship",
	"sqlgen_find_join_path",
	"sqlgen_find_method",
	"sqlgen_find_referencing",
	"sqlgen_get_conventions",
	"sqlgen_get_entity",
	"sqlgen_get_example",
	"sqlgen_health",
	"sqlgen_list_entities",
	"sqlgen_show_sql",
	"sqlgen_validate_manifest",
}

// callTool invokes a tool and fails the test on a transport error or an
// IsError result. Returns the structured output as a generic tree.
func callTool(ctx context.Context, t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) transport error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s, %v) returned IsError result: %s", name, args, toolResultText(res))
	}
	return structuredTree(t, name, res)
}

// callToolExpectError invokes a tool expecting an IsError result and returns
// the decoded {code, message, suggestions} envelope from the structured
// content (the text content carries the human-readable form).
func callToolExpectError(ctx context.Context, t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) transport error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(%s, %v) succeeded, want IsError result", name, args)
	}
	if text := toolResultText(res); text == "" {
		t.Fatalf("CallTool(%s) IsError result has no text content", name)
	}
	return structuredTree(t, name, res)
}

// structuredTree decodes a successful tool result into a generic map.
func structuredTree(t *testing.T, name string, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("CallTool(%s) marshaling structured content: %v", name, err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("CallTool(%s) structured content is not an object: %v\n%s", name, err, raw)
	}
	return tree
}

// toolResultText extracts the first text content of a tool result.
func toolResultText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// driveLifecycle exercises the full §7.2 lifecycle on an established session:
// tools/list → tools/call (each of the 11) → resources/list → resources/read
// (each of the 4 URIs) → prompts/list → prompts/get (each of the 3).
func driveLifecycle(ctx context.Context, t *testing.T, session *mcp.ClientSession) {
	t.Helper()

	// tools/list — exactly the 11 §4.1 tools.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := toolNames(tools)
	if len(got) != len(wantTools) {
		t.Fatalf("ListTools = %v, want the 11 tools %v", got, wantTools)
	}
	for i, name := range wantTools {
		if got[i] != name {
			t.Fatalf("ListTools[%d] = %q, want %q (full list %v)", i, got[i], name, got)
		}
	}

	// tools/call — each of the 11 against the fixture manifest (User table +
	// ActiveUser view, User.Posts o2m, Get postgres SQL body).
	listOut := callTool(ctx, t, session, "sqlgen_list_entities", map[string]any{})
	entities, _ := listOut["entities"].([]any)
	if len(entities) != 2 {
		t.Errorf("list_entities returned %d entities, want 2 (fixture User + ActiveUser)", len(entities))
	}

	entityOut := callTool(ctx, t, session, "sqlgen_get_entity", map[string]any{"name": "User"})
	if entityOut["table"] != "users" {
		t.Errorf("get_entity(User).table = %v, want users", entityOut["table"])
	}

	findOut := callTool(ctx, t, session, "sqlgen_find_method", map[string]any{"query": "Get"})
	if matches, _ := findOut["matches"].([]any); len(matches) == 0 {
		t.Errorf("find_method(Get) returned no matches, want at least one")
	}

	refOut := callTool(ctx, t, session, "sqlgen_find_referencing", map[string]any{"table": "users"})
	if _, ok := refOut["references"]; !ok {
		t.Errorf("find_referencing(users) output lacks references field: %v", refOut)
	}

	relOut := callTool(ctx, t, session, "sqlgen_describe_relationship", map[string]any{"entity": "User", "relationship": "Posts"})
	if relOut["kind"] != "o2m" || relOut["target_entity"] != "Post" {
		t.Errorf("describe_relationship(User, Posts) = kind %v target %v, want o2m Post", relOut["kind"], relOut["target_entity"])
	}

	pathOut := callTool(ctx, t, session, "sqlgen_find_join_path", map[string]any{"from": "User", "to": "ActiveUser"})
	if _, ok := pathOut["paths"]; !ok {
		t.Errorf("find_join_path output lacks paths field: %v", pathOut)
	}

	sqlOut := callTool(ctx, t, session, "sqlgen_show_sql", map[string]any{"entity": "User", "method": "Get"})
	sqlBody, _ := sqlOut["sql"].(string)
	if sqlOut["dialect"] != "postgres" || !strings.Contains(sqlBody, "$1") {
		t.Errorf("show_sql(User, Get) = dialect %v sql %q, want postgres SQL with $1", sqlOut["dialect"], sqlBody)
	}

	callTool(ctx, t, session, "sqlgen_get_conventions", map[string]any{})
	exampleOut := callTool(ctx, t, session, "sqlgen_get_example", map[string]any{"entity": "User"})
	if exampleOut["entity"] != "User" {
		t.Errorf("get_example(User).entity = %v, want User", exampleOut["entity"])
	}

	healthOut := callTool(ctx, t, session, "sqlgen_health", map[string]any{})
	if healthOut["ok"] != true {
		t.Errorf("health.ok = %v, want true", healthOut["ok"])
	}

	validateOut := callTool(ctx, t, session, "sqlgen_validate_manifest", map[string]any{})
	if validateOut["valid"] != true {
		t.Errorf("validate_manifest.valid = %v, want true", validateOut["valid"])
	}

	// The §5.3 typo contract on the wire: IsError result carrying the coded
	// envelope with suggestions, session stays usable afterwards.
	envelope := callToolExpectError(ctx, t, session, "sqlgen_get_entity", map[string]any{"name": "Usre"})
	if code, _ := envelope["code"].(float64); code != -32003 {
		t.Errorf("get_entity(Usre) envelope code = %v, want -32003", envelope["code"])
	}
	if _, ok := envelope["suggestions"]; !ok {
		t.Errorf("get_entity(Usre) envelope lacks suggestions: %v", envelope)
	}

	// resources/list — the 3 fixed URIs; the entity template is listed
	// separately under resources/templates/list.
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources.Resources) != 3 {
		t.Errorf("ListResources returned %d resources, want 3", len(resources.Resources))
	}
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	if len(templates.ResourceTemplates) != 1 {
		t.Errorf("ListResourceTemplates returned %d templates, want 1 (sqlgen://entity/{name})", len(templates.ResourceTemplates))
	}

	// resources/read — each of the 4 URIs.
	for _, uri := range []string{"sqlgen://manifest", "sqlgen://conventions", "sqlgen://config", "sqlgen://entity/User"} {
		res, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("ReadResource(%s): %v", uri, err)
		}
		if len(res.Contents) != 1 || res.Contents[0].MIMEType != "application/json" {
			t.Fatalf("ReadResource(%s) = %d contents (mime %q), want 1 application/json", uri, len(res.Contents), res.Contents[0].MIMEType)
		}
		if !json.Valid([]byte(res.Contents[0].Text)) {
			t.Errorf("ReadResource(%s) body is not valid JSON", uri)
		}
	}

	// prompts/list + prompts/get — the 3 §4.3 templates.
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts.Prompts) != 3 {
		t.Errorf("ListPrompts returned %d prompts, want 3", len(prompts.Prompts))
	}
	promptArgs := map[string]map[string]string{
		"sqlgen-write-query":            {"entity": "User", "goal": "find active admins"},
		"sqlgen-add-relationship-usage": {"entity": "User", "relationship": "Posts"},
		"sqlgen-debug-not-found":        {"entity": "User", "method": "Get"},
	}
	for name, args := range promptArgs {
		res, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("GetPrompt(%s): %v", name, err)
		}
		if len(res.Messages) == 0 {
			t.Errorf("GetPrompt(%s) returned no messages", name)
		}
	}
}

// awaitCondition polls fn until it returns true or the deadline passes.
func awaitCondition(t *testing.T, timeout time.Duration, fn func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, desc)
}

// entityNames extracts the names from a list_entities structured output.
func entityNames(out map[string]any) []string {
	entities, _ := out["entities"].([]any)
	names := make([]string, 0, len(entities))
	for _, e := range entities {
		if m, ok := e.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return names
}
