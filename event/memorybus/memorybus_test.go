package memorybus_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
)

// errHandlerBoom is a sentinel Handler error used to assert error identity
// reaches the subscribe error handler.
var errHandlerBoom = errors.New("handler boom")

func TestPublishSubscribe(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var got []event.Event
	sub, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	want := event.Event{ID: "1", Table: "products", Action: event.Create}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	if diff := cmp.Diff([]event.Event{want}, got); diff != "" {
		t.Errorf("handler events mismatch (-want +got):\n%s", diff)
	}
}

func TestPublishBatch(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var got []event.Event
	var mu sync.Mutex
	_, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	events := []event.Event{
		{ID: "1", Table: "products", Action: event.Create},
		{ID: "2", Table: "products", Action: event.Update},
		{ID: "3", Table: "products", Action: event.Delete},
	}
	if err := bus.PublishBatch(context.Background(), events); err != nil {
		t.Fatalf("PublishBatch() unexpected error: %v", err)
	}

	if diff := cmp.Diff(events, got); diff != "" {
		t.Errorf("PublishBatch() events mismatch (-want +got):\n%s", diff)
	}
}

func TestFilters(t *testing.T) {
	type record struct {
		subID int
		e     event.Event
	}

	tests := []struct {
		name       string
		subOptions []event.SubscribeOptions
		publish    []event.Event
		wantBySub  map[int][]string // sub index → list of event IDs it should receive
	}{
		{
			name: "table filter only",
			subOptions: []event.SubscribeOptions{
				{Tables: []string{"products"}},
			},
			publish: []event.Event{
				{ID: "1", Table: "products", Action: event.Create},
				{ID: "2", Table: "orders", Action: event.Create},
			},
			wantBySub: map[int][]string{0: {"1"}},
		},
		{
			name: "action filter only",
			subOptions: []event.SubscribeOptions{
				{Actions: []event.Action{event.Create}},
			},
			publish: []event.Event{
				{ID: "1", Table: "products", Action: event.Create},
				{ID: "2", Table: "products", Action: event.Update},
			},
			wantBySub: map[int][]string{0: {"1"}},
		},
		{
			name: "table and action filter",
			subOptions: []event.SubscribeOptions{
				{Tables: []string{"products"}, Actions: []event.Action{event.Delete}},
			},
			publish: []event.Event{
				{ID: "1", Table: "products", Action: event.Create},
				{ID: "2", Table: "products", Action: event.Delete},
				{ID: "3", Table: "orders", Action: event.Delete},
			},
			wantBySub: map[int][]string{0: {"2"}},
		},
		{
			name: "empty filter receives all",
			subOptions: []event.SubscribeOptions{
				{},
			},
			publish: []event.Event{
				{ID: "1", Table: "products", Action: event.Create},
				{ID: "2", Table: "orders", Action: event.Update},
			},
			wantBySub: map[int][]string{0: {"1", "2"}},
		},
		{
			name: "multiple subscribers with different filters",
			subOptions: []event.SubscribeOptions{
				{Tables: []string{"products"}},
				{Actions: []event.Action{event.Delete}},
			},
			publish: []event.Event{
				{ID: "1", Table: "products", Action: event.Create},
				{ID: "2", Table: "orders", Action: event.Delete},
				{ID: "3", Table: "products", Action: event.Delete},
			},
			wantBySub: map[int][]string{
				0: {"1", "3"},
				1: {"2", "3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := memorybus.New()
			t.Cleanup(func() { _ = bus.Close() })

			var mu sync.Mutex
			var received []record
			for i, opts := range tt.subOptions {
				_, err := bus.Subscribe(opts, func(ctx context.Context, e event.Event) error {
					mu.Lock()
					defer mu.Unlock()
					received = append(received, record{subID: i, e: e})
					return nil
				})
				if err != nil {
					t.Fatalf("Subscribe(%v) unexpected error: %v", opts, err)
				}
			}

			for _, e := range tt.publish {
				if err := bus.Publish(context.Background(), e); err != nil {
					t.Fatalf("Publish(%v) unexpected error: %v", e, err)
				}
			}

			got := map[int][]string{}
			for _, r := range received {
				got[r.subID] = append(got[r.subID], r.e.ID)
			}
			if diff := cmp.Diff(tt.wantBySub, got); diff != "" {
				t.Errorf("delivery mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConsumerGroupRoundRobin(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	counts := map[int]int{}
	for i := range 2 {
		_, err := bus.Subscribe(event.SubscribeOptions{Group: "workers"}, func(ctx context.Context, e event.Event) error {
			mu.Lock()
			defer mu.Unlock()
			counts[i]++
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
	}

	for range 10 {
		if err := bus.Publish(context.Background(), event.Event{ID: "e", Table: "products", Action: event.Create}); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	if counts[0] != 5 || counts[1] != 5 {
		t.Errorf("group distribution = %v, want {0:5, 1:5}", counts)
	}
}

func TestConsumerGroupBroadcastWhenEmpty(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	counts := map[int]int{}
	for i := range 2 {
		_, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, e event.Event) error {
			mu.Lock()
			defer mu.Unlock()
			counts[i]++
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
	}

	for range 5 {
		if err := bus.Publish(context.Background(), event.Event{ID: "e", Table: "products", Action: event.Create}); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	if counts[0] != 5 || counts[1] != 5 {
		t.Errorf("broadcast delivery = %v, want {0:5, 1:5}", counts)
	}
}

func TestConsumerGroupIndependent(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	counts := map[string]int{}
	subscribe := func(group string) {
		_, err := bus.Subscribe(event.SubscribeOptions{Group: group}, func(ctx context.Context, e event.Event) error {
			mu.Lock()
			defer mu.Unlock()
			counts[group]++
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", group, err)
		}
	}

	subscribe("group-a")
	subscribe("group-b")

	for range 3 {
		if err := bus.Publish(context.Background(), event.Event{ID: "e", Table: "products", Action: event.Create}); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	if counts["group-a"] != 3 || counts["group-b"] != 3 {
		t.Errorf("independent group counts = %v, want group-a:3, group-b:3", counts)
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var received int32
	sub, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, e event.Event) error {
		atomic.AddInt32(&received, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{ID: "1"}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}
	if err := sub.Unsubscribe(); err != nil {
		t.Fatalf("Unsubscribe() unexpected error: %v", err)
	}
	if err := bus.Publish(context.Background(), event.Event{ID: "2"}); err != nil {
		t.Fatalf("Publish() after Unsubscribe() unexpected error: %v", err)
	}

	if got := atomic.LoadInt32(&received); got != 1 {
		t.Errorf("received count after unsubscribe = %d, want 1", got)
	}

	// Double-unsubscribe is a no-op.
	if err := sub.Unsubscribe(); err != nil {
		t.Errorf("second Unsubscribe() unexpected error: %v", err)
	}
}

func TestClose(t *testing.T) {
	bus := memorybus.New()

	var received int32
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, e event.Event) error {
		atomic.AddInt32(&received, 1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{ID: "1"}); err != nil {
		t.Errorf("Publish() after Close() unexpected error: %v", err)
	}
	if err := bus.PublishBatch(context.Background(), []event.Event{{ID: "2"}}); err != nil {
		t.Errorf("PublishBatch() after Close() unexpected error: %v", err)
	}

	if got := atomic.LoadInt32(&received); got != 0 {
		t.Errorf("received count after Close = %d, want 0", got)
	}
}

func TestSubscribeErrorHandlerRoutesError(t *testing.T) {
	var gotEvent event.Event
	var gotErr error
	var calls int

	bus := memorybus.New(memorybus.WithSubscribeErrorHandler(
		func(_ context.Context, e event.Event, err error) {
			gotEvent = e
			gotErr = err
			calls++
		},
	))
	t.Cleanup(func() { _ = bus.Close() })

	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		return errHandlerBoom
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	want := event.Event{ID: "evt-err", Table: "products", Schema: "public", Action: event.Create}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// Dispatch is synchronous, so the sink has already observed the error.
	// Exactly one call — memorybus never redelivers (at-most-once).
	if calls != 1 {
		t.Errorf("error handler calls = %d, want 1", calls)
	}
	if !errors.Is(gotErr, errHandlerBoom) {
		t.Errorf("routed error = %v, want %v", gotErr, errHandlerBoom)
	}
	if diff := cmp.Diff(want, gotEvent); diff != "" {
		t.Errorf("routed event mismatch (-want +got):\n%s", diff)
	}
}

func TestDefaultFailingHandlerNoPanicNoRedeliver(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var calls int
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		calls++
		return errHandlerBoom
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	// No sink configured: the error is discarded, no panic, no redelivery —
	// byte-identical to prior behavior.
	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-default", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	if calls != 1 {
		t.Errorf("handler invocations = %d, want 1 (no redelivery)", calls)
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	const workers = 8
	const events = 200

	var received int64
	var handler event.Handler = func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&received, 1)
		return nil
	}

	// Start with a few long-lived subscribers so every publish hits at least one handler.
	for range workers {
		if _, err := bus.Subscribe(event.SubscribeOptions{}, handler); err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
	}

	var wg sync.WaitGroup
	ctx := context.Background()

	// Concurrent publishers.
	for range workers {
		wg.Go(func() {
			for range events {
				_ = bus.Publish(ctx, event.Event{ID: "x", Table: "products", Action: event.Create})
			}
		})
	}

	// Concurrent subscribe/unsubscribe churn.
	for range workers {
		wg.Go(func() {
			for range events {
				sub, err := bus.Subscribe(event.SubscribeOptions{}, handler)
				if err != nil {
					t.Errorf("Subscribe() unexpected error: %v", err)
					return
				}
				_ = sub.Unsubscribe()
			}
		})
	}

	wg.Wait()

	// Every publish hits at least the `workers` stable subscribers.
	if got := atomic.LoadInt64(&received); got < int64(workers*events*workers) {
		t.Errorf("received = %d, want at least %d", got, workers*events*workers)
	}
}
