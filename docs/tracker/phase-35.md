# Phase 35: Renaming a Detected Relationship

Status: In Progress
PRD Sections: 4.8, 4.13, 5.3, 13.4, 30.4.2

> **Design.** `docs/design/DETECTED_EDGE_RENAME.md`, approved 2026-11-20, is the source for every
> sub-item below. Its §9 lists sub-items N.0–N.3, which are 35.0–35.3 here. Its §6 behavior
> contract sets the Tests Required, and its §11 records rulings Q1–Q4:
> - Q1: any same-key declared edge takes over, whatever its name. The `sqlite` golden loses
>   `BinaryKeyEvents`.
> - Q2: a takeover reports no warning.
> - Q3: a `filter:` / `discriminator:` edge over a real FOREIGN KEY records its constraint. It
>   never takes over.
> - Q4: keys match as a set of column pairs, in any order. The manifest lists the declared `fk`
>   order.
>
> The source is `sqlgen-example` v0.1.2 finding #5. The phase adds no config key. A prototype is at
> `.claude/prototypes/DETECTED_EDGE_RENAME.patch` (design §7, P2). Start 35.2 and 35.3 from it.
>
> **Ground rules for every sub-item:**
> - **Fail first.** Each new behavior test is seen failing on the code before its change. Record it
>   in the Completion Record.
> - **The declared context is used unchanged.** A takeover generates exactly what the
>   exclude-and-declare recipe generates today, plus `constraint`. Go golden churn is limited to
>   the `sqlite` `BinaryKeyEvents` removal (35.3). Manifest golden churn is limited to `constraint`
>   lines in `graphql`, `mysql`, `postgres` and `sqlite` (35.2; `sqlite` gets the unnamed-key label).
> - **Findings** go through `/fix` → Findings triage.

## 35.0 PRD sync — constraint naming and takeover in §4.8, §4.13, §13.4, §30.4.2

**PRD Reference:** Sections 4.8, 4.13, 5.3, 13.4, 30.4.2

**Status:** Complete

### Tasks

- [x] §13.4, "A manual relationship joins the referenced table's primary key" paragraph: replace the "has no FOREIGN KEY constraint behind it" sentence with design §8's constraint-naming wording.
- [x] §13.4: add design §8's "A manual edge over a detected edge's key is that edge, renamed" paragraph after "Auto-detected relationships can be disabled…".
- [x] §13.4, "Detection can leave an edge to be declared": replace the collision recipe with design §8's wording.
- [x] §4.13, "Two detected relationships keep one name" row: add "(including one a declared edge takes over, [13.4])".
- [x] §30.4.2, the `constraint` and `columns` bullets: design §8's wording.
- [x] §4.8, the `relationships` row: add the takeover sentence.
- [x] `docs/configuration.md` → Relationships, "Renaming a detected edge": a single declaration over the same key renames the edge and keeps its `constraint`. Drop `exclude_relationships: [task_assignees]` from the example.
- [x] Registry: narrow `gen.manual.replaces-inferred` to edges whose key differs, and `manifest.json.rel.constraint-manual-omitted` to "a manual relationship with no FOREIGN KEY behind it". Add the design §6.1 rows, with status planned.
- [x] `docs/design/COMPOSITE_FK_RELATIONSHIPS.md` §6.2 and §7.1: add a pointer to this design where they describe manual replacement.
- [x] Run `make req-check`. Review and `make req-rehash SECTIONS=4.8,4.13,13.4,30.4.2`.

### Acceptance Criteria

- The PRD states:
  - a manual edge names the FOREIGN KEY its key is, matching column pairs in any order, with the unnamed-SQLite label;
  - `constraint` is omitted only when no FOREIGN KEY declares the key;
  - an unfiltered manual edge over a detected edge's key (same kind, target and pair set; for `m2m`, the same junction and both keys) takes its place, with no exclusion and no warning;
  - a `filter:` / `discriminator:` edge never takes over.
- `make req-check` and `make refs-check` pass. No code changes.

### Tests Required

