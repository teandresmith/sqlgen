package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// mergeDir builds a temp module root holding an on-disk manifest (so the
// stale sweep sees a live file) and returns the root plus default options
// targeting <root>/.mcp.json.
func mergeDir(t *testing.T) (string, MergeOptions) {
	t.Helper()
	root := t.TempDir()
	writeManifestFile(t, root, "models/manifest_gen.json")
	return root, MergeOptions{
		ConfigPath:   ".mcp.json",
		ModuleRoot:   root,
		ServerKey:    "sqlgen",
		ManifestPath: "models/manifest_gen.json",
		Version:      "0.42.0",
		Action:       MergeUpsert,
	}
}

// writeManifestFile creates an empty placeholder manifest at the given
// module-root-relative path so stale-entry sweeps treat it as live.
func writeManifestFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}
}

// readConfig parses the target .mcp.json into a generic tree.
func readConfig(t *testing.T, root, rel string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}
	return parsed
}

// servers extracts the mcpServers object.
func servers(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	s, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or not an object: %v", cfg["mcpServers"])
	}
	return s
}

// wantEntry is the canonical sqlgen entry for the default mergeDir options.
func wantEntry() map[string]any {
	return map[string]any{
		"command":                "sqlgen",
		"args":                   []any{"mcp", "serve", "--manifest", "./models/manifest_gen.json"},
		sentinelManaged:          true,
		sentinelManifestPath:     "./models/manifest_gen.json",
		sentinelGeneratorVersion: "0.42.0",
	}
}

