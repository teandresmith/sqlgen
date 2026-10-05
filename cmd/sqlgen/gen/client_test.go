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

func loadClientTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("client.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "client.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing client template: %v", err)
	}
	return tmpl
}

func executeClientTemplate(t *testing.T, ctx gen.TableContext) string {
	t.Helper()
	tmpl := loadClientTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/client", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func testClientContext_fullOps() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Description:       "Catalog of products available for sale.",
		Package:           "db",
		Imports:           []string{"context", "github.com/gofrs/uuid/v5", "github.com/teandresmith/sqlgen/database", "github.com/teandresmith/sqlgen/hook"},
		Columns: []gen.ColumnContext{
			{FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{FieldName: "SKU", GoType: "string", DBTag: "sku", JSONTag: "sku"},
			{FieldName: "CompanyID", GoType: "uuid.NullUUID", DBTag: "company_id", JSONTag: "company_id", Nullable: true, Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "DeletedAt", GoType: "*time.Time", DBTag: "deleted_at", JSONTag: "deleted_at", Nullable: true, Import: "time"},
			{FieldName: "UpdatedAt", GoType: "time.Time", DBTag: "updated_at", JSONTag: "updated_at", Import: "time"},
		},
		PKColumns: []gen.ColumnContext{
			{FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
		},
		SoftDelete: &gen.SoftDeleteContext{
			Column:    "deleted_at",
			FieldName: "DeletedAt",
			Strategy:  "timestamp",
		},
		ExcludeDeleted: true,
		StrictUpdates:  true,
		IncrementColumns: []gen.ColumnContext{
			{FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
		},
		Operations: gen.ResolvedOperations{
			Get:         true,
			GetMany:     true,
			Create:      true,
			CreateMany:  true,
			Update:      true,
			UpdateMany:  true,
			UpdateWhere: true,
			Upsert:      true,
			SoftDelete:  true,
			Restore:     true,
			HardDelete:  true,
			Exists:      true,
			Count:       true,
			Increment:   true,
			Paginate:    true,
			Connection:  true,
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		PageSize:   100,
		CursorKeys: []string{"id"},
	}
}

func testClientContext_readOnly() gen.TableContext {
	ctx := testClientContext_fullOps()
	ctx.SoftDelete = nil
	ctx.ExcludeDeleted = false
	ctx.IncrementColumns = nil
	ctx.Operations = gen.ResolvedOperations{
		Get:        true,
		GetMany:    true,
		Exists:     true,
		Count:      true,
		Paginate:   true,
		Connection: true,
	}
	return ctx
}

func testClientContext_noSoftDelete() gen.TableContext {
	ctx := testClientContext_fullOps()
	ctx.SoftDelete = nil
	ctx.ExcludeDeleted = false
	ctx.Operations.SoftDelete = false
	ctx.Operations.Restore = false
	return ctx
}

func testClientContext_noIncrement() gen.TableContext {
	ctx := testClientContext_fullOps()
	ctx.IncrementColumns = nil
	return ctx
}

func testClientContext_compositePK() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		Description:           "Line item in an order.",
		Package:               "db",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Imports:               []string{"context", "github.com/gofrs/uuid/v5", "github.com/teandresmith/sqlgen/database", "github.com/teandresmith/sqlgen/hook"},
		Columns: []gen.ColumnContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		PKColumns: []gen.ColumnContext{
			{FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
			{FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5"},
		},
		SoftDelete: &gen.SoftDeleteContext{
			Column:    "deleted_at",
			FieldName: "DeletedAt",
			Strategy:  "timestamp",
		},
		ExcludeDeleted: true,
		StrictUpdates:  true,
		IncrementColumns: []gen.ColumnContext{
			{FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		Operations: gen.ResolvedOperations{
			Get:         true,
			GetMany:     true,
			Create:      true,
			CreateMany:  true,
			Update:      true,
			UpdateMany:  true,
			UpdateWhere: true,
			Upsert:      true,
			SoftDelete:  true,
			Restore:     true,
			HardDelete:  true,
			Exists:      true,
			Count:       true,
			Increment:   true,
			Paginate:    true,
			Connection:  true,
		},
		Dialect:    "postgres",
		Driver:     "pgx",
		PageSize:   100,
		CursorKeys: []string{"id"},
	}
}

func TestClientTemplate_fullOperations(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	wantPatterns := []string{
		"type ProductClient interface {",
		"Get(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"GetMany(ctx context.Context, input *GetProductsInput, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"Exists(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (bool, error)",
		"ExistsWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) (bool, error)",
		"Count(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) (int64, error)",
		"Create(ctx context.Context, input *CreateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"CreateMany(ctx context.Context, inputs []*CreateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"Update(ctx context.Context, id uuid.UUID, input *UpdateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"UpdateMany(ctx context.Context, items []UpdateProductItem, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"UpdateWhere(ctx context.Context, filter *ProductFilter, input *UpdateProductInput, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"Upsert(ctx context.Context, input *CreateProductInput, target ProductConflictTarget, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"Increment(ctx context.Context, id uuid.UUID, input IncrementInput[ProductIncrementColumn], opts ...func(*CallOptions[ProductFieldOptions])) error",
		"SoftDelete(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"SoftDeleteMany(ctx context.Context, ids []uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"SoftDeleteWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"Restore(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) (*Product, error)",
		"RestoreMany(ctx context.Context, ids []uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"RestoreWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) ([]*Product, error)",
		"HardDelete(ctx context.Context, id uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) error",
		"HardDeleteMany(ctx context.Context, ids []uuid.UUID, opts ...func(*CallOptions[ProductFieldOptions])) error",
		"HardDeleteWhere(ctx context.Context, filter *ProductFilter, opts ...func(*CallOptions[ProductFieldOptions])) error",
		"Paginate(ctx context.Context, input PaginateInput[ProductFilter], opts ...func(*CallOptions[ProductFieldOptions])) (*PaginateResult[Product], error)",
		"Connection(ctx context.Context, input ConnectionInput[ProductFilter], opts ...func(*CallOptions[ProductFieldOptions])) (*Connection[Product], error)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestClientTemplate_readOnlyOperations(t *testing.T) {
	ctx := testClientContext_readOnly()
	output := executeClientTemplate(t, ctx)

	// Should be present.
	wantPresent := []string{
		"Get(ctx context.Context, id uuid.UUID,",
		"GetMany(ctx context.Context, input *GetProductsInput,",
		"Exists(ctx context.Context, id uuid.UUID,",
		"ExistsWhere(ctx context.Context, filter *ProductFilter,",
		"Count(ctx context.Context, filter *ProductFilter,",
		"Paginate(ctx context.Context, input PaginateInput[ProductFilter],",
		"Connection(ctx context.Context, input ConnectionInput[ProductFilter],",
	}
	for _, want := range wantPresent {
		if !strings.Contains(output, want) {
			t.Errorf("output missing read-only method: %q", want)
		}
	}

	// Should NOT be present.
	wantAbsent := []string{
		"Create(ctx",
		"CreateMany(ctx",
		"Update(ctx",
		"UpdateMany(ctx",
		"UpdateWhere(ctx",
		"Upsert(ctx",
		"Increment(ctx",
		"SoftDelete(ctx",
		"SoftDeleteMany(ctx",
		"SoftDeleteWhere(ctx",
		"Restore(ctx",
		"RestoreMany(ctx",
		"RestoreWhere(ctx",
		"HardDelete(ctx",
		"HardDeleteMany(ctx",
		"HardDeleteWhere(ctx",
	}
	for _, absent := range wantAbsent {
		if strings.Contains(output, absent) {
			t.Errorf("read-only output should not contain: %q", absent)
		}
	}
}

func TestClientTemplate_softDeleteConditional(t *testing.T) {
	ctx := testClientContext_noSoftDelete()
	output := executeClientTemplate(t, ctx)

	absentMethods := []string{
		"SoftDelete(ctx",
		"SoftDeleteMany(ctx",
		"SoftDeleteWhere(ctx",
		"Restore(ctx",
		"RestoreMany(ctx",
		"RestoreWhere(ctx",
	}
	for _, absent := range absentMethods {
		if strings.Contains(output, absent) {
			t.Errorf("output without soft delete should not contain: %q", absent)
		}
	}

	// Hard delete should still be present.
	if !strings.Contains(output, "HardDelete(ctx") {
		t.Error("output should still contain HardDelete when soft delete is disabled")
	}
}

func TestClientTemplate_incrementConditional(t *testing.T) {
	ctx := testClientContext_noIncrement()
	output := executeClientTemplate(t, ctx)

	if strings.Contains(output, "Increment(ctx") {
		t.Error("output should not contain Increment when no numeric non-PK columns exist")
	}
}

func TestClientTemplate_compositePKSignatures(t *testing.T) {
	ctx := testClientContext_compositePK()
	output := executeClientTemplate(t, ctx)

	wantPatterns := []string{
		"type OrderItemClient interface {",
		"Get(ctx context.Context, pk OrderItemPK,",
		"Exists(ctx context.Context, pk OrderItemPK,",
		"Update(ctx context.Context, pk OrderItemPK, input *UpdateOrderItemInput,",
		"Increment(ctx context.Context, pk OrderItemPK, input IncrementInput[OrderItemIncrementColumn],",
		"SoftDelete(ctx context.Context, pk OrderItemPK,",
		"SoftDeleteMany(ctx context.Context, pks []OrderItemPK,",
		"Restore(ctx context.Context, pk OrderItemPK,",
		"RestoreMany(ctx context.Context, pks []OrderItemPK,",
		"HardDelete(ctx context.Context, pk OrderItemPK,",
		"HardDeleteMany(ctx context.Context, pks []OrderItemPK,",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK pattern: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Should NOT use single "id" parameter.
	if strings.Contains(output, "id uuid.UUID") {
		t.Error("composite PK interface should not use 'id uuid.UUID' parameter")
	}
}

func TestClientTemplate_callOptionsGeneric(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	// All methods must use CallOptions[ProductFieldOptions].
	if !strings.Contains(output, "CallOptions[ProductFieldOptions]") {
		t.Error("output should reference CallOptions[ProductFieldOptions]")
	}

	// Count how many method signatures use the generic CallOptions.
	count := strings.Count(output, "CallOptions[ProductFieldOptions]")
	// There should be at least one per method (23 methods in full ops).
	if count < 23 {
		t.Errorf("expected at least 23 uses of CallOptions[ProductFieldOptions], got %d", count)
	}
}

func TestClientTemplate_unexportedStruct(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	wantPatterns := []string{
		"type productClient struct {",
		"querier",
		"dialect",
		"table",
		"batchSize",
		"queryLimit",
		"pageSize",
		"cursorKeys",
		"strictUpdates",
		"excludeDeleted",
		"mutationHooks  []hook.MutationHook",
		"queryHooks     []hook.QueryHook",
		"panicHandler   hook.PanicHandler",
		"func newProductClient(querier database.Querier, mutationHooks []hook.MutationHook, queryHooks []hook.QueryHook, panicHandler hook.PanicHandler) *productClient {",
		"dialect:",
		"table:",
		"pageSize:",
		"cursorKeys:",
		"mutationHooks:",
		"queryHooks:",
		"panicHandler:",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing struct/constructor pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestClientTemplate_unexportedStruct_compositePK(t *testing.T) {
	ctx := testClientContext_compositePK()
	output := executeClientTemplate(t, ctx)

	wantPatterns := []string{
		"type orderItemClient struct {",
		"querier",
		"dialect",
		"table",
		"batchSize",
		"queryLimit",
		"strictUpdates",
		"excludeDeleted",
		"mutationHooks  []hook.MutationHook",
		"queryHooks     []hook.QueryHook",
		"panicHandler   hook.PanicHandler",
		"func newOrderItemClient(querier database.Querier, mutationHooks []hook.MutationHook, queryHooks []hook.QueryHook, panicHandler hook.PanicHandler) *orderItemClient {",
		"dialect:",
		"table:",
		"mutationHooks:",
		"queryHooks:",
		"panicHandler:",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing composite PK struct pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestClientTemplate_docComments(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	wantPatterns := []string{
		"// ProductClient provides type-safe operations for the products table.",
		"// Get returns a single product by primary key.",
		"// Soft-deleted products are excluded by default",
		"// GetMany returns products matching the given filter",
		"// Create inserts a new product and returns the created entity.",
		"// SoftDelete marks a product as deleted by setting the deleted_at column",
		"// Restore clears the deleted_at column",
		"// HardDelete permanently removes a product from the database.",
		"// Paginate returns a page of products with total count",
		"// Connection returns a Relay-style cursor-paginated connection",
		"// Increment atomically increments or decrements a numeric column",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing doc comment: %q", want)
		}
	}
}

func TestClientTemplate_softDeleteDocComments(t *testing.T) {
	ctx := testClientContext_noSoftDelete()
	output := executeClientTemplate(t, ctx)

	// Without soft delete, soft delete doc comments should not appear.
	if strings.Contains(output, "Soft-deleted") {
		t.Error("output without soft delete should not contain soft delete doc comments")
	}
}

func TestClientTemplate_strictUpdatesDocComments(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	if !strings.Contains(output, "Returns ErrNotFound if the primary key does not exist.") {
		t.Error("output should contain ErrNotFound doc comment for strict updates")
	}

	// Without strict updates, should not have the ErrNotFound comment.
	ctx.StrictUpdates = false
	output = executeClientTemplate(t, ctx)
	if strings.Contains(output, "Returns ErrNotFound if the primary key does not exist.") {
		t.Error("output without strict updates should not contain ErrNotFound doc comment")
	}
}

func TestClientTemplate_goldenFile_fullOps(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_client_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "client_products_gen.go")

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
		t.Errorf("client_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestClientTemplate_goldenFile_readOnly(t *testing.T) {
	ctx := testClientContext_readOnly()
	output := executeClientTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_client_readonly_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "client_products_readonly_gen.go")

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
		t.Errorf("client_products_readonly_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestClientTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testClientContext_compositePK()
	output := executeClientTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_client_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "client_order_items_gen.go")

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
		t.Errorf("client_order_items_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestClientTemplate_compilesCleanly(t *testing.T) {
	ctx := testClientContext_fullOps()
	output := executeClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestClientTemplate_compilesCleanly_readOnly(t *testing.T) {
	ctx := testClientContext_readOnly()
	output := executeClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestClientTemplate_compilesCleanly_compositePK(t *testing.T) {
	ctx := testClientContext_compositePK()
	output := executeClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// TestClientTemplate_relationshipDefaultSort pins that a relationship's
// static `sort:` reaches the <rel>DefaultSort field the O2M / M2M loaders fall
// back to (PRD §4.8, §13.2). A field that is declared but never assigned
// silently drops the configured order.
func TestClientTemplate_relationshipDefaultSort(t *testing.T) {
	ctx := testClientContext_fullOps()
	ctx.HasO2MRelationships = true
	ctx.O2MRelationships = []gen.RelationshipContext{
		{FieldName: "Reviews", Sort: []gen.SortContext{
			{Column: "created_at", Direction: "DESC"},
			{Column: "id", Direction: "ASC"},
		}},
		{FieldName: "Notes"},
	}
	ctx.M2MRelationships = []gen.RelationshipContext{
		{FieldName: "Tags", Sort: []gen.SortContext{{Column: "name", Direction: "ASC"}}},
	}

	formatted, err := gen.FormatOnly([]byte(executeClientTemplate(t, ctx)), "v0.0.0-test", "products_client_gen.go")
	if err != nil {
		t.Fatalf("FormatOnly: %v", err)
	}
	got := string(formatted)

	for _, want := range []string{
		`reviewsDefaultSort: []sql.Sort{{Column: "created_at", Direction: sql.Desc}, {Column: "id", Direction: sql.Asc}},`,
		`tagsDefaultSort:    []sql.Sort{{Column: "name", Direction: sql.Asc}},`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("constructor missing %q\n\nfull output:\n%s", want, got)
		}
	}
	// An edge with no static sort keeps the nil fallback: the loader then
	// applies no ORDER BY (PRD §4.8 "If omitted, no ordering is applied").
	if strings.Contains(got, "notesDefaultSort:") {
		t.Errorf("an unsorted relationship should not assign notesDefaultSort\n\nfull output:\n%s", got)
	}
}

// --- Test: Stream's interface doc carries the in-transaction rule ---

// client.Products() returns the interface, so the interface's doc comment is
// the one a consumer reads at the call site — the implementation's is not
// enough on its own (PRD §9.4a, §18.5).
func TestClientTemplate_streamInterfaceDocCarriesTheRule(t *testing.T) {
	ctx := testClientContext_fullOps()
	ctx.Operations.Stream = true
	doc := docCommentAbove(t, executeClientTemplate(t, ctx), "\tStream(ctx context.Context")

	for _, want := range []string{
		"Stream refuses an active transaction unless CallOptions.AllowInTransaction",
		"drain or close a result set\n\t// before issuing the next statement on that txCtx",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("Stream interface doc missing %q; doc:\n%s", want, doc)
		}
	}
}
