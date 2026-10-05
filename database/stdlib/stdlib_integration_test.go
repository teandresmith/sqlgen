package stdlib_test

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"os"
	"slices"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	mysqltc "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/database"
	stdlibadapter "github.com/teandresmith/sqlgen/database/stdlib"
)

var (
	mysqlDB  *sql.DB
	sqliteDB *sql.DB
	// mysqlDSN is kept so a test can open a pool with its own settings.
	mysqlDSN string
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

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
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

	code := m.Run()

	_ = mysqlDB.Close()
	_ = sqliteDB.Close()
	_ = mysqlContainer.Terminate(ctx)
	os.Exit(code)
}

func TestExec(t *testing.T) {
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

func TestConstraintUniqueViolation(t *testing.T) {
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "alice", "alice@example.com")
			_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "bob", "alice@example.com")

			if !errors.Is(err, database.ErrConstraintViolation) {
				t.Fatalf("expected ErrConstraintViolation, got %v", err)
			}
			var ce *database.ConstraintError
			if !errors.As(err, &ce) {
				t.Fatalf("expected ConstraintError, got %T: %v", err, err)
			}
			if ce.Type != database.ConstraintUnique {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
			}
			// SQLite names table.column; MySQL's 1062 message names only the key.
			if b.name == "sqlite" && (ce.Constraint != "test_users.email" || ce.Column != "email") {
				t.Errorf("ConstraintError{Constraint:%q Column:%q}, want {%q %q}", ce.Constraint, ce.Column, "test_users.email", "email")
			}
		})
	}
}

func TestConstraintFKViolation(t *testing.T) {
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)

			if b.name == "sqlite" {
				// Enable foreign key enforcement for SQLite
				_, _ = b.db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
			}

			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			_, err := q.Exec(ctx, "INSERT INTO test_orders (user_id, amount) VALUES (?, ?)", 99999, 100)

			if !errors.Is(err, database.ErrConstraintViolation) {
				t.Fatalf("expected ErrConstraintViolation, got %v", err)
			}
			var ce *database.ConstraintError
			if !errors.As(err, &ce) {
				t.Fatalf("expected ConstraintError, got %T: %v", err, err)
			}
			if ce.Type != database.ConstraintForeignKey {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintForeignKey)
			}
			// SQLite's foreign key message names no constraint.
			if b.name == "sqlite" && ce.Constraint != "" {
				t.Errorf("ConstraintError.Constraint = %q, want empty", ce.Constraint)
			}
		})
	}
}

func TestConstraintNotNullViolation(t *testing.T) {
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", nil, "test@example.com")

			if !errors.Is(err, database.ErrConstraintViolation) {
				t.Fatalf("expected ErrConstraintViolation, got %v", err)
			}
			var ce *database.ConstraintError
			if !errors.As(err, &ce) {
				t.Fatalf("expected ConstraintError, got %T: %v", err, err)
			}
			if ce.Type != database.ConstraintNotNull {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintNotNull)
			}
			if ce.Column != "name" {
				t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, "name")
			}
		})
	}
}

func TestConstraintCheckViolation(t *testing.T) {
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)
			q := stdlibadapter.New(b.db)
			ctx := context.Background()

			// Empty name violates CHECK (length(name) > 0)
			_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "", "test@example.com")

			if !errors.Is(err, database.ErrConstraintViolation) {
				t.Fatalf("expected ErrConstraintViolation, got %v", err)
			}
			var ce *database.ConstraintError
			if !errors.As(err, &ce) {
				t.Fatalf("expected ConstraintError, got %T: %v", err, err)
			}
			if ce.Type != database.ConstraintCheck {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintCheck)
			}
		})
	}
}

func TestDeadlockMySQL(t *testing.T) {
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

	var deadlockFound bool
	for range 2 {
		if err := <-errCh; err != nil && errors.Is(err, database.ErrDeadlock) {
			deadlockFound = true
		}
	}
	if !deadlockFound {
		t.Error("expected at least one ErrDeadlock from concurrent MySQL transactions")
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
// exactly as the pool does.
func TestWithSessionMapsErrors(t *testing.T) {
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)
			ctx := context.Background()
			err := sessionPinner(t, b.db).WithSession(ctx, func(session database.Querier) error {
				if _, err := session.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "carol", "carol@example.com"); err != nil {
					return err
				}
				_, err := session.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "dave", "carol@example.com")
				return err
			})
			ce, ok := errors.AsType[*database.ConstraintError](err)
			if !ok {
				t.Fatalf("WithSession() error = %v (%T), want a *database.ConstraintError", err, err)
			}
			if ce.Type != database.ConstraintUnique {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
			}
		})
	}
}

// TestWithSessionBeginsOnThePinnedSessionMySQL opens a transaction on the
// session through its Begin, as a caller of the pinned Querier would.
func TestWithSessionBeginsOnThePinnedSessionMySQL(t *testing.T) {
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
	ce, ok := errors.AsType[*database.ConstraintError](err)
	if !ok {
		t.Fatalf("Commit() error = %v (%T), want a *database.ConstraintError", err, err)
	}
	if ce.Type != database.ConstraintForeignKey {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintForeignKey)
	}
}
