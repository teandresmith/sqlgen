// Package pgx provides a pgx v5 adapter implementing the database.Querier interface.
package pgx

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/teandresmith/sqlgen/database"
)

// Compile-time check that pgxAdapter implements database.Querier.
var _ database.Querier = (*pgxAdapter)(nil)

// pgxAdapter wraps a *pgxpool.Pool to satisfy the database.Querier interface.
type pgxAdapter struct {
	pool *pgxpool.Pool
}

// New returns a database.Querier wrapping the given pgxpool.Pool.
func New(pool *pgxpool.Pool) database.Querier {
	return &pgxAdapter{pool: pool}
}

// Exec executes a query that doesn't return rows.
func (a *pgxAdapter) Exec(ctx context.Context, sql string, arguments ...any) (database.Result, error) {
	tag, err := a.pool.Exec(ctx, sql, arguments...)
	if err != nil {
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("exec: %w", err)
	}
	return &result{tag: tag}, nil
}

// Query executes a query that returns rows.
func (a *pgxAdapter) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("query: %w", err)
	}
	return &rows{r: r}, nil
}

// QueryRow executes a query that returns at most one row.
func (a *pgxAdapter) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{r: a.pool.QueryRow(ctx, sql, args...)}
}

// Begin starts a new database transaction and returns a *database.Tx.
// TxOptions are translated into pgx transaction options.
func (a *pgxAdapter) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	if len(opts) > 1 {
		return nil, fmt.Errorf("pgx: at most one TxOptions may be passed to Begin")
	}

	pgxOpts := pgx.TxOptions{}
	var mode database.CallbackMode
	if len(opts) == 1 {
		pgxOpts = translateTxOptions(opts[0])
		mode = opts[0].CallbackMode
	}

	tx, err := a.pool.BeginTx(ctx, pgxOpts)
	if err != nil {
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			err = mapped
		}
		return nil, fmt.Errorf("beginning pgx transaction: %w", err)
	}

	return database.NewTx(&txConn{tx: tx}, name, mode), nil
}

// SendBatch sends a batch of queries to PostgreSQL in a single round-trip.
// This is used by UpdateMany to pipeline multiple statements efficiently.
// Errors read from the results are mapped like Exec's and Query's.
func (a *pgxAdapter) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return &batchResults{br: a.pool.SendBatch(ctx, batch)}
}

// mapBatchError maps err like Exec does, or returns it unchanged when it is
// not a driver error MapError recognises.
func mapBatchError(err error) error {
	if ok, mapped := MapError(err); ok {
		return mapped
	}
	return err
}

// batchResults maps the errors of a pgx.BatchResults.
type batchResults struct {
	br pgx.BatchResults
}

// Exec reads the next query's result, mapping its error.
func (b *batchResults) Exec() (pgconn.CommandTag, error) {
	tag, err := b.br.Exec()
	return tag, mapBatchError(err)
}

// Query reads the next query's rows, mapping its error and the rows' Err.
func (b *batchResults) Query() (pgx.Rows, error) {
	r, err := b.br.Query()
	return &batchRows{Rows: r}, mapBatchError(err)
}

// QueryRow reads the next query's row, mapping the error Scan reports.
func (b *batchResults) QueryRow() pgx.Row {
	return batchRow{r: b.br.QueryRow()}
}

// Close closes the batch, mapping its error.
func (b *batchResults) Close() error {
	return mapBatchError(b.br.Close())
}

// batchRows maps the error a batch query's rows report.
type batchRows struct {
	pgx.Rows
}

// Err maps the error the rows report.
func (r *batchRows) Err() error {
	return mapBatchError(r.Rows.Err())
}

// batchRow maps the error a batch QueryRow's Scan reports.
type batchRow struct {
	r pgx.Row
}

// Scan scans the row, mapping its error.
func (r batchRow) Scan(dest ...any) error {
	return mapBatchError(r.r.Scan(dest...))
}

// translateTxOptions converts database.TxOptions into pgx.TxOptions.
func translateTxOptions(opts database.TxOptions) pgx.TxOptions {
	txOpts := pgx.TxOptions{}

	switch opts.IsoLevel {
	case database.Serializable:
		txOpts.IsoLevel = pgx.Serializable
	case database.RepeatableRead:
		txOpts.IsoLevel = pgx.RepeatableRead
	case database.ReadCommitted:
		txOpts.IsoLevel = pgx.ReadCommitted
	case database.ReadUncommitted:
		txOpts.IsoLevel = pgx.ReadUncommitted
	}

	switch opts.AccessMode {
	case database.ReadOnly:
		txOpts.AccessMode = pgx.ReadOnly
	case database.ReadWrite:
		txOpts.AccessMode = pgx.ReadWrite
	}

	switch opts.DeferrableMode {
	case database.Deferrable:
		txOpts.DeferrableMode = pgx.Deferrable
	case database.NotDeferrable:
		txOpts.DeferrableMode = pgx.NotDeferrable
	}

	return txOpts
}

