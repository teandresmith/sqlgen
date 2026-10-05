package tests

// LockMode E2E (Tenancy module).
//
// The tenancy example is sqlite-backed (see main_test.go header) so the
// PRD §9.6a SQLite limitation applies: every non-LockNone mode is rejected
// at the runtime guard before any SQL roundtrip. The property
// "LockForUpdate only locks rows in the resolved tenant" — i.e. SELECT…FOR
// UPDATE composing with the tenancy WHERE injection — is not observable on
// SQLite because the SQL is never emitted.
//
// What is observable, and what this file pins, is the *guard ordering*
// invariant: when both tenancy and LockMode are configured on the same
// call, the dialect-aware LockMode guard fires first (and rejects on
// SQLite) — the tenant resolver is not even consulted, no SQL is issued,
// and the resulting error is the SQLite-flavoured rejection.
//
// PRD §9.6a "Hook chain interaction" places the guard at step 2 (before
// the hook chain runs at step 4). That ordering means a tenancy-resolver
// failure cannot mask a SQLite LockMode error and vice-versa: SQLite
// rejects independent of tenant state. Cross-dialect tenant-scope locking
// behaviour belongs in the postgres / mysql tests where the locking SQL is
// actually emitted.
//
// Inter-dialect coverage of the tenancy + LockMode combination at the
// codegen layer is pinned by the TestLockModeGuard_GetMethod_postgres /
// _mysql shape tests, which exercise the same emission path used by the
// tenanted postgres / mysql modules (the tenancy hook is dialect-portable
// and dispatches after the guard).

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// countingTenancyQuerier wraps the stdlib sqlite querier so the LockMode
// rejection tests can pin "no SQL roundtrip" — proving the dialect-aware
// guard short-circuits before any tenancy WHERE injection or DB call.
type countingTenancyQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64
}

func newCountingTenancyQuerier(inner database.Querier) *countingTenancyQuerier {
	return &countingTenancyQuerier{inner: inner}
}

func (c *countingTenancyQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.execs.Add(1)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *countingTenancyQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.queries.Add(1)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *countingTenancyQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.queryRow.Add(1)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *countingTenancyQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingTenancyQuerier) selectOps() int64 {
	return c.queries.Load() + c.queryRow.Load()
}

func (c *countingTenancyQuerier) reset() {
	c.queries.Store(0)
	c.queryRow.Store(0)
	c.execs.Store(0)
}

// TestLockMode_TenancyResolverNotConsulted verifies the guard ordering: the
// SQLite LockMode rejection fires before the tenancy resolver is consulted.
// We wire a "panicking" resolver — if the resolver runs, the test fails. The
// guard must short-circuit with the SQLite-rejection error, so the resolver
// is never reached and no SQL is issued.
func TestLockMode_TenancyResolverNotConsulted(t *testing.T) {
	t.Cleanup(func() { resetDB(t) })

	// Seed via a normal tenant-A resolver so the row exists.
	seedEnv := newEnv(t, withResolver(staticResolver(tenantA)))
	seed, err := seedEnv.client.Products().Create(context.Background(), &models.CreateProductInput{
		Name:  "lock-tenancy-seed",
		SKU:   "lock-tenancy-seed",
		Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create seed product: %v", err)
	}

	// Build a resolver that records whether it was called. The guard MUST
	// fire before the tenancy hook runs, so this counter must remain 0.
	var resolverCalls atomic.Int64
	resolver := tenancy.TenantResolver[uuid.UUID](func(ctx context.Context) (uuid.UUID, error) {
		resolverCalls.Add(1)
		return tenantA, nil
	})

	counter := newCountingTenancyQuerier(dbstdlib.New(testDB))
	client := models.New(counter, models.WithTenantResolver(resolver))

	// Reset both counters — only the guarded call must contribute.
	counter.reset()
	resolverCalls.Store(0)

	const wantSubstr = "LockMode is unsupported on sqlite dialect"

	for _, mode := range []sql.LockMode{
		sql.LockForUpdate,
		sql.LockForShare,
		sql.LockForUpdateNoWait,
		sql.LockForUpdateSkipLocked,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			counter.reset()
			resolverCalls.Store(0)

			_, err := client.Products().Get(context.Background(), seed.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.LockMode = mode
			})
			if err == nil {
				t.Fatalf("Get(LockMode=%s) on tenanted SQLite: err = nil, want %q", mode, wantSubstr)
			}
			if !strings.Contains(err.Error(), wantSubstr) {
				t.Errorf("Get(LockMode=%s) on tenanted SQLite: err = %q, want substring %q", mode, err.Error(), wantSubstr)
			}
			if got := counter.selectOps(); got != 0 {
				t.Errorf("Get(LockMode=%s): SELECT-class ops = %d, want 0 (guard must short-circuit before SQL)", mode, got)
			}
			if got := resolverCalls.Load(); got != 0 {
				t.Errorf("Get(LockMode=%s): tenant resolver called %d times, want 0 (guard must precede tenancy hook)", mode, got)
			}
		})
	}
}

// TestLockMode_TenancyMissingResolverStillSeesSQLiteRejection covers the
// fail-closed companion: even when the tenant resolver would have failed
// with tenancy.ErrMissing, the SQLite LockMode guard fires first. The
// surfaced error is the SQLite rejection, NOT tenancy.ErrMissing — pinning
// the guard ordering rule (PRD §9.6a step 2 precedes step 4 hook chain).
func TestLockMode_TenancyMissingResolverStillSeesSQLiteRejection(t *testing.T) {
	t.Cleanup(func() { resetDB(t) })

	// Seed via a real tenant first so a target ID exists.
	seedEnv := newEnv(t, withResolver(staticResolver(tenantA)))
	seed, err := seedEnv.client.Products().Create(context.Background(), &models.CreateProductInput{
		Name:  "lock-missing-resolver",
		SKU:   "lock-missing-resolver",
		Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create seed: %v", err)
	}

	// Now wire the missing-resolver client. A normal Get would fail with
	// tenancy.ErrMissing — but Get with LockMode must surface the SQLite
	// rejection instead, because the guard runs before the resolver.
	counter := newCountingTenancyQuerier(dbstdlib.New(testDB))
	client := models.New(counter, models.WithTenantResolver(missingResolver()))

	counter.reset()

	_, err = client.Products().Get(context.Background(), seed.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.LockMode = sql.LockForUpdate
	})
	if err == nil {
		t.Fatal("Get(LockForUpdate) with missing resolver: err = nil, want SQLite rejection")
	}
	if errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Get(LockForUpdate) with missing resolver returned ErrMissing — guard ordering broken (tenancy hook ran before LockMode guard): %v", err)
	}
	if !strings.Contains(err.Error(), "LockMode is unsupported on sqlite dialect") {
		t.Errorf("Get(LockForUpdate) error = %q, want SQLite rejection substring", err.Error())
	}
	if got := counter.selectOps(); got != 0 {
		t.Errorf("Get(LockForUpdate): SELECT-class ops = %d, want 0", got)
	}
}
