// Package suite is the cross-dialect conformance suite for the generated client
// (docs/design/CONFORMANCE.md §4). Each dialect module generates the same
// schema into a package at the module path this suite imports, replaces that
// path with its own generated code, and calls Run with a Harness wired to its
// database. Every subtest name is a behavior the requirement registry
// (docs/conformance/client.md) claims, so each one is a string literal.
package suite

import (
	"context"
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
	// Client is built on Querier with no options.
	Client *models.Client
	// Querier is the counting querier Client was built on. Statements inside a
	// transaction run on the transaction's connection and are not recorded.
	Querier *CountingQuerier
	// Reset empties every conformance table.
	Reset func(t *testing.T)
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
}

// CountingQuerier records every statement sent through it.
type CountingQuerier struct {
	Inner database.Querier

	mu    sync.Mutex
	stmts []string
}

func (q *CountingQuerier) record(sql string) {
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
	q.record(sql)
	return q.Inner.Exec(ctx, sql, args...)
}

func (q *CountingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	q.record(sql)
	return q.Inner.Query(ctx, sql, args...)
}

func (q *CountingQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	q.record(sql)
	return q.Inner.QueryRow(ctx, sql, args...)
}

func (q *CountingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return q.Inner.Begin(ctx, name, opts...)
}
