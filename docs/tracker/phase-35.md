# Phase 35: Renaming a Detected Relationship

Status: Not Started
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
>   lines in `graphql`, `mysql` and `postgres` (35.2).
> - **Findings** go through `/fix` → Findings triage.

## 35.0 PRD sync — constraint naming and takeover in §4.8, §4.13, §13.4, §30.4.2

**PRD Reference:** Sections 4.8, 4.13, 5.3, 13.4, 30.4.2

**Status:** Not Started

### Tasks

- [ ] §13.4, "A manual relationship joins the referenced table's primary key" paragraph: replace the "has no FOREIGN KEY constraint behind it" sentence with design §8's constraint-naming wording.
- [ ] §13.4: add design §8's "A manual edge over a detected edge's key is that edge, renamed" paragraph after "Auto-detected relationships can be disabled…".
- [ ] §13.4, "Detection can leave an edge to be declared": replace the collision recipe with design §8's wording.
- [ ] §4.13, "Two detected relationships keep one name" row: add "(including one a declared edge takes over, [13.4])".
- [ ] §30.4.2, the `constraint` and `columns` bullets: design §8's wording.
- [ ] §4.8, the `relationships` row: add the takeover sentence.
- [ ] `docs/configuration.md` → Relationships, "Renaming a detected edge": a single declaration over the same key renames the edge and keeps its `constraint`. Drop `exclude_relationships: [task_assignees]` from the example.
- [ ] Registry: narrow `gen.manual.replaces-inferred` to edges whose key differs, and `manifest.json.rel.constraint-manual-omitted` to "a manual relationship with no FOREIGN KEY behind it". Add the design §6.1 rows, with status planned.
- [ ] `docs/design/COMPOSITE_FK_RELATIONSHIPS.md` §3 and §7.1: add a pointer to this design where they describe manual replacement.
- [ ] Run `make req-check`. Review and `make req-rehash SECTIONS=4.8,4.13,13.4,30.4.2`.

### Acceptance Criteria

- The PRD states:
  - a manual edge names the FOREIGN KEY its key is, matching column pairs in any order, with the unnamed-SQLite label;
  - `constraint` is omitted only when no FOREIGN KEY declares the key;
  - an unfiltered manual edge over a detected edge's key (same kind, target and pair set; for `m2m`, the same junction and both keys) takes its place, with no exclusion and no warning;
  - a `filter:` / `discriminator:` edge never takes over.
- `make req-check` and `make refs-check` pass. No code changes.

### Tests Required

- [ ] `make req-check` clean, with every new row citing its planned test name.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 35.1 Regression guards — coexistence and a fixture move

**PRD Reference:** Sections 13.4, 13.7.3

**Status:** Not Started

### Tasks

- [ ] Write `TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists` (`cmd/sqlgen/gen`): a declared `filter:` edge and a declared `discriminator:` edge over a detected FOREIGN KEY are each emitted alongside the detected edge.
- [ ] Write `TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists` (`cmd/sqlgen/gen`): a declared `o2o` over the same columns as a detected `o2m` is emitted alongside it.
- [ ] Move the `TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` fixture's declared `Email` edge onto a key no FOREIGN KEY declares, so the takeover in 35.3 doesn't remove its `Orders` control. The assertion is unchanged.

### Acceptance Criteria

- Both new tests and the moved fixture pass on today's code, before any Phase 35 change.

### Tests Required

- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists`: passes on current code before any change.
- [ ] `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists`: passes on current code before any change.
- [ ] `unit: cmd/sqlgen/gen: TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` passes with its moved fixture, on current code.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 35.2 Gen — declared edges name their FOREIGN KEY constraint

**PRD Reference:** Sections 13.4, 30.4.2, 4.13

**Status:** Not Started

### Tasks

- [ ] Export `parser.ForeignKeyName(table *Table, c *Constraint) string` from `foreignKeyName` (`parser/relationship.go:148-155`), so a declared edge labels an unnamed SQLite key as detection does.
- [ ] Add `Declared bool` to `RelationshipContext`, set by `configRelationshipToContext`.
- [ ] Switch `validateDuplicateRelationshipFields` (`cmd/sqlgen/gen/context_table.go:995`) from `len(Constraints) > 0` to `!Declared` on both edges.
- [ ] Add `declaredKeyConstraints`. For O2O / O2M (belongs-to and has-one), find the holder table's FOREIGN KEY into the referenced table whose column pairs equal the key's pairs as a set. For M2M, find the junction's FK for `JunctionLocal` and for `JunctionReference`. Return `""` where nothing matches.
- [ ] Set `Constraints` from it in `configRelationshipToContext`, for every declared edge, `filter:` / `discriminator:` edges included.
- [ ] Regenerate the goldens (`make update-golden`, `make update-golden-e2e`). Confirm the only changes are `constraint` lines in the `graphql`, `mysql` and `postgres` manifests.

### Acceptance Criteria

- A declared edge's manifest `constraint` is the FOREIGN KEY whose column pairs equal its key, on every edge kind. An unnamed SQLite key gets the `table(columns) → target(columns)` label.
- A declared key with the same columns as a FOREIGN KEY but different pairs names no constraint. A key no FOREIGN KEY declares omits `constraint`.
- The collision error between two detected edges, and the one between two declared edges, are unchanged.
- Go goldens are byte-identical.

### Tests Required

- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over a foreign key names its constraint` (o2m): fails before the change, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual belongs-to names its constraint`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual has-one names its constraint`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual m2m names both junction constraints`: fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual composite key names its constraint` (pairs listed in another order): fails before, passes after. All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual key pairing a foreign key differently names no constraint`: passes before and after (it pins that set-of-columns is not enough). All dialects.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over an unnamed sqlite key`: fails before, passes after. SQLite.
- [ ] `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/discriminator edge names its constraint`: fails before, passes after. All dialects.
- [ ] Guard: `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship` (no FOREIGN KEY, no `constraint`) still passes.
- [ ] Guard: `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected edge names its constraint`, `…/key into a unique constraint` and `…/unnamed sqlite key` still pass.
- [ ] Guard: `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` still passes.
- [ ] Guard: `unit: cmd/sqlgen/gen: TestNestedEligibility_PinnedAndFreeMembers` still passes.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

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
