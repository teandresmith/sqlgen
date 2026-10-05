package tests

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// Statement counts for nested mutations on MySQL (PRD §9.9.7).
//
// The bound was written against PostgreSQL, and on MySQL it does not carry
// over unchanged. Every verb that routes through a `*Where` method is preceded
// by that method's pre-SELECT (PRD §9.8.6b), a statement PostgreSQL's
// RETURNING never needs. That covers the O2M `connect` adoption, O2M
// `disconnect` and `clear` on either shape. These tests pin the per-verb cost
// MySQL actually pays, and PRD §9.9.7 now states it.
//
// Counting is done by the server. A database.Querier shim cannot see inside a
// transaction, because database.Conn hands the body the *database.Tx and the
// statements go straight to the driver connection it holds (graphql's
// statement_counter_test.go records the vacuous pin that caused). MySQL's
// per-session status counters sit below every Go layer, and a pool of exactly
// one connection makes them this client's counters. Com_* counts a prepared
// execution under its statement class, which is how go-sql-driver sends
// anything carrying arguments. Questions counts every statement, BEGIN and
// COMMIT included.

// statementCounts is how many statements of each class one call issued.
type statementCounts struct {
	Select, Insert, Update, Delete, Total int
}

func (c statementCounts) minus(base statementCounts) statementCounts {
	return statementCounts{
		Select: c.Select - base.Select,
		Insert: c.Insert - base.Insert,
		Update: c.Update - base.Update,
		Delete: c.Delete - base.Delete,
		Total:  c.Total - base.Total,
	}
}

