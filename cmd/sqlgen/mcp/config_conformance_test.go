package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedRawMCPJSON writes content verbatim to <root>/.mcp.json.
func seedRawMCPJSON(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}
}

// readRawMCPJSON returns the bytes of <root>/.mcp.json.
func readRawMCPJSON(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // path is a t.TempDir file
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	return string(raw)
}

// managedEntry is a sqlgen-managed entry serving manifest (stored verbatim).
func managedEntry(manifest string) map[string]any {
	return map[string]any{
		"command":                "sqlgen",
		"args":                   []any{"mcp", "serve", "--manifest", manifest},
		sentinelManaged:          true,
		sentinelManifestPath:     manifest,
		sentinelGeneratorVersion: "0.1.0",
	}
}

// seedMCPJSONTree writes tree as JSON to <root>/.mcp.json.
func seedMCPJSONTree(t *testing.T, root string, tree map[string]any) {
	t.Helper()
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshalling seed tree: %v", err)
	}
	seedRawMCPJSON(t, root, string(raw))
}

// The match key is compared as an absolute path: an existing managed entry
// whose x-sqlgen-manifest spells the same file differently (absolute, or with
// redundant segments) is the matching entry, so it is updated in place under
// its own key and no second entry is added under server_key.
func TestMergeMatchComparesAbsolutePaths(t *testing.T) {
	tests := []struct {
		name   string
		stored func(root string) string
	}{
		{name: "stored absolute, emitted relative", stored: func(root string) string {
			return filepath.Join(root, "models", "manifest_gen.json")
		}},
		{name: "stored with redundant segments", stored: func(string) string {
			return "./models/../models/./manifest_gen.json"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, opts := mergeDir(t)
			seedMCPJSONTree(t, root, map[string]any{"mcpServers": map[string]any{
				"custom-key": managedEntry(tt.stored(root)),
			}})

			if _, err := Merge(opts); err != nil {
				t.Fatalf("Merge() unexpected error: %v", err)
			}
			got := servers(t, readConfig(t, root, ".mcp.json"))
			want := map[string]any{"custom-key": wantEntry()}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Merge() mcpServers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// When server_key and its numbered successors are held by consumer-authored
// entries, the entry lands on the first free key in the sequence sqlgen-2,
// sqlgen-3, ... and every occupant is left as it was.
func TestMergeCollisionKeySequence(t *testing.T) {
	tests := []struct {
		name    string
		taken   []string
		wantKey string
	}{
		{name: "sqlgen and sqlgen-2 taken", taken: []string{"sqlgen", "sqlgen-2"}, wantKey: "sqlgen-3"},
		{name: "sqlgen through sqlgen-3 taken", taken: []string{"sqlgen", "sqlgen-2", "sqlgen-3"}, wantKey: "sqlgen-4"},
		{name: "gap at sqlgen-2", taken: []string{"sqlgen", "sqlgen-3"}, wantKey: "sqlgen-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, opts := mergeDir(t)
			seeded := map[string]any{}
			for _, k := range tt.taken {
				seeded[k] = map[string]any{"command": "consumer-" + k}
			}
			seedMCPJSONTree(t, root, map[string]any{"mcpServers": seeded})

			warnings, err := Merge(opts)
			if err != nil {
				t.Fatalf("Merge() unexpected error: %v", err)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], fmt.Sprintf("%q", tt.wantKey)) {
				t.Errorf("Merge() warnings = %q, want one collision warning naming %q", warnings, tt.wantKey)
			}
			want := map[string]any{tt.wantKey: wantEntry()}
			for _, k := range tt.taken {
				want[k] = map[string]any{"command": "consumer-" + k}
			}
			if diff := cmp.Diff(want, servers(t, readConfig(t, root, ".mcp.json"))); diff != "" {
				t.Errorf("Merge() mcpServers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A monorepo sibling registered under its own key whose manifest is not on
// disk (gitignored or not yet generated) is swept on the next run, while a
// sibling whose manifest exists survives and a consumer entry is untouched.
func TestMergeAbsentSiblingUnderOwnKeySwept(t *testing.T) {
	tests := []struct {
		name   string
		action MergeAction
	}{
		{name: "upsert run", action: MergeUpsert},
		{name: "removal run", action: MergeRemove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, opts := mergeDir(t)
			opts.Action = tt.action
			writeManifestFile(t, root, "billing/manifest_gen.json")
			seedMCPJSONTree(t, root, map[string]any{"mcpServers": map[string]any{
				"sqlgen-analytics": managedEntry("./analytics/manifest_gen.json"),
				"sqlgen-billing":   managedEntry("./billing/manifest_gen.json"),
				"linear":           map[string]any{"command": "linear-mcp"},
			}})

			if _, err := Merge(opts); err != nil {
				t.Fatalf("Merge() unexpected error: %v", err)
			}
			want := map[string]any{
				"sqlgen-billing": managedEntry("./billing/manifest_gen.json"),
				"linear":         map[string]any{"command": "linear-mcp"},
			}
			if tt.action == MergeUpsert {
				want["sqlgen"] = wantEntry()
			}
			if diff := cmp.Diff(want, servers(t, readConfig(t, root, ".mcp.json"))); diff != "" {
				t.Errorf("Merge() mcpServers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The first run rewrites an existing, non-canonical file into the canonical
// form: 2-space indentation and keys sorted alphabetically at every nesting
// level (array order is data and is kept).
func TestMergeCanonicalOutputExactBytes(t *testing.T) {
	root, opts := mergeDir(t)
	seedRawMCPJSON(t, root, "{\"zeta\": 1,\n\t\"mcpServers\": {\"linear\": {\"env\": {\"Z_KEY\": \"z\", \"A_KEY\": \"a\"},"+
		"\t\"command\": \"linear-mcp\", \"args\": [\"--b\", \"--a\"]}},\n"+
		"    \"alpha\": {\"b\": 2, \"a\": {\"y\": true, \"x\": null}}}")

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	const want = `{
  "alpha": {
    "a": {
      "x": null,
      "y": true
    },
    "b": 2
  },
  "mcpServers": {
    "linear": {
      "args": [
        "--b",
        "--a"
      ],
      "command": "linear-mcp",
      "env": {
        "A_KEY": "a",
        "Z_KEY": "z"
      }
    },
    "sqlgen": {
      "args": [
        "mcp",
        "serve",
        "--manifest",
        "./models/manifest_gen.json"
      ],
      "command": "sqlgen",
      "x-sqlgen-generator-version": "0.42.0",
      "x-sqlgen-managed": true,
      "x-sqlgen-manifest": "./models/manifest_gen.json"
    }
  },
  "zeta": 1
}
`
	if diff := cmp.Diff(want, readRawMCPJSON(t, root)); diff != "" {
		t.Errorf("Merge() output is not the canonical form (-want +got):\n%s", diff)
	}
}

// The target is replaced by renaming a fully written temp file over it, not
// rewritten in place: a hard link to the old file still holds the old bytes,
// the target is a new file, and no temp file is left behind. (The fsync step
// between write and rename has no observable effect short of a crash.)
func TestMergeWritesByRenameNotInPlace(t *testing.T) {
	root, opts := mergeDir(t)
	const old = `{"mcpServers": {"linear": {"command": "linear-mcp"}}}`
	seedRawMCPJSON(t, root, old)
	target := filepath.Join(root, ".mcp.json")
	keep := filepath.Join(root, "old-link.json")
	if err := os.Link(target, keep); err != nil {
		t.Skipf("hard links unsupported here: %v", err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}

	linked, err := os.ReadFile(keep) //nolint:gosec // path is a t.TempDir file
	if err != nil {
		t.Fatalf("reading hard link: %v", err)
	}
	if string(linked) != old {
		t.Errorf("old file was rewritten in place: hard link now holds %q, want %q", linked, old)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if os.SameFile(before, after) {
		t.Errorf("Merge() wrote into the existing file; want it replaced by a renamed temp file")
	}
	if got := servers(t, readConfig(t, root, ".mcp.json")); got["sqlgen"] == nil {
		t.Errorf("Merge() target lacks the sqlgen entry after replacement: %v", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("listing module root: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("Merge() left a temp file behind: %s", e.Name())
		}
	}
}

// A removal that empties mcpServers keeps the file when another top-level
// field remains, and that field is preserved unchanged.
func TestMergeRemoveKeepsFileWithOtherTopLevelField(t *testing.T) {
	root, opts := mergeDir(t)
	opts.Action = MergeRemove
	other := map[string]any{"inputs": []any{"a", "b"}, "id": "x"}
	seedMCPJSONTree(t, root, map[string]any{
		"mcpServers": map[string]any{"sqlgen": wantEntry()},
		"otherTool":  other,
	})

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatalf("Merge() removal deleted the file although another top-level field remained: %v", err)
	}
	got := readConfig(t, root, ".mcp.json")
	if diff := cmp.Diff(other, got["otherTool"]); diff != "" {
		t.Errorf("Merge() changed the other top-level field (-want +got):\n%s", diff)
	}
	if s, ok := got["mcpServers"].(map[string]any); ok && len(s) != 0 {
		t.Errorf("Merge() removal left mcpServers entries: %v", s)
	}
	for k := range got {
		if k != "otherTool" && k != "mcpServers" {
			t.Errorf("Merge() removal added top-level field %q", k)
		}
	}
}

// The rendered command_template is split into tokens without a shell: variable
// references, command substitution, globs, tilde and shell operators reach the
// entry's command and args literally.
func TestMergeCommandTemplateNoShellExpansion(t *testing.T) {
	t.Setenv("FC_SHOULD_NOT_EXPAND", "expanded")
	root, opts := mergeDir(t)
	opts.CommandTemplate = `env "$FC_SHOULD_NOT_EXPAND" ${HOME} $(whoami) ` + "`id`" + ` ~/bin *.json a;b c|d e&&f {{ manifest_path }}`

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	entry, ok := servers(t, readConfig(t, root, ".mcp.json"))["sqlgen"].(map[string]any)
	if !ok {
		t.Fatalf("Merge() wrote no sqlgen entry")
	}
	if entry["command"] != "env" {
		t.Errorf("command = %v, want %q", entry["command"], "env")
	}
	wantArgs := []any{"$FC_SHOULD_NOT_EXPAND", "${HOME}", "$(whoami)", "`id`", "~/bin", "*.json", "a;b", "c|d", "e&&f", "./models/manifest_gen.json"}
	if diff := cmp.Diff(wantArgs, entry["args"]); diff != "" {
		t.Errorf("args mismatch (-want +got):\n%s", diff)
	}
}

// copyFixtureManifest copies the package fixture manifest to dir/manifest_gen.json.
func copyFixtureManifest(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/manifest_gen.json")
	if err != nil {
		t.Fatalf("reading fixture manifest: %v", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, manifestFilename)
	if err := os.WriteFile(path, raw, 0o600); err != nil { //nolint:gosec // path is under a t.TempDir directory
		t.Fatalf("writing manifest: %v", err)
	}
	return path
}

// runOverStdio runs Run with opts over in-memory stdio pipes and connects an SDK
// client to it. The server is shut down at cleanup.
func runOverStdio(t *testing.T, opts Options, clientOpts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	opts.Stdio = true
	if opts.Version == "" {
		opts.Version = "test"
	}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, inR, outW, opts) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "fc-test", Version: "0"}, clientOpts)
	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()
	session, err := client.Connect(connCtx, &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		cancel()
		t.Fatalf("connecting over stdio: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		_ = inW.Close()
		_ = outR.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() returned %v on shutdown, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Run() did not return within 5s of shutdown")
		}
	})
	return session
}

// callToolStructured calls a tool and decodes its structured content.
func callToolStructured(t *testing.T, session *mcp.ClientSession, name string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned a tool error: %v", name, res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshalling %s output: %v", name, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s output: %v", name, err)
	}
	return out
}

// listedEntityNames projects a sqlgen_list_entities result to entity names.
func listedEntityNames(out map[string]any) []string {
	list, _ := out["entities"].([]any)
	var names []string
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return names
}

// realPath resolves symlinks (macOS temp dirs live behind /var -> /private/var).
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("resolving %s: %v", p, err)
	}
	return r
}

// With no --manifest, the server starts discovery from the working directory
// and walks up: started two levels below the manifest, it serves that manifest.
func TestRunDiscoversManifestFromWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	want := copyFixtureManifest(t, root)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(nested)

	session := runOverStdio(t, Options{ManifestPath: ""}, nil)
	health := callToolStructured(t, session, "sqlgen_health")
	got, _ := health["manifest_path"].(string)
	if got == "" || realPath(t, got) != realPath(t, want) {
		t.Errorf("sqlgen_health.manifest_path = %q, want the manifest above the working directory %q", got, want)
	}
}

// runHTTPOnly runs Run with only the HTTP transport until cleanup and
// returns its address once it accepts connections.
func runHTTPOnly(t *testing.T, manifest string) string {
	t.Helper()
	addr := freeLoopbackAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, io.NopCloser(strings.NewReader("")), discardWriteCloser{io.Discard}, Options{
			Version: "test", ManifestPath: manifest, HTTPAddr: addr,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("Run() did not return within 10s of cancel")
		}
	})
	waitForListen(t, addr)
	return addr
}

type discardWriteCloser struct{ io.Writer }

func (discardWriteCloser) Close() error { return nil }

// The HTTP transport keeps no session: a request with no prior initialize and
// no Mcp-Session-Id is served, and so is one carrying a session ID the server
// never issued (a session-keeping server answers that with 404).
func TestRunHTTPKeepsNoSession(t *testing.T) {
	addr := runHTTPOnly(t, "testdata/manifest_gen.json")
	const listTools = `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`
	tests := []struct {
		name   string
		header map[string]string
	}{
		{name: "no session header and no initialize", header: nil},
		{name: "session id never issued", header: map[string]string{"Mcp-Session-Id": "fc-never-issued-session"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, msg := postJSONRPC(t, "http://"+addr+"/mcp", listTools, tt.header)
			if status != http.StatusOK {
				t.Fatalf("tools/list status = %d, want 200 (reply %v)", status, msg)
			}
			result, ok := msg["result"].(map[string]any)
			if !ok {
				t.Fatalf("tools/list reply = %v, want a result", msg)
			}
			if tools, _ := result["tools"].([]any); len(tools) != 11 {
				t.Errorf("tools/list returned %d tools, want 11", len(tools))
			}
		})
	}
}

// With the HTTP transport running in the same process, stdio still serves
// requests: a stdio client initializes and calls a tool while /mcp answers.
func TestRunStdioServesWhileHTTPRuns(t *testing.T) {
	addr := freeLoopbackAddr(t)
	session := runOverStdio(t, Options{ManifestPath: "testdata/manifest_gen.json", HTTPAddr: addr}, nil)
	waitForListen(t, addr)
	url := "http://" + addr + "/mcp"

	status, msg := postJSONRPC(t, url, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, nil)
	if status != http.StatusOK || msg["result"] == nil {
		t.Fatalf("HTTP transport not serving: status %d, reply %v", status, msg)
	}
	names := listedEntityNames(callToolStructured(t, session, "sqlgen_list_entities"))
	if !slices.Contains(names, "ActiveUser") {
		t.Errorf("stdio sqlgen_list_entities = %v, want ActiveUser present", names)
	}
	status, msg = postJSONRPC(t, url, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, nil)
	if status != http.StatusOK || msg["result"] == nil {
		t.Errorf("HTTP transport stopped after the stdio call: status %d, reply %v", status, msg)
	}
}

// With watch off, a changed manifest is not reloaded. A watch-on server on the
// same file is the control: once it has reloaded and notified, the watch-off
// server still serves the old data.
func TestRunNoWatchDoesNotReload(t *testing.T) {
	path := copyFixtureManifest(t, t.TempDir())
	reloaded := make(chan struct{}, 8)
	control := runOverStdio(t, Options{ManifestPath: path, Watch: true}, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case reloaded <- struct{}{}:
			default:
			}
		},
	})
	off := runOverStdio(t, Options{ManifestPath: path, Watch: false}, nil)
	if names := listedEntityNames(callToolStructured(t, off, "sqlgen_list_entities")); !slices.Contains(names, "ActiveUser") {
		t.Fatalf("pre-change entities = %v, want ActiveUser", names)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // path is a t.TempDir file
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	mutated := strings.Replace(string(raw), `"name": "ActiveUser"`, `"name": "ArchivedUser"`, 1)
	if mutated == string(raw) {
		t.Fatalf("fixture mutation did not apply")
	}
	tmp := path + ".regen"
	if err := os.WriteFile(tmp, []byte(mutated), 0o600); err != nil { //nolint:gosec // path is under a t.TempDir directory
		t.Fatalf("writing regen file: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("renaming regen file: %v", err)
	}

	waitForReload(t, reloaded)
	if names := listedEntityNames(callToolStructured(t, control, "sqlgen_list_entities")); !slices.Contains(names, "ArchivedUser") {
		t.Fatalf("control server entities after reload = %v, want ArchivedUser", names)
	}
	names := listedEntityNames(callToolStructured(t, off, "sqlgen_list_entities"))
	if !slices.Contains(names, "ActiveUser") || slices.Contains(names, "ArchivedUser") {
		t.Errorf("watch-off server entities = %v, want the original manifest (ActiveUser, no ArchivedUser)", names)
	}
	if h := callToolStructured(t, off, "sqlgen_health"); h["watch_enabled"] != false || h["last_reload_at"] != nil {
		t.Errorf("watch-off health = %v, want watch_enabled false and no last_reload_at", h)
	}
}
