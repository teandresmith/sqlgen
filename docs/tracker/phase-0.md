# Phase 0: Foundation

Status: Complete
PRD Sections: 3, 10, 11.2, 11.3, 11.5, Appendix A, Appendix B, Appendix C

## 0.0 Project Scaffolding — Multi-module workspace, CI, and release infrastructure

**PRD Reference:** Section 3, Appendix C, `guidelines/ARCHITECTURE.md`, `guidelines/CI.md`

**Status:** Complete

### Tasks

- [x] Create `go.work` workspace file referencing all three modules
- [x] Initialize runtime module `go.mod` at project root (`github.com/teandresmith/sqlgen`)
- [x] Initialize parser module `parser/go.mod` (`github.com/teandresmith/sqlgen/parser`)
- [x] Initialize CLI module `cmd/sqlgen/go.mod` (`github.com/teandresmith/sqlgen/cmd/sqlgen`)
- [x] Create directory structure for runtime module: `comparator/`, `database/`, `database/pgx/`, `database/stdlib/`, `database/mock/`, `omittable/`, `sql/`, `metrics/otel/`
- [x] Create directory structure for parser module: `parser/`, `parser/postgres/`, `parser/mysql/`, `parser/sqlite/`, `parser/introspect/`
- [x] Create directory structure for CLI module: `cmd/sqlgen/`, `cmd/sqlgen/config/`, `cmd/sqlgen/gen/`, `cmd/sqlgen/gen/templates/`, `cmd/sqlgen/gotype/`, `cmd/sqlgen/gotype/uuidgoogle/`, `cmd/sqlgen/gotype/uuidgofrs/`, `cmd/sqlgen/gotype/decimal/`
- [x] Create `.golangci.yml` with all 21 linters, `depguard` module boundary rules, and issue exclusions per Appendix C
- [x] Create `.github/workflows/lint.yml` — golangci-lint CI workflow (matrix over 3 modules)
- [x] Create `.github/workflows/test.yml` — unit + integration test CI workflow (matrix over 3 modules)
- [x] Create `.github/workflows/release-please.yml` — automated release management
- [x] Create `.github/workflows/release.yml` — GoReleaser binary builds triggered by tags
- [x] Create `.goreleaser.yml` — cross-platform binary configuration (linux/darwin/windows × amd64/arm64)
- [x] Create `release-please-config.json` with go release type, changelog path, extra-files for version.go
- [x] Create `.release-please-manifest.json` with initial version `0.1.0`
- [x] Create `Makefile` with common dev targets (lint, test, update-golden)

### Acceptance Criteria

- `go work sync` succeeds with all three modules
- Each module's `go.mod` has correct module path and Go version (1.26+)
- `.golangci.yml` enables exactly the 21 linters specified in Appendix C with correct settings
- `depguard` rules enforce: runtime cannot import parser deps, stdlib driver cannot import pgx, pgx driver cannot import database/sql
- All four GitHub Actions workflows are syntactically valid YAML
- `.goreleaser.yml` targets linux/darwin/windows × amd64/arm64 with CGO enabled
- Directory structure matches `guidelines/ARCHITECTURE.md` package map

### Tests Required

- [x] `go work sync` completes without error
- [x] Each `go.mod` is parseable and has correct module path
- [x] `.golangci.yml` is valid YAML with expected linter count

### Completion Record

**Date:** 2026-04-07

**Files created:**
- `go.work`, `go.mod`, `parser/go.mod`, `cmd/sqlgen/go.mod`
- 20 `doc.go` package placeholders across all three modules
- `cmd/sqlgen/main.go` with version variable for GoReleaser
- `.golangci.yml` (19 linters + 2 formatters, golangci-lint v2 format, depguard boundary rules)
- `.github/workflows/lint.yml`, `test.yml`, `release-please.yml`, `release.yml`
- `.goreleaser.yml`, `release-please-config.json`, `.release-please-manifest.json`

**Files updated:**
- `docs/PRD.md` — Appendix C updated to golangci-lint v2 config format, CI snippet pinned to v2.10.1
- `guidelines/CI.md` — lint workflow pinned to v2.10.1, config/exclusions updated to v2 format, formatter split documented

**Notes:** golangci-lint v2 moved `gofumpt` and `goimports` from `linters` to `formatters` section. PRD and guidelines updated to match.

---

## 0.1 `omittable/` — Presence-Tracking Wrapper

**PRD Reference:** Section 10

**Status:** Complete

### Tasks

