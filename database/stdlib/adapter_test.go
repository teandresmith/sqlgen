package stdlib_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/stdlib"
)

// heldTx is a Querier whose Begin hands back a transaction that is already
// open, which is how a test outside the package ends one it began directly.
type heldTx struct {
	database.Querier
	tx *database.Tx
}

func (h heldTx) Begin(context.Context, string, ...database.TxOptions) (*database.Tx, error) {
	return h.tx, nil
}

func rollBack(t *testing.T, tx *database.Tx) {
	t.Helper()
	txCtx, err := database.NewTransaction(context.Background(), heldTx{tx: tx}, "root")
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	if err := database.Rollback(txCtx); err != nil {
		t.Errorf("Rollback() error = %v", err)
	}
}

func TestBeginRejectsMoreThanOneTxOptions(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := stdlib.New(db)

	// withQuerier runs fn against the querier the case is named after.
	tests := []struct {
		name        string
		withQuerier func(t *testing.T, fn func(database.Querier))
	}{
		{
			name:        "pool",
			withQuerier: func(_ *testing.T, fn func(database.Querier)) { fn(q) },
		},
		{
			name: "pinned session",
			withQuerier: func(t *testing.T, fn func(database.Querier)) {
				t.Helper()
				pinner, ok := q.(database.SessionPinner)
				if !ok {
					t.Fatalf("stdlib.New() = %T, want a SessionPinner", q)
				}
				err := pinner.WithSession(context.Background(), func(session database.Querier) error {
					fn(session)
					return nil
				})
				if err != nil {
					t.Fatalf("WithSession() error = %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.withQuerier(t, func(querier database.Querier) {
				tx, err := querier.Begin(context.Background(), "root", database.TxOptions{}, database.TxOptions{})
				if err == nil {
					t.Error("Begin() with two TxOptions error = nil, want an error")
				}
				if tx != nil {
					t.Errorf("Begin() with two TxOptions tx = %v, want nil", tx)
					rollBack(t, tx)
				}

				// The same call with one TxOptions opens a transaction, so the
				// error above is the option count and not the database.
				tx, err = querier.Begin(context.Background(), "root", database.TxOptions{})
				if err != nil {
					t.Errorf("Begin() with one TxOptions error = %v, want nil", err)
				}
				if tx != nil {
					rollBack(t, tx)
				}
			})
		})
	}
}
