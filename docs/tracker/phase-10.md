# Phase 10: End-to-End Tests

Status: In Progress
PRD Sections: 7.2, 7.4, 7.5, 8.1, 9, 13, 16, 17

> **Testing E2E examples:** Each example is its own Go module under `cmd/sqlgen/testdata/examples/`.
> `make check` only covers the main workspace modules — it does **not** run example tests.
> After implementing or modifying example tests, run:
> ```bash
> make check-examples    # lint + test all example modules (requires Docker)
> ```

## 10.1 E2E Test Harness — shared test infrastructure and directory structure

**PRD Reference:** Section 8.1.7

**Status:** Complete

### Tasks

- [x] Create `testdata/examples/` directory structure with README explaining conventions
- [x] Implement shared test harness in `e2e_test.go` that discovers example dirs, runs `cli.NewRootCmd()` generate, diffs against `expected/`, and verifies compilation
- [x] Implement golden file diff helper: compare generated output against `expected/` directory, fail on any difference with clear diff output
- [x] Implement `TestMain` or test helper for testcontainers PostgreSQL/MySQL pools and SQLite in-memory setup *(deferred — completed as part of 10.2)*
- [x] Add `update-golden-e2e` Makefile target for E2E examples (regenerate `expected/` dirs when templates change intentionally)
- [x] Ensure all E2E tests skip when `testing.Short()` is true

### Acceptance Criteria

- `testdata/examples/` directory exists with documented conventions
- Test harness can discover example directories, each containing `sqlgen.yml` and `schema.sql`
- `make update-golden` regenerates golden files for E2E examples
- Running `go test -short` skips all E2E tests
- Golden file diff produces clear, actionable failure messages

### Tests Required

- [x] Harness discovers example directories correctly
- [x] Golden file diff detects added, removed, and changed files
- [x] `testing.Short()` skips E2E tests

### Completion Record

- **Files created:** `cmd/sqlgen/e2e_test.go`, `cmd/sqlgen/testdata/examples/README.md`
- **Files modified:** `Makefile` (added `update-golden-e2e` target)
- **Date completed:** 2026-04-14
- **Notes:** Testcontainers helpers deferred to 10.2 where they are first needed. Compile verification step deemed unnecessary (goimports in generation pipeline + runtime tests cover it). FIX-001 logged and resolved.

---

## 10.2 `postgres` Example — comprehensive PostgreSQL scenario

**PRD Reference:** Sections 7.2, 8.1, 9, 13

**Status:** Complete

### Tasks

- [x] Create `testdata/examples/postgres/sqlgen.yml` config (pgx driver, PostgreSQL dialect)
- [x] Create `testdata/examples/postgres/schema.sql` with public schema tables (users, profiles, categories, products, orders, order_items, user_categories), audit schema tables (audit.users, audit.events), user_role/order_status enums, address composite type, email/positive_int domain types, `COMMENT ON` for all tables/columns/enums/types
- [x] Run generation and commit golden files to `testdata/examples/postgres/expected/`
- [x] Write `testdata/examples/postgres/tests/crud_test.go` — CRUD on each table (Create, Get, Update, Delete)
- [x] Write tests for enum field read/write round-trip (user_role, order_status)
- [x] Write tests for relationship loading: O2O (profile from user), O2M (orders from user, products from category), M2M (users ↔ categories)
- [x] Write tests for filtering with comparators on various types
- [x] Write tests for array column operations (text[], integer[])
- [x] Write tests for JSONB column operations
- [x] Write tests for domain type columns (email, positive_int behave as base type)
- [x] Write tests for composite type columns (address)
- [x] Write tests for offset pagination (limit + offset)
- [x] Write tests for cursor pagination (forward/backward, page info, connection types)

### Acceptance Criteria

- Schema includes all specified tables, enums, composite type, and domain types
- Schema includes `COMMENT ON` statements for all tables, columns, enums, composite types, and domains
- Generated struct and field comments reflect SQL comments
- Generated code compiles cleanly and matches golden files
- All CRUD operations work against a real PostgreSQL database (testcontainers)
- O2O, O2M, and M2M relationships load correctly
- Enum, array, JSONB, domain, and composite type values round-trip correctly
- Both offset and cursor pagination produce correct results

### Tests Required

