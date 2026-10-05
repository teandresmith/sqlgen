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

func loadUpdateTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("update.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "update.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing update template: %v", err)
	}
	return tmpl
}

func executeUpdateTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadUpdateTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/update", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testUpdateContext_singlePK_postgres returns a product context with single UUID PK,
// strict_updates, update_columns, soft delete — PostgreSQL/pgx.
func testUpdateContext_singlePK_postgres() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		StrictUpdates:     true,
		BatchSize:         100,
		UpdateColumns:     []gen.UpdateColumnContext{{Name: "updated_at", FieldName: "UpdatedAt"}},
		Imports: []string{
			"context",
			"fmt",
			"time",
			"github.com/gofrs/uuid/v5",
			"github.com/jackc/pgx/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "omittable.Value[string]", ColumnName: "name", Omittable: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "omittable.Value[float64]", ColumnName: "price", Omittable: true, JSONTag: "price"},
			{FieldName: "SKU", GoType: "omittable.Value[string]", ColumnName: "sku", Omittable: true, JSONTag: "sku"},
			{FieldName: "CompanyID", GoType: "omittable.Value[uuid.NullUUID]", ColumnName: "company_id", Omittable: true, JSONTag: "company_id"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
			{FieldName: "UpdatedAt", GoType: "omittable.Value[time.Time]", ColumnName: "updated_at", Omittable: true, JSONTag: "updated_at"},
		},
		Operations: gen.ResolvedOperations{
			Update:      true,
			UpdateMany:  true,
			UpdateWhere: true,
		},
	}
}

// testUpdateContext_singlePK_mysql returns a MySQL product context (no RETURNING, stdlib).
func testUpdateContext_singlePK_mysql() gen.TableContext {
	ctx := testUpdateContext_singlePK_postgres()
	ctx.Dialect = "mysql"
	ctx.Driver = "stdlib"
	// Remove pgx import for stdlib driver
	ctx.Imports = []string{
		"context",
		"fmt",
		"time",
		"github.com/gofrs/uuid/v5",
		"github.com/teandresmith/sqlgen/comparator",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/sql",
	}
	return ctx
}

// testUpdateContext_noStrictUpdates returns a context with strict_updates disabled.
func testUpdateContext_noStrictUpdates() gen.TableContext {
	ctx := testUpdateContext_singlePK_postgres()
	ctx.StrictUpdates = false
	return ctx
}

// testUpdateContext_noUpdateColumns returns a context without update_columns.
func testUpdateContext_noUpdateColumns() gen.TableContext {
	ctx := testUpdateContext_singlePK_postgres()
	ctx.UpdateColumns = nil
	// Remove UpdatedAt from UpdateInputFields since there are no update_columns
	ctx.UpdateInputFields = ctx.UpdateInputFields[:5]
	return ctx
}

// testUpdateContext_compositePK returns an order_items context with composite PK.
func testUpdateContext_compositePK() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Package:               "db",
		VarName:               "o",
		Dialect:               "postgres",
		Driver:                "pgx",
		StrictUpdates:         true,
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		BatchSize:             100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/jackc/pgx/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Quantity", GoType: "omittable.Value[int32]", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
			{FieldName: "UnitPrice", GoType: "omittable.Value[float64]", ColumnName: "unit_price", Omittable: true, JSONTag: "unit_price"},
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
		},
		Operations: gen.ResolvedOperations{
			Update:      true,
			UpdateMany:  true,
			UpdateWhere: true,
		},
	}
}

// testUpdateContext_stdlib returns a stdlib (non-pgx) context for sequential UpdateMany.
func testUpdateContext_stdlib() gen.TableContext {
	ctx := testUpdateContext_singlePK_postgres()
	ctx.Driver = "stdlib"
	// Remove pgx import for stdlib driver
	ctx.Imports = []string{
		"context",
		"fmt",
		"time",
		"github.com/gofrs/uuid/v5",
		"github.com/teandresmith/sqlgen/comparator",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/sql",
	}
	return ctx
}

// --- Tests: Update ---

