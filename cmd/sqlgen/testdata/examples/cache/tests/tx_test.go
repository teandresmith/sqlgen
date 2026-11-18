package tests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// orderedRecorder wraps *spyMetrics and also records a timeline of (action,
// table) pairs so the commit-ordering test can assert event-before-cache
// semantics.
type orderedRecorder struct {
	*spyMetrics
	mu       sync.Mutex
	timeline []string // e.g., "event:products", "invalidate:products"
}

func (r *orderedRecorder) addEntry(s string) {
	r.mu.Lock()
	r.timeline = append(r.timeline, s)
	r.mu.Unlock()
}

// TestTx_EventBeforeCacheOnCommit is a regression guard. Inside a
// transaction, the event handler fires before the cache invalidation on commit
// (FIFO OnCommit preserves entity-before-view ordering — here, event-before-
// cache since cache is the outermost hook).
func TestTx_EventBeforeCacheOnCommit(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	rec := &orderedRecorder{spyMetrics: env.metrics}

	// Overlay an event subscriber that tags the timeline.
	collector := &orderedCollector{}
	sub, err := env.bus.Subscribe(event.SubscribeOptions{Tables: []string{"products"}},
		func(_ context.Context, e event.Event) error {
			rec.addEntry(fmt.Sprintf("event:%s", e.Table))
			collector.record(e)
			return nil
		})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Prime an existing product so Update can run inside the tx.
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "TxOrder", SKU: uniqueSku(t, "tx-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Swap to synchronous callback mode so OnCommit runs in-thread, and give
	// the client a cache whose backend writes each invalidation into the
	// timeline as it happens, beside the event subscriber's entries.
	c, err := models.NewCache(&timelineBackend{Backend: env.backend, rec: rec})
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	syncClient := models.New(
		env.counter,
		models.WithEventPublisher(env.bus),
		models.WithCache(c),
		models.WithCallbackMode(database.CallbackSync),
	)

	// Drop the setup Create's event: only the transaction's entries count.
	rec.mu.Lock()
	rec.timeline = nil
	rec.mu.Unlock()

	err = syncClient.WithTx(ctx, "order-check", func(ctx context.Context) error {
		_, err := syncClient.Products().Update(ctx, p.ID, &models.UpdateProductInput{
			Name: omittable.Set("TxOrder2"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Verify at least one event + one invalidation recorded.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.timeline) < 2 {
		t.Fatalf("timeline too short: %v", rec.timeline)
	}
	// Find the first event index vs first invalidate index.
	firstEvent, firstInv := -1, -1
	for i, entry := range rec.timeline {
		if firstEvent == -1 && entry == "event:products" {
			firstEvent = i
		}
		if firstInv == -1 && entry == "invalidate:products" {
			firstInv = i
		}
	}
	if firstEvent == -1 || firstInv == -1 {
		t.Fatalf("missing timeline entry, got %v", rec.timeline)
	}
	if firstEvent > firstInv {
		t.Errorf("event-before-cache order violated: event at %d, invalidate at %d (timeline %v)",
			firstEvent, firstInv, rec.timeline)
	}
}

// timelineBackend records every invalidation in the shared timeline at the
// moment the backend receives it.
type timelineBackend struct {
	cache.Backend
	rec *orderedRecorder
}

func (b *timelineBackend) InvalidateMany(ctx context.Context, keys []string) error {
	b.rec.addEntry("invalidate:products")
	return b.Backend.InvalidateMany(ctx, keys)
}

func (b *timelineBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	b.rec.addEntry("invalidate:products")
	return b.Backend.InvalidatePattern(ctx, pattern)
}

type orderedCollector struct {
	mu sync.Mutex
	ev []event.Event
}

func (o *orderedCollector) record(e event.Event) {
	o.mu.Lock()
	o.ev = append(o.ev, e)
	o.mu.Unlock()
}

// TestTx_RollbackDiscardsInvalidation verifies a rolled-back transaction
// produces neither event delivery nor cache invalidation.
func TestTx_RollbackDiscardsInvalidation(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"products"}})

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Rollback", SKU: uniqueSku(t, "rb-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Sync callback mode for deterministic post-commit handling.
	syncClient := models.New(
		env.counter,
		models.WithEventPublisher(env.bus),
		models.WithCache(env.cache),
		models.WithCallbackMode(database.CallbackSync),
	)

	env.backend.reset()
	// Pre-existing event from Create; clear the collector snapshot marker.
	preEvents := len(collector.snapshot())

	rollbackErr := errors.New("intentional rollback")
	err = syncClient.WithTx(ctx, "rollback-check", func(ctx context.Context) error {
		if _, err := syncClient.Products().Update(ctx, p.ID, &models.UpdateProductInput{
			Name: omittable.Set("ShouldNotPersist"),
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if err == nil || !errors.Is(err, rollbackErr) {
		t.Fatalf("WithTx: want rollback error, got %v", err)
	}

	// Settle any async dispatch.
	time.Sleep(100 * time.Millisecond)

	invCalls := env.backend.filterCalls("invalidate", "invalidate_many", "invalidate_pattern")
	if len(invCalls) != 0 {
		t.Errorf("rollback: want 0 invalidations, got %d", len(invCalls))
	}

	postEvents := len(collector.snapshot())
	if postEvents != preEvents {
		t.Errorf("rollback: event count went from %d → %d (should be unchanged)", preEvents, postEvents)
	}
}

// A cache backend that fails every invalidation must not fail a transaction
// that already committed: the write is durable and the cache is
// non-authoritative. The async case is a regression guard only: an async
// callback never reported its error to WithTx, and it may run after the
// failure mode is cleared.
func TestTx_CommittedWriteSurvivesInvalidationFailure(t *testing.T) {
	tests := []struct {
		name string
		mode database.CallbackMode
	}{
		{name: "sync callbacks", mode: database.CallbackSync},
		{name: "async callbacks", mode: database.CallbackAsync},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			// A breaker that never opens, so every failing call reaches the
			// backend and its error reaches the callback.
			env := newEnv(t, withBreaker(cache.NewBreaker(cache.BreakerConfig{
				FailureThreshold: 1_000, ProbeInterval: time.Hour, HalfOpenMaxProbes: 1,
			})))
			p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
				Name: "Before", SKU: uniqueSku(t, "tx-inv-fail"), Price: 1.0,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

			client := models.New(env.counter,
				models.WithEventPublisher(env.bus),
				models.WithCache(env.cache),
				models.WithCallbackMode(tt.mode),
			)
			env.backend.setFail("invalidate")
			err = client.WithTx(ctx, "inv-fail", func(ctx context.Context) error {
				_, err := client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
					Name: omittable.Set("After"),
				})
				return err
			})
			if err != nil {
				t.Fatalf("WithTx returned %v for a committed write; a cache invalidation failure must not reach the caller", err)
			}
			if tt.mode == database.CallbackSync && env.backend.failCount.Load() == 0 {
				t.Fatal("no invalidation failed, so the test proved nothing")
			}

			env.backend.setFail("")
			got, err := client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipCache = true })
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Name != "After" {
				t.Errorf("Name = %q, want %q (the write committed)", got.Name, "After")
			}
		})
	}
}