// countedClient returns a generated client over its own single-connection
// pool, and a function that reports what a call issued on that connection.
func countedClient(t *testing.T) (*models.Client, func(t *testing.T, fn func() error) statementCounts) {
	t.Helper()
	db, err := sql.Open("mysql", testConnStr)
	if err != nil {
		t.Fatalf("opening the counted pool: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	read := func(t *testing.T) map[string]int {
		t.Helper()
		rows, err := db.QueryContext(context.Background(),
			"SHOW SESSION STATUS WHERE Variable_name IN ('Questions', 'Com_select', 'Com_insert', 'Com_update', 'Com_delete')")
		if err != nil {
			t.Fatalf("reading session counters: %v", err)
		}
		defer rows.Close()
		out := make(map[string]int, 5)
		for rows.Next() {
			var name string
			var value int
			if err := rows.Scan(&name, &value); err != nil {
				t.Fatalf("scanning session counters: %v", err)
			}
			out[name] = value
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("reading session counters: %v", err)
		}
		return out
	}

	measure := func(t *testing.T, fn func() error) statementCounts {
		t.Helper()
		before := read(t)
		if err := fn(); err != nil {
			t.Fatalf("measured call: %v", err)
		}
		after := read(t)
		return statementCounts{
			Select: after["Com_select"] - before["Com_select"],
			Insert: after["Com_insert"] - before["Com_insert"],
			Update: after["Com_update"] - before["Com_update"],
			Delete: after["Com_delete"] - before["Com_delete"],
			// Questions also counts the SHOW that took the second reading.
			Total: after["Questions"] - before["Questions"] - 1,
		}
	}
	return models.New(dbstdlib.New(db)), measure
}

// seedParentedEvents inserts n events on owner's edge in one CreateMany.
func seedParentedEvents(t *testing.T, client *models.Client, owner *int64, n int, label string) []int64 {
	t.Helper()
	inputs := make([]*models.CreateUserEventInput, 0, n)
	for i := range n {
		in := &models.CreateUserEventInput{Action: fmt.Sprintf("%s-%03d", label, i)}
		if owner != nil {
			in.UserID = omittable.Set(owner)
		}
		inputs = append(inputs, in)
	}
	rows, err := client.UserEvents().CreateMany(context.Background(), inputs)
	if err != nil {
		t.Fatalf("seeding %d events: %v", n, err)
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	t.Cleanup(func() { _ = client.UserEvents().HardDeleteMany(context.Background(), ids) })
	return ids
}

// TestNestedQueryCount_ClearIsTwoStatementsOnMySQL restates §9.9.7's one exact
// number for this dialect. `clear` names no ids, so it is a single write on
// every dialect, but MySQL precedes the write with the pre-SELECT that names
// the rows it is about to change. Two statements, never more.
func TestNestedQueryCount_ClearIsTwoStatementsOnMySQL(t *testing.T) {
	ctx := context.Background()
	counted, measure := countedClient(t)
	client := newClient()

	t.Run("O2M", func(t *testing.T) {
		user := seedNestedUser(t, client, "count-clear-o2m@example.com")
		seedParentedEvents(t, client, &user, 3, "count-clear-o2m")

		base := measure(t, func() error {
			_, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{})
			return err
		})
		got := measure(t, func() error {
			_, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
				UserEvents: &models.UserUserEventsUpdateNested{Clear: true},
			})
			return err
		}).minus(base)

		if want := (statementCounts{Select: 1, Update: 1, Total: 2}); got != want {
			t.Errorf("an O2M clear cost %+v over the parent-only call, want %+v: one pre-SELECT and one UPDATE", got, want)
		}
	})

	t.Run("M2M", func(t *testing.T) {
		asset, err := client.Assets().CreateWithRelated(ctx, &models.CreateAssetWithRelatedInput{
			Asset: models.CreateAssetInput{Name: "count-clear-m2m"},
			Documents: &models.AssetDocumentsCreateNested{Create: []*models.CreateDocumentInput{
				{EntityType: models.DocumentsEntityTypeEnumSpv, Name: "count-clear-m2m-1.pdf"},
				{EntityType: models.DocumentsEntityTypeEnumSpv, Name: "count-clear-m2m-2.pdf"},
			}},
		})
		if err != nil {
			t.Fatalf("seeding the asset: %v", err)
		}
		t.Cleanup(func() {
			_ = client.Assets().HardDelete(ctx, asset.ID)
			_ = client.Documents().HardDeleteWhere(ctx, &models.DocumentFilter{Name: &comparator.String{In: []string{"count-clear-m2m-1.pdf", "count-clear-m2m-2.pdf"}}})
		})

		base := measure(t, func() error {
			_, err := counted.Assets().UpdateWithRelated(ctx, asset.ID, &models.UpdateAssetWithRelatedInput{})
			return err
		})
		got := measure(t, func() error {
			_, err := counted.Assets().UpdateWithRelated(ctx, asset.ID, &models.UpdateAssetWithRelatedInput{
				Documents: &models.AssetDocumentsUpdateNested{Clear: true},
			})
			return err
		}).minus(base)

		if want := (statementCounts{Select: 1, Delete: 1, Total: 2}); got != want {
			t.Errorf("an M2M clear cost %+v over the parent-only call, want %+v: one pre-SELECT and one DELETE", got, want)
		}
	})
}

