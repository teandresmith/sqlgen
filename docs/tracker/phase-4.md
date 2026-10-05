# Phase 4: Parser Module

Status: In Progress
PRD Sections: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 6

## 4.1 Schema Model — Core types for parsed SQL schemas

**PRD Reference:** Section 5.1, 5.5, 5.7

**Status:** Complete

### Tasks

- [x] Define `Schema` struct in `parser/schema.go` — top-level container holding tables, enums, composite types, domain types, views, relationships
- [x] Define `Table` struct — name, schema, columns, constraints, comment; with deterministic column ordering
- [x] Define `Column` struct — name, type (raw SQL string), nullable, PK, unique, default expression, FK reference, auto-increment, comment
- [x] Define `Constraint` struct — type (PK, FK, UNIQUE, CHECK, INDEX), columns, reference table/columns, name, check expression
- [x] Define `ConstraintType` enum — `PrimaryKey`, `ForeignKey`, `Unique`, `Check`, `Index`
- [x] Define `Enum` struct — name, schema, values (ordered)
- [x] Define `CompositeType` struct — name, schema, attributes (name + type pairs)
- [x] Define `DomainType` struct — name, schema, base type, constraints
- [x] Define `Relationship` struct — name, type (O2O/O2M/M2M), source table, target table, FK column, junction table, junction FKs, filter, sort
- [x] Define `RelationshipType` enum — `OneToOne`, `OneToMany`, `ManyToMany`
- [x] Define `View` struct — name, schema, SQL text, columns (inferred)
- [x] Define `FKReference` struct — table, schema, column
- [x] Implement `Schema.Sort()` — deterministic sorting of all elements (tables by schema+name, columns by ordinal, enums by name, etc.)
- [x] Write unit tests in `parser/schema_test.go`

### Acceptance Criteria

- All types defined match PRD Section 5.1 element descriptions
- `Schema` holds all element types: Tables, Enums, CompositeTypes, DomainTypes, Views, Relationships
- `Table` stores schema-qualified name (schema + name fields, per PRD 5.5)
- `Column` captures all metadata: name, type, nullable, PK, unique, default, FK reference, auto-increment, comment
- `Constraint` supports all five types: PK, FK, UNIQUE, CHECK, INDEX
- `Relationship` supports all three types: O2O, O2M, M2M with junction table fields for M2M
- `Schema.Sort()` produces deterministic ordering — same input always produces same output
- All slices are ordered (not maps) to ensure deterministic iteration
- Package has zero external dependencies (stdlib only)

### Tests Required

- [x] Schema model construction — all fields accessible and correctly typed
- [x] `Schema.Sort()` produces consistent ordering across multiple calls
- [x] `Schema.Sort()` handles empty schema (no tables, no enums)
- [x] Tables sort by schema then name; columns maintain insertion order (ordinal)
- [x] Enums, CompositeTypes, DomainTypes sort by schema then name
- [x] Relationship types have correct string values

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `parser/schema.go`, `parser/schema_test.go`
- **Files modified:** `parser/go.mod`, `parser/go.sum`
- **Notes:** All schema model types, `Schema.Sort()`, and 6 test functions (10 subtests). Zero external deps in production code; `go-cmp` is test-only.

---

## 4.2 PostgreSQL Parser — Parse PostgreSQL DDL into Schema model

**PRD Reference:** Section 5.2 (PostgreSQL), 5.5

**Status:** Complete

### Tasks

