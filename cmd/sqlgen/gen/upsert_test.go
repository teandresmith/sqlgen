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

func loadUpsertTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("upsert.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "upsert.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing upsert template: %v", err)
	}
	return tmpl
}

func executeUpsertTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadUpsertTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/upsert", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testUpsertContext_dbStrategyUUID_postgres returns a product context with PK strategy "db",
// UUID PK with DEFAULT, PostgreSQL dialect, multiple conflict targets.
func testUpsertContext_dbStrategyUUID_postgres() gen.TableContext {
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
		BatchSize:         100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer, HasDefault: true},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "float64", ColumnName: "price", Required: true, JSONTag: "price"},
			{FieldName: "SKU", GoType: "string", ColumnName: "sku", Required: true, JSONTag: "sku"},
			{FieldName: "ID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "id", Omittable: true, JSONTag: "id"},
			{FieldName: "CompanyID", GoType: "omittable.Value[uuid.NullUUID]", ColumnName: "company_id", Omittable: true, JSONTag: "company_id"},
		},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "ProductConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
			{ConstantName: "ProductConflictRegionSlug", Columns: []string{"region", "slug"}, CoversPK: false, Comment: "UNIQUE (region, slug)"},
			{ConstantName: "ProductConflictSKU", Columns: []string{"sku"}, CoversPK: false, Comment: "UNIQUE (sku)"},
		},
		Operations: gen.ResolvedOperations{
			Upsert: true,
		},
	}
}

// testUpsertContext_dbStrategyAutoIncrement_mysql returns a MySQL context with AUTO_INCREMENT PK.
func testUpsertContext_dbStrategyAutoIncrement_mysql() gen.TableContext {
	return gen.TableContext{
		StructName:        "Event",
		TableName:         "events",
		TableNameConstant: "TableEvents",
		Schema:            "",
		Package:           "db",
		VarName:           "e",
		Dialect:           "mysql",
		Driver:            "stdlib",
		PKStrategy:        config.PKStrategyDB,
		BatchSize:         100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", ZeroValue: "0", DBTag: "id", JSONTag: "id", PrimaryKey: true, AutoIncrement: true, FKConvert: gotype.FKStringSprint},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Payload", GoType: "string", ColumnName: "payload", Required: true, JSONTag: "payload"},
		},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "EventConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
		},
		Operations: gen.ResolvedOperations{
			Upsert: true,
		},
	}
}

// testUpsertContext_callerStrategy_postgres returns a caller-strategy context with PK in CreateInput.
func testUpsertContext_callerStrategy_postgres() gen.TableContext {
	return gen.TableContext{
		StructName:        "Tenant",
		TableName:         "tenants",
		TableNameConstant: "TableTenants",
		Schema:            "public",
		Package:           "db",
		VarName:           "t",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyCaller,
		BatchSize:         100,
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
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "ID", GoType: "uuid.UUID", ColumnName: "id", Required: true, JSONTag: "id"},
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Slug", GoType: "string", ColumnName: "slug", Required: true, JSONTag: "slug"},
		},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "TenantConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
			{ConstantName: "TenantConflictSlug", Columns: []string{"slug"}, CoversPK: false, Comment: "UNIQUE (slug)"},
		},
		Operations: gen.ResolvedOperations{
			Upsert: true,
		},
	}
}

// testUpsertContext_compositePK returns an order_items context with composite PK.
func testUpsertContext_compositePK() gen.TableContext {
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
		BatchSize:             100,
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
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", ColumnName: "order_id", Required: true, JSONTag: "order_id"},
			{FieldName: "ProductID", GoType: "uuid.UUID", ColumnName: "product_id", Required: true, JSONTag: "product_id"},
			{FieldName: "UnitPrice", GoType: "float64", ColumnName: "unit_price", Required: true, JSONTag: "unit_price"},
			{FieldName: "Quantity", GoType: "omittable.Value[int32]", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
		},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "OrderItemConflictPK", Columns: []string{"order_id", "product_id"}, CoversPK: true, Comment: "PRIMARY KEY (order_id, product_id)"},
		},
		Operations: gen.ResolvedOperations{
			Upsert: true,
		},
	}
}

