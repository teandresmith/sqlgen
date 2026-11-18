package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// panickingPublisher panics from PublishBatch with the given value.
type panickingPublisher struct{ value any }

func (p panickingPublisher) Publish(context.Context, event.Event) error        { panic(p.value) }
func (p panickingPublisher) PublishBatch(context.Context, []event.Event) error { panic(p.value) }
func (p panickingPublisher) Close() error                                      { return nil }

// onErrorRecorder records every OnError call.
type onErrorRecorder struct {
	mu     sync.Mutex
	errs   []error
	counts []int
}

func (r *onErrorRecorder) onError(_ context.Context, events []event.Event, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
	r.counts = append(r.counts, len(events))
}

func (r *onErrorRecorder) waitCalls(t *testing.T, n int) ([]error, []int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		if len(r.errs) >= n || time.Now().After(deadline) {
			errs, counts := append([]error(nil), r.errs...), append([]int(nil), r.counts...)
			r.mu.Unlock()
			return errs, counts
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

// A publisher that panics does not fail the mutation: the write has already
// happened, so the panic reaches OnError as an error, once, and the mutation
// returns its result — outside a transaction, and on commit in either callback
// mode.
func TestEventConfig_PanickingPublisherIsReportedNotReturned(t *testing.T) {
	ctx := context.Background()
	panicErr := errors.New("transport exploded")

	tests := []struct {
		name  string
		mode  database.CallbackMode // empty: no transaction
		value any
	}{
		{name: "outside a transaction", value: "boom"},
		{name: "outside a transaction error value", value: panicErr},
		{name: "sync commit", mode: database.CallbackSync, value: panicErr},
		{name: "async commit", mode: database.CallbackAsync, value: panicErr},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &onErrorRecorder{}
			opts := []models.ClientOption{
				models.WithEventPublisher(panickingPublisher{value: tt.value}, func(c *event.Config) { c.OnError = rec.onError }),
			}
			if tt.mode != "" {
				opts = append(opts, models.WithCallbackMode(tt.mode))
			}
			client := models.New(dbstdlib.New(testDB), opts...)
			sku := "EV-PANIC-" + string(rune('A'+i))

			var id int64
			create := func(ctx context.Context) error {
				p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "Panic", SKU: sku, Price: 1})
				if err != nil {
					return err
				}
				id = p.ID
				return nil
			}
			var err error
			if tt.mode == "" {
				err = create(ctx)
			} else {
				err = client.WithTx(ctx, "panic", create)
			}
			if err != nil {
				t.Fatalf("mutation returned %v, want nil (the write succeeded)", err)
			}
			t.Cleanup(func() { _ = client.Products().HardDelete(ctx, id) })
			if _, err := client.Products().Get(ctx, id); err != nil {
				t.Errorf("Get(%d) after the mutation: %v, want the written row", id, err)
			}

			errs, counts := rec.waitCalls(t, 1)
			time.Sleep(50 * time.Millisecond) // a second call would surface here
			errs, counts = rec.waitCalls(t, len(errs))
			if len(errs) != 1 {
				t.Fatalf("OnError calls = %d, want 1", len(errs))
			}
			if counts[0] != 1 {
				t.Errorf("OnError events = %d, want 1", counts[0])
			}
			if errs[0] == nil {
				t.Fatal("OnError err = nil, want the recovered panic")
			}
			if want, ok := tt.value.(error); ok && !errors.Is(errs[0], want) {
				t.Errorf("OnError err = %v, want it to wrap the panicked error %v", errs[0], want)
			}
		})
	}
}

// A memorybus subscriber runs inside PublishBatch, so its panic is the
// publisher's panic: the mutation still succeeds and OnError reports it.
func TestEventConfig_PanickingSubscriberDoesNotFailMutation(t *testing.T) {
	ctx := context.Background()
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(context.Context, event.Event) error {
		panic("subscriber exploded")
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	rec := &onErrorRecorder{}
	client := models.New(dbstdlib.New(testDB), models.WithEventPublisher(bus, func(c *event.Config) { c.OnError = rec.onError }))

	p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "SubPanic", SKU: "EV-SUBPANIC-1", Price: 1})
	if err != nil {
		t.Fatalf("Create returned %v, want nil", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })
	if errs, _ := rec.waitCalls(t, 1); len(errs) != 1 || errs[0] == nil {
		t.Errorf("OnError calls = %v, want one non-nil error", errs)
	}
}

// OnError runs once per batch, outside the publish recover: a publisher error
// reaches it once, and a panic OnError raises itself is not reported back to it
// as the publisher's.
func TestEventConfig_OnErrorCalledOnceWhenItPanics(t *testing.T) {
	ctx := context.Background()
	publishErr := errors.New("transport down")
	var calls []error
	client := models.New(dbstdlib.New(testDB), models.WithEventPublisher(&failingPublisher{err: publishErr}, func(c *event.Config) {
		c.OnError = func(_ context.Context, _ []event.Event, err error) {
			calls = append(calls, err)
			panic("onerror exploded")
		}
	}))

	// The OnError panic surfaces as Create's error through the client's panic
	// recovery; which way it should go is an open PRD gap, so it is not pinned.
	_, _ = client.Products().Create(ctx, &models.CreateProductInput{Name: "OnErrPanic", SKU: "EV-ONERR-PANIC-1", Price: 1})
	t.Cleanup(func() {
		sku := "EV-ONERR-PANIC-1"
		rows, _ := client.Products().GetMany(ctx, &models.GetProductsInput{Filter: &models.ProductFilter{SKU: &comparator.String{Eq: &sku}}})
		for _, r := range rows {
			_ = client.Products().HardDelete(ctx, r.ID)
		}
	})
	if len(calls) != 1 {
		t.Fatalf("OnError calls = %d (%v), want 1", len(calls), calls)
	}
	if calls[0] != publishErr { //nolint:errorlint // identity: the publisher's error is passed as-is
		t.Errorf("OnError err = %v, want the publisher's error %v", calls[0], publishErr)
	}
}
