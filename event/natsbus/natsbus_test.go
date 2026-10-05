package natsbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/natsbus"
)

// startServer boots an in-process NATS server on a random port and returns
// a connected client. The server and connection are closed via t.Cleanup.
func startServer(t *testing.T) *nats.Conn {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = server.RANDOM_PORT
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats.Connect() unexpected error: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// waitFor polls fn until it returns true or the timeout elapses. It is used
// to synchronize on asynchronous delivery from the NATS server.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestPublishSubscribe(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []event.Event
	sub, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	want := event.Event{
		ID:        "evt-1",
		Table:     "products",
		Schema:    "public",
		Action:    event.Create,
		PK:        "42",
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]event.Event{want}, got); diff != "" {
		t.Errorf("handler events mismatch (-want +got):\n%s", diff)
	}
}

func TestPublishBatch(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []string
	_, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	events := []event.Event{
		{ID: "1", Table: "products", Schema: "public", Action: event.Create},
		{ID: "2", Table: "products", Schema: "public", Action: event.Update},
		{ID: "3", Table: "products", Schema: "public", Action: event.Delete},
	}
	if err := bus.PublishBatch(context.Background(), events); err != nil {
		t.Fatalf("PublishBatch() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == len(events)
	})

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]string{"1", "2", "3"}, got); diff != "" {
		t.Errorf("PublishBatch() IDs mismatch (-want +got):\n%s", diff)
	}
}

func TestSubjectPattern(t *testing.T) {
	tests := []struct {
		name        string
		opts        []natsbus.Option
		schema      string
		table       string
		wantSubject string
	}{
		{
			name:        "default prefix with schema",
			schema:      "public",
			table:       "products",
			wantSubject: "sqlgen.events.public.products",
		},
		{
			name:        "default prefix without schema",
			schema:      "",
			table:       "products",
			wantSubject: "sqlgen.events.products",
		},
		{
			name:        "custom prefix with schema",
			opts:        []natsbus.Option{natsbus.WithPrefix("myapp.events")},
			schema:      "public",
			table:       "products",
			wantSubject: "myapp.events.public.products",
		},
		{
			name:        "custom prefix without schema",
			opts:        []natsbus.Option{natsbus.WithPrefix("myapp.events")},
			schema:      "",
			table:       "products",
			wantSubject: "myapp.events.products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := startServer(t)
			bus := natsbus.New(conn, tt.opts...)
			t.Cleanup(func() { _ = bus.Close() })

			msgCh := make(chan *nats.Msg, 1)
			rawSub, err := conn.ChanSubscribe(tt.wantSubject, msgCh)
			if err != nil {
				t.Fatalf("ChanSubscribe(%q) unexpected error: %v", tt.wantSubject, err)
			}
			t.Cleanup(func() { _ = rawSub.Unsubscribe() })

			e := event.Event{ID: "x", Table: tt.table, Schema: tt.schema, Action: event.Create}
			if err := bus.Publish(context.Background(), e); err != nil {
				t.Fatalf("Publish() unexpected error: %v", err)
			}

			select {
			case msg := <-msgCh:
				if msg.Subject != tt.wantSubject {
					t.Errorf("received subject = %q, want %q", msg.Subject, tt.wantSubject)
				}
			case <-time.After(time.Second):
				t.Fatalf("no message received on %q", tt.wantSubject)
			}
		})
	}
}

func TestQueueGroupDistributes(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var counts [2]int64
	for i := range 2 {
		idx := i
		_, err := bus.Subscribe(event.SubscribeOptions{Group: "workers"}, func(_ context.Context, _ event.Event) error {
			atomic.AddInt64(&counts[idx], 1)
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
	}

	const total = 20
	for range total {
		if err := bus.Publish(context.Background(), event.Event{ID: "x", Table: "products", Schema: "public", Action: event.Create}); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt64(&counts[0])+atomic.LoadInt64(&counts[1]) == total
	})

	got0 := atomic.LoadInt64(&counts[0])
	got1 := atomic.LoadInt64(&counts[1])
	if got0+got1 != total {
		t.Errorf("queue group total = %d, want %d", got0+got1, total)
	}
	if got0 == 0 || got1 == 0 {
		t.Errorf("queue group distribution = {0: %d, 1: %d}, want both > 0", got0, got1)
	}
}

func TestBroadcastWhenNoGroup(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var counts [2]int64
	for i := range 2 {
		idx := i
		_, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
			atomic.AddInt64(&counts[idx], 1)
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
	}

	const total = 5
	for range total {
		if err := bus.Publish(context.Background(), event.Event{ID: "x", Table: "products", Schema: "public", Action: event.Create}); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	waitFor(t, 2*time.Second, func() bool {
		return atomic.LoadInt64(&counts[0]) == total && atomic.LoadInt64(&counts[1]) == total
	})
}

func TestTableAndActionFilter(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []string
	_, err := bus.Subscribe(event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	events := []event.Event{
		{ID: "1", Table: "products", Schema: "public", Action: event.Create},
		{ID: "2", Table: "products", Schema: "public", Action: event.Update},
		{ID: "3", Table: "orders", Schema: "public", Action: event.Create},
	}
	for _, e := range events {
		if err := bus.Publish(context.Background(), e); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", e.ID, err)
		}
	}

	// Allow time for all events to propagate; only "1" should be delivered.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]string{"1"}, got); diff != "" {
		t.Errorf("filtered delivery mismatch (-want +got):\n%s", diff)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	type payload struct {
		Name  string `json:"name"`
		Price int    `json:"price"`
	}

	var mu sync.Mutex
	var got event.Event
	var received bool
	_, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = e
		received = true
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	ts := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	want := event.Event{
		ID:        "evt-abc",
		Table:     "products",
		Schema:    "public",
		Action:    event.Update,
		PK:        "42",
		Input:     map[string]any{"name": "widget", "price": float64(100)},
		Timestamp: ts,
		Metadata:  map[string]string{"user": "alice"},
	}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return received
	})

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round-trip event mismatch (-want +got):\n%s", diff)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var received int64
	sub, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&received, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{ID: "1", Table: "products", Schema: "public", Action: event.Create}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}
	waitFor(t, time.Second, func() bool { return atomic.LoadInt64(&received) == 1 })

	if err := sub.Unsubscribe(); err != nil {
		t.Fatalf("Unsubscribe() unexpected error: %v", err)
	}
	if err := bus.Publish(context.Background(), event.Event{ID: "2", Table: "products", Schema: "public", Action: event.Create}); err != nil {
		t.Fatalf("Publish() after Unsubscribe() unexpected error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt64(&received); got != 1 {
		t.Errorf("received after unsubscribe = %d, want 1", got)
	}

	// Double-unsubscribe is a no-op.
	if err := sub.Unsubscribe(); err != nil {
		t.Errorf("second Unsubscribe() unexpected error: %v", err)
	}
}

