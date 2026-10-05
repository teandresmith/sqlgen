# Configuration

SQLGen is configured with a single `sqlgen.yml` in your project root. Generate a starter with:

```bash
sqlgen init
```

Validate a config without generating:

```bash
sqlgen validate
```

All relative paths in the file resolve against the directory containing `sqlgen.yml`.

## Editor support

sqlgen ships a JSON Schema for `sqlgen.yml`, which gives you key completion,
hover documentation, and inline errors for misspelled keys and invalid values.

`sqlgen init` wires it up automatically — the config it writes starts with:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/teandresmith/sqlgen/main/cmd/sqlgen/config/schema/v1.json
```

To enable it on an existing config, add that line at the top of your
`sqlgen.yml`. It is read by the VS Code YAML extension (Red Hat), JetBrains
IDEs, and any editor running `yaml-language-server` — no plugin or registry
setup beyond the YAML extension itself.

> The schema checks structure, key names, and closed value sets. It cannot
> express sqlgen's cross-field rules — that `pgx` requires `dialect: postgres`,
> that `connection` is required when `source: database`, that a soft-delete
> column's SQL type must match its declared `type`. A file with no editor
> warnings can still fail `sqlgen validate`, so keep running it in CI.

## Contents

- [Minimal config](#minimal-config) · [Top level](#top-level)
- [`input`](#input) · [`connection`](#connection) · [`introspect`](#introspect) · [`exclude_tables`](#exclude_tables)
- [`output`](#output) · [`generation`](#generation) · [`overrides`](#overrides)
- [`tables`](#tables-per-table) · [`views`](#views) · [`enums`](#enums) · [`extras`](#extras)
- [Optional subsystems](#optional-subsystems) · [Worked examples](#worked-examples)

## Minimal config

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

## Top level

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `version` | string | `v1` | Config schema version, for forward compatibility. |
| `input` | object | see below | Where the schema comes from. |
| `output` | object | see below | Where generated code goes. |
| `generation` | object | see below | Behavioral settings: limits, soft delete, nested mutations, the manifest. |
| `overrides` | object | `{}` | Global SQL-to-Go type mapping. |
| `exclude_tables` | []string | `[]` | Glob patterns for tables to drop from generation entirely. |
| `tables` | map | `{}` | Per-table customization, keyed by table name. |
| `views` | map | `{}` | Per-view customization, keyed by view name. |
| `enums` | map | `{}` | Per-enum customization, keyed by SQL enum name. |
| `extras` | map | `{}` | Custom struct definitions, e.g. for JSONB columns. |
| `api` | object | disabled | GraphQL API generation. Opt-in. |
| `cache` | object | disabled | Caching. Opt-in. |
| `events` | object | disabled | Mutation lifecycle events. Opt-in. |
| `tenancy` | object | disabled | Multi-tenant scoping. Opt-in. |

## `input`

| Field | Type | Default | Valid values | Description |
|-------|------|---------|--------------|-------------|
| `dialect` | string | `postgres` | `postgres`, `mysql`, `sqlite` | SQL dialect used for parsing. |
| `source` | string | `files` | `files`, `database`, `both` | Where the schema is read from. |
| `paths` | []string | `["."]` | — | SQL files or directories to parse. Used when `source` is `files` or `both`. |
| `views` | []string | `[]` | — | Paths to view annotation files or directories. Parsed only as views, never as DDL. |
| `schema` | string | `*` | — | PostgreSQL only: the schema an unqualified table name or `REFERENCES` target resolves to. `*` resolves to `public`. It does not filter which schemas are parsed; `introspect.schemas` filters introspection. Ignored with a warning on MySQL/SQLite. |
| `parse_mode` | string | `strict` | `strict`, `merge` | How duplicate table definitions across files are handled. |
| `connection` | object | — | — | Required when `source` is `database` or `both`. |
| `introspect` | object | — | — | Schema/table filtering for introspection. |

> `dialect` is `postgres` — not `postgresql`. An invalid value fails `sqlgen validate`.

### `connection`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | Full connection URL. Takes precedence over the individual fields below. |
| `host` | string | — | Required for postgres/mysql when `url` is empty. |
| `port` | int | — | Required for postgres/mysql when `url` is empty. |
| `user` | string | — | Required for postgres/mysql when `url` is empty. |
| `password` | string | — | See the environment overrides below. |
| `database` | string | — | Database name, or the file path for SQLite. Required when `url` is empty. |
| `ssl_mode` | string | `disable` (pg), `false` (mysql) | TLS/SSL mode. |

Two environment variables override the file, and take highest precedence — keep credentials out
of `sqlgen.yml`:

| Variable | Overrides |
|----------|-----------|
| `SQLGEN_DB_URL` | `connection.url` **and** all individual fields |
| `SQLGEN_DB_PASSWORD` | `connection.password` |

### `introspect`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schemas` | []string | all | Schemas to include. Empty means all non-system schemas. |
| `exclude_schemas` | []string | `[]` | Schemas to exclude. Applied after inclusion. |
| `tables` | []string | all | Tables to include. Empty means all in the selected schemas. |

