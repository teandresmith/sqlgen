// Package memorybus provides an in-memory implementation of the event
// Publisher and Subscriber interfaces. It is intended for unit tests,
// integration tests, and single-process applications that do not require
// distributed delivery. Dispatch is synchronous and lives entirely within
// the calling process.
package memorybus

import (
	"context"
	"slices"
	"sync"

	"github.com/teandresmith/sqlgen/event"
)

// Option configures a Bus at construction time.
type Option func(*Bus)

// WithSubscribeErrorHandler registers a handler invoked when a subscription
// Handler returns a non-nil error. memorybus is a faithful at-most-once
// reference transport: it never retries or redelivers, so a Handler error is
// otherwise discarded exactly as in production single-process use. Registering
// this makes the failure observable in tests and single-process apps without
// changing delivery semantics (section 28.7.1). It mirrors natsbus's
// option of the same name for parity. The default — no handler — discards the
// error without allocating and is byte-identical to prior behavior.
func WithSubscribeErrorHandler(fn func(ctx context.Context, e event.Event, err error)) Option {
	return func(b *Bus) {
		b.subErrHandler = fn
	}
}

// Bus is an in-memory event.Publisher and event.Subscriber. It is safe for
// concurrent use by multiple goroutines.
type Bus struct {
	mu            sync.RWMutex
	nextID        uint64
	subs          []*subscription
	groupCounters map[string]uint64
	closed        bool

	// subErrHandler observes Handler errors when set. It is
	// configured once at construction, before New returns, so Publish reads it
	// without locking. It never alters delivery — memorybus does not redeliver.
	subErrHandler func(ctx context.Context, e event.Event, err error)
}

// subscription is a registered handler with its filtering options.
type subscription struct {
	id      uint64
	bus     *Bus
	opts    event.SubscribeOptions
	handler event.Handler
}

// Unsubscribe removes the subscription from its bus. Calling Unsubscribe on
// an already-removed subscription is a no-op.
func (s *subscription) Unsubscribe() error {
	s.bus.remove(s.id)
	return nil
}

// New returns a new in-memory Bus with no subscribers.
func New(opts ...Option) *Bus {
	b := &Bus{groupCounters: make(map[string]uint64)}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Publish dispatches the event to all matching subscribers synchronously.
// Handlers are invoked outside the internal lock so they may subscribe,
// unsubscribe, or publish without deadlocking. A Handler error is routed to the
// WithSubscribeErrorHandler handler when one is configured; otherwise it is
// discarded — memorybus is at-most-once and never redelivers.
// Returns nil when the bus is closed (no-op).
func (b *Bus) Publish(ctx context.Context, e event.Event) error {
	targets := b.selectTargets(e)
	for _, s := range targets {
		if err := s.handler(ctx, e); err != nil && b.subErrHandler != nil {
			b.subErrHandler(ctx, e, err)
		}
	}
	return nil
}

// PublishBatch dispatches every event in the slice via Publish.
func (b *Bus) PublishBatch(ctx context.Context, events []event.Event) error {
	for i := range events {
		if err := b.Publish(ctx, events[i]); err != nil {
			return err
		}
	}
	return nil
}

// Close clears all subscriptions. Subsequent Publish/PublishBatch calls are
// no-ops.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = nil
	b.groupCounters = make(map[string]uint64)
	b.closed = true
	return nil
}

// Subscribe registers a handler with the given options. The returned
// Subscription can be cancelled via Unsubscribe. Subscriptions added after
// Close are accepted but receive no events.
func (b *Bus) Subscribe(opts event.SubscribeOptions, handler event.Handler) (event.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	s := &subscription{
		id:      b.nextID,
		bus:     b,
		opts:    opts,
		handler: handler,
	}
	b.subs = append(b.subs, s)
	return s, nil
}

// selectTargets resolves the set of subscribers that should receive e.
// Grouped subscribers participate in round-robin across the group's matching
// members; ungrouped subscribers all receive the event.
func (b *Bus) selectTargets(e event.Event) []*subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	var broadcast []*subscription
	groups := map[string][]*subscription{}
	for _, s := range b.subs {
		if !matches(s.opts, e) {
			continue
		}
		if s.opts.Group == "" {
			broadcast = append(broadcast, s)
			continue
		}
		groups[s.opts.Group] = append(groups[s.opts.Group], s)
	}

	chosen := broadcast
	for name, members := range groups {
		n := b.groupCounters[name]
		chosen = append(chosen, members[n%uint64(len(members))])
		b.groupCounters[name] = n + 1
	}
	return chosen
}

// remove deletes the subscription with the given ID. No-op if not found.
func (b *Bus) remove(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, s := range b.subs {
		if s.id == id {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

// matches reports whether an event satisfies the subscription's table and
// action filters. Empty filter lists match all values.
func matches(opts event.SubscribeOptions, e event.Event) bool {
	if len(opts.Tables) > 0 && !slices.Contains(opts.Tables, e.Table) {
		return false
	}
	if len(opts.Actions) > 0 && !slices.Contains(opts.Actions, e.Action) {
		return false
	}
	return true
}
