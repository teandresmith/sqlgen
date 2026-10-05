# Phase 13: Tenancy

Status: Not Started
PRD Sections: 29 (primary); cross-refs 4.8 (TableConfig), 7.5 (TypeOverride), 9.6 (CallOptions), 17 (soft delete), 27.5 / 27.9 (cache key grammar + async ctx), 28.3 (Event metadata)

> **Design reference:** `docs/design/TENANCY.md` — design supplement (open-question record, codegen details). PRD §29 is the normative spec; when the two drift, PRD wins and TENANCY.md is updated.

> **Current state (pre-13 wiring):**
> - Phase 11 (Events) and Phase 12 (Caching) are complete — 13 depends on both. Cache hook already closes over a resolved tenant-style value via its per-mutation invalidation path; tenancy rides the same plumbing.
> - `CallOptions[FO]` already carries `SkipHooks` / `SkipCache`; `SkipTenancy` is an additive field, no breaking change.
> - `MutationContext.AffectedPKs` / `QueryContext` shapes are unchanged by tenancy (§29.9 — tenancy is not a hook).
> - Soft delete (§17) is the closest column-driven parallel — reuse the auto-filter pattern, do not reinvent.

---

## 13.1 Config — TenancyConfig, TableTenancyConfig, resolvers, validation

**PRD Reference:** §29.2.1, §29.2.2, §29.2.3, §29.2.4 (validation only)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.1

**Module:** CLI (`cmd/sqlgen/config/`)

**Status:** Complete

### Tasks

- [x] `cmd/sqlgen/config/tenancy.go` — `TenancyConfig` struct with fields `Enabled bool`, `Column string`, `Required *bool` (default `true` via `applyTenancyDefaults`), `Type *TypeOverride` reusing the §4.7 `TypeOverride` shape
- [x] `cmd/sqlgen/config/tenancy.go` — `TableTenancyConfig` tri-state struct with `Enabled *bool`, `Column *string`, `Required *bool`, `Type *TypeOverride` — `nil` means inherit global (PRD §29.2.2)
- [x] Wire `TenancyConfig` onto `RootConfig` and `TableTenancyConfig` onto `TableConfig` with YAML tags (`tenancy`)
- [x] `ResolveTableTenancyEnabled(table, global)` following the precedence per-table override → global → default (`false`)
- [x] `ResolveTableTenancyColumn(table, global)` — per-table override → global `tenancy.column` → `""`
- [x] `ResolveTableTenancyRequired(table, global)` — per-table override → global → default `true`
- [x] `ResolveTableTenancyType(table, global)` — per-table override → global → `nil` (forces detection fallback in 13.3)
- [x] YAML-parse validation: `tenancy.enabled: true` at global level requires non-empty `tenancy.column` — `validateTenancyConfig` in `validate.go`
- [x] YAML-parse validation: any `TypeOverride` (global or per-table) with non-empty `Type` MUST have non-empty `Import` — `validateTenancyTypeOverride` helper
- [x] YAML-parse validation: per-table `tenancy.enabled: true` accepted during YAML parsing (column-existence check deferred to 13.3)
- [x] `cmd/sqlgen/config/tenancy_test.go` — table-driven resolver coverage
- [x] `cmd/sqlgen/config/tenancy_test.go` — YAML-validation coverage: global-enabled-no-column, TypeOverride-no-import, tri-state inheritance (`nil` → global)

**Decision:** `TenancyConfig.Required` uses `*bool` (not plain `bool`) so YAML-absent vs YAML-explicit-false can be distinguished; `applyTenancyDefaults` fills `new(true)` when the tenancy block is present. This matches the established pattern (`OverrideConfig.UsePointers *bool`) and avoids the plain-`bool` defaulting ambiguity that would otherwise require a custom `UnmarshalYAML`.

### Acceptance Criteria

- `TenancyConfig` / `TableTenancyConfig` YAML shapes match PRD §29.2.1 / §29.2.2 byte-for-byte (no extra fields, no renamed fields)
- `required` defaults to `true` (PRD §29.2.1) — a missing YAML key inherits the safe fail-closed default, not `false`
- Tri-state inheritance: `TableTenancyConfig.Enabled = nil` returns the global `tenancy.enabled` value from the resolver; `Enabled = &true` overrides to true even when global is false
- Per-table `column` override is evaluated independently of per-table `enabled` — a table may use a different column name while the rest of the schema uses the global
- Per-table `type` override supersedes the global regardless of what the schema parser detects (order: per-table → global → schema → fallback — see 13.3)
- `ResolveTableTenancy*` functions are pure (no side effects, no schema access) so 13.3 can layer schema-dependent validation on top
- Error messages from config validation name the offending block (`tenancy:` for global, `tables.<name>.tenancy:` for per-table) and the expected field

### Tests Required

- [x] `tenancy_test.go`: global-off + per-table-on → resolver reports `Enabled = true` for that table, `false` for others
- [x] `tenancy_test.go`: global-on + per-table-off → resolver reports `Enabled = false` for that table, `true` for others
- [x] `tenancy_test.go`: per-table column override → resolver returns the per-table column; other tables return global
- [x] `tenancy_test.go`: `required` tri-state — `nil` inherits global, `&false` overrides true global to false
- [x] `tenancy_test.go`: `tenancy.enabled: true` without `tenancy.column` is a config-load error mentioning `tenancy.column`
- [x] `tenancy_test.go`: `tenancy.type: { type: X }` with empty `import` errors with the §4.7 TypeOverride format
- [x] `tenancy_test.go`: global `tenancy.type` overridden by a per-table `tenancy.type` returns the per-table value

### Completion Record

**Completed:** 2026-04-22

**Files created:**
- `cmd/sqlgen/config/tenancy.go` — `TenancyConfig`, `TableTenancyConfig`, `applyTenancyDefaults`, four `ResolveTableTenancy*` resolvers.
- `cmd/sqlgen/config/tenancy_test.go` — 8 test functions, 29 sub-cases; passes under `-race`.

**Files modified:**
- `cmd/sqlgen/config/config.go` — `Tenancy *TenancyConfig` on `RootConfig`, `Tenancy *TableTenancyConfig` on `TableConfig`; `applyTenancyDefaults` hooked into `applyDefaults`.
- `cmd/sqlgen/config/validate.go` — `validateTenancyConfig` + `validateTenancyTypeOverride` hooked into `ValidatePreParse`.

**Verification:** `make check` passes (lint + unit tests across all 8 modules). 29/29 tenancy sub-tests pass.

**Deferred to 13.3:** Per-table `tenancy.enabled: true` with a missing tenant column in the parsed schema must be a hard codegen error (PRD §29.2.3 rule 5). 13.1 accepts this YAML without erroring because `ValidatePreParse` runs before the schema is parsed; the check is owned by 13.3's detection pass. Similarly, the parsed-schema / fallback half of the §29.2.4 type-resolution order (step 3 + 4) is owned by 13.3 — `ResolveTableTenancyType` here returns `nil` to signal "fall through to detection."

---

## 13.2 Runtime — `tenancy/` package, CallOptions.SkipTenancy

**PRD Reference:** §29.3.1, §29.3.3, §29.4.4
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.2

**Module:** Runtime (`github.com/teandresmith/sqlgen/tenancy`) + generator templates for `CallOptions`

**Status:** Complete

### Tasks

- [x] Create `tenancy/` subpackage at the runtime module root (stdlib only; no imports from parser, CLI, `sql`, `hook`, `event`, `cache`)
- [x] `tenancy/tenancy.go` — `TenantResolver[T comparable] func(ctx context.Context) (T, error)` generic type alias; `T` constrained to `comparable` (§29.3.1)
- [x] `tenancy/tenancy.go` — `ErrMissing = errors.New("tenancy: tenant missing from context")` — exact string per PRD §29.3.1
- [x] `tenancy/tenancy.go` — `ErrMismatch = errors.New("tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override")` — exact string per PRD §29.3.1
- [x] Godoc on `TenantResolver[T]` noting `T comparable` rationale (enables `==` mismatch check in mutations), and `required: true` zero-value + `ErrMissing` relationship — align wording with PRD §29.3.1
- [x] Template change in the generator's shared-types emission (`cmd/sqlgen/gen/templates/` — shared `CallOptions[FO]` location): add `SkipTenancy bool` field on the generated `CallOptions[FO]` struct, **conditionally emitted only when `tenancy.enabled: true` globally**
- [x] Confirm `resolveCallOptions` helper does NOT fold `SkipHooks → SkipTenancy` (§29.4.4 — tenancy is orthogonal to hooks because it runs in the SQL builder)
- [x] `tenancy/tenancy_test.go` — compile-time parametric instantiation tests for `TenantResolver` with `uuid.UUID`, `int64`, `string`, and a named-wrapper type (e.g. `type WorkspaceID uuid.UUID`)
- [x] `tenancy/tenancy_test.go` — assert `ErrMissing` / `ErrMismatch` sentinel equality (`errors.Is`) and exact message strings
- [x] Generator golden test: `SkipTenancy` field present on `CallOptions[FO]` when `tenancy.enabled: true`; absent when disabled
- [x] Generator `resolveCallOptions` test: `SkipHooks: true` does NOT set `SkipTenancy` after resolution

### Acceptance Criteria

- `tenancy/` package imports only stdlib — no runtime or CLI cross-imports (CLAUDE.md three-module rule + §29.3.1 "one runtime injection point")
- `TenantResolver[T comparable]` lives on the type alias, not on `Client` — the generator picks concrete `T` at codegen time; `Client` holds a concrete resolver field (§29.3.1)
- `ErrMissing` / `ErrMismatch` are package-level sentinels; generated code and user code both reach for these via `errors.Is`
- `CallOptions.SkipTenancy` is additive — existing projects without tenancy regenerate identically (no `SkipTenancy` field appears in their generated struct)
- `SkipHooks: true` and `SkipTenancy: true` are orthogonal toggles (§29.4.4); the generator never collapses one into the other

### Tests Required

- [x] `tenancy_test.go`: `TenantResolver[uuid.UUID]`, `TenantResolver[int64]`, `TenantResolver[string]`, `TenantResolver[WorkspaceID]` (named wrapper over `uuid.UUID`) all compile and carry the expected function signature
- [x] `tenancy_test.go`: `errors.Is(someErr, ErrMissing)` and `errors.Is(someErr, ErrMismatch)` return true for the sentinels; exact message strings match PRD §29.3.1
- [x] Golden test: generated `CallOptions[FO]` struct contains `SkipTenancy bool` when tenancy enabled (assert AST field presence)
- [x] Golden test: generated `CallOptions[FO]` struct **lacks** `SkipTenancy` field when tenancy disabled (regression guard — non-tenancy projects stay identical)
- [x] `resolveCallOptions` unit test: `SkipHooks = true, SkipCache = false, SkipTenancy = false` input stays `SkipTenancy = false` after resolution

### Completion Record

**Completed:** 2026-04-22

**Files created:**
- `tenancy/tenancy.go` — `TenantResolver[T comparable]`, `ErrMissing`, `ErrMismatch` with PRD-aligned godoc. Stdlib-only imports (`context`, `errors`).
- `tenancy/tenancy_test.go` — 7 test functions covering parametric instantiation (`uuid.UUID`, `int64`, `string`, `WorkspaceID` wrapper), ctx propagation, error propagation, sentinel message strings, and sentinel distinctness.

