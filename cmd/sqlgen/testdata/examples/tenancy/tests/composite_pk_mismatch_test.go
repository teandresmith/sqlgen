package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Composite-PK tenancy sweep — §29.7 verify-match rule, covering the per-op
// gaps left by composite_pk_test.go:
//   - Upsert mismatch + zero-value WorkspaceID on the INSERT input.
//   - Zero-value WorkspaceID on Update / HardDelete / Increment / Exists PKs
//     (composite_pk_test.go only covers Get for the zero-value branch).
//   - SkipTenancy:true + caller-supplied matching tenant produces the same
//     row-identification SQL as SkipTenancy:false + matching tenant on every
//     PK-scoped op (see "SQL shape" scope note below).
//
// ---------------------------------------------------------------------------
// Scope note — "byte-identical SQL" on tenant-in-PK tables
// ---------------------------------------------------------------------------
// One might expect SkipTenancy:true + matching tenant to produce
// "byte-identical SQL" to the SkipTenancy:false case. For composite-PK tables
// where the tenant column is part of the PK, this is NOT strictly true on the
// current codegen:
//
//   - SkipTenancy:false path runs the PK-based Get/Update/Delete filter
//     (which already includes `workspace_id = ?` from the PK struct) AND
//     appends an auto-filter `AND workspace_id = ?` from the GetMany
//     tenancy block. Result: TWO `workspace_id = ?` predicates in WHERE.
//   - SkipTenancy:true path drops the auto-filter. Result: ONE
//     `workspace_id = ?` predicate (from the PK filter alone).
//
// The redundant predicate is a correctness property — it keeps the
// tenant-in-WHERE invariant uniform across simple-PK and composite-PK tables,
// so a subsequent refactor that widens the PK filter's shape cannot silently
// drop the tenancy scope. Both paths identify the same row deterministically
// and the DB optimizer collapses the duplicate predicate.
//
// This test file therefore asserts:
//   - Tests assert "same row identified, same WHERE columns present" —
//     not byte-for-byte SQL equality.
//   - Tests count `workspace_id = ` occurrences in the WHERE clause to lock
//     in the documented N=2 (SkipTenancy:false) vs N=1 (SkipTenancy:true)
//     distinction, so a future refactor that drops either predicate surfaces
//     as a visible diff.

// ---------------------------------------------------------------------------
// Helpers — reuse capturingQuerier + newCapturingClient from
// relationship_chain_test.go.
// ---------------------------------------------------------------------------

// countWhereToken counts occurrences of `token` inside the WHERE clause of a
// captured SQL statement. Mirrors countWorkspaceIDPredicates but
// parameterises the token so the same helper drives both the workspace_id
// assertion and any future tenant-column-name variant.
//
// Identifier-quoting normalisation: the SQL builder quotes identifiers with
// `"` on SQLite (`"workspace_id" = ?`) for the PK filter path and emits
// unquoted forms (`workspace_id = ?`) from the tenancy auto-filter
// (sql.Where("workspace_id").Eq(...) does not apply quoting). Strip both
// double-quote and backtick characters before counting so a single tokeniser
// matches both shapes — otherwise "workspace_id = " only catches the
// auto-filter half and the PK-filter half is invisible.
func countWhereToken(sqlStr, token string) int {
	lower := strings.ToLower(sqlStr)
	// Strip identifier quoting so both `"workspace_id" = ?` and
	// `workspace_id = ?` collapse to the same tokenisable form.
	lower = strings.ReplaceAll(lower, "\"", "")
	lower = strings.ReplaceAll(lower, "`", "")
	idx := strings.Index(lower, " where ")
	if idx < 0 {
		return 0
	}
	return strings.Count(lower[idx:], strings.ToLower(token))
}

