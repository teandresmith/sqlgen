package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// buildProjectionAPIContext runs a prepared input through the full table +
// API context build so the assertions below exercise the same resolution
// path codegen does, not a hand-assembled context.
func buildProjectionAPIContext(t *testing.T, in *gen.GenerateInput) *gen.APIContext {
	t.Helper()
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	return apiCtx
}

// projectionAPIContext is the common "one postgres schema, default type
// bindings" shape.
func projectionAPIContext(t *testing.T, schema *parser.Schema) *gen.APIContext {
	t.Helper()
	return buildProjectionAPIContext(t, apiTestInput(t, schema))
}

func fieldBySQLName(t *testing.T, tbl gen.APITableContext, name string) gen.APIFieldContext {
	t.Helper()
	for _, f := range tbl.Fields {
		if f.SQLName == name {
			return f
		}
	}
	t.Fatalf("table %s has no field for column %q", tbl.StructName, name)
	return gen.APIFieldContext{}
}

// TestFilterProjection_perGoType pins the filter projection for every
// comparator-bearing column shape the generator can produce (PRD §26.5.3,
// one field map, two emitters).
//
// The load-bearing assertion is the pairing, not the individual values, and it
// is a biconditional: InputTypeName and TranslatorFunc are populated together
// or empty together. A populated translator must have a schema field to
// dispatch from, and a schema field must have a translator behind it, because
// the alternative is a filter argument the server accepts and ignores
// (PRD §26.5.3).
//
// The rows that reach wantTranslator:"" are therefore also wantInput:"" and
// wantFilterable:false: Enum / JSON / JSONB / Slice have real inputs, and the
// two residual shapes with no sound projection are dropped from the GraphQL
// surface entirely rather than left on the StringComparator fallback. Any row
// that flips is a behaviour change that must show up in the example goldens.
//
// Nullability is part of the projection: a nullable column references the
// `Nullable<X>Comparator` twin, not the base input (PRD §26.4 Rule 2).
func TestFilterProjection_perGoType(t *testing.T) {
	// Coverage note: these fixtures are PostgreSQL-only, matching the two
	// API-enabled example modules. The gotype resolver `apiTestInput` builds
	// is postgres-bound, so a MySQL/SQLite row would need its own resolver
	// as well as a dialect. Three column shapes therefore go unexercised by
	// both this table and the golden gate: a string-typed FK (`CHAR`/
	// `VARCHAR REFERENCES`, routine on MySQL/SQLite), `sql.NullInt64` /
	// `sql.NullFloat64`, and non-enum PostgreSQL arrays.
	tests := []struct {
		name   string
		column parser.Column
		// refTable prepends a `companies` table so an FKReference on the
		// column under test resolves.
		refTable bool
		// scalarOverrides binds uuid -> uuid.UUID and numeric ->
		// decimal.Decimal, the two integrations that change which comparator
		// family a column lands in.
		scalarOverrides bool
		wantFilterable  bool
		wantInput       string
		wantTranslator  string
	}{
		{
			name:           "text",
			column:         parser.Column{Name: "name", Type: "text"},
			wantFilterable: true,
			wantInput:      "StringComparator",
			wantTranslator: "translateStringComparator",
		},
		{
			name:           "nullable_text",
			column:         parser.Column{Name: "description", Type: "text", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableStringComparator",
			wantTranslator: "translateNullableStringComparator",
		},
		{
			name:           "integer",
			column:         parser.Column{Name: "stock", Type: "integer"},
			wantFilterable: true,
			wantInput:      "NumericComparator",
			wantTranslator: "translateNumericComparatorInt32",
		},
		{
			name:           "nullable_bigint",
			column:         parser.Column{Name: "views", Type: "bigint", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableNumericComparator",
			wantTranslator: "translateNullableNumericComparatorInt64",
		},
		{
			// numeric without the decimal integration resolves to float64,
			// which does satisfy comparator.Numeric.
			name:           "numeric_float",
			column:         parser.Column{Name: "weight", Type: "numeric"},
			wantFilterable: true,
			wantInput:      "NumericComparator",
			wantTranslator: "translateNumericComparatorFloat64",
		},
		{
			name:           "boolean",
			column:         parser.Column{Name: "active", Type: "boolean"},
			wantFilterable: true,
			wantInput:      "BooleanComparator",
			wantTranslator: "translateBooleanComparator",
		},
		{
			name:           "timestamp",
			column:         parser.Column{Name: "released_at", Type: "timestamp"},
			wantFilterable: true,
			wantInput:      "TimeComparator",
			wantTranslator: "translateTimeComparator",
		},
		{
			name:           "nullable_timestamp",
			column:         parser.Column{Name: "deleted_at", Type: "timestamp", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableTimeComparator",
			wantTranslator: "translateNullableTimeComparator",
		},
		{
			name: "uuid_foreign_key",
			column: parser.Column{
				Name: "company_id", Type: "uuid",
				FKReference: &parser.FKReference{Table: "companies", Column: "id"},
			},
			refTable:       true,
			wantFilterable: true,
			wantInput:      "IDComparator",
			wantTranslator: "translateIDComparator",
		},
		{
			// A bare uuid that is neither PK nor FK still filters as an
			// identifier once the uuid integration is bound —
			// resolveSimpleComparator keys that arm on the SQL type.
			name:            "uuid_scalar",
			column:          parser.Column{Name: "entity_id", Type: "uuid"},
			scalarOverrides: true,
			wantFilterable:  true,
			wantInput:       "IDComparator",
			wantTranslator:  "translateIDComparator",
		},
		{
			// Decimals filter by range: decimal.Decimal cannot satisfy
			// comparator.Numeric, so the model still filters through
			// comparator.String — but the GraphQL face is DecimalComparator
			// with Decimal-typed operands, which is what makes `{ gt: "100.50"
			// }` compare numerically rather than lexicographically (PRD §26.4
			// "Decimal columns").
			name:            "decimal",
			column:          parser.Column{Name: "price", Type: "numeric"},
			scalarOverrides: true,
			wantFilterable:  true,
			wantInput:       "DecimalComparator",
			wantTranslator:  "translateDecimalComparator",
		},
		{
			// `jsonb` resolves to comparator.JSONB only under the
			// postgres dialect, which is the whole of the PostgreSQL-only
			// gate — see the mysql/sqlite rows in
			// TestSliceJSONComparator_dialectGating.
			name:           "jsonb",
			column:         parser.Column{Name: "metadata", Type: "jsonb", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableJSONBComparator",
			wantTranslator: "translateNullableJSONBComparator",
		},
		{
			name:           "json",
			column:         parser.Column{Name: "settings", Type: "json", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableJSONComparator",
			wantTranslator: "translateNullableJSONComparator",
		},
		{
			name:           "text_array",
			column:         parser.Column{Name: "tags", Type: "text[]"},
			wantFilterable: true,
			wantInput:      "StringSliceComparator",
			wantTranslator: "translateStringSliceComparator",
		},
		{
			// The element's GraphQL type keys the input while its GO type
			// keys the translator, so `integer[]` and `bigint[]` share one
			// IntSliceComparator and split on the func name (PRD §26.4
			// "Sharing").
			name:           "integer_array",
			column:         parser.Column{Name: "scores", Type: "integer[]"},
			wantFilterable: true,
			wantInput:      "IntSliceComparator",
			wantTranslator: "translateIntSliceComparatorInt32",
		},
		{
			name:           "nullable_bigint_array",
			column:         parser.Column{Name: "bigs", Type: "bigint[]", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableIntSliceComparator",
			wantTranslator: "translateNullableIntSliceComparatorInt64",
		},
		{
			// `double precision` is gqlgen's own Float binding, so no width
			// conversion is needed and the func name carries no suffix.
			name:           "double_array",
			column:         parser.Column{Name: "weights", Type: "double precision[]"},
			wantFilterable: true,
			wantInput:      "FloatSliceComparator",
			wantTranslator: "translateFloatSliceComparator",
		},
		{
			name:           "real_array",
			column:         parser.Column{Name: "ratios", Type: "real[]"},
			wantFilterable: true,
			wantInput:      "FloatSliceComparator",
			wantTranslator: "translateFloatSliceComparatorFloat32",
		},
		{
			name:           "boolean_array",
			column:         parser.Column{Name: "flags", Type: "boolean[]"},
			wantFilterable: true,
			wantInput:      "BooleanSliceComparator",
			wantTranslator: "translateBooleanSliceComparator",
		},
		{
			// An array whose element binds to a CUSTOM scalar has no sound
			// projection: gqlgen's list-element pointer rule for those is not
			// one sqlgen has measured for this family (see
			// sliceOperandElemGoType). The column is dropped from BOTH GraphQL
			// surfaces rather than advertising the StringComparator fallback — a schema field the translator cannot dispatch is
			// exactly what §26.5.3 forbids. The model-side filter member is
			// unaffected; only the GraphQL face goes (PRD §26.12).
			name:            "timestamp_array",
			column:          parser.Column{Name: "seen_at", Type: "timestamptz[]"},
			scalarOverrides: true,
			wantFilterable:  false,
			wantInput:       "",
			wantTranslator:  "",
		},
		{
			// `jsonb[]` element types are not `comparable`, so the
			// column is dropped from the model filter. Both halves of the
			// projection must be empty — this is the one case where the
			// schema emits nothing either.
			name:           "jsonb_array",
			column:         parser.Column{Name: "payloads", Type: "jsonb[]"},
			wantFilterable: false,
			wantInput:      "",
			wantTranslator: "",
		},
		// Every one of these ten rows would read `StringComparator` /
		// `translateStringComparator` without opaque routing, so the generated
		// filter would compare a binary, network or duration column against a text
		// parameter. Measured consequences, all silent or fatal at runtime:
		// a base64 `Bytes` operand matched nothing on any dialect, a BLOB
		// column matched nothing at all on SQLite, `like` on inet / macaddr /
		// interval raised SQLSTATE 42883, and a negative Go duration string
		// was read by PostgreSQL with the sign applied to its leading field
		// only. Both nullabilities are present because Rule 2 gives each its
		// own input type and its own translator.
		{
			name:           "bytea",
			column:         parser.Column{Name: "payload", Type: "bytea"},
			wantFilterable: true,
			wantInput:      "BytesComparator",
			wantTranslator: "translateBytesComparator",
		},
		{
			name:           "nullable_bytea",
			column:         parser.Column{Name: "signature", Type: "bytea", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableBytesComparator",
			wantTranslator: "translateNullableBytesComparator",
		},
		{
			// time.Duration is ~int64, so the model comparator is the
			// ordinary Number[T] — only the GraphQL operand type splits it
			// out of NumericComparator (PRD §26.4 Rule 1).
			name:           "interval",
			column:         parser.Column{Name: "dur", Type: "interval"},
			wantFilterable: true,
			wantInput:      "DurationComparator",
			wantTranslator: "translateDurationComparator",
		},
		{
			name:           "nullable_interval",
			column:         parser.Column{Name: "dur_n", Type: "interval", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableDurationComparator",
			wantTranslator: "translateNullableDurationComparator",
		},
		{
			name:           "inet",
			column:         parser.Column{Name: "addr", Type: "inet"},
			wantFilterable: true,
			wantInput:      "IPComparator",
			wantTranslator: "translateIPComparator",
		},
		{
			name:           "nullable_inet",
			column:         parser.Column{Name: "addr_n", Type: "inet", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableIPComparator",
			wantTranslator: "translateNullableIPComparator",
		},
		{
			name:           "cidr",
			column:         parser.Column{Name: "net_block", Type: "cidr"},
			wantFilterable: true,
			wantInput:      "CIDRComparator",
			wantTranslator: "translateCIDRComparator",
		},
		{
			name:           "nullable_cidr",
			column:         parser.Column{Name: "net_block_n", Type: "cidr", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableCIDRComparator",
			wantTranslator: "translateNullableCIDRComparator",
		},
		{
			name:           "macaddr",
			column:         parser.Column{Name: "mac", Type: "macaddr"},
			wantFilterable: true,
			wantInput:      "MacAddrComparator",
			wantTranslator: "translateMacAddrComparator",
		},
		{
			name:           "nullable_macaddr",
			column:         parser.Column{Name: "mac_n", Type: "macaddr", Nullable: true},
			wantFilterable: true,
			wantInput:      "NullableMacAddrComparator",
			wantTranslator: "translateNullableMacAddrComparator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := []parser.Table{}
			if tt.refTable {
				tables = append(tables, parser.Table{
					Name:    "companies",
					Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
				})
			}
			tables = append(tables, parser.Table{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					tt.column,
				},
			})

			in := apiTestInput(t, &parser.Schema{Tables: tables})
			if tt.scalarOverrides {
				configureNullVariantOverrides(t, in)
			}
			apiCtx := buildProjectionAPIContext(t, in)
			f := fieldBySQLName(t, apiTableByName(t, apiCtx, "Product"), tt.column.Name)

			if f.Filterable != tt.wantFilterable {
				t.Errorf("Filterable = %v, want %v", f.Filterable, tt.wantFilterable)
			}
			if f.Filter.InputTypeName != tt.wantInput {
				t.Errorf("Filter.InputTypeName = %q, want %q", f.Filter.InputTypeName, tt.wantInput)
			}
			if f.Filter.TranslatorFunc != tt.wantTranslator {
				t.Errorf("Filter.TranslatorFunc = %q, want %q", f.Filter.TranslatorFunc, tt.wantTranslator)
			}

			// The one-field-map invariant, in its symmetric form: with the
			// StringComparator fallback gone, the pair is populated together or
			// empty together — never one without the other. That biconditional
			// is what makes ValidateAPIFilterCompleteness unable to fire on a
			// context this resolution produced.
			if (f.Filter.InputTypeName == "") != (f.Filter.TranslatorFunc == "") {
				t.Errorf("Filter projection is half-populated: input=%q translator=%q",
					f.Filter.InputTypeName, f.Filter.TranslatorFunc)
			}
			// A non-filterable column contributes to neither surface.
			if !f.Filterable && (f.Filter.InputTypeName != "" || f.Filter.TranslatorFunc != "") {
				t.Errorf("non-filterable column carries a projection: %+v", f.Filter)
			}
			// Every advertised input must be declared by the shared schema.
			if f.Filter.InputTypeName != "" && !hasComparatorFamily(apiCtx, f.Filter.InputTypeName) {
				t.Errorf("Filter.InputTypeName %q is not declared in ComparatorFamilies", f.Filter.InputTypeName)
			}
			// The translator descriptor must agree with the function name it
			// is carried alongside — buildAPIFilterFields registers the
			// descriptor and emits the name, so a mismatch would emit a call
			// to a function the file never declares.
			if f.Filter.TranslatorFunc != f.Filter.Translator.FuncName {
				t.Errorf("Filter.TranslatorFunc = %q but Translator.FuncName = %q",
					f.Filter.TranslatorFunc, f.Filter.Translator.FuncName)
			}
		})
	}
}

func apiTableByName(t *testing.T, ctx *gen.APIContext, structName string) gen.APITableContext {
	t.Helper()
	for _, tbl := range ctx.Tables {
		if tbl.StructName == structName {
			return tbl
		}
	}
	t.Fatalf("api context has no table %q", structName)
	return gen.APITableContext{}
}

func hasComparatorFamily(ctx *gen.APIContext, name string) bool {
	for _, fam := range ctx.ComparatorFamilies {
		if fam.Name == name {
			return true
		}
	}
	return false
}

// TestFilterProjection_pairsSchemaAndTranslator walks every column of a
// mixed schema and asserts the two emitters agree field-for-field: every
// comparator the schema advertises with a translator behind it has a
// matching FilterFields entry, and every FilterFields entry names the
// translator its column's projection resolved.
func TestFilterProjection_pairsSchemaAndTranslator(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "description", Type: "text", Nullable: true},
					{Name: "stock", Type: "integer"},
					{Name: "active", Type: "boolean"},
					{Name: "price", Type: "numeric"},
					{Name: "metadata", Type: "jsonb", Nullable: true},
					{Name: "tags", Type: "text[]"},
					{Name: "created_at", Type: "timestamp"},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	tbl := apiCtx.Tables[0]

	byTranslator := make(map[string]bool, len(tbl.FilterFields))
	for _, ff := range tbl.FilterFields {
		byTranslator[ff.TranslatorFunc] = true
	}

	for _, f := range tbl.Fields {
		if f.PrimaryKey || !f.Filterable {
			continue
		}
		if f.Filter.TranslatorFunc == "" {
			continue
		}
		if !byTranslator[f.Filter.TranslatorFunc] {
			t.Errorf("column %s projects translator %q but no FilterFields entry dispatches it",
				f.SQLName, f.Filter.TranslatorFunc)
		}
	}

	// Reverse direction: no FilterFields entry may name a translator no
	// non-PK column projected. PK columns are excluded from the filter
	// input, so their projections are discarded — counting them here would
	// let a stale entry that only a PK backs slip through.
	projected := make(map[string]bool, len(tbl.Fields))
	for _, f := range tbl.Fields {
		if f.PrimaryKey || !f.Filterable {
			continue
		}
		if f.Filter.TranslatorFunc != "" {
			projected[f.Filter.TranslatorFunc] = true
		}
	}
	for _, ff := range tbl.FilterFields {
		if !projected[ff.TranslatorFunc] {
			t.Errorf("FilterFields entry %s dispatches %q, which no column projected",
				ff.ModelFieldName, ff.TranslatorFunc)
		}
	}
}

// TestSortProjection_digitLeadingColumn is the sort-enum drift regression gate.
// The schema enum and the generated <table>SortFieldToColumn switch used to
// derive the enum value independently — screamingSnakeCase (which applies the
// PRD §8.5 `col_` guard) on the schema side, a bare
// strings.ToUpper(toSnakeCase(...)) on the translator side. A column named
// `2010_revenue` therefore advertised COL_2010_REVENUE and switched on
// "2010_REVENUE", so the lookup returned "" and the sort was silently dropped.
//
// Both now read APIFieldContext.SortEnumValue. This test fails against the
// unguarded translator spelling on the `case` assertion.
func TestSortProjection_digitLeadingColumn(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "reports",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "2010_revenue", Type: "numeric"},
					{Name: "created_at", Type: "timestamp"},
				},
			},
		},
	}
	apiCtx := projectionAPIContext(t, schema)
	tbl := apiCtx.Tables[0]

	f := fieldBySQLName(t, tbl, "2010_revenue")
	const want = "COL_2010_REVENUE"
	if f.SortEnumValue != want {
		t.Errorf("SortEnumValue = %q, want %q", f.SortEnumValue, want)
	}

	// The sort-translator binding reads the same field.
	found := false
	for _, sf := range tbl.SortFields {
		if sf.SQLColumn != "2010_revenue" {
			continue
		}
		found = true
		if sf.EnumValue != want {
			t.Errorf("SortFields EnumValue = %q, want %q", sf.EnumValue, want)
		}
	}
	if !found {
		t.Fatal("SortFields has no entry for 2010_revenue")
	}

	// End to end: the enum value the schema emits must be the literal the
	// generated switch matches on.
	apiCtx.ModelsPackage = "models"
	schemaOut := renderAPITableSchema(t, tbl)
	sortOut := renderSortTranslate(t, apiCtx)

	if !strings.Contains(schemaOut, "\n  "+want+"\n") {
		t.Errorf("schema enum does not contain %q:\n%s", want, schemaOut)
	}
	if !strings.Contains(sortOut, "\tcase \""+want+"\":") {
		t.Errorf("sort translator does not switch on %q:\n%s", want, sortOut)
	}
	// The unprefixed spelling is what the translator used to emit; it must
	// appear nowhere.
	if strings.Contains(sortOut, "\"2010_REVENUE\"") {
		t.Errorf("sort translator still emits the unprefixed enum value:\n%s", sortOut)
	}
}

// TestSortProjection_everySortFieldMatchesSchemaEnum asserts the two sort
// emitters agree for an ordinary schema too, not just the digit-leading
// edge case.
func TestSortProjection_everySortFieldMatchesSchemaEnum(t *testing.T) {
	apiCtx := projectionAPIContext(t, apiTestSchema())
	tbl := apiCtx.Tables[0]
	out := renderAPITableSchema(t, tbl)

	if len(tbl.SortFields) == 0 {
		t.Fatal("table has no sort fields")
	}
	for _, sf := range tbl.SortFields {
		if !strings.Contains(out, "\n  "+sf.EnumValue+"\n") {
			t.Errorf("sort field %q (column %s) is not emitted in the schema enum:\n%s",
				sf.EnumValue, sf.SQLColumn, out)
		}
	}
}

// TestSharedSchema_comparatorFamiliesAreUsageDerived asserts the emission rule
// for the comparator surface; the byte-level regression pin lives in the
// example goldens. The rule: families appear because a column references
// them, in shipped-table order, each followed by its range input.
func TestSharedSchema_comparatorFamiliesAreUsageDerived(t *testing.T) {
	apiCtx := projectionAPIContext(t, apiTestSchema())

	// apiTestSchema has a text, a nullable text, an integer, a nullable
	// jsonb, a numeric and a timestamp — but no boolean and no non-PK ID
	// column. NullableJSONBComparator is the shipped table's last entry, so
	// it also shows the family order is the table's and not the schema's.
	want := []string{
		"StringComparator",
		"NullableStringComparator",
		"NumericComparator",
		"TimeComparator",
		"NullableJSONBComparator",
	}
	got := make([]string, 0, len(apiCtx.ComparatorFamilies))
	for _, fam := range apiCtx.ComparatorFamilies {
		got = append(got, fam.Name)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ComparatorFamilies mismatch (-want +got):\n%s", diff)
	}

	out := renderAPISharedSchema(t, apiCtx)
	// Range inputs ride along with their family and are declared once.
	mustContain(t, out, "input NumericRange {")
	mustContain(t, out, "input TimeRange {")
	if strings.Count(out, "input NumericRange {") != 1 {
		t.Errorf("NumericRange declared more than once:\n%s", out)
	}
	// Every input a per-table schema references must be declared here.
	for _, tbl := range apiCtx.Tables {
		for _, f := range tbl.Fields {
			if f.PrimaryKey || !f.Filterable {
				continue
			}
			if !strings.Contains(out, "input "+f.Filter.InputTypeName+" {") {
				t.Errorf("column %s references undeclared input %s:\n%s",
					f.SQLName, f.Filter.InputTypeName, out)
			}
		}
	}
}
