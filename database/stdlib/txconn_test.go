package stdlib

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/database"
)

// endedTx begins a transaction on an in-memory SQLite database with ctx and
// rolls it back directly through database/sql, so the adapter's txConn wraps a
// transaction database/sql has already ended.
func endedTx(ctx context.Context, t *testing.T) *txConn {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("ending the transaction through database/sql: %v", err)
	}
	return &txConn{tx: tx}
}

func TestTxConnRollbackOfAnEndedTransactionSucceeds(t *testing.T) {
	c := endedTx(context.Background(), t)

	if err := c.Rollback(context.Background()); err != nil {
		t.Errorf("Rollback() of a transaction database/sql already ended = %v, want nil", err)
	}
}

func TestTxConnCommitOfAnEndedTransactionIsTxDone(t *testing.T) {
	tests := []struct {
		name string
		// cancel reports whether the context the transaction was begun with is
		// cancelled before the COMMIT.
		cancel bool
		want   error
	}{
		{name: "context cancelled", cancel: true, want: context.Canceled},
		{name: "context live", cancel: false, want: sql.ErrTxDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := endedTx(ctx, t)
			if tt.cancel {
				cancel()
			}

			err := c.Commit(ctx)

			if !errors.Is(err, database.ErrTxDone) {
				t.Errorf("Commit() of an ended transaction = %v, want it to wrap database.ErrTxDone", err)
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("Commit() of an ended transaction = %v, want it to wrap %v", err, tt.want)
			}
		})
	}
}
