# Phase 20: GraphQL Output Directories (root-relative `schema_dir` / `resolver_dir`)

Status: In Progress (20.1 + 20.2 complete)
PRD Sections: 26.5.8 (canonical — new), 26.5.6 (import-path mechanics), 30.3 / 30.4.3 (manifest breadcrumbs + `graph_top_level`), 4.13 (validation)

> **Normative spec:** PRD §26.5.8 (amended 2026-07-08).

> **Rationale:** Today `schema_dir` / `resolver_dir` resolve *relative to `output.dir`*, pinning the generated graph package under the models tree. Making them **root-relative** (path from the module root, like `output.dir`) lets graph live anywhere — nested (`models/graph`) or a top-level sibling (`graph`) — with no mode flag. This is a net simplification: it deletes the `resolveAPIDir` `output.dir`-join for graph and retires FIX-071's `output.dir`-prefix workaround. Pre-published project — no backward-compat concern; the one example that sets these dirs is updated and its goldens regenerated.

> **Scope rule:** §26 (GraphQL) follow-on. Bounded to the graph output-dir resolution + import wiring + validation + one E2E proof module. The manifest-side consumption (`generation_config.graph_top_level` inference) is **FIX-117**, resolved in Phase 18 *after* this phase lands — not here.

> **Depends on:** Phase 16 GraphQL surface (stable). **Unblocks:** Phase 18 FIX-117 (manifest infers `graph_top_level`), which in turn gates the rest of the 18.2 FIX batch (115/116/118/119/120).

> **Sequencing note:** numerically after Phase 18/19 but a *dependency* of the 18.2 fix batch — runs before FIX-117. Phase 19 is reserved for the MCP server (`docs/design/MCP.md`); this is Phase 20.

---

## 20.1 Root-relative resolution + default + import wiring

**PRD Reference:** §26.5.8 (root-relative semantics + default + disabled-API ignore), §26.5.6 (import-path mechanics), §4.13 (validation).
**Design Reference:** PRD §26.5.8.

**Module:** `cmd/sqlgen/gen/orchestrate.go` (`resolveAPIDir`, `PopulateAPIContextResolverFields`), `cmd/sqlgen/config/config.go` (defaults), `cmd/sqlgen/config/validate.go` (disabled-API ignore).

**Status:** Complete

**Depends on:** Nothing new — self-contained §26 change.

### Tasks

- [x] Make `schema_dir` / `resolver_dir` **root-relative**: graph dir = `filepath.Join(projectRoot, dir)` (abs used verbatim), NOT `filepath.Join(output.dir, dir)`. Drop the `output.dir` base for graph in `resolveAPIDir` (or replace its callers for graph dirs). — `resolveAPIDir` replaced by `resolveGraphDir(projectRoot, dir)` in `orchestrate.go`.
- [x] **Default**: when `schema_dir` / `resolver_dir` unset, default to `<output.dir>/graph` (nested alongside models) — a computed default value, applied in config defaults. Explicit values taken verbatim as root-relative. — `applyAPIDefaults` now computes `filepath.Join(cfg.Output.Dir, "graph")`.
- [x] **Import paths**: graph resolver / `sqlgenresolver` / schema import path = `JoinModulePath(module, <dir>)` directly (dir already root-relative). **Retire FIX-071's `output.dir`-prefix workaround** — no longer needed since the default carries the full path. — FIX-071 join removed from `PopulateAPIContextResolverFields`.
- [x] **Write anchor**: thread a consistent "project root" so graph writes land under the same anchor as models. Reconcile with the E2E golden harness's output-dir remap (`opts.OutputDir` / `OriginalOutputDir`) so redirected write locations stay consistent for graph. — added `Options.ProjectRoot` (default `"."`); graph is never combined with the temp-dir remap (graphql examples generate in-place), so ProjectRoot=cwd keeps graph and models on the same anchor.
- [x] **Disabled API**: when `api.graphql.enabled: false`, ignore `schema_dir` / `resolver_dir` (and all `api.graphql.*`) entirely — no validation, no resolution, no emission (PRD §26.5.8). — `validateAPIConfig` early-returns when `!g.Enabled`; `BuildAPIContext` already nil-gates emission.
- [x] Update the graphql example config (`schema_dir: ./graph` → the root-relative form that reproduces its current nested location, i.e. `models/graph` or unset) and regenerate its goldens. — set to `./models/graph`; goldens byte-clean (no regeneration needed — same resolved location + import path).