- [x] `make req-check` clean, with every new row planned (empty Tests cell; design §6.1 holds the planned test name).

### Completion Record

**Completed 2026-11-20.** Docs only, no code.

- **PRD:** §13.4 gains design §8's constraint-naming sentence, the "A manual edge over a detected edge's key is that edge, renamed" paragraph and the new collision recipe. §4.13's residual-collision row counts a taken-over edge as replaced. §30.4.2's `constraint` and `columns` bullets carry design §8's wording. §4.8's `relationships` row gains the takeover sentence.
- **`docs/configuration.md`:** "Renaming a detected edge" now says a single declaration over the same key renames the edge and keeps its `constraint`. The `exclude_relationships: [task_assignees]` line is gone from the example.
- **Registry:**
  - `cli/gen.md` §13.4: `gen.manual.replaces-inferred` narrowed to a manual edge over a different key. It drops the two `DeclaredReplacesInferred` cases that 35.3 flips (`declared name collides with inferred edge`, `declared name distinct from inferred edge`); they move to `rename.takeover.no-warning` and `rename.takeover.distinct-name` in 35.3. Fifteen planned rows: `rename.constraint.{o2m,belongs-to,has-one,m2m,composite,pairs-not-set,filtered}` and `rename.takeover.{o2m,belongs-to,m2m,composite,self-m2m,no-warning,distinct-name,only-same-key}`, with design §6.3's matrix cells.
  - `cli/manifest.md` §30.4.2: `manifest.json.rel.constraint-manual-omitted` narrowed to a manual relationship with no FOREIGN KEY behind it; planned row `rename.constraint.unnamed-sqlite` (sqlite).
  - `cli/config.md` §4.13: planned row `rename.takeover.clears-collision` (`config-validation: relationship-name-duplicate × fires`).
  - All seventeen have empty Tests cells, as 34.0's planned rows do: `reqcheck` resolves every cited test, so a name that doesn't exist yet can't be cited. Design §6.1 holds each row's planned test name.
  - Reviewed the existing rows under §4, §4.8, §4.13, §13.4, §30, §30.4 and §30.4.2 against the new text; they still hold. Rehashed those seven anchors.
- **Design:** `COMPOSITE_FK_RELATIONSHIPS.md` §6.2 (Consumers) and §7.1 point to `DETECTED_EDGE_RENAME.md`. The task and design §8 said §3, which has no manual-replacement statement; both are corrected.
- **Review (sqlgen-reviewer):** PRD 5/5, no defects. Applied its inline fixes: §13.4's field-name paragraph is now the "otherwise" case, over another key; §4.8's row states the takeover first; `configuration.md` says keeping both edges also needs a different name; `…/exclude_relationships alongside the declaration` moved from `gen.manual.replaces-inferred` to `gen.manual.exclude-relationships`; the Tests Required wording; the design pointer above. Rehashed §4, §4.8 and §13.4 again.
- **Checks:** `make req-check`, `make refs-check` and `make tracker-check` pass.

## 35.1 Regression guards — coexistence and a fixture move

**PRD Reference:** Sections 13.4, 13.7.3

**Status:** Complete

### Tasks

- [x] Write `TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists` (`cmd/sqlgen/gen`): a declared `filter:` edge and a declared `discriminator:` edge over a detected FOREIGN KEY are each emitted alongside the detected edge.
- [x] Write `TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists` (`cmd/sqlgen/gen`): a declared `o2o` over the same columns as a detected `o2m` is emitted alongside it.
- [x] Move the `TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` fixture's declared `Email` edge onto a key no FOREIGN KEY declares, so the takeover in 35.3 doesn't remove its `Orders` control. The assertion is unchanged.

### Acceptance Criteria

- Both new tests and the moved fixture pass on today's code, before any Phase 35 change.

### Tests Required

- [x] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists`: passes on current code before any change.
- [x] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists`: passes on current code before any change.
- [x] `unit: cmd/sqlgen/gen: TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` passes with its moved fixture, on current code.

### Completion Record

