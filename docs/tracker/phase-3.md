# Phase 3: Error System

Status: Complete
PRD Sections: 22, 22.2, 22.5

## 3.1 Error Types — Sentinel errors, ConstraintError, ConstraintType

**PRD Reference:** Section 22

**Status:** Complete

### Tasks

- [x] Define sentinel errors (`ErrNotFound`, `ErrEmptyFilter`, `ErrAmbiguousFilter`, `ErrInvalidCursor`, `ErrDeadlock`, `ErrConnectionFailed`, `ErrConstraintViolation`) in `database/errors.go`
- [x] Define `ConstraintType` type and constants (`ConstraintUnique`, `ConstraintForeignKey`, `ConstraintCheck`, `ConstraintNotNull`) in `database/errors.go`
- [x] Define `ConstraintError` struct with fields `Type`, `Constraint`, `Column`, `Detail`, `Err`
- [x] Implement `ConstraintError.Error() string` with formatted message
- [x] Implement `ConstraintError.Unwrap() error` returning `ErrConstraintViolation`
- [x] Write unit tests in `database/errors_test.go`

### Acceptance Criteria

- All seven sentinel errors are defined with exact string values from PRD Section 22.1
- `ConstraintType` is a `string` type with four constants: `"unique"`, `"foreign_key"`, `"check"`, `"not_null"`
- `ConstraintError` implements the `error` interface
- `errors.Is(constraintErr, ErrConstraintViolation)` returns `true` via `Unwrap()`
- `errors.As(err, &ConstraintError{})` extracts all fields correctly
- `ConstraintError.Error()` produces a readable, formatted message
- Package has zero external dependencies (stdlib only)

### Tests Required

- [x] `errors.Is(constraintErr, ErrConstraintViolation)` returns true
- [x] `errors.As(err, &ce)` extracts `ConstraintError` with correct fields
- [x] Each sentinel error has the exact string from PRD (e.g., `"sqlgen: resource not found"`)
- [x] `ConstraintError.Error()` formatting is consistent and includes type, constraint name, and detail
- [x] All four `ConstraintType` constants have correct string values
- [x] Wrapped `ConstraintError` preserves the full `errors.Is`/`errors.As` chain

### Completion Record

- `database/errors.go` — sentinel errors, ConstraintType, ConstraintError
- `database/errors_test.go` — 8 test functions covering all acceptance criteria
- Completed: 2026-04-08
- Verified: 15/15 PRD requirements, 6/6 tests, 0 guideline issues

---

## 3.2 Driver Error Mapping — Translate driver errors to ConstraintError/sentinels

**PRD Reference:** Section 22.2, 22.5

**Status:** Complete

### Tasks

- [x] Implement `MapError(err error) error` in `database/pgx/errors.go` — maps `*pgconn.PgError` to `ConstraintError` or sentinels
- [x] pgx code mapping: `23505`→Unique, `23503`→FK, `23502`→NotNull, `23514`→Check, `40P01`→`ErrDeadlock`, `08*`→`ErrConnectionFailed`
- [x] Wire `MapError` into pgx driver adapter (wrap query/exec errors through the mapper)
- [x] Implement `mapPqError` in `database/stdlib/errors_postgres.go` — maps `*pq.Error` using same PostgreSQL codes
- [x] Implement `mapMySQLError` in `database/stdlib/errors_mysql.go` — maps `*mysql.MySQLError` by error number
- [x] MySQL message parsers: `parseMySQLConstraint(msg)` and `parseMySQLColumn(msg)` for codes `1062`, `1451`/`1452`, `1048`
- [x] MySQL code mapping: `1062`→Unique, `1451`/`1452`→FK, `1048`→NotNull, `3819`→Check, `1213`→`ErrDeadlock`, `2002`/`2006`/`1040`→`ErrConnectionFailed`
- [x] Implement `mapModerncSQLiteError` in `database/stdlib/errors_sqlite.go` — maps `*sqlite.Error` by extended code
- [ ] ~~Implement `mapMattnSQLiteError`~~ — skipped: mattn/go-sqlite3 requires CGO; errors pass through unmapped
- [x] SQLite message parsers: `parseSQLiteConstraint(msg)` and `parseSQLiteColumn(msg)`
- [x] SQLite code mapping: `2067`→Unique, `787`→FK, `1299`→NotNull, `275`→Check, `1555`→Unique (PK)
- [x] Implement top-level `MapError(err error) error` in `database/stdlib/errors.go` that dispatches to the correct driver-specific mapper
- [x] Wire `MapError` into stdlib driver adapter (wrap query/exec errors through the mapper)
- [x] Ensure unrecognized driver errors are returned unwrapped (no misclassification)
- [x] Write unit tests for each driver mapper
- [x] Write unit tests for MySQL/SQLite message parsers (valid and unrecognized formats)

