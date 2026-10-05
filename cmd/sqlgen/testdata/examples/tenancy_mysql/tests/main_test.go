// Package tests is the MySQL integration leg for tenant capture (PRD §29.5):
// it runs the tenant-capture mechanics that only exist on a dialect without
// RETURNING — the captureAffectedTenants pre-reads, the batched *Many
// pre-read, the widened collectAffected* projections, and the
// upsert post-statement read — against a real MySQL server, across single
// numeric, composite (tenant outside the PK), and string-typed PK shapes.
// The exhaustive nil-tenant behavior suite lives in the sqlite `tenancy`
// example; this module pins the MySQL-only paths and the per-row event
// stamps. The tenant type here is a plain
// string (CHAR(36) column, no type override) — deliberately different from
// the uuid.UUID of the other tenancy examples.
package tests

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

var testDB *sql.DB

// Stable tenant IDs (canonical uuid strings — the tenant type in this
// package is string, resolved from the CHAR(36) column).
const (
	tenantA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	tenantB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	container, err := mysql.Run(
		ctx,
		"mysql:8.0",
		mysql.WithDatabase("sqlgen_tenancy"),
		mysql.WithUsername("test"),
		mysql.WithPassword("test"),
	)
	if err != nil {
		panic(fmt.Sprintf("starting mysql container: %v", err))
	}

	connStr, err := container.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}
	testDB, err = sql.Open("mysql", connStr)
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	for _, stmt := range splitStatements(string(schema)) {
		if stmt == "" {
			continue
		}
		if _, err := testDB.ExecContext(ctx, stmt); err != nil {
			panic(fmt.Sprintf("applying schema statement: %v\nSQL: %s", err, stmt))
		}
	}

	// Views live in annotation files (input.views), not schema.sql.
	viewSQL, err := os.ReadFile("../views/article_stats.sql")
	if err != nil {
		panic(fmt.Sprintf("reading view article_stats: %v", err))
	}
	for _, stmt := range splitStatements(string(viewSQL)) {
		if stmt == "" {
			continue
		}
		if _, err := testDB.ExecContext(ctx, stmt); err != nil {
			panic(fmt.Sprintf("applying view statement: %v\nSQL: %s", err, stmt))
		}
	}

	code := m.Run()

	testDB.Close()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

// splitStatements splits a schema script on statement-terminating semicolons,
// honoring quoted strings (MySQL executes one statement per Exec).
func splitStatements(script string) []string {
	var stmts []string
	var current []byte
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(script); i++ {
		c := script[i]
		if inQuote {
			current = append(current, c)
			if c == quoteChar {
				inQuote = false
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inQuote = true
			quoteChar = c
			current = append(current, c)
		case ';':
			stmt := trimSpaceAndComments(string(current))
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			current = current[:0]
		default:
			current = append(current, c)
		}
	}
	if stmt := trimSpaceAndComments(string(current)); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}

// trimSpaceAndComments strips leading whitespace and `-- …` comment lines so
// pure-comment fragments do not reach the server as empty statements.
func trimSpaceAndComments(s string) string {
	lines := make([]string, 0, 8)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			trimmed := trimLeftSpace(line)
			if trimmed != "" && !hasPrefix(trimmed, "--") {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return trimLeftSpace(out)
}

func trimLeftSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	return s
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// resetDB clears every table between tests.
func resetDB(t *testing.T) {
	t.Helper()
	for _, table := range []string{"articles", "line_items", "documents"} {
		if _, err := testDB.Exec("DELETE FROM " + table); err != nil {
			t.Fatalf("reset %s: %v", table, err)
		}
	}
}

// staticResolver returns a resolver that always yields the given tenant.
func staticResolver(tenant string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) { return tenant, nil }
}

// spyMetrics records cache hits/misses and error signals (used to prove
// precise eviction and the absence of the defensive fallback).
type spyMetrics struct {
	mu       sync.Mutex
	hits     int
	misses   int
	errorOps []string
}

func (m *spyMetrics) Hit(string, hook.TableName)  { m.mu.Lock(); m.hits++; m.mu.Unlock() }
func (m *spyMetrics) Miss(string, hook.TableName) { m.mu.Lock(); m.misses++; m.mu.Unlock() }
func (m *spyMetrics) Set(string, hook.TableName)  {}
func (m *spyMetrics) Invalidate(string, hook.TableName) {
}

func (m *spyMetrics) Error(_ string, _ hook.TableName, op string, _ error) {
	m.mu.Lock()
	m.errorOps = append(m.errorOps, op)
	m.mu.Unlock()
}
func (m *spyMetrics) HydrationStart(string, hook.TableName)           {}
func (m *spyMetrics) HydrationComplete(string, hook.TableName, error) {}
func (m *spyMetrics) CircuitBreakerStateChange(_, _ cache.CircuitState) {
}
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

// tenantEnv bundles two tenant-scoped clients over ONE shared cache.
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

	mk := func(tenant string) *models.Client {
		return models.New(
			dbstdlib.New(testDB),
			models.WithTenantResolver(staticResolver(tenant)),
			models.WithCache(c),
		)
	}
	return &tenantEnv{clientA: mk(tenantA), clientB: mk(tenantB), metrics: metrics}
}

// assertNoFallback pins the no-fallback converse on the MySQL leg: precise capture
// means the cross-tenant table pattern never fires in normal operation.
func assertNoFallback(t *testing.T, m *spyMetrics) {
	t.Helper()
	if n := m.fallbackCount(); n != 0 {
		t.Errorf("defensive fallback fired %d times, want 0 (MySQL pre-read capture must stay precise)", n)
	}
}

// assertHit runs fn and asserts exactly one cache hit and no miss.
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
