# Phase 5: Configuration & Type System

Status: Complete
PRD Sections: 4, 4.13, 5.5, 7

## 5.1 Config Loading — YAML config structs, defaults, and env var overrides

**PRD Reference:** Section 4 (4.1–4.12)

**Status:** Complete

### Tasks

- [x] Define `RootConfig` struct with YAML tags (`version`, `input`, `output`, `generation`, `overrides`, `tables`, `views`, `extras`)
- [x] Define `InputConfig` struct (`dialect`, `source`, `schema`, `paths`, `parse_mode`, `connection`, `introspect`)
- [x] Define `ConnectionConfig` struct (`url`, `host`, `port`, `user`, `password`, `database`, `ssl_mode`)
- [x] Define `IntrospectConfig` struct (`schemas`, `exclude_schemas`, `tables`, `exclude_tables`)
- [x] Define `OutputConfig` struct with sub-configs (`driver`, `dir`, `package`, `layout`, `enums`, `types`, `client`)
- [x] Define `EnumOutputConfig`, `TypeOutputConfig`, `ClientOutputConfig` structs
- [x] Define `GenerationConfig` struct — runtime defaults (`query_limit`, `batch_size`, `page_size`, `cursor_keys`, `uuid_version`, `errors.package`, `strict_updates`), schema detection (`soft_delete_columns`, `update_columns`), column exclusion (`exclude_columns`), operations
- [x] Define `SoftDeleteConfig` struct (`name`, `type`)
- [x] Define `OperationsConfig` struct with preset support and per-operation bool toggles
- [x] Define `OverrideConfig` struct (`use_pointers`, `types` map)
- [x] Define `TypeOverride` struct (`type`, `import`, `zero_value`, `nullable`, `convert`)
- [x] Define `NullableOverride` struct (`type`, `import`)
- [x] Define `ConvertConfig` struct (`db_type`, `cast`, `parse`, `to_db`)
- [x] Define `TableConfig` struct — table-specific fields (`struct_name`, `description`, `overrides`, `primary_key`, `exclude_relationships`, `relationships`, `column_map`, `type_map`) plus generation overrides
- [x] Define `TablePrimaryKeyConfig` struct (`strategy`, `uuid_version`)
- [x] Define `TableRelationship` struct (`name`, `type`, `table`, `fk`, `junction`, `junction_local_fk`, `junction_reference_fk`, `filter`, `sort`, `description`)
- [x] Define `RelationshipSort` struct (`column`, `direction`)
- [x] Define `ColumnOverride` struct (`name`, `description`, `type`, `import`)
- [x] Define `ViewConfig` struct (`struct_name`, `sql`)
- [x] Define `ExtraType` and `ExtraTypeField` structs
- [x] Implement `LoadConfig(path string) (*RootConfig, error)` — reads `sqlgen.yml`/`sqlgen.yaml` from the working directory
- [x] Implement defaults application for all fields (dialect→postgres, source→files, driver→pgx, layout→single_file, query_limit→1000, batch_size→200, page_size→100, cursor_keys→["id"], uuid_version→v4, use_pointers→true, operations→"all", default soft_delete_columns list, default update_columns list)
- [x] Implement `SQLGEN_DB_URL` env var override (overrides `connection.url` and all individual connection fields)
- [x] Implement `SQLGEN_DB_PASSWORD` env var override (overrides `connection.password`)
- [x] Implement per-table generation override resolution: table-level → global `generation` → built-in default
- [x] Implement `output.package` derivation from `output.dir` when omitted

### Acceptance Criteria

- Minimal config (`input.dialect` + `input.paths` + `output.dir`) loads successfully with all defaults applied
- Full config (all fields from Section 4.12 example) loads and round-trips correctly
- `SQLGEN_DB_URL` env var takes precedence over `connection.url` and individual fields
- `SQLGEN_DB_PASSWORD` env var takes precedence over `connection.password`
- Per-table generation fields override global generation fields; global overrides built-in defaults
- `output.package` is derived from directory name when not explicitly set
- Operations presets (`all`, `read_only`, `append_only`, `no_delete`, `no_hard_delete`) expand to correct boolean maps
- `OperationsConfig` with `preset` plus individual toggles correctly applies overrides on top of preset