- [x] Define `Value[T]` generic struct with unexported `value T` and `set bool` fields in `omittable/omittable.go`
- [x] Implement `Set[T](v T) Value[T]` constructor function
- [x] Implement `Omit[T]() Value[T]` constructor function
- [x] Implement `Get() (T, bool)` method on `Value[T]`
- [x] Implement `IsSet() bool` method on `Value[T]`
- [x] Implement `MustGet() T` method on `Value[T]` (panics when not set)
- [x] Implement `json.Marshaler` — `Set(v)` marshals to `v`, unset omits from JSON (`omitzero` behavior)
- [x] Implement `json.Unmarshaler` — absent field → unset, `null` → `Set(nil)` for pointer types, present → `Set(value)`
- [x] Write comprehensive test suite in `omittable/omittable_test.go`

### Acceptance Criteria

- `Set("")` and `Omit[string]()` are distinguishable via `IsSet()` and `Get()`
- `Set(0)` and `Omit[int]()` are distinguishable
- `Set(false)` and `Omit[bool]()` are distinguishable
- `MustGet()` panics with a clear message when called on an unset value
- JSON round-trip preserves the set/unset distinction for all common types
- Absent JSON field unmarshals to unset `Value`
- `null` JSON field unmarshals to `Set(nil)` for pointer types
- Zero-value `Value[T]` (uninitialized) behaves as unset
- Package has zero external dependencies (stdlib only)

### Tests Required

- [x] Zero value distinction: `Set("")` vs `Omit[string]()`, `Set(0)` vs `Omit[int]()`, `Set(false)` vs `Omit[bool]()`
- [x] `Get()` returns `(value, true)` for set and `(zero, false)` for unset
- [x] `IsSet()` returns correct boolean for set and unset values
- [x] `MustGet()` returns value when set, panics when not set
- [x] JSON marshal: `Set(v)` marshals to JSON representation of `v`
- [x] JSON marshal: unset value is omitted from JSON output (struct with `omitzero` tag)
- [x] JSON unmarshal: absent JSON field → unset `Value`
- [x] JSON unmarshal: `null` JSON field → `Set(nil)` for `*string` and other pointer types
- [x] JSON unmarshal: present JSON field → `Set(value)`
- [x] JSON round-trip for types: `string`, `int`, `float64`, `bool`, `*string`, `time.Time`, `[]byte`
- [x] Uninitialized (zero-value) `Value[T]` behaves as unset

### Completion Record

**Date:** 2026-04-07

**Files created:**
- `omittable/omittable.go` — `Value[T]` struct, constructors, methods, JSON marshaling/unmarshaling
- `omittable/omittable_test.go` — comprehensive test suite (table-driven, zero-value distinction, JSON round-trip)

**Files removed:**
- `omittable/doc.go` — superseded by package comment in `omittable.go`

**Notes:** Go 1.26's `encoding/json` requires the `omitzero` struct tag (not `omitempty`) to call `IsZero()` on struct types. The `Value[T].IsZero()` method enables this. Generated code should use `json:",omitzero"` on `omittable.Value` fields.

---

## 0.2 `sql/` — Dialect Interface & Core Types

**PRD Reference:** Sections 11.5, Appendix A, Appendix B

**Status:** Complete

### Tasks

- [x] Define `Table` struct with `Schema string` and `Name string` fields in `sql/dialect.go`
- [x] Define `Dialect` interface with all 10 methods: `Name`, `Placeholder`, `PlaceholderList`, `QuoteIdentifier`, `FormatTable`, `SupportsReturning`, `ReturningClause`, `UpsertClause`, `InCondition`, `NotInCondition`
- [x] Define `Condition` struct with `Clause string` and `Value any` fields
- [x] Define composition types: `And`, `Or`, `Subquery`, `Range` in `sql/condition.go`
- [x] Define `Sort` struct with `Column string` and `Direction string` fields
- [x] Implement `Where(column string)` function returning a `ConditionBuilder`
- [x] Implement `ConditionBuilder` methods: `Eq`, `Neq`, `Gt`, `Gte`, `Lt`, `Lte`
- [x] Implement `ConditionBuilder` methods: `In`, `Nin`, `Like`, `NLike`
- [x] Implement `ConditionBuilder` methods: `Between`, `IsNull`, `IsNotNull`
- [x] Implement `And(conditions...)` and `Or(conditions...)` composition functions
- [x] Implement `Raw(sql string, args ...any)` escape hatch function
- [x] Write test suite in `sql/condition_test.go`

### Acceptance Criteria

- `Table` struct holds schema and name as separate fields
- `Dialect` interface has all 10 methods matching the PRD Appendix A signature
- `Condition` struct contains `Clause` (SQL fragment with `$` placeholder) and `Value`
- `Where("col").Eq(v)` returns `Condition{Clause: "col = $", Value: v}`
- All 12 builder methods produce correct `Condition` structs
- `And`/`Or` support arbitrary nesting of conditions
- `Raw` allows arbitrary SQL with args
- Package has zero external dependencies (stdlib only)

### Tests Required