func TestCloseDrainsAndRejectsPublish(t *testing.T) {
	bus := natsbus.New(startServer(t))

	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}
	if err := bus.Publish(context.Background(), event.Event{ID: "after-close"}); err == nil {
		t.Errorf("Publish() after Close() = nil, want error")
	}
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error { return nil }); err == nil {
		t.Errorf("Subscribe() after Close() = nil, want error")
	}
	// Second Close is a no-op.
	if err := bus.Close(); err != nil {
		t.Errorf("second Close() unexpected error: %v", err)
	}
}

// errHandlerBoom is a sentinel handler error used to assert error identity
// reaches the subscribe error handler.
var errHandlerBoom = errors.New("handler boom")

func TestSubscribeErrorHandlerRoutesError(t *testing.T) {
	var mu sync.Mutex
	var gotEvent event.Event
	var gotErr error
	var calls int

	bus := natsbus.New(startServer(t), natsbus.WithSubscribeErrorHandler(
		func(_ context.Context, e event.Event, err error) {
			mu.Lock()
			defer mu.Unlock()
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

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	})

	// Settle: confirm the handler error routes exactly once (core does not redeliver).
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
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

func TestRetry(t *testing.T) {
	tests := []struct {
		name           string
		attempts       int // WithRetry n
		failCount      int // handler returns an error on its first failCount invocations
		wantHandler    int64
		wantErrHandler int64
	}{
		{
			// Fails n-1 times then succeeds → no error-handler call.
			name:           "succeeds before exhausting retries",
			attempts:       3,
			failCount:      2,
			wantHandler:    3,
			wantErrHandler: 0,
		},
		{
			// Fails all n times → exactly one error-handler call after the retries.
			name:           "exhausts retries then routes once",
			attempts:       3,
			failCount:      3,
			wantHandler:    3,
			wantErrHandler: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handlerCalls, errHandlerCalls int64

			bus := natsbus.New(startServer(t),
				natsbus.WithRetry(tt.attempts, time.Millisecond),
				natsbus.WithSubscribeErrorHandler(func(_ context.Context, _ event.Event, _ error) {
					atomic.AddInt64(&errHandlerCalls, 1)
				}))
			t.Cleanup(func() { _ = bus.Close() })

			if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
				if atomic.AddInt64(&handlerCalls, 1) <= int64(tt.failCount) {
					return errHandlerBoom
				}
				return nil
			}); err != nil {
				t.Fatalf("Subscribe() unexpected error: %v", err)
			}

			if err := bus.Publish(context.Background(), event.Event{
				ID: "evt-retry", Table: "products", Schema: "public", Action: event.Create,
			}); err != nil {
				t.Fatalf("Publish() unexpected error: %v", err)
			}

			waitFor(t, 2*time.Second, func() bool {
				return atomic.LoadInt64(&handlerCalls) == tt.wantHandler
			})
			// Settle: ensure no extra deliveries or error-handler calls arrive.
			time.Sleep(100 * time.Millisecond)

			if got := atomic.LoadInt64(&handlerCalls); got != tt.wantHandler {
				t.Errorf("handler invocations = %d, want %d", got, tt.wantHandler)
			}
			if got := atomic.LoadInt64(&errHandlerCalls); got != tt.wantErrHandler {
				t.Errorf("error handler calls = %d, want %d", got, tt.wantErrHandler)
			}
		})
	}
}

