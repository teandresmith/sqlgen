package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// lintFixture returns a source entity and the API context built from it that
// AGREE on both surfaces — the baseline every case below perturbs in exactly
// one place, so a failure names the perturbation rather than the fixture.
//
// The shape mirrors what buildAPITableContext actually produces: a PK column
// excluded from the filter input but present in the sort enum, two filterable
// columns carrying a populated projection, and one relationship member.
func lintFixture() (gen.APIEntity, gen.APITableContext) {
	src := gen.APIEntity{
		Kind:       "table",
		Name:       "products",
		StructName: "Product",
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", PrimaryKey: true},
			{Name: "name", FieldName: "Name"},
			{Name: "stock", FieldName: "Stock"},
		},
		PKColumns: []gen.ColumnContext{{Name: "id", FieldName: "ID", PrimaryKey: true}},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "Name", ColumnName: "name", Filterable: true},
			{FieldName: "Stock", ColumnName: "stock", Filterable: true},
		},
		RelationshipFilters: []gen.RelationshipFilterContext{
			{FieldName: "OrderItems", SQLName: "order_items", TargetStructName: "OrderItem", TargetTable: "order_items"},
		},
	}
	at := gen.APITableContext{
		StructName: "Product",
		SQLTable:   "products",
		Fields: []gen.APIFieldContext{
			{
				SQLName: "id", GraphQLName: "id", GoFieldName: "ID",
				PrimaryKey: true, Readable: true, Sortable: true, SortEnumValue: "ID",
			},
			{
				SQLName: "name", GraphQLName: "name", GoFieldName: "Name",
				Readable: true, Filterable: true, Sortable: true, SortEnumValue: "NAME",
				Filter: gen.APIFilterProjection{
					InputTypeName:  "StringComparator",
					TranslatorFunc: "translateStringComparator",
				},
			},
			{
				SQLName: "stock", GraphQLName: "stock", GoFieldName: "Stock",
				Readable: true, Filterable: true, Sortable: true, SortEnumValue: "STOCK",
				Filter: gen.APIFilterProjection{
					InputTypeName:  "NumericComparator",
					TranslatorFunc: "translateNumericComparatorInt32",
				},
			},
		},
		FilterFields: []gen.APIFilterField{
			{SQLName: "name", GraphQLName: "name", GoFieldName: "Name", ModelFieldName: "Name", TranslatorFunc: "translateStringComparator"},
			{SQLName: "stock", GraphQLName: "stock", GoFieldName: "Stock", ModelFieldName: "Stock", TranslatorFunc: "translateNumericComparatorInt32"},
		},
		FilterRelationships: []gen.APIFilterRelationship{
			{
				GraphQLName: "orderItems", GoFieldName: "OrderItems", ModelFieldName: "OrderItems",
				InputTypeName: "OrderItemFilter", TranslatorFunc: "translateOrderItemFilter",
			},
		},
		SortFields: []gen.APISortField{
			{EnumValue: "ID", SQLColumn: "id"},
			{EnumValue: "NAME", SQLColumn: "name"},
			{EnumValue: "STOCK", SQLColumn: "stock"},
		},
	}
	return src, at
}

// requireLintError asserts the lint fired and that its message carries every
// token the acceptance criteria demand: the entity, the offending column, and
// the PRD reference that explains the rule.
func requireLintError(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the lint to reject this context, got nil")
	}
	msg := err.Error()
	for _, tok := range want {
		if !strings.Contains(msg, tok) {
			t.Errorf("lint message must mention %q; got %q", tok, msg)
		}
	}
}

// TestAPIFilterCompleteness_BaselinePasses pins that the fixture the negative
// cases perturb is itself clean, so every failure below is attributable.
func TestAPIFilterCompleteness_BaselinePasses(t *testing.T) {
	src, at := lintFixture()
	if err := gen.ValidateAPIFilterCompleteness(src, at); err != nil {
		t.Fatalf("baseline fixture must pass the filter lint: %v", err)
	}
	if err := gen.ValidateAPISortCompleteness(src, at); err != nil {
		t.Fatalf("baseline fixture must pass the sort lint: %v", err)
	}
}

