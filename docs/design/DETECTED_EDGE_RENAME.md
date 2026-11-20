# Renaming a Detected Relationship

> **Status: SYNCED — superseded by PRD §4.8/§4.13/§13.4/§30.4.2 (2026-11-20, Phase 35 closed).** Previously: approved 2026-11-20, drafted 2026-11-19. Verified against source at `82544e16`. Today you can rename an
> FK-detected relationship only by excluding it and declaring it again by hand. Forget the
> exclusion and the table gets two fields over one foreign key. Remember it and the manifest loses
> the edge's FOREIGN KEY `constraint`. This note proposes two rules:
> - A declared edge names the FOREIGN KEY constraint its key is, whether or not detection also
>   found the edge.
> - A declared edge over exactly a detected edge's key takes that edge's place, so no exclusion is
>   needed.
>
> It adds no config key. It leaves detection naming (§5.3 rules 5–6), `exclude_relationships` and
> the field-name replacement rule unchanged. Sibling to PRD §13.4 (Manual Relationship
> Configuration), §5.3 (Relationship Detection) and §30.4.2 (Entity shape).

## 1. Motivation

### 1.1 Symptom

Source: the sqlgen-example v0.1.2 findings, #5. Three of its four manual relationships
(`Subtasks`, `Assignees`, `Watchers`) exist only to rename a detected edge. Reproduced at
`82544e16` with this schema:

```sql
CREATE TABLE tasks (id uuid PRIMARY KEY, title text NOT NULL,
                    parent_task_id uuid REFERENCES tasks(id));
```

The probe set `generation.manifest.enabled: true`, ran `sqlgen generate` (§7, P1), and printed the
`tasks` edges from the struct and `manifest_gen.json`.

| Config | Struct fields | Manifest `fk` |
|---|---|---|
| none | `Tasks []*Task` | `tasks`: `constraint: tasks_parent_task_id_fkey` |
| declare `Subtasks` (o2m, `fk: parent_task_id`) | `Subtasks []*Task`, **`Tasks []*Task`** | `Subtasks` with no constraint, plus `tasks` with `constraint` |
| `exclude_relationships: [tasks]`, then declare `Subtasks` | `Subtasks []*Task` | `Subtasks`, **no `constraint`** |

The second row emits two fields over one foreign key, and no warning is printed. The third row is
the documented recipe, and it drops a fact the schema states.

The example also reports that rule-6 disambiguation renames an existing field when an unrelated
foreign key adds a second edge between the same two tables. That motivates the drift warning in
§4.3, which this note does not propose.

### 1.2 Root cause

- **Declared edges never get a constraint.** `configRelationshipToContext` never sets
  `Constraints` (`cmd/sqlgen/gen/context_table.go:2069-2135`). Only the detected-edge builder fills
  it (`context_table.go:2017-2021`). The manifest reads the constraint from there
  (`cmd/sqlgen/manifest/builder.go:1428,1440-1441`).
  - `stampKeyConstraintFacts` already finds the declared edge's own FOREIGN KEY, to copy its MATCH
    type (`cmd/sqlgen/gen/key_members.go:35-59`), but it never records the name.
- **A declared edge replaces a detected one only by Go field name.** `mergeDeclaredRelationships`
  indexes the detected edges by `FieldName` alone (`context_table.go:1883-1886`) and replaces on a
  name match (`:1935-1941`). A declared edge over the same key but under another name is added
  next to the detected edge. The unit test `TestBuildRelationshipContexts_DeclaredReplacesInferred`
  pins that, in its "declared name distinct from inferred edge" case
  (`cmd/sqlgen/gen/context_table_test.go:1062-1073`).

## 2. Scope

### 2.1 In scope

- **Constraint naming.** A declared edge's manifest `constraint` is the FOREIGN KEY whose column
  pairs equal its key, on every edge kind.
- **Takeover.** A declared edge with no `filter:` or `discriminator:`, whose kind, target and key
  equal a detected edge's, takes the detected edge's place.
- The PRD, `docs/configuration.md` and registry edits that follow.

### 2.2 Non-goals

- **A rename-only config map** (§4.2). It adds a config key. Defer it until declaring the key
  proves too much in practice (the "defer speculative DX" preference).
- **A rename-drift warning** against the previous manifest (§4.3). It's independent, and it works
  only when the manifest is enabled. A separate design can add it.
- **Changing detection naming**, for example stripping the source-table prefix from M2M names. That
  would rename every M2M field in every golden example.
