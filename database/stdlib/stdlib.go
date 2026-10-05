// Package stdlib provides a database/sql adapter implementing the database.Querier interface.
package stdlib

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/teandresmith/sqlgen/database"
)

// Compile-time checks.
var (
	_ database.Querier       = (*stdlibAdapter)(nil)
	_ database.SessionPinner = (*stdlibAdapter)(nil)
	_ database.Querier       = (*sessionConn)(nil)
)

// stdlibAdapter wraps a *sql.DB to satisfy the database.Querier interface.
type stdlibAdapter struct {
	db *sql.DB
}

// New returns a database.Querier wrapping the given *sql.DB.
// This adapter works with any database/sql compatible driver (MySQL, SQLite, PostgreSQL via lib/pq, etc.).
func New(db *sql.DB) database.Querier {
	return &stdlibAdapter{db: db}
}

// Exec executes a query that doesn't return rows.
func (a *stdlibAdapter) Exec(ctx context.Context, query string, arguments ...any) (database.Result, error) {
	res, err := a.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("exec: %w", err)
	}
	return res, nil
}

// Query executes a query that returns rows.
func (a *stdlibAdapter) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	r, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("query: %w", err)
	}
	return &rows{r: r}, nil
}

// QueryRow executes a query that returns at most one row.
func (a *stdlibAdapter) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return &row{r: a.db.QueryRowContext(ctx, query, args...)}
}

// Begin starts a new database transaction and returns a *database.Tx.
// TxOptions are translated into sql.TxOptions. DeferrableMode is ignored
// as it is PostgreSQL-specific and not supported by database/sql.
func (a *stdlibAdapter) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return beginTx(ctx, a.db, name, opts)
}

// WithSession pins one connection from the pool for the duration of fn
// (database.SessionPinner). Statements fn issues run in autocommit mode on that
// connection, exactly as they would on the pool; nothing is begun or committed
// around them. The connection goes back to the pool when fn returns, so fn must
// close any Rows it opened and end any transaction it began on the session
// before it does: *sql.Conn.Close waits for both.
func (a *stdlibAdapter) WithSession(ctx context.Context, fn func(session database.Querier) error) (err error) {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("pinning session: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("releasing session: %w", closeErr)
		}
	}()
	return fn(&sessionConn{conn: conn})
}

// txBeginner is the BeginTx method *sql.DB and *sql.Conn share.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// beginTx starts a transaction on b and wraps it in a *database.Tx.
func beginTx(ctx context.Context, b txBeginner, name string, opts []database.TxOptions) (*database.Tx, error) {
	if len(opts) > 1 {
		return nil, fmt.Errorf("stdlib: at most one TxOptions may be passed to Begin")
	}

	var sqlOpts *sql.TxOptions
	var mode database.CallbackMode
	if len(opts) == 1 {
		sqlOpts = translateTxOptions(opts[0])
		mode = opts[0].CallbackMode
	}

	tx, err := b.BeginTx(ctx, sqlOpts)
	if err != nil {
		return nil, fmt.Errorf("beginning stdlib transaction: %w", err)
	}

	return database.NewTx(&txConn{tx: tx}, name, mode), nil
}

// translateTxOptions converts database.TxOptions into sql.TxOptions.
func translateTxOptions(opts database.TxOptions) *sql.TxOptions {
	txOpts := &sql.TxOptions{}

	switch opts.IsoLevel {
	case database.Serializable:
		txOpts.Isolation = sql.LevelSerializable
	case database.RepeatableRead:
		txOpts.Isolation = sql.LevelRepeatableRead
	case database.ReadCommitted:
		txOpts.Isolation = sql.LevelReadCommitted
	case database.ReadUncommitted:
		txOpts.Isolation = sql.LevelReadUncommitted
	}

	if opts.AccessMode == database.ReadOnly {
		txOpts.ReadOnly = true
	}

	return txOpts
}

// sessionConn wraps a *sql.Conn — one pinned session — to satisfy the
// database.Querier interface. It deliberately does not implement
// database.SessionPinner: it already is a single session.
type sessionConn struct {
	conn *sql.Conn
}

// Exec executes a query that doesn't return rows on the pinned session.
func (c *sessionConn) Exec(ctx context.Context, query string, arguments ...any) (database.Result, error) {
	res, err := c.conn.ExecContext(ctx, query, arguments...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("session exec: %w", err)
	}
	return res, nil
}

// Query executes a query that returns rows on the pinned session.
func (c *sessionConn) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	r, err := c.conn.QueryContext(ctx, query, args...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("session query: %w", err)
	}
	return &rows{r: r}, nil
}