// TestAPIFilterCompleteness_SchemaFieldWithoutTranslatorEntry is the
// advertised-but-dropped filter failure: `input ProductFilter` advertises
// `stock` and the generated translator has no branch for it, so `{stock: {gt:
// 5}}` parses, is discarded, and the server answers with every row. PRD §26.5.3
// makes that unlandable.
func TestAPIFilterCompleteness_SchemaFieldWithoutTranslatorEntry(t *testing.T) {
	src, at := lintFixture()
	at.FilterFields = at.FilterFields[:1] // drop the "stock" translator entry

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "stock", "§26.5.3")
}

// TestAPIFilterCompleteness_TranslatorEntryWithoutSchemaField is the mirror
// direction. A stale entry makes the generated body read `in.Stock` off a
// gqlgen input type whose schema never declared the field, so the graph
// package does not compile.
func TestAPIFilterCompleteness_TranslatorEntryWithoutSchemaField(t *testing.T) {
	src, at := lintFixture()
	// The schema side drops the column (as §32.2 or a non-projectable
	// comparator would) while the translator keeps its entry.
	at.Fields[2].Filterable = false
	at.Fields[2].Filter = gen.APIFilterProjection{}

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "stock", "§26.5.3")
}

// TestAPISortCompleteness_EnumValueWithoutCase is sort-enum drift in its
// general form: the enum value parses, the switch falls through to its default,
// and the read comes back in whatever order the database chose.
func TestAPISortCompleteness_EnumValueWithoutCase(t *testing.T) {
	src, at := lintFixture()
	at.SortFields = at.SortFields[:2] // drop the STOCK case

	requireLintError(t, gen.ValidateAPISortCompleteness(src, at),
		"products", "STOCK", "stock", "§26.5.3")
}

// TestAPISortCompleteness_DigitLeadingColumnDrift is sort-enum drift exactly as
// it was found: `screamingSnakeCase` guards a digit-leading column with the
// §8.5 `col_` prefix and the deleted second derivation did not, so the schema
// advertised COL_2010_REVENUE while the switch cased on 2010_REVENUE. Both
// directions of the lint fire on it — an unmatched enum value AND an
// unreachable case.
func TestAPISortCompleteness_DigitLeadingColumnDrift(t *testing.T) {
	src := gen.APIEntity{
		Kind: "table", Name: "reports", StructName: "Report",
		Columns: []gen.ColumnContext{{Name: "2010_revenue", FieldName: "Col2010Revenue"}},
	}
	at := gen.APITableContext{
		StructName: "Report",
		Fields: []gen.APIFieldContext{{
			SQLName: "2010_revenue", GraphQLName: "col2010Revenue",
			Readable: true, Sortable: true, SortEnumValue: "COL_2010_REVENUE",
		}},
		// The unguarded spelling: strings.ToUpper(toSnakeCase(...)), no guard.
		SortFields: []gen.APISortField{{EnumValue: "2010_REVENUE", SQLColumn: "2010_revenue"}},
	}

	err := gen.ValidateAPISortCompleteness(src, at)
	requireLintError(t, err, "COL_2010_REVENUE", "2010_REVENUE", "§26.5.3")
	if got := strings.Count(err.Error(), "\n") + 1; got != 2 {
		t.Errorf("drift must be reported from both directions, got %d error(s): %v", got, err)
	}
}

// TestAPISortCompleteness_CaseWithoutEnumValue is the reverse direction: a
// switch case nothing can select, because the enum never offers the value.
func TestAPISortCompleteness_CaseWithoutEnumValue(t *testing.T) {
	src, at := lintFixture()
	at.SortFields = append(at.SortFields, gen.APISortField{EnumValue: "PRICE", SQLColumn: "price"})

	requireLintError(t, gen.ValidateAPISortCompleteness(src, at),
		"products", "PRICE", "§26.5.3")
}