- **Declaring an edge into a non-PK UNIQUE key.** Declared keys join the primary key (§13.4), so a
  declared edge never matches a detected edge into a UNIQUE key. That's unchanged.

## 3. Current state (measured from code)

- **Detection.** Each FOREIGN KEY gives at most one detected edge, built by `keyEdge` /
  `m2mEdges` (`parser/relationship.go:258-310`). `Constraint` is the FK name, or for an unnamed
  SQLite key the label `foreignKeyName` builds, `table(cols) → target(cols)`
  (`parser/relationship.go:148-155`). A detected O2O edge is always belongs-to, on the FK holder
  (`:276-279`). Detection produces no has-one edge.
- **Exclusion.** `buildRelationshipContexts` drops excluded detected edges before the merge, by
  pluralized `Name` or by `BaseName` (`context_table.go:1792-1827`).
- **The merge** (`context_table.go:1873-1956`):
  - A declared entry whose `name:` is itself excluded is skipped (`:1908`).
  - Replacement matches on `FieldName` and warns (`:1935-1941`).
  - Replaced detected edges are dropped (`:1945-1950`).
- **Declared key.** A declared key is `fk` mapped by position onto the referenced primary key
  (`context_table.go:2089-2111`).
- **"Detected" is inferred from `Constraints`.** `validateDuplicateRelationshipFields` tells the
  two-detected-edges collision error from the generic one by `len(Constraints) > 0`
  (`context_table.go:995`). Once declared edges carry constraints, that test is wrong, so the design
  adds an explicit flag.
- **Manifest.** `constraintAt` returns `""` for a missing index, and an empty constraint is omitted
  (`manifest/builder.go:1455-1477`). `TestBuild_RelationshipKeyShape` covers detected, unique-key,
  unnamed-SQLite, manual and composite cases (`cmd/sqlgen/manifest/relationship_key_test.go:55-107`).
- **Golden examples.** One golden example declares an edge over a detected key under another name:
  `sqlite` declares `binary_keys.Events` over `binary_key_events.binary_key_id`, next to the
  detected `BinaryKeyEvents` (`cmd/sqlgen/testdata/examples/sqlite/sqlgen.yml:115-120`). No test in
  that example names `BinaryKeyEvents` (grep, P3).

## 4. Options

### 4.1 Option A: declared edges name their constraint and take over a same-key detected edge (recommended)

1. **Constraint naming.** A declared edge records, for each of its keys, the FOREIGN KEY whose
   column pairs equal that key, named as detection names it. This covers the exclude-and-declare
   recipe, a set-aside junction FK that §5.3 rule 4 says to declare by hand, and a polymorphic edge
   over a real FK.
2. **Takeover.** A declared edge with no `filter:` or `discriminator:`, whose kind and target equal
   a detected edge's and whose key equals that edge's key as a set of pairs, takes its place. For
   M2M, "key" means the same junction and both junction keys. No exclusion is needed and no warning
   fires, because no edge is lost.

- **Wins:** no new config surface. The example's three renames each become a single declaration.
  The existing exclude-and-declare configs keep generating the same code and gain their
  constraint. A residual rule-6 collision clears by declaring one of the two edges over its key.
- **Loses:** you still write the key out in full. One edge over one FK can no longer appear under
  two unfiltered names (§11 Q1). It also reverses a §13.4 sentence and the pinned "declared name
  distinct" test case.
- **Size:** the prototype is about 90 lines in `gen` (§7, P2).

### 4.2 Option B: a rename map

`tables.<t>.relationship_names: {<constraint or junction>: <Name>}`, applied after rule 6, so
detection keeps the kind, the key and the constraint.

- **Wins:** the shortest config, and it's the shape the example asked for. It survives a column
  rename.
- **Loses:**
  - It's a new public config key: JSON Schema, validation for an unknown key, and PRD §4.8, §4.13
    and §5.3.
  - An unnamed SQLite FK has only a synthesized label to key on, which changes when a column does.
  - The inverse name (`inverse:`) adds more surface again.
  - It duplicates what Option A already gives.

### 4.3 Option C: a rename-drift warning

`generate` / `diff` compares each constraint's edge name with the previous `manifest_gen.json` and
warns when it changes.

- **Wins:** it catches the "an unrelated migration renamed a public field" hazard, which neither A
  nor B addresses.
- **Loses:** the CLI reads no prior manifest today, and the warning is silent when the manifest is
  disabled. It's orthogonal to A and B and can follow either one.