### Acceptance Criteria

- `schema_dir: graph` emits graph at `<root>/graph` with import path `<module>/graph`; `schema_dir: models/graph` (or unset) emits at `<root>/models/graph` with import path `<module>/models/graph`. Both compile.
- No `output.dir`-relative join remains for graph dirs; FIX-071's workaround is removed and the resolver import path is a direct `JoinModulePath(module, resolver_dir)`.
- With `api.graphql.enabled: false`, no `api.graphql.*` field is validated, resolved, or emitted.
- The existing graphql example regenerates byte-clean at its (unchanged) nested location.

### Tests Required

- [x] `cmd/sqlgen/gen` unit tests: graph dir + import-path derivation for (a) unset (default `<output.dir>/graph`), (b) explicit nested (`models/graph`), (c) top-level (`graph`), (d) custom (`api/graph`). Assert both resolved on-disk dir and derived import path. — `TestResolveGraphDir` (on-disk dir) + `TestGraphImportPaths_RootRelative` (import path, incl. the FIX-071 retirement guard: output.dir=models + resolver_dir=graph → `<module>/graph`) in `api_orchestrate_test.go`; default computation in `config.TestLoadConfig_GraphDirsRootRelativeDefault`.
- [x] Disabled-API: `api.graphql.enabled: false` with `schema_dir` set → no graph files emitted, no validation error. — `TestValidatePreParse_APIGraphQLDisabledIsInert` (no validation) + `TestBuildAPIContext_DisabledIsInert` (no emission).
- [x] Regression: graphql example goldens byte-stable after the config update. — `TestE2EGoldenFiles/graphql` passes byte-clean; `git status` shows only the two edited config files.

### Completion Record

**Landed 2026-07-08.** Root-relative `schema_dir` / `resolver_dir` (§26.5.8). Changes:
- `cmd/sqlgen/config/config.go` — `applyAPIDefaults` defaults unset graph dirs to `filepath.Join(cfg.Output.Dir, "graph")` (computed `<output.dir>/graph`).
- `cmd/sqlgen/config/validate.go` — `validateAPIConfig` early-returns when the GraphQL API is inactive; the guard mirrors `BuildAPIContext`'s emission gate exactly (`!cfg.API.Enabled || !g.Enabled`) so validation and emission never disagree. The now-redundant `g.Enabled &&` guard on the FIX-077 alias-collision check dropped.
- `cmd/sqlgen/cli/generate_test.go` — `TestGenerate_ChainedGraphQL_{MissingGqlgenYml,NoGraphQLFlag,EnvVar}` now `t.Chdir` into the temp project root before running `generate`. These set an absolute temp `output.dir` with a relative `schema_dir: ./graph` and previously ran from the cli package dir; under root-relative resolution `./graph` resolves against cwd, so without the chdir `generate` (which emits graph before the chained gqlgen pre-flight fails) leaked a `cmd/sqlgen/cli/graph/` tree into the source repo. Chdir'ing matches how `sqlgen` actually runs (from the module root) and lands the graph tree in the auto-cleaned temp dir.
- `cmd/sqlgen/gen/gen.go` — new `Options.ProjectRoot` (write anchor for root-relative graph dirs; default `"."`).
- `cmd/sqlgen/gen/orchestrate.go` — `resolveAPIDir` (output.dir-relative) replaced by `resolveGraphDir(projectRoot, dir)` (root-relative); `buildOptions` sets `ProjectRoot: "."`; `PopulateAPIContextResolverFields` retires FIX-071 — resolver / sqlgenresolver import paths now `JoinModulePath(module, g.ResolverDir[/sqlgenresolver])` directly (no output.dir prefix). `ModelsImportPath` still tracks output.dir (unchanged).
- `cmd/sqlgen/testdata/examples/graphql/{sqlgen.yml,gqlgen.yml}` — `schema_dir` / `resolver_dir` `./graph` → `./models/graph` (reproduces the same nested location; goldens byte-identical).

