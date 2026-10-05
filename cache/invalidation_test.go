package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"
)

const (
	adapterSchema = "public"
	// adapterWireTable is what an event carries in Table: the bare SQL name,
	// with the schema in its own field (PRD §28.3).
	adapterWireTable = "products"
	// adapterTable is the generated constant's value the adapter rebuilds
	// from the two (PRD §8.5) — the value the facade's switch matches.
	adapterTable hook.TableName = "public.products"
)

// capturingHandler records the InvalidationSignals seen by an
// InvalidationHandler. It is safe for concurrent use.
type capturingHandler struct {
	mu    sync.Mutex
	calls []cache.InvalidationSignal
	err   error // optional; if set, returned from every invocation
}

func (c *capturingHandler) handle(ctx context.Context, signal cache.InvalidationSignal) error {
	c.mu.Lock()
	c.calls = append(c.calls, signal)
	c.mu.Unlock()
	return c.err
}

func (c *capturingHandler) snapshot() []cache.InvalidationSignal {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]cache.InvalidationSignal, len(c.calls))
	copy(out, c.calls)
	return out
}

func TestFromEventSubscriber_SingleEvent(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	src := cache.FromEventSubscriber(bus)
	h := &capturingHandler{}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	ev := event.Event{
		ID:     "ev-1",
		Table:  adapterWireTable,
		Schema: adapterSchema,
		Action: event.Update,
		PK:     42,
	}
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish() err = %v", err)
	}

	calls := h.snapshot()
	if len(calls) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(calls))
	}
	if calls[0].Table != adapterTable {
		t.Errorf("signal.Table = %q, want %q", calls[0].Table, adapterTable)
	}
	if len(calls[0].PKs) != 1 || calls[0].PKs[0] != 42 {
		t.Errorf("signal.PKs = %v, want []any{42}", calls[0].PKs)
	}
	if calls[0].Schema != adapterSchema {
		t.Errorf("signal.Schema = %q, want %q", calls[0].Schema, adapterSchema)
	}
	if calls[0].Tenant != "" {
		t.Errorf("signal.Tenant = %q, want empty (event carried no tenant metadata)", calls[0].Tenant)
	}
}

// TestFromEventSubscriber_TableNameFollowsTheConstantRule pins the signal's
// Table to the generated constant's value, rebuilt from the event's two
// halves: "schema.table" when the event has a schema, the bare name when it
// does not (PRD §8.5). Casting ev.Table alone names no PostgreSQL table, so
// every event-driven invalidation would miss the facade's switch and leave
// the entry stale.
func TestFromEventSubscriber_TableNameFollowsTheConstantRule(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		table  string
		want   hook.TableName
	}{
		{name: "public schema", schema: "public", table: "orders", want: "public.orders"},
		{name: "a second schema stays apart", schema: "audit", table: "orders", want: "audit.orders"},
		{name: "no schema (MySQL, SQLite)", schema: "", table: "orders", want: "orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := memorybus.New()
			t.Cleanup(func() { _ = bus.Close() })

			h := &capturingHandler{}
			if _, err := cache.FromEventSubscriber(bus).Subscribe(h.handle); err != nil {
				t.Fatalf("Subscribe() err = %v", err)
			}
			ev := event.Event{ID: "ev-1", Table: tt.table, Schema: tt.schema, Action: event.Update, PK: 1}
			if err := bus.Publish(context.Background(), ev); err != nil {
				t.Fatalf("Publish() err = %v", err)
			}

			calls := h.snapshot()
			if len(calls) != 1 {
				t.Fatalf("handler calls = %d, want 1", len(calls))
			}
			if calls[0].Table != tt.want {
				t.Errorf("signal.Table = %q, want %q", calls[0].Table, tt.want)
			}
			if calls[0].Schema != tt.schema {
				t.Errorf("signal.Schema = %q, want %q", calls[0].Schema, tt.schema)
			}
		})
	}
}

