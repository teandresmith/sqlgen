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

func loadTableNameTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "tablename.go.tmpl")
	tmpl, err := template.New("tablename.go.tmpl").Funcs(gen.FuncMap(sql.NewPostgresDialect())).ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	return tmpl
}

func executeTableNameTemplate(t *testing.T, ctx gen.TableNameFileContext) string {
	t.Helper()
	tmpl := loadTableNameTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "tablename", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

func testTableNameFileContext() gen.TableNameFileContext {
	return gen.TableNameFileContext{
		Package: "db",
		Imports: []string{
			"github.com/teandresmith/sqlgen/hook",
		},
		Tables: []gen.TableNameEntry{
			{ConstantName: "TableOrderItems", SQLName: "order_items", Value: "public.order_items"},
			{ConstantName: "TableProducts", SQLName: "products", Value: "public.products"},
			{ConstantName: "TableUsers", SQLName: "users", Value: "public.users"},
		},
		Views: []gen.TableNameEntry{
			{ConstantName: "TableProductSummaries", SQLName: "product_summaries", Value: "public.product_summaries"},
		},
	}
}

func TestTableNameTemplate_goldenFile(t *testing.T) {
	ctx := testTableNameFileContext()
	output := executeTableNameTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "tablenames_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "tablenames_gen.go")

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
		t.Errorf("tablenames_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestTableNameTemplate_compilesCleanly(t *testing.T) {
	ctx := testTableNameFileContext()
	output := executeTableNameTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "tablenames_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestTableNameTemplate_tableConstants(t *testing.T) {
	ctx := gen.TableNameFileContext{
		Package: "db",
		Imports: []string{
			"github.com/teandresmith/sqlgen/hook",
		},
		Tables: []gen.TableNameEntry{
			{ConstantName: "TableProducts", SQLName: "products", Value: "public.products"},
			{ConstantName: "TableUsers", SQLName: "users", Value: "users"},
		},
	}
	output := executeTableNameTemplate(t, ctx)

	wantPatterns := []string{
		`TableProducts hook.TableName = "public.products"`,
		`TableUsers hook.TableName = "users"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTableNameTemplate_viewConstants(t *testing.T) {
	ctx := gen.TableNameFileContext{
		Package: "db",
		Imports: []string{
			"github.com/teandresmith/sqlgen/hook",
		},
		Tables: []gen.TableNameEntry{
			{ConstantName: "TableProducts", SQLName: "products", Value: "public.products"},
		},
		Views: []gen.TableNameEntry{
			{ConstantName: "TableProductSummaries", SQLName: "product_summaries", Value: "public.product_summaries"},
		},
	}
	output := executeTableNameTemplate(t, ctx)

	wantPatterns := []string{
		`TableProducts hook.TableName = "public.products"`,
		`TableProductSummaries hook.TableName = "public.product_summaries"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}