**Recommendation:** A now. B only if declaring keys proves too much in practice. C as its own
design if drift bites.

## 5. Proposed design

**Parser.** Export `foreignKeyName` as `parser.ForeignKeyName(table *Table, c *Constraint)
string`, so a declared edge names an unnamed SQLite key exactly as detection does. This is a
parser-module API addition. The CLI already imports the parser, so the three-module boundary
holds.

**Gen (`cmd/sqlgen/gen`):**

- `RelationshipContext` gains `Declared bool`, set by `configRelationshipToContext`.
  `validateDuplicateRelationshipFields` tests `!Declared` on both edges instead of
  `len(Constraints) > 0`.
- `configRelationshipToContext` sets `Constraints` from `declaredKeyConstraints`:
  - **O2O / O2M:** the holder table's FOREIGN KEY into the referenced table whose
    `(Columns[i], ReferenceColumns[i])` pairs equal the key's pairs as a set.
  - **M2M:** the junction's FK into this table for `JunctionLocal`, and its FK into the target for
    `JunctionReference`.
  - The name is `parser.ForeignKeyName`, and `""` where no constraint matches.
- `mergeDeclaredRelationships` checks for a takeover before the field-name rule. It calls
  `sameKeyInferred`, which looks for an unreplaced detected edge that matches all of these:
  - the same `Type`, `TargetSchema` and `TargetTable`;
  - for O2O / O2M, the same `FKOnTarget` and an equal `Key` pair set;
  - for M2M, the same junction schema and table, and equal `JunctionLocal` and `JunctionReference`
    pair sets.

  A declared edge with a `filter` or `discriminator` never matches. A match marks the detected
  edge replaced and adds no warning. Without a match, the field-name rule runs as today, warning
  included.
- The declared context is used unchanged. A takeover therefore generates exactly what the
  exclude-and-declare recipe generates today, plus `constraint`.

**Manifest.** No code change: `constraintAt` already reads `Constraints`.

**Generated API.** A takeover removes the duplicate field, its loader, its filter member and its
nested surface. In the golden examples this affects only `sqlite`, where `BinaryKeyEvents` goes and
`Events` stays (P3).

### 5.1 Worked example

This is the sqlgen-example app's case, cut down to two renames: an O2M self-reference and an M2M
edge.

```sql
CREATE TABLE users (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE tasks (
  id             uuid PRIMARY KEY,
  title          text NOT NULL,
  parent_task_id uuid REFERENCES tasks(id)          -- tasks_parent_task_id_fkey
);
CREATE TABLE task_assignees (
  task_id uuid REFERENCES tasks(id),                -- task_assignees_task_id_fkey
  user_id uuid REFERENCES users(id),                -- task_assignees_user_id_fkey
  PRIMARY KEY (task_id, user_id)
);
```

Detection names the two edges `tasks` (`Task.Tasks`) and `users` (`Task.Users`). Each rename
below gets the name the app wants.

**Today**, renaming takes an exclusion for each edge plus a declaration:

```yaml
tables:
  tasks:
    exclude_relationships: [tasks, users]
    relationships:
      - { name: Subtasks,  type: o2m, table: tasks, fk: parent_task_id }
      - { name: Assignees, type: m2m, table: users, junction: task_assignees,
          junction_local_fk: task_id, junction_reference_fk: user_id }
```

```json
{ "name": "Subtasks",  "fk": { "table": "tasks", "columns": ["parent_task_id"], "references": ["id"] } }
{ "name": "Assignees", "junction": { "table": "task_assignees",
    "local_fk":  { "columns": ["task_id"], "references": ["id"] },
    "target_fk": { "columns": ["user_id"], "references": ["id"] } } }
```

Leave out `exclude_relationships` and the struct gets both `Subtasks` and `Tasks`, and both
`Assignees` and `Users`: two fields over each key, with no warning (§1.1, row 2). With the
exclusion in place, the manifest loses both constraints (row 3).

**With this design**, you declare the edges and nothing else:

```yaml
tables:
  tasks:
    relationships:
      - { name: Subtasks,  type: o2m, table: tasks, fk: parent_task_id }
      - { name: Assignees, type: m2m, table: users, junction: task_assignees,
          junction_local_fk: task_id, junction_reference_fk: user_id }
```

- `Subtasks` has the same kind (o2m), target (`tasks`) and key (`parent_task_id → id`) as the
  detected `tasks` edge, so it takes that edge's place. `Assignees` matches the detected `users`
  edge on its junction and both junction keys. No warning is printed.
