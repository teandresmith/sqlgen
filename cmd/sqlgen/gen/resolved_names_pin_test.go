package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	sqlgenparser "github.com/teandresmith/sqlgen/parser"
)

// TestGeneratedPackageNames_MatchTemplates is the structural pin behind
// generatedPackageTypes and generatedPackageStems, and the reason the reserved
// sets cannot silently fall behind the templates.
//
// It generates the same package twice over disjoint entity names and
// intersects what the two runs declare. A name present in both renders cannot
// have come from an entity, so it is one sqlgen owns — and every such name must
// be reserved, or a schema is free to resolve onto it and be silently
// overwritten. A template that adds a package-scope declaration or a new fixed
// filename fails this test instead of widening the hazard unnoticed.
//
// The assertion is coverage, not equality: the reserved sets deliberately
// include names from feature-gated artifacts on the same terms §8.5 reserves
// field names — unconditionally, so enabling a feature later never turns a
// valid config invalid. One render cannot reach all of them at once, and the
// probe turns on the two that carry fixed *identifiers* (cache, event hooks).
// The three it leaves off — the API envelope aliases, the manifest embed and
// the MySQL SET file — declare nothing at package scope that is not derived
// from an entity name, so only their stems are reserved and only their stems
// would be missed.
func TestGeneratedPackageNames_MatchTemplates(t *testing.T) {
	first := generateProbePackage(t, "alpha_widgets", "alpha_summaries")
	second := generateProbePackage(t, "beta_gadgets", "beta_digests")

	cfg := probeNameConfig(t, t.TempDir())

	t.Run("exported package-scope names", func(t *testing.T) {
		owned := intersect(first.exported, second.exported)
		if len(owned) == 0 {
			t.Fatal("the two renders share no exported declaration; the probe is not exercising the shared templates")
		}
		reserved := reservedGeneratedTypes(cfg)
		for _, name := range owned {
			if _, ok := reserved[name]; !ok {
				t.Errorf("the generated package declares %q on its own behalf, but no entry in "+
					"generatedPackageTypes reserves it — a schema entity resolving to %q would "+
					"redeclare it", name, name)
			}
		}
	})

	t.Run("file stems", func(t *testing.T) {
		owned := intersect(first.stems, second.stems)
		if len(owned) == 0 {
			t.Fatal("the two renders share no filename; the probe is not exercising the shared templates")
		}
		reserved := reservedGeneratedStems(cfg)
		for _, stem := range owned {
			if _, ok := reserved[stem]; !ok {
				t.Errorf("the generator writes %q_gen.go on its own behalf, but no entry in "+
					"generatedPackageStems reserves the stem — a table resolving to it would be "+
					"overwritten under layout: file_per_table", stem)
			}
		}
	})
}

// TestReservedGeneratedNames_FollowConfig pins the two reserved entries that
// are not frozen: the client type and its file both carry output.client.*, so
// renaming the client must move what it reserves rather than reserve both.
func TestReservedGeneratedNames_FollowConfig(t *testing.T) {
	cfg := probeNameConfig(t, t.TempDir())
	cfg.Output.Client = &config.ClientOutputConfig{Name: "Store", File: "store_gen.go"}

	types := reservedGeneratedTypes(cfg)
	for _, want := range []string{"Store", "StoreOption"} {
		if types[want] != "store_gen.go" {
			t.Errorf("reservedGeneratedTypes()[%q] = %q, want %q", want, types[want], "store_gen.go")
		}
	}
	for _, freed := range []string{"Client", "ClientOption"} {
		if _, ok := types[freed]; ok {
			t.Errorf("reservedGeneratedTypes() still reserves %q after the client was renamed", freed)
		}
	}
	if stems := reservedGeneratedStems(cfg); stems["store"] != "store_gen.go" {
		t.Errorf("reservedGeneratedStems()[%q] = %q, want %q", "store", stems["store"], "store_gen.go")
	}
}

