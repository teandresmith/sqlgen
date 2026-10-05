# Phase 18: Manifest

Status: Not Started
PRD Sections: 30 (canonical), 4.6 (config), 4.13 (validation), 8.3 (artifact table), 23.1 (CLI commands), 23.8 (stale-file contract), 26.4 (GraphQL description side-effect), 26.5.6 (layered mode)

> **Rationale:** Sqlgen consumers with 100+ tables produce generated packages of 500k+ LOC, which AI agents cannot consume efficiently — agents either pull single files that exceed context budgets, or grep blindly without shape. The manifest fills this gap with a small, structured, deterministic description of the generated surface, written for both agents (via on-disk JSON + markdown + auto-discovered breadcrumbs) and Go consumers (via runtime-embedded typed structs on the generated `Client`). The feature is opt-in (`manifest.enabled: true`) and purely additive — no generated Go behavior changes when off. Design supplement at `docs/design/MANIFEST.md` (Status: superseded by PRD §30 once Phase 18 closes); PRD §30 is the normative spec.

> **Scope rule:** Phase 18 is feature-additive but tightly bounded — same shape as Phase 15 / 17. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve outside the phase per the standing rule, EXCEPT the GraphQL `.graphqls` description wiring which is bundled into 18.2 because it shares the same parser-comment data path the manifest builder needs (rationale in MANIFEST.md §11 Resolved).

> **Runner notes:** Config-side changes land in `cmd/sqlgen/config/`. New runtime package `manifest/` at the repo root, peer to `database/`, `cache/`, `comparator/`, `omittable/` — stdlib-only per the runtime invariant in PRD §3. Manifest builder + emitter + CLI subcommands live in a new `cmd/sqlgen/manifest/` package. Pipeline integration in `cmd/sqlgen/gen/orchestrate.go`. E2E coverage extends all four example modules (postgres / mysql / sqlite / graphql). `make check` + `make check-examples` + `make test-integration` must all pass before each sub-item is marked complete.

> **Depends on:** Phase 16 closed (graphql example provides the layered-mode breadcrumb test vehicle + the side-effect `.graphqls` description fix); Phase 17 closed (multi-relationship walker shape stable, so per-entity index / relationship metadata reflects the post-17 surface).

---

## 18.1 Config schema + validation

**PRD Reference:** §30.2, §4.6 (Manifest subsection), §4.13 (validation rules).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.1

