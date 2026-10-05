# Tenant-Scoped Reference Verification — Design & Plan

> **Status:** proposed (design record). Verified against source at HEAD `0bc8c41`. §29 tenancy scopes
> **rows** by a column predicate. It says nothing about whether a foreign key a mutation *writes*
> points at a row inside the caller's tenant, so a caller can point their own row at another
> tenant's row through any generated create or update. This proposes closing that with a
> **generated descriptor + one runtime mutation hook**, rather than by splicing checks into the
> mutation templates. Sibling to PRD §26.4 and
> PRD §29.4.2, and to PRD §29.

## 1. Motivation

### 1.1 Symptom

`tasks` and `cycles` are both tenanted by `workspace_id`. `tasks.cycle_id` references `cycles(id)`.
The generated update input offers it:

```graphql
input UpdateTaskInput {
  projectID:    UUID
  parentTaskID: NullUUID
  cycleID:      NullUUID
  # …
}
```

`updateTask` filters by tenant on the WHERE side, so a caller can only reach *their own* Task. But
nothing constrains the **value** they write into `cycle_id`. A member can point their Task at a
Cycle in another Workspace. The row stays in their tenant; the reference does not.

Reads then leak across the boundary through the relationship loader, because the child load is
scoped by the FK, not re-checked against the tenant.

### 1.2 This is not one column

Measured on the reference consumer (`sqlgen-test`) — 14 tenanted tables, and every FK from a
tenanted table to another tenanted table is exposed the same way:

| table | FK column | → tenanted target | nullability |
|---|---|---|---|
| `tasks` | `project_id` | `projects` | NOT NULL |
| `tasks` | `parent_task_id` | `tasks` | nullable |
| `tasks` | `cycle_id` | `cycles` | nullable |
| `comments` | `task_id` | `tasks` | NOT NULL |
| `time_entries` | `task_id` | `tasks` | NOT NULL |
| `cycles` | `team_id` | `teams` | nullable |
| `projects` | `team_id` | `teams` | nullable |

Seven, on a mid-sized schema. `UpdateTaskInput` alone exposes three of them, and `projectID` moves a
Task into another Workspace's Project — a bigger blast radius than the `cycleID` case that surfaced
this.

The count is the argument. A defect that appears once is a bug to hand-fix; one that appears
wherever two tenanted tables reference each other is a **generator** concern, because only the
generator knows the FK graph.

### 1.3 What already works, and where it stops

**`access: read_only` on the FK column** removes it from the create *and* update inputs while
keeping it readable and filterable. Verified by generation:

```yaml
tables:
  tasks:
    column_map:
      cycle_id: { access: read_only }
```
```graphql
type Task             { id: ID!, workspaceID: String!, title: String!, cycleID: String }
input TaskFilter      { …, cycleID: StringComparator }
input CreateTaskInput { workspaceID: String!, title: String! }   # gone
input UpdateTaskInput { title: String }                          # gone
```

This is real and available today — it closes 4 of the 7 above with no generator change. Two limits:
it **removes** rather than **validates**, so it only helps when every write goes through a blessed
resolver; and §32.4 rejects `read_only` on a required-on-create column, which excludes the three
`NOT NULL` cases unless the table's create API is also masked off.

**A composite foreign key** is the strongest answer and is not a sqlgen feature:

```sql
ALTER TABLE cycles ADD CONSTRAINT cycles_ws_id_key UNIQUE (workspace_id, id);
ALTER TABLE tasks  ADD CONSTRAINT tasks_cycle_same_ws
    FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles(workspace_id, id);
```

The database enforces it. Zero round-trips, and it cannot be bypassed by `SkipTenancy`, by raw SQL,
by a bug in a blessed resolver, or by a mutation added next year that nobody remembered to guard.
MATCH SIMPLE skips the check when `cycle_id IS NULL`, which is exactly right for a nullable FK. It
costs a migration plus a redundant unique index per target, and it does not apply when the FK target
is **untenanted** (`tasks.reporter_id → users`); constraining that is membership policy, not tenancy.

**This design targets the remaining case:** a schema that cannot take the migration, or a consumer
who wants the raw path to be safe rather than removed.

