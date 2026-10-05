package pgx_test

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
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

func TestQuerierMethodsImplemented(t *testing.T) {
	// Compile-time check is in pgx.go: var _ database.Querier = (*pgxAdapter)(nil)
	// This test verifies New() returns a non-nil value at runtime.
	q := pgxadapter.New(testPool)
	if q == nil {
		t.Fatal("New() returned nil")
	}
}

// --- Error mapping integration tests ---

func TestConstraintUniqueViolation(t *testing.T) {
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	_, _ = q.Exec(ctx, "DELETE FROM test_orders")
	_, _ = q.Exec(ctx, "DELETE FROM test_users")
	_, _ = q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "alice", "alice@example.com")

	_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "bob", "alice@example.com")

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
	if ce.Constraint == "" {
		t.Error("ConstraintError.Constraint is empty, want non-empty")
	}
}

func TestConstraintFKViolation(t *testing.T) {
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	_, _ = q.Exec(ctx, "DELETE FROM test_orders")
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	// Insert with non-existent user_id
	_, err := q.Exec(ctx, "INSERT INTO test_orders (user_id, amount) VALUES ($1, $2)", 99999, 100)

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
}

func TestConstraintNotNullViolation(t *testing.T) {
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	_, _ = q.Exec(ctx, "DELETE FROM test_orders")
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", nil, "test@example.com")

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
}

func TestConstraintCheckViolation(t *testing.T) {
	q := pgxadapter.New(testPool)
	ctx := context.Background()

	_, _ = q.Exec(ctx, "DELETE FROM test_orders")
	_, _ = q.Exec(ctx, "DELETE FROM test_users")

	// Empty name violates CHECK (length(name) > 0)
	_, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "", "test@example.com")

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
}

func TestDeadlock(t *testing.T) {
	ctx := context.Background()

	// Setup: create two rows for the deadlock scenario
	_, _ = testPool.Exec(ctx, "DELETE FROM test_orders")
	_, _ = testPool.Exec(ctx, "DELETE FROM test_users")
	_, _ = testPool.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES (1, 'alice', 'alice@example.com')")
	_, _ = testPool.Exec(ctx, "INSERT INTO test_users (id, name, email) VALUES (2, 'bob', 'bob@example.com')")

	errCh := make(chan error, 2)
	ready1 := make(chan struct{})
	ready2 := make(chan struct{})

	// Transaction 1: lock row 1, then try row 2
	go func() {
		tx1, err := testPool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = tx1.Rollback(ctx) }()

		_, err = tx1.Exec(ctx, "UPDATE test_users SET name = 'a1' WHERE id = 1")
		if err != nil {
			errCh <- err
			return
		}
		close(ready1)
		<-ready2

		_, err = tx1.Exec(ctx, "UPDATE test_users SET name = 'a2' WHERE id = 2")
		_, mapped := pgxadapter.MapError(err)
		errCh <- mapped
	}()

	// Transaction 2: lock row 2, then try row 1
	go func() {
		tx2, err := testPool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer func() { _ = tx2.Rollback(ctx) }()

		_, err = tx2.Exec(ctx, "UPDATE test_users SET name = 'b2' WHERE id = 2")
		if err != nil {
			errCh <- err
			return
		}
		close(ready2)
		<-ready1

		_, err = tx2.Exec(ctx, "UPDATE test_users SET name = 'b1' WHERE id = 1")
		_, mapped := pgxadapter.MapError(err)
		errCh <- mapped
	}()

	var deadlockFound bool
	for range 2 {
		if err := <-errCh; err != nil && errors.Is(err, database.ErrDeadlock) {
			deadlockFound = true
		}
	}
	if !deadlockFound {
		t.Error("expected at least one ErrDeadlock from concurrent transactions")
	}
}

// TestCommitMapsDeferredConstraint maps an error that only COMMIT can report.
// A DEFERRABLE INITIALLY DEFERRED unique constraint is checked at commit, so
// the duplicate INSERT succeeds and COMMIT fails with 23505.
func TestCommitMapsDeferredConstraint(t *testing.T) {
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
	ce, ok := errors.AsType[*database.ConstraintError](err)
	if !ok {
		t.Fatalf("Commit() error = %v (%T), want a *database.ConstraintError", err, err)
	}
	if ce.Type != database.ConstraintUnique || ce.Constraint != "test_deferred_code_key" {
		t.Errorf("ConstraintError = {Type: %q, Constraint: %q}, want {%q, %q}", ce.Type, ce.Constraint, database.ConstraintUnique, "test_deferred_code_key")
	}
}
