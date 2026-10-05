package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// frozenLastWordSchema carries one table per shape a frozen last word takes: an acronym
// the library's suffix trim doubled (`user_ips` → `UserIPSIPS`), an acronym the
// generic "-s" rule truncated (`client_os` → `ClientO`), an ordinary noun the
// same rule truncated (`lens` → `Len`), an acronym-final singular that must
// still pluralize (`user_ip` → `user_ips`), and an acronym that is not the last
// word and therefore still inflects.
func frozenLastWordSchema() *parser.Schema {
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
			table("user_ips"),
			table("client_os"),
			table("lens"),
			table("user_ip"),
			table("dns_records"),
		},
	}
}

// TestTableContext_FrozenNames pins the struct name, the file stem and the
// `hook.TableName` constant a frozen word produces, through the real context
// builder. A regression here makes `user_ips` produce `UserIPSIPS` in
// `user_ips_ips_gen.go` under `TableUserIPSIPSes` (PRD §8.5).
func TestTableContext_FrozenNames(t *testing.T) {
	contexts, err := gen.BuildTableContexts(testInput(frozenLastWordSchema()), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	want := map[string]struct{ structName, snakeName, constant string }{
		"user_ips":    {"UserIPS", "user_ips", "TableUserIPSes"},
		"client_os":   {"ClientOS", "client_os", "TableClientOSes"},
		"lens":        {"Lens", "lens", "TableLenses"},
		"user_ip":     {"UserIP", "user_ip", "TableUserIPs"},
		"dns_records": {"DNSRecord", "dns_record", "TableDNSRecords"},
	}
	if len(contexts) != len(want) {
		t.Fatalf("BuildTableContexts() returned %d contexts, want %d", len(contexts), len(want))
	}

	for _, tc := range contexts {
		w, ok := want[tc.TableName]
		if !ok {
			t.Errorf("unexpected table %q", tc.TableName)
			continue
		}
		if tc.StructName != w.structName {
			t.Errorf("table %q: StructName = %q, want %q", tc.TableName, tc.StructName, w.structName)
		}
		if tc.SnakeName != w.snakeName {
			t.Errorf("table %q: SnakeName = %q, want %q", tc.TableName, tc.SnakeName, w.snakeName)
		}
		if tc.TableNameConstant != w.constant {
			t.Errorf("table %q: TableNameConstant = %q, want %q", tc.TableName, tc.TableNameConstant, w.constant)
		}
		if got := gen.TableConstantName(tc.TableName, tc.Schema, "", nil); got != tc.TableNameConstant {
			t.Errorf("table %q: TableNameConstant = %q but TableConstantName spells it %q — the two derivations drifted",
				tc.TableName, tc.TableNameConstant, got)
		}
	}
}

// TestAPITableContext_QueryNamesDistinct pins the invariant that decides
// whether the generated schema is valid at all: the single-row query and the
// Connection query are emitted into one `extend type Query` block, and their
// resolvers are two methods on one receiver. If pluralization returned its
// input for an acronym-final table, both would carry the same name and gqlgen
// would reject the schema (PRD §8.5).
func TestAPITableContext_QueryNamesDistinct(t *testing.T) {
	tables, err := gen.BuildTableContexts(apiTestInput(t, frozenLastWordSchema()), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, apiTestInput(t, frozenLastWordSchema()).Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if len(apiCtx.Tables) == 0 {
		t.Fatal("BuildAPIContext returned no tables")
	}

	for _, at := range apiCtx.Tables {
		if at.QueryName == at.QueryNamePlural {
			t.Errorf("table %q: QueryName and QueryNamePlural are both %q — duplicate field in `extend type Query`",
				at.SQLTable, at.QueryName)
		}
		if at.StructName == at.StructNamePlural {
			t.Errorf("table %q: StructName and StructNamePlural are both %q — duplicate method on the resolver receiver",
				at.SQLTable, at.StructName)
		}
		if got := gen.StructNamePlural(at.StructName); got != at.StructNamePlural {
			t.Errorf("table %q: StructNamePlural = %q, want %q — the two derivations drifted",
				at.SQLTable, at.StructNamePlural, got)
		}
	}
}

// uncountableSchema carries the two uncountable nouns most plausible as table names.
// Their plural is their singular, so without a distinct plural each would
// produce one name for both the single-row query and the Connection query.
func uncountableSchema() *parser.Schema {
	table := func(name string) parser.Table {
		return parser.Table{
			Name: name,
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
				{Name: "title", Type: "text", Nullable: false},
			},
		}
	}
	return &parser.Schema{Tables: []parser.Table{table("media"), table("series")}}
}

// TestAPITableContext_UncountableQueryNamesDistinct is the second route into the
// same invariant TestAPITableContext_QueryNamesDistinct covers for acronyms: an
// uncountable noun pluralizes to itself, so `media` gave `QueryName` and
// `QueryNamePlural` the same name (PRD §8.5).
func TestAPITableContext_UncountableQueryNamesDistinct(t *testing.T) {
	in := apiTestInput(t, uncountableSchema())
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if len(apiCtx.Tables) != 2 {
		t.Fatalf("BuildAPIContext returned %d tables, want 2", len(apiCtx.Tables))
	}

	want := map[string]struct{ structName, plural string }{
		"media":  {"Media", "Medias"},
		"series": {"Series", "Serieses"},
	}
	for _, at := range apiCtx.Tables {
		w := want[at.SQLTable]
		if at.StructName != w.structName {
			t.Errorf("table %q: StructName = %q, want %q", at.SQLTable, at.StructName, w.structName)
		}
		if at.StructNamePlural != w.plural {
			t.Errorf("table %q: StructNamePlural = %q, want %q", at.SQLTable, at.StructNamePlural, w.plural)
		}
		if at.QueryName == at.QueryNamePlural {
			t.Errorf("table %q: QueryName and QueryNamePlural are both %q — duplicate field in `extend type Query`",
				at.SQLTable, at.QueryName)
		}
		if at.StructName == at.StructNamePlural {
			t.Errorf("table %q: StructName and StructNamePlural are both %q — duplicate method on the resolver receiver",
				at.SQLTable, at.StructName)
		}
	}
}

// TestGenerate_UncountableSchemaFieldsDeclaredOnce drives the real emission path:
// a duplicate field name is only invalid once it is written into the schema, so
// the assertion is on the emitted `.graphqls` rather than on the context. A
// regression makes `media_gen.graphqls` declare `media(...)` twice inside one
// `extend type Query` block and gqlgen rejected the schema.
func TestGenerate_UncountableSchemaFieldsDeclaredOnce(t *testing.T) {
	outDir := t.TempDir()
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
	if _, err := gen.Generate(uncountableSchema(), cfg, "test"); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, stem := range []string{"media", "series"} {
		path := filepath.Join(outDir, "graph", stem+"_gen.graphqls")
		body, err := os.ReadFile(path) //nolint:gosec // path is built from t.TempDir
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		seen := map[string]int{}
		inQuery := false
		for line := range strings.SplitSeq(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "extend type Query"):
				inQuery = true
			case trimmed == "}":
				inQuery = false
			case inQuery:
				if field, _, ok := strings.Cut(trimmed, "("); ok {
					seen[field]++
				}
			}
		}
		if len(seen) == 0 {
			t.Errorf("%s: no Query fields found", path)
		}
		for field, n := range seen {
			if n > 1 {
				t.Errorf("%s: Query field %q declared %d times — invalid schema", path, field, n)
			}
		}
	}
}
