package tests

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	var err error
	// Use a file::memory: database with shared cache so multiple *sql.DB
	// connections see the same schema. An anonymous `:memory:` DSN creates
	// one empty database per connection, which breaks the concurrent-reads
	// tests (hydration dedup, singleflight stampede).
	testDB, err = sql.Open("sqlite", "file:cachetest?mode=memory&cache=shared")
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}
	if _, err := testDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		panic(fmt.Sprintf("enabling foreign keys: %v", err))
	}

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testDB.Exec(string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}

// countingQuerier wraps a database.Querier and counts Query / QueryRow / Exec
// calls. Tests use it to assert that a cached Get() skipped the DB on a hit,
// or that singleflight/hydration collapsed N concurrent misses into one query.
type countingQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64
}

func newCountingQuerier(inner database.Querier) *countingQuerier {
	return &countingQuerier{inner: inner}
}

func (c *countingQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.execs.Add(1)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *countingQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.queries.Add(1)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *countingQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.queryRow.Add(1)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *countingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingQuerier) selectQueries() int64 {
	return c.queries.Load() + c.queryRow.Load()
}

func (c *countingQuerier) reset() {
	c.queries.Store(0)
	c.queryRow.Store(0)
	c.execs.Store(0)
}

// spyBackend is a cache.Backend that wraps an inner backend and records every
// call. Tests inspect the recorded call list for invalidation patterns,
// per-table enable semantics, SkipCache verification, etc.
type spyBackend struct {
	inner cache.Backend

	mu    sync.Mutex
	calls []spyCall

	failMode  atomic.Value // string: "", "get", "set", "invalidate", "all"
	failCount atomic.Int64
}

type spyCall struct {
	op      string // "get", "set", "invalidate", "invalidate_many", "invalidate_pattern"
	key     string
	keys    []string
	pattern string
}

func newSpyBackend(inner cache.Backend) *spyBackend {
	sb := &spyBackend{inner: inner}
	sb.failMode.Store("")
	return sb
}

func (s *spyBackend) setFail(mode string) {
	s.failMode.Store(mode)
}

func (s *spyBackend) record(c spyCall) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
}

func (s *spyBackend) snapshot() []spyCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]spyCall, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *spyBackend) reset() {
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
}

func (s *spyBackend) shouldFail(op string) bool {
	mode, _ := s.failMode.Load().(string)
	if mode == "" {
		return false
	}
	if mode == "all" || mode == op {
		s.failCount.Add(1)
		return true
	}
	return false
}

func (s *spyBackend) Get(ctx context.Context, key string) ([]byte, error) {
	s.record(spyCall{op: "get", key: key})
	if s.shouldFail("get") {
		return nil, fmt.Errorf("spy: forced get failure")
	}
	return s.inner.Get(ctx, key)
}

func (s *spyBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.record(spyCall{op: "set", key: key})
	if s.shouldFail("set") {
		return fmt.Errorf("spy: forced set failure")
	}
	return s.inner.Set(ctx, key, value, ttl)
}

func (s *spyBackend) Invalidate(ctx context.Context, key string) error {
	s.record(spyCall{op: "invalidate", key: key})
	if s.shouldFail("invalidate") {
		return fmt.Errorf("spy: forced invalidate failure")
	}
	return s.inner.Invalidate(ctx, key)
}

func (s *spyBackend) InvalidateMany(ctx context.Context, keys []string) error {
	snapshot := append([]string(nil), keys...)
	s.record(spyCall{op: "invalidate_many", keys: snapshot})
	if s.shouldFail("invalidate") {
		return fmt.Errorf("spy: forced invalidate_many failure")
	}
	return s.inner.InvalidateMany(ctx, keys)
}

func (s *spyBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	s.record(spyCall{op: "invalidate_pattern", pattern: pattern})
	if s.shouldFail("invalidate") {
		return fmt.Errorf("spy: forced invalidate_pattern failure")
	}
	return s.inner.InvalidatePattern(ctx, pattern)
}

// filterCalls returns recorded calls whose op matches one of the given ops.
func (s *spyBackend) filterCalls(ops ...string) []spyCall {
	calls := s.snapshot()
	allowed := make(map[string]bool, len(ops))
	for _, op := range ops {
		allowed[op] = true
	}
	var out []spyCall
	for _, c := range calls {
		if allowed[c.op] {
			out = append(out, c)
		}
	}
	return out
}