func TestPublishStampsEnvelopeVersion(t *testing.T) {
	conn := startServer(t)
	bus := natsbus.New(conn)
	t.Cleanup(func() { _ = bus.Close() })

	const subject = "sqlgen.events.public.products"
	msgCh := make(chan *nats.Msg, 1)
	rawSub, err := conn.ChanSubscribe(subject, msgCh)
	if err != nil {
		t.Fatalf("ChanSubscribe(%q) unexpected error: %v", subject, err)
	}
	t.Cleanup(func() { _ = rawSub.Unsubscribe() })

	want := event.Event{ID: "evt-hdr", Table: "products", Schema: "public", Action: event.Create, PK: "7"}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	select {
	case msg := <-msgCh:
		// The wire strings are asserted literally so this test pins the wire
		// contract independently of the (unexported) natsbus constants.
		if got := msg.Header.Get("Sqlgen-Envelope-Version"); got != "1" {
			t.Errorf("Sqlgen-Envelope-Version header = %q, want %q", got, "1")
		}
		// The JSON body is unchanged by the header addition.
		var got event.Event
		if err := json.Unmarshal(msg.Data, &got); err != nil {
			t.Fatalf("Unmarshal(body) unexpected error: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("body decode mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(time.Second):
		t.Fatalf("no message received on %q", subject)
	}
}

func TestSubscribeHeaderTolerance(t *testing.T) {
	const subject = "sqlgen.events.public.products"
	want := event.Event{ID: "evt-tol", Table: "products", Schema: "public", Action: event.Update, PK: "9"}

	tests := []struct {
		name    string
		publish func(conn *nats.Conn, data []byte) error
	}{
		{
			// Forward-compat: a message carrying headers this consumer does not
			// know about still decodes.
			name: "unknown extra headers ignored",
			publish: func(conn *nats.Conn, data []byte) error {
				return conn.PublishMsg(&nats.Msg{
					Subject: subject,
					Data:    data,
					Header: nats.Header{
						"Sqlgen-Envelope-Version": {"1"},
						"X-Future-Header":         {"whatever"},
					},
				})
			},
		},
		{
			// Backward-compat: a pre-header publisher sends no headers at all.
			name: "absent headers (old publisher)",
			publish: func(conn *nats.Conn, data []byte) error {
				return conn.Publish(subject, data)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := startServer(t)
			bus := natsbus.New(conn)
			t.Cleanup(func() { _ = bus.Close() })

			var mu sync.Mutex
			var got event.Event
			var received bool
			if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
				mu.Lock()
				defer mu.Unlock()
				got = e
				received = true
				return nil
			}); err != nil {
				t.Fatalf("Subscribe() unexpected error: %v", err)
			}

			data, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("Marshal() unexpected error: %v", err)
			}
			if err := tt.publish(conn, data); err != nil {
				t.Fatalf("publish unexpected error: %v", err)
			}

			waitFor(t, time.Second, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return received
			})

			mu.Lock()
			defer mu.Unlock()
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("decoded event mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// traceCtxKey is the context key a fakePropagator uses to carry the extracted
// traceparent value. A real adapter would carry an OTel span context here; the
// fake carries the raw string so the test can assert the header→ctx rebuild
// without pulling in a tracing library.
type traceCtxKey struct{}

// fakePropagator is a natsbus.Propagator that round-trips the "traceparent"
// header through a context value. It stands in for a consumer's real OTel
// adapter so the transport-level continuity can be asserted without an OTel
// dependency.
type fakePropagator struct{}

func (fakePropagator) Inject(ctx context.Context, h nats.Header) {
	if tp, ok := ctx.Value(traceCtxKey{}).(string); ok && tp != "" {
		h.Set("traceparent", tp)
	}
}

func (fakePropagator) Extract(parent context.Context, h nats.Header) context.Context {
	if tp := h.Get("traceparent"); tp != "" {
		return context.WithValue(parent, traceCtxKey{}, tp)
	}
	return parent
}

func TestContextPropagationContinuesSpan(t *testing.T) {
	bus := natsbus.New(startServer(t), natsbus.WithContextPropagation(fakePropagator{}))
	t.Cleanup(func() { _ = bus.Close() })

	const traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	var mu sync.Mutex
	var gotTrace any
	var received bool
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, _ event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		gotTrace = ctx.Value(traceCtxKey{})
		received = true
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	// The producer's MetadataFunc would set this at hook entry; the transport
	// only sees it as an ordinary Metadata key that it lifts to a header.
	want := event.Event{
		ID: "evt-trace", Table: "products", Schema: "public", Action: event.Create,
		Metadata: map[string]string{"traceparent": traceparent},
	}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return received
	})

	mu.Lock()
	defer mu.Unlock()
	// The handler ctx continues the producing span: it carries the same
	// traceparent the producer emitted, rebuilt from the wire header across the
	// async hop (one connected trace, not two unrelated ones).
	if gotTrace != traceparent {
		t.Errorf("handler ctx traceparent = %v, want %q", gotTrace, traceparent)
	}
}

func TestContextPropagationHeaderOnWire(t *testing.T) {
	conn := startServer(t)
	bus := natsbus.New(conn)
	t.Cleanup(func() { _ = bus.Close() })

	const (
		subject     = "sqlgen.events.public.products"
		traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	)
	msgCh := make(chan *nats.Msg, 1)
	rawSub, err := conn.ChanSubscribe(subject, msgCh)
	if err != nil {
		t.Fatalf("ChanSubscribe(%q) unexpected error: %v", subject, err)
	}
	t.Cleanup(func() { _ = rawSub.Unsubscribe() })

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-tp", Table: "products", Schema: "public", Action: event.Create,
		Metadata: map[string]string{"traceparent": traceparent},
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	select {
	case msg := <-msgCh:
		// Literal wire string, independent of the unexported natsbus const.
		if got := msg.Header.Get("traceparent"); got != traceparent {
			t.Errorf("traceparent header = %q, want %q", got, traceparent)
		}
	case <-time.After(time.Second):
		t.Fatalf("no message received on %q", subject)
	}
}

func TestNoPropagatorDefaultCtx(t *testing.T) {
	// A traceparent header is on the wire but no propagator is configured: the
	// handler runs on the default ctx (no trace value), with no panic.
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var gotTrace any
	var received bool
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(ctx context.Context, _ event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		gotTrace = ctx.Value(traceCtxKey{})
		received = true
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-noprop", Table: "products", Schema: "public", Action: event.Create,
		Metadata: map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return received
	})

	mu.Lock()
	defer mu.Unlock()
	if gotTrace != nil {
		t.Errorf("handler ctx trace value = %v, want nil (no propagator configured)", gotTrace)
	}
}