// TestNestedQueryCount_DoesNotScaleWithChildren is §9.9.7's invariant on
// MySQL. The same call shape with ten times the children must cost the same,
// and only a per-row statement breaks that. The total is compared rather than
// fixed, because it is a sum over whatever the inner clients issue.
func TestNestedQueryCount_DoesNotScaleWithChildren(t *testing.T) {
	ctx := context.Background()
	counted, measure := countedClient(t)
	client := newClient()

	run := func(t *testing.T, n int) statementCounts {
		t.Helper()
		label := fmt.Sprintf("count-scale-%d", n)
		user := seedNestedUser(t, client, label+"@example.com")
		linked := seedParentedEvents(t, client, &user, n, label+"-linked")
		orphans := seedParentedEvents(t, client, nil, n, label+"-orphan")
		asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: label})
		if err != nil {
			t.Fatalf("seeding the asset: %v", err)
		}
		t.Cleanup(func() {
			_ = client.Assets().HardDelete(ctx, asset.ID)
			_ = client.Documents().HardDeleteWhere(ctx, &models.DocumentFilter{Name: &comparator.String{StartsWith: new(label + "-created-")}})
		})
		docs := make([]int64, 0, n)
		creates := make([]*models.UserUserEventsCreateInput, 0, n)
		docCreates := make([]*models.CreateDocumentInput, 0, n)
		for i := range n {
			docs = append(docs, seedDocument(t, client, fmt.Sprintf("%s-connect-%03d.pdf", label, i)))
			creates = append(creates, &models.UserUserEventsCreateInput{Action: fmt.Sprintf("%s-created-%03d", label, i)})
			docCreates = append(docCreates, &models.CreateDocumentInput{EntityType: models.DocumentsEntityTypeEnumSpv, Name: fmt.Sprintf("%s-created-%03d.pdf", label, i)})
		}

		return measure(t, func() error {
			if _, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
				UserEvents: &models.UserUserEventsUpdateNested{Create: creates, Connect: orphans, Disconnect: linked},
			}); err != nil {
				return err
			}
			_, err := counted.Assets().UpdateWithRelated(ctx, asset.ID, &models.UpdateAssetWithRelatedInput{
				Documents: &models.AssetDocumentsUpdateNested{Create: docCreates, Connect: docs},
			})
			return err
		})
	}

	small, large := run(t, 2), run(t, 20)
	if small != large {
		t.Errorf("statement counts scaled with the child count: %+v for 2 children, %+v for 20. §9.9.7 forbids a per-row statement", small, large)
	}
	// A floor, so a counter that stopped seeing the transaction body cannot
	// make the comparison pass vacuously.
	if small.Update == 0 || small.Insert == 0 {
		t.Fatalf("the counter saw %+v for two nested calls; it is not observing the transaction body", small)
	}
}

// TestNestedQueryCount_ChunksAtBatchSize pins on MySQL that `connect` and
// `disconnect` batch at `batchSize`, stated per statement class so the
// pre-SELECT is visible as its own term.
//
// 401 ids is three chunks at the default batch size of 200. A `disconnect`
// is one pre-SELECT and one UPDATE per chunk. A `connect` is the visibility
// read per chunk, then per adopted chunk the pre-SELECT, the UPDATE, and the
// verify read the shortfall check counts. An unbatched implementation issues
// one of each over the whole list.
func TestNestedQueryCount_ChunksAtBatchSize(t *testing.T) {
	const (
		n         = 401
		batchSize = 200 // generation.batch_size default (PRD §4.6)
	)
	chunks := (n + batchSize - 1) / batchSize

	ctx := context.Background()
	counted, measure := countedClient(t)
	client := newClient()

	user := seedNestedUser(t, client, "count-batch@example.com")
	base := measure(t, func() error {
		_, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{})
		return err
	})

	t.Run("disconnect", func(t *testing.T) {
		linked := seedParentedEvents(t, client, &user, n, "count-batch-linked")
		got := measure(t, func() error {
			_, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
				UserEvents: &models.UserUserEventsUpdateNested{Disconnect: linked},
			})
			return err
		}).minus(base)
		if want := (statementCounts{Select: chunks, Update: chunks, Total: 2 * chunks}); got != want {
			t.Errorf("disconnecting %d ids cost %+v, want %+v", n, got, want)
		}
		rest, err := userEventActions(ctx, client, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(rest) != 0 {
			t.Errorf("%d events are still on the edge; not every chunk ran", len(rest))
		}
	})

	t.Run("connect", func(t *testing.T) {
		orphans := seedParentedEvents(t, client, nil, n, "count-batch-orphan")
		got := measure(t, func() error {
			_, err := counted.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
				UserEvents: &models.UserUserEventsUpdateNested{Connect: orphans},
			})
			return err
		}).minus(base)
		if want := (statementCounts{Select: 3 * chunks, Update: chunks, Total: 4 * chunks}); got != want {
			t.Errorf("connecting %d ids cost %+v, want %+v", n, got, want)
		}
		on, err := userEventActions(ctx, client, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(on) != n {
			t.Errorf("%d of %d orphans were adopted; not every chunk ran", len(on), n)
		}
	})
}
