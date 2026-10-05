package tests

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// Key derivation under auto_increment_increment > 1 (PRD §9.8.5).
//
// MySQL has no RETURNING, so CreateMany derives a multi-row INSERT's keys from
// LastInsertId and the session's auto_increment_increment. InnoDB spaces the
// keys by that step, which is 1 by default, the cluster size under Galera and
// a configured value under multi-primary replication. A derivation that
// assumes 1 names rows the statement did not write. It still returns a
// plausible number of rows once another writer fills the gaps, so every test
// here asserts rows by name, never by count.

// stepConnector opens sessions that cycle through several
// auto_increment_increment values, so one pool can hold sessions that disagree
// on the step, the way a pool does after the setting changes at runtime.
type stepConnector struct {
	next       atomic.Uint64
	connectors []driver.Connector
}

func (s *stepConnector) Connect(ctx context.Context) (driver.Conn, error) {
	i := s.next.Add(1) - 1
	return s.connectors[i%uint64(len(s.connectors))].Connect(ctx)
}

func (s *stepConnector) Driver() driver.Driver { return s.connectors[0].Driver() }

// openStepPool opens a pool against the test database whose sessions run with
// the given auto_increment_increment values, each new session taking the next
// one in turn.
func openStepPool(t *testing.T, steps ...int) *sql.DB {
	t.Helper()
	sc := &stepConnector{}
	for _, step := range steps {
		cfg, err := mysql.ParseDSN(testConnStr)
		if err != nil {
			t.Fatalf("parsing the test DSN: %v", err)
		}
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params["auto_increment_increment"] = strconv.Itoa(step)
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatalf("building a connector for step %d: %v", step, err)
		}
		sc.connectors = append(sc.connectors, connector)
	}
	db := sql.OpenDB(sc)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// categoryIDsByName reads the keys the named categories really have, through
// the shared pool rather than anything the client derived.
func categoryIDsByName(t *testing.T, names []string) map[string]int32 {
	t.Helper()
	got := make(map[string]int32, len(names))
	for _, name := range names {
		var id int32
		if err := testDB.QueryRowContext(context.Background(), "SELECT id FROM categories WHERE name = ?", name).Scan(&id); err != nil {
			t.Fatalf("reading category %s: %v", name, err)
		}
		got[name] = id
	}
	return got
}

func deleteCategoriesLike(t *testing.T, prefix string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(context.Background(), "DELETE FROM categories WHERE name LIKE ?", prefix+"%")
	})
}

