# Phase 17: Sub-Categorized Polymorphism

Status: Complete (Phase 17 closed 2026-05-13; 17.1–17.3 closed 2026-05-12, 17.4 closed 2026-05-13)
PRD Sections: 13.7, 4.8

> **Rationale:** PRD §13.7 was rewritten 2026-05-12 to drop the originally-spec'd discriminator-style filter parser, enum-value validation, and cache-fingerprint plumbing — all of which contradicted §27.6 (relationships are cache-bypass) or required infrastructure with no consumer pull. The simplified model just relaxes the relationship dedup key from `(target, fk)` to `(target, fk, filter)` (byte-equal compare). Phase 17 implements that relaxation end-to-end: config validation, per-relationship FieldOptions field emission, per-relationship loader generation, and example fixtures exercising the asset/documents schema across postgres / mysql / sqlite / graphql.

> **Scope rule:** Phase 17 is feature-additive but tightly bounded — same shape as Phase 15. The deferred enhancements in PRD §13.7.4 (codegen-time `filter:` contents validation, enum literal validation, reverse dispatch) are NOT in scope. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve it outside the phase per the standing rule.

> **Runner notes:** Config-side changes land in `cmd/sqlgen/config/`. Codegen changes land in `cmd/sqlgen/gen/templates/table/`, `cmd/sqlgen/gen/templates/api/`, and supporting orchestration files. E2E tests live under `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,graphql}/tests/`. `make check` + `make check-examples` + `make test-integration` must all pass before each sub-item is marked complete.

---

## 17.1 Config validation + relationship dedup key extension

**PRD Reference:** §13.7.1, §13.7.3
**Design Reference:** `IMPLEMENTATION_ORDER.md` §17.1

**Module:** `cmd/sqlgen/config/validate.go`, `cmd/sqlgen/gen/context_table.go`

**Status:** Complete

**Depends on:** Nothing — pure config-side change.

### Tasks

- [x] Update relationship validation in `cmd/sqlgen/config/validate.go`:
  - Group relationships per parent by `(target_table, fk_column)` for o2o/o2m, `(target_table, junction_table, junction_local_fk, junction_reference_fk)` for m2m.
  - Within each group, identify duplicates by full key including byte-equal `filter:` comparison. Two relationships sharing the full key → hard validation error citing PRD §13.7.3.
  - Two relationships sharing the prefix but with distinct `filter:` values → allowed; require `name:` distinct (already a §4.8 requirement, validate explicitly here).
  - Remove or relax any pre-existing single-relationship-per-(target,fk) check. _(No pre-existing check existed — validation was purely additive.)_
- [x] Update `cmd/sqlgen/gen/context_table.go`:
  - Per-table relationship list passed into `TableContext.Relationships` now carries multiple entries for the same `(target, fk)` pair after the dedup relaxation. Each entry generates its own field on `<Table>FieldOptions`.
  - No struct-shape changes needed — the existing `Name` field already disambiguates the FieldOptions field. _(Verified — `buildRelationshipContexts` appends per config entry; `RelationshipOptionsDef` already dedups by target struct name; relationship-load template emits per `.Filter`; O2O alias generator uses `rel.FieldName + usedAliases` so distinct-name guarantees distinct aliases. No code change required.)_

### Acceptance Criteria

- Two relationships on the same parent with identical `(target, fk, filter)` (byte-equal) produce a hard validation error citing PRD §13.7.3 in the message.
- Two relationships on the same parent with distinct `filter:` strings (or one empty) but the same `(target, fk)` are accepted; both end up as fields on `<Table>FieldOptions`, named per their `name:` config.
- M2M dedup uses the extended key `(target, junction, local_fk, ref_fk, filter)`.
- Pre-existing examples that don't exercise sub-categorization continue to validate and generate identically (golden-stable).

### Tests Required

- [x] `cmd/sqlgen/config/validate_test.go`: table-driven tests covering the two §13.7.3 rules — at minimum: `duplicate full key (o2o)`, `duplicate full key (m2m)`, `distinct filter on shared prefix → allowed`, `distinct filter on m2m shared prefix → allowed`, `missing distinct name: on shared prefix → error`, `single relationship without filter → still allowed`. _(Landed as `TestValidatePreParse_Relationships_DedupRules` — 11 cases including byte-equal whitespace distinction, one-empty-filter coexistence, FIX-093 mixed o2o/o2m collision, and FIX-093 type-synonym variant collision.)_
- [x] `cmd/sqlgen/gen/context_table_test.go`: assert that two relationship config entries sharing `(target, fk)` with distinct filters produce two `RelationshipContext` entries with the expected `Name` values. _(Landed as `TestBuildTableContexts_SubCategorizedRelationships` with three sub-categorized relationships (o2o + 2×o2m) — pins per-relationship Filter passthrough and the single-`DocumentRelationshipOptions` cross-relationship dedup.)_

### Completion Record