// QueryRow executes a query that returns at most one row on the pinned session.
func (c *sessionConn) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return &row{r: c.conn.QueryRowContext(ctx, query, args...)}
}

// Begin starts a transaction on the pinned session.
func (c *sessionConn) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return beginTx(ctx, c.conn, name, opts)
}

// txConn wraps a *sql.Tx to satisfy the database.TxConn interface.
// Savepoints are supported via raw SQL issued by the database.Tx engine
// (SAVEPOINT, RELEASE SAVEPOINT, ROLLBACK TO SAVEPOINT), which works
// with both MySQL and SQLite.
type txConn struct {
	tx *sql.Tx
}

// Exec delegates to the underlying stdlib transaction.
func (c *txConn) Exec(ctx context.Context, query string, arguments ...any) (database.Result, error) {
	res, err := c.tx.ExecContext(ctx, query, arguments...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("tx exec: %w", err)
	}
	return res, nil
}

// Query delegates to the underlying stdlib transaction.
func (c *txConn) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	r, err := c.tx.QueryContext(ctx, query, args...)
	if err != nil {
		if ok, mapped := MapError(err); ok {
			return nil, mapped
		}
		return nil, fmt.Errorf("tx query: %w", err)
	}
	return &rows{r: r}, nil
}

// QueryRow delegates to the underlying stdlib transaction.
func (c *txConn) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return &row{r: c.tx.QueryRowContext(ctx, query, args...)}
}

// row wraps *sql.Row to provide error mapping through MapError.
type row struct {
	r *sql.Row
}

// Scan reads the values from the row into dest, mapping driver errors.
func (r *row) Scan(dest ...any) error {
	if err := r.r.Scan(dest...); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("stdlib scan: %w", err)
	}
	return nil
}

// rows wraps *sql.Rows to provide error mapping through MapError.
type rows struct {
	r *sql.Rows
}

// Next advances to the next row, closing the result set once it is exhausted so
// that the connection is released at the moment iteration reports false.
//
// database/sql does not always do that on its own. Rows.nextLocked returns
// doClose=false when the driver implements driver.RowsNextResultSet and
// HasNextResultSet() reports another set pending — so Next() reports the current
// set exhausted while the connection still carries the next one.
// go-sql-driver/mysql implements that interface, and a MySQL DSN with
// multiStatements=true is enough to reach it. The next statement on the
// transaction then meets a busy connection: "busy buffer", which destroys the
// connection so the rollback cannot run either.
//
// Nothing is lost by closing here: database.Rows exposes no NextResultSet, so a
// pending second result set was unreachable through this interface regardless —
// it only pinned the connection until Close. The pgx adapter needs no
// counterpart; pgx's baseRows.Next calls Close() unconditionally on exhaustion.
func (r *rows) Next() bool {
	if r.r.Next() {
		return true
	}
	_ = r.r.Close() // idempotent; iteration errors still surface through Err
	return false
}

// Scan reads the values from the current row into dest, mapping driver errors.
func (r *rows) Scan(dest ...any) error {
	if err := r.r.Scan(dest...); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("stdlib scan: %w", err)
	}
	return nil
}

// Columns returns the column names of the result set.
func (r *rows) Columns() ([]string, error) {
	cols, err := r.r.Columns()
	if err != nil {
		return nil, fmt.Errorf("stdlib columns: %w", err)
	}
	return cols, nil
}

// Close closes the rows, releasing any held resources.
func (r *rows) Close() error {
	if err := r.r.Close(); err != nil {
		return fmt.Errorf("stdlib close: %w", err)
	}
	return nil
}

// Err returns any error that occurred during iteration, mapping driver errors.
func (r *rows) Err() error {
	if err := r.r.Err(); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("stdlib rows: %w", err)
	}
	return nil
}

// Commit commits the underlying stdlib transaction. A failure here is mapped
// like any statement's: a constraint the database checks at commit time (a
// deferred SQLite foreign key) or a Galera certification failure (MySQL 1213)
// is reported by COMMIT, not by the statement that caused it.
func (c *txConn) Commit(ctx context.Context) error {
	if err := c.tx.Commit(); err != nil {
		if ok, mapped := MapError(err); ok {
			return mapped
		}
		return fmt.Errorf("stdlib commit: %w", err)
	}
	return nil
}

// Rollback rolls back the underlying stdlib transaction.
func (c *txConn) Rollback(ctx context.Context) error {
	if err := c.tx.Rollback(); err != nil {
		return fmt.Errorf("stdlib rollback: %w", err)
	}
	return nil
}
