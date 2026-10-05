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
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

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
// these two dialects: a result set left neither drained nor closed keeps the
// reservation, and a root teardown must proceed without it rather than wait.
// The pooled connection would otherwise be stranded for the life of the
// process.
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

	for _, b := range backends() {
		for _, tt := range tests {
			t.Run(b.name+"/"+tt.name, func(t *testing.T) {
				cleanTables(t, b.db)
				q := stdlibadapter.New(b.db)

				txCtx, err := database.NewTransaction(context.Background(), q, "leak")
				if err != nil {
					t.Fatalf("NewTransaction() error = %v", err)
				}
				tx := database.FromContext(txCtx)

				if _, err := tx.Exec(txCtx, "INSERT INTO test_users (name, email) VALUES (?, ?)", "leak", "leak@example.com"); err != nil {
					t.Fatalf("Exec() error = %v", err)
				}
				rows, err := tx.Query(txCtx, "SELECT id FROM test_users")
				if err != nil {
					t.Fatalf("Query() error = %v", err)
				}
				// Deliberately neither drained nor closed until the assertion
				// is made: closing here hands the reservation back and the
				// probe measures nothing.
				defer func() { _ = rows.Close() }()

				done := make(chan error, 1)
				go func() { done <- tt.teardown(txCtx) }()

				select {
				case err := <-done:
					// Whether the driver accepts a teardown over an open
					// result set is its business; that it was *attempted*
					// rather than waited on is the property under test.
					t.Logf("%s() against a pinned connection = %v", tt.name, err)
				case <-time.After(teardownBound):
					t.Fatalf("%s() did not return within %s: a leaked result set stranded the connection (PRD §18.5)", tt.name, teardownBound)
				}
			})
		}
	}
}