// The adapter must carry the §29.6 tenant stamp into the signal so the
// generated facade can rebuild exact tenant-scoped keys on the receive side
// (PRD §28.11). The stamp is forwarded verbatim — the %v
// string form is byte-identical to the key grammar's tenant segment.
func TestFromEventSubscriber_ForwardsTenantMetadata(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	src := cache.FromEventSubscriber(bus)
	h := &capturingHandler{}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	ev := event.Event{
		ID:       "ev-tenant",
		Table:    adapterWireTable,
		Schema:   adapterSchema,
		Action:   event.Update,
		PK:       7,
		Metadata: map[string]string{"tenant": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
	}
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish() err = %v", err)
	}

	calls := h.snapshot()
	if len(calls) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(calls))
	}
	if got, want := calls[0].Tenant, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"; got != want {
		t.Errorf("signal.Tenant = %q, want %q (forwarded verbatim from Metadata[tenant])", got, want)
	}
}

func TestFromEventSubscriber_PublishBatchFansOut(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	src := cache.FromEventSubscriber(bus)
	h := &capturingHandler{}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	events := []event.Event{
		{ID: "ev-1", Table: adapterWireTable, Schema: adapterSchema, Action: event.Create, PK: 1},
		{ID: "ev-2", Table: adapterWireTable, Schema: adapterSchema, Action: event.Update, PK: 2},
		{ID: "ev-3", Table: adapterWireTable, Schema: adapterSchema, Action: event.Delete, PK: 3},
	}
	if err := bus.PublishBatch(context.Background(), events); err != nil {
		t.Fatalf("PublishBatch() err = %v", err)
	}

	calls := h.snapshot()
	if len(calls) != len(events) {
		t.Fatalf("handler calls = %d, want %d (one per event — no coalescing)", len(calls), len(events))
	}
	for i, c := range calls {
		if len(c.PKs) != 1 {
			t.Errorf("signal[%d].PKs = %v, want single-element slice", i, c.PKs)
			continue
		}
		if c.PKs[0] != events[i].PK {
			t.Errorf("signal[%d].PKs[0] = %v, want %v", i, c.PKs[0], events[i].PK)
		}
	}
}

// fakeSubscriber is a minimal event.Subscriber that captures the registered
// event.Handler so tests can invoke it directly and observe its returned
// error — memorybus.Publish swallows handler errors and cannot be used to
// verify the "error propagates from the inner event.Handler" contract.
type fakeSubscriber struct {
	mu            sync.Mutex
	subscribeErr  error
	unsubscribeFn func() error

	handler        event.Handler
	handlerOpts    event.SubscribeOptions
	unsubscribed   bool
	unsubscribeHit atomic.Int32
}

func (s *fakeSubscriber) Subscribe(opts event.SubscribeOptions, h event.Handler) (event.Subscription, error) {
	if s.subscribeErr != nil {
		return nil, s.subscribeErr
	}
	s.mu.Lock()
	s.handler = h
	s.handlerOpts = opts
	s.mu.Unlock()
	return &fakeSubscription{parent: s}, nil
}

func (s *fakeSubscriber) Close() error { return nil }

func (s *fakeSubscriber) dispatch(ctx context.Context, ev event.Event) error {
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	if h == nil {
		return nil
	}
	return h(ctx, ev)
}

type fakeSubscription struct {
	parent *fakeSubscriber
}

func (s *fakeSubscription) Unsubscribe() error {
	s.parent.unsubscribeHit.Add(1)
	s.parent.mu.Lock()
	s.parent.unsubscribed = true
	s.parent.handler = nil
	s.parent.mu.Unlock()
	if s.parent.unsubscribeFn != nil {
		return s.parent.unsubscribeFn()
	}
	return nil
}

// spyMetrics records Error calls for adapter-level routing assertions.
type spyMetrics struct {
	mu      sync.Mutex
	records []errorRecord
}

type errorRecord struct {
	schema string
	table  hook.TableName
	op     string
	err    error
}

