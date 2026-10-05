package cache_test

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/cache"
)

func TestNoopBackend_GetAlwaysMisses(t *testing.T) {
	ctx := context.Background()
	n := cache.NoopBackend{}

	// Set first.
	if err := n.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("NoopBackend.Set() = %v, want nil", err)
	}
	got, err := n.Get(ctx, "k")
	if err != nil {
		t.Fatalf("NoopBackend.Get() err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("NoopBackend.Get() = %v, want nil (always misses)", got)
	}
}

func TestNoopBackend_InvalidateCallsAreNoOps(t *testing.T) {
	ctx := context.Background()
	n := cache.NoopBackend{}
	if err := n.Invalidate(ctx, "k"); err != nil {
		t.Errorf("NoopBackend.Invalidate() = %v, want nil", err)
	}
	if err := n.InvalidateMany(ctx, []string{"a", "b"}); err != nil {
		t.Errorf("NoopBackend.InvalidateMany() = %v, want nil", err)
	}
	if err := n.InvalidatePattern(ctx, "prefix:*"); err != nil {
		t.Errorf("NoopBackend.InvalidatePattern() = %v, want nil", err)
	}
}

func TestNoopBackend_StatsReturnsZero(t *testing.T) {
	got := cache.NoopBackend{}.Stats()
	if got != (cache.Stats{}) {
		t.Errorf("NoopBackend.Stats() = %+v, want zero Stats", got)
	}
}
