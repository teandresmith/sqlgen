package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/sql"
)

// --- Template loaders ---

func loadViewModelTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("model.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "model.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view model template: %v", err)
	}
	return tmpl
}

func loadViewClientTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("client.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "client.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view client template: %v", err)
	}
	return tmpl
}

func loadViewGetTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("get.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "get.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view get template: %v", err)
	}
	return tmpl
}

func loadViewCountTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("count.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "count.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view count template: %v", err)
	}
	return tmpl
}

func loadViewPaginationTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("pagination.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "pagination.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view pagination template: %v", err)
	}
	return tmpl
}

// --- Template executors ---

func executeViewModelTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewModelTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/model", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func executeViewClientTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewClientTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/client", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func executeViewGetTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewGetTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/get", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func executeViewCountTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewCountTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/count", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func executeViewPaginationTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewPaginationTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/pagination", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testViewContext_withPK returns a view context with @pk annotation,
// @type override (avg_rating as float64), schema-matched columns, and aggregate-inferred types.
func testViewContext_withPK() gen.ViewContext {
	return gen.ViewContext{
		StructName:        "ProductSummary",
		ViewName:          "product_summary",
		TableName:         "product_summary",
		TableNameConstant: "TableProductSummaries",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		HasPK:             true,
		Dialect:           "postgres",
		Driver:            "pgx",
		PageSize:          100,
		QueryLimit:        1000,
		CursorKeys:        []string{"id"},
		HasConnection:     true,
		Imports: []string{
			"context",
			"encoding/base64",
			"encoding/json",
			"fmt",
			"slices",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/hook",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "avg_rating", FieldName: "AvgRating", GoType: "float64", DBTag: "avg_rating", JSONTag: "avg_rating", Description: "Average product rating."},
			{Name: "discount", FieldName: "Discount", GoType: "*float64", DBTag: "discount", JSONTag: "discount", Nullable: true},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "review_count", FieldName: "ReviewCount", GoType: "int64", DBTag: "review_count", JSONTag: "review_count"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"avg_rating", "discount", "id", "name", "review_count"},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "AvgRating", ComparatorType: "*comparator.Number[float64]", ColumnName: "avg_rating", Filterable: true},
			{FieldName: "Discount", ComparatorType: "*comparator.NullableNumber[float64]", ColumnName: "discount", Filterable: true},
			{FieldName: "ID", ComparatorType: "*comparator.ID", ColumnName: "id", Filterable: true},
			{FieldName: "Name", ComparatorType: "*comparator.String", ColumnName: "name", Filterable: true},
			{FieldName: "ReviewCount", ComparatorType: "*comparator.Number[int64]", ColumnName: "review_count", Filterable: true},
		},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "avg_rating", FieldName: "AvgRating", Shape: "direct", ScanExpr: "&p.AvgRating"},
			{ColumnName: "discount", FieldName: "Discount", Shape: "direct", ScanExpr: "&p.Discount"},
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&p.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&p.Name"},
			{ColumnName: "review_count", FieldName: "ReviewCount", Shape: "direct", ScanExpr: "&p.ReviewCount"},
		},
	}
}

// testViewContext_noPK returns a view context without @pk annotation.
func testViewContext_noPK() gen.ViewContext {
	ctx := testViewContext_withPK()
	ctx.HasPK = false
	ctx.PKColumns = nil
	// Remove PrimaryKey flag from ID column
	for i := range ctx.Columns {
		ctx.Columns[i].PrimaryKey = false
	}
	return ctx
}

