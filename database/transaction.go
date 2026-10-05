package database

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Compile-time check that Tx implements Querier.
var _ Querier = (*Tx)(nil)

// TxIsoLevel represents a transaction isolation level.
type TxIsoLevel string

// Transaction isolation levels.
const (
	Serializable    TxIsoLevel = "serializable"
	RepeatableRead  TxIsoLevel = "repeatable read"
	ReadCommitted   TxIsoLevel = "read committed"
	ReadUncommitted TxIsoLevel = "read uncommitted"
)

// TxAccessMode represents a transaction access mode.
type TxAccessMode string

// Transaction access modes.
const (
	ReadWrite TxAccessMode = "read write"
	ReadOnly  TxAccessMode = "read only"
)

// TxDeferrableMode represents a transaction deferrable mode (PostgreSQL only).
type TxDeferrableMode string

// Transaction deferrable modes (PostgreSQL only).
const (
	Deferrable    TxDeferrableMode = "deferrable"
	NotDeferrable TxDeferrableMode = "not deferrable"
)

// TxOptions configures transaction behavior.
type TxOptions struct {
	IsoLevel       TxIsoLevel
	AccessMode     TxAccessMode
	DeferrableMode TxDeferrableMode
	Timeout        time.Duration
	// CallbackMode controls how OnCommit callbacks fire after a successful
	// root commit. Empty string defers to the adapter default (CallbackAsync).
	CallbackMode CallbackMode
}

type txKey struct{}

// FromContext extracts the transaction from the context.
// Returns nil if no transaction exists in the context.
//
// Statements issued through the returned Tx take its connection reservation
// like every other statement on the transaction, so concurrent use is safe;
// the one rule that remains the caller's applies here too — see Tx.
func FromContext(ctx context.Context) *Tx {
	tx, _ := ctx.Value(txKey{}).(*Tx)
	return tx
}

// InTransaction reports whether ctx carries an active, unclosed transaction.
//
// Generated read methods consult this before issuing a SELECT with a non-zero
// [sql.LockMode]: row locks are meaningless outside a transaction, so the
// generator emits a precondition guard that returns a sqlgen-flavoured error
// when the lock mode is set without a transaction in scope. See PRD §9.6a.
func InTransaction(ctx context.Context) bool {
	tx := FromContext(ctx)
	return tx != nil && !tx.IsClosed()
}

// Tx represents an active database transaction with savepoint nesting support.
//
// A Tx carries two locks with deliberately different scopes, and PRD §18.5
// records why they cannot be one.
//
// The sync.Mutex protects this struct's own state — closed, tearingDown, conn,
// callbacks, depth, savepointNames — so IsClosed, OnCommit and the savepoint
// bookkeeping are safe to reach from any goroutine. No acquisition of it spans
// a driver round trip: every method that issues a statement takes it to read
// the connection, releases it across the statement, and re-takes it afterwards
// to record what the statement changed. It has to — database.Conn reaches
// IsClosed() at the head of every generated read, so a struct lock held for the
// length of a network call parks every concurrent reader on it.
//
// connSem is the connection reservation: capacity 1, taken before a statement
// reaches the driver and released when the connection is genuinely free again.
// A Tx is pinned to one driver connection and a connection carries one
// statement at a time, so this is what makes concurrent use of a single txCtx
// safe and serialized on every dialect — rather than a data race and
// "conn busy" on pgx, "busy buffer" and a rollback that cannot run on MySQL,
// and an accident on SQLite. See database/reservation.go for the mechanism and
// the lock-order invariant.
//
// The one rule that remains the caller's: drain or close a result set before
// issuing the next statement on that txCtx. The reservation serializes
// statements; what it cannot do is end a wait whose waiter is its own holder.
// That includes the RELEASE SAVEPOINT or ROLLBACK TO SAVEPOINT a nested
// WithTransaction issues after its fn has left a result set open: savepoint
// statements wait for the reservation, and only a root teardown does not.
//
// Two shapes are still outside what the reservation covers, both stated in
// §18.5. Opening two savepoint *scopes* on one txCtx concurrently is not made
// safe — the reservation serializes statements, not scopes, and two interleaved
// pushes leave Tx's savepoint stack disagreeing with the server's. And a root
// Commit or Rollback proceeds *without* the reservation if the connection is
// pinned, because a teardown that cannot run strands a pooled connection for
// the life of the process.
//
// closed and tearingDown are separate on purpose. closed means the teardown
// completed, which is what PRD §18.4 specifies IsClosed to report; tearingDown
// means a root teardown is in flight and is what makes it at-most-once. A
// statement issued during that window is rejected — it cannot succeed whichever
// way the teardown lands — but IsClosed keeps telling the truth, so the §18.6
// hook shape (tx != nil && !tx.IsClosed() → defer) does not fire a side effect
// for a transaction that has not committed yet.
type Tx struct {
	mu             sync.Mutex
	conn           TxConn
	depth          int
	callbacks      [][]func(ctx context.Context) error
	closed         bool
	tearingDown    bool
	name           string
	savepointNames []string
	callbackMode   CallbackMode
	cancel         context.CancelFunc

	// connSem is the connection reservation. It is never read except to be
	// taken or released, which is why nothing on its path needs tx.mu.
	connSem chan struct{}
}