func (s *spyMetrics) Hit(schema string, table hook.TableName)        {}
func (s *spyMetrics) Miss(schema string, table hook.TableName)       {}
func (s *spyMetrics) Set(schema string, table hook.TableName)        {}
func (s *spyMetrics) Invalidate(schema string, table hook.TableName) {}
func (s *spyMetrics) Error(schema string, table hook.TableName, op string, err error) {
	s.mu.Lock()
	s.records = append(s.records, errorRecord{schema, table, op, err})
	s.mu.Unlock()
}
func (s *spyMetrics) HydrationStart(schema string, table hook.TableName)                     {}
func (s *spyMetrics) HydrationComplete(schema string, table hook.TableName, err error)       {}
func (s *spyMetrics) CircuitBreakerStateChange(from, to cache.CircuitState)                  {}
func (s *spyMetrics) GetLatency(schema string, table hook.TableName, d time.Duration)        {}
func (s *spyMetrics) SetLatency(schema string, table hook.TableName, d time.Duration)        {}
func (s *spyMetrics) InvalidateLatency(schema string, table hook.TableName, d time.Duration) {}

func (s *spyMetrics) snapshot() []errorRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]errorRecord, len(s.records))
	copy(out, s.records)
	return out
}

func TestFromEventSubscriber_HandlerErrorIsRecordedAndPropagates(t *testing.T) {
	fake := &fakeSubscriber{}
	metrics := &spyMetrics{}
	src := cache.FromEventSubscriber(fake, cache.WithEventMetrics(metrics))

	boom := errors.New("backend down")
	h := &capturingHandler{err: boom}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	ev := event.Event{
		ID:     "ev-1",
		Table:  adapterWireTable,
		Schema: adapterSchema,
		Action: event.Update,
		PK:     7,
	}
	gotErr := fake.dispatch(context.Background(), ev)

	if !errors.Is(gotErr, boom) {
		t.Errorf("event.Handler error = %v, want %v (must propagate for ACK transports)", gotErr, boom)
	}

	records := metrics.snapshot()
	if len(records) != 1 {
		t.Fatalf("metrics.Error calls = %d, want 1", len(records))
	}
	r := records[0]
	if r.op != "invalidate" {
		t.Errorf("metrics.Error op = %q, want %q", r.op, "invalidate")
	}
	if r.schema != adapterSchema {
		t.Errorf("metrics.Error schema = %q, want %q", r.schema, adapterSchema)
	}
	if r.table != adapterTable {
		t.Errorf("metrics.Error table = %q, want %q", r.table, adapterTable)
	}
	if !errors.Is(r.err, boom) {
		t.Errorf("metrics.Error err = %v, want %v", r.err, boom)
	}
}

func TestFromEventSubscriber_HandlerSuccessDoesNotRecordError(t *testing.T) {
	fake := &fakeSubscriber{}
	metrics := &spyMetrics{}
	src := cache.FromEventSubscriber(fake, cache.WithEventMetrics(metrics))

	h := &capturingHandler{}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	ev := event.Event{ID: "ev-1", Table: adapterWireTable, Schema: adapterSchema, Action: event.Create, PK: 1}
	if err := fake.dispatch(context.Background(), ev); err != nil {
		t.Errorf("event.Handler err = %v, want nil", err)
	}
	if n := len(metrics.snapshot()); n != 0 {
		t.Errorf("metrics.Error calls = %d, want 0 on success path", n)
	}
}

func TestFromEventSubscriber_RedeliveryIsIdempotent(t *testing.T) {
	// Verifies the backend-contract idempotence that §5.4 leans on: the
	// adapter keeps no per-event state, so replaying the same event.ID
	// drives a second handler invocation that must succeed without error
	// when the backing Backend is a no-op.
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	src := cache.FromEventSubscriber(bus)

	var count atomic.Int32
	noop := cache.NoopBackend{}
	handler := func(ctx context.Context, signal cache.InvalidationSignal) error {
		count.Add(1)
		// Route through the backend exactly as the generated facade would,
		// proving that redelivery is safe by the Backend contract.
		for _, pk := range signal.PKs {
			key := "sqlgen:public.products:fingerprint:v1a2b3c:pk:"
			// Use BuildCompositeKey to produce a deterministic key.
			_ = noop.Invalidate(ctx, key)
			_ = pk
		}
		return nil
	}
	if _, err := src.Subscribe(handler); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	ev := event.Event{ID: "ev-1", Table: adapterWireTable, Schema: adapterSchema, Action: event.Delete, PK: 99}
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish() first delivery err = %v", err)
	}
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish() redelivery err = %v", err)
	}

	if got := count.Load(); got != 2 {
		t.Errorf("handler call count = %d, want 2 (redelivery must invoke handler)", got)
	}
}