- The struct has `Subtasks []*Task` and `Assignees []*User` only. Each loader, filter member and
  nested-mutation input is what the exclude-and-declare config generates today.
- The manifest keeps the constraints:

```json
{ "name": "Subtasks",  "fk": { "table": "tasks", "columns": ["parent_task_id"], "references": ["id"],
    "constraint": "tasks_parent_task_id_fkey" } }
{ "name": "Assignees", "junction": { "table": "task_assignees",
    "local_fk":  { "columns": ["task_id"], "references": ["id"], "constraint": "task_assignees_task_id_fkey" },
    "target_fk": { "columns": ["user_id"], "references": ["id"], "constraint": "task_assignees_user_id_fkey" } } }
```

The existing config, with `exclude_relationships` still in it, generates the same code as before
plus the `constraint` lines. Nothing breaks, and you can delete the exclusions whenever you like.

**What doesn't take over.** Each of these is added next to the detected edge, as it is today:

- **A filtered edge.** For example, `{ name: OpenSubtasks, type: o2m, table: tasks, fk:
  parent_task_id, filter: "status <> 'done'" }`. It still records `tasks_parent_task_id_fkey`
  (ruling 3), but `Tasks` stays: a filtered edge reads only some of the key's rows.
- **A different key.** A declared edge over a column with no FOREIGN KEY gets no `constraint` and
  leaves every detected edge alone.

**Several edges between the same two tables.** An entity that links several documents takes one
of two shapes. Which one decides whether there is a detected edge to replace.

*Shape 1: the key is on `documents` (polymorphic).*

```sql
CREATE TABLE documents (
  id          uuid PRIMARY KEY,
  entity_type text NOT NULL,           -- 'asset.pdf', 'asset.spreadsheet', ...
  entity_id   uuid NOT NULL            -- no FOREIGN KEY: it can point at several tables
);
```

```yaml
tables:
  assets:
    relationships:
      - { name: Pdf,         type: o2o, table: documents, fk: entity_id,
          discriminator: { column: entity_type, value: "asset.pdf" } }
      - { name: Spreadsheet, type: o2o, table: documents, fk: entity_id,
          discriminator: { column: entity_type, value: "asset.spreadsheet" } }
```

- With no FOREIGN KEY on `entity_id`, detection finds no edge. There's no constraint to record and
  nothing to replace. The declared edges behave exactly as they do today (§13.4.1).
- If `documents` belongs to `assets` alone, `entity_id` can carry
  `REFERENCES assets(id)` (`documents_entity_id_fkey`). Then detection also finds an unfiltered
  O2M `documents` edge (`Asset.Documents`, every document of the asset):
  - Both discriminator edges record `documents_entity_id_fkey`: the constraint is a fact about the
    key (ruling 3).
  - Neither replaces `Documents`, because a filtered edge never takes over (§5). `Asset.Documents`
    stays next to `Pdf` and `Spreadsheet`. `exclude_relationships: [documents]` still drops it.

*Shape 2: one FOREIGN KEY per document, on `assets`.*

```sql
CREATE TABLE assets (
  id                      uuid PRIMARY KEY,
  pdf_document_id         uuid REFERENCES documents(id),   -- assets_pdf_document_id_fkey
  spreadsheet_document_id uuid REFERENCES documents(id)    -- assets_spreadsheet_document_id_fkey
);
```

Detection gives a non-unique FOREIGN KEY one O2M edge on the referenced table, and nothing on the
holder (`parser/relationship.go:261-283`). So `documents` gets two O2M edges into `assets`, and
rule 6 renames them `pdf_document_assets` and `spreadsheet_document_assets`. `assets` gets no
detected document field.

- **The `assets` side.** Each document is a declared belongs-to `o2o`. Since there's no detected
  edge on `assets`, nothing is replaced, and each edge records its own constraint:

  ```yaml
  tables:
    assets:
      relationships:
        - { name: Pdf,         type: o2o, table: documents, fk: pdf_document_id }
        - { name: Spreadsheet, type: o2o, table: documents, fk: spreadsheet_document_id }
  ```

  Today these generate the same fields with no `constraint`.
