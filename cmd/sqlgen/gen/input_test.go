package gen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadInputTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("input").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	return tmpl
}

func executeCreateInputTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadInputTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/create-input", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

func executeUpdateInputTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadInputTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/update-input", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

func executeGetInputTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadInputTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/get-input", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

// testInputProductsContext returns a products table context with UUID PK (strategy db),
// soft delete, and update_columns — the reference schema from the PRD.
func testInputProductsContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		PKStrategy:        config.PKStrategyDB,
		CompositePK:       false,
		SoftDelete:        &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
		UpdateColumns:     []gen.UpdateColumnContext{{Name: "updated_at", FieldName: "UpdatedAt"}},
		Columns: []gen.ColumnContext{
			{FieldName: "ID", Name: "id", GoType: "uuid.UUID", Import: "github.com/google/uuid", PrimaryKey: true, HasDefault: true, DBTag: "id", JSONTag: "id", SQLType: "uuid"},
			{FieldName: "Name", Name: "name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{FieldName: "Price", Name: "price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{FieldName: "SKU", Name: "sku", GoType: "string", DBTag: "sku", JSONTag: "sku", Unique: true},
			{FieldName: "CompanyID", Name: "company_id", GoType: "*uuid.UUID", Import: "github.com/google/uuid", Nullable: true, DBTag: "company_id", JSONTag: "company_id"},
			{FieldName: "DeletedAt", Name: "deleted_at", GoType: "*time.Time", Import: "time", Nullable: true, DBTag: "deleted_at", JSONTag: "deleted_at"},
			{FieldName: "UpdatedAt", Name: "updated_at", GoType: "time.Time", Import: "time", HasDefault: true, DBTag: "updated_at", JSONTag: "updated_at"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "ID", GoType: "omittable.Value[uuid.UUID]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "id", Omittable: true, JSONTag: "id"},
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "float64", ColumnName: "price", Required: true, JSONTag: "price"},
			{FieldName: "SKU", GoType: "string", ColumnName: "sku", Required: true, JSONTag: "sku"},
			{FieldName: "CompanyID", GoType: "omittable.Value[*uuid.UUID]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "company_id", Omittable: true, JSONTag: "company_id"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
			{FieldName: "UpdatedAt", GoType: "omittable.Value[time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "updated_at", Omittable: true, JSONTag: "updated_at"},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "omittable.Value[string]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "name", Omittable: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "omittable.Value[float64]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "price", Omittable: true, JSONTag: "price"},
			{FieldName: "SKU", GoType: "omittable.Value[string]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "sku", Omittable: true, JSONTag: "sku"},
			{FieldName: "CompanyID", GoType: "omittable.Value[*uuid.UUID]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "company_id", Omittable: true, JSONTag: "company_id"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
			{FieldName: "UpdatedAt", GoType: "omittable.Value[time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "updated_at", Omittable: true, JSONTag: "updated_at"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "*comparator.ID", ColumnName: "id"},
		},
	}
}

// testInputCallerPKContext returns a context where PK strategy is "caller" (composite PK).
func testInputCallerPKContext() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Package:               "db",
		Dialect:               "postgres",
		PKStrategy:            config.PKStrategyCaller,
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		SoftDelete:            &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
		PKColumns: []gen.ColumnContext{
			{FieldName: "OrderID", Name: "order_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "order_id", JSONTag: "order_id"},
			{FieldName: "ProductID", Name: "product_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "product_id", JSONTag: "product_id"},
		},
		Columns: []gen.ColumnContext{
			{FieldName: "OrderID", Name: "order_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "order_id", JSONTag: "order_id"},
			{FieldName: "ProductID", Name: "product_id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "product_id", JSONTag: "product_id"},
			{FieldName: "Quantity", Name: "quantity", GoType: "int32", HasDefault: true, DBTag: "quantity", JSONTag: "quantity"},
			{FieldName: "UnitPrice", Name: "unit_price", GoType: "float64", DBTag: "unit_price", JSONTag: "unit_price"},
			{FieldName: "DeletedAt", Name: "deleted_at", GoType: "*time.Time", Nullable: true, DBTag: "deleted_at", JSONTag: "deleted_at"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", ColumnName: "order_id", Required: true, JSONTag: "order_id"},
			{FieldName: "ProductID", GoType: "uuid.UUID", ColumnName: "product_id", Required: true, JSONTag: "product_id"},
			{FieldName: "Quantity", GoType: "omittable.Value[int32]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
			{FieldName: "UnitPrice", GoType: "float64", ColumnName: "unit_price", Required: true, JSONTag: "unit_price"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Quantity", GoType: "omittable.Value[int32]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
			{FieldName: "UnitPrice", GoType: "omittable.Value[float64]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "unit_price", Omittable: true, JSONTag: "unit_price"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "OrderID", ComparatorType: "*comparator.ID", ColumnName: "order_id"},
		},
	}
}

// testInputSerialPKContext returns a context with a serial/auto-increment PK (excluded from CreateInput).
func testInputSerialPKContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Event",
		TableName:         "events",
		TableNameConstant: "TableEvents",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		PKStrategy:        config.PKStrategyDB,
		CompositePK:       false,
		Columns: []gen.ColumnContext{
			{FieldName: "ID", Name: "id", GoType: "int64", PrimaryKey: true, AutoIncrement: true, DBTag: "id", JSONTag: "id", SQLType: "bigserial"},
			{FieldName: "Name", Name: "name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		CreateInputFields: []gen.InputFieldContext{
			// ID excluded — auto-increment PK
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "omittable.Value[string]", Import: "github.com/teandresmith/sqlgen/omittable", ColumnName: "name", Omittable: true, JSONTag: "name"},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "*comparator.Number[int64]", ColumnName: "id"},
		},
	}
}

// --- CreateInput tests ---

func TestCreateInputTemplate_dbStrategyUUIDPK_omittable(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeCreateInputTemplate(t, ctx)

	wantPatterns := []string{
		"type CreateProductInput struct {",
		`ID omittable.Value[uuid.UUID]`,
		`json:"id,omitzero"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestCreateInputTemplate_callerPK_required(t *testing.T) {
	ctx := testInputCallerPKContext()
	output := executeCreateInputTemplate(t, ctx)

	wantPatterns := []string{
		"type CreateOrderItemInput struct {",
		`OrderID uuid.UUID`,
		`json:"order_id"`,
		`ProductID uuid.UUID`,
		`json:"product_id"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Caller PK should NOT have omitzero.
	if strings.Contains(output, `json:"order_id,omitzero"`) {
		t.Error("caller PK should not use omitzero tag")
	}
}

func TestCreateInputTemplate_serialPK_excluded(t *testing.T) {
	ctx := testInputSerialPKContext()
	output := executeCreateInputTemplate(t, ctx)

	if strings.Contains(output, "\tID ") {
		t.Errorf("serial PK should be excluded from CreateInput\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "type CreateEventInput struct {") {
		t.Error("output missing CreateEventInput struct")
	}
	if !strings.Contains(output, `Name string`) {
		t.Error("output missing required Name field")
	}
}

func TestCreateInputTemplate_requiredVsOmittableFields(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeCreateInputTemplate(t, ctx)

	// Required fields: bare type, no omitzero.
	requiredPatterns := []string{
		`Name string`,
		`json:"name"`,
		`Price float64`,
		`json:"price"`,
		`SKU string`,
		`json:"sku"`,
	}
	for _, want := range requiredPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing required pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Required fields should NOT have omitzero.
	if strings.Contains(output, `json:"name,omitzero"`) {
		t.Error("required field Name should not use omitzero")
	}

	// Omittable fields: omittable.Value[T], omitzero.
	omittablePatterns := []string{
		`CompanyID omittable.Value[*uuid.UUID]`,
		`json:"company_id,omitzero"`,
		`DeletedAt omittable.Value[*time.Time]`,
		`json:"deleted_at,omitzero"`,
		`UpdatedAt omittable.Value[time.Time]`,
		`json:"updated_at,omitzero"`,
	}
	for _, want := range omittablePatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing omittable pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- UpdateInput tests ---

func TestUpdateInputTemplate_allFieldsOmittable(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeUpdateInputTemplate(t, ctx)

	wantPatterns := []string{
		"type UpdateProductInput struct {",
		`Name omittable.Value[string]`,
		`json:"name,omitzero"`,
		`Price omittable.Value[float64]`,
		`json:"price,omitzero"`,
		`SKU omittable.Value[string]`,
		`json:"sku,omitzero"`,
		`CompanyID omittable.Value[*uuid.UUID]`,
		`json:"company_id,omitzero"`,
		`DeletedAt omittable.Value[*time.Time]`,
		`json:"deleted_at,omitzero"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateInputTemplate_PKExcluded(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeUpdateInputTemplate(t, ctx)

	if strings.Contains(output, "\tID ") {
		t.Errorf("PK should be excluded from UpdateInput\n\nfull output:\n%s", output)
	}
}

func TestUpdateInputTemplate_updateColumnsIncluded(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeUpdateInputTemplate(t, ctx)

	if !strings.Contains(output, "UpdatedAt") {
		t.Errorf("update_columns should be included in UpdateInput as omittable\n\nfull output:\n%s", output)
	}
}

func TestUpdateInputTemplate_compositePK_excludesPK(t *testing.T) {
	ctx := testInputCallerPKContext()
	output := executeUpdateInputTemplate(t, ctx)

	if strings.Contains(output, "OrderID") {
		t.Error("composite PK column OrderID should be excluded from UpdateInput")
	}
	if strings.Contains(output, "ProductID") {
		t.Error("composite PK column ProductID should be excluded from UpdateInput")
	}
	if !strings.Contains(output, "Quantity omittable.Value[int32]") {
		t.Error("output missing Quantity field")
	}
}

// --- GetInput tests ---

func TestGetInputTemplate_allQueryFields(t *testing.T) {
	ctx := testInputProductsContext()
	output := executeGetInputTemplate(t, ctx)

	wantPatterns := []string{
		"type GetProductsInput struct {",
		`Filter *ProductFilter`,
		`json:"filter"`,
		`Limit  *int`,
		`json:"limit"`,
		`Offset *int`,
		`json:"offset"`,
		`Sorts  []sql.Sort`,
		`json:"sorts"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// FieldOptions should NOT be a struct field on GetInput — it belongs on CallOptions.
	if strings.Contains(output, "FieldOptions *") {
		t.Error("GetInput should not contain FieldOptions field — field selection is via CallOptions")
	}
}

func TestGetInputTemplate_compositePK_pluralName(t *testing.T) {
	ctx := testInputCallerPKContext()
	output := executeGetInputTemplate(t, ctx)

	if !strings.Contains(output, "type GetOrderItemsInput struct {") {
		t.Errorf("output missing GetOrderItemsInput struct\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "*OrderItemFilter") {
		t.Errorf("output missing OrderItemFilter reference\n\nfull output:\n%s", output)
	}

	// FieldOptions should NOT be a struct field on GetInput.
	if strings.Contains(output, "FieldOptions *") {
		t.Error("GetInput should not contain FieldOptions field — field selection is via CallOptions")
	}
}

// --- Compilation tests ---

func TestCreateInputTemplate_compilesCleanly(t *testing.T) {
	ctx := testInputProductsContext()
	output := wrapInputForCompile(
		ctx,
		executeCreateInputTemplate(t, ctx),
		executeUpdateInputTemplate(t, ctx),
		executeGetInputTemplate(t, ctx),
	)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "input_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestInputTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testInputCallerPKContext()
	output := wrapInputForCompile(
		ctx,
		executeCreateInputTemplate(t, ctx),
		executeUpdateInputTemplate(t, ctx),
		executeGetInputTemplate(t, ctx),
	)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "input_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestInputTemplate_compilesCleanly_serialPK(t *testing.T) {
	ctx := testInputSerialPKContext()
	output := wrapInputForCompile(
		ctx,
		executeCreateInputTemplate(t, ctx),
		executeUpdateInputTemplate(t, ctx),
		executeGetInputTemplate(t, ctx),
	)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "input_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Golden file tests ---

func TestInputTemplate_goldenFile_products(t *testing.T) {
	ctx := testInputProductsContext()
	output := wrapInputForCompile(
		ctx,
		executeCreateInputTemplate(t, ctx),
		executeUpdateInputTemplate(t, ctx),
		executeGetInputTemplate(t, ctx),
	)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "input_products_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "input_products_gen.go")

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
		t.Errorf("input_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestInputTemplate_goldenFile_orderItems(t *testing.T) {
	ctx := testInputCallerPKContext()
	output := wrapInputForCompile(
		ctx,
		executeCreateInputTemplate(t, ctx),
		executeUpdateInputTemplate(t, ctx),
		executeGetInputTemplate(t, ctx),
	)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "input_order_items_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "input_order_items_gen.go")

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
		t.Errorf("input_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// wrapInputForCompile wraps input template fragments in a compilable Go file.
func wrapInputForCompile(ctx gen.TableContext, fragments ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", ctx.Package)
	b.WriteString("import (\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/omittable\"\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/sql\"\n")

	// Collect imports from input fields.
	imports := make(map[string]bool)
	for _, f := range ctx.CreateInputFields {
		if f.Import != "" && f.Import != "github.com/teandresmith/sqlgen/omittable" {
			imports[f.Import] = true
		}
	}
	for _, f := range ctx.UpdateInputFields {
		if f.Import != "" && f.Import != "github.com/teandresmith/sqlgen/omittable" {
			imports[f.Import] = true
		}
	}
	for imp := range imports {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString(")\n")

	// Stub types so the generated code compiles.
	fmt.Fprintf(&b, "\ntype %sFilter struct{}\n", ctx.StructName)
	fmt.Fprintf(&b, "type %sFieldOptions struct{}\n", ctx.StructName)

	for _, fragment := range fragments {
		b.WriteString(fragment)
	}
	return b.String()
}
