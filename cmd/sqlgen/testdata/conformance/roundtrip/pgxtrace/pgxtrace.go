// Package pgxtrace reports the statements a pgx pool sends to a
// roundtrip.Recorder, through pgx's tracer hooks, so statements sent inside a
// transaction are reported too.
package pgxtrace

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
)

// NewPool opens a pgx pool on dsn whose every connection reports to r, and
// warms n connections so a measured operation that fans out does not wait on
// a connection handshake. Acquiring a connection skips pgxpool's liveness
// ping, so no ping round trip lands between two measured statements.
func NewPool(ctx context.Context, r *roundtrip.Recorder, dsn string, n int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = &tracer{r: r}
	cfg.ShouldPing = func(context.Context, pgxpool.ShouldPingParams) bool { return false }
	cfg.MaxConns = max(cfg.MaxConns, int32(n))
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	conns := make([]*pgxpool.Conn, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { conns[i], errs[i] = pool.Acquire(ctx) })
	}
	wg.Wait()
	for _, c := range conns {
		if c != nil {
			c.Release()
		}
	}
	if err := errors.Join(errs...); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// tracer reports Exec, Query and QueryRow as one statement each, a batch as
// its queued statements sent in one round trip, and CopyFrom as one statement.
// pgx sends BEGIN, COMMIT and ROLLBACK through Exec; the Recorder drops them.
type tracer struct{ r *roundtrip.Recorder }

var (
	_ pgx.QueryTracer    = (*tracer)(nil)
	_ pgx.BatchTracer    = (*tracer)(nil)
	_ pgx.CopyFromTracer = (*tracer)(nil)
)

// doneKey carries a traced call's completion func from its start hook to its
// end hook.
type doneKey struct{}

func withDone(ctx context.Context, done func()) context.Context {
	return context.WithValue(ctx, doneKey{}, done)
}

func finish(ctx context.Context) {
	if done, ok := ctx.Value(doneKey{}).(func()); ok {
		done()
	}
}

func (t *tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return withDone(ctx, t.r.Statement(data.SQL))
}

func (t *tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	finish(ctx)
}

func (t *tracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	sqls := make([]string, len(data.Batch.QueuedQueries))
	for i, q := range data.Batch.QueuedQueries {
		sqls[i] = q.SQL
	}
	return withDone(ctx, t.r.Batch(sqls))
}

func (t *tracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}

func (t *tracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchEndData) {
	finish(ctx)
}

func (t *tracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	return withDone(ctx, t.r.Statement("COPY "+data.TableName.Sanitize()))
}

func (t *tracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromEndData) {
	finish(ctx)
}
