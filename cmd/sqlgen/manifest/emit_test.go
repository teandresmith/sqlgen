package manifest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// --- RunStage / CleanStale test helpers ---

// stageSchema is the two-table fixture the orchestration tests run against. Two
// tables (one of which is opt-out target audit_logs) let the per-table opt-out
// and removed-table stale-cleanup cases distinguish "this file went away" from
// "the whole surface went away".
func stageSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name:    "products",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "name", Type: "text"}},
			},
			{
				Name:    "audit_logs",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "action", Type: "text"}},
			},
		},
	}
}

// fullManifestConfig returns a manifest config with every field explicitly set
// (mirroring config.applyManifestDefaults, which is unexported) so the
// tri-state *bool flags resolve deterministically without a LoadConfig round.
func fullManifestConfig(layout config.JSONLayout) *config.ManifestConfig {
	return &config.ManifestConfig{
		Enabled:          new(true),
		Formats:          []string{config.ManifestFormatJSON, config.ManifestFormatMarkdown},
		JSONFilename:     "manifest_gen.json",
		JSONLayout:       layout,
		JSONPerEntityDir: "entities",
		MarkdownDir:      "manifest",
		EmbedInClient:    new(true),
		Breadcrumbs: config.BreadcrumbsConfig{
			ClaudeMD:   new(true),
			AgentsMD:   new(true),
			PackageDoc: new(true),
		},
	}
}

// stageConfig builds a manifest-enabled postgres config writing to dir.
func stageConfig(dir string, layout config.JSONLayout) *config.RootConfig {
	cfg := baseConfig()
	cfg.Output.Dir = dir
	cfg.Generation.Manifest = fullManifestConfig(layout)
	return cfg
}

// makeStageInput builds the gen contexts for schema+cfg and returns a
// StageInput, mirroring the in-memory state gen.Generate hands the manifest
// stage in production.
func makeStageInput(t *testing.T, schema *parser.Schema, cfg *config.RootConfig, timestamp string) manifest.StageInput {
	t.Helper()
	resolver := gotype.NewResolver(cfg.Input.Dialect, true, cfg.Overrides.Types)
	in := &gen.GenerateInput{Schema: schema, Config: cfg, Resolver: resolver}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	return manifest.StageInput{
		Schema:    schema,
		Config:    cfg,
		Tables:    tables,
		Views:     views,
		Version:   "0.42.0",
		Timestamp: timestamp,
	}
}

func runStage(t *testing.T, in manifest.StageInput) []string {
	t.Helper()
	return runStageResult(t, in).Warnings
}

func runStageResult(t *testing.T, in manifest.StageInput) *manifest.StageResult {
	t.Helper()
	res, err := manifest.RunStage(in)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	return res
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if !exists(t, path) {
		t.Errorf("expected %s to exist, but it does not", path)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if exists(t, path) {
		t.Errorf("expected %s to be absent, but it exists", path)
	}
}

// --- tests ---

func TestRunStage_DisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := baseConfig()
	cfg.Output.Dir = dir
	cfg.Generation.Manifest = nil // no manifest block at all

	warnings := runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	if len(warnings) != 0 {
		t.Errorf("disabled-by-default produced warnings: %v", warnings)
	}
	assertAbsent(t, filepath.Join(dir, "manifest"))
	assertAbsent(t, filepath.Join(dir, "CLAUDE.md"))
	assertAbsent(t, filepath.Join(dir, "AGENTS.md"))
	assertAbsent(t, filepath.Join(dir, "manifest_embed_gen.go"))
}

func TestRunStage_EmitsFullArtifactSet(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutSingle)

	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	md := filepath.Join(dir, "manifest")
	assertExists(t, filepath.Join(md, "manifest_gen.json"))
	assertExists(t, filepath.Join(md, "_index.md"))
	assertExists(t, filepath.Join(md, "_conventions.md"))
	assertExists(t, filepath.Join(md, "product.md"))
	assertExists(t, filepath.Join(md, "audit_log.md"))
	assertExists(t, filepath.Join(dir, "CLAUDE.md"))
	assertExists(t, filepath.Join(dir, "AGENTS.md"))
	assertExists(t, filepath.Join(dir, "manifest_embed_gen.go"))
}

