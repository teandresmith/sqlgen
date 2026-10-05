package cache

import (
	"context"
	"time"
)

// NoopBackend is a Backend that treats every Get as a miss and every
// Set / Invalidate* as a successful no-op. It also satisfies
// StatsReporter with an all-zero Stats snapshot. Safe for tests,
// disabled-cache runtime toggles, and smoke-testing facade wiring
// without a real backend.
type NoopBackend struct{}

// Get always returns a cache miss.
func (NoopBackend) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, nil
}

// Set is a no-op.
func (NoopBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return nil
}

// Invalidate is a no-op.
func (NoopBackend) Invalidate(ctx context.Context, key string) error {
	return nil
}

// InvalidateMany is a no-op.
func (NoopBackend) InvalidateMany(ctx context.Context, keys []string) error {
	return nil
}

// InvalidatePattern is a no-op.
func (NoopBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	return nil
}

// Stats returns the zero Stats snapshot.
func (NoopBackend) Stats() Stats { return Stats{} }
