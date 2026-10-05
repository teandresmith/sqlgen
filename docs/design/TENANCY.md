# SQLGen Tenancy — Design Supplement

> **Public contract (what consumers read):** see `docs/PRD.md` §29.
>
> **This document** is the design supplement — full open-question record (§7),
> implementation task breakdown (§8), and detailed generated-code references
> (§10). The PRD carries the normative spec; this file carries the rationale
> and codegen detail. Same pattern `docs/design/CACHE.md` / PRD §27 follows.
>
> **Scope:** SQLGen's tenancy primitive handles **mandatory tenant scoping** at
> the SQL layer — every generated read and mutation on a tenanted table is
> structurally incapable of crossing tenant boundaries. It does NOT try to be a
> policy/authorization engine. Fine-grained "can user X read resource Y" logic
> belongs in a policy tool (Cerbos, OPA, custom middleware) layered on top.

---

## 1. Overview

See PRD §29.1 for the normative overview (scope, design parallels to soft delete / type overrides / `CallOptions.SkipXxx`, flat-v1 vs hierarchical-v2 posture). The remainder of this document carries implementation rationale and codegen detail only.

---

## 2. Configuration

See PRD §29.2 for the normative spec: `tenancy:` block fields (enabled, column, required, type), `TableTenancyConfig` tri-state overrides, detection rules, type-resolution ordering, and validation (non-nullable, `comparable`, uniform tenant type across tenanted tables, SQL-vs-Go type check).

**Implementation notes beyond the PRD:**

- The detection pass runs during schema resolution, not config parsing — the effective column lookup needs the parsed column set per table.
- Lean on detection in the common case: if `overrides.types.uuid` already resolves `uuid` → `uuid.UUID`, no tenancy-specific `type:` block is needed. Reserve explicit `tenancy.type` for wrapper types (`WorkspaceID` around `uuid.UUID`) or when detection is ambiguous.

---

## 3. Runtime Integration

See PRD §29.3 for the normative spec: `TenantResolver[T comparable]` alias, `ErrMissing` / `ErrMismatch` errors, client wiring via `WithTenantResolver`, and the "type-safe by construction" argument.

**Implementation notes beyond the PRD:**

- The runtime subpackage lives at `github.com/teandresmith/sqlgen/tenancy` (parallels existing `cache/`, `event/`, `hook/`). stdlib-only; no imports from parser, CLI, sql, hook, event, cache.
- The generator picks `T` at codegen time from the resolved tenant column Go type (PRD §29.2.4 validation pass) and emits a concrete `WithTenantResolver(r tenancy.TenantResolver[<T>]) ClientOption`. The `Client` struct holds a concrete field — no generics threaded through hooks, options, or entity clients.
- **Mixed-tenant-type hard-error message.** When a schema has divergent tenant types, the generator emits:
  ```
  sqlgen: tenant column types differ across tenanted tables:
    users.workspace_id     → uuid.UUID
    legacy_widgets.tenant_id → int64
  fix: either unify the column types, set `tables.<name>.tenancy.enabled: false`
  on the odd one out, or consolidate via migration.
  ```
  Truly mixed types are rare (~1–2% of schemas, usually mid-migration). Multi-resolver machinery (per-type `WithTenantResolver<Type>` options, per-table resolver dispatch at the hook layer) is not worth building until three real users have a persistent mixed-type schema and can't consolidate. See §7 Q-deferred list.
- **Wrapper types and aliases.** `type WorkspaceID uuid.UUID` (named type) used uniformly across every tenanted table is a single tenant type — no mixed-type issue. `type WorkspaceID = uuid.UUID` (alias) is literally the same type as `uuid.UUID`; also fine. Only true type divergence triggers the hard error.

---

## 4. Generated Behavior

See PRD §29.4 for the normative spec: read-side WHERE augmentation, mutation-side unified mismatch rule (Q1 resolution), `SkipTenancy` semantics per op-class, raw-op bypass, three properties the design preserves.

---

## 5. Interactions with other features

See PRD §29.5 through §29.10 for the normative interaction specs (cache key grammar, event metadata, composite PKs, soft-delete composition, hooks orthogonality, relationship propagation). Implementation rationale for each decision lives in §7 (open-question resolutions).

### 5.1 Cache — key grammar (§7 Q5 resolution)

See PRD §29.5 and §27.5 (extended).

### 5.2 Events — metadata

See PRD §29.6.

### 5.3 Composite primary keys

See PRD §29.7 for the normative spec: the `XXXPK` struct carries every DDL PK column (tenant included) and the runtime applies the §29.4.2 verify-match rule uniformly on PK-bearing operations (`Get` / `Update` / `HardDelete` / `Increment` / `Exists`).

**Implementation notes beyond the PRD:**

