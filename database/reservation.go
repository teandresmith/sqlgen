package database

import (
	"context"
	"fmt"
)

// The connection reservation.
//
// A Tx is pinned to one driver connection, and a connection carries one
// statement at a time. connSem is a capacity-1 channel on Tx that a statement
// takes before it reaches the driver and gives back when the connection is
// genuinely free again, so two goroutines sharing one txCtx serialize instead
// of racing. See PRD §18.5.
//
// It is a channel rather than a sync.Mutex because acquisition has to be
// selectable against ctx.Done(): a caller who leaks a result set and then
// issues another statement is waiting for itself, and only the context can end
// that wait. sync.Mutex.Lock ignores ctx.
//
// Two invariants hold this together, and both are load-bearing:
//
//   - Lock order is connSem → tx.mu, never the reverse. Nothing may wait for
//     connSem while holding tx.mu. database.Conn reaches IsClosed() — and so
//     tx.mu — at the head of every generated read, so a struct mutex held while
//     waiting for a result-set-scale lock parks every concurrent reader on it.
//     Begin, Commit and Rollback release tx.mu across their statements for
//     exactly this reason (see the two-locks note on Tx).
//   - Nothing on the reservation path takes tx.mu. connSem carries no
//     bookkeeping — no in-flight statement, no holding goroutine id, no third
//     mutex (PRD §18.5, "no goroutine-ownership tracking") — so there is
//     nothing for a second lock to guard and no way for the two to nest.

// acquireConn takes the connection reservation, waiting for the current holder
// if the connection is busy. The wait is bounded only by ctx: a caller that set
// no deadline and no TxOptions.Timeout waits indefinitely, which PRD §18.5
// records as the default case rather than an edge.
//
// The non-blocking select comes first on purpose and is not an optimization. A
// single select over both cases picks pseudo-randomly when both are ready, so a
// statement issued on an already-expired context would sometimes run and
// sometimes fail. Trying the uncontended case first makes it deterministic: if
// the connection is free, it is always taken.
func (tx *Tx) acquireConn(ctx context.Context) error {
	select {
	case tx.connSem <- struct{}{}:
		return nil
	default:
	}

	select {
	case tx.connSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		// Deliberately unnamed: every caller wraps this with the transaction
		// name or the savepoint's, and naming it here too reads as
		// "tx exec held: transaction held: waiting for …".
		return fmt.Errorf("waiting for the connection held by an open result set: %w", ctx.Err())
	}
}

// releaseConn gives the reservation back.
//
// The receive is unguarded — it would block forever on a Tx that holds nothing
// — because every path that reaches it has acquired first: Exec and the
// savepoint statements pair it with a defer, Query and QueryRow hand it to a
// sync.OnceFunc on the result set, and acquireForTeardown returns it only when
// its try-acquire succeeded.
func (tx *Tx) releaseConn() { <-tx.connSem }

// acquireForTeardown takes the reservation for a root COMMIT or ROLLBACK if it
// is free, and returns a no-op release if it is not — the teardown then runs
// without it and lets the driver report whatever it finds.
//
// Root teardown never waits (PRD §18.5). By the time WithTransaction reaches
// the root Commit or Rollback, fn has returned, so anything still holding the
// connection is a leaked result set rather than a statement in flight.
// Blocking would strand that pooled connection for the life of the process;
// proceeding surfaces the driver's own error ("conn busy" on pgx) in
// hundredths of a second, which is what the same leak produced before the
// reservation existed.
//
// This applies at the root only. A savepoint-depth statement takes the
// ordinary blocking acquire: Commit and Rollback cannot tell WithTransaction's
// own unwind, whose fn has returned, from a caller's Commit racing another
// goroutine's live read, where it has not, and failing the second with the
// driver's busy error would fail a generated …WithRelated mutation. See the
// savepoint path in Commit for what that costs.
func (tx *Tx) acquireForTeardown() func() {
	select {
	case tx.connSem <- struct{}{}:
		return tx.releaseConn
	default:
		return func() {}
	}
}

// txRows holds the connection reservation for the driver's real busy window:
// from Query until the result set is drained or closed, whichever comes first.
//
// Releasing on drain rather than on Close is what matches the drivers. pgx's
// baseRows.Next calls Close() unconditionally when iteration runs out, and
// database/sql's Rows.Next does the same at io.EOF — with one exception the
// stdlib adapter closes by hand (see database/stdlib's rows.Next). So by the
// time Next reports false the connection is genuinely free, and holding the
// reservation to Close would overshoot the driver by the whole scan.
//
// Err() stays valid after the release on both adapters: pgx's baseRows.Err
// reads a retained field, and database/sql's Rows.Err reads lasterr under
// closemu.
type txRows struct {
	Rows
	release func()
}

// Next advances to the next row, releasing the reservation when the result set
// runs out. Generated code is already written this way — for rows.Next() { … }
// and then the next statement — which is why the reservation needed no call
// site to change.
func (r *txRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	r.release()
	return false
}

// Close closes the result set and releases the reservation. release is a
// sync.OnceFunc, so closing a set that was already drained is a no-op — which
// generated code relies on, since it both defers Close and sometimes closes
// explicitly.
func (r *txRows) Close() error {
	defer r.release()
	//nolint:wrapcheck // one wrap per layer (guidelines/ERRORS.md): the
	// adapter's Close already carries its own context, and this type adds none.
	return r.Rows.Close()
}

// txRow holds the reservation from QueryRow until the end of Scan. Both
// adapters execute the statement eagerly at QueryRow — pgx's Conn.QueryRow
// calls Query immediately and returns a row over it — and consume the row in
// Scan, so that is the busy window.
//
// A Row has no Close, so Scan is the only thing that ends it: a caller who
// takes a Row from Querier(ctx) and discards it unscanned holds the connection
// for the life of the transaction. That is the QueryRow form of PRD §18.5's
// drain-or-close rule, and database.QueryRowFunc is the spelling that cannot
// get it wrong. Generated code always scans inline.
type txRow struct {
	row     Row
	release func()
}

// Scan reads the row and releases the reservation, whether or not the scan
// succeeded: either way the driver is done with the connection.
func (r *txRow) Scan(dest ...any) error {
	defer r.release()
	//nolint:wrapcheck // one wrap per layer (guidelines/ERRORS.md): both
	// adapters already map and wrap scan errors, and this type adds no context
	// of its own — it exists to release the reservation, not to describe the
	// failure.
	return r.row.Scan(dest...)
}

// errRow is what QueryRow returns when it cannot issue the statement at all —
// the transaction is closed or tearing down, or the reservation could not be
// taken before ctx expired. QueryRow has no error return, so the error is
// reported from Scan.
type errRow struct {
	err error
}

// Scan returns the error that prevented the query from being issued.
func (r *errRow) Scan(_ ...any) error { return r.err }

// compile-time checks that the wrappers satisfy the interfaces they stand in
// for. Both adapters implement exactly the five database.Rows methods, so the
// embedded interface erases nothing.
var (
	_ Rows = (*txRows)(nil)
	_ Row  = (*txRow)(nil)
	_ Row  = (*errRow)(nil)
)
