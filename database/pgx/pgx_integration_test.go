package pgx_test

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

var testPool *pgxpool.Pool

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

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		panic(err)
	}

	schema, err := os.ReadFile("testdata/schema.sql")
	if err != nil {
		panic(err)
	}
	if _, err := testPool.Exec(ctx, string(schema)); err != nil {
		panic(err)
	}

	code := m.Run()

	testPool.Close()
	_ = pgContainer.Terminate(ctx)
	os.Exit(code)
}

func TestExec(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Clean up from any previous run
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	res, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "alice", "alice@example.com")
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

	// LastInsertId is not supported by PostgreSQL
	_, err = res.LastInsertId()
	if err == nil {
		t.Errorf("LastInsertId() expected error for PostgreSQL, got nil")
	}
}

func TestQuery(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Setup
	_, _ = q.Exec(ctx, "DELETE FROM test_users")
	_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "bob", "bob@example.com")
	_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "carol", "carol@example.com")

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
}

func TestQueryRow(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Setup
	_, _ = q.Exec(ctx, "DELETE FROM test_users")
	_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "dave", "dave@example.com")

	row := q.QueryRow(ctx, "SELECT name, email FROM test_users WHERE name = $1", "dave")
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
}

func TestBeginCommit(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Clean up
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	// Begin a transaction via the database package
	txCtx, err := database.NewTransaction(ctx, q, "test_commit")
	if err != nil {
		t.Fatalf("NewTransaction() unexpected error: %v", err)
	}

	tx := database.FromContext(txCtx)
	if tx == nil {
		t.Fatal("FromContext() returned nil")
	}

	_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "eve", "eve@example.com")
	if err != nil {
		t.Fatalf("Tx.Exec() unexpected error: %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() unexpected error: %v", err)
	}

	// Verify the row persisted after commit
	row := q.QueryRow(ctx, "SELECT name FROM test_users WHERE email = $1", "eve@example.com")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("Scan() after commit unexpected error: %v", err)
	}
	if name != "eve" {
		t.Errorf("name = %q, want %q", name, "eve")
	}
}

func TestBeginRollback(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Clean up
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	// Begin a transaction
	txCtx, err := database.NewTransaction(ctx, q, "test_rollback")
	if err != nil {
		t.Fatalf("NewTransaction() unexpected error: %v", err)
	}

	tx := database.FromContext(txCtx)
	_, err = tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "frank", "frank@example.com")
	if err != nil {
		t.Fatalf("Tx.Exec() unexpected error: %v", err)
	}

	if err := database.Rollback(txCtx); err != nil {
		t.Fatalf("Rollback() unexpected error: %v", err)
	}

	// Verify the row was NOT persisted
	row := q.QueryRow(ctx, "SELECT count(*) FROM test_users WHERE email = $1", "frank@example.com")
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() after rollback unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("count after rollback = %d, want 0", count)
	}
}

func TestSendBatch(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Clean up
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	// Use type assertion to access SendBatch — it's pgx-specific, not on Querier
	adapter, ok := q.(interface {
		SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
	})
	if !ok {
		t.Fatal("adapter does not implement SendBatch")
	}

	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO test_users (name, email) VALUES ($1, $2)", "batch1", "batch1@example.com")
	batch.Queue("INSERT INTO test_users (name, email) VALUES ($1, $2)", "batch2", "batch2@example.com")
	batch.Queue("INSERT INTO test_users (name, email) VALUES ($1, $2)", "batch3", "batch3@example.com")

	br := adapter.SendBatch(ctx, batch)

	for i := range 3 {
		tag, err := br.Exec()
		if err != nil {
			t.Fatalf("batch Exec()[%d] unexpected error: %v", i, err)
		}
		if tag.RowsAffected() != 1 {
			t.Errorf("batch Exec()[%d] RowsAffected() = %d, want 1", i, tag.RowsAffected())
		}
	}

	if err := br.Close(); err != nil {
		t.Fatalf("batch Close() unexpected error: %v", err)
	}

	// Verify all rows were inserted
	row := q.QueryRow(ctx, "SELECT count(*) FROM test_users")
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("count after batch = %d, want 3", count)
	}
}

func TestSendBatchMapsConstraintErrors(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()
	adapter := q.(interface {
		SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
	})
	// An empty name violates CHECK (length(name) > 0).
	const insert = "INSERT INTO test_users (name, email) VALUES ($1, $2) RETURNING id"

	tests := []struct {
		name string
		read func(br pgx.BatchResults) error
	}{
		{name: "Exec", read: func(br pgx.BatchResults) error {
			_, err := br.Exec()
			return err
		}},
		{name: "Query", read: func(br pgx.BatchResults) error {
			rows, err := br.Query()
			rows.Close()
			return err
		}},
		{name: "QueryRow Scan", read: func(br pgx.BatchResults) error {
			var id int
			return br.QueryRow().Scan(&id)
		}},
		{name: "Close", read: func(br pgx.BatchResults) error {
			return br.Close()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = q.Exec(ctx, "DELETE FROM test_orders")
			_, _ = q.Exec(ctx, "DELETE FROM test_users")
			batch := &pgx.Batch{}
			batch.Queue(insert, "", "batch@example.com")
			br := adapter.SendBatch(ctx, batch)
			err := tt.read(br)
			_ = br.Close()

			if !errors.Is(err, database.ErrConstraintViolation) {
				t.Fatalf("batch %s error = %v, want ErrConstraintViolation", tt.name, err)
			}
			ce, ok := errors.AsType[*database.ConstraintError](err)
			if !ok {
				t.Fatalf("batch %s error = %T, want *database.ConstraintError", tt.name, err)
			}
			if ce.Type != database.ConstraintCheck {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintCheck)
			}
		})
	}
}

