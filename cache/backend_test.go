package cache_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
)

// fakeBackend implements cache.Backend + cache.StatsReporter + io.Closer
// purely so compile-time assertions can verify those interfaces. It has
// no behavior; tests that exercise real behavior use NoopBackend or
// per-test doubles.
type fakeBackend struct {
	stats cache.Stats
	err   error
}

func (f *fakeBackend) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, f.err
}

func (f *fakeBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return f.err
}

func (f *fakeBackend) Invalidate(ctx context.Context, key string) error {
	return f.err
}

func (f *fakeBackend) InvalidateMany(ctx context.Context, keys []string) error {
	return f.err
}

func (f *fakeBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	return f.err
}

func (f *fakeBackend) Stats() cache.Stats { return f.stats }

func (f *fakeBackend) Close() error { return nil }

func TestBackendInterfaceAssertions(t *testing.T) {
	var _ cache.Backend = (*fakeBackend)(nil)
	var _ cache.StatsReporter = (*fakeBackend)(nil)
	var _ io.Closer = (*fakeBackend)(nil)

	var _ cache.Backend = cache.NoopBackend{}
	var _ cache.StatsReporter = cache.NoopBackend{}
}