- Verify-match was chosen over "omit-the-tenant-from-the-constructor-signature." An omit-from-signature shape would force the `XXXPK` struct to diverge from the DDL PK column set, breaking round-trip patterns (`Get` → pass the returned PK back to a later operation), complicating cache-key construction (the key includes tenant; the struct wouldn't), and making `SkipTenancy`-driven cross-tenant operations awkward (the caller has nowhere to supply the tenant PK component). The compile-time "can't-construct-wrong-tenant" guarantee of an omit-from-signature shape was never airtight either — deserialization, struct copies, and the resolver itself remain runtime.
- The zero value of `T` on the PK surfaces as `ErrMismatch` in the common case — the resolver almost never returns the zero value under `required: true`. This turns "forgot to populate the tenant on `XXXPK`" into a loud, early error with no DB round-trip, matching the behavior of forgetting a required-tenant ctx value. The rare case where a system genuinely uses the zero value as a valid tenant still works via the match branch.
- The generator treats the tenant column identically on both PK construction and mutation-input construction — a single verify-match helper feeds both paths so the semantics stay aligned if the rule ever evolves.

### 5.4 Soft delete

See PRD §29.8.

### 5.5 Hooks

See PRD §29.9 and section 21 (Hooks & Middleware).

---

## 6. v1 Constraints

See PRD §29.11 for the full list of non-goals and v1 workarounds (hierarchical/multi-level, mixed-types, cross-tenant tx, dynamic columns, fine-grained authz, type-aware lint). Rationale for each decision is preserved in §7 below (Q6 for hierarchy, Q7 for lint, etc.).

---

## 7. Open Questions (RESOLVED — retained as decision record)

### Q1. Caller-set-tenant policy on mutations — **RESOLVED**

If a caller passes `UpdateInput{WorkspaceID: someValue, ...}` (or similarly sets the tenant field on a `CreateInput` / `UpsertInput`), what happens?

- **Decision:** **mismatch-comparison policy** — the library compares the caller-supplied tenant to the resolver's value.
  - **Match:** accepted as redundant. The operation proceeds exactly as if the field were unset; generated SQL is byte-identical.
  - **Mismatch:** `ErrTenantMismatch` before the DB round-trip. Caller must correct the input or pass `CallOptions.SkipTenancy: true` to perform the cross-tenant write intentionally.
- **Rationale:** the only case that's genuinely ambiguous is a mismatch — match-on-set produces the same DB-visible outcome as not-set, so rejecting it punishes defensive and round-trip patterns without improving safety. The safety property the library cares about — "no silent cross-tenant write" — is preserved entirely by the mismatch branch. The match branch is a no-op by construction.
- **Why not reject any caller-set tenant (the stricter alternative considered):** that policy punishes the common defensive pattern where a developer copies `Get` results into an `UpdateInput` with all fields populated, including the unchanged tenant. Under strict-reject, every such code path would error with a misleading "use `SkipTenancy` to override" message — when the right fix is simply to leave the field alone. Tolerating the match case keeps the boundary tight (mismatch still errors) while being friendly to the most common misuse.
- **Why not strip the tenant field from input structs entirely:** round-trip symmetry (`Get` → edit → `Update` on the same struct shape) is too useful to break, and the escape-hatch path (`SkipTenancy: true` + cross-tenant write) needs a way to actually express the new tenant. Field stays; presence + value check gates it.
- **Runtime cost:** one `==` per mutation with a Present tenant field, and `T comparable` (PRD §29.3.1) makes it single-instruction. Negligible against the DB round-trip that follows.

### Q2. `required: false` — should it exist at all? — **RESOLVED**

The default is `required: true`. Setting `required: false` means a missing tenant in ctx silently skips the filter — effectively a cross-tenant query. That's the exact footgun tenancy is meant to eliminate, so the knob's existence deserves an explicit decision.

- **Decision:** **keep the knob.** The library enforces a safe default (`required: true` — fail-closed on missing tenant); strictness beyond that is the user's call. Some projects have legitimate mixed-tenancy access patterns (admin bootstrap, tenant-provisioning flows, shared reference-data ingestion) where per-table `required: false` is cleaner than sprinkling `SkipTenancy: true` across every call site. Explicit in YAML is better than implicit in code.
- **Rejected alternative:** remove the knob and force `SkipTenancy: true` on every cross-tenant call. Pros: one fewer config dimension. Cons: demotes a table-level design decision ("this table participates in tenancy differently") to a per-call-site decision, which is noisier and harder to audit at the schema level. YAML is the better home for the policy.
- **Operator guidance (for the docs):** `required: false` is a sharp tool. Use it when the *semantics* of a particular table or the whole deployment genuinely call for missing-tenant-means-cross-tenant behavior. Do not flip it to silence resolver bugs. When in doubt, leave `required: true` and reach for `SkipTenancy: true` at the call site.

### Q3. Relationship propagation — **RESOLVED**

How does the tenant filter propagate across relationships (§13)? Depends on the relationship shape, and on whether the child is itself tenanted.

**Rule:** each table's tenancy is evaluated independently. A tenant filter is applied to whichever side has a tenant column; the other side is scoped transitively (parent's filter + FK chain). No hard error for mixed tenancy — un-tenanted children under tenanted parents is a common, legitimate schema pattern (e.g., `orders.workspace_id` + `orders.id` with `order_line_items.order_id` but no `order_line_items.workspace_id` because line items are only queried alongside their parent order).

