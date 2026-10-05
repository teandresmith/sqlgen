package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadSorterTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "sorter.go.tmpl")
	tmpl, err := template.New("sorter.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing sorter template: %v", err)
	}
	return tmpl
}

func executeSorterTemplate(t *testing.T, ctx gen.SorterContext) string {
	t.Helper()
	tmpl := loadSorterTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "sorter", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

func testSorterContext() gen.SorterContext {
	return gen.SorterContext{
		Package: "db",
		Imports: []string{"github.com/teandresmith/sqlgen/sql"},
		Tables: []gen.SorterTableContext{
			{
				StructName:     "Product",
				SorterTypeName: "ProductSorter",
				Columns: []gen.SorterColumnContext{
					{ColumnName: "id", MethodName: "ID"},
					{ColumnName: "name", MethodName: "Name"},
					{ColumnName: "price", MethodName: "Price"},
				},
			},
		},
	}
}

func testSorterContext_multipleTables() gen.SorterContext {
	return gen.SorterContext{
		Package: "db",
		Imports: []string{"github.com/teandresmith/sqlgen/sql"},
		Tables: []gen.SorterTableContext{
			{
				StructName:     "Order",
				SorterTypeName: "OrderSorter",
				Columns: []gen.SorterColumnContext{
					{ColumnName: "created_at", MethodName: "CreatedAt"},
					{ColumnName: "id", MethodName: "ID"},
				},
			},
			{
				StructName:     "Product",
				SorterTypeName: "ProductSorter",
				Columns: []gen.SorterColumnContext{
					{ColumnName: "id", MethodName: "ID"},
					{ColumnName: "name", MethodName: "Name"},
					{ColumnName: "price", MethodName: "Price"},
				},
			},
		},
	}
}

// --- Test: Sorter struct and method generation per table ---

func TestSorterTemplate_perTableMethods(t *testing.T) {
	ctx := testSorterContext()
	output := executeSorterTemplate(t, ctx)

	wantPatterns := []string{
		"type ProductSorter struct{}",
		"func NewProductSorter() ProductSorter",
		"func (s ProductSorter) ID(direction sql.SortDirection) sql.Sort",
		`Column: "id", Direction: direction`,
		"func (s ProductSorter) Name(direction sql.SortDirection) sql.Sort",
		`Column: "name", Direction: direction`,
		"func (s ProductSorter) Price(direction sql.SortDirection) sql.Sort",
		`Column: "price", Direction: direction`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Multiple tables ---

func TestSorterTemplate_multipleTables(t *testing.T) {
	ctx := testSorterContext_multipleTables()
	output := executeSorterTemplate(t, ctx)

	wantPatterns := []string{
		"type OrderSorter struct{}",
		"func NewOrderSorter() OrderSorter",
		"func (s OrderSorter) CreatedAt(direction sql.SortDirection) sql.Sort",
		"func (s OrderSorter) ID(direction sql.SortDirection) sql.Sort",
		"type ProductSorter struct{}",
		"func NewProductSorter() ProductSorter",
		"func (s ProductSorter) ID(direction sql.SortDirection) sql.Sort",
		"func (s ProductSorter) Name(direction sql.SortDirection) sql.Sort",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Returns sql.Sort with SortDirection ---

func TestSorterTemplate_returnsSQLSort(t *testing.T) {
	ctx := testSorterContext()
	output := executeSorterTemplate(t, ctx)

	if !strings.Contains(output, "sql.Sort{") {
		t.Errorf("output missing sql.Sort literal\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "sql.SortDirection") {
		t.Errorf("output missing sql.SortDirection parameter\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, `"github.com/teandresmith/sqlgen/sql"`) {
		t.Errorf("output missing sql import\n\nfull output:\n%s", output)
	}
}

// --- Test: Doc comments ---

func TestSorterTemplate_docComments(t *testing.T) {
	ctx := testSorterContext()
	output := executeSorterTemplate(t, ctx)

	wantPatterns := []string{
		"// ProductSorter provides type-safe sort helpers for the Product entity.",
		"// Name returns a sort for the name column in the given direction.",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing doc comment: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Compiles cleanly ---

func TestSorterTemplate_compilesCleanly(t *testing.T) {
	ctx := testSorterContext()
	output := executeSorterTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "sorter_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Test: Golden file ---

func TestSorterTemplate_goldenFile(t *testing.T) {
	ctx := testSorterContext()
	output := executeSorterTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "sorter_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "sorter_products_gen.go")

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
		t.Errorf("sorter_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: BuildSorterContext ---

func TestBuildSorterContext_sortsTablesAndColumns(t *testing.T) {
	tables := []gen.TableContext{
		{
			StructName: "Product",
			Columns: []gen.ColumnContext{
				{Name: "price", FieldName: "Price"},
				{Name: "id", FieldName: "ID"},
				{Name: "name", FieldName: "Name"},
			},
		},
		{
			StructName: "Order",
			Columns: []gen.ColumnContext{
				{Name: "id", FieldName: "ID"},
				{Name: "created_at", FieldName: "CreatedAt"},
			},
		},
	}

	ctx := gen.BuildSorterContext("db", tables)

	if ctx.Package != "db" {
		t.Errorf("Package = %q, want %q", ctx.Package, "db")
	}

	if len(ctx.Tables) != 2 {
		t.Fatalf("Tables has %d entries, want 2", len(ctx.Tables))
	}

	// Tables sorted by struct name
	if ctx.Tables[0].StructName != "Order" {
		t.Errorf("Tables[0].StructName = %q, want %q", ctx.Tables[0].StructName, "Order")
	}
	if ctx.Tables[0].SorterTypeName != "OrderSorter" {
		t.Errorf("Tables[0].SorterTypeName = %q, want %q", ctx.Tables[0].SorterTypeName, "OrderSorter")
	}
	if ctx.Tables[1].StructName != "Product" {
		t.Errorf("Tables[1].StructName = %q, want %q", ctx.Tables[1].StructName, "Product")
	}

	// Columns sorted by column name
	if ctx.Tables[0].Columns[0].ColumnName != "created_at" {
		t.Errorf("Order columns[0] = %q, want %q", ctx.Tables[0].Columns[0].ColumnName, "created_at")
	}
	if ctx.Tables[0].Columns[0].MethodName != "CreatedAt" {
		t.Errorf("Order columns[0].MethodName = %q, want %q", ctx.Tables[0].Columns[0].MethodName, "CreatedAt")
	}
	if ctx.Tables[1].Columns[0].ColumnName != "id" {
		t.Errorf("Product columns[0] = %q, want %q", ctx.Tables[1].Columns[0].ColumnName, "id")
	}
}