// NewTx creates a new transaction wrapper around the given connection.
// Driver adapters use this to construct a Tx after beginning a database transaction.
func NewTx(conn TxConn, name string, mode CallbackMode) *Tx {
	if mode == "" {
		mode = CallbackAsync
	}
	return &Tx{
		conn:         conn,
		name:         name,
		callbacks:    [][]func(ctx context.Context) error{nil},
		callbackMode: mode,
		connSem:      make(chan struct{}, 1),
	}
}

// IsClosed returns true if the transaction has been committed or rolled back.
//
// It reports the *completed* teardown, not one in flight: for the length of a
// root COMMIT or ROLLBACK's round trip it still returns false, and flips only
// once the driver has accepted it. A teardown the driver rejects leaves the
// transaction open and IsClosed reporting false, which is what it is.
//
// That matters because of what reads it. PRD §18.6 makes side effects defer on
// `tx != nil && !tx.IsClosed()`, so reporting true early would fire a cache
// invalidation or an event for a transaction that has not committed — §18.6's
// critical invariant, and on the failing path a transaction that never will.
// database.Conn, InTransaction and NewTransaction read it for the same reason
// and take the same answer. What keeps a root teardown at-most-once is the
// separate tearingDown claim, not this flag.
func (tx *Tx) IsClosed() bool {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.closed
}

// stateErrLocked reports why the transaction cannot issue a statement. It must
// be called with tx.mu held, and only when closed or tearingDown is set.
func (tx *Tx) stateErrLocked() error {
	if tx.closed {
		return fmt.Errorf("transaction %s is closed", tx.name)
	}
	return fmt.Errorf("transaction %s is being torn down", tx.name)
}

// OnCommit registers a callback to be executed after a successful root commit.
//
// Ordering guarantees (relied on by the event and cache systems):
//
//   - FIFO within a depth. Callbacks registered via successive OnCommit calls
//     at the same depth fire in registration order. TestOnCommitCallbackOrdering
//     is the regression gate.
//   - Order-preserving savepoint promotion. On RELEASE SAVEPOINT, the
//     savepoint's callbacks are appended to the parent depth's slice — parent
//     callbacks still fire before promoted ones.
//   - At-depth rollback discard. ROLLBACK (root) discards all depths; ROLLBACK
//     TO SAVEPOINT discards only the current depth; siblings and ancestors are
//     untouched.
//
// Callback context: in the default CallbackAsync mode the callback runs on a
// fresh context.Background(), so request-scoped metadata (tracing spans,
// deadlines, auth identity) must not be read from the ctx parameter — close
// over any needed data at registration time. CallbackSync propagates the
// original commit ctx at the cost of blocking the commit return.
//
// Failure handling: a failed callback is retried once before the mode-specific
// fallback (sync returns the error; async logs and continues to the next
// callback). Callbacks should be idempotent.
func (tx *Tx) OnCommit(fn func(ctx context.Context) error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.callbacks[tx.depth] = append(tx.callbacks[tx.depth], fn)
}

// Exec delegates to the underlying transaction connection, holding the
// connection reservation across the driver call. An Exec has no result set, so
// that is its whole busy window.
func (tx *Tx) Exec(ctx context.Context, sql string, arguments ...any) (Result, error) {
	tx.mu.Lock()
	if tx.closed || tx.tearingDown {
		err := tx.stateErrLocked()
		tx.mu.Unlock()
		return nil, err
	}
	conn := tx.conn
	tx.mu.Unlock()

	if err := tx.acquireConn(ctx); err != nil {
		return nil, fmt.Errorf("tx exec %s: %w", tx.name, err)
	}
	defer tx.releaseConn()

	res, err := conn.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, fmt.Errorf("tx exec %s: %w", tx.name, err)
	}
	return res, nil
}