// TestCreateMany_KeysFollowTheSessionStep runs CreateMany from a pool whose
// every session has auto_increment_increment=2, on the pool and inside a
// caller's transaction. 401 rows span three statements at the default batch
// size of 200, and the last one carries a single row, which takes no step.
//
// Verified failing-first: against the `firstID + i` derivation every case
// returns rows other than the ones it inserted.
func TestCreateMany_KeysFollowTheSessionStep(t *testing.T) {
	ctx := context.Background()
	client := models.New(dbstdlib.New(openStepPool(t, 2)))

	tests := []struct {
		name string
		rows int
		inTx bool
	}{
		{name: "3 rows on the pool", rows: 3},
		{name: "3 rows in a caller transaction", rows: 3, inTx: true},
		{name: "401 rows on the pool", rows: 401},
		{name: "401 rows in a caller transaction", rows: 401, inTx: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := fmt.Sprintf("step2-%d-%v-", tt.rows, tt.inTx)
			deleteCategoriesLike(t, prefix)
			inputs := make([]*models.CreateCategoryInput, tt.rows)
			names := make([]string, tt.rows)
			for i := range tt.rows {
				names[i] = fmt.Sprintf("%s%03d", prefix, i)
				inputs[i] = &models.CreateCategoryInput{Name: names[i]}
			}

			var created []*models.Category
			call := func(ctx context.Context) error {
				var err error
				created, err = client.Categories().CreateMany(ctx, inputs)
				return err
			}
			var err error
			if tt.inTx {
				err = client.WithTx(ctx, "step2", call)
			} else {
				err = call(ctx)
			}
			if err != nil {
				t.Fatalf("CreateMany(%d rows): %v", tt.rows, err)
			}

			real := categoryIDsByName(t, names)
			got := make([]string, 0, len(created))
			for _, c := range created {
				got = append(got, c.Name)
				if want, ok := real[c.Name]; ok && c.ID != want {
					t.Errorf("CreateMany returned %s with id %d; its row has id %d", c.Name, c.ID, want)
				}
			}
			slices.Sort(got)
			if diff := cmp.Diff(names, got); diff != "" {
				t.Errorf("CreateMany returned rows other than the ones it inserted (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCreateMany_KeysFromAMixedStepPool runs CreateMany concurrently from a
// pool whose sessions alternate between steps 1 and 3. A step read that ran on
// whichever session the pool handed out, rather than the one that ran the
// INSERT, reads the other step and derives the wrong keys. That is what pins
// the session.
//
// Verified failing-first: with the step read issued on the pool, between 16
// and 36 of the 320 calls returned rows other than their own across four runs.
func TestCreateMany_KeysFromAMixedStepPool(t *testing.T) {
	ctx := context.Background()
	db := openStepPool(t, 1, 3)
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(16)
	client := models.New(dbstdlib.New(db))
	deleteCategoriesLike(t, "mixed-")

	var (
		mu    sync.Mutex
		wrong []string
		wg    sync.WaitGroup
	)
	for g := range 16 {
		wg.Go(func() {
			for r := range 20 {
				inputs := make([]*models.CreateCategoryInput, 3)
				for i := range inputs {
					inputs[i] = &models.CreateCategoryInput{Name: fmt.Sprintf("mixed-%02d-%02d-%d", g, r, i)}
				}
				created, err := client.Categories().CreateMany(ctx, inputs)
				mu.Lock()
				switch {
				case err != nil:
					wrong = append(wrong, fmt.Sprintf("%s: %v", inputs[0].Name, err))
				case len(created) != len(inputs):
					wrong = append(wrong, fmt.Sprintf("%s: %d rows back, want %d", inputs[0].Name, len(created), len(inputs)))
				default:
					for i, c := range created {
						if c.Name != inputs[i].Name {
							wrong = append(wrong, fmt.Sprintf("%s came back as %s", inputs[i].Name, c.Name))
						}
					}
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(wrong) > 0 {
		t.Errorf("%d CreateMany results named rows other than the ones inserted; first: %s", len(wrong), wrong[0])
	}
}

// TestCreateMany_StepReadOpensNoTransaction pins how the pool path reaches one
// session: a pinned connection, not a transaction. A transaction would pin it
// too, but it moves the INSERT's outcome to COMMIT, where Percona XtraDB
// Cluster can report a lost certification conflict as a success.
// Questions counts BEGIN and COMMIT, so a transaction would show as 5.
func TestCreateMany_StepReadOpensNoTransaction(t *testing.T) {
	client, measure := countedClient(t)
	deleteCategoriesLike(t, "pin-")
	inputs := []*models.CreateCategoryInput{{Name: "pin-0"}, {Name: "pin-1"}, {Name: "pin-2"}}

	got := measure(t, func() error {
		_, err := client.Categories().CreateMany(context.Background(), inputs)
		return err
	})
	// The INSERT, the step read and the trailing GetMany.
	want := statementCounts{Select: 2, Insert: 1, Total: 3}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("a 3-row CreateMany on the pool issued other statements than INSERT, step read and re-read (-want +got):\n%s", diff)
	}
}

// TestCreateMany_StaysInTheCallerTransaction rolls back a transaction that ran
// a multi-row CreateMany and expects none of its rows to survive. A
// *database.Tx is already one session, so it is used as it is. If the pool
// path pinned a fresh connection here instead, the INSERT would autocommit
// outside the caller's transaction, and every key test above would still pass.
func TestCreateMany_StaysInTheCallerTransaction(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	deleteCategoriesLike(t, "rollback-")
	names := []string{"rollback-0", "rollback-1", "rollback-2"}
	inputs := make([]*models.CreateCategoryInput, len(names))
	for i, name := range names {
		inputs[i] = &models.CreateCategoryInput{Name: name}
	}

	errAbort := errors.New("abort")
	err := client.WithTx(ctx, "rollback", func(txCtx context.Context) error {
		created, err := client.Categories().CreateMany(txCtx, inputs)
		if err != nil {
			return err
		}
		if len(created) != len(inputs) {
			return fmt.Errorf("CreateMany returned %d rows inside the transaction, want %d", len(created), len(inputs))
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("WithTx: %v, want the abort that rolls it back", err)
	}

	var survived int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM categories WHERE name LIKE 'rollback-%'").Scan(&survived); err != nil {
		t.Fatal(err)
	}
	if survived != 0 {
		t.Errorf("%d of the rolled-back transaction's rows were committed; the INSERT ran outside it", survived)
	}
}

// unpinnableQuerier hides every method but database.Querier's, the way a
// caller's instrumentation wrapper does when it forwards only the interface.
type unpinnableQuerier struct{ database.Querier }

// TestCreateMany_QuerierThatCannotPin covers the one pool the derivation cannot
// make exact. A multi-row CreateMany fails before it writes, rather than
// reading the step from whichever session answers. A single row needs no
// step, and a transaction is already one session, so both still work.
func TestCreateMany_QuerierThatCannotPin(t *testing.T) {
	ctx := context.Background()
	client := models.New(unpinnableQuerier{dbstdlib.New(testDB)})
	deleteCategoriesLike(t, "unpinned-")

	_, err := client.Categories().CreateMany(ctx, []*models.CreateCategoryInput{{Name: "unpinned-a"}, {Name: "unpinned-b"}})
	if err == nil || !strings.Contains(err.Error(), "database.SessionPinner") {
		t.Errorf("multi-row CreateMany through a querier that cannot pin: err = %v, want the SessionPinner error", err)
	}
	if got, err := client.Categories().Count(ctx, &models.CategoryFilter{Name: &comparator.String{In: []string{"unpinned-a", "unpinned-b"}}}); err != nil || got != 0 {
		t.Errorf("the refused CreateMany wrote %d rows (err %v), want none", got, err)
	}

	if _, err := client.Categories().CreateMany(ctx, []*models.CreateCategoryInput{{Name: "unpinned-single"}}); err != nil {
		t.Errorf("single-row CreateMany through a querier that cannot pin: %v", err)
	}

	err = client.WithTx(ctx, "unpinned", func(txCtx context.Context) error {
		_, err := client.Categories().CreateMany(txCtx, []*models.CreateCategoryInput{{Name: "unpinned-tx-a"}, {Name: "unpinned-tx-b"}})
		return err
	})
	if err != nil {
		t.Errorf("multi-row CreateMany in a transaction through a querier that cannot pin: %v", err)
	}
}