**Per-shape mechanics:**

| Shape | How the child loads | Tenancy behavior |
|---|---|---|
| **o2o** | Inline JOIN in the parent `Get` / `GetMany` SQL | JOIN generator adds `AND {childAlias}.{tenantCol} = $N` **only when the child table is tenanted**. Tenant column name is resolved from the child's per-table config. When the child is NOT tenanted, no child-side filter is added — the parent's WHERE tenant filter + the ON condition over the parent PK is the scoping path. |
| **o2m** | Chained `client.Children.GetMany(ctx, input)` | Free — the child's standalone `GetMany` already auto-filters by its own tenant column (when the child is tenanted). Non-tenanted child = no child filter; transitive scoping via the parent-PK `IN (...)` predicate over FKs is the guarantee. |
| **m2m** | Chained junction-read + child `GetMany` | Free for the same reason as o2m. Junction tables are usually non-tenanted by design; the child itself auto-filters if tenanted, transitively if not. |

**Key properties:**

- **No codegen hard error on tenanted-parent-to-non-tenanted-child.** This is an intentional schema pattern (nested sub-entities that only make sense inside their parent), not a misconfiguration.
- **Belt-and-suspenders when both sides are tenanted.** The JOIN generator emits `parent.tenant = $N AND child.tenant = $N` in o2o. If both sides share the same resolver value (always true under `T comparable` + uniform tenant type), this catches corrupted rows where a child's `tenant_id` drifts from its parent's due to bad data or a broken migration.
- **Per-table tenant column name suffices.** The child's own `tables.<child>.tenancy.column` (or the global default) determines the column used on the child side. No per-relationship override needed in v1 — different-column-per-JOIN-side is a theoretical edge case that hasn't surfaced in real schemas.
- **`SkipTenancy: true` propagates.** A parent `Get` called with `SkipTenancy: true` drops the auto-filter on both parent and child sides — the admin/reporting caller owns full row scoping.

**Rejected alternative:** per-relationship opt-out config (e.g., `relationships.<name>.tenancy.enabled: false`). Not worth building in v1 — the per-table tenancy flag plus `SkipTenancy` at the call site already covers the space. If a schema genuinely needs "this specific JOIN should never filter by tenant even though both tables are tenanted," that's exotic enough to earn its own feature request later.

### Q4. `SkipTenancy` and soft delete composition — **RESOLVED**

`SkipTenancy: true` + `IncludeDeleted: true` → "give me all rows including soft-deleted across all tenants." Admin tooling needs this shape; most app code does not.

- **Decision:** **both flags compose orthogonally. No special casing, no artificial combined-flag guard.** `SkipTenancy` toggles the tenant filter; `IncludeDeleted` toggles the soft-delete filter. Each is independent in intent and independent in effect.
- **Rationale:** the library sets safe defaults (tenant auto-scope on, soft-deleted rows hidden) and provides explicit, named opt-outs for each. How a caller chooses to combine those opt-outs is their call — the library should not make value judgments about which escape-hatch combinations are "allowed." Disallowing `SkipTenancy: true && IncludeDeleted: true` would complicate the API for a scenario (admin / audit tooling) that genuinely needs both, and users who want one without the other can already express that.
- **Operator guidance (doc-surface, not code-surface):** projects that want to restrict who can call `SkipTenancy` (e.g., only admin-role handlers) implement the guard at their own boundary — typically a thin wrapper that checks an auth-role ctx marker before passing `CallOptions{SkipTenancy: true}` down. The library does not ship a role mechanism and does not gate escape hatches by role; that's an app-layer concern. Same pattern applies to any `SkipXxx`-shaped flag: the library gives you the knob, your middleware decides who can turn it.

### Q5. Cache key grammar when PKs are globally unique — **RESOLVED**

If every tenant's row PKs are globally unique (UUIDs typically), the `tenant:{tenant}` cache segment is redundant — collision is impossible. But detection of "globally unique PK shape" at codegen is ambiguous (the schema says `uuid` but the application's uniqueness guarantees are external).