- [x] CRUD on users, profiles, categories, products, orders, order_items
- [x] Enum read/write round-trip (user_role, order_status)
- [x] O2O: load profile from user
- [x] O2M: load orders from user, products from category, order_items from order
- [x] M2M: load categories for user via user_categories
- [x] Comparator filtering on text, integer, boolean, timestamptz, numeric types
- [x] Array column read/write (text[], integer[])
- [x] JSONB column read/write
- [x] Domain type columns behave as base type
- [x] Composite type column read/write
- [x] Offset pagination with limit + offset
- [x] Cursor pagination forward/backward with page info
- [x] Multi-schema: CRUD on audit.users (AuditUser) and audit.events (Event)
- [x] Multi-schema: verify public.users and audit.users are isolated (no cross-contamination)

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/postgres/sqlgen.yml`, `schema.sql`, `tests/crud_test.go`, `go.mod`
- **Files modified:** `cmd/sqlgen/e2e_test.go`, `cmd/sqlgen/gen/orchestrate.go`, `cmd/sqlgen/gen/context_table.go`, `cmd/sqlgen/gen/context_client.go`, `cmd/sqlgen/gen/context.go`, `cmd/sqlgen/gotype/gotype.go`, `cmd/sqlgen/cli/pipeline.go`, `cmd/sqlgen/gen/templates/table/pagination.go.tmpl`, `cmd/sqlgen/gen/templates/view/pagination.go.tmpl`, `cmd/sqlgen/gen/templates/table/get.go.tmpl`, `cmd/sqlgen/gen/templates/table/relationships.go.tmpl`, `cmd/sqlgen/gen/templates/shared/_field_options.tmpl`, `cmd/sqlgen/gen/templates/client.go.tmpl`, `sql/condition.go`
- **Golden files generated:** 10 files in `expected/`
- **Date completed:** 2026-04-14
- **Notes:** All 12 E2E tests pass against real PostgreSQL via testcontainers. Fixed multiple template-level bugs exposed by this comprehensive scenario: cursor variable shadowing (FIX-014), composite PK strategy (FIX-015), schema wildcard passthrough (FIX-016), qualified relationship names (FIX-017), chained O2O FieldOptions path (FIX-018), cross-client wiring for O2M/M2M (FIX-019), O2O JOIN ambiguous columns (FIX-020), FieldOptions column sorting (FIX-021), O2M FK field selection (FIX-022), M2M junction column order (FIX-023).

---

## 10.3 `mysql` Example — comprehensive MySQL scenario

**PRD Reference:** Sections 7.2, 8.1, 9, 13

**Status:** Complete

### Tasks

- [x] Create `testdata/examples/mysql/sqlgen.yml` config (mysql driver, MySQL dialect)
- [x] Create `testdata/examples/mysql/schema.sql` with users, profiles, categories, products, orders, order_items, user_categories tables; inline ENUMs; inline `COMMENT` on tables and columns
- [x] Run generation and commit golden files to `testdata/examples/mysql/expected/`
- [x] Write `testdata/examples/mysql/tests/crud_test.go` — CRUD on each table
- [x] Write tests for inline enum read/write round-trip
- [x] Write tests for relationship loading (O2O, O2M, M2M)
- [x] Write tests for unsigned integer types (BIGINT UNSIGNED)
- [x] Write tests for JSON column operations
- [x] Write tests for ON DUPLICATE KEY UPDATE upsert behavior
- [x] Write tests for offset and cursor pagination

### Acceptance Criteria

- Schema uses MySQL-idiomatic types (VARCHAR, DECIMAL, FLOAT, DOUBLE, TINYINT, MEDIUMINT, BIGINT UNSIGNED, DATE, DATETIME, BLOB, inline ENUM)
- Schema includes inline `COMMENT` on tables and columns
- Generated struct and field comments reflect SQL comments
- Generated code uses backtick quoting and `?` placeholders
- All CRUD operations work against a real MySQL database (testcontainers)
- Inline enums round-trip correctly
- ON DUPLICATE KEY UPDATE upsert works correctly
- Unsigned integer types map to correct Go types

### Tests Required

- [x] CRUD on users, profiles, categories, products, orders, order_items
- [x] Inline ENUM read/write round-trip
- [x] O2O: load profile from user
- [x] O2M: load orders from user, products from category
- [x] M2M: load categories for user
- [x] Unsigned integer type handling (BIGINT UNSIGNED)
- [x] JSON column read/write
- [x] Upsert via ON DUPLICATE KEY UPDATE
- [x] Offset pagination
- [x] Cursor pagination

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/mysql/sqlgen.yml`, `schema.sql`, `tests/crud_test.go`, `go.mod`; `types/json.go` (runtime JSONMap type)
- **Files modified:** `cmd/sqlgen/gotype/gotype.go` (JSON → types.JSONMap), `cmd/sqlgen/gen/funcmap.go` (pkFilterExpr functions), `cmd/sqlgen/gen/context.go` (FKGoType field), `cmd/sqlgen/gen/context_table.go` (wireRelationshipFKGoTypes), `cmd/sqlgen/gen/templates/table/get.go.tmpl`, `cmd/sqlgen/gen/templates/table/create.go.tmpl`, `cmd/sqlgen/gen/templates/table/update.go.tmpl`, `cmd/sqlgen/gen/templates/table/delete.go.tmpl`, `cmd/sqlgen/gen/templates/view/get.go.tmpl` (integer PK comparator fix)
- **Golden files generated:** 9 files in mysql `expected/`; postgres `expected/` regenerated
- **Date completed:** 2026-04-15
- **Notes:** Fixed FIX-031 (integer PK comparator type mismatch) and FIX-032 (JSON column stdlib handling via types.JSONMap). All 17 MySQL E2E tests pass against real MySQL via testcontainers. FIX-029 (inline enum auto-apply) and FIX-030 (UNIQUE ConflictTargets) deferred.

---

## 10.3a Batch & Filter CRUD Coverage — postgres and mysql examples

**PRD Reference:** Section 9 (Generated Client Operations)

**Status:** Complete

### Tasks

