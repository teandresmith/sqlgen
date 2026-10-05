package tests

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// A statement counter that sees inside transactions.
//
// `countingQuerier` (query_count_test.go) wraps a database.Querier, which is
// the right layer for the read path and the wrong one for anything
// transactional: database.Conn hands the transaction body the *database.Tx,
// and that Tx holds the driver connection the shim delegated to when it
// answered Begin. Every statement between BEGIN and COMMIT therefore bypasses
// the shim, and a query-count assertion over a method that opens a transaction
// — which is every `…WithRelated` — measures exactly one call.
//
// A pgx QueryTracer sits below that boundary. It is invoked for Exec, Query and
// QueryRow on the connection itself, so it counts the transaction's own BEGIN
// and COMMIT alongside the statements inside it; assertions below allow for
// that small constant rather than trying to subtract it.

// statementCounter is a pgx.QueryTracer that counts every statement issued on
// the connections of the pool it is installed on.
type statementCounter struct{ n atomic.Int64 }

func (s *statementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	s.n.Add(1)
	return ctx
}

func (s *statementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (s *statementCounter) count() int { return int(s.n.Load()) }

func (s *statementCounter) reset() { s.n.Store(0) }

// tracedClient returns a generated client backed by its own pool against the
// shared container, with a statement counter installed on every connection.
//
// MaxConns is 1 so the count cannot be split across connections in a way that
// hides a fan-out, which also matches the shape a transaction runs under.
func tracedClient(t *testing.T) (*models.Client, *statementCounter) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testConnStr)
	if err != nil {
		t.Fatalf("parsing the container DSN: %v", err)
	}
	counter := &statementCounter{}
	cfg.ConnConfig.Tracer = counter
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("creating the traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return models.New(dbpgx.New(pool)), counter
}
