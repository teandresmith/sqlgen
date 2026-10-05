# Phase 23: Cache × Tenancy — Nil-Tenant Invalidation

Status: Complete (closed 2026-07-15)
PRD Sections: 27, 28, 29
Design doc: PRD §27.9/§28.11/§29.4.4/§29.5/§29.6 (SYNCED 2026-07-15 — superseded by PRD §27.9/§28.11/§29.4.4/§29.5/§29.6)

> **Dependency order (record):** 23.1 is the foundation and **blocks 23.2, 23.3, 23.4**.
> 23.4 additionally wants 23.3's event stamp (E1) in place. 23.5 (Axis A) is
> **independent** and may land last or be deferred. 23.6 (example + tests + PRD sync)
> is last. Governing decisions D1–D11 and tests T1–T11 + T7b are attributed to the
> sub-item whose behavior they gate.

---

## 23.1 Structural tenant capture — parallel `m.AffectedTenants`, precise on every shape/dialect

**PRD Reference:** Section 29.2 (uniform tenant type, NOT NULL tenant column), 29.5, 29.7. Design §5.2.1, §10-D1/D9/D10.

**Status:** Complete (2026-07-15)

**Governing decisions:** D9 (structural capture via parallel `m.AffectedTenants []any`, never off the returned entity), D10 (MySQL `*Many` batched pre-read → fallback becomes defensive-only), D1 (MySQL already pre-`SELECT`s; carrying the tenant column is ~free).

### Tasks

- [x] Add `AffectedTenants []any` to `hook.MutationContext` (`hook/hook.go`), index-aligned with `AffectedPKs`; doc-comment the alignment invariant and "populated only for tenant-∉-PK tables; nil otherwise" (additive — non-tenant paths byte-identical).
- [x] Codegen branch (per tenanted table, at capture site): **tenant ∈ PK** → derive from the PK struct (`pk.WorkspaceID`), no carrier written; **tenant ∉ PK** → write the captured tenant into `m.AffectedTenants` in lockstep with `m.AffectedPKs`.
- [x] Widen the PG/SQLite `RETURNING` projection on tenant-∉-PK ops to include the tenant column (single-row by-PK, `*Where`, `*Many`) — `RETURNING "id", "workspace_id"` (`update.go.tmpl`, `delete.go.tmpl`, create write-through path).
- [x] Widen the existing MySQL `*Where` pre-`SELECT` (`collectAffectedIDs` / composite `collectAffectedPKs`, `update.go.tmpl:769`/`:806`, `delete.go.tmpl:488`) to project the tenant column and scan it into the carrier.
- [x] **New** batched MySQL `*Many` pre-read (D10): emit `SELECT "id", "<tenant>" WHERE id IN (…)` before the per-item update/delete loop (`update.go.tmpl:395`) to capture each row's tenant into `m.AffectedTenants` (PG/SQLite get the same via per-item `RETURNING` — no extra round-trip).
- [x] Ensure capture is deterministic and additive: tenancy-disabled and non-tenanted tables emit **no** new columns/carrier (byte-identical output); all three dialects covered in every touched builder.

### Acceptance Criteria

- `m.AffectedTenants` is index-aligned with `m.AffectedPKs` for every tenant-∉-PK op on every dialect; for tenant-∈-PK tables no carrier is written and the tenant is read from the PK struct.
- The captured tenant is **always available** post-mutation for every shape (create, single-row by-PK, `*Where`, `*Many`) on PostgreSQL, MySQL, and SQLite — because the tenant column is NOT NULL (§29.2) and every shape now captures it.
- The tenant is captured **structurally** (PK struct or widened capture query), never read off the returned entity `result` — a partial `FieldOptions` select must not be able to zero the captured tenant (D9).
- MySQL `*Many` on a tenant-∉-PK table performs exactly **one** additional batched pre-read (not per-item); no other dialect gains a round-trip.
- Tenancy-disabled and non-tenanted+cached example output is **byte-identical** to pre-phase (additive-only).

### Tests Required

- [x] Index-alignment pin: a test asserting `m.AffectedTenants[i]` corresponds to `m.AffectedPKs[i]` across a multi-row op (D9 "pinned by a test") — `tenancy/tests/affected_tenants_test.go` (`TestAffectedTenants_indexAlignedWithAffectedPKs` covers Create, UpdateMany, SoftDeleteMany, Restore across two tenants under SkipTenancy; `TestAffectedTenants_absentForTenantInPKAndSharedTables` pins the nil-carrier contract).
- [x] Golden diff shows the widened `RETURNING` / pre-`SELECT` on tenanted+cached examples across all three dialects; non-tenanted golden unchanged (feeds T8) — SQLite via the tenancy example E2E golden (`models_gen.go`, +1144/−214); PostgreSQL + MySQL via `gen/tenant_capture_template_test.go` (per-dialect render assertions incl. the MySQL batched pre-read and widened `collectAffected*`); graphql example (tenant-∈-PK) and all non-tenanted examples byte-identical.
- [x] (Integration) MySQL `*Many` executes the batched `SELECT … WHERE id IN (…)` pre-read (covered concretely by T7b captured under 23.2, run on the MySQL leg) — landed with 23.6's `tenancy_mysql` module (`TestMySQLCapture_UpdateManyBatchedPreReadEvictsPrecisely`, MySQL 8 testcontainer).