- [x] Add `TestCreateMany` to postgres `crud_test.go` — batch insert multiple rows, verify all returned with correct values
- [x] Add `TestUpdateMany` to postgres `crud_test.go` — update multiple rows by PK with different inputs, verify each updated correctly
- [x] Add `TestUpdateWhere` to postgres `crud_test.go` — update rows matching a filter, verify only matching rows changed
- [x] Add `TestHardDeleteMany` to postgres `crud_test.go` — delete multiple rows by PKs, verify all removed
- [x] Add `TestHardDeleteWhere` to postgres `crud_test.go` — delete rows matching a filter, verify only matching rows removed
- [x] Add `TestExistsWhere` to postgres `crud_test.go` — verify true for matching filter, false for non-matching
- [x] Add `TestCount` to postgres `crud_test.go` — verify correct count with filter, zero count with non-matching filter
- [x] Add `TestCreateMany` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestUpdateMany` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestUpdateWhere` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestHardDeleteMany` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestHardDeleteWhere` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestExistsWhere` to mysql `crud_test.go` — same coverage as postgres
- [x] Add `TestCount` to mysql `crud_test.go` — same coverage as postgres

### Acceptance Criteria

- Every generated batch/filter client method has at least one E2E test per dialect (PostgreSQL, MySQL)
- CreateMany returns all created entities with correct values and auto-generated PKs
- UpdateMany updates each row independently and returns updated entities
- UpdateWhere only modifies rows matching the filter and returns them
- HardDeleteMany removes exactly the specified PKs
- HardDeleteWhere removes exactly the rows matching the filter
- ExistsWhere returns true/false correctly based on filter match
- Count returns accurate counts with and without matching rows

### Tests Required

- [x] CreateMany: batch insert 3+ rows, verify returned count and field values (postgres)
- [x] CreateMany: batch insert 3+ rows, verify returned count and field values (mysql)
- [x] UpdateMany: update 2+ rows with different inputs, verify each (postgres)
- [x] UpdateMany: update 2+ rows with different inputs, verify each (mysql)
- [x] UpdateWhere: update by filter, verify only matching rows changed (postgres)
- [x] UpdateWhere: update by filter, verify only matching rows changed (mysql)
- [x] HardDeleteMany: delete 2+ PKs, verify removed and others intact (postgres)
- [x] HardDeleteMany: delete 2+ PKs, verify removed and others intact (mysql)
- [x] HardDeleteWhere: delete by filter, verify only matching removed (postgres)
- [x] HardDeleteWhere: delete by filter, verify only matching removed (mysql)
- [x] ExistsWhere: true for matching filter, false for non-matching (postgres)
- [x] ExistsWhere: true for matching filter, false for non-matching (mysql)
- [x] Count: correct count with filter, zero with non-matching (postgres)
- [x] Count: correct count with filter, zero with non-matching (mysql)

### Completion Record

- **Files modified:** `cmd/sqlgen/testdata/examples/postgres/tests/crud_test.go`, `cmd/sqlgen/testdata/examples/mysql/tests/crud_test.go`
- **Date completed:** 2026-04-15
- **Notes:** Added 7 tests per dialect (TestCreateMany, TestUpdateMany, TestUpdateWhere, TestHardDeleteMany, TestHardDeleteWhere, TestExistsWhere, TestCount). All use categories table for simplicity. Tests use ID-based filters and distinct names to avoid UNIQUE constraint violations on `categories.name`. All pass via `make check-examples`.

---

## 10.4 `sqlite` Example — SQLite type affinity coverage

**PRD Reference:** Sections 7.2, 8.1, 9, 13

**Status:** Complete

### Tasks

- [x] Create `testdata/examples/sqlite/sqlgen.yml` config (sqlite driver, SQLite dialect)
- [x] Create `testdata/examples/sqlite/schema.sql` with users, profiles, categories, products, orders, order_items, user_categories tables using SQLite type affinities
- [x] Run generation and commit golden files to `testdata/examples/sqlite/expected/`
- [x] Write `testdata/examples/sqlite/tests/crud_test.go` — single-entity CRUD on each table (Create, Get, Update, HardDelete, Exists)
- [x] Write tests for batch/filter CRUD operations (CreateMany, UpdateMany, UpdateWhere, HardDeleteMany, HardDeleteWhere, ExistsWhere, Count)
- [x] Write tests for relationship loading (O2O, O2M, M2M)
- [x] Write tests for BOOLEAN stored as INTEGER round-trip
- [x] Write tests for RETURNING clause behavior
- [x] Write tests for ON CONFLICT upsert behavior
- [x] Write tests for offset and cursor pagination

### Acceptance Criteria

- Schema uses all 5 SQLite type affinities: INTEGER, TEXT, REAL, BLOB, BOOLEAN
- Generated code uses `?` placeholders and `"double"` quoting
- All CRUD operations work against an in-memory SQLite database (no testcontainers)
- All batch/filter operations (CreateMany, UpdateMany, UpdateWhere, HardDeleteMany, HardDeleteWhere, ExistsWhere, Count) work correctly
- BOOLEAN↔INTEGER round-trip works correctly
- RETURNING clause is used where supported
- ON CONFLICT upsert works correctly

### Tests Required

- [x] CRUD on users, profiles, categories, products, orders, order_items
- [x] CreateMany: batch insert 3+ rows, verify returned count and field values
- [x] UpdateMany: update 2+ rows with different inputs, verify each
- [x] UpdateWhere: update by filter, verify only matching rows changed
- [x] HardDeleteMany: delete 2+ PKs, verify removed and others intact
- [x] HardDeleteWhere: delete by filter, verify only matching removed
- [x] ExistsWhere: true for matching filter, false for non-matching
- [x] Count: correct count with filter, zero with non-matching
- [x] O2O: load profile from user
- [x] O2M: load orders from user, products from category
- [x] M2M: load categories for user
- [x] BOOLEAN stored as INTEGER round-trip
- [x] RETURNING clause behavior
- [x] ON CONFLICT upsert
- [x] Offset pagination
- [x] Cursor pagination

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/sqlite/sqlgen.yml`, `schema.sql`, `tests/crud_test.go`, `go.mod`
- **Golden files generated:** 8 files in sqlite `expected/`
- **Date completed:** 2026-04-15
- **Notes:** All 21 SQLite E2E tests pass against in-memory SQLite (modernc.org/sqlite, no Docker). FIX-033 resolved — `BuildMultiInsert` now emits raw default expressions instead of `DEFAULT` keyword for SQLite. BLOB round-trip test added beyond spec to cover the 5th type affinity. Upsert uses `ON CONFLICT (sku) DO UPDATE SET` with ProductConflictSku target, verifying same-row update on conflict.

---

## 10.5 Soft Delete — timestamp and boolean strategies across all dialects

**PRD Reference:** Sections 8.1, 17

**Status:** Complete

> **Approach:** Extend the existing postgres, mysql, and sqlite examples with new soft-delete tables
> rather than creating a standalone example. This ensures soft delete is tested against all three
> dialects. New tables are added to avoid changing existing table behavior and breaking prior tests.

### Tasks

**Schema & config changes (all three dialects):**
- [x] Add `articles` table with `deleted_at` column (timestamp soft delete) to postgres `schema.sql`
- [x] Add `tags` table with `is_deleted` column (boolean soft delete) to postgres `schema.sql`
- [x] Add equivalent `articles` and `tags` tables to mysql `schema.sql` (dialect-appropriate types)
- [x] Add equivalent `articles` and `tags` tables to sqlite `schema.sql` (dialect-appropriate types)
- [x] Add `generation.soft_delete_columns` config to all three `sqlgen.yml` files
- [x] Add table config entries for `articles` and `tags` in all three `sqlgen.yml` files
- [x] Regenerate golden files for all three dialects (`make update-golden-e2e`)

**Tests — PostgreSQL:**
- [x] Timestamp soft delete: `SoftDelete` sets `deleted_at` to current time
- [x] Timestamp soft delete: `GetMany` excludes soft-deleted rows by default
- [x] Timestamp soft delete: `Restore` clears `deleted_at` to NULL
- [x] Boolean soft delete: `SoftDelete` sets `is_deleted` to true
- [x] Boolean soft delete: `GetMany` excludes soft-deleted rows by default
- [x] Boolean soft delete: `Restore` sets `is_deleted` to false
- [x] Filter with soft delete comparator overrides default scoping (timestamp)
- [x] `SoftDeleteWhere` with filter conditions (timestamp)
- [x] `RestoreWhere` restores matching soft-deleted rows (boolean)

**Tests — MySQL:**
- [x] Timestamp soft delete: `SoftDelete` / `GetMany` exclusion / `Restore`
- [x] Boolean soft delete: `SoftDelete` / `GetMany` exclusion / `Restore`
- [x] Filter override of default scoping
- [x] `SoftDeleteWhere` / `RestoreWhere`

**Tests — SQLite:**
- [x] Timestamp soft delete: `SoftDelete` / `GetMany` exclusion / `Restore`
- [x] Boolean soft delete: `SoftDelete` / `GetMany` exclusion / `Restore`
- [x] Filter override of default scoping
- [x] `SoftDeleteWhere` / `RestoreWhere`

### Acceptance Criteria

