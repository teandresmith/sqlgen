# SQLGen — Error Handling Standards

> Error taxonomy, driver error mapping, wrapping conventions, and generated code error behavior.
> Read this before writing or modifying error types, driver adapters, or generated client methods.

---

## Table of Contents

- [1. Principles](#1-principles)
- [2. Error Hierarchy](#2-error-hierarchy)
- [3. Sentinel Errors](#3-sentinel-errors)
- [4. ConstraintError](#4-constrainterror)
- [5. Driver Error Mapping](#5-driver-error-mapping)
- [6. Error Wrapping Convention](#6-error-wrapping-convention)
- [7. Generated Code Error Behavior](#7-generated-code-error-behavior)
- [8. Transaction Error Handling](#8-transaction-error-handling)
- [9. Panic Recovery](#9-panic-recovery)
- [10. Custom Error Packages](#10-custom-error-packages)
- [11. Linter Enforcement](#11-linter-enforcement)
- [12. Testing Errors](#12-testing-errors)

---

## 1. Principles

1. **Errors are part of the API.** Every sentinel error, structured error, and wrapping convention is a contract with consumers. Changing error behavior is a breaking change.
2. **Wrap with context, not noise.** Every error from generated code includes the operation and table name. Do not add redundant layers ("query failed: error executing SQL query").
3. **Use `errors.Is` and `errors.As`.** Never compare errors with `==`. The error wrapping chain must be preserved so consumers can inspect at any depth.
4. **Map driver errors at the boundary.** Driver-specific error extraction lives exclusively in `database/pgx/` and `database/stdlib/`. No driver types leak into generated code or the runtime core.
5. **Fail clearly or succeed silently.** Operations that find no matching rows are either errors (`ErrNotFound`) or idempotent (`nil`) depending on the operation. There is no ambiguity — the behavior is specified per method.

---

## 2. Error Hierarchy

```
error (stdlib interface)
├── ErrNotFound              ← sentinel, errors.Is()
├── ErrEmptyFilter           ← sentinel, errors.Is()
├── ErrNilInput              ← sentinel, errors.Is()
├── ErrAmbiguousFilter       ← sentinel, errors.Is()
├── ErrInvalidCursor         ← sentinel, errors.Is()
├── ErrDeadlock              ← sentinel, errors.Is()
├── ErrConnectionFailed      ← sentinel, errors.Is()
├── ErrConstraintViolation   ← sentinel, errors.Is()
│   └── *ConstraintError     ← structured, errors.As()
│       ├── Type: ConstraintUnique
│       ├── Type: ConstraintForeignKey
│       ├── Type: ConstraintCheck
│       └── Type: ConstraintNotNull
├── ErrRefreshConcurrentlyInTx  ← sentinel, errors.Is()
├── ErrAlreadyRelated        ← sentinel, errors.Is()
└── ErrNestedVerbConflict    ← sentinel, errors.Is()

*NestedMutationError         ← structured, errors.As()
    └── Unwrap() → Err, whichever sentinel this failure carries:
        ErrAlreadyRelated, ErrNestedVerbConflict, or ErrNotFound
        (a connect whose target is not visible — PRD §9.9.8)
```

`*NestedMutationError` hangs off no single sentinel, which is what separates it from `*ConstraintError`: the latter always unwraps to `ErrConstraintViolation`, while the former returns whatever the caller put in `Err`. Match the sentinel you care about with `errors.Is`; reach for `errors.As` only when you want the edge, verb or id.

Consumers check sentinels with `errors.Is()` for broad matching, then extract `*ConstraintError` with `errors.As()` when they need specifics.

---

## 3. Sentinel Errors

Eleven sentinel errors. All are checkable with `errors.Is()`.

| Error | String | When Returned |
|-------|--------|--------------|
| `ErrNotFound` | `"sqlgen: resource not found"` | `Get` finds no row. `Update` / `Increment` target a nonexistent PK (when `strict_updates: true`). |
| `ErrEmptyFilter` | `"sqlgen: empty filter on bulk operation"` | Any `*Where` operation (`UpdateWhere`, `SoftDeleteWhere`, `RestoreWhere`, `HardDeleteWhere`) called with nil or empty filter. Prevents accidental bulk operations. |
| `ErrNilInput` | `"sqlgen: nil input"` | A write is called with a nil input pointer — `Create`, `Update`, `Upsert`, `UpdateWhere`, or a nil element in a `CreateMany` / `UpdateMany` batch. Reads normalize a nil input to the zero value instead (PRD §9.4c). |
| `ErrAmbiguousFilter` | `"sqlgen: filter contains both And and Or at the same level"` | A filter mixes `And` and `Or` conditions at the same level, making intent unclear. |
| `ErrInvalidCursor` | `"sqlgen: invalid cursor"` | Cursor cannot be Base64-decoded or JSON-parsed, does not contain expected keys, or both `first` and `last` are provided. |
| `ErrDeadlock` | `"sqlgen: deadlock detected"` | Database detects a deadlock between concurrent transactions. Consumers should retry the transaction. |
| `ErrConnectionFailed` | `"sqlgen: connection failed"` | Connection-level failure: refused, reset, timeout, pool exhaustion. Wraps the underlying driver error. |
| `ErrConstraintViolation` | `"sqlgen: constraint violation"` | Base error for all constraint failures. Use `errors.As()` to extract the specific `*ConstraintError`. |
| `ErrRefreshConcurrentlyInTx` | `"sqlgen: REFRESH MATERIALIZED VIEW CONCURRENTLY cannot run inside a transaction"` | `RefreshConcurrently` on a materialized-view client is called with an ambient transaction in the context. PostgreSQL forbids it inside a transaction block, so the generated method fails fast before issuing any SQL. |
| `ErrAlreadyRelated` | `"sqlgen: already related to another row"` | A nested `connect` names a target already parented to a different row and the edge does not set `allow_reparent`, or a has-one `create` / `connect` finds the parent already holding a different child (PRD §9.9.6). Carried by `*NestedMutationError`, whose `ID` names the target in the first case and the child the edge already holds in the second. |
| `ErrNestedVerbConflict` | `"sqlgen: conflicting verbs on one nested relationship"` | One nested block names the same target under two verbs, or a self-referential edge names the parent's own primary key. Returned before any statement on the edge runs. Carried by `*NestedMutationError`. |

### When to Use Each

- **`ErrNotFound`** — Only for single-entity lookups where the caller expects a row to exist. Never for `GetMany` (which returns an empty slice) or `*Where` operations (which are idempotent).
- **`ErrEmptyFilter`** — Safety net for all `*Where` operations. An empty filter on `DeleteWhere` would delete every row; on `RestoreWhere` would restore every row. This forces the caller to be explicit.
- **`ErrNilInput`** — Only for writes. A read normalizes a nil input to the zero-value input, because "no criteria" is meaningful there; a write cannot, since a zero `Create<T>Input` legally writes a row (each required field's zero value, or with no required field, every column's default), so normalizing would turn a caller's mistake into a silently written one (PRD §9.4c).
- **`ErrDeadlock`** — Returned only when the driver error code is unambiguously a deadlock (PostgreSQL `40P01`, MySQL `1213`). Unrecognized errors are returned as-is from the driver.
- **`ErrConnectionFailed`** — Wraps the original driver error. Consumers can unwrap to inspect the root cause.

---

## 4. ConstraintError

The structured error type for database constraint violations.

```go
type ConstraintError struct {
    Type       ConstraintType // Unique, ForeignKey, Check, NotNull
    Constraint string         // constraint name (e.g., "products_name_key")
    Column     string         // affected column, if detectable
    Detail     string         // database-provided detail message
    Err        error          // original driver error (wrapped)
}

func (e *ConstraintError) Error() string   // formatted message
func (e *ConstraintError) Unwrap() error   // unwraps to ErrConstraintViolation
```

### NestedMutationError

The structured error for nested-mutation failures (PRD §22.2). Same pattern, one difference: `Unwrap` returns the sentinel the caller put in `Err` rather than a fixed one, because a nested failure can be any of three — the two new sentinels, or `ErrNotFound` for a `connect` whose target is not visible.

```go
type NestedMutationError struct {
    Edge string // relationship field name, e.g. "Categories"
    Verb string // "create", "connect", "disconnect", or "clear"
    ID   any    // the offending target id (for an occupied has-one, the child the edge already holds); nil when the failure is not id-scoped
    Err  error  // the sentinel this failure carries
}

func (e *NestedMutationError) Error() string   // "<edge>: <verb>[: <id>]: <cause>"
func (e *NestedMutationError) Unwrap() error   // unwraps to Err
```

`Edge` and `Verb` carry exactly the attribution the wrapped message already states, so a consumer — and the generated GraphQL error mapper — reads the failing relationship off the struct instead of parsing it back out of a string. Never substring-match the message to find the edge.

### ConstraintType Values

| Value | Constant | Triggered By |
|-------|----------|-------------|
| `"unique"` | `ConstraintUnique` | UNIQUE or PRIMARY KEY violation |
| `"foreign_key"` | `ConstraintForeignKey` | FOREIGN KEY violation (insert or delete) |
| `"check"` | `ConstraintCheck` | CHECK constraint violation |
| `"not_null"` | `ConstraintNotNull` | NOT NULL violation |

### Usage Pattern

```go
// Broad check — is it any constraint violation?
if errors.Is(err, database.ErrConstraintViolation) {
    // handle generically
}

// Specific extraction — which constraint, which column?
if ce, ok := errors.AsType[*database.ConstraintError](err); ok {
    switch ce.Type {
    case database.ConstraintUnique:
        // handle duplicate: ce.Constraint, ce.Column
    case database.ConstraintForeignKey:
        // handle missing reference
    case database.ConstraintCheck:
        // handle check violation
    case database.ConstraintNotNull:
        // handle null value
    }
}
```

### Field Population by Driver

Not all drivers expose the same metadata. Fields that cannot be extracted are left empty.

| Field | pgx / lib/pq | MySQL | SQLite |
|-------|:------------:|:-----:|:------:|
| `Type` | From SQLSTATE code | From error number | From extended code |
| `Constraint` | Structured field | Parsed from message | Parsed from message |
| `Column` | Structured field | Parsed from message | Parsed from message |
| `Detail` | Structured field | `Message` field | `Error()` string |

When message parsing fails (unexpected format), `Constraint` and `Column` are left empty. Only `Type` and `Detail` are guaranteed populated.

---

## 5. Driver Error Mapping

Each driver adapter (`database/pgx/`, `database/stdlib/`) translates driver-specific errors into the unified error hierarchy. No driver types leak beyond these packages. The mapping covers every statement the adapters run, and `Commit` (`Begin` and `Rollback` are not mapped). Some errors only COMMIT reports: a PostgreSQL `DEFERRABLE` constraint, a SQLite deferred foreign key, a Galera certification failure (MySQL `1213`). Unmapped, they would leave a transaction's caller unable to tell them apart from any other failure.

### Error Code Tables

**Constraint violations:**

| Error | PostgreSQL (SQLSTATE) | MySQL (Number) | SQLite (Extended Code) |
|-------|-----------------------|----------------|------------------------|
| Unique | `23505` | `1062` | `2067` |
| Foreign key | `23503` | `1451` / `1452` | `787` |
| Check | `23514` | `3819` | `275` |
| Not null | `23502` | `1048` | `1299` |
| Primary key | `23505` | `1062` | `1555` |

**Non-constraint errors:**

| Error | PostgreSQL | MySQL | SQLite |
|-------|-----------|-------|--------|
| Deadlock | `40P01` | `1213` | N/A (file-level locking) |
| Connection | `08*` class | `2002`, `2006`, `1040` | `5` (BUSY), `14` (CANTOPEN) |

### Driver Error Types

Each driver exposes errors as a specific Go type. The adapter uses `errors.As()` to extract the driver error, reads the code, and maps it to a `ConstraintError` or sentinel.

**pgx** — `*pgconn.PgError`:

```go
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) {
    // pgErr.Code         → SQLSTATE string ("23505")
    // pgErr.ConstraintName → "products_name_key"
    // pgErr.ColumnName    → "name"
    // pgErr.Detail        → "Key (name)=(Widget) already exists."
    // pgErr.TableName     → "products"
    // pgErr.SchemaName    → "public"
}
```

Richest metadata. All fields are first-class — no parsing required.

**lib/pq** — `*pq.Error`:

```go
var pqErr *pq.Error
if errors.As(err, &pqErr) {
    // pqErr.Code       → pq.ErrorCode ("23505")
    // pqErr.Constraint → "products_name_key"
    // pqErr.Column     → "name"
    // pqErr.Detail     → "Key (name)=(Widget) already exists."
}
```

Same PostgreSQL wire protocol as pgx — same metadata availability.

**go-sql-driver/mysql** — `*mysql.MySQLError`:

```go
var mysqlErr *mysql.MySQLError
if errors.As(err, &mysqlErr) {
    // mysqlErr.Number   → uint16 (1062)
    // mysqlErr.SQLState → string
    // mysqlErr.Message  → "Duplicate entry 'Widget' for key 'products.name'"
}
```

MySQL does not expose constraint name, table, or column as structured fields. They must be parsed from `Message`:

| Error Number | Message Format | Parsing Target |
|-------------|---------------|----------------|
| `1062` | `"Duplicate entry 'VALUE' for key 'TABLE.CONSTRAINT'"` | Constraint name after `for key` |
| `1451` / `1452` | `"... a foreign key constraint fails (SCHEMA.TABLE, CONSTRAINT ...)"` | Constraint in parentheses |
| `1048` | `"Column 'COLUMN' cannot be null"` | Column name in single quotes |

**modernc.org/sqlite** — `*sqlite.Error`:

```go
var sqliteErr *sqlite.Error
if errors.As(err, &sqliteErr) {
    // sqliteErr.Code() → int (2067)
}
```

Extended codes only. Constraint and column names come from the error message string. SQLite's text is `"UNIQUE constraint failed: users.email"`; modernc's `Error()` wraps it as `"constraint failed: UNIQUE constraint failed: users.email (2067)"`, so the parser skips the result-code prefix and strips the code suffix. Parsing is required.

**mattn/go-sqlite3** — Not supported. This driver requires CGO, which conflicts with the project's CGO-free runtime goal. Errors from mattn/go-sqlite3 pass through unmapped. Use modernc.org/sqlite for structured error mapping.

### Fallback Behavior

When a driver error is not recognized (unknown code, unexpected format):
- The original driver error is returned unwrapped.
- No sentinel error is applied.
- This ensures unknown errors are never silently misclassified.

---

## 6. Error Wrapping Convention

All errors from generated client methods are wrapped with operation context.

### Format

```
"{operation} {table}: {cause}"
```

### Examples

```go
// Client method errors
fmt.Errorf("get product: %w", err)
fmt.Errorf("create product: %w", err)
fmt.Errorf("update product: %w", err)
fmt.Errorf("get many products: %w", err)
fmt.Errorf("soft delete product: %w", err)
fmt.Errorf("hard delete many products: %w", err)

// Scan errors
fmt.Errorf("scan product: %w", err)
fmt.Errorf("scan product with company: %w", err)

// Post-scan conversion errors (custom types with convert config)
fmt.Errorf("parsing column custom_id: %w", err)
```

### Rules

1. **One wrap per layer.** Generated code wraps once with operation + table. The driver adapter wraps once when mapping to `ConstraintError`. Do not double-wrap.
2. **Always use `%w`.** Consumers need `errors.Is()` and `errors.As()` to work through the chain. Use `%v` only at true system boundaries where the underlying error should not be part of the API.
3. **Operation names are lowercase.** `"create product"`, not `"Create Product"` or `"CREATE product"`.
4. **Table names are singular.** `"create product"`, not `"create products"`. The table name matches the Go struct name convention.
5. **The `wrapcheck` linter enforces this.** Errors from external packages (pgx, database/sql) must be wrapped with context before returning.

---

## 7. Generated Code Error Behavior

Every generated method has specified error behavior. This table is the contract — do not deviate.

### Read Operations

| Method | Return Type | No Rows Found | Error Propagation |
|--------|------------|---------------|-------------------|
| `Get` | `(*Entity, error)` | `ErrNotFound` | Standard |
| `GetMany` | `([]*Entity, error)` | Empty slice, `nil` error | Includes filter, query, scan, and relationship loading errors |
| `Exists` | `(bool, error)` | `false, nil` | Standard |
| `ExistsWhere` | `(bool, error)` | `false, nil` | Standard |
| `Count` | `(int64, error)` | `0, nil` | Standard |

### Write Operations

| Method | Return Type | PK Not Found | Constraint Violation | Empty Filter |
|--------|------------|-------------|---------------------|-------------|
| `Create` | `(*Entity, error)` | N/A | `*ConstraintError` | N/A |
| `CreateMany` | `([]*Entity, error)` | N/A | `*ConstraintError` | N/A |
| `Update` | `(*Entity, error)` | `ErrNotFound` (strict) / `nil` | `*ConstraintError` | N/A |
| `UpdateMany` | `error` | Per-item | `*ConstraintError` | N/A |
| `UpdateWhere` | `error` | `nil` (idempotent) | `*ConstraintError` | `ErrEmptyFilter` |
| `Upsert` | `(*Entity, error)` | N/A (creates) | `*ConstraintError` | N/A |
| `Increment` | `error` | `ErrNotFound` (strict) / `nil` | N/A | N/A |

### Delete Operations

| Method | Return Type | PK Not Found | Empty Filter |
|--------|------------|-------------|-------------|
| `SoftDelete` | `error` | `nil` (idempotent) | N/A |
| `SoftDeleteMany` | `error` | `nil` (idempotent) | N/A |
| `SoftDeleteWhere` | `error` | `nil` (idempotent) | `ErrEmptyFilter` |
| `Restore` | `error` | `nil` (idempotent) | N/A |
| `RestoreMany` | `error` | `nil` (idempotent) | N/A |
| `RestoreWhere` | `error` | `nil` (idempotent) | `ErrEmptyFilter` |
| `HardDelete` | `error` | `nil` (idempotent) | N/A |
| `HardDeleteMany` | `error` | `nil` (idempotent) | N/A |
| `HardDeleteWhere` | `error` | `nil` (idempotent) | `ErrEmptyFilter` |

### Pagination Operations

| Method | Return Type | Error Behavior |
|--------|------------|---------------|
| `Paginate` | `(*PaginateResult, error)` | Standard |
| `Connection` | `(*Connection, error)` | `ErrInvalidCursor` for malformed cursors or `first` + `last` both provided |

### strict_updates Behavior

- **Default: `true`** — `Update` and `Increment` return `ErrNotFound` when the target PK does not exist.
- **When `false`** — `Update` and `Increment` return `nil` (idempotent, no error).
- Configurable globally (`generation.strict_updates`) and per-table (`tables.<table>.strict_updates`).
- Delete operations are always idempotent regardless of this setting.

### Empty Update Handling

When `Update` is called with no fields set (all omittable values unset), no UPDATE statement is executed. The method fetches and returns the current entity unchanged with `nil` error.

### Batch Failure Semantics

`CreateMany`, `UpdateMany`, `HardDeleteMany`, `SoftDeleteMany` split into sub-batches of `generation.batch_size` (default: 200).

- If any sub-batch fails, the operation returns the error immediately.
- Previously executed sub-batches are **not** automatically rolled back.
- The consumer must wrap the call in a transaction for atomicity.

---

## 8. Transaction Error Handling

### Automatic Rollback

`WithTransaction` manages the transaction lifecycle:

1. `BEGIN` — starts the transaction (or `SAVEPOINT` for nested calls).
2. Execute `fn` — the consumer's function.
3. On error from `fn`: `ROLLBACK` (or `ROLLBACK TO SAVEPOINT`).
4. On success: `COMMIT` (or `RELEASE SAVEPOINT`).

The consumer never calls `COMMIT` or `ROLLBACK` directly.

### Savepoint Rollback

When a nested `WithTransaction` returns an error:
- `ROLLBACK TO SAVEPOINT` rolls back only that savepoint's work.
- The outer transaction survives and can continue.

When a nested `WithTransaction` returns nil:
- `RELEASE SAVEPOINT` promotes the savepoint's work to the outer transaction.

Root-level rollback discards all callbacks at all depths. Savepoint rollback discards callbacks at that depth only.

### Timeout

When `TxOptions.Timeout` expires, the context is cancelled. In-flight operations return `context.DeadlineExceeded`. The transaction is rolled back by `WithTransaction`'s error handler.

### Deferred Side Effects

Events, cache invalidation, and other side effects are collected via `Tx.OnCommit(fn)` during the transaction. They are flushed only on successful root commit. On rollback, all collected callbacks are discarded.

### Callback Failure (Post-Commit)

When `OnCommit` callbacks fail after a successful commit:

1. Retry once immediately.
2. Log on second failure with full context (table, PK, operation, error).
3. Continue — one failure does not block others.
4. Trip the circuit breaker if failures exceed threshold.

The database commit has already succeeded — callback failure does not roll back the committed transaction.

---

## 9. Panic Recovery

A built-in panic recovery wrapper is always the outermost layer of every chain — it
runs before consumer-registered global hooks. It is not a `QueryHook`/`MutationHook` and
is not registered by the consumer; `hook.BuildQueryChain` / `hook.BuildMutationChain`
install it unconditionally (`hook/chain.go`).

### Default Behavior

1. Recover the panic value.
2. Return an error: `sqlgen: panic in {op} {table}: {r}`, where `{table}` is `m.Table` — the
   `TableXxx` value, already `schema.table` on PostgreSQL (PRD §5.5) — with `m.Schema` prefixed only
   when the value lacks it (`panicTableName`), so the schema is never printed twice.

That is the whole of it — the default **does not log and does not capture a stack
trace**. Consumers who want either must supply a handler (PRD §21.6).

### Custom Recovery

Consumers can register a custom panic handler via `WithPanicHandler(fn)`. The handler
receives `ctx`, the recovered value, table name, and operation, and returns the error the
caller sees. It *replaces* the default rather than wrapping it, and a client has exactly
one — unlike hooks, panic handlers do not chain. A handler that wants a stack trace
calls `debug.Stack()` itself.

Panic recovery always runs regardless of the `SkipHooks` flag in `CallOptions`.

---

## 10. Custom Error Packages

Generated code always wraps with `fmt.Errorf("<operation> <table>: %w", err)` and the
stdlib `errors` package. There is no configuration knob for this — a consumer who wants
stack traces, or any other error library, adds one at the client boundary with a **hook**
(PRD §22.4).

```go
// WithStackTraces attaches a stack trace to every error leaving the client.
func WithStackTraces() hook.QueryHook {
	return func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			res, err := next(ctx, q)
			if err != nil {
				return res, errors.WithStack(err) // cockroachdb/errors, or any package
			}
			return res, nil
		}
	}
}

client := db.New(querier,
	db.WithQueryHook(WithStackTraces()),
	db.WithMutationHook(WithStackTracesMutation()), // the MutationHook twin
)
```

Queries and mutations are separate hook types over separate handlers, so a wrapper that
covers both reads and writes is registered once on each chain.

**Three constraints** (PRD §22.4 carries the full statement of each):

1. **Preserve unwrapping.** The sentinels in section 3 are matched with `errors.Is` and
   `ConstraintError` (section 4) with `errors.As`. A hook that re-formats (`fmt.Errorf("%v", err)`)
   instead of wrapping breaks every one of them silently. Use a wrapper implementing
   `Unwrap` — `errors.WithStack` and `errors.Wrapf` from cockroachdb both do.
2. **`SkipHooks` bypasses the chain.** A call made with `CallOptions.SkipHooks` returns
   its error undecorated. Hook-based decoration is not a completeness guarantee.
3. **Two classes of error never reach the chain.** (a) *Pre-flight guards* return before
   `executeQuery` is called — the lock-mode guard on every read path, which on MySQL
   includes a `SELECT VERSION()` probe whose **driver** error also surfaces pre-chain;
   and, on a tenanted table's `Get` only, `resolve tenant` and the §29.7 verify-match's
   `tenancy.ErrMismatch`. (b) *`Stream`* reports through its iterator: its handler always
   returns `nil, nil`, so tenant-resolution, relationship-filter, query, scan, and
   `rows.Err()` failures reach the consumer via `yield` from inside the handler and no
   hook sees them. The chain's own error (a
   recovered panic or a hook's abort) is yielded last. Outside those, every driver and
   database error routes through the chain.

Panic recovery is a separate seam and runs even when `SkipHooks` is true, but it captures
no stack trace of its own (section 9) — a consumer who wants one supplies a
`WithPanicHandler` that calls `debug.Stack()`.

---

## 11. Linter Enforcement

These linters are enabled specifically to protect the error system:

| Linter | Purpose |
|--------|---------|
| `errcheck` | Unchecked errors in SQL operations, file I/O, and template execution are silent bugs. |
| `errorlint` | Enforces `errors.Is()` / `errors.As()` instead of `==` comparison. The entire error system relies on wrapping. |
| `nilerr` | Catches `if err != nil { return nil }` — returning nil when the error should propagate. |
| `wrapcheck` | Enforces that errors from external packages are wrapped with context before returning. Directly supports the `"{operation} {table}: {cause}"` convention. |
| `exhaustive` | Ensures `switch` on `ConstraintType` covers all cases (`Unique`, `ForeignKey`, `Check`, `NotNull`). |

### nolint Policy

`nolint` directives are permitted only with a justification comment:

```go
//nolint:errcheck // bytes.Buffer.Write cannot fail
_, _ = buf.WriteString(clause)
```

Bare `//nolint` directives without a reason are rejected.

---

## 12. Testing Errors

### Unit Tests — Driver Error Translation

Each driver adapter has unit tests that construct real driver error types and verify the mapping:

**pgx:**
- `*pgconn.PgError` with codes `23505`, `23503`, `23502`, `23514`, `40P01`, `08*` class.
- Verify all `ConstraintError` fields are populated from structured pgx fields.

**MySQL:**
- `*mysql.MySQLError` with numbers `1062`, `1451`, `1452`, `1048`, `3819`, `1213`, `2002`, `2006`.
- Verify message parsing extracts constraint and column names.
- Test graceful fallback when message format is unrecognized (fields left empty).

**SQLite (modernc):**
- `*sqlite.Error` with extended codes `2067`, `787`, `1299`, `275`, `1555`.
- Verify message parsing extracts constraint and column names.

### Integration Tests — Real Constraint Violations

Trigger real violations against test databases via testcontainers:

| Scenario | Expected Error |
|----------|---------------|
| Insert duplicate on UNIQUE column | `ConstraintError{Type: ConstraintUnique}` |
| Insert with invalid FK reference | `ConstraintError{Type: ConstraintForeignKey}` |
| Insert NULL into NOT NULL column | `ConstraintError{Type: ConstraintNotNull}` |
| Insert violating CHECK constraint | `ConstraintError{Type: ConstraintCheck}` |
| Concurrent deadlock (PostgreSQL/MySQL) | `ErrDeadlock` |

### Testing the Wrapping Chain

Verify that `errors.Is()` and `errors.As()` work through the full chain:

```go
// ConstraintError should match both specific and base sentinel
var ce *database.ConstraintError
if !errors.As(err, &ce) {
    t.Fatal("expected ConstraintError")
}
if !errors.Is(err, database.ErrConstraintViolation) {
    t.Fatal("expected ErrConstraintViolation in chain")
}
```

---

## Appendix: Error Review Checklist

- [ ] Sentinel errors used correctly (`ErrNotFound` only for single-entity lookups, etc.)
- [ ] `ConstraintError` fields populated from driver metadata where available
- [ ] Driver error types do not leak beyond `database/pgx/` and `database/stdlib/`
- [ ] Errors wrapped with `"{operation} {table}: {cause}"` format
- [ ] `%w` used for wrapping (not `%v`) to preserve the error chain
- [ ] `errors.Is()` / `errors.As()` used for checking (not `==`)
- [ ] `switch` on `ConstraintType` covers all four cases
- [ ] Unrecognized driver errors returned unwrapped (not silently misclassified)
- [ ] Batch operations document that sub-batches are not auto-rolled-back
- [ ] `strict_updates` behavior matches the configured setting
- [ ] Message parsing for MySQL / SQLite falls back gracefully on unexpected formats
- [ ] `errcheck`, `errorlint`, `wrapcheck`, `nilerr`, `exhaustive` all pass