// failFirstInvalidate fails the first InvalidateMany it sees and passes every
// later call to the wrapped backend.
type failFirstInvalidate struct {
	cache.Backend
	calls   atomic.Int32
	failAll bool
}

func (f *failFirstInvalidate) InvalidateMany(ctx context.Context, keys []string) error {
	if f.calls.Add(1) == 1 || f.failAll {
		return errors.New("transient invalidate failure")
	}
	return f.Backend.InvalidateMany(ctx, keys)
}

// A commit callback that fails runs once more, and only once: a transient
// failure on the first invalidation still evicts the stale entry, and a
// persistent one costs exactly two backend calls.
func TestTx_FailedInvalidationIsRetriedOnce(t *testing.T) {
	tests := []struct {
		name      string
		failAll   bool
		wantName  string
		wantCalls int32
	}{
		{name: "transient failure", failAll: false, wantName: "After", wantCalls: 2},
		{name: "persistent failure", failAll: true, wantName: "Before", wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			inner, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
			if err != nil {
				t.Fatalf("memory.New: %v", err)
			}
			t.Cleanup(func() { _ = inner.Close() })
			flaky := &failFirstInvalidate{Backend: inner, failAll: tt.failAll}
			// A breaker that never opens, so every call reaches the backend.
			c, err := models.NewCache(flaky,
				models.WithOnError(func(context.Context, string, string, hook.TableName, error) {}),
				models.WithCircuitBreaker(cache.NewBreaker(cache.BreakerConfig{FailureThreshold: 1_000, ProbeInterval: time.Hour, HalfOpenMaxProbes: 1})),
			)
			if err != nil {
				t.Fatalf("NewCache: %v", err)
			}
			t.Cleanup(func() { _ = c.Close() })
			client := models.New(dbstdlib.New(testDB), models.WithCache(c), models.WithCallbackMode(database.CallbackSync))

			p, err := client.Products().Create(ctx, &models.CreateProductInput{
				Name: "Before", SKU: uniqueSku(t, "tx-retry"), Price: 1.0,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

			if err := client.WithTx(ctx, "retry", func(ctx context.Context) error {
				_, err := client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("After")})
				return err
			}); err != nil {
				t.Fatalf("WithTx: %v", err)
			}
			if got := flaky.calls.Load(); got != tt.wantCalls {
				t.Errorf("InvalidateMany calls = %d, want %d (the failure, then one retry)", got, tt.wantCalls)
			}
			got, err := client.Products().Get(ctx, p.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("Get served %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

// A key the mutation hook cannot build (an AffectedPKs element of the wrong
// type) involves no backend call, so it is reported through the error policy
// by the hook itself, and still never fails the write.
func TestMutation_UnbuildableKeyIsReportedNotReturned(t *testing.T) {
	tests := []struct {
		name string
		inTx bool
	}{
		{name: "outside a transaction", inTx: false},
		{name: "inside a sync transaction", inTx: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			env := newEnv(t)
			p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
				Name: "Before", SKU: uniqueSku(t, "bad-key"), Price: 1.0,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

			var reported []string
			c, err := models.NewCache(env.backend, models.WithOnError(func(_ context.Context, op, _ string, table hook.TableName, err error) {
				reported = append(reported, op+" "+string(table)+": "+err.Error())
			}))
			if err != nil {
				t.Fatalf("NewCache: %v", err)
			}
			t.Cleanup(func() { _ = c.Close() })
			// Runs inside the cache hook: after the write, it swaps each
			// affected PK for one of the wrong type.
			corrupt := hook.OnMutation(func(next hook.MutationHandler) hook.MutationHandler {
				return func(ctx context.Context, m *hook.MutationContext) (any, error) {
					res, err := next(ctx, m)
					for i, pk := range m.AffectedPKs {
						m.AffectedPKs[i] = int32(pk.(int64))
					}
					return res, err
				}
			}, hook.OpUpdate)
			client := models.New(env.counter,
				models.WithCache(c),
				models.WithMutationHook(corrupt),
				models.WithCallbackMode(database.CallbackSync),
			)

			update := func(ctx context.Context) error {
				_, err := client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("After")})
				return err
			}
			if tt.inTx {
				err = client.WithTx(ctx, "bad-key", update)
			} else {
				err = update(ctx)
			}
			if err != nil {
				t.Fatalf("write returned %v, want nil", err)
			}
			want := []string{`invalidate_many products: cache: InvalidateMany("products"): cache: pk at index 0: expected int64, got int32`}
			if !slices.Equal(reported, want) {
				t.Errorf("OnErrorFunc reports = %q, want %q", reported, want)
			}
		})
	}
}
