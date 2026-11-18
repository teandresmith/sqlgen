package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// prdToolNames is the §31.3 tool table, spelled as the PRD spells it. The
// tests compare against these literals, not the production constants, so a
// renamed tool fails here.
var prdToolNames = []string{
	"sqlgen_list_entities", "sqlgen_get_entity", "sqlgen_find_method",
	"sqlgen_find_referencing", "sqlgen_describe_relationship", "sqlgen_get_conventions",
	"sqlgen_get_example", "sqlgen_find_join_path", "sqlgen_show_sql",
	"sqlgen_health", "sqlgen_validate_manifest",
}

// connectSDKClient stands up a Server over store with the full surface registered and
// returns an initialized in-memory client session plus the server.
func connectSDKClient(t *testing.T, store *Store, opts *mcp.ClientOptions) (*mcp.ClientSession, *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	serverT, clientT := mcp.NewInMemoryTransports()

	srv := New("fs-version", nil)
	srv.store = store
	srv.watchEnabled = true
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, serverT)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "fs-client", Version: "0"}, opts)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		<-done
	})
	return cs, srv
}

// callTool invokes a tool over the session; a transport error fails the test.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s, %v) transport error: %v", name, args, err)
	}
	return res
}

// toolText returns the concatenated text content of a tool result: the bytes the
// server wrote, not a client-side re-encoding.
func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content = %T, want *mcp.TextContent", c)
		}
		b.WriteString(tc.Text)
	}
	return b.String()
}

// toolTree decodes a tool result's structured content into a generic JSON tree.
func toolTree(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("structured content is not an object: %v\n%s", err, raw)
	}
	return tree
}

// jsonTree marshals v and decodes it back into a generic JSON tree.
func jsonTree(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return tree
}

// storeOf installs entities into a disk-less store.
func storeOf(entities ...*manifest.Entity) *Store {
	s := NewStore("/fixture/fs/manifest_gen.json", nil)
	s.install(&manifest.Document{Layout: manifest.LayoutSingle, Dialect: manifest.DialectPostgres}, nil, entities)
	s.ok = true
	return s
}

// chainStore builds N0 -> N1 -> ... -> N<n-1>, each linked by a "next" edge.
func chainStore(n int) *Store {
	names := make([]string, n)
	for i := range n {
		names[i] = "N" + string(rune('0'+i))
	}
	entities := make([]*manifest.Entity, n)
	for i, name := range names {
		e := &manifest.Entity{Kind: manifest.EntityKindTable, Name: name, Table: strings.ToLower(name), PK: manifest.PK{Kind: "single"}}
		if i+1 < n {
			e.Relationships = []manifest.Relationship{{Name: "next", Kind: manifest.RelationshipO2M, TargetEntity: names[i+1]}}
		}
		entities[i] = e
	}
	return storeOf(entities...)
}

// TestDispatchListsPRDSurface lists every §31.3 element through the real SDK dispatch
// path and compares against the PRD's literal names: 11 tools under
// tools/list, the three fixed URIs under resources/list, the entity template
// under resources/templates/list, and the three prompts under prompts/list.
func TestDispatchListsPRDSurface(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	ctx := t.Context()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	resources, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	templates, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	prompts, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}

	toolNames := make([]string, 0, len(tools.Tools))
	resourceURIs := make([]string, 0, len(resources.Resources))
	templateURIs := make([]string, 0, len(templates.ResourceTemplates))
	promptNames := make([]string, 0, len(prompts.Prompts))
	for _, x := range tools.Tools {
		toolNames = append(toolNames, x.Name)
	}
	for _, x := range resources.Resources {
		resourceURIs = append(resourceURIs, x.URI)
	}
	for _, x := range templates.ResourceTemplates {
		templateURIs = append(templateURIs, x.URITemplate)
	}
	for _, x := range prompts.Prompts {
		promptNames = append(promptNames, x.Name)
	}
	lists := map[string][]string{
		"tools/list":               toolNames,
		"resources/list":           resourceURIs,
		"resources/templates/list": templateURIs,
		"prompts/list":             promptNames,
	}

	tests := []struct {
		name    string
		list    string
		literal string
	}{
		{name: "tool sqlgen_list_entities", list: "tools/list", literal: "sqlgen_list_entities"},
		{name: "tool sqlgen_get_entity", list: "tools/list", literal: "sqlgen_get_entity"},
		{name: "tool sqlgen_find_method", list: "tools/list", literal: "sqlgen_find_method"},
		{name: "tool sqlgen_find_referencing", list: "tools/list", literal: "sqlgen_find_referencing"},
		{name: "tool sqlgen_describe_relationship", list: "tools/list", literal: "sqlgen_describe_relationship"},
		{name: "tool sqlgen_get_conventions", list: "tools/list", literal: "sqlgen_get_conventions"},
		{name: "tool sqlgen_get_example", list: "tools/list", literal: "sqlgen_get_example"},
		{name: "tool sqlgen_find_join_path", list: "tools/list", literal: "sqlgen_find_join_path"},
		{name: "tool sqlgen_show_sql", list: "tools/list", literal: "sqlgen_show_sql"},
		{name: "tool sqlgen_health", list: "tools/list", literal: "sqlgen_health"},
		{name: "tool sqlgen_validate_manifest", list: "tools/list", literal: "sqlgen_validate_manifest"},
		{name: "resource manifest", list: "resources/list", literal: "sqlgen://manifest"},
		{name: "resource conventions", list: "resources/list", literal: "sqlgen://conventions"},
		{name: "resource config", list: "resources/list", literal: "sqlgen://config"},
		{name: "template entity", list: "resources/templates/list", literal: "sqlgen://entity/{name}"},
		{name: "prompt sqlgen-write-query", list: "prompts/list", literal: "sqlgen-write-query"},
		{name: "prompt sqlgen-add-relationship-usage", list: "prompts/list", literal: "sqlgen-add-relationship-usage"},
		{name: "prompt sqlgen-debug-not-found", list: "prompts/list", literal: "sqlgen-debug-not-found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Contains(lists[tt.list], tt.literal) {
				t.Errorf("%s = %v, want it to list %q", tt.list, lists[tt.list], tt.literal)
			}
		})
	}

	t.Run("element counts match the PRD", func(t *testing.T) {
		if len(toolNames) != 11 || len(resourceURIs) != 3 || len(templateURIs) != 1 || len(promptNames) != 3 {
			t.Errorf("listed %d tools, %d resources, %d templates, %d prompts; want 11, 3, 1, 3",
				len(toolNames), len(resourceURIs), len(templateURIs), len(promptNames))
		}
	})

	t.Run("entity template not in the resources list", func(t *testing.T) {
		if len(resourceURIs) != 3 {
			t.Errorf("resources/list = %v, want exactly the 3 fixed URIs", resourceURIs)
		}
		for _, uri := range resourceURIs {
			if strings.HasPrefix(uri, "sqlgen://entity/") {
				t.Errorf("resources/list carries %q; the entity resource belongs under resources/templates/list only", uri)
			}
		}
	})

	t.Run("every resource URI uses the sqlgen scheme", func(t *testing.T) {
		all := slices.Concat(resourceURIs, templateURIs)
		if len(all) != 4 {
			t.Fatalf("resources + templates = %v, want 4 entries", all)
		}
		for _, uri := range all {
			if !strings.HasPrefix(uri, "sqlgen://") {
				t.Errorf("resource URI %q does not use the sqlgen:// scheme", uri)
			}
		}
	})

	t.Run("every prompt name is kebab-case with the sqlgen- prefix", func(t *testing.T) {
		kebab := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
		if len(promptNames) != 3 {
			t.Fatalf("prompts/list = %v, want 3 prompts", promptNames)
		}
		for _, name := range promptNames {
			if !strings.HasPrefix(name, "sqlgen-") || !kebab.MatchString(name) {
				t.Errorf("prompt name %q is not kebab-case with a sqlgen- prefix", name)
			}
		}
	})
}