### Tests Required

- [x] Minimal config loads with correct defaults for every field
- [x] Full config loads all fields (based on Section 4.12 example)
- [x] Env var `SQLGEN_DB_URL` overrides config `connection.url`
- [x] Env var `SQLGEN_DB_PASSWORD` overrides config `connection.password`
- [x] Per-table override resolution order: table-level → global → built-in default
- [x] Operations preset string expands to correct operation booleans
- [x] Operations config with preset + individual toggle overrides
- [x] `output.package` derived from `output.dir` directory name
- [x] Default soft_delete_columns list matches PRD priority order
- [x] Default update_columns list matches PRD

### Completion Record

**Files created:**
- `cmd/sqlgen/config/config.go` — All config structs, `LoadConfig`, defaults, env var overrides, operations preset expansion, per-table resolution helpers
- `cmd/sqlgen/config/config_test.go` — 18 test cases covering all acceptance criteria

**Dependencies added:** `gopkg.in/yaml.v3`, `github.com/google/go-cmp/cmp` (test only)

**Completed:** 2026-04-09

**Notes:** Operations uses `UnmarshalYAML` to handle the dual string/struct YAML form. `NullableVariant` similarly handles shorthand string vs full struct. Generation fields use `*int`/`*bool` pointers to distinguish "not set" from zero values for the three-level override resolution.

---

## 5.2 Config Validation — Phase 1 (Pre-Parse)

**PRD Reference:** Section 4.13 (phase 1 rules)

**Status:** Complete

### Tasks

- [x] Implement driver/dialect mismatch validation (`pgx` only valid with `postgres`)
- [x] Implement missing `connection` validation when `source` is `database` or `both`
- [x] Implement invalid `operations` preset name validation (must be one of: `all`, `read_only`, `append_only`, `no_delete`, `no_hard_delete`)
- [x] Implement duplicate explicit `struct_name` validation across tables
- [x] Implement `input.schema` on MySQL/SQLite warning (ignored with warning, not error)
- [x] Implement multi-error aggregation — collect all errors and report together, not one at a time
- [x] Implement `ValidatePreParse(cfg *RootConfig) ([]Warning, error)` returning aggregated errors and warnings
- [x] Implement `ConvertConfig` mutual exclusivity validation — both `cast` and `parse` set is an error; neither set means Scanner/Valuer

### Acceptance Criteria

- `output.driver: pgx` with `input.dialect: mysql` produces a validation error
- `output.driver: pgx` with `input.dialect: sqlite` produces a validation error
- `output.driver: stdlib` works with all three dialects
- Missing `connection` when `source: database` produces a validation error
- Missing `connection` when `source: both` produces a validation error
- `source: files` does not require `connection`
- Invalid operations preset (e.g., `"write_only"`) produces a validation error
- Two tables with same explicit `struct_name` produces a validation error
- `input.schema` on MySQL/SQLite produces a warning (not error), value is ignored
- Multiple validation errors are reported in a single pass
- `ConvertConfig` with both `cast` and `parse` set produces a validation error

### Tests Required

- [x] Driver/dialect mismatch: pgx+mysql → error, pgx+sqlite → error, pgx+postgres → ok, stdlib+any → ok
- [x] Missing connection: source=database without connection → error, source=both without connection → error, source=files → ok
- [x] Invalid operations preset name → error
- [x] Duplicate struct_name across two tables → error
- [x] `input.schema` on MySQL → warning, on SQLite → warning, on PostgreSQL → no warning
- [x] Multiple errors reported together (e.g., driver mismatch + missing connection in same config)
- [x] ConvertConfig with both cast and parse → error
- [x] Valid config passes all pre-parse validations with no errors/warnings

### Completion Record

**Files created:**
- `cmd/sqlgen/config/validate.go` — `ValidatePreParse` with `Warning` type, driver/dialect mismatch, missing connection, invalid preset, duplicate struct_name, ConvertConfig mutual exclusivity, schema dialect warning, multi-error aggregation via `errors.Join`
- `cmd/sqlgen/config/validate_test.go` — 14 test functions covering all acceptance criteria (table-driven where applicable)

