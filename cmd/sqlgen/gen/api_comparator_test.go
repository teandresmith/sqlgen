package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// translatorTestSchema returns a schema covering each comparator family the
// generator emits a translator for: String / Numeric / Boolean / Time / ID,
// plus nullable variants for String / Numeric / Time. The schema also
// includes a soft-delete column so the filter translator covers the
// IncludeDeleted pass-through.
//
// Postgres SQL types are used throughout — the gotype resolver in
// apiTestInput is postgres-bound, and the comparator translator output is
// dialect-independent (SQL emission is delegated to the runtime comparator
// package). Per-dialect coverage in the test names below sweeps the same
// schema through each dialect to confirm the translator stays stable.
func translatorTestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "description", Type: "text", Nullable: true},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "discount", Type: "integer", Nullable: true},
					{Name: "active", Type: "boolean", Nullable: false},
					{Name: "released_at", Type: "timestamp", Nullable: false},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
}

func translatorAPIContext(t *testing.T, dialect config.Dialect) *gen.APIContext {
	t.Helper()
	in := apiTestInput(t, translatorTestSchema())
	in.Config.Input.Dialect = dialect
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"
	return apiCtx
}

func renderComparatorTranslate(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/comparator-translate", ctx); err != nil {
		t.Fatalf("rendering api/comparator-translate: %v", err)
	}
	return buf.String()
}

func TestComparatorFamily_StringPerDialect(t *testing.T) {
	// Per-dialect coverage: the generated translator is dialect-independent
	// (the comparator package itself dispatches per dialect at runtime), but
	// the schema fixture differs per dialect — text vs varchar binding —
	// and the test pins that the translator emission shape stays the same
	// across all three dialects.
	dialects := []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite}
	for _, d := range dialects {
		t.Run(string(d), func(t *testing.T) {
			out := renderComparatorTranslate(t, translatorAPIContext(t, d))

			// Base translator covers the full StringComparator surface
			// (eq, neq, contains, startsWith, endsWith, like, in, nin).
			mustContain(t, out, "func translateStringComparator(in *StringComparator) *comparator.String {")
			ops := []string{
				"if in.Eq != nil",
				"out.Eq = in.Eq",
				"if in.Neq != nil",
				"if in.Contains != nil",
				"out.Contains = in.Contains",
				"if in.StartsWith != nil",
				"if in.EndsWith != nil",
				"if in.Like != nil",
				"if len(in.In) > 0",
				"out.In = append([]string(nil), in.In...)",
				"if len(in.Nin) > 0",
				"out.Nin = append([]string(nil), in.Nin...)",
			}
			for _, op := range ops {
				mustContain(t, out, op)
			}

			// Nullable variant exists (description column is nullable string).
			// It takes its OWN NullableStringComparator input — PRD §26.4
			// Rule 2 makes the two distinct GraphQL input types, so it copies
			// the operands rather than delegating to the base translator, and
			// carries `isNull` into the wrapper's Null field.
			mustContain(t, out, "func translateNullableStringComparator(in *NullableStringComparator) *comparator.NullableString {")
			mustContain(t, out, "out := &comparator.NullableString{}")
			mustContain(t, out, "if in.IsNull != nil {")
			mustContain(t, out, "out.Null = in.IsNull")
			if strings.Contains(out, "base := translateStringComparator(in)") {
				t.Errorf("nullable translator still delegates to the base translator\n%s", out)
			}
		})
	}
}

func TestComparatorFamily_NumericPerDialect(t *testing.T) {
	dialects := []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite}
	for _, d := range dialects {
		t.Run(string(d), func(t *testing.T) {
			out := renderComparatorTranslate(t, translatorAPIContext(t, d))

			// Numeric is per-T — `stock` non-null int32 produces an Int32
			// translator; nullable `discount` produces both base + nullable.
			// The exact T (int32 / int64) depends on each dialect's gotype
			// resolution, so anchor on the family signature plus the
			// dispatch operators rather than a fixed T.
			mustContain(t, out, "comparator.Number[")
			mustContain(t, out, "comparator.NullableNumber[")

			// Each operator path is present — eq / neq / gt / gte / lt / lte / in / between.
			ops := []string{
				"if in.Eq != nil",
				"if in.Neq != nil",
				"if in.Gt != nil",
				"if in.Gte != nil",
				"if in.Lt != nil",
				"if in.Lte != nil",
				"if len(in.In) > 0",
				"if in.Between != nil",
			}
			for _, op := range ops {
				mustContain(t, out, op)
			}

			// Between maps GraphQL NumericRange{From, To} → comparator.Range{Start, End}.
			mustContain(t, out, "Start:")
			mustContain(t, out, "End:")
			mustContain(t, out, "in.Between.From")
			mustContain(t, out, "in.Between.To")
		})
	}
}

func TestComparatorFamily_TimeAndBooleanAndUUID(t *testing.T) {
	dialects := []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite}
	for _, d := range dialects {
		t.Run(string(d), func(t *testing.T) {
			out := renderComparatorTranslate(t, translatorAPIContext(t, d))

			// Time family: non-nullable `released_at` + nullable `deleted_at`.
			mustContain(t, out, "func translateTimeComparator(in *TimeComparator) *comparator.Time {")
			mustContain(t, out, "func translateNullableTimeComparator(in *NullableTimeComparator) *comparator.NullableTime {")
			mustContain(t, out, "&comparator.Range[time.Time]{Start: in.Between.From, End: in.Between.To}")

			// Boolean family: `active` non-null boolean — only base is needed.
			mustContain(t, out, "func translateBooleanComparator(in *BooleanComparator) *comparator.Bool {")
			mustContain(t, out, "out := &comparator.Bool{}")
			if strings.Contains(out, "func translateNullableBooleanComparator(") {
				t.Errorf("nullable boolean translator emitted without a nullable bool column\n%s", out)
			}

			// IDComparator (PK is uuid → ID family; PKs are filterable through
			// IDComparator → translateIDComparator emitted whenever any PK
			// column would propagate. The fixture's PK is excluded from the
			// per-table filter, so without a non-PK ID column the translator
			// is omitted. This anchors the emit-only-when-needed behaviour
			// — switching the schema to add a non-PK ID column would emit it.
			if strings.Contains(out, "translateIDComparator") {
				t.Errorf("IDComparator emitted without a non-PK ID column in the fixture\n%s", out)
			}
		})
	}
}

func TestComparatorFamily_IDOnlyWhenForeignKey(t *testing.T) {
	// Schema with a non-PK ID column (foreign key to another table) —
	// triggers the IDComparator translator so we cover the family.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "companies",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "company_id", Type: "uuid", Nullable: false, FKReference: &parser.FKReference{Table: "companies", Column: "id"}},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	out := renderComparatorTranslate(t, apiCtx)

	mustContain(t, out, "func translateIDComparator(in *IDComparator) *comparator.ID {")
	mustContain(t, out, "out := &comparator.ID{}")
	mustContain(t, out, "if in.Eq != nil")
	mustContain(t, out, "if in.Neq != nil")
	mustContain(t, out, "if len(in.In) > 0")
	mustContain(t, out, "if len(in.Nin) > 0")
}
