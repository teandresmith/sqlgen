# Phase 36: Belongs-To Detection

Status: In Progress
PRD Sections: 4.13, 5.3, 13.1, 13.2, 13.4

> **Design.** `docs/design/BELONGS_TO_DETECTION.md`, approved 2026-11-20, is the source for every
> sub-item below. Its §9 lists sub-items N.0–N.6, which are 36.0–36.6 here. Its §6 behavior
> contract sets the Tests Required, and its §11 records rulings Q1–Q6:
> - Q1: a detected to-one edge is named after its naming member without `_id`, singular. It falls
>   back to the referenced table when there is no naming member, or the stem names a column of the
>   holder.
> - Q2: a detected edge whose Go field equals a column's is omitted with a warning. A declared one
>   there is an error, with or without a `column_map` override.
> - Q3: rule 6 disambiguates to-one and list edges separately.
> - Q4: a declared `o2o` whose `fk` columns exist on both tables takes its side from the FOREIGN KEY
>   over them, else belongs-to.
> - Q5: the Phase 35 chain regression is fixed here (36.3), not on PR #7.
> - Q6: no config key; `exclude_relationships` drops a detected belongs-to.
>
> A prototype is at `.claude/prototypes/BELONGS_TO_DETECTION.patch` (design §7, P2). Start 36.2 and
> 36.5 from it.
>
> **Ground rules for every sub-item:**
> - **Fail first.** Each new behavior test is seen failing on the code before its change. Record it
>   in the Completion Record.
> - **Golden churn is confined.** 36.2–36.4 move no golden except a fixture that declares the shape
>   they fix. 36.5 moves goldens widely; check that every change is a new to-one edge, a singular
>   rename of an old to-one field, or the rename of an example test that reads one.
> - **Findings** go through `/fix` → Findings triage.

## 36.0 PRD sync — belongs-to detection in §4.13, §5.3, §13.1, §13.2

**PRD Reference:** Sections 4.13, 5.3, 13.1, 13.2

**Status:** Complete

### Tasks

- [x] §5.3 intro: a foreign key gives at most one edge in each direction: a to-one edge on its holder and, unless the key is unique, a list edge on the referenced table (design §8).
- [x] §5.3 rule 3: an O2M edge also gives a belongs-to O2O edge on the holder, over the same key and constraint; a unique key gives the O2O edge alone.
- [x] §5.3 rule 5: a to-one edge is named after its naming member without `_id`, or after the referenced table when there is none or the stem names a column of the holder; codegen spells it singular.
- [x] §5.3 rule 6: disambiguation groups edges of one cardinality (to-one, or list) that share a source and a target.
- [x] §13.1: the belongs-to Detection cell reads "every foreign key on a table that is not a junction"; replace "Auto-detection emits the O2O edge on the FK-holding side only" with "The has-one view from the referenced side stays config-declared".
- [x] §13.1: add the FK-side rule: a detected edge's side is the one detection read; a declared `o2o` whose `fk` columns exist on both tables follows the FOREIGN KEY over them, else belongs-to.
- [x] §4.13: add the row "A detected relationship resolves to a column's Go field | Warning", and reword the `column_map` row so a declared relationship claiming a column's field is always an error.
- [x] §13.2: a chained join walks only the edges the target's own struct carries.
- [x] `docs/configuration.md` "Relationships": the detected to-one edge, its naming, and the collision warning.
- [x] Registry: add the design §6.1 rows to the owning files (`parser/schema.md` §5.3, `cli/gen.md` §13.x, `cli/config.md` §4.13) with empty Tests cells, and add the `self-o2m` detection row and the `detected-edge-column-collision` validation row.
- [x] Remove `docs/tracker/backlog.md` line 280 (design §3.4 item 2 measures it; 36.2 fixes it).
- [x] `make req-rehash` the edited sections, then `make req-check`.

### Acceptance Criteria

- Every §8 edit in the design is in the PRD or `configuration.md`, word for word or with only copy-editing.
- `make req-check` and `make refs-check` pass.
- No code changes.

### Tests Required

- [x] `make req-check` passes after the rehash.

### Completion Record

