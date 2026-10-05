# SQLGen Nested Mutations — Create / Update / Upsert With Relationships — Design & Plan

> **Status: ARCHIVED (2026-10-04) — superseded by PRD §9.9** (synced 2026-10-02).
>
> **Do not read this file for current behavior.** The cited PRD sections are normative and
> supersede it on any conflict — see [PRD §9.9](../../PRD.md#99-nested-mutations). Archived because
> Phase 27 is closed and nothing in the code or the PRD references this document; it is kept for
> the record described below, not as a reference.
>
> **Original status note follows.**
>
> **Status: SYNCED — superseded by PRD §9.9 (2026-10-02).** The normative contract now lives in
> `docs/PRD.md` [§9.9](../../PRD.md#99-nested-mutations) — the four write shapes, the FK-nullability verb
> axis, the eligibility matrix and rules E1–E13, the generated surface, execution order, query
> count, error attribution, observability, tenancy and column access — plus
> [§4.6](../../PRD.md#46-generationconfig) / [§4.8](../../PRD.md#48-tableconfig-per-table) / [§4.13](../../PRD.md#413-config-validation-rules)
> (the `nested_mutations` blocks, `relationships[].discriminator`, the four `operations` toggles,
> D20 — Phase 29 later removed the client toggles, leaving `nested_mutations` and D20's `api.operations` half), [§9.2](../../PRD.md#92-write-operations) / [§9.5](../../PRD.md#95-edge-case-behaviors) (`Update`'s
> empty-`FieldOptions` skip, `UpsertMany` and the batched conflict clause),
> [§13.1](../../PRD.md#131-relationship-types) / [§13.4.1](../../PRD.md#1341-discriminator--the-structured-form-of-a-polymorphic-edge)
> / [§13.7](../../PRD.md#137-sub-categorized-polymorphism), §22.1 / §22.3 (`ErrAlreadyRelated`,
> `ErrNestedVerbConflict`, `*NestedMutationError`), §25.1, §26.5.1 / §26.5.5 / §26.12 (the GraphQL
> projection), §27.7, §28, §29.4 / §29.10 and §32.5. Implemented as Phase 27 (27.0–27.11) and
> reconciled line by line against the landed generator at 27.11. This document remains the design
> supplement: the motivation and probe transcripts (§1–§2), the decision record (§3, §12), the
> verb algebra's derivation (§4), the rejected and deferred alternatives (§10), and why the
> `connect` visibility read takes no lock (§13). The companion `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md`
> is retained for its compiled method-by-method examples and its events-and-cache analysis (§7).
> Where wording differs, the PRD wins.
>
> **Superseded claims.** The body below is the design as drafted and is deliberately **not** edited
> to match what landed. These are the places execution moved it, each superseded by the PRD:
>
> - **Phase 29 (2026-10-03) removed the client `operations` toggles** (PRD §4.6). Every
>   `tables.<t>.operations` example below — the §7 `upsert_many: true` among them — now fails
>   validation; E4 and E9 are retired (§9.9.4), and the nested methods follow `nested_mutations` alone.
> - **The nested child input is `<Parent><Edge>CreateInput`**, not `Create<Parent><SingularEdge>Input`
>   (27.7, user ruling 2026-09-18): the old spelling reconstructed the target's own `Create<T>Input`
>   on two shipped example schemas.
> - **E7 — a target's `api.operations` mask narrows the verbs that write the target** (27.10, user
>   ruling 2026-09-23), and the flat update input's `_inc` / `_dec` run inside the nested
>   transaction (§26.5.1).
> - **The MySQL `connect` adoption counts a verify read** rather than relying on a locking pre-SELECT
>   (27.9a, reworked 2026-09-23); E5 refuses *any* junction payload column, optional ones included.
> - **`AffectedPKs` for `UpsertMany` come from the inputs only when the conflict target covers every
>   PK column**; otherwise the sub-batch is read back (27.5, FIX-218).
> - **`Update` with an all-false `FieldOptions` returns `nil, nil`** instead of `ErrNotFound`; the
>   "one fewer round-trip" framing was wrong about HEAD (27.3).
> - **The 27.11 reconciliation** amended E1 (generated, single-keyed target; belongs-to fails it),
>   E8 (an Opaque-PK M2M edge is not write-eligible), E9 (`clear` needs `hard_delete` too), E12 and
>   the verb-conflict check (refused before any statement *on that edge*), the edge order (byte order
>   of the Go field name), the has-one `create` (`Create`, not `CreateMany`), a soft-deleted child
>   under `disconnect` (unlinked, by user ruling 2026-10-02), and D19 (per parent in Go, per family
>   on the API, by user ruling 2026-10-02).
>
> *Original status:* PROPOSED (2026-09-10). Re-verified against HEAD 2026-09-16 (§2.5). Design doc
> only; no code yet. Sourced from a consumer request — *"create the entity alongside their relationships
> in one function, so transaction boundaries close inside the API instead of at the database
> client"* — then investigated against sqlgen source **and measured against real PostgreSQL and
> MySQL containers**. Every structural claim carries a `file:line` citation; every behavioral claim
> carries a probe transcript (§2.4).
>
> **The evidence base was rebuilt on 2026-09-16.** It was taken at `dec00eb`; HEAD is 59 commits
> later and carries three `!` commits, two of which land on this design's foundations. All ten
> structural gaps still hold, every decision survives, and the Go surface still compiles — but
> 24 of 55 load-bearing citations had drifted, Phase 26 changed the type of every nullable FK
> (**F11**, a new silent-no-op hazard), and the fixture grew an eighteenth edge. §2.5 records the
> pass; the repairs are applied throughout.
>
> **This document supersedes and replaces `docs/NESTED_WRITES.md`** (PROPOSED 2026-09-04,
> removed 2026-09-10 — **Q1**). That doc designed the **create half** and deferred the rest
> (*"Deferred: update-side nesting (`update`, `upsert`, `disconnect`, `delete`, `set`)"*). It is
> now folded in whole: its constraints **C1–C9** (§2.1), its decisions as **NW-D1–NW-D14**
> (§3.1), its eligibility rules **E1–E7** (§4.2), its PRD deltas **P1–P13** (§8), its create-side
> Go surface (§5.5) and its open questions (§12). Three of its decisions are **amended** where
> the update half contradicts what they assumed (§3.2). Its history is in git; nothing cites it
> any more.
>
> **Pattern:** sibling design doc in the shape of `docs/design/archive/GRAPHQL_READ_SURFACE.md` (Phase 25) and
> `docs/design/MANIFEST.md` (PRD §30) — findings, decision record, design, PRD deltas, then open
> questions as the gating signal for a `/phase` breakdown.
>
> **Phase: 27.** Claimed 2026-09-16. Phases 0–26 are all Complete and no phase is open
> (`docs/tracker/STATUS.md`), so the number is free. One mechanical prerequisite remains:
> `docs/tracker/IMPLEMENTATION_ORDER.md` stops at Phase 26, and `/phase N` reads its section from that
> file — so the Phase 27 section must be written before the breakdown can run.
>
> **Companion: `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` (2026-09-11).** Writes out every method in
> §5 and §6 as compiled examples across all four edge shapes, classifies the fixture edges
> against §4.1, and measures `UpsertMany` on all three dialects. Its **§7** is the
> events-and-cache analysis **D16** settles in one line and never works through, including six
> open questions (**EQ1–EQ6**).
>
> **Its amendment backlog (A1–A20) is now closed against this document** (2026-09-16). A1, A2, A3,
> A6, A7, A10, A11, A14, A16 and A17 are applied below, joining the five that already were (A4,
> A8, A9, A12, A13), the three folded into Ticket D (A5, A15, A19) and A20. §14's ledger, which
> contradicted the header on A4 and mis-recorded A5/A15 as withdrawn, is repaired. The companion
> itself still needs a sync pass for the edge count and the `uuid.NullUUID` listings (§2.5).
>
> **Hard dependency: one, and it reaches two runtime modules (`hook/` and `sql/` — §11).** Unlike the create half, this feature
> **cannot** be built purely by composing shipped operations. `UpsertMany` does not exist and is required for an idempotent
> M2M `add` (**F4**). Two live defects were also on the path: **F5** is **fixed** — it shipped as
> **FIX-196** on 2026-09-10 (`1d71de4`, `effc99d`) and is no longer a prerequisite — and **F6** is
> closed by the design rather than by a fix.
>
> **Ready for a `/phase` breakdown, with §14 as the list of what that breakdown must settle.**
> This paragraph read "Every open question is answered (§12)" until 2026-09-11; a readiness audit
> found that stale on two counts — §12 predates the events-and-cache analysis (**EQ1–EQ6**), and it
> predates four decisions the design had simply never made (**D18–D21**, now added) plus two it
> cannot make alone (**Q9**, **Q10**). **§14 is the single place to look.** Nothing there blocks the
> breakdown; everything there changes code somebody would otherwise write wrong, which is why it is
> enumerated rather than discovered during the emitter tickets.
>
> **Two items in §14 want to land as standalone FIXes before the phase, and neither is logged yet**
> — **A18** (make the §32.3 redaction switch's `default` fail closed) and **EQ1** (the
> non-tenanted `OpUpdateWhere` cache arm). Both are true today with or without this feature, and
> `docs/tracker/fixes.md` currently holds only FIX-199 and FIX-200.
>
> It is still not *blessed*: §8 lists the PRD amendments that must land as the 27.0 sub-item
> before any code (project rule 1: the PRD is the source of truth).

## Table of Contents

1. [Motivation](#1-motivation)
2. [Findings](#2-findings)
3. [Decision record](#3-decision-record)
4. [Design — the verb algebra](#4-design--the-verb-algebra)
5. [Generated Go surface](#5-generated-go-surface)
6. [Generated GraphQL surface](#6-generated-graphql-surface)
7. [Config surface](#7-config-surface)
8. [PRD deltas required (bless before code)](#8-prd-deltas-required-bless-before-code)
9. [Phased rollout & ticket breakdown](#9-phased-rollout--ticket-breakdown)
10. [Rejected / deferred](#10-rejected--deferred)
11. [Blast radius](#11-blast-radius)
12. [Decision log — resolved questions](#12-decision-log--resolved-questions)
13. [Why the `connect` visibility read takes no lock](#13-why-the-connect-visibility-read-takes-no-lock)
14. [Open questions — what `/phase` still has to settle](#14-open-questions--what-phase-still-has-to-settle)

---

## 1. Motivation

### 1.1 The ask

Three generated mutations should be able to carry relationship work:

1. **`Create`** — create the entity together with new related rows, or associate it with
   existing ones. O2M and M2M, with junction rows written automatically.
2. **`Update`** — `AddContacts` / `RemoveContacts`. `Add` covers both *associate an existing row
   by id* and *create a row and associate it*. Updating a related row's own columns is **not**
   in scope — that is what `UpdateContact` is for.
3. **`Upsert`** — the combination.

### 1.2 What already works, and what the feature actually buys

There is **no atomicity gap**. Every generated mutation opens with
`conn := database.Conn(ctx, c.querier)`, and `Conn` returns the context transaction when one is
live (`database/transaction.go:409-414`). Any composition inside `WithTx` is already atomic,
with savepoint nesting (PRD §18.3), correct deferred side effects (PRD §18.6), and correct
tenancy — the resolver is cached on ctx after the first call (PRD §29.6).

```go
// Works today. Nothing in this document changes it.
err := client.WithTx(ctx, "update user contacts", func(ctx context.Context) error {
    if _, err := client.Users().Update(ctx, uid, &models.UpdateUserInput{
        Name: omittable.Set("New Name"),
    }); err != nil {
        return err
    }
    _, err := client.UserCategories().CreateMany(ctx, []*models.CreateUserCategoryInput{
        {UserID: uid, CategoryID: 7},
    })
    return err
})
```

So the feature buys **three** things, and it is worth being precise about which:

| # | What it buys | Why the baseline cannot |
|---|---|---|
| **1** | **A GraphQL surface at all.** | Generated mutations are one-table-one-call (`templates/api/resolvers.go.tmpl`). A nested write needs a hand-written resolver, forfeiting the field-options walker, the error mapping and the operations mask. |
| **2** | **Correct-by-construction unlink scoping.** | The hand-written version above has no parent guard. Every consumer re-derives `WHERE fk = <parent>` on every unlink, and the one who forgets detaches another parent's rows. §2.4 probe B/A measures both branches. |
| **3** | **Idempotent `add`.** | The obvious hand-written spelling — `CreateMany` on the junction — **errors** when the link already exists (§2.4 probe A). Getting this right requires knowing that the junction's `Upsert` compiles to `ON CONFLICT DO NOTHING`, which is not discoverable from the client interface. |

Item 3 is the one that is easy to miss and expensive to get wrong: the natural
"add a category to a user" implementation is a latent 500 on the second call.

### 1.3 Four write shapes, not three

Relationship *reads* are uniform: the loader does not care which table owns the FK, because the
JOIN or the `WHERE fk IN (…)` is symmetric. Writes are not. The FK's owning side determines
**insert order**, and there are four distinct shapes — not three, because `parser.OneToOne`
covers two of them.

| # | Shape | Fixture in `testdata/examples/graphql` | FK lives on | Insert order | Parent PK flows |
|---|---|---|---|---|---|
| **1** | O2O **belongs-to** | `Profile.Users *User` — `profiles.user_id UNIQUE REFERENCES users` | **parent** | **target first**, then parent | **up** (target PK → parent FK) |
| **2** | O2O **has-one** | `Asset.PrimaryDocument *Document` — `documents.entity_id`, config-declared | target | parent first, then 1 child | down |
| **3** | O2M | `Product.OrderItems`, `User.Orders` | target | parent first, then N children | down |
| **4** | M2M | `User.Categories` via `user_categories` | junction | parent first, targets first, junction rows last | both, into the junction |

Evidence (re-cited at HEAD 2026-09-16): `models_gen.go:20480-20490` (Profile), `:1384-1405`
(Asset — **seven** edges into `documents`, not six: the auto-detected `Documents` M2M joins the six
config-declared ones), `:27816-27837` (User), `:18046-18059` (Product).

**A fifth structural case landed after this section was written, and it is shape 1 with the
PK doubling as the FK.** `3509180` (2026-09-15) added `user_credentials` to the fixture —
`user_id UUID PRIMARY KEY REFERENCES users(id)` (`schema.sql:54-69`) — and taught the parser two
things at once: such a key is **caller-supplied** (PRD §8.6), and a single-column PK is implicitly
unique, so the edge is O2O rather than O2M. It emits `UserCredential.Users *User`
(`models_gen.go:26079`) and **no reverse field on `User`**, because `addO2O` creates exactly one
relationship, on the FK-holder (`parser/relationship.go:176-187`). So it is another belongs-to —
ineligible under **NW-D5**, not a new nestable shape — but it pins something §5.5 should say out
loud: when shape 1 is eventually lifted (§10), the child input for such an edge has **no PK field
left at all** once **C2** elides the traversed FK, because they are the same column.

**Shape 1 inverts the order.** A naive "insert the parent, then insert the children"
implementation produces a foreign-key violation on every belongs-to edge, or silently writes a
zero FK where the column is nullable.

**Shapes 1 and 2 are indistinguishable by `RelationshipType`.** Both are `parser.OneToOne`.
Auto-detection only ever produces shape 1 — `addO2O` puts the edge on the FK-holder
(`parser/relationship.go:176-187`) — but a config-declared relationship can put the FK on the
target, which is exactly what the `assets` fixture does
(`testdata/examples/graphql/sqlgen.yml:170-176`). The generator already copes with this on the
read path, and the way it copes is the tell:

```go
// cmd/sqlgen/gen/context_table.go:2328-2337 — inside buildO2OJoinDetails
onLocal := rel.FKColumn // FK column (on parent by default)
onRemote := pkCol.Name  // PK column (on target by default)
fkOnTarget := false
for _, col := range targetTable.Columns {
    if col.Name == rel.FKColumn {
        fkOnTarget = true
        break
    }
}
if fkOnTarget {
    onLocal = pkCol.Name    // PK on the parent
    onRemote = rel.FKColumn // FK on the target
}
```

That fact is computed **inline, in one function, and thrown away** — `RelationshipContext` has no
`FKOnTarget` field (`cmd/sqlgen/gen/context.go:281-362`). Any write-side emitter needs the same
fact, and re-deriving it in a second place is precisely the failure mode Phase 25's **D1** was
written to kill ("one field map, two emitters" — `docs/design/archive/GRAPHQL_READ_SURFACE.md` §2). Hoisting
`FKOnTarget` onto `RelationshipContext` and rewriting `buildO2OJoinDetails` to read it is a
prerequisite, not a nice-to-have (**NW-D1**, still live per **F2**).

### 1.4 Why the update half is not a small delta on the create half

§1.3 establishes that relationship writes have four shapes where reads have one, because the
FK's owning side determines insert order. The update half adds a second, orthogonal axis: **what an unlink can mean is decided by the FK's nullability, not by
the relationship type.**

| Edge | Type | FK | `disconnect` mechanism |
|---|---|---|---|
| `User.Events` | O2M | `events.user_id` **NULL** | `UPDATE events SET user_id = NULL` |
| `User.Orders` | O2M | `orders.user_id` **NOT NULL** | **impossible without destroying the row** |
| `User.Categories` | M2M | junction | `DELETE FROM user_categories` |

`Events` and `Orders` are the same relationship type, sit on the same parent, and admit
completely different disconnect semantics. Evidence: `cmd/sqlgen/testdata/examples/graphql/schema.sql`
— `events.user_id UUID REFERENCES users(id)` (nullable) vs
`orders.user_id UUID NOT NULL REFERENCES users(id)`.

This is the single fact that shapes §4.

---

## 2. Findings

### 2.1 Create-half constraints (C1–C9)

Carried in from the retired create-half doc and re-verified as still true at `dec00eb`.

| # | Constraint | Evidence | Consequence |
|---|---|---|---|
| **C1** | **Every relationship edge is a single-column FK.** Multi-column FK constraints are dropped: `ApplyConstraintToColumns` only populates `FKReference` when `len(c.Columns) == 1 && len(c.ReferenceColumns) == 1`. | `parser/schema.go:409-437` | FK propagation is always exactly one column. No composite-FK plumbing, ever. Also: a composite-PK table can never be the *referenced* side of a detected edge. |
| **C2** | **The child's FK is a required, non-omittable field on its create input.** `CreateOrderItemInput.OrderID uuid.UUID`, `.ProductID uuid.UUID` — bare types, no `omittable.Value`. | `models_gen.go:14721-14732` | A nested child input must be a **distinct type** with the traversed FK removed. Reusing `CreateOrderItemInput` would force the caller to supply a value that gets overwritten — and on GraphQL, `productID: UUID!` is non-null, so the client must invent a UUID. |
| **C3** | **`upsert<T>` reuses `Create<T>Input`.** `createProduct(input: CreateProductInput!)` and `upsertProduct(input: CreateProductInput!)` share one input type. | `expected/graph/product_gen.graphqls:109,113` | Nested members **cannot** be added to `Create<T>Input`. Upsert would advertise them in the schema and silently ignore them — the exact F2 failure class from Phase 25 (schema accepts, executor drops). |
| **C4** | **A polymorphic edge's `filter:` is a raw SQL predicate string**, e.g. `"entity_type = 'asset.primary' AND name LIKE 'site_%'"`. | `testdata/examples/graphql/sqlgen.yml:176-197`, `config.go:563-575` | It is not invertible into column values in general. `name LIKE 'photo_%'` has no single satisfying value. See **NW-D6**. |
| **C5** | **MySQL `CreateMany` fabricates IDs** as `firstID + int64(i)` from `LastInsertId`. | `templates/table/create.go.tmpl:523-530` | Only correct for contiguous auto-increment. Nested writes must not hang children off a batched parent create. **Updated by FIX-222 (2026-09-24):** the IDs now step by the session's `auto_increment_increment`, read on the session that ran the `INSERT`. That is the nested transaction, or a session pinned without a transaction on the pool. The derivation is exact under Galera and multi-primary steps. The parent-side rule stands. |
| **C6** | **`Create` already has an escape that skips its trailing re-fetch** when `FieldOptions` is non-nil and selects nothing. | `templates/table/create.go.tmpl:141,175,396` | Intermediate writes in a nested operation cost one INSERT each, not INSERT + SELECT. One final `Get` hydrates the whole graph through the normal loaders. |
| **C7** | **M2M junctions are themselves generated tables** with their own client, composite-PK create input, hooks and events. `CreateUserCategoryInput{UserID, CategoryID}`. | `models_gen.go:26045-26048`, `client_gen.go:249` | Junction rows must be written through the junction's own client, not a raw INSERT, or its hooks and events silently stop firing. |
| **C8** | **The generated package name space is collision-checked at codegen** across Go type name, file stem, and `hook.TableName`. | `cmd/sqlgen/gen/resolved_names.go:12-48` | Every new type name this feature introduces must be registered there, or `CreateProductOrderItemInput` can silently collide with a real `product_order_items` table. **Registering entries is not enough (A6).** The registry keys on each entity's *primary* name, and these types key on **(parent, edge)** — a `user_events` table and the `User.Events` edge both resolve to `CreateUserEventInput`, which is precisely the cross-shape class the registry's own scope comment pushes out as *"a `go build` error"* (`:48`). The feature needs a **new key kind**, not new rows in an existing one. No fixture collides today — the eligible pairs resolve to `CreateUserEventInput`, `CreateUserOrderInput`, `CreateProductOrderItemInput`, `CreateOrderOrderItemInput`, `CreateCategoryProductInput` and `CreateWorkspaceNoteChildInput`, and no table claims any of those — so the hazard is latent, not live. |
| **C9** | **The M2M read loader queries the junction raw and the target through its own client.** `conn.Query` on `user_categories` (no tenancy, no soft-delete), then `c.categoryClient.GetMany` (both applied). | `models_gen.go:4326-4340` | A junction row pointing at a target the caller cannot see is **silently dropped from the read**. On the write side nothing checks target visibility at all — this is why `connect` needs a visibility check (**NW-D13**). |

### 2.2 New structural findings

| # | Finding | Evidence | Consequence |
|---|---|---|---|
| **F1** | **Single-row `Update` is the only mutation with no skip-refetch escape.** `Create`, `CreateMany`, `Upsert`, `UpdateMany`, `UpdateWhere` and every delete variant guard their trailing read with `options.FieldOptions != nil && !options.FieldOptions.HasSelectedColumns()`. Single-row `Update` returns `c.Get(...)` unconditionally. | escape present at `create.go.tmpl:141,175,396`, `upsert.go.tmpl:151,230`, `delete.go.tmpl:134,364,880,1081`, `update.go.tmpl:630` *(that one is inside `UpdateMany`)*; absent before `update.go.tmpl:81` and `:203`, the single-row `Update`'s two `return c.Get(ctx, …)` sites | **Corrected 2026-09-17 during 27.3.** The consequence stated here — two reads — is wrong, and the real one is worse. `Get` delegates to `GetMany`, which already short-circuits on an all-false `FieldOptions` and returns `nil, nil`; `Get` reads that empty result and returns **`ErrNotFound`**. So `Update` under an empty `FieldOptions` issued **one** statement and returned `(nil, ErrNotFound)` after a committed `UPDATE` — a spurious failure of the FIX-196 class, not a wasted round-trip. `UpdateWithRelated` composing `Update` for the parent could therefore not have used an empty `FieldOptions` at all. See **D8**; the fix is the same 3-line additive template change, landed in 27.3. |
| **F2** | **`Side` on `RelationshipContext` does not answer "which table holds the FK".** `Side` exists (`gen/context.go:291`, `parser/schema.go:69-95`) and the manifest uses it to split o2o from m2o (`cmd/sqlgen/manifest/builder.go:1226`). But it is *declared*, not structural: `addO2O` and `addO2M` both hard-code `SideParent` (`parser/relationship.go:182`, `:197`), and a config edge defaults to `"parent"` (`config/config.go:556-561`). `assets.PrimaryDocument` declares no `side:` — so `Side == SideParent` — while its FK `entity_id` is physically on the **target** (`testdata/examples/graphql/sqlgen.yml:170-176`). | as cited | **NW-D1** (hoist a structural `FKOnTarget` onto `RelationshipContext`) is **not** superseded by `Side` and is still a prerequisite. The read path still re-derives the fact inline by column scan at `gen/context_table.go:2328-2337`. |
| **F3** | **`HardDeleteMany` on a composite-PK junction is one statement.** It builds `sql.BuildCompositePKBatchCondition` over all pairs (`models_gen.go:25400-25425`). `HardDeleteWhere` also exists and returns affected PKs via `RETURNING`. | as cited | M2M `disconnect` is one statement through the junction's own client — D10-compliant, no new primitive. |
| **F4** | **`UpsertMany` does not exist anywhere in the generated surface** — not in the templates, not in `Operations` (`config/config.go:399-418`), not on any client interface. Only single-row `Upsert(input, target)`. | `grep -rn UpsertMany cmd/sqlgen` → no matches | **This is the one hard dependency.** Idempotent M2M `add` needs a batched conflict-tolerant insert; without `UpsertMany` it is N round-trips, breaking the §25.1 one-statement-per-table contract. See **F7**, **D5**. |
| **F5** | **`Upsert` fails to resolve the PK when the conflicting row is left unchanged — on all three dialects.** One defect, two dialect-specific triggers. *MySQL:* the non-`RETURNING` branch resolves the PK from `execResult.LastInsertId()` (`upsert.go.tmpl:317-345`) and the OK packet carries `insert_id = 0` for an `ON DUPLICATE KEY UPDATE` that modifies no row. *PostgreSQL / SQLite:* when the conflict target covers every inserted column, `updateColumns` is empty and the dialect emits `DO NOTHING` (`sql/postgres.go:88-111`, `sql/sqlite.go:61-83`), which returns zero rows, so the `RETURNING` branch's `QueryRow().Scan()` gets the driver's no-rows error. Scope is the `$needsInsertResolve` condition — single-column, db-generated PK — so composite-PK tables and app- or caller-generated PKs are unaffected. | measured — §2.4 probe E | **Was a live bug in shipped code, independent of this feature: the caller got a failure for a write that happened, which inside `WithTx` rolls back the whole transaction. Resolved as FIX-196 on 2026-09-10.** `UpsertClause` now takes `UpsertClauseOptions{ConflictKeys, UpdateColumns, ResolvePKColumn}`; MySQL opens its set list with `` `pk` = LAST_INSERT_ID(`pk`) ``, and the `RETURNING` dialects fall back to a `resolveUpsertConflictRow` lookup when no row comes back. **No longer blocks this feature** — see **D11**, **E10**, **P17**. |
| **F6** | **`CreateMany` on an already-linked M2M pair raises a unique-constraint violation.** | measured — §2.4 probe A | **NW-D10 + NW-D14** (route junction rows through `CreateMany`; dedupe within the request) is correct for *create* and **wrong for *update***: it dedupes the request against itself but not against the database. |
| **F7** | **The junction's own `Upsert` is already idempotent.** With a pure link table every column is in the conflict target, so `updateColumns` is empty and the dialects emit `ON CONFLICT (…) DO NOTHING` (`sql/postgres.go:100-102`) / `ON DUPLICATE KEY UPDATE k = k` (`sql/mysql.go:63-69`). A junction's PK is caller-known, so there is nothing to resolve and **neither F5 trigger reaches it** — the `DO NOTHING` form is exactly what makes it idempotent. | as cited; measured — §2.4 probe B | The right primitive exists and is dialect-correct; it is only missing its batched form (**F4**). **Sharpened 2026-09-11 by reading the generated junction `Upsert` rather than inferring it** (`models_gen.go:25493-25526`): it is a single `conn.Exec` with **no `RETURNING` branch and no read-back** — `updateColumns` resolves empty via `excludeColumns(columns, append(conflictColumns, …))` — and `m.AffectedPKs` is built from the *input* PK, not from the database (`models_gen.go:25521`). That is the concrete reason neither **F5** trigger can reach a junction, and it is the behavior `UpsertMany` must preserve at batch scale: **its `AffectedPKs` must likewise come from the inputs, not from `RETURNING`**, or §6.2's short-row problem propagates straight into the event fanout (**EQ5**). **Resolved in 27.5 (verified 2026-09-17): this premise no longer holds.** `UpsertMany` sources `AffectedPKs` from the inputs, or from `resolveUpsertManyRows`, which returns exactly one key per deduped value row and errors otherwise — never from `RETURNING` (**A19**) — and the terminal republishes `m.Input = rowInputs`, the deduped slice, so it stays index-aligned with `AffectedPKs` (PRD §28.9, commit `ad575c5`). 27.6's arm therefore **is** an `OpCreateMany`-shaped index arm; see **FIX-219**. |
| **F8** | **Two client-wiring deltas, unchanged from the create half.** `userClient` holds `categoryClient` but not `userCategoryClient` (`client_gen.go:183-193`), and `callbackMode` lives only on the client's options struct (`client_gen.go:21`), not on entity clients. | as cited; reproduced by the compiler in §2.3 | Two additive fields in the existing injection block. |
| **F9** | **`UpsertMany` needs a new `hook.MutationOp`, and three generated switches fail *silently* without it.** `hook/hook.go:17-33` declares one constant per operation and has no `OpUpsertMany`; the PRD mirrors that list (`docs/PRD.md:9537-9542`). Every consumer switch has a permissive default: cache invalidation has **no `default` arm at all** and falls out of the switch to a bare `return nil` (`cache_gen.go:5304-5337`), `mapOpToAction` returns `event.Action("upsert_many")` from its `default` arm (`event_hooks_gen.go:33-66`), and the §32.3 redaction switch falls through to `default: inputVal = mc.Input` (`event_hooks_gen.go:1271-1272`, inside `newUserEventHook` at `:1198`, the table that actually has redacted columns — an earlier draft of this row cited `:117-135`, which is the `assets` hook and has no redaction at all). | as cited; §7.4 of `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` | **One of the two places this feature reaches a runtime module** (see §11). Omitting any arm compiles clean and misbehaves at runtime: stale cache entries, and a new wire action no subscriber filters on. The redaction arm is **worse than a missing payload**, which is how this row first described it. `UpsertMany` sets `mc.Input` to the whole input slice, as `CreateMany` does (`create.go.tmpl:219`), so the `default` arm gives *every* fanned-out event the entire batch — O(N²) on the wire, each event carrying an input that is not its own — and gives it **un-redacted**, `PasswordHash` and `NewPassword` included. `redactUserCreateInput`'s own comment says nil passes through "rather than leaking the raw input"; the default arm does exactly that. Treat it as a **§32.3 violation** with a failing-first pin, not a cosmetic gap. All four edits land together in Ticket D — **D16**, **P24**. |
| **F10** | **A soft-deletable junction cannot be unlinked correctly by either mechanism.** The M2M read loader builds its junction query as a bare `BuildSelect` with only the `IN` condition and **no soft-delete predicate** (`models_gen.go:4326-4331`) — this is **C9**'s "raw junction query", sharpened. A junction carrying `deleted_at` would still generate `SoftDeleteMany` on its own client. | as cited; no fixture junction has a soft-delete column | Hard-deleting destroys a row the schema marked soft-deletable; soft-deleting leaves the link **visible to every subsequent read**. Both are wrong, so the verb is refused — **E11**. |
| **F11** | **Phase 26 changed the type of every nullable FK, and the new spelling makes "set to NULL" and "leave alone" look alike.** The stdlib `uuid` package has no `NullUUID`, so `eb58ae2` bound nullable UUID columns to `*uuid.UUID`: `Event.UserID` is now `*uuid.UUID` (`models_gen.go:7611`) and `Create`/`UpdateEventInput.UserID` are `omittable.Value[*uuid.UUID]`. Under the old `uuid.NullUUID` the two intents were different constructor calls — `omittable.Set(uuid.NullUUID{})` set the column NULL, `omittable.Value[uuid.NullUUID]{}` omitted it. Under a pointer they are `omittable.Set[*uuid.UUID](nil)` and `omittable.Value[*uuid.UUID]{}`, and **the compiler accepts either wherever the other belongs.** | measured — §2.5, mutation 4 of the failing-first set: substituting the omitted form for the NULL form in `disconnect` **builds clean** | **Every `disconnect` and every `clear` on an O2M edge turns on this one expression.** Written the wrong way the `UpdateWhere` sets no column, matches its rows, reports success and unlinks nothing — a silent no-op on the verb whose entire purpose is to unlink, and the one failure shape a `RowsAffected` check cannot catch because the rows *do* match. The other three type hazards §2.3 pins are still compile errors; this one is not, so it needs a **runtime** pin. Ticket F carries it as a failing-first E2E: unlink a child, then assert its FK is actually NULL. |

### 2.3 The Go surface compiles

The `UpdateWithRelated` body in §5.3 and its input types were **compiled against the real
generated package**, not sketched: dropped into
`cmd/sqlgen/testdata/examples/graphql/models/`, `go build` and `go vet` clean under `GOWORK=off`.

The compiler found exactly one thing the design sketch had missed, and it was **F8**:

```
models/zz_probe_nested_update_gen.go:227:20: c.userCategoryClient undefined
    (type *userClient has no field or method userCategoryClient)
```

Substituting a package-level stand-in for that one field produced a clean build.

**Re-run at HEAD on 2026-09-16 (§2.5), and it still builds — but not as written.** The probe was
reassembled from the companion's §3 listings and needed three repairs before it compiled, each of
which is a real change in the generated surface rather than a transcription slip:

| Repair | Cause |
|---|---|
| `omittable.Set(uuid.NullUUID{UUID: parent.ID, Valid: true})` → `omittable.Set(&parent.ID)` | **F11** — `eb58ae2` bound nullable UUIDs to `*uuid.UUID`; `uuid.NullUUID` no longer exists |
| `omittable.Set(uuid.NullUUID{})` → `omittable.Set[*uuid.UUID](nil)` | same |
| `UserCategoriesCreateNested`, `CreateUserOrderInput` declared as stand-ins | the companion's §3.1 listing references both and declares neither — a doc gap, not drift |

With those applied, `go build` and `go vet` are clean. The failing-first set was re-run to confirm
the probe type-checks rather than compiling vacuously, and **the transcripts have changed**:

| Mutation to the probe | Compiler response at HEAD |
|---|---|
| nullable O2M FK assigned bare — `UserID: omittable.Set(parent.ID)` | `cannot use omittable.Set(parent.ID) (value of struct type omittable.Value[uuid.UUID]) as omittable.Value[*uuid.UUID] value in struct literal` |
| NOT NULL O2M FK given a string field — `UserID: parent.Name` | `cannot use parent.Name (variable of type string) as uuid.UUID value in struct literal` |
| junction FK given the parent UUID — `CategoryID: parent.ID` | `cannot use parent.ID (variable of array type uuid.UUID) as int64 value in struct literal` |
| **unlink spelled as omit** — `omittable.Set[*uuid.UUID](nil)` → `omittable.Value[*uuid.UUID]{}` | **none — builds clean.** This mutation used to be a type error and is not one any more. It is **F11**, and it is why `disconnect` needs a runtime pin rather than a compile-time one |

The probe was removed after verification; it is not checked in.

### 2.4 Measured runtime behavior

Five probes. **A–D** ran against the `graphql` example's real generated client on
`postgres:16-alpine`; **C/E** ran on `mysql:8.0`. Everything below is a transcript, not a claim.

#### Probe A — M2M `add` on an existing link (`CreateMany`)

```
A CreateMany on existing link -> err=create user_categories batch: sqlgen: unique constraint
  violation on constraint user_categories_pkey: Key (user_id, category_id)=(f3ef462e-…, 1)
  already exists.
```

→ **F6.** The obvious implementation of `AddCategories` fails on the second call.

#### Probe B — M2M `add` on an existing link (junction `Upsert`)

```
B Upsert on existing link -> err=<nil>
```

→ **F7.** `ON CONFLICT DO NOTHING` is already wired; only its batched form is missing.

#### Probe C — M2M `disconnect` is idempotent and inherently parent-scoped

```
C HardDeleteMany nonexistent    -> err=<nil>
D HardDeleteMany foreign parent -> err=<nil>
links remaining=1
```

Deleting a link that does not exist is a silent no-op, and deleting
`{UserID: <someone else's id>, CategoryID: cat}` **left the real link intact**. Because the
junction's PK embeds the parent, a caller can only ever delete its own links — **no visibility
read is needed on `disconnect`**, which is the asymmetry with `connect` (**D6**).

#### Probe D — the O2M unlink guard

```
A unlink with foreign parent guard -> rows=0 err=<nil>
   event.user_id still owner? true
B unlink with correct parent guard -> rows=1 err=<nil>
   event.user_id now NULL? true
C repeat unlink (already NULL)     -> rows=0 err=<nil>
D unlink with EMPTY filter         -> err=sqlgen: empty filter on bulk operation
```

The `UserID: Eq(parent)` term in the filter is load-bearing and works: a foreign parent's row is
untouched, a repeat is idempotent, and `ErrEmptyFilter` (`update.go.tmpl`, `UpdateWhere`) is a
real backstop against an unguarded bulk unlink.

#### Probe E — MySQL upsert PK resolution, and the fix

Against `mysql:8.0` through `go-sql-driver/mysql v1.9.3` (the repo's driver), on a table whose
`AUTO_INCREMENT` had been advanced past the row under test so a stale id would be visible:

```
plain: insert a@x (new; real id 3)          LastInsertId=3   RowsAffected=1

-- conflict with IDENTICAL values (the broken case) --
plain: upsert a@x -> A   (NO-OP update)     LastInsertId=0   RowsAffected=0
trick: upsert a@x -> A   (NO-OP update)     LastInsertId=3   RowsAffected=0

-- control: conflict with a CHANGED value --
plain: upsert a@x -> B   (real update)      LastInsertId=3   RowsAffected=2
trick: upsert a@x -> C   (real update)      LastInsertId=3   RowsAffected=2

-- control: trick on a genuinely new row --
trick: insert z@x        (new row)          LastInsertId=9   RowsAffected=1
```

`trick` is `ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), name = VALUES(name)`.

> **Note on method.** A first pass probed this with `SELECT LAST_INSERT_ID()` from the `mysql`
> CLI and appeared to show a much broader bug (a conflict returning a *different* row's id).
> That reading was an artifact: `SELECT LAST_INSERT_ID()` reads per-session state, while the Go
> driver's `LastInsertId()` reads the OK packet of the statement itself. Only the driver path
> matters, and it is correct on every branch except the no-op update. The narrower finding is
> the real one.

End-to-end against shipped generated code, `graphql`-shaped fixture on the `mysql` example:

```
=== RUN   TestProbeIdempotentUpsertMySQL
    insert  -> id=1 title="ProbeProduct"
    Upsert (idempotent) returned error: sqlgen: resource not found  (ErrNotFound=true)
    changed -> id=1 price=25
--- FAIL
```

> **Correction — FIX-196, 2026-09-10.** The same test passes on the `postgres` example, and this
> document originally read that as bounding **F5** to MySQL. **It does not.** That control passes
> only because its table leaves a column outside the conflict target, so it takes the
> `DO UPDATE SET` branch. Where the conflict target covers every inserted column — `tenancy`'s
> `tags` and `users` ship exactly that shape (`tenancy/models/models_gen.go:14789`), and so does
> any surrogate-PK + natural-unique-key lookup table — PostgreSQL and SQLite emit `DO NOTHING`,
> return zero rows, and fail the same PK resolution by a different route:
>
> ```
> mysql:    Upsert (idempotent) = sqlgen: resource not found            (ErrNotFound=true)
> postgres: Upsert (idempotent) = upsert category: pgx scan: no rows in result set
> sqlite:   Upsert (idempotent) = upsert tag: stdlib scan: sql: no rows in result set
> ```
>
> The `LAST_INSERT_ID(pk)` mechanism measured above is the MySQL half only; a prepended set-list
> entry cannot repair `DO NOTHING`, which is a change of clause form, so the `RETURNING` dialects
> got a conflict-row lookup fallback instead. Full resolution in `docs/tracker/fixes.md`
> **FIX-196**.

The existing `TestUpsert` (`examples/mysql/tests/crud_test.go:553`) never changes nothing, so it
did not catch this; `upsert_idempotent_test.go` on the `mysql` and `postgres` examples now does.

---

### 2.5 Re-verification against HEAD (2026-09-16)

The evidence in §2.1–§2.4 was taken at `dec00eb`. HEAD is **59 commits** later and carries three
`!` commits, two of which land on this design's foundations. This section records what survived.

**All ten structural gaps still hold**, re-checked directly rather than carried over:
`UpsertMany` absent everywhere (`grep -rn UpsertMany` over `*.go` and `*.tmpl` → 0);
`sql.MultiInsertOptions` still three fields, no conflict clause (`sql/builder.go:52-57`);
`hook.MutationOp` still 16 constants with no `OpUpsertMany` (`hook/hook.go:16-33`);
no `FKOnTarget` anywhere; single-row `Update` still returns `c.Get(...)` unconditionally
(`update.go.tmpl:81`, `:203`); no `discriminator:` in config; none of the three new error types in
`database/errors.go`. **No decision in §3 is invalidated.**

> **Snapshot, not current state (noted 2026-09-17).** The list above is what was true at the
> 2026-09-16 re-verification and is kept verbatim as the record of it. Four of those ten gaps have
> since been closed by this phase, so do not grep the line and conclude the work is outstanding:
> `sql.MultiInsertOptions` now carries the three upsert fields and `BuildMultiInsert` emits the
> conflict clause (**27.4**); `FKOnTarget` is on `RelationshipContext` (**27.1**); `discriminator:`
> is in the config (**27.2**); single-row `Update` no longer returns `c.Get(...)` unconditionally
> (**27.3**). `UpsertMany`, `hook.OpUpsertMany` and the three `database/errors.go` types are still
> absent — they are 27.5, 27.6 and 27.7 respectively.

**Three things did change, and each is applied above.**

**(1) `eb58ae2` — stdlib UUID.** Nullable UUID columns are now `*uuid.UUID`, not `uuid.NullUUID`.
This is **F11**, a new silent-no-op hazard on exactly the two verbs that unlink.

**(2) `3509180` — a foreign-key primary key is caller-supplied.** Added `user_credentials` to the
fixture and taught `columnUnique` to consult `PrimaryKey`, so a 1:1 extension edge is now O2O
rather than O2M. Consequences: the fixture has **eighteen** edges, not seventeen (§4.1), and the
eighteenth is another belongs-to — ineligible, but it pins a note §1.3 now carries about what
happens to the child input when the PK *is* the traversed FK.

**(3) `336f591` — primary-key overrides resolve before relationship detection.** Two consequences.
`parser.Column.PrimaryKey` now carries the resolved key, so **E3** is evaluable from one source
instead of two. And `buildConflictTargets` is now **gated on schema evidence of an index** — a
set-equal PRIMARY KEY constraint, a set-equal non-partial UNIQUE, or an inline single-column
UNIQUE — so a table whose uniqueness is app-enforced emits **no `<T>ConflictPK` constant at all**.
That is a new precondition for every conflict-target argument this design hands out: **E10** now
carries it.

**Citation drift: 24 of 55 load-bearing citations had moved**, checked by content rather than by
line-range validity (a line number that still exists but now points at unrelated code is the
failure mode that matters). All 24 are repaired above. The largest movers were
`parser/relationship.go` (`addO2O` 136→176, `addO2M` 148→191) and the `graphql` fixture's
`models_gen.go`, which grew ~1,700 lines from the `user_credentials` addition alone. **Two claims
were re-read in full rather than re-pointed**, because a moved line is a chance to find the claim
itself is stale: `addO2O`/`addO2M` do still both hard-code `SideParent`, so **F2** and **NW-D1**
stand; and the `fkOnTarget` block §1.3 quotes is byte-identical at its new location.

### 2.6 Measured — the bind-parameter ceiling per dialect (Q9)

Run 2026-09-16 against `postgres:16-alpine`, `mysql:8.0` and `modernc.org/sqlite v1.48.1`, using
the exact statement shape a `disconnect` emits: `SELECT count(*) FROM probe WHERE id IN ($1…$n)`.
Binary-free transcript, not a claim.

| Dialect | Largest `IN` list accepted | First rejected | Error returned |
|---|---:|---:|---|
| **SQLite** | **32,766** | 32,767 | `*sqlite.Error: SQL logic error: too many SQL variables (1)` |
| PostgreSQL | 65,535 | 65,536 | `*errors.errorString: extended protocol limited to 65535 parameters` |
| MySQL | 65,535 | 65,536 | `*mysql.MySQLError: Error 1390 (HY000): Prepared statement contains too many placeholders` |

**Three things follow, and two of them contradict what §14 assumed.**

**SQLite binds at half of what the question named.** Q9 derived its worst case from PostgreSQL's
65535. SQLite stops at 32766, so an O2M `disconnect` breaks at ~32.7k ids and a composite-PK
junction at ~16.4k pairs — two parameters each. The ceiling is therefore **dialect-dependent**,
which is what makes "document the limit" (option b) unattractive: there are three limits, and the
tightest belongs to the dialect people develop against least.

**No dialect's error names anything the caller can act on.** None mentions a table, column, edge or
verb, so §4.4's per-edge attribution cannot reach it. PostgreSQL's is the worst of the three for a
different reason: it is a bare `*errors.errorString` raised **client-side by pgx before the
statement is sent**, so it is not a `*database.ConstraintError`, not a sentinel, and `errors.As`
has nothing structural to match. It would surface to a GraphQL caller as a generic `INTERNAL`.

**Batching at `batchSize` clears all three by two orders of magnitude.** The default is 200
(`models_gen.go:133`), against a worst case of 32,766 — so the ceiling stops being reachable rather
than being relocated or documented. This is what **Q9** resolves to.

---

## 3. Decision record

### 3.1 Create-half decisions (NW-D1–NW-D14)

Carried in from the retired create-half doc, numbered `NW-` to keep them distinct from the
decisions this document adds in §3.3. Three are amended in §3.2; the rest stand as written.

| # | Decision | Rationale |
|---|---|---|
| **NW-D1** | **Hoist `FKOnTarget` onto `RelationshipContext`**, computed once in the relationship builder, and rewrite `buildO2OJoinDetails` to read it instead of re-deriving it. | §1.3. One fact, one derivation, two emitters (read path + write path). This is Phase 25's D1 applied to a second surface. Still live — **F2** confirms `Side` does not answer the same question. |
| **NW-D2** | **A separate composed method, not a change to `Create`.** `client.Products().CreateWithRelated(ctx, *CreateProductWithRelatedInput, opts…)`. | `create.go.tmpl` is already branched across tenancy (∈ PK / ∉ PK / skip) × PK strategy (app / db / caller) × `supportsReturning` × composite PK. Adding a nested arm multiplies an already-multiplied template. A separate template *composes* the shipped operations instead of modifying them: smaller blast radius, independently testable, and zero golden churn for tables with no eligible edge. |
| **NW-D3** | **A separate GraphQL mutation and a separate input type**, `createProductWithRelated(input: CreateProductWithRelatedInput!): Product!`. | Forced by **C3** — `Create<T>Input` is shared with upsert and cannot grow nested members. Since a distinct input type is mandatory anyway, a distinct mutation is nearly free and keeps `createProduct` byte-identical. |
| **NW-D4** | **The wrapper input *contains* `Create<T>Input`; it does not restate its fields.** | Zero drift between the flat and nested forms. The §32 access projection, the column-map overrides, the increment operators and every future create-input change are inherited for free. Restating the field list is a second derivation of the same fact — see **NW-D1**. |
| **NW-D5** | **v1 nests only in the direction the parent's PK flows *down*: shapes 2, 3, 4.** Shape 1 (belongs-to `create`) is deferred with a costed mechanism in §10. | Two payoffs. (a) The emit order becomes unconditionally *parent first* — no topological sort, no ordering inversion, no cycle in the write plan. (b) It sidesteps a hard blocker: eliding the parent's *own* FK column requires a second parent input type plus per-edge XOR validation, because `CreateProfileInput.UserID` is required (**C2**). Shape 1 also loses the least: `connect` on a belongs-to edge is *already expressible* — it is the plain FK column on the create input. |
| **NW-D6** | **Nested writes are ineligible on any edge declaring `filter:`.** Add a structured, invertible `discriminator: {column, value}` to `TableRelationship` as the supported alternative; an edge with `discriminator` and no `filter` **is** eligible. `filter` and `discriminator` are mutually exclusive (config error). | **C4**. Writing through an uninvertible predicate produces the sharpest possible bug: the insert succeeds and the row is **invisible to the edge that created it** — `asset.Attachments` returns rows matching `entity_type = 'asset.attachment'`, so a nested create that leaves `entity_type` to the caller can write a row that never appears under the parent. Refusing is the fail-closed direction. `discriminator` then unlocks §13.7 sub-categorized polymorphism, which is where nesting is *most* valuable. |
| **NW-D7** | **Create-half verbs: `create` on shapes 2/3/4, plus `connect` on M2M only.** | `connect` on shape 1 is redundant with the existing scalar FK field. `connect` on shape 3 is an `UPDATE child SET fk = ?` that steals rows from another parent. **Amended by A3** — O2M `connect` ships, restricted to adopting unparented rows (**D7**). |
| **NW-D8** | **Depth 1.** A nested child input carries no nested block of its own. | Makes cycles structurally impossible — §13.5 self-referential and §13.6 circular edges cannot recurse if there is nowhere to recurse *to*. Keeps GraphQL input types non-recursive, so `max_depth` (§26.6) has nothing new to reason about. Raising the cap later is additive. |
| **NW-D9** | **Nested writes hang off single-row `Create` only, never `CreateMany`.** | **C5**. MySQL's fabricated batch IDs are correct only for contiguous auto-increment; propagating them into child FKs would write children onto the wrong parents, silently, on one dialect. |
| **NW-D10** | **Every write routes through the target's own generated client method** — never a hand-rolled INSERT in the nested template. | **C7**. Hooks, events, cache invalidation, tenancy auto-set, `AffectedPKs` and the §22 constraint-error mapping all live in those methods. Bypassing them would make nested writes a second, silently divergent write path. |
| **NW-D11** | **Query-count contract: one INSERT statement per participating table, plus one final read.** Intermediate writes suppress their re-fetch via **C6**. | Mirrors the §25.1 read guarantee and makes N+1 a *test-detectable* regression rather than a performance surprise. Extended per-verb by **D12**. |
| **NW-D12** | **A codegen completeness lint**, `ValidateNestedWriteEligibility`, modelled on `ValidateAPIWalkerCompleteness` (`cmd/sqlgen/gen/api_walker.go:31`). Codegen hard-errors when an edge is listed in config but fails an eligibility rule (§4.2). | The repo already has this pattern and it is cheap. An explicitly-requested edge that silently generates nothing is worse than a build failure — see the `clientOnlyOperationFields` precedent at `config.go:750-754`, which rejects rather than ignores a knob the user deliberately turned. |
| **NW-D13** | **`connect` verifies target visibility before writing the junction row** — one PK-IN `GetMany` through the *target's own client*, asserting every requested ID came back. A miss is `ErrNotFound` naming the ID. | **This is a security decision, not ergonomics.** The junction FK constraint only proves a row *exists*; it says nothing about whether the caller may see it. Without the check, `connect` on a tenanted target is (a) a **cross-tenant existence oracle** — a foreign-tenant ID succeeds while a nonexistent ID fails with `BAD_REFERENCE`, so the caller can probe the ID space of other tenants — and (b) a **silent no-op**, because the junction row is written but **C9**'s loader filters the target out on every subsequent read. Routing the check through the target's client makes it correct for free: that client already applies the tenant filter, the soft-delete default and the §32 access rules. Cost: one PK-only read per `connect` edge. **Scope, measured in §13:** this is a validation of caller input at call time, not a durable invariant — it closes the oracle and sharpens the error, and it does not promise the target is still visible at commit. |
| **NW-D14** | **Dedupe target IDs before building junction rows.** | The junction's PK *is* the pair, so "connect category 7 twice" is a set operation whose answer is "connected", not a `CONFLICT`. Deduping also covers the `create` ∪ `connect` overlap. **Amended by A1** — deduping the request against itself is not enough on the update side. |

### 3.2 Amendments to the create-half decisions

| # | The create half said | Amended to | Why |
|---|---|---|---|
| **A1** | **NW-D10 / NW-D14** — junction rows go through `CreateMany`; dedupe target IDs within the request. | Junction rows go through **`UpsertMany`** (new, **F4**); dedupe *and* tolerate rows already present. | **F6** — `CreateMany` errors on an existing link. On create this is nearly unreachable (the parent is brand new); on update it is the common case. |
| **A2** | *"No new runtime primitive — this is composition of shipped operations."* (§8 Blast radius) | True for the create half; **false for the update half.** `UpsertMany` is a genuine addition. | **F4** |
| **A3** | **NW-D7** — `connect` on O2M is deferred because it *"steals rows from another parent"*. | `connect` on O2M **ships**, restricted to **adopting unparented rows** (`fk IS NULL`); re-parenting stays opt-in per edge. | The user's ask names it directly. Restricting to adopt-only removes the row-stealing hazard that motivated the deferral (**D7** below). |

Everything else — **C1–C9**, **NW-D1–NW-D6**, **NW-D8–NW-D14** — is adopted unchanged.

### 3.3 New decisions

| # | Decision | Rationale |
|---|---|---|
| **D1** | **One verb vocabulary across all three mutations: `create`, `connect`, `disconnect`, `clear`.** Not `add`/`addOrCreate`/`unlink`/`set`. *(Amended 2026-09-11: the unlink verb was `remove`; `clear` is new — see **D17**.)* | The user's `AddContacts` is spelled as one nested block carrying **both** `create` and `connect` — "add" is the block, not a verb. Keeping the verb names identical across create/update/upsert is what makes **D3** possible. **The spellings were checked against the three tools that have this surface** (`docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §2.1): `create` + `connect` is Prisma's, and Prisma's inverse is **`disconnect`**, not `remove`. `remove` came from ent, whose pair is `add`/`remove` and which has **no** `connect` — so `connect` + `remove` was a mix matching neither tool. It also made `remove` ambiguous between *unlink* and *destroy*, which is why **D9** had to spend a decision cell disclaiming it; `disconnect` is unambiguously the inverse of `connect` and needs no disclaimer. |
| **D2** | **Three separate methods, not new arguments on the existing three.** `CreateWithRelated`, `UpdateWithRelated`, `UpsertWithRelated`. | **NW-D2** (template branching) and **C3** (`Create<T>Input` is shared with `Upsert`, so nested members added there would be advertised and silently dropped). Both still hold. |
| **D3** | **`UpsertWithRelated` reuses the *update* nested block verbatim, and needs no insert-vs-update branch detection.** | Every verb in **D1** is an idempotent set operation: `connect`/`create` on a fresh parent = the create-side behavior; `disconnect` on a fresh parent matches zero rows and is a no-op (**probe C/D**). So one block is correct on both branches. This is what makes "upsert = combination of both" fall out for free instead of requiring a dialect-specific `xmax`-style probe. **Amended by FIX-234 (2026-10-01):** `create` is not idempotent. A repeated upsert carrying it inserts again, and on a has-one that already holds a child it is refused with `ErrAlreadyRelated` by the occupancy read (PRD §9.9.3, §9.9.5). One block is still correct on both branches, since each verb means the same thing on either. |
| **D4** | **The create-side block is a *distinct, narrower type* from the update-side block.** `<Parent><Edge>CreateNested` has `create` + `connect`; `<Parent><Edge>UpdateNested` adds `disconnect`. | Fail-closed against the F2 class **C3** names ("schema accepts, executor drops"). A `disconnect` on `CreateWithRelated` is meaningless, so it must not be expressible — not accepted-and-ignored. |
| **D5** | **Add `UpsertMany` to the generated surface as its own prerequisite ticket**, gated on a new `operations.upsert_many` flag. Junction `add` routes through it, with `Upsert`'s signature — `UpsertMany(ctx, inputs, target, opts…)` (**Q7**). **Confirmed in scope 2026-09-11** after being cut and restored the same day: the alternative — one `CreateMany` for links to freshly created targets plus the junction's single-row `Upsert` per connected target — works and was compiled, but costs one statement per connected target and one `PublishBatch` per link, and `UpsertMany` is wanted on its own merits. The measurements taken while it was out of scope (`docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §6) are **not** reasons against it; they are the ticket's scope, and Ticket D must cover all four. | **F4/F6/F7.** The alternatives are worse: a bespoke batched INSERT inside the nested template violates **D10** (bypasses the junction's hooks, events and cache invalidation, creating a silently divergent second write path); read-then-diff is two statements *and* racy without a lock; N single `Upsert` calls breaks §25.1. `UpsertMany` is independently useful and small — the conflict clause is already dialect-correct (**F7**). |
| **D6** | **`disconnect` performs no visibility read; `connect` does.** | Measured asymmetry, not a stylistic choice. **Probe C/D**: a `disconnect` is keyed on the parent's own PK — the junction pair embeds it, the O2M filter carries `fk = parent` — so it is structurally incapable of touching another parent's rows, and a miss is a no-op. A `connect` is keyed on a **caller-supplied target id** with no such anchor, so without **NW-D13** it is a cross-tenant existence oracle. This holds on **both** shapes: an O2M `connect` reads too, because the `UPDATE`'s row count alone cannot separate *no such row* from *already parented* — see **D7**. |
| **D7** | **O2M `connect` adopts only unparented rows, and reports three outcomes, not two.** The guard filter carries `fk IS NULL`, preceded by a visibility read (**D6**) whose result decides which error the caller gets: *not visible* → **`ErrNotFound`** naming the id (never `CONFLICT` — **P13**); *already parented to a different row* → **`ErrAlreadyRelated`** naming the id (**D15**); *already parented to this parent* → **no-op**, because the requested end state holds. Re-parenting is opt-in per edge via `allow_reparent: true`. | Closes **A3** safely. "Move this row from that parent to me" is a different operation with its own authorization question (PRD §29 says nothing about it), and it is the operation that made the create half defer the verb (**NW-D7**). Adopt-only has no such question: an orphan belongs to nobody. The three-way split is not decoration: collapsing it to `len(updated) != len(requested) → ErrAlreadyRelated` reports a typo'd or cross-tenant id as `CONFLICT / already related`, asserting a relationship that does not exist, and disagrees with M2M `connect`, which maps the identical caller mistake to `NOT_FOUND` (§4.4). The same-parent no-op is what keeps every verb idempotent, which **D3** depends on — M2M `connect` is already idempotent through `UpsertMany`, and an O2M `connect` that errored on a row it had itself just adopted would make `UpsertWithRelated` non-repeatable. |
| **D8** | **A NOT NULL O2M/O2O edge is `create`-only.** No `connect` (nothing is ever unparented, so **D7** can never match), no `disconnect` (would violate the constraint). | Falls out of **D7** plus §1.3 with no special-casing, and the result matches intuition: you never "add an existing order item to a product" or "detach an order from its user". Trying to express either is a schema-modelling error, and the generator declining to emit the field says so at compile time. |
| **D9** | **No `delete` verb in v1.** `disconnect` means *unlink*, never *destroy*. On O2M and O2O the child row is never touched at all — `disconnect` is an FK null-out and nothing else. The **only** row a `disconnect` ever deletes is a junction link, and only when that junction is not soft-deletable (**E11**); a link row carries no data of its own, so deleting it destroys nothing beyond the association the caller asked to drop. | Conflating them is exactly the silent-data-loss failure that gets someone paged, and the distinction is invisible at a GraphQL call site. `delete` also opens orphan handling, referential actions, and soft-delete interaction (`orders` has `deleted_at`, `order_items` does not — so one edge would soft-delete and its sibling hard-delete). The alternative is one line: `client.Orders().SoftDelete(ctx, id)` inside the same `WithTx`. |
| **D10** | **Add the skip-refetch escape to single-row `Update`** (**F1**) as a standalone prerequisite ticket. | Three lines, matching five sibling templates. ~~Valuable on its own: `Update` with an empty `FieldOptions` currently reads a row nobody asked for.~~ **Corrected 2026-09-17 during 27.3 — see F1.** It reads nothing; it returns **`ErrNotFound`** for a write that committed, because `Get`'s own all-false short-circuit yields an empty result that `Get` maps to not-found. The escape is therefore a correctness fix, not an optimisation, and `UpdateWithRelated` was blocked on it rather than merely taxed by it. |
| **D11** | **Fix **F5** as a standalone `/fix` before this feature, not inside it. ✅ Landed — FIX-196, resolved 2026-09-10.** The shipped mechanism is wider than this decision first proposed: `UpsertClause` took an options struct carrying `ResolvePKColumn`; MySQL opens its set list with `<pk> = LAST_INSERT_ID(<pk>)`, and the `RETURNING` dialects gained a `resolveUpsertConflictRow` fallback for the `DO NOTHING` branch, which no prepended set-list entry can express. | It was a live bug on shipped code — wrong entity returned, wrong cache key, wrong event PK — and it deserved its own FIX and regression tests rather than arriving as a side effect. Deciding it separately is also what surfaced the two-dialect half this document's **F5** had missed; folded into the nested work it would have been inherited silently. |
| **D12** | **Query-count contract, extending NW-D11: one write statement per participating table, per verb — *per batch*.** *(Amended 2026-09-11. The original read "one statement per verb" flat, which is **false for `create` against shipped code**: `CreateMany` splits its inputs at `c.batchSize`, default **200** — `create.go.tmpl:261-262`, `models_gen.go:133` — so 500 nested children is 3 statements. An acceptance test asserting a flat 1 would have failed on day one.)* The bound is `ceil(N / batchSize)` for **every id-list-bearing verb** — `create`, `connect`, `disconnect` and the `connect` visibility read alike — and **1** for `clear`, which carries no id list at all. *(Amended again 2026-09-16, when **Q9** was resolved by measurement: §2.6 shows SQLite rejecting an `IN` list at 32766 parameters, half of PostgreSQL's and MySQL's 65535, with an error naming no table, column, edge or verb. A flat `1` is unachievable above that, so the choice was between a dialect-dependent cliff and a uniform batch; the batch wins, and it makes `clear`'s genuine 1 the exception worth stating rather than the rule everything pretends to follow.)* A three-verb M2M block on a batch-sized input is 3 statements (`UpsertMany` targets, `UpsertMany` junction, `HardDeleteMany` junction), not 3 × N — and `ceil(N / batchSize)` of each once N exceeds the batch. `clear` costs one statement and **replaces** `disconnect`'s rather than adding to it — the two are mutually exclusive (**D17**). *(Amended 2026-09-22 by 27.9a: the bound counts **write** statements. On MySQL, which has no `RETURNING`, every `*Where`-routed verb — the O2M `connect` adoption, O2M `disconnect`, and `clear` — is preceded by a pre-SELECT. So `clear` is at most one write plus one read, two statements in all, as measured with server session counters (MySQL skips the write when the read finds nothing). PRD §9.9.7 and §25.1 state it.)* | Makes N+1 a test-detectable regression. Stated per *verb* rather than per *table* because `disconnect` and `connect` on one edge genuinely cannot merge into one statement — one is a DELETE, the other an INSERT. |
| **D13** | **Naming the same target under two verbs on one edge is a validation error**, returned before any statement runs. Concretely: `connect` ∩ `disconnect`, and `create` ∩ `disconnect` where a `create` entry carries an explicit PK. | The order would decide the outcome, and there is no defensible default. Refusing is unambiguous and costs one set intersection. Stated this way because the obvious spelling — "`create` ∩ `disconnect` is empty" — is **vacuous**: a nested `create` normally mints a new row and carries no target id, so the intersection is empty by construction. The case that actually bites is `connect` ∩ `disconnect`; the `create` half only bites when the caller supplies `ID:` on the create input, which is expressible (`CreateUserEventInput.ID` is an `omittable.Value[uuid.UUID]`). |
| **D15** | **One structured error — `*database.NestedMutationError{Edge, Verb, ID}` — that `Unwrap`s to two new sentinels: `database.ErrAlreadyRelated` (**D7**) and `database.ErrNestedVerbConflict` (**D13**). Two `errors.Is` arms in `mapErrorToGQL`.** *(This decision was first recorded as flat sentinels only. Resolved to the structured form per **Q8**, which it now supersedes.)* | Neither failure has an existing sentinel. `database/errors.go:11-46` has `ErrNotFound`, `ErrEmptyFilter`, `ErrAmbiguousFilter`, `ErrInvalidCursor`, `ErrDeadlock`, `ErrConnectionFailed`, `ErrConstraintViolation`, `ErrRefreshConcurrentlyInTx` — and no general "invalid input". GraphQL's `CONFLICT` is reachable **only** through a `*database.ConstraintError` with `ConstraintUnique` (`templates/api/errors.go.tmpl:36-42`), so fabricating one to reach `CONFLICT` would make `errors.As(err, &ce)` report a violation that never fired and put a fake constraint name in `ce`. The sentinels are what the `errors.Is` arms match; the struct carries the two facts §4.4 already commits to putting in the message, so `mapErrorToGQL` can read the edge off the error instead of parsing it back out of a wrapped string — which is what makes **NW-Q4** (`extensions.path: ["categories"]`) answerable at all. `Unwrap` keeps `errors.Is(err, database.ErrAlreadyRelated)` working, the way `ConstraintError` unwraps to `ErrConstraintViolation` (`database/errors.go:93-94`). |

| **D16** | **`UpsertMany` carries a new `hook.MutationOp`; the three `…WithRelated` methods do not.** `OpUpsertMany` is added to the runtime `hook` module, with matching arms in the cache invalidation switch, `mapOpToAction` (→ `event.Upsert`, **not** a new wire action) and the §32.3 redaction switch. | **F9**. `UpsertMany` is a genuine new write path — it reaches the database directly, so it needs its own op or it silently escapes cache invalidation and event mapping. A `…WithRelated` method is the opposite: it writes nothing itself, and every inner call already fires its own chain under its own op. Giving it an op too would double-count every nested mutation in hooks, events and cache invalidation. Mapping `OpUpsertMany` to the existing `event.Upsert` action rather than a new one keeps every subscriber filtering on `event.Upsert` working unchanged. |

| **D17** | **A `clear` verb: one `bool` per edge, not a list.** It unlinks **every** currently-linked row and runs **first**, before `create` and `connect`. When `clear` is true, `disconnect` on the same edge is **ignored**, not an error. Eligibility is identical to `disconnect` (**E8**, **E9**, **E11**) — it is the same statement with the id list dropped. | ent has it (`clearGroups`, `clearChildren`) and it was the one verb in that comparison sqlgen lacked (`docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §2.1). Three things make it worth adding rather than leaving to the caller. **It is strictly cheaper than the enumerated form**: `UPDATE target SET fk = NULL WHERE fk = parent` on O2M, `HardDeleteWhere(local_fk = parent)` on the junction — one statement, no id list, **no read**, and parent-scoped by construction, so **D6**'s "a disconnect is structurally incapable of touching another parent's rows" holds unchanged. **It makes `set` expressible without shipping `set`**: `clear` + `create`/`connect` is exactly replace-the-set, and §10 rejected `set` because it needs a read plus a lock and mass-unlinks from a partially-populated input — running `clear` first gets the same end state with the caller spelling out both halves, so a half-built input cannot silently wipe a set. **A bool is the honest type**: the verb takes no arguments, and a list-shaped API for "all of them" invites `clear: []` meaning something. Ignoring `disconnect` under `clear` is not the **C3** accept-and-drop class: every row `disconnect` names is already unlinked, so the caller's requested end state *holds* — the same reasoning as **D7**'s already-ours no-op and **Q2**'s silent-miss convention. |

| **D18** | **A `discriminator:` edge owns its discriminator column on the write side, exactly as it owns the traversed FK.** Three rules, all new — **NW-D6** specified only that *reads* are unchanged. **(a)** A nested `create` **SETs** the discriminator column to the edge's declared value. **(b)** The discriminator column is **elided from the nested child input**, the same way **C2** elides the traversed FK — it is not the caller's to supply. **(c)** A `connect`'s visibility read carries the discriminator as an extra predicate, so a target that does not already match the edge's value is `ErrNotFound` rather than a silent mislink. | Without **(b)** a caller nesting under `Asset.Attachments` can set `entity_type: 'spv'` and write a row the edge's own loader will never return; without **(c)** they can `connect` one. Both are the **C9**/**F10** silent write-then-invisible shape one level up — the row exists, the write succeeded, and the relationship reads back empty. Eliding rather than validating is the **C2** argument verbatim: a value that is always overwritten should not be expressible, because on GraphQL a non-null field forces the client to invent one. **(c)** costs nothing extra — the `connect` visibility read already exists (**NW-D13**/**D6**) and this only widens its filter. The decision spans Tickets **B** and **E**, which is why neither owned it. |
| **D19** | **A parent with zero eligible edges gets no `…WithRelated` methods at all** — no Go method, no input type, no GraphQL mutation. | **NW-Q2** settled auto-include *per edge* and never ruled on the empty table. `Profile` is the live case: its only relationship is a belongs-to, deferred by **NW-D5**, so `CreateProfileWithRelatedInput` would carry exactly one member — `Profile CreateProfileInput` — and the method would be `Create` with extra steps. It would also add a GraphQL mutation that expresses nothing the flat one cannot, which is surface area with negative value. |
| **D20** | **An operations-mask contradiction is a hard config error, not a silent disable.** `create_with_related: true` with `create: false` is rejected under `sqlgen validate`, and likewise for update and upsert. | The nested method **composes** its base operation — it calls `c.Create` / `c.Update` / `c.Upsert` directly (§5.3) — so without the base there is no method to call and the template would not compile. Failing at validate rather than at `go build` names the config key that is wrong. Silently disabling is the **F2**/**C3** accept-and-drop class: the user set a flag and got nothing, with no signal. **P7** grows this rule, and `ExpandPreset`'s five arms (§7) must be consistent with it — which is exactly the check that catches a preset arm left un-updated. |
| **D21** | **A nil `FieldOptions` returns the parent with its relationship members unpopulated**, even on the edges the call just wrote. | Falls out of §4.3 step 3: the terminal read is gated on the caller having *selected* a relationship, and a nil selection selects none. It is consistent with the flat path — `Get` with nil `FieldOptions` loads columns and never relationships (PRD §27.6) — and it is what keeps the no-selection case at zero extra reads. Stated explicitly because "nil means everything" is the right mental model for *columns* and the wrong one here, and the surprise lands precisely on the caller who just created those rows and expects to see them back. |

> **D14 is deliberately unused.** The number was skipped while the create half was a separate
> document with its own D14 (now **NW-D14**, §3.1). The gap is kept rather than closed so that a
> citation written against either numbering still resolves to exactly one decision.

---

## 4. Design — the verb algebra

### 4.1 The eligibility matrix

> **Census annotated 2026-09-18 by `/verify 27.8`, not rewritten** — this table is a historical record of what the design knew. Two rows are stale against the shipped fixture. `assets.Attachments` / `.Invoices` / `.PrimaryDocument` are listed as emitting nothing pending a migration to `discriminator:`; **27.2 performed that migration**, and all three now emit `create` (their FK, `documents.entity_id`, is NOT NULL, so **E8** leaves them create-only). And **27.8 added a nineteenth edge**, `workspace_notes.DraftChildren` — the only fixture edge that is polymorphic *and* over a nullable FK, which is what makes **D18(c)** reachable at all. The emitted sets match this matrix on all nineteen; the count and those three rows are what moved.


This table *is* the design. Everything in §5 is mechanical once it is fixed.

| Edge shape | FK | `create` | `connect` | `disconnect` | `clear` |
|---|---|---|---|---|---|
| **O2M / has-one, nullable FK** | on target, NULL | ✅ INSERT with `fk = parent` | ✅ visibility read, then `UPDATE … SET fk = parent WHERE id IN (…) AND fk IS NULL` (**D7**) | ✅ `UPDATE … SET fk = NULL WHERE id IN (…) AND fk = parent` | ✅ `UPDATE … SET fk = NULL WHERE fk = parent` — no id list, no read (**D17**) |
| **O2M / has-one, NOT NULL FK** | on target, NOT NULL | ✅ INSERT with `fk = parent` | ❌ **D8** — nothing is ever unparented | ❌ **D8** — would violate the constraint | ❌ **D8** — same constraint |
| **M2M** | junction | ✅ INSERT target, then junction row | ✅ visibility read (**NW-D13**), then junction row | ✅ `HardDeleteMany` on junction PKs — **only when the junction has no soft-delete column** (**E11**) | ✅ `HardDeleteWhere(local_fk = parent)` — same **E11** gate (**D17**) |
| **belongs-to (O2O, FK on parent)** | on parent | ❌ deferred (**NW-D5**) | ❌ redundant — this *is* the scalar FK field | ❌ redundant — set the FK to NULL via the ordinary update input | ❌ redundant — same |

The shape table above has four rows; the fixture has **five distinct structural cases**, because a
self-referential edge is an O2M whose target table *is* the parent table. It gets no row of its own
— every verb behaves identically — but it carries one hazard nothing else does (**E12**).

**Applied to the `graphql` fixture — all eighteen edges** (**A1**, **A2**). The design listed six
until 2026-09-16, and the omission was not cosmetic: three of the twelve missing edges are *more*
verb-complete than any edge it did list, and one is a shape the design never discussed.

| Parent | Edge | Shape | FK | Emitted verbs |
|---|---|---|---|---|
| `User` | `Events` | O2M | `events.user_id` **NULL** | `create`, `connect`, `disconnect`, `clear` |
| `User` | `Orders` | O2M | `orders.user_id` NOT NULL | `create` only (**D8**) |
| `User` | `Categories` | M2M | `user_categories` | `create`, `connect`, `disconnect`, `clear` |
| `Category` | `Users` | M2M | `user_categories` | `create`, `connect`, `disconnect`, `clear` |
| `Category` | `Products` | O2M | `products.category_id` NOT NULL | `create` only (**D8**) |
| `Order` | `OrderItems` | O2M | `order_items.order_id` NOT NULL | `create` only (**D8**) |
| `Product` | `OrderItems` | O2M | `order_items.product_id` NOT NULL | `create` only (**D8**) |
| `Asset` | `Documents` | M2M | `asset_document_links` | `create`, `connect`, `disconnect`, `clear` |
| `Document` | `Assets` | M2M | `asset_document_links` | `create`, `connect`, `disconnect`, `clear` |
| `WorkspaceNote` | `Children` | **O2M self-referential**, tenanted | `workspace_notes.parent_id` **NULL** | `create`, `connect`, `disconnect`, `clear` (**E12**) |
| `Profile` | `Users` | belongs-to | `profiles.user_id` on parent | none — use `profile.userID` (**NW-D5**) |
| `UserCredential` | `Users` | belongs-to, PK *is* the FK | `user_credentials.user_id` on parent | none — **NW-D5** (§1.3) |
| `Asset` | `Attachments` | O2M + `filter:` | `documents.entity_id` | none until migrated to `discriminator:` (**NW-D6**) |
| `Asset` | `Invoices` | O2M + `filter:` | `documents.entity_id` | none — same |
| `Asset` | `PrimaryDocument` | O2O has-one + `filter:` | `documents.entity_id` | none — same |
| `Asset` | `PhotoAttachments` | O2M + **uninvertible** `filter:` | `documents.entity_id` | none, permanently (**C4**) |
| `Asset` | `PrimaryActiveDocument` | O2O has-one + **uninvertible** `filter:` | `documents.entity_id` | none, permanently (**C4**) |
| `Asset` | `LinkedAttachments` | M2M + **uninvertible** `filter:` | `asset_document_links` | none, permanently (**C4**) |

Source: the 18 `db:"-"` relationship fields in `models_gen.go`, cross-referenced against
`schema.sql` FK nullability and `sqlgen.yml`.

**Three rows change the picture, and the first two are free test coverage.**

**`Asset.Documents` / `Document.Assets` are fully eligible today.** This table used to say `assets`
emits nothing "until migrated to `discriminator:`". That is true of the six config-declared,
filtered edges and **false of the seventh**: `asset_document_links` carries real FKs to both
`assets` and `documents` (`schema.sql:178-184`), so the parser auto-detects a plain M2M in each
direction with no filter, and both pass every eligibility rule unchanged. These are the **only M2M
edges in any fixture with a UUID-PK target** — `User.Categories` has an `int64` PK — so they cover
the PK-type axis at no cost. Tickets E and F must use them.

**`WorkspaceNote.Children` is the most verb-complete edge in the fixture**, and the design never
mentioned its shape. It is self-referential (`workspace_notes.parent_id REFERENCES
workspace_notes(id)`, `schema.sql:331`), on a **nullable** FK so all four verbs are eligible, on a
**tenanted** table (§29.2.3 auto-detected), and its client already holds the self-reference it
needs — `workspaceNoteClient.workspaceNoteClient` (`models_gen.go:30894`), wired at
`client_gen.go:193`. So it needs **no new client field**, which makes it the cheapest edge to test
and the one that exercises tenancy propagation into nested children.

**`UserCredential.Users` is the eighteenth edge**, added by `3509180` after this section was
written. Belongs-to, so ineligible — see §1.3 for the note it pins.

That `User.Orders` and `User.Events` — same parent, same relationship type — get different verb
sets is the point of §1.3, and it is why the verb set must be computed per edge from
`RelationshipContext.FKNullable` rather than from `RelationshipContext.Type`.

### 4.2 Eligibility rules

An edge is **nestable** when every rule holds. `ValidateNestedWriteEligibility` (**NW-D12**)
hard-errors on an explicitly-listed edge that fails one, and silently omits an auto-included edge
that fails one. **E1–E7** come from the create half; **E8–E10** are new here.

| # | Rule | Why |
|---|---|---|
| **E1** | The edge is shape 2, 3 or 4 (§1.3) — the parent's PK flows *down* | **NW-D5** |
| **E2** | `filter:` is empty (`discriminator:` is permitted, and required for a polymorphic edge) | **NW-D6**, **C4** |
| **E3** | The parent has a single-column primary key | Inherited from the create half, where the stated reason was "a composite-PK parent cannot be the referenced side of a detected edge". **That reason is too strong** — `ApplyConstraintToColumns` populates `FKReference` for any single-column FK (`parser/schema.go:409-437`) and `addO2M` sources the edge from whatever table it references (`parser/relationship.go:191-202`), so a composite-PK table *is* referenceable through a unique non-PK column. The rule still holds for v1 on the narrower ground that **no example schema exercises it** — every FK in every fixture references `(id)` — and both composite-PK fixtures emit zero relationship members (§5.6). Lifting it is mechanical: §5.6 shows the two deltas (PK-struct argument, referenced-column FK assignment) and there is no third |
| **E4** | The target table has `create` (and `create_many` for shapes 3/4) enabled in its `operations` mask | **NW-D10** — no client method, no nested write |
| **E5** | For M2M: the junction has no required column beyond its two FKs *(Amended 2026-09-22 by 27.9a: no column beyond its two FKs, **optional ones included**. Measured on the `mysql` example, re-connecting an already-linked pair reset `user_categories.slot` from 7 to NULL and returned nil. The link step upserts on the junction's key, and `UpsertMany` puts every other insert column in the update half. The key, tenant and soft-delete columns are exempt: the first two are never in that half, and resetting the third restores the link. PRD §9.9.4 carries the amended rule.)* | otherwise `connect` cannot populate it; see §12 **NW-Q3** |
| **E6** | The target's create input is non-empty after the §32 access projection | a write surface into a table whose columns are all `hidden` / `read_only` writes nothing |
| **E7** | *(GraphQL only)* the target is not `api.enabled: false`, and neither is any column the nested input would expose | the write-side mirror of §26.5.3's read side-channel rule: a nested member into a hidden entity is a door into an entity the schema deliberately closed |
| **E8** | `disconnect`, `clear` and `connect` require `FKNullable` on O2M/O2O edges | **D8**, **D17** — `clear` is `disconnect` with the id list dropped, so it carries the identical gate |
| **E9** | `connect` and `disconnect` on M2M require `operations.upsert_many` and `operations.hard_delete` on the junction | **D5**, **F3** — no client method, no verb |
| **E11** | M2M `disconnect` **and `clear`** require the junction to have **no soft-delete column** | **F10**. With one, neither mechanism is correct: `HardDeleteMany` destroys a row the schema marked soft-deletable, and `SoftDeleteMany` leaves the link visible, because the read loader's junction query carries no soft-delete predicate. Refusing the verb is the fail-closed direction, and it costs nothing today — no fixture junction has the column. `create` and `connect` on such an edge are unaffected |
| **E12** | On a **self-referential** edge, a `connect` naming the parent's own id is refused at validation — `ErrNestedVerbConflict` (**D15**) — and so is a `disconnect` naming it | **A2**. `WorkspaceNote.Children` is the live case. Neither existing guard catches it: **D7** sees an adoptable orphan, because the parent's own `parent_id` may legitimately be NULL, and **D13** sees one verb naming one target. Left unguarded, `connect: [self]` writes `parent_id = id` — a row that is its own parent, which every recursive read walks forever. Refusing at validation costs one comparison and is the only check depth-1 can make: `A.connect(B)` where `B` is `A`'s **ancestor** closes a longer cycle that no depth-1 check can see (§10) |
| **E10** | `UpsertWithRelated` requires the parent's `operations.upsert` **and the existence of a conflict-target constant to pass it**. The **F5** gate is **satisfied** — FIX-196 landed on all three dialects — so no dialect-specific restriction is needed | **D11**. Before FIX-196 the parent PK every nested write keys on could come back `0` (MySQL) or unresolvable (`RETURNING` dialects). **The second clause is new (2026-09-16).** `336f591` gated `buildConflictTargets` on schema evidence of an index — a set-equal PRIMARY KEY constraint, a set-equal non-partial UNIQUE, or an inline single-column UNIQUE — so a table whose uniqueness is app-enforced through `primary_key.columns` now emits **no `<T>ConflictPK` constant at all**. `upsert<T>WithRelated` takes one as an argument (**Q4**, **Q7**, **P23**), so without the constant the emitted method would not compile. The guard fails closed deliberately, and this rule inherits that: no conflict target, no nested upsert surface |

Both halves of that lint are load-bearing: an explicitly-requested edge that silently generates
nothing is worse than a build failure, and an auto-included edge that fails a rule must not break
an unrelated build. It runs under **`sqlgen validate` as well as `sqlgen generate`**, the way
25.2 wired its sibling lint into `buildOneAPITable` / `buildOneAPIView` — a config error should
surface without writing files.

### 4.3 Execution order

Parent first in all three cases (**NW-D5** — v1 nests only where the parent's PK
flows down), so there is no topological sort and no cycle.

```
BEGIN (or SAVEPOINT when ctx already holds a transaction — PRD §18.3)

  1. parent := <Create|Update|Upsert>(ctx, …, FieldOptions with relationships stripped)
        Relationship members MUST be stripped: loading them here would run before the
        nested rows exist and return a stale set.

  2. per eligible edge, in declared order:
       validate      no target named under two verbs                 (D13)
                     (connect ∩ disconnect is not checked when clear is on:
                      disconnect is ignored, so it cannot contradict anything)
       clear    →  FIRST, and the ordering is load-bearing — running it after
                   create/connect would wipe what they had just linked (D17)
                   O2M: <Target>().UpdateWhere(fk = parent, fk = NULL)    -- exactly 1 stmt
                   M2M: <Junction>().HardDeleteWhere(local_fk = parent)   -- exactly 1 stmt
       create   →  <Target>().CreateMany(children with fk = parent)   -- ceil(N/batchSize) stmts
                   (M2M: CreateMany targets, PK-only FieldOptions)
       connect  →  both: <Target>().GetMany(id IN …, Limit: new(0))            -- ceil(N/batchSize) reads
                         visibility check (NW-D13); `Limit: new(0)` is NORMATIVE (A10)
                         any requested id absent -> ErrNotFound naming it
                   O2M: partition the visible rows by their current fk (D7)
                         fk = parent -> no-op;  fk = other -> ErrAlreadyRelated naming it
                         fk IS NULL  -> <Target>().UpdateWhere(id IN … AND fk IS NULL,
                                          fk = parent)                             -- ceil(N/batchSize) stmts
       link     →  M2M only: <Junction>().UpsertMany(dedup(created ∪ connected))  -- ceil(N/batchSize) stmts
       disconnect → SKIPPED ENTIRELY when clear ran (D17): every id it names is
                   already unlinked, so the requested end state holds
                   O2M: <Target>().UpdateWhere(id IN … AND fk = parent, fk = NULL) -- ceil(N/batchSize) stmts
                   M2M: <Junction>().HardDeleteMany(pairs)                         -- ceil(N/batchSize) stmts

  3. IF the caller's FieldOptions selects any relationship:
       parent = Get(ctx, parent.PK, caller's FieldOptions)            -- PRD §25.1 read

COMMIT (or RELEASE SAVEPOINT)
```

**Two spellings in that plan are load-bearing and neither is self-evident from reading it.**

**`Limit: new(0)` on the visibility read is normative, not decoration (A10).** `get.go.tmpl:229-235`
reads a `*Limit` of `0` as *"emit no LIMIT clause"* **and** as *"skip the client's default
`queryLimit`"*. The obvious `Limit: nil` does the opposite: it lets `queryLimit` truncate the read,
so a `connect` naming more targets than the client's page size would find the tail of its own list
absent and report real, visible rows as `NOT_FOUND`. The failure scales with the caller's input
size, which is the shape that passes every small test and fails in production.

**The unlink assignment must use the `Set`-to-nil form, never the zero `omittable.Value` (F11).**
`omittable.Set[*uuid.UUID](nil)` sets the column NULL; `omittable.Value[*uuid.UUID]{}` omits it
from the `SET` list entirely. Since Phase 26 bound nullable UUIDs to `*uuid.UUID`, **the compiler
accepts both in the same position** — so `disconnect` and `clear` written the wrong way match
their rows, report success and unlink nothing. §2.5 measures it: that mutation is the one member
of the failing-first set that no longer fails. Ticket F pins it at runtime.

**Soft-deleted children follow from routing through the target's own client (NW-D10), and are
worth stating because nothing else does.** A `connect` whose target is soft-deleted is
`ErrNotFound` — the visibility read applies the target's soft-delete default, so it is
indistinguishable from a target that does not exist, which is the fail-closed direction. A
`disconnect` naming a soft-deleted child matches nothing, because `UpdateWhere` applies the same
default: the child keeps its FK and stays linked. Under **Q2** that is a silent no-op, which is
the one place the silence hides something a caller might care about. Accepted for v1 — a
soft-deleted child is already absent from every read of the relationship, so the link is
unobservable either way — but it is the case to revisit if a strict mode is ever asked for.

**Contract (D12): `ceil(N / batchSize)` write statements per participating table per verb, plus a
bounded set of reads — never a per-row read.** `clear` is the one verb that is genuinely always a
single statement, because it names no ids (**D17**). The batching is not a performance choice: §2.6
measures SQLite rejecting an `IN` list at 32766 bind parameters, and every dialect's error for that
names nothing the caller or §4.4 can attribute (**Q9**). Each read earns its place:

| Read | When | Why unavoidable |
|---|---|---|
| parent PK resolution | always | children key on `parent.PK`, and `Create`/`Upsert` surface a generated PK only through the return value — `m.AffectedPKs` is on the inner `MutationContext` |
| M2M target PK-only | per edge with a non-empty `create` | junction rows need the generated target PKs |
| connect visibility | per edge with a non-empty `connect`, O2M and M2M alike | **NW-D13** — the FK proves existence, not visibility. On O2M it earns a second keep: it is the only thing that separates *absent* from *already parented*, which the `UPDATE`'s row count cannot (**D7**) |
| terminal re-read | only when the caller selected a relationship | step 1 stripped them |

Everything that does not need its result back passes a non-nil, nothing-selected `FieldOptions`
and takes the skip-refetch branch (**C6**) — which is why **D10** matters: today
single-row `Update` cannot take it (**F1**).

Free consequences of routing through the shipped operations (**NW-D10**): children
inherit the tenant from the ctx-cached resolver (PRD §29.6); each fires its own hook chain; cache
invalidation and events register as `OnCommit` callbacks and fire once after the **root** commit
in FIFO order (`database/transaction.go:110-133`).

### 4.4 Error attribution

Per edge and per verb, wrapping the existing PRD §22.3 convention:

```
update user with related: categories: connect: category 7: sqlgen: resource not found
update user with related: events: disconnect: <wrapped>
update user with related: categories: clear: <wrapped>
```

Attribution stays **per edge and verb, not per row** — a batched statement reports one violation
for the whole statement and the offending index is not recoverable from the driver
(§5.5). The wrapped error is untouched, so `errors.Is(err,
database.ErrConstraintViolation)` and the `*database.ConstraintError` dispatch in `mapErrorToGQL`
keep working: a child FK violation is still `BAD_REFERENCE`, a duplicate still `CONFLICT`. The
two new error mappings this design introduces:

| Condition | Error | GraphQL code |
|---|---|---|
| `connect` target not visible — **O2M and M2M alike** (**D7**) | `ErrNotFound` naming the id — exists today | `NOT_FOUND` — **never** `BAD_REFERENCE`, per **P13**, so the oracle is not reintroduced |
| `connect` target already parented to **another** row (O2M, **D7**) | **`ErrAlreadyRelated`** naming the id — new sentinel (**D15**) | `CONFLICT`, via a new `errors.Is` arm in `mapErrorToGQL` |
| `connect` target already parented to **this** parent (O2M, **D7**) | none — no-op | n/a; the requested end state already holds |
| `create` ∩ `disconnect` on one edge (**D13**) | **`ErrNestedVerbConflict`** — new sentinel (**D15**) | `INVALID_INPUT`, via a new arm |

---

## 5. Generated Go surface

### 5.1 Type families

Four per eligible parent. All names go through `resolved_names.go` (**C8**).

| Type | Shape |
|---|---|
| `Create<Parent>WithRelatedInput` | `Create<Parent>Input` + one optional `…CreateNested` block per edge |
| `Update<Parent>WithRelatedInput` | `Update<Parent>Input` + one optional `…UpdateNested` block per edge |
| `Upsert<Parent>WithRelatedInput` | `Create<Parent>Input` + the **same** `…UpdateNested` blocks (**D3**) |
| `<Parent><Edge>{Create,Update}Nested` | the per-edge verb block |
| ~~`Create<Parent><SingularEdge>Input`~~ → **`<Parent><Edge>CreateInput`** | the child's create input **minus the traversed FK** (**C2**), O2M/O2O only. **Respelled 2026-09-18 by 27.7**, which is the amendment PRD §9.9.5 now carries: the prefix form reconstructs the *target's own* `Create<T>Input` on any child table named `<parent>_<edge>`, and two of this repo's example schemas (`users.Profile` over `user_profiles`, `binary_keys.Events` over `binary_key_events`) failed codegen on it. **C8**'s claim below that "no fixture collides today" was wrong for the same reason — its census listed six pairs and missed both, which **A1** had already flagged. `gen.nestedChildInputName` is the single source; §5's compiled listings still show the old spelling and are a historical record. |

Keyed on the **edge**, not the target table — `assets` declares four separate edges into
`documents` that elide the same FK but differ in discriminator.

**The emitter produces one executor method per edge, called by all three families — not three
inlined copies (A11).** `func (c *<parent>Client) apply<Parent><Edge>UpdateNested(ctx, parent,
nested, options) error`, with the create-side families passing a block narrowed to the verbs they
allow (§5.5). This is what makes **D3** — *"upsert reuses the update block verbatim"* — enforced by
the compiler instead of by review: there is one body, so the three families cannot drift. It also
makes the per-edge verb set a property of one function rather than a condition repeated in three
templates, which is **NW-D1**'s "one fact, one derivation" applied to the write side. The
companion's §3 listings are already written in this shape; §5.3 below shows the executor bodies
inline for readability, not as a second structure.

### 5.2 The input types, on the `graphql` fixture

```go
// --- generated ---

type UpdateUserWithRelatedInput struct {
    // User holds the parent's own columns. Identical to the input accepted by
    // Update — nested members never restate a column (NW-D4).
    User UpdateUserInput `json:"user"`

    // Events - one-to-many via events.user_id (NULLABLE → all three verbs).
    Events *UserEventsUpdateNested `json:"events,omitempty"`

    // Orders - one-to-many via orders.user_id (NOT NULL → create only, D8).
    Orders *UserOrdersUpdateNested `json:"orders,omitempty"`

    // Categories - many-to-many via user_categories.
    Categories *UserCategoriesUpdateNested `json:"categories,omitempty"`
}

type UserEventsUpdateNested struct {
    Create  []*CreateUserEventInput `json:"create,omitempty"`  // new rows, user_id = parent
    Connect []uuid.UUID             `json:"connect,omitempty"` // adopt unparented rows (D7)
    Disconnect []uuid.UUID            `json:"disconnect,omitempty"`  // set user_id = NULL
    Clear      bool                  `json:"clear,omitempty"`      // unlink ALL, runs FIRST (D17)
}

// No Connect, no Disconnect: orders.user_id is NOT NULL (D8).
type UserOrdersUpdateNested struct {
    Create []*CreateUserOrderInput `json:"create,omitempty"`
}

type UserCategoriesUpdateNested struct {
    Create  []*CreateCategoryInput `json:"create,omitempty"`  // no FK to elide on an M2M target
    Connect []int64                `json:"connect,omitempty"`
    Disconnect []int64                `json:"disconnect,omitempty"`  // delete junction rows
    Clear      bool                  `json:"clear,omitempty"`      // unlink ALL, runs FIRST (D17)
}

// CreateUserEventInput is CreateEventInput with user_id removed — that column
// is set from the parent's primary key.
type CreateUserEventInput struct {
    ID          omittable.Value[uuid.UUID]           `json:"id,omitzero"`
    Action      string                               `json:"action"`
    OccurredAt  types.DateTime                       `json:"occurred_at"`
    ProcessedAt omittable.Value[types.NullDateTime]  `json:"processed_at,omitzero"`
    Adjustment  omittable.Value[decimal.NullDecimal] `json:"adjustment,omitzero"`
    // …
}
```

The create-side block is narrower (**D4**):

```go
type UserCategoriesCreateNested struct {
    Create  []*CreateCategoryInput `json:"create,omitempty"`
    Connect []int64                `json:"connect,omitempty"`
    // no Remove — there is nothing to remove from a row that does not exist yet
}
```

And upsert reuses the update block (**D3**):

```go
type UpsertUserWithRelatedInput struct {
    User       CreateUserInput             `json:"user"`  // as Upsert takes today
    Events     *UserEventsUpdateNested     `json:"events,omitempty"`
    Orders     *UserOrdersUpdateNested     `json:"orders,omitempty"`
    Categories *UserCategoriesUpdateNested `json:"categories,omitempty"`
}
```

### 5.3 The emitted method

> **Verified, not sketched (§2.3).** This body compiled clean against
> `cmd/sqlgen/testdata/examples/graphql/models/` under `go build` + `go vet`, with three
> failing-first checks confirming the FK assignments are genuinely type-checked. Two
> substitutions were needed and both are **F8**: `database.CallbackAsync` stands in for the
> un-wired `c.callbackMode`, and the junction client is a package-level stand-in. `UpsertMany`
> is shown as **F4** intends it; the probe used `CreateMany` in its place, which is why **F6**
> is a finding rather than a compile error.
>
> **The D7-revised `connect` block has now been compiled (2026-09-16).** This note used to say it
> had been checked by inspection only and asked for a re-run before the emitter ticket; §2.5's pass
> did that, and the block builds and vets clean. One claim it made is **no longer true**:
> `Event.UserID` is `*uuid.UUID`, not `uuid.NullUUID` (`models_gen.go:7611`) — see **F11**, which
> is the reason the re-run was worth doing rather than a formality.

Field-copy blocks are elided with `…` where mechanical; everything structural is verbatim.

```go
// UpdateWithRelated updates a user together with rows on its nested
// relationships, in a single transaction.
//
// Nested rows are written through the target table's own client, so their
// hooks, events, cache invalidation and tenancy behave exactly as on a direct
// call. When ctx already carries a transaction this becomes a savepoint
// (PRD §18.3) rather than a new transaction.
func (c *userClient) UpdateWithRelated(ctx context.Context, id uuid.UUID, input *UpdateUserWithRelatedInput, opts ...func(*CallOptions[UserFieldOptions])) (*User, error) {
	options := resolveCallOptions(opts)

	var user *User
	err := database.WithTransaction(ctx, c.querier, "update user with related", func(ctx context.Context) error {
		// Parent first. Its own tenant check is what authorizes every nested
		// write below — each one is keyed on this PK.
		parent, err := c.Update(ctx, id, &input.User, func(o *CallOptions[UserFieldOptions]) {
			*o = options
			o.FieldOptions = userNestedParentFieldOptions(options.FieldOptions)
			o.LockMode = sql.LockNone
		})
		if err != nil {
			return err
		}
		user = parent

		// --- Events: O2M on a NULLABLE FK — all three verbs. ---
		if nested := input.Events; nested != nil {
			// clear — FIRST. Running it after create/connect would wipe the
			// rows they just linked; running it first is what makes
			// `clear + create` mean "replace the set" (D17). One UPDATE,
			// parent-scoped, no id list and no read.
			if nested.Clear {
				pid := parent.ID.String()
				if _, err := c.eventClient.UpdateWhere(ctx,
					&EventFilter{UserID: &comparator.NullableID{ID: comparator.ID{Eq: &pid}}},
					&UpdateEventInput{UserID: omittable.Set[*uuid.UUID](nil)}, // → NULL
					func(o *CallOptions[EventFieldOptions]) {
						nestedChildOptions(o, options)
						o.FieldOptions = &EventFieldOptions{}
					}); err != nil {
					return fmt.Errorf("events: clear: %w", err)
				}
			}

			if len(nested.Create) > 0 {
				inputs := make([]*CreateEventInput, 0, len(nested.Create))
				for _, n := range nested.Create {
					if n == nil {
						continue
					}
					inputs = append(inputs, &CreateEventInput{
						ID:     n.ID,
						UserID: omittable.Set(&parent.ID), // nullable FK
						Action: n.Action,
						// …mechanical field copies…
					})
				}
				if _, err := c.eventClient.CreateMany(ctx, inputs, func(o *CallOptions[EventFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &EventFieldOptions{} // C6: skip the re-fetch
				}); err != nil {
					return fmt.Errorf("events: create: %w", err)
				}
			}

			// connect — adopt unparented rows only (D7). A visibility read,
			// then at most one UPDATE. The read is not optional: the UPDATE's
			// row count alone cannot separate "no such event" from "already
			// parented", and reporting a typo'd id as CONFLICT would assert a
			// relationship that does not exist (D6).
			if len(nested.Connect) > 0 {
				ids := make([]string, 0, len(nested.Connect))
				for _, v := range nested.Connect {
					ids = append(ids, v.String())
				}
				visible, err := c.eventClient.GetMany(ctx, &GetEventsInput{
					Filter: &EventFilter{ID: &comparator.ID{In: ids}},
					Limit:  new(0),
				}, func(o *CallOptions[EventFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &EventFieldOptions{ID: true, UserID: true}
					o.LockMode = sql.LockNone
				})
				if err != nil {
					return fmt.Errorf("events: connect: %w", err)
				}
				owner := make(map[uuid.UUID]*uuid.UUID, len(visible))
				for _, row := range visible {
					owner[row.ID] = row.UserID
				}

				adopt := make([]string, 0, len(nested.Connect))
				for _, eid := range nested.Connect {
					cur, ok := owner[eid]
					switch {
					case !ok:
						// Absent, another tenant's, or soft-deleted — all
						// indistinguishable to this caller, and all NOT_FOUND.
						// Never CONFLICT: that would confirm the row exists.
						return fmt.Errorf("events: connect: event %v: %w", eid, ErrNotFound)
					case cur != nil && *cur == parent.ID:
						continue // already ours — the end state holds (D3)
					case cur != nil:
						return fmt.Errorf("events: connect: event %v: %w", eid, ErrAlreadyRelated)
					}
					adopt = append(adopt, eid.String())
				}

				if len(adopt) > 0 {
					// The `fk IS NULL` guard stays: it is what makes the
					// adoption atomic against a concurrent connect that
					// parented the row after the read above.
					adopted, err := c.eventClient.UpdateWhere(ctx,
						&EventFilter{
							ID:     &comparator.ID{In: adopt},
							UserID: &comparator.NullableID{Null: new(true)},
						},
						&UpdateEventInput{UserID: omittable.Set(&parent.ID)},
						func(o *CallOptions[EventFieldOptions]) {
							nestedChildOptions(o, options)
							o.FieldOptions = &EventFieldOptions{ID: true}
						})
					if err != nil {
						return fmt.Errorf("events: connect: %w", err)
					}
					if len(adopted) != len(adopt) {
						// Lost the race — parented between the read and the
						// UPDATE. Same outcome, reported the same way.
						return fmt.Errorf("events: connect: %w", ErrAlreadyRelated)
					}
				}
			}

			// remove — unlink, scoped to THIS parent. The `fk = parent` term is
			// what stops a caller detaching another parent's row by guessing
			// its id; §2.4 probe D measures both branches.
			// Ignored when clear ran (D17): every id it names is already
			// unlinked, so the requested end state holds. Silent rather than an
			// error, matching Q2's convention for a disconnect that writes
			// nothing.
			if !nested.Clear && len(nested.Disconnect) > 0 {
				ids := make([]string, 0, len(nested.Disconnect))
				for _, v := range nested.Disconnect {
					ids = append(ids, v.String())
				}
				pid := parent.ID.String()
				if _, err := c.eventClient.UpdateWhere(ctx,
					&EventFilter{
						ID:     &comparator.ID{In: ids},
						UserID: &comparator.NullableID{ID: comparator.ID{Eq: &pid}},
					},
					&UpdateEventInput{UserID: omittable.Set[*uuid.UUID](nil)}, // → NULL
					func(o *CallOptions[EventFieldOptions]) {
						nestedChildOptions(o, options)
						o.FieldOptions = &EventFieldOptions{}
					}); err != nil {
					return fmt.Errorf("events: disconnect: %w", err)
				}
			}
		}

		// --- Orders: O2M on a NOT NULL FK — create only (D8). ---
		if nested := input.Orders; nested != nil && len(nested.Create) > 0 {
			inputs := make([]*CreateOrderInput, 0, len(nested.Create))
			for _, n := range nested.Create {
				if n == nil {
					continue
				}
				inputs = append(inputs, &CreateOrderInput{
					ID:     n.ID,
					UserID: parent.ID, // NOT NULL FK, assigned bare
					// …
				})
			}
			if _, err := c.orderClient.CreateMany(ctx, inputs, func(o *CallOptions[OrderFieldOptions]) {
				nestedChildOptions(o, options)
				o.FieldOptions = &OrderFieldOptions{}
			}); err != nil {
				return fmt.Errorf("orders: create: %w", err)
			}
		}

		// --- Categories: M2M. ---
		if nested := input.Categories; nested != nil {
			// clear — FIRST, as above. HardDeleteWhere filtered to this
			// parent's local FK: one statement, no id list, no read, and the
			// filter IS the parent scope (D6/D17).
			if nested.Clear {
				pid := parent.ID.String()
				if err := c.userCategoryClient.HardDeleteWhere(ctx,
					&UserCategoryFilter{UserID: &comparator.ID{Eq: &pid}},
					func(o *CallOptions[UserCategoryFieldOptions]) {
						nestedChildOptions(o, options)
					}); err != nil {
					return fmt.Errorf("categories: clear: %w", err)
				}
			}

			targetIDs := make([]int64, 0, len(nested.Create)+len(nested.Connect))

			if len(nested.Create) > 0 {
				// No FK to elide on an M2M target — the parent's PK goes into
				// the junction, so this is CreateCategoryInput verbatim.
				created, err := c.categoryClient.CreateMany(ctx, nested.Create, func(o *CallOptions[CategoryFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &CategoryFieldOptions{ID: true} // PK-only
				})
				if err != nil {
					return fmt.Errorf("categories: create: %w", err)
				}
				for _, row := range created {
					targetIDs = append(targetIDs, row.ID)
				}
			}

			// NW-D13 — the junction FK proves existence, not
			// visibility. Reading through the target's OWN client applies its
			// tenant filter, soft-delete default and access rules, so a target
			// in another tenant is NOT_FOUND here rather than a junction row
			// nobody can ever read.
			if len(nested.Connect) > 0 {
				visible, err := c.categoryClient.GetMany(ctx, &GetCategoriesInput{
					Filter: &CategoryFilter{ID: &comparator.Number[int64]{In: nested.Connect}},
					Limit:  new(0),
				}, func(o *CallOptions[CategoryFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &CategoryFieldOptions{ID: true}
					// Deliberately unlocked: this validates caller input at
					// call time, it does not promise the target is still
					// visible at commit — nothing can, since a soft delete one
					// moment after commit reaches the same end state (§13).
					o.LockMode = sql.LockNone
				})
				if err != nil {
					return fmt.Errorf("categories: connect: %w", err)
				}
				found := make(map[int64]bool, len(visible))
				for _, row := range visible {
					found[row.ID] = true
				}
				for _, tid := range nested.Connect {
					if !found[tid] {
						return fmt.Errorf("categories: connect: category %v: %w", tid, ErrNotFound)
					}
				}
				targetIDs = append(targetIDs, nested.Connect...)
			}

			// UpsertMany, not CreateMany (A1/F4/F6): re-adding an existing link
			// is a set operation whose answer is "linked", and CreateMany
			// raises a PK violation on it (§2.4 probe A). The junction is a
			// pure link table, so this compiles to ON CONFLICT DO NOTHING
			// (F7) on every dialect.
			if len(targetIDs) > 0 {
				links := make([]*CreateUserCategoryInput, 0, len(targetIDs))
				seen := make(map[int64]bool, len(targetIDs))
				for _, tid := range targetIDs {
					if seen[tid] {
						continue
					}
					seen[tid] = true
					links = append(links, &CreateUserCategoryInput{UserID: parent.ID, CategoryID: tid})
				}
				if _, err := c.userCategoryClient.UpsertMany(ctx, links, UserCategoryConflictPK, func(o *CallOptions[UserCategoryFieldOptions]) {
					nestedChildOptions(o, options)
					o.FieldOptions = &UserCategoryFieldOptions{}
				}); err != nil {
					return fmt.Errorf("categories: link: %w", err)
				}
			}

			// remove — no visibility read (D6). The junction PK embeds the
			// parent, so a caller can only ever delete its own links, and a
			// miss is a no-op: §2.4 probe C measures both. A hard delete is
			// correct here because user_categories carries no soft-delete
			// column; a junction that did would not emit this verb at all
			// (E11), since the read loader's junction query has no
			// soft-delete predicate and would keep showing the link.
			if !nested.Clear && len(nested.Disconnect) > 0 {
				pks := make([]UserCategoryPK, 0, len(nested.Disconnect))
				seen := make(map[int64]bool, len(nested.Disconnect))
				for _, tid := range nested.Disconnect {
					if seen[tid] {
						continue
					}
					seen[tid] = true
					pks = append(pks, UserCategoryPK{UserID: parent.ID, CategoryID: tid})
				}
				if err := c.userCategoryClient.HardDeleteMany(ctx, pks, func(o *CallOptions[UserCategoryFieldOptions]) {
					nestedChildOptions(o, options)
				}); err != nil {
					return fmt.Errorf("categories: unlink: %w", err)
				}
			}
		}

		// Terminal read — only when the caller selected a relationship.
		if fo := options.FieldOptions; fo != nil && (fo.Events != nil || fo.Orders != nil || fo.Categories != nil) {
			reloaded, err := c.Get(ctx, parent.ID, func(o *CallOptions[UserFieldOptions]) {
				*o = options
				o.SkipHooks = true
				o.LockMode = sql.LockNone
			})
			if err != nil {
				return err
			}
			user = reloaded
		}
		return nil
	}, database.TxOptions{CallbackMode: c.callbackMode})
	if err != nil {
		return nil, fmt.Errorf("update user with related: %w", err)
	}
	return user, nil
}

// nestedChildOptions copies the option fields that cross a table boundary.
// CallOptions[FO] is generic over the field-options type (shared_types_gen.go),
// so `*o = options` cannot be used across tables. FieldOptions and LockMode
// deliberately do not propagate.
//
// Generic in BOTH type parameters: the PARENT's field-options type varies per
// parent too, so pinning the second argument to CallOptions[UserFieldOptions]
// would make this helper compile for userClient alone.
func nestedChildOptions[FO, PFO any](o *CallOptions[FO], parent CallOptions[PFO]) {
	o.SkipCache, o.SkipEvents = parent.SkipCache, parent.SkipEvents
	o.SkipHooks, o.SkipTenancy = parent.SkipHooks, parent.SkipTenancy
	o.Tenant = parent.Tenant
}

// userNestedParentFieldOptions projects the caller's FieldOptions onto the
// parent write: relationship members are dropped (loading them there would run
// before the nested rows exist), and the primary key is forced on because the
// nested writes read it. A nil selection passes through — the parent reads that
// as "every column".
func userNestedParentFieldOptions(fo *UserFieldOptions) *UserFieldOptions {
	if fo == nil {
		return nil
	}
	scalars := *fo
	scalars.Events, scalars.Orders, scalars.Categories = nil, nil, nil
	scalars.ID = true
	return &scalars
}
```

`UpsertWithRelated` is this function with `c.Update(ctx, id, …)` replaced by
`c.Upsert(ctx, &input.User, target, …)`. Nothing else changes — that is **D3**.

### 5.4 Call site

```go
u, err := client.Users().UpdateWithRelated(ctx, uid, &models.UpdateUserWithRelatedInput{
    User: models.UpdateUserInput{Name: omittable.Set("Ada")},
    Categories: &models.UserCategoriesUpdateNested{
        Connect: []int64{7, 9},          // link existing
        Create:  []*models.CreateCategoryInput{{Name: "New Topic"}},
        Disconnect:  []int64{3},             // unlink
    },
    Events: &models.UserEventsUpdateNested{
        Disconnect: []uuid.UUID{oldEventID}, // set events.user_id = NULL
    },
}, func(o *models.CallOptions[models.UserFieldOptions]) {
    o.FieldOptions = &models.UserFieldOptions{
        ID: true, Name: true,
        // UserFieldOptions.Categories is *CategoryRelationshipOptions, not
        // *CategoryFieldOptions (models_gen.go:28849).
        Categories: &models.CategoryRelationshipOptions{
            FieldOptions: &models.CategoryFieldOptions{ID: true, Name: true},
        },
    }
})
```

The signature matches every other mutation — `(ctx, pk, input, opts ...func(*CallOptions[FO]))`
— so `FieldOptions`, `SkipHooks`, `Tenant` and `LockMode` behave as they do on `Update`.

### 5.5 The create half

`CreateWithRelated` is the same function with `c.Update(ctx, id, …)` replaced by
`c.Create(ctx, &input.Product, …)` and the narrower `…CreateNested` blocks (**D4**). Two things
about it are not derivable from §5.3, and both come from the create half.

**The nested child input elides only the FK it traverses.** `order_items` has two required FKs;
nesting under `products` removes `product_id` and `OrderID` survives. It reads as a surprise the
first time, and it is correct:

```go
// --- generated ---

type CreateProductWithRelatedInput struct {
    // Product holds the parent's own columns. Identical to the input accepted
    // by Create — nested members never restate a column (NW-D4).
    Product CreateProductInput `json:"product"`

    // OrderItems - one-to-many via order_items.product_id.
    OrderItems *ProductOrderItemsCreateNested `json:"order_items,omitempty"`
}

type ProductOrderItemsCreateNested struct {
    Create []*CreateProductOrderItemInput `json:"create,omitempty"`
    // No Connect: order_items.product_id is NOT NULL (D8).
}

// CreateProductOrderItemInput is CreateOrderItemInput with product_id removed —
// that column is set from the parent's primary key. OrderID stays: it is a
// different FK, and nesting under products says nothing about which order the
// item belongs to.
type CreateProductOrderItemInput struct {
    ID          omittable.Value[uuid.UUID]   `json:"id,omitzero"`
    OrderID     uuid.UUID                    `json:"order_id"`
    Quantity    omittable.Value[int32]       `json:"quantity,omitzero"`
    UnitPrice   decimal.Decimal              `json:"unit_price"`
    ExternalRef omittable.Value[*uuid.UUID]  `json:"external_ref,omitzero"`
    SourceIP    omittable.Value[*netip.Addr] `json:"source_ip,omitzero"`
    CreatedAt   omittable.Value[time.Time]   `json:"created_at,omitzero"`
}
```

On a **shape 2 (has-one)** edge the verb takes a pointer rather than a slice, and the runtime
rejects a second row: `AssetPrimaryDocumentCreateNested{ Create *CreateAssetPrimaryDocumentInput }`.
On an **M2M** edge there is no FK to elide — the parent's PK goes into the junction — so `create`
reuses `Create<Target>Input` verbatim. On a **shape 1 (belongs-to)** edge nothing is emitted
(**NW-D5**); `profile.userID` remains the way to link an existing user.

**The FK assignment is type-dependent, and the emitter cannot write `UserID: parent.ID` and
hope.** `events.user_id` is nullable and `orders.user_id` is NOT NULL — same shape, same
relationship type, different assignment. **Updated 2026-09-16**: Phase 26 moved the nullable-UUID
row from the null-wrapper form to the pointer form, because the stdlib `uuid` package has no
`NullUUID` (**F11**). The null-wrapper row stays in the table because it is still live for other
integrations — `decimal.NullDecimal` is the fixture's example (`models_gen.go` `UpdateEventInput`).

| FK column | Create-input field type | Nested assignment |
|---|---|---|
| NOT NULL, plain | `uuid.UUID` | `UserID: parent.ID` |
| NULL, pointer — **stdlib UUID, the default since Phase 26** | `omittable.Value[*uuid.UUID]` | `UserID: omittable.Set(&parent.ID)` |
| NULL, null-wrapper (integration supplying one) | `omittable.Value[decimal.NullDecimal]` | `Adjustment: omittable.Set(decimal.NullDecimal{Decimal: v, Valid: true})` |
| NULL, pointer to a non-addressable expression | `omittable.Value[*int64]` | via a local — Go cannot take the address of a struct field expression inline |

**The unlink spelling is the one to get right, and it is not symmetric with the link spelling.**
Setting the FK to NULL is `omittable.Set[*uuid.UUID](nil)`; `omittable.Value[*uuid.UUID]{}` omits
the column instead, and the compiler takes either (**F11**). Under the old null-wrapper binding
these were visibly different constructor calls, which is why nothing in this design used to say
so.

`RelationshipContext` already carries both facts this needs — `FKNullable` and `FKColumnGoType`
(`cmd/sqlgen/gen/context.go:338-351`), added for the O2M loader's bucket-key unwrap (FIX-069).
The nested-write emitter reads those two fields rather than deriving a third spelling of "is this
FK nullable" — **NW-D1** again.

The create-side body itself is mechanical against §5.3: parent `Create` with relationships
stripped, one `CreateMany` per edge with the traversed FK assigned per the table above, the
M2M visibility read and junction link, and the terminal `Get` only when a relationship was
selected. It was compiled against the generated package the same way (§2.3).

### 5.6 Composite-PK tables

A composite-PK table reaches this feature from two directions, and only one of them emits
anything in v1. Both are worth writing out, because "what is the PK here" is the question every
line of a nested write turns on.

**As a nested *target* — this already happens, on every M2M edge.** The junction is a composite-PK
table, and §5.3 writes it twice: once through `CreateUserCategoryInput` and once through
`UserCategoryPK`. Nothing special is needed, because a composite-PK target is never *generated* —
the caller supplies every PK column:

```go
// The generated composite-PK shapes (models_gen.go:24718-24725, :26045-26048).
type UserCategoryPK struct {
    UserID     uuid.UUID `db:"user_id"     json:"user_id"`
    CategoryID int64     `db:"category_id" json:"category_id"`
}

type CreateUserCategoryInput struct {
    UserID     uuid.UUID `json:"user_id"`
    CategoryID int64     `json:"category_id"`
}

// link  — every PK column is caller-known, so UpsertMany's conflict target is
//          the PK and the row needs no read-back (this is why F5 never applied
//          to junctions: there is no PK to resolve).
links = append(links, &CreateUserCategoryInput{UserID: parent.ID, CategoryID: tid})

// unlink — the PK struct IS the parent-scope guard. A caller can only ever name
//          a pair whose first column is its own id (D6, probe C).
pks = append(pks, UserCategoryPK{UserID: parent.ID, CategoryID: tid})
```

**As a nested *parent* — not emitted in v1 (E3), and the shape is worth pinning anyway.** Two
things change and nothing else does. First the signature, which follows the shipped composite-PK
convention (`Get(ctx, pk UserCategoryPK, …)` at `models_gen.go:24886`,
`Update(ctx, pk UserCategoryPK, …)` at `:25140`):

```go
// Single-column PK, as emitted today:
func (c *userClient) UpdateWithRelated(ctx context.Context, id uuid.UUID,
    input *UpdateUserWithRelatedInput, opts ...func(*CallOptions[UserFieldOptions])) (*User, error)

// Composite PK — the id argument becomes the generated PK struct:
func (c *workspaceSettingClient) UpdateWithRelated(ctx context.Context, pk WorkspaceSettingPK,
    input *UpdateWorkspaceSettingWithRelatedInput, opts ...func(*CallOptions[WorkspaceSettingFieldOptions])) (*WorkspaceSetting, error)
```

Second — and this is the part that reads as a surprise — **the child's FK never carries the PK
struct.** Every edge is a single-column FK (**C1**), so the nested write assigns the one parent
column the FK references, not the parent's identity:

```go
// A config-declared edge from a composite-PK parent, traversing ONE of its
// PK columns:
//
//   workspace_settings:
//     relationships:
//       - {name: Notes, type: one_to_many, table: workspace_notes, fk: workspace_id}
//
// The nested create assigns the referenced column. `parent` is the full entity
// returned by step 1, so every column is in hand — the PK struct is only ever
// used to identify the parent, never to populate a child.
inputs = append(inputs, &CreateWorkspaceNoteInput{
    WorkspaceID: parent.WorkspaceID, // the traversed FK — one column of the composite PK
    Body:        n.Body,
})
```

**What the fixtures actually generate today: nothing.** Both composite-PK tables in the `graphql`
example — `UserCategory` (`models_gen.go:24722-24725`) and `WorkspaceSetting` (`:31683-31688`) —
carry **zero relationship members**. A composite-PK table's auto-detected edges all point *up*
(its PK columns are FKs into other tables), which is shape 1, deferred by **NW-D5**. So no
composite-PK parent block appears anywhere in the fixture, and **E3** costs nothing today.

---

## 6. Generated GraphQL surface

Three added mutations per eligible parent; the existing five are byte-identical.

```graphql
input UpdateUserWithRelatedInput {
  user:       UpdateUserInput!
  events:     UserEventsUpdateNested
  orders:     UserOrdersUpdateNested
  categories: UserCategoriesUpdateNested
}

input UserEventsUpdateNested {
  create:  [CreateUserEventInput!]
  connect: [UUID!]
  disconnect: [UUID!]
  clear:      Boolean
}

"orders.user_id is NOT NULL — connect and remove are not expressible on this edge."
input UserOrdersUpdateNested {
  create: [CreateUserOrderInput!]
}

input UserCategoriesUpdateNested {
  create:  [CreateCategoryInput!]
  connect: [Int!]
  disconnect: [Int!]
  clear:      Boolean
}

extend type Mutation {
  createUser(input: CreateUserInput!): User!
  createUserWithRelated(input: CreateUserWithRelatedInput!): User!             # new
  updateUser(id: UUID!, input: UpdateUserInput!): User!
  updateUserWithRelated(id: UUID!, input: UpdateUserWithRelatedInput!): User!  # new
  upsertUser(input: CreateUserInput!): User!
  upsertUserWithRelated(                                                       # new
    input: UpsertUserWithRelatedInput!
    conflictTarget: UserConflictTarget = PK                                    # Q4
  ): User!
  # …unchanged…
}
```

```graphql
mutation {
  updateUserWithRelated(id: "…", input: {
    user:       { name: "Ada" }
    categories: { connect: [7, 9], create: [{ name: "New Topic" }], disconnect: [3] }
  }) {
    id
    name
    categories { id name }
  }
}
```

The response selection is served by the existing PRD §26.5.2 field-options walker and the single
terminal `Get`, so relationship hydration costs nothing new.

Wiring follows the shipped FIX-064 Shape A split exactly — a `translateUpdateUserWithRelatedInput`
in `input_translate.go.tmpl`, an `M.UpdateUserWithRelated` in `resolvers.go.tmpl`, and a seed
delegating from `*mutationResolver` in `seeds.go.tmpl`. No new architectural seam.

> **Annotated 2026-09-23 (27.10), not rewritten.** The sketch above differs from what landed in
> four places, and PRD §26.5.1 is normative for all of them. (1) The update wrapper's flat member
> is **nullable** (`user: UpdateUserInput`), so a nested-only update need not send `user: {}`.
> (2) The argument is `conflictTarget: UserConflictTarget! = PK`: non-null with a default, and
> required with no default on a table that has no primary-key target. (3) The nested child inputs
> are `<Parent><Edge>CreateInput` (27.7's respelling), not `Create<Parent><Edge>Input`. (4) A
> has-one edge's verbs take one value, not a list. Two rulings were also made here that this
> section never considered. A target's `api.operations` mask narrows the verbs that write it
> (§9.9.4 E7). And the flat update input's `_inc` / `_dec` operators run in the same transaction
> as the nested writes.

**The conflict target reaches GraphQL on the nested mutation only (Q4).** `upsertUser`
hard-codes `models.UserConflictPK` (`graph/sqlgenresolver/resolvers_gen.go:2442`), so on a table
with an app- or db-generated PK where the caller supplies no id it always takes the insert
branch. Harmless on a flat upsert; on the nested one it means the `disconnect` verb silently matches
nothing. `upsertUserWithRelated` therefore takes a generated `<Table>ConflictTarget` enum
argument defaulting to `PK`, and `upsertUser` is left exactly as it is.

To be clear about why: **not** because changing `upsertUser` would break a published schema —
nothing is published, so there is no compatibility cost to weigh. The reason is scope. Nothing
has asked for a conflict target on the flat mutation, and the nested one needs it because
**D3** makes the whole update block reachable from the insert branch. The residual asymmetry —
two upsert mutations with different conflict semantics — is real, and is recorded as a §26.12
known limitation (**P20**) rather than papered over. Revisit if a consumer asks for the flat
form.

---

## 7. Config surface

```yaml
generation:
  nested_mutations:
    enabled: false                  # default OFF — zero golden churn until asked for
    operations: [create, update, upsert]
    verbs: [create, connect, disconnect, clear]
    max_depth: 1                    # NW-D8; the only accepted value in v1

tables:
  users:
    operations:
      upsert_many: true             # D5 — required for M2M connect/create
    nested_mutations:
      # Omit to include every eligible edge. When present, an explicitly listed
      # edge that fails an eligibility rule is a hard error (NW-D12).
      relationships:
        - name: categories
        - name: events
          allow_reparent: true      # D7 — opt out of adopt-only connect
```

The `discriminator:` migration that makes a polymorphic edge nestable (**NW-D6**), worked on the
`assets` fixture:

```yaml
tables:
  assets:
    relationships:
      # Invertible: `filter:` → `discriminator:` makes the edge nestable. Reads
      # are unchanged — the generator compiles a discriminator into the same
      # equality predicate the filter produced.
      - name: Attachments
        type: one_to_many
        table: documents
        fk: entity_id
        discriminator:
          column: entity_type
          value: "asset.attachment"

      # Uninvertible: stays on `filter:`, stays readable, stays non-nestable.
      - name: PhotoAttachments
        type: one_to_many
        table: documents
        fk: entity_id
        filter: "entity_type = 'asset.attachment' AND name LIKE 'photo_%'"
```

`discriminator:` is independently useful — it is the first *structured* description of a
polymorphic edge in the config, and §13.7 sub-categorized polymorphism currently has to express
the same fact as an opaque string.

Go-side deltas:

- `config.Operations` gains `UpsertMany *bool` (**D5**) and `CreateWithRelated` /
  `UpdateWithRelated` / `UpsertWithRelated` `*bool` (`config/config.go:400-419`), plus matching
  `apiOperationFields` entries so the API mask can subtract them independently.
- **`ExpandPreset` must grow all four fields in every one of its five arms**
  (`config/config.go:1260-1310`) — it enumerates each operation explicitly per preset, so a field
  left out of an arm is not a default, it is a nil. Each arm is a decision, not a copy: under
  `append_only` (`upsert: false`) `upsert_many` must be `false` and the three `…WithRelated`
  entries follow their own base operation; under `read_only` all four are `false`; under
  `no_delete` / `no_hard_delete` all four follow `all`. The nested methods track the verb they
  compose, not the delete policy — a `disconnect` on a `no_hard_delete` table is an FK null-out on
  O2M and is simply ineligible on M2M (**E11**, **E9**).
- New `NestedMutationsConfig` on `GenerationConfig`; `TableNestedMutationsConfig` with a
  per-relationship `allow_reparent` on `TableConfig`.
- `config.TableRelationship` gains `Discriminator *RelationshipDiscriminator`
  (`config/config.go:563-576`), unchanged from **NW-D6**.

---

## 8. PRD deltas required (bless before code)

Per project rule 1 the PRD is normative and none of this is in it. These land as the 27.0 PRD-sync
sub-item, in the shape 25.0 used for Phase 25.

**P1–P13** come from the create half; **P14–P23** are new here.

| # | Section | Delta |
|---|---|---|
| **P1** | **§9.2 Write Operations** | Add `CreateWithRelated` to the generated write surface, with the **NW-D2** rationale for it being a separate method. Merged with **P14**. |
| **P2** | **New §9.9 Nested Writes** | Normative home for the four write shapes (§1.3), the eligibility rules (§4.2) and the verb matrix. Merged with **P15**. |
| **P3** | **§13.1 Relationship Types** | Split the O2O row into belongs-to / has-one and state that `FKOnTarget` is carried on the relationship context (**NW-D1**). Read behavior is unchanged; the table is currently silent on a distinction the code already makes. |
| **P4** | **§13.4 Manual Relationship Configuration** | Introduce `discriminator:`, its mutual exclusion with `filter:`, and the rule that only `discriminator` edges are write-eligible (**NW-D6**). |
| **P5** | **§13.7 Sub-Categorized Polymorphism** | Note that nested writes on sub-categorized edges require the §13.4 discriminator form. |
| **P6** | **§4.6 GenerationConfig / §4.8 TableConfig** | The `nested_mutations` blocks; the `…_with_related` entries in the operations mask. |
| **P7** | **§4.13 Config Validation Rules** | `filter` × `discriminator` exclusion; `max_depth` accepts only 1 in v1; an explicitly-listed ineligible edge is a hard error. |
| **P8** | **§25.1 Query Count Guarantee** | Extend to the write side with **NW-D11**, as sharpened per-verb by **D12**. |
| **P9** | **§26.5.1 GraphQL mutations** | `create<Table>WithRelated` in the curated mutation surface, with the **C3** note on why it cannot fold into `create<Table>`. Merged with **P19**. |
| **P10** | **§26.12 Known Limitations** | What the create half does not do: belongs-to `create`, depth > 1, `filter:` edges. Merged with **P20**. |
| **P11** | **§32.5 Interactions** | **E6** / **E7** — the access projection governs nested inputs, and a hidden entity gets no nested member. |
| **P12** | **§29.4 Generated Behavior** (+ §29.10 Relationship Propagation) | **NW-D13** — a `connect` is tenant-checked by reading the target through its own client. §29.10 currently covers relationship *loading* only; the write side needs the same fail-closed statement, and this is the one place nested writes add a security rule rather than inheriting one. Merged with **P21**. |
| **P13** | **§26.5.5 error codes** | **NW-D13**'s visibility miss maps to `NOT_FOUND`, not `BAD_REFERENCE` — pinned so the oracle is not reintroduced by an error-mapping change. |
| **P14** | **§9.2 Write Operations** | Add `UpsertMany` (**D5**) and the three `…WithRelated` methods to the generated write surface. |
| **P15** | **New §9.9 Nested Mutations** | Normative home for the §4.1 eligibility matrix, the verb algebra, **D12**'s query-count contract, and the **D6** read asymmetry. |
| **P16** | **§9.2 `Update` row** | State that `Update` honours the empty-`FieldOptions` skip like every sibling (**D10**/**F1**) — the PRD is currently silent and the template currently disagrees with its siblings. |
| **P17** | ~~§9.5 Upsert Conflict Keys~~ — **already landed** (`effc99d`, with FIX-196) | PRD §9.5 documents the conflict-target-covers-every-column branch; §9.7 states the PK-resolution invariant, the per-dialect mechanism, why the fallback `SELECT` must not filter soft-deleted rows, and why `DO UPDATE SET pk = pk` was rejected (measured: it rewrites the tuple and fires `AFTER UPDATE` on a logical no-op). `guidelines/SQL.md:311-312` carries the dialect-implementer note. **No delta remains.** |
| **P18** | **§13.1 Relationship Types** | Add FK nullability as a first-class property of an edge — it decides the write verb set (§1.3), and the table is currently silent on a distinction the design turns on. |
| **P19** | **§26.5.1 Exposed Resolver Surface** | The three `…WithRelated` mutations in the curated surface, with the **C3** note on why they cannot fold into the existing ones. |
| **P20** | **§26.12 Known Limitations** | What v1 does *not* do: `delete` (**D9**), belongs-to nesting, depth > 1, re-parenting without `allow_reparent`, `filter:` edges, `connect`/`disconnect` on NOT NULL FK edges (**D8**). |
| **P21** | **§29.4 / §29.10** | **D6** — why `disconnect` needs no visibility read and `connect` does. §29.10 covers relationship *loading* only; the write side needs the same fail-closed statement. |
| **P22** | **§22 Errors / §26.5.5** | **D15** — two new sentinels (`ErrAlreadyRelated` → `CONFLICT` for a `connect` target parented elsewhere (**D7**), `ErrNestedVerbConflict` → `INVALID_INPUT` for **D13**) plus the `*database.NestedMutationError{Edge, Verb, ID}` wrapper that unwraps to them. §22.1 enumerates the sentinel set, so it must grow with both; §22.3 states the wrapping convention, so it must say that the wrapper's fields carry the same edge-and-verb attribution the message does (§4.4). |
| **P23** | **§26.5.1 Exposed Resolver Surface** | The generated `<Table>ConflictTarget` enum argument on `upsert<Table>WithRelated` only (**Q4**), with the note that `upsert<Table>` stays PK-conflict-only and the asymmetry is deliberate. |
| **P24** | **§ hook `MutationOp` enumeration** (`docs/PRD.md:9537-9542`) | Add `OpUpsertMany` to the normative constant list, and state **D16**: the `…WithRelated` methods deliberately have **no** op of their own, because each inner call fires its own chain. Pin that `OpUpsertMany` maps to the existing `event.Upsert` action rather than a new wire value (**F9**). |
| **P25** | **§9.9 Nested Mutations / §25.1** | **D12** as twice amended — the query-count bound is `ceil(N / batchSize)` for every id-list-bearing verb and exactly 1 for `clear`, not a flat 1. **Carries Q9's resolution and the §2.6 measurement behind it**, including the per-dialect ceilings, since §25.1 is where a reader would look for why the bound is not 1. §25.1 currently states the read-side guarantee only; the write-side extension must carry the batch term or it contradicts `CreateMany`.  |
| **P26** | **§13.4 Manual Relationship Configuration** | **D18** — a `discriminator:` edge owns its column on the write side: nested `create` sets it, the child input elides it, and `connect`'s visibility read matches on it. **P4** introduces `discriminator:` for reads; this is the half that keeps writes from producing rows the edge cannot read back. |
| **P27** | **§4.13 Config Validation Rules** | **D20** — a `…_with_related` operation enabled without its base operation is a hard validation error. Sits beside **P7**'s other nested-mutation config rules. |
| **P28** | **§9.9 / §26.12** | **D19** (a parent with no eligible edge emits no nested surface at all) and **D21** (a nil `FieldOptions` returns relationship members unpopulated, including on edges the call just wrote). Both are behavior a consumer can observe and neither is currently written anywhere. |
| **P29** | **§27.7 (cache behavior by operation) / §28 (events)** | **A16** — the observability contract for the nested methods, currently written nowhere. Four statements, each observable and none deducible from the flat path: what fires and in what order (each inner call fires its own chain, registered as `OnCommit` callbacks and flushed FIFO after the **root** commit); a rolled-back savepoint emits **nothing**, because at-depth rollback discards the callbacks it accumulated (`database/transaction.go:363-366`, documented at `:117-122`); a relationship-loaded parent is never cacheable (§27.6), so nesting cannot make a cached entity stale; and nested creates deliberately do **not** warm the cache the way flat ones do, because warming costs back the exact read **C6** exists to skip (**EQ4**). The last is a deliberate asymmetry between two write paths, and undocumented asymmetries get filed as bugs. |

---

## 9. Phased rollout & ticket breakdown

Sized so each ticket lands independently and is verifiable on its own.

**FIX first (not part of the phase). ✅ Done.** **F5** — upsert PK resolution on a conflict that
changes nothing — landed as **FIX-196** on 2026-09-10 across all three dialects, with
`upsert_idempotent_test.go` regression tests on the `mysql` and `postgres` examples. Nothing below
is blocked on it, and **E10** needs no dialect gate.

**27.0 — PRD sync (P1–P29).** No code. Gating item for everything below. **Q9 and Q10 are
settled** (§14) — Q9 by measurement (§2.6: batch at `batchSize`, because SQLite's ceiling is 32766
and the errors carry no attribution), Q10 by decision (alphabetical by relationship name). 27.0
writes both into the PRD: Q9 lands in **P25**'s query-count text, Q10 in **P15**'s §9.9.

**Two standalone FIXes land before the phase, not inside it — both now logged and resolved.**
**A18** → **FIX-207**: make the §32.3 redaction switch's `default` arm fail closed, publishing nil
rather than `mc.Input`, so *any* future op added without a redaction arm degrades to no payload
instead of an un-redacted one. **EQ1 / A14** → **FIX-208**: the non-tenanted `OpUpdateWhere` cache
arm, resolved by folding the four `*Where` ops into the key-based invalidation arm. Both were true
at HEAD with or without this feature and both benefited every existing caller, which is why
holding them inside the phase would have buried two general repairs in a feature branch.

**Ticket D is split three ways — Da, Db, Dc.** Lettered, not numbered, because §3.3 already owns **D1**–**D21** as decisions and that section keeps D14 permanently unused precisely so a bare `D<n>` resolves to exactly one thing. Phase 16's `16.8a–g` is the project's precedent for sub-lettering.

It was one ticket carrying a `sql/` runtime change, a new
template across three dialects, a `hook/` constant, three generated switch arms, four unsettled
sub-decisions and six pins — and every later ticket blocks on it. The companion's §8.3 already
names the forced order (`G2 → G1 → G3`), so the split follows the dependency that exists rather
than inventing one.

| Ticket | Content | Depends on | Acceptance |
|---|---|---|---|
| **A** | `FKOnTarget` hoist onto `RelationshipContext` (**NW-D1**, still needed per **F2**) | — | **zero golden movement** |
| **B** | `discriminator:` config (**NW-D6**) **plus its write-side rules (D18)** | — | reads byte-identical before/after the `assets` migration. **Plus D18's rules (b) and (c), both failing-first**: a nested `create` cannot express the discriminator column — it is elided from the child input, **C2**-style — and a `connect` naming a target whose discriminator does not match the edge is `ErrNotFound`. Without them the edge accepts writes it can never read back |
| **C** | Single-row `Update` skip-refetch escape (**D10**) | — | empty-`FieldOptions` `Update` returns `nil, nil` instead of a spurious `ErrNotFound` (corrected from "one fewer round-trip" — see **F1**); zero golden movement elsewhere |
| **Da** | **`sql/` — the batched conflict clause.** Grow `MultiInsertOptions` the three fields `InsertOptions` already carries (`UpsertConflictKeys`, `UpsertUpdateColumns`, `UpsertResolvePKColumn`) and call `d.UpsertClause(...)` from `BuildMultiInsert`, mirroring `BuildInsert:164-170`. ~10 lines, no new imports, **a runtime-module edit** | — | unit coverage per dialect; nothing emits it yet, so **zero golden movement** |
| **Db** | **`UpsertMany` — template and `operations.upsert_many`**, all three dialects, `Upsert`'s signature (**Q7**). **Scope is set by the four measurements in `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §6, each a decision the ticket must make:** **(1)** dedupe inputs by conflict target, last-wins, because PostgreSQL alone rejects an in-statement duplicate on the `DO UPDATE` shape while MySQL and SQLite accept it (§6.1); **(2)** decide and document the `[]*T` return contract on the `DO NOTHING` branch, where `RETURNING` yields only the inserted rows on two of three dialects (§6.2); **(3)** decide the MySQL db-generated-PK contract, where `LAST_INSERT_ID()` after a mixed batch names the first *newly inserted* row, so `firstID + i` is unsound (§6.3). Source `AffectedPKs` from the **inputs**, never from `RETURNING` (**F7**, **A19**) | Da | E2E: batch upsert on a pure link table is idempotent and one statement. **Plus the three dialect probes from §6.1–§6.3 as failing-first pins** |
| **Dc** | **`OpUpsertMany` and its three consumer arms.** The `hook/` constant (**D16**), plus cache invalidation, `mapOpToAction` (→ `event.Upsert`, **not** a new wire action) and the §32.3 redaction switch | Db | **Three regression pins, each failing-first**: rows written by `UpsertMany` are invalidated in the cache; a subscriber filtering `event.Upsert` receives them; the published event carries a **redacted, per-row** input. Every one of the three switches has a permissive default, so **none of these fails at compile time** — the un-redacted arm in particular is a §32.3 violation (**F9**, §7.4), and the per-row fanout cannot be a copy of `OpCreateMany`'s, because §6.2 makes `AffectedPKs` shorter than the input slice and the index-aligned copy misaligns (**EQ5**). **Resolved in 27.5 (verified 2026-09-17): this premise no longer holds.** `UpsertMany` sources `AffectedPKs` from the inputs, or from `resolveUpsertManyRows`, which returns exactly one key per deduped value row and errors otherwise — never from `RETURNING` (**A19**) — and the terminal republishes `m.Input = rowInputs`, the deduped slice, so it stays index-aligned with `AffectedPKs` (PRD §28.9, commit `ad575c5`). 27.6's arm therefore **is** an `OpCreateMany`-shaped index arm; see **FIX-219**. |
| **E** | **Supporting surface for the emitters.** `ErrAlreadyRelated` / `ErrNestedVerbConflict` / `NestedMutationError` (**D15**, **G8**); the junction-client and `callbackMode` fields on every entity client with an M2M edge — three of them, not one (**F8**, **A7**); the `resolved_names.go` **new key kind** (**A6**, **G9**); the four `Operations` fields across all **five** `ExpandPreset` arms (**G10**, **D20**) — five, not six: `all`/`""` is one arm covering two spellings, plus a `default` that errors | — | each independently testable; **zero golden movement** except the client-wiring fields |
| **F** | Go `CreateWithRelated` | A, B, Dc, E | E2E on all four shapes + the polymorphic negative. **Must use `Asset.Documents` / `Document.Assets`** — the only M2M edges with a UUID-PK target (§4.1) |
| **G** | Go `UpdateWithRelated` + `UpsertWithRelated`, **including the Q9 batching** on `connect`, `disconnect` and the `connect` visibility read | C, F | E2E per §4.1 cell, incl. the **D8** negatives, the **D7** adopt-only guard and the **E12** self-connect refusal on `WorkspaceNote.Children`. **Plus the F11 runtime pin**: unlink a child, then assert its FK is actually NULL — the wrong spelling compiles, matches its rows and reports success. **Plus a Q9 pin**: a `disconnect` naming more ids than `batchSize` issues `ceil(N/batchSize)` statements and succeeds — sized past **32766**, the SQLite ceiling (§2.6), so the test fails on a single-statement implementation rather than merely being slow |
| **H** | GraphQL projection for all three | F, G | verified through the real gqlgen server in `graphql/tests`, as 25.9 did |

Suggested order: **A ∥ B ∥ C ∥ E ∥ (Da → Db → Dc) → F → G → H**. A, B, C, E and the whole D chain
are standalone improvements that ship value even if the feature is later deferred — which makes
the end of Dc a natural checkpoint: every prerequisite has landed and paid for itself, and the
decision to build F/G/H can be taken on fresh information rather than up front.

---

## 10. Rejected / deferred

**Rejected: fold nested members into `Create<T>Input` / `Update<T>Input`** (Prisma's shape).
Blocked for create by **C3** (`Upsert` shares the input, so the schema would
advertise members upsert drops). Rejected for update for symmetry and because it multiplies the
most-branched template in the project.

**Rejected: a `set` verb** (replace the whole related set). It is `disconnect(current \ submitted) ∪
connect(submitted \ current)`, which needs a read of the current set inside the transaction plus
a lock to be race-free — and its failure mode is mass deletion from a partially-populated input.

**`clear` + `create`/`connect` now expresses it outright** (**D17**), and better: because `clear`
runs first (§4.3), `{clear: true, connect: [7, 9]}` *is* "the set is now exactly 7 and 9" — same
end state, one extra statement, **no read and no lock**, because `clear` is unconditional rather
than a diff. The failure mode is gone too: the caller wrote `clear: true` explicitly, so a
half-populated `connect` list cannot silently mass-unlink the way a half-populated `set` list
would.

**Rejected: a bespoke batched INSERT for junction rows.** Bypasses the junction client's hooks,
events and cache invalidation (**C7**, **D10**) — a second write path that diverges silently.
This is what makes **D5** a dependency rather than a convenience.

**Deferred: the `delete` verb (D9).** Destructive, and its meaning splits on whether the target
has a soft-delete column — `orders` does, `order_items` does not, and they are siblings under
the same parent. Revisit once a real schema needs it.

**Deferred: cascade delete — a `SoftDeleteWithRelated`, not a verb.** Distinct from **D9**, which
refuses *"delete this named child"*. This is the parent's own deletion propagating, which the
nested block never touches. The gap is real and only the application layer can close it:
**`ON DELETE CASCADE` cannot fire on a soft delete**, because a soft delete is an `UPDATE` and FK
referential actions only run on a real `DELETE`. The example schemas do declare
`ON DELETE CASCADE` (`graphql/schema.sql:143-144,157-158`), but only on junctions and only the
hard-delete path reaches it. So soft-deleting a `user` today leaves every `order` live with
`user_id` pointing at a row the caller can no longer see: the child is orphaned but visible, the
parent invisible but referenced — **C9**'s silent-drop shape, one level up.

Deferred on one gating question, not on cost. The delete half is cheap — one
`UPDATE … WHERE fk = parent` per child table, the same shape and query count as `disconnect`.
**Cascade *restore* is the problem:** restoring the parent must distinguish children the cascade
deleted from children that were already deleted beforehand, which needs persisted state (a
deletion-batch id, or `deleted_by`), not a loop. A cascade that cannot be symmetrically undone
silently resurrects rows someone deliberately deleted, so the delete half must not ship alone.
Two further splits to settle with it: children differ in capability (`orders` has `deleted_at`,
`order_items` does not, and they are siblings under one parent), and cascade is inherently
recursive, which collides with depth-1 (**NW-D8**). Shape when it lands: a `SoftDeleteWithRelated`
method plus a per-edge `on_delete: cascade | unlink | restrict` in config. There is currently **no
`cascade` or `on_delete` anywhere** in the config or the PRD — this is greenfield.

**Deferred: re-parenting by default (D7).** `allow_reparent: true` is the escape hatch; making it
the default needs a PRD §29 answer for "may a caller move a row between parents within a tenant?"

**Deferred: nested writes under `CreateMany` / `UpdateMany` / `UpdateWhere`.** **NW-D9**
blocks the first on **C5** (MySQL fabricates batch IDs as `firstID + i`); the other two
have no single parent PK to hang children off.

**Rejected: a bespoke mutation per combination** (`createProductWithReviews`,
`createProductWithReviewsAndTags`). Combinatorial in the number of edges.

**Deferred: shape 1 — belongs-to `create` (NW-D5).** The mechanism is known and costed, so this
is a follow-on, not an unknown: emit a second parent input, `Create<T>NestedInput`, identical to
`Create<T>Input` except that FK columns backing nestable belongs-to edges are relaxed to
optional; use it only inside the wrapper; add a runtime XOR check per edge ("exactly one of
`profile.userID` or `users.create`"); and extend the executor to a two-phase order (up-edges,
then parent, then down-edges). Cost is one extra input type per parent plus N runtime checks —
both derivable from the same `CreateInputFields` list, so **NW-D1**-safe. Deferred because the
ordering inversion is the entire complexity budget of the feature, and `connect` on this shape is
already free (it is the scalar FK field).

**Rejected: suppressing events and caching for the nested methods entirely (A17).** Considered
because one call now fans out across up to four tables, and rejected on a mechanism the premise
gets wrong. `SkipCache` on a mutation does not merely skip *population* — PRD §27.7 is explicit
that it means "no cache set on miss, **no invalidation on mutation**", and the hook returns before
`dispatchMutation` (`cache_gen.go:5058`). Suppressing would leave a cached `Event` carrying a
`user_id` that a nested `disconnect` had already nulled, served to every *subsequent* flat `Get`
until TTL. That is not "this operation bypasses the cache", it is this operation **corrupting the
cache for every other operation**: population and invalidation are not symmetric, and only the
first is ever safe to skip. Event suppression is sound but silent — a subscriber materialising a
read model would diverge with no error and no gap marker. And the premise does not hold anyway:
the §1.2 hand-written baseline touches the same tables and fires the same events, so suppressing
would make two paths with identical database effects differ in observability, which is the
divergence this feature exists to remove. A caller who wants a silent nested write already has
`SkipEvents` / `SkipCache`, propagated to children by `nestedChildOptions`; that choice belongs to
the consumer, not to the generator.

**Deferred: cycles on a self-referential edge (A3).** **E12** refuses a `connect` naming the
parent's own id, which closes the depth-0 loop. It does **not** close a longer one: `A.connect(B)`
where `B` is already `A`'s ancestor makes the subtree a ring, and no depth-1 check can see it —
detecting it needs an ancestor walk (a recursive CTE, or N reads up the chain) inside the
transaction, on every `connect` on every self-referential edge. Deferred on cost, not on doubt:
the read is unbounded in the tree's depth and the payoff is a schema-modelling error the
application can also prevent. `WorkspaceNote.Children` is the only fixture edge that can reach
this. Revisit if a consumer models a hierarchy deeper than one level and relies on recursive reads.

**Deferred: depth > 1 (NW-D8).** Additive — the wrapper types already exist; recursion, a cycle
guard, and a GraphQL `max_depth` interaction are the new parts.

**Deferred: M2M junctions carrying payload columns (E5).** Both fixture junctions are pure
two-column link tables, so the case is unexercised. The alternative — `connect: [{id: X, role:
"owner"}]` — waits for a real schema; a junction with a required payload column is arguably a
first-class entity the caller should write directly.

**Deferred: a parent correlation field on `hook.MutationContext` (Q6).** A nested `disconnect` fires
the child's `OpUpdateWhere` chain with no indication it came from an unlink, so an audit hook
cannot distinguish it from a bulk FK null-out. The hand-written baseline (§1.2) has the identical
property, so nothing is lost by waiting; revisit before GA.

---

## 11. Blast radius

| Area | Change |
|---|---|
| `parser/` | **None.** Relationship detection unchanged. |
| `sql/` | **`MultiInsertOptions` + `BuildMultiInsert` grow the upsert clause** (~10 lines, no new imports). The **F5** repair already landed with FIX-196 — `UpsertClause` takes `UpsertClauseOptions` and MySQL emits the `LAST_INSERT_ID(pk)` self-assignment — and that clause is reused unchanged. But this row previously read **None** on the grounds that `UpsertMany` reuses `BuildInsert`, and `BuildInsert` is **single-row** (`sql/builder.go:152`). The batched builder is `BuildMultiInsert` (`:184`), whose `MultiInsertOptions` (`:53-57`) carries no `UpsertConflictKeys` / `UpsertUpdateColumns` / `UpsertResolvePKColumn`. So a one-statement `UpsertMany` — which **D12** and §25.1 require — needs those three fields and a `d.UpsertClause(...)` call, mirroring `BuildInsert:164-170`. `hook/` is therefore **not** the only runtime module this feature touches. See `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §6.4. |
| `hook/` | **One constant** — `OpUpsertMany` in the `MutationOp` list (`hook/hook.go:17-33`), plus the three generated switch arms that consume it (**F9**, **D16**). One of **two** runtime-module edits, the other being the `sql/` row above. |
| **cache invalidation precision** | **Resolved ahead of this phase by FIX-208 — no cost left for this feature to carry (A14, EQ1).** O2M `connect` and `disconnect` compile to `UpdateWhere`, whose **non-tenanted** arm used to pattern-wipe the target table's entire cache namespace while the **tenanted** arms beside it evicted precisely (Phase 13's T7). FIX-208 merged the `*Where` ops into the key-based arm: all four now evict one key per matched row from `AffectedPKs`, which every `*Where` template already captures. Measured before the change — the wipe was a full-keyspace `SCAN` on both shipped backends and lost to precise eviction at every row count, while the orphans it swept (~190 B each) expire on TTL. So linking one child now evicts one key, and M2M `disconnect` (via `HardDeleteMany`) was already precise. |
| `database/`, `comparator/`, `omittable/`, `event/` | **None.** `OpUpsertMany` maps onto the existing `event.Upsert` action, so the `event` module's `Action` set is unchanged. |
| `cmd/sqlgen/config/` | Additive: `NestedMutationsConfig`, `TableNestedMutationsConfig`, `RelationshipDiscriminator`, four `Operations` fields, four `apiOperationFields` entries, validation rules. |
| `cmd/sqlgen/gen/context*.go` | `FKOnTarget` on `RelationshipContext` (Ticket A — the only edit to existing behavior); a nested-mutation context builder; `resolved_names.go` registration for five new type families. |
| `cmd/sqlgen/gen/templates/` | New `table/upsert_many.go.tmpl`, `table/{create,update,upsert}_with_related.go.tmpl`; 3 lines in `table/update.go.tmpl` (**D10**); additive arms in `shared/_input.tmpl` and five `api/*.tmpl`. |
| entity-client wiring | Two additive fields per affected client, in the existing injection block (`client_gen.go:183-193`): the **junction client** per M2M edge, and **`callbackMode`** (**F8**). |
| Golden files | **Zero movement** until `nested_mutations.enabled: true` — except Ticket C's 3-line `Update` change (behavior only under empty `FieldOptions`; needs a golden refresh). The **F5** golden churn has already landed with FIX-196. |
| Manifest (§30) | Should describe per-relationship write eligibility and verb set — additive, `schema_version` stays `0.1.0`. |
| MCP (§31) | `RelationshipDescription` could carry the verb set; not required for v1. |

---

## 12. Decision log — resolved questions

**Q1–Q8** were this document's; **NW-Q1–NW-Q6** came from the create half and are recorded here
because that document is gone. Everything in *this* table is resolved.

> **This section used to end "Nothing below is open", and read as though the whole design were
> settled. That is no longer true, and the claim was load-bearing** — it is what the header cited
> to call the design ready for a `/phase` breakdown. The open items now live in **§14**, added
> 2026-09-11: two questions this document cannot answer on its own (**Q9**, **Q10**), six carried
> in from the events-and-cache analysis (**EQ1–EQ6**), and the amendment backlog. Nothing below
> changed; the readiness claim did. **§14 was itself audited on 2026-09-16** — its amendment
> ledger was internally inconsistent and is repaired there.

| # | Question | Resolution |
|---|---|---|
| **Q1** | Does this doc supersede the create-half doc, or sit beside it? | **Supersede, and delete it.** `docs/NESTED_WRITES.md` is removed; **C1–C9**, **NW-D1–NW-D14**, **E1–E7**, **P1–P13**, its create-side Go surface and its open questions are folded into §2.1, §3.1, §4.2, §8, §5.5 and this table. One normative source; the history is in git. |
| **Q2** | `disconnect` on a nullable O2M: unlink, or is a miss an error? | **Unlink, and a miss is a silent no-op.** The requested end state ("not linked") already holds, so there is nothing to report. Matches the shipped idempotence convention on `SoftDeleteWhere` / `HardDeleteWhere` / `UpdateWhere`, and §2.4 probes C/D measure it. The asymmetry with `connect` — which *does* hard-error on a miss — is deliberate and documented in **D6**: a `connect` that misses writes state the caller cannot see; a `disconnect` that misses writes nothing. |
| **Q3** | Is `allow_reparent` per-edge config, or a per-call option? | **Per-edge config, never per-call.** It sits at `tables.<t>.nested_mutations.relationships[].allow_reparent`, so the decision is static, reviewable in one place and auditable in the repo. A `CallOptions.AllowReparent` would make re-parenting a per-request authorization decision, and PRD §29.11 puts fine-grained authorization out of scope ("compose with a policy layer"). Additive later if demand appears. |
| **Q4** | `upsert<T>WithRelated` when the GraphQL conflict target is PK-only | **Expose `<Table>ConflictTarget` on the nested mutation only** (§6, **P23**). `upsert<Table>` is untouched — on scope grounds, not compatibility grounds. The resulting asymmetry is a §26.12 known limitation (**P20**). |
| **Q5** | Should `create` + `connect` be one polymorphic list? | **Two lists.** GraphQL has no union input type, so `connectOrCreate: [{where, create}]` is an input object with two nullable members and a runtime XOR check — schema-expressible but not schema-enforced, which is the **C3** failure class again. Ordering across verbs is not observable: **D12** batches each verb into one statement. |
| **Q6** | Do nested child hooks see their parent? | **Not in v1; revisit before GA.** A nested `disconnect` fires the child's `OpUpdateWhere` chain with no marker distinguishing it from a bulk FK null-out. The hand-written baseline (§1.2) has the identical property, so nobody loses anything — but "who unlinked this" is a question audit logs get asked, and a correlation field on `hook.MutationContext` is additive. Recorded in §10. |
| **Q7** | Does `UpsertMany` need its own conflict-target argument, or infer PK? | **Mirror `Upsert`'s signature** — `UpsertMany(ctx, inputs, target, opts…)`. `UpsertMany` is a general-purpose addition (**D5**), not a junction helper; diverging its signature for the nested caller's convenience is a special case that would read as an inconsistency everywhere else. |
| **Q8** | Two flat sentinels, or one structured `NestedMutationError`? | **Structured** — `*database.NestedMutationError{Edge, Verb, ID}` unwrapping to both sentinels, so `errors.Is` still works and `mapErrorToGQL` can read the edge off the error instead of parsing it out of a wrapped string (**D15**). This is also what makes **NW-Q4** answerable. |
| **NW-Q1** | Naming: `CreateWithRelated` vs `CreateWith` / `CreateGraph` / `CreateNested` | **`…WithRelated`**, as used throughout. `CreateGraph` was the runner-up and is disqualified by "graph" already meaning something else in a GraphQL schema. |
| **NW-Q2** | Auto-include every eligible edge, or an explicit allowlist? | **Auto-include**, narrowed by `tables.<t>.nested_mutations.relationships` when present (§7). The eligibility rules are conservative and the feature is globally opt-in; allowlist-always means a new table silently gets no nested surface. |
| **NW-Q3** | M2M junctions with columns beyond the two FKs (**E5**) | **Refuse the edge in v1.** Both fixture junctions are pure link tables, so the case is unexercised; `connect: [{id, role}]` waits for a real schema. Recorded in §10. |
| **NW-Q4** | Should a GraphQL child failure carry `extensions.path: ["categories"]`? | **Yes, settled in 27.0.** Free once **D15**'s error carries `Edge`; the alternative is a consumer substring-matching the message to find out which edge failed, which is what the structured error exists to prevent. |
| **NW-Q5** | Do child hooks see their parent? | Same question as **Q6**; resolved there. |
| **NW-Q6** | Does the terminal read belong inside the transaction? | **Inside**, as §4.3 draws it — matching what `Create` already does (`templates/table/create.go.tmpl:145-149`). Reading after commit cannot hold locks and costs a second visibility round-trip, and it would diverge from every other mutation. |

---

## 13. Why the `connect` visibility read takes no lock

Resolved by measurement, because the intuition cuts both ways: the read is inside a transaction,
so it *feels* protected, and the FK constraint *does* take a lock on the referenced row. Neither
saves it, and the reason differs per dialect.

Generated code never sets `IsoLevel` (nothing in `cmd/sqlgen/gen/templates/` mentions it), so a
nested mutation runs at the server default — **read committed** on PostgreSQL, **repeatable read**
on MySQL. Measured on `postgres:16-alpine` and `mysql:8.0`, two sessions, `A` holding the nested
transaction open:

| # | A holds | B does | PostgreSQL | MySQL |
|---|---|---|---|---|
| 1 | `BEGIN` + plain `SELECT` (the read as drawn) | soft-delete the target | **succeeds** → link written to a soft-deleted target | **succeeds** → same |
| 2 | `BEGIN` + the junction `INSERT` already done | soft-delete the target | **succeeds** — FK lock does not stop it | **blocks** (lock wait timeout) |
| 3 | `BEGIN` + `SELECT … FOR SHARE` / `LOCK IN SHARE MODE` | soft-delete the target | **blocks** | **blocks** |

Row 2 is the one worth keeping. PostgreSQL's FK enforcement takes `FOR KEY SHARE` on the
referenced row, which **by design permits non-key updates** — and a soft delete writes
`deleted_at`, a non-key column — so the window stays open until commit. InnoDB takes a plain
shared record lock, which does conflict, so on MySQL the window closes at the insert. Both
dialects leave row 1 reachable, so the hole is real on both; they differ only in width.

**And yet the right answer is `LockNone`, as §5.3 draws it.** The state the lock prevents —
a junction row whose target is invisible — is reachable anyway, by a soft delete committed one
millisecond *after* our transaction commits. No lock can prevent that, and the end state is
byte-identical. So the system has to tolerate that state regardless, which means locking buys a
narrower window on a condition it cannot actually maintain, in exchange for holding shared row
locks on every connected target for the rest of the transaction — blocking the soft-delete path
behind unrelated nested writes (rows 2 and 3 are lock-wait timeouts, not errors).

What **NW-D13** actually guarantees is therefore worth stating exactly, because it is easy to
over-read: the visibility read is a **validation of caller input at call time**, not a durable
invariant. It closes the cross-tenant existence oracle — a foreign-tenant id fails whether or not
a lock is held, since the read still runs — and it turns an opaque FK violation into a precise
`NOT_FOUND`. It does not, and cannot, promise the target is still visible at commit.

The durable fix for an orphaned link is not a lock on `connect`; it is cleaning up links when the
target is deleted — the cascade work deferred in §10, where the same problem appears from the
other side.

---

## 14. Open questions — what `/phase` still has to settle

Added 2026-09-11. §12 resolved everything that gated the breakdown *as the design stood then*;
these are what a readiness audit turned up since. **None is a reason to delay the breakdown** —
each is a decision the 27.0 PRD-sync sub-item can carry — but every one of them changes code
somebody would otherwise write wrong, which is why they are named rather than discovered.

| # | Question | Why this document cannot answer it alone |
|---|---|---|
| **Q9** | ~~Do `connect` and `disconnect` batch, or do they keep one statement and inherit a parameter ceiling?~~ **RESOLVED 2026-09-16 by measurement: (a) — they batch at `batchSize`.** See §2.6 for the transcript. | The question named PostgreSQL's 65535 as the bound and derived "roughly 65k ids on an O2M edge, roughly 32k pairs on a composite-PK junction". **Both numbers are wrong, and the error is worse than assumed.** SQLite's ceiling is **32766**, half of PostgreSQL's and MySQL's — so the true bound is ~32.7k ids and ~16.4k pairs, and it is **dialect-dependent**. That kills option (b): documenting the cliff means documenting *three* cliffs, and a consumer who develops on PostgreSQL and deploys on SQLite finds theirs in production at half the tested depth. It also weakens (c): a validation cap would have to be dialect-parameterised to avoid rejecting requests two of the three dialects would happily serve. **(a) is the only dialect-independent answer, and it is what the project already does** — `CreateMany` batches at `c.batchSize` (default 200), and **D12** had already conceded `ceil(N / batchSize)` for `create`. Batching the other verbs makes the contract uniform instead of carrying one batched verb and three unbatched ones. Safety is not a concern: every verb runs inside the transaction, so a split statement is still atomic, and **D7**'s three-way partition is computed from the single visibility read *before* any `UPDATE`, so splitting the write cannot split the decision. |
| **Q10** | ~~What is "declared order"?~~ **RESOLVED 2026-09-16: alphabetical by relationship name.** | Taking the recommendation this row already carried, because nothing surfaced against it. It is **total** (every edge has a name, and names are unique per parent), **independent of the optional config block** — config order only exists when the user wrote `tables.<t>.nested_mutations.relationships`, so keying on it would make ordering appear and disappear with an unrelated setting — and it is **already the generator's tie-break elsewhere** (`cachedTables` sorts by struct name, `context_cache.go:115-117`). Parser detection order was rejected for the reason the project bans map iteration in templates: it is an emergent property of a traversal, not a decision, and it moves when detection changes — which `336f591` and `3509180` both just demonstrated. This fixes error attribution when two edges would both fail (§4.4), answers **EQ2**'s "may subscribers depend on this ordering" with a yes-if-pinned, and makes golden output deterministic. |

Also open, tracked elsewhere and listed here so this section is the single place to look:

- **EQ1–EQ6** — `docs/design/archive/NESTED_MUTATIONS_EXAMPLES.md` §7.6. Cache precision on the non-tenanted
  `OpUpdateWhere` arm (**EQ1**, the one with teeth — it decides whether O2M `connect`/`disconnect`
  wipe the target table's cache namespace — since resolved by FIX-208), event-ordering as contract (**EQ2**), a nested
  correlation marker (**EQ3**, restating **Q6**), cache population on nested creates (**EQ4**),
  `UpsertMany`'s per-row event input (**EQ5**), and partial skip flags (**EQ6**).
- **A1–A20** — **closed against this document 2026-09-16.** A1, A2, A3, A6, A7, A10, A11, A14,
  A16 and A17 were applied in that pass, joining A4, A8, A9, A12 and A13 (already applied), A5,
  A15 and A19 (folded into Ticket D's scope) and A20. *This entry previously read "Five are
  applied; A4, A5 and A15 were withdrawn when D5 was restored" — which contradicted the header on
  A4 and was wrong about A5/A15: restoring **D5** is what made them **necessary**, since both
  widen a ticket that only exists when `UpsertMany` is in scope.* **The companion still owes its
  own sync**: its §2 edge count says seventeen (now eighteen), and its §3/§4 listings and §8.1
  failing-first table are written against `uuid.NullUUID`, which no longer exists (**F11**).
- **A18 and EQ1 were unlogged FIXes.** Both were recommended to land *before* the phase (§9) and
  both since have: **FIX-207** (A18) and **FIX-208** (EQ1), each filed and resolved on 2026-09-16.
- **FIX-200** — junction tables default to caching that the M2M edge never reads. **Resolved
  2026-09-16 as documented behavior, not a code change**, so this is no longer a prerequisite of any
  kind. The diagnosis held — the loader's junction query bypasses the cache and the junction's own
  by-pair `Get(pk)` is the only cached read — but the proposed default flip would have needed a
  second PRD §27.2 exception alongside the views one, and `tables.<junction>.cache.enabled: false`
  already expresses it per table. PRD §27.7 now carries the behavior. One correction that matters
  here: FIX-200 cited **E5** for a “pure link table, no payload” predicate; E5 actually reads *no
  **required** column beyond the two FKs*, and `tenancy`'s `tag_links` (NOT NULL `workspace_id`
  outside the PK) is the fixture the two readings disagree on.

### One inherited behavior worth writing down rather than deciding

`SkipTenancy` propagates to every nested child through `nestedChildOptions` (§5.3), and under it
`applyTenancy` stays false (`create.go.tmpl:229-230`), so the tenant column is **not** auto-set and
the caller must populate it on each nested child input. That is identical to the flat path and so
is not a new rule — but the nested call site is the one place where the child inputs are several
layers down from the `SkipTenancy` the caller typed, so it belongs in §29's generated-behavior
text (**P12**/**P21**) rather than being left to be rediscovered.

---

## References

- `docs/tracker/fixes.md` **FIX-196** — the shipped **F5** repair (all three dialects, `1d71de4` + `effc99d`)
- `docs/design/archive/GRAPHQL_READ_SURFACE.md` — D1 "one field map, two emitters"; the sibling-doc template
- `docs/design/archive/GRAPHQL.md` §6.0 — the curated mutation surface
- PRD §9.2 (write operations), §9.5 (upsert conflict keys), §13 (relationships), §18
  (transactions), §22 (errors), §25.1 (query count), §26.5 (GraphQL), §29 (tenancy), §32
  (column access)
- Fixtures: `cmd/sqlgen/testdata/examples/graphql/schema.sql`, `.../sqlgen.yml:163-201`;
  `cmd/sqlgen/testdata/examples/mysql/` and `.../postgres/` for the **F5** dialect cases
  (`upsert_idempotent_test.go` in each)
