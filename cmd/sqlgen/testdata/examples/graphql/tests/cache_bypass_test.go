package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// PRD §26.11 — `Cache-Control: no-cache` header → CallOptions.SkipCache=true
// for the duration of the request. PRD §27.4 — cache is wired as the
// outermost hook, so a hit short-circuits before the DB round-trip. PRD §27.8
// — hydration is enabled by default, so a partial fetch (the natural shape
// of every GraphQL query, since gqlgen derives FieldOptions from the
// selection set) misses-then-hydrates: synchronous partial DB read, async
// background fetch of the full entity, then the entity is written to cache.
// The second identical call then hits.
//
// We assert via:
//   - `spyMetrics` — counts Hit / Miss / Set so the cache contract is pinned
//     independent of pgx round-trip details.
//   - `spyBackend` — exposes `waitForSet` so the second call doesn't race
//     the background hydration goroutine (the first observable Set is the
//     hydration completing; before that, the second call would also miss).
//   - The standalone DB-counter on the bypass path — the no-cache request
//     produces a fresh round-trip even after the cache is warm, so the
//     counter delta is a load-bearing signal that the bypass short-circuit
//     fired through to the DB.

// spyBackend wraps a cache.Backend and counts Set invocations so tests can
// poll for hydration completion before issuing the "expect a hit" probe.
type spyBackend struct {
	inner    cache.Backend
	setCount atomic.Int64
}

func (s *spyBackend) Get(ctx context.Context, key string) ([]byte, error) {
	return s.inner.Get(ctx, key)
}

func (s *spyBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	err := s.inner.Set(ctx, key, value, ttl)
	if err == nil {
		s.setCount.Add(1)
	}
	return err
}

func (s *spyBackend) Invalidate(ctx context.Context, key string) error {
	return s.inner.Invalidate(ctx, key)
}

func (s *spyBackend) InvalidateMany(ctx context.Context, keys []string) error {
	return s.inner.InvalidateMany(ctx, keys)
}

func (s *spyBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	return s.inner.InvalidatePattern(ctx, pattern)
}

func (s *spyBackend) waitForSet(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.setCount.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited 2s for %d Set calls, observed %d", want, s.setCount.Load())
}

// cacheSpyMetrics records cache.MetricsRecorder calls so tests can assert
// Hit / Miss counts directly without inferring them from DB round-trips.
type cacheSpyMetrics struct {
	mu     sync.Mutex
	hits   int
	misses int
	sets   int
}

func (m *cacheSpyMetrics) Hit(_ string, _ hook.TableName) {
	m.mu.Lock()
	m.hits++
	m.mu.Unlock()
}

func (m *cacheSpyMetrics) Miss(_ string, _ hook.TableName) {
	m.mu.Lock()
	m.misses++
	m.mu.Unlock()
}

func (m *cacheSpyMetrics) Set(_ string, _ hook.TableName) {
	m.mu.Lock()
	m.sets++
	m.mu.Unlock()
}

func (m *cacheSpyMetrics) Invalidate(_ string, _ hook.TableName)                  {}
func (m *cacheSpyMetrics) Error(_ string, _ hook.TableName, _ string, _ error)    {}
func (m *cacheSpyMetrics) HydrationStart(_ string, _ hook.TableName)              {}
func (m *cacheSpyMetrics) HydrationComplete(_ string, _ hook.TableName, _ error)  {}
func (m *cacheSpyMetrics) CircuitBreakerStateChange(_, _ cache.CircuitState)      {}
func (m *cacheSpyMetrics) GetLatency(_ string, _ hook.TableName, _ time.Duration) {}
func (m *cacheSpyMetrics) SetLatency(_ string, _ hook.TableName, _ time.Duration) {}
func (m *cacheSpyMetrics) InvalidateLatency(_ string, _ hook.TableName, _ time.Duration) {
}

func (m *cacheSpyMetrics) snapshot() (hits, misses, sets int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits, m.misses, m.sets
}

// newCachedHandler boots a parallel gqlgen handler stack wired with an
// in-memory cache backend (spy-wrapped), a metrics spy, and a counting
// querier. The standard `testServer` is reused for seeding so its queries
// aren't counted — only requests against `httpsrv.URL` are.
func newCachedHandler(t *testing.T) (string, *countingQuerier, *spyBackend, *cacheSpyMetrics) {
	t.Helper()

	innerBackend, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = innerBackend.Close() })

	backend := &spyBackend{inner: innerBackend}
	metrics := &cacheSpyMetrics{}

	c, err := models.NewCache(backend, models.WithMetricsRecorder(metrics))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	counter := &countingQuerier{inner: dbpgx.New(testPool)}
	client := models.New(counter, models.WithCache(c))

	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	srv := handler.NewDefaultServer(es)
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL, counter, backend, metrics
}

