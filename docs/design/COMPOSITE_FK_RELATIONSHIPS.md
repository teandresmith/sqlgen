# Relationships Through Composite Foreign Keys

> **Status: proposed (design record, for iteration).** Verified against source at `main`
> `e38ded5a` on 2026-10-16. A composite foreign key gives sqlgen no relationship, so the
> tenant-isolation pattern `TENANT_SCOPED_REFERENCES.md` §1.3 recommends, the composite
> `(workspace_id, x_id) → (workspace_id, id)`, removes the edge it guards. This proposes
> deriving the relationship from any composite foreign key whose referenced columns include
> the target's whole primary key. The rule reads the schema only, so it needs no config and
> gives the same answer on every dialect and every source. Sibling to PRD §5.3 (relationship
> detection), §13.4 (manual relationships) and §29.10 (tenancy and relationships).

## 1. Motivation

### 1.1 Symptom

`sqlgen-example` (the task-tracker reference app) enforces tenant isolation in the database.
Migration 0004 replaces the single-column foreign key `tasks.cycle_id → cycles(id)` with a
composite one, and 0005 does the same for `projects.team_id → teams(id)`:

```sql
ALTER TABLE cycles ADD CONSTRAINT cycles_ws_id_key UNIQUE (workspace_id, id);
ALTER TABLE tasks DROP CONSTRAINT tasks_cycle_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_cycle_same_ws
    FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id);
```

Generated with the CLI at `e38ded5a`, `source: files`:

```
Generation error: validating nested mutations: tables.public.cycles.nested_mutations.relationships: "Tasks" names no relationship on this table (PRD §4.8)
tables.public.teams.nested_mutations.relationships: "Projects" names no relationship on this table (PRD §4.8)
```

The app generated before only because of a bug. Before the introspection work, the
PostgreSQL parser did not name inline foreign keys, so `DROP CONSTRAINT tasks_cycle_id_fkey`
did nothing and the old single-column reference survived. With constraints named the way
the database names them, the drop applies, and `files` now agrees with the database: the
database has only the composite key. `source: database` on the old CLI would have lost the
edge too.

### 1.2 What a composite foreign key loses today

A minimal schema generated twice under tenancy, once with `tasks.cycle_id REFERENCES
cycles(id)` and once with the composite key above. The outputs differ only in the
relationship:

| Surface | Single-column FK | Composite FK |
|---|---|---|
| Manifest `Cycle.relationships` | `[{name: tasks, kind: o2m, fk: tasks.cycle_id}]` | `[]` |
| `Cycle.Tasks []*Task` field and its loader | present | absent |
| `CycleFilter` relationship member and client wiring (`taskClient`, `tasksDefaultSort`) | present | absent |
| Nested mutations over `Cycle.Tasks` | eligible | `names no relationship` |
| Increment operators (`TaskIncrementColumn`), measured with `bigint` keys | `cycle_id` excluded (PRD §8.2) | `TaskIncrementCycleID` generated |
| `tasks.cycle_id` comparator, create and update inputs | identical | identical |

So two things are lost. One is the edge. The other is the column's standing as a foreign-key
column: with integer keys, the composite key's FK column gets increment operators, which PRD
§8.2 rules out for foreign keys ("arithmetic on an identity").

### 1.3 Why this matters beyond one app

`TENANT_SCOPED_REFERENCES.md` §1.3 names the composite foreign key as "the strongest answer"
to cross-tenant references: the database enforces it, and nothing bypasses it. sqlgen should
not penalize a schema for taking that advice. Every tenanted→tenanted reference guarded this
way loses its relationship today, so the cost grows with the schema: that doc's reference
consumer has seven such references.

### 1.4 The workaround today

PRD §13.4 manual relationships restore the edge:

```yaml
tables:
  cycles:
    relationships:
      - name: Tasks
        type: o2m
        table: tasks
        fk: cycle_id
```

This was measured on `sqlgen-example`. With both edges declared, the app generates; it builds,
vets and passes its full test suite against the current runtime. The generated Go is also
byte-identical under `files`, `database` and `both`. But the workaround is hand-maintained
per edge, and `applyConfigDeclaredFKs` marks the reference `Synthetic`, the flag for "the
database does not enforce this". Here that is backwards: the database enforces it more
strictly than a plain foreign key would.

## 2. Root cause

