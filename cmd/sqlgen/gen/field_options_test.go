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
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

func loadFieldOptionsTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("field_options").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	return tmpl
}

func executeFieldOptionsTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadFieldOptionsTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field-options", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

func testFieldOptionsTableContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		CompositePK:       false,
		Columns: []gen.ColumnContext{
			{FieldName: "ID", Name: "id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "id", JSONTag: "id"},
			{FieldName: "Name", Name: "name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{FieldName: "Price", Name: "price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{FieldName: "CompanyID", Name: "company_id", GoType: "uuid.NullUUID", Nullable: true, DBTag: "company_id", JSONTag: "company_id"},
			{FieldName: "CreatedAt", Name: "created_at", GoType: "time.Time", DBTag: "created_at", JSONTag: "created_at"},
		},
		Relationships: []gen.RelationshipContext{
			{
				Name:             "company",
				Type:             parser.OneToOne,
				TargetTable:      "companies",
				TargetStructName: "Company",
				FKColumn:         "company_id",
				FieldName:        "Company",
				GoType:           "*Company",
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
				JSONTag:          "tags",
			},
		},
		RelationshipOptionsDefs: []gen.RelationshipOptionsDef{
			{StructName: "ReviewRelationshipOptions", TargetStructName: "Review"},
			{StructName: "TagRelationshipOptions", TargetStructName: "Tag"},
		},
	}
}

func testFieldOptionsNoRelsContext() gen.TableContext {
	return gen.TableContext{
		StructName:        "Tag",
		TableName:         "tags",
		TableNameConstant: "TableTags",
		Schema:            "public",
		Package:           "db",
		Dialect:           "postgres",
		CompositePK:       false,
		Columns: []gen.ColumnContext{
			{FieldName: "ID", Name: "id", GoType: "uuid.UUID", PrimaryKey: true, DBTag: "id", JSONTag: "id"},
			{FieldName: "Name", Name: "name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
	}
}

func TestFieldOptionsTemplate_columnBooleans(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"type ProductFieldOptions struct {",
		"ID bool `json:\"id\"`",
		"Name bool `json:\"name\"`",
		"Price bool `json:\"price\"`",
		"CompanyID bool `json:\"company_id\"`",
		"CreatedAt bool `json:\"created_at\"`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_o2oRelationship(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	if !strings.Contains(output, "Company *CompanyFieldOptions `json:\"company\"`") {
		t.Errorf("output missing O2O relationship field\n\nfull output:\n%s", output)
	}
}

func TestFieldOptionsTemplate_o2mM2mRelationships(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"Reviews *ReviewRelationshipOptions `json:\"reviews\"`",
		"Tags *TagRelationshipOptions `json:\"tags\"`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_relationshipOptionsStructs(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"type ReviewRelationshipOptions struct {",
		"FieldOptions *ReviewFieldOptions",
		`json:"field_options"`,
		"Filter       *ReviewFilter",
		`json:"filter"`,
		"Sorts        []sql.Sort",
		`json:"sorts"`,
		"type TagRelationshipOptions struct {",
		"FieldOptions *TagFieldOptions",
		"Filter       *TagFilter",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_columnsMethod(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"func (fo *ProductFieldOptions) Columns() []string {",
		`if fo.ID {`,
		`cols = append(cols, "id")`,
		`if fo.Name {`,
		`cols = append(cols, "name")`,
		`if fo.Price {`,
		`cols = append(cols, "price")`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_columnMapMethod(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"func (fo *ProductFieldOptions) ColumnMap() map[string]string {",
		`"ID": "id"`,
		`"Name": "name"`,
		`"Price": "price"`,
		`"CompanyID": "company_id"`,
		`"CreatedAt": "created_at"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_hasSelectedColumnsMethod(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := executeFieldOptionsTemplate(t, ctx)

	wantPatterns := []string{
		"func (fo *ProductFieldOptions) HasSelectedColumns() bool {",
		"if fo.ID {",
		"if fo.Company != nil {",
		"if fo.Reviews != nil {",
		"if fo.Tags != nil {",
		"return false",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestFieldOptionsTemplate_noRelationships(t *testing.T) {
	ctx := testFieldOptionsNoRelsContext()
	output := executeFieldOptionsTemplate(t, ctx)

	if !strings.Contains(output, "type TagFieldOptions struct {") {
		t.Error("output missing FieldOptions struct")
	}
	if strings.Contains(output, "RelationshipOptions") {
		t.Error("output should not contain RelationshipOptions when no O2M/M2M relationships")
	}
	if strings.Contains(output, "!= nil") {
		t.Error("output should not contain nil checks when no relationships")
	}
}

func TestFieldOptionsTemplate_compilesCleanly(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := wrapFieldOptionsForCompile(ctx, executeFieldOptionsTemplate(t, ctx))

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "field_options_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestFieldOptionsTemplate_goldenFile(t *testing.T) {
	ctx := testFieldOptionsTableContext()
	output := wrapFieldOptionsForCompile(ctx, executeFieldOptionsTemplate(t, ctx))

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "field_options_products_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "field_options_products_gen.go")

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
		t.Errorf("field_options_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// wrapFieldOptionsForCompile wraps the field options template output in a compilable Go file.
func wrapFieldOptionsForCompile(ctx gen.TableContext, fragment string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", ctx.Package)
	b.WriteString("import (\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/comparator\"\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/sql\"\n")
	b.WriteString(")\n")
	// Stub types so the generated code compiles.
	for _, rel := range ctx.Relationships {
		if rel.Type == parser.OneToOne {
			fmt.Fprintf(&b, "\ntype %sFieldOptions struct{}\n", rel.TargetStructName)
		} else {
			fmt.Fprintf(&b, "\ntype %sFieldOptions struct{}\n", rel.TargetStructName)
			fmt.Fprintf(&b, "type %sFilter struct{}\n", rel.TargetStructName)
		}
	}
	b.WriteString(fragment)
	return b.String()
}