- **Decision:** **always emit the `tenant:` segment for tenanted tables in v1, no knob.** Ships the safe default; add the knob if a real user asks.
- **Rationale:** the library can't reliably detect at codegen whether PKs are globally unique — `uuid` columns usually are, but applications can generate integer or string PKs scoped per-tenant. Defaulting to "tenant segment always present" makes the cache correctness-by-construction against schema shapes we can't introspect. Redundant key bytes for UUID-PK schemas (~40 extra bytes per entry) is negligible against the per-key value payload; labeled-segment grammar (PRD §27.5) makes the extra segment self-describing, not a positional magic string.
- **Rejected alternative:** config opt-out `tenancy.cache.include_in_key: false` for projects with verified globally-unique PKs. Adds a knob the library can't safely default on the user's behalf and that saves bytes in an already-negligible place. Not worth the config-surface cost unless three real users request it.

### Q6. Hierarchical / multi-level tenancy — **RESOLVED (deferred to v2)**

Real-world multi-tenant products often have more than one scoping axis: an org contains workspaces, workspaces contain projects; a user belongs to multiple workspaces; super-admins read across all tenants. "Tenant hierarchy" covers several distinct patterns, and they don't collapse to one design:

| Pattern | Shape | Example |
|---|---|---|
| **Nested tenants** | Multiple scoping columns on every table; every level filters. Some legitimate queries span a level. | `org_id` + `workspace_id`; org-admin reads across all workspaces in their org. |
| **Flat + admin escalation** | One column per table; specific requests (super-admin, audit) need cross-tenant reads. | `workspace_id`; audit handler reads every workspace. |
| **Group membership (M:N)** | User belongs to multiple tenants; scope is "any tenant I belong to," not `=`. | User in many workspaces; feed query returns rows from all of them. |
| **Implicit FK hierarchy** | Schema has the tree (`products.workspace_id → workspaces.org_id`); scoping is still per-table-per-level. | Join to `workspaces` to filter by `org_id`; not really tenancy, just schema. |

- **Decision:** **flat / single-level tenancy in v1. Hierarchical tenancy is deferred to a potential v2.** One tenant column per table, one resolved tenant per request. Each hierarchy pattern above has a usable v1 workaround:
  - **Nested tenants** → scope to the finest-grained level (workspace) and let the resolver pick the active workspace from ctx. Org admins use the same resolver path; they just pick a different workspace per request (the usual "switch workspace" UI pattern).
  - **Flat + admin escalation** → `CallOptions.SkipTenancy: true` + a hand-filtered `WHERE workspace_id IN (...)`. Explicit, auditable, loud in code review.
  - **Group membership** → handle at the resolver or with a per-request scoped-client wrapper; or `SkipTenancy` + `IN`. Rare enough that manual is acceptable.
  - **Implicit FK hierarchy** → not a tenancy concern; schema joins handle it.

- **Rationale:** the four patterns diverge enough that one primitive can't serve all three active cases (nested, admin, group). Fitting them all means config bloat (levels, optional levels, required-per-level flags), multi-field resolvers, cache key doubling, error-mode multiplication — and most projects don't need any of it. Shipping flat-only keeps the primitive tight and leaves concrete demand to shape v2.

- **Migration path is clean — v1 does not box in v2.** If hierarchy ever lands:
  - **Config** — `tenancy.column` stays meaningful; a future `tenancy.levels: [org_id, workspace_id]` is an additive superset. Existing configs keep working.
  - **Cache key grammar** — labeled segments mean `tenant:{t}:fingerprint:...` extends to `org:{o}:workspace:{w}:fingerprint:...` as an additive grammar change, no breaking migration.
  - **Resolver type alias** — `TenantResolver[T comparable]` generalizes to `MultiTenantResolver[Levels ...]` or a struct-return shape; the flat alias stays for single-level schemas.
  - **SQL builder hooks** — filter injection is already per-column; extending to N columns is mechanical.

- **Revisit criterion:** three or more real users ask for nested tenancy and have schemas that won't consolidate to flat.

### Q7. Type-aware lint extension — **RESOLVED**