## 2. Why not splice the check into the mutation templates

The obvious implementation emits the verification inline in each mutation body, gated on a
`tenancy.verify_references: true` config flag. It works, and it is worse on five counts:

- **Six splice points.** `create`, `create_many`, `update`, `update_many`, `update_where`, `upsert`
  each need the block, and each needs it in the right place relative to tenant resolution.
- **Two batching strategies, generated twice.** A single-row create wants one multi-`EXISTS` probe;
  a 200-row `CreateMany` wants a per-target-table `IN` probe, because N inputs × M FK columns is
  600 subqueries. Both shapes would live in templates.
- **Config-gated means golden churn for everyone** who turns it on, and a second code path through
  every mutation template forever.
- **Template-resident logic is template-tested.** The interesting parts — dedup, batch collection,
  diffing found-vs-requested — are ordinary algorithms that deserve ordinary table-driven tests.
- **It re-derives what the hook chain already provides**: ordering, panic recovery, `SkipHooks`,
  and the ambient transaction.

## 3. Decision record

| # | Decision | Rationale |
|---|---|---|
| **D1** | **The generator emits data, not control flow.** Each generated input type carrying ≥1 FK into a tenanted table gets a `TenantScopedReferences()` method returning a `[]tenancy.Reference` descriptor. No mutation template changes. | The only thing the generator uniquely knows is the FK-to-tenanted-table graph. Emitting it as a value keeps every mutation body byte-identical and moves the verification algorithm into testable runtime code. |
| **D2** | **Verification is one runtime `hook.MutationHook`**, installed by the consumer, not a config flag. | `hook.MutationHook` is `func(next MutationHandler) MutationHandler` — a middleware that can enrich `ctx` before the terminal runs. Installing it is the opt-in, so generated output is identical whether or not it is used, and it composes with hook ordering, `SkipHooks`, and panic recovery for free. |
| **D3** | **The hook resolves the tenant and stashes it on `ctx` via `tenancy.WithResolvedTenant`.** | Hooks run before the terminal, so `m.Tenant` is not yet set. The terminal's `resolveTenant` checks `tenancy.CachedTenant` **first**, so stashing preserves §29.6 single-resolve — the user's resolver is still invoked exactly once per operation. |
| **D4** | **The probe runs on `database.Conn(ctx, fallback)`.** | That is the same accessor every generated mutation uses, so under `WithTx` the probe and the write share one transaction and the TOCTOU window closes without a codegen decision. `FOR SHARE` can close it completely as a runtime option. |
| **D5** | **Batch shapes are handled in the hook, not the generator.** | `MutationContext.Input` is documented (hook.go:65–81) as the **batch** view for `CreateMany` / `UpdateMany`, so one `collect` function covers single-op and batch alike, deduping `(table, key)` across the batch. |
| **D6** | **Skip verification when there is no tenant to verify against** — `SkipTenancy`, or `required:false` with a zero resolver. | Consistent with every other tenancy guard. The cross-tenant write path stays deliberately reachable. |

## 4. The design

### 4.1 Runtime contract (new, in `tenancy/`)

```go
// Reference is one foreign-key value that must resolve inside the active tenant.
type Reference struct {
	Column        string // input column that supplied it, for diagnostics
	Schema, Table string // the referenced table
	KeyColumn     string // the referenced key column, usually "id"
	KeyValue      any
	TenantColumn  string // the referenced table's tenant column
}

// ReferenceScoped is implemented by generated input types that carry at least
// one foreign key into a tenanted table. Input types without one do not
// implement it, so the hook's type assertion fails fast and costs nothing.
type ReferenceScoped interface {
	TenantScopedReferences() []Reference
}

var ErrCrossTenantReference = errors.New("tenancy: reference resolves outside the active tenant")
```

### 4.2 Generated per input type

Emitted only for tables with ≥1 FK into a tenanted table, alongside the input struct — never inside
a mutation body:

```go
// TenantScopedReferences reports the foreign keys this input sets whose target
// table is tenanted (PRD §29.13). reporter_id is absent: users is a shared
// table, so there is no tenant to verify it against.
func (in *CreateTaskInput) TenantScopedReferences() []tenancy.Reference {
	refs := []tenancy.Reference{
		// Required FK — plain T on CreateInput, always present.
		{Column: "project_id", Schema: "public", Table: "projects",
			KeyColumn: "id", KeyValue: in.ProjectID, TenantColumn: "workspace_id"},
	}
	// Nullable FK — omittable + a Null wrapper, so two guards.
	if v, ok := in.ParentTaskID.Get(); ok && v.Valid {
		refs = append(refs, tenancy.Reference{Column: "parent_task_id", Schema: "public",
			Table: "tasks", KeyColumn: "id", KeyValue: v.UUID, TenantColumn: "workspace_id"})
	}
	if v, ok := in.CycleID.Get(); ok && v.Valid {
		refs = append(refs, tenancy.Reference{Column: "cycle_id", Schema: "public",
			Table: "cycles", KeyColumn: "id", KeyValue: v.UUID, TenantColumn: "workspace_id"})
	}
	return refs
}
```

Three clause shapes fall out of the existing input typing and must all be generated: a **required**
FK is a plain `T` on the create input but `omittable.Value[T]` on the update input, and a
**nullable** FK is `omittable.Value[uuid.NullUUID]` on both (per the project's null-type rule), so it
needs the `.Valid` guard — clearing a nullable FK has nothing to verify.

**Codegen inputs.** `ColumnContext.FKReference{Table, Schema, Column}` names the target; the tenancy
map says whether that target is tenanted and on which column. `annotateO2OChildTenancy` is the
direct precedent — it already walks a table's related tables, consults the tenancy map, and
annotates the parent context with which targets are tenanted plus their tenant column names,
including the awkward untenanted-parent/tenanted-child case. A parallel `annotateTenantedFKTargets`
pass is the same shape.

### 4.3 The hook

```go
func VerifyReferences[T comparable](q database.Querier, d sql.Dialect, resolve TenantResolver[T]) hook.MutationHook {
	return func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			refs := collect(m.Input) // single-op AND batch shapes, deduped by (table, key)
			if len(refs) == 0 {
				return next(ctx, m)
			}
			t, apply, err := resolveOnce[T](ctx, resolve)
			if err != nil {
				return nil, err
			}
			if !apply {
				return next(ctx, m) // D6: no tenant to verify against
			}
			ctx = WithResolvedTenant(ctx, t, apply) // D3: terminal reuses this
			if err := probe(ctx, database.Conn(ctx, q), d, refs, t); err != nil {
				return nil, err
			}
			return next(ctx, m)
		}
	}
}
```

### 4.4 The probe

Two shapes, chosen by size — both live in runtime code, so the choice is a runtime branch rather
than a second generated path:

**Single-row** — one round-trip, one row, N boolean columns; column *i* corresponds to `refs[i]`, so
the failing column can be named:

```sql
SELECT
  EXISTS(SELECT 1 FROM "public"."projects" WHERE "id" = $1 AND "workspace_id" = $2),
  EXISTS(SELECT 1 FROM "public"."cycles"   WHERE "id" = $3 AND "workspace_id" = $4)
```

**Batch** — grouped by target table, values deduped, returning the *valid* keys; anything requested
and not returned is cross-tenant:

```sql
SELECT "id" FROM "public"."projects" WHERE "workspace_id" = $1 AND "id" IN ($2, $3, $4)
```

Probe count is then bounded by how many distinct tenanted tables the mutating table references —
three for `tasks` — not by batch size. A 200-row `CreateMany` pointing at three projects issues one
three-element `IN`.

A single `UNION ALL` across target tables is deliberately **not** used: key types differ (`uuid` for
one target, `bigint` for another), so the union would need a dialect-specific cast to typecheck.

### 4.5 Wiring

```go
client := database.New(dbpgx.New(pool),
    database.WithTenantResolver(tenancy.Resolver()),
    database.WithMutationHook(sqltenancy.VerifyReferences(pool, dialect, tenancy.Resolver())),
)
```

Errors map to `FORBIDDEN` through the existing API error mapper, alongside `ErrMismatch`.

## 5. What this does not cover

Stated plainly, because a security mechanism that is believed to cover more than it does is worse
than none:

- **`Increment` on an integer FK.** The target value is not known before the write, so no pre-check
  helps — the destination row is whatever sits at `current + delta`. The answer was to stop
  generating increment operators for FK columns at all, since incrementing a foreign key is
  arithmetic on an identity and a bug independent of tenancy. **Done** — `buildIncrementColumns` now
  excludes FK columns alongside PKs (PRD §8.2 eligibility). It is called out here because this
  design cannot cover it, not because it remains open.
- **FKs into shared (untenanted) tables.** `tasks.reporter_id → users` gets no rule, because `users`
  has no tenant. Constraining who may be named as reporter is membership policy.
- **`SkipTenancy`, `CallOptions.Tenant`-less paths, and raw SQL** — bypassed by design (D6).
- **A consumer who never installs the hook** gets nothing. This is the cost of D2's install-time
  opt-in, and the argument for pairing it with a generate-time diagnostic that reports every
  tenanted→tenanted FK lacking a composite-FK guard.

## 6. Demonstrating it in an example

The mechanism is invisible in a schema without a tenanted→tenanted FK, and no example has one
today — the same blind spot that hid the update-input defect until a consumer regen surfaced it.

Add to the `graphql` example (whose tenant type is already `uuid.UUID`, satisfying §29.2.4):

```sql
-- workspace_folders: tenanted, referenced by workspace_notes. The pair is the
-- example's tenanted -> tenanted FK, and the only shape in which the reference
-- hook has anything to verify.
CREATE TABLE workspace_folders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL
);

ALTER TABLE workspace_notes ADD COLUMN folder_id UUID REFERENCES workspace_folders(id);
```

The example then shows both states from one schema, the way `newHeaderTenantedHandler` already
isolates tenancy-driven requests:

| Handler | `createWorkspaceNote(folderID: <folder in tenant B>)` under header A |
|---|---|
| default test server (hook not installed) | succeeds — a cross-tenant reference is written |
| handler with `VerifyReferences` installed | `ErrCrossTenantReference` → `FORBIDDEN`, no row written |

That contrast in a single example is the clearest statement of what the feature buys, and it doubles
as the regression test. Coverage should also pin: a nullable FK left unset issues no probe; a
`CreateMany` batch pointing at one folder issues one probe; and `SkipTenancy` bypasses the check.

## 7. Blast radius

- **Runtime:** new `tenancy/references.go` — `Reference`, `ReferenceScoped`, `ErrCrossTenantReference`,
  `VerifyReferences`, `collect`, `probe`. Imports `sql` and `database`; no external dependency.
- **Generator:** one `annotateTenantedFKTargets` pass and one new emitted method per qualifying
  input type. **No mutation template changes**, so tables without a tenanted→tenanted FK are
  byte-identical.
- **Golden churn:** limited to examples that gain the fixture in §6. Existing examples have no
  qualifying FK, so they do not move.
- **Consumers:** additive. Nothing changes until the hook is installed.
- **PRD:** a new §29.13 documenting the rule, the descriptor contract, the hook, and §5's
  non-coverage.

## 8. Rejected / deferred

- **Inline generation into the mutation templates** — rejected (§2).
- **Reflection over struct tags** to extract FK values generically, avoiding the generated method —
  rejected. The generated mutation bodies advertise "explicit per-field checks, no reflection," and
  a reflective hot path in every mutation contradicts that for no benefit; the generated method is
  a dozen mechanical lines.
- **Making the hook mandatory / always-on** — rejected: it costs a round-trip, and a composite FK
  makes it redundant. Consumers who can migrate should, and should not pay for this.
- **Extending the same seam to general input validation** — **not needed as a sqlgen feature.**
  Generated input types live in the consumer's own package, so a consumer can add
  `func (in *CreateTaskInput) Validate() error` in a hand-written file in that package and install a
  matching `Validatable` hook today. References are different only because the FK graph is knowledge
  the consumer would otherwise hand-maintain across every table. This is worth documenting as a
  pattern, not building.
- **Verifying on read** (re-checking that a loaded relationship is in-tenant) — deferred. It is the
  same predicate on the other side, but the fix belongs at write time; a row whose FK already points
  cross-tenant is corrupt data, and hiding it on read masks the corruption rather than preventing it.
