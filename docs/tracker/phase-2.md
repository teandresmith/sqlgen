# Phase 2: Database Abstraction Layer

Status: Complete
PRD Sections: 18, 19.1, 19.2, 19.3, 19.4

## 2.1 `database/` — Core Interfaces & Transaction Engine

**PRD Reference:** Section 18, Section 19.1

**Status:** Complete

### Tasks

- [x] Define `Result` interface — `RowsAffected() (int64, error)`, `LastInsertId() (int64, error)`
- [x] Define `Row` interface — `Scan(dest ...any) error`
- [x] Define `Rows` interface — `Next() bool`, `Scan(dest ...any) error`, `Columns() ([]string, error)`, `Close() error`, `Err() error`
- [x] Define `Querier` interface — `Exec`, `Query`, `QueryRow`, `Begin`
- [x] Define `TxIsoLevel`, `TxAccessMode`, `TxDeferrableMode` types and constants
- [x] Define `TxOptions` struct — `IsoLevel`, `AccessMode`, `DeferrableMode`, `Timeout`
- [x] Define `CallbackMode` type and constants (`CallbackAsync`, `CallbackSync`)
- [x] Implement `Tx` struct — `sync.Mutex` protected, fields: `depth int`, `callbacks [][]func(ctx context.Context) error`, `closed bool`, `name string`, underlying querier
- [x] Implement `Tx.IsClosed() bool`
- [x] Implement `Tx.OnCommit(fn func(ctx context.Context) error)` — appends to `callbacks[depth]`
- [x] Implement `Tx.Exec`, `Tx.Query`, `Tx.QueryRow`, `Tx.Begin` — delegate to underlying transaction, `Begin` creates a savepoint (satisfies `Querier`)
- [x] Implement `NewTransaction(ctx, querier, name, ...TxOptions) (context.Context, error)` — begin or savepoint, inject into context
- [x] Implement `FromContext(ctx) *Tx`
- [x] Implement `Commit(ctx) error` — root: COMMIT + fire callbacks (async/sync); savepoint: RELEASE + promote callbacks
- [x] Implement `Rollback(ctx) error` — root: ROLLBACK + discard all; savepoint: ROLLBACK TO + discard at depth
- [x] Implement `WithTransaction(ctx, querier, name, fn, ...TxOptions) error` — auto begin/commit/rollback wrapper
- [x] Implement `Conn(ctx, querier) Querier` — returns Tx if open, else original querier
- [x] Implement `QueryFunc(ctx, sql, args, fn)` — query + auto-close rows
- [x] Implement `QueryRowFunc(ctx, sql, args, fn)` — single row with callback
- [x] Implement callback execution: async mode (detached context, goroutine), sync mode (caller context, sequential)
- [x] Implement callback retry: retry once on failure, log on second failure, continue to next callback

### Acceptance Criteria

- `Querier` interface has exactly `Exec`, `Query`, `QueryRow`, `Begin` methods with correct signatures
- `Result`, `Row`, `Rows` interfaces match PRD §19.1
- `TxOptions` supports `IsoLevel`, `AccessMode`, `DeferrableMode`, `Timeout` — variadic, panics if >1 passed
- `NewTransaction` begins a real transaction when no Tx in context, creates a savepoint when Tx exists
- `FromContext` returns `*Tx` from context or nil
- `Commit` at root fires callbacks in registration order; at savepoint promotes callbacks to parent depth
- `Rollback` at root discards all callbacks; at savepoint discards callbacks at that depth only
- `WithTransaction` auto-commits on nil return, auto-rolls-back on error return
- `Conn(ctx, querier)` returns Tx when open and not closed, falls back to querier otherwise
- `IsClosed()` returns true after Commit or Rollback
- Async callbacks receive a detached context (`context.Background()` derived), not the transaction context
- Sync callback failure propagates as the return error, first failure stops remaining callbacks
- Callback retry: one immediate retry, log on second failure, continue to remaining callbacks
- All `Tx` operations are `sync.Mutex` protected and safe under `-race`
- Core package depends only on stdlib (`context`, `sync`, `fmt`, `log`)

### Tests Required

