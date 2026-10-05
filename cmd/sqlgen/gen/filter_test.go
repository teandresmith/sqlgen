package gen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadFilterTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("filter").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	return tmpl
}

func executeFilterTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadFilterTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/filter", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

func testFilterTableContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		CompositePK:       false,
		SoftDelete:        &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
		Columns: []gen.ColumnContext{
			{FieldName: "ID", Name: "id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "id", JSONTag: "id"},
			{FieldName: "Name", Name: "name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{FieldName: "Price", Name: "price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{FieldName: "CreatedAt", Name: "created_at", GoType: "time.Time", DBTag: "created_at", JSONTag: "created_at"},
			{FieldName: "DeletedAt", Name: "deleted_at", GoType: "*time.Time", Nullable: true, DBTag: "deleted_at", JSONTag: "deleted_at"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "CreatedAt", ComparatorType: "*comparator.Time", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "created_at", Filterable: true},
			{FieldName: "DeletedAt", ComparatorType: "*comparator.NullableTime", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "deleted_at", IsSoftDeleteColumn: true, Filterable: true},
			{FieldName: "ID", ComparatorType: "*comparator.ID", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "id", Filterable: true},
			{FieldName: "Name", ComparatorType: "*comparator.String", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "name", Filterable: true},
			{FieldName: "Price", ComparatorType: "*comparator.Number[float64]", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "price", Filterable: true},
		},
	}
}

