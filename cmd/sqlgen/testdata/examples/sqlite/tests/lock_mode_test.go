package tests

// LockMode E2E (SQLite).
//
// PRD §9.6a "SQLite limitation": SQLite has no per-row locking — its
// concurrency model uses whole-database write locks via BEGIN IMMEDIATE /
// BEGIN EXCLUSIVE. Setting any LockMode other than LockNone against the
// SQLite dialect returns a sqlgen error at the runtime guard; no SQL is
// issued.
//
// This file pins that contract: every non-LockNone mode on Get / GetMany /
// Connection rejects immediately with the "LockMode is unsupported on sqlite
// dialect" error, with zero queries reaching the underlying *sql.DB. The
// counting wrapper makes the "no SQL roundtrip" assertion structural — a
// regression that emitted FOR UPDATE on SQLite would tick the counter.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	sqlpkg "github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// countingSqliteQuerier wraps the stdlib sqlite querier and counts every
// Query / QueryRow / Exec call. The SQLite-rejection tests pin the counter
// at zero — the runtime guard must short-circuit before any DB roundtrip.
type countingSqliteQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64
}

func newCountingSqliteQuerier(inner database.Querier) *countingSqliteQuerier {
	return &countingSqliteQuerier{inner: inner}
}

func (c *countingSqliteQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.execs.Add(1)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *countingSqliteQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.queries.Add(1)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *countingSqliteQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.queryRow.Add(1)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *countingSqliteQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingSqliteQuerier) totalDBOps() int64 {
	return c.queries.Load() + c.queryRow.Load() + c.execs.Load()
}

func (c *countingSqliteQuerier) reset() {
	c.queries.Store(0)
	c.queryRow.Store(0)
	c.execs.Store(0)
}

// TestLockMode_SQLiteRejectsAllNonNoneModes covers Get / GetMany / Connection
// across every non-LockNone mode. The runtime guard must reject every
// combination with the sqlite-specific error string and issue zero SQL.
//
// Inside-transaction is exercised too: even when database.InTransaction is
// true, the SQLite guard arm still rejects (it short-circuits BEFORE the
// transaction precondition path). This is the PRD §9.6a contract — SQLite
// does not support row locks at all, regardless of tx state.
func TestLockMode_SQLiteRejectsAllNonNoneModes(t *testing.T) {
	ctx := context.Background()

	counter := newCountingSqliteQuerier(dbstdlib.New(testDB))
	client := models.New(counter)

	// Seed via a separate plain client so the rejection tests below have a
	// known PK to exercise. Reset the counter before each subtest so only
	// the guarded call contributes.
	seedClient := newClient()
	seed, err := seedClient.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-sqlite-seed",
	})
	if err != nil {
		t.Fatalf("Create seed: %v", err)
	}
	t.Cleanup(func() { _ = seedClient.Categories().HardDelete(ctx, seed.ID) })

	const wantSubstr = "LockMode is unsupported on sqlite dialect"

	modes := []sqlpkg.LockMode{
		sqlpkg.LockForUpdate,
		sqlpkg.LockForShare,
		sqlpkg.LockForUpdateNoWait,
		sqlpkg.LockForUpdateSkipLocked,
	}

	for _, mode := range modes {
		t.Run("Get/"+mode.String()+"/outside_tx", func(t *testing.T) {
			counter.reset()
			_, err := client.Categories().Get(ctx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = mode
			})
			assertSQLiteRejection(t, mode, err, counter.totalDBOps(), wantSubstr)
		})

		t.Run("Get/"+mode.String()+"/inside_tx", func(t *testing.T) {
			counter.reset()
			txErr := client.WithTx(ctx, "lock_sqlite_get", func(txCtx context.Context) error {
				_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
					o.LockMode = mode
				})
				return err
			})
			// Inside a tx, the guard error is returned through WithTx's
			// rollback path — the BEGIN itself does count as a DB op, so
			// we only check that the inner Get issued no QUERY.
			assertSQLiteRejectionInTx(t, mode, txErr, counter, wantSubstr)
		})

		t.Run("GetMany/"+mode.String()+"/outside_tx", func(t *testing.T) {
			counter.reset()
			_, err := client.Categories().GetMany(ctx, &models.GetCategoriesInput{}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = mode
			})
			assertSQLiteRejection(t, mode, err, counter.totalDBOps(), wantSubstr)
		})

		t.Run("Connection/"+mode.String()+"/outside_tx", func(t *testing.T) {
			counter.reset()
			pageSize := 10
			_, err := client.Categories().Connection(ctx, models.ConnectionInput[models.CategoryFilter]{
				First: &pageSize,
			}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = mode
			})
			assertSQLiteRejection(t, mode, err, counter.totalDBOps(), wantSubstr)
		})
	}
}

// assertSQLiteRejection pins the rejection contract: the guard must return
// an error containing the SQLite-specific phrase, and zero DB ops must have
// been issued — proving the rejection happened in the runtime guard, not
// in the dialect's BuildSelect or in the database itself.
func assertSQLiteRejection(t *testing.T, mode sqlpkg.LockMode, err error, dbOps int64, wantSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("LockMode=%s on SQLite: err = nil, want %q", mode, wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("LockMode=%s on SQLite: err = %q, want substring %q", mode, err.Error(), wantSubstr)
	}
	if dbOps != 0 {
		t.Errorf("LockMode=%s on SQLite: db ops = %d, want 0 (guard must short-circuit before SQL)", mode, dbOps)
	}
}

// assertSQLiteRejectionInTx is the in-tx variant: BEGIN/ROLLBACK do count
// against the counter, so we only assert that the inner SELECT was NOT
// issued. We confirm that by checking the counter is below the threshold
// for "BEGIN + SELECT + ROLLBACK". A tx that actually issued the SELECT
// would have queries >= 1; the rejected tx has queries = 0 (only execs for
// BEGIN + ROLLBACK).
func assertSQLiteRejectionInTx(t *testing.T, mode sqlpkg.LockMode, err error, counter *countingSqliteQuerier, wantSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("LockMode=%s in tx on SQLite: err = nil, want %q", mode, wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("LockMode=%s in tx on SQLite: err = %q, want substring %q", mode, err.Error(), wantSubstr)
	}
	// The Get's SELECT would route through Query / QueryRow on the tx
	// querier. Both must remain at zero — the guard must reject before the
	// SQL.
	if got := counter.queries.Load() + counter.queryRow.Load(); got != 0 {
		t.Errorf("LockMode=%s in tx on SQLite: SELECT-class ops = %d, want 0 (guard must short-circuit before SQL)", mode, got)
	}
}