- **The `documents` side.** Renaming one of its rule-6 edges is a declared O2M over that one key.
  It replaces only the edge over the same key:

  ```yaml
  tables:
    documents:
      relationships:
        - { name: PdfAssets, type: o2m, table: assets, fk: pdf_document_id }
  ```

  `Document.PdfAssets` replaces `Document.PdfDocumentAssets` and keeps
  `assets_pdf_document_id_fkey`. `Document.SpreadsheetDocumentAssets` isn't declared, so it keeps
  its rule-6 name. Each FOREIGN KEY is its own key, so a declaration can't replace the wrong edge.
- **When the column is unique** (`pdf_document_id uuid UNIQUE REFERENCES documents(id)`), rule 3
  makes the edge a detected belongs-to `o2o` on `assets`, named `pdf_document` by rule 6
  (`Asset.PdfDocument`). The declared `Pdf` above then has the same kind, target and key, so it
  replaces `PdfDocument` and keeps the constraint.

An undeclared edge keeps its rule-6 name. Adding another FOREIGN KEY between the same two tables
later can still rename it. Option C (§4.3) is the guard for that, and it stays deferred.

**Composite keys.** Take `cycles` keyed `PRIMARY KEY (workspace_id, id)`, and a foreign key on
`tasks` declared in the other order:
`FOREIGN KEY (cycle_id, workspace_id) REFERENCES cycles (id, workspace_id)`.

- The declared `fk: [workspace_id, cycle_id]` maps by position onto the primary key (§13.4), so
  its pairs are `workspace_id → workspace_id` and `cycle_id → id`. Those are the foreign key's
  pairs, listed in another order. Pairs are compared as a set (ruling 4), so it takes over and
  records the constraint. The manifest lists `columns` in the declared order.
- `fk: [cycle_id, workspace_id]` maps to `cycle_id → workspace_id` and `workspace_id → id`. That's
  a different key, so it matches nothing and records no constraint
  (`rename.constraint.pairs-not-set`).

## 6. Behavior contract

### 6.1 New and changed behavior

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| rename.constraint.o2m | A declared O2M edge whose key equals a FOREIGN KEY's column pairs names that constraint in the manifest, `fk.constraint` | §13.4, §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over a foreign key names its constraint` | relationships: o2m × struct-field | passing |
| rename.constraint.belongs-to | A declared belongs-to O2O edge names the FOREIGN KEY on this table that its key is | §13.4, §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual belongs-to names its constraint` | relationships: belongs-to × struct-field | passing |
| rename.constraint.m2m | A declared M2M edge names each junction key's FOREIGN KEY in `local_fk.constraint` and `target_fk.constraint` | §13.4, §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual m2m names both junction constraints` | relationships: m2m × struct-field | passing |
| rename.constraint.composite | A declared composite key matches its FOREIGN KEY by column pairs, in any listed order | §13.4 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual composite key names its constraint` | relationships: composite-o2m × struct-field | passing |
| rename.constraint.pairs-not-set | A declared key with the same columns as a FOREIGN KEY but different pairs names no constraint | §13.4 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual key pairing a foreign key differently names no constraint` | relationships: composite-belongs-to × struct-field | passing |
| rename.constraint.unnamed-sqlite | A declared edge over an unnamed SQLite FOREIGN KEY is labelled `table(columns) → target(columns)`, as detection labels it | §30.4.2 | sqlite | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship over an unnamed sqlite key` | relationships: o2m × struct-field | passing |
| rename.takeover.o2m | A declared O2M edge with no `filter:` or `discriminator:`, over exactly a detected edge's key into the same target, takes its place: one field, under the declared name | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/o2m` | relationships: o2m × struct-field | passing |
| rename.takeover.belongs-to | The same holds for a declared belongs-to O2O edge | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/belongs-to` | relationships: belongs-to × struct-field | passing |
| rename.takeover.m2m | The same holds for a declared M2M edge over the same junction and both junction keys | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/m2m` | relationships: m2m × struct-field | passing |
| rename.takeover.composite | The same holds for a composite key, with the pairs listed in any order | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/composite o2m` | relationships: composite-o2m × struct-field | passing |
| rename.takeover.self-m2m | A declared self-referential M2M edge takes over only the direction whose junction keys it matches | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/self-referential m2m direction` | relationships: self × struct-field | passing |
| rename.takeover.no-warning | A takeover reports no warning | §13.4 (§11 Q2) | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/declared name collides with inferred edge` | relationships: o2m × struct-field | passing |
| rename.takeover.distinct-name | A declared edge under another name over the detected key no longer adds a second field | §13.4 (§11 Q1) | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/declared name distinct from inferred edge`; `golden: sqlite` | relationships: o2m × struct-field | passing |
| rename.takeover.clears-collision | When two detected edges keep one name after rule 6, declaring one of them over its key clears the collision error without `exclude_relationships` | §5.3 rule 6, §4.13 | all | `unit: cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionClearedByDeclaringOneEdge` | config-validation: relationship-name-duplicate × fires | passing |
| rename.constraint.has-one | A declared has-one O2O edge names the FOREIGN KEY on the related table that its key is | §13.4, §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual has-one names its constraint` | relationships: has-one × struct-field | passing |
| rename.constraint.filtered | A declared `filter:` or `discriminator:` edge over a FOREIGN KEY names that constraint, and the detected edge over the same key is still emitted | §13.4, §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/discriminator edge names its constraint` | relationships: polymorphic × struct-field | passing |
| rename.takeover.only-same-key | When several detected edges join the same two tables, a declared edge takes the place of the one over its key only, and the others keep their rule-6 names | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/one of several edges into one target` | relationships: o2m × struct-field | passing |
| rename.takeover.onto-another-field | A declared edge that takes over one detected edge under another detected edge's Go field is the §13.7.3 duplicate-field error, not a second replacement | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_TakeoverOntoAnotherDetectedFieldIsAnError` | relationships: o2m × struct-field | passing |