// TestNoTracingDependency pins the invariant that neither natsbus nor the
// stdlib-only event package pulls in an OpenTelemetry propagator: the OTel
// adapter is entirely consumer-supplied via the Propagator interface. It shells
// out to `go list` (fast, no Docker) to inspect the production import closure.
func TestNoTracingDependency(t *testing.T) {
	for _, pkg := range []string{
		"github.com/teandresmith/sqlgen/event/natsbus",
		"github.com/teandresmith/sqlgen/event",
	} {
		//nolint:gosec // G204: pkg ranges over fixed package-path literals declared just above, never external input.
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for dep := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			if strings.Contains(dep, "opentelemetry") || strings.Contains(dep, "go.opentelemetry.io") {
				t.Errorf("%s imports a tracing dependency %q; OTel must stay consumer-supplied", pkg, dep)
			}
		}
	}
}

func TestDefaultFailingHandlerNoPanicNoRedeliver(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	var calls int64
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&calls, 1)
		return errHandlerBoom
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-default", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool { return atomic.LoadInt64(&calls) == 1 })
	// With no error handler and no retry, the error is discarded: no panic,
	// no redelivery (core is at-most-once).
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("handler invocations = %d, want 1 (core does not redeliver)", got)
	}
}

// startJetStreamServer boots an in-process NATS server with JetStream enabled
// (backed by a per-test temp store dir) and returns a connected client. The
// server and connection are closed via t.Cleanup.
func startJetStreamServer(t *testing.T) *nats.Conn {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = server.RANDOM_PORT
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats.Connect() unexpected error: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func TestJetStreamPublishStoresMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{
		Name:        "SQLGEN_EVENTS",
		DedupWindow: time.Minute,
	}))
	t.Cleanup(func() { _ = bus.Close() })

	want := event.Event{
		ID:        "evt-js-1",
		Table:     "products",
		Schema:    "public",
		Action:    event.Create,
		PK:        "42",
		Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
	}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	ctx := context.Background()
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("jetstream.New() unexpected error: %v", err)
	}
	stream, err := js.Stream(ctx, "SQLGEN_EVENTS")
	if err != nil {
		t.Fatalf("Stream() unexpected error: %v", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("Stream.Info() unexpected error: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("stored message count = %d, want 1", info.State.Msgs)
	}

	msg, err := stream.GetMsg(ctx, info.State.LastSeq)
	if err != nil {
		t.Fatalf("GetMsg() unexpected error: %v", err)
	}
	if got := msg.Header.Get("Nats-Msg-Id"); got != want.ID {
		t.Errorf("Nats-Msg-Id header = %q, want %q (= Event.ID)", got, want.ID)
	}
	var got event.Event
	if err := json.Unmarshal(msg.Data, &got); err != nil {
		t.Fatalf("Unmarshal stored body: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stored event mismatch (-want +got):\n%s", diff)
	}
}

func TestJetStreamDedupWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{
		Name:        "SQLGEN_EVENTS",
		DedupWindow: time.Minute,
	}))
	t.Cleanup(func() { _ = bus.Close() })

	// Same Event.ID published twice inside the dedup window: the broker collapses
	// the second publish (same Nats-Msg-Id) into the first stored message.
	e := event.Event{ID: "evt-dup", Table: "products", Schema: "public", Action: event.Create}
	for range 2 {
		if err := bus.Publish(context.Background(), e); err != nil {
			t.Fatalf("Publish() unexpected error: %v", err)
		}
	}

	ctx := context.Background()
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("jetstream.New() unexpected error: %v", err)
	}
	stream, err := js.Stream(ctx, "SQLGEN_EVENTS")
	if err != nil {
		t.Fatalf("Stream() unexpected error: %v", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("Stream.Info() unexpected error: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Errorf("stored message count = %d, want 1 (dedup on Nats-Msg-Id)", info.State.Msgs)
	}
}

func TestJetStreamProvisioningFailureSurfaced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	// startServer boots a core NATS server with JetStream DISABLED, so stream
	// provisioning at construction fails. The failure must surface from the first
	// Publish/Subscribe, not be swallowed.
	bus := natsbus.New(startServer(t), natsbus.WithJetStream(natsbus.StreamConfig{
		Name: "SQLGEN_EVENTS",
	}))
	t.Cleanup(func() { _ = bus.Close() })

	if err := bus.Publish(context.Background(), event.Event{ID: "x", Table: "products"}); err == nil {
		t.Error("Publish() error = nil, want provisioning failure surfaced")
	}
	if _, err := bus.Subscribe(event.SubscribeOptions{}, func(context.Context, event.Event) error {
		return nil
	}); err == nil {
		t.Error("Subscribe() error = nil, want provisioning failure surfaced")
	}
}

func TestJetStreamCoreModeCreatesNoStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	// A JetStream-enabled server, but a core-mode bus (no WithJetStream): no
	// stream is provisioned and behavior is unchanged from core NATS.
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn)
	t.Cleanup(func() { _ = bus.Close() })

	if err := bus.Publish(context.Background(), event.Event{ID: "c", Table: "products", Schema: "public"}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("jetstream.New() unexpected error: %v", err)
	}
	if _, err := js.Stream(context.Background(), "SQLGEN_EVENTS"); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Errorf("Stream() error = %v, want %v (core mode provisions no stream)", err, jetstream.ErrStreamNotFound)
	}
}

func TestJetStreamRedeliversOnError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	// The handler fails its first failCount deliveries (each naks for redelivery),
	// then succeeds (acks). At-least-once: the error return means "redeliver me".
	const failCount = 2
	var calls int64
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		if atomic.AddInt64(&calls, 1) <= failCount {
			return errHandlerBoom
		}
		return nil
	}, natsbus.WithDurable("projector"), natsbus.WithBackoff(10*time.Millisecond))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-js-redeliver", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// failCount naks + 1 successful delivery = failCount+1 total invocations.
	waitFor(t, 3*time.Second, func() bool {
		return atomic.LoadInt64(&calls) == failCount+1
	})
	// Settle: once the success acks, redelivery stops — no extra invocations.
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != failCount+1 {
		t.Errorf("handler invocations = %d, want %d (redeliver on error, ack on success)", got, failCount+1)
	}
}

func TestJetStreamAcksOnSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	var calls int64
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&calls, 1)
		return nil
	}, natsbus.WithDurable("acker"))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-js-ack", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt64(&calls) == 1 })
	// A nil return acks immediately; the message is delivered exactly once (no
	// redelivery).
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("handler invocations = %d, want 1 (nil return acks, no redelivery)", got)
	}
}

func TestJetStreamDeadLetterAfterMaxDeliver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	// The dead-letter subject sits outside the stream's captured subjects
	// ("sqlgen.events.>"), so a plain core subscription observes the routed message.
	const (
		dlqSubject = "sqlgen.dlq"
		maxDeliver = 3
	)
	dlqCh := make(chan *nats.Msg, 4)
	dlqSub, err := conn.ChanSubscribe(dlqSubject, dlqCh)
	if err != nil {
		t.Fatalf("ChanSubscribe(%q) unexpected error: %v", dlqSubject, err)
	}
	t.Cleanup(func() { _ = dlqSub.Unsubscribe() })

	// A handler that always fails: it is nak'd up to maxDeliver, then the final
	// failed delivery routes the message to the dead-letter subject.
	var calls int64
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&calls, 1)
		return errHandlerBoom
	}, natsbus.WithDurable("dlq-proj"), natsbus.WithMaxDeliver(maxDeliver), natsbus.WithDeadLetter(dlqSubject))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	want := event.Event{
		ID: "evt-dlq", Table: "products", Schema: "public", Action: event.Create, PK: "5",
		Metadata: map[string]string{"traceparent": "00-trace-span-01"},
	}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// After maxDeliver failed deliveries the message lands on the DLQ subject with
	// its original envelope and headers intact.
	select {
	case msg := <-dlqCh:
		if got := msg.Header.Get("Nats-Msg-Id"); got != want.ID {
			t.Errorf("DLQ Nats-Msg-Id header = %q, want %q (= Event.ID)", got, want.ID)
		}
		if got := msg.Header.Get("traceparent"); got != "00-trace-span-01" {
			t.Errorf("DLQ traceparent header = %q, want %q", got, "00-trace-span-01")
		}
		var got event.Event
		if err := json.Unmarshal(msg.Data, &got); err != nil {
			t.Fatalf("Unmarshal DLQ body: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("DLQ payload mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no message routed to DLQ subject %q", dlqSubject)
	}

	// Settle: exactly maxDeliver handler invocations and exactly one DLQ message —
	// termination stops further redelivery.
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != maxDeliver {
		t.Errorf("handler invocations = %d, want %d (deliver up to max, then dead-letter)", got, maxDeliver)
	}
	if extra := len(dlqCh); extra != 0 {
		t.Errorf("extra DLQ messages = %d, want 0 (message terminated after routing)", extra)
	}
}

func TestJetStreamUndecodableBodyTerminated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	// An undecodable body is Term()'d — the handler is never invoked for it and
	// it is not redelivered in a loop. A valid message published afterward is
	// still delivered, proving the consumer keeps working.
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []event.Event
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	}, natsbus.WithDurable("decoder"))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// A malformed body published directly to a stream-captured subject: the stream
	// stores it, the consumer delivers it, jsMsgHandler fails to decode and Term()s it.
	if err := conn.Publish("sqlgen.events.public.products", []byte("{ not valid json")); err != nil {
		t.Fatalf("raw Publish() unexpected error: %v", err)
	}
	good := event.Event{ID: "evt-good", Table: "products", Schema: "public", Action: event.Create}
	if err := bus.Publish(context.Background(), good); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// Only the valid event reaches the handler; the malformed one is terminated,
	// never decoded into the handler, never redelivered.
	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	time.Sleep(200 * time.Millisecond)

	// Distinguish Term() from a Nak loop: a Nak'd malformed message would sit in
	// the consumer's ack-pending set and be redelivered repeatedly. Term removes
	// it, so after both messages settle nothing is ack-pending and nothing was
	// redelivered. (From the handler's view alone Term and Nak are indistinguishable
	// — the bad message never reaches the handler either way — so the consumer state
	// is what actually pins the terminal action.)
	assertConsumerSettled(t, conn, "decoder")

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]event.Event{good}, got); diff != "" {
		t.Errorf("delivered events mismatch (-want +got):\n%s", diff)
	}
}

