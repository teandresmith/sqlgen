package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// rwdGenerate runs the full pipeline for a postgres schema into a temp dir,
// one file per table, with extra appended to the config, and returns every
// generated Go file parsed and as source, keyed by path relative to the
// output directory.
func rwdGenerate(t *testing.T, schema *sqlparser.Schema, extra string) (map[string]*ast.File, map[string]string) {
	t.Helper()
	outDir := t.TempDir()
	body := `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ` + outDir + `
  package: db
  layout: file_per_table
` + extra
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, err := Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	sources := map[string]string{}
	err = filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path) //nolint:gosec // test helper, walking the generated output directory
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		files[rel] = f
		sources[rel] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("reading generated files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Generate() wrote no Go files")
	}
	return files, sources
}

// rwdTypeNames returns every package-level type name declared in files.
func rwdTypeNames(files map[string]*ast.File) map[string]bool {
	names := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				names[spec.(*ast.TypeSpec).Name.Name] = true
			}
		}
	}
	return names
}

// rwdIDColumn is an auto-increment bigint primary key.
var rwdIDColumn = sqlparser.Column{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true}

// TestReservedWord_ExposedTypeNamesUnescaped pins PRD §8.5 "Reserved Word
// Handling": the `Val` escape applies only to generated locals, so a table or
// enum whose name is a Go keyword yields its plain PascalCase type name
// ("capitalized forms of Go keywords (`Type`, `Range`, `Error`, `Map`) are
// valid Go identifiers").
func TestReservedWord_ExposedTypeNamesUnescaped(t *testing.T) {
	tests := []struct {
		name    string
		schema  *sqlparser.Schema
		want    string
		escaped string
	}{
		{
			name:    "table maps gives struct Map",
			schema:  &sqlparser.Schema{Tables: []sqlparser.Table{{Name: "maps", Columns: []sqlparser.Column{rwdIDColumn}}}},
			want:    "Map",
			escaped: "MapVal",
		},
		{
			name:    "table ranges gives struct Range",
			schema:  &sqlparser.Schema{Tables: []sqlparser.Table{{Name: "ranges", Columns: []sqlparser.Column{rwdIDColumn}}}},
			want:    "Range",
			escaped: "RangeVal",
		},
		{
			name: "enum type gives type Type",
			schema: &sqlparser.Schema{
				Enums: []sqlparser.Enum{{Name: "type", Values: []string{"alpha", "beta"}}},
				Tables: []sqlparser.Table{{Name: "widgets", Columns: []sqlparser.Column{
					rwdIDColumn,
					{Name: "kind", Type: "type"},
				}}},
			},
			want:    "Type",
			escaped: "TypeVal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, _ := rwdGenerate(t, tt.schema, "")
			types := rwdTypeNames(files)
			if !types[tt.want] {
				t.Errorf("generated package declares no type %s", tt.want)
			}
			if types[tt.escaped] {
				t.Errorf("generated package declares %s; the reserved-word escape must not reach exposed type names", tt.escaped)
			}
		})
	}
}

// TestReservedWord_LowercaseFormEscapesLocal pins PRD §8.5: any column whose
// lowercase form matches a reserved word produces the exposed field `Type` and
// the generated local `typeVal`. The local is what CreateMany declares for the
// column's value.
func TestReservedWord_LowercaseFormEscapesLocal(t *testing.T) {
	tests := []struct {
		name   string
		table  string
		column string
		file   string
	}{
		{name: "lower case type", table: "widgets", column: "type", file: "widget_gen.go"},
		{name: "title case Type", table: "gadgets", column: "Type", file: "gadget_gen.go"},
		{name: "upper case TYPE", table: "gizmos", column: "TYPE", file: "gizmo_gen.go"},
	}
	schema := &sqlparser.Schema{}
	for _, tt := range tests {
		schema.Tables = append(schema.Tables, sqlparser.Table{Name: tt.table, Columns: []sqlparser.Column{
			rwdIDColumn,
			{Name: tt.column, Type: "text", Default: "'kind'"},
		}})
	}
	_, sources := rwdGenerate(t, schema, "")

	localDecl := regexp.MustCompile(`(?m)^\s*var typeVal any = `)
	field := regexp.MustCompile(`(?m)^\s*Type\s+string\s+` + "`")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, ok := sources[tt.file]
			if !ok {
				t.Fatalf("no generated file %s", tt.file)
			}
			if !localDecl.MatchString(src) {
				t.Errorf("%s declares no local typeVal for column %q", tt.file, tt.column)
			}
			if !field.MatchString(src) {
				t.Errorf("%s declares no exposed field Type for column %q", tt.file, tt.column)
			}
			if got := safeGoIdent(toCamelCase(FieldName(tt.column))); got != "typeVal" {
				t.Errorf("local for column %q = %q, want %q", tt.column, got, "typeVal")
			}
		})
	}
}

