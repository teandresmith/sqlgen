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

func loadPaginationTableTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("pagination.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "pagination.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing pagination template: %v", err)
	}
	return tmpl
}

func executePaginationTableTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadPaginationTableTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/pagination", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---

// testPaginationContext_postgres returns a product context with soft delete and both operations.
func testPaginationContext_postgres() gen.TableContext {
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
		PageSize:          100,
		CursorKeys:        []string{"id"},
		Imports: []string{
			"context",
			"encoding/base64",
			"encoding/json",
			"fmt",
			"slices",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"time",
		},
		Columns: []gen.ColumnContext{
			{Name: "deleted_at", FieldName: "DeletedAt", GoType: "*time.Time", DBTag: "deleted_at", JSONTag: "deleted_at", Nullable: true, Import: "time"},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "price", FieldName: "Price", GoType: "float64", DBTag: "price", JSONTag: "price"},
			{Name: "sku", FieldName: "SKU", GoType: "string", DBTag: "sku", JSONTag: "sku"},
			{Name: "updated_at", FieldName: "UpdatedAt", GoType: "time.Time", DBTag: "updated_at", JSONTag: "updated_at", Import: "time"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"deleted_at", "id", "name", "price", "sku", "updated_at"},
		SoftDelete: &gen.SoftDeleteContext{
			Column:    "deleted_at",
			FieldName: "DeletedAt",
			Strategy:  "timestamp",
		},
		ExcludeDeleted: true,
		Operations: gen.ResolvedOperations{
			Get:        true,
			GetMany:    true,
			Count:      true,
			Paginate:   true,
			Connection: true,
		},
	}
}

// testPaginationContext_noSoftDelete returns a context without soft delete.
func testPaginationContext_noSoftDelete() gen.TableContext {
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
		PageSize:          50,
		CursorKeys:        []string{"id"},
		Imports: []string{
			"context",
			"encoding/base64",
			"encoding/json",
			"fmt",
			"slices",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "int64", DBTag: "id", JSONTag: "id", PrimaryKey: true, FKConvert: gotype.FKStringSprint},
		},
		AllColumnNames: []string{"id", "name"},
		Operations: gen.ResolvedOperations{
			Get:        true,
			GetMany:    true,
			Count:      true,
			Paginate:   true,
			Connection: true,
		},
	}
}

// testPaginationContext_multiCursorKeys returns a context with composite cursor keys.
func testPaginationContext_multiCursorKeys() gen.TableContext {
	return gen.TableContext{
		StructName:        "Event",
		TableName:         "events",
		TableNameConstant: "TableEvents",
		Schema:            "public",
		Package:           "db",
		VarName:           "e",
		Dialect:           "postgres",
		Driver:            "pgx",
		PKStrategy:        config.PKStrategyDB,
		PageSize:          25,
		CursorKeys:        []string{"created_at", "id"},
		Imports: []string{
			"context",
			"encoding/base64",
			"encoding/json",
			"fmt",
			"slices",
			"github.com/gofrs/uuid/v5",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"time",
		},
		Columns: []gen.ColumnContext{
			{Name: "created_at", FieldName: "CreatedAt", GoType: "time.Time", DBTag: "created_at", JSONTag: "created_at", Import: "time"},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/gofrs/uuid/v5", FKConvert: gotype.FKStringStringer},
		},
		AllColumnNames: []string{"created_at", "id", "name"},
		Operations: gen.ResolvedOperations{
			Get:        true,
			GetMany:    true,
			Count:      true,
			Paginate:   true,
			Connection: true,
		},
	}
}

// --- Paginate template tests ---

