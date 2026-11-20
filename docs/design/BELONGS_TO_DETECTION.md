# Belongs-To Detection

> **Status:** approved (2026-11-20). Verified against source at `f5dac70c`. Detection gives a
> non-unique foreign key only its list edge, on the referenced table, so a row can't reach its
> parent: `Project.tasks` is generated, `Task.project` isn't. This note proposes detecting the
> to-one (belongs-to) edge of every foreign key on a table that isn't a junction, with
> stem-based names (`author_id` → `Author`). It also fixes four bugs on the to-one path that the
> change would otherwise spread. It leaves junction tables, has-one detection, M2M naming and
> nested writes alone. Sibling to PRD §5.3 (Relationship Detection), §13.1 (Relationship Types),
> §13.4 (Manual Relationship Configuration) and §4.13 (Config Validation Rules).

## 1. Motivation

### 1.1 Symptom

The `sqlgen-example` app (v0.1.2) has 43 foreign-key clauses in its migrations and declares no
to-one edge. Its GraphQL `Task` type carries every parent only as a scalar id:

```
$ awk '/^type Task /,/^}/' internal/graph/task_gen.graphqls | grep -v '"'
type Task {
  id: UUID!
  workspaceID: UUID!
  projectID: UUID!
  parentTaskID: UUID
  reporterID: UUID!
  ...
  cycleID: UUID
  assignees: [User!]!
  subtasks: [Task!]!
  ...
}
```

A client can go from a Project to its tasks, but not from a Task to its project, reporter, cycle
or parent task, except by issuing a second query by id. The minimal case, run through
`BuildTableContexts` at `f5dac70c` (§7, P1):

```
CREATE TABLE users (id bigint PRIMARY KEY);
CREATE TABLE posts (id bigint PRIMARY KEY, author_id bigint REFERENCES users(id));

users: [OneToMany:Posts]
posts: (no relationships)
```

A detected to-one edge, where one exists, is also named after the plural table:
`UserCredential.Users *User`, `Profile.Users *User`, `OwnerProfile.Owners *Owner`,
`TaskReport.Tasks *Task` in the committed goldens (§7, P2).

### 1.2 Root cause

- `keyEdge` (`parser/relationship.go:261-283`) returns one edge per foreign key: O2O on the
  holder when a key of the holder lies within the FK's columns, else O2M on the referenced table.
  PRD §5.3 states the same rule ("each FOREIGN KEY constraint gives at most one edge").
- A to-one edge is named after the referenced table verbatim (`parser/relationship.go:279`, PRD
  §5.3 rule 5), and `relationshipToContext` pluralizes list names but never singularizes a
  to-one name (`cmd/sqlgen/gen/context_table.go:2023-2027`).

## 2. Scope

### 2.1 In scope

- A belongs-to O2O edge on the holding table for every non-unique foreign key that already gives
  an O2M edge, single-column or composite.
