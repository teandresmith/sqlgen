# Phase 1: SQL Builder Functions

Status: Complete
PRD Sections: Appendix A, Appendix B, Section 8.7

## 1.1 Dialect Implementations — PostgreSQL, MySQL, SQLite

**PRD Reference:** Appendix B

**Status:** Complete

### Tasks

- [x] Implement `PostgresDialect` struct with pgx driver mode in `sql/postgres.go`
- [x] Implement stdlib driver mode via `NewPostgresStdlibDialect()` (single struct with `driver` field)
- [x] Implement `NewPostgresDialect()` and `NewPostgresStdlibDialect()` constructors
- [x] Implement `MySQLDialect` struct in `sql/mysql.go`
- [x] Implement `NewMySQLDialect()` constructor
- [x] Implement `SQLiteDialect` struct in `sql/sqlite.go`
- [x] Implement `NewSQLiteDialect()` constructor
- [x] Write table-driven dialect tests in `sql/postgres_test.go`, `sql/mysql_test.go`, `sql/sqlite_test.go`
- [x] Update `comparator/` tests to replace `testDialect` stubs with real dialect implementations and add SQLite dialect coverage

### Acceptance Criteria

- [x] `PostgresDialect` (pgx mode): `Placeholder(1)` → `$1`, `InCondition("col", 3)` → `"col" = ANY($)`
- [x] `PostgresDialect` (stdlib mode): `InCondition("col", 3)` → `"col" IN ($, $, $)`, `NotInCondition("col", 3)` → `"col" NOT IN ($, $, $)`
- [x] `MySQLDialect`: `Placeholder(1)` → `?`, `QuoteIdentifier("name")` → `` `name` ``, `SupportsReturning()` → `false`
- [x] `SQLiteDialect`: `Placeholder(1)` → `?`, `QuoteIdentifier("name")` → `"name"`, `SupportsReturning()` → `true`
- [x] `FormatTable` for PostgreSQL includes schema: `"public"."users"`; MySQL and SQLite ignore schema
- [x] `UpsertClause` for MySQL uses `ON DUPLICATE KEY UPDATE col = VALUES(col)` (no conflict target); PostgreSQL and SQLite use `ON CONFLICT (...) DO UPDATE SET col = excluded.col`
- [x] `ReturningClause` for MySQL returns empty string
- [x] PostgresDialect has unexported `driver` field — not stateless, but immutable after construction
- [x] All methods are deterministic and pure
- [x] All dialect types satisfy `sql.Dialect` interface (compile-time assertion)
- [x] Package has zero external dependencies (stdlib only)
- [x] Comparator tests no longer use `testDialect` stubs; all four real dialects are covered

### Tests Required

- [x] `Placeholder()`: PostgreSQL returns `$N`, MySQL returns `?`, SQLite returns `?`
- [x] `PlaceholderList()`: PostgreSQL returns `$1, $2, $3`, MySQL returns `?, ?, ?`, SQLite returns `?, ?, ?`
- [x] `QuoteIdentifier()`: PostgreSQL returns `"name"`, MySQL returns `` `name` ``, SQLite returns `"name"`
- [x] `FormatTable()`: PostgreSQL with schema → `"public"."users"`, PostgreSQL without schema → `"users"`, MySQL → `` `users` ``, SQLite → `"users"`
- [x] `SupportsReturning()`: PostgreSQL → `true`, MySQL → `false`, SQLite → `true`
- [x] `ReturningClause()`: PostgreSQL/SQLite produce `RETURNING "col1", "col2"`, MySQL returns `""`
- [x] `UpsertClause()`: PostgreSQL/SQLite produce `ON CONFLICT (...) DO UPDATE SET`, MySQL produces `ON DUPLICATE KEY UPDATE`
- [x] `InCondition()`: PostgreSQL+pgx → `= ANY($)`, PostgreSQL+stdlib → `IN ($, $, $)`, MySQL → `IN ($, $, $)`, SQLite → `IN ($, $, $)`
- [x] `NotInCondition()`: PostgreSQL+pgx → `!= ALL($)`, PostgreSQL+stdlib → `NOT IN ($, $, $)`, MySQL → `NOT IN ($, $, $)`, SQLite → `NOT IN ($, $, $)`
- [x] Compile-time `var _ Dialect = PostgresDialect{}` assertions for all dialect types
- [x] Comparator test suites pass with real dialect implementations (no stubs)

### Completion Record

**Date:** 2026-04-07