// assertConsumerSettled fails if the named durable consumer has any ack-pending
// or redelivered messages — the fingerprint of a Nak/redelivery loop. A cleanly
// Term()'d or Ack()'d message leaves both counters at zero.
func assertConsumerSettled(t *testing.T, conn *nats.Conn, durable string) {
	t.Helper()
	ctx := context.Background()
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("jetstream.New() unexpected error: %v", err)
	}
	cons, err := js.Consumer(ctx, "SQLGEN_EVENTS", durable)
	if err != nil {
		t.Fatalf("Consumer(%q) unexpected error: %v", durable, err)
	}
	info, err := cons.Info(ctx)
	if err != nil {
		t.Fatalf("Consumer.Info() unexpected error: %v", err)
	}
	if info.NumAckPending != 0 {
		t.Errorf("consumer %q NumAckPending = %d, want 0 (message not left pending — no Nak loop)", durable, info.NumAckPending)
	}
	if info.NumRedelivered != 0 {
		t.Errorf("consumer %q NumRedelivered = %d, want 0 (message not redelivered — Term/Ack, not Nak)", durable, info.NumRedelivered)
	}
}

func TestJetStreamFilteredOutAcked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	// A message that fails the per-message action filter is
	// Ack()'d (consumed, not redelivered), while a matching message is delivered.
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []string
	// No Tables filter → the consumer's FilterSubject is the wildcard, so both
	// events reach the consumer and the action filter is applied per-message.
	sub, err := bus.SubscribeWith(event.SubscribeOptions{Actions: []event.Action{event.Create}},
		func(_ context.Context, e event.Event) error {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, e.ID)
			return nil
		}, natsbus.WithDurable("filter-consumer"))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// The Update fails the Create-only filter (acked, not delivered to the handler);
	// the Create matches and is delivered.
	events := []event.Event{
		{ID: "upd", Table: "products", Schema: "public", Action: event.Update},
		{ID: "cre", Table: "products", Schema: "public", Action: event.Create},
	}
	for _, e := range events {
		if err := bus.Publish(context.Background(), e); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", e.ID, err)
		}
	}

	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	// Settle: the filtered-out Update was acked, so it is not redelivered into the
	// handler.
	time.Sleep(200 * time.Millisecond)

	// Pin Ack() over Nak(): had the filtered-out Update been Nak'd it would be
	// redelivered in a loop and stay ack-pending. Both counters at zero prove it
	// was consumed (acked), not looping.
	assertConsumerSettled(t, conn, "filter-consumer")

	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]string{"cre"}, got); diff != "" {
		t.Errorf("delivered IDs mismatch (-want +got):\n%s", diff)
	}
}

func TestJetStreamMaxDeliverWithoutDeadLetter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	// Criterion #3: WithMaxDeliver without WithDeadLetter caps broker redelivery at
	// n and then follows JetStream's default terminal behavior — no DLQ routing, no
	// crash, no unbounded redelivery loop.
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	const maxDeliver = 2
	var calls int64
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, _ event.Event) error {
		atomic.AddInt64(&calls, 1)
		return errHandlerBoom // always fails — never acked
	}, natsbus.WithDurable("bounded"), natsbus.WithMaxDeliver(maxDeliver))
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-bounded", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, 3*time.Second, func() bool { return atomic.LoadInt64(&calls) == maxDeliver })
	// Settle: the broker must not deliver past the max-deliver cap.
	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != maxDeliver {
		t.Errorf("handler invocations = %d, want %d (MaxDeliver caps redelivery; no DLQ, no crash)", got, maxDeliver)
	}
}

// collectIDs subscribes a fresh durable JetStream consumer with the given
// delivery-start option, waits for want events, and returns their IDs sorted. It
// factors the common replay/backfill test shape.
func collectIDs(t *testing.T, bus *natsbus.Bus, durable string, want int, startOpt natsbus.SubscribeOption) []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	sub, err := bus.SubscribeWith(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e.ID)
		return nil
	}, natsbus.WithDurable(durable), startOpt)
	if err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == want
	})
	// Settle: no extra deliveries beyond the expected replay window.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	out := append([]string(nil), got...)
	sort.Strings(out)
	return out
}

func TestJetStreamDeliverAllReplaysHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	// Publish the full history BEFORE any subscriber exists — a fresh consumer must
	// rebuild its projection from stream history, not just live traffic.
	ids := []string{"1", "2", "3", "4", "5"}
	for _, id := range ids {
		if err := bus.Publish(context.Background(), event.Event{
			ID: id, Table: "products", Schema: "public", Action: event.Create,
		}); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", id, err)
		}
	}

	got := collectIDs(t, bus, "replayer", len(ids), natsbus.WithDeliverAll())
	if diff := cmp.Diff(ids, got); diff != "" {
		t.Errorf("WithDeliverAll replay mismatch (-want +got):\n%s", diff)
	}
}

