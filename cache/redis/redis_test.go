package redis_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	cacheredis "github.com/teandresmith/sqlgen/cache/redis"
)

// All Backend tests run against a real Redis server spun up via
// testcontainers. The fake-Redis (miniredis) layer was removed once the
// integration suite landed — real SCAN pagination, real EXPIRE
// semantics, real DEL batching, and race-detector coverage of the Redis
// network path subsume what the fake offered.
//
// `go test -short ./cache/redis/...` skips the whole suite; CI runs
// without -short.

var (
	intClient    *goredis.Client
	intContainer *tcredis.RedisContainer
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		panic(fmt.Errorf("start redis container: %w", err))
	}
	intContainer = container

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Errorf("redis connection string: %w", err))
	}
	opts, err := goredis.ParseURL(connStr)
	if err != nil {
		panic(fmt.Errorf("parse redis URL: %w", err))
	}
	intClient = goredis.NewClient(opts)

	code := m.Run()

	_ = intClient.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

// commandRecorder is a go-redis hook that records every command flowing
// through the client. Tests use it to assert command counts, variadic
// DEL batching, and the absence of KEYS calls.
type commandRecorder struct {
	mu       sync.Mutex
	commands []recordedCommand
}

type recordedCommand struct {
	name string
	args []any
}

func (r *commandRecorder) DialHook(next goredis.DialHook) goredis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (r *commandRecorder) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		r.record(cmd)
		return next(ctx, cmd)
	}
}

func (r *commandRecorder) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			r.record(cmd)
		}
		return next(ctx, cmds)
	}
}

func (r *commandRecorder) record(cmd goredis.Cmder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, recordedCommand{name: cmd.Name(), args: cmd.Args()})
}

func (r *commandRecorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.commands {
		if strings.EqualFold(c.name, name) {
			n++
		}
	}
	return n
}

func (r *commandRecorder) findByName(name string) []recordedCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []recordedCommand
	for _, c := range r.commands {
		if strings.EqualFold(c.name, name) {
			out = append(out, c)
		}
	}
	return out
}

// newBackend returns a Backend wired to the shared integration container
// with a fresh recorder hook. Every test calls FLUSHDB first so it starts
// from a clean keyspace; the hook is attached AFTER the flush so the
// recorder only captures test-initiated commands.
func newBackend(t *testing.T, opts ...cacheredis.Option) (*cacheredis.Backend, *goredis.Client, *commandRecorder) {
	t.Helper()
	ctx := context.Background()

	if err := intClient.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FLUSHDB: %v", err)
	}

	connStr, err := intContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	parsed, err := goredis.ParseURL(connStr)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	client := goredis.NewClient(parsed)
	rec := &commandRecorder{}
	client.AddHook(rec)
	t.Cleanup(func() { _ = client.Close() })

	b := cacheredis.New(client, opts...)
	t.Cleanup(func() { _ = b.Close() })
	return b, client, rec
}

func TestGetSetRoundTrip(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() err = %v, want nil", err)
	}
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get() err = %v, want nil", err)
	}
	if !bytes.Equal(got, []byte("v")) {
		t.Errorf("Get() = %q, want %q", got, "v")
	}
}

func TestGetMissReturnsNilNil(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	got, err := b.Get(ctx, "absent")
	if err != nil {
		t.Fatalf("Get(absent) err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Get(absent) = %q, want nil (redis.Nil must map to miss)", got)
	}
}

func TestSetWithTTLExpires(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "k", []byte("v"), 500*time.Millisecond); err != nil {
		t.Fatalf("Set() err = %v", err)
	}

	// Present before expiry.
	got, err := b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get(pre-expiry) err = %v", err)
	}
	if !bytes.Equal(got, []byte("v")) {
		t.Errorf("Get(pre-expiry) = %q, want %q", got, "v")
	}

	// Real Redis EXPIRE — wait out the TTL.
	time.Sleep(750 * time.Millisecond)

	got, err = b.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get(post-expiry) err = %v", err)
	}
	if got != nil {
		t.Errorf("Get(post-expiry) = %q, want nil (EXPIRE must elapse)", got)
	}
}

func TestSetZeroTTLDoesNotExpire(t *testing.T) {
	b, client, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set() err = %v", err)
	}

	// TTL command returns -1 when the key has no expiration, -2 when
	// the key is missing. -1 proves Set without EX landed on Redis.
	dur, err := client.TTL(ctx, "k").Result()
	if err != nil {
		t.Fatalf("TTL() err = %v", err)
	}
	if dur != -1*time.Nanosecond {
		t.Errorf("TTL() = %v, want -1ns (no expiry)", dur)
	}
}