- [x] Add `pg_query_go/v6` dependency to `parser/go.mod`
- [x] Define `Parser` interface in `parser/parser.go` — `Parse(filename string, sql []byte) error`, `Schema() *Schema`
- [x] Implement `postgres.Parser` in `parser/postgres/postgres.go`
- [x] Pass 1: Parse `CREATE TABLE` — extract table name, columns (name, type, nullable, default, PK, unique, FK), inline constraints
- [x] Pass 1: Parse `CREATE TYPE ... AS ENUM` — extract enum name and values
- [x] Pass 1: Parse `CREATE TYPE ... AS` (composite) — extract type name and attributes
- [x] Pass 1: Parse `CREATE DOMAIN` — extract domain name, base type, constraints
- [x] Pass 2: Parse `COMMENT ON TABLE/COLUMN` — attach comments to tables and columns
- [x] Pass 2: Parse `ALTER TABLE ADD COLUMN` — add column to existing table
- [x] Pass 2: Parse `ALTER TABLE DROP COLUMN` — remove column from table
- [x] Pass 2: Parse `ALTER TABLE ADD CONSTRAINT` — add constraint (PK, FK, UNIQUE, CHECK)
- [x] Pass 2: Parse `ALTER TABLE DROP CONSTRAINT` — remove named constraint
- [x] Schema qualification: bare table names get `input.schema` prefix (default `"public"`)
- [x] Handle quoted identifiers and reserved word column names
- [x] Handle `serial`/`bigserial`/`smallserial` as auto-increment integer types
- [x] Handle `GENERATED { ALWAYS | BY DEFAULT } AS IDENTITY` as auto-increment
- [x] Handle array types (`text[]`, `integer[]`, etc.)
- [x] Write unit tests in `parser/postgres/postgres_test.go`

### Acceptance Criteria

- `CREATE TABLE` produces correct `Table` with all columns, types, nullability, defaults, constraints
- `CREATE TYPE ... AS ENUM` produces `Enum` with ordered values
- `CREATE TYPE ... AS (...)` produces `CompositeType` with attributes
- `CREATE DOMAIN` produces `DomainType` with base type and constraints
- `COMMENT ON TABLE/COLUMN` attaches comments to the correct table/column
- `ALTER TABLE` operations (ADD/DROP COLUMN, ADD/DROP CONSTRAINT) modify tables correctly
- Bare table name `products` → `public.products` when schema default is `"public"`
- Explicit `myschema.products` → `myschema.products` regardless of default
- Quoted identifiers like `"order"` are parsed without error
- `serial` → auto-increment `int32`, `bigserial` → auto-increment `int64`
- `GENERATED { ALWAYS | BY DEFAULT } AS IDENTITY` → auto-increment with the column's declared type
- Array types `text[]` → type string `"text[]"`
- Parser is re-entrant — can be called with multiple files sequentially

### Tests Required

- [x] Unit: `CREATE TABLE` with all column types (text, int, timestamp, bool, numeric, uuid, json, bytea, arrays)
- [x] Unit: `CREATE TABLE` with inline PK, UNIQUE, NOT NULL, DEFAULT, FK REFERENCES
- [x] Unit: `CREATE TABLE` with table-level constraints (composite PK, composite UNIQUE, named FK, CHECK)
- [x] Unit: `CREATE TYPE ... AS ENUM` with multiple values
- [x] Unit: `CREATE TYPE ... AS (...)` composite type
- [x] Unit: `CREATE DOMAIN` with CHECK constraint
- [x] Unit: `COMMENT ON TABLE` and `COMMENT ON COLUMN`
- [x] Unit: `ALTER TABLE ADD COLUMN`, `ALTER TABLE DROP COLUMN`
- [x] Unit: `ALTER TABLE ADD CONSTRAINT`, `ALTER TABLE DROP CONSTRAINT`
- [x] Unit: Schema qualification — bare name gets default schema, explicit schema preserved
- [x] Unit: Quoted identifiers and reserved words as column names
- [x] Unit: `serial`/`bigserial`/`smallserial` detection
- [x] Unit: `GENERATED ALWAYS AS IDENTITY` and `GENERATED BY DEFAULT AS IDENTITY` detection
- [x] Unit: Empty table (no columns beyond PK)
- [x] Unit: Table with no PK

### Completion Record

- **Date:** 2026-04-08
- **Files created:** `parser/parser.go`, `parser/postgres/postgres.go`, `parser/postgres/postgres_test.go`
- **Files modified:** `parser/go.mod`, `parser/go.sum`
- **Notes:** Two-pass PostgreSQL parser using pg_query_go/v6 (CGO). Parser interface defined. 15 test functions (17 subtests for serial/identity) covering all column types, inline/table-level constraints, enum/composite/domain types, COMMENT ON, ALTER TABLE ADD/DROP COLUMN/CONSTRAINT, schema qualification, quoted identifiers, serial and identity column detection, empty tables, no-PK tables, re-entrant parsing, and cross-schema FK references. Zero lint issues.

---

