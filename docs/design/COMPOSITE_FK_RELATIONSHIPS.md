# Relationships Through Composite Foreign Keys

> **Status: ruled, broken down as Phase 32 (2026-10-21; `docs/tracker/phase-32.md`, where N
> is 32). Option D: full composite relationships. Rulings R1–R7 settled 2026-10-20 (§7), each
> from a measured probe (§9).** Verified against source at `bd7783ea` on 2026-10-20; the
> §6 code references re-checked at `244ce2ee` on 2026-10-21; §9.1 re-verified at `244ce2ee`
> on 2026-10-22 (32.0), which wrote §4 into the PRD with the five rulings in §7.1. §10–§12
> (behavior contract, definition of done, contract rulings Q1–Q7) retrofitted 2026-11-05 at
> `716c15e1`, after 32.0–32.2 landed.
> sqlgen models a relationship as one foreign-key column joined to the target's first
> primary-key column. A composite foreign key therefore gives no relationship, so the
> tenant-isolation pattern `TENANT_SCOPED_REFERENCES.md` §1.3 recommends, the composite
> `(workspace_id, x_id) → (workspace_id, id)`, removes the edge it guards. This proposes
> making the relationship a foreign-key *constraint*: every pair of columns it joins, to
> whatever key it references. A single-column foreign key becomes the N = 1 case of the same
> model. Sibling to PRD §5.3 (relationship detection), §9.9 (nested mutations), §13.4 (manual
> relationships) and §29.10 (tenancy and relationships).

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

`sqlgen validate` at `bd7783ea`, `source: files`:

```
Error: validating nested mutations: tables.public.cycles.nested_mutations.relationships: "Tasks" names no relationship on this table (PRD §4.8)
Error: tables.public.teams.nested_mutations.relationships: "Projects" names no relationship on this table (PRD §4.8)
```

The app generated before only because of a bug. Before the introspection work, the
PostgreSQL parser did not name inline foreign keys, so `DROP CONSTRAINT tasks_cycle_id_fkey`
did nothing and the old single-column reference survived. With constraints named the way
the database names them, the drop applies, and `files` now agrees with the database: the
database has only the composite key.

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

The same schema style reaches further than the one shape in §1.1:

- **Tenant junctions.** `task_labels(workspace_id, task_id, label_id)` with
  `(workspace_id, task_id) → tasks` and `(workspace_id, label_id) → labels` is the guarded
  form of an M2M junction. It has no edge today, and it is not M2M under PRD §5.3 either way.
- **Composite-PK targets.** A table keyed `(workspace_id, id)` is the natural end of the same
  style, and PRD §29.7 already specifies composite-PK tenanted tables. A reference to one has
  no single column to join on. The nested-mutation eligibility gates (E1, E3) and the
  relationship filter (`context_relfilter.go:139`) already exclude composite-PK parents.

### 1.4 The interim workaround

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

Measured on `sqlgen-example` (§9.2): with both edges declared the app generates, builds, vets
and passes its full test suite. `applyConfigDeclaredFKs` marks the reference `Synthetic`, the
flag for "the database does not enforce this", which misdescribes a composite key. The flag
is harmless here: only `autoDetectPKStrategy` (`context_table.go:1171`) reads it, and only for
a primary-key column. This is the answer until the phase in §6 lands; it is not a fix.

## 2. Root cause

The relationship model holds one column per side. `parser.Relationship` carries `FKColumn`,
`JunctionLocalFK` and `JunctionReferenceFK`, one string each (`parser/schema.go:342`), and
detection (`parser.DetectRelationships`) reads only `Column.FKReference`. Only
`deriveFKReferences` (`parser/canonical.go`) sets that, from a **single-column** FOREIGN KEY:
`fkReferenceOwner` skips any constraint with `len(c.Columns) != 1`. A composite FOREIGN KEY
stays in `Table.Constraints`, complete on all three dialects and under every source, but it
never reaches detection. Nothing warns.

Past the parser, the target side of every edge is the target's first primary-key column, not
the columns the foreign key references. At `bd7783ea`, `PKColumns[0]` is read 6 times in
`context_table.go`, 2 in `context_relfilter.go` and 12 in `context_nested.go` for relationship
wiring, loading, filtering and nested writes.

So the gap is the model, not a missing detection rule. A rule that projects a composite key
onto one column (option A) restores one shape of edge and leaves the model where it is.

## 3. Options

| | Option | Gives the edge back | Config | Cost |
|---|---|---|---|---|
| A | **Key-covering rule.** A composite FK whose referenced columns include the target's single-column PK becomes a reference through that one column pair. | For that shape only | None | One parser pass (prototyped, §9.2) |
| B | **Tenant-column rule.** A narrowed to FKs whose other columns are the configured tenant column. | Tenancy users only | `tenancy.column` must reach the parser | Couples detection to config |
| C | **Document the §13.4 workaround.** | By hand, per edge | Per edge | Zero code; every tenanted user repeats it |
| **D** | **Full composite relationships.** A relationship is an FK constraint: N column pairs, joined to the key the FK references. | For every FK the database accepts | None for detected edges; `fk` takes a list for manual ones | A phase (§6) |

**Recommendation: D.** It is the only option that matches what the database enforces:

- **It covers the shapes §1.3 lists.** The tenant pattern, tenant junctions and composite-PK
  targets are one rule, not three.
- **The tenant pattern already references a UNIQUE key, not the PK.** `tasks_cycle_same_ws`
  references `cycles_ws_id_key`. Option D's target key is the FK's referenced columns, so it
  covers that shape directly, and FKs to any non-PK UNIQUE key with it (former Q2).
- **It settles the old open questions instead of deferring them.** Every FK constraint
  yields an edge, so the silent case shrinks to keys the database itself would refuse (former
  Q1). "Tenant-guarded" is visible in the model (former Q3). Every member of an FK
  constraint is an FK column (former Q5).
- **A would be thrown away.** An A edge loads through one column pair. Under D the same edge
  loads through the whole key, so its generated code changes shape. With no
  backward-compatibility constraint (pre-release), building A first only adds work. A also
  turns some tenant junctions into M2M edges that the current M2M code cannot write (§9.3).

B is A narrowed to one naming convention. C stays the interim answer (§1.4).

## 4. Proposed design (option D)

### 4.1 The model

`parser.Relationship` replaces its three column strings with key lists:

```go
type KeyPair struct {
    Local      string // a column on the table holding the foreign key
    Referenced string // the column it references on the other table
}

type Relationship struct {
    // ... Name, BaseName, Type, Side, SourceTable, TargetTable, Filter, Sort as today
    Constraint string    // the FOREIGN KEY's name; "" for a manual relationship
    Key        []KeyPair // O2O / O2M: child FK columns → parent key columns
    // M2M: two FK constraints on the junction
    JunctionTable     string
    JunctionLocal     []KeyPair // junction → this table
    JunctionReference []KeyPair // junction → target
    JunctionLocalConstraint, JunctionReferenceConstraint string // each junction FK's name (§4.7's manifest `junction`)
}
```

`Key` is in the FK's declared column order. A single-column foreign key is a one-pair
`Key`. `FKColumn`, `JunctionLocalFK` and `JunctionReferenceFK` are removed, not kept beside
the lists.

### 4.2 Detection

