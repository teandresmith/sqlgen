// Package tests is the pgx integration leg for tenant capture (PRD §29.5): it
// compiles and runs the RETURNING-based tenant capture on the pgx driver —
// above all the UpdateMany SendBatch path (per-item `RETURNING <tenant>`
// scanned through br.Query()), which no other example compiles. The
// exhaustive nil-tenant behavior suite lives in the sqlite `tenancy`
// example.
package tests

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	sqlgenpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

var testPool *pgxpool.Pool

var (
	tenantA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tenantB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	container, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("sqlgen_tenancy"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic(fmt.Sprintf("starting postgres container: %v", err))
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}
	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		panic(fmt.Sprintf("opening pool: %v", err))
	}

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testPool.Exec(ctx, string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	// Views live in annotation files (input.views), not schema.sql.
	for _, viewFile := range []string{"article_stats", "workspace_line_totals"} {
		viewSQL, err := os.ReadFile("../views/" + viewFile + ".sql")
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		if _, err := testPool.Exec(ctx, string(viewSQL)); err != nil {
			panic(fmt.Sprintf("applying view %s: %v", viewFile, err))
		}
	}
	// REFRESH MATERIALIZED VIEW CONCURRENTLY requires a UNIQUE index covering
	// all rows; the @pk annotation asserts it generator-side, the server needs
	// the real index.
	if _, err := testPool.Exec(ctx, "CREATE UNIQUE INDEX workspace_line_totals_workspace_id_idx ON workspace_line_totals (workspace_id)"); err != nil {
		panic(fmt.Sprintf("creating workspace_line_totals unique index: %v", err))
	}

	code := m.Run()

	testPool.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func resetDB(t *testing.T) {
	t.Helper()
	for _, table := range []string{"articles", "line_items"} {
		if _, err := testPool.Exec(context.Background(), "DELETE FROM "+table); err != nil {
			t.Fatalf("reset %s: %v", table, err)
		}
	}
}

func staticResolver(tenant uuid.UUID) func(ctx context.Context) (uuid.UUID, error) {
	return func(ctx context.Context) (uuid.UUID, error) { return tenant, nil }
}

// spyMetrics mirrors the MySQL leg's recorder: hits/misses for precision
// proofs, error ops for the fallback-never-fires pin.
type spyMetrics struct {
	mu       sync.Mutex
	hits     int
	misses   int
	errorOps []string
}

func (m *spyMetrics) Hit(string, hook.TableName)        { m.mu.Lock(); m.hits++; m.mu.Unlock() }
func (m *spyMetrics) Miss(string, hook.TableName)       { m.mu.Lock(); m.misses++; m.mu.Unlock() }
func (m *spyMetrics) Set(string, hook.TableName)        {}
func (m *spyMetrics) Invalidate(string, hook.TableName) {}

func (m *spyMetrics) Error(_ string, _ hook.TableName, op string, _ error) {
	m.mu.Lock()
	m.errorOps = append(m.errorOps, op)
	m.mu.Unlock()
}
func (m *spyMetrics) HydrationStart(string, hook.TableName)                   {}
func (m *spyMetrics) HydrationComplete(string, hook.TableName, error)         {}
func (m *spyMetrics) CircuitBreakerStateChange(_, _ cache.CircuitState)       {}
func (m *spyMetrics) GetLatency(string, hook.TableName, time.Duration)        {}
func (m *spyMetrics) SetLatency(string, hook.TableName, time.Duration)        {}
func (m *spyMetrics) InvalidateLatency(string, hook.TableName, time.Duration) {}

func (m *spyMetrics) hitCount() int  { m.mu.Lock(); defer m.mu.Unlock(); return m.hits }
func (m *spyMetrics) missCount() int { m.mu.Lock(); defer m.mu.Unlock(); return m.misses }

func (m *spyMetrics) fallbackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, op := range m.errorOps {
		if op == "invalidate_tenant_fallback" {
			n++
		}
	}
	return n
}

type tenantEnv struct {
	clientA *models.Client
	clientB *models.Client
	metrics *spyMetrics
}

func newTenantEnv(t *testing.T) *tenantEnv {
	t.Helper()
	backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	metrics := &spyMetrics{}
	c, err := models.NewCache(backend, models.WithMetricsRecorder(metrics))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	mk := func(tenant uuid.UUID) *models.Client {
		return models.New(
			sqlgenpgx.New(testPool),
			models.WithTenantResolver(staticResolver(tenant)),
			models.WithCache(c),
		)
	}
	return &tenantEnv{clientA: mk(tenantA), clientB: mk(tenantB), metrics: metrics}
}

func assertNoFallback(t *testing.T, m *spyMetrics) {
	t.Helper()
	if n := m.fallbackCount(); n != 0 {
		t.Errorf("defensive fallback fired %d times, want 0 (pgx RETURNING capture must stay precise)", n)
	}
}

func assertHit(t *testing.T, m *spyMetrics, what string, fn func()) {
	t.Helper()
	hits, misses := m.hitCount(), m.missCount()
	fn()
	if got := m.hitCount() - hits; got != 1 {
		t.Errorf("%s: hits delta = %d, want 1 (entry should have survived)", what, got)
	}
	if got := m.missCount() - misses; got != 0 {
		t.Errorf("%s: misses delta = %d, want 0", what, got)
	}
}
