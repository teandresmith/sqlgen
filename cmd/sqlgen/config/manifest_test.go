package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// withManifest returns a valid config with a manifest block attached. The
// block is populated as-is (no defaults applied), matching the shape a
// consumer would supply via YAML before LoadConfig fills in unset fields.
func withManifest(m *config.ManifestConfig) *config.RootConfig {
	cfg := validConfig()
	cfg.Generation.Manifest = m
	return cfg
}

// TestValidatePreParse_Manifest_FormatsEmpty covers PRD §4.13:
// "manifest.formats: [] with enabled: true → error".
func TestValidatePreParse_Manifest_FormatsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		formats []string
		wantErr bool
	}{
		{name: "empty formats with enabled is error", formats: []string{}, wantErr: true},
		{name: "json+markdown is valid", formats: []string{"json", "markdown"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:    new(true),
				Formats:    tt.formats,
				JSONLayout: config.JSONLayoutSingle,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.formats") {
					t.Errorf("error %q does not mention manifest.formats", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_UnknownFormat covers PRD §4.13:
// "Unknown manifest.formats value → error".
func TestValidatePreParse_Manifest_UnknownFormat(t *testing.T) {
	tests := []struct {
		name    string
		formats []string
		wantErr bool
	}{
		{name: "unknown 'xml' rejected", formats: []string{"xml"}, wantErr: true},
		{name: "json-only valid", formats: []string{"json"}, wantErr: false},
		// markdown-only excludes json, so embed_in_client must be off to stay
		// valid — this case exercises format-name validation, not the
		// embed/json interaction.
		{name: "markdown-only valid", formats: []string{"markdown"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:       new(true),
				Formats:       tt.formats,
				JSONLayout:    config.JSONLayoutSingle,
				EmbedInClient: new(false),
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.formats") {
					t.Errorf("error %q does not mention manifest.formats", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_EmbedRequiresJSON covers PRD §30.2 /
// §30.6 / §4.13: embed_in_client embeds the on-disk JSON manifest onto the
// Client, so json must be an emitted format. A config that turns embed on
// (explicitly or by default) while excluding json from formats is rejected,
// because it would otherwise render a manifest_embed_gen.go whose //go:embed
// targets an unwritten JSON file.
func TestValidatePreParse_Manifest_EmbedRequiresJSON(t *testing.T) {
	tests := []struct {
		name    string
		formats []string
		embed   *bool
		wantErr bool
	}{
		{name: "markdown-only with embed on (explicit) is error", formats: []string{"markdown"}, embed: new(true), wantErr: true},
		{name: "markdown-only with embed defaulted (nil→true) is error", formats: []string{"markdown"}, embed: nil, wantErr: true},
		{name: "markdown-only with embed off is valid", formats: []string{"markdown"}, embed: new(false), wantErr: false},
		{name: "json+markdown with embed on is valid", formats: []string{"json", "markdown"}, embed: new(true), wantErr: false},
		{name: "json-only with embed on is valid", formats: []string{"json"}, embed: new(true), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:       new(true),
				Formats:       tt.formats,
				JSONLayout:    config.JSONLayoutSingle,
				EmbedInClient: tt.embed,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.embed_in_client") {
					t.Errorf("error %q does not mention manifest.embed_in_client", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_JSONLayoutEnum covers PRD §4.13:
// "manifest.json_layout not in single/per_entity → error".
func TestValidatePreParse_Manifest_JSONLayoutEnum(t *testing.T) {
	tests := []struct {
		name    string
		layout  config.JSONLayout
		wantErr bool
	}{
		{name: "single is valid", layout: config.JSONLayoutSingle, wantErr: false},
		{name: "per_entity is valid", layout: config.JSONLayoutPerEntity, wantErr: false},
		{name: "unknown rejected", layout: config.JSONLayout("split"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:    new(true),
				Formats:    []string{"json", "markdown"},
				JSONLayout: tt.layout,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.json_layout") {
					t.Errorf("error %q does not mention manifest.json_layout", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_MarkdownDirPlacement covers PRD §4.13:
// "manifest.markdown_dir placement conflict → error" — three sub-rules.
func TestValidatePreParse_Manifest_MarkdownDirPlacement(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{name: "manifest is valid", dir: "manifest", wantErr: false},
		{name: "docs/manifest is valid", dir: "docs/manifest", wantErr: false},
		{name: "dot rejected", dir: ".", wantErr: true},
		{name: "underscore-prefixed rejected", dir: "_internal", wantErr: true},
		{name: "graph collides with PRD §8.3 artifact subdir", dir: "graph", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:     new(true),
				Formats:     []string{"json", "markdown"},
				JSONLayout:  config.JSONLayoutSingle,
				MarkdownDir: tt.dir,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.markdown_dir") {
					t.Errorf("error %q does not mention manifest.markdown_dir", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_JSONPerEntityDirPlacement covers PRD §4.13:
// "manifest.json_per_entity_dir placement conflict → error".
func TestValidatePreParse_Manifest_JSONPerEntityDirPlacement(t *testing.T) {
	tests := []struct {
		name       string
		entityDir  string
		mdDir      string
		wantErr    bool
		wantString string
	}{
		{name: "entities is valid", entityDir: "entities", mdDir: "manifest", wantErr: false},
		{name: "dot rejected", entityDir: ".", mdDir: "manifest", wantErr: true, wantString: "manifest.json_per_entity_dir"},
		{name: "underscore-prefixed rejected", entityDir: "_entities", mdDir: "manifest", wantErr: true, wantString: "manifest.json_per_entity_dir"},
		{name: "equal to markdown_dir rejected", entityDir: "manifest", mdDir: "manifest", wantErr: true, wantString: "manifest.json_per_entity_dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:          new(true),
				Formats:          []string{"json", "markdown"},
				JSONLayout:       config.JSONLayoutSingle,
				MarkdownDir:      tt.mdDir,
				JSONPerEntityDir: tt.entityDir,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantString) {
					t.Errorf("error %q does not mention %q", err.Error(), tt.wantString)
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_PerTableOptInForbidden covers PRD §4.13:
// "tables.<name>.manifest.enabled: true with global manifest.enabled: false → error".
// Per-table is opt-out only.
//
// The "global block omitted entirely" subform exercises the same validator
// branch as "global enabled: false" (both resolve to enabled=false in
// validateManifestConfig), but it's pinned explicitly so a future refactor
// of how the validator distinguishes nil-global from false-global can't
// silently regress the rule for consumers who set per-table flags without
// declaring the global block.
func TestValidatePreParse_Manifest_PerTableOptInForbidden(t *testing.T) {
	tests := []struct {
		name         string
		globalMfst   *config.ManifestConfig // nil = omit the global block entirely
		tableEnabled *bool
		wantErr      bool
	}{
		{
			name:         "table opt-out with global on is valid",
			globalMfst:   &config.ManifestConfig{Enabled: new(true), Formats: []string{"json", "markdown"}, JSONLayout: config.JSONLayoutSingle},
			tableEnabled: new(false),
			wantErr:      false,
		},
		{
			name:         "table opt-in with global on is valid (no-op)",
			globalMfst:   &config.ManifestConfig{Enabled: new(true), Formats: []string{"json", "markdown"}, JSONLayout: config.JSONLayoutSingle},
			tableEnabled: new(true),
			wantErr:      false,
		},
		{
			name:         "table opt-in with global off is error",
			globalMfst:   &config.ManifestConfig{Enabled: new(false)},
			tableEnabled: new(true),
			wantErr:      true,
		},
		{
			name:         "table unset with global off is valid",
			globalMfst:   &config.ManifestConfig{Enabled: new(false)},
			tableEnabled: nil,
			wantErr:      false,
		},
		{
			name:         "table opt-in with global block omitted is error",
			globalMfst:   nil,
			tableEnabled: new(true),
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(tt.globalMfst)
			cfg.Tables["users"] = config.TableConfig{
				Manifest: &config.TableManifestConfig{Enabled: tt.tableEnabled},
			}
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "tables.users.manifest.enabled") {
					t.Errorf("error %q does not mention tables.users.manifest.enabled", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_BreadcrumbsRequireEnabled covers PRD §4.13:
// "manifest.breadcrumbs.*: true with manifest.enabled: false → error".
func TestValidatePreParse_Manifest_BreadcrumbsRequireEnabled(t *testing.T) {
	tests := []struct {
		name        string
		breadcrumbs config.BreadcrumbsConfig
		wantErr     bool
		wantString  string
	}{
		{name: "all breadcrumbs unset is valid", breadcrumbs: config.BreadcrumbsConfig{}, wantErr: false},
		{name: "claude_md true is error", breadcrumbs: config.BreadcrumbsConfig{ClaudeMD: new(true)}, wantErr: true, wantString: "manifest.breadcrumbs.claude_md"},
		{name: "agents_md true is error", breadcrumbs: config.BreadcrumbsConfig{AgentsMD: new(true)}, wantErr: true, wantString: "manifest.breadcrumbs.agents_md"},
		{name: "package_doc true is error", breadcrumbs: config.BreadcrumbsConfig{PackageDoc: new(true)}, wantErr: true, wantString: "manifest.breadcrumbs.package_doc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:     new(false),
				Breadcrumbs: tt.breadcrumbs,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantString) {
					t.Errorf("error %q does not mention %q", err.Error(), tt.wantString)
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestValidatePreParse_Manifest_EmbedRequiresEnabled covers PRD §4.13:
// "manifest.embed_in_client: true with manifest.enabled: false → error".
func TestValidatePreParse_Manifest_EmbedRequiresEnabled(t *testing.T) {
	tests := []struct {
		name    string
		embed   *bool
		wantErr bool
	}{
		{name: "embed unset is valid", embed: nil, wantErr: false},
		{name: "embed false is valid", embed: new(false), wantErr: false},
		{name: "embed true is error", embed: new(true), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:       new(false),
				EmbedInClient: tt.embed,
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidatePreParse: expected error, got nil")
				}
				if !strings.Contains(err.Error(), "manifest.embed_in_client") {
					t.Errorf("error %q does not mention manifest.embed_in_client", err.Error())
				}
				if !strings.Contains(err.Error(), "30.2") {
					t.Errorf("error %q does not cite PRD §30.2", err.Error())
				}
			} else if err != nil {
				t.Fatalf("ValidatePreParse: unexpected error: %v", err)
			}
		})
	}
}

// TestLoadConfig_ManifestDefaultsApplied verifies that when the YAML sets
// `manifest: { enabled: true }` alone, applyDefaults fills the unset fields
// to the PRD §30.2 defaults: formats, json_layout, markdown_dir, breadcrumbs
// sub-flags, embed_in_client, include_examples, include_internal.
func TestLoadConfig_ManifestDefaultsApplied(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
generation:
  manifest:
    enabled: true
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	m := cfg.Generation.Manifest
	if m == nil {
		t.Fatalf("Generation.Manifest is nil; expected populated")
	}
	if m.Enabled == nil || !*m.Enabled {
		t.Errorf("Enabled = %v, want true", m.Enabled)
	}
	if got, want := m.Formats, []string{"json", "markdown"}; !slices.Equal(got, want) {
		t.Errorf("Formats = %v, want %v", got, want)
	}
	if m.JSONLayout != config.JSONLayoutSingle {
		t.Errorf("JSONLayout = %q, want %q", m.JSONLayout, config.JSONLayoutSingle)
	}
	if m.MarkdownDir != "manifest" {
		t.Errorf("MarkdownDir = %q, want %q", m.MarkdownDir, "manifest")
	}
	if m.JSONFilename != "manifest_gen.json" {
		t.Errorf("JSONFilename = %q, want %q", m.JSONFilename, "manifest_gen.json")
	}
	if m.JSONPerEntityDir != "entities" {
		t.Errorf("JSONPerEntityDir = %q, want %q", m.JSONPerEntityDir, "entities")
	}
	if m.IncludeExamples == nil || !*m.IncludeExamples {
		t.Errorf("IncludeExamples = %v, want true", m.IncludeExamples)
	}
	if m.IncludeInternal == nil || *m.IncludeInternal {
		t.Errorf("IncludeInternal = %v, want false", m.IncludeInternal)
	}
	if m.EmbedInClient == nil || !*m.EmbedInClient {
		t.Errorf("EmbedInClient = %v, want true", m.EmbedInClient)
	}
	if m.Breadcrumbs.ClaudeMD == nil || !*m.Breadcrumbs.ClaudeMD {
		t.Errorf("Breadcrumbs.ClaudeMD = %v, want true", m.Breadcrumbs.ClaudeMD)
	}
	if m.Breadcrumbs.AgentsMD == nil || !*m.Breadcrumbs.AgentsMD {
		t.Errorf("Breadcrumbs.AgentsMD = %v, want true", m.Breadcrumbs.AgentsMD)
	}
	if m.Breadcrumbs.PackageDoc == nil || !*m.Breadcrumbs.PackageDoc {
		t.Errorf("Breadcrumbs.PackageDoc = %v, want true", m.Breadcrumbs.PackageDoc)
	}
}

// TestLoadConfig_ManifestOmitted verifies that omitting the manifest block
// leaves cfg.Generation.Manifest == nil after defaults — no manifest
// artifacts should be produced downstream.
func TestLoadConfig_ManifestOmitted(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if cfg.Generation.Manifest != nil {
		t.Errorf("Generation.Manifest = %+v, want nil", cfg.Generation.Manifest)
	}
}

// TestResolveTableManifestEnabled covers the per-table resolution order from
// PRD §30.2: table-level → global → false.
func TestResolveTableManifestEnabled(t *testing.T) {
	tests := []struct {
		name   string
		global *config.ManifestConfig
		table  *config.TableManifestConfig
		want   bool
	}{
		{name: "no global, no table → false", global: nil, table: nil, want: false},
		{name: "global enabled, table unset → true", global: &config.ManifestConfig{Enabled: new(true)}, table: nil, want: true},
		{name: "global enabled, table opt-out → false", global: &config.ManifestConfig{Enabled: new(true)}, table: &config.TableManifestConfig{Enabled: new(false)}, want: false},
		{name: "global disabled, table unset → false", global: &config.ManifestConfig{Enabled: new(false)}, table: nil, want: false},
		{name: "global enabled, table explicit true → true", global: &config.ManifestConfig{Enabled: new(true)}, table: &config.TableManifestConfig{Enabled: new(true)}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := config.GenerationConfig{Manifest: tt.global}
			tbl := config.TableConfig{Manifest: tt.table}
			got := config.ResolveTableManifestEnabled(tbl, gen)
			if got != tt.want {
				t.Errorf("ResolveTableManifestEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoadConfig_ManifestPerTableOptOut covers the per-table opt-out from
// PRD §30.2 end-to-end through LoadConfig: global enabled, one table opted
// out — resolution flags the opted-out table only.
func TestLoadConfig_ManifestPerTableOptOut(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
generation:
  manifest:
    enabled: true
tables:
  users:
    manifest:
      enabled: false
  posts:
    description: posts table
`)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	users := cfg.Tables["users"]
	posts := cfg.Tables["posts"]
	if config.ResolveTableManifestEnabled(users, cfg.Generation) {
		t.Errorf("users: expected manifest disabled, got enabled")
	}
	if !config.ResolveTableManifestEnabled(posts, cfg.Generation) {
		t.Errorf("posts: expected manifest enabled, got disabled")
	}
}

// TestLoadConfig_ManifestMCPDefaults verifies the MCP.md §3.4 defaults fill
// in when manifest.enabled is true and the mcp block is omitted or partial.
func TestLoadConfig_ManifestMCPDefaults(t *testing.T) {
	tests := []struct {
		name         string
		yamlBlock    string
		wantEmit     bool
		wantConfigs  []string
		wantKey      string
		wantTemplate string
	}{
		{
			name:        "omitted mcp block gets full defaults",
			yamlBlock:   "",
			wantEmit:    true,
			wantConfigs: []string{".mcp.json"},
			wantKey:     "sqlgen",
		},
		{
			name: "partial mcp block keeps explicit values",
			yamlBlock: `
    mcp:
      emit_project_config: false
      project_configs: [.mcp.json, .cursor/mcp.json]
      server_key: sqlgen-models
      command_template: "direnv exec . sqlgen mcp serve --manifest {{ manifest_path }}"
`,
			wantEmit:     false,
			wantConfigs:  []string{".mcp.json", ".cursor/mcp.json"},
			wantKey:      "sqlgen-models",
			wantTemplate: "direnv exec . sqlgen mcp serve --manifest {{ manifest_path }}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
generation:
  manifest:
    enabled: true`+tt.yamlBlock)

			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			mcp := cfg.Generation.Manifest.MCP
			if mcp == nil {
				t.Fatalf("Generation.Manifest.MCP is nil; expected populated")
			}
			if mcp.EmitProjectConfig == nil || *mcp.EmitProjectConfig != tt.wantEmit {
				t.Errorf("EmitProjectConfig = %v, want %v", mcp.EmitProjectConfig, tt.wantEmit)
			}
			if !slices.Equal(mcp.ProjectConfigs, tt.wantConfigs) {
				t.Errorf("ProjectConfigs = %v, want %v", mcp.ProjectConfigs, tt.wantConfigs)
			}
			if mcp.ServerKey != tt.wantKey {
				t.Errorf("ServerKey = %q, want %q", mcp.ServerKey, tt.wantKey)
			}
			if mcp.CommandTemplate != tt.wantTemplate {
				t.Errorf("CommandTemplate = %q, want %q", mcp.CommandTemplate, tt.wantTemplate)
			}
		})
	}
}

// TestValidatePreParse_Manifest_MCPEmitRequiresEnabled covers MCP.md §3.4:
// mcp.emit_project_config: true with manifest.enabled: false is an error.
func TestValidatePreParse_Manifest_MCPEmitRequiresEnabled(t *testing.T) {
	cfg := withManifest(&config.ManifestConfig{
		Enabled: new(false),
		MCP:     &config.MCPConfig{EmitProjectConfig: new(true)},
	})
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "manifest.mcp.emit_project_config") {
		t.Errorf("error %q does not mention manifest.mcp.emit_project_config", err.Error())
	}
}

// TestValidatePreParse_Manifest_MCPProjectConfigPlacement covers MCP.md §3.4:
// project_configs entries must be module-root-relative and must not escape
// the module root.
func TestValidatePreParse_Manifest_MCPProjectConfigPlacement(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "plain relative path is valid", target: ".mcp.json"},
		{name: "nested relative path is valid", target: ".cursor/mcp.json"},
		{name: "absolute path rejected", target: "/etc/mcp.json", wantErr: "not absolute"},
		{name: "leading dotdot rejected", target: "../mcp.json", wantErr: "escape the module root"},
		{name: "interior dotdot rejected", target: "a/../mcp.json", wantErr: "escape the module root"},
		{name: "empty entry rejected", target: "", wantErr: "cannot be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withManifest(&config.ManifestConfig{
				Enabled:    new(true),
				Formats:    []string{"json"},
				JSONLayout: config.JSONLayoutSingle,
				MCP: &config.MCPConfig{
					EmitProjectConfig: new(true),
					ProjectConfigs:    []string{tt.target},
					ServerKey:         "sqlgen",
				},
			})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePreParse(%q) unexpected error: %v", tt.target, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse(%q) expected error, got nil", tt.target)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse(%q) error = %q, want containing %q", tt.target, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidatePreParse_Manifest_MCPPlacementEnforcedWhenDisabled verifies the
// project_configs escape rule also holds with manifest.enabled: false — the
// removal path walks the same targets.
func TestValidatePreParse_Manifest_MCPPlacementEnforcedWhenDisabled(t *testing.T) {
	cfg := withManifest(&config.ManifestConfig{
		Enabled: new(false),
		MCP: &config.MCPConfig{
			ProjectConfigs: []string{"../escape/mcp.json"},
		},
	})
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "escape the module root") {
		t.Errorf("error %q does not mention module-root escape", err.Error())
	}
}
