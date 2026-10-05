package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

func loadGetTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("get.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "get.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing get template: %v", err)
	}
	return tmpl
}

func executeGetTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadGetTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/get", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func testGetContext_singlePK() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Description:       "Catalog of products available for sale.",
		Package:           "db",
		VarName:           "p",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"time",
		},
		Columns: []gen.ColumnContext{
			{Name: "company_id", FieldName: "CompanyID", GoType: "uuid.NullUUID", DBTag: "company_id", JSONTag: "company_id", Nullable: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "deleted_at", FieldName: "DeletedAt", GoType: "*time.Time", DBTag: "deleted_at", JSONTag: "deleted_at", Nullable: true, Import: "time"},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "price", FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{Name: "sku", FieldName: "SKU", GoType: "string", DBTag: "sku", JSONTag: "sku"},
			{Name: "updated_at", FieldName: "UpdatedAt", GoType: "time.Time", DBTag: "updated_at", JSONTag: "updated_at", Import: "time"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"company_id", "deleted_at", "id", "name", "price", "sku", "updated_at"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "company_id", FieldName: "CompanyID", Shape: "direct", ScanExpr: "&p.CompanyID"},
			{ColumnName: "deleted_at", FieldName: "DeletedAt", Shape: "direct", ScanExpr: "&p.DeletedAt"},
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&p.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&p.Name"},
			{ColumnName: "price", FieldName: "Price", Shape: "direct", ScanExpr: "&p.Price"},
			{ColumnName: "sku", FieldName: "SKU", Shape: "direct", ScanExpr: "&p.SKU"},
			{ColumnName: "updated_at", FieldName: "UpdatedAt", Shape: "direct", ScanExpr: "&p.UpdatedAt"},
		},
		SoftDelete: &gen.SoftDeleteContext{
			Column:    "deleted_at",
			FieldName: "DeletedAt",
			Strategy:  "timestamp",
		},
		ExcludeDeleted: true,
		Operations: gen.ResolvedOperations{
			Get:     true,
			GetMany: true,
			Count:   true,
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		QueryLimit: 100,
	}
}

func testGetContext_compositePK() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Description:           "Line item in an order.",
		Package:               "db",
		VarName:               "o",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "quantity", FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"order_id", "product_id", "quantity"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "order_id", FieldName: "OrderID", Shape: "direct", ScanExpr: "&o.OrderID"},
			{ColumnName: "product_id", FieldName: "ProductID", Shape: "direct", ScanExpr: "&o.ProductID"},
			{ColumnName: "quantity", FieldName: "Quantity", Shape: "direct", ScanExpr: "&o.Quantity"},
		},
		Operations: gen.ResolvedOperations{
			Get:     true,
			GetMany: true,
			Count:   true,
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		QueryLimit: 100,
	}
}

func testGetContext_withRelationships() gen.TableContext {
	ctx := testGetContext_singlePK()
	ctx.Imports = append(ctx.Imports, "golang.org/x/sync/errgroup")
	ctx.HasO2MRelationships = true
	ctx.O2MRelationships = []gen.RelationshipContext{
		{
			Name:             "reviews",
			Type:             parser.OneToMany,
			TargetTable:      "reviews",
			TargetStructName: "Review",
			FKColumn:         "product_id",
			FKFieldName:      "ProductID",
			FieldName:        "Reviews",
			GoType:           "[]*Review",
			JSONTag:          "reviews",
			// FKColumnGoType is the resolved Go type of the FK column itself.
			// Production codegen populates this via wireRelationshipFKMetadata;
			// unit tests must set it to drive the fkLoadGuard / fkLoadKey
			// helpers (NOT NULL FK → bare uuid.UUID, no guard, .String() unwrap).
			FKColumnGoType: "uuid.UUID",
		},
	}
	ctx.M2MRelationships = []gen.RelationshipContext{
		{
			Name:                "tags",
			Type:                parser.ManyToMany,
			TargetTable:         "tags",
			TargetStructName:    "Tag",
			JunctionTable:       "product_tags",
			JunctionSchema:      "public",
			JunctionLocalFK:     "product_id",
			JunctionReferenceFK: "tag_id",
			FieldName:           "Tags",
			GoType:              "[]*Tag",
			JSONTag:             "tags",
			// FKGoType is the target's PK Go type (Tag.ID is uuid.UUID).
			// Production codegen populates this via wireRelationshipFKMetadata;
			// unit tests must set it to drive the target-type-aware
			// fkToStringByGoType conversion.
			FKGoType: "uuid.UUID",
			// TargetPKFieldName / TargetPKColumn are the target's PK. Same wiring
			// pass, same fixture rule: the M2M loader spells its map key with the
			// field and declares the column on the target fetch.
			TargetPKFieldName: "ID",
			TargetPKColumn:    "id",
		},
	}
	ctx.Relationships = append(
		ctx.Relationships,
		ctx.O2MRelationships[0],
		ctx.M2MRelationships[0],
	)
	return ctx
}

