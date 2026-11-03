package stdlib_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"
	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"
	"github.com/testcontainers/testcontainers-go"
	mysqltc "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/database"
	stdlibadapter "github.com/teandresmith/sqlgen/database/stdlib"
)

var (
	mysqlDB  *sql.DB
	sqliteDB *sql.DB
	// postgresDB is PostgreSQL through lib/pq. It is not one of backends(),
	// whose tests use ? placeholders; tests that cover the stdlib adapter on
	// PostgreSQL use it directly.
	postgresDB *sql.DB
	// mysqlDSN and postgresDSN are kept so a test can open a pool with its
	// own settings.
	mysqlDSN    string
	postgresDSN string
)

type backend struct {
	name string
	db   *sql.DB
}

func backends() []backend {
	return []backend{
		{name: "mysql", db: mysqlDB},
		{name: "sqlite", db: sqliteDB},
	}
}

func cleanTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), "DELETE FROM test_users"); err != nil {
		t.Fatalf("cleaning test_users: %v", err)
	}
}

// requireDocker skips an integration test under -short, where TestMain starts
// no containers and only the unit tests run.
func requireDocker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	// MySQL via testcontainers
	mysqlContainer, err := mysqltc.Run(
		ctx,
		"mysql:8.0",
		mysqltc.WithDatabase("sqlgen_test"),
		mysqltc.WithUsername("test"),
		mysqltc.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("port: 3306  MySQL Community Server").WithOccurrence(1),
		),
	)
	if err != nil {
		panic(err)
	}

	mysqlConnStr, err := mysqlContainer.ConnectionString(ctx, "multiStatements=true")
	if err != nil {
		panic(err)
	}

	mysqlDSN = mysqlConnStr
	mysqlDB, err = sql.Open("mysql", mysqlConnStr)
	if err != nil {
		panic(err)
	}
	if err := mysqlDB.PingContext(ctx); err != nil {
		panic(err)
	}

	mysqlSchema, err := os.ReadFile("testdata/mysql_schema.sql")
	if err != nil {
		panic(err)
	}
	if _, err := mysqlDB.ExecContext(ctx, string(mysqlSchema)); err != nil {
		panic(err)
	}

	// SQLite in-memory (single connection to share state)
	sqliteDB, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	sqliteDB.SetMaxOpenConns(1)

	// Enable foreign key enforcement for SQLite
	if _, err := sqliteDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		panic(err)
	}

	sqliteSchema, err := os.ReadFile("testdata/sqlite_schema.sql")
	if err != nil {
		panic(err)
	}
	if _, err := sqliteDB.ExecContext(ctx, string(sqliteSchema)); err != nil {
		panic(err)
	}

	// PostgreSQL via testcontainers, opened through lib/pq
	pgContainer, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("sqlgen_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		panic(err)
	}

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}

	postgresDSN = pgConnStr
	postgresDB, err = sql.Open("postgres", pgConnStr)
	if err != nil {
		panic(err)
	}

	postgresSchema, err := os.ReadFile("testdata/postgres_schema.sql")
	if err != nil {
		panic(err)
	}
	if _, err := postgresDB.ExecContext(ctx, string(postgresSchema)); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = mysqlDB.Close()
	_ = sqliteDB.Close()
	_ = postgresDB.Close()
	_ = mysqlContainer.Terminate(ctx)
	_ = pgContainer.Terminate(ctx)
	os.Exit(code)
}

func TestExec(t *testing.T) {
	requireDocker(t)
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTable(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			res, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "alice", "alice@example.com")
			if err != nil {
				t.Fatalf("Exec() unexpected error: %v", err)
			}

			affected, err := res.RowsAffected()
			if err != nil {
				t.Fatalf("RowsAffected() unexpected error: %v", err)
			}
			if affected != 1 {
				t.Errorf("RowsAffected() = %d, want 1", affected)
			}
		})
	}
}

func TestQuery(t *testing.T) {
	requireDocker(t)
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTable(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "bob", "bob@example.com")
			_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "carol", "carol@example.com")

			rows, err := q.Query(ctx, "SELECT name, email FROM test_users ORDER BY name")
			if err != nil {
				t.Fatalf("Query() unexpected error: %v", err)
			}
			defer func() { _ = rows.Close() }()

			cols, err := rows.Columns()
			if err != nil {
				t.Fatalf("Columns() unexpected error: %v", err)
			}
			if len(cols) != 2 || cols[0] != "name" || cols[1] != "email" {
				t.Errorf("Columns() = %v, want [name email]", cols)
			}

			type user struct {
				Name  string
				Email string
			}
			var users []user
			for rows.Next() {
				var u user
				if err := rows.Scan(&u.Name, &u.Email); err != nil {
					t.Fatalf("Scan() unexpected error: %v", err)
				}
				users = append(users, u)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("Err() unexpected error: %v", err)
			}

			if len(users) != 2 {
				t.Fatalf("Query() returned %d rows, want 2", len(users))
			}
			if users[0].Name != "bob" || users[0].Email != "bob@example.com" {
				t.Errorf("users[0] = %+v, want {Name:bob Email:bob@example.com}", users[0])
			}
			if users[1].Name != "carol" || users[1].Email != "carol@example.com" {
				t.Errorf("users[1] = %+v, want {Name:carol Email:carol@example.com}", users[1])
			}
		})
	}
}