**Completed 2026-11-20.** Tests and registry only, no generator code.

- **Tests (`cmd/sqlgen/gen`):**
  - `context_table_test.go`: `TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists` (`filter`, `discriminator`) declares `LedgerEntries` over the detected `entries` key with a predicate. Both fields are emitted, with no warning. `TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists` declares a has-one `o2o` `LatestEntry` over the same columns as the detected `o2m`. Both are emitted, each keeping its kind, with no warning.
  - `filter_relationship_test.go`: `TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` declares `Email` over `orders.workspace_id`, which no foreign key declares, instead of `user_id`. The assertions are unchanged.
- **Guard run (current code, before any Phase 35 code change):** all three pass (`go test ./gen/ -run '…Coexists|…ColumnComparatorKeepsItsFieldName' -v`).
- **Probe:** with the moved edge renamed `WorkspaceOrders`, it gets a relationship filter member. So `Email`'s missing member is still the collision's doing, not a key that never gets one. Reverted.
- **Registry (`cli/gen.md` §13.4):** two guard rows, `gen.manual.filtered-coexists-with-detected` and `gen.manual.other-kind-coexists-with-detected`. Design §6.2 marks both rows guarded.
- **Checks:** `make check` and `make check-examples` pass. `make check-examples` ran because the diff touches `cmd/sqlgen/gen/**`.
- **Review (sqlgen-reviewer):** PRD 3/3, no findings, no doc fixes. It checked fail-first against the prototype in a throwaway worktree. Disabling the filtered-edge early return fails `FilteredEdgeOverDetectedKeyCoexists`. Dropping the kind comparison fails `DeclaredOfAnotherKindCoexists`. Putting `Email` back on `user_id` fails `ColumnComparatorKeepsItsFieldName` on the `Orders` control, so the move is needed.

## 35.2 Gen — declared edges name their FOREIGN KEY constraint

**PRD Reference:** Sections 13.4, 30.4.2, 4.13

**Status:** Complete

### Tasks

- [x] Export `parser.ForeignKeyName(table *Table, c *Constraint) string` from `foreignKeyName` (`parser/relationship.go:148-155`), so a declared edge labels an unnamed SQLite key as detection does.
- [x] Add `Declared bool` to `RelationshipContext`, set by `configRelationshipToContext`.
- [x] Switch `validateDuplicateRelationshipFields` (`cmd/sqlgen/gen/context_table.go:995`) from `len(Constraints) > 0` to `!Declared` on both edges.
- [x] Add `declaredKeyConstraints`. For O2O / O2M (belongs-to and has-one), find the holder table's FOREIGN KEY into the referenced table whose column pairs equal the key's pairs as a set. For M2M, find the junction's FK for `JunctionLocal` and for `JunctionReference`. Return `""` where nothing matches.
- [x] Set `Constraints` from it in `configRelationshipToContext`, for every declared edge, `filter:` / `discriminator:` edges included.
- [x] Regenerate the goldens (`make update-golden`, `make update-golden-e2e`). Confirm the only changes are `constraint` lines in the `graphql`, `mysql`, `postgres` and `sqlite` manifests.

### Acceptance Criteria

- A declared edge's manifest `constraint` is the FOREIGN KEY whose column pairs equal its key, on every edge kind. An unnamed SQLite key gets the `table(columns) → target(columns)` label.
- A declared key with the same columns as a FOREIGN KEY but different pairs names no constraint. A key no FOREIGN KEY declares omits `constraint`.
- The collision error between two detected edges, and the one between two declared edges, are unchanged.
- Go goldens are byte-identical.

### Tests Required

- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over a foreign key names its constraint` (o2m): fails before the change, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual belongs-to names its constraint`: fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual has-one names its constraint`: fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual m2m names both junction constraints`: fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual composite key names its constraint` (pairs listed in another order): fails before, passes after. All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual key pairing a foreign key differently names no constraint`: passes before and after (it pins that set-of-columns is not enough). All dialects.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over an unnamed sqlite key`: fails before, passes after. SQLite.
- [x] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/discriminator edge names its constraint`: fails before, passes after. All dialects.
- [x] Guard: `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship` (no FOREIGN KEY, no `constraint`) still passes.
- [x] Guard: `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected edge names its constraint`, `…/key into a unique constraint` and `…/unnamed sqlite key` still pass.
- [x] Guard: `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` still passes.
- [x] Guard: `unit: cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` still passes.
- [x] Guard: `unit: cmd/sqlgen/gen: TestNestedEligibility_PinnedAndFreeMembers` still passes.

### Completion Record

**Completed 2026-11-20.**

- **Code:**
  - `parser/relationship.go`: `foreignKeyName` is exported as `ForeignKeyName`.
  - `cmd/sqlgen/gen/context.go`: `RelationshipContext.Declared`, and the `Constraints` doc.
  - `cmd/sqlgen/gen/context_table.go`: `configRelationshipToContext` sets `Declared` and `Constraints`. `validateDuplicateRelationshipFields` pairs two edges only when neither is declared.
  - `cmd/sqlgen/gen/key_members.go`: `declaredKeyConstraints`, `foreignKeyOver` and `samePairSet`. `samePairSet` is the pair-set match 35.3's `sameKeyInferred` reuses.
- **Tests (`cmd/sqlgen/manifest/relationship_key_test.go`):** eight cases in `TestBuild_RelationshipKeyShape`, each run per dialect as a `<case>/<dialect>` subtest. The SQLite case runs on SQLite only, and the other seven on all three dialects. Each case compares every key's `columns`, `references` and `constraint` (`relationshipOf` and `relationshipKeys` are the new helpers). The fixtures name their constraints (`CONSTRAINT posts_author …`), so the expected name is the same on every dialect.
- **Fail-first (before the code change):** seven cases fail, and every diff line is the missing `constraint` (`- Constraint: "posts_author"` / `+ Constraint: ""`). `manual key pairing a foreign key differently names no constraint` passes, as required.
- **Guards (before and after):** `TestBuild_RelationshipKeyShape/{manual relationship, detected edge names its constraint, key into a unique constraint, unnamed sqlite key}`, `TestGenerate_DetectedNameCollisionIsAnError`, `TestValidateResolvedFieldNames_DuplicateRelationshipField` and `TestNestedEligibility_PinnedAndFreeMembers` pass.
- **Goldens:** no Go golden moved, and `make update-golden` changed nothing. `make update-golden-e2e` added `constraint` lines to the manifests only:
  - `graphql` (`asset`, `user`, `workspace_note`), `mysql` and `postgres`, as design P3 measured. The generated `models/manifest` copies in each example move with their `expected/` goldens;
  - `sqlite`, five `table(columns) → target(columns)` labels on declared edges (`asset_document_links` ×2, `binary_key_events`, `owner_profiles`, `profiles`). P3 ran the prototype, which left an unnamed key `""`. Exporting the label (the first task) adds them, so the ground rule, this task and design §9 now list `sqlite`.
- **Registry:** the eight `rename.constraint.*` rows cite their tests. `rename.constraint.filtered` also cites `TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists/discriminator` for "the detected edge is still emitted", and `rename.constraint.unnamed-sqlite` also cites `golden: sqlite`. Design §6.1's Status column moves in 35.3, with the takeover rows.
- **Review (sqlgen-reviewer):** PRD 5/6, plus one partial. It confirmed fail-first: reverting to the prototype's `c.Name` fails only the unnamed-SQLite case. It also confirmed the `sqlite` churn attribution, and that every new manifest line sits on a declared edge whose key pairs match one FOREIGN KEY. Applied:
  - **Identical keys:** `declaredKeyConstraints` took the first match in declaration order, while detection keeps the identical key whose name sorts first (`firstOfIdentical`). That left the label dependent on the source, since introspectors list constraints by name. It now picks the same key: first by name, then by declaration. New case `TestBuild_RelationshipKeyShape/manual relationship over identical foreign keys names the one detection keeps` failed first on all three dialects (`posts_b` for `posts_a`) and passes now. It is cited on `rename.constraint.o2m`.
  - **Doc fixes:** `manifest/types.go`, the `FK` and `JunctionKey` docs say `Constraint` is omitted when no FOREIGN KEY declares the key.