### `exclude_tables`

A top-level field that filters tables regardless of input source — files and introspection alike.
`*` matches any sequence, `?` matches a single character; patterns are case-sensitive. A matched
table is dropped from generation entirely.

```yaml
exclude_tables:
  - temp_*
  - _internal_*
  - report_acl
```

## `output`

| Field | Type | Default | Valid values | Description |
|-------|------|---------|--------------|-------------|
| `driver` | string | `pgx` | `pgx`, `stdlib` | Go database driver the generated code imports. |
| `dir` | string | `.` | — | Default output directory for all artifacts. |
| `package` | string | derived from `dir` | — | Default Go package name. |
| `layout` | string | `single_file` | `single_file`, `file_per_table` | File layout strategy for tables. |
| `enums` | object | — | — | Override output location for enums (`file`, `package`). |
| `types` | object | — | — | Override output location for composite/domain types (`file`, `package`). |
| `client` | object | — | — | Unified client settings (`name` — default `Client`, `file` — default `client_gen.go`). |

> `pgx` is only compatible with `dialect: postgres`. Use `stdlib` for MySQL and SQLite, and for
> PostgreSQL via `database/sql`.

## `generation`

Global defaults controlling what is generated and how it behaves at runtime. Six of these can
be overridden per-table under `tables.<table>` — `query_limit`, `batch_size`, `page_size`,
`cursor_keys`, `exclude_columns` and `strict_updates`. Resolution order for those is
table value → global value → built-in default. The rest are global-only.

### Runtime defaults

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `query_limit` | int | `1000` | Default `LIMIT` for `GetMany` when none is given. |
| `batch_size` | int | `200` | Rows per batch in `CreateMany` / `UpdateMany` / `DeleteMany`. |
| `page_size` | int | `100` | Default page size for pagination. |
| `cursor_keys` | []string | `["id"]` | Columns used to encode keyset-pagination cursors. |
| `uuid_version` | string | `v4` | UUID strategy for app-generated UUID PKs. `v4` random, `v7` time-ordered. |
| `strict_updates` | bool | `true` | When true, `Update` and `Increment` return `ErrNotFound` for a missing PK. When false they are idempotent and return `nil`. |
| `exclude_columns` | []string | `[]` | Columns excluded across all tables. Merged with any per-table list. |

### Soft delete

Soft delete activates when a configured column is detected on a table. Declare the columns you
use under `generation.soft_delete_columns`:

```yaml
generation:
  soft_delete_columns:
    - name: deleted_at
      type: timestamp
    - name: is_deleted
      type: bool
```

Detection runs in priority order — `deleted_at`, `deleted_datetime`, `is_deleted`, `deleted_flag`,
`deleted`. Three column types are supported:

| Type | On soft delete | On restore | Compatible SQL types |
|------|----------------|------------|----------------------|
| `timestamp` | `SET col = CURRENT_TIMESTAMP` | `SET col = NULL` | `timestamp`, `timestamptz`, `datetime` |
| `bool` | `SET col = TRUE` | `SET col = FALSE` | `bool`, `boolean` |
| `integer` | `SET col = 1` | `SET col = 0` | `int`, `integer`, `smallint`, `tinyint` |

