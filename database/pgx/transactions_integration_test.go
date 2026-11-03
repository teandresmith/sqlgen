package pgx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

// holdWindow is how long a statement from a second goroutine must stay
// blocked while the first goroutine holds an open result set. With the
// reservation it waits until the result set is released however long that
// takes, so the window only decides how long the test looks for a statement
// that ran when it should have waited.
const holdWindow = 500 * time.Millisecond

// singleConnPool opens a one-connection pool whose sessions default to
// isolation level defaultIso, so a transaction that reports a different level
// can only have got it from its own BEGIN.
func singleConnPool(t *testing.T, defaultIso string) *pgxpool.Pool {
	t.Helper()
	cfg := testPool.Config().Copy()
	cfg.MaxConns = 1
	params := map[string]string{"default_transaction_isolation": defaultIso}
	for k, v := range cfg.ConnConfig.RuntimeParams {
		if _, set := params[k]; !set {
			params[k] = v
		}
	}
	cfg.ConnConfig.RuntimeParams = params
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("opening a one-connection pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// showInTx begins a transaction with opts and returns what SHOW setting
// reports inside it.
func showInTx(t *testing.T, q database.Querier, setting string, opts database.TxOptions) string {
	t.Helper()
	ctx := context.Background()
	txCtx, err := database.NewTransaction(ctx, q, "show", opts)
	if err != nil {
		t.Fatalf("NewTransaction(%+v) error = %v", opts, err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	var got string
	if err := database.FromContext(txCtx).QueryRow(txCtx, "SHOW "+setting).Scan(&got); err != nil {
		t.Fatalf("SHOW %s inside the transaction: %v", setting, err)
	}
	return got
}

func TestTxOptionsIsolationLevel(t *testing.T) {
	requireDocker(t)
	tests := []struct {
		name  string
		level database.TxIsoLevel
		// sessionDefault differs from the level asked for, so the level
		// reported can only come from the transaction's options.
		sessionDefault string
		want           string
	}{
		{name: "serializable", level: database.Serializable, sessionDefault: "read committed", want: "serializable"},
		{name: "repeatable read", level: database.RepeatableRead, sessionDefault: "serializable", want: "repeatable read"},
		{name: "read committed", level: database.ReadCommitted, sessionDefault: "serializable", want: "read committed"},
		{name: "read uncommitted", level: database.ReadUncommitted, sessionDefault: "serializable", want: "read uncommitted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := pgxadapter.New(singleConnPool(t, tt.sessionDefault))
			got := showInTx(t, q, "transaction_isolation", database.TxOptions{IsoLevel: tt.level})
			if got != tt.want {
				t.Errorf("transaction_isolation in a %q transaction = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

func TestTxOptionsReadOnly(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()
	q := pgxadapter.New(testPool)
	if _, err := q.Exec(ctx, "DELETE FROM test_orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Exec(ctx, "DELETE FROM test_users"); err != nil {
		t.Fatal(err)
	}

	err := database.WithTransaction(ctx, q, "read_only", func(txCtx context.Context) error {
		_, err := database.Conn(txCtx, q).Exec(txCtx, "INSERT INTO test_users (name, email) VALUES ($1, $2)", "ro", "ro@example.com")
		return err
	}, database.TxOptions{AccessMode: database.ReadOnly})

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "25006" {
		t.Fatalf("INSERT in a ReadOnly transaction error = %v, want read_only_sql_transaction (25006)", err)
	}
	var n int
	if err := testPool.QueryRow(ctx, "SELECT COUNT(*) FROM test_users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows after the ReadOnly transaction = %d, want 0", n)
	}
}

func TestTxOptionsDeferrable(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	opts := database.TxOptions{
		IsoLevel:       database.Serializable,
		AccessMode:     database.ReadOnly,
		DeferrableMode: database.Deferrable,
	}
	if got := showInTx(t, q, "transaction_deferrable", opts); got != "on" {
		t.Errorf("transaction_deferrable in a Deferrable transaction = %q, want %q", got, "on")
	}
}

func TestNestedTransactionSavepoints(t *testing.T) {
	requireDocker(t)
	const (
		insert = "INSERT INTO test_users (name, email) VALUES ($1, $2)"
		count  = "SELECT COUNT(*) FROM test_users WHERE email = $1"
	)
	errInner := errors.New("inner scope failed")

	t.Run("inner rollback keeps outer writes", func(t *testing.T) {
		ctx := context.Background()
		q := pgxadapter.New(testPool)
		cleanUsers(t)

		err := database.WithTransaction(ctx, q, "outer_scope", func(txCtx context.Context) error {
			if _, err := database.Conn(txCtx, q).Exec(txCtx, insert, "outer", "outer@example.com"); err != nil {
				return err
			}
			innerErr := database.WithTransaction(txCtx, q, "inner_scope", func(spCtx context.Context) error {
				if _, err := database.Conn(spCtx, q).Exec(spCtx, insert, "inner", "inner@example.com"); err != nil {
					return err
				}
				return errInner
			})
			if !errors.Is(innerErr, errInner) {
				t.Errorf("inner WithTransaction() error = %v, want %v", innerErr, errInner)
			}
			_, err := database.Conn(txCtx, q).Exec(txCtx, insert, "after", "after@example.com")
			return err
		})
		if err != nil {
			t.Fatalf("outer WithTransaction() error = %v", err)
		}

		for email, want := range map[string]int{"outer@example.com": 1, "inner@example.com": 0, "after@example.com": 1} {
			if got := countRows(t, count, email); got != want {
				t.Errorf("rows with email %q = %d, want %d", email, got, want)
			}
		}
	})

	t.Run("inner release keeps inner writes", func(t *testing.T) {
		ctx := context.Background()
		q := pgxadapter.New(testPool)
		cleanUsers(t)

		err := database.WithTransaction(ctx, q, "outer_scope", func(txCtx context.Context) error {
			if _, err := database.Conn(txCtx, q).Exec(txCtx, insert, "outer", "outer@example.com"); err != nil {
				return err
			}
			return database.WithTransaction(txCtx, q, "inner_scope", func(spCtx context.Context) error {
				_, err := database.Conn(spCtx, q).Exec(spCtx, insert, "inner", "inner@example.com")
				return err
			})
		})
		if err != nil {
			t.Fatalf("outer WithTransaction() error = %v", err)
		}

		for email, want := range map[string]int{"outer@example.com": 1, "inner@example.com": 1} {
			if got := countRows(t, count, email); got != want {
				t.Errorf("rows with email %q = %d, want %d", email, got, want)
			}
		}
	})
}

// TestReservationSerializesTwoGoroutinesOverAnOpenResultSet holds a result set
// open on one goroutine while a second goroutine issues a statement on the
// same txCtx. The second must wait for the result set to be released and then
// succeed; reaching the driver while the set is open is `conn busy` on pgx.
func TestReservationSerializesTwoGoroutinesOverAnOpenResultSet(t *testing.T) {
	requireDocker(t)
	q := pgxadapter.New(testPool)
	txCtx, err := database.NewTransaction(context.Background(), q, "two_goroutines")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)
	defer func() { _ = database.Rollback(txCtx) }()

	rows, err := tx.Query(txCtx, "SELECT i FROM generate_series(1, 3) AS i")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !rows.Next() {
		t.Fatalf("Next() = false on the first row, want true (err %v)", rows.Err())
	}

	done := make(chan error, 1)
	go func() {
		_, err := tx.Exec(txCtx, "SELECT 1")
		done <- err
	}()

	select {
	case err := <-done:
		_ = rows.Close()
		t.Fatalf("Exec() from a second goroutine returned (error %v) while the first held an open result set, want it to wait", err)
	case <-time.After(holdWindow):
	}

	n := 1
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() = %v", err)
	}
	if n != 3 {
		t.Errorf("read %d rows, want 3", n)
	}
	if err := rows.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if err := <-done; err != nil {
		t.Errorf("Exec() from the second goroutine after the release = %v, want nil", err)
	}
	if _, err := tx.Exec(txCtx, "SELECT 1"); err != nil {
		t.Errorf("Exec() after both statements = %v, want nil", err)
	}
	if err := database.Commit(txCtx); err != nil {
		t.Errorf("Commit() = %v, want nil", err)
	}
}

func cleanUsers(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, "DELETE FROM test_orders"); err != nil {
		t.Fatalf("cleaning test_orders: %v", err)
	}
	if _, err := testPool.Exec(ctx, "DELETE FROM test_users"); err != nil {
		t.Fatalf("cleaning test_users: %v", err)
	}
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}
