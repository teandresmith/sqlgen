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

func loadCreateTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("create.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "create.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing create template: %v", err)
	}
	return tmpl
}

func executeCreateTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadCreateTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/create", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testCreateContext_dbStrategyUUID_postgres returns a product context with PK strategy "db",
// UUID PK with DEFAULT (omittable), PostgreSQL dialect.
func testCreateContext_dbStrategyUUID_postgres() gen.TableContext {
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
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
			{FieldName: "UpdatedAt", GoType: "omittable.Value[time.Time]", ColumnName: "updated_at", Omittable: true, JSONTag: "updated_at"},
		},
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// testCreateContext_dbStrategyAutoIncrement_mysql returns a context with MySQL AUTO_INCREMENT PK (strategy "db").
func testCreateContext_dbStrategyAutoIncrement_mysql() gen.TableContext {
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
			// PK excluded — AUTO_INCREMENT
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Payload", GoType: "string", ColumnName: "payload", Required: true, JSONTag: "payload"},
		},
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// testCreateContext_appStrategyV4 returns a product context with PK strategy "app", UUID v4, PostgreSQL.
func testCreateContext_appStrategyV4() gen.TableContext {
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
		UUIDVersion:       "v4",
		// gofrs's v4 value spelling, matching the import below. Production
		// fills this in attachUUIDGeneration from the package's selected
		// integration; a hand-built app-strategy fixture must set it or the
		// create/upsert templates emit a bare `pkValue = `.
		PKAutoGenExpr: "uuid.Must(uuid.NewV4())",
		BatchSize:     100,
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
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "Price", GoType: "float64", ColumnName: "price", Required: true, JSONTag: "price"},
			{FieldName: "SKU", GoType: "string", ColumnName: "sku", Required: true, JSONTag: "sku"},
			{FieldName: "ID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "id", Omittable: true, JSONTag: "id"},
			{FieldName: "CompanyID", GoType: "omittable.Value[uuid.NullUUID]", ColumnName: "company_id", Omittable: true, JSONTag: "company_id"},
		},
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// testCreateContext_appStrategyV7 returns a product context with PK strategy "app", UUID v7, PostgreSQL.
func testCreateContext_appStrategyV7() gen.TableContext {
	ctx := testCreateContext_appStrategyV4()
	ctx.UUIDVersion = "v7"
	ctx.PKAutoGenExpr = "uuid.Must(uuid.NewV7())"
	return ctx
}

