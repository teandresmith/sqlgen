# E2E Example Projects

Each subdirectory is a self-contained end-to-end test scenario. The test harness
(`e2e_test.go`) automatically discovers these directories and runs them.

## Directory Structure

```
examples/
  postgres/
    sqlgen.yml          # Config file (paths/output are relative to this dir)
    schema.sql          # DDL schema (public + audit schemas)
    views/              # View annotation files — parsed via input.views
      product_summary.sql       # @pk, @type, @nullable directives
      category_stats.sql        # No @pk (read-only aggregate view)
    expected/           # Golden files — committed, compared on every test run
      models_gen.go
      enums_gen.go
      views_gen.go
      ...
    tests/              # Runtime tests — exercise generated code against real DB
      main_test.go              # TestMain, newClient, shared helpers
      crud_test.go              # Single-entity CRUD (Create, Get, Update, Delete, Exists)
      batch_test.go             # Batch/filter ops (CreateMany, UpdateMany, *Where, Count)
      relationship_test.go      # O2O, O2M, M2M relationship loading
      pagination_test.go        # Offset and cursor pagination
      soft_delete_test.go       # SoftDelete, Restore, filter override, *Where
      filter_test.go            # Comparator filtering (postgres-specific)
      types_test.go             # Enum, array, JSONB, domain, composite (postgres-specific)
      multi_schema_test.go      # audit.users, audit.events (postgres-specific)
      qualified_relationship_test.go # Schema-qualified relationship `table:` into public.notes / audit.notes
      composite_pk_test.go      # 2-column and 3-column composite PKs
      views_test.go             # Read-only view clients (@pk and no-@pk views)
      type_overrides_test.go    # Table-level type overrides
```

## Examples

| Directory | Dialect | Driver | Purpose |
|-----------|---------|--------|---------|
| `postgres/` | PostgreSQL | pgx | Full coverage with pgx batch pipelining |
| `postgres_stdlib/` | PostgreSQL | stdlib (`lib/pq`) | A subset of `postgres/`'s schema, validates stdlib driver path (sequential batch exec, `pq.Array` for arrays) |
| `mysql/` | MySQL | stdlib (`go-sql-driver/mysql`) | MySQL-specific types (SET, enums, blob family, unsigned widths), dialect differences. Carries a GraphQL surface — it is where a `SET` column compiles into a real gqlgen schema. Turns on nested mutations, so it covers the write path without `RETURNING` (`tests/nested_*_test.go`) |
| `sqlite/` | SQLite | stdlib (`modernc.org/sqlite`) | Type affinity coverage, in-memory DB. Carries a GraphQL surface — it is where SQLite's **dialect-default** `types.DateTime` / `types.NullDateTime` compile into a real gqlgen schema, alongside its BLOB primary key and `file_per_table` layout |
| `graphql/` | PostgreSQL | pgx | The GraphQL API surface end to end through a booted gqlgen server: every relationship kind, the scalar registry, views, cache and tenancy. It is the main nested-mutations example: all four write shapes, a polymorphic `discriminator:` edge and the `create<T>WithRelated` / `update<T>WithRelated` / `upsert<T>WithRelated` mutations (`tests/nested_mutations_*_test.go`). See its [README](graphql/README.md) |
| `graphql_null_wrappers/` | PostgreSQL | pgx | Module-wide `overrides.use_pointers: false` (the seven `database/sql` wrappers on relationship loaders, soft delete and filters) plus the gqlgen-bundled `Int64` scalar route. See its [README](graphql_null_wrappers/README.md) |
| `graphql_top_level/` | PostgreSQL | pgx | A root-relative `schema_dir: graph`, so the graph package lands at a top-level `./graph` beside `./models` (§26.5.8). See its [README](graphql_top_level/README.md) |
| `cache/` | SQLite | stdlib (`modernc.org/sqlite`) | The cache layer: read-through, hydration, the circuit breaker, invalidation on writes, including `*Where` and restore, transactions and views |
| `events/` | SQLite | stdlib (`modernc.org/sqlite`) | Mutation events: batch and `*Where` events, per-table opt-out, metadata, column-access redaction and deferred events inside a transaction |
| `tenancy/` | SQLite | stdlib (`modernc.org/sqlite`) | The full multi-tenancy suite: isolation, nil-tenant behavior, composite PKs, relationships, cache and event interplay |
| `tenancy_mysql/` | MySQL | stdlib (`go-sql-driver/mysql`) | The tenant-capture paths with no `RETURNING`: pre-reads, the batched `*Many` pre-read and the upsert post-read |
| `tenancy_postgres/` | PostgreSQL | pgx | `RETURNING`-based tenant capture on pgx, including the `UpdateMany` `SendBatch` path |

The dialect examples share the same test file structure. `postgres_stdlib/` runs a subset
of `postgres/`'s schema and test suite: it omits the `assets`, `documents`,
`asset_document_links`, `counters` and `rate_limits` tables and the tests that
use them. Beyond that, the differences are the driver setup in `main_test.go`
and the generated batch code (no `pgx.Batch`).

## Conventions

1. **`sqlgen.yml`** must exist in the example root. Its `input.paths`,
   `input.views`, and `output.dir` use relative paths (relative to the example directory).

2. **`schema.sql`** (or a `schema/` directory) holds the DDL that the parser reads.

3. **`views/`** holds view annotation files (one per view). Each file contains
   annotation comments (`@pk`, `@type`, `@nullable`) followed by a `CREATE VIEW`
   statement. These files are listed in `input.views` for code generation, and
   also executed by `TestMain` to create the views in the test database.

4. **`expected/`** contains the golden output. Running `make update-golden-e2e`
   regenerates these files. Any difference between generated output and
   `expected/` fails the test.

5. **`tests/`** contains Go test files that import the generated code
   and run operations against a real database. These are separate from the
   golden file tests — they validate runtime behavior, not generation output.

6. **`main_test.go`** contains `TestMain` (container/DB setup), `newClient`,
   and any shared helpers. It applies `schema.sql` and all view files from
   `views/`. All other test files in `tests/` share the connection established here.

7. **One file per feature area.** Tests are split by concern (CRUD, batch,
   relationships, pagination, soft delete, views, etc.) for readability.
   New features get a new file rather than appending to an existing one.

## Test Phases

For each example, the harness runs:

1. **Generate** — runs `sqlgen generate --config sqlgen.yml` via `cli.NewRootCmd()`
2. **Compare** — diffs generated output against `expected/` (any difference fails)

Generation itself validates compilation via `goimports`. Runtime tests in
`tests/` compile the generated code by importing it.

## Updating Golden Files

When templates change intentionally:

```bash
make update-golden-e2e
```

Review the diff, then commit the updated `expected/` directories.

## Skipping

All E2E tests are skipped when running `go test -short ./...`.