// Query delegates to the underlying transaction connection, handing the
// connection reservation to the returned result set. The reservation is
// released when the set is drained or closed, whichever comes first.
func (tx *Tx) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	tx.mu.Lock()
	if tx.closed || tx.tearingDown {
		err := tx.stateErrLocked()
		tx.mu.Unlock()
		return nil, err
	}
	conn := tx.conn
	tx.mu.Unlock()

	if err := tx.acquireConn(ctx); err != nil {
		return nil, fmt.Errorf("tx query %s: %w", tx.name, err)
	}
	r, err := conn.Query(ctx, sql, args...)
	if err != nil {
		tx.releaseConn()
		return nil, fmt.Errorf("tx query %s: %w", tx.name, err)
	}
	return &txRows{Rows: r, release: sync.OnceFunc(tx.releaseConn)}, nil
}

// QueryRow delegates to the underlying transaction connection, holding the
// connection reservation until the end of Scan — both adapters execute the
// statement eagerly here and consume the row there.
//
// QueryRow has no error return, so a transaction that cannot issue the
// statement comes back as a Row whose Scan reports why.
func (tx *Tx) QueryRow(ctx context.Context, sql string, args ...any) Row {
	tx.mu.Lock()
	if tx.closed || tx.tearingDown {
		err := tx.stateErrLocked()
		tx.mu.Unlock()
		return &errRow{err: err}
	}
	conn := tx.conn
	tx.mu.Unlock()

	if err := tx.acquireConn(ctx); err != nil {
		return &errRow{err: fmt.Errorf("tx query row %s: %w", tx.name, err)}
	}
	return &txRow{row: conn.QueryRow(ctx, sql, args...), release: sync.OnceFunc(tx.releaseConn)}
}

// Begin creates a savepoint within the current transaction.
// This enables transparent nesting — the caller does not need to know
// whether they are at the root or within a savepoint.
// Transaction options are ignored for savepoints — they inherit the parent's settings.
//
// A savepoint statement participates in the connection reservation and blocks
// for it, unlike a root teardown: it runs while the enclosing fn is still live,
// so a nested WithTx issued against a txCtx another goroutine is mid-read on
// waits for that read rather than failing (PRD §18.5). Every generated
// …WithRelated mutation opens a savepoint, so this is a first-class path.
func (tx *Tx) Begin(ctx context.Context, name string, _ ...TxOptions) (*Tx, error) {
	tx.mu.Lock()
	if tx.closed || tx.tearingDown {
		err := tx.stateErrLocked()
		tx.mu.Unlock()
		return nil, err
	}
	if !isSafeSavepointName(name) {
		tx.mu.Unlock()
		return nil, fmt.Errorf("database: savepoint name %q must match [A-Za-z_][A-Za-z0-9_]* and not shadow a SQL reserved word (received from WithTransaction/NewTransaction/WithTx at %s)", name, tx.name)
	}
	conn := tx.conn
	tx.mu.Unlock()

	if err := tx.acquireConn(ctx); err != nil {
		return nil, fmt.Errorf("creating savepoint %s: %w", name, err)
	}
	// Held across the bookkeeping below, not just the statement. If the
	// reservation were given back in between, two goroutines could exec their
	// SAVEPOINTs in one order and push their names in the other, leaving Tx's
	// stack disagreeing with the server's. tx.mu is taken only in short windows
	// nested inside it — the lock order is connSem → tx.mu, never the reverse.
	defer tx.releaseConn()

	// The statement runs outside tx.mu — see the two-locks note on Tx.
	if _, err := conn.Exec(ctx, fmt.Sprintf("SAVEPOINT %s", name)); err != nil {
		return nil, fmt.Errorf("creating savepoint %s: %w", name, err)
	}

	tx.mu.Lock()
	if tx.closed || tx.tearingDown {
		// A root teardown landed while the SAVEPOINT was in flight — it does
		// not take the reservation, so holding it is no protection here.
		// Recording the frame anyway would leave a closed Tx at depth > 0 with
		// a savepoint name nothing can release, so report what the check at the
		// top would have. Re-checking here is what keeps Begin atomic against
		// teardown now that the mutex no longer spans the statement.
		err := tx.stateErrLocked()
		tx.mu.Unlock()
		return nil, err
	}
	tx.depth++
	tx.callbacks = append(tx.callbacks, nil)
	tx.savepointNames = append(tx.savepointNames, name)
	tx.mu.Unlock()
	return tx, nil
}

