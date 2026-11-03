package pgx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

func TestBeginRejectsMoreThanOneTxOptions(t *testing.T) {
	// Nothing listens on port 1, so anything that reaches the server fails
	// with ErrConnectionFailed; the rejection must come before that.
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := pgxadapter.New(pool)

	tx, err := q.Begin(context.Background(), "root", database.TxOptions{}, database.TxOptions{})
	if err == nil {
		t.Fatal("Begin() with two TxOptions error = nil, want an error")
	}
	if tx != nil {
		t.Errorf("Begin() with two TxOptions tx = %v, want nil", tx)
	}
	if errors.Is(err, database.ErrConnectionFailed) {
		t.Errorf("Begin() with two TxOptions error = %v, want it rejected before contacting the server", err)
	}
}