### 6.2 Regression guards

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| A declared edge with no FOREIGN KEY behind it omits `constraint` | §30.4.2 | `cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship` | guarded |
| A detected edge names its constraint, a UNIQUE-key edge included | §30.4.2 | `cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected edge names its constraint`; `…/key into a unique constraint` | guarded |
| An unnamed SQLite detected edge is labelled `table(columns) → target(columns)` | §30.4.2 | `cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/unnamed sqlite key` | guarded |
| Exclude plus declare still generates the declared edge alone, without a warning (guard.exclude-plus-declare) | §13.4 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred/exclude_relationships alongside the declaration` | guarded |
| A declared edge whose key differs from the detected edge's, but which claims its Go field, still replaces it with the warning | §13.4 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverCompositeFK` | guarded |
| `exclude_relationships` matches the base name and the disambiguated plural | §5.3 rule 6, §4.8 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_ExcludeByBaseName` | guarded |
| Two detected edges still sharing a name is an error naming both constraints | §4.13, §5.3 rule 6 | `cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` | guarded |
| Two declared edges resolving to one Go field is an error, now that both may carry constraints | §13.7.3 | `cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` | guarded |
| Discriminator sub-categorized edges stay distinct | §13.7.3 | `cmd/sqlgen/gen: TestDiscriminator_SubCategorizedEdgesStayDistinct` | guarded |
| A declared `filter:` or `discriminator:` edge over a detected FOREIGN KEY coexists with the detected edge | §13.7.3 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_FilteredEdgeOverDetectedKeyCoexists` | guarded |
| A declared edge of another kind over the same columns (o2o against a detected o2m) coexists | §13.4 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOfAnotherKindCoexists` | guarded |
| A declared key keeps joining the referenced primary key, in `fk` order | §13.4 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey` | guarded |
| A declared edge's pinned members and MATCH type are unchanged | §9.9.2 | `cmd/sqlgen/gen: TestNestedEligibility_PinnedAndFreeMembers` | guarded |

The column-comparator test `TestRelationshipFilters_ColumnComparatorKeepsItsFieldName` declares
`Email` over the detected `orders` key as a convenience, so the takeover removes its `Orders`
control (P2). N.1 moves its fixture to a key no FOREIGN KEY declares. That's a fixture change; the
behavior it guards doesn't change.

### 6.3 Module matrix cells

#### relationships (CLI, `docs/conformance/cli/gen.md`)

| Type | struct-field | loader | filter | nested-mutation |
|---|---|---|---|---|
| `o2m` | rename.takeover.o2m | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `belongs-to` | rename.takeover.belongs-to | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `has-one` | rename.constraint.has-one (takeover n/a: detection gives no has-one edge, §5.3 rule 5) | n/a: no detected has-one edge to take over | n/a: same | n/a: same |
| `m2m` | rename.takeover.m2m, rename.constraint.m2m | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `composite-o2m` | rename.takeover.composite | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `composite-belongs-to` | rename.constraint.pairs-not-set | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `composite-has-one` | n/a: no detected has-one edge (§5.3 rule 5) | n/a: same | n/a: same | n/a: same |
| `composite-m2m` | n/a: each junction key matches as a pair set (rename.constraint.m2m, rename.takeover.m2m), so a composite key adds no case | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `self` | rename.takeover.self-m2m | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `circular` | n/a: a cycle A→B→A is two edges, each covered by its own kind's row | guard.exclude-plus-declare | guard.exclude-plus-declare | guard.exclude-plus-declare |
| `polymorphic` | rename.constraint.filtered (takeover n/a: a `filter:` / `discriminator:` edge never takes over, §13.7.3) | n/a: never takes over | n/a: same | n/a: same |