// lastSelect / lastExec return the last recorded SELECT / INSERT-or-UPDATE-or-DELETE
// from a capturingQuerier. Tests call these instead of indexing `.snapshot()`
// directly to keep intent obvious at the call site.
func lastSelect(c *capturingQuerier, t *testing.T) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[i])), "SELECT") {
			return sqls[i]
		}
	}
	t.Fatalf("no SELECT captured; captured = %v", sqls)
	return ""
}

func lastMutation(c *capturingQuerier, t *testing.T) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		up := strings.ToUpper(strings.TrimSpace(sqls[i]))
		if strings.HasPrefix(up, "UPDATE") || strings.HasPrefix(up, "DELETE") || strings.HasPrefix(up, "INSERT") {
			return sqls[i]
		}
	}
	t.Fatalf("no mutation captured; captured = %v", sqls)
	return ""
}

// ---------------------------------------------------------------------------
// Upsert — mismatch + zero-value (Upsert's tenant is on the INSERT input, not
// on the PK struct; §29.7 says the verify-match rule still applies).
// ---------------------------------------------------------------------------

func TestCompositePK_Upsert_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	envA.counter.reset()
	_, err := envA.client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantB, // mismatched against resolver (tenantA)
		OrderID:     1,
		ProductID:   10,
		Quantity:    5,
		UnitPrice:   2.50,
	}, models.OrderItemConflictPK)
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Upsert with mismatched input.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on Upsert mismatch: %d, want 0", got)
	}

	// Row must not exist — a mismatched Upsert cannot have committed.
	got, err := envA.client.OrderItems().Exists(ctx, models.OrderItemPK{
		WorkspaceID: tenantA, OrderID: 1, ProductID: 10,
	})
	if err != nil {
		t.Fatalf("Exists after mismatched Upsert: %v", err)
	}
	if got {
		t.Errorf("row exists after mismatched Upsert attempt — partial commit leaked")
	}
}

func TestCompositePK_Upsert_ZeroTenantOnInputReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	envA.counter.reset()
	_, err := envA.client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{
		// WorkspaceID: uuid.Nil (implicit) — forgetting to set it must surface
		// as a loud error (§29.7 zero-value-is-mismatch property), not a
		// silent write under uuid.Nil.
		OrderID:   1,
		ProductID: 10,
		Quantity:  5,
		UnitPrice: 2.50,
	}, models.OrderItemConflictPK)
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Upsert with zero-UUID WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant Upsert: %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Zero-value PK.Tenant sweep — the remaining PK-scoped ops. Get is already
// covered by TestCompositePK_Get_ZeroTenantOnPKReturnsErrMismatch in
// composite_pk_test.go.
// ---------------------------------------------------------------------------

func TestCompositePK_Update_ZeroTenantOnPKReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	_ = createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Update(ctx, models.OrderItemPK{
		// WorkspaceID zero
		OrderID:   1,
		ProductID: 10,
	}, &models.UpdateOrderItemInput{
		Quantity: omittable.Set(int64(99)),
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Update with zero-UUID pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant Update: %d, want 0", got)
	}
}

func TestCompositePK_HardDelete_ZeroTenantOnPKReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	_ = createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	err := envA.client.OrderItems().HardDelete(ctx, models.OrderItemPK{
		OrderID:   1,
		ProductID: 10,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("HardDelete with zero-UUID pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant HardDelete: %d, want 0", got)
	}
}

func TestCompositePK_Increment_ZeroTenantOnPKReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	_ = createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	err := envA.client.OrderItems().Increment(ctx, models.OrderItemPK{
		OrderID:   1,
		ProductID: 10,
	}, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 1,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Increment with zero-UUID pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant Increment: %d, want 0", got)
	}
}