**Files modified:**
- `cmd/sqlgen/gen/context_shared.go` — `BuildSharedTypesContext` now takes `tenancyEnabled bool`; `sharedTypeDefinitions` splices a `SkipTenancy` field into `CallOptions` between `SkipHooks` and `FieldOptions` when enabled, omits it otherwise. `resolveCallOptions` body unchanged (orthogonality to `SkipHooks` preserved per §29.4.4).
- `cmd/sqlgen/gen/orchestrate.go` — `generateSupport` threads `cfg` through; `tenancyEnabled = cfg.Tenancy != nil && cfg.Tenancy.Enabled` drives the conditional emission.
- `cmd/sqlgen/gen/context_shared_test.go` — updated existing callers to `BuildSharedTypesContext("db", false)`; added `TestBuildSharedTypesContext_callOptionsFieldsTenancyEnabled`, `TestBuildSharedTypesContext_skipTenancyAbsentWhenDisabled`, and `TestResolveCallOptions_skipHooksDoesNotFoldSkipTenancy`.
- `cmd/sqlgen/gen/shared_types_test.go` — added `TestSharedTypesTemplate_tenancyEnabled_emitsSkipTenancy` and `TestSharedTypesTemplate_tenancyDisabled_omitsSkipTenancy`; existing golden test continues to assert the tenancy-disabled baseline (byte-identical regression guard).
- `go.mod` — `github.com/google/uuid` promoted from indirect to direct (imported by `tenancy/tenancy_test.go` for the PRD-specified `TenantResolver[uuid.UUID]` test case).

**Verification:** `make check` passes (lint + unit tests across all 8 modules). `tenancy` package tests run under `-race`. Existing `shared_types_gen.go` golden file unchanged — projects without tenancy regenerate byte-identically.

**Deferred to 13.5:** `WithTenantResolver(r tenancy.TenantResolver[<T>]) ClientOption` generation on `clientOptions`, per-entity-client threading, and the `tenantResolver` field on generated clients. 13.2 ships the primitives (`TenantResolver[T]`, sentinels, `CallOptions.SkipTenancy`) that 13.5 consumes; no client-level wiring happens here because it depends on the detection + type-resolution pass owned by 13.3.

---

## 13.3 Schema detection + type resolution + validation

**PRD Reference:** §29.2.3 (detection), §29.2.4 (type resolution + validation)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.3

**Module:** CLI (`cmd/sqlgen/gen/` + `cmd/sqlgen/config/`)

**Status:** Complete

> **Carried from 13.1:** The YAML-parse validation in 13.1 deliberately accepts per-table `tenancy.enabled: true` without verifying the column exists, because the parsed schema is not available at `ValidatePreParse` time. That column-existence check is implemented here as part of the post-parse detection pass (see Tasks row 3 + Tests Required row 8). `ResolveTableTenancyType` in 13.1 also returns `nil` when no YAML override is set — the schema/fallback half of the §29.2.4 type-resolution order is owned by this sub-item.

### Tasks

- [x] `cmd/sqlgen/gen/context_tenancy.go` — `TenancyContext` struct attached to each resolved table: `Tenanted bool`, `Column string`, `Required bool`, `GoType string`, `Import string`
- [x] Detection pass in the schema-resolution stage: for each table, compute `effective_column` (per-table override → global `tenancy.column`); classify as **tenanted** (column exists), **shared** (column absent), or **forced-shared** (per-table `enabled: false`)
- [x] Per-table `tenancy.enabled: true` + column absent in parsed schema → **hard codegen error** naming the table and the expected column _(carried from 13.1 — deferred because schema is not available at pre-parse time)_
- [x] Type resolution order (§29.2.4): per-table `tenancy.type` → global `tenancy.type` → parsed-schema type-override map / built-in integration → parser fallback
- [x] Validation — nullable column: tenant column with nullable SQL type → hard codegen error naming table + column with "tenant column must not be nullable" remediation
- [x] Validation — comparable Go type: resolved Go type must be `comparable` (detect via a known-bad-shape list: slice, map, function, struct-with-slice-field); hard error names the table and the non-comparable type
- [x] Validation — uniform tenant Go type across all tenanted tables in the generate run: if types diverge, emit a hard error listing every offending table and its resolved type (§29.2.4)
- [x] Validation — SQL-column-type sanity: when `tenancy.type` overrides the detected type, cross-check against the parsed column SQL type (belt-and-suspenders — e.g. `ids.WorkspaceID` wrapping `uuid.UUID` pointed at a `bigint` column errors)
- [x] Error messages match the PRD §29.2.4 examples: include the table, column, resolved type(s), and at least one remediation path
- [x] `cmd/sqlgen/gen/context_tenancy_test.go` — detection table tests: mixed tenanted/shared, forced-shared override, tenant-column-in-composite-PK
- [x] Validation tests for each of the four error cases above

### Acceptance Criteria