## 4.3 MySQL Parser — Parse MySQL DDL into Schema model

**PRD Reference:** Section 5.2 (MySQL)

**Status:** Complete

### Tasks

- [x] Add `vitess/sqlparser` dependency to `parser/go.mod`
- [x] Implement `mysql.Parser` in `parser/mysql/mysql.go` conforming to `Parser` interface
- [x] Parse `CREATE TABLE` — extract columns, types, nullable, default, PK, unique, FK, indexes
- [x] Parse inline `ENUM('val1', 'val2')` column type — extract values, treat as string column
- [x] Parse `AUTO_INCREMENT` column attribute
- [x] Parse unsigned integer types (`INT UNSIGNED`, `BIGINT UNSIGNED`)
- [x] Parse inline comments (`COMMENT 'text'` on columns and tables)
- [x] Parse `ALTER TABLE ADD COLUMN`, `ALTER TABLE DROP COLUMN`, `ALTER TABLE MODIFY COLUMN`
- [x] Parse `ALTER TABLE ADD INDEX`, `ALTER TABLE DROP INDEX`
- [x] Parse `ALTER TABLE ADD CONSTRAINT`, `ALTER TABLE DROP CONSTRAINT`
- [x] MySQL has no schema prefix — table names are always bare
- [x] Write unit tests in `parser/mysql/mysql_test.go`

### Acceptance Criteria

- `CREATE TABLE` produces correct `Table` with all MySQL column types
- Inline `ENUM('a','b','c')` extracts enum values and produces an `Enum` entry linked to the column
- `AUTO_INCREMENT` sets `Column.AutoIncrement = true`
- `UNSIGNED` integer types are captured in the type string (e.g., `"int unsigned"`)
- Inline `COMMENT 'text'` attaches to the correct column or table
- `ALTER TABLE` operations modify tables correctly (ADD/DROP/MODIFY COLUMN, ADD/DROP INDEX/CONSTRAINT)
- Table names have no schema prefix (MySQL uses databases, not schemas)
- Parser handles MySQL-specific syntax without error

### Tests Required

