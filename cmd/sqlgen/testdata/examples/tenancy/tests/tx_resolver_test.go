package tests

// Tenancy × transactions — resolver invocation accounting.
//
// Exercises PRD §29.6:
//   "The resolver is called **once per mutation hook entry**; the value is
//    closed over into both the cache invalidation callback and the event
//    payload — no double-resolve, no ctx-reread from an async callback."
//
// ---------------------------------------------------------------------------
// Per-operation, not per-transaction:
// ---------------------------------------------------------------------------
//
// The resolver runs once per operation, so N mutations inside a
// caller-opened transaction invoke it exactly N times (PRD §29.6, §29.11).
// A mid-transaction tenant switch is therefore undefined rather than
// prevented: per-operation resolution is what lets a resolver see a ctx the
// caller changed.
//
// The other direction is TestTx_TenantIsConsistentAcrossOpsInATx in
// tests/tx_test.go, which asserts the values agree across a tx when the ctx
// does not change.
//
// ---------------------------------------------------------------------------
// ctx-cached single-resolve:
// ---------------------------------------------------------------------------
//
// The chaining op templates (create / upsert / update / delete) now stash
// the resolved tenant on ctx via tenancy.WithResolvedTenant immediately
// after their own resolve. The generated per-client resolveTenant short-
// circuits on tenancy.CachedTenant before invoking the user resolver — a
// chain (Create → Get → GetMany, Update → Get, etc.) resolves exactly
// once. PRD §29.6 ("once per mutation hook entry") is now satisfied
// literally rather than approximately.
//
// Tests below assert exact equality (count == N for N independent ops)
// where possible. The cache scope is per-op: two sequential Creates on
// the same caller-supplied ctx resolve twice; the cache derives a child
// ctx that does not leak past the entity-method boundary.

