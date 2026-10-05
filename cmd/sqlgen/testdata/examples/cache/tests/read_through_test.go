package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestReadThrough_MissThenHit verifies the cache read-through contract: first
// Get misses, runs a DB query, populates the cache; second Get hits and does
// not touch the DB (PRD §27.7).
func TestReadThrough_MissThenHit(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Widget", SKU: uniqueSku(t, "rt-1"), Price: 1.99,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Ensure a cold cache — the Create mutation hook populated it; clear so
	// the first Get truly exercises miss → DB → Set.
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	env.counter.reset()
	env.backend.reset()

	// First Get: miss → DB → cache set.
	first, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get (miss): %v", err)
	}
	if first.ID != p.ID {
		t.Fatalf("Get returned wrong entity: %+v", first)
	}

	// Cold miss path: outer probe (miss) + in-flight re-check inside the
	// singleflight callback (also miss, since the cache really is cold) +
	// one Set after the DB fetch. The in-flight re-check is what
	// makes this 2 backend Gets instead of 1; on a warm cache the outer
	// probe hits and the flight is never entered (asserted below).
	missCalls := env.backend.filterCalls("get")
	setCalls := env.backend.filterCalls("set")
	if len(missCalls) != 2 || len(setCalls) != 1 {
		t.Fatalf("first Get: want 2 get + 1 set, got %d get + %d set", len(missCalls), len(setCalls))
	}
	if env.counter.selectQueries() == 0 {
		t.Fatalf("first Get: expected DB query, got 0")
	}

	// Second Get: hit → no DB, no new Set.
	dbQueriesBefore := env.counter.selectQueries()
	env.backend.reset()

	second, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get (hit): %v", err)
	}
	if second.ID != p.ID || second.Name != "Widget" {
		t.Fatalf("Get hit returned wrong entity: %+v", second)
	}

	if env.counter.selectQueries() != dbQueriesBefore {
		t.Errorf("second Get: DB queries went from %d to %d (cache miss?)", dbQueriesBefore, env.counter.selectQueries())
	}
	getCalls := env.backend.filterCalls("get")
	setCalls = env.backend.filterCalls("set")
	if len(getCalls) != 1 {
		t.Errorf("second Get: want 1 backend Get call, got %d", len(getCalls))
	}
	if len(setCalls) != 0 {
		t.Errorf("second Get: want 0 backend Set calls (hit), got %d", len(setCalls))
	}
	if env.metrics.hitCount() == 0 {
		t.Error("second Get: expected metrics.Hit, got none")
	}
}

// TestReadThrough_SingleflightDedup verifies that 50 concurrent Gets on a cold
// key collapse into a single DB round-trip via golang.org/x/sync/singleflight.
func TestReadThrough_SingleflightDedup(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Stampede", SKU: uniqueSku(t, "sf-1"), Price: 2.99,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Force a cold cache by clearing anything that the Create's cache.Set
	// populated. The cache hook only populates on Get-miss, but the mutation
	// hook's set<Product> populates on Create — wipe the backend's inner
	// store so the next Get truly cold-starts.
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}

	env.counter.reset()
	env.backend.reset()

	const N = 50
	var wg sync.WaitGroup
	var errs atomic.Int64
	for range N {
		wg.Go(func() {
			got, err := env.client.Products().Get(ctx, p.ID)
			if err != nil || got == nil || got.ID != p.ID {
				errs.Add(1)
			}
		})
	}
	wg.Wait()
	if errs.Load() != 0 {
		t.Fatalf("%d concurrent Gets failed", errs.Load())
	}

	dbCalls := env.counter.selectQueries()
	if dbCalls != 1 {
		t.Errorf("singleflight dedup: want exactly 1 DB query for %d concurrent misses, got %d", N, dbCalls)
	}
}