// TestReservedWord_KeywordsEscape pins PRD §8.5: every Go keyword the PRD lists
// is in the escape set and escapes to `<kw>Val`.
func TestReservedWord_KeywordsEscape(t *testing.T) {
	tests := []struct{ name string }{
		{name: "break"},
		{name: "case"},
		{name: "chan"},
		{name: "const"},
		{name: "continue"},
		{name: "default"},
		{name: "defer"},
		{name: "else"},
		{name: "fallthrough"},
		{name: "for"},
		{name: "func"},
		{name: "go"},
		{name: "goto"},
		{name: "if"},
		{name: "import"},
		{name: "interface"},
		{name: "map"},
		{name: "package"},
		{name: "range"},
		{name: "return"},
		{name: "select"},
		{name: "struct"},
		{name: "switch"},
		{name: "type"},
		{name: "var"},
	}
	if len(tests) != 25 {
		t.Fatalf("keyword table has %d entries, want the PRD's 25", len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !goReservedIdents[tt.name] {
				t.Errorf("goReservedIdents lacks keyword %q", tt.name)
			}
			if got, want := safeGoIdent(tt.name), tt.name+"Val"; got != want {
				t.Errorf("safeGoIdent(%q) = %q, want %q", tt.name, got, want)
			}
		})
	}
}

