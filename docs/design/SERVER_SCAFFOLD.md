# GraphQL Server Scaffold Opt-Out

> **Status:** approved (2026-11-20; drafted 2026-11-19). Verified against source at `82544e16`. Every `sqlgen generate`
> with `api.graphql.enabled: true` writes the one-time `<resolver_dir>/server.go` when it is
> missing, so an app that builds its own `handler.Server` cannot get rid of the file: deleting it
> only brings it back on the next run. This proposes a boolean `api.graphql.server_scaffold`
> (default `true`); when it is `false`, `server.go` is never written. The regenerated
> `sqlgenresolver/limits_gen.go` and its `ApplyQueryLimits` stay as they are, so a hand-written
> server still enforces the configured limits. Sibling to PRD §26.5.6 (Module Boundary & Wrapper
> Subcommand) and §26.6 (Query Complexity Limits).

## 1. Motivation

### 1.1 Symptom

Reported by the `sqlgen-example` app on its upgrade to v0.1.2 (finding 4,
`sqlgen-example/.scratch/sqlgen-v0.1.2/FINDINGS.md`):

```
$ make generate && git status --short | grep '??'
?? internal/graph/server.go
?? internal/graph/sqlgenresolver/limits_gen.go
```

The app serves GraphQL from its own `internal/server/server.go`, with
`FixedComplexityLimit(200)` and a hand-written `FixedDepthLimit{12}`
(`internal/server/limits.go`). The scaffolded `graph.NewServer` goes unused. Deleting it doesn't
help, because the next generate writes it again, so the app has to commit an unused file.

The behavior is pinned today by
`unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server.go is written when absent`, which
runs `Generate` on a project with no `server.go` and finds one written.

### 1.2 Root cause

`generateAPIServerFile` (`cmd/sqlgen/gen/api_limits.go:79-113`) checks one thing: whether
`<configured resolver_dir>/server.go` exists (`:87`). If it doesn't, the file is written. No
config is read. `generateAPIServerFiles` (`cmd/sqlgen/gen/orchestrate.go:1197-1213`) calls it on
every run in which the API is generated. PRD §26.5.6's file table
(`docs/PRD.md:13397`, "`graph/server.go` | sqlgen (one-shot) | **Only if missing**") offers no
opt-out.

## 2. Scope

### 2.1 In scope

- A config key, `api.graphql.server_scaffold`, which turns off writing `server.go`.
- Its JSON Schema entry, its default, and its documentation in `docs/configuration.md`.
- PRD §26.3 (API config table), §26.5.6 and §26.6 wording.

### 2.2 Non-goals

- **Changing `limits_gen.go`.** It stays regenerated on every run whatever the key says. It's the
  supported way for a hand-written server to apply the configured limits.
- **Deleting an existing `server.go`.** sqlgen never deletes consumer-owned files (§26.5.6
  ownership; §23 orphan rule "reports, not deletes").
- **Reconciling depth semantics with gqlgen's `FixedDepthLimit`.** sqlgen's depth counts
  relationship fields only (§26.6), while the example's counts every field hop. They measure
  different things, and that stays a consumer choice.
- **The two `server.go` backlog lines** (`docs/tracker/backlog.md:191-192`: hard-coded client
  name in the resolver scaffold, and the `exec.package` assumption). The first is about
  `resolver.go`, and the second is a template-content issue this key doesn't touch.

## 3. Current state (measured from code)

- `server.go` is written only when absent. The check uses the configured path rather than the
  write path, so `sqlgen diff` never proposes a file the consumer already owns
  (`cmd/sqlgen/gen/api_limits.go:79-92`).
- It's skipped when the API context has no tables, models import path or sqlgenresolver import
  path (`api_limits.go:83-85`).
- It carries no generated header (`api_limits.go:104-105`), and its body calls
  `sqlgenresolver.ApplyQueryLimits(srv)` and `sqlgenresolver.WithCallOptionsMiddleware(srv)`
  (`TestGenerate_GraphQLQueryLimits/server.go is written when absent`,
  `cmd/sqlgen/gen/api_query_limits_test.go:112-133`).
- `limits_gen.go` is regenerated on every run (`orchestrate.go:1199-1203`;
  `TestGenerate_GraphQLQueryLimits/the limits file is regenerated with new values on a second run`).
