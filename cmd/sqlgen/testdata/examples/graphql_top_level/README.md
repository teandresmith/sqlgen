# graphql_top_level — top-level GraphQL output-dir E2E example

Proves the **root-relative** `api.graphql.schema_dir` / `resolver_dir`
(§26.5.8): with `schema_dir: graph` (no `./models` prefix) the
generated graph package lands at the **top-level `./graph`** — a *sibling* of
`./models`, not nested under it — and its import paths resolve to
`<module>/graph` and compile.

This complements the sibling [`graphql`](../graphql) example, which pins the
**nested** layout (`schema_dir: ./models/graph` → `<module>/models/graph`).
Between the two, both topologies the root-relative resolution supports are
covered end-to-end.

The schema is trimmed to two tables (`categories`, `products`, with one M2O
relationship) — just enough to emit a graph package and prove topology +
import resolution. Resolver / walker / connection behavior is exercised by the
`graphql` example and not re-proven here.

## Layout

```
graphql_top_level/
├── schema.sql          # 2-table input
├── sqlgen.yml          # schema_dir: graph / resolver_dir: graph (root-relative)
├── gqlgen.yml          # consumer-owned; paths mirror graph/ (top-level)
├── models/             # output.dir — row types, client, envelopes
├── graph/              # SIBLING of models/ — .graphqls + resolvers + sqlgenresolver/
└── expected/           # golden tree (models contents at root + graph/ subtree)
```

## Regenerate goldens

```bash
cd cmd/sqlgen && go test -run 'TestE2EGoldenFiles/graphql_top_level' -update-e2e -count=1
```