- **Checks:** `make check` passes. The tie-break first pushed `declaredKeyConstraints` past `cyclop`'s limit of 15, so the per-table lookup moved into `foreignKeyOver`, with no suppression. `make check-examples` passes, and `TestE2EGoldenFiles` without `-update` passes on the tie-break: no golden moved beyond the regenerated manifests.
- **Verify (2026-11-20):** PRD 6/6, tests 13/13, and an independent reviewer confirmed the tie-break's fail-first (reverted, `posts_b` for `posts_a` on all three dialects). Applied:
  - `manual key pairing a foreign key differently names no constraint` is now a composite belongs-to (`tasks.Cycle`), the `composite-belongs-to × struct-field` cell its row names; it was an o2m. With the foreign key's pairing it names `tasks_cycle`, so the case can fail.
  - A stale test comment in the `composite key in declared order` case is shortened.

## 35.3 Gen — a declared edge over a detected key takes its place

**PRD Reference:** Sections 13.4, 5.3 rule 6, 4.13

**Status:** Not Started

### Tasks

- [ ] Add `sameKeyInferred` and call it from `mergeDeclaredRelationships` (`cmd/sqlgen/gen/context_table.go:1873-1956`) before the field-name rule. It finds an unreplaced detected edge that matches on all of these:
  - the same `Type`, `TargetSchema` and `TargetTable`;
  - for O2O / O2M, the same `FKOnTarget` and an equal `Key` pair set;
  - for M2M, the same junction and equal `JunctionLocal` and `JunctionReference` pair sets.

  A `filter:` / `discriminator:` edge never matches.
- [ ] On a match, mark the detected edge replaced, with no warning. Otherwise fall back to the field-name rule and its warning, as today.
- [ ] Update the two `TestBuildRelationshipContexts_DeclaredReplacesInferred` cases whose expectations change (ruling Q1 and Q2).
- [ ] Regenerate the goldens. Confirm the only Go change is `sqlite` losing `BinaryKeyEvents` (and its loader, filter member and nested surface).
- [ ] Probe `sqlgen-example`: drop its three `exclude_relationships` entries and regenerate. Confirm the struct and GraphQL surface are unchanged, and that `Subtasks`, `Assignees` and `Watchers` keep `constraint` in the manifest (design §10 item 6).
- [ ] Move each design §6.1 registry row from planned to its test.

### Acceptance Criteria

- An unfiltered declared edge with the same kind, target and key (pair set) as a detected edge replaces it under the declared name, with no `exclude_relationships` entry and no warning. For M2M, that means the same junction and both keys.
- When several detected edges join the same two tables, only the one over the declared key is replaced. The others keep their rule-6 names.
- A self-referential M2M declaration replaces only the direction whose junction keys it matches.
- When two detected edges keep one name after rule 6, declaring one of them over its key clears the collision error.
- A declared edge whose key differs, but which claims a detected edge's Go field, still replaces it with today's warning.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/o2m`: fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/belongs-to`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/m2m`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/composite o2m` (pairs in another order): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/self-referential m2m direction`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/one of several edges into one target`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/declared name collides with inferred edge` (now no warning): fails before the expectation change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/declared name distinct from inferred edge` (now one field) and `golden: sqlite`: fail before, pass after. All dialects.
- [ ] `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionClearedByDeclaringOneEdge`: fails before, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/exclude_relationships alongside the declaration` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverCompositeFK` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_ExcludeByBaseName` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestDiscriminator_SubCategorizedEdgesStayDistinct` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey` still passes.
- [ ] Guard: the 35.1 coexistence tests still pass.
- [ ] Sweep: `make check` and `make check-examples` are green.

### Completion Record

_Filled in when complete: files changed, commit references, notes._