**Completed:** 2026-04-09

**Notes:** Uses `errors.Join` for multi-error aggregation. Schema default value `"*"` is excluded from the MySQL/SQLite warning. Table-level operations presets and table-level ConvertConfig are both validated in addition to global-level.

---

## 5.3 `gotype/` — SQL-to-Go Type Mapping

**PRD Reference:** Section 7 (7.1–7.3, 7.5)

**Status:** Complete

### Tasks

- [x] Define `GoType` struct to represent a resolved Go type (`Name`, `Import`, `ZeroValue`, `Nullable`, `NullableImport`, `IsSlice`)
- [x] Implement PostgreSQL built-in type mappings (all types from Section 7.2 PostgreSQL table)
- [x] Implement MySQL built-in type mappings (all types from Section 7.2 MySQL table)
- [x] Implement SQLite built-in type mappings (all types from Section 7.2 SQLite table)
- [x] Implement nullable handling: `use_pointers: true` → `*T`, `use_pointers: false` → `sql.Null*` types
- [x] Implement array type mapping for PostgreSQL (`T[]` → `[]GoT`; nullable arrays use same slice type)
- [x] Implement type resolution chain: table-level `type_map` → table-level `overrides.types` → global `overrides.types` → built-in optional type → built-in dialect mapping
- [x] Implement `ConvertConfig` support — generate metadata for `cast`, `parse`, and `to_db` expressions
- [x] Implement FK string conversion derivation (`.String()` method → `.Valid` null guard → `fmt.Sprint()` fallback)
- [x] Implement `Resolve(dialect, sqlType, nullable, overrides) GoType` as the main entry point

### Acceptance Criteria

- All PostgreSQL types from Section 7.2 map to correct Go types (both non-nullable and nullable)
- All MySQL types from Section 7.2 map to correct Go types (both non-nullable and nullable)
- All SQLite types from Section 7.2 map to correct Go types (both non-nullable and nullable)
- Table-level `type_map` takes precedence over all other mappings
- Table-level `overrides.types` takes precedence over global `overrides.types`
- Global `overrides.types` takes precedence over built-in optional types and dialect defaults
- `use_pointers: true` produces pointer types for nullable columns
- `use_pointers: false` produces `sql.Null*` types for nullable columns
- PostgreSQL array types (`text[]`, `int4[]`, etc.) map to Go slices
- Array types with configured overrides use the override's Go type (e.g., `uuid[]` → `[]uuid.UUID`)
- ConvertConfig with `cast` produces infallible conversion metadata
- ConvertConfig with `parse` produces fallible conversion metadata

### Tests Required

- [x] Each precedence level in the resolution chain overrides the ones below
- [x] All PostgreSQL type mappings (non-nullable and nullable)
- [x] All MySQL type mappings (non-nullable and nullable)
- [x] All SQLite type mappings (non-nullable and nullable)
- [x] Nullable with `use_pointers: true` → pointer types
- [x] Nullable with `use_pointers: false` → `sql.Null*` types
- [x] PostgreSQL array types with and without overrides
- [x] ConvertConfig: cast mode produces correct metadata
- [x] ConvertConfig: parse mode produces correct metadata
- [x] ConvertConfig: no convert (Scanner/Valuer) — no conversion metadata
- [x] FK string conversion derivation for types with `.String()`, `.Valid`, and fallback

### Completion Record

**Files created:**
- `cmd/sqlgen/gotype/gotype.go` — GoType struct, Resolver with 5-level resolution chain, per-dialect built-in mappings (PostgreSQL, MySQL, SQLite), nullable handling (pointer and sql.Null*), PostgreSQL array type support, ConvertConfig metadata, FK string conversion derivation
- `cmd/sqlgen/gotype/gotype_test.go` — 164 test cases covering all acceptance criteria: resolution chain precedence, all dialect mappings (non-nullable and nullable), pointer vs sql.Null* modes, array types with/without overrides, ConvertConfig cast/parse/none, FK string conversion, case normalization, type_map literals, nullable override variants

**Completed:** 2026-04-09

