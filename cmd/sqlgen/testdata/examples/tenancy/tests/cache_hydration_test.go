package tests

// Tenancy × cache hydration coverage (PRD §27.8, §29.5).
//
// `articles` is the only table in the tenancy example that combines tenancy
// AND a soft-delete column (`deleted_at`). It is therefore the natural
// fixture for "partial-fetch hydration on tenanted + soft-deleted table
// respects both filters; background hydration does not leak cross-tenant
// rows".
//
// The hydration goroutine fires the same `next` chain the original Get
// uses, with `FieldOptions = full-parent` and `SkipHooks: true` — and it
// reuses the tenant resolved at the entity-method boundary (`q.Tenant`)
// rather than re-resolving from a Background ctx. Both filter properties
// flow from "the SQL builder applies tenant + soft-delete filters" — the
// hydration query goes through the same builder, so anything that's
// excluded for a normal Get is excluded for the hydration query too.
//
// Coverage:
//   1. Hydration cache key carries the requesting tenant's segment (per
//      PRD §29.5) → tenant B's Get on the same PK is a clean miss with
//      DB-level isolation (no cross-tenant cache leak).
//   2. Hydration of a soft-deleted row is a no-op (the DB returns
//      ErrNotFound under the soft-delete filter, the hydration goroutine
//      bails before writing to the cache).
//   3. Hydration after Restore populates the cache fresh (round-trip
//      restored row → cache → hot reads serve fresh data).

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// hydrationSpyBackend wraps a cache backend and records the keys of every
// Set call so tests can assert "the hydrated key carries tenant A's segment
// AND not tenant B's". Set is the terminal hydration step — observing it
// proves the hydration goroutine reached the write.
type hydrationSpyBackend struct {
	inner cache.Backend

	mu       sync.Mutex
	setKeys  []string
	setCount atomic.Int64
}

func newHydrationSpyBackend(inner cache.Backend) *hydrationSpyBackend {
	return &hydrationSpyBackend{inner: inner}
}

func (s *hydrationSpyBackend) Get(ctx context.Context, key string) ([]byte, error) {
	return s.inner.Get(ctx, key)
}

func (s *hydrationSpyBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	s.setKeys = append(s.setKeys, key)
	s.mu.Unlock()
	s.setCount.Add(1)
	return s.inner.Set(ctx, key, value, ttl)
}

func (s *hydrationSpyBackend) Invalidate(ctx context.Context, key string) error {
	return s.inner.Invalidate(ctx, key)
}

func (s *hydrationSpyBackend) InvalidateMany(ctx context.Context, keys []string) error {
	return s.inner.InvalidateMany(ctx, keys)
}

func (s *hydrationSpyBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	return s.inner.InvalidatePattern(ctx, pattern)
}

func (s *hydrationSpyBackend) snapshotSetKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.setKeys))
	copy(out, s.setKeys)
	return out
}

func (s *hydrationSpyBackend) waitForSetCount(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.setCount.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hydration: waited 2s for %d Set calls, observed %d", want, s.setCount.Load())
}

// newHydrationEnv builds a tenancy testEnv with a hydration-spy-wrapped
// cache backend. Returns the env plus the spy so the test can inspect Set
// keys and poll for hydration completion.
func newHydrationEnv(t *testing.T, tenant uuid.UUID) (*testEnv, *hydrationSpyBackend) {
	t.Helper()

	innerBackend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = innerBackend.Close() })

	spy := newHydrationSpyBackend(innerBackend)
	metrics := newSpyMetrics()
	c, err := models.NewCache(spy, models.WithMetricsRecorder(metrics))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	counter := newCountingQuerier(dbstdlib.New(testDB))
	resolver := staticResolver(tenant)
	client := models.New(
		counter,
		models.WithTenantResolver(resolver),
		models.WithCache(c),
	)

	return &testEnv{
		client:   client,
		counter:  counter,
		cache:    c,
		backend:  spy,
		metrics:  metrics,
		resolver: resolver,
	}, spy
}

