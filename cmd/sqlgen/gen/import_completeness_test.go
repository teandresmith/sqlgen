package gen_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// Every generated file must declare a complete import set of its own.
//
// goimports will resolve whatever a file references and does not declare, so an
// incomplete set is not a correctness bug — it is a cost. imports.Process
// resolves against assumed package names first and returns before it builds a
// resolver at all when nothing is missing; a missing standard-library path is
// still cheap, but a missing non-stdlib path sends it through a full scan of
// GOMODCACHE. That scan is per file and shares nothing between calls, because
// imports.Process constructs a fresh ProcessEnv each time and the public API
// exposes no way to hand it one (x/tools/imports/forward.go).
//
// The model and view emitters used to lean on that back-fill for context, fmt,
// slices, iter and the four sqlgen runtime packages. Two files — models_gen.go
// and views_gen.go — were then 97% of all generation time, and the cost scaled
// with the size of the developer's module cache rather than with the schema.
//
// Nothing else observes the difference: the emitted file is byte-identical
// either way, and TestE2EGoldenFiles would keep passing while the suite crept
// back to minutes. So this test watches the boundary directly, asking goimports
// to report every path it had to resolve on the emitter's behalf.

// importFixtureDialect carries the per-dialect spellings the shared fixture
// schema needs, so one schema shape can be generated on all three dialects.
type importFixtureDialect struct {
	dialect config.Dialect
	driver  string
	uuid    string
	text    string
	integer string
	numeric string
	stamp   string
	json    string
	// array is the column type for a bare-slice column, empty on the dialects
	// that have no array type. Postgres populates it to reach the pq.Array
	// scan shape.
	array string
}

var importFixtureDialects = []importFixtureDialect{
	{
		dialect: config.DialectPostgres, driver: "pgx",
		uuid: "uuid", text: "text", integer: "bigint", numeric: "numeric",
		stamp: "timestamptz", json: "jsonb", array: "text[]",
	},
	{
		dialect: config.DialectMySQL, driver: "stdlib",
		uuid: "binary(16)", text: "varchar(255)", integer: "bigint", numeric: "decimal(10,2)",
		stamp: "datetime", json: "json",
	},
	{
		dialect: config.DialectSQLite, driver: "stdlib",
		uuid: "text", text: "text", integer: "integer", numeric: "real",
		stamp: "timestamp", json: "text",
	},
}

// importFixtureSchema is one schema shaped to reach as much of the table and
// view templates as a hand-built fixture can: a parent with a soft-delete
// column and an arithmetic counter, a child carrying both a required and a
// nullable foreign key (O2M plus the nullable-FK loader), a JSON column, a
// unique column, a view, an enum and a composite type.
func importFixtureSchema(d importFixtureDialect) *sqlparser.Schema {
	authorCols := []sqlparser.Column{
		{Name: "id", Type: d.uuid, PrimaryKey: true},
		{Name: "email", Type: d.text, Unique: true, InlineUnique: true},
		{Name: "display_name", Type: d.text},
		{Name: "view_count", Type: d.integer},
		{Name: "balance", Type: d.numeric},
		{Name: "profile", Type: d.json, Nullable: true},
		{Name: "created_at", Type: d.stamp},
		{Name: "deleted_at", Type: d.stamp, Nullable: true},
	}
	if d.array != "" {
		authorCols = append(authorCols, sqlparser.Column{Name: "aliases", Type: d.array, Nullable: true})
	}

	return &sqlparser.Schema{
		Tables: []sqlparser.Table{
			{Name: "authors", Columns: authorCols},
			{
				Name: "articles",
				Columns: []sqlparser.Column{
					{Name: "id", Type: d.integer, PrimaryKey: true, AutoIncrement: true},
					{Name: "author_id", Type: d.uuid, FKReference: &sqlparser.FKReference{Table: "authors", Column: "id"}},
					{Name: "editor_id", Type: d.uuid, Nullable: true, FKReference: &sqlparser.FKReference{Table: "authors", Column: "id"}},
					{Name: "title", Type: d.text},
					{Name: "body", Type: d.text, Nullable: true},
					{Name: "published_at", Type: d.stamp, Nullable: true},
				},
			},
		},
		Views: []sqlparser.View{
			{
				Name: "author_stats",
				SQL:  "SELECT id, display_name, view_count FROM authors",
				Columns: []sqlparser.Column{
					{Name: "id", Type: d.uuid, PrimaryKey: true},
					{Name: "display_name", Type: d.text},
					{Name: "view_count", Type: d.integer},
				},
			},
		},
	}
}