// txConn wraps a pgx.Tx to satisfy the database.TxConn interface.
type txConn struct {
	tx pgx.Tx
}

// Exec delegates to the underlying pgx transaction.
func (c *txConn) Exec(ctx context.Context, sql string, arguments ...any) (database.Result, error) {
	tag, err := c.tx.Exec(ctx, sql, arguments...)
	if err != nil {
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("tx exec: %w", err)
	}
	return &result{tag: tag}, nil
}

// Query delegates to the underlying pgx transaction.
func (c *txConn) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := c.tx.Query(ctx, sql, args...)
	if err != nil {
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("tx query: %w", err)
	}
	return &rows{r: r}, nil
}

// QueryRow delegates to the underlying pgx transaction.
func (c *txConn) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{r: c.tx.QueryRow(ctx, sql, args...)}
}

// Commit commits the underlying pgx transaction. A failure here is mapped like
// any statement's: a DEFERRABLE constraint is checked at commit time, so its
// violation is reported by COMMIT, not by the statement that caused it.
func (c *txConn) Commit(ctx context.Context) error {
	if err := c.tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) {
			return fmt.Errorf("pgx commit: %w: %w", database.ErrTxDone, err)
		}
		if ok, mapped := mapErrorCtx(ctx, err); ok {
			return mapped
		}
		return fmt.Errorf("pgx commit: %w", err)
	}
	return nil
}

// Rollback rolls back the underlying pgx transaction. pgx closes its
// transaction whether or not the ROLLBACK succeeds, and on a failure drops the
// connection, which ends the transaction on the server. A transaction pgx had
// already closed, or a ROLLBACK that failed only because ctx has ended, is
// rolled back, so it reports success; any other failure is still reported,
// wrapped as database.ErrTxDone so the Tx closes.
func (c *txConn) Rollback(ctx context.Context) error {
	if err := c.tx.Rollback(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) || ctx.Err() != nil {
			return nil //nolint:nilerr // the transaction is rolled back, which is what Rollback reports
		}
		return fmt.Errorf("pgx rollback: %w: %w", database.ErrTxDone, err)
	}
	return nil
}

// result wraps pgconn.CommandTag to satisfy the database.Result interface.
type result struct {
	tag pgconn.CommandTag
}

// RowsAffected returns the number of rows affected by the command.
func (r *result) RowsAffected() (int64, error) {
	return r.tag.RowsAffected(), nil
}

// LastInsertId is not supported by PostgreSQL and always returns 0 with an error.
// Use RETURNING clauses instead.
func (r *result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("pgx: LastInsertId is not supported by PostgreSQL, use RETURNING")
}

// rows wraps pgx.Rows to satisfy the database.Rows interface.
type rows struct {
	r pgx.Rows
}

// Next advances to the next row.
//
// No release-on-exhaustion counterpart is needed here, unlike the stdlib
// adapter: pgx's baseRows.Next calls Close() unconditionally when iteration runs
// out, which drains the result reader through to ReadyForQuery and frees the
// connection. Err() stays valid afterwards — baseRows.Err reads a retained
// field.
func (r *rows) Next() bool {
	return r.r.Next()
}

// Scan reads the values from the current row into dest.
func (r *rows) Scan(dest ...any) error {
	if err := r.r.Scan(dest...); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("pgx scan: %w", err)
	}
	return nil
}

// Columns returns the column names of the result set.
func (r *rows) Columns() ([]string, error) {
	descs := r.r.FieldDescriptions()
	cols := make([]string, len(descs))
	for i, d := range descs {
		cols[i] = d.Name
	}
	return cols, nil
}

// Close closes the rows, releasing any held resources.
func (r *rows) Close() error {
	r.r.Close()
	return nil
}

// Err returns any error that occurred during iteration.
func (r *rows) Err() error {
	if err := r.r.Err(); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("pgx rows: %w", err)
	}
	return nil
}

// row wraps pgx.Row to satisfy the database.Row interface.
type row struct {
	r pgx.Row
}

// Scan reads the values from the row into dest.
func (r *row) Scan(dest ...any) error {
	if err := r.r.Scan(dest...); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("pgx scan: %w", err)
	}
	return nil
}
