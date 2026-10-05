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

// 27.5 — UpsertMany (Ticket Db, PRD §9.2, §9.5, §25.1).
//
// The three measured decisions the ticket had to make (companion §6.1–§6.3) are
// each pinned here at the template level, and again at runtime in the example
// modules' upsert_many_test.go:
//
//   - §6.1 dedupe: the emitted method collapses inputs resolving to one conflict
//     target before it builds a statement, last occurrence wins. PostgreSQL
//     alone rejects an in-statement duplicate on the DO UPDATE shape.
//   - §6.2 return contract: entities come from the terminal re-fetch, never from
//     RETURNING, which drops every row that took the DO NOTHING branch. No
//     UpsertMany statement carries a RETURNING clause on any dialect.
//   - §6.3 MySQL db-generated PK: nothing reads LastInsertId and nothing
//     fabricates ids as firstID+i. A table whose key the input does not carry
//     reads the written rows back by the conflict target instead.

func loadUpsertManyTemplate(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl, err := template.New("upsert_many.go.tmpl").
		Funcs(gen.FuncMap(dialect)).
		ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "table", "upsert_many.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing upsert_many template: %v", err)
	}
	return tmpl
}

func executeUpsertManyTemplate(t *testing.T, ctx gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadUpsertManyTemplate(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/upsert-many", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

// --- Test contexts ---
//
// Each reuses an upsert_test.go context and turns on UpsertMany, so the two
// templates are always exercised against the same table shapes. ScanShapes is
// added here because only upsert_many reads it — resolveUpsertManyRows scans
// the read-back rows through scan<Plural> and keys them with <table>ColumnValue.

func testUpsertManyContext_dbStrategyUUID_postgres() gen.TableContext {
	ctx := testUpsertContext_dbStrategyUUID_postgres()
	ctx.Operations.UpsertMany = true
	ctx.ScanShapes = []gen.ScanShapeContext{
		{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&p.ID"},
		{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&p.Name"},
		{ColumnName: "price", FieldName: "Price", Shape: "direct", ScanExpr: "&p.Price"},
		{ColumnName: "sku", FieldName: "SKU", Shape: "direct", ScanExpr: "&p.SKU"},
	}
	return ctx
}

func testUpsertManyContext_dbStrategyAutoIncrement_mysql() gen.TableContext {
	ctx := testUpsertContext_dbStrategyAutoIncrement_mysql()
	ctx.Operations.UpsertMany = true
	ctx.ScanShapes = []gen.ScanShapeContext{
		{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&e.ID"},
		{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&e.Name"},
		{ColumnName: "payload", FieldName: "Payload", Shape: "direct", ScanExpr: "&e.Payload"},
	}
	return ctx
}

func testUpsertManyContext_callerStrategy_postgres() gen.TableContext {
	ctx := testUpsertContext_callerStrategy_postgres()
	ctx.Operations.UpsertMany = true
	return ctx
}

func testUpsertManyContext_compositePK() gen.TableContext {
	ctx := testUpsertContext_compositePK()
	ctx.Operations.UpsertMany = true
	return ctx
}

// A composite-PK table carrying a UNIQUE that is *not* the key. This is the one
// shape where resolveUpsertManyRows has to resolve a composite key from a
// read-back, and nothing else in the suite reaches it: every composite fixture
// and every composite table in the 12 example trees declares only ConflictPK.
// order_items gains a UNIQUE on (order_id, line_no) — a second line for the
// same product is a real schema, and line_no does not name the key.
func testUpsertManyContext_compositePK_nonCoveringTarget() gen.TableContext {
	ctx := testUpsertManyContext_compositePK()
	ctx.CreateInputFields = append(ctx.CreateInputFields, gen.InputFieldContext{
		FieldName: "LineNo", GoType: "int32", ColumnName: "line_no", Required: true, JSONTag: "line_no",
	})
	ctx.ConflictTargets = append(ctx.ConflictTargets, gen.ConflictTargetContext{
		ConstantName: "OrderItemConflictOrderIDLineNo",
		Columns:      []string{"order_id", "line_no"},
		CoversPK:     false,
		Comment:      "UNIQUE (order_id, line_no)",
	})
	ctx.ScanShapes = []gen.ScanShapeContext{
		{ColumnName: "order_id", FieldName: "OrderID", Shape: "direct", ScanExpr: "&o.OrderID"},
		{ColumnName: "product_id", FieldName: "ProductID", Shape: "direct", ScanExpr: "&o.ProductID"},
		{ColumnName: "line_no", FieldName: "LineNo", Shape: "direct", ScanExpr: "&o.LineNo"},
		{ColumnName: "unit_price", FieldName: "UnitPrice", Shape: "direct", ScanExpr: "&o.UnitPrice"},
	}
	return ctx
}

func testUpsertManyContext_sqlite() gen.TableContext {
	ctx := testUpsertManyContext_dbStrategyUUID_postgres()
	ctx.Dialect = "sqlite"
	ctx.Driver = "stdlib"
	return ctx
}

// --- Emission gating ---

func TestUpsertManyTemplate_disabled(t *testing.T) {
	ctx := testUpsertManyContext_compositePK()
	ctx.Operations.UpsertMany = false
	output := executeUpsertManyTemplate(t, ctx, sql.NewPostgresDialect())

	if strings.Contains(output, "func (c *orderItemClient) UpsertMany") {
		t.Errorf("UpsertMany emitted with operations.upsert_many disabled:\n%s", output)
	}
}

// The conflict-target enum lives in table/upsert but is shared surface: a table
// with upsert off and upsert_many on still needs the type its own signature
// names, or the package does not compile (PRD §9.2 — "drawn from the same
// generated enum").
func TestUpsertTemplate_conflictTargetEmittedForUpsertManyAlone(t *testing.T) {
	ctx := testUpsertManyContext_dbStrategyUUID_postgres()
	ctx.Operations.Upsert = false
	output := executeUpsertTemplate(t, ctx, sql.NewPostgresDialect())

	for _, want := range []string{
		"type ProductConflictTarget int",
		"var productConflictColumns = map[ProductConflictTarget][]string{",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("upsert:false + upsert_many:true output missing %q\n\nfull output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "func (c *productClient) Upsert(") {
		t.Error("single-row Upsert emitted with operations.upsert disabled")
	}
}

// --- The conflict clause, per dialect ---

func TestUpsertManyTemplate_conflictClauseFields(t *testing.T) {
	tests := []struct {
		name    string
		ctx     gen.TableContext
		dialect sql.Dialect
	}{
		{"postgres", testUpsertManyContext_dbStrategyUUID_postgres(), sql.NewPostgresDialect()},
		{"mysql", testUpsertManyContext_dbStrategyAutoIncrement_mysql(), sql.NewMySQLDialect()},
		{"sqlite", testUpsertManyContext_sqlite(), sql.NewSQLiteDialect()},
		{"composite-pk", testUpsertManyContext_compositePK(), sql.NewPostgresDialect()},
		{"caller-strategy", testUpsertManyContext_callerStrategy_postgres(), sql.NewPostgresDialect()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpsertManyTemplate(t, tt.ctx, tt.dialect)

			// The batched conflict clause is what the statement is built from.
			for _, want := range []string{
				"sql.BuildMultiInsert(c.dialect, c.table, sql.MultiInsertOptions{",
				"UpsertConflictKeys:  conflictColumns,",
				"UpsertUpdateColumns: updateColumns,",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
				}
			}

			// §6.3: UpsertResolvePKColumn republishes at most one row's id into
			// MySQL's OK packet, which after a batch names neither the caller's
			// row nor the first inserted one. Nothing here reads LastInsertId,
			// so requesting it would only mislead.
			// Matched in their code shapes, not their prose ones: the doc
			// comment names all three deliberately, to say why they are absent.
			for _, unwanted := range []string{
				"UpsertResolvePKColumn:",
				".LastInsertId()",
				"firstID + int64(",
			} {
				if strings.Contains(output, unwanted) {
					t.Errorf("output contains %q — UpsertMany must not resolve keys from the statement (PRD §9.7)\n\nfull output:\n%s", unwanted, output)
				}
			}

			// §6.2: RETURNING yields only the rows actually inserted, so no
			// UpsertMany statement may carry one.
			if strings.Contains(output, "ReturningColumns:") {
				t.Errorf("output sets ReturningColumns — UpsertMany sources entities from the terminal re-fetch (PRD §9.5)\n\nfull output:\n%s", output)
			}
		})
	}
}

// --- §6.1: the dedupe ---

func TestUpsertManyTemplate_dedupesLastWins(t *testing.T) {
	tests := []struct {
		name    string
		ctx     gen.TableContext
		dialect sql.Dialect
	}{
		{"postgres", testUpsertManyContext_dbStrategyUUID_postgres(), sql.NewPostgresDialect()},
		{"mysql", testUpsertManyContext_dbStrategyAutoIncrement_mysql(), sql.NewMySQLDialect()},
		{"sqlite", testUpsertManyContext_sqlite(), sql.NewSQLiteDialect()},
		{"composite-pk", testUpsertManyContext_compositePK(), sql.NewPostgresDialect()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpsertManyTemplate(t, tt.ctx, tt.dialect)

			for _, want := range []string{
				"conflictPositions, haveConflictKey := upsertConflictPositions(columns, conflictColumns)",
				"key, keyed := upsertDedupeKey(row, conflictPositions)",
				// Last occurrence wins: the earlier row's slot is overwritten
				// rather than the later row being dropped.
				"if j, ok := seen[key]; ok {",
				"dedupedRows[j] = row",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
				}
			}

			// The dedupe spans the whole call, not a sub-batch: it must run
			// before the batching loop, or PRD §25.1's "after the dedupe" is
			// not what the statement count reflects.
			dedupeAt := strings.Index(output, "upsertDedupeKey(row, conflictPositions)")
			batchAt := strings.Index(output, "for start := 0; start < len(valueRows); start += c.batchSize {")
			if dedupeAt < 0 || batchAt < 0 {
				t.Fatalf("could not locate dedupe (%d) or batch loop (%d)\n\nfull output:\n%s", dedupeAt, batchAt, output)
			}
			if dedupeAt > batchAt {
				t.Errorf("dedupe runs inside the batch loop; PRD §25.1 counts statements after a whole-call dedupe")
			}
		})
	}
}

// --- PK sourcing ---

// A caller-known key (composite, caller or app strategy) is taken from the
// inputs, which is what keeps a pure link table at one statement per
// sub-batch.
// A key the input carries is only the *written* row's key when the conflict
// target covers every primary-key column. On any other target the conflicting
// row keeps its own key, so the written rows are read back instead — the bug
// this test originally pinned the wrong way round. Both halves are
// asserted here: the input-sourced fast path where the target covers the key,
// and the read-back where a declared target does not.
func TestUpsertManyTemplate_pkFromInput(t *testing.T) {
	tests := []struct {
		name        string
		ctx         gen.TableContext
		wantPKExpr  string
		wantAffects string
		// wantReadBack is true when the table declares at least one conflict
		// target that does not cover the primary key, which is a runtime
		// choice the emitted method has to be able to make.
		wantReadBack bool
	}{
		{
			name: "composite-pk-every-target-covers-key",
			ctx:  testUpsertManyContext_compositePK(),
			// Matched before gofmt normalizes the composite literal's trailing
			// comma, so the raw template output is what is asserted.
			wantPKExpr:  "rowPKs = append(rowPKs, OrderItemPK{OrderID: input.OrderID, ProductID: input.ProductID, }",
			wantAffects: "m.AffectedPKs = toAnySlice(rowPKs)",
			// order_items declares only ConflictPK, so the answer is static and
			// the method pays no runtime branch and no second statement.
			wantReadBack: false,
		},
		{
			name: "caller-strategy-with-a-non-covering-target",
			ctx:  testUpsertManyContext_callerStrategy_postgres(),
			// tenants declares TenantConflictSlug alongside TenantConflictPK.
			// The input-sourced collection still happens — it is what the
			// covering target uses — but it can no longer be the only path.
			wantPKExpr:   "rowIDs = append(rowIDs, input.ID)",
			wantAffects:  "m.AffectedPKs = toAnySlice(rowIDs)",
			wantReadBack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpsertManyTemplate(t, tt.ctx, sql.NewPostgresDialect())

			if !strings.Contains(output, tt.wantPKExpr) {
				t.Errorf("output missing %q\n\nfull output:\n%s", tt.wantPKExpr, output)
			}
			if !strings.Contains(output, tt.wantAffects) {
				t.Errorf("output missing %q", tt.wantAffects)
			}

			if got := strings.Contains(output, "resolveUpsertManyRows"); got != tt.wantReadBack {
				if tt.wantReadBack {
					t.Errorf("a conflict target that does not cover the primary key must read the written rows back; the conflicting row keeps its own key (PRD §9.5)\n\nfull output:\n%s", output)
				} else {
					t.Errorf("every declared conflict target covers the primary key, so the key is caller-known and must not cost a read-back statement\n\nfull output:\n%s", output)
				}
			}

			// The choice is made per call, off the generated coverage table —
			// never off the PK strategy alone, which is what made the
			// input-sourced key wrong on a non-covering target.
			if tt.wantReadBack {
				for _, want := range []string{
					"ConflictCoversPK = map[",
					"pkFromInput := ",
					"if !pkFromInput {",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("output missing %q — the read-back must be selected at call time\n\nfull output:\n%s", want, output)
					}
				}
			} else if strings.Contains(output, "pkFromInput") {
				t.Errorf("a table whose targets all cover the key must not emit a runtime branch\n\nfull output:\n%s", output)
			}
		})
	}
}

// A composite key resolved from a read-back is a distinct code path: the helper
// has to return []OrderItemPK rather than a bare key, select every PK column
// rather than only the first, and rebuild the struct from the scanned row.
// resolveUpsertManyRows was generalized for exactly this and nothing else
// exercised it — every composite fixture and every composite example table
// declares only ConflictPK, so the generalization shipped untested.
func TestUpsertManyTemplate_compositePKReadBack(t *testing.T) {
	ctx := testUpsertManyContext_compositePK_nonCoveringTarget()
	output := executeUpsertManyTemplate(t, ctx, sql.NewPostgresDialect())

	for _, want := range []string{
		// The runtime branch exists at all.
		"orderItemConflictCoversPK = map[OrderItemConflictTarget]bool{",
		"OrderItemConflictOrderIDLineNo: false,",
		"OrderItemConflictPK: true,",
		"pkFromInput := orderItemConflictCoversPK[target]",
		// The helper is composite-shaped end to end.
		"positions []int, rows [][]any) ([]OrderItemPK, error)",
		"pk OrderItemPK",
		"pks := make([]OrderItemPK, len(rows))",
		"byKey[key] = resolvedRow{pk: OrderItemPK{OrderID: entity.OrderID, ProductID: entity.ProductID, }}",
		// Every PK column is read back, not just the first — a lookup that
		// selected only order_id could not rebuild the struct.
		`for _, required := range []string{"order_id", "product_id"} {`,
		// The resolved keys replace the input-sourced ones.
		"var resolvedPKs []OrderItemPK",
		"resolvedPKs = append(resolvedPKs, batchKeys...)",
		"rowPKs = resolvedPKs",
		"m.AffectedPKs = toAnySlice(rowPKs)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
		}
	}

	// The input-sourced collection still exists — it is what the covering
	// target uses — so the two must not be confused for one another.
	if !strings.Contains(output, "rowPKs = append(rowPKs, OrderItemPK{OrderID: input.OrderID, ProductID: input.ProductID, }") {
		t.Errorf("the covering-target path must still collect keys from the inputs\n\nfull output:\n%s", output)
	}
}

// The coverage table is what the runtime branch reads, so a target that does
// not cover the key must report false — a table that answered true everywhere
// would take the input-sourced path on every target and drop the read-back.
func TestUpsertManyTemplate_conflictCoversPKTable(t *testing.T) {
	output := executeUpsertManyTemplate(t, testUpsertManyContext_callerStrategy_postgres(), sql.NewPostgresDialect())

	for _, want := range []string{
		"TenantConflictPK: true,",
		"TenantConflictSlug: false,",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("coverage table missing %q\n\nfull output:\n%s", want, output)
		}
	}
}