// testCreateContext_callerStrategy returns a context with PK strategy "caller", non-composite, PostgreSQL.
func testCreateContext_callerStrategy() gen.TableContext {
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
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// testCreateContext_compositePK returns an order_items context with composite PK.
func testCreateContext_compositePK() gen.TableContext {
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
			{FieldName: "DeletedAt", GoType: "omittable.Value[*time.Time]", ColumnName: "deleted_at", Omittable: true, JSONTag: "deleted_at"},
		},
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// testCreateContext_appStrategy_mysql returns a MySQL context with app strategy (UUID, no RETURNING).
func testCreateContext_appStrategy_mysql() gen.TableContext {
	ctx := testCreateContext_appStrategyV4()
	ctx.Dialect = "mysql"
	ctx.Driver = "stdlib"
	return ctx
}

// --- Test: Create with PK strategy "db" (PostgreSQL RETURNING) ---

func TestCreateTemplate_dbStrategy_postgres(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Create(ctx context.Context, input *CreateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"options := resolveCallOptions(opts)",
		"conn := database.Conn(ctx, c.querier)",
		`columns := []string{"name", "price", "sku", }`,
		`args := []any{input.Name, input.Price, input.SKU, }`,
		// db strategy UUID — PK handled as regular omittable field
		"if v, ok := input.ID.Get(); ok {",
		`columns = append(columns, "id")`,
		// Other omittable fields
		"if v, ok := input.CompanyID.Get(); ok {",
		"if v, ok := input.DeletedAt.Get(); ok {",
		"if v, ok := input.UpdatedAt.Get(); ok {",
		// insertAndResolveID path — always resolves PK for events
		"id, err := c.insertAndResolveID(ctx, conn, columns, args)",
		`fmt.Errorf("create product: %w", err)`,
		"m.AffectedPKs = []any{id}",
		// Empty FieldOptions short-circuit
		"if options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns() {",
		"return nil, nil",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Create with PK strategy "db" (MySQL LastInsertId) ---

func TestCreateTemplate_dbStrategy_mysql(t *testing.T) {
	ctx := testCreateContext_dbStrategyAutoIncrement_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *eventClient) Create(ctx context.Context, input *CreateEventInput",
		`columns := []string{"name", "payload", }`,
		`args := []any{input.Name, input.Payload, }`,
		// insertAndResolveID path (MySQL db strategy)
		"id, err := c.insertAndResolveID(ctx, conn, columns, args)",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have PK in columns (auto-increment excluded)
	if strings.Contains(output, `"id"`) && strings.Contains(output, "input.ID") {
		t.Error("auto-increment PK should not appear in columns or args")
	}

	// insertAndResolveID should use LastInsertId, not RETURNING
	if !strings.Contains(output, "execResult.LastInsertId()") {
		t.Errorf("MySQL insertAndResolveID should use LastInsertId\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL insertAndResolveID should not use RETURNING")
	}
}

// --- Test: Create with PK strategy "app" (UUID v4 generation) ---

func TestCreateTemplate_appStrategy_v4(t *testing.T) {
	ctx := testCreateContext_appStrategyV4()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Create(ctx context.Context, input *CreateProductInput",
		// App strategy PK handling
		"var pkValue uuid.UUID",
		"if v, ok := input.ID.Get(); ok {",
		"pkValue = v",
		"pkValue = uuid.Must(uuid.NewV4())",
		`columns = append(columns, "id")`,
		"args = append(args, pkValue)",
		// Other omittable fields still handled
		"if v, ok := input.CompanyID.Get(); ok {",
		// insertAndResolveID (RETURNING on PostgreSQL)
		"id, err := c.insertAndResolveID(ctx, conn, columns, args)",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Create with PK strategy "app" (UUID v7 generation) ---

func TestCreateTemplate_appStrategy_v7(t *testing.T) {
	ctx := testCreateContext_appStrategyV7()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	// Should use uuid.Must(uuid.NewV7()) instead of uuid.New()
	if !strings.Contains(output, "uuid.Must(uuid.NewV7())") {
		t.Errorf("output should use uuid.Must(uuid.NewV7()) for v7\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "uuid.New()") {
		t.Errorf("v7 strategy should not use uuid.New()\n\nfull output:\n%s", output)
	}
}

// --- app-strategy PK with `string` GoType ---
//
// When no `uuid` type override is configured, gotype maps SQL `uuid` →
// Go `string`. The app-strategy PK path must emit `uuid.NewString()` /
// `uuid.Must(uuid.NewV7()).String()` so the generated assignment
// `var pkValue string ... pkValue = <gen>` compiles. Returning
// `uuid.UUID` here was the original compile error.

func testCreateContext_appStrategy_stringPK(version config.UUIDVersion) gen.TableContext {
	ctx := testCreateContext_appStrategyV4()
	ctx.UUIDVersion = version
	// google-bound, matching the upsert twin: the point of the shape is the
	// `.String()` suffix, and google is the one integration whose v4 string
	// form is a different call rather than a suffix of its value form.
	ctx.Imports = []string{
		"context",
		"fmt",
		"github.com/google/uuid",
		"github.com/teandresmith/sqlgen/comparator",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/sql",
	}
	ctx.PKAutoGenExpr = "uuid.NewString()"
	if version == config.UUIDVersionV7 {
		ctx.PKAutoGenExpr = "uuid.Must(uuid.NewV7()).String()"
	}
	ctx.PKColumns = []gen.ColumnContext{
		{Name: "id", FieldName: "ID", GoType: "string", ZeroValue: `""`, DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringNone},
	}
	ctx.CreateInputFields = []gen.InputFieldContext{
		{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
		{FieldName: "Price", GoType: "float64", ColumnName: "price", Required: true, JSONTag: "price"},
		{FieldName: "SKU", GoType: "string", ColumnName: "sku", Required: true, JSONTag: "sku"},
		{FieldName: "ID", GoType: "omittable.Value[string]", ColumnName: "id", Omittable: true, JSONTag: "id"},
	}
	return ctx
}

func TestCreateTemplate_appStrategy_v4_stringPK(t *testing.T) {
	// PostgreSQL: needsInsertResolve=true → createMany batch uses `pkAny any`.
	pgOut := executeCreateTemplate(t, testCreateContext_appStrategy_stringPK(config.UUIDVersionV4), sql.NewPostgresDialect())

	wantPG := []string{
		"var pkValue string",
		"pkValue = uuid.NewString()", // single Create
		"pkAny = uuid.NewString()",   // createMany via insertAndResolveID branch
	}
	for _, want := range wantPG {
		if !strings.Contains(pgOut, want) {
			t.Errorf("postgres output missing: %q\n\nfull output:\n%s", want, pgOut)
		}
	}

	// MySQL: no RETURNING → needsInsertResolve=false → createMany batch uses `pkValue` + `batchIDs []string`.
	myCtx := testCreateContext_appStrategy_stringPK(config.UUIDVersionV4)
	myCtx.Dialect = "mysql"
	myCtx.Driver = "stdlib"
	myOut := executeCreateTemplate(t, myCtx, sql.NewMySQLDialect())

	wantMySQL := []string{
		"var pkValue string",
		"var batchIDs []string",
		"pkValue = uuid.NewString()",
	}
	for _, want := range wantMySQL {
		if !strings.Contains(myOut, want) {
			t.Errorf("mysql output missing: %q\n\nfull output:\n%s", want, myOut)
		}
	}

	// Across both dialects: must NOT assign uuid.UUID to a `string` variable.
	for _, out := range []string{pgOut, myOut} {
		for _, bad := range []string{"pkValue = uuid.New()", "pkAny = uuid.New()"} {
			if strings.Contains(out, bad) {
				t.Errorf("output must not contain %q (would assign uuid.UUID to string)\n\nfull output:\n%s", bad, out)
			}
		}
	}
}

func TestCreateTemplate_appStrategy_v7_stringPK(t *testing.T) {
	ctx := testCreateContext_appStrategy_stringPK(config.UUIDVersionV7)
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "pkValue = uuid.Must(uuid.NewV7()).String()") {
		t.Errorf("v7 + string PK should use uuid.Must(uuid.NewV7()).String()\n\nfull output:\n%s", output)
	}
	// Must NOT use the uuid.UUID-returning form, which won't assign to `string`.
	if strings.Contains(output, "pkValue = uuid.Must(uuid.NewV7())\n") || strings.Contains(output, "pkValue = uuid.Must(uuid.NewV7()) ") {
		t.Errorf("v7 + string PK must not assign uuid.UUID to string var\n\nfull output:\n%s", output)
	}
}

// --- Test: Create with PK strategy "caller" ---

func TestCreateTemplate_callerStrategy(t *testing.T) {
	ctx := testCreateContext_callerStrategy()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *tenantClient) Create(ctx context.Context, input *CreateTenantInput",
		// PK is required — in static columns/args
		`columns := []string{"id", "name", "slug", }`,
		`args := []any{input.ID, input.Name, input.Slug, }`,
		// insertAndResolveID (RETURNING on PostgreSQL)
		"id, err := c.insertAndResolveID(ctx, conn, columns, args)",
		"o.SkipHooks = true",
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

// --- Test: Create with composite PK ---

func TestCreateTemplate_compositePK(t *testing.T) {
	ctx := testCreateContext_compositePK()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) Create(ctx context.Context, input *CreateOrderItemInput",
		// All PK columns required
		`"order_id", "product_id", "unit_price"`,
		"input.OrderID, input.ProductID, input.UnitPrice",
		// Omittable non-PK fields
		"if v, ok := input.Quantity.Get(); ok {",
		"if v, ok := input.DeletedAt.Get(); ok {",
		// Direct Exec (no insertAndResolveID)
		"sql.BuildInsert(c.dialect, c.table, sql.InsertOptions{",
		"conn.Exec(ctx, query, queryArgs...)",
		// Get with composite PK — uses SkipHooks to prevent nested hook invocation
		"c.Get(ctx, OrderItemPK{OrderID: input.OrderID, ProductID: input.ProductID, }, func(o *CallOptions[OrderItemFieldOptions])",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have insertAndResolveID
	if strings.Contains(output, "insertAndResolveID") {
		t.Error("composite PK should not use insertAndResolveID")
	}
}

// --- Test: CreateMany with batch splitting ---

func TestCreateTemplate_createMany_batchSplitting(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) CreateMany(ctx context.Context, inputs []*CreateProductInput",
		"for start := 0; start < len(inputs); start += c.batchSize {",
		"end := min(start+c.batchSize, len(inputs))",
		"batch := inputs[start:end]",
		"var allIDs []uuid.UUID",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: CreateMany multi-row INSERT ---

func TestCreateTemplate_createMany_multiRowInsert(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"var valueRows [][]any",
		"for _, input := range batch {",
		"sql.Default",
		"valueRows = append(valueRows, []any{",
		// multiInsertAndResolveIDs for PostgreSQL
		"ids, err := c.multiInsertAndResolveIDs(ctx, conn, columns, valueRows)",
		"allIDs = append(allIDs, ids...)",
		// Re-fetch
		"idStrings := make([]string, len(allIDs))",
		"idStrings[i] = id.String()",
		`ID: &comparator.ID{In: idStrings}`,
		"Limit: new(0)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: CreateMany with no writable column ---

// testCreateContext_noWritableColumn is a table whose only column is a
// database-generated key, so Create<T>Input has no field at all.
func testCreateContext_noWritableColumn(dialect config.Dialect) gen.TableContext {
	return gen.TableContext{
		StructName:        "KeyOnlyRow",
		TableName:         "key_only_rows",
		TableNameConstant: "TableKeyOnlyRows",
		Package:           "db",
		VarName:           "k",
		Dialect:           dialect,
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
		Operations: gen.ResolvedOperations{
			Create:     true,
			CreateMany: true,
		},
	}
}

// A multi-row INSERT must name a column: PostgreSQL and SQLite reject
// `() VALUES (), ()`. With no writable column, CreateMany names the key and
// sends each dialect's default sentinel for it — and has no per-input loop
// variable left to leave unused, which would be a compile error.
func TestCreateTemplate_createMany_noWritableColumn(t *testing.T) {
	tests := []struct {
		name     string
		dialect  sql.Dialect
		ctxName  config.Dialect
		sentinel string
	}{
		{"postgres", sql.NewPostgresDialect(), "postgres", "valueRows[i] = []any{sql.Default}"},
		{"mysql", sql.NewMySQLDialect(), "mysql", "valueRows[i] = []any{sql.Default}"},
		{"sqlite", sql.NewSQLiteDialect(), "sqlite", `valueRows[i] = []any{sql.NewDefaultExpr("NULL")}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeCreateTemplate(t, testCreateContext_noWritableColumn(tt.ctxName), tt.dialect)

			for _, want := range []string{
				`columns := []string{"id"}`,
				"valueRows := make([][]any, len(batch))",
				"for i := range batch {",
				tt.sentinel,
				"ids, err := c.multiInsertAndResolveIDs(ctx, conn, columns, valueRows)",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
				}
			}
			if strings.Contains(output, "for _, input := range batch {") {
				t.Errorf("CreateMany kept a per-input loop with nothing to read from input\n\nfull output:\n%s", output)
			}
		})
	}
}

// --- Test: CreateMany composite PK ---

func TestCreateTemplate_createMany_compositePK(t *testing.T) {
	ctx := testCreateContext_compositePK()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *orderItemClient) CreateMany(ctx context.Context, inputs []*CreateOrderItemInput",
		"var allPKs []OrderItemPK",
		// No multiInsertAndResolveIDs
		"sql.BuildMultiInsert(c.dialect, c.table, sql.MultiInsertOptions{",
		"conn.Exec(ctx, query, queryArgs...)",
		// Collect PKs from input
		"allPKs = append(allPKs, OrderItemPK{OrderID: input.OrderID, ProductID: input.ProductID, })",
		// Re-fetch with PKs filter
		`Filter: &OrderItemFilter{PKs: allPKs}`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have multiInsertAndResolveIDs
	if strings.Contains(output, "multiInsertAndResolveIDs") {
		t.Error("composite PK CreateMany should not use multiInsertAndResolveIDs")
	}
}

// --- Test: insertAndResolveID — PostgreSQL RETURNING ---

func TestCreateTemplate_insertAndResolveID_postgres(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) insertAndResolveID(ctx context.Context, conn database.Querier, columns []string, args []any) (uuid.UUID, error)",
		`ReturningColumns: []string{"id"}`,
		"var id uuid.UUID",
		"conn.QueryRow(ctx, query, queryArgs...).Scan(&id)",
		"return uuid.UUID{}, err",
		"return id, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: insertAndResolveID — MySQL LastInsertId ---

func TestCreateTemplate_insertAndResolveID_mysql(t *testing.T) {
	ctx := testCreateContext_dbStrategyAutoIncrement_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *eventClient) insertAndResolveID(ctx context.Context, conn database.Querier, columns []string, args []any) (int64, error)",
		"conn.Exec(ctx, query, queryArgs...)",
		"execResult.LastInsertId()",
		"return 0, err",
		"return int64(rawID), nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT use RETURNING
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL insertAndResolveID should not use RETURNING")
	}
}

// --- Test: multiInsertAndResolveIDs — PostgreSQL RETURNING ---

func TestCreateTemplate_multiInsertAndResolveIDs_postgres(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) multiInsertAndResolveIDs(ctx context.Context, conn database.Querier, columns []string, valueRows [][]any) ([]uuid.UUID, error)",
		"sql.BuildMultiInsert(c.dialect, c.table, sql.MultiInsertOptions{",
		`ReturningColumns: []string{"id"}`,
		"conn.Query(ctx, query, args...)",
		"defer rows.Close()",
		"var ids []uuid.UUID",
		"rows.Next()",
		"rows.Scan(&id)",
		`fmt.Errorf("scan product id: %w", err)`,
		"rows.Err()",
		"return ids, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// RETURNING reports the keys, so nothing is derived and no session is pinned.
	for _, unwanted := range []string{"insertAndDeriveIDs", "SessionPinner", "auto_increment_increment"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output contains %q — only the MySQL variant derives keys\n\nfull output:\n%s", unwanted, output)
		}
	}
}

// --- Test: multiInsertAndResolveIDs — MySQL LastInsertId ---

// The MySQL variant derives a batch's keys from LastInsertId and the session's
// auto_increment_increment, so the INSERT and the read must share a session
// (PRD §9.8.5): a transaction as it is, the pool pinned without one.
func TestCreateTemplate_multiInsertAndResolveIDs_mysql(t *testing.T) {
	ctx := testCreateContext_dbStrategyAutoIncrement_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		"func (c *eventClient) multiInsertAndResolveIDs(ctx context.Context, conn database.Querier, columns []string, valueRows [][]any) ([]int64, error)",
		"if _, inTx := conn.(*database.Tx); inTx || len(valueRows) == 1 {",
		"pinner, ok := conn.(database.SessionPinner)",
		`errors.New("deriving the keys of a multi-row insert needs a transaction or a querier that implements database.SessionPinner")`,
		"err := pinner.WithSession(ctx, func(session database.Querier) error {",
		"ids, err = c.insertAndDeriveIDs(ctx, session, columns, valueRows)",
		"func (c *eventClient) insertAndDeriveIDs(ctx context.Context, session database.Querier, columns []string, valueRows [][]any) ([]int64, error)",
		"sql.BuildMultiInsert(c.dialect, c.table, sql.MultiInsertOptions{",
		"session.Exec(ctx, query, args...)",
		"execResult.LastInsertId()",
		`session.QueryRow(ctx, "SELECT @@auto_increment_increment").Scan(&step)`,
		"ids := make([]int64, len(valueRows))",
		"ids[i] = int64(firstID + int64(i)*step)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// A transaction around the INSERT would pin the session too, but it moves the
	// write's outcome to COMMIT, where Percona XtraDB Cluster can report a lost
	// certification conflict as a success. The pool path must not open one.
	for _, unwanted := range []string{"WithTransaction", "NewTransaction", ".Begin(ctx"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output contains %q — the key derivation must not open a transaction\n\nfull output:\n%s", unwanted, output)
		}
	}

	// Should NOT use RETURNING
	if strings.Contains(output, "ReturningColumns") {
		t.Error("MySQL multiInsertAndResolveIDs should not use RETURNING")
	}
}

// --- Test: No insertAndResolveID for composite PK ---

func TestCreateTemplate_noInsertResolve_compositePK(t *testing.T) {
	ctx := testCreateContext_compositePK()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "insertAndResolveID") {
		t.Error("composite PK should not generate insertAndResolveID")
	}
	if strings.Contains(output, "multiInsertAndResolveIDs") {
		t.Error("composite PK should not generate multiInsertAndResolveIDs")
	}
}

// --- Test: MySQL app strategy — no insertAndResolveID, known PK ---

func TestCreateTemplate_appStrategy_mysql_knownPK(t *testing.T) {
	ctx := testCreateContext_appStrategy_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	wantPatterns := []string{
		// App strategy PK handling
		"var pkValue uuid.UUID",
		"pkValue = uuid.Must(uuid.NewV4())",
		// Direct Exec (no insertAndResolveID)
		"sql.BuildInsert(c.dialect, c.table, sql.InsertOptions{",
		"conn.Exec(ctx, query, queryArgs...)",
		// Get with known pkValue — uses SkipHooks
		"c.Get(ctx, pkValue, func(o *CallOptions[ProductFieldOptions])",
		"o.SkipHooks = true",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT have insertAndResolveID
	if strings.Contains(output, "insertAndResolveID") {
		t.Error("MySQL app strategy should not use insertAndResolveID")
	}
}

// --- Test: Empty FieldOptions short-circuit ---

func TestCreateTemplate_emptyFieldOptions(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns()") {
		t.Errorf("output should contain FieldOptions short-circuit check\n\nfull output:\n%s", output)
	}
}

// --- Test: CreateMany empty FieldOptions skip fetch ---

func TestCreateTemplate_createMany_emptyFieldOptions(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	// The FieldOptions check should appear in CreateMany
	count := strings.Count(output, "options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns()")
	if count < 2 {
		t.Errorf("expected FieldOptions check in both Create and CreateMany, got %d occurrences", count)
	}
}

// --- Golden file tests ---

func TestCreateTemplate_goldenFile_dbStrategyPostgres(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_create_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "create_products_gen.go")

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
		t.Errorf("create_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testCreateContext_compositePK()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_create_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "create_order_items_gen.go")

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
		t.Errorf("create_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: Template compiles cleanly ---

func TestCreateTemplate_compilesCleanly_dbStrategyPostgres(t *testing.T) {
	ctx := testCreateContext_dbStrategyUUID_postgres()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestCreateTemplate_compilesCleanly_dbStrategyMySQL(t *testing.T) {
	ctx := testCreateContext_dbStrategyAutoIncrement_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "events_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestCreateTemplate_compilesCleanly_appStrategyV4(t *testing.T) {
	ctx := testCreateContext_appStrategyV4()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestCreateTemplate_compilesCleanly_callerStrategy(t *testing.T) {
	ctx := testCreateContext_callerStrategy()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "tenants_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestCreateTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testCreateContext_compositePK()
	output := executeCreateTemplate(t, ctx, sql.NewPostgresDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestCreateTemplate_compilesCleanly_appStrategyMySQL(t *testing.T) {
	ctx := testCreateContext_appStrategy_mysql()
	output := executeCreateTemplate(t, ctx, sql.NewMySQLDialect())

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_create_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}
