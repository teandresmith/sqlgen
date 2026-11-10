// Package suite is the cross-dialect conformance suite for the generated client
// (docs/design/CONFORMANCE.md §4). Each dialect module generates the same
// schema into a package at the module path this suite imports, replaces that
// path with its own generated code, and calls Run with a Harness wired to its
// database. Every subtest name is a behavior the requirement registry
// (docs/conformance/client.md) claims, so each one is a string literal.
package suite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teandresmith/sqlgen/database"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// Harness is the per-dialect wiring the suite runs against.
type Harness struct {
	// Dialect is postgres, mysql or sqlite; Driver is pgx or stdlib. They label
	// failures only: no test branches on them (CONFORMANCE.md §4.4).
	Dialect string
	Driver  string
	// Database names the server and its version, e.g. "PostgreSQL 16.4". Run
	// logs it for the conformance report.
	Database string
	// Client is built on Base with no options.
	Client *models.Client
	// Querier is the counting querier Client's statements are recorded on.
	// Statements inside a transaction run on the transaction's connection and
	// are not recorded.
	Querier *CountingQuerier
	// Base is the querier Client was built on: Querier itself, or a
	// driver-specific wrapper around it that records what it sends. A subtest
	// that builds a client of its own builds it on Base, so it takes the same
	// code paths Client does, unless it means to bypass Base's driver
	// capabilities.
	Base database.Querier
	// Caps are the dialect's capabilities. A subtest whose expectation differs
	// by dialect derives it from these, never from Dialect.
	Caps Capabilities
	// Reset empties every conformance table.
	Reset func(t *testing.T)
}

// Capabilities are the dialect differences the PRD states, by name.
type Capabilities struct {
	// RowLocks: a LockMode other than LockNone adds a row-lock clause to a
	// read inside a transaction. Without it, any such LockMode is an error.
	RowLocks bool
	// CompositeLoadChunk is how many keys of two columns one loader statement
	// binds (PRD §25.1).
	CompositeLoadChunk int
	// CaseInsensitiveText: the conformance schema's plain text columns compare
	// case-insensitively.
	CaseInsensitiveText bool
}

// Capability names one Capabilities field.
type Capability string

// CapRowLocks is Capabilities.RowLocks.
const CapRowLocks Capability = "RowLocks"

// CapCaseInsensitiveText is Capabilities.CaseInsensitiveText.
const CapCaseInsensitiveText Capability = "CaseInsensitiveText"

// Require skips t, naming the capability, when the dialect lacks it.
func (h *Harness) Require(t *testing.T, c Capability) {
	t.Helper()
	if !h.has(c) {
		t.Skipf("requires capability %s, which %s lacks", c, h.Dialect)
	}
}

// Lacks skips t, naming the capability, when the dialect has it: the subtest
// covers the dialects without it.
func (h *Harness) Lacks(t *testing.T, c Capability) {
	t.Helper()
	if h.has(c) {
		t.Skipf("covers dialects without capability %s, which %s has", c, h.Dialect)
	}
}

func (h *Harness) has(c Capability) bool {
	switch c {
	case CapRowLocks:
		return h.Caps.RowLocks
	case CapCaseInsensitiveText:
		return h.Caps.CaseInsensitiveText
	}
	panic("unknown capability " + string(c))
}

