package tests

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// SkipEvents suppresses all event publishing for a single mutation regardless
// of how many entities are affected.
func TestEventConfig_SkipEvents(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{})

	// Create with SkipEvents — no event expected.
	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Skip", SKU: "EV-SKIP-1", Price: 1,
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipEvents = true
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	// Sanity: a follow-up mutation WITHOUT SkipEvents does fire an event. This
	// guards against false positives (e.g., subscription not wired up).
	if _, err := client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
		Name: omittable.Set("Skip2"),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := got.snapshot()
	// Only the Update should have been published (SkipEvents suppressed the Create).
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (Create suppressed by SkipEvents)", len(events))
	}
	if events[0].Action != event.Update {
		t.Errorf("Action = %q, want Update", events[0].Action)
	}
}

// Per-table events.enabled: false in sqlgen.yml prevents the audit_logs hook
// from being registered at all, so no events fire for that table.
func TestEventConfig_PerTableDisabled(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{})

	al, err := client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{Message: "audit"})
	if err != nil {
		t.Fatalf("Create audit_log: %v", err)
	}
	t.Cleanup(func() { _ = client.AuditLogs().HardDelete(ctx, al.ID) })

	if n := len(got.snapshot()); n != 0 {
		t.Errorf("per-table disabled: got %d events, want 0", n)
	}

	// Products still produce events — confirms the bus is wired and the
	// override is scoped only to audit_logs.
	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "N", SKU: "EV-PTD-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("after Product Create: got %d events, want 1", len(events))
	}
	if events[0].Table != "products" {
		t.Errorf("Table = %q, want products", events[0].Table)
	}
}

// failingPublisher always returns an error from PublishBatch so we can verify
// the OnError callback is invoked and that the mutation still succeeds.
type failingPublisher struct {
	err error
}

func (p *failingPublisher) Publish(_ context.Context, _ event.Event) error {
	return p.err
}

func (p *failingPublisher) PublishBatch(_ context.Context, _ []event.Event) error {
	return p.err
}

func (p *failingPublisher) Close() error { return nil }

func TestEventConfig_OnErrorCallback(t *testing.T) {
	ctx := context.Background()
	publishErr := errors.New("transport down")

	var callbackMu sync.Mutex
	var callbackEvents []event.Event
	var callbackErr error
	onError := func(_ context.Context, events []event.Event, err error) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackEvents = append(callbackEvents, events...)
		callbackErr = err
	}

	client := models.New(
		dbstdlib.New(testDB),
		models.WithEventPublisher(&failingPublisher{err: publishErr}, func(c *event.Config) {
			c.OnError = onError
		}),
	)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "OnErr", SKU: "EV-ONERR-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create should succeed despite publish failure: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	callbackMu.Lock()
	defer callbackMu.Unlock()
	if !errors.Is(callbackErr, publishErr) {
		t.Errorf("OnError err = %v, want %v", callbackErr, publishErr)
	}
	if len(callbackEvents) != 1 {
		t.Fatalf("OnError events = %d, want 1", len(callbackEvents))
	}
	if callbackEvents[0].PK != p.ID {
		t.Errorf("OnError event PK = %v, want %v", callbackEvents[0].PK, p.ID)
	}
}

// The event hook is prepended to the mutation hook chain (outermost position),
// so it observes the result AFTER any user-registered WithMutationHook has run.
func TestEventConfig_EventHookOutermost(t *testing.T) {
	ctx := context.Background()

	var order atomic.Int32
	var userHookOrder atomic.Int32
	var eventHookOrder atomic.Int32

	// User mutation hook: records order once the terminal completes, so later
	// we can compare against the event hook's publish order.
	userHook := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, mc *hook.MutationContext) (any, error) {
			result, err := next(ctx, mc)
			userHookOrder.Store(order.Add(1))
			return result, err
		}
	}

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	sub, err := bus.Subscribe(event.SubscribeOptions{Actions: []event.Action{event.Create}}, func(_ context.Context, _ event.Event) error {
		eventHookOrder.Store(order.Add(1))
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	client := models.New(
		dbstdlib.New(testDB),
		models.WithEventPublisher(bus),
		models.WithMutationHook(userHook),
	)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Order", SKU: "EV-ORDER-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	uh := userHookOrder.Load()
	eh := eventHookOrder.Load()
	if uh == 0 {
		t.Fatalf("user hook never ran")
	}
	if eh == 0 {
		t.Fatalf("event hook never fired")
	}
	// The event hook is outermost: it finishes AFTER the user hook
	// completes (the user hook's next() returns first, then the event
	// hook's post-logic publishes). Expect eh > uh.
	if eh <= uh {
		t.Errorf("event hook order %d should be > user hook order %d (event hook outermost, runs last)", eh, uh)
	}
}
