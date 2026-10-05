# SQLGen — Implementation Order

> Reference document for implementing SQLGen in dependency order.
> Each phase builds on the previous. Within a phase, items are ordered by internal dependency.

---

## Phase 0: Foundation

### 0.0 Project Scaffolding

**What to build:**
- Initialize the 3-module Go workspace with directory structure per `guidelines/ARCHITECTURE.md`:
  - Runtime module (`go.mod` at root): `comparator/`, `database/`, `database/pgx/`, `database/stdlib/`, `database/mock/`, `omittable/`, `sql/`, `metrics/otel/`
  - Parser module (`parser/go.mod`): `parser/`, `parser/postgres/`, `parser/mysql/`, `parser/sqlite/`, `parser/introspect/`
  - CLI module (`cmd/sqlgen/go.mod`): `cmd/sqlgen/`, `cmd/sqlgen/config/`, `cmd/sqlgen/gen/`, `cmd/sqlgen/gen/templates/`, `cmd/sqlgen/gotype/`, `cmd/sqlgen/gotype/uuidgoogle/`, `cmd/sqlgen/gotype/uuidgofrs/`, `cmd/sqlgen/gotype/decimal/`
  - `go.work` for local multi-module development
- `.golangci.yml` with all linter rules, `depguard` module boundary enforcement, and issue exclusions
- `.github/workflows/lint.yml` — golangci-lint CI workflow
- `.github/workflows/test.yml` — unit + integration test CI workflow
- `.github/workflows/release-please.yml` — automated release management
- `.github/workflows/release.yml` — GoReleaser binary builds triggered by tags
- `.goreleaser.yml` — cross-platform binary configuration (linux/darwin/windows × amd64/arm64)
- `release-please-config.json` and `.release-please-manifest.json`

**Depends on:** Nothing

**PRD Reference:** Appendix C, Section 3, `guidelines/ARCHITECTURE.md` (Module Layout)

---

### Foundation Libraries

Pure libraries with no I/O, no config, no codegen. These are leaf nodes — everything else depends on them.

### 0.1 `omittable/` — Presence-Tracking Wrapper

**Module:** Runtime (`github.com/teandresmith/sqlgen`)

**What to build:**
- `Value[T]` generic struct with `value T` and `set bool` fields
- `Set[T](v T) Value[T]` constructor
- `Omit[T]() Value[T]` constructor
- `Get() (T, bool)`, `IsSet() bool`, `MustGet() T` methods
- `json.Marshaler` / `json.Unmarshaler` implementation
  - `Set(v)` marshals to `v`
  - Unset value is omitted from JSON output via `IsZero()` method (use `omitzero` struct tag)
  - Absent JSON field unmarshals to unset
  - `null` JSON field unmarshals to `Set(nil)` for pointer types

**Tests:**
- Zero value distinction: `Set("")` vs `Omit[string]()`, `Set(0)` vs `Omit[int]()`, `Set(false)` vs `Omit[bool]()`
- JSON round-trip for all common types: `string`, `int`, `float64`, `bool`, `*string`, `time.Time`, `[]byte`
- `MustGet()` panics when not set

**Depends on:** Nothing (stdlib only)

**PRD Reference:** Section 10

---

### 0.2 `sql/` — Dialect Interface & Core Types

**Module:** Runtime

**What to build:**
- `Table` struct (`Schema string`, `Name string`)
- `Dialect` interface:
  - `Name() string`
  - `Placeholder(position int) string`
  - `PlaceholderList(start, count int) string`
  - `QuoteIdentifier(name string) string`
  - `FormatTable(table Table) string`
  - `SupportsReturning() bool`
  - `ReturningClause(columns []string) string`
  - `UpsertClause(conflictKeys, updateColumns []string) string`
  - `SupportsArrayParams() bool`
  - `SupportsTupleIN() bool`
- `Condition` struct (`Clause string`, `Value any`)
- Composition types: `And`, `Or`, `Subquery`, `Range`
- `Sort` struct (`Column string`, `Direction string`)
- `ConditionBuilder` — fluent API:
  - `Where(column) → builder`
  - `Eq`, `Neq`, `Gt`, `Gte`, `Lt`, `Lte`, `In`, `Nin`, `Like`, `NLike`, `Between`, `IsNull`, `IsNotNull`
  - `And(conditions...)`, `Or(conditions...)`, `Raw(sql, args...)`

**Tests:**
- `ConditionBuilder` produces correct `Condition` structs
- `And`/`Or` composition nesting
- `Where("col").Eq(v)` → `Condition{Clause: "col = $", Value: v}`

**Depends on:** Nothing (stdlib only)

**PRD Reference:** Sections 11.5, Appendix A

---

### 0.3 `comparator/` — Type-Safe Filter Operators

**Module:** Runtime

**What to build:**

Universal comparators (all dialects):
- `ID` — `Eq`, `Neq`, `In`, `Nin`, `Gt`, `Gte`, `Lt`, `Lte`, `Custom`
- `NullableID` — all of ID + `Null`
- `String` — all of ID + `Contains`, `StartsWith`, `EndsWith`, `Like`, `NLike`
- `NullableString` — all of String + `Null`
- `Number[T]` — `Eq`, `Neq`, `In`, `Nin`, `Gt`, `Gte`, `Lt`, `Lte`, `Between`, `NBetween`, `Custom`
- `NullableNumber[T]` — all of Number + `Null`
- `Bool` / `NullableBool` — `Eq`, `Neq`, `Custom` (+ `Null`)
- `Time` / `NullableTime` — same operators as Number (+ `Null`)
- `Enum[T]` / `NullableEnum[T]` — `Eq`, `Neq`, `In`, `Nin`, `Custom` (+ `Null`)
- `JSON` / `NullableJSON` — `Contains`, `HasKey`, `Custom` (+ `Null`)

PostgreSQL-only comparators:
- `JSONB` / `NullableJSONB` — `HasKey`, `HasAnyKey`, `HasAllKeys`, `Contains`, `ContainedBy`, `PathExists`, `Custom` (+ `Null`)
- `Slice[T]` / `NullableSlice[T]` — `ContainsAny`, `ContainsAll`, `ContainedBy`, `IsEmpty`, `Custom` (+ `Null`)

Each comparator implements:
```go
Parse(columnName string, dialect sql.Dialect) []sql.Condition
```

**Tests:**
- Each comparator type produces correct `sql.Condition` output
- Dialect-aware behavior for `In`/`Nin` (ANY vs expanded IN)
- JSON/JSONB dialect-specific SQL (PostgreSQL vs MySQL operators)
- Null comparator adds `IS NULL` / `IS NOT NULL`

**Depends on:** `sql/` (Condition, Dialect)

**PRD Reference:** Sections 11.2, 11.3

---

## Phase 1: SQL Builder Functions

### 1.1 Dialect Implementations

**Module:** Runtime

**What to build:**
- `PostgresDialect` (with driver awareness: pgx vs stdlib for IN/NOT IN behavior)
- `MySQLDialect`
- `SQLiteDialect`

**Tests per dialect:**
- `Placeholder()` → `$1` vs `?`
- `QuoteIdentifier()` → `"double"` vs `` `backtick` ``
- `FormatTable()` → `"public"."users"` vs `` `users` `` vs `"users"`
- `SupportsReturning()` → true/false
- `UpsertClause()` → `ON CONFLICT ... DO UPDATE SET` vs `ON DUPLICATE KEY UPDATE`

**Depends on:** `sql/` (Dialect interface)

**Follow-up:** Update `comparator/` tests to replace `testDialect` stubs with real dialect implementations and add SQLite dialect coverage.

**PRD Reference:** Appendix B

---

### 1.2 Builder Functions

**Module:** Runtime

**What to build:**
- `BuildSelect(dialect, table, opts) → (sql string, args []any)`
  - Column selection, WHERE from conditions, ORDER BY, LIMIT, OFFSET
- `BuildSelectJoin(dialect, table, joins, opts)` — O2O LEFT JOIN with aliased columns
- `BuildInsert(dialect, table, columns, values, opts)` — single row + optional RETURNING
- `BuildMultiInsert(dialect, table, columns, valueRows, opts)` — multi-row VALUES
- `BuildUpdate(dialect, table, setClauses, conditions, opts)` — partial update + optional RETURNING
- `BuildCount(dialect, table, conditions)`
- `BuildExists(dialect, table, conditions)`
- `BuildIncrement(dialect, table, column, amount, conditions)`
- `BuildSoftDelete(dialect, table, opts)` — SoftDeleteOptions carries column, type, conditions, RETURNING
- `BuildHardDelete(dialect, table, conditions)`
- `BuildColumnsFromFieldOptions(fieldOptionsMap map[string]string)` — resolves selected columns

**Tests per function per dialect:**
- Correct SQL output with varying inputs
- Empty conditions → no WHERE clause
- Schema-qualified table names
- Composite PK WHERE clauses (tuple IN for PostgreSQL, expanded OR for MySQL)
- Edge cases: empty IN list, nil FieldOptions, zero-length batch

**Depends on:** Phase 0 (`sql.Dialect`, `sql.Table`, `sql.Condition`)

**PRD Reference:** Appendix A, Section 8.7

---

## Phase 2: Database Abstraction Layer

### 2.1 `database/` — Core Interfaces & Transaction Engine

**Module:** Runtime

**What to build:**

Interfaces:
- `Querier` — `Exec`, `Query`, `QueryRow`, `Begin`
- `Result` — `RowsAffected`, `LastInsertId`
- `Row` — `Scan`
- `Rows` — `Next`, `Scan`, `Columns`, `Close`, `Err`

Transaction engine:
- `Tx` struct — wraps underlying transaction, `sync.Mutex` protected, satisfies `Querier`
  - `Exec`, `Query`, `QueryRow` — delegate to underlying transaction
  - `Begin` — creates a savepoint (increments depth), enables transparent nesting via `Conn()`
  - `IsClosed() bool`
  - `OnCommit(fn func(ctx context.Context) error)` — callback registry
  - Internal: `depth int`, `callbacks [][]func(...)` (depth-indexed stack)
- `TxOptions` — `IsoLevel`, `AccessMode`, `DeferrableMode`, `Timeout`
- `CallbackMode` — `async` (default) / `sync`
- `NewTransaction(ctx, querier, name, ...TxOptions) (context.Context, error)` — begin or savepoint
- `FromContext(ctx) *Tx`
- `Commit(ctx) error` — root: COMMIT + fire callbacks; savepoint: RELEASE + promote callbacks
- `Rollback(ctx) error` — root: ROLLBACK + discard; savepoint: ROLLBACK TO + discard at depth
- `WithTransaction(ctx, querier, name, fn, ...TxOptions) error` — auto begin/commit/rollback
- `Conn(ctx, querier) Querier` — returns Tx if open, else original querier

Convenience:
- `QueryFunc(ctx, sql, args, fn)` — query + auto-close rows
- `QueryRowFunc(ctx, sql, args, fn)` — single row with callback

**Tests:**
- Transaction lifecycle: begin → operations → commit
- Savepoint nesting: begin → savepoint → release → commit
- Savepoint rollback: begin → savepoint → rollback to savepoint → commit (outer survives)
- `OnCommit` callback ordering: registration order preserved
- `OnCommit` promotion: savepoint release promotes callbacks to parent depth
- `OnCommit` discard: savepoint rollback discards callbacks at that depth
- Root rollback discards all callbacks at all depths
- `Conn()` returns Tx when open, fallback when closed
- `IsClosed()` after commit/rollback
- Concurrent safety with `-race` flag

**Depends on:** stdlib only (`context`, `sync`, `fmt`)

**PRD Reference:** Section 18, Section 19.1

---

### 2.2 `database/pgx/` — pgx Driver Adapter

**Module:** Runtime

**What to build:**
- `New(pool *pgxpool.Pool) database.Querier`
- Wraps `pgxpool.Pool` to satisfy `Querier` interface
- `SendBatch` support for batch operations (UpdateMany pipelining)
- Transaction creation via `pool.Begin(ctx)`
- Error mapping: `*pgconn.PgError` → `ConstraintError` (deferred to Phase 3 integration)

**Tests:**
- Integration tests with testcontainers (PostgreSQL)
- Implements all Querier methods correctly
- Batch pipelining via `SendBatch`

**Depends on:** `database/`, `jackc/pgx/v5`

**PRD Reference:** Section 19.2

---

### 2.3 `database/stdlib/` — stdlib Driver Adapter

**Module:** Runtime

**What to build:**
- `New(db *sql.DB) database.Querier`
- Wraps `*sql.DB` to satisfy `Querier` interface
- Transaction creation via `db.BeginTx(ctx, opts)`
- Savepoints via raw SQL (`SAVEPOINT`, `RELEASE SAVEPOINT`, `ROLLBACK TO SAVEPOINT`)

**Tests:**
- Integration tests with testcontainers (MySQL) and in-memory SQLite
- Savepoint SQL correctness per dialect

**Depends on:** `database/`, `database/sql`

**PRD Reference:** Section 19.2

---

### 2.4 `database/mock/` — Mock Driver

**Module:** Runtime

**What to build:**
- `New() *MockQuerier`
- Configurable function fields: `ExecFn`, `QueryFn`, `QueryRowFn`, `BeginFn`
- Mock `Row`, `Rows`, `Result` types

**Tests:**
- Verify configurable functions are called
- Default behavior (nil functions) returns reasonable defaults

**Depends on:** `database/` only

**PRD Reference:** Section 19.2

---

## Phase 3: Error System

### 3.1 Error Types

**Module:** Runtime

**What to build:**
- Sentinel errors:
  - `ErrNotFound`
  - `ErrEmptyFilter`
  - `ErrAmbiguousFilter`
  - `ErrInvalidCursor`
  - `ErrDeadlock`
  - `ErrConnectionFailed`
  - `ErrConstraintViolation`
- `ConstraintError` struct:
  - `Type ConstraintType` (Unique, ForeignKey, Check, NotNull)
  - `Constraint string`, `Column string`, `Detail string`
  - `Err error` (original driver error)
  - `Error() string`, `Unwrap() error` (unwraps to `ErrConstraintViolation`)
- `ConstraintType` constants

**Tests:**
- `errors.Is(constraintErr, ErrConstraintViolation)` returns true
- `errors.As(err, &ConstraintError{})` extracts fields
- Error message formatting

**Depends on:** stdlib `errors`

**PRD Reference:** Section 22

---

### 3.2 Driver Error Mapping

**Module:** Runtime (within each driver sub-package)

**What to build:**

In `database/pgx/`:
- `*pgconn.PgError` → `ConstraintError` mapping
- Code mapping: `23505` → Unique, `23503` → FK, `23502` → NotNull, `23514` → Check
- `40P01` → `ErrDeadlock`
- `08*` class → `ErrConnectionFailed`

In `database/stdlib/`:
- MySQL: `*mysql.MySQLError` → `ConstraintError`
  - `1062` → Unique, `1451`/`1452` → FK, `1048` → NotNull, `3819` → Check
  - `1213` → `ErrDeadlock`, `2002`/`2006` → `ErrConnectionFailed`
  - Message parsing for constraint name, column name
- SQLite (modernc): `*sqlite.Error` → `ConstraintError`
  - `2067` → Unique, `787` → FK, `1299` → NotNull, `275` → Check
  - Message parsing for constraint/column
- Note: mattn/go-sqlite3 is not supported (requires CGO); errors pass through unmapped
- `*pq.Error` → same as pgx mapping (same PostgreSQL codes)

**Tests:**
- Unit: each driver error type maps to correct `ConstraintType`
- Unit: MySQL/SQLite message parsing extracts constraint/column names
- Unit: graceful fallback when message format is unrecognized (empty fields, Type still set)
- Integration: trigger real constraint violations against each DB, verify `ConstraintError` fields

**Depends on:** Error types, driver packages

**PRD Reference:** Section 22.2, 22.5

---

## Phase 4: Parser Module (separate Go module)

**Module:** Parser (`github.com/teandresmith/sqlgen/parser`)

### 4.1 Schema Model

**What to build:**
- `Schema` — top-level container
- `Table` — name, schema, columns, constraints, comment
- `Column` — name, type, nullable, PK, unique, default, FK reference, auto-increment, comment
- `Constraint` — type (PK, FK, UNIQUE, CHECK, INDEX), columns, reference table/columns, name
- `Enum` — name, values
- `CompositeType` — name, attributes (name + type pairs)
- `DomainType` — name, base type, constraints
- `Relationship` — name, type (O2O/O2M/M2M), source table, target table, FK column, junction table, junction FKs, filter, sort
- `View` — name, SQL, columns (inferred)

**Tests:**
- Schema model construction and field access
- Deterministic sorting of all elements (tables, columns, enums)

**Depends on:** Nothing

**PRD Reference:** Section 5.1

---

### 4.2 PostgreSQL Parser

**What to build:**
- Statement-by-statement parsing via `pg_query_go/v6`, applied in file order as PostgreSQL applies them (PRD §5.2):
  - `CREATE TABLE`, `CREATE TYPE` (enum, composite, domain)
  - `COMMENT ON`, `ALTER TABLE` (ADD/DROP/MODIFY COLUMN, ADD/DROP CONSTRAINT)
- Schema qualification normalization (bare name → `input.schema` prefix)
- Full column metadata extraction (type, nullable, default, PK, unique, FK, comment)
- Auto-increment detection: `serial`/`bigserial`/`smallserial` and `GENERATED { ALWAYS | BY DEFAULT } AS IDENTITY`

**Tests:**
- All DDL statement types
- ALTER TABLE operations
- Enum, composite type, domain type parsing
- Schema-qualified vs bare table names
- Reserved word column names, quoted identifiers
- Serial and identity column detection
- Empty tables, tables with no PK

**Depends on:** Schema model, `pg_query_go/v6` (CGO)

**PRD Reference:** Section 5.2

---

### 4.3 MySQL Parser

**What to build:**
- Parsing via `vitess/sqlparser`:
  - `CREATE TABLE` with columns, constraints, indexes, inline comments
  - Inline `ENUM` values from column type definitions
  - `ALTER TABLE`: ADD/DROP/MODIFY COLUMN, ADD/DROP INDEX, ADD/DROP CONSTRAINT

**Tests:**
- All supported DDL
- Inline enum extraction
- Unsigned integer types
- `AUTO_INCREMENT` detection

**Depends on:** Schema model, `vitess/sqlparser`

**PRD Reference:** Section 5.2

---

### 4.4 SQLite Parser

**What to build:**
- Parsing via `rqlite/sql`:
  - `CREATE TABLE` with columns and constraints
  - `ALTER TABLE`: ADD COLUMN, RENAME COLUMN, RENAME TABLE only
  - Type affinity handling (defaults to BLOB)

**Tests:**
- Supported DDL and ALTER TABLE subset
- Type affinity edge cases

**Depends on:** Schema model, `rqlite/sql`

**PRD Reference:** Section 5.2

---

### 4.5 Relationship Detection

**What to build:**
- Post-parse pass over all tables:
  - **M2M:** Table has exactly 2 FK columns, both in composite PK/UNIQUE → bidirectional relationship
  - **O2O:** Column has FK + uniqueness — inline `UNIQUE`, a single-column `UNIQUE` constraint, or being the table's sole PRIMARY KEY column (PRD §13.1)
  - **O2M:** Column has FK, not unique (fallback)
- Self-referential relationship handling
- Circular relationship detection (A→B→A)

**Tests:**
- Each relationship type detected correctly
- Junction table with composite PK → M2M
- Self-referential FK → O2O or O2M based on UNIQUE
- Table with 3+ FKs in PK → not M2M (not exactly 2)

**Depends on:** Schema model (post-parse)

**PRD Reference:** Section 5.3

---

### 4.6 Soft Delete & Update Column Detection

**What to build:**
- Check each table's columns against `soft_delete_columns` config list in priority order
- First match determines soft delete strategy (timestamp, bool, integer)
- Check columns against `update_columns` list for auto-set on update

**Tests:**
- Priority ordering (deleted_at wins over is_deleted)
- Type validation (varchar column with soft delete name → validation error)
- No match → no soft delete for that table

**Depends on:** Schema model, config (soft_delete_columns, update_columns)

**PRD Reference:** Sections 5.4, 17.1

---

### 4.7 Multi-File Parsing

**What to build:**
- File ordering: lexicographic within directories, specified order across paths
- Down-file filtering: skip `*.down.sql`, `*_down.sql`; process `*.up.sql`, `*_up.sql`
- Sequential migration application: files parsed in order, statements applied to accumulating schema
- Duplicate `CREATE` detection: table, enum, composite type, domain type → validation error if already exists
- `DROP TABLE` / `DROP TYPE` with referential validation (FK references, column type usage)

**Tests:**
- File ordering correctness
- Down-file patterns skipped
- Duplicate CREATE TABLE → error
- Duplicate CREATE TYPE → error
- Sequential CREATE + ALTER ADD COLUMN → correct final schema
- Sequential CREATE + DROP TABLE → table removed
- DROP TABLE with FK reference → error
- DROP TYPE with column reference → error

**Depends on:** Dialect parsers

**PRD Reference:** Section 5.6

---

### 4.8 Database Introspection

**What to build:**
- `Introspector` interface: `Introspect(ctx, connString, schema, opts) error`, `Close() error`
- PostgreSQL introspector: `information_schema` + `pg_catalog` queries
- MySQL introspector: `information_schema` queries
- SQLite introspector: `PRAGMA` statements
- Schema/table filtering via `IntrospectConfig`
- `both` mode: introspect DB → parse files → merge overlay

**Tests:**
- Integration: parse schema file → introspect same schema from live DB → verify identical schema model
- Filtering: include/exclude schemas and tables
- `both` mode: DB base + file overlay → correct merged schema
- `both` mode: file CREATE TABLE on existing DB table → validation error

**Depends on:** Schema model, database drivers

**PRD Reference:** Section 6

---

## Phase 5: Configuration & Type System (CLI module)

**Module:** CLI (`github.com/teandresmith/sqlgen/cmd/sqlgen`)

### 5.1 Config Loading

**What to build:**
- YAML loading from `sqlgen.yml` / `sqlgen.yaml`
- All config structs: `RootConfig`, `InputConfig`, `OutputConfig`, `GenerationConfig`, `OverrideConfig`, `TableConfig`, `ViewConfig`, `ExtraType`
- Defaults application (all fields from Section 4)
- Environment variable overrides: `SQLGEN_DB_URL`, `SQLGEN_DB_PASSWORD`
- Per-table generation override resolution (table-level → global → built-in default)

**Tests:**
- Minimal config loads with correct defaults
- Full config loads all fields
- Env vars override config values
- Per-table override resolution order

**Depends on:** YAML library (e.g., `gopkg.in/yaml.v3`)

**PRD Reference:** Section 4

---

### 5.2 Config Validation — Phase 1 (Pre-Parse)

**What to build:**
- Driver/dialect mismatch (pgx + MySQL → error)
- Missing `connection` for `database`/`both` source
- Invalid `operations` preset names
- Duplicate explicit `struct_name` overrides
- `input.schema` on MySQL/SQLite → warning
- All errors reported together (not one at a time)

**Tests:**
- Each validation rule: positive (valid) and negative (error/warning) cases
- Multiple errors reported in one pass

**Depends on:** `config/`

**PRD Reference:** Section 4.13 (phase 1 rules)

---

### 5.3 `gotype/` — SQL-to-Go Type Mapping

**What to build:**
- Type resolution chain implementation:
  1. Table-level `type_map`
  2. Table-level `overrides.types`
  3. Global `overrides.types`
  4. Built-in optional type (UUID/decimal)
  5. Built-in dialect mapping
- Per-dialect default mappings (all types from Section 7.2)
- Nullable handling: `use_pointers: true` → `*T`, `false` → `sql.Null*`
- Array type mapping (`T[]` → `[]GoT`)
- FK string conversion derivation (`.String()` → `.Valid` → `fmt.Sprint()`)

**Tests:**
- Each precedence level overrides the ones below
- All PostgreSQL/MySQL/SQLite type mappings
- Nullable with pointers vs sql.Null types
- Array types with and without overrides

**Depends on:** `config/`

**PRD Reference:** Section 7

---

### 5.4 Built-In Type Integrations

**What to build:**
- `gotype/uuidgoogle/` — `github.com/google/uuid` mapping
  - `uuid.UUID`, `uuid.NullUUID`, zero value `uuid.UUID{}`
  - v4 and v7 generation support
- `gotype/uuidgofrs/` — `github.com/gofrs/uuid/v5` mapping
  - `uuid.UUID`, `uuid.NullUUID`, zero value `uuid.Nil`
- `gotype/decimal/` — `github.com/shopspring/decimal` mapping
  - `decimal.Decimal`, `decimal.NullDecimal`, zero value `decimal.Decimal{}`

**Tests:**
- Each integration resolves correct Go type, import, zero value, nullable variant
- Scanner/Valuer compliance

**Depends on:** `gotype/`

**PRD Reference:** Section 7.4

---

### 5.5 Config Validation — Phase 2 (Post-Parse)

**What to build:**
- `cursor_keys` referencing non-existent columns
- `soft_delete_columns` type mismatch (configured name matches column but incompatible type)
- Struct name collisions from auto-detection (PascalCase normalization)
- `exclude_columns` removes all columns → warning, skip table
- Ambiguous bare table name in config when multiple schemas contain the same table name → validation error

**Tests:**
- Each rule: positive and negative cases
- Runs after schema model is available

**Depends on:** `config/`, `parser/` schema model

**PRD Reference:** Sections 4.13 (phase 2 rules), 5.5 (schema qualification ambiguity)

---

## Phase 6: Code Generation Engine

**Module:** CLI

### 6.1 Generator Scaffolding

**What to build:**
- `gen/gen.go` — orchestrator: calls each generator in the fixed sequence (enums → types → errors → tables → sorters → pagination → connections → views → unified client)
- `gen/format.go` — formatting pipeline: template execution → header injection → `goimports` → file write (`0o600` permissions, `os.MkdirAll` for directories)
- `gen/funcmap.go` — custom template function registry (initially empty, populated in 6.3)
- Generated file header: `// Code generated by sqlgen vX.Y.Z. DO NOT EDIT.`

**Tests:**
- Orchestrator calls generators in correct order
- Formatting pipeline: raw template output → valid Go file
- `goimports` failure includes raw template output in error message
- Header is first line of every generated file

**Depends on:** `config/`, `parser/`, `gotype/`, `golang.org/x/tools/imports`

**PRD Reference:** Section 8.1

---

### 6.2 Naming Engine

**What to build:**
- Naming helpers in `gen/naming.go`, over the sqlgen-owned caser and acronym set in
  `gen/acronyms.go` (PRD §8.5):
  - `toCamelCase(s string) string` → `identCaser.ToCamel`
  - `toPascalCase(s string) string` → `identCaser.ToPascal`
  - `toSnakeName(s string) string` → `snakeCaser.ToSnake` — spells snake_case from the SQL name
  - `toSnakeCase(s string) string` → `splitIdentForSnake` — reads a Go identifier back, longest
    acronym match; the `struct_name` override path only (FIX-154)
  - `toSingular(s string) string` / `toPlural(s string) string` → the sqlgen-owned inflector in
    `gen/inflect.go`; a final canonical acronym is frozen in the singular direction and the plural
    direction always appends (FIX-161)
- The acronym set is `canonicalAcronyms` in `gen/acronyms.go`, passed to the caser directly. No
  casing or inflection state is process-global: FIX-154 removed the `flect.LoadAcronyms` mutation
  and FIX-161 removed the dependency, whose `init()` also read `inflections.json` from the working
  directory.
- `StructName` / `SnakeName` / `TableConstantName` / `StructNamePlural` / `FieldName` helpers that
  compose those functions
- Multi-schema naming: prefix struct names with schema when multiple schemas present

**Tests:**
- All examples from Section 8.5 (user_id → UserID, ip_address → IPAddress, etc.)
- Singular/plural edge cases (companies → Company, statuses → Status)
- Multi-schema: single schema → no prefix, multiple schemas → prefix

**Depends on:** `gen/acronyms.go`, `ettle/strcase` (identifier casing only)

**PRD Reference:** Section 8.5

---

### 6.3 Template Context Builders & Function Map

**What to build:**

Context structs (pre-computed data for templates):
- `EnumContext` — enum name, Go type name, values
- `TypeContext` — composite/domain/extra type data
- `TableContext` — all data needed for a table's templates:
  - Struct name, table name, schema, columns with resolved Go types
  - Doc comment per table (resolved: config `description` > parsed SQL `COMMENT ON TABLE` > none)
  - Doc comment per column (resolved: config `column_map.<col>.description` > parsed SQL `COMMENT ON COLUMN` > none)
  - PK columns, PK strategy per column, composite PK struct (if applicable)
  - Soft delete column + type (if detected)
  - Update columns (auto-set)
  - Relationships (O2O, O2M, M2M) with resolved types
  - Enabled operations (from config)
  - Conflict targets (from UNIQUE constraints)
  - IncrementColumn candidates (numeric non-PK columns)
  - Filter field → comparator type mapping
  - FieldOptions fields (columns + relationship fields)
  - CreateInput field classification (required, omittable, excluded)
  - UpdateInput fields (all omittable, excluded computed/auto columns)
  - Scan target shape per column (direct, wrapped, intermediate)
- `ViewContext` — similar to TableContext but read-only subset
- `ClientContext` — list of all entity clients for unified client template

Function map additions:
- **Naming:** `toPascalCase`, `toCamelCase`, `structNamePlural`, `toSingular`, `safeGoIdent`
- **Types:** `goType`, `isNullableType`, `comparatorType`, `zeroValue`
- **Soft Delete:** `softDeleteSwitch`, `softDeleteValue`
- **Relationships:** `fkStringExpr`, `fkNullGuard`, `isM2M`
- **Inputs:** `createInputFieldType`, `updateInputFieldType`, `pkAutoGenType`
- **Dialect:** `placeholder`, `quoteIdentifier`, `supportsReturning`

**Tests:**
- Context builders produce correct data from representative schemas
- Each funcmap function is pure and deterministic

**Depends on:** `gen/`, `gotype/`, `parser/` schema

**PRD Reference:** Sections 4.8 (Doc Comment Resolution), 8.1.2, 8.1.3

#### 6.3a `sql.Default` Sentinel Value

**PRD Reference:** Section 9.8.5 (CreateMany), Appendix A (SQL Builder Functions)

**What:** Add a sentinel value to the `sql` package that `BuildMultiInsert` recognizes. When encountered in a value row, it emits the SQL `DEFAULT` keyword instead of a placeholder. Enables multi-row INSERTs where omittable fields differ per row while keeping all rows in the same column list.

**Depends on:** 6.1 (generator scaffolding), Phase 1 (sql package)

**Produces:** `sql/default.go` with sentinel type and value, updated `BuildMultiInsert` logic

#### 6.3b Shared Generic Types

**PRD Reference:** Section 9.6 (CallOptions), Section 9.4 (PaginateInput, ConnectionInput, IncrementInput, PaginateResult), Section 9.8 (resolveCallOptions, toAnySlice)

**What:** Design and implement the template context for shared generic types generated once per package: `CallOptions[FO]`, `PaginateInput[F]`, `ConnectionInput[F]`, `IncrementInput[C]`, `PaginateResult[T]`, plus `resolveCallOptions` and `toAnySlice` helpers.

**Depends on:** 6.3 (template context builders)

**Produces:** Template context additions for shared types, template for shared types file