// Run runs every method contract against h, one method at a time.
func Run(t *testing.T, h *Harness) {
	// make conformance reads this line into the report header; keep its prefix.
	t.Logf("conformance harness: database %s", h.Database)
	t.Run("HardDeleteMany", func(t *testing.T) { hardDeleteMany(t, h) })
	t.Run("SoftDeleteMany", func(t *testing.T) { softDeleteMany(t, h) })
	t.Run("RestoreMany", func(t *testing.T) { restoreMany(t, h) })
	t.Run("SoftDelete", func(t *testing.T) { softDelete(t, h) })
	t.Run("Restore", func(t *testing.T) { restore(t, h) })
	t.Run("SoftDeleteWhere", func(t *testing.T) { softDeleteWhere(t, h) })
	t.Run("RestoreWhere", func(t *testing.T) { restoreWhere(t, h) })
	t.Run("HardDelete", func(t *testing.T) { hardDelete(t, h) })
	t.Run("HardDeleteWhere", func(t *testing.T) { hardDeleteWhere(t, h) })
	t.Run("Get", func(t *testing.T) { get(t, h) })
	t.Run("GetMany", func(t *testing.T) { getMany(t, h) })
	t.Run("Exists", func(t *testing.T) { exists(t, h) })
	t.Run("Count", func(t *testing.T) { count(t, h) })
	t.Run("ExistsWhere", func(t *testing.T) { existsWhere(t, h) })
	t.Run("Paginate", func(t *testing.T) { paginate(t, h) })
	t.Run("Connection", func(t *testing.T) { connection(t, h) })
	t.Run("Stream", func(t *testing.T) { stream(t, h) })
	t.Run("Create", func(t *testing.T) { create(t, h) })
	t.Run("CreateMany", func(t *testing.T) { createMany(t, h) })
	t.Run("Update", func(t *testing.T) { update(t, h) })
	t.Run("UpdateMany", func(t *testing.T) { updateMany(t, h) })
	t.Run("UpdateWhere", func(t *testing.T) { updateWhere(t, h) })
	t.Run("Increment", func(t *testing.T) { increment(t, h) })
	t.Run("Upsert", func(t *testing.T) { upsert(t, h) })
	t.Run("UpsertMany", func(t *testing.T) { upsertMany(t, h) })
	t.Run("CreateWithRelated", func(t *testing.T) { createWithRelated(t, h) })
	t.Run("UpdateWithRelated", func(t *testing.T) { updateWithRelated(t, h) })
	t.Run("UpsertWithRelated", func(t *testing.T) { upsertWithRelated(t, h) })
	t.Run("ErrorWrapping", func(t *testing.T) { errorWrapping(t, h) })
	t.Run("Concurrency", func(t *testing.T) { concurrency(t, h) })
}

// CountingQuerier records every statement sent through it.
type CountingQuerier struct {
	Inner database.Querier

	mu    sync.Mutex
	stmts []string
}

// Record records sql as sent. A driver-specific wrapper calls it for
// statements it sends without going through q, such as a pgx batch.
func (q *CountingQuerier) Record(sql string) {
	q.mu.Lock()
	q.stmts = append(q.stmts, sql)
	q.mu.Unlock()
}

// Statements returns the statements recorded since the last ResetCount.
func (q *CountingQuerier) Statements() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.stmts...)
}

// ResetCount clears the recorded statements.
func (q *CountingQuerier) ResetCount() {
	q.mu.Lock()
	q.stmts = nil
	q.mu.Unlock()
}

func (q *CountingQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	q.Record(sql)
	return q.Inner.Exec(ctx, sql, args...)
}

func (q *CountingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	q.Record(sql)
	return q.Inner.Query(ctx, sql, args...)
}

func (q *CountingQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	q.Record(sql)
	return q.Inner.QueryRow(ctx, sql, args...)
}

func (q *CountingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return q.Inner.Begin(ctx, name, opts...)
}

// WithSession forwards to Inner when Inner is a database.SessionPinner,
// recording the statements fn issues on the session.
func (q *CountingQuerier) WithSession(ctx context.Context, fn func(session database.Querier) error) error {
	pinner, ok := q.Inner.(database.SessionPinner)
	if !ok {
		return errors.New("conformance: the inner querier cannot pin a session")
	}
	return pinner.WithSession(ctx, func(session database.Querier) error {
		return fn(&sessionQuerier{Querier: session, q: q})
	})
}

// sessionQuerier records on q the statements sent on one pinned session.
type sessionQuerier struct {
	database.Querier
	q *CountingQuerier
}

func (s *sessionQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	s.q.Record(sql)
	return s.Querier.Exec(ctx, sql, args...)
}

func (s *sessionQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	s.q.Record(sql)
	return s.Querier.Query(ctx, sql, args...)
}

func (s *sessionQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	s.q.Record(sql)
	return s.Querier.QueryRow(ctx, sql, args...)
}