// TestAPISortCompleteness_CaseReturnsUnknownColumn covers the one sort failure
// that is neither a parse error nor a compile error: the switch returns a name
// that goes straight into ORDER BY, so a stale column fails every sorted read
// at the database.
func TestAPISortCompleteness_CaseReturnsUnknownColumn(t *testing.T) {
	src, at := lintFixture()
	at.SortFields[2].SQLColumn = "stock_level" // renamed out from under the case

	requireLintError(t, gen.ValidateAPISortCompleteness(src, at),
		"products", "STOCK", "stock_level")
}

// TestAPISortCompleteness_SortableColumnWithoutEnumValue guards the third sort
// shape: a column gated INTO the enum block that resolved no value renders a
// blank line where a value belongs, which gqlgen rejects outright.
func TestAPISortCompleteness_SortableColumnWithoutEnumValue(t *testing.T) {
	src, at := lintFixture()
	at.Fields[2].SortEnumValue = ""
	at.SortFields = at.SortFields[:2]

	requireLintError(t, gen.ValidateAPISortCompleteness(src, at),
		"products", "stock", "§8.5")
}

// TestAPIFilterCompleteness_FilterableColumnWithoutInputType is the filter
// twin of the empty-sort-enum guard: a column gated INTO the `input <T>Filter`
// block with no comparator input resolved renders `name: `, which gqlgen
// rejects outright. Unreachable while APIFieldContext.Filterable is read back
// off the projection — which is exactly the fact this asserts.
func TestAPIFilterCompleteness_FilterableColumnWithoutInputType(t *testing.T) {
	src, at := lintFixture()
	at.Fields[2].Filter.InputTypeName = ""

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "stock", "§26.5.3")
}

// TestAPIFilterCompleteness_RelationshipMemberHalfRendered covers the
// relationship arm's silent-drop shape. `FilterRelationships` is one slice read
// by both emitters, so the two cannot name different members — but an entry
// missing its translator half still renders a schema field with nothing behind
// it, which is the same failure through a different door.
func TestAPIFilterCompleteness_RelationshipMemberHalfRendered(t *testing.T) {
	src, at := lintFixture()
	at.FilterRelationships[0].TranslatorFunc = ""

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "orderItems", "§26.5.3")
}

// TestAPIFilterCompleteness_RelationshipMemberNotOnModelFilter covers the
// direction the column half gets for free: the emitted member assigns
// `out.OrderItems`, so the model `<T>Filter` must have that member or the
// generated translator does not compile.
func TestAPIFilterCompleteness_RelationshipMemberNotOnModelFilter(t *testing.T) {
	src, at := lintFixture()
	src.RelationshipFilters = nil

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "orderItems", "OrderItems", "§11.1")
}

// TestAPIFilterCompleteness_RelationshipMemberCollidesWithColumn pins the
// "the column wins" tiebreak buildAPIFilterRelationships applies. A column
// field and a relationship member land in the same `input <T>Filter` block, so
// a shared name makes the whole document invalid — and gqlgen reports it a long
// way from the schema line that caused it.
func TestAPIFilterCompleteness_RelationshipMemberCollidesWithColumn(t *testing.T) {
	src, at := lintFixture()
	at.FilterRelationships[0].GraphQLName = "name"

	requireLintError(t, gen.ValidateAPIFilterCompleteness(src, at),
		"products", "name", "§26.4")
}

