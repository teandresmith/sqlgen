package featuressuite

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/hook"
)

// BackendCall is one call a RecordingBackend received.
type BackendCall struct {
	// Op is get, set, invalidate, invalidate_many or invalidate_pattern.
	Op string
	// Keys are the call's keys, or its one pattern.
	Keys []string
	// Hit reports whether a get found its key.
	Hit bool
}

// RecordingBackend is a cache.Backend that records each call before passing
// it to Inner.
type RecordingBackend struct {
	Inner cache.Backend

	mu    sync.Mutex
	calls []BackendCall
}

func (b *RecordingBackend) record(c BackendCall) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, c)
}

// Get records the lookup and whether it hit.
func (b *RecordingBackend) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := b.Inner.Get(ctx, key)
	b.record(BackendCall{Op: "get", Keys: []string{key}, Hit: err == nil && v != nil})
	return v, err
}

// Set records the key.
func (b *RecordingBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	b.record(BackendCall{Op: "set", Keys: []string{key}})
	return b.Inner.Set(ctx, key, value, ttl)
}

// Invalidate records the key.
func (b *RecordingBackend) Invalidate(ctx context.Context, key string) error {
	b.record(BackendCall{Op: "invalidate", Keys: []string{key}})
	return b.Inner.Invalidate(ctx, key)
}

// InvalidateMany records the keys.
func (b *RecordingBackend) InvalidateMany(ctx context.Context, keys []string) error {
	b.record(BackendCall{Op: "invalidate_many", Keys: slices.Clone(keys)})
	return b.Inner.InvalidateMany(ctx, keys)
}

// InvalidatePattern records the pattern.
func (b *RecordingBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	b.record(BackendCall{Op: "invalidate_pattern", Keys: []string{pattern}})
	return b.Inner.InvalidatePattern(ctx, pattern)
}

// Calls returns the recorded calls with Op op, in call order, or every call
// when op is empty.
func (b *RecordingBackend) Calls(op string) []BackendCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []BackendCall
	for _, c := range b.calls {
		if op == "" || c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// Invalidations returns the invalidate, invalidate_many and
// invalidate_pattern calls, in call order.
func (b *RecordingBackend) Invalidations() []BackendCall {
	var out []BackendCall
	for _, c := range b.Calls("") {
		if c.Op != "get" && c.Op != "set" {
			out = append(out, c)
		}
	}
	return out
}

// Clear forgets the recorded calls. The cached entries stay.
func (b *RecordingBackend) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = nil
}

// CacheError is one report to a cache's OnErrorFunc.
type CacheError struct {
	Op    string
	Table hook.TableName
	Err   error
}

// CacheErrors records what a cache reports to its OnErrorFunc.
type CacheErrors struct {
	mu   sync.Mutex
	errs []CacheError
}

// OnError is the cache's cache.OnErrorFunc.
func (e *CacheErrors) OnError(_ context.Context, op, _ string, table hook.TableName, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errs = append(e.errs, CacheError{Op: op, Table: table, Err: err})
}

// All returns the reports, in order.
func (e *CacheErrors) All() []CacheError {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.errs)
}

// HandlerResults is a cache.InvalidationSource that wraps Inner and records
// what the cache's handler returns for each signal.
type HandlerResults struct {
	Inner cache.InvalidationSource

	mu   sync.Mutex
	errs []error
}

// Subscribe subscribes a handler that records h's result to Inner.
func (s *HandlerResults) Subscribe(h cache.InvalidationHandler) (cache.InvalidationSubscription, error) {
	return s.Inner.Subscribe(func(ctx context.Context, signal cache.InvalidationSignal) error {
		err := h(ctx, signal)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.errs = append(s.errs, err)
		return err
	})
}

// Close closes Inner.
func (s *HandlerResults) Close() error { return s.Inner.Close() }

// Results returns the handler's result for each signal, in order.
func (s *HandlerResults) Results() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.errs)
}
