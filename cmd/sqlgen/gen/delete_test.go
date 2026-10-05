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

func loadDeleteTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("delete.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "delete.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing delete template: %v", err)
	}
	return tmpl
}

func executeDeleteTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadDeleteTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/delete", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testDeleteContext_timestamp returns a product context with timestamp soft delete (deleted_at).
func testDeleteContext_timestamp() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		BatchSize:         100,
		SoftDelete:        &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
		ExcludeDeleted:    true,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "comparator.ID", ColumnName: "id"},
			{FieldName: "Name", ComparatorType: "comparator.String", ColumnName: "name"},
			{FieldName: "DeletedAt", ComparatorType: "comparator.NullableTime", ColumnName: "deleted_at", IsSoftDeleteColumn: true},
		},
		Operations: gen.ResolvedOperations{
			SoftDelete: true,
			Restore:    true,
			HardDelete: true,
		},
	}
}

// testDeleteContext_bool returns a context with bool soft delete (is_deleted).
func testDeleteContext_bool() gen.TableContext {
	ctx := testDeleteContext_timestamp()
	ctx.SoftDelete = &gen.SoftDeleteContext{Column: "is_deleted", FieldName: "IsDeleted", Strategy: "bool"}
	ctx.FilterFields = []gen.FilterFieldContext{
		{FieldName: "ID", ComparatorType: "comparator.ID", ColumnName: "id"},
		{FieldName: "IsDeleted", ComparatorType: "comparator.Bool", ColumnName: "is_deleted", IsSoftDeleteColumn: true},
	}
	return ctx
}

// testDeleteContext_integer returns a context with integer soft delete (deleted).
func testDeleteContext_integer() gen.TableContext {
	ctx := testDeleteContext_timestamp()
	ctx.SoftDelete = &gen.SoftDeleteContext{Column: "deleted", FieldName: "Deleted", Strategy: "integer"}
	ctx.FilterFields = []gen.FilterFieldContext{
		{FieldName: "ID", ComparatorType: "comparator.ID", ColumnName: "id"},
		{FieldName: "Deleted", ComparatorType: "comparator.Number[int32]", ColumnName: "deleted", IsSoftDeleteColumn: true},
	}
	return ctx
}

// testDeleteContext_noSoftDelete returns a context with only HardDelete (no soft delete).
func testDeleteContext_noSoftDelete() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		BatchSize:         100,
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
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "ID", ComparatorType: "comparator.ID", ColumnName: "id"},
		},
		Operations: gen.ResolvedOperations{
			HardDelete: true,
		},
	}
}

// testDeleteContext_mysql returns a MySQL context (no RETURNING, stdlib).
func testDeleteContext_mysql() gen.TableContext {
	ctx := testDeleteContext_timestamp()
	ctx.Dialect = "mysql"
	ctx.Driver = "stdlib"
	return ctx
}

// testDeleteContext_compositePK returns a composite PK context with soft delete.
func testDeleteContext_compositePK() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Package:               "db",
		VarName:               "o",
		Dialect:               "postgres",
		Driver:                "pgx",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		BatchSize:             100,
		SoftDelete:            &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"},
		ExcludeDeleted:        true,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "DeletedAt", ComparatorType: "comparator.NullableTime", ColumnName: "deleted_at", IsSoftDeleteColumn: true},
			{FieldName: "OrderID", ComparatorType: "comparator.ID", ColumnName: "order_id"},
			{FieldName: "ProductID", ComparatorType: "comparator.ID", ColumnName: "product_id"},
		},
		Operations: gen.ResolvedOperations{
			SoftDelete: true,
			Restore:    true,
			HardDelete: true,
		},
	}
}

// --- Tests: FieldOptions skip fetch ---

func TestDeleteTemplate_skipFetch_softDelete(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"options := resolveCallOptions(opts)",
		"if options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns() {",
	}
	// SoftDelete, SoftDeleteMany, Restore, RestoreMany should all have skipFetch
	for _, method := range []string{"SoftDelete", "SoftDeleteMany", "Restore", "RestoreMany"} {
		for _, want := range wantPatterns {
			if !strings.Contains(output, want) {
				t.Errorf("%s output missing skipFetch pattern: %q", method, want)
			}
		}
	}
}

// --- Tests: SoftDelete ---

