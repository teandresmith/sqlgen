# graphql_null_wrappers — `use_pointers: false` + gqlgen-bundled scalar E2E example

This module exists to compile and run two configurations that no other
example reaches, both of which had produced non-compiling generated code that
only unit tests were pinning.

## What it covers

### 1. Module-wide `overrides.use_pointers: false`

§4.7's flag makes every nullable column of a `gotype.nullSQL` type resolve to a
`database/sql` wrapper instead of `*T`. The `graphql` example's `scalar_probes`
table reaches the same seven Go types through a **table-scoped**
`overrides.types` block, which pins their schema, gqlgen binding and input
translator — but `scalar_probes` is a standalone table with no FK, no
relationship, no soft delete and no cursor keys, so it cannot reach the paths
the module-wide flag actually changes.

This schema is built for exactly those:

| Column | Wrapper | Path it pulls in |
|---|---|---|
| `accounts.parent_id` | `sql.NullInt64` | nullable self-FK → M2O + O2M relationship loaders key on a wrapper |
| `accounts.deleted_at` | `sql.NullTime` | soft-delete predicate reads and stamps a wrapper |
| `accounts.balance` | `decimal.NullDecimal` | integration-owned Null variant alongside the stdlib family |
| `accounts.tenant_ref` | `uuid.NullUUID` | ditto |
| `nickname` / `flag` / `small_count` / `mid_count` / `big_count` / `ratio` / `observed_at` | the seven `sql.NullX` | one column per wrapper, module-wide rather than table-scoped |

PostgreSQL is deliberate: it is the only dialect that reaches all seven
wrappers **and** lands soft delete on `sql.NullTime`. SQLite maps every integer
to `int64` (so no `NullInt16` / `NullInt32`) and claims `datetime` for
`types.NullDateTime` (so no `NullTime`).

A models package that did not compile under this flag, on every dialect, once
went unnoticed because no module set it. Its regression pin was a unit test;
this module is the compile-and-run pin it wanted.

### 2. `marshaling: builtin` — the gqlgen-bundled scalar route

```yaml
api.graphql.scalars:
  Int64: { go_type: int64, marshaling: builtin }
```

`builtin` means gqlgen ships the marshaler **and** sqlgen pins the
`models:` entry to it. Without the pin, gqlgen's `Int64` extraBuiltin has the
model list `[graphql.Int, graphql.Int64]` and a generated position takes
`Model[0]` — Go `int` — against an `int64` model field, which is an input
translator that does not compile.

`api.graphql.scalars` is keyed by Go **type**, so this retypes every `int64`
column in the module: both BIGSERIAL PKs, `external_ref`, `big_count`'s
non-null partner and the `entries.account_id` FK. That blast radius — PK
resolver args, cursors, filter inputs, relationship loaders — is why the route
needed a module of its own rather than a line in an existing example.

## Layout

```
graphql_null_wrappers/
├── schema.sql          # 2 tables — one column per wrapper, plus a nullable self-FK
├── sqlgen.yml          # use_pointers: false + Int64 builtin scalar
├── gqlgen.yml          # consumer-owned; mirrors ./models/graph
├── models/             # output.dir — row types, client, envelopes
│   └── graph/          # nested graph package (.graphqls + resolvers + sqlgenresolver/)
├── expected/           # golden tree
└── tests/              # live gqlgen handler over a real PostgreSQL
    ├── main_test.go            # container + handler wiring, gqlExec helpers
    ├── null_wrappers_test.go   # every wrapper: values, nulls, comparators
    ├── bundled_scalar_test.go  # Int64 losslessness past 2^53, schema introspection
    ├── relationship_test.go    # nullable-FK loaders (wrapper) vs NOT NULL (bare int64)
    └── soft_delete_test.go     # soft delete / restore over sql.NullTime
```

## Regenerate goldens

```bash
cd cmd/sqlgen && go test -run 'TestE2EGoldenFiles/graphql_null_wrappers' -update-e2e -count=1
```