**Verification:** `make check` clean (all modules, `-short`); `make check-examples` clean (all 8 example modules — graphql/tests ~33.5s); `TestE2EGoldenFiles/graphql` byte-clean in non-short mode. No stray source-tree writes after a full `go test ./cmd/sqlgen/...` run (the cli-test leak below was caught by the auto-review and fixed). test-integration not re-run (no runtime SQL path touched — graph-dir change is codegen path/import wiring only).

**Auto-review follow-ups (both addressed before `/done`):** (1) HIGH — `go test ./cmd/sqlgen/cli/...` leaked a generated `cmd/sqlgen/cli/graph/` tree into the repo, because three chained-graphql generate tests ran from the cli package dir with a relative `schema_dir`; fixed by chdir'ing those tests into their temp project root. (2) minor — the disabled-API validation gate and the emission gate disagreed on `api.enabled`; aligned `validateAPIConfig` to `!cfg.API.Enabled || !g.Enabled` to match `BuildAPIContext`.

**Carry-over (out of scope for 20.1, tracked elsewhere):**
- `cmd/sqlgen/manifest/builder.go:157` still uses the pre-§26.5.8 `graph_top_level` heuristic (`g.SchemaDir != "" && g.SchemaDir != "."`), which is now wrong for the root-relative model. Correcting it to "resolved graph dir not under `output.dir`" is **FIX-117** (Phase 18), explicitly deferred by this phase's scope rule.
- 20.2 (`graphql_top_level` E2E module) will exercise the top-level topology end-to-end; the golden harness only diffs the `output.dir` subtree today, so 20.2 owns any harness wiring needed to capture a sibling `graph/` tree. See the 20.2 "Harness-wiring notes" section for the full set of items 20.1 verification surfaced (golden-diff coverage, the in-place-vs-temp-remap graph import-path fragility, and the `reservedManifestArtifactDirs` literal-`graph` check).

**Verification (`/verify 20.1`, 2026-07-08):** independent re-check passed — 5/5 PRD requirements (§26.5.8 / §26.5.6 / §4.13) PASS, 3/3 required tests present, 0 guideline issues, 0 fixes logged. `sqlgen-reviewer` subagent in full agreement. `make check` clean; `TestE2EGoldenFiles/graphql` byte-clean; graphql example + `models/graph` package build clean; `resolveAPIDir` confirmed fully retired (no dangling refs); standalone `sqlgen graphql gen` path consistent. Ready for `/done` — confirmed complete.

---

## 20.2 Top-level E2E example module (`graphql_top_level`)

**PRD Reference:** §26.5.8 (top-level placement), §30.3 (breadcrumbs).
**Design Reference:** PRD §26.5.8 (Approach A).

**Module:** `cmd/sqlgen/testdata/examples/graphql_top_level/` (new module).

**Status:** Complete

**Depends on:** 20.1.

### Tasks

- [x] New example module `cmd/sqlgen/testdata/examples/graphql_top_level/` mirroring the graphql example schema, **trimmed to ~2–3 tables** (only enough to prove topology + import resolution + breadcrumb paths — not re-prove resolver/walker/connection behavior the main graphql example covers). — 2 tables (`categories`, `products`) with one M2O relationship; global uuid/numeric overrides only (no cache/tenancy/soft-delete — those are re-proven by the `graphql` example).
- [x] `sqlgen.yml` sets `api.graphql.enabled: true` with `schema_dir: graph` / `resolver_dir: graph` (root-relative → `<root>/graph`, a sibling of `./models`). — done; `gqlgen.yml` paths mirror the top-level `graph/`.
- [x] Generate + commit the `expected/` golden tree (graph at `./graph`, sibling to `./models`). — captured; `expected/` holds the models contents at its root and the sibling `graph/` subtree. Verified import paths bake `<module>/graph[/model|/sqlgenresolver]` (not `<module>/models/graph`) — FIX-071 retirement exercised end-to-end.
- [x] `tests/` proving the module compiles (`go build ./...`) — validates import-path resolution for the top-level layout. — `tests/wiring_test.go` (`TestTopLevelGraphWiring`): constructs the top-level graph Resolver against the models client + sqlgenresolver Q/M and builds an ExecutableSchema; DB-free compile proof.
- [x] Wire into `make check-examples` / `TestE2EGoldenFiles`. — automatic: `make check-examples` discovers via `wildcard examples/*/go.mod`, `TestE2EGoldenFiles` via `sqlgen.yml`. **Harness extended** (`cmd/sqlgen/e2e_test.go`) to fold graph dirs resolving *outside* `output.dir` into the golden set — see below.

