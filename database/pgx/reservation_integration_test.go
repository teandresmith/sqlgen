package pgx_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

// The connection reservation, measured against a real PostgreSQL connection
// (PRD §18.5).
//
// A second statement issued while the first still owns the connection is a
// protocol violation here, reported as `conn busy` and poisoning the connection
// so the rollback cannot run either — and pgx is the one adapter where two
// goroutines on one txCtx are also a Go-level data race. database/sql
// serializes each driver call on the connection's own lock, so the stdlib
// adapter has no race to find; MySQL still rejects the overlap (`busy buffer`,
// then a rollback that cannot run) and SQLite tolerates it.
//
// The bounded-wait probes live here only because the wait they measure belongs
// to database.Tx, not to the adapter: once the reservation refuses the second
// statement, the driver never sees it, so one adapter covers every dialect.
// pgx is the one that shows the most when the reservation is removed.

// leakWaitBound is the deadline the bounded-wait probes give a statement that
// is waiting for a connection nothing will release. PRD §18.5 states the wait
// is bounded only by ctx or TxOptions.Timeout; these measure that it actually
// is, so the value has to be long enough that a hang is distinguishable from a
// prompt failure.
const leakWaitBound = 2 * time.Second

// teardownBound is how long a root Commit or Rollback may take against a
// connection a leaked result set has pinned. §18.5 says it never waits at all,
// so anything near leakWaitBound is a regression; this is slack, not a target.
const teardownBound = 5 * time.Second

// drain reads a result set to exhaustion, which is what releases the
// reservation (PRD §18.5: drained or closed, whichever comes first).
func drain(t *testing.T, rows database.Rows) int {
	t.Helper()
	var n int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Errorf("Scan() error = %v", err)
			return n
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() after draining = %v, want nil — the error must survive the release", err)
	}
	return n
}

// TestReservationSerializesAFanOutInsideATransaction is the case the whole
// design exists for, and the shape the generated relationship loader has: N
// reads fanned out over one txCtx, every one of them landing on the same driver
// connection because database.Conn hands the body the *database.Tx.
//
// Unbounded and unserialized this was a data race in pgx's statement cache
// followed by `conn busy`, and because the rollback could not run on a busy
// connection either, the whole transaction was lost with it. Run
// under -race: the failure was a data race first and an error second.
func TestReservationSerializesAFanOutInsideATransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	q := pgxadapter.New(testPool)
	ctx := context.Background()

	// Row counts differ per edge so a read cannot pass by returning another
	// one's result set.
	wantRows := []int{3, 5, 7}

	err := database.WithTransaction(ctx, q, "fanout", func(txCtx context.Context) error {
		conn := database.Conn(txCtx, q)
		if conn != database.Querier(database.FromContext(txCtx)) {
			t.Error("Conn() did not return the transaction — the probe would not reach the reservation")
		}

		got := make([]int, len(wantRows))
		var wg sync.WaitGroup
		for i, want := range wantRows {
			wg.Go(func() {
				rows, err := conn.Query(txCtx, "SELECT i FROM generate_series(1, $1) AS i", want)
				if err != nil {
					t.Errorf("Query(%d rows) error = %v", want, err)
					return
				}
				defer func() {
					if err := rows.Close(); err != nil {
						t.Errorf("Close() error = %v", err)
					}
				}()
				got[i] = drain(t, rows)
			})
		}
		wg.Wait()

		for i, want := range wantRows {
			if got[i] != want {
				t.Errorf("edge %d read %d rows, want %d", i, got[i], want)
			}
		}

		// The connection survived: the original failure poisoned it, so a
		// statement issued after the fan-out is what proves it did not.
		if _, err := conn.Exec(txCtx, "SELECT 1"); err != nil {
			t.Errorf("Exec() after the fan-out = %v, want nil — the connection was poisoned", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransaction() error = %v", err)
	}
}

// TestLeakedResultSetDoesNotStrandTheConnection is the regression gate for the
// hang PRD §18.5's teardown exemption exists to avoid. A result set that is
// never drained and never closed keeps the reservation; by the time
// WithTransaction reaches Commit or Rollback, fn has returned, so that is a
// leak rather than a statement in flight. Blocking on it would strand a pooled
// connection for the life of the process — strictly worse than the loud driver
// error callers already get today.
func TestLeakedResultSetDoesNotStrandTheConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name     string
		teardown func(ctx context.Context) error
	}{
		{name: "commit", teardown: database.Commit},
		{name: "rollback", teardown: database.Rollback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := pgxadapter.New(testPool)

			txCtx, err := database.NewTransaction(context.Background(), q, "leak_"+tt.name)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			rows, err := tx.Query(txCtx, "SELECT i FROM generate_series(1, 2) AS i")
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			// Deliberately neither drained nor closed until the assertion is
			// made: closing here would hand the reservation back and the probe
			// would measure nothing.
			defer func() { _ = rows.Close() }()

			done := make(chan error, 1)
			go func() { done <- tt.teardown(txCtx) }()

			select {
			case err := <-done:
				// The driver's own complaint is the expected outcome, and it is
				// what callers already see today. What must not happen is a
				// wait: the assertion is that this arrived at all.
				if err == nil {
					t.Logf("%s() against a pinned connection = nil", tt.name)
				} else {
					t.Logf("%s() against a pinned connection = %v", tt.name, err)
				}
			case <-time.After(teardownBound):
				t.Fatalf("%s() did not return within %s: a leaked result set stranded the connection (PRD §18.5)", tt.name, teardownBound)
			}
		})
	}
}