// postGQLAt mirrors postGQL but accepts request headers — the cache-bypass
// path is driven via the `Cache-Control: no-cache` header through the live
// generated middleware (PRD §26.11). Kept local to this file because the
// only header-aware caller is the bypass test.
func postGQLAt(t *testing.T, url, query string, vars map[string]any, headers map[string]string) gqlResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", resp.StatusCode, raw)
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response (raw=%s): %v", raw, err)
	}
	return out
}

// TestCache_BaselineHit pins the §27.7 cache contract — first query lands a
// miss (partial DB read + async hydration), the hydrated full entity then
// populates the cache, second identical query is a hit. Each phase is
// asserted via the spyMetrics counters; we use the spy backend's Set count
// to gate the "expect hit" probe so the test isn't racing the background
// hydration goroutine.
func TestCache_BaselineHit(t *testing.T) {
	truncateAll(t)
	u := seedUser(t, "cache-baseline@example.com", "CacheBaseline")

	url, _, backend, metrics := newCachedHandler(t)

	q := `query ($id: UUID!) { user(id: $id) { id email name } }`
	vars := map[string]any{"id": u.ID}

	// First call — cache miss + DB roundtrip + async hydration. The
	// synchronous return carries the partial result; hydration writes the
	// full entity to the cache backend on a goroutine.
	resp := postGQL(t, url, q, vars)
	if len(resp.Errors) != 0 {
		t.Fatalf("first query errors: %+v", resp.Errors)
	}
	hits, misses, _ := metrics.snapshot()
	if hits != 0 || misses != 1 {
		t.Fatalf("first call: hits=%d misses=%d, want hits=0 misses=1", hits, misses)
	}

	// Wait for the hydration goroutine's cache Set before asserting the
	// second call is a hit.
	backend.waitForSet(t, 1)

	// Second call — full entity sits in the cache. Probe hits, no extra
	// miss is recorded.
	resp = postGQL(t, url, q, vars)
	if len(resp.Errors) != 0 {
		t.Fatalf("second query errors: %+v", resp.Errors)
	}
	hits, misses, _ = metrics.snapshot()
	if hits != 1 || misses != 1 {
		t.Errorf("second call: hits=%d misses=%d, want hits=1 misses=1", hits, misses)
	}
}

// TestCache_NoCacheHeaderBypasses pins the §26.11 + §27.7 contract
// end-to-end: `Cache-Control: no-cache` translates to
// CallOptions.SkipCache=true via the generated middleware, which short-
// circuits the cache hook BEFORE the probe — so neither a Hit nor a Miss
// is recorded, and the DB is consulted directly. We assert:
//
//   - The bypass call records no additional Hit / Miss (the cache hook
//     never reached the probe).
//   - The counting querier observes one additional DB round-trip on the
//     bypass call (the request did reach the DB).
func TestCache_NoCacheHeaderBypasses(t *testing.T) {
	truncateAll(t)
	u := seedUser(t, "cache-bypass@example.com", "CacheBypass")

	url, counter, backend, metrics := newCachedHandler(t)

	q := `query ($id: UUID!) { user(id: $id) { id email name } }`
	vars := map[string]any{"id": u.ID}

	// Warm the cache.
	resp := postGQL(t, url, q, vars)
	if len(resp.Errors) != 0 {
		t.Fatalf("warm-up errors: %+v", resp.Errors)
	}
	backend.waitForSet(t, 1)

	// Sanity: the next call hits the cache (no DB round-trip beyond what
	// the warm-up + hydration already produced; new Miss count = 0).
	resp = postGQL(t, url, q, vars)
	if len(resp.Errors) != 0 {
		t.Fatalf("hit-confirm errors: %+v", resp.Errors)
	}
	hits, misses, _ := metrics.snapshot()
	if hits != 1 {
		t.Fatalf("hit-confirm: hits=%d, want 1", hits)
	}

	// Pin the counter BEFORE the bypass call. Anything that follows is
	// caused by the bypass path alone.
	dbBefore := counter.count()

	// Bypass — Cache-Control: no-cache must produce a DB round-trip even
	// though a cached entity exists.
	resp = postGQLAt(t, url, q, vars, map[string]string{"Cache-Control": "no-cache"})
	if len(resp.Errors) != 0 {
		t.Fatalf("bypass errors: %+v", resp.Errors)
	}

	dbAfter := counter.count()
	if dbAfter <= dbBefore {
		t.Errorf("bypass: db counter unchanged (%d → %d); want a fresh round-trip", dbBefore, dbAfter)
	}

	// Cache hook was short-circuited — no new Hit, no new Miss recorded.
	hitsAfter, missesAfter, _ := metrics.snapshot()
	if hitsAfter != hits {
		t.Errorf("bypass: hits=%d, want unchanged %d (cache hook should not have probed)", hitsAfter, hits)
	}
	if missesAfter != misses {
		t.Errorf("bypass: misses=%d, want unchanged %d (cache hook should not have probed)", missesAfter, misses)
	}
}