### Harness-wiring notes (carried over from 20.1 verification)

> **Two items 20.1 left inert that this module makes live. Neither is a 20.1 bug — they only surface once a graphql example emits a top-level (sibling) `graph/` tree, which is exactly what this module is.**

- **Golden diff must capture the sibling `graph/` tree.** The current `TestE2EGoldenFiles` only diffs the `output.dir` subtree (`findOutputDir` + `compareWithExpected` in `cmd/sqlgen/e2e_test.go`), so a top-level `./graph` sibling would generate but go **undiffed** — a silent gap that reads as "covered." Extend the harness to also diff the resolved graph dir when it lives outside `output.dir`.
- **Keep this module on the in-place `runGenerate` path — do NOT let it hit the temp-remap `runGenerateToDir`.** The remap path rewrites `output.dir` to a `t.TempDir()` and relies on `SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE` (FIX-062) to keep import paths stable. That override only rescues `ModelsImportPath` (via `apiImportDir`/`OriginalOutputDir`); the **graph** import path reads raw `g.ResolverDir` (`orchestrate.go` `PopulateAPIContextResolverFields`) with **no override analog**. Worse, if graph dirs were left **unset**, the default `filepath.Join(cfg.Output.Dir, "graph")` (`config.applyAPIDefaults`) would compute off the *rewritten temp* `output.dir` and bake an absolute `/var/folders/...` path into graph import paths — FIX-062 re-manifesting for graph, with models import paths staying stable while graph diverges. Two mitigations, both already how the nested graphql example dodges this: (1) set `schema_dir`/`resolver_dir` **explicitly** (this module does — `graph`), and (2) generate **in-place** at `ex.Path` (`isGraphQLExample` → `runGenerate`, matching the graphql branch). If a future change ever needs a *remapped* graphql example, add a graph-side import override mirroring `SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE` first.
- **`reservedManifestArtifactDirs` still hardcodes the literal `"graph"`** (`config/validate.go`). Under root-relative dirs the manifest `markdown_dir`-vs-graph collision check should compare against the *resolved* graph dir (and only when GraphQL is enabled), per design note PRD §26.5.8 Not blocking for topology/compile; fold into the 18.x manifest-validation pass (adjacent to FIX-117).

### Acceptance Criteria

- The module generates graph at `./graph` (sibling to `./models`) and **compiles** — proving root-relative import paths resolve for a top-level layout.
- Golden tree matches byte-for-byte under `TestE2EGoldenFiles`.
- Breadcrumb `graph/CLAUDE.md` back-pointer resolves to `../models/manifest/_index.md` (the top-level relative path) once manifest emission lands (assertion deferred to 18.9 — see below).

### Tests Required

- [x] `TestE2EGoldenFiles` covers the new module (topology + compile — assertions 1–2 from the design note). — `TestE2EGoldenFiles/graphql_top_level` passes byte-clean; the extended harness diffs both the `models/` tree (expected root) and the sibling `graph/` tree (expected/graph).
- [x] `go build ./...` green for the module. — `GOWORK=off go build ./...`, `go vet ./...`, and `go test ./tests/` all green.

> **Ownership split (design note §10):** 20.2 owns the *topology + compile* proof (assertions 1–2). The *manifest* assertions (3–5: `generation_config.graph_top_level`, breadcrumb relative path, `Client.Manifest()` round-trip + `sqlgen manifest validate`) are added to **both** examples' `tests/manifest_test.go` in **Phase 18.9**, on top of a topology that already generates + compiles.

### Completion Record