- [x] Unit: `CREATE TABLE` with MySQL types (tinyint, smallint, int, bigint, float, double, decimal, varchar, text, datetime, timestamp, date, json, blob, bool, enum, set)
- [x] Unit: `CREATE TABLE` with inline constraints (PK, UNIQUE, NOT NULL, DEFAULT, FK)
- [x] Unit: Inline `ENUM('val1', 'val2')` extraction
- [x] Unit: `AUTO_INCREMENT` detection
- [x] Unit: Unsigned integer types (`INT UNSIGNED`, `BIGINT UNSIGNED`)
- [x] Unit: Inline `COMMENT` on column and table
- [x] Unit: `ALTER TABLE ADD COLUMN`, `DROP COLUMN`, `MODIFY COLUMN`
- [x] Unit: `ALTER TABLE ADD INDEX`, `DROP INDEX`
- [x] Unit: `ALTER TABLE ADD CONSTRAINT` (FK, CHECK), `DROP CONSTRAINT`
- [x] Unit: Table with no explicit PK

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/mysql/mysql.go`, `parser/mysql/mysql_test.go`
- **Files modified:** `parser/go.mod`, `parser/go.sum`
- **Notes:** MySQL parser using vitess.io/vitess sqlparser (pure Go, no CGO). Parser interface implemented. 12 test functions covering all MySQL column types, inline constraints, ENUM extraction, AUTO_INCREMENT, UNSIGNED types, inline COMMENT, ALTER TABLE ADD/DROP/MODIFY COLUMN, ALTER TABLE ADD/DROP INDEX, ALTER TABLE ADD/DROP CONSTRAINT (FK, CHECK), table-level constraints, no-PK tables, and re-entrant parsing. Zero lint issues.

---

## 4.4 SQLite Parser — Parse SQLite DDL into Schema model

**PRD Reference:** Section 5.2 (SQLite)

**Status:** Complete

### Tasks

- [x] Add `rqlite/sql` dependency to `parser/go.mod`
- [x] Implement `sqlite.Parser` in `parser/sqlite/sqlite.go` conforming to `Parser` interface
- [x] Parse `CREATE TABLE` — extract columns, types, nullable, default, PK, unique, FK, constraints
- [x] Handle type affinity: unrecognized types default to BLOB
- [x] Handle `AUTOINCREMENT` / `INTEGER PRIMARY KEY` (implicit rowid alias)
- [x] Parse `ALTER TABLE ADD COLUMN`
- [x] Parse `ALTER TABLE DROP COLUMN` (SQLite 3.35+)
- [x] Parse `ALTER TABLE RENAME COLUMN`
- [x] Parse `ALTER TABLE RENAME TABLE`
- [x] Parse generated columns: `GENERATED ALWAYS AS (expr) VIRTUAL|STORED` and shorthand `AS (expr)` (SQLite 3.31+)
- [x] Recognize `STRICT` table modifier (SQLite 3.37+)
- [x] Recognize `WITHOUT ROWID` table modifier (SQLite 3.8.2+) — no AUTOINCREMENT allowed, INTEGER PRIMARY KEY is not a rowid alias
- [x] SQLite has no schema prefix — table names are always bare
- [x] No support for MODIFY COLUMN, enums, composite types, or domain types
- [x] Write unit tests in `parser/sqlite/sqlite_test.go`

### Acceptance Criteria

- `CREATE TABLE` produces correct `Table` with SQLite column types
- Type affinity is applied: unrecognized type names default to `"blob"`
- `INTEGER PRIMARY KEY` detected as auto-increment (rowid alias) — except in WITHOUT ROWID tables
- Generated columns (`GENERATED ALWAYS AS`, `AS`) parsed with expression and storage type (VIRTUAL/STORED)
- `STRICT` and `WITHOUT ROWID` table modifiers are recognized and stored
- `ALTER TABLE ADD COLUMN` adds column to existing table
- `ALTER TABLE DROP COLUMN` removes column from existing table (SQLite 3.35+)
- `ALTER TABLE RENAME COLUMN` renames column in existing table
- `ALTER TABLE RENAME TABLE` renames the table
- No schema prefix on any table name
- Unsupported DDL (MODIFY COLUMN, CREATE TYPE, etc.) is gracefully rejected or ignored

### Tests Required

- [x] Unit: `CREATE TABLE` with SQLite types (integer, text, real, blob, boolean, numeric, varchar)
- [x] Unit: `CREATE TABLE` with constraints (PK, UNIQUE, NOT NULL, DEFAULT, FK, CHECK)
- [x] Unit: Type affinity — unrecognized type defaults to BLOB
- [x] Unit: `INTEGER PRIMARY KEY` auto-increment detection
- [x] Unit: `INTEGER PRIMARY KEY` in `WITHOUT ROWID` table is NOT auto-increment
- [x] Unit: Generated column with `GENERATED ALWAYS AS (expr) STORED`
- [x] Unit: Generated column with shorthand `AS (expr)` (defaults to VIRTUAL)
- [x] Unit: `STRICT` table modifier recognized
- [x] Unit: `WITHOUT ROWID` table modifier recognized
- [x] Unit: `ALTER TABLE ADD COLUMN`
- [x] Unit: `ALTER TABLE DROP COLUMN`
- [x] Unit: `ALTER TABLE RENAME COLUMN`
- [x] Unit: `ALTER TABLE RENAME TABLE`
- [x] Unit: Table with no PK
- [x] Unit: Unsupported ALTER TABLE operations are rejected

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/sqlite/sqlite.go`, `parser/sqlite/sqlite_test.go`
- **Files modified:** `parser/go.mod`, `parser/go.sum`, `parser/schema.go`
- **Notes:** SQLite parser using rqlite/sql (pure Go, no CGO). Parser interface implemented. 17 test functions covering all SQLite column types, inline and table-level constraints, type affinity (unrecognized types default to blob), INTEGER PRIMARY KEY auto-increment detection, WITHOUT ROWID tables (INTEGER PK is NOT auto-increment), generated columns (STORED and VIRTUAL shorthand), STRICT modifier, ALTER TABLE ADD/DROP/RENAME COLUMN, ALTER TABLE RENAME TABLE, no-PK tables, unsupported operations rejected, re-entrant parsing, and table-level constraints (composite PK, UNIQUE, FK, CHECK). Schema model extended with Table.Strict, Table.WithoutRowID, Column.GeneratedExpr, Column.GeneratedStorage. Zero lint issues.