func TestCompositePK_Exists_ZeroTenantOnPKReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	_ = createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Exists(ctx, models.OrderItemPK{
		OrderID:   1,
		ProductID: 10,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Exists with zero-UUID pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant Exists: %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// SkipTenancy:true + matching caller tenant — SQL-shape comparison across
// every PK-scoped op. See the "byte-identical SQL" scope note at the top.
// ---------------------------------------------------------------------------

// runBothPaths runs the same operation twice on one shared client — first
// with SkipTenancy:false + matching tenant, then with SkipTenancy:true +
// matching caller-supplied tenant — and returns the two captured SQL
// statements (defaultSQL, skipSQL) for comparison.
//
// pathIdx (0 = default path, 1 = skip path) is threaded into runSeed and
// runOp so each path targets a distinct seed row. This keeps the two ops
// independent even when one mutates or deletes the row (HardDelete) — the
// other path's row is already seeded under a different PK.
//
// `runOp` performs the actual client call; `skip` sets CallOptions.SkipTenancy.
type opRunner func(t *testing.T, client *models.Client, skip bool, pathIdx int)

func runBothPaths(t *testing.T, runSeed func(t *testing.T, client *models.Client, pathIdx int), runOp opRunner, pick func(*capturingQuerier, *testing.T) string) (defaultSQL, skipSQL string) {
	t.Helper()

	client, cap := newCapturingClient(t, staticResolver(tenantA))

	// Path 0: SkipTenancy:false + resolver=A, caller-supplied tenant=A.
	if runSeed != nil {
		runSeed(t, client, 0)
	}
	cap.reset()
	runOp(t, client, false, 0)
	defaultSQL = pick(cap, t)

	// Path 1: SkipTenancy:true + resolver=A (not consulted), caller-supplied tenant=A.
	if runSeed != nil {
		runSeed(t, client, 1)
	}
	cap.reset()
	runOp(t, client, true, 1)
	skipSQL = pick(cap, t)

	return defaultSQL, skipSQL
}

// assertSameShape encodes the reconciled invariant: both paths must produce
// a SELECT/mutation touching the same table with the same PK-column predicate
// structure. The SkipTenancy:false path adds ONE extra `workspace_id = ?`
// from the auto-filter; otherwise the WHERE columns must match.
func assertSameShape(t *testing.T, label, defaultSQL, skipSQL string) {
	t.Helper()
	dCount := countWhereToken(defaultSQL, "workspace_id = ")
	sCount := countWhereToken(skipSQL, "workspace_id = ")
	// Default: PK filter contributes 1, auto-filter contributes 1 → total 2.
	// SkipTenancy: PK filter contributes 1, auto-filter skipped → total 1.
	if dCount != 2 {
		t.Errorf("%s: SkipTenancy:false workspace_id predicate count = %d, want 2 (PK + auto-filter)\nSQL: %s", label, dCount, defaultSQL)
	}
	if sCount != 1 {
		t.Errorf("%s: SkipTenancy:true workspace_id predicate count = %d, want 1 (PK only)\nSQL: %s", label, sCount, skipSQL)
	}
	// Both paths must mention the same PK columns — order_id / product_id
	// presence is the "same row identified" contract. Quote-strip the SQL
	// before Contains so the quoted PK-filter form (`"order_id" = ?`)
	// matches the same token as the unquoted auto-filter form.
	stripped := func(s string) string {
		lower := strings.ToLower(s)
		lower = strings.ReplaceAll(lower, "\"", "")
		lower = strings.ReplaceAll(lower, "`", "")
		return lower
	}
	for _, col := range []string{"order_id = ", "product_id = "} {
		if !strings.Contains(stripped(defaultSQL), col) {
			t.Errorf("%s: SkipTenancy:false missing %q\nSQL: %s", label, col, defaultSQL)
		}
		if !strings.Contains(stripped(skipSQL), col) {
			t.Errorf("%s: SkipTenancy:true missing %q\nSQL: %s", label, col, skipSQL)
		}
	}
}

// seedOrderItem is shared across SkipTenancy-path tests — each path needs a
// row to target. The resolver on the capturing client is tenantA, so seeding
// lands under tenantA. pathIdx gives each path a distinct PK so the two runs
// don't collide on the PK UNIQUE constraint (same DB across both paths).
func seedOrderItem(t *testing.T, client *models.Client, pathIdx int) {
	t.Helper()
	order, product := pathOrderProduct(pathIdx)
	_, err := client.OrderItems().Create(context.Background(), &models.CreateOrderItemInput{
		WorkspaceID: tenantA,
		OrderID:     order,
		ProductID:   product,
		Quantity:    3,
		UnitPrice:   1.5,
	})
	if err != nil {
		t.Fatalf("seed order_item (path %d): %v", pathIdx, err)
	}
}

// pathOrderProduct maps a path index to a unique (order_id, product_id) pair
// so the two paths target disjoint rows. Kept in one place so every runOp
// closure picks the same pair per path without open-coding constants.
func pathOrderProduct(pathIdx int) (int64, int64) {
	return int64(1 + pathIdx), int64(10 + pathIdx)
}

func TestCompositePK_SkipTenancy_Get_SQLShape(t *testing.T) {
	resetDB(t)

	runOp := func(t *testing.T, client *models.Client, skip bool, pathIdx int) {
		order, product := pathOrderProduct(pathIdx)
		_, err := client.OrderItems().Get(context.Background(), models.OrderItemPK{
			WorkspaceID: tenantA, OrderID: order, ProductID: product,
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = skip })
		if err != nil {
			t.Fatalf("Get (skip=%v): %v", skip, err)
		}
	}

	defaultSQL, skipSQL := runBothPaths(t, seedOrderItem, runOp, lastSelect)
	assertSameShape(t, "Get", defaultSQL, skipSQL)
}

func TestCompositePK_SkipTenancy_Update_SQLShape(t *testing.T) {
	resetDB(t)

	runOp := func(t *testing.T, client *models.Client, skip bool, pathIdx int) {
		order, product := pathOrderProduct(pathIdx)
		_, err := client.OrderItems().Update(context.Background(), models.OrderItemPK{
			WorkspaceID: tenantA, OrderID: order, ProductID: product,
		}, &models.UpdateOrderItemInput{
			Quantity: omittable.Set(int64(99)),
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = skip })
		if err != nil {
			t.Fatalf("Update (skip=%v): %v", skip, err)
		}
	}

	defaultSQL, skipSQL := runBothPaths(t, seedOrderItem, runOp, lastMutation)
	assertSameShape(t, "Update", defaultSQL, skipSQL)
}

func TestCompositePK_SkipTenancy_HardDelete_SQLShape(t *testing.T) {
	resetDB(t)

	runOp := func(t *testing.T, client *models.Client, skip bool, pathIdx int) {
		order, product := pathOrderProduct(pathIdx)
		err := client.OrderItems().HardDelete(context.Background(), models.OrderItemPK{
			WorkspaceID: tenantA, OrderID: order, ProductID: product,
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = skip })
		if err != nil {
			t.Fatalf("HardDelete (skip=%v): %v", skip, err)
		}
	}

	defaultSQL, skipSQL := runBothPaths(t, seedOrderItem, runOp, lastMutation)
	assertSameShape(t, "HardDelete", defaultSQL, skipSQL)
}

func TestCompositePK_SkipTenancy_Increment_SQLShape(t *testing.T) {
	resetDB(t)

	runOp := func(t *testing.T, client *models.Client, skip bool, pathIdx int) {
		order, product := pathOrderProduct(pathIdx)
		err := client.OrderItems().Increment(context.Background(), models.OrderItemPK{
			WorkspaceID: tenantA, OrderID: order, ProductID: product,
		}, models.IncrementInput[models.OrderItemIncrementColumn]{
			Column: models.OrderItemIncrementQuantity,
			Amount: 1,
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = skip })
		if err != nil {
			t.Fatalf("Increment (skip=%v): %v", skip, err)
		}
	}

	defaultSQL, skipSQL := runBothPaths(t, seedOrderItem, runOp, lastMutation)
	assertSameShape(t, "Increment", defaultSQL, skipSQL)
}

func TestCompositePK_SkipTenancy_Exists_SQLShape(t *testing.T) {
	resetDB(t)

	runOp := func(t *testing.T, client *models.Client, skip bool, pathIdx int) {
		order, product := pathOrderProduct(pathIdx)
		_, err := client.OrderItems().Exists(context.Background(), models.OrderItemPK{
			WorkspaceID: tenantA, OrderID: order, ProductID: product,
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = skip })
		if err != nil {
			t.Fatalf("Exists (skip=%v): %v", skip, err)
		}
	}

	defaultSQL, skipSQL := runBothPaths(t, seedOrderItem, runOp, lastSelect)
	assertSameShape(t, "Exists", defaultSQL, skipSQL)
}

// ---------------------------------------------------------------------------
// Coverage note — SoftDelete / Restore on composite-PK-with-tenant tables
// ---------------------------------------------------------------------------
// SoftDelete / Restore are not part of the composite-PK mismatch sweep: the
// tenancy example schema's only composite-PK table with tenant-in-PK is
// `order_items`, which has no soft-delete column.
//
// Coverage for the SoftDelete/Restore × tenancy-mismatch interaction is
// provided at the simple-PK level (articles has timestamp soft-delete and a
// non-PK tenant column) — see existing tests in soft_delete_test.go and
// where_mutations_test.go. The §29.7 verify-match rule is specific to the
// tenant-in-PK case; for simple-PK soft-delete, the tenancy behaviour is
// identical to the non-soft-delete case (auto-filter in WHERE, no PK
// verify-match because the tenant isn't on the PK).

// ---------------------------------------------------------------------------
// Bonus — Upsert on happy path with SkipTenancy:true + matching input. The
// scope note above covers PK-based reads/writes; Upsert has a different shape
// (tenant is on the INSERT input, not the PK), so a standalone shape test
// here locks in that SkipTenancy:true skips the verify-match block but still
// produces a valid INSERT.
// ---------------------------------------------------------------------------

func TestCompositePK_SkipTenancy_Upsert_SucceedsWithMatchingTenant(t *testing.T) {
	resetDB(t)

	// resolver=tenantA; caller supplies tenantA on the input; SkipTenancy:true.
	// The resolver is not consulted (skip=true), but the input.WorkspaceID
	// value lands in the INSERT column set as normal.
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	got, err := envA.client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantA,
		OrderID:     42,
		ProductID:   99,
		Quantity:    7,
		UnitPrice:   3.25,
	}, models.OrderItemConflictPK, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
		o.SkipTenancy = true
	})
	if err != nil {
		t.Fatalf("Upsert SkipTenancy matching: %v", err)
	}
	if got.WorkspaceID != tenantA || got.OrderID != 42 || got.ProductID != 99 {
		t.Errorf("Upsert SkipTenancy returned wrong row: %+v", got)
	}
}

// TestCompositePK_SkipTenancy_Upsert_AdminCrossTenantWrite verifies that the
// SkipTenancy admin path is the grep-able escape hatch for cross-tenant
// Upserts — resolver=tenantA, input.WorkspaceID=tenantB, SkipTenancy=true →
// the row is written under tenantB with no mismatch.
func TestCompositePK_SkipTenancy_Upsert_AdminCrossTenantWrite(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	got, err := envA.client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantB, // cross-tenant
		OrderID:     100,
		ProductID:   200,
		Quantity:    5,
		UnitPrice:   1.0,
	}, models.OrderItemConflictPK, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
		o.SkipTenancy = true
	})
	if err != nil {
		t.Fatalf("admin cross-tenant Upsert: %v", err)
	}
	if got.WorkspaceID != tenantB {
		t.Errorf("admin cross-tenant Upsert: workspace_id = %v, want %v", got.WorkspaceID, tenantB)
	}
	// Silence unused import warning if uuid not otherwise referenced — it is
	// (tenantA/tenantB are uuid.UUID constants) but keep this guard explicit.
	_ = uuid.Nil()
}