A configured soft-delete column with an incompatible type (`varchar`, `uuid`, …) is a validation
error rather than a silent skip.

Tables with a soft-delete column also gain `Restore` / `RestoreMany` / `RestoreWhere`, and reads
exclude soft-deleted rows by default.

### No `operations` setting

The client generates **every method the schema allows**; there is no per-operation switch under
`generation` or `tables.<table>`, and a leftover `operations` key there is a validation error that
points at `api.operations`. The only gates are schema facts (PRD §4.6):

| Schema fact | Effect |
|-------------|--------|
| The entity is a view | Read methods only |
| The table has no primary key | The table is skipped entirely |
| No soft-delete column | No `SoftDelete*` / `Restore*` |
| No incrementable column | No `Increment` |
| No conflict target | No `Upsert` / `UpsertMany` / `UpsertWithRelated` |
| `nested_mutations` off for the family, or no eligible edge | No `…WithRelated` method |

To control what the generated **API** exposes, use the `api.operations` mask (globally, or per
table under `tables.<table>.api.operations`). It is subtractive and takes a preset or explicit keys:

| Preset | Keys on |
|--------|---------|
| `all` | Every key (the default) |
| `read_only` | `get`, `paginate`, `connection` |
| `append_only` | `read_only` plus `create`, `create_many`, `create_with_related` |
| `no_delete` | Every key except `soft_delete`, `hard_delete`, `restore` |
| `no_hard_delete` | Every key except `hard_delete` |

```yaml
api:
  enabled: true
  operations: read_only        # default mask for every table

tables:
  orders:
    api:
      operations:              # replaces the global mask for this table
        preset: all
        hard_delete: false
```

Each key names the client method its surface calls: `get`, `paginate`, `connection`, `create`,
`create_many`, `update`, `update_where`, `upsert`, `soft_delete`, `hard_delete`, `restore`,
`create_with_related`, `update_with_related`, `upsert_with_related`. The client-only keys
(`get_many`, `update_many`, `upsert_many`, `exists`, `count`, `increment`, `stream`) are rejected
in a mask. Each `…_with_related` key requires its base key in the same mask.

### `nested_mutations`

Opt-in and off by default. Controls whether the three `…WithRelated` methods are generated at all;
with `enabled: false` nothing nested is emitted anywhere in the package. It is the only client-side
control over the nested methods.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. |
| `operations` | []string | `[create, update, upsert]` | Which nested families to emit. Closed set. The upsert family also needs a conflict target. |
| `verbs` | []string | `[create, connect, disconnect, clear]` | Which verbs may appear in a nested block. Closed set; per-edge eligibility can only narrow it further. |
| `max_depth` | int | `1` | Nesting depth. `1` is the only accepted value in v1. |

```yaml
generation:
  nested_mutations:
    enabled: true
    verbs: [create, connect]
```

