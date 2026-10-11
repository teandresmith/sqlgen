// Package livedb is test support for live-database tests: PostgreSQL and
// MySQL containers, per-test databases, the example setup applier, and the
// documented differences between code generated from files and from a
// database. Only _test.go files import it, so the sqlgen binary never links it.
package livedb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	// The introspectors register the pgx, mysql and sqlite database/sql
	// drivers, so tests open databases with the drivers sqlgen uses.
	_ "github.com/teandresmith/sqlgen/parser/introspect"
)

// The containers start on first use rather than in TestMain, so the rest of
// a test binary does not wait on them. Ryuk removes them when the binary exits.
var (
	pgOnce    sync.Once
	pgAddr    string
	pgErr     error
	mysqlOnce sync.Once
	mysqlAddr string
	mysqlErr  error

	// dbSeq names each test's database.
	dbSeq atomic.Int64
)

const password = "test"

func requireLiveDatabase(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live-database test in short mode")
	}
}

// PostgresAddr returns the host:port of the shared PostgreSQL 16 container,
// starting it on first use. It skips the test in short mode.
func PostgresAddr(t *testing.T) string {
	t.Helper()
	requireLiveDatabase(t)
	pgOnce.Do(func() {
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("sqlgen"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword(password),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			),
		)
		if err != nil {
			pgErr = err
			return
		}
		pgAddr, pgErr = c.PortEndpoint(ctx, "5432/tcp", "")
	})
	if pgErr != nil {
		t.Fatalf("starting postgres: %v", pgErr)
	}
	return pgAddr
}

// MySQLServerAddr returns the host:port of the shared MySQL 8.0 container,
// starting it on first use. It skips the test in short mode.
func MySQLServerAddr(t *testing.T) string {
	t.Helper()
	requireLiveDatabase(t)
	mysqlOnce.Do(func() {
		ctx := context.Background()
		c, err := tcmysql.Run(ctx, "mysql:8.0",
			tcmysql.WithDatabase("sqlgen"),
			tcmysql.WithUsername("root"),
			tcmysql.WithPassword(password),
		)
		if err != nil {
			mysqlErr = err
			return
		}
		mysqlAddr, mysqlErr = c.PortEndpoint(ctx, "3306/tcp", "")
	})
	if mysqlErr != nil {
		t.Fatalf("starting mysql: %v", mysqlErr)
	}
	return mysqlAddr
}

// PostgresURL returns the URL of database on the server at addr.
func PostgresURL(addr, database string) string {
	return "postgres://test:" + password + "@" + addr + "/" + database + "?sslmode=disable"
}

// MySQLDSN returns the DSN of database on the server at addr.
func MySQLDSN(addr, database string) string {
	return "root:" + password + "@tcp(" + addr + ")/" + database
}

// ExecSQL runs statements, which may be several, on the database at conn.
func ExecSQL(t *testing.T, driver, conn, statements string) {
	t.Helper()
	db, err := sql.Open(driver, conn)
	if err != nil {
		t.Fatalf("opening %s: %v", driver, err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(t.Context(), statements); err != nil {
		t.Fatalf("applying SQL: %v\n%s", err, statements)
	}
}

// NewPostgresDatabase creates an empty database and returns its URL.
func NewPostgresDatabase(t *testing.T) string {
	t.Helper()
	addr := PostgresAddr(t)
	name := fmt.Sprintf("t%d", dbSeq.Add(1))
	ExecSQL(t, "pgx", PostgresURL(addr, "sqlgen"), "CREATE DATABASE "+name)
	t.Cleanup(func() {
		db, err := sql.Open("pgx", PostgresURL(addr, "sqlgen"))
		if err != nil {
			return
		}
		defer db.Close() //nolint:errcheck // test cleanup
		_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return PostgresURL(addr, name)
}

// NewMySQLDatabase creates an empty database and returns its DSN.
func NewMySQLDatabase(t *testing.T) string {
	t.Helper()
	addr := MySQLServerAddr(t)
	name := fmt.Sprintf("t%d", dbSeq.Add(1))
	ExecSQL(t, "mysql", MySQLDSN(addr, ""), "CREATE DATABASE "+name)
	t.Cleanup(func() {
		db, err := sql.Open("mysql", MySQLDSN(addr, ""))
		if err != nil {
			return
		}
		defer db.Close() //nolint:errcheck // test cleanup
		_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name)
	})
	return MySQLDSN(addr, name)
}

// NewSQLiteDatabase creates an empty database file and returns its path.
// It skips the test in short mode, as the container-backed creators do.
func NewSQLiteDatabase(t *testing.T) string {
	t.Helper()
	requireLiveDatabase(t)
	path := filepath.Join(t.TempDir(), "app.db")
	ExecSQL(t, "sqlite", path, "PRAGMA user_version = 1")
	return path
}

// ApplySetup builds an example's database at conn from the example directory
// dir: schema.sql, then every views/*.sql in lexical order, then
// after_views.sql when present. Each file runs as one Exec.
func ApplySetup(t *testing.T, dialect, conn, dir string) {
	t.Helper()
	views, err := filepath.Glob(filepath.Join(dir, "views", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(views)
	files := append([]string{filepath.Join(dir, "schema.sql")}, views...)
	afterViews := filepath.Join(dir, "after_views.sql")
	if _, err := os.Stat(afterViews); err == nil {
		files = append(files, afterViews)
	}

	driver, dsn := "", conn
	switch dialect {
	case "postgres":
		driver = "pgx"
	case "mysql":
		// The MySQL driver sends one statement per Exec unless asked.
		driver = "mysql"
		sep := "?"
		if strings.Contains(conn, "?") {
			sep = "&"
		}
		dsn = conn + sep + "multiStatements=true"
	case "sqlite":
		driver = "sqlite"
	default:
		t.Fatalf("apply setup: unknown dialect %q", dialect)
	}
	for _, path := range files {
		data, err := os.ReadFile(path) //nolint:gosec // test reads an example's own setup files
		if err != nil {
			t.Fatalf("apply setup: %v", err)
		}
		ExecSQL(t, driver, dsn, string(data))
	}
}
