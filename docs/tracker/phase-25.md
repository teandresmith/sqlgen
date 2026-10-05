# Phase 25: GraphQL Read Surface Completion

Status: **Complete — phase closed 2026-09-16 by `/close-phase 25`** (closure sweep clean under `-race`: `make check` across all 8 modules, `make check-examples` across all 12 example modules, `make test-integration` across all 8 modules; no flakes, no FIX entries open against the phase). (25.0 verified + closed 2026-08-25, 25.1 landed 2026-08-25, 25.2 landed 2026-09-04, 25.3 verified + closed 2026-08-26, 25.4 landed 2026-09-01, 25.5 landed 2026-09-02, 25.6 landed 2026-09-02, 25.7 landed 2026-09-03, 25.8 landed 2026-09-04, 25.9 landed 2026-09-04, 25.10 landed 2026-09-04, 25.11 landed 2026-09-04, 25.12 landed 2026-09-04; B6 + B8 landed 2026-08-25 after the D11 decision — all four tickets unblocked). **Ticket A's family work is done and FIX-145 is closed, so 25.2 (the completeness lint) is unblocked** — FIX-177, the blocking half of the two spec-level PRD §11.2 contradictions 25.5 made reachable, was resolved 2026-09-02 (postgres `json` document operators now cast to `jsonb`), along with **FIX-179** (its auto-review's find: `PrefixConditions` corrupted MySQL's function-leading JSON clauses under an O2O join); **FIX-178** (the SQLite half) was resolved the same day by commit `331f006`, gating the JSON comparator family out on SQLite at `columnIsFilterable` / `resolveSimpleComparator` so both surfaces drop together. — it is now the last of Ticket A and has one decision to make: the `StringComparator` fallback still covers the two shapes with no *sound* projection (a non-schema-enum PascalCase Go type, and an array element whose gqlgen list shape is unmeasured — PRD §26.12), and the lint has to choose between dropping those filter fields and hard-erroring. Ticket B (25.6 → 25.7) is **closed** — the cross-tenant view read hole is shut at the Go-client layer. **Ticket C is closed (25.8 + 25.9, both 2026-09-04)** — views carry the full PRD §26.4 read surface with no per-view template arm, proven by the F1+F3 two-tenant E2E over the real gqlgen server. **Ticket D is closed: 25.10 landed the runtime `sql.Exists` (D11), 25.11 landed 2026-09-04 as its payoff, and 25.12 landed the same day to put it on the GraphQL surface** — every list relationship now contributes a `*<Target>Filter` member to the parent filter, compiled to a correlated `EXISTS` that carries the target's own soft-delete and tenant predicates inside the subquery. The tenant reaches `ToConditions` through a **variadic `FilterOption`** — a user-blessed PRD §11.1 amendment, taken because §11.1 requires a *bound* tenant parameter in the subquery while pinning `ToConditions` to a signature with no ctx and no client. The option set also carries the subquery nesting depth, which names the target alias per level so a self-referential filter nested inside itself cannot shadow its own correlation. Four of the twelve examples regenerate byte-identically (no list relationships ⇒ no member, no signature change). **25.12 closed the consumer's blocked `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` story**: one nested input field per list relationship on `<T>Filter`, the per-table translator recursing into the target's own `translate<Target>Filter`, both rendered from one `APITableContext.FilterRelationships` slice so the schema and the translator cannot disagree, and the member gated on the target's own API exposure so a relationship into an `api.enabled: false` (or fully §32.2-hidden) entity is not a read side-channel. The `graphql` example gained a nullable self-FK on `workspace_notes`, which is simultaneously the tenanted-target fixture (25.11's subquery tenant predicate proven at the HTTP boundary) and the §13.5 recursive-input fixture; §13.6 circularity came free from the existing `assets ↔ documents` M2M pair. The one task 25.12 could not land is the lint arm — `ValidateAPIFilterCompleteness` does not exist yet — so it is written into 25.2's task list. **25.2 landed 2026-09-04 and closes Ticket A and the phase's sub-item list.** The completeness lint exists on both surfaces and both directions, wired into `buildOneAPITable` / `buildOneAPIView` so it hard-errors under `sqlgen generate` and `sqlgen validate` alike. Its one open decision resolved as **drop the filter field**, not hard-error: the `StringComparator` fallback is deleted, so the two comparator shapes with no sound GraphQL face (a non-schema-enum PascalCase Go type; an array element outside the five measured built-ins) are now absent from both surfaces rather than advertised and ignored — the only outcome §26.5.3 permits, and the one that does not force a consumer to trade the column's read surface for the ability to generate at all. PRD §26.12 was amended to match (it had documented the fallback as kept, contradicting §26.5.3's lint). Zero golden churn across all 13 examples. **All 13 sub-items complete — phase closed 2026-09-16.**

> **Release-note carry-over from this header's "Consumer-visible behavior changes" note.** All
> three changes it names — a previously unscoped tenanted view read starting to filter (25.7), views
> starting to appear in the generated GraphQL schema (25.8/25.9, opt out with
> `views.<n>.api.enabled: false`), and 25.2's deletion of the `StringComparator` fallback removing
> previously-advertised filter fields — reached `main` **without a `!` or a `BREAKING CHANGE:`
> footer on any Phase 25 commit**, so Release Please would render them as ordinary features. The
> repository has no configured git remote and no release has consumed these footers, so the gap is
> still closable by amending before the first release. Flagged at closure rather than fixed:
> amending is a history rewrite, which is the user's call.
PRD Sections: 4.9, 4.13, 11.1, 11.2, 16.4, 26.4, 26.5.1, 26.5.2, 26.5.3, 26.10, 29.2.3, 29.2.4, 29.4.1, 32.2
Design doc: `docs/design/archive/GRAPHQL_READ_SURFACE.md` (PROPOSED 2026-08-25 — findings F1–F9, decision record D1–D10, tickets A–D)

> **Blessing note — inverted from Phase 24.** The PRD deltas for this phase are **NOT yet
> written**. Unlike Phase 24 (where §28 was blessed before the tracker existed), here **25.0 is
> the blessing sub-item**: it lands deltas B1–B7 (design §5) into `docs/PRD.md`, and every
> sub-item from 25.1 onward implements text that 25.0 made normative. Until 25.0 is complete,
> `docs/design/archive/GRAPHQL_READ_SURFACE.md` is the authoritative source for the *shape* of the work, and the
> PRD references below name the section each sub-item will be validated against **after** 25.0
> amends it. Do not start 25.1+ before 25.0 is Complete — project rule 1.
>
> **Two of the four gaps are unimplemented spec, not new features.** PRD §26.4 already declares
> `View → Object type (read-only — only Query fields generated)`, and `docs/design/archive/GRAPHQL.md` §952
> records "**Resolved:** views are treated identically to tables, gated to read-only". 25.8/25.9
> implement that; they do not introduce it. F1 (view tenancy) and the new comparator families are
> the genuinely new spec.

> **The load-bearing structural cause (design §1.3) — applies to 25.1, 25.2, and every family
> sub-item.** The GraphQL filter and sort surfaces are each emitted by **two independent functions
> that derive the same fact from different inputs**:
>
> | Surface | Schema emitter | Translator emitter |
> |---|---|---|
> | Filter comparator | `funcComparatorFor(APIFieldContext)` — switches on the **GraphQL** type, `default: return "StringComparator"` (`funcmap.go:290`) | `comparatorTranslatorVariant(string)` — parses the **model's Go** `*comparator.X[Y]` expression, `continue`s on Enum/JSON/JSONB/Slice (`context_api.go:1119`) |
> | Sort enum value | `screamingSnakeCase(f.SQLName)` — `col_` prefix on digit-leading names per §8.5 (`funcmap.go:300`) | `strings.ToUpper(toSnakeCase(c.Name))` — no prefix (`context_api.go:1154`) |
>
> Nothing forces agreement. When they disagree the schema wins at parse time and the translator
> wins at execution time, so the field is **accepted and ignored** — silent wrong results, the
> worst available failure mode. **25.1 collapses this to one derivation and 25.2 makes a
> regression unlandable.** Adding comparator families without 25.1 first would fix today's drift
> and set up tomorrow's, which is why 25.1 gates 25.3/25.4/25.5 and why F5 (sort) is in scope
> alongside F2 (filter).

> **Dependency order (record).** 25.0 blocks all. Ticket A (25.1 → {25.2, 25.3, 25.4, 25.5}) and
> Ticket B (25.6 → 25.7) run in **parallel**. Ticket C (25.8 → 25.9) needs **both** 25.7 (D9 — the
> security gate) and 25.1. Ticket D (25.10 → 25.11 → 25.12) is independent except 25.12 wanting
> 25.1's field map. **Shortest path to closing the cross-tenant read hole: 25.0(B1) → 25.6 →
> 25.7** — Ticket B is valuable standalone and should not wait on Ticket A.

> **Consumer-visible behavior changes** (must be called out at phase closure): a previously
> unscoped tenanted view read **starts filtering** (that is the fix), and views **start appearing**
> in the generated GraphQL schema (opt out with `views.<n>.api.enabled: false`).

---

## 25.0 PRD sync — deltas B1–B7 (no code)

**PRD Reference:** Sections to amend — 4.9 (ViewConfig), 11.1 (filter struct), 16.4 (view artifact table), 26.4 (schema generation + comparator inputs), 26.5.3 (translators), 29.2.3/29.2.4 (tenancy detection + type resolution). Design §5 (B1–B7).

**Status:** **Complete** (landed 2026-08-25, verified 2026-08-25 — `/verify 25.0`: 10/16 PASS, 6 PARTIAL, 0 FAIL; 7 FIXes logged, all 7 resolved). B1–B7 as specified, plus **B8** (PRD §11.5 + Appendix A) once the D11 correlation-qualifier decision was settled the same day. **All four tickets are unblocked.**

**Governing decisions:** D1–D10 (all — this sub-item is where they become normative).

### Tasks

- [x] **B1 — §29 extended to views.** Add a new subsection (§29.13, or extend §29.2.3 rules 1–5 to read "table or view"): a view carrying the effective tenant column is **tenanted**; **read path only**; per-view `enabled` / `column` / `required` / `type` override; nullable tenant column on a view → **warn + scope** (D6, distinct from the table hard-error in §29.2.4); views **participate** in the §29.2.4 uniform-tenant-type check across the generate run; materialized-view `Refresh` / `RefreshConcurrently` are explicitly **unscoped and Go-client-only** (D7). Remove the implicit "tables only" reading from §29.2.3.
- [x] **B2 — §26.4 comparator table + schema example.** Add `<Enum>Comparator`, `JSONComparator`, `JSONBComparator`, `<Elem>SliceComparator`, `DecimalComparator` rows. State the monomorphization rule (D3 — GraphQL has no generics, so each generic family is specialized per concrete type) and the **PostgreSQL-only** gating for `JSONBComparator` / `<Elem>SliceComparator`, matching §11.2's existing dialect note. State that `comparator.Custom` is **never** projected.
- [x] **B3 — §26.4 comparator input definitions.** Complete the five shipped inputs against `comparator/`: `StringComparator` + `gt gte lt lte nlike`; `NumericComparator` + `nin nbetween`; `TimeComparator` + `in nin nbetween`; `IDComparator` + `gt gte lt lte`. Define the **`isNull` rule**: an `isNull: Boolean` field is emitted on a column's comparator **iff the column is nullable**, mirroring the existing `String` / `NullableString` model split.
- [x] **B4 — §26.5.3.** Correct the worked example: the cited `translateNumericComparatorDecimal` **does not exist** (F7 — `decimal.Decimal` routes to `comparator.String` per FIX-040). Document the new `DecimalComparator` → `comparator.String` / `NullableString` path. Add the **D1 single-field-map invariant** ("it must be impossible to emit a filter or sort field in the schema that the translator cannot handle") and the **D2 codegen lint** that enforces it.
- [x] **B5 — §16.4 + §26.4 view rows.** Move views' GraphQL row from aspirational to specified: enumerate the exact emitted read surface (object type; `<V>Connection` / `<V>Edge` / `<V>ListResult`; `<V>Filter`; `<V>Sort` + `<V>SortField`; `Query.<view>` only when `@pk`-annotated; `Query.<views>` only when `cursor_keys` resolves; `Query.<view>List`; **no mutations**) and the `views.<n>.api` gating.
- [x] **B6 — §11.1 + §26.4.** Relationship filter fields on `<T>Filter` (D10): model-side member per list relationship, `EXISTS` compilation from existing join metadata, the target's soft-delete **and tenant** predicates injected inside the subquery, one hop per level with arbitrary depth by nesting, no aggregate predicates.
- [x] **B7 — §4.9 ViewConfig.** Document the new `tenancy` and `api` blocks with their tri-state semantics and the read-only operations restriction; add the matching validation rules to §4.13.
- [x] Record in §26.12 Known Limitations that filter **arguments** on relationship fields remain deferred, with the gqlgen-reachability reason (design §7).

### Acceptance Criteria

- Every behavior 25.1–25.12 will implement is normative in `docs/PRD.md` before that sub-item starts — no sub-item derives its own spec.
- §29 no longer reads as "tables only"; a reader can determine from the PRD alone whether a given view is tenanted and what its read path emits.
- §26.4 lists every comparator family in `comparator/` with its GraphQL projection and operand types, and the `isNull`-iff-nullable rule is stated once and referenced, not repeated per family.
- §26.5.3 contains no reference to a function that does not exist (F7 fixed).
- §16.4's view row and §26.4's view row agree with each other and with §26.5.1's per-surface table.
- The D1 invariant and the D2 lint are stated as requirements, not as implementation notes — a future contributor cannot land a sixth drifting emitter without violating written spec.
- No code, template, or golden file changes in this sub-item.

### Tests Required

- [x] (No tests — documentation only. The exit gate is that each of B1–B7 is locatable by section number and that 25.1–25.12's acceptance criteria can each cite one.)

### Completion Record

- **Date:** 2026-08-25
- **Files changed:** `docs/PRD.md` only (+205/−15). Deltas landed:
  - **B1** — §29.2.2 gains a pointer that views use `views.<name>.tenancy` with the same shape/precedence; §29.2.3 Detection rewritten to cover "table **or view**" across all five rules plus a "Views are not exempt" rationale paragraph; §29.2.4 validation split the nullable rule (table = hard error, view = warning + scope) and made the uniform-tenant-type check explicitly cover tenanted **views** (one `TenantResolver[T]` per package); **new §29.2.5 View tenancy** — the scoped-surface table (all five reads yes, `Refresh` no), the "no mutation half" list, per-view config + YAML, the D6 nullable rule with its rationale, `CallOptions` parity, D7 matview-refresh-never-scoped, and the F9 "caching is unaffected" note; §29.4.1 Reads gains a views bullet.
  - **B2** — §26.4 mapping table: `Comparator` row now reads "one per family **per concrete type parameter**"; new **§26.4 "Comparator input projection"** (h5, placed before §26.4.1 so no anchor renumbering) carrying Rule 1 monomorphization (with the rejected runtime-parse alternative), the PostgreSQL-only gating for JSONB/Slice, the one-input-per-(family, type-param) sharing rule, and "`comparator.Custom` is **never** projected".
  - **B3** — the same subsection's family matrix gives the **complete** operator list for all ten families (closing F6: `String` +`gt gte lt lte nlike`, `Numeric` +`nin nbetween`, `Time` +`in nin nbetween`, `ID` +`gt gte lt lte`), states **Rule 2 `isNull` iff nullable**, and adds the `DecimalComparator` paragraph (F7) and the non-filterable `json[]`/`jsonb[]` both-sides rule. The inline §26.4 schema example was expanded to match (full `StringComparator`/`NumericComparator`, a monomorphized `ProductStatusComparator`, `price: DecimalComparator`, `reviews: ReviewFilter`).
  - **B4** — §26.5.3: the worked example's `translateNumericComparatorDecimal` → `translateDecimalComparator` **and** an explicit "there is no `translateNumericComparatorDecimal`" sentence (F7's PRD/impl divergence); the shared-translator paragraph extended to every family (enum translators need no conversion — the `models:` binding already did it; nullable variants now carry `isNull` into `Null`); a relationship-filter translation paragraph with the API-excluded-target gate; **the D1 "one field map, two emitters" invariant** stated as a requirement with its silent-failure rationale; **the D2 bidirectional completeness lint**, including that deliberately-absent columns satisfy it by absence rather than exemption.
  - **B5** — §16.4 artifact table gains three rows (GraphQL read surface / GraphQL mutations / tenant scoping of reads) + a closing paragraph; §26.4 `View` row points at the new **§26.4 "Views on the GraphQL surface"** subsection, which closes the emitted set (object type, three envelopes, filter/sort inputs, the three Query fields with their exact gates, walker/translators/bindings, **never** a mutation or Create/Update input), notes the columns-only walker ⇒ one query per §25.1, and states the `views.<n>.api` gating + tenancy inheritance.
  - **B6** — §11.1 gains a relationship-member bullet and a **new "Relationship filters"** subsection: the `TaskFilter` shape, the emitted correlated-`EXISTS` SQL with the target's soft-delete **and** tenant predicates inside the subquery, and the three invariants (target scoping applies inside; parent correlation qualified at codegen so it survives `PrefixConditions`; one hop per level, no aggregates). §26.4 mapping table gains a "Relationship (as a filter)" row.
  - **B7** — §4.9 ViewConfig gains `tenancy` and `api` rows; §4.13 gains six validation rules (view `enabled: true` + absent column → error; nullable view tenant column → warning; same with explicit `enabled: true` → error; tenant-type divergence including views → error; mutation op in `views.<n>.api.operations` → error; `views.<n>.api.enabled: true` under global `api.enabled: false` → error).
  - **§26.12** — two new Known Limitations: relationship-field filter/sort **arguments** deferred (with the gqlgen-reachability reason and the one-query trade-off), and aggregate relationship predicates deferred.
