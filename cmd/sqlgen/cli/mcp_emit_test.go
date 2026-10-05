package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// mcpEmitConfig builds a manifest-enabled config with the given MCP block,
// mirroring post-defaults state (LoadConfig fills these in production).
func mcpEmitConfig(mcpBlock *config.MCPConfig) *config.RootConfig {
	return &config.RootConfig{
		Output: config.OutputConfig{Dir: "models"},
		Generation: config.GenerationConfig{
			Manifest: &config.ManifestConfig{
				Enabled:      new(true),
				JSONFilename: "manifest_gen.json",
				MCP:          mcpBlock,
			},
		},
	}
}

// chdirWithManifest moves the test into a temp module root containing a live
// manifest file at models/manifest_gen.json.
func chdirWithManifest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models", "manifest"), 0o750); err != nil {
		t.Fatalf("mkdir models/manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "models", "manifest", "manifest_gen.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("writing manifest fixture: %v", err)
	}
	t.Chdir(root)
	return root
}

func readServerKeys(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var parsed struct {
		Servers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	keys := make([]string, 0, len(parsed.Servers))
	for k := range parsed.Servers {
		keys = append(keys, k)
	}
	return keys
}

func TestEmitMCPProjectConfigsMultiTarget(t *testing.T) {
	root := chdirWithManifest(t)
	cfg := mcpEmitConfig(&config.MCPConfig{
		EmitProjectConfig: new(true),
		ProjectConfigs:    []string{".mcp.json", ".cursor/mcp.json"},
		ServerKey:         "sqlgen",
	})

	warnings, err := emitMCPProjectConfigs(cfg, "0.42.0")
	if err != nil {
		t.Fatalf("emitMCPProjectConfigs() unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("emitMCPProjectConfigs() warnings = %v, want none", warnings)
	}
	for _, target := range []string{".mcp.json", ".cursor/mcp.json"} {
		keys := readServerKeys(t, filepath.Join(root, target))
		if len(keys) != 1 || keys[0] != "sqlgen" {
			t.Errorf("target %s server keys = %v, want [sqlgen]", target, keys)
		}
	}
}

func TestEmitMCPProjectConfigsFailureDoesNotBlockOtherTargets(t *testing.T) {
	root := chdirWithManifest(t)
	// First target sits in a read-only directory → fails; the second target
	// must still be written, and the aggregate error must name the failure.
	roDir := filepath.Join(root, "readonly")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("mkdir readonly: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) }) //nolint:gosec // restore owner perms so TempDir cleanup can delete
	cfg := mcpEmitConfig(&config.MCPConfig{
		EmitProjectConfig: new(true),
		ProjectConfigs:    []string{"readonly/mcp.json", ".mcp.json"},
		ServerKey:         "sqlgen",
	})

	_, err := emitMCPProjectConfigs(cfg, "0.42.0")
	if err == nil {
		t.Fatal("emitMCPProjectConfigs() expected error for read-only target, got nil")
	}
	if !strings.Contains(err.Error(), "readonly/mcp.json") {
		t.Errorf("emitMCPProjectConfigs() error = %q, want it to name the failed target", err)
	}
	keys := readServerKeys(t, filepath.Join(root, ".mcp.json"))
	if len(keys) != 1 || keys[0] != "sqlgen" {
		t.Errorf(".mcp.json server keys = %v, want [sqlgen] despite the sibling failure", keys)
	}
}

func TestEmitMCPProjectConfigsOptOutRemoves(t *testing.T) {
	root := chdirWithManifest(t)
	cfg := mcpEmitConfig(&config.MCPConfig{
		EmitProjectConfig: new(true),
		ProjectConfigs:    []string{".mcp.json"},
		ServerKey:         "sqlgen",
	})
	if _, err := emitMCPProjectConfigs(cfg, "0.42.0"); err != nil {
		t.Fatalf("emitMCPProjectConfigs(emit) unexpected error: %v", err)
	}

	cfg.Generation.Manifest.MCP.EmitProjectConfig = new(false)
	if _, err := emitMCPProjectConfigs(cfg, "0.42.0"); err != nil {
		t.Fatalf("emitMCPProjectConfigs(opt-out) unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf(".mcp.json still present after opt-out (stat err = %v), want deleted", err)
	}
}

func TestEmitMCPProjectConfigsManifestDisabledRemovesWithDefaults(t *testing.T) {
	root := chdirWithManifest(t)
	// Emit first, then disable the whole manifest block (nil — as when the
	// consumer deletes it from YAML). The removal path must still resolve
	// the default target/key and clean up.
	cfg := mcpEmitConfig(&config.MCPConfig{
		EmitProjectConfig: new(true),
		ProjectConfigs:    []string{".mcp.json"},
		ServerKey:         "sqlgen",
	})
	if _, err := emitMCPProjectConfigs(cfg, "0.42.0"); err != nil {
		t.Fatalf("emitMCPProjectConfigs(emit) unexpected error: %v", err)
	}

	cfg.Generation.Manifest = nil
	if _, err := emitMCPProjectConfigs(cfg, "0.42.0"); err != nil {
		t.Fatalf("emitMCPProjectConfigs(manifest nil) unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf(".mcp.json still present after manifest removal (stat err = %v), want deleted", err)
	}
}

func TestManifestIdentityPathHonorsImportOverride(t *testing.T) {
	m := &config.ManifestConfig{JSONFilename: "manifest_gen.json"}

	if got, want := manifestIdentityPath(m, "models"), filepath.Join("models", "manifest", "manifest_gen.json"); got != want {
		t.Errorf("manifestIdentityPath() = %q, want %q", got, want)
	}

	custom := &config.ManifestConfig{MarkdownDir: "meta", JSONFilename: "m.json"}
	if got, want := manifestIdentityPath(custom, "models"), filepath.Join("models", "meta", "m.json"); got != want {
		t.Errorf("manifestIdentityPath(custom dirs) = %q, want %q", got, want)
	}

	t.Setenv("SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE", "stable-models")
	if got, want := manifestIdentityPath(m, "/tmp/redirected"), filepath.Join("stable-models", "manifest", "manifest_gen.json"); got != want {
		t.Errorf("manifestIdentityPath() with override = %q, want %q", got, want)
	}
}
