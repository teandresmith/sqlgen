package stdlib_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	stdlibadapter "github.com/teandresmith/sqlgen/database/stdlib"
)

// The connection reservation on the database/sql adapters (PRD §18.5).
//
// database/sql serializes each driver call on the connection's own lock, so
// the Go-level data race pgx reports cannot happen here. Serialized calls are
// not a reserved connection, though, and the two dialects part ways on what
// that leaves: MySQL rejects a statement issued while a result set is still
// open — `busy buffer`, then a rollback that fails with `invalid connection` —
// on one goroutine or two, while SQLite tolerates it. So a consumer who
// develops on SQLite meets the defect in production on either of the other
// two, and the reservation is what makes all three behave the same. What these
// assert is that the guarantee now holds here *for the stated reason*, and
// that adding it broke neither backend.

// teardownBound is how long a root teardown may take against a connection a
// leaked result set has pinned. §18.5 says it never waits at all, so this is
// slack rather than a target.
const teardownBound = 5 * time.Second

// TestReservationSerializesAFanOutInsideATransaction is the cross-dialect half
// of the acceptance criterion: a Tx's statements serialize on all three
// dialects. It is the same shape the generated relationship loader has — N
// reads fanned out over one txCtx, every one landing on the same connection
// because database.Conn hands the body the *database.Tx.
//
// Run under -race. Unlike the pgx counterpart this is not failing-first: it
// passed before the reservation too. On SQLite that is the driver's tolerance;
// on MySQL it is that these one-to-three-row reads did not happen to overlap an
// open result set, since one that does is `busy buffer` there. The generated
// loader's four-edge read in the mysql example tree does overlap, and fails
// without the reservation. The assertion here is that the reservation did not
// change a shape that already passed.
func TestReservationSerializesAFanOutInsideATransaction(t *testing.T) {
	requireDocker(t)

	// Row counts differ per edge so a read cannot pass by returning another
	// one's result set. The seeded rows are what each read counts.
	wantRows := []int{1, 2, 3}

	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			cleanTables(t, b.db)
			ctx := context.Background()
			q := stdlibadapter.New(b.db)

			for i := range 6 {
				name := fmt.Sprintf("fanout%d", i)
				if _, err := q.Exec(ctx, "INSERT INTO test_users (name, email) VALUES (?, ?)", name, name+"@example.com"); err != nil {
					t.Fatalf("seeding row %d: %v", i, err)
				}
			}

			err := database.WithTransaction(ctx, q, "fanout", func(txCtx context.Context) error {
				conn := database.Conn(txCtx, q)
				got := make([]int, len(wantRows))

				var wg sync.WaitGroup
				for i, want := range wantRows {
					wg.Go(func() {
						rows, err := conn.Query(txCtx, "SELECT id FROM test_users ORDER BY id LIMIT ?", want)
						if err != nil {
							t.Errorf("Query(%d rows) error = %v", want, err)
							return
						}
						defer func() {
							if err := rows.Close(); err != nil {
								t.Errorf("Close() error = %v", err)
							}
						}()
						var n int
						for rows.Next() {
							var id int64
							if err := rows.Scan(&id); err != nil {
								t.Errorf("Scan() error = %v", err)
								return
							}
							n++
						}
						if err := rows.Err(); err != nil {
							t.Errorf("rows.Err() after draining = %v, want nil — the error must survive the release", err)
						}
						got[i] = n
					})
				}
				wg.Wait()

				for i, want := range wantRows {
					if got[i] != want {
						t.Errorf("edge %d read %d rows, want %d", i, got[i], want)
					}
				}

				// The connection survived the fan-out and is still usable.
				if _, err := conn.Exec(txCtx, "DELETE FROM test_users WHERE id < 0"); err != nil {
					t.Errorf("Exec() after the fan-out = %v, want nil", err)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("WithTransaction() error = %v", err)
			}
		})
	}
}