A takeover keeps the declared context exactly as exclude-and-declare builds it today, so the
loader, filter and nested columns change nothing beyond what that guard already covers.

#### config-validation (CLI, `docs/conformance/cli/config.md`)

| Rule | fires | message | severity | together | schema |
|---|---|---|---|---|---|
| `relationship-name-duplicate` | rename.takeover.clears-collision | n/a: the message is unchanged | n/a: unchanged | n/a: unchanged | n/a: not a JSON Schema rule |

### 6.4 Dialect differences

None. An unnamed SQLite FOREIGN KEY is labelled the same way for declared and detected edges
(rename.constraint.unnamed-sqlite). MySQL and PostgreSQL name every constraint.

## 7. Probe record

| # | Date | sha | What ran | Result |
|---|---|---|---|---|
| P1 | 2026-11-19 | `82544e16` | Built `cmd/sqlgen` at HEAD. Generated the `tasks` self-reference schema from §1.1 under three configs (no config; declared `Subtasks`; exclude plus declare) | Reproduced the table in §1.1: two fields and no warning with the declaration alone, and no `constraint` with exclude plus declare |
| P2 | 2026-11-19 | `82544e16` + prototype | Applied `.claude/prototypes/DETECTED_EDGE_RENAME.patch` (`Declared` flag, `declaredKeyConstraints`, `sameKeyInferred`). Reran P1, plus a fourth config declaring `Children` with `filter: "title <> ''"` over the same FK. Ran `go test ./gen/ ./manifest/ ./cli/ ./config/` | Declared alone: one `Subtasks` field with `constraint: tasks_parent_task_id_fkey`. Exclude plus declare: the same. Filtered `Children` coexists with `tasks`, and both name the constraint. Tests: `manifest`, `cli` and `config` pass. `gen` fails only `DeclaredReplacesInferred` (the two cases rename.takeover.* changes) and `ColumnComparatorKeepsItsFieldName` (the fixture noted in §6.2) |
| P3 | 2026-11-19 | `82544e16` + prototype | `go test -run TestE2EGoldenFiles` with no `-update` | Only 4 examples differ. In `graphql`, `mysql` and `postgres`, the only change is added manifest `constraint` lines on declared edges (`workspace_notes_parent_id_fkey`, `user_badges_user_id_fkey`, `asset_document_links_*`, `profiles_*`). In `sqlite`, `BinaryKeyEvents` (field, filter member and translator) is removed, and `Events` stays. No test in that example names `BinaryKeyEvents` |

The prototype doesn't yet use `parser.ForeignKeyName`: an unnamed SQLite key gives `""`. N.2
switches to the exported label.

## 8. PRD and doc impact