// callsForTable returns recorded calls whose key / keys / pattern reference the
// given table segment.
func (s *spyBackend) callsForTable(table string) []spyCall {
	calls := s.snapshot()
	needle := ":" + table + ":"
	var out []spyCall
	for _, c := range calls {
		switch c.op {
		case "get", "set", "invalidate":
			if strings.Contains(c.key, needle) {
				out = append(out, c)
			}
		case "invalidate_many":
			for _, k := range c.keys {
				if strings.Contains(k, needle) {
					out = append(out, c)
					break
				}
			}
		case "invalidate_pattern":
			if strings.Contains(c.pattern, needle) {
				out = append(out, c)
			}
		}
	}
	return out
}

// spyMetrics records every MetricsRecorder call. Tests assert ordering
// (events-before-cache), hydration lifecycle, circuit breaker transitions, and
// error routing.
type spyMetrics struct {
	mu                 sync.Mutex
	hits               []string
	misses             []string
	sets               []string
	invalidates        []string
	errors             []metricError
	hydrationStart     []string
	hydrationComplete  []metricError
	breakerTransitions []breakerTransition
}

type metricError struct {
	schema string
	table  hook.TableName
	op     string
	err    error
}

type breakerTransition struct {
	from cache.CircuitState
	to   cache.CircuitState
}