func TestRunStage_PerTableOptOut(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutPerEntity)
	cfg.Tables["audit_logs"] = config.TableConfig{
		Manifest: &config.TableManifestConfig{Enabled: new(false)},
	}

	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	md := filepath.Join(dir, "manifest")
	// The opted-out entity is absent in both markdown and per-entity JSON.
	assertAbsent(t, filepath.Join(md, "audit_log.md"))
	assertAbsent(t, filepath.Join(md, "entities", "audit_log.json"))
	// The included entity is present.
	assertExists(t, filepath.Join(md, "product.md"))
	assertExists(t, filepath.Join(md, "entities", "product.json"))
}

func TestCleanStale_RemovedTable(t *testing.T) {
	layouts := map[string]config.JSONLayout{
		"single":     config.JSONLayoutSingle,
		"per_entity": config.JSONLayoutPerEntity,
	}
	for name, layout := range layouts {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := stageConfig(dir, layout)

			// First run: both tables present.
			runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))
			md := filepath.Join(dir, "manifest")
			assertExists(t, filepath.Join(md, "audit_log.md"))

			// Second run: audit_logs removed from the schema.
			reduced := &parser.Schema{Tables: stageSchema().Tables[:1]}
			runStage(t, makeStageInput(t, reduced, cfg, "2026-07-09T00:00:00Z"))

			assertAbsent(t, filepath.Join(md, "audit_log.md"))
			assertExists(t, filepath.Join(md, "product.md"))
			if layout == config.JSONLayoutPerEntity {
				assertAbsent(t, filepath.Join(md, "entities", "audit_log.json"))
				assertExists(t, filepath.Join(md, "entities", "product.json"))
			}
		})
	}
}

func TestRunStage_EnabledFlipRemovesArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutSingle)

	// Enabled run writes the full set.
	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))
	assertExists(t, filepath.Join(dir, "manifest"))
	assertExists(t, filepath.Join(dir, "CLAUDE.md"))
	assertExists(t, filepath.Join(dir, "manifest_embed_gen.go"))

	// Disable and regenerate: the whole surface is swept.
	cfg.Generation.Manifest.Enabled = new(false)
	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	assertAbsent(t, filepath.Join(dir, "manifest"))
	assertAbsent(t, filepath.Join(dir, "CLAUDE.md"))
	assertAbsent(t, filepath.Join(dir, "AGENTS.md"))
	assertAbsent(t, filepath.Join(dir, "manifest_embed_gen.go"))
}

func TestRunStage_BreadcrumbOptOut(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutSingle)

	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))
	assertExists(t, filepath.Join(dir, "CLAUDE.md"))
	assertExists(t, filepath.Join(dir, "AGENTS.md"))

	// Turn off claude_md only; AGENTS.md must survive.
	cfg.Generation.Manifest.Breadcrumbs.ClaudeMD = new(false)
	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	assertAbsent(t, filepath.Join(dir, "CLAUDE.md"))
	assertExists(t, filepath.Join(dir, "AGENTS.md"))
}

func TestRunStage_MarkerGatedDeletionPreservesUserFile(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutSingle)

	// A hand-authored CLAUDE.md with no provenance marker.
	userClaude := filepath.Join(dir, "CLAUDE.md")
	userBody := []byte("# My project notes\n\nHand-written, sqlgen must not touch this.\n")
	if err := os.WriteFile(userClaude, userBody, 0o600); err != nil {
		t.Fatalf("seeding user CLAUDE.md: %v", err)
	}

	// Enabled run: sqlgen skips the user file (returns a warning) but still
	// writes AGENTS.md.
	warnings := runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))
	if len(warnings) == 0 {
		t.Error("expected a user-owned-file warning for CLAUDE.md, got none")
	}
	if got := readFile(t, userClaude); !bytes.Equal(got, userBody) {
		t.Error("sqlgen overwrote the user-authored CLAUDE.md")
	}

	// Disable and regenerate: the user file is still preserved (marker-gated
	// deletion), while the sqlgen-owned AGENTS.md is removed.
	cfg.Generation.Manifest.Enabled = new(false)
	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	if got := readFile(t, userClaude); !bytes.Equal(got, userBody) {
		t.Error("sqlgen deleted or modified the user-authored CLAUDE.md on disable")
	}
	assertAbsent(t, filepath.Join(dir, "AGENTS.md"))
}