**Notes:** Resolver uses `RegisterIntegration` method as a hook for 5.4 (built-in type integrations). Array types recursively resolve their base type through the full override chain. Reference types ([]byte, map[string]any, net.IP, net.HardwareAddr) use the same type for nullable. Types without sql.Null* equivalents (time.Duration, net.IPNet) always use pointer for nullable regardless of usePointers setting.

---

## 5.4 Built-In Type Integrations

**PRD Reference:** Section 7.4

**Status:** Complete

### Tasks

- [x] Implement `gotype/uuidgoogle/` package — google/uuid mapping (`uuid.UUID`, `uuid.NullUUID`, zero value `uuid.UUID{}`, v4/v7 generation support)
- [x] Implement `gotype/uuidgofrs/` package — gofrs/uuid mapping (`uuid.UUID`, `uuid.NullUUID`, zero value `uuid.Nil`, v4/v7 generation support)
- [x] Implement `gotype/decimal/` package — shopspring/decimal mapping (`decimal.Decimal`, `decimal.NullDecimal`, zero value `decimal.Decimal{}`)
- [x] Wire built-in integrations into the type resolution chain (step 4: after global overrides, before dialect defaults)
- [x] Implement auto-detection: when `overrides.types.uuid` matches a known library import, activate the corresponding integration
- [x] Ensure explicit `overrides.types` fields always take precedence over built-in integration defaults

### Acceptance Criteria

- google/uuid: `uuid` SQL type resolves to `uuid.UUID` with import `github.com/google/uuid`
- google/uuid: nullable `uuid` resolves to `*uuid.UUID` (pointers) or `uuid.NullUUID`
- google/uuid: zero value is `uuid.UUID{}`
- gofrs/uuid: `uuid` SQL type resolves to `uuid.UUID` with import `github.com/gofrs/uuid/v5`
- gofrs/uuid: nullable `uuid` resolves to `*uuid.UUID` (pointers) or `uuid.NullUUID`
- gofrs/uuid: zero value is `uuid.Nil`
- shopspring/decimal: `numeric`/`decimal` SQL types resolve to `decimal.Decimal` with import `github.com/shopspring/decimal`
- shopspring/decimal: nullable resolves to `*decimal.Decimal` or `decimal.NullDecimal`
- shopspring/decimal: zero value is `decimal.Decimal{}`
- All three integrations indicate Scanner/Valuer compliance (no convert config needed)
- Explicit override fields take precedence over built-in integration defaults

### Tests Required

- [x] google/uuid: resolves correct Go type, import, zero value, nullable variant (both pointer and NullUUID)
- [x] gofrs/uuid: resolves correct Go type, import, zero value, nullable variant (both pointer and NullUUID)
- [x] shopspring/decimal: resolves correct Go type, import, zero value, nullable variant (both pointer and NullDecimal)
- [x] Scanner/Valuer compliance — no convert config generated for any integration
- [x] Explicit override on top of built-in integration takes precedence
- [x] Array type with UUID override: `uuid[]` → `[]uuid.UUID`

### Completion Record

**Files created:**
- `cmd/sqlgen/gotype/uuidgoogle/uuidgoogle.go` — google/uuid integration: ImportPath, SQLTypes, Override
- `cmd/sqlgen/gotype/uuidgofrs/uuidgofrs.go` — gofrs/uuid integration: ImportPath, SQLTypes, Override
- `cmd/sqlgen/gotype/decimal/decimal.go` — shopspring/decimal integration: ImportPath, SQLTypes, Override

**Files modified:**
- `cmd/sqlgen/gotype/gotype.go` — Added auto-detection (`detectIntegrations`), override enrichment (`enrichOverride`), `knownIntegrations` registry, and imports for integration packages
- `cmd/sqlgen/gotype/gotype_test.go` — 19 new test cases: 5 per integration (type/import/zero/nullable/Scanner-Valuer), 3 precedence tests, 1 array test

**Completed:** 2026-04-09