func TestUpdateTemplate_partialFields(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Update(ctx context.Context, id uuid.UUID, input *UpdateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"conn := database.Conn(ctx, c.querier)",
		"setClauses := make(map[string]any)",
		`if v, ok := input.Name.Get(); ok {`,
		`setClauses["name"] = v`,
		`if v, ok := input.Price.Get(); ok {`,
		`setClauses["price"] = v`,
		`if v, ok := input.SKU.Get(); ok {`,
		`setClauses["sku"] = v`,
		`if v, ok := input.CompanyID.Get(); ok {`,
		`if v, ok := input.DeletedAt.Get(); ok {`,
		`if v, ok := input.UpdatedAt.Get(); ok {`,
		"sql.BuildUpdate(c.dialect, c.table, sql.UpdateOptions{",
		`sql.Where(c.dialect.QuoteIdentifier("id")).Eq(id)`,
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_emptyInput(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	// Empty update returns current entity via Get with SkipHooks
	wantPatterns := []string{
		"if len(setClauses) == 0 {",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing empty update pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_strictUpdates(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"count, err := execResult.RowsAffected()",
		`fmt.Errorf("update product rows affected: %w", err)`,
		"if count == 0 {",
		"return nil, ErrNotFound",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing strict_updates pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_noStrictUpdates(t *testing.T) {
	ctx := testUpdateContext_noStrictUpdates()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "ErrNotFound") {
		t.Error("output without strict_updates should not contain ErrNotFound")
	}
	// Should discard execResult
	if !strings.Contains(output, "_ = execResult") {
		t.Errorf("output without strict_updates should discard execResult\n\nfull output:\n%s", output)
	}
}

func TestUpdateTemplate_autoSetUpdateColumns(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		`if _, ok := input.UpdatedAt.Get(); !ok {`,
		`setClauses["updated_at"] = time.Now()`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing update_columns auto-set: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_noUpdateColumns(t *testing.T) {
	ctx := testUpdateContext_noUpdateColumns()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "time.Now()") {
		t.Error("output without update_columns should not contain time.Now()")
	}
}

// --- Tests: UpdateMany ---

func TestUpdateTemplate_updateMany_pgx(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"type UpdateProductItem struct {",
		"ID uuid.UUID",
		"Input *UpdateProductInput",
		"func (c *productClient) UpdateMany(ctx context.Context, items []UpdateProductItem, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"pgxBatch := &pgx.Batch{}",
		"pgxBatch.Queue(query, args...)",
		// Type assertion for SendBatch
		"batcher, ok := conn.(interface {",
		"SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults",
		"batcher.SendBatch(ctx, pgxBatch)",
		"br.Exec()",
		"br.Close()",
		// Sequential fallback
		"batchQueries",
		"batchArgs",
		"conn.Exec(ctx, q, batchArgs[i]...)",
		// Skip no-op
		"if len(setClauses) == 0 {",
		"allIDs = append(allIDs, item.ID)",
		// Re-fetch
		"idStrings[i] = id.String()",
		`ID: &comparator.ID{In: idStrings}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing pgx UpdateMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_updateMany_stdlib(t *testing.T) {
	ctx := testUpdateContext_stdlib()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) UpdateMany(",
		"for _, item := range batch {",
		"conn.Exec(ctx, query, args...)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing stdlib UpdateMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have pgx batch
	if strings.Contains(output, "pgxBatch") || strings.Contains(output, "SendBatch") {
		t.Error("stdlib UpdateMany should not use pgx batch")
	}
}

// --- Tests: UpdateWhere ---

func TestUpdateTemplate_updateWhere_postgres(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) UpdateWhere(ctx context.Context, filter *ProductFilter, input *UpdateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"conds := filter.ToConditions(c.dialect)",
		"if len(conds) == 0 {",
		"return nil, ErrEmptyFilter",
		// RETURNING path
		`ReturningColumns: []string{"id"}`,
		"rows, err := conn.Query(ctx, query, args...)",
		"rows.Scan(&id)",
		`ids = append(ids, id.String())`,
		// Re-fetch
		`ID: &comparator.ID{In: ids}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing UpdateWhere postgres pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_updateWhere_mysql(t *testing.T) {
	ctx := testUpdateContext_singlePK_mysql()
	output := executeUpdateTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *productClient) UpdateWhere(",
		"return nil, ErrEmptyFilter",
		// collectAffectedIDs path
		"ids, err := c.collectAffectedIDs(ctx, conn, conds)",
		// collectAffectedIDs helper
		"func (c *productClient) collectAffectedIDs(",
		`Columns:    []string{"id"}`,
		"rows.Scan(&id)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing UpdateWhere mysql pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have RETURNING
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL UpdateWhere should not use RETURNING")
	}
}

func TestUpdateTemplate_updateWhere_idempotent(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	// When no rows match, return nil (idempotent)
	if !strings.Contains(output, "if len(ids) == 0 {\n\t\t\treturn nil, nil\n\t\t}") {
		t.Errorf("output missing idempotent nil return for empty ids\n\nfull output:\n%s", output)
	}
}

func TestUpdateTemplate_updateWhere_emptyFilter(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "return nil, ErrEmptyFilter") {
		t.Errorf("output missing ErrEmptyFilter\n\nfull output:\n%s", output)
	}
}

// --- Tests: Composite PK ---

func TestUpdateTemplate_compositePK_update(t *testing.T) {
	ctx := testUpdateContext_compositePK()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) Update(ctx context.Context, pk OrderItemPK, input *UpdateOrderItemInput,",
		"sql.BuildCompositePKConditions(c.dialect,",
		`[]string{"order_id", "product_id", }`,
		`[]any{pk.OrderID, pk.ProductID, }`,
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK Update pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT use single id parameter
	if strings.Contains(output, "id uuid.UUID") {
		t.Error("composite PK Update should not use 'id uuid.UUID' parameter")
	}
}

func TestUpdateTemplate_compositePK_updateMany(t *testing.T) {
	ctx := testUpdateContext_compositePK()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"type UpdateOrderItemItem struct {",
		"PK    OrderItemPK",
		"Input *UpdateOrderItemInput",
		"var allPKs []OrderItemPK",
		"allPKs = append(allPKs, item.PK)",
		`Filter: &OrderItemFilter{PKs: allPKs}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK UpdateMany pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUpdateTemplate_compositePK_updateWhere(t *testing.T) {
	ctx := testUpdateContext_compositePK()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) UpdateWhere(",
		// RETURNING for composite PK on PostgreSQL
		`ReturningColumns: []string{"order_id", "product_id", }`,
		"rows.Scan(&pk.OrderID, &pk.ProductID)",
		`Filter: &OrderItemFilter{PKs: pks}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK UpdateWhere pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Golden file tests ---

func TestUpdateTemplate_goldenFile_singlePK(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_update_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "update_products_gen.go")

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
		t.Errorf("update_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testUpdateContext_compositePK()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_update_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "update_order_items_gen.go")

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
		t.Errorf("update_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateTemplate_goldenFile_mysql(t *testing.T) {
	ctx := testUpdateContext_singlePK_mysql()
	output := executeUpdateTemplate(t, ctx, sql.NewMySQLDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_update_mysql_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "update_products_mysql_gen.go")

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
		t.Errorf("update_products_mysql_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: Template compiles cleanly ---

func TestUpdateTemplate_compilesCleanly_singlePK(t *testing.T) {
	ctx := testUpdateContext_singlePK_postgres()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_update_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestUpdateTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testUpdateContext_compositePK()
	output := executeUpdateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_update_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestUpdateTemplate_compilesCleanly_mysql(t *testing.T) {
	ctx := testUpdateContext_singlePK_mysql()
	output := executeUpdateTemplate(t, ctx, sql.NewMySQLDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_update_mysql_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Test: Empty FieldOptions short-circuit ---

// TestUpdateTemplate_emptyFieldOptions pins the empty-FieldOptions
// escape on single-row Update, matching the sibling pins in create_test.go and
// upsert_test.go. Both sites are guarded — the §9.5 empty-update branch and
// the terminal re-fetch — so single-row Update carries exactly two, and
// UpdateMany / UpdateWhere carry their own on top of that.
func TestUpdateTemplate_emptyFieldOptions(t *testing.T) {
	const guard = "if options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns() {"

	tests := []struct {
		name    string
		ctx     gen.TableContext
		dialect sql.Dialect
	}{
		{"single PK postgres", testUpdateContext_singlePK_postgres(), sql.NewPostgresDialect()},
		{"single PK mysql", testUpdateContext_singlePK_mysql(), sql.NewMySQLDialect()},
		{"composite PK", testUpdateContext_compositePK(), sql.NewPostgresDialect()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpdateTemplate(t, tt.ctx, tt.dialect)

			// Isolate the single-row Update body — UpdateMany and UpdateWhere
			// carry their own guards, so counting across the whole file would
			// pass even with both single-row sites missing.
			body := singleRowUpdateBody(t, output)
			if got := strings.Count(body, guard); got != 2 {
				t.Errorf("single-row Update has %d FieldOptions guards, want 2 (empty-update branch + terminal re-fetch)\n\nbody:\n%s", got, body)
			}
			if !strings.Contains(body, "return nil, nil") {
				t.Errorf("single-row Update missing the skip return\n\nbody:\n%s", body)
			}

			// The empty-update branch must be guarded too (PRD §9.2: "on the
			// empty-update path as well as the normal one"). Anchor on the
			// branch comment so this fails if only the terminal site is guarded.
			idx := strings.Index(body, "// Empty update — no fields set")
			if idx < 0 {
				t.Fatalf("empty-update branch not found\n\nbody:\n%s", body)
			}
			if !strings.Contains(body[idx:idx+len(guard)+200], guard) {
				t.Errorf("empty-update branch is not guarded by the FieldOptions escape\n\nbranch:\n%s", body[idx:min(idx+400, len(body))])
			}
		})
	}
}

// singleRowUpdateBody returns the text of the single-row Update method, from
// its func line to the start of the next top-level declaration.
func singleRowUpdateBody(t *testing.T, output string) string {
	t.Helper()

	start := strings.Index(output, ") Update(ctx context.Context,")
	if start < 0 {
		t.Fatalf("single-row Update not found in output:\n%s", output)
	}
	rest := output[start:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		return rest[:end]
	}
	return rest
}
