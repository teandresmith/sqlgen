package database_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/database"
)

type ctxKey struct{}

func TestSyncCallbacksReceiveTheCallersContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "caller")
	q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackSync}

	txCtx, err := database.NewTransaction(ctx, q, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	var got any
	database.FromContext(txCtx).OnCommit(func(cbCtx context.Context) error {
		got = cbCtx.Value(ctxKey{})
		return nil
	})

	if err := database.Commit(txCtx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got != "caller" {
		t.Errorf("callback ctx.Value(key) = %v, want %q", got, "caller")
	}
}

// The async tests run inside a synctest bubble: synctest.Wait returns once
// every goroutine is durably blocked, so "Commit has returned while the
// callback is still parked" is an observation rather than a race against a
// timer, and a callback that never fires deadlocks the bubble instead of
// hanging the package.

func TestAsyncCommitDoesNotWaitForCallbacks(t *testing.T) {
	tests := []struct {
		name string
		mode database.CallbackMode
	}{
		{name: "CallbackAsync", mode: database.CallbackAsync},
		{name: "empty CallbackMode defaults to async", mode: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				q := &mockQuerier{conn: &mockTxConn{}, mode: tt.mode}
				txCtx, err := database.NewTransaction(context.Background(), q, "root")
				if err != nil {
					t.Fatalf("NewTransaction() error = %v", err)
				}

				unblock := make(chan struct{})
				finished := make(chan struct{})
				database.FromContext(txCtx).OnCommit(func(context.Context) error {
					<-unblock
					close(finished)
					return nil
				})

				committed := make(chan error, 1)
				go func() { committed <- database.Commit(txCtx) }()
				synctest.Wait()

				select {
				case err := <-committed:
					if err != nil {
						t.Errorf("Commit() error = %v", err)
					}
				default:
					t.Error("Commit() has not returned while a callback is still running; the caller must not wait")
				}

				close(unblock)
				<-finished
			})
		})
	}
}

func TestAsyncCallbacksGetADetachedContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "caller"))
		q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackAsync}

		txCtx, err := database.NewTransaction(parent, q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		type seen struct {
			err         error
			value       any
			hasDeadline bool
		}
		got := make(chan seen, 1)
		cancelled := make(chan struct{})
		database.FromContext(txCtx).OnCommit(func(cbCtx context.Context) error {
			<-cancelled
			_, hasDeadline := cbCtx.Deadline()
			got <- seen{err: cbCtx.Err(), value: cbCtx.Value(ctxKey{}), hasDeadline: hasDeadline}
			return nil
		})

		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		// The caller abandons its context as soon as the commit returns, and
		// the callback looks at its own only after that.
		cancel()
		close(cancelled)

		s := <-got
		if s.err != nil {
			t.Errorf("callback ctx.Err() = %v, want nil: cancelling the caller's context must not reach it", s.err)
		}
		if s.value != nil {
			t.Errorf("callback ctx.Value(key) = %v, want nil: the context must not derive from the caller's", s.value)
		}
		if s.hasDeadline {
			t.Error("callback ctx has a deadline, want none")
		}
	})
}

func TestAsyncCallbacksFireInRegistrationOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackAsync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		tx := database.FromContext(txCtx)

		const n = 3
		order := make(chan int, n)
		for i := range n {
			tx.OnCommit(func(context.Context) error { order <- i; return nil })
		}

		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		got := make([]int, 0, n)
		for range n {
			got = append(got, <-order)
		}
		if diff := cmp.Diff([]int{0, 1, 2}, got); diff != "" {
			t.Errorf("async callback order (-want +got):\n%s", diff)
		}
	})
}

// lockedBuffer is a bytes.Buffer safe to write from the callback goroutine and
// read from the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog redirects the standard logger for the length of the test.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(out)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return out
}

// The hook that registers a callback is what knows the table, primary key and
// operation, so it puts them in the callback's error; the runtime's part is to
// log that error once the retry has failed too.
func TestAsyncCallbackFailureIsRetriedOnceThenLogged(t *testing.T) {
	logged := captureLog(t)

	synctest.Test(t, func(t *testing.T) {
		q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackAsync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		tx := database.FromContext(txCtx)

		callbackErr := errors.New("publish orders create pk=7: broker unreachable")
		var attempts atomic.Int32
		tx.OnCommit(func(context.Context) error {
			attempts.Add(1)
			return callbackErr
		})
		nextRan := make(chan struct{})
		tx.OnCommit(func(context.Context) error {
			close(nextRan)
			return nil
		})

		if err := database.Commit(txCtx); err != nil {
			t.Errorf("Commit() error = %v, want nil: async callback failures do not reach the caller", err)
		}
		// The callbacks run in order on one goroutine, so the failing one has
		// been retried and logged by the time the next one runs.
		<-nextRan

		if got := attempts.Load(); got != 2 {
			t.Errorf("failing callback attempts = %d, want 2 (the call and one retry)", got)
		}
		out := logged.String()
		if !strings.Contains(out, callbackErr.Error()) {
			t.Errorf("log output = %q, want it to contain the callback's error %q", out, callbackErr)
		}
		if n := strings.Count(out, callbackErr.Error()); n != 1 {
			t.Errorf("log lines carrying the callback's error = %d (%q), want 1", n, out)
		}
	})
}