// TestCacheHydration_TenantScopedKeyNoCrossTenantLeak verifies §29.5: the
// hydration goroutine writes to a cache key that carries the resolving
// tenant's segment — not "any tenant" or "default". A Get from a different
// tenant on the same PK does NOT see the hydrated entry; it misses the
// cache and the DB returns nothing under that tenant's filter (so the
// caller sees ErrNotFound).
func TestCacheHydration_TenantScopedKeyNoCrossTenantLeak(t *testing.T) {
	resetDB(t)

	envA, spyA := newHydrationEnv(t, tenantA)

	ctx := context.Background()

	a, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "Hydrate-A",
	})
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}

	// Force cold cache so the post-Create cache write is gone — the only
	// cache write we want to observe is the hydration write.
	if err := envA.cache.InvalidateTable(ctx, models.TableArticles); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	// Drain the spy state too.
	spyA.mu.Lock()
	spyA.setKeys = nil
	spyA.mu.Unlock()
	spyA.setCount.Store(0)

	// Partial Get on the live row → returns the partial result; hydration
	// fires asynchronously.
	partial, err := envA.client.Articles().Get(ctx, a.ID, func(o *models.CallOptions[models.ArticleFieldOptions]) {
		o.FieldOptions = &models.ArticleFieldOptions{ID: true, Title: true}
	})
	if err != nil {
		t.Fatalf("partial Get A: %v", err)
	}
	if partial.ID != a.ID || partial.Title != "Hydrate-A" {
		t.Fatalf("partial: %+v", partial)
	}

	// Wait for hydration to complete (one Set call from hydrate goroutine).
	spyA.waitForSetCount(t, 1)

	// The hydrated key MUST contain tenant A's UUID segment and MUST NOT
	// contain tenant B's. This is the §29.5 grammar: tenant: segment
	// before fingerprint.
	keys := spyA.snapshotSetKeys()
	if len(keys) != 1 {
		t.Fatalf("want exactly 1 hydration Set, got %d: %+v", len(keys), keys)
	}
	if !strings.Contains(keys[0], "tenant:"+tenantA.String()) {
		t.Errorf("hydrated key %q missing tenant A segment", keys[0])
	}
	if strings.Contains(keys[0], "tenant:"+tenantB.String()) {
		t.Errorf("hydrated key %q contains tenant B segment — cross-tenant leak", keys[0])
	}

	// Tenant B with the SAME backend issues a Get on tenant A's PK.
	// Two independent guards prove no cross-tenant leak:
	//   1. The cache key for tenant B differs (different :tenant: segment),
	//      so the cache MISSES.
	//   2. The DB-level tenancy filter excludes A's row from B's reads, so
	//      the Get returns ErrNotFound.
	// The hydration cache entry is invisible to B.
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB.client = models.New(
		envB.counter,
		models.WithTenantResolver(envB.resolver),
		models.WithCache(envA.cache),
	)
	envB.cache = envA.cache

	missesBefore := envA.metrics.missCount()
	hitsBefore := envA.metrics.hitCount()

	if _, err := envB.client.Articles().Get(ctx, a.ID); err == nil {
		t.Fatalf("Get B on A's PK: err = nil, want ErrNotFound (cross-tenant cache leak!)")
	}
	if got := envA.metrics.hitCount() - hitsBefore; got != 0 {
		t.Errorf("Get B: cache hits = %d, want 0 (any hit means tenant B saw tenant A's hydrated entry)", got)
	}
	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("Get B: misses = %d, want 1", got)
	}
}

// TestCacheHydration_SoftDeletedRowNotHydrated verifies that hydration on a
// soft-deleted row is a no-op. The DB returns ErrNotFound under the
// auto-applied soft-delete filter, the partial Get path returns the
// caller's error before the hydration call, and no cache write fires.
//
// (The implementation in readThroughArticle calls hydrateArticle ONLY
// after `result, err := next(ctx, q)` returns nil error — soft-delete
// returns ErrNotFound here, so hydration never even launches.)
func TestCacheHydration_SoftDeletedRowNotHydrated(t *testing.T) {
	resetDB(t)

	env, spy := newHydrationEnv(t, tenantA)

	ctx := context.Background()

	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "ToDelete",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := env.client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Drain spy state.
	spy.mu.Lock()
	spy.setKeys = nil
	spy.mu.Unlock()
	spy.setCount.Store(0)

	// Partial Get on the soft-deleted row.
	_, err = env.client.Articles().Get(ctx, a.ID, func(o *models.CallOptions[models.ArticleFieldOptions]) {
		o.FieldOptions = &models.ArticleFieldOptions{ID: true, Title: true}
	})
	if err == nil {
		t.Fatalf("partial Get on soft-deleted row: err = nil, want ErrNotFound")
	}

	// Give any stray hydration goroutine a moment to NOT run.
	time.Sleep(150 * time.Millisecond)

	if got := spy.setCount.Load(); got != 0 {
		t.Errorf("hydration on soft-deleted row: Set count = %d, want 0 (hydration must respect deleted_at filter)", got)
	}
}

// TestCacheHydration_RestoredRowHydratesFresh verifies the
// soft-delete → restore cycle: after Restore, the cache is empty (the
// invalidate_many cleared anything cached), and a fresh partial Get
// hydrates the live restored entity. A follow-up full Get hits the cache.
func TestCacheHydration_RestoredRowHydratesFresh(t *testing.T) {
	resetDB(t)

	env, spy := newHydrationEnv(t, tenantA)

	ctx := context.Background()

	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "Restore-cycle",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := env.client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := env.client.Articles().Restore(ctx, a.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Drain spy state — count only the post-Restore hydration write.
	spy.mu.Lock()
	spy.setKeys = nil
	spy.mu.Unlock()
	spy.setCount.Store(0)

	partial, err := env.client.Articles().Get(ctx, a.ID, func(o *models.CallOptions[models.ArticleFieldOptions]) {
		o.FieldOptions = &models.ArticleFieldOptions{ID: true, Title: true}
	})
	if err != nil {
		t.Fatalf("partial Get post-restore: %v", err)
	}
	if partial.Title != "Restore-cycle" {
		t.Errorf("partial.Title = %q, want %q", partial.Title, "Restore-cycle")
	}

	spy.waitForSetCount(t, 1)

	keys := spy.snapshotSetKeys()
	if len(keys) != 1 {
		t.Fatalf("want exactly 1 hydration Set, got %d", len(keys))
	}
	if !strings.Contains(keys[0], "tenant:"+tenantA.String()) {
		t.Errorf("post-restore hydrated key %q missing tenant A segment", keys[0])
	}

	// Full Get now hits the freshly-hydrated cache entry.
	hitsBefore := env.metrics.hitCount()
	full, err := env.client.Articles().Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("full Get post-hydration: %v", err)
	}
	if full.Title != "Restore-cycle" {
		t.Errorf("full.Title = %q, want %q", full.Title, "Restore-cycle")
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Error("full Get post-hydration: expected cache hit, got 0")
	}
}