- Timestamp-based soft delete (`deleted_at`) sets column to `CURRENT_TIMESTAMP` and filters with `IS NULL` in all three dialects
- Boolean-based soft delete (`is_deleted`) sets column to `TRUE` and filters with `= FALSE` in all three dialects
- `SoftDelete`, `SoftDeleteMany`, `SoftDeleteWhere` methods are generated for both table types
- `Restore`, `RestoreMany`, `RestoreWhere` methods are generated for both table types
- `GetMany` automatically excludes soft-deleted rows; setting the soft delete comparator overrides this
- Existing tests for other tables remain unaffected (new tables only)

### Tests Required

- [x] Timestamp `SoftDelete` sets column correctly (postgres, mysql, sqlite)
- [x] Timestamp `GetMany` excludes soft-deleted rows (postgres, mysql, sqlite)
- [x] Timestamp `Restore` clears column to NULL (postgres, mysql, sqlite)
- [x] Boolean `SoftDelete` sets column to true (postgres, mysql, sqlite)
- [x] Boolean `GetMany` excludes soft-deleted rows (postgres, mysql, sqlite)
- [x] Boolean `Restore` sets column to false (postgres, mysql, sqlite)
- [x] Filter with soft delete comparator overrides default scoping (postgres, mysql, sqlite)
- [x] `SoftDeleteWhere` with filter conditions (postgres, mysql, sqlite)
- [x] `RestoreWhere` scopes to soft-deleted rows (postgres, mysql, sqlite)

### Completion Record