// everyToolCall is one call of every tool against the shared fixture store, with
// get_entity also called compact so a compaction that leaked into the stored
// record would change the later full call.
var everyToolCall = []struct {
	name string
	tool string
	args map[string]any
}{
	{name: "list_entities", tool: "sqlgen_list_entities", args: map[string]any{}},
	{name: "get_entity_compact", tool: "sqlgen_get_entity", args: map[string]any{"name": "User", "compact": true}},
	{name: "get_entity", tool: "sqlgen_get_entity", args: map[string]any{"name": "User"}},
	{name: "find_method", tool: "sqlgen_find_method", args: map[string]any{"query": "Get"}},
	{name: "find_referencing", tool: "sqlgen_find_referencing", args: map[string]any{"table": "users"}},
	{name: "describe_relationship", tool: "sqlgen_describe_relationship", args: map[string]any{"entity": "User", "relationship": "roles"}},
	{name: "describe_relationship_compact", tool: "sqlgen_describe_relationship", args: map[string]any{"entity": "User", "relationship": "posts", "compact": true}},
	{name: "find_join_path", tool: "sqlgen_find_join_path", args: map[string]any{"from": "User", "to": "Comment"}},
	{name: "show_sql", tool: "sqlgen_show_sql", args: map[string]any{"entity": "User", "method": "Get"}},
	{name: "get_conventions", tool: "sqlgen_get_conventions", args: map[string]any{}},
	{name: "get_example", tool: "sqlgen_get_example", args: map[string]any{"entity": "User"}},
	{name: "health", tool: "sqlgen_health", args: map[string]any{}},
	{name: "validate_manifest", tool: "sqlgen_validate_manifest", args: map[string]any{}},
}

// TestDispatchToolsIdempotentOnOneServer calls every tool twice on the same server
// and store, and compares the bytes each call returned. A tool that mutated
// shared state on its first call would answer the repeat differently.
func TestDispatchToolsIdempotentOnOneServer(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)

	first := make([]string, len(everyToolCall))
	for i, c := range everyToolCall {
		first[i] = toolText(t, callTool(t, cs, c.tool, c.args))
	}
	covered := map[string]bool{}
	for i, c := range everyToolCall {
		covered[c.tool] = true
		t.Run(c.name, func(t *testing.T) {
			again := toolText(t, callTool(t, cs, c.tool, c.args))
			if again != first[i] {
				t.Errorf("%s(%v) changed on repeat:\n first=%s\n again=%s", c.tool, c.args, first[i], again)
			}
		})
	}
	for _, name := range prdToolNames {
		if !covered[name] {
			t.Errorf("tool %s not exercised", name)
		}
	}
}

// snapshotDir records every file under dir: its bytes, mode and mtime.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := info.Mode().String() + " " + info.ModTime().UTC().Format(time.RFC3339Nano)
		if !d.IsDir() {
			data, err := os.ReadFile(path) //nolint:gosec // path is under a t.TempDir
			if err != nil {
				return err
			}
			entry += " " + string(data)
		}
		out[path] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

