package memory_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/cache/memory"
)

func newBackend(t *testing.T, opts memory.Options) *memory.Backend {
	t.Helper()
	b, err := memory.New(opts)
	if err != nil {
		t.Fatalf("memory.New(%+v) unexpected error: %v", opts, err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestNew_MaxSizeZeroReturnsError(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := memory.New(memory.Options{MaxSize: tt.maxSize})
			if err == nil {
				_ = b.Close()
				t.Fatalf("memory.New(MaxSize=%d) err = nil, want error", tt.maxSize)
			}
			if b != nil {
				t.Errorf("memory.New(MaxSize=%d) backend = %v, want nil", tt.maxSize, b)
			}
			// Error must name the field so operators can diagnose config.
			wantSubstr := "MaxSize"
			if !bytes.Contains([]byte(err.Error()), []byte(wantSubstr)) {
				t.Errorf("memory.New(MaxSize=%d) err = %q, want to contain %q", tt.maxSize, err, wantSubstr)
			}
		})
	}
}

func TestInterfaceAssertions(t *testing.T) {
	b := newBackend(t, memory.Options{MaxSize: 16})

	var _ cache.Backend = b
	var _ cache.StatsReporter = b
	var _ io.Closer = b
}

func TestGetAndSetRoundTrip(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16})

	got, err := b.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get(missing) err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() err = %v, want nil", err)
	}
	got, err = b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get(k) err = %v, want nil", err)
	}
	if !bytes.Equal(got, []byte("v")) {
		t.Errorf("Get(k) = %q, want %q", got, "v")
	}
}

func TestSetPerEntryTTLExpires(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16})

	// Long TTL entry — must survive the short sleep.
	if err := b.Set(ctx, "long", []byte("L"), time.Hour); err != nil {
		t.Fatalf("Set(long) err = %v", err)
	}
	// Short TTL entry — must expire.
	if err := b.Set(ctx, "short", []byte("S"), 50*time.Millisecond); err != nil {
		t.Fatalf("Set(short) err = %v", err)
	}

	got, err := b.Get(ctx, "short")
	if err != nil {
		t.Fatalf("Get(short) err = %v", err)
	}
	if !bytes.Equal(got, []byte("S")) {
		t.Errorf("Get(short) = %q, want %q (pre-expiry)", got, "S")
	}

	time.Sleep(150 * time.Millisecond)

	got, err = b.Get(ctx, "short")
	if err != nil {
		t.Fatalf("Get(short) post-expiry err = %v", err)
	}
	if got != nil {
		t.Errorf("Get(short) post-expiry = %q, want nil (expired)", got)
	}

	got, err = b.Get(ctx, "long")
	if err != nil {
		t.Fatalf("Get(long) err = %v", err)
	}
	if !bytes.Equal(got, []byte("L")) {
		t.Errorf("Get(long) = %q, want %q (not expired)", got, "L")
	}
}

func TestSetZeroTTLUsesDefaultTTL(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16, DefaultTTL: 40 * time.Millisecond})

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() err = %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if got != nil {
		t.Errorf("Get() = %q, want nil (expired under DefaultTTL)", got)
	}
}

func TestSetZeroTTLWithZeroDefaultNeverExpires(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16}) // DefaultTTL == 0

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() err = %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if !bytes.Equal(got, []byte("v")) {
		t.Errorf("Get() = %q, want %q (must not expire)", got, "v")
	}
}

func TestInvalidateRemovesPresentAndIgnoresAbsent(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16})

	if err := b.Set(ctx, "k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Set() err = %v", err)
	}
	if err := b.Invalidate(ctx, "k"); err != nil {
		t.Fatalf("Invalidate(present) err = %v, want nil", err)
	}
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if got != nil {
		t.Errorf("Get() after Invalidate = %q, want nil", got)
	}
	if err := b.Invalidate(ctx, "absent"); err != nil {
		t.Errorf("Invalidate(absent) err = %v, want nil (absent is not an error)", err)
	}
}

func TestInvalidateManyRemovesPresentAndIgnoresAbsent(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16})

	if err := b.Set(ctx, "a", []byte("A"), time.Hour); err != nil {
		t.Fatalf("Set(a) err = %v", err)
	}
	if err := b.Set(ctx, "b", []byte("B"), time.Hour); err != nil {
		t.Fatalf("Set(b) err = %v", err)
	}

	if err := b.InvalidateMany(ctx, []string{"a", "b", "missing"}); err != nil {
		t.Fatalf("InvalidateMany() err = %v, want nil", err)
	}

	for _, k := range []string{"a", "b"} {
		got, err := b.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
		if got != nil {
			t.Errorf("Get(%q) after InvalidateMany = %q, want nil", k, got)
		}
	}
}

