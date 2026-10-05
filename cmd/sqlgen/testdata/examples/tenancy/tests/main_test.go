// Package tests is the E2E suite for the tenancy example. It exercises every
// observable property of sqlgen's tenancy primitive (PRD §29) end-to-end against
// a real SQLite database: filter injection on reads, mutation auto-set + the
// mismatch check, missing-tenant fail-closed, the SkipTenancy admin path,
// composition with soft delete, cache scoping by tenant + per-tenant pattern
// invalidation, event metadata propagation, relationship propagation across
// o2o / o2m / m2m, and transaction propagation.
//
// SQLite was chosen as the dialect because the runtime tenancy primitive is
// dialect-portable (proven by unit tests in cache/key_test.go and the generator
// tests in cmd/sqlgen/gen/) and SQLite is available without Docker, so the suite
// can run under `go test -short`-friendly conditions in CI. Cross-dialect SQL
// portability of the generated code itself is covered by the existing
// postgres/, mysql/, sqlite/ example suites.
package tests

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

var testDB *sql.DB

// Two stable tenant IDs are reused across every test. Tests reset the database
// between runs (TRUNCATE-style DELETE) so cross-test row state never leaks; the
// tenant IDs themselves are constants so test failures are easy to interpret.
var (
	tenantA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tenantB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	var err error
	// file::memory: + cache=shared so multiple *sql.DB connections see the
	// same schema. An anonymous `:memory:` DSN gives one empty database per
	// connection, which breaks any test that opens a transaction (the tx
	// connection wouldn't see the schema).
	testDB, err = sql.Open("sqlite", "file:tenancytest?mode=memory&cache=shared")
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

	// Views live in annotation files (input.views), not schema.sql — apply
	// them to the test database the same way the sqlite/postgres examples do.
	for _, viewFile := range []string{"product_stats", "workspace_summaries"} {
		viewSQL, err := os.ReadFile("../views/" + viewFile + ".sql")
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		if _, err := testDB.Exec(string(viewSQL)); err != nil {
			panic(fmt.Sprintf("applying view %s: %v", viewFile, err))
		}
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}

// staticResolver returns a TenantResolver that always returns the given tenant.
// Tests that need to switch tenants per-call use ctxResolver instead.
func staticResolver(t uuid.UUID) tenancy.TenantResolver[uuid.UUID] {
	return func(context.Context) (uuid.UUID, error) { return t, nil }
}

// missingResolver always returns ErrMissing — exercises the §29.3.1 fail-closed
// path for tenancy.required:true.
func missingResolver() tenancy.TenantResolver[uuid.UUID] {
	return func(context.Context) (uuid.UUID, error) {
		return uuid.Nil(), tenancy.ErrMissing
	}
}

// tenantCtxKey is the context key used by ctxResolver. Defined as a private
// struct type per the stdlib convention against using string keys.
type tenantCtxKey struct{}

// withTenantCtx attaches a tenant to ctx for the ctxResolver to read.
func withTenantCtx(ctx context.Context, t uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, t)
}

// ctxResolver reads the tenant from ctx via withTenantCtx. Returns
// tenancy.ErrMissing when the value is not set so the fail-closed branch is
// exercised under realistic conditions (a real app's resolver would do the
// same).
func ctxResolver() tenancy.TenantResolver[uuid.UUID] {
	return func(ctx context.Context) (uuid.UUID, error) {
		v, ok := ctx.Value(tenantCtxKey{}).(uuid.UUID)
		if !ok {
			return uuid.Nil(), tenancy.ErrMissing
		}
		return v, nil
	}
}

// resetDB clears all data between tests so each test starts with an empty
// database. Schema is preserved.
func resetDB(t *testing.T) {
	t.Helper()
	// Child-before-parent order: articles, posts, and user_profiles all carry an
	// FK to users, so users has to be deleted after them.
	tables := []string{
		"post_tags", "tag_links", "tags", "posts", "user_profiles", "articles", "users",
		"products", "order_items",
		"audit_logs", "legacy_widgets", "workspaces",
	}
	for _, table := range tables {
		if _, err := testDB.Exec("DELETE FROM " + table); err != nil {
			t.Fatalf("reset %s: %v", table, err)
		}
	}
}

// countingQuerier wraps a database.Querier and counts Query/QueryRow/Exec
// invocations so tests can assert "no DB query was issued" (the §29.3.1
// fail-closed contract: missing-tenant returns ErrMissing BEFORE the round trip).
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

func (c *countingQuerier) totalDBOps() int64 {
	return c.queries.Load() + c.queryRow.Load() + c.execs.Load()
}

func (c *countingQuerier) reset() {
	c.queries.Store(0)
	c.queryRow.Store(0)
	c.execs.Store(0)
}

// spyMetrics records cache.MetricsRecorder calls. Cache isolation tests use it
// to verify that tenant B's first Get on the same PK as tenant A is a MISS;
// the nil-tenant invalidation tests use the recorded error ops to prove the
// defensive fallback fires only when forced (PRD §29.5).
type spyMetrics struct {
	mu       sync.Mutex
	hits     []string
	misses   []string
	sets     []string
	errorOps []string
}