func TestQueryRow(t *testing.T) {
	requireDocker(t)
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTable(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "dave", "dave@example.com")

			row := q.QueryRow(ctx, "SELECT name, email FROM test_users WHERE name = ?", "dave")
			var name, email string
			if err := row.Scan(&name, &email); err != nil {
				t.Fatalf("Scan() unexpected error: %v", err)
			}
			if name != "dave" {
				t.Errorf("name = %q, want %q", name, "dave")
			}
			if email != "dave@example.com" {
				t.Errorf("email = %q, want %q", email, "dave@example.com")
			}
		})
	}
}

func TestBeginCommitMySQL(t *testing.T) {
	requireDocker(t)
	cleanTable(t, mysqlDB)
	q := stdlibadapter.New(mysqlDB)
	ctx := context.Background()

	txCtx, err := database.NewTransaction(ctx, q, "test_commit")
	if err != nil {
		t.Fatalf("NewTransaction() unexpected error: %v", err)
	}

	tx := database.FromContext(txCtx)
	if tx == nil {
		t.Fatal("FromContext() returned nil")
	}

	_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "eve", "eve@example.com")
	if err != nil {
		t.Fatalf("Tx.Exec() unexpected error: %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() unexpected error: %v", err)
	}

	// Verify the row persisted after commit
	row := q.QueryRow(ctx, "SELECT name FROM test_users WHERE email = ?", "eve@example.com")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("Scan() after commit unexpected error: %v", err)
	}
	if name != "eve" {
		t.Errorf("name = %q, want %q", name, "eve")
	}
}

func TestBeginCommitSQLite(t *testing.T) {
	requireDocker(t)
	cleanTable(t, sqliteDB)
	q := stdlibadapter.New(sqliteDB)
	ctx := context.Background()

	txCtx, err := database.NewTransaction(ctx, q, "test_commit")
	if err != nil {
		t.Fatalf("NewTransaction() unexpected error: %v", err)
	}

	tx := database.FromContext(txCtx)
	if tx == nil {
		t.Fatal("FromContext() returned nil")
	}

	_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "eve", "eve@example.com")
	if err != nil {
		t.Fatalf("Tx.Exec() unexpected error: %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() unexpected error: %v", err)
	}

	// Verify the row persisted after commit
	row := q.QueryRow(ctx, "SELECT name FROM test_users WHERE email = ?", "eve@example.com")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("Scan() after commit unexpected error: %v", err)
	}
	if name != "eve" {
		t.Errorf("name = %q, want %q", name, "eve")
	}
}

func TestSavepointRollback(t *testing.T) {
	requireDocker(t)
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTable(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			// Begin outer transaction
			txCtx, err := database.NewTransaction(ctx, q, "outer")
			if err != nil {
				t.Fatalf("NewTransaction() unexpected error: %v", err)
			}

			tx := database.FromContext(txCtx)

			// Insert in outer transaction
			_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "outer_user", "outer@example.com")
			if err != nil {
				t.Fatalf("outer Exec() unexpected error: %v", err)
			}

			// Create savepoint (nested transaction)
			spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
			if err != nil {
				t.Fatalf("NewTransaction() savepoint unexpected error: %v", err)
			}

			spTx := database.FromContext(spCtx)

			// Insert in savepoint
			_, err = spTx.Exec(spCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "inner_user", "inner@example.com")
			if err != nil {
				t.Fatalf("inner Exec() unexpected error: %v", err)
			}

			// Rollback savepoint — inner insert should be undone
			if err := database.Rollback(spCtx); err != nil {
				t.Fatalf("Rollback() savepoint unexpected error: %v", err)
			}

			// Commit outer — only outer_user should survive
			if err := database.Commit(txCtx); err != nil {
				t.Fatalf("Commit() unexpected error: %v", err)
			}

			// Verify outer_user exists
			row := q.QueryRow(ctx, "SELECT count(*) FROM test_users WHERE email = ?", "outer@example.com")
			var count int
			if err := row.Scan(&count); err != nil {
				t.Fatalf("Scan() unexpected error: %v", err)
			}
			if count != 1 {
				t.Errorf("outer_user count = %d, want 1", count)
			}

			// Verify inner_user does NOT exist
			row = q.QueryRow(ctx, "SELECT count(*) FROM test_users WHERE email = ?", "inner@example.com")
			if err := row.Scan(&count); err != nil {
				t.Fatalf("Scan() unexpected error: %v", err)
			}
			if count != 0 {
				t.Errorf("inner_user count after savepoint rollback = %d, want 0", count)
			}
		})
	}
}