// testUpsertContext_sqlite returns an SQLite context (RETURNING support, ? placeholders).
func testUpsertContext_sqlite() gen.TableContext {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	ctx.Dialect = "sqlite"
	ctx.Driver = "stdlib"
	return ctx
}

// --- Test: ConflictTarget enum generation ---

func TestUpsertTemplate_conflictTargetEnum(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"type ProductConflictTarget int",
		"ProductConflictPK ProductConflictTarget = iota // PRIMARY KEY (id)",
		"ProductConflictRegionSlug // UNIQUE (region, slug)",
		"ProductConflictSKU // UNIQUE (sku)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: ConflictColumns map generation ---

func TestUpsertTemplate_conflictColumnsMap(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"var productConflictColumns = map[ProductConflictTarget][]string{",
		`ProductConflictPK: {"id"},`,
		`ProductConflictRegionSlug: {"region", "slug"},`,
		`ProductConflictSKU: {"sku"},`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Upsert SQL for PostgreSQL (ON CONFLICT DO UPDATE SET) ---

func TestUpsertTemplate_postgres(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Upsert(ctx context.Context, input *CreateProductInput, target ProductConflictTarget, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"options := resolveCallOptions(opts)",
		"conn := database.Conn(ctx, c.querier)",
		`columns := []string{"name", "price", "sku", }`,
		`args := []any{input.Name, input.Price, input.SKU, }`,
		"conflictColumns := productConflictColumns[target]",
		`updateColumns := excludeColumns(columns, append(conflictColumns, "id"))`,
		// insertUpsertAndResolveID path
		"id, err := c.insertUpsertAndResolveID(ctx, conn, columns, args, conflictColumns, updateColumns)",
		`fmt.Errorf("upsert product: %w", err)`,
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: insertUpsertAndResolveID — PostgreSQL RETURNING ---

func TestUpsertTemplate_insertUpsertAndResolveID_postgres(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) insertUpsertAndResolveID(ctx context.Context, conn database.Querier, columns []string, args []any, conflictColumns, updateColumns []string) (uuid.UUID, error)",
		"UpsertConflictKeys:    conflictColumns,",
		"UpsertUpdateColumns:   updateColumns,",
		`UpsertResolvePKColumn: "id",`,
		`ReturningColumns:      []string{"id"},`,
		"var id uuid.UUID",
		"return uuid.UUID{}, err",
		"return id, nil",
		// DO NOTHING returns no row on conflict, so the RETURNING
		// branch reads through Query/Next and falls back to a lookup by the
		// conflict columns rather than treating "no row" as an error.
		"rows, err := conn.Query(ctx, query, queryArgs...)",
		"found := rows.Next()",
		"return c.resolveUpsertConflictRow(ctx, conn, columns, args, conflictColumns)",
		"func (c *productClient) resolveUpsertConflictRow(ctx context.Context, conn database.Querier, columns []string, args []any, conflictColumns []string) (uuid.UUID, error)",
		"i := slices.Index(columns, col)",
		"conds = append(conds, sql.Where(c.dialect.QuoteIdentifier(col)).Eq(args[i]))",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Upsert SQL for MySQL (ON DUPLICATE KEY UPDATE) ---

func TestUpsertTemplate_mysql(t *testing.T) {
	ctx := testUpsertContext_dbStrategyAutoIncrement_mysql()
	output := executeUpsertTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *eventClient) Upsert(ctx context.Context, input *CreateEventInput, target EventConflictTarget, opts ...func(*CallOptions[EventFieldOptions])) (*Event, error)",
		`columns := []string{"name", "payload", }`,
		`args := []any{input.Name, input.Payload, }`,
		"conflictColumns := eventConflictColumns[target]",
		`updateColumns := excludeColumns(columns, append(conflictColumns, "id"))`,
		"id, err := c.insertUpsertAndResolveID(ctx, conn, columns, args, conflictColumns, updateColumns)",
		`fmt.Errorf("upsert event: %w", err)`,
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// insertUpsertAndResolveID should use LastInsertId, not RETURNING
	if !strings.Contains(output, "execResult.LastInsertId()") {
		t.Errorf("MySQL insertUpsertAndResolveID should use LastInsertId\n\nfull output:\n%s", output)
	}

	// LastInsertId() reports insert_id = 0 for a conflict that
	// modifies no row, so the statement has to republish the PK. The dialect
	// only emits the LAST_INSERT_ID self-assignment when handed the column.
	if !strings.Contains(output, `UpsertResolvePKColumn: "id",`) {
		t.Errorf("MySQL insertUpsertAndResolveID must set UpsertResolvePKColumn\n\nfull output:\n%s", output)
	}
	// MySQL preserves the PK inside the clause, so it needs no lookup fallback.
	if strings.Contains(output, "resolveUpsertConflictRow") {
		t.Errorf("MySQL should not emit resolveUpsertConflictRow\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL insertUpsertAndResolveID should not use RETURNING")
	}
}

// --- Test: Upsert SQL for SQLite (ON CONFLICT DO UPDATE SET) ---

func TestUpsertTemplate_sqlite(t *testing.T) {
	ctx := testUpsertContext_sqlite()
	output := executeUpsertTemplate(t, ctx, sql.NewSQLiteDialect())

	wantPatterns := []string{
		"func (c *productClient) Upsert(ctx context.Context, input *CreateProductInput, target ProductConflictTarget",
		"conflictColumns := productConflictColumns[target]",
		// SQLite supports RETURNING — should use insertUpsertAndResolveID
		"id, err := c.insertUpsertAndResolveID(ctx, conn, columns, args, conflictColumns, updateColumns)",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// SQLite supports RETURNING — insertUpsertAndResolveID should use it
	if !strings.Contains(output, `ReturningColumns:      []string{"id"}`) {
		t.Errorf("SQLite insertUpsertAndResolveID should use RETURNING\n\nfull output:\n%s", output)
	}

	// SQLite shares PostgreSQL's DO NOTHING branch, so it needs the
	// same conflict-row fallback.
	if !strings.Contains(output, "resolveUpsertConflictRow") {
		t.Errorf("SQLite insertUpsertAndResolveID should fall back to resolveUpsertConflictRow\n\nfull output:\n%s", output)
	}
}

// --- app-strategy Upsert with `string` GoType PK ---
//
// Mirror of the create-template regression. gotype maps SQL `uuid` →
// Go `string` when no override is configured; the upsert app-strategy
// branch (upsert.go.tmpl:108) must emit the `.String()` variants so the
// `var pkValue string ... pkValue = <gen>` assignment compiles.

func testUpsertContext_appStrategy_stringPK(version config.UUIDVersion) gen.TableContext {
	// google's string forms, matching the import below. Production fills this
	// in attachUUIDGeneration; a hand-built app-strategy fixture must set it.
	genExpr := "uuid.NewString()"
	if version == config.UUIDVersionV7 {
		genExpr = "uuid.Must(uuid.NewV7()).String()"
	}
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyApp,
		UUIDVersion:       version,
		PKAutoGenExpr:     genExpr,
		BatchSize:         100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "string", ZeroValue: `""`, DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringNone},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "float64", ColumnName: "price", Required: true, JSONTag: "price"},
			{FieldName: "ID", GoType: "omittable.Value[string]", ColumnName: "id", Omittable: true, JSONTag: "id"},
		},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "ProductConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
		},
		Operations: gen.ResolvedOperations{
			Upsert: true,
		},
	}
}