**Notes:** Auto-detection scans global overrides for known import paths and registers integrations. For SQL types the user explicitly overrode, `enrichOverride` fills in unspecified fields (zero_value, nullable) from the integration. For SQL types covered by the integration but not overridden by the user, the integration is registered at step 4 of the resolution chain. This enables the decimal integration to cover both `numeric` and `decimal` SQL types even when the user only configures one. Nullable columns always use the integration's dedicated null type (e.g., `uuid.NullUUID`, `decimal.NullDecimal`) regardless of the `use_pointers` setting — these library-provided types are purpose-built for SQL scanning and preferred over `*T` pointers.

---

## 5.5 Config Validation — Phase 2 (Post-Parse)

**PRD Reference:** Sections 4.13 (phase 2 rules), 5.5 (schema qualification ambiguity)

**Status:** Complete

### Tasks

- [x] Implement `cursor_keys` column existence validation — each column in `cursor_keys` must exist in the table (global checked against all tables, table-level checked against that table)
- [x] Implement `soft_delete_columns` type mismatch validation — configured name matches column but type is incompatible (not `timestamp`, `bool`, or `integer`)
- [x] Implement struct name collision detection from auto-detection — PascalCase normalization collisions (e.g., `user_roles` and `userroles` both becoming `UserRole`)
- [x] Implement `exclude_columns` exhaustion warning — global + table-level exclusion removes all columns → warning, skip table
- [x] Implement ambiguous bare table name validation — bare config key matches tables in multiple schemas → validation error
- [x] Implement `ValidatePostParse(cfg *RootConfig, tables []SchemaTable) ([]Warning, error)` with multi-error aggregation

### Acceptance Criteria

- `cursor_keys: [nonexistent_col]` produces a validation error naming the column and table
- Global `cursor_keys` checked against every table; table-level override checked against only that table
- Soft delete column with incompatible type (e.g., `varchar`, `text`, `uuid`) produces a validation error
- Soft delete column with compatible type (`timestamp`, `bool`, `integer`) passes validation
- Two tables producing same PascalCase struct name (without explicit `struct_name`) produces a validation error with guidance to use `struct_name`
- `exclude_columns` removing all columns from a table produces a warning (not error) and table is skipped
- Bare config key matching tables in multiple schemas produces a validation error requiring schema qualification
- Bare config key with table in single schema passes validation
- MySQL/SQLite bare keys never produce ambiguity errors (no schema concept)
- Multiple post-parse errors reported together

### Tests Required

- [x] `cursor_keys` referencing non-existent column → error
- [x] `cursor_keys` referencing existing column → ok
- [x] Global `cursor_keys` validated against all tables
- [x] Table-level `cursor_keys` validated against only that table
- [x] Soft delete column type mismatch (varchar column) → error
- [x] Soft delete column type match (timestamp, bool, integer) → ok
- [x] Struct name collision from auto-detection → error
- [x] No collision when explicit `struct_name` is used → ok
- [x] `exclude_columns` removes all columns → warning, table skipped
- [x] Ambiguous bare table name across schemas → error
- [x] Bare table name in single schema → ok
- [x] Multiple post-parse errors reported together

### Completion Record

**Files modified:**
- `cmd/sqlgen/config/validate.go` — Added `SchemaTable`, `SchemaColumn` types, `ValidatePostParse` with 5 sub-validators (cursor_keys existence, soft delete type compatibility, auto struct name collisions, exclude_columns exhaustion, ambiguous bare names), plus helpers (`findTableConfig`, `qualifiedTableName`, `columnNameSet`, `columnTypeMap`, `isSoftDeleteTypeCompatible`, `normalizeForCollision`)
- `cmd/sqlgen/config/validate_test.go` — 13 new test functions covering all acceptance criteria: cursor_keys (4 tests), soft delete (2 tests with 7 sub-cases), struct name collisions (2 tests), exclude_columns exhaustion (1 test), ambiguous bare names (3 tests), multi-error aggregation (1 test)

**Completed:** 2026-04-09

**Notes:** Uses lightweight `SchemaTable`/`SchemaColumn` types rather than importing the parser module directly, keeping config's dependency graph clean. Struct name collision detection uses normalized comparison (lowercase + strip underscores) to catch collisions like `user_roles`/`userroles`. Soft delete type compatibility handles dialect variations (timestamptz, datetime, boolean, tinyint). Ambiguous bare name validation only runs for PostgreSQL since MySQL/SQLite have no schema concept.