// importFixtureConfig writes a config for one dialect/layout/feature
// combination and loads it through the real loader, so defaults land exactly as
// they do for a consumer.
func importFixtureConfig(t *testing.T, outDir string, d importFixtureDialect, layout, extra string) *config.RootConfig {
	t.Helper()

	body := `version: v1
input:
  dialect: ` + string(d.dialect) + `
  paths:
    - ./schema.sql
output:
  driver: ` + d.driver + `
  dir: ` + outDir + `
  package: models
  layout: ` + layout + `
generation:
  soft_delete_columns:
    - name: deleted_at
      type: timestamp
` + extra

	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// TestGeneratedImportsAreDeclared asserts that goimports adds nothing to any
// emitted file — that every emitter declares what its templates reference.
//
// A failure names the file and the paths that were back-filled; the fix is to
// add them to that emitter's import set (modelTemplateImports for the table and
// view templates), not to silence the test. An import that a given file does
// not use is fine and needs no entry here: the Format pass prunes it.
func TestGeneratedImportsAreDeclared(t *testing.T) {
	// The observer is package-level state in gen; these subtests share it.
	cases := []struct {
		name   string
		layout string
		extra  string
	}{
		{
			name:   "single_file_full_features",
			layout: "single_file",
			extra: `events:
  enabled: true
cache:
  enabled: true
  ttl: "1h"
  serializer: json
`,
		},
		{
			name:   "file_per_table",
			layout: "file_per_table",
		},
	}

	for _, d := range importFixtureDialects {
		for _, tc := range cases {
			t.Run(string(d.dialect)+"/"+tc.name, func(t *testing.T) {
				var drift []string
				gen.ObserveImportDriftForTest(t, func(filename string, added []string) {
					drift = append(drift, filepath.Base(filename)+": "+strings.Join(added, ", "))
				})

				outDir := t.TempDir()
				cfg := importFixtureConfig(t, outDir, d, tc.layout, tc.extra)
				if _, err := gen.Generate(importFixtureSchema(d), cfg, "test"); err != nil {
					t.Fatalf("Generate() error = %v", err)
				}

				if len(drift) > 0 {
					sort.Strings(drift)
					t.Errorf("goimports back-filled imports the emitters did not declare:\n  %s\n"+
						"Each back-filled non-stdlib path costs a full GOMODCACHE scan for that file. "+
						"Declare them on the emitter's import set.", strings.Join(drift, "\n  "))
				}
			})
		}
	}
}

// TestModelTemplateImportsCoverRuntimePackages pins the four sqlgen runtime
// packages the table and view templates bind. They are the entries that matter
// most in modelTemplateImports: a missing standard-library path is resolved
// from goimports' built-in table at no cost, while a missing one of these is
// what triggers the module-cache walk.
func TestModelTemplateImportsCoverRuntimePackages(t *testing.T) {
	outDir := t.TempDir()
	cfg := importFixtureConfig(t, outDir, importFixtureDialects[0], "single_file", "")
	if _, err := gen.Generate(importFixtureSchema(importFixtureDialects[0]), cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	models := readGenerated(t, filepath.Join(outDir, "models_gen.go"))
	for _, want := range []string{
		"github.com/teandresmith/sqlgen/comparator",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/omittable",
		"github.com/teandresmith/sqlgen/sql",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("models_gen.go does not import %q", want)
		}
	}

	// The superset must still be pruned, or file_per_table would ship
	// dangling imports: nothing in this schema reaches for net/http.
	if slices.Contains(strings.Split(models, "\n"), "\t\"net/http\"") {
		t.Error("models_gen.go imports net/http")
	}
}