// testViewContext_aggregateInference returns a view context demonstrating
// various aggregate inference types: COUNT → int64, AVG → *float64, SUM → base type,
// MIN/MAX → pointer of base.
func testViewContext_aggregateInference() gen.ViewContext {
	return gen.ViewContext{
		StructName:        "SalesSummary",
		ViewName:          "sales_summary",
		TableName:         "sales_summary",
		TableNameConstant: "TableSalesSummaries",
		Schema:            "public",
		Package:           "db",
		VarName:           "s",
		Dialect:           "postgres",
		Driver:            "pgx",
		PageSize:          100,
		QueryLimit:        1000,
		CursorKeys:        []string{"category"},
		Imports: []string{
			"context",
			"encoding/base64",
			"encoding/json",
			"fmt",
			"slices",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/hook",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "category", FieldName: "Category", GoType: "string", DBTag: "category", JSONTag: "category"},
			{Name: "max_price", FieldName: "MaxPrice", GoType: "*float64", DBTag: "max_price", JSONTag: "max_price", Nullable: true},
			{Name: "min_price", FieldName: "MinPrice", GoType: "*float64", DBTag: "min_price", JSONTag: "min_price", Nullable: true},
			{Name: "order_count", FieldName: "OrderCount", GoType: "int64", DBTag: "order_count", JSONTag: "order_count"},
			{Name: "total_revenue", FieldName: "TotalRevenue", GoType: "float64", DBTag: "total_revenue", JSONTag: "total_revenue"},
		},
		AllColumnNames: []string{"category", "max_price", "min_price", "order_count", "total_revenue"},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "Category", ComparatorType: "*comparator.String", ColumnName: "category", Filterable: true},
			{FieldName: "MaxPrice", ComparatorType: "*comparator.NullableNumber[float64]", ColumnName: "max_price", Filterable: true},
			{FieldName: "MinPrice", ComparatorType: "*comparator.NullableNumber[float64]", ColumnName: "min_price", Filterable: true},
			{FieldName: "OrderCount", ComparatorType: "*comparator.Number[int64]", ColumnName: "order_count", Filterable: true},
			{FieldName: "TotalRevenue", ComparatorType: "*comparator.Number[float64]", ColumnName: "total_revenue", Filterable: true},
		},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "category", FieldName: "Category", Shape: "direct", ScanExpr: "&s.Category"},
			{ColumnName: "max_price", FieldName: "MaxPrice", Shape: "direct", ScanExpr: "&s.MaxPrice"},
			{ColumnName: "min_price", FieldName: "MinPrice", Shape: "direct", ScanExpr: "&s.MinPrice"},
			{ColumnName: "order_count", FieldName: "OrderCount", Shape: "direct", ScanExpr: "&s.OrderCount"},
			{ColumnName: "total_revenue", FieldName: "TotalRevenue", Shape: "direct", ScanExpr: "&s.TotalRevenue"},
		},
	}
}

// --- Model tests ---