- Nothing generated refers to `NewServer`. Its only references are the template
  (`cmd/sqlgen/gen/templates/api/server.go.tmpl:12`) and the example tests that start the server
  (`cmd/sqlgen/testdata/examples/graphql/tests/main_test.go` and others), so skipping the file
  leaves every generated package compiling.
- `GraphQLAPIConfig` (`cmd/sqlgen/config/config.go:1172-1183`) has no such key. Defaults are
  applied at `config.go:1914-1919`. The JSON Schema's `graphql` object is closed
  (`cmd/sqlgen/config/schema/v1.json:677-680`, `additionalProperties: false`), and
  `TestSchemaV1_noDriftFromConfigStructs` (`cmd/sqlgen/config/schema_test.go:496`) keeps the
  struct and the schema in step.
- `gqlgen.yml` is the only other one-time scaffold. `generate` never writes it; only
  `sqlgen graphql init` does (PRD `:11877`). `server.go` is the only one-time file `generate`
  creates by itself.
- Registry rows that pin today's behavior: `api.artifacts.server-go`,
  `api.artifacts.server-go-kept`, `api.complexity.server-applies-limits`
  (`docs/conformance/cli/api.md:663-664,730`).

## 4. Options

### 4.1 A config key `api.graphql.server_scaffold: bool` (recommended)

Default `true`. With `false`, `generate` and `diff` never write or propose `server.go`.

- **Wins:** it's explicit and discoverable, it's one boolean, and it has no effect on any existing
  config or golden.
- **Loses:** one more public config key.

### 4.2 Documentation only

Document that the file comes back and that a hand-written server should call `ApplyQueryLimits`.

- **Wins:** no new surface.
- **Loses:** the app still carries a file it never uses, and a new reader can't tell it's dead
  code.

### 4.3 Implicit detection

Skip the scaffold when the resolver package already declares `NewServer`, or when a marker file
exists.

- **Wins:** no config key.
- **Loses:** it means scanning source files. It also fails quietly if the consumer names the
  function something else, and a marker file is a convention no one finds.

**Recommendation:** 4.1, plus 4.2's documentation of the `ApplyQueryLimits` pattern.

## 5. Proposed design

**Config.** Add `ServerScaffold *bool \`yaml:"server_scaffold"\`` to `GraphQLAPIConfig`. When it's
unset, it defaults to `true` in the same `applyDefaults` block as `max_depth` and
`max_complexity` (`config.go:1914-1919`). A tri-state pointer keeps "unset" distinguishable, as
the cache and tenancy `enabled` fields already do. Add the key to `v1.json`'s `graphql`
properties as `boolean`. There's no validation rule: the key is a plain boolean with no
interaction with other keys.

**Gen.** `generateAPIServerFiles` passes the resolved boolean, and `generateAPIServerFile`
returns `"", nil` before its `os.Stat` when it's `false`. `limits_gen.go` is unchanged.

**CLI.** No change. `diff` reads the same files `generate` writes, so it stops proposing
`server.go` too.

**Existing `server.go` with `server_scaffold: false`.** Left untouched. sqlgen neither reads nor
deletes it (§2.2).

**Docs.** In `docs/configuration.md` § `api.graphql`, add a table row and change the
"query limits and `server.go`" paragraph to name the key.

## 6. Behavior contract