#### 6.3c Context Alignment with PRD Section 9.8

**PRD Reference:** Sections 9.8 (Design Principle 1), 10.3 (CreateInput classification), 11.1 (filter struct), 9.8 (soft delete scoping)

**What:** Fix existing context builders to match updated PRD: UUID PKs are `omittable.Value[T]` in CreateInput (not excluded), update_columns are omittable (not excluded) in both Create/Update inputs, Get/GetMany/Count are always-on operations, and new context fields (`ExcludeDeleted`, `IsSoftDeleteColumn`, `JSONTag`, `exclude_deleted` config) support template generation for soft delete scoping and struct tags.

**Depends on:** 6.3 (context builders exist), 6.3b (shared types designed)

**Produces:** Updated `context_table.go` (input classification, operations, filter fields), updated `context.go` (new fields on TableContext, FilterFieldContext, InputFieldContext), new config field in `config/` package

---

### 6.4 Templates

Build and test templates in this order. Each should compile and pass golden-file tests before moving to the next.

#### 6.4a `enum.tmpl` + `types.tmpl`

Simplest templates — no client logic. All custom types (except domain aliases) implement `sql.Scanner` and `driver.Valuer` so they work as Shape 1 (Direct) scan targets on both pgx and stdlib.

- **Enums:** Go string type + constants + `String()` method + validation + string-based `Value()`/`Scan()`. Enables `enum[]` column support on both drivers.
- **Composite types:** Go struct with typed fields + JSON-based `Value()`/`Scan()`. Stored as JSONB columns.
- **Domain types:** Go type alias — inherits base type driver behavior, no `Value()`/`Scan()`.
- **Extra types:** Go struct from config-defined fields with tags + JSON-based `Value()`/`Scan()`. Stored as JSONB columns.

---

#### 6.4b `error.tmpl` + `tablename.tmpl`

- `error.tmpl`: Sentinel error variable re-exports, `ConstraintError`/`ConstraintType` type aliases
- `tablename.tmpl`: `TableName` string type + one constant per table/view

---

#### 6.4c Shared Fragments

- `shared/_field.tmpl` — renders a struct field with `db`/`json` tags, doc comment, correct Go type
- `shared/_imports.tmpl` — import block with deduplication
- `shared/_doc.tmpl` — doc comment helper

---

#### 6.4d `table/model.tmpl`

- Struct definition with all columns as fields
- Relationship fields (`*Related` for O2O, `[]*Related` for O2M/M2M)
- Composite PK struct (if applicable)
- `db` and `json` struct tags

---

#### 6.4e `shared/_filter.tmpl` + `shared/_field_options.tmpl`

- `{Table}Filter` struct — one comparator field per column (including soft delete column as regular comparator) + `And`/`Or`. Composite PK tables include `PKs` field for batch lookups.
- `ToConditions(dialect)` method — explicit per-field nil-checks, no reflection. No implicit soft delete handling — scoping is done by generated methods.
- `{Table}FieldOptions` struct — one bool per column + `*{Related}FieldOptions` for O2O + `*{Related}RelationshipOptions` for O2M/M2M
- `ColumnMap()` method

**PRD Reference:** Sections 11, 12

---

#### 6.4f `shared/_input.tmpl`

- `Create{Table}Input` — field classification: required (bare type), omittable (`omittable.Value[T]`), excluded (PK auto-gen, computed, update columns)
- `Update{Table}Input` — all mutable fields wrapped in `omittable.Value[T]`, excluded (PK, computed, update columns)
- `Get{Table}sInput` — Filter, FieldOptions, Limit, Offset, Sorts

---

#### 6.4g `table/client.tmpl`

- `{Table}Client` exported interface — methods based on enabled operations
- `{table}Client` unexported struct — implements interface, holds `database.Querier`
- `CallOptions` struct
- Constructor (internal — called by unified client `New`)

---

#### 6.4h `table/get.tmpl`

**This is the most complex template — contains scan function generation.**

- `Get(ctx, pk, ...CallOptions) → (*Entity, error)` — single row by PK
- `GetMany(ctx, *GetInput, ...CallOptions) → ([]*Entity, error)` — filter + field selection + limit/offset/sorts
- `scan{Table}s(rows, columns) → ([]*Entity, error)` — generated scan function:
  - `switch` on column name → scan target (Shape 1: direct, Shape 2: wrapped, Shape 3: intermediate)
  - Post-scan conversions for Shape 3 columns
  - No reflection
- Relationship loading in `GetMany`:
  - O2O: handled by JOIN scan function (see 6.4o)
  - O2M/M2M: collect FK values → batched `GetMany` on related table → map back to parents
  - Parallel loading via `errgroup`, inside a transaction and out — the `Tx`'s connection reservation serializes it inside one (PRD §13.2 step 4, §18.5)

---

#### 6.4i `table/create.tmpl`

- `Create(ctx, *CreateInput, ...CallOptions) → (*Entity, error)`
  - PK strategy: `db` → omit PK from INSERT, `app` → generate UUID + include, `caller` → include from input
  - RETURNING clause (PostgreSQL/SQLite) or `LastInsertId()` (MySQL)
  - MySQL workaround: `Exec()` → `Get()` by known PK
- `CreateMany(ctx, []*CreateInput, ...CallOptions) → ([]*Entity, error)`
  - Split into batches of `batch_size`
  - Multi-row INSERT per batch

---

#### 6.4j `table/update.tmpl`

- `Update(ctx, pk, *UpdateInput, ...CallOptions) → (*Entity, error)`
  - Per-field `IsSet()` checks → SET clause
  - Empty update → return current entity unchanged
  - `strict_updates: true` → `ErrNotFound` if PK missing
  - Auto-set `update_columns` to `NOW()`
- `UpdateMany(ctx, []UpdateItem, ...CallOptions) → error`
  - pgx: `SendBatch` pipelining
  - stdlib: sequential in implicit transaction
- `UpdateWhere(ctx, *Filter, *UpdateInput, ...CallOptions) → error`
  - Idempotent (nil when no rows match)
  - `ErrEmptyFilter` when filter is nil/empty

---

#### 6.4k `table/delete.tmpl`