func TestViewModelTemplate_withTypeAnnotation(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewModelTemplate(t, ctx)

	wantPatterns := []string{
		"type ProductSummary struct {",
		"AvgRating float64", // @type override
		"ReviewCount int64", // COUNT inference
		"ID uuid.UUID",      // schema matching
		"Name string",       // schema matching
		"Discount *float64", // @nullable
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewModelTemplate_schemaMatchedTypes(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewModelTemplate(t, ctx)

	// Verify columns retain schema-matched Go types
	wantPatterns := []string{
		"ID uuid.UUID",
		"Name string",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing schema-matched type: %q", want)
		}
	}
}

func TestViewModelTemplate_aggregateInference(t *testing.T) {
	ctx := testViewContext_aggregateInference()
	output := executeViewModelTemplate(t, ctx)

	wantPatterns := []string{
		"OrderCount int64",     // COUNT → int64
		"TotalRevenue float64", // SUM → base type
		"MinPrice *float64",    // MIN → pointer of base
		"MaxPrice *float64",    // MAX → pointer of base
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing aggregate type: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewModelTemplate_structTags(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewModelTemplate(t, ctx)

	wantPatterns := []string{
		`db:"id" json:"id"`,
		`db:"name" json:"name"`,
		`db:"avg_rating" json:"avg_rating"`,
		`db:"review_count" json:"review_count"`,
		`db:"discount" json:"discount"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing tag: %q", want)
		}
	}
}

func TestViewModelTemplate_noRelationshipFields(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewModelTemplate(t, ctx)

	if strings.Contains(output, `db:"-"`) {
		t.Error("view model should not contain relationship fields")
	}
}

func TestViewModelTemplate_compilesCleanly(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewModelTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "product_summary_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Client tests ---

func TestViewClientTemplate_readOnlyInterface(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	// Should contain read operations
	wantPatterns := []string{
		"type ProductSummaryClient interface {",
		"Get(ctx context.Context,",
		"GetMany(ctx context.Context,",
		"Count(ctx context.Context,",
		"Paginate(ctx context.Context,",
		"Connection(ctx context.Context,",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT contain write operations
	notWant := []string{
		"Create(",
		"CreateMany(",
		"Update(",
		"UpdateMany(",
		"UpdateWhere(",
		"Upsert(",
		"SoftDelete(",
		"HardDelete(",
		"Restore(",
		"Increment(",
	}
	for _, nw := range notWant {
		if strings.Contains(output, nw) {
			t.Errorf("output should not contain write operation: %q", nw)
		}
	}
}

func TestViewClientTemplate_getMethodWithPK(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	if !strings.Contains(output, "Get(ctx context.Context, id uuid.UUID") {
		t.Errorf("output missing Get method with PK parameter\n\nfull output:\n%s", output)
	}
}

func TestViewClientTemplate_noGetMethodWithoutPK(t *testing.T) {
	ctx := testViewContext_noPK()
	output := executeViewClientTemplate(t, ctx)

	// GetMany should still be present
	if !strings.Contains(output, "GetMany(") {
		t.Error("output missing GetMany method")
	}

	// Get should NOT be present (no @pk)
	if strings.Contains(output, "\tGet(ctx") {
		t.Errorf("output should not contain Get method without @pk\n\nfull output:\n%s", output)
	}
}

func TestViewClientTemplate_concreteStruct(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	wantPatterns := []string{
		"type productSummaryClient struct {",
		"querier      database.Querier",
		"dialect      sql.Dialect",
		"table        sql.Table",
		"queryLimit   int",
		"pageSize     int",
		"cursorKeys   []string",
		"mutationHooks []hook.MutationHook",
		"queryHooks   []hook.QueryHook",
		"panicHandler hook.PanicHandler",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewClientTemplate_constructor(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	wantPatterns := []string{
		"func newProductSummaryClient(querier database.Querier, mutationHooks []hook.MutationHook, queryHooks []hook.QueryHook, panicHandler hook.PanicHandler) *productSummaryClient {",
		`Name: "product_summary"`,
		"sql.NewPostgresDialect()",
		"mutationHooks:",
		"queryHooks:",
		"panicHandler:",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewClientTemplate_compilesCleanly(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "product_summary_client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Get tests ---

func TestViewGetTemplate_allColumnsVar(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	wantPatterns := []string{
		"var productSummaryAllColumns = []string{",
		`"avg_rating"`,
		`"discount"`,
		`"id"`,
		`"name"`,
		`"review_count"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewGetTemplate_scanFunction(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	wantPatterns := []string{
		"func scanProductSummaries(rows database.Rows, columns []string) ([]*ProductSummary, error) {",
		"p := &ProductSummary{}",
		`case "id":`,
		"targets[idx] = &p.ID",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewGetTemplate_getMethodWithPK(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productSummaryClient) Get(ctx context.Context, id uuid.UUID",
		"c.GetMany(ctx, &GetProductSummariesInput{",
		"return nil, ErrNotFound",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewGetTemplate_noGetMethodWithoutPK(t *testing.T) {
	ctx := testViewContext_noPK()
	output := executeViewGetTemplate(t, ctx)

	// GetMany should still be present
	if !strings.Contains(output, "func (c *productSummaryClient) GetMany(") {
		t.Error("output missing GetMany method")
	}

	// Get should NOT be generated
	if strings.Contains(output, "func (c *productSummaryClient) Get(ctx context.Context, id") {
		t.Errorf("output should not contain Get method without @pk")
	}
}

func TestViewGetTemplate_getManyMethod(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productSummaryClient) GetMany(ctx context.Context, input *GetProductSummariesInput",
		"resolveCallOptions(opts)",
		"database.Conn(ctx, c.querier)",
		"sql.BuildSelect(c.dialect, c.table",
		"scanProductSummaries(rows, columns)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewGetTemplate_noO2OJoins(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	notWant := []string{
		"resolveO2OJoins",
		"BuildSelectJoin",
		"loadRelationships",
		"errgroup",
	}
	for _, nw := range notWant {
		if strings.Contains(output, nw) {
			t.Errorf("view get should not contain table relationship logic: %q", nw)
		}
	}
}

func TestViewGetTemplate_noSoftDelete(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	notWant := []string{
		"excludeDeleted",
		"softDelete",
		"SoftDelete",
		"is not deleted",
	}
	for _, nw := range notWant {
		if strings.Contains(output, nw) {
			t.Errorf("view get should not contain soft delete logic: %q", nw)
		}
	}
}

func TestViewGetTemplate_compilesCleanly(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "product_summary_get_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Count tests ---

func TestViewCountTemplate_countMethod(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewCountTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productSummaryClient) Count(ctx context.Context, filter *ProductSummaryFilter",
		"sql.BuildCount(c.dialect, c.table",
		`count product_summary`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewCountTemplate_noSoftDelete(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewCountTemplate(t, ctx)

	if strings.Contains(output, "excludeDeleted") {
		t.Error("view count should not contain soft delete logic")
	}
}

func TestViewCountTemplate_compilesCleanly(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewCountTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "product_summary_count_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Pagination tests ---

func TestViewPaginationTemplate_paginateMethod(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewPaginationTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productSummaryClient) Paginate(ctx context.Context, input PaginateInput[ProductSummaryFilter]",
		"c.Count(ctx, input.Filter",
		"c.GetMany(ctx, &GetProductSummariesInput{",
		"PaginateResult[ProductSummary]",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewPaginationTemplate_connectionMethod(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewPaginationTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productSummaryClient) Connection(ctx context.Context, input ConnectionInput[ProductSummaryFilter]",
		"Connection[ProductSummary]",
		"decodeCursor(",
		"cursorKeyset(c.dialect, c.cursorKeys, cursor, ",
		"cursorSorts(",
		"encodeProductSummaryCursor(",
		"Edge[ProductSummary]",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestViewPaginationTemplate_encodeCursorFunction(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewPaginationTemplate(t, ctx)

	wantPatterns := []string{
		"func encodeProductSummaryCursor(keys []string, p *ProductSummary) (string, error) {",
		`case "id":`,
		"json.Marshal(out)",
		"base64.StdEncoding.EncodeToString(data)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// Regression: same shadow vector applies to the view pagination
// template — when a view's StructName starts with "M", VarName == "m" and
// the local cursor map must not also be named "m".
func TestViewPaginationTemplate_encodeCursor_doesNotShadowEntity_MStartingStruct(t *testing.T) {
	ctx := testViewContext_withPK()
	ctx.StructName = "MetricSummary"
	ctx.ViewName = "metric_summary"
	ctx.TableName = "metric_summary"
	ctx.TableNameConstant = "TableMetricSummaries"
	ctx.VarName = "m"
	output := executeViewPaginationTemplate(t, ctx)

	wantPatterns := []string{
		"func encodeMetricSummaryCursor(keys []string, m *MetricSummary) (string, error)",
		"out := make(map[string]any, len(keys))",
		"out[\"id\"] = m.ID",
		"json.Marshal(out)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	forbidden := []string{
		"m := make(map[string]any",
		`m["id"] = m.ID`,
	}
	for _, bad := range forbidden {
		if strings.Contains(output, bad) {
			t.Errorf("output should not contain shadow pattern %q\n\nfull output:\n%s", bad, output)
		}
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "metric_summary_pagination_gen.go"); err != nil {
		t.Fatalf("FormatOnly failed — encode<View>Cursor has a parse error: %v", err)
	}
}

func TestViewPaginationTemplate_compilesCleanly(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewPaginationTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "product_summary_pagination_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Golden file tests ---

func TestViewGetTemplate_goldenFile(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewGetTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "view_product_summary_get_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "view_product_summary_get_gen.go")

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
		t.Errorf("view_product_summary_get_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestViewClientTemplate_goldenFile(t *testing.T) {
	ctx := testViewContext_withPK()
	output := executeViewClientTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "view_product_summary_client_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "view_product_summary_client_gen.go")

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
		t.Errorf("view_product_summary_client_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Materialized view refresh template tests ---

func loadViewRefreshTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("refresh.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "view", "refresh.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing view refresh template: %v", err)
	}
	return tmpl
}

func executeViewRefreshTemplate(t *testing.T, ctx gen.ViewContext) string {
	t.Helper()
	tmpl := loadViewRefreshTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/refresh", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// testViewContext_matview returns a materialized-view context with a
// discovered PK (unique index) — the full-surface shape: Refresh +
// RefreshConcurrently.
func testViewContext_matview() gen.ViewContext {
	ctx := testViewContext_withPK()
	ctx.Materialized = true
	ctx.ConcurrentlyRefreshable = true
	return ctx
}

// testViewContext_matviewNoUniqueIndex returns a materialized-view context
// without a qualifying unique index — Refresh only.
func testViewContext_matviewNoUniqueIndex() gen.ViewContext {
	ctx := testViewContext_noPK()
	ctx.Materialized = true
	ctx.ConcurrentlyRefreshable = false
	return ctx
}

func TestViewRefreshTemplate_bothMethods(t *testing.T) {
	output := executeViewRefreshTemplate(t, testViewContext_matview())

	wantPatterns := []string{
		"func (c *productSummaryClient) Refresh(ctx context.Context) error",
		"func (c *productSummaryClient) RefreshConcurrently(ctx context.Context) error",
		"sql.BuildRefreshMaterializedView(c.dialect, c.table, false)",
		"sql.BuildRefreshMaterializedView(c.dialect, c.table, true)",
		"Op: hook.OpRefresh",
		"ACCESS EXCLUSIVE",
		"cannot run inside a transaction",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// The transaction guard must fail fast: the InTransaction check appears
// before the hook chain / SQL execution, wrapping the sentinel so
// errors.Is(err, database.ErrRefreshConcurrentlyInTx) matches.
func TestViewRefreshTemplate_txGuardPrecedesExecution(t *testing.T) {
	output := executeViewRefreshTemplate(t, testViewContext_matview())

	concIdx := strings.Index(output, "func (c *productSummaryClient) RefreshConcurrently")
	if concIdx < 0 {
		t.Fatalf("RefreshConcurrently not emitted:\n%s", output)
	}
	conc := output[concIdx:]

	guardIdx := strings.Index(conc, "database.InTransaction(ctx)")
	sentinelIdx := strings.Index(conc, "database.ErrRefreshConcurrentlyInTx")
	execIdx := strings.Index(conc, "c.executeQuery(")
	if guardIdx < 0 || sentinelIdx < 0 || execIdx < 0 {
		t.Fatalf("guard pieces missing (guard=%d sentinel=%d exec=%d):\n%s", guardIdx, sentinelIdx, execIdx, conc)
	}
	if guardIdx > execIdx {
		t.Errorf("InTransaction guard appears after executeQuery — guard must fail fast before any SQL:\n%s", conc)
	}
	if sentinelIdx > execIdx {
		t.Errorf("sentinel return appears after executeQuery — guard must return before any SQL:\n%s", conc)
	}

	// Plain Refresh has no transaction guard.
	refreshOnly := output[:concIdx]
	if strings.Contains(refreshOnly, "InTransaction") {
		t.Errorf("plain Refresh must not carry a transaction guard:\n%s", refreshOnly)
	}
}

func TestViewRefreshTemplate_noUniqueIndex_refreshOnly(t *testing.T) {
	output := executeViewRefreshTemplate(t, testViewContext_matviewNoUniqueIndex())

	if !strings.Contains(output, ") Refresh(ctx context.Context) error") {
		t.Errorf("Refresh missing:\n%s", output)
	}
	if strings.Contains(output, "RefreshConcurrently") {
		t.Errorf("RefreshConcurrently must not be emitted without a unique index:\n%s", output)
	}
}

func TestViewRefreshTemplate_regularViewEmitsNothing(t *testing.T) {
	tmpl := loadViewRefreshTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "view/refresh", testViewContext_withPK()); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Errorf("regular view emitted refresh code:\n%s", got)
	}
}

func TestViewClientTemplate_matviewInterface(t *testing.T) {
	output := executeViewClientTemplate(t, testViewContext_matview())

	wantPatterns := []string{
		"Refresh(ctx context.Context) error",
		"RefreshConcurrently(ctx context.Context) error",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("client interface missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	regular := executeViewClientTemplate(t, testViewContext_withPK())
	if strings.Contains(regular, "Refresh") {
		t.Errorf("regular view client must not mention Refresh:\n%s", regular)
	}
}

func TestViewRefreshTemplate_compilesCleanly(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  gen.ViewContext
	}{
		{"with unique index", testViewContext_matview()},
		{"without unique index", testViewContext_matviewNoUniqueIndex()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := executeViewRefreshTemplate(t, tc.ctx)
			if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "matview_refresh_gen.go"); err != nil {
				t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
			}
		})
	}
}

func TestViewRefreshTemplate_goldenFile(t *testing.T) {
	output := executeViewRefreshTemplate(t, testViewContext_matview())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "view_matview_refresh_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "golden", "view_matview_refresh_gen.go"), formatted)
}

func TestViewRefreshTemplate_goldenFile_noUniqueIndex(t *testing.T) {
	output := executeViewRefreshTemplate(t, testViewContext_matviewNoUniqueIndex())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "view_matview_refresh_plain_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "golden", "view_matview_refresh_plain_gen.go"), formatted)
}

func TestViewClientTemplate_goldenFile_matview(t *testing.T) {
	output := executeViewClientTemplate(t, testViewContext_matview())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "view_matview_client_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "golden", "view_matview_client_gen.go"), formatted)
}

// compareGolden asserts formatted output against the golden file at path,
// rewriting it when -update is set.
func compareGolden(t *testing.T, goldenPath string, formatted []byte) {
	t.Helper()
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
	if diff := cmp.Diff(string(want), string(formatted)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", filepath.Base(goldenPath), diff)
	}
}
