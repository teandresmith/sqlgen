# SQLGen

Generate a complete, type-safe Go data layer — and the services around it — from your SQL schema.

SQLGen reads your schema (from `.sql` files, a live database, or both) and generates idiomatic Go:
structs, a typed client with up to 28 methods per entity, filters, pagination, relationship loading, and
nested writes that create, update or upsert a row together with its related rows.
From that same schema it can also generate a **GraphQL API**, an **MCP server** for AI agents, a
**cache layer** with invalidation, a **mutation event stream**, and **enforced multi-tenancy** —
each opt-in, none of them required.

No ORM, no reflection, no `interface{}` round-trips. Everything is generated Go you can read.

## What it generates

| | |
|---|---|
| **Schema in** | PostgreSQL · MySQL · SQLite — from SQL files, live introspection, or both. Enums, composite types, domains, views, and PostgreSQL materialized views |
| **Typed client** | 18–28 methods per entity: CRUD, single and batch upsert, increment, cursor + offset pagination, streaming, typed filters and comparators, field selection, sorting, relationship loading (O2O / O2M / M2M), nested mutations through relationships, transactions, lock modes |
| **Generated services** | GraphQL API (schema + gqlgen resolvers) · MCP server · machine-readable manifest |
| **Cross-cutting** | Caching (in-memory / Redis, invalidation, circuit breaker) · Events (in-memory / NATS + JetStream) · Multi-tenancy enforced in SQL · Column access control · Hooks and middleware · OpenTelemetry metrics |

Core runtime packages depend only on the Go standard library.

## Installation

```bash
# Pre-built binary (recommended) — download from GitHub Releases
# for your platform: linux and macOS (amd64, arm64), windows (amd64;
# Windows on ARM runs it under emulation)

# From source (requires CGO + a C compiler)
go install github.com/teandresmith/sqlgen/cmd/sqlgen@latest
```

The runtime and the optional modules (`cache/redis`, `event/natsbus`, …) are released together; require them at the same version.

## Quick start

```bash
sqlgen init        # write a starter sqlgen.yml
sqlgen generate    # generate code from your schema
sqlgen diff        # verify generated code is up to date (CI-friendly)
```

A minimal `sqlgen.yml`:

```yaml
version: v1

input:
  dialect: postgres
  paths:
    - ./schema.sql

output:
  driver: pgx
  dir: ./db
  package: db
```

See **[Configuration](./docs/configuration.md)** for the full reference.

## From schema to typed Go

Given ordinary DDL:

```sql
CREATE TABLE categories (
    id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL
);

CREATE TABLE products (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    price       NUMERIC(10, 2) NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    category_id UUID NOT NULL REFERENCES categories(id),
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

`sqlgen generate` produces a client where the foreign key becomes a loadable relationship, every
column becomes a typed filter, and you choose which fields to select:

```go
// Load categories with their products — one call, no N+1.
categories, err := client.Categories().GetMany(ctx, &db.GetCategoriesInput{
    Filter: &db.CategoryFilter{
        ID: &comparator.ID{Eq: new(catID)},
    },
}, func(o *db.CallOptions[db.CategoryFieldOptions]) {
    o.FieldOptions = &db.CategoryFieldOptions{
        ID:   true,
        Name: true,
        Products: &db.ProductRelationshipOptions{
            FieldOptions: &db.ProductFieldOptions{
                ID:    true,
                Name:  true,
                Price: true,
            },
        },
    }
})
```

Pagination is typed the same way, with cursor and offset variants:

```go
page, err := client.Products().Paginate(ctx, db.PaginateInput[db.ProductFilter]{
    Filter: &db.ProductFilter{IsActive: &comparator.Bool{Eq: new(true)}},
    Limit:  20,
    Offset: 0,
})
// page.Items, page.TotalCount
```

The full per-entity surface:

```
Get            GetMany        Count         Exists        ExistsWhere
Create         CreateMany     Update        UpdateMany    UpdateWhere
Upsert         UpsertMany     Increment     Paginate      Connection
Stream         HardDelete     HardDeleteMany HardDeleteWhere
SoftDelete     SoftDeleteMany SoftDeleteWhere              (soft-delete tables)
Restore        RestoreMany    RestoreWhere                 (soft-delete tables)
CreateWithRelated  UpdateWithRelated  UpsertWithRelated    (nested mutations, opt-in)
```

Methods the schema cannot support are not generated: a view gets read methods only, and a table
without a conflict target gets no upsert. You choose what the generated API exposes with
`api.operations`, not by turning client methods off.

With `generation.nested_mutations.enabled: true`, a row and its related rows are written in one
transaction:

```go
// Create a category and two products under it; category_id is filled in from the new row.
category, err := client.Categories().CreateWithRelated(ctx, &db.CreateCategoryWithRelatedInput{
    Category: db.CreateCategoryInput{Name: "Tools"},
    Products: &db.CategoryProductsCreateNested{
        Create: []*db.CategoryProductsCreateInput{
            {Name: "Hammer", Price: hammerPrice},
            {Name: "Wrench", Price: wrenchPrice},
        },
    },
})
```

Because `products.category_id` is `NOT NULL`, `create` is the only verb on this edge. A nullable
foreign key also allows `connect`, `disconnect` and `clear`, and an M2M edge links and unlinks rows
through its junction table.

## Dialect support

| Feature | PostgreSQL | MySQL | SQLite |
|---------|:----------:|:-----:|:------:|
| Placeholder style | `$1, $2` | `?` | `?` |
| Identifier quoting | `"double"` | `` `backtick` `` | `"double"` |
| `RETURNING` clause | Yes | No | Yes (3.35+) |
| Upsert | `ON CONFLICT DO UPDATE` | `ON DUPLICATE KEY UPDATE` | `ON CONFLICT DO UPDATE` |
| Enum types | `CREATE TYPE … AS ENUM` | Inline `ENUM(...)` | Not supported |
| Composite types | Yes | No | No |
| Domain types | Yes | No | No |
| Materialized views | Yes | No | No |
| Multi-schema | Yes | Databases only | No |
| Column comments | `COMMENT ON` | Inline `COMMENT` | Not supported |
| Introspection | `pg_catalog` + `information_schema` | `information_schema` | `PRAGMA` |
| Driver | `pgx` or `stdlib` | `stdlib` | `stdlib` |

Where a dialect lacks a capability, sqlgen adapts rather than failing. MySQL has no `RETURNING`,
so an inserted integer PK is retrieved with `LastInsertId()`; because a database-generated UUID is
unretrievable there, UUID primary keys on MySQL default to being generated by the client before
the INSERT.

## Features

**Nested mutations** — opt-in `CreateWithRelated`, `UpdateWithRelated` and `UpsertWithRelated`
methods write a row together with rows on its relationships, in one transaction, on all three
dialects. Each relationship takes a block of `create`, `connect`, `disconnect` and `clear`. Which
verbs a relationship allows comes from the schema: the foreign key's nullability and the
relationship shape decide it. The same methods are exposed on the GraphQL API as
`create<T>WithRelated`, `update<T>WithRelated` and `upsert<T>WithRelated`. See
[`nested_mutations`](./docs/configuration.md#nested_mutations).

**GraphQL API generation** — emits `.graphqls` schema files and concrete gqlgen resolvers wired to
the generated client: filters, sorting, pagination, field-selection translation, mutations, and
error mapping. Per-table opt-in.

**MCP server** — `sqlgen mcp` serves your generated surface to AI agents over the Model Context
Protocol, so an agent can answer "what's available on `users`?" without reading 50k tokens of
generated Go.

**Caching** — read-through caching with in-memory and Redis backends, msgpack serialization,
source-driven invalidation, a circuit breaker, and cache metrics. Injected via hooks, so generated
code is unchanged when caching is off.

**Events** — publishes mutation lifecycle events through an in-memory bus or NATS, with JetStream
durability, durable consumers, dead-letter queues, and replay.

**Multi-tenancy** — every generated read and mutation on a tenanted table is structurally incapable
of crossing a tenant boundary. Enforcement is in the SQL, not in a convention.

**Column access control** — classify a column so its value is withheld from external surfaces (the
generated API, event payloads, the manifest) while the Go client keeps full access.

**Manifest** — an optional machine-readable description of the generated surface, with
[`sqlgen manifest validate` and `diff`](./docs/cli-manifest.md) for CI and PR review.

**Observability** — OpenTelemetry metrics, structured logging, and hooks for tracing.

## Documentation

| Document | Purpose |
|----------|---------|
| [Configuration](./docs/configuration.md) | Full `sqlgen.yml` reference, including editor/IDE setup |
| [`sqlgen manifest` CLI](./docs/cli-manifest.md) | Manifest validate / diff tooling |
| [Examples](./cmd/sqlgen/testdata/examples/) | Complete, tested projects for every dialect and feature |

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[MIT](./LICENSE)