// TestAPILintCompleteness_NonFilterableColumnPasses is the non-filterable
// negative test. A `jsonb[]` column resolves to `[]types.JSON`, which cannot satisfy the
// comparator package's `T comparable` constraint, so it is dropped from the
// model filter, the GraphQL input, and the translator alike. PRD §26.5.3
// requires it to satisfy the lint by that absence rather than by an exemption —
// so the lint has no jsonb arm, and this test is what proves none is needed.
//
// It stays sortable: §32.2 is what removes a column from the sort surface, and
// nothing about comparability does.
func TestAPILintCompleteness_NonFilterableColumnPasses(t *testing.T) {
	src, at := lintFixture()
	src.Columns = append(src.Columns, gen.ColumnContext{Name: "payloads", FieldName: "Payloads"})
	src.FilterFields = append(src.FilterFields,
		gen.FilterFieldContext{FieldName: "Payloads", ColumnName: "payloads", Filterable: false})
	at.Fields = append(at.Fields, gen.APIFieldContext{
		SQLName: "payloads", GraphQLName: "payloads", GoFieldName: "Payloads",
		Readable: true, Sortable: true, SortEnumValue: "PAYLOADS",
		Filterable: false, Filter: gen.APIFilterProjection{},
	})
	at.SortFields = append(at.SortFields, gen.APISortField{EnumValue: "PAYLOADS", SQLColumn: "payloads"})

	if err := gen.ValidateAPIFilterCompleteness(src, at); err != nil {
		t.Errorf("a jsonb[] column absent from both filter surfaces must pass: %v", err)
	}
	if err := gen.ValidateAPISortCompleteness(src, at); err != nil {
		t.Errorf("a jsonb[] column is still sortable and must pass: %v", err)
	}
}

// TestAPILintCompleteness_AccessRestrictedColumnPasses is the §32.2 negative
// test. A `hidden` / `internal` column is dropped from every API surface at
// once — object type, filter input, sort enum, walker — so both lints must be
// silent about it rather than demanding an entry it deliberately lacks.
func TestAPILintCompleteness_AccessRestrictedColumnPasses(t *testing.T) {
	src, at := lintFixture()
	src.Columns = append(src.Columns, gen.ColumnContext{Name: "password_hash", FieldName: "PasswordHash"})
	src.FilterFields = append(src.FilterFields,
		gen.FilterFieldContext{FieldName: "PasswordHash", ColumnName: "password_hash", Filterable: true})
	// Every API capability false: readable, writable, filterable, sortable.
	at.Fields = append(at.Fields, gen.APIFieldContext{
		SQLName: "password_hash", GraphQLName: "passwordHash", GoFieldName: "PasswordHash",
	})

	if err := gen.ValidateAPIFilterCompleteness(src, at); err != nil {
		t.Errorf("an access-restricted column must pass the filter lint: %v", err)
	}
	if err := gen.ValidateAPISortCompleteness(src, at); err != nil {
		t.Errorf("an access-restricted column must pass the sort lint: %v", err)
	}
}

// TestAPILintCompleteness_ViewRoutesThroughTheSameLints proves the two lints
// have no view arm: a ViewContext projected through the real
// apiEntityFromView constructor is linted by the identical code path a table
// is (PRD §16.4). A view carries no relationship filters, so the
// relationship loop has nothing to check — which is the correct outcome and
// not an exemption.
func TestAPILintCompleteness_ViewRoutesThroughTheSameLints(t *testing.T) {
	ent := gen.APIEntityFromView(gen.ViewContext{
		ViewName:   "product_summary",
		StructName: "ProductSummary",
		Columns: []gen.ColumnContext{
			{Name: "product_id", FieldName: "ProductID"},
			{Name: "total", FieldName: "Total"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "Total", ColumnName: "total", Filterable: true},
		},
	})
	if ent.Kind != "view" {
		t.Fatalf("APIEntityFromView must carry the view kind, got %q", ent.Kind)
	}
	at := gen.APITableContext{
		StructName: "ProductSummary", SQLTable: "product_summary", IsView: true,
		Fields: []gen.APIFieldContext{
			{SQLName: "product_id", GraphQLName: "productID", Readable: true, Sortable: true, SortEnumValue: "PRODUCT_ID"},
			{
				SQLName: "total", GraphQLName: "total", Readable: true, Sortable: true, SortEnumValue: "TOTAL",
				Filterable: true,
				Filter: gen.APIFilterProjection{
					InputTypeName:  "NumericComparator",
					TranslatorFunc: "translateNumericComparatorInt64",
				},
			},
		},
		FilterFields: []gen.APIFilterField{
			{SQLName: "total", GraphQLName: "total", GoFieldName: "Total", ModelFieldName: "Total", TranslatorFunc: "translateNumericComparatorInt64"},
		},
		SortFields: []gen.APISortField{
			{EnumValue: "PRODUCT_ID", SQLColumn: "product_id"},
			{EnumValue: "TOTAL", SQLColumn: "total"},
		},
	}

	if err := gen.ValidateAPIFilterCompleteness(ent, at); err != nil {
		t.Errorf("a consistent view context must pass the filter lint: %v", err)
	}
	if err := gen.ValidateAPISortCompleteness(ent, at); err != nil {
		t.Errorf("a consistent view context must pass the sort lint: %v", err)
	}

	// And the message names the entity in the consumer's own words.
	at.FilterFields = nil
	requireLintError(t, gen.ValidateAPIFilterCompleteness(ent, at),
		"view", "product_summary", "total")
}

