# Getting started

This guide walks from a SQL schema to a generated client and shows what that client looks like in
use. For every config field, see [Configuration](./configuration.md). For a complete application
built on a generated client, see [sqlgen-example](https://github.com/teandresmith/sqlgen-example).

## Install

Download a pre-built binary for your platform from
[GitHub Releases](https://github.com/teandresmith/sqlgen/releases): linux and macOS (amd64, arm64),
or windows (amd64; Windows on ARM runs it under emulation).

To build from source instead (requires CGO and a C compiler):

```bash
go install github.com/teandresmith/sqlgen/cmd/sqlgen@latest
```

The runtime and the optional modules (`cache/redis`, `event/natsbus`, …) are released together;
require them at the same version.

## Configure and generate

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

`input` can also introspect a live database, or combine both; see
[Configuration](./configuration.md).

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
column becomes a typed filter, and you choose which fields to select.

### Reading with relationships

```go
// Load categories with their products — one call, no N+1.
// catID is a string: UUID columns filter through comparator.ID.
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

### Pagination

Pagination is typed the same way, with offset (`Paginate`) and cursor (`Connection`) variants:

```go
page, err := client.Products().Paginate(ctx, db.PaginateInput[db.ProductFilter]{
    Filter: &db.ProductFilter{IsActive: &comparator.Bool{Eq: new(true)}},
    Limit:  20,
    Offset: 0,
})
// page.Items, page.TotalCount, page.HasMore
```

### Nested mutations

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
through its junction table. See [`nested_mutations`](./configuration.md#nested_mutations).

## The per-entity client

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

## Next steps

- [Configuration](./configuration.md): the full `sqlgen.yml` reference, including GraphQL, MCP,
  caching, events and multi-tenancy.
- [Dialect support](./dialects.md): how PostgreSQL, MySQL and SQLite differ.
- [Database round trips](./round-trips.md): how many statements each client method sends, and
  how many of them wait on each other, measured on every dialect.
- [sqlgen-example](https://github.com/teandresmith/sqlgen-example): a multi-tenant GraphQL API
  wired the recommended way, with cache, events, hooks and metrics.
- [Per-feature test projects](../cmd/sqlgen/testdata/examples/): complete, tested projects for
  every dialect and feature.