The cache lint (§27 CLI §19A) already plans type-aware scanning. A tenancy lint rule could check that handler code sets the tenant on ctx before calling tenanted-table methods — but "ctx has the tenant" is not a type-resolvable predicate (it's a runtime value question).

- **Decision:** **no tenancy-specific lint rule, in v1 or v2.** Runtime `ErrMissingTenant` already surfaces forgotten-tenant cases loudly; a missing tenant fails the first tenanted op with a clear, actionable error. No static analysis can do materially better without expensive data-flow tracking that chases ctx values across function boundaries.
- **Rationale:** "did the caller put a tenant on ctx before calling `client.X`?" is inherently a runtime-value question, not a type question. AST-only analysis can't see through `auth.WithTenant(ctx, ...)` calls in middleware layers. Type-aware analysis via `golang.org/x/tools/go/packages` buys some tracing but still can't resolve "is `ctx` the same ctx the middleware wrote to?" in realistic code. The payoff is small; the `sqlgen lint` dependency and maintenance cost is not. `ErrMissingTenant` at the first call is a strictly better signal than a speculative lint warning.
- **No v2 escalation path planned.** Unlike Q6, this isn't "deferred until demand" — it's "we shouldn't build it." If the cache lint (§19A stretch) ever ships, reuse its `packages.Load` infrastructure for cache call-site rules; don't layer tenancy on top.

---

## 8. Phase 13 Task Breakdown

See `docs/tracker/IMPLEMENTATION_ORDER.md` Phase 13 (§13.1 through §13.11) for the normative task breakdown, per-sub-item file list, test coverage, and dependency graph. STATUS.md tracks current state.

---

## 9. YAML Examples

See PRD §29.12 for minimum-config, opt-outs, and wrapper-type examples.

---

## 10. Generated Code References

Concrete snippets showing the tenancy delta against the baseline generator. The `products` table is used throughout as the example (`workspace_id uuid` tenant column, `string`-typed PK in the sample schema — UUIDs are stored as `string` in the generated Go today; `uuid.UUID` is the common alternative). Assume:

- `cache.enabled: false` for most snippets (cache interactions called out in §10.7)
- `events.enabled: false` unless noted
- `generation.package: store` (generated package name)
- Runtime module: `github.com/teandresmith/sqlgen`

**Column name is configurable.** Throughout §10, `workspace_id` / `WorkspaceID` are placeholders for whatever the project sets as `tenancy.column` (PRD §29.2) — `tenant_id` / `TenantID`, `org_id` / `OrgID`, `organization_id` / `OrganizationID`, etc. The generator substitutes the configured name (lowercase SQL column, Go-idiomatic PascalCase field on `*Input` structs) everywhere these snippets show the example value.

Tenancy-specific logic is flagged with `// [tenancy]` comments so reviewers can quickly audit the delta against a non-tenanted baseline.

### 10.1 Runtime Package Surface

The runtime module is split across `database/`, `sql/`, `hook/`, `omittable/`, `event/`, `cache/`, etc. — no root-level `sqlgen` package. Tenancy adds a new subpackage `github.com/teandresmith/sqlgen/tenancy`:

```go
// tenancy/tenancy.go
package tenancy

import (
    "context"
    "errors"
)

// TenantResolver extracts the active tenant from a request context. T is
// pinned at codegen time from the resolved tenant column Go type, and
// constrained to `comparable` so mismatch checks (PRD §29.4.2) use `==` directly.
type TenantResolver[T comparable] func(ctx context.Context) (T, error)

var (
    ErrMissing  = errors.New("tenancy: tenant missing from context")
    ErrMismatch = errors.New("tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override")
)
```

Per-project callers import this as `tenancy.TenantResolver[T]` / `tenancy.ErrMissing` / `tenancy.ErrMismatch`.

**Generated `CallOptions[FO]` gains one field** — the struct is generated per-project in `shared_types_gen.go`, not a runtime import, so the addition is a template change under `cmd/sqlgen/gen/templates/table/`:

```go
// Generated in shared_types_gen.go.
type CallOptions[FO any] struct {
    SkipCache    bool `json:"skip_cache"`
    SkipEvents   bool `json:"skip_events"`
    SkipHooks    bool `json:"skip_hooks"`
    SkipTenancy  bool `json:"skip_tenancy"`  // [tenancy]
    FieldOptions *FO  `json:"field_options"`
}
```

`resolveCallOptions` already folds `SkipHooks` into `SkipCache + SkipEvents`. `SkipTenancy` is **not** folded in — tenancy runs in the SQL builders, not as a hook, so it is orthogonal to `SkipHooks` (PRD §29.4.4).

### 10.2 Generated Client Additions

The existing `Client` struct holds a `querier database.Querier` and an unexported `opts clientOptions`. Tenancy threads a resolver through both.

```go
// client_gen.go — additions marked [tenancy]. Existing shape preserved.
package store

import (
    "github.com/google/uuid"
    "github.com/teandresmith/sqlgen/database"
    "github.com/teandresmith/sqlgen/hook"
    "github.com/teandresmith/sqlgen/tenancy" // [tenancy]
)

type clientOptions struct {
    callbackMode  database.CallbackMode
    panicHandler  func(ctx context.Context, r any, table hook.TableName, op string) error
    mutationHooks []hook.MutationHook
    queryHooks    []hook.QueryHook

    // [tenancy] Concrete T — pinned at codegen to the resolved tenant column Go type.
    tenantResolver tenancy.TenantResolver[uuid.UUID]
}

// [tenancy] Emitted with a concrete T so callers see a type-safe signature
// without generic instantiation. Uniform tenant type across tenanted tables
// is enforced at codegen (PRD §29.2.4); mixed-type schemas are a hard error.
func WithTenantResolver(r tenancy.TenantResolver[uuid.UUID]) ClientOption {
    return func(o *clientOptions) { o.tenantResolver = r }
}
```

Each per-entity client constructor (`newProductClient`, etc.) currently takes `(querier, mutationHooks, queryHooks, panicHandler)`. The tenancy extension adds the resolver as a trailing parameter for tenanted tables:

```go
// [tenancy] Signature extension — unchanged for non-tenanted tables.
c.product = newProductClient(querier, options.mutationHooks, options.queryHooks,
    options.panicHandler, options.tenantResolver)
```

**Non-tenanted projects** (global `tenancy.enabled: false` or no tenant columns detected) generate neither the `tenantResolver` field nor `WithTenantResolver` — zero API surface change.

### 10.3 Read Operations — `Get` and `GetMany`

Generated `Get` is a thin wrapper that delegates to `GetMany` with a 1-row filter. The tenancy delta therefore lives in `GetMany`'s condition-building block — `Get` itself does not change.

```go
// [tenancy] delta in productClient.GetMany.
// Existing condition building:
var conds []sql.Condition
if input.Filter != nil {
    conds = input.Filter.ToConditions(c.dialect)
}
conds = append(conds, input.conditions...)

// [tenancy] Append tenant filter after user/internal predicates so it always AND's.
if !options.SkipTenancy {
    workspaceID, err := c.tenantResolver(ctx)
    if err != nil {
        return nil, fmt.Errorf("get products: resolve tenant: %w", err)
    }
    conds = append(conds, sql.Where("workspace_id").Eq(workspaceID))
}

// Unchanged — feeds the same sql.BuildSelect call:
query, args := sql.BuildSelect(c.dialect, c.table, sql.SelectOptions{
    Columns:    columns,
    Conditions: conds,
    OrderBy:    input.Sorts,
    Limit:      limitPtr,
    Offset:     offsetPtr,
})
```

Error wrapping follows the project convention `"{op} {table-plural-or-singular}: %w"` — `GetMany` uses plural (`"get products: %w"`); `Get` would use singular if it resolved its own SQL, but it delegates, so the wrap happens once in `GetMany`.

### 10.4 Write Operations — `Create`, `Update`, `Upsert`

**`Create` — tenant column is injected into the generated `columns` / `args` lists** (baseline builds `columns := []string{...}; args := []any{...}` and extends them per-column via `input.X.Get()` checks):

```go
// [tenancy] delta inside productClient.Create, before the insertAndResolveID call.
var workspaceID string // [tenancy] concrete T at codegen
if !options.SkipTenancy {
    resolved, err := c.tenantResolver(ctx)
    if err != nil {
        return nil, fmt.Errorf("create product: resolve tenant: %w", err)
    }
    // [tenancy] Mismatch check (Q1): caller-set tenant must match the resolver.
    if v, ok := input.WorkspaceID.Get(); ok && v != resolved {
        return nil, tenancy.ErrMismatch
    }
    workspaceID = resolved

    // [tenancy] Auto-set the tenant column regardless of whether the caller
    // populated the field — match-case is a no-op, unset-case fills it.
    columns = append(columns, "workspace_id")
    args = append(args, workspaceID)
} else {
    // [tenancy] SkipTenancy: caller is responsible for populating the column.
    // If they didn't, the DB's NOT NULL constraint surfaces the error.
    if v, ok := input.WorkspaceID.Get(); ok {
        columns = append(columns, "workspace_id")
        args = append(args, v)
    }
}

id, err := c.insertAndResolveID(ctx, conn, columns, args)
// ... existing codegen continues unchanged ...
```

`CreateMany` applies the same check inside its per-row loop so a mismatch in any row errors the batch before any DB write.

**`Update` — tenant becomes a WHERE condition; SET clause excludes the tenant column** under normal operation:

```go
// [tenancy] delta inside productClient.Update.
var workspaceID string
if !options.SkipTenancy {
    resolved, err := c.tenantResolver(ctx)
    if err != nil {
        return nil, fmt.Errorf("update product: resolve tenant: %w", err)
    }
    if v, ok := input.WorkspaceID.Get(); ok && v != resolved {
        return nil, tenancy.ErrMismatch
    }
    workspaceID = resolved
}

// Existing SET-clause building (per-field input.X.Get() checks). Tenant column
// is INTENTIONALLY not added to setClauses under normal operation — a row's
// tenant is not a mutable attribute.

// [tenancy] Under SkipTenancy:true the caller may be moving the row to a
// different tenant; admit the column into SET in that case.
if options.SkipTenancy {
    if v, ok := input.WorkspaceID.Get(); ok {
        setClauses["workspace_id"] = v
    }
}

conds := []sql.Condition{sql.Where("id").Eq(id)}
if !options.SkipTenancy {
    conds = append(conds, sql.Where("workspace_id").Eq(workspaceID)) // [tenancy]
}

query, args := sql.BuildUpdate(c.dialect, c.table, sql.UpdateOptions{
    SetClauses: setClauses,
    Conditions: conds,
})
// ... existing Exec / RowsAffected / ErrNotFound flow continues unchanged ...
```

`Upsert` mirrors `Create` (auto-set + mismatch check — inserts must populate the column), with the `UPDATE` side of `ON CONFLICT` following the `Update` rule (tenant excluded from the SET column list when `SkipTenancy` is false).

### 10.5 `*Where` Operations

`UpdateWhere` / `SoftDeleteWhere` / `HardDeleteWhere` / `RestoreWhere` build conditions via `filter.ToConditions(c.dialect)` and return `ErrEmptyFilter` if the filter is empty. Tenancy appends its condition after the filter:

```go
// [tenancy] delta inside productClient.UpdateWhere.
conds := filter.ToConditions(c.dialect)
if len(conds) == 0 {
    return nil, ErrEmptyFilter
}

// ... existing SET clause building (tenant excluded — same as §10.4) ...

if !options.SkipTenancy {
    resolved, err := c.tenantResolver(ctx)
    if err != nil {
        return nil, fmt.Errorf("update products where: resolve tenant: %w", err)
    }
    if v, ok := input.WorkspaceID.Get(); ok && v != resolved {
        return nil, tenancy.ErrMismatch
    }
    conds = append(conds, sql.Where("workspace_id").Eq(resolved))
}

query, args := sql.BuildUpdate(c.dialect, c.table, sql.UpdateOptions{
    SetClauses: setClauses,
    Conditions: conds,
})
```

### 10.6 Relationship Loaders

**o2o (inline JOIN via `sql.BuildSelectJoin`).** The generator emits `sql.JoinClause` values with a pre-formatted `On` SQL string. Placeholder numbering is computed only from the outer `Conditions`, so the cleanest place to inject the child-side tenant filter is the **outer WHERE**, not the JOIN `On` — this keeps `sql.JoinClause` semantics unchanged. Belt-and-suspenders applies only when the child is itself tenanted (§7 Q3):

```go
// [tenancy] Inside the productClient read path when Profile (o2o) is selected
// and Profile is tenanted. Existing join resolution is unchanged:
joins := c.resolveO2OJoins(c.dialect, fo) // sql.JoinClause{Table, Alias, On, Columns}

conds := input.Filter.ToConditions(c.dialect)
if !options.SkipTenancy {
    workspaceID, err := c.tenantResolver(ctx)
    if err != nil {
        return nil, fmt.Errorf("get products: resolve tenant: %w", err)
    }
    // [tenancy] Parent-side tenant filter (always present on tenanted parents).
    conds = append(conds, sql.Where("p.workspace_id").Eq(workspaceID))
    // [tenancy] Belt-and-suspenders — only when the child table is tenanted.
    // Emitted per-join by the generator, omitted when the child has no tenant column.
    if profileIsTenanted {
        conds = append(conds, sql.Where("p2.workspace_id").Eq(workspaceID))
    }
}

result, resultArgs := sql.BuildSelectJoin(c.dialect, c.table, joins, sql.SelectOptions{
    Alias:      "p",
    Columns:    columns,
    Conditions: conds,
    OrderBy:    input.Sorts,
})
```

**o2m / m2m (chained `GetMany`).** `loadRelationships` invokes the child's `GetMany` directly, reusing that method's own condition-building — which, when the child is tenanted, already auto-appends the child's tenant filter. The only codegen change is threading the parent's resolved `options` (specifically `SkipTenancy`) into the child call so the escape hatch propagates:

```go
// [tenancy] loadRelationships threads options through to the child GetMany so
// SkipTenancy propagates. No new tenant filter logic here — the child's own
// GetMany handles it (§10.3).
children, err := c.orderItemClient.GetMany(ctx, &GetOrderItemsInput{
    Filter: filter,
    Sorts:  sorts,
}, func(o *CallOptions[OrderItemFieldOptions]) {
    o.SkipHooks = true
    o.SkipTenancy = parentOptions.SkipTenancy // [tenancy]
    o.FieldOptions = relFieldOptions
})
```

### 10.7 Cache Key Construction

The base cache helpers (spec in `docs/design/CACHE.md` §5.6):

```go
// cache/key.go — base helpers.
func BuildKey(prefix, schema, table, fingerprint string, pk any) string
func BuildCompositeKey(prefix, schema, table, fingerprint string, pks []any) string
func BuildTablePattern(prefix, schema, table string) string
```

Tenancy adds three parallel helpers whose signatures mirror the base shape with a `tenant any` parameter inserted before `fingerprint` — matching the labeled-segment grammar in PRD §29.5 (`tenant:` segment precedes `fingerprint:`):

```go
// cache/key.go — tenancy helpers.
// Returns {prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fingerprint}:pk:{pk}.
func BuildTenantKey(prefix, schema, table string, tenant any, fingerprint string, pk any) string

// Composite-PK variant — pks must be in DDL PK column order (same invariant as BuildCompositeKey).
func BuildTenantCompositeKey(prefix, schema, table string, tenant any, fingerprint string, pks []any) string

// Returns {prefix}:{schema}.{table}:tenant:{tenant}:* — single trailing wildcard,
// works on every pattern backend (PRD §29.5 rationale for tenant-before-fingerprint).
func BuildTenantTablePattern(prefix, schema, table string, tenant any) string
```

Generated per-table key helpers choose the right overload based on per-table tenancy:

```go
// [tenancy] Tenanted Product table.
func (c *cacheFacade) keyForProduct(tenant string, pk string) string {
    return cache.BuildTenantKey(c.prefix, "public", "products", tenant, fingerprintProducts, pk)
}

// Non-tenanted OrderItem table.
func (c *cacheFacade) keyForOrderItem(pk OrderItemPK) string {
    return cache.BuildCompositeKey(c.prefix, "public", "order_items", fingerprintOrderItems,
        []any{pk.OrderID, pk.ProductID})
}
```

The cache query/mutation hook receives the tenant value that the entity method already resolved (threaded via the hook `QueryContext` / `MutationContext`), so key construction does not re-invoke `TenantResolver`.

### 10.8 End-to-End Trace — `Update` inside a Transaction

Minimal trace of `client.Products().Update(ctx, id, input)` where tenancy, caching, and events are all enabled and the call happens inside an open transaction.

```
1. Handler opens a transaction. `db` is the user's database.Querier:
     ctx, err := database.NewTransaction(ctx, db, "update-product")
   The returned ctx carries the *database.Tx.

2. Handler: client.Products().Update(ctx, productID, &UpdateProductInput{Name: omittable.Set("x")})
   — Products() returns the ProductClient interface.

3. productClient.Update body (§10.4 delta applied):
   a. options := resolveCallOptions(opts)
      → CallOptions{SkipTenancy: false, SkipHooks: false, ...}
   b. c.executeMutation(ctx, options.SkipHooks, &hook.MutationContext{
        Op: hook.OpUpdate, Table: TableProducts, Schema: "public",
        PK: productID, Input: input, CallOptions: options,
      }, func(ctx, m) { ... })
      — wraps the closure in the mutation hook chain via hook.BuildMutationChain.
   c. Inside the closure:
        - conn := database.Conn(ctx, c.querier)
          → returns the tx from ctx via database.FromContext, else c.querier.
        - [tenancy] resolved, _ := c.tenantResolver(ctx)  → W
        - [tenancy] input.WorkspaceID.Get() returns (_, false) — no mismatch check fires.
        - setClauses built from omittable fields (tenant column excluded).
        - conds := []sql.Condition{sql.Where("id").Eq(productID),
                                    sql.Where("workspace_id").Eq(W)}  // [tenancy]
        - query, args := sql.BuildUpdate(c.dialect, c.table, sql.UpdateOptions{...})
          → UPDATE "products" SET "name" = $1, "updated_at" = $2 WHERE "id" = $3 AND "workspace_id" = $4
        - conn.Exec(ctx, query, args...) runs on the tx.
   d. Hook chain:
        - Cache hook (outermost): registers tx.OnCommit(invalidate).
        - Event hook: registers tx.OnCommit(publish) with tenant copied into
          Event.Metadata["tenant"] (Event.Metadata is map[string]string).
        - User-registered hooks run per the chain.

4. Handler calls database.Commit(ctx) — runs OnCommit callbacks in FIFO order.

5. OnCommit callbacks fire:
   a. Event callback publishes:
        Event{Table: "products", Action: "update", PK: productID,
              Metadata: map[string]string{"tenant": W}}
      — runs on context.Background() in default CallbackAsync mode;
        request-scoped ctx values must be closed over at registration time.
   b. Cache callback invalidates:
        key := cache.BuildTenantKey(prefix, "public", "products", W,
                                     fingerprintProducts, productID)
            → "sqlgen:public.products:tenant:W:fingerprint:vX:pk:productID"
        backend.InvalidateMany(ctx, [key])
```

Every tenancy interaction is visible: resolution at method entry, mismatch-check gate on the input (skipped here since the input did not carry `WorkspaceID`), filter injection in WHERE, cache key tenant scoping, event metadata tenant. `OnCommit` FIFO ordering preserves the event-before-cache guarantee that the cache design relies on.

