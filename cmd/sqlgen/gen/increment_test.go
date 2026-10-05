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
	"github.com/teandresmith/sqlgen/sql"
)

func loadIncrementTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("increment.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "increment.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing increment template: %v", err)
	}
	return tmpl
}

func executeIncrementTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadIncrementTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/increment", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testIncrementContext_postgres returns a product context with increment columns, PostgreSQL.
func testIncrementContext_postgres() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyDB,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		StrictUpdates: true,
		IncrementColumns: []gen.ColumnContext{
			{FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{FieldName: "Stock", GoType: "int32", DBTag: "stock", JSONTag: "stock"},
		},
		Operations: gen.ResolvedOperations{
			Increment: true,
		},
	}
}

// testIncrementContext_noStrictUpdates returns a context without strict_updates.
func testIncrementContext_noStrictUpdates() gen.TableContext {
	ctx := testIncrementContext_postgres()
	ctx.StrictUpdates = false
	return ctx
}

// testIncrementContext_compositePK returns a context with composite PK.
func testIncrementContext_compositePK() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Package:               "db",
		VarName:               "o",
		Dialect:               "postgres",
		Driver:                "pgx",
		PKStrategy:            config.PKStrategyCaller,
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		StrictUpdates: true,
		IncrementColumns: []gen.ColumnContext{
			{FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		Operations: gen.ResolvedOperations{
			Increment: true,
		},
	}
}

// --- IncrementColumn enum tests ---

func TestIncrementTemplate_enumGeneration(t *testing.T) {
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"type ProductIncrementColumn string",
		`ProductIncrementPrice ProductIncrementColumn = "price"`,
		`ProductIncrementStock ProductIncrementColumn = "stock"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestIncrementTemplate_enumExcludesPKAndNonNumeric(t *testing.T) {
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	// PK column "id" should not appear in the enum
	if strings.Contains(output, "ProductIncrementID") {
		t.Errorf("IncrementColumn enum should not include PK column\n\nfull output:\n%s", output)
	}
}

// --- Increment method tests ---

func TestIncrementTemplate_positiveAmount(t *testing.T) {
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Increment(ctx context.Context, id uuid.UUID, input IncrementInput[ProductIncrementColumn], opts ...func(*CallOptions[ProductFieldOptions])) error",
		"conn := database.Conn(ctx, c.querier)",
		"query, args := sql.BuildIncrement(c.dialect, c.table, string(input.Column), input.Amount, []sql.Condition{",
		`sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)`,
		"execResult, err := conn.Exec(ctx, query, args...)",
		`fmt.Errorf("increment product %s: %w", input.Column, err)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestIncrementTemplate_negativeAmountDecrement(t *testing.T) {
	// Negative amount is handled by BuildIncrement (col = col + (-N)),
	// so the template just passes input.Amount directly.
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	// Verify amount is passed through — no special negative handling in template
	if !strings.Contains(output, "input.Amount") {
		t.Errorf("template should pass input.Amount to BuildIncrement\n\nfull output:\n%s", output)
	}
}

func TestIncrementTemplate_strictUpdatesMissingPK(t *testing.T) {
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"count, err := execResult.RowsAffected()",
		`fmt.Errorf("increment product rows affected: %w", err)`,
		"if count == 0 {",
		"return nil, ErrNotFound",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Doc comment should mention ErrNotFound
	if !strings.Contains(output, "Returns ErrNotFound if the primary key does not exist.") {
		t.Errorf("doc comment should mention ErrNotFound when strict_updates is true\n\nfull output:\n%s", output)
	}
}

func TestIncrementTemplate_noStrictUpdates(t *testing.T) {
	ctx := testIncrementContext_noStrictUpdates()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	// Should NOT have RowsAffected check
	if strings.Contains(output, "RowsAffected") {
		t.Errorf("Increment without strict_updates should not check RowsAffected\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "ErrNotFound") {
		t.Errorf("Increment without strict_updates should not reference ErrNotFound\n\nfull output:\n%s", output)
	}
	// Should suppress execResult
	if !strings.Contains(output, "_ = execResult") {
		t.Errorf("Increment without strict_updates should suppress execResult\n\nfull output:\n%s", output)
	}
}

func TestIncrementTemplate_skippedNoIncrementableColumns(t *testing.T) {
	ctx := testIncrementContext_postgres()
	ctx.IncrementColumns = nil // no incrementable columns
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "IncrementColumn") {
		t.Errorf("template should be skipped when no incrementable columns\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "func (c *productClient) Increment") {
		t.Errorf("Increment method should not be generated when no incrementable columns\n\nfull output:\n%s", output)
	}
}

func TestIncrementTemplate_skippedOperationDisabled(t *testing.T) {
	ctx := testIncrementContext_postgres()
	ctx.Operations.Increment = false
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "IncrementColumn") {
		t.Errorf("template should be skipped when Increment operation is disabled\n\nfull output:\n%s", output)
	}
}

func TestIncrementTemplate_compositePK(t *testing.T) {
	ctx := testIncrementContext_compositePK()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"type OrderItemIncrementColumn string",
		`OrderItemIncrementQuantity OrderItemIncrementColumn = "quantity"`,
		"func (c *orderItemClient) Increment(ctx context.Context, pk OrderItemPK, input IncrementInput[OrderItemIncrementColumn], opts ...func(*CallOptions[OrderItemFieldOptions])) error",
		"query, args := sql.BuildIncrement(c.dialect, c.table, string(input.Column), input.Amount, sql.BuildCompositePKConditions(c.dialect,",
		`[]string{"order_id", "product_id", },`,
		`[]any{pk.OrderID, pk.ProductID, })`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Golden file test ---

func TestIncrementTemplate_goldenFile(t *testing.T) {
	ctx := testIncrementContext_postgres()
	output := executeIncrementTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_increment_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "increment_products_gen.go")

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
		t.Errorf("increment_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}