---

## 4.5 Relationship Detection — Detect O2O, O2M, M2M from FK constraints

**PRD Reference:** Section 5.3

**Status:** Complete

### Tasks

- [x] Implement `DetectRelationships(schema *Schema)` in `parser/relationship.go`
- [x] M2M detection: table has exactly 2 FK columns, both in composite PK or UNIQUE constraint → create bidirectional relationships
- [x] O2O detection: column has FK + UNIQUE constraint
- [x] O2M detection: column has FK, not unique (fallback)
- [x] Handle self-referential FK (table references itself)
- [x] Handle circular references (A→B→A)
- [x] Table with 3+ FKs in composite PK → NOT M2M (exactly 2 required)
- [x] Write unit tests in `parser/relationship_test.go`

### Acceptance Criteria

- Junction table with exactly 2 FK columns in composite PK/UNIQUE → M2M detected, bidirectional relationships created
- Column with FK + UNIQUE → O2O relationship
- Column with FK, not unique → O2M relationship (default fallback)
- Self-referential FK produces a valid relationship (source = target)
- Table with 3+ FK columns in PK is not treated as M2M
- Circular FK references (A→B→A) are detected without infinite loops
- All detected relationships are added to `Schema.Relationships`

### Tests Required

- [x] Unit: Junction table with composite PK of 2 FKs → M2M bidirectional
- [x] Unit: Junction table with composite UNIQUE of 2 FKs → M2M bidirectional
- [x] Unit: FK column with UNIQUE constraint → O2O
- [x] Unit: FK column without UNIQUE → O2M
- [x] Unit: Self-referential FK → relationship with source = target
- [x] Unit: Table with 3+ FK columns in PK → not M2M
- [x] Unit: Circular FK references (A→B→A) → no infinite loop, relationships detected
- [x] Unit: Table with no FKs → no relationships detected

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/relationship.go`, `parser/relationship_test.go`
- **Files modified:** None
- **Notes:** DetectRelationships post-parse pass with M2M (junction table with exactly 2 FKs in composite PK/UNIQUE), O2O (FK + UNIQUE), and O2M (FK without UNIQUE, default fallback). 8 test functions covering all relationship types, self-referential FKs, 3+ FKs not M2M, circular references without infinite loops, and no-FK tables. Zero lint issues, zero external deps.

---

## 4.6 Soft Delete & Update Column Detection — Detect auto-managed columns

**PRD Reference:** Sections 5.4, 17.1, 17.2, 17.5

**Status:** Complete

### Tasks

- [x] Implement `DetectSoftDelete(table *Table, columns []string) (column string, strategy string)` in `parser/` — checks columns against config list in priority order
- [x] Strategy detection from column type: timestamp type → `"timestamp"`, bool type → `"bool"`, integer type → `"integer"`
- [x] Type validation: reject columns whose type doesn't match a valid soft delete strategy (e.g., varchar)
- [x] Implement `DetectUpdateColumns(table *Table, columns []string) []string` — find columns matching `update_columns` config list
- [x] Write unit tests

### Acceptance Criteria

- First matching column in priority-ordered list wins (e.g., `deleted_at` before `is_deleted`)
- Timestamp-typed column → strategy `"timestamp"` (sets `CURRENT_TIMESTAMP`, restores `NULL`)
- Bool-typed column → strategy `"bool"` (sets `TRUE`, restores `FALSE`)
- Integer-typed column → strategy `"integer"` (sets `1`, restores `0`)
- Column with non-matching type (e.g., `deleted_at` as `varchar`) → validation error
- No matching column → no soft delete for that table (returns empty)
- Update column detection returns all matching columns from the config list

### Tests Required

- [x] Unit: Priority ordering — `deleted_at` wins over `is_deleted` when both exist
- [x] Unit: Timestamp column detected correctly → `"timestamp"` strategy
- [x] Unit: Bool column detected correctly → `"bool"` strategy
- [x] Unit: Integer column detected correctly → `"integer"` strategy
- [x] Unit: Type mismatch (varchar for soft delete column name) → validation error
- [x] Unit: No match → no soft delete
- [x] Unit: Update column detection returns matching columns

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/detect.go`, `parser/detect_test.go`
- **Files modified:** None
- **Notes:** DetectSoftDelete with priority-ordered column matching, type-based strategy inference (timestamp/bool/integer), and type validation (unsupported types return error). DetectUpdateColumns returns all matching columns in config order. 2 test functions (15 subtests for soft delete, 4 subtests for update columns) covering priority ordering, all three strategy types with SQL type variants, type mismatch errors, no-match cases, and update column detection. Zero lint issues, zero external deps.