func TestMergeCreatesNewFile(t *testing.T) {
	root, opts := mergeDir(t)

	warnings, err := Merge(opts)
	if err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Merge() warnings = %v, want none", warnings)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	if diff := cmp.Diff(map[string]any{"sqlgen": wantEntry()}, got); diff != "" {
		t.Errorf("Merge() created file mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeUpsertAppendsAlongsideOtherTools(t *testing.T) {
	root, opts := mergeDir(t)
	existing := `{
  "mcpServers": {
    "linear": {"command": "linear-mcp", "args": []}
  },
  "otherTopLevel": true
}`
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	cfg := readConfig(t, root, ".mcp.json")
	got := servers(t, cfg)
	want := map[string]any{
		"linear": map[string]any{"command": "linear-mcp", "args": []any{}},
		"sqlgen": wantEntry(),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Merge() servers mismatch (-want +got):\n%s", diff)
	}
	if cfg["otherTopLevel"] != true {
		t.Errorf("Merge() dropped unrelated top-level field otherTopLevel")
	}
}

func TestMergeUpdatesManagedEntryInPlace(t *testing.T) {
	root, opts := mergeDir(t)
	// Existing managed entry under a custom key with outdated args + version.
	existing := map[string]any{
		"mcpServers": map[string]any{
			"my-models": map[string]any{
				"command":                "sqlgen",
				"args":                   []any{"mcp", "serve", "--manifest", "./models/manifest_gen.json", "--stale-flag"},
				sentinelManaged:          true,
				sentinelManifestPath:     "./models/manifest_gen.json",
				sentinelGeneratorVersion: "0.41.0",
			},
		},
	}
	seedJSON(t, root, existing)

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	// Key preserved ("my-models"), content refreshed, no second entry added.
	if diff := cmp.Diff(map[string]any{"my-models": wantEntry()}, got); diff != "" {
		t.Errorf("Merge() in-place update mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeUnmanagedCollisionPicksFreeKeyAndWarns(t *testing.T) {
	root, opts := mergeDir(t)
	existing := map[string]any{
		"mcpServers": map[string]any{
			"sqlgen": map[string]any{"command": "my-own-wrapper", "args": []any{}},
		},
	}
	seedJSON(t, root, existing)

	warnings, err := Merge(opts)
	if err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `unmanaged "sqlgen"`) || !strings.Contains(warnings[0], `"sqlgen-2"`) {
		t.Errorf("Merge() warnings = %v, want one unmanaged-collision warning naming sqlgen-2", warnings)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	want := map[string]any{
		"sqlgen":   map[string]any{"command": "my-own-wrapper", "args": []any{}},
		"sqlgen-2": wantEntry(),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Merge() collision handling mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeInvalidJSONFailsWithoutOverwrite(t *testing.T) {
	root, opts := mergeDir(t)
	invalid := "{\n  \"mcpServers\": {\n"
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(invalid), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}

	_, err := Merge(opts)
	if err == nil {
		t.Fatal("Merge() on invalid JSON: expected error, got nil")
	}
	if !strings.Contains(err.Error(), ".mcp.json:") || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("Merge() error = %q, want line/col reference and refusing-to-overwrite text", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // test helper, path is controlled
	if readErr != nil {
		t.Fatalf("re-reading .mcp.json: %v", readErr)
	}
	if string(raw) != invalid {
		t.Errorf("Merge() modified the invalid file: %q", raw)
	}
}

func TestMergeNonObjectServersFails(t *testing.T) {
	root, opts := mergeDir(t)
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": []}`), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}
	if _, err := Merge(opts); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Errorf("Merge() error = %v, want mcpServers-not-an-object error", err)
	}
}

func TestMergeMonorepoSiblingsCoexist(t *testing.T) {
	root, opts := mergeDir(t)
	// A sibling package's manifest exists on disk → its entry must survive.
	writeManifestFile(t, root, "analytics/manifest_gen.json")
	sibling := map[string]any{
		"command":                "sqlgen",
		"args":                   []any{"mcp", "serve", "--manifest", "./analytics/manifest_gen.json"},
		sentinelManaged:          true,
		sentinelManifestPath:     "./analytics/manifest_gen.json",
		sentinelGeneratorVersion: "0.42.0",
	}
	seedJSON(t, root, map[string]any{
		"mcpServers": map[string]any{"sqlgen-analytics": sibling},
	})

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	want := map[string]any{
		"sqlgen-analytics": sibling,
		"sqlgen":           wantEntry(),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Merge() monorepo coexistence mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeManagedKeyOccupiedByLiveSiblingPicksFreeKey(t *testing.T) {
	root, opts := mergeDir(t)
	// The default key is held by a *managed* entry serving a different, live
	// manifest (a monorepo sibling that also used the default key). It must
	// not be overwritten; we take the next free key without the unmanaged
	// warning.
	writeManifestFile(t, root, "analytics/manifest_gen.json")
	sibling := map[string]any{
		"command":                "sqlgen",
		"args":                   []any{"mcp", "serve", "--manifest", "./analytics/manifest_gen.json"},
		sentinelManaged:          true,
		sentinelManifestPath:     "./analytics/manifest_gen.json",
		sentinelGeneratorVersion: "0.42.0",
	}
	seedJSON(t, root, map[string]any{
		"mcpServers": map[string]any{"sqlgen": sibling},
	})

	warnings, err := Merge(opts)
	if err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Merge() warnings = %v, want none for a managed sibling", warnings)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	want := map[string]any{
		"sqlgen":   sibling,
		"sqlgen-2": wantEntry(),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Merge() managed-sibling collision mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeStaleEntryCleanupOnOutputDirMove(t *testing.T) {
	root, opts := mergeDir(t)
	// Old entry points at a manifest path that no longer exists (output.dir
	// moved). It is swept; the new entry lands under the (now free) key.
	stale := map[string]any{
		"command":                "sqlgen",
		"args":                   []any{"mcp", "serve", "--manifest", "./old-models/manifest_gen.json"},
		sentinelManaged:          true,
		sentinelManifestPath:     "./old-models/manifest_gen.json",
		sentinelGeneratorVersion: "0.41.0",
	}
	seedJSON(t, root, map[string]any{
		"mcpServers": map[string]any{"sqlgen": stale},
	})

	warnings, err := Merge(opts)
	if err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Merge() warnings = %v, want none", warnings)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	if diff := cmp.Diff(map[string]any{"sqlgen": wantEntry()}, got); diff != "" {
		t.Errorf("Merge() stale cleanup mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeRemoveDeletesEntryAndEmptyFile(t *testing.T) {
	root, opts := mergeDir(t)
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge(upsert) unexpected error: %v", err)
	}

	opts.Action = MergeRemove
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge(remove) unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("Merge(remove) left .mcp.json behind (stat err = %v), want file deleted", err)
	}
}

func TestMergeRemovePreservesOtherEntries(t *testing.T) {
	root, opts := mergeDir(t)
	seedJSON(t, root, map[string]any{
		"mcpServers": map[string]any{
			"linear": map[string]any{"command": "linear-mcp", "args": []any{}},
			"sqlgen": wantEntry(),
		},
	})

	opts.Action = MergeRemove
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge(remove) unexpected error: %v", err)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	want := map[string]any{"linear": map[string]any{"command": "linear-mcp", "args": []any{}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Merge(remove) mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeRemoveNoFileIsNoOp(t *testing.T) {
	_, opts := mergeDir(t)
	opts.Action = MergeRemove
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge(remove) with no file: unexpected error: %v", err)
	}
}

func TestMergeRemoveNoMatchLeavesFileUntouched(t *testing.T) {
	root, opts := mergeDir(t)
	// Consumer-authored file with custom formatting and no sqlgen entries: a
	// removal run must not rewrite (canonicalize) it.
	original := "{\n      \"mcpServers\": {\"linear\": {\"command\": \"linear-mcp\"}}\n}\n"
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(original), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}

	opts.Action = MergeRemove
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge(remove) unexpected error: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("re-reading .mcp.json: %v", err)
	}
	if string(raw) != original {
		t.Errorf("Merge(remove) rewrote an untouched file:\n got: %q\nwant: %q", raw, original)
	}
}

func TestMergeCommandTemplateRendering(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		wantCommand string
		wantArgs    []any
		wantErr     string
	}{
		{
			name:        "direnv wrapper with substitution",
			template:    "direnv exec . sqlgen mcp serve --manifest {{ manifest_path }}",
			wantCommand: "direnv",
			wantArgs:    []any{"exec", ".", "sqlgen", "mcp", "serve", "--manifest", "./models/manifest_gen.json"},
		},
		{
			name:        "quoted token with spaces stays one arg",
			template:    `wrapper --label "my models" sqlgen mcp serve --manifest {{ manifest_path }}`,
			wantCommand: "wrapper",
			wantArgs:    []any{"--label", "my models", "sqlgen", "mcp", "serve", "--manifest", "./models/manifest_gen.json"},
		},
		{
			name:     "unbalanced quote is an error",
			template: `wrapper "unclosed sqlgen mcp serve`,
			wantErr:  "unbalanced",
		},
		{
			name:     "whitespace-only render is an error",
			template: "   ",
			wantErr:  "empty command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, opts := mergeDir(t)
			opts.CommandTemplate = tt.template

			_, err := Merge(opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Merge() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Merge() unexpected error: %v", err)
			}
			got := servers(t, readConfig(t, root, ".mcp.json"))["sqlgen"].(map[string]any)
			if got["command"] != tt.wantCommand {
				t.Errorf("Merge() command = %v, want %q", got["command"], tt.wantCommand)
			}
			if diff := cmp.Diff(tt.wantArgs, got["args"]); diff != "" {
				t.Errorf("Merge() args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMergeCanonicalFormatIsIdempotent(t *testing.T) {
	root, opts := mergeDir(t)
	// Consumer file with custom indentation and unsorted keys.
	messy := "{\"zeta\": 1,\n        \"mcpServers\": {\n\t\"linear\": {\"command\": \"linear-mcp\"}\n},\n\"alpha\": {\"b\": 2, \"a\": 1}}"
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(messy), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() first run: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading after first run: %v", err)
	}
	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() second run: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading after second run: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("Merge() not byte-idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
	// Canonical shape: sorted top-level keys, 2-space indent, no tabs.
	text := string(first)
	if strings.Contains(text, "\t") {
		t.Errorf("Merge() output contains tabs:\n%s", text)
	}
	if strings.Index(text, `"alpha"`) > strings.Index(text, `"mcpServers"`) {
		t.Errorf("Merge() output keys not alphabetically sorted:\n%s", text)
	}
}

func TestMergeReadOnlyDirFails(t *testing.T) {
	root, opts := mergeDir(t)
	roDir := filepath.Join(root, "readonly")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("mkdir readonly: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) }) //nolint:gosec // restore owner perms so TempDir cleanup can delete
	opts.ConfigPath = "readonly/.mcp.json"

	_, err := Merge(opts)
	if err == nil {
		t.Fatal("Merge() into read-only dir: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "readonly/.mcp.json") {
		t.Errorf("Merge() error = %q, want it to name the target path", err)
	}
}

func TestMergeLeftoverTmpFileIsReplaced(t *testing.T) {
	root, opts := mergeDir(t)
	// Simulate a crash mid-write from a previous run: the tmp file exists,
	// the target was never touched. The next merge must succeed and leave no
	// tmp file behind.
	if err := os.WriteFile(filepath.Join(root, ".mcp.json.tmp"), []byte("partial garbage"), 0o600); err != nil {
		t.Fatalf("seeding tmp file: %v", err)
	}

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	got := servers(t, readConfig(t, root, ".mcp.json"))
	if diff := cmp.Diff(map[string]any{"sqlgen": wantEntry()}, got); diff != "" {
		t.Errorf("Merge() after leftover tmp mismatch (-want +got):\n%s", diff)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("Merge() left .mcp.json.tmp behind (stat err = %v)", err)
	}
}

func TestMergeCreatesNestedTargetDirectory(t *testing.T) {
	root, opts := mergeDir(t)
	opts.ConfigPath = ".cursor/mcp.json"

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	got := servers(t, readConfig(t, root, ".cursor/mcp.json"))
	if diff := cmp.Diff(map[string]any{"sqlgen": wantEntry()}, got); diff != "" {
		t.Errorf("Merge() nested target mismatch (-want +got):\n%s", diff)
	}
}

func TestMergeAbsoluteManifestPathStoredVerbatim(t *testing.T) {
	root, opts := mergeDir(t)
	abs := filepath.Join(root, "models", "manifest_gen.json")
	opts.ManifestPath = abs

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	entry := servers(t, readConfig(t, root, ".mcp.json"))["sqlgen"].(map[string]any)
	if entry[sentinelManifestPath] != abs {
		t.Errorf("Merge() stored manifest path = %v, want absolute %q verbatim", entry[sentinelManifestPath], abs)
	}
}

func TestTokenizeCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "plain tokens", input: "a b c", want: []string{"a", "b", "c"}},
		{name: "collapsed whitespace", input: "  a\t b  ", want: []string{"a", "b"}},
		{name: "double quotes group", input: `a "b c" d`, want: []string{"a", "b c", "d"}},
		{name: "single quotes group", input: "a 'b c' d", want: []string{"a", "b c", "d"}},
		{name: "empty quoted token survives", input: `a "" b`, want: []string{"a", "", "b"}},
		{name: "adjacent quote merges into token", input: `--label="x y"`, want: []string{"--label=x y"}},
		{name: "empty input", input: "", want: nil},
		{name: "unbalanced double quote", input: `a "b`, wantErr: true},
		{name: "unbalanced single quote", input: "a 'b", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokenizeCommand(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("tokenizeCommand(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tokenizeCommand(%q) unexpected error: %v", tt.input, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tokenizeCommand(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}

func TestOffsetLineCol(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		offset   int64
		wantLine int
		wantCol  int
	}{
		{name: "first line", raw: "abc", offset: 2, wantLine: 1, wantCol: 3},
		{name: "second line", raw: "ab\ncd", offset: 4, wantLine: 2, wantCol: 2},
		{name: "offset past end clamps", raw: "ab", offset: 99, wantLine: 1, wantCol: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := offsetLineCol([]byte(tt.raw), tt.offset)
			if line != tt.wantLine || col != tt.wantCol {
				t.Errorf("offsetLineCol(%q, %d) = %d:%d, want %d:%d", tt.raw, tt.offset, line, col, tt.wantLine, tt.wantCol)
			}
		})
	}
}

// seedJSON writes a JSON tree to the module root's .mcp.json.
func seedJSON(t *testing.T, root string, tree map[string]any) {
	t.Helper()
	raw, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshaling seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), raw, 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}
}

func TestMergePreservesSiblingNumericForm(t *testing.T) {
	root, opts := mergeDir(t)
	// A sibling entry carrying numbers that a float64 round-trip would
	// rewrite: an integer beyond 2^53 and a plain decimal. §3.4 promises
	// other tools' entries are never touched.
	existing := `{
  "mcpServers": {
    "custom": {
      "command": "custom-mcp",
      "maxTokens": 9007199254740993,
      "temperature": 0.30
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding .mcp.json: %v", err)
	}

	if _, err := Merge(opts); err != nil {
		t.Fatalf("Merge() unexpected error: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json")) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("re-reading .mcp.json: %v", err)
	}
	for _, want := range []string{"9007199254740993", "0.30"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("Merge() rewrote sibling numeric value: output lacks %q:\n%s", want, raw)
		}
	}
}
