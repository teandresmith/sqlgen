package gen

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/sql"
)

// TestGeneratedCode_AlwaysParses_CreateTemplate is the structural guarantee
// behind PRD §8.5 (Reserved Word Handling): rendering create.go.tmpl with a
// column whose camelCase form is a Go keyword, predeclared identifier, or
// generator-reserved local must always produce parseable Go. The test fuzzes
// every entry of goReservedIdents across the PostgreSQL (default) and SQLite
// (NewDefaultExpr) branches — the two structurally distinct emission shapes
// in the template's bare-local section. If a future template edit drops the
// `| safeGoIdent` pipe from any of the eight emission sites, at least one of
// the ~75 reserved names will produce code that fails to parse, and the
// corresponding sub-test fails loudly.
//
// Scope notes:
//   - Pure go/parser.ParseFile — catches keyword-as-identifier syntax errors
//     (`var type any` won't parse), which is the production-discovered
//     defect that motivated safeGoIdent. Predeclared-identifier
//     shadowing (`var errVal any` while still referencing `err`) is not
//     caught at the parse layer but cannot occur with the current uniform
//     pipe application across all eight sites; the test still pins that
//     state.
//   - The tenancy branch shares the same emission shape as the default
//     branch (one `var ... safeGoIdent`, two assigns, one append). Covering
//     postgres+sqlite is sufficient for the structural guarantee.
func TestGeneratedCode_AlwaysParses_CreateTemplate(t *testing.T) {
	dialects := []struct {
		name    string
		dialect sql.Dialect
		key     config.Dialect
	}{
		{name: "postgres", dialect: sql.NewPostgresDialect(), key: config.DialectPostgres},
		{name: "sqlite", dialect: sql.NewSQLiteDialect(), key: config.DialectSQLite},
	}

	for _, d := range dialects {
		t.Run(d.name, func(t *testing.T) {
			tmpl := loadCreateTemplateInternal(t, d.dialect)
			for name := range goReservedIdents {
				t.Run(name, func(t *testing.T) {
					ctx := minimalCreateContextForColumn(name, d.key)
					out := renderCreateAndWrap(t, tmpl, ctx)
					if _, err := parser.ParseFile(token.NewFileSet(), "x.go", out, 0); err != nil {
						t.Fatalf("rendered code does not parse with column %q (dialect=%s):\nerror: %v\n\n--- output ---\n%s",
							name, d.name, err, out)
					}
				})
			}
			// §8.5 "Digit-Leading Handling" — column names that begin with a
			// digit must also produce parseable Go. Without the prefix guard
			// in toPascalCase / toCamelCase / safeGoIdent, the bare-local
			// emission site would render `var 2010 any = sql.Default` which
			// fails to parse.
			for _, digitColumn := range digitLeadingColumnFixtures {
				t.Run(digitColumn, func(t *testing.T) {
					ctx := minimalCreateContextForColumn(digitColumn, d.key)
					out := renderCreateAndWrap(t, tmpl, ctx)
					if _, err := parser.ParseFile(token.NewFileSet(), "x.go", out, 0); err != nil {
						t.Fatalf("rendered code does not parse with column %q (dialect=%s):\nerror: %v\n\n--- output ---\n%s",
							digitColumn, d.name, err, out)
					}
				})
			}
		})
	}
}

// digitLeadingColumnFixtures pins the §8.5 prefix rule against the same
// emission sites covered by the reserved-word sweep. Covers numeric-only,
// digit-prefixed snake_case, and single-digit-with-suffix shapes.
var digitLeadingColumnFixtures = []string{
	"2010",
	"2010_revenue",
	"1st_place",
}

// loadCreateTemplateInternal mirrors gen_test.loadCreateTemplate but uses the
// internal-package FuncMap directly so we can read goReservedIdents in the
// same file.
func loadCreateTemplateInternal(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("create.go.tmpl").
		Funcs(FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "create.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing create template: %v", err)
	}
	return tmpl
}

func renderCreateAndWrap(t *testing.T, tmpl *template.Template, ctx TableContext) string {
	t.Helper()
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/create", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

// minimalCreateContextForColumn builds a one-omittable-column TableContext
// whose column name is the supplied reserved word. The column is omittable
// and non-PK, so the default ("sql.Default") branch in create.go.tmpl is the
// path exercised for the postgres dialect; the sqlite dialect drives the
// `sql.NewDefaultExpr` branch via the same column. Both branches must emit
// the column-derived local through the safeGoIdent pipe.
func minimalCreateContextForColumn(reserved string, dialectKey config.Dialect) TableContext {
	fieldName := FieldName(reserved)
	imports := []string{
		"context",
		"fmt",
		"github.com/gofrs/uuid/v5",
		"github.com/teandresmith/sqlgen/comparator",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/omittable",
		"github.com/teandresmith/sqlgen/sql",
	}
	return TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           dialectKey,
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyDB,
		BatchSize:         100,
		Imports:           imports,
		PKColumns: []ColumnContext{
			{
				Name:       "id",
				FieldName:  "ID",
				GoType:     "uuid.UUID",
				ZeroValue:  "uuid.UUID{}",
				DBTag:      "id",
				JSONTag:    "id",
				PrimaryKey: true,
				Import:     "github.com/gofrs/uuid/v5",
				FKConvert:  gotype.FKStringStringer,
				HasDefault: true,
			},
		},
		CreateInputFields: []InputFieldContext{
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "ID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "id", Omittable: true, JSONTag: "id"},
			{
				FieldName:   fieldName,
				GoType:      "omittable.Value[string]",
				ColumnName:  reserved,
				Omittable:   true,
				JSONTag:     reserved,
				DefaultExpr: "''",
			},
		},
		Operations: ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}