// §6.3 — a database-generated key cannot come from the statement on any
// dialect, so the sub-batch is read back by the conflict target it used.
func TestUpsertManyTemplate_dbPKReadsBackByConflictTarget(t *testing.T) {
	tests := []struct {
		name    string
		ctx     gen.TableContext
		dialect sql.Dialect
		client  string
	}{
		{"postgres-uuid-default", testUpsertManyContext_dbStrategyUUID_postgres(), sql.NewPostgresDialect(), "productClient"},
		{"mysql-auto-increment", testUpsertManyContext_dbStrategyAutoIncrement_mysql(), sql.NewMySQLDialect(), "eventClient"},
		{"sqlite-uuid-default", testUpsertManyContext_sqlite(), sql.NewSQLiteDialect(), "productClient"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpsertManyTemplate(t, tt.ctx, tt.dialect)

			for _, want := range []string{
				"func (c *" + tt.client + ") resolveUpsertManyRows(",
				"batchIDs, err := c.resolveUpsertManyRows(ctx, conn, conflictColumns, conflictPositions, batch)",
				"sql.BuildCompositePKBatchCondition(c.dialect, conflictColumns, keySets)",
				// The guard runs before any statement: a target the input
				// cannot supply leaves the written rows unnameable.
				"conflict target names a column the input does not supply",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
				}
			}

			// The read-back must precede the AffectedPKs assignment, or the
			// slice it publishes is the empty one.
			resolveAt := strings.Index(output, "batchIDs, err := c.resolveUpsertManyRows(")
			affectedAt := strings.Index(output, "m.AffectedPKs = toAnySlice(rowIDs)")
			if resolveAt < 0 || affectedAt < 0 || resolveAt > affectedAt {
				t.Errorf("read-back (%d) must precede AffectedPKs (%d)", resolveAt, affectedAt)
			}
		})
	}
}