**Files created:**
- `sql/postgres.go` — `PostgresDialect` with `DriverMode` (pgx/stdlib), `DriverPgx`/`DriverStdlib` constants, `NewPostgresDialect()`, `NewPostgresStdlibDialect()`, `SupportsArrayParams()`
- `sql/mysql.go` — `MySQLDialect`, `NewMySQLDialect()`
- `sql/sqlite.go` — `SQLiteDialect`, `NewSQLiteDialect()`
- `sql/postgres_test.go` — table-driven tests for all PostgreSQL dialect methods (pgx + stdlib)
- `sql/mysql_test.go` — table-driven tests for all MySQL dialect methods
- `sql/sqlite_test.go` — table-driven tests for all SQLite dialect methods

**Files modified:**
- `sql/dialect.go` — added `placeholderTokenList()` helper for `$` token generation
- `comparator/comparator.go` — replaced `d.Name() == "postgres"` check with `SupportsArrayParams()` interface detection
- `comparator/comparator_test.go` — replaced `testDialect` stubs with real dialect instances (pgx, stdlib, mysql, sqlite)
- `comparator/id_test.go` — added postgres stdlib + sqlite coverage to IN/NIN tests
- `comparator/string_test.go` — added postgres stdlib + sqlite coverage to IN test
- `comparator/number_test.go` — added postgres stdlib + sqlite coverage to IN test
- `comparator/time_test.go` — added postgres stdlib + sqlite coverage to IN test
- `comparator/enum_test.go` — added postgres stdlib + sqlite coverage to IN test
- `omittable/omittable.go` — wrapped `json.Marshal`/`json.Unmarshal` errors to fix `wrapcheck` lint
- `guidelines/SQL.md` — updated dialect implementation rule: "immutable after construction" instead of "stateless — no fields"

**Notes:**
- Used single `PostgresDialect` struct with unexported `driver DriverMode` field instead of two separate types. Both constructors return the same type; only `InCondition`/`NotInCondition` and `SupportsArrayParams()` differ.
- `InCondition`/`NotInCondition` use `$` placeholder tokens (not positional `$1`/`?`). The builder (Phase 1.2) resolves `$` tokens to dialect-specific placeholders at the correct position, consistent with how `Condition.Clause` works.
- Comparator IN/NIN detection changed from `d.Name() == "postgres"` to `SupportsArrayParams()` interface check. This ensures PostgreSQL+stdlib correctly gets expanded IN instead of ANY.

---

## 1.2 Builder Functions — SQL Statement Builders

**PRD Reference:** Appendix A, Section 8.7

**Status:** Complete

### Tasks

- [x] Define option structs for builder functions in `sql/builder.go`:
  - `SelectOptions` — `Columns []string`, `Conditions []Condition`, `OrderBy []Sort`, `Limit *int`, `Offset *int`
  - `InsertOptions` — `Columns []string`, `Values []any`, `ReturningColumns []string`, `UpsertConflictKeys []string`, `UpsertUpdateColumns []string`
  - `MultiInsertOptions` — `Columns []string`, `ValueRows [][]any`, `ReturningColumns []string`
  - `UpdateOptions` — `SetClauses map[string]any`, `Conditions []Condition`, `ReturningColumns []string`
  - `SoftDeleteOptions` — `SoftDeleteColumn string`, `SoftDeleteType string` (timestamp, bool, integer)
- [x] Implement `BuildSelect(d Dialect, t Table, opts SelectOptions) (string, []any)`
  - Column selection (or `*` when empty)
  - WHERE clause from conditions
  - ORDER BY from sort list
  - LIMIT and OFFSET
- [x] Implement `BuildSelectJoin(d Dialect, t Table, joins []JoinClause, opts SelectOptions) (string, []any)`
  - Define `JoinClause` struct: `Table Table`, `Alias string`, `On string`, `Columns []string`, `SoftDeleteColumn string`
  - O2O LEFT JOIN with aliased columns (`"alias.column"` pattern)
  - Soft delete conditions on joined tables
  - Support for chained O2O JOINs (multiple LEFT JOINs)
- [x] Implement `BuildInsert(d Dialect, t Table, opts InsertOptions) (string, []any)`
  - Single row INSERT INTO ... VALUES (...)
  - Optional RETURNING clause (dialect-dependent)
  - Optional upsert via `UpsertClause()` when conflict keys provided
- [x] Implement `BuildMultiInsert(d Dialect, t Table, opts MultiInsertOptions) (string, []any)`
  - Multi-row VALUES: `(...), (...), ...`
  - Correct placeholder numbering across rows
  - Optional RETURNING clause
- [x] Implement `BuildUpdate(d Dialect, t Table, opts UpdateOptions) (string, []any)`
  - SET clause from key-value pairs (sorted columns for determinism)
  - WHERE clause from conditions
  - Optional RETURNING clause
