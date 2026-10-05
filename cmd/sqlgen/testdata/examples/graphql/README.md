# GraphQL E2E Example

End-to-end example for the GraphQL API surface (PRD §26). Combines
`sqlgen generate` (schema-driven Go runtime) with a chained
`sqlgen graphql gen` wrapper invocation (gqlgen subprocess) to produce a
compilable, runnable graph package.

The example exercises every relationship variant (O2O, M2O, O2M, M2M, plus
multi-column filter relationships), every PRD §26.4.1 scalar registry category
(1/3/4) against real postgres column types (UUID, NUMERIC, JSONB, TIMESTAMPTZ,
JSON), the cache facade (PRD §27, opt-in via `WithCache`), and the tenancy
contract (PRD §29, auto-detected per §29.2.3 — `workspace_settings` carries
the tenant column inside its composite PK, `workspace_notes` carries it as an
ordinary column, and every other table is shared).

It also carries the **view** read surface (PRD §26.4). Both view
kinds are present, under `views/`:

- `workspace_note_summary` — a regular view over `workspace_notes`. It projects
  `workspace_id`, so §29.2.5 scopes its reads by detection alone with no
  `views.<n>.tenancy` block; `@pk: id` and the inherited `cursor_keys` default
  mean all three read queries are emitted. Its ENUM and JSONB columns are what
  put the enum and JSON comparator families on a view's `<V>Filter`.
- `category_price_totals` — a **materialized** view over the shared `products`
  table, with its own `cursor_keys` (views have no primary-key fallback,
  §4.13). It pins that a matview emits the identical read surface and that
  `Refresh` / `RefreshConcurrently` stay Go-client-only.

Neither emits a mutation field or a Create/Update input, and neither needs a
consumer-authored `models:` entry in `gqlgen.yml` — the bindings are merged in
automatically. `tests/view_tenancy_test.go` and `tests/view_read_surface_test.go`
drive both over the live gqlgen server.

It also turns on **nested mutations** (PRD §9.9, `generation.nested_mutations`).
The Go client gets `CreateWithRelated` / `UpdateWithRelated` /
`UpsertWithRelated`, and the API gets `create<T>WithRelated` /
`update<T>WithRelated` / `upsert<T>WithRelated`. The schema covers all four
write shapes, including a has-one edge, a polymorphic edge declared with
`discriminator:`, a self-referential tenanted edge and `filter:` edges that are
deliberately not writable. `tests/nested_mutations_*_test.go` drives them
through the Go client and the live gqlgen server.

## Migration from the legacy resolver layout

`sqlgen graphql gen` used to emit per-table resolver methods directly on
`*queryResolver` / `*mutationResolver` into `<resolver_dir>/resolvers_gen.go`,
alongside `field_options_gen.go` and `connection_walker_gen.go` in the same
package. Empirical testing against gqlgen v0.17.90 showed that emission shape
collides with gqlgen's resolvergen pass under both `follow-schema` and
`single-file` layouts — gqlgen copies the method bodies into its own
destination file without removing them from `resolvers_gen.go`, producing
duplicate declarations on the first run.

The current layout relocates the per-table impls into a sqlgen-owned helper
sub-package outside gqlgen's filename glob. Consumers on the original layout
need a one-time migration:

1. **Delete the legacy sqlgen-owned files** in `<resolver_dir>/`:

   ```bash
   rm models/graph/resolvers_gen.go \
      models/graph/field_options_gen.go \
      models/graph/connection_walker_gen.go
   ```

   These files are replaced by `<resolver_dir>/sqlgenresolver/*` (sqlgen-owned,
   regenerated on every run) plus `<resolver_dir>/<schema>_gen.resolvers.go`
   seeds (sqlgen seeds these on first run; gqlgen owns them after).

2. **Add `Q` and `M` to the gqlgen-scaffolded `Resolver` struct** in
   `<resolver_dir>/resolver.go`:

   ```go
   package graph

   import (
       models "<your-module>/<models-pkg>"
       "<your-module>/<models-pkg>/graph/sqlgenresolver"
   )

   type Resolver struct {
       Client *models.Client
       Q      *sqlgenresolver.Q
       M      *sqlgenresolver.M
   }
   ```

   On a fresh project sqlgen scaffolds this for you (the wrapper writes
   `resolver.go` on first run if absent). On an existing project,
   `mergeResolverScaffold` AST-edits the file to add the two fields plus the
   `sqlgenresolver` import without disturbing consumer-added fields or methods.
   You can still add your own fields (loggers, auth clients, request-scoped
   state) — sqlgen never overwrites `resolver.go` after the first run.