- [x] `Where("col").Eq(v)` → `Condition{Clause: "col = $", Value: v}`
- [x] `Where("col").Neq(v)` → `Condition{Clause: "col != $", Value: v}`
- [x] `Where("col").Gt(v)`, `Gte`, `Lt`, `Lte` produce correct clauses
- [x] `Where("col").In(vals...)` produces correct clause
- [x] `Where("col").Like(v)` and `NLike(v)` produce correct clauses
- [x] `Where("col").Between(a, b)` produces correct clause with `Range` value
- [x] `Where("col").IsNull()` → `Condition{Clause: "col IS NULL", Value: nil}`
- [x] `Where("col").IsNotNull()` → `Condition{Clause: "col IS NOT NULL", Value: nil}`
- [x] `And(cond1, cond2)` produces an `And` composition containing both conditions
- [x] `Or(cond1, cond2)` produces an `Or` composition containing both conditions
- [x] Nested `And`/`Or` composition (And containing Or containing And)
- [x] `Raw("price * quantity > $", 1000)` produces correct condition

### Completion Record

**Date:** 2026-04-07

**Files created:**
- `sql/dialect.go` — `Table` struct, `Dialect` interface (10 methods), `Condition` struct, `Sort` struct
- `sql/condition.go` — `And`/`Or` composition functions, `Subquery`/`Range` types, `ConditionBuilder` (12 methods), `Raw` function
- `sql/condition_test.go` — comprehensive table-driven test suite (16 test functions covering all 12 builder methods, composition, nesting, Raw)

**Files removed:**
- `sql/doc.go` — superseded by package comment in `dialect.go`

**Notes:** `And`/`Or` are exported as functions (returning `Condition`) with unexported backing types (`andConditions`/`orConditions`) stored in `Condition.Value`. Accessor methods `IsAnd()`/`IsOr()` on `Condition` allow builders to extract the composition. The test dependency on `go-cmp` is test-only (`_test` package).

---

## 0.3 `comparator/` — Type-Safe Filter Operators

**PRD Reference:** Sections 11.2, 11.3

**Status:** Complete

### Tasks

- [x] Define `Parse(columnName string, dialect sql.Dialect) []sql.Condition` pattern used by all comparators
- [x] Implement `ID` comparator (`Eq`, `Neq`, `In`, `Nin`, `Gt`, `Gte`, `Lt`, `Lte`, `Custom`) in `comparator/id.go`
- [x] Implement `NullableID` comparator (all of ID + `Null`) in `comparator/id.go`
- [x] Implement `String` comparator (all of ID + `Contains`, `StartsWith`, `EndsWith`, `Like`, `NLike`) in `comparator/string.go`
- [x] Implement `NullableString` comparator (all of String + `Null`) in `comparator/string.go`
- [x] Implement `Number[T]` generic comparator (`Eq`, `Neq`, `In`, `Nin`, `Gt`, `Gte`, `Lt`, `Lte`, `Between`, `NBetween`, `Custom`) in `comparator/number.go`
- [x] Implement `NullableNumber[T]` comparator (all of Number + `Null`) in `comparator/number.go`
- [x] Implement `Bool` comparator (`Eq`, `Neq`, `Custom`) in `comparator/bool.go`
- [x] Implement `NullableBool` comparator (all of Bool + `Null`) in `comparator/bool.go`
- [x] Implement `Time` comparator (same operators as Number) in `comparator/time.go`
- [x] Implement `NullableTime` comparator (all of Time + `Null`) in `comparator/time.go`
- [x] Implement `Enum[T]` generic comparator (`Eq`, `Neq`, `In`, `Nin`, `Custom`) in `comparator/enum.go`
- [x] Implement `NullableEnum[T]` comparator (all of Enum + `Null`) in `comparator/enum.go`
- [x] Implement `JSON` comparator (`Contains`, `HasKey`, `Custom`) in `comparator/json.go`
- [x] Implement `NullableJSON` comparator (all of JSON + `Null`) in `comparator/json.go`
- [x] Implement `JSONB` comparator (`HasKey`, `HasAnyKey`, `HasAllKeys`, `Contains`, `ContainedBy`, `PathExists`, `Custom`) in `comparator/jsonb.go` (PostgreSQL-only)
- [x] Implement `NullableJSONB` comparator (all of JSONB + `Null`) in `comparator/jsonb.go`
- [x] Implement `Slice[T]` comparator (`ContainsAny`, `ContainsAll`, `ContainedBy`, `IsEmpty`, `Custom`) in `comparator/slice.go` (PostgreSQL-only)
- [x] Implement `NullableSlice[T]` comparator (all of Slice + `Null`) in `comparator/slice.go`
- [x] Write test suites per comparator type

### Acceptance Criteria