func TestPerEntryTTLsExpireIndependently(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "short", []byte("s"), 500*time.Millisecond); err != nil {
		t.Fatalf("Set(short) err = %v", err)
	}
	if err := b.Set(ctx, "long", []byte("l"), 5*time.Second); err != nil {
		t.Fatalf("Set(long) err = %v", err)
	}

	// After the short TTL, short is gone but long is still alive.
	time.Sleep(750 * time.Millisecond)

	gotShort, err := b.Get(ctx, "short")
	if err != nil {
		t.Fatalf("Get(short) err = %v", err)
	}
	if gotShort != nil {
		t.Errorf("Get(short) post-expiry = %q, want nil", gotShort)
	}

	gotLong, err := b.Get(ctx, "long")
	if err != nil {
		t.Fatalf("Get(long) err = %v", err)
	}
	if !bytes.Equal(gotLong, []byte("l")) {
		t.Errorf("Get(long) = %q, want %q (longer TTL must not have expired)", gotLong, "l")
	}
}

func TestInvalidateRemovesPresentAndIgnoresAbsent(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "k", []byte("v"), 0); err != nil {
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

func TestInvalidateManyIssuesSingleVariadicDEL(t *testing.T) {
	b, _, rec := newBackend(t)
	ctx := context.Background()

	for _, k := range []string{"a", "b", "c"} {
		if err := b.Set(ctx, k, []byte("x"), 0); err != nil {
			t.Fatalf("Set(%q) err = %v", k, err)
		}
	}

	before := rec.count("del")
	if err := b.InvalidateMany(ctx, []string{"a", "b", "c", "missing"}); err != nil {
		t.Fatalf("InvalidateMany() err = %v, want nil", err)
	}
	after := rec.count("del")
	if got := after - before; got != 1 {
		t.Errorf("DEL call count = %d, want 1 (InvalidateMany must use a single variadic DEL)", got)
	}

	// Args for the DEL call should include the command + all four keys.
	dels := rec.findByName("del")
	if len(dels) == 0 {
		t.Fatalf("no DEL command recorded")
	}
	last := dels[len(dels)-1]
	wantArgs := []any{"del", "a", "b", "c", "missing"}
	if len(last.args) != len(wantArgs) {
		t.Fatalf("DEL args length = %d, want %d: got %v", len(last.args), len(wantArgs), last.args)
	}
	for i := range wantArgs {
		if last.args[i] != wantArgs[i] {
			t.Errorf("DEL args[%d] = %v, want %v", i, last.args[i], wantArgs[i])
		}
	}

	for _, k := range []string{"a", "b", "c"} {
		got, err := b.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
		if got != nil {
			t.Errorf("Get(%q) after InvalidateMany = %q, want nil", k, got)
		}
	}
}

func TestInvalidateManyEmptyIsNoOp(t *testing.T) {
	b, _, rec := newBackend(t)
	ctx := context.Background()

	if err := b.InvalidateMany(ctx, nil); err != nil {
		t.Errorf("InvalidateMany(nil) err = %v, want nil", err)
	}
	if got := rec.count("del"); got != 0 {
		t.Errorf("DEL count = %d, want 0 for empty InvalidateMany", got)
	}
}

func TestInvalidatePatternWith10KKeysUsesSCANAndBatchedDELs(t *testing.T) {
	// Use the default scan count so we observe real DEL batching —
	// 10_000 keys / 500 COUNT ≈ 20 SCAN pages ≈ 20+ DEL calls.
	b, _, rec := newBackend(t)
	ctx := context.Background()

	const matching = 10_000
	// Pipeline the seed SETs through the admin client to keep setup
	// time bounded; intClient is hookless, so these SETs are invisible
	// to the test's recorder.
	pipe := intClient.Pipeline()
	for i := range matching {
		pipe.Set(ctx, "sqlgen:public.products:pk:"+strconv.Itoa(i), []byte("v"), 0)
	}
	// Non-matching keys that must survive.
	for i := range 10 {
		pipe.Set(ctx, "sqlgen:public.orders:pk:"+strconv.Itoa(i), []byte("o"), 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}

	if err := b.InvalidatePattern(ctx, "sqlgen:public.products:*"); err != nil {
		t.Fatalf("InvalidatePattern() err = %v", err)
	}

	// KEYS is production-unsafe — the whole point of SCAN. Regression
	// guard: pattern deletion must NEVER fall back to KEYS.
	if got := rec.count("keys"); got != 0 {
		t.Errorf("KEYS call count = %d, want 0 (production safety — KEYS blocks the server)", got)
	}

	// SCAN pagination observed — 10k keys against COUNT=500 defaults
	// means the cursor must iterate more than once.
	if got := rec.count("scan"); got < 2 {
		t.Errorf("SCAN call count = %d, want >= 2 (10k keys must require paginated SCAN)", got)
	}

	// DEL batching observed — one DEL per SCAN page, not a single
	// unbounded variadic. With 10k keys we expect multiple DEL calls.
	dels := rec.findByName("del")
	if len(dels) < 2 {
		t.Errorf("DEL call count = %d, want >= 2 (InvalidatePattern must batch DELs per SCAN page)", len(dels))
	}
	// No single DEL should carry all 10k keys. Args layout is
	// ["del", key1, key2, ...] so len(args)-1 is the key count per call.
	for i, d := range dels {
		n := len(d.args) - 1
		if n > 2*cacheredis.DefaultScanCount {
			t.Errorf("DEL[%d] carried %d keys, want <= %d (batch must stay bounded by scan page size)", i, n, 2*cacheredis.DefaultScanCount)
		}
	}

	// Every matching key gone (sample every 500th to keep the test fast).
	for i := 0; i < matching; i += 500 {
		k := "sqlgen:public.products:pk:" + strconv.Itoa(i)
		got, err := b.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
		if got != nil {
			t.Errorf("Get(%q) after pattern = %q, want nil", k, got)
		}
	}

	// Non-matching keys survived.
	for i := range 10 {
		k := "sqlgen:public.orders:pk:" + strconv.Itoa(i)
		got, err := b.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
		if !bytes.Equal(got, []byte("o")) {
			t.Errorf("Get(%q) = %q, want %q (pattern must not match)", k, got, "o")
		}
	}
}

func TestInvalidatePatternWithSmallScanCount(t *testing.T) {
	b, _, rec := newBackend(t, cacheredis.WithScanCount(10))
	ctx := context.Background()

	const matching = 50
	for i := range matching {
		k := "p:" + strconv.Itoa(i)
		if err := b.Set(ctx, k, []byte("v"), 0); err != nil {
			t.Fatalf("Set(%q) err = %v", k, err)
		}
	}

	if err := b.InvalidatePattern(ctx, "p:*"); err != nil {
		t.Fatalf("InvalidatePattern() err = %v", err)
	}

	// Every matching key must be gone.
	for i := range matching {
		k := "p:" + strconv.Itoa(i)
		got, err := b.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
		if got != nil {
			t.Errorf("Get(%q) = %q, want nil (pattern must still complete with small COUNT)", k, got)
		}
	}

	// COUNT is advisory — assert WithScanCount(10) flowed through to
	// the SCAN command's COUNT argument. Args layout is
	// ["scan", cursor, "match", pattern, "count", N].
	scans := rec.findByName("scan")
	if len(scans) == 0 {
		t.Fatalf("no SCAN command recorded")
	}
	var sawCount bool
	for _, s := range scans {
		for i := 0; i+1 < len(s.args); i++ {
			name, ok := s.args[i].(string)
			if !ok || !strings.EqualFold(name, "count") {
				continue
			}
			if n, ok := s.args[i+1].(int64); ok && n == 10 {
				sawCount = true
			}
		}
	}
	if !sawCount {
		t.Errorf("SCAN args did not carry COUNT=10: recorded = %+v", scans)
	}
}

func TestConcurrentInvalidateManyGetSet(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := t.Context()

	const (
		workers = 8
		iters   = 200
	)
	var (
		wg       sync.WaitGroup
		errCount atomic.Uint64
	)

	// Writers fill a rolling window of keys.
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range iters {
				key := fmt.Sprintf("race:w%d:%d", id, i)
				if err := b.Set(ctx, key, []byte("v"), 0); err != nil {
					errCount.Add(1)
					return
				}
			}
		}(w)
	}

	// Readers race the writers — misses and hits both valid.
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range iters {
				key := fmt.Sprintf("race:w%d:%d", id, i)
				if _, err := b.Get(ctx, key); err != nil {
					errCount.Add(1)
					return
				}
			}
		}(w)
	}

	// Invalidators tear down batches of keys from each worker's namespace.
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i += 10 {
				batch := make([]string, 0, 10)
				for j := range 10 {
					batch = append(batch, fmt.Sprintf("race:w%d:%d", id, i+j))
				}
				if err := b.InvalidateMany(ctx, batch); err != nil {
					errCount.Add(1)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	if got := errCount.Load(); got != 0 {
		t.Errorf("concurrent operations produced %d errors, want 0", got)
	}
}

func TestStatsWithLocalStatsEnabled(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()

	if err := b.Set(ctx, "a", []byte("A"), 0); err != nil {
		t.Fatalf("Set(a) err = %v", err)
	}
	if err := b.Set(ctx, "b", []byte("B"), 0); err != nil {
		t.Fatalf("Set(b) err = %v", err)
	}
	if _, err := b.Get(ctx, "a"); err != nil {
		t.Fatalf("Get(a) err = %v", err)
	}
	if _, err := b.Get(ctx, "missing"); err != nil {
		t.Fatalf("Get(missing) err = %v", err)
	}
	if err := b.Invalidate(ctx, "b"); err != nil {
		t.Fatalf("Invalidate(b) err = %v", err)
	}

	got := b.Stats()
	if got.Sets != 2 {
		t.Errorf("Stats().Sets = %d, want 2", got.Sets)
	}
	if got.Hits != 1 {
		t.Errorf("Stats().Hits = %d, want 1", got.Hits)
	}
	if got.Misses != 1 {
		t.Errorf("Stats().Misses = %d, want 1", got.Misses)
	}
	if got.Invalidations != 1 {
		t.Errorf("Stats().Invalidations = %d, want 1", got.Invalidations)
	}
	// Real Redis reports INFO keyspace; one key remains (a) after the
	// invalidation of b, so Entries must be positive.
	if got.Entries == 0 {
		t.Errorf("Stats().Entries = 0, want > 0 (INFO keyspace must report live keys)")
	}
}

func TestStatsWithLocalStatsDisabled(t *testing.T) {
	b, _, _ := newBackend(t, cacheredis.WithLocalStats(false))
	ctx := context.Background()

	if err := b.Set(ctx, "a", []byte("A"), 0); err != nil {
		t.Fatalf("Set() err = %v", err)
	}
	if _, err := b.Get(ctx, "a"); err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if _, err := b.Get(ctx, "missing"); err != nil {
		t.Fatalf("Get(missing) err = %v", err)
	}
	if err := b.Invalidate(ctx, "a"); err != nil {
		t.Fatalf("Invalidate() err = %v", err)
	}

	got := b.Stats()
	if got.Hits != 0 || got.Misses != 0 || got.Sets != 0 || got.Invalidations != 0 {
		t.Errorf("Stats() with WithLocalStats(false) = %+v, want all counters zero", got)
	}
}

func TestCloseDefaultIsNoOpClientStaysUsable(t *testing.T) {
	ctx := context.Background()
	connStr, err := intContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	opts, err := goredis.ParseURL(connStr)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	client := goredis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	b := cacheredis.New(client) // no WithOwnedClient

	if err := b.Close(); err != nil {
		t.Fatalf("Close() err = %v, want nil", err)
	}
	// Second Close is idempotent.
	if err := b.Close(); err != nil {
		t.Errorf("second Close() err = %v, want nil (idempotent)", err)
	}

	// Client must still be usable — ownership was not transferred.
	if err := client.Ping(ctx).Err(); err != nil {
		t.Errorf("client.Ping after Backend Close err = %v, want nil (no-op Close must not close client)", err)
	}
}

func TestCloseOwnedClientClosesClient(t *testing.T) {
	ctx := context.Background()
	connStr, err := intContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	opts, err := goredis.ParseURL(connStr)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	client := goredis.NewClient(opts)
	b := cacheredis.New(client, cacheredis.WithOwnedClient())

	if err := b.Close(); err != nil {
		t.Fatalf("Close() err = %v, want nil", err)
	}

	// Once closed, the client rejects new operations.
	err = client.Ping(ctx).Err()
	if err == nil {
		t.Errorf("client.Ping after owned Close err = nil, want error (owned client must be closed)")
	} else if !errors.Is(err, goredis.ErrClosed) && !strings.Contains(err.Error(), "closed") {
		t.Errorf("client.Ping after owned Close err = %v, want ErrClosed-like error", err)
	}

	// Second Close remains idempotent.
	if err := b.Close(); err != nil {
		t.Errorf("second Close() err = %v, want nil (idempotent)", err)
	}
}
