package tests

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/event"
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

	// Swap to synchronous callback mode so OnCommit runs in-thread before we
	// read the timeline.
	syncClient := models.New(
		env.counter,
		models.WithEventPublisher(env.bus),
		models.WithCache(env.cache),
		models.WithCallbackMode(database.CallbackSync),
	)

	// Tag cache invalidations via backend spy — wrap the existing spy so
	// every invalidate_many also appends to the timeline.
	orig := env.backend
	_ = orig // keep for later

	// Run the mutation in a transaction.
	env.backend.reset()
	err = syncClient.WithTx(ctx, "order-check", func(ctx context.Context) error {
		_, err := syncClient.Products().Update(ctx, p.ID, &models.UpdateProductInput{
			Name: omittable.Set("TxOrder2"),
		})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Append a marker for every backend invalidate_many / invalidate_pattern.
	for _, c := range env.backend.snapshot() {
		if c.op == "invalidate_many" || c.op == "invalidate_pattern" {
			rec.addEntry("invalidate:products")
		}
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