**Completed 2026-11-20.** Docs only, no code. Files: `docs/PRD.md`, `docs/configuration.md`, `docs/conformance/{parser/schema.md,cli/gen.md,cli/config.md,cli/config-fields.md,cli/api-schema.md,cli/manifest.md,cli/naming.md,client.md,runtime/sql.md}`, `docs/tracker/{phase-36.md,STATUS.md,backlog.md}`. Uncommitted at completion.

- **PRD:** §5.3 intro (one edge in each direction; a to-one edge on the holder, a list edge on the referenced table unless the key is unique), rule 3 (an O2M edge also gives a belongs-to O2O edge over the same key and constraint), rule 5 (to-one naming from the naming member, its fallback, singular spelling), rule 6 (grouping by cardinality). §13.1: belongs-to Detection cell, the has-one view stays config-declared, the FK-side rule. §13.2: the chain sentence. §4.13: the new detected-edge warning row and the reworded `column_map` collision row.
- **Copy-edits beyond design §8:** the §13.1 has-one Detection cell carries the uniqueness definition the belongs-to cell lost (it read "same"); the new §4.13 row carries the resolution-level tag; rule 6's `exclude_relationships` sentence adds "and a to-one edge's referenced table", stating ruling Q6 and design §5.1 (`BaseName` stays the referenced table) for a to-one edge that was never renamed (auto-accepted: PRD text for an unspecified case, no code or golden effect).
- **`docs/configuration.md` "Relationships":** both directions of a foreign key, to-one naming and its fallback, the collision warning, and `exclude_relationships` by referenced table.
- **Registry:** design §6.1 rows, one per matrix cell, with empty Tests cells (planned): `parser/schema.md` §5.3 `detect.belongs-to.{single,composite,self,non-junction-fks}.*`, `naming.to-one.{stem,fallback}.*`, `naming.rule6.by-cardinality.*` (new `self-o2m` matrix row); `cli/gen.md` new §5.3 anchor `naming.to-one.singular`, §13.1 `fkside.{detected,declared}`, §13.2 `loader.belongs-to.detected`, `chain.matches-target`, §13.4 `takeover.belongs-to.detected`; `cli/api-schema.md` §26.4 `api.belongs-to.graphql` and `cli/manifest.md` §30.4.2 `manifest.belongs-to.detected`, with no matrix cell, since §13.x states neither (auto-accepted reviewer question: docs only, no golden or surface change; `belongs-to × struct-field` stays claimed); `cli/config.md` §4.13 `collision.declared-vs-column` (on the registry's `column-name-collision` rule, the design's `column-map-name-collision`) and `collision.detected-vs-column{,.message,.severity}` (new `detected-edge-column-collision` rule, `together` and `schema` n/a). Narrowed to the new text: `parser.rel.single-o2o-unique.{postgres,mysql,sqlite}` (O2O alone, no name claim), `parser.rel.disambiguation.{postgres,mysql,sqlite}`, `parser.rel.one-edge-per-fk`, `config.column-name-collision`, `naming.field.collision.override-introduced-only`, `gen.relationships.fk-on-target`. Removed `parser.rel.edge-naming-base.o2o`, superseded by `naming.to-one.*` (its test stays claimed by `parser.rel.single-o2o-unique.postgres`). Reviewed the other rows under §4, §4.13, §5.3, §8.5, §13.1 and §13.2 (also `client.md`, `runtime/sql.md`); they hold. Rehashed those six sections.
- **Review (sqlgen-reviewer):** PRD 9/10, one partial (the dropped §8.5 link; §8.5 does state list-name pluralization), restored. Applied its doc fixes: §8.5 Validation and the §4.13 disambiguation row match the new collision and cardinality rules; §13.1's `FKOnTarget` sentence no longer claims the column-name test for every edge; §13.1 "its edge is O2O, not O2M" → "it gives an O2O edge and no list edge"; rule 5's naming-member sentence reordered; the §4.13 phase-3 list names the new warning; `configuration.md` reflowed. Its high-confidence item, a planned row `naming.to-one.exclude-referenced-table` that no sub-item tests, is dropped (`gen.manual.exclude-relationships` covers base names) and the test gap is a backlog line.
- **Backlog:** removed the same-name `FKOnTarget` line (design §3.4 item 2; 36.2 fixes it); added the to-one exclude-by-referenced-table test gap.
- **Checks:** `make req-check`, `make refs-check`, `make tracker-check` and `make check` pass. No `check-examples` trigger (docs only).
- **Verify (2026-11-20):** PRD 10/10 per the independent reviewer; applied its doc fixes (§5.3 rule 4's three-FK table also gives three belongs-to edges; `configuration.md` "renames the list edges"). Its four questions, ruled by the user 2026-11-20:
  1. A `column_map` override renaming a column onto a detected edge's field stays the `column_map` error. The new §4.13 row (and §8.5, `configuration.md`, `collision.detected-vs-column`) covers only a field no override introduced. `TestColumnNameOverride_CollidesWithRelationshipField` and 36.4's `column_map`-message guard stay unchanged.
  2. `exclude_relationships` matching a to-one edge by its referenced table can drop both edges of a self-referential FK or of two tables referencing each other; documented in §5.3 rule 6 and `configuration.md`, with a declared takeover keeping either.
  3. Rule 6 does not apply rule 5's column fallback: two to-one edges renamed onto column fields are both omitted with the warning, which names the fix. No text change.
  4. 36.5's Tests Required gains a self-referential `ShapeSourceParity` test for `detect.belongs-to.self.source-parity`, and a task to update the citations its rename makes stale (user-authorized edit to 36.5).
- **Re-verify (2026-11-20):** all four rulings applied (reviewer 4/4, no FAIL). Applied its doc fixes: rule 6 "one table's name"; §13.4's `exclude_relationships` sentence names the base-name and referenced-table matches; `configuration.md` reflow; the stale-citation item moved to 36.5's Tasks.

## 36.1 Regression guard — a hidden FK column keeps its relationship

**PRD Reference:** Section 32

**Status:** Complete

### Tasks

- [x] Write the §32 guard on a declared belongs-to whose FK column is classified `internal`: the GraphQL type omits the id, and the relationship field stays.

### Acceptance Criteria

- The guard passes on current code, before any change.

### Tests Required

- [x] `unit: cmd/sqlgen/gen: TestAccess_HiddenForeignKeyKeepsItsRelationship` (a classified FK column hides the id while its relationship object stays exposable, PRD §32): passes on current code before any change. All dialects.

### Completion Record

- **Completed:** 2026-11-20. `make check` and `make check-examples` green; `/verify 36.1` PRD 1/1, tests 1/1, reviewer agreement full, no FIX logged.
- **Files:** `cmd/sqlgen/gen/api_access_test.go` (new guard); `docs/conformance/cli/access-control.md` (`gen.access.relationship-fk-hidden` row now cites the new guard alongside the existing one).
- **Contract run (before any change):** `go test ./cmd/sqlgen/gen -run TestAccess_HiddenForeignKeyKeepsItsRelationship` passes on postgres, mysql and sqlite. The guard parses real DDL per dialect (`parseDDLForTest` runs no detection, so the only edge is the declared `Company` o2o), asserts a `public` control first (`companyID:` and `company: Company` present), then with `company_id` `internal` asserts `type User` and the walker drop `companyID` and keep `company`.
- **Note:** design §6.2 listed this behavior as unguarded ("none found"); `TestGraphQLSchema_HiddenFKKeepsRelationship` already guarded it for a detected unique FK with role `hidden`. The new guard adds the declared belongs-to, the `internal` role and all three dialects. No production code changed.

## 36.2 Gen — FK side from the key's holder

**PRD Reference:** Sections 13.1, 13.4

**Status:** Not Started

### Tasks

- [ ] `inferredFKOnTarget` (`cmd/sqlgen/gen/context_table.go`) reads the side from detection for every key: the holder is the target of an O2M edge and the source of an O2O one. Drop the column-name fallback.
- [ ] `configRelationshipToContext`: for an `o2o` whose `fk` columns exist on both tables, take the side from the FOREIGN KEY constraint over those columns (holder = the table declaring it); with none, belongs-to.
- [ ] Keep `fkColumnOnTarget` for the unambiguous case and document the rule in its comment.

### Acceptance Criteria

- A self-referential belongs-to joins `target.<referenced> = this.<fk>` and loads the parent row.
- A declared `o2o` over a column named like the referenced column joins those two columns.
- A declared has-one keeps `FKOnTarget` and joins the target's FK.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_DetectedSelfReferentialBelongsToJoinsTheParent` (PRD §13.1): fails before the change, passes after. All dialects. Use a unique self-referential FK, the detected shape today.
- [ ] `example: sqlite: TestBelongsTo_SelfReferentialLoadsTheParent` (PRD §13.1): fails before, passes after. SQLite.
- [ ] `unit: cmd/sqlgen/gen: TestConfigRelationship_O2OSideFromForeignKey/self-referential` (PRD §13.1, §13.4): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestConfigRelationship_O2OSideFromForeignKey/same-name key column` (PRD §13.1, §13.4): fails before, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_chainedFKOnTarget` and `example: graphql: TestO2OJoin_HasOneDifferingPKNames` still pass.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair` still passes.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 36.3 Gen — chains walk the target's own edges

**PRD Reference:** Sections 13.2, 13.4

**Status:** Not Started

### Tasks

- [ ] `findO2ORelationships` builds the target's edges the way its own context does: exclusion by name or base name, the Phase 35 takeover (`sameKeyInferred`), and the omitted edges, so a chain never names a field the target struct lacks.
- [ ] Prefer reusing the target's built relationship contexts over a second derivation.

### Acceptance Criteria

- With a declared edge taking over a detected to-one edge, a third table's chained join reads the declared field, and the generated package builds.
- With a to-one edge excluded by its base name, no chain reaches it.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/takeover` (PRD §13.2, §13.4): fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/excluded by base name` (PRD §13.2, §4.8): fails before, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped` still passes.
- [ ] Guard: `example: tenancy: TestO2OChain_SingleQueryForChainedLoad`, `TestO2OChain_TenantWHEREOnEveryAlias` and `TestO2OChain_CrossTenantRowsFilteredAtEveryChainLevel` still pass.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 36.4 Gen — detected-edge collisions with a column

**PRD Reference:** Section 4.13

**Status:** Not Started

### Tasks

- [ ] In `buildRelationshipContexts`, omit a detected edge whose resolved Go field equals a column's, with a warning naming the column and the edge and pointing to declaring it under another name (PRD §13.4).
- [ ] Ungate `validateDuplicateFieldNames` for relationships: a declared relationship claiming a column's field is an error, with or without a `column_map` override. Column-vs-column stays override-gated.

### Acceptance Criteria

- `users(posts TEXT)` with a detected `posts` edge generates a package that builds, with the warning, and `User.Posts` is the column.
- A declared edge named like a column fails `validate` and `generate` with the `column_map` error's message.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedEdgeOnAColumnFieldIsOmitted` (PRD §4.13): fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/cli: TestGenerate_DetectedEdgeOnAColumnFieldWarns` (PRD §4.13): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestValidateDuplicateFieldNames/declared relationship on a column field` (PRD §4.13): fails before, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` still passes.
- [ ] Guard: the existing `column_map` collision tests still pass with their messages unchanged.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 36.5 Parser + gen — belongs-to detection and to-one naming

**PRD Reference:** Sections 5.3, 13.1, 13.4

**Status:** Not Started

### Tasks

- [ ] `parser/relationship.go`: `keyEdge` becomes `keyEdges`. A foreign key that gives an O2M edge also gives a belongs-to O2O edge on the holder, with the same `Key` and `Constraint`. A unique key and a junction are unchanged.
- [ ] `toOneName`: the naming member without `_id`, else the referenced table when the stem is empty or names a holder column. Apply it to every detected to-one edge. `BaseName` stays the referenced table.
- [ ] `disambiguateRelationshipNames`: group by `(SourceTable, TargetTable, to-one | list)`.
- [ ] `relationshipToContext`: singularize a to-one name.
- [ ] Regenerate the goldens (`make update-golden`, `make update-golden-e2e`). Check that every change is a new to-one edge, a singular rename of an old to-one field, or a takeover keeping a declared name.
- [ ] Rename the example tests that read the old plural to-one fields (graphql, mysql, postgres and tenancy examples; design §7 P2).
- [ ] Registry: update the citations the rename makes stale — `parser.rel.single-o2o-unique.{mysql,sqlite}` (`TestDetectRelationships_Dialects/…: single-column UNIQUE FK is O2O named after the referenced table`) and `parser.rel.three-fk-junction.source-parity` (`ShapeSourceParity/three single-column FKs…`) — when their subtests are renamed.

### Acceptance Criteria

- `posts.author_id → users` gives `User.Posts` and `Post.Author`, on every dialect and source.
- A self-referential FK gives `Task.ParentTask` and keeps `Task.Tasks`.
- A unique FK still gives only its belongs-to edge, and a junction only its M2M edges.
- A declared belongs-to over a detected key takes over the new detected edge; the postgres example's `audit.notes.Author` keeps its name.
- `make check` and `make check-examples` are green.

### Tests Required

- [ ] `unit: parser: TestDetectRelationships/non-unique foreign key gives a list edge and a belongs-to edge` (PRD §5.3): fails before the change, passes after. All dialects. Also `integration: parser/introspect: TestDetectRelationships_SourceParity`.
- [ ] `unit: parser: TestDetectRelationships/composite foreign key gives a belongs-to edge over every pair` (PRD §5.3): fails before, passes after. All dialects.
- [ ] `unit: parser: TestDetectRelationships/self-referential foreign key gives both edges` (PRD §5.3): fails before, passes after. All dialects.
- [ ] `unit: parser: TestDetectRelationships/three foreign keys on a non-junction give three belongs-to edges` (PRD §5.3): fails before, passes after. All dialects.
- [ ] `unit: parser: TestDetectRelationships/to-one edge named after its key column` (PRD §5.3 rule 5): fails before, passes after. All dialects.
- [ ] `unit: parser: TestDetectRelationships/to-one edge whose stem is a column takes the table name` (PRD §5.3 rule 5): fails before, passes after. All dialects.
- [ ] `unit: parser: TestDetectRelationships/self-referential list edge keeps its name beside its belongs-to edge` (PRD §5.3 rule 6): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedToOneFieldIsSingular` (PRD §5.3 rule 5, §8.5): fails before, passes after. All dialects.
- [ ] `conformance: GetMany/a detected belongs-to loads the parent row in the same query` (PRD §13.2): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected belongs-to names its constraint` (PRD §30.4.2): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/detected belongs-to over a non-unique key` (PRD §13.4): fails before, passes after. All dialects. Plus `golden: postgres`.
- [ ] `golden: graphql`: a detected belongs-to is a nullable `<camelName>: <Target>` field (PRD §26.4).
- [ ] `integration: parser/introspect: TestDetectRelationships_ShapeSourceParity/self-referential foreign key gives both edges` (PRD §5.3): files and database give identical edges for a self-referential non-unique FK; add the shape to the parity schema. Fails before, passes after. All dialects. Claims `detect.belongs-to.self.source-parity`.
- [ ] Guard: `unit: parser: TestDetectRelationships` (non-self-referential O2M names, unique FKs, junctions), `parser/introspect: TestDetectRelationships_SourceParity` and `parser: TestDetectRelationships_BothSourceParity` still pass.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_ExcludeByBaseName`, `…_DeclaredOverDetectedKeyTakesItsPlace` and `…_DeclaredOverCompositeFK` still pass.
- [ ] Guard: `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestRelationshipFilters_ListRelationshipsOnly` and `TestNestedEligibility_ListedShapeFailureIsReported/belongs-to edge` still pass.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestRelationshipsTemplate_softDeleteOnRelated`, `example: sqlite: TestO2OJoinMiss_ReadsAsNil` and `example: graphql: TestO2OJoin_GraphQLConnection` still pass.
- [ ] Guard: `unit: cmd/sqlgen/manifest: TestBuild_O2OSideDisambiguation` and `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple` still pass.
- [ ] Sweep: `make check` and `make check-examples` are green.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 36.6 Probe — `sqlgen-example`

**PRD Reference:** Section 5.3

**Status:** Not Started

### Tasks

- [ ] Regenerate `sqlgen-example` with this build in a scratch copy (`GOWORK=off`) and build it.
- [ ] Record the new to-one fields on `Task` and any new warning (design §10 item 6).

### Acceptance Criteria

- `Task.project`, `Task.reporter`, `Task.cycle` and `Task.parentTask` are generated, the app builds, and no new warning prints.

### Tests Required

- [ ] Probe: `sqlgen generate` and `go build ./...` on the scratch copy succeed.

### Completion Record

_Filled in when complete: files changed, commit references, notes._