func TestUpsertTemplate_appStrategy_v4_stringPK(t *testing.T) {
	ctx := testUpsertContext_appStrategy_stringPK(config.UUIDVersionV4)
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "var pkValue string") {
		t.Errorf("output should declare `var pkValue string`\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "pkValue = uuid.NewString()") {
		t.Errorf("v4 + string PK should use uuid.NewString()\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "pkValue = uuid.New()") {
		t.Errorf("v4 + string PK must not assign uuid.UUID to string var\n\nfull output:\n%s", output)
	}
}

func TestUpsertTemplate_appStrategy_v7_stringPK(t *testing.T) {
	ctx := testUpsertContext_appStrategy_stringPK(config.UUIDVersionV7)
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "pkValue = uuid.Must(uuid.NewV7()).String()") {
		t.Errorf("v7 + string PK should use uuid.Must(uuid.NewV7()).String()\n\nfull output:\n%s", output)
	}
}

// --- Test: Upsert with caller strategy (PK in columns, excluded from SET) ---

func TestUpsertTemplate_callerStrategy(t *testing.T) {
	ctx := testUpsertContext_callerStrategy_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *tenantClient) Upsert(ctx context.Context, input *CreateTenantInput, target TenantConflictTarget",
		// PK is required — in static columns/args
		`columns := []string{"id", "name", "slug", }`,
		`args := []any{input.ID, input.Name, input.Slug, }`,
		// PK excluded from update columns
		`updateColumns := excludeColumns(columns, append(conflictColumns, "id"))`,
		// insertUpsertAndResolveID (RETURNING on PostgreSQL)
		"id, err := c.insertUpsertAndResolveID(ctx, conn, columns, args, conflictColumns, updateColumns)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have app-strategy UUID generation
	if strings.Contains(output, "pkValue") {
		t.Error("caller strategy should not have pkValue variable")
	}
}