func TestJetStreamDeliverFromSequence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	// Four events → stream sequences 1..4 (single subject, published in order, no
	// dedup window set). Starting at sequence 3 must skip 1..2 and replay 3..4.
	ids := []string{"1", "2", "3", "4"}
	for _, id := range ids {
		if err := bus.Publish(context.Background(), event.Event{
			ID: id, Table: "products", Schema: "public", Action: event.Create,
		}); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", id, err)
		}
	}

	got := collectIDs(t, bus, "seq-replayer", 2, natsbus.WithDeliverFromSequence(3))
	if diff := cmp.Diff([]string{"3", "4"}, got); diff != "" {
		t.Errorf("WithDeliverFromSequence(3) offset mismatch (-want +got):\n%s", diff)
	}
}

func TestJetStreamDeliverFromTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	conn := startJetStreamServer(t)
	bus := natsbus.New(conn, natsbus.WithJetStream(natsbus.StreamConfig{Name: "SQLGEN_EVENTS"}))
	t.Cleanup(func() { _ = bus.Close() })

	// Publish an "old" batch, record a cutoff between two 100ms gaps, then publish a
	// "new" batch. Starting at the cutoff time must replay only the new batch.
	for _, id := range []string{"old-1", "old-2"} {
		if err := bus.Publish(context.Background(), event.Event{
			ID: id, Table: "products", Schema: "public", Action: event.Create,
		}); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", id, err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(100 * time.Millisecond)
	for _, id := range []string{"new-1", "new-2"} {
		if err := bus.Publish(context.Background(), event.Event{
			ID: id, Table: "products", Schema: "public", Action: event.Create,
		}); err != nil {
			t.Fatalf("Publish(%q) unexpected error: %v", id, err)
		}
	}

	got := collectIDs(t, bus, "time-replayer", 2, natsbus.WithDeliverFromTime(cutoff))
	if diff := cmp.Diff([]string{"new-1", "new-2"}, got); diff != "" {
		t.Errorf("WithDeliverFromTime(cutoff) offset mismatch (-want +got):\n%s", diff)
	}
}

func TestSubscribeInterfaceNarrowing(t *testing.T) {
	bus := natsbus.New(startServer(t))
	t.Cleanup(func() { _ = bus.Close() })

	// Held as the frozen event.Subscriber interface, only the core Subscribe is
	// in reach — this line compiling proves *Bus still satisfies event.Subscriber
	// after SubscribeWith's variadic was added to the concrete type.
	var narrowed event.Subscriber = bus

	var mu sync.Mutex
	var viaInterface, viaConcrete int
	if _, err := narrowed.Subscribe(event.SubscribeOptions{Tables: []string{"products"}},
		func(_ context.Context, _ event.Event) error {
			mu.Lock()
			viaInterface++
			mu.Unlock()
			return nil
		}); err != nil {
		t.Fatalf("Subscribe() via event.Subscriber unexpected error: %v", err)
	}

	// Held as the concrete *Bus, the durable knobs are reachable. In core mode
	// they are inert, so this behaves as an ordinary core subscription.
	if _, err := bus.SubscribeWith(event.SubscribeOptions{Tables: []string{"products"}},
		func(_ context.Context, _ event.Event) error {
			mu.Lock()
			viaConcrete++
			mu.Unlock()
			return nil
		}, natsbus.WithDurable("inert-in-core")); err != nil {
		t.Fatalf("SubscribeWith() unexpected error: %v", err)
	}

	if err := bus.Publish(context.Background(), event.Event{
		ID: "evt-narrow", Table: "products", Schema: "public", Action: event.Create,
	}); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// Both subscriptions get core-filter semantics for the event.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return viaInterface == 1 && viaConcrete == 1
	})
}

// TestCloseUnsubscribeConcurrentNoRace: Close and Unsubscribe both touch each
// subscription's natsSub/jsConsume handles, and must do so only under the bus
// lock. Racing Close against a concurrent Unsubscribe on every subscription,
// under -race, guards against the unsynchronized field access the earlier Close
// had (it detached handles outside the lock).
func TestCloseUnsubscribeConcurrentNoRace(t *testing.T) {
	bus := natsbus.New(startServer(t))

	const n = 8
	subs := make([]event.Subscription, 0, n)
	for range n {
		sub, err := bus.Subscribe(event.SubscribeOptions{}, func(context.Context, event.Event) error { return nil })
		if err != nil {
			t.Fatalf("Subscribe() unexpected error: %v", err)
		}
		subs = append(subs, sub)
	}

	var wg sync.WaitGroup
	wg.Add(len(subs) + 1)
	go func() {
		defer wg.Done()
		_ = bus.Close()
	}()
	for _, sub := range subs {
		go func(s event.Subscription) {
			defer wg.Done()
			_ = s.Unsubscribe()
		}(sub)
	}
	wg.Wait()
}

