package gen_test

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

// renderManifestEmbed parses and executes the manifest-embed template, then
// runs it through the real preamble + goimports pipeline so the assertions run
// against a formatted, syntactically-valid Go file (the same pipeline the
// orchestrator uses).
func renderManifestEmbed(t *testing.T, ctx *gen.ManifestEmbedContext) string {
	t.Helper()
	tmpl, err := template.New("manifest_embed.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(filepath.Join("templates", "manifest_embed.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing manifest-embed template: %v", err)
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "manifest-embed", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	raw := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	formatted, err := gen.Format(raw, "v0.0.0-test", "manifest_embed_gen.go")
	if err != nil {
		t.Fatalf("formatting (goimports) failed — invalid Go: %v", err)
	}
	return string(formatted)
}

func manifestEmbedConfig(layout config.JSONLayout) *config.RootConfig {
	cfg := &config.RootConfig{}
	cfg.Output.Package = "store"
	cfg.Generation.Manifest = &config.ManifestConfig{
		Enabled:          new(true),
		EmbedInClient:    new(true),
		MarkdownDir:      "manifest",
		JSONFilename:     "manifest_gen.json",
		JSONLayout:       layout,
		JSONPerEntityDir: "entities",
	}
	return cfg
}

func TestBuildManifestEmbedContext_Gating(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.RootConfig)
		wantNil bool
	}{
		{"enabled + embed", func(*config.RootConfig) {}, false},
		{"no manifest block", func(c *config.RootConfig) { c.Generation.Manifest = nil }, true},
		{"manifest disabled", func(c *config.RootConfig) { c.Generation.Manifest.Enabled = new(false) }, true},
		{"embed_in_client false", func(c *config.RootConfig) { c.Generation.Manifest.EmbedInClient = new(false) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := manifestEmbedConfig(config.JSONLayoutSingle)
			tt.mutate(cfg)
			got := gen.BuildManifestEmbedContext(cfg, 2)
			if (got == nil) != tt.wantNil {
				t.Fatalf("BuildManifestEmbedContext nil=%v, want nil=%v", got == nil, tt.wantNil)
			}
		})
	}
}

func TestBuildManifestEmbedContext_Paths(t *testing.T) {
	single := gen.BuildManifestEmbedContext(manifestEmbedConfig(config.JSONLayoutSingle), 2)
	if single.PerEntity {
		t.Error("single layout: PerEntity should be false")
	}
	if single.EmbedJSONPath != "manifest/manifest_gen.json" {
		t.Errorf("EmbedJSONPath = %q", single.EmbedJSONPath)
	}
	if single.EmbedEntitiesPath != "" {
		t.Errorf("single layout: EmbedEntitiesPath should be empty, got %q", single.EmbedEntitiesPath)
	}

	per := gen.BuildManifestEmbedContext(manifestEmbedConfig(config.JSONLayoutPerEntity), 2)
	if !per.PerEntity {
		t.Error("per_entity layout: PerEntity should be true")
	}
	if per.EmbedEntitiesPath != "manifest/entities" {
		t.Errorf("EmbedEntitiesPath = %q", per.EmbedEntitiesPath)
	}
}

// TestBuildManifestEmbedContext_PerEntityZeroEntities pins the compile-trap
// guard: a per_entity package with zero entities never creates the entities
// directory, so it must downgrade to the single-file embed rather than emit a
// //go:embed all: directive over a non-existent directory.
func TestBuildManifestEmbedContext_PerEntityZeroEntities(t *testing.T) {
	ctx := gen.BuildManifestEmbedContext(manifestEmbedConfig(config.JSONLayoutPerEntity), 0)
	if ctx == nil {
		t.Fatal("context should still emit the top-level index embed")
	}
	if ctx.PerEntity {
		t.Error("zero-entity per_entity must downgrade PerEntity to false")
	}
	if ctx.EmbedEntitiesPath != "" {
		t.Errorf("EmbedEntitiesPath should be empty, got %q", ctx.EmbedEntitiesPath)
	}

	out := renderManifestEmbed(t, ctx)
	if strings.Contains(out, "//go:embed all:") {
		t.Errorf("zero-entity per_entity output must not embed the entities dir\n\n%s", out)
	}
	if !strings.Contains(out, "//go:embed manifest/manifest_gen.json") {
		t.Errorf("zero-entity per_entity output must still embed the top-level index\n\n%s", out)
	}
}

func TestManifestEmbedTemplate_SingleLayout(t *testing.T) {
	out := renderManifestEmbed(t, gen.BuildManifestEmbedContext(manifestEmbedConfig(config.JSONLayoutSingle), 2))

	wantContains := []string{
		"//go:embed manifest/manifest_gen.json",
		"var sqlgenManifestFS embed.FS",
		`"embed"`, // embed import genuinely referenced via embed.FS
		"func (c *Client) Manifest() *manifest.Document {",
		"func (c *Client) ManifestEntity(table string) (*manifest.Entity, error) {",
		"func (c *Client) ManifestVersion() string {",
		`sqlgenManifestFS.ReadFile("manifest/manifest_gen.json")`,
		"manifest.LoadInto(raw, entityFS, &sqlgenManifestDoc, sqlgenManifestEntities)",
		"return nil, manifest.ErrEntityNotFound",
		"sqlgenManifestVersion = sqlgenManifestDoc.SchemaVersion",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("single-layout output missing %q\n\n%s", w, out)
		}
	}

	// Single layout must NOT embed or sub the entities directory.
	for _, unwanted := range []string{"//go:embed all:", "fs.Sub"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("single-layout output should not contain %q\n\n%s", unwanted, out)
		}
	}
}

func TestManifestEmbedTemplate_PerEntityLayout(t *testing.T) {
	out := renderManifestEmbed(t, gen.BuildManifestEmbedContext(manifestEmbedConfig(config.JSONLayoutPerEntity), 2))

	wantContains := []string{
		"//go:embed manifest/manifest_gen.json",
		"//go:embed all:manifest/entities",
		"var sqlgenManifestFS embed.FS",
		`fs.Sub(sqlgenManifestFS, "manifest/entities")`,
		"func (c *Client) ManifestEntity(table string) (*manifest.Entity, error) {",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("per-entity output missing %q\n\n%s", w, out)
		}
	}
}