Relationship detection (`parser.DetectRelationships`, `parser/relationship.go`) reads each
column's `FKReference`. Only `deriveFKReferences` (`parser/canonical.go`) sets it, from a
**single-column** FOREIGN KEY on that column: `fkReferenceOwner` skips any constraint with
`len(c.Columns) != 1`. A composite FOREIGN KEY stays in `Table.Constraints`, but it never
reaches a column, so it never reaches detection or any other check that keys off the column's
reference. The manifest does not list foreign keys as constraints at all (`manifest/builder.go`
skips them): it shows a foreign key only as a relationship, so a composite one appears nowhere
in it.

The skip is deliberate: a relationship is modeled as one FK column joined to the target's
key (`Relationship.FKColumn`). It is also silent. No warning says the composite key produced
nothing.

## 3. Options

| | Option | Gives the edge back | Config | Cost |
|---|---|---|---|---|
| **A** | **Key-covering rule.** A composite FK whose referenced columns include the target's whole single-column primary key is a reference through that column pair. | Yes, for any such FK | None | One schema pass, plus parity tests |
| B | **Tenant-column rule.** Reduce a composite FK only when its other columns are the configured tenant column on both sides. | Tenancy users only | `tenancy.column` must reach relationship detection | Couples relationships to tenancy; the parser module cannot read CLI config, so it needs a new parameter |
| C | **Document the §13.4 workaround.** | By hand, per edge | Per edge | Zero code; the `Synthetic` mismatch stays; every tenanted user repeats it |
| D | **Full composite relationships.** Join on every column pair. | Yes, and composite-PK targets too | None | Loaders, filters, nested connect, manifest `fk` shape and GraphQL all assume one FK column; priced in §8 |

**Recommendation: A.** It is sound for every schema, not just tenanted ones. If the
referenced columns include the target's primary key, each child row names at most one
target row through the PK column alone. The other column pairs only add equality conditions
the database already enforces. So loading through the PK pair returns exactly the rows the
composite key admits.

B is A narrowed to one naming convention, and it needs config at detection time. C stays the
answer for anything A does not cover. D solves a larger problem (composite-PK targets) that
no one has asked for yet; §8 prices it.

## 4. Proposed design (option A)

### 4.1 The rule

For a FOREIGN KEY `F` on table `C` with columns `(c1..cn)`, `n ≥ 2`, referencing table `T`
columns `(t1..tn)`:

1. `T` has a single-column primary key `pk`. It is the **resolved** key, after
   `tables.<t>.primary_key` overrides, since detection already runs after
   `applyPrimaryKeyOverrides`.
2. Exactly one `ti` equals `pk`, compared the way the dialect compares column names
   (`parser.SameColumnName`).
3. Then `ci` gets `FKReference{Table: T, Schema: T's schema, Column: pk}`. The other columns
   of `F` get nothing from `F`.

A column that already has a single-column FK keeps it. The derived reference fills only a
column that has none, so a schema mid-migration that holds both keys (as the example did
before 0004 dropped one) still yields one edge, not two. When two composite keys would give
one column a reference, the existing tie-break applies: the constraint whose name sorts first
on PostgreSQL and MySQL, the first declared on SQLite.

### 4.2 Where it runs

`deriveFKReferences` runs per table during canonicalization. At that point the target table
may not be parsed yet, and its primary key may still change through an override. The rule
therefore runs as a schema-wide pass at the start of `DetectRelationships`, the one point
where every table and every resolved primary key is known. All three CLI call sites
(`generate.go`, `pipeline.go`, `validate.go`) already call it after the schema is complete, so
the pass sees the same schema under `files`, `database` and `both`.

The pass must be idempotent. `validate` and `generate` may each run detection over a schema,
and canonicalizing again clears derived references (`deriveFKReferences` resets non-synthetic
ones).

### 4.3 What the derived reference turns on

Everything a single-column FK on the same column turns on, since the composite key is at
least as strong:

- the relationship (O2M, or O2O when `ci` alone is UNIQUE), with its loader, filter member,
  manifest edge, GraphQL field and nested-mutation eligibility;
- the "FK column" treatments: increment exclusion (PRD §8.2, `isIncrementEligible` checks
  `col.FKReference`) and PK strategy detection (`autoDetectPKStrategy`). The reference is
  schema-declared, so it is not `Synthetic`.
