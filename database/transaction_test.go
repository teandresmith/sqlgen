package database_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/database"
)

// --- Mock types ---

type mockTxConn struct {
	mu         sync.Mutex
	execCalls  []string
	execFn     func(ctx context.Context, sql string, args ...any) (database.Result, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (database.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) database.Row
	commitFn   func(ctx context.Context) error
	rollbackFn func(ctx context.Context) error
}

func (m *mockTxConn) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	m.mu.Lock()
	m.execCalls = append(m.execCalls, sql)
	m.mu.Unlock()
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return &mockResult{}, nil
}

func (m *mockTxConn) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *mockTxConn) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}

func (m *mockTxConn) Commit(ctx context.Context) error {
	if m.commitFn != nil {
		return m.commitFn(ctx)
	}
	return nil
}

func (m *mockTxConn) Rollback(ctx context.Context) error {
	if m.rollbackFn != nil {
		return m.rollbackFn(ctx)
	}
	return nil
}

func (m *mockTxConn) getExecCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.execCalls))
	copy(result, m.execCalls)
	return result
}

type mockQuerier struct {
	conn *mockTxConn
	mode database.CallbackMode
}

func (m *mockQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	return m.conn.Exec(ctx, sql, args...)
}

func (m *mockQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	return m.conn.Query(ctx, sql, args...)
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return m.conn.QueryRow(ctx, sql, args...)
}

func (m *mockQuerier) Begin(_ context.Context, name string, _ ...database.TxOptions) (*database.Tx, error) {
	return database.NewTx(m.conn, name, m.mode), nil
}

type mockResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (r *mockResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }
func (r *mockResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }

type mockRow struct {
	scanErr error
}

func (r *mockRow) Scan(_ ...any) error { return r.scanErr }

type mockRows struct {
	closed  atomic.Bool
	closeFn func() error
	nextFn  func() bool
	scanFn  func(dest ...any) error
	errFn   func() error
}

func (r *mockRows) Next() bool {
	if r.nextFn != nil {
		return r.nextFn()
	}
	return false
}

func (r *mockRows) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

func (r *mockRows) Columns() ([]string, error) { return nil, nil }

func (r *mockRows) Close() error {
	r.closed.Store(true)
	if r.closeFn != nil {
		return r.closeFn()
	}
	return nil
}

func (r *mockRows) Err() error {
	if r.errFn != nil {
		return r.errFn()
	}
	return nil
}

// --- Helpers ---

func newTestSetup(mode database.CallbackMode) (context.Context, *mockQuerier) {
	conn := &mockTxConn{}
	q := &mockQuerier{conn: conn, mode: mode}
	return context.Background(), q
}

// --- Tests ---

func TestTransactionLifecycle(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	txCtx, err := database.NewTransaction(ctx, q, "test_tx")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	if tx == nil {
		t.Fatal("FromContext() returned nil")
	}

	if _, err := tx.Exec(txCtx, "INSERT INTO users (name) VALUES ($1)", "alice"); err != nil {
		t.Fatalf("Tx.Exec() error = %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if !tx.IsClosed() {
		t.Error("IsClosed() = false after commit, want true")
	}
}

func TestSavepointNesting(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	txCtx, err := database.NewTransaction(ctx, q, "outer")
	if err != nil {
		t.Fatalf("NewTransaction(outer) error = %v", err)
	}

	spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
	if err != nil {
		t.Fatalf("NewTransaction(inner_sp) error = %v", err)
	}

	if err := database.Commit(spCtx); err != nil {
		t.Fatalf("Commit(savepoint) error = %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit(outer) error = %v", err)
	}

	calls := q.conn.getExecCalls()
	want := []string{"SAVEPOINT inner_sp", "RELEASE SAVEPOINT inner_sp"}
	if len(calls) < len(want) {
		t.Fatalf("exec calls = %v, want at least %v", calls, want)
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("exec call[%d] = %q, want %q", i, calls[i], w)
		}
	}
}

func TestSavepointRollback(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	txCtx, err := database.NewTransaction(ctx, q, "outer")
	if err != nil {
		t.Fatalf("NewTransaction(outer) error = %v", err)
	}

	spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
	if err != nil {
		t.Fatalf("NewTransaction(inner_sp) error = %v", err)
	}

	if err := database.Rollback(spCtx); err != nil {
		t.Fatalf("Rollback(savepoint) error = %v", err)
	}

	tx := database.FromContext(txCtx)
	if tx.IsClosed() {
		t.Error("outer transaction closed after savepoint rollback, want open")
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit(outer) error = %v", err)
	}

	calls := q.conn.getExecCalls()
	want := []string{"SAVEPOINT inner_sp", "ROLLBACK TO SAVEPOINT inner_sp"}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("exec call[%d] = %q, want %q", i, calls[i], w)
		}
	}
}

func TestOnCommitCallbackOrdering(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	var order []int

	txCtx, err := database.NewTransaction(ctx, q, "test")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error { order = append(order, 1); return nil })
	tx.OnCommit(func(_ context.Context) error { order = append(order, 2); return nil })
	tx.OnCommit(func(_ context.Context) error { order = append(order, 3); return nil })

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("callback count = %d, want 3", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("order[%d] = %d, want %d", i, v, i+1)
		}
	}
}