// TestDispatchToolsLeaveManifestAndFilesUnchanged loads a manifest from disk, calls
// every tool (validate_manifest both in memory and against the on-disk path),
// and checks that no file in the manifest's directory changed and that the
// served manifest is the one served before.
func TestDispatchToolsLeaveManifestAndFilesUnchanged(t *testing.T) {
	path := copyFixtureManifest(t, t.TempDir())
	dir := filepath.Dir(path)
	store := NewStore(path, nil)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cs, _ := connectSDKClient(t, store, nil)

	beforeFiles := snapshotDir(t, dir)
	beforeRaw := bytes.Clone(store.Raw())
	beforeDoc := jsonTree(t, store.Manifest())
	beforeEntities := jsonTree(t, store.Entities())
	beforeHealth := store.Health()

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"sqlgen_list_entities", map[string]any{}},
		{"sqlgen_get_entity", map[string]any{"name": "User"}},
		{"sqlgen_get_entity", map[string]any{"name": "User", "compact": true}},
		{"sqlgen_find_method", map[string]any{"query": "Get", "fuzzy": true}},
		{"sqlgen_find_referencing", map[string]any{"table": "users"}},
		{"sqlgen_describe_relationship", map[string]any{"entity": "User", "relationship": "Posts"}},
		{"sqlgen_find_join_path", map[string]any{"from": "User", "to": "ActiveUser"}},
		{"sqlgen_show_sql", map[string]any{"entity": "User", "method": "Get"}},
		{"sqlgen_get_conventions", map[string]any{}},
		{"sqlgen_get_example", map[string]any{"entity": "User"}},
		{"sqlgen_health", map[string]any{}},
		{"sqlgen_validate_manifest", map[string]any{}},
		{"sqlgen_validate_manifest", map[string]any{"path": path}},
	}
	called := map[string]bool{}
	for _, c := range calls {
		called[c.tool] = true
		if res := callTool(t, cs, c.tool, c.args); res.IsError {
			t.Fatalf("%s(%v) IsError: %s", c.tool, c.args, toolText(t, res))
		}
	}
	for _, name := range prdToolNames {
		if !called[name] {
			t.Fatalf("tool %s not exercised", name)
		}
	}

	t.Run("no tool writes a file", func(t *testing.T) {
		if diff := cmp.Diff(beforeFiles, snapshotDir(t, dir)); diff != "" {
			t.Errorf("files under the manifest dir changed (-before +after):\n%s", diff)
		}
	})
	t.Run("served manifest unchanged", func(t *testing.T) {
		if !bytes.Equal(beforeRaw, store.Raw()) {
			t.Error("served raw manifest changed after the tool calls")
		}
		if diff := cmp.Diff(beforeDoc, jsonTree(t, store.Manifest())); diff != "" {
			t.Errorf("served document changed (-before +after):\n%s", diff)
		}
		if diff := cmp.Diff(beforeEntities, jsonTree(t, store.Entities())); diff != "" {
			t.Errorf("served entities changed (-before +after):\n%s", diff)
		}
		if diff := cmp.Diff(beforeHealth, store.Health()); diff != "" {
			t.Errorf("store health changed (-before +after):\n%s", diff)
		}
	})
}

// tieStore holds several equal-hop join paths from A to B and several
// references into table "t": both orders can vary under an unstable sort.
func tieStore() *Store {
	a := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "A", Table: "a", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "r4", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r2", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "viaC", Kind: manifest.RelationshipO2M, TargetEntity: "C"},
			{Name: "r3", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r1", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "viaD", Kind: manifest.RelationshipO2M, TargetEntity: "D"},
		},
	}
	b := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "B", Table: "b", PK: manifest.PK{Kind: "single"}}
	c := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "C", Table: "c", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "toB", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "t", Kind: manifest.RelationshipM2O, TargetEntity: "T", FK: &manifest.FK{Table: "c", Columns: []string{"t_id"}}},
		},
	}
	d := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "D", Table: "d", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "toB", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "owner", Kind: manifest.RelationshipM2O, TargetEntity: "T", FK: &manifest.FK{Table: "d", Columns: []string{"owner_id"}}},
			{Name: "editor", Kind: manifest.RelationshipM2O, TargetEntity: "T", FK: &manifest.FK{Table: "d", Columns: []string{"editor_id"}}},
		},
	}
	tt := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "T", Table: "t", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "ds", Kind: manifest.RelationshipO2M, TargetEntity: "D", FK: &manifest.FK{Table: "d", Columns: []string{"owner_id"}}},
			{Name: "cs", Kind: manifest.RelationshipO2M, TargetEntity: "C", FK: &manifest.FK{Table: "c", Columns: []string{"t_id"}}},
		},
	}
	return storeOf(tt, d, c, b, a)
}

// TestDispatchRepeatIsByteIdentical repeats one call on the same server and
// compares the bytes the server wrote, on fixtures where ordering can vary:
// five equal-hop A->B paths plus two 2-hop ones, and four references into t.
func TestDispatchRepeatIsByteIdentical(t *testing.T) {
	cs, _ := connectSDKClient(t, tieStore(), nil)
	tests := []struct {
		name    string
		tool    string
		args    map[string]any
		listKey string
		minLen  int
	}{
		{name: "find_join_path", tool: "sqlgen_find_join_path", args: map[string]any{"from": "A", "to": "B"}, listKey: "paths", minLen: 5},
		{name: "find_referencing", tool: "sqlgen_find_referencing", args: map[string]any{"table": "t"}, listKey: "references", minLen: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callTool(t, cs, tt.tool, tt.args)
			if res.IsError {
				t.Fatalf("%s IsError: %s", tt.tool, toolText(t, res))
			}
			if items, _ := toolTree(t, res)[tt.listKey].([]any); len(items) < tt.minLen {
				t.Fatalf("%s returned %d %s, want at least %d so the order can vary", tt.tool, len(items), tt.listKey, tt.minLen)
			}
			first := toolText(t, res)
			for i := range 5 {
				if again := toolText(t, callTool(t, cs, tt.tool, tt.args)); again != first {
					t.Fatalf("repeat %d of %s(%v) differs:\n first=%s\n again=%s", i+1, tt.tool, tt.args, first, again)
				}
			}
		})
	}
}