### Completion Record

**Implemented and verified 2026-07-15** (`/verify` clean — 6/6 acceptance criteria PASS, two independent reviewer passes with 0 defects, 0 FIXes logged; `make check` + `make check-examples` + E2E golden harness green). Files changed:

- `hook/hook.go` — additive `MutationContext.AffectedTenants []any` with the alignment-invariant doc contract: index-aligned with `AffectedPKs`; populated only for tenanted tables with tenant ∉ PK; an individual element is nil when the mutation did not materialize a row for that PK (idempotent delete of a missing key, no-op batch item) — consumers skip such elements; a nil *slice* on a tenant-∉-PK table signals an unwired path (23.2's defensive fallback trigger).
- `sql/builder.go` — new `BuildIncrementReturning` (additive; keeps `BuildIncrement`'s positional signature so non-tenanted output is byte-identical) + `sql/builder_test.go` cross-dialect cases.
- Templates (`cmd/sqlgen/gen/templates/table/`): `create.go.tmpl` (Go-side capture of the inserted tenant value — resolved or caller-supplied; `allTenants` lockstep in CreateMany), `update.go.tmpl` (single-row RETURNING/pre-read; UpdateMany per-item RETURNING on PG — pgx batch switched to `br.Query()` — /SQLite sequential scan/MySQL one batched pre-read; UpdateWhere widened RETURNING + widened `collectAffectedIDs`/`collectAffectedPKs` variants), `delete.go.tmpl` (same treatment across SoftDelete/Restore/HardDelete × single/Many/Where), `upsert.go.tmpl` (widened `insertUpsertAndResolveID` returning `(pk, tenant, error)` on RETURNING dialects; MySQL post-statement read — captures the *existing row's* tenant on the conflict-update path, per D9), `increment.go.tmpl` (`BuildIncrementReturning` / pre-read), `client.go.tmpl` (per-table `captureAffectedTenants` map helper, emitted only for MySQL tenant-∉-PK tables with a consuming op).
- Tests: `gen/tenant_capture_template_test.go` (8 cross-dialect render tests incl. tenant-∈-PK / non-tenanted no-carrier pins), tenancy example `affected_tenants_test.go` (alignment pin, real DB), regenerated tenancy example goldens.

Notes for 23.2 (recorded, not blockers): (1) element-nil semantics above — 23.2's invalidation loop skips nil elements (nothing was mutated for that PK) and reserves the table-pattern fallback + signal for a nil/absent carrier slice; (2) a SkipTenancy update that *changes* the tenant column captures the post-update tenant on PG/SQLite (RETURNING) and the pre-update tenant on MySQL (pre-read) — the design doesn't adjudicate tenant-moving updates (§29.4.2: a row's tenant is not a mutable attribute under normal operation), so the old-tenant cache entry on PG/SQLite survives until TTL in that off-the-paved-road case; (3) MySQL upsert conflicting on a non-PK unique key inherits the pre-existing `AffectedPKs` inaccuracy (input-PK-derived) — the capture misses and yields a nil element there, same blast radius as today's invalidation.

---

## 23.2 B.0 cache-invalidation rewrite — derive the tenant from the row, remove the hard error

**PRD Reference:** Section 27.9 (cache MUST NOT break user-facing calls; `RouteError`; callback modes async/sync; `MetricsRecorder`/`OnErrorFunc`), 29.5. Design §5.2.1 (B.0), §5.2.2 (B.1 defensive fallback), §6, §10-D3.

**Status:** Complete (2026-07-15)

**Depends on:** 23.1.

**Governing decisions:** G1–G3 (correct/consistent/no-escape), D3 (no strict/safe knob — always safe-degrade + always signal), D4 (keep `Backend` floor at prefix + trailing `*`).

### Tasks

- [x] Rewrite `invalidateAffectedTenanted` (`cache.go.tmpl`) to build the tenant-scoped key from the captured tenant (PK struct when tenant ∈ PK; `m.AffectedTenants[i]` when tenant ∉ PK) instead of type-asserting `m.Tenant`. **Remove the "tenant type mismatch" hard error.**
- [x] Rewrite the `OpCreate` write-through branch to source the tenant structurally (not from `m.Tenant.(uuid.UUID)`), so a `SkipTenancy` create write-through populates the cache under the correct `…:tenant:X:…` key instead of silently skipping.
- [x] Route the `dispatchMutation` by-PK / `*Many` group (`OpUpdate`/`OpUpsert`/`OpSoftDelete`/`OpHardDelete`/`OpRestore`/`OpIncrement` + `*Many`) through the row-derived path for tenanted tables — plus the `*Where` group, which now evicts precisely from the widened capture instead of the cross-tenant table pattern (T7a).
- [x] Retain `BuildTablePattern` → `invalidatePattern` as a **defensive-only** fallback for an unexpected nil/absent captured tenant; route it through `cache.RouteError` + emit the `MetricsRecorder`/`OnErrorFunc` signal; **return no error** (enforce G2) — `invalidateTenantedFallback`, emitted once per tenanted package.
- [x] Confirm no invalidation path returns a hard `error` under `CallbackSync` (a committed write must never surface a spurious failure) — the pre-flight refusal is gone; the only error that can propagate is a backend failure, identical to the non-tenanted path and sanctioned by the §27.9 callback-mode contract (T3 pins nil returns in both modes).
- [x] Regenerate tenanted+cached example goldens on all three dialects; diff and confirm churn is confined to the invalidation body + capture (23.1) — tenancy (SQLite, tenant-∉-PK + ∈-PK + required:false) and graphql (PostgreSQL, tenant-∈-PK) `cache_gen.go` shifted; cache/events and all non-tenanted examples byte-identical.

### Acceptance Criteria

- **G1** — a `SkipTenancy` (and `required:false`-zero-tenant) mutation on a tenanted+cached table leaves the cache correct: the affected entry is evicted/refreshed under its exact `…:tenant:{X}:…:pk:{id}` key.
- **G2** — no invalidation path escapes a committed mutation as a caller-visible error, in **either** callback mode; outside a tx the inline path also cannot hard-error.
- **G3** — create / update / delete behave **consistently** under an unresolved tenant by one documented policy (row-derived precise eviction, defensive pattern only on the should-never-happen path).
- Precise eviction on every shape/dialect: no over-invalidation in normal operation (the table pattern is dead unless the defensive branch fires).
- The defensive fallback, when forced, over-evicts (table pattern) **and** emits the observability signal **and** returns nil — one behavior, no config knob (D3).
- No public API change; callers do nothing.

### Tests Required

All in `tenancy/tests/nil_tenant_invalidation_test.go` unless noted:

- [x] **T1** — `SkipTenancy` update evicts the exact entry, tenant-∉-PK (`TestNilTenant_T1_…`; the update runs through the *other* tenant's client so the resolver value actively disagrees with the row).
- [x] **T2** — `SkipTenancy` delete (soft + hard) evicts the exact entry; S untouched (`TestNilTenant_T2_…`).
- [x] **T3** — no callback error escapes a committed write, both callback modes (`TestNilTenant_T3_…`; durability via `SkipCache` read, async eviction via bounded poll, zero cache error signals).
- [x] **T4** — `required:false` zero-tenant, no `SkipTenancy` (`legacy_widgets`/`org_id`) still evicts exactly (`TestNilTenant_T4_…`).
- [x] **T5** — tenant-in-PK precise for Update/HardDelete by PK, `*Many`, `*Where` under `SkipTenancy` (`TestNilTenant_T5_…`).
- [x] **T6** — create write-through under `SkipTenancy` caches under the row's tenant key immediately; partial-`FieldOptions` variant caches nothing (D9 caveat) (`TestNilTenant_T6_…`).
- [x] **T7** — precise `*Where` (a) and `*Many` (b) on `articles` under `SkipTenancy`, unaffected rows survive (`TestNilTenant_T7_…`); SQLite `RETURNING` leg here — the MySQL batched-pre-read leg runs on the MySQL integration leg wired in 23.6 (with 23.1's compile-coverage task).
- [x] **T7b** — forced capture gap degrades to the table pattern + exactly one `invalidate_tenant_fallback` signal + no error (`TestNilTenant_T7b_…`); the converse no-signal assertion closes every T1–T7 test.
- [x] **T8** — non-tenanted byte-identical: golden regen changed only the two tenanted examples' `cache_gen.go`; cache/events/postgres/mysql/sqlite examples untouched (re-verified at closure per 23.6).

### Completion Record

**Implemented and verified 2026-07-15** (`/verify` clean — all G1–G3/§27.9/§29.5 criteria PASS, two independent reviewer passes with 0 defects, 0 FIXes logged; `make check` + `make check-examples` + golden harness green). Files changed:

- `cmd/sqlgen/gen/templates/cache.go.tmpl` — `invalidateAffectedTenanted(ctx, table, tenants []any, pks []any)` rebuilds every key from the row's own tenant: ∈-PK tables via the asserted PK struct → `keyForX(pk.Field, pk)`; ∉-PK tables via `cache.BuildTenantKey`/`BuildCompositeTenantKey` with the raw `any` carrier element (deliberately un-asserted — the §29.5 `%v` grammar makes the concrete local value and 23.4's metadata string build byte-identical keys). Hard "tenant type mismatch" error deleted. New `invalidateTenantedFallback` (cross-tenant pattern + `RouteError` op `invalidate_tenant_fallback` with nil breaker + return nil — G2/D3). Per-element nil skip per the `hook.AffectedTenants` contract. `dispatchMutation` snapshots `m.AffectedTenants` for async callbacks, routes the by-PK/`*Many` group AND the `*Where` group (T7a precise) through the row-derived path, and sources create/createMany write-through structurally (entity PK field ∈-PK; captured carrier ∉-PK with a pk→tenant re-alignment map for CreateMany because GetMany order ≠ AffectedPKs order; nil capture → safe-lossy skip).
- `cmd/sqlgen/gen/context_cache.go` — `CachedTable.TenantInPK`/`TenantFieldName`.
- `cache/error.go` — `OnErrorFunc` doc gains the `invalidate_tenant_fallback` op (reviewer nit).
- Goldens: tenancy (SQLite; ∉-PK single-int64 + ∈-PK composite + required:false) and graphql (PostgreSQL; ∈-PK) `cache_gen.go`; all other examples byte-identical (T8).
- Tests: `tenancy/tests/nil_tenant_invalidation_test.go` — T1–T7b plus a reviewer-suggested per-element nil-skip case (`SoftDeleteMany` with a missing PK); `main_test.go` spyMetrics error-op recording.

Notes: (1) `handleInvalidation` still hard-errors tenanted tables on the event-driven path — that is the live §28.11 defect deliberately fixed in 23.4, not a 23.2 regression. (2) PRD §29.5's "no new invalidation path" sentence now describes pre-phase behavior — rewritten during the 23.6 PRD sync. (3) Composite-∉-PK / string-PK invalidation branches are render-pinned only until 23.6's compiled fixtures (tracked there). (4) The defensive fallback swallows its own backend error after signaling (loses the §27.9 single retry on that should-never-happen path) — design-sanctioned by G2/D3.

---

## 23.3 Event tenant publish (E1) — stamp the captured tenant on every shape

**PRD Reference:** Section 29.6 (single-resolve; tenant in `Event.Metadata["tenant"]`), 28.3 (event envelope `map[string]string`). Design §5.4.2 (E1), §5.4.3.

**Status:** Complete (2026-07-15)

**Depends on:** 23.1.

### Tasks

- [x] Feed the event's tenant from the **same** structurally-captured tenant (23.1: `m.AffectedTenants` / PK struct), not only from `m.Tenant`, in the event hook (`event_hooks.go.tmpl`) — the stamp moved **inside the per-PK fanout loop** so each event carries its own row's tenant (a SkipTenancy batch across tenants stamps per-row, which a batch-shared value could never do); codegen branch: tenant ∈ PK reads `typedPK.<Field>` (or the PK directly when the tenant is the sole PK column), ∉ PK reads `mc.AffectedTenants[i]`.
- [x] Keep the metadata-key-omitted path as a defensive soft-degrade only (nil captured tenant → key absent, never `"<nil>"`) — pinned end-to-end by the unmaterialized-row T9 case.
- [x] Confirm no redaction conflict: redaction touches only `Event.Input`, never `Event.Metadata`, and the tenant column is excluded from mutation inputs — the tenant block sits after the input/redaction switch and touches only `tenantMeta`.
- [x] Preserve the `%v` stamp format so the string round-trips to the exact key bytes (invariant shared with 23.4) — `fmt.Sprintf("%v", …)` on the same value the key builders receive.

### Acceptance Criteria

- A `SkipTenancy` mutation on a tenanted table publishes an event whose `Metadata["tenant"]` equals the row's tenant (from the captured value), across all shapes including MySQL `*Many` (precise per D10).
- A forced-nil-tenant variant omits the `"tenant"` key entirely (soft degrade) — never emits `"<nil>"`.
- The metadata string is byte-identical to the key-build `%v` form (round-trip invariant with 23.4).

### Tests Required

- [x] **T9** — event carries the tenant under `SkipTenancy` (`tenancy/tests/event_tenant_stamp_test.go`): (a) SkipTenancy `UpdateMany` across two tenants stamps each event with its own row's tenant; (b) tenant-∈-PK stamps from the PK struct; forced-nil variant (batch with a missing PK) asserts the key is **absent**, never `"<nil>"`; plus the `required:false`-zero second nil path. MySQL `*Many` leg rides the 23.6 integration wiring. Existing pins of the pre-E1 behavior updated to the new contract: `TestEvent_SkipTenancyStampsRowTenant` (was `…OmitsTenantMetadata`) and `TestEventMetadata_SkipTenancy_AcrossActions`; render tests in `gen/event_hooks_template_test.go` updated + new tenant-∈-PK case.

### Completion Record

**Implemented and verified 2026-07-15** (`/verify` clean — 5/5 criteria PASS, two reviewer passes with 0 defects, 0 FIXes; `make check` + `make check-examples` + golden harness green). Files changed:

- `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` — the `Metadata["tenant"]` stamp moved from a single pre-loop `mc.Tenant` map (shared across the batch, absent under SkipTenancy) to a **per-event stamp inside the AffectedPKs fanout loop**, sourced from the row: `typedPK.<Field>` for composite tenant-∈-PK (failed assert → key omitted), direct `%v` of the PK when the tenant is the sole PK column, `mc.AffectedTenants[i]` for tenant-∉-PK (nil element → key omitted — soft degrade, never `"<nil>"`). Built synchronously before OnCommit registration (async-ctx safety preserved). `%v` format kept — byte-identical to the §29.5 key grammar (round-trip invariant 23.4 consumes).
- `cmd/sqlgen/gen/context_event.go` — `EventTableContext.TenantInPK`/`TenantFieldName`/`CompositePKStructName`.
- Goldens: tenancy + graphql `event_hooks_gen.go` only; events/cache and all non-tenanted examples byte-identical.
- Tests: `tenancy/tests/event_tenant_stamp_test.go` (T9 ×4 incl. per-row stamps across two tenants in one SkipTenancy batch — impossible under the old batch-shared map); pre-E1 pins **deliberately re-encoded to the new frozen-design contract** (`TestEvent_SkipTenancyStampsRowTenant`, `TestEventMetadata_SkipTenancy_AcrossActions`) plus stale-comment refreshes; render tests updated + new tenant-∈-PK and sole-PK-tenant cases.

Notes: (1) behavior change is design-sanctioned — SkipTenancy / required:false-zero mutations now publish tenant-STAMPED events where they previously omitted the key; no production consumer reads `Metadata["tenant"]` yet (the adapter dropping it is the §28.11 defect 23.4 fixes), so no hidden dependents. (2) Verify-reviewer catch adopted: **§29.6 added to 23.6's PRD-sync list** (its ¶2 "resolver closed over into the event payload" is superseded by E1). (3) An event for an unmaterialized row (missing PK in a *Many batch) is tenant-less by design → 23.4's receiver will take the full-table fallback for such events; noted for the 23.6 doc sync.

---

## 23.4 Event-driven invalidation fix (E2) — `InvalidationSignal` struct + precise per-PK receive (fixes §28.11)

**PRD Reference:** Section 28.11 (event-driven invalidation via `FromEventSubscriber` / `InvalidationSource`), 27.9. Design §5.4.1/§5.4.2 (E2), §10-D5/D6.

**Status:** Complete (2026-07-15)

**Depends on:** 23.1 (+ 23.3 for the published stamp).

**Governing decisions:** D5 (§28.11 is a **live defect** for every tenanted table — fix here, not as a separate `/fix`), D6 (widen the signal via a struct, not a positional param — one break spent on a future-proof shape).

### Tasks

- [x] Replace `InvalidationHandler`'s destructured `(table, pks)` with a single `InvalidationSignal{Table, Schema, Tenant, PKs}` struct (`cache/invalidation.go`) — breaking change to the public `InvalidationSource.Subscribe` extension point (acceptable pre-release; D6 rationale in the struct doc).
- [x] Update the `FromEventSubscriber` adapter (`cache/event_adapter.go`) to fill `Signal.Tenant` from `ev.Metadata["tenant"]` (no longer drop it) — plus `Schema` from `ev.Schema`.
- [x] Rewrite `handleInvalidation` to route tenanted-table signals to the **precise per-PK** `invalidateAffectedTenanted` (the same local path — the string tenant repeated per PK; 23.2's un-asserted `%v` key build is what makes the string reuse byte-identical). Tenant-∈-PK tables are precise even without a stamp (the PK carries the tenant), so they skip the tenant-empty check entirely.
- [x] Rebuild the exact key on the receive side from `signal.Tenant` (string), `Table`, `PK`, and the **local codegen-constant fingerprint** (no re-parse; `%v` round-trip); full-table `BuildTablePattern` fallback (via 23.2's `invalidateTenantedFallback` — signal + nil return) only when a tenant-∉-PK signal has no tenant.
- [x] Update all internal call sites + example wiring for the new struct signature — `cache/invalidation_test.go` harness (+ new `TestFromEventSubscriber_ForwardsTenantMetadata`), generated `NewCache` subscribe wiring regenerates automatically; the cache (non-tenanted) example's `cache_gen.go` shifts for the signature only.
- [x] Confirm the rolling-deploy edge is safe: a receiver builds keys with its **own** fingerprint (it only evicts its own cache) — documented in the generated `handleInvalidation` comment.

### Acceptance Criteria

- A publish→subscribe round-trip via `FromEventSubscriber` on a **tenanted** table evicts the **precise per-PK** tenant-scoped key (byte-identical to the local path's key) and returns **no error** — where today it hard-errors (`InvalidateMany` on a tenanted table) and redelivers forever.
- A tenant-absent event falls back to the full-table pattern (defensive).
- Other tenants' entries survive the precise case.
- `InvalidationSignal` absorbs future fields (op, fingerprint) without another breaking change (D6).

### Tests Required

- [x] **T10** — event-driven invalidation works for tenanted tables (`tenancy/tests/event_driven_invalidation_test.go`, two-instance local-publisher/remote-receiver setup over one memorybus): (a) SkipTenancy update on the local instance → remote evicts the precise per-PK tenant-scoped key, no error, no fallback signal; (b) hand-crafted tenant-less event → full-table fallback fires (+ exactly the anomaly signal); (c) the other tenant's remote entry survives (a). **Landed failing first** — both legs reproduced the §28.11 defect (stale remote entry, no fallback) against the pre-fix code, then went green after the rewrite.

### Completion Record

**Implemented and verified 2026-07-15** (`/verify` clean — all criteria PASS, two reviewer passes with 0 defects, 0 FIXes; `make check` + `make check-examples` + golden harness green; **T10 landed failing first**, reproducing the live defect, then went green). Files changed:

- `cache/invalidation.go` — `InvalidationSignal{Table, Schema, Tenant, PKs}` + `InvalidationHandler func(ctx, InvalidationSignal) error`: the one sanctioned pre-release break to `InvalidationSource.Subscribe` (D6; struct absorbs future fields without re-breaking).
- `cache/event_adapter.go` — the shim fills the signal: `Tenant` from `ev.Metadata["tenant"]` (verbatim — the E1 stamp), `Schema` from `ev.Schema`; nil-Metadata safe.
- `cmd/sqlgen/gen/templates/cache.go.tmpl` — `handleInvalidation(ctx, signal)`: empty PKs → `InvalidateTable`; tenanted ∉-PK → precise per-PK via `invalidateAffectedTenanted` with the string tenant repeated (23.2's un-asserted `%v` key build makes the string byte-identical to the local key; fingerprint is the receiver's own codegen constant → rolling-deploy safe), tenant-less → `invalidateTenantedFallback` (signal + nil return — ACK, since redelivery can never gain a stamp); tenanted ∈-PK → precise from the PK structs regardless of stamp (fallback still guards PK-shape assert failures); non-tenanted unchanged.
- Goldens: cache (signature-only churn), graphql (∈-PK), tenancy `cache_gen.go`.
- Tests: `cache/invalidation_test.go` harness → signal shape + `TestFromEventSubscriber_ForwardsTenantMetadata`; `tenancy/tests/event_driven_invalidation_test.go` — T10 two-instance round-trip (precise + survival + no signal / tenant-less → table pattern + one signal) + direct handler-return-nil assertions via a capturing source.

Notes: (1) wire-shape caveat — over JSON transports `ev.PK` arrives as float64/map, so composite-PK asserts fail → safe fallback; memorybus keeps concrete types (precise). Pre-existing sensitivity shared with `InvalidateMany`. (2) One signal carries one tenant — inherent to D6; the adapter emits 1 PK/signal, and a Layer-2 multi-PK-mixed-tenant source must split signals. (3) The precise receive path propagates backend errors (redeliver); the tenant-less fallback ACKs after over-evicting — asymmetry recorded in 23.6's §27.9 sync task for PRD wording.

---

## 23.5 Axis A — explicit-tenant `CallOptions` field (`Tenant *T`)

**PRD Reference:** Section 29.4.2 (`ErrMismatch`, verify-match), 29.4.4 (`SkipTenancy`), 29.2 (uniform tenant type per package). Design §5.1 (A.1d), §10-D2/D7/D11.

**Status:** Complete (2026-07-15)

**Depends on:** none (independent — may be last or deferred).

**Governing decisions:** D7 (concretely-typed `Tenant *<tenant type>` field, not ctx/`any`/generic), D2 (explicit tenant **wins** over `SkipTenancy` — a distinct third resolution mode), D11 (`ErrMismatch` on explicit-vs-input-column disagreement).

### Tasks

- [x] Add a concretely-typed `Tenant *<package tenant type>` field to the `CallOptions` struct, emitted **only** when `tenancyEnabled` (same gate as `SkipTenancy`) **and the package has at least one tenanted table** (the uniform type §29.2 guarantees must exist to bake it); tenant import threaded into `shared_types_gen.go`. No generic `T`, no runtime type-assert.
- [x] Set via `o.Tenant = new(v)` (Go 1.26 `new` with initial value — no `ptr*` helper) — exercised throughout T11.
- [x] Resolution: `resolveTenant(ctx, explicit *T)` short-circuits to `(*explicit, true, nil)` before the ctx-cache / resolver / required checks — one seam; every `c.resolveTenant(ctx)` call site passes `options.Tenant`. Chained ops inherit via both the copied options and the existing `tenancy.WithResolvedTenant` ctx stash, so relationship loaders needed no changes.
- [x] Precedence (D2): normalized once in `resolveCallOptions` — `Tenant != nil` clears `SkipTenancy` — so every downstream `if !options.SkipTenancy` gate behaves correctly unchanged.
- [x] Disagreement (D11): falls out of the **existing** §29.4.2 verify-match blocks (`input.X` / `pk.X` vs `resolvedTenant == *o.Tenant`) → `tenancy.ErrMismatch`, no mutation.
- [x] Emit the field inert/omitted when tenancy is off — golden churn confined to tenancy + graphql `shared_types_gen.go`/`models_gen.go`; a tenancy-enabled-but-zero-tenanted-tables package emits neither the field nor the normalization (unit-pinned).

### Acceptance Criteria

- A create/update with `o.Tenant = new(X)` and **no** ctx resolver applies the tenant filter/auto-set and evicts the exact `…:tenant:X:…` key (feeds the same 23.1 capture; no new invalidation logic).
- Compile-time type safety: a wrong-typed tenant does not build; nil pointer = unset; no runtime type-assert in generated code; no generic-signature churn.
- `o.Tenant = new(X)` with input tenant column `= Y` (≠ X) → `tenancy.ErrMismatch`, no mutation.
- `o.Tenant` set together with `SkipTenancy: true` → explicit wins (scoped to X), no error.
- Field absent/inert when tenancy is disabled.

### Tests Required

- [x] **T11(a)** — explicit tenant: `o.Tenant = new(X)` (client with **no resolver at all**) applies filter/auto-set and evicts the exact `…:tenant:X:…` key (`tenancy/tests/explicit_tenant_test.go`).
- [x] **T11(b)** — `o.Tenant = new(X)` but input tenant column = `Y` (≠ X) → `tenancy.ErrMismatch`, does **not** mutate — pinned for create AND update.
- [x] **T11(c)** — `o.Tenant` set with `SkipTenancy: true` → explicit wins (scoped to X), no error (D2) — proven negatively: the combined flags on another tenant's row return `ErrNotFound` under strict updates, where a `SkipTenancy` win would have mutated cross-tenant.

### Completion Record

**Implemented and verified 2026-07-15** (`/verify` clean — 7/7 criteria PASS, two reviewer passes with 0 defects, 0 FIXes; `make check` + `make check-examples` + golden harness green). Files changed:

- `cmd/sqlgen/gen/context_shared.go` — `CallOptions` gains the concretely-typed `Tenant *<uniform tenant type>` field (JSON tag `tenant`), emitted only when `tenancyEnabled` and the package has a tenanted table; tenant import threaded; `resolveCallOptionsBody(hasExplicitTenant)` adds the D2 normalization (`Tenant != nil` clears `SkipTenancy`) at the single option-resolution chokepoint.
- `cmd/sqlgen/gen/orchestrate.go` — `firstTenantedTableType` derives the §29.2-uniform tenant type from the sorted table contexts.
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — `resolveTenant(ctx, explicit *T)`: explicit non-nil short-circuits to `(*explicit, true, nil)` **before** the ctx-cache/resolver/required checks — one seam; a nested explicit tenant therefore beats a stale chained stash.
- All 9 op templates: `c.resolveTenant(ctx)` → `c.resolveTenant(ctx, options.Tenant)` (chains and relationship loads inherit via the copied options + the existing `WithResolvedTenant` ctx stash — no loader changes).
- Goldens: tenancy + graphql `models_gen.go`/`shared_types_gen.go` only.
- Tests: `explicit_tenant_test.go` T11 a/b/c; unit pins for emission gates + the D2 normalization; template pins updated.

Notes: (1) D11 fell out of the existing §29.4.2 verify-match blocks — zero new mismatch logic. (2) GraphQL per-call middleware deliberately does NOT surface `Tenant` (consistent with `SkipTenancy`'s exclusion from the curated §26 surface; both reviewers concurred). (3) §29.4.4 PRD text describing the explicit mode lands in the 23.6 sync (already listed).

---

## 23.6 Example wiring + full test sweep + PRD reconciliation + design-doc SYNCED

**PRD Reference:** Sections 27.9, 28.11, 29.4.4, 29.5 (sync targets). Design §7 (migration/golden churn), §8 (test plan), §10.

**Status:** Complete (2026-07-15)

**Depends on:** 23.1–23.5 (all in-scope sub-items).

### Tasks

- [x] Wire the tenancy example (`cmd/sqlgen/testdata/examples/tenancy/`) with a memory cache backend + event bus so a single example covers cache + event surfaces; add fixtures needed by T1–T11 — the example already carried cache+events config and all three fixture shapes (`articles`, `order_items`, `legacy_widgets`); 23.2–23.5 added the shared-cache two-tenant env, error-op spy metrics, and the T-suite files.
- [x] Land the full `T1–T11 + T7b` suite — T1–T8+T7b in `nil_tenant_invalidation_test.go`, T9 in `event_tenant_stamp_test.go`, T10 in `event_driven_invalidation_test.go`, T11 in `explicit_tenant_test.go` (all sqlite example); MySQL `*Many` pre-read leg in `tenancy_mysql`, `FromEventSubscriber` round-trip in T10 + the pgx leg in `tenancy_postgres`.
- [x] Compile-and-run legs for the non-SQLite capture paths — **two new example modules**: `tenancy_mysql` (MySQL 8 testcontainer; string tenant type over CHAR(36); single/composite-∉-PK/string-PK tables; runs the D10 batched pre-read, widened collects, single-row pre-reads, per-row event stamps) and `tenancy_postgres` (Postgres 16 testcontainer, pgx; runs the SendBatch per-item `RETURNING` capture via `br.Query()`). **The legs immediately paid off, catching two real defects no other gate could see:** (1) 23.1's MySQL delete/restore paths emitted `_, err := conn.Exec` after the pre-read already declared `err` — a compile error on every MySQL tenanted+soft-delete table (fixed in `delete.go.tmpl`: assignment when the pre-read precedes); (2) a **pre-existing latent** `InvalidateMany` defect — when every cached table is tenanted, the trailing key-dispatch block was unreachable (`go vet` failure; fixed in `cache.go.tmpl` with an `$anyNonTenantedCached` gate). Neither fix changed any existing golden.
- [x] Sync normative bits into the PRD: §27.9 (`InvalidationSignal` handler shape, tenant forwarding in the adapter, receive-path error contract), §28.11 (precise per-PK tenanted receive, rolling-deploy fingerprint safety, tenant-less degrade), §29.4.4 (three resolution modes; explicit-tenant field, D2 precedence, D11 mismatch), §29.5 (row-derived invalidation + defensive fallback + nil-element semantics), §29.6 (per-row structural stamp, `%v` round-trip, soft degrade).
- [x] Regenerate all affected goldens on all three dialects; confirm tenancy-disabled + non-tenanted output byte-identical (T8) — golden harness green and idempotent; only the two new modules appeared in the diff at closure.
- [x] Flip PRD §27.9/§28.11/§29.4.4/§29.5/§29.6 → **SYNCED**; five implementation adjudications recorded in the status banner (nil-element carrier semantics, ∈-PK receive precision, receive-path error contract, D2 normalization site, tenant-moving-update capture asymmetry).
- [x] `/close-phase 23` — full test sweep (`make check`, `make check-examples`, `make test-integration`), FIX triage, STATUS.md + design-doc bookkeeping — landed 2026-07-15: all three legs clean under `-race` (check ~6m23s; examples ~2m10s across 12 modules incl. the two new legs; integration ~9m07s, longest `cmd/sqlgen/cli` ~510s), no flakes; FIX triage empty (0 open, 0 carried); STATUS.md row Complete + closure Current Focus entry; `IMPLEMENTATION_ORDER.md` Phase 23 marked Complete.

### Acceptance Criteria

- All of T1–T11 + T7b pass across the relevant dialects/legs; the example builds and regenerates clean.
- PRD §27.9 / §28.11 / §29.4.4 / §29.5 reflect the shipped behavior; no PRD/design drift.
- PRD §27.9/§28.11/§29.4.4/§29.5/§29.6 marked SYNCED and superseded by the PRD sections above.
- Full sweep green under `-race`; no flakes; FIX triage recorded.

### Tests Required

- [x] The complete T1–T11 + T7b suite lands in the tenancy example (each test attributed to its gating sub-item above; this sub-item guarantees they exist and run green together).
- [x] Regression: tenancy-disabled and non-tenanted+cached examples byte-identical (T8, re-verified at closure).

### Completion Record

- **Date:** 2026-07-15
- **Files changed:** `cmd/sqlgen/gen/templates/table/delete.go.tmpl` (MySQL tenanted soft-delete/restore/hard-delete: `_, err =` assignment branch when the tenant pre-read already declared `err` — fixes a 23.1 compile defect on every MySQL tenanted+soft-delete table), `cmd/sqlgen/gen/templates/cache.go.tmpl` (`$anyNonTenantedCached` gate — fixes a pre-existing latent `InvalidateMany` dead-code/`go vet` failure when every cached table is tenanted), `cache/event_adapter.go` (godoc synced to the D6 signal shape), `docs/PRD.md` (§27.9, §28.11, §29.4.4, §29.5, §29.6 synced to shipped behavior; stale positional-handler sentence in the §27.9 error-routing bullet rewritten per reviewer catch), PRD §27.9/§28.11/§29.4.4/§29.5/§29.6 (banner → **SYNCED (2026-07-15)** with five recorded implementation adjudications), plus **two new example modules**: `cmd/sqlgen/testdata/examples/tenancy_mysql/` (MySQL 8 testcontainer; string tenant over CHAR(36) — deliberate type diversity vs the uuid examples; single-PK+soft-delete / composite-∉-PK / string-PK tables; exercises the D10 batched `*Many` pre-read, widened collects, single-row pre-reads, per-row event stamps, no-fallback pin) and `cmd/sqlgen/testdata/examples/tenancy_postgres/` (Postgres 16 + pgx; uuid tenant via `overrides.types`; exercises pgx `SendBatch` per-item `RETURNING` capture via `br.Query()`).
- **Notes:** The compile-run legs immediately caught the two real codegen defects above that no sqlite-only gate could see — validating the leg strategy flagged by 23.1's reviewers. Verification: two independent reviewer passes; pass 1 found one stale PRD sentence (fixed inline, re-confirmed by pass 2); pass 2 confirmed all findings with zero high-confidence issues. Zero FIX entries logged for the entire phase. Sweep: `make check` exit 0, `make check-examples` exit 0 (both docker legs included), zero FAILs. T8 holds — no pre-existing golden changed. The remaining unchecked task above (`/close-phase 23`) is the phase-closure step itself, checked when the closure sweep completes.
