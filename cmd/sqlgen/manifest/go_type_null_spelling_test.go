package manifest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// chainBuildInput is testBuildInput with the resolver honouring the
// config's overrides.use_pointers setting rather than forcing pointers.
func chainBuildInput(t *testing.T, schema *parser.Schema, cfg *config.RootConfig) manifest.BuildInput {
	t.Helper()
	resolver := gotype.NewResolver(cfg.Input.Dialect, *cfg.Overrides.UsePointers, cfg.Overrides.Types)
	in := &gen.GenerateInput{Schema: schema, Config: cfg, Resolver: resolver}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	return manifest.BuildInput{
		Schema:    schema,
		Config:    cfg,
		Tables:    tables,
		Enums:     gen.BuildEnumContexts(schema, nil, cfg),
		Version:   "0.42.0",
		Timestamp: "2026-05-16T00:00:00Z",
	}
}

// TestManifestGoType_NamesDatabaseSQLNullTypeUnaliased pins that the
// manifest's published `go_type` for a nullable column under
// `overrides.use_pointers: false` spells the database/sql wrapper as
// `sql.NullString`, the name a consumer importing database/sql writes, not
// the alias a generated file imports it under.
func TestManifestGoType_NamesDatabaseSQLNullTypeUnaliased(t *testing.T) {
	cfg := baseConfig()
	cfg.Overrides.UsePointers = new(false)
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "notes",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "body", Type: "text", Nullable: true},
		},
	}}}

	doc, err := manifest.Build(chainBuildInput(t, schema, cfg))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dir := t.TempDir()
	mfst := &config.ManifestConfig{JSONFilename: "manifest_gen.json", JSONLayout: config.JSONLayoutSingle, MarkdownDir: "manifest"}
	if err := manifest.EmitJSON(doc, mfst, dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest", "manifest_gen.json")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("reading emitted manifest: %v", err)
	}

	var published struct {
		Entities []struct {
			Table   string `json:"table"`
			Columns []struct {
				Name   string `json:"name"`
				GoType string `json:"go_type"`
			} `json:"columns"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &published); err != nil {
		t.Fatalf("decoding emitted manifest: %v", err)
	}
	found := false
	for _, e := range published.Entities {
		if e.Table != "notes" {
			continue
		}
		for _, c := range e.Columns {
			if c.Name != "body" {
				continue
			}
			found = true
			if c.GoType != "sql.NullString" {
				t.Errorf("published go_type of nullable text column = %q, want %q", c.GoType, "sql.NullString")
			}
		}
	}
	if !found {
		t.Fatalf("emitted manifest has no notes.body column:\n%s", data)
	}
}