func TestOnCommitPromotion(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	var order []string

	txCtx, err := database.NewTransaction(ctx, q, "outer")
	if err != nil {
		t.Fatalf("NewTransaction(outer) error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error { order = append(order, "A"); return nil })

	spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
	if err != nil {
		t.Fatalf("NewTransaction(inner_sp) error = %v", err)
	}

	tx.OnCommit(func(_ context.Context) error { order = append(order, "B"); return nil })
	tx.OnCommit(func(_ context.Context) error { order = append(order, "C"); return nil })

	if err := database.Commit(spCtx); err != nil {
		t.Fatalf("Commit(savepoint) error = %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit(outer) error = %v", err)
	}

	want := []string{"A", "B", "C"}
	if len(order) != len(want) {
		t.Fatalf("callback count = %d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestOnCommitDiscard(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	var order []string

	txCtx, err := database.NewTransaction(ctx, q, "outer")
	if err != nil {
		t.Fatalf("NewTransaction(outer) error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error { order = append(order, "A"); return nil })

	spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
	if err != nil {
		t.Fatalf("NewTransaction(inner_sp) error = %v", err)
	}

	tx.OnCommit(func(_ context.Context) error { order = append(order, "B"); return nil })

	if err := database.Rollback(spCtx); err != nil {
		t.Fatalf("Rollback(savepoint) error = %v", err)
	}

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit(outer) error = %v", err)
	}

	if len(order) != 1 || order[0] != "A" {
		t.Errorf("callbacks = %v, want [A]", order)
	}
}

func TestRootRollbackDiscardsAll(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	var called bool

	txCtx, err := database.NewTransaction(ctx, q, "outer")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error { called = true; return nil })

	spCtx, err := database.NewTransaction(txCtx, q, "inner_sp")
	if err != nil {
		t.Fatalf("NewTransaction(inner_sp) error = %v", err)
	}
	tx.OnCommit(func(_ context.Context) error { called = true; return nil })

	if err := database.Commit(spCtx); err != nil {
		t.Fatalf("Commit(savepoint) error = %v", err)
	}

	if err := database.Rollback(txCtx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if called {
		t.Error("callbacks fired after root rollback, want all discarded")
	}
}

func TestConnReturnsTransaction(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	// Without transaction — returns original querier
	got := database.Conn(ctx, q)
	if got != q {
		t.Error("Conn() without transaction should return original querier")
	}

	// With open transaction — returns Tx
	txCtx, err := database.NewTransaction(ctx, q, "test")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)
	got = database.Conn(txCtx, q)
	if got != tx {
		t.Error("Conn() with open transaction should return Tx")
	}

	// After close — falls back to original querier
	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	got = database.Conn(txCtx, q)
	if got != q {
		t.Error("Conn() with closed transaction should return original querier")
	}
}

func TestIsClosedAfterCommitAndRollback(t *testing.T) {
	tests := []struct {
		name   string
		action string
	}{
		{name: "closed after commit", action: "commit"},
		{name: "closed after rollback", action: "rollback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, q := newTestSetup(database.CallbackAsync)
			txCtx, err := database.NewTransaction(ctx, q, "test")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			if tx.IsClosed() {
				t.Fatal("IsClosed() = true before action, want false")
			}

			switch tt.action {
			case "commit":
				err = database.Commit(txCtx)
			case "rollback":
				err = database.Rollback(txCtx)
			}
			if err != nil {
				t.Fatalf("%s() error = %v", tt.action, err)
			}

			if !tx.IsClosed() {
				t.Errorf("IsClosed() = false after %s, want true", tt.action)
			}
		})
	}
}

// TestConcurrentSafety covers the mutex's actual scope: the Tx struct's own
// state. The querier here is a fake with no driver connection underneath, so
// this says nothing about how concurrent *statements* reach the driver — the
// connection reservation is what makes those safe, and the tests below it are
// what assert it.
func TestConcurrentSafety(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	txCtx, err := database.NewTransaction(ctx, q, "concurrent")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			tx.OnCommit(func(_ context.Context) error { return nil })
			_, _ = tx.Exec(txCtx, fmt.Sprintf("SELECT %d", i))
			_ = tx.IsClosed()
		})
	}
	wg.Wait()

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func TestWithTransactionAutoCommit(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	var committed bool
	q.conn.commitFn = func(_ context.Context) error {
		committed = true
		return nil
	}

	err := database.WithTransaction(ctx, q, "test", func(txCtx context.Context) error {
		_, err := database.FromContext(txCtx).Exec(txCtx, "INSERT INTO t VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatalf("WithTransaction() error = %v", err)
	}

	if !committed {
		t.Error("transaction was not committed")
	}
}

func TestWithTransactionAutoRollback(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	var rolledBack bool
	q.conn.rollbackFn = func(_ context.Context) error {
		rolledBack = true
		return nil
	}

	testErr := errors.New("test error")
	err := database.WithTransaction(ctx, q, "test", func(_ context.Context) error {
		return testErr
	})
	if !errors.Is(err, testErr) {
		t.Fatalf("WithTransaction() error = %v, want %v", err, testErr)
	}

	if !rolledBack {
		t.Error("transaction was not rolled back")
	}
}

func TestNestedWithTransaction(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	err := database.WithTransaction(ctx, q, "outer", func(txCtx context.Context) error {
		return database.WithTransaction(txCtx, q, "inner_sp", func(spCtx context.Context) error {
			tx := database.FromContext(spCtx)
			if tx == nil {
				t.Error("FromContext() returned nil in nested WithTransaction")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("WithTransaction() error = %v", err)
	}

	calls := q.conn.getExecCalls()
	if len(calls) < 2 {
		t.Fatalf("exec calls = %v, want savepoint operations", calls)
	}
	if calls[0] != "SAVEPOINT inner_sp" {
		t.Errorf("exec call[0] = %q, want %q", calls[0], "SAVEPOINT inner_sp")
	}
	if calls[1] != "RELEASE SAVEPOINT inner_sp" {
		t.Errorf("exec call[1] = %q, want %q", calls[1], "RELEASE SAVEPOINT inner_sp")
	}
}

func TestTxOptionsVariadic(t *testing.T) {
	tests := []struct {
		name    string
		opts    []database.TxOptions
		wantErr bool
	}{
		{name: "zero args uses defaults", opts: nil},
		{name: "one arg applies", opts: []database.TxOptions{{IsoLevel: database.Serializable}}},
		{name: "more than one returns error", opts: []database.TxOptions{{}, {}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, q := newTestSetup(database.CallbackAsync)

			txCtx, err := database.NewTransaction(ctx, q, "test", tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Error("NewTransaction() expected error with >1 TxOptions, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			_ = database.Commit(txCtx)
		})
	}
}

func TestAsyncCallbackMode(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	var done atomic.Bool

	txCtx, err := database.NewTransaction(ctx, q, "test")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error {
		done.Store(true)
		return nil
	})

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Wait for async callback with timeout
	deadline := time.After(2 * time.Second)
	for !done.Load() {
		select {
		case <-deadline:
			t.Fatal("async callback did not fire within timeout")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestSyncCallbackMode(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackSync)

	callbackErr := errors.New("callback failed")
	var callCount atomic.Int32

	txCtx, err := database.NewTransaction(ctx, q, "test")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	tx.OnCommit(func(_ context.Context) error {
		callCount.Add(1)
		return callbackErr
	})
	// Second callback should not run because the first fails after retry
	tx.OnCommit(func(_ context.Context) error {
		callCount.Add(1)
		return nil
	})

	err = database.Commit(txCtx)
	if err == nil {
		t.Fatal("Commit() error = nil, want callback error")
	}

	// First callback: initial call + retry = 2 calls, then error returned
	if callCount.Load() != 2 {
		t.Errorf("callback call count = %d, want 2 (initial + retry)", callCount.Load())
	}
}

func TestCallbackRetryOnFailure(t *testing.T) {
	t.Run("retry succeeds", func(t *testing.T) {
		ctx, q := newTestSetup(database.CallbackSync)

		var attempts atomic.Int32

		txCtx, err := database.NewTransaction(ctx, q, "test")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		tx := database.FromContext(txCtx)
		tx.OnCommit(func(_ context.Context) error {
			n := attempts.Add(1)
			if n == 1 {
				return errors.New("transient error")
			}
			return nil
		})

		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v, want nil (retry should succeed)", err)
		}

		if attempts.Load() != 2 {
			t.Errorf("attempts = %d, want 2 (initial + retry)", attempts.Load())
		}
	})

	t.Run("retry fails continues to next in async", func(t *testing.T) {
		ctx, q := newTestSetup(database.CallbackAsync)

		var secondCalled atomic.Bool

		txCtx, err := database.NewTransaction(ctx, q, "test")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		tx := database.FromContext(txCtx)
		// First callback always fails
		tx.OnCommit(func(_ context.Context) error {
			return errors.New("persistent error")
		})
		// Second callback should still fire in async mode
		tx.OnCommit(func(_ context.Context) error {
			secondCalled.Store(true)
			return nil
		})

		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		deadline := time.After(2 * time.Second)
		for !secondCalled.Load() {
			select {
			case <-deadline:
				t.Fatal("second async callback did not fire within timeout")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
}

func TestQueryFuncAutoCloseRows(t *testing.T) {
	rows := &mockRows{}
	q := &staticQuerier{rows: rows, row: &mockRow{}}

	var called bool
	err := database.QueryFunc(context.Background(), q, "SELECT 1", nil, func(r database.Rows) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("QueryFunc() error = %v", err)
	}
	if !called {
		t.Error("callback was not called")
	}
	if !rows.closed.Load() {
		t.Error("rows were not closed after QueryFunc")
	}
}

func TestQueryRowFuncSingleRow(t *testing.T) {
	row := &mockRow{}
	q := &staticQuerier{rows: &mockRows{}, row: row}

	var called bool
	err := database.QueryRowFunc(context.Background(), q, "SELECT 1", nil, func(r database.Row) error {
		called = true
		return r.Scan()
	})
	if err != nil {
		t.Fatalf("QueryRowFunc() error = %v", err)
	}
	if !called {
		t.Error("callback was not called")
	}
}

func TestOperationsOnClosedTransaction(t *testing.T) {
	ctx, q := newTestSetup(database.CallbackAsync)

	txCtx, err := database.NewTransaction(ctx, q, "test")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	tx := database.FromContext(txCtx)
	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if _, err := tx.Exec(txCtx, "SELECT 1"); err == nil {
		t.Error("Exec on closed transaction should return error")
	}

	if _, err := tx.Query(txCtx, "SELECT 1"); err == nil {
		t.Error("Query on closed transaction should return error")
	}

	row := tx.QueryRow(txCtx, "SELECT 1")
	if err := row.Scan(); err == nil {
		t.Error("QueryRow.Scan on closed transaction should return error")
	}

	if _, err := tx.Begin(txCtx, "sp"); err == nil {
		t.Error("Begin on closed transaction should return error")
	}
}

func TestFromContextReturnsNil(t *testing.T) {
	tx := database.FromContext(context.Background())
	if tx != nil {
		t.Errorf("FromContext(background) = %v, want nil", tx)
	}
}

func TestCommitWithoutTransaction(t *testing.T) {
	if err := database.Commit(context.Background()); err == nil {
		t.Error("Commit without transaction should return error")
	}
}

func TestRollbackWithoutTransaction(t *testing.T) {
	if err := database.Rollback(context.Background()); err == nil {
		t.Error("Rollback without transaction should return error")
	}
}

// TestTxBegin_RejectsUnsafeSavepointName guards savepoint names. The database
// layer is driver-agnostic and cannot QuoteIdentifier per dialect, so
// caller-supplied savepoint names must validate at the sqlgen boundary against
// a cross-dialect safe grammar. Without this gate, names containing hyphens or
// shadowing a reserved word (e.g. "inner" in postgres) reach the DB and surface
// as cryptic dialect-specific syntax errors instead of a clear validation
// message.
func TestTxBegin_RejectsUnsafeSavepointName(t *testing.T) {
	tests := []struct {
		name      string
		spName    string
		wantError bool
	}{
		{name: "reserved word lowercase", spName: "inner", wantError: true},
		{name: "reserved word mixed case", spName: "Inner", wantError: true},
		{name: "reserved word outer", spName: "outer", wantError: true},
		{name: "reserved word select", spName: "select", wantError: true},
		{name: "hyphen", spName: "outer-release", wantError: true},
		{name: "leading digit", spName: "1inner", wantError: true},
		{name: "space", spName: "outer inner", wantError: true},
		{name: "double quote", spName: `"inner"`, wantError: true},
		{name: "backtick", spName: "`inner`", wantError: true},
		{name: "empty", spName: "", wantError: true},
		{name: "valid underscore", spName: "outer_release", wantError: false},
		{name: "valid leading underscore", spName: "_sp", wantError: false},
		{name: "valid alnum tail", spName: "sp1", wantError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, q := newTestSetup(database.CallbackAsync)

			txCtx, err := database.NewTransaction(ctx, q, "outer_tx")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}

			tx := database.FromContext(txCtx)
			_, err = tx.Begin(txCtx, tt.spName)
			if tt.wantError {
				if err == nil {
					t.Fatalf("Tx.Begin(%q) error = nil, want validation error", tt.spName)
				}
				// Confirm no SAVEPOINT SQL was issued — validation must reject
				// before the conn.Exec call.
				for _, call := range q.conn.getExecCalls() {
					if call == fmt.Sprintf("SAVEPOINT %s", tt.spName) {
						t.Errorf("SAVEPOINT %q issued despite validation error", tt.spName)
					}
				}
			} else if err != nil {
				t.Fatalf("Tx.Begin(%q) error = %v, want nil", tt.spName, err)
			}
		})
	}
}

// staticQuerier is a simple Querier that returns fixed rows/row for convenience tests.
type staticQuerier struct {
	rows *mockRows
	row  *mockRow
}

func (q *staticQuerier) Exec(_ context.Context, _ string, _ ...any) (database.Result, error) {
	return &mockResult{}, nil
}

func (q *staticQuerier) Query(_ context.Context, _ string, _ ...any) (database.Rows, error) {
	return q.rows, nil
}

func (q *staticQuerier) QueryRow(_ context.Context, _ string, _ ...any) database.Row {
	return q.row
}

func (q *staticQuerier) Begin(_ context.Context, _ string, _ ...database.TxOptions) (*database.Tx, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- InTransaction ---

func TestInTransaction_NoTransactionInContext(t *testing.T) {
	if database.InTransaction(context.Background()) {
		t.Error("InTransaction(ctx) = true on a bare ctx, want false")
	}
}

func TestInTransaction_TrueInsideWithTransaction(t *testing.T) {
	q := &mockQuerier{conn: &mockTxConn{}}

	var observed bool
	err := database.WithTransaction(context.Background(), q, "in_tx_check", func(ctx context.Context) error {
		observed = database.InTransaction(ctx)
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransaction: %v", err)
	}
	if !observed {
		t.Error("InTransaction(ctx) = false inside WithTransaction body, want true")
	}
}

func TestInTransaction_FalseAfterCommit(t *testing.T) {
	q := &mockQuerier{conn: &mockTxConn{}}

	txCtx, err := database.NewTransaction(context.Background(), q, "commit_check")
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	if !database.InTransaction(txCtx) {
		t.Fatal("InTransaction = false immediately after NewTransaction, want true")
	}
	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if database.InTransaction(txCtx) {
		t.Error("InTransaction(ctx) = true after Commit, want false (tx is closed)")
	}
}

func TestInTransaction_FalseAfterRollback(t *testing.T) {
	q := &mockQuerier{conn: &mockTxConn{}}

	txCtx, err := database.NewTransaction(context.Background(), q, "rollback_check")
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	if !database.InTransaction(txCtx) {
		t.Fatal("InTransaction = false immediately after NewTransaction, want true")
	}
	if err := database.Rollback(txCtx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if database.InTransaction(txCtx) {
		t.Error("InTransaction(ctx) = true after Rollback, want false (tx is closed)")
	}
}

func TestInTransaction_TrueInsideSavepoint(t *testing.T) {
	q := &mockQuerier{conn: &mockTxConn{}}

	err := database.WithTransaction(context.Background(), q, "outer_for_savepoint", func(outerCtx context.Context) error {
		// Nested call creates a savepoint via NewTransaction's reuse path; the
		// active outer Tx remains the source of truth for InTransaction.
		return database.WithTransaction(outerCtx, q, "inner_savepoint", func(innerCtx context.Context) error {
			if !database.InTransaction(innerCtx) {
				t.Error("InTransaction = false inside savepoint body, want true")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("WithTransaction: %v", err)
	}
}

// --- PRD §18.5: no tx.mu acquisition spans a driver round trip ---

// testWaitBound is how long runWithin gives a call that must not block. The
// paths it guards fail by parking forever — on a non-reentrant mutex, or on a
// reservation nothing will release — so the value only has to be long enough
// that a slow machine is not mistaken for a deadlock.
const testWaitBound = 5 * time.Second

// runWithin runs fn on its own goroutine and fails if it has not returned
// within testWaitBound. Without the bound a regression would hang the whole
// package until the test binary's timeout rather than naming the path that
// broke.
func runWithin(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(testWaitBound):
		t.Fatalf("%s did not return within %s: it is waiting on a lock held across a driver call (PRD §18.5)", what, testWaitBound)
	}
}

// TestStatementsDoNotHoldStructMutex gates the rule PRD §18.5 states as "two
// locks, and why they cannot be one": every Tx method that issues a statement
// releases tx.mu across it. database.Conn reaches IsClosed() — and so tx.mu —
// at the head of every generated read, and the cache layer reaches OnCommit
// from inside a hook, so a struct mutex held for the length of a round trip
// parks both. Each case below reaches back into the Tx from inside the driver
// call; on a regression the non-reentrant mutex deadlocks.
//
// IsClosed() must report false from inside every one of them, the two root
// teardowns included: a teardown in flight has not committed or rolled back
// yet, which is what PRD §18.4 specifies the flag to report and what §18.6's
// deferred side effects key off. What makes a root teardown at-most-once is the
// separate tearingDown claim — TestRootTeardownIsClaimedOnce gates that, and
// TestIsClosedDuringRootTeardown gates the window's full consequences.
func TestStatementsDoNotHoldStructMutex(t *testing.T) {
	tests := []struct {
		name string
		// drive issues the statement under test. It runs with the Tx already
		// open at the depth the case needs.
		drive func(t *testing.T, txCtx context.Context, q *mockQuerier)
		// hook names the mockTxConn field the driver call arrives on.
		hook string
		// wantCalls is how many driver calls drive is expected to make, so a
		// case cannot pass by never reaching the statement it names.
		wantCalls int
	}{
		{
			name:      "savepoint begin",
			hook:      "exec",
			wantCalls: 1,
			drive: func(t *testing.T, txCtx context.Context, q *mockQuerier) {
				t.Helper()
				if _, err := database.NewTransaction(txCtx, q, "sp"); err != nil {
					t.Errorf("NewTransaction(savepoint) error = %v", err)
				}
			},
		},
		{
			name:      "savepoint release",
			hook:      "exec",
			wantCalls: 2,
			drive: func(t *testing.T, txCtx context.Context, q *mockQuerier) {
				t.Helper()
				spCtx, err := database.NewTransaction(txCtx, q, "sp")
				if err != nil {
					// Not t.Fatalf: drive runs on runWithin's goroutine.
					t.Errorf("NewTransaction(savepoint) error = %v", err)
					return
				}
				if err := database.Commit(spCtx); err != nil {
					t.Errorf("Commit(savepoint) error = %v", err)
				}
			},
		},
		{
			name:      "savepoint rollback",
			hook:      "exec",
			wantCalls: 2,
			drive: func(t *testing.T, txCtx context.Context, q *mockQuerier) {
				t.Helper()
				spCtx, err := database.NewTransaction(txCtx, q, "sp")
				if err != nil {
					// Not t.Fatalf: drive runs on runWithin's goroutine.
					t.Errorf("NewTransaction(savepoint) error = %v", err)
					return
				}
				if err := database.Rollback(spCtx); err != nil {
					t.Errorf("Rollback(savepoint) error = %v", err)
				}
			},
		},
		{
			name:      "plain exec",
			hook:      "exec",
			wantCalls: 1,
			drive: func(t *testing.T, txCtx context.Context, _ *mockQuerier) {
				t.Helper()
				tx := database.FromContext(txCtx)
				if _, err := tx.Exec(txCtx, "UPDATE t SET c = 1"); err != nil {
					t.Errorf("Exec() error = %v", err)
				}
			},
		},
		{
			name:      "root commit",
			hook:      "commit",
			wantCalls: 1,
			drive: func(t *testing.T, txCtx context.Context, _ *mockQuerier) {
				t.Helper()
				if err := database.Commit(txCtx); err != nil {
					t.Errorf("Commit(root) error = %v", err)
				}
			},
		},
		{
			name:      "root rollback",
			hook:      "rollback",
			wantCalls: 1,
			drive: func(t *testing.T, txCtx context.Context, _ *mockQuerier) {
				t.Helper()
				if err := database.Rollback(txCtx); err != nil {
					t.Errorf("Rollback(root) error = %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{}
			q := &mockQuerier{conn: conn, mode: database.CallbackSync}

			// reenter is what the driver call runs. Both acquisitions below are
			// tx.mu; both are reached from real generated code (database.Conn
			// and the cache layer's dispatchRefresh respectively).
			observed := make(chan bool, 8)
			reenter := func() { t.Error("a driver call was made before the Tx existed") }
			conn.execFn = func(context.Context, string, ...any) (database.Result, error) {
				reenter()
				return &mockResult{}, nil
			}
			conn.commitFn = func(context.Context) error {
				reenter()
				return nil
			}
			conn.rollbackFn = func(context.Context) error {
				reenter()
				return nil
			}

			txCtx, err := database.NewTransaction(context.Background(), q, "root")
			if err != nil {
				t.Fatalf("NewTransaction(root) error = %v", err)
			}
			tx := database.FromContext(txCtx)
			reenter = func() {
				observed <- tx.IsClosed()
				tx.OnCommit(func(context.Context) error { return nil })
			}

			runWithin(t, tt.name, func() { tt.drive(t, txCtx, q) })

			close(observed)
			var calls int
			for got := range observed {
				calls++
				if got {
					t.Errorf("IsClosed() inside %s driver call %d = true, want false", tt.hook, calls)
				}
			}
			if calls != tt.wantCalls {
				t.Errorf("%s driver calls = %d, want %d", tt.hook, calls, tt.wantCalls)
			}
		})
	}
}

// TestBeginRejectsATeardownRacingItsSavepoint closes the window the untangle
// opens in Begin. Pre-change, Begin held tx.mu for its whole body, so the
// closed check and the bookkeeping push were one atomic step. With the mutex
// released across the SAVEPOINT statement they are two, and a root teardown
// landing between them would leave a closed Tx at depth > 0 carrying a
// savepoint name nothing can release. The dangling frame is not reachable
// through the public API — Commit and Rollback both short-circuit on closed —
// so what this asserts is the observable half: Begin must not report success
// for a savepoint scope the caller does not have.
//
// The interleaving is made deterministic here by tearing the transaction down
// from inside the SAVEPOINT statement itself — which is only reachable at all
// because the mutex is no longer held across it.
func TestBeginRejectsATeardownRacingItsSavepoint(t *testing.T) {
	conn := &mockTxConn{}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	conn.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
		if strings.HasPrefix(sql, "SAVEPOINT ") {
			if err := database.Commit(txCtx); err != nil {
				t.Errorf("Commit(root) from inside the savepoint statement error = %v", err)
			}
		}
		return &mockResult{}, nil
	}

	if _, err := tx.Begin(txCtx, "sp"); err == nil {
		t.Error("Begin() error = nil after the transaction was torn down mid-statement, want a closed error")
	}
	if !tx.IsClosed() {
		t.Error("IsClosed() = false, want true — the test did not reproduce the race")
	}
}

// TestRootTeardownIsClaimedOnce pins what replaced the mutex as the thing that
// serializes root teardown. Only one of two racing teardowns may reach the
// driver: two COMMITs on one connection would fire the OnCommit callbacks twice
// and race the driver's own closed flag.
func TestRootTeardownIsClaimedOnce(t *testing.T) {
	tests := []struct {
		name   string
		second func(ctx context.Context) error
	}{
		{name: "commit vs commit", second: database.Commit},
		{name: "commit vs rollback", second: database.Rollback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var teardowns, fired atomic.Int64
			release := make(chan struct{})
			entered := make(chan struct{})
			// A regression sends a second Commit to the driver, so the signal
			// has to survive being raised twice — closing it unconditionally
			// would panic the binary instead of reporting teardowns = 2.
			signalEntered := sync.OnceFunc(func() { close(entered) })

			conn := &mockTxConn{}
			conn.commitFn = func(context.Context) error {
				if teardowns.Add(1) > 1 {
					// A regression put a second COMMIT on the wire. Return at
					// once rather than parking here: blocking would let
					// runWithin time out and report a held mutex, which is the
					// wrong diagnosis, and would strand this goroutine because
					// nothing reaches close(release).
					return nil
				}
				signalEntered()
				<-release
				return nil
			}
			conn.rollbackFn = func(context.Context) error {
				teardowns.Add(1)
				return nil
			}
			q := &mockQuerier{conn: conn, mode: database.CallbackSync}

			txCtx, err := database.NewTransaction(context.Background(), q, "root")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			database.FromContext(txCtx).OnCommit(func(context.Context) error {
				fired.Add(1)
				return nil
			})

			firstErr := make(chan error, 1)
			go func() { firstErr <- database.Commit(txCtx) }()

			<-entered // the first teardown is mid-round-trip
			secondErrCh := make(chan error, 1)
			runWithin(t, "the second teardown", func() {
				secondErrCh <- tt.second(txCtx)
			})
			secondErr := <-secondErrCh
			close(release)

			if err := <-firstErr; err != nil {
				t.Errorf("first Commit() error = %v, want nil", err)
			}
			if secondErr == nil {
				t.Error("second teardown error = nil, want a rejection — it must not reach the driver")
			}
			if got := teardowns.Load(); got != 1 {
				t.Errorf("driver teardowns = %d, want 1", got)
			}
			if got := fired.Load(); got != 1 {
				t.Errorf("OnCommit callbacks fired = %d, want 1", got)
			}
		})
	}
}

// TestFailedRootTeardownReopensTransaction covers the other half of the claim:
// a teardown the driver rejected releases it again, so the Tx does not itself
// reject the caller's next attempt.
//
// What it asserts is that the rollback reaches the driver, not that the driver
// accepts it — both shipped adapters mark their own transaction done before
// calling out (database/sql sets tx.done, pgx returns ErrTxClosed), so whether
// a real COMMIT can be followed by a real ROLLBACK is the adapter's business.
// The property that belongs to Tx is that the claim is not still held.
func TestFailedRootTeardownReopensTransaction(t *testing.T) {
	wantErr := errors.New("connection reset")

	var rollbacks atomic.Int64
	conn := &mockTxConn{}
	conn.commitFn = func(context.Context) error { return wantErr }
	conn.rollbackFn = func(context.Context) error {
		rollbacks.Add(1)
		return nil
	}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	if err := database.Commit(txCtx); !errors.Is(err, wantErr) {
		t.Fatalf("Commit() error = %v, want it to wrap %v", err, wantErr)
	}
	if database.FromContext(txCtx).IsClosed() {
		t.Error("IsClosed() = true after a failed COMMIT, want false — the claim was not released")
	}

	err = database.Rollback(txCtx)
	if got := rollbacks.Load(); got != 1 {
		t.Errorf("driver rollbacks after a failed COMMIT = %d, want 1: Tx rejected the teardown itself (%v)", got, err)
	}
}

// TestSavepointUnwindSurvivesAnEmptiedStack gates the bounds guard the untangle
// made necessary. With tx.mu held for the whole body, tx.depth could not change
// between the savepoint statement and the bookkeeping that follows it; released
// across the statement they are two steps, and the concurrent savepoint scopes
// PRD §18.5 carves out as unsupported can empty the stack in between — where
// tx.callbacks[tx.depth-1] indexes -1 and panics in the consumer's process.
//
// §18.5 declines to *detect* concurrent scopes, so the guard restores the
// pre-change failure mode (wrong-but-safe bookkeeping) and nothing more: the
// statement has already run, so the caller still sees nil, and the root
// transaction is left usable.
//
// The reservation does not close the window, and this is where that is checked.
// depth is read to choose the acquire mode — root teardowns do not block,
// savepoint statements do — so it is read *before* there is anything to hold,
// and a goroutine that already holds the reservation can pop the frame while
// this one waits for it. What the reservation did remove is the single-
// goroutine spelling an earlier version of this test used: re-entering a
// savepoint teardown from inside another one's statement now waits for a
// reservation its own caller holds. So the interleaving is driven from two
// goroutines, with synctest.Wait standing in for the observation "the second
// one is parked on the reservation".
func TestSavepointUnwindSurvivesAnEmptiedStack(t *testing.T) {
	tests := []struct {
		name string
		// pop runs first, empties the stack, and parks inside its statement
		// until the waiter is in place.
		pop func(ctx context.Context) error
		// popStmt is the statement pop issues; it is the one that blocks.
		popStmt string
		// unwind reads depth while pop still holds the frame, then waits for
		// the reservation and finds the stack emptied under it. It is the other
		// verb, so its own statement does not match popStmt.
		unwind func(ctx context.Context) error
		// unwindStmt is the statement unwind issues once it is let through.
		unwindStmt string
	}{
		{
			name: "release savepoint against an emptied stack",
			pop:  database.Rollback, popStmt: "ROLLBACK TO SAVEPOINT",
			unwind: database.Commit, unwindStmt: "RELEASE SAVEPOINT",
		},
		{
			name: "rollback to savepoint against an emptied stack",
			pop:  database.Commit, popStmt: "RELEASE SAVEPOINT",
			unwind: database.Rollback, unwindStmt: "ROLLBACK TO SAVEPOINT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn := &mockTxConn{}
				q := &mockQuerier{conn: conn, mode: database.CallbackSync}

				hold := make(chan struct{})
				conn.execFn = func(_ context.Context, sql string, _ ...any) (database.Result, error) {
					if strings.HasPrefix(sql, tt.popStmt) {
						<-hold
					}
					return &mockResult{}, nil
				}

				txCtx, err := database.NewTransaction(context.Background(), q, "root")
				if err != nil {
					t.Fatalf("NewTransaction() error = %v", err)
				}
				tx := database.FromContext(txCtx)
				if _, err := tx.Begin(txCtx, "sp"); err != nil {
					t.Fatalf("Begin() error = %v", err)
				}

				popped := make(chan error, 1)
				go func() { popped <- tt.pop(txCtx) }()
				synctest.Wait() // pop holds the reservation, parked in its statement

				unwound := make(chan error, 1)
				go func() { unwound <- tt.unwind(txCtx) }()
				synctest.Wait() // unwind has read depth == 1 and is waiting for the reservation

				close(hold) // pop finishes, empties the stack, releases
				if err := <-popped; err != nil {
					t.Errorf("%s error = %v, want nil", tt.popStmt, err)
				}
				if err := <-unwound; err != nil {
					t.Errorf("%s against an emptied stack = %v, want nil: the statement already ran", tt.unwindStmt, err)
				}
				if tx.IsClosed() {
					t.Error("IsClosed() = true, want false — only the savepoint frame was unwound")
				}

				// The root must still be usable: a corrupted stack shows up here.
				if err := database.Commit(txCtx); err != nil {
					t.Errorf("Commit(root) after the unwind = %v, want nil", err)
				}
			})
		})
	}
}

// --- PRD §18.5: the connection reservation ---

// rowsOf returns a result set that yields n rows and then reports exhaustion.
func rowsOf(n int) *mockRows {
	left := n
	r := &mockRows{}
	r.nextFn = func() bool {
		if left == 0 {
			return false
		}
		left--
		return true
	}
	return r
}

// newHeldTx opens a root transaction whose connection is pinned by a result set
// that is neither drained nor closed — the leak PRD §18.5 says turns the next
// statement into a wait. It returns the transaction context and the leaked set.
func newHeldTx(t *testing.T, conn *mockTxConn) (context.Context, database.Rows) {
	t.Helper()
	conn.queryFn = func(context.Context, string, ...any) (database.Rows, error) {
		return rowsOf(2), nil
	}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}
	txCtx, err := database.NewTransaction(context.Background(), q, "held")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	rows, err := database.FromContext(txCtx).Query(txCtx, "SELECT 1")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	return txCtx, rows
}

// expiredCtx returns a context derived from parent that is already done, which
// is what makes the acquisition paths below deterministic: acquireConn's second
// select has exactly one ready case whichever way the connection is held.
func expiredCtx(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx
}

// TestQueryHoldsTheReservationUntilDrainedOrClosed pins the release points PRD
// §18.5 specifies for Query: the connection is held from entry until the result
// set is drained or closed, whichever comes first, and a caller who does both
// releases once.
//
// Releasing twice would not corrupt anything visible — it would park the
// releasing goroutine on an empty channel forever — so "exactly once" is
// asserted by finishing the result set inside a bound and then issuing another
// statement.
func TestQueryHoldsTheReservationUntilDrainedOrClosed(t *testing.T) {
	tests := []struct {
		name string
		// finish consumes the result set the way the case is named.
		finish func(t *testing.T, rows database.Rows)
	}{
		{
			name: "drained",
			finish: func(t *testing.T, rows database.Rows) {
				t.Helper()
				for rows.Next() {
				}
			},
		},
		{
			name: "closed without draining",
			finish: func(t *testing.T, rows database.Rows) {
				t.Helper()
				if err := rows.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			},
		},
		{
			name: "drained and then closed",
			finish: func(t *testing.T, rows database.Rows) {
				t.Helper()
				for rows.Next() {
				}
				if err := rows.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			},
		},
		{
			name: "closed twice",
			finish: func(t *testing.T, rows database.Rows) {
				t.Helper()
				for range 2 {
					if err := rows.Close(); err != nil {
						t.Errorf("Close() error = %v", err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{}
			txCtx, rows := newHeldTx(t, conn)
			tx := database.FromContext(txCtx)

			// Held: the acquisition falls through to the ctx arm, which is
			// already done, so the wait ends at once and names itself.
			_, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 1")
			if err == nil {
				t.Fatal("Exec() error = nil while a result set held the connection, want the wait to fail")
			}
			if !strings.Contains(err.Error(), "waiting for the connection held by an open result set") {
				t.Errorf("Exec() error = %v, want it to name the open result set", err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Exec() error = %v, want it to wrap the context error", err)
			}

			runWithin(t, "finishing the result set", func() { tt.finish(t, rows) })

			// Released. The same expired context must now succeed: an
			// uncontended acquisition always wins, which is why acquireConn
			// tries the non-blocking case before selecting on ctx.Done().
			if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 2"); err != nil {
				t.Errorf("Exec() after the result set was finished = %v, want nil", err)
			}
		})
	}
}

// TestQueryReleasesWhenTheDriverRejectsIt covers the one Query path with no
// result set to carry the reservation.
func TestQueryReleasesWhenTheDriverRejectsIt(t *testing.T) {
	wantErr := errors.New("syntax error")
	conn := &mockTxConn{}
	conn.queryFn = func(context.Context, string, ...any) (database.Rows, error) {
		return nil, wantErr
	}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "rejected")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	if _, err := tx.Query(txCtx, "SELECT 1"); !errors.Is(err, wantErr) {
		t.Fatalf("Query() error = %v, want it to wrap %v", err, wantErr)
	}
	if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 1"); err != nil {
		t.Errorf("Exec() after a rejected Query = %v, want nil — the reservation was not given back", err)
	}
}

// TestQueryRowHoldsTheReservationUntilScan pins the QueryRow window: both
// adapters execute the statement eagerly at QueryRow and consume the row in
// Scan, so the reservation spans both (PRD §18.5).
func TestQueryRowHoldsTheReservationUntilScan(t *testing.T) {
	conn := &mockTxConn{}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "queryrow")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	row := tx.QueryRow(txCtx, "SELECT 1")
	if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 1"); err == nil {
		t.Error("Exec() error = nil while an unscanned row held the connection")
	}

	if err := row.Scan(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	// Scanning again must not park on an empty channel: the release is a
	// sync.OnceFunc.
	runWithin(t, "a second Scan", func() { _ = row.Scan() })

	if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 2"); err != nil {
		t.Errorf("Exec() after Scan = %v, want nil", err)
	}
}

// TestQueryRowReportsAnAcquisitionFailureFromScan covers the shape QueryRow has
// to use because it has no error return: the failure comes back as a Row whose
// Scan reports it.
func TestQueryRowReportsAnAcquisitionFailureFromScan(t *testing.T) {
	conn := &mockTxConn{}
	txCtx, _ := newHeldTx(t, conn)
	tx := database.FromContext(txCtx)

	err := tx.QueryRow(expiredCtx(txCtx), "SELECT 1").Scan()
	if err == nil {
		t.Fatal("Scan() error = nil, want the acquisition failure")
	}
	if !strings.Contains(err.Error(), "waiting for the connection held by an open result set") {
		t.Errorf("Scan() error = %v, want it to name the open result set", err)
	}
}

// TestQueryRowFuncReleasesWhetherOrNotFnScans is PRD §18.5's reason for
// pointing consumers at QueryRowFunc as the leak-proof spelling of
// Querier(ctx). An fn that returns early without scanning would otherwise leave
// the reservation held for the life of the transaction.
func TestQueryRowFuncReleasesWhetherOrNotFnScans(t *testing.T) {
	guardErr := errors.New("guard rejected the row")

	tests := []struct {
		name    string
		fn      func(database.Row) error
		wantErr error
	}{
		{
			name:    "fn returns without scanning",
			fn:      func(database.Row) error { return guardErr },
			wantErr: guardErr,
		},
		{
			name: "fn scans",
			fn:   func(r database.Row) error { return r.Scan() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{}
			q := &mockQuerier{conn: conn, mode: database.CallbackSync}

			txCtx, err := database.NewTransaction(context.Background(), q, "rowfunc")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			runWithin(t, "QueryRowFunc", func() {
				if err := database.QueryRowFunc(txCtx, tx, "SELECT 1", nil, tt.fn); !errors.Is(err, tt.wantErr) {
					t.Errorf("QueryRowFunc() error = %v, want %v", err, tt.wantErr)
				}
			})

			if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 1"); err != nil {
				t.Errorf("Exec() after QueryRowFunc = %v, want nil — the reservation was not given back", err)
			}
		})
	}
}

// TestRootTeardownProceedsWithoutTheReservation is the regression gate for the
// hang PRD §18.5 exists to avoid. A result set left open pins the connection;
// blocking the teardown on it would strand that pooled connection for the life
// of the process, which is strictly worse than the driver's loud complaint.
func TestRootTeardownProceedsWithoutTheReservation(t *testing.T) {
	tests := []struct {
		name     string
		teardown func(ctx context.Context) error
	}{
		{name: "commit", teardown: database.Commit},
		{name: "rollback", teardown: database.Rollback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{}
			txCtx, _ := newHeldTx(t, conn) // the result set is never finished
			runWithin(t, "a root teardown against a pinned connection", func() {
				if err := tt.teardown(txCtx); err != nil {
					t.Errorf("%s() error = %v, want nil — the driver accepted it", tt.name, err)
				}
			})
			if !database.FromContext(txCtx).IsClosed() {
				t.Error("IsClosed() = false after the teardown landed, want true")
			}
		})
	}
}

// TestSavepointStatementsWaitForTheReservation is the other half of the depth
// rule PRD §18.5 records: only a root teardown proceeds without the
// reservation. A savepoint statement runs while the enclosing fn is still live
// — every generated …WithRelated mutation opens one — so it waits for the
// in-flight read rather than failing with a busy connection.
func TestSavepointStatementsWaitForTheReservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &mockTxConn{}
		txCtx, rows := newHeldTx(t, conn)
		tx := database.FromContext(txCtx)

		began := make(chan error, 1)
		go func() {
			_, err := tx.Begin(txCtx, "sp")
			began <- err
		}()
		synctest.Wait()

		select {
		case err := <-began:
			t.Fatalf("Begin() returned %v while a result set held the connection, want it to wait", err)
		default:
		}
		if got := conn.getExecCalls(); len(got) != 0 {
			t.Errorf("driver exec calls while the connection was held = %v, want none", got)
		}

		for rows.Next() { //nolint:revive // draining is the point; the body is empty by design
		}
		synctest.Wait()

		if err := <-began; err != nil {
			t.Errorf("Begin() after the result set drained = %v, want nil", err)
		}
		if got := conn.getExecCalls(); len(got) != 1 || !strings.HasPrefix(got[0], "SAVEPOINT ") {
			t.Errorf("driver exec calls = %v, want one SAVEPOINT", got)
		}
	})
}

// TestConcurrentBeginKeepsTheStackInStatementOrder is the acceptance criterion
// for holding the reservation across the savepoint *bookkeeping* rather than
// only across the statement. Released in between, two goroutines could exec
// their SAVEPOINTs in one order and push their names in the other, and Tx's
// stack would then disagree with the server's.
//
// The disagreement is asserted through the driver, which is where it would do
// damage: unwinding the frames one at a time must RELEASE them in exactly the
// reverse of the order the server saw them created.
func TestConcurrentBeginKeepsTheStackInStatementOrder(t *testing.T) {
	const scopes = 16

	conn := &mockTxConn{}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	var wg sync.WaitGroup
	for i := range scopes {
		wg.Go(func() {
			if _, err := tx.Begin(txCtx, fmt.Sprintf("sp%d", i)); err != nil {
				t.Errorf("Begin(sp%d) error = %v", i, err)
			}
		})
	}
	wg.Wait()

	for i := range scopes {
		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit(savepoint %d) error = %v", i, err)
		}
	}

	var created, released []string
	for _, stmt := range conn.getExecCalls() {
		switch {
		case strings.HasPrefix(stmt, "SAVEPOINT "):
			created = append(created, strings.TrimPrefix(stmt, "SAVEPOINT "))
		case strings.HasPrefix(stmt, "RELEASE SAVEPOINT "):
			released = append(released, strings.TrimPrefix(stmt, "RELEASE SAVEPOINT "))
		}
	}

	want := slices.Clone(created)
	slices.Reverse(want)
	if diff := cmp.Diff(want, released); diff != "" {
		t.Errorf("RELEASE order does not invert the SAVEPOINT order (-want +got):\n%s", diff)
	}
}

// TestIsClosedDuringRootTeardown is the decision PRD §18.5 and §18.6 record for
// the window between a root teardown's claim and the driver's answer.
//
// closed means the teardown *completed* — §18.4's own wording — and tearingDown
// is what makes the teardown at-most-once. Reporting closed early would be
// cheaper, but it lies to the four surfaces that read IsClosed, and the one
// that matters is §18.6's: a hook that sees IsClosed() == true fires its side
// effect immediately, for a transaction that has not committed yet and, on a
// teardown the driver rejects, never will. That is §18.6's critical invariant.
//
// The other half of the decision is that the callbacks are snapshotted *after*
// the driver call, so a callback registered inside the window still belongs to
// this commit rather than being appended to a slice nobody will read.
func TestIsClosedDuringRootTeardown(t *testing.T) {
	var fired atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})

	conn := &mockTxConn{}
	conn.commitFn = func(context.Context) error {
		close(entered)
		<-release
		return nil
	}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "teardown")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	committed := make(chan error, 1)
	go func() { committed <- database.Commit(txCtx) }()
	<-entered // the COMMIT is on the wire and has not been answered

	if tx.IsClosed() {
		t.Error("IsClosed() = true during a root COMMIT still in flight, want false (PRD §18.4)")
	}
	if !database.InTransaction(txCtx) {
		t.Error("InTransaction() = false during a root COMMIT still in flight, want true")
	}
	if database.Conn(txCtx, q) != database.Querier(tx) {
		t.Error("Conn() fell back to the pool during a root COMMIT still in flight, want the transaction")
	}

	// §18.6's hook shape takes the defer branch here, so the callback it
	// registers must still fire with this commit.
	tx.OnCommit(func(context.Context) error {
		fired.Add(1)
		return nil
	})

	// A statement, on the other hand, cannot succeed whichever way the teardown
	// lands, so it is rejected rather than sent.
	if _, err := tx.Exec(txCtx, "UPDATE t SET c = 1"); err == nil {
		t.Error("Exec() error = nil during a root teardown, want a rejection")
	}
	if got := conn.getExecCalls(); len(got) != 0 {
		t.Errorf("driver exec calls during a root teardown = %v, want none", got)
	}

	close(release)
	if err := <-committed; err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if !tx.IsClosed() {
		t.Error("IsClosed() = false after the COMMIT landed, want true")
	}
	if got := fired.Load(); got != 1 {
		t.Errorf("callbacks registered during the teardown window that fired = %d, want 1", got)
	}
}