func testGetContext_withO2O() gen.TableContext {
	ctx := testGetContext_singlePK()
	ctx.HasO2ORelationships = true
	ctx.O2ORelationships = []gen.RelationshipContext{
		{
			Name:             "company",
			Type:             parser.OneToOne,
			TargetTable:      "companies",
			TargetStructName: "Company",
			FKColumn:         "company_id",
			FKFieldName:      "CompanyID",
			FieldName:        "Company",
			GoType:           "*Company",
			JSONTag:          "company",
		},
	}
	ctx.Relationships = append(ctx.Relationships, ctx.O2ORelationships[0])
	return ctx
}

func testGetContext_scanShapes() gen.TableContext {
	return gen.TableContext{
		StructName:        "Widget",
		TableName:         "widgets",
		TableNameConstant: "TableWidgets",
		Schema:            "public",
		Package:           "db",
		VarName:           "w",
		Imports: []string{
			"context",
			"fmt",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "tags", FieldName: "Tags", GoType: "[]string", DBTag: "tags", JSONTag: "tags", IsSlice: true},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
		},
		AllColumnNames: []string{"id", "name", "tags"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&w.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&w.Name"},
			{ColumnName: "tags", FieldName: "Tags", Shape: "wrapped", ScanExpr: "pq.Array(&w.Tags)"},
		},
		Operations: gen.ResolvedOperations{
			Get:     true,
			GetMany: true,
			Count:   true,
		},
		Dialect:    "postgres",
		Driver:     "stdlib",
		QueryLimit: 100,
	}
}

// --- Test: Get by single PK ---

func TestGetTemplate_singlePK(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productClient) Get(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"products, err := c.GetMany(ctx, &GetProductsInput{",
		"ID: &comparator.ID{Eq: new(id.String())}",
		"Limit: new(1)",
		"return nil, ErrNotFound",
		"return products[0], nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Get by composite PK ---

func TestGetTemplate_compositePK(t *testing.T) {
	ctx := testGetContext_compositePK()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *orderItemClient) Get(ctx context.Context, pk OrderItemPK, opts ...func(*CallOptions[OrderItemFieldOptions])) (*OrderItem, error)",
		"OrderID: &comparator.ID{Eq: new(pk.OrderID.String())}",
		"ProductID: &comparator.ID{Eq: new(pk.ProductID.String())}",
		"Limit: new(1)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT use single "id" parameter.
	if strings.Contains(output, "id uuid.UUID") {
		t.Error("composite PK Get should not use 'id uuid.UUID' parameter")
	}
}

// --- Test: GetMany with filter, limit, offset, sorts ---

