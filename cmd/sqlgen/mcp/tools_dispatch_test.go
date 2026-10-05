package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectWithTools stands up a Server with the fixture store and registered tool
// surface on one end of an in-memory transport, returning an initialized client
// session. This exercises the real SDK dispatch path (schema inference,
// tools/list, tools/call, error envelope) rather than the pure tool functions.
func connectWithTools(ctx context.Context, t *testing.T) *mcp.ClientSession {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()

	srv := New("test-version", nil)
	srv.store = newFixtureStore()
	srv.watchEnabled = true
	srv.registerTools()

	go func() { _ = srv.Serve(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestDispatchListsAllElevenTools(t *testing.T) {
	cs := connectWithTools(t.Context(), t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := make(map[string]bool)
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	want := []string{
		"sqlgen_list_entities", "sqlgen_get_entity", "sqlgen_find_method",
		"sqlgen_find_referencing", "sqlgen_describe_relationship", "sqlgen_find_join_path",
		"sqlgen_show_sql", "sqlgen_get_conventions", "sqlgen_get_example",
		"sqlgen_health", "sqlgen_validate_manifest",
	}
	if len(res.Tools) != len(want) {
		t.Errorf("registered %d tools, want %d", len(res.Tools), len(want))
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestDispatchCallToolReturnsStructuredContent(t *testing.T) {
	cs := connectWithTools(t.Context(), t)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "sqlgen_get_entity",
		Arguments: map[string]any{"name": "User"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Name != "User" {
		t.Errorf("entity name = %q, want User", out.Name)
	}
}

// TestDispatchNotFoundIsToolError confirms a business error surfaces as an
// IsError tool result carrying the {code, message, suggestions} envelope — not
// a protocol-level error (which the SDK client would collapse, dropping the
// code + suggestions). The connection stays usable afterward (MCP.md §5.3).
func TestDispatchNotFoundIsToolError(t *testing.T) {
	cs := connectWithTools(t.Context(), t)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "sqlgen_get_entity",
		Arguments: map[string]any{"name": "Usr"},
	})
	if err != nil {
		t.Fatalf("CallTool returned a transport error (should be an IsError result): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true on an unknown entity")
	}
	var env struct {
		Code        int      `json:"code"`
		Message     string   `json:"message"`
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Code != int(CodeEntityNotFound) {
		t.Errorf("code = %d, want %d", env.Code, CodeEntityNotFound)
	}
	if len(env.Suggestions) != 1 || env.Suggestions[0] != "User" {
		t.Errorf("suggestions = %v, want [User]", env.Suggestions)
	}

	// The session must remain usable after a tool error.
	if _, err := cs.ListTools(t.Context(), nil); err != nil {
		t.Errorf("session unusable after a tool error: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