// firstMissBackend is a cache.Backend wrapper that returns a forced miss on
// the FIRST Get for any given key, then delegates to the inner backend on all
// subsequent Gets. It deterministically simulates the stampede edge
// where the outer cache probe observes miss but the in-flight re-check (the
// leader's second Get inside the DoChan callback) hits a cache that a sibling
// flight has already populated. Without an inner re-check, the singleflight
// callback would run a duplicate DB query; with it, the inner re-check returns the populated entity without invoking next.
type firstMissBackend struct {
	inner       cache.Backend
	firstMissed sync.Map
	getCount    atomic.Int64
}

func (b *firstMissBackend) Get(ctx context.Context, key string) ([]byte, error) {
	b.getCount.Add(1)
	if _, loaded := b.firstMissed.LoadOrStore(key, struct{}{}); !loaded {
		return nil, nil
	}
	return b.inner.Get(ctx, key)
}

func (b *firstMissBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return b.inner.Set(ctx, key, value, ttl)
}

func (b *firstMissBackend) Invalidate(ctx context.Context, key string) error {
	return b.inner.Invalidate(ctx, key)
}

func (b *firstMissBackend) InvalidateMany(ctx context.Context, keys []string) error {
	return b.inner.InvalidateMany(ctx, keys)
}

func (b *firstMissBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	return b.inner.InvalidatePattern(ctx, pattern)
}

// TestReadThrough_SingleflightLateStraggler is the deterministic stampede
// regression guard. The flaky TestReadThrough_SingleflightDedup catches the
// race only when scheduling timing happens to expose it; this test forces the
// outer-probe-observed-miss + flight-callback-observed-hit ordering directly
// via firstMissBackend, so the in-flight re-check must fire to satisfy the
// assertion. Without the re-check, the leader skips straight to next
// and runs a duplicate DB query, failing this test deterministically.
func TestReadThrough_SingleflightLateStraggler(t *testing.T) {
	ctx := context.Background()

	inner, err := cachememory.New(cachememory.Options{MaxSize: 100})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })

	fixed := &firstMissBackend{inner: inner}
	env := newEnvWithMemory(t, fixed)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Straggler", SKU: uniqueSku(t, "ls-1"), Price: 4.99,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Create's mutation hook populated the inner cache via Set (which
	// firstMissBackend delegates straight through). The next Get(pk) for
	// this product key has not yet been observed by firstMissBackend, so
	// its first observation will be a forced miss while subsequent Gets
	// for the same key delegate to the populated inner.
	env.counter.reset()
	getsBefore := fixed.getCount.Load()

	got, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.ID != p.ID {
		t.Fatalf("Get returned wrong entity: %+v", got)
	}

	if dbCalls := env.counter.selectQueries(); dbCalls != 0 {
		t.Errorf("late-straggler: want 0 DB queries (in-flight re-check should hit cache), got %d", dbCalls)
	}
	// The outer probe + the in-flight re-check together must produce
	// exactly two backend Get calls. Anything less means the re-check is
	// missing; anything more means an unexpected extra probe was added.
	if gets := fixed.getCount.Load() - getsBefore; gets != 2 {
		t.Errorf("late-straggler: want 2 backend Gets (outer + in-flight re-check), got %d", gets)
	}
}

// TestReadThrough_InvalidateTable clears every fingerprint generation via the
// direct Cache.InvalidateTable API (PRD §27.9).
func TestReadThrough_InvalidateTable(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "ToInvalidate", SKU: uniqueSku(t, "it-1"), Price: 3.99,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Warm the cache.
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("warm Get: %v", err)
	}

	env.backend.reset()
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_pattern")
	if len(calls) != 1 {
		t.Fatalf("InvalidateTable: want 1 invalidate_pattern call, got %d", len(calls))
	}
	if calls[0].pattern != "sqlgen:products:*" {
		t.Errorf("InvalidateTable pattern = %q, want %q", calls[0].pattern, "sqlgen:products:*")
	}
}