- **§13.4, the paragraph that starts "A manual relationship joins the referenced table's primary
  key".** Replace "A manual edge has no FOREIGN KEY constraint behind it, so its manifest
  `constraint` is omitted ([30.4.2](#3042-entity-shape))" with: "A manual edge names, for each of
  its keys, the FOREIGN KEY constraint whose column pairs equal that key, in any order, as
  detection names it ([30.4.2](#3042-entity-shape)). A key no FOREIGN KEY declares has no
  `constraint`."
- **§13.4, a new paragraph after "Auto-detected relationships can be disabled…":** "**A manual
  edge over a detected edge's key is that edge, renamed.** A declared relationship with no
  `filter:` or `discriminator:` takes a detected edge's place when it has the same kind and target
  and the same key as a set of column pairs. For `m2m`, that means the same junction and both
  junction keys. The detected edge is not emitted, no `exclude_relationships` entry is needed, and
  no warning is reported, since no edge is lost. One unfiltered edge over one key is therefore
  never emitted twice. A declared edge that matches no detected edge's key falls back to the
  field-name rule below."
- **§13.4, "Detection can leave an edge to be declared":** replace "For a collision, list the
  detected name in `exclude_relationships` and declare the edge under a name of your choosing" with
  "For a collision, declare one of the edges over its key under a name of your choosing. It takes
  that edge's place. Listing the detected name in `exclude_relationships` and declaring both edges
  also works."
- **§4.13, the "Two detected relationships keep one name" row.** After "an excluded or replaced
  edge does not count", add "(including one a declared edge takes over, [13.4])". The error text
  is unchanged.
- **§30.4.2, the `constraint` bullet.** Replace "A manual relationship has no constraint behind it,
  and `constraint` is omitted" with "A manual relationship names the FOREIGN KEY its key is
  ([13.4]), and the same label for an unnamed SQLite key. `constraint` is omitted when no FOREIGN
  KEY declares the key." In the `columns` bullet, after "in the foreign key's declared order", add
  "(for a manual relationship, its `fk` order)".
- **§4.8, the `relationships` row.** Add: "A manual relationship over exactly a detected edge's key
  takes that edge's place, without a warning ([13.4])."
- **`docs/configuration.md` → Relationships, "Renaming a detected edge":** rewrite it to say that a
  single declaration over the same key renames the edge and keeps its `constraint`. Drop the
  `exclude_relationships: [task_assignees]` line from the example.
- **Registry.**
  - `gen.manual.replaces-inferred` narrows to edges whose key differs.
  - New rows from §6.1.
  - `manifest.json.rel.constraint-manual-omitted` narrows to "a manual relationship with no FOREIGN
    KEY behind it".
- **Design notes.** `COMPOSITE_FK_RELATIONSHIPS.md` is SYNCED. Its §6.2 and §7.1 statements about
  manual replacement get a pointer to this note.

## 9. Plan

| # | Sub-item | Contract rows | Notes |
|---|---|---|---|
| N.0 | PRD sync (§8), `configuration.md`, registry rows | — | Blocks everything |
| N.1 | Regression guards: the filtered-edge and other-kind coexistence tests, and moving the `ColumnComparatorKeepsItsFieldName` fixture to an undeclared key | the two unguarded §6.2 rows | Pass on today's code |
| N.2 | Constraint naming: export `parser.ForeignKeyName`, add the `Declared` flag, switch the collision check to it, add `declaredKeyConstraints` | rename.constraint.* | Manifest goldens gain `constraint` in `graphql`, `mysql`, `postgres`, and in `sqlite` through the unnamed-key label (P3 ran before the label) |
| N.3 | Takeover: `sameKeyInferred` in `mergeDeclaredRelationships` | rename.takeover.* (including rename.takeover.only-same-key) | `sqlite` golden loses `BinaryKeyEvents`. The two `DeclaredReplacesInferred` cases flip |

## 10. Definition of done

The work closes only when every item below holds:

1. Every §6.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §6.2 row is guarded, and every guard still passes.
3. Every §6.3 cell is an ID or a stated `n/a`.
4. Every §8 edit has landed, and every §11 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too when the
   change reaches a runtime or driver package.
6. The sqlgen-example config drops its three `exclude_relationships` entries, and its manifest
   keeps `constraint` on `Subtasks`, `Assignees` and `Watchers`.

## 11. Rulings (settled 2026-11-20)

| # | Question | Ruling | Evidence |
|---|---|---|---|
| 1 | Should a same-key declared edge take over even when its name differs (the alias case)? | Yes, any same-key match. The `sqlite` golden losing `BinaryKeyEvents` and the two flipped unit cases are accepted churn. | User ruling; prototype §7 |
| 2 | Should a takeover report a warning? | No. Nothing is dropped. | User ruling |
| 3 | Does constraint naming apply to a `filter:` / `discriminator:` edge over a real FOREIGN KEY? | Yes. The constraint is a fact about the key. | User ruling; P2 in §7 |
| 4 | Does a key match require the same pairs, or the same pairs in the FK's order? | Same pairs in any order. The manifest lists the declared `fk` order, and no generated code moves. | User ruling |

## 12. Rejected alternatives

- **Inheriting the detected edge's context wholesale, with the declared name overlaid.** It would
  differ from what exclude-and-declare generates today (for example, the key order and the
  description), so existing configs would churn. Using the declared context keeps today's recipe
  byte-identical apart from `constraint`.
- **Matching on column sets rather than pairs.** `stampKeyConstraintFacts` matches by set
  (`key_members.go:52`), but a declared `fk: [b, a]` against `FOREIGN KEY (a, b) REFERENCES t (x,
  y)` joins `b → x`. That's a different key, and it must not claim the constraint
  (rename.constraint.pairs-not-set).
