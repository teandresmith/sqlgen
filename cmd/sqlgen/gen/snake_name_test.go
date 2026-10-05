package gen_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// acronymStemSchema carries one table per shape the file stem has to survive: a
// plain name, a name whose acronym extends another in the canonical set, and a
// name whose single-letter word leaves the PascalCase form no seam at all.
func acronymStemSchema() *parser.Schema {
	table := func(name string) parser.Table {
		return parser.Table{
			Name: name,
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
				{Name: "name", Type: "text", Nullable: false},
			},
		}
	}
	return &parser.Schema{
		Tables: []parser.Table{
			table("products"),
			table("osi_layers"),
			table("https_urls"),
			table("ids_alerts"),
			table("a_vpn_bs"),
		},
	}
}

// TestTableContext_SnakeName pins the file stem the generator derives for each
// table. Reading the stem back out of the PascalCase struct name would spell
// `osi_layers` as `os_ilayer_gen.go` — a name that also collides with the stem
// a table literally called `os_ilayers` would produce (PRD §8.5).
func TestTableContext_SnakeName(t *testing.T) {
	in := testInput(acronymStemSchema())

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	want := map[string]string{
		"products":   "product",
		"osi_layers": "osi_layer",
		"https_urls": "https_url",
		"ids_alerts": "ids_alert",
		"a_vpn_bs":   "a_vpn_b",
	}
	if len(contexts) != len(want) {
		t.Fatalf("BuildTableContexts() returned %d contexts, want %d", len(contexts), len(want))
	}

	seen := make(map[string]string, len(contexts))
	for _, tc := range contexts {
		if got := tc.SnakeName; got != want[tc.TableName] {
			t.Errorf("table %q: SnakeName = %q, want %q", tc.TableName, got, want[tc.TableName])
		}
		if prev, ok := seen[tc.SnakeName]; ok {
			t.Errorf("tables %q and %q share the file stem %q — one would overwrite the other",
				prev, tc.TableName, tc.SnakeName)
		}
		seen[tc.SnakeName] = tc.TableName
	}
}

// TestTableContext_SnakeName_StructNameOverride pins the one path with no SQL
// name to read: `tables.<t>.struct_name` supplies a Go identifier, so the stem
// is read back out of it.
func TestTableContext_SnakeName_StructNameOverride(t *testing.T) {
	in := testInput(acronymStemSchema())
	in.Config.Tables["osi_layers"] = config.TableConfig{StructName: "NetworkLayer"}

	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	for _, tc := range contexts {
		if tc.TableName != "osi_layers" {
			continue
		}
		if tc.StructName != "NetworkLayer" {
			t.Fatalf("StructName = %q, want %q", tc.StructName, "NetworkLayer")
		}
		if tc.SnakeName != "network_layer" {
			t.Errorf("SnakeName = %q, want %q", tc.SnakeName, "network_layer")
		}
		return
	}
	t.Fatal("osi_layers context not built")
}

// TestAPITableContext_SnakeNameMatchesTable pins the coupling between the three
// per-table filenames. The Go file, the `.graphqls` schema and the gqlgen
// resolver seed all take their stem from this one field, so they cannot drift
// apart (PRD §8.5).
func TestAPITableContext_SnakeNameMatchesTable(t *testing.T) {
	in := apiTestInput(t, acronymStemSchema())

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	stems := make(map[string]string, len(tables))
	for _, tc := range tables {
		stems[tc.TableName] = tc.SnakeName
	}
	if len(apiCtx.Tables) == 0 {
		t.Fatal("BuildAPIContext returned no tables")
	}
	for _, at := range apiCtx.Tables {
		if got, want := at.SnakeName, stems[at.SQLTable]; got != want {
			t.Errorf("table %q: APITableContext.SnakeName = %q, want %q (TableContext.SnakeName)",
				at.SQLTable, got, want)
		}
	}
}

// TestGenerate_FilePerTableStems drives the real emission path end to end:
// `file_per_table` layout writes one `<stem>_gen.go` per table, and the API
// pass writes one `<stem>_gen.graphqls` beside it. Asserting the files on disk
// is what catches a stem regression that a context-level test would miss —
// a PascalCase-derived stem puts `osi_layers` in `os_ilayer_gen.go`, and two
// tables could land in the same file with the second silently overwriting the
// first
// (PRD §8.5).
func TestGenerate_FilePerTableStems(t *testing.T) {
	outDir := t.TempDir()

	// gen.Generate needs a fully-defaulted config, which only config.LoadConfig
	// produces — the hand-built testInput fixture is enough for the context
	// builders but leaves the emission stage's Options nil.
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(`version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: `+outDir+`
  package: db
  layout: file_per_table
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: `+filepath.Join(outDir, "graph")+`
    resolver_dir: `+filepath.Join(outDir, "graph")+`
    package: graph
`), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if _, err := gen.Generate(acronymStemSchema(), cfg, "test"); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	assertHasAll(t, outDir, []string{
		"product_gen.go", "osi_layer_gen.go", "https_url_gen.go",
		"ids_alert_gen.go", "a_vpn_b_gen.go",
	})
	assertHasNone(t, outDir, []string{"os_ilayer_gen.go", "http_surl_gen.go", "id_salert_gen.go", "avpnb_gen.go"})

	assertHasAll(t, filepath.Join(outDir, "graph"), []string{
		"product_gen.graphqls", "osi_layer_gen.graphqls", "https_url_gen.graphqls",
		"ids_alert_gen.graphqls", "a_vpn_b_gen.graphqls",
	})
}

// assertHasAll fails for every wanted filename missing from dir.
func assertHasAll(t *testing.T, dir string, want []string) {
	t.Helper()
	got := dirEntries(t, dir)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("%s: missing %q (have %s)", dir, w, strings.Join(got, ", "))
		}
	}
}

// assertHasNone fails for every PascalCase-derived (wrong) filename still
// present in dir.
func assertHasNone(t *testing.T, dir string, unwanted []string) {
	t.Helper()
	got := dirEntries(t, dir)
	for _, u := range unwanted {
		if slices.Contains(got, u) {
			t.Errorf("%s: %q should not be emitted", dir, u)
		}
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}
