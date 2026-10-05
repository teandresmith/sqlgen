# SQLGen Tenancy — Improvements and a Multi-Level Model

> **Status: DRAFT (2026-09-11).** Design exploration; no code, no decisions blessed. Sourced from
> reading the reference consumer, `../sqlgen-example` — a multi-tenant task tracker built as the
> adopter-facing example and live integration bed. Every finding below carries a `file:line` from
> either that consumer or this repo; nothing here is inferred from how tenancy *ought* to behave.
>
> **Two halves, and they are separable.** §2–§3 are the flat model's gaps and what would close
> them. §4 is the multi-level question. They are ordered that way because one of §3's proposals
> turns out to be the substrate §4 needs anyway (§5).
>
> **One finding is a live read leak on the generated GraphQL surface** (**F2**), not a hypothetical.
> It is the reason this document exists rather than a `/fix` entry.
>
> **The live leak has a one-line fix.** **T7** — a detected junction defaults to `api.enabled:
> false` — closes **F2** by not generating a surface a junction was never meant to have, using a
> tri-state config flag that already exists. It also shrinks **T2** from a correlated `EXISTS` on
> the M2M loader to guarded writes and a cold-path filter, because the expensive part of derived
> tenancy existed to protect exactly the surface **T7** deletes.
>
> **One proposal is a net deletion.** **T6** removes the tenant from the cache key: it partitions
> nothing (a row has one tenant, so the entry count is identical either way), it is re-encoded
> twice on a tenanted composite-PK table, and it is the single piece of the flat model that cannot
> be carried into a multi-scope one — so §4 is gated on it.
>
> **Pattern:** sibling design doc in the shape of `docs/design/archive/GRAPHQL_READ_SURFACE.md` and
> `docs/design/archive/NESTED_MUTATIONS.md` — findings, proposals, design space, sequencing, open questions.

## Contents

