# Phase 36: Belongs-To Detection

Status: Complete (closed 2026-11-21)
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

**Status:** Complete

### Tasks

- [x] `inferredFKOnTarget` (`cmd/sqlgen/gen/context_table.go`) reads the side from detection for every key: the holder is the target of an O2M edge and the source of an O2O one. Drop the column-name fallback.
- [x] `configRelationshipToContext`: for an `o2o` whose `fk` columns exist on both tables, take the side from the FOREIGN KEY constraint over those columns (holder = the table declaring it); with none, belongs-to.
- [x] Keep `fkColumnOnTarget` for the unambiguous case and document the rule in its comment.

### Acceptance Criteria

- A self-referential belongs-to joins `target.<referenced> = this.<fk>` and loads the parent row.
- A declared `o2o` over a column named like the referenced column joins those two columns.
- A declared has-one keeps `FKOnTarget` and joins the target's FK.

### Tests Required

- [x] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_DetectedSelfReferentialBelongsToJoinsTheParent` (PRD §13.1): fails before the change, passes after. All dialects. Use a unique self-referential FK, the detected shape today.
- [x] `example: sqlite: TestBelongsTo_SelfReferentialLoadsTheParent` (PRD §13.1): fails before, passes after. SQLite.
- [x] `unit: cmd/sqlgen/gen: TestConfigRelationship_O2OSideFromForeignKey/self-referential` (PRD §13.1, §13.4): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/gen: TestConfigRelationship_O2OSideFromForeignKey/same-name key column` (PRD §13.1, §13.4): fails before, passes after. All dialects.
- [x] Guard: `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_chainedFKOnTarget` and `example: sqlite: TestO2OJoin_HasOneDifferingPKNames` still pass.
- [x] Guard: `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair` still passes.

### Completion Record

- **Completed:** 2026-11-20. Uncommitted at completion.
- **Files:** `cmd/sqlgen/gen/context_table.go` (`inferredFKOnTarget` reads the side from detection for every key; new `declaredFKOnTarget` and helpers; `fkColumnOnTarget` and the O2O join comment document the rule); `cmd/sqlgen/gen/o2o_fk_side_test.go` (new); sqlite example: `schema.sql` gains a `steps` table (unique self-referential `previous_step_id`), `tests/belongs_to_self_test.go` (new), `expected/` and `models/` regenerated; `docs/conformance/cli/gen.md` (`fkside.detected`, `fkside.declared` Tests cells); `docs/tracker/backlog.md` (removed the 2026-11-05 `fkColumnOnTarget` one-column-key line from 32.5, an older copy of the line 36.0 removed; fixed here).
- **Declared rule:** an `o2o` whose `fk` columns are all on the target and not all on the source stays has-one (`fkColumnOnTarget`). On both tables, the source's FOREIGN KEY over exactly those columns into the target makes it belongs-to; else the target's into the source makes it has-one; else belongs-to. A self-reference therefore reads belongs-to. `o2m` is unchanged.
- **Contract run (before the change):** new-behavior tests fail for the predicted reason on all three dialects: `TestBuildO2OJoinDetails_DetectedSelfReferentialBelongsToJoinsTheParent` (`FKOnTarget = true`, ON `this.id = target.parent_task_id`); `TestConfigRelationship_O2OSideFromForeignKey/self-referential` (same); `…/same-name key column` (`FKOnTarget = true`, key referenced `id`, constraint `""`, ON `this.id = target.code`). `example: sqlite: TestBelongsTo_SelfReferentialLoadsTheParent` against goldens generated by the old code: the second step's previous step is nil and the first step loads the second. Guards pass before and after: `TestBuildO2OJoinDetails_chainedFKOnTarget`, `TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair`, `example: sqlite: TestO2OJoin_HasOneDifferingPKNames` (the Tests Required line and design §6.2 said `graphql`; corrected), and an added guard subtest `…/has-one over a same-name key column` (a has-one whose key column exists on both tables keeps `FKOnTarget` from the target's FOREIGN KEY).
- **Golden churn:** `make update-golden-e2e` over every example moved only `sqlite`, all from the new `steps` fixture (its table files, client, graph, manifest and table-name lists). No existing golden changed.
- **Checks:** `make check` and `make check-examples` pass.
- **Review (sqlgen-reviewer):** PRD 4/4 (the three acceptance criteria plus O2M unchanged), tests 6/6, fail-first confirmed independently, no high-confidence issue, no question. Applied its doc fixes: `RelationshipContext.FKOnTarget`'s comment (`context.go`) says the target holds the key and names the two derivations; `fkColumnOnTarget`'s comment limits its say to O2M and the unambiguous O2O case. Not applied: its correction to 36.0's record. 36.0 did remove the 2026-11-10 same-name line (`git show 918cce10 -- docs/tracker/backlog.md`); the line removed here is the older 32.5 copy.
- **Verify (2026-11-20):** PRD 3/3, tests 6/6 plus two added subtests, independent reviewer agreed and confirmed fail-first on its own. Its high-confidence item: `fkside.declared` claimed "belongs-to when none does" without a test. Added `TestConfigRelationship_O2OSideFromForeignKey/no foreign key` (a self-referential `o2o` with no FOREIGN KEY), which fails before the change (`FKOnTarget = true`, ON `this.id = target.parent_task_id`) and passes after, and cited it in the row. Applied its doc fixes: `declaredFKOnTarget`'s comment covers O2M; `fkColumnOnTarget`'s doubled parenthesis; the `FKOnTarget` field comment rewrap; `gen.relationships.fk-on-target` points to `fkside.declared` for the both-tables case. The 36.0-record item is declined as above.
- **Auto-accepted (reviewer question):** a declared self-referential `o2o` meant as has-one ("the row whose parent is me") now reads belongs-to, because the source's own FOREIGN KEY wins. Kept as is: PRD §13.1 and design ruling Q4 say so, and `side:` drives only the manifest kind. No example or test declares that shape, and no golden moves.

