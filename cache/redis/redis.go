// Package redis provides a Redis-backed cache.Backend implementation using
// github.com/redis/go-redis/v9. It is a separate Go module so the Redis
// driver dependency only lands in consumer projects that opt in.
//
// The package is named by capability ("redis"), matching the distributed
// transport it wraps. Connection lifecycle ownership stays with the caller
// by default — use WithOwnedClient to transfer it to the Backend.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teandresmith/sqlgen/cache"
)

// DefaultScanCount is the SCAN page size used when WithScanCount is not
// supplied. Smaller values trade more round-trips for cheaper individual
// SCAN calls; larger values do the opposite.
const DefaultScanCount = 500

// Option configures a Backend at construction time.
type Option func(*Backend)

// WithOwnedClient transfers client lifecycle ownership to the Backend.
// When set, Close closes the underlying client. Without this option,
// Close is a no-op and the caller retains ownership — the default, so
// wrapping a shared client does not surprise-close it.
func WithOwnedClient() Option {
	return func(b *Backend) {
		b.ownedClient = true
	}
}

// WithScanCount overrides the SCAN page size used by InvalidatePattern.
// Values <= 0 are ignored, keeping DefaultScanCount.
func WithScanCount(n int) Option {
	return func(b *Backend) {
		if n > 0 {
			b.scanCount = n
		}
	}
}

// WithLocalStats toggles the local atomic stats counters. Defaults to
// true. Pass WithLocalStats(false) for a zero-overhead Stats path when
// the caller does not consume StatsReporter output.
func WithLocalStats(enabled bool) Option {
	return func(b *Backend) {
		b.localStats = enabled
	}
}

// Backend is a Redis cache.Backend. It satisfies cache.Backend,
// cache.StatsReporter, and io.Closer.
type Backend struct {
	client      redis.UniversalClient
	ownedClient bool
	scanCount   int
	localStats  bool

	hits          atomic.Uint64
	misses        atomic.Uint64
	sets          atomic.Uint64
	invalidations atomic.Uint64

	closed atomic.Bool
}

// Compile-time interface assertions.
var (
	_ cache.Backend       = (*Backend)(nil)
	_ cache.StatsReporter = (*Backend)(nil)
	_ io.Closer           = (*Backend)(nil)
)

// New wraps a caller-owned Redis client. The default is to leave
// lifecycle ownership with the caller; use WithOwnedClient to transfer
// it to the returned Backend.
func New(client redis.UniversalClient, opts ...Option) *Backend {
	b := &Backend{
		client:     client,
		scanCount:  DefaultScanCount,
		localStats: true,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Get returns the bytes stored under key. A miss (redis.Nil) is mapped
// to (nil, nil) per the cache.Backend contract. Other errors propagate.
func (b *Backend) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := b.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if b.localStats {
				b.misses.Add(1)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("redis: get %q: %w", key, err)
	}
	if b.localStats {
		b.hits.Add(1)
	}
	return val, nil
}

// Set stores value under key. A positive ttl is applied as EX; ttl == 0
// stores the entry with no expiration (plain SET without EX).
func (b *Backend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := b.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis: set %q: %w", key, err)
	}
	if b.localStats {
		b.sets.Add(1)
	}
	return nil
}

// Invalidate deletes key. An absent key is not an error — Redis DEL
// returns 0 in that case rather than signaling failure.
func (b *Backend) Invalidate(ctx context.Context, key string) error {
	if err := b.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis: del %q: %w", key, err)
	}
	if b.localStats {
		b.invalidations.Add(1)
	}
	return nil
}

// InvalidateMany deletes every key in keys via a single variadic DEL.
// Absent keys are not errors. An empty slice is a no-op.
func (b *Backend) InvalidateMany(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := b.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis: del %d keys: %w", len(keys), err)
	}
	if b.localStats {
		b.invalidations.Add(uint64(len(keys)))
	}
	return nil
}

// InvalidatePattern deletes every key matching pattern. It walks the
// keyspace with SCAN MATCH pattern COUNT n and issues DEL in batches
// — it NEVER uses KEYS, which blocks the Redis server on large
// keyspaces and is unsafe in production.
func (b *Backend) InvalidatePattern(ctx context.Context, pattern string) error {
	var (
		cursor  uint64
		deleted uint64
	)
	for {
		keys, next, err := b.client.Scan(ctx, cursor, pattern, int64(b.scanCount)).Result()
		if err != nil {
			return fmt.Errorf("redis: scan %q: %w", pattern, err)
		}
		if len(keys) > 0 {
			if err := b.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("redis: del %d keys: %w", len(keys), err)
			}
			deleted += uint64(len(keys))
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	if b.localStats && deleted > 0 {
		b.invalidations.Add(deleted)
	}
	return nil
}

// Stats returns a cache.Stats snapshot. Hits, misses, sets, and
// invalidations come from local atomic counters when WithLocalStats is
// true; they remain zero when stats are disabled. Entries is a
// best-effort sum parsed from INFO keyspace and is left at zero when
// unavailable.
func (b *Backend) Stats() cache.Stats {
	snap := cache.Stats{}
	if b.localStats {
		snap.Hits = b.hits.Load()
		snap.Misses = b.misses.Load()
		snap.Sets = b.sets.Load()
		snap.Invalidations = b.invalidations.Load()
	}
	snap.Entries = b.countEntries()
	return snap
}

// countEntries parses INFO keyspace output and sums "db<N>:keys=NN"
// across all reported databases. Any error or unparseable field yields
// 0 — Entries is a best-effort gauge.
func (b *Backend) countEntries() uint64 {
	info, err := b.client.Info(context.Background(), "keyspace").Result()
	if err != nil {
		return 0
	}
	var total uint64
	for line := range strings.SplitSeq(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "db") {
			continue
		}
		// Expected: "db0:keys=10,expires=0,avg_ttl=0"
		_, fields, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for f := range strings.SplitSeq(fields, ",") {
			k, v, ok := strings.Cut(f, "=")
			if !ok || k != "keys" {
				continue
			}
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				total += n
			}
		}
	}
	return total
}

// Close is a no-op unless the Backend was constructed with
// WithOwnedClient, in which case it closes the underlying client.
// Close is idempotent.
func (b *Backend) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	if !b.ownedClient {
		return nil
	}
	if err := b.client.Close(); err != nil {
		return fmt.Errorf("redis: close client: %w", err)
	}
	return nil
}