func (m *spyMetrics) Hit(schema string, table hook.TableName) {
	m.mu.Lock()
	m.hits = append(m.hits, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Miss(schema string, table hook.TableName) {
	m.mu.Lock()
	m.misses = append(m.misses, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Set(schema string, table hook.TableName) {
	m.mu.Lock()
	m.sets = append(m.sets, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Invalidate(schema string, table hook.TableName) {
	m.mu.Lock()
	m.invalidates = append(m.invalidates, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Error(schema string, table hook.TableName, op string, err error) {
	m.mu.Lock()
	m.errors = append(m.errors, metricError{schema: schema, table: table, op: op, err: err})
	m.mu.Unlock()
}

func (m *spyMetrics) HydrationStart(schema string, table hook.TableName) {
	m.mu.Lock()
	m.hydrationStart = append(m.hydrationStart, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) HydrationComplete(schema string, table hook.TableName, err error) {
	m.mu.Lock()
	m.hydrationComplete = append(m.hydrationComplete, metricError{schema: schema, table: table, op: "hydrate", err: err})
	m.mu.Unlock()
}

func (m *spyMetrics) CircuitBreakerStateChange(from, to cache.CircuitState) {
	m.mu.Lock()
	m.breakerTransitions = append(m.breakerTransitions, breakerTransition{from: from, to: to})
	m.mu.Unlock()
}

func (m *spyMetrics) GetLatency(schema string, table hook.TableName, d time.Duration)        {}
func (m *spyMetrics) SetLatency(schema string, table hook.TableName, d time.Duration)        {}
func (m *spyMetrics) InvalidateLatency(schema string, table hook.TableName, d time.Duration) {}

func (m *spyMetrics) hitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.hits)
}

func (m *spyMetrics) missCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.misses)
}

func (m *spyMetrics) waitHydrationCompleteCount(t *testing.T, want int) []metricError {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		if len(m.hydrationComplete) >= want {
			out := make([]metricError, len(m.hydrationComplete))
			copy(out, m.hydrationComplete)
			m.mu.Unlock()
			return out
		}
		m.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t.Fatalf("hydration: got %d completions, want %d", len(m.hydrationComplete), want)
	return nil
}

// testEnv bundles the materials a test needs: client + counting querier + bus +
// spy backend + spy metrics + cache facade. Each test gets a fresh env to
// avoid cross-test contamination.
type testEnv struct {
	client  *models.Client
	counter *countingQuerier
	bus     *memorybus.Bus
	backend *spyBackend
	metrics *spyMetrics
	cache   *models.Cache
}

type envOptions struct {
	cacheOpts  []models.CacheOption
	clientOpts []models.ClientOption
	wireEvents bool
	breaker    *cache.Breaker
}

type envOption func(*envOptions)

func withCacheOpts(opts ...models.CacheOption) envOption {
	return func(o *envOptions) { o.cacheOpts = append(o.cacheOpts, opts...) }
}

func withClientOpts(opts ...models.ClientOption) envOption {
	return func(o *envOptions) { o.clientOpts = append(o.clientOpts, opts...) }
}

func wireEventAdapter() envOption {
	return func(o *envOptions) { o.wireEvents = true }
}

func withBreaker(b *cache.Breaker) envOption {
	return func(o *envOptions) { o.breaker = b }
}

func newEnv(t *testing.T, opts ...envOption) *testEnv {
	t.Helper()

	cfg := envOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}

	innerBackend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = innerBackend.Close() })
	spy := newSpyBackend(innerBackend)
	metrics := &spyMetrics{}

	cacheOpts := []models.CacheOption{
		models.WithMetricsRecorder(metrics),
	}
	if cfg.breaker != nil {
		cacheOpts = append(cacheOpts, models.WithCircuitBreaker(cfg.breaker))
	}

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	if cfg.wireEvents {
		cacheOpts = append(cacheOpts, models.WithInvalidationSource(cache.FromEventSubscriber(bus)))
	}
	cacheOpts = append(cacheOpts, cfg.cacheOpts...)

	c, err := models.NewCache(spy, cacheOpts...)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	counter := newCountingQuerier(dbstdlib.New(testDB))

	clientOpts := []models.ClientOption{
		models.WithEventPublisher(bus),
		models.WithCache(c),
	}
	clientOpts = append(clientOpts, cfg.clientOpts...)

	client := models.New(counter, clientOpts...)
	return &testEnv{
		client:  client,
		counter: counter,
		bus:     bus,
		backend: spy,
		metrics: metrics,
		cache:   c,
	}
}

// newEnvWithMemory wires a real in-memory cache backend (not the spy) for
// tests that want to exercise actual storage behavior.
func newEnvWithMemory(t *testing.T, inner cache.Backend, opts ...envOption) *testEnv {
	t.Helper()

	cfg := envOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}
	spy := newSpyBackend(inner)
	metrics := &spyMetrics{}

	cacheOpts := []models.CacheOption{
		models.WithMetricsRecorder(metrics),
	}
	if cfg.breaker != nil {
		cacheOpts = append(cacheOpts, models.WithCircuitBreaker(cfg.breaker))
	}
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	cacheOpts = append(cacheOpts, cfg.cacheOpts...)

	c, err := models.NewCache(spy, cacheOpts...)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	counter := newCountingQuerier(dbstdlib.New(testDB))

	clientOpts := []models.ClientOption{
		models.WithEventPublisher(bus),
		models.WithCache(c),
	}
	clientOpts = append(clientOpts, cfg.clientOpts...)
	client := models.New(counter, clientOpts...)

	return &testEnv{
		client:  client,
		counter: counter,
		bus:     bus,
		backend: spy,
		metrics: metrics,
		cache:   c,
	}
}

// eventCollector accumulates events delivered to a subscription. Safe for
// concurrent use because async callback mode may dispatch from a goroutine.
type eventCollector struct {
	mu     sync.Mutex
	events []event.Event
}

func (c *eventCollector) handler(_ context.Context, e event.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *eventCollector) snapshot() []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.Event, len(c.events))
	copy(out, c.events)
	return out
}

func (c *eventCollector) waitFor(t *testing.T, n int) []event.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.events) >= n {
			out := make([]event.Event, len(c.events))
			copy(out, c.events)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("waitFor(%d): only %d events delivered", n, len(c.events))
	return nil
}

func subscribeEvents(t *testing.T, bus *memorybus.Bus, opts event.SubscribeOptions) *eventCollector {
	t.Helper()
	ec := &eventCollector{}
	sub, err := bus.Subscribe(opts, ec.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return ec
}

// uniqueSku returns a test-scoped SKU string.
func uniqueSku(t *testing.T, suffix string) string {
	return t.Name() + ":" + suffix
}

// waitUntil polls until pred returns true or deadline elapses.
func waitUntil(t *testing.T, timeout time.Duration, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return pred()
}