// TestLeakedResultSetBoundsTheNextStatement covers the honest regression PRD
// §18.5 records: a misuse pgx reports immediately becomes a wait, bounded only
// by the caller's ctx or by TxOptions.Timeout. These are the only coverage of
// paths that were measured as permanent hangs before the design answered them.
//
// The §2.3 single-goroutine case is the third row, and it is the one that names
// the changed symptom: a goroutine that holds a result set open and then issues
// another statement is waiting for itself, so it must get the reservation's own
// bounded error and never the driver's `conn busy`.
func TestLeakedResultSetBoundsTheNextStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name string
		// opts configures the transaction; the TxOptions.Timeout case bounds
		// the wait without the caller touching a context at all.
		opts []database.TxOptions
		// query is the statement whose result set is leaked.
		query string
		// partialDrain reads one row before abandoning the set, which is the
		// §2.3 shape: a set that has started but not finished.
		partialDrain bool
		// deadline puts the bound on the ctx the next statement is issued with.
		deadline bool
	}{
		{
			name:     "ctx deadline",
			query:    "SELECT i FROM generate_series(1, 1) AS i",
			deadline: true,
		},
		{
			name:  "TxOptions.Timeout",
			opts:  []database.TxOptions{{Timeout: leakWaitBound}},
			query: "SELECT i FROM generate_series(1, 1) AS i",
		},
		{
			name:         "undrained multi-row set on the same goroutine",
			query:        "SELECT i FROM generate_series(1, 2) AS i",
			partialDrain: true,
			deadline:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := pgxadapter.New(testPool)

			// TxOptions.Timeout's clock starts at NewTransaction, a ctx
			// deadline's at context.WithTimeout below. The elapsed assertion
			// measures from whichever one bounds this case.
			boundStart := time.Now()
			txCtx, err := database.NewTransaction(context.Background(), q, "bounded", tt.opts...)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx := database.FromContext(txCtx)

			rows, err := tx.Query(txCtx, tt.query)
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			defer func() { _ = rows.Close() }()
			if tt.partialDrain && !rows.Next() {
				t.Fatalf("Next() = false on the first row, want true")
			}

			stmtCtx := txCtx
			if tt.deadline {
				var cancel context.CancelFunc
				boundStart = time.Now()
				stmtCtx, cancel = context.WithTimeout(txCtx, leakWaitBound)
				defer cancel()
			}

			_, execErr := tx.Exec(stmtCtx, "SELECT 1")
			elapsed := time.Since(boundStart)

			if execErr == nil {
				t.Fatal("Exec() error = nil while a result set held the connection, want a bounded failure")
			}
			if !errors.Is(execErr, context.DeadlineExceeded) {
				t.Errorf("Exec() error = %v, want it to wrap context.DeadlineExceeded", execErr)
			}
			if !strings.Contains(execErr.Error(), "waiting for the connection held by an open result set") {
				t.Errorf("Exec() error = %v, want it to name the open result set", execErr)
			}
			if strings.Contains(execErr.Error(), "conn busy") {
				t.Errorf("Exec() error = %v, want the reservation's bounded error rather than the driver's", execErr)
			}
			if elapsed < leakWaitBound {
				t.Errorf("Exec() failed after %s, want it to wait out the %s bound", elapsed, leakWaitBound)
			}
			if elapsed > leakWaitBound+teardownBound {
				t.Errorf("Exec() took %s, want it bounded near %s", elapsed, leakWaitBound)
			}

			if err := database.Rollback(txCtx); err != nil {
				t.Logf("Rollback() after the bounded failure = %v", err)
			}
		})
	}
}