// savepointReservedWords is a conservative cross-dialect reserved-word set:
// each token is reserved in at least one of postgres, mysql, sqlite as a bare
// identifier and is plausible as a human-readable transaction name. The
// database layer is driver-agnostic — it cannot QuoteIdentifier per dialect —
// so callers must pick names that are safe everywhere.
var savepointReservedWords = map[string]struct{}{
	"inner": {}, "outer": {}, "select": {}, "from": {}, "where": {},
	"order": {}, "group": {}, "having": {}, "join": {}, "on": {},
	"as": {}, "and": {}, "or": {}, "not": {}, "null": {},
	"true": {}, "false": {},
}

// isSafeSavepointName reports whether name is safe to interpolate as a bare
// SAVEPOINT identifier across postgres, mysql, and sqlite. The grammar accepted
// is [A-Za-z_][A-Za-z0-9_]*; reserved words from savepointReservedWords are
// rejected case-insensitively.
func isSafeSavepointName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	_, reserved := savepointReservedWords[strings.ToLower(name)]
	return !reserved
}

// NewTransaction begins a new transaction or creates a savepoint if a transaction
// already exists in the context. The transaction is injected into the returned context.
// At most one TxOptions value is accepted; passing more than one returns an error.
func NewTransaction(ctx context.Context, querier Querier, name string, opts ...TxOptions) (context.Context, error) {
	if len(opts) > 1 {
		return ctx, fmt.Errorf("database: at most one TxOptions may be passed to NewTransaction")
	}

	// If a transaction already exists and is open, create a savepoint
	if tx := FromContext(ctx); tx != nil && !tx.IsClosed() {
		if _, err := tx.Begin(ctx, name); err != nil {
			return ctx, err
		}
		return ctx, nil
	}

	var txOpts TxOptions
	if len(opts) == 1 {
		txOpts = opts[0]
	}

	// Apply timeout if set
	var cancel context.CancelFunc
	if txOpts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, txOpts.Timeout)
	}

	tx, err := querier.Begin(ctx, name, opts...)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return ctx, fmt.Errorf("beginning transaction %s: %w", name, err)
	}

	tx.mu.Lock()
	tx.cancel = cancel
	tx.mu.Unlock()

	return context.WithValue(ctx, txKey{}, tx), nil
}