func TestResultLastInsertId(t *testing.T) {
	requireDocker(t)
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTable(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			res, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "frank", "frank@example.com")
			if err != nil {
				t.Fatalf("Exec() unexpected error: %v", err)
			}

			affected, err := res.RowsAffected()
			if err != nil {
				t.Fatalf("RowsAffected() unexpected error: %v", err)
			}
			if affected != 1 {
				t.Errorf("RowsAffected() = %d, want 1", affected)
			}

			id, err := res.LastInsertId()
			if err != nil {
				t.Fatalf("LastInsertId() unexpected error: %v", err)
			}
			if id <= 0 {
				t.Errorf("LastInsertId() = %d, want > 0", id)
			}
		})
	}
}

func TestQuerierMethodsImplemented(t *testing.T) {
	requireDocker(t)
	// Compile-time check is in stdlib.go: var _ database.Querier = (*stdlibAdapter)(nil)
	// This test verifies New() returns a non-nil value at runtime.
	q := stdlibadapter.New(sqliteDB)
	if q == nil {
		t.Fatal("New() returned nil")
	}
}

// --- Error mapping integration tests ---

func cleanTables(t *testing.T, db *sql.DB) {
	t.Helper()
	_, _ = db.ExecContext(context.Background(), "DELETE FROM test_orders")
	_, _ = db.ExecContext(context.Background(), "DELETE FROM test_users")
}

// mysqlErrNumber reports whether err is the server's *mysql.MySQLError with
// error number num.
func mysqlErrNumber(num uint16) func(error) bool {
	return func(err error) bool {
		me, ok := errors.AsType[*mysql.MySQLError](err)
		return ok && me.Number == num
	}
}

// sqliteErrCode reports whether err is a SQLite driver error with extended
// result code code.
func sqliteErrCode(code int) func(error) bool {
	return func(err error) bool {
		se, ok := errors.AsType[interface {
			error
			Code() int
		}](err)
		return ok && se.Code() == code
	}
}

// pqErrCode reports whether err is the server's *pq.Error with SQLSTATE code.
func pqErrCode(code pqerror.Code) func(error) bool {
	return func(err error) bool {
		pe, ok := errors.AsType[*pq.Error](err)
		return ok && pe.Code == code
	}
}

// assertConstraintError checks that err is exactly want, with Err holding the
// driver's own error (isDriverErr), and that it unwraps to
// ErrConstraintViolation. Rows use explicit ids so a Detail that quotes the
// failing row is fixed.
func assertConstraintError(t *testing.T, err error, want database.ConstraintError, isDriverErr func(error) bool) {
	t.Helper()
	if !errors.Is(err, database.ErrConstraintViolation) {
		t.Errorf("error = %v, want it to wrap ErrConstraintViolation", err)
	}
	ce, ok := errors.AsType[*database.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want a *database.ConstraintError", err, err)
	}
	if !isDriverErr(ce.Err) {
		t.Fatalf("ConstraintError.Err = %v (%T), want the driver's own error for this violation", ce.Err, ce.Err)
	}
	want.Err = ce.Err
	if diff := cmp.Diff(want, *ce, cmp.Comparer(func(a, b error) bool { return a == b })); diff != "" { //nolint:errorlint // identity: Err must be the very error the driver returned
		t.Errorf("ConstraintError mismatch (-want +got):\n%s", diff)
	}
}

// execOn cleans db's tables, runs each setup statement, and returns the error
// of stmt.
func execOn(t *testing.T, db *sql.DB, setup []string, stmt string, args ...any) error {
	t.Helper()
	cleanTables(t, db)
	q := stdlibadapter.New(db)
	ctx := context.Background()
	for _, s := range setup {
		if _, err := q.Exec(ctx, s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	_, err := q.Exec(ctx, stmt, args...)
	return err
}

func TestConstraintUniqueViolation(t *testing.T) {
	requireDocker(t)
	const seed = "INSERT INTO test_users (id, name, email) VALUES (9001, 'alice', 'alice@example.com')"
	t.Run("mysql", func(t *testing.T) {
		err := execOn(t, mysqlDB, []string{seed}, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9002, "bob", "alice@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: "email",
			Detail:     "Duplicate entry 'alice@example.com' for key 'test_users.email'",
		}, mysqlErrNumber(1062))
	})
	t.Run("sqlite", func(t *testing.T) {
		err := execOn(t, sqliteDB, []string{seed}, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9002, "bob", "alice@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: "test_users.email",
			Column:     "email",
			Detail:     "constraint failed: UNIQUE constraint failed: test_users.email (2067)",
		}, sqliteErrCode(2067))
	})
	t.Run("postgres", func(t *testing.T) {
		err := execOn(t, postgresDB, []string{seed}, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9002, "bob", "alice@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: "test_users_email_key",
			Detail:     "Key (email)=(alice@example.com) already exists.",
		}, pqErrCode("23505"))
	})
}