One edge per FOREIGN KEY constraint (PRD §5.2's canonical model), read from
`Table.Constraints` after `ResolveReferenceNames` and `applyPrimaryKeyOverrides`. The existing
call sites (`generate.go`, `pipeline.go`, `validate.go`) already run detection there.

1. **Target key.** The referenced columns must be, as a set, the target's resolved primary
   key (the column flags after `primary_key` overrides), its declared PRIMARY KEY constraint
   (§7.1), or one of its non-partial UNIQUE constraints. A single-column FK with no column list (`REFERENCES t`) references the
   primary key, as today; on SQLite, empty `ReferenceColumns` means the parent key could not
   be resolved. Any other FK yields no edge and a warning naming the constraint and §13.4.
   SQLite FKs are usually unnamed, so the warning (and the manifest's `constraint`, §4.7)
   falls back to `table(columns) → target(columns)`. PostgreSQL refuses such a key at DDL
   time. MySQL 8.0 accepts one to any indexed columns, and 8.4 refuses it only while
   `restrict_fk_on_non_standard_key` is on (the default; corrected at 32.0, unprobed). SQLite
   accepts it and fails at DML time. So the warning reaches MySQL and SQLite under either
   source. This drops N = 1 edges today's detection emits: a SQLite single-column FK to a
   non-key column (fixture 10b, §9.4), and the MySQL equivalent. That edge's loader joins the
   wrong column (§4.9), so the change is deliberate.
2. **Redundant FKs.** An FK whose (local → referenced) pairs are a subset of another FK's
   pairs to the same target gives no edge; of two identical FKs, the one whose name sorts first
   is kept, and of two unnamed ones (SQLite) the first declared (ruled 2026-10-23: no source
   may answer differently, and the MySQL catalog keeps no declared order).
   A schema mid-migration that holds both `cycle_id → cycles(id)` and
   `(workspace_id, cycle_id) → cycles(workspace_id, id)` gets one edge, through the composite
   key. Without this step both edges are emitted with colliding names (§9.4, fixtures 12c,
   12e). Ruled under R6.
3. **O2O or O2M.** O2O when the child has a PRIMARY KEY or non-partial UNIQUE whose columns
   are a subset of the FK's columns. A unique subset makes the whole FK unique, so this is
   today's "FK column with UNIQUE" rule extended to N columns. Otherwise O2M.
4. **M2M.** A table is a junction when, after setting aside every FK constraint whose columns
   are all members of the table's other FK constraints, exactly two FK constraints remain and
   a PRIMARY KEY or non-partial UNIQUE covers the union of their columns. "Covers" means
   contains, as PRD §5.3 says today. The columns may overlap. Under tenancy, a junction is
   then M2M exactly when the database guarantees both ends share a tenant:
   `workspace_id REFERENCES workspaces` is set aside because the two composite keys cover
   `workspace_id`, while a junction with three single-column FKs keeps three constraints and
   stays three O2Ms, as today. Ruled under R6, with two stated consequences:
   - **A set-aside FK gives no edge on a junction**, as a junction's FKs give only the M2M
     today. `workspaces → task_labels` has no O2M.
   - **A set-aside FK can be a real parent, not only the tenant column.** `order_items` with
     `order_id → orders`, `product_id → products` and
     `(warehouse_id, product_id) → stock` links orders ↔ stock: the products FK lies inside
     the stock key and is set aside, so `Order.Products` is not detected (a §13.4 manual
     relationship restores it). `shipments` with `order_id → orders` and
     `(order_id, line_no) → order_lines` links order lines ↔ carriers. Both shapes need a
     composite FK, so no schema without one changes.
5. **Naming.** The `_id` stem comes from one member column, the *naming member*, chosen by
   the first step that yields exactly one column (ruled under R5):
   1. the only member neither in the child's primary key nor a member of another FK
      constraint on the child;
   2. else the only member not a member of another FK constraint, primary key ignored;
   3. else the member paired with the target's single-column primary key;
   4. else none: the edge keeps its base name.

   The tenant pattern names `Tasks` from `cycle_id`, as a single-column FK would. Step 2 names
   the two directions of `cycle_notes(workspace_id, cycle_id, prev_cycle_id, n)`, whose every
   member is in its primary key, `cycle` and `prev_cycle`; without it both were `cycle_notes`.
6. **Self-references and disambiguation** run as today, keyed on the naming member. A name
   still shared by two edges on one table after disambiguation, once `exclude_relationships`
   and manual replacements apply (§7.1), is a hard generation error
   naming both constraints and pointing to §13.4 (`exclude_relationships` plus a manual
   relationship), never two fields of one name. No measured schema reaches it (§9.4).

Detection runs on the schema alone, so it gives the same answer under `files`, `database` and
`both`.

### 4.3 Pinned members

Some members of an FK constraint must never be written through the relationship, because
writing them would change something other than the edge. A member is **pinned** when it is:

- the table's tenant column (PRD §29), which tenancy already writes and guards;
- part of the child's primary key, so writing it would change the row's identity; or
- a member of another FK constraint on the same table, so writing it would move another
  edge. `workspace_id` in `tasks` is pinned twice over: tenant column and shared with
  `tasks.workspace_id → workspaces`.

The rest are **free** members. Pinning needs the tenancy config, so it is computed in `gen`,
not the parser. A relationship with no free member has no verb that rewrites a row (connect,
disconnect, clear, reparent) and keeps `create`, as a NOT NULL edge does (§7.1). Ruled under
R1; each kind was measured to do harm when written (§9.4). Pinning applies to O2M and has-one
edges only: M2M link and unlink insert and delete junction rows and never rewrite a member.

Pinning governs rewriting an existing row (connect, `allow_reparent`). A nested `create`
sets every member from the parent's referenced columns, the tenant column included; when
tenancy also sets it, the two must agree (§7.1).

Two existing rules extend to every member rather than adding pinned kinds (R1):

- **E1 covers every member**, the tenant column included (§7.1). Each must be a field of the target's create input,
  as E1 requires of the one FK column today. A generated column is not, and the database
  refuses to write one (`column "cycle_id" can only be updated to DEFAULT`), so an edge with a
  generated member is read-only, exactly as a generated single-column FK is today
  (measured: `"cycle_id" is absent from the target's create input`).
- **Every member must be in the generated model on both sides.** An edge with a member
  removed by `exclude_columns`, on the child or the parent, gives no edge and a warning
  naming the column and §13.4; a manual relationship over an excluded link column is an
  error, since the user declared it by name. Before FIX-358 an excluded single-column FK still
  yielded its edge: an O2M one did not compile, while O2O and M2M edges built and loaded
  (measured, FIX-358 vet). The rule drops every shape anyway (ruled 2026-10-21 under
  FIX-358): `exclude_columns` means "not in the model", and `access: hidden` (PRD §32) hides
  a column while keeping its edge. FIX-358 landed this rule for N = 1.

### 4.4 Reads

| Shape | Today | Under D |
|---|---|---|
| O2O (inline JOIN) | `ON c.id = t.cycle_id` | `ON c.workspace_id = t.workspace_id AND c.id = t.cycle_id`. `JoinClause.On` is opaque SQL, so no runtime change. |
| O2M (batched load) | collect parent `PKColumns[0]`, child `GetMany` with `cycle_id IN (…)`, group by `fkLoadKey` string | collect the parent's referenced-column tuples, filter the child on its FK members, group by an `[N]string` of `fkLoadKey`s |
| M2M (junction) | junction `local IN (…)`, typed scan (FIX-357), map, target fetch on the target key | per parent chunk, the junction step filters on a tuple and the target fetch matches the target key against `(SELECT j.ref … FROM junction j WHERE (j.local …) IN (chunk))` (FIX-359's junction subquery, with tuple IN); maps key on `[N]string` |
| Relationship filter (`EXISTS`) | `sql.Exists(correlationColumn string, …)`, composite-PK parents skipped | correlates on every pair; the composite-PK skip is lifted |

Rules shared by every loader:

- **The parent side is the referenced columns, not the parent's PK.** For the tenant pattern
  that is `(workspace_id, id)` on `cycles`. The loader forces every referenced column on in
  the parent's `FieldOptions` and every FK member on in the child's. Today it forces only
  the parent PK.
- **NULL members match nothing.** A child with any NULL member references no row under MATCH
  SIMPLE, and a parent key with a NULL member (a UNIQUE key may hold one) is referenced by
  nothing. Both are skipped before the query (R2).
- **The tuple filter is internal.** It is appended to `Get<T>Input.conditions`, which the
  `filter:` and discriminator settings already use. It must append, not replace. `Filter.PKs`
  is the child's own key and is not reused.
- **Composite keys reuse `fkLoadKey`.** FIX-357 made each key column scan into its own Go type
  and stringify through `fkLoadKey`, which handles binary and network keys. An `[N]string` of
  those is comparable, so tuple maps need no new key type.
- **Tenancy is unchanged.** An O2M load runs the child's own `GetMany`, which filters by the
  child's tenant column (PRD §29.10). The tuple adds `workspace_id = …` again, which the
  composite key guarantees anyway.
- **Every step is chunked** at `floor(maxParams / 2 / N)` parent keys per query, where
  `maxParams` is the dialect's bind limit (SQLite 32766, MySQL 65535, PostgreSQL 65535) and
  `N` the key's column count. Half the budget is left for the consumer filter, tenancy,
  soft-delete and `filter:` parameters; a chunk of exactly 32766 keys failed on SQLite once a
  one-parameter consumer filter was added (§9.4). Ruled under R4, refined by the FIX-359
  rulings (2026-10-21):
  - **The limit is a generator-side literal**, not runtime API: the funcmap's
    `loadChunkSize N` bakes `floor(limit / 2 / N)` into the template (16383 on SQLite, 32767
    on PostgreSQL and MySQL for N = 1). Tuple loaders pass the key's column count.
  - **The M2M target fetch chunks by parent, not by target key.** Per parent chunk it matches
    the target key against a junction subquery over that chunk. Chunking on target keys
    silently mis-sorts each parent's slice once a load's distinct targets exceed a chunk
    (measured: 2 parents × 16400 users, name DESC, each slice split at "…-16383" /
    "…-16384"), breaking PRD §13.2's per-parent order.
  - **The pgx `= ANY($1)` O2M path stays one query**, since it binds one array parameter.
    Every other driver chunks the O2M step; both M2M steps chunk on every driver, pgx
    included. A composite-key O2M on pgx expands per key, so it chunks too.
  - PRD §25.1 becomes "one query per step per chunk", with a dialect clause for the pgx O2M
    path; unchanged below the chunk size. FIX-359 lands it for N = 1, which fails today, and
    this design's tuple loaders reuse it.

There is no single-pair projection, even when the referenced columns include a single-column
primary key. One loader shape for every N keeps one code path, and N = 1 generates what it
generates today (§4.8).

### 4.5 Writes (nested mutations)

- **Connect identifies the target by its primary key.** The input is the target's PK, or its
  `<T>PK` struct for a composite key, as `Get` takes it. The target is loaded through its own
  tenant-scoped client (PRD §9.9), which reads its referenced columns.
- **Pinned members are verified, free members are written.** A pinned member must equal the
  target's referenced value, or the connect fails with `ErrAlreadyRelated` before any write
  (§7.1). Free
  members take the target's values. `allow_reparent` never moves a pinned member.
- **Disconnect nulls the free members.** Under MATCH SIMPLE it is eligible when every free
  member is nullable. Under MATCH FULL it is eligible only when every member is free and
  nullable, because PostgreSQL refuses a key that mixes NULL and non-NULL members but accepts
  an all-NULL one (measured). `FKNullable` is computed from the free members, not one
  column. The parser records `Constraint.Match` for PostgreSQL only (`FkMatchtype` from
  `pg_query` in the file parser, `pg_constraint.confmatchtype` in introspection); MySQL and
  SQLite are always SIMPLE, because neither enforces MATCH FULL (§9.4). Ruled under R2. A
  single-column FK is unaffected: nulling its one member is all-NULL.
- **`ErrAlreadyRelated`** keys its owner map on the `[N]string` tuple.
- **M2M link and unlink** insert and delete the junction row by the union of both key lists.
  Disconnect requires a junction PRIMARY KEY equal to that union, which replaces the two-column
  rule (§7.1).
- **Composite-PK parents and targets** stay ineligible in this phase: E1 and E3 keep
  excluding them, and they get every read surface (loaders, relationship filters, manifest).
  Lifting the gates is the next phase (R3). It needs a GraphQL `<T>PKInput` per composite-PK
  table, since a composite `Get` takes separate arguments today
  (`workspaceSetting(workspaceID: UUID!, key: ID!)`), and E8's opaque-key rule extended to
  key structs, since a `<T>PK` holding `[]byte` or `net.IP` is not a Go map key. The model
  above needs no change for it.

The nested child input (`<Parent><Edge>CreateInput`) drops every member of the traversed key,
the tenant column included, as it drops the one FK column today, since a nested `create` sets
them all from the parent. When tenancy also sets the tenant column, the two must agree, or it is
`ErrAlreadyRelated` (§7.1).

### 4.6 Column treatments

Every member of any FK constraint counts as an FK column:

- **Increment exclusion (PRD §8.2).** `isIncrementEligible` and `incrementBlockedBySQLType`
  read FK membership from the constraints, not `col.FKReference`. `workspace_id` on `tasks`
  can only fail the key if incremented.
- **PK strategy (PRD §8.6).** A single-column PK that is a member of any FK constraint is
  `caller`. Today a composite-FK member is classified by type in `autoDetectPKStrategy`, so a
  UUID key would be minted by the client.
- **Comparator typing.** `Column.FKReference` stays a single-column property. It types the
  column's comparator for loaders that pass `&comparator.ID{In: …}`. Tuple loaders build their
  condition internally and do not need it.

### 4.7 Surfaces

| Surface | Today | Under D |
|---|---|---|
| Config `relationships[].fk`, `junction_local_fk`, `junction_reference_fk` (`config/config.go:722-725`, `schema/v1.json`) | one string | a string or a list (R7). A list maps by position onto the resolved primary key of the table it references, and its length must equal that key's column count, or validation fails. A string is a one-item list, so today's configs mean what they mean now. There is no `references` key: a manual edge joins the primary key, as PRD §13.4 says today. Decoded like `Operations` and `NullableVariant` (`config/config.go:564,630`); `oneOf` string / string array in `v1.json`. |
| Manifest `fk: {table, schema, column}`, `junction: {local_fk, target_fk}` (`manifest/types.go`, `cmd/sqlgen/manifest/types.go`) | one column | `fk: {table, schema, columns, references, constraint}`, `junction` with column lists; `SchemaVersion` `0.1.0` → `0.2.0` (`cmd/sqlgen/manifest/types.go:19`); `MANIFEST.md`. Here `references` is the manifest's record of the referenced columns, not a config key. |
| MCP `find_referencing` (`cmd/sqlgen/mcp/tools_graph.go:39,80-89,119`) | `fk_column` string, also the dedup key | `fk_columns` list; the dedup key compares the joined list; `MCP.md` |
| GraphQL | relationship fields, filter inputs, connect by target ID | field shapes unchanged; connect takes the target's PK input, which for a composite key is the PK input type |

The manifest names the constraint behind each detected edge (former Q4). It is the first
place a foreign-key constraint is named in the manifest, and it shows where an edge came from.

### 4.8 Runtime

- **`sql.Exists` with N correlation columns.** The subquery referenced one `CorrelationToken`;
  it needs one per pair. This is a public API change, which pre-release allows. 32.1 made
  `Exists` take `string | []string`, so a one-pair call site is unchanged, and added
  `CorrelationTokenAt(i)`, where index 0 is `CorrelationToken`.
- **`sql.PrefixConditions` on tuple IN.** A prefixed tuple-IN renders
  `c.("workspace_id", "id") IN (…)` on PostgreSQL and SQLite, which is invalid SQL. Measured:
  PostgreSQL 17 `syntax error at or near "("`, SQLite `near "(": syntax error`. 32.1 made
  the batch condition a structured value that `PrefixConditions` qualifies per column.
- **MySQL tuple IN.** `MySQLDialect.SupportsTupleIN` returned false; 32.1 removed the flag,
  and composite-key batches use tuple IN on every dialect.
  Measured on 8.0.46 and 8.4.11 through go-sql-driver: `(a, b) IN ((?, ?), …)` works, and
  EXPLAIN shows the same composite-index range scan as OR-of-ANDs. It is far faster: 32 ms vs
  1.49 s at 10000 pairs, 122 ms vs 2 min 3 s at 32767. This also
  speeds up today's composite-PK batch operations on MySQL (`BuildCompositePKBatchCondition`).
  `<T>Filter.PKs` does not go through it: it renders OR-of-ANDs on every dialect (§9.1), and
  moving it is a backlog item (§7.1).
- **No runtime API for the chunk size.** FIX-359 keeps the bind limit in the generator
  (§4.4), so `sql.Dialect` gains no `MaxParams()` method (ruled 2026-10-21).

### 4.9 The N = 1 invariant

For every foreign key that references its target's single-column primary key, the generated
Go must be byte-identical to today's. Every existing example has only such keys (each
non-`id` reference in `testdata/examples`, `owners(owner_key)`, `badges(code)`, `hosts(addr)`
and `fingerprints(digest)`, is that table's PK). So the Go goldens must not move when the
model changes, and that is the regression gate for every sub-item. The manifest does move
(§4.7): every edge's `fk` changes shape.

Intended changes in output for N = 1, all on schemas no example has:

- **A single-column FK to a non-PK UNIQUE column** now loads through the referenced column.
  Today the loader filters the child's FK by the parent's primary key
  (`TargetPKColumn`, `context_table.go:206-207`), and the generated code does not compile.
  Measured by the FIX-358 vet at `bd7783ea`: `users(id INT PRIMARY KEY, email TEXT NOT NULL
  UNIQUE)`, `invites.email REFERENCES users(email)` → `cannot use
  &comparator.NullableNumber[int32]{…} as *comparator.NullableID value in struct literal`;
  the PK-override form (`cycles(id PK, code UNIQUE)`, `tasks.cycle_id REFERENCES
  cycles(id)`, `primary_key.columns: [code]`) → `cannot use &comparator.NullableID{…} as
  *comparator.NullableNumber[int32] value`. Folded into this phase rather than filed
  (ruled 2026-10-21): N.4 and N.5 replace the same `PKColumns[0]` wiring, and both
  schemas become regression tests there.
- **A SQLite single-column FK to a non-key column** loses its edge and gains a warning
  (§4.2 rule 1). Measured: today it emits `O2M workspaces->cycles` via `workspace_slug`.
  A MySQL single-column FK to a non-unique indexed column (8.0, or 8.4 with
  `restrict_fk_on_non_standard_key` off) does the same; not measured.
- **An excluded FK column** loses its edge (§4.3), landed early by FIX-358. Before it, an O2M one
  did not compile; O2O and M2M ones worked and are dropped by ruling.
- **A column with single-column FKs to two tables** gives an edge to each (32.2). Today
  `deriveFKReferences` gives the column one reference, so it gives one edge.

## 5. What this does not cover

- **A foreign key to a table outside the generated schema** (another schema not introspected,
  or an excluded table). No edge, as today.
- **A foreign key whose referenced columns are not a key** (§4.2 rule 1). No edge, with a
  warning.
- **MATCH FULL and MATCH PARTIAL.** Detected and read like SIMPLE. FULL gates disconnect on
  PostgreSQL only (§4.5, R2). PARTIAL is rejected at DDL time by PostgreSQL and by our
  PostgreSQL parser (`MATCH PARTIAL not yet implemented`), and ignored by MySQL and SQLite.
- **ON DELETE / ON UPDATE.** Not recorded. Their actions take effect on all three engines
  (MySQL cascade measured), so whether to record them is its own question, not R2's.
- **Composite-PK parents and targets in nested mutations.** The next phase (R3, §4.5).
- **Manual relationships to a non-PK key.** A manual edge joins the primary key, as today
  (R7).
- **Polymorphic references** (PRD §13.7) stay manual relationships with `filter:`.

## 6. Plan

One phase. N is the next free phase number at `/phase` time. Two filed fixes land first,
because they fix today's N = 1 code on the same sites with the rule this design reuses:

- **FIX-358:** an excluded FK column still yields its edge; an O2M one does not compile
  (§4.3's missing-member rule).
- **FIX-359:** loaders fail past the dialect's bind limit (§4.4's chunking: a generator-side literal, the M2M junction subquery, pgx O2M left as one query).

Sub-items in dependency order:

| # | Sub-item | Notes |
|---|---|---|
| N.0 | **PRD sync.** §4.6/§4.8 config reference, §5.3, §8.2, §8.6, §9.9 (E1 over every member, pinning, MATCH FULL disconnect), §13.4, §25.1 (chunks), §29.10, §30, `MANIFEST.md`, `MCP.md`. Re-run §9.1 at HEAD. MATCH, MySQL tuple IN and the non-PK FK loader failure are already measured (§9.4, §4.9). | Blocks everything |
| N.1 | **Runtime.** N-column `Exists`; tuple-IN prefixing; MySQL tuple IN (flag removed). | Independent of N.2 |
| N.2 | **Parser model and detection.** `KeyPair`, `Relationship.Key`, `Constraint.Match` (PostgreSQL file parser and introspection), detection per §4.2 including redundant FKs and the naming rule, the unresolvable-key warning (printed after detection, not through `schema.Warnings`, which is printed first), the residual-name-collision error. Parity under `files` and `database` on all three dialects. Starts from the §9.4 prototype. | Go goldens unchanged |
| N.3 | **Config, manifest and MCP shapes.** §4.7. Manual relationships produce the same `Key` lists. | Manifest goldens move once |
| N.4 | **Gen contexts.** Key pairs replace the `PKColumns[0]` reads in relationship code; pinned and free members; E1 over every member; FK membership for increments and PK strategy (§4.6). | Go goldens unchanged |
| N.5 | **Read templates.** O2O join, O2M and M2M loaders (chunked, tuple keys), relationship filters. | `get.go.tmpl`, the filter template, `relationships.go.tmpl` |
| N.6 | **Nested writes through composite FKs.** §4.5 for single-column-PK parents and targets, including MATCH FULL disconnect eligibility. | `nested.go.tmpl` |
| N.7 | **GraphQL.** Relationship fields and filters for the new edges. | |
| N.8 | **Examples.** `tenancy_postgres` gains a composite tenant FK, a tenant junction (M2M) and a composite-PK target (read surfaces only), each with load and filter tests, and nested tests where eligible. `mysql` and `sqlite` each gain one composite FK. A `sqlgen-example` probe against HEAD generates and passes with no manual relationships (that repo carries no §1.4 workaround; it fails `validate` today, §1.1). | New goldens only |

**Next phase (R3):** composite-PK parents and targets in nested mutations. Lift E1 and E3;
GraphQL `<T>PKInput` types; E8's opaque-key rule for key
structs.

### 6.1 Tests

- **Parser, table-driven** (`parser/relationship_test.go`): the tenant pattern gives O2M;
  a UNIQUE subset gives O2O; reversed column order; FK to a non-PK UNIQUE; FK to a composite
  PK; an FK whose referenced columns are not a key gives no edge and a warning; a tenant
  junction is M2M and a three-single-FK junction is not; `cycle_settings` (workspace FK plus
  composite FK, UNIQUE on both) is O2O, not M2M; the redundant single-plus-composite and
  duplicate-FK cases give one edge; `order_items` and `shipments` set aside a real parent;
  naming for each R5 step, including `cycle_notes`; a residual collision is an error; a PK
  override moves the target key; running detection twice changes nothing. The §9.4
  prototype's fixtures are the starting set.
- **Parity:** each shape above under `files` and `database` on PostgreSQL, MySQL and SQLite,
  through `assertSchemaParity` and the `parser/introspect` integration tests. MySQL 8.4 is
  documented to require the referenced columns to form a unique key by default
  (`restrict_fk_on_non_standard_key`; not probed here). `(workspace_id, id)` is one either
  way.
- **Runtime:** `Exists` with 1 and 2 pairs on every dialect; prefixed tuple IN; MySQL tuple
  IN.
- **Gen:** pinned and free classification (tenant, PK member, shared member); E1 over every
  member (a generated member is read-only); disconnect eligibility under SIMPLE and FULL;
  increment and PK-strategy treatment of every member.
- **Loaders:** an N = 2 load past one chunk on every dialect, checking every child set.
- **E2E:** the N = 1 invariant (§4.9) on every sub-item; the N.8 examples against live
  databases, including a connect across tenants that must fail before the write.

### 6.2 Blast radius

- **Generated Go:** unchanged for every existing example (§4.9). Schemas with composite FKs
  gain the edges a single-column FK would have, plus the shapes in §1.3.
- **Manifest:** `fk` and `junction` change shape for every edge; `SchemaVersion` bumps. MCP
  output follows.
- **Config:** `fk` and the junction keys accept a list (R7). No new key.
- **Runtime:** `sql.Exists` changes signature; MySQL composite-key batches switch to tuple IN.
- **Detection on schemas with composite FKs:** a junction's ends can move (§4.2 rule 4).
- **Consumers:** a manual relationship that duplicates a detected one replaces it with a
  warning (PRD §13.4), so a consumer that applied the §1.4 workaround keeps working until it
  removes it.

## 7. Rulings (settled 2026-10-20)

Each ruling followed a measured probe (§9.4) and took the recommended option.

| # | Question | Ruling | Evidence |
|---|---|---|---|
| R1 | Is the pinned set complete? | **Yes: tenant column, child-PK members, members shared with another FK.** Plus E1 over every non-tenant member, and every member must be in the generated model on both sides or the edge is dropped with a warning (§4.3). | PG 17: writing the tenant column moved a task to workspace 2 while its project stayed in 1; a shared member raised a raw FK error when the other edge was linked and silently moved the row when it was NULL; a PK member re-keyed the row. A generated member cannot be written. An excluded FK column yielded its edge before FIX-358; an O2M one did not compile (FIX-358; O2O and M2M built, and the FIX-358 ruling drops them too). |
| R2 | MATCH semantics | **Record `Constraint.Match` on PostgreSQL only; MySQL and SQLite are always SIMPLE. Under FULL, disconnect only when every member is free and nullable** (§4.5). | Only PG enforces FULL. MySQL parses it, reports it in `MATCH_OPTION` and ignores it; SQLite ignores it and reports `NONE`. PG accepts an all-NULL FULL key, refuses a mixed one. |
| R3 | Composite-PK parents and targets (E1, E3) | **Next phase.** This phase gives them every read surface (§4.5). | GraphQL has no composite-key input type; `<T>PK` with opaque fields is not a map key; E1 already refuses the writes, so deferring breaks nothing; no consumer needs it. |
| R4 | Chunk loader IN lists? | **Yes, at `floor(maxParams / 2 / N)`, every loader step; PRD §25.1 "one query per edge per chunk". N = 1 first as FIX-359** (§4.4). | Today's loaders already fail: SQLite > 32766 parents, MySQL > 65535, every M2M junction on pgx. A hand-chunked loader returned 40000 parents correctly; a chunk of 32766 failed with a one-parameter consumer filter, so the headroom is needed. |
| R5 | Naming rule | **The four-step rule in §4.2 rule 5, and a residual collision is a hard error.** | All 15 examples keep today's names. The doc's original rule collided on `cycle_notes`; the added step fixed it; no collision remains in any measured output. |
| R6 | M2M rule | **Redundant FKs first (§4.2 rule 2), then the set-aside rule with containment, as PRD §5.3 has it today. State that a set-aside FK gives no edge on a junction and can move a junction's ends** (§4.2 rule 4). | All 15 examples unchanged; tenant junctions detected; no entity table became M2M. A narrower variant (set aside only columns both ends share) made `cycle_settings` a false M2M and was rejected. |
| R7 | Manual relationship config | **`fk`, `junction_local_fk`, `junction_reference_fk` take a string or a list mapped by position onto the resolved PK. No `references` key** (§4.7). | `yaml.v3` dual-form decode works and rejects a map; the config package already decodes two forms. A non-PK manual target would be a new capability no one has asked for. |

The old open questions are settled by the design: Q1 by §4.2 rule 1, Q2 by §4.2 rule 1 and
§4.9, Q3 by §4.3 (a tenant member in the key marks the edge as tenant-guarded for
`TENANT_SCOPED_REFERENCES.md`'s hook), Q4 by §4.7, Q5 by §4.6.

### 7.1 Rulings at 32.0 (2026-10-22)

Nine points §4 left open, settled while writing the PRD and in its review, each taking the
recommended option:

| Question | Ruling |
|---|---|
| Does an edge with no free member lose `create`? | **No.** It loses the rewrite verbs only and stays `create`-only, like a NOT NULL edge; they drop silently, as a NOT NULL edge's do. M2M is exempt. The literal "readable but not writable" would have taken `create` from a single-column FK in the child's PK (`order_lines(order_id, line_no)`), an unlisted N = 1 change. |
| M2M unlink: junction PK, or PK or UNIQUE, equal to the union? | **Primary key only.** `HardDeleteMany` keeps keying on the junction PK, and no surrogate-keyed junction gains unlink verbs. |
| Which error is a pinned-member mismatch? | **`ErrAlreadyRelated`** (`CONFLICT`), naming the target: the row is bound to another key through a member the edge cannot write. A nested `create` whose tenancy-set tenant column differs from the parent's gets it too; under `SkipTenancy` only the parent sets it, so no mismatch exists. |
| Manifest `junction` shape | **Nested, like `fk`:** `{table, schema, local_fk: {columns, references, constraint}, target_fk: {…}}` (PRD §30.4.2). |
| Nested child input and the tenant column | **Every key member is elided, tenant column included**, and set from the parent; a tenancy-set value that disagrees is `ErrAlreadyRelated`. E1 therefore covers every member, the tenant column included, which supersedes R1's "every non-tenant member". Keeping the tenant column in the input would have added a field to a single-column tenant FK edge, an N = 1 change. |
| PK-override target | **Rule 1 also accepts the declared PRIMARY KEY constraint.** With `primary_key.columns: [code]` on `cycles(id PK, code UNIQUE)`, `tasks.cycle_id → cycles(id)` keeps its edge and loads through `id`, as §4.9 expects. |
| Residual name collision | **Checked after `exclude_relationships` and manual replacements** (resolution-level), so the remedy it names can clear it. Detection still renames; the error lives where exclusions are known. |
| `MATCH FULL` `connect` with a pinned member | **Generated only with `allow_reparent: true`.** Such a key never has an unparented row, so the default `connect` could only report a no-op or a conflict. Dropped silently, as a schema fact. |
| `Filter.PKs` and tuple IN | **PRD §9.8.13 states the code:** OR-of-ANDs on every dialect. Routing it through `BuildCompositePKBatchCondition` is a backlog line, since it moves every composite-PK example's Go goldens. |


## 8. Interaction with tenant-scoped reference verification

`TENANT_SCOPED_REFERENCES.md` proposes a runtime hook that probes tenanted→tenanted
references. An edge whose key contains the tenant column on both sides is enforced by the
database, so the hook can skip it. Under D the generator knows this from the key, so no new
flag is needed.

## 9. Probe record

### 9.1 What already exists (re-verified at `bd7783ea` and `244ce2ee`)

At `244ce2ee` (2026-10-22): the composite-FK parity tests pass on all three dialects under both
sources (`TestPostgresIntrospect_TableConstraints`, `TestMySQLIntrospect_TableConstraints`,
`TestSQLiteIntrospect_TableConstraints`, `TestParityFixtures`), and a SQLite probe of
`(workspace_id, cycle_id) → Cycles (WORKSPACE_ID, ID)` gave identical `Columns` and resolved
`ReferenceColumns` under both. The builders sit at `sql/builder.go:461-531` and the
`PKColumns[0]` relationship reads are 6 / 2 / 12, as below.


- **The parser keeps the data.** `parser.Constraint` stores a composite FK's `Columns` and
  `ReferenceColumns` on all three dialects and under every source. `ResolveReferenceNames`
  already rewrites `ReferenceTable` and `ReferenceColumns` to the target's declared spelling,
  so detection can compare names exactly.
- **Composite keys already have query machinery** (`sql/builder.go:461-531`):

  | Case | PostgreSQL | SQLite | MySQL |
  |---|---|---|---|
  | One key | `"workspace_id" = $1 AND "id" = $2` | same, `?` | same, `?` |
  | Many keys | `("workspace_id", "id") IN (($1, $2), ($3, $4))` | same, `?` | `((a = ? AND b = ?) OR (a = ? AND b = ?))` |

  `<T>Filter.PKs` compiles to OR-of-ANDs everywhere, and keyset cursors expand a composite key
  to `(a > $) OR (a = $ AND b > $)`. The `<T>PK` structs, the opaque `JoinClause.On` and the
  per-table `keyFor<T>` helpers are reusable as they stand.
- **M2M keys are typed** (FIX-357, `bd7783ea`). Each junction column scans into its own Go
  type and both maps key on `fkLoadKey`, which tuple keys build on (§4.4). M2M targets filter
  on their own key field (FIX-356, `c5c3862d`).

### 9.2 Option A, prototyped and measured (2026-10-20)

A 64-line `deriveKeyedReferences` pass at the start of `DetectRelationships` gave the two
`sqlgen-example` edges with no manual relationships. The example generated, built, vetted and
passed its full suite (`go test ./...` with testcontainers). Its output matched the §1.4
workaround's except for edge order and the manifest name (`tasks` detected, `Tasks`
declared). `go test -short ./parser/... ./cmd/sqlgen/...` passed. The patch is not kept; D
replaces it (§3).

### 9.3 Found along the way

| Finding | Status |
|---|---|
| An M2M whose target PK is not named `id` generated code that did not compile | Fixed, FIX-356 (`c5c3862d`) |
| A prefixed tuple-IN renders invalid SQL on PostgreSQL and SQLite | Measured (§4.8); unreachable from generated code today; fixed by N.1 |
| A single-column PK that is also a composite-FK member is classified by type in `autoDetectPKStrategy` | Reading only; fixed by N.4 (§4.6) |
| A single-column FK to a non-PK column (UNIQUE, or the PK-override form) generates a loader that does not compile | Measured by the FIX-358 vet (§4.9); folded into N.4 / N.5 with both schemas as regression tests |
| Under option A, a tenant junction with two composite FKs and no single-column FK on the tenant column becomes M2M, with a three-column junction key that M2M disconnect cannot handle | Reading only; one reason A is not recommended |
| MySQL is marked as lacking tuple IN (`sql/mysql.go:103`) | Measured: it works, and OR-of-ANDs is up to 1000× slower (§4.8); fixed in N.1 (tuple IN everywhere; flag removed) |
| An FK column removed by `exclude_columns` still yielded its edge; an O2M one did not compile | Fixed, FIX-358 (`c875fec9`) |
| Excluding a table's only PK column passes `validate` and crashes `generate` (`index .PKColumns 0`) | Fixed, FIX-360: now a schema-level validation error (PRD §4.13) |
| Relationship loaders fail past the dialect's bind limit (O2M on SQLite and MySQL, every M2M junction including pgx) | Fixed, FIX-359 (`2ae215f8`) |

### 9.4 Probes for the rulings (2026-10-20, at `bd7783ea`)

All throwaway: worktrees and example copies were removed. The detection prototype is kept for
N.2 to start from at `.claude/prototypes/composite-fk/detect-proto.patch` (gitignored, local
to this checkout). It applies at `bd7783ea` and adds `parser/proto_detect.go` and
`cmd/sqlgen/cli/proto_detect_test.go`.

- **R1, pinned members (PostgreSQL 17).** `tasks(workspace_id, project_id → projects(id),
  (workspace_id, cycle_id) → cycles(workspace_id, id))`: `UPDATE tasks SET workspace_id = 2,
  cycle_id = 20` succeeded and left the task's project in workspace 1. `docs` with two
  composite FKs sharing `workspace_id`: writing it through one edge raised
  `docs_workspace_id_team_id_fkey` when the other edge was linked and moved the row to
  workspace 2 when it was NULL. `cycle_members` keyed `(cycle_id, user_id)`: the update
  re-keyed the row. A generated member: `column "cycle_id" can only be updated to DEFAULT`.
  sqlgen today: a generated single-column FK listed for nested mutations fails with
  `"cycle_id" is absent from the target's create input`; an excluded one compiles to
  `unknown field CycleID in struct literal of type TaskFilter`.
- **R2, MATCH (PostgreSQL 17.11, MySQL 8.4.11 and 8.0.46, SQLite 3.51.0 CLI and modernc
  3.51.3).** PostgreSQL rejects a mixed-NULL FULL key on insert and on `SET pid = NULL` with
  `MATCH FULL does not allow mixing of null and nonnull key values`, accepts the all-NULL
  update, and accepts the single-column FULL disconnect; `confmatchtype` is `s` / `f`.
  MySQL accepts FULL and PARTIAL, enforces neither, reports them in
  `REFERENTIAL_CONSTRAINTS.MATCH_OPTION`, and drops the clause from `SHOW CREATE TABLE`.
  SQLite accepts any MATCH word, enforces SIMPLE, and `foreign_key_list.match` is always
  `NONE`. Our parsers accept FULL on all three and keep nothing; `pg_query`
  (`Constraint.FkMatchtype`) and vitess (`ReferenceDefinition.Match`) already carry it.
- **R4, bind limits.** PostgreSQL 65535 (pgx `extended protocol limited to 65535
  parameters`, lib/pq the same limit), MySQL 65535 (`Error 1390`; unbounded with
  `interpolateParams=true`), SQLite 32766 (`too many SQL variables`). Today's loaders on the
  `postgres`, `sqlite` and `mysql` example copies: see FIX-359. Hand-chunked SQLite O2M
  loader, 40000 parents, consumer filter `InStock: Eq(true)`: unchunked and 32766 fail;
  32765 and 16000 return 40000 parents with the 40 correct child sets in 2 and 3 queries.
- **R5 / R6, detection prototype.** `parser.DetectRelationshipsProto` over all 15 examples
  through the CLI's `parseSchema` path: identical edges, types and names to today in every
  example; `sqlgen-example` gains exactly `cycles → tasks` and `teams → projects`. Twenty
  crafted fixtures cover the tenant pattern, tenant junctions with and without the
  workspace FK, a three-single-FK junction, a composite-PK target, O2O by subset, two FKs to
  one target, a composite self-reference, an all-pinned key, non-key references on SQLite
  (composite and single-column), redundant and duplicate FKs, and adversarial junctions
  (`shipments`, `order_items`, an entity with a payload UNIQUE). The original naming rule
  collided on `cycle_notes` (both directions `cycle_notes`); the added step named them
  `cycle` / `prev_cycle`, and a duplicate-name scan of the final run found none. The
  narrower M2M variant turned `cycle_settings` into a false workspaces ↔ cycles M2M.
- **R7, config decode.** A `yaml.v3` type with `UnmarshalYAML` decodes `fk: cycle_id`,
  `fk: [workspace_id, cycle_id]` and the block-list form, and rejects a map with
  `cannot unmarshal !!map into []string`.

### 9.5 Contract retrofit (2026-11-05, at `716c15e1`)

Read-only checks behind §10, no probe run:

- **The registry already holds most of the contract.** 32.0 registered the PRD text it wrote,
  so the composite-FK requirements sit in `docs/conformance/` with empty Tests cells:
  `config.relationship-fk-length-mismatch*` (§4.13), `gen.nested.composite-key-free-members`,
  `gen.nested.match-full` (§9.9), `gen.filter.rel-composite-correlation`,
  `gen.filter.member-any-key` (§11.1), `gen.relationships.joins-referenced-columns`,
  `gen.relationships.composite-key-edge` (§13.1), `gen.fk-convert.composite-member`,
  `gen.fk-convert.null-unique-referenced-skipped` (§13.3), `gen.manual.fk-list-by-position`
  (§13.4), nine `manifest.json.rel.*` rows (§30.4.2), four `mcp.surface.*` rows (§31.3) and four
  `api.*` rows (§26.4, §26.5.3). §10.1 reuses those IDs, so `/implement` fills their Tests cells
  rather than adding rows.
- **Two registry rows claim more than their tests show.** `gen.increment.fk-excluded` (§8.2)
  and `gen.pk-strategy.fk-pk-caller` (§8.6) both state "a member of a composite FK included",
  but their tests (`TestIncrementColumns_ExcludeForeignKeys`, `TestPKStrategyDetection/… FK PK …`)
  build single-column FKs only, and the code still reads `col.FKReference`
  (`cmd/sqlgen/gen/context_table.go:1282`, `:2097`, `:2118`). §1.2's measured symptom
  (`TaskIncrementCycleID` generated) therefore still holds at HEAD. §10.1 lists the composite
  half as planned.
- **The PRD gives no rows for some behaviors it states.** §13.2 (composite O2O join, tuple
  O2M and M2M loads, the condition appended rather than replacing), §9.6 (every referenced and
  FK column unioned into a narrowed projection), §25.1 (`s = C` for a composite key on pgx),
  §9.9.5–§9.9.8 (pinned check, free-member writes, nested `create` over every member, the tenant
  mismatch) and §30.4 (`schema_version` `0.2.0`) have no registry row for the composite case.
  §10.1 adds them as new IDs.
- **Staging-guard tests are inverted later.** `TestGenerate_CompositeKeyEdgeIsOmitted` and
  `TestRelationshipFilters_CompositePKParentEmitsNone` pin today's output, and the registry marks
  both as staging guards. 32.5 replaces them (§10.1).
- **`tenancy_postgres` generates no GraphQL.** The only tenanted GraphQL example is `graphql`
  (`tenancy.column: workspace_id`). 32.7's "same rows as the Go client, under tenancy" needs a
  composite FK there, and 32.8 does not plan one (ruled: the `graphql` example gains one in 32.7, §12 Q4).

## 10. Behavior contract

Retrofitted 2026-11-05 at `716c15e1`, after 32.0–32.2 landed. §10.0 records what has landed;
§10.1–§10.4 hold what 32.3–32.8 must deliver, following `docs/design/templates/design.md` §6.
IDs are the registry's own where a row exists (§9.5), and new IDs in the registry's style
otherwise; the new ones go into the registry at 32.3's start (§10.5).

### 10.0 Landed (32.0–32.2)

| Area | Registry rows | Status |
|---|---|---|
| Detection, every §5.3 shape on three dialects plus source parity | `parser.rel.*` (`parser/schema.md` §5.3, the full `relationship-detection` matrix) | passing |
| Non-key FK warning | `config.fk-references-non-key`, `.message` (`cli/config.md` §4.13) | passing |
| `Exists` over N pairs, prefixed tuple IN | `cond.exists-pairs.*`, `builder.select-join.prefixed-tuple-in` (`runtime/sql.md`) | passing |
| Composite-PK batches use tuple IN on MySQL | `artifacts.mutations.composite-pk`, `hard-delete-many.composite-pk` | passing |
| Chunk size `floor(L / 2 / N)` at N = 1 | `load.chunk-size` (`client.md` §25.1) | passing at N = 1; N = 2 in §10.1 |
| Excluded link column drops the edge | `config.exclude-columns-removes-link-column*` | passing at N = 1; N = 2 in §10.1 |

### 10.1 New and changed behavior

Every row's PRD text landed at 32.0, so every anchor is existing.

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| gen.manual.fk-list-by-position | `fk`, `junction_local_fk` and `junction_reference_fk` take a string or a list; a list maps by position onto the referenced table's primary key; a string is a one-item list | §13.4 | all | `unit: cmd/sqlgen/config: TestLoadConfig_RelationshipKeyForms/string`, `/flow list`, `/block list`, `/map is rejected`; `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey` | — | passing |
| config.relationship-fk-length-mismatch | A key list whose length is not the referenced primary key's column count is an error, a string into a composite key and an empty list included | §4.13 | all | `unit: cmd/sqlgen/config: TestValidatePostParse_RelationshipKeyLength/fk list too short`, `/junction_local_fk list too long`, `/string into a composite key`, `/empty list` | config-validation: relationship-fk-length-mismatch × fires | passing |
| config.relationship-fk-length-mismatch.message | The error names the relationship, the key, the list and the primary key's columns | §4.13 | all | `unit: cmd/sqlgen/config: TestValidatePostParse_RelationshipKeyLength/fk list too short` | config-validation: relationship-fk-length-mismatch × message | passing |
| config.relationship-fk-length-mismatch.severity | Reported as an error | §4.13 | all | same | config-validation: relationship-fk-length-mismatch × severity | passing |
| config.relationship-fk-length-mismatch.together | Reported with the run's other validation errors | §4.13 | all | `unit: cmd/sqlgen/cli: TestValidate_ReportsEachGenerationRuleWithTheOthers/relationship key list of the wrong length` | config-validation: relationship-fk-length-mismatch × together | passing |
| config.schema.relationship-key-forms | `v1.json` accepts a string or a string list for the three key fields and rejects a map | §4.14 | all | `unit: cmd/sqlgen/config: TestSchemaV1_RelationshipKeyForms/string`, `/list`, `/map is rejected` | — | passing |
| manifest.json.rel.columns-references | `fk.columns` and `fk.references` list the pairs in the FK's declared order, `columns[i]` referencing `references[i]` | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/composite key in declared order` | — | passing |
| manifest.json.rel.single-column-lists | A single-column edge has one-item `columns` and `references` | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/single-column key`; `golden: postgres` | — | passing |
| manifest.json.rel.references-target-key | `references` names the target's PK or the UNIQUE columns the FK names | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/key into a unique constraint` | — | passing |
| manifest.json.rel.references-manual | A manual relationship's `references` is the referenced primary key | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship` | — | passing |
| manifest.json.rel.junction-local-fk | `junction.local_fk` is `{columns, references, constraint}` into this entity | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_M2MJunctionSchema` (extended) | — | passing |
| manifest.json.rel.junction-target-fk | `junction.target_fk` is `{columns, references, constraint}` into the target | §30.4.2 | all | same | — | passing |
| manifest.json.rel.constraint | `constraint` names the FOREIGN KEY the edge came from | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/detected edge names its constraint` | — | passing |
| manifest.json.rel.constraint-sqlite-unnamed | An unnamed SQLite FK is named `table(columns) → target(columns)` | §30.4.2 | sqlite | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/unnamed sqlite key`; `golden: sqlite` | — | passing |
| manifest.json.rel.constraint-manual-omitted | A manual relationship omits `constraint` | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestBuild_RelationshipKeyShape/manual relationship` | — | passing |
| manifest.json.top.schema-version | `schema_version` is `0.2.0` | §30.4, §30.8 | all | `unit: cmd/sqlgen/manifest: TestEmitJSON_SchemaVersion`; `golden: postgres` | — | passing |
| manifest.load.key-lists | The runtime `manifest` package loads a `0.2.0` document's key lists and junction keys | §30.6 | all | `unit: manifest: TestLoad_RelationshipKeyLists` | — | passing |
| mcp.surface.find-referencing.output | `sqlgen_find_referencing` returns `[{entity, relationship_name, kind, fk_columns}]` | §31.3 | all | `unit: cmd/sqlgen/mcp: TestFindReferencing` (extended) | mcp-surface: find-referencing × output | passing |
| mcp.surface.find-referencing.fk-columns-order | `fk_columns` is in the FK's declared order, one item for a single-column key | §31.3 | all | `unit: cmd/sqlgen/mcp: TestFindReferencing/composite key in declared order` | — | passing |
| mcp.surface.find-referencing.fk-columns-whole | Dedup and sort compare `fk_columns` whole: a two-column and a one-column key into one table are two results | §31.3 | all | `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple/two-column and one-column keys into one table` | — | passing |
| mcp.surface.describe-relationship.references | An FK edge's join shape gives the columns its FK columns reference | §31.3 | all | `unit: cmd/sqlgen/mcp: TestDescribeRelationship_JoinShapeNamesReferencedColumns` | — | passing |
| config.relationship-key-into-view-without-pk | A key into a view maps onto its `@pk`; a belongs-to `fk` or an m2m `junction_reference_fk` into a view without `@pk` is an error, written or inferred | §4.13 | all | `unit: cmd/sqlgen/config: TestValidatePostParse_RelationshipKeyIntoView`; `unit: cmd/sqlgen/cli: TestValidate_ReportsEachGenerationRuleWithTheOthers/relationship key into a view` | config-validation: relationship-key-into-view-without-pk × fires, message, severity, together | passing |
| gen.manual.fk-list-into-view | A declared key into a view maps by position onto the view's `@pk` columns | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredKeyListMapsOntoPrimaryKey/a view target's key is its @pk` | — | passing |
| manifest.json.rel.columns-references-required | `schema/v1.json` requires `columns` and `references` on every `fk` and junction key | §30.4.2 | all | `unit: cmd/sqlgen/manifest: TestSchemaV1_RelationshipKeyRequiresColumnsAndReferences` | — | passing |
| gen.relationships.joins-referenced-columns | An edge joins the columns its FK references, not the target's PK: a single-column FK to a non-PK UNIQUE column and the PK-override form generate code that compiles, nested writers included (the test builds it outside `-short`) | §13.1 | all | `unit: cmd/sqlgen/cli: TestGenerate_EdgeJoinsReferencedColumns/fk to a non-PK unique column`, `/m2m junction into a non-PK unique column`, `/fk into the declared key under a primary-key override` | — | passing (32.4) |
| gen.increment.fk-excluded (composite half) | A member of a composite FK gets no increment operator | §8.2 | all | `unit: cmd/sqlgen/gen: TestIncrementColumns_ExcludeForeignKeys/composite FK member` | — | passing (32.4) |
| gen.pk-strategy.fk-pk-caller (composite half) | A single-column PK that is a member of a composite FK is `caller` | §8.6 | all | `unit: cmd/sqlgen/gen: TestPKStrategyDetection/postgres UUID PK in a composite FK → caller`, `/mysql auto-increment bigint PK in a composite FK → caller`, `/sqlite autoincrement integer PK in a composite FK → caller` | — | passing (32.4) |
| gen.nested.composite-key-free-members | A member is pinned when it is the tenant column, a child-PK member or a member of another FK; the rewrite verbs write free members only; an edge with no free member is `create`-only, silently | §9.9.2 | all | `unit: cmd/sqlgen/gen: TestNestedEligibility_PinnedAndFreeMembers/tenant column`, `/child primary-key member`, `/member shared with another key`, `/every member pinned keeps create only`, `/free member` | relationships: composite-o2m × nested-mutation | passing (32.6) |
| gen.nested.match-full | Under PostgreSQL `MATCH FULL`, `disconnect` / `clear` need every member free and nullable, and `connect` with a pinned member needs `allow_reparent`; MySQL and SQLite read every key as SIMPLE | §9.9.4 (E8) | postgres (MySQL, SQLite: SIMPLE) | `unit: cmd/sqlgen/gen: TestNestedEligibility_MatchFull/simple with nullable free members`, `/full with a pinned member has no disconnect`, `/full with every member free keeps disconnect`, `/full with a pinned member connects only under allow_reparent`, `/mysql and sqlite read full as simple` | — | passing (32.4) |
| config.exclude-columns-removes-link-column (composite) | An excluded member of a composite key, on the child or the parent, drops the detected edge with a warning | §4.13 | all | `unit: cmd/sqlgen/gen: TestRelationshipTargets_ExcludedCompositeKeyMemberDropsInferredEdge/composite key member on the child`, `/composite key member on the parent` | config-validation: exclude-columns-removes-link-column × fires | passing (32.5) |
| gen.relationships.composite-key-edge | A composite-FK edge is generated: O2O by a unique subset, O2M otherwise, M2M on a tenant junction | §13.1 | all | `unit: cmd/sqlgen/cli: TestGenerate_CompositeKeyEdgeIsGenerated/composite o2m`, `/composite o2o`, `/chained o2o`, `/tenant junction m2m` (replaces `TestGenerate_CompositeKeyEdgeIsOmitted`) | relationships: composite-o2m × struct-field (and the other composite rows, §10.3) | passing (32.5) |
| gen.manual.replaces-inferred (composite) | A one-pair manual relationship replaces the composite edge it duplicates, with the warning, so the §1.4 workaround keeps generating | §13.4 | all | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredOverCompositeFK` | — | passing (32.5) |
| load.o2o-composite-key | An O2O join ANDs one equality per key pair | §13.2 | all | `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair`; `conformance: Get/an O2O through a composite key joins on every pair` | relationships: composite-belongs-to × loader | passing (32.5) |
| load.o2m-composite-key | An O2M load filters the child with tuple IN over the parent's referenced columns and gives each parent exactly its children | §13.2 | all | `unit: cmd/sqlgen/gen: TestGetTemplate_o2mCompositeKeyLoading/pgx`, `/stdlib`; `conformance: GetMany/a composite foreign key loads each parent's children on every pair` | relationships: composite-o2m × loader | passing (32.5) |
| load.m2m-composite-key | An M2M load reads the junction by tuple and maps each target to its parents on every key column, in the target query's order | §13.2 | all | `unit: cmd/sqlgen/gen: TestGetTemplate_m2mCompositeKeyLoading`; `conformance: GetMany/an M2M through composite junction keys maps each target to its parents` | relationships: composite-m2m × loader | passing (32.5) |
| load.key-condition-appended | The key condition is appended to the child query's conditions, so a consumer filter still applies | §13.2 | all | `conformance: GetMany/a consumer filter on a composite edge applies with the key condition` | — | passing (32.5) |
| gen.fk-convert.composite-member | Every member read off a child is guarded and unwrapped; a child with a NULL member loads under no parent and raises no error | §13.3 | all | `unit: cmd/sqlgen/gen: TestGetTemplate_o2mCompositeKeyLoading/nullable member is guarded`; `conformance: GetMany/a child with a NULL key member loads under no parent` | — | passing (32.5) |
| gen.fk-convert.null-unique-referenced-skipped | A parent whose referenced UNIQUE column holds NULL is skipped before the query | §13.3 | all | `conformance: GetMany/a parent whose referenced unique key holds NULL loads no children` | — | passing (32.5) |
| get-many.required-columns-composite-key | A narrowed projection still unions every referenced parent column and every child FK member, so the edge loads | §9.6 | all | `unit: cmd/sqlgen/gen: TestRequiredColumns_compositeKeyMembers`; `conformance: GetMany/a projection without key columns still loads a composite edge` | — | passing (32.5) |
| load.chunk-size (N = 2) | A two-column key binds at most `floor(L / 2 / 2)` parent keys per statement, 4000 on PostgreSQL (§12 Q6), and loads every child set past one chunk | §25.1 | all | `conformance: GetMany/a composite key load past one chunk binds at most its chunk per statement` | — | passing (32.5) |
| query-count.composite-o2m-pgx | A composite-key O2M on pgx takes one statement per chunk (`s = C`), not one `= ANY` statement | §25.1 | postgres (pgx) | `unit: cmd/sqlgen/gen: TestGetTemplate_o2mCompositeKeyLoading/pgx`; `conformance: GetMany/a composite key load past one chunk binds at most its chunk per statement` | — | passing (32.5) |
| gen.filter.rel-composite-correlation | A relationship filter's `EXISTS` correlates on every key pair | §11.1 | all | `unit: cmd/sqlgen/gen: TestFilterTemplate_CompositeKeyCorrelatesEveryPair`; `conformance: GetMany/a relationship filter through a composite key matches on every pair` | relationships: composite-o2m × filter | passing (32.5) |
| gen.filter.member-any-key | Every list relationship gets a filter member: a composite-PK parent, an edge into a UNIQUE key, an M2M into a composite-PK target | §11.1 | all | `unit: cmd/sqlgen/gen: TestRelationshipFilters_MemberForAnyKey/composite-PK parent`, `/edge into a unique key`, `/m2m into a composite-PK target` (replaces `TestRelationshipFilters_CompositePKParentEmitsNone`) | relationships: composite-m2m × filter | passing (32.5) |
| gen.nested.e3-keeps-relationship-filters | A composite-PK parent with no nested surface keeps its relationship filters | §9.9.4 | all | `unit: cmd/sqlgen/gen: TestRelationshipFilters_MemberForAnyKey/composite-PK parent` | — | passing (32.5) |
| tenant-rel.composite-load-in-tenant | A composite tenant edge's load runs the child's tenant-scoped `GetMany` | §29.10 | all | `example: tenancy_postgres: TestCompositeTenantFK_LoadStaysInTenant` | — | planned (32.8) |
| gen.nested.child-input-drops-key-members | `<Parent><Edge>CreateInput` drops every member of the traversed key, the tenant column included | §9.9.5 | all | `unit: cmd/sqlgen/gen: TestNestedTemplate_ChildCreateInputDropsEveryKeyMember` | — | passing (32.6) |
| gen.nested.e1-target-shape (composite) | An edge whose key has a generated member, or references a nullable column in any position, is not write-eligible | §9.9.4 (E1) | all | `unit: cmd/sqlgen/gen: TestNestedEligibility_TargetShapeFailuresAreNotWriteEligible/generated member of a composite key`, `TestNestedEligibility_ReferencedNullableMemberIsNotWriteEligible` | — | passing (32.6) |
| nested.composite-create-sets-every-member | A nested `create` sets every key member from the parent's referenced columns | §9.9.6 | all | `unit: cmd/sqlgen/gen: TestNestedTemplate_CompositeKeyExecutor/create sets every member from the parent`; `example: tenancy_postgres: TestCreateWithRelated_CreateSetsEveryKeyMemberFromTheParent` | — | passing (32.6) |
| nested.composite-create-tenant-mismatch | A nested `create` whose tenancy-set tenant column differs from the parent's value, or an M2M link row whose key-set tenant column differs from the resolved tenant, is `ErrAlreadyRelated`, on a key of any length | §9.9.8 | all | `example: tenancy_postgres: TestUpdateWithRelated_TenancySetTenantMismatchIsAlreadyRelated`, `TestUpdateWithRelated_SingleColumnTenantKeyMismatchIsAlreadyRelated`, `TestUpdateWithRelated_LinkTenantMismatchIsAlreadyRelated` | — | passing (32.6) |
| nested.unlink-tenant-mismatch | A `disconnect` or `clear` whose key takes the tenant column from a parent column that differs from the resolved tenant is `ErrAlreadyRelated`, attributed to the verb, with no row unlinked, whether or not it names a row on the edge | §9.9.6, §9.9.8 | all | `example: tenancy_postgres: TestUpdateWithRelated_UnlinkTenantMismatchIsAlreadyRelated`; `unit: cmd/sqlgen/gen: TestNestedTemplate_UnlinkComparesAParentHeldTenant` | — | passing (32.6) |
| nested.m2m-create-shared-column-mismatch | A nested M2M `create` whose created target differs from the parent's value on a column both junction keys share is `ErrAlreadyRelated`, attributed to `create`, no id, before the link step, and the created row rolls back | §9.9.6, §9.9.8 | all | `example: tenancy_postgres: TestUpdateWithRelated_CreateSharedJunctionColumnMismatchIsAlreadyRelated`; `unit: cmd/sqlgen/gen: TestNestedTemplate_CompositeKeyExecutor/m2m create compares the column both keys share` | — | passing (32.6) |
| nested.composite-connect-pinned-mismatch | A `connect` target whose pinned member, or on an M2M edge a column both junction keys share, differs from the parent's value is `ErrAlreadyRelated` naming it, before the adoption write or the link step, `allow_reparent` included | §9.9.6, §9.9.8 | all | `example: tenancy_postgres: TestUpdateWithRelated_PinnedMemberMismatchIsAlreadyRelated`, `TestUpdateWithRelated_NullablePinnedMember`, `TestUpdateWithRelated_ConnectSharedJunctionColumnMismatchIsAlreadyRelated`, `TestUpdateWithRelated_ConnectUnderSharedParentMismatchIsAlreadyRelated` | — | passing (32.6) |
| nested.composite-connect-writes-free-members | `connect` and `allow_reparent` write the free members only | §9.9.2 | all | `example: tenancy_postgres: TestUpdateWithRelated_ConnectWritesOnlyFreeMembers` | — | passing (32.6) |
| nested.composite-connect-cross-tenant | Under tenancy, a `connect` naming another tenant's row is `ErrNotFound` before any write | §9.9.10 | all | `example: tenancy_postgres: TestUpdateWithRelated_CrossTenantConnectIsNotFoundBeforeAnyWrite` | — | passing (32.6) |
| nested.composite-connect-held-elsewhere | A target parented to another row through a composite key, or holding a partly NULL key, is `ErrAlreadyRelated` naming it; `allow_reparent` adopts a partly NULL key | §9.9.3, §9.9.6 | all | `example: tenancy_postgres: TestUpdateWithRelated_ConnectTargetHeldByAnotherParentIsAlreadyRelated`, `TestUpdateWithRelated_PartlyNullKeyIsAlreadyRelated`, `TestUpdateWithRelated_AllowReparentAdoptsAPartlyNullKey` | — | passing (32.6) |
| nested.composite-disconnect-nulls-free-members | `disconnect` and `clear` null the free members only | §9.9.2 | all | `example: tenancy_postgres: TestUpdateWithRelated_DisconnectNullsOnlyFreeMembers` | — | passing (32.6) |
| nested.composite-match-full-disconnect-nulls-every-member | Under PostgreSQL `MATCH FULL` with every member free, `disconnect` and `clear` null every member | §9.9.2 | postgres | `example: tenancy_postgres: TestUpdateWithRelated_MatchFullDisconnectNullsEveryMember` | — | passing (32.6) |
| nested.junction-key-union | M2M link and unlink write and delete the junction row by both keys' union; `disconnect` needs a junction PK equal to it | §9.9.4 (E5, E11) | all | `unit: cmd/sqlgen/gen: TestNestedEligibility_TenantJunctionUnlinkNeedsPKOverTheUnion/pk equals union`, `/surrogate pk has no disconnect`; `example: tenancy_postgres: TestUpdateWithRelated_TenantJunctionLinksAndUnlinksThroughEveryColumn` | relationships: composite-m2m × nested-mutation | passing (32.6) |
| api.relationship-field.composite-pk | A composite-PK table's edges, and composite-FK edges, are fields on its object type | §26.4 | all | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositeKeyRelationshipFields`; `example: graphql: TestCompositeTenantFK_GraphQLMatchesGoClient` | api-generation: relationship-field × composite-pk | planned (32.7) |
| api.relationship-filter-member.composite-pk | A composite-PK parent's `<T>Filter` input carries relationship filter members | §26.4 | all | `unit: cmd/sqlgen/gen: TestGraphQLSchema_CompositeKeyRelationshipFields/composite-PK parent filter` | api-generation: relationship-filter-member × composite-pk | planned (32.7) |
| api.filter.relationship-composite-pk-parent | A composite-PK parent's relationship filter member is translated | §26.5.3 | all | `unit: cmd/sqlgen/gen: TestGraphQLFilterTranslation_RelationshipMember/composite-PK parent` | api-generation: relationship-filter × composite-pk | planned (32.7) |
| api.filter.relationship-composite-fk-edge | A composite-FK edge's relationship filter member is translated | §26.5.3 | all | `unit: cmd/sqlgen/gen: TestGraphQLFilterTranslation_RelationshipMember/composite foreign key`; `example: graphql: TestCompositeTenantFK_GraphQLRelationshipFilter` | — | planned (32.7) |
| api.composite-edge.key-columns-forced | A GraphQL query selecting no key column still loads a composite edge | §26.5.2, §9.6 | all | `example: graphql: TestCompositeTenantFK_EdgeLoadsWithoutSelectedKeys` | — | planned (32.7) |
| e2e.composite-tenant-schema | `sqlgen-example` validates, generates, builds and passes its suite with no manual relationships, `Cycle.Tasks` and `Team.Projects` detected | §5.3, §29.10 | postgres | probe, recorded in 32.8's Completion Record (`reference_example_probe_harness`) | — | planned (32.8) |

### 10.2 Regression guards

Each row is an existing behavior on a path 32.3–32.8 rewrites.

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| Every existing example's Go output is byte-identical (design §4.9) | §5.3, §13 | `golden: <every example>` (`TestE2EGoldenFiles`) | guarded |
| Detection and output agree under `database` | §6 | `TestE2EDatabaseTwin` | guarded |
| A string `fk` / `junction_local_fk` / `junction_reference_fk` means what it does today | §13.4 | `unit: cmd/sqlgen/config: TestLoadConfig_FullConfig`; `unit: cmd/sqlgen/config: TestSchemaV1_acceptsUnexercisedSurface`; `unit: cmd/sqlgen/gen: TestGenerate_RelationshipSideCaseInsensitive` | guarded |
| A manual relationship replaces a detected one with a warning | §13.4 | `unit: cmd/sqlgen/gen: TestBuildRelationshipContexts_DeclaredReplacesInferred` | guarded |
| A one-column O2M load uses `IN`, or one `= ANY($1)` statement on pgx | §13.2, §25.1 | `unit: cmd/sqlgen/gen: TestGetTemplate_o2mRelationshipLoading/pgx`, `/stdlib` | guarded |
| A one-column M2M load scans typed keys and fetches targets by their own key | §13.2 | `unit: cmd/sqlgen/gen: TestGetTemplate_m2mRelationshipLoading`; `TestGetTemplate_m2mJunctionScansTypedKeys`; `TestGetTemplate_m2mTargetFilterUsesTargetPKField` | guarded |
| A one-pair O2O join, belongs-to and has-one | §13.2 | `unit: cmd/sqlgen/gen: TestBuildO2OJoinDetails_chainedFKOnTarget`; `TestBuildO2OJoinDetails_fkOnTargetParentPKName`; `TestRelationshipsTemplate_resolveO2OJoins` | guarded |
| An O2M into a composite-PK table filters on its one FK column | §13.2 | `unit: cmd/sqlgen/gen: TestGenerate_O2MIntoCompositePKTable_LoaderFiltersOnFK` | guarded |
| One-column loads chunk past the bind limit, and a consumer filter is ANDed in | §25.1 | `example: postgres|postgres_stdlib|mysql|sqlite: TestRelationshipLoad_PastBindLimit`, `TestRelationshipLoad_M2MTargetsPastBindLimit` | guarded |
| One-pair relationship filters, polymorphic filters included | §11.1 | `unit: cmd/sqlgen/gen: TestFilterTemplate_O2MEmitsCorrelatedExists`; `TestFilterTemplate_M2MEmitsJunctionJoin`; `TestDiscriminator_RelationshipFilterBindsTheValue` | guarded |
| Composite-PK methods and `Filter.PKs` (OR of ANDs) | §8.7, §9.8.13 | `conformance: GetMany/composite keys return only the listed tuples` and the other `*.composite-pk` rows; `unit: cmd/sqlgen/gen: TestFilterTemplate_compositePK_PKsField` | guarded |
| Loader child reads inherit `SkipCache` / `SkipEvents` / `SkipHooks` and `SkipTenancy` | §13.2, §29.10 | `unit: cmd/sqlgen/gen: TestRelationshipCallOptions`; `TestTenancyTemplate_loadRelationships_propagatesSkipTenancy` | guarded |
| Fanned-out loads inside a transaction | §18.5 | `conformance: Get/selecting two fanned-out relationships inside a transaction loads both` | guarded |
| No read mutates the caller's `FieldOptions` | §9.6 | `conformance: GetMany/a read leaves the caller's field options unchanged`; `unit: cmd/sqlgen/gen: TestRequiredColumns_noFieldOptionsMutation` | guarded |
| A relationship-loaded read bypasses the cache | §27.6 | `example: cache: TestSkip_RelationshipBypass` | guarded |
| One-column increments and PK strategy for FK columns | §8.2, §8.6 | `unit: cmd/sqlgen/gen: TestIncrementColumns_ExcludeForeignKeys`; `TestPKStrategyDetection` | guarded |
| One-column nested verbs follow FK nullability; connect splits three ways | §9.9.2, §9.9.6 | `unit: cmd/sqlgen/gen: TestNestedEligibility_VerbSetPerShape`; `conformance: CreateWithRelated/connect naming a post that does not exist returns ErrNotFound …` (`nested.o2m-connect-three-way`) | guarded |
| A single-column FK whose member is a PK member or the tenant column is `create`-only (`gen.nested.single-column-pinned-create-only`) | §9.9.2 | `unit: cmd/sqlgen/gen: TestNestedEligibility_SingleColumnPinnedFKIsCreateOnly/child primary-key member`, `/tenant column` | guarded |
| The nested child input drops a single-column tenant FK's column (`context_nested.go`) | §9.9.5 | `unit: cmd/sqlgen/gen: TestNestedEligibility_ChildInputDropsTenantFKColumn`; `TestNestedTemplate_ChildCreateInputDropsEveryKeyMember/single-column tenant key` | guarded |
| E1 / E3 keep composite-PK targets and parents out of nested writes, with loaders and manifest entries kept | §9.9.4 | `unit: cmd/sqlgen/gen: TestNestedEligibility_TargetShapeFailuresAreNotWriteEligible/composite-PK target`; `TestNestedEligibility_ListedShapeFailureIsReported/composite-PK parent`; `TestNestedEligibility_CompositePKParentKeepsItsLoaders`; `unit: cmd/sqlgen/manifest: TestBuild_E3CompositePKParentKeepsManifestEntries` | guarded |
| `find_referencing` dedups by the full tuple | §31.3 | `unit: cmd/sqlgen/mcp: TestDispatchFindReferencingDedupsByFullTuple` | guarded |
| One-column GraphQL relationship fields, filters and nested inputs | §26.4, §26.5 | `golden: graphql`; `example: graphql: TestSurfacePromises_ChunkedParentSetAddsOneQueryPerChunk` | guarded |

### 10.3 Module matrix cells

**`relationship-detection` (parser), `condition-composition` (runtime).** Complete, filled by
32.1 and 32.2 (§10.0). No row changes.

**`config-validation` (CLI)**, new row:

| Rule | fires | message | severity | together | schema |
|---|---|---|---|---|---|
| `relationship-fk-length-mismatch` | config.relationship-fk-length-mismatch | config.relationship-fk-length-mismatch.message | config.relationship-fk-length-mismatch.severity | config.relationship-fk-length-mismatch.together | n/a: the length is checked against the parsed schema's primary key, which the JSON Schema cannot see (§4.14) |

**`relationships` (CLI)**, four new rows (ruled, §12 Q2):

| Type | struct-field | loader | filter | nested-mutation |
|---|---|---|---|---|
| `composite-o2m` | gen.relationships.composite-key-edge | load.o2m-composite-key | gen.filter.rel-composite-correlation | gen.nested.composite-key-free-members |
| `composite-belongs-to` | rel.composite-belongs-to.struct-field | load.o2o-composite-key | n/a: filter members are generated for list relationships only (§11.1) | n/a: shape 1 is never nestable (§9.9.1) |
| `composite-has-one` | rel.composite-has-one.struct-field | rel.composite-has-one.loader | n/a: filter members are generated for list relationships only (§11.1) | n/a: a has-one side is declared and a declared key list maps onto the parent's primary key, so only a composite-PK parent reaches this shape, and E3 excludes it |
| `composite-m2m` | rel.composite-m2m.struct-field | load.m2m-composite-key | gen.filter.member-any-key | nested.junction-key-union |

A registry row claims one matrix cell, so the struct-field cells other than `composite-o2m` take
`rel.composite-*.struct-field` rows and the has-one loader cell `rel.composite-has-one.loader`
(registered at 32.3). Their tests are the matching subtests of
`TestGenerate_CompositeKeyEdgeIsGenerated` and `TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair`
(32.5). `e2e.composite-tenant-schema` is not registered: a probe has no registry test home, so 32.8
records it in its Completion Record only.

**`api-generation` (CLI)**, the `composite-pk` column of existing rows:

| Element | composite-pk |
|---|---|
| `relationship-field` | api.relationship-field.composite-pk |
| `relationship-filter-member` | api.relationship-filter-member.composite-pk |
| `relationship-filter` | api.filter.relationship-composite-pk-parent |
| `create-with-related`, `update-with-related`, `upsert-with-related`, `nested-inputs` | n/a: E3 keeps composite-PK parents out of nested writes in this phase (§9.9.4, ruling R3) |

**`mcp-surface` (CLI)**: `find-referencing × output` is mcp.surface.find-referencing.output;
`listed`, `deterministic` and `error` are unchanged and stay on their current rows.

**`manifest-artifacts`, `generated-artifacts`, `client-methods`.** No new artifact or method, so
no cell changes. The client-methods matrix has no relationship column, so the `load.*` rows carry
no cell.

### 10.4 Dialect differences

32.3 adds the first two to PRD Appendix B as capabilities (§12, Q5).

| Difference | PostgreSQL | MySQL | SQLite | PRD |
|---|---|---|---|---|
| `MATCH FULL` enforced | yes: FULL gates `disconnect` / `clear` / `connect` (gen.nested.match-full) | parsed, ignored: read as SIMPLE | ignored: read as SIMPLE | §9.9.4 (E8) |
| Bind-parameter limit `L` | 65535 | 65535 | 32766 | §25.1 |
| Chunk at N = 2 | 4000 (tuple `IN` cap, §12) | 16383 | 8191 | §25.1 |
| One-statement O2M | pgx at N = 1 only (`= ANY($1)`); every composite key, and a one-column key into a nullable column, chunks | never | never | §25.1 |
| FK to a non-key column | refused at DDL time | warned (8.0; 8.4 with `restrict_fk_on_non_standard_key` off) | warned | §5.3 (landed) |

### 10.5 Contract rows per sub-item

| Sub-item | Turns these §10.1 rows from planned to passing | Also |
|---|---|---|
| 32.3 | gen.manual.fk-list-by-position, config.relationship-fk-length-mismatch (×4), config.schema.relationship-key-forms, manifest.json.rel.* (×9), manifest.json.top.schema-version, manifest.load.key-lists, mcp.surface.* (×4) | First task: register the new §10.1 IDs and the four `relationships` matrix rows, and write the two unguarded §10.2 tests against today's code (§12, Q3). PRD Appendix B gains the `MATCH FULL` and bind-limit rows (§12, Q5) |
| 32.4 | gen.relationships.joins-referenced-columns, gen.increment.fk-excluded (composite), gen.pk-strategy.fk-pk-caller (composite), gen.nested.composite-key-free-members (context), gen.nested.match-full | |
| 32.5 | config.exclude-columns-removes-link-column (composite), gen.relationships.composite-key-edge, gen.manual.replaces-inferred (composite), load.*, gen.fk-convert.composite-member, gen.fk-convert.null-unique-referenced-skipped, get-many.required-columns-composite-key, load.chunk-size (N = 2), query-count.composite-o2m-pgx, gen.filter.rel-composite-correlation, gen.filter.member-any-key, gen.nested.e3-keeps-relationship-filters | Replaces both staging-guard tests. The conformance schema gains its composite FKs here, on every dialect, so the `conformance:` rows fail first (§12, Q1) |
| 32.6 | gen.nested.composite-key-free-members (template), gen.nested.child-input-drops-key-members, gen.nested.e1-target-shape (composite), nested.* (×9) | `tenancy_postgres` gains its composite tenant FK, tenant junction and a `MATCH FULL` table here, so the nested `example:` rows fail first |
| 32.7 | api.* (×5) | The `graphql` example gains a composite tenant FK (§12, Q4) |
| 32.8 | tenant-rel.composite-load-in-tenant, e2e.composite-tenant-schema | The `tenancy_postgres` composite FK backs the 32.6 `example:` rows, so its schema lands in 32.6, not here; 32.8 adds the `mysql` / `sqlite` composite FKs and the `sqlgen-example` probe |

## 11. Definition of done

Phase 32 closes only when every item below holds:

1. Every §10.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §10.2 row is guarded, and every guard still passes.
3. Every §10.3 cell is an ID or a stated `n/a`.
4. Every PRD edit has landed (32.0, plus 32.3's Appendix B rows), and every §12 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too, since the
   change reaches runtime and driver packages.
6. Both staging-guard tests are gone, and no `compositeKeyEdge` guard remains in `gen`.
7. The registry rows in §10.1 carry their tests, and `gen.increment.fk-excluded` and
   `gen.pk-strategy.fk-pk-caller` list a composite-member test.

## 12. Rulings

### Rulings (settled 2026-11-05)

Each took the recommended option.

| # | Question | Ruling | Evidence |
|---|---|---|---|
| Q1 | Where do the cross-dialect read rows (composite O2O / O2M / M2M loads, NULL members, relationship filters) get tested? | **The conformance suite.** Its schema gains a composite FK into `order_items(order_id, product_id)` (a composite-PK target) and one into a UNIQUE pair, on every dialect, in 32.5, so the `conformance:` rows can fail first there. Examples keep the tenancy, pinned-member and bind-limit cases. | CONFORMANCE.md §2 puts generated-client behavior on every dialect in the suite; §4.1's schema has `order_items` already. |
| Q2 | How do composite edges enter the CLI `relationships` matrix? | **Four new rows**, `composite-o2m`, `composite-belongs-to`, `composite-has-one`, `composite-m2m`, with the existing columns (§10.3). | The parser matrix already names `composite-o2m`; a new column would reshape every row. |
| Q3 | Where are the two unguarded §10.2 tests written? | **32.3's first task**, with the registration of the new §10.1 IDs. Both must pass on today's code before any change. | Phase 32 is already broken down; a separate sub-item adds bookkeeping for two tests. |
| Q4 | Where does 32.7's GraphQL-versus-Go comparison run? | **The `graphql` example gains a composite tenant FK** in 32.7. | `graphql` is the only tenanted GraphQL example (§9.5). |
| Q5 | Do the dialect differences in §10.4 become PRD Appendix B capabilities? | **Yes: two rows**, "FOREIGN KEY `MATCH FULL` enforced" (PostgreSQL yes; MySQL parsed, ignored; SQLite ignored) and "Bind-parameter limit" (65535 / 65535 / 32766), added in 32.3. The pgx one-statement O2M is a driver fact and stays in §25.1. | CONFORMANCE.md §4.4: a dialect difference is a named capability from Appendix B. |
| Q6 | Does `floor(L / 2 / N)` hold for tuple `IN` on PostgreSQL? (found at 32.5) | **No: cap a key of several columns at 4000 keys per statement on PostgreSQL.** This refines R4, which was measured at N = 1 only. `VALUES` with typed casts was rejected for now: it needs generated SQL casts and a new runtime condition. | At the default 2MB `max_stack_depth`, `(a, b) IN (…)` passed 7500 rows and failed at 8000 on PostgreSQL 16, and passed 7000 and failed at 7500 on 17, with `stack depth limit exceeded`; two and three columns behaved alike. MySQL and SQLite pass at their full chunk. |
| Q7 | Where do the N = 2 bind-limit rows run? (found at 32.5) | **The conformance suite**, not the `examples/*` bind-limit tests: those examples have no composite FK until 32.6 and 32.8, and adding one here would break 32.5's no-golden-change gate. | The suite covers all four drivers and already records statements. Applied at 32.5 and confirmed by the user 2026-11-06. |

No question is open.