### Acceptance Criteria

- pgx: `*pgconn.PgError` codes map to correct `ConstraintType` with all structured fields populated (`Constraint`, `Column`, `Detail`)
- lib/pq: `*pq.Error` codes map identically to pgx (same PostgreSQL codes, same field extraction)
- MySQL: `*mysql.MySQLError` numbers map to correct `ConstraintType`; constraint and column names are parsed from `Message`
- SQLite (modernc): `*sqlite.Error` extended codes map correctly; constraint and column parsed from message
- mattn/go-sqlite3 is not supported (requires CGO); errors pass through unmapped
- MySQL/SQLite parsers fall back gracefully on unrecognized message formats — `Constraint` and `Column` left empty, `Type` and `Detail` still set
- `ErrDeadlock` returned for PostgreSQL `40P01` and MySQL `1213`; not for SQLite
- `ErrConnectionFailed` returned for PostgreSQL `08*` class, MySQL `2002`/`2006`/`1040`, SQLite `5`/`14`
- Unrecognized driver errors returned as-is (not wrapped in a sentinel)
- No driver types leak beyond `database/pgx/` and `database/stdlib/`
- Driver adapters automatically map errors from query/exec operations

### Tests Required

- [x] Unit: pgx — each code (`23505`, `23503`, `23502`, `23514`) maps to correct `ConstraintType` with all fields
- [x] Unit: pgx — `40P01` maps to `ErrDeadlock`
- [x] Unit: pgx — `08006` (and other `08*`) maps to `ErrConnectionFailed`
- [x] Unit: pgx — unrecognized code returns original error unwrapped
- [x] Unit: lib/pq — same code mappings as pgx, verified independently
- [x] Unit: MySQL — each number (`1062`, `1451`, `1452`, `1048`, `3819`) maps to correct `ConstraintType`
- [x] Unit: MySQL — `1213` maps to `ErrDeadlock`
- [x] Unit: MySQL — `2002`, `2006`, `1040` map to `ErrConnectionFailed`
- [x] Unit: MySQL — message parsing extracts constraint and column names for `1062`, `1451`/`1452`, `1048`
- [x] Unit: MySQL — unrecognized message format leaves `Constraint` and `Column` empty
- [x] Unit: SQLite (modernc) — each extended code (`2067`, `787`, `1299`, `275`, `1555`) maps correctly
- [x] Unit: SQLite (modernc) — message parsing extracts constraint and column
- [ ] ~~Unit: SQLite (mattn)~~ — skipped: mattn/go-sqlite3 not supported (CGO dependency)
- [x] Unit: SQLite — unrecognized message format falls back gracefully
- [x] Integration: trigger real UNIQUE violation → `ConstraintError{Type: ConstraintUnique}`
- [x] Integration: trigger real FK violation → `ConstraintError{Type: ConstraintForeignKey}`
- [x] Integration: trigger real NOT NULL violation → `ConstraintError{Type: ConstraintNotNull}`
- [x] Integration: trigger real CHECK violation → `ConstraintError{Type: ConstraintCheck}`
- [x] Integration: concurrent deadlock (PostgreSQL/MySQL) → `ErrDeadlock`

### Completion Record

- `database/pgx/errors.go` — pgx MapError: PgError → ConstraintError/sentinels
- `database/pgx/errors_test.go` — 8 test functions covering all pgx codes and edge cases
- `database/pgx/pgx.go` — wired MapError into all error paths (Exec, Query, Scan, txConn)
- `database/stdlib/errors.go` — top-level MapError dispatch across all drivers
- `database/stdlib/errors_postgres.go` — lib/pq error mapping (same PostgreSQL codes as pgx)
- `database/stdlib/errors_mysql.go` — MySQL error mapping + message parsers
- `database/stdlib/errors_sqlite.go` — modernc (interface-based) SQLite mapping + parsers
- `database/stdlib/errors_test.go` — 20 test functions covering all drivers, parsers, and dispatch
- `database/stdlib/stdlib.go` — added row/rows wrappers, wired MapError into all error paths
- `database/pgx/testdata/schema.sql` — added test_orders table, CHECK constraint
- `database/stdlib/testdata/mysql_schema.sql` — added test_orders table, CHECK constraint
- `database/stdlib/testdata/sqlite_schema.sql` — added test_orders table, CHECK constraint
- `database/pgx/pgx_integration_test.go` — 5 integration tests (UNIQUE, FK, NOT NULL, CHECK, deadlock)
- `database/stdlib/stdlib_integration_test.go` — 5 integration tests (UNIQUE, FK, NOT NULL, CHECK, deadlock)
- Completed: 2026-04-08
- Notes: mattn/go-sqlite3 not supported — errors pass through unmapped. modernc.org/sqlite is the supported SQLite driver (CGO-free).
