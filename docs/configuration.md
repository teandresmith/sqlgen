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

> The schema checks structure, key names, and closed value sets. Of sqlgen's
> cross-field rules it checks only a relationship's link key (`fk` for o2o /
> o2m, `junction` for m2m), not the others — such as that `pgx` requires
> `dialect: postgres`, that `connection` is required when `source: database`,
> that a soft-delete column's SQL type must match its declared `type`. A file with no editor
> warnings can still fail `sqlgen validate`, so keep running it in CI.
>
> The schema expects a number or `true`/`false` in integer and boolean fields, so it flags
> `port: ${DB_PORT}` even though sqlgen accepts it. See [Environment variables](#environment-variables).

## Environment variables

Any value in `sqlgen.yml` can reference an environment variable as `${NAME}`. Use it to keep
secrets out of the file, or to change a setting per environment:

```yaml
input:
  dialect: postgres
  source: database
  connection:
    url: postgres://app:${SQLGEN_DB_PASSWORD}@db.internal:5432/appdb?sslmode=require
```

- `NAME` is letters, digits and `_`, and doesn't start with a digit. `$${` is a literal `${`.
  Any other `$` is kept as written, so `$1` and `$HOME` are unchanged.
- An unset variable fails `sqlgen generate` and `sqlgen validate` (exit 2). The error names the
  line, the key and the variable, and every unset variable is reported at once. A variable set
  to the empty string substitutes the empty string.
- There are no defaults: `${NAME:-default}` is an error.
- Keys aren't expanded, only values.
- An unquoted reference takes the type of its value, so `port: ${DB_PORT}` works. A quoted one is
  always a string, so `port: "${DB_PORT}"` is an error. Inside `[...]` or `{...}`, quote the
  reference: `paths: ["${SCHEMA_DIR}", ./views]`.
- Errors from loading the config never show a variable's value.

**Passwords in a URL.** Substitution is literal, so a password containing `/`, `?`, `#` or `%`
breaks a postgres URL. Use the individual fields instead (`password: ${DB_PASSWORD}`), which
sqlgen escapes for you, or store the password percent-encoded.

**Committed output.** A reference in a setting that reaches generated code, such as
`output.package`, makes the output depend on the environment that ran `generate`. If you commit
generated files and check them for drift in CI, keep references to connection and deployment
settings.

## Contents

- [Environment variables](#environment-variables) · [Minimal config](#minimal-config) · [Top level](#top-level)
- [`input`](#input) · [`connection`](#connection) · [`introspect`](#introspect) · [`exclude_tables`](#exclude_tables)
- [`output`](#output) · [`generation`](#generation) · [`overrides`](#overrides)
- [`tables`](#tables-per-table) · [Relationships](#relationships) · [`views`](#views) · [`enums`](#enums) · [`extras`](#extras)
- [Optional subsystems](#optional-subsystems) · [`api.graphql`](#apigraphql) · [Worked examples](#worked-examples)

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
| `paths` | []string | `["."]` | — | SQL files or directories to parse. Used when `source` is `files` or `both`; ignored under `database`, with a warning when set explicitly. |
| `views` | []string | `[]` | — | Paths to view annotation files or directories. Parsed only as views, never as DDL. |
| `schema` | string | `*` | — | PostgreSQL only: the schema an unqualified table name or `REFERENCES` target resolves to. `*` resolves to `public`. It does not filter which schemas are parsed; `introspect.schemas` filters introspection. Ignored with a warning on MySQL/SQLite. |
| `connection` | object | — | — | Required when `source` is `database` or `both`. |
| `introspect` | object | — | — | Schema/table filtering for introspection. Ignored with a warning under `source: files`. |

> `dialect` is `postgres` — not `postgresql`. An invalid value fails `sqlgen validate`.

### `connection`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | — | Full connection URL. Takes precedence over the individual fields below. |
| `host` | string | — | Required for postgres/mysql when `url` is empty. A value starting with `/` is a Unix socket: the socket directory on postgres, the socket file on mysql. |
| `port` | int | `5432` (pg), `3306` (mysql) | Ignored on SQLite, and on mysql when `host` is a socket file. |
| `user` | string | — | Required for postgres/mysql when `url` is empty. On mysql it cannot contain `:`, which the DSN has no escape for. |
| `password` | string | — | Usually a reference, `password: ${DB_PASSWORD}`. See the environment overrides below. |
| `database` | string | — | Database name, or the file path for SQLite. Required when `url` is empty. A SQLite file that does not exist is a connection error; sqlgen does not create it. A `file:` URI that sets `mode` is opened as that mode says. |
| `ssl_mode` | string | `disable` (pg), `false` (mysql) | TLS/SSL mode. |

Two environment variables override the file, and take highest precedence — keep credentials out
of `sqlgen.yml`:

| Variable | Overrides |
|----------|-----------|
| `SQLGEN_DB_URL` | `connection.url` **and** all individual fields |
| `SQLGEN_DB_PASSWORD` | `connection.password` |

`url` wins over the individual fields, `password` included, so `SQLGEN_DB_PASSWORD` does nothing
when the connection comes from a `url`. Reference it in the URL instead, as in the
[example above](#environment-variables). sqlgen warns when the variable is set and a `url`
ignores it.

### `introspect`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schemas` | []string | all | Schemas to include. Empty means all non-system schemas. PostgreSQL only; ignored with a warning on MySQL/SQLite. |
| `exclude_schemas` | []string | `[]` | Schemas to exclude. Applied after inclusion. PostgreSQL only; ignored with a warning on MySQL/SQLite. |
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
| `package` | string | derived from `dir` | — | Default Go package name. Derived from the last element of the absolute `dir`, lowercased with `-` turned into `_`, so `dir: .` run from `/src/models` gives `models`. |
| `layout` | string | `single_file` | `single_file`, `file_per_table` | File layout strategy for tables. |
| `enums` | object | — | — | Override the enums file name (`file`, `package`); the file must stay directly in `dir` and `package` must equal `output.package`. |
| `types` | object | — | — | Override the composite/domain types file name (`file`, `package`); the file must stay directly in `dir` and `package` must equal `output.package`. |
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
`deleted`. An explicit empty list (`soft_delete_columns: []`) turns detection off; leaving the key
out keeps the default list. `update_columns: []` works the same way. Three column types are supported:

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

### Relationships

sqlgen detects relationships from FOREIGN KEY constraints. You configure them per table with two
keys: `exclude_relationships` drops detected edges, and `relationships` declares edges that
detection can't find or names differently. PRD §5.3 and §13.4 are the full rules.

**How detected edges are named.** Each FOREIGN KEY gives at most one edge in each direction,
joining every column pair the constraint lists, so a composite key gives one edge each way, not one
per column. On a table that isn't a junction, a foreign key gives a to-one edge on the table holding
it and, unless the key is unique, a list edge on the table it references. So `tasks.cycle_id →
cycles` gives both `Task.Cycle` and `Cycle.Tasks`. A unique key gives only the to-one edge; the
has-one view from the referenced side is declared under `relationships`.

| Edge | Named after | Example (`tasks.cycle_id → cycles`) |
|------|-------------|-------------------------------------|
| O2M | The table holding the foreign key, pluralized | `cycles` gets `tasks` (`Cycle.Tasks`) |
| O2O (to-one) | The key's naming member without `_id`, singular | `tasks` gets `cycle` (`Task.Cycle`) |
| M2M | The table at the other end of the junction | `tasks` gets `labels` through `task_labels` |

The naming member is usually the key's one column outside the primary key and the table's other
foreign keys (PRD §5.3 rule 5 has every step), such as `author_id` in `posts.author_id → users`,
which gives `Post.Author`. A to-one edge falls back to the referenced table's name (`Post.User`)
when the key has no naming member, or when the stem names a column of the holding table.

A detected edge whose Go field would equal a column's is omitted, with a warning naming the column
and the edge (a `column_map` rename onto the edge's field is an error instead): `users(posts TEXT)`
keeps the `Posts` column and drops the detected `posts` edge. Declare the edge under another name to
keep it. A declared relationship whose field equals a column's is an error.

When two or more edges of one cardinality (to-one, or list) share a source and a target, each is
renamed from its own key. `<stem>` is the key's naming member, a column such as `parent_task_id`,
without its `_id` suffix. An O2M edge becomes `<stem>_<name>`, an O2O edge becomes `<stem>`, and an
M2M edge takes its junction table's name. So a second foreign key between the same two tables
renames the list edges that were already there. Adding `tasks.parent_task_id → tasks` beside a
`task_dependencies` junction turns `Task.Tasks` into `Task.ParentTaskTasks`.

**`exclude_relationships`** lists the detected names to drop. It matches the snake_case detected
name (`task_assignees`, `parent_task_tasks`), not the Go field name, and a renamed edge also matches
the name it had before the rename. A to-one edge also matches its referenced table, so
`exclude_relationships: [users]` on `posts` drops `Post.Author`. On a self-referential table, or two
tables that reference each other, one entry can match both the list edge and the to-one edge; declare
the one you want to keep over its key.

**`relationships`** declares an edge. `fk`, `junction_local_fk` and `junction_reference_fk` each take
a column name or a list. A list maps by position onto the referenced table's primary key, in its
`PRIMARY KEY (...)` order, and must have one entry per key column:

```yaml
tables:
  cycles:
    relationships:
      - name: Tasks
        type: o2m              # o2o | o2m | m2m
        table: tasks
        fk: [workspace_id, cycle_id]   # → cycles (workspace_id, id)
  tasks:
    relationships:
      - name: Assignees
        type: m2m
        table: users
        junction: task_assignees
        junction_local_fk: task_id
        junction_reference_fk: user_id
```

The other fields are `side` (O2O: `parent` or `child`), `filter` or `discriminator` (polymorphic
edges, mutually exclusive), `sort` (O2M and M2M), and `description`.

**Renaming a detected edge** takes a single declaration over the same key, as `Assignees` does
above. A declared edge with no `filter` or `discriminator`, of the same kind and target and over the
same key column pairs (for M2M, the same junction and both junction keys), takes the detected edge's
place, with no warning and no `exclude_relationships` entry. Its manifest entry keeps the FOREIGN
KEY's `constraint`. A declared edge over a different key that resolves to the same Go field as a
detected one also replaces it, with a warning naming both. If you want both edges, give the declared
one a different name, and a different key or a `filter`. A rename onto another detected edge's field
is an error: pick a name no other edge resolves to. A declared key no FOREIGN KEY declares has no
`constraint`.

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

## `api.graphql`

`api.graphql` generates a GraphQL schema, gqlgen resolvers and translators into the models package.
Run `sqlgen graphql init` once to scaffold `gqlgen.yml`. After that, `sqlgen generate` runs the
gqlgen step too, unless you pass `--no-graphql`. PRD §26 is the full specification.

```yaml
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: graph          # root-relative; default <output.dir>/graph
    resolver_dir: graph        # root-relative; default <output.dir>/graph
    max_depth: 5
    max_complexity: 1000
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Generate the GraphQL surface. Also requires `api.enabled: true`. |
| `schema_dir` | `<output.dir>/graph` | Directory for `*_gen.graphqls`, relative to the module root. |
| `resolver_dir` | `<output.dir>/graph` | Directory for resolvers and helpers, relative to the module root. |
| `package` | from `resolver_dir` | Go package name for the resolver code. |
| `max_depth` | `5` | Maximum relationship nesting depth. Only relationship fields count toward it. |
| `max_complexity` | `1000` | gqlgen complexity limit. Queries over it are rejected before execution. |
| `server_scaffold` | `true` | Write the one-time `server.go` into `resolver_dir` when it's missing. Set `false` if your application builds its own `handler.Server`. |
| `field_casing` | `camel_case` | `camel_case` or `snake_case` for emitted field names. |
| `gqlgen_config` | `./gqlgen.yml` | Your `gqlgen.yml`. sqlgen merges its entries into a temporary copy and never edits this file. |
| `gqlgen_bin` | `go run github.com/99designs/gqlgen` | How to invoke gqlgen. The default uses your `go.mod` pin. |
| `scalars` | empty | Custom scalar bindings (`go_type`, `marshaling`, `marshaler_package`) added to the built-in set. |

Per table, `tables.<t>.api` sets `enabled` and `operations`.

**The query limits and `server.go`.** `max_depth` and `max_complexity` are written to the regenerated
`<resolver_dir>/sqlgenresolver/limits_gen.go`, whose `ApplyQueryLimits(*handler.Server)` installs
them. sqlgen also writes `<resolver_dir>/server.go`, a starter `NewServer` that calls
`ApplyQueryLimits`. It's yours to edit, and sqlgen never overwrites it. While `server_scaffold` is
`true`, sqlgen writes it again whenever the file is missing. If your application builds its own
`handler.Server`, set `server_scaffold: false`: sqlgen then never writes `server.go` and leaves any
existing one alone. Your server should then do what the scaffold does:

- Call `sqlgenresolver.ApplyQueryLimits(srv)`, so the limits in `sqlgen.yml` are the ones it
  enforces.
- Wrap the handler in `sqlgenresolver.WithCallOptionsMiddleware`, so the per-call headers
  (`Cache-Control: no-cache`, `X-Skip-Events`, `X-Skip-Hooks`) keep working.

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
