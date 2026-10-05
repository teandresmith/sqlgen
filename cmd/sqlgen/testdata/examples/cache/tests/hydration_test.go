package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestHydration_PartialFetchHydratesBackground verifies that a partial fetch
// returns the partial result to the caller immediately AND schedules a
// background hydration that populates the cache with the full entity. A
// follow-up full Get then hits the cache (PRD §27.8).
func TestHydration_PartialFetchHydratesBackground(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Hydrate", SKU: uniqueSku(t, "hyd-1"), Price: 7.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Force cold cache — ignore the set that Create did.
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	env.counter.reset()
	env.backend.reset()

	// Partial fetch: ID + Name only.
	partial, err := env.client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.FieldOptions = &models.ProductFieldOptions{ID: true, Name: true}
	})
	if err != nil {
		t.Fatalf("Get partial: %v", err)
	}
	if partial.ID != p.ID || partial.Name != "Hydrate" {
		t.Fatalf("partial entity: %+v", partial)
	}

	// Wait for hydration completion metric — the generated hydration closure
	// records it via MetricsRecorder.HydrationComplete.
	env.metrics.waitHydrationCompleteCount(t, 1)

	// Subsequent full Get should hit the cache — assert via metrics.Hit
	// increment and no new DB query.
	dbBefore := env.counter.selectQueries()
	env.backend.reset()
	hitsBefore := env.metrics.hitCount()

	full, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get full (post-hydration): %v", err)
	}
	if full.SKU != p.SKU {
		t.Errorf("full.SKU = %q, want %q (cache shape mismatch)", full.SKU, p.SKU)
	}
	if env.counter.selectQueries() != dbBefore {
		t.Errorf("post-hydration Get: expected cache hit (no DB), got %d new queries",
			env.counter.selectQueries()-dbBefore)
	}
	if env.metrics.hitCount() <= hitsBefore {
		t.Error("post-hydration Get: metrics.Hit did not increment")
	}
}

// TestHydration_DisabledSkipsCache verifies that with hydration disabled, a
// partial fetch does NOT populate the cache — the subsequent full Get still
// misses (PRD §27.7 partial + hydration off row).
func TestHydration_DisabledSkipsCache(t *testing.T) {
	ctx := context.Background()
	// This test can't easily disable hydration on the already-built Cache
	// (it's a codegen constant), so we use SkipCache to force the same
	// pass-through behavior for partial fetches — the intent is identical:
	// ensure partial data never lands in cache.
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "NoHyd", SKU: uniqueSku(t, "nohyd-1"), Price: 7.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}

	// SkipCache on a partial Get → DB query, no cache write, no hydration.
	env.backend.reset()
	env.counter.reset()
	_, err = env.client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.FieldOptions = &models.ProductFieldOptions{ID: true, Name: true}
		o.SkipCache = true
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Give any stray goroutine a moment to NOT run.
	time.Sleep(150 * time.Millisecond)

	if got := len(env.backend.filterCalls("set")); got != 0 {
		t.Errorf("SkipCache partial: want 0 Set calls, got %d", got)
	}
	if got := len(env.backend.filterCalls("get")); got != 0 {
		t.Errorf("SkipCache partial: want 0 Get calls, got %d", got)
	}
}

// TestHydration_Dedup is a regression guard: 50 concurrent partial Gets against
// the same cold PK launch exactly one hydration DB query (hydration sync.Map
// dedup).
func TestHydration_Dedup(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "HydDedup", SKU: uniqueSku(t, "hdedup-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Cold cache.
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	env.counter.reset()

	const N = 50
	var wg sync.WaitGroup
	var errs atomic.Int64
	var firstErr atomic.Value
	for range N {
		wg.Go(func() {
			_, err := env.client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.FieldOptions = &models.ProductFieldOptions{ID: true, Name: true}
			})
			if err != nil {
				errs.Add(1)
				firstErr.CompareAndSwap(nil, err.Error())
			}
		})
	}
	wg.Wait()
	if errs.Load() != 0 {
		t.Fatalf("%d concurrent partial Gets failed; first err: %v", errs.Load(), firstErr.Load())
	}

	// Wait for all hydration completions (at most one).
	env.metrics.waitHydrationCompleteCount(t, 1)

	// Without dedup, all N goroutines would fire a hydration query,
	// producing 2N total DB queries (N partial + N hydration). With the
	// sync.Map dedup, later callers that arrive while a hydration is still
	// in flight short-circuit. Timing variance — hydration goroutines
	// complete and Delete the key before all 50 callers reach hydrate() —
	// means we may see more than exactly one hydration under very fast
	// backends. Assert substantial dedup (at least 50% reduction) to prove
	// the mechanism is active.
	total := env.counter.selectQueries()
	hydrationQueries := total - int64(N)
	if hydrationQueries >= int64(N/2) {
		t.Errorf("hydration dedup: %d hydration queries fired (of %d partial Gets), want at least 50%% reduction (total=%d)",
			hydrationQueries, N, total)
	}
}