func TestPaginationTableTemplate_paginateWithLimitAndOffset(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Paginate(ctx context.Context, input PaginateInput[ProductFilter], opts ...func(*CallOptions[ProductFieldOptions])) (*PaginateResult[Product], error)",
		"totalCount, err := c.Count(ctx, input.Filter, internalOpts)",
		`fmt.Errorf("paginate products count: %w", err)`,
		"c.GetMany(ctx, &GetProductsInput{",
		"Filter: input.Filter,",
		"Sorts:  input.Sort,",
		"Limit:  &input.Limit,",
		"Offset: &input.Offset,",
		`fmt.Errorf("paginate products: %w", err)`,
		"return &PaginateResult[Product]{",
		"Items:      products,",
		"TotalCount: totalCount,",
		"Offset:     input.Offset,",
		"Limit:      input.Limit,",
		"HasMore:    int64(input.Offset+input.Limit) < totalCount,",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// PaginateResult-specific fields and arithmetic must not appear.
	// Note: HasNextPage / HasPreviousPage still live on Connection's PageInfo,
	// so we anchor on the dropped Paginate-result fields and the dropped local
	// arithmetic by name (with trailing punctuation that disambiguates from
	// the Connection block).
	droppedPatterns := []string{
		"TotalPages:",
		"PageSize:",
		"CurrentPage:",
		"currentPage := ",
		"totalPages := ",
	}
	for _, want := range droppedPatterns {
		if strings.Contains(output, want) {
			t.Errorf("output should not contain dropped %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestPaginationTableTemplate_paginateTotalCountViaSeparateQuery(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	// Count is called before GetMany
	countIdx := strings.Index(output, "c.Count(ctx, input.Filter")
	getManyIdx := strings.Index(output, "c.GetMany(ctx")
	if countIdx < 0 || getManyIdx < 0 {
		t.Fatalf("missing Count or GetMany call\n\nfull output:\n%s", output)
	}
	if countIdx > getManyIdx {
		t.Error("Count should be called before GetMany")
	}
}

func TestPaginationTableTemplate_paginateSoftDeleteDoc(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "Soft-deleted products are excluded by default. Set Filter.DeletedAt to override.") {
		t.Errorf("missing soft delete doc comment\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "A nil filter paginates all non-deleted products.") {
		t.Errorf("missing nil filter doc comment\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_paginateWithoutSoftDelete(t *testing.T) {
	ctx := testPaginationContext_noSoftDelete()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "func (c *categoryClient) Paginate(") {
		t.Errorf("missing Paginate method\n\nfull output:\n%s", output)
	}
	if strings.Contains(output, "Soft-deleted") {
		t.Errorf("should not mention soft delete when not configured\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "A nil filter paginates all categories.") {
		t.Errorf("missing nil filter doc comment\n\nfull output:\n%s", output)
	}
}

// --- Connection template tests ---

func TestPaginationTableTemplate_connectionForward(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func (c *productClient) Connection(ctx context.Context, input ConnectionInput[ProductFilter], opts ...func(*CallOptions[ProductFieldOptions])) (*Connection[Product], error)",
		// Mutual exclusivity validation
		"if input.First != nil && input.Last != nil {",
		"return nil, ErrInvalidCursor",
		// Total count before cursor conditions
		"totalCount, err := c.Count(ctx, input.Filter, internalOpts)",
		`fmt.Errorf("connection products count: %w", err)`,
		// Cursor decode
		"if input.After != nil {",
		"cursor, err := decodeCursor(*input.After)",
		`keysetConds = append(keysetConds, cursorKeyset(c.dialect, c.cursorKeys, cursor, ">"))`,
		// Direction
		"limit := c.pageSize",
		"ascending := true",
		"if input.First != nil {",
		"limit = *input.First",
		// Delegates to GetMany with limit+1
		"connLimit := limit + 1",
		"c.GetMany(ctx, &GetProductsInput{",
		"Filter:     input.Filter,",
		"Sorts:      cursorSorts(c.cursorKeys, ascending),",
		"Limit:      &connLimit,",
		"conditions: keysetConds,",
		// Trim
		"hasMore := len(products) > limit",
		"products = products[:limit]",
		// Reverse for backward
		"slices.Reverse(products)",
		// Build edges
		"edges := make([]Edge[Product], len(products))",
		"encodeProductCursor(c.cursorKeys, row)",
		"Edge[Product]{",
		// PageInfo (TotalCount lives on Connection, not PageInfo)
		"HasNextPage:     (ascending && hasMore) || (!ascending && input.Before != nil)",
		"HasPreviousPage: (!ascending && hasMore) || (ascending && input.After != nil)",
		"pageInfo.StartCursor = &edges[0].Cursor",
		"pageInfo.EndCursor = &edges[len(edges)-1].Cursor",
		"return &Connection[Product]{Edges: edges, PageInfo: pageInfo, TotalCount: totalCount}, nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Regression guard: TotalCount must not be assigned inside the PageInfo
	// literal (it lives on the top-level Connection).
	if strings.Contains(output, "TotalCount:      totalCount,") {
		t.Errorf("TotalCount must not appear in PageInfo literal\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_connectionBackward(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"if input.Before != nil {",
		"cursor, err := decodeCursor(*input.Before)",
		`keysetConds = append(keysetConds, cursorKeyset(c.dialect, c.cursorKeys, cursor, "<"))`,
		"} else if input.Last != nil {",
		"limit = *input.Last",
		"ascending = false",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestPaginationTableTemplate_connectionFirstPage(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	// When no cursor provided, the method should still work (no after/before decoding)
	// The default limit is c.pageSize, ascending = true
	if !strings.Contains(output, "limit := c.pageSize") {
		t.Errorf("should default to c.pageSize\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_connectionFirstAndLastError(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "if input.First != nil && input.Last != nil {") {
		t.Errorf("missing first+last validation\n\nfull output:\n%s", output)
	}
	// Should return ErrInvalidCursor
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "input.First != nil && input.Last != nil") {
			if i+1 < len(lines) && strings.Contains(lines[i+1], "ErrInvalidCursor") {
				return // found it
			}
		}
	}
	t.Errorf("first+last should return ErrInvalidCursor\n\nfull output:\n%s", output)
}

func TestPaginationTableTemplate_connectionHasNextHasPreviousFlags(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	// HasNextPage = (ascending && hasMore) || (!ascending && input.Before != nil)
	if !strings.Contains(output, "(ascending && hasMore) || (!ascending && input.Before != nil)") {
		t.Errorf("missing HasNextPage logic\n\nfull output:\n%s", output)
	}
	// HasPreviousPage = (!ascending && hasMore) || (ascending && input.After != nil)
	if !strings.Contains(output, "(!ascending && hasMore) || (ascending && input.After != nil)") {
		t.Errorf("missing HasPreviousPage logic\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_connectionResultReversal(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "if !ascending {") {
		t.Errorf("missing backward reversal check\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "slices.Reverse(products)") {
		t.Errorf("missing slices.Reverse call\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_connectionDelegatesToGetMany(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	// Connection delegates to GetMany — soft delete, column selection, O2O joins,
	// and relationship loading are all handled there, not in Connection itself.
	if !strings.Contains(output, "c.GetMany(ctx, &GetProductsInput{") {
		t.Errorf("Connection should delegate to GetMany\n\nfull output:\n%s", output)
	}
	// Connection should NOT directly reference excludeDeleted — that's GetMany's job
	if strings.Contains(output, "excludeDeleted") {
		t.Errorf("Connection should not reference excludeDeleted directly\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_connectionWithoutSoftDelete(t *testing.T) {
	ctx := testPaginationContext_noSoftDelete()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if !strings.Contains(output, "func (c *categoryClient) Connection(") {
		t.Errorf("missing Connection method\n\nfull output:\n%s", output)
	}
	// Delegates to GetMany
	if !strings.Contains(output, "c.GetMany(ctx, &GetCategoriesInput{") {
		t.Errorf("Connection should delegate to GetMany\n\nfull output:\n%s", output)
	}
}

func TestPaginationTableTemplate_encodeCursor(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func encodeProductCursor(keys []string, p *Product) (string, error)",
		"out := make(map[string]any, len(keys))",
		"for _, key := range keys {",
		"switch key {",
		`case "id":`,
		"out[\"id\"] = p.ID",
		`case "name":`,
		"out[\"name\"] = p.Name",
		`case "price":`,
		"out[\"price\"] = p.Price",
		"json.Marshal(out)",
		`fmt.Errorf("encode product cursor: %w", err)`,
		"base64.StdEncoding.EncodeToString(data)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestPaginationTableTemplate_encodeCursorMultiKeys(t *testing.T) {
	ctx := testPaginationContext_multiCursorKeys()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func encodeEventCursor(keys []string, e *Event) (string, error)",
		`case "created_at":`,
		"out[\"created_at\"] = e.CreatedAt",
		`case "id":`,
		"out[\"id\"] = e.ID",
		`case "name":`,
		"out[\"name\"] = e.Name",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// Regression: when StructName starts with "M" (VarName == "m"), the
// local cursor map inside encode<Table>Cursor must not be named "m" — that
// would shadow the *<Table> parameter and turn every `{{ $.VarName }}.<Field>`
// read into a map key access (compile error).
func TestPaginationTableTemplate_encodeCursor_doesNotShadowEntity_MStartingStruct(t *testing.T) {
	ctx := testPaginationContext_postgres()
	ctx.StructName = "MacroScenarioCounter"
	ctx.TableName = "macro_scenario_counters"
	ctx.TableNameConstant = "TableMacroScenarioCounters"
	ctx.VarName = "m"
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	wantPatterns := []string{
		"func encodeMacroScenarioCounterCursor(keys []string, m *MacroScenarioCounter) (string, error)",
		"out := make(map[string]any, len(keys))",
		"out[\"id\"] = m.ID",
		"json.Marshal(out)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Forbidden patterns: the shadow vector this fix closes.
	forbidden := []string{
		"m := make(map[string]any",
		`m["id"] = m.ID`,
	}
	for _, bad := range forbidden {
		if strings.Contains(output, bad) {
			t.Errorf("output should not contain shadow pattern %q\n\nfull output:\n%s", bad, output)
		}
	}

	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "macro_scenario_counters_pagination_gen.go"); err != nil {
		t.Fatalf("FormatOnly failed — encode<Table>Cursor has a parse error: %v", err)
	}
}

func TestPaginationTableTemplate_paginateDisabled(t *testing.T) {
	ctx := testPaginationContext_postgres()
	ctx.Operations.Paginate = false
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *productClient) Paginate(") {
		t.Error("Paginate method should not be generated when disabled")
	}
}

func TestPaginationTableTemplate_connectionDisabled(t *testing.T) {
	ctx := testPaginationContext_postgres()
	ctx.Operations.Connection = false
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *productClient) Connection(") {
		t.Error("Connection method should not be generated when disabled")
	}
	if strings.Contains(output, "encodeProductCursor") {
		t.Error("encodeProductCursor should not be generated when Connection disabled")
	}
}

// --- Golden file tests ---

func TestPaginationTableTemplate_goldenFile(t *testing.T) {
	ctx := testPaginationContext_postgres()
	output := executePaginationTableTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_pagination_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v\n\nraw output:\n%s", err, output)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "pagination_products_gen.go")

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
		t.Errorf("pagination_products_gen.go mismatch (-want +got):\n%s", diff)
	}
}