// TestLeakedResultSetDoesNotStrandTheConnection is the teardown exemption on
// these dialects: a result set left neither drained nor closed keeps the
// reservation, and a root teardown must proceed without it rather than wait.
// The pooled connection would otherwise be stranded for the life of the
// process. The driver then accepts the teardown: the COMMIT persists the
// transaction's write and the ROLLBACK discards it.
func TestLeakedResultSetDoesNotStrandTheConnection(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) {
		t.Run("commit", func(t *testing.T) { assertLeakedTeardownSucceeds(t, mysqlDialect(), database.Commit, 1) })
		t.Run("rollback", func(t *testing.T) { assertLeakedTeardownSucceeds(t, mysqlDialect(), database.Rollback, 0) })
	})
	t.Run("sqlite", func(t *testing.T) {
		t.Run("commit", func(t *testing.T) { assertLeakedTeardownSucceeds(t, sqliteDialect(), database.Commit, 1) })
		t.Run("rollback", func(t *testing.T) { assertLeakedTeardownSucceeds(t, sqliteDialect(), database.Rollback, 0) })
	})
	t.Run("postgres", func(t *testing.T) {
		t.Run("commit", func(t *testing.T) { assertLeakedTeardownSucceeds(t, postgresDialect(), database.Commit, 1) })
		t.Run("rollback", func(t *testing.T) { assertLeakedTeardownSucceeds(t, postgresDialect(), database.Rollback, 0) })
	})
}

// assertLeakedTeardownSucceeds writes a row in a root transaction, leaves a
// result set open, and ends the transaction with teardown: it must return
// promptly, succeed, close the Tx, and leave wantRows rows behind.
func assertLeakedTeardownSucceeds(t *testing.T, d dialect, teardown func(context.Context) error, wantRows int) {
	t.Helper()
	cleanTables(t, d.db)
	q := stdlibadapter.New(d.db)

	txCtx, err := database.NewTransaction(context.Background(), q, "leak")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	if _, err := tx.Exec(txCtx, d.insert, "leak", "leak@example.com"); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	rows, err := tx.Query(txCtx, "SELECT id FROM test_users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	// Deliberately neither drained nor closed until the assertion is made:
	// closing here hands the reservation back and the probe measures nothing.
	defer func() { _ = rows.Close() }()

	done := make(chan error, 1)
	go func() { done <- teardown(txCtx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("teardown against a pinned connection = %v, want nil", err)
		}
		if !tx.IsClosed() {
			t.Error("IsClosed() = false after the teardown, want true")
		}
	case <-time.After(teardownBound):
		t.Fatalf("teardown did not return within %s: a leaked result set stranded the connection", teardownBound)
	}

	_ = rows.Close()
	if !tx.IsClosed() {
		// A refused teardown leaves the transaction holding the connection,
		// and SQLite's pool has only one.
		_ = database.Rollback(txCtx)
	}
	if got := d.rows(t, "leak@example.com"); got != wantRows {
		t.Errorf("rows after the teardown = %d, want %d", got, wantRows)
	}
}

// TestDrainedResultSetReleasesTheConnection iterates a result set to
// exhaustion without closing it and then issues another statement on the same
// transaction. Exhaustion alone must give the connection back: the statement
// runs at once instead of waiting on the reservation until its deadline.
func TestDrainedResultSetReleasesTheConnection(t *testing.T) {
	requireDocker(t)
	t.Run("mysql", func(t *testing.T) { assertDrainReleases(t, mysqlDialect()) })
	t.Run("sqlite", func(t *testing.T) { assertDrainReleases(t, sqliteDialect()) })
	t.Run("postgres", func(t *testing.T) { assertDrainReleases(t, postgresDialect()) })
}

func assertDrainReleases(t *testing.T, d dialect) {
	t.Helper()
	cleanTables(t, d.db)
	ctx := context.Background()
	q := stdlibadapter.New(d.db)

	txCtx, err := database.NewTransaction(ctx, q, "drain_release")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	tx := database.FromContext(txCtx)

	rows, err := tx.Query(txCtx, "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	var n int
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Errorf("rows.Err() after draining = %v, want nil", err)
	}
	if n != 3 {
		t.Errorf("drained %d rows, want 3", n)
	}

	stmtCtx, cancel := context.WithTimeout(txCtx, 2*time.Second)
	defer cancel()
	if _, err := tx.Exec(stmtCtx, d.insert, "drain", "drain@example.com"); err != nil {
		t.Errorf("Exec() after draining without Close = %v, want nil: the drained result set still holds the connection", err)
	}

	_ = rows.Close()
	if err := database.Rollback(txCtx); err != nil {
		t.Errorf("Rollback() error = %v", err)
	}
}