func TestSyncCallbacksThroughWithTransaction(t *testing.T) {
	t.Run("WithTransaction returns after every callback has run", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackSync}

			unblock := make(chan struct{})
			var ran atomic.Int32
			returned := make(chan error, 1)
			go func() {
				returned <- database.WithTransaction(context.Background(), q, "root", func(c context.Context) error {
					tx := database.FromContext(c)
					tx.OnCommit(func(context.Context) error { ran.Add(1); return nil })
					tx.OnCommit(func(context.Context) error { <-unblock; ran.Add(1); return nil })
					return nil
				})
			}()
			synctest.Wait()

			select {
			case err := <-returned:
				t.Fatalf("WithTransaction() returned %v while a callback was still running, want it to wait", err)
			default:
			}

			close(unblock)
			if err := <-returned; err != nil {
				t.Fatalf("WithTransaction() error = %v", err)
			}
			if got := ran.Load(); got != 2 {
				t.Errorf("callbacks run by the time WithTransaction returned = %d, want 2", got)
			}
		})
	})

	t.Run("a callback failure is the WithTransaction error and the commit stands", func(t *testing.T) {
		var rollbacks atomic.Int64
		conn := &mockTxConn{rollbackFn: func(context.Context) error { rollbacks.Add(1); return nil }}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}

		callbackErr := errors.New("publish failed")
		var txCtx context.Context
		err := database.WithTransaction(context.Background(), q, "root", func(c context.Context) error {
			txCtx = c
			database.FromContext(c).OnCommit(func(context.Context) error { return callbackErr })
			return nil
		})

		if !errors.Is(err, callbackErr) {
			t.Errorf("WithTransaction() error = %v, want it to wrap the callback's error", err)
		}
		if !database.FromContext(txCtx).IsClosed() {
			t.Error("IsClosed() = false, want true: the COMMIT succeeded before the callback failed")
		}
		if got := rollbacks.Load(); got != 0 {
			t.Errorf("driver rollbacks = %d, want 0: a committed transaction cannot be rolled back", got)
		}
	})
}

func TestWithTransactionCallbacks(t *testing.T) {
	t.Run("fn returning nil fires the callbacks", func(t *testing.T) {
		q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackSync}

		var fired bool
		err := database.WithTransaction(context.Background(), q, "root", func(c context.Context) error {
			database.FromContext(c).OnCommit(func(context.Context) error { fired = true; return nil })
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction() error = %v", err)
		}
		if !fired {
			t.Error("callback did not fire after fn returned nil")
		}
	})

	t.Run("fn returning an error discards the callbacks", func(t *testing.T) {
		var commits atomic.Int64
		conn := &mockTxConn{commitFn: func(context.Context) error { commits.Add(1); return nil }}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}

		fnErr := errors.New("fn failed")
		var fired bool
		err := database.WithTransaction(context.Background(), q, "root", func(c context.Context) error {
			database.FromContext(c).OnCommit(func(context.Context) error { fired = true; return nil })
			return fnErr
		})
		if !errors.Is(err, fnErr) {
			t.Errorf("WithTransaction() error = %v, want fn's error", err)
		}
		if fired {
			t.Error("callback fired although fn returned an error")
		}
		if got := commits.Load(); got != 0 {
			t.Errorf("driver commits = %d, want 0", got)
		}
	})

	t.Run("a failed nested scope rolls back to its savepoint and the outer scope commits", func(t *testing.T) {
		var commits atomic.Int64
		conn := &mockTxConn{commitFn: func(context.Context) error { commits.Add(1); return nil }}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}

		innerErr := errors.New("out of stock")
		var fired []string
		var nestedErr error
		err := database.WithTransaction(context.Background(), q, "checkout", func(txCtx context.Context) error {
			database.FromContext(txCtx).OnCommit(func(context.Context) error { fired = append(fired, "order"); return nil })
			nestedErr = database.WithTransaction(txCtx, q, "items", func(spCtx context.Context) error {
				database.FromContext(spCtx).OnCommit(func(context.Context) error { fired = append(fired, "item"); return nil })
				return innerErr
			})
			return nil
		})
		if err != nil {
			t.Fatalf("outer WithTransaction() error = %v", err)
		}
		if !errors.Is(nestedErr, innerErr) {
			t.Errorf("nested WithTransaction() error = %v, want the inner fn's error", nestedErr)
		}
		if diff := cmp.Diff([]string{"SAVEPOINT items", "ROLLBACK TO SAVEPOINT items"}, conn.getExecCalls()); diff != "" {
			t.Errorf("driver statements (-want +got):\n%s", diff)
		}
		if got := commits.Load(); got != 1 {
			t.Errorf("driver commits = %d, want 1", got)
		}
		if diff := cmp.Diff([]string{"order"}, fired); diff != "" {
			t.Errorf("callbacks fired (-want +got):\n%s", diff)
		}
	})
}

func TestOnCommitOutsideASuccessfulCommitNeverFires(t *testing.T) {
	t.Run("registered after the commit landed", func(t *testing.T) {
		q := &mockQuerier{conn: &mockTxConn{}, mode: database.CallbackSync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		tx := database.FromContext(txCtx)
		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		var fired bool
		tx.OnCommit(func(context.Context) error { fired = true; return nil })
		if err := database.Commit(txCtx); err == nil {
			t.Error("second Commit() error = nil, want the transaction to be closed")
		}
		if fired {
			t.Error("a callback registered on a committed transaction fired")
		}
	})

	t.Run("registered before a rejected COMMIT", func(t *testing.T) {
		commitErr := errors.New("connection reset")
		conn := &mockTxConn{commitFn: func(context.Context) error { return commitErr }}
		q := &mockQuerier{conn: conn, mode: database.CallbackSync}
		txCtx, err := database.NewTransaction(context.Background(), q, "root")
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		var fired bool
		database.FromContext(txCtx).OnCommit(func(context.Context) error { fired = true; return nil })

		if err := database.Commit(txCtx); !errors.Is(err, commitErr) {
			t.Fatalf("Commit() error = %v, want %v", err, commitErr)
		}
		if err := database.Rollback(txCtx); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
		if fired {
			t.Error("a callback fired for a transaction whose COMMIT the driver rejected")
		}
	})
}