Per-table, `tables.<table>.nested_mutations.relationships` narrows which edges a nested mutation
may reach. Omit it to include every eligible edge; when present, only the listed edges are
reachable and a listed edge that fails an eligibility rule is a hard error rather than a silent
omission. Each entry takes a `name` (required — the relationship's resolved Go field name) and an
optional `allow_reparent`, which lets an O2M `connect` adopt a row already parented elsewhere.

### `manifest`

Opt-in structured description of the generated package — JSON, markdown, and a runtime-embedded
surface on the `Client`. Nothing manifest-related is emitted unless enabled.

```yaml
generation:
  manifest:
    enabled: true
    json_layout: single     # or: per_entity
```

See the [`sqlgen manifest` CLI reference](./cli-manifest.md).

## `overrides`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `use_pointers` | bool | `true` | Use `*T` for nullable columns instead of `sql.Null*`. |
| `types` | map | `{}` | Per-SQL-type override, keyed by SQL type name (`uuid`, `numeric`, …). |

UUID and decimal types have built-in integrations. Use `overrides.types` for anything else, or to
change the behavior of a built-in:

```yaml
overrides:
  types:
    uuid:
      type: uuid.UUID
      import: github.com/google/uuid
      nullable: uuid.NullUUID
    numeric:
      type: decimal.Decimal
      import: github.com/shopspring/decimal
      nullable: decimal.NullDecimal
```

| `TypeOverride` field | Required | Description |
|---------------------|----------|-------------|
| `type` | Yes | Go type name, e.g. `uuid.UUID`. |
| `import` | Yes | Import path for the type. |
| `zero_value` | No | Zero-value literal. Derived automatically; set only when derivation is wrong. |
| `nullable` | No | Nullable variant. Shorthand is a type name sharing the parent's import; the full form takes `{type, import, underlying_field, valid_field, valid_method, valid_invert}`. |

The type must cross the driver boundary on its own: it implements `sql.Scanner` and
`driver.Valuer`, or the driver special-cases it (`uuid.UUID`). Wrap a type that does neither in one
of your own that implements the two methods, and name the wrapper here — sqlgen performs no
conversion of its own.

Prefer a dedicated null type (`uuid.NullUUID`, `decimal.NullDecimal`) over `*T` for custom types —
it keeps the generated scan path free of pointer indirection.

## `tables` (per-table)

Keyed by table name, bare or schema-qualified. Six `generation` fields can be overridden here
(`query_limit`, `batch_size`, `page_size`, `cursor_keys`, `exclude_columns`, `strict_updates`), plus
table-specific settings: relationships, column overrides, doc comments, and polymorphic
relationship declarations.

A polymorphic edge takes one of two forms, and they are mutually exclusive on a single
relationship. `filter:` is raw SQL appended to the loader's `WHERE` clause — it expresses anything,
and because it is not invertible into a value a writer could set, such an edge is read-only.
`discriminator: {column, value}` is the structured form for the common case where the predicate is
one column equal to one value; it compiles to the same predicate `filter:` produced, and it is the
only polymorphic form nested mutations can write through.

Because `filter:` is raw SQL in the configured dialect, **write bare identifiers and single-quoted
string values** — `entity_type = 'asset.primary'` — the one spelling that means the same thing on
all three dialects. The dialects disagree about the double quote: on PostgreSQL and SQLite `"col"`
is a quoted identifier, while on **MySQL it is a string literal**, so `"entity_type" = 'asset.primary'`
compares two constants and the relationship silently loads nothing. sqlgen rejects that at codegen
on MySQL — quote a MySQL identifier with backticks. See PRD §13.7.1 for the full table.

```yaml
tables:
  public.users:
    api:
      operations: read_only
    exclude_columns:
      - password_hash
  audit_log:
    cursor_keys:
      - occurred_at
      - id
```

## `views`

Per-view settings, keyed by view name. Most commonly `cursor_keys`, since a view often lacks the
global default key:

```yaml
views:
  order_totals:
    cursor_keys:
      - product_id
```

## `enums`

Per-enum customization keyed by SQL enum name — chiefly renaming to avoid a collision with a
generated type of the same name.

## `extras`

Custom struct definitions, typically to give a JSONB column a typed Go shape rather than
`map[string]any`.

## Optional subsystems

Four blocks are disabled unless present. Each is opt-in and independent:

| Block | Enables |
|-------|---------|
| `api` | GraphQL schema + gqlgen resolver generation |
| `cache` | Read-through caching with invalidation |
| `events` | Mutation lifecycle events |
| `tenancy` | Mandatory tenant scoping on every generated query |

## Worked examples

Every configuration in `cmd/sqlgen/testdata/examples/` is a real, tested project — each directory
has a complete `sqlgen.yml`, its schema, and the generated output committed alongside:

| Example | Shows |
|---------|-------|
| `postgres` | Schemas, enums, domains, views, materialized views, manifest |
| `mysql`, `sqlite` | Dialect-specific configuration with the `stdlib` driver |
| `postgres_stdlib` | PostgreSQL via `database/sql` rather than pgx |
| `graphql`, `graphql_top_level` | API generation, including root-relative output directories |
| `cache`, `events` | The `cache` and `events` blocks |
| `tenancy`, `tenancy_postgres`, `tenancy_mysql` | Tenant scoping across dialects |