- The rule links only the column that points at the target's primary key. The composite
  key's other columns (`workspace_id`) stay incrementable unless something else excludes
  them; tenancy already excludes a tenant column (`!col.Tenant`). Q5 asks whether every column
  of a composite FK should count as an FK column for increment purposes.

Under tenancy (PRD §29.10), an O2M load runs the child's own `GetMany`, which already filters
by the child's tenant column. Loading through `cycle_id` alone is therefore tenant-scoped
without the extra pair, and the composite key guarantees the workspaces match anyway.

### 4.4 Nested mutations

`connect` writes only `ci` (`cycle_id`). The child's other columns (`workspace_id`) are
already set on the row, and tenancy pins them. A connect to a target in another tenant fails
before the write, because `connect` resolves the target through its own tenant-scoped client
(PRD §9.9). If that guard were ever bypassed, the composite key would reject the write with
an FK violation. `allow_reparent` is unaffected.

### 4.5 Interaction with tenant-scoped reference verification

`TENANT_SCOPED_REFERENCES.md` proposes a runtime hook that probes tenanted→tenanted
references. A reference derived from a composite key whose other pair is the tenant column
on both sides is already enforced by the database, so the hook could skip it. Whether to
record that ("tenant-guarded") on the reference is open (§7, Q3). This design does not need
it.

## 5. What this does not cover

- **Composite-PK targets.** `T`'s primary key is `(workspace_id, id)`, and the FK covers the
  whole key. There is no single column to join on, which is option D. These stay edgeless; see
  Q1 on whether to warn.
- **A composite FK that does not include `T`'s primary key**, for example one referencing a
  composite UNIQUE that does not contain the PK. The child row then may not identify one
  target row through any single column. No edge.
- **Single-column UNIQUE targets.** An FK covering a non-PK single-column UNIQUE key is
  equally sound (Q2). Left out until relationships to non-PK keys are confirmed to load
  correctly.
- **M2M junctions with a tenant column.** A junction such as `task_labels(workspace_id,
  task_id, label_id)` with `workspace_id REFERENCES workspaces` has three FK columns and is not
  M2M under PRD §5.3, whether its other keys are composite or not. That limit predates this
  design and is unchanged by it.

## 6. Plan

### 6.1 Parser (`parser/relationship.go`, `parser/canonical.go`)

- A `deriveKeyedReferences(schema)` pass at the start of `DetectRelationships`, implementing
  §4.1. It reuses `fkReferenceOwner`'s tie-break for the composite case.
- No change to `deriveFKReferences` or to the dialect parsers.

### 6.2 Tests

- **Unit**, table-driven in `parser/relationship_test.go`:
  - the tenant pattern gives O2M;
  - a UNIQUE child column gives O2O;
  - the reversed column order `(id, workspace_id)` still works;
  - a single-column FK on the same column wins;
  - two composite keys on one column follow the tie-break;
  - a composite-PK target gives no edge;
  - a PK override on the target moves the rule;
  - running the pass twice changes nothing.
- **Parity**: the composite key under `files` and `database` on PostgreSQL, MySQL and SQLite,
  through `assertSchemaParity` and one `parser/introspect` integration test per dialect. All
  three dialects support composite foreign keys. MySQL 8.4 is expected to require the
  referenced columns to form a unique key (unverified; `(workspace_id, id)` is one either way).
- **Example**: give `tenancy_postgres` (or the `graphql` example, alongside
  `TENANT_SCOPED_REFERENCES.md` §6) one composite tenant key and a relationship test over it.
  The golden churn is limited to that example.
- **Regression**: `sqlgen-example` generates with the two §1.4 manual relationships removed.

### 6.3 PRD

- §5.3: a paragraph stating the rule in §4.1 and the limits in §5.
- §13.4: name the composite-key case as covered by detection, so it no longer needs a manual
  relationship.
- §29.10: one sentence noting that a composite tenant key gives the same edge as a
  single-column one.

### 6.4 Blast radius

- **Generated output:** changes only for schemas that have a composite FK covering a
  single-column PK. Those gain the edge they would have with a plain FK. No example has one
  today, so existing goldens do not move.
- **Config:** no new keys.
- **Consumers:** additive. A manual relationship that duplicates a derived one replaces it
  with a warning (PRD §13.4), so `sqlgen-example`'s workaround keeps working until it is
  removed.

## 7. Open questions