- [x] Transaction lifecycle: begin → operations → commit
- [x] Savepoint nesting: begin → savepoint → release → commit
- [x] Savepoint rollback: begin → savepoint → rollback to savepoint → commit (outer survives)
- [x] `OnCommit` callback ordering: registration order preserved
- [x] `OnCommit` promotion: savepoint release promotes callbacks to parent depth
- [x] `OnCommit` discard: savepoint rollback discards callbacks at that depth
- [x] Root rollback discards all callbacks at all depths
- [x] `Conn()` returns Tx when open, fallback when closed
- [x] `IsClosed()` after commit/rollback
- [x] Concurrent safety with `-race` flag
- [x] `WithTransaction` auto-commit on nil return
- [x] `WithTransaction` auto-rollback on error return
- [x] Nested `WithTransaction` creates savepoints transparently
- [x] `TxOptions` variadic: zero args uses defaults, one arg applies, >1 panics
- [x] Async callback mode: callbacks fire in goroutine, `WithTransaction` returns before callbacks complete
- [x] Sync callback mode: `WithTransaction` waits for callbacks, propagates first error
- [x] Callback retry on failure: retry once, log on second failure, continue to next
- [x] `QueryFunc` auto-closes rows after callback
- [x] `QueryRowFunc` single row with callback

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `database/querier.go`, `database/transaction.go`, `database/transaction_test.go`
- All 21 tasks complete, all 19 tests pass with `-race`
- Added `TxConn` interface for driver adapters to wrap raw transactions
- Added `NewTx` constructor for driver adapter use
- PRD compliance verified: 34/34 requirements passing

---

## 2.2 `database/pgx/` — pgx Driver Adapter

**PRD Reference:** Section 19.2, Section 19.4

**Status:** Complete

### Tasks

- [x] Implement `pgxAdapter` struct wrapping `*pgxpool.Pool`
- [x] Implement `New(pool *pgxpool.Pool) database.Querier` constructor
- [x] Implement `Exec(ctx, sql, args) (database.Result, error)` — wraps pgx result
- [x] Implement `Query(ctx, sql, args) (database.Rows, error)` — wraps pgx rows
- [x] Implement `QueryRow(ctx, sql, args) database.Row` — wraps pgx row
- [x] Implement `Begin(ctx, name, ...TxOptions) (*database.Tx, error)` — uses `pool.BeginTx(ctx)` with options
- [x] Implement pgx `Result` wrapper to satisfy `database.Result`
- [x] Implement pgx `Rows` wrapper to satisfy `database.Rows`
- [x] Implement pgx `Row` wrapper to satisfy `database.Row`
- [x] Implement `SendBatch(ctx, batch)` support for batch operations

### Acceptance Criteria

- `New(pool)` returns a `database.Querier` wrapping `*pgxpool.Pool`
- All `Querier` methods correctly delegate to the underlying pool
- `Begin` translates `TxOptions` into pgx-compatible options
- `SendBatch` pipelines multiple statements in a single round-trip
- pgx result/rows/row wrappers satisfy the `database.Result`/`database.Rows`/`database.Row` interfaces
- Package imports only `database/` and `jackc/pgx/v5` — no other internal packages

### Tests Required

- [x] Integration test with testcontainers (PostgreSQL): Exec, Query, QueryRow
- [x] Integration test: Begin → operations → Commit
- [x] Integration test: Begin → operations → Rollback
- [x] Integration test: SendBatch pipelines multiple statements
- [x] Verify all Querier methods are implemented correctly
- [x] Result wrapper returns correct RowsAffected

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `database/pgx/pgx.go`, `database/pgx/pgx_integration_test.go`, `database/pgx/testdata/schema.sql`
- All 10 tasks complete, all 7 integration tests pass with `-race`
- `txConn` adapter wraps `pgx.Tx` to satisfy `database.TxConn` interface
- `LastInsertId` returns an error (PostgreSQL uses RETURNING instead)
- `SendBatch` exposed as additional method beyond `Querier` interface

---

## 2.3 `database/stdlib/` — stdlib Driver Adapter

**PRD Reference:** Section 19.2, Section 19.4

**Status:** Complete

### Tasks

- [x] Implement `stdlibAdapter` struct wrapping `*sql.DB`
- [x] Implement `New(db *sql.DB) database.Querier` constructor
- [x] Implement `Exec(ctx, sql, args) (database.Result, error)` — wraps stdlib result
- [x] Implement `Query(ctx, sql, args) (database.Rows, error)` — wraps stdlib rows
- [x] Implement `QueryRow(ctx, sql, args) database.Row` — wraps stdlib row
- [x] Implement `Begin(ctx, name, ...TxOptions) (*database.Tx, error)` — uses `db.BeginTx(ctx, opts)`
- [x] Implement stdlib `Result` wrapper to satisfy `database.Result`
- [x] Implement stdlib `Rows` wrapper to satisfy `database.Rows`
- [x] Implement stdlib `Row` wrapper to satisfy `database.Row`
- [x] Implement savepoint support via raw SQL (`SAVEPOINT`, `RELEASE SAVEPOINT`, `ROLLBACK TO SAVEPOINT`)