// --- Test: Upsert with composite PK (no insertUpsertAndResolveID) ---

func TestUpsertTemplate_compositePK(t *testing.T) {
	ctx := testUpsertContext_compositePK()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) Upsert(ctx context.Context, input *CreateOrderItemInput, target OrderItemConflictTarget",
		`"order_id", "product_id", "unit_price"`,
		"input.OrderID, input.ProductID, input.UnitPrice",
		"conflictColumns := orderItemConflictColumns[target]",
		`updateColumns := excludeColumns(columns, append(conflictColumns, "order_id", "product_id"))`,
		// Direct Exec (no insertUpsertAndResolveID)
		"sql.BuildInsert(c.dialect, c.table, sql.InsertOptions{",
		"UpsertConflictKeys:  conflictColumns,",
		"UpsertUpdateColumns: updateColumns,",
		"conn.Exec(ctx, query, queryArgs...)",
		// Get with composite PK — uses SkipHooks
		"c.Get(ctx, OrderItemPK{OrderID: input.OrderID, ProductID: input.ProductID, }, func(o *CallOptions[OrderItemFieldOptions])",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have insertUpsertAndResolveID
	if strings.Contains(output, "insertUpsertAndResolveID") {
		t.Error("composite PK should not use insertUpsertAndResolveID")
	}
}

// --- Test: Empty FieldOptions short-circuit ---

func TestUpsertTemplate_emptyFieldOptions(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"if options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns() {",
		"return nil, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Upsert not generated when operation disabled ---

func TestUpsertTemplate_disabled(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	ctx.Operations.Upsert = false
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *productClient) Upsert") {
		t.Error("Upsert method should not be generated when disabled")
	}
	if strings.Contains(output, "ConflictTarget") {
		t.Error("ConflictTarget type should not be generated when Upsert is disabled")
	}
}

// --- Test: Golden file comparison ---

