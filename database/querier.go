package database

import "context"

// Result represents the result of a database operation that doesn't return rows.
type Result interface {
	RowsAffected() (int64, error)
	LastInsertId() (int64, error)
}

// Row represents a single row returned by a query.
type Row interface {
	Scan(dest ...any) error
}

// Rows represents the result set of a query.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Columns() ([]string, error)
	Close() error
	Err() error
}

// Querier defines the interface for executing database operations.
// Both connection pools and transactions implement this interface.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (Result, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Begin(ctx context.Context, name string, opts ...TxOptions) (*Tx, error)
}

// SessionPinner is implemented by a pooled Querier that can run several
// statements on one database session without opening a transaction.
//
// A pool hands each statement whichever session is idle, so per-session state
// — MySQL's auto_increment_increment and LAST_INSERT_ID among it — read by a
// second statement may describe a session the first one never ran on. A
// transaction pins a session too, but it also changes how the writes inside it
// commit, which is not a side effect a key lookup may have (PRD §19.1).
type SessionPinner interface {
	// WithSession calls fn with a Querier bound to a single session, held until
	// fn returns. Each statement fn issues commits on its own, exactly as it
	// would on the pool. fn must close any Rows and end any transaction it began
	// on the session before returning; the release waits for both. The session
	// Querier may itself be a SessionPinner or not, so fn must not pin again.
	WithSession(ctx context.Context, fn func(session Querier) error) error
}

// TxConn represents the underlying database transaction connection.
// Driver adapters wrap their native transaction types to implement this interface.
type TxConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (Result, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// CallbackMode controls how OnCommit callbacks are executed after a successful commit.
type CallbackMode string

const (
	// CallbackAsync fires callbacks in a separate goroutine with a detached context.
	// The caller does not wait for callbacks to complete.
	CallbackAsync CallbackMode = "async"

	// CallbackSync fires callbacks sequentially in the caller's context.
	// The first callback failure stops execution and propagates the error.
	CallbackSync CallbackMode = "sync"
)
