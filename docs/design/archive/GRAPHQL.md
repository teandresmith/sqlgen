# GraphQL API Generation — Design & Plan

> **Status: ARCHIVED (2026-09-16) — superseded by PRD §26 (synced 2026-04-29).**
>
> **Do not read this file for current behavior.** The normative GraphQL spec is
> [PRD §26](../../PRD.md#26-api-generation); it supersedes this document on any conflict, and
> §26 has moved since the sync — Phase 25 (GraphQL read surface completion) and Phase 26 (stdlib
> UUID) both amended it. This file is retained only for the Phase 16 design record: the open
> questions ([§19](#19-open-questions--decisions-needed)), the testing-strategy notes
> ([§17](#17-testing-strategy)), and the cross-library convention comparisons that motivated the
> curated resolver surface ([§6](#6-resolver-generation-strategy-sync-prd-265)).
>
> Archived per `docs/tracker/phase-16.md` 16.9 ("Archive `docs/GRAPHQL.md`"). Archival alone does
> not close Phase 16 — the remaining 16.9 tasks are still open.
>
> The §21 sync plan has been executed. The authoritative GraphQL spec now lives in `docs/PRD.md`:
> - §26.3 Configuration (config table with `gqlgen_config`, `gqlgen_bin`, `scalars`, `max_complexity`)
> - §26.4 Schema generation (full type-mapping table + curated example schema)
> - §26.4.1 Scalar marshaling (built-in registry + 4-category model)
> - §26.5 Resolver generation (intro)
> - §26.5.1 Exposed resolver surface (curated per-table surface + delete/restore naming + nullability rules)
> - §26.5.2 Field selection translation (recursive walker + completeness lint)
> - §26.5.3 Filter / sort / pagination translation
> - §26.5.4 Mutation translation (including `_inc` / `_dec` input operators)
> - §26.5.5 Error mapping (sentinel → `extensions.code` table)
> - §26.5.6 Module boundary + wrapper subcommand
> - §26.5.7 Why no dataloader
> - §26.10 Per-table API configuration (operations gating)
>
> The implementation plan moved to `docs/tracker/IMPLEMENTATION_ORDER.md` Phase 16 (sub-items 16.1–16.9).
>
> The implementation itself landed across sub-items 16.1–16.8g; see `docs/tracker/phase-16.md` for the per-sub-item completion records and `docs/tracker/fixes.md` for the ~20 FIX entries closed during 16.8 bring-up.
>
> **Original purpose preserved below for context.** The original goal was: working design for sqlgen's GraphQL API generation feature, stepping through every part of the feature, capturing decisions, and listing references to use during development. Once stable, sections marked **[SYNC]** would be merged back into `docs/PRD.md` §26.

---

## 1. Scope

**In scope (Phase 16 GraphQL):**
- Generate `.graphqls` schema files from the parsed SQL schema
- Generate concrete gqlgen resolver implementations that wire to the existing unified client (curated surface — see §6.0; not every client method becomes a root field)
- Provide a `sqlgen graphql gen` wrapper that merges the model / schema / scalar mappings sqlgen needs into the consumer-owned `gqlgen.yml` in memory and subprocess-invokes gqlgen (the on-disk `gqlgen.yml` stays consumer-owned)
- Generate `fieldOptionsFromFields` helpers — recursive walkers that translate GraphQL selection sets into typed `FieldOptions`
- Generate filter / sort / pagination translation helpers — both cursor (Connection) and offset (`<Type>ListResult` envelope) at the root, no per-relationship pagination args (relationships are flat `[T!]!`; see §6.0)
- Generate query depth and complexity gates
- Per-table opt-in / opt-out via `api.enabled` and existing `operations` config
- Per-call `CallOptions` plumbing from HTTP headers (`Cache-Control: no-cache`, `X-Skip-Events`, `X-Skip-Hooks`)

**Out of scope (deferred):**
- REST API generation — Phase 16
- gRPC API generation — future, see PRD §26.13
- GraphQL subscriptions (depends on event system; deferred until Phase 17 if pursued)
- Field-level authorization helpers (consumer responsibility; documented patterns only)
- File uploads (separate endpoint pattern)

**Non-goals:**
- No business logic in generated resolvers — pure translation only
- No dataloader for sqlgen-generated code (see §14 below)
- No reflection at runtime — codegen produces concrete walkers per entity

---

## 2. Design Principles

1. **Schema-driven, single source of truth.** SQL schema → sqlgen models → GraphQL schema + resolvers. The consumer never hand-writes type definitions for tables.
2. **Reuse, don't duplicate.** GraphQL types map onto existing sqlgen-generated Go structs via `gqlgen.yml` model mappings. No parallel type hierarchy.
3. **Parent-prefetch only — no dataloader.** Field selection is translated to a complete `FieldOptions` tree at the root resolver. Child resolvers return the already-loaded value. PRD §25.1 query-count guarantee holds end-to-end.
4. **Pure translation layer.** Generated resolvers translate inputs → client call → outputs. No caching, retry, auth, or state lives in the resolver layer.
5. **Module boundary preserved.** `gqlgen` lives in the consumer's `go.mod`. The sqlgen runtime (`./`) stays stdlib-only. Generated resolvers live in the consumer's project alongside `models_gen.go`.
6. **Per-table opt-out.** Tables with `api.enabled: false` or `operations: read_only` are excluded from mutations (or entirely from the schema).

---

## 3. Pipeline & Module Boundary

```
SQL schema files          (existing input)
    │
    ▼
sqlgen parse              (existing parser/)
    │
    ▼
sqlgen generate           (Phase 16: extends gen/ with API context + templates)
    │
    ├─→ models_gen.go         (existing — entity / filter / input structs)
    ├─→ {table}_gen.go        (existing — unified client)
    ├─→ graph/*_gen.graphqls  (NEW — GraphQL schema files)
    ├─→ graph/resolvers_gen.go (NEW — concrete resolver impls)
    ├─→ graph/field_options_gen.go (NEW — recursive selection-set walkers)
    ├─→ graph/filter_translate_gen.go (NEW — input type → Filter struct)
    └─→ graph/middleware_gen.go (NEW — HTTP header → CallOptions)
    │
    ▼
sqlgen graphql gen         (NEW — wrapper: reads the consumer's gqlgen.yml, merges
    │                        sqlgen-owned models / schema / scalars in memory,
    │                        writes a temp config, subprocess-invokes the gqlgen
    │                        binary. The on-disk gqlgen.yml is consumer-owned and
    │                        never modified.)
    │
    └─→ graph/generated_gen.go  (gqlgen-managed — server, resolver interfaces, scalar handling)
```

**Module boundary:**

| Module | gqlgen dep? | Reason |
|--------|:-----------:|--------|
| sqlgen runtime (`./`) | NO | Must stay stdlib-only per ARCHITECTURE.md |
| `parser/` | NO | Pure SQL parsing |
| `cmd/sqlgen/` | NO | Codegen tool — emits gqlgen-shaped files but does not import gqlgen |
| Consumer project | YES | gqlgen is a build-time tool the consumer invokes |

**Key invariant:** `cmd/sqlgen` generates files that *use* gqlgen APIs (e.g., `graphql.CollectFieldsCtx`), but does not itself import gqlgen. The compile-time check happens in the consumer's `go build`, not sqlgen's CI. This matches how the runtime cache module emits `cache.GetAs[*T]` calls without importing the consumer's types.

**Wrapper invariant:** `sqlgen graphql gen` does the YAML merge in-process (using `cmd/sqlgen`'s YAML dependency, not new) and shells out to gqlgen via `os/exec`. cmd/sqlgen still does not import gqlgen as a Go dependency — the consumer's project is the only place gqlgen lives.

---

## 4. Generated Artifacts (per project)

| File | Generator | Regenerated each run? | Editable by consumer? |
|------|-----------|:--------------------:|:---------------------:|
| `graph/*_gen.graphqls` | sqlgen | Yes | No (`*_gen.*` files are managed) |
| `graph/*.graphqls` (custom) | — | — | Yes — sqlgen merges adjacent custom schema |
| `graph/resolvers_gen.go` | sqlgen | Yes | No |
| `graph/field_options_gen.go` | sqlgen | Yes | No |
| `graph/filter_translate_gen.go` | sqlgen | Yes | No |
| `graph/middleware_gen.go` | sqlgen | Yes | No |
| `graph/scalars_gen.go` | sqlgen | Yes | No — external `MarshalX`/`UnmarshalX` for category-4 types (`UUID`, `Decimal`, `json.RawMessage`-override `JSON` binding, etc.); skipped for sqlgen-runtime types (`JSON`, `DateTime`) which carry their own methods. See §5.1 |
| `graph/generated_gen.go` | gqlgen (via `sqlgen graphql gen` wrapper) | After each wrapper run | No (gqlgen-managed) |
| `gqlgen.yml` | consumer | No (one-shot scaffold via `sqlgen graphql init` if missing) | Yes — fully consumer-owned; sqlgen never reads or writes the on-disk file outside the one-shot init |
| `graph/server.go` | sqlgen (one-shot) | **Only if missing** | Yes — entry-point bootstrapping |

**gqlgen.yml ownership:** The consumer owns `gqlgen.yml` end-to-end so they can freely add custom directives, bind hand-written resolver models, configure plugins, register custom scalars not present in the SQL schema, etc. sqlgen contributes its required entries (model mappings for managed tables, the `graph/*_gen.graphqls` schema glob, scalars derived from `api.graphql.scalars` config) at wrapper-invocation time — they are merged into a temp config and passed to gqlgen as a subprocess. The consumer's file on disk is left alone.

**Schema glob:** the consumer's `gqlgen.yml` should reference `graph/*.graphqls` so consumer-authored schema files (custom scalars, computed types) merge with sqlgen's generated ones. The `sqlgen graphql init` scaffold sets this up; the wrapper enforces it on each run by ensuring the merged config includes the glob.

---

## 5. SQL → GraphQL Schema Generation **[SYNC: PRD §26.4]**

| SQL Concept | GraphQL Concept | Notes |
|-------------|----------------|-------|
| Table | Object type (`Product`) | PascalCase |
| View | Object type (read-only — only Query fields generated) | |
| Column | Field | Name controlled by `field_casing` config |
| `NOT NULL` column | `Type!` | |
| Nullable column | `Type` | |
| Enum type | GraphQL enum | Values always `SCREAMING_SNAKE_CASE` |
| Composite type | Object type | |
| Domain type | Underlying scalar | (e.g., `email_address` → `String`) |
| O2O / M2O relationship — NOT NULL FK | `Type!` | Single object reference |
| O2O / M2O relationship — nullable FK | `Type` | Single object reference |
| O2M relationship | `[Type!]!` | Flat list, no per-relationship pagination args (see §19 Q4); the bulk-IN loader populates the full set |
| M2M relationship | `[Type!]!` | Same as O2M |
| `id` (PK) — Go `uuid.UUID` | `UUID!` | sqlgen-generated external marshalers (see §5.1) |
| `id` (PK) — Go `string` | `ID!` | GraphQL `ID` (spec built-in) |
| `id` (PK) — Go `int` / `int64` | `Int!` | GraphQL `Int` (spec built-in) |
| Timestamp / Time / Date — Go `types.DateTime` / `types.NullDateTime` | `DateTime` (declared as `scalar DateTime` on first use) | sqlgen-shipped runtime type with native `MarshalGQL` / `UnmarshalGQL`; nullable column binds to `types.NullDateTime`, both share one scalar |
| Timestamp / Time / Date — Go `time.Time` | `Time` | gqlgen-bundled built-in scalar (auto-binds `time.Time`); used when consumer overrides the binding |
| UUID column (non-PK) | `UUID` if Go type is `uuid.UUID`; `String` if Go type is `string` | Driven by the resolved Go binding (same rule as PK) — `uuid.UUID` → sqlgen-generated external marshalers; `string` → GraphQL `String` |
| `numeric` / `decimal` | `Decimal` if Go type is `decimal.Decimal`; `Float` otherwise | Driven by the resolved Go type — if the column's Go binding is `github.com/shopspring/decimal.Decimal` (or another configured decimal type via `overrides.types`), emit `Decimal` with sqlgen-generated external marshalers; otherwise fall back to `Float` (spec built-in) |
| JSON / JSONB — Go `types.JSON` | `JSON` (declared as `scalar JSON` on first use) | sqlgen-shipped runtime type with native `MarshalGQL` / `UnmarshalGQL`; default for JSON/JSONB columns. Consumers overriding to `json.RawMessage` bind to the same `JSON` scalar via sqlgen-generated external marshalers (passthrough). |
| Filter struct | Input type (`ProductFilter`) | One per table, mirrors generated `XFilter` Go struct |
| Comparator | Input type (`StringComparator`, `NullableTimeComparator`, `DecimalComparator`, etc.) | One per (family, operand type, nullability) — reused across all tables; a nullable column takes the `Nullable<X>` twin, which adds `isNull` (PRD §26.4 Rule 2). Emitted only when a column references it (Rule 3) |
| Create input | Input type (`CreateProductInput`) | Mirrors `CreateProductInput` Go struct (omittable fields → optional GraphQL fields) |
| Update input | Input type (`UpdateProductInput`) | Mirrors `UpdateProductInput` Go struct |
| Cursor pagination (root) | Relay Connection (`<Type>Connection`, `<Type>Edge`, `PageInfo`) | Reuses `database.Connection[T]` / `Edge[T]` / `PageInfo`; emitted for every readable table |
| Offset pagination (root) | `<Type>ListResult { items, totalCount, offset, limit }` envelope | Distinct from `[T!]!` so relationships and root-list shapes don't collide; `totalCount` removes the need for a separate count resolver |
| `Operations` config (read-only / disabled) | Excludes Mutations or whole type | Per-table |

Field naming follows `api.graphql.field_casing` — accepted values `camel_case` (default) or `snake_case`. The config value uses snake_case for uniformity with other sqlgen config; the *emitted* GraphQL field names are camelCase or snake_case per the chosen value. Type names always PascalCase, Query/Mutation root names always camelCase, Enum values always SCREAMING_SNAKE_CASE. PRD §26.3 captures this — no change needed.

### 5.1 Scalar Marshaling

gqlgen requires every non-primitive scalar to be marshalable in one of four ways (see https://gqlgen.com/reference/scalars/):

1. **Spec built-ins.** `String`, `Int`, `Float`, `Boolean`, `ID` — handled by gqlgen with no consumer-side code.
2. **gqlgen-bundled built-ins.** `Time`, `Map`, `Upload`, `Any`, `Int64` — gqlgen ships `MarshalX` / `UnmarshalX` for these. We emit no marshaler body, but we *do* merge a `models:` entry pinning the scalar to the bundled marshaler that carries the declared `go_type`. That is not redundant with gqlgen's defaults: it binds `Int64` to the two-entry list `[graphql.Int, graphql.Int64]` and a generated input field takes `Model[0]`, so an `int64` column routed onto `Int64` used to get an `int` field and a translator that did not compile. Both halves of `marshaling: builtin` are config-validated — the name must be one gqlgen bundles, the `go_type` one its marshaler carries (FIX-170).
3. **Method-based marshaling.** Type has `MarshalGQL(io.Writer)` and `UnmarshalGQL(any) error` methods. gqlgen picks them up via `gqlgen.yml` model mapping. **This is where sqlgen-shipped runtime types live** — see "Sqlgen-shipped scalar types" below.
4. **External marshalers.** Free functions `MarshalX(v T) graphql.Marshaler` and `UnmarshalX(v any) (T, error)` declared in a Go package the gqlgen.yml `models:` entry references. Used for third-party types (`uuid.UUID`, `decimal.Decimal`) that don't ship with the marshaling methods.

#### Sqlgen-shipped scalar types

The sqlgen runtime ships custom types that already model nullability and JSON-shape semantics: `types.JSON`, `types.DateTime`, `types.NullDateTime`. These types implement `MarshalGQL(io.Writer)` and `UnmarshalGQL(any) error` natively, using only stdlib (`io.Writer`, `any`) — **no gqlgen import is added to the runtime module, the stdlib-only invariant holds.** `types.JSON.UnmarshalGQL` accepts any JSON shape (object, array, scalar, null) and re-encodes to bytes; shape constraints are a resolver-layer concern (see PRD §7.6 for the rationale).

GraphQL-side behavior:
- When a column's resolved Go type is one of these, sqlgen emits a `scalar X` declaration into the generated `.graphqls` on first use (deduped — once per scalar, not once per column).
- The wrapper merges a `models:` entry into the consumer's `gqlgen.yml` binding the GraphQL scalar to the runtime type. `DateTime` lists both `types.DateTime` and `types.NullDateTime` so gqlgen picks the right one based on field nullability.
- No `MarshalX` / `UnmarshalX` is emitted into `graph/scalars_gen.go` for these — the methods live on the runtime types themselves.

| Scalar | Bound Go type | Source | Marshaler |
|--------|---------------|--------|-----------|
| `String` / `Int` / `Float` / `Boolean` / `ID` | `string` / `int` / `float64` / `bool` / `string` | GraphQL spec | built-in (none needed) |
| `Time` | `time.Time` | gqlgen-bundled | bundled — `models:` pinned to `graphql.Time` (category 2) |
| `JSON` | `types.JSON` (default) or `json.RawMessage` (override) | sqlgen runtime / stdlib | **method-based, native on the type** when bound to `types.JSON`; **sqlgen-generated external marshalers** (passthrough) when bound to `json.RawMessage`. Both bindings produce the same `JSON` scalar in the schema. |
| `DateTime` | `types.DateTime` (NOT NULL) / `types.NullDateTime` (NULL) | sqlgen runtime | **method-based, native on the type** |
| `UUID` | `uuid.UUID` | third-party | **sqlgen-generated external marshalers** (uses `String()` / `uuid.Parse`) |
| `Decimal` | `decimal.Decimal` | third-party | **sqlgen-generated external marshalers** (uses `String()` / `decimal.NewFromString`) |
| Custom scalar (consumer-declared) | consumer type | consumer | consumer-supplied — `api.graphql.scalars` declares mode (`builtin` / `method` / `external`); sqlgen emits no body (FIX-159) |

**Generated file: `graph/scalars_gen.go`** — holds the external `MarshalX` / `UnmarshalX` functions for every **built-in registry** category-4 scalar in use. Not emitted when every scalar in the schema falls into categories 1–3 or is consumer-declared — a consumer-declared `external` scalar is marshaled from its own `marshaler_package` and contributes nothing here (FIX-159; PRD §26.4.1 is authoritative). The wrapper merges the corresponding `models:` entries into the consumer's `gqlgen.yml` so gqlgen picks them up:

```yaml
# Merged into the consumer's gqlgen.yml at wrapper time
models:
  UUID:
    model:
      - <consumer>/graph.UUID            # type alias declared in graph/scalars_gen.go
  Decimal:
    model:
      - <consumer>/graph.Decimal
```

**Detection.** sqlgen carries a built-in registry of known types (`uuid.UUID`, `shopspring/decimal.Decimal`, `json.RawMessage`, etc.) and the marshaling mode each requires. For unknown types appearing in `api.graphql.scalars`, the consumer must declare `marshaling: method | external | builtin`; codegen errors otherwise rather than silently producing a non-compiling resolver. (No reflection — detection is purely config-driven.)

**Why generate, not vendor:** the consumer already has `uuid.UUID` / `decimal.Decimal` in their `go.mod` via the existing sqlgen integration; adding a sqlgen-vendored marshaler library would force a second import of the same dep and risk version drift. Generating into the consumer's project keeps the existing single-source-of-truth.

---

## 6. Resolver Generation Strategy **[SYNC: PRD §26.5]**

Generated resolvers are **concrete implementations** of the gqlgen-generated resolver interfaces. They translate inputs → client call → outputs, with no business logic.

### 6.0 Exposed Resolver Surface

Not every method on the unified client becomes a root resolver. The exposed surface is curated to match the conventions established by Hasura, PostGraphile, and Prisma's GraphQL plugins, which keeps the schema small and predictable. Per-row computed mutations (increment, decrement) are folded into update inputs as **input operators**, not separate root fields — see §9.1.

**Per-table surface:**

| Surface | Generated when... | Backed by client method |
|---------|-------------------|-------------------------|
| `<table>(id): <Type>` query | always (if `api.enabled` and reads allowed) | `Get` |
| `<table>s(filter, sort, first, after, last, before): <Type>Connection!` query (cursor) | always | `Connection` |
| `<table>List(filter, sort, limit, offset): <Type>ListResult!` query (offset, see envelope below) | always | `List` |
| `create<Table>(input)` mutation | `operations` includes create | `Create` |
| `create<Table>s(inputs)` mutation | `operations` includes create | `CreateMany` |
| `update<Table>(id, input)` mutation | `operations` includes update | `Update` (with `_inc` / `_dec` input operators dispatching to `Increment` / `Decrement` server-side) |
| `update<Table>s(filter, input)` mutation | `operations` includes update | `UpdateMany` |
| `upsert<Table>(input)` mutation | `operations` includes upsert AND PK / unique present | `Upsert` |
| `delete<Table>(id): Boolean!` mutation | `operations` includes hard delete only | `HardDelete` |
| `delete<Table>(id): <Type>` (nullable) mutation | `operations` includes soft delete only | `SoftDelete` |
| `hardDelete<Table>(id): Boolean!` + `softDelete<Table>(id): <Type>` mutations | both delete variants enabled | `HardDelete` / `SoftDelete` (no bare `delete<Table>` is generated) |
| `restore<Table>(id): <Type>` mutation | `operations` includes soft delete | `Restore` |

**`<Type>ListResult` envelope** (one per readable table):

```graphql
type ProductListResult {
  items:      [Product!]!
  totalCount: Int!
  offset:     Int!
  limit:      Int!
}
```

The envelope is intentionally distinct from `[Product!]!` so the schema's *shape* signals semantics: `Connection` = cursor paginated, `ListResult` = offset paginated, `[T!]!` = unbounded relationship list. Connection's own `totalCount` field on `<Type>Connection` covers count for cursor mode, and `ListResult.totalCount` covers it for offset mode — so a separate `<table>Count` resolver is redundant and is **not** generated.

**Excluded from the GraphQL surface (intentionally):**

| Excluded method | Why | How to express in GraphQL instead |
|-----------------|-----|-----------------------------------|
| `Exists` | redundant with single-by-PK | `<table>(id)` returns null when missing |
| `Count` | folded into `<Type>Connection.totalCount` and `<Type>ListResult.totalCount` | `<table>List(filter, limit: 0).totalCount` for "count only, no rows" |
| `GetMany(ids)` | derivable | `<table>s(filter: { id: { in: [...] } })` |
| `Find(filter)` | derivable | `<table>s(filter, first: 1)` then take edge |
| `Increment` / `Decrement` | per-row computed mutations idiomatically belong inside update inputs | `update<Table>(id, input: { stock_inc: 5 })` — see §9.1 |
| Tx primitives, savepoints | transaction lifecycle is server-internal | not exposed; consumer can compose mutations in a single request and rely on gqlgen middleware to wrap a tx if desired |
| `Restore` (when soft delete is disabled) | meaningless | not generated |
| Per-relationship pagination args | runtime loader uses bulk-IN, doesn't support per-row LIMIT (see §19 Q4) | relationships emit as `[Type!]!`; future phase may upgrade to lateral-join SQL |

This matches the cross-library pattern: every per-row computed mutation becomes an input operator, exists/find/get-many fold into the list query, and transaction primitives never cross the GraphQL boundary.

### 6.1 Query resolvers

```go
func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Get(ctx, id, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *queryResolver) Products(ctx context.Context, filter *ProductFilterInput, first *int32, after *string, last *int32, before *string, sort []*ProductSortInput) (*database.Connection[models.Product], error) {
    return r.client.Products.Connection(ctx, database.ConnectionInput[models.ProductFilter]{
        First:  first,
        After:  after,
        Last:   last,
        Before: before,
        Filter: translateProductFilter(filter),
        Sort:   translateProductSort(sort),
    }, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = productFieldOptionsFromContext(ctx)
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *queryResolver) ProductList(ctx context.Context, filter *ProductFilterInput, sort []*ProductSortInput, limit *int32, offset *int32) (*ProductListResult, error) {
    fo := productFieldOptionsFromContext(ctx)
    out, err := r.client.Products.List(ctx, &models.ListInput[models.ProductFilter]{
        Filter: translateProductFilter(filter),
        Sort:   translateProductSort(sort),
        Limit:  derefInt32(limit, 50),
        Offset: derefInt32(offset, 0),
    }, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
    if err != nil { return nil, err }
    return &ProductListResult{
        Items:      out.Items,
        TotalCount: int32(out.TotalCount),
        Offset:     int32(out.Offset),
        Limit:      int32(out.Limit),
    }, nil
}
```

`Exists`, `Count`, `GetMany`, and `Find` are intentionally not exposed — see §6.0 for the rationale and the GraphQL-side equivalents.

### 6.2 Mutation resolvers

```go
func (r *mutationResolver) CreateProduct(ctx context.Context, input CreateProductInput) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Create(ctx, translateCreateProductInput(input), func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *mutationResolver) CreateProducts(ctx context.Context, inputs []CreateProductInput) ([]*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    in := make([]*models.CreateProductInput, len(inputs))
    for i := range inputs { in[i] = translateCreateProductInput(inputs[i]) }
    return r.client.Products.CreateMany(ctx, in, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *mutationResolver) UpdateProduct(ctx context.Context, id string, input UpdateProductInput) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Update(ctx, id, translateUpdateProductInput(input), func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *mutationResolver) UpdateProducts(ctx context.Context, filter *ProductFilterInput, input UpdateProductInput) ([]*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.UpdateMany(ctx, translateProductFilter(filter), translateUpdateProductInput(input), func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

func (r *mutationResolver) DeleteProduct(ctx context.Context, id string) (bool, error) {
    err := r.client.Products.HardDelete(ctx, id)
    if errors.Is(err, sqlgen.ErrNotFound) { return false, nil }
    return err == nil, err
}

// Generated when the table has a soft-delete column AND `operations` enables it.
func (r *mutationResolver) SoftDeleteProduct(ctx context.Context, id string) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.SoftDelete(ctx, id, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

// Generated alongside SoftDelete.
func (r *mutationResolver) RestoreProduct(ctx context.Context, id string) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Restore(ctx, id, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

// Generated when `operations` enables upsert AND a PK / unique constraint exists.
func (r *mutationResolver) UpsertProduct(ctx context.Context, input CreateProductInput) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Upsert(ctx, translateCreateProductInput(input), func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}
```

**Crucial: `Create`, `CreateMany`, `Update`, `UpdateMany`, `Upsert`, `SoftDelete`, `Restore` already chain a `Get`/`GetMany` internally when `FieldOptions` is non-nil.** The resolver just passes FieldOptions through — no extra round-trip in the resolver layer. `HardDelete` returns no entity (boolean envelope; see §9.3 for the naming-rules table when soft and hard delete coexist).

The update translator (`translateUpdateProductInput`) reads the input's `_inc` / `_dec` fields and dispatches to `Increment` / `Decrement` on the client side; the resolver never calls those methods directly. See §9.1.

### 6.3 Field resolvers — relationships

Generated field resolvers for relationships return the already-loaded value. No DB call:

```go
func (r *productResolver) Company(ctx context.Context, p *models.Product) (*models.Company, error) {
    return p.Company, nil  // pre-loaded by parent Get/GetMany via FieldOptions
}

func (r *productResolver) Reviews(ctx context.Context, p *models.Product) ([]*models.Review, error) {
    return p.Reviews, nil  // pre-loaded
}
```

If a relationship is selected at runtime but `FieldOptions` did not include it (this should be impossible if the recursive walker is correct — see §7 below), the field returns nil. Document this contract: "selected ⇒ available, unselected ⇒ nil." The generated walker test (§17.3) is the regression guard.

---

## 7. Field Selection Translation — The Central Mechanism

This is the load-bearing piece. Every guarantee depends on the walker being recursive and complete.

### 7.1 Approach

Per-entity generated function that walks gqlgen's selection set tree and produces a fully-populated `FieldOptions`:

```go
// graph/field_options_gen.go — generated per entity
func productFieldOptionsFromContext(ctx context.Context) *models.ProductFieldOptions {
    return productFieldOptionsFromCollected(graphql.CollectFieldsCtx(ctx, nil))
}

func productFieldOptionsFromCollected(fields []graphql.CollectedField) *models.ProductFieldOptions {
    fo := &models.ProductFieldOptions{}
    for _, f := range fields {
        switch f.Name {
        case "id":        fo.ID = true
        case "name":      fo.Name = true
        case "price":     fo.Price = true
        case "createdAt": fo.CreatedAt = true
        case "company":
            fo.Company = companyFieldOptionsFromCollected(graphql.CollectFields(f.Selections, nil))
        case "reviews":
            fo.Reviews = reviewFieldOptionsFromCollected(graphql.CollectFields(f.Selections, nil))
            // O2M / M2M produce a Connection-shaped field; walk into edges → node.
        case "tags":
            fo.Tags = tagFieldOptionsFromCollected(graphql.CollectFields(f.Selections, nil))
        }
    }
    return fo
}
```

### 7.2 Connection unwrapping

For O2M / M2M relationships exposed as Connections, the GraphQL selection looks like `reviews { edges { node { author { name } } } }`. The walker has to descend through `edges` → `node` → real field selections:

```go
case "reviews":
    fo.Reviews = unwrapConnectionAndWalk(f, reviewFieldOptionsFromCollected)
```

The `unwrapConnectionAndWalk` helper is shared (one impl in a generated `graph/connection_walker_gen.go`).

### 7.3 Fragment & directive handling

`graphql.CollectFieldsCtx` and `graphql.CollectFields` already:
- Flatten named and inline fragments
- Apply `@skip` / `@include` directives
- Resolve aliases transparently (the walker sees the underlying field name)

We rely on these being correct. No custom fragment expansion needed.

### 7.4 Risk: walker bug

The only failure mode is a missing case in the generated `switch` — a relationship the walker doesn't recognize, so it's silently nil in the response. Mitigation:

- **Generated unit test per table** (added at codegen time): builds a deeply-nested GraphQL query touching every relationship and asserts `fieldOptionsFromContext` produces the expected `FieldOptions` tree.
- **Lint rule:** every column / relationship in the parsed schema MUST have a corresponding case in the walker — fail codegen if not.

---

## 8. Filter / Sort / Pagination Translation

### 8.1 Filter translator

GraphQL `ProductFilterInput` → Go `*models.ProductFilter`. Generated per table:

```go
// graph/filter_translate_gen.go
func translateProductFilter(in *ProductFilterInput) *models.ProductFilter {
    if in == nil { return nil }
    out := &models.ProductFilter{}
    if in.Name != nil   { out.Name = translateStringComparator(in.Name) }
    if in.Price != nil  { out.Price = translateNumericComparator(in.Price) }
    if in.CreatedAt != nil { out.CreatedAt = translateTimeComparator(in.CreatedAt) }
    if len(in.And) > 0 {
        for _, sub := range in.And { out.And = append(out.And, translateProductFilter(sub)) }
    }
    if len(in.Or) > 0 {
        for _, sub := range in.Or  { out.Or  = append(out.Or,  translateProductFilter(sub)) }
    }
    if in.IncludeDeleted != nil { out.IncludeDeleted = *in.IncludeDeleted }
    return out
}
```

Comparator translators (`translateStringComparator`, `translateNumericComparator`, etc.) are **shared, not per-table** — one impl per comparator family in `graph/comparator_translate_gen.go`. This avoids combinatorial explosion.

### 8.2 Sort translator

```go
func translateProductSort(in []*ProductSortInput) []sort.Order {
    var out []sort.Order
    for _, s := range in {
        out = append(out, sort.Order{
            Column:    productSortFieldToColumn(s.Field),
            Direction: directionToSortDirection(s.Direction),
        })
    }
    return out
}
```

`productSortFieldToColumn` is a generated `switch` over the GraphQL enum.

### 8.3 Pagination

Relay Connection args (`first`, `after`, `last`, `before`) map directly to `database.ConnectionInput`. No translation function needed — pass-through.

---

## 9. Mutation Translation

### 9.1 Create / Update inputs

GraphQL `CreateProductInput` (struct with optional fields) → `*models.CreateProductInput` (struct with `omittable.Value[T]` fields):

```go
func translateCreateProductInput(in CreateProductInput) *models.CreateProductInput {
    out := &models.CreateProductInput{Name: in.Name, Price: in.Price}
    if in.CompanyID != nil { out.CompanyID = omittable.Set(*in.CompanyID) }
    return out
}
```

The omittable distinction is important: GraphQL "null" and "omitted" are distinguishable in input types, and `omittable.Value[T]` preserves that. PRD §17 (omittable) is the source of truth.

#### Increment / Decrement as input operators

`Increment` and `Decrement` are not exposed as separate root mutations (see §6.0 for rationale). Instead, the generated `UpdateProductInput` carries paired `_inc` / `_dec` fields for every numeric column the schema defines. Following the Hasura / Prisma convention, the client sets at most one of `<field>`, `<field>_inc`, `<field>_dec` per column per request.

GraphQL schema (generated):

```graphql
input UpdateProductInput {
  name: String
  # numeric columns get a value field + _inc + _dec
  stock:     Int
  stock_inc: Int
  stock_dec: Int
  price:     Decimal
  price_inc: Decimal
  price_dec: Decimal
}
```

Translator dispatches per column:

```go
func translateUpdateProductInput(ctx context.Context, in UpdateProductInput) updatePlan {
    plan := updatePlan{set: &models.UpdateProductInput{}}
    if in.Name      != nil { plan.set.Name  = omittable.Set(*in.Name) }
    if in.Stock     != nil { plan.set.Stock = omittable.Set(*in.Stock) }
    if in.StockInc  != nil { plan.inc = append(plan.inc, columnDelta{Col: "stock", Delta: *in.StockInc}) }
    if in.StockDec  != nil { plan.dec = append(plan.dec, columnDelta{Col: "stock", Delta: *in.StockDec}) }
    // ...
    return plan
}
```

The mutation resolver runs the plan in this order, all on the same client instance so any `Tx` wrapping middleware sees one logical update:

1. `Update(id, plan.set)` — only if any `_set` fields are present.
2. For each `inc` entry: `Increment(id, col, delta)`.
3. For each `dec` entry: `Decrement(id, col, delta)`.

Validation: codegen rejects an input that mixes `_set` and `_inc` for the same column (one wins is ambiguous). At runtime, the resolver returns a `gqlerror` with `extensions.code = INVALID_INPUT` if both arrive. `_inc` and `_dec` for the same column in one call is also rejected.

#### Bulk inputs

`createMany` takes `[CreateProductInput!]!` (a list of the regular create input). `updateMany` takes a single `UpdateProductInput` plus a `ProductFilterInput` — same input shape as the one-row update, applied to every row matching the filter. Bulk variants share the same `_inc` / `_dec` translation.

### 9.2 Mutation result types

GraphQL mutation result types may include relationship fields (e.g., `mutation { createProduct(...) { reviews { ... } } }`). This works because:

1. `Create` takes `FieldOptions` via `CallOptions`
2. `Create` chains a `Get` internally with those options when `FieldOptions != nil`
3. The chained `Get` loads relationships per the FieldOptions tree

So the mutation resolver pattern from §6.2 works for any mutation result selection — no special-casing needed.

### 9.3 Delete and Restore

```go
func (r *mutationResolver) DeleteProduct(ctx context.Context, id string) (bool, error) {
    err := r.client.Products.HardDelete(ctx, id)
    if errors.Is(err, sqlgen.ErrNotFound) { return false, nil }
    return err == nil, err
}

// Generated when the table has a soft-delete column AND `operations` includes soft delete.
func (r *mutationResolver) SoftDeleteProduct(ctx context.Context, id string) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.SoftDelete(ctx, id, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}

// Generated alongside SoftDelete — restoring an entity that's currently soft-deleted.
func (r *mutationResolver) RestoreProduct(ctx context.Context, id string) (*models.Product, error) {
    fo := productFieldOptionsFromContext(ctx)
    return r.client.Products.Restore(ctx, id, func(o *models.CallOptions[models.ProductFieldOptions]) {
        o.FieldOptions = fo
        for _, f := range callOptionsFromHTTP(ctx) { f(o) }
    })
}
```

Naming rules driven by `operations` config:

| Operations include | Generated mutations |
|--------------------|---------------------|
| hard delete only | `delete<Table>(id): Boolean!` |
| soft delete only | `delete<Table>(id): <Table>` (soft delete, returns the soft-deleted row, nullable), `restore<Table>(id): <Table>` (nullable) |
| both | `hardDelete<Table>(id): Boolean!`, `softDelete<Table>(id): <Table>` (nullable), `restore<Table>(id): <Table>` (nullable) — no bare `delete<Table>` is generated, forcing clients to choose explicitly |

Returning the entity from soft delete and restore matches Hasura's convention and lets the client use a `FieldOptions` selection on the result (e.g., to refresh related rows in cache).

---

## 10. Error Mapping

| Runtime Error | GraphQL Error | `extensions.code` |
|---------------|---------------|-------------------|
| `database.ErrNotFound` | `gqlerror.Error{Message: "not found"}` | `NOT_FOUND` |
| `*database.ConstraintError` with `Type: ConstraintUnique` | `gqlerror.Error{Message: "duplicate"}` | `CONFLICT` |
| `*database.ConstraintError` with `Type: ConstraintForeignKey` | `gqlerror.Error{Message: "fk constraint"}` | `BAD_REFERENCE` |
| `*database.ConstraintError` with `Type: ConstraintCheck` | `gqlerror.Error{Message: "check failed"}` | `INVALID_INPUT` |
| `*database.ConstraintError` with `Type: ConstraintNotNull` | `gqlerror.Error{Message: "missing required"}` | `INVALID_INPUT` |
| `tenancy.ErrMissing` | `gqlerror.Error{Message: "tenant required"}` | `UNAUTHENTICATED` |
| `tenancy.ErrMismatch` | `gqlerror.Error{Message: "tenant mismatch"}` | `FORBIDDEN` |
| Other | `gqlerror.Error{Message: err.Error()}` | `INTERNAL` |

Implementation: a single `mapErrorToGQL(err error) error` helper in `graph/errors_gen.go` wraps every resolver return. Bare sentinels match via `errors.Is`; constraint violations match via a single `errors.As(err, *database.ConstraintError)` extraction + `switch ce.Type` against `database.ConstraintUnique` / `ConstraintForeignKey` / `ConstraintCheck` / `ConstraintNotNull`. `errors.As` walks the wrap chain just like `errors.Is`, so `fmt.Errorf("…: %w", err)` is absorbed transparently. Field paths are filled by gqlgen automatically.

References: PRD §26.5.5, §22.2 (ConstraintError), `database/errors.go` for sentinel definitions.

---

## 11. CallOptions, Auth & Tenancy

### 11.1 HTTP header → CallOptions middleware

PRD §26.11 defines this. Generated middleware extracts `Cache-Control: no-cache`, `X-Skip-Events: true`, `X-Skip-Hooks: true` and stashes a per-request flag struct into ctx under an unexported `callOptionsKey` type. Resolvers consume via `callOptionsFromHTTP[FO any](ctx)` which projects the flags into a typed setter slice for the per-table `CallOptions[<Table>FieldOptions]`.

```go
// graph/middleware_gen.go
type callOptionsKey struct{}

type httpCallOptions struct {
    SkipCache  bool
    SkipEvents bool
    SkipHooks  bool
}

func WithCallOptionsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var opts httpCallOptions
        if r.Header.Get("Cache-Control") == "no-cache" {
            opts.SkipCache = true
        }
        if r.Header.Get("X-Skip-Events") == "true" {
            opts.SkipEvents = true
        }
        if r.Header.Get("X-Skip-Hooks") == "true" {
            opts.SkipHooks = true
        }
        ctx := context.WithValue(r.Context(), callOptionsKey{}, opts)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func callOptionsFromHTTP[FO any](ctx context.Context) []func(*models.CallOptions[FO]) {
    opts, ok := ctx.Value(callOptionsKey{}).(httpCallOptions)
    if !ok {
        return nil
    }
    var out []func(*models.CallOptions[FO])
    if opts.SkipCache {
        out = append(out, func(o *models.CallOptions[FO]) { o.SkipCache = true })
    }
    if opts.SkipEvents {
        out = append(out, func(o *models.CallOptions[FO]) { o.SkipEvents = true })
    }
    if opts.SkipHooks {
        out = append(out, func(o *models.CallOptions[FO]) { o.SkipHooks = true })
    }
    return out
}
```

The read side is generic so it can return setters typed for each resolver's specific `CallOptions[<Table>FieldOptions]`. The stash side stores a concrete flag struct because a `[]func(*CallOptions[FO])` slice can't survive `context.Value` round-tripping with the right element type for an unknown future FO. Setter ordering matches the documented header order. The `SkipHooks ⇒ SkipCache + SkipEvents` implication is enforced by the runtime's `resolveCallOptions` in `shared_types_gen.go`, so the middleware records only the explicit flag.

### 11.2 Tenancy

Tenancy is **not** generated by the GraphQL layer. The consumer's `WithTenantResolver` plumbing (PRD §29) runs at the unified-client layer. The GraphQL middleware just needs to set whatever ctx state the resolver expects — typically a JWT-claims extractor in middleware that puts the tenant ID into ctx where `tenancy.Resolver[T]` reads it.

Documented pattern (in the generated `graph/server.go` template):

```go
mux := http.NewServeMux()
mux.Handle("/graphql", chain(
    extractJWTMiddleware,        // consumer-written: pulls tenant from JWT, sets ctx
    WithCallOptionsMiddleware,   // sqlgen-generated
    handler.NewDefaultServer(graph.NewExecutableSchema(...)),
))
```

### 11.3 Authorization

PRD §26.7 captures this. Generated code does not enforce auth. Consumer wires gqlgen directives or middleware. Optional: sqlgen could emit empty directive stubs (`@auth(role: String)`) for known column-level auth conventions, but defer until a real consumer demands it.

---

## 12. Configuration **[SYNC: PRD §26.3]**

```yaml
api:
  enabled: true                          # global opt-in
  graphql:
    enabled: true
    schema_dir: ./graph                  # output dir for *_gen.graphqls
    resolver_dir: ./graph                # output dir for resolver impls + helpers
    package: graph                       # Go package for generated resolver code
    max_depth: 5                         # query nesting depth limit
    max_complexity: 1000                 # gqlgen complexity gate
    field_casing: camel_case             # camel_case | snake_case
    gqlgen_config: ./gqlgen.yml          # path to consumer-owned gqlgen.yml (default: ./gqlgen.yml)
    gqlgen_bin: gqlgen                   # how the wrapper invokes gqlgen; default uses `go run github.com/99designs/gqlgen` from the consumer module
    scalars:                             # additions / overrides beyond the built-in registry
                                         # (registry covers types.JSON, types.DateTime, types.NullDateTime,
                                         # uuid.UUID, decimal.Decimal, time.Time, json.RawMessage)
      EmailAddress:                      # consumer-defined type that already implements MarshalGQL / UnmarshalGQL
        go_type:    example.com/types.Email
        marshaling: method
      CountryCode:                       # consumer wants sqlgen to emit external marshalers for their type
        go_type:    example.com/types.CountryCode
        marshaling: external

tables:
  products:
    api:
      enabled: true                      # default: follows global
      operations: read_only              # excludes mutations
  internal_metrics:
    api:
      enabled: false                     # excluded entirely
```

Existing fields from PRD §26.3 retained verbatim. New additions:
- `api.graphql.package` — explicit package name (default derives from `resolver_dir`)
- `api.graphql.max_complexity` — gqlgen complexity limit (default 1000)
- `api.graphql.gqlgen_config` — path to the consumer-owned `gqlgen.yml` the wrapper merges into (default `./gqlgen.yml`)
- `api.graphql.gqlgen_bin` — invocation form for the gqlgen binary; default `go run github.com/99designs/gqlgen` so the consumer's own `go.mod` pin selects the gqlgen version
- `api.graphql.scalars` — custom scalar Go-type mappings **added to** the built-in registry (a `go_type` or scalar name the registry already owns is a config error, as is two entries sharing one `go_type`; a declaration no column resolves to warns at generate time). Each entry has `go_type`, `marshaling: builtin | method | external`, and — for `external` — `marshaler_package`, the import path of the package holding the consumer's `MarshalX` / `UnmarshalX` free functions. sqlgen emits no marshaler body for these; it wires the `scalar` declaration and the gqlgen binding (FIX-159 — PRD §26.3 / §26.4.1 are authoritative). Scalars a column resolves to are merged into the consumer's gqlgen config at wrapper runtime, never written to the file on disk. Consumer-defined entries in `gqlgen.yml` take precedence on key collision.

Validation: `cmd/sqlgen/config/validate.go` adds `validateAPIConfig` that runs after parse and checks scalar names exist in the parsed schema's used types. The wrapper additionally validates that the merged gqlgen config is well-formed before invoking the gqlgen subprocess.

---

## 13. Dependencies & Module Boundary

| Dependency | Where it lives | Why |
|------------|---------------|-----|
| `github.com/99designs/gqlgen` | Consumer `go.mod` | Build-time tool the consumer invokes (via the sqlgen wrapper or directly) |
| `github.com/vektah/gqlparser/v2` | Consumer `go.mod` (transitive) | gqlgen runtime dep |
| sqlgen runtime (`./`) | Consumer `go.mod` | Already a dep — generated resolvers call into it |
| sqlgen `cmd/sqlgen` | Build-tool only | Generates code; not imported at runtime |

**Critical rule:** `cmd/sqlgen` does **not** import gqlgen. It emits files that reference gqlgen APIs by literal string in templates, and the `sqlgen graphql gen` wrapper invokes the gqlgen binary as a subprocess (resolved from the consumer module via `go run github.com/99designs/gqlgen`). Compile-time validation happens in the consumer's `go build`, after `gqlgen generate` has run to produce the resolver interfaces the generated impls satisfy.

**Wrapper data flow:**
1. Consumer runs `sqlgen graphql gen` (typically wired into `//go:generate`).
2. Wrapper reads `gqlgen_config` (consumer-owned `gqlgen.yml`) into a YAML AST.
3. Wrapper merges sqlgen-owned entries: `models:` for managed tables, `schema:` glob for `graph/*.graphqls`, `scalars:` from `api.graphql.scalars`, scalars derived from column types. Consumer-authored keys win on collision.
4. Wrapper writes the merged config to a temp file and runs the gqlgen binary against it via `os/exec`.
5. Temp file is removed on success. The on-disk `gqlgen.yml` is untouched.

This mirrors the existing pattern: `cmd/sqlgen/gen/templates/cache.go.tmpl` emits `cache.GetAs[*Product]` calls without importing the consumer's `models` package.

---

## 14. Why No Dataloader for Generated Code

A dataloader batches per-item fetches across sibling resolvers in the same tick. sqlgen's API design eliminates the cases where this is needed within the generated surface:

| GraphQL pattern | sqlgen handling | Dataloader needed? |
|----------------|-----------------|:------------------:|
| `product(id: 1) { name }` | `Get` with `FieldOptions{Name: true}` | No |
| `product(id: 1) { company { name } }` | `Get` with `FieldOptions{Company: {...}}` — JOIN'd | No |
| `products { reviews { author { name } } }` | `GetMany` + 1 batched `IN` for reviews + 1 batched `IN` for authors | No |
| `createProduct(input) { reviews { count } }` | `Create` chains `Get` internally with FieldOptions | No |
| Custom resolver doing per-item DB call | Consumer-written code outside sqlgen | **Yes (consumer's choice)** |

**Documented contract:** "sqlgen-generated resolvers issue `1 + count(O2M selected) + 2 * count(M2M selected)` queries per request, regardless of result set size — each M2M edge is a junction query + an entities query." Same as PRD §25.1.

If the recursive walker has a bug and a relationship goes un-prefetched, the child resolver returns nil — it does **not** lazy-load. This fails fast and is caught by the per-table walker test (§7.4).

---

## 15. Multi-Schema Namespacing

sqlgen already disambiguates same-named tables across schemas (e.g., `public.users` → `User`, `audit.users` → `AuditUser` per FIX-027). GraphQL inherits this:

| SQL | Go struct | GraphQL type |
|-----|-----------|--------------|
| `public.users` | `User` | `User` |
| `audit.users` | `AuditUser` | `AuditUser` |
| `public.products` | `Product` | `Product` |

GraphQL has no concept of schemas — disambiguation happens at the type-name level, identical to Go. Query field naming follows the same prefix:

```graphql
type Query {
  user(id: ID!): User
  auditUser(id: ID!): AuditUser
  users(...): UserConnection!
  auditUsers(...): AuditUserConnection!
}
```

References: FIX-027 (postgres multi-schema example), `cmd/sqlgen/gen/context_table.go::buildStructName`.

---

## 16. Per-Table API Configuration **[SYNC: PRD §26.10]**

| Config | Effect on GraphQL |
|--------|-------------------|
| `api.enabled: false` (table-level) | Type and all queries/mutations excluded from schema |
| `operations: read_only` | Type kept; only Query fields generated, no Mutations |
| `operations: read_write_no_delete` | Mutations exclude `delete*` |
| `operations: <preset name>` | Per-preset (PRD §4.6) |
| `tenancy.required: true` | Tenant resolver runs server-side; failure → `UNAUTHENTICATED` GraphQL error |

The per-table `operations` config already drives client code generation. The GraphQL generator reads the same resolved per-table operations and emits matching schema fields + resolver methods. No new config field needed for filtering.

---

## 17. Testing Strategy

### 17.1 Generator unit tests (`cmd/sqlgen/gen/`)

- `TestGraphQLSchema_emitsExpectedTypesPerTable` — table-driven, asserts emitted `.graphqls` for known schemas
- `TestGraphQLSchema_respectsFieldCasing` — camelCase vs snake_case
- `TestGraphQLSchema_excludesDisabledOperations` — `read_only` → no Mutations
- `TestGraphQLResolver_emitsConcreteImpls` — golden-style template output check
- `TestFieldOptionsWalker_buildsRecursiveTree` — given a fixture selection set, verify the walker output matches the expected `FieldOptions`
- `TestFilterTranslator_handlesCompoundLogic` — and/or/not nesting

### 17.2 E2E example module

New example: `cmd/sqlgen/testdata/examples/graphql/`. Same shape as the `tenancy` and `events` examples — a self-contained Go module with sqlgen.yml, schema.sql, generated artifacts, and a `tests/` dir with `-race` integration tests.

The example's `tests/` would:
- Spin up a real `gqlgen` server
- Issue actual GraphQL queries
- Verify response shape, error codes, and DB query counts (using a counting `Querier` wrapper)

Ensures the end-to-end pipeline works with a real gqlgen run.

### 17.3 Walker-completeness lint

A codegen-time check: every column / relationship in `parser.Schema` must have a corresponding case in the generated walker. Fails the generate step if any are missing — prevents the §7.4 walker-bug class entirely.

### 17.4 Goldens

`cmd/sqlgen/testdata/examples/graphql/expected/` holds golden `*_gen.graphqls`, `resolvers_gen.go`, `field_options_gen.go`, etc. Regenerated via `make update-golden-e2e`. Same workflow as existing examples.

---

## 18. Implementation Phases (proposed sub-items for `phase-16.md`)

### Phase 16.1 — Schema generation
- Schema-walker producing `.graphqls`
- SQL → GraphQL type mapping per §5
- field_casing + custom scalar config
- **Runtime-module change (stdlib-only):** add `MarshalGQL(io.Writer)` and `UnmarshalGQL(any) error` methods to `types.JSON`, `types.DateTime`, `types.NullDateTime`. Test in the runtime test suite — JSON round-trip (object, array, scalar, null shapes for `types.JSON`), nullable round-trip, error paths
- Built-in scalar registry: known types and marshaling mode (`types.JSON` / `types.DateTime` / `types.NullDateTime` → method-based via runtime methods, no codegen output; `uuid.UUID` → external, `decimal.Decimal` → external, `json.RawMessage` → external (binds to the same `JSON` scalar as `types.JSON`), `time.Time` → gqlgen-bundled, primitives → spec built-in)
- Emit-on-use scalar declarations: when a column's Go binding resolves to a sqlgen-shipped scalar type (`JSON`, `DateTime`), include `scalar X` in the generated `.graphqls` exactly once per scalar
- Emit `graph/scalars_gen.go` with `MarshalX` / `UnmarshalX` for every category-4 scalar in use (skipping category-3 sqlgen-shipped types)
- Codegen error for unknown scalars in `api.graphql.scalars` that lack a `marshaling:` declaration
- Generator unit tests (per scalar: round-trip via the emitted Marshal/Unmarshal or the runtime method, and an integration test that loads the merged gqlgen.yml and confirms gqlgen accepts the bindings)

### Phase 16.2 — gqlgen wrapper subcommand
- `sqlgen graphql init` — one-shot scaffold of `gqlgen.yml` (only if missing)
- `sqlgen graphql gen` — read consumer-owned `gqlgen.yml`, merge sqlgen-owned `models:` / `schema:` / `scalars:` in memory, write temp config, subprocess-invoke gqlgen via `go run`
- YAML-AST merge with consumer-wins precedence on key collision
- Wrapper unit tests: greenfield init, layered-on-existing merge, scalar collision precedence, malformed-config error path
- No new Go dependency on gqlgen in `cmd/sqlgen` (subprocess only)

### Phase 16.3 — Resolver scaffolding
- gqlgen interface satisfaction (Query, Mutation per table)
- **Curated surface per §6.0** — emit only the resolvers in scope: `<table>`, `<table>s` (Connection), `<table>List` (ListResult envelope), `create<Table>`, `create<Table>s`, `update<Table>`, `update<Table>s`, `upsert<Table>`, `delete<Table>`, `softDelete<Table>`, `restore<Table>`. Do not emit resolvers for `Exists`, `Count`, `GetMany`, or `Find`. Per-table emit a `<Type>ListResult` envelope (items, totalCount, offset, limit).
- Per-table gating: each resolver only emitted when the table's resolved `operations` config includes the corresponding op AND prerequisites are met (e.g., `softDelete<Table>` requires a soft-delete column, `upsert<Table>` requires a PK or unique constraint)
- Concrete resolver impls calling unified client
- Pass-through `FieldOptions` from translator (placeholder until 16.4)
- Mutation chaining via existing `Create`/`Update`/`CreateMany`/`UpdateMany` FieldOptions support
- `_inc` / `_dec` input-operator dispatch in update translator (see §9.1) — `_set` then `Increment` then `Decrement`, with codegen rejection of conflicting `_set` + `_inc` and runtime rejection of `_inc` + `_dec` on the same column
- Resolver scaffolding unit tests covering: surface inclusion/exclusion under each operations preset, `_inc` / `_dec` translator dispatch, soft-vs-hard delete naming rules per §9.3

### Phase 16.4 — Field selection translator
- Recursive `fieldOptionsFromCollected` walker per table
- Connection unwrapping helper
- Walker-completeness lint
- Walker unit tests with fixture queries

### Phase 16.5 — Filter / sort / pagination translators
- Per-comparator family translator (shared, not per-table)
- Per-table `translateXFilter` driving comparators
- Sort translator + sort-field enum
- Pagination pass-through

### Phase 16.6 — Error mapping + middleware
- `mapErrorToGQL` with sentinel coverage (§10)
- `WithCallOptionsMiddleware` HTTP wrapper
- `callOptionsFromHTTP` resolver helper

### Phase 16.7 — Multi-schema + per-table config
- AuditX → `AuditX` GraphQL type
- `api.enabled: false` exclusion
- `operations` preset filtering

### Phase 16.8 — E2E example
- New `cmd/sqlgen/testdata/examples/graphql/` module
- Real gqlgen server (booted via `sqlgen graphql gen`) + integration tests
- Query-count assertions via counting Querier
- Golden files
- Layered-mode regression test: pre-seed a `gqlgen.yml` with custom directives + extra models, run the wrapper, assert the consumer keys survive and sqlgen keys are merged in

### Phase 16.9 — Documentation + PRD sync
- Sync this doc back into PRD §26
- Update `IMPLEMENTATION_ORDER.md` to remove "Deferred" status
- Update `STATUS.md`

---

## 19. Open Questions / Decisions Needed

1. **Custom scalar handling.** ~~Default scalar mappings (`DateTime`, `UUID`, `Decimal`, `JSON`) — emit as gqlgen scalar config, or require consumer to write them in `graph/scalars.go`?~~ **Resolved (option C — wrapper):** sqlgen carries default scalar mappings in `api.graphql.scalars`; the wrapper merges them into the consumer's `gqlgen.yml` at invocation time, with consumer-defined entries winning on key collision. The on-disk `gqlgen.yml` is consumer-owned. See §4 / §12 / §13.
2. **Operation preset ⇒ schema field naming.** ~~When soft delete is enabled but hard delete is not, should the mutation be `deleteProduct` or `softDeleteProduct`?~~ **Resolved:** when only one delete variant is enabled, name it `deleteProduct` (its semantics are unambiguous from the operations config). When both are enabled, generate `softDeleteProduct` + `hardDeleteProduct` (no bare `deleteProduct` — forces clients to be explicit). `restoreProduct` is generated whenever soft delete is enabled. See §9.3 for the table form.
3. **Subscriptions.** Out of scope for Phase 16. The pieces are in place — PRD §28 already defines a typed event system, and the existing distributed event-bus interface (consumer-pluggable transport) covers the multi-instance case. A GraphQL subscription resolver would be a thin wrapper over `FromEventSubscriber` reusing the same `FieldOptions` translation as queries. Open design points if/when this gets picked up: WebSocket transport scaffolding in `graph/server.go`, connection-init auth context (tenant resolver fires once at connect), backpressure policy on slow consumers, optional sequence-cursor / replay semantics. **Deferred** until consumer demand surfaces — no in-memory-only blocker, no transport rebuild needed; the work is purely the GraphQL-side wiring.
4. **Pagination on O2M / M2M relationship fields.** ~~Relay says relationship fields can themselves accept pagination args. sqlgen's relationship loaders do not currently support per-relationship pagination — they load all related rows.~~ **Resolved (skip wrapper / no per-rel args):** relationships emit as `[Type!]!` with no pagination args. The bulk-IN loader populates the full set per request, preserving the §14 query-count guarantee. Root-level pagination is fully supported (Connection + ListResult); per-relationship pagination is deferred to a future phase that ships lateral-join / window-function SQL for "top N per parent." Schema today is intentionally narrower than long-term; the choice is YAGNI over future-proofing.
5. **Computed / view fields.** ~~Views work the same as tables (read-only).~~ **Resolved:** views are treated identically to tables, gated to read-only (no Mutation fields). Computed columns (`@type` + `@expr`) reuse the existing view pipeline — no special handling.
6. **Nullability of mutation results.** ~~`createProduct` returns `Product!` (non-nullable) on success, errors on failure. But what about mutations that conditionally return nothing (e.g., `softDeleteProduct` returning the soft-deleted entity vs. void)?~~ **Resolved:** create / update / upsert / createMany / updateMany return non-nullable (`Product!` / `[Product!]!`) — failures surface as GraphQL errors, never null results. Delete-class mutations (`softDelete<Table>`, `restore<Table>`) return nullable `Product` so the client can distinguish "not found" (null) from "internal error" (GraphQL error). Hard delete returns `Boolean!` (true on delete, false on missing).

---

## 20. References

### Internal
- **PRD §26** (`API Generation`) — current spec; this doc supersedes during design phase
- **PRD §25.1** (`Query Count Guarantee`) — performance contract that drives the no-dataloader decision
- **PRD §17** (`Omittable`) — input type semantics
- **PRD §13** (`Errors`) — sentinel error definitions for §10 mapping
- **PRD §28** (`Events`) — possible subscription source (deferred)
- **PRD §29** (`Tenancy`) — middleware integration pattern in §11.2
- **`docs/tracker/IMPLEMENTATION_ORDER.md`** §D.1 — original deferred-feature note
- **`guidelines/ARCHITECTURE.md`** — module boundary rules (§13)
- **`guidelines/TEMPLATES.md`** — codegen template authoring
- **`guidelines/GO.md`** — naming and error conventions
- **`cmd/sqlgen/testdata/examples/postgres/models/models_gen.go:317`** — Create signature with FieldOptions
- **`cmd/sqlgen/testdata/examples/tenancy/`** — example module shape to mirror in §17.2
- **FIX-027** — multi-schema struct disambiguation (§15)
- **FIX-056** — event hook per-entity input (relevant if subscriptions are pursued)

### External
- gqlgen project: https://github.com/99designs/gqlgen
- gqlgen field collection: https://gqlgen.com/reference/field-collection/
- GraphQL spec (October 2021): https://spec.graphql.org/October2021/
- Relay Connection spec: https://relay.dev/graphql/connections.htm
- gqlparser: https://github.com/vektah/gqlparser