func TestQuerierMethodsImplemented(t *testing.T) {
	requireDocker(t)
	// Compile-time check is in pgx.go: var _ database.Querier = (*pgxAdapter)(nil)
	// This test verifies New() returns a non-nil value at runtime.
	q := pgxadapter.New(testPool)
	if q == nil {
		t.Fatal("New() returned nil")
	}
}

// --- Error mapping integration tests ---

// assertConstraintError checks that err is exactly want, with Err holding the
// server's *pgconn.PgError carrying code, and that it unwraps to
// ErrConstraintViolation. Rows use explicit ids so the server's Detail, which
// quotes the failing row, is fixed.
func assertConstraintError(t *testing.T, err error, want database.ConstraintError, code string) {
	t.Helper()
	if !errors.Is(err, database.ErrConstraintViolation) {
		t.Errorf("error = %v, want it to wrap ErrConstraintViolation", err)
	}
	ce, ok := errors.AsType[*database.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want a *database.ConstraintError", err, err)
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](ce.Err)
	if !ok || pgErr.Code != code {
		t.Fatalf("ConstraintError.Err = %v (%T), want the server's *pgconn.PgError with code %s", ce.Err, ce.Err, code)
	}
	want.Err = ce.Err
	if diff := cmp.Diff(want, *ce, cmp.Comparer(func(a, b error) bool { return a == b })); diff != "" { //nolint:errorlint // identity: Err must be the very error the driver returned
		t.Errorf("ConstraintError mismatch (-want +got):\n%s", diff)
	}
}

func TestConstraintUniqueViolation(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()
	cleanUsers(t)

	if _, err := q.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9001, "alice", "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	_, err := q.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9002, "bob", "alice@example.com")

	assertConstraintError(t, err, database.ConstraintError{
		Type:       database.ConstraintUnique,
		Constraint: "test_users_email_key",
		Detail:     "Key (email)=(alice@example.com) already exists.",
	}, "23505")
}

func TestConstraintFKViolation(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()
	cleanUsers(t)

	_, err := q.Exec(ctx, "INSERT INTO test_orders (id, user_id, amount) VALUES ($1, $2, $3)", 9001, 99999, 100)

	assertConstraintError(t, err, database.ConstraintError{
		Type:       database.ConstraintForeignKey,
		Constraint: "test_orders_user_id_fkey",
		Detail:     `Key (user_id)=(99999) is not present in table "test_users".`,
	}, "23503")
}

func TestConstraintNotNullViolation(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()
	cleanUsers(t)

	_, err := q.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9001, nil, "test@example.com")

	assertConstraintError(t, err, database.ConstraintError{
		Type:   database.ConstraintNotNull,
		Column: "name",
		Detail: "Failing row contains (9001, null, test@example.com).",
	}, "23502")
}

func TestConstraintCheckViolation(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	ctx := context.Background()
	cleanUsers(t)

	// Empty name violates CHECK (length(name) > 0)
	_, err := q.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES ($1, $2, $3)", 9001, "", "test@example.com")

	assertConstraintError(t, err, database.ConstraintError{
		Type:       database.ConstraintCheck,
		Constraint: "test_users_name_check",
		Detail:     "Failing row contains (9001, , test@example.com).",
	}, "23514")
}