**Landed 2026-07-08.** Top-level GraphQL output-dir E2E proof (§26.5.8, Approach A).

**New module `cmd/sqlgen/testdata/examples/graphql_top_level/`:**
- `schema.sql` — 2 tables (`categories` BIGSERIAL PK; `products` UUID PK, M2O → categories, NUMERIC price). Trimmed to just enough to emit a graph package with one relationship resolver.
- `sqlgen.yml` — `api.graphql.enabled: true`, `schema_dir: graph` / `resolver_dir: graph` (root-relative → top-level `./graph`, sibling of `./models`); global uuid → `uuid.UUID` + numeric → `decimal.Decimal` overrides (the two scalars the schema needs). No cache / tenancy / soft-delete — re-proven by the `graphql` example, out of scope here.
- `gqlgen.yml` — consumer-owned; every path mirrors the top-level `graph/` (`graph/*.graphqls`, `graph/generated_gen.go`, `graph/model/…`, resolver `dir: graph`).
- `generate.go` (`//go:generate sqlgen graphql gen`), `tools.go` (pins gqlgen + gqlparser, mirrors the graphql example), `README.md`.
- `go.mod` / `go.sum` — derived from the graphql example, module path renamed, `go mod tidy` pruned to the actual dependency set (gqlgen, gqlparser, uuid, pgx, decimal, x/sync, sqlgen replace). `replace github.com/teandresmith/sqlgen => ../../../../..` (same nesting depth as the graphql example).
- `expected/` golden tree — models contents at the expected root **plus** a sibling `graph/` subtree.
- `tests/wiring_test.go` — `TestTopLevelGraphWiring`: DB-free compile proof; constructs the top-level `graph.Resolver{Client, Q, M}` and builds an `ExecutableSchema`, exercising the `<module>/graph[/model|/sqlgenresolver]` import paths.

**Harness change (`cmd/sqlgen/e2e_test.go`) — the one non-mechanical piece:**
- The golden diff previously only walked `output.dir`, so a top-level sibling `graph/` would generate but go **undiffed** (the silent gap 20.1 verification flagged). Added `siblingGraphDirs` + `extractGraphDirs` (line-based, mirroring `apiGraphQLEnabled`): they read `api.graphql.schema_dir` / `resolver_dir`, resolve root-relative, and return the ones that land **outside** `output.dir`. `copyOutputToExpected` and `compareWithExpected` now take a `siblings` map and fold each sibling tree in under its root-relative path (`expected/graph/…`).
- **Inert for every existing example:** graph nested under `output.dir` (the `graphql` example's `models/graph`) yields an empty `siblings` map → identical behavior. Confirmed by a full `TestE2EGoldenFiles` run (all examples byte-clean, 38.7s) and `git status` showing the `graphql` example untouched.

**Verification:** `go build ./...` / `go vet ./...` / `go test ./tests/` green for the new module (GOWORK=off, no Docker). `TestE2EGoldenFiles/graphql_top_level` byte-clean; full `TestE2EGoldenFiles` byte-clean across all examples. `cmd/sqlgen` module: vet clean, `go test -short -race ./...` green, `golangci-lint` 0 issues. New module + cmd/sqlgen `gofumpt -d` clean; new-module `golangci-lint` 0 issues. Docker `test-examples` sweep across other (untouched, byte-clean) modules not re-run — the new module's test is DB-free and its full example-check pipeline (fmt/lint/vet/test) is green; no runtime SQL path touched. No `/security-review` trigger (changed paths are the test harness + a testdata example — neither an embed target nor `cli/**`).

**Manifest breadcrumb assertions (design note §10 assertions 3–5) intentionally deferred to Phase 18.9**, layered on this topology once manifest emission lands. `graph_top_level` inference remains **FIX-117** (Phase 18).

---

## Follow-on (not in this phase)

- **FIX-117 (Phase 18):** after 20.1 lands, the manifest builder infers `generation_config.graph_top_level` = "resolved graph dir not under `output.dir`" (PRD §30.4.3). Resolves FIX-117; gates the rest of the 18.2 FIX batch (115/116/118/119/120).
- **18.9:** manifest E2E assertions on `graphql_top_level` (see ownership split above).