// TestDispatchGetEntityIncludesMethodsRelationshipsFilterSort reads the emitted JSON
// of sqlgen_get_entity and checks its methods, relationships, filter and sort.
func TestDispatchGetEntityIncludesMethodsRelationshipsFilterSort(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	res := callTool(t, cs, "sqlgen_get_entity", map[string]any{"name": "User"})
	if res.IsError {
		t.Fatalf("get_entity(User) IsError: %s", toolText(t, res))
	}
	got := toolTree(t, res)

	var user *manifest.Entity
	for _, e := range fixtureEntities() {
		if e.Name == "User" {
			user = e
		}
	}
	want := map[string]any{
		"methods":       jsonTree(t, user.Methods),
		"relationships": jsonTree(t, user.Relationships),
		"filter":        jsonTree(t, user.Filter),
		"sort":          jsonTree(t, user.Sort),
	}
	for key, w := range want {
		if diff := cmp.Diff(w, got[key]); diff != "" {
			t.Errorf("get_entity(User).%s mismatch (-want +got):\n%s", key, diff)
		}
	}

	// Spot-check the literal content so an empty fixture cannot pass.
	methods, _ := got["methods"].(map[string]any)
	var methodNames []string
	for _, group := range []string{"query", "mutation"} {
		ms, _ := methods[group].([]any)
		for _, m := range ms {
			methodNames = append(methodNames, m.(map[string]any)["name"].(string))
		}
	}
	if diff := cmp.Diff([]string{"Get", "GetMany", "Upsert"}, methodNames); diff != "" {
		t.Errorf("method names (-want +got):\n%s", diff)
	}
	rels, _ := got["relationships"].([]any)
	if len(rels) != 2 || rels[0].(map[string]any)["name"] != "posts" || rels[1].(map[string]any)["name"] != "roles" {
		t.Errorf("relationships = %v, want posts and roles", rels)
	}
	if f, _ := got["filter"].(map[string]any); f["type"] != "UserFilter" {
		t.Errorf("filter = %v, want type UserFilter", got["filter"])
	}
	if s, _ := got["sort"].(map[string]any); s["type"] != "UserSort" {
		t.Errorf("sort = %v, want type UserSort", got["sort"])
	}
}

// TestDispatchCompactGetEntityRetainsIdentity compares the emitted compact JSON with
// the full JSON minus exactly the droppable fields: everything else (names,
// types, PK, FK columns, method signatures, join shape, filter fields) must
// come through unchanged.
func TestDispatchCompactGetEntityRetainsIdentity(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	full := toolTree(t, callTool(t, cs, "sqlgen_get_entity", map[string]any{"name": "User"}))
	compact := toolTree(t, callTool(t, cs, "sqlgen_get_entity", map[string]any{"name": "User", "compact": true}))

	// The droppable fields are populated in the full record, so dropping them
	// is observable.
	if full["comment"] == "" || full["examples"] == nil || full["indexes"] == nil {
		t.Fatalf("fixture full record lacks comment/examples/indexes: %v", full)
	}

	want := jsonTree(t, full).(map[string]any)
	want["comment"] = ""
	delete(want, "examples")
	want["indexes"] = nil
	for _, c := range want["columns"].([]any) {
		col := c.(map[string]any)
		col["comment"] = ""
		delete(col, "check")
		delete(col, "default_kind")
	}
	if diff := cmp.Diff(want, compact); diff != "" {
		t.Errorf("compact get_entity(User) differs from full minus droppable fields (-want +got):\n%s", diff)
	}

	// The retained identity is non-empty in the compact output.
	cols := compact["columns"].([]any)
	if c0 := cols[0].(map[string]any); c0["name"] != "id" || c0["go_type"] != "string" || c0["db_type"] != "uuid" {
		t.Errorf("compact column[0] = %v, want name id, go_type string, db_type uuid", c0)
	}
	rels := compact["relationships"].([]any)
	if fk, _ := rels[0].(map[string]any)["fk"].(map[string]any); fmt.Sprint(fk["columns"]) != "[user_id]" || fk["table"] != "posts" {
		t.Errorf("compact relationships[0].fk = %v, want posts.user_id", fk)
	}
	if j, _ := rels[1].(map[string]any)["junction"].(map[string]any); fmt.Sprint(j["local_fk"].(map[string]any)["columns"]) != "[user_id]" ||
		fmt.Sprint(j["target_fk"].(map[string]any)["columns"]) != "[role_id]" {
		t.Errorf("compact relationships[1].junction = %v, want user_id/role_id", j)
	}
	q := compact["methods"].(map[string]any)["query"].([]any)
	if m := q[0].(map[string]any); m["name"] != "Get" || m["returns"] != "*User" || len(m["params"].([]any)) != 1 {
		t.Errorf("compact methods.query[0] = %v, want Get(id) *User", m)
	}
	if ff := compact["filter"].(map[string]any)["fields"].([]any); len(ff) != 1 || ff[0].(map[string]any)["name"] != "Email" {
		t.Errorf("compact filter.fields = %v, want [Email]", ff)
	}
}

// TestDispatchGetExampleOps covers op=write and the documented no-op compact flag.
func TestDispatchGetExampleOps(t *testing.T) {
	d := fixtureDeps()

	t.Run("op=write keeps only write snippets", func(t *testing.T) {
		got, err := getExample(d, GetExampleInput{Entity: "User", Op: "write"})
		if err != nil {
			t.Fatalf("getExample: %v", err)
		}
		want := GetExampleOutput{Entity: "User", Read: []string{}, Write: []string{"u, err := c.Users().Create(ctx, in)"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("op=write mismatch (-want +got):\n%s", diff)
		}
	})

	tests := []struct {
		name string
		op   string
	}{
		{name: "compact is a no-op with no op", op: ""},
		{name: "compact is a no-op with op=read", op: "read"},
		{name: "compact is a no-op with op=write", op: "write"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plain, err := getExample(d, GetExampleInput{Entity: "User", Op: tt.op})
			if err != nil {
				t.Fatalf("getExample: %v", err)
			}
			compact, err := getExample(d, GetExampleInput{Entity: "User", Op: tt.op, Compact: true})
			if err != nil {
				t.Fatalf("getExample compact: %v", err)
			}
			if len(plain.Read)+len(plain.Write) == 0 {
				t.Fatalf("fixture yields no snippets for op %q", tt.op)
			}
			a, _ := json.Marshal(plain)
			b, _ := json.Marshal(compact)
			if !bytes.Equal(a, b) {
				t.Errorf("compact changed the snippets:\n plain=%s\n compact=%s", a, b)
			}
		})
	}
}