- Stem-based naming for every detected to-one edge, singular in generated code.
- Four bugs on the to-one path, each measured at `f5dac70c` (§3.4). Detection would otherwise put
  them on most schemas:
  1. A self-referential belongs-to joins a child instead of the parent.
  2. An `o2o` whose key column has the same name as the referenced column joins the wrong
     columns.
  3. A detected edge whose Go field equals a column's field generates a package that doesn't
     compile, with no diagnostic.
  4. A chained O2O join walks an edge that the target dropped (by takeover or by
     `exclude_relationships` base name), so the generated package doesn't compile. Item 4 is a
     regression from Phase 35 (PR #7). It is fixed here, as N.3 (§11 Q5).

### 2.2 Non-goals

- **Junction tables.** A junction gives only its M2M edge (rule 4). `TaskAssignee.task` would
  need the inverse O2M (`Task.taskAssignees`) to be consistent, and neither is asked for.
- **Has-one detection.** The referenced side of a unique foreign key stays config-declared
  (§13.1). A UNIQUE constraint says "at most one", which a has-one needs, but the parent's view
  of an extension row is a modelling choice the PRD leaves to config.
- **M2M naming** (`taskAssignees` → `assignees`) and the **rename-drift warning**. Both are
  separate naming changes, listed as follow-ons in the research notes.
- **Nested writes.** A belongs-to is shape 1 and is never nestable (§9.9.1), so the nested
  surface doesn't change.
- **The manifest `o2o` / `m2o` split.** A detected belongs-to is `kind: "o2o"`, as a declared one
  is today (`cmd/sqlgen/manifest/builder.go:1478-1496`).

## 3. Current state (measured from code)

### 3.1 Detection

- `DetectRelationships` (`parser/relationship.go:22-55`) collects each table's foreign keys,
  takes the junction path when `junctionKeys` finds one (M2M only), and otherwise appends
  `keyEdge` for each foreign key that gives an edge.
- `keyEdge` sets `Constraint` and `Key` from the foreign key on both kinds, so a second edge over
  the same key can carry the same facts with `SourceTable` and `TargetTable` swapped.
- `namingMember` (`:321-351`) already picks one member column per key. Rule 6 uses it to rename
  an O2O edge to `<stem>` (`:428-429`).
- `disambiguateRelationshipNames` (`:394-415`) groups edges by `(SourceTable, TargetTable)` and
  ignores kind.

### 3.2 Codegen of a to-one edge

- **Load path.** A declared belongs-to over a non-unique key already generates and runs. The
  postgres example's `audit.notes.Author` is one. It loads by a LEFT JOIN in the parent's own
  `GetMany`, only when the selection asks for it (`resolveO2OJoins`, e.g.
  `examples/graphql/expected/models_gen.go:2204-2210`). `Get`, `Paginate` and `Connection` all
  read through `GetMany` (`:2106`, `:5150`, `:5236`). A to-one edge never costs an extra query,
  and GraphQL selections reach it through field options
  (`graph/sqlgenresolver/field_options_gen.go:104-107`), with no per-field resolver.
- **Tenancy and soft delete.** The join adds the child's tenant predicate (PRD §29, line 15524)
  and its soft-delete predicate (`buildSingleO2ODetail`, `context_table.go:3176-3180`, `3236-3240`).
- **Chains.** `buildSingleO2ODetail` recurses into every O2O edge of the target, up to depth 6
  (`context_table.go:3149-3151`, `3244-3252`). The edges come from `findO2ORelationships`
  (`:3264-3310`).

### 3.3 Filters, nested writes, manifest, MCP

- A to-one edge contributes no filter member (§11.1; `TestRelationshipFilters_ListRelationshipsOnly`).
- A belongs-to admits no nested verb (§9.9.1;
  `TestNestedEligibility_ListedShapeFailureIsReported/belongs-to edge`).
- The manifest emits an O2O edge with `fk.table` = the holder (§30.4.2).
- `sqlgen_find_referencing` dedups edges by full tuple (`TestDispatchFindReferencingDedupsByFullTuple`).

### 3.4 Four bugs on the to-one path (all at `f5dac70c`)

1. **Self-referential belongs-to joins a child (P3).** `Parent` declared over
   `tasks.parent_task_id` gets `FKOnTarget = true`, because `fkColumnOnTarget`
   (`context_table.go:2184-2201`) only asks whether the target table has a column of that name.
   The join is `p.parent_task_id = t.id`. On SQLite, the root task's `.Parent` is its child, and
   the child's `.Parent` is nil.
2. **Same-name key column joins the wrong columns (P3).** `CodeRow` declared over
   `items.code → codes.code` gets `FKOnTarget = true`, and the join is `c.code = i.id`. The item
   with `code = 'A'` loads code `'1'`. This is backlog line 280, which guessed it was "likely
   harmless on reads". It isn't. `inferredFKOnTarget` (`:2207-2212`) avoids it for composite
   detected keys only.
3. **Column vs detected edge compiles twice (P4).** `users(posts TEXT)` with a detected `posts`
   edge passes `sqlgen validate` and `generate`, and `models_gen.go` declares `Posts` twice. The
   field-collision check runs only when a `column_map` override is present (PRD §4.13 row "column
   and a relationship cannot claim the same Go field … Only collisions an override introduces are
   reported"; `validateDuplicateFieldNames`, `context_table.go:1029-1039`).
4. **Chains walk dropped edges (P5).** A declared `Writer` over a unique `posts.author_id` takes
   over the detected `users` edge (PRD §13.4, Phase 35). `post_details`' chain still reads
   `fo.Posts.Users`, which `PostFieldOptions` no longer has, so the package doesn't build:
   `fo.Posts.Users undefined (type *PostFieldOptions has no field or method Users)`.
   `findO2ORelationships` excludes by `r.Name` only and never applies the takeover
   (`context_table.go:3276-3290`). Before Phase 35 the same rename needed an
   `exclude_relationships` entry, which it honoured.

## 4. Options

### 4.1 Option A: detect belongs-to by default (recommended)

Every non-unique foreign key on a non-junction table gives two edges: today's O2M on the
referenced table, and a belongs-to O2O on the holder.

- **Wins:** every app gets parent navigation with no config, in Go and GraphQL. It reuses the
  tested belongs-to path: no new loader, no new query shape and no N+1. A declared belongs-to
  over the same key becomes a Phase 35 takeover, so existing configs keep their names. P2 measured
  churn of 1–5% per example.
- **Loses:** every app's generated surface grows by one field per foreign key, and goldens move
  widely. That's acceptable pre-release (no backward-compatibility constraint). A hidden FK
  column now exposes its parent object, which §32 already allows ("the nested relationship object
  remains exposable").

### 4.2 Option B: opt-in key (`generation.relationships.belongs_to: true`)

- **Wins:** no churn for anyone who doesn't ask.
- **Loses:** it adds a config key, and the default stays wrong for the common case: the example
  app would still have none. Every new adopter would have to find the switch.

### 4.3 Option C: document declaring belongs-to by hand

- **Wins:** nothing changes.
- **Loses:** one declaration per foreign key, which is the work detection exists to save.

### 4.4 Naming sub-options

- **N1, referenced table, singular** (`posts.author_id → users` gives `User`). Close to rule 5 as
  written, but it loses the role: two FKs into `users` need rule 6 anyway, and a single
  `reporter_id` reads as `User`.
- **N2, the naming member's stem** (recommended): `author_id` → `Author`, `parent_task_id` →
  `ParentTask`, `(workspace_id, cycle_id)` → `Cycle`. It falls back to the referenced table when
  there's no naming member, or when the stem is a column of the holder (`keyed_k` has no `_id` to
  strip). Rule 6 already names a disambiguated to-one edge this way, so N2 makes the default and
  the disambiguated name agree.

Recommendation: **A with N2.**

## 5. Proposed design

### 5.1 Parser (`parser/relationship.go`)

- `keyEdge` becomes `keyEdges`. For a foreign key that gives an O2M edge, it also returns a
  belongs-to O2O edge with source and target swapped and the same `Key` and `Constraint`. A
  unique foreign key still gives only its O2O edge, and a junction only its M2M edges.
- Every detected to-one edge is named `toOneName`: the naming member without `_id`, unless that
  is empty or names a column of the holder, in which case the referenced table's name. `BaseName`
  stays the referenced table's name, so `exclude_relationships: [users]` on `posts` drops it, as
  rule 6's base-name match does today.
- Rule 6 groups edges by `(SourceTable, TargetTable, to-one | list)`, not by `(SourceTable,
  TargetTable)` alone. Otherwise a self-referential FK's new belongs-to would rename its list edge
  `Tasks` → `ParentTaskTasks` (P2). §11 Q3.

### 5.2 Gen (`cmd/sqlgen/gen`)

- **Singular to-one names.** `relationshipToContext` singularizes a to-one name, as it pluralizes
  a list name (`Users *User` → `User *User`).
- **FK side from the key's holder.**
  - A detected edge reads its side from detection: the holder is the target of an O2M edge and
    the source of an O2O one. `inferredFKOnTarget` loses its column-name fallback.
  - A declared `o2o` whose `fk` columns exist on both tables (a self-reference, or a column
    sharing the referenced column's name) takes its side from the FOREIGN KEY constraint over
    those columns. With none, it is belongs-to. §11 Q4.
- **Collisions with a column.** A detected edge whose Go field equals a column's resolved field
  is omitted with a warning naming the column, the edge, and how to declare it under another name.
  This matches the other omitted-edge warnings (§4.13). A declared edge in the same position stays
  the existing `validateDuplicateFieldNames` error, now ungated for relationships. §11 Q2.
- **Chains walk the target's own edge set.** `findO2ORelationships` reads the target's merged
  relationships (exclusion by name or base name, takeover, the omissions above), so a chain never
  names a field the target struct lacks. It lands as N.3 (§11 Q5).

### 5.3 Generated API

Each detected belongs-to adds:
- a `*Target` struct field tagged `db:"-" json:"<snake>"`;
- a `*TargetFieldOptions` member;
- its LEFT JOIN branch in `resolveO2OJoins`, with chains;
- a nullable `<camel>: Target` GraphQL field and its field-options case;
- a manifest relationship.

The chain depth stays 6. P2 measured the size: no example grows by more than 5%.

### 5.4 Docs

- `docs/configuration.md` "Relationships": the to-one edge, its naming, and the collision warning.

## 6. Behavior contract

### 6.1 New and changed behavior

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| detect.belongs-to.single | A non-unique single-column FOREIGN KEY on a table that isn't a junction gives an O2M edge on the referenced table and a belongs-to O2O edge on the holding table, over the same key and constraint | §5.3 (proposed in §8) | all | `unit: parser: TestDetectRelationships/non-unique foreign key gives a list edge and a belongs-to edge`; `integration: parser/introspect: TestDetectRelationships_SourceParity` | relationship-detection: single-o2m × {postgres, mysql, sqlite, source-parity} | planned |
| detect.belongs-to.composite | A composite non-unique FOREIGN KEY gives the belongs-to edge over every pair, in the constraint's order | §5.3 (proposed) | all | `unit: parser: TestDetectRelationships/composite foreign key gives a belongs-to edge over every pair` | relationship-detection: composite-o2m × {postgres, mysql, sqlite, source-parity} | planned |
| detect.belongs-to.self | A self-referential non-unique FOREIGN KEY gives both edges on its own table | §5.3 (proposed) | all | `unit: parser: TestDetectRelationships/self-referential foreign key gives both edges` | relationship-detection: self-o2m × {postgres, mysql, sqlite, source-parity} | planned |
| detect.belongs-to.non-junction-fks | A three-FK table that isn't a junction gives a belongs-to edge for each foreign key, beside its three O2M edges | §5.3 (proposed) | all | `unit: parser: TestDetectRelationships/three foreign keys on a non-junction give three belongs-to edges` | relationship-detection: three-fk-junction × {postgres, mysql, sqlite, source-parity} | planned |
| naming.to-one.stem | A detected to-one edge is named after its naming member without `_id` (`author_id` → `author`) | §5.3 rule 5 (proposed) | all | `unit: parser: TestDetectRelationships/to-one edge named after its key column` | relationship-detection: disambiguation × {postgres, mysql, sqlite, source-parity} | planned |
| naming.to-one.fallback | A to-one edge with no naming member, or whose stem names a column of the holder, is named after the referenced table | §5.3 rule 5 (proposed) | all | `unit: parser: TestDetectRelationships/to-one edge whose stem is a column takes the table name` | relationship-detection: disambiguation × {postgres, mysql, sqlite, source-parity} | planned |
| naming.to-one.singular | A detected to-one edge's Go field and JSON tag are singular (`users` → `User`, `user`) | §5.3 rule 5, §8.5 (proposed) | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedToOneFieldIsSingular` | relationships: belongs-to × struct-field | planned |
| naming.rule6.by-cardinality | Disambiguation groups to-one and list edges separately, so a self-referential FK's list edge keeps its undisambiguated name | §5.3 rule 6 (proposed; §11 Q3) | all | `unit: parser: TestDetectRelationships/self-referential list edge keeps its name beside its belongs-to edge` | relationship-detection: self-o2m × {postgres, mysql, sqlite, source-parity} | planned |
| collision.detected-vs-column | A detected edge whose Go field equals a column's resolved field is omitted, with a warning naming the column and the edge | §4.13 (proposed; §11 Q2) | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DetectedEdgeOnAColumnFieldIsOmitted`; `unit: cmd/sqlgen/cli: TestGenerate_DetectedEdgeOnAColumnFieldWarns` | config-validation: detected-edge-column-collision × {fires, message text, severity} | planned |
| collision.declared-vs-column | A declared edge whose Go field equals a column's resolved field is an error, with or without a `column_map` override | §4.13 (proposed; §11 Q2) | all | `unit: cmd/sqlgen/gen: TestValidateDuplicateFieldNames/declared relationship on a column field` | config-validation: column-map-name-collision × fires | planned |
| fkside.detected | A detected edge's FK side comes from detection: a belongs-to joins `target.<referenced> = this.<fk>`, a self-referential one included | §13.1 (proposed) | all | `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_DetectedSelfReferentialBelongsToJoinsTheParent`; `example: sqlite: TestBelongsTo_SelfReferentialLoadsTheParent` | relationships: self × loader | planned |
| fkside.declared | A declared `o2o` whose `fk` columns exist on both tables takes its side from the FOREIGN KEY over them, else belongs-to | §13.1, §13.4 (proposed; §11 Q4) | all | `unit: cmd/sqlgen/gen: TestConfigRelationship_O2OSideFromForeignKey/self-referential`; `…/same-name key column` | relationships: belongs-to × loader | planned |
| chain.matches-target | A chained O2O join walks only the edges the target's own struct carries: none excluded by name or base name, and none taken over | §13.2, §13.4 (proposed; §11 Q5) | all | `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_ChainUsesTheTargetsEdges/takeover`; `…/excluded by base name` | relationships: belongs-to × loader | planned |
| loader.belongs-to.detected | A detected belongs-to loads its parent through the parent query's LEFT JOIN when selected, on `Get`, `GetMany`, `Paginate` and `Connection` | §13.2 (existing, for declared edges) | all | `conformance: GetMany/a detected belongs-to loads the parent row in the same query` | relationships: belongs-to × loader | planned |
| api.belongs-to.graphql | A detected belongs-to is a nullable `<camelName>: <Target>` field on the GraphQL type | §26.4 (existing) | all | `golden: graphql` | relationships: belongs-to × struct-field | planned |
| manifest.belongs-to.detected | The manifest lists a detected belongs-to as `kind: "o2o"`, with `fk.table` the holder and its `constraint` | §30.4.2 (existing) | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected belongs-to names its constraint` | relationships: belongs-to × struct-field | planned |
| takeover.belongs-to.detected | A declared belongs-to over a detected belongs-to's key takes its place (the postgres example's `audit.notes.Author` keeps its name) | §13.4 (existing) | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace/detected belongs-to over a non-unique key`; `golden: postgres` | relationships: belongs-to × struct-field | planned |

### 6.2 Regression guards

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| A non-self-referential O2M edge keeps its name and key | §5.3 | `parser: TestDetectRelationships` | guarded |
| A unique FK gives only its belongs-to O2O edge, no has-one | §5.3 rule 3, §13.1 | `parser: TestDetectRelationships` (single-o2o-unique, single-o2o-sole-pk) | guarded |
| A junction gives only its M2M edges, and a set-aside FK on one gives none | §5.3 rule 4 | `parser: TestDetectRelationships` (junction-m2m, tenant-junction-set-aside) | guarded |
| Detection gives the same answer under `files`, `database` and `both` | §5.3 | `parser/introspect: TestDetectRelationships_SourceParity`; `parser: TestDetectRelationships_BothSourceParity` | guarded |
| `exclude_relationships` matches the base name and the disambiguated name | §4.8, §5.3 rule 6 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_ExcludeByBaseName` | guarded |
| Two detected list edges left one name is an error naming both constraints | §4.13, §5.3 rule 6 | `cmd/sqlgen/cli: TestGenerate_DetectedNameCollisionIsAnError` | guarded |
| A declared edge over a detected key takes its place; over another key it replaces by field with a warning | §13.4 | `cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverDetectedKeyTakesItsPlace`; `…_DeclaredOverCompositeFK` | guarded |
| A declared has-one keeps `FKOnTarget` and joins the target's FK | §13.1 | `cmd/sqlgen/gen: TestBuildO2OJoinDetails_chainedFKOnTarget`; `examples/graphql: TestO2OJoin_HasOneDifferingPKNames` | guarded |
| A composite to-one key joins on every pair | §13.1 | `cmd/sqlgen/gen: TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair` | guarded |
| A to-one join adds the child's tenant predicate on every chain alias | §29 | `examples/tenancy: TestO2OChain_TenantWHEREOnEveryAlias`; `…_CrossTenantRowsFilteredAtEveryChainLevel` | guarded |
| A to-one join excludes a soft-deleted target | §17.3 | `cmd/sqlgen/gen: TestRelationshipsTemplate_softDeleteOnRelated` | guarded |
| A to-one join miss reads as nil | §13.2 | `examples/sqlite: TestO2OJoinMiss_ReadsAsNil` | guarded |
| A chained to-one load is one query | §13.2 | `examples/tenancy: TestO2OChain_SingleQueryForChainedLoad` | guarded |
| A chain into an excluded table is dropped | §13.2 | `cmd/sqlgen/gen: TestRelationshipTargets_ChainedO2OIntoExcludedTableDropped` | guarded |
| A to-one edge contributes no filter member | §11.1 | `cmd/sqlgen/gen: TestRelationshipFilters_ListRelationshipsOnly` | guarded |
| A belongs-to admits no nested verb | §9.9.1 | `cmd/sqlgen/gen: TestNestedEligibility_ListedShapeFailureIsReported/belongs-to edge` | guarded |
| The manifest splits `o2o` / `m2o` by `side` | §30.4.2 | `cmd/sqlgen/manifest: TestBuild_O2OSideDisambiguation` | guarded |
| `sqlgen_find_referencing` dedups by full tuple | §31 | `cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple` | guarded |
| A GraphQL `Connection` read with a to-one selection loads it | §26, §13.2 | `examples/graphql: TestO2OJoin_GraphQLConnection` | guarded |
| A classified (hidden) FK column hides the id while its relationship object stays exposable | §32 | none found | unguarded |
| Two declared edges on one field is an error | §13.7.3 | `cmd/sqlgen/gen: TestValidateResolvedFieldNames_DuplicateRelationshipField` | guarded |

### 6.3 Module matrix cells

#### relationship-detection (parser)

| FK shape | postgres | mysql | sqlite | source-parity |
|---|---|---|---|---|
| `single-o2m` | detect.belongs-to.single | detect.belongs-to.single | detect.belongs-to.single | detect.belongs-to.single |
| `composite-o2m` | detect.belongs-to.composite | detect.belongs-to.composite | detect.belongs-to.composite | detect.belongs-to.composite |
| `self-o2m` (new row) | detect.belongs-to.self, naming.rule6.by-cardinality | detect.belongs-to.self, naming.rule6.by-cardinality | detect.belongs-to.self, naming.rule6.by-cardinality | detect.belongs-to.self |
| `three-fk-junction` | detect.belongs-to.non-junction-fks | detect.belongs-to.non-junction-fks | detect.belongs-to.non-junction-fks | detect.belongs-to.non-junction-fks |
| `disambiguation` | naming.to-one.stem, naming.to-one.fallback | naming.to-one.stem, naming.to-one.fallback | naming.to-one.stem, naming.to-one.fallback | naming.to-one.stem |
| `junction-m2m` | guard (TestDetectRelationships) | guard | guard | guard (TestDetectRelationships_SourceParity) |

#### relationships (CLI codegen)

| Type | struct-field | loader | filter | nested-mutation |
|---|---|---|---|---|
| `belongs-to` | naming.to-one.singular, api.belongs-to.graphql, manifest.belongs-to.detected, takeover.belongs-to.detected | loader.belongs-to.detected, fkside.declared, chain.matches-target | n/a: §11.1, a to-one edge contributes no filter member | n/a: §9.9.1, shape 1 is never nestable |
| `composite-belongs-to` | detect.belongs-to.composite | guard (TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair) | n/a: §11.1, as above | n/a: §9.9.1, as above |
| `self` | detect.belongs-to.self | fkside.detected | n/a: §11.1, the to-one direction; the list direction is unchanged | n/a: §9.9.1 for the to-one direction; the list direction is unchanged |
| `circular` | n/a: a cycle is two edges, each covered by its own kind's row | n/a: same | n/a: same | n/a: same |

#### config-validation (CLI)

| Rule | fires | message text | severity | reported together | agrees with the JSON Schema |
|---|---|---|---|---|---|
| `detected-edge-column-collision` (new row) | collision.detected-vs-column | collision.detected-vs-column | collision.detected-vs-column (warning) | n/a: a warning is printed with the other omission warnings, never joined into an error | n/a: resolution-level, the config text cannot express it |
| `column-map-name-collision` | collision.declared-vs-column | guard (existing message) | guard (error) | guard | n/a: resolution-level |

### 6.4 Dialect differences

None. SQLite's unnamed foreign keys keep their `table(columns) → target(columns)` label on the
new edge, as on the O2M edge (§30.4.2).

## 7. Probe record

All on `f5dac70c`, 2026-11-20.

- **P1, detected edges across schema shapes.** A throwaway `gen_test` probe ran
  `DetectRelationships` and `BuildTableContexts` (postgres) over six schemas. `posts.author_id`
  gives `users: [Posts]` and nothing on `posts`. Adding `editor_id` renames it to
  `AuthorPosts` / `EditorPosts`. Two M2M edges into `users` are `TaskAssignees` / `TaskWatchers`.
- **P2, prototype** (`.claude/prototypes/BELONGS_TO_DETECTION.patch`). It covers the parser
  `keyEdges` and `toOneName`, singular to-one names, and detected-side `inferredFKOnTarget`.
  - `make update-golden-e2e` regenerated every example. Golden lines: graphql +0.9%, sqlite
    +1.2%, postgres +1.5%, mysql +1.6%, tenancy_postgres +1.0%, introspect_* +3–5%; cache and
    events unchanged.
  - To-one struct fields went from 7 to 18 (sqlite and postgres), from 4 to 13 (mysql) and from 0
    to 12 (tenancy_postgres).
  - The first run failed in gqlgen: `Field BoolKeyedNote.keyedK can only be defined once`, a stem
    equal to its own column. The column fallback fixed it.
  - `make static-examples`: the generated packages compile. Hand-written example tests in
    graphql, mysql, postgres and tenancy reference the old plural names (`r.Tasks`, `b.Users`)
    and need renaming.
  - A self-referential FK's list edge became `ParentTaskTasks` beside `ParentTask` (§11 Q3).
- **P3, wrong to-one joins at HEAD.** A SQLite probe module (sibling example directory,
  `GOWORK=off`) with a declared `Parent` over `tasks.parent_task_id` and `CodeRow` over
  `items.code → codes.code`. Generated joins: `p.parent_task_id = t.id` and `c.code = i.id`.
  Results: root `.Parent = child`, child `.Parent = nil`, and item `code='A'` `.CodeRow = one`.
  With P2's detected side, the joins are `p.id = t.parent_task_id` and `c.code = i.code`.
- **P4, column vs detected edge at HEAD.** `users(id, posts TEXT)` with `posts.user_id`:
  `sqlgen validate` prints "Config and schema are valid", `generate` succeeds, and `models_gen.go`
  declares `Posts *string` and `Posts []*Post` on `User`. With P2, `items(code)` gets a `Code`
  edge beside the `Code` column: `go build` fails with `Code redeclared`.
- **P5, chain after takeover at HEAD.** `posts(author_id UNIQUE → users)` with a declared
  `Writer` and `post_details(post_id PK → posts)`: `go build` fails with
  `fo.Posts.Users undefined (type *PostFieldOptions has no field or method Users)`.
- **Load path.** The graphql golden's `Get`, `Paginate` and `Connection` read through `GetMany`,
  which builds the O2O LEFT JOIN from field options (`models_gen.go:2106`, `:2204-2210`, `:5150`,
  `:5236`).

## 8. PRD and doc impact

- **§5.3 intro**, "each FOREIGN KEY constraint gives at most one edge" becomes: "each FOREIGN KEY
  constraint gives at most one edge in each direction. On a table that is not a junction, a
  foreign key gives a to-one edge on the table holding it, and, unless the key is unique, a list
  edge on the table it references."
- **§5.3 rule 3**, add: "An O2M edge also gives a belongs-to O2O edge on the holding table, over
  the same key and constraint. A unique key gives the O2O edge alone; its referenced side is
  config-declared (§13.1)."
- **§5.3 rule 5**, the O2O clause becomes: "a to-one edge after its naming member without `_id`
  (`author_id` gives `author`), or after the referenced table when the key has no naming member
  or the stem names a column of the holding table." Codegen spells a to-one name singular, as it
  spells a list name plural (§8.5).
- **§5.3 rule 6**, "When two or more edges share a source and a target table" becomes "When two or
  more edges of one cardinality (to-one, or list) share a source and a target table".
- **§13.1**, the belongs-to row's Detection cell becomes "every foreign key on a table that is not
  a junction". Remove "Auto-detection emits the O2O edge on the FK-holding side only" and add:
  "The has-one view from the referenced side stays config-declared."
- **§13.1, FK side**, add: "A detected edge's side is the one detection read. A declared `o2o`
  whose `fk` columns exist on both tables takes its side from the FOREIGN KEY over those columns,
  and is belongs-to when none does."
- **§4.13**:
  - New row: "A detected relationship resolves to a column's Go field | Warning | The edge is
    omitted, and the warning names the column and the edge. Declare the edge under another name
    to keep it ([13.4])."
  - The `column_map` row's last sentence becomes "Between two columns, only collisions an override
    introduces are reported. A declared relationship that claims a column's field is always an
    error."
- **§13.2**, add: "A chained join walks the edges the target's own struct carries: an edge
  excluded by name or base name, taken over (§13.4) or omitted (§4.13) is not chained."
- **`docs/configuration.md` "Relationships"**: the detected to-one edge, its naming and the
  collision warning.
- **`docs/tracker/backlog.md` line 280**: remove it; §3.4 item 2 measures it, and N.2 fixes it.

## 9. Plan

| # | Sub-item | Contract rows | Notes |
|---|---|---|---|
| N.0 | PRD sync | — | §8 edits and registry rows. Blocks everything |
| N.1 | Regression guards | §6.2 unguarded row (§32 hidden FK) | Pass on today's code |
| N.2 | FK side from the key's holder | fkside.detected, fkside.declared | Fixes §3.4 items 1–2. No golden moves except a self-referential or same-name declared `o2o` |
| N.3 | Chains walk the target's own edges | chain.matches-target | Fixes §3.4 item 4, the Phase 35 regression (§11 Q5) |
| N.4 | Detected-edge collisions with a column | collision.detected-vs-column, collision.declared-vs-column | Fixes §3.4 item 3 |
| N.5 | Belongs-to detection and to-one naming | detect.*, naming.*, loader.belongs-to.detected, api.belongs-to.graphql, manifest.belongs-to.detected, takeover.belongs-to.detected | Wide golden churn; renames the old plural to-one fields in example tests |
| N.6 | `sqlgen-example` probe | — | Regenerate; confirm `Task.project`, `reporter`, `cycle` and `parentTask` and a clean build |

## 10. Definition of done

The work closes only when every item below holds:

1. Every §6.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §6.2 row is guarded, and every guard still passes.
3. Every §6.3 cell is an ID or a stated `n/a`.
4. Every §8 edit has landed, and every §11 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too when the
   change reaches a runtime or driver package.
6. The `sqlgen-example` app generates and builds with `Task.project`, `Task.reporter`,
   `Task.cycle` and `Task.parentTask`, and with no new warning.

## 11. Rulings (settled 2026-11-20)

| # | Question | Ruling | Evidence |
|---|---|---|---|
| Q1 | How is a detected to-one edge named? | After its naming member without `_id` (`author_id` → `Author`). It falls back to the referenced table when there's no naming member or the stem names a column of the holder. Spelled singular. | §4.4 N2; P2's `keyed_k` collision drove the fallback |
| Q2 | What happens when a detected edge's Go field equals a column's? | The detected edge is omitted with a warning naming the column and the edge. A declared edge there is an error, with or without a `column_map` override. | P4: today it compiles twice with no diagnostic |
| Q3 | How does rule 6 group edges? | By cardinality: to-one and list edges are disambiguated separately, so a self-referential list edge keeps its name. | P2: `Tasks` → `ParentTaskTasks` under kind-agnostic grouping |
| Q4 | How does a declared `o2o` with `fk` columns on both tables pick its side? | From the FOREIGN KEY over those columns, else belongs-to. `side:` still drives only the manifest kind. | P3: today self-referential and same-name declarations join the wrong rows |
| Q5 | Where is the Phase 35 chain regression fixed? | Here, as N.3. PR #7 merges as is. | P5 |
| Q6 | Is there an off switch? | No config key. A detected belongs-to is dropped per table with `exclude_relationships`, by name or base name. | Same as every other detected edge |

## 12. Rejected alternatives

- **Option B (opt-in key)** and **Option C (docs only)**, §4.2–§4.3.
- **Suffixing a colliding name** (`CodeRef`). It invents a naming rule the user can't predict;
  the omission warning names the fix instead.
