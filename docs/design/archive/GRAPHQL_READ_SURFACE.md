# SQLGen GraphQL Read Surface — Comparator Projection, View Tenancy & View API — Design & Plan

> **Status: ARCHIVED (2026-09-16) — superseded by PRD §11.1 / §11.2 / §11.5 / §16.4 / §26.4 /
> §26.5.1 / §26.5.3 / §26.12 / §29.2.5 / §4.9 / §4.13 + Appendix A** (synced 2026-09-11).
>
> **Do not read this file for current behavior.** The cited PRD sections are normative and
> supersede it on any conflict — see [PRD §26](../../PRD.md#26-api-generation) for the GraphQL
> surface. Archived because Phase 25 is closed and nothing in the code or the PRD references this
> document; it is kept for the record described below, not as a reference.
>
> **Original status note follows.** Implemented and closed as
> **Phase 25** (all 13 sub-items, 25.0–25.12; tickets A–D closed). The normative behavior lives in
> the cited PRD sections, which supersede this document on any conflict; this file is retained for
> the decision record (D1–D11), the gap analysis (F1–F9), and the rejected/deferred record (§7).
>
> Originally sourced from a consumer-side gap report written while building a real GraphQL read
> surface against generated output, then re-verified against sqlgen source — every claim below
> carries a file:line citation, and that pass found five defects the consumer report did not
> (F5–F9 in [§1.2](#12-additional-findings-not-in-the-consumer-report)).
>
> **The §5 PRD deltas B1–B7 have all landed** (25.0, verified 2026-08-25; B6/B8 2026-08-25), so
> §5 is a historical record of the sync, not outstanding work. Two decisions were amended during
> implementation and the PRD is authoritative on both: **D11** — the correlated `EXISTS` carries
> its correlation column structurally (`sql.Exists`) and the builder resolves the qualifier per
> read path, rather than the doc's originally-sketched textual form; and the **D2 completeness
> lint** resolved as *drop the filter field*, not hard-error, so the `StringComparator` fallback
> described below is deleted (PRD §26.12 amended to match).

## 1. Motivation

The generated GraphQL read surface was verified end-to-end through the HTTP seam for the first
time by a reference consumer. It works for scalar column filters, sort, both pagination shapes,
and one-query relationship loading. It fails in four places, one of which is a **cross-tenant
read hole** and one of which is a **silent wrong-results** bug — the schema advertises filter
fields the translator ignores, so the server returns the unfiltered set and the client believes
it filtered.

### 1.1 Verified findings

| # | Finding | Class | Evidence |
|---|---------|-------|----------|
| **F1** | Views carrying the tenant column are **not tenant-scoped**. `ViewContext` has no tenancy field at all; `BuildTenancyContext` iterates `schema.Tables` only. | Security | `cmd/sqlgen/gen/context_tenancy.go:57` (`for i := range schema.Tables`), `context_view.go:74-99` (no `Tenancy` field in the returned `ViewContext` literal), `context_client.go:124-131` (`TenantedEntities` built from `tables` only) |
| **F2** | Enum / JSON / JSONB / Slice filter fields are **advertised in the schema and silently dropped by the translator**. | Correctness | Schema side: `templates/api/schema.graphqls.tmpl:41` emits `{{ comparatorFor $f }}`, and `funcmap.go:275-292` falls through to `StringComparator` (default arm at `:290`) for every unrecognised bare type — including enum type names. Translator side: `context_api.go:1119-1122` skips the field when `comparatorTranslatorVariant` returns `false`, which it does for `Enum` / `JSON` / `JSONB` / `Slice` (`context_api.go:1168-1194` handles only `String`/`Bool`/`Time`/`ID`/`Number`). |
| **F3** | Views get **no GraphQL surface**. `generateAPI` never receives view contexts. | Feature gap (unimplemented spec) | `orchestrate.go:160` — `generateAPI(tmpl, tableContexts, schema, …)`; views are generated at `orchestrate.go:145-150` and passed only to `generateClientAndHooks`. `cli/graphql.go:265-278` builds the gqlgen `models:` merge from `apiCtx.Tables` only, which is why a consumer must hand-write the `models:` binding for a view. |
| **F4** | A root list cannot be filtered by a **related entity**. Relationship fields take no arguments and `<T>Filter` has no relationship member. | Design limitation | `templates/api/schema.graphqls.tmpl:13-16` (relationship fields emitted with no args), `templates/shared/_filter.tmpl:4-15` (filter struct is columns + `PKs` + `and`/`or` only) |

### 1.2 Additional findings (not in the consumer report)

| # | Finding | Class | Evidence |
|---|---------|-------|----------|
| **F5** | **Sort-enum drift on digit-leading columns.** The schema emits enum values through `screamingSnakeCase`, which prefixes `col_` when the name starts with a digit (per PRD §8.5). The sort translator builds its switch cases with a bare `strings.ToUpper(toSnakeCase(...))` — no prefix. A column like `2010_revenue` therefore advertises `COL_2010_REVENUE` and switches on `"2010_REVENUE"`, so `<table>SortFieldToColumn` returns `""` and the sort is **silently dropped**. Same failure class as F2, different emitter pair. | Correctness | `funcmap.go:300-302` (`screamingSnakeCase`) vs `context_api.go:1154` (`EnumValue: strings.ToUpper(toSnakeCase(c.Name))`); consumed at `templates/api/schema.graphqls.tmpl:51` and `templates/api/sort_translate.go.tmpl:58-64` |
| **F6** | **The five shipped comparator inputs are themselves incomplete projections** of their Go counterparts. `StringComparator` omits `gt/gte/lt/lte/nlike`; `NumericComparator` omits `nin/nbetween`; `TimeComparator` omits `in/nin/nbetween`; `IDComparator` omits `gt/gte/lt/lte`; **none** of the five expose the `Null` operator, so `IS NULL` / `IS NOT NULL` is unreachable from GraphQL on every nullable column. Not silently wrong (absent, not ignored) — but the same "project the family faithfully" work. | Feature gap | `templates/api/shared.graphqls.tmpl:27-79` (hard-coded input blocks) vs `comparator/string.go:8-23`, `number.go:8-20`, `time.go:10-22`, `id.go:8-19`, `bool.go:8-13`; the translator template documents the `Null` omission explicitly at `templates/api/comparator_translate.go.tmpl:24-29` ("the GraphQL comparator inputs do not currently expose a `null` field") |
| **F7** | **Decimal columns cannot be range-filtered at all over GraphQL.** `decimal.Decimal` routes to `comparator.String` on the model side (FIX-040), so the schema emits `StringComparator` — which has no `gt/gte/lt/lte` (F6). `price: { gt: … }` is inexpressible. Note PRD §26.5.3's own example claims `translateNumericComparatorDecimal`, which does not exist. | Feature gap + PRD/impl divergence | `funcmap.go:287-288`, `context_table.go:1122-1134`, PRD §26.5.3 example line 10607 |
| **F8** | **`sql.PrefixConditions` will corrupt any `EXISTS` condition.** `prefixCondition` unconditionally does `c.Clause = alias + "." + c.Clause` on leaf clauses. The O2O-join read path pipes every filter condition through it (`templates/table/get.go.tmpl:235`, `:275`), so an `EXISTS (SELECT …)` clause emitted by a relationship filter would render as `t.EXISTS (SELECT …)`. This is a hard prerequisite for F4, and the existing contract comment already flags the limitation. | Runtime prerequisite | `sql/condition.go:143-167` (the prefix at `:164`) |
| **F9** | **Views have no cache read path**, so making them tenanted carries **no** cache-key coupling — cached views participate only in invalidation-by-source-table. This narrows the F1 fix substantially versus the table equivalent (no §29.5 key-grammar work, no `AffectedTenants` carrier, no fingerprint change). | Scope reducer (good news) | `templates/view/*.tmpl` contain no cache references; `context_cache.go:73-81` (`CachedView` has no `Tenanted` field), `context_cache.go:218-233` |

### 1.3 The load-bearing structural cause behind F2 and F5

The GraphQL filter and sort surfaces are each emitted by **two independent functions that derive
the same fact from different inputs**:

| Surface | Schema emitter | Translator emitter | Derived from |
|---|---|---|---|
| Filter comparator | `funcComparatorFor(APIFieldContext)` → switches on `GraphQLBare` | `comparatorTranslatorVariant(string)` → parses the model's `*comparator.X[Y]` type expression | GraphQL type vs. Go type |
| Sort enum value | `screamingSnakeCase(f.SQLName)` | `strings.ToUpper(toSnakeCase(c.Name))` | same input, two spellings |

Nothing forces agreement. Whenever they disagree, the schema wins at parse time and the
translator wins at execution time — the field is accepted and ignored. **Any fix that adds
comparator families without collapsing this to one derivation is a fix for today's drift and a
setup for tomorrow's.** Collapsing it is the durable half of this phase and the reason F5 is in
scope alongside F2.

---

## 2. Decision record

| # | Decision | Rationale |
|---|---|---|
| **D1** | **One field map, two emitters.** Compute the filter projection **once per column** into `APIFieldContext` (`ComparatorInput`, `TranslatorFunc`, `ComparatorValueType`), and have the schema template and the filter translator both read those fields. Same for sort (`SortEnumValue` computed once, read by both). Delete `funcComparatorFor` and the second sort spelling. | The invariant becomes structural, not a convention: it is *impossible* to emit a filter field in the schema that the translator can't handle, because they read the same struct field. |
| **D2** | **Add a codegen completeness lint**, `ValidateAPIFilterCompleteness` / `ValidateAPISortCompleteness`, modelled on the existing `ValidateAPIWalkerCompleteness` (`api_walker.go`). Codegen hard-errors if a schema-emitted filter or sort field has no translator entry, or vice versa. | D1 makes drift impossible by construction; D2 makes a future regression impossible to land silently. The repo already has this pattern and it is cheap. |
| **D3** | **Monomorphize the generic comparator families per concrete type.** `Enum[T]` → `input <EnumGraphQLName>Comparator` reusing the existing enum binding (`<EnumGraphQLName>EnumComparator` when the derived name is one of the fixed families' — FIX-175); `Slice[T]` → `input <Elem>SliceComparator`; `JSON` → `input JSONComparator`; `JSONB` → `input JSONBComparator`; decimal → `input DecimalComparator` (operands typed `Decimal`, translating into `comparator.String`). | GraphQL has no generics. Monomorphizing keeps schema-level validation (`{eq: "banana"}` is rejected by the parser, not at runtime) and reuses the enum/scalar bindings that already exist. Rejected alternative in [§7](#7-rejected--deferred). |
| **D4** | **Complete the five existing families in the same ticket** (F6/F7): add the missing operators, and expose `IS NULL` on nullable columns. ⚠️ **Superseded in part by PRD §26.4 Rule 2 (2026-08-25):** nullability is a **separate input type** (`Nullable<X>Comparator`), not a conditional `isNull` field on a shared input — GraphQL input types are global, so the conditional form cannot hold alongside the one-input-per-family sharing rule. The operator-completion half stands. | The projection is either faithful or it isn't. Doing it once, driven by D1's field map, costs one pass over the operator tables; doing it later means a second golden-file churn for no reason. Pre-release, so no compatibility cost ([[feedback_no_backward_compat]]). |
| **D5** | **A view carrying the tenant column is tenanted, exactly like a table** — read path only, fail-closed, resolved from the same `TenantResolver`. Auto-detected; opt out with `views.<name>.tenancy.enabled: false`. | Detection-based inclusion is the established §29.2.3 rule for tables. Applying a different rule to views is what created the hole. Views are read-only, so `Get`/`GetMany`/`Count`/`Paginate`/`Connection` are the entire blast radius: no mutation mismatch check, no `captureAffectedTenants`, no cache keys (F9). |
| **D6** | **A nullable tenant column on a view is scoped anyway, with a codegen warning** — not the hard error tables get. It becomes a hard error only when the user wrote `views.<n>.tenancy.enabled: true` explicitly. | On a table, a nullable tenant column is a schema bug (a row nobody can see). On a view it is a legitimate `LEFT JOIN` / aggregate artifact. Scoping is the fail-closed direction either way — `WHERE workspace_id = $1` drops NULL rows — so the secure outcome needs no error. Erroring would block legitimate views for no security gain. |
| **D7** | **Materialized-view `Refresh` / `RefreshConcurrently` stay unscoped and Go-client-only.** | `REFRESH MATERIALIZED VIEW` recomputes the whole relation; a per-tenant refresh is not a thing. Views emit no GraphQL mutations (D8), so `Refresh` never reaches the API surface — it stays an operator-level Go call. |
| **D8** | **Views become first-class members of `APIContext.Tables` with `IsView: true`**, not a parallel `APIViewContext`. Mutation emission gates on `!IsView`; the operations mask for a view accepts only `get` / `get_many` / `paginate` / `connection` and config-errors on a mutation key. | Every downstream template, the walker, the envelope aliases, the gqlgen `models:` merge, the resolver seeds, and both translators already iterate `apiCtx.Tables`. A parallel type would fork all of them. |
| **D9** | **F3 (view GraphQL surface) ships strictly after F1 (view tenancy)** and in the same phase. | Exposing an unscoped view over HTTP *is* the delivery vehicle for the cross-tenant hole. Shipping F3 first would ship the vulnerability, on by default. |
| **D10** | **F4 ships as junction/FK-backed filter fields on the parent filter, compiling to `EXISTS`** — `TaskFilter.assignees: UserFilter`. Filter *arguments* on relationship fields are **deferred** ([§7](#7-rejected--deferred)). Requires a prefix-exempt condition kind in `sql/` (F8). | This is the shape the consumer's actual story needs — `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` composes with column filters and with `and`/`or` for free, because it rides the existing filter-input + translator path and gqlgen unmarshals it as part of `TaskFilter`. Relationship-field args cannot compose with a root filter, and cost far more (see §7). |
| **D11** | **The `EXISTS` correlation qualifier is resolved by the builder, not baked in at codegen** (settled 2026-08-25). `sql.Exists(correlationColumn, subquery)` is a structured condition carrying only the parent-side **column**; `BuildSelect` / `BuildSelectJoin` render `<alias-or-quoted-table>."col"` at build time. | The qualifier is path-dependent — `FROM "tasks"` on the plain read path, `FROM "tasks" t` on the O2O-join path — so any pre-baked qualifier is invalid SQL on one of them; surviving `PrefixConditions` is necessary but not sufficient. A structured value is also exempt from alias prefixing **by construction** (it is not a leaf clause), which the `Qualified bool` alternative would have made an opt-out someone can forget. Rejected: a `{{parent}}` substitute token (keeps clauses stringly-typed, fails at the DB rather than at codegen) and always-aliasing `BuildSelect` (rewrites the SQL text of every non-join `SELECT` in the project). Normative text: PRD §11.5. |

---

## 3. Design

### 3.1 F2/F5/F6/F7 — comparator & sort projection (one field map)

**New per-column projection.** `mapColumnToGraphQL` gains a single call that resolves the
column's *complete* filter projection from the same inputs the model side already uses
(`ColumnContext` + dialect), and writes it onto `APIFieldContext`:

```go
type APIFilterProjection struct {
    InputTypeName string // "TaskStatusComparator", "JSONBComparator", "StringSliceComparator"
    TranslatorFunc string // "translateTaskStatusComparator", "translateNullableJSONBComparator"
    Operators []APIComparatorOperator // name → GraphQL type, for the shared-schema emitter
    Nullable  bool  // drives `isNull` emission and the Nullable<X> wrapper
}
```

The schema template reads `$f.Filter.InputTypeName`; the filter translator reads
`$f.Filter.TranslatorFunc`. `funcComparatorFor` is deleted. `shared.graphqls.tmpl` stops
hard-coding the five inputs and renders from `APIContext.ComparatorFamilies` (which already
exists as a `[]string` and becomes `[]APIComparatorFamily` carrying the operator list) — so a
newly monomorphized family needs no template edit.

**Family matrix** (Go → GraphQL). ⚠️ The `isNull` column below reflects the **pre-blessing**
shape; PRD §26.4 Rule 2 is normative: each family projects a base input plus a
`Nullable<X>Comparator` twin (same operands + `isNull: Boolean`), and a column references
whichever matches its nullability. Rule 1 likewise carves out `Number[T]` — all Go numerics bind
to the single `Float` scalar, so the numeric family is **not** monomorphized (as this matrix
already shows). Read the matrix for operators and operand types; read the PRD for naming.

| Go comparator | GraphQL input | Operand type | New? |
|---|---|---|---|
| `String` | `StringComparator` | `String` | + `gt gte lt lte nlike isNull` |
| `ID` | `IDComparator` | `ID` | + `gt gte lt lte isNull` |
| `Number[T]` | `NumericComparator` | `Float` | + `nin nbetween isNull` |
| `Bool` | `BooleanComparator` | `Boolean` | + `isNull` |
| `Time` | `TimeComparator` | `Time` | + `in nin nbetween isNull` |
| `String` (decimal column) | **`DecimalComparator`** | `Decimal` | new (F7) |
| `Enum[T]` | **`<EnumGraphQLName>Comparator`** (`…EnumComparator` on collision) | the bound enum | new — `eq neq in nin isNull` |
| `JSON` | **`JSONComparator`** | `JSON` / `String` | new — `contains hasKey isNull` |
| `JSONB` | **`JSONBComparator`** | `JSON` / `String` / `[String!]` | new — `hasKey hasAnyKey hasAllKeys contains containedBy pathExists isNull` |
| `Slice[T]` | **`<Elem>SliceComparator`** | `[<Elem>!]` | new — `containsAny containsAll containedBy isEmpty isNull` |

`JSONBComparator` and `*SliceComparator` are emitted **only** under `input.dialect: postgres`,
matching the model-side rule in `resolveSimpleComparator` and PRD §11.2's PostgreSQL-only note.
`comparator.Custom` is never projected (it is a raw-SQL escape hatch, not an API surface).

**Enum translation** is a one-liner because the enum is already bound: the GraphQL `DONE` → Go
`TaskStatus("done")` round-trip is the existing `models:` binding (`cli/graphql.go:303-311`), so
`translateTaskStatusComparator` copies pointers and slices with no conversion.

**Sort (F5).** `APIFieldContext` gains `SortEnumValue`, computed once by `screamingSnakeCase`.
Both the schema enum and `buildAPISortFields` read it. `buildAPISortFields` stops re-deriving.

### 3.2 F1 — view tenancy

**Detection.** `BuildTenancyContext` gains a second loop over `schema.Views`, producing entries
in the same map keyed by qualified name, via a `resolveViewTenancy` that mirrors
`resolveTableTenancy` minus the write-path concerns:

- same `tenancy.column` resolution and per-view `enabled` override (new — [§4](#4-config-surface-additions));
- **participates in the §29.2.4 uniform-type validation** across the run — the tenant resolver
  is a single typed function, so a view whose tenant column resolves to `int64` while tables
  resolve to `uuid.UUID` must hard-error like a mismatched table would;
- nullable tenant column → warn + scope (D6);
- no `InPrimaryKey` handling (views have no DDL PK; `@pk` is an annotation for `Get`, and §29.7's
  composite-PK constructor-omission rule is a create-path concern that views don't have).

**Attachment.** New `attachTenancyToViews(views, tenancyMap)` mirroring `attachTenancyToTables`,
setting `ViewContext.Tenancy *TableTenancyContext` (reuse the type — the fields are a superset)
and folding the tenant import plus `github.com/teandresmith/sqlgen/tenancy` into `ViewContext.Imports`.

**Emission.** The view templates gain the same three blocks the table templates already have,
copied structurally so the diff reads as a port rather than a new design:

| Template | Block |
|---|---|
| `view/client.go.tmpl` | `tenantResolver tenancy.TenantResolver[T]` field + the `resolveTenant(ctx, explicit)` method (verbatim port of `table/client.go.tmpl:242-305`, whose `resolveTenant` starts at `:272`, minus the `captureAffectedTenants` half) |
| `view/get.go.tmpl` | tenant predicate in `Get` (PK path) and `GetMany` |
| `view/count.go.tmpl` | tenant predicate in `Count` |
| `view/pagination.go.tmpl` | tenant predicate in `Paginate` and `Connection` |

`CallOptions` needs **no change** — `SkipTenancy` and `Tenant *T` are already package-level and
present whenever tenancy is enabled (`context_shared.go:50-59`), and view methods already accept
`opts ...func(*CallOptions[...])`. `applyClientTenancy` extends to take views so
`ClientContext.TenantedEntities` includes tenanted views and the unified client threads the
resolver in (`templates/client.go.tmpl:188-191`). No cache work (F9).

### 3.3 F3 — view GraphQL surface

`generateAPI` takes `views []ViewContext`. `BuildAPIContext` appends one `APITableContext` per
API-enabled view with `IsView: true`, `Operations` masked to the read set, and
`HasCreateInput`/`HasUpdateInput` false — which the existing FIX-071 gates already use to
suppress the create/update inputs and the mutation block. `funcHasAnyMutation` returns false for
a view, so no `extend type Mutation` is emitted.

Per view, this yields exactly: the object type, `<V>Connection` / `<V>Edge` / `<V>ListResult`,
`<V>Filter`, `<V>Sort` + `<V>SortField`, `Query.<view>` (only when `HasPK`),
`Query.<views>` (connection, only when `HasConnection` — `cursor_keys` already gates this),
`Query.<view>List`, the field-options walker, the filter/sort translators, the envelope aliases,
and the gqlgen `models:` bindings. Views have no relationships, so the walker is columns-only and
the §25.1 query-count contract for a view read is exactly one query.

`ValidateAPIWalkerCompleteness` is generalized to take the column set + relationship set rather
than a `TableContext`, so views are linted identically.

`cli/graphql.go::buildMergeInput` picks views up for free once they are in `apiCtx.Tables` —
which retires the hand-written `models:` entry the consumer needs today.

### 3.4 F4/F8 — relationship filtering

**Runtime prerequisite (F8).** ✅ **Settled 2026-08-25 (D11) — normative in PRD §11.5.** `sql/`
gains a structured `Exists{CorrelationColumn, Subquery}` condition. Codegen emits the correlation
**column** and never a qualifier; the builder resolves it at render time — `SelectOptions.Alias`
when the statement aliased its table, `Dialect.FormatTable(table)` otherwise — so one emitted
value is correct on both the plain `BuildSelect` path and the aliased `BuildSelectJoin` path.
`PrefixConditions` passes it through untouched because it is a structured value rather than a
leaf string clause, the same way `And` / `Or` / `Subquery` / `Range` already are, so the
exemption cannot be forgotten. *(Superseded reading: the original text below proposed
pre-qualifying at codegen — that is wrong, because the qualifier is path-dependent. Surviving
`PrefixConditions` is necessary but not sufficient.)*

**Model side.** `<T>Filter` gains one field per list relationship:

```go
type TaskFilter struct {
    // … columns …
    Assignees *UserFilter `json:"assignees"`   // O2M / M2M → EXISTS
    Labels    *LabelFilter `json:"labels"`
    And []*TaskFilter
    Or  []*TaskFilter
}
```

`ToConditions` compiles each non-nil relationship filter into an `EXISTS` correlated subquery
built from the join metadata already on `RelationshipContext` (`FKColumn` for O2M/O2O;
`JunctionTable` + `JunctionLocalFK` + `JunctionReferenceFK` for M2M — `context.go:239-250`).
The target filter's own `ToConditions` supplies the inner `WHERE`, so nesting and `and`/`or`
compose recursively, and the target's soft-delete and **tenant** predicates must be injected
into the subquery on the same rules the target's own read path uses.

**GraphQL side.** `<T>Filter` gains `assignees: UserFilter`; the filter translator recurses into
`translateUserFilter`. Nothing new is needed in gqlgen — it is an ordinary nested input.

**Scope discipline.** One level per hop, arbitrary depth by nesting; no aggregate predicates
(`count > 3`) in this phase.

---

## 4. Config surface additions

All additive, all tri-state pointers matching the established table shape.

```yaml
views:
  project_stats:
    tenancy:
      enabled: false      # opt out of the auto-detected view tenant scope (D5)
      column: tenant_id   # per-view column override, mirrors tables.<t>.tenancy.column
    api:
      enabled: false      # exclude this view from the GraphQL surface
      operations:         # subtractive; read ops only — a mutation key is a config error
        connection: false
```

`ViewConfig` (`config/config.go:602`) gains `Tenancy *TableTenancyConfig` and `API *ViewAPIConfig`.
`ViewAPIConfig` reuses `Operations` but validates against a read-only subset of
`apiOperationFields` — setting `create` / `update` / `soft_delete` / … on a view is a config
error, in the same spirit as `clientOnlyOperationFields` (`config/config.go:686-692`): silently
ignoring a knob the user deliberately turned is worse than saying the block can't express it.

---

## 5. PRD deltas required (bless before code)

| Delta | Section | Change |
|---|---|---|
| **B1** | §29 (new §29.13, or extend §29.2.3) | Tenancy detection applies to **views** as well as tables: a view carrying the tenant column is tenanted; read path only; per-view `enabled` / `column` override; nullable tenant column → warn + scope (D6); views participate in the §29.2.4 uniform-type check; matview `Refresh` explicitly unscoped (D7). Also remove the implicit "tables only" reading from §29.2.3. |
| **B2** | §26.4 comparator table + schema example | Add the missing families: `<Enum>Comparator`, `JSONComparator`, `JSONBComparator`, `<Elem>SliceComparator`, `DecimalComparator`. State the monomorphization rule (D3) and the PostgreSQL-only gating for JSONB/Slice. |
| **B3** | §26.4 comparator input definitions | Complete the five shipped inputs (F6) and define the `isNull` rule: exposed iff the column is nullable. |
| **B4** | §26.5.3 | Correct the `translateNumericComparatorDecimal` example (F7 — that function does not exist; decimals route through `comparator.String`) and document the new `DecimalComparator` path. State the D1 single-field-map invariant and the D2 lint. |
| **B5** | §16.4 table + §26.4 | Views' GraphQL row moves from aspirational to specified: enumerate the exact emitted read surface (§3.3) and the `views.<n>.api` gating. |
| **B6** | §11.1 / §26.4 | Relationship filter fields on `<T>Filter` (D10) — model-side field, `EXISTS` compilation, target-side soft-delete + tenant injection, one-hop-per-level scope. |
| **B7** | §4.9 ViewConfig | Document the new `tenancy` and `api` blocks (§4). |

B1–B5 are the F1/F2/F3 half and are independent. B6 is F4 and can be blessed separately if the
D10 shape is not accepted (§7).

---

## 6. Phased rollout & ticket breakdown

Proposed as **Phase 25 — GraphQL Read Surface Completion**. Ticket letters map to the consumer
report's A–D so the two documents stay cross-referable.

### 25.0 — PRD sync (B1–B7) *(no code; unblocks everything)*
Land the §5 deltas. Per project rule 1 the spec is blessed before the code, and every ticket
below cites it.

### Ticket A — comparator & sort projection *(closes F2, F5, F6, F7)*
- **25.1** — **One field map.** Compute the filter projection and sort enum value once per column
  onto `APIFieldContext`; repoint `schema.graphqls.tmpl`, `filter_translate.go.tmpl`,
  `sort_translate.go.tmpl`, and `buildAPISortFields` at it; delete `funcComparatorFor` and the
  duplicate sort spelling. Drive `shared.graphqls.tmpl` from `ComparatorFamilies`. **Fixes F5 on
  its own.** *Pure refactor — must be byte-identical on the existing examples except the F5
  digit-leading case, which is the proof it worked.*
- **25.2** — **Completeness lint** (D2). `ValidateAPIFilterCompleteness` +
  `ValidateAPISortCompleteness`, wired into the same codegen error path as the walker lint.
  *Failing-first: hand-build a context with a schema field and no translator entry, assert the
  hard error.* Depends on 25.1.
- **25.3** — **Complete the five shipped families** (F6) + `isNull` on nullable columns +
  `DecimalComparator` (F7). *Depends on 25.1; golden churn expected and intended.*
- **25.4** — **Enum comparators.** `<EnumGraphQLName>Comparator` per used enum (disambiguated to
  `<EnumGraphQLName>EnumComparator` against the fixed families — FIX-175), reusing the
  existing enum binding. Nullable enums get `isNull`. Enum-array columns route to 25.5's slice
  form, not here. Depends on 25.1.
- **25.5** — **JSON / JSONB / Slice comparators.** `JSONComparator` (all dialects),
  `JSONBComparator` + `<Elem>SliceComparator` (postgres only). The `json[]`/`jsonb[]`
  non-filterable case (FIX-103) must keep being skipped on *both* sides — 25.2's lint is the
  guard. Depends on 25.1.

### Ticket B — view tenancy *(closes F1 — the security fix; valuable standalone)*
- **25.6** — **Detection + attachment.** `resolveViewTenancy`, view loop in
  `BuildTenancyContext`, uniform-type participation, `attachTenancyToViews`, D6 nullable warning,
  `views.<n>.tenancy` config + validation, `applyClientTenancy` over views so the unified client
  threads the resolver. *No template changes yet — assert the resolved context in unit tests.*
- **25.7** — **Read-path emission.** Tenant predicate into `view/get.go.tmpl` (Get + GetMany),
  `count.go.tmpl`, `pagination.go.tmpl` (Paginate + Connection); `resolveTenant` +
  `tenantResolver` into `view/client.go.tmpl`. *E2E: a tenanted view returns only the resolver's
  tenant across all five read methods; `SkipTenancy` and explicit `Tenant` behave as on tables;
  `tenancy.required: true` + absent resolver → `ErrMissing`.* Depends on 25.6.

### Ticket C — view GraphQL surface *(closes F3; **depends on B**, D9)*
- **25.8** — **Views into `APIContext`.** `IsView` on `APITableContext`, read-only operations
  mask, `views.<n>.api` config + mutation-key validation, generalized walker lint, `generateAPI`
  signature. Depends on 25.7 (D9) and 25.1.
- **25.9** — **Emission + binding.** Per-view schema file, walker, filter/sort translators,
  envelope aliases, resolver seeds, gqlgen `models:` merge. *E2E: query a tenanted view over
  HTTP as two tenants and assert isolation — the exact hole this phase closes; plus a query-count
  assertion that a view read is one query.* Depends on 25.8.

### Ticket D — relationship filtering *(closes F4; independent of A/B/C)*
- **25.10** — **`sql` prefix-exempt condition** (F8) + unit tests proving an `EXISTS` clause
  survives `PrefixConditions` and placeholder numbering stays stable across the O2O-join path.
- **25.11** — **Model-side relationship filter fields.** `<T>Filter` relationship members;
  `EXISTS` compilation for O2M / M2M / O2O from the existing join metadata; target-side
  soft-delete and tenant injection inside the subquery; recursion through `and`/`or`.
  *Cross-dialect E2E — the subquery shape differs in quoting, not structure.* Depends on 25.10.
- **25.12** — **GraphQL projection.** Relationship fields on `<T>Filter` input + translator
  recursion. *E2E: `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` — the consumer's
  actual blocked story.* Depends on 25.11 and 25.1.

**Dependency order.** 25.0 first. A (25.1 → {25.2, 25.3, 25.4, 25.5}) and B (25.6 → 25.7) are
parallel. C (25.8 → 25.9) needs both 25.7 and 25.1. D (25.10 → 25.11 → 25.12) is independent
except 25.12 wanting 25.1's field map. **Shortest path to closing the security hole is 25.0(B1) →
25.6 → 25.7.**

---

## 7. Rejected / deferred

- **`StringComparator` + runtime enum parse** (instead of D3). Rejected: `{status: {eq: "banana"}}`
  becomes a runtime error instead of a schema rejection, losing the introspection contract and
  the client-side codegen benefit that is the whole point of a typed enum.
- **Filter/sort arguments on relationship fields** (`assignees(filter: …)`) — **deferred**, not
  rejected. The runtime already supports it: `<Target>RelationshipOptions` carries `Filter` and
  `Sorts` (`templates/shared/_field_options.tmpl:56-60`). The blocker is arg *decoding*: nested
  field arguments are not reachable through gqlgen's public API from inside the field-options
  walker — `graphql.CollectedField` exposes raw `ast.Value` arguments, and the generated
  `unmarshalInput<T>Filter` methods are unexported on gqlgen's `executionContext`. Supporting it
  means sqlgen generating its own `map[string]any` → model-filter decoder, duplicating gqlgen's
  unmarshaling and re-implementing every custom scalar's parse. The alternative — per-relationship
  field resolvers, which gqlgen *would* generate arg parsing for — forfeits the §25.1 one-query
  guarantee. Neither trade is worth taking before D10 is in consumers' hands, and D10 covers the
  reported story. Revisit only if concrete demand surfaces for *per-parent-row* relationship
  filtering, which is what this shape uniquely buys.
- **Aggregate relationship predicates** (`assignees: {count: {gt: 3}}`). Deferred — needs a
  `GROUP BY`/`HAVING` subquery shape and its own operator vocabulary.
- **`comparator.Custom` projection.** Never. It is a raw-SQL escape hatch; exposing it over HTTP
  is an injection surface.
- **Tenant-scoping matview `Refresh`** (D7). Not a coherent operation.
- **View mutations of any kind.** Views are read-only per §16.4; nothing here changes that.

---

## 8. Blast radius

| Area | Change |
|---|---|
| `sql/` (runtime core) | One additive prefix-exempt condition affordance (25.10). Stdlib-only, no new deps. |
| `comparator/` | **None.** Every family the GraphQL layer gains already exists and is already used by the Go client. |
| `tenancy/` | **None.** `TenantResolver` is reused as-is. |
| `cache/` | **None** (F9 — views have no cache read path). |
| Generator (`cmd/sqlgen/gen`) | The bulk: `context_api.go` (field map, view entries), `context_tenancy.go` (view detection), `context_view.go` (tenancy attachment), `context_client.go` (tenanted views), `orchestrate.go` (view→API threading), `funcmap.go` (delete `funcComparatorFor`), new lints. |
| Templates | `api/{schema.graphqls,shared.graphqls,filter_translate,sort_translate,comparator_translate}.tmpl`; `view/{client,get,count,pagination}.tmpl`; `shared/_filter.tmpl`. |
| `cmd/sqlgen/config` | Additive `ViewConfig.Tenancy` / `.API` + validation. |
| `cmd/sqlgen/cli` | `buildMergeInput` picks up views for free once they are in `apiCtx.Tables`. |
| Golden files | Substantial, intended churn in the `graphql` example (new comparator inputs, `isNull`, view surface) and in any tenanted example carrying a view. 25.1 is the one ticket that must be byte-identical apart from the F5 fix. |
| Consumer-visible | Two behavior changes worth calling out in the phase closure: a previously-unscoped tenanted view read **starts filtering** (the fix), and views **start appearing** in the GraphQL schema (opt out with `views.<n>.api.enabled: false`). |