// TestDispatchDescribeRelationshipCarriesFilter checks the emitted join shape carries the
// edge's filter clause.
func TestDispatchDescribeRelationshipCarriesFilter(t *testing.T) {
	post := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Post", Table: "posts", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "published_comments", Kind: manifest.RelationshipO2M, TargetEntity: "Comment", FK: &manifest.FK{Table: "comments", Columns: []string{"post_id"}}, Filter: "status = 'published'"},
		},
	}
	comment := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "Comment", Table: "comments", PK: manifest.PK{Kind: "single"}}
	d := &toolDeps{store: storeOf(post, comment)}

	for _, compact := range []bool{false, true} {
		got, err := describeRelationship(d, DescribeRelationshipInput{Entity: "Post", Relationship: "published_comments", Compact: compact})
		if err != nil {
			t.Fatalf("describeRelationship(compact=%v): %v", compact, err)
		}
		tree := jsonTree(t, got).(map[string]any)
		if tree["filter"] != "status = 'published'" {
			t.Errorf("compact=%v: filter = %v, want the edge's filter clause", compact, tree["filter"])
		}
	}
}

// TestDispatchFindReferencingDedupsByFullTuple pins dedup by the full tuple: two identical
// declarations collapse to one result, while a declaration differing only in
// kind stays a separate result.
func TestDispatchFindReferencingDedupsByFullTuple(t *testing.T) {
	target := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Target", Table: "targets", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "link", Kind: manifest.RelationshipO2M, TargetEntity: "A", FK: &manifest.FK{Table: "a", Columns: []string{"ref_id"}}},
			{Name: "link", Kind: manifest.RelationshipO2M, TargetEntity: "A", FK: &manifest.FK{Table: "a", Columns: []string{"ref_id"}}},
			{Name: "link", Kind: manifest.RelationshipM2O, TargetEntity: "A", FK: &manifest.FK{Table: "a", Columns: []string{"ref_id"}}},
		},
	}
	a := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "A", Table: "a", PK: manifest.PK{Kind: "single"}}
	got, err := findReferencing(&toolDeps{store: storeOf(target, a)}, FindReferencingInput{Table: "targets"})
	if err != nil {
		t.Fatalf("findReferencing: %v", err)
	}
	want := []Reference{
		{Entity: "A", RelationshipName: "link", Kind: "m2o", FKColumns: []string{"ref_id"}},
		{Entity: "A", RelationshipName: "link", Kind: "o2m", FKColumns: []string{"ref_id"}},
	}
	if diff := cmp.Diff(want, got.References); diff != "" {
		t.Errorf("references (-want +got):\n%s", diff)
	}

	// PRD §31.3: fk_columns is compared whole, so a two-column key and the
	// one-column key that is its prefix are two results.
	t.Run("two-column and one-column keys into one table", func(t *testing.T) {
		target := &manifest.Entity{
			Kind: manifest.EntityKindTable, Name: "Target", Table: "targets", PK: manifest.PK{Kind: "composite"},
			Relationships: []manifest.Relationship{
				{Name: "link", Kind: manifest.RelationshipO2M, TargetEntity: "A", FK: &manifest.FK{Table: "a", Columns: []string{"workspace_id", "ref_id"}}},
				{Name: "link", Kind: manifest.RelationshipO2M, TargetEntity: "A", FK: &manifest.FK{Table: "a", Columns: []string{"workspace_id"}}},
			},
		}
		got, err := findReferencing(&toolDeps{store: storeOf(target, a)}, FindReferencingInput{Table: "targets"})
		if err != nil {
			t.Fatalf("findReferencing: %v", err)
		}
		want := []Reference{
			{Entity: "A", RelationshipName: "link", Kind: "o2m", FKColumns: []string{"workspace_id"}},
			{Entity: "A", RelationshipName: "link", Kind: "o2m", FKColumns: []string{"workspace_id", "ref_id"}},
		}
		if diff := cmp.Diff(want, got.References); diff != "" {
			t.Errorf("references (-want +got):\n%s", diff)
		}
	})
}

// TestDispatchFindJoinPathMaxHopsDefaultAndClamps pins the max_hops default (4) and both clamps on a
// chain N0 -> ... -> N7, choosing targets where the clamped and unclamped
// values give different answers.
func TestDispatchFindJoinPathMaxHopsDefaultAndClamps(t *testing.T) {
	d := &toolDeps{store: chainStore(8)}
	tests := []struct {
		name     string
		to       string
		maxHops  *int
		wantHops []int
	}{
		{name: "default reaches 4 hops", to: "N4", maxHops: nil, wantHops: []int{4}},
		{name: "default stops before 5 hops", to: "N5", maxHops: nil, wantHops: []int{}},
		{name: "explicit 5 reaches 5 hops", to: "N5", maxHops: new(5), wantHops: []int{5}},
		{name: "0 clamps up to 1", to: "N1", maxHops: new(0), wantHops: []int{1}},
		{name: "negative clamps up to 1", to: "N1", maxHops: new(-3), wantHops: []int{1}},
		{name: "0 clamps to 1, not more", to: "N2", maxHops: new(0), wantHops: []int{}},
		{name: "10 clamps down to 6 and reaches 6 hops", to: "N6", maxHops: new(10), wantHops: []int{6}},
		{name: "10 clamps down to 6 and stops before 7 hops", to: "N7", maxHops: new(10), wantHops: []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findJoinPath(d, FindJoinPathInput{From: "N0", To: tt.to, MaxHops: tt.maxHops})
			if err != nil {
				t.Fatalf("findJoinPath: %v", err)
			}
			hops := []int{}
			for _, p := range got.Paths {
				hops = append(hops, p.Hops)
			}
			if diff := cmp.Diff(tt.wantHops, hops); diff != "" {
				t.Errorf("N0->%s hops (-want +got):\n%s", tt.to, diff)
			}
		})
	}
}