// TestRunStage_NestedGraphBreadcrumbs pins that when GraphQL is enabled the
// generated graph package receives its own breadcrumbs (PRD §30.3), whether the
// graph tree is a top-level sibling of models or — as here, the default layout —
// nested under it (resolver_dir = <output.dir>/graph). The graph breadcrumb
// carries an inward relative pointer to the models manifest, and is swept on
// disable just like the models breadcrumbs.
func TestRunStage_NestedGraphBreadcrumbs(t *testing.T) {
	dir := t.TempDir()
	cfg := stageConfig(dir, config.JSONLayoutSingle)
	graphDir := filepath.Join(dir, "graph")
	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{
			Enabled:     true,
			SchemaDir:   graphDir,
			ResolverDir: graphDir,
			Package:     "graph",
		},
	}

	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	// The nested graph package gets its own breadcrumbs, distinct from the
	// models breadcrumbs written at the output dir.
	assertExists(t, filepath.Join(dir, "CLAUDE.md"))
	assertExists(t, filepath.Join(graphDir, "CLAUDE.md"))
	assertExists(t, filepath.Join(graphDir, "AGENTS.md"))
	// The graph breadcrumb points inward at the models manifest.
	if got := readFile(t, filepath.Join(graphDir, "CLAUDE.md")); !bytes.Contains(got, []byte("../manifest/_index.md")) {
		t.Errorf("nested graph CLAUDE.md missing inward manifest pointer ../manifest/_index.md; got:\n%s", got)
	}

	// Disable and regenerate: the nested graph breadcrumbs are swept too.
	cfg.Generation.Manifest.Enabled = new(false)
	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))
	assertAbsent(t, filepath.Join(graphDir, "CLAUDE.md"))
	assertAbsent(t, filepath.Join(graphDir, "AGENTS.md"))
}

// TestCleanStale_GuardsAgainstOutputDirWipe pins the containment guard: a
// disabled manifest config whose markdown_dir resolves back to the output dir
// (explicit ".") must never RemoveAll the output tree. The disabled-path
// validator does not reject markdown_dir ".", so the guard is the only defense.
func TestCleanStale_GuardsAgainstOutputDirWipe(t *testing.T) {
	dir := t.TempDir()

	// A pre-existing file that must survive.
	keep := filepath.Join(dir, "important_gen.go")
	if err := os.WriteFile(keep, []byte("package models\n"), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	cfg := baseConfig()
	cfg.Output.Dir = dir
	cfg.Generation.Manifest = &config.ManifestConfig{
		Enabled:     new(false),
		MarkdownDir: ".", // resolves markdownDir == outputDir
	}

	runStage(t, makeStageInput(t, stageSchema(), cfg, "2026-07-09T00:00:00Z"))

	if !exists(t, keep) {
		t.Fatal("CleanStale wiped the output directory via markdown_dir \".\"")
	}
}

func TestRunStage_TimestampDeterminism(t *testing.T) {
	read := func(dir string) []byte {
		t.Helper()
		return readFile(t, filepath.Join(dir, "manifest", "manifest_gen.json"))
	}

	const ts = "2026-07-09T12:34:56Z"

	dir1 := t.TempDir()
	runStage(t, makeStageInput(t, stageSchema(), stageConfig(dir1, config.JSONLayoutSingle), ts))
	dir2 := t.TempDir()
	runStage(t, makeStageInput(t, stageSchema(), stageConfig(dir2, config.JSONLayoutSingle), ts))

	if !bytes.Equal(read(dir1), read(dir2)) {
		t.Error("two runs with the same --manifest-timestamp produced differing manifest_gen.json")
	}
	if !bytes.Contains(read(dir1), []byte(ts)) {
		t.Errorf("generated_at not pinned to %q in manifest_gen.json", ts)
	}
}