---

## 4.7 Multi-File Parsing — File ordering, sequential migration application, and down-file filtering

**PRD Reference:** Section 5.6

**Status:** Complete

### Tasks

- [x] Implement file discovery: expand directories to sorted file lists (lexicographic order)
- [x] Implement down-file filtering: skip `*.down.sql` and `*_down.sql` patterns
- [x] Implement sequential migration application: files parsed in order, statements applied to accumulating schema
- [x] Duplicate `CREATE` detection in dialect parsers: table, enum, composite type, domain type → error if already exists
- [x] `DROP TABLE` with FK reference validation → error (implemented in dialect parsers)
- [x] `DROP TYPE` with column type reference validation → error (implemented in dialect parsers)
- [x] Simplify `ParseFiles` — remove merge mode, snapshot logic, `ParseMode` type
- [x] Write unit tests

### Acceptance Criteria

- Files within a directory are sorted lexicographically
- Paths listed in config are processed in specified order
- `*.down.sql` and `*_down.sql` files are skipped; `*.up.sql`, `*_up.sql`, and `*.sql` are processed
- Duplicate `CREATE TABLE` across files → validation error
- Duplicate `CREATE TYPE` / `CREATE DOMAIN` across files → validation error
- `ALTER TABLE ADD COLUMN` in a later file extends the table from an earlier file
- `ALTER TABLE DROP COLUMN` in a later file removes the column
- `DROP TABLE` in a later file removes the table from the schema
- `DROP TABLE` when another table has a FK reference → validation error
- `DROP TYPE` when a column uses the type → validation error
- Sequential application produces the same result as running migrations against a real database

### Tests Required

