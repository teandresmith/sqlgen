package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

func loadModelTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("model.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "model.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing model template: %v", err)
	}
	return tmpl
}

func executeModelTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadModelTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/model", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func testSimpleTableContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Description:       "Catalog of products available for sale.",
		Package:           "db",
		Imports:           []string{"time", "github.com/gofrs/uuid/v5"},
		Columns: []gen.ColumnContext{
			{FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Description: "Unique identifier for the product.", Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name", Description: "Display name shown in storefront."},
			{FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price", Description: "Unit price in USD."},
			{FieldName: "SKU", GoType: "string", DBTag: "sku", JSONTag: "sku", Description: "Stock keeping unit, unique across catalog."},
			{FieldName: "CompanyID", GoType: "uuid.NullUUID", DBTag: "company_id", JSONTag: "company_id", Nullable: true, Description: "References the manufacturing company.", Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "DeletedAt", GoType: "*time.Time", DBTag: "deleted_at", JSONTag: "deleted_at", Nullable: true, Description: "Soft delete timestamp.", Import: "time"},
			{FieldName: "UpdatedAt", GoType: "time.Time", DBTag: "updated_at", JSONTag: "updated_at", Description: "Last modification timestamp.", Import: "time"},
		},
		PKColumns: []gen.ColumnContext{
			{FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Description: "Unique identifier for the product.", Import: "github.com/gofrs/uuid/v5"},
		},
	}
}

func testTableContextWithRelationships() gen.TableContext {
	ctx := testSimpleTableContext()
	ctx.Relationships = []gen.RelationshipContext{
		{
			Name:             "company",
			Type:             parser.OneToOne,
			TargetTable:      "companies",
			TargetStructName: "Company",
			FKColumn:         "company_id",
			FieldName:        "Company",
			GoType:           "*Company",
			Description:      "one-to-one relationship with the companies table.",
			JSONTag:          "company",
		},
		{
			Name:             "reviews",
			Type:             parser.OneToMany,
			TargetTable:      "reviews",
			TargetStructName: "Review",
			FKColumn:         "product_id",
			FieldName:        "Reviews",
			GoType:           "[]*Review",
			Description:      "one-to-many relationship with the reviews table.",
			JSONTag:          "reviews",
		},
		{
			Name:             "tags",
			Type:             parser.ManyToMany,
			TargetTable:      "tags",
			TargetStructName: "Tag",
			JunctionTable:    "product_tags",
			FieldName:        "Tags",
			GoType:           "[]*Tag",
			Description:      "many-to-many relationship with the tags table.",
			JSONTag:          "tags",
		},
	}
	return ctx
}

func testCompositePKContext() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Description:           "Line item in an order.",
		Package:               "db",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Imports:               []string{"github.com/gofrs/uuid/v5"},
		Columns: []gen.ColumnContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Description: "References the parent order.", Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Description: "References the purchased product.", Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity", Description: "Number of units ordered."},
		},
		PKColumns: []gen.ColumnContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Description: "References the parent order.", Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Description: "References the purchased product.", Import: "github.com/gofrs/uuid/v5"},
		},
	}
}

func TestModelTemplate_simpleTable(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	wantPatterns := []string{
		"type Product struct {",
		"ID uuid.UUID",
		"Name string",
		"Price float64",
		"CompanyID uuid.NullUUID",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestModelTemplate_fieldDescriptions(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	wantPatterns := []string{
		"// ID - Unique identifier for the product.",
		"// Name - Display name shown in storefront.",
		"// Price - Unit price in USD.",
		"// SKU - Stock keeping unit, unique across catalog.",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing field description: %q", want)
		}
	}
}

func TestModelTemplate_structTags(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	wantPatterns := []string{
		`db:"id" json:"id"`,
		`db:"name" json:"name"`,
		`db:"price" json:"price"`,
		`db:"sku" json:"sku"`,
		`db:"company_id" json:"company_id"`,
		`db:"deleted_at" json:"deleted_at"`,
		`db:"updated_at" json:"updated_at"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing tag: %q", want)
		}
	}
}

func TestModelTemplate_dbTagBeforeJSON(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	for _, col := range ctx.Columns {
		dbTag := `db:"` + col.DBTag + `"`
		jsonTag := `json:"` + col.JSONTag + `"`
		dbIdx := strings.Index(output, dbTag)
		jsonIdx := strings.Index(output, jsonTag)
		if dbIdx < 0 || jsonIdx < 0 {
			t.Fatalf("missing tags for %s in output", col.FieldName)
		}
		if dbIdx >= jsonIdx {
			t.Errorf("db tag should come before json tag for %s", col.FieldName)
		}
	}
}

func TestModelTemplate_docComment(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	if !strings.Contains(output, "// Product - Catalog of products available for sale.") {
		t.Error("output missing doc comment for struct")
	}
}

func TestModelTemplate_noDocCommentWhenEmpty(t *testing.T) {
	ctx := testSimpleTableContext()
	ctx.Description = ""
	output := executeModelTemplate(t, ctx)

	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "type Product struct") {
			if i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "//") {
				t.Error("unexpected doc comment before struct with empty description")
			}
			break
		}
	}
}

func TestModelTemplate_relationships(t *testing.T) {
	ctx := testTableContextWithRelationships()
	output := executeModelTemplate(t, ctx)

	wantPatterns := []string{
		"// Company - one-to-one relationship with the companies table.",
		"Company *Company `db:\"-\" json:\"company\"`",
		"// Reviews - one-to-many relationship with the reviews table.",
		"Reviews []*Review `db:\"-\" json:\"reviews\"`",
		"// Tags - many-to-many relationship with the tags table.",
		"Tags []*Tag `db:\"-\" json:\"tags\"`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestModelTemplate_noRelationshipSection_whenNone(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	if strings.Contains(output, "relationship with") {
		t.Error("output should not contain relationship fields when there are no relationships")
	}
}

func TestModelTemplate_compositePKStruct(t *testing.T) {
	ctx := testCompositePKContext()
	output := executeModelTemplate(t, ctx)

	wantPatterns := []string{
		"// OrderItemPK - composite primary key for the order_items table.",
		"type OrderItemPK struct {",
		`OrderID uuid.UUID`,
		`ProductID uuid.UUID`,
		`db:"order_id" json:"order_id"`,
		`db:"product_id" json:"product_id"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestModelTemplate_noCompositePKStruct_whenSinglePK(t *testing.T) {
	ctx := testSimpleTableContext()
	output := executeModelTemplate(t, ctx)

	if strings.Contains(output, "PK struct") || strings.Contains(output, "ProductPK") {
		t.Error("output should not contain composite PK struct for single PK table")
	}
}

func TestModelTemplate_compilesCleanly(t *testing.T) {
	ctx := testTableContextWithRelationships()
	output := executeModelTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestModelTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testCompositePKContext()
	output := executeModelTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestModelTemplate_goldenFile(t *testing.T) {
	ctx := testTableContextWithRelationships()
	output := executeModelTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "model_products_gen.go")

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
		t.Errorf("model_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestModelTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testCompositePKContext()
	output := executeModelTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "model_order_items_gen.go")

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
		t.Errorf("model_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}
