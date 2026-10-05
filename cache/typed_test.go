package cache_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cache"
)

// mapBackend is an in-memory Backend used purely for the typed helper tests. It
// is intentionally simpler than the first-party backends (memory, redis) — no
// TTL expiry, no stats, no pattern support beyond trailing "*" that this test
// suite never exercises. Tests that need the real semantics use
// cache.NoopBackend or the first-party memory backend once it is available.
type mapBackend struct {
	mu      sync.Mutex
	entries map[string][]byte
	setErr  error
	getErr  error
}

func newMapBackend() *mapBackend {
	return &mapBackend{entries: map[string][]byte{}}
}

func (b *mapBackend) Get(ctx context.Context, key string) ([]byte, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.entries[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (b *mapBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if b.setErr != nil {
		return b.setErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[key] = value
	return nil
}

func (b *mapBackend) Invalidate(ctx context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, key)
	return nil
}

func (b *mapBackend) InvalidateMany(ctx context.Context, keys []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range keys {
		delete(b.entries, k)
	}
	return nil
}

func (b *mapBackend) InvalidatePattern(ctx context.Context, pattern string) error { return nil }

func TestGetAs_MissReturnsZeroFalseNil(t *testing.T) {
	ctx := context.Background()
	b := newMapBackend()
	s := cache.JSONSerializer{}

	got, hit, err := cache.GetAs[sampleEntity](ctx, b, s, "k")
	if err != nil {
		t.Fatalf("GetAs() miss returned error: %v", err)
	}
	if hit {
		t.Errorf("GetAs() miss hit = true, want false")
	}
	if got != (sampleEntity{}) {
		t.Errorf("GetAs() miss value = %+v, want zero", got)
	}
}

func TestGetAs_HitReturnsDecodedValue(t *testing.T) {
	ctx := context.Background()
	b := newMapBackend()
	s := cache.JSONSerializer{}

	want := sampleEntity{ID: 7, Name: "bob"}
	if err := cache.SetAs(ctx, b, s, "k", want, 0); err != nil {
		t.Fatalf("SetAs() unexpected error: %v", err)
	}

	got, hit, err := cache.GetAs[sampleEntity](ctx, b, s, "k")
	if err != nil {
		t.Fatalf("GetAs() unexpected error: %v", err)
	}
	if !hit {
		t.Errorf("GetAs() hit = false, want true")
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetAs() value mismatch (-want +got):\n%s", diff)
	}
}

func TestSetAs_MarshalErrorDoesNotTouchBackend(t *testing.T) {
	ctx := context.Background()
	b := newMapBackend()
	// json can't encode a channel.
	err := cache.SetAs(ctx, b, cache.JSONSerializer{}, "k", make(chan int), 0)
	if err == nil {
		t.Fatalf("SetAs() with unmarshalable value = nil err, want error")
	}
	if len(b.entries) != 0 {
		t.Errorf("SetAs() with marshal error still wrote to backend: %v", b.entries)
	}
}

func TestGetOrSet_MissCallsLoadThenStores(t *testing.T) {
	ctx := context.Background()
	b := newMapBackend()
	s := cache.JSONSerializer{}

	want := sampleEntity{ID: 9, Name: "carol"}
	var loadCalls int
	got, err := cache.GetOrSet(ctx, b, s, "k", 0, func(ctx context.Context) (sampleEntity, error) {
		loadCalls++
		return want, nil
	})
	if err != nil {
		t.Fatalf("GetOrSet() miss unexpected error: %v", err)
	}
	if loadCalls != 1 {
		t.Errorf("GetOrSet() miss loadCalls = %d, want 1", loadCalls)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetOrSet() miss value mismatch (-want +got):\n%s", diff)
	}
	// Second call must be a hit.
	got2, err := cache.GetOrSet(ctx, b, s, "k", 0, func(ctx context.Context) (sampleEntity, error) {
		loadCalls++
		return sampleEntity{}, nil
	})
	if err != nil {
		t.Fatalf("GetOrSet() hit unexpected error: %v", err)
	}
	if loadCalls != 1 {
		t.Errorf("GetOrSet() hit loadCalls = %d, want 1 (load not re-run)", loadCalls)
	}
	if diff := cmp.Diff(want, got2); diff != "" {
		t.Errorf("GetOrSet() hit value mismatch (-want +got):\n%s", diff)
	}
}

func TestGetOrSet_LoadErrorPropagatesWithoutSet(t *testing.T) {
	ctx := context.Background()
	b := newMapBackend()
	s := cache.JSONSerializer{}

	loadErr := errors.New("db down")
	_, err := cache.GetOrSet(ctx, b, s, "k", 0, func(ctx context.Context) (sampleEntity, error) {
		return sampleEntity{}, loadErr
	})
	if !errors.Is(err, loadErr) {
		t.Errorf("GetOrSet() err = %v, want %v", err, loadErr)
	}
	if len(b.entries) != 0 {
		t.Errorf("GetOrSet() load error still wrote to backend: %v", b.entries)
	}
}

func TestKeysFromAny_HappyPathPreservesOrder(t *testing.T) {
	pks := []any{int64(1), int64(2), int64(3)}
	keyer := func(v int64) string { return "k:" + itoa(v) }
	got, err := cache.KeysFromAny(pks, keyer)
	if err != nil {
		t.Fatalf("KeysFromAny() unexpected error: %v", err)
	}
	want := []string{"k:1", "k:2", "k:3"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("KeysFromAny() mismatch (-want +got):\n%s", diff)
	}
}

func TestKeysFromAny_EmptySliceReturnsEmptyNilError(t *testing.T) {
	got, err := cache.KeysFromAny(nil, func(int64) string { return "" })
	if err != nil {
		t.Fatalf("KeysFromAny(nil) err = %v, want nil", err)
	}
	if got == nil {
		t.Errorf("KeysFromAny(nil) = nil slice, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("KeysFromAny(nil) len = %d, want 0", len(got))
	}
}

func TestKeysFromAny_TypeMismatchErrorFormat(t *testing.T) {
	pks := []any{int64(1), "not-an-int"}
	_, err := cache.KeysFromAny(pks, func(int64) string { return "" })
	if err == nil {
		t.Fatalf("KeysFromAny() with type mismatch = nil err, want error")
	}
	msg := err.Error()
	for _, want := range []string{"index 1", "int64", "string"} {
		if !strings.Contains(msg, want) {
			t.Errorf("KeysFromAny() error = %q, want substring %q", msg, want)
		}
	}
}

// itoa is a tiny test helper. strconv would work equally well but adds an
// import only used here; the table is small.
func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