func TestConstraintFKViolation(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		err := execOn(t, mysqlDB, nil, "INSERT INTO test_orders (id, user_id, amount) VALUES (?, ?, ?)", 9001, 99999, 100)
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: "test_orders_user_fk",
			Detail:     "Cannot add or update a child row: a foreign key constraint fails (`sqlgen_test`.`test_orders`, CONSTRAINT `test_orders_user_fk` FOREIGN KEY (`user_id`) REFERENCES `test_users` (`id`))",
		}, mysqlErrNumber(1452))
	})
	t.Run("sqlite", func(t *testing.T) {
		err := execOn(t, sqliteDB, nil, "INSERT INTO test_orders (id, user_id, amount) VALUES (?, ?, ?)", 9001, 99999, 100)
		// SQLite's foreign key message names no constraint and no column.
		assertConstraintError(t, err, database.ConstraintError{
			Type:   database.ConstraintForeignKey,
			Detail: "constraint failed: FOREIGN KEY constraint failed (787)",
		}, sqliteErrCode(787))
	})
	t.Run("postgres", func(t *testing.T) {
		err := execOn(t, postgresDB, nil, "INSERT INTO test_orders (id, user_id, amount) VALUES ($1, $2, $3)", 9001, 99999, 100)
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: "test_orders_user_id_fkey",
			Detail:     `Key (user_id)=(99999) is not present in table "test_users".`,
		}, pqErrCode("23503"))
	})
}

func TestConstraintNotNullViolation(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		err := execOn(t, mysqlDB, nil, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9001, nil, "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:   database.ConstraintNotNull,
			Column: "name",
			Detail: "Column 'name' cannot be null",
		}, mysqlErrNumber(1048))
	})
	t.Run("sqlite", func(t *testing.T) {
		err := execOn(t, sqliteDB, nil, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9001, nil, "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintNotNull,
			Constraint: "test_users.name",
			Column:     "name",
			Detail:     "constraint failed: NOT NULL constraint failed: test_users.name (1299)",
		}, sqliteErrCode(1299))
	})
	t.Run("postgres", func(t *testing.T) {
		err := execOn(t, postgresDB, nil, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9001, nil, "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:   database.ConstraintNotNull,
			Column: "name",
			Detail: "Failing row contains (9001, null, test@example.com).",
		}, pqErrCode("23502"))
	})
}

func TestConstraintCheckViolation(t *testing.T) {
	requireDocker(t)
	// An empty name violates CHECK (length(name) > 0) on every dialect.
	t.Run("mysql", func(t *testing.T) {
		err := execOn(t, mysqlDB, nil, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9001, "", "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: "test_users_name_check",
			Detail:     "Check constraint 'test_users_name_check' is violated.",
		}, mysqlErrNumber(3819))
	})
	t.Run("sqlite", func(t *testing.T) {
		err := execOn(t, sqliteDB, nil, "INSERT INTO test_users (id, name, email) VALUES (?, ?, ?)", 9001, "", "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: "length(name) > 0",
			Detail:     "constraint failed: CHECK constraint failed: length(name) > 0 (275)",
		}, sqliteErrCode(275))
	})
	t.Run("postgres", func(t *testing.T) {
		err := execOn(t, postgresDB, nil, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9001, "", "test@example.com")
		assertConstraintError(t, err, database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: "test_users_name_check",
			Detail:     "Failing row contains (9001, , test@example.com).",
		}, pqErrCode("23514"))
	})
}