### 6.1 New and changed behavior

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| scaffold.off-not-written | With `api.graphql.server_scaffold: false`, `generate` writes no `server.go` and does not list it among the files it wrote | §26.5.6 (proposed in §8) | all | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false writes no server.go` | api-generation: `graph/server.go` × single PK | planned |
| scaffold.off-limits-still-written | With `api.graphql.server_scaffold: false`, `sqlgenresolver/limits_gen.go` is still written with the configured `max_depth` and `max_complexity` | §26.6 (proposed in §8) | all | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false still writes the limits file` | api-generation: `sqlgenresolver/limits_gen.go` × single PK | planned |
| scaffold.off-existing-kept | With `api.graphql.server_scaffold: false`, an existing `server.go` is left byte-for-byte unchanged | §26.5.6 (proposed in §8) | all | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server_scaffold false leaves an existing server.go untouched` | api-generation: `graph/server.go` × single PK | planned |
| scaffold.default-true | An unset `api.graphql.server_scaffold` behaves as `true` | §26.3 API config table (proposed in §8) | all | `unit: cmd/sqlgen/config: TestLoad_GraphQLServerScaffoldDefault` | config-fields: `api.graphql.server_scaffold` × default | planned |
| scaffold.diff-not-proposed | With `api.graphql.server_scaffold: false` and no `server.go` on disk, `sqlgen diff` reports no `server.go` change | §23 `diff` coverage + §26.5.6 (proposed in §8) | all | `unit: cmd/sqlgen/cli: TestDiff_ServerScaffoldOffProposesNoServer` | commands: `diff` × success × output | planned |
| scaffold.schema-accepts | The JSON Schema accepts `api.graphql.server_scaffold` as a boolean and rejects a non-boolean | §26.3 (proposed in §8) | all | `unit: cmd/sqlgen/config: TestSchemaV1_fieldValueTypes/api.graphql.server_scaffold` | config-fields: `api.graphql.server_scaffold` × schema | planned |

### 6.2 Regression guards

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| With the key unset, `server.go` is written when absent | §26.5.6 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server.go is written when absent` | guarded |
| With the key unset, an existing `server.go` is left untouched | §26.5.6 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/an existing server.go is left untouched` | guarded |
| `limits_gen.go` is regenerated with new values on every run | §26.6 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/the limits file is regenerated with new values on a second run` | guarded |
| The scaffolded `server.go` applies the limits | §26.6 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLServerCarriesQueryLimits` | guarded |
| `max_depth` / `max_complexity` default to 5 / 1000 | §4, §26.6 | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/defaults are depth 5 and complexity 1000 when unset` | guarded |
| The config structs and `v1.json` do not drift | §4.14 | `unit: cmd/sqlgen/config: TestSchemaV1_noDriftFromConfigStructs` | guarded |
| Every example config still validates against the schema | §4.14 | `unit: cmd/sqlgen/config: TestSchemaV1_acceptsExampleConfigs` | guarded |
| `sqlgen diff` does not propose a `server.go` the consumer already owns | §23, §26.5.6 | none found | unguarded |
| The GraphQL examples still start their server through the scaffolded `NewServer` | §26.5.6 | `example: graphql: TestQueryLimits`; `golden: graphql`; `golden: graphql_top_level` | guarded |

### 6.3 Module matrix cells

#### API generation (CLI)

The rows touched are the two §26.5.6 artifacts. Columns are the table kinds; the key is
project-wide, so the same ID covers each.

| Artifact | single PK | composite PK | no PK | soft delete | tenanted | view | materialized view |
|---|---|---|---|---|---|---|---|
| `graph/server.go` (`server_scaffold: false`) | scaffold.off-not-written, scaffold.off-existing-kept | n/a: project-wide file, independent of table kind (§26.5.6) | n/a: no-PK tables are not generated (§9.4b) | n/a: project-wide file | n/a: project-wide file | n/a: project-wide file | n/a: project-wide file |
| `sqlgenresolver/limits_gen.go` (`server_scaffold: false`) | scaffold.off-limits-still-written | n/a: project-wide file | n/a: no-PK tables are not generated (§9.4b) | n/a: project-wide file | n/a: project-wide file | n/a: project-wide file | n/a: project-wide file |

#### Config fields (CLI, `config-fields` in `docs/conformance/cli/config-fields.md`)

| Field | default | explicit | schema |
|---|---|---|---|
| `api.graphql.server_scaffold` | scaffold.default-true | scaffold.off-not-written | scaffold.schema-accepts |

No §4.13 validation rule is added. A non-boolean value is a YAML type error from the strict
loader, so the config-validation matrix gains no row.

#### Commands (CLI)

| Command | success × exit code | success × output |
|---|---|---|
| `diff` (`server_scaffold: false`, no `server.go`) | scaffold.diff-not-proposed | scaffold.diff-not-proposed |

The config rows landed in `api-schema.md` §26.3, which declares no matrix, so they carry no matrix cell. The `server.go` rows in `api.md` leave the cell empty, as the existing `server.go` rows do.

### 6.4 Dialect differences

None. The key touches only GraphQL file emission, which doesn't vary by dialect.

## 7. Probe record