- `SoftDelete(ctx, pk, ...CallOptions) → error` — SET soft delete column
- `SoftDeleteMany(ctx, []pk, ...CallOptions) → error`
- `SoftDeleteWhere(ctx, *Filter, ...CallOptions) → error`
- `Restore(ctx, pk, ...CallOptions) → error` — CLEAR soft delete column
- `RestoreMany(ctx, []pk, ...CallOptions) → error`
- `RestoreWhere(ctx, *Filter, ...CallOptions) → error`
- `HardDelete(ctx, pk, ...CallOptions) → error` — physical DELETE
- `HardDeleteMany(ctx, []pk, ...CallOptions) → error`
- `HardDeleteWhere(ctx, *Filter, ...CallOptions) → error`
- All idempotent (nil when PK doesn't exist or no rows match filter)

---

#### 6.4l `table/upsert.tmpl`

- `Upsert(ctx, *CreateInput, conflictKeys, ...CallOptions) → (*Entity, error)`
- `{Table}ConflictTarget` enum type — one constant per PK + UNIQUE constraint
- `{table}ConflictColumns` map — constant → column set
- Dialect-aware SQL: `ON CONFLICT ... DO UPDATE SET` (PG/SQLite) vs `ON DUPLICATE KEY UPDATE` (MySQL)

---

#### 6.4m `table/exists.tmpl` + `table/count.tmpl`

- `Exists(ctx, pk, ...CallOptions) → (bool, error)`
- `ExistsWhere(ctx, *Filter, ...CallOptions) → (bool, error)`
- `Count(ctx, *Filter, ...CallOptions) → (int64, error)`

---

#### 6.4n `table/increment.tmpl`

- `{Table}IncrementColumn` string enum — one constant per numeric non-PK column
- `Increment(ctx, pk, column, amount, ...CallOptions) → error`
  - Atomic `SET col = col + amount`
  - Negative amount = decrement
  - `strict_updates` behavior for missing PK
- If table has no incrementable columns, skip this template entirely

---

#### 6.4o `table/relationships.tmpl`

- O2O JOIN loading:
  - `scan{Table}sWithOneToOneJoins(rows, columns)` — prefix-based JOIN scan function (one per table, covers all O2O aliases)
  - NULL detection for LEFT JOIN misses
  - Chained O2O flattened into multi-JOIN query
  - Soft delete on related table added to JOIN condition
  - Polymorphic filter in JOIN condition
- O2M batched loading:
  - Collect FK values from parents → `IN` query on related table
  - Consumer filter merged with FK filter (AND)
  - Consumer sorts or static relationship sort applied
  - Map results back to parent entities by FK
- M2M batched loading:
  - Query junction table joined with related table
  - Map results back through junction
- All collection loads run in parallel via `errgroup`, inside a transaction and out — the `Tx`'s connection reservation serializes it inside one (PRD §13.2 step 4, §18.5)

**PRD Reference:** Section 13

---

#### 6.4p `sorter.tmpl` + `pagination.tmpl` + `connection.tmpl`

- Per-table sorter structs with typed methods returning `sql.Sort` (accepts `sql.SortDirection`)
- Shared pagination types:
  - `PaginateResult[T]` — `Items []*T`, `Total int64`
- Shared connection types (Relay spec):
  - `Connection[T]` — `Edges []Edge[T]`, `PageInfo`
  - `Edge[T]` — `Node *T`, `Cursor string`
  - `PageInfo` — `HasNextPage`, `HasPreviousPage`, `StartCursor`, `EndCursor`, `TotalCount`
  - `ConnectionInput` — `First`, `Last`, `After`, `Before`
- Cursor encoding/decoding (Base64 JSON)

**PRD Reference:** Sections 14, 15

---

#### 6.4q `table/pagination.tmpl`

- `Paginate(ctx, *Filter, limit, offset, ...CallOptions) → (*PaginateResult, error)`
  - Uses `BuildSelect` with LIMIT + OFFSET
  - Separate `BuildCount` for total
- `Connection(ctx, *Filter, *ConnectionInput, ...CallOptions) → (*Connection, error)`
  - Forward pagination: `first` + `after` → keyset WHERE + LIMIT first+1
  - Backward pagination: `last` + `before` → keyset WHERE DESC + LIMIT last+1 → reverse
  - Both `first` and `last` → `ErrInvalidCursor`
  - Cursor decode/encode using `cursor_keys` config

**PRD Reference:** Section 14

---

#### 6.4r View Parsing

**Blocks 6.4s.** Two paths for populating views:
- **Introspection (live DB):** discover views from database catalog with accurate column types; annotations overlay where specified
- **Annotation files (no live DB):** dedicated `.sql` file with bare SELECT + `@pk`, `@type`, `@nullable`, `@import` comments; column type resolution per PRD 16.3 (annotation → schema matching → aggregate inference → `any`)
- `CREATE VIEW` / `DROP VIEW` in DDL files skipped gracefully (no error, warning emitted)
- Aggregate inference: `COUNT` → `int64`, `AVG` → `*float64`, `SUM` → base type, `MIN`/`MAX` → pointer, `STRING_AGG`/`GROUP_CONCAT` → `*string`, `ARRAY_AGG` → `[]any`

**PRD Reference:** Sections 16.1, 16.2, 16.3

---

#### 6.4s View Templates

Read-only subset of table templates (depends on 6.4r):
- `view/model.tmpl` — struct from view columns (type resolution via @type annotation, schema matching, aggregate inference)
- `view/client.tmpl` — view client with read operations only
- `view/get.tmpl` — Get (if `@pk` annotated), GetMany
- `view/count.tmpl` — Count
- `view/pagination.tmpl` — Paginate, Connection

**PRD Reference:** Section 16

---

#### 6.4t `client.tmpl` — Unified Client

**Always last — references all entity clients.**

- `Client` struct — `querier`, one unexported field per entity client
- `New(querier, ...ClientOption) *Client` — constructs all entity clients
- `ClientOption` functional options: `WithMutationHook`, `WithQueryHook`, `WithPanicHandler`, `WithCallbackMode`
- Per-table accessor: `func (c *Client) Products() ProductClient`
- Per-view accessor: `func (c *Client) ProductSummary() ProductSummaryClient`
- `Begin(ctx, name, ...TxOptions) (context.Context, error)` — delegates to `database.NewTransaction`
- `Commit(ctx) error` — delegates to `database.Commit`
- `Rollback(ctx) error` — delegates to `database.Rollback`
- `WithTx(ctx, name, fn, ...TxOptions) error` — delegates to `database.WithTransaction`
- `Querier(ctx) database.Querier` — escape hatch, returns Tx from context if open
- `Raw(ctx, sql, args, fn) error` — global hooks only; takes a scan callback so the result set cannot outlive the call (Phase 28.5, PRD §20.2)
- `RawExec(ctx, sql, args) (Result, error)` — global hooks only
- `Ping(ctx) error`
- `Close() error` — delegates to closeable dependencies

**PRD Reference:** Section 20

---

## Phase 7: Hooks & Middleware

### 7.1 Hook Types

**Module:** Runtime

**What to build:**
- `MutationHook`, `MutationHandler`, `MutationContext`
- `QueryHook`, `QueryHandler`, `QueryContext`
- `MutationOp` enum (all 16 operations)
- `QueryOp` enum (all 6 operations)
- `TableName` type (constants generated per table in Phase 6)

**Depends on:** Runtime module types

**PRD Reference:** Section 21.2

---

### 7.2 Generic Helpers

**What to build:**
- `ForMutation[Input, Result](table, fn) MutationHook` — type-safe table-scoped mutation hook
- `ForQuery[Input, Result](table, fn) QueryHook` — type-safe table-scoped query hook
- `OnMutation(hook, ops...) MutationHook` — operation filter
- `OnQuery(hook, ops...) QueryHook` — operation filter
- `ForTable(hook, tables...) MutationHook` — table filter
- `RejectMutation(ops...) MutationHook` — block operations

**Tests:**
- Generic helpers pass through when table doesn't match
- Operation filters only fire on specified ops
- `RejectMutation` returns error

**Depends on:** Hook types

**PRD Reference:** Section 21.4

---

### 7.3 Hook Chain Execution

**What to build:**
- Chain builder: global hooks → entity-specific hooks → operation
- Execution order: outermost first (first registered = outermost)
- Built-in panic recovery hook (always outermost)
- `WithPanicHandler` for custom recovery
- Entity client constructors receive hook slices from unified client (deferred from 7.1)

**Tests:**
- Execution order matches registration order
- Global hooks run before entity hooks
- Panic in hook → caught, converted to error
- Custom panic handler receives context

**Depends on:** Hook types, generic helpers

**PRD Reference:** Sections 21.3, 21.5, 21.6

---

### 7.4 CallOptions Integration

**What to build:**
- `CallOptions[FO any]` generic struct: `SkipCache`, `SkipEvents`, `SkipHooks`, `FieldOptions *FO`
- `...func(*CallOptions[{Table}FieldOptions])` parameter on all generated methods
- `SkipHooks` implies `SkipCache` and `SkipEvents`
- Relationship propagation: parent `CallOptions` flow to child relationship loads
- Empty `FieldOptions` short-circuit: `&FieldOptions{}` (all false) skips entity fetch on mutations

**Tests:**
- Options correctly propagate through hook chain
- `SkipHooks` bypasses hooks (except panic recovery)
- Relationship loads inherit parent options
- Empty FieldOptions causes mutation methods to return nil (skip fetch)

**Depends on:** Hook chain, generated client methods (6.3b shared generic types)

**PRD Reference:** Sections 9.6, 9.8

---

## Phase 8: CLI

**Module:** CLI

### 8.1 `cmd/sqlgen/` — Entry Point & `generate`

**What to build:**
- CLI framework: `github.com/spf13/cobra`
- `generate` as default command (no subcommand required)
- `--config`, `--verbose`, `--quiet`, `--version` flags
- Config discovery: `--config` → `sqlgen.yml` → `sqlgen.yaml` → error
- Exit codes: 0 (success), 1 (generation error), 2 (config error), 3 (schema error), 4 (connection error)
- Full pipeline: load config → validate phase 1 → parse schema → validate phase 2 → generate → format → write

**Depends on:** All previous phases

**PRD Reference:** Section 23

---

### 8.2 `init` Command

- Generate minimal `sqlgen.yml` with defaults
- Interactive dialect prompt if TTY detected
- Non-interactive: default to `postgres`

**PRD Reference:** Section 23.4

---

### 8.3 `validate` Command

- Load config + parse schema
- Report all validation errors together
- Exit code 2 on failure

**PRD Reference:** Section 23.1

---

### 8.4 `diff` Command

- Generate to temp directory
- Compare against existing generated files
- Report created/modified/deleted
- Exit code 0 (no changes) or 1 (changes detected)

**PRD Reference:** Section 23.6

---

### 8.5 `completion` Command

- Shell completion scripts: bash, zsh, fish, powershell

**PRD Reference:** Section 23.5

---

### 8.6 `lint` Command

- Parse consumer Go files with `go/ast`
- Find `hook.ForMutation[...]` / `hook.ForQuery[...]` calls
- Validate generic type params match table's types
- Validate operations exist for table
- `--paths`, `--fail-on` flags
- Severity levels: error, warning, info

**PRD Reference:** Section 23.7

---

### 8.7 Stale File Cleanup

- In `file_per_table` mode: track generated files, delete orphaned `*_gen.go` files
- Only within the output directory

**PRD Reference:** Section 23.8

---

## Phase 9: CLI Refactor

**Module:** CLI

This phase extracts the monolithic `main.go` and `lint.go` (~1,500 lines in `package main`) into a dedicated `cli/` package with one file per command and shared pipeline helpers. `main.go` becomes a thin entry point. No behavior changes — pure structural refactoring.

### 9.1 `cli/` Package Scaffold & Root Command

**What to build:**
- Create `cmd/sqlgen/cli/` package
- `root.go`: `NewRootCmd() *cobra.Command`, `Execute() int`, `cliFlags` struct, `exitError` type, `exitCodeFromError`, exit code constants, `version` var (for ldflags)
- Reduce `cmd/sqlgen/main.go` to thin entry point: `os.Exit(cli.Execute())`

**Depends on:** Phase 8 (all CLI commands implemented)

---

### 9.2 Shared Pipeline Extraction

**What to build:**
- `pipeline.go`: `loadAndValidate`, `parseSchema`, `newParser`, `viewParserForDialect`, `parseViewFiles`, `toSchemaTables`, `printWarnings`, `printVerbose`, `splitJoinedErrors`
- These are the shared helpers used by generate, validate, diff, and lint commands

**Depends on:** 9.1

---

### 9.3 Command Extraction

**What to build:**
- `generate.go`: `makeGenerateRunE` + stale file cleanup wiring
- `init.go`: init command RunE, `runInit`, `initConfigContent`, `driverForDialect`
- `validate.go`: `makeValidateRunE`
- `diff.go`: `makeDiffRunE`, `diffFiles`, `compareGeneratedFiles`, `findDeletedFiles`, `printDiffSummary`, `fileChange`
- `completion.go`: `newCompletionCmd`
- `lint.go`: `newLintCmd` and all lint types/functions (move from `cmd/sqlgen/lint.go`)
- Delete original `cmd/sqlgen/main.go` command logic and `cmd/sqlgen/lint.go`

**Depends on:** 9.1, 9.2

---

### 9.4 Test Migration

**What to build:**
- Move all CLI tests from `cmd/sqlgen/main_test.go` and `cmd/sqlgen/lint_test.go` to `cmd/sqlgen/cli/`
- Update `executeCommand` helper to use `cli.NewRootCmd()`
- Ensure all existing tests pass unchanged (no behavioral changes)
- Delete empty test files from `cmd/sqlgen/`

**Depends on:** 9.3

---

## Phase 10: End-to-End Tests

**Module:** CLI (testdata lives under the CLI module)

End-to-end integration tests using example projects under `testdata/examples/`. Each example is a self-contained scenario with `sqlgen.yml`, `schema.sql`, committed golden files in `expected/`, and Go tests in `tests/` that exercise generated code against real databases. See PRD Section 8.1 (End-to-End Integration Tests).

Each example test:
1. **Generate** — runs sqlgen against the example's config and schema
2. **Compare** — diffs generated output against golden files in `expected/` (any difference fails)
3. **Compile** — verifies the generated code compiles cleanly
4. **Exercise** — `tests/` imports generated code and runs operations against a real database (testcontainers for PostgreSQL/MySQL, in-memory for SQLite)

### 10.1 E2E Test Harness

**What to build:**
- `testdata/examples/` directory structure
- Shared test harness: helper that discovers example dirs, runs sqlgen generate, diffs against `expected/`, compiles generated code
- `make update-golden` target to regenerate golden files when templates change intentionally
- Skip E2E tests when `testing.Short()` is true

**Depends on:** Phase 9 (CLI refactor — so the CLI is callable programmatically via `cli.NewRootCmd()`)

---

### 10.2 `postgres` Example

**What to build:**
- `testdata/examples/postgres/` — comprehensive PostgreSQL scenario covering types, relationships, enums, composite types, domain types, and multi-schema name disambiguation

**Schema design (single-PK tables with relationships + broad type coverage):**

```sql
-- Enum type
CREATE TYPE user_role AS ENUM ('admin', 'editor', 'viewer');
CREATE TYPE order_status AS ENUM ('pending', 'confirmed', 'shipped', 'delivered', 'cancelled');

-- Composite type
CREATE TYPE address AS (street text, city text, state text, zip text, country text);

-- Domain type
CREATE DOMAIN email AS text CHECK (VALUE ~* '^.+@.+\..+$');
CREATE DOMAIN positive_int AS integer CHECK (VALUE > 0);

-- users: broad scalar type coverage + enum + domain
CREATE TABLE users (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    email_address email NOT NULL UNIQUE,
    role user_role NOT NULL DEFAULT 'viewer',
    age smallint,                          -- nullable int16
    bio varchar(1000),                     -- nullable string
    is_active boolean NOT NULL DEFAULT true,
    balance numeric(12,2) NOT NULL,        -- float64
    login_count integer NOT NULL DEFAULT 0,-- int32
    avatar bytea,                          -- nullable []byte
    metadata jsonb,                        -- nullable map[string]any
    tags text[] NOT NULL DEFAULT '{}',     -- string array
    scores integer[],                      -- nullable int array
    ip_address inet,                       -- nullable net.IP
    home_address address,                  -- composite type
    last_login timestamptz,                -- nullable time.Time
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- profiles: O2O with users (single FK)
CREATE TABLE profiles (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL UNIQUE REFERENCES users(id),
    website text,
    github_handle varchar(100),
    rating real,                           -- float32
    verified boolean NOT NULL DEFAULT false,
    preferences json                       -- json (not jsonb)
);

-- categories: self-contained, used for M2M
CREATE TABLE categories (
    id serial PRIMARY KEY,
    name text NOT NULL UNIQUE,
    description text,
    sort_order positive_int                -- domain type
);

-- products: O2M from categories
CREATE TABLE products (
    id bigserial PRIMARY KEY,
    category_id integer NOT NULL REFERENCES categories(id),
    title text NOT NULL,
    description text,
    price numeric(10,2) NOT NULL,
    weight_kg double precision,            -- float64
    in_stock boolean NOT NULL DEFAULT true,
    sku varchar(50) NOT NULL UNIQUE,
    attributes jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- orders: FK to users, tests O2M
CREATE TABLE orders (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id),
    status order_status NOT NULL DEFAULT 'pending',
    total numeric(12,2) NOT NULL,
    notes text,
    ordered_at timestamptz NOT NULL DEFAULT now()
);

-- order_items: junction-like (order + product)
CREATE TABLE order_items (
    id bigserial PRIMARY KEY,
    order_id bigint NOT NULL REFERENCES orders(id),
    product_id bigint NOT NULL REFERENCES products(id),
    quantity positive_int NOT NULL,
    unit_price numeric(10,2) NOT NULL
);

-- user_categories: M2M junction table (users ↔ categories)
CREATE TABLE user_categories (
    user_id bigint NOT NULL REFERENCES users(id),
    category_id integer NOT NULL REFERENCES categories(id),
    PRIMARY KEY (user_id, category_id)
);
```

**Relationships exercised:**
- O2O: `profiles.user_id` → `users.id` (unique FK)
- O2M: `orders.user_id` → `users.id`, `products.category_id` → `categories.id`, `order_items.order_id` → `orders.id`
- M2M: `users` ↔ `categories` via `user_categories` junction table

**Types exercised:**
- Scalars: `text`, `varchar`, `smallint`, `integer`, `bigint`, `boolean`, `numeric`, `real`, `double precision`, `bytea`, `timestamptz`
- Special: `jsonb`, `json`, `inet`, `text[]`, `integer[]`
- Custom: `user_role` enum, `order_status` enum, `address` composite, `email` domain, `positive_int` domain

**Comments:** Schema uses `COMMENT ON` for all tables, columns, enums, composite types, and domains. Verifies that doc comments propagate to generated struct and field comments.

**Runtime tests (`tests/`):** testcontainers-go PostgreSQL
- CRUD on each table (Create, Get, Update, Delete)
- Enum field read/write round-trip
- Relationship loading (O2O profile from user, O2M orders from user, products from category)
- Filtering with comparators on various types
- Array column operations
- JSONB column operations
- Domain type columns behave as their base type
- Composite type columns
- Offset pagination (limit + offset)
- Cursor pagination (forward/backward, page info, connection types)

**Depends on:** 10.1

**PRD Reference:** Sections 7.2, 8.1, 13

---

### 10.3 `mysql` Example

**What to build:**
- `testdata/examples/mysql/` — MySQL equivalent with broad type coverage and relationships

**Schema design (MySQL-idiomatic types, same logical structure as postgres):**

```sql
-- users: broad MySQL type coverage
CREATE TABLE users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    role ENUM('admin', 'editor', 'viewer') NOT NULL DEFAULT 'viewer', -- inline enum
    age SMALLINT,                           -- nullable int16
    bio TEXT,                               -- nullable string
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    balance DECIMAL(12,2) NOT NULL,         -- float64
    login_count INT NOT NULL DEFAULT 0,     -- int32
    avatar BLOB,                            -- nullable []byte
    metadata JSON,                          -- nullable map[string]any
    rating FLOAT,                           -- nullable float32
    score DOUBLE,                           -- nullable float64
    tiny_flag TINYINT,                      -- nullable int8
    medium_val MEDIUMINT,                   -- nullable int32
    big_unsigned BIGINT UNSIGNED,           -- nullable uint64
    birth_date DATE,                        -- nullable time.Time
    last_login DATETIME,                    -- nullable time.Time
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- profiles: O2O with users
CREATE TABLE profiles (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE,
    website VARCHAR(500),
    github_handle VARCHAR(100),
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    preferences JSON,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- categories
CREATE TABLE categories (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT
);

-- products: O2M from categories
CREATE TABLE products (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    category_id INT NOT NULL,
    title VARCHAR(255) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    weight_kg DOUBLE,
    in_stock BOOLEAN NOT NULL DEFAULT TRUE,
    sku VARCHAR(50) NOT NULL UNIQUE,
    attributes JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

-- orders: O2M from users
CREATE TABLE orders (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    status ENUM('pending', 'confirmed', 'shipped', 'delivered', 'cancelled') NOT NULL DEFAULT 'pending',
    total DECIMAL(12,2) NOT NULL,
    notes TEXT,
    ordered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- order_items: junction-like
CREATE TABLE order_items (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    quantity INT NOT NULL,
    unit_price DECIMAL(10,2) NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- user_categories: M2M junction
CREATE TABLE user_categories (
    user_id BIGINT NOT NULL,
    category_id INT NOT NULL,
    PRIMARY KEY (user_id, category_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
```

**Types exercised:**
- Scalars: `VARCHAR`, `TEXT`, `TINYINT`, `SMALLINT`, `MEDIUMINT`, `INT`, `BIGINT`, `BIGINT UNSIGNED`, `BOOLEAN`, `DECIMAL`, `FLOAT`, `DOUBLE`, `DATE`, `DATETIME`, `TIMESTAMP`, `BLOB`
- Special: `JSON`, inline `ENUM`
- Dialect differences: backtick quoting, `?` placeholders, `ON DUPLICATE KEY`, no `RETURNING`

**Comments:** Schema uses MySQL inline `COMMENT` on tables and columns. Verifies that doc comments propagate to generated struct and field comments.

**Runtime tests (`tests/`):** testcontainers-go MySQL
- CRUD on each table
- Inline enum read/write round-trip
- Relationship loading (O2O, O2M, M2M)
- Unsigned integer types
- JSON column operations
- `ON DUPLICATE KEY UPDATE` upsert behavior
- Offset and cursor pagination

**Depends on:** 10.1

**PRD Reference:** Sections 7.2, 8.1, 13

---

### 10.4 `sqlite` Example

**What to build:**
- `testdata/examples/sqlite/` — SQLite equivalent with type affinity coverage

**Schema design (SQLite type affinities, same logical structure):**

```sql
-- users: SQLite type affinity coverage
CREATE TABLE users (
    id INTEGER PRIMARY KEY,                 -- autoincrement via rowid
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL DEFAULT 'viewer',     -- no native enum, use TEXT
    age INTEGER,                            -- nullable int64 (SQLite has no smallint)
    bio TEXT,                               -- nullable string
    is_active BOOLEAN NOT NULL DEFAULT 1,
    balance REAL NOT NULL,                  -- float64
    login_count INTEGER NOT NULL DEFAULT 0,
    avatar BLOB,                            -- nullable []byte
    metadata TEXT,                          -- JSON stored as TEXT
    last_login TEXT,                        -- datetime stored as TEXT
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- profiles: O2O
CREATE TABLE profiles (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id),
    website TEXT,
    github_handle TEXT,
    verified BOOLEAN NOT NULL DEFAULT 0,
    preferences TEXT
);

-- categories
CREATE TABLE categories (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT
);

-- products: O2M
CREATE TABLE products (
    id INTEGER PRIMARY KEY,
    category_id INTEGER NOT NULL REFERENCES categories(id),
    title TEXT NOT NULL,
    price REAL NOT NULL,
    weight_kg REAL,
    in_stock BOOLEAN NOT NULL DEFAULT 1,
    sku TEXT NOT NULL UNIQUE,
    attributes TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- orders: O2M
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'pending',
    total REAL NOT NULL,
    notes TEXT,
    ordered_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- order_items
CREATE TABLE order_items (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    product_id INTEGER NOT NULL REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL
);

-- user_categories: M2M junction
CREATE TABLE user_categories (
    user_id INTEGER NOT NULL REFERENCES users(id),
    category_id INTEGER NOT NULL REFERENCES categories(id),
    PRIMARY KEY (user_id, category_id)
);
```

**Types exercised:**
- All 5 SQLite type affinities: `INTEGER`, `TEXT`, `REAL`, `BLOB`, `BOOLEAN` (mapped to int64, string, float64, []byte, bool)
- Dialect differences: `?` placeholders, `"double"` quoting, `RETURNING` (3.35+), no native enum/composite/domain types

**Runtime tests (`tests/`):** SQLite in-memory (no testcontainers)
- CRUD on each table
- Relationship loading (O2O, O2M, M2M)
- BOOLEAN stored as INTEGER round-trip
- RETURNING clause behavior
- `ON CONFLICT` upsert behavior
- Offset and cursor pagination

**Depends on:** 10.1

**PRD Reference:** Sections 7.2, 8.1, 13

---

### 10.5 Soft Delete — cross-dialect

**What to build:**
- Extend all three dialect examples (postgres, mysql, sqlite) with soft delete tables
- Add `articles` table with `deleted_at` (timestamp) and `tags` table with `is_deleted` (boolean) to each dialect
- Add `generation.soft_delete_columns` config to each `sqlgen.yml`
- Tests in `tests/soft_delete_test.go` per dialect: SoftDelete, GetMany exclusion, Restore, filter override, SoftDeleteWhere, RestoreWhere

**Depends on:** 10.1

**PRD Reference:** Sections 8.1, 17

---

### 10.5a Test File Refactor

**What to build:**
- Split each dialect's monolithic `crud_test.go` into focused files by feature area
- `main_test.go` (TestMain, helpers), `crud_test.go` (single-entity CRUD), `batch_test.go`, `relationship_test.go`, `pagination_test.go`, `soft_delete_test.go`
- Postgres-specific: `filter_test.go`, `types_test.go`, `multi_schema_test.go`
- Pure file reorganization — no test logic changes

**Depends on:** 10.5

---

### 10.6 Composite PK — cross-dialect

**What to build:**
- Extend all three dialect examples with a `product_tag_labels` table (3-column composite PK)
- Existing `user_categories` already provides 2-column PK coverage
- Tests in `tests/composite_pk_test.go` per dialect: Get, Update, Delete, Upsert with full composite key

**Depends on:** 10.5a

**PRD Reference:** Sections 8.1, 9

---

### 10.7 Views — cross-dialect

**What to build:**
- Add view annotation files and `input.views` config to all three dialect examples
- Each dialect gets a `product_summary` view with `@pk`, `@type`, `@nullable` directives
- Tests in `tests/views_test.go` per dialect: Get, GetMany, Count, Paginate, Connection, no mutations

**Depends on:** 10.5a

**PRD Reference:** Sections 8.1, 16

---

### 10.8 Type Overrides — cross-dialect

**What to build:**
- Add `tables.<table>.overrides.types` to a specific table in each dialect
- Postgres: override `uuid` → `uuid.UUID` (google/uuid) and `numeric` → `decimal.Decimal` (shopspring/decimal)
- MySQL/SQLite: equivalent table-level overrides where applicable
- Tests in `tests/type_overrides_test.go` per dialect: round-trip with overridden types, verify non-overridden tables unchanged

**Depends on:** 10.5a

**PRD Reference:** Sections 4.7, 4.8, 7.4, 7.5, 8.1


---

## Phase 11: Event System

**Depends on:** Hooks (Phase 7), Transactions/OnCommit (Phase 2.1), E2E Tests (Phase 10)

**PRD Reference:** Section 28

### 11.1 `event/` Package — Types and Interfaces

**Module:** Runtime (`github.com/teandresmith/sqlgen`)

**What to build:**
- `Event` struct (`ID`, `Table`, `Schema`, `Action`, `PK`, `Input`, `Timestamp`, `Metadata`)
- `Action` type with constants: `Create`, `Update`, `Delete`, `Upsert`
- `Config` struct (`OnError` callback)
- `Publisher` interface (`Publish`, `PublishBatch`, `Close`)
- `Subscriber` interface (`Subscribe`, `Close`)
- `SubscribeOptions` struct (`Tables`, `Actions`, `Group`)
- `Handler` function type (`func(ctx context.Context, event Event) error`)
- `Subscription` interface (`Unsubscribe`)

**Constraints:**
- stdlib-only — no external dependencies
- No imports from parser, CLI, or other runtime packages
- Follows the same pattern as `database/` package

**Tests:**
- Unit tests for Action constants, Event construction
- Verify zero-value semantics

**Depends on:** Nothing (stdlib only)

---

### 11.2 EventConfig — Configuration

**Module:** CLI (`cmd/sqlgen/config/`)

**What to build:**
- `EventConfig` struct in config (`enabled`)
- `TableEventConfig` for per-table overrides
- Add `events` field to `RootConfig`
- Add `events` field to `TableConfig`
- YAML parsing and validation
- Validation: `events.enabled` requires no additional fields

**Depends on:** 11.1

---

### 11.3 Event Hook — Generated Code

**Module:** CLI (`cmd/sqlgen/gen/templates/`)

**What to build:**
- Generated `event_hooks_gen.go` with per-entity event hooks
- `buildEventHooks(publisher, config)` factory scoped via `hook.ForTable`
- `WithEventPublisher` client option — prepends event hooks as outermost
- Mutation hook implementation: translate `hook.MutationContext` → `event.Event`, publish
- `AffectedPKs []any` on `MutationContext` — terminal always populates
- Transaction-safe: use `Tx.OnCommit` to defer events within transactions, discard on rollback
- `SkipEvents` CallOption suppresses publishing
- Per-table event config: respect `enabled` overrides
- Batch operations: iterate `AffectedPKs`, publish via `PublishBatch`
- Event carries `mc.Input` directly (mutation input, zero cost)

**Depends on:** 11.1, 11.2

---

### 11.4 `event/memorybus/` — In-Memory Transport

**Module:** Runtime (`github.com/teandresmith/sqlgen`)

**What to build:**
- `memorybus.New()` constructor returning a type that implements both `event.Publisher` and `event.Subscriber`
- Synchronous in-process dispatch via goroutine-safe subscription registry
- `Subscribe` with table/action filtering per `SubscribeOptions`
- Consumer group support: when `Group` is set, round-robin dispatch to one subscriber in the group; when empty, broadcast to all
- `Subscription.Unsubscribe()` to remove a handler
- `PublishBatch` iterates and calls `Publish` per event
- `Close()` clears all subscriptions
- Thread-safe via `sync.RWMutex`

**Constraints:**
- stdlib-only — no external dependencies
- Part of the runtime module (no separate `go.mod`)

**Tests:**
- Publish/Subscribe round-trip
- Table and action filtering
- Consumer group round-robin vs broadcast
- Unsubscribe stops delivery
- Thread-safety under concurrent publish/subscribe
- Close clears subscriptions

**Depends on:** 11.1

---

### 11.5 `event/natsbus/` — NATS Transport

**Module:** Separate Go module (`github.com/teandresmith/sqlgen/event/natsbus`)

**What to build:**
- `natsbus.New(conn *nats.Conn, ...Option)` constructor implementing both `event.Publisher` and `event.Subscriber`
- Subject pattern: `{prefix}.{schema}.{table}` (configurable prefix, default `sqlgen.events`)
- JSON serialization of `event.Event` for NATS messages
- `Subscribe` maps to NATS subscriptions; `Group` maps to NATS queue groups
- Table/action filtering: subscribe to specific subjects or wildcard + filter in handler
- `Close()` drains connection and unsubscribes all
- Own `go.mod` with dependency on `github.com/nats-io/nats.go`

**Constraints:**
- Separate Go module — must not be imported by the runtime module
- Depends on `github.com/nats-io/nats.go`

**Tests:**
- Integration tests using embedded NATS server (`github.com/nats-io/nats-server/v2/server`)
- Publish/Subscribe round-trip
- Subject pattern generation
- Consumer group (queue group) distribution
- Table/action filtering
- JSON serialization round-trip of Event
- Close drains cleanly

**Depends on:** 11.1

---

### 11.6 Event System E2E Tests

**What to build:**
- Integration tests using `memorybus` publisher to verify events are published correctly
- Test all mutation types produce correct events (Create, Update, Delete, Upsert)
- Test transaction-safe behavior: events deferred until commit, discarded on rollback
- Test `SkipEvents` suppresses publishing
- Test per-table event config overrides
- Test batch operations produce multiple events
- Test event carries correct `Input` from mutation context

**Depends on:** 11.1, 11.2, 11.3, 11.4

---

## Phase 12: Caching

**Depends on:** Event System (Phase 11)

**PRD Reference:** Section 27

**Design Reference:** `docs/design/CACHE.md` — single source of truth for Phase 12. All sub-items below derive acceptance criteria from that document.

### 12.1 `cache/` Runtime Core

**Module:** Runtime (`github.com/teandresmith/sqlgen`)

**What to build:**
- `Backend` interface: `Get`, `Set`, `Invalidate`, `InvalidateMany`, `InvalidatePattern` (all mandatory). Miss semantics: `Get` returns `(nil, nil)`.
- Optional `StatsReporter` interface with `Stats` struct (`Hits`, `Misses`, `Sets`, `Invalidations`, `Evictions`, `Entries`).
- Graceful shutdown via stdlib `io.Closer`.
- `Serializer` interface + `JSONSerializer` default.
- Key helpers: `BuildKey(prefix, schema, table, fingerprint, pk)`, `BuildCompositeKey(prefix, schema, table, fingerprint, pks)`, `BuildTablePattern(prefix, schema, table)` (fingerprint-agnostic). Labeled-segment grammar: `{prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}` — `pk:` is terminal, composite components colon-separated under that label (PRD §27.5).
- `Breaker` + `BreakerConfig` per CACHE.md §5.7 transition table.
- `MetricsRecorder` interface with schema-leading parameter on every per-table method; `CircuitState` enum.
- `OnErrorFunc` + `DefaultOnCacheError` — cache error policy (CACHE.md §5.8).
- Typed helpers: `GetAs[T]`, `SetAs[T]`, `GetOrSet[T]`, `KeysFromAny[T]`.
- `NoopBackend` (implements `Backend` + `StatsReporter` as no-ops).

**Constraints:**
- stdlib + `golang.org/x/sync` (for singleflight in 12.8) only.
- No imports from parser, CLI, or other runtime packages (cache may import `hook` and `event`).

**Tests:**
- `backend_test.go`: interface shape tests (compile-time assertions).
- `serializer_test.go`: JSON round-trip.
- `key_test.go`: simple/composite/empty-schema key grammar, fingerprint-presence, composite-fingerprint cases.
- `breaker_test.go`: cases (a)–(j) from CACHE.md §20.1 (full transition table coverage; run with `-race`).
- `error_test.go`: all-three-channels fire in order (metrics → OnErrorFunc → breaker); panicking `OnErrorFunc` does not skip breaker accounting; nil `MetricsRecorder` is zero-cost.
- `typed_test.go`: `GetAs` / `SetAs` / `GetOrSet` round-trips, miss semantics, `KeysFromAny` happy path + type-mismatch error format + empty-slice case.
- `noop_test.go`: `NoopBackend` satisfies `Backend` + `StatsReporter`; `Get` always misses; `Invalidate*` succeed as no-ops.

**Depends on:** Nothing (stdlib + `golang.org/x/sync`).

---

### 12.2 `cache/` Invalidation — `InvalidationSource` + `FromEventSubscriber`

**Module:** Runtime (`github.com/teandresmith/sqlgen`)

**What to build:**
- `InvalidationSource` interface (`Subscribe(handler) (Subscription, error)`, `Close`).
- `InvalidationHandler func(ctx, table hook.TableName, pks []any) error`.
- `InvalidationSubscription` with `Unsubscribe`.
- `FromEventSubscriber(event.Subscriber) InvalidationSource` — synchronous per-event adapter. Registers with empty `SubscribeOptions{}`; each event produces one handler call with `pks = []any{ev.PK}`. Handler errors are returned from the inner `event.Handler` (for ACK-capable transports) and recorded via `MetricsRecorder.Error`. No event-ID dedup — backend invalidation is idempotent.

**Tests:**
- `invalidation_test.go`: 1 event → 1 `InvalidationHandler` call; handler error is both recorded on `MetricsRecorder` and returned from event `Handler`; duplicate `event.ID` redelivery produces a second no-op invalidation without error.

**Depends on:** 12.1, 11.1

---

### 12.3 Config — CacheConfig, resolvers, validation

**Module:** CLI (`cmd/sqlgen/config/`)

**What to build:**
- `CacheConfig` (Enabled, Version, TTL, Serializer, KeyPrefix, Hydration, CircuitBreaker).
- `HydrationConfig`, `CircuitBreakerConfig` (FailureThreshold, ProbeInterval, HalfOpenMaxProbes).
- `Serializer` enum (`json` | `msgpack` | `custom`).
- `TableCacheConfig` (tri-state pointers: Enabled, TTL, Serializer).
- `ViewCacheConfig` (tri-state pointers: Enabled, TTL, Serializer); `ViewConfig.InvalidateOn []string`.
- Resolvers: `ResolveTableCacheEnabled/TTL/Serializer`, `ResolveViewCacheEnabled/TTL/Serializer` (view caching opt-in — does NOT inherit from global `cache.enabled`).
- YAML-parse validation: ttl parses as duration (default 1h); serializer ∈ {json, msgpack, custom, ``}; `key_prefix` empty/unset → default `"sqlgen"` with stderr warning; hydration.timeout parses (default 30s); circuit_breaker thresholds ≥ 1 / > 0; views.*.cache.enabled:true requires non-empty invalidate_on (hard error).
- Post-parse validation: `views.*.invalidate_on` names resolve to tables in the parsed schema AND are included in generation (hard errors otherwise).
- Construction-time validation: `serializer: custom` requires `WithSerializer(...)`.

**Tests:**
- Config tests: unset key_prefix → "sqlgen" + stderr warning emitted; explicit "" → same; explicit "custom" → verbatim, no warning. Capture stderr in test to assert warning text.
- Per-table override precedence (table opt-in under global opt-out).
- View opt-in rule (global enabled=true, view.cache.enabled unset → view not cached).
- `invalidate_on` unknown-table + excluded-table hard errors.

**Depends on:** Nothing (config package changes only).

---

### 12.4 `cache/memory/` Backend

**Module:** Separate Go module (`github.com/teandresmith/sqlgen/cache/memory`)

**What to build:**
- `memory.New(Options)` returning concrete `*Backend` satisfying `cache.Backend` + `cache.StatsReporter` + `io.Closer`.
- Backed by `github.com/maypok86/otter/v2` (implementation detail — not in public surface).
- Options: `MaxSize` (required — errors on `≤ 0`), `DefaultTTL`, `StatsEnabled`.
- `InvalidatePattern` via entry iteration + prefix match. Pattern contract: prefix + trailing `*`.
- Per-entry TTL honored (otter native support).
- Thread-safe via otter's internal lock sharding.

**Tests:**
- `memory_test.go`: Get/Set/Invalidate/InvalidateMany/InvalidatePattern/Stats/Close; TTL expiry; iteration-based pattern invalidation; `New(MaxSize: 0)` errors.

**Depends on:** 12.1

---

### 12.5 `cache/redis/` Backend

**Module:** Separate Go module (`github.com/teandresmith/sqlgen/cache/redis`)

**What to build:**
- `redis.New(client redis.UniversalClient, ...Option)` returning concrete `*Backend` satisfying `cache.Backend` + `cache.StatsReporter` + `io.Closer`.
- Options: `WithOwnedClient` (Close closes client), `WithScanCount` (default 500), `WithLocalStats` (default true).
- `InvalidatePattern` via `SCAN MATCH pattern COUNT n` + `DEL` in batches. Never `KEYS`.
- `redis.Nil` → `(nil, nil)` miss.
- Local atomic counters for stats; `Entries` from `INFO` when available.

**Tests:**
- `redis_test.go`: testcontainers-backed (`testcontainers-go/modules/redis`) — real SCAN pagination, DEL batching, `redis.Nil` miss, `WithScanCount` arg flow, stats counters, owned-client Close. `-short` skips the suite.

**Depends on:** 12.1

---

### 12.6 `cache/msgpack/` Serializer

**Module:** Separate Go module (`github.com/teandresmith/sqlgen/cache/msgpack`)

**What to build:**
- `msgpack.New()` returning `cache.Serializer` backed by `github.com/vmihailenco/msgpack/v5`.
- `msgpack.NewWith(Options)` for preconfigured encoder/decoder pools.
- No construction-time validation wiring — generator auto-injects when `cache.serializer: msgpack`.

**Tests:**
- `msgpack_test.go`: round-trip, struct-tag compatibility with JSON-tagged generated structs.

**Depends on:** 12.1

---

### 12.7 `metrics/otel/` `MetricsRecorder`

**Module:** Separate Go module (`github.com/teandresmith/sqlgen/metrics/otel`)

**What to build:**
- `otel.New(provider metric.MeterProvider) cache.MetricsRecorder`. Nil provider → `otel.GetMeterProvider()`.
- Counter instruments: `sqlgen.cache.hits|misses|sets|invalidations|errors|hydrations` — each labeled with `schema`, `table` (+ `op` / `status` where relevant).
- Histogram instruments: `sqlgen.cache.get.duration|set.duration|invalidate.duration` — seconds — labeled with `schema`, `table`.
- Gauge: `sqlgen.cache.circuit_breaker` — `state` label — NOT labeled with schema/table (breaker is `*Cache`-scoped).
- Empty `schema` ("") for MySQL/SQLite is a valid label value.

**Tests:**
- `otel_test.go`: `Hit(schema="public", table="products")` and `Hit(schema="archive", table="products")` produce two distinct metric series (capture OTel collector output).

**Depends on:** 12.1

---

### 12.8 Generator — `BuildCacheContext`, `cache.go.tmpl`, client wiring

**Module:** CLI (`cmd/sqlgen/gen/`)

**What to build:**
- `BuildCacheContext(root config.RootConfig)` in `cmd/sqlgen/gen/context_cache.go` (mirrors `context_event.go`).
- `CachedTable` / `CachedView` with `Fingerprint` fields (computed at codegen per CACHE.md §10.1: sha256-8char over sorted column name/type/tag + PK column order + serializer identity + `cache.version`).
- `ClientContext` additions: `CacheEnabled`, `CacheConfig`, `CachedTables`, `CachedViews`, `ViewInvalidateMap`.
- `cache.go.tmpl` emitting `cache_gen.go` with:
  - `Cache` struct + `NewCache(backend cache.Backend, opts ...CacheOption) (*Cache, error)`.
  - `CacheOption`s: `WithInvalidationSource`, `WithMetricsRecorder`, `WithSerializer`, `WithCircuitBreaker`, `WithOnError`.
  - Public methods: `QueryHook()`, `MutationHook()`, `Invalidate`, `InvalidateMany`, `InvalidateTable`, `Close`.
  - Per-table `fingerprint{Table}` constants, typed `keyFor{Table}(pk T)` helpers, `ttlFor{Table}()`, optional `serializerFor{Table}()`, `fullParentFieldOptions{Table}()`.
  - `InvalidateMany` / `InvalidateTable` dispatch switches (matching CACHE.md §9.3).
  - `patternsForSourceTable(table hook.TableName) []string` for view invalidation.
  - Internal `invalidateKeys` / `invalidatePattern` helpers (metrics + breaker + singleflight integration).
  - `hasAnyRelationship{Table}(fo any) bool` helpers.
  - `hydrate{Table}` helper with `sync.Map`-dedup + `defer Delete` lifecycle.
  - Singleflight-wrapped read-through using `golang.org/x/sync/singleflight`.
- Conditional imports: msgpack import only when `serializer: msgpack`.
- `client.go.tmpl` additions: `WithCache(c *Cache) Option` shorthand; hook chain ordering per CACHE.md §9.5 (cache outermost → events → user → terminal).
- Conditional emission: `cache_gen.go` only when `cache.enabled: true`.

**Tests:**
- Golden file comparison for `cache_gen.go`.
- Regression: existing non-cache examples still regenerate identically.
- Fingerprint determinism test: identical schema → identical fingerprint; adding a column → different fingerprint; bumping `cache.version` → different fingerprint.

**Depends on:** 12.1, 12.2, 12.3

---

### 12.9 E2E Example + Tests (in-memory, SQLite)

**Module:** CLI (`cmd/sqlgen/testdata/examples/cache/`)

**What to build:**
- New example mirroring `events/`: `sqlgen.yml` with `cache.enabled: true`, a per-table override disabling cache, a view with `invalidate_on`.
- E2E test coverage:
  - Read-through: miss → DB → hit on second call.
  - Full-entity invariant: partial fetch with hydration on → cache populated; hydration off → not populated.
  - All mutation ops trigger correct invalidation.
  - Key-based invalidation on `*Where` ops — one `InvalidateMany` over the
    matched PKs, no table pattern wipe (FIX-208).
  - View invalidation on source-table mutation.
  - `SkipCache` bypasses both paths.
  - Per-table `cache.enabled: false` suppresses all cache activity.
  - Direct `Cache.Invalidate*` methods.
  - Composite-PK round-trip: mutation → `tx.OnCommit` → `InvalidateMany(table, AffectedPKs)` → cache miss on next `Get(pk)`.
  - Event-then-cache commit ordering (trips if FIFO `OnCommit` regresses).
  - Fingerprint change: regenerate with `cache.version: 1` → `cache.version: 2` (same schema), assert `Get` misses and repopulates.
  - Fingerprint change: add column to DDL, regenerate, assert cache miss on re-deploy.
  - Circuit breaker: inject a failing backend, assert state transitions + metric emission.
  - Transaction safety: commit triggers invalidation, rollback does not.
- Concurrency: 50 concurrent `Get(pk)` on a cold key → exactly one DB round-trip (singleflight assertion); hydration dedup assertion.

**Depends on:** 12.4, 12.8

---

### 12.10 Redis Integration Test Suite

**Module:** `cache/redis/`

**What to build:**
- testcontainers-based Redis integration tests alongside the unit tests.
- `testing.Short()` skip guard for fast-feedback loops.
- Assertions: same as in-memory suite, plus SCAN-vs-KEYS behavior and DEL batching.
- Lives in `cache/redis/redis_integration_test.go` (no separate job).

**Depends on:** 12.5, 12.8

---

### 12.11 Sync design back into `docs/PRD.md`

**Module:** Documentation

**What to build:**
- PRD.md updates per CACHE.md §25 "Sync-Back Targets" table — all sections in §27, plus §4.8 TableConfig, §4.9 ViewConfig alignment, §9.6 `SkipCache` confirmation.
- Godoc sync-backs: `database/transaction.go:OnCommit` (FIFO + promotion + rollback contracts), `hook/hook.go:MutationContext.AffectedPKs` (element-shape contract + "used by cache invalidation"), `guidelines/ARCHITECTURE.md` (`golang.org/x/sync/singleflight` row).

**Depends on:** all Phase 12 tasks above

---

### 12.12 Stretch — Type-aware `sqlgen lint` cache rules

**Status:** Deferred (2026-04-22) — moved to a post-Phase 13 dev-tooling batch alongside the §29 tenant-aware lint rule (which also depends on this 12.12 infrastructure). Build-time UX upgrade, not a safety requirement; runtime dispatch in 12.8 already surfaces type mismatches with descriptive errors at call time.

**Module:** CLI (`cmd/sqlgen/cli/lint_cache.go`)

**What to build:**
- Extend `tableInfo` with PK metadata (`PKGoType`, `PKImportPath`, `PKComposite`, `PKStructName`, `PKFields`).
- New type-aware pass using `golang.org/x/tools/go/packages` with `NeedTypes | NeedTypesInfo | NeedSyntax | NeedImports`.
- Scanner: `*<facade>.Invalidate` / `.InvalidateMany` / `.InvalidateTable` call sites; resolve static types via `types.Info`.
- `validateCacheCall` diagnostics per CACHE.md §19A table: wrong PK type, composite struct mismatch, `[]any` element mismatch, dynamic-table info-level.
- Wire into existing `sqlgen lint` pipeline (reuses `lintIssue`, `formatLintOutput`, severity levels).
- Adds `golang.org/x/tools/go/packages` to the CLI module only — runtime stays stdlib + `x/sync` only.

**Tests:**
- Golden lint output for a fixture project with each diagnostic kind.

**Depends on:** 12.8 (needs generated `hook.TableName` constants + PK-type metadata).

---

## Phase 13: Tenancy

**Depends on:** Event System (Phase 11), Caching (Phase 12)

**PRD Reference:** Section 29

**Design Reference:** `docs/design/TENANCY.md` — design supplement (open-question record, codegen details). PRD §29 is the normative spec.

### 13.1 Config — TenancyConfig, TableTenancyConfig, resolvers, validation

**Module:** CLI (`cmd/sqlgen/config/`)

**What to build:**
- `TenancyConfig` (`Enabled`, `Column`, `Required`, `Type` — `TypeOverride` shape from section 4.7).
- `TableTenancyConfig` with tri-state pointers (`*bool` Enabled, `*string` Column, `*bool` Required, `*TypeOverride` Type) — `nil` means inherit global.
- Resolvers: `ResolveTableTenancyEnabled/Column/Required/Type` following the `cache`/`events` precedence pattern (per-table → global → detection).
- YAML-parse validation: `enabled: true` requires non-empty `column`; `type` (if set) has non-empty `type` + `import`.
- Post-parse validation: per-table `enabled: true` with no column present in the parsed schema is a hard codegen error naming the table and expected column.

**Tests:**
- Config tests: global-off overridden by per-table-on; per-table-off under global-on; column-override per table; tri-state inheritance (nil → global).
- Validation tests: `enabled: true` without `column` errors; `tables.X.tenancy.enabled: true` without the column in schema errors with table name + expected column.

**Depends on:** Nothing (config package changes only).

---

### 13.2 Runtime — `tenancy/` package, CallOptions.SkipTenancy

**Module:** Runtime (`github.com/teandresmith/sqlgen/tenancy`)

**What to build:**
- New subpackage `tenancy/` (parallels existing `cache/`, `event/`, `hook/` layout).
- `TenantResolver[T comparable] func(ctx context.Context) (T, error)` — generic type alias, `T` constrained to `comparable` (enables `==` mismatch checks; PRD §29.3.1).
- `ErrMissing = errors.New("tenancy: tenant missing from context")`.
- `ErrMismatch = errors.New("tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override")`.
- Template change in `cmd/sqlgen/gen/templates/shared_types.go.tmpl` (or equivalent): add `SkipTenancy bool` to the generated `CallOptions[FO]` struct (conditional — only emitted when `tenancy.enabled: true`).
- Confirm `resolveCallOptions` does NOT fold `SkipHooks → SkipTenancy` (tenancy runs in SQL builders, not as a hook; PRD §29.4.4).

**Constraints:**
- stdlib only (no imports from parser, CLI, sql, hook, event, cache).

**Tests:**
- `tenancy_test.go`: compile-time interface shape tests; `TenantResolver` parametric instantiation for `uuid.UUID`, `int64`, `string`, and a named-wrapper type.
- Generated `CallOptions` golden test: `SkipTenancy` field presence when tenancy enabled; absent otherwise.
- `SkipHooks: true` does not set `SkipTenancy` after `resolveCallOptions`.

**Depends on:** Nothing (runtime subpackage + template change).

---

### 13.3 Schema detection + type resolution + validation

**Module:** CLI (`cmd/sqlgen/gen/` + `cmd/sqlgen/config/`)

**What to build:**
- Detection pass in the schema-resolution stage: for each table, look up `effective_column` (per-table override → global `tenancy.column`); classify as tenanted, shared, or opt-out per PRD §29.2.3.
- `TenancyContext` struct attached to each resolved table: `Tenanted bool`, `Column string`, `Required bool`, `GoType string`, `Import string`.
- Validation pass:
  - Tenant column MUST NOT be nullable (hard error).
  - Resolved Go type MUST be `comparable` (hard error naming table + type).
  - **Uniform tenant type across all tenanted tables in a generate run** — if types diverge, emit hard error naming every offending table and the types involved (PRD §29.2.4).
  - SQL-column-type check against configured Go type (belt-and-suspenders for wrapper-type misconfigurations).
- Hard-error cases include actionable remediation text matching PRD §29.2.4 examples.

**Tests:**
- Detection: schema with mixed tenanted/shared tables resolves correctly; opt-out forces shared even when column exists.
- Nullable-column error: clear message naming table + column.
- Comparable-type error: non-comparable Go type (e.g. a struct with a slice field) errors at validation.
- Mixed-type error: two tenanted tables with divergent Go types both listed in the error message.
- SQL-vs-configured mismatch: `tenancy.type: WorkspaceID` (wrapping `uuid.UUID`) pointed at a `bigint` column errors.

**Depends on:** 13.1, 13.2

---

### 13.4 SQL builder additions

**Module:** Runtime (`sql/`)

**What to build:**
- Confirm existing `sql.Where(col).Eq(v)` + `SelectOptions.Conditions` / `UpdateOptions.Conditions` / `UpdateOptions.SetClauses` surface is sufficient — tenancy adds conditions to existing slices, not new builder APIs (the conditions are simple `col = $N` shapes already expressible).
- Extend `sql.BuildSelectJoin` handling for the o2o case: the tenant filter for a tenanted child table is added to the outer `Conditions`, NOT the JOIN `On` string (placeholder numbering is computed only from outer conditions — sql/builder.go). No API change; document the convention.
- Add godoc to `SelectOptions` / `UpdateOptions` noting tenant-filter conditions are standard `Condition` values composed by the generator before the `BuildXxx` call — the builders remain tenancy-agnostic.

**Tests:**
- `builder_test.go`: Update tests covering UPDATE / SELECT / SELECT-JOIN with an extra tenant condition in the `Conditions` slice produce the expected SQL across all three dialects (pg `$N`, MySQL/SQLite `?`).
- Round-trip: same set of conditions with different tenant values produces same SQL skeleton (placeholder positions identical, args differ).

**Depends on:** 13.3

---

### 13.5 Generator — BuildTenancyContext, template changes, client wiring

**Module:** CLI (`cmd/sqlgen/gen/`)

**What to build:**
- `BuildTenancyContext(root config.RootConfig, schema parser.Schema)` in `cmd/sqlgen/gen/context_tenancy.go` (mirrors `context_event.go` / `context_cache.go`).
- `ClientContext` additions: `TenancyEnabled bool`, `TenancyGoType string`, `TenancyImport string`, `TenancyColumn string`.
- Template changes across `cmd/sqlgen/gen/templates/table/`:
  - `client.go.tmpl`: emit `tenantResolver tenancy.TenantResolver[<T>]` on `clientOptions`; emit `WithTenantResolver(r tenancy.TenantResolver[<T>]) ClientOption` with concrete `<T>`; thread `tenantResolver` into each tenanted entity client constructor.
  - `get.go.tmpl`: resolve tenant at method entry, append `sql.Where("<col>").Eq(resolved)` to `conds` when `!options.SkipTenancy` and the table is tenanted.
  - `create.go.tmpl` / `upsert.go.tmpl`: resolve tenant, run mismatch check (`input.X.Get()` vs resolver), auto-set tenant column into `columns`/`args` when not `SkipTenancy`.
  - `update.go.tmpl`: resolve tenant, mismatch check, append tenant to `Conditions` (NOT to `SetClauses` unless `SkipTenancy: true` and the caller explicitly set the tenant field).
  - `delete.go.tmpl`: append tenant to `Conditions`.
  - All `*Where` variants: append tenant after `filter.ToConditions(c.dialect)`.
  - Composite PK path: if the tenant column is part of the DDL PK, the generated `XXXPK` struct **omits the tenant field from its constructor** and pulls it from the resolver (PRD §29.7).
- Error wrapping follows PRD / CLAUDE.md convention: `"{op} {table-sing-or-plural}: %w"` (e.g. `"get products: resolve tenant: %w"`, `"update product: resolve tenant: %w"`).
- Raw / RawExec: emit `// sqlgen: raw query bypasses tenancy` comment above the method.
- Conditional emission: tenancy blocks only emitted when `tenancy.enabled: true` globally AND the table is detected as tenanted.

**Tests:**
- Golden-file regeneration of `cmd/sqlgen/testdata/examples/postgres/models/models_gen.go` with a tenanted table added to the example schema.
- Regression: non-tenanted projects (every existing example other than the new tenancy one) regenerate identically — zero API-surface change when tenancy is disabled.
- Composite-PK generation: `XXXPK` struct omits the tenant column; constructor signature verified via AST compare.

**Depends on:** 13.1, 13.2, 13.3, 13.4

---

### 13.6 Cache integration — tenant key segment + per-tenant pattern helper

**Module:** Runtime (`cache/`) + CLI (`cmd/sqlgen/gen/`)

**What to build:**
- `cache.BuildTenantTablePattern(prefix, schema, table string, tenant any) string` — returns `{prefix}:{schema}.{table}:tenant:{tenant}:*` (PRD §29.5).
- Tenant-aware overload in the generated per-table key helpers:
  - For tenanted tables, `keyFor{Table}(tenant T, pk PK) string` constructs the key with the `tenant:{tenant}` segment placed **before** `fingerprint:` (PRD §27.5 / §29.5).
  - For non-tenanted tables, the helper keeps the Phase 12 shape.
- Generator (12.8 extension): thread resolved tenant through the cache hook — the mutation/query hook reuses the value already resolved by the entity method instead of re-invoking `TenantResolver`.
- Template change in `cache.go.tmpl` to emit the new per-table helper signature for tenanted tables.
- Fingerprint input: tenancy is folded into the schema fingerprint as a **structural marker** (tenanted vs not) — the tenant *value* is never in the fingerprint, only the structural fact.

**Tests:**
- `key_test.go`: tenant-scoped key grammar — PostgreSQL-with-schema, MySQL/SQLite empty-schema, composite-PK cases, `BuildTenantTablePattern` for tenant-offboarding.
- Fingerprint determinism: toggling a table's tenancy changes its fingerprint; changing the tenant value on a key does NOT change fingerprint.
- E2E: cross-tenant cache isolation — tenant A's `Get(pk)` populates cache; tenant B's `Get(pk)` misses (different key segments) even when PKs collide.
- Per-tenant invalidation: `BuildTenantTablePattern` clears only tenant X's entries; tenant Y's entries survive.

**Depends on:** 13.5, 12.1, 12.8

---

### 13.7 Event integration — Event.Metadata["tenant"]

**Module:** CLI (`cmd/sqlgen/gen/`)

**What to build:**
- Template change in the event hook emission (Phase 11 `event.go.tmpl` or wherever the `Event` is constructed) to inject `Metadata["tenant"] = fmt.Sprintf("%v", resolvedTenant)` when the table is tenanted.
- Tenant value is closed over from the entity method's resolve call — no second `TenantResolver` invocation inside the async `tx.OnCommit` callback (PRD §29.6 + §27.9 async-ctx concern).
- Conditional emission: only when the table is tenanted AND `events.enabled: true`.

**Tests:**
- E2E: mutation on a tenanted table produces an event with `Metadata["tenant"]` set to the resolver value.
- Non-tenanted table: event has no `"tenant"` key in metadata (regression guard — don't leak empty strings).
- Async-ctx safety: assert the tenant value in the event matches the request-ctx tenant at method-entry time, not whatever `context.Background()` would see during the `OnCommit` callback.

**Depends on:** 13.5, 11.3

---

### 13.8 Relationship propagation

**Module:** CLI (`cmd/sqlgen/gen/`)

**What to build:**
- o2o (inline JOIN): when the child table is tenanted, generator appends the child-side tenant condition to the parent's outer WHERE (NOT to the JOIN `On` string — PRD §29.10 + 13.4 convention). When the child is non-tenanted, no child-side filter is added.
- o2m / m2m: `loadRelationships` method in the parent entity client threads parent `options` (specifically `SkipTenancy`) into the child's `GetMany` call via the functional-option callback (PRD §29.10).
- Belt-and-suspenders when both parent and child are tenanted: both sides get `tenant = $N` in the outer WHERE.
- No per-relationship tenancy config in v1 — per-table tenancy flag + `SkipTenancy` at call site covers the space.

**Tests:**
- o2o golden: tenanted parent + tenanted child produces `WHERE p.tenant = $N AND c.tenant = $N`.
- o2o golden: tenanted parent + non-tenanted child produces `WHERE p.tenant = $N` only.
- o2m E2E: parent `GetMany` with relationship load — child's tenant filter is applied via its own `GetMany` auto-filter.
- `SkipTenancy: true` propagates: parent call with `SkipTenancy: true` drops auto-filter on both parent and children.

**Depends on:** 13.5

---

### 13.9 E2E Example + Tests — cross-dialect

**Module:** CLI (`cmd/sqlgen/testdata/examples/tenancy/`)

**What to build:**
- New example `testdata/examples/tenancy/` with:
  - `sqlgen.yml`: `tenancy.enabled: true`, `column: workspace_id`, one per-table opt-out (`audit_logs`), one legacy column override (`legacy_widgets: { tenancy: { column: org_id } }`).
  - Schema with tenanted + shared tables, o2o / o2m / m2m relationship examples, and composite-PK tenant-in-PK case.
  - Cross-dialect coverage: postgres + mysql + sqlite test files.
- E2E test coverage:
  - Basic isolation: tenant A cannot read tenant B's rows via `Get` / `GetMany`.
  - Mutation mismatch: `Create` / `Update` with a caller-set tenant that mismatches the resolver returns `tenancy.ErrMismatch`.
  - Mutation redundant-match: `Create` / `Update` with a caller-set tenant that matches the resolver succeeds; generated SQL byte-identical to the unset case.
  - Missing-tenant fail-closed: `required: true` + no tenant in ctx returns `tenancy.ErrMissing` before the DB round-trip.
  - `SkipTenancy: true`: admin path reads across tenants.
  - Soft-delete + tenancy composition: both filters AND'd.
  - Cache isolation: tenant A and B populate distinct cache entries for the same PK (when PKs collide).
  - Event metadata: mutation events carry `Metadata["tenant"]`.
  - Relationship propagation: o2o/o2m/m2m loads respect tenant scope per PRD §29.10.
  - Transaction: resolved tenant at tx-open propagates; attempting a cross-tenant write inside the tx errors consistently.

**Depends on:** 13.5, 13.6, 13.7, 13.8

---

### 13.10 Sync design back into `docs/PRD.md`

**Module:** Documentation

**What to build:**
- Reconcile any implementation findings from 13.1–13.9 back into PRD §29 and the five cross-reference sites (§4.8 TableConfig, §9.6 CallOptions, §17 soft-delete parallel, §27.5 cache key grammar, §28.3 Event metadata). Initial sync was completed on 2026-04-21 during Phase 13 planning; this sub-item captures post-implementation reconciliation only.
- Godoc sync-backs: `tenancy/tenancy.go` (`TenantResolver[T]`, `ErrMissing`, `ErrMismatch`) verbatim-aligned with PRD §29.3.1 wording.
- TENANCY.md §10 (generated code reference) updated if codegen patterns drifted from the snippets during implementation.

**Depends on:** all Phase 13 tasks above

---

### 13.11 Stretch — Tenancy lint rule

> **Status: Cancelled (2026-09-16).** PRD §29.11 "Constraints" lists a tenancy-specific lint rule as an explicit non-goal ("There is no tenancy-specific lint rule. `ErrMissing` at the first tenanted call is a strictly better signal than speculative static analysis"), and §23.7's `sqlgen lint` rule set carries no tenancy rule. Nothing below is spec-backed any more; it is retained as the rejected design. See `docs/tracker/phase-13.md` §13.11 for the full rationale. 12.12 is unaffected and stays deferred on its own merits.

**Module:** CLI (`cmd/sqlgen/cli/lint_tenancy.go`)

**What to build:**
- Extend `sqlgen lint` (section 19A / Phase 12.12 infrastructure) with a warning-level rule: raw SQL (`Raw`, `RawExec`) on tenanted tables emits a warning — "this query may need an explicit tenant filter."
- Type-aware pass reusing `golang.org/x/tools/go/packages` infrastructure from 12.12.
- Scanner: `*<facade>.Raw` / `.RawExec` call sites where the resolved table type is tenanted.
- No data-flow tracking of ctx values across function boundaries — the rule is purely "is this call site using raw SQL on a tenanted table?" (PRD §29.11 explicitly deferred type-aware ctx tracking).
- Severity: warning only (not error). Users who want to suppress can disable the rule in `sqlgen.yml` lint config.

**Tests:**
- Golden lint output for a fixture project with `Raw` / `RawExec` on both tenanted and non-tenanted tables.
- Suppression via config works.

**Depends on:** 13.5, 12.12

---

## Phase 14: E2E Coverage Expansion

**Depends on:** All prior phases (features under test must be implemented and stable).

**PRD Reference:** N/A — gap-fill driven by the 2026-04-23 audit of `docs/PRD.md` against `cmd/sqlgen/testdata/examples/`.

**Rationale:** Post-Phase-13 audit surfaced features that are implemented but not exercised end-to-end, plus interaction combinations (tenancy × cache × soft-delete, events × tx, etc.) where a regression could land silently. This phase is **additive** — it adds tests against existing behavior, it does not introduce new runtime/codegen features. Each sub-item is a coherent test bundle verifiable on its own.

**Scope rule:** If a sub-item uncovers a bug (not just a missing test), open a `/fix` item rather than expanding scope in-place. Keep this phase test-only.

### 14.1 Core CRUD — `Increment` e2e

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**What to build:**
- New `increment_test.go` per dialect covering: increment on `int32` / `int64` / `smallint` / numeric-decimal columns; negative delta (decrement); zero delta (no-op); `ErrNotFound` on missing PK with `strict_updates: true`; idempotent success with `strict_updates: false`.
- Verify generated signatures reject non-numeric columns at compile time (golden: expected compile error for a fixture that mis-selects a text column — tested via a `//go:build ignore` fixture + `go vet` gate in the test harness, or a dedicated negative-codegen test).
- Tenancy variant in `testdata/examples/tenancy/increment_test.go`: `Increment` respects tenant filter; `SkipTenancy: true` bypasses; composite-PK tenant-mismatch returns `tenancy.ErrMismatch` before the DB round-trip.

**PRD Reference:** §9.2, §29.4.2

**Depends on:** Nothing (feature already implemented).

---

### 14.2 Core CRUD — `Exists` / `ExistsWhere` e2e

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**What to build:**
- New `exists_test.go` per dialect: `Exists(pk)` on present/absent rows; `ExistsWhere(filter)` with `In`, `Gte`, `Like`, and null-column filters; `ErrEmptyFilter` semantics for `ExistsWhere`.
- Soft-delete scoping: soft-deleted rows return `false` from `Exists`; `SkipSoftDelete: true` returns `true`.
- Tenancy scoping (in tenancy example): cross-tenant `Exists` returns `false`; `SkipTenancy: true` returns `true`.

**PRD Reference:** §9.1

**Depends on:** Nothing.

---

### 14.3 Core CRUD — `*Where` mutation edge cases

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/` and `testdata/examples/{cache,events,tenancy}/`

**What to build:**
- `where_mutations_test.go` per dialect covering `UpdateWhere`, `SoftDeleteWhere`, `HardDeleteWhere`, `RestoreWhere`:
  - `ErrEmptyFilter` fires when filter produces zero conditions (per §9.5).
  - Idempotent return: zero-rows-matched returns `nil` error and a zero affected-rows count.
  - Soft-delete filter composition: `SoftDeleteWhere` on an already-deleted row is a no-op.
  - Cache variant: one `InvalidateMany` per `*Where` call carrying exactly the matched rows' keys, and no table pattern wipe; a zero-match predicate invalidates nothing (FIX-208, cross-reference with 12.5 behavior).
  - Events variant: N rows affected → N events emitted, each with correct PK and `Input` field populated per §28.6.
- Tenancy variant: `UpdateWhere` with caller-set tenant mismatching resolver returns `tenancy.ErrMismatch`; resolver tenant is AND'd into the final `WHERE`.

**PRD Reference:** §9.3, §9.5, §28.4, §28.6, §29.4

**Depends on:** Nothing.

---

### 14.4 Cursor pagination (`Connection`) e2e

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**What to build:**
- New `connection_test.go` per dialect covering: `First`/`After`, `Last`/`Before`, forward + backward traversal across a seeded 50-row table.
- Opaque cursor round-trip: decode → encode yields byte-identical cursor; tampered cursor returns a typed error (not a panic).
- `Connection.PageInfo` (`HasNextPage`, `HasPreviousPage`, `StartCursor`, `EndCursor`) correctness at page boundaries.
- Composite-PK cursor: postgres `order_items` (3-column PK) — verify cursor encodes all PK components; forward pagination deterministic across ties.
- Tenancy variant: `Connection` on tenanted table respects tenant filter; cross-tenant rows never appear in any page.

**PRD Reference:** §9.4, §14

**Depends on:** Nothing.

---

### 14.5 Filter operator breadth

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**What to build:**
- Extend `filter_test.go` per dialect to cover every operator in §11.3:
  - `Like` / `ILike` (Postgres-only for `ILike`) on text columns; pattern with `%` and `_` escaping.
  - `Null` / `NotNull` on nullable columns.
  - `Between` on numeric and timestamp columns.
  - `Custom` raw-SQL condition (§11.3) — verify placeholder numbering is threaded correctly through the builder.
  - Composition: And/Or semantics across ≥3 conditions; nested Or inside And.
- Array / JSON column filters where supported (PG `@>`, MySQL `JSON_CONTAINS`).
- Round-trip: same filter value set produces byte-identical SQL skeleton across invocations (placeholder positions stable).

**PRD Reference:** §11.3

**Depends on:** Nothing.

---

### 14.6 Tenancy × relationships — O2O chain + M2M junction variance

**Module:** `cmd/sqlgen/testdata/examples/tenancy/`

**What to build:**
- Extend the tenancy schema with an O2O chain A → B → C, all three tables tenanted, and add `relationship_chain_test.go`:
  - `GetMany` with nested `FieldOptions` loads the full chain in one query.
  - Every JOIN-side emits the child tenant condition in the outer `WHERE` (not the `ON` clause — per §29.10 + 13.4).
  - `SkipTenancy: true` propagates to every level; absence of `SkipTenancy` re-scopes at every level.
- `m2m_junction_test.go`: parent tenanted + junction non-tenanted (`post_tags`) — assert cross-tenant leak prevention even though the junction has no tenant column; cache keys remain per-tenant for the parent side.
- Parent tenanted + junction tenanted variant: both sides AND'd into the final `WHERE`.

**PRD Reference:** §29.2.3, §29.10, §13.2

**Depends on:** Tenancy example schema extension (14.6 itself).

---

### 14.7 Tenancy error & composite-PK mismatch sweep

**Module:** `cmd/sqlgen/testdata/examples/tenancy/`

**What to build:**
- Extend `composite_pk_test.go` (or new `tenancy_errors_test.go`) to cover tenant-mismatch rejection on every mutation variant: `Update`, `Upsert`, `HardDelete`, `SoftDelete`, `Restore`, `Increment`, `Exists`, `Get` — all composite-PK cases where tenant is part of the PK (§29.7 verify-match rule).
- `ErrMissing` from `TenantResolver` returning zero with `required: true` — test on every entry-point method (Get/Create/Update/Upsert/HardDelete/SoftDelete/Restore/Increment/Exists/GetMany/CreateMany/UpdateMany/*Where/Connection).
- `required: false` + zero-value resolver: resolver is skipped, no filter added (regression guard).
- SkipTenancy with caller-supplied tenant field: round-trip SQL matches the `SkipTenancy: false` + matching-tenant case (golden comparison).

**PRD Reference:** §29.3.1, §29.4.2, §29.7

**Depends on:** Nothing.

---

### 14.8 Transactions — deferred events, savepoints, batch partial failure

**Module:** `cmd/sqlgen/testdata/examples/{events,cache,postgres}/`

**What to build:**
- `events/tx_test.go`: events emitted inside a `Tx` are deferred until commit; rollback discards all deferred events; events fire in insertion order on commit; multiple mutations in one Tx produce exactly N events on commit (not 1 per mutation at issue time).
- `postgres/tx_savepoint_test.go`: nested `Tx` uses savepoints per dialect (`SAVEPOINT` / `RELEASE SAVEPOINT` / `ROLLBACK TO SAVEPOINT`); inner rollback preserves outer Tx state; SQLite uses `ROLLBACK TRANSACTION TO SAVEPOINT` syntax (§18).
- `postgres/batch_tx_failure_test.go`: `CreateMany` spanning ≥2 sub-batches inside a Tx — first sub-batch succeeds, second fails; assert full rollback (atomicity) and error matches §9.5 wording; outside a Tx, assert the documented "previously executed sub-batches not auto-rolled back" semantics.
- Tenancy + Tx: the resolver is invoked **once per mutation**, not once at Tx open — N mutations inside one `WithTx` invoke it exactly N times, and one chained `Create` (→ terminal `Get` → `GetMany`) invokes it exactly once. *(Corrected 2026-09-18. This bullet read "tenant resolved once at Tx open; mutations inside Tx reuse the resolved value (no second resolver call per §29.6)" — the per-Tx reading, attributed to the section that says the opposite. Phase 14.8 reconciled the identical wording in `phase-14.md` and in `tx_resolver_test.go`'s header but never reached this file, which `/phase` reads. PRD §29.11 now states the per-operation rule outright, including that a mid-transaction tenant switch is undefined rather than prevented.)*

**PRD Reference:** §9.5, §18, §18.5, §28.4, §29.6

**Depends on:** Nothing.

---

### 14.9 Nullable & array type-override round-trip

**Module:** `cmd/sqlgen/testdata/examples/postgres/`

**What to build:**
- Extend `type_overrides_test.go`:
  - Nullable `uuid.NullUUID` column round-trip: insert NULL → read back `NullUUID{Valid: false}`; insert value → read back `NullUUID{Valid: true, UUID: v}`.
  - Nullable `decimal.NullDecimal` same round-trip.
  - Array-of-KSUID (`ksuid.KSUID[]`) scan from PG array column.
  - Empty-result `GetMany` on a table with overridden types returns `nil, nil` (not an allocation-laden zero-length slice scan panic).

**PRD Reference:** §7.4, §7.5

**Depends on:** Nothing.

---

### 14.10 Cache — fingerprint kill-switch, restore-after-soft-delete, view + tenancy

**Module:** `cmd/sqlgen/testdata/examples/cache/` and `testdata/examples/tenancy/`

**What to build:**
- `cache/fingerprint_test.go`: bump schema fingerprint (manually via config flag or regen with a schema edit); assert old-fingerprint keys are unreachable from new-fingerprint reads (kill-switch per §27.5).
- `cache/restore_test.go`: `SoftDeleteWhere` → row cached as absent → `RestoreWhere` → subsequent `Get` serves fresh row, not stale cache.
- `cache/view_invalidation_test.go`: cached view invalidation fires when source table mutates; if both source and view are tenanted, invalidation scope matches the mutating tenant (no cross-tenant invalidation amplification).
- `tenancy/cache_hydration_test.go`: partial-fetch hydration on tenanted soft-deleted table respects both tenant filter and soft-delete filter; background hydration does not leak cross-tenant rows.

**PRD Reference:** §27.5, §27.6, §27.11, §29.5

**Depends on:** Nothing.

---

### 14.11 Events — tenant metadata, batch events, `Input` field

**Module:** `cmd/sqlgen/testdata/examples/{events,tenancy}/`

**What to build:**
- `events/input_field_test.go`: `Event.Input` carries the exact mutation input — `CreateInput` for `Create`, `UpdateInput` for `Update`, `{filter, setClauses}` for `UpdateWhere`, etc. — round-trip type-asserted to the concrete generated type.
- `events/batch_test.go`: `CreateMany` / `UpdateMany` emit one event per entity, each with correct per-entity `PK` and `Input`; ordering matches insertion order.
- `tenancy/event_metadata_test.go`:
  - Non-tenanted table mutation → event has no `"tenant"` key in `Metadata`.
  - `SkipTenancy: true` on a tenanted-table mutation → event still carries `Metadata["tenant"]` (it reflects what was actually written, not the call option) OR is omitted if the table's tenant column was not set — assert whichever behavior matches §29.6 and document in the test.
  - Batch mutation on tenanted table → every event carries `Metadata["tenant"]` with the same value.

**PRD Reference:** §28.3, §28.4, §28.6, §29.6

**Depends on:** Nothing.

---

### 14.12 Omittable — null-vs-unset, zero-change `Update` short-circuit

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**What to build:**
- `omittable_test.go` per dialect:
  - `Update` with zero fields set on the `UpdateInput` returns the entity unchanged without issuing a DB round-trip (§9.5) — assert via a query-counting driver wrapper or a test against the generated method body.
  - Null-vs-unset round-trip on nullable columns: `omittable.Set(nil)` writes SQL NULL; `omittable.Omit()` leaves the column untouched.
  - `Create` with `omittable.Omit()` on a DEFAULT-bearing column: DB default is honored, generated SQL excludes the column from `INSERT`.
  - JSON round-trip: `omittable.Omit[T]()` field marshals to absent (via `omitzero`); `omittable.Set(v)` marshals to `v`; null JSON unmarshals to `Set(nil)` for pointer `T`.

**PRD Reference:** §9.5, §10

**Depends on:** Nothing.

---

### 14.13 Sync coverage expansion into tracker

**Module:** Documentation (`docs/tracker/`)

**What to build:**
- Run `make check` + `make check-examples` + `go test ./... -race` on the full test additions from 14.1–14.12; record any flakes/timing issues in `docs/tracker/phase-14.md`.
- Update `docs/PRD.md` only if a sub-item uncovers drift between spec and implementation (prefer `/fix` otherwise — see Scope rule above).

**Depends on:** 14.1–14.12

---

## Phase 15: Stream + LockMode

**Depends on:** Phase 14 closed (E2E coverage stable so new feature work has a known-good baseline).

**PRD Reference:** §9.1 (Read Operations table), §9.4a (Streaming Operations), §9.6 (Per-Call Options), §9.6a (Lock Modes), §4.6 (Operations config — `stream` toggle).

**Rationale:** Phase 14 closed with the unified client surface verified end-to-end across all dialect + tenancy + cache + events combinations. Phase 15 adds the two genuine capability extensions identified in the post-Phase-14 design discussion:

- **`Stream`** — memory-bounded iterator-style read for ETL / bulk processing workloads. Yields scalar columns one at a time via Go 1.23+ `iter.Seq2`; relationship-topology constraint enforced at the type level via a dedicated `Stream{Table}FieldOptions`.
- **`LockMode`** — row-level locking primitive (`FOR UPDATE`, `FOR SHARE`, `NOWAIT`, `SKIP LOCKED`) exposed as a `CallOptions` field on read methods. Enables read-modify-write workflows and `FOR UPDATE SKIP LOCKED` job queue patterns without dropping to raw SQL.

Both extend existing surfaces — they do not introduce new architectural concepts (no new modules, no new hook types beyond `OpStream`). The transaction flow, hook chain, cache layer, tenancy plumbing, and codegen template structure all absorb the additions without restructuring.

**Scope rule:** Phase 15 is feature-additive but tightly bounded. The two features are independent on paper but share a code-generation pass and several runtime helpers (`InTransaction`, `SkipCache` propagation), so they ship together rather than in separate phases. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve it outside the phase per the standing rule.

### 15.1 LockMode runtime — enum, helpers, dialect SQL emission

**Module:** `database/`, `sql/`, dialect adapters in `database/pgx/` and `database/stdlib/`.

**What to build:**
- New exported `LockMode` enum in `sql/` (or new `sql/lock.go`): `LockNone`, `LockForUpdate`, `LockForShare`, `LockForUpdateNoWait`, `LockForUpdateSkipLocked`. Constants documented per PRD §9.6a.
- Add `LockMode LockMode` field to `sql.SelectOptions`.
- Dialect-specific `BuildSelect` implementations append the lock clause after `ORDER BY`/`LIMIT`/`OFFSET`:
  - PostgreSQL: `FOR UPDATE`, `FOR SHARE`, `FOR UPDATE NOWAIT`, `FOR UPDATE SKIP LOCKED`.
  - MySQL: `FOR UPDATE`, `LOCK IN SHARE MODE`, `FOR UPDATE NOWAIT` (8.0+), `FOR UPDATE SKIP LOCKED` (8.0+).
  - SQLite: error at the `BuildSelect` call (or returned via the runtime guard in 15.2).
- New exported `database.InTransaction(ctx context.Context) bool` helper that wraps `FromContext(ctx) != nil && !tx.IsClosed()`. Single one-line addition to `database/transaction.go`.
- Optional: detect MySQL version (`SELECT VERSION()`) once at client init for the version-aware NoWait/SkipLocked guard. Cache result on the client struct.

**PRD Reference:** §9.6a (Lock Modes), §20.4 (Transactions).

**Tests required:** Unit tests in `sql/builder_test.go` covering each LockMode constant on each dialect; SQL string assertions for the appended clause. Unit tests in `database/transaction_test.go` for `InTransaction` (active tx, closed tx, no tx).

**Depends on:** Nothing — pure runtime additions.

---

### 15.2 LockMode codegen — CallOptions field, runtime guard, threading

**Module:** `cmd/sqlgen/gen/templates/`, `cmd/sqlgen/gen/funcmap.go`, `cmd/sqlgen/gen/orchestrate.go`.

**What to build:**
- `CallOptions[FO]` template gains a `LockMode LockMode` field (always emitted; non-conditional).
- Per-table `Get`, `GetMany`, `Connection` method bodies gain the runtime guard at the top of the closure:
  ```go
  if options.LockMode != LockNone {
      if !database.InTransaction(ctx) {
          return nil, fmt.Errorf("get %s: LockMode requires an active transaction", c.table.Name)
      }
      options.SkipCache = true
  }
  ```
- Methods thread `options.LockMode` into `sql.SelectOptions.LockMode`.
- Chained internal `Get` calls in `Create`/`Update`/`Upsert`/`Restore`/`SoftDelete*` write paths force `LockMode: LockNone` on the internal call (alongside the existing `SkipHooks: true`). Generated chained-Get sites updated across all relevant templates.
- Cache template (`cache.go.tmpl`) requires no change — the existing `SkipCache` short-circuit handles the cache-bypass case naturally.

**PRD Reference:** §9.6a (Transaction precondition, Hook chain interaction, Chained internal calls do not inherit LockMode), §27.7 (cache QueryHook bypass paths).

**Tests required:** Generator unit tests in `cmd/sqlgen/gen/` asserting per-template emission of the guard, the `SkipCache` force, and the `LockMode: LockNone` chain-internal exclusion. Update existing generator tests for `Get`/`GetMany`/`Connection` and write-method chained-Get sites.

**Depends on:** 15.1.

---

### 15.3 LockMode E2E

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/lock_mode_test.go`.

**What to build:**
- Per-dialect `lock_mode_test.go` covering:
  - Read-modify-write happy path under `LockForUpdate` inside `WithTx` — concurrent goroutine attempting same lock blocks, completes after commit.
  - `LockForUpdateNoWait` on a contended row returns the dialect-specific lock-not-available error within sqlgen's wrapper.
  - `LockForUpdateSkipLocked` job queue pattern: two concurrent workers each `GetMany(LIMIT 10, FOR UPDATE SKIP LOCKED)` from a 20-row backlog, both succeed, total processed equals 20 (no rows lost or double-processed).
  - Outside-transaction call returns the sqlgen "LockMode requires active transaction" error, no SQL issued.
  - SQLite returns the "unsupported on sqlite dialect" error for any non-`LockNone` mode.
  - Savepoint-doesn't-release-lock semantic — verify via two `WithTx`-nested savepoints + a third concurrent reader.
  - Cache bypass — counting cache wrapper confirms zero cache hits when `LockForUpdate` is set, even on rows previously cached via `Get`.
- Tenancy variant in `cmd/sqlgen/testdata/examples/tenancy/tests/lock_mode_test.go`: `LockForUpdate` only locks rows in the resolved tenant; cross-tenant rows remain unlocked.
- `MySQL < 8.0` fallback path — covered via a unit test in `cmd/sqlgen/gen/` (without testcontainer) since the example modules use `mysql:8.0`.

**PRD Reference:** §9.6a (full coverage of usage patterns and validation rules).

**Depends on:** 15.2.

---

### 15.4 Stream generation — types, method body, hook op

**Module:** `cmd/sqlgen/gen/templates/table/`, `cmd/sqlgen/gen/context_table.go`, `hook/hook.go`.

**What to build:**
- New `hook.OpStream` constant in `hook/hook.go` (alongside `OpGet`, `OpGetMany`, etc.).
- New per-table generated types:
  - `Stream{Table}Input` — fields: `Filter *XFilter`, `Sorts []sql.Sort`. **No** `Limit`, `Offset`.
  - `Stream{Table}FieldOptions` — bool fields for scalar columns only. **No** relationship fields. Generates `Columns()` and `HasSelectedColumns()` methods mirroring the regular FieldOptions methods.
- New `Stream` template (`cmd/sqlgen/gen/templates/table/stream.go.tmpl`) emitting the iterator body per the PRD §9.4a implementation pattern. Forces `options.SkipCache = true` after `resolveCallOptions`.
- New `scan{Table}Row` helper (single-row variant of `scan{Table}s`) emitted in the scan template; reuses existing column-resolution logic.
- New `operations.stream: bool` config field in `cmd/sqlgen/config/config.go` — default `true`. Gate Stream emission on this. Update the operations preset table so `read_only` includes `stream` (PRD §4.6 already updated to reflect this).
- Validation: `cmd/sqlgen/config/validate.go` accepts the new field; no special validation rules.
- Cache template gains an `OpStream` short-circuit at the top of `QueryHook` only if not already covered by the existing `SkipCache` early-return (it is — confirm during implementation that no extra dispatch is needed).

**PRD Reference:** §9.4a (Streaming Operations), §4.6 (Operations Presets + OperationsConfig), §27.7 (cache QueryHook bypass).

**Tests required:** Generator unit tests for `Stream` method emission (per-table), `Stream{Table}Input` and `Stream{Table}FieldOptions` struct shape, scalar-only-FieldOptions invariant (no relationship fields ever emitted), `OpStream` hook op constant, `scan{Table}Row` helper emission.

**Depends on:** Nothing. Independent of 15.1–15.3 — Stream and LockMode share Phase 15 only because they ship together; no code dependency.

---

### 15.5 Stream E2E

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/stream_test.go`.

**What to build:**
- Per-dialect `stream_test.go` covering:
  - Memory-bounded iteration: stream a 10k-row table, verify peak memory usage stays well below the materialized equivalent (process-level RSS or `runtime.MemStats` heap delta).
  - Early termination via `break` releases rows + connection cleanly (deferred `rows.Close` runs).
  - Filter / sort propagation: `WHERE` and `ORDER BY` correctly applied to the streaming SELECT.
  - Hook chain integration: tenancy filter narrows streamed rows; soft-delete filter excludes deleted rows from stream output.
  - `SkipCache` force: counting cache wrapper confirms zero cache hits / zero writes for streamed rows.
  - Iterator yields `(nil, err)` and terminates on connection failure mid-stream.
  - `StreamFieldOptions{ID: true, Name: true}` selects only those columns (verify via `rows.Columns()` capture in the test querier wrapper).
- Tenancy variant in `cmd/sqlgen/testdata/examples/tenancy/tests/stream_test.go`: tenant filter applied; `SkipTenancy: true` streams across tenants.
- Document the per-driver buffering caveat (MySQL stdlib buffers full result set client-side without streaming flags) in the test file header — informs future consumers.

**PRD Reference:** §9.4a (full coverage of constraints, use cases, hook integration).

**Depends on:** 15.4.

---

### 15.6 Phase closure — tracker sync + integration sweep

**Module:** Documentation (`docs/tracker/phase-15.md`, `docs/tracker/STATUS.md`).

**What to build:**
- Run `make check` + `make check-examples` + `make test-integration` after every 15.1–15.5 sub-item lands; record any flakes/timing in `docs/tracker/phase-15.md`.
- `/fix` triage: any pre-existing bug uncovered during 15.x test bring-up gets a FIX entry, not in-place scope expansion.
- PRD reconciliation: §9.4a and §9.6a were drafted pre-implementation. Any divergence between the spec and landed codegen gets reconciled either inline at the test-file header (test-only divergences) or via a targeted PRD edit (behavioral divergences) — same pattern as Phase 14's closing sweep.
- Update `docs/tracker/STATUS.md` Current Focus to reflect Phase 15 closure.

**Depends on:** 15.1–15.5.

---

## Phase 16: GraphQL API Generation

> **Status: Complete (closed 2026-09-16).** All 9 sub-items (16.1–16.9, with 16.8 decomposed into 16.8a–g) landed; the 16.8 umbrella closed 2026-05-14 and 16.9 — the closure sub-item — 2026-09-16. Normative spec PRD §26 (§26.3 config, §26.4 / §26.4.1 schema + scalars, §26.5.x resolvers, §26.10 per-table gating). Full sweep clean under `-race` (check 1m29.8s / check-examples 2m00.2s / test-integration 1m31.1s), no flakes. 0 FIX entries originate in the phase — the ~20 surfaced during 16.8 bring-up (FIX-064, FIX-065–FIX-084) were resolved within it; 2 pre-dating entries carried forward (FIX-199, FIX-200). Working design `docs/design/archive/GRAPHQL.md` (archived 2026-09-16 — superseded by PRD §26, which 25.0 and 26.0 have amended since the original sync).

**Depends on:** Phase 15 closed (unified client surface stable; Stream + LockMode landed and reconciled in PRD).

**PRD Reference:** §26 (`API Generation`) — full GraphQL spec lives in §26.3 (config), §26.4 (schema generation), §26.4.1 (scalar marshaling), §26.5 (resolver generation: §26.5.1 exposed surface, §26.5.2 field-selection walker, §26.5.3 filter/sort/pagination translation, §26.5.4 mutation translation including `_inc`/`_dec`, §26.5.5 error mapping, §26.5.6 module boundary + wrapper subcommand, §26.5.7 no-dataloader rationale), §26.10 (per-table gating).

**Rationale:** Phase 15 closed the direct-Go-consumer surface. Phase 16 extends sqlgen to non-Go consumers via a generated GraphQL layer that sits in front of the unified client. The pipeline is schema-driven end-to-end: SQL schema → existing sqlgen models → generated `.graphqls` + concrete gqlgen resolver impls + recursive selection-set walkers + filter / sort / pagination translators + HTTP-header → CallOptions middleware. The §26.5.2 walker is the load-bearing piece — every guarantee (no N+1, no dataloader, query-count contract per §25.1) depends on the walker being recursive and complete.

**Reference design:** `docs/design/archive/GRAPHQL.md` is the working design document used during the Phase 16 design phase. After the PRD §26 sync it became a stale reference and was archived (moved there from `docs/GRAPHQL.md` on 2026-09-16, per 16.9).

**Scope rule:** Phase 16 ships the GraphQL surface only. REST API generation (the existing PRD §26.8 Supplementary section) is deferred to a post-Phase-16 follow-on that reuses the schema-traversal scaffolding from this phase. gRPC remains in PRD §26.13 as a "Future" item. Subscriptions are out of scope per PRD §26.12 (deferred until the WebSocket/replay design questions get answered).

**Module boundary invariant:** `cmd/sqlgen` does NOT import gqlgen (`github.com/99designs/gqlgen`). Generated files reference gqlgen APIs by literal string in templates; the `sqlgen graphql gen` wrapper invokes the gqlgen binary as a subprocess via `os/exec` (resolved from the consumer module via `go run`). The runtime module (`./`) stays stdlib-only — even the new `MarshalGQL` / `UnmarshalGQL` methods on `types.JSON` / `types.DateTime` / `types.NullDateTime` use only `io.Writer` and `any`. See PRD §26.5.6.

### 16.1 Schema generation + scalar marshaling

**Module:** `cmd/sqlgen/gen/templates/api/`, `cmd/sqlgen/gen/context_api.go`, `cmd/sqlgen/gen/orchestrate.go`, runtime `types/` (for the new `MarshalGQL`/`UnmarshalGQL` methods).

**What to build:**
- Schema-walker producing `graph/*_gen.graphqls` per the SQL → GraphQL mapping in PRD §26.4.
- SQL → GraphQL type mapping per §26.4 table (PascalCase types, `field_casing` for fields, NOT NULL / nullable propagation, scalar resolution per the §26.4.1 registry).
- Custom scalar config validation: `api.graphql.scalars` declared types must exist in the parsed schema's used types (registry covers `types.JSON`, `types.DateTime`, `types.NullDateTime`, `uuid.UUID`, `decimal.Decimal`, `time.Time`, `json.RawMessage`).
- **Runtime-module change (stdlib-only):** add `MarshalGQL(io.Writer)` and `UnmarshalGQL(any) error` methods to `types.JSON`, `types.DateTime`, `types.NullDateTime`. Test in the runtime test suite — JSON round-trip across all four shapes (object, array, scalar, null) for `types.JSON`, nullable round-trip for `types.NullDateTime`, error paths. Note: unlike the prior `types.JSONMap`, `types.JSON.UnmarshalGQL` accepts non-object inputs without rejecting (see PRD §7.6).
- Built-in scalar registry: known types and marshaling mode (per the §26.4.1 table). `types.JSON` / `DateTime` / `NullDateTime` → method-based via runtime methods (no codegen output); `uuid.UUID` / `decimal.Decimal` → external; `json.RawMessage` → external (binds to the same `JSON` scalar as `types.JSON`); `time.Time` → gqlgen-bundled; primitives → spec built-in.
- Emit-on-use scalar declarations: when a column's Go binding resolves to a sqlgen-shipped scalar type (`JSON`, `DateTime`), include `scalar X` in the generated `.graphqls` exactly once per scalar.
- Emit `graph/scalars_gen.go` with `MarshalX` / `UnmarshalX` for every category-4 scalar in use (skipping category-3 sqlgen-shipped types).
- Codegen error for unknown scalars in `api.graphql.scalars` that lack a `marshaling:` declaration.

**Tests required:** Generator unit tests (`TestGraphQLSchema_emitsExpectedTypesPerTable`, `TestGraphQLSchema_respectsFieldCasing`, `TestGraphQLSchema_excludesDisabledOperations`); per-scalar round-trip via the emitted Marshal/Unmarshal or the runtime method; integration test that loads the merged `gqlgen.yml` and confirms gqlgen accepts the bindings.

**Depends on:** Nothing — pure additive codegen + runtime method additions.

---

### 16.2 gqlgen wrapper subcommand

**Module:** `cmd/sqlgen/cli/`, `cmd/sqlgen/wrapper/` (new package for the YAML merge logic).

**What to build:**
- `sqlgen graphql init` — one-shot scaffold of `gqlgen.yml` (only if missing; never overwrites). Sets up the `schema:` glob to include `graph/*.graphqls`.
- `sqlgen graphql gen` — the wrapper described in PRD §26.5.6:
  1. Read `gqlgen_config` (consumer-owned `gqlgen.yml`) into a YAML AST.
  2. Merge sqlgen-owned entries: `models:` for managed tables, `schema:` glob for `graph/*.graphqls`, `scalars:` from `api.graphql.scalars`, scalars derived from column types. **Consumer-authored keys win on collision.**
  3. Write the merged config to a temp file and run the gqlgen binary against it via `os/exec`.
  4. Remove the temp file on success. The on-disk `gqlgen.yml` is untouched.
- YAML-AST merge with consumer-wins precedence on key collision.
- No new Go dependency on gqlgen in `cmd/sqlgen` (subprocess only).

**Tests required:** Wrapper unit tests covering greenfield init, layered-on-existing merge (custom directives + extra models survive), scalar-collision precedence (consumer wins), malformed-config error path, temp-file cleanup on both success and subprocess failure.

**Depends on:** 16.1.

---

### 16.3 Resolver scaffolding — curated surface

**Module:** `cmd/sqlgen/gen/templates/api/resolvers.go.tmpl`, `cmd/sqlgen/gen/context_api.go`.

**What to build:**
- gqlgen interface satisfaction (Query, Mutation per table).
- **Curated surface per PRD §26.5.1** — emit only the resolvers in scope: `<table>`, `<table>s` (Connection), `<table>List` (ListResult envelope), `create<Table>`, `create<Table>s`, `update<Table>`, `update<Table>s`, `upsert<Table>`, `delete<Table>`, `softDelete<Table>`, `restore<Table>`. Do NOT emit resolvers for `Exists`, `Count`, `GetMany`, or `Find`. Per-table emit a `<Type>ListResult` envelope (items, totalCount, offset, limit).
- Per-table gating: each resolver only emitted when the table's resolved `operations` config includes the corresponding op AND prerequisites are met (e.g., `softDelete<Table>` requires a soft-delete column, `upsert<Table>` requires a PK or unique constraint).
- Concrete resolver impls calling unified client.
- Pass-through `FieldOptions` from translator (placeholder until 16.4).
- Mutation chaining via existing `Create`/`Update`/`CreateMany`/`UpdateMany` FieldOptions support (no per-resolver Get).
- `_inc` / `_dec` input-operator dispatch in update translator (PRD §26.5.4) — `_set` then `Increment` then `Decrement`, with codegen rejection of conflicting `_set` + `_inc` and runtime rejection of `_inc` + `_dec` on the same column.
- Delete/restore naming rules per PRD §26.5.1 (`hard-only` → bare `delete<Table>`; `soft-only` → bare `delete<Table>` returning the entity + `restore<Table>`; `both enabled` → `hardDelete<Table>` + `softDelete<Table>` + `restore<Table>`, no bare `delete<Table>`).
- Mutation-result nullability per PRD §26.5.1 (create/update/upsert/createMany/updateMany return non-nullable; delete-class soft / restore return nullable; hardDelete returns `Boolean!`).

**Tests required:** Resolver scaffolding unit tests covering surface inclusion/exclusion under each operations preset, `_inc` / `_dec` translator dispatch, soft-vs-hard delete naming rules, mutation-result nullability per op shape.

**Depends on:** 16.1, 16.2.

---

### 16.4 Field selection translator (the central walker)

**Module:** `cmd/sqlgen/gen/templates/api/field_options.go.tmpl`, `cmd/sqlgen/gen/templates/api/connection_walker.go.tmpl`.

**What to build:**
- Recursive `fieldOptionsFromCollected` walker per table (PRD §26.5.2 emission shape).
- Connection unwrapping helper (`unwrapConnectionAndWalk`) — single shared impl in `graph/connection_walker_gen.go`.
- **Walker-completeness lint (codegen-time):** every column / relationship in `parser.Schema` MUST have a corresponding case in the generated walker — fail codegen if any are missing.
- Walker unit tests with fixture queries (deeply-nested selection touching every relationship; assert produced FieldOptions tree matches expected shape; verify `@skip` / `@include` / fragment / alias handling via gqlgen's `CollectFieldsCtx`).
- Document the "selected ⇒ available, unselected ⇒ nil" contract; child resolver returns nil if walker missed a case (impossible if lint passes; fail-fast guard).

**Tests required:** Per-table walker test fixture; walker-completeness lint negative test (introduce a missing case, confirm codegen fails); fragment/directive coverage tests using gqlgen test helpers.

**Depends on:** 16.3.

---

### 16.5 Filter / sort / pagination translators

**Module:** `cmd/sqlgen/gen/templates/api/filter_translate.go.tmpl`, `cmd/sqlgen/gen/templates/api/comparator_translate.go.tmpl`, `cmd/sqlgen/gen/templates/api/sort_translate.go.tmpl`.

**What to build:**
- Per-comparator family translator (shared, NOT per-table) — one impl per `comparator.X` in `graph/comparator_translate_gen.go`. Avoids combinatorial explosion across tables.
- Per-table `translateXFilter` driving comparators (PRD §26.5.3 shape).
- Sort translator + per-table `<Type>SortField` enum + `<table>SortFieldToColumn` switch.
- Pagination pass-through (Connection args → `database.ConnectionInput`; `<table>List` args → `models.ListInput[F]`).

**Tests required:** Filter translator covers and/or/not nesting, includeDeleted passthrough, every comparator family per dialect; sort translator pins the column-name lookup is generated correctly; pagination is a pass-through test.

**Depends on:** 16.3.

---

### 16.6 Error mapping + middleware

**Module:** `cmd/sqlgen/gen/templates/api/errors.go.tmpl`, `cmd/sqlgen/gen/templates/api/middleware.go.tmpl`.

**What to build:**
- `mapErrorToGQL` covering the PRD §26.5.5 sentinel table (`ErrNotFound` → `NOT_FOUND`, `ErrUniqueViolation` → `CONFLICT`, `ErrForeignKeyViolation` → `BAD_REFERENCE`, `ErrCheckViolation` / `ErrNotNullViolation` → `INVALID_INPUT`, `tenancy.ErrMissing` → `UNAUTHENTICATED`, `tenancy.ErrMismatch` → `FORBIDDEN`, fallthrough → `INTERNAL`).
- `WithCallOptionsMiddleware` HTTP wrapper (PRD §26.11) — extracts `Cache-Control: no-cache`, `X-Skip-Events: true`, `X-Skip-Hooks: true` from the request and stashes a `[]func(*CallOptions)` slice into ctx.
- `callOptionsFromHTTP` resolver helper that pulls the slice back out and applies it to the per-call `CallOptions`.

**Tests required:** Unit test for every sentinel → extension code mapping; middleware test for header → CallOptions translation; resolver test confirming options propagate through to the unified client call.

**Depends on:** 16.3.

---

### 16.7 Multi-schema + per-table API config

**Module:** `cmd/sqlgen/gen/context_api.go`, `cmd/sqlgen/gen/orchestrate.go`.

**What to build:**
- Multi-schema disambiguation per PRD §26.4 / FIX-027: `audit.users` → GraphQL type `AuditUser`, query field `auditUser(id)`, list `auditUsers(...)`. Reuse `cmd/sqlgen/gen/context_table.go::buildStructName` so the GraphQL prefix matches the Go prefix.
- `api.enabled: false` (table-level) — type and all queries/mutations excluded from schema.
- `operations` preset filtering — every preset (read_only / append_only / no_delete / no_hard_delete / all) gates the surface per PRD §26.5.1 + §26.10.
- `tenancy.required: true` interaction — tenant resolver runs server-side; failure surfaces as `UNAUTHENTICATED` per the §26.5.5 mapping.

**Tests required:** Multi-schema example fixture confirms `auditUser` / `auditUsers` types coexist with `user` / `users`; per-preset emission tests confirm correct surface narrowing; tenancy missing-resolver test confirms `UNAUTHENTICATED` propagates through `mapErrorToGQL`.

**Depends on:** 16.3, 16.6.

---

### 16.8 E2E example module

**Module:** `cmd/sqlgen/testdata/examples/graphql/` (new) — same shape as `tenancy` and `events` examples (self-contained Go module with `sqlgen.yml`, `schema.sql`, `expected/` goldens, `tests/` dir with `-race` integration tests).

**What to build:**
- Real `gqlgen` server booted via `sqlgen graphql gen` + actual GraphQL queries against the running server.
- Verify response shape (`PageInfo`, `Edges`, `ListResult`), error codes (`extensions.code`), and DB query counts (counting `Querier` wrapper pinning §25.1's `1 + count(O2M selected) + 2 * count(M2M selected)`).
- Layered-mode regression test: pre-seed a `gqlgen.yml` with custom directives + extra models, run the wrapper, assert the consumer keys survive and sqlgen keys are merged in.
- Goldens under `cmd/sqlgen/testdata/examples/graphql/expected/` — `*_gen.graphqls`, `resolvers_gen.go`, `field_options_gen.go`, `filter_translate_gen.go`, `comparator_translate_gen.go`, `errors_gen.go`, `middleware_gen.go`, `scalars_gen.go`. Regenerated via `make update-golden-e2e`.
- Walker-completeness regression: deliberately mis-structured fixture confirms the §26.5.2 lint catches missing cases.

**Tests required:** Full E2E coverage of every curated-surface mutation/query at least once; per-`extensions.code` error path; cache-bypass via `Cache-Control: no-cache` header; tenancy hook composes correctly with GraphQL resolvers.

**Depends on:** 16.1–16.7.

---

### 16.9 Phase closure — PRD sync, GRAPHQL.md archival, tracker update

**Module:** Documentation (`docs/PRD.md`, `docs/design/archive/GRAPHQL.md`, `docs/tracker/IMPLEMENTATION_ORDER.md`, `docs/tracker/phase-16.md`, `docs/tracker/STATUS.md`).

**What to build:**
- Run `make check` + `make check-examples` + `make test-integration` after every 16.1–16.8 sub-item lands; record any flakes / timing in `docs/tracker/phase-16.md`.
- `/fix` triage: any pre-existing bug uncovered during 16.x test bring-up gets a FIX entry, not in-place scope expansion.
- PRD reconciliation: §26 was synced from `docs/design/archive/GRAPHQL.md` at the start of Phase 16 (this PRD edit happened pre-implementation per the same pattern as Phase 15's §9.4a / §9.6a). Any divergence between the spec and landed codegen gets reconciled either inline at the test-file header (test-only divergences) or via a targeted PRD edit (behavioral divergences) — same pattern as Phase 14 / Phase 15 closing sweeps.
- **Archive `docs/GRAPHQL.md`.** Per its §21 sync plan, it became a stale reference once §26 was fully synced. **Done 2026-09-16:** moved to `docs/design/archive/GRAPHQL.md` with a redirect header pointing readers at PRD §26.
- Update `docs/tracker/STATUS.md` Current Focus to reflect Phase 16 closure.
- Confirm every 16.1–16.8 Completion Record is filled in.

**Depends on:** 16.1–16.8.

---

## Phase 17: Sub-Categorized Polymorphism

**Depends on:** Phase 16 closed (the graphql example provides the test vehicle for per-relationship walker emission; with both layouts working in 16.8, Phase 17 doesn't need to revisit the gqlgen wrapper).

**PRD Reference:** §13.7 (Sub-Categorized Polymorphism — the post-2026-05-12 simplified rewrite), §4.8 (TableRelationship `filter:` field — the foundation §13.7 extends).

**Rationale:** PRD §13.7 was rewritten 2026-05-12 to drop the originally-spec'd discriminator-style filter parser, enum-value validation, and cache-fingerprint plumbing — all of which contradicted §27.6 (relationships are cache-bypass) or required infrastructure with no consumer pull. The simplified model just relaxes the relationship dedup key from `(target, fk)` to `(target, fk, filter)` (byte-equal compare). Phase 17 implements that relaxation end-to-end: config validation, per-relationship FieldOptions field emission, per-relationship loader generation, and example fixtures exercising the asset/documents schema across postgres / mysql / sqlite / graphql.

**Scope rule:** Phase 17 is feature-additive but tightly bounded — same shape as Phase 15. The deferred enhancements in PRD §13.7.4 (codegen-time filter validation, enum literal validation, reverse dispatch) are NOT in scope; defer to a future phase if consumer demand materializes. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve outside the phase per the standing rule.

### 17.1 Config validation + relationship dedup key extension

**Module:** `cmd/sqlgen/config/validate.go`, `cmd/sqlgen/gen/context_table.go`.

**What to build:**
- Update relationship validation:
  - Group relationships per parent by `(target_table, fk_column)` for o2o/o2m, `(target_table, junction_table, junction_local_fk, junction_reference_fk)` for m2m.
  - Within each group, detect duplicates by the full key including byte-equal `filter:` comparison. Two relationships sharing the full key → hard validation error citing PRD §13.7.3.
  - Two relationships sharing the prefix but with distinct `filter:` values → allowed; require `name:` distinct (already a §4.8 requirement, validate explicitly here).
  - Remove or relax any pre-existing single-relationship-per-(target,fk) check.
- Update `TableContext` plumbing in `cmd/sqlgen/gen/context_table.go`:
  - The per-table relationship list passed into `TableContext.Relationships` carries multiple entries for the same `(target, fk)` pair after the dedup relaxation. Each entry produces its own field on `<Table>FieldOptions` (the existing `Name` field disambiguates).
  - No struct-shape changes needed — purely a relaxation of the upstream filter.

**PRD Reference:** §13.7.1, §13.7.3.

**Tests required:** Table-driven validation tests in `cmd/sqlgen/config/validate_test.go` covering the two §13.7.3 rules (duplicate full key → error; distinct filter on shared prefix → allowed; same for m2m). Generator tests in `cmd/sqlgen/gen/context_table_test.go` confirming the multi-entry shape lands as expected.

**Depends on:** Nothing — pure config-side change.

---

### 17.2 Codegen — per-relationship loader emission

**Module:** `cmd/sqlgen/gen/templates/table/`, `cmd/sqlgen/gen/templates/api/`, `cmd/sqlgen/gen/orchestrate.go`.

**What to build:**
- The per-table relationship loader template (`relationship_load.go.tmpl` or equivalent) already accepts a per-relationship `filter:` and AND's it onto WHERE. With 17.1's dedup change, the per-table loader generation iterates the multi-entry relationship list and emits one loader call per relationship. Likely no template change required for the SQL builder side — only confirmation that the iterating context flows correctly.
- GraphQL field-options walker (`cmd/sqlgen/gen/templates/api/field_options.go.tmpl`): emit one `case "<field>":` per relationship using the relationship's `name:` as the GraphQL field name (camelCased per §26.5.2 conventions).
- GraphQL schema generator (`cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl`): emit one selectable field per relationship on the parent's GraphQL type.
- The walker-completeness lint (PRD §26.5.2) automatically covers per-relationship field emission since it iterates declared fields against case clauses — no lint changes needed.

**PRD Reference:** §13.7.1, §26.5.2.

**Tests required:** Generator unit tests in `cmd/sqlgen/gen/api_walker_test.go` asserting per-relationship case emission for a parent with multiple sub-categorized relationships (in-memory fixture). Template golden tests in `cmd/sqlgen/gen/` pinning the per-relationship FieldOptions field generation + loader emission shape against a synthetic fixture.

**Depends on:** 17.1.

---

### 17.3 E2E example + cross-dialect integration tests

**Module:** `cmd/sqlgen/testdata/examples/postgres/`, `cmd/sqlgen/testdata/examples/mysql/`, `cmd/sqlgen/testdata/examples/sqlite/`, `cmd/sqlgen/testdata/examples/graphql/`.

**What to build:**
- Extend the postgres example's `schema.sql` with `assets` and `documents` tables. `documents.entity_type` is a PG enum type (`document_entity_type_enum`); `documents.entity_id` is the FK column. Mirror the schema in `mysql/` (use `ENUM('asset.primary', ...)` column type) and `sqlite/` (use `TEXT` since SQLite has no enums).
- Add three relationship entries on `assets` in each `sqlgen.yml`: `PrimaryDocument` (o2o, `filter: "entity_type = 'asset.primary'"`), `Attachments` (o2m, `entity_type = 'asset.attachment'`), `Invoices` (o2m, `entity_type = 'asset.invoice'`).
- Mirror in the graphql example so the API walker surfaces the asset/documents schema.
- Regenerate goldens via `make update-golden-e2e`. Verify diffs are localized to the new tables — no spillover into existing fixtures.
- Per-dialect integration test (`cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/sub_categorized_polymorphism_test.go`): seed assets + documents covering all three discriminator values plus a stranded `spv` value; query an asset with all three relationships set; assert each returns its proper subset and the stranded row appears in none.
- Curated-surface test in the graphql example confirming the three nested fields each return only their discriminator's rows.

**PRD Reference:** §13.7.1, §13.7.3.

**Tests required:** Per-dialect `sub_categorized_polymorphism_test.go` covering happy path + stranded-row exclusion; graphql curated-surface test; golden file regen across all four affected examples (postgres / mysql / sqlite / graphql).

**Depends on:** 17.2.

---

### 17.4 Phase closure sweep

**Module:** Documentation only.

**What to build:**
- `make check` + `make check-examples` + `make test-integration` clean across the full repo under `-race`.
- Confirm 17.1–17.3 Completion Records are filled in.
- Update `docs/tracker/STATUS.md` Current Focus to reflect Phase 17 closure with the surface delta summary.
- `/fix` triage: any pre-existing bug uncovered during 17.x test bring-up gets a FIX entry, not in-place scope expansion.
- Optional PRD reconciliation: any spec-vs-landed-codegen divergence reconciled inline at the test file header (test-only) or via a targeted PRD edit (behavioral) — same pattern as Phase 14 / 15 / 16 closure sweeps.

**Depends on:** 17.1–17.3.

---

## Phase 18: Manifest

**Depends on:** Phase 16 closed (graphql example provides the separate-graph-package breadcrumb test vehicle + the side-effect `.graphqls` description fix); Phase 17 closed (multi-relationship walker shape stable, so per-entity index / relationship metadata reflects the post-17 surface).

**PRD Reference:** §30 (canonical), §4.6 (config), §4.13 (validation), §8.3 (artifact table), §23.1 (CLI commands).

**Rationale:** Sqlgen consumers with 100+ tables produce generated packages of 500k+ LOC, which AI agents cannot consume efficiently — agents either pull single files that exceed context budgets, or grep blindly without shape. The manifest fills this gap with a small, structured, deterministic description of the generated surface, written for both agents (via on-disk JSON + markdown + auto-discovered breadcrumbs) and Go consumers (via runtime-embedded typed structs on the generated `Client`). The feature is opt-in (`manifest.enabled: true`) and purely additive — no generated Go behavior changes when off. Design supplement at `docs/design/MANIFEST.md` carries open-question history and rationale; PRD §30 is the normative spec.

**Scope rule:** Phase 18 is feature-additive but tightly bounded — same shape as Phase 15 / 17. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve outside the phase per the standing rule, EXCEPT the GraphQL `.graphqls` description wiring which is bundled into 18.2 because it shares the same parser-comment data path the manifest builder needs (rationale in MANIFEST.md §11 Resolved).

**Runner notes:** Config-side changes land in `cmd/sqlgen/config/`. New runtime package `manifest/` at the repo root, peer to `database/`, `cache/`, `comparator/`, `omittable/` — stdlib-only per the runtime invariant in PRD §3. Manifest builder + emitter + CLI subcommands live in a new `cmd/sqlgen/manifest/` package. Pipeline integration in `cmd/sqlgen/gen/orchestrate.go`. E2E coverage extends all four example modules (postgres / mysql / sqlite / graphql). `make check` + `make check-examples` + `make test-integration` must all pass before each sub-item is marked complete.

---

### 18.1 Config schema + validation

**PRD Reference:** §30.2, §4.6 (Manifest subsection), §4.13 (validation rules).

**Module:** `cmd/sqlgen/config/types.go`, `cmd/sqlgen/config/resolve.go`, `cmd/sqlgen/config/validate.go`.

**What to build:**
- `ManifestConfig` / `BreadcrumbsConfig` / `TableManifestConfig` types per PRD §30.2 with YAML tags and pointer-bool fields for nullable defaults.
- `JSONLayout` named-string alias with `JSONLayoutSingle` / `JSONLayoutPerEntity` constants.
- `RootConfig.Generation.Manifest` field plumbed through the resolution pipeline. Per-table override resolved per PRD §4.6's resolution-order rule.
- All validation rules from PRD §4.13's new manifest entries.

**Tests required:** `cmd/sqlgen/config/validate_test.go` with one positive + one negative case per validation rule; resolution tests confirming per-table opt-out + defaults applied when fields unset.

**Depends on:** Nothing — pure config-side change.

---

### 18.2 Manifest builder package

**PRD Reference:** §30.4 (entity shape), §30.7 (extended metadata), §26.4 (GraphQL description wiring side-effect).

**Module:** `cmd/sqlgen/manifest/builder.go`, `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl`.

**What to build:**
- `Build(schema, cfg, tables[], api?) (*manifest.Document, error)` entry point in `cmd/sqlgen/manifest/builder.go` consuming existing in-memory contexts. Read-only against current state; no re-parsing.
- Per-entity / per-column / per-method transformation into the manifest types (from the new runtime `manifest/` package, defined in 18.7).
- **Extended metadata extraction** (PRD §30.7): `COMMENT ON TABLE` / `COMMENT ON COLUMN` (parser already populates `Table.Comment` / `Column.Comment` end-to-end — PG `pg_description`, MySQL `information_schema`, file-parser tests pin both); full index list (UNIQUE + non-unique + partial + GIN/GIST); CHECK constraints flattened onto participating columns; `default_kind` inference (`literal` / `function` / `expression`); `features.events.types[]` from event config; `features.cache.key_pattern` from cache config + PK shape.
- **MCP-driven addenda** (PRD §30.4.3 + §30.7; added 2026-05-15 for Phase 19 dependency, see `docs/design/MCP.md` §10 "Scope expansion" and `docs/design/MANIFEST.md` §5.1.1 + §5.3):
  - Compute the top-level `generation_config` snapshot from the resolved package-wide config: boolean toggles for `cache`, `tenancy`, `events`, `soft_delete`, `views`, `graphql`, `graph_top_level`, `audit_columns`, and `pagination`. `graph_top_level` is inferred from the root-relative `resolver_dir` → `output.dir` path relationship (Phase 20 / §30.4.3), not read from a flag. Pure read over the existing resolved config — no new inputs, no secrets / DSNs / paths.
  - Hook into the existing SQL-build pipeline (where `_gen.go` query bodies are composed) to persist each method's canonical SQL into the in-memory `*manifest.Document` as `methods[].sql_bodies: {<dialect>: string}`. Single-dialect packages produce a single-key map keyed on `cfg.Dialect`. Placeholders emitted as the dialect renders them (`$1`/`?`); filter / sort / pagination use `<filter>` / `<sort>` / `LIMIT $N` tokens. Unit tests cover capture for FindByID / FindBy* unique-index / List / Count / Walk / Create / Update / Delete / Upsert*.
- **GraphQL `.graphqls` description wiring (side-effect, bundled in this sub-item):** update `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl` so type-level and field-level `"""description"""` blocks read `Table.Comment` / `Column.Comment` when non-empty, falling back to the existing template-rendered placeholder only when empty. Pre-existing Phase 16 gap (parser had the data, templates emitted placeholders); fixed here because the builder is touching the same parser fields anyway. Manifest and `.graphqls` files agree on descriptions after this lands.
- No emission yet — builds the in-memory `*manifest.Document` only.

**Tests required:** `cmd/sqlgen/manifest/builder_test.go` table-driven over synthetic schemas (single-PK, composite-PK, tenanted, soft-deleted, cached, with-events, with-views, m2m) plus each extended-metadata branch. Golden tests in `cmd/sqlgen/gen/` for the GraphQL template description-wiring (with-comment fixture + fallback fixture).

**Depends on:** 18.1.

---

### 18.3 JSON emission + JSON Schema

**PRD Reference:** §30.4 (shape), §30.5 (versioning).

**Module:** `cmd/sqlgen/manifest/emit_json.go`, `cmd/sqlgen/manifest/schema/v1.json`.

**What to build:**
- Deterministic JSON encoder handling both `single` and `per_entity` layouts per PRD §30.4. Custom encoder with HTML-escaping disabled; pre-sorted iteration; no `range` over map.
- `cmd/sqlgen/manifest/schema/v1.json` — JSON Schema definition covering the canonical shape including extended-metadata fields from 18.2.
- `--manifest-timestamp` flag on the `generate` CLI for golden-test determinism.
- Stale-file cleanup integration for per-entity JSON files and the `manifest/` directory on opt-out.
- **MCP-driven addenda (added 2026-05-15):** schema covers `generation_config` (top-level, required) and `methods[].sql_bodies` (per-method, optional but emitted whenever 18.2's SQL capture succeeded). Encoder sorts the `sql_bodies` dialect keys + `generation_config` field keys for byte-stable output. A manifest emitted without `generation_config` is invalid against schema v1; manifests without `sql_bodies` validate (the field is optional at schema level) but the MCP server surfaces `-32005 METHOD_SQL_UNAVAILABLE` for any method missing it.

**Tests required:** byte-equal golden snapshots for both layouts; `TestEmitJSON_Deterministic` (twice-emit byte equality); `TestEmitJSON_ValidatesAgainstSchema` (every example validates against `schema/v1.json` using `github.com/santhosh-tekuri/jsonschema/v5` as test-only dep — promoted to CLI dep in 18.8); schema-rejection tests confirming a manifest missing `generation_config` fails validation.

**Depends on:** 18.2.

---

### 18.4 Markdown emission

**PRD Reference:** §30.3 (file layout), §30.4.1 (conventions block).

**Module:** `cmd/sqlgen/manifest/emit_markdown.go`, `cmd/sqlgen/manifest/templates/{index,conventions,entity}.md.tmpl`.

**What to build:**
- `_index.md.tmpl` — package-wide entity scan table.
- `_conventions.md.tmpl` — entry points + error sentinels (GraphQL codes) + pagination + `CallOptions` + soft-delete + **Comparator** package walkthrough (families, fields, composition) + **Omittable** package walkthrough (purpose, construction, methods, JSON semantics).
- `entity.md.tmpl` — per-entity markdown with sections: header, files, columns table (with `comment` column from `Column.Comment`), indexes table (new), CHECK-constraints subsection (when any column has `check`), relationships, query / mutation methods, filter, sort, multi-line read + write examples.
- `text/template` execution over pre-sorted slices; deterministic byte output.
- Per-entity filename matches the SQL table name verbatim (reuses existing `_gen.go` file prefix).
- **MCP-driven addenda (added 2026-05-15):**
  - Per-method **Generated SQL** subsection in `entity.md.tmpl`, sourced from `methods[].sql_bodies`. One `sql`-fenced code block per dialect key (single-dialect packages: one block). Renders syntax highlighting cleanly in GitHub / mkdocs.
  - `_index.md.tmpl` gains a **Features** line near the top sourced from `generation_config`, listing the enabled package-wide features as a comma-separated list (e.g., "Features: cache, events, soft_delete, pagination"). Falls back to "Features: none" when all toggles are false.

**Tests required:** byte-equal golden snapshots for index, conventions, and a per-entity file. Cover the new metadata fields, including: per-method Generated SQL block (with + without sql_bodies fallback), features line on `_index.md` (multiple toggles on, all off).

**Depends on:** 18.2.

---

### 18.5 Breadcrumb files + package doc-comment pointer

**PRD Reference:** §30.3.

**Module:** `cmd/sqlgen/manifest/emit_breadcrumbs.go`, `cmd/sqlgen/manifest/templates/breadcrumb.md.tmpl`.

**What to build:**
- Emit identical `CLAUDE.md` + `AGENTS.md` at the output package root per `breadcrumbs.claude_md` / `breadcrumbs.agents_md`. Single template drives both.
- **Separate graph-package variant:** when GraphQL is enabled, the breadcrumbs in the graph package (at the resolved `resolver_dir` — nested `<output.dir>/graph`, or a top-level sibling when `generation_config.graph_top_level`) use a different template pointing at three sources (models manifest via relative path, sibling `*_gen.graphqls`, PRD §26). Relative path computed at generation time.
- Extend the existing per-file generated-header pass to inject the manifest pointer into `models_gen.go`'s `// Package <name>` doc comment per `breadcrumbs.package_doc`. Other `*_gen.go` files don't repeat — Go's `godoc` folding makes single placement sufficient.

**Tests required:** content pin for both files (models-package + separate graph-package variant); opt-out semantics across the three sub-flags.

**Depends on:** 18.2.

---

### 18.6 Pipeline wiring + stale cleanup

**PRD Reference:** §30.3 (stale cleanup), PRD §23.8 (stale-file contract).

**Module:** `cmd/sqlgen/gen/orchestrate.go`.

**What to build:**
- Hook manifest stage into orchestrator AFTER all other generation stages so it reflects the landed code shape. Order: parse → contexts → entity Go files → shared / unified-client / API files → build `*manifest.Document` → emit JSON → emit markdown → emit breadcrumbs → emit `manifest_embed_gen.go` (18.7) → stale-file cleanup → formatter pass.
- Extend stale-file cleanup to cover `manifest/*.md`, `manifest/entities/*.json`, `manifest_gen.json`, breadcrumb files, `manifest_embed_gen.go`, and the manifest pointer in `models_gen.go`'s doc comment when toggled off.
- Disabled-by-default regression test.

**Tests required:** extend `orchestrate_test.go` with disabled-by-default + opt-out cases; stale-cleanup test confirming removed-table → manifest entry gone (both layouts).

**Depends on:** 18.3, 18.4, 18.5, 18.7.

---

### 18.7 Runtime-embedded manifest + Go types package

**PRD Reference:** §30.6.

**Module:** New top-level package `manifest/`; generated file `manifest_embed_gen.go`.

**What to build:**
- New runtime package `manifest/` at repo root, stdlib-only. Exports ~27 named types per PRD §30.6: `Document`, `EntityIndex`, `Entity`, `Column`, `Relationship`, `Method`, `Filter`, `Sort`, `Features`, `PK`, `Conventions`, `Generator`, `Enum`, `Extra`, **`GenerationConfig`** (added 2026-05-15), plus typed aliases `Layout` / `Dialect` / `EntityKind` / `RelationshipKind`. Same types serve as JSON unmarshal target AND public Client API.
- `Document` gains a `GenerationConfig GenerationConfig \`json:"generation_config"\`` field (added 2026-05-15). `Method` gains a `SQLBodies map[Dialect]string \`json:"sql_bodies,omitempty"\`` field (added 2026-05-15). See `docs/design/MANIFEST.md` §5.9 for the full struct sketch.
- Sentinel errors `manifest.ErrEntityNotFound`, `manifest.ErrParseManifest`.
- `manifest.LoadInto(...)` helper used by the generated `init()` to handle both layouts uniformly.
- Generated `manifest_embed_gen.go` with `//go:embed` directives + the three Client methods + `init()` parse pass. Emitted only when `manifest.embed_in_client: true` (default).

**Tests required:** JSON round-trip for each named type (including `GenerationConfig` + `Method.SQLBodies`); method-signature pins; init-time parse success; layout-agnostic `ManifestEntity` lookup; drift check (`ManifestVersion()` agrees with `Document.SchemaVersion`); marshal round-trip; binary-size regression (<50KB total package growth on a 100-entity fixture); `Document.GenerationConfig` decodes correctly from a fixture manifest with every toggle in both true and false states.

**Depends on:** 18.2.

---

### 18.8 CLI subcommands (`sqlgen manifest validate` + `diff`)

**PRD Reference:** §30.8, §23.1 (CLI command index).

**Module:** `cmd/sqlgen/cli/cmd_manifest*.go`.

**What to build:**
- New `manifest` parent command with two subcommands.
- `sqlgen manifest validate <path>` — load file + validate against the schema referenced by `$schema` URL (fallback to `cmd/sqlgen/manifest/schema/v1.json` offline). Summary + exit codes 0 / 1 / 2.
- `sqlgen manifest diff <old> <new>` — structured schema-evolution diff over entities / columns / methods / sentinels / features. Default human-readable; `--json` machine-readable. Exit code 0 if no diff, 1 if any. Honors both layouts transparently.
- Promote `github.com/santhosh-tekuri/jsonschema/v5` from test-only to CLI dep.
- README / CLI-docs update.

**Tests required:** `cmd_manifest_validate_test.go` (valid + invalid + missing + malformed → exit codes); `cmd_manifest_diff_test.go` (column added / removed, entity added / removed, method signature change, sentinel change, feature toggle; `--json` schema test).

**Depends on:** 18.3.

---

### 18.9 E2E example surface across all dialects

**PRD Reference:** §30 (all subsections).

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/`.

**What to build:**
- Extend each example's `sqlgen.yml` with `generation.manifest:` block. At least one example uses `per_entity` layout (graphql) to exercise both code paths.
- Fixture data exercising extended metadata: `COMMENT ON TABLE` / `COMMENT ON COLUMN` (PG / MySQL native; SQLite `--`), CHECK constraints, composite indexes, partial indexes where supported.
- Regenerate `expected/manifest/` goldens + `expected/CLAUDE.md` + `expected/AGENTS.md` per example. The graphql example adds `expected/graph/CLAUDE.md` + `expected/graph/AGENTS.md` per the separate graph-package variant.
- Add `tests/manifest_test.go` per example covering file-on-disk shape, runtime `Client.Manifest()` round-trip, `ManifestEntity` across both layouts, `ManifestVersion` agreement, JSON Schema validation, `sqlgen manifest validate` CLI invocation against the emitted file.

**Tests required:** All four `tests/manifest_test.go` pass under `-race`. `TestE2EGoldenFiles` continues to pass.

**Depends on:** 18.1–18.8.

---

### 18.10 Phase closure sweep + PRD sync

**Module:** Documentation + repo-wide test sweep.

**What to build:**
- `make check` + `make check-examples` + `make test-integration` clean across the full repo under `-race`. Record any flakes / long-pole timings in the Completion Record.
- Confirm 18.1–18.9 Completion Records are filled in.
- Update `docs/tracker/STATUS.md` Phase 18 row to `10 | 10 | Complete` and update Current Focus with the surface-delta summary.
- Confirm `docs/design/MANIFEST.md` Status header reflects "SYNCED — superseded by PRD §30" with the closure date.
- Tag manifest JSON Schema as `v1.0.0`; freeze the v1 contract. Future changes go to `schema/v2.json` per PRD §30.5.
- `/fix` triage: any pre-existing bug uncovered during 18.x test bring-up gets a FIX entry, not in-place scope expansion. Exception logged: the GraphQL description-wiring fix bundled into 18.2 (same data path; MANIFEST.md §11 Resolved).

**Depends on:** 18.1–18.9.

---

## Phase 19: MCP Server

**Design supplement:** `docs/design/MCP.md` (SYNCED — normative spec now PRD §31, landed at 19.7 closure 2026-07-10). Full trackable breakdown: `docs/tracker/phase-19.md`. **Phase complete (closed 2026-07-10).**

**Goal:** An MCP server — `sqlgen mcp serve` — that consumes the Phase 18 manifest and exposes it to AI agents as **11 read-only tools + 4 resources + 3 prompts** over **stdio** (default, agent-auto-spawned) and **Streamable HTTP** (opt-in, loopback-bound, stateless), plus project-local `.mcp.json` emission so registration is a committed file. New package `cmd/sqlgen/mcp/`; new CLI subcommand; one new dependency (`github.com/modelcontextprotocol/go-sdk`) scoped to `cmd/sqlgen/` only. Runtime (`./`) and parser (`parser/`) stay untouched.

**Value scope (MCP.md §1.1):** earns its cost for large, same-repo packages behind a wrapper layer — the differentiator is the precision query interface, not discoverability. The cross-module wrapper-library case is a documented limitation, not a structural fix.

**⚠️ Freeze-gate caveat (user decision 2026-07-10):** MCP.md §11 names a frozen manifest **schema v1.0.0** as Phase 19's hard prerequisite. That freeze is **deferred**; the schema ships at `0.1.0`. This phase proceeds **against `0.1.0`**, treating the freeze as a parallel track. Consequence: the deferred freeze pair (shape `conventions` sub-blocks `call_options`/`soft_delete`/`comparator`/`omittable` + tighten `conventions.required` + bump to `1.0.0`) may reshape exactly the fields 19.3/19.4 read — re-verify those if the freeze lands mid-phase. Both hard-dependency addenda (`methods[].sql_bodies`, top-level `generation_config`) already landed in Phase 18, so `show_sql` and `sqlgen://config` are unblocked. 19.7 aligns the MCP server version to the actual `schema_version` at closure (not a silent `1.0.0`).

**Scope rule:** feature-additive, tightly bounded (same shape as Phase 15/17/18). A bug surfaced in pre-existing code gets a `/fix`, resolved outside the phase per the standing rule.

**Runner notes:** New package `cmd/sqlgen/mcp/` (SDK-backed; imports `cmd/sqlgen/manifest` types read-only, never the emitter). Config-side changes in `cmd/sqlgen/config/`. Pipeline hook for `.mcp.json` emission runs as the last manifest-stage step (after breadcrumbs) — mind the `cmd/sqlgen/manifest` ↔ `cmd/sqlgen/gen` import direction established in 18.6 (coordinate from the CLI layer). E2E extends all four example modules. `make check` + `make check-examples` + `make test-integration` must all pass before each sub-item is marked complete.

---

### 19.1 Server skeleton + transports + SDK adoption

**Reference:** MCP.md §2, §3.1, §5.1–§5.2, §6.1–§6.2, §6.4.

**What to build:**
- Add `github.com/modelcontextprotocol/go-sdk` to `cmd/sqlgen/go.mod` (pin to release current at implementation start); confirm it lands only in the CLI module.
- `cmd/sqlgen/mcp/server.go` — handshake / dispatch / shutdown via SDK primitives; advertise exactly tools + resources + prompts.
- `stdio.go` (default transport) + `http.go` (Streamable HTTP, stateless, `--http <port>` binds `127.0.0.1` only; non-loopback refused at flag-parse; notification push no-op).
- `sqlgen mcp serve` subcommand with §3.1 flags (`--manifest`, `--stdio`, `--http`, `--watch`/`--no-watch`, `--log`, `--log-level`); `slog` to stderr (stdout reserved for JSON-RPC); graceful `SIGINT` shutdown.

**Tests required:** SDK handshake replay; transport selection by flag; HTTP loopback-only enforcement; graceful shutdown.

**Depends on:** Phase 18 (manifest emitted at `0.1.0`).

---

### 19.2 Manifest loader + watch mode

**Reference:** MCP.md §3.1, §5.2, §6.3, §6.5.

**What to build:**
- `cmd/sqlgen/mcp/store.go` — RW-locked `Store`; `Load` reads → unmarshal → `ValidateAgainstSchema` → atomic swap. Default discovery walks up from CWD for `manifest_gen.json` (`-32001` if absent).
- `watch.go` — fsnotify wrapper; good reload emits `tools/list_changed` + `resources/list_changed` (stdio only). Failed reload retains the last good manifest and flips `health.ok=false`. Watch default-on; `--no-watch` opts out.

**Tests required:** validation + atomic swap under concurrent reads + schema-version mismatch; fsnotify dedup + rename-vs-rewrite + failed-reload-retains-good.

**Depends on:** 19.1.

---

### 19.3 Tools implementation (the 11) + fuzzy suggestions

**Reference:** MCP.md §4.1, §4.4, §5.3, §6.5.

**What to build:**
- `tools.go` registry/dispatch (all `sqlgen_*`, read-only/idempotent) + `tools_entity.go` / `tools_graph.go` / `tools_sql.go` / `tools_meta.go` / `tools_diag.go` per the §4.1 table.
- `suggest.go` — Levenshtein-≤2, case-insensitive, top-3, distance-then-lexicographic; feeds `-32003`/`-32004`/`-32006` `data.suggestions[]`.
- `compact` flag on `get_entity`/`describe_relationship`/`get_example`; `find_join_path` BFS (`max_hops` default 4 clamp `[1,6]`, ≤5 shortest-first, lexicographic ties, cycle-free); `show_sql` dialect resolution + `-32005` (absent `sql_bodies`) vs `-32006` (unknown method); `health` diagnostic; `validate_manifest` in-memory vs `path`.
- **Churn-watch:** `get_conventions`/`get_entity` read the deferred-freeze conventions sub-blocks — pin to `0.1.0`.

**Tests required:** per-tool fixtures (single-PK/composite-PK/tenanted/view/m2m/soft-deleted); `find_join_path` hop/cycle/clamp; `show_sql` dialect + absent-field + unknown-method; `suggest` threshold/case/top-3/tie-break/empty; health-state transitions; `TestDispatch_Deterministic`.

**Depends on:** 19.2.

---

### 19.4 Resources + Prompts implementation

**Reference:** MCP.md §4.2, §4.3, §4.4, §5.2.

**What to build:**
- `resources.go` — `sqlgen://manifest` (256 KB `warning`), `sqlgen://entity/<name>` (`-32003` + suggestions), `sqlgen://conventions`, `sqlgen://config` (surfaces landed `generation_config`; empty-object+warning fallback). `resources/list_changed` on watch reload.
- `prompts.go` — 3 templates (`sqlgen-write-query`, `sqlgen-add-relationship-usage`, `sqlgen-debug-not-found`) via `text/template`; stateless; no `prompts/list_changed`.
- **Churn-watch:** `sqlgen://conventions` reads the deferred-freeze sub-blocks — pin to `0.1.0`.

**Tests required:** per-URI shape + 256 KB warning + config present/absent fallback; prompt expansion (3 templates), missing-argument validation, unknown-prompt path.

**Depends on:** 19.2 (resources) + 19.1 (prompt registry).

---

### 19.5 `.mcp.json` emission + config block + agent-integration docs

**Reference:** MCP.md §3.2–§3.4, §6.6, §7.1.

**What to build:**
- `config.ManifestConfig.MCP` block (§3.4): `emit_project_config` (default `true` when `manifest.enabled`), `project_configs` (list, default `[.mcp.json]`), `server_key`, `command_template`; resolution table → global → default. Validation: reject `emit_project_config` without `manifest.enabled`; reject `..`-escaping `project_configs` entries.
- `mergeconfig.go` (§6.6) — per-`ConfigPath` upsert/remove; sentinel markers; unmanaged-collision → warn + free key; canonicalization; atomic write; `command_template` render. Pipeline loops `project_configs` as the last manifest-stage step; per-target failure reports but doesn't abort others (run exits non-zero if any failed).
- `docs/design/MCP.md` §3.2 per-client walkthrough + manual fallback + dual-target pattern + worked postgres example + troubleshooting. **Doc-honesty follow-through:** add a "Registration reach & limitations" subsection (cross-module `$GOMODCACHE` gap + binary/version-skew) so §1.1's limitation reference has a home; note the stdio server is agent-auto-spawned per session.

**Tests required:** `mergeconfig_test.go` over the full §3.4/§7.1 edge-case table incl. multi-target; config resolution + the two negative validation rules.

**Depends on:** 19.1 (server command referenced by emitted entry).

---

### 19.6 E2E surface across all dialects

**Reference:** MCP.md §7.2–§7.4. Parallels Phase 16's gqlgen-subprocess pattern.

**What to build:**
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/mcp_test.go` — boot against each example manifest; 11 tools / 4 resources / 3 prompts; dialect-aware `show_sql`.
- `integration/client_stdio_test.go` + `client_http_test.go` — full JSON-RPC lifecycle per transport + HTTP no-notification-push contract; watch-mode notification subtest (both transports).
- Gated `claude_code_smoke_test.go` (`CLAUDE_CODE_BIN`, skipped by default); server-survives-regen-mid-conversation cross-cutting test.

**Tests required:** four example legs; stdio + HTTP lifecycle; watch notification contract; gated smoke test.

**Depends on:** 19.3, 19.4, 19.5.

---

### 19.7 Phase closure sweep + PRD sync

**Reference:** MCP.md §8.

**What to build:**
- `make check` + `make check-examples` + `make test-integration` clean under `-race`.
- Land PRD §31 (§31.1–§31.5) per §8 (§31.1 uses the honest §1.1 framing); PRD §23.1 gains a `sqlgen mcp serve` row; PRD §30 cross-reference to §31.
- Update STATUS.md. Align MCP server version to the actual `schema_version` at closure (freeze-aware; not a silent `1.0.0`).

**Tests required:** full sweep green; doc-consistency check (PRD §31 tables match MCP.md §4 surface 11/4/3).

**Depends on:** 19.1–19.6.

---

## Phase 21: PostgreSQL Materialized Views

> **Status: Complete (closed 2026-07-13).**
> **Normative spec:** PRD §16.5 (landed at 21.6).

> **Rationale:** Postgres materialized views are invisible to today's introspection
> (`information_schema.views` excludes them; they live in `pg_matviews`), so they get no
> generated client at all. This phase discovers them and generates a read-only client
> identical to the regular-view surface (`Get`/`GetMany`/`Count`/`Paginate`/`Connection`,
> filter + sort by any column) **plus** the one verb a matview adds — `Refresh` /
> `RefreshConcurrently`. Reads query the matview **by name** (already optimal — scans the
> precomputed rows), so the read model reuses the entire existing view pipeline unchanged.

> **Scope rule:** PostgreSQL-only (MySQL/SQLite have no materialized views — annotation-file
> `CREATE MATERIALIZED VIEW` under those dialects is a hard error). Reuses `parser.View`
> (+ `Materialized` / `ConcurrentlyRefreshable` flags), `ViewConfig`, and
> `templates/view/*` — no new schema-object type, no runtime/parser-DDL churn for the
> other two dialects. No `CREATE MATERIALIZED VIEW` DDL emission, no index management, no
> auto-refresh scheduling, no incremental refresh.

> **Depends on:** Phase 16 (view surface, stable). **Postgres-only** feature.

**What to build (6 sub-items — see PRD §16.5):**
- **21.1** — Data model (`View.Materialized` / `.ConcurrentlyRefreshable`) + Postgres
  introspection (`pg_matviews` pass + unique-index discovery → deterministic PK).
- **21.2** — Annotation-file parser: `CREATE MATERIALIZED VIEW` (+ `@pk` ⇒ PK +
  concurrent-refreshable; non-PG / `OR REPLACE` / `input.paths` rules).
- **21.3** — `sql.BuildRefreshMaterializedView` + `hook.OpRefresh` +
  `ErrRefreshConcurrentlyInTx` guard + `templates/view/refresh.go.tmpl` (gated).
- **21.4** — Cache (refresh always invalidates own entries) + manifest/MCP surface
  (`materialized: true` marker + refresh-method advertisement).
- **21.5** — Testcontainers Postgres E2E across the full surface incl. both refresh
  paths + extend the `postgres` example module.
- **21.6** — PRD sync (proposed §16.5, §16.4 capability table, §4.9 notes) + `/close-phase 21`.

**Tests required:** parser unit (introspection + annotation + dialect rejection), codegen
golden (read surface == view golden + refresh methods; no-unique-index ⇒ `Refresh` only),
SQL-builder quoting, Postgres integration (full read surface + `Refresh` observes row
changes + `RefreshConcurrently` success with unique index and hard-error inside a tx),
`postgres` example regenerates + compiles.

---

## Phase 22: Column Access Control

**Normative spec:** PRD §32.

**Depends on:** Phase 16 (GraphQL surface), Phase 11 (events), Phase 18 (manifest).

A per-column `access` classification (`public` / `read_only` / `write_only` / `hidden` / `internal`) that constrains which **external** surfaces a value escapes through — the generated API (GraphQL/REST), event payloads, and the manifest — while the core Go client (entity struct, `Create*`/`Update*` inputs, filter, sort, cache) always retains every surviving column. A projection over external surfaces, not `exclude_columns` and not authorization. **Cache is deliberately out of scope** (round-trip-safety landmine — design Q1); **REST folds into the deferred REST feature (D.1)** when it lands.

**Sub-items** (`docs/tracker/phase-22.md`):
- **22.1** Config `access` field (`ColumnOverride`) + gen-layer resolution + the table-driven role→capability model + §32.4 validation.
- **22.2** Manifest per-column `access` + `redacted` (additive against schema `0.1.0`) + `manifest diff` reclassification reporting.
- **22.3** Event payload redaction — per-table `redact<T>*Input` clone for tables with `write_only`/`internal` columns; amends the §28.6 "same pointer" guarantee (byte-unchanged for tables without redacted columns).
- **22.4** GraphQL projection (schema type / input / filter / sort filtered by capability) + the **walker-completeness lint amendment** (`ValidateAPIWalkerCompleteness`: "every column must have a case" → "every **API-readable** column must, and restricted columns must not").
- **22.5** E2E example coverage across all in-scope surfaces + core-client/cache faithfulness.
- **22.6** Closure sweep + design-doc → SYNCED.

**Out of scope:** cache field redaction (design Q1), REST projection (D.1), SQL-comment `@sqlgen:access=` directive (design Q2), the raw per-axis capability object (design Q3).

---

## Phase 23: Cache × Tenancy — Nil-Tenant Invalidation

> **Status: Complete (closed 2026-07-15).**

**Normative spec:** PRD §27.9 / §28.11 / §29.4.4 / §29.5 / §29.6.

**Depends on:** Phase 12 (caching), Phase 13 (tenancy), Phase 11 (events). Self-contained within the existing cache + tenancy codegen — no new module.

Cache keys for tenanted tables embed the tenant (§29.5 `tenant`-before-`fingerprint` grammar), so a mutation run under `CallOptions.SkipTenancy` (which leaves `m.Tenant == nil`) cannot build the row's key: today the by-PK update/delete path **hard-errors** in an async post-commit callback (silent staleness), create silently skips, and `*Where` degrades to a table pattern — three inconsistent behaviors for one condition. The fix makes invalidation **derive the tenant structurally from the mutated row itself** (never the resolver, never a possibly-partial returned entity), precise on every dialect/shape. The same captured tenant is stamped into **event payloads**, and a **live latent defect** is fixed: cross-instance event-driven cache invalidation (§28.11) is currently broken for *every* tenanted table (D5). Finally, an optional typed explicit-tenant `CallOption` (A.1d) closes the expressiveness gap that made people misuse `SkipTenancy`.

**Sub-items** (`docs/tracker/phase-23.md` — dependency order recorded below):
- **23.1** — Structural tenant capture: parallel `m.AffectedTenants []any` index-aligned with `m.AffectedPKs` (D9); widen the existing affected-row capture to carry the tenant column (`RETURNING` PG/SQLite; MySQL `*Where` pre-`SELECT`; **new batched `SELECT id, <tenant> WHERE id IN (…)` pre-read for MySQL `*Many`**, D1/D10); codegen branch — tenant ∈ PK reads from the PK struct (no carrier), tenant ∉ PK reads from `m.AffectedTenants`. *Foundation — blocks 23.2–23.4.*
- **23.2** — B.0 cache-invalidation rewrite (G1–G3): rewrite `invalidateAffectedTenanted` + the create write-through to build the key from the captured tenant; **remove the hard error**; retain the `BuildTablePattern` fallback as a **defensive net** routed through `cache.RouteError` / `MetricsRecorder` / `OnErrorFunc` (should never fire). *Depends on 23.1.*
- **23.3** — Event tenant publish / E1: stamp `Event.Metadata["tenant"]` from the captured tenant across all shapes (not just `m.Tenant`) so `SkipTenancy` mutations emit tenant-stamped events. *Depends on 23.1.*
- **23.4** — Event-driven invalidation fix / E2 (fixes §28.11, D5): replace `InvalidationHandler`'s `(table, pks)` with an `InvalidationSignal{Table, Tenant, PKs, Schema}` struct (D6, breaking public extension point — acceptable pre-release); `FromEventSubscriber` reads `Metadata["tenant"]`; `handleInvalidation` routes tenanted tables to the **precise per-PK** `invalidateAffectedTenanted` (fingerprint is a local codegen constant, `%v` round-trips). *Depends on 23.1 (+ 23.3 for the stamp).*
- **23.5** — Axis A explicit-tenant option (D7, D2, D11): concretely-typed `Tenant *<package tenant type>` field on `CallOptions`, emitted only when tenancy is enabled, set via `new(v)`; explicit tenant **wins** over `SkipTenancy` (D2); disagreement with an input-carried tenant column → `tenancy.ErrMismatch` (D11). *Independent — may be last or deferred.*
- **23.6** — Example wiring + tests + PRD reconciliation: tenancy example gains tests **T1–T11 + T7b** (design §8); sync normative bits into PRD §27.9 / §28.11 / §29.4.4 / §29.5; flip PRD §27.9/§28.11/§29.4.4/§29.5/§29.6 → SYNCED; `/close-phase 23`.

**Governing decisions (D1–D11, design §10):** D1 MySQL pre-`SELECT` makes tenant capture ~free; D2 explicit tenant wins over `SkipTenancy`; D3 no strict/safe knob (always safe-degrade + signal); D4 keep `Backend` floor at prefix + trailing `*` (B.3 rejected; `GlobBackend` future-only); D5 §28.11 is broken for tenanted tables — fix here; D6 signal is an `InvalidationSignal` struct; D7 Axis A = concretely-typed `Tenant *T`; D8 read path needs **no change**; D9 structural capture via parallel `m.AffectedTenants`; D10 MySQL `*Many` batched pre-read → fallback is defensive-only; D11 `ErrMismatch` on explicit-vs-input-column disagreement.

**Dependency order:** 23.1 (foundation) before 23.2–23.4; 23.4 also wants 23.3's stamp; 23.5 independent; 23.6 last.

**Tests required (design §8):** T1 (`SkipTenancy` update evicts exact entry, tenant-∉-PK), T2 (`SkipTenancy` soft+hard delete), T3 (no callback error escapes a committed write, sync+async), T4 (`required:false` zero-tenant, no `SkipTenancy`), T5 (tenant-in-PK precise for all shapes), T6 (create write-through under `SkipTenancy` incl. partial-`FieldOptions`), T7 (precise `*Where`/`*Many` on all dialects incl. MySQL `*Many` pre-read), T7b (defensive fallback never fires normally, degrades safely if forced), T8 (public-only / non-tenanted byte-identical), T9 (event carries tenant under `SkipTenancy`), T10 (event-driven invalidation works for tenanted tables — guards a live defect), T11 (explicit tenant: precise key + `ErrMismatch` on disagreement).

**Out of scope:** the `tenant`-before-`fingerprint` grammar and the `Backend` contract are unchanged (N1/N2); no authorization layer (N3); no event-schema redesign beyond tenant carriage (N4); B.3 cross-tenant by-PK eviction and the `GlobBackend` capability interface (D4 — sketch in design §11, not built).

---

## Phase 24: Event Bus — First-Class NATS Transport

> **Status: Complete (closed 2026-07-31).** All 11 sub-items (24.1–24.11; EB Phases 0–3) landed; normative spec PRD §28.3/§28.5/§28.7/§28.7.1/§28.8/§29.6. `WithCodec` (EB-3.3) dropped (no proto/msgpack need surfaced); 24.8 durable knobs ride `*Bus.SubscribeWith` (variadic-on-`Subscribe` was not compilable). Full sweep clean under `-race` (check / check-examples / test-integration); no open FIXes.

**Normative spec:** PRD §28 (§28.3 wire format, §28.5 Handler-error semantics, §28.7/§28.7.1 transports incl. JetStream, §28.8 `MetadataFunc`), §29.6 (tenant metadata). The PRD §28 deltas (B1–B10) are **already blessed and written** — this phase implements them; it does not re-derive them.

**Depends on:** Phase 11 (event system: `event.Event`/`Publisher`/`Subscriber`/`Config`, memorybus, natsbus core), Phase 12 (caching, for the `FromEventSubscriber` invalidation path), Phase 23 (tenant `Metadata["tenant"]` stamp + `InvalidationSignal`). natsbus is its own Go module (`event/natsbus`, depends on `nats.go`); the shared `event` package and the generator are runtime/CLI.

The shipped `event/natsbus` runs over **core NATS**: fire-and-forget publish, JSON with Go field names, post-receive filtering, `_ = handler(...)` (errors discarded). It is a faithful at-most-once demo. This phase makes natsbus a production-grade transport — at-least-once delivery, redelivery/DLQ, replay/backfill, observable handler failures, and traces that survive the async hop — **without** disturbing the stdlib-only `event` contract that memorybus and every generated `event_hooks_gen.go` depend on. The load-bearing constraint (design §1.2): in the default `CallbackAsync` mode the deferred `Tx.OnCommit` publish runs on a fresh `context.Background()`, so request-scoped data (spans, actor, request-id) must be captured **at hook entry**, not read from the publish ctx — which is why consumer metadata and traceparent are a producer-side `MetadataFunc` concern, not a transport one.

**Sub-items** (`docs/tracker/phase-24.md` — tickets EB-0…EB-3; dependency order recorded below):
- **24.1** — Contract hygiene: `json` tags on `event.Event` (snake_case, EB-0.1); regenerate golden natsbus/serialized JSON. §28.5 Handler-error doc (EB-0.2) and the §28.7 PRD prep (EB-0.3) are **already blessed** — honored by later sub-items. *Foundation — wire hygiene benefits every transport; unblocks all.*
- **24.2** — `event.Config.MetadataFunc` (EB-1.7, **D6**): additive shared-pkg field + the single new generator injection point — merge `MetadataFunc(ctx)` at hook entry into each fanned-out `Event.Metadata` (per-event shallow clone; system `"tenant"` applied last/unoverridable). Transport-agnostic; memorybus/natsbus both exercise it; traceparent rides this path. *Highest blast radius (shared `event` pkg + generator). Depends on 24.1.*
- **24.3** — natsbus subscribe error routing + retry (EB-1.1/EB-1.2, **D2/D3**): `WithSubscribeErrorHandler` — stop `_ = handler(...)`, route errors; `WithRetry(n, backoff)` — bounded in-process retry before the error handler on core mode. *natsbus-local. Depends on 24.1.*
- **24.4** — natsbus header plumbing + envelope version (EB-1.3): publish via `PublishMsg` with NATS headers; introduce `Sqlgen-Envelope-Version`. *natsbus-local; foundation for 24.5 (traceparent) + 24.7 (`Nats-Msg-Id`). Depends on 24.1.*
- **24.5** — natsbus trace-context propagation (EB-1.5, **D4**): lift `Metadata["traceparent"]` → NATS header on publish; `WithContextPropagation(adapter)` rebuilds the ctx from the header on subscribe (continuing the producing span). Adapter is a natsbus-local Inject/Extract interface over `nats.Header` — no hard OTel dep; `event/` never imports a propagator. *natsbus-local. Depends on 24.4 (headers) + 24.2 (traceparent rides `MetadataFunc`).*
- **24.6** — memorybus optional local error handler (EB-1.6, **D5** parity): faithful at-most-once reference impl gains an optional error sink for D3 parity; **no durability emulation.** *Small, stdlib-only runtime pkg. Independent.*
- **24.7** — JetStream: `StreamConfig` + `WithJetStream` (EB-2.1, **D1**): publish to a persisted stream; `Nats-Msg-Id` = `Event.ID` for broker-side dedup. *natsbus-local headline. Depends on 24.4 + 24.1.*
- **24.8** — JetStream durable consumers + explicit ack (EB-2.2, **D2** takes effect): `nil → Ack`, `err → Nak(backoff)`. *Depends on 24.7.*
- **24.9** — JetStream `WithMaxDeliver` + `WithDeadLetter` → DLQ subject after N failures (EB-2.3). *Depends on 24.8.*
- **24.10** — JetStream replay/backfill: `WithDeliverAll` / start-time / start-sequence subscribe options (EB-2.4). *Depends on 24.7.*
- **24.11** — Convenience & niceties (EB-3.1/3.2/3.3): `natsbus.Connect(url, ...)` blessed prod defaults; `WithActionSubjects()` (bumps envelope version); `WithCodec()` (only if a real proto/msgpack need surfaces, else drop). PRD §28.7 already normative (EB-2.5 blessed). *natsbus-local; slot anytime.*

**Governing decisions (D1–D6, design §2):** D1 JetStream opt-in, `New(conn)` stays core/byte-identical; D2 Handler-error = redeliver where supported, else route to error handler (§28.5 doc, no signature change); D3 all JetStream/durability vocabulary natsbus-local — `event.SubscribeOptions` NOT extended; D4 traceparent is an ordinary `MetadataFunc` key, only natsbus does header lift/rebuild (no OTel in stdlib-only `event/`); D5 memorybus gains no durability emulation; D6 `event.Config.MetadataFunc` merged at hook entry, per-event clone, `"tenant"` reserved/applied-last.

**Dependency order:** 24.1 (foundation) before all; 24.2 independent of natsbus but gates 24.5's traceparent; 24.3/24.4 after 24.1; 24.5 after 24.4 + 24.2; 24.6 independent; 24.7 after 24.4; 24.8 → 24.9 chain; 24.10 after 24.7; 24.11 independent (slot anytime). Phases 0–1 (24.1–24.6) are shippable observability with **no durability**; Phase 2 (24.7–24.10) is the durability headline; Phase 3 (24.11) is optional sugar.

**Tests required (design §7 exits):** EB-0 — `make check` + `make check-examples` clean, memorybus unaffected. EB-1 — failing-first tests for core error routing, a two-service trace-continuity integration test, and a metadata-merge test asserting reserved `"tenant"` wins + per-event isolation across a multi-tenant batch. EB-2 — JetStream integration tests (testcontainers, `testing.Short()`-skippable): at-least-once redelivery after handler error, dedup window, DLQ routing after N deliveries, replay onto a fresh durable consumer.

**Out of scope:** client-side dedup for core NATS (`WithDedup(store)` — rejected, re-creates the per-adopter dedup bug; dedup is JetStream-only); JetStream vocabulary on `event.SubscribeOptions` (D3); memorybus durability emulation (D5); transport-side typed `PK`/`Input` decode (belongs in generated consumer helpers, not the transport); `CallbackSync` for trace propagation (blocks commit return).

---

## Phase 25: GraphQL Read Surface Completion

> **Status: Complete (closed 2026-09-16).** All 13 sub-items (25.0–25.12) landed; tickets A–D all closed. Normative spec PRD §11.1/§11.2/§11.5/§16.4/§26.4/§26.5.1/§26.5.3/§26.12/§29.2.5/§4.9/§4.13 + Appendix A. Full sweep clean under `-race` (check / check-examples / test-integration); no FIX entries remain open against the phase. **Two consumer-visible behavior changes to carry into the release notes:** a previously unscoped tenanted view read now filters (25.7 — the security fix), and views now appear in the generated GraphQL schema (25.8/25.9; opt out with `views.<n>.api.enabled: false`); 25.2's deletion of the `StringComparator` fallback also removes previously-advertised filter fields. Design doc `docs/design/archive/GRAPHQL_READ_SURFACE.md` (**SYNCED 2026-09-11** — findings F1–F9, decision record D1–D11, tickets A–D). Sourced from a consumer-side gap report written while building a real GraphQL read surface against generated output, then re-verified against sqlgen source. **25.0 (PRD sync, deltas B1–B7) landed 2026-08-25** per project rule 1 — B1–B5 / B7 blessed, B6 partial and B8 outstanding (Ticket-D only, behind the `EXISTS` correlation-qualifier decision). Ticket A (25.1) and Ticket B (25.6) are unblocked.

**Normative spec:** PRD §26.4 / §26.5.3 (comparator projection), §16.4 + §26.4 (view GraphQL surface), §29 (tenancy — must be extended to views), §11.1 (relationship filter fields). **Design doc:** `docs/design/archive/GRAPHQL_READ_SURFACE.md`. Unlike Phase 24, the PRD deltas here were **not blessed up front** — 25.0 wrote them (§5 of the design doc, B1–B7) and every later sub-item cites them. Where the design doc and the PRD now disagree (D4 / §3.1 on nullable comparator inputs), the **PRD wins** — the design doc carries superseded markers.

**Depends on:** Phase 13 (tenancy: `TenantResolver`, detection/validation pass, `CallOptions.SkipTenancy`/`Tenant`), Phase 16 (GraphQL API generation: schema/translator/walker/resolver emitters, gqlgen wrapper merge), Phase 21 (materialized views — D7 keeps `Refresh` unscoped), Phase 22 (column access — the §32.2 capability gates every projection decision below).

Two of the four gaps are **unimplemented spec**, not new features: PRD §26.4 already declares `View → Object type (read-only — only Query fields generated)` and `docs/design/archive/GRAPHQL.md` §952 records "**Resolved:** views are treated identically to tables, gated to read-only". The other two need PRD amendments first.

The load-bearing structural cause (design §1.3): the GraphQL filter and sort surfaces are each emitted by **two independent functions deriving the same fact from different inputs** — the schema from the GraphQL type (`funcComparatorFor`, defaulting to `StringComparator`), the translator from the model's Go comparator type expression (`comparatorTranslatorVariant`, `continue`-ing on Enum/JSON/JSONB/Slice); and the sort enum from `screamingSnakeCase` vs a bare `strings.ToUpper(toSnakeCase(...))`. Nothing forces agreement, and when they disagree the field is **accepted and ignored** — silent wrong results, the worst failure mode. Collapsing this to one derivation is the durable half of the phase and the reason F5 is in scope alongside F2.

**Sub-items** (`docs/tracker/phase-25.md` — mapped from `docs/design/archive/GRAPHQL_READ_SURFACE.md` §6 tickets A–D; dependency order recorded below):
- **25.0** — **PRD sync (B1–B7).** No code. §29 extended to views (detection, read-path-only, per-view override, D6 nullable rule, uniform-type participation, D7 matview `Refresh` unscoped); §26.4 gains `<Enum>Comparator` / `JSONComparator` / `JSONBComparator` / `<Elem>SliceComparator` / `DecimalComparator` + the monomorphization rule (D3) + PostgreSQL-only gating; §26.4 comparator inputs completed (F6) with the `isNull`-iff-nullable rule; §26.5.3 corrected (the cited `translateNumericComparatorDecimal` does not exist — F7) and given the D1 single-field-map invariant + D2 lint; §16.4/§26.4 specify the exact view read surface; §11.1 relationship filter fields (D10); §4.9 documents `views.<n>.tenancy` / `views.<n>.api`. *Blocks all.*
- **25.1** — **One field map (D1).** Compute each column's full filter projection and sort enum value **once** onto `APIFieldContext`; repoint `schema.graphqls.tmpl`, `filter_translate.go.tmpl`, `sort_translate.go.tmpl` and `buildAPISortFields` at it; delete `funcComparatorFor` and the duplicate sort spelling; drive `shared.graphqls.tmpl` from `ComparatorFamilies` instead of hard-coded blocks. **Closes F5 on its own.** *Pure refactor — must regenerate byte-identically on every example except the F5 digit-leading case, which is the proof it worked. Depends on 25.0.*
- **25.2** — **Completeness lint (D2).** `ValidateAPIFilterCompleteness` + `ValidateAPISortCompleteness`, modelled on the existing `ValidateAPIWalkerCompleteness` and wired into the same codegen hard-error path. *Failing-first: a hand-built context with a schema field and no translator entry must error. Depends on 25.1.*
- **25.3** — **Complete the five shipped families (F6/F7).** Missing operators (`String`: gt/gte/lt/lte/nlike; `Numeric`: nin/nbetween; `Time`: in/nin/nbetween; `ID`: gt/gte/lt/lte), `isNull` on nullable columns, and a dedicated `DecimalComparator` (operands typed `Decimal`, translating into `comparator.String`). *Golden churn expected and intended. Depends on 25.1.*
- **25.4** — **Enum comparators (D3).** `<EnumGraphQLName>Comparator` per used enum (`eq neq in nin` + `isNull` when nullable), reusing the existing gqlgen enum binding so translation is a pointer/slice copy with no conversion. Enum-array columns route to 25.5's slice form. *Depends on 25.1.*
- **25.5** — **JSON / JSONB / Slice comparators (D3).** `JSONComparator` (all dialects); `JSONBComparator` + `<Elem>SliceComparator` (postgres only, matching `resolveSimpleComparator` and PRD §11.2). The FIX-103 `json[]`/`jsonb[]` non-filterable case must keep being skipped on **both** sides — 25.2's lint is the guard. `comparator.Custom` is never projected. *Depends on 25.1.*
- **25.6** — **View tenancy: detection + attachment (D5/D6).** `resolveViewTenancy` + a view loop in `BuildTenancyContext`; views participate in the §29.2.4 uniform-type check; `attachTenancyToViews`; nullable tenant column → warn + scope (hard error only under explicit `enabled: true`); `views.<n>.tenancy` config + validation; `applyClientTenancy` extended over views so the unified client threads the resolver in. *No template changes yet — assert the resolved context in unit tests. Depends on 25.0.*
- **25.7** — **View tenancy: read-path emission (F1 — the security fix).** Tenant predicate into `view/get.go.tmpl` (Get + GetMany), `count.go.tmpl`, and `pagination.go.tmpl` (Paginate + Connection); `tenantResolver` field + `resolveTenant` ported into `view/client.go.tmpl`. No `CallOptions` change needed (`SkipTenancy`/`Tenant` are already package-level); no cache work (F9 — views have no cache read path). *E2E: a tenanted view returns only the resolver's tenant across all five read methods; `SkipTenancy` and explicit `Tenant` behave as on tables; `tenancy.required: true` + absent resolver → `ErrMissing`. Depends on 25.6.*
- **25.8** — **Views into `APIContext` (D8).** `IsView` on `APITableContext`, operations mask narrowed to the read set, `views.<n>.api` config + mutation-key config error, `ValidateAPIWalkerCompleteness` generalized off `TableContext`, `generateAPI` signature takes views. *Depends on 25.7 (D9) and 25.1.*
- **25.9** — **View GraphQL emission + binding (F3).** Per-view schema file, field-options walker (columns-only — views have no relationships, so a view read is exactly one query per §25.1), filter/sort translators, envelope aliases, resolver seeds, and the gqlgen `models:` merge (which picks views up for free once they are in `apiCtx.Tables`, retiring the hand-written consumer binding). *E2E: query a tenanted view over HTTP as two tenants and assert isolation — the exact hole this phase closes — plus a query-count assertion. Depends on 25.8.*
- **25.10** — **`sql` correlated-`EXISTS` condition (F8, D11).** A structured `Exists{CorrelationColumn, Subquery}` value + `sql.Exists(col, subquery)` constructor: codegen emits the correlation **column only**, and the builder resolves the qualifier at render time (`SelectOptions.Alias` when aliased, `FormatTable` otherwise), so one emitted value is correct on both the plain and O2O-join read paths. Exempt from alias prefixing by construction — it is not a leaf clause. Normative in PRD §11.5 + Appendix A (B8). *Unit tests: an `EXISTS` clause survives prefixing and placeholder numbering stays stable across the O2O-join path. Depends on 25.0.*
- **25.11** — **Model-side relationship filter fields (D10).** One `<T>Filter` member per list relationship, compiled to a correlated `EXISTS` from the join metadata already on `RelationshipContext` (`FKColumn` for O2M/O2O; `JunctionTable` + both junction FKs for M2M); the target's own soft-delete **and tenant** predicates injected into the subquery on the same rules its own read path uses; recursion through `and`/`or`. One hop per level, arbitrary depth by nesting; no aggregate predicates. *Cross-dialect E2E — the subquery shape differs in quoting, not structure. Depends on 25.10.*
- **25.12** — **Relationship filter GraphQL projection (F4).** Relationship fields on the `<T>Filter` input + translator recursion (an ordinary nested gqlgen input — no new machinery). *E2E: `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` — the consumer's actual blocked story. Depends on 25.11 and 25.1.*

**Governing decisions (D1–D11, design §2):** D1 one field map, two emitters (schema + translator read the same `APIFieldContext` fields); D2 codegen completeness lint makes a future regression unlandable; D3 monomorphize the generic families per concrete type (GraphQL has no generics — keeps schema-level validation instead of runtime parse errors); D4 complete the five shipped families in the same pass (pre-release, so one golden churn not two); D5 a view carrying the tenant column is tenanted exactly like a table, read path only, fail-closed, auto-detected with a per-view opt-out; D6 nullable tenant column on a view → warn + scope, not the hard error tables get (legitimate `LEFT JOIN`/aggregate artifact, and scoping is fail-closed either way); D7 matview `Refresh`/`RefreshConcurrently` stay unscoped and Go-client-only; D8 views become first-class `APIContext.Tables` entries with `IsView: true` rather than a parallel context type; D9 the view GraphQL surface ships strictly **after** view tenancy (shipping it first would ship the vulnerability, on by default); D10 relationship filtering lands as junction/FK-backed filter fields compiling to `EXISTS`, with filter *arguments* on relationship fields deferred; **D11** the `EXISTS` correlation qualifier is resolved by the builder from a structured `sql.Exists`, never baked in at codegen (the qualifier is path-dependent, and a structured value is prefix-exempt by construction).

**Dependency order:** 25.0 before all. Ticket A (25.1 → {25.2, 25.3, 25.4, 25.5}) and Ticket B (25.6 → 25.7) run in parallel. Ticket C (25.8 → 25.9) needs both 25.7 (D9) and 25.1. Ticket D (25.10 → 25.11 → 25.12) is independent except 25.12 wanting 25.1's field map. **Shortest path to closing the cross-tenant read hole is 25.0(B1) → 25.6 → 25.7** — Ticket B is valuable standalone, before any of the API work.

**Tests required (design §6 exits):** 25.1 — golden regeneration byte-identical across all 13 examples except the F5 case (the refactor's proof). 25.2 — failing-first lint tests in both directions (schema field without translator entry; translator entry without schema field). 25.3–25.5 — per-family translator unit tests plus an E2E asserting `status`/`priority`/`customFields` filters actually narrow the result set (the F2 regression pin). 25.7 — cross-dialect E2E tenanted-view isolation across Get/GetMany/Count/Paginate/Connection, `SkipTenancy` + explicit `Tenant`, and `required: true` → `ErrMissing`. 25.9 — two-tenant view query over the real gqlgen server, plus a counting-Querier one-query assertion. 25.10 — `PrefixConditions` placeholder-stability unit tests. 25.11/25.12 — cross-dialect `EXISTS` E2E including composition with a column filter and with `and`/`or`.

**Out of scope:** `StringComparator` + runtime enum parse (rejected — loses schema-level validation); filter/sort **arguments** on relationship fields (deferred, not rejected — nested field args are unreachable through gqlgen's public API from inside the field-options walker, and the per-relationship-resolver alternative forfeits the §25.1 one-query guarantee); aggregate relationship predicates (`assignees: {count: {gt: 3}}` — needs `GROUP BY`/`HAVING`); `comparator.Custom` projection (raw-SQL escape hatch, an injection surface over HTTP); tenant-scoping matview `Refresh` (D7); view mutations of any kind (§16.4). See `docs/design/archive/GRAPHQL_READ_SURFACE.md` §7.

**Consumer-visible behavior changes** (flag at phase closure): a previously-unscoped tenanted view read **starts filtering** (the fix), and views **start appearing** in the generated GraphQL schema (opt out with `views.<n>.api.enabled: false`).

---

## Phase 26: Standard Library UUID as First-Class

> **Status: Complete (closed 2026-09-15).** All 7 sub-items (26.0–26.5 plus 26.3a) landed; 26.4 and 26.5 shipped together as the breaking pair. Normative spec PRD §7.2/§7.3/§7.4/§4.13/§11.2/§26.4/§26.4.1/§28.8. Full sweep clean under `-race` (check / check-examples / test-integration); no FIX entries logged against the phase — the three live defects it surfaced (`scalars.go.tmpl`'s hardcoded `uuid.Parse`, the nameless parse call rendered from a hand-built `APIContext`, and the TEXT-column retype that will not scan under pgx) were each fixed inline with the PRD amended first. Migration plan `docs/design/archive/UUID_MIGRATION.md` (**SYNCED 2026-09-15** — findings measured against `go1.27.1`, `pgx v5.9.1`, `modernc.org/sqlite v1.48.1`, and PostgreSQL 16; method recorded in §11 of that doc). Go 1.27 ships a `uuid` package in the standard library; this phase makes it the binding a `uuid` column resolves to with no configuration, and demotes `github.com/google/uuid` / `github.com/gofrs/uuid/v5` to supported overrides.

**Normative spec:** PRD §7.2 (built-in type mappings), §7.3 (nullable handling), §7.4 (built-in type integrations), §26.4.1 (scalar marshaling), §4.13 (config validation rules). **Migration plan:** `docs/design/archive/UUID_MIGRATION.md`. As in Phase 25, the PRD deltas are **not blessed up front** — 26.0 writes them and every later sub-item cites them. The plan document's own step ordering puts documentation last; the tracker inverts that per project rule 1, so 26.0 leads.

**Depends on:** Go 1.27 toolchain floor (`guidelines/GO.md` §12, PRD §1) — already the stated target. No phase dependency: the type-resolution machinery this phase touches (`cmd/sqlgen/gotype`) has been stable since Phase 5.

The premise that motivates the phase is already settled and needs no investigation: the stdlib type implements neither `driver.Valuer` nor `sql.Scanner`, and **this does not matter**. `database/sql` special-cases `uuid.UUID` in both directions (`driver/types.go` on write, `convert.go` on read), and pgx routes it through the same `[16]byte` → `pgtype.byte16Wrapper` → `UUIDCodec` path `github.com/google/uuid` has always taken — that type is also `[16]byte` and also implements no pgx interface. Verified on both `output.driver` values across all five `QueryExecMode` settings, `uuid[]` arrays, `SendBatch`, `CopyFrom`, `RETURNING`, and `CollectRows` (plan §3).

The load-bearing structural fact (plan §7): **two UUID libraries cannot coexist in one generated package**, under either `output.layout`. Imports are resolved per package, not per file (`resolvePackageImports` deliberately resolves once over the concatenated bodies and seeds every file with the union), so `file_per_table` fails identically to `single_file`. Two defects surface together — a name collision (`uuid redeclared in this block`) and, worse, a **silent mis-resolution**: the surviving `uuid` binds to whichever import wins, so a google-backed table's `uuid.NullUUID` resolves against the standard library and fails with `undefined: uuid.NullUUID`. The existing `stdsql "database/sql"` aliasing does not generalize, because both packages are named `uuid`, both export `UUID`, and the consumer writes `type: uuid.UUID` for either — the qualifier carries no information about which is meant. This is why 26.2 exists and why it must land before 26.4.

**Sub-items** (`docs/tracker/phase-26.md` — mapped from `docs/design/archive/UUID_MIGRATION.md` §8–§10; dependency order recorded below):
- **26.0** — **PRD sync.** No code. §7.2 `uuid` row default Go type `string` → `uuid.UUID`; §7.4 promotes the standard library to the first integration table and demotes google/gofrs to alternatives, recording that the stdlib has **no** null wrapper; §7.3 notes the `nullPtrOnly` classification for `uuid.UUID`; §26.4.1 gates the `NullUUID` scalar to the wrapper-backed integrations; §4.13 gains the 26.2 validation row. *Blocks all.*
- **26.1** — **`uuidstd` integration package.** `cmd/sqlgen/gotype/uuidstd/`, parallel to `uuidgoogle/` and `uuidgofrs/`: `ImportPath = "uuid"`, `SQLTypes() = ["uuid"]`, `Override()` with `Type: "uuid.UUID"`, `ZeroValue: "uuid.UUID{}"`, and **no** `Nullable` (the package has no wrapper, so `goTypeFromOverride`'s value-typed branch yields `*uuid.UUID`). Registered in `knownIntegrations`. No default change. *Additive — opting in via config is the only way to reach it. Depends on 26.0.*
- **26.2** — **Reject mixed UUID libraries in one package.** A resolution-time (phase 3) validation error naming both import paths, the tables that selected each, and directing the user to split them across `output.dir` packages. *Failing-first: a config with a global stdlib override and a table-scoped google override must error, where today it emits a package that does not compile. Depends on 26.0.*
- **26.3** — **Fix the `detectIntegrations` global-vs-table asymmetry.** `detectIntegrations()` walks only `r.globalOverrides`, so a **table-scoped** `overrides.types.uuid` is never enriched: dropping its `nullable:` silently yields `*uuid.UUID`, while the same edit to a **global** override is silently refilled with `uuid.NullUUID` by `enrichOverride`. Same config text, two results depending on nesting depth. *Pre-existing bug, independent of this migration but adjacent enough to fix in the same pass. Depends on 26.0.*
- **26.3a** — **Per-integration UUID generation expressions.** The two sites that emit a UUID-generating call — the app-strategy primary key (`funcmap.go::funcPKAutoGenType`) and the generated event ID (`context_event.go`) — both hardcoded `github.com/google/uuid`. The event-hooks file appended google's **import** unconditionally, so any package binding another library got two packages named `uuid`, from a file 26.2's rule cannot read; and google's spellings (`uuid.New()`, `uuid.NewString()`, `uuid.Must(...)`) exist in neither of the other two libraries, so an app-strategy PK could not compile under gofrs or the standard library. Both now resolve from the package's selected integration per PRD §7.4 "Generating UUID values", falling back to the standard library when no `uuid` column selects one. *Resolves blockers B-1 and B-2 from the 26.0 decision record. Must land before 26.4 and 26.5 — 26.5 migrates three events-enabled examples to the standard library, which without this would emit two UUID imports in one package. Depends on 26.0.*
- **26.4** — **Flip the default binding.** New `goUUID` `typeInfo` (`name: "uuid.UUID"`, `imp: "uuid"`, `zero: "uuid.UUID{}"`, `kind: nullPtrOnly`, `fk: FKStringStringer`) and `"uuid": goString` → `"uuid": goUUID` in `postgresMappings`. Only PostgreSQL declares a `uuid` SQL type, so this is a one-line mapping change; MySQL and SQLite are unaffected. **Breaking** — retypes `uuid` columns for any consumer who never configured UUID. *Depends on 26.1 and 26.2 — shipping the flip before the validation would break a project with one table still pinned to `google/uuid` with a confusing diagnostic instead of a config error.*
- **26.5** — **Migrate examples.** Five to the standard library (`postgres`, `graphql`, `graphql_top_level`, `tenancy`, `tenancy_postgres`); **`postgres_stdlib` held on `github.com/google/uuid`** as the wrapper-path regression guard (its `output.driver: stdlib` keeps the `database/sql` path covered with a wrapper type); **`graphql_null_wrappers` moved to `github.com/gofrs/uuid/v5`**, closing a gap where **no example exercised `uuidgofrs` at all**. Each example needs a `sqlgen.yml` edit *and* a `go.mod` edit, and any table-scoped `overrides` or `column_map.<col>.import` naming a UUID library must move with the global one or trip 26.2. *Depends on 26.4.*

**Governing decisions (plan §7.4, §10):** **one UUID library per generated package**, enforced rather than documented (26.2); two separate generated packages in one module remain a supported arrangement for a project that genuinely needs both, and compose with the existing multi-package monorepo pattern. **Two** non-stdlib example guards, not one — one would leave `uuidgofrs` untested, and once the stdlib is the default nothing else would ever pull gofrs into a build. The two wrapper integrations are **not** redundant: `uuidgoogle` spells the zero UUID `uuid.UUID{}`, `uuidgofrs` spells it as the package **variable** `uuid.Nil`, and the standard library spells it as neither (its `Nil` is a *function*), which is precisely why `ZeroValue` is a per-integration field and why gofrs coverage keeps that field honest.

**Dependency order:** 26.0 before all. 26.1, 26.2 and 26.3 are independent of each other and run in parallel. 26.3a needs 26.0 and lands after 26.3. 26.4 needs 26.1 + 26.2 + 26.3a. 26.5 needs 26.4. **26.4 and 26.5 are the breaking release and must ship together** with the GraphQL schema change below; 26.0–26.3a are additive and can land independently.

**Tests required (plan §9 gates):** 26.1 — resolver unit tests; opting in via config produces `uuid.UUID` / `*uuid.UUID` with the correct zero value and FK extraction. 26.2 — failing-first config test on a two-library config. 26.3 — global and table-scoped overrides with identical text resolve identically. 26.3a — failing-first on a stdlib-bound events-enabled config emitting two UUID imports; a codegen matrix of integration × version × PK shape against the PRD §7.4 table; the emitted spellings compiled against all three real libraries. 26.4 — full golden regeneration; every example compiles; E2E suite green. 26.5 — `TestE2EGoldenFiles` byte-clean, integration suites green under Docker, and **a build of the example tree still pulls both wrapper libraries**, proving neither integration went dark.

**Out of scope:** aliasing support for two UUID libraries in one package (plan §7.2 — needs the import path carried per column through emission; 26.2 converts the case to a config error instead); any change to `pgtype.UUID` interop (sqlgen scans into the resolved type, never into `pgtype.UUID` — see `guidelines/GO.md` §12 and jackc/pgx#2636); a `NullUUID` equivalent for the standard library.

**Consumer-visible behavior changes** (flag at phase closure): **(1)** `uuid` columns with no override retype from `string` to `uuid.UUID` (26.4). **(2)** The GraphQL `NullUUID` scalar **disappears** for stdlib-bound packages and nullable UUID fields become `UUID`; the generated `MarshalNullUUID` / `UnmarshalNullUUID` pair stops being emitted. Filter inputs are unchanged (`NullableIDComparator` either way). **(3)** Nullable UUID columns lose `.Valid` / `.UUID` in favor of `!= nil` / `*v` — mechanical, and the compiler catches every site. **(4)** The **Go module floor is `go 1.27.0`** (26.3a): a generated package that binds the standard library `uuid` needs that directive in the consumer's `go.mod` — `go build` accepts it below 1.27, but `go vet` and `go test` do not. Every sqlgen module and example moved to `go 1.27.0` in 26.3a. **JSON and cache payloads are byte-identical** across the migration (plan §6.1), so no cache flush is required and the change is reversible; the published GraphQL schema is the only one-way surface.

---

## Phase 27: Nested Mutations — Create / Update / Upsert With Relationships

> **Status: Complete (closed 2026-10-02).** All 14 sub-items (27.0–27.11 plus 27.0a and 27.9a) landed. Normative spec PRD §9.9, with §4.6/§4.8/§4.13/§9.2/§9.5/§13.1/§13.4.1/§22.1/§22.3/§25.1/§26.5.1/§26.5.5/§26.12/§27.7/§28/§29.4/§29.10/§32.5. Full sweep clean under `-race` (check / check-examples / test-integration). The 27.11 reconciliation of §9.9 against the generator fixed a silently dropped listed edge (E1/E3) and the nested error-wrap name inside the sub-item; FIX-224 is carried into Phase 29.1. Design doc `docs/design/archive/NESTED_MUTATIONS.md` (SYNCED 2026-10-02 — superseded by PRD §9.9; PROPOSED 2026-09-10, re-verified against HEAD 2026-09-16) with companion `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md`. Sourced from a consumer request — *"create the entity alongside their relationships in one function, so transaction boundaries close inside the API instead of at the database client"*. Adds `CreateWithRelated` / `UpdateWithRelated` / `UpsertWithRelated` to the generated Go client and the GraphQL mutation surface, carrying a per-edge block of four verbs (`create`, `connect`, `disconnect`, `clear`) at depth 1.

**Normative spec:** none yet — **27.0 writes it**, as 25.0 and 26.0 did. The design doc's §8 enumerates 29 PRD deltas (**P1–P29**) against §9.2, §9.5, §9.9 (new), §13.1, §13.4, §13.7, §4.6, §4.8, §4.13, §22, §25.1, §26.5.1, §26.5.5, §26.12, §27.7, §28, §29.4, §29.10 and §32.5. Per project rule 1 no code lands before that sync.

**Depends on:** nothing structural — Phases 0–26 are all Complete. **Two FIXes landed first**, both true at HEAD independently of this feature and both benefiting every existing caller: **FIX-207** (the §32.3 event-redaction `default` arm published the raw input, reachable through the three `*Where` delete ops whose `Input` is the caller's filter) and **FIX-208** (the non-tenanted `OpUpdateWhere` cache arm wiped the table's whole namespace, which O2M `connect`/`disconnect` both compile to — since resolved by moving every `*Where` op onto key-based eviction). Both are resolved, so neither gates 27.6 or 27.9 any longer.

**What the feature actually buys**, stated precisely because there is **no atomicity gap** to close — every generated mutation already opens with `database.Conn(ctx, c.querier)`, so any composition inside `WithTx` is already atomic with correct savepoints, deferred side effects and tenancy (design §1.2). It buys three things: a **GraphQL surface at all** (generated mutations are one-table-one-call, so a nested write today means a hand-written resolver forfeiting the field-options walker, error mapping and operations mask); **correct-by-construction unlink scoping** (the hand-written version has no parent guard, and the consumer who forgets `WHERE fk = <parent>` detaches another parent's rows); and an **idempotent `add`** (the obvious spelling — `CreateMany` on the junction — errors on the second call, measured in design §2.4 probe A).

**The load-bearing structural fact** (design §1.3, §1.4): relationship *reads* are uniform, but writes have **four shapes**, because the FK's owning side decides insert order — and a second, orthogonal axis decides the verb set, because **what an unlink can mean is fixed by FK nullability, not by relationship type**. `User.Events` and `User.Orders` are both O2M on the same parent; the first admits all four verbs and the second admits only `create`, because `orders.user_id` is NOT NULL and detaching would violate it. The verb set is therefore computed per edge from `RelationshipContext.FKNullable`, never from `.Type`.

**Sub-items** (`docs/tracker/phase-27.md` — mapped from `docs/design/archive/NESTED_MUTATIONS.md` §9 tickets A–H; dependency order recorded below):
- **27.0** — **PRD sync (P1–P29).** No code. Writes §9.9 (new, the normative home for the four write shapes, the eligibility matrix and the verb algebra), the `nested_mutations` config blocks, the `…_with_related` operations-mask entries and their validation rules, two new error sentinels, the `OpUpsertMany` hook op, the query-count extension to §25.1, and the observability contract (**P29**). **Carries the two settled open questions**: **Q9** (batch at `batchSize` — measured, design §2.6) into §25.1, **Q10** (declared order = alphabetical by relationship name) into §9.9. *Blocks all.*
- **27.0a** — **Config surface + JSON schema, every key in one landing.** Added 2026-09-16 after the 27.0 review found `cmd/sqlgen/config/schema/v1.json` named by **no** sub-item while `TestSchemaV1_noDriftFromConfigStructs` (`cmd/sqlgen/config/schema_test.go:309-395`) is reflection-driven and path-aware, so it fails on any Go config field with no schema property at its path. The keys were split across 27.2, 27.5 and 27.7, which pays that tax three times for one reason — and the two failure modes disagree: the `nested_mutations` blocks and `relationships[].discriminator` hard-error under `KnownFields(true)`, while `operations.*` keys are **silently accepted and dropped**, since `Operations` defines its own `UnmarshalYAML` and `yaml.Node.Decode` builds a fresh non-strict decoder the setting cannot reach (`config.go:1441-1444`). Lands: the four `Operations` toggles across all **five** `ExpandPreset` arms (five, not six — `all`/`""` is one arm, plus an erroring `default`), `applyOperationOverrides`, the `apiOperationFields` / `clientOnlyOperationFields` split (`upsert_many` is client-only, the three `…_with_related` are maskable), both `nested_mutations` blocks, `RelationshipDiscriminator`, the **`relationshipDedupKey` discriminator component** 27.2's fixture migration needs, the five config-answerable §4.13 validation rules, and the four `schema/v1.json` `$defs` edits. *Acceptance: **zero golden movement** — verified structurally: every consumer of `config.Operations` is a hand-written field list, the manifest emits no `operations` key and its `Relationship` struct has no `discriminator` slot, MCP reads none of it, and `gen.ResolvedOperations` is a separate struct this sub-item deliberately does not extend. Blocks 27.2, 27.5, 27.7, 27.8.*
- **27.1** — **`FKOnTarget` hoist onto `RelationshipContext`** (Ticket **A**, **NW-D1**). The fact is computed inline and thrown away inside `buildO2OJoinDetails` (`context_table.go:2328-2337`); `Side` does not answer the same question, because `addO2O` and `addO2M` both hard-code `SideParent` (`parser/relationship.go:182`, `:197`) while a config edge can put the FK on the target. Rewrite the read path to consume the hoisted field. *Acceptance: **zero golden movement**. Independent.*
- **27.2** — **`discriminator:` config plus its write-side rules** (Ticket **B**, **NW-D6**, **D18**). A structured, invertible `{column, value}` alternative to `filter:`, mutually exclusive with it. Write side: a nested `create` **sets** the column, the child input **elides** it (**C2**-style), and a `connect`'s visibility read **matches on it**. *Without all three the edge accepts writes its own loader can never read back. Independent.*
- **27.3** — **Single-row `Update` skip-refetch escape** (Ticket **C**, **D10**, **F1**). `Update` is the only mutation returning `c.Get(...)` unconditionally (`update.go.tmpl:81`, `:203`); five sibling templates guard it with `FieldOptions != nil && !HasSelectedColumns()`. Three lines. *Valuable alone — today `Update` with an empty `FieldOptions` reads a row nobody asked for. Independent.*
- **27.4** — **`sql/` batched conflict clause** (Ticket **Da**). Grow `MultiInsertOptions` the three fields `InsertOptions` already carries and call `d.UpsertClause(...)` from `BuildMultiInsert`. ~10 lines, no new imports. *A **runtime-module** edit — the design's blast radius read "None" until the companion caught that `BuildInsert` is single-row. Nothing emits it yet, so zero golden movement.*
- **27.5** — **`UpsertMany`** (Ticket **Db**, **D5**). Template, `operations.upsert_many`, all three dialects, `Upsert`'s signature. **Four decisions the ticket must make, each set by a measurement** (companion §6): dedupe inputs by conflict target, last-wins, because PostgreSQL alone rejects an in-statement duplicate on the `DO UPDATE` shape; the `[]*T` return contract on the `DO NOTHING` branch, where `RETURNING` yields only inserted rows on two of three dialects; the MySQL db-generated-PK contract, where `LAST_INSERT_ID()` after a mixed batch names the first *newly inserted* row so `firstID + i` is unsound; and `AffectedPKs` sourced from the **inputs**, never from `RETURNING`. *Depends on 27.4.*
- **27.6** — **`OpUpsertMany` and its three consumer arms** (Ticket **Dc**, **D16**, **F9**). The `hook/` constant, plus cache invalidation, `mapOpToAction` (→ the existing `event.Upsert`, **not** a new wire action) and the §32.3 redaction switch. *Every one of the three switches has a permissive default, so omitting an arm **compiles clean and misbehaves at runtime**. Depends on 27.5, and on FIX-207 having made the redaction default fail closed.*
- **27.7** — **Supporting surface for the emitters** (Ticket **E**). `ErrAlreadyRelated` / `ErrNestedVerbConflict` / `*database.NestedMutationError` (**D15**); the junction-client and `callbackMode` fields on every entity client with an M2M edge — **three of them, not one**; ~~both halves corrected when 27.7 landed (2026-09-18)~~: junction clients are **four**, not three (`documentClient` too, per **A1**), and `callbackMode` is gated on the table having a *nested surface* rather than an M2M edge — `UpdateWithRelated` exists on O2M-only parents and passes it to the transaction it opens, so the M2M gate would have been wrong on 27.8's first such parent; the `resolved_names.go` **new key kind** (**A6** — the nested names key on (parent, edge), which falls in the registry's documented cross-shape blind spot, so new entries are not sufficient); ~~the four `Operations` fields across all `ExpandPreset` arms~~ — moved to **27.0a**. *Independent of 27.4–27.6.*
- **27.8** — **Go `CreateWithRelated`** (Ticket **F**). *E2E on all four shapes plus the polymorphic negative. **Must use `Asset.Documents` / `Document.Assets`** — the only M2M edges in any fixture with a UUID-PK target. Depends on 27.1, 27.2, 27.6, 27.7.*
- **27.9** — **Go `UpdateWithRelated` + `UpsertWithRelated`**, including the **Q9 batching** on `connect`, `disconnect` and the `connect` visibility read (Ticket **G**). *E2E per eligibility-matrix cell, incl. the **D8** negatives, the **D7** adopt-only three-way split, and the **E12** self-connect refusal on `WorkspaceNote.Children`. Depends on 27.3 and 27.8.*
- **27.10** — **GraphQL projection for all three** (Ticket **H**). *Verified through the real gqlgen server in `graphql/tests`, as 25.9 did. Depends on 27.8 and 27.9.*
- **27.11** — **Closure sweep.** *`/close-phase 27`.*

**Governing decisions** (design §3): **three separate methods, not new arguments** — `Create<T>Input` is shared with `Upsert` (**C3**), so nested members added there would be advertised in the schema and silently dropped, the exact accept-and-drop class Phase 25 was written to kill. **Depth 1**, which makes cycles structurally impossible and keeps GraphQL input types non-recursive. **Every write routes through the target's own generated client** (**NW-D10**) — never a hand-rolled INSERT — so hooks, events, cache invalidation, tenancy auto-set and constraint-error mapping are inherited rather than reimplemented. **`connect` verifies target visibility first** (**NW-D13**) — a security decision, not ergonomics: the FK constraint proves a row *exists*, not that the caller may see it, so without the read `connect` is a cross-tenant existence oracle and a silent no-op. **No `delete` verb** (**D9**): `disconnect` means unlink, never destroy. **`clear` runs first** (**D17**), which is what makes `set` expressible without shipping `set`.

**Dependency order:** 27.0 before all, then **27.0a** before every sub-item that touches config (27.2, 27.5, 27.7 and, through them, 27.8); FIX-207 and FIX-208 before 27.6 and 27.9 respectively. 27.1 and 27.3 need neither and can run at any time; 27.2, 27.7 and the 27.4 → 27.5 → 27.6 chain run in parallel once 27.0a has landed. 27.8 needs 27.1 + 27.2 + 27.6 + 27.7; 27.9 needs 27.3 + 27.8; 27.10 needs both. **27.1–27.7 each ship value standalone** — `UpsertMany` is a real gap in the write surface, the `Update` refetch is a wasted round-trip today, and `discriminator:` is the first structured description of a polymorphic edge in the config — which makes the end of 27.7 a natural checkpoint where the prerequisites have paid for themselves and F/G/H can be decided on fresh information.

**Tests required:** 27.1 — zero golden movement. 27.2 — reads byte-identical across the `assets` migration, plus **D18**(b)/(c) failing-first. 27.3 — one fewer round-trip on empty-`FieldOptions` `Update`. 27.4 — per-dialect unit coverage. 27.5 — idempotent batch upsert on a pure link table in one statement, plus the three dialect probes from companion §6.1–§6.3 as failing-first pins. 27.6 — three failing-first regression pins (cache invalidation, `event.Upsert` delivery, per-row **redacted** input), none of which fails at compile time. 27.8/27.9 — E2E per matrix cell; **plus the F11 runtime pin** (unlink a child, then assert its FK is actually NULL — since Phase 26 bound nullable UUIDs to `*uuid.UUID`, the wrong spelling compiles, matches its rows and reports success) and **a Q9 pin** sized past 32766, the SQLite bind-parameter ceiling. 27.10 — real gqlgen server.

**Out of scope (design §10):** the `delete` verb; **cascade delete** (`SoftDeleteWithRelated` — deferred on cascade *restore*, which needs persisted state to distinguish children the cascade deleted from children already deleted, not on cost); belongs-to `create` (shape 1 — the ordering inversion is the entire complexity budget, and `connect` on that shape is already the scalar FK field); depth > 1; a `set` verb (**D17** makes it expressible); M2M junctions carrying payload columns; re-parenting by default; nested writes under `CreateMany` / `UpdateMany` / `UpdateWhere`; and a parent correlation field on `hook.MutationContext` (**Q6**/**EQ3** — revisit before GA).

## Phase 28: Transaction Connection Lock

> **Status: Complete (closed 2026-09-22).** All 8 sub-items (28.0–28.7) landed. Full sweep clean under `-race` (check / check-examples / test-integration); no FIX entries logged against the phase — every review finding was fixed inside its own sub-item. **Three consumer-visible behavior changes to carry into the release notes**, none of whose commits carries a `!` or `BREAKING CHANGE:` footer: `Raw` takes a scan callback and returns only `error` (28.5); `Stream` inside a transaction errors unless `CallOptions.AllowInTransaction` is set (28.3); and a part-drained result set followed by another statement on the same transaction now waits rather than failing (28.2, design §4.2). Design doc `docs/design/archive/TX_CONNECTION_LOCK.md` (SYNCED 2026-09-22 — superseded by PRD §18.5 and the sections below; drafted 2026-09-19, every claim in it carries either a `file:line` from this repo or a measurement taken 2026-09-18/19/21 against `postgres:16-alpine`, `mysql:8.0` and `modernc.org/sqlite`). Moves the obligation *"one statement at a time per transaction"* from the **caller** to the `Tx` itself.

**Normative spec:** PRD §18.5 (the reservation and the one caller rule), plus §18.2, §18.4, §18.6, §20.2, §20.5, §9.4a, §9.6, §13.2, §19.1 and §29.4.3 — written by 28.0, as 25.0, 26.0 and 27.0 did, and amended by 28.2, 28.3, 28.5 and 28.6 where the landed code moved it. The design doc's banner lists every claim execution superseded.

**Depends on:** FIX-221 (Resolved 2026-09-19), which bounded the relationship loader's `errgroup` to one worker inside a transaction. That fix stands on its own and is what makes this phase optional rather than urgent; **28.4 removes the bound**, which the reservation makes redundant.

**The load-bearing structural fact** (design §2.5): the connection's busy window ends when a result set is **drained**, not when our `Close()` runs — both drivers close the result set themselves the moment `Next` reports exhaustion. An earlier draft scoped the lock to `Close()` and concluded that 47 query sites across 14 templates had to convert to a callback form in one all-or-nothing change, because an un-converted site would **deadlock**. That was correct for that scope, and the scope was wrong. Scoping the reservation to the driver's real busy window gets the same safety with **zero call-site conversions** — measured both ways, same tree, same day (§2.6): drain-scope passes eleven suites and three dialects with no template edited; close-scope hangs a test at 1m36s.

**The second structural fact** (§2.4): `tx.mu` guards the `Tx` struct's own state and is read on *every* call — `database.Conn` takes it via `IsClosed()` before issuing anything. A microsecond-scale lock and a result-set-scale lock **cannot be the same lock**; holding `tx.mu` across a result set parks the relationship loader's goroutines in `IsClosed()` in 45 seconds. The design therefore adds one new field (`connSem chan struct{}`, capacity 1 — a channel rather than a mutex because acquisition must be selectable against `ctx.Done()`), and nothing on the reservation path may touch `tx.mu`.

**Sub-items** (`docs/tracker/phase-28.md` — mapped from the design's §5.2 sequencing):
- **28.0** — **PRD sync.** No code. §18.5's caller-serializes rule narrows to *drain or close a result set before the next statement on that `txCtx`*; §20.5's withdrawn `errgroup` example is restored, now correct; §20.2/§18.6/§29.4.3 carry `Raw`'s new signature; §9.4a gains `Stream`'s guard, the `AllowInTransaction` opt-in and a decision-table row; §13.2 reverts FIX-221's caveat. **Decides two things the design leaves open**: whether `Begin` blocks on the reservation or proceeds without it, and whether `AllowInTransaction` is emitted unconditionally or gated on the package emitting a `Stream`. *Blocks all.*
- **28.1** — **Runtime preconditions.** Untangle `tx.mu` from the savepoint/commit/rollback statements so no acquisition spans a round trip; fix `database/stdlib`'s `rows.Next()` to close on exhaustion (§2.11 — without it release-on-drain is **unsound** for multi-result-set statements on MySQL, where the reservation is released while the connection still carries the next set). *Zero golden movement. Blocks 28.2.*
- **28.2** — **The reservation** (§3.1–§3.4). `connSem`, a `ctx`-selectable `acquireConn`, `txRows` releasing on `Next() == false`, `txRow` releasing at the end of `Scan`, and a **non-blocking teardown** so a leaked result set can never strand a pooled connection. Plus the `QueryRowFunc` fix (§3.8). *Zero template edits, zero golden movement. Blocks 28.3, 28.4, 28.6.*
- **28.3** — **`Stream` refuses a transaction, with an `AllowInTransaction` opt-in** (§2.10, §4.6, §6.2). `Stream` is the only generated method that runs consumer code *inside* an open result set, so it is the one API the reservation makes worse — a write inside the `yield` body goes from a loud `conn busy` to a hang that also stalls pool teardown. Opting `Stream` *out* of the reservation is ruled out by measurement: it preserves 13 data races. *Depends on 28.0, 28.2.*
- **28.4** — **Remove FIX-221's `g.SetLimit(1)`** (§6.5). The fan-out is unbounded again and the reservation serializes it, which is faster than the bound since the loads pipeline against the lock rather than waiting on `errgroup` slots — and it is what makes the generated tree exercise the reservation on every multi-edge read inside a transaction. *Depends on 28.2.*
- **28.5** — **`Raw` takes a scan callback** (§3.7). The one generated API that hands a live result set to a consumer, closed by construction rather than by documentation. The parameter shape deliberately matches `database.QueryFunc`. *Reviewable on its own; depends on 28.0 only.*
- **28.6** — **Documentation sweep** (§4.5). Generated doc comments, the `Tx` doc comment, `QueryFunc`/`QueryRowFunc` as the safe spelling, and marking the design doc SYNCED. *Depends on 28.2–28.5.*
- **28.7** — **Closure sweep.** *`/close-phase 28`.*

**Governing decisions** (design §5.1, §6): **the reservation goes in the `Tx` methods, not in call-site wrappers** — the funnel is the only place that sees every statement, and it is the only placement that reaches `client.Querier(ctx)`, which §20.2 specifies as a first-class escape hatch *with a worked example*. Wrapper-first would make the guarantee conditional on finishing 82 conversions across twelve golden trees; lock-first makes those wrappers optional hygiene. **No goroutine-id self-deadlock detection** (§6.1) — measured, and dropped for a `runtime.Stack` traceback per transactional read plus a third mutex. **The fan-out bound does not come back as belt-and-braces** (§6.5).

**The one honest regression** (§4.2): a caller that opens a result set, abandons it part-drained, and then issues another statement on the same transaction gets a wait where pgx reports `conn busy` today. Any deadline converts it to a loud error — but `TxOptions.Timeout` defaults to `0` and §18.2 makes zero-means-no-timeout an explicit design choice, so **the unbounded case is the default case, not an edge**. Two things narrow it: `Raw`'s callback (28.5) and `Stream`'s guard (28.3) close the two widest paths to a self-hold, leaving a consumer who holds rows open through `Querier(ctx)` — documented misuse.

**Tests required:** 28.1 — the §2.11 multi-result-set probe on MySQL, failing-first. 28.2 — the §2.8 leak probes (leak → next statement / `Commit` / `Rollback` / `ctx` deadline), which are the only coverage of paths that used to hang, plus an unbounded `-race` fan-out at 0 races. 28.3 — a refused `Stream` issues **no SQL**; an opted-in one survives §2.10 probe B. 28.4 — `relationship_tx_fanout_test.go` passes unchanged, before and after. 28.5 — two `Raw` reads fanned out over one `txCtx` under `-race`. 28.7 — full sweep, all three dialects, `-race`.

**Out of scope:** goroutine-id detection; `QueryFunc` adoption at the 82 generated call sites; an `ExecFunc`; constraining `Querier(ctx)` or `database.FromContext`; pipelining / `pgx.Tx.SendBatch` (the only thing that actually reduces latency inside a transaction, and orthogonal — the lock is about safety, batching about speed); and making `Stream` genuinely transaction-safe, which §4.6 establishes is impossible without breaking its memory bound, §9.4a's no-`Limit` contract, cross-dialect portability, or transaction semantics.

## Phase 29: Generate Every Client Method the Schema Allows

> **Status: Complete (closed 2026-10-03).** Both sub-items (29.0 PRD sync, 29.1 implementation) landed. Normative spec PRD §4.6 Operations, with §4.13/§9.8/§9.9.4/§20.2/§23.7/§26.5.1/§26.10/§30.4.3. FIX-224 is resolved. Full sweep clean under `-race`. Removes the per-table `operations` toggles from the Go client. The client generates every method the schema allows, and `api.operations` becomes the only per-operation control. Decided 2026-09-24 from a dependency map of the 21 toggles, traced from the generator code, and a survey of other generators.

**Why.**
- **No other DB-first client generator does this.** SQLBoiler, Bob, Ent, GORM Gen, go-jet, jOOQ, SeaORM and Prisma Client offer table include/exclude, package-wide feature flags, or per-artifact switches, but no per-table, per-operation opt-out. Their client write limits come from schema facts: views, tables without a primary key, immutable columns. Per-operation controls live at the API layer (PostGraphile `@omit`, Hasura permissions, Keystone `graphql.omit`, Amplify `disableOperations`, entgql).
- **A client opt-out is not enforcement.** `Raw`, another service or a hand-written query still reaches the table. Database grants are the enforcement; `api.operations` is the exposure control.
- **The toggles cost more than they give.** Splitting a group breaks the build twice over, both measured: `update_where: false` (FIX-224) and `paginate: false`, where `<t>List` calls a `Paginate` that was not generated. `get`, `get_many` and `count` are toggles that do nothing. Nested rules E4, E8 and E9 silently drop edges when a target or junction table has a method off.
- **Nothing uses them.** No example config sets `operations`; only tests do (40 Go literals, 6 YAML fixtures).

**Normative spec:** PRD §4.6, §4.13, §9.8, §9.9.4, §10 (entity client interfaces), §26.5.1 and §26.10, written by 29.0.

**Depends on:** Phase 27 closed. 29.1 edits the nested eligibility rules and `context_api_nested.go` that 27.8–27.10 landed. **Resolves FIX-224**, whose mask-key rename lands here.

**Sub-items** (`docs/tracker/phase-29.md`):
- **29.0** — **PRD sync.** No code. Removes `generation.operations` and `tables.<t>.operations`; states that the client emits every method the schema allows; keeps the schema-fact gates (views, tables without a PK, the soft-delete column, incrementable columns, cursor keys); drops the per-table `*_with_related` toggles, leaving nested mutations governed by `nested_mutations` alone; strips the toggle clauses from E4, E8 and E9; makes `api.operations` the only per-operation control. **Decides three things:** the mask key for `update<T>s` (FIX-224 recommends `update_where`); whether a leftover `operations` key fails with the strict decoder's plain unknown-field error or a message that points at `api.operations`; and whether the per-table over-reach warning survives now that the client set is the schema-allowed set. *Blocks 29.1.*
- **29.1** — **Remove the client toggles.** Config, validation and JSON schema; `toResolvedOperations` derives from schema facts only; nested eligibility stops reading other tables' toggles; the API gates read the mask alone; the manifest's pagination flag and lint rule 3 go; tests move off the toggles. Example goldens should not move, since no example sets `operations`, except the manifest's `generation_config.pagination` field (and its `_index.md` `Features:` entry), which 29.0 drops, and one `hook.OpIncrement` line in the `graphql` example's `event_hooks_gen.go` now that Increment is the incrementable-column fact. *Depends on 29.0.*

Closure is `/close-phase 29`, with no sub-item of its own.

**Tests required:** 29.1 — an `operations` key at either level fails validation; resolved operations equal the schema facts for a view, a table without a soft-delete column, one without an incrementable column and one without resolvable cursor keys; nested eligibility is unchanged by any toggle on a target or junction table; `api.operations: {update_where: false}` removes `update<T>s` and the O2M / has-one relink verbs into that table; `TestE2EGoldenFiles` is byte-identical across all 12 examples apart from the dropped `generation_config.pagination` manifest field (and its `_index.md` `Features:` entry) and the one `graphql` `event_hooks_gen.go` `OpIncrement` line.

**Out of scope:** a table-level write policy (such as `writes: none | append`), deferred until a real need appears; changes to `api.operations` presets beyond the FIX-224 key rename; the other findings from the dependency map (`update<T>s` dropping `_inc` / `_dec`, `validateIncDecNamespace` ignoring `increment`) unless they are logged as FIX entries.

## Deferred Features

These remain deferred until consumer demand justifies the implementation cost. Phase 15 establishes the unified client surface as feature-complete for direct Go consumers; Phase 16 extends it to GraphQL consumers. The deferred items below extend further or address adjacent surfaces.

### D.1 API Generation — REST (Section 26.8)

**Depends on:** Phase 16 stable. The schema-traversal scaffolding from Phase 16 (`fieldOptionsFromCollected` walker shape, filter / comparator translators, error-mapping helper) is reusable for REST handler generation; sequencing REST after GraphQL avoids building two independent translation layers.

**What to build:**
- `net/http` handlers per table per enabled operation (PRD §26.8 endpoint table).
- Standard REST response format (`data`, `meta`, `error`).
- OpenAPI 3.x spec generation.
- `NewAPIHandler(client, ...opts) → http.Handler`.
- `CallOptions` from HTTP headers (reuse Phase 16's `WithCallOptionsMiddleware` per PRD §26.11).
- Per-table path overrides.

---

### D.2 GraphQL Subscriptions (Section 26.12)

**Depends on:** Phase 16 stable; design questions resolved (WebSocket transport, connection-init auth context, backpressure policy, optional sequence-cursor / replay semantics). The infrastructure pieces are in place (§28 typed event system + the existing distributed event-bus interface); a future phase would add a thin subscription resolver wrapping `FromEventSubscriber` and reusing the §26.5.2 walker.

**What to build:**
- WebSocket transport scaffolding in `graph/server.go`.
- Connection-init auth context (tenant resolver fires once at connect, cached for the connection lifetime).
- Backpressure policy on slow consumers.
- Optional sequence-cursor / replay semantics tied to event-bus partition offsets.
- Field-selection walker reuse for subscription payload shape (no new walker — `fieldOptionsFromCollected` works as-is).

---

## Testing Strategy Summary

| Phase | Test Type | Infrastructure |
|-------|-----------|---------------|
| 0 | Unit tests only | None |
| 1 | Unit tests (SQL string comparison) | None |
| 2 | Unit + Integration | testcontainers (PG, MySQL), SQLite in-memory |
| 3 | Unit + Integration | testcontainers for real constraint violations |
| 4 | Unit + Integration | testcontainers (parse file → introspect DB → compare) |
| 5 | Unit tests | None |
| 6 | Unit (template output) + Golden file comparison | None |
| 7 | Unit (with mock driver) | None |
| 8 | Unit (CLI commands) + E2E (example projects) | testcontainers for E2E |
| 9 | Existing tests migrated (no new tests) | None |
| 10 | E2E (golden file comparison + runtime tests) | testcontainers (PG, MySQL), SQLite in-memory |
| 11 | Unit (event types) + E2E (mock publisher) | testcontainers (PG, MySQL), SQLite in-memory |
| 12 | Unit (cache/ core, backends, serializer, breaker, singleflight, error policy) + Integration (Redis via testcontainers, skip with -short) + E2E (in-memory backend, SQLite; event-driven invalidation, fingerprint regen, composite-PK round-trip, circuit breaker, tx safety) | testcontainers (Redis), SQLite in-memory |
| 13 | Unit (tenancy/ package, config resolvers, validation) + E2E (cross-dialect tenancy example: isolation, mismatch, missing-tenant, SkipTenancy, soft-delete + tenancy, cache isolation, event metadata, relationship propagation, tx) | testcontainers (PG, MySQL), SQLite in-memory |
| 14 | E2E only — additive gap-fill across existing dialect + cache + events + tenancy examples (no new runtime code) | testcontainers (PG, MySQL, Redis), SQLite in-memory |
| 15 | Unit (LockMode dialect emission, codegen guards, Stream template + scalars-only FieldOptions invariant, OpStream hook op) + E2E (cross-dialect Stream iteration with memory-bound assertions, LockMode read-modify-write semantics, FOR UPDATE SKIP LOCKED job-queue across concurrent tx, SQLite-rejection paths, tenancy/cache integration) | testcontainers (PG, MySQL), SQLite in-memory |
| 16 | Unit (schema generator per table / casing / operations gating, scalar registry round-trips, gqlgen-wrapper YAML merge with consumer-wins precedence, recursive walker fixture queries with fragment / `@skip` / alias coverage, walker-completeness lint, comparator translators per family, error-mapping per sentinel, header → CallOptions middleware, multi-schema disambiguation) + E2E (real gqlgen server booted via wrapper subprocess; full curated-surface query / mutation coverage; PageInfo / Edges / ListResult shape pins; `extensions.code` per-sentinel pins; counting-Querier query-count assertions per §25.1; layered-mode `gqlgen.yml` regression with custom directives + extra models; cache-bypass via `Cache-Control: no-cache`; tenancy resolver missing → `UNAUTHENTICATED`) | testcontainers (PG, MySQL), SQLite in-memory; gqlgen subprocess via consumer `go run` |

**E2E example projects** (`testdata/examples/`): Three dialect examples (postgres, mysql, sqlite) each containing `sqlgen.yml`, `schema.sql`, `expected/` golden files, and `tests/` split by feature area (CRUD, batch, relationships, pagination, soft delete, composite PK, views, type overrides). All features are tested across all three dialects. See Section 8.1.7 for details.

**CI:** Unit tests run on every PR (no external deps). Integration tests run in a separate job with `SQLGEN_INTEGRATION=true`. Both use `-race`.