func TestDeleteTemplate_softDelete_timestamp(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) SoftDelete(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"conn := database.Conn(ctx, c.querier)",
		"sql.BuildSoftDelete(c.dialect, c.table, sql.SoftDeleteOptions{",
		`"deleted_at"`,
		"sql.SoftDeleteTimestamp",
		`Conditions: []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)}`,
		`fmt.Errorf("soft delete product: %w", err)`,
		// Re-fetch with soft delete override — unwrap single result from GetMany
		"ID: &comparator.ID{Eq: new(id.String())}",
		"DeletedAt: &comparator.NullableTime{Null: new(false)}",
		"Limit: new(1)",
		"if len(results) == 0 {",
		"return results[0], nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDelete timestamp pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_softDelete_bool(t *testing.T) {
	ctx := testDeleteContext_bool()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"sql.SoftDeleteBool",
		`"is_deleted"`,
		"IsDeleted: &comparator.Bool{Eq: new(true)}",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDelete bool pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_softDelete_integer(t *testing.T) {
	ctx := testDeleteContext_integer()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"sql.SoftDeleteInteger",
		`"deleted"`,
		"Deleted: &comparator.Number[int32]{Neq: new(0)}",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDelete integer pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Tests: SoftDeleteMany ---

func TestDeleteTemplate_softDeleteMany(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) SoftDeleteMany(ctx context.Context, ids []uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"idStrings := make([]string, len(ids))",
		"idStrings[i] = id.String()",
		"sql.BuildSoftDelete(c.dialect, c.table, sql.SoftDeleteOptions{",
		`Conditions: []sql.Condition{sql.Where(c.dialect.QuoteIdentifier("id")).In(toAnySlice(idStrings)...)}`,
		`fmt.Errorf("soft delete products: %w", err)`,
		"ID: &comparator.ID{In: idStrings}",
		"DeletedAt: &comparator.NullableTime{Null: new(false)}",
		"Limit: new(0)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDeleteMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Tests: SoftDeleteWhere ---

func TestDeleteTemplate_softDeleteWhere_postgres(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) SoftDeleteWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"conds := filter.ToConditions(c.dialect)",
		"return nil, ErrEmptyFilter",
		// Always scope to non-deleted
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull())`,
		// skipFetch path
		"skipFetch := options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns()",
		// RETURNING path
		`ReturningColumns: []string{"id"}`,
		"rows, err := conn.Query(ctx, query, args...)",
		"rows.Scan(&id)",
		`ids = append(ids, id.String())`,
		`fmt.Errorf("scan soft deleted product id: %w", err)`,
		// Re-fetch
		"ID: &comparator.ID{In: ids}",
		"DeletedAt: &comparator.NullableTime{Null: new(false)}",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDeleteWhere postgres pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_softDeleteWhere_mysql(t *testing.T) {
	ctx := testDeleteContext_mysql()
	output := executeDeleteTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *productClient) SoftDeleteWhere(",
		// collectAffectedIDs path
		"ids, err := c.collectAffectedIDs(ctx, conn, conds)",
		// collectAffectedIDs helper generated
		"func (c *productClient) collectAffectedIDs(",
		`Columns:    []string{"id"}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing SoftDeleteWhere mysql pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have RETURNING
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL SoftDeleteWhere should not use RETURNING")
	}
}

func TestDeleteTemplate_softDeleteWhere_idempotent(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "if len(ids) == 0 {\n\t\t\treturn nil, nil\n\t\t}") {
		t.Errorf("output missing idempotent nil return for empty ids\n\nfull output:\n%s", output)
	}
}

// --- Tests: Restore ---

func TestDeleteTemplate_restore_timestamp(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Restore(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"sql.BuildUpdate(c.dialect, c.table, sql.UpdateOptions{",
		`SetClauses: map[string]any{"deleted_at": nil}`,
		`sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)`,
		`fmt.Errorf("restore product: %w", err)`,
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Restore timestamp pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_restore_bool(t *testing.T) {
	ctx := testDeleteContext_bool()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, `SetClauses: map[string]any{"is_deleted": false}`) {
		t.Errorf("output missing Restore bool setClauses\n\nfull output:\n%s", output)
	}
}

func TestDeleteTemplate_restore_integer(t *testing.T) {
	ctx := testDeleteContext_integer()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, `SetClauses: map[string]any{"deleted": 0}`) {
		t.Errorf("output missing Restore integer setClauses\n\nfull output:\n%s", output)
	}
}

// --- Tests: RestoreWhere ---

func TestDeleteTemplate_restoreWhere_scopesToDeleted(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) RestoreWhere(",
		// Always scope to deleted rows
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNotNull())`,
		"return nil, ErrEmptyFilter",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing RestoreWhere pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_restoreWhere_bool_scopesToDeleted(t *testing.T) {
	ctx := testDeleteContext_bool()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("is_deleted")).Eq(true))`) {
		t.Errorf("output missing RestoreWhere bool scoping\n\nfull output:\n%s", output)
	}
}

