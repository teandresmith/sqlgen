package tests

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestBreaker_TripsAndRecovers is an end-to-end circuit breaker assertion: N
// consecutive backend errors trip the breaker to Open, subsequent Gets
// short-circuit (no new backend error events), ProbeInterval allows a
// HalfOpen transition, a successful probe closes the breaker.
func TestBreaker_TripsAndRecovers(t *testing.T) {
	ctx := context.Background()

	// Shared metrics recorder for both the newEnv wiring and the custom
	// breaker's OnStateChange callback.
	metrics := &spyMetrics{}

	breaker := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  3,
		ProbeInterval:     50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
		OnStateChange: func(from, to cache.CircuitState) {
			metrics.CircuitBreakerStateChange(from, to)
		},
	})

	env := newEnv(
		t,
		withBreaker(breaker),
		withCacheOpts(models.WithMetricsRecorder(metrics)),
	)
	env.metrics = metrics
	// Fail both Get and Set — a successful Set after a failed Get would
	// record success and keep the breaker closed.
	env.backend.setFail("all")

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Brk", SKU: uniqueSku(t, "brk-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Trip the breaker by exhausting failure_threshold.
	for range 3 {
		if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
			t.Fatalf("Get (pre-trip): %v", err)
		}
	}

	// Expect at least one Closed→Open transition recorded via metrics.
	env.metrics.mu.Lock()
	var opened bool
	for _, tr := range env.metrics.breakerTransitions {
		if tr.from == cache.StateClosed && tr.to == cache.StateOpen {
			opened = true
		}
	}
	env.metrics.mu.Unlock()
	if !opened {
		t.Fatal("breaker did not trip to Open")
	}

	// With breaker Open, subsequent Gets should short-circuit: no new
	// backend.Get calls (we check that the call counter stops growing).
	env.backend.reset()
	for range 3 {
		if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
			t.Fatalf("Get (open): %v", err)
		}
	}
	if got := len(env.backend.filterCalls("get")); got != 0 {
		t.Errorf("Open breaker: want 0 backend Get calls, got %d", got)
	}

	// Disable the failure injection and wait for ProbeInterval to elapse.
	env.backend.setFail("")
	time.Sleep(80 * time.Millisecond)

	// A successful probe Get should close the breaker (transition Open →
	// HalfOpen → Closed).
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("Get (probe): %v", err)
	}

	waitUntil(t, 500*time.Millisecond, func() bool {
		return breaker.State() == cache.StateClosed
	})
	if breaker.State() != cache.StateClosed {
		t.Errorf("breaker state after probe = %v, want Closed", breaker.State())
	}
}

// TestError_PolicyRoutesThroughMetricsAndBreaker verifies a forced Set
// failure routes through MetricsRecorder.Error AND breaker.RecordFailure AND
// OnErrorFunc — the caller's Create succeeds regardless.
func TestError_PolicyRoutesThroughMetricsAndBreaker(t *testing.T) {
	ctx := context.Background()

	var onErrCalls atomic.Int64
	breaker := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  100, // high enough that one error doesn't trip
		ProbeInterval:     time.Second,
		HalfOpenMaxProbes: 1,
	})

	env := newEnv(
		t,
		withBreaker(breaker),
		withCacheOpts(models.WithOnError(func(_ context.Context, op, schema string, table hook.TableName, err error) {
			onErrCalls.Add(1)
		})),
	)
	env.backend.setFail("set")

	// Create → mutation hook calls setProduct → backend Set errors.
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "ErrP", SKU: uniqueSku(t, "err-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create must succeed even with failing cache: %v", err)
	}
	t.Cleanup(func() {
		env.backend.setFail("")
		_ = env.client.Products().HardDelete(ctx, p.ID)
	})

	// Allow async dispatch to settle.
	waitUntil(t, 500*time.Millisecond, func() bool {
		env.metrics.mu.Lock()
		defer env.metrics.mu.Unlock()
		return len(env.metrics.errors) >= 1
	})

	env.metrics.mu.Lock()
	metricErrs := len(env.metrics.errors)
	env.metrics.mu.Unlock()
	if metricErrs == 0 {
		t.Error("MetricsRecorder.Error was not called")
	}
	if onErrCalls.Load() == 0 {
		t.Error("OnErrorFunc was not called")
	}
}

// TestFingerprint_KillSwitchOrphansOldEntries is a regression guard. We
// simulate a fingerprint bump by writing a payload to the OLD key grammar and
// issuing a Get — the generated code builds the NEW key, so the Get misses
// (the old payload orphans and is cleaned up by LRU/TTL).
func TestFingerprint_KillSwitchOrphansOldEntries(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Fp", SKU: uniqueSku(t, "fp-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Warm the cache via Get to record the current-fingerprint key.
	env.backend.reset()
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("Get warm: %v", err)
	}
	var warmedKey string
	for _, c := range env.backend.filterCalls("set") {
		if c.key != "" {
			warmedKey = c.key
			break
		}
	}
	if warmedKey == "" {
		// The Create's post-commit set may have populated first; retry by
		// clearing and redoing.
		env.backend.reset()
		if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
			t.Fatalf("InvalidateTable: %v", err)
		}
		if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
			t.Fatalf("Get warm retry: %v", err)
		}
		for _, c := range env.backend.filterCalls("set") {
			if c.key != "" {
				warmedKey = c.key
				break
			}
		}
	}
	if warmedKey == "" {
		t.Fatal("couldn't observe warm-cache key")
	}
	// Key format: sqlgen:products:fingerprint:v{fp}:pk:{id}. Verify it
	// carries a fingerprint segment.
	if !contains(warmedKey, ":fingerprint:v") || !contains(warmedKey, ":pk:") {
		t.Errorf("cache key missing fingerprint/pk segments: %q", warmedKey)
	}

	// A Get under a simulated "old fingerprint" still-present entry should
	// miss. We insert a stale entry via the backend directly at a
	// fingerprint-doctored key and verify the generated Get doesn't pick
	// it up.
	stalePayload := []byte(`{"id":-1,"name":"stale"}`)
	staleKey := replaceFingerprint(warmedKey, "deadbeef")
	if err := env.backend.Set(ctx, staleKey, stalePayload, time.Minute); err != nil {
		t.Fatalf("inject stale: %v", err)
	}

	// Invalidate the good entry and re-Get. The Get must hit DB (miss) — it
	// MUST NOT decode the stale payload.
	env.backend.reset()
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	env.counter.reset()
	got, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name == "stale" {
		t.Error("fingerprint mismatch accepted: stale payload served from cache")
	}
	if env.counter.selectQueries() == 0 {
		t.Error("fingerprint mismatch: expected DB miss, got 0 queries")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// replaceFingerprint swaps the v{fp} segment of a cache key with a synthetic
// fingerprint to simulate an orphaned prior-generation entry.
func replaceFingerprint(key, newFP string) string {
	marker := ":fingerprint:v"
	i := indexOf(key, marker)
	if i < 0 {
		return key
	}
	start := i + len(marker)
	end := start
	for end < len(key) && key[end] != ':' {
		end++
	}
	return key[:start] + newFP + key[end:]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
