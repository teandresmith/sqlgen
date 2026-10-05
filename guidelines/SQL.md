# SQLGen — SQL & Dialect Standards

> Multi-dialect SQL conventions, builder patterns, parameterization rules, and dialect-specific behavior.
> Read this before writing or modifying SQL builders, comparators, or dialect implementations.

---

## Table of Contents

- [1. Principles](#1-principles)
- [2. Dialect Interface](#2-dialect-interface)
- [3. Placeholder Styles](#3-placeholder-styles)
- [4. Identifier Quoting](#4-identifier-quoting)
- [5. Table Formatting](#5-table-formatting)
- [6. SQL Builder Functions](#6-sql-builder-functions)
- [7. Parameterization & Injection Prevention](#7-parameterization--injection-prevention)
- [8. IN / NOT IN Handling](#8-in--not-in-handling)
- [9. RETURNING Clause](#9-returning-clause)
- [10. Upsert Syntax](#10-upsert-syntax)
- [11. Soft Delete SQL](#11-soft-delete-sql)
- [12. Batch Operations](#12-batch-operations)
- [13. Filtering & Conditions](#13-filtering--conditions)
- [14. Relationship Loading SQL](#14-relationship-loading-sql)
- [15. Pagination SQL](#15-pagination-sql)
- [16. Transactions & Savepoints](#16-transactions--savepoints)
- [17. Error Code Mapping](#17-error-code-mapping)
- [18. Dialect Feature Matrix](#18-dialect-feature-matrix)
- [19. Adding a New Builder Function](#19-adding-a-new-builder-function)

---

## 1. Principles

1. **Every value is parameterized.** No string interpolation of user-supplied data into SQL. All values flow through placeholders. No exceptions.
2. **Every identifier is quoted.** Table names go through `FormatTable()`. Column names go through `QuoteIdentifier()`. This prevents collisions with reserved words and ensures correctness across dialects.
3. **Dialect differences live behind the `Dialect` interface.** Builder functions accept a `Dialect` and call its methods. They do not switch on dialect name strings.
4. **Builders produce SQL strings and argument slices.** They never execute queries. Execution is the caller's responsibility.
5. **Test every dialect.** A builder function that works for PostgreSQL but produces invalid MySQL syntax is a bug. Every builder has test cases for all three dialects.

---

## 2. Dialect Interface

The `sql.Dialect` interface is the single abstraction point for all dialect differences. Every SQL builder function receives a `Dialect` as its first parameter.

```go
type Dialect interface {
    Name() string
    Placeholder(position int) string
    PlaceholderList(start, count int) string
    QuoteIdentifier(name string) string
    FormatTable(table Table) string
    SupportsReturning() bool
    ReturningClause(columns []string) string
    UpsertClause(opts UpsertClauseOptions) string
    SupportsArrayParams() bool
    SupportsTupleIN() bool
    BackslashEscapes() bool
    DefaultValuesClause() string
    LockClause(mode LockMode) string
}
```

### Implementation Rules

- Each dialect has its own file: `postgres.go`, `mysql.go`, `sqlite.go`.
- Dialect structs are immutable after construction. Fields (e.g., driver mode) are set via constructors and must not change. No exported mutable fields.
- All methods must be deterministic and pure.
- Never add dialect-specific logic outside the `Dialect` interface. If a builder needs dialect-specific behavior, add a method to the interface and implement it in all three dialects.

---

## 3. Placeholder Styles

| Dialect | Style | Example | Notes |
|---------|-------|---------|-------|
| PostgreSQL | Positional numbered | `$1`, `$2`, `$3` | Position is 1-indexed |
| MySQL | Positional by order | `?`, `?`, `?` | Position implicit from argument order |
| SQLite | Positional by order | `?`, `?`, `?` | Same as MySQL |

### Rules

- Call `Dialect.Placeholder(position)` to produce a single placeholder. The `position` argument is 1-indexed.
- Call `Dialect.PlaceholderList(start, count)` to produce a comma-separated list (e.g., `$1, $2, $3` or `?, ?, ?`). Used in INSERT VALUES and IN clauses.
- Track placeholder position as you build SQL. Each new argument increments the position counter.
- For MySQL and SQLite, `Placeholder()` ignores the position argument and always returns `?`, but callers must still pass the correct position to keep argument ordering consistent.

### Example

```go
// position tracks the next placeholder number
pos := 1
clause := fmt.Sprintf("%s = %s", d.QuoteIdentifier("name"), d.Placeholder(pos))
pos++
// PostgreSQL: "name" = $1
// MySQL:      `name` = ?
```

---

## 4. Identifier Quoting

| Dialect | Quote Character | Example |
|---------|----------------|---------|
| PostgreSQL | Double quotes | `"users"`, `"created_at"` |
| MySQL | Backticks | `` `users` ``, `` `created_at` `` |
| SQLite | Double quotes | `"users"`, `"created_at"` |

### Rules

- Always quote identifiers via `Dialect.QuoteIdentifier()`. Never hard-code quote characters.
- `QuoteIdentifier` doubles an embedded quote character (`say"hi` → `"say""hi"`; `` a`b `` → ``` `a``b` ``` on MySQL), the standard escape on all three dialects. `FormatTable`, `ReturningClause` and `UpsertClause` quote through it.
- Column names are always bare identifiers — never schema-qualified. Only table references use `FormatTable()`.
- Quoting prevents collisions with SQL reserved words (`order`, `group`, `user`, `key`, `type`, `index`, etc.) and ensures names with special characters are handled correctly.

### Column Aliases in JOINs

For relationship loading via JOINs, column aliases use the `"alias.column"` pattern:

| Dialect | Alias Syntax | Example |
|---------|-------------|---------|
| PostgreSQL | `AS "p.id"` | `SELECT p."id" AS "p.id"` |
| MySQL | `` AS `p.id` `` | `` SELECT p.`id` AS `p.id` `` |
| SQLite | `AS "p.id"` | `SELECT p."id" AS "p.id"` |

The alias is produced by quoting the combined `alias.column` string, not by concatenating separately quoted parts.

---

## 5. Table Formatting

The `sql.Table` struct carries schema and name:

```go
type Table struct {
    Schema string // "public", "billing", "" for MySQL/SQLite
    Name   string // "users", "order_items"
}
```

`Dialect.FormatTable()` produces the correct syntax:

| Dialect | `Table{Schema: "public", Name: "users"}` | `Table{Name: "users"}` |
|---------|------------------------------------------|------------------------|
| PostgreSQL | `"public"."users"` | `"users"` |
| MySQL | `` `users` `` | `` `users` `` |
| SQLite | `"users"` | `"users"` |

### Rules

- PostgreSQL always includes the schema in generated SQL, even for the `public` schema.
- MySQL and SQLite ignore the `Schema` field entirely.
- Use `FormatTable()` in `FROM`, `INSERT INTO`, `UPDATE`, `DELETE FROM`, and `JOIN` clauses. Nowhere else.
- Never construct table references by string concatenation. Always use `FormatTable()`.

---

## 6. SQL Builder Functions

All builders live in `sql/builder.go` and follow the same signature pattern:

```go
func Build<Operation>(d Dialect, t Table, ...) (string, []any)
```

They return the SQL string and the argument slice. They never execute queries.

### Builder Inventory

| Function | SQL Pattern | Used By |
|----------|-------------|---------|
| `BuildSelect` | `SELECT ... FROM {table} WHERE ... ORDER BY ... LIMIT ... OFFSET` | Get, GetMany, Paginate |
| `BuildSelectJoin` | `SELECT {alias}.{col} AS "{alias}.{col}" FROM {table} {alias} LEFT JOIN ... ORDER BY {alias}.{col}` | O2O relationship loading |
| `BuildInsert` | `INSERT INTO {table} (...) VALUES (...)` + optional conflict clause + optional `RETURNING`. The conflict clause is omitted when the target names a column the statement does not write (PRD §9.5 Upsert Conflict Keys) | Create, Upsert |
| `BuildInsert` (no column) | `INSERT INTO {table} DEFAULT VALUES` (PostgreSQL/SQLite) or `INSERT INTO {table} () VALUES ()` (MySQL), from `Dialect.DefaultValuesClause()`, + optional `RETURNING`. No conflict clause: SQLite accepts none after `DEFAULT VALUES`, so an upsert here is a plain insert on every dialect (PRD §9.5 Upsert Conflict Keys) | Create, Upsert with a zero input on a table with no required field |
| `BuildMultiInsert` | `INSERT INTO {table} (...) VALUES (...), (...), ...` — emits `DEFAULT` keyword for `sql.Default` sentinel values; emits raw SQL expressions for `sql.DefaultExpr` values (SQLite) | CreateMany |
| `BuildUpdate` | `UPDATE {table} SET ... WHERE ...` + optional `RETURNING` | Update |
| `BuildCount` | `SELECT COUNT(*) FROM {table} WHERE ...` | Count |
| `BuildExists` | `SELECT EXISTS(SELECT 1 FROM {table} WHERE ...)` | Exists |
| `BuildIncrement` | `UPDATE {table} SET col = col + $N WHERE ...` | Increment |
| `BuildSoftDelete` | `UPDATE {table} SET deleted_at = CURRENT_TIMESTAMP WHERE ...` + optional `RETURNING` | SoftDelete, SoftDeleteWhere |
| `BuildHardDelete` | `DELETE FROM {table} WHERE ...` | HardDelete |

### Rules

- Every builder must handle all three dialects. Test all three.
- Builders compose `WHERE` clauses from `[]sql.Condition`. They do not build conditions — that is the comparator's job.
- `RETURNING` is appended only when `Dialect.SupportsReturning()` returns true.
- Column lists are always sorted to ensure deterministic SQL output.
- **`Limit`/`Offset` are `*int` pointers.** `nil` means omit the clause entirely. The builder emits the clause only when the pointer is non-nil. It does **not** interpret `0` specially — `LIMIT 0` returns zero rows in SQL. The "0 means no limit" semantic is the generated code's responsibility: resolve `new(0)` → `nil` before calling the builder.

---

## 7. Parameterization & Injection Prevention

This is the most critical section. Every path from user input to SQL must go through parameterization.

### Rules

1. **All values use placeholders.** Comparator `Parse()` methods produce `sql.Condition` structs with a `Clause` containing a `$` placeholder and a separate `Value`. The builder replaces `$` with the dialect-specific placeholder (`$1`, `?`).
2. **All identifiers use `QuoteIdentifier()`.** Column names in `WHERE`, `SET`, `ORDER BY`, and `SELECT` clauses are quoted. Table names use `FormatTable()`.
3. **No string formatting of values into SQL.** Never use `fmt.Sprintf` to put a value into a SQL string. Values go into the `[]any` argument slice.
4. **`gosec` is enabled.** The linter catches SQL injection patterns in builder code. Do not suppress `gosec` warnings without a justification comment and code review.
5. **No reflection for column mapping.** All column-to-field mapping in generated code is compile-time generated switch statements, not runtime reflection.

### The `sql.Condition` Type

```go
type Condition struct {
    Clause string // SQL fragment, e.g.: "name" = $ or "price" BETWEEN $ AND $
    Value  any    // parameter value(s)
    Column string // the column token inside Clause that alias qualification rewrites
}
```

**Set `Column` whenever you build a condition from a known column.** `sql.PrefixConditions` qualifies filter conditions with the parent alias on the O2O-join read path. With `Column` set it rewrites that token in place; with `Column` empty it prepends `alias + "."` to the whole clause, which is right only when the column is the clause's leading token. `ConditionBuilder` sets it for you, so `sql.Where(col).Eq(v)` needs no thought — the field matters when you hand-build a `Condition`, and it is the difference between `JSON_CONTAINS(u.metadata, ?)` and the broken `u.JSON_CONTAINS(metadata, ?)`. Leave it empty on `sql.Raw`, which has no single column to name.

The `$` in `Clause` is a placeholder token that builders replace with the dialect-specific placeholder at the correct position. This is the only mechanism for injecting values into SQL.

### Raw SQL Escape Hatch

The `sql.Raw()` function allows arbitrary SQL conditions:

```go
sql.Raw("price * quantity > $", 1000.00)
```

Even raw conditions use the `$` placeholder pattern. The value is still parameterized. Document any use of `Raw()` with a comment explaining why the built-in comparators are insufficient.

A `Raw` clause may instead number its own args, `$1`…`$k`, exactly as a subquery may ([§13 Subquery Support](#subquery-support), PRD §11.5). The scalar (one arg) and multi-arg paths both read either form with the subquery's rule and its quote-aware scan, so `sql.Raw("owner_id = $1", 10)` at position 2 renders `owner_id = $2` on PostgreSQL and `owner_id = ?` on MySQL and SQLite. Do not rewrite a numbered token by replacing the `$` and keeping the digits: SQLite reads a leftover `?1` as a numbered parameter bound to the statement's first arg and returns wrong rows with no error.

---

## 8. IN / NOT IN Handling

IN/NOT IN handling varies by dialect and driver because PostgreSQL with pgx supports native array parameters.

| Dialect | Driver | IN SQL | NOT IN SQL | Arg Shape |
|---------|--------|--------|------------|-----------|
| PostgreSQL | pgx | `"col" = ANY($1)` | `"col" != ALL($1)` | Single array arg |
| PostgreSQL | stdlib | `"col" IN ($1, $2, ...)` | `"col" NOT IN ($1, $2, ...)` | Expanded args |
| MySQL | stdlib | `` `col` IN (?, ?, ...) `` | `` `col` NOT IN (?, ?, ...) `` | Expanded args |
| SQLite | stdlib | `"col" IN (?, ?, ...)` | `"col" NOT IN (?, ?, ...)` | Expanded args |

**`comparator.Enum` is a permanent exception to row 1** and takes the expanded form on pgx too. A generated enum is a named string type, and pgx has no encode plan for a slice of one against the column's enum-array OID — `= ANY($1)` failed the query outright, while the scalar operators went through the driver's string-kind conversion and worked. Expanding asks the driver for nothing beyond what `Eq` already proved it can do. The placeholder cost is bounded by the enum's declared members rather than by the caller, which is why the trade is right here and nowhere else. `comparator.Slice` is on neither row: it sends the whole array as a **PostgreSQL array text literal** string, which is what lets an enum array travel without OID registration. That literal is built by hand, so `sliceToArrayLiteral` owns the array-literal grammar — see §8.1.

### 8.1 Array literals (`comparator.Slice`)

`comparator.Slice` does not use either row above. Its operators (`&&` / `@>` / `<@`) take an array operand, and it builds a **PostgreSQL array text literal** (`{admin,editor}`) rather than handing pgx a typed slice — deliberately, since that is what lets an enum array travel without registering the type's OID.

Building the literal by hand means `sliceToArrayLiteral` owns PostgreSQL's array-literal grammar, and honouring it is **not cosmetic**. Quote an element when it is empty, case-insensitively `NULL`, or contains `{`, `}`, `,`, `"`, `\` or whitespace; escape `"` and `\` inside the quotes. Each case is a distinct way an unquoted element is *misread rather than rejected* — and the comma is the dangerous one: it splits one element into two server-side, so the predicate matches a different set and the query returns silently wrong rows with no error. Enum members are identifiers and never need quoting, which is exactly why the defect stayed invisible until a `text[]` column carried a caller-supplied value.

### Why ANY/ALL for PostgreSQL + pgx?

- Single placeholder regardless of list size — cleaner SQL.
- pgx handles Go slice-to-PostgreSQL array conversion natively.
- Avoids parameter count explosion for large IN lists.

### Implementation

The comparators choose the row: `parseIn` / `parseNin` (`comparator/comparator.go`) emit `col = ANY($)` / `col != ALL($)` with one slice arg when `Dialect.SupportsArrayParams()` is true, and `col IN $` / `col NOT IN $` with `[]any` otherwise. The builder expands a `col IN $` clause carrying `[]any` to one placeholder per value on **every** dialect and never rewrites it to `ANY` — that is what lets `comparator.Enum` opt out. `sql.Where(col).In(...)` therefore always takes the expanded form, and so do the generated code paths built on it: the `SoftDeleteMany` / `RestoreMany` / `HardDeleteMany` id lists, the `UpdateMany` / `UpsertMany` tenant-capture pre-reads, and the M2M junction loader. Filter-driven reads (`GetMany`, the O2M loader, the M2M target fetch) go through the comparators and take the pgx row. An empty list renders `1 = 0` (IN) or `1 = 1` (NOT IN).

---

## 9. RETURNING Clause

| Dialect | Supported | Notes |
|---------|-----------|-------|
| PostgreSQL | Yes | Full support for INSERT, UPDATE, DELETE |
| MySQL | No | Requires workaround |
| SQLite | Yes | SQLite 3.35+ |

### PostgreSQL and SQLite

Builders append `RETURNING col1, col2, ...` when `Dialect.SupportsReturning()` returns true. The returned columns are used to scan the resulting entity directly from the write statement.

### MySQL Workaround

MySQL has no `RETURNING` clause. Generated code follows this pattern instead:

| Operation | PK Strategy | MySQL Pattern |
|-----------|-------------|---------------|
| Create | `db` (AUTO_INCREMENT) | `Exec()` -> `LastInsertId()` -> `Get()` |
| Create | `app` (UUID) | PK known before INSERT; `Exec()` -> `Get()` by known PK |
| Create | `caller` | PK provided by caller; `Exec()` -> `Get()` by known PK |
| Update/Upsert | any | `Exec()` -> `Get()` to fetch resulting entity |

This is a two-round-trip pattern. The generated code handles it transparently — consumers do not need to know about the workaround.

---

## 10. Upsert Syntax

Upsert syntax is one of the sharpest dialect differences.

| Dialect | SQL |
|---------|-----|
| PostgreSQL | `INSERT INTO ... ON CONFLICT (col1, col2) DO UPDATE SET col3 = excluded.col3, ...` |
| MySQL | `INSERT INTO ... ON DUPLICATE KEY UPDATE col3 = VALUES(col3), ...` |
| SQLite | `INSERT INTO ... ON CONFLICT (col1, col2) DO UPDATE SET col3 = excluded.col3, ...` |

### MySQL Caveat

`ON DUPLICATE KEY UPDATE` does not accept an explicit conflict target — it triggers on **any** unique constraint violation. The `conflictKeys` parameter in `Dialect.UpsertClause()` is accepted for API consistency but does not affect the MySQL SQL output. This means MySQL upserts are less precise than PostgreSQL/SQLite upserts when a table has multiple unique constraints.

`BuildInsert` rules out the case where that imprecision is never what the caller meant: a conflict target naming a column the statement does not write (a database-generated key, or a column left to its `DEFAULT`). It emits no conflict clause there on any dialect, so a collision on another unique index is a unique violation rather than an update of the colliding row (PRD §9.5 Upsert Conflict Keys).

### Update Columns in Upsert

The `DO UPDATE SET` clause includes all non-PK, non-conflict-key columns from `CreateInput`. Primary key columns and conflict target columns are excluded from `SET` — they are the identity, not the payload.

### Preserving the PK Through a No-Op Conflict

`UpsertClauseOptions.ResolvePKColumn` names the single-column PK the caller has to read back from the statement, and is `""` when the caller already knows it (composite, app-, or caller-generated). It exists because a conflict that changes nothing still has to yield the conflicting row's PK, and each dialect loses that differently:

| Dialect | Behavior |
|---------|----------|
| MySQL | Opens the set list with `` `pk` = LAST_INSERT_ID(`pk`) ``. Without it the OK packet carries `insert_id = 0` and `LastInsertId()` returns 0. It must lead the list — MySQL evaluates assignments left to right, so a later `` `pk` = VALUES(`pk`) `` would overwrite the preserved id. It also subsumes the degenerate `` `k` = `k` `` form rather than stacking on it |
| PostgreSQL / SQLite | Ignored. The clause cannot preserve a PK the way `LAST_INSERT_ID()` does, so the generated caller detects the empty `DO NOTHING` result set and resolves the PK with a follow-up `SELECT` on the conflict columns |

Both halves are one defect. Do not "simplify" the MySQL self-assignment away or reorder it into the set list: the regression tests live in the `mysql`, `postgres`, and `tenancy` examples, and the invariant is pinned in PRD §9.7.

### Column Order: `BuildSelect` Sorts, `ReturningClause` Does Not

`BuildSelect` emits its column list sorted (`writeSelectColumns` → `sortedCopy`) so that output is deterministic regardless of how the caller assembled the list. `ReturningClause` emits the caller's order verbatim. The two therefore disagree for the same input:

```
Columns/ReturningColumns: {"id", "account_id"}
  SELECT    -> SELECT "account_id", "id" FROM ...
  RETURNING -> ... RETURNING "id", "account_id"
```

**Any generated code that scans a multi-column `BuildSelect` result must bind targets by column name, not by position** — via `rows.Columns()` and a name switch, the way `scan<Entity>` does. A positional scan happens to work only while the declared order matches ascending sort, and fails *silently* when it does not and the mistargeted columns share a Go type. This bit `resolveUpsertConflictRow` and `captureAffectedTenants`, both of which select `(pk…, tenant)`: correct for `workspace_id`, wrong for `account_id`.

Single-column selects are exempt — there is no order to get wrong — which is why the tenant lookup on the composite-PK upsert branch requests one column rather than reusing a wider helper.

---

## 11. Soft Delete SQL

Soft delete SQL varies by the configured column type.

### Write Operations

| Column Type | SQL on Soft Delete | SQL on Restore |
|-------------|-------------------|----------------|
| `timestamp` | `SET deleted_at = CURRENT_TIMESTAMP` | `SET deleted_at = NULL` |
| `bool` | `SET is_deleted = TRUE` | `SET is_deleted = FALSE` |
| `integer` | `SET deleted = 1` | `SET deleted = 0` |

### Automatic WHERE Clause Injection

When a table has soft delete enabled (`exclude_deleted: true`, the default), the **generated methods** (not the builders) inject a default filter condition. Builders themselves are stateless — they produce SQL from the conditions they receive. The injection happens in the method layer before calling the builder:

| Column Type | Injected WHERE | Zero Value |
|-------------|---------------|------------|
| `timestamp` | `WHERE "deleted_at" IS NULL` | `NULL` |
| `bool` | `WHERE "is_deleted" = $1` (false) | `FALSE` |
| `integer` | `WHERE "deleted" = $1` (0) | `0` |

### Soft Delete Scoping

Soft delete filtering is controlled by two mechanisms working together:

**1. Comparator on the filter.** The soft delete column gets a regular comparator field on the filter struct (e.g., `DeletedAt *comparator.NullableTime` for timestamp strategy). There is no `IncludeDeleted` or `IsSoftDeleted` field — the comparator is the single mechanism.

**2. Method-level default injection.** When `exclude_deleted` is enabled (default when a soft delete column exists), each generated method checks whether the caller set the soft delete comparator on the filter:
- **Not set** → method injects the default "is not deleted" condition (e.g., `deleted_at IS NULL`)
- **Set** → method uses the caller's comparator, no default injected

Exceptions:
- `SoftDelete*` — always adds "is not deleted" condition (only soft-delete active rows)
- `Restore*` — always adds "is deleted" condition (only restore deleted rows)
- `Get` / `Exists` (PK methods with no filter) — always inject the default when `exclude_deleted` is enabled

`ToConditions` produces only explicit caller-set conditions — no implicit behavior. See PRD Section 9.8 for full implementation details.

---

## 12. Batch Operations

Batch operations use different SQL strategies depending on the operation type and driver.

| Operation | SQL Strategy | pgx | stdlib |
|-----------|-------------|-----|--------|
| `CreateMany` | Multi-row INSERT: `VALUES (...), (...), (...)` | Single statement | Single statement |
| `HardDeleteMany` | `DELETE ... WHERE id IN (...)` | Single statement | Single statement |
| `SoftDeleteMany` | `UPDATE ... SET deleted_at = CURRENT_TIMESTAMP WHERE id IN (...)` | Single statement | Single statement |
| `RestoreMany` | `UPDATE ... SET deleted_at = NULL WHERE id IN (...)` | Single statement | Single statement |
| `UpdateMany` | N individual UPDATE statements | Pipelined via `SendBatch` (1 round-trip) | Sequential in transaction (N round-trips) |

### Batch Size Splitting

All batch operations split input into sub-batches of `generation.batch_size` (default: 200). This prevents:

- Exceeding database parameter count limits (PostgreSQL: 65535 parameters per statement).
- Generating excessively large SQL strings.
- Causing memory pressure on the database server's query parser.

### SQLite DEFAULT Limitation

SQLite does not support the `DEFAULT` keyword as a value expression in `INSERT ... VALUES(...)`. The `sql.Default` sentinel used by PostgreSQL and MySQL causes a syntax error on SQLite. Instead, the code generator emits `sql.DefaultExpr` for SQLite — a sentinel that carries the raw SQL default expression (e.g., `datetime('now')`, `'viewer'`, `0`). For nullable columns without a DEFAULT clause, the expression is `NULL`. `BuildMultiInsert` writes the expression inline instead of the `DEFAULT` keyword.

| Dialect | Omittable field not set | Sentinel | SQL output |
|---------|------------------------|----------|------------|
| PostgreSQL | `sql.Default` | `DEFAULT` | `VALUES (DEFAULT, $1)` |
| MySQL | `sql.Default` | `DEFAULT` | `VALUES (DEFAULT, ?)` |
| SQLite | `sql.NewDefaultExpr("datetime('now')")` | Raw expression | `VALUES (datetime('now'), ?)` |

### pgx Pipeline Advantage

For `UpdateMany`, pgx's `SendBatch` pipelines all UPDATE statements into a single network round-trip. The database executes them sequentially, but there is only one round-trip of latency instead of N. The stdlib driver cannot pipeline, so it issues N sequential statements within a transaction.

---

## 13. Filtering & Conditions

Comparators produce `sql.Condition` values. Builders consume them. This separation keeps SQL generation clean.

### Standard Operators

| Comparator Method | SQL Produced |
|-------------------|-------------|
| `Eq(v)` | `"col" = $` |
| `Neq(v)` | `"col" != $` |
| `Gt(v)` | `"col" > $` |
| `Gte(v)` | `"col" >= $` |
| `Lt(v)` | `"col" < $` |
| `Lte(v)` | `"col" <= $` |
| `In(v...)` | `"col" = ANY($)` or `"col" IN ($, $, ...)` (dialect-dependent) |
| `Nin(v...)` | `"col" != ALL($)` or `"col" NOT IN ($, $, ...)` (dialect-dependent) |
| `Like(v)` | `"col" LIKE $` |
| `NLike(v)` | `"col" NOT LIKE $` |
| `Between(a, b)` | `"col" BETWEEN $ AND $` |
| `IsNull()` | `"col" IS NULL` |
| `IsNotNull()` | `"col" IS NOT NULL` |

### JSON / JSONB Operators (dialect-specific)

| Operator | PostgreSQL | MySQL | SQLite |
|----------|-----------|-------|--------|
| `Contains` | `"col"::jsonb @> $` | `JSON_CONTAINS(col, $)` | — |
| `HasKey` | `"col"::jsonb ? $` | `JSON_CONTAINS_PATH(col, 'one', $)` | — |

**`comparator.JSON` covers PostgreSQL and MySQL only.** `Parse` has no `sqlite` arm, so it returns zero conditions there. Do not "close the gap" by adding a partial one: SQLite has a faithful `HasKey` (`json_type(col, $) IS NOT NULL`) but no containment operator, and every emulation it can express is position-sensitive where `@>` / `JSON_CONTAINS` compare array elements order-independently — a wrong subset instead of an over-broad one. The generator's answer is to omit the filter field entirely on SQLite, on the model side and the GraphQL side alike, from one predicate over the resolved comparator (`columnIsFilterable`). See PRD §11.2.

**`comparator.JSON` casts on PostgreSQL; `comparator.JSONB` does not.** `@>` and `?` are jsonb-only operators, so the uncast form raises SQLSTATE 42883 on a `json` column. The cast is unconditional because `Parse` never sees the column's SQL type — and it is free where the column is already `jsonb`, since the planner elides a same-type cast and a GIN index still matches. `JSONB` needs no cast: it is only ever selected for a `jsonb` column. `HasKey`'s operand does **not** port between dialects — PostgreSQL's `?` takes a bare key (`region`), MySQL's `JSON_CONTAINS_PATH` takes a path (`$.region`). See PRD §11.2.

PostgreSQL-only JSONB operators: `?` (HasKey), `?\|` (HasAnyKey), `?&` (HasAllKeys), `@>` (Contains), `<@` (ContainedBy), `@?` (PathExists).

### Array/Slice Operators (PostgreSQL-only)

| Operator | SQL |
|----------|-----|
| `ContainsAny` | `"col" && $` |
| `ContainsAll` | `"col" @> $` |
| `ContainedBy` | `"col" <@` $ |

### Condition Composition

Conditions are composed with `And` and `Or`:

```go
q := d.QuoteIdentifier
conditions := sql.And(
    sql.Where(q("status")).Eq("active"),
    sql.Or(
        sql.Where(q("role")).Eq("admin"),
        sql.Where(q("role")).Eq("moderator"),
    ),
)
// WHERE "status" = $1 AND ("role" = $2 OR "role" = $3)
```

`sql.Where` and every comparator's `Parse` write the column token they are given verbatim, into both `Clause` and `Column`; they never quote. The caller quotes through `Dialect.QuoteIdentifier`, and generated code always does. `sql.Where("status")` renders `status = $1`, which is invalid SQL for a keyword, mixed-case or digit-leading column. The quoted forms in the operator tables above assume a quoted column. Because the token is verbatim, a caller can also pass an already-qualified one, such as `j.Alias + "." + d.QuoteIdentifier(col)`.

The builder helpers that take a column name rather than a WHERE token quote it themselves, so they take a raw name: `sql.Exists(correlationColumn, …)` (qualified with the enclosing alias at build time), `BuildCompositePKConditions` / `BuildCompositePKBatchCondition`, `SoftDeleteOptions.Column` and `BuildIncrement`. Passing them a quoted name quotes it twice.

### Subquery Support

```go
sql.Where("company_id").In(sql.Subquery{
    SQL:  `SELECT "id" FROM "companies" WHERE "country" = $`,
    Args: []any{"US"},
})
// WHERE company_id IN (SELECT "id" FROM "companies" WHERE "country" = $1)
```

`In` / `Nin` given a single `sql.Subquery` render a nested SELECT; a `Subquery` mixed with other values is bound as a value like any other. Subqueries use the same parameterization rules as top-level conditions: the subquery's SQL carries `$` tokens, and the builder numbers them into the enclosing statement's sequence in `Conditions` order, so the subquery never needs to know its position. The SQL itself is the caller's to write, identifiers quoted for the target dialect.

The subquery (`Subquery` or `Exists`) may instead number its own args, `$1`…`$k`, where `$N` is `Args[N-1]` and may repeat or appear out of order (PRD §11.5). One form per subquery: any `$N` token puts it in the numbered form. PostgreSQL renders `$N` at its absolute position (`$1` in a subquery at position 2 is `$2`, every time it appears); MySQL and SQLite emit `?` per token and list the args in token order, duplicating a repeated one.

A `$` inside a string literal, a quoted identifier, a comment or a PostgreSQL dollar-quoted string is the SQL's own text (`'$.tier'`, `"price$usd"`) and takes no arg. Every other `$` is a token. Quotes match the SQL-standard way, a doubled quote escaping itself. On MySQL a backslash also escapes the character after it inside a `'…'` or `"…"` string literal, as MySQL's default `sql_mode` reads it. PostgreSQL and SQLite read a backslash there literally, and a PostgreSQL `E'…'` escape is not recognised, so write `''` on them.

SQL outside both forms is the caller's to fix: the builder neither checks nor repairs it. The bare form numbers the first `len(Args)` tokens, leaves any further `$` as written and returns every arg; the numbered form copies a token it cannot read as one of the subquery's args (a bare `$`, `$0`, `$N` past `len(Args)`) through as written. What the database then does, measured on pgx v5, lib/pq, go-sql-driver/mysql and modernc sqlite with the subquery between two other conditions:

| Shape | PostgreSQL | MySQL | SQLite |
|-------|------------|-------|--------|
| More bare `$` than args | `syntax error at or near "$"` (42601) | `Unknown column '$'` (1054) | `unrecognized token: "$"` |
| Fewer bare `$` than args, or none | pgx `expected 3 arguments, got 4`; lib/pq `got 4 parameters but the statement requires 3` | `sql: expected 3 arguments, got 4` | **no error**: the surplus args shift onto the placeholders that follow |
| `$` and `$N` mixed | `syntax error at or near "$"` (42601) | `Unknown column '$'` (1054) | `unrecognized token: "$"` |
| `$0` | `there is no parameter $0` (42P02) | `Unknown column '$0'` (1054) | `missing named argument "0"` |
| `$N` past `len(Args)` | **no error** when the enclosing statement's `$N` exists and its type fits | `Unknown column '$3'` (1054) | `missing argument with index 4` |
| An arg no `$N` references | `could not determine data type of parameter $2` (42P18) | **no error**: the arg is left out | **no error**: the arg is left out |

---

## 14. Relationship Loading SQL

### One-to-One (LEFT JOIN)

O2O relationships are loaded in a single query via LEFT JOIN:

```sql
SELECT p."id" AS "p.id", p."name" AS "p.name",
       c."id" AS "c.id", c."name" AS "c.name"
FROM "public"."products" p
LEFT JOIN "public"."companies" c ON c."id" = p."company_id"
WHERE p."price" > $1
```

When the joined table has soft delete enabled, the JOIN condition includes the filter:

```sql
LEFT JOIN "public"."companies" c ON c."id" = p."company_id" AND c."deleted_at" IS NULL
```

### One-to-One Chains (multi-JOIN)

O2O chains follow the same pattern with multiple LEFT JOINs:

```sql
SELECT p."id" AS "p.id", p."name" AS "p.name",
       c."id" AS "c.id", c."name" AS "c.name",
       c2."id" AS "c2.id", c2."name" AS "c2.name"
FROM "public"."products" p
LEFT JOIN "public"."companies" c ON c."id" = p."company_id"
LEFT JOIN "public"."countries" c2 ON c2."id" = c."country_id"
WHERE p."price" > $1
```

Join aliases are bare, so each one is a letter plus an optional counter (`c`, `c2`), never a prefix of the field name, which can be a keyword (`in`, `user`). PRD §13.2 has the rule.

### One-to-Many (batched IN query)

O2M relationships use a separate batched query:

```sql
SELECT "id", "rating", "product_id"
FROM "public"."reviews"
WHERE "product_id" = ANY($1)
  AND "rating" >= $2
ORDER BY "created_at" DESC
```

Results are grouped by the FK column in Go code and attached to the parent entities.

### Many-to-Many (junction table)

M2M relationships query through the junction table. The SQL depends on the specific junction table schema.

---

## 15. Pagination SQL

### Offset-Based

Standard `LIMIT` / `OFFSET`:

```sql
SELECT ... FROM "public"."products"
WHERE ...
ORDER BY "created_at" DESC
LIMIT 20 OFFSET 40
```

### Cursor-Based (Keyset)

Cursor-based pagination uses `WHERE` conditions on the cursor keys instead of `OFFSET`. The generated
`cursorKeyset` helper expands the composite key into an OR of ANDs (not a row-value comparison) and
quotes every key through the dialect; on the O2O JOIN path `PrefixConditions` qualifies each quoted key
with the parent alias (`p."created_at"`):

**Forward pagination:**
```sql
SELECT ... FROM "public"."products"
WHERE (("created_at" > $1) OR ("created_at" = $2 AND "id" > $3))
ORDER BY "created_at" ASC, "id" ASC
LIMIT 21  -- first + 1 to detect hasNextPage
```

**Backward pagination:**
```sql
SELECT ... FROM "public"."products"
WHERE (("created_at" < $1) OR ("created_at" = $2 AND "id" < $3))
ORDER BY "created_at" DESC, "id" DESC
LIMIT 21  -- last + 1 to detect hasPreviousPage
```

Results from backward pagination are reversed before returning to the caller.

Cursors are Base64-encoded JSON containing the cursor key values:
```
base64({"id":"550e8400-...","created_at":"2024-01-15T09:30:00Z"})
```

---

## 16. Transactions & Savepoints

### BEGIN Statement

```sql
-- Default (zero-value TxOptions)
BEGIN

-- With options
BEGIN ISOLATION LEVEL SERIALIZABLE READ ONLY DEFERRABLE
```

### Isolation Level Support

| Level | PostgreSQL | MySQL | SQLite |
|-------|-----------|-------|--------|
| `Serializable` | Yes | Yes | Yes (default and only level) |
| `RepeatableRead` | Yes | Yes (default) | No |
| `ReadCommitted` | Yes (default) | Yes | No |
| `ReadUncommitted` | Yes | Yes | No |

| Option | PostgreSQL | MySQL | SQLite |
|--------|-----------|-------|--------|
| `ReadOnly` | Yes | Yes (`START TRANSACTION READ ONLY`) | No |
| `Deferrable` | Yes | No (ignored) | No (ignored) |

SQLite only supports `SERIALIZABLE`. Other isolation levels are silently ignored.

### Savepoint Syntax

Savepoints enable nested transactions.

| Operation | PostgreSQL | MySQL | SQLite |
|-----------|-----------|-------|--------|
| Create | `SAVEPOINT name` | `SAVEPOINT name` | `SAVEPOINT name` |
| Release | `RELEASE SAVEPOINT name` | `RELEASE SAVEPOINT name` | `RELEASE SAVEPOINT name` |
| Rollback | `ROLLBACK TO SAVEPOINT name` | `ROLLBACK TO SAVEPOINT name` | `ROLLBACK TRANSACTION TO SAVEPOINT name` |

Note the SQLite difference: `ROLLBACK TRANSACTION TO SAVEPOINT` vs `ROLLBACK TO SAVEPOINT`. This must be handled in the SQLite dialect implementation.

### Driver Differences

| Driver | Transaction Mechanism | Savepoint Mechanism |
|--------|----------------------|---------------------|
| pgx | Native `tx.Begin(ctx)` | Native `tx.Begin(ctx)` (creates savepoint) |
| stdlib | `db.BeginTx(ctx, opts)` | Raw SQL issued internally by the transaction engine |

---

## 17. Error Code Mapping

SQL errors from different databases must be mapped to a common error hierarchy. Each driver adapter extracts the error code and maps it.

### Error Codes by Dialect

| Error | PostgreSQL | MySQL | SQLite |
|-------|-----------|-------|--------|
| Unique violation | `23505` | `1062` | `2067` (`SQLITE_CONSTRAINT_UNIQUE`) |
| FK violation | `23503` | `1451` / `1452` | `787` (`SQLITE_CONSTRAINT_FOREIGNKEY`) |
| Check violation | `23514` | `3819` | `275` (`SQLITE_CONSTRAINT_CHECK`) |
| Not null violation | `23502` | `1048` | `1299` (`SQLITE_CONSTRAINT_NOTNULL`) |
| Deadlock | `40P01` | `1213` | N/A (file-level locking) |
| Connection failed | `08*` class | `2002`, `2006`, `1040` | `5` (`SQLITE_BUSY`), `14` (`SQLITE_CANTOPEN`) |

### Driver Error Types

| Driver | Error Type | Metadata Available |
|--------|-----------|-------------------|
| pgx | `*pgconn.PgError` | `ConstraintName`, `ColumnName`, `Detail`, `TableName`, `SchemaName` — first-class fields |
| lib/pq | `*pq.Error` | Same fields as pgx (same PostgreSQL wire protocol) |
| go-sql-driver/mysql | `*mysql.MySQLError` | `Number`, `SQLState`, `Message` only — constraint name must be parsed from message string |
| modernc.org/sqlite | `*sqlite.Error` | Code only — constraint and column parsed from error message |

### Error Wrapping Convention

All mapped errors follow this wrapping format:

```
"{operation} {table}: {cause}"
```

Example: `"create product: unique constraint violation on column email"`.

---

## 18. Dialect Feature Matrix

Quick reference for what each dialect supports.

### SQL Features

| Feature | PostgreSQL | MySQL | SQLite |
|---------|-----------|-------|--------|
| `RETURNING` | Yes | No | Yes (3.35+) |
| Explicit upsert conflict target | Yes | No (triggers on any unique) | Yes |
| Named ENUM types | Yes | No (inline only) | No |
| Composite types | Yes | No | No |
| Domain types | Yes | No | No |
| Array types (`text[]`, etc.) | Yes | No | No |
| JSONB operators (`@>`, `?`, etc.) | Yes | No | No |
| JSON functions | Yes | Yes (`JSON_CONTAINS`, etc.) | Yes (`json_extract`, `->`, `->>` since 3.38.0) |
| Multi-schema | Yes | No (databases only) | No |
| `COMMENT ON` | Yes | No | No |
| `ALTER TABLE DROP COLUMN` | Yes | Yes | Yes (3.35+) |
| `ALTER TABLE MODIFY COLUMN` | Yes | Yes | No |
| Partial unique indexes | Yes | No | Yes (3.8.0+) |
| `DEFAULT` in VALUES clause | Yes | Yes | No (use `sql.DefaultExpr` instead) |

### Transaction Features

| Feature | PostgreSQL | MySQL | SQLite |
|---------|-----------|-------|--------|
| Isolation level selection | Yes (4 levels) | Yes (4 levels) | No (always SERIALIZABLE) |
| `READ ONLY` | Yes | Yes | No |
| `DEFERRABLE` | Yes | No | No |
| Savepoints | Yes | Yes | Yes (different rollback syntax) |

### Type System

| Feature | PostgreSQL | MySQL | SQLite |
|---------|-----------|-------|--------|
| UUID native type | Yes | `CHAR(36)` / `BINARY(16)` | Text affinity |
| Array columns | Yes | No | No |
| JSONB (binary JSON) | Yes | No | No |
| JSON | Yes | Yes | No |
| `inet`, `cidr`, `macaddr` | Yes | No | No |
| `interval` | Yes | No | No |
| Unsigned integers | No | Yes | No |
| Type affinity | No | No | Yes |

### Driver Capabilities

| Feature | pgx | stdlib |
|---------|-----|--------|
| Native array parameters (`= ANY($1)`) | Yes | No (must expand) |
| `SendBatch` pipelining | Yes | No |
| Native `RETURNING` scan | Yes | Via `QueryRow` |
| `LastInsertId()` | No (uses RETURNING) | Yes |

---

## 19. Adding a New Builder Function

When adding a new SQL builder or modifying an existing one:

### Checklist

1. **Define the signature.** Follow the pattern: `func Build<Operation>(d Dialect, t Table, ...) (string, []any)`. Return SQL string and argument slice.
2. **Implement for all dialects.** If the operation requires dialect-specific SQL (e.g., different upsert syntax), add a method to the `Dialect` interface and implement it in all three dialects.
3. **Parameterize all values.** Use `Dialect.Placeholder(pos)` for every value. Track position correctly. Never interpolate values into SQL strings.
4. **Quote all identifiers.** Use `Dialect.QuoteIdentifier()` for columns, `Dialect.FormatTable()` for tables.
5. **Sort column lists.** Column order in `SELECT`, `INSERT`, and `SET` clauses must be deterministic.
6. **Write table-driven tests.** Test all three dialects in the same test function using the dialect as a test case parameter. Verify both the SQL string and the argument slice.
7. **Test edge cases.** Empty condition lists, single-column tables, composite primary keys, nullable columns, tables with no schema.
8. **Run `gosec`.** Verify the linter does not flag the new builder for SQL injection patterns.

### Test Pattern

```go
func TestBuildNewOperation(t *testing.T) {
    tests := []struct {
        name    string
        dialect sql.Dialect
        table   sql.Table
        // ... operation-specific inputs
        wantSQL  string
        wantArgs []any
    }{
        {
            name:    "postgres basic",
            dialect: sql.NewPostgresDialect(),
            table:   sql.Table{Schema: "public", Name: "users"},
            // ...
            wantSQL:  `... expected SQL for PostgreSQL ...`,
            wantArgs: []any{...},
        },
        {
            name:    "mysql basic",
            dialect: sql.NewMySQLDialect(),
            table:   sql.Table{Name: "users"},
            // ...
            wantSQL:  `... expected SQL for MySQL ...`,
            wantArgs: []any{...},
        },
        {
            name:    "sqlite basic",
            dialect: sql.NewSQLiteDialect(),
            table:   sql.Table{Name: "users"},
            // ...
            wantSQL:  `... expected SQL for SQLite ...`,
            wantArgs: []any{...},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotSQL, gotArgs := sql.BuildNewOperation(tt.dialect, tt.table, ...)
            if gotSQL != tt.wantSQL {
                t.Errorf("BuildNewOperation() sql = %q, want %q", gotSQL, tt.wantSQL)
            }
            if diff := cmp.Diff(tt.wantArgs, gotArgs); diff != "" {
                t.Errorf("BuildNewOperation() args mismatch (-want +got):\n%s", diff)
            }
        })
    }
}
```

---

## Appendix: SQL Review Checklist

- [ ] All values are parameterized via `Dialect.Placeholder()` — no string interpolation
- [ ] All identifiers are quoted via `QuoteIdentifier()` or `FormatTable()`
- [ ] SQL output is tested for all three dialects (PostgreSQL, MySQL, SQLite)
- [ ] Argument slice matches placeholder positions exactly
- [ ] Column lists are sorted for deterministic output
- [ ] `RETURNING` is conditional on `Dialect.SupportsReturning()`
- [ ] IN/NOT IN uses `sql.Where(col).In()` / `Nin()` or the comparators' `parseIn` / `parseNin` (not hand-built)
- [ ] Edge cases tested: empty conditions, single column, composite PK, no schema
- [ ] `gosec` passes without suppression
- [ ] MySQL workarounds documented where `RETURNING` is unavailable
- [ ] Dialect-specific operators (JSONB, arrays) are gated to the correct dialect