func TestDeleteTemplate_restoreWhere_integer_scopesToDeleted(t *testing.T) {
	ctx := testDeleteContext_integer()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("deleted")).Eq(1))`) {
		t.Errorf("output missing RestoreWhere integer scoping\n\nfull output:\n%s", output)
	}
}

func TestDeleteTemplate_restoreWhere_idempotent(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	// RestoreWhere with RETURNING: idempotent on empty IDs
	if !strings.Contains(output, `fmt.Errorf("scan restored product id: %w", err)`) {
		t.Errorf("output missing RestoreWhere RETURNING scan\n\nfull output:\n%s", output)
	}
}

// --- Tests: HardDelete ---

func TestDeleteTemplate_hardDelete(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) HardDelete(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) error",
		"sql.BuildHardDelete(c.dialect, c.table, sql.HardDeleteOptions{",
		`sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)`,
		`fmt.Errorf("hard delete product: %w", err)`,
		"return nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing HardDelete pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_hardDeleteMany(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) HardDeleteMany(ctx context.Context, ids []uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) error",
		`sql.Where(c.dialect.QuoteIdentifier("id")).In(toAnySlice(ids)...)`,
		`fmt.Errorf("hard delete products: %w", err)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing HardDeleteMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_hardDeleteWhere(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) HardDeleteWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) error",
		"conds := filter.ToConditions(c.dialect)",
		"return nil, ErrEmptyFilter",
		"sql.BuildHardDelete(c.dialect, c.table, sql.HardDeleteOptions{",
		`fmt.Errorf("hard delete products where: %w", err)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing HardDeleteWhere pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Tests: No soft delete ---

func TestDeleteTemplate_noSoftDelete_onlyHardDelete(t *testing.T) {
	ctx := testDeleteContext_noSoftDelete()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	// HardDelete methods should be present
	if !strings.Contains(output, "func (c *productClient) HardDelete(") {
		t.Error("output should contain HardDelete when no soft delete")
	}
	if !strings.Contains(output, "func (c *productClient) HardDeleteMany(") {
		t.Error("output should contain HardDeleteMany when no soft delete")
	}
	if !strings.Contains(output, "func (c *productClient) HardDeleteWhere(") {
		t.Error("output should contain HardDeleteWhere when no soft delete")
	}

	// SoftDelete/Restore should NOT be present
	absentPatterns := []string{
		"SoftDelete(",
		"SoftDeleteMany(",
		"SoftDeleteWhere(",
		"Restore(",
		"RestoreMany(",
		"RestoreWhere(",
	}
	for _, absent := range absentPatterns {
		if strings.Contains(output, absent) {
			t.Errorf("output without soft delete should not contain %q", absent)
		}
	}
}

// --- Tests: Composite PK ---

func TestDeleteTemplate_compositePK_softDelete(t *testing.T) {
	ctx := testDeleteContext_compositePK()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) SoftDelete(ctx context.Context, pk OrderItemPK,",
		"sql.BuildCompositePKConditions(c.dialect,",
		`[]string{"order_id", "product_id", }`,
		`[]any{pk.OrderID, pk.ProductID, }`,
		"PKs: []OrderItemPK{pk}",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK SoftDelete pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_compositePK_softDeleteMany(t *testing.T) {
	ctx := testDeleteContext_compositePK()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) SoftDeleteMany(ctx context.Context, pks []OrderItemPK,",
		"pkValues := make([][]any, len(pks))",
		"sql.BuildCompositePKBatchCondition(c.dialect,",
		"PKs: pks",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK SoftDeleteMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_compositePK_hardDeleteMany(t *testing.T) {
	ctx := testDeleteContext_compositePK()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) HardDeleteMany(ctx context.Context, pks []OrderItemPK,",
		"pkValues := make([][]any, len(pks))",
		"sql.BuildCompositePKBatchCondition(c.dialect,",
		"sql.BuildHardDelete(c.dialect, c.table,",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK HardDeleteMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestDeleteTemplate_compositePK_softDeleteWhere(t *testing.T) {
	ctx := testDeleteContext_compositePK()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) SoftDeleteWhere(",
		// RETURNING for composite PK on PostgreSQL
		`ReturningColumns: []string{"order_id", "product_id", }`,
		"rows.Scan(&pk.OrderID, &pk.ProductID)",
		"Filter: &OrderItemFilter{",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK SoftDeleteWhere pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Tests: collectAffectedIDs/PKs not duplicated ---

func TestDeleteTemplate_collectAffectedIDs_notDuplicated_withUpdateWhere(t *testing.T) {
	ctx := testDeleteContext_mysql()
	ctx.Operations.UpdateWhere = true
	output := executeDeleteTemplate(t, ctx, sql.NewMySQLDialect())

	// collectAffectedIDs should NOT be generated by delete template when UpdateWhere is true
	// (it's already generated by the update template)
	if strings.Contains(output, "func (c *productClient) collectAffectedIDs(") {
		t.Error("delete template should not generate collectAffectedIDs when UpdateWhere is true")
	}
}

// --- Golden file tests ---

func TestDeleteTemplate_goldenFile_singlePK(t *testing.T) {
	ctx := testDeleteContext_timestamp()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_delete_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "delete_products_gen.go")

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
		t.Errorf("delete_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestDeleteTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testDeleteContext_compositePK()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_delete_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "delete_order_items_gen.go")

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
		t.Errorf("delete_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestDeleteTemplate_goldenFile_noSoftDelete(t *testing.T) {
	ctx := testDeleteContext_noSoftDelete()
	output := executeDeleteTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_delete_no_soft_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "delete_products_no_soft_gen.go")

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
		t.Errorf("delete_products_no_soft_gen.go mismatch (-want +got):\n%s", diff)
	}
}