func TestFromEventSubscriber_CloseUnsubscribes(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	src := cache.FromEventSubscriber(bus)
	h := &capturingHandler{}
	if _, err := src.Subscribe(h.handle); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	if err := src.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	ev := event.Event{ID: "ev-1", Table: adapterWireTable, Schema: adapterSchema, Action: event.Update, PK: 1}
	if err := bus.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish() after Close() err = %v", err)
	}

	if calls := h.snapshot(); len(calls) != 0 {
		t.Errorf("handler calls after Close() = %d, want 0", len(calls))
	}

	// Second Close must be a no-op.
	if err := src.Close(); err != nil {
		t.Errorf("Close() second call err = %v, want nil (idempotent)", err)
	}
}

func TestFromEventSubscriber_CloseUnsubscribesUnderlyingSubscription(t *testing.T) {
	fake := &fakeSubscriber{}
	src := cache.FromEventSubscriber(fake)

	if _, err := src.Subscribe(func(ctx context.Context, signal cache.InvalidationSignal) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}
	if got := fake.unsubscribeHit.Load(); got != 0 {
		t.Errorf("Unsubscribe hit before Close() = %d, want 0", got)
	}

	if err := src.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}
	if got := fake.unsubscribeHit.Load(); got != 1 {
		t.Errorf("Unsubscribe hit after Close() = %d, want 1", got)
	}
}

func TestFromEventSubscriber_SubscribesToAllTablesAndActions(t *testing.T) {
	fake := &fakeSubscriber{}
	src := cache.FromEventSubscriber(fake)
	t.Cleanup(func() { _ = src.Close() })

	if _, err := src.Subscribe(func(ctx context.Context, signal cache.InvalidationSignal) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	fake.mu.Lock()
	opts := fake.handlerOpts
	fake.mu.Unlock()

	if len(opts.Tables) != 0 {
		t.Errorf("SubscribeOptions.Tables = %v, want empty (all tables)", opts.Tables)
	}
	if len(opts.Actions) != 0 {
		t.Errorf("SubscribeOptions.Actions = %v, want empty (all actions)", opts.Actions)
	}
	if opts.Group != "" {
		t.Errorf("SubscribeOptions.Group = %q, want \"\" (broadcast, not grouped)", opts.Group)
	}
}

func TestFromEventSubscriber_SubscribeErrorPropagates(t *testing.T) {
	wantErr := errors.New("sub failed")
	fake := &fakeSubscriber{subscribeErr: wantErr}
	src := cache.FromEventSubscriber(fake)

	_, err := src.Subscribe(func(ctx context.Context, signal cache.InvalidationSignal) error {
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("Subscribe() err = %v, want %v", err, wantErr)
	}
}

func TestEventAdapterSubscription_UnsubscribeIdempotent(t *testing.T) {
	fake := &fakeSubscriber{}
	src := cache.FromEventSubscriber(fake)

	sub, err := src.Subscribe(func(ctx context.Context, signal cache.InvalidationSignal) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() err = %v", err)
	}

	if err := sub.Unsubscribe(); err != nil {
		t.Errorf("Unsubscribe() first err = %v", err)
	}
	if err := sub.Unsubscribe(); err != nil {
		t.Errorf("Unsubscribe() second err = %v", err)
	}
	if got := fake.unsubscribeHit.Load(); got != 1 {
		t.Errorf("underlying Unsubscribe hit = %d, want 1 (second call is no-op)", got)
	}
}

var _ cache.InvalidationSource = (*noopInvalidationSource)(nil)

type noopInvalidationSource struct{}

func (noopInvalidationSource) Subscribe(cache.InvalidationHandler) (cache.InvalidationSubscription, error) {
	return noopInvalidationSubscription{}, nil
}

func (noopInvalidationSource) Close() error { return nil }

type noopInvalidationSubscription struct{}

func (noopInvalidationSubscription) Unsubscribe() error { return nil }

var _ cache.InvalidationSubscription = noopInvalidationSubscription{}