**Files changed (2026-05-12):**
- `cmd/sqlgen/config/validate.go` — added `validateRelationships` (wired into `ValidatePreParse`) + `relationshipDedupKey` helper + `isM2M` synonym predicate + `sortedTableKeys` (deterministic per-table iteration for stable diagnostic ordering). Validation enforces (a) duplicate full dedup key → hard error citing PRD §13.7.3, (b) duplicate `name:` per parent → hard error citing PRD §4.8. Per PRD §13.7.3 the dedup key is `(target, fk, filter)` for o2o/o2m and `(target, junction, junction_local_fk, junction_reference_fk, filter)` for m2m — **relationship type is intentionally not part of the key**, so o2o and o2m share a bucket (matching PRD wording verbatim) and `parseRelationshipType` synonyms (`one_to_one`↔`o2o`, `one_to_many`↔`o2m`, `many_to_many`↔`m2m`) cannot bypass dedup. `filter:` compared byte-equal — no whitespace normalization (PRD §13.7.1). Field separator is `\x00` (cannot appear in a valid SQL identifier or filter); each key prefixed with a sentinel (`"m"` for m2m, `"x"` for non-m2m) so an o2o/o2m entry with empty fk cannot alias into an m2m bucket.
- `cmd/sqlgen/config/validate_test.go` — added `TestValidatePreParse_Relationships_DedupRules` (11 table-driven cases). Beyond the spec-required minimum: byte-equal whitespace distinction (`entity_type='x'` vs `entity_type = 'x'` accepted as distinct sub-categories), shared-prefix-with-one-empty-filter (the "all rows + a sub-category" pattern from §13.7.1's stranded-rows note), per-rule error-message substring assertions (`PRD §13.7.3` for dedup, `PRD §4.8` for name), and two FIX-093-driven cases pinning the strict-PRD reading: mixed o2o/o2m collision on identical `(target, fk, filter)` and `o2o`/`one_to_one` synonym-variant collision.
- `cmd/sqlgen/gen/context_table_test.go` — added `TestBuildTableContexts_SubCategorizedRelationships`. Schema is the minimal viable `asset` + `documents` shape from PRD §13.7.1 (no PG enum dependency — `entity_type` typed as `text` since the test exercises codegen-context build, not schema parsing). Asserts (a) name→filter map round-trip across all 3 relationships, (b) Filter strings preserved verbatim (the per-table relationship loader template emits them onto WHERE, so a stray normalization would silently change executed SQL), (c) `DocumentRelationshipOptions` emitted exactly once across all 3 sub-categorized fields. Does not pin slice ordering because `buildRelationshipContexts` sorts alphabetically — the load-bearing invariant is set membership, not order.

**Reconciliations (vs 17.1 spec text):**
1. **No pre-existing single-relationship-per-(target,fk) check to relax.** The task says "Remove or relax any pre-existing single-relationship-per-(target,fk) check"; a grep across `cmd/sqlgen/config/`, `cmd/sqlgen/gen/`, and `parser/` surfaced none. The validation landed in 17.1 is purely additive — without it, the existing codepath would silently accept duplicate relationships (FieldOptions field collision would surface as a Go compile error at consumer build time, far from the typo).
2. **`context_table.go` required zero code changes.** Both auto-detected and config relationships already flow through `buildRelationshipContexts` as one append-per-entry. `RelationshipOptionsDef` dedups by target struct name (one `DocumentRelationshipOptions` regardless of relationship count). The O2O alias generator (`generateAlias(rel.FieldName, usedAliases)`) uses the relationship's `FieldName` — derived from `Name` — so the distinct-name guarantee from 17.1's validation prevents alias collisions automatically. Verification covered by the new context_table_test.go test.
3. **FIX-093 landed inline.** Initial implementation prepended `type` to the dedup key as a defensive measure (catches exact-type duplicates only). `/verify 17.1`'s reviewer pass flagged this as a deviation from PRD §13.7.3's explicit single-key-shape-for-o2o-and-o2m wording. FIX-093 dropped `type` from the key and switched to structural-only distinction (m2m vs non-m2m via the `isM2M` synonym-aware predicate). The two new test cases (mixed o2o/o2m duplicate, `o2o`/`one_to_one` synonym-variant duplicate) lock the strict-PRD reading in. All nine pre-existing cases continue to pass — the refactor preserves exact-type duplicate detection while now also catching mixed-type and synonym-variant duplicates.

**Carry-over note for 17.2.** The static `rel.Filter` is currently honored end-to-end **only on the O2O JOIN path** (`cmd/sqlgen/gen/templates/table/relationships.go.tmpl:6–10` AND's it into the join's `On` clause). The O2M and M2M relationship loaders in `cmd/sqlgen/gen/templates/table/get.go.tmpl:367–388` / `:494–517` merge `fo.<Field>.Filter` (FieldOptions-supplied at call time) onto the per-target `Filter` slice — they do not consult the static config `rel.Filter`. With 17.1's validation relaxed, multi-entry O2M/M2M sub-categorization is structurally valid but the dispatching `filter:` predicate does not yet reach the generated SQL. 17.2's "per-relationship loader emission" tasks include threading `rel.Filter` into the O2M/M2M loader builders (likely as an unconditional `And` term prepended to the `filter` value before the `BuildSelect` call). This is template work, not a verification — the 17.2 spec should read "extend" rather than "confirm" for the O2M/M2M loader path.

**Verification:** `make check` clean (8 modules, 0 lint, all `-short -race` tests green; cmd/sqlgen/config ~1.4s incl. 11-case dedup-rule suite, cmd/sqlgen/gen ~5.1s). `make check-examples` clean across all 8 example modules (0 lint, all tests green; graphql/tests ~36.5s, mysql ~12.4s, postgres ~5.6s, others <3s — no regression from the validation addition). One FIX surfaced + resolved inline: **FIX-093** (dedup key `type`-prepending deviation from PRD §13.7.3; resolved 2026-05-12).

---

## 17.2 Codegen — per-relationship loader emission

**PRD Reference:** §13.7.1, §26.5.2
**Design Reference:** `IMPLEMENTATION_ORDER.md` §17.2

**Module:** `cmd/sqlgen/gen/templates/table/`, `cmd/sqlgen/gen/templates/api/`, `cmd/sqlgen/gen/orchestrate.go`

**Status:** Complete

**Depends on:** 17.1.

### Tasks

- [x] Verify the per-table relationship loader template iterates `TableContext.Relationships` correctly post-17.1 (each entry produces its own `LoadXxx` call). _(Confirmed via `TestGetTemplate_subCategorizedO2MFilter` — two O2M relationships sharing `(documents, entity_id)` emit two independent `if fo.<Field> != nil` blocks and two `g.Go` invocations.)_ Extended the loader to thread the static `rel.Filter` into `Get<Target>Input.conditions` via `sql.Raw(...)` on both O2M and M2M paths — the 17.1 carry-over note flagged this as missing.
- [x] GraphQL field-options walker (`cmd/sqlgen/gen/templates/api/field_options.go.tmpl`): emit one `case "<field>":` per relationship using the relationship's `name:` as the GraphQL field name (camelCased per §26.5.2 conventions). _(No template change needed — the walker already iterates `$t.Relationships` per-entry; verified by `TestWalker_SubCategorizedRelationships`.)_
- [x] GraphQL schema generator (`cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl`): emit one selectable field per relationship on the parent's GraphQL type. _(No template change needed — `api/table-schema` already iterates `$t.Relationships` per-entry; verified by the same test asserting `primaryDocument: Document`, `attachments: [Document!]!`, `invoices: [Document!]!`.)_
- [x] Confirm the walker-completeness lint (PRD §26.5.2 — `cmd/sqlgen/gen/api_walker.go`) covers per-relationship field emission. _(Confirmed inline in `TestWalker_SubCategorizedRelationships` by invoking `gen.ValidateAPIWalkerCompleteness` against the sub-categorized asset/documents fixture — lint passes; no lint changes required.)_

### Acceptance Criteria

- A parent table with three relationships sharing `(target, fk)` (distinct filters) produces:
  - Three fields on `<Table>FieldOptions`.
  - Three relationship loader calls in the parent client's `loadRelationships` body.
  - Three selectable fields on the parent's GraphQL type.
  - Three matching `case "<field>":` clauses in the per-table walker.
- Walker-completeness lint (PRD §26.5.2 message format) holds against the new asset/documents schema once 17.3 lands.
- Existing example goldens that don't exercise sub-categorization remain byte-stable.

### Tests Required

- [x] `cmd/sqlgen/gen/api_walker_test.go`: assert per-relationship case emission for a parent with multiple sub-categorized relationships (in-memory fixture, no example regeneration). _(Landed as `TestWalker_SubCategorizedRelationships` — 3-relationship asset/documents fixture asserts walker-case emission, target-FieldOptions descent, `DocumentRelationshipOptions` wrap for the O2M pair, walker-completeness lint pass, and per-relationship GraphQL schema field emission in a single test for shared-fixture economy.)_
- [x] Template golden tests in `cmd/sqlgen/gen/`: pin per-relationship FieldOptions field generation + loader emission against a synthetic asset/documents fixture. _(Landed in `cmd/sqlgen/gen/get_test.go` as `TestGetTemplate_subCategorizedO2MFilter` (two O2M sub-categorized relationships → two independent `if fo.<Field>` blocks + two distinct `conditions: []sql.Condition{sql.Raw(...)}` lines + 2× `g.Go`), `TestGetTemplate_subCategorizedM2MFilter` (M2M filter threads through after junction resolves IDs), and `TestGetTemplate_o2mNoStaticFilterNoConditions` (negative test — empty `rel.Filter` MUST NOT emit `conditions:` so every existing example golden stays byte-stable).)_

### Completion Record

**Files changed (2026-05-12):**
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — threaded `rel.Filter` into the O2M loader (line ~390) and M2M loader (line ~520) via a new `{{- if .Filter }}conditions: []sql.Condition{sql.Raw({{ printf "%q" .Filter }})},{{ end }}` block on the `Get<Target>Input` literal. The `conditions` slot already existed for internal use (Connection keyset injection — `cmd/sqlgen/gen/templates/shared/_input.tmpl:43`) and threads through to `sql.BuildSelect` via `conds = append(conds, input.conditions...)` (`get.go.tmpl:205`), so the static filter AND's onto the WHERE clause alongside the consumer-supplied `fo.<Field>.Filter`. Empty `rel.Filter` (the pre-17.2 default for every example) skips the block entirely — keeps all existing example goldens byte-stable. `sql.Raw` and the `sql` package are already imported by every parent client (used in the M2M junction query and in `sql.BuildSelect`), no import wiring needed.
- `cmd/sqlgen/gen/get_test.go` — added 3 tests: `TestGetTemplate_subCategorizedO2MFilter`, `TestGetTemplate_subCategorizedM2MFilter`, `TestGetTemplate_o2mNoStaticFilterNoConditions`. The negative test is load-bearing: it locks in the byte-stability invariant for every pre-17.2 example, so an accidental future change to emit `conditions:` unconditionally would break every golden file.
- `cmd/sqlgen/gen/api_walker_test.go` — added `TestWalker_SubCategorizedRelationships`. One shared fixture asserts (a) per-relationship case emission in the walker (`primaryDocument` / `attachments` / `invoices`), (b) shared `DocumentRelationshipOptions` wrap for the O2M pair (one struct, two fields — pinned to upstream `TestBuildTableContexts_SubCategorizedRelationships`), (c) walker-completeness lint passes for the multi-entry context (PRD §26.5.2), and (d) GraphQL schema generator emits one selectable field per relationship with the correct cardinality suffix (`Document` for o2o vs `[Document!]!` for o2m). The lint-check is the third leg of the contract: walker case + schema field + completeness lint.

**Reconciliations (vs 17.2 spec text):**
1. **First task changed from "verify" to "verify + extend".** The 17.1 carry-over note flagged that the static `rel.Filter` was only honored on the O2O JOIN path (`templates/table/relationships.go.tmpl:6-10`), and that O2M / M2M loaders merged only the FieldOptions-supplied `fo.<Field>.Filter` — the spec-required dispatching predicate did not reach the SQL. 17.2 extends the loader template to thread `rel.Filter` through `Get<Target>Input.conditions`. This is now reflected in the task wording.
2. **GraphQL walker and schema templates required zero changes.** Both `api/field-options` (`cmd/sqlgen/gen/templates/api/field_options.go.tmpl:48-58`) and `api/table-schema` (`cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl:12-15`) already iterate `Relationships` per-entry. The 17.1 dedup relaxation lands multi-entry `Relationships` on every downstream context (verified inline by `TestBuildTableContexts_SubCategorizedRelationships`), so per-relationship emission flows through naturally. The new walker test exercises this end-to-end for the first time.
3. **Walker-completeness lint required no changes.** `gen.ValidateAPIWalkerCompleteness` (`cmd/sqlgen/gen/api_walker.go:34-44`) iterates `table.Relationships` by relationship `Name` (not by `(target, fk)` pair) against the API context's `Relationships` slice. Since 17.1 keeps Names distinct per parent (PRD §4.8) and the API context is built per-entry, the lint passes for the multi-entry shape without modification. The new test confirms this inline.

**Carry-over for 17.3.** None. The 17.2 surface is complete: config validation (17.1), per-table loader emission with static-filter threading (17.2), GraphQL walker per-relationship cases (17.2), GraphQL schema per-relationship fields (17.2), walker-completeness lint (17.2). 17.3 lands the example fixtures and per-dialect integration tests that exercise the full stack against real databases.

**Verification:** `make check` clean (8 modules, 0 lint, all `-short -race` tests green; cmd/sqlgen/gen ~5.7s including the 3 new get_test.go + 1 new api_walker_test.go cases). `make check-examples` clean across all 8 example modules (0 lint, all tests green; graphql/tests ~38.4s, mysql/tests ~12.7s, postgres/tests ~5.4s, sqlite/tests ~2.2s, others <2s). No example uses `filter:` on a relationship yet (verified via grep), so the template's conditional emission produces byte-identical output for every pre-17.2 golden — no `make update-golden-e2e` or `make update-golden` regeneration needed.

---

## 17.3 E2E example + cross-dialect integration tests

**PRD Reference:** §13.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §17.3

**Module:** `cmd/sqlgen/testdata/examples/postgres/`, `cmd/sqlgen/testdata/examples/mysql/`, `cmd/sqlgen/testdata/examples/sqlite/`, `cmd/sqlgen/testdata/examples/graphql/`

**Status:** Complete

**Depends on:** 17.2.

### Tasks

- [x] Extend the postgres example's `schema.sql` with `assets` and `documents` tables. `documents.entity_type` is a PG enum type (`document_entity_type_enum`); `documents.entity_id` is the FK column.
- [x] Add three relationship entries on `assets` in `cmd/sqlgen/testdata/examples/postgres/sqlgen.yml`: `PrimaryDocument` (o2o, `filter: "entity_type = 'asset.primary'"`), `Attachments` (o2m, `entity_type = 'asset.attachment'`), `Invoices` (o2m, `entity_type = 'asset.invoice'`).
- [x] Mirror the schema in `mysql/` (use `ENUM('asset.primary', ...)` column type) and `sqlite/` (use `TEXT` since SQLite has no enums).
- [x] Mirror in the graphql example so the API walker surfaces the asset/documents schema; relationship config goes in `cmd/sqlgen/testdata/examples/graphql/sqlgen.yml`.
- [x] Regenerate goldens via `make update-golden-e2e`. Verify the diffs are localized to the new tables. _(Diffs are localized to assets/documents; the only cross-table delta is a stable cosmetic reordering of the per-client `<rel>DefaultSort` / `<target>Client` field grouping introduced by the dedup fix below — every existing example continues to compile and pass its tests.)_
- [x] Per-dialect integration test (`cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/sub_categorized_polymorphism_test.go`): seed assets + documents covering all three discriminator values plus a stranded `spv` value; query an asset with all three relationships set; assert each returns its proper subset and the stranded row appears in none.
- [x] Curated-surface test in `cmd/sqlgen/testdata/examples/graphql/tests/sub_categorized_polymorphism_test.go` confirming the three nested fields each return only their discriminator's rows.

### Acceptance Criteria

- All four example modules (postgres / mysql / sqlite / graphql) regenerate cleanly via `make update-golden-e2e`.
- Per-dialect integration tests pass under `-race` against real databases (testcontainers for PG / MySQL; in-memory for SQLite).
- Graphql curated-surface test pins the per-relationship subset return shape.
- Stranded discriminator values are silently excluded from all three relationships, consistent with PRD §13.7.1.

### Tests Required

- [x] `cmd/sqlgen/testdata/examples/postgres/tests/sub_categorized_polymorphism_test.go`
- [x] `cmd/sqlgen/testdata/examples/mysql/tests/sub_categorized_polymorphism_test.go`
- [x] `cmd/sqlgen/testdata/examples/sqlite/tests/sub_categorized_polymorphism_test.go`
- [x] `cmd/sqlgen/testdata/examples/graphql/tests/sub_categorized_polymorphism_test.go` (curated-surface variant)

### Completion Record

**Files changed (2026-05-12):**
- `cmd/sqlgen/testdata/examples/postgres/{schema.sql,sqlgen.yml}` — added `document_entity_type_enum`, `assets`, `documents`; three relationship entries on `assets` (`PrimaryDocument` o2o, `Attachments` o2m, `Invoices` o2m), each with a distinct `filter:` against `entity_type`.
- `cmd/sqlgen/testdata/examples/mysql/{schema.sql,sqlgen.yml}` — same shape with the inline `ENUM('asset.primary', 'asset.attachment', 'asset.invoice', 'spv')` discriminator. BIGINT PKs (no UUID); FK column typed `BIGINT NOT NULL` (no REFERENCES — polymorphic).
- `cmd/sqlgen/testdata/examples/sqlite/{schema.sql,sqlgen.yml}` — TEXT discriminator (no native enum). INTEGER PKs. Test seeds against literal string discriminators.
- `cmd/sqlgen/testdata/examples/graphql/{schema.sql,sqlgen.yml}` — full PG enum discriminator (`document_entity_type_enum`), curated GraphQL surface exposes `primaryDocument` / `attachments` / `invoices` on `Asset`.
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/sub_categorized_polymorphism_test.go` — per-dialect integration tests; each seeds 1 primary + 2 attachments + 1 invoice + 1 stranded `spv` row, queries the parent with all three relationships set, asserts subset membership and stranded-row exclusion.
- `cmd/sqlgen/testdata/examples/graphql/tests/sub_categorized_polymorphism_test.go` — curated-surface variant. Same fixture shape exercised through GraphQL mutations + a single nested-fields query.
- `cmd/sqlgen/testdata/examples/graphql/tests/main_test.go` — `truncateAll` extended with `documents` and `assets` (TRUNCATE CASCADE order: children first, parents last, both before their referenced lookup tables).
- All eight example modules' `models/` and `expected/` directories — regenerated via `make update-golden-e2e`. Diff is localized to the new tables plus the cosmetic per-client field reorder from the dedup fix below.

**Reconciliations (fixes landed inline to unblock 17.3):**

1. **FIX-094 (sub-categorized o2m client-ref duplication).** Pre-existing template bug surfaced for the first time by multi-target o2m sub-categorization: `cmd/sqlgen/gen/templates/table/client.go.tmpl` emitted one `<Target>Client *<Target>Client` field per O2M/M2M relationship, so two relationships sharing a target table (e.g. `Attachments` + `Invoices` both → `documents`) compiled to `documentClient redeclared`. Fix splits the emission: per-relationship `<rel>DefaultSort` stays, but the target-client refs come from a new per-table `RelationshipTargetClients []string` field deduped by target struct name. Importantly, this is NOT `RelationshipOptionsDefs` (which `orchestrate.deduplicateRelationshipOptionsDefs` rebases across tables for one-struct-per-package emission — using it would steal the `Client` field from sibling parents). Added `buildRelationshipTargetClients(o2m, m2m)` next to `buildRelationshipOptionsDefs` and threaded the field through `assembleTableContext`.

2. **FIX-095 (filter-comparator mismatch on config-declared polymorphic FKs).** Pre-existing limitation surfaced when `documents.entity_id` has no SQL `REFERENCES` clause (the PRD §13.7.1 example explicitly omits it because polymorphism allows multiple parents). The relationship loader emits `&comparator.ID{In: ...}` for the FK filter expression, but the column-side `DocumentFilter.EntityID` was typed `*comparator.String` because `resolveSimpleComparator` only upgrades to `ID` when `col.PrimaryKey || col.FKReference != nil`. Fix adds `applyConfigDeclaredFKs(schema, cfg)` at the top of `BuildTableContexts`: walks every `cfg.Tables[*].Relationships[*]`, looks up the target column on the schema, and stamps a synthetic `parser.FKReference` when none exists. Synthetic ref's destination points back to the declaring parent for traceability; only its existence influences codegen (`resolveSimpleComparator`'s comparator-type decision). All pre-existing examples are unaffected — every prior `fk:` in YAML targets a column that already has a SQL `REFERENCES` clause.

3. **FIX-096 (column-enum → GraphQL-enum codegen, including the slice path).** Pre-existing gap surfaced the first time a PG enum column flows through the GraphQL surface. Two symptoms: (a) `graphQLTypeForGoType` fell through to `String` for column types it didn't recognize, so `documents.entity_type` emitted as `String!` instead of the enum — gqlgen then generated a `documentResolver.EntityType` panic stub because it couldn't bridge `models.DocumentEntityTypeEnum` to a GraphQL `String` field; (b) the same lookup missed the PostgreSQL-array companion (`<Enum>Slice`), so a `document_entity_type_enum[]` column would have hit the same panic-stub trap on output and a compile-time type-mismatch in the input translator (`[]models.<Enum>` vs `models.<Enum>Slice`). Fix lands the column-enum → GraphQL-enum projection end-to-end:
   - **Context.** New `APIEnumContext` / `APIEnumValue` types on `APIContext.UsedEnums`. `BuildAPIContext` now accepts an `[]EnumContext` slice (sourced from `BuildEnumContexts` in both `generateAPI` and the public `runGraphQLGen` entry — public `ComputeNameCollisions` exposed so the latter can build its own collision set without rebuilding the whole pipeline).
   - **Per-column projection.** `graphQLTypeForGoType` consults a `goType → EnumContext` lookup populated from both `e.GoTypeName` and `e.SliceGoTypeName`, so the bare-enum AND slice paths resolve symmetrically. `mapColumnToGraphQL` records each referenced enum in `usedEnumsByName` so the shared-schema template emits exactly the enum declarations actually referenced by an exposed column.
   - **Schema emission.** `templates/api/shared.graphqls.tmpl` now emits one `enum <GraphQLName> { ASSETPRIMARY ASSETATTACHMENT ... }` block per used enum. Identifier shape is `strings.ToUpper(toPascalCase(value))` — matches the existing `<T>SortField` convention so no new naming rule.
   - **Wire-format bridging.** `templates/enum.go.tmpl` gained `MarshalGQL(io.Writer)` / `UnmarshalGQL(any)` on both the bare enum and its named-slice variant via duck typing (no gqlgen import in the models package; just stdlib `io`). The methods translate between the GraphQL identifier (`ASSETPRIMARY`) and the SQL literal (`asset.primary`) on every read/write, so the database column receives the SQL form unchanged.
   - **gqlgen models merge.** `cli/graphql.go::buildMergeInput` binds `<EnumGraphQLName> → [<modelsPkg>.<Enum>, <modelsPkg>.<Enum>Slice]`. Bare-enum entry listed first so gqlgen picks it for scalar contexts; the slice entry covers `[<Enum>!]!` fields without falling back to a resolver stub.
   - **Input-translator slice cast.** `buildAPIInputFields` detects when `col.IsSlice` and the model uses a named slice (suffix `Slice`, `col.SliceElemType != ""`), and emits `models.<Enum>Slice(in.<Field>)`. `funcInputCoerceDeref` applies the cast on the `GqlgenIsBareNullable` path too (gqlgen emits bare `[]T` for nullable slice update fields). `gqlgenIsBareNullable` was extended to `col.IsSlice || (col.Nullable && (json/jsonmap))` so all slice columns flow through the no-deref branch uniformly.
   - **Enum-file import.** Added `io` to enum-file imports unconditionally — small cost, keeps the per-file import set deterministic regardless of API enablement.
   - **Verified end-to-end via the curated-surface test.** Mutation seeds with `[ASSETPRIMARY, ASSETATTACHMENT]` GraphQL identifiers; query reads back `declaredCategories` as `[ASSETPRIMARY, ASSETATTACHMENT]` (the slice MarshalGQL round-trip) and `entityType` as `ASSETPRIMARY/ASSETATTACHMENT/ASSETINVOICE` (the scalar MarshalGQL round-trip).

4. **FIX-097 (sqlite test infrastructure: `:memory:` is per-connection).** Pre-existing test-infra limitation surfaced when the sub-categorized o2m fan-out fires two errgroup goroutines for `Attachments` + `Invoices`. `modernc.org/sqlite` with the bare `:memory:` connection string gives each pool connection its own private in-memory database — schema applied on the first connection is invisible to every subsequent one. Single-relationship existing tests passed by accident because `sql.DB` reused the warm connection; two-relationship loaders break the moment a fresh connection is grabbed. Fix switches `cmd/sqlgen/testdata/examples/sqlite/tests/main_test.go` to `file::memory:?cache=shared`, which is shared across all pool connections. Existing tests continue to pass under the new connection string.

**Surface delta summary.** The four fixes above are all required to ship 17.3's example fixtures. Each is a separate concern — codegen dedup (FIX-094), config-declared FK comparator alignment (FIX-095), enum input translator cast (FIX-096), and sqlite shared-cache test infra (FIX-097) — and each could land independently. They are bundled here because they all surfaced during 17.3 bring-up and were strictly necessary to compile / lint / pass the new example modules across all four dialects. None expanded scope beyond what the PRD §13.7 contract requires.

**Verification:** `make check` clean (8 modules, 0 lint, all `-short -race` tests green). `make check-examples` clean across all 8 example modules: graphql/tests ~27s, mysql/tests ~10s, postgres/tests ~5.5s, sqlite/tests ~2s, others <2s. The new integration tests pass:
- `postgres/tests::TestSubCategorizedPolymorphism`
- `mysql/tests::TestSubCategorizedPolymorphism`
- `sqlite/tests::TestSubCategorizedPolymorphism`
- `graphql/tests::TestCuratedSurface_SubCategorizedPolymorphism`

**Carry-over for 17.4.** None on functional surface. 17.4 runs the closure sweep (`make check` + `make check-examples` + `make test-integration` under `-race`) and updates STATUS.md to mark Phase 17 complete. The four FIX entries above should be triaged into `/fix` records during 17.4 closure if they aren't already (they were never filed as separate `/fix` entries since each only manifests when 17.3 lands, and each ships fully resolved here).

---

## 17.4 Phase closure sweep

**PRD Reference:** §13.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §17.4

**Module:** Documentation + repo-wide test sweep

**Status:** Complete

**Depends on:** 17.1–17.3.

### Tasks

- [x] Run `make check` + `make check-examples` + `make test-integration` against the full repository under `-race`; record any flakes / timing in this Completion Record.
- [x] Confirm 17.1–17.3 Completion Records are filled in (files changed, commit references, sweep results, any inline drift notes).
- [x] Update `docs/tracker/STATUS.md` Current Focus to reflect Phase 17 closure with the surface delta summary.
- [x] `/fix` triage: any pre-existing bug uncovered during 17.x test bring-up gets a FIX entry, not in-place scope expansion.
- [x] Optional PRD reconciliation: any spec-vs-landed-codegen divergence reconciled inline at the test file header (test-only) or via a targeted PRD edit (behavioral). _(Handled inline during 17.3 + FIX-098: PRD §13.7.1 gained a "Multi-column filters" paragraph documenting codegen-time identifier auto-qualification; §13.7.4 reframed deferred items to `filter:` *contents* validation. No 17.4-specific PRD edits needed.)_

### Acceptance Criteria

- Full repo passes `make check` + `make check-examples` + `make test-integration` clean under `-race`.
- All 17.x Completion Records filled in.
- STATUS.md Current Focus reflects Phase 17 closure with the surface delta summarized.
- No silently-deferred work — anything not landed gets a FIX entry or a PRD §13.7.4 row.

### Tests Required

- N/A (closure sweep — runs the existing matrix; no new tests).

### Completion Record

**Files changed (2026-05-13):**
- `docs/tracker/phase-17.md` — marked 17.4 tasks complete + filled in this record; flipped the phase Status header from "In Progress" to "Complete".
- `docs/tracker/STATUS.md` — Phase 17 row in the Overview table flipped to `4 | 4 | Complete`; Current Focus rewritten to reflect Phase 17 closure with the per-sub-item surface delta summary.

**Sweep results.** All three Make targets pass clean under `-race` against today's HEAD (`d8f3b74 fix: correct filter parsing for table relationships` — the FIX-098 landing commit). No flakes observed; timings recorded below for the long-pole legs.

| Target | Result | Notes |
|--------|--------|-------|
| `make check` | clean | 8 modules, 0 lint. Longest legs: cmd/sqlgen/cli ~39.7s, cmd/sqlgen/gen ~5.5s, cmd/sqlgen/gotype ~2.5s, cmd/sqlgen/config ~2.3s, parser/sqlite ~2.2s, database/stdlib ~2.1s. All other modules <2s. |
| `make check-examples` | clean | 8 example modules, 0 lint. graphql/tests ~32.7s (curated-surface incl. `TestCuratedSurface_SubCategorizedPolymorphism` + `TestCuratedSurface_SubCategorizedPolymorphism_MultiColumnFilter`); mysql/tests ~15.1s; postgres/tests ~6.2s; postgres_stdlib/tests ~3.2s; sqlite/tests ~2.1s; cache/tests ~1.9s; tenancy/tests ~2.0s; events/tests ~1.7s. All four new `TestSubCategorizedPolymorphism` (postgres / mysql / sqlite) + curated-surface variants green. |
| `make test-integration` | clean | Full sweep without `-short`. Long poles: cmd/sqlgen ~5.4 min (Docker testcontainer e2e), cmd/sqlgen/cli ~3.4 min (Docker testcontainer CLI), cache/redis ~22.4s (testcontainer redis), cmd/sqlgen/gen ~14.5s, parser/introspect ~11.4s, database/stdlib ~10.0s, database/pgx ~4.2s. No flakes; no -race detector reports. |

**17.1–17.3 completion records.** All filled in by the time 17.4 started. 17.1's record documents the additive validation in `cmd/sqlgen/config/validate.go::validateRelationships`, the dedup-key shape decision (`(target, fk, filter)` for o2o/o2m and `(target, junction, junction_local_fk, junction_reference_fk, filter)` for m2m — relationship type intentionally not in the key per PRD §13.7.3), and FIX-093's strict-PRD reconciliation. 17.2's record documents the static-filter threading into `Get<Target>Input.conditions` on the O2M / M2M loader paths (the 17.1 carry-over flagged this gap) and the byte-stability invariant pinned by `TestGetTemplate_o2mNoStaticFilterNoConditions`. 17.3's record documents the four-dialect example surface (`assets` + `documents` + `document_entity_type_enum`), the per-dialect integration tests, the curated GraphQL surface, and the four inline FIXes (094–097) — each filed against `docs/tracker/fixes.md` ahead of 17.4. No drift between landed code and tracker text was found during the sweep audit.

**FIX triage.** All five FIXes that surfaced during Phase 17 bring-up are filed and resolved in `docs/tracker/fixes.md`:
- **FIX-093** (17.1 inline) — dedup key `type`-prepending deviation from PRD §13.7.3's explicit single-key-shape-for-o2o-and-o2m wording. Resolved 2026-05-12.
- **FIX-094** (17.3 inline) — sub-categorized O2M / M2M client-ref duplication via per-relationship emission in `client.go.tmpl`. Resolved 2026-05-12 by introducing per-table `RelationshipTargetClients` deduped by target struct name (separate from cross-table `RelationshipOptionsDefs`).
- **FIX-095** (17.3 inline) — filter-comparator mismatch on config-declared polymorphic FKs (target column without SQL `REFERENCES`). Resolved 2026-05-12 by adding `applyConfigDeclaredFKs` pre-pass in `BuildTableContexts` that stamps a synthetic `parser.FKReference` on every config-declared `fk:` column so `resolveSimpleComparator` upgrades to `comparator.ID` uniformly.
- **FIX-096** (17.3 inline) — column-enum → GraphQL-enum codegen including the PostgreSQL-array `<Enum>Slice` companion. Resolved 2026-05-12 by lighting up `APIEnumContext.UsedEnums`, per-column projection through both `e.GoTypeName` and `e.SliceGoTypeName`, schema emission via `templates/api/shared.graphqls.tmpl`, `MarshalGQL` / `UnmarshalGQL` on both the bare enum and its named-slice variant in `templates/enum.go.tmpl` (stdlib `io` only — no gqlgen import in the models package), gqlgen models-merge binding `<EnumGraphQLName> → [<modelsPkg>.<Enum>, <modelsPkg>.<Enum>Slice]`, and the named-slice cast in `buildAPIInputFields` / `funcInputCoerceDeref`.
- **FIX-097** (17.3 inline) — sqlite `:memory:` is per-connection in `modernc.org/sqlite`, so multi-relationship errgroup loaders saw an empty schema on fresh pool connections. Resolved 2026-05-12 by switching `cmd/sqlgen/testdata/examples/sqlite/tests/main_test.go` to `file::memory:?cache=shared`.
- **FIX-098** (post-17.3, pre-17.4) — multi-column `filter:` identifier qualification: the O2O JOIN ON template prepended `<alias>.` only to the leading identifier of a `filter:` predicate, so any multi-column raw filter rendered with bare unqualified trailing identifiers. Resolved 2026-05-13 (commit `d8f3b74`) by landing per-dialect codegen-time identifier qualification using each dialect's native SQL parser — new `parser/{postgres,mysql,sqlite}/filter_qualifier.go` exposing `QualifyFilterIdentifiers(expr, alias string)`, dispatched from `cmd/sqlgen/gen/filter_qualifier.go::qualifyFilter`, consumed by `buildSingleO2ODetail` so the qualified result is stored on `O2OJoinDetail.Filter`; template `relationships.go.tmpl:7` simplified to emit `" AND " + {{ printf "%q" .Filter }}`. Surface also extended every example with an `asset_document_links` junction and three new relationships (o2o / o2m / m2m) exercising multi-column raw filters; per-dialect `TestSubCategorizedPolymorphism_MultiColumnFilter` + graphql `TestCuratedSurface_SubCategorizedPolymorphism_MultiColumnFilter` lock the contract in. PRD §13.7.1 gained a "Multi-column filters" paragraph; §13.7.4 reframed its first deferred item as `filter:` *contents* validation (column-existence). Enum literal validation and reverse dispatch (§13.7.2) remain as separate deferred items in §13.7.4.

No additional bugs surfaced during the 17.4 sweep — every leg landed clean on first run.

**Verification.** `make check` clean, `make check-examples` clean, `make test-integration` clean — all under `-race`. Tracker reconciliation: `docs/tracker/STATUS.md` Phase 17 row flipped to `Complete` with sub-items `4 | 4`; Current Focus reflects closure; `docs/tracker/fixes.md` carries all five Phase 17 FIXes in the Resolved section (none Open).

**Phase 17 closure note.** PRD §13.7's relaxed-dedup contract is delivered end-to-end across config validation (17.1), codegen with per-relationship loader emission and static-filter threading on all three relationship types (17.2 + FIX-098), four-dialect example fixtures with integration coverage (17.3), and the GraphQL curated surface including the column-enum → GraphQL-enum projection on bare + slice paths (17.3 + FIX-096). PRD §13.7.4 deferred items — codegen-time `filter:` *contents* validation (column-existence), enum literal validation, reverse dispatch (§13.7.2) — remain explicitly out of scope and will be re-evaluated against consumer pull when next surfaced.