// The read-back's lookup column list is copied before it is sorted: it starts
// life aliasing the package-level <table>ConflictColumns map entry, and sorting
// that in place would corrupt every later call's conflict target.
func TestUpsertManyTemplate_lookupColumnsCopiedBeforeSort(t *testing.T) {
	output := executeUpsertManyTemplate(t, testUpsertManyContext_dbStrategyUUID_postgres(), sql.NewPostgresDialect())

	for _, want := range []string{
		"lookupColumns := make([]string, 0, len(conflictColumns)+2)",
		"lookupColumns = append(lookupColumns, conflictColumns...)",
		"slices.Sort(lookupColumns)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\n\nfull output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "slices.Sort(conflictColumns)") {
		t.Error("conflictColumns sorted in place — it aliases the package-level conflict-target map")
	}
}

// PRD §9.4c: a nil element is the same caller error a nil Upsert argument is.
func TestUpsertManyTemplate_nilInputGuard(t *testing.T) {
	output := executeUpsertManyTemplate(t, testUpsertManyContext_compositePK(), sql.NewPostgresDialect())

	guardAt := strings.Index(output, "return nil, ErrNilInput")
	executorAt := strings.Index(output, "c.executeMutation(")
	if guardAt < 0 || executorAt < 0 {
		t.Fatalf("could not locate nil guard (%d) or executor (%d)", guardAt, executorAt)
	}
	if guardAt > executorAt {
		t.Error("nil element guard must run before the executor, so nil fires no hook and publishes no event")
	}
}

