package database_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/database"
)

// beginRecorder is a mockQuerier that records what each Begin call received:
// the options as passed, and the context the root transaction was begun on.
type beginRecorder struct {
	mockQuerier

	mu     sync.Mutex
	opts   [][]database.TxOptions
	ctxs   []context.Context
	begins int
}

func (r *beginRecorder) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	r.mu.Lock()
	r.opts = append(r.opts, slices.Clone(opts))
	r.ctxs = append(r.ctxs, ctx)
	r.begins++
	r.mu.Unlock()
	return database.NewTx(r.conn, name, r.mode), nil
}

func (r *beginRecorder) recordedOpts() [][]database.TxOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.opts)
}

func (r *beginRecorder) beginCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.begins
}

func (r *beginRecorder) beginCtx(t *testing.T) context.Context {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ctxs) != 1 {
		t.Fatalf("Begin calls = %d, want 1", len(r.ctxs))
	}
	return r.ctxs[0]
}

func newBeginRecorder(conn *mockTxConn) *beginRecorder {
	return &beginRecorder{mockQuerier: mockQuerier{conn: conn, mode: database.CallbackSync}}
}

func TestTxOptionConstantValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "Serializable", got: string(database.Serializable), want: "serializable"},
		{name: "RepeatableRead", got: string(database.RepeatableRead), want: "repeatable read"},
		{name: "ReadCommitted", got: string(database.ReadCommitted), want: "read committed"},
		{name: "ReadUncommitted", got: string(database.ReadUncommitted), want: "read uncommitted"},
		{name: "ReadWrite", got: string(database.ReadWrite), want: "read write"},
		{name: "ReadOnly", got: string(database.ReadOnly), want: "read only"},
		{name: "Deferrable", got: string(database.Deferrable), want: "deferrable"},
		{name: "NotDeferrable", got: string(database.NotDeferrable), want: "not deferrable"},
		{name: "CallbackAsync", got: string(database.CallbackAsync), want: "async"},
		{name: "CallbackSync", got: string(database.CallbackSync), want: "sync"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// everyOption sets every TxOptions field to a non-default value, so a field
// dropped on the way to the driver shows up in the diff.
var everyOption = database.TxOptions{
	IsoLevel:       database.RepeatableRead,
	AccessMode:     database.ReadOnly,
	DeferrableMode: database.Deferrable,
	Timeout:        time.Hour,
	CallbackMode:   database.CallbackSync,
}

func TestRootBeginReceivesTheTxOptions(t *testing.T) {
	tests := []struct {
		name  string
		begin func(ctx context.Context, q database.Querier, opts ...database.TxOptions) error
		opts  []database.TxOptions
	}{
		{
			name: "NewTransaction without TxOptions",
			begin: func(ctx context.Context, q database.Querier, opts ...database.TxOptions) error {
				_, err := database.NewTransaction(ctx, q, "root", opts...)
				return err
			},
		},
		{
			name: "NewTransaction with TxOptions",
			begin: func(ctx context.Context, q database.Querier, opts ...database.TxOptions) error {
				_, err := database.NewTransaction(ctx, q, "root", opts...)
				return err
			},
			opts: []database.TxOptions{everyOption},
		},
		{
			name: "WithTransaction with TxOptions",
			begin: func(ctx context.Context, q database.Querier, opts ...database.TxOptions) error {
				return database.WithTransaction(ctx, q, "root", func(context.Context) error { return nil }, opts...)
			},
			opts: []database.TxOptions{everyOption},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newBeginRecorder(&mockTxConn{})
			if err := tt.begin(context.Background(), q, tt.opts...); err != nil {
				t.Fatalf("begin error = %v", err)
			}
			want := [][]database.TxOptions{tt.opts}
			if diff := cmp.Diff(want, q.recordedOpts(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("TxOptions reaching Querier.Begin (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSavepointIgnoresTxOptions(t *testing.T) {
	tests := []struct {
		name string
		// nest opens a savepoint on txCtx with everyOption and closes it again.
		nest      func(t *testing.T, txCtx context.Context, q database.Querier) error
		wantExecs []string
	}{
		{
			name: "NewTransaction",
			nest: func(_ *testing.T, txCtx context.Context, q database.Querier) error {
				_, err := database.NewTransaction(txCtx, q, "sp", everyOption)
				return err
			},
			wantExecs: []string{"SAVEPOINT sp"},
		},
		{
			name: "WithTransaction",
			nest: func(t *testing.T, txCtx context.Context, q database.Querier) error {
				return database.WithTransaction(txCtx, q, "sp", func(spCtx context.Context) error {
					if deadline, ok := spCtx.Deadline(); ok {
						t.Errorf("savepoint context has deadline %v, want none: its Timeout must be ignored", deadline)
					}
					return nil
				}, everyOption)
			},
			wantExecs: []string{"SAVEPOINT sp", "RELEASE SAVEPOINT sp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockTxConn{}
			q := newBeginRecorder(conn)
			txCtx, err := database.NewTransaction(context.Background(), q, "root")
			if err != nil {
				t.Fatalf("NewTransaction(root) error = %v", err)
			}

			if err := tt.nest(t, txCtx, q); err != nil {
				t.Fatalf("nested call error = %v", err)
			}

			if got := q.beginCount(); got != 1 {
				t.Errorf("Querier.Begin calls = %d, want 1 (the root only)", got)
			}
			if diff := cmp.Diff(tt.wantExecs, conn.getExecCalls()); diff != "" {
				t.Errorf("driver statements (-want +got):\n%s", diff)
			}
		})
	}
}

// requireDeadlineNear fails unless ctx carries a deadline in [from+d, to+d],
// the window a context.WithTimeout(d) created between from and to must land
// in, where d is the one-hour Timeout these tests set.
func requireDeadlineNear(ctx context.Context, t *testing.T, what string, from, to time.Time) {
	t.Helper()
	const d = time.Hour
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Errorf("%s has no deadline, want one %s out", what, d)
		return
	}
	if deadline.Before(from.Add(d)) || deadline.After(to.Add(d)) {
		t.Errorf("%s deadline = %v, want between %v and %v", what, deadline, from.Add(d), to.Add(d))
	}
}

func TestTxOptionsTimeout(t *testing.T) {
	t.Run("BEGIN, COMMIT and the transaction context carry the deadline", func(t *testing.T) {
		var commitCtx context.Context
		conn := &mockTxConn{commitFn: func(ctx context.Context) error { commitCtx = ctx; return nil }}
		q := newBeginRecorder(conn)

		from := time.Now()
		txCtx, err := database.NewTransaction(context.Background(), q, "root", database.TxOptions{Timeout: time.Hour})
		to := time.Now()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		requireDeadlineNear(q.beginCtx(t), t, "the context Begin received", from, to)
		requireDeadlineNear(txCtx, t, "the transaction context", from, to)
		requireDeadlineNear(commitCtx, t, "the context COMMIT received", from, to)
	})

	t.Run("ROLLBACK carries the deadline", func(t *testing.T) {
		var rollbackCtx context.Context
		conn := &mockTxConn{rollbackFn: func(ctx context.Context) error { rollbackCtx = ctx; return nil }}
		q := newBeginRecorder(conn)

		from := time.Now()
		err := database.WithTransaction(context.Background(), q, "root", func(context.Context) error {
			return errors.New("fn failed")
		}, database.TxOptions{Timeout: time.Hour})
		to := time.Now()
		if err == nil {
			t.Fatal("WithTransaction() error = nil, want fn's error")
		}

		requireDeadlineNear(rollbackCtx, t, "the context ROLLBACK received", from, to)
	})

	t.Run("a zero Timeout sets no deadline", func(t *testing.T) {
		var commitCtx context.Context
		conn := &mockTxConn{commitFn: func(ctx context.Context) error { commitCtx = ctx; return nil }}
		q := newBeginRecorder(conn)

		txCtx, err := database.NewTransaction(context.Background(), q, "root", database.TxOptions{})
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		for _, c := range []struct {
			what string
			ctx  context.Context
		}{
			{"the context Begin received", q.beginCtx(t)},
			{"the transaction context", txCtx},
			{"the context COMMIT received", commitCtx},
		} {
			if deadline, ok := c.ctx.Deadline(); ok {
				t.Errorf("%s has deadline %v, want none", c.what, deadline)
			}
		}
	})

	t.Run("expiry fails the in-flight statement and WithTransaction rolls back", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var rollbacks int
			conn := &mockTxConn{
				// A driver statement that runs until its context ends.
				execFn: func(ctx context.Context, _ string, _ ...any) (database.Result, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				},
				rollbackFn: func(context.Context) error { rollbacks++; return nil },
			}
			q := newBeginRecorder(conn)

			var txCtx context.Context
			err := database.WithTransaction(context.Background(), q, "root", func(c context.Context) error {
				txCtx = c
				_, err := database.FromContext(c).Exec(c, "UPDATE t SET c = 1")
				return err
			}, database.TxOptions{Timeout: time.Second})

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("WithTransaction() error = %v, want the deadline error", err)
			}
			if rollbacks != 1 {
				t.Errorf("driver rollbacks = %d, want 1", rollbacks)
			}
			if !database.FromContext(txCtx).IsClosed() {
				t.Error("IsClosed() = false after the timed-out transaction rolled back")
			}
			if got := database.Conn(txCtx, q); got != database.Querier(q) {
				t.Errorf("Conn() = %T, want the original querier", got)
			}
		})
	})
}

// TestMoreThanOneTxOptionsReachesNoBegin rejects a second TxOptions before
// the querier is asked to begin anything, so no driver transaction is opened
// and left behind by the failing call.
func TestMoreThanOneTxOptionsReachesNoBegin(t *testing.T) {
	two := []database.TxOptions{{IsoLevel: database.Serializable}, {}}
	tests := []struct {
		name string
		run  func(q *beginRecorder) error
	}{
		{
			name: "NewTransaction",
			run: func(q *beginRecorder) error {
				_, err := database.NewTransaction(context.Background(), q, "two_opts", two...)
				return err
			},
		},
		{
			name: "WithTransaction",
			run: func(q *beginRecorder) error {
				return database.WithTransaction(context.Background(), q, "two_opts", func(context.Context) error {
					t.Error("fn ran despite more than one TxOptions")
					return nil
				}, two...)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newBeginRecorder(&mockTxConn{})

			if err := tt.run(q); err == nil {
				t.Errorf("%s with two TxOptions error = nil, want an error", tt.name)
			}
			if got := q.beginCount(); got != 0 {
				t.Errorf("%s with two TxOptions reached Begin %d times, want 0", tt.name, got)
			}
		})
	}
}