// startServerURL boots an in-process NATS server and returns its client URL,
// for exercising natsbus.Connect (which dials a URL rather than taking an
// already-connected *nats.Conn). The server is shut down via t.Cleanup.
func startServerURL(t *testing.T) string {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = server.RANDOM_PORT
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

// startJetStreamServerURL is startServerURL with JetStream enabled, for the
// Connect + WithJetStream pairing test.
func startJetStreamServerURL(t *testing.T) string {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = server.RANDOM_PORT
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

// TestConnectPublishSubscribe is the Connect smoke test: a bus built by dialing
// a URL with the blessed resilience defaults publishes and delivers an event
// end to end, and Close (which drains the Connect-owned connection) succeeds.
func TestConnectPublishSubscribe(t *testing.T) {
	bus, err := natsbus.Connect(startServerURL(t))
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []event.Event
	sub, err := bus.Subscribe(event.SubscribeOptions{}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	want := event.Event{ID: "evt-conn", Table: "products", Schema: "public", Action: event.Create, PK: "1"}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	if diff := cmp.Diff([]event.Event{want}, got); diff != "" {
		t.Errorf("delivered event mismatch (-want +got):\n%s", diff)
	}
}

// TestConnectDialFailure asserts a dial failure is returned, not swallowed.
func TestConnectDialFailure(t *testing.T) {
	// A syntactically valid but unreachable URL: nats.Connect fails fast rather
	// than entering the reconnect loop for the initial dial.
	_, err := natsbus.Connect("nats://127.0.0.1:1")
	if err == nil {
		t.Fatal("Connect() to an unreachable server: got nil error, want a dial error")
	}
}

// TestConnectPairsWithJetStream verifies the PRD claim that a Connect-built bus
// is "ready to pair with WithJetStream": passing WithJetStream through Connect
// provisions the stream and persists a published event.
func TestConnectPairsWithJetStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JetStream integration test in short mode")
	}
	url := startJetStreamServerURL(t)
	bus, err := natsbus.Connect(url, natsbus.WithJetStream(natsbus.StreamConfig{
		Name:        "SQLGEN_EVENTS",
		DedupWindow: time.Minute,
	}))
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	want := event.Event{ID: "evt-conn-js", Table: "products", Schema: "public", Action: event.Create, PK: "7"}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	// Verify persistence via an independent JS client on the same server.
	verify, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("nats.Connect(verify) unexpected error: %v", err)
	}
	t.Cleanup(verify.Close)
	js, err := jetstream.New(verify)
	if err != nil {
		t.Fatalf("jetstream.New() unexpected error: %v", err)
	}
	ctx := context.Background()
	stream, err := js.Stream(ctx, "SQLGEN_EVENTS")
	if err != nil {
		t.Fatalf("Stream() unexpected error: %v", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("Stream.Info() unexpected error: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("stored message count = %d, want 1", info.State.Msgs)
	}
}

// TestActionSubjectsPublish asserts WithActionSubjects publishes to
// "{prefix}.{schema}.{table}.{action}" and stamps the bumped envelope version.
func TestActionSubjectsPublish(t *testing.T) {
	conn := startServer(t)
	bus := natsbus.New(conn, natsbus.WithActionSubjects())
	t.Cleanup(func() { _ = bus.Close() })

	const subject = "sqlgen.events.public.tasks.create"
	msgCh := make(chan *nats.Msg, 1)
	rawSub, err := conn.ChanSubscribe(subject, msgCh)
	if err != nil {
		t.Fatalf("ChanSubscribe(%q) unexpected error: %v", subject, err)
	}
	t.Cleanup(func() { _ = rawSub.Unsubscribe() })

	want := event.Event{ID: "evt-act", Table: "tasks", Schema: "public", Action: event.Create, PK: "3"}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	select {
	case msg := <-msgCh:
		if msg.Subject != subject {
			t.Errorf("received subject = %q, want %q", msg.Subject, subject)
		}
		// The action-qualified scheme is a distinct wire scheme → version 2.
		// Wire string asserted literally, independent of the natsbus constant.
		if got := msg.Header.Get("Sqlgen-Envelope-Version"); got != "2" {
			t.Errorf("Sqlgen-Envelope-Version header = %q, want %q", got, "2")
		}
		var got event.Event
		if err := json.Unmarshal(msg.Data, &got); err != nil {
			t.Fatalf("Unmarshal(body) unexpected error: %v", err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("body decode mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(time.Second):
		t.Fatalf("no message received on %q", subject)
	}
}

// TestActionSubjectsBrokerFilter asserts that on a WithActionSubjects bus a
// subscription for a single table + single action narrows to
// "{prefix}.*.{table}.{action}", so a non-matching action never reaches the
// subscriber (broker-side action filtering).
func TestActionSubjectsBrokerFilter(t *testing.T) {
	bus := natsbus.New(startServer(t), natsbus.WithActionSubjects())
	t.Cleanup(func() { _ = bus.Close() })

	var mu sync.Mutex
	var got []event.Action
	sub, err := bus.Subscribe(event.SubscribeOptions{
		Tables:  []string{"tasks"},
		Actions: []event.Action{event.Create},
	}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e.Action)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	create := event.Event{ID: "c", Table: "tasks", Schema: "public", Action: event.Create}
	update := event.Event{ID: "u", Table: "tasks", Schema: "public", Action: event.Update}
	if err := bus.Publish(context.Background(), update); err != nil {
		t.Fatalf("Publish(update) unexpected error: %v", err)
	}
	if err := bus.Publish(context.Background(), create); err != nil {
		t.Fatalf("Publish(create) unexpected error: %v", err)
	}

	// Only the create should ever arrive; the update publishes to
	// "...tasks.update", outside the subscription's "...tasks.create" subject.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	// Give any misrouted update a chance to arrive before asserting exclusivity.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if diff := cmp.Diff([]event.Action{event.Create}, got); diff != "" {
		t.Errorf("delivered actions mismatch (-want +got):\n%s", diff)
	}
}