func newSpyMetrics() *spyMetrics { return &spyMetrics{} }

func (m *spyMetrics) Hit(_ string, table hook.TableName) {
	m.mu.Lock()
	m.hits = append(m.hits, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Miss(_ string, table hook.TableName) {
	m.mu.Lock()
	m.misses = append(m.misses, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Set(_ string, table hook.TableName) {
	m.mu.Lock()
	m.sets = append(m.sets, string(table))
	m.mu.Unlock()
}

func (m *spyMetrics) Invalidate(_ string, _ hook.TableName) {}

func (m *spyMetrics) Error(_ string, _ hook.TableName, op string, _ error) {
	m.mu.Lock()
	m.errorOps = append(m.errorOps, op)
	m.mu.Unlock()
}
func (m *spyMetrics) HydrationStart(_ string, _ hook.TableName)              {}
func (m *spyMetrics) HydrationComplete(_ string, _ hook.TableName, _ error)  {}
func (m *spyMetrics) CircuitBreakerStateChange(_, _ cache.CircuitState)      {}
func (m *spyMetrics) GetLatency(_ string, _ hook.TableName, _ time.Duration) {}
func (m *spyMetrics) SetLatency(_ string, _ hook.TableName, _ time.Duration) {}
func (m *spyMetrics) InvalidateLatency(_ string, _ hook.TableName, _ time.Duration) {
}

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

func (m *spyMetrics) setCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sets)
}

// errorOpCount returns how many Error signals were recorded for op.
func (m *spyMetrics) errorOpCount(op string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, got := range m.errorOps {
		if got == op {
			n++
		}
	}
	return n
}

func (m *spyMetrics) errorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.errorOps)
}

// testEnv bundles materials a test needs. Each test calls newEnv to get a
// fresh client + counting querier so concurrent tests don't share atomics.
type testEnv struct {
	client   *models.Client
	counter  *countingQuerier
	bus      *memorybus.Bus
	cache    *models.Cache
	backend  cache.Backend
	metrics  *spyMetrics
	resolver tenancy.TenantResolver[uuid.UUID]
}

type envOpt func(*envCfg)

type envCfg struct {
	resolver     tenancy.TenantResolver[uuid.UUID]
	withCache    bool
	withEvents   bool
	metadataFunc func(context.Context) map[string]string
}

func withResolver(r tenancy.TenantResolver[uuid.UUID]) envOpt {
	return func(c *envCfg) { c.resolver = r }
}

func withCache() envOpt {
	return func(c *envCfg) { c.withCache = true }
}

func withEvents() envOpt {
	return func(c *envCfg) { c.withEvents = true }
}

// withMetadataFunc configures the event publisher's MetadataFunc (PRD §28.8).
// Implies withEvents. Used to exercise consumer-injected metadata on
// tenanted tables (reserved-key precedence, per-row tenant + shared keys).
func withMetadataFunc(fn func(context.Context) map[string]string) envOpt {
	return func(c *envCfg) {
		c.withEvents = true
		c.metadataFunc = fn
	}
}

// newEnv constructs a fresh client wired with the requested options. The
// default resolver is staticResolver(tenantA); pass withResolver to override.
func newEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	cfg := envCfg{resolver: staticResolver(tenantA)}
	for _, o := range opts {
		o(&cfg)
	}

	counter := newCountingQuerier(dbstdlib.New(testDB))

	clientOpts := []models.ClientOption{
		models.WithTenantResolver(cfg.resolver),
	}

	env := &testEnv{counter: counter, resolver: cfg.resolver}

	if cfg.withCache {
		backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
		if err != nil {
			t.Fatalf("memory.New: %v", err)
		}
		t.Cleanup(func() { _ = backend.Close() })

		metrics := newSpyMetrics()
		c, err := models.NewCache(backend, models.WithMetricsRecorder(metrics))
		if err != nil {
			t.Fatalf("NewCache: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })

		env.cache = c
		env.backend = backend
		env.metrics = metrics
		clientOpts = append(clientOpts, models.WithCache(c))
	}

	if cfg.withEvents {
		bus := memorybus.New()
		t.Cleanup(func() { _ = bus.Close() })
		env.bus = bus
		var evOpts []func(*event.Config)
		if cfg.metadataFunc != nil {
			evOpts = append(evOpts, func(c *event.Config) { c.MetadataFunc = cfg.metadataFunc })
		}
		clientOpts = append(clientOpts, models.WithEventPublisher(bus, evOpts...))
	}

	env.client = models.New(counter, clientOpts...)
	return env
}

// eventCollector accumulates events from a memorybus subscription. Safe for
// concurrent dispatch (the default callback mode is async).
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
		got := len(c.events)
		c.mu.Unlock()
		if got >= n {
			return c.snapshot()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor(%d): only %d events delivered", n, len(c.snapshot()))
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