- Every comparator implements `Parse(columnName string, dialect sql.Dialect) []sql.Condition`
- Each operator field, when set, produces the correct `sql.Condition` with proper clause and value
- `Null` field on nullable comparators produces `IS NULL` (true) or `IS NOT NULL` (false)
- `In`/`Nin` on universal comparators produce dialect-aware SQL (ANY/ALL for PostgreSQL+pgx, expanded IN/NOT IN for others)
- JSON comparator produces dialect-specific SQL: `@>` / `?` for PostgreSQL, `JSON_CONTAINS` / `JSON_CONTAINS_PATH` for MySQL
- JSONB comparator produces PostgreSQL-specific operators: `?`, `?|`, `?&`, `@>`, `<@`, `@?`
- Slice comparator produces PostgreSQL-specific operators: `&&`, `@>`, `<@`
- `Custom` field on all comparators passes through raw `sql.Condition` values
- `Between`/`NBetween` on Number and Time produce correct `Range` conditions
- Package depends only on `sql/` package (stdlib + internal)

### Tests Required

- [x] `ID` comparator: each operator produces correct `sql.Condition` output
- [x] `NullableID`: `Null` set to true → `IS NULL`, false → `IS NOT NULL`
- [x] `String` comparator: `Contains("foo")` → `LIKE '%foo%'`, `StartsWith("foo")` → `LIKE 'foo%'`, `EndsWith("foo")` → `LIKE '%foo'`
- [x] `Number[int]`: `Eq`, `In`, `Between` produce correct conditions
- [x] `Number[float64]`: same operators with float values
- [x] `Bool`: `Eq(true)` and `Eq(false)` produce correct conditions
- [x] `Time`: `Between(start, end)` produces correct `Range` condition
- [x] `Enum[string]`: `In` with multiple values produces correct condition
- [x] `JSON`: `Contains` produces `@>` for PostgreSQL dialect, `JSON_CONTAINS` for MySQL dialect
- [x] `JSON`: `HasKey` produces `?` for PostgreSQL, `JSON_CONTAINS_PATH` for MySQL
- [x] `JSONB`: `HasAnyKey`, `HasAllKeys`, `ContainedBy`, `PathExists` produce correct PostgreSQL operators
- [x] `Slice[string]`: `ContainsAny`, `ContainsAll`, `ContainedBy`, `IsEmpty` produce correct PostgreSQL operators
- [x] Dialect-aware behavior: `In`/`Nin` with PostgreSQL+pgx vs expanded placeholder dialects
- [x] `Custom` field on each comparator passes through raw conditions
- [x] Multiple operators set simultaneously produce multiple conditions in output slice

### Completion Record

**Date:** 2026-04-07

**Files created:**
- `comparator/comparator.go` — package comment, `Numeric` constraint, `Range[T]` type, shared helper functions (`parseIn`, `parseNin`, `parseNull`, `parseBetween`, `parseNBetween`)
- `comparator/id.go` — `ID` and `NullableID` comparators
- `comparator/string.go` — `String` and `NullableString` comparators (with `Contains`, `StartsWith`, `EndsWith`)
- `comparator/number.go` — `Number[T]` and `NullableNumber[T]` generic comparators (with `Between`, `NBetween`)
- `comparator/bool.go` — `Bool` and `NullableBool` comparators
- `comparator/time.go` — `Time` and `NullableTime` comparators
- `comparator/enum.go` — `Enum[T]` and `NullableEnum[T]` generic comparators
- `comparator/json.go` — `JSON` and `NullableJSON` comparators (dialect-specific: PostgreSQL `@>`/`?`, MySQL `JSON_CONTAINS`/`JSON_CONTAINS_PATH`)
- `comparator/jsonb.go` — `JSONB` and `NullableJSONB` comparators (PostgreSQL-only: `?`, `?|`, `?&`, `@>`, `<@`, `@?`)
- `comparator/slice.go` — `Slice[T]` and `NullableSlice[T]` comparators (PostgreSQL-only: `&&`, `@>`, `<@`)
- `comparator/comparator_test.go` — test dialect stubs
- `comparator/id_test.go`, `string_test.go`, `number_test.go`, `bool_test.go`, `time_test.go`, `enum_test.go`, `json_test.go`, `jsonb_test.go`, `slice_test.go` — comprehensive test suites

**Files removed:**
- `comparator/doc.go` — superseded by package comment in `comparator.go`

**Notes:** Nullable variants embed the base comparator and add a `Null *bool` field. `In`/`Nin` are dialect-aware: PostgreSQL uses `= ANY($)` / `!= ALL($)` with array parameters; other dialects use `IN $` / `NOT IN $` for builder expansion. Generic comparators use `Numeric` constraint for Number and `comparable` for Enum and Slice. All struct fields have snake_case JSON tags with `omitempty` for future API serialization support (not in PRD — added per user request).
