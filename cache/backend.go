// Package cache provides the runtime cache layer for sqlgen. It defines the
// Backend interface that consumer-supplied cache implementations satisfy,
// cache-key grammar helpers, a reusable circuit breaker, a metrics recorder
// interface, an error-handling policy, and a set of typed convenience
// helpers that sit on top of the byte-level Backend.
//
// The package imports only the Go standard library, golang.org/x/sync,
// and sqlgen's hook and event packages. It MUST NOT depend on the parser
// or CLI modules.
package cache

import (
	"context"
	"time"
)

// Backend is the interface a cache implementation must satisfy. All methods
// are mandatory — pattern invalidation is a core requirement (driven by view
// cache invalidation, InvalidateTable, and the tenant capture-gap fallback),
// not an optional capability.
//
// Contract:
//
//   - Get MUST return (nil, nil) for a cache miss. A non-nil error indicates
//     a real backend failure.
//   - Invalidate, InvalidateMany, and InvalidatePattern MUST treat an absent
//     key as a success. Deleting something that is not there is not an error.
//     This keeps the event-driven invalidation adapter idempotent under
//     at-least-once transports.
//
// Pattern grammar accepted by the contract is prefix + trailing "*"
// (e.g. "sqlgen:public.products:*"). Backends MAY support richer glob
// patterns but the interface contract is this minimal form.
type Backend interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Invalidate(ctx context.Context, key string) error
	InvalidateMany(ctx context.Context, keys []string) error
	InvalidatePattern(ctx context.Context, pattern string) error
}

// StatsReporter is an optional snapshot interface. A Backend may additionally
// implement StatsReporter to expose pull-based cumulative counters and
// point-in-time gauges. Fields the backend cannot report are left zero.
type StatsReporter interface {
	Stats() Stats
}

// Stats is a point-in-time snapshot of cache backend activity. Cumulative
// counters monotonically increase for the lifetime of the backend; gauges
// reflect the current state.
type Stats struct {
	Hits          uint64
	Misses        uint64
	Sets          uint64
	Invalidations uint64
	Evictions     uint64

	Entries uint64
}