func testFilterCompositePKContext() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Package:               "db",
		Dialect:               "postgres",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Columns: []gen.ColumnContext{
			{FieldName: "OrderID", Name: "order_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "order_id", JSONTag: "order_id"},
			{FieldName: "ProductID", Name: "product_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "product_id", JSONTag: "product_id"},
			{FieldName: "Quantity", Name: "quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		PKColumns: []gen.ColumnContext{
			{FieldName: "OrderID", Name: "order_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "order_id", JSONTag: "order_id"},
			{FieldName: "ProductID", Name: "product_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "product_id", JSONTag: "product_id"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "OrderID", ComparatorType: "*comparator.ID", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "order_id", Filterable: true},
			{FieldName: "ProductID", ComparatorType: "*comparator.ID", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "product_id", Filterable: true},
			{FieldName: "Quantity", ComparatorType: "*comparator.Number[int32]", ComparatorImport: "github.com/teandresmith/sqlgen/comparator", ColumnName: "quantity", Filterable: true},
		},
	}
}

func TestFilterTemplate_correctComparatorTypes(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	wantPatterns := []string{
		"type ProductFilter struct {",
		"CreatedAt *comparator.Time",
		`json:"created_at"`,
		"DeletedAt *comparator.NullableTime",
		`json:"deleted_at"`,
		"ID *comparator.ID",
		`json:"id"`,
		"Name *comparator.String",
		`json:"name"`,
		"Price *comparator.Number[float64]",
		`json:"price"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFilterTemplate_andOrFields(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	wantPatterns := []string{
		`And []*ProductFilter ` + "`" + `json:"and"` + "`",
		`Or  []*ProductFilter ` + "`" + `json:"or"` + "`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFilterTemplate_toConditionsPerFieldNilCheck(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	wantPatterns := []string{
		"func (f *ProductFilter) ToConditions(dialect sql.Dialect) []sql.Condition {",
		`if f.ID != nil {`,
		`f.ID.Parse(dialect.QuoteIdentifier("id"), dialect)`,
		`if f.Name != nil {`,
		`f.Name.Parse(dialect.QuoteIdentifier("name"), dialect)`,
		`if f.Price != nil {`,
		`f.Price.Parse(dialect.QuoteIdentifier("price"), dialect)`,
		`if f.CreatedAt != nil {`,
		`f.CreatedAt.Parse(dialect.QuoteIdentifier("created_at"), dialect)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// The comparator writes its column token verbatim, so a raw name here is
	// an unquoted identifier in the WHERE clause.
	for _, raw := range []string{`.Parse("id", dialect)`, `.Parse("created_at", dialect)`} {
		if strings.Contains(output, raw) {
			t.Errorf("output passes an unquoted column to a comparator: %q", raw)
		}
	}
}

// TestFilterTemplate_toConditionsNilReceiverGuard pins the guard that makes a
// nil filter reach ErrEmptyFilter on the *Where methods instead of
// dereferencing nil. Those methods dereference their filter only in this call,
// so the guard has to be here rather than at each call site; the behavior half
// lives in the postgres example's nil_filter_test.go.
func TestFilterTemplate_toConditionsNilReceiverGuard(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	want := "func (f *ProductFilter) ToConditions(dialect sql.Dialect) []sql.Condition {\n\tif f == nil {\n\t\treturn nil\n\t}"
	if !strings.Contains(output, want) {
		t.Errorf("output missing the nil-receiver guard as the first statement:\n\nfull output:\n%s", output)
	}
}

// TestFilterTemplate_andOrRecursion pins the composition shape ToConditions
// emits (PRD §11.1). A filter object's own fields are always conjunctive; only
// membership in the Or list is disjunctive. So each Or member is wrapped in
// sql.And of its OWN conditions, and the members are OR'd together as ONE group
// condition appended to conds.
//
// The negative assertion is the point of the test. Appending a per-member
// sql.Or straight onto conds — the spelling this replaced — hands the caller N
// conditions, and buildWhere joins those with AND: `Or: [A, B]` compiled to
// `A AND B`, and a lone member's own fields were OR'd against each other.
// Both readings are wrong, and both are invisible in a test that only checks
// that the identifiers `f.Or` and `sql.Or` appear somewhere in the output.
func TestFilterTemplate_andOrRecursion(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	wantPatterns := []string{
		"for _, sub := range f.And {",
		"sub.ToConditions(dialect)",
		"conds = append(conds, sql.And(subConds...))",
		"var orGroup []sql.Condition",
		"for _, sub := range f.Or {",
		"orGroup = append(orGroup, sql.And(subConds...))",
		"if len(orGroup) > 0 {",
		"conds = append(conds, sql.Or(orGroup...))",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	if strings.Contains(output, "conds = append(conds, sql.Or(subConds...))") {
		t.Errorf("each Or member is still wrapped in its own sql.Or, which the caller ANDs\n\nfull output:\n%s", output)
	}
}

func TestFilterTemplate_softDeleteAsRegularComparator(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	// Soft delete column should be a regular comparator field.
	if !strings.Contains(output, "DeletedAt *comparator.NullableTime") {
		t.Error("output missing DeletedAt as regular comparator field")
	}

	// No IncludeDeleted or IsSoftDeleted fields — the model filter delegates
	// soft-delete inclusion to the column comparator itself; the GraphQL
	// `includeDeleted: Boolean` translates to setting `out.DeletedAt = &{}`
	// in the API filter translator (graph package), not a model-side field.
	if strings.Contains(output, "IncludeDeleted") {
		t.Error("output should not contain IncludeDeleted")
	}
	if strings.Contains(output, "IsSoftDeleted") {
		t.Error("output should not contain IsSoftDeleted")
	}

	// Soft delete column gets a nil-check in ToConditions like any other field.
	if !strings.Contains(output, `if f.DeletedAt != nil {`) {
		t.Error("output missing nil-check for DeletedAt in ToConditions")
	}
	if !strings.Contains(output, `f.DeletedAt.Parse(dialect.QuoteIdentifier("deleted_at"), dialect)`) {
		t.Error("output missing Parse call for DeletedAt in ToConditions")
	}
}

func TestFilterTemplate_compositePK_PKsField(t *testing.T) {
	ctx := testFilterCompositePKContext()
	output := executeFilterTemplate(t, ctx)

	if !strings.Contains(output, "PKs []OrderItemPK `json:\"pks\"`") {
		t.Errorf("output missing PKs field\n\nfull output:\n%s", output)
	}
}

func TestFilterTemplate_compositePK_toConditions(t *testing.T) {
	ctx := testFilterCompositePKContext()
	output := executeFilterTemplate(t, ctx)

	wantPatterns := []string{
		"if len(f.PKs) > 0 {",
		"for _, pk := range f.PKs {",
		"sql.And(",
		`Value: pk.OrderID`,
		`Value: pk.ProductID`,
		"sql.Or(pkConds...)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFilterTemplate_noPKsFieldWhenSinglePK(t *testing.T) {
	ctx := testFilterTableContext()
	output := executeFilterTemplate(t, ctx)

	if strings.Contains(output, "PKs [") {
		t.Error("output should not contain PKs field for single PK table")
	}
}

func TestFilterTemplate_compilesCleanly(t *testing.T) {
	ctx := testFilterTableContext()
	output := wrapFilterForCompile(ctx, executeFilterTemplate(t, ctx))

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "filter_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestFilterTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testFilterCompositePKContext()
	output := wrapFilterForCompile(ctx, executeFilterTemplate(t, ctx))

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "filter_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestFilterTemplate_goldenFile(t *testing.T) {
	ctx := testFilterTableContext()
	output := wrapFilterForCompile(ctx, executeFilterTemplate(t, ctx))

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "filter_products_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "filter_products_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("filter_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestFilterTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testFilterCompositePKContext()
	output := wrapFilterForCompile(ctx, executeFilterTemplate(t, ctx))

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "filter_order_items_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "filter_order_items_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("filter_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// wrapFilterForCompile wraps the filter template output in a compilable Go file.
func wrapFilterForCompile(ctx gen.TableContext, fragment string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", ctx.Package)
	b.WriteString("import (\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/comparator\"\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/sql\"\n")
	if ctx.CompositePK {
		b.WriteString("\t\"github.com/gofrs/uuid/v5\"\n")
	}
	b.WriteString(")\n")
	if ctx.CompositePK {
		fmt.Fprintf(&b, "\ntype %s struct {\n", ctx.CompositePKStructName)
		for _, pk := range ctx.PKColumns {
			fmt.Fprintf(&b, "\t%s %s `db:%q json:%q`\n", pk.FieldName, pk.GoType, pk.DBTag, pk.JSONTag)
		}
		b.WriteString("}\n")
	}
	b.WriteString(fragment)
	return b.String()
}