3. **Initialize `Q` and `M` at app boot** with the same `*models.Client`
   the rest of the resolver uses, and wrap the gqlgen handler with
   `sqlgenresolver.WithCallOptionsMiddleware` so the per-request HTTP →
   `CallOptions` bridge (PRD §26.11) is in place:

   ```go
   client := models.New(dbpgx.New(pool))
   resolver := &graph.Resolver{
       Client: client,
       Q:      &sqlgenresolver.Q{Client: client},
       M:      &sqlgenresolver.M{Client: client},
   }
   es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
   srv := handler.NewDefaultServer(es)
   http.Handle("/query", sqlgenresolver.WithCallOptionsMiddleware(srv))
   ```

   `WithCallOptionsMiddleware` is a stdlib `net/http` middleware, not a
   `graphql.HandlerExtension` — it must wrap the gqlgen server, not be
   registered via `srv.Use(...)`. See `tests/main_test.go` in this example for
   a working harness (httptest-backed equivalent).

4. **Re-run `sqlgen graphql gen`** (or `sqlgen generate`, which chains it)
   to seed the per-schema delegation files (`<schema>_gen.resolvers.go`
   under `follow-schema`, or a single `resolver.go` under `single-file`). On
   subsequent runs sqlgen's panic-stub rewriter swaps in `r.Q.<Field>(...)` /
   `r.M.<Field>(...)` delegations for any newly-managed fields gqlgen scaffolds
   as `panic("not implemented")`.

After step 4 the consumer package compiles cleanly under both
`resolver.layout: follow-schema` and `single-file`. See `docs/PRD.md` §26.5.6
for the generated-artifacts table.

## Layout

```
graphql/
  sqlgen.yml             # input/output + api.graphql + cache + tenancy
  gqlgen.yml             # consumer-owned; merged with sqlgen-emitted entries at wrapper time
  schema.sql             # 19 tables + every relationship variant + scalar registry coverage
  go.mod / go.sum        # pgx + gqlgen + cache/memory deps; `tool github.com/99designs/gqlgen` pins gqlgen
  generate.go            # //go:generate sqlgen graphql gen — belt-and-braces (`sqlgen generate` chains it inline)
  models/
    *_gen.go             # sqlgen-emitted runtime (client, models, cache, etc.)
    api_envelopes_gen.go # per-table generic aliases (UserConnection = Connection[User], ...)
    graph/
      *_gen.graphqls         # sqlgen-emitted schemas (one per table)
      shared_gen.graphqls    # scalar declarations + global comparator inputs + Query/Mutation
      <table>_gen.resolvers.go # sqlgen-seeded + gqlgen-owned per-schema resolver files
      generated_gen.go       # gqlgen-emitted
      resolver.go            # sqlgen-scaffolded root Resolver { Client; Q; M }
      sqlgenresolver/        # sqlgen-owned helper sub-package
        resolvers_gen.go     # *Q / *M with per-table query / mutation methods
        field_options_gen.go
        connection_walker_gen.go
        middleware_gen.go
        errors_gen.go
      model/
        models_gen.go        # gqlgen-emitted input / payload types
  expected/                  # golden tree mirroring models/ — committed; diffed by TestE2EGoldenFiles
  tests/                     # runtime tests against postgres testcontainer + live gqlgen handler
```

## gqlgen version

Pinned to `github.com/99designs/gqlgen v0.17.95` as a `tool` dependency in the
example's `go.mod`. The
panic-stub rewriter and the resolver seed flow rely on gqlgen's
"preserve-existing-method-bodies" semantics; if you run a different gqlgen
version, validate the cross-layout flow end-to-end before depending on the
rewriter.

## Running

The harness in `cmd/sqlgen/e2e_test.go::TestE2EGoldenFiles/graphql` invokes
`sqlgen generate` against this example and diffs the in-place tree against
`expected/`. Runtime tests under `tests/` boot a postgres testcontainer plus a
live gqlgen HTTP handler and exercise the generated surface end-to-end.

```bash
# Regenerate goldens after intentional template changes.
make update-golden-e2e

# Run golden-diff + runtime tests for every example.
make check-examples
```