1. [Why this document](#1-why-this-document)
2. [Findings — where the flat model strains](#2-findings--where-the-flat-model-strains)
3. [Proposals for the flat model](#3-proposals-for-the-flat-model)
4. [Multi-level tenancy — the design space](#4-multi-level-tenancy--the-design-space)
5. [Recommended sequencing](#5-recommended-sequencing)
6. [Open questions](#6-open-questions)

---

## 1. Why this document

PRD §29 models a tenant as **a property of a row**: a table carrying the configured column is
tenanted, reads get `WHERE <col> = $tenant`, writes auto-set it, and a `TenantResolver[T]` supplies
the value per call. That model is sound and the consumer uses it cleanly — `internal/tenancy/tenancy.go`
is 150 lines, fail-closed, and the middleware resolves the Active Workspace once per request.

The strain shows up in three places the model has no vocabulary for:

- a tenant is also a property of an **edge** — a reference from one tenanted row to another (**F1**);
- a junction between two tenanted entities is classified **shared**, because classification is
  column-presence (**F2**);
- the escape hatch, `SkipTenancy`, carries four meanings at once (**F3**).

And one the model has no shape for at all: execution **outside a request** (**F4**).

---

## 2. Findings — where the flat model strains

### 2.1 F1 — tenancy scopes rows, not references

`tasks.cycle_id → cycles(id)` was an ordinary FK. Nothing stopped `updateTask` pointing a Task at a
Cycle in **another** workspace: the tenant filter passes (the Task is yours) and the FK only checks
that the Cycle exists. Same shape on `projects.team_id → teams(id)`.

The consumer fixed it in DDL — `migrations/0004_task_cycle_tenant_fk.up.sql`,
`0005_project_team_tenant_fk.up.sql` — with a composite FK, and said why:

> Enforced by the storage engine, so it cannot be bypassed by SkipTenancy, a resolver bug, or a
> mutation added later.

```sql
ALTER TABLE cycles ADD CONSTRAINT cycles_ws_id_key UNIQUE (workspace_id, id);
ALTER TABLE tasks  DROP CONSTRAINT tasks_cycle_id_fkey;
ALTER TABLE tasks  ADD CONSTRAINT tasks_cycle_same_ws
    FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id);
```

**And it cost them the relationship.** `ApplyConstraintToColumns` populates `FKReference` only when
`len(c.Columns) == 1 && len(c.ReferenceColumns) == 1` (`parser/schema.go:419-428` — this is
constraint **C1** in `docs/design/archive/NESTED_MUTATIONS.md`). They also dropped the single-column FK the
composite one subsumes, so the edge is invisible to sqlgen. Confirmed in the generated structs:

```go
// internal/database/task_gen.go — Task's relationship fields
Assignees, Attachments, Subtasks, Watchers, Comments, DependsOnTasks, Labels, Tasks, TimeEntries
// ...no Cycle.

// internal/database/project_gen.go — Project's relationship fields
Tasks
// ...no Team.
```

Both are now bare `CycleID` / `TeamID` scalars: no loader, no filter member, no GraphQL nested
field, and — relevant to `docs/design/archive/NESTED_MUTATIONS.md` — no nestable edge, since that design's **C1**
is the same constraint.

**The inversion is the bug.** Writing the correct, storage-enforced DDL is currently *punished*.

### 2.2 F2 — a junction between two tenanted entities classifies as shared, and it leaks

Tenancy is auto-detected by column presence (PRD §29.2.3). `task_labels`, `task_assignees`,
`task_watchers`, `task_dependencies` and `team_members` carry no `workspace_id`, so every one is
**shared** — despite both endpoints being tenanted.

**The M2M read path is not the problem.** The loader queries the junction anchored to parent ids
that were themselves tenant-filtered, then fetches targets through the target's own client, which
applies its tenant filter. A cross-tenant link therefore yields a target that is filtered out and
the row is silently dropped — constraint **C9** in `docs/design/archive/NESTED_MUTATIONS.md`. Odd, but not a leak.

**The junction's own generated client is the problem, and it is exposed** — closed by **T7**, which
stops generating the surface rather than tenant-checking it. From
`internal/graph/task_label_gen.graphqls:46-47` and `task_assignee_gen.graphqls:53-56`:

```graphql
taskLabel(taskID: UUID!, labelID: UUID!): TaskLabel
taskLabels(filter: TaskLabelFilter, first: Int, after: String, ...): TaskLabelConnection!
taskAssignees(filter: TaskAssigneeFilter, first: Int, ...): TaskAssigneeConnection!
```

The table is classified shared, so **these queries carry no tenant predicate at all**. Any
authenticated caller can enumerate every `(task_id, label_id)` and `(task_id, user_id)` pair in the
database, across every workspace. They cannot read the Tasks, Labels or Users themselves — but they
obtain the ids and the shape of the graph. That is a live cross-tenant read on the generated
surface.

Two further consequences, both visible in the consumer:

- **Events carry no tenant.** `internal/events/projector.go:58-65` hand-maintains a
  `junctionAnchors` map, and `tenantOfTask` / `tenantOfTeam` (`:155-168`) re-read the anchor entity
  **under `SkipTenancy`** to recover the tenant — one extra read per junction event, and a map that
  must be updated whenever a junction is added.
- **Cache keys are not tenant-scoped**, because `BuildTenantKey` (`cache/key.go:86`) is only used
  for tables tenancy detected. Largely moot: **FIX-200** (resolved 2026-09-16) found junction
  caching to be write-cost-only *for a junction reached only through its M2M edge* — not
  universally; the junction's own by-pair `Get(pk)` is read-through, which is exactly the shape an
  ACL link table is read in. **T6** removes the tenant from every key regardless.

**The consumer independently derived the anchor rule this document proposes.** Their map is
`task_assignees → tasks/task_id`, `team_members → teams/team_id`,
`memberships → workspaces/workspace_id`. That is exactly "the tenanted endpoint," reconstructed by
hand because the generator would not.

### 2.3 F3 — `SkipTenancy` is one flag carrying four meanings

1. Do not filter reads by tenant.
2. Do not auto-set the tenant on writes.
3. *Emergent*: "this is trusted server code." `internal/authz/authz.go:162-199` reads it as an
   authorization signal.
4. *Side effect nobody chose*: no resolved tenant means **no per-tenant cache key** can be built — so a `SkipTenancy` read cannot use the cache at all. **T6** dissolves this one: with an identity key there is always a key, and the check is simply skipped.

(4) forced a second trust channel into existence. From `authz.go:29-34`:

> SkipTenancy leaves the tenant unresolved, so the per-tenant cache key can never be built; the
> system grant carries the trust without giving up tenant resolution.

`WithSystemGrant` exists **solely** because `SkipTenancy` is too blunt for "trusted, but still
tenant-resolved."

(3) also costs them a type switch, because `CallOptions[FO]` is generic over the field-options type
and a hook therefore **cannot read it without naming every table**. `selfAuthorized`
(`authz.go:162-199`) is seven arms, five of which are dead, each carrying a comment apologising for
itself:

```go
case database.CallOptions[database.LabelFieldOptions]:
    // No server-side Label bootstrap path exists today; this case keeps the
    // switch in sync with guardedTables so a future SkipTenancy Label write
    // (e.g. seeding default Labels in bootstrapWorkspace) is trusted.
    return o.SkipTenancy
```

That maintenance hazard is sqlgen's doing, not the consumer's.

### 2.4 F4 — the resolver assumes a request

`TenantResolver[T]` is `func(ctx) (T, error)` reading a request-scoped context value
(`internal/tenancy/tenancy.go:74-82`). The activity projector runs off the request path, so there is
no Active Workspace and every write is `SkipTenancy` plus a hand-stamped `workspace_id`
(`projector.go:97-108`). Any cron, worker, or backfill lands in the same place.

**Notably, the better hatch already exists and was not used.** `CallOptions.Tenant` is documented
as *"explicit tenant: resolve tenancy to this value (filter + auto-set) instead of the ctx
resolver; wins over SkipTenancy"* (`shared_types_gen.go:18`). The projector knows the workspace id;
passing it as `Tenant` would keep tenancy resolved — preserving the cache key and not tripping
their own authz signal. That it reached for `SkipTenancy` instead is a discoverability failure:
`SkipTenancy` is the obvious name, so it is the one people find.

### 2.5 What is already correct, and must not regress

Worth recording so a redesign does not trade it away.

| Property | Evidence |
|---|---|
| Fail-closed by default under `required: true` — a tenanted operation with no resolvable tenant errors before touching the database | `tenancy.go:74-82`; PRD §29.4 |
| The resolver is cached on ctx after first call, so a nested composition pays one resolution | PRD §29.6 |
| Tenant-in-composite-PK is verify-match, not omit-from-signature | PRD §29.7; `memberships(workspace_id, user_id)` |
| `*Where` invalidation on a tenanted table evicts precisely rather than clearing across tenants | PRD §29.5. **Note:** the *precision* must survive **T6**, but its *mechanism* — per-row captured tenants used to rebuild tenant-scoped keys — does not, and does not need to: under an identity key the same PKs evict precisely with no tenant at all |
| Relationship filters inject the target's tenant predicate **inside** the correlated subquery | PRD §11.1; Phase 25.11 |

---

## 3. Proposals for the flat model

Seven. **T1**, **T6** and **T7** change existing rules; the rest are additive.

Two carry more weight than their numbering suggests. **T7** is the cheapest item here and closes the
only live leak (**F2**) — and it is what shrinks **T2** to a fraction of its original size, so it
should land first. **T6** has to happen before any multi-level work, because the tenant-keyed cache
is the single piece of the flat model that cannot be carried forward at all (§4.5).

### T1 — Decompose a composite FK into a discriminator plus correlation columns

> **Revised 2026-09-11, and the revision is the point.** This proposal first read *"recognize the
> **tenant-qualified** FK"* on the assumption that a composite FK's extra column is the tenant
> column. **That assumption is false** — **TQ1** is now answered. The extra column can be anything
> the referenced side happens to have a `UNIQUE`/PK on, and the most common real case is not
> tenancy at all: **PostgreSQL requires a partitioned table's primary key to include every
> partition-key column**, so any FK into a time-partitioned `orders` is
> `(created_at, order_id) → orders(created_at, id)`. A tenancy-shaped rule would have handled one
> instance of a general pattern and mis-modelled the rest.

The general shape is not "one column plus a tenant." It is **one discriminating column plus N
correlation columns that must be equal on both sides**:

```
   composite FK:  (created_at, order_id) ──→ orders (created_at, id)
                        │          │                    │       │
        correlation ────┘          │                    │       └── target PK
        (equal on both sides)      └── discriminator ───┘         (identifies the edge)
```

**The discriminator is identifiable structurally**: it is the column whose *referenced* counterpart
is the target's single-column primary key. Everything else in the constraint is correlation.

| Composite FK | Target PK | Discriminator | Correlation |
|---|---|---|---|
| `(workspace_id, cycle_id) → cycles(workspace_id, id)` | `cycles.id` | `cycle_id` | `workspace_id` |
| `(created_at, order_id) → orders(created_at, id)` | `orders.id` | `order_id` | `created_at` |
| `(country_code, postal_code) → regions(country_code, postal_code)` | *composite* | — | — |

The third row has no single-column PK among the referenced columns, so it does not decompose. That
is a genuine composite edge and stays **out of scope**, consistent with `docs/design/archive/NESTED_MUTATIONS.md`
**E3**, which already defers composite-PK parents.

**What changes.** `FKReference` is single-column today (`parser/schema.go:215-219`) and would grow
to carry the discriminator plus its correlations:

```go
type FKReference struct {
    Table   string
    Schema  string
    Column  string        // the discriminator's referenced column — unchanged meaning
    Correlate []Correlation // new: pairs that must be equal on both sides
}

type Correlation struct {
    LocalColumn, ReferenceColumn string
    IsTenant                     bool // set when LocalColumn is the configured tenant column
}
```

**Loaders already have the primitive.** `BuildCompositePKBatchCondition(d, columns, valueSets)`
(`sql/builder.go:404-412`) emits a multi-column `IN`, with the dialect split already handled —
tuple `IN` where supported, expanded `OR` otherwise. A correlated O2M load is that call rather than
today's single-column `IN`:

```sql
-- children of parents P, correlated on created_at
WHERE (created_at, order_id) IN ((p1.created_at, p1.id), (p2.created_at, p2.id), …)
```

with one worthwhile specialization: **when a correlation column is the tenant column it is constant
across the whole request**, so it collapses to a scalar equality instead of a tuple — `WHERE
cycle_id IN (…) AND workspace_id = $tenant` — and the emitted SQL is byte-identical to today's plus
a predicate sqlgen already emits everywhere else.

**Nested writes** assign every FK column from the parent rather than one, and **C2** elides all of
them from the child input rather than one. Both are list-shaped versions of what the code does now.

**`IsTenant` is a specialization, not the mechanism.** It exists so **T3** knows not to warn about a
reference the storage engine already constrains, and so the security property is legible in the
manifest. The decomposition itself is ordinary FK machinery that tenancy happens to benefit from —
which is why it also unlocks partitioned-table schemas sqlgen cannot model at all today.

**Cost.** This is *medium*, not the "one clause" the first draft claimed: a parser type change, a
discriminator rule, correlated loaders, N-column FK propagation in nested writes, and N-column
elision in **C2**. It is not exotic — composite PKs already exercise most of the same primitives —
but it should be costed as its own ticket rather than folded into a tenancy phase.

### T2 — Derived junction tenancy, scoped to the paths that still need it

> **Rewritten 2026-09-11.** This proposal was much larger: it applied a correlated `EXISTS` to
> *every* junction read, including the M2M relationship loader, which put it on a hot path and made
> "default-on or opt-in" a real fight (the old **TQ2**). **T7** removes most of the need. With the
> junction's public API surface gone, the loader turns out to require no check at all, and what
> remains is small.

**The M2M loader is deliberately excluded, and that is the whole saving.** It is already safe
without a junction-side predicate:

| Edge | Junction read | Target fetch | Outcome |
|---|---|---|---|
| `Task.Labels` (both endpoints tenanted) | anchored to the caller's tenant-filtered tasks | `labels` filtered by its own client | a cross-tenant label is dropped — **C9** |
| `Task.Assignees` (one endpoint tenanted) | anchored to the caller's tenant-filtered tasks | `users` is shared; nothing to filter | the anchor alone is sufficient |

The parent side is tenant-scoped before the junction is ever queried, so every row the loader sees
is anchored to a row the caller already owns. Adding a predicate there would buy nothing and cost a
semi-join on the project's most-optimized read path. **Excluding it is a decision, not an
oversight.**

**What still needs derived tenancy**, all of it off the hot path:

1. **Writes.** `TaskLabels().Create(…)` is unguarded — it will happily link the caller's task to
   another tenant's label. Guarded in one statement, no read-before-write, failing closed:

   ```sql
   INSERT INTO task_labels (task_id, label_id)
   SELECT $1, $2
   WHERE EXISTS (SELECT 1 FROM tasks  WHERE id = $1 AND workspace_id = $tenant)
     AND EXISTS (SELECT 1 FROM labels WHERE id = $2 AND workspace_id = $tenant)
   ```

2. **Events.** A junction mutation publishes no tenant today, which is why the consumer's projector
   hand-maintains `junctionAnchors` and re-reads the anchor per event (§2.2). Classifying the
   junction as derived-tenanted means the write path resolves the ambient tenant like any tenanted
   table and stamps it — **no extra read**, because the write was already scoped to that tenant by
   the guard above.

3. **Direct client reads.** `TaskLabels().GetMany(…)` in Go still bypasses tenancy after **T7**
   hides the API surface. The same correlated `EXISTS` applies — and the cost objection is gone,
   because this is server-side code on a cold path, not the loader.

**Check every tenanted endpoint, not just an anchor.** Where a check runs, it covers all endpoints
that can contribute scope, which self-adjusts to the two shapes with no special case: with both
endpoints tenanted the second check is belt-and-braces; with one tenanted and one shared there is
only one endpoint that *can* be checked, and it carries the full weight. The alternative —
anchor-only — leaves a corrupt row **invisible through the loader but visible through the direct
client**, which is the same row giving two answers.

**Tenant derivation still picks one endpoint.** Filtering checks all; *deriving* a value to stamp
on an event needs exactly one. Since every tenanted endpoint must agree for the row to be visible,
any of them yields the same answer — pick deterministically (first FK in column order).

**Derivation is one hop, never transitive.** If a junction's endpoint does not itself carry the
tenant column, do not chase it further to *its* parent. Transitive derivation means arbitrary join
chains, unpredictable cost, and a rule no one can evaluate by reading the schema. One hop, or the
junction is shared.

**The boundary, which is easy to over-read.** Derived tenancy guarantees *the anchor is yours*. It
says nothing about whether the other end should have been linkable. With `task_assignees`, nothing
stops assigning any global `User` to your task — whether that user is a member of your workspace is
an **authorization** question answered by Membership, not a tenancy question, and it is precisely
what the consumer's `internal/authz` exists for.

### T3 — Lint for cross-tenant-capable references

Purely structural, no new syntax: tenanted table A has a single-column FK into tenanted table B,
and no composite FK carries the tenant.

```
$ sqlgen validate

  tasks.cycle_id → cycles.id
    both tables are tenanted on `workspace_id`, but the reference is not.
    updateTask can point a Task at a Cycle in another workspace; the tenant
    filter passes (the Task is yours) and the FK only checks existence.

    fix: FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
         (requires UNIQUE (workspace_id, id) on cycles)
```

Those are the two the consumer found by reasoning. Nothing told them whether they had found all of
them.

**This lint is gated on T1.** Recommending a composite FK while `parser/schema.go:419-428` still
drops it means recommending the adopter trade a relationship for the constraint — which is the
trade §2.1 identifies as the bug. Ship **T3** after **T1**, or it advises people into the same
hole the consumer fell into.

Once **T1** lands, the lint can also stay quiet on references the storage engine already
constrains, by reading `Correlation.IsTenant`.

### T4 — Put tenancy on `hook.MutationContext`

```go
type MutationContext struct {
    // ...
    Tenant     any          // resolved tenant, nil when none
    TenantMode TenancyMode  // TenancyResolved | TenancyExplicit | TenancySkipped
}
```

collapses `authz.go:162-199` from seven arms to:

```go
func selfAuthorized(ctx context.Context, m *hook.MutationContext) bool {
    return systemGranted(ctx) || m.TenantMode == hook.TenancySkipped
}
```

Table-agnostic, nothing to keep in sync, and it splits **F3**'s meaning (3) — "trusted" — from
meanings (1) and (2) by letting a hook see *how* the tenant resolved rather than inferring it from
an opt-out flag.

### T5 — A ctx-level tenant for job scope

`CallOptions.Tenant` already covers per-call (§2.4). What is missing is job scope:

```go
ctx = tenancy.WithTenant(ctx, workspaceID)   // whole job runs as this tenant
_, err = p.client.Activities().Create(ctx, input)
```

Small, and it makes the background case obvious instead of leaving `SkipTenancy` as the path of
least resistance. It becomes a **prerequisite** rather than a nicety if **T2** lands, since derived
junction writes fail closed when no tenant resolves.

### T6 — Remove the tenant from the cache key

**The tenant segment partitions nothing, and it is the one piece of the flat model that cannot be
carried into a multi-scope one.**

Today a tenanted table keys through `BuildTenantKey` / `BuildCompositeTenantKey`
(`cache/key.go:86`), giving the grammar

```
{prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fp}:pk:{pk}
```

**The root cause is a category error: the key mixes *identity* with *audience*.**

sqlgen has an **entity cache**, not a result-set cache. The query hook passes everything that is
not a single-row `Get` straight through — `if q.Op != hook.OpGet { return next(ctx, q) }`
(`cache.go.tmpl:781`) — and bypasses even that for `SkipCache` and relationship-loaded reads
(§27.6). Bulk reads are uncached because their filter permutations are unbounded (PRD §27.7). So
the cache holds exactly one shape of thing: **one row, keyed by its own primary key, carrying its
own tenant on the value.**

Tenant-in-key is the right design for the *other* kind of cache. If you cached the answer to
`GetMany(filter)`, that answer would be valid only for the tenant whose predicate produced it, and
the asker would have to be part of the key. sqlgen never caches a query result — so putting the
tenant in the key encodes **who asked** into a key that identifies **what was asked about**.

Every symptom traces to that one mistake:

| Symptom | Because |
|---|---|
| The segment duplicates on `workspace_settings` (below) | the asker *is* part of the identity there, so it lands twice |
| It cannot survive multi-scope | one row has N possible askers, but only one identity |
| `InvalidateMany` is refused on tenanted tables | the invalidator knows the row, never the asker |
| The event adapter must ferry a tenant stamp across processes | an invalidation signal is about a row; the key demands an asker |

Removing it is therefore not a trade so much as a correction — and because there is only one read
entry point, the replacement is **one comparison in one generated function per table**. The bulk of
**T6** is deletion on the invalidation side; the read-side addition is a single `if`.

**It does not separate anything.** A row has exactly one tenant, so there is exactly one entry per
row with or without the segment — the entry count is identical. The segment re-encodes a value
already present on the cached entity, because §27.6 guarantees only *full* entities are cached and
the tenant column is therefore always on the value. On a tenanted table whose tenant column sits
inside the primary key it is re-encoded *twice*:

```go
// cmd/sqlgen/testdata/examples/graphql/models/cache_gen.go:4110-4115
func (c *Cache) keyForWorkspaceSetting(tenant uuid.UUID, pk WorkspaceSettingPK) string {
    return cache.BuildCompositeTenantKey(..., tenant, []any{pk.WorkspaceID, pk.Key})
}
```

```
taskr:public.workspace_settings:tenant:A:fingerprint:v1:pk:A:theme
                                       ▲                    ▲
                                       └── tenant segment   └── the same value, as PK[0]
```

What the segment actually provides is a **lookup gate**: a reader cannot form the key without
already knowing the tenant. That is an access check in the shape of a key — and it works only when
scope is exactly one known value.

**It does not survive multi-scope.** Take row R in workspace W, visible to org O:

| Keying | Result |
|---|---|
| under W only | org-level reads never hit; the cache is dead for them |
| under O only | workspace-level reads never hit |
| under **both** | two entries for one row, and evicting R means enumerating every scope that can see it |

Only the third is correct, and it breaks eviction rather than merely costing storage:
`dispatchMutation` snapshots `AffectedTenants` — *the row's own* tenant, one value per row
(`cache.go.tmpl:859`). Under containment it would have to capture the full ancestor set on every
write and get it right, or entries go stale invisibly. The mutation path has no way to perform that
walk.

**The proposal.** One grammar for every table, and tenancy checked on the value:

```
key:   {prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}
hit:   entity.<TenantField> ∈ scope  ?  serve  :  treat as a miss
```

In the flat case this is **provably equivalent** — same entry count, same behavior — minus the
segment. Under set scope or containment it is the only shape that works: one entry per row however
many scopes can see it, eviction by identity with no tenant set to enumerate, and a check that
degrades correctly (flat scope is a one-element set, containment a descendant set, `SkipTenancy`
skips it entirely).

**What this removes**

| Removed | Why it can go |
|---|---|
| `BuildTenantKey`, `BuildCompositeTenantKey`, `writeTenant` | one grammar replaces the tenanted/untenanted split |
| `BuildTenantTablePattern` | **never called by generated code** — it appears only in comments and error strings |
| `invalidateAffectedTenanted` and the tenanted arms of the invalidation switch | invalidation is by identity |
| The `InvalidateMany` refusal on tenanted tables (`examples/tenancy/expected/cache_gen.go:3253`) | the public API starts working on every table; it fails today *only* because the caller cannot build a key without each row's tenant |
| The cache's consumption of `AffectedTenants` | — |

**What it does not remove.** `hook.MutationContext.AffectedTenants` **stays**: the event hooks
consume it for the §29.6 per-event tenant stamp (`event_hooks.go.tmpl:197-208`), and that consumer
genuinely wants the row's own tenant. T6 drops the cache's use of it, not the field.

**What is lost: effectively nothing.**

Per-tenant bulk eviction goes away, and it costs less than it first appears — **not because it is
unused, but because it is no cheaper than what replaces it.** `InvalidatePattern` is a full
keyspace scan on every backend:

```go
// cache/memory/memory.go:176-181
for k := range b.cache.Keys() {          // every key in the cache
    if strings.HasPrefix(k, prefix) { victims = append(victims, k) }
}
```

Redis is the same shape (`SCAN` + `DEL`). So a per-tenant pattern and a per-table pattern pay the
**same walk** and differ only in how many keys they delete at the end — a selectivity win on an
operation that is already O(keyspace), not a performance one.

| Scenario | Covered by |
|---|---|
| Normal mutations | per-row invalidation — generated code already keys per row and never uses the tenant pattern |
| Tenant offboarding, DB-side cascade delete | `InvalidateTable`. Not tenant-specific: it is the general "the database changed behind sqlgen" case, same as a restore or a manual `UPDATE` |
| Incident response — "flush tenant X" | `InvalidateTable`; rare and operational, so coarseness is acceptable |
| Right-to-erasure over cached data | `InvalidateTable`. Over-deleting cached data is never the compliance problem, and TTL bounds it regardless |
| Memory pressure | TTL and the backend's own size eviction, not targeted invalidation |

Over-evicting other tenants during a rare operation costs a brief miss storm. Keeping the
capability costs the tenant segment on **every key, forever**, plus everything in the removal table
above. The only residual argument is speculative — a future backend might evict per-tenant
efficiently (a per-tenant hash, keyspace notifications) — and if that ever mattered the answer is a
tenant→keys index built then, not a segment paid for now.

**The one thing genuinely traded away:**

- **The lookup gate.** A cross-tenant hit stops being unformable and becomes checked. These defend
  the same thing: with the segment, the value at `…:tenant:A:pk:R` is still the full row including
  `workspace_id = A`, so anyone with raw backend access reads it either way. The segment protects
  *sqlgen's own read path* from a bug — which is exactly what the value check protects. Both reduce
  to "one generated helper must be correct"; the comparison is the easier of the two to test.

**Where the check lives — decided: generated code, not `cache/`.**

`cache/` is a storage abstraction: get, put, invalidate. The check is a domain concern and belongs
with every other tenancy decision, all of which are already generated — `resolveTenant`, `apply`,
the `WHERE` predicate, §29.7 verify-match. Splitting it across a runtime module and the generated
code is the "one fact, two derivations" failure **NW-D1** and **FIX-153** exist to prevent.

It also needs no new home: `readThrough<T>` is *already* generated (a method on the generated
`*Cache` facade in the models package, e.g. `cache_gen.go:3620`), so the comparison lands where the
key construction already is. `cache.Backend` stays a pure key-value interface.

The objection — "N copies of a security-relevant check" — does not hold: it is **one template arm
rendered N times**, so a bug is one bug in one place and it appears in every golden file. That is
more visible than a runtime helper, not less.

**The outcome: `cache/` ends up with zero tenancy concepts.** Today it holds three:

| Site | Today | After T6 |
|---|---|---|
| `key.go:73-110` | `BuildTenantKey`, `BuildCompositeTenantKey`, `BuildTenantTablePattern`, `writeTenant` | deleted — one grammar |
| `invalidation.go:52` | `InvalidationSignal.Tenant string` | deleted — a signal is table + PKs |
| `event_adapter.go:71-79` | reads `ev.Metadata["tenant"]` so the receiving facade can rebuild tenant-scoped keys | deleted — identity keys need no stamp |

The third carries a benefit worth calling out on its own. The adapter's contract today is that a
missing tenant stamp *"leaves `Signal.Tenant` empty (the facade then **degrades tenanted tables to
a full-table clear**)"* (`event_adapter.go:71-73`). So cross-process invalidation silently
over-evicts whenever the stamp does not survive the hop — a fallback that exists only because the
key needs a tenant to rebuild. Under T6 cross-process invalidation is precise for tenanted tables
**unconditionally**, and the degradation path disappears.

To be precise about what is *not* removed: the event's own `metadata["tenant"]` stamp (PRD §29.6)
stays — subscribers filter on it. T6 stops the **cache adapter** consuming it, exactly as it stops
the cache consuming `AffectedTenants` without removing the field.

**The invariant must be replaced, not dropped.** A failing-first regression — *tenant B gets a miss
for tenant A's cached row* — is what now carries it, and it is a more direct assertion than the
key-shape tests it replaces.

Phase 23 (*Cache × Tenancy — Nil-Tenant Invalidation*, six sub-items) exists because a tenanted key
must answer "what key when there is no tenant?" T6 dissolves that question rather than answering
it.

---

### T7 — A detected junction defaults to no generated API surface

**This is the cheapest fix in the document and it closes the only live leak.**

**F2** is live because sqlgen is table-driven: every table gets a full public read surface, and a
junction gets one it was never meant to have.

```graphql
# internal/graph/task_label_gen.graphqls:46-47 — generated, and unscoped
taskLabel(taskID: UUID!, labelID: UUID!): TaskLabel
taskLabels(filter: TaskLabelFilter, first: Int, after: String, …): TaskLabelConnection!
```

A service reads `task.labels`. It does not read `task_labels` rows — the junction is how the
relationship is *implemented*, not something the domain models. Exposing it publishes an
implementation detail, and because the table classifies as shared (§2.2) it publishes it unscoped.

**The mechanism already exists.** `TableAPIConfig.Enabled` is a tri-state pointer — nil inherits
the global, `true` exposes, `false` excludes (`config/config.go:709-712`). Only the **default**
changes:

> A parser-detected M2M junction defaults to `api.enabled: false`. A junction that is genuinely a
> domain entity opts back in explicitly.

No new predicate, no new syntax, no runtime cost — a default flip plus golden churn.

**Why this must be a default with an override, and cannot be inferred.** The obvious rule —
"a pure link table is plumbing" — does not survive contact with real DDL. From the consumer:

| Junction | Beyond the two FKs | Actually |
|---|---|---|
| `task_labels` | — | plumbing |
| `task_assignees` | `assigned_at` (defaulted) | plumbing |
| `task_watchers` | `created_at` (defaulted) | plumbing |
| `team_members` | `added_at` (defaulted) | plumbing |
| `task_dependencies` | `type` (defaulted), `created_at` | arguably either |
| `memberships` | `role` (defaulted), timestamps | **an entity** — it has its own admin mutations |

Neither candidate predicate separates them. *"Exactly two columns"* catches only `task_labels` and
wrongly promotes `task_assignees`. *"No column the caller must supply"* — **E5**'s phrasing in
`docs/design/archive/NESTED_MUTATIONS.md` — is the opposite failure: every one of these has defaults, **including
`memberships.role`**, so it would demote a first-class entity to plumbing. Whether a junction is an
entity is a modelling fact the schema does not carry, so it is config with a sensible default, not
detection.

**What it buys beyond closing F2:** it is what shrinks **T2** from "correlated `EXISTS` on the M2M
loader, default-on, golden churn everywhere" to "guard writes, stamp events, filter a cold Go-side
read." The expensive part of derived tenancy existed to protect a surface that should not have been
generated.

**The same insight is already filed against two other surfaces.** **FIX-200** proposed junctions
default to no caching; **E5** gates nested-write eligibility on junction shape. API, cache and
nested writes are three consumers of one fact — *this table is plumbing* — and all three currently
try to infer it separately. Whatever config key answers it here should answer it for all three
rather than becoming a third predicate.

FIX-200 closed as documentation rather than a default change (2026-09-16), and two findings from it
constrain this proposal. **First, the predicate is not free to invent**: FIX-200 cited E5 for “pure
link table, no payload,” but E5 actually reads *no **required** column beyond the two FKs* — and the
two readings disagree on `tenancy`'s `tag_links`, whose NOT NULL `workspace_id` sits outside the PK
while its own schema comment calls it a pure junction. A tenant column is precisely the case a
tenancy-side predicate has to settle. **Second, inference alone cannot answer it**: measured, a
config-declared `tables.<t>.relationships` M2M over a surrogate-PK table with no composite
constraint generates the full M2M edge while `junctionConstraint` returns false, so any flag derived
only from schema detection is blind to a junction the config explicitly names. That is an argument
*for* the config key this section proposes rather than a fourth inference — but the key has to be
set at both sources, not just the parser.

---

---

## 4. Multi-level tenancy — the design space

ADR-0004 in the consumer records that flat was chosen *because of the tool*:

> We chose flat tenancy (not an Organization → Workspace hierarchy) because sqlgen's tenancy is v1
> flat/single-level — a second level would be hand-rolled against the grain of the tool.

So the constraint is real and shaping adopter schemas. What follows is the space, not a decision.

### 4.1 Three independent axes

"Multi-tenancy" bundles three different asks. Separating them is most of the work.

| Axis | Flat model today | The ask |
|---|---|---|
| **Cardinality** | resolver returns **one** value; filter is `= $tenant` | scope to a **set** — "my five workspaces" |
| **Depth** | one level | containment — Org → Workspace → … |
| **Multiplicity** | one tenant column | several independent axes — e.g. workspace × region |

They are genuinely independent. The consumer needs cardinality *today* and does not have it: the
`X-Workspace-Id` header selects exactly one Active Workspace (`tenancy.go:29`), so a user who
belongs to five workspaces cannot ask "show me my tasks across all of them." That is a flat-model
limitation with nothing to do with hierarchy.

### 4.2 Option survey for depth

| # | Approach | Read predicate | Cost |
|---|---|---|---|
| **D1** | **Denormalize every level onto every row** — `tasks(org_id, workspace_id, …)` | `WHERE org_id = $x` or `workspace_id = $y` | Fastest read; every table grows a column per level; every write must maintain them; auto-detect must choose which column is "the" tenant |
| **D2** | **Closure / ancestor table** — `tenant_tree(ancestor, descendant)` | `WHERE workspace_id IN (SELECT descendant FROM tenant_tree WHERE ancestor = $scope)` | One column per row; a subquery per read; the tree is app-maintained |
| **D3** | **Materialized path** — `tenant_path ltree` | `WHERE tenant_path <@ $scope` | Elegant and indexable; **PostgreSQL-only**, which conflicts with the three-dialect rule |
| **D4** | **Recursive CTE** over a self-referential tenant table | `WITH RECURSIVE …` | Portable, no extra table; most expensive, and hard to compose inside an existing predicate |

### 4.3 The unification — set-valued scope subsumes depth

The important observation: **D2's subquery and a plain `IN` list are the same predicate shape.**
They differ only in whether the descendant set is computed in the application or in the database.

So if the generator emits

```sql
WHERE <tenant_col> IN (<scope>)
```

then hierarchy becomes a **resolver** concern rather than a generator concern. The app decides
whether `<scope>` is one id (flat, today), the caller's five workspaces (cardinality), or every
descendant of an org (depth). sqlgen never learns the word "hierarchy."

That requires splitting the resolver, because reads and writes want different things — a write must
name **one** tenant:

```go
type TenantResolver[T comparable] interface {
    ReadScope(ctx context.Context) (Scope[T], error)  // set: values, or a subquery
    WriteTenant(ctx context.Context) (T, error)       // exactly one
}
```

For the flat case both derive from a single value and today's behavior is unchanged.

### 4.4 What still needs generator support

Set-valued scope does not make everything free.

- **Writes remain single-valued.** If a caller is scoped at org level and creates a Task, *which*
  workspace? There is no defensible default, so `WriteTenant` must be explicit and a write with an
  ambiguous scope is an error. This is the part hierarchy cannot abstract away.
- **§29.7 verify-match** (tenant inside a composite PK) becomes: the supplied tenant must be **in**
  the read scope, and **equal** the write target.
- **`D1`-style denormalized levels** would need `tenancy.column` to accept a list, plus a rule for
  which level auto-detect treats as the row's own tenant.

### 4.5 What breaks, honestly

- **Cache keys — already handled, by T6.** This bullet previously sat under "what breaks": a *set*
  scope cannot be a key component without exploding the keyspace, and keying a row under every
  scope that can see it fragments one row into N entries whose eviction requires an ancestor walk
  the mutation path cannot perform. **T6** removes the tenant from the key for reasons that hold in
  the flat model alone, and the multi-scope case then needs no further change: one entry per row,
  eviction by identity, and the hit check widens from `== tenant` to `∈ scope`. T6 is therefore a
  **prerequisite** for §4, not a consequence of it — which is the main reason it is worth doing
  even if multi-level never ships.
- **Parameter limits.** A resolver returning 10,000 workspace ids renders a 10,000-element `IN`
  list — the same PostgreSQL 65535 bind-parameter cliff `docs/design/archive/NESTED_MUTATIONS.md` §14 **Q9**
  raises for `connect`/`disconnect`. Above some size the scope must become a subquery (D2), which
  is an argument for `Scope[T]` supporting both forms from the start.
- **`required: true` semantics.** Does an empty scope mean "no access" (fail closed, correct) or
  "unscoped"? It must be the former, and it must be impossible to spell the latter accidentally.

---

## 5. Recommended sequencing

**T7 → T6 ∥ T1 → T3 → T2 → T4/T5**, then revisit depth.

1. **T7** first, on cost-to-value alone: a tri-state default flip plus golden churn, and it closes
   the only live leak in this document. It is also a prerequisite in practice rather than in
   principle — **T2** is sized around it, and doing **T2** first would mean building an `EXISTS`
   hot path to protect a surface **T7** deletes.
2. **T1** next *despite being the largest* — not because it is cheap (it is medium; the original
   "one clause" estimate died with **TQ1**), but because everything else is worth less without it.
   It removes an active disincentive to writing correct DDL, restores relationships that exist in
   the schema today, and — now that it is general rather than tenancy-shaped — unlocks
   partitioned-table schemas sqlgen cannot model at all. **T3** is near-useless while its
   recommended fix still costs the adopter a relationship.
3. **T3** then: it tells adopters where **T1** applies, and is near-useless before it.
4. **T2**, now much smaller — guarded writes, event stamping, and a cold-path read filter. It
   needs **T5**, since a derived junction write fails closed when no tenant resolves.
5. **T4** and **T5** are small and independent; **T5** is pulled forward by **T2**.
6. **T6** runs in parallel with all of it — it touches only the cache layer and shares no code with
   **T1**–**T5**, **T7** included. Worth starting early rather than late: it is a **net deletion** (two key builders,
   a pattern helper, the tenanted invalidation arms, the `InvalidateMany` refusal), it removes the
   question Phase 23 spent six sub-items answering, and it is a hard prerequisite for §4 (§4.5).
   The one thing that makes it *not* free is that it changes every tenanted table's cache key, so
   it wants its own golden refresh and the failing-first isolation test landing together.

**On depth: do T6, then the cardinality half, and do not build hierarchy yet.** §4.3 is the reason —
set-valued scope is the substrate hierarchy needs anyway, it delivers the "across my workspaces"
query the consumer cannot express today, and it keeps hierarchy out of the generator entirely. If
depth is still wanted after that, D2 composes with it at the resolver with no further generator
change.

This also revises an earlier position in this repo's review notes, which said depth was purely
hypothetical. It is not *urgent*, but ADR-0004 shows it is already shaping adopter schemas, and
§4.3 means the enabling step pays for itself independently.

---

## 6. Open questions

| # | Question | Why open |
|---|---|---|
| ~~**TQ1**~~ | ~~Is "the extra column is exactly the tenant column" true in general?~~ | **ANSWERED — no**, 2026-09-11. The extra column can be any column the referenced side has a `UNIQUE`/PK on, and the most common real case is not tenancy: PostgreSQL requires a partitioned table's PK to include every partition-key column, so an FK into a time-partitioned table is `(created_at, order_id) → orders(created_at, id)`. **T1** was rewritten from a tenancy-shaped rule into general discriminator + correlation decomposition, and re-costed from "one clause" to medium. The tenant case survives as `Correlation.IsTenant`, a flag **T3** consumes. |
| ~~**TQ2**~~ | ~~Is derived junction tenancy (**T2**) default-on or opt-in?~~ | **LARGELY DISSOLVED**, 2026-09-11. The question had teeth only because the old **T2** put a correlated `EXISTS` on the M2M loader. **T2** as rewritten excludes the loader deliberately — it is already safe via parent anchoring plus target-side filtering — so what remains (guarded writes, event stamping, cold Go-side reads) has no hot path and no loader golden churn. Default-on is now uncontentious. *Original text retained below for the record.* |
| ~~**TQ2** (original)~~ | Default-on is fail-closed and closes **F2**; nothing is published so there is no migration cost. But it adds a subquery to the M2M loader's hot path for every untenanted junction — the one place §25.1's query-count work was most careful — and "we added a subquery to your hot path" should be a decision, not a side effect. Lean: default-on, because the alternative ships a generator that emits an unfiltered cross-tenant query by default and relies on the user noticing. |
| ~~**TQ3**~~ | ~~When both junction endpoints are tenanted, assert agreement or trust one anchor?~~ | **ANSWERED — check every tenanted endpoint**, 2026-09-11, folded into **T2**. Anchor-only leaves a corrupt row invisible through the loader but visible through the direct client — the same row giving two answers. Checking all endpoints self-adjusts to both shapes with no special case, and the cost objection died with the loader exclusion. Derivation (which tenant to *stamp*) still picks one endpoint deterministically, since all tenanted endpoints must agree for the row to be visible at all. |
| **TQ4** | Does `Scope[T]` (§4.3) carry values, a subquery, or both? | Both is more machinery; values-only inherits the parameter cliff in §4.5. Settle before the resolver interface changes, since it is the signature everything else hangs off. |
| ~~**TQ6**~~ | ~~Is losing per-tenant bulk eviction acceptable?~~ | **ANSWERED — yes**, 2026-09-11. Not merely unused: **it is no cheaper than the alternative.** `InvalidatePattern` is a full keyspace scan on every backend — `for k := range b.cache.Keys()` with a prefix test (`cache/memory/memory.go:176-181`), and `SCAN` + `DEL` on Redis — so a per-tenant pattern and a per-table pattern cost the *same walk* and differ only in how many keys they delete at the end. It is a selectivity win on an operation already O(keyspace). See **T6** for the scenario-by-scenario replacement. |
| ~~**TQ7**~~ | ~~Where does the **T6** hit check live?~~ | **ANSWERED — generated code**, 2026-09-11. `cache/` is get/put/invalidate; the check is a domain concern and belongs with every other tenancy decision, all of which are already generated. Folded into **T6**, along with the finding that this leaves `cache/` with zero tenancy concepts across all three sites that hold them today. |
| **TQ5** | Should `SkipTenancy` be deprecated in favour of explicit modes once **T4** and **T5** land? | Its four meanings (**F3**) are what make it dangerous. With `Tenant`, `WithTenant` and `TenantMode` available, the remaining honest use is "genuinely untenanted read" — a much narrower flag that deserves a narrower name. |

---

## References

- Consumer: `../sqlgen-example` — `internal/tenancy/tenancy.go`, `internal/authz/authz.go`,
  `internal/events/projector.go`, `migrations/0004`–`0005`, `docs/adr/0004-flat-workspace-tenancy-global-users.md`
- PRD §29 (tenancy), §27.5/§29.5 (tenant cache keys), §11.1 (relationship-filter subqueries)
- `parser/schema.go:419-428` — **C1**, the single-column FK restriction
- `sql/condition.go:152` — `sql.Exists`, the correlated-EXISTS primitive **T2** builds on
- `cache/key.go:73-95` — `BuildTenantKey` / `BuildCompositeTenantKey`, the segment **T6** removes
- `cmd/sqlgen/testdata/examples/graphql/models/cache_gen.go:4110-4115` — the double-encoded tenant on `workspace_settings`
- `cmd/sqlgen/testdata/examples/tenancy/expected/cache_gen.go:3253` — `InvalidateMany` refusing every tenanted table
- `cmd/sqlgen/gen/templates/event_hooks.go.tmpl:197-208` — the `AffectedTenants` consumer **T6** must not break
- `docs/design/archive/NESTED_MUTATIONS.md` — **C1**, **C9**, **NW-D13**, and §14 **Q9** (the parameter cliff)
- `docs/tracker/fixes.md` **FIX-200** — junction caching is write-cost-only under the current loader