// TestAPILintCompleteness_BuiltContextsPassBothLints runs the two lints over
// contexts BuildAPIContext actually produced, rather than over hand-built ones.
// The fixture deliberately covers every shape that could make a projection
// asymmetric: a comparator with no sound GraphQL face (`timestamptz[]`), a
// non-comparable array (`jsonb[]`), a §32.2 hidden column, a decimal
// (whose input type and model comparator deliberately disagree), an enum, and
// a relationship filter member.
//
// The lints are wired into buildOneAPITable / buildOneAPIView, so BuildAPIContext
// returning without error is already the assertion; re-running them here states
// it explicitly and would survive an accidental unwiring of the call sites.
func TestAPILintCompleteness_BuiltContextsPassBothLints(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "categories",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "stock", Type: "integer"},
					// No sound projection — dropped from both GraphQL surfaces.
					{Name: "seen_at", Type: "timestamptz[]"},
					// Non-comparable — dropped from the model filter too.
					{Name: "payloads", Type: "jsonb[]"},
					// §32.2 — dropped from every API surface.
					{Name: "internal_score", Type: "integer"},
					{
						Name: "category_id", Type: "bigint",
						FKReference: &parser.FKReference{Table: "categories", Column: "id"},
					},
				},
			},
		},
	}

	in := apiTestInput(t, schema)
	configureNullVariantOverrides(t, in)
	in.Config.Tables["products"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"internal_score": {Access: "internal"},
		},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	byStruct := map[string]gen.APIEntity{}
	for _, tc := range tables {
		byStruct[tc.StructName] = gen.APIEntityFromTable(tc)
	}
	for _, at := range apiCtx.Tables {
		ent, ok := byStruct[at.StructName]
		if !ok {
			t.Fatalf("no source entity for API table %q", at.StructName)
		}
		if err := gen.ValidateAPIFilterCompleteness(ent, at); err != nil {
			t.Errorf("%s: filter lint: %v", at.StructName, err)
		}
		if err := gen.ValidateAPISortCompleteness(ent, at); err != nil {
			t.Errorf("%s: sort lint: %v", at.StructName, err)
		}
	}

	// The two columns with no GraphQL filter face must be absent from BOTH
	// sides — the shape §26.5.3 requires, and the reason the lints above are
	// silent about them.
	products := apiTableByName(t, apiCtx, "Product")
	for _, col := range []string{"seen_at", "payloads"} {
		f := fieldBySQLName(t, products, col)
		if f.Filterable || f.Filter.InputTypeName != "" || f.Filter.TranslatorFunc != "" {
			t.Errorf("column %q must carry no filter projection, got %+v", col, f.Filter)
		}
		for _, ff := range products.FilterFields {
			if ff.SQLName == col {
				t.Errorf("column %q must have no translator entry", col)
			}
		}
	}
}