func TestGetTemplate_getManyQueryBuilding(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productClient) GetMany(ctx context.Context, input *GetProductsInput, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"options := resolveCallOptions(opts)",
		"conn := database.Conn(ctx, c.querier)",
		"columns := productAllColumns",
		"columns = unionColumns(selected, input.requiredColumns...)",
		"conds = input.Filter.ToConditions(c.dialect)",
		"} else if c.queryLimit > 0 {",
		"sql.BuildSelect(c.dialect, c.table, sql.SelectOptions{",
		"Columns:    columns",
		"Conditions: conds",
		"OrderBy:    input.Sorts",
		"Limit:      limitPtr",
		"Offset:     offsetPtr",
		"scanProducts(rows, columns)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: GetMany with FieldOptions (partial column selection) ---

func TestGetTemplate_getManyFieldOptions(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"fieldOptions := options.FieldOptions",
		"!fieldOptions.HasSelectedColumns()",
		"return nil, nil",
		"if selected := fieldOptions.Columns(); len(selected) > 0 {",
		"columns = unionColumns(selected, input.requiredColumns...)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Scan function shapes ---

func TestGetTemplate_scanFunction_allShapes(t *testing.T) {
	ctx := testGetContext_scanShapes()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		// Scan function signature
		"func scanWidgets(rows database.Rows, columns []string) ([]*Widget, error)",
		// Shape 1 — direct
		`case "id":`,
		"targets[idx] = &w.ID",
		`case "name":`,
		"targets[idx] = &w.Name",
		// Shape 2 — wrapped
		`case "tags":`,
		"targets[idx] = pq.Array(&w.Tags)",
		// Error messages
		`fmt.Errorf("scan widget:`,
		`fmt.Errorf("iterate widget rows:`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Direct-only scan function ---

func TestGetTemplate_scanFunction_directOnly(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"func scanProducts(rows database.Rows, columns []string) ([]*Product, error)",
		`case "id":`,
		"targets[idx] = &p.ID",
		`case "name":`,
		"targets[idx] = &p.Name",
		`case "price":`,
		"targets[idx] = &p.Price",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Scan loop index does not shadow entity var ---

// When VarName == "i" (any struct starting with "I", e.g. Inverter), the
// scan loop's index must not collide with the outer entity local. Pin the
// loop index name to `idx` and assert the entity assign `i := &Inverter{}`
// is followed by `for idx, col := range columns` (not `for i, col`).
func TestGetTemplate_scanLoop_doesNotShadowEntity_IStartingStruct(t *testing.T) {
	ctx := testGetContext_singlePK()
	ctx.StructName = "Inverter"
	ctx.TableName = "inverters"
	ctx.TableNameConstant = "TableInverters"
	ctx.VarName = "i"
	for j := range ctx.ScanShapes {
		ctx.ScanShapes[j].ScanExpr = strings.Replace(ctx.ScanShapes[j].ScanExpr, "&p.", "&i.", 1)
	}
	output := executeGetTemplate(t, ctx)

	if !strings.Contains(output, "i := &Inverter{}") {
		t.Fatalf("expected entity assign `i := &Inverter{}` in output\n\n%s", output)
	}
	if strings.Contains(output, "for i, col := range columns") {
		t.Errorf("scan loop must not use `i` as loop index when VarName==i (shadows entity)\n\n%s", output)
	}
	if !strings.Contains(output, "for idx, col := range columns") {
		t.Errorf("scan loop should use `idx` as loop index\n\n%s", output)
	}
	if strings.Contains(output, "targets[i] = &i.") {
		t.Errorf("`targets[i] = &i.X` is the shadow bug; loop index should be `idx`\n\n%s", output)
	}
}

// --- Test: AllColumns variable ---

func TestGetTemplate_allColumnsVariable(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	want := `var productAllColumns = []string{"company_id", "deleted_at", "id", "name", "price", "sku", "updated_at", }`
	if !strings.Contains(output, want) {
		t.Errorf("output missing allColumns variable: %q\n\nfull output:\n%s", want, output)
	}
}

// --- Test: Soft delete condition in GetMany ---

func TestGetTemplate_softDeleteCondition(t *testing.T) {
	tests := []struct {
		name      string
		strategy  config.SoftDeleteType
		column    string
		fieldName string
		want      string
	}{
		{
			name:      "timestamp",
			strategy:  config.SoftDeleteTimestamp,
			column:    "deleted_at",
			fieldName: "DeletedAt",
			want:      `sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull()`,
		},
		{
			name:      "bool",
			strategy:  config.SoftDeleteBool,
			column:    "is_deleted",
			fieldName: "IsDeleted",
			want:      `sql.Where(c.dialect.QuoteIdentifier("is_deleted")).Eq(false)`,
		},
		{
			name:      "integer",
			strategy:  config.SoftDeleteInteger,
			column:    "deleted",
			fieldName: "Deleted",
			want:      `sql.Where(c.dialect.QuoteIdentifier("deleted")).Eq(0)`,
		},
		{
			// A `column_map.<col>.name` override renames the filter field the
			// soft-delete guard reads; the template must spell the override,
			// not the naming engine's form.
			name:      "renamed field",
			strategy:  config.SoftDeleteTimestamp,
			column:    "deleted_at",
			fieldName: "ArchivedAt",
			want:      `sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull()`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testGetContext_singlePK()
			ctx.SoftDelete = &gen.SoftDeleteContext{
				Column:    tt.column,
				FieldName: tt.fieldName,
				Strategy:  tt.strategy,
			}
			output := executeGetTemplate(t, ctx)

			guard := "c.excludeDeleted && (input.Filter == nil || input.Filter." + tt.fieldName + " == nil)"
			if !strings.Contains(output, guard) {
				t.Errorf("output missing excludeDeleted guard %q", guard)
			}
			if !strings.Contains(output, tt.want) {
				t.Errorf("output missing soft delete condition: %q\n\nfull output:\n%s", tt.want, output)
			}
		})
	}
}

// --- Test: No soft delete condition when not configured ---

func TestGetTemplate_noSoftDelete(t *testing.T) {
	ctx := testGetContext_compositePK()
	output := executeGetTemplate(t, ctx)

	if strings.Contains(output, "excludeDeleted") {
		t.Error("output without soft delete should not contain excludeDeleted")
	}
	if strings.Contains(output, "IsNull") {
		t.Error("output without soft delete should not contain IsNull")
	}
}

// --- Test: O2M relationship loading ---

func TestGetTemplate_o2mRelationshipLoading(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *productClient) loadRelationships(ctx context.Context, conn database.Querier, products []*Product, fo *ProductFieldOptions) error",
		"g, ctx := errgroup.WithContext(ctx)",
		"if fo.Reviews != nil {",
		`ProductID: &comparator.ID{In: productIDs}`,
		"c.reviewClient.GetMany(ctx, &GetReviewsInput{",
		"Limit:  new(0)",
		// The child fetch declares the FK on the input struct rather than
		// flipping it on the caller's FieldOptions.
		`requiredColumns: []string{"product_id"},`,
		"co.FieldOptions = fo.Reviews.FieldOptions",
		"byFK := make(map[string][]*Review)",
		"return g.Wait()",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing O2M pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: M2M relationship loading ---

func TestGetTemplate_m2mRelationshipLoading(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"if fo.Tags != nil {",
		`sql.BuildSelect(c.dialect, sql.Table{Schema: "public", Name: "product_tags"}`,
		`"product_id", "tag_id"`,
		`sql.Where(c.dialect.QuoteIdentifier("product_id")).In(toAnySlice(productIDs)...)`,
		"parentToTargetIDs := make(map[string][]string)",
		"c.tagClient.GetMany(ctx, &GetTagsInput{",
		// The target fetch declares the target PK and the target-to-parent map
		// is keyed on it — a narrowed FieldOptions that omitted it
		// would strand every loaded target.
		`requiredColumns: []string{"id"},`,
		"co.FieldOptions = fo.Tags.FieldOptions",
		// Targets are walked in fetch order so the Sorts carry through.
		"parentsByTarget := make(map[string][]int, len(",
		"for _, i := range parentsByTarget[",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing M2M pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Parallel loading via errgroup ---

func TestGetTemplate_parallelLoading(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	// Both O2M and M2M should use g.Go
	goCount := strings.Count(output, "g.Go(func() error {")
	if goCount != 2 {
		t.Errorf("expected 2 g.Go calls (O2M + M2M), got %d", goCount)
	}
}

// --- Test: the fan-out carries no transaction-aware bound (PRD §13.2 step 4) ---

// An earlier version bounded the errgroup to one worker inside a transaction,
// because two selected edges put two statements on one pgx connection: `conn
// busy` plus a data race in its statement cache. §18.5's connection reservation serializes
// them at the Tx instead, so the bound is redundant — and it must stay gone
// rather than return as a belt-and-braces, because with it the generated tree
// never reaches the reservation on a multi-edge read inside a transaction.
//
// The adjacency, not just the absence of SetLimit, is what this pins: a bound
// spelled some other way would still have to sit between the group and the
// first edge.
func TestGetTemplate_relationshipFanOutIsUnbounded(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	if strings.Contains(output, "SetLimit") {
		t.Error("loadRelationships must not bound the errgroup; the Tx's connection reservation serializes the fan-out (PRD §13.2 step 4, §18.5)")
	}

	want := "g, ctx := errgroup.WithContext(ctx)\n\n\tif fo."
	if !strings.Contains(output, want) {
		t.Errorf("nothing may sit between the errgroup and the first edge; output missing:\n%s\n\nfull output:\n%s", want, output)
	}
}

// --- Test: GetMany's doc gates each sentence on the feature it describes ---

// docCommentAbove returns the contiguous comment block on the lines directly
// above the first line of out containing marker.
func docCommentAbove(t *testing.T, out, marker string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		start := i
		for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "//") {
			start--
		}
		return strings.Join(lines[start:i], "\n")
	}
	t.Fatalf("marker %q not found in output:\n%s", marker, out)
	return ""
}

// The relationship-loading sentence was once emitted under the soft-delete
// gate, so a table with relationships and no soft delete documented nothing
// about them, while a soft-deleted table without any documented a fan-out it
// does not have. Each sentence is gated on its own feature, in both the
// implementation and the interface.
func TestGetManyDoc_sentencesGatedOnTheirOwnFeature(t *testing.T) {
	const (
		softDeleteSentence   = "Soft-deleted products are excluded by default."
		relationshipSentence = "When FieldOptions includes O2M or M2M relationships"
	)

	tests := []struct {
		name          string
		softDelete    bool
		relationships bool
	}{
		{name: "neither", softDelete: false, relationships: false},
		{name: "soft delete only", softDelete: true, relationships: false},
		{name: "relationships only", softDelete: false, relationships: true},
		{name: "both", softDelete: true, relationships: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getCtx := testGetContext_singlePK()
			if tt.relationships {
				getCtx = testGetContext_withRelationships()
			}
			clientCtx := testClientContext_fullOps()
			if !tt.softDelete {
				getCtx.SoftDelete = nil
				getCtx.ExcludeDeleted = false
				clientCtx = testClientContext_noSoftDelete()
			}
			clientCtx.HasO2MRelationships = tt.relationships

			docs := map[string]string{
				"implementation": docCommentAbove(t, executeGetTemplate(t, getCtx), "func (c *productClient) GetMany("),
				"interface":      docCommentAbove(t, executeClientTemplate(t, clientCtx), "\tGetMany(ctx context.Context"),
			}
			for site, doc := range docs {
				if got := strings.Contains(doc, softDeleteSentence); got != tt.softDelete {
					t.Errorf("%s: soft-delete sentence present = %v, want %v; doc:\n%s", site, got, tt.softDelete, doc)
				}
				if got := strings.Contains(doc, relationshipSentence); got != tt.relationships {
					t.Errorf("%s: relationship sentence present = %v, want %v; doc:\n%s", site, got, tt.relationships, doc)
				}
			}
		})
	}
}

// --- Test: O2O branching in GetMany ---

func TestGetTemplate_o2oBranching(t *testing.T) {
	ctx := testGetContext_withO2O()
	output := executeGetTemplate(t, ctx)

	wantPatterns := []string{
		"o2oJoins := c.resolveO2OJoins(c.dialect, fieldOptions)",
		"if len(o2oJoins) > 0 {",
		"sql.BuildSelectJoin(c.dialect, c.table, o2oJoins, sql.SelectOptions{",
		`Alias:      "p"`,
		"scanProductsWithOneToOneJoins(rows, joinColumns)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing O2O branch pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: No O2O branch when no O2O relationships ---

func TestGetTemplate_noO2OBranch(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	if strings.Contains(output, "resolveO2OJoins") {
		t.Error("output without O2O relationships should not contain resolveO2OJoins")
	}
	if strings.Contains(output, "BuildSelectJoin") {
		t.Error("output without O2O relationships should not contain BuildSelectJoin")
	}
}

// --- Test: No loadRelationships when no O2M/M2M ---

func TestGetTemplate_noRelationshipLoading(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	if strings.Contains(output, "loadRelationships") {
		t.Error("output without O2M/M2M relationships should not contain loadRelationships")
	}
	if strings.Contains(output, "errgroup.WithContext") {
		t.Error("output without O2M/M2M relationships should not call errgroup.WithContext")
	}
}

// --- Test: Golden file comparison ---

func TestGetTemplate_goldenFile_singlePK(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_get_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "get_products_gen.go")

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
		t.Errorf("get_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestGetTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testGetContext_compositePK()
	output := executeGetTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_get_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "get_order_items_gen.go")

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
		t.Errorf("get_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestGetTemplate_goldenFile_withRelationships(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_get_relationships_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "get_products_relationships_gen.go")

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
		t.Errorf("get_products_relationships_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: Template compiles cleanly ---

func TestGetTemplate_compilesCleanly(t *testing.T) {
	ctx := testGetContext_singlePK()
	output := executeGetTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_get_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestGetTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testGetContext_compositePK()
	output := executeGetTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_get_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestGetTemplate_compilesCleanly_scanShapes(t *testing.T) {
	ctx := testGetContext_scanShapes()
	output := executeGetTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "widgets_get_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Regression: nullable FK on O2M target ---
//
// When the FK column on the child side of an O2M is nullable, the child's
// filter struct types the FK field as *comparator.NullableNumber[T] (or
// *comparator.NullableID for string PKs). The relationship loader must emit
// the matching constructor — &comparator.NullableNumber[T]{Number: ...} or
// &comparator.NullableID{ID: ...} — instead of the non-nullable variant. This
// mirrors a self-referencing hierarchy table (e.g. categories.parent_id) where
// the parent linkage is naturally optional.

func testGetContext_selfRefNullableFK_numeric() gen.TableContext {
	ctx := gen.TableContext{
		StructName:        "Category",
		TableName:         "categories",
		TableNameConstant: "TableCategories",
		Schema:            "public",
		Package:           "db",
		VarName:           "c",
		Imports: []string{
			"context",
			"fmt",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"golang.org/x/sync/errgroup",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "parent_id", FieldName: "ParentID", GoType: "*int64", DBTag: "parent_id", JSONTag: "parent_id", Nullable: true},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
		},
		AllColumnNames: []string{"id", "name", "parent_id"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&c.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&c.Name"},
			{ColumnName: "parent_id", FieldName: "ParentID", Shape: "direct", ScanExpr: "&c.ParentID"},
		},
		Operations: gen.ResolvedOperations{
			Get:     true,
			GetMany: true,
			Count:   true,
		},
		Dialect:             "postgres",
		Driver:              "pgx",
		QueryLimit:          100,
		HasO2MRelationships: true,
		O2MRelationships: []gen.RelationshipContext{
			{
				Name:             "children",
				Type:             parser.OneToMany,
				TargetTable:      "categories",
				TargetSchema:     "public",
				TargetStructName: "Category",
				FKColumn:         "parent_id",
				FKFieldName:      "ParentID",
				FieldName:        "Children",
				GoType:           "[]*Category",
				JSONTag:          "children",
				FKGoType:         "int64",
				FKNullable:       true,
				// FK column is nullable *int64 (categories.parent_id) — drives
				// the pointer-guard form: `if r.ParentID != nil` +
				// `fmt.Sprint(*r.ParentID)` for the bucket key.
				FKColumnGoType: "*int64",
			},
		},
	}
	ctx.Relationships = append(ctx.Relationships, ctx.O2MRelationships[0])
	return ctx
}

func TestGetTemplate_o2mRelationshipLoading_nullableNumericFK(t *testing.T) {
	ctx := testGetContext_selfRefNullableFK_numeric()
	output := executeGetTemplate(t, ctx)

	want := "ParentID: &comparator.NullableNumber[int64]{Number: comparator.Number[int64]{In: categoryIDs}}"
	if !strings.Contains(output, want) {
		t.Errorf("output missing nullable-FK pattern: %q\n\nfull output:\n%s", want, output)
	}
	// Must NOT emit the non-nullable variant for the same field.
	if strings.Contains(output, "ParentID: &comparator.Number[int64]{In:") {
		t.Errorf("output emits non-nullable Number for nullable FK column\n\nfull output:\n%s", output)
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "categories_get_gen.go"); err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func testGetContext_selfRefNullableFK_string() gen.TableContext {
	ctx := gen.TableContext{
		StructName:        "Folder",
		TableName:         "folders",
		TableNameConstant: "TableFolders",
		Schema:            "public",
		Package:           "db",
		VarName:           "f",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"golang.org/x/sync/errgroup",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "parent_id", FieldName: "ParentID", GoType: "uuid.NullUUID", DBTag: "parent_id", JSONTag: "parent_id", Nullable: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"id", "name", "parent_id"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&f.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&f.Name"},
			{ColumnName: "parent_id", FieldName: "ParentID", Shape: "direct", ScanExpr: "&f.ParentID"},
		},
		Operations: gen.ResolvedOperations{
			Get:     true,
			GetMany: true,
			Count:   true,
		},
		Dialect:             "postgres",
		Driver:              "pgx",
		QueryLimit:          100,
		HasO2MRelationships: true,
		O2MRelationships: []gen.RelationshipContext{
			{
				Name:             "children",
				Type:             parser.OneToMany,
				TargetTable:      "folders",
				TargetSchema:     "public",
				TargetStructName: "Folder",
				FKColumn:         "parent_id",
				FKFieldName:      "ParentID",
				FieldName:        "Children",
				GoType:           "[]*Folder",
				JSONTag:          "children",
				FKGoType:         "uuid.UUID",
				FKNullable:       true,
				// FK column is nullable uuid.NullUUID (folders.parent_id) —
				// drives the Null-wrapper form: `if r.ParentID.Valid`
				// guard + `r.ParentID.UUID.String()` bucket key.
				FKColumnGoType: "uuid.NullUUID",
			},
		},
	}
	ctx.Relationships = append(ctx.Relationships, ctx.O2MRelationships[0])
	return ctx
}

func TestGetTemplate_o2mRelationshipLoading_nullableStringFK(t *testing.T) {
	ctx := testGetContext_selfRefNullableFK_string()
	output := executeGetTemplate(t, ctx)

	want := "ParentID: &comparator.NullableID{ID: comparator.ID{In: folderIDs}}"
	if !strings.Contains(output, want) {
		t.Errorf("output missing nullable-string-FK pattern: %q\n\nfull output:\n%s", want, output)
	}
	if strings.Contains(output, "ParentID: &comparator.ID{In:") {
		t.Errorf("output emits non-nullable ID for nullable FK column\n\nfull output:\n%s", output)
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "folders_get_gen.go"); err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// testGetContext_subCategorizedO2M pins the per-relationship loader
// emission for sub-categorized O2M relationships: three relationships on
// `asset` share `(target=documents, fk=entity_id)` but each carries its own
// `filter:` discriminator (PRD §13.7.1). The loader must emit one
// `if fo.<Field> != nil` block per relationship, and each block must thread
// the static config `Filter` into the target's `Get<Target>Input.conditions`
// so the discriminator predicate AND's onto the WHERE clause.
func testGetContext_subCategorizedO2M() gen.TableContext {
	ctx := testGetContext_singlePK()
	ctx.StructName = "Asset"
	ctx.TableName = "assets"
	ctx.TableNameConstant = "TableAssets"
	ctx.VarName = "a"
	ctx.Imports = append(ctx.Imports, "golang.org/x/sync/errgroup")
	ctx.HasO2MRelationships = true
	ctx.O2MRelationships = []gen.RelationshipContext{
		{
			Name:             "Attachments",
			Type:             parser.OneToMany,
			TargetTable:      "documents",
			TargetStructName: "Document",
			FKColumn:         "entity_id",
			FKFieldName:      "EntityID",
			FieldName:        "Attachments",
			GoType:           "[]*Document",
			JSONTag:          "attachments",
			Filter:           "entity_type = 'asset.attachment'",
			FKGoType:         "uuid.UUID",
			FKColumnGoType:   "uuid.UUID",
		},
		{
			Name:             "Invoices",
			Type:             parser.OneToMany,
			TargetTable:      "documents",
			TargetStructName: "Document",
			FKColumn:         "entity_id",
			FKFieldName:      "EntityID",
			FieldName:        "Invoices",
			GoType:           "[]*Document",
			JSONTag:          "invoices",
			Filter:           "entity_type = 'asset.invoice'",
			FKGoType:         "uuid.UUID",
			FKColumnGoType:   "uuid.UUID",
		},
	}
	ctx.Relationships = append(
		ctx.Relationships,
		ctx.O2MRelationships[0],
		ctx.O2MRelationships[1],
	)
	return ctx
}

func TestGetTemplate_subCategorizedO2MFilter(t *testing.T) {
	ctx := testGetContext_subCategorizedO2M()
	output := executeGetTemplate(t, ctx)

	// Each relationship emits its own loader block, named per the relationship's
	// FieldName — distinct loaders despite the shared (target, fk) pair.
	wantPatterns := []string{
		"if fo.Attachments != nil {",
		"if fo.Invoices != nil {",
		`conditions: []sql.Condition{sql.Raw("entity_type = 'asset.attachment'")},`,
		`conditions: []sql.Condition{sql.Raw("entity_type = 'asset.invoice'")},`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing sub-categorized O2M pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Two distinct g.Go calls — one per relationship loader.
	if got := strings.Count(output, "g.Go(func() error {"); got != 2 {
		t.Errorf("expected 2 g.Go calls (one per sub-categorized relationship), got %d", got)
	}

	// Without Filter the conditions field must NOT appear at all — guard
	// against an accidental unconditional emission that would prepend an empty
	// `sql.Raw("")` to every loader.
	if strings.Contains(output, `sql.Raw("")`) {
		t.Errorf("output emits empty sql.Raw — Filter conditional likely broken\n\nfull output:\n%s", output)
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "assets_get_gen.go"); err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// TestGetTemplate_subCategorizedM2MFilter pins the M2M variant of
// sub-categorized loaders: a static `filter:` on an M2M relationship lands on
// the target query AFTER the junction resolves target IDs, AND'd alongside
// any consumer-supplied FieldOptions filter (PRD §13.7.3).
func TestGetTemplate_subCategorizedM2MFilter(t *testing.T) {
	ctx := testGetContext_singlePK()
	ctx.StructName = "Asset"
	ctx.TableName = "assets"
	ctx.TableNameConstant = "TableAssets"
	ctx.VarName = "a"
	ctx.Imports = append(ctx.Imports, "golang.org/x/sync/errgroup")
	ctx.M2MRelationships = []gen.RelationshipContext{
		{
			Name:                "Tags",
			Type:                parser.ManyToMany,
			TargetTable:         "tags",
			TargetStructName:    "Tag",
			JunctionTable:       "asset_tags",
			JunctionSchema:      "public",
			JunctionLocalFK:     "asset_id",
			JunctionReferenceFK: "tag_id",
			FieldName:           "Tags",
			GoType:              "[]*Tag",
			JSONTag:             "tags",
			Filter:              "scope = 'asset'",
			FKGoType:            "uuid.UUID",
			TargetPKFieldName:   "ID",
			TargetPKColumn:      "id",
		},
	}
	ctx.HasO2MRelationships = true // M2M loaders live behind this flag too
	ctx.Relationships = append(ctx.Relationships, ctx.M2MRelationships[0])

	output := executeGetTemplate(t, ctx)

	want := `conditions: []sql.Condition{sql.Raw("scope = 'asset'")},`
	if !strings.Contains(output, want) {
		t.Errorf("output missing M2M static filter pattern: %q\n\nfull output:\n%s", want, output)
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "assets_get_gen.go"); err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// TestGetTemplate_o2mNoStaticFilterNoConditions is the negative complement to
// TestGetTemplate_subCategorizedO2MFilter: when `rel.Filter` is empty (the
// default for every example), the loader must NOT emit a
// `conditions:` field. This pins byte-stability of every example golden.
func TestGetTemplate_o2mNoStaticFilterNoConditions(t *testing.T) {
	ctx := testGetContext_withRelationships()
	output := executeGetTemplate(t, ctx)

	if strings.Contains(output, "conditions: []sql.Condition{sql.Raw(") {
		t.Errorf("loader emits conditions:sql.Raw despite empty rel.Filter — would break every existing golden\n\nfull output:\n%s", output)
	}
}
