# SQLGen — CI & Local Checks

> CI pipeline configuration, linting rules, test matrix, multi-module testing, release builds, and local verification.
> Read this before modifying CI workflows, linter config, or release infrastructure.

---

## Table of Contents

- [1. Principles](#1-principles)
- [2. CI Pipeline Overview](#2-ci-pipeline-overview)
- [3. Test Matrix](#3-test-matrix)
- [4. Multi-Module Testing](#4-multi-module-testing)
- [5. Linting](#5-linting)
- [6. Module Boundary Enforcement](#6-module-boundary-enforcement)
- [7. Race Detection & Leak Profiling](#7-race-detection--leak-profiling)
- [8. Golden File Verification](#8-golden-file-verification)
- [9. Generated Code Freshness](#9-generated-code-freshness)
- [10. Semantic Versioning](#10-semantic-versioning)
- [11. Automated Release Management](#11-automated-release-management)
- [12. Binary Distribution](#12-binary-distribution)
- [13. Running CI Locally](#13-running-ci-locally)
- [14. Exit Codes](#14-exit-codes)

---

## 1. Principles

1. **CI is the single source of truth.** If it passes in CI, it ships. If it fails in CI, it does not. No manual overrides, no "it works on my machine."
2. **Fast feedback first.** Unit tests and linting run before integration tests. Failures are caught in seconds, not minutes.
3. **Every module is tested independently.** The three-module split (runtime, parser, CLI) means each module has its own `go.mod` and its own CI test run. A change to the parser module cannot silently break the runtime module.
4. **Cross-platform binaries.** The CLI ships pre-built binaries for Linux and macOS on amd64 and arm64, and for Windows on amd64.
5. **No skipping hooks.** CI runs with the same hooks and checks that developers run locally. No `--no-verify`, no `--skip-ci`.

---

## 2. CI Pipeline Overview

GitHub Actions runs three workflows on every pull request and every push to `main` (the maintainer commits to `main` directly, so the push trigger is what tests most changes):

```
Pull request / push to main
    │
    ├── Lint              ← golangci-lint × 8 modules, plus refs-check and tracker-check
    │
    ├── Test (Unit)       ← go test -short -race × 8 modules (no containers)
    │       │
    │       ▼
    ├── Test (Integration) ← go test -race × 8 modules (testcontainers), once every unit job passes
    │
    └── Examples          ← lint + test × every example module
                            (skips docs-only changes)
```

**Module matrices (`lint.yml`, `test.yml`)** match `MODULES` in the `Makefile` and the `use` list in `go.work`. A new sub-module goes in all four places together.

**Example matrix (`examples.yml`)** runs every module under `cmd/sqlgen/testdata/examples/*/` against real databases. A `discover` job lists the directories that hold a `go.mod`, the same glob as the Makefile's `EXAMPLE_DIRS`, so a new example is picked up without editing the workflow.

### Workflow Files

The YAML lives in `.github/workflows/`; read it there rather than copying it here, where copies go stale.

| File | Jobs | Notes |
|------|------|-------|
| `lint.yml` | `golangci-lint` per module; `repo-checks` | golangci-lint also runs the gofumpt and goimports formatters. `repo-checks` runs `make refs-check` and `make tracker-check`. |
| `test.yml` | `unit`, then `integration`, per module | `integration` has `needs: unit`. |
| `examples.yml` | `discover`, then `lint` and `test` per example | `GOWORK: off`, and `paths-ignore` for docs-only changes. |
| `release-please.yml` | `release-please`, `tag-modules`, then `release` | Calls `release.yml` when a release is created. See [Section 11](#11-automated-release-management). |
| `release.yml` | `build` per target, then `publish` | Native cgo builds on five runners. Runs when `release-please.yml` calls it, or by hand. |
| `probe.yml` | `probe` per target | Runs by hand, and on a push to `main` that changes `probe.yml` or `release.yml`. Builds the CLI and runs the parser tests on the five release runners; see [Native Builds](#native-builds). |

Every workflow sets `permissions: contents: read` at the top and widens it per job only where needed (the release and tagging jobs). PR runs cancel when a newer commit lands on the same branch; pushes to `main` never cancel.

### Key Details

- Go version is read from `.tool-versions` via `setup-go`'s `go-version-file`, the same pin `make tools-check` compares against locally. The module cache key is the module's `go.sum` plus `go.work.sum`.
- Action majors: `actions/checkout@v7`, `actions/setup-go@v7`, `golangci/golangci-lint-action@v9`, `googleapis/release-please-action@v5`, `actions/upload-artifact@v7`, `actions/download-artifact@v8`. Check each one's latest release when touching a workflow.
- Example jobs set `GOWORK: off`. Each example's `go.mod` has `replace github.com/teandresmith/sqlgen => ../../../..`, and the repo-root `go.work` would override it.
- The examples matrix is wide (12 examples × 2 = 24 runners), and most of them start testcontainers, so docs-only changes skip it.
- The golangci-lint version is read from `.tool-versions` (see [Section 5](#5-linting)) by a step that runs at the repo root, not from a literal in either YAML. The action's own `version-file:` input cannot be used here: it resolves the path relative to `working-directory`, and every lint leg sets that to a module or example directory.
- Unit tests use `-short` to skip integration tests (no containers needed). Modules without testcontainer-backed tests (`event/natsbus`, `cache/memory`, `cache/msgpack`, `metrics/otel`) run their full suite in both jobs — redundant but cheap; the alternative is per-module branching that's harder to keep in sync.
- Integration tests run without `-short` — testcontainers spins up PostgreSQL, MySQL, and Redis (`cache/redis`). SQLite uses in-memory databases. NATS (`event/natsbus`) uses an in-process server, no container.
- Both test jobs use `-race` to detect data races.
- Both test jobs use `-count=1` to disable test caching.
- The matrix list in both YAMLs must stay in sync with `MODULES` in the `Makefile`.
- Both test jobs have `-timeout=30m`, matching `TEST_TIMEOUT` in the `Makefile`. The bound exists to catch a hang, not to pace a normal run: `cmd/sqlgen/cli` alone takes 7-10 minutes under `-race`, so Go's 10m default sits inside the noise band and fails a clean tree under load. Change the two together — the workflows do not read the `Makefile`.
- The lint job is a required check — all violations must be resolved before merging.

---

## 3. Test Matrix

### Test Types and When They Run

| Type | Command | When | What It Tests |
|------|---------|------|--------------|
| Unit | `go test -short -race -timeout=30m ./...` | Every PR, fast | Logic, builders, type resolution, config validation, template output |
| Integration | `go test -race -timeout=30m ./...` | Every PR, after unit | Real database operations via testcontainers |
| Golden file | Part of unit tests | Every PR | Template output matches committed golden files |
| Lint | `golangci-lint run --timeout=5m` | Every PR | Code quality, style, module boundaries, security |
| Freshness | `sqlgen diff` (exit code check) | Optional CI step | Generated code matches current schema + config |

### Database Test Infrastructure

| Database | Container | Test Helper |
|----------|-----------|-------------|
| PostgreSQL | `postgres:16-alpine` | `testutil.NewPostgres(t)` → `*pgxpool.Pool` |
| MySQL | `mysql:8` | `testutil.NewMySQL(t)` → `*sql.DB` |
| SQLite | In-memory (no container) | `testutil.NewSQLite(t)` → `*sql.DB` |

Containers are created once per test package via `TestMain` and cleaned up automatically. Each test gets its own schema (PostgreSQL) or database (MySQL) for isolation.

---

## 4. Multi-Module Testing

The project has **eight Go modules**, listed in the `Makefile`'s `MODULES` variable. Three are the primary architectural modules; the remaining five are optional sub-modules with their own external dependencies (NATS, Redis, msgpack, OpenTelemetry, in-process LRU).

| # | Module | Path | Architectural Role | Dependencies | CGO | Test Style |
|---|--------|------|--------------------|--------------|:---:|------------|
| 1 | Runtime | `.` | Core (imported by generated code) | stdlib + pgx + errgroup | No | Unit + integration (testcontainers) |
| 2 | Parser | `parser/` | DDL parsing | pg_query_go (CGO), vitess, rqlite | Yes (Postgres parser) | Unit |
| 3 | CLI | `cmd/sqlgen/` | The `sqlgen` binary | runtime + parser + config + gen | Yes (transitively) | Unit + golden + integration (testcontainers) |
| 4 | NATS event bus | `event/natsbus/` | Optional event transport | runtime + nats.go | No | Unit (in-process NATS server) |
| 5 | In-process cache | `cache/memory/` | Optional cache backend | runtime + LRU lib | No | Unit |
| 6 | Redis cache | `cache/redis/` | Optional cache backend | runtime + redis client | No | Unit + integration (Redis testcontainer) |
| 7 | MsgPack serializer | `cache/msgpack/` | Optional cache serializer | runtime + msgpack lib | No | Unit |
| 8 | OpenTelemetry metrics | `metrics/otel/` | Optional observability | runtime + otel | No | Unit |

All eight modules are in the CI matrix and in `make test` / `make lint` / `make check`. The matrix in `.github/workflows/test.yml` and `.github/workflows/lint.yml` must stay in sync with `MODULES` in the `Makefile`.

### Why Independent Testing Matters

- A change to `sql/builder.go` (runtime module) should not require CGO to test.
- A change to `parser/postgres/` should not pull in the code generator.
- A change to `cache/redis/` should not require pulling in NATS or OpenTelemetry — and a regression there should fail PR CI, which it currently doesn't.
- CI matrix runs each module in its own job with its own `go.mod`.

### Module-Specific Test Suites

| Module | Key Test Areas |
|--------|---------------|
| Runtime | SQL builders (all dialects), comparators, omittable, transactions, error mapping, struct scanning, hooks, tenancy, cache abstraction |
| Parser | DDL parsing per dialect, relationship detection, introspection, multi-file parsing, migration filtering |
| CLI | Config validation, code generation, template output, golden files, CLI commands, exit codes, GraphQL pipeline |
| `event/natsbus/` | NATS publisher/subscriber against a `nats:latest` testcontainer |
| `cache/memory/` | LRU eviction, TTL, concurrent access |
| `cache/redis/` | Read-through, breaker behavior against a `redis:7-alpine` testcontainer |
| `cache/msgpack/` | Serializer round-trip for every supported value shape |
| `metrics/otel/` | Metric emission per cache / DB hook |

---

## 5. Linting

### Toolchain Pin

File: `.tool-versions` at project root — asdf/mise format, and the single source of truth
for tool versions:

```
golang 1.27.1
golangci-lint 2.13.2
gofumpt 0.12.0
```

Three consumers read it: `make tools-check` (which `make check` and `make check-examples`
run first, warning but not failing on drift), the lint jobs in `lint.yml` and
`examples.yml`, and the `/implement`, `/verify`, `/fix-implement` and `/close-phase`
preflights. Bump the version here and CI follows; there is no second copy to update.

**golangci-lint must be built with a Go at or above the repo's `go.mod` floor.** The
released binaries are prebuilt, so the runner's `setup-go` version does not affect this —
only the pin does. A binary built with an older Go refuses to start at all:

```
Error: can't load config: the Go language version (go1.26) used to build golangci-lint
is lower than the targeted Go version (1.27.0)
```

Check with `golangci-lint --version`, which reports the Go it was built with. **Any move
of the `go.mod` floor must be paired with a `.tool-versions` bump**, or every lint leg
fails before linting anything.

The GitHub Action major is pinned separately, in the workflow YAML. It tracks
golangci-lint's own major: `golangci-lint-action@v6` and earlier support golangci-lint v1
**only** and reject a v2 version during resolution, before any binary is downloaded; v7+
support v2 only. Since `.golangci.yml` is `version: "2"`, the action must stay at v7 or
later — this repo pins `@v9`, the first major on the node24 runtime.

### Configuration

File: `.golangci.yml` at project root.

```yaml
version: "2"

run:
  timeout: 5m
  modules-download-mode: readonly
```

### Enabled Linters (19 linters + 2 formatters = 21 total)

In golangci-lint v2, `gofumpt` and `goimports` moved from `linters` to the `formatters` section.

**Correctness:**

| Linter | Purpose |
|--------|---------|
| `errcheck` | Unchecked errors in template execution, file I/O, SQL operations |
| `govet` | Struct tag issues — critical for generated `db`/`json` tags |
| `staticcheck` | Impossible type assertions, deprecated APIs, unreachable code |
| `nilerr` | Catches `if err != nil { return nil }` |
| `errorlint` | Enforces `errors.Is()` / `errors.As()` instead of `==` |
| `bodyclose` | HTTP response body leaks in introspection code |
| `exhaustive` | `switch` on `ConstraintType`, `MutationOp`, dialect types must cover all cases |

**Module Boundary:**

| Linter | Purpose |
|--------|---------|
| `depguard` | Protects the 3-module architecture (see [Section 6](#6-module-boundary-enforcement)) |
| `gomoddirectives` | Catches accidental `replace` directives that should not ship |

**Error Handling:**

| Linter | Purpose |
|--------|---------|
| `wrapcheck` | Errors from external packages must be wrapped with context |

**Style:**

| Linter | Purpose |
|--------|---------|
| `revive` | `exported` (doc comments), `unexported-return`, `context-as-argument`, `error-return` |
| `misspell` | Typos in comments and strings — matters for generated user-visible code |

**Quality:**

| Linter | Purpose |
|--------|---------|
| `gocritic` | Unnecessary conversions, append to nil, redundant `fmt.Sprint` |
| `prealloc` | Slice preallocation for batch operations and context building |
| `unconvert` | Unnecessary type conversions |
| `unparam` | Unused function parameters |
| `cyclop` | Max complexity: 15 |
| `funlen` | Max lines: 100, max statements: 60 |

**Security:**

| Linter | Purpose |
|--------|---------|
| `gosec` | SQL injection patterns, hardcoded credentials, weak crypto |

**Formatters (v2 separate section):**

| Formatter | Purpose |
|-----------|---------|
| `gofumpt` | Stricter `gofmt` — consistent formatting beyond the standard |
| `goimports` | Import grouping: stdlib, external, internal |

### Linter Settings

```yaml
linters:
  settings:
    cyclop:
      max-complexity: 15
    funlen:
      lines: 100
      statements: 60
    exhaustive:
      default-signifies-exhaustive: true
    revive:
      rules:
        - name: exported
          arguments: [checkPrivateReceivers]
        - name: unexported-return
        - name: context-as-argument
        - name: error-return
```

### Excluded Linters

| Linter | Why Excluded |
|--------|-------------|
| `lll` | Template strings and SQL builder code produce long lines |
| `godox` | Overly noisy during active development |
| `wsl` | Subjective whitespace style, causes churn |
| `nlreturn` | Style preference, not correctness |
| `varnamelen` | Conflicts with idiomatic Go: `ctx`, `db`, `tx`, `pk`, `fk`, `m`, `q` |
| `ireturn` | Conflicts with `{Table}Client` interface return pattern |

### Exclusions

In v2, exclusions moved under `linters.exclusions`:

```yaml
linters:
  exclusions:
    paths:
      - testdata
    rules:
      - path: _test\.go
        linters: [funlen, cyclop, wrapcheck]
      - path: _gen\.go
        linters: [funlen, cyclop, gocritic, revive]
    generated: strict
```

Test files are relaxed on length, complexity, and error wrapping. Generated golden files skip quality and style linters (generated methods can be long). The `generated: strict` setting skips files with the standard generated-code header.

### nolint Policy

`nolint` directives are permitted only with a justification comment:

```go
//nolint:errcheck // bytes.Buffer.Write cannot fail
_, _ = buf.WriteString(clause)
```

Bare `//nolint` without a reason is rejected.

---

## 6. Module Boundary Enforcement

`depguard` enforces the 3-module architecture at the linter level. These rules prevent accidental imports that would break the module isolation guarantees.

```yaml
depguard:
  rules:
    runtime-no-parser:
      files:
        - "!**/parser/**"
        - "!**/cmd/**"
      deny:
        - pkg: "github.com/pganalyze/pg_query_go/v6"
          desc: "runtime module must not import parser dependencies (CGO)"
        - pkg: "github.com/vitessio/vitess"
          desc: "runtime module must not import parser dependencies"
        - pkg: "github.com/rqlite/sql"
          desc: "runtime module must not import parser dependencies"

    no-pgx-in-stdlib:
      files:
        - "**/database/stdlib/**"
      deny:
        - pkg: "github.com/jackc/pgx/v5"
          desc: "stdlib driver must not import pgx — consumers choose one driver"

    no-stdlib-in-pgx:
      files:
        - "**/database/pgx/**"
      deny:
        - pkg: "database/sql"
          desc: "pgx driver must not import database/sql — drivers are mutually exclusive"
```

### What These Rules Prevent

| Rule | Prevents |
|------|----------|
| `runtime-no-parser` | Runtime module importing CGO-dependent parser packages. Consumers would need a C compiler. |
| `no-pgx-in-stdlib` | stdlib adapter importing pgx. Consumers who choose stdlib should not pull in pgx. |
| `no-stdlib-in-pgx` | pgx adapter importing database/sql. Drivers are mutually exclusive. |

---

## 7. Race Detection & Leak Profiling

### Race Detection

All test runs use `-race`. This is non-negotiable.

```bash
go test -race ./...
```

Packages with goroutine safety requirements:
- `database/` — the transaction engine carries two locks with different scopes. The `sync.Mutex` guards the `Tx` struct's own state (`IsClosed`, `OnCommit`, savepoint depth); the `connSem` reservation makes concurrent *statements* on one transaction safe by serializing them against the transaction's single connection. Tests here assert both, and `reservation_integration_test.go` in each adapter is where the second lives. What is still **not** safe, and what no test may assert, is opening two savepoint *scopes* on one `txCtx` concurrently — the reservation serializes statements, not scopes (PRD §18.5)
- Generated client — concurrent `Get`/`GetMany`/`Create` from multiple goroutines
- `omittable/` — concurrent `Set`/`Get`

### Goroutine Leak Detection

As of Go 1.27 `goroutineleak` is a predefined `runtime/pprof` profile, so CI needs no `GOEXPERIMENT` flag:

```bash
go test -race ./...
```

Tests that want the report write the profile directly:

```go
pprof.Lookup("goroutineleak").WriteTo(w, 1)
```

This detects goroutines blocked on primitives that can never be unblocked — particularly valuable for concurrent relationship loading with `errgroup` and transaction lifecycle management.

---

## 8. Golden File Verification

Golden files are committed to the repository and compared against fresh generation output in CI.

### How It Works

1. CI runs `go test ./...` in the CLI module.
2. Example tests in `testdata/examples/` generate code from each example's schema.
3. Generated output is diffed against committed golden files in `expected/`.
4. Any difference fails the test.

### Updating Golden Files

When a template change intentionally alters output:

```bash
make update-golden
```

Review the diff carefully in the PR — it shows the exact impact across all example scenarios.

---

## 9. Generated Code Freshness

The `sqlgen diff` command verifies that committed generated code matches the current schema and config:

```bash
sqlgen diff
```

- Exit `0`: generated code is up to date.
- Exit `1`: changes detected — generated code is stale.

This can be added as a CI step to catch PRs that modify schema or config without regenerating:

```yaml
- name: Verify generated code is fresh
  run: sqlgen diff
```

---

## 10. Semantic Versioning

All releases follow [Semantic Versioning 2.0.0](https://semver.org/) (`MAJOR.MINOR.PATCH`).

### Version Format

```
v<MAJOR>.<MINOR>.<PATCH>
```

Examples: `v1.0.0`, `v1.3.2`, `v2.0.0`

### What Triggers Each Increment

| Increment | When | Examples |
|-----------|------|---------|
| **MAJOR** | Breaking changes to generated code, runtime API, config schema, or CLI flags | Removing a sentinel error, changing `Querier` interface, renaming config fields, changing generated method signatures |
| **MINOR** | New features, new config options, new generated operations, new dialect support | Adding cursor pagination, adding a new comparator type, adding `events` config |
| **PATCH** | Bug fixes, performance improvements, documentation, dependency updates | Fixing MySQL error parsing, correcting placeholder position, updating testcontainer versions |

### What Counts as a Breaking Change

Because sqlgen spans three modules and generates code, breaking changes can appear in several places:

| Surface | Breaking | Not Breaking |
|---------|----------|-------------|
| **Generated code** | Method signature change, struct field removal/rename, import path change | Adding a new method, adding a new struct field |
| **Runtime API** | Interface method change, sentinel error removal, type change | Adding a new exported function, adding a new error |
| **Config schema** | Removing/renaming a field, changing default behavior | Adding a new optional field with a backward-compatible default |
| **CLI** | Removing a command/flag, changing exit code semantics | Adding a new command/flag, adding a new exit code |

### Conventional Commits

All commits to `main` must follow the [Conventional Commits](https://www.conventionalcommits.org/) specification. This enables automated version determination and changelog generation.

#### Format

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

#### Types

| Type | Version Bump | Purpose |
|------|:------------:|---------|
| `feat` | MINOR | New feature |
| `fix` | PATCH | Bug fix |
| `docs` | — (no release) | Documentation only |
| `chore` | — (no release) | Maintenance (deps, CI config, tooling) |
| `refactor` | — (no release) | Code change that neither fixes a bug nor adds a feature |
| `test` | — (no release) | Adding or correcting tests |
| `perf` | PATCH | Performance improvement |
| `ci` | — (no release) | CI configuration changes |

#### Breaking Change Footer

A `BREAKING CHANGE:` footer on any commit type triggers a **MAJOR** bump:

```
feat(config)!: rename `query_limit` to `default_limit`

BREAKING CHANGE: The `generation.query_limit` config field has been renamed
to `generation.default_limit`. Update your sqlgen.yml files.
```

The `!` after the type/scope is shorthand for the same thing.

#### Scopes

Use scopes to identify the affected area:

| Scope | Area |
|-------|------|
| `sql` | SQL builders, dialect interface |
| `comparator` | Filter operators |
| `omittable` | Value[T] wrapper |
| `database` | Querier, transactions, scanning, driver adapters |
| `parser` | DDL parsing, introspection |
| `gen` | Code generation, templates |
| `config` | Configuration loading, validation |
| `cli` | CLI commands, flags |
| `deps` | Dependency updates |

#### Examples

```
feat(gen): add cursor pagination template

fix(database): correct MySQL constraint name parsing for 1062 errors

feat(comparator)!: change Slice comparator to use type parameter

BREAKING CHANGE: Slice comparator now requires a type parameter.
Generated code must be regenerated.

chore(deps): update pg_query_go to v6.3.0

docs: add SQL.md dialect guidelines

perf(sql): preallocate argument slice in BuildMultiInsert
```

### Multi-Module Versioning

All eight modules release together under one version. Release Please tags the root (`v0.3.0`); the `tag-modules` job then tags every other module at the same version with Go's path-prefixed form (`parser/v0.3.0`, `cmd/sqlgen/v0.3.0`, `cache/redis/v0.3.0`, …), which is what `go get` and `go install` resolve for a submodule.

A module that imports no other module of this repo (`parser`) is tagged at the release commit. Every other module is tagged one commit later, on a pin commit that sets its in-repo requirements to the release version and adds the matching `go.sum` entries. `go install …/cmd/sqlgen@v0.3.0` refuses a module whose `go.sum` is incomplete, so the pins cannot be skipped. The pin commit is reachable only through those tags and is never pushed to a branch.

On `main`, no module requires another module of this repo; `go.work` resolves them locally. A `GOWORK=off` build of `cmd/sqlgen` or a plugin module therefore fails on `main` and works at its tag. Consumers pin one version across modules:

```
require (
	github.com/teandresmith/sqlgen v0.3.0
	github.com/teandresmith/sqlgen/cache/redis v0.3.0
)
```

The CLI binary embeds the same version string, so `sqlgen --version` names the runtime release its generated code targets.

---

## 11. Automated Release Management

Releases are fully automated using [Release Please](https://github.com/googleapis/release-please). No manual tagging, no manual changelog editing.

### How It Works

1. Developers merge PRs with conventional commit messages into `main`.
2. Release Please maintains a **release PR** that tracks accumulated changes since the last release.
3. The release PR auto-updates with each merge — it bumps the version based on commit types and builds the changelog.
4. When ready to release, a maintainer merges the release PR.
5. Merging the release PR creates a **Git tag** (`v1.3.0`) and a **GitHub Release** with the generated changelog.
6. The same `release-please.yml` run tags the other seven modules (`tag-modules`, see [Multi-Module Versioning](#multi-module-versioning)) and then calls the **release workflow**, which builds the CLI on five native runners and attaches the binaries to the GitHub Release. It is called rather than tag-triggered because a tag pushed with `GITHUB_TOKEN` does not start other workflows.

### Release Please Workflow

File: `.github/workflows/release-please.yml`. It runs on every push to `main` and passes no `release-type` input: that input makes the action ignore `release-please-config.json` and `.release-please-manifest.json`, which it otherwise reads by default. When the run creates a release, `tag-modules` runs `.github/scripts/tag-modules.sh` with the new tag, and then the `release` job calls `release.yml` with it.

`tag-modules.sh` derives the module list from `go.work` and each module's in-repo imports from `go list`, so a new module needs no script change. It writes the pins with `go mod edit -require` and `go mod tidy`, not `go get`: under `GOPRIVATE`, `go get <path>@<version>` resolves the path as a package first and can settle on the root module, which does not contain a submodule's packages.

### Release Please Configuration

`release-please-config.json` at the project root sets the `go` release type, `CHANGELOG.md`, and the two pre-major bump rules (see [Pre-Major Version](#pre-major-version-v0xx)). `.release-please-manifest.json` tracks the current version; Release Please updates it.

### Changelog Generation

Release Please generates `CHANGELOG.md` automatically from conventional commits. The changelog groups entries by type:

```markdown
## [1.3.0](https://github.com/teandresmith/sqlgen/compare/v1.2.0...v1.3.0) (2026-04-07)

### Features

* **gen:** add cursor pagination template ([#142](https://github.com/teandresmith/sqlgen/issues/142)) ([abc1234](https://github.com/teandresmith/sqlgen/commit/abc1234))
* **comparator:** add JSONB path exists operator ([#138](https://github.com/teandresmith/sqlgen/issues/138)) ([def5678](https://github.com/teandresmith/sqlgen/commit/def5678))

### Bug Fixes

* **database:** correct MySQL constraint name parsing for 1062 errors ([#140](https://github.com/teandresmith/sqlgen/issues/140)) ([ghi9012](https://github.com/teandresmith/sqlgen/commit/ghi9012))
```

Do not edit `CHANGELOG.md` manually — it is managed by Release Please.

### Version in the Binary

`cmd/sqlgen/cli/version.go` declares `var Version = "dev"`, holding the release without its leading `v`. The release build's `ldflags` set it (`-X github.com/teandresmith/sqlgen/cmd/sqlgen/cli.Version=0.3.0`). A `go install …/cmd/sqlgen@v0.3.0` build has no ldflags, so the CLI reads the module version Go records in the binary instead. A build from a checkout records a pseudo-version (or `+dirty`), which names no release, so it keeps `dev`. `sqlgen --version` and the manifest's `generator.version` print the result. Release Please does not edit source files.

### Release + Build Pipeline

The full release flow chains two workflows and a script:

```
Push to main
    │
    ▼
Release Please (runs on every push to main)
    │
    ├── No releasable commits → updates release PR (or no-ops)
    │
    └── Release PR merged → creates root tag + GitHub Release
                                │
                                ▼
                          tag-modules (path-prefixed tag per module)
                                │
                                ▼
                          release.yml: build × 5 native runners → publish
                                │
                                ▼
                          Binaries + checksums.txt attached to the GitHub Release
```

### Release Workflow

File: `.github/workflows/release.yml`. Its `build` matrix checks out `cmd/sqlgen/<tag>` on each target's own runner, builds with `GOWORK=off` (the module graph `go install` resolves) and `CGO_ENABLED=1`, smoke-runs `sqlgen --version`, and archives the binary with `LICENSE` and `README.md`. Its `publish` job writes `checksums.txt` and uploads everything with `gh release upload`. Run it by hand (`workflow_dispatch` with the root tag) to rebuild a release's binaries.

### Pre-Major Version (v0.x.x)

While the project is pre-`v1.0.0`, Release Please is configured with `bump-minor-pre-major: true`:

- `feat` commits bump MINOR (`0.1.0` → `0.2.0`).
- `fix` commits bump PATCH (`0.2.0` → `0.2.1`).
- `BREAKING CHANGE` also bumps MINOR (not MAJOR) during pre-1.0 development.

After `v1.0.0`, breaking changes bump MAJOR as normal.

---

## 12. Binary Distribution

The CLI ships pre-built binaries for all major platforms, built by `release.yml`.

### Supported Platforms

| OS | Architectures |
|----|--------------|
| Linux | amd64, arm64 |
| macOS | amd64 (Intel), arm64 (Apple Silicon) |
| Windows | amd64 (Windows on ARM runs it under emulation) |

### Native Builds

The CLI requires CGO because the PostgreSQL parser (`pg_query_go/v6`) links against `libpg_query`. Cross-compiling cgo needs a C toolchain per target, including a macOS SDK, so each target builds on a runner of its own OS and architecture instead:

| Target | Runner |
|--------|--------|
| linux/amd64 | `ubuntu-latest` |
| linux/arm64 | `ubuntu-24.04-arm` |
| darwin/amd64 | `macos-15-intel` |
| darwin/arm64 | `macos-latest` |
| windows/amd64 | `windows-latest` |

There is no windows/arm64 build. The `windows-11-arm` runner's only C compiler is an x86-64 MinGW `gcc`, which cannot assemble the arm64 code cgo needs, so the probe failed there. Building it would take an arm64 toolchain such as llvm-mingw, plus finding out whether libpg_query builds with it.

`.github/workflows/probe.yml` builds the CLI and runs the parser tests on the same five runners. It runs on its own when a push to `main` changes `probe.yml` or `release.yml`, and by hand before relying on a target. `release.yml` and `probe.yml` list the same runners; change them together.

### Installation Methods

```bash
# Pre-built binary (recommended — no CGO toolchain needed)
# Download from GitHub Releases for your platform

# From source (requires CGO + C compiler)
go install github.com/teandresmith/sqlgen/cmd/sqlgen@latest
```

---

## 13. Running CI Locally

Before pushing, run the full CI suite locally to catch issues early.

### Quick Check (unit tests + lint)

```bash
# All 8 modules in one shot (recommended)
make tools-check  # warn on drift from the .tool-versions pin
make test         # -short -race across MODULES from Makefile
make lint         # golangci-lint across MODULES
```

`make check` and `make check-examples` both run `tools-check` first, so it only needs invoking
directly when checking the toolchain on its own.

For ad-hoc per-module work, the Makefile's `MODULES` variable lists every module — the targets iterate that list, so they stay accurate as the layout evolves.

### Full Suite (includes integration tests + E2E examples)

```bash
# Integration tests across all modules (requires Docker for testcontainers)
make test-integration

# End-to-end example modules (separate go.mods under cmd/sqlgen/testdata/examples/)
make check-examples   # lint + test for every example module
```

### Verify Generated Code

```bash
# Unit-level golden tests (template output)
go test ./cmd/sqlgen/gen/...

# E2E golden tree (per-example expected/ directories)
cd cmd/sqlgen && go test -run TestE2EGoldenFiles -count=1 -timeout=10m

# Generated code matches current schema
sqlgen diff
```

### Makefile Targets

| Target | What It Does |
|--------|-------------|
| `make test` | Run unit tests across all 8 modules (`-short -race -timeout=$(TEST_TIMEOUT)`) |
| `make test-integration` | Run full test suite across all 8 modules (`-race -timeout=$(TEST_TIMEOUT)`) |
| `make lint` | Run `golangci-lint` across all 8 modules |
| `make tools-check` | Warn on drift between installed tools and the `.tool-versions` pin |
| `make check` | `make tools-check` + `make lint` + `make test` — pre-push check |
| `make update-golden` | Regenerate unit-level template golden files |
| `make test-examples` | Test every E2E example module (real DBs, requires Docker) |
| `make lint-examples` | Lint every E2E example module |
| `make check-examples` | `make tools-check` + `make lint-examples` + `make test-examples` |
| `make update-golden-e2e` | Regenerate the `expected/` tree per example module |

> **Note**: The CI workflows in `.github/workflows/` matrix all 8 modules and run the same commands these targets dispatch locally — `make check` and PR CI should agree on pass/fail.

---

## 14. Exit Codes

### CLI Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Generation error (template failure, file write error) |
| `2` | Config error (invalid YAML, validation failure) |
| `3` | Schema error (parse failure, ambiguous names, constraint violations) |
| `4` | Connection error (database introspection failed) |

### CI-Relevant Command Behavior

| Command | CI Usage | Exit Behavior |
|---------|----------|--------------|
| `sqlgen diff` | Verify generated code is fresh | `0` = up to date, `1` = stale |
| `sqlgen lint --fail-on error` | Schema linting | `0` = no errors (warnings OK), `1` = errors found |
| `sqlgen validate` | Config validation | `0` = valid, `2` = invalid (all errors reported together) |
| `sqlgen generate --quiet` | Regenerate in CI | `0` = success, `1` = generation error |

---

## Appendix: CI Review Checklist

- [ ] All three modules tested independently (runtime, parser, CLI)
- [ ] Unit tests pass with `-short -race`
- [ ] Integration tests pass with `-race -timeout=30m`
- [ ] `golangci-lint` passes with `--timeout=5m` across all modules
- [ ] Golden files are up to date (`make update-golden` diff reviewed)
- [ ] `sqlgen diff` exits `0` (generated code is fresh)
- [ ] No `//nolint` without justification comment
- [ ] No new `depguard` violations (module boundaries intact)
- [ ] No new `gosec` findings (SQL injection, hardcoded secrets)
- [ ] Race detector passes on all concurrent code paths
- [ ] Release binaries build for all 5 platform targets (linux/darwin × amd64/arm64, windows/amd64)
- [ ] Commit messages follow Conventional Commits format
- [ ] Breaking changes have `BREAKING CHANGE:` footer
- [ ] `CHANGELOG.md` is not manually edited (managed by Release Please)