// TestDeadlock drives a real deadlock through the adapter's transaction path
// and checks that the error the losing statement returns is ErrDeadlock and
// still carries the server's own error.
func TestDeadlock(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	q := pgxadapter.New(testPool)

	_, _ = testPool.Exec(ctx, "DELETE FROM test_orders")
	_, _ = testPool.Exec(ctx, "DELETE FROM test_users")
	_, _ = testPool.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES (1, 'alice', 'alice@example.com')")
	_, _ = testPool.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES (2, 'bob', 'bob@example.com')")

	errCh := make(chan error, 2)
	ready1 := make(chan struct{})
	ready2 := make(chan struct{})

	// lockThenCross locks row first, waits for the other transaction to lock
	// its row, then updates the other transaction's row.
	lockThenCross := func(name string, first, second int, mine, theirs chan struct{}) {
		// A setup failure still releases the other transaction, so the test
		// fails on the error instead of hanging.
		released := false
		defer func() {
			if !released {
				close(mine)
			}
		}()
		txCtx, err := database.NewTransaction(ctx, q, name)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = database.Rollback(txCtx) }()
		tx := database.FromContext(txCtx)

		if _, err := tx.Exec(txCtx, "UPDATE test_users SET name = $1 WHERE id = $2", name, first); err != nil {
			errCh <- err
			return
		}
		close(mine)
		released = true
		<-theirs

		_, err = tx.Exec(txCtx, "UPDATE test_users SET name = $1 WHERE id = $2", name, second)
		errCh <- err
	}
	go lockThenCross("tx1", 1, 2, ready1, ready2)
	go lockThenCross("tx2", 2, 1, ready2, ready1)

	var deadlockErr error
	for range 2 {
		if err := <-errCh; errors.Is(err, database.ErrDeadlock) {
			deadlockErr = err
		} else if err != nil {
			t.Errorf("the other transaction's statement error = %v, want nil or ErrDeadlock", err)
		}
	}
	if deadlockErr == nil {
		t.Fatal("neither transaction's statement returned ErrDeadlock")
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](deadlockErr)
	if !ok {
		t.Fatalf("deadlock error = %v, want it to wrap the server's *pgconn.PgError", deadlockErr)
	}
	if pgErr.Code != "40P01" {
		t.Errorf("wrapped PgError code = %q, want %q", pgErr.Code, "40P01")
	}
}

// TestCommitMapsDeferredConstraint maps an error that only COMMIT can report.
// A DEFERRABLE INITIALLY DEFERRED unique constraint is checked at commit, so
// the duplicate INSERT succeeds and COMMIT fails with 23505.
func TestCommitMapsDeferredConstraint(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `CREATE TABLE test_deferred (
		id INT PRIMARY KEY,
		code TEXT NOT NULL,
		CONSTRAINT test_deferred_code_key UNIQUE (code) DEFERRABLE INITIALLY DEFERRED
	)`); err != nil {
		t.Fatalf("creating test_deferred: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), "DROP TABLE test_deferred") })

	txCtx, err := database.NewTransaction(ctx, pgxadapter.New(testPool), "deferred_unique")
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
	}, "23505")
}

func TestTimeoutRollsBackAndClosesTheTransaction(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	q := pgxadapter.New(testPool)
	if _, err := q.Exec(ctx, "DELETE FROM test_users"); err != nil {
		t.Fatal(err)
	}
	const insert = "INSERT INTO test_users (name, email) VALUES ($1, $2)"

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
	if err := testPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0: the timeout should roll the write back", n)
	}
}

func TestTimeoutBeforeCommitRollsBackAndClosesTheTransaction(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	q := pgxadapter.New(testPool)
	if _, err := q.Exec(ctx, "DELETE FROM test_users"); err != nil {
		t.Fatal(err)
	}

	var txCtx context.Context
	err := database.WithTransaction(ctx, q, "timeout_commit", func(c context.Context) error {
		txCtx = c
		if _, err := database.Conn(c, q).Exec(c, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "dora", "dora@example.com"); err != nil {
			return err
		}
		<-c.Done()
		return nil
	}, database.TxOptions{Timeout: time.Second})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WithTransaction() error = %v, want the commit's deadline error", err)
	}
	if !database.FromContext(txCtx).IsClosed() {
		t.Error("IsClosed() = false after the failed commit was rolled back")
	}
	if got := database.Conn(txCtx, q); got != q {
		t.Errorf("Conn() = %T, want the pool once the transaction is closed", got)
	}
	var n int
	if err := testPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0: the timed-out transaction must not commit", n)
	}
}

// TestTxConnOnAnEndedTransaction drives the adapter's Rollback and Commit on
// a pgx transaction that pgx has already closed.
func TestTxConnOnAnEndedTransaction(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	endedTx := func(t *testing.T) database.TxConn {
		t.Helper()
		tx, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("ending the transaction through pgx: %v", err)
		}
		return pgxadapter.NewTxConn(tx)
	}

	t.Run("rollback succeeds", func(t *testing.T) {
		if err := endedTx(t).Rollback(ctx); err != nil {
			t.Errorf("Rollback() of a transaction pgx already closed = %v, want nil", err)
		}
	})
	t.Run("commit is ErrTxDone", func(t *testing.T) {
		err := endedTx(t).Commit(ctx)
		if !errors.Is(err, database.ErrTxDone) {
			t.Errorf("Commit() of a transaction pgx already closed = %v, want it to wrap database.ErrTxDone", err)
		}
		if !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("Commit() of a transaction pgx already closed = %v, want it to wrap pgx.ErrTxClosed", err)
		}
	})
}

func TestRejectedPasswordIsNotConnectionFailed(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	cfg := testPool.Config().Copy()
	cfg.ConnConfig.Password = "wrong"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, err = pgxadapter.New(pool).Exec(ctx, "SELECT 1")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "28P01" {
		t.Fatalf("Exec() error = %v, want the server's 28P01 rejection", err)
	}
	if errors.Is(err, database.ErrConnectionFailed) {
		t.Errorf("Exec() error = %v, want a rejected password not to be ErrConnectionFailed", err)
	}
}