| # | Question | Leaning |
|---|---|---|
| Q1 | Warn when a composite FK yields no relationship (composite-PK target, no covered PK)? It is silent today. | Yes, once per constraint, naming §13.4: loud beats silent, and it would have explained §1.1 at once. |
| Q2 | Include single-column UNIQUE targets (§5)? | Not in the first cut; check how a plain FK to a non-PK UNIQUE loads first. |
| Q3 | Record "tenant-guarded" on the derived reference for `TENANT_SCOPED_REFERENCES.md`'s hook? | Defer until that hook is built; derivable later from the constraint. |
| Q4 | Should the manifest relationship carry the composite constraint's name, so a reader can tell the edge came from one? | Undecided. The manifest lists no foreign-key constraints today, so this would be the first place one is named. |
| Q5 | Exclude every column of a composite FK from increment operators, not just the one the rule links? Incrementing `workspace_id` on such a row can only fail the key. | Yes. That is the §8.2 rule's intent, and it needs no relationship. |

## 8. What option D would cost

Probed at `e38ded5a` on 2026-10-16, to price full composite relationships against option A.
Running example: `tasks(workspace_id, cycle_id) → cycles(workspace_id, id)`.

### 8.1 What already exists

- **The parser keeps the data.** `parser.Constraint` stores a composite FK's `Columns` and
  `ReferenceColumns` on all three dialects and under every source. Only the relationship model
  (`parser.Relationship.FKColumn`, `JunctionLocalFK`, `JunctionReferenceFK`, one string each)
  reduces it to one column.
- **Composite primary keys already have query machinery** (`sql/builder.go:461-490`):

  | Case | PostgreSQL | SQLite | MySQL |
  |---|---|---|---|
  | One key | `"workspace_id" = $1 AND "id" = $2` | same, `?` | same, `?` |
  | Many keys | `("workspace_id", "id") IN (($1, $2), ($3, $4))` | same, `?` | `((a = ? AND b = ?) OR (a = ? AND b = ?))` |

  `<T>Filter.PKs` compiles to the OR-of-ANDs form everywhere, and keyset cursors expand a
  composite key to `(a > $) OR (a = $ AND b > $)`. The `<T>PK` structs, the opaque
  `JoinClause.On` and the FK load-key helpers are reusable as they stand.
- **The join's target side is never the FK's referenced columns.** Every loader, filter and
  nested path joins on the target's or parent's first PK column (`PKColumns[0]`: 5 uses in
  `context_table.go`, 2 in `context_relfilter.go`, 12 in `context_nested.go`). Option D must
  introduce a target key equal to `ReferenceColumns`. That is a new semantic, not a wider type.

### 8.2 Reads, per relationship shape

| Shape | Today | Under D |
|---|---|---|
| O2O (inline JOIN) | `ON c.id = t.cycle_id` | `ON c.workspace_id = t.workspace_id AND c.id = t.cycle_id`. `On` is opaque SQL, so no runtime change. |
| O2M (batched load) | collect parent PK[0] values, child `GetMany` with `cycle_id IN (…)`, group by `map[string][]*T` | collect `(workspace_id, id)` tuples, filter the child on `(workspace_id, cycle_id)`, group by a struct key. Skip a child with any NULL member (MATCH SIMPLE: such a row references nothing). |
| M2M (junction) | junction `local_fk IN (…)`, string-scanned maps, then target fetch | the junction holds 2×N columns: both steps become tuple filters with tuple-keyed maps |
| Relationship filter (`EXISTS`) | `sql.Exists(correlationColumn string, …)` (`sql/condition.go:202`) | N correlated columns: a public runtime API change. Composite-PK parents are skipped today (`context_relfilter.go:139`). |

Three constraints on the O2M and M2M loaders:

- **Tuple IN cannot be prefixed yet.** `sql.PrefixConditions` (`sql/condition.go:223`) on a
  tuple-IN condition renders `c.("workspace_id", "id") IN (…)` on PostgreSQL and SQLite, which
  is invalid SQL. It is latent today, because tuple IN reaches only unaliased write
  statements. Loaders must use OR-of-ANDs, or prefixing must be fixed first. MySQL's
  OR-of-ANDs prefixes correctly.
- **The child-side tuple filter has no public home.** `Filter.PKs` covers only the child's own
  key. The loader would append to the internal `Get<T>Input.conditions`, which the `filter:`
  and discriminator settings already use. It must append, not replace.