- 2026-11-19, `82544e16`: `make test` ran
  `TestGenerate_GraphQLQueryLimits/server.go is written when absent`, which passed: `Generate` on
  a project with no `server.go` writes one. That's the reported symptom in unit form.
- 2026-11-19, `82544e16`: read `cmd/sqlgen/gen/api_limits.go:79-113` and
  `orchestrate.go:1197-1213`. Neither function reads any config key for the scaffold.
- No prototype was needed. The change is a single early return behind a resolved boolean.

## 8. PRD and doc impact

**PRD §26.5.6, file table row** (`docs/PRD.md:13397`):

> | `graph/server.go` | sqlgen (one-shot) | **Only if missing**, and only while `api.graphql.server_scaffold` is `true` (the default) | Yes — entry-point bootstrapping. With `server_scaffold: false` sqlgen never writes it and never touches one that exists; a hand-written server calls `sqlgenresolver.ApplyQueryLimits` itself ([§26.6](#266-query-complexity-limits)) and wraps its handler in `sqlgenresolver.WithCallOptionsMiddleware` for the per-call headers ([§26.11](#2611-per-call-options-via-http-headers)). |

**PRD §26.6, protections bullet** (`docs/PRD.md:13463`), append:

> With `api.graphql.server_scaffold: false` no `server.go` is written, and `limits_gen.go` is
> written all the same: a consumer that builds its own `handler.Server` applies the configured
> limits by calling `sqlgenresolver.ApplyQueryLimits(srv)` on it, and wraps it in
> `sqlgenresolver.WithCallOptionsMiddleware` as the scaffold does ([§26.11](#2611-per-call-options-via-http-headers)).

**PRD §26.3 API config table** (after `api.graphql.max_complexity`, `docs/PRD.md:12315`):

> | `api.graphql.server_scaffold` | `bool` | `true` | Write the one-time `server.go` into `resolver_dir` when it is missing ([§26.5.6](#2656-module-boundary--wrapper-subcommand)). Set `false` when the application builds its own `handler.Server`; `limits_gen.go` is still generated ([§26.6](#266-query-complexity-limits)). |

**`docs/configuration.md` § `api.graphql`:** add the table row. Replace the closing paragraph's
"deleting it doesn't stick" sentence with "set `server_scaffold: false` to stop writing it".

**`cmd/sqlgen/config/schema/v1.json`:** `"server_scaffold": {"type": "boolean", "default": true, "description": …}`.

**Registry** (`docs/conformance/cli/api.md`, and `api-schema.md` §26.3 for the config rows beside the other `api.graphql` defaults): the §6.1 rows move there in N.0.

## 9. Plan

| # | Sub-item | Contract rows | Notes |
|---|---|---|---|
| N.0 | PRD sync | — | §8 wording; move §6.1 rows into the registry. Blocks everything |
| N.1 | Regression guards | the unguarded `diff` row in §6.2 | `TestDiff_KeepsExistingServerGo` on today's code; must pass |
| N.2 | Config key, default and JSON Schema | scaffold.default-true, scaffold.schema-accepts | `config.go`, `v1.json` |
| N.3 | Gen: skip the scaffold; docs | scaffold.off-not-written, scaffold.off-limits-still-written, scaffold.off-existing-kept, scaffold.diff-not-proposed | `api_limits.go`, `orchestrate.go`, `docs/configuration.md` |

## 10. Definition of done

The work closes only when every item below holds:

1. Every §6.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §6.2 row is guarded, and every guard still passes.
3. Every §6.3 cell is an ID or a stated `n/a`.
4. Every §8 edit has landed, and every §11 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too when the
   change reaches a runtime or driver package.
6. No golden changes: every example leaves the key unset.

## 11. Rulings (settled 2026-11-20)

| # | Question | Ruling | Evidence |
|---|---|---|---|
| 1 | Key name and shape | `api.graphql.server_scaffold: bool`, default `true` (a tri-state pointer internally) | User ruling |
| 2 | Warn when the key is `false` but a `server.go` exists? | No. The file may be the app's own server, which is the case the key serves. | User ruling |

## 12. Rejected alternatives

- **Implicit `NewServer` detection** (§4.3): it depends on source scanning, and it fails quietly
  when the consumer names the function differently.