- [x] Implement `BuildCount(d Dialect, t Table, conditions []Condition) (string, []any)`
  - `SELECT COUNT(*) FROM {table} WHERE ...`
- [x] Implement `BuildExists(d Dialect, t Table, conditions []Condition) (string, []any)`
  - `SELECT EXISTS(SELECT 1 FROM {table} WHERE ...)`
- [x] Implement `BuildIncrement(d Dialect, t Table, column string, amount any, conditions []Condition) (string, []any)`
  - `UPDATE {table} SET col = col + $N WHERE ...`
- [x] Implement `BuildSoftDelete(d Dialect, t Table, opts SoftDeleteOptions) (string, []any)`
  - Timestamp: `SET deleted_at = CURRENT_TIMESTAMP`
  - Bool: `SET is_deleted = TRUE`
  - Integer: `SET deleted = 1`
- [x] Implement `BuildHardDelete(d Dialect, t Table, conditions []Condition) (string, []any)`
  - `DELETE FROM {table} WHERE ...`
- [x] Implement `BuildColumnsFromFieldOptions(fieldOptionsMap map[string]string) []string`
  - Resolves selected columns from field option mappings
- [x] Implement internal `buildWhere` helper for expanding `[]Condition` into WHERE clause
  - Handle `$` placeholder replacement with dialect-specific placeholders
  - Handle `And`/`Or` composition with parenthesization
  - Handle `Range` values (BETWEEN with two placeholders)
  - Handle `IN`/`NOT IN` via `Dialect.InCondition()`/`NotInCondition()`
  - Handle `IS NULL`/`IS NOT NULL` (no placeholder)
  - Handle `Subquery` values
  - Track placeholder position across all conditions
- [x] Implement composite PK WHERE clause generation (Section 8.7)
  - Single row: `WHERE "col1" = $1 AND "col2" = $2`
  - Batch PostgreSQL: tuple IN — `WHERE ("col1", "col2") IN (($1, $2), ($3, $4))`
  - Batch MySQL: expanded OR — `WHERE ("col1" = ? AND "col2" = ?) OR ("col1" = ? AND "col2" = ?)`
- [x] Write table-driven builder tests in `sql/builder_test.go`

### Acceptance Criteria

- Every builder returns `(string, []any)` — SQL string and argument slice
- All values are parameterized via `Dialect.Placeholder()` — no string interpolation
- All identifiers are quoted via `QuoteIdentifier()` for columns, `FormatTable()` for tables
- Column lists in SELECT, INSERT, and SET clauses are sorted for deterministic output
- RETURNING clause is appended only when `Dialect.SupportsReturning()` returns `true`
- IN/NOT IN conditions use `Dialect.InCondition()` / `Dialect.NotInCondition()`
- `BuildSelect` with empty conditions produces no WHERE clause
- `BuildSelect` with schema-qualified table produces correct `FormatTable()` output
- `BuildUpdate` handles `omittable.Value[T]` fields correctly (set values included, unset values excluded)
- `BuildSoftDelete` produces correct SQL for all three column types (timestamp, bool, integer)
- Composite PK: single-row WHERE uses AND; batch uses tuple IN (PostgreSQL) or expanded OR (MySQL)
- All builders tested across all three dialects with correct SQL output verified
- Empty IN list does not produce invalid SQL
- Zero-length batch in `BuildMultiInsert` does not produce invalid SQL
- `BuildSelectJoin` produces aliased columns using `"alias.column"` quoted pattern
- Package has zero external dependencies (stdlib only, `cmp` allowed in test files)

### Tests Required