func TestInvalidatePattern(t *testing.T) {
	ctx := context.Background()

	t.Run("removes matching prefix", func(t *testing.T) {
		b := newBackend(t, memory.Options{MaxSize: 32})

		entries := map[string][]byte{
			"sqlgen:public.products:pk:1": []byte("1"),
			"sqlgen:public.products:pk:2": []byte("2"),
			"sqlgen:public.orders:pk:9":   []byte("9"),
		}
		for k, v := range entries {
			if err := b.Set(ctx, k, v, time.Hour); err != nil {
				t.Fatalf("Set(%q) err = %v", k, err)
			}
		}

		if err := b.InvalidatePattern(ctx, "sqlgen:public.products:*"); err != nil {
			t.Fatalf("InvalidatePattern() err = %v, want nil", err)
		}

		for _, k := range []string{"sqlgen:public.products:pk:1", "sqlgen:public.products:pk:2"} {
			got, err := b.Get(ctx, k)
			if err != nil {
				t.Fatalf("Get(%q) err = %v", k, err)
			}
			if got != nil {
				t.Errorf("Get(%q) after pattern = %q, want nil", k, got)
			}
		}

		// Non-matching key must survive.
		got, err := b.Get(ctx, "sqlgen:public.orders:pk:9")
		if err != nil {
			t.Fatalf("Get(orders) err = %v", err)
		}
		if !bytes.Equal(got, []byte("9")) {
			t.Errorf("Get(orders) = %q, want %q (pattern should not match)", got, "9")
		}
	})

	t.Run("missing trailing star errors", func(t *testing.T) {
		b := newBackend(t, memory.Options{MaxSize: 16})
		if err := b.InvalidatePattern(ctx, "sqlgen:products"); err == nil {
			t.Errorf("InvalidatePattern(no *) err = nil, want error")
		}
	})
}

func TestStatsReportsActivity(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 32, StatsEnabled: true})

	// 2 sets.
	if err := b.Set(ctx, "a", []byte("A"), time.Hour); err != nil {
		t.Fatalf("Set(a) err = %v", err)
	}
	if err := b.Set(ctx, "b", []byte("B"), time.Hour); err != nil {
		t.Fatalf("Set(b) err = %v", err)
	}
	// 1 hit.
	if _, err := b.Get(ctx, "a"); err != nil {
		t.Fatalf("Get(a) err = %v", err)
	}
	// 1 miss.
	if _, err := b.Get(ctx, "missing"); err != nil {
		t.Fatalf("Get(missing) err = %v", err)
	}
	// 1 invalidation.
	if err := b.Invalidate(ctx, "b"); err != nil {
		t.Fatalf("Invalidate(b) err = %v", err)
	}

	got := b.Stats()
	if got.Hits == 0 {
		t.Errorf("Stats().Hits = 0, want > 0")
	}
	if got.Misses == 0 {
		t.Errorf("Stats().Misses = 0, want > 0")
	}
	if got.Sets != 2 {
		t.Errorf("Stats().Sets = %d, want 2", got.Sets)
	}
	if got.Invalidations != 1 {
		t.Errorf("Stats().Invalidations = %d, want 1", got.Invalidations)
	}
}

func TestStatsDisabledSuppressesOtterCounters(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 16}) // StatsEnabled: false

	if err := b.Set(ctx, "a", []byte("A"), time.Hour); err != nil {
		t.Fatalf("Set(a) err = %v", err)
	}
	if _, err := b.Get(ctx, "a"); err != nil {
		t.Fatalf("Get(a) err = %v", err)
	}
	if _, err := b.Get(ctx, "missing"); err != nil {
		t.Fatalf("Get(missing) err = %v", err)
	}

	got := b.Stats()
	if got.Hits != 0 || got.Misses != 0 || got.Evictions != 0 {
		t.Errorf("Stats() otter counters = %+v, want all zero when StatsEnabled is false", got)
	}
	if got.Sets != 1 {
		t.Errorf("Stats().Sets = %d, want 1 (tracked regardless of StatsEnabled)", got.Sets)
	}
}

func TestCloseIsIdempotentAndBlocksFurtherCalls(t *testing.T) {
	ctx := context.Background()
	b, err := memory.New(memory.Options{MaxSize: 16})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}

	if err := b.Set(ctx, "k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Set() err = %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("first Close() err = %v, want nil", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close() err = %v, want nil (idempotent)", err)
	}

	if _, err := b.Get(ctx, "k"); !errors.Is(err, memory.ErrClosed) {
		t.Errorf("Get after Close err = %v, want ErrClosed", err)
	}
	if err := b.Set(ctx, "k2", []byte("v"), time.Hour); !errors.Is(err, memory.ErrClosed) {
		t.Errorf("Set after Close err = %v, want ErrClosed", err)
	}
	if err := b.Invalidate(ctx, "k"); !errors.Is(err, memory.ErrClosed) {
		t.Errorf("Invalidate after Close err = %v, want ErrClosed", err)
	}
	if err := b.InvalidateMany(ctx, []string{"k"}); !errors.Is(err, memory.ErrClosed) {
		t.Errorf("InvalidateMany after Close err = %v, want ErrClosed", err)
	}
	if err := b.InvalidatePattern(ctx, "k*"); !errors.Is(err, memory.ErrClosed) {
		t.Errorf("InvalidatePattern after Close err = %v, want ErrClosed", err)
	}
}

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t, memory.Options{MaxSize: 1024, StatsEnabled: true})

	const workers = 100
	const opsPerWorker = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(id int) {
			defer wg.Done()
			for i := range opsPerWorker {
				key := "w:" + strconv.Itoa(id) + ":k:" + strconv.Itoa(i%8)
				val := []byte(strconv.Itoa(i))
				switch i % 4 {
				case 0:
					_ = b.Set(ctx, key, val, time.Hour)
				case 1:
					_, _ = b.Get(ctx, key)
				case 2:
					_ = b.Invalidate(ctx, key)
				case 3:
					_ = b.InvalidatePattern(ctx, "w:"+strconv.Itoa(id)+":*")
				}
			}
		}(w)
	}
	wg.Wait()

	// Stats call under concurrent reads must not race either.
	_ = b.Stats()
}