### Acceptance Criteria

- `New(db)` returns a `database.Querier` wrapping `*sql.DB`
- All `Querier` methods correctly delegate to the underlying `*sql.DB`
- `Begin` translates `TxOptions` into `sql.TxOptions` for `db.BeginTx`
- Savepoints issued as raw SQL for MySQL and SQLite (stdlib has no savepoint API)
- Package imports only `database/` and `database/sql` — no other internal packages

### Tests Required

- [x] Integration test with testcontainers (MySQL): Exec, Query, QueryRow
- [x] Integration test with in-memory SQLite: Exec, Query, QueryRow
- [x] Integration test: Begin → operations → Commit (MySQL)
- [x] Integration test: Begin → operations → Commit (SQLite)
- [x] Savepoint SQL correctness per dialect (MySQL vs SQLite `ROLLBACK TRANSACTION TO SAVEPOINT`)
- [x] Verify all Querier methods are implemented correctly
- [x] Result wrapper returns correct RowsAffected and LastInsertId

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `database/stdlib/stdlib.go`, `database/stdlib/stdlib_integration_test.go`, `database/stdlib/testdata/mysql_schema.sql`, `database/stdlib/testdata/sqlite_schema.sql`
- **Files removed:** `database/stdlib/doc.go` (package comment moved to `stdlib.go`)
- All 10 tasks complete, all 8 integration tests pass with `-race` (MySQL via testcontainers, SQLite in-memory)
- No Result/Rows/Row wrappers needed — `database/sql` types satisfy `database.Result`/`database.Rows`/`database.Row` interfaces directly via structural typing
- `txConn` adapter wraps `*sql.Tx` to satisfy `database.TxConn` interface
- `DeferrableMode` ignored in `translateTxOptions` (PostgreSQL-specific, not in `sql.TxOptions`)
- Savepoints work via raw SQL through the `database.Tx` engine — verified against both MySQL and SQLite

---

## 2.4 `database/mock/` — Mock Driver

**PRD Reference:** Section 19.2

**Status:** Complete

### Tasks

- [x] Implement `MockQuerier` struct with configurable function fields: `ExecFn`, `QueryFn`, `QueryRowFn`, `BeginFn`
- [x] Implement `New() *MockQuerier` constructor
- [x] Implement `Exec` — calls `ExecFn` if set, returns default otherwise
- [x] Implement `Query` — calls `QueryFn` if set, returns default otherwise
- [x] Implement `QueryRow` — calls `QueryRowFn` if set, returns default otherwise
- [x] Implement `Begin` — calls `BeginFn` if set, returns default otherwise
- [x] Implement `MockResult` with configurable `RowsAffectedFn`, `LastInsertIdFn`
- [x] Implement `MockRow` with configurable `ScanFn`
- [x] Implement `MockRows` with configurable `NextFn`, `ScanFn`, `ColumnsFn`, `CloseFn`, `ErrFn`

### Acceptance Criteria

- `New()` returns a `*MockQuerier` that satisfies `database.Querier`
- Each method calls its corresponding function field if set
- Default behavior (nil functions) returns reasonable zero values without panicking
- `MockResult`, `MockRow`, `MockRows` satisfy `database.Result`, `database.Row`, `database.Rows`
- Package imports only `database/` — no external dependencies

### Tests Required

- [x] Verify configurable ExecFn is called with correct arguments
- [x] Verify configurable QueryFn is called with correct arguments
- [x] Verify configurable QueryRowFn is called with correct arguments
- [x] Verify configurable BeginFn is called with correct arguments
- [x] Default behavior (nil functions) returns zero values without error
- [x] MockResult returns configured RowsAffected and LastInsertId
- [x] MockRows iterates correctly with configured NextFn and ScanFn

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `database/mock/mock.go`, `database/mock/mock_test.go`
- **Files removed:** `database/mock/doc.go` (package comment moved to `mock.go`)
- All 9 tasks complete, all 11 tests pass with `-race`
- Types named `Querier`, `Result`, `Row`, `Rows` (no stuttering per Go guidelines)
- `NewRow(values...)` convenience constructor for values-based scanning
- `Row.Values` field uses `reflect` for generic scan-to-pointer (stdlib only)
- Compile-time interface checks for all four types
