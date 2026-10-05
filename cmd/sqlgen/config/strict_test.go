package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// Strict config loading. LoadConfig used plain yaml.Unmarshal, so an
// unrecognized key was dropped without a word. Writing `output.path` instead of
// `output.dir` left Output.Dir empty, which defaults to "." — silently
// retargeting every generated file, and every stale-artifact removal, at
// whatever directory sqlgen was invoked from.

func TestLoadConfig_RejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring of the reported error
	}{
		{
			name: "the typo that caused the incident",
			yaml: `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  path: ./models
`,
			wantErr: "field path not found",
		},
		{
			name: "unknown top-level block",
			yaml: `
input:
  dialect: postgres
  paths: ["./migrations"]
outputs:
  dir: ./models
`,
			wantErr: "field outputs not found",
		},
		{
			name: "unknown key nested under generation.manifest",
			yaml: `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
generation:
  manifest:
    enabled: true
    markdwn_dir: manifest
`,
			wantErr: "field markdwn_dir not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, tt.yaml))
			if err == nil {
				t.Fatalf("LoadConfig() accepted a config with an unknown key, want an error naming it")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LoadConfig() error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadConfig_AcceptsShippedExamples is the other half of strictness: every
// config this repository ships must still load. A false positive here is a
// key the structs are missing, not a typo in the example.
func TestLoadConfig_AcceptsShippedExamples(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "testdata", "examples", "*", "sqlgen.yml"))
	if err != nil {
		t.Fatalf("globbing example configs: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no example configs found — the glob no longer matches the examples tree")
	}

	for _, path := range matches {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			if _, err := config.LoadConfig(path); err != nil {
				t.Errorf("LoadConfig(%s) = %v, want it to load under strict decoding", path, err)
			}
		})
	}
}

// TestLoadConfig_EmptyDocument pins that strictness did not turn an empty
// config file into a parse error. yaml.Decoder reports an empty document as
// io.EOF where yaml.Unmarshal reported nothing at all; validation, not the
// parser, should be what names the missing fields.
func TestLoadConfig_EmptyDocument(t *testing.T) {
	cfg, err := config.LoadConfig(writeTestConfig(t, ""))
	if err != nil {
		t.Fatalf("LoadConfig() on an empty config = %v, want defaults", err)
	}
	if cfg.Output.Dir != "." {
		t.Errorf("Output.Dir = %q, want %q", cfg.Output.Dir, ".")
	}
}

// TestValidatePreParse_OutputDirWarning pins the second half of the output.dir
// config guard: an omitted output.dir silently anchors the whole run on the
// working directory, so it warns — while an explicit "." is a deliberate choice
// and says nothing.
func TestValidatePreParse_OutputDirWarning(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantWarn bool
	}{
		{name: "omitted, defaults to the working directory", output: "output:\n  package: models\n", wantWarn: true},
		{name: "explicitly the module root", output: "output:\n  dir: \".\"\n", wantWarn: false},
		{name: "explicit subdirectory", output: "output:\n  dir: ./models\n", wantWarn: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadConfig(writeTestConfig(t, "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\n"+tt.output))
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}

			warnings, err := config.ValidatePreParse(cfg)
			if err != nil {
				t.Fatalf("ValidatePreParse() unexpected error: %v", err)
			}

			var got bool
			for _, w := range warnings {
				if strings.Contains(w.Message, "output.dir is not set") {
					got = true
				}
			}
			if got != tt.wantWarn {
				t.Errorf("ValidatePreParse() output.dir warning = %v, want %v (warnings: %v)", got, tt.wantWarn, warnings)
			}
		})
	}
}

// TestValidateManifest_RejectsEscapingPlacements pins placement containment in
// config validation. PRD §30.2 types markdown_dir / json_per_entity_dir as
// "Subdirectory under" their parent and json_filename as a bare "Filename";
// the emitters and the stale-cleanup pass both join these onto the parent, so
// a value that escapes writes and prunes outside the tree sqlgen owns.
func TestValidateManifest_RejectsEscapingPlacements(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.ManifestConfig)
		wantErr  string
		wantPass bool
	}{
		{name: "defaults are accepted", mutate: func(*config.ManifestConfig) {}, wantPass: true},
		{
			name:     "nested subdirectory is accepted",
			mutate:   func(m *config.ManifestConfig) { m.MarkdownDir = "docs/manifest" },
			wantPass: true,
		},
		{
			name:    "markdown_dir escaping via ..",
			mutate:  func(m *config.ManifestConfig) { m.MarkdownDir = "../docs" },
			wantErr: "must be a subdirectory of its parent",
		},
		{
			name:    "markdown_dir exactly ..",
			mutate:  func(m *config.ManifestConfig) { m.MarkdownDir = ".." },
			wantErr: "must be a subdirectory of its parent",
		},
		{
			name:    "markdown_dir absolute",
			mutate:  func(m *config.ManifestConfig) { m.MarkdownDir = "/tmp/manifest" },
			wantErr: "must be a subdirectory of its parent",
		},
		{
			name:    "json_per_entity_dir escaping via ..",
			mutate:  func(m *config.ManifestConfig) { m.JSONPerEntityDir = "../entities" },
			wantErr: "must be a subdirectory of its parent",
		},
		{
			name:    "json_filename carrying a path separator",
			mutate:  func(m *config.ManifestConfig) { m.JSONFilename = "../real/manifest_gen.json" },
			wantErr: "must be a bare filename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &config.ManifestConfig{
				Enabled:          new(true),
				Formats:          []string{config.ManifestFormatJSON, config.ManifestFormatMarkdown},
				JSONFilename:     "manifest_gen.json",
				JSONLayout:       config.JSONLayoutSingle,
				JSONPerEntityDir: "entities",
				MarkdownDir:      "manifest",
				EmbedInClient:    new(true),
			}
			tt.mutate(m)

			cfg := &config.RootConfig{
				Version: "v1",
				Input:   config.InputConfig{Dialect: config.DialectPostgres, Paths: []string{"./migrations"}},
				Output:  config.OutputConfig{Driver: config.DriverPgx, Dir: "./models", Package: "models"},
			}
			cfg.Generation.Manifest = m

			_, err := config.ValidatePreParse(cfg)
			if tt.wantPass {
				if err != nil && strings.Contains(err.Error(), "manifest.") {
					t.Errorf("ValidatePreParse() rejected a valid manifest placement: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() accepted %s, want an error mentioning %q", tt.name, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse() error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