// TestDispatchFindJoinPathShortestFirst uses a graph where the 2-hop path sorts first
// lexicographically and is discovered first, so only a hop-count sort puts
// the 1-hop path ahead of it.
func TestDispatchFindJoinPathShortestFirst(t *testing.T) {
	a := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "A", Table: "a", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "a_detour", Kind: manifest.RelationshipO2M, TargetEntity: "C"},
			{Name: "z_direct", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
		},
	}
	c := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "C", Table: "c", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{{Name: "to_b", Kind: manifest.RelationshipO2M, TargetEntity: "B"}},
	}
	b := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "B", Table: "b", PK: manifest.PK{Kind: "single"}}

	got, err := findJoinPath(&toolDeps{store: storeOf(a, b, c)}, FindJoinPathInput{From: "A", To: "B"})
	if err != nil {
		t.Fatalf("findJoinPath: %v", err)
	}
	want := []JoinPath{
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "z_direct"}}},
		{Hops: 2, Path: []JoinHop{{Entity: "A", Relationship: "a_detour"}, {Entity: "C", Relationship: "to_b"}}},
	}
	if diff := cmp.Diff(want, got.Paths); diff != "" {
		t.Errorf("paths (-want +got):\n%s", diff)
	}
}

// TestDispatchValidateManifestNoPathValidatesLoaded covers the no-path mode: it validates the
// loaded manifest (a schema-invalid loaded manifest is reported invalid), and
// it validates in memory (removing or corrupting the file on disk after the
// load changes nothing).
func TestDispatchValidateManifestNoPathValidatesLoaded(t *testing.T) {
	t.Run("schema-invalid loaded manifest is invalid", func(t *testing.T) {
		// fixtureDeps serves {"schema_version":"0.1.0"}, which fails the schema,
		// from a path that does not exist on disk.
		got, err := validateManifest(fixtureDeps(), ValidateManifestInput{})
		if err != nil {
			t.Fatalf("validateManifest: %v", err)
		}
		if got.Valid || len(got.Errors) == 0 || got.SchemaVersion != "0.1.0" {
			t.Errorf("got %+v, want valid=false, schema_version 0.1.0 and a non-empty errors list", got)
		}
	})

	t.Run("schema-valid loaded manifest is valid", func(t *testing.T) {
		got, err := validateManifest(&toolDeps{store: loadRealFixture(t)}, ValidateManifestInput{})
		if err != nil {
			t.Fatalf("validateManifest: %v", err)
		}
		if !got.Valid || len(got.Errors) != 0 {
			t.Errorf("got %+v, want valid=true and no errors", got)
		}
	})

	tests := []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{name: "file removed after load still validates in memory", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "file corrupted after load still validates in memory", mutate: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(`{"schema_version":"0.1.0"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := copyFixtureManifest(t, t.TempDir())
			store := NewStore(path, nil)
			if err := store.Load(); err != nil {
				t.Fatalf("Load: %v", err)
			}
			tt.mutate(t, path)
			got, err := validateManifest(&toolDeps{store: store}, ValidateManifestInput{})
			if err != nil {
				t.Fatalf("validateManifest after the disk change: %v", err)
			}
			if !got.Valid || len(got.Errors) != 0 {
				t.Errorf("got %+v, want valid=true from the in-memory manifest", got)
			}
		})
	}
}

// TestDispatchManifestResourceThreshold pins the size-warning threshold at the
// literal 256 KB (262144 bytes) and checks the warning advises targeted tools.
func TestDispatchManifestResourceThreshold(t *testing.T) {
	warningFor := func(t *testing.T, size int) (string, bool) {
		t.Helper()
		raw := bytes.Repeat([]byte("x"), size)
		res, err := manifestResource(&toolDeps{store: storeWithRaw(fixtureDoc(), raw)}, "sqlgen://manifest")
		if err != nil {
			t.Fatalf("manifestResource: %v", err)
		}
		if len(res.Contents) != 1 || len(res.Contents[0].Text) != size {
			t.Fatalf("served %d contents, want one full %d-byte body", len(res.Contents), size)
		}
		w, ok := res.Contents[0].Meta["warning"].(string)
		return w, ok
	}

	tests := []struct {
		name        string
		size        int
		wantWarning bool
	}{
		{name: "exactly 256 KB has no warning", size: 256 * 1024, wantWarning: false},
		{name: "one byte over 256 KB warns", size: 256*1024 + 1, wantWarning: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := warningFor(t, tt.size)
			if ok != tt.wantWarning {
				t.Errorf("%d-byte manifest: warning present = %v, want %v", tt.size, ok, tt.wantWarning)
			}
		})
	}

	t.Run("warning advises targeted tool calls", func(t *testing.T) {
		w, ok := warningFor(t, 256*1024+1)
		if !ok {
			t.Fatal("no warning above the threshold")
		}
		named := regexp.MustCompile(`sqlgen_[a-z_]+`).FindAllString(w, -1)
		if len(named) == 0 {
			t.Errorf("warning %q names no sqlgen tool to call instead", w)
		}
		for _, n := range named {
			if !slices.Contains(prdToolNames, n) {
				t.Errorf("warning %q names %q, which is not a sqlgen tool", w, n)
			}
		}
	})
}

// TestDispatchConventionsResourceServesBlock reads sqlgen://conventions through the
// SDK and compares the whole body with the manifest's conventions block.
func TestDispatchConventionsResourceServesBlock(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "sqlgen://conventions"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	var got any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if diff := cmp.Diff(jsonTree(t, fixtureDoc().Conventions), got); diff != "" {
		t.Errorf("conventions body (-want +got):\n%s", diff)
	}
	block := got.(map[string]any)
	sentinels, _ := block["error_sentinels"].([]any)
	if len(sentinels) != 1 || sentinels[0].(map[string]any)["name"] != "ErrNotFound" {
		t.Errorf("error_sentinels = %v, want [ErrNotFound]", block["error_sentinels"])
	}
	if co, _ := block["call_options"].(map[string]any); co["type"] != "CallOptions" {
		t.Errorf("call_options = %v, want type CallOptions", block["call_options"])
	}
	if p, _ := block["pagination"].(map[string]any); p["page_type"] != "Page" {
		t.Errorf("pagination = %v, want page_type Page", block["pagination"])
	}
}

// TestDispatchPromptExpansions gets each prompt through the SDK by its PRD name and
// checks the expansion carries what §31.3 says the prompt does.
func TestDispatchPromptExpansions(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	tests := []struct {
		name   string
		prompt string
		args   map[string]string
		want   []string
	}{
		{
			name: "write-query loads the entity with compact false", prompt: "sqlgen-write-query",
			args: map[string]string{"entity": "User", "goal": "list admins"},
			want: []string{`sqlgen_get_entity({"name": "User", "compact": false})`},
		},
		{
			name: "write-query asks for a Go snippet with real method names", prompt: "sqlgen-write-query",
			args: map[string]string{"entity": "User", "goal": "list admins"},
			want: []string{"go snippet", "real method names", "actual generated method names"},
		},
		{
			name: "add-relationship-usage loads the entity example", prompt: "sqlgen-add-relationship-usage",
			args: map[string]string{"entity": "User", "relationship": "roles"},
			want: []string{`sqlgen_get_example({"entity": "User"})`},
		},
		{
			name: "add-relationship-usage walks through preloading joining writing", prompt: "sqlgen-add-relationship-usage",
			args: map[string]string{"entity": "User", "relationship": "roles"},
			want: []string{"walk me through", "preloading", "joining", "writing across the roles relationship on user"},
		},
		{
			name: "add-relationship-usage includes pagination and filter caveats", prompt: "sqlgen-add-relationship-usage",
			args: map[string]string{"entity": "User", "relationship": "roles"},
			want: []string{"sqlgen_get_conventions({})", "pagination and filter caveats", "pagination or filter caveats"},
		},
		{
			name: "debug-not-found loads the entity", prompt: "sqlgen-debug-not-found",
			args: map[string]string{"entity": "User"},
			want: []string{`sqlgen_get_entity({"name": "User"})`},
		},
		{
			name: "debug-not-found loads the error sentinels", prompt: "sqlgen-debug-not-found",
			args: map[string]string{"entity": "User"},
			want: []string{"sqlgen_get_conventions({})", "error-sentinels"},
		},
		{
			name: "debug-not-found suggests tenancy soft-delete and PK checks", prompt: "sqlgen-debug-not-found",
			args: map[string]string{"entity": "User", "method": "Get"},
			want: []string{"tenancy filtering", "soft-delete state", "primary-key shape"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: tt.prompt, Arguments: tt.args})
			if err != nil {
				t.Fatalf("GetPrompt(%s): %v", tt.prompt, err)
			}
			var b strings.Builder
			for _, m := range res.Messages {
				if tc, ok := m.Content.(*mcp.TextContent); ok {
					b.WriteString(tc.Text)
				}
			}
			text := strings.ToLower(b.String())
			for _, w := range tt.want {
				if !strings.Contains(text, strings.ToLower(w)) {
					t.Errorf("%s expansion lacks %q:\n%s", tt.prompt, w, b.String())
				}
			}
		})
	}
}

// TestDispatchReloadNeverSendsPromptListChanged triggers the watch-reload path twice and
// checks that tools and resources list_changed arrive while prompts
// list_changed never does.
func TestDispatchReloadNeverSendsPromptListChanged(t *testing.T) {
	toolsCh := make(chan struct{}, 16)
	resourcesCh := make(chan struct{}, 16)
	promptsCh := make(chan struct{}, 16)
	opts := &mcp.ClientOptions{
		ToolListChangedHandler:     func(context.Context, *mcp.ToolListChangedRequest) { toolsCh <- struct{}{} },
		ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) { resourcesCh <- struct{}{} },
		PromptListChangedHandler:   func(context.Context, *mcp.PromptListChangedRequest) { promptsCh <- struct{}{} },
	}
	cs, srv := connectSDKClient(t, newFixtureStore(), opts)

	await := func(ch <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("no %s within 10s of a reload", what)
		}
	}
	for range 2 {
		srv.onManifestReload()
		await(toolsCh, "notifications/tools/list_changed")
		await(resourcesCh, "notifications/resources/list_changed")
		if _, err := cs.ListPrompts(t.Context(), nil); err != nil {
			t.Fatalf("ListPrompts after reload: %v", err)
		}
	}
	select {
	case <-promptsCh:
		t.Error("received notifications/prompts/list_changed on a reload, want none")
	default:
	}
}

// TestDispatchSuggestions covers the suggestion rules through the tools: the
// candidate set is the named entity's own names, matching ignores case, and
// the order is distance first.
func TestDispatchSuggestions(t *testing.T) {
	d := fixtureDeps()
	tests := []struct {
		name string
		call func() error
		code ErrorCode
		want []string
	}{
		{
			// GetMany exists only on User; Post's own Get is distance 3.
			name: "show_sql suggests only the named entity's methods",
			call: func() error { _, err := showSQL(d, ShowSQLInput{Entity: "Post", Method: "GetMan"}); return err },
			code: CodeMethodNotFound, want: []string{},
		},
		{
			name: "show_sql suggests the method on the entity that has it",
			call: func() error { _, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "GetMan"}); return err },
			code: CodeMethodNotFound, want: []string{"GetMany"},
		},
		{
			// roles exists only on User; Post's own comments is far away.
			name: "describe_relationship suggests only the named entity's relationships",
			call: func() error {
				_, err := describeRelationship(d, DescribeRelationshipInput{Entity: "Post", Relationship: "role"})
				return err
			},
			code: CodeRelationshipNotFound, want: []string{},
		},
		{
			// Case-sensitively USER is distance 3 from User; ignoring case it is 0.
			name: "get_entity matches ignoring case",
			call: func() error { _, err := getEntity(d, GetEntityInput{Name: "USER"}); return err },
			code: CodeEntityNotFound, want: []string{"User"},
		},
		{
			name: "describe_relationship matches ignoring case",
			call: func() error {
				_, err := describeRelationship(d, DescribeRelationshipInput{Entity: "User", Relationship: "ROLES"})
				return err
			},
			code: CodeRelationshipNotFound, want: []string{"roles"},
		},
		{
			name: "show_sql matches ignoring case",
			call: func() error { _, err := showSQL(d, ShowSQLInput{Entity: "User", Method: "GETMANY"}); return err },
			code: CodeMethodNotFound, want: []string{"GetMany"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNotFound(t, tt.call(), tt.code, tt.want)
		})
	}

	t.Run("get_entity orders by distance before name", func(t *testing.T) {
		// cats: Cat is distance 1, Bat distance 2; a name sort gives [Bat, Cat].
		bat := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "Bat", Table: "bats", PK: manifest.PK{Kind: "single"}}
		cat := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "Cat", Table: "cats", PK: manifest.PK{Kind: "single"}}
		_, err := getEntity(&toolDeps{store: storeOf(bat, cat)}, GetEntityInput{Name: "cats"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"Cat", "Bat"})
	})

	unit := []struct {
		name   string
		target string
		cands  []string
		want   []string
	}{
		{name: "suggest matches ignoring case", target: "USER", cands: []string{"User", "Users"}, want: []string{"User", "Users"}},
		{name: "suggest orders by distance before name", target: "cat", cands: []string{"bat", "cat"}, want: []string{"cat", "bat"}},
	}
	for _, tt := range unit {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, suggest(tt.target, tt.cands)); diff != "" {
				t.Errorf("suggest(%q, %v) (-want +got):\n%s", tt.target, tt.cands, diff)
			}
		})
	}
}

// TestDispatchToolErrorChannel drives each per-tool business error through the SDK
// and checks the IsError result: its code, its message, the text content with
// suggestions inlined, and an empty-but-present suggestions array on a miss.
func TestDispatchToolErrorChannel(t *testing.T) {
	cs, _ := connectSDKClient(t, newFixtureStore(), nil)
	missing := filepath.Join(t.TempDir(), "missing.json")

	tests := []struct {
		name            string
		tool            string
		args            map[string]any
		code            int
		wantInMessage   string
		wantSuggestions []string // nil: the code carries no suggestions
	}{
		{name: "-32003 unknown entity", tool: "sqlgen_get_entity", args: map[string]any{"name": "Usr"}, code: -32003, wantInMessage: "Usr", wantSuggestions: []string{"User"}},
		{name: "-32004 unknown relationship", tool: "sqlgen_describe_relationship", args: map[string]any{"entity": "User", "relationship": "role"}, code: -32004, wantInMessage: "role", wantSuggestions: []string{"roles"}},
		{name: "-32005 no sql body", tool: "sqlgen_show_sql", args: map[string]any{"entity": "User", "method": "Upsert"}, code: -32005, wantInMessage: "Upsert"},
		{name: "-32006 unknown method", tool: "sqlgen_show_sql", args: map[string]any{"entity": "User", "method": "Ge"}, code: -32006, wantInMessage: "Ge", wantSuggestions: []string{"Get"}},
		{name: "-32603 internal from a tool body", tool: "sqlgen_validate_manifest", args: map[string]any{"path": missing}, code: -32603, wantInMessage: "missing.json"},
		{name: "-32003 far miss has empty suggestions", tool: "sqlgen_get_entity", args: map[string]any{"name": "xyzzy"}, code: -32003, wantInMessage: "xyzzy", wantSuggestions: []string{}},
		{name: "-32004 far miss has empty suggestions", tool: "sqlgen_describe_relationship", args: map[string]any{"entity": "User", "relationship": "xyzzy"}, code: -32004, wantInMessage: "xyzzy", wantSuggestions: []string{}},
		{name: "-32006 far miss has empty suggestions", tool: "sqlgen_show_sql", args: map[string]any{"entity": "User", "method": "xyzzy"}, code: -32006, wantInMessage: "xyzzy", wantSuggestions: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callTool(t, cs, tt.tool, tt.args)
			if !res.IsError {
				t.Fatalf("%s(%v) is not an IsError result: %s", tt.tool, tt.args, toolText(t, res))
			}
			raw, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatalf("marshal structured content: %v", err)
			}
			var env map[string]json.RawMessage
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatalf("structured content is not an object: %s", raw)
			}
			var code int
			var message string
			_ = json.Unmarshal(env["code"], &code)
			_ = json.Unmarshal(env["message"], &message)
			if code != tt.code {
				t.Errorf("envelope code = %d, want %d (%s)", code, tt.code, raw)
			}
			if message == "" || !strings.Contains(message, tt.wantInMessage) {
				t.Errorf("envelope message = %q, want a message naming %q", message, tt.wantInMessage)
			}
			if _, nested := env["data"]; nested {
				t.Errorf("envelope nests a data field: %s", raw)
			}

			text := toolText(t, res)
			if !strings.HasPrefix(text, message) {
				t.Errorf("text content = %q, want the readable message %q first", text, message)
			}
			if tt.wantSuggestions == nil {
				return
			}
			sraw, present := env["suggestions"]
			if !present {
				t.Fatalf("envelope lacks suggestions: %s", raw)
			}
			var got []string
			if err := json.Unmarshal(sraw, &got); err != nil || got == nil {
				t.Fatalf("suggestions = %s, want a JSON array", sraw)
			}
			if diff := cmp.Diff(tt.wantSuggestions, got); diff != "" {
				t.Errorf("suggestions (-want +got):\n%s", diff)
			}
			for _, s := range tt.wantSuggestions {
				if !strings.Contains(text, s) {
					t.Errorf("text content %q does not inline suggestion %q", text, s)
				}
			}
		})
	}

	t.Run("resource far miss has empty data.suggestions", func(t *testing.T) {
		_, err := entityResource(fixtureDeps(), "sqlgen://entity/xyzzy")
		var je *jsonrpc.Error
		if !errors.As(err, &je) {
			t.Fatalf("error = %T (%v), want *jsonrpc.Error", err, err)
		}
		if string(je.Data) != `{"suggestions":[]}` {
			t.Errorf("data = %s, want {\"suggestions\":[]}", je.Data)
		}
	})
}
