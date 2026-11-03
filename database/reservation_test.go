package database_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/database"
)

func TestExecHoldsTheReservationAcrossTheDriverCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		conn := &mockTxConn{execFn: func(_ context.Context, sql string, _ ...any) (database.Result, error) {
			if sql == "UPDATE first" {
				<-release
			}
			return &mockResult{}, nil
		}}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		tx := database.FromContext(txCtx)

		first := make(chan error, 1)
		go func() { _, err := tx.Exec(txCtx, "UPDATE first"); first <- err }()
		synctest.Wait()

		second := make(chan error, 1)
		go func() { _, err := tx.Exec(txCtx, "UPDATE second"); second <- err }()
		synctest.Wait()

		select {
		case err := <-second:
			t.Errorf("second Exec() returned %v while the first was still in the driver, want it to wait", err)
			close(release)
			return
		default:
		}
		if diff := cmp.Diff([]string{"UPDATE first"}, conn.getExecCalls()); diff != "" {
			t.Errorf("driver statements while the first Exec was in flight (-want +got):\n%s", diff)
		}

		close(release)
		if err := <-first; err != nil {
			t.Errorf("first Exec() error = %v", err)
		}
		if err := <-second; err != nil {
			t.Errorf("second Exec() error = %v", err)
		}
		if diff := cmp.Diff([]string{"UPDATE first", "UPDATE second"}, conn.getExecCalls()); diff != "" {
			t.Errorf("driver statements (-want +got):\n%s", diff)
		}
	})
}

func TestSavepointUnwindWaitsForALeakedResultSet(t *testing.T) {
	tests := []struct {
		name     string
		unwind   func(ctx context.Context) error
		wantStmt string
	}{
		{name: "release savepoint", unwind: database.Commit, wantStmt: "RELEASE SAVEPOINT sp"},
		{name: "rollback to savepoint", unwind: database.Rollback, wantStmt: "ROLLBACK TO SAVEPOINT sp"},
	}

	// leakInSavepoint opens a savepoint and leaves a result set open inside it.
	leakInSavepoint := func(t *testing.T) (context.Context, *mockTxConn, database.Rows) {
		t.Helper()
		conn := &mockTxConn{queryFn: func(context.Context, string, ...any) (database.Rows, error) {
			return rowsOf(2), nil
		}}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction(root) error = %v", err)
		}
		spCtx, err := database.NewTransaction(txCtx, q, "sp")
		if err != nil {
			t.Fatalf("NewTransaction(sp) error = %v", err)
		}
		rows, err := database.FromContext(spCtx).Query(spCtx, "SELECT 1")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		return spCtx, conn, rows
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("waits until the result set is drained", func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					spCtx, conn, rows := leakInSavepoint(t)

					done := make(chan error, 1)
					go func() { done <- tt.unwind(spCtx) }()
					synctest.Wait()

					select {
					case err := <-done:
						t.Fatalf("unwind returned %v while a result set held the connection, want it to wait", err)
					default:
					}
					if diff := cmp.Diff([]string{"SAVEPOINT sp"}, conn.getExecCalls()); diff != "" {
						t.Errorf("driver statements while the connection was held (-want +got):\n%s", diff)
					}

					for rows.Next() { //nolint:revive // draining is the point; the body is empty by design
					}
					if err := <-done; err != nil {
						t.Errorf("unwind after the result set drained = %v, want nil", err)
					}
					if diff := cmp.Diff([]string{"SAVEPOINT sp", tt.wantStmt}, conn.getExecCalls()); diff != "" {
						t.Errorf("driver statements (-want +got):\n%s", diff)
					}
				})
			})

			t.Run("fails when the context expires without reaching the driver", func(t *testing.T) {
				spCtx, conn, rows := leakInSavepoint(t)

				err := tt.unwind(expiredCtx(spCtx))
				if !errors.Is(err, context.Canceled) {
					t.Errorf("unwind error = %v, want it to wrap the context error", err)
				}
				if diff := cmp.Diff([]string{"SAVEPOINT sp"}, conn.getExecCalls()); diff != "" {
					t.Errorf("driver statements (-want +got):\n%s", diff)
				}

				// The savepoint is still open: once the set is closed the
				// same unwind goes through.
				if err := rows.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
				if err := tt.unwind(spCtx); err != nil {
					t.Errorf("unwind after the result set closed = %v, want nil", err)
				}
				if diff := cmp.Diff([]string{"SAVEPOINT sp", tt.wantStmt}, conn.getExecCalls()); diff != "" {
					t.Errorf("driver statements (-want +got):\n%s", diff)
				}
			})
		})
	}
}

func TestQueryFuncReleasesTheReservationInsideATransaction(t *testing.T) {
	fnErr := errors.New("fn stopped early")

	tests := []struct {
		name    string
		fn      func(database.Rows) error
		wantErr error
	}{
		{name: "fn returns without draining", fn: func(database.Rows) error { return nil }},
		{name: "fn returns an error without draining", fn: func(database.Rows) error { return fnErr }, wantErr: fnErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{queryFn: func(context.Context, string, ...any) (database.Rows, error) {
				return rowsOf(2), nil
			}}
			q := &mockQuerier{conn: conn, mode: database.CallbackSync}
			txCtx, err := database.NewTransaction(context.Background(), q, "root")
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			if err := database.QueryFunc(txCtx, tx, "SELECT 1", nil, tt.fn); !errors.Is(err, tt.wantErr) {
				t.Errorf("QueryFunc() error = %v, want %v", err, tt.wantErr)
			}

			// An expired context only fails if the reservation is still held.
			if _, err := tx.Exec(expiredCtx(txCtx), "UPDATE t SET c = 1"); err != nil {
				t.Errorf("Exec() after QueryFunc = %v, want nil: the reservation was not given back", err)
			}
		})
	}
}