- **Files modified:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/{schema.sql,sqlgen.yml,tests/crud_test.go}`; `cmd/sqlgen/gen/funcmap.go` (added `softDeleteFilterOverride`); `cmd/sqlgen/gen/templates/table/delete.go.tmpl` (fixed soft delete filter override expression); `cmd/sqlgen/gen/funcmap_test.go`; `cmd/sqlgen/gen/delete_test.go`
- **Golden files regenerated:** All three dialect `expected/` directories updated
- **Date completed:** 2026-04-15
- **Notes:** Fixed pre-existing template bug (FIX-034): generated soft delete re-fetch code used `&*comparator.Type{Null: new(false)}` which was invalid Go (`*` prefix from filter field type leaked into literal). Also fixed `comparator.Bool` missing `Null` field — Bool uses `Eq: new(true)` instead. Integer soft delete uses `Neq: new(0)`. SQLite uses `DATETIME` (not `TEXT`) for `deleted_at` to pass soft_delete_columns type validation. All 6 tests per dialect pass (18 total new E2E tests).

---

## 10.5a Test File Refactor — split large crud_test.go files by feature area

**PRD Reference:** Section 8.1

**Status:** Complete

> **Approach:** The single `crud_test.go` file in each dialect example has grown to 1500-2000+ lines
> and will continue growing with composite PK, views, and type override tests. Split each into
> smaller, focused test files for readability and maintainability.

### Tasks

**All three dialects (postgres, mysql, sqlite):**
- [x] Extract `TestMain`, `newClient`, and shared helpers into `main_test.go`
- [x] Keep single-entity CRUD tests (Create, Get, Update, HardDelete, Exists) in `crud_test.go`
- [x] Extract batch/filter CRUD tests (CreateMany, UpdateMany, UpdateWhere, HardDeleteMany, HardDeleteWhere, ExistsWhere, Count) into `batch_test.go`
- [x] Extract relationship tests (O2O, O2M, M2M) into `relationship_test.go`
- [x] Extract pagination tests (offset, cursor) into `pagination_test.go`
- [x] Extract soft delete tests (SoftDelete, Restore, filter override, SoftDeleteWhere, RestoreWhere) into `soft_delete_test.go`

**Postgres-specific additional splits:**
- [x] Extract comparator filter tests into `filter_test.go`
- [x] Extract type round-trip tests (enum, array, JSONB, domain, composite) into `types_test.go`
- [x] Extract multi-schema tests (audit.users, audit.events) into `multi_schema_test.go`

### Acceptance Criteria

- All existing tests pass without modification to test logic (pure file reorganization)
- Each test file has a clear single responsibility
- `main_test.go` contains only `TestMain`, `newClient`, and shared helpers used across files
- No test is duplicated or lost in the split
- `make check-examples` passes with no regressions

### Tests Required

- [x] `make check-examples` passes after refactor (all existing tests still green)

### Completion Record

- **Files created:** `main_test.go`, `batch_test.go`, `relationship_test.go`, `pagination_test.go`, `soft_delete_test.go` (all 3 dialects); `filter_test.go`, `types_test.go`, `multi_schema_test.go` (postgres only)
- **Files rewritten:** `crud_test.go` (all 3 dialects — reduced from 2127/1777/1686 lines to focused CRUD + dialect-specific tests)
- **Date completed:** 2026-04-16
- **Notes:** Pure file reorganization, no test logic changes. Postgres split into 9 files, MySQL and SQLite into 6 files each. MySQL/SQLite dialect-specific tests (enum, unsigned, JSON, upsert, filter, boolean, RETURNING, blob) kept in `crud_test.go` since they don't have dedicated split targets. All 93 E2E tests pass (35 postgres, 29 mysql, 29 sqlite).

---

## 10.6 Composite PK — composite primary key operations across all dialects

**PRD Reference:** Sections 8.1, 9

**Status:** Complete

> **Approach:** The `user_categories` junction table already has a 2-column composite PK in all
> three dialects. Add a 3-column PK table (`product_tag_labels`) to each dialect and write tests
> exercising Get, Update, Delete, and Upsert with the full composite key.
>
> **Test file:** Create new `tests/composite_pk_test.go` in each dialect.

### Tasks

**Schema changes (all three dialects):**
- [x] Add `product_tag_labels` table with 3-column composite PK to postgres `schema.sql`
- [x] Add equivalent table to mysql `schema.sql`
- [x] Add equivalent table to sqlite `schema.sql`
- [x] Add table config entries in all three `sqlgen.yml` files
- [x] Regenerate golden files for all three dialects

**Tests — PostgreSQL** (`tests/composite_pk_test.go` — new file):
- [x] Get by 2-column composite PK (`user_categories`)
- [x] Get by 3-column composite PK (`product_tag_labels`)
- [x] Update by composite PK (only specified fields updated)
- [x] HardDelete by composite PK
- [x] Upsert with composite PK conflict target
- [x] GetMany with filter on composite PK table

**Tests — MySQL** (`tests/composite_pk_test.go` — new file):
- [x] Get / Update / HardDelete / Upsert by composite PK
- [x] GetMany with filter on composite PK table

**Tests — SQLite** (`tests/composite_pk_test.go` — new file):
- [x] Get / Update / HardDelete / Upsert by composite PK
- [x] GetMany with filter on composite PK table

### Acceptance Criteria

- Generated `{Table}PK` struct includes all PK columns in all three dialects
- Get, Update, Delete, Upsert all require and use the full composite PK
- WHERE clauses include all PK columns with correct dialect-specific placeholders
- Both 2-column and 3-column composite PKs work correctly across all dialects

### Tests Required

- [x] Get by 2-column composite PK (postgres, mysql, sqlite)
- [x] Get by 3-column composite PK (postgres, mysql, sqlite)
- [x] Update by composite PK (postgres, mysql, sqlite)
- [x] HardDelete by composite PK (postgres, mysql, sqlite)
- [x] Upsert with composite PK conflict target (postgres, mysql, sqlite)
- [x] GetMany with filter on composite PK table (postgres, mysql, sqlite)

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/composite_pk_test.go`
- **Files modified:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/{schema.sql,sqlgen.yml}`
- **Golden files regenerated:** All three dialect `expected/` directories updated
- **Date completed:** 2026-04-16
- **Notes:** Added `product_tag_labels` table with 3-column composite PK (product_id, tag_name, label) and a non-PK `description` column for update testing. 6 tests per dialect (18 total new E2E tests). All pass via `make check-examples`.

---

## 10.7 Views — view generation with annotations across all dialects

**PRD Reference:** Sections 8.1, 16

**Status:** Complete

> **Approach:** Add view annotation files and `input.views` config to all three dialect examples.
> Each dialect gets a `product_summary` view over the existing `products` table, testing `@pk`,
> `@type`, and `@nullable` directives.
>
> **Test file:** Create new `tests/views_test.go` in each dialect.

### Tasks

**Config & view file changes (all three dialects):**
- [x] Create `views/product_summary.sql` view annotation file for postgres (with `@pk`, `@type`, `@nullable`)
- [x] Create equivalent view file for mysql
- [x] Create equivalent view file for sqlite
- [x] Add `input.views` config to all three `sqlgen.yml` files
- [x] Regenerate golden files for all three dialects

**Tests — PostgreSQL** (`tests/views_test.go` — new file):
- [x] View Get by PK (when `@pk` is set)
- [x] View GetMany with filters
- [x] View Count
- [x] View Paginate (offset)
- [x] View Connection (cursor)
- [x] Aggregate column type from `@type` annotation
- [x] Nullable column from `@nullable` directive
- [x] No mutation methods generated for views

**Tests — MySQL** (`tests/views_test.go` — new file):
- [x] View Get / GetMany / Count / Paginate / Connection
- [x] `@type` and `@nullable` directives work correctly

**Tests — SQLite** (`tests/views_test.go` — new file):
- [x] View Get / GetMany / Count / Paginate / Connection
- [x] `@type` and `@nullable` directives work correctly

### Acceptance Criteria

- View client is read-only in all dialects: Get, GetMany, Count, Paginate, Connection — no Create/Update/Delete
- `@pk` enables the Get method; without it only GetMany/Count/pagination are available
- `@type` overrides produce correct Go types and imports
- `@nullable` forces columns to nullable variants
- Column types resolve correctly from source tables via schema matching
- View queries execute successfully against real databases in all three dialects

### Tests Required

- [x] View Get by PK (postgres, mysql, sqlite)
- [x] View GetMany with filters (postgres, mysql, sqlite)
- [x] View Count (postgres, mysql, sqlite)
- [x] View Paginate offset (postgres, mysql, sqlite)
- [x] View Connection cursor (postgres, mysql, sqlite)
- [x] `@type` annotation produces correct Go type (postgres, mysql, sqlite)
- [x] `@nullable` directive forces nullable variant (postgres, mysql, sqlite)
- [x] No mutation methods generated (postgres, mysql, sqlite)

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/views/{product_summary,category_stats}.sql`, `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/views_test.go`
- **Files modified:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/{sqlgen.yml,tests/main_test.go}`; `parser/schema.go` (added GoTypeLiteral/GoTypeImport fields to Column); `parser/view.go` (set GoTypeLiteral for @type/aggregate columns, fixed MIN/MAX `*` prefix, fixed SUM/MIN/MAX GoTypeLiteral gate); `cmd/sqlgen/gotype/gotype.go` (exported FromLiteral, added `any`/`json.RawMessage` to knownGoTypes); `cmd/sqlgen/gen/context_view.go` (use FromLiteral for pre-resolved Go types); `cmd/sqlgen/gen/orchestrate.go` (added shared/filter, shared/field-options, shared/get-input to viewTemplateNames); `parser/view_test.go` (updated MIN/MAX expectations)
- **Golden files regenerated:** All three dialect `expected/` directories updated with `views_gen.go`
- **Date completed:** 2026-04-16
- **Notes:** Two views per dialect: `product_summary` (with `@pk`, tests Get + read-only client) and `category_stats` (no `@pk`, tests comprehensive aggregate inference: COUNT, SUM, AVG, MIN, MAX, STRING_AGG, BOOL_OR, JSON_AGG for postgres; GROUP_CONCAT, JSON_ARRAYAGG for mysql; GROUP_CONCAT for sqlite). Fixed four pre-existing bugs: (FIX-035) view @type/aggregate columns resolved to `string` via wrong resolver path; (FIX-036) missing shared templates for view filter/field-options/get-input types; (FIX-037) MIN/MAX prepended `*` to SQL types making them unresolvable; (FIX-038) `json.RawMessage` and `any` missing from knownGoTypes. All 30 view E2E tests pass (12 postgres, 9 mysql, 9 sqlite).

---

## 10.8 Type Overrides — table-level `overrides.types` across dialects

**PRD Reference:** Sections 4.7, 4.8, 7.4, 7.5, 8.1

**Status:** Complete

> **Approach:** Use `tables.<table>.overrides.types` (per PRD 4.8) on a specific table in each
> dialect to override SQL-to-Go type mappings without affecting other tables. For postgres, override
> `uuid` → `uuid.UUID` (google/uuid) and `numeric` → `decimal.Decimal` (shopspring/decimal) on a
> single table. MySQL and SQLite get equivalent table-level overrides where applicable. This keeps
> existing tests untouched since only the targeted table's generated types change.
>
> **Test file:** Create new `tests/type_overrides_test.go` in each dialect.

### Tasks

**Postgres:**
- [x] Add `tables.<table>.overrides.types` for a specific table in postgres `sqlgen.yml` (e.g., map `uuid` → `uuid.UUID`, `numeric` → `decimal.Decimal`)
- [x] Add a custom (non-built-in) type override: map a `text` column to `ksuid.KSUID` (`github.com/segmentio/ksuid`) to verify the generic `overrides.types` path works for types with no built-in integration
- [x] Add `google/uuid`, `shopspring/decimal`, and `segmentio/ksuid` dependencies to postgres `go.mod`
- [x] Regenerate golden files — verify only the overridden table uses `uuid.UUID` / `decimal.Decimal` / `ksuid.KSUID`
- [x] Write tests in `tests/type_overrides_test.go` (new file):
  - [x] UUID round-trip: create with `uuid.UUID`, get returns same value
  - [x] Decimal round-trip: create with `decimal.Decimal`, precision preserved
  - [x] KSUID round-trip: create with `ksuid.KSUID`, get returns same value
  - [x] `uuid[]` array column mapping to `[]uuid.UUID` (if applicable on the overridden table)
  - [x] Nullable UUID and nullable decimal variants

**MySQL:**
- [x] Add `tables.<table>.overrides.types` for a specific table (e.g., map DECIMAL columns, and a `text`/`varchar` column to `ksuid.KSUID`)
- [x] Write tests in `tests/type_overrides_test.go` (new file):
  - [x] Table-level override round-trip (built-in integration type)
  - [x] KSUID round-trip (custom non-built-in type)

**SQLite:**
- [x] Add `tables.<table>.overrides.types` for a specific table (e.g., map REAL columns, and a `text` column to `ksuid.KSUID`)
- [x] Write tests in `tests/type_overrides_test.go` (new file):
  - [x] Table-level override round-trip (built-in integration type)
  - [x] KSUID round-trip (custom non-built-in type)

### Acceptance Criteria

- Only the targeted table(s) use overridden types — other tables remain unchanged
- UUID columns on the overridden table generate as `uuid.UUID` (google/uuid) instead of `string`
- Decimal/numeric columns on the overridden table generate as `decimal.Decimal` (shopspring/decimal) instead of `float64`
- KSUID columns on the overridden table generate as `ksuid.KSUID` (segmentio/ksuid) instead of `string` — validates the generic `overrides.types` path works for types with no built-in integration
- Generated code includes correct import paths for the overridden table
- Scan/Value methods work correctly for overridden types against real databases
- Existing tests for non-overridden tables are unaffected

### Tests Required

- [x] UUID override round-trip: create with `uuid.UUID`, get returns same UUID (postgres)
- [x] Decimal override round-trip: create with `decimal.Decimal`, precision preserved (postgres)
- [x] KSUID override round-trip: create with `ksuid.KSUID`, get returns same value (postgres)
- [x] UUID array column maps to `[]uuid.UUID` on overridden table (postgres)
- [x] Nullable UUID and nullable decimal variants work (postgres)
- [x] Table-level override round-trip — built-in integration type (mysql, sqlite)
- [x] KSUID override round-trip — custom non-built-in type (mysql, sqlite)
- [x] Non-overridden tables still use default types (all dialects)

### Completion Record

- **Files created:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/type_overrides_test.go`
- **Files modified:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/{schema.sql,sqlgen.yml,go.mod,go.sum}`; `cmd/sqlgen/gotype/gotype.go` (added `lookupOverride` helper with modifier stripping for override lookups); `cmd/sqlgen/gen/context_table.go` (fixed `isNumericGoType` to exclude `decimal.Decimal` struct types; fixed `resolveSimpleComparator` to use `comparator.ID` for PK/FK columns with FKConvert)
- **Golden files regenerated:** All three dialect `expected/` directories updated with warehouse table code
- **Date completed:** 2026-04-16
- **Notes:** Added `warehouses` table to all three dialects for type override testing. Postgres overrides: `uuid` → `uuid.UUID`, `numeric` → `decimal.Decimal`, `text` → `ksuid.KSUID`. MySQL overrides: `decimal` → `decimal.Decimal`, `text` → `ksuid.KSUID`. SQLite overrides: `real` → `decimal.Decimal`, `varchar` → `ksuid.KSUID`. Fixed three pre-existing bugs: (FIX-039) `resolveByType` did not strip SQL type modifiers (e.g., `numeric(10,2)`) when looking up overrides — only `builtinResolve` did; (FIX-040) `isNumericGoType` treated `decimal.Decimal`/`decimal.NullDecimal` as Go numeric primitives, but they are structs that don't satisfy the `comparator.Numeric` type constraint; (FIX-041) `resolveSimpleComparator` only matched `string` base types for `comparator.ID`, missing overridden PK types like `uuid.UUID` that have FK string conversion. All 5 postgres, 4 mysql, and 4 sqlite type override E2E tests pass. Total: 13 new E2E tests.

---

## 10.9 Enum Arrays & SET Filtering — dialect-specific collection types

**PRD Reference:** Sections 7.2, 9, 11.2, 13

**Status:** Complete

> **Approach:** PostgreSQL supports enum array columns (`user_role[]`) that resolve to named slice
> types (e.g., `UserRoleSlice`) with `Scan`/`Value` methods for PostgreSQL array text format, and
> use `comparator.Slice[UserRole]` for filtering via native array operators (`&&`, `@>`, `<@`).
> Named slice types are needed because custom enum types have dynamically assigned OIDs that pgx
> cannot resolve without manual type registration. MySQL SET columns (`SET('read','write','delete','admin')`)
> resolve to named `[]ValueType` slices and use `comparator.String` for filtering.

### Tasks

**Generator — enum slice types (PostgreSQL only):**
- [x] Add `SliceGoTypeName` field to `EnumContext` in `gen/context.go`
- [x] Add `Dialect` field to `EnumFileContext`
- [x] Update `BuildEnumContexts` to accept dialect and set `SliceGoTypeName`
- [x] Add named slice type generation to `enum.go.tmpl` (String, Value, Scan methods)
- [x] Update `gotype.go` to resolve `enum[]` to named slice type (e.g., `UserRoleSlice`)
- [x] Add `SliceElemType` to `GoType` and `ColumnContext` for comparator resolution
- [x] Update scan shape logic to skip `pq.Array` wrapping for named enum slices
- [x] Update `comparator.Slice` to encode values as PostgreSQL array text literals
- [x] Update PRD.md and TEMPLATES.md documentation

**PostgreSQL — enum array column:**
- [x] Add `allowed_roles user_role[]` column to the `warehouses` table in postgres `schema.sql`
- [x] Add `COMMENT ON COLUMN` for the new column
- [x] Regenerate golden files — verify `AllowedRoles` is `UserRoleSlice` with `comparator.Slice[UserRole]`
- [x] Write tests in `tests/type_overrides_test.go`:
  - [x] Create with enum array, get returns same values
  - [x] Update enum array column
  - [x] Filter `ContainsAny`: match rows containing any of the given enum values
  - [x] Filter `ContainsAll`: match rows containing all of the given enum values
  - [x] Filter `IsEmpty`: match rows with empty vs non-empty arrays

**MySQL — SET column CRUD and filtering:**
- [x] Write tests in `tests/set_test.go`:
  - [x] Create user with SET permissions, get returns same values
  - [x] Update SET permissions
  - [x] Filter SET column with `comparator.String` Eq (exact match)
  - [x] Filter SET column with `comparator.String` Contains (substring match for multi-value SETs)

### Acceptance Criteria

- PostgreSQL enum array column (`user_role[]`) generates as `UserRoleSlice` (named slice type)
- Named slice type has `Scan`/`Value` methods for PostgreSQL array text format
- Enum array uses `comparator.Slice[UserRole]` (not `comparator.Slice[string]`) in the filter
- Enum array CRUD round-trips correctly against real PostgreSQL (create, get, update)
- Enum array filtering with `ContainsAny`, `ContainsAll`, and `IsEmpty` works correctly
- MySQL SET column round-trips correctly (create, get, update)
- MySQL SET column filtering works via `comparator.String`
- Existing tests are unaffected

### Tests Required

- [x] Enum array create + get round-trip (postgres)
- [x] Enum array update (postgres)
- [x] Enum array `ContainsAny` filter (postgres)
- [x] Enum array `ContainsAll` filter (postgres)
- [x] Enum array `IsEmpty` filter (postgres)
- [x] SET create + get round-trip (mysql)
- [x] SET update (mysql)
- [x] SET filter Eq exact match (mysql)
- [x] SET filter Contains substring match (mysql)

### Completion Record

**Date completed:** 2026-04-16

**Files changed:**
- `comparator/slice.go` — encode values as PostgreSQL array text literals
- `comparator/slice_test.go` — update expected values to string literals
- `cmd/sqlgen/gotype/gotype.go` — add `SliceElemType` field, resolve enum[] to named slice
- `cmd/sqlgen/gen/context.go` — add `SliceGoTypeName` to EnumContext, `Dialect` to EnumFileContext, `SliceElemType` to ColumnContext
- `cmd/sqlgen/gen/context_enum.go` — accept dialect, set `SliceGoTypeName` for postgres
- `cmd/sqlgen/gen/context_table.go` — propagate `SliceElemType`, skip pq.Array for named slices, add `github.com/lib/pq` import for stdlib+postgres bare slices
- `cmd/sqlgen/gen/context_test.go` — pass dialect to BuildEnumContexts
- `cmd/sqlgen/gen/orchestrate.go` — pass dialect and "strings" import for postgres
- `cmd/sqlgen/gen/templates/enum.go.tmpl` — add named slice type generation block
- `cmd/sqlgen/testdata/examples/postgres/schema.sql` — add `allowed_roles` column
- `cmd/sqlgen/testdata/examples/postgres/tests/type_overrides_test.go` — 5 enum array tests
- `cmd/sqlgen/testdata/examples/mysql/tests/set_test.go` — 4 SET column tests
- `docs/PRD.md` — document enum slice types
- `guidelines/TEMPLATES.md` — update EnumContext description

## 10.10 `postgres_stdlib` Example — PostgreSQL with `lib/pq` stdlib driver

**PRD Reference:** Section 8.1.7

**Status:** Complete

> Validates that generated code works with `database/sql` + `github.com/lib/pq` (pure stdlib)
> instead of `pgx`. Uses the same schema as the `postgres` example. The key difference is
> batch operations use sequential exec instead of `pgx.Batch` pipelining.

### Tasks

- [x] Create `testdata/examples/postgres_stdlib/` directory
- [x] Copy `schema.sql` from `postgres/` example (same schema, same tables)
- [x] Copy `views/` directory from `postgres/` example
- [x] Create `sqlgen.yml` with `dialect: postgres`, `driver: stdlib`
- [x] Configure same `tables:` section as `postgres/` (relationships, type_map, overrides, cursor_keys)
- [x] Generate expected golden files (`expected/` directory)
- [x] Verify golden files compile and contain no `pgx` imports
- [x] Create `tests/main_test.go` using testcontainers postgres + `database/sql` + `github.com/lib/pq`
- [x] Create `go.mod` with `lib/pq` dependency (no pgx)
- [x] Port CRUD tests from `postgres/tests/` adapted for stdlib (same logic, `lib/pq` driver)
- [x] Port relationship, pagination, batch, soft delete, composite PK, views, and type override tests
- [x] Verify all tests pass with `make check-examples`

### Acceptance Criteria

- Example generates valid Go code with `driver: stdlib` — no `pgx` imports in generated output
- All CRUD, relationship, pagination, batch, soft delete, composite PK, view, and override tests pass
- Batch operations use sequential exec (no `pgx.Batch`/`SendBatch`)
- Golden file diff passes in CI
- `lib/pq` is the only postgres driver dependency in the example's `go.mod`

### Tests Required

- [x] Golden file generation matches expected output
- [x] Basic CRUD (create, get, update, hard delete) for users, products, orders
- [x] O2O relationship loading (users → profiles)
- [x] O2M/M2M relationship loading
- [x] Cursor pagination (forward + backward)
- [x] Batch create and batch update
- [x] Soft delete (timestamp + boolean strategies)
- [x] Composite PK operations
- [x] View queries (product_summary, category_stats)
- [x] Type overrides (uuid.UUID, decimal.Decimal, ksuid.KSUID)
- [x] Enum/enum array handling via `lib/pq`

### Completion Record

**Date completed:** 2026-04-16

**Files created:**
- `cmd/sqlgen/testdata/examples/postgres_stdlib/sqlgen.yml` — config with `dialect: postgres`, `driver: stdlib`
- `cmd/sqlgen/testdata/examples/postgres_stdlib/schema.sql` — copied from postgres example
- `cmd/sqlgen/testdata/examples/postgres_stdlib/views/` — copied from postgres example
- `cmd/sqlgen/testdata/examples/postgres_stdlib/go.mod` — `lib/pq` dependency, no pgx
- `cmd/sqlgen/testdata/examples/postgres_stdlib/go.sum`
- `cmd/sqlgen/testdata/examples/postgres_stdlib/expected/` — 11 golden files (no pgx imports)
- `cmd/sqlgen/testdata/examples/postgres_stdlib/models/` — generated code for test compilation
- `cmd/sqlgen/testdata/examples/postgres_stdlib/tests/main_test.go` — testcontainers postgres + `database/sql` + `lib/pq`
- `cmd/sqlgen/testdata/examples/postgres_stdlib/tests/` — 12 test files ported from postgres example

## 10.11 SQLite `DateTime` Custom Type — treat DATETIME columns as `time.Time`

**PRD Reference:** Section 8.1.7, 13

**Status:** Complete

> SQLite has no native date/time storage class — values are stored as TEXT. Previously all
> datetime types (`datetime`, `timestamp`, `date`) mapped to `string` in generated code.
> This task added `types.DateTime` / `types.NullDateTime` custom types that wrap `time.Time`
> with `sql.Scanner` and `driver.Valuer` implementations for SQLite text↔time round-tripping.
> DateTime is registered as a built-in integration (same pattern as uuid.NullUUID / decimal.NullDecimal)
> so nullable columns always use `types.NullDateTime` regardless of `usePointers`.
>
> **Design decisions:**
> - `DateTime` embeds `time.Time` (all time.Time methods available directly)
> - `NullDateTime` has `Time time.Time` + `Valid bool` fields
> - Scan handles: `string`, `[]byte`, `int64` (Unix epoch), `float64` (Julian day), `time.Time` (pre-parsed by driver), `nil`
> - Built-in layouts: `"2006-01-02 15:04:05"`, RFC3339, RFC3339Nano, datetime with fractional seconds, datetime with timezone, date-only
> - `RegisterLayout(layout string)` — adds custom parse layouts (thread-safe via `sync.RWMutex`)
> - `SetOutputLayout(layout string)` — configures Value() output format (default: `time.RFC3339Nano`)
> - Julian day float64 → time conversion: `time.Unix(int64((jd - 2440587.5) * 86400), 0)`
> - Registered as a dialect-default integration in `registerDialectDefaults()` (same path as uuid/decimal), not via built-in type mappings

### Tasks

- [x] Add `types.DateTime` type to `types/` package — embeds `time.Time`, implements `sql.Scanner` (string, []byte, int64 Unix epoch, float64 Julian day, time.Time, nil), implements `driver.Valuer` (marshals via configurable output layout, default RFC3339Nano)
- [x] Add `types.NullDateTime` type — `Time time.Time` + `Valid bool`, implements `sql.Scanner` and `driver.Valuer`
- [x] Add `RegisterLayout(layout string)` — thread-safe (sync.RWMutex) function to append custom parse layouts
- [x] Add `SetOutputLayout(layout string)` — thread-safe function to set Value() output format
- [x] Add unit tests for `DateTime` and `NullDateTime` — scan from string (all built-in layouts), scan from []byte, scan from int64 (Unix epoch), scan from float64 (Julian day), scan from time.Time (pre-parsed), scan from nil, Value round-trip, RegisterLayout custom format, SetOutputLayout custom output
- [x] Register DateTime as a built-in integration for SQLite dialect in `registerDialectDefaults()` — datetime, timestamp, timestamptz, date resolve to `types.DateTime` / `types.NullDateTime`
- [x] Update SQLite schema `created_at`, `updated_at`, `ordered_at` columns from `TEXT` to `DATETIME` to exercise types.DateTime
- [x] Regenerate SQLite example golden files (`expected/` directory)
- [x] Verify SQLite example models use `types.DateTime` (non-null) and `types.NullDateTime` (nullable)
- [x] Update soft delete tests — `deleted_at` uses `types.NullDateTime` with `.Valid` checks
- [x] Update CRUD tests — `created_at` uses `.IsZero()` instead of `== ""`
- [x] Verify all example tests pass with `make check-examples` (including non-SQLite examples to catch regressions)

### Acceptance Criteria

- `types.DateTime` embeds `time.Time` and `types.NullDateTime` has `Time`/`Valid` fields, both with full Scanner/Valuer support
- Scan defensively handles all SQLite wire types: string, []byte, int64 (Unix epoch), float64 (Julian day), time.Time, nil
- Built-in layouts cover: `"2006-01-02 15:04:05"`, RFC3339, RFC3339Nano, fractional seconds, timezone-aware, date-only
- `RegisterLayout` and `SetOutputLayout` are thread-safe (sync.RWMutex) and documented
- Default output layout is `time.RFC3339Nano`
- SQLite `datetime`/`timestamp`/`date` columns generate as `types.DateTime` (non-null) or `types.NullDateTime` (nullable)
- `types.DateTime` round-trips correctly through SQLite TEXT storage (write time → read time)
- All existing SQLite E2E tests pass with the new type
- Soft delete with `deleted_at DATETIME` works correctly with `types.NullDateTime`
- All doc comments mention that input/output layouts are configurable

### Completion Record

**Date completed:** 2026-04-16

**Files created:**
- `types/datetime.go` — `DateTime`, `NullDateTime`, `RegisterLayout`, `SetOutputLayout`, built-in layouts, Julian day conversion
- `types/datetime_test.go` — 26 test cases covering all scan types, round-trips, custom layouts

**Files modified:**
- `cmd/sqlgen/gotype/gotype.go` — added `registerDialectDefaults()` to register DateTime integration for SQLite, added `types.DateTime` to `deriveFKMethod` stringer list
- `cmd/sqlgen/gotype/gotype_test.go` — updated SQLite datetime/timestamp/date expected types
- `cmd/sqlgen/testdata/examples/sqlite/schema.sql` — changed `created_at`, `updated_at`, `ordered_at` from `TEXT` to `DATETIME`
- `cmd/sqlgen/testdata/examples/sqlite/expected/` — regenerated all golden files
- `cmd/sqlgen/testdata/examples/sqlite/models/` — regenerated models
- `cmd/sqlgen/testdata/examples/sqlite/tests/crud_test.go` — `CreatedAt.IsZero()` instead of `== ""`
- `cmd/sqlgen/testdata/examples/sqlite/tests/soft_delete_test.go` — `.Valid` checks instead of nil checks, `NullableTime` comparator instead of `NullableString`
- `cmd/sqlgen/gen/context_table.go` — `resolveSimpleComparator` maps `types.DateTime` and `types.NullDateTime` to `comparator.Time` / `comparator.NullableTime`