// TestReservedWord_PredeclaredEscape pins PRD §8.5: the escape set covers the
// predeclared identifiers — every name in the Go universe scope — and each
// escapes to `<name>Val`.
func TestReservedWord_PredeclaredEscape(t *testing.T) {
	names := types.Universe.Names()
	if len(names) == 0 {
		t.Fatal("types.Universe has no names")
	}
	for _, name := range names {
		if !goReservedIdents[name] {
			t.Errorf("goReservedIdents lacks predeclared identifier %q", name)
		}
		if got, want := safeGoIdent(name), name+"Val"; got != want {
			t.Errorf("safeGoIdent(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestDigitLeading_PKParameterTakesColPrefix pins the parameter half of PRD
// §8.5 "Digit-Leading Handling": the resolver helpers take one Go parameter per
// primary-key column, spelled from its GraphQL name, so a digit-leading key
// column's parameter takes the `col` guard in both field_casing modes.
func TestDigitLeading_PKParameterTakesColPrefix(t *testing.T) {
	tests := []struct {
		name   string
		casing string
	}{
		{name: "camel_case", casing: config.FieldCasingCamel},
		{name: "snake_case", casing: config.FieldCasingSnake},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &sqlparser.Schema{Tables: []sqlparser.Table{{Name: "reports", Columns: []sqlparser.Column{
				{Name: "2010_id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "label", Type: "text"},
			}}}}
			in := rwdAPIInput(schema, tt.casing, nil)
			tables, err := BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			if len(apiCtx.Tables) != 1 || len(apiCtx.Tables[0].PKArgs) != 1 {
				t.Fatalf("built %d API tables, want 1 with one PK argument", len(apiCtx.Tables))
			}
			params := funcPKGoArgs(apiCtx.Tables[0].PKArgs)
			if !strings.HasPrefix(params, "col") {
				t.Errorf("PK parameter list = %q, want the parameter to take the col guard", params)
			}
		})
	}
}

// rwdAPIInput builds a postgres GenerateInput with the GraphQL API enabled
// under casing, and column_map renames from renames.
func rwdAPIInput(schema *sqlparser.Schema, casing string, renames map[string]map[string]string) *GenerateInput {
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "db"
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.CursorKeys = []string{"id"}
	cfg.Generation.UUIDVersion = "v4"
	cfg.Generation.StrictUpdates = new(true)
	cfg.Tables = map[string]config.TableConfig{}
	cfg.Views = map[string]config.ViewConfig{}
	cfg.Extras = map[string]config.ExtraType{}
	cfg.Overrides.Types = map[string]config.TypeOverride{}
	cfg.Overrides.UsePointers = new(true)
	for table, cols := range renames {
		tc := config.TableConfig{ColumnMap: map[string]config.ColumnOverride{}}
		for col, name := range cols {
			tc.ColumnMap[col] = config.ColumnOverride{Name: name}
		}
		cfg.Tables[table] = tc
	}
	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{
			Enabled:     true,
			SchemaDir:   "./graph",
			ResolverDir: "./graph",
			Package:     "graph",
			FieldCasing: casing,
		},
	}
	return &GenerateInput{
		Schema:   schema,
		Config:   cfg,
		Resolver: gotype.NewResolver(cfg.Input.Dialect, true, cfg.Overrides.Types),
	}
}

// TestDigitLeading_GraphQLFieldCasing pins PRD §8.5 "GraphQL surface" for a
// digit-leading column under both `field_casing` modes: the bound Go field is
// `Col2010Revenue`; the `db:` and `json:` tags keep `2010_revenue`; the filter
// and the create and update inputs carry an explicit gqlgen.yml field-map
// entry for every field; the row type gets an entry only for a renamed column.
func TestDigitLeading_GraphQLFieldCasing(t *testing.T) {
	tests := []struct {
		name        string
		casing      string
		wantGraphQL string
	}{
		{name: "camel_case", casing: config.FieldCasingCamel, wantGraphQL: "col2010Revenue"},
		{name: "snake_case", casing: config.FieldCasingSnake, wantGraphQL: "col_2010_revenue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &sqlparser.Schema{Tables: []sqlparser.Table{{Name: "reports", Columns: []sqlparser.Column{
				rwdIDColumn,
				{Name: "2010_revenue", Type: "bigint"},
				{Name: "label", Type: "text"},
			}}}}
			in := rwdAPIInput(schema, tt.casing, map[string]map[string]string{"reports": {"label": "Caption"}})
			tables, err := BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			if len(tables) != 1 || len(apiCtx.Tables) != 1 {
				t.Fatalf("built %d table and %d API contexts, want 1 each", len(tables), len(apiCtx.Tables))
			}
			tc, at := tables[0], apiCtx.Tables[0]

			t.Run("the bound Go field is Col2010Revenue", func(t *testing.T) {
				found := false
				for _, f := range at.Fields {
					if f.SQLName != "2010_revenue" {
						continue
					}
					found = true
					if f.GraphQLName != tt.wantGraphQL {
						t.Errorf("GraphQL field = %q, want %q", f.GraphQLName, tt.wantGraphQL)
					}
					if f.GoFieldName != "Col2010Revenue" {
						t.Errorf("bound Go field = %q, want %q", f.GoFieldName, "Col2010Revenue")
					}
				}
				if !found {
					t.Fatal("row type has no field for column 2010_revenue")
				}
			})

			t.Run("the db and json tags keep the SQL name", func(t *testing.T) {
				found := false
				for _, c := range tc.Columns {
					if c.Name != "2010_revenue" {
						continue
					}
					found = true
					if c.FieldName != "Col2010Revenue" {
						t.Errorf("struct field = %q, want %q", c.FieldName, "Col2010Revenue")
					}
					if c.DBTag != "2010_revenue" {
						t.Errorf("db tag = %q, want %q", c.DBTag, "2010_revenue")
					}
					if c.JSONTag != "2010_revenue" {
						t.Errorf("json tag = %q, want %q", c.JSONTag, "2010_revenue")
					}
				}
				if !found {
					t.Fatal("table context has no column 2010_revenue")
				}
			})

			overrides := APIGoFieldOverrides(apiCtx)

			t.Run("filter and inputs carry an entry for every field", func(t *testing.T) {
				type entry struct{ graphQL, goName string }
				surfaces := []struct {
					typeName string
					fields   []entry
				}{
					{typeName: at.StructName + "Filter"},
					{typeName: "Create" + at.StructName + "Input"},
					{typeName: "Update" + at.StructName + "Input"},
				}
				for _, f := range at.FilterFields {
					surfaces[0].fields = append(surfaces[0].fields, entry{f.GraphQLName, f.GoFieldName})
				}
				for _, f := range at.CreateInputFields {
					surfaces[1].fields = append(surfaces[1].fields, entry{f.GraphQLName, f.GoFieldName})
				}
				for _, f := range at.UpdateInputFields {
					surfaces[2].fields = append(surfaces[2].fields, entry{f.GraphQLName, f.GoFieldName})
				}
				for _, op := range at.UpdateOps {
					surfaces[2].fields = append(surfaces[2].fields,
						entry{op.IncGraphQLName, op.IncGoField}, entry{op.DecGraphQLName, op.DecGoField})
				}
				for _, s := range surfaces {
					hasDigit := false
					for _, f := range s.fields {
						if f.graphQL == "" {
							continue
						}
						if f.graphQL == tt.wantGraphQL {
							hasDigit = true
						}
						if got := overrides[s.typeName][f.graphQL].FieldName; got != f.goName {
							t.Errorf("%s.%s field-map entry = %q, want %q", s.typeName, f.graphQL, got, f.goName)
						}
					}
					if !hasDigit {
						t.Errorf("%s has no field %s", s.typeName, tt.wantGraphQL)
					}
					if got := overrides[s.typeName][tt.wantGraphQL].FieldName; got != "Col2010Revenue" {
						t.Errorf("%s.%s field-map entry = %q, want %q", s.typeName, tt.wantGraphQL, got, "Col2010Revenue")
					}
				}
			})

			t.Run("the row type has an entry only for the renamed column", func(t *testing.T) {
				want := map[string]APIModelField{"label": {FieldName: "Caption"}}
				if diff := cmp.Diff(want, overrides[at.StructName]); diff != "" {
					t.Errorf("row type %s field map (-want +got):\n%s", at.StructName, diff)
				}
			})
		})
	}
}