// Commit commits the current transaction or releases the current savepoint.
// At root: issues COMMIT and fires registered callbacks.
// At savepoint: issues RELEASE SAVEPOINT and promotes callbacks to the parent depth.
func Commit(ctx context.Context) error {
	tx := FromContext(ctx)
	if tx == nil {
		return fmt.Errorf("no transaction in context")
	}

	tx.mu.Lock()

	if tx.closed || tx.tearingDown {
		err := tx.teardownStateErrLocked()
		tx.mu.Unlock()
		return err
	}

	if tx.depth > 0 {
		name := tx.savepointNames[tx.depth-1]
		conn := tx.conn
		tx.mu.Unlock()

		// A savepoint statement blocks for the reservation; the teardown
		// exemption below applies at the root only. Commit cannot tell
		// WithTransaction's own unwind, whose fn has returned, from a caller's
		// Commit racing another goroutine's live read, where it has not — and
		// a try-acquire would fail the second with the driver's busy error on
		// a path every …WithRelated mutation reaches. The cost is that a
		// nested scope which leaks a result set and then unwinds waits on
		// itself, bounded only by ctx (PRD §18.5, "Savepoints").
		if err := tx.acquireConn(ctx); err != nil {
			return fmt.Errorf("releasing savepoint %s: %w", name, err)
		}
		// Held across the bookkeeping for the reason Begin states.
		defer tx.releaseConn()

		// The statement runs outside tx.mu — see the two-locks note on Tx.
		if _, err := conn.Exec(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", name)); err != nil {
			return fmt.Errorf("releasing savepoint %s: %w", name, err)
		}

		tx.mu.Lock()
		// depth is re-read rather than carried across the statement, and the
		// guard is what keeps a concurrently-emptied stack from indexing out of
		// range. The reservation does not close this window: depth is read
		// above to choose the acquire mode, before there is anything to hold,
		// so a second goroutine already holding it can pop the frame while this
		// one waits. Two savepoint scopes opened on one Tx at the same time
		// already mismatch their names — PRD §18.5 carves that out as
		// unsupported and does not detect it — but it must not become a panic
		// in the consumer's process. When the guard is false the RELEASE
		// SAVEPOINT has already run, so this still reports success; it recorded
		// no bookkeeping because there was no frame left to pop.
		if tx.depth > 0 {
			// Promote callbacks to parent depth
			tx.callbacks[tx.depth-1] = append(tx.callbacks[tx.depth-1], tx.callbacks[tx.depth]...)
			tx.callbacks = tx.callbacks[:tx.depth]
			tx.savepointNames = tx.savepointNames[:tx.depth-1]
			tx.depth--
		}
		tx.mu.Unlock()
		return nil
	}

	// Root: COMMIT. tearingDown is claimed before the driver call and is what
	// serializes teardown once the mutex no longer spans it — a second Commit
	// (or a Rollback) racing this one is rejected here instead of reaching the
	// driver, where two teardowns on one connection would fire the OnCommit
	// callbacks twice and race the driver's own closed flag.
	//
	// closed stays false for the length of the round trip, because that is what
	// it means (PRD §18.4) and because §18.6's deferred side effects key off
	// IsClosed: flipping it early fires a cache invalidation or an event for a
	// transaction that has not committed yet, and on a teardown the driver
	// rejects, for one that never will.
	tx.tearingDown = true
	conn := tx.conn
	mode := tx.callbackMode
	tx.mu.Unlock()

	// Teardown never waits — see acquireForTeardown.
	release := tx.acquireForTeardown()
	err := conn.Commit(ctx)
	release()

	tx.mu.Lock()
	tx.tearingDown = false
	if err != nil {
		// The transaction is still open and still the caller's to roll back,
		// so nothing is claimed. Whether the driver still accepts a rollback is
		// the adapter's business: database/sql marks its Tx done before calling
		// out, and pgx returns ErrTxClosed.
		tx.mu.Unlock()
		return fmt.Errorf("committing transaction %s: %w", tx.name, err)
	}
	tx.closed = true
	// Snapshotted after the driver call, not before: a callback registered
	// during the round trip belongs to this commit, and taking the snapshot
	// under the same critical section that sets closed is what leaves no
	// interval where OnCommit appends to a slice nobody will read.
	callbacks := tx.callbacks[0]
	cancel := tx.cancel
	tx.mu.Unlock()

	// Fire callbacks outside the lock
	callbackErr := fireCallbacks(ctx, callbacks, mode)

	if cancel != nil {
		cancel()
	}

	return callbackErr
}

// teardownStateErrLocked reports why a teardown cannot proceed. It must be
// called with tx.mu held, and only when closed or tearingDown is set.
func (tx *Tx) teardownStateErrLocked() error {
	if tx.closed {
		return fmt.Errorf("transaction %s is already closed", tx.name)
	}
	return fmt.Errorf("transaction %s is already being torn down", tx.name)
}

// Rollback rolls back the current transaction or rolls back to the current savepoint.
// At root: issues ROLLBACK and discards all callbacks at all depths.
// At savepoint: issues ROLLBACK TO SAVEPOINT and discards callbacks at the current depth.
func Rollback(ctx context.Context) error {
	tx := FromContext(ctx)
	if tx == nil {
		return fmt.Errorf("no transaction in context")
	}

	tx.mu.Lock()

	if tx.closed || tx.tearingDown {
		err := tx.teardownStateErrLocked()
		tx.mu.Unlock()
		return err
	}

	if tx.depth > 0 {
		name := tx.savepointNames[tx.depth-1]
		conn := tx.conn
		tx.mu.Unlock()

		// Blocks for the reservation, for the reason Commit's savepoint path
		// states.
		if err := tx.acquireConn(ctx); err != nil {
			return fmt.Errorf("rolling back savepoint %s: %w", name, err)
		}
		defer tx.releaseConn()

		// The statement runs outside tx.mu — see the two-locks note on Tx.
		if _, err := conn.Exec(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)); err != nil {
			return fmt.Errorf("rolling back savepoint %s: %w", name, err)
		}

		tx.mu.Lock()
		// Guarded for the reason Commit's savepoint path states.
		if tx.depth > 0 {
			// Discard callbacks at this depth
			tx.callbacks = tx.callbacks[:tx.depth]
			tx.savepointNames = tx.savepointNames[:tx.depth-1]
			tx.depth--
		}
		tx.mu.Unlock()
		return nil
	}

	// Root: ROLLBACK — discard all callbacks. tearingDown is claimed before the
	// driver call, and closed flips only after it lands, for the reasons Commit
	// states.
	tx.tearingDown = true
	conn := tx.conn
	tx.mu.Unlock()

	// Teardown never waits — see acquireForTeardown.
	release := tx.acquireForTeardown()
	err := conn.Rollback(ctx)
	release()

	tx.mu.Lock()
	tx.tearingDown = false
	if err != nil {
		tx.mu.Unlock()
		return fmt.Errorf("rolling back transaction %s: %w", tx.name, err)
	}
	tx.closed = true
	cancel := tx.cancel
	tx.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	return nil
}