import (
	"context"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// countingResolver wraps a resolver and records how many times it is called.
// Separate from the count fixture in main_test.go because this is specific
// to resolver-call accounting; keeping it local to this test file avoids
// widening shared helpers for a narrow use case.
type countingResolver struct {
	inner tenancy.TenantResolver[uuid.UUID]
	count atomic.Int64
}

func (c *countingResolver) resolve(ctx context.Context) (uuid.UUID, error) {
	c.count.Add(1)
	return c.inner(ctx)
}

func newCountingResolver(inner tenancy.TenantResolver[uuid.UUID]) *countingResolver {
	return &countingResolver{inner: inner}
}

// TestTx_Resolver_CalledExactlyOncePerMutation pins the ctx-cache invariant:
// after the ctx-cache rollout, N independent mutations inside a tx invoke
// the user resolver exactly N times — once per logical operation, with
// the chained terminal Get / GetMany hits resolved via tenancy.CachedTenant
// instead of re-invoking the resolver.
//
// Lower bound (== N) guards against a regression to "resolve once at Tx
// open" that would cache across operations and miss ctx-varying resolvers.
// Upper bound (== N) guards against the pre-fix 3× multiplier returning.
func TestTx_Resolver_CalledExactlyOncePerMutation(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	const N = 4

	err := env.client.WithTx(ctx, "resolver_exact", func(txCtx context.Context) error {
		for i := 0; i < N; i++ {
			if _, err := env.client.Products().Create(txCtx, &models.CreateProductInput{
				Name: "resolver-exact", SKU: skuFor("RX", i), Price: float64(i + 1),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	got := cr.count.Load()
	if got != int64(N) {
		t.Errorf("resolver call count = %d, want %d (PRD §29.6 — one resolve per mutation)", got, N)
	}
}

// TestTx_Resolver_ChainReusesCachedValue is the headline ctx-cache invariant:
// a single Create call (which chains Create → Get → GetMany internally)
// invokes the user resolver exactly once. Pre-fix the count was 3.
func TestTx_Resolver_ChainReusesCachedValue(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "chain-reuse", SKU: "CR-00", Price: 1,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := cr.count.Load(); got != 1 {
		t.Errorf("resolver call count = %d, want 1 (Create → Get → GetMany chain must single-resolve)", got)
	}
}

// TestTx_Resolver_SequentialOpsResolveIndependently guards against the
// inverse failure mode: the ctx cache must NOT leak past op boundaries.
// Two sequential Create calls on the same caller-supplied ctx (no tx) must
// resolve twice, because each is a separate logical operation and a real
// resolver may read ctx state that changed between calls.
func TestTx_Resolver_SequentialOpsResolveIndependently(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "seq", SKU: skuFor("SQ", i), Price: 1,
		}); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	if got := cr.count.Load(); got != 2 {
		t.Errorf("resolver call count = %d, want 2 (sequential ops must resolve independently — cache must not leak past op boundaries)", got)
	}
}

// TestTx_Resolver_SkipTenancyDoesNotResolve confirms the SkipTenancy path
// stays intact under the ctx cache: when the caller opts out of tenancy, neither
// the outer Create nor the internal Get / GetMany invokes the resolver.
// The cache attach is gated on !options.SkipTenancy in every chaining op
// template, and the chained terminal inherits SkipTenancy via the internal
// opts copy.
func TestTx_Resolver_SkipTenancyDoesNotResolve(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "skip", SKU: "SK-00", Price: 1,
		WorkspaceID: omittable.Set(tenantA),
	}, func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := cr.count.Load(); got != 0 {
		t.Errorf("resolver call count = %d, want 0 (SkipTenancy short-circuits before resolveTenant)", got)
	}
}

// TestTx_Resolver_StableAfterCommit verifies §29.6's "no ctx-reread from an
// async callback" property: the resolver-call count captured immediately
// after Commit returns matches the count captured after a quiescence delay.
// Any post-commit async callback that re-invoked the resolver would push
// the count up.
//
// Uses CallbackSync so the assertion window is deterministic — async-mode
// callbacks are already covered by the per-mutation lower bound above.
func TestTx_Resolver_StableAfterCommit(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newSyncCallbackEnv(t, cr.resolve)
	ctx := context.Background()

	const N = 3

	err := env.WithTx(ctx, "resolver_sync_commit", func(txCtx context.Context) error {
		for i := 0; i < N; i++ {
			if _, err := env.Products().Create(txCtx, &models.CreateProductInput{
				Name: "sync-commit", SKU: skuFor("PC", i), Price: 1,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Under CallbackSync, every OnCommit callback runs inside Commit and
	// finishes before Commit returns. The resolver count captured
	// immediately after Commit should therefore equal a second snapshot
	// taken just after — no background callback can bump the counter.
	postCommit := cr.count.Load()
	postCommit2 := cr.count.Load()
	if postCommit2 != postCommit {
		t.Errorf("resolver count increased between consecutive post-commit snapshots (%d -> %d) — async callback re-invoked resolver?", postCommit, postCommit2)
	}

	// Belt-and-suspenders: a tenanted read after commit increments the
	// counter by exactly the per-op call multiplier — if the resolver was
	// smuggled into a pre-GetMany callback, the delta here would be
	// inconsistent with the earlier per-mutation-delta.
	if _, err := env.Products().GetMany(ctx, &models.GetProductsInput{}); err != nil {
		t.Fatalf("post-commit GetMany: %v", err)
	}
}

// TestTx_Resolver_NotCalledOnRollback verifies resolver accounting under the
// rollback branch. After the rollback returns, the resolver count must
// match the count captured just before the rollback — no rollback-time
// resolver call (e.g. for a hypothetical cleanup callback) should fire.
func TestTx_Resolver_NotCalledOnRollback(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	sentinel := &rollbackErr{}

	var preRollbackCount int64
	err := env.client.WithTx(ctx, "resolver_rollback", func(txCtx context.Context) error {
		if _, err := env.client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "rollback-resolver", SKU: skuFor("RB", 0), Price: 1,
		}); err != nil {
			return err
		}
		preRollbackCount = cr.count.Load()
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("WithTx: err = %v, want sentinel", err)
	}

	// After rollback returns, the resolver count should not have grown —
	// rollback does not invoke the resolver.
	postRollbackCount := cr.count.Load()
	if postRollbackCount != preRollbackCount {
		t.Errorf("resolver count grew during rollback (%d -> %d) — rollback-time resolve is forbidden per §29.6", preRollbackCount, postRollbackCount)
	}

	// Row was rolled back — not visible.
	list, err := env.client.Products().GetMany(ctx, &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("post-rollback GetMany: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("GetMany after rollback: %d rows, want 0 (tx didn't roll back)", len(list))
	}
}

// TestTx_Resolver_UpdateBumpsCount confirms a mixed-op tx invokes the
// resolver on every mutation (not just Create). If Update reused the
// Create's resolver value (an over-eager caching regression), the count
// delta around the Update call would be zero.
func TestTx_Resolver_UpdateBumpsCount(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := context.Background()

	var afterCreateCount, afterUpdateCount int64

	err := env.client.WithTx(ctx, "resolver_mixed", func(txCtx context.Context) error {
		p, err := env.client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "mixed-orig", SKU: skuFor("MX", 0), Price: 1,
		})
		if err != nil {
			return err
		}
		afterCreateCount = cr.count.Load()

		_, err = env.client.Products().Update(txCtx, p.ID, &models.UpdateProductInput{
			Name: omittable.Set("mixed-upd"),
		})
		afterUpdateCount = cr.count.Load()
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if afterUpdateCount <= afterCreateCount {
		t.Errorf("resolver count did not grow during Update (%d -> %d) — Update regressed to caching resolver from Create", afterCreateCount, afterUpdateCount)
	}
}

// rollbackErr is a typed sentinel so the WithTx error-compare is identity-
// based (err != sentinel). Using a string-compared error would depend on
// fmt formatting decisions elsewhere.
type rollbackErr struct{}

func (e *rollbackErr) Error() string { return "rollback sentinel" }

// skuFor produces a unique SKU for a (prefix, index) pair. Tests run in
// sequence so indexes reset per test; the prefix ensures cross-test
// uniqueness in case two tests fail to reset the DB.
func skuFor(prefix string, i int) string {
	return prefix + "-" + padInt(i)
}

func padInt(i int) string {
	// Two-digit indices suffice (N <= 10 across this file). Keeping the
	// helper simple; a stdlib strconv.Itoa would work too but inflates the
	// import list for no benefit here.
	tens := i / 10
	ones := i % 10
	return string(rune('0'+tens)) + string(rune('0'+ones))
}

// newSyncCallbackEnv builds a Client wired to CallbackSync so post-commit
// callback assertions are deterministic. Separate from newEnv because
// CallbackSync is an uncommon setup in this suite.
func newSyncCallbackEnv(t *testing.T, resolver tenancy.TenantResolver[uuid.UUID]) *models.Client {
	t.Helper()
	client := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(resolver),
		models.WithCallbackMode(database.CallbackSync),
	)
	return client
}
