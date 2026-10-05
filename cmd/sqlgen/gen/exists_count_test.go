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

func loadExistsTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("exists.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "exists.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing exists template: %v", err)
	}
	return tmpl
}

func executeExistsTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadExistsTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/exists", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func loadCountTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("count.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "count.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing count template: %v", err)
	}
	return tmpl
}

func executeCountTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadCountTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/count", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testExistsCountContext_postgres returns a product context with soft delete, PostgreSQL.
func testExistsCountContext_postgres() gen.TableContext {
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
		SoftDelete: &gen.SoftDeleteContext{
			Column:    "deleted_at",
			FieldName: "DeletedAt",
			Strategy:  "timestamp",
		},
		ExcludeDeleted: true,
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "comparator.ID", ColumnName: "id"},
			{FieldName: "DeletedAt", ComparatorType: "comparator.Time", ColumnName: "deleted_at", IsSoftDeleteColumn: true},
			{FieldName: "Name", ComparatorType: "comparator.String", ColumnName: "name"},
		},
		Operations: gen.ResolvedOperations{
			Exists: true,
			Count:  true,
		},
	}
}

// testExistsCountContext_noSoftDelete returns a context without soft delete.
func testExistsCountContext_noSoftDelete() gen.TableContext {
	return gen.TableContext{
		StructName:        "Category",
		TableName:         "categories",
		TableNameConstant: "TableCategories",
		Schema:            "public",
		Package:           "db",
		VarName:           "c",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyCaller,
		Imports: []string{
			"context",
			"fmt",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", ZeroValue: "0", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "comparator.Number[int64]", ColumnName: "id"},
			{FieldName: "Name", ComparatorType: "comparator.String", ColumnName: "name"},
		},
		Operations: gen.ResolvedOperations{
			Exists: true,
			Count:  true,
		},
	}
}

// testExistsCountContext_compositePK returns a context with composite PK.
func testExistsCountContext_compositePK() gen.TableContext {
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
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "OrderID", ComparatorType: "comparator.ID", ColumnName: "order_id"},
			{FieldName: "ProductID", ComparatorType: "comparator.ID", ColumnName: "product_id"},
		},
		Operations: gen.ResolvedOperations{
			Exists: true,
			Count:  true,
		},
	}
}

// --- Exists template tests ---

func TestExistsTemplate_existsWithPK(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Exists(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (bool, error)",
		"conn := database.Conn(ctx, c.querier)",
		`conds := []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)}`,
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull())`,
		"query, args := sql.BuildExists(c.dialect, c.table, conds)",
		"var exists bool",
		"conn.QueryRow(ctx, query, args...).Scan(&exists)",
		`fmt.Errorf("exists product: %w", err)`,
		"return exists, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestExistsTemplate_existsWithoutSoftDelete(t *testing.T) {
	ctx := testExistsCountContext_noSoftDelete()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *categoryClient) Exists(ctx context.Context, id int64, opts ...func(*CallOptions[CategoryFieldOptions])) (bool, error)",
		`conds := []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)}`,
		"query, args := sql.BuildExists(c.dialect, c.table, conds)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have soft delete injection
	if strings.Contains(output, "deleted_at") {
		t.Errorf("Exists without soft delete should not reference deleted_at\n\nfull output:\n%s", output)
	}
}

func TestExistsTemplate_existsWhereWithFilter(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) ExistsWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) (bool, error)",
		"conds = filter.ToConditions(c.dialect)",
		"if filter == nil || filter.DeletedAt == nil {",
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull())`,
		"query, args := sql.BuildExists(c.dialect, c.table, conds)",
		`fmt.Errorf("exists product where: %w", err)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestExistsTemplate_existsWhereWithoutSoftDelete(t *testing.T) {
	ctx := testExistsCountContext_noSoftDelete()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *categoryClient) ExistsWhere(ctx context.Context, filter *CategoryFilter, opts ...func(*CallOptions[CategoryFieldOptions])) (bool, error)",
		"conds = filter.ToConditions(c.dialect)",
		"query, args := sql.BuildExists(c.dialect, c.table, conds)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have soft delete injection
	if strings.Contains(output, "filter.DeletedAt") {
		t.Errorf("ExistsWhere without soft delete should not check DeletedAt\n\nfull output:\n%s", output)
	}
}

func TestExistsTemplate_compositePK(t *testing.T) {
	ctx := testExistsCountContext_compositePK()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) Exists(ctx context.Context, pk OrderItemPK, opts ...func(*CallOptions[OrderItemFieldOptions])) (bool, error)",
		"conds := sql.BuildCompositePKConditions(c.dialect,",
		`[]string{"order_id", "product_id", },`,
		`[]any{pk.OrderID, pk.ProductID, })`,
		"query, args := sql.BuildExists(c.dialect, c.table, conds)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestExistsTemplate_disabled(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	ctx.Operations.Exists = false
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *productClient) Exists") {
		t.Error("Exists method should not be generated when disabled")
	}
	if strings.Contains(output, "func (c *productClient) ExistsWhere") {
		t.Error("ExistsWhere method should not be generated when disabled")
	}
}

// --- Count template tests ---

func TestCountTemplate_withFilter(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	output := executeCountTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Count(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) (int64, error)",
		"conn := database.Conn(ctx, c.querier)",
		"conds = filter.ToConditions(c.dialect)",
		"if filter == nil || filter.DeletedAt == nil {",
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull())`,
		"query, args := sql.BuildCount(c.dialect, c.table, conds)",
		"var count int64",
		"conn.QueryRow(ctx, query, args...).Scan(&count)",
		`fmt.Errorf("count products: %w", err)`,
		"return count, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestCountTemplate_withoutSoftDelete(t *testing.T) {
	ctx := testExistsCountContext_noSoftDelete()
	output := executeCountTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *categoryClient) Count(ctx context.Context, filter *CategoryFilter, opts ...func(*CallOptions[CategoryFieldOptions])) (int64, error)",
		"query, args := sql.BuildCount(c.dialect, c.table, conds)",
		`fmt.Errorf("count categories: %w", err)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have soft delete injection
	if strings.Contains(output, "deleted_at") {
		t.Errorf("Count without soft delete should not reference deleted_at\n\nfull output:\n%s", output)
	}
}

func TestCountTemplate_nilFilterCountsAll(t *testing.T) {
	ctx := testExistsCountContext_noSoftDelete()
	output := executeCountTemplate(t, ctx, sql.NewPostgresDialect())

	// The doc comment should indicate nil filter counts all
	if !strings.Contains(output, "A nil filter counts all categories.") {
		t.Errorf("Count doc comment should mention nil filter\n\nfull output:\n%s", output)
	}
}

func TestCountTemplate_disabled(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	ctx.Operations.Count = false
	output := executeCountTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *productClient) Count") {
		t.Error("Count method should not be generated when disabled")
	}
}

// --- Golden file tests ---

func TestExistsTemplate_goldenFile(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	output := executeExistsTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_exists_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "exists_products_gen.go")

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
		t.Errorf("exists_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestCountTemplate_goldenFile(t *testing.T) {
	ctx := testExistsCountContext_postgres()
	output := executeCountTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_count_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "count_products_gen.go")

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
		t.Errorf("count_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}