- [x] `BuildSelect`: basic select with columns, conditions, order, limit, offset — all three dialects
- [x] `BuildSelect`: empty conditions → no WHERE clause
- [x] `BuildSelect`: schema-qualified table name (PostgreSQL `"public"."users"`)
- [x] `BuildSelect`: no columns → `SELECT *`
- [x] `BuildSelectJoin`: single O2O JOIN with aliased columns — all three dialects
- [x] `BuildSelectJoin`: chained O2O JOINs (multiple LEFT JOINs)
- [x] `BuildSelectJoin`: soft delete condition on joined table
- [x] `BuildInsert`: single row insert — all three dialects
- [x] `BuildInsert`: insert with RETURNING (PostgreSQL/SQLite get clause, MySQL gets none)
- [x] `BuildInsert`: insert with upsert (conflict keys + update columns) — all three dialects
- [x] `BuildMultiInsert`: multi-row insert with correct placeholder numbering — all three dialects
- [x] `BuildMultiInsert`: multi-row with RETURNING
- [x] `BuildMultiInsert`: zero-length batch → no invalid SQL
- [x] `BuildUpdate`: basic update with SET and conditions — all three dialects
- [x] `BuildUpdate`: update with RETURNING
- [x] `BuildUpdate`: sorted column order in SET clause
- [x] `BuildCount`: basic count with conditions — all three dialects
- [x] `BuildCount`: empty conditions → count all rows
- [x] `BuildExists`: basic exists check — all three dialects
- [x] `BuildIncrement`: increment column by amount — all three dialects
- [x] `BuildSoftDelete`: timestamp column type → `SET deleted_at = CURRENT_TIMESTAMP`
- [x] `BuildSoftDelete`: bool column type → `SET is_deleted = TRUE`
- [x] `BuildSoftDelete`: integer column type → `SET deleted = 1`
- [x] `BuildHardDelete`: basic delete with conditions — all three dialects
- [x] WHERE clause: `And` composition → `(cond1 AND cond2)`
- [x] WHERE clause: `Or` composition → `(cond1 OR cond2)`
- [x] WHERE clause: nested `And`/`Or`
- [x] WHERE clause: `Between` → `col BETWEEN $1 AND $2`
- [x] WHERE clause: `IN` with PostgreSQL+pgx → `= ANY($1)`, expanded for others
- [x] WHERE clause: `IS NULL` / `IS NOT NULL` (no placeholder consumed)
- [x] WHERE clause: empty IN list handling
- [x] Composite PK: single row WHERE — `"col1" = $1 AND "col2" = $2` (PostgreSQL)
- [x] Composite PK: batch tuple IN — `("col1", "col2") IN (($1, $2), ($3, $4))` (PostgreSQL)
- [x] Composite PK: batch expanded OR — `("col1" = ? AND "col2" = ?) OR (...)` (MySQL)
- [x] `BuildColumnsFromFieldOptions`: resolves column names from field option map

### Completion Record

**Date:** 2026-04-08

**Files created:**
- `sql/builder.go` — Option structs (`SelectOptions`, `InsertOptions`, `MultiInsertOptions`, `UpdateOptions`, `SoftDeleteOptions`, `JoinClause`), all builder functions (`BuildSelect`, `BuildSelectJoin`, `BuildInsert`, `BuildMultiInsert`, `BuildUpdate`, `BuildCount`, `BuildExists`, `BuildIncrement`, `BuildSoftDelete`, `BuildHardDelete`, `BuildColumnsFromFieldOptions`), composite PK helpers (`BuildCompositePKConditions`, `BuildCompositePKBatchCondition`), internal WHERE clause engine (`buildWhere`, `expandCondition`, `expandComposition`, `expandRange`, `expandSubquery`, `expandSlice`, `expandIN`, `replaceDollars`)
- `sql/builder_test.go` — Comprehensive table-driven tests for all builders across all three dialects, WHERE clause expansion tests (And/Or/nested, Between, IN/NOT IN, IS NULL, Subquery, empty IN), composite PK tests (single row, batch tuple IN, batch expanded OR), placeholder numbering verification

**Files modified:**
- `sql/postgres.go` — Added `SupportsTupleIN() bool` method (returns true)
- `sql/sqlite.go` — Added `SupportsTupleIN() bool` method (returns true)
- `docs/PRD.md` — Updated soft delete timestamp SQL from `NOW()` to `CURRENT_TIMESTAMP`
- `guidelines/SQL.md` — Updated soft delete timestamp SQL from `NOW()` to `CURRENT_TIMESTAMP`

**Notes:**
- `SelectOptions` includes an `Alias` field for JOIN queries. For `BuildSelect` it is unused; for `BuildSelectJoin` it specifies the primary table alias.
- `buildWhere` resolves `$` placeholder tokens to dialect-specific positional placeholders via character-by-character scanning (avoids collision between PostgreSQL's `$N` format and the `$` token convention).
- IN/NOT IN expansion: `expandSlice` checks `" NOT IN $"` suffix before `" IN $"` to avoid false matches (since ` NOT IN $` also ends with ` IN $`).
- Empty IN list → `1 = 0` (always false). Empty NOT IN list → `1 = 1` (always true).
- Composite PK batch: uses `Dialect.SupportsTupleIN()` to select strategy — PostgreSQL/SQLite get tuple IN syntax, MySQL gets expanded OR via And/Or condition composition.
- `BuildSoftDelete` uses `CURRENT_TIMESTAMP` for timestamp type — standard SQL that works across all three dialects (PostgreSQL, MySQL, SQLite).
- Column lists in SELECT, INSERT, and SET clauses are sorted for deterministic output. Insert values are reordered to match sorted column order.