// TestCommitMapsDeferredConstraintPostgres maps an error that only COMMIT can
// report through lib/pq. A DEFERRABLE INITIALLY DEFERRED unique constraint is
// checked at commit, so the duplicate INSERT succeeds and COMMIT fails.
func TestCommitMapsDeferredConstraintPostgres(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	if _, err := postgresDB.ExecContext(ctx, "DELETE FROM test_deferred"); err != nil {
		t.Fatalf("cleaning test_deferred: %v", err)
	}

	txCtx, err := database.NewTransaction(ctx, stdlibadapter.New(postgresDB), "deferred_unique")
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	tx := database.FromContext(txCtx)
	if _, err := tx.Exec(txCtx, "INSERT INTO test_deferred (id, code) VALUES (1, 'a'), (2, 'a')"); err != nil {
		t.Fatalf("duplicate INSERT failed before COMMIT, so the check was not deferred: %v", err)
	}

	err = database.Commit(txCtx)
	_ = database.Rollback(txCtx) // the transaction is still the caller's after a failed COMMIT
	assertConstraintError(t, err, database.ConstraintError{
		Type:       database.ConstraintUnique,
		Constraint: "test_deferred_code_key",
		Detail:     "Key (code)=(a) already exists.",
	}, pqErrCode("23505"))

	var n int
	if err := postgresDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_deferred").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows after the failed COMMIT = %d, want 0", n)
	}
}

func TestDeadlockMySQL(t *testing.T) {
	requireDocker(t)
	cleanTables(t, mysqlDB)
	ctx := context.Background()

	// Setup: create two rows
	_, _ = mysqlDB.ExecContext(ctx, "INSERT INTO test_users (id, name, email) VALUES (1, 'alice', 'alice@example.com')")
	_, _ = mysqlDB.ExecContext(ctx, "INSERT INTO test_users (id, name, email) VALUES (2, 'bob', 'bob@example.com')")

	errCh := make(chan error, 2)
	ready1 := make(chan struct{})
	ready2 := make(chan struct{})

	q := stdlibadapter.New(mysqlDB)

	// Transaction 1: lock row 1, then try row 2
	go func() {
		txCtx, err := database.NewTransaction(ctx, q, "tx1")
		if err != nil {
			errCh <- err
			return
		}
		tx := database.FromContext(txCtx)

		_, err = tx.Exec(txCtx, "UPDATE test_users SET name = ? WHERE id = ?", "a1", 1)
		if err != nil {
			_ = database.Rollback(txCtx)
			errCh <- err
			return
		}
		close(ready1)
		<-ready2

		_, err = tx.Exec(txCtx, "UPDATE test_users SET name = ? WHERE id = ?", "a2", 2)
		_ = database.Rollback(txCtx)
		errCh <- err
	}()

	// Transaction 2: lock row 2, then try row 1
	go func() {
		txCtx, err := database.NewTransaction(ctx, q, "tx2")
		if err != nil {
			errCh <- err
			return
		}
		tx := database.FromContext(txCtx)

		_, err = tx.Exec(txCtx, "UPDATE test_users SET name = ? WHERE id = ?", "b2", 2)
		if err != nil {
			_ = database.Rollback(txCtx)
			errCh <- err
			return
		}
		close(ready2)
		<-ready1

		_, err = tx.Exec(txCtx, "UPDATE test_users SET name = ? WHERE id = ?", "b1", 1)
		_ = database.Rollback(txCtx)
		errCh <- err
	}()

	var deadlockErr error
	for range 2 {
		if err := <-errCh; errors.Is(err, database.ErrDeadlock) {
			deadlockErr = err
		}
	}
	if deadlockErr == nil {
		t.Fatal("expected at least one ErrDeadlock from concurrent MySQL transactions")
	}
	if !mysqlErrNumber(1213)(deadlockErr) {
		t.Errorf("deadlock error = %v, want it to wrap the server's *mysql.MySQLError 1213", deadlockErr)
	}
}

// TestRowsNextReleasesConnectionOnDrainMySQL is the §2.11 probe behind the
// release-on-drain reservation PRD §18.5 specifies: the connection must be free
// the moment iteration reports exhaustion, not merely when Close runs.
//
// database/sql does not give that for free. Rows.nextLocked keeps the
// connection when the driver implements driver.RowsNextResultSet and another
// result set is pending; go-sql-driver/mysql does, and this harness connects
// with multiStatements=true (see TestMain), so a multi-statement query reaches
// the exception. Without the adapter closing on exhaustion, the multi case
// fails with "busy buffer" and destroys the connection, taking the rollback
// with it. MySQL only — neither SQLite nor pgx has a next-result-set path here.
func TestRowsNextReleasesConnectionOnDrainMySQL(t *testing.T) {
	requireDocker(t)
	tests := []struct {
		name  string
		query string
		want  []int
	}{
		{name: "single result set", query: "SELECT 1", want: []int{1}},
		{name: "multiple result sets", query: "SELECT 1; SELECT 2", want: []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTables(t, mysqlDB)
			ctx := context.Background()
			q := stdlibadapter.New(mysqlDB)

			txCtx, err := database.NewTransaction(ctx, q, "mrs_probe")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			rows, err := tx.Query(txCtx, tt.query)
			if err != nil {
				t.Fatalf("Query(%q) error = %v", tt.query, err)
			}
			// Deliberately not closed before the Exec below: draining is what
			// has to free the connection.
			defer func() {
				_ = rows.Close() // idempotent; the drain already closed it
			}()

			var got []int
			for rows.Next() {
				var n int
				if err := rows.Scan(&n); err != nil {
					t.Fatalf("Scan() error = %v", err)
				}
				got = append(got, n)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows.Err() after draining %q = %v", tt.query, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("drained %q = %v, want %v", tt.query, got, tt.want)
			}

			_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "mrs", "mrs@example.com")
			if err != nil {
				t.Errorf("Exec() after draining %q = %v, want nil: the drained result set still pins the connection", tt.query, err)
			}

			if err := database.Rollback(txCtx); err != nil {
				t.Errorf("Rollback() after draining %q = %v, want nil", tt.query, err)
			}
		})
	}
}