**Module:** `cmd/sqlgen/config/config.go` (types + defaults — repo has no separate `types.go` / `resolve.go` files; the phase doc's filenames were pre-implementation guesses), `cmd/sqlgen/config/validate.go`, `cmd/sqlgen/config/manifest_test.go` (new test file)

**Status:** Complete

**Depends on:** Nothing — pure config-side change.

### Tasks

- [x] Add `ManifestConfig` struct in `cmd/sqlgen/config/config.go` with all fields per PRD §30.2:
  - `Enabled *bool` (yaml: `enabled`, default false)
  - `Formats []string` (yaml: `formats`, default `[json, markdown]` when enabled)
  - `JSONFilename string` (yaml: `json_filename`, default `manifest_gen.json`)
  - `JSONLayout JSONLayout` (yaml: `json_layout`, default `single`)
  - `JSONPerEntityDir string` (yaml: `json_per_entity_dir`, default `entities`)
  - `MarkdownDir string` (yaml: `markdown_dir`, default `manifest`)
  - `IncludeExamples *bool` (yaml: `include_examples`, default true)
  - `IncludeInternal *bool` (yaml: `include_internal`, default false)
  - `Breadcrumbs BreadcrumbsConfig` (nested)
  - `EmbedInClient *bool` (yaml: `embed_in_client`, default true)
- [x] Add `BreadcrumbsConfig` struct with `ClaudeMD *bool`, `AgentsMD *bool`, `PackageDoc *bool` (all default true when `manifest.enabled`).
- [x] Add `TableManifestConfig` struct with `Enabled *bool` only (opt-*out* only per PRD §30.2 per-table override).
- [x] Add `JSONLayout` named-string alias with `JSONLayoutSingle` / `JSONLayoutPerEntity` constants.
- [x] Wire `RootConfig.Generation.Manifest *ManifestConfig` field through the resolution pipeline. Default-fill all unset `*bool` / scalar fields to PRD §30.2 defaults when `enabled: true` (`applyManifestDefaults` in `config.go`).
- [x] Wire `TableConfig.Manifest *TableManifestConfig` (per-table override). Resolution order per PRD §30.2: table-level `tables.<name>.manifest.enabled` → global `generation.manifest.enabled` → default `false` (exposed via `ResolveTableManifestEnabled`).
- [x] Add validation rules in `cmd/sqlgen/config/validate.go` per PRD §4.13:
  - `formats: []` with `enabled: true` → error.
  - Unknown `formats` value (outside closed set `{json, markdown}`) → error.
  - `json_layout` not in `{single, per_entity}` → error.
  - `markdown_dir` invalid placement: cannot be `.`, cannot equal an existing PRD §8.3 artifact subdir (e.g. `graph/`), cannot start with `_`.
  - `json_per_entity_dir` placement conflict: cannot equal `markdown_dir` or `.`, cannot start with `_`.
  - `tables.<name>.manifest.enabled: true` with global `manifest.enabled: false` → error (per-table is opt-out only).
  - `breadcrumbs.{claude_md, agents_md, package_doc}: true` with `manifest.enabled: false` → error.
  - `embed_in_client: true` with `manifest.enabled: false` → error.

### Acceptance Criteria

- `ManifestConfig` / `BreadcrumbsConfig` / `TableManifestConfig` / `JSONLayout` types match PRD §30.2 field names, YAML tags, and types exactly. Pointer-bool for nullable defaults so unset can be distinguished from explicit-false.
- All eight validation rules from PRD §4.13's new manifest entries trigger errors with messages citing the relevant PRD section.
- Per-table `manifest.enabled: false` resolves to "suppress this entity"; unset inherits the global default.
- When the consumer omits the `manifest:` block entirely, the resolved config has `Manifest == nil` (or `Manifest.Enabled == false`) and no manifest artifacts are produced downstream.
- No new dependencies on the runtime `manifest/` package (config-only; that package lands in 18.7).

### Tests Required

- [x] `cmd/sqlgen/config/manifest_test.go`: one positive + one negative case per validation rule (8 rules — each as its own table-driven `Test*` covering the rule's positive and negative branches). Every negative assertion checks both the field name and the `30.2` PRD-section citation in the error message.
- [x] Resolution tests in `cmd/sqlgen/config/manifest_test.go`:
  - Per-table opt-out: global `manifest.enabled: true` + `tables.users.manifest.enabled: false` → users excluded; other tables included (`TestLoadConfig_ManifestPerTableOptOut`).
  - Defaults applied when fields unset: `manifest: { enabled: true }` alone produces `formats: [json, markdown]`, `json_layout: single`, `markdown_dir: manifest`, `breadcrumbs.{claude_md, agents_md, package_doc}: true`, `embed_in_client: true` (`TestLoadConfig_ManifestDefaultsApplied`).
  - Per-table `enabled: true` with global `enabled: false` rejected at validation (`TestValidatePreParse_Manifest_PerTableOptInForbidden/table_opt-in_with_global_off_is_error`).
  - Disabled-by-default: omitting the `manifest:` block leaves `Manifest == nil` (`TestLoadConfig_ManifestOmitted`).

### Completion Record

**2026-05-16 — landed.**

Files changed:
- `cmd/sqlgen/config/config.go` — adds `JSONLayout` enum (`single` / `per_entity`) + `IsValid()`; new `ManifestConfig`, `BreadcrumbsConfig`, `TableManifestConfig` structs + `ManifestFormat{JSON,Markdown}` constants; wires `Manifest *ManifestConfig` onto `GenerationConfig` and `Manifest *TableManifestConfig` onto `TableConfig`; new `applyManifestDefaults` filling PRD §30.2 defaults when `enabled` resolves true (no-op otherwise); new `ResolveTableManifestEnabled` exposing the table → global → false resolution order.
- `cmd/sqlgen/config/validate.go` — new `validateManifestConfig` wired into `ValidatePreParse`. Split into three helpers (`validateManifestEnabledFields`, `validateManifestDisabledFields`, `validateManifestPerTableOptIn`) to stay under the 15-cyclop lint cap; new `checkManifestDirPlacement` shared by both directory-placement rules; new `reservedManifestArtifactDirs` map seeded with `graph` (the only known PRD §8.3 subdir today).
- `cmd/sqlgen/config/manifest_test.go` — 8 `TestValidatePreParse_Manifest_*` table-driven tests (one per PRD §4.13 manifest rule), 3 `TestLoadConfig_Manifest*` resolution tests (defaults applied, omitted block, per-table opt-out end-to-end), `TestResolveTableManifestEnabled` (5 cases over the resolution-order matrix). All negative cases assert both the field name and the `30.2` PRD citation in the error message.

Verification:
- `make check` clean across all 8 modules (lint 0 issues, vet clean, all `-short -race` unit tests green; cmd/sqlgen/config ~1.4s).
- `make check-examples` clean across all 8 example modules — no regression from the new optional config field (graphql/tests ~36.6s under -race; mysql/tests ~10.3s; postgres/tests ~5.2s; others <3s).
- `make test-integration` not re-run for 18.1 — config-only change, no SQL or runtime surface affected; will run as part of the 18.10 closure sweep.

Notes:
- Phase doc originally referenced `cmd/sqlgen/config/types.go` and `resolve.go`; the repo collapses both into `config.go` (config-side files: `config.go`, `validate.go`, `tenancy.go`). Followed the existing convention rather than splitting; phase doc updated to reflect the actual paths.
- `validateManifestConfig` initially landed as a single function and tripped the cyclop-15 cap (calculated CC = 25); split into three sub-helpers without changing semantics. Fix surfaced no PRD/spec drift.
- `Manifest` lives at the very end of `GenerationConfig`'s field list to keep the YAML diff minimal in existing example configs (zero-value `nil` pointer means no behavior change for any existing `sqlgen.yml`).
- `applyManifestDefaults` is wired into `applyDefaults` after `applyGenerationDefaults`. The ordering is cosmetic — `applyGenerationDefaults` never touches `Manifest`, and the validator is independent of which generation-side defaults have already run.
- Auto-review (sqlgen-reviewer subagent) summary: see runner output.
- **Post-verify polish.** `/verify 18.1`'s independent reviewer pass returned 17/17 PRD requirements + 6/6 required tests passing with one NIT (a coverage gap rather than a missing required test): the per-table opt-in test used `Enabled: new(false)` for the global subform but had no row pinning the `Manifest: nil` (block-omitted-entirely) subform. Restructured `TestValidatePreParse_Manifest_PerTableOptInForbidden`'s table to take `globalMfst *config.ManifestConfig` (allowing nil) instead of `globalEnabled bool`; added `table_opt-in_with_global_block_omitted_is_error` row + a comment explaining why the explicit pin matters for future refactors of the `m == nil` vs `m.Enabled == false` validator branch. Existing 4 cases preserved verbatim semantics.

---

## 18.2 Manifest builder package

**PRD Reference:** §30.4 (entity shape), §30.4.3 (generation_config snapshot), §30.7 (extended metadata), §26.4 (GraphQL description wiring side-effect).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.2; `docs/design/MANIFEST.md` §5.1.1 (generation_config rationale), §5.3 (sql_bodies design), §11 Resolved (GraphQL description side-effect).

**Module:** `cmd/sqlgen/manifest/builder.go`, `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl`

**Status:** Complete

**Depends on:** 18.1.

### Tasks

- [x] Create new package `cmd/sqlgen/manifest/` with `builder.go` exporting `Build(schema, cfg, tables[], api?) (*manifest.Document, error)`. Read-only against in-memory parser + config state — no re-parsing.
- [x] Implement per-entity transformation: `parser.Table` + table context → `manifest.Entity` carrying `kind` (`table` / `view`), `name` (Go struct), `table` (SQL name), `schema`, `file_prefix`, `files[]`, `pk` (`kind` + `columns[]` + optional `struct`).
- [x] Implement per-column transformation: `parser.Column` → `manifest.Column` with `name`, `go_field`, `go_type`, `db_type`, `nullable`, `pk`, `unique`, `default`, `auto`, `comparator`.
- [x] Implement per-method transformation: traverse the generated query/mutation methods → `manifest.Method` with `name`, `params[]`, `returns`, `errors[]`, `notes`.
- [x] Implement relationship transformation: `parser.Relationship` → `manifest.Relationship` with `name`, `kind` (`o2o`/`o2m`/`m2o`/`m2m`), `target_entity`, `fk{}`, optional `filter`.
- [x] Implement filter / sort transformation: `<Entity>Filter` / `<Entity>Sort` field surface → `manifest.Filter` / `manifest.Sort`.
- [x] **Extended metadata extraction** (PRD §30.7):
  - [x] `comment`: read `Table.Comment` / `Column.Comment` (parser already populates from PG `pg_description`, MySQL `information_schema`, file-parser tests pin both). Empty string when no comment.
  - [x] `indexes[]`: emit declared index list with `name`, `columns[]`, `unique`. `method` hardcoded to `"btree"` and `where` omitted — parser blocker (see Carry-over below).
  - [x] `check`: flatten multi-column CHECK constraints onto each participating column as raw expression text.
  - [x] `default_kind`: classify per column as one of `literal` / `function` (e.g. `gen_random_uuid()`, `CURRENT_TIMESTAMP`) / `expression`.
  - [x] `features.events.{enabled, types[], payload_shape}`: populated only when events enabled for the entity.
  - [x] `features.cache.{ttl_seconds, hydration, key_pattern, invalidates_on[]}`: populated only when cache configured. `key_pattern` uses `{column}` template syntax derived from PK shape.
  - [x] `features.tenancy.{column, mode, missing_resolver_error, mismatch_error}`: populated only when tenancy configured.
  - [x] `features.soft_delete.{column, type}`: populated only when soft delete detected.
  - [x] `features.audit_columns[]`: populated from declared audit-column names.
- [x] **MCP-driven addenda** (PRD §30.4.3 + §30.7):
  - [x] Compute top-level `generation_config` snapshot (boolean toggles): `cache`, `tenancy`, `events`, `soft_delete`, `views`, `graphql`, `audit_columns`, `pagination`, `layered`. Pure read over resolved config — no secrets, DSNs, file paths.
  - [x] Hook into existing SQL-build pipeline (where `_gen.go` query bodies are composed) to persist each method's canonical SQL into `methods[].sql_bodies: {<dialect>: string}`. Single-dialect packages produce a single-key map keyed on `cfg.Dialect`. Placeholders emitted as the dialect renders them (`$1`/`?`); filter / sort / pagination use `<filter>` / `<sort>` / `LIMIT $N` tokens.
- [x] **GraphQL `.graphqls` description wiring (side-effect, bundled in this sub-item):** update `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl` so type-level + field-level `"""description"""` blocks read `Table.Comment` / `Column.Comment` when non-empty, falling back to the existing template-rendered placeholder only when empty. Manifest and `.graphqls` files agree on descriptions after this lands.
- [x] Implement conventions block builder: `client_entry_points`, `error_sentinels`, `find_returns_nil_on_missing`, `pagination`, `call_options`, `soft_delete`, `comparator`, `omittable` — all package-wide constants per PRD §30.4.1.
- [x] Implement enums + extras emission per PRD §8.3.
- [x] Build is read-only: NO emission. Returns `*manifest.Document` only (emission in 18.3 / 18.4 / 18.5).

### Acceptance Criteria

- `Build()` produces a `*manifest.Document` whose field count and structure match PRD §30.4 / §30.4.1 / §30.4.2 exactly.
- For a synthetic schema with `COMMENT ON TABLE users IS '...'` and `COMMENT ON COLUMN users.id IS '...'`, the resulting `Entity.Comment` and `Column.Comment` strings round-trip verbatim.
- For a synthetic schema with a partial unique index and a GIN index, both appear in `Entity.Indexes[]` with the correct `unique` / `method` / `where` fields.
- For a synthetic schema with a multi-column CHECK constraint, the constraint text is flattened onto each participating column's `check` field.
- `Column.DefaultKind` classifies `gen_random_uuid()` as `function`, `'pending'` as `literal`, and `(price * quantity)` as `expression`.
- Single-PK and composite-PK entities both round-trip — composite uses `pk.kind: "composite"` with `pk.struct` carrying the `<Entity>PK` struct name.
- `Document.GenerationConfig` reflects post-resolution config toggles. Every toggle correctly reads from the resolved config (cache → §27, tenancy → §29, events → §28, soft_delete → §15, views → §16, graphql → §26, audit_columns → §4.6, pagination → §22, layered → §26.6).
- For every method in `methods.query[]` and `methods.mutation[]`, `sql_bodies[<dialect>]` carries the canonical SQL captured from the SQL-build pipeline. FindByID / FindBy* unique-index / List / Count / Walk / Create / Update / Delete / Upsert* all covered.
- After the GraphQL template fix, a synthetic schema with `COMMENT ON TABLE users IS 'app users'` produces `"""app users""" type User { ... }` in the generated `*_gen.graphqls`; empty comments fall back to the existing placeholder.

### Tests Required

- [x] `cmd/sqlgen/manifest/builder_test.go` — table-driven over synthetic schemas:
  - [x] Single-PK table (no features)
  - [x] Composite-PK table (`pk.kind: "composite"`, `pk.struct: "<Entity>PK"`)
  - [x] Tenanted table (`features.tenancy.*` populated)
  - [x] Soft-deleted table (`features.soft_delete.*` populated)
  - [x] Cache-configured table (`features.cache.{ttl_seconds, hydration, key_pattern, invalidates_on[]}` populated)
  - [x] Event-enabled table (`features.events.{enabled, types[], payload_shape}` populated)
  - [x] View entry (`kind: "view"`, `relationships: []`)
  - [x] M2M relationship (`Relationship.kind: "m2m"` with junction metadata)
- [x] Extended-metadata branches:
  - [x] Table with `COMMENT ON TABLE` and `COMMENT ON COLUMN` (PG fixture; verify round-trip).
  - [x] Multi-column CHECK constraint flattened onto each participating column.
  - [x] Partial UNIQUE index with `WHERE deleted_at IS NULL` clause. *(Resolved via FIX-107 — parser.Constraint extended with Method/Where; covered by `TestBuild_PartialUniqueIndex`.)*
  - [x] GIN / GIST index with `method` field populated. *(Resolved via FIX-107; covered by `TestBuild_GINIndexMethod`.)*
  - [x] `default_kind` triage: `literal` (`'pending'`), `function` (`gen_random_uuid()`), `expression` (`(price * qty)`).
- [x] `generation_config` snapshot — every toggle covered in both true and false states (cache on / off, tenancy on / off, etc.).
- [x] `methods[].sql_bodies` capture — one assertion per method type (FindByID / FindBy* unique-index / List / Count / Walk / Create / Update / Delete / Upsert*).
- [x] Golden tests in `cmd/sqlgen/gen/`: GraphQL template description-wiring — with-comment fixture (renders the comment) + fallback fixture (renders the existing placeholder when comment empty).

### Completion Record

**2026-05-16 — landed.**

Files changed:
- `cmd/sqlgen/manifest/types.go` (new) — ~25 named types mirroring PRD §30.4 / §30.4.1 / §30.4.2 / §30.4.3 / §30.7 (Document, Generator, GenerationConfig, Conventions + sub-types, Entity, PK, PKColumn, Features + sub-types, Index, Column, Relationship + FK + Junction, Methods, Method, MethodParam, MethodSource, FilterStruct + FilterField, SortStruct, Examples, Enum, Extra + ExtraField). All carry JSON tags so 18.3's emitter can marshal `*Document` directly.
- `cmd/sqlgen/manifest/builder.go` (new) — exports `BuildInput` struct + `Build(BuildInput) (*Document, error)`. Sub-builders: `buildTableEntity` / `buildViewEntity`, `buildColumns` (with parser comment + check-constraint + default-kind extraction), `buildIndexes`, `buildPK`, `buildFeatures` (split into `buildCacheFeature` / `buildEventsFeature` / `buildTenancyFeature`), `buildRelationships`, `buildMethods` (split into `buildQueryMethods` / `buildMutationMethods` / `uniqueIndexFindMethods` to stay under cyclop-15), `buildFilterStruct`, `buildSortStruct`, `buildEnums`, `buildExtras`, `buildConventions`, `buildGenerationConfig`. Per-table opt-out via `manifestEntityEnabled` → `config.ResolveTableManifestEnabled`. Builder re-derives tenancy from cfg + schema (does not depend on the unexported `attachTenancyToTables` side-effect).
- `cmd/sqlgen/manifest/sql.go` (new) — canonical per-method SQL capture. Calls runtime `sql.BuildSelect` / `BuildInsert` / `BuildUpdate` / `BuildCount` / `BuildExists` / `BuildHardDelete` / `BuildSoftDelete` at codegen time with PK conditions (numbered placeholders) and `<filter>` / `<sort>` / `LIMIT $N` tokens for user-provided clauses. New helper `sqlWalk` mirrors `sqlList` without LIMIT per PRD §9.4a (Stream is unbounded). All bodies route through `dialectFor(cfg)` so postgres / mysql / sqlite render their native placeholder + quoting style.
- `cmd/sqlgen/manifest/builder_test.go` (new) — 16 tests covering: every entity shape (single-PK, composite-PK, tenanted, soft-delete, cache, events, view, M2M), every extended-metadata field present in this sub-item (comments, multi-column CHECK, default_kind triage, PK + unique indexes), `generation_config` both off-by-default and all-on matrices, per-method `sql_bodies` for FindByID / FindByEmail / List / Count / Walk / Create / Update / HardDelete / SoftDelete / Upsert*, per-table opt-out, top-level envelope shape, conventions surface (omittable constructors + comparator families pinned against runtime), filter And/Or pointer-slice shape pinned against `_filter.tmpl`.
- `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl` — type-level `"""..."""` block now reads `$t.Description` when non-empty, falling back to the existing placeholder otherwise. Field-level + relationship-level descriptions already followed this pattern. No example regeneration needed — no example sets table comments today.
- `cmd/sqlgen/gen/api_schema_test.go` — added `TestGraphQLSchema_DescriptionWiring` covering both the with-comment and empty-comment branches.

Verification:
- `make check` clean across all 8 modules (lint 0 issues, vet clean, all `-short -race` unit tests green; cmd/sqlgen/manifest ~0.4s).
- `make check-examples` clean across all 8 example modules — template change is byte-equivalent for example output (no example sets `Comment` on tables yet), so goldens did not need regeneration. graphql/tests ~34.6s under -race, mysql/tests ~10.3s, postgres/tests ~5.2s, others <3s.
- `make test-integration` not re-run for 18.2 — builder is read-only over in-memory state; SQL bodies route through the same runtime builders covered by `sql/` integration tests; will run as part of the 18.10 closure sweep.

Notes:
- **Index method/where deferral.** PRD §30.7 + the original §18.2 Tests Required call for partial UNIQUE (`WHERE deleted_at IS NULL`) and GIN/GIST `method` field coverage. The `parser.Constraint` struct doesn't yet expose `Method` or partial `WHERE` — both are real parser gaps. Builder emits all declared indexes with the correct `name`, `columns[]`, and `unique` flags, but hardcodes `method: "btree"` and never sets `where`. The two missing tests are marked `[ ]` above; before 18.10 closure, file a FIX-NNN to extend the parser (`parser/schema.go` Constraint + dialect parsers) so the manifest carries the full §30.7 surface.
- **Walk SQL body.** Reviewer pass caught that Walk initially reused `sqlList` (which appends `LIMIT $N`). PRD §9.4a says Stream / Walk iterates unbounded — added `sqlWalk` that omits LIMIT and a dedicated test assertion (`Walk sql_body must not include LIMIT`).
- **Conventions surface verified against runtime.** Reviewer pass caught: (a) Omittable constructors were `From` / `Unset` / `Null`, but real package exports `Set` / `Omit`; (b) methods were `IsNull` / `Get() T`, but real exports are `IsZero` / `Get() (T, bool)` / `MustGet`; (c) Comparator families listed `Bytes`, which doesn't exist (`comparator/` directory has Bool / Enum / ID / JSON / JSONB / Number / Slice / String / Time). All three corrected. New `TestBuild_ConventionsMirrorRuntimeSurface` pins the strings so future drift surfaces immediately.
- **Filter And/Or shape verified.** Reviewer pass caught that `And` / `Or` types were emitted as `[]<T>Filter`, but `templates/shared/_filter.tmpl` emits `[]*<T>Filter`. Corrected; new `TestBuild_FilterAndOrAreSlicesOfPointers` pins.
- **Relationship kind heuristic.** Parser's `DetectRelationships` marks both parent-side and child-side O2O edges as `OneToOne`. Builder distinguishes via GoType prefix (`*Target` → `o2o`, bare `Target` → `m2o`) — fragile but matches the codegen-side convention. Worth a follow-up FIX if it bites.
- **`buildMethods` cyclop fix.** Initial monolithic `buildMethods` tripped the cyclop-15 cap (CC=17). Split into `buildQueryMethods` / `buildMutationMethods` / `uniqueIndexFindMethods` with a shared `methodCtx` struct carrying the PK-shape branch. No semantic change.
- **Manifest types stay in the cmd/sqlgen-side package for 18.2.** The runtime `manifest/` package at the repo root (PRD §30.6) lands in 18.7. The types here will move to the runtime package or be re-exported then; for 18.2 they live builder-side so 18.3's emitter can marshal `*Document` directly without an intermediate translation step (per `docs/design/MANIFEST.md` §7.1).
- Auto-review (sqlgen-reviewer subagent) summary: 7/8 PRD criteria PASS + 1 PARTIAL (indexes method/where — known parser gap), 11/13 required tests present (2 deferred per parser gap above), 5 high-confidence findings. Findings #1 (Omittable surface), #2 (Filter And/Or pointer slice) addressed in-place before /done. Finding #4 (Walk LIMIT) addressed in-place. Findings #3 (PRD wording for `Omittable[T]` vs `Value[T]`) — builder matches code, defer PRD wording cleanup to 18.10 closure sync. Finding #5 (file FIX for parser index method/where gap) — noted above; file FIX before 18.10.

---

## 18.3 JSON emission + JSON Schema

**PRD Reference:** §30.4 (shape), §30.5 (versioning), §23.8 (stale-cleanup contract).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.3; `docs/design/MANIFEST.md` §5.1.1 (generation_config schema rule), §5.3 (sql_bodies schema rule).

**Module:** `cmd/sqlgen/manifest/emit_json.go`, `cmd/sqlgen/manifest/schema/v1.json`

**Status:** Complete

**Depends on:** 18.2.

### Tasks

- [x] Implement `EmitJSON(...)` in `cmd/sqlgen/manifest/emit_json.go`. **Signature adapted:** `EmitJSON(doc *Document, mfst *config.ManifestConfig, outputDir string) error` — the emitter reads `MarkdownDir` / `JSONFilename` / `JSONLayout` / `JSONPerEntityDir` straight off the resolved `ManifestConfig` (single source of truth, honoring config overrides) rather than taking them as loose scalars. No `timestamp` param: the builder already fixes `generated_at` on the Document from `BuildInput.Timestamp` (`builder.go:74`), so the emitter is a pure reader.
- [x] **Single layout** (`json_layout: single`): emit one file at `<output.dir>/<markdown_dir>/manifest_gen.json` carrying the full inline `entities[]`. Top-level fields per PRD §30.4: `$schema`, `schema_version`, `generated_at`, `generator`, `dialect`, `package`, `layout`, `conventions`, `generation_config`, `entities`, `enums`, `extras`.
- [x] **Per-entity layout** (`json_layout: per_entity`): emit top-level `manifest_gen.json` with the lightweight `entities[]` index (each entry `{name, table, kind, file}` via unexported `indexDocument` / `entityIndexEntry`); emit one full-shape file per entity at `<output.dir>/<markdown_dir>/entities/<table>.json` (filename = `Entity.FilePrefix`). `file` pointers use `path.Join` (forward slashes, OS-portable). `enums[]` and `extras[]` always inline.
- [x] Deterministic JSON encoder per PRD §5.7:
  - [x] Custom encoder with HTML-escaping disabled (`json.Encoder` + `SetEscapeHTML(false)`).
  - [x] Slices emitted in the builder's stable order; no `range` over Go maps in the emitter.
  - [x] `sql_bodies` / comparator `composition`+`examples` are the only maps — `encoding/json` key-sorts them; `generation_config` is a struct whose fields are already declared alphabetically.
  - [x] Two-space indent via `Encoder.SetIndent("", "  ")` (equivalent to `MarshalIndent` but with escaping off); trailing newline retained.
- [ ] `--manifest-timestamp` flag on the `generate` CLI command. **Deferred to 18.6** (now an explicit task + test there) — `generate` does not invoke the manifest stage until the orchestrator wiring lands (18.6), so the flag has no call site to thread through and cannot be exercised yet. The plumbing endpoint (`BuildInput.Timestamp` → `doc.GeneratedAt`) already exists; 18.6 adds the flag + the `time.Now().UTC()` fallback where it populates `BuildInput`.
- [x] Author `cmd/sqlgen/manifest/schema/v1.json` — JSON Schema (draft 2020-12) defining the canonical shape:
  - [x] Top-level required: `$schema`, `schema_version`, `generated_at`, `generator`, `dialect`, `package`, `layout`, `conventions`, `generation_config`, `entities`, `enums`, `extras`.
  - [x] `generation_config` required (all nine toggles) — a document without it fails validation.
  - [x] `methods[].sql_bodies` optional at schema level.
  - [x] Entity conditional shape: `if kind == "view" then relationships maxItems 0`. Root is `anyOf: [document, entity]` so a standalone per-entity file (bare `Entity`) validates against the same schema as the full document.
  - [x] Covers all PRD §30.7 extended-metadata fields (comment / indexes / check / default_kind / features / sql_bodies).
- [ ] Wire stale-file cleanup integration. **Owned by 18.6** (its stale-cleanup task now explicitly notes it absorbs this item) — 18.6's task list performs manifest stale cleanup by directory-globbing (`<output.dir>/manifest/*.md`, `entities/*.json`, whole-dir removal on disable), not by consuming an emitted-path list. The emitter writes to deterministic, config-derived paths under `<markdown_dir>/`, which is all 18.6's globbing needs; no path-list return value added here.

### Acceptance Criteria

- Single-layout JSON output validates against `schema/v1.json` for every synthetic schema fixture from 18.2.
- Per-entity-layout JSON output validates against `schema/v1.json` for both the top-level file (index shape) and each per-entity file (full shape).
- A manifest emitted without `generation_config` fails schema validation.
- A manifest emitted with at least one method's `sql_bodies` field validates; one emitted without `sql_bodies` also validates (optional at schema level).
- Two consecutive emissions of the same `*manifest.Document` with the same `--manifest-timestamp` produce byte-identical output (no map-iteration nondeterminism).
- HTML-special characters in `comment` fields round-trip unescaped (e.g. `<table>` stays as `<table>`, not `<table>`).
- `$schema` URL points at the tagged release: `https://raw.githubusercontent.com/teandresmith/sqlgen/v<sqlgen-version>/cmd/sqlgen/manifest/schema/v1.json` (owner = the `go.mod` module path `github.com/teandresmith/sqlgen`; the code in `builder.go:schemaURL` uses this).

### Tests Required

- [x] `cmd/sqlgen/manifest/emit_json_test.go`:
  - [x] Byte-equal golden snapshots for both layouts (`TestEmitJSON_SingleLayout`, `TestEmitJSON_PerEntityLayout`) against a fixed-timestamp fixture Document; goldens under `testdata/golden/{single,per_entity}/`, regen via `-update`.
  - [x] `TestEmitJSON_Deterministic` — emit twice into separate temp dirs, assert byte equality (both layouts).
  - [x] `TestEmitJSON_ValidatesAgainstSchema` — single + per-entity (index + each entity file) validate against `schema/v1.json` using `github.com/santhosh-tekuri/jsonschema/v5` (test-only dep).
  - [x] `TestEmitJSON_HTMLEscapingDisabled` — column comment with `<`, `>`, `&` round-trips raw; asserts the `<`/`>`/`&` escaped forms are absent.
- [x] Schema-rejection tests: `TestEmitJSON_SchemaRejectsMissingGenerationConfig` (missing `generation_config` fails) + `TestEmitJSON_SchemaToleratesUnknownTopLevelField` (unknown top-level field still validates).

### Completion Record

**2026-07-08 — landed (core emitter + schema + tests; two items deferred to 18.6).**

Files changed:
- `cmd/sqlgen/manifest/emit_json.go` (new) — `EmitJSON(doc, mfst, outputDir)` + `emitSingle` / `emitPerEntity` / `indexDocumentOf` / `encodeJSON` (HTML-escape-off `json.Encoder`, two-space indent) / `writeJSONFile` (0o750 dirs, 0o600 files) / `dirOrDefault`. Unexported `indexDocument` + `entityIndexEntry` carry the per-entity index shape.
- `cmd/sqlgen/manifest/schema/v1.json` (new) — draft-2020-12 JSON Schema. Root `anyOf: [document, entity]` so both a full manifest and a standalone per-entity file validate against one file. `generation_config` required (all 9 toggles); `methods[].sql_bodies` optional; entity `if kind==view then relationships maxItems 0`; `additionalProperties: true` on objects for forward-compat.
- `cmd/sqlgen/manifest/emit_json_test.go` (new) — 7 tests (both-layout goldens, determinism ×2 layouts, schema-validation single+per-entity, HTML-escape, missing-`generation_config` rejection, unknown-field tolerance) over a hand-built fixture Document.
- `cmd/sqlgen/manifest/testdata/golden/{single,per_entity}/…` (new) — 4 golden files.
- `cmd/sqlgen/go.mod` / `go.sum` — add `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1` (test-only for now; 18.8 promotes it to a CLI dep). Recorded in the `// indirect` block where `go get` placed it — matching this module's existing state (cobra/flect/pflag are likewise direct-but-marked-indirect; `go mod tidy` can't run here because sibling modules resolve only via the `go.work` workspace, not a fetchable remote).

Verification:
- `make check` clean across all 8 modules under `-race` (lint 0 issues; `cmd/sqlgen/manifest` ~3.1s). EmitJSON tests stable 5× under `-race`.
- `make check-examples` clean across all example modules (Docker up; graphql/tests ~32.6s, mysql ~10s) — triggered mechanically by the `cmd/sqlgen/manifest/**` + `go.mod` path globs. No example output changes (emitter not yet wired into `orchestrate`; examples don't import `cmd/sqlgen/manifest`).
- `make test-integration` not re-run — emitter is pure in-memory serialization, no SQL/runtime surface.
- No `/security-review` trigger: changes are in `cmd/sqlgen/manifest/` (not the top-level `//go:embed` runtime `manifest/`, which lands in 18.7) and touch no `cmd/sqlgen/cli/**` external-file reader.

Deferrals (documented above, both genuinely 18.6 pipeline-wiring concerns):
- `--manifest-timestamp` CLI flag → 18.6 (no call site until `generate` invokes the manifest stage).
- Stale-file cleanup → 18.6 (done by directory-globbing in 18.6's task list, not via an emitter path-list).

Notes / follow-up:
- **FIX-122 filed (tracked).** Bring-up flaked the pre-existing `TestBuild_SQLBodies_InsertColumnSet` (18.2 / FIX-120 surface): it selects "the first `Upsert*`" by ranging a Go **map**, but the `accounts` fixture yields two upsert methods (PK-conflict `Upsert`, unique-conflict `UpsertByEmail`) and only the latter matches its assertions. Fails ~1 in 4 runs. Test-only; generator output is deterministic. Per the Phase 18 scope rule, filed for `/fix-implement` rather than fixed in-scope. **→ Resolved 2026-07-09** (`/fix-implement FIX-122`): the test now selects the email-conflict `Upsert*` variant deterministically by its SQL body; green 50/50 + full `make check` clean. `make check` is stable again.
- **Auto-review (sqlgen-reviewer):** 7/7 PRD PASS, 6/6 required tests present, 0 blocking. Two non-blocking regression notes: (1) medium — `indexDocument` hand-mirrors `Document`'s top-level envelope with no parity enforcement; **addressed in-place** by adding `TestEmitJSON_IndexEnvelopeMatchesDocumentTopLevel` (black-box: single vs per-entity-index top-level key sets must match). (2) low — schema under-constrains the always-emitted `conventions` sub-blocks (`call_options` / `soft_delete` / `comparator` / `omittable` not in `required` nor shape-validated); coverage gap only (additionalProperties keeps output valid). Left for 18.8 (`manifest validate`) / 18.10 to tighten. Reviewer also noted the phase-doc's stale `layered` toggle wording — code correctly uses PRD §30.4.3's `graph_top_level`.
- **`/verify 18.3` (2026-07-09):** 7/7 PRD PASS, 6/6 required tests present, `make check` + `make check-examples` clean; independent reviewer in full agreement, 0 blocking. Two non-blocking follow-ups tracked to their owning sub-items rather than logged as FIXes (all 18.3 spec requirements + Tests Required are satisfied): (a) **schema-vs-real-`Build()` coverage** — the 18.3 emitter tests only validate a hand-built fixture Document, so a builder-side regression (`nil`→`null` on a required array, unlisted enum, renamed field) would slip past 18.3; added an explicit real-orchestrated-`Build → EmitJSON → schema-validate` task + acceptance criterion + Tests-Required line to **18.9** (guards the FIX-068 / FIX-059 divergence shape). (b) **conventions `required` tightening** — added an explicit pre-freeze task to **18.10**. Housekeeping: corrected the stale `$schema` owner in this sub-item's acceptance criteria (`teandresmith` → `tsly-tech`, matching `go.mod` + `builder.go:schemaURL`).

---

## 18.4 Markdown emission

**PRD Reference:** §30.3 (file layout), §30.4.1 (conventions block).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.4; `docs/design/MANIFEST.md` §5.1.1 (Features line), §5.3 (Generated SQL block).

**Module:** `cmd/sqlgen/manifest/emit_markdown.go`, `cmd/sqlgen/manifest/templates/{_index,_conventions,entity}.md.tmpl`

**Status:** Complete

**Depends on:** 18.2.

### Tasks

- [x] Implement `EmitMarkdown(...)` in `cmd/sqlgen/manifest/emit_markdown.go`. **Signature adapted** (same rationale as 18.3's `EmitJSON`): `EmitMarkdown(doc *Document, mfst *config.ManifestConfig, outputDir string) error` — reads `MarkdownDir` off the resolved `ManifestConfig` (single source of truth, honors config override) rather than a loose `outDir` scalar. `include_examples` is already applied at build time (builder only populates `Entity.Examples` when set), so the emitter just renders `Examples` when present.
- [x] Author `cmd/sqlgen/manifest/templates/_index.md.tmpl`:
  - Header (package name, dialect, generator version).
  - **Features line** sourced from `generation_config`: comma-separated list of enabled toggles (e.g. `Features: cache, events, soft_delete, pagination`). Fallback: `Features: none` when all toggles are false.
  - Entity scan table: one row per entity with name, table, kind, file_prefix, brief description from comment.
  - Cross-reference link to `_conventions.md`.
- [x] Author `cmd/sqlgen/manifest/templates/_conventions.md.tmpl`:
  - **Entry points** (`c.Q.<Entity>()` / `c.M.<Entity>()`).
  - **Error sentinels** with GraphQL codes. *(Corrected 2026-09-03 by FIX-180: this originally specified 5 entries — `ErrNotFound`, `ErrConflict`, `ErrBadReference`, `ErrInvalidInput`, `ErrForbidden` — of which only `ErrNotFound` exists; the other four are PRD §26.5.5 GraphQL `extensions.code` values, not Go identifiers. The template now renders the seven §22.1 sentinels plus the two `tenancy` ones when tenancy is configured, and a sibling constraint-code table.)*
  - **Pagination** convention reference (Page type, list envelope suffix, cursor encoding).
  - **CallOptions** package walkthrough.
  - **Soft delete** convention (column name + type).
  - **Comparator package walkthrough**: families (ID, String, Number, Time, Bool, Bytes, Enum, JSON), fields, composition rules, examples.
  - **Omittable package walkthrough**: purpose, construction patterns (`omittable.Set(...)` / `omittable.Unset[T]()` / `omittable.Null[T]()`), methods (`IsSet`, `IsNull`, `Get`, `MustGet`), JSON marshaling semantics.
- [x] Author `cmd/sqlgen/manifest/templates/entity.md.tmpl` — per-entity markdown:
  - Header (Go struct name, SQL table name, kind, comment).
  - Files section (`<file_prefix>_gen.go` and any siblings).
  - Columns table — `name | go_field | go_type | db_type | nullable | pk | unique | default | comparator | comment`.
  - Indexes table — `name | columns | unique | method | where` (new in 18.x).
  - CHECK-constraints subsection — when any column has `check`, render flattened per-column.
  - Relationships table — `name | kind | target | fk | filter`.
  - Query methods subsection — one entry per method with params, returns, errors, notes.
  - Mutation methods subsection — same shape.
  - **Per-method Generated SQL subsection** — sourced from `methods[].sql_bodies`. One sql-fenced code block per dialect key (single-dialect packages: one block).
  - Filter type subsection.
  - Sort type subsection.
  - Read examples + write examples (gated by `include_examples`).
- [x] Use `text/template` (NOT `html/template` — avoids HTML escaping) with pre-sorted slices passed into execution context. Deterministic byte output. (Every map — `sql_bodies`, comparator `composition`/`examples` — is projected to a sorted slice by a funcmap helper; no template ranges a Go map.)
- [x] Per-entity filename reuses the existing `_gen.go` file prefix from `Entity.FilePrefix`. (Amended by FIX-163: this was written as "matches the SQL table name verbatim", which the `_gen.go` prefix never did — it singularizes. `Entity.FilePrefix` is now `TableContext.SnakeName` read directly, so `users` → `manifest/user.md` beside `user_gen.go`.)
- [x] Multi-schema disambiguation mirrors Go file naming. (Amended by FIX-163: the "inherited for free" note was wrong — the builder's private `filePrefix` prefixed on `schema != ""`, while `StructName` prefixes only on a cross-schema bare-name collision, so a lone `audit.audit_users` was published as `audit_audit_user`. Reading `SnakeName` makes the mirroring real: `public.users` alongside `audit.users` → `manifest/public_user.md`; a lone `public.users` → `manifest/user.md`.)

### Acceptance Criteria

- `_index.md` renders the Features line correctly: `cache, events, soft_delete` when those three toggles are true; `none` when all false.
- `_conventions.md` covers all 8 reference sections (entry points, error sentinels, pagination, CallOptions, soft-delete, comparator walkthrough, omittable walkthrough) — pinned by content fixture.
- Per-entity `.md` renders the Generated SQL block with one ` ```sql ` fence per dialect; multi-dialect packages render multiple blocks ordered alphabetically by dialect.
- Per-entity `.md` includes the new Indexes table when entity has indexes; omits the CHECK subsection cleanly when no column has a check constraint.
- Markdown output is byte-deterministic across runs (no map iteration; all slices sorted).
- Filename collision: multi-schema schemas with same-named tables get the `<schema>_<table>.md` form to match the Go file naming.

### Tests Required

- [x] Byte-equal golden snapshots for `_index.md`, `_conventions.md`, and per-entity files (`users.md` table + `active_users.md` view) against the shared fixture Document (`TestEmitMarkdown_Goldens`). Goldens under `testdata/golden/markdown/`, regen via `-update`.
- [x] `TestEmitMarkdown_FeaturesLine`:
  - All toggles on → `Features: audit_columns, cache, events, graph_top_level, graphql, pagination, soft_delete, tenancy, views`. **Deviation from the doc's original string:** the toggle the doc wrote as `layered` is emitted as `graph_top_level` — the `generation_config` field was renamed in Phase 20 (PRD §30.4.3); labels are the JSON field names in alphabetical order.
  - All toggles off → `Features: none`.
  - Mixed → only enabled toggles listed.
- [x] `TestEmitMarkdown_GeneratedSQLBlock`:
  - Method with `sql_bodies` populated → renders the block.
  - Method without `sql_bodies` (or empty map) → omits the block cleanly (method entry still renders).
  - Multi-dialect map → multiple ` ```sql ` blocks ordered alphabetically.
- [x] `TestEmitMarkdown_Determinism` — twice-emit byte equality across all four artifact files.
- [x] `TestEmitMarkdown_MultiSchemaFilename` — schema-qualified entity (`FilePrefix: public_users`) → `public_users.md`, no bare `users.md` (pins the §30.3 `<schema>_<table>.md` form; added post-review to close the reviewer's UNTESTED note).

### Completion Record

**2026-07-09 — landed.**

Files changed:
- `cmd/sqlgen/manifest/emit_markdown.go` (new) — `EmitMarkdown(doc, mfst, outputDir)` + `renderMarkdown` / `normalizeMarkdown` / `writeMarkdownFile`. Templates are embedded via `//go:embed templates/*.md.tmpl` and parsed once into a package-level `text/template` set (funcmap-wired). Funcmap helpers (all pure, deterministic): `featuresLine` (sorted `generation_config` toggle labels or `none`), `join`, `mark` (bool→`yes`/``), `mdCell` (flattens newlines + escapes `|` for table cells; HTML-special chars pass through), `firstLine`, `sqlBlocks` (map→dialect-sorted `[]sqlBlock`), `sortedPairs` (map→key-sorted `[]pair`), `fkCell` (FK / M2M-junction rendering), `checkedColumns` (columns carrying a CHECK). `normalizeMarkdown` collapses 3+ newlines to one blank line + guarantees a single trailing newline, so section-boundary template whitespace never leaks more than one blank line (keeps the templates maintainable without whitespace-golf).
- `cmd/sqlgen/manifest/templates/_index.md.tmpl` (new) — header (package / dialect / generator / schema_version), the `Features:` line, entity scan table (name, table, kind, linked file, brief description from the comment's first line), cross-reference to `_conventions.md`.
- `cmd/sqlgen/manifest/templates/_conventions.md.tmpl` (new) — all 8 convention sub-blocks (client entry points + `find_returns_nil_on_missing`, error sentinels table, pagination, CallOptions, soft-delete, comparator walkthrough w/ families + shape-notes + composition + examples, omittable walkthrough).
- `cmd/sqlgen/manifest/templates/entity.md.tmpl` (new) — header (struct/table/kind/source/comment), Files, Primary key, Columns table, Indexes table (when present), Check-constraints subsection (`with checkedColumns`, omitted cleanly when none), Relationships table (when present), Query + Mutation method subsections (shared `method` named template with per-method Generated SQL — one ` ```sql ` fence per dialect, dialect-labeled, alphabetical), Filter / Sort subsections, gated Read/Write examples.
- `cmd/sqlgen/manifest/emit_markdown_test.go` (new) — `TestEmitMarkdown_Goldens` (4 files), `TestEmitMarkdown_FeaturesLine` (all-on / all-off / mixed), `TestEmitMarkdown_GeneratedSQLBlock` (populated / empty / multi-dialect-ordered), `TestEmitMarkdown_Determinism`. Reuses `newFixtureDocument` / `manifestCfg` / `checkGolden` / `readFile` from `emit_json_test.go` (same `manifest_test` package).
- `cmd/sqlgen/manifest/testdata/golden/markdown/{_index,_conventions,users,active_users}.md` (new) — 4 golden files.

Verification:
- `make check` clean across all 8 modules under `-race` (lint 0 issues; `cmd/sqlgen/manifest` ~3.0s). Markdown tests stable.
- `make check-examples` clean across all example modules (Docker up; graphql/tests ~33.7s, mysql ~10s) — triggered mechanically by the `cmd/sqlgen/manifest/**` path glob. No example output changes (emitter not yet wired into `orchestrate` — that lands in 18.6; examples don't import `cmd/sqlgen/manifest`).
- `make test-integration` not re-run — emitter is pure in-memory serialization, no SQL/runtime surface.
- No `/security-review` trigger: changes are in `cmd/sqlgen/manifest/` (builder-side, not the top-level `//go:embed` runtime `manifest/`, which lands in 18.7) and touch no `cmd/sqlgen/cli/**` external-file reader.

Notes / deviations:
- **Signature adaptation:** `EmitMarkdown(doc *Document, mfst *config.ManifestConfig, outputDir string)` (not the doc's `(doc, outDir)`), matching 18.3's `EmitJSON` so `markdown_dir` overrides are honored from the resolved config. `include_examples` needs no emitter-side flag — the builder already gates `Entity.Examples` population.
- **`layered` → `graph_top_level`:** the doc's `TestEmitMarkdown_FeaturesLine` all-on string listed `layered`; the actual `generation_config` toggle is `graph_top_level` (renamed in Phase 20, PRD §30.4.3). Test + phase doc updated to the real field name. No code drift — `featuresLine` reads the same struct the JSON snapshot serializes.
- **Determinism:** no template ranges a Go map; every map is projected to a sorted slice in Go first. `normalizeMarkdown` additionally guards blank-line runs.
- **Auto-review (sqlgen-reviewer):** 6/7 PRD PASS + 1 PARTIAL (multi-schema filename untested), 4/4 required tests present, 0 blocking. Two items addressed in-place before `/done`: (1) reviewer flagged that `normalizeMarkdown` collapsed blank lines *globally* including inside ` ```sql `/` ```go ` fences — a latent data-loss risk once 18.6 populates multi-line examples; made it **fence-aware** (blank lines inside a fence are preserved verbatim). Goldens are byte-identical (current fixtures have single-line SQL + no examples). (2) added `TestEmitMarkdown_MultiSchemaFilename` to close the UNTESTED multi-schema filename note. Reviewer nits (non-blocking, no action): MANIFEST §18.4 names the CHECK block a "Validation" subsection while the template renders `## Check constraints` (phase-doc acceptance doesn't pin the heading); "all 8 reference sections" is a doc miscount (7 headed sections; the 8th, `find_returns_nil_on_missing`, is folded under entry points). Reviewer also confirmed the omittable/comparator convention data is 18.2-builder-owned (matches MANIFEST §6.2), not an emitter concern.
- **`/verify 18.4` (2026-07-09):** 7/7 PRD PASS, 5/5 required tests present, `make check` + `make check-examples` clean; independent reviewer in full agreement, 0 blocking. One tracked coverage gap logged + resolved before `/done`: **FIX-123** — the per-entity `## Read/Write examples` template branch was implemented but untested (`Entity.Examples` nil in `newFixtureDocument`). Resolved via `/fix-implement FIX-123` (2026-07-09): added `TestEmitMarkdown_Examples` (read+write / read-only subtests) exercising the branch; the fence-preservation assertion uses **two consecutive** blank lines and is mutation-verified (neutering `normalizeMarkdown`'s `inFence` guard fails the test), genuinely regression-guarding the fence-awareness added during 18.4 review. Test-only, no production change.

---

## 18.5 Breadcrumb files + package doc-comment pointer

**PRD Reference:** §30.3.
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.5

**Module:** `cmd/sqlgen/manifest/emit_breadcrumbs.go`, `cmd/sqlgen/manifest/templates/breadcrumb.md.tmpl`, `cmd/sqlgen/manifest/templates/breadcrumb_layered.md.tmpl`

**Status:** Complete

**Depends on:** 18.2.

### Tasks

- [x] Implement `EmitBreadcrumbs(in BreadcrumbsInput) ([]string, error)` in `cmd/sqlgen/manifest/emit_breadcrumbs.go`. **Signature adapted** (same rationale as 18.3's `EmitJSON` / 18.4's `EmitMarkdown`): takes a `BreadcrumbsInput` struct (`Doc`, `Breadcrumbs config.BreadcrumbsConfig`, `OutputDir`, `MarkdownDir`, `GraphDir`) and **returns the user-owned-file warnings** rather than printing them — `gen.Generate` has no warning channel today, so 18.6 threads the returned `[]string` into the CLI's existing `printWarnings` path. The layered-mode "graph package also gets breadcrumbs" case is driven by `GraphDir != ""` (there is no stored `layered` bool — PRD §30.4.3 infers graph topology from the resolved dir), which 18.6 sets to the resolved graph dir when GraphQL is enabled.
- [x] Author `cmd/sqlgen/manifest/templates/breadcrumb.md.tmpl` — single source for both `CLAUDE.md` and `AGENTS.md` (identical content per PRD §30.3):
  - Header pointing at `manifest/_index.md`.
  - Pointers to entity-level files (`manifest/<table>.md` per entity).
  - Pointer to `_conventions.md` for shared reference.
- [x] Author `cmd/sqlgen/manifest/templates/breadcrumb_layered.md.tmpl` — for the `graph/` package in layered mode:
  - Pointer 1: models manifest via relative path (computed at generation time from `output.api.graph_dir` to `output.dir`).
  - Pointer 2: sibling `*_gen.graphqls` files in the same directory (self-describing GraphQL schema).
  - Pointer 3: PRD §26 (project-invariant resolver / walker / middleware semantics).
- [x] Emit `<output.dir>/CLAUDE.md` when `breadcrumbs.claude_md: true`. Emit `<output.dir>/AGENTS.md` when `breadcrumbs.agents_md: true`. Single template drives both (rendered once, written to both paths → byte-identical).
- [x] In layered mode (`GraphDir` set): also emit `<GraphDir>/CLAUDE.md` and `<GraphDir>/AGENTS.md` using the layered template.
- [x] **Provenance marker + non-destructive collision handling** (PRD §30.3 *Breadcrumb ownership*). Breadcrumbs land in `<output.dir>/` (default `.`, the module root) where a hand-authored `CLAUDE.md` commonly already exists, so sqlgen must not blindly overwrite:
  - [x] Prepend the leading HTML-comment provenance marker to both templates via the `{{ marker }}` funcmap (sourced from the `breadcrumbMarker` const, single source of truth). Exact block per PRD §30.3:
    ```
    <!-- Generated by sqlgen — manifest breadcrumb.
         sqlgen rewrites this file on each `sqlgen generate` and removes it when the manifest
         is disabled. To manage this file yourself, delete this comment; sqlgen will then never
         overwrite or remove it. -->
    ```
  - [x] Before writing each breadcrumb, read any existing file at the target path and apply the ownership rule: **absent** → write; **present with marker** (leading comment block contains the literal `Generated by sqlgen`) → overwrite; **present without marker** → skip the write, do not error.
  - [x] On the skip branch, emit a generation-time warning (not a hard fail) naming the file, offering the copy-pasteable pointer block for agent auto-discovery, and pointing at the `breadcrumbs.<flag>: false` opt-out. Message shape per PRD §30.3 warning example. (Returned to the caller as a warning string; 18.6 prints via `printWarnings`.)
  - [x] Detection helper `BreadcrumbIsSqlgenOwned(path) (bool, error)` (**exported** so 18.6's stale cleanup in the `gen` package can reuse it) shared by emit (18.5) and stale cleanup (18.6) — single source of truth for the marker check (core `hasSqlgenMarker`). Marker is authored **only** here; it is never part of the pasteable pointer block (`pointerBlock`), so pasting the block into a user file does not hand it to sqlgen.
- [ ] Extend existing per-file generated-header pass: when `breadcrumbs.package_doc: true`, inject a one-line manifest pointer (e.g. `// Manifest: see manifest/_index.md`) into `models_gen.go`'s `// Package <name>` doc comment. **Deferred to 18.6** — this is a `cmd/sqlgen/gen` orchestrate/preamble concern (`WrapWithPreamble`/`injectHeader`; no generated file carries a `// Package` doc comment today) with no call site until 18.6 wires the manifest stage into `Generate`. Mirrors 18.3's `--manifest-timestamp` deferral, and 18.6's stale-cleanup task list already owns "Manifest pointer in `models_gen.go` doc comment (regenerated each run; doc-comment line cleared when `package_doc: false`)".
- [x] Resolve relative path computation: from `GraphDir` to `<output.dir>/<markdown_dir>/_index.md` via `filepath.Rel` + `filepath.ToSlash` (`relIndexPath`); pinned in `TestEmitBreadcrumbs_LayeredRelativePath` (top-level sibling + nested-graph cases).

### Acceptance Criteria

- `CLAUDE.md` and `AGENTS.md` have byte-identical content (single template, two output paths), including the leading provenance marker.
- In layered mode, `graph/CLAUDE.md` and `graph/AGENTS.md` carry the three-pointer layout (models manifest path, `*_gen.graphqls`, PRD §26) plus the provenance marker.
- **Ownership rule (PRD §30.3):** an existing breadcrumb file whose leading comment block contains `Generated by sqlgen` is overwritten; one without the marker is left untouched and generation continues (no hard fail).
- A pre-existing user-authored `CLAUDE.md` (no marker) at `<output.dir>/` survives generation verbatim, and a warning is emitted naming the file, offering the pointer block, and citing the `breadcrumbs.claude_md: false` opt-out.
- Deleting the marker line from a previously sqlgen-authored breadcrumb converts it to user-owned: the next run neither overwrites nor deletes it.
- The relative path from `graph/CLAUDE.md` to `models/manifest/_index.md` is correct on both POSIX and Windows path separators (use `filepath.ToSlash` for output to keep markdown portable).
- `models_gen.go` package doc comment carries the manifest pointer line when `package_doc: true`; reverts cleanly when toggled off (covered by 18.6 stale cleanup).
- Opt-out semantics across the three sub-flags:
  - `claude_md: false` → no `CLAUDE.md` emitted.
  - `agents_md: false` → no `AGENTS.md` emitted.
  - `package_doc: false` → no manifest pointer in `models_gen.go` doc comment.

### Tests Required

- [x] Content pin for `CLAUDE.md` + `AGENTS.md` (byte-identical assertion), including the leading provenance marker (`TestEmitBreadcrumbs_Goldens`).
- [x] Content pin for `graph/CLAUDE.md` + `graph/AGENTS.md` in layered-mode variant (`TestEmitBreadcrumbs_LayeredGoldens`).
- [x] `TestEmitBreadcrumbs_OptOut` — `claude_md: false` → no `CLAUDE.md` (AGENTS.md still written); `agents_md: false` → no `AGENTS.md` (CLAUDE.md still written). The `package_doc` sub-flag governs the `models_gen.go` doc-comment pointer, which `EmitBreadcrumbs` does not touch (deferred to 18.6); its opt-out is exercised in 18.6's `orchestrate_test.go`.
- [x] Relative-path computation pinned for the layered-mode case (`TestEmitBreadcrumbs_LayeredRelativePath`, top-level sibling + nested-graph).
- [x] `TestEmitBreadcrumbs_Ownership` — sub-tests over the four ownership states:
  - Target absent → file written with marker.
  - Target present *with* marker → overwritten (byte-equals freshly-rendered content, stale body gone).
  - Target present *without* marker (hand-authored) → left byte-identical; warning emitted (asserts the file path, the pointer block, and the `breadcrumbs.claude_md: false` opt-out appear in the warning).
  - Marker line deleted from a prior sqlgen file → treated as user-owned (not overwritten; warning emitted).
- [x] `TestEmitBreadcrumbs_MarkerNotInPointerBlock` — asserts the pasteable pointer block surfaced in the warning does **not** contain the `Generated by sqlgen` marker (guards the self-clobber path).
- [x] `TestEmitBreadcrumbs_Determinism` — twice-emit byte equality across all four artifact files (models + layered).

### Completion Record

**2026-07-09 — landed (breadcrumb emitter + templates + ownership; package-doc pointer deferred to 18.6).**

Files changed:
- `cmd/sqlgen/manifest/emit_breadcrumbs.go` (new) — `EmitBreadcrumbs(BreadcrumbsInput) ([]string, error)` + `writeBreadcrumbPair` / `writeBreadcrumb` (ownership rule: absent → write, marker → overwrite, no-marker → skip+warn) / `BreadcrumbIsSqlgenOwned` (exported, shared with 18.6 stale cleanup) / `hasSqlgenMarker` (leading-comment-block marker check, so deleting the marker line disowns the file) / `ownershipWarning` + `pointerBlock` (marker-free pasteable block) / `modelsBreadcrumbData` / `layeredBreadcrumbData` / `relIndexPath` (`filepath.Rel` + `ToSlash`) / `deref`. `breadcrumbMarker` const is the single source for both the `{{ marker }}` funcmap and the `breadcrumbMarkerID` detection substring.
- `cmd/sqlgen/manifest/templates/breadcrumb.md.tmpl` (new) — marker + header pointing at `_index.md` + per-entity pointer list + `_conventions.md` pointer. Single template drives both `CLAUDE.md` and `AGENTS.md`.
- `cmd/sqlgen/manifest/templates/breadcrumb_layered.md.tmpl` (new) — marker + the three-pointer layered layout (models manifest via computed relative path, sibling `*_gen.graphqls`, PRD §26).
- `cmd/sqlgen/manifest/emit_markdown.go` — added `"marker"` to `markdownFuncs` (the breadcrumb templates share the package markdown template set / `//go:embed templates/*.md.tmpl`, so the funcmap must resolve `marker` at parse time).
- `cmd/sqlgen/manifest/emit_breadcrumbs_test.go` (new) — `TestEmitBreadcrumbs_Goldens`, `_LayeredGoldens`, `_LayeredRelativePath` (2 cases), `_OptOut` (claude_md / agents_md), `_Ownership` (4 states), `_MarkerNotInPointerBlock`, `_Determinism`. Reuses `newFixtureDocument` / `checkGolden` / `readFile` from `emit_json_test.go` (same `manifest_test` package).
- `cmd/sqlgen/manifest/testdata/golden/breadcrumbs/{CLAUDE,graph_CLAUDE}.md` (new) — 2 golden files (AGENTS.md is asserted byte-equal to CLAUDE.md rather than pinned separately).

Verification:
- `make check` clean across all 8 modules under `-race` (lint 0 issues; `cmd/sqlgen/manifest` ~2.8s).
- `make check-examples` clean across all example modules (Docker up; graphql/tests ~33.4s, mysql ~11.5s) — triggered mechanically by the `cmd/sqlgen/manifest/**` path glob. No example output changes (emitter not yet wired into `orchestrate` — 18.6; examples don't import `cmd/sqlgen/manifest`).
- `make test-integration` not re-run — emitter is pure in-memory serialization + local file I/O, no SQL/runtime surface.
- No `/security-review` trigger: changes are in builder-side `cmd/sqlgen/manifest/` (not the top-level `//go:embed` runtime `manifest/`, which lands in 18.7) and touch no `cmd/sqlgen/cli/**` external-file reader.

Notes / deviations:
- **Signature adaptation + returned warnings.** `EmitBreadcrumbs(BreadcrumbsInput) ([]string, error)` — struct input (matching 18.3/18.4's config-driven adaptation) and the ownership warning is **returned** rather than printed, because `gen.Generate` has no warning channel and the CLI already owns `printWarnings`. 18.6 threads the `[]string` into that path.
- **Layered driven by `GraphDir`, not a `layered` bool.** There is no stored layered flag; PRD §30.4.3 infers graph topology. `GraphDir != ""` (set by 18.6 when GraphQL is enabled) triggers the graph breadcrumbs. Aligns with PRD §30.3's "when GraphQL is enabled, the graph package receives breadcrumbs."
- **Package-doc pointer deferred to 18.6** (one task + its `package_doc` opt-out acceptance/test line left `[ ]`/noted). It's a `cmd/sqlgen/gen` preamble concern with no call site until the manifest stage is wired into `Generate`; 18.6's task list already owns it. Same deferral shape as 18.3's `--manifest-timestamp`.
- **`BreadcrumbIsSqlgenOwned` exported** so 18.6's stale cleanup (in the `gen` package) can reuse the exact marker check — the phase doc sketched it unexported (`breadcrumbIsSqlgenOwned`), but cross-package reuse requires export.
- Auto-review (sqlgen-reviewer subagent) summary: see runner output.

---

## 18.6 Pipeline wiring + stale cleanup

**PRD Reference:** §30.3 (stale cleanup), PRD §23.8 (stale-file contract).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.6

**Module:** `cmd/sqlgen/cli/generate.go`, `cmd/sqlgen/cli/root.go`, `cmd/sqlgen/manifest/emit.go` (new), `cmd/sqlgen/manifest/clean.go` (new), `cmd/sqlgen/gen/orchestrate.go`, `cmd/sqlgen/gen/context_manifest.go`. **Placement deviation:** the manifest stage lives in the CLI coordinator + a new `manifest` sub-package, NOT inside `gen.Generate` as the phase doc originally sketched. `cmd/sqlgen/manifest` imports `cmd/sqlgen/gen` (for `TableContext`/`ViewContext`/`APIContext`/`BuildManifestEmbedContext`), so calling `manifest.Build`/`Emit*` from inside `gen.Generate` would form an import cycle. The CLI is the one layer that imports both. See Completion Record.

**Status:** Complete

**Depends on:** 18.3, 18.4, 18.5, 18.7.

### Tasks

- [x] Hook the manifest stage into the pipeline AFTER all other generation stages so it reflects the landed code shape. **Realized as a CLI-coordinated stage** (`generate.go` step 7c → `manifest.RunStage`) rather than inside `gen.Generate` (import-cycle constraint above). Order:
  1. Parse schema
  2. Build contexts
  3. Emit entity `_gen.go` files
  4. Emit shared / unified-client / API files
  5. Build `*manifest.Document` (calls 18.2 `manifest.Build`)
  6. Emit JSON (calls 18.3 `manifest.EmitJSON`)
  7. Emit markdown (calls 18.4 `manifest.EmitMarkdown`)
  8. Emit breadcrumbs (calls 18.5 `manifest.EmitBreadcrumbs`)
  9. Emit `manifest_embed_gen.go` (18.7) — call `gen.BuildManifestEmbedContext(cfg, len(doc.Entities))` and, when non-nil, `renderAndWrite(tmpl, "manifest-embed", ctx, ctx.Package, ctx.Imports, <output.dir>/manifest_embed_gen.go, version)`. **Pass the real entity count** (`len(doc.Entities)`): the context downgrades a zero-entity per_entity package to the single-file embed so the generated `//go:embed all:manifest/entities` never targets a non-existent directory (18.7 already guards this in `BuildManifestEmbedContext` + a test; 18.6 only has to thread the count).
  10. Stale-file cleanup pass
  11. Formatter pass (`gofmt` / `goimports`)
- [x] Gate the manifest stage on `cfg.Generation.Manifest != nil && *cfg.Generation.Manifest.Enabled` (`manifest.manifestEnabled`). When off, `RunStage` skips build/emit entirely; stale cleanup still runs to clear any prior artifacts.
- [x] **Add the `--manifest-timestamp` flag to the `generate` CLI command** (`cmd/sqlgen/cli/root.go` + `generate.go`), threading it → `manifest.StageInput.Timestamp` → `BuildInput.Timestamp` → `doc.GeneratedAt`. **Threading deviation:** goes flag → `StageInput` (not `gen.Options`), since the manifest stage is CLI-coordinated (above), not inside `gen.Generate`. The builder already consumes `BuildInput.Timestamp` (`builder.go:74`) and the 18.3 emitter is a pure reader, so this stage only populates the flag value. Unset → `time.Now().UTC().Format(time.RFC3339)` (`resolveManifestTimestamp`). **Deferred here from 18.3.** Pins `generated_at` for golden-test determinism (PRD §30.3; needed by the 18.9 E2E goldens).
- [x] Extend stale-file cleanup per PRD §23.8 to cover the manifest surface (`manifest.CleanStale`; **absorbs 18.3's deferred cleanup item** — directory-glob-based over deterministic config-derived paths, no emitter path-list threaded from 18.3):
  - `<output.dir>/manifest/*.md` (per-entity markdown removed when entity removed from schema)
  - `<output.dir>/manifest/entities/*.json` (per-entity JSON removed similarly in per-entity layout)
  - `<output.dir>/manifest/manifest_gen.json` (cleared when enabled flips true→false)
  - `<output.dir>/manifest/_index.md` and `<output.dir>/manifest/_conventions.md` (cleared on opt-out)
  - `<output.dir>/CLAUDE.md` / `<output.dir>/AGENTS.md` (cleared on per-flag opt-out) — **marker-gated: only removed when the file carries the `Generated by sqlgen` provenance marker** (PRD §30.3 *Breadcrumb ownership*). A user-authored or user-adopted (de-marked) breadcrumb is never deleted. Reuses the 18.5 `breadcrumbIsSqlgenOwned` detection helper.
  - `<output.dir>/manifest_embed_gen.go` (cleared when `embed_in_client: false`)
  - Layered-mode breadcrumbs: `<output.api.graph_dir>/CLAUDE.md` + `<output.api.graph_dir>/AGENTS.md` — same marker-gated deletion rule.
  - Manifest pointer in `models_gen.go` doc comment (regenerated each run; doc-comment line cleared when `package_doc: false`) — **DEFERRED to 18.9.** This is the sole remaining `breadcrumbs.package_doc` obligation (18.5 already deferred the *injection* here). It touches `gen`'s shared file-header path (`WrapWithPreamble`/`injectHeader`) — a `// Package <name>` doc comment is not emitted on any generated file today — so it carries golden-churn risk across every `models_gen.go` and has **no test in this sub-item's Tests Required**. It is best landed against the 18.9 E2E example vehicle that can validate the header change end-to-end. All other cleanup targets (markdown, per-entity JSON, top-level JSON, `_index`/`_conventions`, marker-gated breadcrumbs, `manifest_embed_gen.go`, layered graph breadcrumbs) are implemented + tested here.
- [x] Treat the entire `<output.dir>/manifest/` directory as removed when manifest is disabled — `CleanStale` `os.RemoveAll`s the directory itself (not just files inside it).
- [x] Disabled-by-default behavior: with no `manifest:` block in `sqlgen.yml`, no manifest artifacts are emitted and no warnings produced (`TestRunStage_DisabledByDefault`).

### Acceptance Criteria

- Orchestrator-emitted files match the order above; manifest building reads from the same in-memory contexts that produced the `_gen.go` files (no re-parsing).
- A schema with `manifest.enabled: true` produces the full artifact set per PRD §30.3.
- Toggling `manifest.enabled: false` after a prior `true` removes the entire `manifest/` directory + the **marker-carrying** breadcrumb files + `manifest_embed_gen.go` + the doc-comment pointer line — on the next run. Breadcrumb files lacking the `Generated by sqlgen` marker (user-authored or user-adopted) are preserved (PRD §30.3).
- Removing a table from the schema removes its per-entity markdown and (per-entity-layout) JSON file on the next run.
- Disabled-by-default: omitting the `manifest:` block produces zero manifest artifacts and zero warnings.

### Tests Required

- [x] Orchestration tests (**relocated** to `cmd/sqlgen/manifest/emit_test.go` from the sketched `gen/orchestrate_test.go`, since the stage is CLI-coordinated + lives in the `manifest` package — exercising `manifest.RunStage`/`CleanStale` directly over real gen contexts is the faithful, isolated coverage):
  - `TestRunStage_DisabledByDefault`: no `manifest:` block, assert no manifest files + zero warnings.
  - `TestRunStage_PerTableOptOut`: `tables.audit_logs.manifest.enabled: false`, assert `audit_log.md` + `entities/audit_log.json` not emitted while `product.*` are.
  - `TestRunStage_EmitsFullArtifactSet`: enabled → full artifact set present (JSON, `_index`/`_conventions`, per-entity md, `CLAUDE.md`/`AGENTS.md`, `manifest_embed_gen.go`).
- [x] Stale-cleanup tests:
  - `TestCleanStale_RemovedTable` (subtests `single` + `per_entity`) — generate with table, regenerate without, assert per-entity md/json gone, surviving entity retained.
  - `TestRunStage_EnabledFlipRemovesArtifacts` — `enabled` flip true→false removes entire `manifest/` dir + marker breadcrumbs + embed file.
  - `TestRunStage_BreadcrumbOptOut` — `claude_md: true→false` removes `CLAUDE.md` (marker present); `AGENTS.md` retained.
  - `TestRunStage_MarkerGatedDeletionPreservesUserFile` — a hand-authored (marker-less) `CLAUDE.md` is preserved verbatim on both the enabled run (skip + warn) and the disable flip (not deleted); sqlgen-owned `AGENTS.md` still swept.
  - `TestCleanStale_GuardsAgainstOutputDirWipe` (added post-review) — disabled config with `markdown_dir: "."` must not `RemoveAll` the output tree; a seeded output-dir file survives.
- [x] `--manifest-timestamp` determinism: `TestRunStage_TimestampDeterminism` (two runs, same timestamp → byte-identical `manifest_gen.json`, `generated_at` pinned) + `TestResolveManifestTimestamp` in `cli/generate_test.go` (flag verbatim; unset → valid RFC3339 UTC).

### Completion Record

**2026-07-09 — landed (core pipeline wiring + stale cleanup + timestamp; one item deferred to 18.9).**

**Placement / architecture (deviation from the phase-doc sketch).** The phase doc placed this in `cmd/sqlgen/gen/orchestrate.go`. That is architecturally impossible: `cmd/sqlgen/manifest` imports `cmd/sqlgen/gen` (`BuildInput.Tables []gen.TableContext`, `BuildManifestEmbedContext`, etc., verified via `go list -deps`), so `gen.Generate` calling `manifest.Build`/`Emit*` would form an import cycle. The manifest stage is therefore coordinated from the **CLI** (the one layer that imports both) after `gen.Generate` returns, via a new `manifest.RunStage` orchestration entry point. Same "adapt the pre-implementation sketch + document" pattern as 18.1's file-path adaptation and 18.3/18.4/18.5's signature adaptations.

Files changed:
- `cmd/sqlgen/gen/orchestrate.go` — `GenerateResult` now carries the in-memory `Tables []TableContext` / `Views []ViewContext` / `API *APIContext` so the downstream manifest stage reuses the exact contexts that produced the `_gen.go` files (satisfies the "no re-parsing / same in-memory contexts" acceptance criterion without a rebuild). `generateAPI` refactored to also return the built `*APIContext`.
- `cmd/sqlgen/gen/context_manifest.go` — new exported `RenderManifestEmbed(ctx, outputDir, version)` wrapping the package-private `renderAndWrite("manifest-embed", …)` pipeline (the template + preamble/goimports plumbing are `gen`-private; the manifest stage calls this exported entry point). No-op on nil ctx (embed disabled). Loads the template set with the Postgres dialect purely to satisfy `loadTemplates` — the template uses no dialect funcs.
- `cmd/sqlgen/manifest/emit.go` (new) — `RunStage(StageInput) ([]string, error)`: gates on `manifestEnabled`; when enabled, `Build` → `EmitJSON`/`EmitMarkdown` (each gated by `formats`) → `EmitBreadcrumbs` → `gen.BuildManifestEmbedContext(cfg, len(doc.Entities))` + `gen.RenderManifestEmbed` → `CleanStale`; when disabled, `CleanStale` only. Returns the breadcrumb user-owned-file warnings. `ResolveGraphBreadcrumbDir` exposes the layered-mode graph dir (reuses the builder's `graphTopLevel`) so breadcrumbs/cleanup manage `graph/CLAUDE.md`+`AGENTS.md` only in layered mode.
- `cmd/sqlgen/manifest/clean.go` (new) — `CleanStale(CleanInput)` per PRD §23.8/§30.3: disabled → `os.RemoveAll` the whole `<markdown_dir>/` + marker-gated breadcrumb deletion (both output + layered graph dir) + `manifest_embed_gen.go`; enabled → per-flag breadcrumb opt-out (marker-gated), embed removal when `embed_in_client:false`, glob-prune orphaned per-entity `*.md` (keep `_index`/`_conventions`+current entities) and per-entity `*.json` (per_entity layout; single layout removes the whole `entities/` dir). Breadcrumb deletion reuses 18.5's exported `BreadcrumbIsSqlgenOwned`.
- `cmd/sqlgen/cli/root.go` — `cliFlags.manifestTimestamp` + `--manifest-timestamp` registered on the root + `generate` commands.
- `cmd/sqlgen/cli/generate.go` — step 7c: after gen + stale-cleanup + chained GraphQL, call `manifest.RunStage` with the returned contexts + resolved timestamp; `printManifestWarnings` surfaces breadcrumb warnings. `resolveManifestTimestamp` (flag verbatim, else `time.Now().UTC()` RFC3339). Placed **after** gen's `CleanStaleFiles` so `manifest_embed_gen.go` (a `_gen.go` file gen doesn't know about) survives the file_per_table stale sweep.
- `cmd/sqlgen/manifest/emit_test.go` (new) — 8 orchestration tests (see Tests Required).
- `cmd/sqlgen/cli/generate_test.go` — `TestResolveManifestTimestamp`.

Verification:
- `make check` clean across all 8 modules under `-race` (lint 0 issues; `cmd/sqlgen/manifest` ~2.7s, `cmd/sqlgen/gen` ~7.1s, `cmd/sqlgen/cli` ~41.9s).
- `make check-examples` clean across all example modules (Docker up; graphql/tests ~32s, mysql ~11.7s). No example enables manifest, so `RunStage` runs the disabled path (cleanup over non-existent artifacts) → example output byte-unchanged, no golden regen.
- `make test-integration` not re-run — the stage is pure in-memory build + local-FS emission/cleanup; no SQL or driver surface.
- **Security-review:** not fired. Changes touch `cmd/sqlgen/manifest/` + `cmd/sqlgen/gen/` + CLI `generate.go` (no new external-file/URL reader — the ownership check reads only config-derived breadcrumb paths under the output tree), and do **not** touch the top-level `//go:embed` runtime `manifest/` package (18.7's surface). Formal `/security-review` deferred to `/verify 18.6` per the standing pattern.

Deferrals:
- **`breadcrumbs.package_doc` → `models_gen.go` doc-comment pointer → 18.9** (documented in Tasks). Only remaining task item; untested here, touches the shared header path, wants the E2E vehicle.
- **`diff` manifest preview → follow-up.** `sqlgen diff` (which runs `gen.Generate` into a temp dir) does not run the manifest stage, so it does not preview manifest-artifact changes. Non-manifest diff output is unaffected. Left as a documented follow-up (out of this sub-item's Tests Required + acceptance, which are `generate`-scoped).

Notes:
- **`BuildInput.API` is currently unread by the builder** (verified) — wiring `result.API` through is future-proofing + honors the "same in-memory contexts" criterion; it changes no output today.
- **Report totals** (`generated N files`) do not count manifest artifacts (`RunStage` returns warnings, not a file list). Cosmetic; left as-is.
- **Auto-review (sqlgen-reviewer): 8/8 in-scope PRD PASS (9th `package_doc` deferred), 9/9 required tests present, guidelines PASS. One high-confidence finding — addressed in-place before `/done`:** a disabled config with an explicit `markdown_dir: "."` resolves `markdownDir` back to the output dir, so the disabled-path `os.RemoveAll(markdownDir)` would wipe the whole output tree (the disabled-path validator, unlike the enabled path's `checkManifestDirPlacement`, does not reject `markdown_dir: "."`). Fixed with a containment guard (`removeManifestDir`/`within` in `clean.go`) applied to **both** `RemoveAll` sites — it refuses to remove any target that resolves to the base dir itself or escapes it via `..`. New regression test `TestCleanStale_GuardsAgainstOutputDirWipe` (seeds a file in the output dir, runs the disabled path with `markdown_dir: "."`, asserts the file survives). **Superseded by FIX-210:** containment was the only gate this added, and it is not ownership — a correctly-named `<output.dir>/manifest/` that sqlgen never wrote passed it and was removed. The guard now also requires proof of a prior `enabled: true` run. Reviewer's other notes were non-blocking confirmations (embed-file post-sweep ordering correct; `file_per_table` delete-then-recreate churn is cosmetic; emit/clean use symmetric path bases).
- **`/verify 18.6` (2026-07-09):** 8/8 in-scope PRD PASS (`package_doc` deferred to 18.9), all Tests Required present, `make check` + `make check-examples` clean; independent `sqlgen-reviewer` in full agreement, 0 blocking. Two FIXes surfaced by this verify were resolved in-tree before `/done`: **FIX-124** (nested-graph breadcrumbs — `ResolveGraphBreadcrumbDir` now gates on `graphQLEnabled` not `graphTopLevel`; `TestRunStage_NestedGraphBreadcrumbs`) and **FIX-125** (embed-requires-json validation rule — rejects `embed_in_client: true` with a `formats` list lacking `json`; `TestValidatePreParse_Manifest_EmbedRequiresJSON`). No FIX logged from this pass. Security-review: no finding — no new external-file/URL reader; runtime `//go:embed manifest/` untouched. One UNTESTED acceptance criterion (`manifest_embed_gen.go` survives the `file_per_table` stale sweep — an ordering guarantee no unit test can exercise, and no example enables manifest) **tracked to 18.9's Tests Required** rather than logged as a FIX, per the `/verify 18.3` precedent for E2E-vehicle gaps.

---

## 18.7 Runtime-embedded manifest + Go types package

**PRD Reference:** §30.6.
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.7; `docs/design/MANIFEST.md` §5.9 (full struct sketch).

**Module:** New top-level package `manifest/` at repo root; generated file `manifest_embed_gen.go` (template lives in `cmd/sqlgen/gen/templates/`).

**Status:** Complete

**Depends on:** 18.2.

### Tasks

- [x] Create new runtime package `manifest/` at repo root (peer to `database/`, `cache/`, `comparator/`, `omittable/`). Stdlib-only — runtime dependency invariant per PRD §3.
- [x] Export the named types per PRD §30.6 + the 2026-05-15 MCP additions:
  - `Document` (carries `SchemaVersion`, `GeneratedAt`, `Generator`, `Dialect`, `Package`, `Conventions`, `GenerationConfig`, `Layout`, `Entities`, `Enums`, `Extras`).
  - `EntityIndex` (lightweight: `Name`, `Table`, `Kind`, optional `File`).
  - `Entity` (full record).
  - `Column`, `Relationship`, `Method`, `Filter`, `Sort`, `Features`, `PK`, `Conventions`, `Generator`, `Enum`, `Extra`.
  - `GenerationConfig` (added 2026-05-15; 9 boolean toggles per PRD §30.4.3).
  - Typed aliases: `Layout` (string), `Dialect` (string), `EntityKind` (string), `RelationshipKind` (string).
- [x] `Document.GenerationConfig GenerationConfig` field with JSON tag `json:"generation_config"`.
- [x] `Method.SQLBodies map[Dialect]string` field with JSON tag `json:"sql_bodies,omitempty"`.
- [x] Sentinel errors:
  - `manifest.ErrEntityNotFound = errors.New("manifest: entity not found")`
  - `manifest.ErrParseManifest = errors.New("manifest: parse failure (corrupt embed)")`
- [x] Implement `manifest.LoadInto(topJSON []byte, entityFS fs.FS, dst *Document, entityMap map[string]*Entity) error` (signature per this task list, not MANIFEST.md §5.9's `**Document`/`embed.FS` sketch — `*Document`+`fs.FS` is cleaner and the nil-able `fs.FS` is what the single-layout call site passes; §5.9 is superseded design):
  - Parses top-level JSON into `*Document`. `Document.Entities` decodes to the **lightweight** `[]EntityIndex` (per PRD §30.6 "Entities[] is always the lightweight index").
  - Walks `entityFS` if non-nil (per-entity layout) and parses each `<file_prefix>.json` into a full `Entity`, keyed by `Entity.Table` in `entityMap`.
  - Single-layout (`entityFS == nil`): the full records are recovered by a **second decode** of `topJSON` through a `struct{ Entities []Entity }` view (the raw single-layout JSON carries full inline entities even though the typed `Document.Entities` is lightweight) → `entityMap`.
  - Returns `ErrParseManifest` (wrapped, multi-`%w`) on any parse failure.
- [x] Generator side: add template `cmd/sqlgen/gen/templates/manifest_embed.go.tmpl` emitting `manifest_embed_gen.go` in the output package containing:
  - `//go:embed <markdown_dir>/<json_filename>` into a single `embed.FS` (always when `embed_in_client: true`). **Adapted:** embeds into an `embed.FS` and reads via `ReadFile`, not a bare `[]byte` var — a directive-only `[]byte`+`import "embed"` gets its `embed` import stripped by the `goimports` pass in `renderAndWrite`; embedding into `embed.FS` genuinely references the package so the import survives (and would otherwise break real generated output, not just the test).
  - `//go:embed all:<markdown_dir>/<json_per_entity_dir>` stacked onto the same `embed.FS` (only in per-entity layout); the entity subtree is scoped via `fs.Sub` before `LoadInto`.
  - Package-level vars for parsed document + entity lookup map + pre-extracted version.
  - `init()` calling `manifest.LoadInto(...)` once (panics on `ErrParseManifest`).
  - Three `Client` methods: `Manifest() *manifest.Document`, `ManifestEntity(table string) (*manifest.Entity, error)`, `ManifestVersion() string`.
  - **Template file + `ManifestEmbedContext` builder (`context_manifest.go`) + render test land here; the orchestrate `renderAndWrite` call site is 18.6 stage 9** (mirrors 18.3/18.4/18.5 emitters deferring their orchestrate wiring to 18.6). The template auto-registers via the existing `templates/*.go.tmpl` glob in `loadTemplates` — no template-loading change needed in 18.6.
- [x] Gate emission on `cfg.Generation.Manifest.EmbedInClient == true` (default true when manifest enabled). `BuildManifestEmbedContext(cfg, entityCount)` returns `nil` when manifest off / `embed_in_client: false`; the orchestrator skips the render when nil (call site wired in 18.6). **Zero-entity per_entity guard:** `BuildManifestEmbedContext` downgrades a `per_entity`-layout package with `entityCount == 0` to the single-file embed (`PerEntity=false`), since the emitter never creates `manifest/entities/` in that case and a `//go:embed all:` over a missing dir would fail to compile; `LoadInto` then reads the empty index → empty entity map (behaviorally identical). Pinned by `TestBuildManifestEmbedContext_PerEntityZeroEntities`.
- [x] `ManifestEntity` is layout-agnostic — always returns the full record regardless of `single` vs `per_entity` on disk (both paths populate `entityMap` keyed by `Entity.Table`).

### Acceptance Criteria

- The `manifest/` package imports only stdlib (verified via `go list -deps` test).
- `manifest.LoadInto` round-trips a synthetic `Document` (marshal → unmarshal → deep-equal).
- `GenerationConfig` decodes correctly from a fixture manifest with every toggle in both true and false states.
- `Method.SQLBodies` decodes correctly from a fixture manifest carrying one method with multi-dialect bodies.
- Generated `Client.Manifest()` returns the parsed document; safe for concurrent reads (read-only after init).
- Generated `Client.ManifestEntity("users")` returns the full record under both layouts; returns `(nil, manifest.ErrEntityNotFound)` for unknown tables.
- Generated `Client.ManifestVersion()` returns the `schema_version` string pre-extracted at init.
- `ManifestVersion()` agrees with `Document.SchemaVersion` (drift check).
- Binary-size regression: total package growth on a 100-entity fixture is <50KB (raw embedded JSON ~30KB; struct overhead negligible at init).

### Tests Required

- [x] `manifest/document_test.go` — JSON round-trip for each named type (`TestDocument_RoundTrip` over a fully-populated Document, `TestEntity_RoundTrip` over a full Entity), including `GenerationConfig` and `Method.SQLBodies` (`TestMethod_SQLBodiesDecode`).
- [x] `manifest/load_test.go` — `LoadInto` against single-layout fixture (`TestLoadInto_SingleLayout`) and per-entity-layout fixture (`TestLoadInto_PerEntityLayout`). *(In-process `fs.FS` via `testing/fstest.MapFS` — LoadInto takes `fs.FS`, which `embed.FS` and `MapFS` satisfy identically; the real `embed.FS` path is exercised by the 18.9 E2E generated-Client build.)*
- [x] `manifest/load_test.go::TestLoadInto_ErrorWrapping` — corrupt top-level JSON, corrupt inline entity view, and corrupt per-entity file all trigger `ErrParseManifest` (via `errors.Is`).
- [~] Method-signature pin tests: **covered at the template-render level** (`cmd/sqlgen/gen/manifest_embed_test.go` asserts the three exact method signatures + `init` + `LoadInto` call render and pass `goimports`). Reflection-against-a-real-generated-`Client` **deferred to 18.9** (needs an orchestrated + compiled example Client, which arrives with the 18.6 wiring).
- [x] Layout-agnostic `ManifestEntity` lookup: `TestLoadInto_LayoutAgnostic` — same fixture loaded single vs per-entity → deep-equal `entityMap["users"]` (the logic the layout-agnostic Client method delegates to). Real-Client version folds into 18.9.
- [x] Drift check: `TestLoadInto_VersionExtraction` pins `Document.SchemaVersion` (the field the generated `ManifestVersion()` pre-extracts). `ManifestVersion()`-vs-`Manifest().SchemaVersion` on a real Client → 18.9.
- [ ] Binary-size regression: generate a 100-entity fixture, compile a stub binary that imports the generated `Client`, assert delta < 50KB vs a baseline without manifest embedding. **Deferred to 18.9** — requires a compiled generated binary (no orchestrate call site until 18.6).
- [x] `Document.GenerationConfig` decode test: `TestGenerationConfig_Decode` — 18 sub-cases (9 toggles × 2 states each) confirm correct boolean decoding.
- [x] `manifest/deps_test.go::TestManifestPackage_StdlibOnly` — `go list -deps` confirms the runtime package imports only stdlib (PRD §30.6 / §3).
- [x] `cmd/sqlgen/gen/manifest_embed_test.go` — `BuildManifestEmbedContext` gating (4 states) + path derivation (both layouts) + full render-through-`goimports` for single and per-entity layouts.

### Completion Record

**2026-07-09 — landed (runtime package + generator template + render test; three Client-generated tests deferred to 18.9).**

Files changed:
- `manifest/doc.go` (new) — package doc for the runtime-embedded manifest types (stdlib-only, peer to `database/`/`cache/`/`comparator/`/`omittable/`, PRD §30.6 / §3).
- `manifest/types.go` (new) — ~30 named types mirroring the canonical manifest JSON shape (PRD §30.4 / §30.4.1 / §30.4.2 / §30.4.3 / §30.7). Typed aliases `Layout` / `Dialect` / `EntityKind` / `RelationshipKind` (each a `~string` with `String()` + a small constant set); `EntityIndex` (lightweight index form); full `Entity` + all nested types (`Column`, `Relationship`, `Method` with `SQLBodies map[Dialect]string`, `Features` + sub-features, `Index`, `PK`, `Conventions` + sub-types, `Generator`, `GenerationConfig`, `Enum`, `Extra`, …). `Document.Entities` is `[]EntityIndex` (lightweight, per PRD §30.6); `Document` omits the JSON-only `$schema` field (not needed at runtime, per §5.9 field list).
- `manifest/errors.go` (new) — `ErrEntityNotFound` + `ErrParseManifest` sentinels.
- `manifest/load.go` (new) — `LoadInto(topJSON []byte, entityFS fs.FS, dst *Document, entityMap map[string]*Entity) error`. Single layout (nil `entityFS`): second-decodes `topJSON` through a `struct{ Entities []Entity }` view to recover full inline records; per-entity layout: `fs.WalkDir` over `entityFS`, decoding each `*.json` into a full `Entity` keyed by `Entity.Table`. All failures wrapped in `ErrParseManifest` (multi-`%w`).
- `manifest/document_test.go`, `manifest/load_test.go`, `manifest/deps_test.go` (new) — round-trip (Document + Entity), `GenerationConfig` 18-case decode, `SQLBodies` decode, `LoadInto` single/per-entity/layout-agnostic/version/error-wrapping, stdlib-only `go list -deps` check.
- `cmd/sqlgen/gen/templates/manifest_embed.go.tmpl` (new) — the `manifest-embed` defined template. Embeds the top-level JSON (+ `all:` entities subtree in per-entity layout) into one `embed.FS`, `init()` reads + `fs.Sub`-scopes + calls `manifest.LoadInto`, and defines the three `Client` methods. Auto-registers via the existing `templates/*.go.tmpl` glob.
- `cmd/sqlgen/gen/context_manifest.go` (new) — `ManifestEmbedContext` + `BuildManifestEmbedContext(cfg)` (returns nil when manifest off / `embed_in_client: false`; forward-slashed embed paths derived from `markdown_dir` / `json_filename` / `json_per_entity_dir`).
- `cmd/sqlgen/gen/manifest_embed_test.go` (new) — gating (4 states), path derivation (both layouts), and full render-through-`goimports` for single + per-entity layouts (pins the three method signatures, `init`, `LoadInto` call, embed directives, and the `embed`-import survival).

Verification:
- `make check` clean across all 8 modules (lint 0 issues; `manifest` ~2.3s, `cmd/sqlgen/gen` ~6.4s).
- `make check-examples` — triggered mechanically by the `manifest/**` + `cmd/sqlgen/gen/**` path globs. Clean: the template is registered but never rendered (no `renderAndWrite` call until 18.6 wiring; no example enables manifest), so example output is byte-unchanged — no golden regeneration.
- `make test-integration` not re-run — runtime package is pure in-memory JSON/`fs` decoding, no SQL/runtime-driver surface.

Design decisions (flagged for `/verify`):
- **Import path `github.com/teandresmith/sqlgen/manifest`** — the PRD §30.6 / MANIFEST.md §5.9 examples say `github.com/teandresmith/sqlgen`, but `go.mod` is `github.com/teandresmith/sqlgen` (the same stale-owner drift the 18.3 record already corrected in `builder.go:schemaURL`). Used the real module path.
- **`GeneratedAt string`** (not §5.9's non-normative `time.Time` sketch) — mirrors the builder-side `cmd/sqlgen/manifest` types + the on-disk RFC3339 string, keeps the two type sets wire-identical, and avoids `cmp.Diff`-on-`time.Time` unexported-field friction in the round-trip test. PRD §30.6 normative text does not pin the Go type.
- **`LoadInto` signature** per this task list (`*Document` + `fs.FS`), not §5.9's `**Document` + `embed.FS`. `fs.FS` is what lets the single-layout call site pass `nil`; the template controls the only real call site so the two stay consistent. MANIFEST.md §5.9 is superseded design.
- **`embed.FS` (not bare `[]byte`) for the JSON** — see the task note: a directive-only `[]byte` loses its `embed` import to the `renderAndWrite` `goimports` pass, which would break real generated output. Embedding into `embed.FS` references the package genuinely.
- **Type names `Filter` / `Sort`** match PRD §30.6's type list (post-`/verify` rename from the initial `FilterStruct` / `SortStruct`, which was un-idiomatic Go). Both the runtime `manifest/` and builder-side `cmd/sqlgen/manifest/` packages were renamed together (incl. the `buildFilterStruct`→`buildFilter` / `buildSortStruct`→`buildSort` helpers) so the two type sets stay identical; the JSON shape (`filter` / `sort` fields) is unchanged.

Deferred to 18.9 (all need an orchestrated + compiled example `Client`, which arrives once 18.6 wires the `renderAndWrite` call — same shape as 18.3's real-`Build`→emit deferral):
- Method-signature pin via reflection against a real generated `Client` (covered at the template-render level here).
- Layout-agnostic `Client.ManifestEntity` on a real Client (covered at the `LoadInto` layer here).
- `ManifestVersion()`-vs-`Manifest().SchemaVersion` drift on a real Client (covered at the load layer here).
- Binary-size regression (<50KB delta) — needs a compiled generated binary.

Deferred to 18.6 (the orchestrate wiring sub-item, which already depends on 18.7):
- The `renderAndWrite(tmpl, "manifest-embed", …)` call site + `EmbedInClient` gate (stage 9) + the `manifest_embed_gen.go` stale-cleanup on `embed_in_client: false`.

Security-review trigger: the new top-level `manifest/` package is shipped via `//go:embed` to every consumer, so the supply-chain trigger fires. It is a stdlib-only decoder of build-time-embedded (sqlgen-generated, not runtime-attacker-controlled) JSON — no filesystem writes, no network, no `exec` in non-test code. Formal `/security-review` **deferred to `/verify 18.7`** (per the `/implement` option to defer), where the reviewer pass covers the security rules.

Notes / follow-up:
- **Zero-entity per_entity compile-trap closed in-code** (auto-review edge finding). `BuildManifestEmbedContext(cfg, entityCount)` takes the emitted entity count and downgrades a `per_entity` package with zero entities to the single-file embed — otherwise the generated `//go:embed all:manifest/entities` would fail to compile against the never-created directory. Guarded + tested here (`TestBuildManifestEmbedContext_PerEntityZeroEntities`); 18.6's call site only threads `len(doc.Entities)`.
- The runtime `manifest/` (root module) and the builder-side `cmd/sqlgen/manifest/` are two distinct packages by design: the builder-side `Document.Entities` is `[]Entity` (full, marshaled into single-layout JSON); the runtime `Document.Entities` is `[]EntityIndex` (lightweight, per PRD §30.6). The asymmetry is intentional — full records reach the runtime via `entityMap`, not `Document.Entities`.
- Per-entity lookup keys on `Entity.Table`; a multi-schema same-table-name collision would last-write-win. No example/test exercises that today; flagged for a follow-up FIX if it surfaces (matches the 18.2 relationship-kind heuristic caveat style).
- Auto-review (sqlgen-reviewer subagent) summary: see runner output.

**`/verify 18.7` (2026-07-09):** 10/10 PRD requirements PASS, all in-scope required tests present, `make check` + `make check-examples` clean, independent reviewer in full agreement, 0 blocking. Load path confirmed security-clean (stdlib-only in-memory JSON decode of build-time-trusted bytes — satisfies the deferred `/security-review` trigger). One PARTIAL (type names `FilterStruct`/`SortStruct` vs PRD §30.6's `Filter`/`Sort`) resolved in-place before `/done` rather than logged as a FIX — see below. One non-blocking test-fidelity nit (runtime `load_test.go` never walks a genuinely `fs.Sub`-rooted FS with a stray sibling top-level `.json`) folded into **18.9** as `TestLoadInto_SubRootedFS` (task + Tests-Required line).

**Post-verify rename (2026-07-09):** `FilterStruct` → `Filter` and `SortStruct` → `Sort` across **both** the runtime `manifest/` and builder-side `cmd/sqlgen/manifest/` packages (kept in lockstep so the two type sets stay identical), incl. the builder helpers `buildFilterStruct` → `buildFilter` / `buildSortStruct` → `buildSort`. Field/type-name coincidence (`Filter Filter`, `Sort Sort`) is legal Go; the `sort` stdlib package coexists with the `Sort` type (case-distinct). JSON tags (`filter` / `sort`) unchanged → no manifest output / golden churn. `go build` + `go vet` + `go test` + `golangci-lint` clean on both packages. This closes the sole `/verify` PARTIAL — the exported type list now matches PRD §30.6 exactly, so 18.7 is fully PASS.

**`/done 18.7` (2026-07-09).**

---

## 18.8 CLI subcommands (`sqlgen manifest validate` + `diff`)

**PRD Reference:** §30.8, §23.1 (CLI command index).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.8

**Module:** `cmd/sqlgen/cli/cmd_manifest.go`, `cmd/sqlgen/cli/cmd_manifest_validate.go`, `cmd/sqlgen/cli/cmd_manifest_diff.go`

**Status:** Complete

**Depends on:** 18.3.

### Tasks

- [x] Add new `manifest` parent command in `cmd/sqlgen/cli/cmd_manifest.go` with two subcommands.
- [x] `sqlgen manifest validate <path>`:
  - Load the file at `<path>`.
  - Read its `$schema` URL; fetch the schema if reachable. Fallback to embedded `cmd/sqlgen/manifest/schema/v1.json` when offline or URL unreachable.
  - Validate the manifest against the schema using `github.com/santhosh-tekuri/jsonschema/v5` (promoted from test-only to CLI dep).
  - Print summary: `schema_version`, entity / enum / extra counts.
  - Exit codes: 0 success, 1 schema validation failure, 2 I/O failure.
- [x] `sqlgen manifest diff <old> <new>`:
  - Load both manifests.
  - Walk entities / columns / methods / sentinels / features computing additions, removals, and field changes.
  - Default output: human-readable structured diff (one section per dimension).
  - `--json` flag: machine-readable JSON shape for CI scripts.
  - Honor both layouts transparently (resolve `entities[].file` indirection when per-entity).
  - Exit code 0 if no diff, 1 if any diff.
- [x] Promote `github.com/santhosh-tekuri/jsonschema/v5` from test-only to CLI dep in `cmd/sqlgen/go.mod`.
- [x] README / CLI-docs update covering the two commands + exit code semantics + `--json` output schema.

### Acceptance Criteria

- `sqlgen manifest validate <valid.json>` exits 0 and prints the summary line.
- `sqlgen manifest validate <invalid.json>` exits 1 and prints a structured error citing the offending JSON path.
- `sqlgen manifest validate <missing.json>` exits 2 with a clear I/O error.
- `sqlgen manifest validate <malformed.json>` exits 1 (parse failure) with a clear error.
- `sqlgen manifest diff <same.json> <same.json>` exits 0 with no diff output.
- `sqlgen manifest diff <old.json> <new.json>` exits 1 and prints structured human-readable diff covering: column added / removed / type-changed, entity added / removed, method signature change, sentinel change, feature toggle change.
- `sqlgen manifest diff --json` produces machine-readable JSON conforming to a documented schema.
- Per-entity-layout diffs work: the resolver follows `entities[].file` pointers to load full entity records before diffing.

### Tests Required

- [x] `cmd/sqlgen/cli/cmd_manifest_validate_test.go`:
  - Valid manifest → exit 0 + summary.
  - Invalid manifest (missing `generation_config`) → exit 1.
  - Missing file → exit 2.
  - Malformed JSON → exit 1.
  - Offline fallback: `$schema` URL unreachable → falls back to embedded schema.
- [x] `cmd/sqlgen/cli/cmd_manifest_diff_test.go`:
  - Column added (existing entity, new column) → diff entry.
  - Column removed → diff entry.
  - Column type changed (e.g. `int` → `bigint`) → diff entry.
  - Entity added / removed → diff entry.
  - Method signature change → diff entry.
  - Sentinel added / removed → diff entry.
  - Feature toggle change (`features.cache.ttl_seconds` updated) → diff entry.
  - `--json` schema test: pin the output structure.
  - Same-file diff → exit 0, no output.
  - Per-entity-layout diff → file-pointer resolution works.

### Completion Record

**2026-07-09 — landed.**

Files changed:
- `cmd/sqlgen/manifest/schema.go` (new) — `//go:embed schema/v1.json` → exported `SchemaV1() []byte`, the offline-fallback schema for `manifest validate`. Lives in the builder-side package (only package that can `//go:embed` the schema dir) and is imported by the CLI.
- `cmd/sqlgen/cli/cmd_manifest.go` (new) — `newManifestCmd` parent + the three manifest exit-code constants (`manifestExitClean` 0 / `manifestExitFail` 1 / `manifestExitIOFail` 2, distinct from root.go's generation codes per PRD §30.8's git-style convention).
- `cmd/sqlgen/cli/cmd_manifest_validate.go` (new) — `validate <path>`: read → parse → resolve schema (fetch `$schema` URL, fall back to embedded) → `jsonschema` validate → summary. Exit 0/1/2. Network fetch (`httpFetchSchema`) is scheme-allowlisted (http/https only), 10s-timeout-bounded, and size-capped (4 MiB `io.LimitReader`); injectable via the `manifestSchemaFetcher` package var for hermetic tests.
- `cmd/sqlgen/cli/cmd_manifest_diff.go` (new) — `diff <old> <new>` [`--json`]: layout-aware loader (follows `entities[].file` pointers in per_entity, re-reads inline `entities[]` in single) → structured diff over entities / columns / methods / sentinels / features / generation_config. Human output (one section per dimension) or deterministic `--json` (all slices sorted, HTML-escaping off). Exit 0 (identical) / 1 (differ). Uniform `setDiff{added,removed,changed}` + `kvChange{key,old,new}` model keeps the engine small.
- `cmd/sqlgen/cli/root.go` — `rootCmd.AddCommand(newManifestCmd(flags))`.
- `cmd/sqlgen/go.mod` — promoted `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1` from `// indirect` to a direct require (now a CLI dep, not just test-only).
- `cmd/sqlgen/cli/testdata/manifest/valid_single.json` (new) — schema-valid single-layout fixture (copy of the 18.3 golden) for the validate tests.
- `cmd/sqlgen/cli/cmd_manifest_validate_test.go` (new) — 7 tests: valid+summary, fetch-used-when-reachable, missing-`generation_config`→1, missing-file→2, malformed→1, offline-fallback→0, junk-fetched-schema→embedded-fallback. Fetcher stubbed in every test (no network).
- `cmd/sqlgen/cli/cmd_manifest_diff_test.go` (new) — 11 tests: same-file→0/no-output, column added/removed/type-changed, entity added+removed, method-signature change, sentinel change, per-entity `cache.ttl_seconds` change, generation_config toggle change, `--json` shape (round-trips into `manifestDiff`), per_entity layout file-pointer resolution, per_entity `../` traversal rejected. Manifests built as full-inline Go structs, mutated per case.
- `README.md` — new "Manifest tooling" section: both commands, exit-code table, `--json` diff shape.

Verification:
- `make check` clean across all 8 modules under `-race` (lint 0 issues; `cmd/sqlgen/cli` ~43s). All 18 new tests green.
- `make check-examples` clean across all example modules (Docker up; graphql/tests ~34s, mysql ~10s) — triggered mechanically by the `cmd/sqlgen/manifest/**` (schema.go) + `go.mod` path globs. No example enables manifest and the new subcommands never run during example generation, so example output is byte-unchanged (no golden regen).
- `make test-integration` not re-run — the subcommands are pure on-disk JSON tooling, no SQL/runtime surface.

Notes / design decisions:
- **Exit codes are manifest-local.** PRD §30.8 specifies validate 0/1/2 and diff 0/1 with git-style semantics, distinct from root.go's `ExitConfig`/`ExitSchema`. Modeled via the existing `exitError{code}` mechanism with dedicated constants. Diff-found returns a non-printed sentinel (`errManifestDiff`) carrying code 1; `SilenceErrors` keeps it off stderr.
- **Malformed vs missing.** File read error → exit 2 (I/O); JSON parse error → exit 1 (per the acceptance split); schema-validation failure → exit 1.
- **Diff uses the runtime `manifest` types** (`github.com/teandresmith/sqlgen/manifest`, imported the same way `cli` already imports `sql`) — the canonical decode target, layout-agnostic. Column "type changed" compares (go_type, db_type, nullable); method "signature change" compares a rendered `name(params) returns` string.
- **Security surface (trigger fired).** The new `validate` subcommand is a `cmd/sqlgen/cli/**` command that reads a user-supplied file path and fetches an external URL (`$schema`) — both listed security surfaces. Mitigations in place: scheme allowlist, bounded timeout, response-size cap, and total fallback-to-embedded on any fetch failure (so a hostile/unreachable URL degrades gracefully, never blocks). File reads carry `//nolint:gosec` with a by-design reason. **Formal `/security-review` deferred to `/verify 18.8`.**
- **Auto-review (sqlgen-reviewer):** 7/7 PRD PASS, 16/16 required tests present, 0 blocking. Three security/correctness notes, all PRD-inherent or low-impact. **Two addressed in-place before `/done`:** (1) `loadPerEntityForDiff` followed `entities[].file` pointers with no containment check — added `withinDir(base, target)` guard (rejects `../` traversal) + `TestManifestDiff_PerEntityTraversalRejected` (exit 2), and tightened the `//nolint:gosec` reason to match. (2) A reachable `$schema` URL returning a non-compilable body previously failed the gate (exit 1) instead of degrading — `resolveSchema` now falls back to the embedded schema on fetch **or compile** failure (`fetchAndCompileSchema` split out) + `TestManifestValidate_JunkFetchedSchemaFallsBack`. Test count is now **18** (7 validate + 11 diff). **One note deferred to `/verify 18.8` (candidate `tracked` FIX):** broader `$schema`-fetch SSRF hardening (no private-IP/link-local block, `http.DefaultClient` follows redirects) — genuinely PRD-inherent (§30.8 mandates validating against the `$schema` URL) and a threat-model design decision, so left for the formal `/security-review` that `/verify` runs. Reviewer also noted (non-blocking) that `diff` intentionally covers only entities/columns/methods/sentinels/features/generation_config per §30.8 — PK/relationship/index/enum/extra changes are out of scope by spec.
- **`/verify 18.8` (2026-07-09):** 8/8 stated PRD acceptance criteria PASS, 19/19 required tests present, `make check` + `make check-examples` clean; independent reviewer in full agreement, 0 blocking. One tracked coverage gap logged + resolved before `/done`: **FIX-126** — `manifest diff` under-detected column/method *changes* (`columnSig` compared only Go/DB type + nullability; `methodSig` omitted `errors[]`), so a PK/UNIQUE/default flip or an error-set change on an otherwise-unchanged column/method surfaced as no diff — a blind spot for a §30.8 "schema changes are intentional and reviewed" gate. Resolved via `/fix-implement FIX-126` (2026-07-09): widened both signatures (sorted `errors=[…]` so reorder is not a spurious diff) + 3 regression tests; `make check` clean, change confined to `cmd/sqlgen/cli/**` (no golden/example impact). Reviewer's SSRF residual on `validate`'s `$schema` fetch (redirects followed, no private-IP block) confirmed **PRD-inherent + low-severity** (the `$schema` is a builder-set constant in generated manifests; the fetched body is used only as a schema, never returned) — left for the formal phase-18 `/security-review`, no FIX.

---

## 18.9 E2E example surface across all dialects

**PRD Reference:** §30 (all subsections).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.9

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/`

**Status:** Complete

**Depends on:** 18.1–18.8.

### Tasks

- [x] Extend each example's `sqlgen.yml` with a `generation.manifest:` block:
  - `postgres/sqlgen.yml`: `manifest.enabled: true`, `json_layout: single` (default).
  - `mysql/sqlgen.yml`: `manifest.enabled: true`, `json_layout: single`.
  - `sqlite/sqlgen.yml`: `manifest.enabled: true`, `json_layout: single`, **`output.layout: file_per_table`** (satisfies the 18.6 stale-sweep guard below).
  - `graphql/sqlgen.yml`: `manifest.enabled: true`, `json_layout: per_entity` (exercises the per-entity code path + the layered-mode breadcrumb variant).
- [x] Fixture data exercising extended metadata in each example:
  - `COMMENT ON TABLE` / `COMMENT ON COLUMN`: postgres + mysql schemas already carry these; **graphql** gets `COMMENT ON` on `categories` (+ columns) — closing the untested §26.4 `.graphqls`-description-wiring path E2E (the comment now flows to the manifest, Go doc comments, AND `graph/category_gen.graphqls`). SQLite has no comment syntax the parser retains (`--` line comments are not attached), so it carries none.
  - Composite (non-unique) indexes: all four examples (`idx_order_items_order_product`).
  - Partial indexes where supported: postgres + sqlite (`idx_articles_active ... WHERE deleted_at IS NULL`, `where` field round-trips); not mysql (no partial-index grammar).
  - **CHECK constraints: landed via FIX-127 (resolved 2026-07-09).** Originally deferred: confirmed empirically that CHECK did not surface in the manifest end-to-end in ANY dialect — the parser never populates `parser.Constraint.Columns` for CHECK-typed constraints, so the builder's `checkExpressionForColumn` flatten never matched (masked because 18.2's builder tests hand-set `Columns`). FIX-127 resolved this builder-side (`checkConstraintColumns` derives the participating columns from the CHECK expression text when `Columns` is empty) and re-added table-level `CHECK (unit_price >= 0 AND quantity > 0)` fixtures on **postgres + mysql + sqlite** `order_items`; goldens regenerated (`check` flattens onto `quantity` + `unit_price`). The three dialects together exercise all tokenizer paths: bare identifiers (pg), vitess-lowercased `and` (mysql), and rqlite double-quoted identifiers (sqlite). See `docs/tracker/fixes.md` (Resolved → FIX-127).
- [x] Regenerate golden trees per example:
  - `expected/manifest/manifest_gen.json` (every example).
  - `expected/manifest/_index.md`, `expected/manifest/_conventions.md` (every example).
  - `expected/manifest/<table>.md` (per entity, every example).
  - `expected/manifest/entities/<table>.json` (graphql example only — per-entity layout).
  - `expected/CLAUDE.md`, `expected/AGENTS.md` (every example).
  - `expected/manifest_embed_gen.go` (every example).
  - `expected/graph/CLAUDE.md`, `expected/graph/AGENTS.md` (graphql example only — layered-mode variant).
- [x] Add `tests/manifest_test.go` per example covering the runtime/Client sub-tests:
  - File-on-disk shape: read `manifest_gen.json`, verify top-level fields present + entity count matches the embedded Client view (`TestManifest_OnDiskShape`).
  - Runtime `Client.Manifest()` round-trip: envelope + entity index match the on-disk file (`TestManifest_ClientRoundTrip`).
  - `ManifestEntity` lookup: `Client.ManifestEntity("users")` returns the full record under both layouts; unknown → `ErrEntityNotFound` (`TestManifest_EntityLookup`).
  - `ManifestVersion` agreement: `Client.ManifestVersion() == Client.Manifest().SchemaVersion` (`TestManifest_VersionAgreement`).
- [x] JSON Schema validation + `sqlgen manifest validate` CLI, over REAL orchestrated output. **Placement deviation:** these two sub-tests live in `cmd/sqlgen/cli/manifest_validate_e2e_test.go` (`TestManifestValidate_E2EExamples`), NOT the example `tests/` dirs. The example modules import only the root module — the embedded `schema/v1.json` and the `jsonschema/v5` library the CLI uses live in `cmd/sqlgen`, unreachable across the module boundary. The `cli` test drives the real `manifest validate` command (same jsonschema lib + embedded schema, fetcher forced offline for hermeticity) over every manifest-enabled example's committed `manifest_gen.json` (+ graphql `entities/*.json`). **This is the first schema check against real orchestrated `Build → EmitJSON` output and it immediately caught a builder-side regression — view entities emitted `indexes: null` (nil slice) vs the schema's array type (FIX-128, the exact FIX-068/FIX-059 divergence shape). Fixed in-scope.**
- [x] Close the `fs.Sub` test-fidelity gap in `manifest/load_test.go` — `TestLoadInto_SubRootedFS` builds a `fstest.MapFS` with a top-level `manifest/manifest_gen.json` sibling alongside `manifest/entities/*.json`, applies `fs.Sub(…, "manifest/entities")` (mirroring the generated `init()`), and asserts only the two real entities load — no phantom `entityMap[""]` from the stray top-level file.

### Acceptance Criteria

- All four examples generate clean against the new `manifest:` config block.
- The graphql example emits per-entity-layout artifacts under `manifest/entities/` AND the layered-mode `graph/CLAUDE.md` + `graph/AGENTS.md` pair.
- Golden trees match byte-for-byte under `TestE2EGoldenFiles`.
- `tests/manifest_test.go` passes under `-race` in all four examples.
- `make test-integration` passes including the new manifest tests across PG + MySQL + SQLite testcontainers.
- `--manifest-timestamp` flag pins `generated_at` for golden-test determinism — verify the flag works in the `generate` CLI.
- The real orchestrated `Build → EmitJSON` output (every example) validates against `schema/v1.json` — closing the 18.3 coverage gap where only a hand-built fixture Document was schema-checked (no `Build()` output ever reached the validator). A `nil`-vs-`[]` slice, unlisted enum, or renamed builder field must fail this check, not slip through.

### Tests Required

- [x] `cmd/sqlgen/testdata/examples/postgres/tests/manifest_test.go` — the four runtime/Client sub-tests.
- [x] `cmd/sqlgen/testdata/examples/mysql/tests/manifest_test.go` — same.
- [x] `cmd/sqlgen/testdata/examples/sqlite/tests/manifest_test.go` — same (no testcontainer; in-memory SQLite; `file_per_table` layout).
- [x] `cmd/sqlgen/testdata/examples/graphql/tests/manifest_test.go` — same, `per_entity` layout (`ManifestEntity` resolves from the embedded `entities/` FS); per-entity + layered-breadcrumb artifacts pinned by `TestE2EGoldenFiles`.
- [x] Real-`Build()`-output schema validation (every example) — `cli.TestManifestValidate_E2EExamples` loads the on-disk `manifest_gen.json` (and, for graphql, each `entities/<table>.json`) produced by the orchestrated pipeline and validates it against the embedded `schema/v1.json` via the real `manifest validate` command. Asserts on real generator output, not a hand-built `Document` — the guard the 18.3 emitter tests could not provide (parent `/verify 18.3`). **Moved to `cmd/sqlgen/cli` (not the example `tests/`) for module-boundary reasons — see the task note above. Caught FIX-128 on first run.**
- [x] `manifest/load_test.go::TestLoadInto_SubRootedFS` — walks an `fs.Sub`-rooted FS containing a sibling top-level `.json` next to the `entities/` subtree; asserts the top-level file is not decoded into `entityMap` (closes the `fs.Sub` fidelity gap flagged in `/verify 18.7`).
- [x] **`manifest_embed_gen.go` survives the `file_per_table` stale sweep** — the sqlite example uses `output.layout: file_per_table` + `manifest.enabled: true` (+ default `embed_in_client: true`); `expected/manifest_embed_gen.go` is present in the regenerated golden tree and `TestE2EGoldenFiles` + `check-examples` (compile) pass, proving the embed file is not swept. Surfacing `file_per_table` end-to-end also exposed FIX-129 (per-file emission shipped incomplete imports → non-compiling output); fixed in-scope.
- [x] `TestE2EGoldenFiles` continues to pass across every example after golden regeneration.

### Completion Record

**2026-07-09 — landed.**

Files changed (source):
- `cmd/sqlgen/e2e_test.go` — threads a fixed `--manifest-timestamp` (`goldenManifestTimestamp = 2026-01-01T00:00:00Z`) through both `runGenerate` (graphql in-place) and `runGenerateToDir` (temp-dir) so manifest `generated_at` is byte-deterministic across golden runs.
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/sqlgen.yml` — `generation.manifest` blocks (pg/mysql/sqlite `single`, graphql `per_entity`); sqlite also flips `output.layout: file_per_table`.
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/schema.sql` — composite (all four) + partial (pg/sqlite) index fixtures; graphql `COMMENT ON categories` (+ columns) to exercise the `.graphqls` description wiring E2E. **CHECK fixtures on pg/mysql/sqlite `order_items` added later via FIX-127** (`CONSTRAINT order_items_amounts_positive CHECK (unit_price >= 0 AND quantity > 0)`).
- `cmd/sqlgen/manifest/builder.go` — **FIX-128**: `buildViewEntity` emits `Indexes: []Index{}` (was `nil`) so view `indexes` serialize as `[]` not `null` (schema-valid). **FIX-127**: `checkConstraintColumns` derives CHECK participating columns from the expression text when `parser.Constraint.Columns` is empty (all dialect parsers leave it empty for checks) — surfaces per-column `check` metadata E2E.
- `cmd/sqlgen/gen/format.go` + `orchestrate.go` — **FIX-129**: `resolvePackageImports` resolves the package's complete import set once (single goimports scan), and `generateTablesPerFile` / `generateViewsPerFile` emit each file seeded with that complete set + a normal `Format` — which finds nothing missing (so no per-file module scan) and prunes each file's unused imports (fast, correct). Fixes `file_per_table` shipping non-compiling output (missing back-filled imports).

Files added (tests):
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/manifest_test.go` — 4 runtime/Client sub-tests each.
- `cmd/sqlgen/cli/manifest_validate_e2e_test.go` — `TestManifestValidate_E2EExamples` (real-output schema + CLI validate, hermetic).
- `cmd/sqlgen/gen/format_imports_internal_test.go` — `TestResolvePackageImports_BackfillsAndPrunes` (FIX-129 unit guard).
- `manifest/load_test.go` — `TestLoadInto_SubRootedFS`.

Golden trees regenerated for all four manifest-enabled examples (manifest JSON/MD, breadcrumbs, `manifest_embed_gen.go`, graphql `entities/` + `graph/` breadcrumbs, sqlite per-table `*_gen.go` split).

Verification:
- `make check` clean across all modules under `-race`.
- `make check-examples` clean (Docker) — all example test modules green incl. the 4 new `manifest_test.go` + sqlite `file_per_table` compile; graphql 31.8s, mysql 10.2s, postgres 5.3s, sqlite 2.2s.
- `TestManifestValidate_E2EExamples` validates real orchestrated output for all four examples against the embedded schema.

Notes / deviations:
- **Schema-validation + CLI-validate sub-tests placed in `cmd/sqlgen/cli`, not the example `tests/`** (module boundary — the embedded schema + `jsonschema/v5` live in `cmd/sqlgen`, unreachable from the root-module-only example modules). The example `tests/` carry the four runtime/Client sub-tests. Net six-sub-test coverage preserved; split by dependency locality.
- **CHECK fixtures landed via FIX-127 (resolved 2026-07-09)** — originally deferred here because CHECK did not surface E2E in any dialect (parser never populates `Constraint.Columns` for checks). FIX-127 fixed the builder-side derivation and re-added table-level CHECK fixtures on pg + mysql + sqlite `order_items` (goldens regenerated; `check` flattens onto `quantity` + `unit_price`). Composite/partial indexes + comments + CHECK now cover the full extended-metadata surface (§30.7).
- **Two blocking bugs found + fixed in-scope**, both surfaced by 18.9's new guards and gating its own acceptance: FIX-128 (view `indexes: null`, caught by the real-output schema check — the FIX-068/FIX-059 divergence shape) and FIX-129 (`file_per_table` non-compiling output, caught by flipping sqlite to that layout — no example had ever used it). Per-user direction the FIX-129 fix keeps `FormatOnly`-level per-file speed via one-scan-then-prune rather than goimports-per-file.
- `--manifest-timestamp` flag verified working end-to-end (pins `generated_at`; goldens deterministic).
- Auto-review (sqlgen-reviewer subagent): see runner output.

**`/verify 18.9` (2026-07-09):** 6/6 PRD requirements PASS (5 core 18.9 acceptance criteria + CHECK-via-FIX-127 §30.7), 9/9 required tests present, `make check` + `make check-examples` clean (Docker), `TestManifestValidate_E2EExamples` green across all four examples. Independent `sqlgen-reviewer` in full agreement, 0 blocking — confirmed the three focus areas: FIX-129's one-scan-then-prune back-fills every per-file import (union runs over the concatenation of all table bodies, so a single-table-only package is still present; per-file `Format` can only prune, never drop a used import); a scripted scan of all real entities found zero nil-serialized required arrays (FIX-128 holds; `: null` hits are all nullable object pointers, schema-valid); determinism clear (all map ranges emit sorted, all slices `sort.SliceStable`/`sort.Strings`). 0 FIXes logged. Bookkeeping: the CHECK-deferred references in this record were updated to point at FIX-127's resolution.

**`/done 18.9` (2026-07-09).** All tasks + Tests Required checked; Status → Complete. Phase 18 now 9/10 (18.10 closure sweep remaining).

---

## 18.10 Phase closure sweep + PRD sync

**PRD Reference:** §30 closure + §30.5 (schema v1.0.0 freeze).
**Design Reference:** `IMPLEMENTATION_ORDER.md` §18.10

**Module:** Documentation + repo-wide test sweep.

**Status:** Complete (2026-07-09 — JSON Schema v1.0.0 freeze pair deferred by user direction; see Completion Record)

**Depends on:** 18.1–18.9.

### Tasks

- [x] Run `make check` across all 8 modules under `-race`. Record timings + any flakes.
- [x] Run `make check-examples` across all 8 example modules. Record timings.
- [x] Run `make test-integration` (full sweep, no `-short`) including testcontainer-backed PG / MySQL / Redis suites. Record timings + any flakes.
- [x] Confirm 18.1–18.9 Completion Records are filled in.
- [x] Update `docs/tracker/STATUS.md` Phase 18 row to `10 | 10 | Complete`. Add Current Focus entry summarizing the surface delta (new `manifest:` config block, new runtime `manifest/` package with ~27 types, new `cmd/sqlgen/manifest/` builder + emitter, two new CLI subcommands, JSON Schema v1.0.0 frozen, GraphQL `.graphqls` descriptions wired through `Table.Comment` / `Column.Comment`).
- [x] Confirm `docs/design/MANIFEST.md` Status header reflects "SYNCED — superseded by PRD §30" with the closure date. _(Already SYNCED 2026-05-15 — accurate sync-execution date; left as-is rather than restamping today.)_
- [ ] **Tighten `schema/v1.json` before the freeze:** _(Deferred with the v1.0.0 freeze — see Completion Record.)_ the always-emitted `conventions` sub-blocks `call_options` / `soft_delete` / `comparator` / `omittable` are currently neither in the `conventions` `required` list nor shape-validated (only `client_entry_points` / `error_sentinels` / `find_returns_nil_on_missing` / `pagination` are). `additionalProperties: true` keeps output valid, but a dropped/renamed sub-block would pass validation silently. Add the four to `required` and give each a minimal property shape so the v1.0.0 contract pins the full conventions surface. (Surfaced by `/verify 18.3`, 2026-07-09 — reviewer-confirmed non-blocking coverage gap; the emitter always writes all eight sub-blocks per PRD §30.4.1.)
- [ ] Tag `cmd/sqlgen/manifest/schema/v1.json` as `schema_version: 1.0.0` — freeze the v1 contract. Future breaking changes go to `schema/v2.json` per PRD §30.5. _(Deferred by user direction — `schema_version` held at `0.1.0`; see Completion Record.)_
- [x] `/fix` triage: any pre-existing bug uncovered during 18.x test bring-up gets a FIX entry, not in-place scope expansion. Exception logged: the GraphQL description-wiring fix bundled into 18.2 (same data path; MANIFEST.md §11 Resolved).

### Acceptance Criteria

- `make check` + `make check-examples` + `make test-integration` all pass clean under `-race`.
- All 10 Phase 18 sub-items have filled Completion Records.
- STATUS.md row reflects `10 | 10 | Complete`.
- `docs/design/MANIFEST.md` Status header updated.
- Manifest JSON Schema v1.0.0 frozen at `cmd/sqlgen/manifest/schema/v1.json`.
- Any pre-existing bugs surfaced during phase bring-up are filed as `/fix` entries with FIX-NNN IDs, not expanded into the phase scope.

### Tests Required

- [x] Full `make check` sweep clean.
- [x] Full `make check-examples` sweep clean.
- [x] Full `make test-integration` sweep clean (testcontainers up).
- [x] No new test failures introduced; no flake patterns in the 3-run sweep.

### Completion Record

**2026-07-09 — Phase 18 closed via `/close-phase 18`.**

Test sweep (all clean, `-race`, no flakes):
- `make check` — clean across all 8 modules (lint 0 issues, vet clean, `-short -race` unit tests green; runtime/parser unit legs ~1.2–2.2s each; cmd/sqlgen subpackages green).
- `make check-examples` — clean across all example modules under `-race` (longest legs: graphql/tests ~35.0s, mysql/tests ~10.1s, postgres/tests ~4.9s, postgres_stdlib ~3.0s, sqlite ~2.2s).
- `make test-integration` — clean, testcontainers (PG/MySQL/Redis) up (longest legs: cmd/sqlgen/cli ~393s, cmd/sqlgen E2E ~181s, gen ~31s, parser/introspect ~9.3s, database/stdlib ~8.6s, cache/redis ~5.9s; ~7:09 wall total). Zero FAIL/panic.

FIX triage: 0 open at closure. The full Phase 18 FIX batch (FIX-113…FIX-129) is Resolved — including the carried-over FIX-115/116/117/118/119/120 that earlier gated this closure, and the 18.9-surfaced blocking FIX-127/128/129. 0 carried forward; 0 must-resolve outstanding.

Trackers updated:
- `docs/tracker/STATUS.md` — Phase 18 Overview row → `10 | 10 | Complete`; new closure Current Focus entry (per-sub-item summary + sweep results + surface delta + freeze deferral).
- `docs/tracker/phase-18.md` — this record; task/Tests-Required checkboxes ticked (freeze pair left unchecked, deferred).
- `docs/design/MANIFEST.md` — already SYNCED (2026-05-15, superseded by PRD §30); accurate as-is, not restamped.
- `docs/tracker/IMPLEMENTATION_ORDER.md` — unchanged (no per-phase in-progress marker).

Notes / deferrals:
- **JSON Schema v1.0.0 freeze deferred by user direction (2026-07-09).** `schema_version` is held at `0.1.0`. The two pre-freeze tasks are coupled and carried forward together to run when the version bump is greenlit: (1) tighten `cmd/sqlgen/manifest/schema/v1.json` — add the always-emitted `conventions` sub-blocks `call_options` / `soft_delete` / `comparator` / `omittable` to `conventions.required` and give each a minimal property shape (surfaced by `/verify 18.3`; emitter already writes all eight per §30.4.1, so this is a pure contract-tightening with no output change); (2) tag `schema_version: 1.0.0` in the shared `SchemaVersion` const (`cmd/sqlgen/manifest/types.go`) + runtime `manifest/types.go`, regenerating all manifest goldens (unit `manifest/testdata/golden/**` + the four E2E example manifest trees) and updating the hand-written `0.1.0` fixtures in `cmd/sqlgen/cli/testdata/manifest/**`, `cmd/sqlgen/cli/cmd_manifest_{validate,diff}_test.go`, and runtime `manifest/{load,document}_test.go`.
- No golden churn this closure — the sweep is verification-only (no schema or version change landed).
- No new dependencies; no runtime-invariant changes.