// An empty call writes nothing — and must read nothing. The terminal GetMany
// filters on the written keys, and a key filter built from an empty set
// contributes no condition at all, so an unguarded empty call reads the whole
// table back.
func TestUpsertManyTemplate_emptyInputShortCircuits(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  gen.TableContext
	}{
		{"composite-pk", testUpsertManyContext_compositePK()},
		{"db-strategy", testUpsertManyContext_dbStrategyUUID_postgres()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := executeUpsertManyTemplate(t, tt.ctx, sql.NewPostgresDialect())
			if !strings.Contains(output, "if len(inputs) == 0 {") {
				t.Errorf("output missing the empty-input short circuit\n\nfull output:\n%s", output)
			}
		})
	}
}

// The op is its own MutationOp: it is a direct write path, and every consumer
// switch keyed on MutationOp has a permissive default, so reusing OpUpsert
// would hand a whole input slice to a single-row arm (PRD §21.2, §28, §32.3).
func TestUpsertManyTemplate_usesOwnMutationOp(t *testing.T) {
	output := executeUpsertManyTemplate(t, testUpsertManyContext_compositePK(), sql.NewPostgresDialect())

	if !strings.Contains(output, "Op: hook.OpUpsertMany,") {
		t.Errorf("output does not use hook.OpUpsertMany\n\nfull output:\n%s", output)
	}
	// The published input is the deduped slice so events stay index-aligned
	// with AffectedPKs.
	if !strings.Contains(output, "m.Input = rowInputs") {
		t.Error("output does not republish the deduped input slice")
	}
}

// --- Golden files ---

func TestUpsertManyTemplate_goldenFile_compositePK(t *testing.T) {
	ctx := testUpsertManyContext_compositePK()
	output := executeUpsertManyTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "order_items_upsert_many_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}
	compareUpsertManyGolden(t, "upsert_many_order_items_gen.go", formatted)
}

func TestUpsertManyTemplate_goldenFile_dbStrategy(t *testing.T) {
	ctx := testUpsertManyContext_dbStrategyUUID_postgres()
	output := executeUpsertManyTemplate(t, ctx, sql.NewPostgresDialect())

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "products_upsert_many_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}
	compareUpsertManyGolden(t, "upsert_many_products_gen.go", formatted)
}

func compareUpsertManyGolden(t *testing.T, name string, formatted []byte) {
	t.Helper()
	goldenPath := filepath.Join("testdata", "golden", name)

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
	if diff := cmp.Diff(string(want), string(formatted)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
	}
}