// --- SessionPinner (PRD §19.1) ---

func sessionPinner(t *testing.T, db *sql.DB) database.SessionPinner {
	t.Helper()
	q := stdlibadapter.New(db)
	pinner, ok := q.(database.SessionPinner)
	if !ok {
		t.Fatalf("stdlib.New returned %T, which does not implement database.SessionPinner", q)
	}
	return pinner
}

// TestWithSessionRunsOnOneSessionMySQL checks the property MySQL's CreateMany
// key derivation relies on: every statement fn issues reaches the session that
// ran the first, so per-session state such as LAST_INSERT_ID and
// auto_increment_increment describes that statement. And each statement still
// commits on its own — nothing is begun around them.
//
// The pool keeps no idle connections, so a statement that is not pinned opens
// a new session every time rather than happening to reuse the one just freed.
// A pool INSERT runs between the session's INSERT and its LAST_INSERT_ID read:
// on the pinned session that read still names the session's own INSERT.
func TestWithSessionRunsOnOneSessionMySQL(t *testing.T) {
	requireDocker(t)
	cleanTable(t, mysqlDB)
	ctx := context.Background()
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatalf("opening a pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxIdleConns(0)
	q := stdlibadapter.New(db)

	err = sessionPinner(t, db).WithSession(ctx, func(session database.Querier) error {
		var first int64
		if err := session.QueryRow(ctx, "SELECT CONNECTION_ID()").Scan(&first); err != nil {
			return err
		}
		res, err := session.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?), (?, ?)",
			"pin1", "pin1@example.com", "pin2", "pin2@example.com")
		if err != nil {
			return err
		}
		inserted, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "pool", "pool@example.com"); err != nil {
			return err
		}

		var sessionLast, again int64
		if err := session.QueryRow(ctx, "SELECT LAST_INSERT_ID(), CONNECTION_ID()").Scan(&sessionLast, &again); err != nil {
			return err
		}
		if again != first {
			t.Errorf("statements in one WithSession ran on connections %d and %d", first, again)
		}
		if sessionLast != inserted {
			t.Errorf("LAST_INSERT_ID() on the session = %d, want the session's INSERT's %d", sessionLast, inserted)
		}

		// Read through the pool, on another session, before fn returns.
		var visible int
		if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM test_users WHERE email IN (?, ?)", "pin1@example.com", "pin2@example.com").Scan(&visible); err != nil {
			return err
		}
		if visible != 2 {
			t.Errorf("another session sees %d of the session's 2 rows before WithSession returns, want 2 — the INSERT did not commit on its own", visible)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithSession: %v", err)
	}
}

// TestWithSessionReleasesTheConnection returns the pinned connection to the
// pool however fn ends. On SQLite, whose pool holds one connection, a leaked
// pin would hang every later test.
func TestWithSessionReleasesTheConnection(t *testing.T) {
	requireDocker(t)
	errFn := errors.New("fn failed")
	tests := []struct {
		name    string
		fn      func(database.Querier) error
		wantErr error
		panics  bool
	}{
		{name: "fn succeeds", fn: func(database.Querier) error { return nil }},
		{name: "fn fails", fn: func(database.Querier) error { return errFn }, wantErr: errFn},
		{name: "fn panics", fn: func(database.Querier) error { panic("fn panicked") }, panics: true},
	}
	for _, b := range backends() {
		for _, tt := range tests {
			t.Run(b.name+"/"+tt.name, func(t *testing.T) {
				pinner := sessionPinner(t, b.db)
				err := func() (err error) {
					defer func() {
						if r := recover(); r != nil && !tt.panics {
							panic(r)
						}
					}()
					return pinner.WithSession(context.Background(), tt.fn)
				}()
				if !tt.panics && !errors.Is(err, tt.wantErr) {
					t.Errorf("WithSession() error = %v, want %v", err, tt.wantErr)
				}
				if inUse := b.db.Stats().InUse; inUse != 0 {
					t.Errorf("%d connections still in use after WithSession returned", inUse)
				}
			})
		}
	}
}