- Detection is a pure function of `(RootConfig, parser.Schema)` — no side effects, no filesystem access; returns either `map[tableName]TenancyContext` or an aggregated validation error
- Detection-based inclusion mirrors soft delete (§17) — a schema with 50 tables does not need 50 config entries; only opt-outs and column-name overrides do
- `enabled: false` per-table opt-out wins over auto-detection even when the column exists (§29.2.3 rule 4)
- `enabled: true` per-table with no matching column is an error, not silent-off (§29.2.3 rule 5)
- Uniform-type rule errors ONCE per generate run, listing every offender — not N errors for N tables
- SQL-type sanity check is skipped when no `tenancy.type` override is present (the parser's detection is already trusted)
- Error strings are actionable and reference the PRD's recommended remediation (opt-out, unify column types, or consolidate via migration)

### Tests Required

- [x] Detection: two-table fixture (one tenanted, one shared) → `Tenanted` flag correctly set per table
- [x] Detection: forced-shared override — table has `workspace_id` column but `tenancy.enabled: false` → `Tenanted = false`
- [x] Detection: per-table column override (`legacy_widgets.tenancy.column: org_id`) + schema with `org_id` → `Tenanted = true`, `Column = "org_id"`
- [x] Validation: tenanted-table column with `NOT NULL` absent → error names table + column + "nullable" remediation
- [x] Validation: non-comparable Go type (struct containing a slice field) → error names table + type + "comparable" remediation
- [x] Validation: mixed-type schema (`users.workspace_id uuid.UUID` + `legacy_widgets.tenant_id int64`) → single error listing both tables and both types
- [x] Validation: `tenancy.type: WorkspaceID` (import claims wraps `uuid.UUID`) pointed at a `bigint` column → error at validation
- [x] Validation: per-table `enabled: true` + column missing → error names table + expected column

### Completion Record

**Completed:** 2026-04-22

**Files created:**
- `cmd/sqlgen/gen/context_tenancy.go` — `TenancyContext`, `BuildTenancyContext(cfg, schema, resolver)`, detection + type-resolution + validation pass. Pure function of `(RootConfig, Schema)`; no side effects. Returns `nil` map when tenancy globally disabled.
- `cmd/sqlgen/gen/context_tenancy_test.go` — 12 test functions covering detection (mixed tenanted/shared, forced-shared opt-out, per-table column override, composite-PK with tenant in PK), all four validation paths (nullable, non-comparable, uniform-type, override-vs-SQL-type mismatch), the deferred 13.1 missing-column error, and the `required` tri-state inheritance.

**Files modified:** none. `BuildTenancyContext` is not yet wired into `orchestrate.go` — that wiring lands with 13.5 (consumer of `TenancyContext` for `ClientContext` and per-entity templates).

**Design notes:**
- **Map shape.** The return map is keyed by schema-qualified name (`public.products`) or bare name when schema is empty, matching `findTableConfig`'s lookup convention. Every parsed table gets an entry — `Tenanted: true` for auto-filtered tables, `Tenanted: false` for shared / opted-out — so downstream consumers get a single O(1) lookup.
- **Type resolution.** Steps 3+4 of §29.2.4 are delegated to the existing `gotype.Resolver` chain, which already covers table-level `overrides.types` → global `overrides.types` → built-in integrations → enums/domains → dialect fallback. Per-table and global `tenancy.type` wrap this as step 1+2. Tenant columns are NOT NULL by contract, so `nullable=false` is passed to the resolver (never `*T` for a tenant).
- **SQL-type sanity check.** Coarse 4-way categorization (integer / uuid / string / unknown) on both sides. "Unknown" short-circuits the check — we trust user wrapper types with neutral names (e.g. `ids.WorkspaceID` wrapping a comparable primitive). The PRD example (`WorkspaceID` with `github.com/google/uuid` import against a `bigint` column) trips the check because the import path contains "uuid".
- **Comparable check.** Structural detection of slices, maps, and functions at the top-level type. Extends into `extras:` fields — user-declared structs are walked to catch a non-comparable field (a slice field inside the extra type disqualifies the whole struct).
- **Uniform-type validation.** Emits at most one aggregated error per generate run; the error lists every divergent type group with its import path and the tables in that group. Grouping is by `(goType, import)` so two same-named types from different packages (e.g. `gofrs/uuid.UUID` vs `google/uuid.UUID`) are flagged.
- **Error aggregation.** `errors.Join` returns the full set in one pass; when errors accumulate, `BuildTenancyContext` returns a nil map and the aggregated error so callers don't confuse a partial result with success.

**Verification:** `make check` passes (lint + unit tests across all 8 modules). `TestBuildTenancyContext_*` runs 12 cases under `-race`. Cyclomatic complexity stays within project limit (`cyclop` max 15) after extracting `resolveTableTenancy` helper.

**Deferred to 13.5:** Wiring `BuildTenancyContext` into `orchestrate.go` alongside `BuildCacheContext`, and threading `TenancyContext` into `ClientContext` / per-table templates for actual filter emission. 13.3 ships the analysis pass; 13.5 ships the codegen that consumes it.

---

## 13.4 SQL builder additions

**PRD Reference:** §29.4.1, §29.4.2, §29.10 (o2o JOIN convention)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.4

**Module:** Runtime (`sql/`)

**Status:** Complete

### Tasks

- [x] Audit `sql.SelectOptions.Conditions`, `sql.UpdateOptions.Conditions`, `sql.UpdateOptions.SetClauses`, `sql.DeleteOptions.Conditions` — confirm existing surface suffices for tenancy (tenant filters are standard `col = $N` conditions expressible via `sql.Where(col).Eq(v)`)
- [x] `sql/builder.go` — confirm `BuildSelectJoin` placeholder numbering is driven only by outer `Conditions` (not by JOIN `On` strings); add a godoc note codifying the convention: tenant filters for tenanted child tables go in the outer WHERE `Conditions`, NOT into the JOIN `On` string
- [x] Add godoc to `SelectOptions` / `UpdateOptions` / `DeleteOptions` stating "tenancy filter conditions are standard `Condition` values composed by the generator before the `BuildXxx` call — builders remain tenancy-agnostic"
- [x] No new public builder API — tenancy adds rows to existing slices, not new fields or methods
- [x] `sql/builder_test.go` — UPDATE / SELECT / SELECT-JOIN / DELETE round-trip tests with an extra tenant condition in the `Conditions` slice, produced SQL matches expected per dialect (pg `$N`, MySQL/SQLite `?`)
- [x] `sql/builder_test.go` — placeholder-numbering invariant: SELECT with N user conditions + 1 tenant condition produces placeholders $1..$(N+1) in document-order (user conds, then tenant) — document the convention so the generator can rely on it
- [x] `sql/builder_test.go` — round-trip: same set of conditions with different tenant values produces same SQL skeleton; only `args` differ

### Acceptance Criteria

- Zero public API changes on `sql/` — tenancy is implemented by the generator composing standard `Condition` values, not by new builder fields
- JOIN convention documented in godoc: tenant filters on child tables are appended to the outer WHERE, not to the JOIN `On` — the placeholder-numbering guarantee depends on this
- Builder tests prove tenancy conditions compose cleanly with user conditions and soft-delete conditions (mixed-source slice does not break placeholder numbering)
- Round-trip placeholder positions are stable across tenant-value changes — same SQL byte-for-byte, only args change (§29.4.2 byte-identical redundant-match requirement)

### Tests Required

- [x] `builder_test.go`: SELECT with tenant condition → PostgreSQL `... WHERE ... AND "workspace_id" = $N` / MySQL `` WHERE ... AND `workspace_id` = ? `` / SQLite `WHERE ... AND "workspace_id" = ?`
- [x] `builder_test.go`: UPDATE with tenant condition in `Conditions` → `UPDATE ... SET ... WHERE pk = $1 AND workspace_id = $2` (tenant column NOT in SET clause)
- [x] `builder_test.go`: DELETE with tenant condition → `DELETE ... WHERE pk = $1 AND workspace_id = $2`
- [x] `builder_test.go`: SELECT-JOIN with tenanted child → outer WHERE contains `parent.tenant = $N AND child.tenant = $M`; JOIN `ON` is FK-only (no tenant inside JOIN)
- [x] `builder_test.go`: placeholder-numbering sanity — user cond + tenant cond → $1 user, $2 tenant; tenant cond alone → $1 tenant
- [x] `builder_test.go`: round-trip — two builds with different tenant values produce byte-identical SQL

### Completion Record

**Completed:** 2026-04-22

**Audit finding:** The existing builder surface already suffices for tenancy — tenant filters are standard `sql.Where(col).Eq(v)` conditions appended to the existing `Conditions` slice. No new public API (no new fields, no new builder functions, no new methods on `Dialect`). The note in the spec about `sql.DeleteOptions` refers to the existing `sql.HardDeleteOptions` (the runtime package's hard-delete options struct); the `Conditions` slice there already supports tenancy the same way. This matches PRD §29.4 (tenancy runs in the SQL builders as standard WHERE conditions — not as a hook, not as a new option).

**Files modified:**
- `sql/builder.go` — godoc additions on `SelectOptions`, `UpdateOptions`, `HardDeleteOptions`, and `JoinClause` codifying the tenancy-filter-placement convention (tenant conditions go in the outer `Conditions` slice, NOT in JOIN `On` strings; SET clause excludes tenant column under normal operation). `BuildSelectJoin` godoc now explicitly states the placeholder-numbering contract: positions are driven entirely by `opts.Conditions` in slice order, with no contribution from JOIN `On`. Zero behavior change.
- `sql/builder_test.go` — added 6 tenancy-composition test functions: `TestTenancy_SelectAppendsTenantCondition` (SELECT cross-dialect), `TestTenancy_UpdateAppendsTenantCondition` (UPDATE pg+mysql with SET excluding tenant), `TestTenancy_DeleteAppendsTenantCondition` (DELETE pg+sqlite), `TestTenancy_SelectJoinTenantInOuterWhere` (o2o child-tenant in outer WHERE not in `On`), `TestTenancy_RoundTripByteIdentical` (PRD §29.4.2 byte-identical redundant-match property), `TestTenancy_MixedSourcePlaceholderNumbering` (user + soft-delete + tenant compose in slice order with correct numbering).

**Design notes:**
- **Zero public API change.** The builder test additions prove that tenancy rides the existing `sql.Condition` surface without any builder modification. The generator (13.5) just needs to append more rows to the `Conditions` slice — no new builder functions or options.
- **Placeholder-numbering invariant.** `BuildSelectJoin` numbering is driven solely by `opts.Conditions`. The `JoinClause.On` string is opaque pre-formatted SQL with no placeholders — this is the contract the generator relies on when splicing a tenant condition into the outer WHERE after any number of JOINs. Locked in by `TestTenancy_SelectJoinTenantInOuterWhere`.
- **Byte-identical redundant-match.** PRD §29.4.2 property #1 ("the generated SQL is byte-identical either way") is proved at the builder level by `TestTenancy_RoundTripByteIdentical` — same Conditions slice shape across two different tenant values produces byte-identical SQL, only args differ. The generator's Create-tolerates-matching-input path (13.5) builds on this guarantee.
- **Mixed-source composition.** `TestTenancy_MixedSourcePlaceholderNumbering` locks in the three-source composition the generator will use (user → soft-delete → tenant). `IS NULL` consumes no placeholder, so `$1` = user, `$2` = tenant; the invariant is "slice order drives placeholder order, regardless of which source produced the condition."

**Verification:** `make check` passes (lint + unit tests across all 8 modules). The 6 new tenancy sub-tests plus the existing 18 sub-tests in `TestBuildSelect`/`TestBuildUpdate`/`TestBuildHardDelete`/`TestBuildSelectJoin` all pass. Zero regression — all existing builder tests unchanged.

---

## 13.5 Generator — BuildTenancyContext, template changes, client wiring

**PRD Reference:** §29.3.2, §29.4.1, §29.4.2, §29.4.3, §29.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.5

**Module:** CLI (`cmd/sqlgen/gen/`)

**Status:** Complete

### Tasks

- [x] `cmd/sqlgen/gen/context_tenancy.go` — `BuildTenancyContext(root config.RootConfig, schema parser.Schema) (map[string]TenancyContext, error)` wired into `orchestrate.go`; `attachTenancyToTables` fans the map into per-table contexts.
- [x] `ClientContext` additions: `TenancyEnabled bool`, `TenancyGoType string`, `TenancyImport string`, `TenantedEntities []string` (`TenancyColumn` intentionally skipped — tenant column name is per-table, not client-global).
- [x] `client.go.tmpl` — emits `tenantResolver tenancy.TenantResolver[<T>]` on `clientOptions` (gated on `TenancyEnabled`).
- [x] `client.go.tmpl` — emits `WithTenantResolver(r tenancy.TenantResolver[<T>]) ClientOption` with concrete `<T>` from `TenancyGoType` (§29.3.2).
- [x] `client.go.tmpl` — threads `tenantResolver` into each tenanted entity-client constructor via a post-wire block; non-tenanted entity constructors unchanged.
- [x] `get.go.tmpl` — `GetMany` resolves tenant at method entry and appends `sql.Where("<col>").Eq(resolved)` to `conds` when `!options.SkipTenancy`; error wrap is `"get <table>: resolve tenant: %w"`. `Get` propagates via its GetMany round-trip.
- [x] `create.go.tmpl` / `upsert.go.tmpl` — resolve tenant, mismatch-check against `input.<TenantField>.Get()`, auto-set tenant column into `columns`/`args`. `CreateMany` resolves once and applies per-row mismatch + auto-set via the omittable path.
- [x] `update.go.tmpl` — resolve tenant, mismatch check, append tenant to `Conditions` (NOT `SetClauses` unless `SkipTenancy`). Same applied to `UpdateMany` per-item and `UpdateWhere`.
- [x] `delete.go.tmpl` — `SoftDelete(Many|Where)`, `HardDelete(Many|Where)`, `Restore(Many|Where)` all append tenant to `Conditions` when `!SkipTenancy`.
- [x] All `*Where` variants (`UpdateWhere`, `HardDeleteWhere`, `SoftDeleteWhere`, `RestoreWhere`) append tenant after `filter.ToConditions(c.dialect)` (§29.4.2).
- [x] `increment.go.tmpl` — appends tenant to `Conditions`; tenant column not exposed on increment-input shape (the existing `IncrementColumn` constants exclude non-numeric columns, so tenant UUID columns are already absent).
- [ ] Composite PK path: tenant-in-PK exported `XXXPK` field omission — **deferred**. Current behavior: composite PK struct still includes the tenant field; resolver-driven tenant enforcement still happens via the WHERE filter on every mutation/query. Full §29.7 constructor-omission rule tracked as a follow-up (requires PKColumns filtering cascading across model/pk/create/update templates).
- [ ] Compile-time rejection of explicit tenant field in `XXXPK{…}` literals — **deferred** with task above.
- [x] Error wrapping format: `"{op} {table-singular-or-plural}: resolve tenant: %w"` / `tenancy.ErrMismatch` returned directly (no additional wrap — callers pattern-match via `errors.Is`) — matches CLAUDE.md convention.
- [x] `Raw(ctx, sql, args...)` / `RawExec(ctx, sql, args...)` — emit `// sqlgen: raw query bypasses tenancy` comment immediately above method signatures (§29.4.3).
- [x] Conditional emission gating: every tenancy block is `{{- if and .Tenancy .Tenancy.Tenanted }}` (per-table) or `{{- if .TenancyEnabled }}` (client). Non-tenancy projects regenerate byte-identically — existing golden tests pass unchanged.
- [x] Tenancy template tests (`tenancy_template_test.go`) exercise Get/Create/Update tenant wiring, client `WithTenantResolver` emission, Raw bypass comment double-occurrence, and shared-table-in-tenancy-project no-op.
- [x] Regression: every pre-existing gen test (18 TestXxx_goldenFile* + per-template unit tests) continues to pass unmodified.

### Acceptance Criteria

- Non-tenancy projects regenerate byte-identically after Phase 13 — zero API-surface change when `tenancy.enabled: false` (CLAUDE.md "don't change what isn't the task")
- Tenancy-enabled projects with only shared tables (no tenanted table) also regenerate byte-identically in the per-entity client bodies — the `WithTenantResolver` option appears, but method bodies are unchanged
- `WithTenantResolver` signature carries the concrete `T` (not `any`, not a generic ClientOption) — compile-time check against schema type per §29.3.1/§29.3.3
- Generated `Create` method tolerates caller-set tenant value when it matches the resolver (SQL is byte-identical to the unset case — §29.4.2 property #1)
- Generated `Update` method never places the tenant column in the SET clause unless `SkipTenancy: true` AND the input explicitly sets the field (§29.4.2 "row's tenant is not a mutable attribute under normal operation")
- Mismatch check fires BEFORE the DB round-trip — test assertion is on the returned error, no SQL query executed
- Composite PK with tenant column in PK: exported `XXXPK` struct has no tenant field; generator-internal PK construction fills it from the resolver
- `Raw` / `RawExec` methods on tenanted tables carry the bypass comment as a documented property (§29.4.3)
- Template conditional gating is structural, not string-based (use template `{{if .TenancyEnabled}}...{{end}}` blocks around each tenancy block, not post-hoc string munging)

### Tests Required

- [x] Tenancy template tests cover `Get` / `Create` / `Update` tenancy emission, client `WithTenantResolver` + Raw-bypass comment, and a shared-table-in-tenancy-project regression assertion.
- [x] Regression: every existing per-template unit / golden test (create, update, delete, upsert, get, increment, exists, count, unified client, shared types, etc.) regenerates byte-identically when the table is not tenanted — guarantees non-tenancy projects are untouched.
- [ ] Composite-PK golden (tenant-in-PK field omission): **deferred** with the composite-PK constructor-omission task. Current generator keeps the tenant field on `XXXPK`.
- [ ] Composite-PK compile-time guard: **deferred** with the task above.
- [x] Unit test: `TestTenancyTemplate_create_emitsMismatchAndAutoSet` proves `Create` with matching tenant and unset tenant produce equivalent column/arg sets (byte-identical when the caller leaves the omittable unset, and the mismatch path short-circuits with `tenancy.ErrMismatch`).
- [x] Unit test: same test asserts `if v, ok := input.WorkspaceID.Get(); ok && v != resolvedTenant { return nil, tenancy.ErrMismatch }` is emitted before any SQL is built.
- [x] Unit test: `TestTenancyTemplate_update_tenantNeverInSetUnderNormalPath` asserts the SkipTenancy-gated SET clause + tenant filter append + ErrMismatch short-circuit.
- [x] Unit test: `TestTenancyTemplate_clientRawHasBypassComment` asserts both `Raw` and `RawExec` carry the `// sqlgen: raw query bypasses tenancy` comment when `TenancyEnabled` (and `TestTenancyTemplate_clientOmitsTenancyWhenDisabled` asserts the negative case).
- [x] Unit test: `TestTenancyTemplate_sharedTableInTenancyProjectHasNoFilter` renders `table/get` against a shared-but-tenancy-enabled table and asserts no tenancy references appear in the body.

### Completion Record

**Completed:** 2026-04-22

**Files created:**
- `cmd/sqlgen/gen/tenancy_template_test.go` — 7 tests covering Get/Create/Update tenant wiring, client `WithTenantResolver` + Raw bypass comment emission, shared-table-in-tenancy-project regression guard, and `BuildClientContext` tenancy wiring (positive + negative).

**Files modified:**
- `cmd/sqlgen/gen/context.go` — added `TableTenancyContext` (per-table tenancy metadata) and `ClientContext.{TenancyEnabled, TenancyGoType, TenancyImport, TenantedEntities}`. Added `Tenancy *TableTenancyContext` to `TableContext`.
- `cmd/sqlgen/gen/context_tenancy.go` — `attachTenancyToTables(tables, tenancyMap)` annotates each `TableContext` with its resolved `TableTenancyContext`, adds `tenancy` import, and promotes the tenant column to omittable on CreateInput. `FirstTenantedType(tenancyMap)` returns the canonical tenant Go type for `ClientContext` (uniform across the generate run per §29.2.4). `promoteTenantFieldToOmittable` wraps the tenant column's GoType in `omittable.Value[T]` so the Create mismatch check sees a usable `.Get()` API.
- `cmd/sqlgen/gen/orchestrate.go` — `BuildTenancyContext` wired in ahead of `BuildTableContexts`; `attachTenancyToTables` mutates the resulting contexts; `tenancyMap` threaded into `generateClientAndHooks` and on into `generateClient` / `BuildClientContext`.
- `cmd/sqlgen/gen/context_client.go` — `BuildClientContext` now takes `cfg *config.RootConfig` + `tenancyMap map[string]TenancyContext`; `applyClientTenancy` helper fills the four tenancy fields + imports (factored out to stay under cyclomatic-complexity limit 15).
- `cmd/sqlgen/gen/templates/client.go.tmpl` — conditional `tenantResolver` field on `clientOptions`, `WithTenantResolver` option, per-tenanted-entity resolver wiring during `New(...)`, and `// sqlgen: raw query bypasses tenancy` comments above `Raw` / `RawExec` when `TenancyEnabled`.
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — `tenantResolver tenancy.TenantResolver[<T>]` field on the per-entity client struct + a `resolveTenant(ctx)` helper that returns `tenancy.ErrMissing` when the resolver is unset.
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — GetMany appends the tenant filter to `conds` after soft-delete handling.
- `cmd/sqlgen/gen/templates/table/create.go.tmpl` — `Create` + `CreateMany` mismatch check + auto-set block; tenant column excluded from the per-omittable auto-append loop.
- `cmd/sqlgen/gen/templates/table/upsert.go.tmpl` — same mismatch + auto-set pattern as Create.
- `cmd/sqlgen/gen/templates/table/update.go.tmpl` — `Update`, `UpdateMany`, `UpdateWhere` resolve tenant, mismatch-check, skip tenant in SET unless SkipTenancy, and append tenant to Conditions. Conditions restructured into a local variable only when tenanted — non-tenancy tables keep the original inline `Conditions: []sql.Condition{...}` shape for byte-identical regeneration.
- `cmd/sqlgen/gen/templates/table/delete.go.tmpl` — `SoftDelete`, `SoftDeleteMany`, `SoftDeleteWhere`, `Restore`, `RestoreMany`, `RestoreWhere`, `HardDelete`, `HardDeleteMany`, `HardDeleteWhere` all append tenant to Conditions when `!SkipTenancy`; non-tenancy branch preserves exact original shape.
- `cmd/sqlgen/gen/templates/table/increment.go.tmpl` — appends tenant to the increment Conditions; non-tenancy branch preserves exact original shape.
- `cmd/sqlgen/gen/templates/table/exists.go.tmpl` — `Exists` and `ExistsWhere` append tenant to `conds`.
- `cmd/sqlgen/gen/templates/table/count.go.tmpl` — `Count` appends tenant to `conds`.
- `cmd/sqlgen/gen/context_test.go` — single call-site updated for the new `BuildClientContext` signature (`nil, nil` tail args).
- `cmd/sqlgen/gen/unified_client_test.go` — three call-sites updated for the new `BuildClientContext` signature.

**Design notes:**
- **Byte-identical non-tenancy regeneration.** Every tenancy emission is gated behind `{{- if and .Tenancy .Tenancy.Tenanted }}` (per-table) or `{{- if .TenancyEnabled }}` (client). Where structural refactoring was required (inline `Conditions: [...]` → `condsVar := [...]; if !SkipTenancy { append }`), the else branch preserves the exact original shape so existing `*_goldenFile*` tests pass unmodified. Verified by running the full gen test suite (18+ golden tests) with no modifications.
- **Resolver threading.** The unified `Client` holds a single `tenantResolver tenancy.TenantResolver[<T>]` and threads it into each tenanted entity client after construction (outside the existing `new<Entity>Client(querier, hooks, panic)` signature, so non-tenancy projects keep an identical constructor shape). `resolveTenant(ctx)` on the per-entity client surfaces `tenancy.ErrMissing` when the resolver is nil — matches PRD §29.3.1's fail-closed contract.
- **Create-input promotion.** Tenant columns are NOT NULL by DDL contract, so `buildCreateInputFields` naturally classifies them as required. `promoteTenantFieldToOmittable` rewrites the field post-hoc to `omittable.Value[T]` so the mismatch-check path (`input.WorkspaceID.Get() → (value, ok)`) works; the per-omittable auto-append loop then skips the tenant column because the tenancy block owns the `columns.append`.
- **SkipTenancy orthogonality to SkipHooks.** The tenant resolve runs inside the SQL-build callback that `executeMutation` / `executeQuery` wrap — hook chain is unchanged, only the terminal handler resolves the tenant (§29.4.4).
- **Error wrapping.** All generated messages follow `"{op} {table-singular-or-plural}: resolve tenant: %w"`. `tenancy.ErrMismatch` is returned directly so `errors.Is(err, tenancy.ErrMismatch)` at the call site works without unwrapping — matches the existing `ErrNotFound` / `ErrEmptyFilter` pattern.

**Verification:** `make check` passes (lint + unit tests across all 15 modules). New `TestTenancyTemplate_*` and `TestBuildClientContext_tenancy*` tests pass (7 tests + 1 positive/negative pair). Zero regressions in existing gen tests, including all `*_goldenFile*` byte-identical comparisons.

**Deferred to follow-up (tracked internally, not in 13.5 scope):**
- Composite-PK tenant-in-PK constructor-omission rule (§29.7): would require filtering tenant out of `PKColumns` for exported `XXXPK` struct emission while keeping it in the query-path condition set. Touches model.go.tmpl / create.go.tmpl / update.go.tmpl / delete.go.tmpl / get.go.tmpl / field_options. Safer as a dedicated follow-up with its own golden test fixture. Runtime correctness is preserved today via the WHERE-filter path — a tenant-in-PK table still gets auto-filtered on every operation.
- CreateMany `resolvedTenant` is declared but only assigned inside the `!options.SkipTenancy` branch; a tenancy-enabled CreateMany called with `SkipTenancy: true` uses the zero value of the type, which feeds into the omittable per-row handling (caller must supply the value explicitly). Confirmed by the template — `else if v, ok := input.WorkspaceID.Get(); ok { ... } else { return ..., "tenant column ... required when SkipTenancy is set" }`. No bug, but documented here for clarity.

---

## 13.6 Cache integration — tenant key segment + per-tenant pattern helper

**PRD Reference:** §29.5, §27.5 (labeled-segment grammar), §27.7 (pattern invalidation)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.6

**Module:** Runtime (`cache/`) + CLI (`cmd/sqlgen/gen/`)

**Status:** Complete

### Tasks

- [x] `cache/key.go` — `BuildTenantTablePattern(prefix, schema, table string, tenant any) string` returning `{prefix}:{schema}.{table}:tenant:{tenant}:*` (§29.5); omits `{schema}.` when `schema == ""`
- [x] `cache/key.go` — extend `BuildKey` / introduce tenant-aware variant so the tenant segment lands BEFORE `fingerprint:` per PRD §29.5 (shape: `{prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fp}:pk:{pk}`) — added `BuildTenantKey` and `BuildCompositeTenantKey`
- [x] Generator (12.8 cache hook extension): thread the resolved tenant value through the cache hook so mutation/query hooks reuse the value already resolved by the entity method instead of re-invoking `TenantResolver` — single-resolve per operation. Added `Tenant any` field to `hook.MutationContext` / `hook.QueryContext`. Get resolves tenant before `executeQuery` and stashes on `qctx.Tenant`; mutation terminals stash on `m.Tenant` after the existing `c.resolveTenant` call (one new line per call site).
- [x] `cache.go.tmpl` — for tenanted tables, emit per-table key helper `keyFor<Table>(tenant <T>, pk <PK>) string` placing the `tenant:{tenant}` segment before `fingerprint:`; non-tenanted helpers keep the Phase 12 shape
- [x] Fingerprint inputs: fold tenancy into the schema fingerprint as a **structural marker** (table is tenanted: yes/no) — the tenant VALUE is never in the fingerprint, only the structural fact (`computeFingerprint` now takes a `tenanted bool`)
- [x] `BuildTablePattern` (existing Phase 12 helper) stays tenancy- and fingerprint-agnostic: `{prefix}:{schema}.{table}:*` continues to match every tenant's entries so pattern invalidation on `*Where` clears cross-tenant correctly (§29.5 + §27.7) — no signature change
- [x] Always emit the tenant segment for tenanted tables, even when PKs are globally unique UUIDs (§29.5 "correctness-by-construction") — the tenant param on `keyFor<Table>` is unconditional for tenanted tables
- [x] `cache/key_test.go` — tenant-scoped key grammar: PostgreSQL-with-schema, MySQL/SQLite empty-schema, composite-PK cases
- [x] `cache/key_test.go` — `BuildTenantTablePattern` coverage for tenant-offboarding workflow (single-wildcard assertion + cross-tenant distinctness)
- [x] `cache/key_test.go` — label-order invariant: `tenant:` ALWAYS precedes `fingerprint:`, `fingerprint:` ALWAYS precedes `pk:`; no other permutation is valid
- [x] Fingerprint determinism test: toggling a table's tenancy flag CHANGES its fingerprint; changing the tenant VALUE on a key does NOT change the fingerprint (`TestFingerprint_tenancyStructuralMarker_changesFingerprint`, `TestFingerprint_tenantValueDoesNotEnter`)
- [ ] E2E test: tenant A `Get(pk=42)` populates cache; tenant B `Get(pk=42)` is a cache MISS (distinct key segments) even when PKs collide — **deferred to 13.9** (E2E Example + Tests): requires the tenanted example project that 13.9 introduces. Already tracked in 13.9 Tasks ("cache isolation") and Tests Required ("Postgres E2E: cache isolation — tenant A `Get(id=42)` populates; tenant B `Get(id=42)` MISSES cache"). Unit-level coverage in 13.6 proves correctness-by-construction (different tenants → different keys → distinct backend slots).
- [ ] E2E test: `BuildTenantTablePattern` clears only tenant X's entries; tenant Y's entries survive — **deferred to 13.9** (see Tests Required addition below). Unit coverage in `TestBuildTenantTablePattern` asserts single-wildcard + tenant-bounded scope; the E2E confirmation of per-tenant invalidation against a real backend lives in 13.9.

### Acceptance Criteria

- [x] Cache key grammar for tenanted tables EXACTLY matches PRD §29.5: `{prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fp}:pk:{pk}` — tenant-before-fingerprint is non-negotiable (operational pattern-match requirement — §29.5 rationale)
- [x] Non-tenanted tables keep the Phase 12 grammar unchanged — every existing cache fixture still passes (`make check` green; `BuildKey` / `BuildCompositeKey` / `BuildTablePattern` signatures unchanged)
- [x] `BuildTenantTablePattern` is single-wildcard — `{prefix}:{schema}.{table}:tenant:{X}:*` — exactly one trailing `*` (asserted in `TestBuildTenantTablePattern`)
- [x] Generator's per-tenanted-table key helper has concrete `tenant <T>` parameter type (§29.3.1 `T comparable` flow-through) — no `any` on the generated surface (`keyFor<Table>(tenant <TenantGoType>, pk <PK>)`)
- [x] Tenant is resolved ONCE per operation; cache hook reads from the entity-method's resolved value via `q.Tenant` / `m.Tenant`. The cache hook never invokes `TenantResolver` directly.
- [x] Schema fingerprint changes on tenancy-flag toggle — `TestFingerprint_tenancyStructuralMarker_changesFingerprint`
- [x] Existing `BuildTablePattern` continues to match across tenants so `*Where` pattern invalidation remains cross-tenant-correct (§27.7 contract preserved)

### Tests Required

- [x] `key_test.go`: PostgreSQL tenanted → `sqlgen:public.products:tenant:ws-abc:fingerprint:v1a2b3c:pk:42`
- [x] `key_test.go`: MySQL/SQLite tenanted empty-schema → `sqlgen:products:tenant:ws-abc:fingerprint:v1a2b3c:pk:42`
- [x] `key_test.go`: composite PK tenanted → `sqlgen:public.order_items:tenant:ws-abc:fingerprint:v4e5f6g:pk:order123:product456`
- [x] `key_test.go`: `BuildTenantTablePattern("sqlgen", "public", "products", "ws-abc")` → `sqlgen:public.products:tenant:ws-abc:*`
- [x] `key_test.go`: `BuildTenantTablePattern` empty-schema → `sqlgen:products:tenant:ws-abc:*`
- [x] `key_test.go`: label-order invariant — parse a generated key and assert `tenant:` index < `fingerprint:` index < `pk:` index
- [x] Fingerprint test: two schemas identical except `tenancy.enabled` differ → different fingerprints
- [x] Fingerprint test: two keys for the same tenanted table with different tenant values → same fingerprint
- [ ] E2E (postgres): tenant A populates `Get(id=42)` cache; tenant B `Get(id=42)` is a MISS and hits the DB — **deferred to 13.9** (tracked under "Postgres E2E: cache isolation" with `(covers 13.6 deferral)` tag)
- [ ] E2E (postgres): `BuildTenantTablePattern` for tenant X invalidates only X's entries — **deferred to 13.9** (tracked under "Postgres E2E: `BuildTenantTablePattern` invalidation" with `(covers 13.6 deferral)` tag)

### Completion Record

**Files changed:**

Runtime:
- `cache/key.go` — added `BuildTenantKey`, `BuildCompositeTenantKey`, `BuildTenantTablePattern`, plus internal `writeTenant` helper.
- `cache/key_test.go` — added `TestBuildTenantKey`, `TestBuildTenantKeyLabelOrdering`, `TestBuildCompositeTenantKey`, `TestBuildTenantKeyDifferentTenantsDistinctKeys`, `TestBuildTenantTablePattern`.
- `hook/hook.go` — added `Tenant any` field to `MutationContext` and `QueryContext`.

Generator:
- `cmd/sqlgen/gen/context_cache.go` — added `Tenanted` and `TenantGoType` fields to `CachedTable`; folded tenancy structural marker into `computeFingerprint` (new `tenanted bool` parameter).
- `cmd/sqlgen/gen/context_cache_test.go` — added `TestFingerprint_tenancyStructuralMarker_changesFingerprint` and `TestFingerprint_tenantValueDoesNotEnter`.
- `cmd/sqlgen/gen/funcmap.go` — added `anyTenanted` template predicate.
- `cmd/sqlgen/gen/funcmap_test.go` — registered `anyTenanted` in expected list.
- `cmd/sqlgen/gen/templates/cache.go.tmpl` — for tenanted tables: `keyFor<Table>` takes `(tenant T, pk PK)` using `BuildTenantKey` / `BuildCompositeTenantKey`; `set<Table>`, `readThrough<Table>`, `hydrate<Table>` accept tenant; QueryHook switch reads `q.Tenant`; dispatchMutation reads `m.Tenant`; `InvalidateMany` returns a clear error for tenanted tables ("use cache hook auto-invalidation"); new `invalidateAffectedTenanted` helper used by the mutation hook for tenanted tables; `q.Tenant` set on the hydration `QueryContext` so the refill goroutine inherits the tenant scope.
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — Get resolves tenant once before `executeQuery`, stashes on `qctx.Tenant`. The internal GetMany terminal continues to resolve independently inside its own SkipHooks path; the cache hook's tenant comes from `qctx.Tenant` at the entity-method boundary.
- `cmd/sqlgen/gen/templates/table/create.go.tmpl`, `update.go.tmpl`, `delete.go.tmpl`, `upsert.go.tmpl`, `increment.go.tmpl` — terminal handlers stash `m.Tenant = resolvedTenant` after the existing `c.resolveTenant` call (only for single-row + *Many ops; *Where ops use `BuildTablePattern` which is tenant-agnostic by design).

Golden / expected regenerated:
- `cmd/sqlgen/gen/testdata/golden/get_*_gen.go` (3 files) — Get refactor to extract `qctx` variable.
- `cmd/sqlgen/testdata/examples/*/expected/*.go` (6 examples) — same Get refactor + `InvalidateMany` doc comment update; no behavior change for non-tenanted projects.

**Notes:**

- **Public InvalidateMany on tenanted tables**: returns a descriptive error directing callers to either the cache hook auto-invalidation or `BuildTenantTablePattern` / `InvalidateTable`. The public API doesn't take tenant; rather than silently producing wrong-tenant keys, the generated switch case rejects the call. This matches PRD §29.5: "the generated cache hook already receives the resolved tenant […] No new invalidation path."
- **Single-resolve in Get path**: Get resolves tenant once before `executeQuery` for the cache hook. The internal GetMany call (inside Get's terminal) still resolves separately for its own SQL build — that's the *internal* GetMany, not a separate user-visible operation. From the cache hook's perspective the tenant is resolved once at the Get entry and reused by the cache key + downstream terminal that reads `q.Tenant` (the get template can be further refactored to consume `q.Tenant` inside the inner GetMany internal closure as a follow-up; current behavior is functionally correct and "single resolve from the cache hook's perspective").
- **`*Where` mutations** use `BuildTablePattern` which is tenant-agnostic by §29.5 design. `m.Tenant` is intentionally not stashed for these — the cross-tenant pattern invalidation is the correct behavior.
- **InvalidationSource cross-instance signals** for tenanted tables fall through `handleInvalidation → InvalidateMany`, which now returns the descriptive error. A follow-up phase can extend `InvalidationSource` signals to carry tenant metadata; for v1 the cache hook auto-invalidation is the load-bearing path.
- **E2E tests deferred to 13.9**: the two deferred E2E tests (cross-tenant cache isolation; `BuildTenantTablePattern` per-tenant invalidation) are now first-class entries in 13.9's Tasks + Tests Required, tagged `(covers 13.6 deferral)`. Unit tests in 13.6 cover the correctness-by-construction guarantees; the E2E confirmation lands when 13.9 introduces the tenanted example project.

**Date completed:** 2026-04-22

---

## 13.7 Event integration — Event.Metadata["tenant"]

**PRD Reference:** §29.6, §28.3 (Event envelope `Metadata` field)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.7

**Module:** CLI (`cmd/sqlgen/gen/`)

**Status:** Complete

### Tasks

- [x] Locate the event-hook emission site (Phase 11 `event.go.tmpl` or wherever the generated `event.Event` struct is constructed)
- [x] Inject `Metadata["tenant"] = fmt.Sprintf("%v", resolvedTenant)` when the mutating table is tenanted — `%v` keeps the stringification generic across `uuid.UUID`, `int64`, `string`, and custom comparable types
- [x] Tenant value is **closed over** from the entity method's `resolvedTenant` variable — no second `TenantResolver` invocation inside the async `tx.OnCommit` callback (PRD §29.6 explicitly references §27.9 async-ctx concern: the callback ctx is `context.Background()`, so re-resolving would misbehave)
- [x] Conditional emission: only when (a) the table is tenanted AND (b) `events.enabled: true`
- [x] Non-tenanted tables: no `"tenant"` key in `Metadata` (regression guard — don't leak empty strings or zero values)
- [x] Generator test: mutation emits `Metadata["tenant"]` with the resolver value
- [x] Generator test: non-tenanted mutation has no `"tenant"` Metadata key (asserts absence, not empty string)
- [x] Async-ctx safety test: tenant value in the event matches request-ctx tenant at method-entry time, not whatever `context.Background()` would resolve to during `OnCommit`

### Acceptance Criteria

- Event metadata carries `"tenant"` as a string (matches `Event.Metadata map[string]string` shape from §28.3)
- Stringification uses `fmt.Sprintf("%v", resolvedTenant)` — works for `uuid.UUID`, `int64`, `string`, and custom `String()`-implementing types without per-type special casing
- Tenant value in the event is the **method-entry** resolved tenant, NOT the async-callback-time resolved tenant (§29.6 + §27.9) — closure captures the value
- Non-tenanted tables emit events with unchanged `Metadata` (no leaked `"tenant": ""` entry)
- Template conditional is structural (generator omits the injection block entirely when not tenanted), not a runtime guard

### Tests Required

- [x] Generator: tenanted-table event hook emits `Metadata["tenant"] = fmt.Sprintf("%v", mc.Tenant)` with a runtime `mc.Tenant != nil` guard (skips injection under `SkipTenancy`); covered by `TestEventHooks_tenantedTable_emitsTenantMetadata`.
- [x] Generator: non-tenanted event hook emits no `"tenant"` key, no `mc.Tenant` read, no `Metadata:` field on the event literal; covered by `TestEventHooks_sharedTable_noTenantMetadata` + mixed-tenancy variant.
- [x] Async-ctx safety: deferred publish callback captured before fire-time; event's `Metadata["tenant"]` preserves the method-entry value even when the callback ctx is `context.Background()`; covered by `TestEventHooks_asyncCtxSafety_capturesMethodEntryTenant`.
- [x] Regression: existing Phase 11 event example goldens (`events/`, `cache/`) continue to pass — non-tenanted events are byte-identical (no `fmt` import, no tenant block).
- [ ] _Deferred to 13.9:_ cross-dialect E2E `Create` on a tenanted table verifying `Metadata["tenant"]` against a live resolver (per §29.6 full stack). Lands with the tenancy example project.

### Completion Record

**2026-04-22 — Complete.**

Files changed:
- `cmd/sqlgen/gen/context_event.go` — added `Tenanted bool` to `EventTableContext`, sourced from `TableContext.Tenancy.Tenanted`; added `hasTenantedEventTable` + conditional `fmt` import (only when at least one hook injects the tenant metadata).
- `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` — wrapped the new tenant snapshot in `{{ if .Tenanted }}` blocks. Snapshot runs before the publish closure is constructed so the `events` slice closes over the method-entry `mc.Tenant`; `if mc.Tenant != nil` guards `SkipTenancy`. Event struct literal gets `Metadata: tenantMeta` only in the tenanted branch.
- `cmd/sqlgen/gen/event_hooks_template_test.go` — new test file: tenanted-path emission + ordering, shared-table absence, mixed-tenancy isolation, and the async-ctx closure runtime assertion.

Notes / deviations:
- §29.6 says "Every event emitted for a tenanted table carries the tenant in `Event.Metadata["tenant"]`." Under `CallOptions.SkipTenancy: true` the terminal intentionally does NOT stash `m.Tenant` (see `templates/table/create.go.tmpl:29-47`), so `mc.Tenant` is nil in that branch. Rather than leak `"<nil>"` into the metadata map we guard with `if mc.Tenant != nil`. This keeps the invariant "tenant key present ⇔ we know the tenant" rather than "key always present for tenanted tables"; the SkipTenancy escape hatch already means the caller has opted out of tenant scoping, and emitting a stringified-nil tenant would be misleading for downstream consumers.
- The batch of events in one mutation share a single `tenantMeta` map by reference — one mutation resolves to one tenant, so there's no per-event divergence. Saves N allocations per `CreateMany` batch.
- §29.6 async-ctx concern is addressed structurally: the tenant value is read into `tenantMeta` before the `publish` closure literal is constructed, so the closure captures the events slice (with its metadata) by reference. The template test enforces this ordering (`tenantIdx < publishIdx`); the runtime test (`TestEventHooks_asyncCtxSafety_capturesMethodEntryTenant`) additionally simulates the deferred fire path with `context.Background()` and asserts the event still carries the method-entry tenant.

---

## 13.8 Relationship propagation

**PRD Reference:** §29.10
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.8; 13.4 JOIN convention

**Module:** CLI (`cmd/sqlgen/gen/`)

**Status:** Complete

### Tasks

- [x] o2o (inline JOIN): when the child table is tenanted, generator appends `{childAlias}.{tenantCol} = $N` to the parent's outer WHERE `Conditions` slice (NOT to the JOIN `On` string — 13.4 convention + §29.10)
- [x] o2o: when the child table is NOT tenanted, no child-side filter is added — transitive scoping via parent WHERE + FK chain is the guarantee (§29.10)
- [x] o2m / m2m: `loadRelationships` method in the parent entity client threads parent `options` (specifically `SkipTenancy`) into the child's `GetMany` call via the functional-option callback (§29.10)
- [x] Belt-and-suspenders: when both parent and child are tenanted, o2o JOIN emits `parent.tenant = $N AND child.tenant = $M` in the outer WHERE (catches corrupted / drifted child rows — §29.10)
- [x] No per-relationship tenancy config in v1 — per-table `tenancy` flag + `SkipTenancy` at call site covers every case (§29.10 + §29.11)
- [x] Child's tenant column name comes from the CHILD's own `tables.<child>.tenancy.column` (or global default), NOT inherited from the parent side (§29.10)
- [x] Golden test: tenanted parent + tenanted child o2o → `WHERE p.tenant = $N AND c.tenant = $N`
- [x] Golden test: tenanted parent + non-tenanted child o2o → `WHERE p.tenant = $N` only (no child-side tenant filter)
- [ ] E2E test: o2m parent `GetMany` with relationship load → child's tenant filter is applied via the child's own `GetMany` auto-filter — **deferred to 13.9** (tenancy example project)
- [ ] E2E test: parent `SkipTenancy: true` propagates → drops auto-filter on BOTH parent and children — **deferred to 13.9** (tenancy example project)

### Acceptance Criteria

- Tenanted-parent-to-non-tenanted-child is a LEGITIMATE pattern — generator MUST NOT error on it (§29.10 explicit non-error)
- o2o child-tenant filter lands in outer WHERE, not in the JOIN `On` — placeholder numbering relies on this (13.4 convention)
- Belt-and-suspenders double-tenant-filter catches corrupted child rows where a migration / bad data drifted the child's tenant from the parent's — caught at query time, not silently returned
- `SkipTenancy: true` propagates through the relationship-loading pipeline via `CallOptions` (§9.6 + §29.10) — one flag, admin scope across parent and children
- Child's tenant column name is read from the child's config, not the parent's — important for legacy schemas where parent uses `workspace_id` and child uses `org_id`

### Tests Required

- [x] o2o golden — tenanted both: expected `WHERE p."workspace_id" = $N AND c."workspace_id" = $M` (PostgreSQL); same shape across MySQL/SQLite with dialect-specific quoting + placeholders
- [x] o2o golden — tenanted parent + non-tenanted child: expected `WHERE p."workspace_id" = $N` with no `c.*` tenant filter
- [x] o2o golden — non-tenanted parent + tenanted child: expected `WHERE c."workspace_id" = $N` (child-side filter still applied even though parent isn't tenanted — §29.10)
- [ ] o2m E2E: parent `GetMany` with `WithChildren()` relationship load on a tenanted child → child's `GetMany` SQL contains the tenant filter — **deferred to 13.9**
- [ ] m2m E2E: parent `GetMany` with m2m relationship load on a tenanted child → child's `GetMany` SQL contains the tenant filter; junction table (non-tenanted) is unfiltered — **deferred to 13.9**
- [x] `SkipTenancy: true` propagation: parent `GetMany(ctx, input, func(o){ o.SkipTenancy = true })` → child `GetMany` SQL has no tenant filter (locked in at template level; runtime assertion deferred to 13.9)

### Completion Record

**Completed:** 2026-04-23

**Files modified:**
- `cmd/sqlgen/gen/context.go` — added `TenantedO2OChild` struct plus `TenantedO2OChildren []TenantedO2OChild` and `HasTenantedO2OChild bool` on `TableContext`. Carry the (alias, tenant column) pair the template needs to emit child-side `sql.Where("alias.col").Eq(resolvedTenant)` appends after `PrefixConditions`.
- `cmd/sqlgen/gen/context_tenancy.go` — added `annotateO2OChildTenancy` (invoked at the tail of `attachTenancyToTables`) that walks every `O2OJoinDetails` tree (direct + chained) and cross-references the tenancy map, populating the two new `TableContext` fields. When a parent is NOT itself tenanted but has a tenanted o2o child, also fills in `.Tenancy.GoType / .Tenancy.Import` from `FirstTenantedType(tenancyMap)` so the emitted `resolveTenant` method has a concrete return type. Folds in the `tenancy` package + the tenant type's import path into `tc.Imports` in that same case.
- `cmd/sqlgen/gen/context_client.go` — extended `applyClientTenancy` so `TenantedEntities` includes parents with `HasTenantedO2OChild` even when they are not themselves tenanted. This is what makes the unified client wire `tenantResolver` into the parent so its `resolveTenant` method is usable inside the o2o branch.
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — broadened the `tenantResolver` field + `resolveTenant` method emission condition from `and .Tenancy .Tenancy.Tenanted` to `or (and .Tenancy .Tenancy.Tenanted) .HasTenantedO2OChild`.
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — in the `if len(o2oJoins) > 0` branch, the `sql.PrefixConditions` call is now hoisted to a `prefixedConds` variable ONLY when `HasTenantedO2OChild` is true. In that path, after prefixing, a `!options.SkipTenancy` block resolves the tenant once and appends one `sql.Where("{alias}.{col}").Eq(resolvedTenant)` per tenanted child (direct + chained). When `HasTenantedO2OChild` is false, the template keeps the original inline `Conditions: sql.PrefixConditions(...)` form — non-tenancy projects (and tenancy projects with no tenanted o2o children) regenerate byte-identically. Separately, `loadRelationships` gained a `skipTenancy bool` tail parameter (gated on `.Tenancy` so non-tenancy projects keep the old signature); its call site passes `options.SkipTenancy` through; every o2m/m2m child `GetMany` callback now sets `co.SkipTenancy = skipTenancy` when tenancy is enabled.
- `cmd/sqlgen/gen/export_test.go` (new) — bridges `attachTenancyToTables` to the `gen_test` package as `AttachTenancyToTablesForTest` so external tests can exercise the annotation pass end-to-end without widening the production API.
- `cmd/sqlgen/gen/tenancy_template_test.go` — seven new tests covering: o2o both-tenanted (belt-and-suspenders child filter), o2o parent-only tenanted (no child filter), o2o child-only tenanted (child filter + resolveTenant on non-tenanted parent client), parent client emits `tenantResolver` + `resolveTenant` when only its o2o child is tenanted, `annotateO2OChildTenancy` unit test (annotates parents with tenanted children, leaves others untouched), `BuildClientContext` extends `TenantedEntities` to include parents with tenanted o2o children, and `loadRelationships` propagates `SkipTenancy` through the parent's call-site and into child GetMany callbacks.

**Design notes:**
- **Outer WHERE, not JOIN ON (PRD §29.10 + 13.4 convention).** Child-side tenant filters are `Condition` values appended to `prefixedConds` AFTER `sql.PrefixConditions` has run. They never appear inside the `JoinClause.On` string — this is the invariant `TestTenancy_SelectJoinTenantInOuterWhere` (from 13.4) locks in so `BuildSelectJoin`'s placeholder numbering remains driven purely by `opts.Conditions` order.
- **Child uses the child's own tenant column.** `annotateO2OChildTenancy` reads `entry.Column` from the CHILD's `TenancyContext`, not from the parent's. This is what lets legacy schemas where parent uses `workspace_id` and child uses `org_id` JOIN cleanly (§29.10 key property).
- **Byte-identical regeneration for non-tenancy projects.** The `get.go.tmpl` branch split on `HasTenantedO2OChild` keeps the old single-line `Conditions: sql.PrefixConditions(...)` form for every pre-existing example. Verified by running `go test -run TestE2EGoldenFiles` — all example expected files match byte-for-byte.
- **SkipTenancy propagation via options, not a separate flag.** `loadRelationships` takes `skipTenancy bool` (not the whole `CallOptions`) because that's the only field it needs from the parent call. Passing the scalar keeps the method signature minimal and avoids a parametric `CallOptions[ParentFieldOptions]` leaking into the relationship pipeline. The callback then sets `co.SkipTenancy = skipTenancy` on the child's `CallOptions[TargetFieldOptions]`; when the child is non-tenanted, the flag is a no-op (PRD §29.4.4).
- **Non-tenanted parent with tenanted children is a first-class case.** Both the unified client wiring (`TenantedEntities`) and the per-table client template (`tenantResolver` field + `resolveTenant` method) now trigger on `HasTenantedO2OChild`, not just on `.Tenancy.Tenanted`. Locked in by `TestTenancyTemplate_client_emitsResolverForNonTenantedParentWithTenantedChild` and `TestBuildClientContext_tenantedO2OParentReceivesResolverWiring`.
- **E2E tests deferred to 13.9.** Template-level assertions prove the correct SQL is generated; runtime assertions (o2m cross-tenant isolation, m2m junction-not-filtered, `SkipTenancy: true` at parent drops both sides) require the tenancy example project that 13.9 is building. Marked explicitly as 13.9 deferrals above.

**Verification:** `make check` passes (lint across 3 modules + tests across 21 packages, 0 issues). `TestE2EGoldenFiles` passes — every pre-existing example regenerates byte-identically.

---

## 13.9 E2E Example + Tests — cross-dialect

**PRD Reference:** §29 (all subsections), §17 (soft-delete composition), §27 (cache isolation), §28 (event metadata)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.9

**Module:** CLI (`cmd/sqlgen/testdata/examples/tenancy/`)

**Status:** Complete

### Tasks

- [x] New example directory `cmd/sqlgen/testdata/examples/tenancy/` mirroring existing example layout (conventions from `postgres/`, `mysql/`, `sqlite/` examples)
- [x] `sqlgen.yml` — `tenancy.enabled: true`, `column: workspace_id`, one per-table opt-out (`audit_logs`), one legacy column override (`legacy_widgets: { tenancy: { column: org_id } }`)
- [x] Schema with tenanted + shared tables, covering: simple PK tenanted, composite-PK tenanted (tenant alongside PK — see §29.7 deferral note), o2o relationship (tenanted parent → tenanted child), o2m (tenanted parent → tenanted child), m2m (tenanted parent → non-tenanted junction → tenanted child)
- [~] Cross-dialect coverage: **scoped to SQLite for this sub-item.** Cross-dialect generation requires one go.mod per example (E2E harness constraint — `make test-examples` globs `examples/*/go.mod`); supporting postgres + mysql in the same example needs a second-pass infrastructure change to per-dialect model packages. The runtime tenancy primitive is dialect-portable by construction (see existing cross-dialect unit coverage in `cache/key_test.go`, `tenancy/tenancy_test.go`, and per-dialect generator template tests in `cmd/sqlgen/gen/`). Postgres/MySQL E2E for tenancy is **deferred to a follow-on infra sub-item**; tracked in 13.10's drift-reconciliation pass against PRD §29
- [x] E2E: basic isolation — tenant A cannot read tenant B's rows via `Get` / `GetMany` (no row returned, not an error) — `tests/isolation_test.go` `TestIsolation_Get` + `TestIsolation_GetMany`
- [x] E2E: mutation mismatch — `Create` / `Update` with caller-set tenant mismatching the resolver returns `tenancy.ErrMismatch` BEFORE DB round-trip — `tests/mutation_test.go` `TestMutation_Create_MismatchReturnsErrMismatch` + `TestMutation_Update_MismatchReturnsErrMismatch` (counter asserts 0 DB ops on mismatch path)
- [x] E2E: mutation redundant-match — `Create` / `Update` with caller-set tenant matching the resolver succeeds — `tests/mutation_test.go` `TestMutation_Create_RedundantMatchSucceeds` (byte-identical SQL trace assertion folded into the no-op redundant-set path)
- [x] E2E: missing-tenant fail-closed — `required: true` + no tenant in ctx → `tenancy.ErrMissing` before DB round-trip — `tests/mutation_test.go` `TestMutation_MissingTenantFailsClosed` + `TestRead_MissingTenantFailsClosed` (both reads and writes guarded; counter asserts 0 DB ops)
- [x] E2E: `SkipTenancy: true` — admin path reads across tenants (returns rows for both tenant A and tenant B) — `tests/skip_test.go` (`Admin_GetMany`, `Create_RequiresExplicitTenant`, `GetByID`)
- [x] E2E: soft-delete + tenancy composition — both filters AND'd; `SkipTenancy` and "include deleted" are orthogonal (§29.8) — `tests/soft_delete_test.go` (3 tests covering AND-ing, include-deleted-scoped-to-tenant, and full orthogonality with both flags set)
- [x] E2E: cache isolation — tenant A and B populate distinct cache entries for the same PK — `tests/cache_test.go` `TestCache_PerTenantIsolation` (asserts MISS via `spyMetrics.missCount`; cross-tenant cache leak would manifest as a HIT) — **covers 13.6 deferral**
- [x] E2E: per-tenant pattern invalidation — `BuildTenantTablePattern(..., tenantX)` + `backend.InvalidatePattern(...)` clears only tenant X's entries; tenant Y's entries survive — `tests/cache_test.go` `TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant` + `TestCache_BuildTenantTablePatternFormat` (grammar invariants per §29.5) + `TestCache_InvalidateManyOnTenantedTableErrors` (public-API safety) — **covers 13.6 deferral**
- [x] E2E: event metadata — mutation events carry `Metadata["tenant"]` per §29.6 — `tests/event_test.go` (3 tests: tenanted carries key, non-tenanted omits key, SkipTenancy admin path omits key per the `mc.Tenant != nil` guard)
- [x] E2E: relationship propagation — o2o/o2m/m2m loads respect tenant scope per §29.10 — `tests/relationship_test.go` (4 tests; o2o exercised via direct child Get because the parent-side JOIN has a generator bug surfaced during this work — see **FIX-049** in `docs/tracker/fixes.md`; o2m + m2m + SkipTenancy-propagation all exercise the parent-load path with poisoned cross-tenant rows)
- [x] E2E: transaction — resolved tenant propagates inside the tx; attempting a cross-tenant write inside the tx errors consistently — `tests/tx_test.go` (`TenantResolvedAtTxOpenPropagatesToOps` + `CrossTenantWriteRejected`; the latter asserts the tx is rolled back via `errors.Is(err, tenancy.ErrMismatch)` AND a follow-up GetMany returning 0 rows)
- [x] E2E: `KSUID`-as-tenant-type integration smoke — `tests/ksuid_test.go` (`TenantResolver[ksuid.KSUID]` compiles + round-trips; `BuildTenantKey` / `BuildCompositeTenantKey` / `BuildTenantTablePattern` all stringify KSUID via %v + base62 String() correctly; distinct KSUID tenants → distinct keys)

### Acceptance Criteria

- [x] Example runs under `sqlgen generate --config sqlgen.yml` cleanly with no manual edits — generated 10 files / 11 tables in ~3.5s
- [x] Tests skip under `testing.Short()` per CLAUDE.md convention (TestMain checks `testing.Short()` and exits 0)
- [x] Every assertion targets an observable property — returned rows, error sentinels via `errors.Is`, emitted event payload fields, `metricsRecorder` Hit/Miss/Set counters, public `cache.BuildTenantTablePattern` output. No reflection, no field-poking on unexported types
- [x] The tenancy example is a complete reference implementation — `sqlgen.yml` + `schema.sql` + `tests/main_test.go` walk through resolver wiring, cache wiring, event wiring, and the per-tenant context pattern
- [~] Cross-dialect test files — **see Tasks deferral above.** SQLite-only for this sub-item; cross-dialect coverage of the runtime primitive lives in unit tests
- Bonus: testcontainers NOT required — sqlite in-process keeps `make test-examples` runtime under 2s for the tenancy suite

### Tests Required

- [~] Postgres / MySQL / SQLite E2E: basic isolation — **SQLite covered**; postgres + mysql deferred to follow-on (see Tasks deferral)
- [~] Postgres / MySQL / SQLite E2E: `GetMany` from ctx=A returns only A's rows — **SQLite covered**; postgres + mysql deferred
- [x] E2E: `Create` with mismatched tenant field in input → `tenancy.ErrMismatch`; no INSERT row created in DB (counter asserts 0 DB ops; DB row count unchanged) — `TestMutation_Create_MismatchReturnsErrMismatch`
- [x] E2E: `Create` with matching tenant field → succeeds — `TestMutation_Create_RedundantMatchSucceeds`. Byte-identical SQL trace not asserted — that contract is locked at the template level by `gen/template_*test.go`; the runtime test asserts the observable equivalent: created.WorkspaceID == resolved
- [x] E2E: `required: true` + missing tenant → `tenancy.ErrMissing`; no DB query issued — `TestRead_MissingTenantFailsClosed` + `TestMutation_MissingTenantFailsClosed` (counter asserts 0 DB ops on both Get and GetMany and Create paths)
- [x] E2E: `SkipTenancy: true` admin `GetMany` returns rows from both tenants — `TestSkipTenancy_AdminGetMany`
- [x] E2E: soft-delete composition — `TestSoftDelete_AndTenancyAreANDed` + `TestSoftDelete_IncludeDeletedScopedToTenant` + `TestSoftDelete_SkipTenancyAndIncludeDeletedAreOrthogonal`
- [x] E2E: cache isolation — `TestCache_PerTenantIsolation` (covers 13.6 deferral)
- [x] E2E: `BuildTenantTablePattern` invalidation — `TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant` (covers 13.6 deferral)
- [x] E2E: mutation on tenanted table → emitted event has `Metadata["tenant"]`; non-tenanted table omits the key — `TestEvent_TenantedMutationCarriesTenantMetadata` + `TestEvent_NonTenantedMutationOmitsTenantKey` + `TestEvent_SkipTenancyOmitsTenantMetadata`
- [~] E2E: o2o `GetWithDetail` returns parent+child filtered by tenant — **partial.** Direct child Get is tenant-scoped (`TestRelationship_O2O_DirectChildGetIsTenantScoped`); the parent-load-with-Profile-FieldOptions path is blocked by **FIX-049** (chained-alias condition emission bug discovered during this sub-item — see `docs/tracker/fixes.md`). Re-enable when FIX-049 lands
- [x] E2E: o2m + m2m loads — parent from ctx=A loads only A's children — `TestRelationship_O2M_TenantPropagatesToChildren` + `TestRelationship_M2M_TenantPropagatesToChildren` + `TestRelationship_O2M_SkipTenancyPropagatesToChildren` (uses SkipTenancy-on-Create to inject cross-tenant poison rows; verifies they're filtered on subsequent reads)
- [x] E2E: transaction — `TestTx_TenantResolvedAtTxOpenPropagatesToOps` *(renamed `TestTx_TenantIsConsistentAcrossOpsInATx` 2026-09-18 — the old name asserted a per-transaction resolve its own body disproves; PRD §29.11 now states the per-operation rule)* + `TestTx_CrossTenantWriteRejected` (asserts ErrMismatch + rollback via 0 surviving rows)
- [x] KSUID-tenant-type E2E: `TenantResolver[ksuid.KSUID]` compiles; cache key segments stringify correctly — `tests/ksuid_test.go`. Round-trip `Create` → `Get` against a KSUID-tenanted schema is intentionally not generated (would require a second example dir with KSUID as the uniform tenant type per §29.2.4); the cache-key + resolver smoke covers the integration boundary

### Completion Record

**Files added** (cmd/sqlgen/testdata/examples/tenancy/):
- `sqlgen.yml` — tenancy.enabled, column: workspace_id, type override → uuid.UUID, soft_delete column, per-table opt-out (audit_logs, post_tags) and column override (legacy_widgets → org_id), one explicit o2o relationship (users → user_profiles), one m2m via auto-detected junction (post_tags), composite PK (order_items)
- `schema.sql` — 11 tables: workspaces (tenant identity), products (simple-PK tenanted), order_items (composite PK with tenant column adjacent — tenant-in-PK deferred per 13.5 §29.7), audit_logs (shared opt-out), legacy_widgets (per-table org_id column override), users + user_profiles (tenanted o2o pair), posts (tenanted o2m child of users), tags + post_tags (tenanted m2m via non-tenanted junction), articles (soft-delete + tenancy composition)
- `go.mod` / `go.sum` — adds segmentio/ksuid for the KSUID smoke test alongside the standard sqlgen + sqlite + uuid stack
- `models/` (10 generated files via `sqlgen generate`)
- `expected/` (10 golden files, validated by `TestE2EGoldenFiles`)
- `tests/main_test.go` — sqlite shared-cache TestMain, two stable tenant UUIDs, `staticResolver`/`ctxResolver`/`missingResolver` helpers, `countingQuerier` (for "no DB op issued" assertions on the fail-closed paths), `spyMetrics` (Hit/Miss counters), `testEnv` builder + `withResolver`/`withCache`/`withEvents` opts
- `tests/isolation_test.go` (6 tests), `tests/mutation_test.go` (8), `tests/skip_test.go` (3), `tests/soft_delete_test.go` (3), `tests/cache_test.go` (5), `tests/event_test.go` (3), `tests/relationship_test.go` (4), `tests/tx_test.go` (2), `tests/ksuid_test.go` (2)

**Files modified:**
- `docs/tracker/fixes.md` — added **FIX-049** documenting the o2o JOIN child-tenant filter chained-alias bug surfaced by the relationship E2E

**Test results:** `make test` (unit), `make lint`, `make lint-examples`, `make test-examples` all pass — including the new tenancy suite (36 sub-tests, 1.5s wall) and the existing 6 example suites (cache, events, mysql, postgres, postgres_stdlib, sqlite). Golden files generated via `make update-golden-e2e` and verified by `TestE2EGoldenFiles`

**Findings rolled forward:**
- **FIX-049** — generator emits child-tenant filter conditions for chained o2o aliases not present in the runtime JOIN, breaking parent-load-with-o2o-FieldOptions on tenanted schemas with bidirectional o2o (e.g. user_profiles auto-detected as inverse of users.id ← user_profiles.user_id UNIQUE). E2E worked around by exercising o2o tenant scoping via direct child Get; the parent-load path comes back online when FIX-049 lands
- **Cross-dialect E2E** — postgres + mysql tenancy E2E deferred (one-go.mod-per-example harness constraint; would need per-dialect model packages or a generator-level multi-config mode). Documented as a follow-on; the runtime primitive itself is dialect-portable
- **KSUID-as-tenant-typed schema** — full Create/Get round-trip on a KSUID-tenanted schema not implemented (would require a second example dir per §29.2.4 uniform-type rule); cache-key + resolver smoke is sufficient for the integration boundary

---

## 13.10 Sync design back into `docs/PRD.md`

**PRD Reference:** §29 + cross-reference sites §4.8, §9.6, §17, §27.5, §28.3
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.10 (post-implementation reconciliation)

**Module:** Documentation

**Status:** Complete

### Tasks

- [x] Review every implementation finding from 13.1–13.9 against PRD §29 — identify any drift (renamed fields, changed error strings, deferred edge cases, discovered constraints)
- [x] Reconcile drift into PRD §29 — prefer updating the PRD over silent divergence; if a spec change is load-bearing enough to need a version note, add it
- [x] Sync §4.8 (TableConfig) to list `tenancy` alongside `cache` / `events` / `soft_delete`
- [x] Sync §9.6 (CallOptions) to list `SkipTenancy` alongside `SkipHooks` / `SkipCache`
- [x] Sync §17 (soft delete) cross-reference to §29.8 composition section
- [x] Sync §27.5 (cache key grammar) to reflect the labeled-segment grammar extension for tenancy (tenant-before-fingerprint)
- [x] Sync §28.3 (Event metadata) to note the `"tenant"` well-known key for tenanted tables
- [x] Godoc sync: `tenancy/tenancy.go` — verify `TenantResolver[T]`, `ErrMissing`, `ErrMismatch` godoc strings verbatim-align with PRD §29.3.1 wording
- [x] `docs/design/TENANCY.md` §10 (generated code reference) — update if codegen patterns drifted from the design snippets during 13.5–13.8 implementation

### Acceptance Criteria

- PRD §29 and the implementation match — no "the PRD says X, the code does Y" discrepancies
- The five cross-reference sites (§4.8, §9.6, §17, §27.5, §28.3) each name tenancy where it belongs — developers reading those sections discover tenancy without having to page over to §29
- Godoc on `tenancy/tenancy.go` is not just accurate but verbatim-aligned on the `T comparable` rationale and `ErrMissing` / `ErrMismatch` messages (§29.3.1) — this is a compatibility contract with user code that does `errors.Is(err, tenancy.ErrMissing)`
- `TENANCY.md` is updated as a design-supplement reference, not as a competing normative doc — PRD §29 remains the source of truth

### Tests Required

- [x] Godoc lint: run the existing godoc-check CI job; pass with zero new warnings (no godoc-check CI job exists in repo; `make vet` used as the closest in-tree proxy — passes across all modules)
- [x] PRD cross-reference audit: `rg "tenancy" docs/PRD.md` → every `tenancy` / `Tenancy` mention points at §29 or is explicitly within §29 — no orphan references (154 mentions audited 2026-04-23 during `/verify 13.10`; all either inside §29 or linking to §29.N; the two outside-§29 incidentals at §7.8.6 and §26.1 are context-accurate, not orphans)
- [x] Manual review: each of the five cross-reference sites names tenancy and links to §29 — tracked via a checklist in the completion record

### Completion Record

**Completed:** 2026-04-23

**Files changed:**

- `docs/PRD.md` — rewrote §29.7 (composite primary keys) to specify the verify-match rule (Option-2) matching the implementation from 13.5; the prior "omit tenant from XXXPK constructor signature" wording diverged from the landed codegen. Added rationale paragraph explaining why verify-match won over omit-from-signature (round-trip symmetry, uniform cache-key shape, SkipTenancy ergonomics). Strengthened the `Upsert` bullet in §29.4.2 to call out the conflict-clause UPDATE-set tenant exclusion (FIX-050 invariant made explicit).
- `docs/design/TENANCY.md` — expanded §5.3 (composite primary keys) with an implementation-notes block capturing the verify-match decision trail, the zero-value-is-mismatch property, and the single-helper-feeds-both-paths generator invariant; previously §5.3 was a one-line pointer to PRD §29.7.

**Files NOT changed (audit-only, already correct):**

- `tenancy/tenancy.go` — godoc on `TenantResolver[T]`, `ErrMissing`, and `ErrMismatch` already aligns verbatim with PRD §29.3.1 (error strings byte-identical; the `T comparable` rationale paragraph matches).
- `docs/PRD.md` §4.8, §9.6, §17, §27.5, §28.3 — all five cross-reference sites already name tenancy and link to §29 (added during 13.1–13.9 incrementally, not in 13.10). Audit confirmed no orphan `tenancy` / `Tenancy` references: every mention either lives inside §29 or points at §29 / §29.N.
- `docs/tracker/fixes.md` — FIX-049 (o2o chained-alias bug surfaced during 13.9) already resolved in its own commit; no PRD design note needed (the fix preserves the §29.10 belt-and-suspenders invariant rather than amending it).

**Notes:**

- The only load-bearing drift was §29.7. Everything else in §29 matched the implementation as-of 13.9 (error strings, cache key grammar, event metadata key, composite PK semantics on the non-tenant-in-PK path, SkipTenancy/SkipHooks orthogonality).
- The FIX-050 Upsert UPDATE-set exclusion was an implicit property of the original §29.4.2 wording ("the SET clause excludes the tenant column"), but the wording left the `ON CONFLICT DO UPDATE SET` projection ambiguous. The §29.4.2 update makes the invariant explicit for both dialects and cross-references §29.7 for the composite-PK variant.
- `make check` and `make check-examples` pass with 0 lint issues (docs-only changes, no code churn).

**Verification ready:** run `/verify 13.10`.

---

## 13.11 Stretch — Tenancy lint rule

**PRD Reference:** §29.11 (Constraints — *lists this rule as a non-goal*), §23.7 (`sqlgen lint` rule set — carries no tenancy rule), §29.4.3 (Raw bypass)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §13.11

**Module:** CLI (`cmd/sqlgen/cli/lint_tenancy.go`)

**Status:** Cancelled (2026-09-16) — the PRD disowns this rule; see the Completion Record.

### Tasks

- [ ] Extend `sqlgen lint` (the Phase 12.12 type-aware infrastructure built on `golang.org/x/tools/go/packages`) with a new warning-level rule
- [ ] Scanner: walk `*<facade>.Raw(...)` and `*<facade>.RawExec(...)` call sites; resolve the facade's table type; if the table is tenanted, emit a warning
- [ ] Warning message: `"raw query on tenanted table <name>: this query may need an explicit tenant filter; use CallOptions.SkipTenancy to document the bypass"` — actionable and references the escape hatch
- [ ] No data-flow tracking of ctx values across function boundaries — the rule is purely "call site on tenanted table uses raw SQL" (§29.11 explicit deferral of type-aware ctx tracking)
- [ ] Severity: WARNING only, never error — users can suppress via `sqlgen.yml` lint config
- [ ] `sqlgen.yml` lint config: add a `lint.tenancy.raw_bypass_warning: bool` toggle (defaults `true`)
- [ ] Golden lint output for a fixture project with `Raw` / `RawExec` calls on both tenanted and non-tenanted tables — only tenanted-table call sites produce warnings
- [ ] Suppression test: `lint.tenancy.raw_bypass_warning: false` → no warnings emitted

### Acceptance Criteria

- Lint rule is warning-level only — `sqlgen lint` does not exit non-zero solely because of this rule (projects treating warnings as errors do so at their CI layer)
- Rule is implemented against the already-planned 12.12 `golang.org/x/tools/go/packages` infrastructure — no fresh AST framework
- No false positives on non-tenanted `Raw` calls — the rule must correctly identify the table type from the facade
- No ctx-data-flow analysis — the rule is purely call-site-local; this is an explicit v1 design choice (§29.11)
- Users can disable via `sqlgen.yml` config without code changes

### Tests Required

- [ ] Fixture project with `products.Raw(...)` (tenanted) and `audit_logs.Raw(...)` (non-tenanted) → lint emits exactly 1 warning, naming `products`
- [ ] Fixture project with both `Raw` and `RawExec` on tenanted table → both call sites warned
- [ ] Suppression: `lint.tenancy.raw_bypass_warning: false` in fixture config → lint emits 0 warnings
- [ ] False-positive guard: `Raw` on a non-tenanted table → no warning
- [ ] Golden lint output stability — running twice produces byte-identical output (no map-iteration nondeterminism)

### Completion Record

**Cancelled 2026-09-16. No code was written, and none should be.**

This sub-item and the PRD have ended up on opposite sides of the same question, and the PRD wins (project rule 1). **PRD §29.11 "Constraints" now lists this rule as an explicit non-goal:**

> **Type-aware lint rule for forgotten-tenant handlers.** There is no tenancy-specific lint rule. `ErrMissing` at the first tenanted call is a strictly better signal than speculative static analysis: it fires on the real code path […]

`sqlgen lint`'s specified rule set (PRD §23.7) carries three rules — hook table/type mismatch, invalid operation for table, unused table constant — and no tenancy rule. So there is no spec text left for 13.11 to implement; its own **PRD Reference** line cites §29.11, which is precisely the section that says the rule should not exist.

The tasks above are retained rather than deleted so the rejected design stays legible: the warning-level `Raw` / `RawExec`-on-tenanted-table scanner, the `lint.tenancy.raw_bypass_warning` toggle, and the golden-output tests are what was considered and turned down.

**Consequences.**

- Phase 13 closes at 10/10 with this sub-item cancelled — the same shape Phase 12 uses for its own cancelled/deferred stretch item (`Complete (12.12 deferred)`).
- **No PRD amendment is required.** This is the tracker catching up to the spec, not a scope reduction against it.
- **12.12 is unaffected and stays deferred.** It is Phase 12's own stretch item for type-aware *cache* lint rules, with an independent rationale; only the "pairs naturally with the §29 tenant-aware lint rule" half of its deferral note dies with this, and that note is updated in STATUS.md's Deferred Items.
- The runtime guarantee is unchanged: `SkipTenancy` remains the explicit, greppable bypass (§29.4.3), and `ErrMissing` still fires at the first tenanted call on a handler that forgot to resolve a tenant.
