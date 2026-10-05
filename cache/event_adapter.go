package cache

import (
	"context"
	"fmt"
	"sync"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/hook"
)

// EventAdapterOption configures the InvalidationSource returned by
// FromEventSubscriber. Options let the generated *Cache facade wire
// observability (metrics) without breaking the PRD signature for
// user-direct callers that pass no options.
type EventAdapterOption func(*eventAdapter)

// WithEventMetrics wires a MetricsRecorder so handler-returned errors are
// recorded via MetricsRecorder.Error(schema, table, "invalidate", err)
// before propagating to the underlying event.Handler. The generated facade
// always supplies this; user-direct callers may omit it.
func WithEventMetrics(m MetricsRecorder) EventAdapterOption {
	return func(a *eventAdapter) { a.metrics = m }
}

// FromEventSubscriber creates an InvalidationSource that translates sqlgen
// mutation events into cache invalidation signals. It subscribes to all
// tables and all mutation actions with an empty event.SubscribeOptions{}.
//
// Semantics (PRD §27.9, CACHE.md §5.4):
//
//   - Synchronous per event. Each incoming event.Event produces exactly one
//     InvalidationHandler call carrying InvalidationSignal{Table, Schema,
//     Tenant, PKs: []any{ev.PK}} — the tenant forwarded verbatim from
//     ev.Metadata["tenant"] (PRD §29.6). There is no coalescing window —
//     publisher-side batching (event.Publisher PublishBatch) is preserved
//     transparently: the adapter forwards 1:1.
//   - Idempotent by backend contract. Backend.Invalidate / InvalidateMany
//     for an absent key is a success. At-least-once transports (NATS
//     redelivery, Kafka replay) are safe without event.ID dedup — the
//     adapter keeps no per-event state.
//   - Error routing. When the handler returns an error, the adapter (1)
//     records it via MetricsRecorder.Error(schema, table, "invalidate",
//     err) when WithEventMetrics is wired, and (2) returns the error from
//     the inner event.Handler so ACK-capable transports redeliver. Nothing
//     is silently swallowed.
func FromEventSubscriber(sub event.Subscriber, opts ...EventAdapterOption) InvalidationSource {
	a := &eventAdapter{sub: sub}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

type eventAdapter struct {
	sub     event.Subscriber
	metrics MetricsRecorder

	mu       sync.Mutex
	eventSub event.Subscription
	closed   bool
}

// Subscribe wires the InvalidationHandler to the underlying event.Subscriber
// via an event.Handler shim. The shim rebuilds the generated hook.TableName
// value from (ev.Schema, ev.Table) with tableNameFor, forwards pks as
// []any{ev.PK} — matching the Invalidate* element-shape contract for
// single-PK and composite-PK tables alike — and carries the
// tenant stamp from ev.Metadata["tenant"] (PRD §29.6) so
// the receiving facade can rebuild exact tenant-scoped keys for tenanted
// tables instead of refusing them. A missing/empty stamp leaves
// Signal.Tenant empty (the facade then degrades tenanted tables to a
// full-table clear).
func (a *eventAdapter) Subscribe(handler InvalidationHandler) (InvalidationSubscription, error) {
	shim := func(ctx context.Context, ev event.Event) error {
		table := tableNameFor(ev.Schema, ev.Table)
		err := handler(ctx, InvalidationSignal{
			Table:  table,
			Schema: ev.Schema,
			Tenant: ev.Metadata["tenant"],
			PKs:    []any{ev.PK},
		})
		if err != nil && a.metrics != nil {
			a.metrics.Error(ev.Schema, table, "invalidate", err)
		}
		return err
	}

	sub, err := a.sub.Subscribe(event.SubscribeOptions{}, shim)
	if err != nil {
		return nil, fmt.Errorf("cache: subscribe to event source: %w", err)
	}

	a.mu.Lock()
	a.eventSub = sub
	a.mu.Unlock()

	return &eventAdapterSubscription{adapter: a, sub: sub}, nil
}

// tableNameFor returns the generated hook.TableName value for a table:
// "schema.table" when the table has a schema (every PostgreSQL table), the
// bare name otherwise (MySQL, SQLite) — the rule PRD §8.5 sets for the
// constants. An event carries the two halves separately (PRD §28.3), so
// casting ev.Table alone names no PostgreSQL table: the facade rejects the
// signal as an unknown table, the entries stay stale, and the error goes back
// to the bus handler rather than to the cache's onError.
func tableNameFor(schema, table string) hook.TableName {
	if schema == "" {
		return hook.TableName(table)
	}
	return hook.TableName(schema + "." + table)
}

// Close unsubscribes the underlying event.Subscription and marks the adapter
// closed. Safe to call more than once — subsequent calls return nil.
func (a *eventAdapter) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	sub := a.eventSub
	a.eventSub = nil
	a.mu.Unlock()

	if sub != nil {
		if err := sub.Unsubscribe(); err != nil {
			return fmt.Errorf("cache: unsubscribe event source: %w", err)
		}
	}
	return nil
}

// eventAdapterSubscription is the InvalidationSubscription returned by
// eventAdapter.Subscribe. Unsubscribe is idempotent via sync.Once.
type eventAdapterSubscription struct {
	adapter *eventAdapter
	sub     event.Subscription

	once sync.Once
	err  error
}

// Unsubscribe detaches the handler from the underlying event.Subscriber.
// Idempotent via sync.Once — subsequent calls return the first call's error.
func (s *eventAdapterSubscription) Unsubscribe() error {
	s.once.Do(func() {
		if err := s.sub.Unsubscribe(); err != nil {
			s.err = fmt.Errorf("cache: unsubscribe event source: %w", err)
		}
		s.adapter.mu.Lock()
		if s.adapter.eventSub == s.sub {
			s.adapter.eventSub = nil
		}
		s.adapter.mu.Unlock()
	})
	return s.err
}