// TestWithSessionMapsErrors maps a statement's error on the pinned session
// exactly as the pool does: the same violation yields the same
// *database.ConstraintError, field for field, carrying the driver's own error.
func TestWithSessionMapsErrors(t *testing.T) {
	requireDocker(t)
	const insert = "INSERT INTO test_users (name, email) VALUES (?, ?)"
	// sameDriverError compares two driver errors by the code the driver
	// reports, since the two calls return distinct error values.
	sameDriverError := cmp.Comparer(func(a, b error) bool {
		if ma, ok := errors.AsType[*mysql.MySQLError](a); ok {
			mb, ok := errors.AsType[*mysql.MySQLError](b)
			return ok && ma.Number == mb.Number && ma.Message == mb.Message
		}
		type coder interface {
			error
			Code() int
		}
		if ca, ok := errors.AsType[coder](a); ok {
			cb, ok := errors.AsType[coder](b)
			return ok && ca.Code() == cb.Code() && ca.Error() == cb.Error()
		}
		return false
	})
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()

			cleanTables(t, b.db)
			q := stdlibadapter.New(b.db)
			if _, err := q.Exec(ctx, insert, "carol", "carol@example.com"); err != nil {
				t.Fatalf("seeding through the pool: %v", err)
			}
			_, poolErr := q.Exec(ctx, insert, "dave", "carol@example.com")
			poolCE, ok := errors.AsType[*database.ConstraintError](poolErr)
			if !ok {
				t.Fatalf("pool Exec() error = %v (%T), want a *database.ConstraintError", poolErr, poolErr)
			}

			cleanTables(t, b.db)
			err := sessionPinner(t, b.db).WithSession(ctx, func(session database.Querier) error {
				if _, err := session.Exec(ctx, insert, "carol", "carol@example.com"); err != nil {
					return err
				}
				_, err := session.Exec(ctx, insert, "dave", "carol@example.com")
				return err
			})
			ce, ok := errors.AsType[*database.ConstraintError](err)
			if !ok {
				t.Fatalf("WithSession() error = %v (%T), want a *database.ConstraintError", err, err)
			}
			if ce.Type != database.ConstraintUnique {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
			}
			if diff := cmp.Diff(*poolCE, *ce, sameDriverError); diff != "" {
				t.Errorf("session ConstraintError differs from the pool's for the same violation (-pool +session):\n%s", diff)
			}
		})
	}
}

// TestWithSessionBeginsOnThePinnedSessionMySQL opens a transaction on the
// session through its Begin, as a caller of the pinned Querier would.
func TestWithSessionBeginsOnThePinnedSessionMySQL(t *testing.T) {
	requireDocker(t)
	cleanTable(t, mysqlDB)
	ctx := context.Background()
	err := sessionPinner(t, mysqlDB).WithSession(ctx, func(session database.Querier) error {
		var pinned int64
		if err := session.QueryRow(ctx, "SELECT CONNECTION_ID()").Scan(&pinned); err != nil {
			return err
		}
		return database.WithTransaction(ctx, session, "on_session", func(txCtx context.Context) error {
			tx := database.FromContext(txCtx)
			var inTx int64
			if err := tx.QueryRow(txCtx, "SELECT CONNECTION_ID()").Scan(&inTx); err != nil {
				return err
			}
			if inTx != pinned {
				t.Errorf("the session's transaction runs on connection %d, the session on %d", inTx, pinned)
			}
			_, err := tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "erin", "erin@example.com")
			return err
		})
	})
	if err != nil {
		t.Fatalf("WithSession: %v", err)
	}
	var name string
	if err := mysqlDB.QueryRowContext(ctx, "SELECT name FROM test_users WHERE email = ?", "erin@example.com").Scan(&name); err != nil {
		t.Errorf("the committed row is not readable: %v", err)
	}
}

// TestCommitMapsDeferredConstraintSQLite maps an error that only COMMIT can
// report. With defer_foreign_keys on, SQLite checks foreign keys at commit, so
// the orphan INSERT succeeds and COMMIT fails; Galera certification failures
// (MySQL 1213) reach the caller the same way. It uses its own database because
// what a driver does after a failed COMMIT is its own business: modernc rolls
// the transaction back itself, but a driver that left it open would strand the
// shared handle's one connection for every later test.
func TestCommitMapsDeferredConstraintSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	schema, err := os.ReadFile("testdata/sqlite_schema.sql")
	if err != nil {
		t.Fatalf("reading sqlite schema: %v", err)
	}
	for _, stmt := range []string{"PRAGMA foreign_keys = ON", string(schema)} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("preparing sqlite: %v", err)
		}
	}

	txCtx, err := database.NewTransaction(ctx, stdlibadapter.New(db), "deferred_fk")
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	tx := database.FromContext(txCtx)
	if _, err := tx.Exec(txCtx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		t.Fatalf("deferring foreign keys: %v", err)
	}
	if _, err := tx.Exec(txCtx, "INSERT INTO test_orders (user_id, amount) VALUES (?, ?)", 999999, 1); err != nil {
		t.Fatalf("orphan INSERT failed before COMMIT, so the check was not deferred: %v", err)
	}

	err = database.Commit(txCtx)
	_ = database.Rollback(txCtx) // the transaction is still the caller's after a failed COMMIT
	assertConstraintError(t, err, database.ConstraintError{
		Type:   database.ConstraintForeignKey,
		Detail: "constraint failed: FOREIGN KEY constraint failed (787)",
	}, sqliteErrCode(787))
}