- **Placeholders double per key**, and the `IN` list is not chunked today. The loader must
  also force on every FK and every referenced column in the `FieldOptions` it passes, where
  today it forces only the parent PK.

### 8.3 Writes (nested mutations)

Mechanical work: write N columns instead of one (`FKFieldName`, `FKAssignExpr`,
`FKUpdateAssignExpr`, `FKUnlinkAssignExpr`); drop N columns from the child input; load every
referenced parent column; key the `ErrAlreadyRelated` owner map on a tuple. Decisions:

1. **Who writes a member column that is also the tenant column or part of the child's PK?**
   Proposed: such a column is *pinned*. It is verified against the parent and never written.
   Otherwise `SkipTenancy` plus `connect` could move a child to another tenant.
2. **Nullability and disconnect.** Under MATCH SIMPLE, unlinking nulls only the non-pinned
   members, and `FKNullable` must come from those columns, not one. The parser records neither
   the MATCH type nor ON DELETE.
3. **O2O detection.** Must the UNIQUE constraint cover the whole FK, or only its non-pinned
   columns?
4. **M2M with a shared tenant column.** Both FK halves write `workspace_id`, and the junction PK
   has three columns, while disconnect requires a two-column junction PK.
5. **Composite-PK parents and targets.** The nested-mutation eligibility gates (E1, E3) exclude
   them today. Lifting that is a project of its own, and it is the main payoff of D.

`allow_reparent` must never move a pinned column. Delete, cache, events and tenancy reads are
unaffected: there are no cascades, and none of them follow foreign keys.

### 8.4 Surfaces that need a decision

| Surface | Today | Under D |
|---|---|---|
| Config `relationships[].fk` (`config/config.go:722-725`, `schema/v1.json`) | one string; also `junction_local_fk`, `junction_reference_fk` | a list form; PRD §4.8 and the config reference |
| Manifest `fk: {table, schema, column}`, `junction: {local_fk, target_fk}` (both `manifest/types.go` copies) | one column | a list, which bumps `SchemaVersion` from `0.1.0` (`cmd/sqlgen/manifest/types.go:19`); `MANIFEST.md` |
| MCP `find_referencing` `fk_column` (`cmd/sqlgen/mcp/tools_graph.go:39,80-89`) | a string, also used as the dedup key | a list, and a new dedup key; `MCP.md` |
| Relationship naming (`parser/relationship.go`, the `_id` stem) | trims `_id` from the one column | a rule for N columns, for example the non-pinned column |
| M2M detection (PRD §5.3) | exactly two FK columns | exactly two FK *constraints* |

### 8.5 Size

- **Outside templates:** about 15 Go files. Roughly 12 are mechanical (parser model, contexts,
  config, both manifests, MCP); about 7 surfaces need the decisions above.
- **Templates:** the large ones. `get.go.tmpl` (all loaders), `nested.go.tmpl`, the filter
  template and `relationships.go.tmpl`.
- **Runtime:** `sql.Exists` with N columns, and the `PrefixConditions` fix.
- **Specs:** two JSON schemas, the PRD (§4.8, §5.3, §9.9, §13, §29.10, §30), `MANIFEST.md`,
  `MCP.md` and the configuration reference.

Generating single-column edges as the N = 1 case should leave existing goldens unchanged, but
every loader and nested template is rewritten.

### 8.6 Verdict

D is a phase, not a fix. What it buys beyond A is composite-PK targets, which no one has asked
for. A covers the tenant pattern with one schema pass and does not block D: a reference A
derives is the N = 1 projection of the same constraint, so D could replace it later without
changing what A's edges load.

### 8.7 Found along the way

| Finding | Status |
|---|---|
| An M2M whose target PK is not named `id` generates code that does not compile; the loader hard-codes `ID:` | Measured on `main`; filed as FIX-356 |
| A prefixed tuple-IN renders invalid SQL on PostgreSQL and SQLite (§8.2) | Measured in isolation; unreachable from generated code today |
| A single-column PK that is also a composite-FK member is classified by type in `autoDetectPKStrategy` (`context_table.go:1140`), since it has no `FKReference`; a UUID key would be minted by the client | Reading only. Option A fixes it for the member it links. |
| MySQL is marked as lacking tuple IN (`sql/mysql.go:103`), though MySQL 8 likely supports it | Unverified; affects only the shape of the SQL |