// TestComputeNameCollisions_GroupsByResolvedName pins the widened grouping.
// The set is keyed by SQL name — that is what StructName and SnakeName are
// handed — but membership is decided by the *resolved* name, so two different
// SQL names that land on one Go name are both marked and both take the schema
// prefix. Keyed by the SQL name alone they were left unprefixed and collided.
func TestComputeNameCollisions_GroupsByResolvedName(t *testing.T) {
	tests := []struct {
		name   string
		tables []sqlgenparser.Table
		enums  []sqlgenparser.Enum
		want   []string
	}{
		{
			name: "same bare name in two schemas",
			tables: []sqlgenparser.Table{
				{Name: "users", Schema: "public"},
				{Name: "users", Schema: "audit"},
			},
			want: []string{"users"},
		},
		{
			name: "plural and singular of one noun in two schemas",
			tables: []sqlgenparser.Table{
				{Name: "people", Schema: "public"},
				{Name: "person", Schema: "audit"},
			},
			want: []string{"people", "person"},
		},
		{
			name: "an entity category other than tables joins the group",
			tables: []sqlgenparser.Table{
				{Name: "orders", Schema: "public"},
			},
			enums: []sqlgenparser.Enum{{Name: "order", Schema: "audit"}},
			want:  []string{"order", "orders"},
		},
		{
			name: "names that only look alike are not grouped",
			tables: []sqlgenparser.Table{
				{Name: "user_roles", Schema: "public"},
				{Name: "userroles", Schema: "audit"},
			},
		},
		{
			name: "one schema needs no prefix",
			tables: []sqlgenparser.Table{
				{Name: "people", Schema: "public"},
				{Name: "person", Schema: "public"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeNameCollisions(&sqlgenparser.Schema{Tables: tt.tables, Enums: tt.enums})
			want := make(map[string]bool, len(tt.want))
			for _, n := range tt.want {
				want[n] = true
			}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("computeNameCollisions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestComputeNameCollisions_PrefixesStructAndStemTogether states the invariant
// the shared key exists for: StructName and SnakeName consult one set, so an
// entity either takes the schema prefix on both or on neither. A struct that
// moved without its file stem would put two structs in one file.
func TestComputeNameCollisions_PrefixesStructAndStemTogether(t *testing.T) {
	schema := &sqlgenparser.Schema{Tables: []sqlgenparser.Table{
		{Name: "people", Schema: "public"},
		{Name: "person", Schema: "audit"},
		{Name: "orders", Schema: "public"},
	}}
	collisions := computeNameCollisions(schema)

	for _, tbl := range schema.Tables {
		structPrefixed := StructName(tbl.Name, tbl.Schema, collisions) != StructName(tbl.Name, "", nil)
		stemPrefixed := SnakeName(tbl.Name, tbl.Schema, collisions) != SnakeName(tbl.Name, "", nil)
		if structPrefixed != stemPrefixed {
			t.Errorf("%s.%s: StructName prefixed = %v but SnakeName prefixed = %v; the two must move together",
				tbl.Schema, tbl.Name, structPrefixed, stemPrefixed)
		}
	}
}

// probePackage is what one render of the shared templates declared.
type probePackage struct {
	// exported holds every exported package-scope identifier, sorted.
	exported []string
	// stems holds the `_gen.go` stem of every emitted file, sorted.
	stems []string
}

// generateProbePackage runs the real pipeline over one table and one view with
// the given names, with the feature-gated artifacts turned on so their
// declarations reach the intersection too.
func generateProbePackage(t *testing.T, table, view string) probePackage {
	t.Helper()

	outDir := t.TempDir()
	cfg := probeNameConfig(t, outDir)
	schema := &sqlgenparser.Schema{
		Tables: []sqlgenparser.Table{{
			Name: table,
			Columns: []sqlgenparser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "label", Type: "text"},
				{Name: "hits", Type: "integer"},
			},
		}},
		Views: []sqlgenparser.View{{
			Name: view,
			Columns: []sqlgenparser.Column{
				{Name: "id", Type: "uuid"},
				{Name: "label", Type: "text"},
			},
		}},
	}

	if _, err := Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate(%q, %q) error: %v", table, view, err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("reading %s: %v", outDir, err)
	}

	var pkg probePackage
	names := make(map[string]bool)
	for _, entry := range entries {
		stem, ok := strings.CutSuffix(entry.Name(), "_gen.go")
		if !ok {
			continue
		}
		pkg.stems = append(pkg.stems, stem)

		src, err := os.ReadFile(filepath.Join(outDir, entry.Name())) //nolint:gosec // test helper, reading back a t.TempDir the test just generated into
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		for _, name := range exportedPackageNames(t, entry.Name(), src) {
			names[name] = true
		}
	}
	pkg.exported = slices.Sorted(maps.Keys(names))
	slices.Sort(pkg.stems)
	return pkg
}

// probeNameConfig is a fully-defaulted config with the feature-gated artifacts
// enabled, loaded through LoadConfig because that is the only path that applies
// the defaults the emission stage's Options depend on. `single_file` is the
// layout under test: it emits models_gen.go and views_gen.go, whose stems a
// table can also resolve to, while a per-table layout would contribute only
// entity stems that the intersection discards anyway.
func probeNameConfig(t *testing.T, outDir string) *config.RootConfig {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	body := `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ` + outDir + `
  package: db
  layout: single_file
events:
  enabled: true
cache:
  enabled: true
  ttl: "1h"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// exportedPackageNames returns every exported identifier declared at package
// scope in one generated file: types, funcs, consts and vars alike, since any
// of them redeclares a struct name that equals it.
func exportedPackageNames(t *testing.T, filename string, src []byte) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filename, src, 0)
	if err != nil {
		t.Fatalf("generated %s does not parse: %v", filename, err)
	}

	var names []string
	keep := func(name string) {
		if name != "_" && ast.IsExported(name) {
			names = append(names, name)
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil { // methods are scoped to their receiver
				keep(d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					keep(s.Name.Name)
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						keep(ident.Name)
					}
				}
			}
		}
	}
	return names
}

// intersect returns the sorted values present in both sorted slices.
func intersect(a, b []string) []string {
	var out []string
	for _, v := range a {
		if slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}