func TestTimeoutRollsBackAndClosesTheTransaction(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		assertTimeoutRollsBack(t, mysqlDB, "INSERT INTO test_users (name, email) VALUES (?, ?)")
	})
	t.Run("sqlite", func(t *testing.T) {
		assertTimeoutRollsBack(t, sqliteDB, "INSERT INTO test_users (name, email) VALUES (?, ?)")
	})
	t.Run("postgres", func(t *testing.T) {
		assertTimeoutRollsBack(t, postgresDB, "INSERT INTO test_users (name, email) VALUES ($1, $2)")
	})
}

func TestCancelledContextBeforeCommitRollsBackAndClosesTheTransaction(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		assertCancelBeforeCommitRollsBack(t, mysqlDB, "INSERT INTO test_users (name, email) VALUES (?, ?)")
	})
	t.Run("sqlite", func(t *testing.T) {
		assertCancelBeforeCommitRollsBack(t, sqliteDB, "INSERT INTO test_users (name, email) VALUES (?, ?)")
	})
	t.Run("postgres", func(t *testing.T) {
		assertCancelBeforeCommitRollsBack(t, postgresDB, "INSERT INTO test_users (name, email) VALUES ($1, $2)")
	})
}

// assertCancelBeforeCommitRollsBack cancels the caller's context inside fn and
// then returns nil, so the COMMIT fails on the cancelled context before
// database/sql has rolled the transaction back on its own. A Timeout cannot
// drive this path deterministically here: the timer closes the transaction
// context's Done channel before it cancels the child database/sql derives in
// BeginTx, so a COMMIT issued in between can still succeed.
func assertCancelBeforeCommitRollsBack(t *testing.T, db *sql.DB, insert string) {
	t.Helper()
	cleanTable(t, db)
	q := stdlibadapter.New(db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var txCtx context.Context
	err := database.WithTransaction(ctx, q, "cancel_commit", func(c context.Context) error {
		txCtx = c
		if _, err := database.Conn(c, q).Exec(c, insert, "dora", "dora@example.com"); err != nil {
			return err
		}
		cancel()
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithTransaction() error = %v, want the commit's cancellation error", err)
	}
	if !database.FromContext(txCtx).IsClosed() {
		t.Error("IsClosed() = false after the failed commit was rolled back")
	}
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0: the cancelled transaction must not commit", n)
	}
}

// assertTimeoutRollsBack runs a transaction whose TxOptions.Timeout expires
// after one write, and checks that the write is rolled back, that
// WithTransaction returns the statement's deadline error, and that the Tx
// reports closed.
func assertTimeoutRollsBack(t *testing.T, db *sql.DB, insert string) {
	t.Helper()
	cleanTable(t, db)
	q := stdlibadapter.New(db)
	ctx := context.Background()

	var txCtx context.Context
	var stmtErr error
	err := database.WithTransaction(ctx, q, "timeout", func(c context.Context) error {
		txCtx = c
		if _, err := database.Conn(c, q).Exec(c, insert, "dora", "dora@example.com"); err != nil {
			return err
		}
		<-c.Done()
		_, stmtErr = database.Conn(c, q).Exec(c, insert, "eve", "eve@example.com")
		return stmtErr
	}, database.TxOptions{Timeout: time.Second})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WithTransaction() error = %v, want a deadline error", err)
	}
	if stmtErr == nil || err != stmtErr { //nolint:errorlint // identity: the rollback succeeded, so WithTransaction returns fn's error untouched
		t.Errorf("WithTransaction() error = %q, want exactly the statement's error %q: the rollback after the timeout must report success", err, stmtErr)
	}
	if !database.FromContext(txCtx).IsClosed() {
		t.Error("IsClosed() = false after the timeout rolled the transaction back")
	}
	if got := database.Conn(txCtx, q); got != q {
		t.Errorf("Conn() = %T, want the pool once the transaction is closed", got)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0: the timeout should roll the write back", n)
	}
}
