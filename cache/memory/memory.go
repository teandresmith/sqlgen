// Package memory provides an in-memory cache.Backend backed by
// github.com/maypok86/otter/v2. It is a separate Go module so the otter
// dependency lands only in consumer projects that opt in to this backend.
//
// The package is named by capability ("memory"), not by library, so the
// underlying implementation can be swapped without a breaking change.
package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"

	"github.com/teandresmith/sqlgen/cache"
)

// Options configures a Backend. MaxSize is required; zero / negative
// values are rejected by New.
type Options struct {
	// MaxSize is the maximum number of entries the backend may hold.
	// Required — the underlying cache mandates a bound. New returns
	// an error when MaxSize <= 0.
	MaxSize int

	// DefaultTTL is the per-entry TTL used when Set is called with
	// ttl == 0. A zero DefaultTTL means entries set with ttl == 0
	// never expire.
	DefaultTTL time.Duration

	// StatsEnabled turns on otter's native stats recorder. When false,
	// Stats() reports zero hits / misses / evictions (Sets and
	// Invalidations are still tracked by the backend since otter does
	// not record them natively).
	StatsEnabled bool
}

// entry is the value stored inside the otter cache. The per-entry TTL
// is bundled with the value so otter's ExpiryCalculator can read the
// configured expiration directly from the entry snapshot.
type entry struct {
	value []byte
	ttl   time.Duration // 0 means no expiration
}

// Backend is an in-memory cache.Backend. It satisfies cache.Backend,
// cache.StatsReporter, and io.Closer.
type Backend struct {
	cache      *otter.Cache[string, entry]
	defaultTTL time.Duration

	sets          atomic.Uint64
	invalidations atomic.Uint64

	closed       atomic.Bool
	statsEnabled bool
}

// Compile-time interface assertions.
var (
	_ cache.Backend              = (*Backend)(nil)
	_ cache.StatsReporter        = (*Backend)(nil)
	_ interface{ Close() error } = (*Backend)(nil)
)

// ErrClosed is returned by Backend methods after Close has been called.
var ErrClosed = errors.New("memory: backend is closed")

// New constructs a Backend from the given Options. It returns an error
// when MaxSize <= 0 — there is no silent default.
func New(opts Options) (*Backend, error) {
	if opts.MaxSize <= 0 {
		return nil, fmt.Errorf("memory: Options.MaxSize must be > 0, got %d", opts.MaxSize)
	}

	otterOpts := &otter.Options[string, entry]{
		MaximumSize: opts.MaxSize,
		ExpiryCalculator: otter.ExpiryWritingFunc(func(e otter.Entry[string, entry]) time.Duration {
			// Returning 0 signals otter to leave the current expiration
			// untouched. For newly-created entries the initial expiration
			// is "unreachable" (effectively never expires), which is the
			// exact semantic we want when ttl == 0. Returning a large
			// sentinel duration here would overflow otter's internal
			// nowNano + duration computation into an already-expired time.
			ttl := e.Value.ttl
			if ttl <= 0 {
				return 0
			}
			return ttl
		}),
	}
	if opts.StatsEnabled {
		otterOpts.StatsRecorder = stats.NewCounter()
	}

	c, err := otter.New(otterOpts)
	if err != nil {
		return nil, fmt.Errorf("memory: build otter cache: %w", err)
	}

	return &Backend{
		cache:        c,
		defaultTTL:   opts.DefaultTTL,
		statsEnabled: opts.StatsEnabled,
	}, nil
}

// Get returns the bytes stored for key. It returns (nil, nil) on a miss,
// matching cache.Backend's contract.
func (b *Backend) Get(_ context.Context, key string) ([]byte, error) {
	if b.closed.Load() {
		return nil, ErrClosed
	}
	e, ok := b.cache.GetIfPresent(key)
	if !ok {
		return nil, nil
	}
	return e.value, nil
}

// Set stores value under key. When ttl > 0 it is used as the per-entry
// TTL. When ttl == 0 the backend falls back to Options.DefaultTTL; if
// that is also zero the entry never expires.
func (b *Backend) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if b.closed.Load() {
		return ErrClosed
	}
	resolved := ttl
	if resolved == 0 {
		resolved = b.defaultTTL
	}
	b.cache.Set(key, entry{value: value, ttl: resolved})
	b.sets.Add(1)
	return nil
}

// Invalidate removes key. An absent key is not an error.
func (b *Backend) Invalidate(_ context.Context, key string) error {
	if b.closed.Load() {
		return ErrClosed
	}
	b.cache.Invalidate(key)
	b.invalidations.Add(1)
	return nil
}

// InvalidateMany removes every key in keys. Absent keys are not errors.
func (b *Backend) InvalidateMany(_ context.Context, keys []string) error {
	if b.closed.Load() {
		return ErrClosed
	}
	for _, k := range keys {
		b.cache.Invalidate(k)
	}
	b.invalidations.Add(uint64(len(keys)))
	return nil
}

// InvalidatePattern removes every key starting with the pattern's
// prefix. The pattern contract is prefix + trailing "*"; any other
// form returns an error.
func (b *Backend) InvalidatePattern(_ context.Context, pattern string) error {
	if b.closed.Load() {
		return ErrClosed
	}
	if !strings.HasSuffix(pattern, "*") {
		return fmt.Errorf("memory: pattern %q must end with %q", pattern, "*")
	}
	prefix := pattern[:len(pattern)-1]

	var victims []string
	for k := range b.cache.Keys() {
		if strings.HasPrefix(k, prefix) {
			victims = append(victims, k)
		}
	}
	for _, k := range victims {
		b.cache.Invalidate(k)
	}
	b.invalidations.Add(uint64(len(victims)))
	return nil
}

// Stats returns a cache.Stats snapshot. When Options.StatsEnabled is
// false, hits / misses / evictions are zero — otter is not recording
// them. Sets and Invalidations are always tracked by the backend.
// Entries reflects otter's current estimated size.
func (b *Backend) Stats() cache.Stats {
	// otter.Cache.EstimatedSize returns int but is never negative — it is
	// an approximate entry count bounded by MaximumSize. Clamp before the
	// uint64 conversion so gosec's G115 overflow check is satisfied.
	entries := max(0, b.cache.EstimatedSize())
	snap := cache.Stats{
		Sets:          b.sets.Load(),
		Invalidations: b.invalidations.Load(),
		Entries:       uint64(entries),
	}
	if b.statsEnabled {
		ostats := b.cache.Stats()
		snap.Hits = ostats.Hits
		snap.Misses = ostats.Misses
		snap.Evictions = ostats.Evictions
	}
	return snap
}

// Close stops otter's background goroutines and marks the backend
// closed. Subsequent Get / Set / Invalidate* calls return ErrClosed.
// Close is idempotent.
func (b *Backend) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	b.cache.StopAllGoroutines()
	return nil
}