## 36.3 Gen — chains walk the target's own edges

**PRD Reference:** Sections 13.2, 13.4

**Status:** Complete

### Tasks

- [x] `findO2ORelationships` builds the target's edges the way its own context does: exclusion by name or base name, the Phase 35 takeover (`sameKeyInferred`), and the omitted edges, so a chain never names a field the target struct lacks.
- [x] Prefer reusing the target's built relationship contexts over a second derivation.

### Acceptance Criteria

- With a declared edge taking over a detected to-one edge, a third table's chained join reads the declared field, and the generated package builds.
- With a to-one edge excluded by its base name, no chain reaches it.

### Tests Required

- [x] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/takeover` (PRD §13.2, §13.4): fails before the change, passes after. All dialects.
- [x] `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/excluded by base name` (PRD §13.2, §4.8): fails before, passes after. All dialects.
- [x] Guard: `unit: cmd/sqlgen/gen: TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped` still passes.
- [x] Guard: `example: tenancy: TestO2OChain_SingleQueryForChainedLoad`, `TestO2OChain_TenantWHEREOnEveryAlias` and `TestO2OChain_CrossTenantRowsFilteredAtEveryChainLevel` still pass.

### Completion Record

- **Completed:** 2026-11-20. Uncommitted at completion.
- **Files:** `cmd/sqlgen/gen/context_table.go` (`findO2ORelationships` returns the O2O subset of `buildRelationshipContexts` for the target; `chainedRelInfo` removed); `cmd/sqlgen/gen/chain_target_edges_test.go` (new); `cmd/sqlgen/gen/context_test.go` (the `chainedFKOnTarget` comment no longer describes the old direct-constructor path); `docs/conformance/cli/gen.md` (`chain.matches-target` Tests cell).
- **Reuse:** the chain calls the same `buildRelationshipContexts` the target's own context calls, so exclusion by name or base name, the takeover, and every omitted edge (including 36.4's, when it lands there) apply with no second derivation. The target's built context is not read directly: tables build in schema order, so a chain can reach a target not built yet. An error on the target yields no chain; the target's own build reports it.
- **Contract run (before the change):** `TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/takeover` fails on all three dialects with the chain reading `fo.Posts.Users` beside `fo.Posts.Writer`; `…/excluded by base name` fails on all three with `fo.Posts.Author` and `fo.Posts.Editor` chained though `posts` excludes `users`. Guard `TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped` passes before and after.
- **Golden churn:** none. `make update-golden-e2e` and `make update-golden` moved no file.
- **Checks:** `make check` passes; `make check-examples` (triggered by `cmd/sqlgen/gen/**`) passes, including the tenancy `TestO2OChain_*` guards.
- **Review (sqlgen-reviewer):** PRD 2/2, tests 4/4, fail-first confirmed independently, no high-confidence issue, no question. Noted that chained edges now come in the target's own `Name` order (schema order, declared last, before); no example has a chained target with two or more O2O edges, so no golden moved. Applied its doc fixes: `findO2ORelationships`' comment shortened; one blank line before 36.4; STATUS.md's 27.1 note on `findO2ORelationships` put in the past tense.
- **Verify (2026-11-20):** PRD 3/3, tests 4/4, independent reviewer agreed and confirmed fail-first on its own. Added guard subtest `…/excluded by name` (passes before and after: the old code already excluded by `Name`) and cited it, `TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped/fk column excluded` and `…/target excluded` in `chain.matches-target`, so each clause of the row has a test. No FIX logged.

## 36.4 Gen — detected-edge collisions with a column

**PRD Reference:** Section 4.13

**Status:** Complete

### Tasks

- [x] In `buildRelationshipContexts`, omit a detected edge whose resolved Go field equals a column's, with a warning naming the column and the edge and pointing to declaring it under another name (PRD §13.4).
- [x] Ungate `validateDuplicateFieldNames` for relationships: a declared relationship claiming a column's field is an error, with or without a `column_map` override. Column-vs-column stays override-gated.

### Acceptance Criteria

- `users(posts TEXT)` with a detected `posts` edge generates a package that builds, with the warning, and `User.Posts` is the column.
- A declared edge named like a column fails `validate` and `generate` with the `column_map` error's message.

### Tests Required

- [x] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedEdgeOnAColumnFieldIsOmitted` (PRD §4.13): fails before the change, passes after. All dialects.
- [x] `unit: cmd/sqlgen/cli: TestGenerate_DetectedEdgeOnAColumnFieldWarns` (PRD §4.13): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/gen: TestValidateDuplicateFieldNames/declared relationship on a column field` (PRD §4.13): fails before, passes after. All dialects.
- [x] Guard: `unit: cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` still passes.
- [x] Guard: the existing `column_map` collision tests still pass with their messages unchanged.

### Completion Record

- **Completed:** 2026-11-20. Uncommitted at completion.
- **Files:** `cmd/sqlgen/gen/context_table.go` (new `omitDetectedOnColumnFields`, called from `buildRelationshipContexts` after the declared merge; `validateDuplicateFieldNames` reports a column and a relationship on one field with no override gate, two columns still only when an override introduced the pair; `validateResolvedFieldNames`' comment); `cmd/sqlgen/gen/column_field_collision_test.go` (new); `cmd/sqlgen/cli/column_field_collision_test.go` (new); `cmd/sqlgen/gen/chain_target_edges_test.go` (subtest `omitted on a column field`); `cmd/sqlgen/gen/filter_relationship_test.go`, `cmd/sqlgen/gen/relfilter_static_test.go` (also two stale case comments), `cmd/sqlgen/gen/context_table_test.go` (comment), `cmd/sqlgen/gen/export_test.go` (`WireRelationshipFiltersForTest`); `docs/conformance/cli/{config.md,gen.md,naming.md}`.
- **Omission:** a detected edge is dropped when its Go field is the unoverridden field of a column that survives `exclude_columns`. It runs after the merge, so an edge a declared one took over warns nothing. A field a `column_map` override introduced keeps the edge and stays the `column_map` error (36.0 ruling 1). The chain reads the same `buildRelationshipContexts`, so an omitted edge is never chained.
- **Contract run (before the change):** new-behavior tests fail for the predicted reason on all three dialects: `TestBuildRelationshipContexts_DetectedEdgeOnAColumnFieldIsOmitted` (users keeps relationship `posts` on `Posts`, no warning); `TestGenerate_DetectedEdgeOnAColumnFieldWarns` (no warning on validate or generate, `User` declares `Posts` twice, `go build`: `Posts redeclared`); `TestValidateDuplicateFieldNames/declared relationship on a column field` (no error); `TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/omitted on a column field` (chain reads `fo.Posts.Users`). Guards pass before and after: `TestValidateResolvedFieldNames_DuplicateRelationshipField`, `TestColumnNameOverride_CollisionRejected`, `TestColumnNameOverride_CollidesWithRelationshipField` (message unchanged), `TestFieldNameCollision_OnlyOverrideIntroduced`, and the added subtests `TestValidateDuplicateFieldNames/{declared relationship on its own field,column override onto a declared relationship's field,two columns on one field with no override}`, and `TestBuildRelationshipContexts_DeclaredUnderAnotherNameKeepsTheEdge` (a declared edge over the key under another name takes over with no warning).
- **Fixtures that declared the rejected shape:** two tests declared an edge on a column's field and expected generation to succeed. `TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` (PRD §11.1's "the column wins" skip, `gen.filter.member-column-wins`) now declares `Emails`, renames the built edge onto `Email` and reruns the filter pass through `WireRelationshipFiltersForTest`, since config can no longer reach the skip; it still fails with the skip disabled. `TestRelationshipFilter_UnreadableListFilterFailsGeneration/o2m whose field name a column takes` is removed (the shape is now the `column_map` error before any filter is read) and its citation dropped from `gen.subcat.malformed-filter-fails`. The remaining cases all get a member (a composite-PK parent correlates on every pair, a view target has its own filter), so the row's no-member leg is now `TestRelationshipFilter_UnreadableFilterFailsWithoutAMember`, which renames a built edge onto the `name` column's field and reruns the pass; it fails with the up-front filter parse disabled. Auto-accepted (`/fix` auto-accept rule): no golden churn, no public surface (a test-only export), and it follows the PRD §4.13 rule rather than reversing one.
- **Golden churn:** none. `make update-golden-e2e` and `make update-golden` moved no file.
- **Checks:** `make check` and `make check-examples` (triggered by `cmd/sqlgen/gen/**`) pass.
- **Review (sqlgen-reviewer):** PRD 3/3, tests 5/5, fail-first confirmed independently (neutralizing only the omission call fails the new test with the `column_map` error). Its high-confidence item: the record's claim that the composite-PK and view cases cover an edge with no member was false; added `TestRelationshipFilter_UnreadableFilterFailsWithoutAMember` and cited it in `gen.subcat.malformed-filter-fails`. Applied its doc fixes: `validateDuplicateRelationshipFields`' and `TestValidateResolvedFieldNames_DuplicateRelationshipField`'s comments no longer call validateDuplicateFieldNames override-gated for relationships; the reworked filter test's comment drops "no longer"; two stale case comments in `TestRelationshipFilter_UnreadableListFilterFailsGeneration`.
- **Auto-accepted (reviewer questions, `/fix` auto-accept rule: no golden churn, no public surface, no PRD or ruling reversed):** (1) PRD §11.1's "the column wins" sentence and `gen.filter.member-column-wins` stay; config no longer reaches the filter skip, so it is defensive, and the reworked test still pins it. (2) A declared edge on a column's field reports under `tables.<t>.column_map:` though no `column_map` entry exists; the acceptance criteria and the §4.13 row require the `column_map` error's message.
- **Verify (2026-11-20):** PRD 3/3, tests 5/5, independent reviewer agreed and confirmed fail-first on its own (restoring the override gate fails `TestValidateDuplicateFieldNames/declared relationship on a column field` on all dialects). No FIX logged. Added guard `TestBuildRelationshipContexts_DeclaredUnderAnotherNameKeepsTheEdge` (passes before and after; it fails if the omission ran before the merge) and cited it and `TestColumnNameOverride_CollidesWithRelationshipField` in `collision.detected-vs-column`. Applied its doc fixes: `validateDuplicateRelationshipFields`' comment reflowed; this record names the added guard.
- **Auto-accepted (reviewer question, `/fix` auto-accept rule: no code change, no golden churn, no public surface, no PRD text or ruling reversed):** an identity override (`column_map.posts.name: Posts`) counts as an override, so a detected edge on that field is the `column_map` error, not the omission. Kept: 36.0 ruling 1 keys on the override being present, and the error is the louder outcome.

## 36.5 Parser + gen — belongs-to detection and to-one naming

**PRD Reference:** Sections 5.3, 13.1, 13.4

**Status:** Complete

### Tasks

- [x] `parser/relationship.go`: `keyEdge` becomes `keyEdges`. A foreign key that gives an O2M edge also gives a belongs-to O2O edge on the holder, with the same `Key` and `Constraint`. A unique key and a junction are unchanged.
- [x] `toOneName`: the naming member without `_id`, else the referenced table when the stem is empty or names a holder column. Apply it to every detected to-one edge. `BaseName` stays the referenced table.
- [x] `disambiguateRelationshipNames`: group by `(SourceTable, TargetTable, to-one | list)`.
- [x] `relationshipToContext`: singularize a to-one name.
- [x] Regenerate the goldens (`make update-golden`, `make update-golden-e2e`). Check that every change is a new to-one edge, a singular rename of an old to-one field, or a takeover keeping a declared name.
- [x] Rename the example tests that read the old plural to-one fields (graphql, mysql, sqlite and tenancy examples; design §7 P2).
- [x] Registry: update the citations the rename makes stale — `parser.rel.single-o2o-unique.{mysql,sqlite}` (`TestDetectRelationships_Dialects/…: single-column UNIQUE FK is O2O named after the referenced table`) and `parser.rel.three-fk-junction.source-parity` (`ShapeSourceParity/three single-column FKs…`) — when their subtests are renamed.

### Acceptance Criteria

- `posts.author_id → users` gives `User.Posts` and `Post.Author`, on every dialect and source.
- A self-referential FK gives `Task.ParentTask` and keeps `Task.Tasks`.
- A unique FK still gives only its belongs-to edge, and a junction only its M2M edges.
- A declared belongs-to over a detected key takes over the new detected edge; the postgres example's `audit.notes.Author` keeps its name.
- `make check` and `make check-examples` are green.

### Tests Required

- [x] `unit: parser: TestDetectRelationships/non-unique foreign key gives a list edge and a belongs-to edge` (PRD §5.3): fails before the change, passes after. All dialects. Also `integration: parser/introspect: TestDetectRelationships_SourceParity`.
- [x] `unit: parser: TestDetectRelationships/composite foreign key gives a belongs-to edge over every pair` (PRD §5.3): fails before, passes after. All dialects.
- [x] `unit: parser: TestDetectRelationships/self-referential foreign key gives both edges` (PRD §5.3): fails before, passes after. All dialects.
- [x] `unit: parser: TestDetectRelationships/three foreign keys on a non-junction give three belongs-to edges` (PRD §5.3): fails before, passes after. All dialects.
- [x] `unit: parser: TestDetectRelationships/to-one edge named after its key column` (PRD §5.3 rule 5): fails before, passes after. All dialects.
- [x] `unit: parser: TestDetectRelationships/to-one edge whose stem is a column takes the table name` (PRD §5.3 rule 5): fails before, passes after. All dialects.
- [x] `unit: parser: TestDetectRelationships/self-referential list edge keeps its name beside its belongs-to edge` (PRD §5.3 rule 6): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedToOneFieldIsSingular` (PRD §5.3 rule 5, §8.5): fails before, passes after. All dialects.
- [x] `conformance: GetMany/a detected belongs-to loads the parent row in the same query` (PRD §13.2): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected belongs-to names its constraint` (PRD §30.4.2): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/detected belongs-to over a non-unique key` (PRD §13.4): fails before, passes after. All dialects. Plus `golden: postgres` (vacuous: `audit.notes.Author` has no FOREIGN KEY under it; see the Completion Record).
- [x] `golden: graphql`: a detected belongs-to is a nullable `<camelName>: <Target>` field (PRD §26.4).
- [x] `integration: parser/introspect: TestDetectRelationships_ShapeSourceParity/self-referential foreign key gives both edges` (PRD §5.3): files and database give identical edges for a self-referential non-unique FK; add the shape to the parity schema. Fails before, passes after. All dialects. Claims `detect.belongs-to.self.source-parity`.
- [x] Guard: `unit: parser: TestDetectRelationships` (non-self-referential O2M names, unique FKs, junctions), `parser/introspect: TestDetectRelationships_SourceParity` and `parser: TestDetectRelationships_BothSourceParity` still pass.
- [x] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_ExcludeByBaseName`, `…_DeclaredOverDetectedKeyTakesItsPlace` and `…_DeclaredOverCompositeFK` still pass.
- [x] Guard: `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` still passes.
- [x] Guard: `unit: cmd/sqlgen/gen: TestRelationshipFilters_ListRelationshipsOnly` and `TestNestedEligibility_ListedShapeFailureIsReported/belongs-to edge` still pass.
- [x] Guard: `unit: cmd/sqlgen/gen: TestRelationshipsTemplate_softDeleteOnRelated`, `example: sqlite: TestO2OJoinMiss_ReadsAsNil` and `example: graphql: TestO2OJoin_GraphQLConnection` still pass.
- [x] Guard: `unit: cmd/sqlgen/manifest: TestBuild_O2OSideDisambiguation` and `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple` still pass.
- [x] Sweep: `make check` and `make check-examples` are green.

### Completion Record

- **Completed 2026-11-20.** Uncommitted at completion. `make check` and `make check-examples` green after the verify doc fixes; no FIX logged.
- **Files:** `parser/relationship.go` (`keyEdges` gives the belongs-to beside the O2M edge; `toOneName`; rule 6 groups by `(source, target, to-one | list)`); `cmd/sqlgen/gen/context_table.go` (`relationshipToContext` singularizes a to-one name). Tests: `parser/relationship_test.go` (seven new all-dialect cases, belongs-to edges added to the existing wants), `parser/relationship_dialects_test.go`, `parser/introspect/relationship_{parity,shapes_parity}_test.go` (new self-referential shape), `cmd/sqlgen/gen/to_one_naming_test.go` (new), `cmd/sqlgen/gen/context_table_test.go` (takeover subtest with a no-declaration control), `cmd/sqlgen/manifest/relationship_key_test.go`, `cmd/sqlgen/gen/api_access_test.go` (detected variant of the §32 guard), conformance `suite/get.go`; to-one renames in `cmd/sqlgen/{gen,cli,manifest}` tests and in the graphql, mysql, sqlite, tenancy example tests and conformance `suite/composite_key.go`. Registry: Tests cells for every planned 36.5 row (`parser/schema.md`, `cli/gen.md`, `cli/api-schema.md`, `cli/manifest.md`); stale citations renamed (`…single-column UNIQUE FK is O2O alone`, `…three O2M and three belongs-to edges`); `naming.reserved-field-{columns,hasselectedcolumns}.field.relationship` cite the declared case. Goldens: every example's `expected/` and `models/`, every conformance `models/`.
- **Contract run (before the change):** every new parser case fails on all three dialects with the belongs-to edge missing (and the unique `profiles_owner` edge named `users`, not `owner`); with `keyEdges` applied but rule 6 still kind-agnostic, `…self-referential list edge keeps its name…` fails with the list edge renamed `parent_categories`, the predicted reason. `ShapeSourceParity/self-referential foreign key gives both edges` fails on all three dialects for the missing O2O. With the parser and gen changes stashed: `TestBuildRelationshipContexts_DetectedToOneFieldIsSingular` (`users`/`Users`, or no edge), `…_DeclaredOverDetectedKeyTakesItsPlace/detected belongs-to over a non-unique key` (control: no detected edge on posts) and `TestBuild_RelationshipKeyShape/detected belongs-to names its constraint` (no relationship `author`) fail; the conformance subtest cannot compile at HEAD (`Post` has no `Author`). Guards (`TestDetectRelationships` existing cases, `SourceParity`, `BothSourceParity`, `ExcludeByBaseName`, `DeclaredOverDetectedKeyTakesItsPlace`, `DeclaredOverCompositeFK`, `TestGenerate_DetectedNameCollisionIsAnError`, `RelationshipFilters_ListRelationshipsOnly`, `NestedEligibility_…/belongs-to edge`, `softDeleteOnRelated`, `O2OSideDisambiguation`, `DispatchFindReferencingDedupsByFullTuple`, the sqlite/graphql example guards) pass after; the parser guards' wants gain only belongs-to edges and singular to-one renames (diffed case by case).
- **Golden churn:** checked mechanically (relationship struct fields, GraphQL fields, manifest names): every change is a new `*Target` to-one field, a singular rename of an old to-one field (`Users`→`User`, `Tasks`→`Task`, `Owners`→`Owner`, sqlite `Steps`→`PreviousStep`, stem-named), and its join, field-options, walker and manifest consequences. No list edge moved in any example. postgres `audit.notes.Author` keeps its name: it is declared over a column with no FOREIGN KEY, so no detected edge lies under it, and the takeover is pinned by the unit subtest alone. New fallback-named edges: postgres `Warehouse.Warehouse` (stem `nullable_ref` names its column), conformance `ItemReceipt.OrderItem` (no naming member). postgres_stdlib now also warns that `tasks`' belongs-to `cycle` is omitted with its `exclude_columns`-removed key, as its list edge already was.
- **Consequence of rule 6 by cardinality (PRD §5.3):** a list edge that shared a group only with a to-one edge is no longer renamed: `BothSourceParity`'s M2M `labels → tasks` is `tasks` again, not `task_labels` (it shared the pair with the unique `owner_task` O2O). No example has the shape.
- **Fixture adjustments:** the to-one name is singular, so a detected to-one can no longer derive `Columns` / `HasSelectedColumns`; those two reserved-name matrix cases now declare the edge. The time-key refusal tests exclude the belongs-to (`exclude_relationships: [stamps]`) where they pin the list edge's message, and a new case pins the belongs-to's; `chain …/omitted on a column field` uses a fallback name (`writers` → `Writer` beside a `writer` column), since a stem no longer collides.
- **36.1 reviewer carry-forward:** `TestAccess_HiddenForeignKeyKeepsItsRelationship` gains `/detected` subtests (a detected non-unique FK whose column is `internal` keeps `company: Company`), passing.
- **Checks:** `make check` and `make check-examples` green; `parser/introspect` integration tests green.
- **Review (sqlgen-reviewer):** PRD 6/6, tests 19/19, fail-first confirmed independently (dropping the belongs-to from `keyEdges` fails the non-unique case on every dialect), no high-confidence issue, no doc correction. Its questions: (2) rule 6 renaming a fallback-named to-one edge onto a column, which is then omitted with the warning, is 36.0 ruling 3, unchanged. (1) Two to-one edges from one column into two tables sharing a stem stay the PRD §5.3 rule 6 generation error, and (3) `exclude_relationships` does not match a to-one edge's singular generated name, as rule 6 lists only the name, the pre-rename name and the referenced table: both auto-accepted as is (`/fix` auto-accept rule: gates green, no golden churn against the alternative, no public surface, follows the PRD). The error's "after disambiguation" wording, the exclude asymmetry and the `loader.belongs-to.detected` coverage (GetMany only) are backlog lines.
- **Verify (2026-11-20):** PRD 5/5, tests 20/20, the independent reviewer agreed and confirmed fail-first on its own (dropping the `toSingular` branch fails `…DetectedToOneFieldIsSingular` on every dialect). No FIX logged. It refuted the postgres-golden evidence for the takeover: `audit.notes` has no FOREIGN KEY, so `Author` holds vacuously and only the unit subtest pins the takeover. Applied its doc fixes: `golden: postgres` dropped from `takeover.belongs-to.detected` and the design §6.1 row; this record's golden-churn line; the rename task names sqlite, not postgres; `naming.to-one.fallback.postgres` also cites the no-naming-member case; the `relationshipToContext` and `to_one_naming_test.go` comments; PRD §4.8's `exclude_relationships` row no longer calls the rule-5 name table-derived (rehashed §4 and §4.8, rows reviewed, they hold). Q1–Q3 confirmed as recorded above; on Q3 the reviewer notes that the list path matches the plural generated name for the reason the to-one path does not, which stays the backlog line.

## 36.6 Probe — `sqlgen-example`

**PRD Reference:** Section 5.3

**Status:** Complete

### Tasks

- [x] Regenerate `sqlgen-example` with this build in a scratch copy (`GOWORK=off`) and build it.
- [x] Record the new to-one fields on `Task` and any new warning (design §10 item 6).

### Acceptance Criteria

- `Task.project`, `Task.reporter`, `Task.cycle` and `Task.parentTask` are generated, the app builds, and no new warning prints.

### Tests Required

- [x] Probe: `sqlgen generate` and `go build ./...` on the scratch copy succeed.

### Completion Record

- **Completed 2026-11-20.** Uncommitted at completion. Probe only: no source, test or golden file changes.
- **Harness:** detached worktrees of HEAD `9fb3f22e` and of `main` (`f5dac70c`, before this phase) as a baseline; a CLI built from each; two `rsync` copies of `sqlgen-example` (at `35d4d5d`, pinned to v0.1.2) with `go mod edit -replace` of `sqlgen`, `cache/redis`, `event/natsbus` and `metrics/otel` onto the matching worktree; `GOWORK=off` for every step in the example copies (each CLI was built inside its worktree's workspace). The sibling repo was not touched, and the copies and worktrees were deleted afterwards.
- **Result:** `sqlgen generate` (with the inline gqlgen step), `go build ./...`, `go vet ./...` and `go test -short ./...` succeed on the HEAD copy. `Task` gains `Project *Project`, `Reporter *User`, `Cycle *Cycle`, `ParentTask *Task` and `Workspace *Workspace`; GraphQL `Task` gains `project`, `reporter`, `cycle`, `parentTask` and `workspace`. `Subtasks`, `Tasks`, `DependsOnTasks`, `Assignees`, `Watchers` and the other list edges are unchanged.
- **Warnings:** both builds print only `CREATE VIEW project_stats skipped in DDL file`. No new warning.
- **Other tables:** every relationship-field change is a new to-one edge (each tenanted table's `Workspace`, `Comment.Author`/`Task`, `Activity.Actor`, `Cycle.Team`, `Project.Team`, `TimeEntry.Task`/`User`, and `User` on `api_tokens`, `attachments` and `invitations`) or the singular rename `WorkspaceSetting.Workspaces` → `Workspace`. No hand-written code in the app reads the old name, so it builds unchanged. Besides the harness's own `go.mod`/`go.sum` replace edits, only `_gen` files, the manifest, and `.mcp.json`'s generator-version stamp (`dev`, the same in the baseline) differ.
- **Checks:** `make check` green; `make check-examples` not triggered (tracker only).
- **Review (sqlgen-reviewer):** acceptance 3/3, probe test 1/1, no high-confidence issue, no question. Re-ran `go build` and `go vet` (and the verify pass `go test -short`) on the HEAD copy and diffed every model's relationship fields itself, confirming this record. Applied its two doc corrections: the cleanup sentence holds once the probe directory is removed, and the "only `_gen` files" line now names the harness's `go.mod`/`go.sum` edits.
- **Verify (2026-11-20):** acceptance 3/3, probe test 1/1; `make check` reused (diff fingerprint unchanged since the gate), `make check-examples` not triggered; `req-check` green, no registry row for the probe (design §9 lists none). The independent reviewer reproduced the probe on a fresh copy (identical output but the manifest timestamp) and agreed in full. No FIX logged. Applied its doc fixes: the `GOWORK=off` wording and the worktree cleanup (`git worktree remove`); the STATUS.md "Next" line moves at `/done`.