func TestUpsertTemplate_goldenFile_singlePK(t *testing.T) {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_upsert_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "upsert_products_gen.go")

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
		t.Errorf("upsert_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestUpsertTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testUpsertContext_compositePK()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_upsert_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "upsert_order_items_gen.go")

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
		t.Errorf("upsert_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- the fallback lookup must bind scan targets by column name ---
//
// sql.BuildSelect sorts its column list (sql/builder.go: writeSelectColumns →
// sortedCopy) while ReturningClause does not. A tenanted table whose tenant
// column sorts BEFORE its PK column — account_id, company_id, customer_id — is
// therefore returned as (tenant, pk) by the fallback SELECT but as (pk, tenant)
// by RETURNING. A positional scan binds those to the wrong targets, and does so
// silently whenever the two share a Go type.
//
// The `tenancy` example escapes only by alphabetical accident (id < workspace_id),
// so this pins the ordering rule directly rather than relying on an example.
func TestUpsertTemplate_conflictRowFallbackBindsByName(t *testing.T) {
	ctx := testUpsertContext_tenantSortsBeforePK()
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		// Name-matched binding, not rows.Scan(&id, &rowTenant).
		"cols, err := rows.Columns()",
		"targets := make([]any, len(cols))",
		"for i, col := range cols {",
		`case "id":`,
		"targets[i] = &id",
		`case "account_id":`,
		"targets[i] = &rowTenant",
		"if err := rows.Scan(targets...); err != nil {",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// The positional form is what was wrong; it must not come back *in the
	// fallback*. The RETURNING scan above it stays positional and is correct:
	// ReturningClause does not sort, so it emits the requested order.
	_, fallback, ok := strings.Cut(output, "func (c *invoiceClient) resolveUpsertConflictRow")
	if !ok {
		t.Fatalf("output missing resolveUpsertConflictRow\n\nfull output:\n%s", output)
	}
	if strings.Contains(fallback, "rows.Scan(&id, &rowTenant)") {
		t.Errorf("fallback still scans positionally — misbinds when the tenant column sorts first\n\nfallback:\n%s", fallback)
	}
	if !strings.Contains(output, "Scan(&id, &rowTenant)") {
		t.Errorf("the RETURNING scan should stay positional (ReturningClause does not sort)\n\nfull output:\n%s", output)
	}

	// A vanished conflicting row is a documented sentinel, not a raw driver error.
	if !strings.Contains(output, "return uuid.UUID{}, nil, ErrNotFound") {
		t.Errorf("fallback should return ErrNotFound when the conflicting row is gone\n\nfull output:\n%s", output)
	}
}

// testUpsertContext_tenantSortsBeforePK returns a tenanted postgres context
// whose tenant column ("account_id") sorts before its PK column ("id").
func testUpsertContext_tenantSortsBeforePK() gen.TableContext {
	return gen.TableContext{
		StructName:        "Invoice",
		TableName:         "invoices",
		TableNameConstant: "TableInvoices",
		Package:           "db",
		VarName:           "i",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyDB,
		BatchSize:         100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/comparator",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"github.com/teandresmith/sqlgen/tenancy",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", HasDefault: true},
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
			{Name: "account_id", FieldName: "AccountID", GoType: "uuid.UUID", DBTag: "account_id", JSONTag: "account_id", Import: "github.com/gofrs/uuid/v5"},
			{Name: "number", FieldName: "Number", GoType: "string", DBTag: "number", JSONTag: "number"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Number", GoType: "string", ColumnName: "number", Required: true, JSONTag: "number"},
			{FieldName: "AccountID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "account_id", Omittable: true, JSONTag: "account_id"},
		},
		AllColumnNames: []string{"account_id", "id", "number"},
		ConflictTargets: []gen.ConflictTargetContext{
			{ConstantName: "InvoiceConflictAccountIDNumber", Columns: []string{"account_id", "number"}, CoversPK: false, Comment: "UNIQUE (account_id, number)"},
			{ConstantName: "InvoiceConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
		},
		Operations: gen.ResolvedOperations{Upsert: true},
		Tenancy: &gen.TableTenancyContext{
			Tenanted:     true,
			Column:       "account_id",
			FieldName:    "AccountID",
			GoType:       "uuid.UUID",
			Import:       "github.com/gofrs/uuid/v5",
			Required:     true,
			InPrimaryKey: false,
		},
	}
}

// TestUpsert_OmittedWithoutAConflictTarget pins the no-target case. A table
// whose key is app-enforced through `primary_key.columns`, with no UNIQUE
// beside it, emits no conflict-target constant (PRD §9.5), so the only value
// Upsert's `target` argument could take is the zero value. That names no
// conflict columns, the builder drops the conflict clause, and the call is a
// plain INSERT: measured on PostgreSQL, MySQL and SQLite, a second Upsert of
// the same key inserts a duplicate row and returns the first one, and
// UpsertMany of two inputs returns four entities. The methods and the empty
// enum are omitted instead, which is the conflict-target rule UpsertWithRelated
// follows, applied to the methods it composes.
func TestUpsert_OmittedWithoutAConflictTarget(t *testing.T) {
	cols := []parser.Column{
		{Name: "id", Type: "uuid", PrimaryKey: true},
		{Name: "code", Type: "text"},
	}
	override := &config.TablePrimaryKeyConfig{Columns: []string{"id"}}
	tests := []struct {
		name       string
		table      parser.Table
		pk         *config.TablePrimaryKeyConfig
		wantUpsert bool
	}{
		{
			name: "schema-declared key keeps Upsert",
			table: parser.Table{
				Name: "probe_keys", Schema: "public", Columns: cols,
				Constraints: []parser.Constraint{{Name: "probe_keys_pkey", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			},
			wantUpsert: true,
		},
		{
			name:  "app-enforced key with no UNIQUE omits Upsert",
			table: parser.Table{Name: "probe_keys", Schema: "public", Columns: cols},
			pk:    override,
		},
		{
			name: "app-enforced key beside a UNIQUE keeps Upsert on that target",
			table: parser.Table{
				Name: "probe_keys", Schema: "public", Columns: cols,
				Constraints: []parser.Constraint{{Name: "probe_keys_code_key", Type: parser.Unique, Columns: []string{"code"}}},
			},
			pk:         override,
			wantUpsert: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(&parser.Schema{Tables: []parser.Table{tt.table}})
			if tt.pk != nil {
				input.Config.Tables["probe_keys"] = config.TableConfig{PrimaryKey: tt.pk}
			}
			contexts, err := gen.BuildTableContextsFromSchema(input.Schema, input.Config)
			if err != nil {
				t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("got %d table contexts, want 1", len(contexts))
			}
			tc := contexts[0]

			if got := len(tc.ConflictTargets) > 0; got != tt.wantUpsert {
				t.Fatalf("fixture is wrong: has conflict targets = %v, want %v", got, tt.wantUpsert)
			}
			ops := tc.Operations
			if ops.Upsert != tt.wantUpsert || ops.UpsertMany != tt.wantUpsert || (ops.UpsertWithRelated && !tt.wantUpsert) {
				t.Errorf("Operations Upsert=%v UpsertMany=%v UpsertWithRelated=%v, want Upsert=UpsertMany=%v",
					ops.Upsert, ops.UpsertMany, ops.UpsertWithRelated, tt.wantUpsert)
			}

			out := executeUpsertTemplate(t, tc, sql.NewPostgresDialect()) +
				executeUpsertManyTemplate(t, tc, sql.NewPostgresDialect()) +
				renderTableTemplate(t, loadTableTemplates(t, "client.go.tmpl"), "table/client", tc)
			for _, frag := range []string{
				"type ProbeKeyConflictTarget int",
				"probeKeyConflictColumns = map[ProbeKeyConflictTarget][]string{",
				"func (c *probeKeyClient) Upsert(",
				"func (c *probeKeyClient) UpsertMany(",
				"\tUpsert(ctx context.Context",
				"\tUpsertMany(ctx context.Context",
			} {
				if got := strings.Contains(out, frag); got != tt.wantUpsert {
					t.Errorf("output contains %q = %v, want %v", frag, got, tt.wantUpsert)
				}
			}
		})
	}
}