- [x] Unit: File ordering — lexicographic within directory
- [x] Unit: Down-file patterns skipped (`*.down.sql`, `*_down.sql`)
- [x] Unit: Up-file patterns not skipped (`*.up.sql`, `*_up.sql`)
- [x] Unit: Duplicate CREATE TABLE across files → error
- [x] Unit: Duplicate CREATE TYPE across files → error
- [x] Unit: Sequential CREATE TABLE + ALTER TABLE ADD COLUMN → correct final schema
- [x] Unit: Sequential CREATE TABLE + DROP TABLE → table removed
- [x] Unit: Sequential CREATE TABLE + ALTER TABLE DROP COLUMN → column removed
- [x] Unit: DROP TABLE with FK reference → error
- [x] Unit: DROP TYPE with column reference → error
- [x] Unit: DROP TABLE + re-CREATE TABLE → works (fresh definition)
- [x] Dialect: PostgreSQL DROP TABLE/TYPE/DOMAIN + duplicate CREATE tests
- [x] Dialect: MySQL DROP TABLE + duplicate CREATE tests
- [x] Dialect: SQLite DROP TABLE + duplicate CREATE tests

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/multifile.go`, `parser/multifile_test.go`
- **Files modified:** `parser/schema.go` (CheckTableDropRefs, CheckTypeDropRefs), `parser/postgres/postgres.go` (DROP TABLE/TYPE/DOMAIN handling, duplicate CREATE detection), `parser/mysql/mysql.go` (DROP TABLE handling, duplicate CREATE detection), `parser/sqlite/sqlite.go` (DROP TABLE handling, duplicate CREATE detection), `parser/postgres/postgres_test.go`, `parser/mysql/mysql_test.go`, `parser/sqlite/sqlite_test.go`
- **Notes:** Simplified design — no merge mode. ParseFiles is a simple sequential loop. Duplicate CREATE detection and DROP validation live in dialect parsers. Multi-file tests cover file discovery (5 tests), duplicate CREATE (2), sequential migrations (5), drop validation (2), schema qualification (1), and ALTER on nonexistent (1). Dialect parser tests cover DROP TABLE/TYPE/DOMAIN with and without references, duplicate CREATE, and cross-file scenarios. Zero lint issues.

---

## 4.8 Database Introspection — Live database schema discovery

**PRD Reference:** Section 6

**Status:** Complete

### Tasks

- [x] Define `Introspector` interface in `parser/introspect/introspect.go` — `Name() string`, `Introspect(ctx, connString, schema *Schema, opts IntrospectionOptions) error`, `Close() error`
- [x] Define `IntrospectionOptions` struct — schema/table include/exclude filters, wildcard support (`*`, `?`)
- [x] Implement PostgreSQL introspector — `information_schema` + `pg_catalog` queries for tables, columns, constraints, enums, composite types, domain types, comments
- [x] Implement MySQL introspector — `information_schema` queries for tables, columns, constraints, comments
- [x] Implement SQLite introspector — `PRAGMA table_list`, `PRAGMA table_info`, `PRAGMA foreign_key_list`, etc.
- [x] Implement schema/table filtering with include/exclude lists and wildcard matching
- [x] Implement `both` mode merge: introspect DB → parse files → overlay file changes onto introspected schema
- [x] `both` mode: file `CREATE TABLE` on existing DB table → validation error
- [x] `both` mode: file `ALTER TABLE` on DB table → apply modification
- [x] `both` mode: file column conflict (same name, different type vs DB) → validation error
- [x] Write integration tests using testcontainers

### Acceptance Criteria

- `Introspector` interface defined with `Name()`, `Introspect()`, `Close()` methods
- PostgreSQL introspector discovers tables, columns (all metadata), PK, FK, UNIQUE, CHECK, enums, composite types, domain types, comments, multi-schema
- MySQL introspector discovers tables, columns, PK, FK, UNIQUE, CHECK, comments
- SQLite introspector discovers tables, columns, PK, FK via PRAGMAs
- Include/exclude filters work with exact names and wildcards (`*`, `?`)
- `both` mode: DB base + file overlay → correct merged schema
- `both` mode: file `CREATE TABLE` on existing DB table → validation error
- `both` mode: file column conflict with DB → validation error
- Introspected schema model is identical to parsed schema model for the same DDL

### Tests Required

- [x] Integration: PostgreSQL — parse schema file → introspect same schema from live DB → verify identical schema model
- [x] Integration: MySQL — parse schema file → introspect same schema from live DB → verify identical schema model
- [x] Integration: SQLite — parse schema file → introspect same schema from live DB → verify identical schema model
- [x] Integration: Include/exclude schema filtering (PostgreSQL)
- [x] Integration: Include/exclude table filtering with wildcards
- [x] Integration: `both` mode — DB base + file ALTER TABLE overlay → correct merged schema
- [x] Integration: `both` mode — file CREATE TABLE on existing DB table → validation error
- [x] Integration: `both` mode — file column conflict → validation error

### Completion Record

- **Date:** 2026-04-09
- **Files created:** `parser/introspect/introspect.go`, `parser/introspect/postgres.go`, `parser/introspect/mysql.go`, `parser/introspect/sqlite.go`, `parser/introspect/merge.go`, `parser/introspect/introspect_integration_test.go`
- **Files modified:** `parser/postgres/postgres.go` (Seed method), `parser/mysql/mysql.go` (Seed method), `parser/sqlite/sqlite.go` (Seed method), `parser/go.mod`, `parser/go.sum`
- **Files deleted:** `parser/introspect/doc.go`
- **Notes:** Three dialect introspectors (PostgreSQL via pg_catalog/information_schema, MySQL via information_schema, SQLite via PRAGMAs). Introspector interface with Name(), Introspect(), Close(). IntrospectionOptions with schema/table include/exclude filters and wildcard support. "Both" mode via Merge function using Seedable interface — seeds parser with introspected schema, parses files, validates column conflicts (duplicate column names with different types). 8 integration tests (10 with subtests) using testcontainers for PostgreSQL and MySQL, file-based SQLite. Seed method added to all three dialect parsers. New dependencies: pgx/v5/stdlib, go-sql-driver/mysql, modernc.org/sqlite, testcontainers-go. Zero lint issues.
