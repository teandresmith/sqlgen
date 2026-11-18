package database_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/teandresmith/sqlgen/database"
)

func TestStatementsDuringRootTeardownAreRejected(t *testing.T) {
	teardowns := []struct {
		name string
		// park makes conn's root teardown of this kind block on release after
		// closing entered.
		park     func(conn *mockTxConn, entered, release chan struct{})
		teardown func(ctx context.Context) error
	}{
		{
			name: "commit",
			park: func(conn *mockTxConn, entered, release chan struct{}) {
				conn.commitFn = func(context.Context) error { close(entered); <-release; return nil }
			},
			teardown: database.Commit,
		},
		{
			name: "rollback",
			park: func(conn *mockTxConn, entered, release chan struct{}) {
				conn.rollbackFn = func(context.Context) error { close(entered); <-release; return nil }
			},
			teardown: database.Rollback,
		},
	}

	statements := []struct {
		name string
		run  func(txCtx context.Context, q database.Querier) error
	}{
		{
			name: "exec",
			run: func(txCtx context.Context, _ database.Querier) error {
				_, err := database.FromContext(txCtx).Exec(txCtx, "UPDATE t SET c = 1")
				return err
			},
		},
		{
			name: "query",
			run: func(txCtx context.Context, _ database.Querier) error {
				_, err := database.FromContext(txCtx).Query(txCtx, "SELECT 1")
				return err
			},
		},
		{
			name: "query row",
			run: func(txCtx context.Context, _ database.Querier) error {
				return database.FromContext(txCtx).QueryRow(txCtx, "SELECT 1").Scan()
			},
		},
		{
			name: "begin",
			run: func(txCtx context.Context, _ database.Querier) error {
				_, err := database.FromContext(txCtx).Begin(txCtx, "sp")
				return err
			},
		},
		{
			name: "new transaction",
			run: func(txCtx context.Context, q database.Querier) error {
				_, err := database.NewTransaction(txCtx, q, "sp")
				return err
			},
		},
	}

	for _, td := range teardowns {
		t.Run(td.name, func(t *testing.T) {
			for _, st := range statements {
				t.Run(st.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						var driverReads atomic.Int64
						conn := &mockTxConn{
							queryFn: func(context.Context, string, ...any) (database.Rows, error) {
								driverReads.Add(1)
								return &mockRows{}, nil
							},
							queryRowFn: func(context.Context, string, ...any) database.Row {
								driverReads.Add(1)
								return &mockRow{}
							},
						}
						entered, release := make(chan struct{}), make(chan struct{})
						td.park(conn, entered, release)
						finish := sync.OnceFunc(func() { close(release) })
						q := newBeginRecorder(conn)

						txCtx, err := database.NewTransaction(context.Background(), q, "root")
						if err != nil {
							t.Fatalf("NewTransaction() error = %v", err)
						}

						finished := make(chan error, 1)
						go func() { finished <- td.teardown(txCtx) }()
						<-entered // the teardown is on the wire and not yet answered

						// A statement that is not rejected waits for the
						// reservation the teardown holds, so it runs on its own
						// goroutine and the bubble reports whether it returned.
						stmt := make(chan error, 1)
						go func() { stmt <- st.run(txCtx, q) }()
						synctest.Wait()
						select {
						case err = <-stmt:
						default:
							t.Errorf("%s during the teardown is waiting, want an immediate rejection", st.name)
							finish()
							err = <-stmt
						}
						finish()
						if tdErr := <-finished; tdErr != nil {
							t.Errorf("%s error = %v", td.name, tdErr)
						}

						if err == nil {
							t.Fatalf("%s during the teardown error = nil, want a rejection", st.name)
						}
						if !strings.Contains(err.Error(), "torn down") {
							t.Errorf("%s error = %q, want it to name the teardown", st.name, err)
						}
						if got := conn.getExecCalls(); len(got) != 0 {
							t.Errorf("driver exec calls = %v, want none", got)
						}
						if got := driverReads.Load(); got != 0 {
							t.Errorf("driver query calls = %d, want none", got)
						}
						if got := q.beginCount(); got != 1 {
							t.Errorf("Querier.Begin calls = %d, want 1: no second root may open", got)
						}
					})
				})
			}
		})
	}
}

func TestRejectedRootRollbackLeavesTheTransactionOpen(t *testing.T) {
	rollbackErr := errors.New("connection reset")
	var rollbacks atomic.Int64
	conn := &mockTxConn{rollbackFn: func(context.Context) error {
		if rollbacks.Add(1) == 1 {
			return rollbackErr
		}
		return nil
	}}
	q := &mockQuerier{conn: conn, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(context.Background(), q, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	if err := database.Rollback(txCtx); !errors.Is(err, rollbackErr) {
		t.Fatalf("Rollback() error = %v, want it to wrap %v", err, rollbackErr)
	}
	if tx.IsClosed() {
		t.Error("IsClosed() = true after a rejected ROLLBACK, want false")
	}
	if got := database.Conn(txCtx, q); got != database.Querier(tx) {
		t.Errorf("Conn() = %T after a rejected ROLLBACK, want the transaction", got)
	}

	if err := database.Rollback(txCtx); err != nil {
		t.Errorf("second Rollback() error = %v, want nil", err)
	}
	if got := rollbacks.Load(); got != 2 {
		t.Errorf("driver rollbacks = %d, want 2: the second attempt must reach the driver", got)
	}
	if !tx.IsClosed() {
		t.Error("IsClosed() = false after the driver accepted the ROLLBACK, want true")
	}
}