// WithTransaction runs fn within a transaction. If fn returns nil, the transaction
// is committed. If fn returns an error, the transaction is rolled back.
// Nesting is transparent — inner calls create savepoints automatically.
func WithTransaction(ctx context.Context, querier Querier, name string, fn func(context.Context) error, opts ...TxOptions) error {
	txCtx, err := NewTransaction(ctx, querier, name, opts...)
	if err != nil {
		return err
	}

	if err := fn(txCtx); err != nil {
		if rbErr := Rollback(txCtx); rbErr != nil {
			return fmt.Errorf("rollback failed: %w (original error: %w)", rbErr, err)
		}
		return err
	}

	return Commit(txCtx)
}

// Conn returns the active transaction from the context if one exists and is not closed.
// Otherwise, it returns the provided querier.
func Conn(ctx context.Context, querier Querier) Querier {
	if tx := FromContext(ctx); tx != nil && !tx.IsClosed() {
		return tx
	}
	return querier
}

// QueryFunc executes a query and passes the resulting rows to fn.
// Rows are automatically closed after fn returns.
//
// Inside a transaction the rows carry the connection reservation (see Tx), and
// the deferred Close is what gives it back — so the result set cannot outlive
// the call, and a caller reaching for a generated client's Querier(ctx) gets
// its hook-free access without being able to leak one (PRD §18.5). What the
// callback cannot prevent is breaking the one rule that remains the caller's
// from inside fn: drain or close a result set before issuing the next statement
// on that txCtx. A statement fn issues on the same transaction before the rows
// are drained waits on the reservation fn's own rows hold.
func QueryFunc(ctx context.Context, q Querier, sql string, args []any, fn func(Rows) error) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("query func: %w", err)
	}
	defer func() {
		_ = rows.Close() // best-effort close; errors are surfaced via rows.Err()
	}()
	if err := fn(rows); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration: %w", err)
	}
	return nil
}

// QueryRowFunc executes a single-row query and passes the resulting row to fn.
//
// Inside a transaction the row carries the connection reservation, which Scan
// gives back (see database/reservation.go). An fn that returns without scanning
// — an early return on a guard, say — would otherwise leave it held, so this
// releases after fn returns either way. The release is a sync.OnceFunc, so an
// fn that did scan has already made the deferred call a no-op.
//
// This is what makes QueryRowFunc the leak-proof spelling PRD §18.5 points
// consumers at when they reach for Querier(ctx) without wanting its sharp edge.
// As with QueryFunc, the rule still binds fn itself: a statement fn issues on
// the same transaction before it calls Scan waits on the reservation the row
// holds.
func QueryRowFunc(ctx context.Context, q Querier, sql string, args []any, fn func(Row) error) error {
	row := q.QueryRow(ctx, sql, args...)
	if tr, ok := row.(*txRow); ok {
		defer tr.release()
	}
	return fn(row)
}

func fireCallbacks(ctx context.Context, callbacks []func(ctx context.Context) error, mode CallbackMode) error {
	if len(callbacks) == 0 {
		return nil
	}

	switch mode {
	case CallbackSync:
		return fireCallbacksSync(ctx, callbacks)
	default:
		fireCallbacksAsync(callbacks)
		return nil
	}
}

func fireCallbacksAsync(callbacks []func(ctx context.Context) error) {
	go func() {
		ctx := context.Background()
		for _, fn := range callbacks {
			if err := fn(ctx); err != nil {
				// Retry once
				if retryErr := fn(ctx); retryErr != nil {
					log.Printf("database: callback failed after retry: %v", retryErr)
				}
			}
		}
	}()
}

func fireCallbacksSync(ctx context.Context, callbacks []func(ctx context.Context) error) error {
	for _, fn := range callbacks {
		if err := fn(ctx); err != nil {
			// Retry once
			if retryErr := fn(ctx); retryErr != nil {
				return retryErr
			}
		}
	}
	return nil
}