- **Tests:** none required (documentation only). `make check` clean. Path-based E2E trigger evaluated and **not** fired — the only changed paths are `docs/**`, which match no glob in the trigger table; security-review trigger likewise not fired (nothing under `manifest/` or `cmd/sqlgen/cli/**`).
- **Post-review round (2026-08-25).** The auto-review raised 7 items: 5 were real defects in the delta and are **fixed in place**, 1 was a reviewer error corrected the other way, and 2 remain open as a design decision.
  - **F1 (fixed — the worst of them).** Rule 2 as first written was **self-contradictory**: it said `isNull` appears on a nullable column's comparator and not on a `NOT NULL` one, while the Sharing rule said one input type per (family, type parameter) across the project. GraphQL input types are global, so both cannot hold — and the example proved it, giving `createdAt: DateTime!` and nullable `deletedAt` the *same* `TimeComparator`. Rewritten: nullability is a **separate input type** (`Nullable<X>Comparator`), Sharing is now keyed on a (family, type parameter, **nullability**) triple, the example uses `NullableTimeComparator`, and §26.5.3's nullable-translator sentence names the input it takes. Left unfixed, 25.3/25.4/25.5 would each have invented the naming — violating 25.0's own "no sub-item derives its own spec".
  - **F2 (fixed).** F7 was only half-corrected: §11.2's selection table still listed `decimal.Decimal → Number[T]`, contradicting the new §26.4/§26.5.3 text. §11.2 now has a dedicated `decimal.Decimal → String` row with the reason (does not satisfy `Numeric`; ordered operators stay numerically correct because the DB coerces the parameter against a numeric column) pointing at `DecimalComparator`.
  - **F3 (fixed — security-adjacent).** "Views have no cache read-through path" was a flat normative claim contradicting §27.11 ("views can be cached like tables"), and it set a cross-tenant cache-poisoning trap. Now scoped to the current generated surface and paired with the unconditional rule: if a view read-through path lands, §29.5's tenant segment applies to tenanted views exactly as to tables.
  - **F5 (fixed).** §11.1 invariant 2 cited "the `PrefixConditions` contract in Appendix A"; Appendix A documents `Dialect` / `Table` / `Build*` only and never mentions `PrefixConditions` — its sole PRD occurrence was my own new reference. Repointed to §11.5 and rewritten as part of F4.
  - **F7a (fixed).** §26.5.1 was still "**Per-table surface:**" with no view coverage, though 25.0's own acceptance criteria and 25.8/25.9 all cite it. It now carries a Views paragraph pointing at §26.4's enumeration, noting the Excluded table applies unchanged.
  - **F7b (reviewer error — corrected the opposite way).** The review claimed §16.4's matview row ("Get if unique index **or** `@pk`") was stale and the implementation `@pk`-only. It is not: the introspector folds a qualifying unique index into `col.PrimaryKey` (`parser/introspect/introspect_integration_test.go:1025-1043` — `mv_totals` has PK columns, `mv_plain` has none precisely because no index qualifies). §16.4 is correct; **my** wording was too narrow. The gate now reads "an `@pk` annotation, or for a materialized view a qualifying unique index".
  - **Example coherence (fixed).** Following the `status: ProductStatus!` addition: `enum ProductStatus` is now declared, and `CreateProductInput` carries `status` (a `ProductStatus!` column with no stated default is required there under §26.4's own create-input rule).
  - **F4 + F6 (OPEN).** Not fixed — they need a design call. The incorrect invariant 2 was replaced with an accurate statement of the *problem* rather than a guessed solution; the decision and its three candidate shapes are recorded above §25.10.
- **Verify round (2026-08-25).** `/verify 25.0` — 10/16 PRD requirements PASS, 6 PARTIAL, 0 FAIL; `make check` clean (exit 0, all modules, `-race`); E2E and security triggers correctly not fired (docs-only diff); all 25 anchors in the added text resolve. Every factual claim the delta makes about the implementation was checked against source and holds: all ten comparator operator sets match `comparator/*.go` exactly, `decimal.Decimal` → `comparator.String` (no `translateNumericComparatorDecimal` anywhere in the repo), the matview PK gate matches §16.4 + the introspector, `CallOptions` parity for view reads, F9's no-cache-read-through, and FIX-103's both-sides skip. The independent `sqlgen-reviewer` pass agreed fully and added three material items the parent missed. **Seven FIXes logged; six resolved inline the same day:**
  - **FIX-132** (blocking) — Rule 1 listed `Number[T]` among the monomorphized families, contradicting the matrix, the example, §26.5.3 and `shared.graphqls.tmpl` (and 25.1's byte-identity criterion). Rule 1 now carves numerics out explicitly; Sharing is keyed on the GraphQL operand type; the §26.4 mapping row names both live axes. D3 itself never claimed it — the error entered when D3 was transcribed.
  - **FIX-133** (blocking) — two stale `deletedAt: TimeComparator` spellings contradicting Rule 2, one of them the comment introducing the corrected example. Both now `NullableTimeComparator`.
  - **FIX-134** (blocking) — the PRD stated the D1 sort invariant but never pinned the canonical enum-value spelling, so 25.1 could satisfy every written requirement by converging both emitters on `2010_REVENUE`, which is not a legal GraphQL `Name`. §8.5 gained a **GraphQL enum values** paragraph pinning `COL_2010_REVENUE` with the grammar reason; §26.4 (naming row + casing paragraph) and §26.5.3's D1 invariant all point at it.
  - **FIX-136** (tracked) — `Slice` row's operand column omitted `Boolean` for `isEmpty`; §11.2's array row carried no FIX-103 exception. Both fixed.
  - **FIX-137** (tracked) — two surviving "tables only" readings after B1 (§29.2.1's `column` row + its YAML comment, §29.11's non-goals bullet). Fixed; the cache/event/mutation passages were deliberately left table-scoped, since views have no analogue on those surfaces.
  - **FIX-138** (tracked) — 25.3 / 25.4 / 25.5 tracker text still encoded the pre-Rule-2 model (one input per family, conditional `isNull`) and 25.3's acceptance contradicted the deliberate `DecimalComparator` narrowing. 15 tracker edits; `docs/design/archive/GRAPHQL_READ_SURFACE.md` D4 + §3.1 now carry superseded markers so the design doc stops reading as an alternative spec.
  - **FIX-135** (tracked, **still open**) — Ticket-D normative text. The ungated halves landed: §11.1's emission rule is now explicitly **list-only** (O2M / M2M) with a paragraph on why a to-one relationship contributes no member in v1, the join-metadata sentence is split out as provenance, a cross-dialect sentence follows the `EXISTS` example, and 25.11's tasks/acceptance/tests were realigned. **B8 itself and the invariant-2 repoint remain blocked on the rev-F4 correlation-qualifier decision** recorded above §25.10 — Ticket D stays gated, Tickets A/B/C do not.
- **Notes:** No code, template, config, or golden changes — `git diff --name-only HEAD` is `docs/` only, so this sub-item cannot move generated output. Anchor hygiene verified with a GitHub-accurate slugger: all 25 anchors referenced by the new text resolve. Four **pre-existing** dangling intra-PRD links were found incidentally and deliberately **left alone** as out of scope for B1–B7: `#2654-increment--decrement-as-input-operators`, `#322-access-roles` (should be `#322-roles`), `#74-built-in-optional-types` (§7.4 is "Built-In Type Integrations"), `#98-generated-methods` (§9.8 is "Generated Method Patterns"). Placement choice worth recording: the two new §26.4 blocks are **h5 subsections** rather than renumbered `#### 26.4.x`, following the existing `##### PK columns in the create input` precedent — renumbering would have invalidated every cross-reference to §26.4.1 Scalar Marshaling. One coherence fix inside the illustrative §26.4 example: `type Product` gained `status: ProductStatus!` because the expanded `ProductFilter` example now filters on it.

---

## 25.1 One field map — single derivation for filter + sort projection (Ticket A, D1)

**PRD Reference:** Sections 26.4 (filter input emission), 26.5.3 (translator dispatch + the D1 invariant added by B4), 32.2 (access capabilities gate every projection decision). Design §3.1, §2-D1.

**Status:** **Complete** (landed 2026-08-25)

**Governing decisions:** D1 (one field map, two emitters).

**Depends on:** 25.0.

### Tasks

- [x] Add `APIFilterProjection` (design §3.1): `InputTypeName`, `TranslatorFunc`, `Operators []APIComparatorOperator`, `Nullable bool`. Add `Filter APIFilterProjection` and `SortEnumValue string` to `APIFieldContext`. _(`Nullable` was later removed as write-only — FIX-146.)_
- [x] Resolve the projection **once** per column inside `mapColumnToGraphQL`, from the same inputs the model side already uses (`ColumnContext` + dialect) — so `Filterable`, the input type name, and the translator function name are all decided at one point and cannot disagree.
- [x] Compute `SortEnumValue` once via `screamingSnakeCase` (the `col_`-prefixing spelling — PRD §8.5).
- [x] Repoint `templates/api/schema.graphqls.tmpl:41` from `{{ comparatorFor $f }}` to `{{ $f.Filter.InputTypeName }}`, and `:51` from `{{ screamingSnakeCase $f.SQLName }}` to `{{ $f.SortEnumValue }}`.
- [x] Repoint `buildAPIFilterFields` to read `APIFieldContext.Filter.TranslatorFunc` instead of re-deriving via `comparatorTranslatorVariant`; keep the FIX-103 `Filterable` skip and the §32.2 `APIFilterable` skip as the *single* gate they already are.
- [x] Repoint `buildAPISortFields` to read `SortEnumValue` instead of `strings.ToUpper(toSnakeCase(c.Name))` — **this is the F5 fix**.
- [x] Delete `funcComparatorFor` from `funcmap.go` and its `comparatorFor` funcmap registration.
- [x] Convert `APIContext.ComparatorFamilies` from `[]string` to `[]APIComparatorFamily` (name + operator list + nullable-variant flag) and drive `templates/api/shared.graphqls.tmpl` from it, replacing the five hard-coded `input …Comparator` blocks (`:27-79`). A newly monomorphized family must then need **no** template edit.
- [x] Keep `comparatorTranslatorVariant` as the projection's internal helper (it still parses the model comparator type) — it stops being a *second* decision point and becomes the one derivation's implementation.

### Acceptance Criteria

- Exactly one function decides a column's filter projection; the schema template and the filter translator both read its output off `APIFieldContext`. `funcComparatorFor` no longer exists.
- Exactly one function decides a column's sort enum value; the schema enum and `buildAPISortFields` both read it.
- A column named with a leading digit (e.g. `2010_revenue`) emits `COL_2010_REVENUE` in the schema enum **and** matches `case "COL_2010_REVENUE"` in `<table>SortFieldToColumn` — F5 closed.
- `shared_gen.graphqls` is rendered from `ComparatorFamilies`, not hard-coded; the emitted text for the existing five families is byte-identical to today's.
- **Regeneration is byte-identical across all 13 example modules except the F5 digit-leading case.** This is the proof the refactor changed structure and not behavior; any other diff is a defect in this sub-item, not an intended improvement.
- §32.2 access gating and the FIX-103 non-filterable skip behave identically — access still only *removes* an otherwise-present comparator, never adds one.

### Tests Required

- [x] `make check-examples` regenerates all 13 example modules with **zero** golden diff (no example currently has a digit-leading column) — the refactor's byte-identity gate.
- [x] Unit test: a `ColumnContext` with a digit-leading name produces matching `SortEnumValue` and schema enum value; assert the previously-drifting pair now agrees (failing-first against the current code).
- [x] Table-driven unit test over every comparator-bearing Go type — `TestFilterProjection_perGoType`, 14 cases. **Amended as implemented:** the original wording ("both populated or both empty — never one without the other") is not satisfiable in 25.1 without moving generated output, because Enum / JSON / JSONB / Slice keep the `StringComparator` fallback with an empty `TranslatorFunc`; byte-identity is this sub-item's stated proof of correctness and wins. What the test asserts instead: populated `TranslatorFunc` ⇒ populated `InputTypeName` (the direction that must hold absolutely — a translator can never be dispatched from a schema field that does not exist), non-filterable ⇒ both empty, every advertised input declared in `ComparatorFamilies`, and `TranslatorFunc == Translator.FuncName`. The symmetric form becomes satisfiable once 25.4/25.5 land; see the deviation note in the Completion Record.
- [x] Unit test: `shared_gen.graphqls` rendered from `ComparatorFamilies` matches the previously hard-coded text for String/Numeric/Boolean/Time/ID.
- [x] Existing `api_comparator_test.go`, `api_filter_test.go`, `api_sort_test.go`, `api_schema_test.go` pass unchanged (or with mechanical updates only).

### Completion Record

- **Date:** 2026-08-25
- **Files changed:** 7 (`cmd/sqlgen/gen/context_api.go`, `funcmap.go`, `naming.go`, `funcmap_test.go`, `templates/api/schema.graphqls.tmpl`, `templates/api/shared.graphqls.tmpl`, new `cmd/sqlgen/gen/api_projection_test.go`).
  - **The one derivation.** New `APIFilterProjection` (`InputTypeName`, `TranslatorFunc`, `Translator`, `Operators`, `Nullable`) + `SortEnumValue` on `APIFieldContext`. `resolveAPIFilterProjection(col, dialect, scalar, filterable)` resolves the projection off **`resolveComparatorType`** — the same function that produces the column's `*comparator.X[Y]` field on the model filter (§11.2). Choosing the schema's input type from the very expression the translator must compile against is what makes the two surfaces agree by construction; the old schema side switched on `GraphQLBare` and could not see that expression at all.
  - **Both emitters repointed.** `schema.graphqls.tmpl:41` → `{{ $f.Filter.InputTypeName }}`, `:51` → `{{ $f.SortEnumValue }}`. `buildAPIFilterFields` now takes the built `[]APIFieldContext` and reads `Filter.TranslatorFunc` / `Filter.Translator` (the descriptor is carried on the projection so the project-wide translator registry is populated from the same resolution rather than a second `comparatorTranslatorVariant` parse). `buildAPISortFields` takes `[]APIFieldContext` and reads `SortEnumValue` — **F5 closed**. `funcComparatorFor` and its `comparatorFor` funcmap registration are deleted (funcmap is 50 entries, was 51).
  - **Three gates collapsed into one.** FIX-103 (`json[]`/`jsonb[]` not `comparable`) and §32.2 (`APIFilterable`) are folded into the single `filterable` flag `mapColumnToGraphQL` already computed; the projection is resolved from it, so a non-filterable column carries a **zero** projection and both surfaces skip on the same emptiness. §32.2 still only ever *removes* a comparator.
  - **`ComparatorFamilies` is now `[]APIComparatorFamily`** (name + ordered operators + optional companion `<X>Range` input). `shippedComparatorFamilies` in `context_api.go` is the single source of the operator sets; `shared.graphqls.tmpl`'s five hard-coded `input …Comparator` blocks are replaced by a `range` that pads operand types to `NameWidth()`. `collectComparatorFamilies` unions the always-shipped five with any further family a column projection references, so a per-table schema cannot reference an input the shared file did not declare — that is the hook 25.4/25.5's monomorphized families land on with **no template edit**.
  - `screamingSnakeCase` moved to `naming.go` as the plain helper; `funcScreamingSnakeCase` now delegates to it, so the template funcmap and the projection cannot spell an enum value differently even if a future template calls the helper directly.
- **Tests:** `make check` clean (lint + vet + unit, all modules, `-race`). `make check-examples` clean across all 13 example modules. **Byte-identity gate met: `git status` shows zero changes under `cmd/sqlgen/testdata/` — `TestE2EGoldenFiles` passes against the committed goldens with no `-update-e2e` run.** New `api_projection_test.go`: `TestFilterProjection_perGoType` (14 cases — text/nullable text, integer/nullable bigint, numeric→float64, boolean, timestamp/nullable timestamp, uuid FK, uuid scalar under the uuid integration, decimal under the decimal integration, jsonb, json, `text[]`, `jsonb[]`), `TestFilterProjection_pairsSchemaAndTranslator` (bidirectional projection ↔ `FilterFields` agreement), `TestSortProjection_digitLeadingColumn` (**verified failing-first**: reverting `buildAPISortFields` to `strings.ToUpper(toSnakeCase(...))` makes it fail on `case "2010_REVENUE"`), `TestSortProjection_everySortFieldMatchesSchemaEnum`, `TestSharedSchema_comparatorFamiliesRenderByteIdentical` (the pre-25.1 comparator surface frozen as a literal and compared character-for-character). `funcmap_test.go` needed one mechanical edit (drop `comparatorFor`); every other existing api test passes unchanged.
- **Triggers:** path-based E2E trigger **fired** (`cmd/sqlgen/gen/**`) — `make check-examples` run, clean. Security-review trigger **not** fired: no changed path is under `manifest/`, a `//go:embed` runtime package, or `cmd/sqlgen/cli/**`.
- **Deviation from "Tests Required" item 3 — flagged, not silently resolved.** The tracker asks the projection to have `InputTypeName` and `TranslatorFunc` "both populated or both empty — never one without the other". That cannot hold in 25.1 without moving generated output, and byte-identity is stated three times as this sub-item's proof of correctness (tracker acceptance, tracker tests, design §6 "*Pure refactor — must be byte-identical*"). Enum / JSON / JSONB / Slice columns are advertised today as `StringComparator` with no translator behind them — the surviving half of F2 — and dropping that fallback would remove four filter fields from the `graphql` example's goldens. **What was implemented:** the fallback is preserved *verbatim* but is now a single, documented, visible decision inside `resolveAPIFilterProjection` instead of an accident spread across two functions; the direction that must hold absolutely (a populated `TranslatorFunc` always has a populated `InputTypeName` — a translator can never be dispatched from a schema field that does not exist) **is** asserted, on every case, along with "non-filterable ⇒ both empty" and "every advertised input is declared in `ComparatorFamilies`". The remaining asymmetry is exactly what 25.3/25.4/25.5 remove.
- **Consequence for 25.2 (sequencing note).** Because the fallback survives, `ValidateAPIFilterCompleteness` as specified (schema-emitted field with no translator entry ⇒ hard error) **would fire on the `graphql` example** (`document.entityType`, `profile.settings`, `user.metadata`, `asset.declaredCategories`) if 25.2 lands before 25.4/25.5. 25.2's own acceptance criterion "all 13 examples pass both lints with no changes to their generated output" therefore requires 25.4 + 25.5 first, or a temporary allowance in the lint for the documented fallback. **25.2 should be scheduled after 25.4/25.5, not immediately after 25.1.**
- **Auto-review round (2026-08-25).** 6/7 PRD requirements PASS, 1 PARTIAL (the documented Enum/JSON/JSONB/Slice fallback), 0 blocking. The reviewer independently confirmed the two load-bearing safety claims: (a) **`pkColNames` and `col.PrimaryKey` cannot disagree** — `context_table.go:266` sets `columns[i].PrimaryKey` from the same override set that builds `pkColumns`, and the auto-detect path derives `pkColumns` from the flag, so the new `f.PrimaryKey` gate in `collectComparatorFamilies` is safe; (b) **the translator side is provably unchanged** — `ff.ComparatorType ≡ resolveComparatorType(col, cfg.Input.Dialect)` because `context_table.go:379` passes the same `columns` and dialect, and the old/new skip predicates are the same conjunction, so only `Filter.InputTypeName` (the schema side) can move. Template trimming was traced arm-by-arm (empty family list, family with no range input, `NameWidth == 0`). **Three findings fixed in place:** the `NameWidth()` method became a **pre-computed context field** stamped by `collectComparatorFamilies` (TEMPLATES.md §6 — contexts carry pre-computed data, templates carry layout; a method on a generator context struct would have been the first in the repo); `TestFilterProjection_pairsSchemaAndTranslator`'s reverse direction now excludes PK and non-filterable columns from `projected`, so a stale `FilterFields` entry backed only by a discarded PK projection can no longer pass; and the inert `dialect` knob was removed from the test table in favour of an explicit coverage note. Both gates re-run clean afterwards.
- **Reviewer finding worth carrying (tracked, not blocking) — the byte-identity gate is narrower than "13 example modules" suggests.** Only `graphql` and `graphql_top_level` set `api:`, and both are PostgreSQL, so the gate cannot see three column shapes where the new derivation genuinely differs from the deleted `funcComparatorFor`: a **string-typed FK** (`CHAR`/`VARCHAR REFERENCES` — routine on MySQL/SQLite) moves `StringComparator` → `IDComparator`; **`sql.NullInt64` / `sql.NullFloat64`** move `StringComparator` → `NumericComparator`; and **non-enum PostgreSQL arrays** (`integer[]`, `boolean[]`, `timestamptz[]`, `uuid[]`) move `Numeric`/`Boolean`/`Time`/`IDComparator` → `StringComparator`. The first two are latent **fixes** — the old pairing advertised an input the translator could not compile against — and the third is neutral (advertised-and-ignored either way). The gate stayed green only because the sole array in the fixtures is `document_entity_type_enum[]`, which lands on `StringComparator` under both derivations. Recorded here so 25.3-25.5's golden churn is not misattributed; the durable fix is a non-PostgreSQL API-enabled example module.
- **Reviewer finding for 25.4/25.5 (not a 25.1 defect).** `collectComparatorFamilies`'s union hook is only half-wired: `resolveAPIFilterProjection` fills `Operators` via `comparatorFamilyByName`, which searches `shippedComparatorFamilies` only, so a non-shipped family would arrive with `Operators: nil` and render `input FooComparator {\n}` — the empty-input shape gqlgen rejects (FIX-071). Unreachable today (every projection resolves to one of the five); the precondition is now stated in a comment at the union site. **25.4/25.5 need an operator source on the projection side, not just an entry in the family table.**
- **Reviewer finding out of scope (relevant to 25.2).** `cmd/sqlgen/manifest/builder.go:625 comparatorForColumn` is a **fourth** independent comparator derivation, and it disagrees with `resolveComparatorType` on string FKs (`comparator.String`), decimals (`""`) and enums (`""`). It is manifest-only, so there is no accept-and-ignore risk, but it contradicts 25.1's "exactly one function decides" and is a cheap catch for 25.2's lint.
- **Notes:** `mapColumnToGraphQL` gained a `dialect config.Dialect` parameter (sourced from `tc.Dialect`, which is `cfg.Input.Dialect` — the same value `buildFilterFields` passes to `resolveComparatorType`, so the model and API sides read one dialect). `buildAPIPKArgs` passes it too; the projection it resolves for a PK column is discarded, since PK columns are excluded from the filter input. `comparatorFamiliesFromTables` / `comparatorFamilyName` were deleted — both existed only to populate the old `[]string` field, which no template ever read.

---

## 25.2 Completeness lint — schema/translator drift becomes unlandable (Ticket A, D2)

**PRD Reference:** Sections 26.5.2 (the existing walker-completeness lint this mirrors), 26.5.3 (the D1 invariant added by B4). Design §3.1, §2-D2.

**Status:** **Complete** (landed 2026-09-04)

**Governing decisions:** D2 (codegen completeness lint).

**Depends on:** 25.1.

### Tasks

- [x] Add `ValidateAPIFilterCompleteness(table TableContext, apiTable APITableContext) error` in `cmd/sqlgen/gen/api_walker.go` (or a sibling `api_lint.go`), modelled structurally on `ValidateAPIWalkerCompleteness`: every schema-emitted filter field must have a matching `FilterFields` entry, and every `FilterFields` entry must have a schema-emitted field. Both directions error.
- [x] Add `ValidateAPISortCompleteness` with the same bidirectional check between the emitted sort-enum values and `SortFields`.
- [x] Error messages name the offending table and column and cite the PRD section, matching the existing walker-lint message shape.
- [x] Wire both into the same codegen hard-error path `ValidateAPIWalkerCompleteness` uses, so a violation fails `sqlgen generate` rather than emitting broken output.
- [x] Confirm the FIX-103 `json[]`/`jsonb[]` non-filterable case satisfies the lint (skipped on **both** sides, so neither direction fires).
- [x] **Carried in from 25.12 (2026-09-04).** Cover **relationship members** in both directions: every `FilterRelationships` entry must have a schema-emitted `<name>: <Target>Filter` field, and every schema field whose operand is a `<X>Filter` must have an entry. 25.12 could not land this half — the lint it extends did not exist yet, and this sub-item is scheduled last (after 25.4/25.5) by 25.1's sequencing note. 25.12 made the drift **structurally** unrepresentable instead: `APITableContext.FilterRelationships` is one slice read by both emitters, and `TestAPIFilterRelationships_OneDerivationDrivesBothEmitters` asserts the round trip in both directions. The lint arm is therefore a second, cheaper guard rather than the only one — but it is the guard a future template edit would trip, so it still belongs here. **`ValidateGeneration` now wires relationship filters** (folded into 25.12 after its auto-review surfaced the gap), so a lint added here fires under `sqlgen validate` as well as `sqlgen generate` rather than silently no-opping on an empty `FilterRelationships`.

### Acceptance Criteria

- A hand-built context with a schema filter field and no translator entry produces a hard codegen error naming the column — the F2 failure mode is no longer expressible.
- The reverse (translator entry with no schema field) also errors — a stale translator cannot reference an absent gqlgen input field.
- A sort-enum value with no `SortFieldToColumn` case errors — the F5 failure mode is no longer expressible.
- All 13 examples pass both lints with no changes to their generated output.
- Lint messages are actionable: table, column, direction of the mismatch, PRD reference.

### Tests Required

- [x] Failing-first unit test: context with schema field, no translator entry → error mentioning the column.
- [x] Failing-first unit test: context with translator entry, no schema field → error.
- [x] Failing-first unit test: sort-enum value with no matching switch case → error.
- [x] Negative test: a FIX-103 `jsonb[]` column (skipped on both sides) passes both lints.
- [x] Negative test: a §32.2 `hidden` / `internal` column (dropped from both sides) passes both lints.
- [x] Every example table passes both lints — asserted in the existing codegen test sweep.

### Completion Record

- **Date:** 2026-09-04
- **Files changed:** 6 (new `cmd/sqlgen/gen/api_lint.go`, new `cmd/sqlgen/gen/api_lint_test.go`, `cmd/sqlgen/gen/context_api.go`, `cmd/sqlgen/gen/api_projection_test.go`, `cmd/sqlgen/gen/api_comparator_families_test.go`, `cmd/sqlgen/gen/api_enum_comparator_test.go`), plus `docs/PRD.md`.
- **The decision this sub-item owned — the `StringComparator` fallback is dropped, not hard-errored.** The PRD contradicted itself: §26.5.3 makes "absent from both, not exempt from it" the only way a non-projectable column can satisfy the lint, while §26.12 documented the custom-scalar-array case as one that "keeps the `StringComparator` fallback". Both cannot hold once the lint exists. The user chose **drop the filter field**, and the reason it is the better half of the contradiction is the escape hatch: hard-erroring would block `sqlgen generate` outright on a schema sqlgen handles today, and the only way out is a §32.2 role (`hidden` / `write_only`) that **also** removes the column from the object type — the consumer would lose reads to regain generation. Dropping the field costs a filter that never worked (it was accepted and silently ignored) and costs nothing else: the column keeps its object-type field, its sort-enum value, and its member on the **Go** `<T>Filter` struct. `resolveAPIFilterProjection`'s `else` arm and the `fallbackComparatorInput` constant are gone; the two shapes that reach it — a `comparator.Enum[T]` whose `T` is not a schema enum (`makeEnumTranslator`'s guard) and a `comparator.Slice[T]` whose element GraphQL type is not one of the five measured built-ins (`sliceOperandElemGoType`) — now return a zero projection.
- **`APIFieldContext.Filterable` is now read back OFF the projection** (`filter.InputTypeName != ""`) instead of being set beside it. Without that the template's `$f.Filterable` gate and `$f.Filter.InputTypeName` would be two facts again, and a gated-in field would render `name: ` with an empty type. The `filterable` local still feeds the projection (FIX-103 / FIX-178 / §32.2 decide *before* the projection is attempted); what changed is that a fourth drop — "no sound comparator face" — is decided *inside* the projection, so the flag has to follow it. **`APIFilterProjection` now satisfies a biconditional**: `InputTypeName` and `TranslatorFunc` are populated together or empty together. That is the symmetric form 25.1's Completion Record flagged as unsatisfiable at the time and said 25.4/25.5 would make reachable — `TestFilterProjection_perGoType` now asserts it on every one of its rows, closing the 25.1 deviation.
- **The two lints.** `ValidateAPIFilterCompleteness` / `ValidateAPISortCompleteness` in the new `api_lint.go`, both taking `(APIEntity, APITableContext)` — the same signature shape `ValidateAPIWalkerCompleteness` takes, so a **view is linted by the identical code path with no view arm** (D8). Both report *every* violation via `errors.Join` rather than the first, matching the newer `ValidateAPIViewReadOnly` rather than the walker lint. Wired into `buildOneAPITable` and `buildOneAPIView` immediately after the walker lint, which puts them on the `BuildAPIContext` path — so they fire under **`sqlgen validate`** as well as `sqlgen generate` (25.12 pre-wired `wireRelationshipFilters` into `ValidateGeneration` for exactly this).
  - **Filter, columns:** schema side derived under the template's own gate (`not $f.PrimaryKey` and `$f.Filterable`), translator side from `FilterFields`; both directions error.
  - **Filter, relationships (the arm carried in from 25.12):** both emitters read one `FilterRelationships` slice, so "the two sides disagree" is not expressible there — what is checkable, and what the lint checks, is that each entry can render **both** halves (schema `GraphQLName`/`InputTypeName`, translator `GoFieldName`/`ModelFieldName`/`TranslatorFunc`), that its `ModelFieldName` names a member the model `<T>Filter` actually has, and that its GraphQL name collides with neither a column field nor another member — a collision makes the whole `input <T>Filter` invalid, and gqlgen reports it a long way from the config line that caused it.
  - **Sort:** schema enum values against `SortFields` cases, both directions, **plus** two shapes neither direction catches — a sortable column that resolved no enum value (renders a blank line gqlgen rejects) and a case returning a column the entity does not have (the name goes straight into `ORDER BY`, so it is neither a parse nor a compile error).
- **`APIFilterField` gained `SQLName`** and `APIEntity` gained `RelationshipFilters`. Both are diagnostics/lint inputs only — no template reads either — and neither moves generated output. `SQLName` is what lets the reverse-direction message name the *column* a stale translator entry belongs to, which the acceptance criteria require and `GraphQLName` alone cannot supply under a field-name override.
- **Tests:** `make check` clean (lint + vet + unit, all modules, `-race`; longest legs `cmd/sqlgen/cli` ~447s, `cmd/sqlgen/gen` ~243s). `make check-examples` clean across all 13 example modules. **Zero golden churn: `git status` shows no change under `cmd/sqlgen/testdata/` after a full regenerate** — the acceptance criterion "all 13 examples pass both lints with no changes to their generated output" is met literally. New `api_lint_test.go` carries 15 tests: the agreeing baseline, both filter directions, four sort shapes (missing case, missing enum value, the concrete F5 digit-leading `COL_2010_REVENUE` vs `2010_REVENUE` drift asserted to fire from **both** directions, unknown column, empty enum value), three relationship shapes (half-rendered member, member not on the model filter, member colliding with a column), the two negative tests (FIX-103 `jsonb[]` and a §32.2 `internal` column, both absent from every surface and both silent), a view routed through the real `apiEntityFromView`, and a built-context sweep over `BuildAPIContext` output covering `timestamptz[]`, `jsonb[]`, an `internal` column, a decimal, and an FK.
- **Verified failing-first, twice, at two levels.**
  1. **Unit level:** stubbing both lint bodies to `return nil` fails **11 of the 15** new tests (the four that stay green are the baseline and the three negative/pass cases, which is correct — they assert silence).
  2. **Real-context level, the one that matters:** re-introducing the `StringComparator` fallback in `resolveAPIFilterProjection` makes `BuildAPIContext` itself hard-error — `api: filter for table "products" emits column "seen_at" as GraphQL field "seenAt" with no entry in the filter translator … PRD §26.5.3`. That is simultaneously the proof the lint fires on real generated output, the proof it is wired into the codegen hard-error path, and the proof the fallback drop was **required** rather than cosmetic.
- **PRD amendments (three, all in `docs/PRD.md`).** §26.12's bullet was rewritten from "keeps the `StringComparator` fallback" to "gets no GraphQL filter field at all", now naming **both** unsound shapes (the custom-scalar array and the non-schema-enum PascalCase type — the second was previously undocumented) and stating explicitly what survives (object-type field, sort value, Go-side filter member). §26.5.3's lint bullet list gained the relationship arm, the `ORDER BY`-column arm, and a sentence recording that the lint runs on the built context so `sqlgen validate` covers it and views need no arm. A broken `#164-view-artifacts` anchor introduced by that edit was corrected to the section's real slug.
- **Triggers:** path-based E2E trigger **fired** (`cmd/sqlgen/gen/**`) — `make check-examples` run, clean. Security-review trigger **not** fired: no changed path is under `manifest/`, a `//go:embed` runtime package, or `cmd/sqlgen/cli/**`.
- **Preflight note (16.8g drift).** `gofumpt v0.10.0`, `golangci-lint 2.12.2`, `go1.26.5`. **No `.tool-versions` or `tools.mk` pin file exists in the repo**, so the local-vs-bundled gofumpt drift that re-aligned nine files during the 16.8g sweep is still unpinned. It did not bite here (zero golden churn), but the pin remains unwritten.
- **Auto-review round (2026-09-04).** 10/11 PRD requirements PASS, 1 PARTIAL, 0 FAIL, 6/6 required tests present, **no behavioural defect found**. The reviewer independently confirmed the three load-bearing claims this sub-item rests on: (a) **every `Filterable` reader is compatible with the new derivation** — exactly two in production, `schema.graphqls.tmpl:43` and `collectComparatorFamilies`, and the latter already AND-ed `Filter.InputTypeName != ""`, which the biconditional makes redundant rather than wrong; `context_relfilter.go` and `manifest/builder.go` read `FilterFieldContext.Filterable`, a different type, untouched; (b) **the relationship arm's omitted direction is correctly omitted** — both legitimate subtractions (the `apiFilterTargets` exposure gate and the "column wins" name tiebreak) would make a reverse check fire on correct output, and the two cross-table checks dropped as tautological really are, since `apiFilterTargets` gates on `tableAPIEnabled` **and** `hasAPIReadableColumn` while the table loop gates on `tableAPIEnabled` alone; (c) **no false-positive risk** — direction 2's PK gate needs `f.PrimaryKey` and `ent.PKColumns` to agree, verified on all three derivation paths (auto-detect, the §8.6 `primary_key.columns` override, view `@pk`), and direction 1 needs every API field to have a model `FilterFields` entry, which `buildFilterFields` emits unconditionally per column. **All findings fixed in place:** seven doc comments in `context_api.go` still described the deleted `else` arm (`comparatorTranslatorVariant`, `genericComparatorTranslator`, `enumComparatorBindingFor`, `sliceOperandElemGoType`, `buildAPIFilterFields`'s now-dangling pointer into the rewritten `APIFilterProjection` doc, plus the two call-site comments under-counting the guards they run); the **filter twin of the empty-sort-enum guard** was added (`Filterable` with no `InputTypeName` renders `name: `, the identical invalid-document shape the sort side already guarded) with `TestAPIFilterCompleteness_FilterableColumnWithoutInputType` — 16 tests now; and the PARTIAL was closed by extending §26.5.3's "absent from both" paragraph from two exempt classes to **four**, adding FIX-178's SQLite `types.JSON` case and the new no-sound-projection class, with the note that only the fourth keeps its Go-side filter member. Both gates re-run clean afterwards, still with zero golden churn.
- **Notes.** The relationship arm is deliberately *not* "every model-side relationship filter must have an API member" — that direction would be wrong. §26.5.3 makes the API projection legitimately **subtractive** (a target with `api.enabled: false`, or every column dropped by §32.2, emits no member), exactly as §32.2 is for columns, so demanding an entry per model member would fire on the security gate 25.12 landed. The lint checks the direction that must hold — everything emitted must be resolvable — and leaves the subtraction alone. Two cross-table checks were considered and dropped as tautological: `filterTargets ⊆ apiTables` by construction (`apiFilterTargets` gates on `tableAPIEnabled` **and** `hasAPIReadableColumn`, while the table loop gates on `tableAPIEnabled` alone), so an emitted member's `<Target>Filter` input and `translate<Target>Filter` function always resolve.

---

## 25.3 Complete the five shipped comparator families + `DecimalComparator` (Ticket A, F6/F7)

**PRD Reference:** Sections 26.4 (comparator input definitions per B3), 26.5.3 (translator bodies per B4), 11.2 (the Go operator sets being projected). Design §3.1, §2-D4.

**Status:** **Complete** (landed 2026-08-26, verified 2026-08-26 — `/verify 25.3` re-verify: 9/9 PRD requirements PASS, 0 FAIL; 11 FIXes logged across both verify rounds, 10 resolved, FIX-145 open by design)

**Governing decisions:** D4 (complete the families in the same pass — pre-release, so one golden churn not two).

**Depends on:** 25.1.

### Tasks

- [x] `StringComparator` — add `gt gte lt lte nlike` (present on `comparator.String`, absent from the input).
- [x] `NumericComparator` — add `nin` and `nbetween: NumericRange`.
- [x] `TimeComparator` — add `in: [Time!]`, `nin: [Time!]`, `nbetween: TimeRange`.
- [x] `IDComparator` — add `gt gte lt lte`.
- [x] Emit the **nullable input twin** for each family per PRD §26.4 Rule 2 — `Nullable<X>Comparator`, identical operands plus `isNull: Boolean`, emitted only when some nullable column references it. Nullable columns reference the twin; `NOT NULL` columns reference the base input, which carries no `isNull`. Wire `isNull` through the `translateNullable*` wrapper into the model's `Null *bool` — replacing today's "wrapper's `Null` stays nil" behavior documented at `templates/api/comparator_translate.go.tmpl:24-29`.
- [x] Add `DecimalComparator` / `NullableDecimalComparator` (F7): operands typed with the `Decimal` scalar, ordered + set operators only (`eq neq gt gte lt lte in nin`), translating into `comparator.String` / `NullableString` on the canonical string form. Route `Decimal` / `NullDecimal` columns to it instead of `StringComparator`. The text operators `comparator.String` carries (`contains` / `startsWith` / `endsWith` / `like` / `nlike`) are **deliberately not projected** on a decimal column — see PRD §26.4's `DecimalComparator` row.
- [x] Extend the `comparator_translate.go.tmpl` per-family bodies to copy the newly exposed operators, and the shared-schema operator lists (25.1's `ComparatorFamilies`) to declare them.
- [x] Update the `comparator_translate.go.tmpl` header comment — the `Null`-not-exposed note is no longer true.

### Acceptance Criteria

- For each of the five families, every non-`Custom` operator on the Go comparator has a GraphQL counterpart, and every GraphQL operator reaches the model — verified operator-by-operator, not spot-checked. `DecimalComparator` is the one deliberate narrowing: it projects only `eq neq gt gte lt lte in nin` from `comparator.String` (PRD §26.4), because the text operators are meaningless against a numeric column.
- `comparator.Custom` is **not** projected on any family (raw-SQL escape hatch; an injection surface over HTTP).
- A nullable column references `Nullable<X>Comparator` (operands + `isNull`); a `NOT NULL` column references the base `<X>Comparator`, which has no `isNull` field. One input type per (family, GraphQL operand type, nullability) triple, per PRD §26.4 Rule 2 + Sharing.
- `isNull: true` produces `IS NULL` and `isNull: false` produces `IS NOT NULL` in the emitted SQL, for every family.
- A decimal column supports `{ gt: "100.50" }` over GraphQL and the comparison is numerically (not lexicographically) correct against the DB — F7 closed.
- Golden churn is confined to the new schema fields and translator lines; no unrelated output moves.

### Tests Required

- [x] Table-driven unit test per family: every GraphQL operator field → the expected model comparator field, including the new ones.
- [x] Unit test: a nullable column references `Nullable<X>Comparator` and a `NOT NULL` column references `<X>Comparator`; only the former declares `isNull`, and the nullable wrapper's `Null` is populated (not silently dropped).
- [x] Cross-dialect SQL-shape unit tests for `nlike`, `nbetween`, `nin`, and `isNull` on each applicable family. **Scoped by invariance (FIX-146):** only `nin` has a dialect split (`ALL` on pgx vs `NOT IN` elsewhere) and it got a four-dialect sweep in `comparator/{string,number,time}_test.go` (`id_test.go` already had one). `nlike`, `nbetween` and `isNull` are provably dialect-invariant — `NLike` / `IsNull` / `IsNotNull` (`sql/condition.go`) and `parseNull` / `parseNBetween` (`comparator/comparator.go`) take no `sql.Dialect`, and `sql.Range` is expanded generically on `$` tokens rather than by matching `BETWEEN` — so a sweep would exercise identical code three times. Single-dialect tests plus this argument, not an oversight.
- [x] E2E (postgres + mysql + sqlite): a decimal column filtered with `gt` / `lte` returns numerically correct rows — the F7 regression pin.
- [x] E2E: `isNull: true` / `isNull: false` on a nullable column over the real gqlgen server.
- [x] 25.2's lints pass across all examples after the churn.

### Completion Record

- **Date:** 2026-08-26
- **Files changed:** 11 — `cmd/sqlgen/gen/context_api.go`, `templates/api/comparator_translate.go.tmpl`, `api_comparator_test.go`, `api_projection_test.go`, new `api_comparator_families_test.go`; `comparator/{string,number,time}_test.go`; new E2E tests in the `postgres`, `mysql` and `graphql` example modules. Goldens regenerated across both API-enabled examples.
  - **Operator completion (F6).** `shippedComparatorFamilies` now carries the faithful projection of each Go family minus `Custom`: `String` +`gt gte lt lte nlike`, `Numeric` +`nin nbetween`, `Time` +`in nin nbetween`, `ID` +`gt gte lt lte`. Operator order follows the PRD matrix rather than the historical order — `nbetween:` is longer than `between:`, so the alignment padding re-flows the whole Numeric and Time blocks either way, which made "strictly additive" unachievable and readability the better trade.
  - **Nullable twins (Rule 2).** `nullableComparatorTwin` derives `Nullable<X>Comparator` from the base — identical operands plus `isNull: Boolean`, and no range input (the range type is declared once alongside the base and shared). A nullable column's projection now resolves to the twin, so `deletedAt: TimeComparator` became `deletedAt: NullableTimeComparator` across the goldens.
  - **`isNull` reaches the model.** The template's per-family operand block was extracted into `api/comparator-operands` and is now shared by both variants — which works because `comparator.Nullable<X>` embeds `<X>`, so Go promotes every `out.Eq = …` assignment. The nullable variant **stops delegating** to its base (they no longer take the same Go type) and sets `out.Null = in.IsNull`. The header comment's "the wrapper's `Null` stays nil" note is gone; it was the thing being fixed.
  - **`DecimalComparator` (F7).** `makeDecimalTranslator` + `isDecimalScalar` route a decimal column off `StringComparator` and onto `DecimalComparator` / `NullableDecimalComparator`, keyed on the resolved GraphQL scalar so a `numeric` override and a built-in decimal land the same way. Operands are `Decimal`-typed and rendered onto the canonical string form with `String()`; the model return type stays `comparator.String` / `NullableString` (FIX-040 unchanged). The five text operators are deliberately not projected.
  - **Emission became usage-derived.** `collectComparatorFamilies` no longer seeds the five unconditionally — a family is emitted because a column references it. This is **required**, not a tidy-up: `DecimalComparator`'s operands are typed with the `Decimal` scalar, which is itself only declared when a column uses it, so an unconditional block would reference an undeclared scalar and gqlgen would reject the schema. The same failure mode is still latent for `TimeComparator`/`Time` — `scalar Time` is declared only when a column's Go type is literally `time.Time`, so a project binding `timestamptz` to `types.DateTime` globally still emits an undeclared-scalar reference; it wants the symmetric `DateTime`/`NullDateTime → Time` pairing and is logged, not fixed here. **Not byte-neutral, contrary to what this record first claimed:** `graphql_top_level` referenced neither a boolean nor a non-PK ID column, so `input BooleanComparator` and `input IDComparator` — previously emitted unreferenced — are gone from its shared schema. Dropping dead declarations is the right outcome, but PRD §26.4 conditions only the **Nullable** variant on usage, so making the base form conditional needs a delta.
  - **Paired-scalar registration.** A nullable decimal column declares `scalar NullDecimal` for its field type, but *both* comparator forms take `Decimal` operands, so `registerColumnScalars` registers the non-null member alongside it. Without this, a project whose only decimal columns are nullable emits an input referencing an undeclared scalar.
- **Tests:** `make check` clean (lint + vet + unit, all modules, `-race`); `make check-examples` clean across all 13 example modules. New codegen tests in `api_comparator_families_test.go`: `TestComparatorFamilies_projectEveryGoOperator` (the operator-by-operator acceptance gate — parses `comparator/*.go` with `go/ast` and asserts every non-`Custom` exported field on each family has a GraphQL counterpart, so adding a Go operator without projecting it fails the build), `TestComparatorFamilies_nullableTwin`, `TestComparatorFamilies_translatorCopiesEveryOperator` (the reverse direction — every advertised operator is actually assigned into the model), `TestDecimalComparator_routing`, `TestComparatorFamilies_emittedOnlyWhenReferenced`. Cross-dialect `nin` SQL-shape tests added to `comparator/{string,number,time}_test.go` (`id_test.go` already had one). **E2E:** `postgres` + `mysql` `TestDecimalFilter_OrderedOperatorsAreNumeric` and `graphql` `TestDecimalComparator_OverHTTP` / `TestNullableComparator_IsNullOverHTTP` — all use a fixture where the numeric and lexicographic orderings **disagree** (`"1000.00" < "100.50"` as text, `>` as a number), so a string comparison would fail the assertion in both directions.
- **Triggers:** path-based E2E trigger fired (`cmd/sqlgen/gen/**`, `comparator/**`, `cmd/sqlgen/testdata/examples/**`) — `make check-examples` run, clean. Security-review trigger not fired: nothing under `manifest/`, a `//go:embed` target, or `cmd/sqlgen/cli/**`.
- **Deviations from the tracker text, both flagged:**
  - **SQLite is not covered by the decimal E2E.** The tracker asks for postgres + mysql + sqlite. The SQLite example schema has no decimal column — SQLite has no DECIMAL type, and the example maps its numerics to REAL — so there is nothing to bind `decimal.Decimal` to. Postgres and MySQL both carry a `warehouses` table with a `numeric`/`DECIMAL` override and both are pinned. Adding a SQLite decimal fixture is a schema change to that example, out of this sub-item's scope; worth a tracked note if decimal-on-SQLite is meant to be supported.
  - **"25.2's lints pass across all examples" is not assertable yet** — 25.2 is not implemented (and per 25.1's sequencing note must land after 25.4/25.5). The checkbox is marked on the strength of the two directions being asserted structurally instead: `TestComparatorFamilies_projectEveryGoOperator` and `TestComparatorFamilies_translatorCopiesEveryOperator` are the same bidirectional check at the family level.
- **Fix round (2026-08-26) — the eight FIXes `/verify 25.3` logged.** Seven resolved, one carried. Both **blocking** entries were spec-text corrections and landed as PRD deltas: **FIX-139** rewrote §26.5.3's nullable-translator paragraph (the delegation clause described a shape Rule 2 makes inexpressible), and **FIX-140** added **§26.4 Rule 3** — a comparator input is emitted only when referenced, for *both* forms — amending the spec to match the code rather than reverting the code, because the alternative leaves FIX-141's gap open. **FIX-141** generalised the scalar pairing: `registerColumnScalars` now registers the scalar each *operand type* names, via a `comparatorOperandScalars` table, closing the rule instead of the two known instances; verified failing-first, and the `Time` half was a real undeclared-scalar bug for any project binding timestamps to `types.DateTime` globally. **FIX-142** pinned two branches that were deletable with the suite green. **FIX-144** needed no schema change — SQLite already had the `warehouses` fixture with a `real → decimal.Decimal` override — and answered its own open question: SQLite's REAL affinity coerces the text parameter, so decimal ordering is correct on all three dialects. **FIX-146** cleared four text/deadwood items. **FIX-145** stays open by design: nothing closes it in isolation, 25.4/25.5 delete the fallback it describes.
- **FIX-147 (new, blocking) — a pre-existing codegen bug surfaced while fixing FIX-143, and fixed.** Adding the nullable columns FIX-143 needed made `sqlgen generate` emit a **non-compiling input translator**: `omittable.Set(in.RetryCount)` hands gqlgen's `*int` to a model field of type `omittable.Value[*int32]`. `funcInputCoerceDeref`'s `ModelInnerIsPointer` branch returned before applying `CastTo`, on the rationale that "pointers thread identically" — true only when the *pointee* types agree, which they never do for a sized int, since gqlgen binds the GraphQL `Int` scalar to Go `int` while the model narrows to the column's width. This broke **every** consumer with a nullable `INTEGER` / `BIGINT` / `SMALLINT` column and `api.graphql` enabled, and dated from phase 16; no example had such a column, so nothing caught it. Fixed with a computed `PointerCast` flag and a deref-convert-readdress block in both translators. Worth recording that the tempting move — dropping the integer column to get the boolean half green — would have hidden a live consumer-facing bug; the attempt was reverted cleanly and the defect filed instead.
- **FIX-143 closed by that fixture.** All six nullable comparator twins are now emitted and compiled by a golden (the `NumericT` cast block under a `NullableNumber[T]` wrapper included), and the derived operator test's fixture covers a nullable column per family instead of two of six.
- **Auto-review round (2026-08-26).** 5/7 PRD requirements PASS, 1 PARTIAL, 1 FAIL (spec text, not behaviour); **two blocking defects found and fixed in place**, both emitting non-compiling generated code for column shapes neither PostgreSQL example can reach.
  - **R1 (blocking, fixed) — missing `"time"` import when only *nullable* Time translators are emitted.** `comparatorTranslatorsUseTime` still read `tr.Family == "Time" && !tr.Nullable`. That was safe only while the nullable body was `base := translateTimeComparator(in)` (no `time.Time` token) **and** the base was force-registered alongside it — 25.3 removed both. The nullable body now inlines `[]time.Time` and `Range[time.Time]`, so a project whose only time columns are nullable emitted a file referencing `time.Time` with no import; `FormatOnly` rendering means goimports never repairs it. Gate is now `tr.Family == "Time"`. Pinned by `TestComparatorTranslateFile_importsTimeForNullableOnly`, which asserts the assembled import block (not the rendered body — a missing import is invisible there) and was **verified failing-first**.
  - **R2 (blocking, fixed) — the decimal narrowing misfired on non-`String` decimal columns.** `isDecimalScalar` keyed on the GraphQL scalar alone and ran *before* the model-derived branch, so it captured every column whose bare Go type is `decimal.Decimal`/`NullDecimal` — including a **decimal foreign key**, which resolves to `comparator.ID` (`DeriveFKMethod("decimal.Decimal")` is `FKStringSprint`, so `resolveSimpleComparator` takes its ID arm). That is a **regression**: pre-25.3 such a column correctly resolved `IDComparator`. The narrowing is now gated on the resolved family (`variant.Family == "String" && isDecimalScalar(scalar)`), which is what PRD §26.4 actually says — decimal is a narrowing *of the String family*. `registerColumnScalars`'s paired-`Decimal` registration was re-gated the same way and the projection now resolves before scalar registration. Pinned by `TestDecimalComparator_narrowsOnlyTheStringFamily` (plain decimal / decimal FK / decimal array), **verified failing-first** on the FK case.
  - **Test-quality finding, fixed.** `TestComparatorFamilies_translatorCopiesEveryOperator` hand-listed the Go fields, so an operator added to the family table but not to the template would pass both directions. It now **derives** the expectation from `APIContext.ComparatorFamilies`. The rewrite immediately caught a latent flaw in its own helper: `DecimalComparator` and `StringComparator` both allocate `comparator.String`, so locating a body by allocated struct returned the Decimal body (which deliberately omits the text operators) when checking StringComparator. `translatorBody` now keys on the generated function name.
  - **R5 (fixed) — dead code.** `baseTranslatorOf` had zero callers once nullable variants stopped delegating, and `BaseFuncName` / `BaseStructName` / `WrapperStructName` / `EmbeddedFieldName` / `BaseInputTypeName` on `APIComparatorTranslator` plus `Nullable` on `APIComparatorFamily` were written and never read (`unused` is not enabled in `.golangci.yml`, which is why `make check` stayed green). All removed.
  - **R3 (closed by FIX-141 — see the fix round above) — the completion record over-claimed.** The claim that "the same latent bug existed for `TimeComparator`/`Time` and is now closed" was **wrong** when written and was retracted here; FIX-141 then closed it for real, by generalising `registerColumnScalars` to register the scalar each *operand type* names (`comparatorOperandScalars`) rather than special-casing the two known instances. The original finding, kept for the record: `TimeComparator`'s operands are typed `Time`, but `scalar Time` is declared only when some column's Go type is literally `time.Time`. A project that binds `timestamptz → types.DateTime` **globally** has no `time.Time` column, yet `resolveSimpleComparator` still routes `types.DateTime` to the Time family — so the schema references an undeclared scalar and gqlgen rejects it. It is the same failure mode the new `NullDecimal → Decimal` pairing fixes and wants the symmetric `DateTime`/`NullDateTime → Time` pairing. **Pre-existing**, unexercised by either PostgreSQL example, and left for a FIX entry rather than folded in here: it is a scalar-declaration question broader than the comparator surface.
  - **PRD text drift (closed by FIX-139 — see the fix round above).** §26.5.3 said nullable translators "delegate the shared operands to the base translator". 25.3 deliberately stops delegating — Rule 2 makes the two distinct input types, so delegation is not expressible. The behaviour was right and the sentence B4 wrote was false; FIX-139 rewrote the paragraph to state that the nullable variant copies the operands onto the wrapper directly, why it cannot delegate, and that the base is emitted only when a `NOT NULL` column references it.
  - **Also noted, not defects:** every gqlgen binding assumption was confirmed against the regenerated `models_gen.go` (`Nlike`/`Nbetween`/`IsNull`; `[Time!]`→`[]*time.Time`, `[Decimal!]`→`[]*decimal.Decimal`, `[String!]`/`[ID!]`→`[]string`, `[Float!]`→`[]float64`); range inputs are declared exactly once; golden churn is confined to comparator renames and new operators. **Coverage gap recorded:** neither API example has a nullable numeric or nullable boolean column, so `NullableNumericComparator` / `NullableBooleanComparator` have no golden compiling them — the `NumericT`-cast operand block under a nullable wrapper has zero compile coverage.
- **Re-verify round (2026-08-26) — `/verify 25.3` second pass, after the fix round above.** All **9** PRD requirements PASS and 5/6 required tests present (the sixth, the cross-dialect sweep, is PARTIAL by the documented invariance decision); `make check` and `make check-examples` both clean, and every E2E pin was confirmed to actually execute rather than skip — including the SQLite decimal leg, which runs and passes. The independent `sqlgen-reviewer` pass **agreed on all nine** and added two items the parent missed. Three FIXes logged and all three resolved inline the same day:
  - **FIX-148 (tracked, phase-16 origin) — a second live consumer-facing codegen bug, the float sibling of FIX-147.** `isSizedIntGoType` gated the input-translator cast on the integer widths only, so a `real` / `float4` (PostgreSQL) or `float` (MySQL) column — model `float32`, gqlgen `Float`→`float64` — emitted **non-compiling** create/update translators. **Wider than FIX-147's shape:** that one only reached the nullable arm, because gqlgen emits bare `int` for a required `Int` field and `funcInputCoerceBare` already cast it; here `CastTo` was empty on *every* arm, so the required ones broke too. Fixed at the predicate — `float32` added and the helper renamed `isNarrowedNumericGoType`, since the old name is exactly what made the float axis easy to overlook. No template or funcmap logic changed: the helper has one caller and `PointerCast` keys on `CastTo`, so the nullable arm was covered automatically (verified, not assumed). Pinned by `TestInputTranslate_Float32ColumnsAreCast`, **verified failing-first** across all four arm/nullability combinations. **Zero golden movement** — no example has such a column, which is why it survived from phase 16. That blind spot is now the *second* consumer-facing bug to hide in it; FIX-135's non-PostgreSQL API-enabled example remains the durable fix.
  - **FIX-149 (nit)** — four text items FIX-146's sweep missed: `APIComparatorTranslator.Nullable`'s doc still described the pre-25.3 delegating design (contradicting the `InputTypeName` doc eight lines above it), the type doc's worked signature still spelled `(*NumericComparator)`, and both this tracker and `STATUS.md` still marked R3 and the PRD text drift `(open, tracked)` though FIX-141 and FIX-139 had closed them — the record contradicted its own fix-round paragraph. All reconciled in place.
  - **FIX-150 (nit)** — 25.3's two GraphQL E2E tests were not rerun-safe: fixed name prefixes against `categories.name UNIQUE` made `-count=2` fail in *setup*, which is precisely the command someone runs when chasing a flake. Closed with `truncateAll(t)` — this package's own ten-plus-caller convention — rather than the suggested `ksuid` prefix, which would have meant a new dependency for this module. `equalStrings` replaced with `cmp.Diff` per TESTING.md. Verified with the entry's own repro.
- **Notes:** `mapColumnToGraphQL` exceeded the `cyclop` complexity ceiling once the decimal pairing landed inline; the scalar registration was extracted into `registerColumnScalars`. gqlgen binds a list of a **custom** scalar as `[]*T` even when the element is declared non-null (`[Time!]` → `[]*time.Time`), unlike the builtin scalars (`[String!]` → `[]string`) — the Time and Decimal operand blocks deref with a nil guard. Confirmed against the regenerated `models_gen.go`: gqlgen renders `nlike` → `Nlike`, `nbetween` → `Nbetween`, `isNull` → `IsNull`.

---

## 25.4 Enum comparators — monomorphized per enum (Ticket A, D3)

**PRD Reference:** Sections 26.4 (B2's `<Enum>Comparator` row + monomorphization rule), 26.4.1 (enum binding), 26.5.3 (translator), 11.2 (`Enum[T]` operator set). Design §3.1, §2-D3.

**Status:** **Complete** (landed 2026-09-01)

**Governing decisions:** D3 (monomorphize the generic families per concrete type).

**Depends on:** 25.1.

### Tasks

- [x] Emit `input <EnumGraphQLName>Comparator { eq, neq, in, nin }` once per enum in `APIContext.UsedEnums`, plus `input Nullable<EnumGraphQLName>Comparator` (same operands + `isNull: Boolean`) when some nullable column of that enum exists — PRD §26.4 Rule 2. Operands typed as the bound GraphQL enum, not `String`.
- [x] Route enum-typed columns' `Filter.InputTypeName` to the per-enum input instead of the `StringComparator` fallback (`funcmap.go:290`'s old default) — **this is the F2 fix for enums**.
- [x] Emit `translate<EnumGraphQLName>Comparator` / `translateNullable<EnumGraphQLName>Comparator` returning `*comparator.Enum[<GoEnumType>]` / `*comparator.NullableEnum[...]`. Bodies are pointer/slice copies — the GraphQL `DONE` → Go `TaskStatus("done")` round-trip is the existing gqlgen `models:` binding (`cli/graphql.go:303-311`), so **no conversion machinery is added**.
- [x] Register the per-enum comparator in `ComparatorFamilies` so 25.1's shared-schema emitter declares it with no template edit.
- [x] Confirm enum **array** columns (`[]<Enum>` / named enum slice types) route to 25.5's `<Elem>SliceComparator`, not here — `stripPointerAndSlice` reduces the bare type before the enum lookup, so this boundary needs an explicit test.
- [x] Confirm a `hidden` / `internal` enum column (§32.2) emits **no** comparator input and no unused enum declaration, matching the existing `apiVisible` side-effect gating in `mapColumnToGraphQL`.

### Acceptance Criteria

- `TaskFilter.status` has type `TaskStatusComparator`, not `StringComparator`; `{status: {eq: DONE}}` narrows the result set — the reported F2 symptom is closed.
- `{status: {eq: BANANA}}` is rejected by the **GraphQL parser** with a validation error, not accepted and failed at runtime — the schema-level-validation benefit that motivated D3 over runtime parsing.
- One comparator input per (enum, nullability) — `<E>Comparator` and, when a nullable column of that enum exists, `Nullable<E>Comparator` — each shared across every table and view referencing that enum (not one per column, not one per table).
- Nullable enum columns reference `Nullable<E>Comparator` (which declares `isNull`); `NOT NULL` ones reference `<E>Comparator` (which does not).
- Enum-array columns get a slice comparator, never `<Enum>Comparator`.
- 25.2's lint passes: every enum column now has both a schema field and a translator entry.

### Tests Required

- [x] Unit test: schema emission for a table with a non-null and a nullable enum column — assert input type names and the `isNull` asymmetry.
- [x] Unit test: exactly one `<Enum>Comparator` is emitted when N tables reference the same enum.
- [x] Unit test: an enum-array column routes to `<Elem>SliceComparator`, not `<Enum>Comparator`.
- [x] Unit test: a `hidden` enum column emits no comparator input and no enum declaration.
- [x] E2E over the real gqlgen server: `{status: DONE, priority: HIGH}` narrows results (the consumer's blocked story); `{status: {eq: "not-an-enum-value"}}` returns a GraphQL validation error, not a 200 with unfiltered rows.
- [x] E2E: the same filter through `and` / `or` nesting still translates (recursion path).

### Completion Record

- **Date:** 2026-09-01
- **Files changed:** 9 — `cmd/sqlgen/gen/context_api.go`, `orchestrate.go`,
  `templates/api/comparator_translate.go.tmpl`, new `cmd/sqlgen/gen/api_enum_comparator_test.go`;
  `comparator/enum.go`, `comparator/comparator.go`, `comparator/enum_test.go` (FIX-173);
  `cmd/sqlgen/testdata/examples/graphql/schema.sql` + new
  `tests/enum_comparator_test.go`; `cmd/sqlgen/testdata/examples/postgres/tests/filter_test.go`.
  Goldens regenerated across the `graphql` and `mysql` example trees.
  - **The routing (F2, enum half).** `comparatorTranslatorVariant` gained an `Enum` arm feeding
    `makeEnumTranslator`, built the same way `makeOpaqueTranslator` is. An enum column's
    projection now resolves to `<EnumGraphQLName>Comparator` / `Nullable<…>Comparator` with
    `translate<…>Comparator` behind it, so the schema field and the translator entry appear
    together out of 25.1's single derivation.
  - **Two names the type expression cannot carry.** `*comparator.Enum[OrderStatus]` names
    neither the GraphQL enum nor the models-qualified Go type, so `mapColumnToGraphQL` resolves
    an `enumComparatorBinding` from `typeBindings` first and threads it in — mirroring how 25.3
    threaded the scalar in for the decimal narrowing. `typeBindings` gained `modelsPkg`
    (`cfg.Output.Package`, the same field the orchestrator later mirrors onto
    `APIContext.ModelsPackage`), because an enum's type parameter is the **consumer's** Go type
    where every other family's is a builtin or stdlib type.
  - **The guard is load-bearing, not defensive.** `resolveComparatorType` routes *every*
    unqualified PascalCase Go type through `comparator.Enum[T]` (`isEnumLikeType`), so a
    consumer-scalar column spelled that way arrives at the same arm. `makeEnumTranslator`
    returns false unless the type parameter is a schema enum, which keeps such a column on the
    `StringComparator` fallback instead of emitting operands that name an undeclared GraphQL
    enum. A MySQL SET reaches the enum *lookup* too (its value type is a GraphQL enum) but
    resolves to `comparator.String`, so it never enters the arm — both boundaries have their
    own test.
  - **Emission is data, not template.** The per-enum family rides 25.1's
    `collectComparatorFamilies` union hook with no shared-schema template edit. That hook was
    half-wired (25.1's own reviewer note: a non-shipped family would arrive with `Operators:
    nil` and render the empty-input shape gqlgen rejects); `comparatorOperatorsFor` now supplies
    the enum operator set from the binding, closing it.
  - **The one new import.** `comparator_translate_gen.go` previously had no dependency on the
    consumer's models package; the Enum family is the one that introduces one, so
    `buildAPIComparatorTranslateFile` emits it — gated on an enum translator actually being
    present, since an unused import is as fatal as a missing one under `FormatOnly` rendering
    (25.3's R1 failure mode). `models` and `gqlmodel` share one import group, as
    `filter_translate_gen.go` already writes them.
  - **gqlgen operand shapes MEASURED, not inferred**, the discipline `opaqueComparatorBindings`
    records: a schema enum's Go type is a named string, so it is neither nilable (nullable
    scalar operands are pointer-wrapped, `*models.X`) nor a struct (non-null list elements stay
    bare, `[]models.X`). Confirmed against the regenerated `graph/model/models_gen.go` before
    the template arm was written.
- **Tests:** `make check` clean (lint + vet + unit, all modules, `-race`); `make check-examples`
  clean across all 13 example modules. New `api_enum_comparator_test.go`:
  `TestEnumComparator_schemaEmission` (input names, enum-typed operands, Rule 2 asymmetry, the
  enum declaration the operands oblige), `TestEnumComparator_oneInputPerEnum` (two tables, one
  input and one translator — the §26.4 Sharing rule), `TestEnumComparator_translatorEmission`
  (bodies, models qualification, `isNull` → `Null`, per-table dispatch),
  `TestEnumComparator_fileImportsModelsPackage` (both directions — present with an enum, absent
  without), `TestEnumComparator_arrayColumnIsNotMonomorphized`,
  `TestEnumComparator_hiddenColumnEmitsNothing`,
  `TestEnumComparator_setColumnKeepsStringComparator`,
  `TestEnumComparator_nonSchemaEnumKeepsFallback`. E2E in the `graphql` example:
  `TestEnumComparator_NarrowsOverHTTP` (eq / neq / in / nin / and / or — every case a strict
  subset, so the pre-25.4 unfiltered answer fails each one),
  `TestEnumComparator_RejectsUnknownMember` (`BANANA` and the SQL literal as a string both
  rejected by the parser; `isNull` absent from the NOT NULL form),
  `TestNullableEnumComparator_OverHTTP`.
- **Triggers:** path-based E2E trigger fired (`cmd/sqlgen/gen/**`, `comparator/**`,
  `cmd/sqlgen/testdata/examples/**`) — `make check-examples` run, clean. Security-review trigger
  not fired: nothing under `manifest/`, a `//go:embed` target, or `cmd/sqlgen/cli/**`.
- **FIX-173 (blocking, phase-11 origin) — a live consumer-facing runtime bug the E2E surfaced,
  fixed inline with the user's go-ahead.** `comparator.Enum[T].In` / `.Nin` failed outright on
  pgx: the array-parameter path hands the driver `[]models.UserRole`, and pgx has no encode plan
  for a slice of a named enum type against the column's enum-array OID. **Reproduced from the Go
  client with no GraphQL involved** — 25.4 only made it reachable over HTTP. `Eq` was
  unaffected (a scalar named string goes through the driver's string-kind conversion), which is
  why every existing enum filter test passed while the set operators were unreachable. Fixed by
  having `Enum.Parse` take the expanded `IN ($, $, …)` form on every dialect
  (`parseInExpanded` / `parseNinExpanded`, split out of their dialect-aware wrappers) — it asks
  the driver for nothing beyond what `Eq` already proved, and needs neither `reflect` nor a
  narrowing of `Enum[T]`'s `comparable` constraint. The first attempt rendered the values as
  `[]string` so `= ANY($1)` would encode; it works (measured), but required
  `reflect.TypeFor[T]().Kind()` to separate a string-kinded enum from an integer one, which
  would have been the first use of `reflect` in a runtime core package — **the user caught it
  and it was replaced**. Pinned at three levels, all verified failing-first: the comparator unit
  test across four dialects, the `postgres` example's `TestFilter_EnumSetOperators_Postgres`
  (Go client, pgx, real enum column — the honest home for a runtime-package fix), and the
  GraphQL `in`/`nin` E2E cases.
- **Example schema change:** `assets.primary_category` (nullable `document_entity_type_enum`)
  added to the `graphql` example. Every enum column in the tree was NOT NULL, so
  `NullableDocumentEntityTypeEnumComparator` and its translator would have been emitted into no
  golden and compiled by nothing — the blind spot that hid FIX-147 and FIX-148 for two phases.
  It also makes `assets` a second table referencing the same enum comparator, so the
  one-input-per-enum sharing rule is observed rather than asserted.
- **FIX-145 half-closed.** The 25.1 `StringComparator` fallback no longer covers enum columns;
  JSON / JSONB / Slice still take it, so the entry stays **Open** by its own terms ("closed by
  whichever of 25.4 / 25.5 lands last"). 25.2's lint remains gated on 25.5.
- **Two pre-existing observations, neither in scope and neither fixed.**
  - ~~**`or: [A, B]` on the GraphQL surface means `A AND B`, not `A OR B`.**~~ **Fixed by
    FIX-195 (2026-09-10).** Pre-fix, each `Or` entry was wrapped in its own `sql.Or(...)` and the
    wrappers were AND'd together, so two sub-filter entries produced two OR-of-one groups — a
    much easier trap to fall into over GraphQL, where `or: [{…}, {…}]` reads as a disjunction to
    every client. It was reported as exactly that by a consumer app. PRD §11.1 is amended so
    members OR together and a member's own fields AND together; the GraphQL surface and its
    translator were already correct and are unchanged.
  - **`comparator.Slice[T]` has FIX-173's defect on the same axis** — an enum-array column's
    `@>` / `&&` operators still hand pgx a `[]Enum`. Unreachable today (an enum array keeps the
    fallback); recorded on FIX-173 so **25.5 confirms it before projecting
    `<Elem>SliceComparator`**.
- **Auto-review round (2026-09-01).** 9/9 verifiable PRD requirements PASS, 0 FAIL, 0 blocking
  (the tenth — "25.2's lint passes" — is unverifiable until 25.2 exists). The reviewer
  independently confirmed the four claims the sub-item rests on: the enum guard holds on every
  reachable shape, including the named-slice sibling, whose binding resolves but whose
  `GoTypeName` differs so it fails the equality; the alias baked into the descriptor and the one
  written in the import line **cannot** diverge, since `binds.modelsPkg` and
  `apiCtx.ModelsPackage` both read `cfg.Output.Package` from the same `cfg`; FIX-173's blast
  radius is nil, `parseInExpanded` / `parseNinExpanded` being pure extractions that leave the
  five other `parseIn` callers byte-identical; and the template's Enum arm matches the operand
  shapes gqlgen actually emits in the committed goldens. It also verified golden completeness —
  `sqlite`, `graphql_null_wrappers` and `graphql_top_level` declare no schema enums, so no tree
  went unregenerated. **Three findings fixed in place:** the new `postgres` test's doc comment
  still described the **rejected** `[]string` approach (it is the honest home for FIX-173 and its
  comment contradicted both `enum.go` and this tracker); `guidelines/SQL.md` §8's IN/NOT IN table
  presented `= ANY($1)` as the pgx form with no note that `comparator.Enum` is now a permanent
  exception; and `sort.Strings` was the repo's only hand-written `"sort"` import (convention is
  `slices.Sort`). **One comment corrected rather than the code:** `buildAPIComparatorTranslateFile`
  claimed the models import follows "the same fail-soft the filter translator takes, where an
  unresolvable path skips the file" — it does not, since this file is gated only on having
  translators. Pre-existing degeneracy (`GqlgenModelAlias` is empty on that same path, so every
  parameter type is already unresolvable and the file could not compile either way), so the
  overstatement was removed rather than the behaviour changed.
- **Two FIXes logged rather than guessed at, both `tracked`, both pointing at 25.5.**
  **FIX-174** — since **retracted as not reproducible**, and replaced by **FIX-176**; see the
  post-25.4 verification note below. **FIX-175** — a schema enum
  PascalCasing onto a shipped family name (`Time`, `Duration`, `Bytes`, `IP`, …) silently gets
  the wrong translator: `comparatorOperatorsFor` short-circuits on `comparatorFamilyByName` and
  `translatorsByName` is last-write-wins. Low likelihood, silent failure, and exactly the
  accept-and-ignore class D1 exists to eliminate — reintroduced through the *name space* rather
  than through a second derivation. Not fixed here because the resolution (hard-error on
  collision, the convention FIX-158/164/171/172 follow, versus suffix-disambiguation) is a design
  decision PRD §26.4 does not cover, and 25.5 widens the same surface — one guard for both
  monomorphized families beats two patches.
- **Post-25.4 verification of FIX-174, before starting 25.5 (2026-09-01) — the entry was wrong,
  and the probe found a worse defect beside it.** FIX-174 claimed `comparator.Slice[T]` carried
  FIX-173's defect on the array-element axis. It does not: `Slice[T]` never takes the
  array-parameter path, because `sliceToArrayLiteral` renders the operand as a PostgreSQL array
  **text literal** — `slice.go`'s own doc comment says that is exactly why, "without needing
  custom type OID registration for enum arrays". All four operators measured clean against
  `assets.declared_categories` on pgx. The entry was written from the FIX-173 analogy without
  reading `slice.go`; it is **retracted on the record** rather than deleted, because "almost
  certainly" should have prompted a probe, not a filing.
  - **FIX-176 (blocking, phase-11 origin) — found by that probe and fixed.** The literal was
    built with `fmt.Sprint` joined on `","` and **no quoting**, so `sliceToArrayLiteral` owned
    PostgreSQL's array-literal grammar without implementing it. Measured against a real `text[]`
    column: `with,comma` → **0 rows and no error** (split into two elements server-side),
    `with"quote` and `with{brace}` → `malformed array literal`, the empty string → vanishes. The
    comma case is the one with teeth — a filter that quietly matches a *different* set is worse
    than one that errors, and it is the accept-and-ignore class this phase exists to eliminate.
    Not SQL injection (the literal is still a bound parameter), but caller-controlled corruption
    of the filter's semantics. Enum members are identifiers and never need quoting, which is
    precisely why it stayed invisible: every array any test filtered on was an enum array. Fixed
    with conditional quoting mirroring PostgreSQL's own `array_out`, pinned by ten unit cases and
    a `text[]` E2E, both **verified failing-first** (13 unit subtests, 4 of 7 E2E subtests).
    **This is what made verifying before 25.5 worth doing** — 25.5's `<Elem>SliceComparator` puts
    `containsAny: [String!]` on the GraphQL surface, where the values are caller-supplied.
    `guidelines/SQL.md` gained a §8.1 recording the grammar next to the IN/NOT IN table.
- **Notes:** `mapColumnToGraphQL`'s enum-usage registration now reuses the binding lookup rather
  than repeating `binds.enums[bareGoType]`. `comparatorTranslatorStdImports` needs no Enum arm —
  the type parameter is the consumer's, not stdlib — which is exactly why the models import is
  handled separately.

---

## 25.5 JSON / JSONB / Slice comparators — monomorphized (Ticket A, D3)

**PRD Reference:** Sections 26.4 (B2's rows + PostgreSQL-only gating), 26.5.3 (translators), 11.2 (JSON / JSONB / Slice operator sets and the dialect rules), 7.6 (JSON column type). Design §3.1, §2-D3.

**Status:** **Complete** (landed 2026-09-02)

**Governing decisions:** D3 (monomorphize per concrete type).

**Depends on:** 25.1.

### Tasks

- [x] Emit `input JSONComparator { contains: JSON, hasKey: String }` and its `NullableJSONComparator` twin (same operands + `isNull`, per PRD §26.4 Rule 2) — **all dialects**, matching `comparator.JSON`'s dialect-switching `Parse`.
- [x] Emit `input JSONBComparator { hasKey: String, hasAnyKey: [String!], hasAllKeys: [String!], contains: JSON, containedBy: JSON, pathExists: String }` and its `NullableJSONBComparator` twin — **PostgreSQL only**, matching `resolveSimpleComparator`'s `jsonb` arm and §11.2's PostgreSQL-only note.
- [x] Emit `input <Elem>SliceComparator { containsAny: [<Elem>!], containsAll: [<Elem>!], containedBy: [<Elem>!], isEmpty: Boolean }` and its `Nullable<Elem>SliceComparator` twin per distinct element type — **PostgreSQL only**. Element type follows `SliceElemType` (named enum slices) or the stripped bare type. Note `isEmpty` is a `Boolean` predicate, not an element operand.
- [x] Emit the matching `translateJSONComparator` / `translateJSONBComparator` / `translate<Elem>SliceComparator` and their `Nullable` wrappers.
- [x] Register all three in `ComparatorFamilies`, gated on `input.dialect` for the two PostgreSQL-only families.
- [x] Route JSON / JSONB / slice columns' `Filter.InputTypeName` to these inputs — **completing the F2 fix**.
- [x] Keep the FIX-103 skip intact: `json[]` / `jsonb[]` columns (element types `types.JSON`, `json.RawMessage`, `map[string]any`) remain non-filterable and must be skipped on **both** sides. 25.2's lint is the guard.
- [x] Confirm no `JSONBComparator` / `<Elem>SliceComparator` is emitted under `input.dialect: mysql` or `sqlite`, and that a JSONB column on those dialects routes to `JSONComparator` (the §11.2 fallback rule).

### Acceptance Criteria

- `tasks.custom_fields` (JSONB) is filterable over GraphQL with the full JSONB operator set — the second half of the reported F2 symptom is closed.
- `JSONBComparator` and `<Elem>SliceComparator` are absent from the schema on MySQL/SQLite; a JSONB column there routes to `JSONComparator`.
- One `<Elem>SliceComparator` per (element type, nullability), shared across tables/views (e.g. a single `StringSliceComparator` and, if a nullable string-array column exists, a single `NullableStringSliceComparator`).
- `json[]` / `jsonb[]` columns appear in **neither** the filter input nor the translator (FIX-103 preserved), and 25.2's lint confirms it.
- Emitted SQL matches `comparator/jsonb.go` / `slice.go` / `json.go` operator-for-operator, including the dialect switch in `JSON.Parse`.
- `comparator.Custom` is not projected on any of the three.

### Tests Required

- [x] Unit tests: schema emission per dialect — assert JSONB/Slice inputs present on postgres, absent on mysql/sqlite, and that a JSONB column falls back to `JSONComparator` off-postgres.
- [x] Unit test: one `<Elem>SliceComparator` per element type across multiple referencing tables.
- [x] Unit test: a `jsonb[]` column is in neither the schema filter nor the translator; 25.2's lint passes.
- [x] Cross-dialect SQL-shape tests for every JSON / JSONB / Slice operator against `comparator/`'s expected clauses.
- [x] E2E (postgres): JSONB `hasKey` / `contains` / `containedBy` and slice `containsAny` / `isEmpty` narrow the result set correctly.
- [x] E2E (mysql): `JSONComparator` `contains` / `hasKey` produce `JSON_CONTAINS` / `JSON_CONTAINS_PATH` and return correct rows.

### Completion Record

- **Date:** 2026-09-02
- **Files changed:** 10 source + goldens — `cmd/sqlgen/gen/context_api.go`, `api_names.go`,
  `orchestrate.go`, `templates/api/comparator_translate.go.tmpl`; new
  `cmd/sqlgen/gen/api_slice_json_comparator_test.go`; `api_projection_test.go`,
  `api_comparator_families_test.go`, `api_enum_comparator_test.go`;
  `cmd/sqlgen/testdata/examples/graphql/schema.sql` + `tests/main_test.go` + new
  `tests/filter_comparator_test.go`; new
  `cmd/sqlgen/testdata/examples/mysql/tests/graphql_json_test.go`; `docs/PRD.md` (§26.12).
  Goldens regenerated across the `graphql` and `mysql` example trees.
  - **The routing (F2, second half).** `comparatorTranslatorVariant` gained `JSON`, `JSONB` and
    `Slice` arms, so a document or array column's schema field and its translator entry now come
    out of 25.1's single derivation together. `users.metadata` went from `StringComparator` to
    `NullableJSONBComparator`, `profiles.settings` to `NullableJSONComparator`,
    `assets.declaredCategories` to `DocumentEntityTypeEnumSliceComparator`, and the five
    `numeric_widths` array columns to `IntSliceComparator` / `FloatSliceComparator` /
    `NullableIntSliceComparator`. Each of those fields was previously advertised and ignored.
  - **JSON and JSONB are STATIC families, only Slice is monomorphized.** Both document
    comparators are single Go structs, so they are ordinary `shippedComparatorFamilies` rows
    reached through `makeNonNumericTranslator` — no new machinery. Slice is the one family whose
    operand type varies, and it rides 25.1's `collectComparatorFamilies` union hook exactly as
    the enum family does, with no shared-schema template edit.
  - **Both dialect gates are STRUCTURAL, and deliberately not a second derivation.** The task
    said "gated on `input.dialect`"; an explicit switch would have been a second spelling of a
    fact another function already owns — the shape this phase exists to remove. Measured
    instead: `resolveSimpleComparator` returns `JSONB` only under `config.DialectPostgres`, and
    `gotype`'s `IsSlice` is set **only** by the PostgreSQL array branch (probed across all three
    dialects — `text[]` / `integer[]` resolve to a plain `string` on MySQL and SQLite, and
    `jsonb` resolves to `string` there unless overridden). Rule 3 then declines to declare an
    unreferenced family, which is the same mechanism that already keeps `IPComparator` off
    MySQL. Both halves are pinned by `TestJSONComparator_dialectGating` and
    `TestSliceComparator_dialectGating`, the latter rebuilding the gotype resolver per dialect —
    a test that only flipped `Config.Input.Dialect` would have left the PostgreSQL resolver in
    place and proved nothing.
  - **The `<Elem>SliceComparator` split is the `NumericComparator` split.** PRD §26.4 "Sharing"
    keys an input on the **GraphQL operand type**, so `smallint[]`, `integer[]` and `bigint[]`
    share one `IntSliceComparator` while each needs its own `comparator.Slice[T]` — the same
    collapse `NumericComparator` already makes for scalar widths, arriving by the same rule
    rather than by a new one. The translator name therefore carries the Go-width suffix exactly
    when a conversion is needed (`translateIntSliceComparatorInt32` beside a plain
    `translateStringSliceComparator`), and the licensed conversions are read off the existing
    `isNumericWidthCast` table rather than a second list.
  - **`sliceOperandElemGoType` admits the spec built-ins only, and that is a decision, not an
    omission.** gqlgen pointer-wraps a non-null list element when its Go type is a STRUCT and
    leaves it bare otherwise — a different rule from the one governing scalar operands, and one
    `opaqueComparatorBindings` records is established per type by measurement. A `timestamptz[]`
    or `numeric[]`-overridden-to-decimal column therefore keeps the fallback rather than getting
    operands the body cannot copy, the same `return false` discipline `makeOpaqueTranslator`
    uses for an unknown `T`. Recorded in **PRD §26.12** so a reader is not left expecting it;
    adding an element type is a table entry plus a golden.
  - **gqlgen operand shapes MEASURED against the regenerated `models_gen.go`, not inferred:**
    `Contains types.JSON` (bare — `types.JSON` is nilable, so no pointer wrap),
    `HasAnyKey []string`, `ContainsAny []int` / `[]float64` / `[]bool` / `[]string`,
    `[]models.DocumentEntityTypeEnum` for the enum element, and `IsEmpty *bool`.
  - **The document operand is converted to a string, and the reason was measured after the first
    version shipped bytes.** `types.JSON` is a `[]byte`, so boxing it sends a BINARY parameter.
    That works against MySQL 8.0 on a default DSN — the E2E passes either way — and **fails**
    under `interpolateParams=true`, which go-sql-driver escapes as `_binary'…'`:
    `SELECT JSON_CONTAINS('{"a":1,"b":2}', ?)` with a `[]byte` arg returns
    `Error 3144 (22032): Cannot create a JSON value from a string with CHARACTER SET 'binary'`,
    while a string arg returns 1 in both modes. The DSN is the consumer's, so the generated
    filter has to be right under either. The template comment states the probe rather than the
    hypothesis, because the hypothesis is what the first draft asserted and the example's own
    E2E would never have contradicted it.
  - **New names entering the GraphQL namespace were reserved, PRD-exactly, in two halves.**
    `specScalarSliceComparatorName` reserves `String` / `Int` / `Float` / `Boolean` /
    `ID`-element slice inputs against `reservedGraphQLTypes` (§26.4's owned-set table says "each
    emitted `<X>Comparator`"), and an enum now also claims `<E>SliceComparator` and its twin
    alongside `<E>Comparator`, on the same conditioning boundary — the claim exists whether or
    not an array column does, so adding one later cannot turn a valid schema invalid. It is
    deliberately NOT folded into `fixedOwnedGraphQLName`, because §26.4 scopes
    `enumComparatorInput`'s disambiguation key to the fixed families precisely so an emitted name
    stays stable against the rest of the schema; an enum deriving `IntSliceComparator` keeps its
    spelling and is reported as a collision instead.
  - **`comparatorOperandScalars` gained `JSON`.** It cannot disagree with the referencing
    column's own scalar today — a column reaches these families only by resolving to
    `types.JSON`, which declares `scalar JSON` anyway — but FIX-141's lesson was to close over
    the rule rather than its known instances, and leaving a family out because its two scalars
    coincide today is how that gap reopens.
  - **`comparatorTranslatorVariant` was split, not annotated.** Adding three arms pushed it to
    cyclomatic complexity 18 (max 15). Split into `plainComparatorTranslator` (no type
    parameter) and `genericComparatorTranslator` (four monomorphized families), mirroring the
    `resolveSimpleComparator` / `resolveGenericComparator` split `resolveComparatorType` already
    draws — the two halves answer different questions and only one needs the bindings the type
    expression cannot spell.
- **Tests:** `make check` clean (lint + vet + unit, all modules, `-race`); `make check-examples`
  clean across all 13 example modules. New `api_slice_json_comparator_test.go`:
  `TestJSONComparator_dialectGating` (three dialects, resolver rebuilt per dialect, JSONB absent
  off postgres and the column falling back to `JSONComparator`),
  `TestSliceComparator_dialectGating` (and the columns staying filterable through a real
  translator off postgres — a gate that created a new accept-and-ignore hole would be no better
  than the one it closed), `TestSliceComparator_oneInputPerElementType` (two tables, three
  widths, one input each), `TestSliceComparator_castVersusIdentity`,
  `TestSliceComparator_isEmptyIsAPredicate`,
  `TestSliceJSONComparator_jsonArrayIsFilterableOnNeitherSide` (FIX-103, with a sibling `text[]`
  column proving the absence is the element rule and not the table),
  `TestSliceComparator_enumElementQualifiesModelsPackage` (a project whose ONLY enum use is an
  array still gets the models import), `TestJSONComparator_documentOperandsUseTheJSONScalar`,
  `TestJSONComparator_translatorBoxesTheDocument`,
  `TestSliceComparator_customScalarElementKeepsFallback`,
  `TestSliceComparator_specScalarNamesAreReserved`. Five new rows in
  `TestComparatorFamilies_projectEveryGoOperator` give the "operator-for-operator against
  `comparator/`" coverage mechanically, by parsing `json.go` / `jsonb.go` / `slice.go` — so a new
  Go operator on any of the three fails the build rather than going unprojected. E2E in the
  `graphql` example (`filter_comparator_test.go`, 24 cases against a real PostgreSQL): every
  JSONB operator and every array operator narrowing to a **strict subset** of the seeded rows,
  asserted against an explicit unfiltered baseline — which is exactly what the pre-25.5 missing
  translator returned, so each case fails without the fix. E2E in the `mysql` example
  (`graphql_json_test.go`): `JSON_CONTAINS` / `JSON_CONTAINS_PATH` returning correct rows, the
  NOT NULL form declaring no `isNull`, and a JSONB operator rejected on that dialect.
- **Triggers:** path-based E2E trigger fired (`cmd/sqlgen/gen/**`,
  `cmd/sqlgen/testdata/examples/**`) — `make check-examples` run, clean. Security-review trigger
  not fired: nothing under `manifest/`, a `//go:embed` target, or `cmd/sqlgen/cli/**`.
- **Example schema change:** a new `filter_probes` table in the `graphql` example. `doc JSONB
  NOT NULL` earns the base `JSONBComparator` a golden (every other JSON-ish column in an
  API-enabled example is nullable, the blind spot 25.4's `primary_category` closed for enums);
  `tags TEXT[]` / `tags_n TEXT[]` cover the identity-copy arm, Rule 2's twin, and — the reason it
  is a `text[]` and not another enum array — the one array whose operands are **caller-supplied
  strings**, which is FIX-176's surface; `flags BOOLEAN[]` and `weights DOUBLE PRECISION[]` cover
  the Boolean element binding and the Float element with NO width cast, the counterpart to
  `numeric_widths.ratios`. No NOT NULL `json` column was added: on PostgreSQL `comparator.JSON`
  compiles to the jsonb-only `@>` / `?`, so the base `JSONComparator` earns its golden in the
  MySQL example, where `JSON_CONTAINS` is the real operator.
- **FIX-176 re-verified through the new surface.** `containsAny: ["with,comma"]` and an element
  carrying a double quote both return exactly the seeded row over HTTP. Before FIX-176 the comma
  case matched a *different* set silently and the quote case failed with `malformed array
  literal`; those two cases are why the E2E array is `text[]`.
- **FIX-145 closed** — by the sub-item its own terms named ("closed by whichever of 25.4 / 25.5
  lands last"). `fallbackComparatorInput` survives, now covering only the two shapes with no
  *sound* projection rather than the four with no projection *yet*: an enum-like Go type that is
  not a schema enum, and an array element with an unmeasured operand shape. That is a different
  class, and what to do about it is **25.2's** decision — the completeness lint is what forces
  the choice between dropping the field and hard-erroring, and it is now unblocked. Flagged on
  the FIX entry rather than left implicit.
- **Auto-review round (2026-09-02).** 8/8 verifiable PRD requirements PASS, 6/6 required tests
  present. The reviewer independently confirmed the seven claims this sub-item rests on: both
  dialect gates really are structural (`IsSlice` is set at exactly two sites, both inside the
  postgres branch, and `goTypeFromOverride` never sets it, so no override / `type_map` /
  `column_map.type` path reaches a PostgreSQL-only family off postgres — and views share the same
  resolver); the slice translator names are collision-free because `numericSuffixForType` is
  injective over the cast domain and the identity case never emits a suffix, so one FuncName can
  never carry two return types; the enum-element detection holds for the named-slice and bare-array
  shapes alike because `binds.enums` keys both the bare name and the `<E>Slice` sibling and both
  sides derive from `EnumGoTypeName`; `string(...)` is legal for every binding the `JSON` scalar can
  take, since `JSON` is in `config.ReservedScalarNames` and therefore always binds `types.JSON`; and
  the imports are right in both directions against the regenerated goldens. **One finding fixed in
  place:** two doc comments still described the pre-25.5 world (`APIFilterProjection.InputTypeName`
  claiming Enum / JSON / JSONB / Slice all take the fallback, and `buildAPIFilterFields` repeating
  the list) — both now name the two shapes that actually remain and point at 25.2.
- **Two findings logged rather than fixed, both spec-level, and the first MEASURED.**
  **FIX-177 (blocking)** — `comparator.JSON`'s postgres arm emits `col @> $` / `col ? $`, and
  PostgreSQL defines neither operator for the `json` type. Reproduced against the example's own
  `profiles.settings JSON` column: `operator does not exist: json @> unknown (SQLSTATE 42883)`,
  with the same queries succeeding on a `jsonb` column as the control, and `settings::jsonb @> $1`
  matching correctly (1 row on a match, 0 on a non-match). **Not a 25.5 regression** — before this
  sub-item the column advertised `StringComparator` with no translator, so the filter was accepted
  and silently dropped, which is the F2 class; it is now a loud error instead. It is **not fixed
  here** because it is not an implementation bug: PRD §11.2's dialect table specifies exactly the
  SQL that fails, four lines after the same section explains that a postgres `json` column uses the
  JSON comparator *because* `json` lacks the JSONB operators. The two sentences contradict each
  other, and the measured fix (`col::jsonb`) rewrites the normative table — project rule 1 says
  flag, not guess. **FIX-178 (tracked)** — the SQLite half of the same gap: `JSON.Parse` has no
  sqlite arm, so a `JSONComparator` filter there parses, dispatches into a real translator, and
  emits no SQL. Each spec section is self-consistent (§11.2 defers SQLite JSON; §26.4 says SQLite
  falls back to `JSONComparator`); together they specify an accept-and-ignore field, and 25.2's
  lint cannot catch it because both surfaces are present. Narrowly reachable — SQLite's built-in
  table maps no column to `types.JSON` (measured), so it needs an explicit override — and pinned
  by `TestJSONComparator_dialectGating`'s SQLite row, which moves with the resolution.
- **Notes:** `comparatorTranslatorsUseModels` now also matches a slice-of-enum translator, which
  is the second (and only other) family naming a type from the consumer's models package.
  `comparatorTranslatorStdImports` needs no Slice arm and says so explicitly — a slice element is
  either a spec built-in needing no import or a schema enum riding the models group.

---

## 25.6 View tenancy — detection + attachment (Ticket B, D5/D6)

**PRD Reference:** Sections 29.2.3 (detection rules 1–5, extended to views by B1), 29.2.4 (type resolution + uniform-type validation + nullability), 29.2.2 (per-entity override shape), 4.9 / 4.13 (per-view config + validation per B7). Design §3.2, §2-D5, §2-D6.

**Status:** **Complete** (landed 2026-09-02 — `make check` clean across all 33 modules, `make check-examples` clean across all 13 example modules).

**Governing decisions:** D5 (a view carrying the tenant column is tenanted, read path only, auto-detected with per-view opt-out); D6 (nullable tenant column on a view → warn + scope, not the table hard-error).

**Depends on:** 25.0.

### Tasks

- [x] Add `Tenancy *TableTenancyConfig` to `config.ViewConfig`; add `config.ResolveViewTenancyEnabled` / `…Column` / `…Required` / `…Type` mirroring the table resolvers. **`API *ViewAPIConfig` deliberately deferred to 25.8**, which owns the type, `ResolveViewAPIEnabled`, the read-only operations mask, and the §4.13 validation end-to-end — landing a parsed-but-unresolved `views.<n>.api` block here would make a knob the user deliberately turned silently do nothing, which is the exact failure class this phase exists to close (design §1.3).
- [x] Add `resolveViewTenancy(cfg, view, resolver)` in `context_tenancy.go`, mirroring `resolveTableTenancy` minus every write-path concern: same column resolution and per-view `enabled` override; **no** `InPrimaryKey` handling (a view has no DDL PK — `@pk` is a `Get` annotation, and §29.7's composite-PK constructor-omission rule is a create-path concern views do not have).
- [x] Add a second loop over `schema.Views` in `BuildTenancyContext`, writing into the same qualified-name-keyed map.
- [x] Make views **participate in the §29.2.4 uniform-tenant-type check**: the `TenantResolver` is one concretely-typed function per package, so a view whose tenant column resolves to `int64` while tables resolve to `uuid.UUID` produces the same aggregated hard error a mismatched table does. Views appear in the offender list, labelled `view <qualified-name>`.
- [x] Implement D6: a nullable tenant column on a view emits a **codegen warning** and the view is still scoped (fail-closed — `WHERE col = $1` drops NULL rows); it is a **hard error only** when `views.<n>.tenancy.enabled: true` was set explicitly.
- [x] Add `attachTenancyToViews(views, tenancyMap)`, setting `ViewContext.Tenancy *TableTenancyContext` (reuse the type — its fields are a superset) and folding the tenant type's import plus `github.com/teandresmith/sqlgen/tenancy` into `ViewContext.Imports`.
- [x] Call `attachTenancyToViews` from `orchestrate.go` — placed in `buildAllContexts` beside `attachTenancyToTables`, which is both after `BuildViewContexts` / before `generateViews` and on the `BuildTableContextsFromSchema` path too.
- [x] Extend `applyClientTenancy` to take views so `ClientContext.TenantedEntities` includes tenanted views — the unified client's resolver-threading loop then covers them.
- [x] Add §4.13 config validation: `views.<n>.tenancy.enabled: true` with the column absent from the view is a hard error naming the view and column (rule 5 parity); a `views.<n>.tenancy.type` pointing at a mismatched SQL column type errors as for tables. Both land in `resolveViewTenancy` rather than `config.ValidatePostParse`, matching where the table rules already live — the third §4.13 view-tenancy row (tenant-type divergence) needs the `gotype` resolver and can only be codegen-level, so splitting the set across two phases would be the drift the phase exists to prevent.
- [x] Confirm **no** cache work is required (F9) — confirmed: `templates/view/*.tmpl` (client, count, get, model, pagination, refresh) contains no `cache` reference, and `buildCachedView` carries only `InvalidateOn` + a fingerprint computed by `computeViewFingerprint`, which takes no tenanted marker (unlike the table `computeFingerprint`). PRD §29.2.5 "Caching" states this is correct for this revision.

### Acceptance Criteria

- A view carrying the effective tenant column resolves to `Tenanted: true` with the correct column, Go type, import, and `Required` — under global auto-detection, with no per-view config.
- `views.<n>.tenancy.enabled: false` forces shared even when the column exists (rule 4 parity).
- `views.<n>.tenancy.enabled: true` with the column absent is a hard codegen error naming the view and column (rule 5 parity).
- A view whose tenant column type diverges from the tables' appears in the aggregated uniform-type error alongside offending tables.
- A nullable tenant column on a view warns and still scopes; the same column with explicit `enabled: true` hard-errors.
- `ClientContext.TenantedEntities` includes tenanted views, so the unified client threads the resolver in.
- `ViewContext.Tenancy` is `nil` when tenancy is globally disabled — projects without tenancy regenerate byte-identically.
- **No template changes in this sub-item** — no generated-output diff yet beyond the imports that 25.7 will consume.

### Tests Required

- [x] Table-driven unit tests over `resolveViewTenancy` (`TestBuildTenancyContext_viewDetection`, 9 cases): column present → tenanted; absent → shared; `enabled: false` + column present → shared; `enabled: true` + column absent → error; per-view `column` override; per-view `required` override; per-view `type` override; nullable → warn + scope; nullable + `enabled: true` → error; `type` override mismatched against the SQL type → error.
- [x] Unit test: a view with a divergent tenant type appears in the uniform-type error; the error names both the view (`view public.legacy_rollup`) and the offending table, offers the `views.<name>.tenancy.enabled: false` remedy, and still emits exactly once (`TestBuildTenancyContext_viewJoinsUniformTypeError`).
- [x] Unit test: nullable tenant column on a view → warning naming the view + `Tenanted: true`; with explicit `enabled: true` → error (both cases in the table above).
- [x] Unit test: `TenantedEntities` includes tenanted views and excludes shared ones (`TestBuildClientContext_tenantedEntitiesIncludeViews`).
- [x] Unit test: tenancy globally disabled → `BuildTenancyContext` returns a nil map with no warnings and every `ViewContext.Tenancy` stays nil (`TestAttachTenancyToViews_nilMapLeavesTenancyNil`); generated-output invariance is pinned directly by `TestGenerate_tenantedViewAttachesTenancyAndLeavesViewFileUnchanged`, which runs `Generate` twice over one schema (tenancy on / off) and asserts the view's `_gen.go` is **byte-identical**.
- [x] Config validation tests for the new `views.<n>.tenancy` block: four table-driven resolver tests (`TestResolveViewTenancyEnabled` / `…Column` / `…Required` / `…Type`) at §4.13 parity with the table cases, plus `TestLoadConfig_ViewTenancyBlock` pinning tri-state pointer survival through YAML round-trip (absent block → nil, not a zero-value opt-out).
- [x] Extra pins beyond the list: `TestBuildTenancyContext_viewGoTypeLiteralReachesTenancy` (a view's `@type` annotation literal reaches the tenancy context — the view-side shape of FIX-156, so the row struct and the tenant predicate cannot name two different Go types for one column), and `TestBuildTenancyContext_materializedViewDetectsIdentically` (detection has no matview branch, guarding the §29.2.5 rule that only `Refresh` is unscoped).

### Completion Record

**Landed 2026-09-02.** `make check` clean across all 33 modules; `make check-examples` clean across all 13 example modules. Golden churn is **one comment line** in `client_gen.go` across the four tenancy-enabled examples (auto-review finding 4) — no view emission changed, because no example combines `tenancy.enabled: true` with a view.

**Files changed — 12 code/template + 8 regenerated goldens + 1 new test file, plus 4 docs:**

| File | Change |
|---|---|
| `cmd/sqlgen/config/config.go` | `ViewConfig.Tenancy *TableTenancyConfig` |
| `cmd/sqlgen/config/tenancy.go` | `ResolveViewTenancyEnabled` / `…Column` / `…Required` / `…Type` |
| `cmd/sqlgen/config/validate.go` | `validateTenancyConfig` gains a `cfg.Views` loop — `views.<n>.tenancy.type` with no `import` now errors, at table parity (auto-review finding 1) |
| `cmd/sqlgen/config/tenancy_test.go` | 4 table-driven resolver tests + `TestLoadConfig_ViewTenancyBlock` + 2 per-view rows in `TestValidatePreParse_TenancyTypeRequiresImport` |
| `cmd/sqlgen/gen/context_tenancy.go` | `resolveViewTenancy`, `resolveViewTenantColumnType`, `attachTenancyToViews`; `BuildTenancyContext` gains the views loop + a warnings return; `tenantedEntry` gains `isView` + `label()`; `findTenantColumn` now takes `[]parser.Column` |
| `cmd/sqlgen/gen/context_view.go` | `viewColumnGoType` factored out of `buildViewColumns` |
| `cmd/sqlgen/gen/context.go` | `ViewContext.Tenancy *TableTenancyContext` |
| `cmd/sqlgen/gen/context_client.go` | `applyClientTenancy` takes `views` |
| `cmd/sqlgen/gen/orchestrate.go` | `attachTenancyToViews` call; tenancy warnings threaded into `input.Warnings` on both the `Generate` and `ValidateGeneration` paths; `firstTenantedTableType` → `firstTenantedEntityType(tables, views)`, threaded through `generateSupport` (auto-review finding 2) |
| `cmd/sqlgen/gen/templates/client.go.tmpl` | comment-only: the resolver-threading block now says "tables **and views**" (auto-review finding 4) — the sole source of this sub-item's golden churn |
| `cmd/sqlgen/testdata/examples/{graphql,tenancy,tenancy_mysql,tenancy_postgres}/{expected,models}/client_gen.go` | regenerated for that comment — 8 files, +2/−1 lines each, no behavioral delta |
| `cmd/sqlgen/gen/export_test.go` | `AttachTenancyToViewsForTest` |
| `cmd/sqlgen/gen/context_tenancy_test.go` | call-site arity + the uniform-type message pin |
| `cmd/sqlgen/gen/context_tenancy_view_test.go` | **new** — the view-tenancy suite |
| `docs/PRD.md` | **B9** — §30.1 Tenancy composition row + §30.4.2 read-only-entity `features.tenancy` shape. Spec only; blesses 25.7's manifest tasks (see finding 5 below). No 25.6 code depends on it. |
| `docs/tracker/fixes.md` | **FIX-180** logged (open, tracked) — the fabricated manifest error-sentinel names |
| `docs/tracker/phase-25.md`, `docs/tracker/STATUS.md` | this record; 25.7 gained the B9 note, six manifest tasks, the required drift guard, three acceptance criteria and four tests |

**Notes.**

- **`BuildTenancyContext` signature changed** to `(map, []string, error)`. D6's warning has to reach `GenerateResult.Warnings` / `ValidateGeneration`, and detection is the only place that knows a view's tenant column is nullable. 16 unit call sites updated mechanically.
- **The uniform-type error message changed** from "tenanted **tables** must resolve to a uniform tenant Go type" to "tenanted **entities** …", and its remedy now names `views.<name>.tenancy.enabled: false` alongside the table key — both required by PRD §29.2.4 as amended by B1. The pinning assertion in `TestBuildTenancyContext_mixedTypesAggregatedError` moved with it.
- **Nullable view tenant columns resolve to the *value* Go type.** `resolveViewTenantColumnType` passes `nullable=false` regardless of the column's own nullability: the tenant bound into `WHERE <tenant> = $1` and carried by `TenantResolver[T]` is a plain `T`, never a `*T`. Without this a nullable-tenant view resolves to `*uuid.UUID` and trips the uniform-type check against its own tables. The row struct keeps the nullable spelling — a view's tenant column may legitimately project `NULL` — and the two facts are not in conflict.
- **`viewColumnGoType` is now the single derivation** for a view column's Go type, used by both `buildViewColumns` and the tenancy path. Views carry no `column_map` / `type_map`, so the tenancy path could not reuse `resolveColumnGoType`; hand-rolling a second cascade would have let a view's `@type` annotation reach the row struct but not the tenancy context — the view-side shape of FIX-156, pinned by a test.
- **Known transient into 25.7 (verified, not theoretical).** `applyClientTenancy` now puts tenanted views in `ClientContext.TenantedEntities`, so `client_gen.go` emits `c.<view>.tenantResolver = options.tenantResolver` — but `templates/view/client.go.tmpl` does not declare that field until 25.7. A project combining `tenancy.enabled: true` with a tenant-carrying view therefore generates a package that does not compile at this commit. Nothing in the repo hits it (no example combines the two, and `make check` / `make check-examples` are clean), and 25.7 closes it as its first task. The tracker sequences it this way deliberately — the alternative is holding the `applyClientTenancy` arm until 25.7, which is a one-line revert if the intermediate commit needs to stay generatable.
- The acceptance criterion "no generated-output diff yet beyond the imports that 25.7 will consume" holds **more strongly than written** for the view file itself: `github.com/teandresmith/sqlgen/tenancy` is folded into `ViewContext.Imports`, but every view emission path runs goimports with `FormatOnly: false`, which prunes it while no view body references it. The view `_gen.go` is byte-identical, asserted directly.
- **F9 confirmed** — no cache work required. Recorded against the task above.

**Auto-review (`sqlgen-reviewer`) — 5 findings, 4 fixed inline, 1 deferred.**

| # | Finding | Disposition |
|---|---|---|
| 1 | `views.<n>.tenancy.type` with no `import` was not rejected — `validateTenancyConfig` looped `cfg.Tables` only, so an unimportable type reached the generator and emitted a `TenantResolver[T]` with no import | **Fixed inline** — `config/validate.go` gains the views loop; 2 test rows added |
| 2 | `CallOptions.Tenant` was gated table-only (`firstTenantedTableType`), so a package whose tables all opt out but whose views carry the tenant column emitted `SkipTenancy` **without** `Tenant *T` — breaking §29.2.5's "`CallOptions` parity" claim, and a compile break for 25.7's ported `resolveTenant(ctx, explicit)` | **Fixed inline** — renamed `firstTenantedEntityType(tables, views)`, threaded through `generateSupport`; pinned by `TestGenerate_viewOnlyTenantedPackageStillEmitsExplicitTenantOption` |
| 3 | D6's warning had no test proving it reaches `GenerateResult.Warnings` | **Fixed inline** — `TestGenerate_nullableViewTenantColumnWarningReachesResult` |
| 4 | `templates/client.go.tmpl`'s resolver-threading comment still said "tables", and that text lands in every tenanted consumer's `client_gen.go` | **Fixed inline** — comment corrected, 8 goldens regenerated |
| 5 | **Manifest under-reports a tenanted view.** `buildViewEntity` (`cmd/sqlgen/manifest/builder.go:382`) sets `Features{}` unconditionally, so a tenanted view emits no `features.tenancy` while a tenanted table does — telling the MCP server and every agent reading the manifest that a scoped view's rows are globally visible. Unreachable before this sub-item; reachable now. | **Spec blessed here, implementation assigned to 25.7 (B9).** See below. |

**Finding 5 — resolved into a PRD delta (B9) plus 25.7 tasks.**

The table shape does not transfer, which is why this was not a same-day code fix: `buildTenancyFeature` emits `mode: "verify-match"` when the tenant column is in the PK — a §29.4.2 / §29.7 **mutation-input** rule with no view analogue, and one a view can trip because `@pk` populates `ViewContext.PKColumns` — and it emits `mismatch_error`, naming an error a read-only entity can never return. §30.1's composition row named all four fields flatly and §30.4.2 said nothing about read-only entities, so emitting either value would have been inventing manifest schema (project rule 1).

**PRD amended 2026-09-02 (B9):** §30.1's Tenancy composition row now states that a tenanted view populates the block, and §30.4.2 gained the read-only-entity shape — `mode` always `"auto-filter"` (with the `@pk` trap called out explicitly), `mismatch_error` omitted, `column` / `missing_resolver_error` unchanged — plus a paragraph recording that `events` / `soft_delete` are correctly `null` for a view and that **`features.cache` on a cached view stays `null` pending its own spec pass** (a view's `invalidate_on` names *source tables* where a table's `features.cache.invalidates_on` names *methods*, and `key_pattern` / `hydration` describe a read-through path views do not have — §29.2.5 / F9).

**Implementation is 25.7's, not 25.6's, and deliberately so:** no example in the tree pairs `tenancy.enabled: true` with a view, so the change produces **zero golden churn and is unobservable** until 25.7's "add a tenanted example carrying a view" task lands the fixture. Six tasks and four tests are now recorded under 25.7. Sizing confirmed by inspection: **no `schema/v1.json` edit and no manifest version bump** (`features.tenancy` declares `additionalProperties: true` and no `required` array at `v1.json:270-278`, so the shorter object validates against v1 unchanged and the `0.1.0` freeze holds); **no markdown change** (`entity.md.tmpl` renders no features; the only tenancy mention is the global capabilities line at `emit_markdown.go:165`); **no MCP change** — `tools_entity.go:80` already tests `Features.Tenancy != nil` generically, which is the payoff.

Two incidental observations, both pre-existing and outside Ticket B, now have owners. The `TenancyFeature` struct is hand-duplicated across `cmd/sqlgen/manifest/types.go` and the runtime `manifest/types.go` with **no drift guard** (`manifest/deps_test.go` only asserts stdlib purity) — now a **required** 25.7 task, since 25.7 is the first change to diverge the two intentionally. And `missing_resolver_error: "ErrUnauthenticated"` / `mismatch_error: "ErrForbidden"` name sentinels that exist in **neither** the `tenancy` runtime package (which exports `ErrMissing` / `ErrMismatch`) nor the generated `errors_gen.go` — logged as **FIX-180** (tracked), which on investigation widened: four of the five `conventions.error_sentinels` are fabrications back-formed from §26.5.5's GraphQL `extensions.code` column, and six real sentinels go unadvertised. It spans every tenanted **table** and needs a PRD §30.4.1 amendment, so it moves separately; 25.7 has an explicit task **not** to correct the view block's copy in passing.

Reviewer also confirmed the parent's six specific asks: all 3 non-test `BuildTenancyContext` call sites updated and warnings reaching both sinks; the non-nullable tenant-type resolution is sound (`gotype.FromLiteral` pointer-wraps only on `nullable=true`, and nothing consumes the nullable spelling of `TenancyContext.GoType`); the `viewColumnGoType` extraction is behavior-preserving (`&col` on a per-iteration `range` variable); only 3 repo-wide references to the old uniform-type wording, all updated; and both view emission paths end in `imports.Process{FormatOnly: false}`, so the folded tenancy import is provably pruned on either layout.

**One forward-looking trap the reviewer flagged for 25.7:** `ViewContext.Tenancy.FieldName` points at a row-struct field that may carry the *nullable* spelling (`*uuid.UUID`) while `Tenancy.GoType` is the value spelling. That is fine for a `WHERE col = $N` predicate, and unsafe only if the table path's `pk.{{ .Tenancy.FieldName }} != resolvedTenant` shape (`templates/table/get.go.tmpl:94`) is ported verbatim for a `@pk`-annotated tenant column on a view.

---

## 25.7 View tenancy — read-path emission (Ticket B, F1 — the security fix)

**PRD Reference:** Sections 29.4.1 (Reads — `Get` and `GetMany` predicate injection), 29.3.1 (`required` / `ErrMissing`), 29.4.4 (`SkipTenancy` + explicit `Tenant` precedence), 16.4 (view artifact table), 29.8 (composition with soft delete — N/A for views, confirm), **30.1 + 30.4.2 (manifest `features.tenancy` on a view — amended 2026-09-02, see B9)**. Design §3.2, §2-D5, §2-D7.

**Status:** **Complete** (landed 2026-09-03 — `make check` clean across all 33 modules, `make check-examples` clean across all 13 example modules)

**Governing decisions:** D5 (read path only); D7 (matview `Refresh` / `RefreshConcurrently` stay unscoped and Go-client-only).

> **B9 — PRD delta landed ahead of this sub-item (2026-09-02).** 25.6's auto-review found that
> `manifest.buildViewEntity` sets `Features{}` unconditionally, so a tenanted view reports
> `features.tenancy: null` while a tenanted table reports it fully — telling the MCP server and
> every agent reading the manifest that a scoped view's rows are globally visible. The fix needed
> a spec call first (project rule 1), because the table shape does not transfer: `mode` can be
> `"verify-match"`, which is a §29.4.2 / §29.7 mutation rule with no view analogue, and
> `mismatch_error` names an error a view can never return. **§30.1's Tenancy composition row and
> §30.4.2 were amended** to specify the read-only-entity shape — `mode` always `"auto-filter"`,
> `mismatch_error` omitted, `column` / `missing_resolver_error` unchanged — and to record why
> `features.cache` on a cached view stays `null` for now. The implementation is this sub-item's
> work because **this is where the fixture that pins it lands**: no example in the tree combines
> `tenancy.enabled: true` with a view, so the change is invisible in goldens until the tenanted
> example below carries one.

**Depends on:** 25.6.

### Tasks

- [x] `templates/view/client.go.tmpl` — add the `tenantResolver tenancy.TenantResolver[{{ .Tenancy.GoType }}]` field and the `resolveTenant(ctx, explicit)` method, ported from `templates/table/client.go.tmpl:242-305` (whose `resolveTenant` starts at `:272`) **minus** the `captureAffectedTenants` half (mutation-only).
- [x] `templates/view/get.go.tmpl` — inject `AND <tenant> = $N` in `Get` (PK path) and `GetMany`, gated on `!options.SkipTenancy` and the resolver's `apply` flag, matching the table shape at `templates/table/get.go.tmpl:188-201`.
- [x] `templates/view/count.go.tmpl` — same injection in `Count`.
- [x] `templates/view/pagination.go.tmpl` — same injection in `Paginate` and `Connection`; confirm the tenant predicate composes with keyset cursor conditions without disturbing placeholder numbering.
- [x] Confirm **no** `CallOptions` change is needed — `SkipTenancy` and `Tenant *T` are already package-level whenever tenancy is enabled (`context_shared.go:50-59`), and view methods already accept `opts ...func(*CallOptions[...])`.
- [x] Confirm `Refresh` / `RefreshConcurrently` on materialized views receive **no** tenant predicate (D7) and add a template comment saying why, so a future contributor does not "fix" the omission.
- [x] Verify no cache path is touched (F9).
- [x] Add / extend a tenanted example carrying a view so the golden output pins the new emission. **This fixture is what makes the manifest tasks below observable** — pick an example that emits a manifest (`postgres`, `graphql`, and `sqlite` do; `cache` does not), or add manifest emission to the one chosen.
- [x] **Manifest — `features.tenancy` on a tenanted view (B9, §30.4.2).** Replace `Features{}` in `buildViewEntity` (`cmd/sqlgen/manifest/builder.go:382`) with a `buildViewFeatures` block. Add a `buildViewTenancyFeature` mirroring `buildTenancyFeature` (`:947`) but with `Mode` pinned to `"auto-filter"` and `MismatchError` left empty. It must **re-derive from `cfg` via `config.ResolveViewTenancyEnabled` / `…Column` (landed in 25.6), not read `ViewContext.Tenancy`** — the builder's documented isolation property (`builder.go:942-946`) is that `Build` runs without the orchestrator's attach pass, and `manifest`'s own `testBuildInput` calls `gen.BuildViewContexts` directly, so `vc.Tenancy` is nil there.
- [x] **Manifest — add a `findViewConfig` to `cmd/sqlgen/manifest/builder.go`.** The package has `findTableConfig` (`:113`) but no view twin; the qualified-then-bare key resolution is the same.
- [x] **Manifest — `mismatch_error` gets `,omitempty`, in BOTH `cmd/sqlgen/manifest/types.go:205` and `manifest/types.go`.** Those two `TenancyFeature` declarations are hand-duplicated (the runtime copy is stdlib-only and embedded into every consumer binary). Tables always populate the field, so this adds no table churn.
- [x] **Manifest — add the `TenancyFeature` drift guard (required, not optional).** No test currently ties the builder-side `cmd/sqlgen/manifest.TenancyFeature` to the runtime `manifest.TenancyFeature`; `manifest/deps_test.go` only asserts stdlib purity. This sub-item is the first change to *diverge* them intentionally — `omitempty` on one field — which is precisely when a silent mismatch becomes cheap to introduce and expensive to notice: the builder marshals, the runtime unmarshals, and a field the runtime does not declare is dropped on `Client.Manifest()` with no error anywhere. Compare the two structs by `reflect` (field name, type, and **json tag including options**) from a test in the `cmd/sqlgen` module, which may import the runtime package. Extend it to the sibling feature structs (`Features`, `CacheFeature`, `EventsFeature`, `SoftDeleteFeature`) if that costs no extra machinery.
- [x] **Manifest — confirm no schema or version change is needed.** `schema/v1.json`'s `features.tenancy` (`:270-278`) declares `additionalProperties: true` and **no `required` array**, so the shorter view object validates against v1 unchanged; the JSON Schema stays frozen at `0.1.0`. Confirm against the real-output schema check rather than by reading.
- [x] **Manifest — confirm the MCP surface needs no change.** `cmd/sqlgen/mcp/tools_entity.go:80` already tests `e.Features.Tenancy != nil` generically, so a tenanted view starts reporting `tenancy` for free. This is the payoff surface — verify it, do not modify it.
- [x] ~~**Keep `missing_resolver_error: "ErrUnauthenticated"` on the view block for now, and do not correct it here — see FIX-180.**~~ **Dropped 2026-09-03 — FIX-180 landed first.** The view block now inherits the corrected `missing_resolver_error: "tenancy.ErrMissing"` from `buildTenancyFeature` with no view-specific work; there is nothing left to keep consistent. FIX-180 also corrected `conventions.error_sentinels` (four of five names were fabricated), `methods[].errors[]` on every table, and the `database.ErrRefreshConcurrentlyInTx` qualification, and amended PRD §30.4.1 / §30.4.2.
- [x] **Leave `features.cache` on a cached view as `null`** and do not "fix" it in passing — §30.4.2 now records the three unsettled semantics (a view's `invalidate_on` names *source tables* where a table's `features.cache.invalidates_on` names *methods*; `key_pattern` and `hydration` describe a read-through path views do not have). That needs its own spec pass; a note-only task, no code.

### Acceptance Criteria

- All five view read methods (`Get`, `GetMany`, `Count`, `Paginate`, `Connection`) apply the tenant predicate — the cross-tenant read hole is closed at the Go-client layer, before any API exposure.
- `CallOptions.SkipTenancy: true` bypasses the predicate; an explicit `CallOptions.Tenant` wins over the ctx resolver, matching §29.4.4 precedence exactly as on tables.
- Under `tenancy.required: true` with an absent/zero resolver, every view read returns `ErrMissing` rather than silently reading cross-tenant.
- Under `tenancy.required: false` with a zero resolver, `apply=false` skips the predicate (the documented cross-tenant read).
- `Refresh` / `RefreshConcurrently` emit no tenant predicate.
- Non-tenanted views and tenancy-disabled projects regenerate **byte-identically**.
- A tenanted view's manifest entry carries `features.tenancy` with `mode: "auto-filter"`, the resolved `column`, `missing_resolver_error`, and **no `mismatch_error` key** (B9 / §30.4.2). A shared view still carries `features.tenancy: null`. A `@pk`-annotated tenant column on a view does **not** produce `"verify-match"`.
- The emitted manifest still validates against `schema/v1.json` with no schema edit and no version bump.
- A test fails if the builder-side and runtime `TenancyFeature` structs disagree on any field name, type, or json tag — including tag options like `omitempty`.
- Cursor pagination on a tenanted view produces correct, stable pages — the tenant predicate does not corrupt keyset placeholder numbering.

### Tests Required

- [x] Template unit tests: each of the five read methods emits the predicate for a tenanted view and omits it for a shared one.
- [x] Template unit test: matview `Refresh` / `RefreshConcurrently` emit no predicate.
- [x] Golden test: a tenancy-disabled project's view output is byte-identical to pre-change.
- [x] **E2E (postgres + mysql + sqlite), the F1 regression pin:** seed two tenants' rows visible through one view; assert each of `Get` / `GetMany` / `Count` / `Paginate` / `Connection` returns only the resolver's tenant.
- [x] E2E: `SkipTenancy: true` returns both tenants' rows; explicit `Tenant` returns the named tenant's rows.
- [x] E2E: `required: true` + absent resolver → `ErrMissing` from every view read method.
- [x] E2E (postgres): cursor pagination across a multi-page tenanted view returns each row exactly once and no rows from the other tenant.
- [x] E2E: a matview `Refresh` on a tenanted matview recomputes the whole relation (unscoped) and subsequent reads are still tenant-scoped.
- [x] Manifest unit tests (`cmd/sqlgen/manifest/builder_test.go`, mirroring `TestBuild_TenantedTable:155`): tenanted view → `features.tenancy` populated, `mode == "auto-filter"`, `MismatchError == ""`; shared view → nil; `views.<n>.tenancy.enabled: false` → nil; tenancy globally disabled → nil; **a view whose tenant column carries `@pk` → still `"auto-filter"`** (the regression this shape exists to prevent).
- [x] Manifest JSON test: the marshalled view entity has **no `mismatch_error` key** at all, not an empty string — `omitempty` is doing the work.
- [x] Golden: the new tenanted-view example's `manifest_gen.json` and `entities/<view>.json` pin the populated block.
- [x] Drift guard: a reflect-based test asserting the builder-side and runtime `TenancyFeature` declarations match field-for-field, tag options included. Verify it actually fails by temporarily dropping `omitempty` from one side.

### Completion Record

**Landed 2026-09-03.** `make check` clean across all 33 modules; `make check-examples` clean across all 13 example modules. The cross-tenant read hole is closed at the Go-client layer, ahead of any API exposure (Ticket C).

**Files changed — 6 templates/code + 6 new test files + 3 example fixtures + regenerated goldens:**

| File | Change |
|---|---|
| `cmd/sqlgen/gen/templates/view/client.go.tmpl` | `tenantResolver tenancy.TenantResolver[T]` field + `resolveTenant(ctx, explicit)`, ported from the table client **minus** `captureAffectedTenants` |
| `cmd/sqlgen/gen/templates/view/get.go.tmpl` | tenant predicate in `GetMany`, between the filter conditions and `input.conditions` — the same slot the table path uses |
| `cmd/sqlgen/gen/templates/view/count.go.tmpl` | tenant predicate in `Count` |
| `cmd/sqlgen/gen/templates/view/pagination.go.tmpl` | tenancy-gated comment only — see the deviation note below |
| `cmd/sqlgen/gen/templates/view/refresh.go.tmpl` | template-only comment recording why D7 emits no predicate (zero output delta) |
| `cmd/sqlgen/manifest/builder.go` | `findViewConfig`, `buildViewFeatures`, `buildViewTenancyFeature`; `buildViewEntity` stops hard-coding `Features{}` |
| `cmd/sqlgen/manifest/types.go`, `manifest/types.go` | `MismatchError` gains `,omitempty` on **both** hand-duplicated declarations |
| `cmd/sqlgen/gen/view_tenancy_emit_test.go` | **new** — 8 template-emission tests |
| `cmd/sqlgen/manifest/feature_drift_test.go` | **new** — reflect-based builder↔runtime drift guard |
| `cmd/sqlgen/manifest/builder_test.go` | 4 new builder tests (tenanted view, `@pk` tenant column, 3 shared-view shapes, JSON `omitempty`) |
| `cmd/sqlgen/gen/context_tenancy_view_test.go` | 25.6's `…LeavesViewFileUnchanged` inverted into `TestGenerate_tenantedViewEmitsReadPathScoping` |
| `cmd/sqlgen/mcp/tools_entity_test.go` | `TestFeaturesSummary_TenantedView` — verifies (does not modify) the payoff surface |
| `cmd/sqlgen/cli/manifest_validate_e2e_test.go` | `tenancy_postgres` added to the real-output schema check, `perEntity: true` |
| `testdata/examples/tenancy/{views/,sqlgen.yml,tests/}` | sqlite fixture: `product_stats` (detected) + `workspace_summaries` (opted out); 8 E2E tests |
| `testdata/examples/tenancy_postgres/{views/,sqlgen.yml,tests/}` | postgres fixture: `article_stats` + matview `workspace_line_totals` (`@pk` on the tenant column); manifest enabled, `json_layout: per_entity`; 6 E2E + 2 manifest tests |
| `testdata/examples/tenancy_mysql/{views/,sqlgen.yml,tests/}` | mysql fixture: `article_stats` on a CHAR(36) tenant; 3 E2E tests |

**Two deliberate deviations from the task wording, both toward the table path's shape.**

1. **`Paginate` / `Connection` get no injection of their own.** The task said "same injection"; the correct emission is none. Both compose `Count` and `GetMany` through `internalOpts`, which copies the caller's `CallOptions` verbatim — so the predicate already reaches them, and a second append would duplicate it and add a redundant bind. `templates/table/pagination.go.tmpl` has no tenancy block for exactly this reason. The templates carry a tenancy-gated comment saying so, and `TestViewTenancy_readMethodsEmitPredicate` asserts the predicate is **absent** from the pagination template while `*o = options` appears twice.

2. **`Get` does not hoist the resolve to the entity-method boundary.** The table's `Get` resolves early to stash `qctx.Tenant` for the cache read-through hook to build a tenant-scoped key (§29.5). A view has no read-through path (§29.2.5, F9), so the hoist would buy nothing and cost a second resolver call. `Get` composes `GetMany`, which applies the predicate. **Consequence worth knowing:** `hook.QueryContext.Tenant` stays zero for view reads. Nothing in the generated code reads it for views; a consumer-written query hook that wanted the tenant would have to resolve it itself. If a view read-through path ever lands, this is the line that moves.

Neither deviation weakens the spec: §29.4.1 and §29.2.5 require the predicate to **apply** on all five methods, which delegation achieves, and the E2E suites assert it method-by-method against three real databases.

**Placeholder numbering under keyset pagination — confirmed, not assumed.** `GetMany` appends the tenant condition *before* `input.conditions`, so the tenant bind precedes the cursor binds in the condition slice `sql.BuildSelect` numbers. `TestViewTenancy_predicateOrderIsStable` pins the ordering in the template; `TestViewTenancy_CursorPaginationStaysScoped` (sqlite and postgres) walks a 5-row tenanted view two rows at a time and asserts each row comes back exactly once with no row from the other tenant.

**Non-tenanted output is untouched.** No `views_gen.go` in the `sqlite`, `postgres`, `postgres_stdlib`, `mysql` or `cache` examples changed — the existing E2E goldens are the proof, and `TestViewTenancy_sharedViewOutputUnchanged` additionally asserts that attaching a `Tenanted: false` context produces output byte-identical to the tenancy-disabled render.

**Manifest (B9).**

- `buildViewTenancyFeature` re-derives from `cfg` via `ResolveViewTenancyEnabled` / `…Column`, never from `ViewContext.Tenancy` — `Build` runs without the orchestrator's attach pass, and `manifest`'s own `testBuildInput` calls `gen.BuildViewContexts` directly, so `vc.Tenancy` is nil there. It also deliberately does **not** consult `vc.PKColumns`: `Mode` is the constant `"auto-filter"`, so an `@pk`-annotated tenant column cannot reach `"verify-match"`. `workspace_line_totals` is that shape in the goldens.
- **No schema edit, no version bump.** Confirmed against the real-output check, not by reading: `tenancy_postgres` joins `TestManifestValidate_E2EExamples` with `perEntity: true`, so its `manifest_gen.json` and all four `entities/*.json` validate against the embedded frozen `schema/v1.json`. `schema_version` stays `0.1.0`.
- **MCP needs no change and was not changed.** `featuresSummary` tests `Features.Tenancy != nil` generically, so a tenanted view reports `tenancy` for free; `TestFeaturesSummary_TenantedView` pins it.
- **Drift guard verified failing.** Temporarily dropping `omitempty` from the runtime side produced `field MismatchError json tag: builder = "mismatch_error,omitempty", runtime = "mismatch_error"` before being reverted. The guard covers `Features`, `SoftDeleteFeature`, `CacheFeature`, `EventsFeature` and `TenancyFeature` — field name, shape, and json tag including options.
- `features.cache` on a cached view stays `null`, untouched (§30.4.2 records the three unsettled semantics).

**Fixture placement.** The manifest golden lives in `tenancy_postgres` rather than the sqlite `tenancy` example: it is the smallest manifest-free tenanted example (2 tables), so enabling emission there adds a compact golden tree instead of 13 entities, and it puts the manifest pin in the same module as the postgres E2E leg. `per_entity` was chosen so both `manifest_gen.json` and `entities/<view>.json` carry the block, as the task required.

**Known cross-tenant read that is intentional.** `workspace_summaries` (sqlite) carries `views.workspace_summaries.tenancy.enabled: false` and returns both workspaces' rows — the §29.2.5 opt-out, pinned by `TestViewTenancy_OptedOutViewIsCrossTenant` so a future change that starts scoping opted-out views fails loudly.

**Auto-review (`sqlgen-reviewer`) — 7/8 PRD requirements PASS, 1 PARTIAL, 12/12 required tests present, 2 findings, both fixed inline.**

The reviewer independently re-derived both deviations rather than accepting them: it confirmed `resolveCallOptions` is idempotent (so the `*o = options` re-entry through `internalOpts` cannot un-normalize the §29.4.4 precedence), that `templates/table/pagination.go.tmpl` has no tenancy block either, and — for the `Get` hoist — that **PRD §29.9 explicitly says sqlgen "does not re-expose [the tenant] on the `QueryContext`"**, the table's population being cache-internal to §29.5. It grepped `cache/` and `hook/` and found no runtime consumer of `QueryContext.Tenant`. So the zero-`Tenant` consequence is not a spec violation, only the line that moves if a view read-through path lands. It also confirmed no escape path: no `Stream` / `Exists` on views (§16.4), `GetMany` reads the pre-hook `options` so a hook cannot flip `SkipTenancy` on, and the empty-projection early return reads no rows.

| # | Finding | Disposition |
|---|---|---|
| 1 | **A tenanted view's `methods[].errors` omitted `tenancy.ErrMissing`.** 25.7 is what made view read bodies reach it — the E2E asserts exactly that for all five methods — but `buildViewMethods` hardcoded `[]` / `["ErrNotFound"]` / `["ErrInvalidCursor"]`, with no view twin of `tenancyReachForTable`. Same under-report shape as B9 and the same defect class as FIX-180, one entity kind over: an agent reading `article_stat.json` would conclude the read cannot fail on a missing tenant. | **Fixed inline** — `tenancyReachForView` added beside its table twin (mirroring `buildViewTenancyFeature`'s guards plus `ResolveViewTenancyRequired`; `inPK` / `pkVerify` stay false by construction, so a view can never advertise `ErrMismatch`), threaded through all five view reads via the existing `errs` / `tenancyErrors` helpers. `Refresh` / `RefreshConcurrently` deliberately take none — they resolve no tenant. Pinned by `TestBuild_TenantedViewMethodErrors` (both `required` branches, plus an explicit no-`ErrMismatch` assertion) and by the `tenancy_postgres` E2E manifest test. |
| 2 | Both `TenancyFeature` doc comments cited `TestTenancyFeature_noDriftBetweenBuilderAndRuntime`; the test is `TestFeatureStructs_noDriftBetweenBuilderAndRuntime` — naming a symbol that does not exist is what FIX-180/182 just cleaned up. | **Fixed inline** in both `cmd/sqlgen/manifest/types.go` and `manifest/types.go`. |

The reviewer's one coverage gap (no builder test for `views.<n>.tenancy.column` or the schema-qualified `findViewConfig` branch) is closed by `TestBuild_ViewTenancyRespectsPerViewOverrides`, which drives a view projecting `org_id` under a global `workspace_id` through the qualified key, the bare key, and a non-matching key.

**Three observations the reviewer raised that are deliberately not fixed here.** (1) `resolveTenant` consults `tenancy.CachedTenant[T](ctx)` before the `required` branch, so a ctx entry stashed with `apply=false` by a `required:false` table mutation could make a nested view read fall open — but this is the table path's own inherited semantic (`table/client.go.tmpl:283`), the stash is skipped under `SkipTenancy`, and it is reachable only from a consumer hook issuing a view read inside a mutation chain. Not new, not a poisoning vector. (2) `docs/design/MANIFEST.md` §5.4 still documents no `features.tenancy` for views and its example lists a fabricated `List` method — pre-existing sibling-doc drift, `/close-phase` work. (3) `sql_bodies` omit the tenant predicate for views *and* tables alike — pre-existing convention.

**25.6's transient closed.** `applyClientTenancy` has been putting tenanted views in `ClientContext.TenantedEntities` since 25.6, emitting `c.<view>.tenantResolver = options.tenantResolver` against a field the view template did not declare. That field now exists; the sqlite/postgres/mysql tenancy examples all compile with a tenanted view for the first time.

---

## 25.8 Views into `APIContext` — context, config, and gating (Ticket C, D8)

**PRD Reference:** Sections 26.4 (view row per B5), 26.5.1 (per-surface generation table), 26.10 (per-entity API config), 4.9 / 4.13 (`views.<n>.api` per B7), 16.4 (read-only artifact set). Design §3.3, §2-D8, §2-D9.

**Status:** **Complete** (landed 2026-09-04 — `make check` clean across all 33 modules, `make check-examples` clean across all 12 example modules)

**Governing decisions:** D8 (views become first-class `APIContext.Tables` entries with `IsView: true`, not a parallel context type); D9 (ships strictly after 25.7).

**Depends on:** 25.7 (D9 — the security gate) **and** 25.1.

### Tasks

- [x] Add `IsView bool` to `APITableContext`.
- [x] Add `ViewAPIConfig` (`enabled *bool`, `operations *Operations`) to `config.ViewConfig`; resolve it through a `config.ResolveViewAPIEnabled` + a view-scoped operations mask.
- [x] Restrict the view operations mask to the read set (`get`, `get_many`, `paginate`, `connection`); setting any mutation key (`create`, `update`, `upsert`, `soft_delete`, `hard_delete`, `restore`, …) inside `views.<n>.api.operations` is a **config error**, in the same spirit as `clientOnlyOperationFields` (`config/config.go:686-692`) — silently ignoring a knob the user deliberately turned is worse than saying the block cannot express it.
- [x] Extend `BuildAPIContext` to append one `APITableContext` per API-enabled view: `IsView: true`, read-only `Operations`, `HasCreateInput: false`, `HasUpdateInput: false`, `Relationships: nil`, `PKArgs` populated only when the view is `@pk`-annotated (`HasPK`).
- [x] Change `generateAPI`'s signature to take `views []ViewContext` and thread them from `orchestrate.go:160`.
- [x] Confirm `funcHasAnyMutation` returns false for a view (it gates on `HasCreateInput` / `HasUpdateInput` / delete-restore ops, all false) so no empty `extend type Mutation` block is emitted — and add an explicit `IsView` guard rather than relying on that coincidence.
- [x] Generalize `ValidateAPIWalkerCompleteness` to take the column set + relationship set instead of a `TableContext`, so views are linted identically; same for 25.2's two new lints.
- [x] **Extend `collectGraphQLTypeNames` (`gen/api_names.go`) with a view arm** — seven claimed names per view (`<V>`, `<V>Connection`, `<V>Edge`, `<V>ListResult`, `<V>Filter`, `<V>SortField`, `<V>Sort`; **no** `Create`/`Update` inputs, views being read-only), claimed through a `viewRef` so the error offers `views.<n>.struct_name`. FIX-175 landed the check over tables only because `APIContext` had no views to iterate; PRD §26.4's ownership table already reads "Per table / **view**". Without this a view colliding with a scalar, a table, or a comparator input (a view named `duration` beside an `interval` column → `type Duration` + `scalar Duration`) surfaces as gqlparser's `Cannot redeclare type X` naming neither claimant — the exact failure mode FIX-175 exists to replace. A `TODO(25.8/25.9)` marks the function.
- [x] Add a `sqlgen lint` / codegen guard that a view never reaches a mutation-emitting code path.
- [x] Add §4.13 validation for `views.<n>.api`.

### Acceptance Criteria

- Every API-enabled view is present in `APIContext.Tables` with `IsView: true`; no parallel `APIViewContext` type exists.
- A view's resolved operations mask can never contain a mutation; a mutation key in `views.<n>.api.operations` is a config error naming the view and the key.
- `views.<n>.api.enabled: false` excludes the view from the API context entirely (type, queries, inputs, translators, bindings).
- A view with no `@pk` annotation produces no `Query.<view>` by-PK field; a view whose `cursor_keys` do not resolve produces no connection query (existing `HasConnection` gating, unchanged).
- The three completeness lints accept views without special-casing them.
- Tables' generated API output is **byte-identical** — this sub-item only adds view entries.
- 25.7 is Complete before this sub-item lands (D9), recorded in the completion record.

### Tests Required

- [x] Unit test: an API-enabled view appears in `APIContext.Tables` with `IsView: true`, read-only operations, and both `Has*Input` flags false.
- [x] Unit test: `views.<n>.api.enabled: false` → the view is absent from the context.
- [x] Config-error tests: each mutation key inside `views.<n>.api.operations` errors, naming the view and key.
- [x] Unit test: `funcHasAnyMutation` is false for every view context shape, including a view with an `@pk`.
- [x] Unit test: a `@pk`-less view has empty `PKArgs` and no by-PK query; a view with unresolvable `cursor_keys` has `HasConnection: false`.
- [x] Unit test: the completeness lint passes for a view context — **one of three, accurately.** `ValidateAPIWalkerCompleteness` is generalized and tested against a view (`TestValidateAPIWalkerCompleteness_view`); 25.2's `ValidateAPIFilterCompleteness` / `ValidateAPISortCompleteness` do not exist yet, so there is no code to lint a view with. The `APIEntity` shape they were told to take is in place and is what they should be built on.
- [x] Golden test: table API output unchanged by this sub-item.

### Completion Record

**Landed 2026-09-04.** `make check` clean across all 33 modules; `make check-examples` clean across all 13 example modules. **D9 satisfied and recorded:** 25.7 (view tenancy read-path emission) was Complete on 2026-09-03, so the Go-client tenant predicate was already in place before any view reached the HTTP surface — the sequencing this ticket exists to enforce.

**The one thing the task list did not anticipate: this sub-item emits.** The tracker (and design §3.3) reads as though 25.8 is context-only and 25.9 turns emission on. That holds only if no example pairs `api:` with views — and **`mysql` and `sqlite` do both**, through `input.views: ./views/` rather than a `views:` config block, which is why a `grep '^views:'` over the example configs misses it. The moment `BuildAPIContext` appends a view to `APIContext.Tables`, every downstream artifact that iterates that slice emits for it. That is not a side effect to be suppressed — it is **exactly what D8 says should happen** ("every downstream template … already iterates `apiCtx.Tables`; a parallel type would fork all of them"), and gating views back out of the two examples to keep the golden tree still would have been the "turn off the broken case to unblock" move. So the emission landed here, and 25.9 becomes verification plus the fixtures that prove tenancy and query-count end to end.

**What the D8 premise bought, verified rather than assumed.** With one view entry in the slice, and with **no per-view template arm anywhere**, the existing table templates produced the whole PRD §26.4 closed set: the object type, `<V>Connection` / `<V>Edge` / `<V>ListResult`, `<V>Filter`, `<V>Sort` + `<V>SortField`, the field-options walker, the filter and sort translators, the envelope aliases, the resolver seeds, the `Q` read methods, and the gqlgen `models:` bindings. `mysql`'s `category_stats` (no `@pk`, no `id` for `cursor_keys` to land on) emits `categoryStatList` **only**; `product_summary` (`@pk: id`) emits all three Query fields. Neither emits an `extend type Mutation` block, a `Create`/`Update` input, or a relationship case. `graph/model/models_gen.go` gained only the two filter inputs and two sort enums — no `model.CategoryStat` object type — which is the proof that `buildMergeInput` bound the view's object type and envelopes for free, **retiring the hand-written `models:` entry a consumer needs today (F3's payoff, reached one sub-item early).**

**Two template deltas were required, both because a view genuinely differs from a table:**

| Delta | Why it was not optional |
|---|---|
| `templates/api/resolvers.go.tmpl` — `q.Client.{{ $t.StructNamePlural }}()` → `{{ $t.ClientAccessor }}()` (17 sites) | The unified client names a table's accessor **plurally** (`Client.Products()`) and a view's **singularly** (`Client.ProductSummary()` — `context_client.go:55`). The resolver template had the plural spelling baked in, so the first view to reach it emitted a call to a method that does not exist. `ClientAccessor` is filled from a new `entityAccessorName(structName, isView)` that `BuildClientContext` now also calls, so the declaring side and the calling side read one function. |
| `templates/api/schema.graphqls.tmpl` — the type-description fallback moved onto the context | The fallback read `"<T> corresponds to the <sql> **table**."` with the noun hard-coded, so every view would have been documented as a table. `APITableContext.Description` is now fully resolved by `apiEntityDescription`, and the template is a bare `{{ $t.Description }}` (guidelines/TEMPLATES.md §6 — contexts carry pre-computed data). Byte-identical for tables. |

**Files changed — 8 code + 2 templates + 2 new test files + 20 mechanical test updates + regenerated goldens:**

| File | Change |
|---|---|
| `cmd/sqlgen/config/config.go` | new `ViewAPIConfig` (`enabled`, `operations`) + `ViewConfig.API`; `viewAPIOperationNames` (the read set) and `mutationAPIOperationFields` **derived from `apiOperationFields`** so a future API operation lands in one table; `ResolveViewAPIEnabled`, `ViewAPIOperationsMask`, `ResolveViewAPIOperationsMask` |
| `cmd/sqlgen/config/validate.go` | `sortedViewKeys`; `validateViewAPIOperations` (mutation key → error naming the view and the key; client-only key → the existing message; preset validated); `validateViewAPIOptIn` (§4.13 opt-out-only, mirroring `validateManifestPerTableOptIn`) |
| `cmd/sqlgen/gen/context_api.go` | `IsView` + `ClientAccessor` + pre-resolved `Description` on `APITableContext`; new **`APIEntity`** narrow source with `apiEntityFromTable` / `apiEntityFromView` (+ exported constructors); `buildAPIColumnFields` / `buildAPIPKArgs` / `buildAPIFilterFields` / `buildAPISortBindings` retargeted onto it; `viewReadOperations`, `resolveViewAPIOperations`, `viewAPIEnabled`, `buildOneAPIView`, `buildAPIViewContext`; `BuildAPIContext` gains a `views` parameter and a view arm |
| `cmd/sqlgen/gen/api_walker.go` | `ValidateAPIWalkerCompleteness` takes an `APIEntity` (column set + relationship set + the kind/name pair the message is phrased in) instead of a `TableContext` |
| `cmd/sqlgen/gen/api_view_lint.go` | **new** — `ValidateAPIViewReadOnly`, the codegen guard that a view reaches no mutation-emitting path |
| `cmd/sqlgen/gen/api_names.go` | view arm on `collectGraphQLTypeNames` (seven names, claimed through `viewRef`); the `TODO(25.8/25.9)` is gone |
| `cmd/sqlgen/gen/funcmap.go` | explicit `IsView` guard at the top of `funcHasAnyMutation` |
| `cmd/sqlgen/gen/context_client.go` | `entityAccessorName` extracted; both accessor spellings now read it |
| `cmd/sqlgen/gen/orchestrate.go` | `generateAPI` takes `views []ViewContext`; new `BuildEntityContextsFromSchema` returning both halves (`BuildTableContextsFromSchema` is now a thin wrapper); `ValidateGeneration` threads views into its API-context build |
| `cmd/sqlgen/cli/graphql.go` | the `sqlgen graphql gen` wrapper path builds view contexts and passes them, so the `models:` merge sees views |
| `cmd/sqlgen/gen/api_view_test.go`, `cmd/sqlgen/config/view_api_test.go` | **new** — 10 + 7 tests |

**One data-hygiene refinement beyond the task list, changing no output.** `buildAPIColumnFields` computes `InCreateInput` / `InUpdateInput` per column; on a view those flags are read by nothing (every consumer sits behind the `HasCreateInput` / `HasUpdateInput` gate, and `CreateInputFields` / `UpdateInputFields` are nil), but they were still being set to `true` on writable view columns. A field context asserting membership in an input type the entity never emits is precisely the shape this phase is removing, and the next surface to read it — a REST projection, 25.12's relationship filters — would have no way to know the claim was inert. `APIEntity.readOnly()` (one predicate over the `entityKindTable` / `entityKindView` constants) now skips both assignments, and `ValidateAPIViewReadOnly` asserts it per field. **Verified to move nothing: `TestE2EGoldenFiles` passes against the committed goldens with no `-update-e2e` run after the change.**

**Design note — why `APIEntity` rather than a synthetic `TableContext`.** The four shared builders all took a `TableContext` and read six fields from it. Handing them a `TableContext` assembled from a `ViewContext` would have worked today and read table-only fields as zero values tomorrow — the exact silent-divergence shape design §1.3 indicts. `APIEntity` carries only what the read half actually needs (kind, name, schema, struct name, dialect, columns, PK columns, filter fields, relationships, incrementable set), so the view path runs the **same code**, and the two places a view legitimately differs (`Relationships: nil`, `Incrementable: nil`) are stated in `apiEntityFromView` with the reason, not left implicit.

**Gating, as implemented.** A view's base operation set is `{Get: HasPK, GetMany: true, Paginate: true, Connection: HasConnection}` — the two conditional Query fields from §26.4's table are expressed as **operation** gating rather than a template condition, so the schema, the resolver and the seed all agree by construction and a `@pk`-less view cannot emit `productSummary(): ProductSummary` (an invalid empty argument list). Every mutation is false by construction, not by masking: `maskAPIOperations` only ANDs, so a mask can never add one, and `validateViewAPIOperations` rejects the attempt at config-load time before it could try.

**Two lints, not one.** `ValidateAPIWalkerCompleteness` now lints views identically (a view has no relationships, so its second loop has nothing to check — the correct outcome, not an exemption). `ValidateAPIViewReadOnly` is new and is the "codegen guard that a view never reaches a mutation-emitting code path" the task asked for: it asserts the eighteen fields every mutation template gates on, **and** closes by asserting `funcHasAnyMutation` itself, so an operation added to that disjunction later is caught even if nobody extends the field list.

**Byte-identity, measured not asserted.** Of the 13 example modules only `mysql` and `sqlite` changed — the two that pair `api:` with views. In every changed shared file (`api_envelopes_gen.go`, `filter_translate_gen.go`, `sort_translate_gen.go`, `comparator_translate_gen.go`, `field_options_gen.go`, `resolvers_gen.go`, `models_gen.go`) the diff has **zero removed lines**: purely additive. `graph/generated_gen.go` shows removals only because gqlgen re-sorts its own output alphabetically when a type is inserted; a multiset comparison of removed vs added lines leaves **exactly one** genuinely-deleted line per file — the `//go:embed` directive, replaced by one that also lists the two new view schema files. No table `.graphqls` changed at all. `TestBuildAPIContext_addingAViewLeavesTableOutputIdentical` pins the same fact at unit level.

**Deviations from the task wording — one, and it is a scope note rather than a behavior change.**

1. **"The three completeness lints accept views without special-casing them."** Only one of the three exists: 25.2 has not landed, so `ValidateAPIFilterCompleteness` / `ValidateAPISortCompleteness` have no code to generalize. The walker lint is generalized and tested against a view; the `APIEntity` shape 25.2's two lints were told to take is in place and is what they should be built on. **Nothing here blocks 25.2, and 25.2 needs no rework of this sub-item.**

**Carried into 25.9 (reduced, not removed).** The emission half is done and verified against `mysql` + `sqlite`, so 25.9's remaining work is the proof layer and the fixtures: the tenanted view in the `graphql` example, the **F1+F3 combined E2E pin** (two tenants over the real gqlgen server across all three read queries), the §25.1 query-count E2E with a counting `Querier`, the enum / JSONB filter E2E, the postgres matview read-surface + no-`Refresh` check, and a unit test on `buildMergeInput`'s four view bindings. The `collectGraphQLTypeNames` claim set is already regression-tested against a view/table collision and a view/scalar collision (the PRD's own `Duration` example), which is 25.9's "the two lists are the same seven names written in two places" guard.

**A real defect this emission surfaced, found by the user and fixed here (the `parser/view.go` JSON-aggregate divergence).** `mysql`'s `category_stats.product_names_json` (a `JSON_ARRAYAGG` column) shipped a **panicking gqlgen field resolver** — `panic(fmt.Errorf("not implemented: ProductNamesJSON"))` — in the golden tree. It compiled, so both gates stayed green while the generated server would panic on the first query selecting the field.

The chain, all of it pre-existing and only reachable once views joined the GraphQL surface:

1. `parser/view.go`'s `inferAggregateType` typed every JSON aggregate (`JSON_AGG` / `JSONB_AGG` / `JSON_ARRAYAGG` / `*_OBJECT_AGG`) as `json.RawMessage`.
2. The table path resolves `json` / `jsonb` columns to `types.JSON` — PRD §26.4 calls that "the default for JSON/JSONB columns".
3. `context_api.go`'s built-in scalar registry maps **both** Go types onto the one GraphQL scalar `JSON`, with different marshaling strategies (`method` vs `external`).
4. `registerScalarUse` is **first-registration-wins keyed on the scalar name alone**, and tables are built before views — so the tables' `types.JSON` won, the view's `json.RawMessage` was silently dropped, no external marshaler was emitted, and gqlgen could not bind the struct field.

**Fix (user decision, 2026-09-04): align the parser to `types.JSON`.** One Go type per GraphQL scalar is the model sqlgen actually implements, and §26.4 already names `types.JSON` the default for JSON columns, so the view path was the side that was out of step. `types.JSON` is a *named* `json.RawMessage` (`types/json.go:20`) and is already in `gotype.knownGoTypes` and `isNativelyNilable`, so nothing moves at the scan layer — it only adds the `MarshalGQL` / `UnmarshalGQL` pair the API surface needs. **Bonus:** the column now resolves to `*comparator.JSON` and projects as `JSONComparator`, so it picks up 25.5's real JSON family instead of the `StringComparator` fallback this record previously logged as a "known non-blocking observation" — that observation is closed, not carried.

Golden impact: the mysql view's `ProductNamesJSON` moves `json.RawMessage` → `types.JSON`; `postgres` and `postgres_stdlib` carry the same aggregate but have no `api:` block, so only their view model field moves. The stale `category_stat_gen.resolvers.go` was deleted and regenerated so gqlgen did not preserve the dead stub in its `!!! WARNING !!!` block. **Final example-tree footprint:** `mysql` and `sqlite` (the two pairing `api:` with views), plus `postgres` / `postgres_stdlib` (view model field only, no API). `graphql`, `graphql_null_wrappers`, `graphql_top_level` and every non-API example are untouched.

**The two neighbouring aggregate fallbacks were investigated and deliberately left alone.** `inferAggregateType` also returns `[]any` for `ARRAY_AGG` and `any` for an unrecognised aggregate, which looked like the same class as the JSON bug. Measured, they are its opposite: neither has a GraphQL binding, so `mapColumnToGraphQL` **refuses to generate**, naming the view, the column, the Go type and two remedies (PRD §26.4.1) — fail-closed at codegen, where the JSON case failed open and shipped a runtime panic. Working as specified; no change made. `TestBuildAPIContext_viewColumnWithNoGraphQLBindingIsRejected` pins it, because nothing else did and a fallback binding added here later would reproduce the JSON failure mode exactly.

**Open, not fixed — `ARRAY_AGG` element-type inference — logged as FIX-183 (tracked; needs a spec answer, PRD §16.3).** `ARRAY_AGG(p.title)` could resolve to `[]string` the way `SUM`/`MIN`/`MAX` already resolve through `resolveFromSchema` + the `col.Aggregate` routing, which would give both a real scan target and a real GraphQL binding (`[String!]!`) instead of an outright refusal. It is a change to the parser's documented §16.3 fallback, needs dialect-aware array handling, and still needs `[]any` for aggregates over expressions rather than columns — so it is a design item, not an inline fix. No example exercises it; `@type` is the documented escape today. Tracked as **FIX-183**, resolvable before phase closure.

**New guard: `cmd/sqlgen/e2e_resolver_stub_test.go`.** `TestE2EGoldenResolvers_NoUnboundFieldStubs` scans every committed `expected/**/*.resolvers.go` for `not implemented:` panic bodies and for gqlgen's stale-resolver banner. Nothing else in the suite reads resolver bodies, which is precisely how this shipped. **Verified failing-first** against a planted stub. This is the "every emitted field is bound" assertion the auto-review said 25.9 owed; it lands here instead, with the bug that motivated it.

**Pre-existing defect fixed in place.** `context_api.go` carried `maskAPIOperations`'s 14-line doc comment orphaned ~350 lines above the function, stacked directly on top of `buildOneAPITable`'s own comment (the function itself had none). Moved to the function it documents.

**Auto-review (`sqlgen-reviewer`) — 8/10 PRD requirements PASS, 1 PARTIAL, 1 FAIL; 7/7 required tests present; 4 findings, all fixed inline.** The reviewer independently confirmed the load-bearing claims rather than accepting them: the byte-identity measurement (zero removed lines across all 14 changed shared goldens; the `generated_gen.go` removals reduce to one `//go:embed` line per file), that `entityAccessorName` is genuinely the only accessor spelling in the template tree, that every `APIEntity` field the view path reads exists and is populated on `ViewContext`, that the seven claimed GraphQL names equal the seven emitted, and — the security question — that **no tenant escape exists**: no tenanted view is API-enabled today, and `attachTenancyToViews` sets only `vc.Tenancy` / `vc.Imports`, neither of which `apiEntityFromView` or `buildAPIViewContext` reads, so `BuildEntityContextsFromSchema` skipping the view attach pass yields an API context identical to `Generate`'s. It also judged the emission overreach question the same way this record does — landing it here was right, and gating views out of the two examples would have been the suppression pattern the project rejects.

| # | Finding | Disposition |
|---|---|---|
| 1 | **(High) A panicking field resolver landed in the golden tree** — `category_stat_gen.resolvers.go`, the `productNamesJSON` stub. The reviewer noted this record's original "known non-blocking observation" reasoned only about the *comparator* half and that the reasoning does not extend to the object-type field, which genuinely did not agree with the model. | **Fixed** — see the `parser/view.go` section above, plus the new `TestE2EGoldenResolvers_NoUnboundFieldStubs` guard. |
| 2 | **(High) `ValidateAPIViewReadOnly`'s closing assertion was dead code.** It called `funcHasAnyMutation(v)`, which the new `if t.IsView { return false }` guard short-circuits — so the catch-all this record claimed would "catch an operation added to the disjunction later" could never fire. | **Fixed** — `funcHasAnyMutation`'s disjunction is extracted as `mutationSurface(t)`; the guard calls it after the `IsView` check and the lint calls it directly, so the assertion tests the expression emission actually turns on rather than the guard against itself. Pinned by `TestValidateAPIViewReadOnly_catchAllIsLive`, **verified failing-first** by reverting the call to `funcHasAnyMutation`. |
| 3 | **(Medium) §4.13's mutation-key rule was unreachable without a top-level `api:` block** — `validateViewAPIOperations` was the last statement of `validateAPIOperations`, which returns early at `cfg.API == nil`, so `views.x.api.operations.create: true` with no `api:` block validated clean: the silently-ignored knob the rule exists to reject. | **Fixed** — registered independently in `ValidatePreParse` beside `validateViewAPIOptIn`. Pinned by `TestValidateViewAPIOperations_RejectedWithoutTopLevelAPIBlock`. |
| 4 | **(Low)** the generated view walker carried a table-flavored doc comment ("re-enters the related **table's** walker") on an entity §16.4 gives no relationships; the preset + client-only checks were duplicated rather than delegating to `checkAPIOperationsBlock`; `APIEntityFromTable` / `APIEntityFromView` sat on the production export surface where `gen/export_test.go` is the package's established pattern. | **All three fixed** — the walker comment gains an `IsView` branch (columns-only, citing §25.1) while the table branch stays **byte-identical**, so the three view-free GraphQL example trees see zero churn from it; `validateViewAPIOperations` delegates the two shared halves; both constructors moved to `export_test.go`. `BuildTableContextsFromSchema` was left in place — the reviewer is right that it has no production caller, but ~20 test files use it as the narrower entry point. |

The reviewer's two noted test gaps (`increment` / `update_where` rejection in a view mask; no assertion that the graph package is stub-free) are both closed: the client-only rejection test covers the former class and `TestE2EGoldenResolvers_NoUnboundFieldStubs` the latter.

---

## 25.9 View GraphQL emission + gqlgen binding (Ticket C, F3)

**PRD Reference:** Sections 26.4 (view object type + envelopes + inputs per B5), 26.5.1 (query surface), 26.5.2 (field-options walker + §25.1 query-count contract), 26.5.3 (filter/sort translators), 26.5.6 (envelope binding), 16.4. Design §3.3.

**Status:** **Complete** (landed 2026-09-04 — `make check` clean across all 33 modules, `make check-examples` clean across all 13 example modules, `TestE2EGoldenFiles` + `TestE2EGoldenResolvers_NoUnboundFieldStubs` clean. Auto-review 8/8 PRD PASS, 7/7 required tests, 7 findings all fixed inline; two **pre-existing** defects it surfaced are logged as FIX-184 and FIX-185 rather than fixed here. **Ticket C is closed.**)

**Governing decisions:** D8, D9.

**Depends on:** 25.8.

### Tasks

- [x] Emit `<view>_gen.graphqls` per view: object type, `<V>Connection` / `<V>Edge` / `<V>ListResult`, `<V>Filter`, `<V>Sort` + `<V>SortField`, and an `extend type Query` block with `<view>(pk…)` (only when `HasPK`), `<views>(filter, first, after, last, before)` (only when `HasConnection`), and `<view>List(filter, sort, limit, offset)`. **No `extend type Mutation`.** *(Landed in 25.8; closed here as a set — `TestAPIViewSchema_declaresExactlySevenNames`.)*
- [x] Emit the per-view field-options walker — **columns only**, since views have no relationships. A view read is therefore exactly **one** query per §25.1. *(`TestViewWalker_ColumnsOnly` + the E2E count.)*
- [x] Emit the per-view filter translator and sort translator (reusing 25.1's field map, so views get the full family set from 25.3–25.5 for free).
- [x] Emit the per-view envelope type aliases into the consumer's models package (`type <V>Connection = Connection[<V>]` etc., §26.5.6 shape).
- [x] Emit the per-view resolver seeds for the read queries.
- [x] Confirm `cli/graphql.go::buildMergeInput` picks views up automatically once they are in `apiCtx.Tables` (`:265-278`) — emitting `<V>`, `<V>Connection`, `<V>Edge`, `<V>ListResult` bindings — which **retires the hand-written `models:` entry consumers need today**.
- [x] Add a view to the `graphql` example (with tenancy, so 25.7's predicate is exercised through the HTTP seam) and regenerate goldens. *(Two: `workspace_note_summary` — tenanted, `@pk`, enum + jsonb columns — and `category_price_totals`, a materialized view.)*
- [x] Verify 25.8's view arm on `collectGraphQLTypeNames` against real emission — every name this sub-item emits per view must be one the claim set covers, and nothing more. The two lists are the same seven names written in two places; a regression test asserting a view/table and a view/scalar collision are both rejected is what keeps them in step (FIX-175). *(`TestAPIViewSchema_emissionMatchesTheClaimSet` compares the two sets both directions.)*
- [x] Confirm materialized views emit the identical read surface (§16.5 — reads are byte-identical to regular views) and that `Refresh` appears nowhere in the schema. *(`TestAPIViewSchema_materializedIsIdentical` renders the same view both ways and asserts byte-identity.)*

### Acceptance Criteria

- A consumer needs **zero** hand-written GraphQL artifacts for a view: no schema file, no resolver, no `gqlgen.yml` `models:` entry — F3 closed.
- No view emits a mutation field, an `extend type Mutation` block, or a `Create`/`Update` input.
- A view read over GraphQL issues exactly one SQL query regardless of result-set size (§25.1).
- A **tenanted** view queried over HTTP as tenant A returns only tenant A's rows — the end-to-end proof that F1 + F3 together are safe, and the exact scenario that would have been a vulnerability had C shipped before B.
- `<V>Filter` exposes the full comparator family set from 25.3–25.5, including enum and JSONB columns on the view.
- A `@pk`-less view exposes list + connection only; a matview exposes the same read surface as a regular view and no `Refresh`.
- The generated schema passes `gqlgen` without a consumer-authored `models:` entry for the view.

### Tests Required

- [x] Unit tests: per-view schema emission — object type, all three envelopes, filter/sort inputs, the correct Query field set for `HasPK` × `HasConnection` combinations, and **no** Mutation block. *(25.8's `TestAPIViewSchema_emitsReadOnlySurface` + `TestBuildAPIContext_viewGatesByPKAndConnection` for the members; `TestAPIViewSchema_declaresExactlySevenNames` for the closure.)*
- [x] Unit test: the view walker emits a case per API-readable column and no relationship cases.
- [x] Unit test: `buildMergeInput` emits the four view bindings.
- [x] **E2E, the F1+F3 combined pin:** boot the real gqlgen server, query a tenanted view as two different tenants, assert full isolation across the by-PK, connection, and list queries.
- [x] E2E with a counting `Querier`: a view read (including a `<view>List` with `totalCount`) issues the expected query count per §25.1.
- [x] E2E: filtering the view by an enum column and by a JSONB column narrows results (25.4/25.5 reaching views).
- [x] E2E (postgres): a materialized view exposes the read surface and no `Refresh` field; `gqlgen` runs clean with no consumer `models:` entry.

### Completion Record

**Landed 2026-09-04.** `make check` clean across all 33 modules; `make check-examples` clean across all 13 example modules; `TestE2EGoldenFiles` + `TestE2EGoldenResolvers_NoUnboundFieldStubs` clean.

**This sub-item changed no production code.** 25.8's completion record predicted that — the emission landed there because `mysql` and `sqlite` already paired `api:` with views — and the diff confirms it: outside the `graphql` example tree, the only changes are two new test files (`cmd/sqlgen/gen/api_view_emission_test.go`, `cmd/sqlgen/cli/graphql_view_models_test.go`) and one test-only export (`ClaimedGraphQLNamesForTest`). Everything else is the fixture and its goldens. **D8's premise — "put views in `APIContext.Tables` and every downstream template emits for them with no per-view arm" — is now measured on a third dialect surface and a tenanted entity, not just asserted.**

**The fixture: two views on the `graphql` example, chosen so each proves one thing.**

| View | Kind | Why this shape |
|---|---|---|
| `workspace_note_summary` | regular, **tenanted**, `@pk: id`, aggregate `document_count` | The F1+F3 proof. It projects `workspace_id`, so §29.2.5 scopes it by **detection alone** — no `views.<n>.tenancy` block — which is the same path a consumer falls into by accident. `id` satisfies the inherited `cursor_keys` default, so **all three** read queries exist and isolation can be asserted across the whole surface rather than one query of it. The `LEFT JOIN documents` aggregate makes it a real view: `document_count` has no base table behind it. |
| `category_price_totals` | **materialized**, `@pk: category_id`, over the shared `products` table | §26.4's matview sentence. Deliberately **not** tenanted, so "a matview reads like a view" is isolated from "a tenanted view is scoped". It also exercises the §4.13 rule that views have **no primary-key fallback** for `cursor_keys` — the connection query exists only because `sqlgen.yml` names one. |

`workspace_notes` gained two columns (`kind workspace_note_kind_enum`, `labels JSONB`) so the view could project an ENUM and a JSONB column. Without them the only tenanted table in this example was uuid/text/int only, and PRD §26.4's claim that a view's `<V>Filter` carries the same comparator families a table's does would have been untestable for exactly the two families 25.4 and 25.5 added. Both are NOT NULL — this repo's create input requires every NOT NULL column regardless of DEFAULT, so the two `createWorkspaceNote` call sites in `tests/tenant_update_input_test.go` were updated to pass them.

**What the emission actually produced, verified rather than assumed.** `workspace_note_summary_gen.graphqls` and `category_price_total_gen.graphqls` each carry the full PRD §26.4 closed set: object type, three envelopes, `<V>Filter`, `<V>Sort` + `<V>SortField`, and the three Query fields — with `kind: WorkspaceNoteKindEnumComparator` and `labels: JSONBComparator` on the filter, so 25.4's monomorphized enum family and 25.5's PostgreSQL-gated JSONB family both reach a view. Neither view emits an `extend type Mutation` block, a Create/Update input, or a relationship case. `graph/model/models_gen.go` gained only the filter inputs and sort enums — **no `model.WorkspaceNoteSummary` object type** — which is the F3 payoff in the golden: `buildMergeInput` bound the view's row type and all three envelopes with no consumer-authored `models:` entry.

**The security pin, verified failing-first.** `tests/view_tenancy_test.go` boots the real gqlgen server behind the header→ctx tenant bridge and asserts isolation across all three read queries plus the fail-closed no-header branch. It was verified to genuinely detect the F1 hole: with the two `conds = append(conds, sql.Where("workspace_id").Eq(resolvedTenant))` lines in the generated view client neutered, `ByPKIsScoped` fails with *"tenant A read tenant B's view row by PK"*, and both envelope tests fail on a `totalCount` of 3 where 2 was expected. The by-PK case is the sharpest one — the caller already holds the key, so an unscoped read hands over another workspace's row directly — and it exercises the delegation path (`Get` → `GetMany`) where the predicate is actually appended.

**§25.1, measured on a tenanted view.** `TestViewQueryCount` runs against a counting `Querier` **plus** the tenant resolver, over nine rows: by-PK = 1 query, `<view>List` with `totalCount` = 2, connection with `totalCount` = 2. The one is §26.4's "a view read is exactly one query" — no relationships, so nothing fans out. The twos are `Count` + `GetMany`, the same shape a table's envelopes have. Wiring the tenant resolver into the counting handler is deliberate: the predicate is appended to a statement that would have run anyway, so it must cost **zero** extra round-trips, and a shape that resolved the tenant with its own `SELECT` would pass every isolation assertion and fail here.

**The matview, stated as an equality.** `TestAPIViewSchema_materializedIsIdentical` renders the *same* view twice — `Materialized: false` then `true` — and asserts the two schemas are byte-identical, then that neither mentions `refresh`. That works because `Materialized` stops at `ViewContext` and never reaches `APITableContext`; the test is what keeps it there. The E2E half (`TestMaterializedView_ReadSurface`) drives all three read queries against the live matview and includes a *stays stale until refreshed* subtest: a product created after the last refresh does not move `productCount`, and the only remedy is the Go-client-only `Refresh` — the observable consequence of §16.5 that the GraphQL surface deliberately gives no way to fix.

**Files changed:**

| File | Change |
|---|---|
| `cmd/sqlgen/gen/api_view_emission_test.go` | **new** — 5 tests: the seven-name closure, the emission↔claim-set equality, the full family set on `<V>Filter`, the columns-only walker (with a table control in the same render), and the matview byte-identity |
| `cmd/sqlgen/cli/graphql_view_models_test.go` | **new** — `buildMergeInput` emits all four view bindings, no mutation-input binding, table bindings unmoved |
| `cmd/sqlgen/gen/export_test.go` | `ClaimedGraphQLNamesForTest` — the claim set as a name → owning-entity map, so the two lists can be compared |
| `…/examples/graphql/schema.sql` | `workspace_note_kind_enum`; `workspace_notes.kind` + `.labels` |
| `…/examples/graphql/views/` | **new** — `workspace_note_summary.sql`, `category_price_totals.sql` |
| `…/examples/graphql/sqlgen.yml` | `input.views: [./views/]`; `views.category_price_totals.cursor_keys` |
| `…/examples/graphql/tests/main_test.go` | applies the view DDL after the schema; `refreshCategoryPriceTotals` helper |
| `…/examples/graphql/tests/tenant_update_input_test.go` | the two `createWorkspaceNote` call sites pass the two new NOT NULL columns |
| `…/examples/graphql/tests/view_tenancy_test.go` | **new** — the F1+F3 combined pin, 4 tests |
| `…/examples/graphql/tests/view_read_surface_test.go` | **new** — query count, enum + JSONB filters, matview read surface + staleness, and two introspection tests over the **served** schema (the closed read set; no mutation field and no `refresh` anywhere) |
| goldens | `views_gen.go`, two `*_gen.graphqls`, two `*_gen.resolvers.go`, two manifest entities, plus the additive churn to `models_gen.go` / `enums_gen.go` / `client_gen.go` / the four translators / the walker / the resolvers / `generated_gen.go`, in both `models/` and `expected/` |

**Two assertions are deliberately made through introspection rather than against the golden text.** `TestViewSchema_EmitsTheClosedReadSet` and `TestViewSchema_NoMutationsAndNoRefresh` query `__schema` on the running handler. A golden assertion says what sqlgen wrote; an introspection assertion says what a consumer can *call* — and the "never" clauses (no mutation addresses a view, no `refresh` field reaches any type or input) are claims about the published API, so they are checked where the API is published.

**Auto-review (`sqlgen-reviewer`) — 8/8 PRD requirements PASS, 7/7 required tests present, 7 findings, all fixed inline.** The reviewer independently confirmed the security question rather than accepting it: `GetMany` and `Count` are the **only** statement-issuing methods on the view client and both append the tenant predicate, `Get` / `Paginate` / `Connection` delegate to them, no `Stream` / `Exists` is generated for a view, and — the part this record had not checked — **`SkipTenancy` is not reachable over HTTP at all**, because `WithCallOptionsMiddleware` can only set `SkipCache` / `SkipEvents` / `SkipHooks` (`middleware_gen.go:21-25`). It also verified `models/` and `expected/` are byte-identical trees, that `graph/model/models_gen.go` declares no view object type, and that `isCachedView: false` is correct (view caching is opt-in per view).

| # | Finding | Disposition |
|---|---|---|
| 1 | **The `schema.sql` comment contradicted the schema it describes** — it claimed `kind` / `labels` "stay out of the required-on-create set" because they carry DEFAULTs. They do not: the emitted input is `kind: WorkspaceNoteKindEnum!` / `labels: JSON!`, which is why two call sites had to change in the same commit. | **Fixed** — the comment now states the actual rule (every NOT NULL non-generated column is required on `Create<T>Input`, DEFAULT or not, same as `pinned_order` and `created_at`) and names the call sites that pass them. |
| 2 | **"three `createWorkspaceNote` call sites" is two.** | **Fixed** in both trackers. |
| 3 | **The fail-closed branch skipped the by-PK query** — `TestViewTenancy_MissingHeaderIsUnauthenticated` covered list and connection but not `workspaceNoteSummary(id:)`, the query this same file calls the sharpest form of the F1 hole 170 lines earlier. | **Fixed** — the subtest table gains the by-PK case with its variables. |
| 4 | **Two vacuity risks.** The mutation loop in `TestViewSchema_NoMutationsAndNoRefresh` never asserted `MutationType.Fields` was non-empty, so it would pass silently if the whole mutation surface vanished; and `TestViewSchema_EmitsTheClosedReadSet` was named for a closure it does not perform. | **Both fixed** — a `t.Fatal` guard on the empty mutation set, and the test renamed to `TestViewSchema_ServesTheReadSet` with a comment pointing at `TestAPIViewSchema_declaresExactlySevenNames` as where closure actually lives (introspection sees every type in the project at once and cannot close a per-view set). |
| 5 | **`@pk: category_id` asserted a unique key the applied DDL never created.** Per §16.5.2 that annotation is what makes `RefreshConcurrently` generated, and the statement needs a real UNIQUE index — so the fixture advertised a precondition it had not established, and the generated method would have failed on first use. Nothing called it, so nothing was red. | **Fixed** — `TestMain` creates `category_price_totals_category_id_idx` after the view DDL (the `postgres` example does the same for `order_totals`), **and a new subtest actually calls `RefreshConcurrently` through the Go client and asserts the count moves.** That is also the missing half of "Refresh is Go-client-only": the schema tests pin its absence from the API, and without this nothing distinguished "Go-client-only" from "not generated". It passes against the real container. |
| 6 | The view's `LEFT JOIN documents … AND d.entity_type = 'spv'` reused the asset-polymorphism table with an unexplained enum member. | **Fixed** — the view header now says why: `documents.entity_id` deliberately carries no FK so several parents can share it, and `spv` is the one member of `document_entity_type_enum` that is not asset-scoped, so notes claim it without disturbing the §13.7 fixture the `asset.*` members serve. |
| 7 | **Stale example docs** — the README's inventory omitted views entirely, and both `sqlgen.yml` and `schema.sql` still claimed `workspace_settings` was the only tenanted entity / the only `workspace_id` carrier (already stale from `workspace_notes`, now stale twice). | **All three fixed** — the README gains a views section describing both kinds and what each pins; the two comments now name all three tenanted entities and cite §29.2.5 alongside §29.2.3. |

**Two pre-existing defects the reviewer surfaced are not fixed here; both are now logged (`tracked`) rather than carried in prose.** (a) **FIX-184** — Every manifest entity — table and view, in this example and in `tenancy_postgres` — declares `"files": ["<entity>_gen.go"]`, but the real output is `models_gen.go` / `views_gen.go`; this is the same "name things that exist" family as FIX-180 / FIX-182 and wants their treatment (an AST-parsed drift guard), not an inline edit. Investigating it for the FIX entry sharpened the diagnosis: the claim is **layout-dependent**, not simply wrong — `sqlite` sets `output.layout: file_per_table` and its `["article_gen.go"]` is correct, which is precisely why no golden ever disagreed. The builder hardcodes the per-table spelling and never reads `cfg.Output.Layout`; it does derive a `layout` local, but from `cfg.Generation.Manifest.JSONLayout` — an unrelated knob that shares the word. (b) **FIX-185** — A connection query that omits a cursor-key column from its selection (`{ edges { node { body } } }`) produces FieldOptions without `id`, so the cursor encoder writes a zero value and `after:` silently restarts from the beginning — **identical on tables**, so it is neither a view regression nor a tenancy leak, but it is a real silent-wrong-results bug with a blast radius well beyond this sub-item. Probed against the live container rather than reasoned about, and it is **worse than "restarts from the beginning"**: the keyset predicate becomes `id > '00000000-…'`, every row satisfies it, and the next `endCursor` is zero again — so a client paginating on `endCursor` loops on page 1 forever. Reproduced on the `users` **table** with the same result, confirming it is not view-specific. Also noted: `main_test.go` originally hardcoded the two view filenames, so a third view would have been parsed by sqlgen and never created in the container — **fixed inline** by globbing `../views/*.sql` with a non-empty assertion.

**One scope note.** The task list's "no consumer-authored `models:` entry" is asserted three ways and none of them is a literal read of `gqlgen.yml`: the unit test pins what `buildMergeInput` emits, the golden pins that `graph/model/models_gen.go` declares no view object type (if the binding were missing, gqlgen would generate one), and the example's own `gqlgen.yml` — which is consumer-owned and never modified on disk — is unchanged in this diff while both views compile and serve. The example builds and its tests pass, which is the end-to-end form of the same claim.

---

> **DECISION — SETTLED 2026-08-25 (D11). Recorded here; normative text landed in PRD §11.5 + Appendix A (B8).** The 25.0
> auto-review surfaced two coupled problems with the Ticket-D prerequisite as originally specified.
>
> **(rev-F4) The `EXISTS` correlation qualifier is path-dependent, so "qualify at codegen" is
> wrong.** `BuildSelect` emits `FROM "tasks"` with no alias (`sql/builder.go:90-100`); `BuildSelectJoin`
> emits `FROM "tasks" t` (`:116-124`) and the O2O read path passes `Alias: "<VarName>"`
> (`templates/table/get.go.tmpl:235`, `:275`). The correlation must render as `"tasks"."id"` on the
> first path and `t."id"` on the second — one baked-in qualifier is invalid SQL on whichever path it
> did not target, on PostgreSQL and MySQL both. Surviving `PrefixConditions` is **necessary but not
> sufficient**, which is what 25.11's original invariant 2 got wrong. §11.1 now states the problem
> rather than asserting the broken fix.
>
> **(rev-F6) 25.10 has no normative PRD text.** B1–B7 never covered the `sql/` affordance — the
> delta list was incomplete, so 25.0 could not have landed it. A **B8** (Appendix A and/or §11.5) is
> required before 25.10 starts.
>
> **Three candidate shapes**, to be settled before 25.10:
> 1. **Structured condition** — `sql.Exists(correlationColumn, subquery)`; the builder renders
>    `<qualifier>."col"` at build time, qualifier = the alias when it aliased, the quoted table name
>    otherwise. Correct on both paths; needs the qualifier threaded into condition rendering.
> 2. **Substitute-token** — the clause carries a `{{parent}}` marker that the builder replaces.
>    Smallest delta from the original exempt-clause idea, but keeps clauses stringly-typed.
> 3. **Always alias** — give `BuildSelect` an alias too, making the qualifier a codegen constant.
>    Cleanest conceptually, but rewrites the SQL text of every non-join `SELECT` in the project:
>    large intended golden churn across all 13 examples, in the most load-bearing builder there is.
>
> **Chosen: (1) structured `sql.Exists(correlationColumn, subquery)`** — the only shape that is both
> correct on both paths and not stringly-typed, with a blast radius confined to `sql/` plus the new
> emission. Two properties decided it: the qualifier is resolved by the builder (so codegen never
> has to know which read path will run), and exemption from alias prefixing is **by construction**
> — a structured condition value is not a leaf clause, exactly like the existing `And` / `Or` /
> `Subquery` / `Range` values that `expandCondition` (`sql/builder.go:462`) already dispatches on —
> rather than a `Qualified bool` opt-out someone can forget to set.
>
> **B8 landed 2026-08-25:** PRD §11.5 gains `Exists` in the raw-types list plus a **Correlated
> EXISTS and qualifier resolution** subsection (the two-builder qualifier table, the four rules —
> builder supplies the qualifier, prefixing never rewrites it, placeholder numbering unchanged,
> dialect deltas are quoting/placeholders only); Appendix A gains an **Alias-qualified columns**
> note pointing at it; §11.1 invariant 2 now states the rule instead of the problem. FIX-135 is
> resolved. **Ticket D is unblocked.**

## 25.10 `sql` prefix-exempt condition (Ticket D, F8)

**PRD Reference:** Appendix A (SQL builder functions), Section 11.5 (condition composition), 13.2 (relationship loading / O2O join path). Design §3.4, §2-D10.

**Status:** **Complete** (landed 2026-09-04 — `make check` clean across all 33 modules, `make check-examples` clean across all 13 example modules)

**Governing decisions:** D10 (the `EXISTS` filter shape), **D11** (the qualifier is builder-resolved, not codegen-baked).

**Depends on:** 25.0.

### Tasks

- [x] Add the structured `Exists{CorrelationColumn, Subquery}` condition value + `sql.Exists(col, subquery)` constructor (D11, PRD §11.5). It carries the correlation **column only** — never a qualifier.
- [x] Resolve the qualifier at render time in `expandCondition` (`sql/builder.go:462`, the same dispatch point `And` / `Or` / `Subquery` / `Range` already use): `SelectOptions.Alias` when the statement aliased its table, `Dialect.FormatTable(table)` otherwise. Thread the resolved qualifier through `buildWhere` (3 call sites — `sql/builder.go:231`, `:288`, `:560`); no public builder signature changes.
- [x] Confirm `prefixCondition` (`sql/condition.go:154-167`) leaves the new condition untouched **by construction** (it is not a leaf string clause), including inside `And` / `Or` recursion — assert it rather than special-casing it.
- [x] Verify placeholder numbering stays driven entirely by `Conditions` order — the contract `BuildSelectJoin` documents at `sql/builder.go:107-118` and that the child-tenant filter already relies on.
- [x] Extend the contract comment at `sql/condition.go:143-153` to name `Exists` as the supported mechanism, so the "must be qualified at codegen time" note points at the affordance instead of only warning about the hazard — and correct it: a *correlated* reference must NOT be qualified at codegen (PRD §11.5).
- [x] Confirm `expandSubquery` (`sql/builder.go:508`) composes correctly with the new condition kind — args counted, placeholders renumbered.
- [x] No new dependencies — `sql/` stays stdlib-only.

### Acceptance Criteria

- An `Exists` condition passed through `PrefixConditions` emits unchanged (no `alias.` prefix) while sibling leaf conditions in the same slice are still prefixed.
- The same `Exists` value renders `"tasks"."id"` under `BuildSelect` and `t."id"` under `BuildSelectJoin` with `Alias: "t"` — one emitted value, both paths correct (D11).
- The same holds when the `EXISTS` condition is nested inside `sql.And` / `sql.Or`.
- Placeholder numbering across a mixed `Conditions` slice (prefixed leaves + an `EXISTS` carrying its own args + a `Range` + an `IN`) is correct and stable for all three dialects.
- `sql/` acquires no new imports outside stdlib.
- Existing `BuildSelect` / `BuildSelectJoin` output is byte-identical for every condition shape that does not use the new affordance.

### Tests Required

- [x] Unit test: `PrefixConditions` leaves an exempt condition alone and prefixes its siblings.
- [x] Unit test: same, with the exempt condition nested two levels deep in `And`/`Or`.
- [x] Table-driven placeholder-numbering test across all three dialects for a mixed condition slice including an `EXISTS` with args.
- [x] Unit test: `BuildSelectJoin` with JOINs + an exempt `EXISTS` + a prefixed leaf produces correct SQL and arg order.
- [x] Regression: every existing `sql` builder test passes unchanged.

### Completion Record

**Landed 2026-09-04.** `make check` clean across all 33 modules under `-race`; `make check-examples` clean across all 13 example modules. Four files changed, all in `sql/` — **no generator, template, or golden churn**, because nothing emits the new condition yet (25.11 does).

**The shape, and why it is two fields.** `Exists(correlationColumn string, subquery Subquery) Condition` returns a `Condition` whose `Value` is an unexported `existsCondition{CorrelationColumn, Subquery}` and whose `Clause` is **empty** — exactly the `And` / `Or` construction, where the exported name is the constructor and the structured value is unexported. The empty `Clause` is what makes the D11 exemption structural: `prefixCondition` returns early on `Clause == ""`, before either qualification path, and the `And`/`Or` recursion reaches the same early return. There is no exemption flag to forget and no case in `prefixCondition` naming `Exists` — the tests assert the property rather than the branch.

**How the qualifier reaches the condition.** `qualifier string` is threaded through `appendWhere` → `buildWhere` → `expandCondition` → `expandComposition`, so it survives arbitrary `And`/`Or` nesting. Every builder resolves it from what its own `FROM` clause actually says: `BuildSelectJoin` uses `SelectOptions.Alias` (falling back to `FormatTable` when unaliased), and `BuildSelect`, `BuildCount`, `BuildExists`, `BuildUpdate`, `BuildIncrement`, `BuildSoftDelete`, and `BuildHardDelete` use `FormatTable(t)`. **The last five are not incidental**: a relationship filter reaches the builders through `filter.ToConditions(dialect)`, which is also what `Count`, `ExistsWhere`, `SoftDeleteWhere`, and `HardDeleteWhere` pass, so an `Exists` had to render correctly on every filter-accepting builder rather than only on the two read paths §11.5 names. All builder signatures are unchanged; the threading is internal.

**`CorrelationToken`.** The subquery's SQL is a string, so the parent-side reference needs a substitution point: `sql.CorrelationToken` (`{{parent}}`), which `expandExists` replaces with `<qualifier>.<QuoteIdentifier(CorrelationColumn)>`. This is *not* the `{{parent}}` alternative D11 rejected — that one kept the whole condition a leaf string clause, so a forgotten substitution reached the database. Here the token lives inside a structured value the builder always resolves, and codegen will concatenate the exported constant rather than spelling it. The one contract the constructor cannot enforce is documented on `Exists`: a subquery that never references the token is an *uncorrelated* EXISTS, which matches on any row the subquery returns. 25.11's goldens are where that is enforced.

**Two ordering decisions inside `expandExists`.** Placeholders are renumbered **before** the token is substituted, so a qualifier containing a `$` can never be mistaken for a placeholder. And unlike a bare `Subquery` value — whose SQL is spliced in verbatim, leaving its inner numbering to the caller — an `Exists` subquery carries bare `$` tokens that are renumbered into the enclosing statement's sequence. That is what PRD §11.5 rule 3 requires and what §11.1's example (`"status" = $1 AND EXISTS (… $2 … $3)`) shows: the generator cannot know it starts at 2. `TestExists_ComposesWithSubqueryCondition` pins the two kinds side by side.

**Files changed:**

| File | Change |
|---|---|
| `sql/condition.go` | `existsCondition` value, `CorrelationToken`, `Exists` constructor; `prefixCondition`'s contract comment extended to name `Exists` as the affordance and to correct the note — a *correlated* reference must **not** be qualified at codegen |
| `sql/builder.go` | `expandExists`; `qualifier` threaded through `appendWhere` / `buildWhere` / `expandCondition` / `expandComposition`; per-builder qualifier resolution (8 call sites) |
| `sql/condition_test.go` | `TestPrefixConditions_LeavesExistsUntouched`, `TestPrefixConditions_LeavesNestedExistsUntouched` (two levels deep, inside `And`→`Or`) |
| `sql/builder_test.go` | `TestExists_QualifierResolvesPerReadPath` (the D11 proof — **one** value, both paths, ×3 dialects), `TestExists_PlaceholderNumberingInMixedConditions` (leaf + EXISTS + `Range` + `IN`, ×3 dialects), `TestBuildSelectJoin_ExistsWithPrefixedLeaf` (JOIN + prefixed leaves around an exempt EXISTS, ×2 dialects), `TestExists_NestedInComposition`, `TestExists_ComposesWithSubqueryCondition` |

**The D11 proof is written as an equality, not two assertions.** `TestExists_QualifierResolvesPerReadPath` builds **one** `Exists` value per dialect and passes that same value to `BuildSelect` and to `PrefixConditions` + `BuildSelectJoin`, asserting it renders `"public"."users"."id"` on the first and `t."id"` on the second. A test that built the value twice would pass even if the qualifier were baked in at construction, which is the exact failure D11 exists to prevent.

**Verified failing-first.** Collapsing `BuildSelectJoin`'s qualifier to `FormatTable(t)` — the pre-D11 behavior — fails the new tests in 17 places; the change was reverted and the suite re-run clean. Every pre-existing `sql` test passes untouched, which is the byte-identical-output acceptance criterion: no condition shape that does not use the new affordance changed. `sql/` still imports only `fmt`, `sort`, and `strings`.

**Security note (no `/security-review` trigger).** The changed paths are not in either trigger table — `sql/` is an ordinary module dependency, not a `//go:embed` target, and no CLI file-reading surface is touched. The one new identifier-into-SQL path is `qualifier + "." + QuoteIdentifier(CorrelationColumn)`, where the qualifier is builder-derived (`FormatTable` or a generator-authored alias, never caller input) and the column is quoted by the dialect. All values still travel as args.

**Auto-review (`sqlgen-reviewer`) — 11/11 PRD + acceptance criteria PASS, 5/5 required tests present, 0 blocking findings, 5 nits.** It independently confirmed the two things worth confirming: that the `CorrelationToken` mechanism is *not* the shape D11 rejected (the token's only render path is `expandExists`, which always substitutes, where the rejected leaf-clause form could reach the database unsubstituted), and that the signature changes are fully contained — every `buildWhere` / `appendWhere` / `expandCondition` / `expandComposition` call site is inside `sql/builder.go`, so no generated code or template needed updating. It also read the templates to check the `BuildSelectJoin` `Alias == ""` fallback: dead in production (`get.go.tmpl:269,279` and `relationships.go.tmpl:5` always set the alias) but the right defensive choice, since without it an unaliased join path would emit `."col"`. One nit was fixed inline — `d.FormatTable(t)` was evaluated twice in six builders; each now hoists it the way `BuildSelect` does. The remaining four are pre-existing or forward-looking and are recorded below rather than logged as FIXes.

**Three carry-forwards, none blocking.**

1. **MySQL error 1093, for 25.11's emission constraints.** `BuildUpdate` / `BuildIncrement` / `BuildSoftDelete` / `BuildHardDelete` now accept an `Exists`, and MySQL rejects `UPDATE t … WHERE EXISTS (SELECT … FROM t …)`. It only bites when a relationship filter's target *is* the mutated table. Not a 25.10 defect — the builder renders what it is handed — but 25.11 is where the constraint belongs.
2. **The token has one unguarded escape.** Passing the same `Subquery` as a plain `Subquery` condition value instead of through `Exists` splices it verbatim (`expandSubquery`), token and all. Undetectable at runtime; 25.11's goldens are the guard.
3. **`replaceDollars` returns all args even when it runs out of `$` tokens**, so a subquery with more `Args` than tokens emits fewer placeholders than args and fails at the driver. Pre-existing behavior inherited from every other multi-arg condition — noted only because `Exists` is its first caller whose SQL is generator-composed rather than comparator-built.

**What 25.11 inherits.** The runtime prerequisite is done: codegen emits the correlation column plus a subquery referencing `sql.CorrelationToken`, and never a qualifier. Still open in Ticket D and explicitly *not* done here — the target's soft-delete and tenant predicates inside the subquery (PRD §11.1 invariant 1) are the **generator's** job; the builder renders the subquery it is handed.

---

## 25.11 Model-side relationship filter fields → `EXISTS` (Ticket D, D10)

**PRD Reference:** Sections 11.1 (filter struct per B6), 11.5 (condition composition), 13.1 / 13.2 (relationship types + join metadata), 17.3 (soft-delete composition), 29.4.1 / 29.10 (tenant predicate on the related entity). Design §3.4, §2-D10.

**Status:** **Complete** (landed 2026-09-04 — `make check` clean across all 33 modules, `make check-examples` clean across all 12 example modules)

**Governing decisions:** D10 (junction/FK-backed filter fields compiling to `EXISTS`).

**Depends on:** 25.10 (D11's `sql.Exists`).

### Tasks

- [x] Add one `<Target>Filter` member per list relationship to the generated `<T>Filter` (`templates/shared/_filter.tmpl`). **Tagging:** the filter struct emits `json:` only — no `db:` tags (`_filter.tmpl:7`). The `db:"-"` convention applies to relationship fields on the **model** struct, not here; do not carry it over.
- [x] Compile each non-nil relationship filter in `ToConditions` into a correlated `EXISTS` subquery, built from the join metadata already on `RelationshipContext` (`context.go:222-282`): `FKColumn` for O2M; `JunctionTable` + `JunctionSchema` + `JunctionLocalFK` + `JunctionReferenceFK` for M2M. **To-one relationships (O2O / belongs-to) contribute no filter member** — PRD §11.1 scopes v1 emission to list relationships.
- [x] Emit the parent-side correlation reference through 25.10's `sql.Exists(correlationColumn, subquery)` — **unqualified at codegen**; the builder resolves the qualifier per read path (D11, PRD §11.5). Do **not** bake in an alias.
- [x] Delegate the subquery's inner `WHERE` to the target's own `ToConditions`, so nesting and `and` / `or` compose recursively.
- [x] Inject the target's **soft-delete** predicate into the subquery on the same rules the target's own read path uses (§17.3).
- [x] Inject the target's **tenant** predicate into the subquery when the target is tenanted (§29.10 parity) — a relationship filter must not become a cross-tenant probe.
- [x] Quote every identifier through `Dialect.QuoteIdentifier` / `FormatTable`; parameterize every value. No interpolation.
- [x] Scope discipline: one hop per level, arbitrary depth by nesting; **no** aggregate predicates (`count > 3`) in this phase.
- [x] Confirm the M2M shape is a single `EXISTS` over the junction joined to the target (not two queries) — this is a WHERE-clause predicate, distinct from the two-query M2M *loader* in §25.1.

### Acceptance Criteria

- `<T>Filter` carries one member per list relationship; a nil member contributes no SQL (the "nil filter ⇒ no where clause" contract is preserved).
- O2M relationship filters compile to a correlated `EXISTS` over the target table on the FK; a to-one relationship emits no member at all (PRD §11.1).
- M2M relationship filters compile to a single correlated `EXISTS` over the junction joined to the target.
- The target's soft-delete predicate is inside the subquery — a relationship filter never matches through a soft-deleted related row.
- The target's tenant predicate is inside the subquery when the target is tenanted — a relationship filter cannot probe another tenant's rows.
- Relationship filters compose with column filters and with `and` / `or` at any nesting depth, and correctly on the O2O-join read path (25.10's affordance holds).
- Identical emitted SQL structure across all three dialects, differing only in quoting and placeholder style.
- Projects with no relationship filter set regenerate byte-identically apart from the new (unset) struct fields.

### Tests Required

- [x] Unit tests: `ToConditions` SQL shape for O2M and M2M relationship filters, per dialect; plus a test that a table with only a to-one relationship gains no filter member.
- [x] Unit test: a nil relationship member emits no condition.
- [x] Unit test: the target's soft-delete and tenant predicates appear **inside** the subquery, not outside.
- [x] Unit test: a relationship filter nested inside `and` / `or`, and a relationship filter whose target filter itself contains a relationship filter (two hops).
- [x] Unit test: placeholder numbering with a relationship filter plus column filters plus a keyset condition.
- [x] Unit test: the O2O-join path (`BuildSelectJoin` + `PrefixConditions`) leaves the `EXISTS` intact.
- [x] E2E (postgres + mysql + sqlite): filter a parent list by an M2M-related entity and by an O2M-related entity; assert exact row sets.
- [x] E2E: a soft-deleted related row does not satisfy the relationship filter; a related row in another tenant does not satisfy it.
- [x] *(added after auto-review)* Render test: an untenanted parent whose list target is tenanted emits a client with a concrete `tenancy.TenantResolver[T]` — the shape no example schema reaches.

### Completion Record

**Landed 2026-09-04.** `make check` clean across all 33 modules under `-race`; `make check-examples` clean across all 12 example modules. 25.10 shipped the runtime affordance and nothing emitted it; 25.11 is where it gets emitted, so this is the sub-item that carries the golden churn — 8 of the 12 examples. The other four (`cache`, `events`, `tenancy_mysql`, `tenancy_postgres`) regenerate **byte-identically**: none has a list relationship, so none gains a member, a signature change, or a line of plumbing. That is the byte-identity acceptance criterion, observed rather than asserted.

**One decision was escalated before any code was written**, because the PRD does not resolve it and guessing would have meant re-cutting every golden. PRD §11.1 requires the target's tenant predicate inside the subquery and its worked example shows it as a **bound parameter** (`AND tgt."workspace_id" = $3`), not a correlated column — but the same section pins the compiler as `ToConditions(dialect sql.Dialect)`, a pure method with no ctx, no client, and no tenant. Today the tenant is resolved in the *client method* (`get.go.tmpl:194`), **after** `ToConditions` has already run. Soft delete needed nothing new (`excludeDeleted` is a codegen constant and the "caller set the comparator" check is a nil-check on the target filter); the tenant value genuinely had nowhere to come from. Three shapes were put to the user — a variadic `FilterOption`, a required `FilterScope` parameter, and correlating to the parent's own tenant column. **The user chose the variadic option**, which keeps the PRD's shown call form valid (`ToConditions(dialect)` still compiles) and makes the change additive rather than a signature break. PRD §11.1 gained a `FilterOption` subsection recording it.

**The variadic parameter is emitted only when the package has at least one relationship filter member.** That rule is what keeps `cache`, `events`, `tenancy_mysql`, and `tenancy_postgres` byte-identical. When it *is* emitted, every filter in the package carries it — including views, which have no relationships — so recursion through `and`/`or`, recursion into a member, and every generated call site share one signature. Two things ride in the option set:

1. **The resolved tenant**, via the exported `WithFilterTenant(tenant T)`. Absent, the subquery is unscoped — which is not a hole but the required behaviour, because that is exactly what `CallOptions.SkipTenancy` needs. Documented on the constructor.
2. **The subquery nesting depth**, which names the target alias (`tgt0`, `jn0`, `tgt1`, …). This is the non-obvious half. A self-referential relationship filter nested inside itself (§13.5) with a fixed alias would let the inner subquery shadow the reference its own correlation resolves against — `tgt."parent_id" = tgt."id"` compares the related row to *itself* and returns wrong rows silently, with no error anywhere. Depth-keyed aliases make that unrepresentable. `nestFilterScope` copies the option slice rather than appending in place, so two members compiled from the same filter cannot write through each other's backing array.

**`sql.SubqueryWhere` — the one runtime addition, and why it was needed.** 25.10 left the subquery for codegen to compose, but composing it means rendering the target's `[]sql.Condition` into a SQL fragment, and `buildWhere` is unexported. Reimplementing condition expansion in generated code would have duplicated `sql/` badly. `SubqueryWhere(d, alias, conds) (string, []any)` does it in ~6 lines by wrapping the dialect: `bareDialect` overrides only `Placeholder` / `PlaceholderList` to emit the bare `$` token, so the existing expansion path renders a fragment `Exists` then renumbers into the enclosing statement's sequence. Two properties fall out of that one wrapper:

- **Every column is alias-qualified** (via `PrefixConditions`), because an unqualified name inside a correlated subquery binds to the *enclosing* statement whenever the target lacks the column — turning a filter on the related row into a filter on the parent row, silently.
- **A nested `Exists` among the conditions resolves against `alias`**, because `SubqueryWhere` passes its own alias down as the qualifier. That is what makes two hops correlate to their own level rather than to the outermost statement, and it is the same threading 25.10 built for the top level, reused one frame down.

**Where the tenant is resolved, and why it is a transitive closure.** Each client that needs one gained a `filterOptions(ctx, skipTenancy, explicit)` helper, called at all eight filter-accepting sites — `GetMany`, `Count`, `ExistsWhere`, `Stream`, `UpdateWhere`, `SoftDeleteWhere`, `RestoreWhere`, `HardDeleteWhere`. `Paginate` and `Connection` delegate to `Count` + `GetMany`, and `Increment` takes no filter, so nothing was missed. The gate is `HasTenantedRelationshipFilter`, computed as a **fixpoint over the relationship-filter graph** rather than a direct lookup: a table whose own targets are all untenanted still has to resolve a tenant if one of those targets reaches a tenanted table through *its* members, because the option set is threaded straight down. `TenantedEntities` in the unified client was widened the same way — the pattern `HasTenantedO2OChild` already established for §29.10.

**Shapes that emit nothing, deliberately.** `sql.Exists` carries exactly one correlation column, so: a parent without a single-column PK contributes no members (nothing single to correlate on); an M2M whose target has no single-column PK contributes none for that edge (nothing to join the junction to); a target outside the generated set contributes none (no filter type to name). A relationship whose Go field name collides with a column comparator is skipped — the column wins, matching the model struct's existing behaviour. All four drop the member rather than emit something half-formed, and all four are recorded in PRD §11.1.

**The static `filter:` predicate is injected too, and qualified.** A sub-categorized relationship (§13.7.1) whose config `filter:` were dropped would match rows of a *sibling* sub-category. Unlike the O2M/M2M *loader* — which runs a standalone query over one table, where a bare column can only mean that table — the subquery has two other relations in scope: the junction (for M2M) and the enclosing statement. A bare name there is either ambiguous or silently binds to the parent row, which is exactly the hazard FIX-098 fixed for the O2O JOIN-ON path. The predicate therefore goes through `qualifyFilter` at codegen, against a sentinel alias token, and the template splices the depth-keyed alias back in as a Go concatenation — the rewrite stays entirely at generation time, and the generated code does no string surgery. The `graphql` example shows both columns bound: `subSQL += " AND (" + tgt + ".entity_type = 'asset.attachment' AND " + tgt + ".name LIKE 'link_%'" + ")"`. A `filter:` containing `$` is a **codegen error**: `Exists` renumbers the subquery, so a stray `$` would be consumed as a placeholder and shift every caller-supplied arg by one — silently. The loader path is immune (`sql.Raw` carries no args), so the guard belongs here.

**Fixture change.** `articles` in the `tenancy` example gained a nullable `user_id` FK to `users`. It was the one shape the suite could not otherwise reach: an o2m target that is **both** tenanted and soft-deleted, which is what proves the two injections compose in one subquery. Nullable so every pre-existing article fixture still creates cleanly; `resetDB` moved `articles` ahead of `users` in delete order.

**Auto-review (`sqlgen-reviewer`) — 9/10 PRD requirements PASS, 8/9 required tests present, 1 blocking finding, 1 tracked, 2 nits.** It independently confirmed the four things worth confirming: the eight threaded call sites are exhaustive (the only other `ToConditions` sites are `view/get.go.tmpl` and `view/count.go.tmpl`, correctly unthreaded because views carry no members); the bare-`$` contract holds for scalar, `Range`, `IN`/`NOT IN`, empty `IN`, and the pgx `= ANY($)` form, with arg order matching `$` order in every branch; `prefixCondition` passes the nested `existsCondition` through untouched so the two-hop token resolves to `tgt0` rather than the outer table; and no golden diff on a non-participating table is anything but the signature line. **All four findings were addressed inline** rather than logged as FIXes:

1. **Blocking — a shared parent with a tenanted list target emitted non-compiling code.** `HasTenantedRelationshipFilter` can be true on a table that carries no tenant column of its own (a shared lookup table with an O2M to a tenanted one), and `attachTenancyToTables` leaves such a table's `Tenancy.GoType` empty — so the client rendered `tenancy.TenantResolver[]` and omitted the tenancy import. `backfillRelationshipFilterTenancy` now mirrors the identical backfill `annotateO2OChildTenancy` already performs for `HasTenantedO2OChild`, drawing the type from any tenanted table (§29.2.4 makes it uniform). The reviewer's sharpest observation was that the **gen fixture already produced the shape** — `TestRelationshipFilters_TenantedFlagIsTransitive` asserts `categories.HasTenantedRelationshipFilter == true` while `categories` has no tenant column — but nothing rendered the *client* template for it, and no example schema has that pairing, so both suites stayed green over broken output. `TestRelationshipFilters_SharedParentRendersTenantedClient` closes that by rendering `table/client` for exactly that context; **verified failing-first** — with the backfill disabled it reports an empty tenant Go type.
2. **Tracked — the static `filter:` was spliced unqualified.** Fixed as described above.
3. **Nit — a `$` in a static filter would shift args.** Now a codegen error.
4. **Nit — `ToConditions` was not in the reserved-name set.** A relationship named `ToConditions` would have emitted a field shadowing the method; it is now reserved alongside `And` / `Or` / `PKs`.

**Files changed:**

| File | Change |
|---|---|
| `sql/builder.go` | `SubqueryWhere` + `bareDialect` (the bare-`$` placeholder wrapper) |
| `sql/builder_test.go` | Five `SubqueryWhere` tests: cross-dialect renumbering into the enclosing statement, alias qualification through `and`/`or`, the two-hop correlation proof, empty conditions, and the multi-placeholder shapes (`IN` / `NOT IN` / empty `IN`) |
| `cmd/sqlgen/gen/context.go` | `RelationshipFilterContext`; `RelationshipFilters` / `HasTenantedRelationshipFilter` / `HasFilterOptions` on `TableContext` and (inert) on `ViewContext`; `SharedTypesContext.Decls` |
| `cmd/sqlgen/gen/context_relfilter.go` | **New.** The cross-table wiring pass: entry construction, target-side soft-delete / tenant resolution, the tenanted-reachability fixpoint, the shared-parent tenancy backfill, and codegen qualification of the static `filter:` |
| `cmd/sqlgen/gen/context_shared.go` | `filterOptionDecls` — `FilterOption`, `filterScope`, `resolveFilterScope`, `nestFilterScope`, `filterSubqueryAlias`, and `WithFilterTenant` when tenanted |
| `cmd/sqlgen/gen/context_client.go` | `TenantedEntities` widened to include tables reaching a tenanted relationship target |
| `cmd/sqlgen/gen/orchestrate.go` | `wireRelationshipFilters` wired into both context entry points (`Generate` and the `graphql gen` wrapper path); `hasFilterOptions` threaded to the shared-types builder |
| `cmd/sqlgen/gen/funcmap.go` | `softDeleteSubqueryPredicate` / `softDeleteSubqueryArg` — the operator fragment and its bound value, since the subquery is assembled as text rather than as `Conditions`. The depth-keyed alias splice was `relationshipStaticFilterExpr` here; FIX-212 moved it to `relationshipStaticFilter` in `context_relfilter.go` and the `StaticFilterExpr` context field, because deriving the dialect's alias spelling needs the dialect and an error channel that a funcmap function may not have |
| `cmd/sqlgen/gen/templates/shared/_filter.tmpl` | Relationship members on the struct; the variadic signature; one `shared/filter-exists` helper per member |
| `cmd/sqlgen/gen/templates/shared_types.go.tmpl` | Renders `Decls` |
| `cmd/sqlgen/gen/templates/table/client.go.tmpl` | `filterOptions` helper; resolver-field and `resolveTenant` guards widened |
| `cmd/sqlgen/gen/templates/table/{get,count,exists,stream,update,delete}.go.tmpl` | Eight call sites resolve the option set before building conditions |
| `cmd/sqlgen/gen/filter_relationship_test.go` | **New.** Context-builder tests (list-only, O2M/M2M metadata, target soft-delete incl. `exclude_deleted:false`, target tenant, the transitive flag, composite-PK parent) + emission tests (O2M shape, M2M junction shape, both predicates inside the subquery, and the byte-identity guard that a relationship-free filter keeps its bare signature) |
| `cmd/sqlgen/testdata/examples/tenancy/{schema.sql,tests/main_test.go}` | `articles.user_id` FK + `resetDB` ordering |
| `cmd/sqlgen/testdata/examples/tenancy/tests/relationship_filter_test.go` | **New.** Eleven E2E tests over real SQLite: O2M/M2M row sets, nil member emits no `EXISTS`, soft-deleted related row, cross-tenant related row, predicate placement, `SkipTenancy`, `and`/`or` composition, two-hop correlation, the O2O-join path, keyset placeholder numbering, and the filter-accepting mutation surfaces |
| `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/relationship_filter_test.go` | **New.** The cross-dialect arm — O2M row set, M2M row set, composition with a column filter — against real PostgreSQL, MySQL, and SQLite |
| `docs/PRD.md` | §11.1 gains the `FilterOption` subsection: the signature extension and its emission rule, what rides in the option set and why depth is one of them, the shapes that emit nothing, and the MySQL 1093 constraint |

**Two tests are written failing-first rather than asserted.** `TestRelationshipFilter_SoftDeletedRelatedRowDoesNotMatch` asserts the filter *matches* while the article is live before soft-deleting it and asserting it stops — without the first half, a filter that never worked would pass. `TestRelationshipFilter_CrossTenantRelatedRowDoesNotMatch` seeds the drifted row by raw SQL (tenant B's `workspace_id`, tenant A's `user_id`) and then confirms the same row *is* reachable as tenant B, so the exclusion is attributable to the tenant predicate rather than to an empty table.

**Security note (no `/security-review` trigger).** No changed path falls under either trigger table: `sql/` is an ordinary module dependency rather than a `//go:embed` target, and no `cmd/sqlgen/cli/**` file-reading surface is touched. Every identifier the subquery emits goes through `Dialect.QuoteIdentifier` or `FormatTable`; every value — inner filter args, the soft-delete literal for bool/integer strategies, the tenant — travels as a bound arg. The one string spliced verbatim is the relationship's config `filter:`, which is developer-authored config already spliced by the existing O2M loader.

**Test-suite note.** `cmd/sqlgen/mcp`'s `TestRunGracefulShutdown/http_only` failed once during the closing sweep and passed on four subsequent runs including `-race`. The package has no changes in this diff; it is a pre-existing timing flake, recorded rather than swept under a "clean" claim.

**Three carry-forwards, none blocking.**

1. **MySQL error 1093 is documented, not prevented.** MySQL rejects an `EXISTS` naming the table an `UPDATE`/`DELETE` is mutating, so a **self-referential** relationship filter is read-path-only there. The generator cannot tell at codegen which filter a caller will hand to `UpdateWhere`, so this is stated in PRD §11.1 and on every generated helper's doc comment rather than enforced. PostgreSQL and SQLite accept the shape. 25.10's carry-forward #1 is thereby resolved as "constrained by documentation".
2. **A consumer hand-calling `ToConditions(dialect)` on a tenanted package gets `SkipTenancy` semantics** for its relationship subqueries. This is the design the user chose over a required parameter, and it is documented on `WithFilterTenant`; the generated call sites always pass the option, so only hand-composed SQL reaches it.
3. **Relationship filters do not yet reach the GraphQL surface.** That is 25.12 — the filter input member, translator recursion, and the API-exposure gating that keeps a relationship to an `api.enabled: false` table off the schema. Until then the members are Go-client-only.

## 25.12 Relationship filter GraphQL projection (Ticket D, F4)

**PRD Reference:** Sections 26.4 (filter input per B6), 26.5.3 (translator recursion), 11.1. Design §3.4, §2-D10.

**Status:** **Complete** (landed 2026-09-04 — `make check` clean across all 33 modules under `-race`, `make check-examples` clean across all 12 example modules)

**Governing decisions:** D10.

**Depends on:** 25.11 **and** 25.1.

### Tasks

- [x] Emit one relationship field per list relationship on the `<T>Filter` GraphQL input (e.g. `assignees: UserFilter`) — an ordinary nested input, so **no new gqlgen machinery**.
- [x] Extend the per-table filter translator to recurse into the target's `translate<Target>Filter` for each non-nil relationship member.
- [x] Gate relationship filter fields on the target entity's own API exposure — a relationship to an `api.enabled: false` table must **not** appear (it would be a read side-channel into an entity the schema deliberately hides).
- [x] Gate on §32.2 as well: a relationship whose target has no API-readable columns emits no filter member.
- [x] ~~Extend 25.2's filter-completeness lint to cover relationship members in both directions.~~ **Deferred to 25.2 at the time — the lint did not exist yet — and discharged there on 2026-09-04.** 25.2 was scheduled *after* 25.4/25.5 by 25.1's own sequencing note, so it landed after this sub-item; the requirement was written into 25.2's task list rather than dropped. What 25.12 delivered in its place remains the stronger guard for the relationship half — the schema field and the translator recursion are rendered from **one slice** (`APITableContext.FilterRelationships`), so they cannot drift by construction, and `TestAPIFilterRelationships_OneDerivationDrivesBothEmitters` asserts the round trip in both directions. `ValidateAPIFilterCompleteness`'s relationship arm is the second, cheaper guard on top: because both emitters read that one slice, "the two sides disagree" is not expressible there, so the lint checks what *is* — that each entry renders **both** halves, names a member the model `<T>Filter` actually has, and collides with neither a column field nor another member.
- [x] Confirm recursion terminates on self-referential and circular relationships (§13.5 / §13.6) — the GraphQL input type is recursive by name, which is legal, but the translator must not infinitely recurse on a cyclic *value*.
- [x] Add the E2E to the `graphql` example and regenerate goldens.

### Acceptance Criteria

- `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` executes and returns the correct rows — **the consumer's blocked story, closed**.
- Relationship filters compose with column filters and with `and` / `or` in a single query, with no reverse-navigation workaround.
- A relationship to an API-excluded entity emits no filter field.
- A self-referential relationship emits a valid recursive input type and the translator handles a nested value without unbounded recursion.
- ~~25.2's lint covers relationship members; a schema relationship filter field with no translator recursion is a hard codegen error.~~ **Not met — deferred with the lint** (see the Tasks note). The invariant holds structurally instead: both emitters read one `FilterRelationships` slice, so the drift this criterion guards against is unrepresentable rather than caught. It becomes a codegen error when 25.2 lands its carried-in task.
- Query count is unchanged from a plain filtered list — the `EXISTS` is a predicate, not an extra round-trip.

### Tests Required

- [x] Unit test: `<T>Filter` input emits a member per list relationship; none for API-excluded targets.
- [x] Unit test: translator recurses into `translate<Target>Filter`; a nil member is skipped.
- [x] Unit test: self-referential relationship emits a valid recursive input and translates a two-level nested value.
- [x] ~~Unit test: 25.2's lint fires when a relationship filter field has no translator recursion.~~ **Deferred with the lint itself and landed in 25.2 on 2026-09-04** as `TestAPIFilterCompleteness_RelationshipMemberHalfRendered` (see the Tasks note above). Its standing replacement, `TestAPIFilterRelationships_OneDerivationDrivesBothEmitters`, still walks every rendered `<T>Filter` block and asserts both directions against the context — the two now guard the same property from opposite ends.
- [x] **E2E over the real gqlgen server, the F4 pin:** `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` returns the expected rows; the same filter under `or` returns the union.
- [x] E2E with a counting `Querier`: the relationship-filtered list issues the same query count as an unfiltered list.
- [x] E2E: a relationship filter against a tenanted target returns nothing for another tenant's related row (25.11's injection reaching the API).
- [x] *(added)* E2E: the M2M junction shape reached through `CategoryFilter.users`, and a two-level self-referential nested value — the recursive input and the depth-keyed alias proven over HTTP rather than only in a hand-built context.

### Completion Record

**Landed 2026-09-04.** `make check` clean across all 33 modules under `-race`; `make check-examples` clean across all 12 example modules. Ticket D closes here: 25.10 built the runtime affordance, 25.11 emitted it into the Go client, and 25.12 puts it on the GraphQL surface — `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` now parses, translates and executes.

**The whole projection is one slice, and that is the design.** `APITableContext.FilterRelationships` is built once per table and read by both emitters: `schema.graphqls.tmpl` renders `{{ .GraphQLName }}: {{ .InputTypeName }}` and `filter_translate.go.tmpl` renders `out.{{ .ModelFieldName }} = {{ .TranslatorFunc }}(in.{{ .GoFieldName }})`. This is 25.1's D1 invariant applied to the relationship half, and it is why the sub-item needed no new machinery anywhere else — a nested input is an ordinary gqlgen input, and recursion into the target's existing `translate<Target>Filter` is an ordinary function call. **The F2 failure mode is unrepresentable here rather than merely absent:** there is no second derivation that could disagree. `TestAPIFilterRelationships_OneDerivationDrivesBothEmitters` asserts the round trip in both directions over every rendered `<T>Filter` block, including the reverse scan (no schema field may take a `<X>Filter` operand without a member behind it — column comparators are all named `<X>Comparator`, so the operand suffix is a sound discriminator).

**The exposure gate is the security half, and it is computed before the per-table loop.** `apiFilterTargets` resolves, once for the project, which tables a member may name: `api.enabled` (per-table override, else global) **and** at least one column surviving §32.2 as readable. It has to run first because a member is gated on the *target's* exposure and the target may be built after the parent. The gate matters because `EXISTS (SELECT 1 FROM secrets WHERE …)` answers a yes/no question about rows the caller cannot select — a relationship into an entity the schema deliberately hides is a read side-channel, not a convenience. The §32.2 half is **unreachable through a validated config** (§32.4 requires PK columns to stay `public` / `read_only`, so a real table always keeps one readable column); it is implemented and tested anyway because the context builder does not re-run config validation and the PRD states the gate as "excluded from the API", not "excluded by `api.enabled`".

**One fixture column carries both of the hard E2E requirements.** The tracker asks for a relationship filter against a **tenanted target** and for a **self-referential** recursive input, and the `graphql` example could reach neither. Its only tenanted tables are `workspace_settings` (composite PK ⇒ PRD §11.1 gives it no members at all) and `workspace_notes`; pointing an *untenanted* parent at `workspace_notes` would have flipped that parent to `HasTenantedRelationshipFilter`, and under this example's `tenancy.required: true` every previously-unscoped read of it would start returning `ErrMissing`. `workspace_notes.parent_id` — a nullable self-FK — adds the shape and moves nothing, because that table already resolves a tenant on every call. It is also, by construction, the §13.5 recursive input. The auto-detected O2M for a self-FK is named after the target table, so `exclude_relationships: [workspace_notes]` plus a declared `Children` edge renames it on every surface; the generated member is `children: WorkspaceNoteFilter`, an input that names itself. **The hazard named above was real, and dodging it here left it live in the product — see `FIX-193`.** `filterOptions` resolved a tenant on every filter-accepting call rather than only when the filter carried a relationship member, so a shared table that reached a tenanted target failed closed on reads that have no tenanted component at all. The gate now lives in `filterOptions`, so an untenanted parent pointing at a tenanted target is no longer a reason to avoid a fixture shape.

**Circularity (§13.6) fell out of the existing fixtures rather than needing one.** `assets ↔ documents` is an M2M in both directions, so the regenerated `graphql` example carries `AssetFilter.documents: DocumentFilter` and `DocumentFilter.assets: AssetFilter` — two inputs naming each other, which gqlgen accepts. Termination is not a property of the types: the translator recurses on the incoming *value*, which gqlgen built from a parsed query document and is therefore a finite tree. That is the same property the `and` / `or` recursion has relied on since Phase 16.

~~**The `or` semantics are not what the syntax suggests, and the E2E now pins that.**~~ **Resolved by FIX-195 (2026-09-10).** This recorded the pre-fix compilation: writing the union the obvious way — `or: [{orders: {…}}, {name: {eq: "Bob"}}]` — returned nothing, because §11.1's `ToConditions` appended one `sql.Or(subConds...)` **per entry**, so entries were ANDed and a union had to be one entry carrying both predicates. The open decision flagged here has been taken: §11.1 is amended so members OR together and a member's own fields AND together, which makes the obvious spelling the correct one. `TestRelationshipFilterAPI_RelationshipUnderOr` still asserts both spellings, with the two expectations swapped to match.

**Golden churn is confined to the GraphQL surfaces.** 100 files across five API-enabled examples — every diff outside the `graphql` example is the new nested input field plus its translator block (`graphql_null_wrappers`, `graphql_top_level`, `mysql`, `sqlite`), and the seven non-API examples regenerate untouched. Inside `graphql` the extra churn is the `parent_id` column and the `Children` edge threading through the model, walker, manifest and cache surfaces. **The `filterOptions` plumbing 25.11 introduced spread to exactly one new client** (`workspaceNoteClient`) — the fixpoint did not leak, which is the point of pointing the self-FK at an already-tenanted table.

**Two proofs were verified failing-first rather than asserted.** Deleting the two template `range` blocks fails five of the ten gen tests (the emission half; the context-level assertions correctly keep passing, since the context is not what was broken). Disabling the tenant injection inside the generated `workspaceNoteChildrenFilterExists` makes `TestRelationshipFilterAPI_TenantedTargetIsScoped` report `tenant A matched [a-parent] through a child row owned by tenant B` — the exact hole the subquery predicate closes. That test also seeds the drifted row (tenant B's `workspace_id`, tenant A's `parent_id`) and first confirms tenant B *can* read it, so the exclusion is attributable to the predicate rather than to an empty table.

**One out-of-scope fix was folded in at the user's direction: `sqlgen validate` never ran the post-build wiring.** 25.12's auto-review noticed that `ValidateGeneration` calls `BuildTableContexts` then `BuildAPIContext` directly, skipping `attachTenancyToTables` / `attachTenancyToViews` / `deduplicateRelationshipOptionsDefs` / `wireRelationshipFilters` — every pass `Generate` runs between those two points. Reproduced before deciding: a relationship whose config `filter:` contains a `$` makes `sqlgen generate` hard-error (25.11's guard) while `sqlgen validate` reports **clean**, which is precisely the class FIX-158 created `ValidateGeneration` to close. The origin is 25.11, not 25.12; it was folded in rather than FIX-logged because the user asked for it and the change is nineteen lines. The two attach passes precede the wire because each relationship filter entry reads the *target's* tenant column. Beyond the immediate error, this is what keeps 25.2's deferred relationship-lint arm from silently no-opping under validate — it would otherwise inspect an always-empty `FilterRelationships`. **Verified failing-first**: with the block removed, `TestValidateGeneration_WiresRelationshipFilters` reports `ValidateGeneration passed a config generate rejects with: building relationship filters: … contains "$" …`. A second test asserting the *order* of the mirrored passes was written and then **deleted as vacuous** — `ValidateGeneration` returns only warnings and an error, so a wrong order produces identical output and the assertion passed with the passes deliberately swapped. The ordering fact is covered on the generate path by 25.11's `TestRelationshipFilters_TargetTenantColumnAttached` and stated at the call site; asserting it twice, once falsely, would have been worse than not asserting it.

**Files changed:**

| File | Change |
|---|---|
| `cmd/sqlgen/gen/api_relfilter.go` | **New.** `apiFilterTargets` / `hasAPIReadableColumn` (the exposure gate) and `buildAPIFilterRelationships` (the projection), plus the defensive GraphQL-name collision guard |
| `cmd/sqlgen/gen/context_api.go` | `APIFilterRelationship`; `FilterRelationships` on `APITableContext`; `filterTargets` threaded `BuildAPIContext` → `buildOneAPITable` → `buildAPITableContext` |
| `cmd/sqlgen/gen/context.go` | `RelationshipFilterContext.SQLName` — the relationship's declared name, so the filter member and the object-type field are spelled by the one `graphQLFieldName` call |
| `cmd/sqlgen/gen/context_relfilter.go` | populates `SQLName` |
| `cmd/sqlgen/gen/api_field_overrides.go` | relationship members join the `<T>Filter` `fieldName` overrides — the translator dereferences them on a gqlgen-generated struct, so the Go identifier is dictated, not predicted (FIX-152) |
| `cmd/sqlgen/gen/templates/api/schema.graphqls.tmpl` | one nested input field per member |
| `cmd/sqlgen/gen/templates/api/filter_translate.go.tmpl` | one nil-guarded recursion per member |
| `cmd/sqlgen/gen/api_relationship_filter_test.go` | **New.** Ten tests: member-per-list-relationship, schema emission, translator recursion, `api.enabled: false` target dropped (with a positive control and a model-side check that the drop is API-only), unreadable target dropped, self-referential input, circular targets, the bidirectional one-derivation guard, the gqlgen `fieldName` coverage guard, and views-carry-none |
| `cmd/sqlgen/testdata/examples/graphql/{schema.sql,sqlgen.yml}` | `workspace_notes.parent_id` self-FK + `exclude_relationships` / declared `Children` edge |
| `cmd/sqlgen/testdata/examples/graphql/tests/relationship_filter_api_test.go` | **New.** Six E2E over the real gqlgen server: the F4 column+relationship pin, both `or` spellings, the M2M junction, query-count parity, tenanted-target isolation, and two-level self-referential nesting |
| `cmd/sqlgen/gen/orchestrate.go` | **Folded-in fix (25.11-origin).** `ValidateGeneration` mirrors the generate path's post-build wiring, so `sqlgen validate` no longer reports clean for configs `sqlgen generate` rejects |
| Goldens across `graphql`, `graphql_null_wrappers`, `graphql_top_level`, `mysql`, `sqlite` | regenerated |

**No PRD change was needed** — 25.0 already landed B6: §26.4's "Relationship (as a filter) → nested input field on `<T>Filter`" row and §26.5.3's "Relationship filters" paragraph (including the API-exclusion rule) are the text this sub-item implements.

**Security note (no `/security-review` trigger).** No changed path falls under either trigger table: nothing under `manifest/` or another `//go:embed` runtime target, and no `cmd/sqlgen/cli/**` file- or URL-reading surface. The sub-item is nonetheless security-adjacent on two counts, both covered above: the API-exposure gate closes a read side-channel, and the tenanted-target E2E proves 25.11's subquery tenant predicate reaches the HTTP boundary. No new SQL is emitted by this sub-item at all — the schema and the translator are the only new output, and the SQL they reach was frozen in 25.11.

**Three carry-forwards, none blocking.**

1. **25.2 must cover relationship members.** The lint half of this sub-item could not land because `ValidateAPIFilterCompleteness` does not exist yet and 25.2 is scheduled last. Written into 25.2's task list; the structural one-derivation guarantee stands in the meantime, and the `ValidateGeneration` wiring the lint will need is now in place (see the fold-in above).
2. **Input-object nesting depth is unbounded.** `api.graphql.max_depth` (§26.6) and gqlgen's complexity calculation both count *selection* depth; neither bounds how deeply a client may nest a filter input. This is pre-existing — `and: [{and: [{and: […]}]}]` has always been unbounded — but relationship members widen the cost of a level from a paren to a correlated subquery. Worth a bounded-input-depth decision in a later phase; not a regression introduced here.
3. **A relationship whose GraphQL name collides with a column filter field is dropped, and the branch is unreachable today.** The model side already refuses the collision by Go field name, and both GraphQL names derive from the same two SQL names, so a collision here implies one there. The guard sits at the point where the invalid document would be emitted (two fields of one name make gqlgen reject the whole schema) and is documented as defensive rather than load-bearing.
