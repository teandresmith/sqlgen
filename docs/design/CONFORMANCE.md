# PRD Conformance: Module Matrices, Cross-Dialect Suite & Requirement Registry — Design & Plan

> **Status:** draft for review (design record, not a plan of record). Rulings 1–14 settled
> 2026-10-23 (§9); no questions open. The §7 step 1 prototype measured the
> cross-dialect suite's `replace` plumbing as working (§4.2). Proposes three linked pieces:
> **module conformance matrices** (§3), which give every module (runtime, parser, CLI and the
> generated client) the axes its PRD sections vary over, so a missing test is an empty cell; a
> **cross-dialect conformance suite** (§4), one shared black-box suite for the generated client,
> run unchanged on every dialect against a real database; and a **requirement registry** (§5),
> one file per module, which ties each PRD behavior to the tests that prove it, fails when they
> drift apart, and reports which PRD sections have no requirements at all. Test and doc tooling
> only: no generated code, config field or runtime API changes.

## 1. Motivation

### 1.1 Behavior tests are written per dialect

Generated-client behavior is tested in `cmd/sqlgen/testdata/examples/*/tests`, and each dialect
module has its own copy:

| Module | Test files |
|---|---|
| `postgres` | 44 |
| `mysql` | 40 |
| `sqlite` | 40 |
| File names present in all three | 26 |

Some Postgres-only files are dialect-specific by nature (`matview`, `multi_schema`). Others look
dialect-neutral but exist only in `postgres/tests`: `no_pk`, `empty_batch`, `stream_tx`,
`update_skip_refetch`, `batch_tx_failure`. This comparison only matched file names and grepped
for test names, so MySQL or SQLite may cover some of these under other names. Either way, nothing
states which behaviors are guaranteed on which dialect. A behavior can be tested on Postgres and
only assumed on SQLite, and no check notices.

### 1.2 PRD behaviors are not traceable to tests

The PRD (about 16.4k lines) states behavior in prose and has no requirement IDs. `/verify`
rebuilds a checklist from that prose on every run, so two runs can disagree, and nobody can see
what a run missed. In the FIX tracker's `Surfaced by` field, `/verify`, sweeps and `/close-phase`
together account for most findings, far more than `/implement`. Gaps are found after the code is
written, not before.

### 1.3 Most defects come from modules a client suite cannot reach

Tallying the `Origin` field of the 254 resolved FIX entries by module (approximate; a few
origins name a doc or phase rather than a package):

| Origin | FIXes |
|---|---|
| `cmd/sqlgen/gen` | 90 |
| `parser` (all dialects, introspection) | 73 |
| `cmd/sqlgen/config` | 21 |
| `cmd/sqlgen/manifest` and runtime `manifest` | about 20 |
| Runtime packages (`sql`, `comparator`, `database`, `event`) | about 15 |
| Other CLI (`cli`, `gotype`, `wrapper`), docs, examples | the rest |

A cross-dialect suite for the generated client exercises `gen` output and the runtime, but it
never sees a parser that drops a constraint or a config rule that accepts what §4.13 rejects. It
only sees their downstream effects, if the conformance schema happens to trigger them. PRD
conformance needs a test matrix for every module, not just the client.

### 1.4 Goal

1. Every module has a conformance matrix (§3): rows and columns taken from what its PRD sections
   vary over. Every cell is a test or an explicit `n/a` with a reason.
2. Every exposed generated method has one written contract, tested once and run on every dialect
   (§4).
3. Every PRD behavior is listed in its module's registry file with its PRD section, the tests that
   prove it, and its status (§5). PRD sections with no requirements are reported.
4. A fast check (`make check`, no Docker) fails when a registry file and its tests disagree. A full
   run (`make conformance`, Docker) produces a committed report.

## 2. Scope

**In scope:** PRD conformance for every module of the three-module architecture and the
generated client:

| Registry file | Module / packages | PRD |
|---|---|---|
| `runtime.md` | `sql/`, `comparator/`, `omittable/`, `database/` and drivers, `cache/`, `tenancy/`, `event/`, `hook/`, `types/`, `manifest/` | §10, §11, §14, §15, §18, §19, §21, §22, §24, §27, §28, §29 (runtime), App. A, App. B |
| `parser/` | split by package (§7 step 6): `dialects.md` (`postgres`, `mysql`, `sqlite`), `schema.md` (`parser`), `introspect.md` (`introspect`) | §5, §6, §16.1, §16.2, §17.1 |
| `cli/` | `cmd/sqlgen/`, split by slice (§7 step 6): `config.md` (`config`), `gen.md` (`gen`), `naming.md` and `access-control.md` (§8.5 and §32, rows over `gen`), `gotype.md` (`gotype`), `api.md` and `api-schema.md` (§26, rows over `gen`), `commands.md` (`cmd/sqlgen`, `cli`, `wrapper`), `manifest.md` (`manifest`), `mcp.md` (`mcp`) | §4, §7, §8, §12, §13, §16, §23, §26, §30, §31, §32 |
| `client.md` | The generated client, through the §4 suite | §9, §17, §20, §25, §29 (generated behavior) |

Each test home stays where it is best placed:

| Test home | Used for | Speed |
|---|---|---|
| `unit: <dir>` | Pure functions: builders, comparators, naming, type resolution, config rules, parser cases | milliseconds |
| `integration: <dir>` | Drivers, transactions, error mapping, introspection against real databases | seconds, Docker |
| `golden: <example>` | Generated source text, GraphQL / REST / gRPC output, the manifest | seconds |
| `conformance` | Generated-client behavior on every dialect (§4) | seconds, Docker |
| `example: <example>` | Dialect- or variant-specific behavior with no cross-dialect contract | seconds, Docker |

**Out of scope:**

- **Behavior over time** (concurrency §18.5 / §20.5, watch mode §23.10, event transport delivery
  §28.7) gets no matrix rows. Where the PRD states a testable property of it, that property is
  still a registry row pointing at whatever test covers it.
- **Rationale and rejected alternatives** in the PRD (e.g. §21.10, §24.4) state no behavior and
  are marked `informative` in the section ownership map (§5.2), so the coverage report skips
  them.

## 3. Module conformance matrices

### 3.1 Rules

- A matrix's **rows** are the units its PRD sections enumerate: rules, operators, methods, DDL
  features, commands. Its **columns** are the axes those sections say behavior varies over:
  dialect, driver, nullability, source, table kind, outcome.
- **Every cell** is a registry ID (§5) or `n/a: <reason>`. An empty cell is a gap, and the report
  (§5.5) shows it.
- **A cell asserts only what the PRD promises.** Where a column doesn't apply to a row, the cell
  is `n/a` with the PRD reason. It is never a guessed expectation (§4.5 has the measured example).
- **A matrix is declared, not drawn.** Each registry file declares its matrices' rows and columns
  in a header block. The report renders them from the registry rows, so a matrix can't drift from
  the tests.
- **Dialect columns use capabilities** (§4.4). Where the PRD says a dialect behaves differently,
  that difference is the expectation, not a skip.

### 3.2 Runtime (`runtime.md`)

| Matrix | Rows | Columns | Test home | PRD |
|---|---|---|---|---|
| SQL builders | each builder in App. A | dialect × {basic, `RETURNING`, empty input, composite key, quoted identifier} | unit | App. A, §8.7 |
| Comparators | each comparator × operator | dialect × {value, nil, empty list, escaping} | unit | §11.2, §11.3 |
| Condition composition | `And` / `Or` / `Not` / `Exists` / prefixing shapes | dialect × {placeholder numbering, nesting depth} | unit | §11.5 |
| Lock modes | 5 modes | dialect | unit + integration | §9.6a |
| Transactions | `WithTransaction`, `NewTransaction`, savepoints, options, `IsClosed`, deferred callbacks | driver (pgx, stdlib-pg, mysql, sqlite) | integration | §18 |
| Error mapping | 6 error classes | driver | unit (synthetic driver errors) + integration (real violations) | §22.1, §22.2 |
| Cache keys and invalidation | key shapes, pattern invalidation, fingerprint inputs | {single PK, composite PK, schema / no schema, tenanted} | unit | §27.5, §27.9, §29.5 |
| Hooks | hook type × lifecycle point | {fires, order, skip, panic recovery} | unit | §21 |
| Omittable | `Value` states | {set, unset, null} × JSON round trip | unit | §10 |

### 3.3 Parser (`parser/`)

| Matrix | Rows | Columns | Test home | PRD |
|---|---|---|---|---|
| Schema model | each §5.1 element and attribute (23) | dialect × source (`files`, `database`); `both` is the merge rows, since it seeds the file parser with the introspected schema | unit (files) + integration (database) | §5.1, §6.1, §6.3 |
| Relationship detection | §5.3 FK shapes (22) | dialect, plus `source-parity`: detection is one function over the model | unit + integration | §5.3 |
| Soft-delete detection | the §4.6 default columns | `detected`, `outranks-lower`: detection matches names only | unit | §5.4 |
| Qualification | the §5.5 table's cases and its FK and view rules | dialect | unit | §5.5 |
| Determinism | each sorted element | shuffled input order → sorted model | unit | §5.7 |
| Introspection filtering | `schemas`, `exclude_schemas`, `tables` | dialect | integration | §6.4 |

`INTROSPECTION.md` §6.5's parity matrix already follows this pattern for the `files` vs
`database` axis: every cell is a passing test or a "—" with a reason. The parser registry absorbs
it rather than restating it.

### 3.4 CLI (`cli.md`)

| Matrix | Rows | Columns | Test home | PRD |
|---|---|---|---|---|
| Config validation | each §4.13 rule (about 85) | {fires, message text, severity, reported together with others, agrees with the JSON Schema} | unit | §4.13, §4.14 |
| Type resolution | each §7.2 SQL type | dialect × {not null, nullable} × {built-in, integration, column override, type override} | unit | §7.1–§7.6 |
| Naming | each §8.5 rule (about 60 table rows) | {struct, field, type name, file stem} | unit | §8.5 |
| Generated artifacts | each artifact in §8.2 / §8.3 | table kind ({single PK, composite PK, no PK, soft delete, tenanted, view, materialized view}) | unit + golden | §8.2, §8.3, §8.6, §9.4b, §16.4 |
| Relationships (codegen) | relationship type (o2o, o2m, m2o, m2m, self, circular, polymorphic) | {struct field and tags, loader, filter, nested mutation} | unit + golden | §13, §9.9 |
| Field selection and access control | column access role | the §32.2 surfaces ({Go client, API in, API out, filter, sort, event payload, cache, manifest}); REST, not generated yet, has rows outside the matrix | unit + golden | §12, §32 |
| Commands | each command (`generate`, `validate`, `init`, `diff`, `lint`, `completion`, `manifest …`) | outcome ({success, config error, schema error, generation error}) × {exit code, output} | unit | §23 |
| API generation | GraphQL / REST / gRPC surface element | table kind | golden | §26 |
| Manifest and MCP | manifest field / MCP tool | {present, shape, error code} | unit + golden | §30, §31 |

### 3.5 Generated client (`client.md`)

Rows: the 28 generated methods, declared as the matrix's closed row list (§5.1).

| Group | Methods |
|---|---|
| Reads | `Get`, `GetMany`, `Count`, `Exists`, `ExistsWhere`, `Stream`, `Paginate`, `Connection` |
| Writes | `Create`, `CreateMany`, `Update`, `UpdateMany`, `UpdateWhere`, `Upsert`, `UpsertMany`, `Increment` |
| Nested writes | `CreateWithRelated`, `UpdateWithRelated`, `UpsertWithRelated` |
| Soft delete | `SoftDelete`, `SoftDeleteMany`, `SoftDeleteWhere`, `Restore`, `RestoreMany`, `RestoreWhere` |
| Hard delete | `HardDelete`, `HardDeleteMany`, `HardDeleteWhere` |

Columns: the cross-cutting concerns every method must get right, each run on every dialect
through the §4 suite.

| Concern | PRD |
|---|---|
| Happy path | §9.1–9.4 |
| Nil / empty input | §9.4c |
| Not found / no match | §9.5 |
| Soft-delete interaction (excluded by default, opt-in to see) | §17.3 |
| Inside a transaction; rollback | §18 |
| Hooks fire with the right `Op` and input | §21 |
| Error mapping (unique, FK, check, not-null) | §22.2 |
| Field selection | §12 |
| Lock mode (reads) | §9.6a |
| Composite-PK variant | §8.7 |

There is no context-cancellation column. The PRD states cancellation behavior only in specific
places: the transaction `Timeout` (§18.2), deferred-callback contexts (§18.6) and the errgroup
relationship loads (§20.5). Those are registered under their own sections. A general per-method
cancellation contract is a PRD gap, logged in `backlog.md` (§9 ruling 3).

## 4. Cross-dialect conformance suite

The test home for `client.md`.

### 4.1 Conformance schema

One small logical schema, written in each dialect's DDL. Each table exists to exercise one shape
of key or feature:

| Table | Exercises |
|---|---|
| `products` | Single PK, every common column type, filters, sorting, pagination |
| `order_items` | Composite PK (`<T>PK` struct, tuple `IN` batches) |
| `documents` | Soft delete (timestamp); CHECK constraints a soft delete and a restore can violate |
| `revisions` | Composite-PK soft delete (timestamp) |
| `labels` | Case-insensitive text PK (PostgreSQL `citext`, MySQL `_ci` collation, SQLite `NOCASE`): a batch the database matched more rows in |
| `flags` | Soft delete (bool) |
| `audit_rows` | No PK: no PK methods generated (§9.4b) |
| `counters` | `Increment`; `strict_updates: false`; a UNIQUE `name`, a CHECK on `hits`, and a NOT NULL `updated_at` with no DEFAULT that an insert can leave out |
| `authors` / `posts` / `author_profiles` / `tags` / `author_tags` | Nested mutations: one parent with an edge of each nestable shape, O2M on a nullable FK (every verb), a config-declared has-one on a UNIQUE nullable FK, and M2M through a pure junction; a UNIQUE `authors.name` conflict target, CHECK and UNIQUE constraints on the children, and soft delete on the parent and on `posts` |

One `sqlgen.yml` per dialect, kept as close to identical as each dialect allows, so the generated
Go API is the same everywhere apart from §7.2 type differences. Variants that change the API
(tenancy, cache) get their own suite later (§8). They don't belong in the core schema.

### 4.2 Module layout: one suite, many dialects

The `testing/fstest.TestFS` pattern: the suite is a library that exports `Run(t, Harness)`, and
each dialect module calls it from a single test.

```text
cmd/sqlgen/testdata/conformance/
  suite/                    module …/conformance/suite
    go.mod                  requires …/conformance/models (placeholder version)
    harness.go              Harness, capabilities, CountingQuerier
    helpers.go              shared fixtures and assertions
    hard_delete_many.go     func hardDeleteMany(t *testing.T, h *Harness)
    …                       one file per method
    run.go                  func Run(t *testing.T, h *Harness)
  postgres/                 module …/conformance/postgres
    go.mod                  replace …/conformance/models => ./models
    schema.sql  sqlgen.yml
    models/                 generated; own go.mod, module …/conformance/models
    conformance_test.go     testcontainers setup + func TestConformance(t) { suite.Run(t, h) }
  postgres_stdlib/  mysql/  sqlite/    same shape
```

Every dialect's generated package declares the same module path, `…/conformance/models`. The suite
imports that path, and each dialect module's `replace` points it at its own generated code. A
`replace` in the main module applies to the whole build graph, so the suite compiles against
whichever dialect is running. No source is copied.

**Prototyped 2026-10-23, and `replace` works.** The patch is saved at
`.claude/prototypes/conformance-replace.patch`, minus `_gen.go` and `go.sum`. It has `suite` plus
`postgres` (pgx, testcontainers PG16) and `sqlite` (modernc, in memory) modules and five
`HardDeleteMany` subtests. Measured:

- **It builds, vets, lints and tests.** Under `GOWORK=off`, `go vet`, `golangci-lint` (0 issues,
  repo config) and `go test -race` pass in all three modules. Both dialects ran the same suite:
  Postgres statements carried `"public".` and SQLite's did not, so each run compiled against its
  own generated package.
- **`GOWORK=off` is required.** In workspace mode, `go test` fails with "directory prefix . does
  not contain modules listed in go.work". This is the same constraint the examples already have.
- **The suite module builds on its own.** Its `go.mod` replaces `…/conformance/models` with
  `../sqlite/models`, so it can be vetted and linted alone. A replace inside a dependency is
  ignored, so each dialect module's own replace wins.
- **The models package can be its own module.** `sqlgen generate` writes into a `models/` that has
  its own `go.mod`, and regeneration leaves `go.mod` and hand-written files alone.
- **The generated method signatures were identical across dialects.** `go doc -all` diffs showed
  only field-type differences, in three groups:
  - Postgres `INTEGER` → `int32`, but SQLite → `int64`. **Rule:** the conformance schema uses
    `BIGINT` for every integer.
  - SQLite `INTEGER PRIMARY KEY` is a rowid, so `Create<T>Input` has no `ID`, while a plain
    Postgres `BIGINT` PK is caller-supplied. **Rule:** use an identity / `AUTO_INCREMENT` key on
    every dialect, so the PK strategy matches.
  - Timestamps are `time.Time` on Postgres and `types.DateTime` on SQLite, which §7.2 requires.
    **Absorbed** by a hand-written `models/conformance.go` per dialect that declares
    `type Timestamp = …` and `func Now() Timestamp`. The suite names only `models.Timestamp`.

### 4.3 Harness

```go
// Harness is the per-dialect wiring the suite runs against.
type Harness struct {
	Dialect Dialect          // postgres, mysql or sqlite
	Driver  string           // pgx, stdlib, …
	Client  *models.Client
	Caps    Capabilities     // what this dialect supports (§4.4)
	Querier *CountingQuerier // records the statements Client sends
	Base    database.Querier // the querier Client was built on: Querier, or a driver wrapper around it

	// Reset returns every conformance table to its seed state.
	Reset func(t *testing.T)
}
```

- **Isolation:** `Reset` truncates and reseeds before each subtest. The suite runs sequentially
  within a dialect, and dialect modules run in parallel.
- **Query counting is unsolved inside transactions.** The prototype wraps `database.Querier` in a
  `CountingQuerier`. That works outside a transaction, but statements inside one run on the
  driver's `TxConn` that `database.NewTx` holds, so the wrapper never sees them. Measured: a
  `HardDeleteMany` inside `WithTransaction` recorded zero statements. Counting assertions for the
  query-count tables (§9.8, §25.1) need one of two things:
  - a driver-level hook: a pgx `Tracer` (PRD §24.3) for pgx, and a wrapped `database/sql`
    driver for stdlib;
  - or a counting `TxConn` seam in the runtime. That would be a runtime API change and needs a
    PRD ruling.

  Until then, counting subtests run outside transactions only.

### 4.4 Dialect differences: capabilities, not `switch`

No test, in the suite or in any module matrix, branches on the dialect name. Where behavior
legitimately differs, the difference is a named capability taken from PRD Appendix B, and the
expectation follows from it:

```go
func lockModes(t *testing.T, h *Harness) {
	t.Run("LockForUpdate on a dialect without row locks returns an error", func(t *testing.T) {
		if h.Caps.RowLocks {
			t.Skip("covered by the supported-dialect subtest")
		}
		// assert the §9.6a runtime error
	})
}
```

- **A skip is always for a capability, never a bare `t.Skip`.** A helper,
  `h.Require(t, CapRowLocks)`, skips with a message that names the capability, and the report
  shows that message (§5.5).
- **Behavior that differs by design** (MySQL `SoftDeleteWhere` taking 3 queries where the others
  take 2) is a capability-derived expectation, not a skip. Both values are tested.

### 4.5 Shape of a method contract

One file per method. Subtest names are the behavior statements: they read as the contract, and
the registry refers to them.

```go
// hardDeleteMany: HardDeleteMany permanently removes every row whose key is listed.
func hardDeleteMany(t *testing.T, h *Harness) {
	t.Run("empty key list deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		if err := h.Client.Products().HardDeleteMany(ctx, nil); err != nil {
			t.Fatalf("HardDeleteMany(nil) error = %v, want nil", err)
		}
		assertProductCount(t, h, 1)
	})
	t.Run("composite keys delete only listed tuples", func(t *testing.T) { /* … */ })
	t.Run("missing keys are not an error", func(t *testing.T) { /* … */ })
	t.Run("rolled-back transaction leaves rows", func(t *testing.T) { /* … */ })
	t.Run("fires one OpHardDeleteMany hook", func(t *testing.T) { /* … */ })
}
```

Subtest names carry no PRD numbers, IDs or labels (`guidelines/GO.md` → When to Comment). The link
to the PRD lives only in the registry. The same rule holds for every module's unit tests.

**A subtest asserts only what the PRD promises.** The prototype's first draft asserted that "an
empty key list runs no query". Both dialects failed, because `HardDeleteMany(nil)` then sent
`DELETE … WHERE 1 = 0`. The PRD says only "Batch hard delete" (§9.3 table), and `1 = 0` is the
builder's designed empty-batch condition. The test had invented a contract. The §5.3 check that
every claimed test maps to a PRD-anchored registry row is the guard against this.

## 5. Requirement registry

### 5.1 Files and format

`docs/conformance/` holds one registry file per module (§2): `runtime.md`, `parser.md`, `cli.md`,
`client.md`. A file that passes about 500 rows is split into a directory of per-package files
(e.g. `docs/conformance/cli/config.md`), with the §5.2 map updated in the same commit (§9 ruling
7). Each file has five parts:

1. **A header** with the file's seeding status (§5.4), the module's package directories (the
   orphan check of §5.3 check 2 scans them), and its matrix declarations. A matrix's rows are an
   open set (every row a cell names) unless the declaration lists them after its row unit
   (`rows=method:Get,GetMany,…`). A listed matrix rejects a cell naming any other row and counts
   every listed row's empty cells, including a row no cell names yet:

   ```markdown
   <!-- seeding: in-progress -->
   <!-- packages: cmd/sqlgen/config cmd/sqlgen/gen -->
   <!-- matrix: config-validation rows=rule cols=fires,message,severity,together,schema -->
   ```

2. **One section per PRD section the module owns**, each anchored and hashed:

   ```markdown
   ## §9.4c Nil input handling
   <!-- prd: 9.4c sha256: 3f1a… -->

   | ID | Requirement | Tests | Applies to | Matrix cell |
   |---|---|---|---|---|
   | nil-input.getmany | `GetMany(ctx, nil)` behaves exactly as `GetMany(ctx, &Get<T>Input{})` | `conformance: GetMany/nil input equals zero input` | all | client-methods: GetMany × nil-empty-input |
   | nil-input.batch-empty | An empty key list deletes nothing and returns no error | `conformance: HardDeleteMany/empty key list deletes nothing` | all | client-methods: HardDeleteMany × nil-empty-input |
   | lock.sqlite-error | A lock mode other than `LockNone` on SQLite is a runtime error | `conformance: LockModes/LockForUpdate on a dialect without row locks returns an error` | sqlite | client-methods: Get × lock-mode |
   ```

3. **`n/a` declarations** for matrix cells that don't apply. A reason that cites a PRD section
   stands on its own. A reason with no PRD citation needs the user's explicit acceptance, recorded
   as `accepted: <date>` (§9 ruling 9):

   ```markdown
   | Matrix cell | Reason | Accepted |
   |---|---|---|
   | client-methods: Count × field-selection | §12 field selection applies to entity reads; Count returns a number | |
   | sql-builders: BuildCount × returning | Count has no rows to return | 2026-11-02 |
   ```

4. **An `unclaimed` list** of tests in the module's packages that back no PRD behavior (helpers,
   internal invariants, fuzz seeds), each with a one-line reason (§9 ruling 10). They are not
   folded into requirement rows, because that would invent contracts:

   ```markdown
   | Unclaimed test | Reason |
   |---|---|
   | cmd/sqlgen/gen: TestFuncmapIndent | Template helper; no PRD behavior |
   ```

5. **A `PRD gaps` table** of behavior the code has, usually pinned by tests, that no PRD text
   states (§9 ruling 15). A gap is a question for the user, not a requirement. Its tests resolve
   and count as claimed. Its Ruling stays empty until the user rules `amend-prd <date>` or
   `drop-pin <date>`, and the entry is deleted once the ruling is applied:

   ```markdown
   | Gap | Behavior | Tests | Section | Ruling |
   |---|---|---|---|---|
   | gap.hard-delete-many.nil-statement | `HardDeleteMany(ctx, nil)` sends no statement and reads nothing back | `example: postgres: TestEmptyBatch_HardDeleteManyUnaffected` | §9.4c | |
   ```

Row fields:

- **ID:** a readable slug, unique across all registry files. It appears only in the registry,
  never in code.
- **Tests:** one or more entries separated by `;`, each `<home>: …` with the home from §2's
  table: `conformance: Method/subtest`, `unit: <dir>: TestName/subtest`,
  `integration: <dir>: TestName`, `example: <example>: TestName` or `golden: <example>`.
  `<dir>` is the package directory relative to the repository root, so a name is never
  ambiguous between packages.
- **Applies to:** `all`, or a comma-separated list of `postgres`, `mysql`, `sqlite`, `pgx`,
  `stdlib`. Anything narrower than `all` names the capability or quotes the PRD.
- **Matrix cell:** `<matrix>: <row> × <column>`, naming a matrix the file declares, so the report
  can render the matrices.
- **Status**, derived, never hand-written: `pass`, `fail`, `skip (capability)` or `untested`. A
  row with an empty Tests cell is `untested`. That's how a known gap is recorded.

### 5.2 Section ownership map

`docs/conformance/README.md` maps every level-2 and level-3 PRD heading (238 on 2026-10-23) to
exactly one registry file, or marks it `informative` (overview, rationale, rejected
alternatives, examples with no behavior of their own). A deeper heading (`9.8.10`, `26.4.1`)
belongs to its nearest mapped ancestor, and an anchor may name it. A section split across
modules (§29 tenancy: runtime keys, generated behavior) names one owner, and the other file's
rows cite the section without owning it. `req-check` fails on a level-2 or level-3 heading the
map doesn't list, so a new PRD section can't arrive without an owner.

### 5.3 Static check: `make req-check` (part of `make check`, no Docker)

The tool is `cmd/sqlgen/internal/reqcheck` (stdlib only: `go/parser` and `go/ast`), run by
`make req-check` from `cmd/sqlgen/internal/cmd/reqcheck`. It runs six checks:

1. **Tests exist.** It parses every test file the registry points at (the suite and the module
   packages), collects the test tree (top-level `Test*` functions or suite method functions, and
   their string-literal `t.Run` names), and fails when a registry row names a test that doesn't
   exist.
2. **No orphan tests.** It fails when a suite subtest is claimed by no row. For unit tests this
   check applies only to packages whose registry file is `complete` (§5.4), because existing unit
   tests far outnumber the rows seeded so far. There, a test must be claimed by a row or listed as
   `unclaimed` with a reason.
3. **Anchors resolve.** It fails when a `<!-- prd: … -->` heading no longer exists in
   `docs/PRD.md`.
4. **The PRD hasn't changed underneath.** Each section block stores a hash of the PRD section's
   text. When the PRD section changes, the check fails with the section name until someone reviews
   the requirements under it and runs `make req-rehash` (§9 ruling 4).
5. **Ownership is complete.** It fails when a PRD heading is missing from the §5.2 map, or is
   owned by more than one file.
6. **Matrix cells are well formed.** It fails when a row's matrix cell names a matrix or column
   its file doesn't declare, or a row outside a matrix's declared row list (§5.1; without a list,
   rows are an open set), or when a cell declared `n/a` is also
   claimed by a requirement. It also fails when an `n/a` reason cites no PRD section (`§…`) and
   has no `accepted` date in `YYYY-MM-DD` form, or cites a `§` that is not a PRD section.

It also rejects malformed rows: an ID that is not a lower-case slug or is reused across files, an
unknown applies-to value, a requirement outside an anchor, an unrecognized table, and a
`not-started` file that already has rows. A PRD gap fails on an ID without the `gap.` prefix, an
empty behavior, a test that does not resolve, no `§` citation or one that is not a PRD section,
and a ruling other than `amend-prd` or `drop-pin` with a date. A requirement ID may not start with
`gap.`. An open or ruled gap is a note, never a failure. And it fails on any directory holding
`_test.go` files (outside `testdata`) that no registry file lists in `<!-- packages: … -->` and
the ownership map doesn't mark `<!-- unregistered: … -->` (test support, repository tooling), so a
module's tests can't escape the orphan check by living in an unlisted package.
`make req-rehash SECTIONS=…` fails on a section that no anchor names.

Check 1 reads subtest names statically: a string literal, a string field of a ranged table
literal (keyed or positional, `t.Run(tt.name, …)`), or a key of a ranged map literal. It follows
calls to package-level helpers that take a `*testing.T`, which is how the suite's `Run` reaches
each method's subtests, and a subtest body sees the tables of the test enclosing it. A literal
name containing `/` is nested, as `go test` reports it. Any other name (`fmt.Sprint(i)`) is
computed: a unit test itself can still be claimed, but a path below it fails with a message
saying the names are computed. In the suite, a computed name fails outright, because every
suite subtest must be claimable.

### 5.4 Coverage ratchet

Seeding hundreds of rows takes time, so coverage is reported before it is enforced. Each registry
file declares its seeding status:

| Status | Meaning | Effect |
|---|---|---|
| `not-started` | No rows yet | Report shows the module as not started; no failures |
| `in-progress` | Rows being seeded | Report lists owned PRD sections with no rows, declared matrices with no rows, and empty matrix cells; no failures |
| `complete` | Every owned section has rows, every declared matrix has at least one row, every matrix cell is a row or `n/a` | `req-check` **fails** on any owned section with no rows, any declared matrix with no rows, any empty cell, and any orphan test in the module's packages |

A file moves to `complete` only by an explicit edit in a commit that `req-check` passes. From
then on, coverage can't silently fall back: a new PRD section or matrix row in that module fails
the check until it has rows.

### 5.5 Full run and report: `make conformance` (Docker)

`make conformance` runs every registered test home with `go test -json`: the module packages
(unit and integration), the example modules, and every conformance dialect module. It then joins
the results with the registry and writes `docs/conformance/REPORT.md`:

- **Per module:** seeding status, rows, `untested`, `fail`, `skip`.
- **Per requirement × dialect / driver:** pass, fail, skip with the capability message, or
  `untested`.
- **Every declared matrix** rendered with its cells filled, `n/a`, or shown empty.
- **PRD coverage:** owned sections with no rows, per module, and the `informative` count.

**Joining results (step 4).** The joiner lives in `reqcheck` (`results.go`, `report.go`), run by
`reqcheck -report <results dir>`, and refuses a registry that fails `req-check`. Each `go test -json`
event's package path is `github.com/teandresmith/sqlgen/<dir>` for every module in the repository,
so a reference's `<dir>` names its package directly. Names match as `go test` reports them, with
spaces as underscores, the same rule `req-check` resolves with. A `conformance:` reference becomes
`TestConformance/<Method>/<subtest>` in each dialect module, the targets, whose dialect and driver
are the string literals its test sets on the harness. The suite logs a `conformance harness:
database …` line, which gives the header its database versions.

- **Not run is never pass.** A test with no result of its own is `not run`, with the reason: its
  package is missing or failed to build, or a parent test failed first (no Docker: the container does not start) or skipped.
- **On one target**, a failure or a test that did not run decides. Otherwise one pass is enough,
  since a sibling subtest may skip for a capability another covers. All skips make a skip, with
  the skip messages.
- **A row's status** is its most severe target: `fail`, `not run`, `untested`, `skip`, `pass`. A
  target the row's Applies-to excludes shows `—` and does not count. A row whose conformance
  tests apply to no existing dialect module is `not run`.
- **Matrices** are rendered whole, one line per matrix row; a filled cell shows its row's status.
- **`make conformance` exits non-zero** after writing the report when any `go test` run failed. A
  stale `models/` aborts it before anything runs.

`make check` doesn't regenerate the report, because that needs Docker. It is regenerated and
committed by `/close-phase` (§6) and by a deliberate `make conformance` run. It is not rewritten
on every test run, to keep churn down (§9 ruling 2). Its header records the commit and the
database versions it ran against.

## 6. Harness integration

| Skill | Change |
|---|---|
| `/design` | Writes each new design note's §6 behavior contract (`docs/design/templates/design.md`): new behaviors with their PRD anchor and planned test, regression guards for every touched path, and the touched module matrix cells. These rows become registry rows at the PRD-sync sub-item. |
| `/phase` | Carries the fronting design note's §6 rows into each sub-item's Tests Required (unguarded guards on N.1). |
| `/implement` | Writes regression guards first (they must pass on current code), then new-behavior tests (they must fail for the predicted reason), then the code. Adds or updates the registry rows for the PRD sections it implements, in whichever module file owns them. |
| `/verify` | Reads the registry rows for the sub-item's PRD sections instead of re-deriving a checklist from prose. A row with no test, a skip without a capability, an empty matrix cell the sub-item touches, or a failing cell is a FAIL. The reviewer then judges the narrow question: does each test actually exercise its requirement? |
| `/close-phase` | Runs `make conformance` and commits the report. No `untested` or `fail` row may remain for the phase's PRD sections unless the user has accepted it. |
| PRD sync | A PRD edit trips the hash check (§5.3 check 4). A new PRD heading trips the ownership check (§5.3 check 5). The same commit reviews, rehashes and assigns. |

## 7. Plan

1. **Prototype the cross-dialect plumbing (§4.2).** Done 2026-10-23: `replace` works.
2. **Registry tool and ownership map.** Done 2026-10-23: `req-check` with the six static
   checks (in `make check`), `make req-rehash`, the §5.1 file format, and the §5.2 map with all
   238 level-2 and level-3 PRD headings assigned (96 `cli.md`, 64 `runtime.md`, 40 `client.md`,
   16 `parser.md`, 22 `informative`). All four registry files start `not-started` with the §3
   matrices declared. A probe seeding one §4.13 row against a real `config` test resolved it,
   failed on a `pending` hash and on a PRD edit under the anchor, and listed 87 coverage notes
   for `cli.md` once it was `in-progress`.
3. **Two pilots, config first (§9 ruling 8):**
   1. **`cli.md`, config validation matrix (§3.4).** Seed a row for each §4.13 rule, map each to
      the existing `config` tests, mark gaps `untested`, then fill them. Pure unit tests, no
      Docker. This proves `req-check`, the ratchet and the `unclaimed` list on about 85 existing
      PRD rows, and measures how mechanical seeding a PRD table is.
   2. **`client.md`, `HardDeleteMany` row (§3.5)**, once `req-check` works on config. Every
      concern on every dialect, porting the existing per-dialect example tests into the suite and
      deleting the copies once the ports pass (§9 ruling 6). This measures the suite's cost per
      method.

   Record from both: rows seeded, gaps found, defects found, hours spent.

   **Config pilot, recorded 2026-10-23:**
   - **Rows seeded.** The 87 §4.13 rules, each with 5 columns, give 435 cells: 293 requirement
     rows and 142 `n/a` declarations. 3 more rows cover the prose under the table (exit code 2,
     `validate` reporting phase-3 rules, the partial cursor-key fallback). No cell is empty.
     Each `n/a` cites a PRD section: §4.13 where the PRD states no message, or the rule is a
     warning or a silent fallback; §4.14 where the schema does not express the rule. None needed
     an `accepted` date. A row claims one cell, so a rule's `fires` row is `config.<slug>`
     and its other columns are `config.<slug>.<column>`.
   - **Gaps.** At seeding, 75 rows were `untested`: 59 `together`, 9 `message`, 5 `schema`, and
     the `fires` and `severity` rows of the `fk` list-length rule. That rule is not implemented
     yet; it is planned as 32.3. After the new tests, the 4 rows of that rule are the only ones
     left. 4 tests are claimed at the top level because their subtest names are computed. Two
     range over a `[]string` literal, which `req-check` does not read; two build names by
     concatenation. The `unclaimed` list was not exercised, because `cli.md` is still
     `in-progress` and the orphan check is off.
   - **Defects: 6**, each fixed with a fail-first test:
     1. An explicit `manifest.formats: []` was defaulted at load, so its error never fired.
     2. `markdown_dir` / `json_per_entity_dir` accepted an interior `..` escape.
     3. They did not clean the path before the reserved-name and equality checks.
     4. The three manifest placement rules ran only when the manifest was enabled.
     5. The no-UNIQUE-coverage warning did not match the PRD's quoted text.
     6. In `validate`, one table that failed to build hid five resolution-level rules on every
        other table, which breaks reporting all errors together.

     6 behaviors pinned by tests with no PRD text went to `backlog.md` as `prd-gap` items. They
     moved to `cli.md`'s PRD gaps table once it existed (§9 ruling 15).
   - **Tests written.** 4 table-driven "reported together" tests (pre-parse, post-parse,
     generation and view tenancy; 56 cases, one of them guarding the fix for defect 6 against reporting a
     false error for an edge into the table that failed), 2 env-reference cases, 7 JSON Schema cases, and
     message assertions tightened in 8 existing tests.
   - **Time.** About 40 minutes of wall time, with the test mapping fanned out to four parallel
     agents (about 4–5 minutes each). Seeding itself was mechanical: one generator script, with
     `req-check` failing only on the computed names. Most of the effort was the `together`
     column, which no existing test covered rule by rule.

   **Client pilot (`HardDeleteMany`), recorded 2026-10-23:**
   - **Suite landed** at `cmd/sqlgen/testdata/conformance/` (`suite`, `postgres` on pgx, `sqlite`
     on stdlib). The schema has three tables so far: `products`, `order_items` and `documents`
     (timestamp soft delete). A SQLite surrogate key is `INTEGER PRIMARY KEY`, the only rowid
     spelling. Every other integer is `BIGINT`. The generated APIs then differ only in timestamp
     types, the nullable `deleted_at` included (`*time.Time` against `types.NullDateTime`). The suite
     never names that type: it soft-deletes through the client and counts rows with SQL.
     The generated `models/` is committed like the examples. `make test-conformance` fails when
     `sqlgen diff` reports it stale. `make check-conformance` (fmt, lint, vet, test) is a
     prerequisite of `check-examples`. A CI job comes with step 4. SQLite needs its foreign-key
     pragma in the DSN (`_pragma=foreign_keys(1)`), because a `PRAGMA` statement reaches one
     pooled connection only.
   - **Rows seeded: 12**, under 11 anchors. 8 claim a `client-methods` cell: happy path, nil and
     empty input, not found, soft delete, transaction, hooks, error mapping, composite PK. 4 have
     no cell: rollback (§18.3), a hook aborting (§21.5), and the two batch rows (§9.5, §25.3). The 2
     `n/a` cells cite §12 (field selection: the method returns no entity) and §9.6a (lock modes are
     ignored on deletes). No `n/a` needed an `accepted` date. Rows cite sections other files own
     (§8.7, §18, §21, §22.2), as §5.2 allows. The anchors are the subsections the rows quote, so an
     unrelated edit elsewhere in §18 or §21 does not trip the hash.
   - **Gaps: 2**, the two batch rows, which stayed `untested` until FIX-361 landed.
   - **Defects: 1, a blocking FIX.** `HardDeleteMany`, `SoftDeleteMany` and `RestoreMany` ignore
     `generation.batch_size` (§9.5, §19.4, §25.3). With `batch_size: 2`, 5 keys sent one `DELETE`
     where 3 were expected. A failing third key also undid the first sub-batch. The two fail-first
     subtests were held back for the fix and landed with it.
   - **PRD gaps: 1**, logged in `backlog.md` and later moved to `client.md`'s PRD gaps table:
     `HardDeleteMany(nil)` sent `DELETE … WHERE 1 = 0` (no statement once sub-batching landed) and
     read nothing back. The Postgres example's pin of that behavior stays, claimed by the gap until
     the user rules on it.
   - **Ported:** `TestHardDeleteMany` was deleted from the `postgres` and `sqlite` examples. Step 5
     deleted the `mysql` and `postgres_stdlib` copies. The MySQL composite-PK batch test stays,
     because it also covers `Filter.PKs`. The cache, events and tenancy
     variants' uses are not ports (§8). The deleted Postgres copy keyed on a UUID; key-type coverage
     is the `type-resolution` matrix's job, not this method's. Until step 4's CI job, CI runs no
     `HardDeleteMany` test on Postgres or SQLite: only `make check-examples` does.
   - **Review lessons.** The reviewer ran a mutation check: a generated `HardDeleteMany` that
     ignored the transaction in its context still passed the first draft of the transaction
     subtest, because the delete only autocommitted outside the transaction. The subtest now
     asserts that nothing reaches the counting querier. Two more subtests over-asserted: one
     checked `RejectMutation`'s message text, the other the order of `AffectedPKs`. Both now assert
     only what the PRD states. For step 6: mutate the generated method once per concern and watch
     its subtest fail.
   - **Cost per method.** Setup was a one-off: plumbing, schema rules, Makefile and freshness
     check. The method itself took 11 subtests (about 200 lines; 13 with the two that landed with the fix) and 12 rows. Each concern was one
     or two subtests of 10 to 25 lines, built on shared fixtures. Runtime is about 0.5 s on SQLite
     and about 4 s on Postgres, nearly all of it container start, so per-method runtime is
     negligible. Most of the effort per method is reading the PRD for each concern: about 10
     sections for one method. Wall time was about 2 hours, including the plumbing and a stop for
     the defect.
4. **Report generator** and `make conformance`. Done 2026-10-24.
   - **Built.** `reqcheck -report` joins `go test -json` results with the registry
     (`results.go`, `report.go`) and writes `docs/conformance/REPORT.md` (§5.5, "Joining results").
     `make conformance` runs the 8 modules (unit and integration together), the 15 example modules
     and the 2 dialect modules, after the `sqlgen diff` freshness check, now shared with
     `test-conformance` as `conformance-fresh`. The harness gained a `Database` field that `Run`
     logs, which gives the header PostgreSQL 16.15 and SQLite 3.51.3. The `conformance` CI job runs
     the suite per dialect beside the example jobs and leaves the report alone (ruling 2).
     `/close-phase` reads and commits the report instead of `req-check -v`. Four design choices
     were ruled: matrices render whole (config-validation is 87 lines), the joiner lives in
     `reqcheck`, the target always writes the report and then fails on any failed run, and the
     requirement tables take one column per dialect module plus one for the other tests.
   - **Time.** About 50 minutes of wall time. A full `make conformance` takes about 8.5 minutes on
     a laptop under `-race`; most of that is the `cmd/sqlgen` module and the example containers.
   - **What the report showed.** On the first run, 305 of 310 rows passed. 4 were `untested`: the
     `fk` list-length rule, not yet implemented. 1 was `not run`, and that was a joiner bug, not a
     test. The subtest `markdown_dir naming an artifact subdir through ./` ends in `/`, which
     `go test` keeps as an empty last segment, but the reference was split on `/` with empty
     segments dropped. Static resolution had the same flaw from the other side: a reference
     stopping partway through a slashed name (`TestSlash/from` for `from/to`) passed `req-check`,
     though `go test` never reports such a test. Both now split names the same way, and a partial
     name fails the check. After the fix: 306 pass, 0 fail, 0 skip, 0 not run, 4 untested. No
     registered test fails on current code. Of the 7 open PRD gaps, 5 have passing pins and 2
     have no tests, because they were found by reading code. No registry content changed.
   - **Review lessons.** The reviewer probed real `go test -json` output and found that a
     multi-line or bare `t.Skip` and stray container output corrupted the skip message. Continuation
     lines are now taken only when indented deeper than the logged line. It also found that a row
     whose conformance tests apply to no existing dialect module could pass on its other tests.
     That row is now `not run`.
   - **Follow-up.** `/close-phase` ran `make check-examples` and `make test-integration` before
     `make conformance`, which runs the same tests again. Its sweep is now `make check`,
     `make static-examples` (the format, lint and vet half of `check-examples`) and
     `make conformance`, and its `--skip-integration` flag is gone.
5. **Add the MySQL and `postgres_stdlib` conformance modules**, since MySQL differs most (no
   `RETURNING`, `AUTO_INCREMENT`). Done 2026-10-24.
   - **Built.** `mysql/` (MySQL 8.0 through `go-sql-driver/mysql`, `parseTime=true`) and
     `postgres_stdlib/` (PostgreSQL 16 through `lib/pq`, as the `postgres_stdlib` example uses).
     `postgres_stdlib` reads the pgx module's `schema.sql`, so the two differ only in the driver;
     an edit to that DDL makes both modules' `models/` stale, and `conformance-fresh` catches it.
     The MySQL schema uses `BIGINT AUTO_INCREMENT` keys and `TIMESTAMP(6)` columns, and its
     harness resets with `DELETE`, because `TRUNCATE` refuses a table a foreign key references.
     The Makefile, the CI `discover` job and the report's target columns found both modules
     with no change. The `TestHardDeleteMany` copies in the `mysql` and `postgres_stdlib`
     examples were deleted (ruling 6).
   - **Time.** About 30 minutes, plus the gate runs.
   - **What it showed.** No subtest fails or skips on either module: 19 of 19 pass on each. The
     generated API (`go doc -all`) matches the pgx module's except that MySQL's table-name
     constants have no `public.` prefix. One schema fact was measured, not assumed: with the
     foreign key written inline (`product_id BIGINT NOT NULL REFERENCES products(id)`), MySQL
     8.0 created no constraint, and the foreign-key subtest failed with a nil error. The key is
     now a table constraint. (`backlog.md` already records that 9.x enforces the inline form.)
     No capability was needed yet: the three seeded methods behave the same on every dialect.
     The regenerated report has four target columns (MySQL 8.0.46, PostgreSQL 16.15 twice,
     SQLite 3.51.3) and the same totals as step 4: 306 pass, 0 fail, 0 not run, 4 untested.
6. **Seed module by module**, using the FIX origin tally (§1.3) to choose the order: `cli.md`
   (gen, then gotype, naming and the rest), `parser.md`, `client.md` (method by method), then
   `runtime.md`. Each file goes `in-progress` → `complete` on its own.

   **First slice, `cmd/sqlgen/gen`, recorded 2026-10-25:**
   - **Split first.** The slice would have taken `cli.md` past ruling 7's ~500 rows, so it became
     `cli/config.md` (the §4.13 rows), `cli/gen.md`, `cli/gotype.md`, `cli/commands.md`
     (`cmd/sqlgen`, `cli`, `wrapper`) and `cli/manifest.md`, one file per slice. The
     access-control matrix columns changed to the §32.2 surfaces (`go-client`, `api-in`,
     `api-out`, `filter`, `sort`, `event-payload`, `cache`, `manifest`); REST, not generated
     yet, has rows outside the matrix. Generated-artifacts rows are the 12 §8.2 artifacts and the
     10 §16.4 capabilities §8.2 does not list; §16.4's GraphQL rows wait for the api-generation
     slice, and the package-wide §8.3 artifacts are rows with no cell. Relationships rows are
     §13.1's belongs-to, has-one, O2M and M2M plus self, circular and polymorphic (no m2o: §13.1
     has no such type). The slice also took §22.1, §22.4, §22.5 and §28.9, which `gen` emits.
   - **Rows seeded: 468** under 38 anchors, 167 of them matrix cells (106 generated-artifacts, 35
     access-control, 26 relationships), plus **50 `n/a`** (48 generated-artifacts, 2
     relationships), each citing a PRD section; none needed an `accepted` date. The five
     `access-control × manifest` cells are left for the manifest slice. Anchors include sections
     other files own (§9.8, §9.9, §10.3, §13.2, §17.3, §29.x), and rows cite `parser`, `gotype`,
     `cli`, `sql` and example tests where the behavior lives there (§16.3 is mostly the parser).
   - **Gaps.** At seeding, 56 rows were `untested`. After the new tests, 19 are: 11 are Phase 32
     work not yet started (composite-key edges, FKs to a UNIQUE column, the `fk` list form, which
     the staging guard keeps out of `gen` until 32.4–32.6); 4 are REST; the rest are
     `go:generate` (needs an end-to-end module), the tenant filter's comparator type (a PRD gap),
     and two clauses split off over-claiming rows in review: a composite-PK parent keeping its
     manifest entries (the manifest slice), and the self-edge `ErrNestedVerbConflict` refusal,
     which is runtime behavior for the client slice. A third split-off clause, a composite-PK
     parent's relationship filters, is one of the 11 Phase 32 rows.
   - **Defects: 2 fixed**, each with a fail-first test: JSON view aggregates were generated NOT NULL
     (§16.3, in the parser), and `Restore` on a missing key returned `ErrNotFound` instead of
     `nil, nil` (§9.3, §9.5, §9.8.8). Five more findings were Phase 32 sub-items already planned.
   - **PRD amendments: 7**, six ruled and one following a fix: the header version has no leading `v` (§8.1.5); MySQL and
     SQLite UUID keys are classified by their storage type (§8.6, §9.8.4; the dead `uuid` arms and
     their hand-built pins were removed); `ColumnMap()` is not consumed by
     `BuildColumnsFromFieldOptions` (§12.2); the condition order is not part of the contract
     (§29.8); only `RestoreWhere` scopes to deleted rows (§9.6, §9.8.8); a composite-PK target's
     loader filters on the FK column (§8.7); and the §9.8.8 `Restore` example, updated with the fix. **PRD gaps: 25** in
     `gen.md` (24 new) and 1 more in `client.md`, all open.
   - **Tests written.** About 50 test functions or table cases over 20 files, by four agents in
     worktrees and then by hand. The worktrees were created from `main`, not the working branch,
     so their first runs held on code 35 commits old; two tests failed once merged, because
     relationship detection now reads FOREIGN KEY constraints. Rebasing the agents fixed it.
   - **Report.** `make conformance` at `4d17539d`: 780 rows, 757 pass, 0 fail, 0 skip, 0 not run,
     23 untested (19 in `cli/gen.md`, the 4 `fk` list-length rows in `cli/config.md`).
   - **Time.** About 4.5 hours of wall time: 15 minutes to scope, 20 to map with four agents, then
     defect stops and rulings in four rounds, test-writing agents, two review passes and the gates.
     The mapping itself was mechanical; the cost was rulings and review.
   - **For the next slice (gotype):** create worktree agents from the working branch, or check
     their base before trusting a result; have mapping agents split a requirement at every
     "and", since review found rows claiming tests that assert only one clause; give each agent
     the list of planned work (tracker) so planned gaps are not reported as defects; and assign
     helper names up front, since two agents wrote the same function-extraction helper.
   **Second slice, `cmd/sqlgen/gotype`, recorded 2026-10-25:**
   - **Matrix shape.** The `type-resolution` header has no dialect column, so the dialect axis is
     carried by per-dialect row slugs, one per row of the three §7.2 tables, with each row's
     spellings together: 27 `postgres-…`, 24 `mysql-…` and 6 `sqlite-…` rows, each with
     Applies-to set to its dialect. MySQL `set`, listed twice in §7.2, is two rows
     (`mysql-set-type`, `mysql-set-string`) plus a PRD gap asking for the condition. The header
     did not change. A cell may be claimed by more than one row; `nullable` is two rows where the
     two `use_pointers` settings differ.
   - **Rows seeded: 484** under the six §7 anchors, 266 of them matrix cells, plus **52 `n/a`**:
     51 `integration` cells citing §7.4 (the integrations match only `uuid` and `numeric`,
     `decimal`) and `mysql-set-type × type-override` citing §7.2 / §8.3. None needed an
     `accepted` date. The matrix rows were generated from the spec table of the test that fills
     them, so the registry and the test cannot drift. Rows cite `gen`, `config`, `parser`,
     `manifest`, `sql`, `types`, golden and example tests where the behavior lives there.
   - **Gaps.** The mapping agents' drafts had about 210 of 520 rows untested, most of them the
     `column-override` and `type-override` cells and the `use_pointers: false` forms. Four
     table-driven tests over every spelling of every §7.2 row (built-in mapping, type override,
     integration, column override; about 520 subtests) filled the matrix, which left 40. After
     the new tests, 7 remain, all driver-level: a MySQL `tinyint(1)` value other than 0 or 1, SQLite
     `date()` rows against bound dates, the pgx and stdlib behavior of a `text` column retyped to
     `uuid.UUID`, a stdlib `uuid` column under `database/sql`, and a typed JSON struct scanning
     directly. They need example-module tests.
   - **Defects: 1 fixed**, with a fail-first test: `types.JSON.UnmarshalJSON` accepted malformed
     input, since `json.RawMessage.UnmarshalJSON` copies unchecked (§7.6).
   - **PRD amendments: 4 ruled**, plus wording from review: a wrapper-backed integration's wrapper
     (`uuid.NullUUID`, `decimal.NullDecimal`) is the nullable variant at either `use_pointers`
     setting (§7.3, §7.4, §4.7; the code and its tests already did this); a bare `datetime` key
     reaches a `DATETIME(6)` column behind a `datetime(6)` key, by §7.1's modifier-stripped lookup
     (§7.2); the §7.6 typed-access example uses `column_map`, since the strict loader rejected
     `columns.<col>.go_type`; and `UnmarshalJSON`'s row follows the fix. Review also corrected
     `types.NullDateTime`'s underlying field to `Time` (§4.7). **PRD gaps: 24** in `gotype.md`, all
     open: among them PG and SQLite `time` → `string`, the unknown-type `string` fallback,
     PostgreSQL override keys matching only the parser's spelling (`int4` misses `integer`),
     inline MySQL enums, and an `overrides.types` entry with no `nullable:` ignoring
     `use_pointers: false`, which contradicts §4.7.
   - **Tests written.** 4 matrix tests and 1 key-precision test by hand; 14 gotype, 7 runtime
     (`types`, `sql`) and 8 gen / parser / manifest tests by three agents working on this branch
     in separate files with assigned helper prefixes, so no worktree and no helper clash. One
     agent-mapped row over-claimed (the unknown-type fallback inside a §7.4 row); review found two
     unjustified Applies-to narrowings and four rows whose tests asserted part of the requirement,
     all fixed.
   - **Report.** `make conformance` at `52dc8321`: 1264 rows, 1234 pass, 0 fail, 0 skip, 0 not
     run, 30 untested (7 in `cli/gotype.md`, 19 in `cli/gen.md`, 4 in `cli/config.md`).
   - **Time.** About 2.5 hours of wall time: 15 minutes to scope, 10 to map with five agents, one
     round of four rulings, the matrix tests and registry generator, three test-writing agents,
     one review pass and the gates.
   - **For the next slice (naming, §8.5):** generate matrix rows from the filling test's own spec
     table, as here, since §8.5 is a table too (about 60 rows × 4 columns); fix the row shape (one
     row per cell, or several when a column has two settings) in the mapping brief, since agents
     chose differently; tell agents that ranging over a `[]string` literal gives computed names
     `req-check` cannot read, and that Applies-to narrower than `all` needs a PRD basis; and a
     `gen` test that sets `Config.Overrides` must rebuild `input.Resolver`, since `testInput`
     builds its own.
   **PRD-gap rulings pass, recorded 2026-10-25:**
   - **Ruled: 56 gaps**, every one open across the registry: 46 `amend-prd`, 6 `drop-pin`, and 4
     whose behavior the PRD already stated, which became requirement rows the code did not yet
     meet (an override with no `nullable` under `use_pointers: false`, every bad `cursor_keys`
     column reported, a view target's relationship-filter member, goimports on the API files).
   - **Code fixes: 14**, among them PostgreSQL synonym `overrides.types` keys rejected and the
     keys then matched case-insensitively, the unknown-type `string` fallback warned, list
     relationships named after filter members rejected, the tenant column filtered by its tenant
     Go type, and a write matching no row (a by-key delete, restore, update or increment)
     recording no affected key.
   - **Review-found defects and regressions**, fixed in the same pass: a MySQL key pre-read
     scanned its columns in declaration order while `BuildSelect` emits them sorted, so a key not
     declared in name order failed to scan or recorded its columns swapped; and the written-key
     fix compared listed keys with `==`, so on MySQL's case-insensitive collation a batch listing
     `('COLOR','Red')` deleted `('color','red')` and recorded no key. A sub-batch the database
     matched more rows in now records every listed key, a safe superset.
   - **Backlog: 12 findings** logged rather than filed.
   - **Report.** `make conformance` at `6b47b050`: 1350 rows, 1306 pass, 0 fail, 0 skip, 0 not
     run, 44 untested.

   **Third slice, `naming` (§8.5), recorded 2026-10-26:**
   - **Matrix shape.** The header did not change. Its 51 rows are the rows of the §8.5 tables:
     13 acronym examples, 7 field-override surfaces, 9 reserved field names, 3 entity claims, 8
     reserved-package groups, the 4-row `struct_name` example, the 4 digit-leading sites plus the
     prose digit-leading-table rule, and the 2-row `field_casing` table. The acronym-source table
     (three counts) has no row. The user fixed the column meanings: `struct` is a table's or view's
     struct name, `field` a column's or relationship's Go field, `type-name` the other
     package-scope claims (enum, SET, composite and domain type names, the `hook.TableName`
     constant), `file-stem` the `<stem>_gen.go` stem. A PRD row listing several surfaces or
     carriers is one requirement row per item on the same cell; a list of reserved names stays one
     row, with a subtest per name. The acronym struct and stem cells assert the singular forms
     (`ttl_seconds` → `TTLSecond`, `ttl_second`).
   - **One anchor.** `req-check` keys only numbered headings, so §8.5's six level-4 subsections
     share the `8.5` anchor (one `###` heading each in `gen.md`), and an edit anywhere in §8.5 trips
     every row's hash.
   - **Rows seeded: 311**, 155 of them on 87 matrix cells, plus **117 `n/a`**, each citing §8.5;
     none needed an `accepted` date. 104 matrix rows are generated from the spec tables of
     `TestNamingAcronymExamples`, `TestReservedFieldNamesMatrix`, `TestReservedPackageNamesMatrix`
     and `TestNamingDigitLeadingTable`. Rows cite `gen`, `config`, `cli`, golden and example
     tests.
   - **Gaps.** The four mapping agents' drafts had 88 of about 285 rows untested: the acronym
     struct and stem cells, the reserved-name cells for view columns, many-to-many edges and
     non-table claimants, the nested claim's structural conditioning, and most of the
     acronym-set prose. After the new tests, none are.
   - **Defects: 2 fixed**, each with a fail-first test:
     1. A one-to-one edge into a view claimed no nested-mutation type names, though §8.5 claims
        them for an edge whose target is outside the generated set; the name check read the
        emitters' candidate set, which drops that edge.
     2. A table whose singular is a Go keyword (`maps` → `Map`) failed to generate: the unified
        client's unexported field was `map *mapClient`. The field now takes the `Val` escape.
        Compiling the output for nine keyword-named tables found a second site, the field's type
        spelled from the escaped field name.
   - **PRD amendments: 5**, all ruled: an enum collision escapes through `enums.<enum>.struct_name`
     (§4.11), where §8.5 said no non-table entity had an override; the snake-case GraphQL row type
     is bound by gqlgen's case-insensitive match, with explicit `gqlgen.yml` entries only on the
     filter and inputs; the reserved-word escape covers a table's or view's struct name where it
     is emitted bare, with the fix; the field-override table's "SQL column constants", which
     named nothing generated, became the `<T>Increment<Field>` values and the `<t>AllColumns` /
     `<t>ConflictColumns` lists; and inflection reads the acronym set's membership directly for
     the freeze rule, while casing alone goes through the four helpers. A sixth ruling, dropping
     "parameters" from the `col` guard, was reversed once review found the resolver helpers' PK
     parameters: it had rested on an agent's wrong claim that no parameter is spelled from a
     column. **PRD gaps: 11**, all open,
     among them the reserved-name error naming its escape hatch, the `shared` GraphQL stem, a SET's
     second `<Set>Value` claim, and the digit-leading stem's underscore placement. One adjacent
     finding went to `backlog.md`: the resolver helpers' PK parameters are not escaped, so a PK
     column named `type` likely emits `type string`.
   - **Tests written.** 2 matrix tests and the keyword-table test by hand; 9 files by three agents
     on this branch with assigned helper prefixes (`rfm`/`nfr`, `rpm`/`rnr`/`lsn`, `arr`/`anc`/`rwd`),
     about 600 subtests in all, and a fourth agent's review fixes. Lint flagged 4 gosec findings in
     the agents' files; they carry `nolint` reasons now.
   - **Review.** The slice review found duplicate rows on two cells, the `parameters` clause with
     no row, three gaps the PRD already states (`xsrfs`, `Fishes`, `Client_IP`, now rows), rows
     whose tests covered one owner or one carrier of several, and `gqlgen-field-map` rows needing a
     split; all fixed. The fix commit's keyword-table test used helpers from a test file still
     untracked, so the gen test package did not compile at `00dfa2e2` until the next commit.
   - **Report.** `make conformance` at `6a11867a`: 1661 rows, 1617 pass, 0 fail, 0 skip, 0 not
     run, 44 untested; all 311 §8.5 rows pass.
   - **Time.** About 2.5 hours of wall time: 10 minutes to scope, 7 to map with four agents, two
     defect stops with four rounds of rulings, four test-writing agents, five review passes and
     the gates.
   - **For the next slice:** a mapping agent's "no such X exists" is a claim, not a measurement, so
     check it before turning it into a ruling question; commit a fix's test together with any
     untracked helper file it calls; `req-check` accepts a duplicate `n/a` cell, so dedupe the
     declarations a generator emits; a parse-only test of generated output misses
     type errors, so compile a probe before closing a codegen defect; and tell agents who probe
     the shared tree to keep their probe file compiling, since one agent's broken probe stopped
     another's `go test`.

   **Fourth slice, API generation, GraphQL (§26), recorded 2026-10-27:**
   - **Scope.** §26 intro, §26.2–§26.7, §26.9, §26.10, §26.12 and §16.4's GraphQL rows (the ones
     the first gen slice deferred). §26.8 REST, §26.11 HTTP headers and §26.13 gRPC are left for a
     later slice: their map entries have no rows yet.
   - **Split first.** `cli/gen.md` had 1026 rows, so all of §26 and the `api-generation` matrix
     moved to a new `cli/api.md` before seeding. At 840 rows it split again: `cli/api.md` keeps
     the matrix and §26.5–§26.12 (608 rows, including the 48 schema matrix rows, under a §26.4
     anchor of its own), and `cli/api-schema.md` takes §26–§26.4.1 and §16.4 (251). `api.md` is
     still past ruling 7's ~500, because a row can only name a matrix its own file declares.
     Neither file lists a package: the API code and its tests live in `cmd/sqlgen/gen`, whose
     orphan check stays with `gen.md`, and the rows claim `gen`, `config`, `cli`, `wrapper`,
     `types`, golden and example tests.
   - **Matrix shape.** The header did not change. Its 29 rows are the per-entity surface: 11
     schema types (object type, the two envelopes, filter, sort, create and update inputs, the
     `_inc` / `_dec` operators, relationship field, relationship filter member, nested inputs),
     3 queries, 11 mutations and 4 resolver-layer pieces (walker, filter and sort translators,
     envelope aliases). The user fixed the columns: `single-pk` a plain one-column-PK table,
     `composite-pk`, `no-pk`, `soft-delete`, `tenanted`, `view` a regular view (materialized-view
     rows carry no cell). `object-type × no-pk` is one row asserting the whole surface is absent,
     and the other 28 `no-pk` cells are `n/a` citing §9.4b. A cell where §26 says nothing about the
     shape is still a row: the shape gets the element.
   - **Anchors.** 20: §26, §26.2–§26.4.1, §26.5, §26.5.1–§26.5.8, §26.6, §26.7, §26.9, §26.10,
     §26.12 and §16.4, with unnumbered subsections (field casing, PK columns in the create input,
     comparator projection, type-name ownership, views, generated artifacts) as `###` headings.
     The §26 anchor hashes the whole chapter, so any §26 edit trips it.
   - **Rows seeded: 859**, 158 of them on 146 matrix cells, plus **28 `n/a`**; none needed an
     `accepted` date. Four mapping agents (schema and scalars; resolver surface and walker;
     filter / sort / mutation translation; errors, wrapper and config) drafted 840 rows in about
     17 minutes.
   - **Gaps.** At seeding 106 rows were untested. After the new tests, 11 are: 8 are Phase 32.7
     composite-FK work (relationship fields, filter members and nested families on a composite-PK
     parent or a multi-pair edge), one needs a live MySQL database (any unique violation takes the
     nested upsert's update branch), and two state nothing observable (no business logic, no
     row-level security beyond tenancy).
   - **Defects: 9 fixed**, each with a fail-first test:
     1. `update<T>s` checked the `_inc` / `_dec` pairings and then dropped the operators.
     2. The per-column operator dispatch lived in the gqlgen-owned seed, written once, so the
        example advertised `retryCount_inc` and three more operators its seed never applied. The
        seeds are one-line delegations now, the dispatch is in the regenerated `M` methods, and
        the five GraphQL examples' seeds were migrated (no compatibility shim: unreleased).
     3. An explicit `null` could not clear a nullable column: gqlgen's plain pointers made it look
        omitted. The wrapper merges a per-field `omittable` key, which sqlgen owns.
     4. A NOT NULL column with a default was required in the GraphQL create input though
        omittable in Go.
     5. `max_depth` / `max_complexity` were parsed and never read, and `graph/server.go` was never
        written. A regenerated `limits_gen.go` (gqlgen's complexity limit and a relationship-depth
        extension) and a one-shot `server.go` that applies it are emitted now.
     6. A table with no API-readable column emitted a field-less type and an empty sort enum,
        which gqlparser rejects; it now has no API surface, decided by the same predicate as the
        edges into it. Validation keeps primary keys readable, so this is defense in depth.
     7. `<field>` with `<field>_dec` was accepted, while `<field>` with `<field>_inc` was rejected
        for the reason that applies to both.
     8. and 9. Review of the new bulk path found its read-back skipping query hooks and issuing
        one `Get` per row (N+1, against §25.1); it skips only the cache and reads back with one
        `GetMany` now.
   - **PRD amendments: 18**, all ruled or following a fix: to-one relationship fields are nullable
     whatever the FK; the composite-type → object row is dropped (§8.3 stores composites as
     jsonb); `_inc` / `_dec` are `Int`; the walker guard is sqlgen's own tests, not a generated
     one; the generated-artifacts table and paths match the code, and the `gqlgen.yml` row says
     the wrapper reads it; `omittable` joins the sqlgen-owned keys; operators dispatch as signed
     `Increment`s with at most one of a column's set, `_inc` and `_dec`; the codegen set/inc bullet
     is dropped; count-only is `<table>List(filter) { totalCount }` (`limit: 0` pages everything
     under §9.4c); hard delete returns `true` without error (the client delete is idempotent);
     camel-case examples use `productID`; plus the §26.6 limits file and depth rule, the
     unreadable-table sentence, the defaulted create-input example and four `Decrement` mentions.
     **PRD gaps: 44**, all open, among them the primary key absent from the GraphQL filter,
     to-one edges with no filter member, and a row the bulk call soft-deletes skipping its
     operator.
   - **Tests written.** 28 new test files (9 `gen`, 3 `cli`, 2 `config`, 3 `wrapper`, 1 `types`,
     10 in the `graphql` example, one of them inside its generated `sqlgenresolver` package, which
     only `make check-examples` runs), by two fail-first agents, two fix agents, three
     test-writing agents with assigned helper prefixes, and by hand.
   - **Review.** Four sqlgen-reviewer passes. They found the bulk path's hook skip and N+1, a
     soft-delete NOT_FOUND it caused, an uncompiled composite-key branch (probed, then rendered by
     a golden), `set` with `_dec`, rows claiming more than their tests (the MySQL engine half, the
     validate lint), and stale PRD sentences, all fixed. Eleven adjacent findings went to
     `backlog.md`; two of them were later settled by rulings and removed.
   - **Report.** `make conformance` at `24b8f7e2`: 2520 rows, 2465 pass, 0 fail, 0 skip, 0 not
     run, 55 untested (11 in `cli/api.md`, 28 in `cli/gen.md`, 7 each in `cli/config.md` and
     `cli/gotype.md`, 2 in `client.md`); all 251 `cli/api-schema.md` rows pass.
   - **Time.** About 7 hours of wall time over two days: 30 minutes to split and scope, 17 to map
     with four agents, then 9 defect stops in four rounds of rulings, two fix agents, three
     test-writing agents, four review passes and the gates.
   - **For the next slice (REST, headers, gRPC):** the seed-once rule makes anything rendered into
     a gqlgen-owned seed a stale-code risk, so check every seeded body for logic before mapping;
     an agent's "this test covers X" deserves the same check as "no such X exists", since two rows
     claimed an engine behavior their test did not run; a test inside an example's generated
     package runs only under `make check-examples`, so prefer the example's `tests` package; and
     a mapping agent's suspected defect is cheaper to confirm with a fail-first test before the
     ruling than after, which is what made nine rulings quick.

   **Fifth slice, CLI commands (§23) and the rest of §26, recorded 2026-10-27:**
   - **Scope.** §23.1–§23.10 into `cli/commands.md`, and §26.8 REST, §26.11 HTTP headers and
     §26.13 gRPC, the sections the GraphQL slice left. REST is deferred (IMPLEMENTATION_ORDER
     Appendix D.1) and gRPC is future work, so their 47 rows record the PRD's promises as
     untested planned work; §26.11's GraphQL half is generated and has tests.
   - **Placement first.** `cli/api.md` was already at 608 rows, so the three sections went to a
     new `cli/api-http.md` with no package line and no matrix (REST already sat outside the
     `api-generation` matrix, and the middleware is package-wide). Nothing moved out of `api.md`.
   - **Matrix shape.** The header did not change. Its 12 rows are the 11 §23.1 commands plus bare
     `sqlgen`; the user fixed the columns: `success` is the command's §23 job (exit 0, or `diff`'s
     exit 1 on changes), `config-error` exit 2, `schema-error` exit 3, `generation-error` exit 1.
     Exit 4 has no column, so its rows carry no cell. The 48 cells are 33 rows and 15 `n/a`
     (init, completion, the manifest commands and `mcp serve`, which read no config or schema),
     each citing a PRD section; none needed an `accepted` date. `manifest` and `mcp serve` rows
     hold only what §23.1 says; §30.8 and §31 wait for the manifest slice.
   - **Anchors.** 13: §23.1–§23.10 in `commands.md`, §26.8, §26.11 and §26.13 in `api-http.md`.
     §23 needs no anchor of its own, since its descendants are owned.
   - **Rows seeded: 308**, 229 in `commands.md` (37 on matrix cells) and 79 in `api-http.md`.
     Four mapping agents (commands, flags and discovery; init, completion and diff; lint, stale
     cleanup, exit codes and watch; §26.8 / §26.11 / §26.13) drafted them in about 8 minutes.
     The cli tests that back sections other files own became rows there: §4.3 and §4.14 anchors in
     `config.md`, a §16 anchor and a §10.3 row in `gen.md`, a §9.5 row in `client.md`.
   - **Gaps.** At seeding, about 60 of 223 `commands.md` rows were untested. Two read-only claim
     checks then flagged 37 rows across both files, most resting on tests that did not assert the
     clause: early returns on a
     generation error, `err != nil` where an exit code was promised, substring checks for an exact
     format, and a stale-sweep fixture without sqlgen's header that would survive under either
     layout. Four test-writing agents (flags, init, completion, diff and exits; lint; stale
     cleanup and watch; the §26.11 headers in the graphql example) filled them. One `commands.md`
     row stays untested: a template failure cannot be reached without editing the embedded
     templates. In `api-http.md`, 4 rows stay untested besides the planned 47: the REST half, the
     shared header convention, panic recovery under `X-Skip-Hooks`, and where the `SkipHooks`
     implication is enforced (over HTTP the whole hook chain is dropped, so it is not observable).
   - **Unclaimed.** The orphan check will apply to `commands.md` once it is `complete`. An agent
     classified the 84 tests no row claimed: 10 backed existing `commands.md` rows, 15 backed rows in
     other files, 22 are `unclaimed` (17 superseded or vacuous tests, 4 E2E harness tests, one
     wrapper-internal test), 9 pinned unstated behavior and became PRD gaps, and 29 back
     `parser.md` and `manifest.md` sections and wait for those slices; they are the only orphans
     left. `TestGenerate_CompositeKeyEdgeIsOmitted` is listed as unclaimed: it is the staging guard
     for the planned composite-key edges.
   - **Defects: 8 fixed**, each with a fail-first test:
     1. `--config` had no file completion, so shells offered every file instead of .yml / .yaml.
     2. No flag value completed; `lint --fail-on` now offers error, warning and info.
     3. init took any character device as a TTY, so `sqlgen init </dev/null` prompted.
     4. `graphql init` with no config exited 2 and printed nothing.
     5. `-q` let the gqlgen subprocess's stdout and the merge warnings through.
     6. Lint's rebuilt call spelled `[]*User` as `*[]User`, and kept package qualifiers.
     7. Relationship loads dropped `SkipHooks`, `SkipCache` and `SkipEvents` (§9.6), so
        `X-Skip-Hooks` still ran query hooks on related rows. Copying the options naively would
        have inherited the `SkipHooks` that `Get`, `Connection` and write read-backs force on their
        inner `GetMany`, skipping every child hook, authorization included; the generated
        `CallOptions` carries an unexported caller record instead, which `Get` restores when a
        hook replaces its options. `LockMode` stays off relationship loads, by ruling.
     8. With the usage-error ruling: errors outside the exit-code paths exited 1 and printed nothing
        (`SilenceErrors`); unknown subcommands under `graphql`, `manifest` and `mcp` printed help
        and exited 0; leaf commands accepted and dropped extra arguments. `validate` now exits 1
        on a resolution-level error, as `generate` and `lint` do.
   - **PRD amendments: 13**, all ruled: §23.10 names `mcp serve`'s manifest reload as the one
     file-watching behavior; §4.13's exit 2 covers the config phases and resolution-level errors
     exit 1 (§8.1's test list follows); §23.9 gains usage errors exiting 2, every failure printing
     its error, `diff`'s exit 1 as a result, and "a table defined twice" for the undefined
     "ambiguous table names" (an ambiguous config key is a §5.5 config error); §23.7 shows the
     real output, one finding per mismatching type argument, and `--paths` replacing
     `output.dir`; §23.2 verbose prints counts; §23.6 drops empty summary categories and says
     `N files`; §23.1 `manifest validate` reads no config; §9.6 and §9.6a say relationship loads
     take the caller's toggles but no `LockMode`. **PRD gaps: 26** in `commands.md`, all open.
     One adjacent finding went to `backlog.md`: a locked read that JOIN-loads an o2o edge likely
     fails on PostgreSQL (`FOR UPDATE` on the nullable side of an outer join).
   - **Tests written.** 11 new cli test files, 1 gen file and 1 graphql example file, 55 test
     functions with their table cases, by two fix agents, four test-writing agents with assigned helper
     prefixes (`tfl`, `tln`, `tst`, `thd`) and by hand. Golden churn from the loader fix: 78
     regenerated files.
   - **Review.** Two claim-check passes and one sqlgen-reviewer pass. The reviewer found the
     accept-and-drop arguments, the caller record lost when a hook replaces `CallOptions`, and
     generic type arguments keeping their qualifiers, all fixed; and four doc nits, applied.
   - **Report.** `make conformance` at `8712940c`: 2842 rows, 2735 pass, 0 fail, 0 skip, 0 not
     run, 107 untested (51 in `cli/api-http.md`, 47 of them planned REST and gRPC; 28 in
     `cli/gen.md`, 11 in `cli/api.md`, 7 each in `cli/config.md` and `cli/gotype.md`, 2 in
     `client.md`, 1 in `cli/commands.md`); 228 of the 229 `commands.md` rows pass.
   - **Time.** About 5 hours of wall time: 20 minutes to place and scope, 8 to map with four
     agents, six rounds of rulings (21 questions), two fix agents, four test-writing agents,
     three verification and review passes, and the gates.
   - **For the next slice:** an agent's claim that a test "covers X" was wrong for about one row
     in six here, mostly through early returns and substring checks, so run a read-only claim
     check on every drafted row before writing tests, not after; give the claim checker the
     defect shapes (early return, `err != nil` for an exit code, substring for a format, a fixture
     that passes either way); a fix that copies options through internal hops needs the hops
     enumerated first, since forced internal toggles look like the caller's; and a cobra leaf
     command needs an `Args` check, since one without it accepts and drops extra arguments.

   **Sixth slice, manifest (§30) and MCP server (§31), recorded 2026-10-27:**
   - **Scope.** §30, §30.2–§30.5, §30.7 and §30.8; §31 and §31.2–§31.5; gen.md's five
     `access-control × manifest` cells and the composite-PK parent keeping its manifest entries;
     and the 8 `cmd/sqlgen/cli` tests the CLI slice left orphaned. §30.1, §30.6 and §31.1 stay
     where the map puts them. Planned work, untested rather than defects: Phase 32.3 (the key-list
     `fk` / `junction` / `constraint` shape, `schema_version` `0.2.0`, MCP `fk_columns`) and the
     manifest JSON Schema 1.0.0 freeze.
   - **Split first.** About 600 rows were estimated against ruling 7's ~500, so `cli/manifest.md`
     kept §30 and `cmd/sqlgen/manifest`, and a new `cli/mcp.md` took §31, `cmd/sqlgen/mcp` and
     `mcp/integration`; the README file list and ownership map followed.
   - **Matrix shape.** Two matrices, declared with the user. `manifest-artifacts`: the 11 §30.3
     artifacts (the two breadcrumbs, the package-doc pointer, the embed file, the JSON, `_index.md`,
     `_conventions.md`, per-entity markdown and JSON, the graph breadcrumb pair) × `emitted`,
     `gated`, `removed-when-disabled`, `ownership`, `deterministic`; 2 `n/a` (the package-doc
     pointer and the embed file sit in DO NOT EDIT files). `mcp-surface`: the 11 tools, 4
     resources and 3 prompts × `listed`, `output`, `deterministic`, `error`; 16 `n/a` (§31.3
     promises byte-equality for tools only; no failure input for the list, health and fixed-resource
     elements; `validate-manifest`'s `valid: false` is a result, not an error). The
     `access-control × manifest` cells assert each role's canonical-JSON column entry; the
     markdown table's omission of the markers and `sql_bodies` staying real SQL are rows with no
     cell.
   - **Anchors.** 15: §30, §30.2, §30.3, §30.4, §30.4.1–§30.4.3, §30.5, §30.7, §30.8, §31,
     §31.2–§31.5. The §30 and §31 anchors hash whole chapters, so any edit in them trips their rows.
   - **Rows seeded: 743**, 468 in `manifest.md` (65 on matrix cells), 267 in `mcp.md` (60), and
     8 in `gen.md`, plus 3 `config.md` rows for a new §4.13 rule. Four mapping agents drafted 708
     in about 15 minutes.
   - **Claim check before tests.** Four read-only checkers given the defect shapes flagged 51 of
     about 577 tested rows (1 in 11): substring checks that could not tie a value to its column,
     a determinism test that built a fresh store per call, `listed` rows compared against the
     code's own constants, a `.mcp.json` line/col check matching the colon after the path, and
     fixtures where the condition never arose. 63 rows were edited and 16 split.
   - **Gaps.** At seeding 181 rows were untested. Four test-writing agents with assigned helper
     prefixes filled 154; a review pass then found the method-order test compared only checked-in
     goldens (reverting the builder still passed it), four partial rows, hand-rolled waits and a
     racy subprocess harness, all fixed, and the tests renamed from their batch prefixes. 26 stay
     untested: 15 Phase 32.3 and freeze rows, 6 release-process rules, the 3 go-sdk replies below,
     and 2 rows with nothing observable. Every test in the three packages backs a row or is
     listed as unclaimed (19, with reasons).
   - **Defects: 13**, each confirmed with a fail-first test; 12 fixed, one ruled out of the PRD:
     1. `breadcrumbs.package_doc` was never read; `models_gen.go` (the client file when there is
        none) now carries the pointer.
     2. `generation_config.views` read the `views:` config map, so five goldens with views said
        `false`.
     3. `generation_config.cache` / `events` read the global flag, not each table's resolution.
     4. Per-entity `examples` were always empty, so `sqlgen_get_example` returned nothing; they
        are generated from the entity's methods and a test vets every line of three goldens.
     5. `features.cache.invalidates_on` was a fixed list naming `Delete`, which no method has.
     6. A format dropped from `formats`, or GraphQL disabled, left its artifacts behind.
     7. `.mcp.json` with trailing content was accepted and rewritten without it.
     8. A truncated or empty `.mcp.json` failed with no line and column.
     9. `mcp serve`'s startup failures did not name `-32001` / `-32002`.
     10. gen ignored a per-table `cache.enabled: true` under a global false, which §4.8 and §27.2
         allow; the manifest fix had reported `features.cache` for code that did not exist.
     11. Manifest methods were in an order matching neither the interface nor the implementation.
     12. `--manifest-timestamp` wrote any string into `generated_at`.
     13. `include_internal` was never read; ruled out of the PRD and the config.

     Review of the fixes found four more, fixed in the same commits: example lines used
     `uuid.New()`, which gofrs lacks; ID filters used `"example"` on any column; breadcrumbs
     pointed at markdown a json-only run had deleted; and graph-breadcrumb cleanup reached
     `<output.dir>/graph` with no `api` block. Two go-sdk behaviors are ruled a known limitation:
     an unknown method gets code `0`, and malformed stdio input ends the session with no
     `-32700` / `-32600`. An upstream issue is drafted, not filed.
   - **PRD amendments: 26**, all ruled or following a fix, among them: the package-doc pointer's
     form and file; breadcrumbs following `formats`; stale cleanup on a format flip and a GraphQL
     disable; which collections are sorted and which keep source order (corrected once, after a
     fill test showed the first wording false); omitted-when-empty members; `examples` content;
     `invalidates_on` per §27.7; `…WithRelated` methods carrying no `sql_bodies`; the graph
     breadcrumb's third pointer; `.mcp.json` line/col cases and "key preserved"; startup failures
     on stderr; `sqlgen://config` serving `generation_config`; the go-sdk limitation; the §27
     cache opt-in; per-table `events.enabled` opt-out only, with a new §4.13 rule; the RFC3339
     timestamp; and four slips (`user.md`, ten re-exported sentinels, `ErrEmptyFilter` on the
     mutating `*Where` methods only, the `entities/user.json` example). **PRD gaps: 48** (16
     `manifest.md`, 32 `mcp.md`), all open.
   - **Tests written.** 23 new test files across `manifest`, `mcp`, `cli`, `gen`, `config` and
     `cmd/sqlgen` (the example-line compile check), by two fail-first agents, five fix agents,
     four test-writing agents and a review-fix agent. Source-mode parity tests run against live
     databases and are registered as `integration:`. Golden churn: the manifest files of the
     eight manifest-enabled examples, plus 14 `models_gen.go` and 2 `client_gen.go` for the
     pointer.
   - **Review.** Three sqlgen-reviewer passes, each finding real defects: the four above, then the
     method-order test that guarded nothing. The CLI slice's lesson held: an agent's "this test
     covers X" was wrong for 1 row in 11 here, down from 1 in 6, because the checkers ran before
     any test was written.
   - **Report.** `make conformance` at `446a9834`: 3588 rows, 3456 pass, 0 fail, 0 skip, 0 not
     run, 132 untested (51 in `cli/api-http.md`, 27 in `cli/gen.md`, 16 in `cli/manifest.md`,
     11 in `cli/api.md`, 10 in `cli/mcp.md`, 7 each in `cli/config.md` and `cli/gotype.md`, 2 in
     `client.md`, 1 in `cli/commands.md`).
   - **Time.** About 4 hours of wall time: 20 minutes to scope and split, 15 to map with four
     agents, the claim check, seven rounds of questions (26, four of them the matrix and split), seven fix agents, four
     test-writing agents, three review passes and the gates.
   - **For the next slice:** a fix ruled for the manifest can describe behavior the generator
     does not have (the cache opt-in), so check that the code a fix reports on actually exists
     before shipping the report; a test that compares two committed outputs guards nothing that
     produces them, so mutate the producer once and watch the test fail; a PRD amendment drafted
     in a ruling question is a claim too, so give it the same fill test before it lands; agents
     writing tests in batches name them after the batch, so assign descriptive names in the
     brief; and a rename map applied by script must remap refs to deleted tests, not drop them.
   **Seventh slice, config fields (§4) and the feature config blocks, recorded 2026-10-28:**
   - **Scope.** §4, §4.1, §4.2, §4.4–§4.12, §27.2, §28.8, §29.2 and §29.11, gen.md's empty §32.6
     anchor, the 85 `cmd/sqlgen/config` tests no row claimed, and `config.md`'s untested rows. The
     four `fk` list-length rows are Phase 32.3 planned work and stay untested.
   - **Split first.** About 600 new rows were estimated against `config.md`'s 473 lines, so
     `cli/config-fields.md` took §4–§4.12 and `cli/config-features.md` took §27.2, §28.8, §29.2
     and §29.11. Neither lists a package: `req-check` reads an unclaimed list per file, so the
     `cmd/sqlgen/config` package line and its unclaimed list stay in `config.md`. §4.12 and §32.6
     got rows by ruling (each example loads, validates and matches the schema).
   - **Matrix shape.** Two shapes, declared with the user, each per file because a matrix is
     declared once: `config-fields` / `feature-fields` (rows = every field with a PRD default,
     generated from the tables' Default columns; cols = `default`, `explicit`, `schema`) and
     `config-resolution` / `feature-resolution` (rows = every field set at several levels; cols =
     `table-wins`, `table-inherits`, `view-wins`, `view-inherits`). 237 rows fill cells; 23 `n/a`,
     each citing §4.9 (ViewConfig lacks the field), §27.2 (serializer is global) or §4.6.
   - **Anchors.** 17: §4, §4.1, §4.2, §4.4–§4.12 in `config-fields.md`; §27.2, §28.8, §29.2,
     §29.11 in `config-features.md`; §5.5 in `parser.md` (the config-key qualification tests),
     which moves it to `in-progress`. The §4 anchor hashes the whole chapter, so any §4.x edit
     trips its two rows.
   - **Rows seeded: 499**, 326 in `config-fields.md`, 134 in `config-features.md`, 33 in
     `config.md` (the `SQLGEN_DB_*` overrides, the schema drift row and five new §4.13 rule
     families), 3 in `parser.md` and 3 in `gen.md`. Four mapping agents drafted 474 in about 15
     minutes; they claimed every `config-resolution` cell twice (§4.6 and §4.8 state the same
     order), merged onto the §4.6 rows.
   - **Claim check before tests.** Two read-only checkers flagged 118 of 475 rows (1 in 4, up from
     1 in 11): every `× schema` row rested on the drift test, which checks presence and never the
     type; fixtures where table and global values were equal, or the global equalled the default;
     a `max_depth` default nothing reads; a MySQL "never ambiguous" fixture with one table;
     template substrings for runtime behavior; and expected values read from the code's own
     constants. 111 edits applied, 4 rejected with reasons, 96 rows emptied for the writers.
   - **Gaps.** 169 untested rows went to four writers with assigned prefixes (`tsc` schema types,
     `tdf` §4.1–§4.5 and the examples, `tgf` §4.6–§4.11, `tft` features). The schema writer's 130
     subtests reject a wrong type at each path and accept each closed-set value; 63 scripted
     `v1.json` mutations each failed. 1 row stays untested besides the 4 planned: the resolver
     invoked once across a nested mutation, which only a runtime count in a tenancy-plus-nested
     example could show. Every `cmd/sqlgen/config` test backs a row or is unclaimed (7).
   - **Defects: 17 ruled**, each with a fail-first test:
     1. A partial `cache.hydration` block loaded `enabled: false`.
     2. A partial `cache.circuit_breaker` block got no defaults, so `enabled: false` alone failed
        validation and `failure_threshold: 3` alone disabled the breaker.
     3. `tenancy.type` always required `import`, unlike the §4.7 rule it claims.
     4. An absolute `output.enums.file` / `output.types.file` was joined under `output.dir`.
     5. That file outside `output.dir`, or a different `package`, generated a package that does not
        build; now a §4.13 error (ruled over cross-package wiring).
     6. A cached view's `invalidate_on` naming an `exclude_tables` table passed.
     7. A relationship without `table` failed late with exit 1.
     8. `output.dir` left at `"."` derived the package name `"."`.
     9. `input.parse_mode` was never read (§5.6 says duplicates always error); removed.
     10. `tables.<t>.overrides.use_pointers` was accepted and ignored; now an error.
     11. A per-entity `tenancy.enabled: true` under a global off was ignored; now an error.
     12. `views.<v>.sql` was never read and `struct_name` never required; `sql` removed, `struct_name`
         optional.
     13. An explicit `soft_delete_columns: []` / `update_columns: []` became the default list; `[]`
         now turns detection off and `cursor_keys: []` is an error.
     14. Non-strict `Update` on a missing key returned `ErrNotFound` through its read-back, an
         empty update included; `UpdateWithRelated` now writes nothing nested for a missing parent.
     15. An M2M edge with only `junction` queried column `""`; keys are inferred per side.
     16. A relationship's `description` was never read.
     17. `extras` field imports were never emitted and `extras` / `fields` keys were re-cased
         (`CIDRs` → `CidRs`); keys are verbatim and a non-identifier is a §4.13 error.
   - **PRD amendments: 16**, all ruled: §4.2 drops `parse_mode`; §4.5 states the package
     derivation and the file placement rule; §4.6 and §4.8 name the six per-table generation fields
     and the empty-list meaning, and the non-strict nested update; §4.8 makes a table's
     `overrides` `types`-only and `tenancy.enabled` opt-out only; §4.9 makes `struct_name` optional
     and drops `sql`; §4.11 says the enum default is singular PascalCase; §29.2.2 says the same for
     tenancy; and §4.13 gains six rules (relationship `table`, tenancy opt-in, per-table
     `use_pointers`, empty `cursor_keys`, the enums/types file placement, extras names). **PRD
     gaps: 23** new (20 `config-fields.md`, 3 `config-features.md`) plus one in `manifest.md`, all
     open; three gaps the rulings settled were deleted. Four adjacent findings went to
     `backlog.md`.
   - **Tests written.** 25 new test files across `config`, `gen`, `gotype`, `cli`, `event`, three
     example modules and the tenancy example, by four fix agents, four writers and two clean-up
     passes; one runtime unit test generates a SQLite and a pgx package into a temp module and runs
     it. Golden churn: 8 files, the Update bodies and the MySQL / events nested guard.
   - **Review.** Two claim checkers and two sqlgen-reviewer passes. The first found stale comments
     and an ambiguity guard the placement rule made dead (removed); the second found a test that
     pinned a single-string junction key on a composite PK that §4.13 rejects, fixtures that could
     not tell a materialized view, a derived package or a joined path apart, and seven duplicate
     tests from parallel writers, all fixed or folded.
   - **Completion sweep.** With each `cli/*` file set to `complete`, eight pass and are flipped:
     `config.md`, `config-fields.md`, `config-features.md`, `api.md`, `api-http.md`,
     `api-schema.md`, `manifest.md` and `mcp.md`. Three do not: `commands.md` (17 orphans: 16 cli tests
     of schema sources, views, introspection and PK overrides, which back §5, §6, §8.6 and §16
     rows, plus `TestE2EDatabaseTwin`), `gotype.md` (4 orphans that predate this slice) and
     `gen.md` (503 orphans: the gen package's tests were never fully mapped).
   - **Report.** `make conformance` at `f9e3a49e`: 4087 rows, 3956 pass, 0 fail, 0 skip, 0 not
     run, 131 untested (51 in `cli/api-http.md`, 27 in `cli/gen.md`, 16 in `cli/manifest.md`,
     11 in `cli/api.md`, 10 in `cli/mcp.md`, 7 in `cli/gotype.md`, 4 in `cli/config.md`, 2 in
     `client.md`, 1 each in `cli/commands.md`, `cli/config-fields.md` and
     `cli/config-features.md`). The first run failed one row: an `event` subtest that read
     `log.Writer() == os.Stderr`, which `go test -json` breaks; it asserted the standard library,
     not sqlgen, and was dropped.
   - **Effort.** Not timed. Mapping with four agents took about 15 minutes; then the claim
     check, seven rounds of rulings (21 questions), seven fix agents, four test-writing agents,
     two review passes with their clean-ups, and the gates.
   - **For the next slice (parser.md):** the claim-check hit rate rose to 1 in 4 here, mostly
     schema rows resting on a presence-only test, so a mapping agent must name what a test checks
     (presence, type, value) before citing it; generate matrix rows only where a column can fail
     differently from its siblings; when two agents may claim one cell (a section that restates
     another's rule), assign the cell in the brief; parallel writers duplicate tests, so give each
     the others' file list; a mapping script must take only test references from an agent's cell,
     never its notes; and a fixture's backup directory must be unique per agent.

   **Eighth slice, parser (§5, §6, §16.1, §16.2, §17.1), recorded 2026-10-28:**
   - **Scope.** Every section the ownership map gave `parser.md`, its six declared matrices, the
     450 top-level tests in `parser`, `parser/postgres`, `parser/mysql`, `parser/sqlite` and
     `parser/introspect`, and the 17 cli tests `commands.md` could not claim. Phase 32's §5.3
     detection landed in 32.2, so no §5.3 row waits on planned work.
   - **Split first.** About 900 rows were estimated, so `parser.md` became a directory of three
     per-package files: `parser/dialects.md` (`postgres`, `mysql`, `sqlite`; §5, §5.1, §5.2 and
     `schema-model`), `parser/schema.md` (`parser`; §5.3–§5.7, §16.1, §17.1 and the
     relationship, soft-delete, qualification and determinism matrices) and
     `parser/introspect.md` (`introspect`; §6–§6.4, §16.2 and `introspection-filtering`). Each
     carries its own packages' line, so each keeps its own unclaimed list.
   - **Matrix shape, ruled before seeding.** `both` seeds the file parser with the introspected
     schema and runs the same `ParseFiles`, and detection runs once on the final model, so the
     source columns collapse where the code makes them identical. `schema-model`: 23 rows from
     the §5.1 table's elements and attributes × dialect × {files, database} (138 cells, 26 `n/a`
     from §5.1's No cells and §5.5). `relationship-detection`: 22 FK shapes from §5.3's rules ×
     {postgres, mysql, sqlite, source-parity}. `soft-delete-detection`: the five §4.6 default
     columns × {detected, outranks-lower}, since detection matches names only.
     `determinism`: the seven sorted elements × shuffled-input, since one `Schema.Sort` serves
     every source. `qualification` and `introspection-filtering` kept their declared shape.
   - **Rows seeded: 1003** (423 in `dialects.md`, 353 in `schema.md`, 227 in `introspect.md`),
     plus 11 new rows in other files for parser tests that back their sections, in
     `cli/gen.md` (§16, §13.7) and `client.md` (new §16.5.1 and §16.5.2 anchors) and refs added
     to 5 rows in `cli/gen.md` and `cli/gotype.md`. Seven mapping agents drafted about 990 rows
     in about 15 minutes; 76 tests they handed to another agent's section were left orphaned and
     a resolver placed them (31 refs on existing rows, 19 new rows, 26 unclaimed). 58 tests are
     unclaimed with reasons.
   - **Claim check before tests.** Six read-only checkers over about 840 tested rows dropped 63
     refs and added 38 (1 row in 12, down from 1 in 4): presence-only constraint and flag tests,
     a source-parity helper that checks its want edges only as a subset and so cannot show an
     edge is absent, sort fixtures where schema-first and name-first orders agree, soft-delete
     fixtures whose column order equalled the list order, a MATCH row never parsing `PARTIAL`,
     `Match` oracles cited for message text, and a SQLite stored-DDL row resting on parity
     whose two sides share the file parser. Six rows were split and four restated.
   - **Gaps.** 176 untested rows went to five writers with assigned files and helper prefixes
     (`pgw`, `msw`, `rdw`, `qvw`, `inw`); 173 were filled, 31 new test files, all mutation-checked
     (a final pass ran the 61 mutations writers had deferred while I edited the producing files;
     one test that passed for the wrong reason was tightened). 2 rows stay untested: the Go type
     of a column beside a same-named domain (`gotype`'s job) and the MySQL/SQLite view files never
     reaching the DDL parser (a cli routing test). One row was removed by ruling (below).
   - **Defects: 3 ruled**, each with a fail-first test:
     1. PostgreSQL `RENAME TO` / `RENAME COLUMN` / `RENAME CONSTRAINT` (and, found in review,
        `SET SCHEMA`) on a skipped view, materialized view or partition failed with "table not
        found"; they are skipped and rekey the relation. The review found the first fix let a
        partition be renamed onto its parent, after which `DROP TABLE` overflowed the stack; a
        name already in use is now refused.
     2. MySQL `ALTER COLUMN … SET DEFAULT` / `DROP DEFAULT` were silently ignored; applied.
     3. §16.1's unrecognized annotation directive was ignored silently; it now warns, naming the
        directive and the file.
   - **PRD amendments: 6**, all ruled: §5.2 lists MySQL `ALTER COLUMN SET/DROP DEFAULT` (and
     Appendix B); §5.3 limits `REFERENCES t` with no column list to PostgreSQL and SQLite; §5.7
     says columns keep their declared order; §6.1 scopes "the database's view stands" to
     PostgreSQL and MySQL, since SQLite re-reads a re-created view; §6.1 states the catalog-view
     type check for PostgreSQL only, as no other dialect can reach it; §16.1 limits
     `CREATE OR REPLACE VIEW` to PostgreSQL and MySQL. Each landed with its rows' tests. **PRD
     gaps: 58** new, all open. Four adjacent findings went to `backlog.md`.
   - **Review.** Two sqlgen-reviewer passes: the first found the stack overflow; the second found
     a leaked PostgreSQL 18 container (cleanup terminated it with the cancelled `t.Context()`),
     view renames bypassing the collision check, an untested parent-rekey branch and an overlay
     test that keyed the overlay itself, all fixed.
   - **Completion sweep.** With the three parser files and `commands.md` set to `complete`,
     `req-check` passes: no orphans, empty cells or uncovered sections. All four are flipped.
   - **Report.** `make conformance` at `aaa5cf00`: 5098 rows, 4965 pass, 0 fail, 0 skip, 0 not run,
     133 untested, 2 of them in the parser files. The first run failed one row: a `prealloc`
     lint fix made the shape-parity helper return an empty slice where its no-edge case expects
     `nil`, which `-short` had hidden; fixed in its own commit.
   - **Effort.** Not timed. Mapping took about 15 minutes on seven agents; then the orphan
     resolver, six claim checkers, four rounds of rulings (11 questions), five writers, a mutation
     pass, two review passes with their fix-ups, and the gates.
   - **For the next slice (gen.md):** a writer's mutation backup restored after I had edited
     the same file wiped my edit, so freeze the files a writer may mutate while anyone else edits
     them, or have writers mutate in one shell command (mutate, test, restore); a `prealloc`
     lint fix turns a `nil` slice into an empty one, which `cmp.Diff` tells apart, and `-short`
     hides the integration tests that catch it, so run them after lint fixes; a PRD edit under a
     chapter anchor (`§16`) trips that anchor in another file, so rehash it in the PRD commit;
     and a handoff to another agent's section is a claim that it will be cited, so resolve
     handoffs before the completion sweep.

   **Ninth slice, cli files (`gen.md`, `gotype.md`), recorded 2026-10-29:**
   - **Scope.** The 494 `cmd/sqlgen/gen` tests no row claimed, the 4 `gotype` orphans, the 27
     untested rows in `gen.md` and 7 in `gotype.md`, and the parser slice's 2 leftovers. By owning
     section the orphans split 281 `client.md`, 116 `gen.md`, 59 `runtime.md`, 32 `api*.md`, 4
     `gotype.md`, 1 each `config-features.md` and `commands.md`. Ruled before mapping: rows go to
     the file that owns their section (option a), so `client.md` gains rows from the generator's
     side and `runtime.md` moves to `in-progress`; matrix cells in those two files stay empty for
     their own slices. Sections `gen.md` already held (§9.8, §9.9, §29.4) kept their rows.
   - **Split first.** `gen.md` had 999 rows, twice ruling 7's ~500. §8.5 and the `naming` matrix
     with its 117 `n/a` cells and 11 gaps moved to `cli/naming.md` (428), and §32 and the
     `access-control` matrix to `cli/access-control.md` (90). The packages line and its orphan
     check stay in `gen.md`.
   - **Rows seeded: 212** (5098 → 5310), most in `client.md` (34 → 185 entries) and the first 62 in
     `runtime.md`, plus about 100 refs on existing rows. Nine mapping agents, one per group of PRD
     subsections (§9.8.x split by method), placed 457 tests; 37 handed between sections were placed
     by a resolver. Golden tests backed a row only after a generator mutation failed them: 60 such
     mutations, run one at a time after mapping so no agent tested a mutated tree, all failed. 177
     tests are unclaimed with reasons (174 `gen`, 3 `gotype`).
   - **Claim check before tests.** Six read-only checkers over the 275 rows this slice touched
     dropped 40 refs, added 23 and narrowed Applies-to on 17 (the review that followed dropped 3 more): presence-only
     template checks, substrings shared between methods (`Exists` / `ExistsWhere`, `Raw` /
     `Querier`), expectations taken from the fixture (`PKAutoGenExpr` written by the test), TTLs
     and orders equal to the defaults, a json-array row on a hand-built context, and one-dialect
     renders on `all` rows. Two rows were restated and the dropped tests listed as unclaimed.
   - **Gaps.** Two writers (`gnw`, `gtw`) filled 18 rows with new files only, each mutation-checked
     in one shell command; a review then hardened four (the 0755 test re-execs under `umask 000`,
     `UpdateMany`'s item type covers every dialect, tenanted `Count` runs generated code, the probe
     module stops pinning versions). 15 rows stay untested: 11 composite foreign-key rows waiting
     on Phase 32 (32.4–32.6) and the 4 REST rows of §32.3 (REST generation is deferred).
   - **Defects: 5 ruled fixes**, each with a fail-first test: stdlib `UpdateMany` ran without a
     transaction (§9.8.6a, §19.4); `Client.Close` closed only the querier (§20.2); MySQL's tenanted
     `UpdateMany` pre-read every item before the batch loop (§29.5), and the review of that fix
     found a repeated key losing its pre-mutation tenant; a bare built-in name reached another
     schema's type of that name (§7.1), and its review found the domain-base hop did too. One
     suspected defect (an inline PK restated in config losing its conflict target) did not
     reproduce: the "known limitation" subtest built a shape no parser produces, now corrected.
     Five composite-key findings are Phase 32 work, not defects.
   - **PRD amendments: 7**, all ruled, each with its fill test: input structs carry only `json`
     tags; §9.4, §9.8 and §9.8.11 take §14.1's Paginate shape; §9.8.12 and §14.2 put `TotalCount`
     on `Connection[T]`; §27.5 hashes columns in declaration order (a positional msgpack encoding
     depends on it) with a bare `custom` serializer; §7.1 keeps built-in names out of the
     bare-name fallback; §7.4 drops `type_map` from the uuid retype; §7.6 says a typed JSON
     override implements `Scan`/`Value` on `database/sql` drivers. **PRD gaps: 23** new, all open,
     among them the side effects of earlier committed sub-batches when a later one fails. One
     adjacent finding (the pagination template's unchecked nil result) went to `backlog.md`.
   - **Review.** Seven sqlgen-reviewer passes (registry, amendments, the three fixes, the writers'
     tests, the gotype fix); each finding was fixed before its commit.
   - **Completion sweep.** With `gen.md`, `gotype.md`, `naming.md` and `access-control.md` set to
     `complete`, the first run listed 22 `gen` orphans, the tests the claim check had dropped plus
     one new test; once listed or cited, `req-check` passes and all four are flipped.
   - **Report.** `make conformance` at `be42a359`: 5310 rows, 5198 pass, 0 fail, 0 skip, 0 not run,
     112 untested, 15 of them in the four files this slice completed.
   - **Effort.** Not timed. Mapping took about 10 minutes on nine agents; then the resolver, six
     claim checkers, three rounds of rulings (12 questions), the fixer, two writers, a follow-up
     writer, seven review passes and the gates.
   - **For the next slice (`client.md`, method by method):** many rows in `client.md` now come from
     the generator's side (template tests), so seed each method's `client-methods` cells from the
     conformance suite and add its refs to those rows rather than writing new ones; a template
     golden is the strongest generator-side proof, but only after a mutation; a worktree an agent
     creates may start from an old commit, so pin it (`git checkout --detach <sha>`) before it
     runs anything; two lint runs in parallel trip golangci-lint's lock, so gate one tree at a
     time; and an executed probe (`gnwRunGeneratedProbe`) catches what a rendered-text check
     misses, as the tenanted `Count` mutation showed.

   **Tenth slice, `client.md` delete family, recorded 2026-10-29:**
   - **Scope.** `SoftDelete`, `SoftDeleteMany`, `SoftDeleteWhere`, `Restore`, `RestoreMany`,
     `RestoreWhere`, `HardDelete` and `HardDeleteWhere` brought to the `HardDeleteMany` standard, §29.8,
     and the two untested batch-superset rows. Measured first: 137 example tests had one of these
     methods as their subject (38 portable on every dialect, the rest cache, events, tenancy, MySQL
     affected-key or bool-strategy tests); of the 80 cells, 7 were `n/a` with a PRD citation (by-key
     nil input, hard-delete field selection and lock mode), none needing sign-off; no concern needed a
     capability once subtests assert outcomes rather than the §9.8.8 statement counts.
   - **Rulings before writing (8):** grow the schema (a composite-key soft-delete table `revisions`, two
     CHECKs on `documents` a soft delete and a restore can trip, and a case-insensitive key table
     `labels`: `CITEXT`, `utf8mb4_0900_ai_ci`, `NOCASE`); lock mode is a row where the method re-reads
     and `n/a` on the hard deletes; no capability yet; one defect fixed in code; four PRD amendments.
     Three more came from the writers: §9.8 item 4's scoping, RestoreMany in §9.5's batch-failure
     list and the §9.3 no-match wording.
   - **Suite.** Four writers, one per method pair, each with its own file, helper prefix and pinned
     mutation worktree; every concern was mutation-checked, on MySQL too where its body differs. The
     suite went from 17 to 126 subtests per dialect, all passing on the four targets. Two equivalent
     mutants: a mutation-side empty-`FieldOptions` guard cannot be observed, because `GetMany` returns
     `nil, nil` for an all-false selection (logged in `backlog.md`).
   - **Claim check before porting.** Two read-only checkers found 6 defect groups, all fixed: 8 rollback
     subtests passed for a no-op (now each checks the write inside the transaction), two
     field-selection subtests a sibling method would pass, 5 composite fixtures whose matched tuples
     formed a full per-column cross product (the pilot's `HardDeleteMany` one included), batch hooks
     compared as sets where §9.5 says list order, a missing `Input` check, and a collation left to the
     server default.
   - **Rows.** `client.md` 173 → 260 rows (5310 → 5397 in all), with conformance refs added to 10
     existing rows rather than new ones. All 90 delete-family cells are filled or `n/a`, and
     `edge.batch-db-matched-superset-returning` now runs on every dialect.
     `edge.batch-db-matched-superset-tenanted` stays untested until a tenancy suite exists (§8).
   - **Ported:** 32 per-dialect example tests (the `*Where` empty-filter and zero-match tests,
     `TestHardDeleteWhere`, `TestArticleSoftDeleteWhere`, `TestHardDeleteByCompositePK`, SQLite's
     missing-key and timestamp tests). The PostgreSQL and MySQL timestamp tests stay because they
     assert recency, and the bool-strategy, already-soft-deleted and `TestEmptyBatch` tests stay too.
   - **Defect: 1, ruled and fixed** (`80aba8d5`). `SoftDeleteWhere` and `RestoreWhere` ANDed the
     `DeletedAt` comparator their doc comments said they ignore, so a contradicting comparator matched
     nothing and a comparator-only `RestoreWhere` restored rows instead of returning `ErrEmptyFilter`.
     Four fail-first subtests landed with the fix. `/verify` did not run, because the fix has no FIX ID.
   - **PRD amendments: 7**, each with its fill test or rows (`e294dc4b`): §9.6's empty-`FieldOptions`
     sentence (keys are still learned), §9.8 item 4 (the default is a read-method rule; `UpdateWhere`
     and `HardDeleteWhere` address every matched row), §9.5 (RestoreMany), §9.3 (no entities and no
     error), the §9.8.8 samples (`Input: ids`, the comparator dropped) and §29.8 (no `IncludeDeleted`).
     §29.8 is seeded from the tenancy example. The review of the §9.8 amendment found that
     `UpdateWhere`'s trailing read drops the soft-deleted rows it wrote; that is logged in `backlog.md`
     for the writes group. `gap.batch.partial-failure-side-effects` is still open.
   - **Review.** Four sqlgen-reviewer passes (schema, suite, fix, PRD); each finding was fixed or
     logged before its commit.
   - **Completion sweep.** With `client.md` set to `complete`, `req-check` failed only on the 10 owned
     sections with no rows (§9.1, §9.4, §15.3, §20.3, §20.4, §20.5, §22.3, §25.4, §29.7, §29.9). It
     reported no empty cells for the other 19 methods, because matrix rows were an open set: a method
     with no row was not a row, and flipping the file would not have enforced their 190 cells. Ruled
     after the sweep: a matrix declaration may list its rows (`rows=method:Get,GetMany,…`), and
     `client-methods` now lists all 28, so their empty cells are notes until the file goes `complete`.
   - **Report.** `make conformance` at `e294dc4b`: 5397 rows, 5286 pass, 0 fail, 0 skip, 0 not run,
     111 untested; `client.md` 259 pass, 1 untested.
   - **For the next slice (reads: `Get`, `GetMany`, `Count`, `Exists`, `ExistsWhere`, `Stream`,
     `Paginate`, `Connection`):** it fills §9.1 and §9.4 and is the first group where lock mode and
     the soft-delete default are real cells. A rollback subtest must show the write inside the
     transaction, or a no-op passes it. A composite fixture must make the matched tuples cross, or a
     per-column `IN` passes. Writers mutating in their own pinned worktrees let four run in parallel
     with no cross-talk. On MySQL a re-soft-delete inside one second leaves `CURRENT_TIMESTAMP`
     unchanged, so a changed-rows mutation is caught only by timing. Lastly, the matrix's closed row
     list makes the reads' 80 empty cells notes from the start, so the sweep can count what is left.

   **Eleventh slice, `client.md` reads, recorded 2026-10-29:**
   - **Scope.** `Get`, `GetMany`, `Exists`, `Count`, `ExistsWhere`, `Paginate`, `Connection` and `Stream`
     brought to the delete-family standard, with §9.1, §9.4 and §15.3, which had no rows. Measured first:
     211 example tests had one of these methods as their subject (95 portable, the rest views,
     tenancy, cache, driver error codes, SQL text or pool statistics); lock mode needed the first
     capability; the schema already had one relationship (`products` → `order_items`, one-to-many), so
     it did not grow; no cell needed statement counts inside a transaction, though "a refused `Stream`
     issues no SQL" cannot be shown there (§4.3), so that subtest shows that no hook fires.
   - **Rulings before writing (10):** the first capability, `Caps.RowLocks` with `Require` and `Lacks`
     (true on PostgreSQL and MySQL, false on SQLite), and a row-lock row in Appendix B; reuse
     `products`/`order_items`, leaving one-to-one and many-to-many to the nested-writes slice;
     `products` ordered by `cursor_keys: [price, id]`, so the cursor order is not the insertion order;
     multi-method rows split per method; two defects and one conflict fixed in code; three PRD
     amendments; and one `n/a` signed off (`Stream × composite-pk`). Six more came from the claim
     check and the review: three Stream prose lines (§9.4a), §17.3's read-method default, `LockMode`
     ignored on `Count`, `Exists`, `ExistsWhere` and `Stream` (§9.6a), and reads that match nothing
     (§9.5).
   - **Suite.** Four writers, one per method group, each with its own file, helper prefix and pinned
     mutation worktree. The suite went from 126 to 225 subtests per dialect, all passing on the four
     targets. The lock subtests prove a lock with a contending transaction (`otherTx`), and `NOWAIT`
     with a positive control after release. `order_items` takes `page_size: 2`, so the default page
     size is observable. Equivalent mutants: removing `Get`'s own lock guard (its inner `GetMany`
     re-runs it), and dropping a tiebreaker on a composite or rowid table, where the database answers
     ties in key order whatever the `ORDER BY`.
   - **Claim check before porting.** Two read-only checkers found 18 defects, all fixed: fixtures where
     the target was the first row a loosened key also returns, a soft-delete fixture with no live row,
     lock subtests a `LockMode`-ignoring read would pass, a presence-only `NOWAIT` check, a transaction
     precondition claimed on SQLite where the dialect guard fires first, tie orders that matched
     insertion order, a page size below the default, a `t.Fatal` inside a transaction function, a dead
     assertion, and a leaked result set that would have hung the rest of the Stream run.
   - **Rows.** `client.md` 260 → 329 rows (5397 → 5466 in all): 69 new rows, conformance refs on 21
     template-seeded rows, the `lock.requires-transaction` and `lock.sqlite-rejected` rows split per
     method, and `gap.get-many.field-options-not-mutated` turned into a row. All 80 read cells are
     filled (59) or `n/a` (21), and §9.1, §9.4 and §15.3 have rows.
   - **Ported:** 73 per-dialect example tests (the `Count`, `Exists` and `ExistsWhere` tests, composite
     `Get`, the offset and cursor walks, the lock guard, savepoint, read-modify-write and `SKIP LOCKED`
     tests, the Stream baseline, soft-delete, row-failure, hook-error and refused-transaction tests,
     graphql's nil-input read check). They went in the commit after the port (`03cb7a5b`), not the
     same one as ruling 6 says, after a review of the deletions added the short last page the offset
     walk covered. The `NOWAIT` driver-code tests, SQL-text, pool, cancellation and buffering tests,
     views and the 3-column-key tests stay.
   - **Defects: 2, and one PRD conflict, ruled and fixed** (`f5ddc894`). An `OFFSET` with no `LIMIT`
     was emitted bare, which MySQL and SQLite reject, so `Paginate` with an offset and a zero limit
     failed; the builders now write the dialect's new `NoLimitClause()` first. `Paginate`'s `COUNT` ran
     before its page read's lock guard; it now takes the guard itself. And a zero `PaginateInput`
     returned every row with `HasMore` true; `HasMore` now needs `Limit > 0`. `/verify` did not run,
     because the fixes have no FIX ID.
   - **PRD amendments: 14**, each with its fill test (`f5ddc894`, `5c66ef52`): §9.6a and §9.8.11
     (`Paginate` takes a lock), §9.8, §14.1 (`HasMore`), Appendices A and B (`NoLimitClause`, row locks,
     a bare `OFFSET`), §9.6 (`GetMany`'s empty selection returns no entities and no error; no read
     writes to the caller's `FieldOptions`), §9.6a (`LockMode` ignored on four reads), §9.5 (reads that
     match nothing), §9.4c and §17.3 (every read adds the soft-delete default), §9.4a (result-set
     errors, early break, hook errors yielded last) and the §9.8.10 `Count` sample. Five findings went
     to `backlog.md`, among them §21.2's missing `OpStream` (runtime.md's) and `Connection` with
     `First: 0`.
   - **Review.** Four sqlgen-reviewer passes (prep, fix, suite and registry, deletions); each finding
     was fixed, logged or ruled before its commit.
   - **Completion sweep.** The 8 reads have no empty cell, no untested row and no unsigned `n/a`. With
     `client.md` set to `complete`, `req-check` fails only on the 110 empty cells of the 11 write and
     nested-write methods and on 7 owned sections with no rows (§20.3, §20.4, §20.5, §22.3, §25.4, §29.7,
     §29.9). Not flipped.
   - **Report.** `make conformance` at `03cb7a5b`: 5466 rows, 5355 pass, 0 fail, 0 skip, 0 not run,
     111 untested; `client.md` 328 pass, 1 untested (`edge.batch-db-matched-superset-tenanted`).
   - **For the next slice (writes: `Create`, `CreateMany`, `Update`, `UpdateMany`, `UpdateWhere`,
     `Upsert`, `UpsertMany`, `Increment`):** it fills 80 of the 110 remaining cells and is where
     error mapping becomes a real cell for most methods (unique, FK, check, not-null). The backlog's
     `UpdateWhere` return set (soft-deleted rows it writes but does not return) lands there. A
     composite tiebreaker cannot be proven on a table the database stores in key order, so pick a
     fixture that is not. Check the suite's own claims about oracles: a yield inside the hook chain is
     recovered, so a range panic is not an oracle there. Finally, port in the same commit as the
     suite when the deletions are known up front; reviewing them first moved this slice's deletions
     to a commit of their own.

   **Twelfth slice, `client.md` writes, recorded 2026-10-30:**
   - **Scope.** `Create`, `CreateMany`, `Update`, `UpdateMany`, `UpdateWhere`, `Upsert`, `UpsertMany`
     and `Increment` brought to the reads standard. Measured first on all four targets: 272 example
     tests had a write as their subject (131 portable, 66 once copies across modules count once; 26
     dialect-specific; 114 tied to cache, events, tenancy or introspection), and only 24 were cited
     in `client.md`. 74 of the 80 cells are real. No capability was needed: MySQL's affected-key
     lookup and `LAST_INSERT_ID` against `RETURNING` differ only in mechanism. No cell needs
     statement counts inside a transaction, so sub-batch splitting is shown by outcome (a later
     sub-batch fails and the earlier ones stay). Known blind spot: `CountingQuerier.Begin` records
     nothing, so stdlib `UpdateMany`'s implicit `BEGIN` is invisible.
   - **Rulings before writing (8):** a `counters` table on every dialect (`UNIQUE` name, `CHECK
     (hits >= 0)`, `NOT NULL updated_at` with no DEFAULT, `strict_updates: false`), so unique,
     check and not-null are reachable on one table; three harness changes (`CountingQuerier`
     pins a session, the pgx module builds clients on a wrapper that forwards `SendBatch`, and
     `Harness.Base` names the querier clients are built on); three defects fixed in code (MySQL
     1364, write read-backs, the pgx `UpdateMany` fallback); `UpsertMany`'s SET clause on conflict
     stated in §9.5; §9.6a's `LockMode` list and an unmatched `UpdateMany` key stated; and one
     `error-mapping` column, filled by one subtest per constraint kind the method can reach, with
     the row saying why the others cannot occur. Ten more came during the slice: `SendBatch`
     errors mapped in the pgx adapter; `UpsertMany`, the three `*WithRelated` terminal reads and
     the GraphQL update resolvers' re-reads folded into the read-back fix; `UpdateWhere`'s empty
     path kept; the PRD's "then call `Get`" wording amended to the read-back; a case-insensitive
     `UpsertMany` that the database merges errors on its short read; §9.8.5 states the `RETURNING`
     order `CreateMany` relies on; `UpsertMany` joins §9.5's batch-failure list; `CreateMany`'s
     return order left a PRD gap. The Go build cache filled the disk once (186 GB) and was cleared
     with the owner's approval.
   - **Suite.** Four writers, one per method pair (`cr`, `up`, `uw`, `us`), each with its own file,
     helper prefix and worktree pinned at the post-fix commit. The suite went from 225 to 375
     subtests per dialect, all passing on the four targets. Each concern was mutated once, on
     MySQL too where its generated body differs: about 48 mutations for `Create`/`CreateMany`, 70
     for `Update`/`UpdateMany`, 58 for `UpdateWhere`/`Increment` and 57 for `Upsert`/`UpsertMany`.
     Equivalent mutants, reported and claiming no cell: the empty-`FieldOptions` early return in
     `CreateMany`, `UpdateMany`, `UpdateWhere` (twice) and `UpsertMany`, whose terminal `GetMany`
     has the same guard; the MySQL session pin at `auto_increment_increment=1`; and `UpsertMany`'s
     per-row "no definite conflict value" refusal, which the target refusal always precedes.
   - **Claim check before porting.** Two read-only checkers found 11 defects, all fixed:
     single-item batches a first-item-only check would pass, three §17.5 subtests that checked
     only that `updated_at` moved (one "explicit value" half passed the stored value), an
     `Increment` happy path a read-then-write would pass, two composite targets created before
     their crossing tuples, a hook recorder still holding the previous call's keys, two rows citing
     only subtests a known mutant passes (they gained the refs that catch it), and a row stating a
     narrowed hook `Input` the PRD does not promise (now a PRD gap).
   - **Rows.** `client.md` 329 → 433 rows (5466 → 5572 in all, with `runtime.md`'s two new mapping rows):
     conformance refs on template-seeded rows, 6 `n/a` cells each citing the PRD (`Create`,
     `CreateMany`, `Upsert`, `UpsertMany` × not-found under §9.2; `Increment` × field-selection
     under §9.8.9a and × nil-empty-input under §9.4c), and 4 PRD gaps:
     `gap.create-many.return-order`, `gap.update-many.empty-item`,
     `gap.update-many.hook-input-narrowed`, `gap.upsert.hook-pk-nil`. The subtest pinning
     `gap.update-many.empty-item` left the suite, since every conformance subtest must be claimed
     by a requirement. All 80 write cells are filled (74) or `n/a` (6). No golden was newly
     claimed: the template goldens these rows cite were seeded before the slice, so none needed a
     mutation run.
   - **Ported:** 60 per-dialect example tests, in the porting commit itself (`8a6a7bcc`), as the
     eleventh slice advised; five files left empty were removed. A review of the deletions
     restored four whose shape the conformance schema cannot reproduce: MySQL
     `TestAffectedKeys_UpdateRecordsOnlyWrittenKeys` and
     `TestAffectedKeys_UpdateWhereRecordsCompositeKeysByColumn`, and PostgreSQL
     `TestUpsertMany_CallerKnownKey_CoveringTargetIssuesNoReadBack` and
     `TestUpsertMany_LinkTableStatementCount`.
   - **Defects: 5, each fixed with a fail-first test.** MySQL 1364 (a NOT NULL column with no
     DEFAULT left out of an INSERT) maps to `ConstraintNotNull` (`8a9f5782`). Without `SendBatch`
     (a wrapping middleware, or any call inside a caller's transaction) pgx `UpdateMany` ran a
     sub-batch's statements with no transaction; each sub-batch now runs in `WithTransaction`, a
     savepoint inside a caller's transaction (`fd10834e`). The pgx adapter's `SendBatch` returned
     unmapped driver errors (`6d662a3d`). A write's read-back applied the not-deleted default, so a
     write that left its row soft-deleted committed and then returned `ErrNotFound` or a short
     slice; the 7 flat writes, `UpsertMany`, the three `*WithRelated` terminal reads and the
     GraphQL `update<T>`, `update<T>s` and `update<T>WithRelated` re-reads now return it, and the
     backlog's `UpdateWhere` return-set item is closed (`426914a3`). `UpsertMany` deduped by Go
     value while MySQL and SQLite merged case-only duplicates under a case-insensitive key, so it
     returned fewer rows with no error; its terminal read now errors on the shortfall
     (`5ae35e2a`). `/verify` did not run, because the fixes have no FIX ID.
   - **PRD amendments**, each with its fill test: §8.1.7, §22.2 and §22.5 (1048 / 1364); §9.8.6a,
     §19.4 and §25.3 (the fallback and per-sub-batch atomicity on every driver); §22.2 (`SendBatch`
     mapping); §9.2, §9.7, §9.8, §9.8.4, §9.8.6, §9.8.7 and §9.9 (the read-back); §9.2 and §9.5 (the
     short read); §9.6a (every write method, `Restore*` included, ignores `LockMode`); §9.5 (an
     unmatched `UpdateMany` key, `UpsertMany`'s SET clause on conflict, `UpsertMany` in the
     batch-failure list); and §9.8.5 (`RETURNING` in VALUES order, with §9.5 and §9.7's "promises
     nothing" narrowed to `DO NOTHING`). One finding went to `backlog.md`: an error a write returns
     after its statement committed skips cache invalidation and events.
   - **Review.** sqlgen-reviewer passes on the prep, each fix, the suite and registry, and the
     deletions. Among their findings: a `SendBatch` test case named for the rows' `Err` path that
     never reached it (pgx's pipeline mode fails `Query` first; the case and row were narrowed),
     a resolver import gap on composite keys and a PK named `key`, §9.5's "every non-key,
     non-target column" contradicting its own exclusions, and the four deletions above. One
     process slip: the first read-back fix (`eb99d518`) was committed while `check-examples` was
     failing, because the wrapper's `echo` masked the gate's exit code. It was amended to
     `426914a3` after the gate passed, and gate exit codes now go to their own file.
   - **Completion sweep.** The 8 writes have no empty cell, no untested row and no unsigned `n/a`.
     With `client.md` set to `complete`, `req-check` fails only on the 27 empty cells of the 3
     nested-write methods and on the 7 owned sections with no rows (§20.3, §20.4, §20.5, §22.3,
     §25.4, §29.7, §29.9). Not flipped.
   - **Report.** `make conformance` at `8a6a7bcc`: 5572 rows, 5461 pass, 0 fail, 0 skip, 0 not run,
     111 untested; `client.md` 432 pass, 1 untested (`edge.batch-db-matched-superset-tenanted`, as
     before), and `runtime.md` 61 pass.
   - **For the next slice (nested writes: `CreateWithRelated`, `UpdateWithRelated`,
     `UpsertWithRelated`):** it fills the last 27 cells, then the 7 empty owned sections stand
     between `client.md` and `complete`. The schema's one relationship (`products` →
     `order_items`, one-to-many) will not do: one-to-one and many-to-many were left to this slice.
     The three `*WithRelated × soft-delete` cells are already filled by the read-back fix. A
     subtest that pins a PRD gap cannot live in the suite, so measure a behavior against the PRD
     before writing its subtest. And read a gate's exit code from its own file, never from the
     tail of a wrapper.

   **Thirteenth slice, `client.md` nested writes, recorded 2026-10-30:**
   - **Scope.** `CreateWithRelated`, `UpdateWithRelated` and `UpsertWithRelated` brought to the writes
     standard. Measured first: 51 example tests had a nested method as their subject (27 portable, 13
     dialect-specific: polymorphic, self-referential, tenancy, MySQL's key step or exact statement
     counts; 11 GraphQL surface), all in `graphql` and `mysql`. No conformance table could take a
     nested edge (`products` → `order_items` points at a composite-PK target, which fails E1). The
     three soft-delete cells cited only a PostgreSQL example test. No capability was needed: the
     nested bodies are byte-identical across dialects and MySQL differs only inside the inner flat
     calls. Every §9.9.7 count falls inside the method's own transaction, which `CountingQuerier`
     cannot see (§4.3), so no subtest counts statements and those rows keep their template refs.
   - **Rulings before writing (4):** the schema (`authors` with an O2M `posts` on a nullable FK, a
     config-declared has-one `author_profiles` on a UNIQUE nullable FK, and M2M `tags` through the
     pure junction `author_tags`; edges listed by name); §9.6a and §9.4c amended to name the three
     methods; and an empty-`FieldOptions` defect fixed in code. Seven more came during the slice:
     `batch_size: 2` on `authors`, because a nested verb chunks its id lists at the parent's batch
     size (whose size §9.9.7 means went to `backlog.md`); the edge allowlist reordered to Tags,
     Profile, Posts so the byte order of processing is observable; `ErrNestedVerbConflict`'s `ID` is
     the offending target under §22.2, so it is a row, not a gap; a nil create-list entry folded into
     the §9.4c amendment; §9.9.6 clarified that the terminal read's relationship loads take the
     caller's toggles, as §9.6 already said; the O2M half of "a repeated connect id is one adoption"
     read from §9.9.5's set-operation wording; and one anchor per cell type.
   - **Suite.** Three writers, one per method (`cwr`, `uwr`, `swr`), each with its own file and a
     worktree pinned at the post-fix commit. The suite gained 106 subtests per dialect, all passing on
     the four targets; the one in-transaction lock subtest needs `CapRowLocks`. A shared
     `beginCounter` shows a nil input begins no transaction, which `CountingQuerier` cannot. About 55,
     65 and 41 mutations, on MySQL too where an inner call differs. Equivalent mutants, claiming no
     cell: copying the caller's `LockMode` onto the parent write (each flat write ignores it), and
     onto the terminal read on PostgreSQL and MySQL, where the parent row is unseen by any other
     transaction or already locked by the write; there only the in-transaction `NOWAIT` subtest on
     `UpdateWithRelated` sees it.
   - **Claim check before porting.** Two read-only checkers found 23 defects, all fixed: a nil-input
     check a post-`BEGIN` guard would pass, a "past the batch size" subtest on a 200-row batch, a
     hook `Input` asserted by pointer identity, edge-order subtests whose order matched the config
     list, a chunking row a no-chunking implementation passed (now an exact per-call hook count), a
     verify-read row whose refs no mutant of the verify read fails (the refs left), a strip row cited
     to the wrong subtest, a verb-conflict fixture where a late check wrote nothing to catch,
     rollback subtests that did not show the earlier writes ran, an occupancy fixture whose only
     profile a loosened key also returns, two stale oracle comments and presence-only nits.
   - **Rows.** `client.md` 433 → 475 rows (5572 → 5614 in all): 42 new rows under §9.4c, §9.6a,
     §9.9 and the new §9.9.3, §9.9.5 and §9.9.9 anchors, conformance refs on 11 template-seeded
     §9.9.x rows and the §9.6 empty-`FieldOptions` row, three `n/a` cells citing §9.9.4 E3 and E1 (composite-pk), the three
     soft-delete rows moved to suite refs on every dialect, and one repointed ref in
     `cli/config-fields.md`. No PRD gap. No golden was newly claimed, so none needed a mutation run.
   - **Ported:** 16 graphql example tests, in the porting commit itself (`68587989`), among them
     the connect attribution, rollback, savepoint, nil-`FieldOptions`, unlink, clear, three-way
     split, both-branch upsert and soft-deleted-parent tests and the multi-edge terminal reads. Two
     were split: graphql's verb-conflict test keeps its explicit-key case, and MySQL's O2M adoption
     test keeps its REPEATABLE READ race. The NOT NULL O2M edge tests, the polymorphic,
     self-referential and tenancy tests, the MySQL key-step and statement-count tests, the
     `strict_updates: false` tests and the GraphQL surface tests stay.
   - **Defects: 1, fixed with a fail-first test.** An empty `&FieldOptions{}` returned a parent
     with only its key set, because the parent write forces the key on; the three methods now
     return `nil` (`26863337`). `/verify` did not run, because the fix has no FIX ID.
   - **PRD amendments**, each with its fill test: §9.6a (the three methods ignore `LockMode` and pass
     it to no inner call), §9.4c (a nil input returns `ErrNilInput` before the transaction; a nil or
     empty verb block writes nothing; a nil create-list entry is `ErrNilInput` on that edge's
     `create`), and §9.9.6 (the terminal read's relationship loads). Two findings went to
     `backlog.md`: whose `batch_size` §9.9.7 means, and §9.8.6's rationale for the escape on
     `Update`, which no nested call takes.
   - **Review.** sqlgen-reviewer passes on the prep, the fix, and the suite, registry and deletions.
     Among their findings: the parent batch size above, the five cell types filed under different
     anchors per method, and stale comments left in two example files after the deletions.
   - **Completion sweep.** The 3 nested methods have no empty cell, no untested row and no unsigned
     `n/a`; all 28 methods' 280 cells are filled or `n/a`. With `client.md` set to `complete`,
     `req-check` fails only on the 7 owned sections with no rows (§20.3, §20.4, §20.5, §22.3,
     §25.4, §29.7, §29.9). Not flipped.
   - **Report.** `make conformance` at `68587989`: 5614 rows, 5503 pass, 0 fail, 0 skip, 0 not
     run, 111 untested; `client.md` 474 pass, 1 untested (`edge.batch-db-matched-superset-tenanted`,
     as before).
   - **For the next slice (the 7 empty owned sections):** they are all that stands between
     `client.md` and `complete`, and several are prose a suite cannot show (§20.3's examples,
     §20.4's configuration), so measure which ones state a testable behavior before writing any,
     and expect `n/a`-style rulings for the rest. §29.7 and §29.9 are tenancy sections the core
     schema cannot reach without the variant suite (§8). A row's cited subtests must kill a mutant
     of what the row states, not of something nearby: two rows here cited subtests that no mutant
     of their own claim failed. And when a config list sets an order, check that it differs from
     the order under test.

   **Fourteenth slice, `client.md` owned sections, recorded 2026-10-30:**
   - **Scope.** The 7 owned sections with no rows: §20.3, §20.4, §20.5, §22.3, §25.4, §29.7 and
     §29.9. Measured first:
     - §20.3 is a code sample and §20.4 repeats §4.5 word for word, so neither states a behavior
       of its own.
     - §20.5 and §25.4 can be shown on all four targets. Under `-race`, with a query-hook barrier
       proving the calls overlap, a shared client, a fan-out on one `txCtx`, and a failed
       relationship load each behave as §20.5 says, so no defect turned up.
     - §22.3 departed broadly. 80 failing calls across the 28 methods showed five departures:
       1. pgx returned a RETURNING set's `rows.Err()` unwrapped.
       2. Flat methods returned their sentinels bare.
       3. `GetMany` said `get products:`, and `Get` never said `get product:`.
       4. On stdlib, `UpdateMany` lost its prefix when `Begin` failed.
       5. The other 27 operation words were unstated and irregular.
     - §29.7 and §29.9 runtime behavior exists only in the `tenancy` example (SQLite), because the
       MySQL and PostgreSQL tenancy schemas have no tenant-in-PK table.
   - **Rulings (11):**
     - §20.3 and §20.4 re-owned as `informative`. The stale §20.3 sample and the §8.7 signatures
       were fixed inline.
     - §29.7 and §29.9 cite `example: tenancy:` tests with Applies-to `sqlite`, with new tests for
       the gaps. The variant suite stays a §8 follow-on.
     - New suite files: `error_wrapping.go` and `concurrency.go`; the §25.4 subtests go in `get.go`.
     - Four §22.3 defects fixed in code: the pgx `rows.Err`, bare sentinels, the `Get`/`GetMany`
       prefixes, and the stdlib `UpdateMany`.
     - §22.3 amended with a per-method prefix table built on one rule: singular for one-row
       methods, `{verb} many {table}`, `{verb} {table} where`, Increment without the column, and
       Refresh / RefreshConcurrently for views.
     - The inner-prefix rule: the method's own prefix leads exactly once, and the cause may carry
       an inner method's prefix (`get product: get many products: …`). This resolved two rulings
       my wording had set in conflict.
     - Followed from those rulings without a question:
       - `tenancy.ErrMismatch` is prefixed too.
       - A §21.6 sentence says a recovered panic comes back under the prefix.
       - §29.7 and §29.4.2 no longer name the nonexistent `UpsertInput`.
       - The §9.4a, §9.6a and §9.8.x samples, `TENANCY.md` §10 and `ERRORS.md` §6 match the new
         rule.
   - **Fix (`101e8703`).** Built by one agent in a worktree pinned to the prep commit:
     - Each exported method now wraps once at its return, and inner steps keep only a step label.
     - Fill test: 49 `ErrorWrapping` subtests, with one or more per method, on every dialect.
     - 42 mutants on PostgreSQL and MySQL, none equivalent.
     - An AST scan by the reviewer found no exported return path left unprefixed.
     - Two reviewer passes added coverage: view and Refresh tests in the `postgres` example, a
       `tenancy.ErrMismatch` test, and a recovered-panic subtest.
     - `/verify` did not run, because the fix has no FIX ID.
   - **Suite (`d101220d`).**
     - Subtests: `Concurrency` (2), plus 5 `Get`/`GetMany` subtests for §25.4 and the failed load,
       and one §13.2 subtest for a multi-edge read inside a transaction. All are barrier-gated.
     - Six tenancy example tests: a matching PK proceeds, `SkipTenancy` writes across tenants by
       PK, the update input lacks the tenant, `SkipHooks` keeps the tenant filter, the hook
       `Input` is unchanged, and `AffectedPKs` carry the tenant.
     - Mutants killed: a memo cache and singleflight on each read, an errgroup without
       cancellation, a partial `GetMany`, a client field written in `Get` (`-race`), the `Tx`
       reservation removed (on pgx, PostgreSQL stdlib and MySQL), each verify-match forced to
       fail, `AffectedPKs` without the tenant, the tenant filter dropped under `SkipHooks`, the
       caller's input mutated, and the resolver consulted under `SkipTenancy`.
     - Equivalent mutant: the reservation removal on SQLite, whose driver serializes the shared
       connection itself. A subtest comment records it.
   - **Claim check.** Three reviewer passes on the suite commit found 4 defects, all fixed:
     - a hook-`Input` row whose test compared only the filter, which an injected internal
       condition passed;
     - a row citing a unit fixture with the tenant outside the PK;
     - a shared-client row whose goroutines all read one author, so crossed results would pass;
     - a §13.2 subtest whose two loads were not gated, so its kill was probabilistic.
   - **Rows.** `client.md` 475 → 521 rows (5614 → 5660 in all):
     - 31 under §22.3, plus one under a new §21.6 anchor;
     - 3 under §20.5, 2 under §25.4, 5 under §29.7, 3 under §29.9, and one under §13.2.
     - No PRD gap. Two adjacent findings went to `backlog.md`: the API layer's `InvalidInputError`
       strings, and whether §22.3's `{table}` includes a schema.
   - **Ported:** `TestLoadRelationships_MultiEdgeInsideTransaction`, deleted from the `sqlite`,
     `mysql` and `graphql` examples. What the deletion loses: a NOT NULL-FK O2M edge, and the
     polymorphic per-edge checks, neither of which is about serialization.
   - **Completion sweep.** With `client.md` set to `complete`, `req-check` passes. Flipped on the
     user's sign-off (`c1088a83`), after the report was generated.
   - **Report.** `make conformance` at `d101220d`: 5660 rows, 5549 pass, 0 fail, 0 skip, 0 not
     run, 111 untested. `client.md` has 520 pass and 1 untested
     (`edge.batch-db-matched-superset-tenanted`, as before).
   - **For the next slice:**
     - `go test -run` splits its pattern on `/` before alternation, so
       `X/(a/b|c/d)` matches nothing and a mutant reads as surviving with "fails=none". Run one
       pattern per subtest path.
     - When two rulings are worded separately, check them against each other before acting: here
       the `Get` prefix and the `Paginate` prefix rulings conflicted, and an extra question
       settled it.
     - A row about a field of a struct the hook sees needs the whole struct compared, unexported
       fields included.
     - A barrier only proves overlap for the reads it gates.

   **Fifteenth slice, the `runtime.md` split and `runtime/sql.md`, recorded 2026-11-01:**
   - **Scope.** `runtime.md` was the last `in-progress` file. As `complete` it failed on 567
     things: 37 owned sections with no rows, 8 empty matrices, 3 empty `error-mapping` cells and
     516 orphan tests. Claiming the orphans would take it past ruling 7's ~500 rows, so it was split
     first, and `runtime/sql.md` (`sql`, `comparator`, `omittable`; 168 orphans) was seeded.
   - **Split (`73619393`).** Six files under `runtime/`, each `in-progress`:
     - `sql.md`: `sql`, `comparator`, `omittable`; §10, §11.2–§11.5, §15.1, Appendix A and B.
     - `database.md`: `database` and its drivers; §18, §19, §22, §22.2.
     - `hooks.md`: `hook`; §21, §24, §24.2.
     - `cache.md`: the `cache` packages and `metrics/otel`, which records §27.13's cache metrics;
       §27, §29.5.
     - `events.md`: the `event` packages; §28, §29.6.
     - `support.md`: `types`, `manifest` and `tenancy`; §29.3, §30.6. `types` backs §7, whose rows
       are `cli/gotype.md`'s.
     - The 64 rows, 3 gaps and 9 matrix declarations moved unchanged. `lock-modes` went to `sql.md`,
       where the lock clauses are built.
   - **Rulings (15):**
     - The six-file split, and `sql.md` first.
     - §10.1 re-owned as `informative`, and §10.3 as `cli/gen.md`, which already held its rows.
     - Four matrices redeclared to the axes the PRD states:
       - `sql-builders`: the ten Appendix A builders × `basic`, `returning`.
       - `comparators`: by dialect and driver, with rows only for operators whose SQL varies.
       - `condition-composition`: six shapes × `placeholder-numbering`, `nested-in-and-or`, since
         no `Not` exists and no depth is stated.
       - `omittable`: `unset`, `set`, `set-zero` × `value`, `json-round-trip`.
     - Three PRD amendments with no code change:
       - Enum `In`/`Nin` expand on pgx too (§11.2, §11.3, Appendix A).
       - The PostgreSQL-only comparators are never selected elsewhere, rather than being a
         generation-time error (§11.2).
       - `BuildUpdate` writes the SET map it is given (Appendix A).
     - One §11.2 sentence: a universal operator named like a `ConditionBuilder` method produces
       that method's clause.
     - A drop-pin: the subtest asserting `DEFAULT` in a SQLite `VALUES` list.
     - Three code fixes (below), and two follow-up rulings on them: MySQL before 8.0 and MariaDB
       reject `OF`, so the client rejects the locked join read there before SQL; and a JSON null is
       the zero `T` even for a `T` with its own `UnmarshalJSON`, so a `types.JSON` null is SQL NULL.
   - **Defects: 3, each fixed with a fail-first test:**
     - `0e488fff`: a locked read that selected an O2O edge appended a bare lock clause after the
       `LEFT JOIN`. PostgreSQL rejected it, and MySQL locked the child too, so `NOWAIT` failed on a
       child held elsewhere and `SKIP LOCKED` returned the parent with the edge nil. The clause now
       names the parent alias (`FOR UPDATE OF a`). Fill test: a new `Get` suite subtest, failing on
       pgx, PostgreSQL stdlib and MySQL before the fix.
     - `321fbf0c`: a non-key `uuid` column overridden to `string` filtered as `String`, against
       §11.2. The uuid arm now runs before the string arm, on the model and GraphQL sides.
     - `5713b954`: decoding `null` into a `Value[int]` holding 5 left `5, true`, against §10.2.
     - `/verify` did not run, because the fixes have no FIX ID.
   - **Measurement.** Two read-only agents mapped the 168 orphans and every behavior sentence of
     the owned sections. Two writers then seeded `sql` and `comparator` in worktrees pinned to
     `0e488fff`; `omittable` was seeded inline. The writers ran about 175 mutants. Equivalent
     mutants: the no-column upsert path on MySQL, which drops the clause either way, and a doubled
     quote closing a literal, which no token scan can tell from closing and reopening one.
   - **Claim check.** Two checkers found 16 defects, all fixed:
     - `Column` dropped from `NullableJSONB.Null` and `NullableSlice.Null`, which no test caught;
     - an Exists-qualifier test on a schema-less table, where the table name and the formatted table
       read the same;
     - a JSON `HasKey` operand never given the other dialect's form;
     - `Eq(nil)` compared to `IsNull()` by rendered SQL only;
     - a soft-delete row duplicating `parser/schema.md`'s rows, with each value type on one dialect;
     - a multi-insert upsert shape with at most one update column, which let reversed columns pass;
     - a SQLite join row citing an `OFFSET` subtest;
     - and nine narrower wording, citation and gap-text fixes.
   - **Rows.** `runtime/sql.md` 1 → 248 rows (5660 → 5909 in all), with 24 `n/a` cells citing the
     PRD, 3 unclaimed tests and 29 open gaps, 6 of them measured by a probe with no pinning test.
     Refs were added to rows in `client.md`, `cli/gen.md`, `cli/api-schema.md` and
     `parser/schema.md`; `client.md` gained 2 §9.6a rows for the lock fix. One finding went to
     `backlog.md`: a tenanted O2O read puts the child's tenant filter in the outer `WHERE`, which
     drops a parent with no child (`gap.sql.join-tenant-outer-where` pins it).
   - **Completion sweep.** With `runtime/sql.md` set to `complete`, `req-check` passes. Flipped on
     the user's sign-off (`8a901c01`), after the report was generated.
   - **Report.** `make conformance` at `7c30ea3c`: 5909 rows, 5798 pass, 0 fail, 0 skip, 0 not
     run, 111 untested. `runtime/sql.md` has 248 pass.
   - **For the next slice (`runtime/database.md`):**
     - It holds the 130 `database` orphans, the `transactions` and `error-mapping` matrices (the
       latter with 3 empty `not-null` cells), and §18 and §19, most of whose tests need Docker.
       After it, `cache.md` (98), `hooks.md` (35), `events.md` (54) and `support.md` (31).
     - Subtest names computed by `t.Run(string(x))` cannot be cited; cite the top-level test or give
       the case a literal name.
     - An example test under `models/` is not an `example:` ref, which resolves under `tests/`.
     - `gofumpt` rewrites two single quotes in a comment into a typographic quote, so spell the
       sequence out.
     - When a fix comes from a ruling, check its effect on every `T` or caller it reaches before
       committing: the null fix first looked like a one-type change, and its `types.JSON` effect
       needed a second ruling.

   **Sixteenth slice, `runtime/database.md`, recorded 2026-11-03:**
   - **Scope.** `runtime/database.md` (`database`, `database/mock`, `database/pgx`,
     `database/stdlib`) had 3 rows. As `complete` it failed on 143 things: 9 owned sections with no
     rows (§18.1–§18.5, §19.1–§19.4), the empty `transactions` matrix, 3 empty `not-null` cells and
     130 orphan tests.
   - **Rulings (20):**
     - Four code fixes (below), and six follow-up rulings on them:
       - an `ErrTxDone` sentinel for a teardown the driver has already ended;
       - `WithTransaction` rolls back after a failed root `COMMIT`;
       - a pgx `ROLLBACK` that fails for any reason other than an ended context closes the `Tx` and
         still returns its error (`conn busy` stays loud);
       - the transport mapping decides on the call's context, so a driver's connect timeout counts
         and the caller's deadline does not;
       - of the server's rejections at connect, only capacity (`53300`, `1040`) is
         `ErrConnectionFailed`;
       - §22.1 drops "pool exhaustion", which surfaces as the caller's deadline.
     - Five PRD amendments with no code change: `Deferrable` is pgx-only and ignored through
       `stdlib` (§18.2); more than one `TxOptions` returns an error (§18.3); the retry log carries the
       callback's error (§18.6); savepoints are raw SQL on every driver (§18.6); the core package
       imports only the standard library, `database/sql` included (§19.3).
     - Both matrices redeclared with fixed rows: `transactions` with eight features (isolation,
       read-only, deferrable, timeout, savepoint, commit-rollback, reservation, teardown-never-waits);
       `error-mapping` with the six §22.2 classes.
     - Test homes: `database/stdlib` gains a PostgreSQL container opened through `lib/pq` for the
       `stdlib-postgres` cells, and the driver packages' `TestMain` runs the unit tests under
       `-short` (`67ac7fd4`); they had exited before `m.Run()`.
     - The flip to `complete`.
   - **Defects: 4, each fixed with a fail-first test:**
     - `d02c6f66`: in sync callback mode a cancelled context did not stop the remaining `OnCommit`
       callbacks. They are now skipped and `WithTransaction` returns the context's error.
     - `6d5869f8`: after `TxOptions.Timeout` expired, `WithTransaction` returned "rollback failed"
       and the `Tx` kept reporting open, so `database.Conn` handed out a dead transaction, on every
       driver. `ErrTxDone` is re-exported by the generated package, reserved as a package-scope name
       and advertised in the manifest's `error_sentinels`.
     - `e3b7e1ee`: a refused connection was not `ErrConnectionFailed` on any driver, and MySQL's
       `2002` / `2006` could never occur. Transport failures now map, at `Begin` as at every
       statement.
     - The §19.3 import of `database/sql` was ruled a PRD amendment, not a code fix.
     - `/verify` did not run, because the fixes have no FIX ID.
   - **Measurement.** Two read-only agents mapped every behavior sentence of §18–§19 and §22.2 and
     the 130 orphans. Two writers then added coverage in worktrees pinned to `e3b7e1ee` (core unit,
     and driver integration with Docker), and a third tightened the tests the claim check flagged.
     The writers ran more than 100 mutants. Equivalent mutants:
     - dropping the isolation translation on SQLite, which modernc ignores anyway;
     - the "not this driver" branches of `mapPqError` and `mapMySQLError`, whose value no caller
       reads;
     - the stdlib adapter's own `rows.Close` on drain for a single result set, which `database/sql`
       closes itself;
     - shape-based transport mapping against a handshake timeout, which lib/pq and MySQL report as a
       plain socket deadline.
   - **Timing.** One cited test depends on time to catch its mutant:
     `TestReservationSerializesTwoGoroutinesOverAnOpenResultSet` holds a result set open 500 ms. With
     the reservation disabled it failed 20 of 20 runs on every driver, and the real code passed 20 of
     20. A `stdlib` timeout-then-commit test was dropped: the timer closes the transaction context
     before it cancels the child `database/sql` derives in `BeginTx`, so a `COMMIT` in that window
     can commit (1 in about 140 runs on MySQL). A cancelled caller context drives that path
     deterministically.
   - **Claim check.** One checker found 18 defects, all fixed:
     - deadlock tests that never checked the wrapped driver error, and a pgx deadlock test that
       bypassed the adapter;
     - "unrecognized error returned as-is" checked only by `ok == false`;
     - Timeout tests that told the statement's error from a failed rollback by a format-string
       prefix;
     - the struct-mutex test missing `Query` and `QueryRow`;
     - release-on-drain tested on MySQL only, now on every driver;
     - SQLite constraint names, MySQL's key-prefix stripping and lib/pq's leaked-teardown success
       stated as requirements; now PRD gaps;
     - and eleven narrower wording, citation and gap-text fixes.
   - **Rows.** `runtime/database.md` 3 → 137 rows (5909 → 6045 in all), with 2 `n/a` cells citing
     the PRD, 10 unclaimed tests and 19 open gaps, 5 of them read from code or measured by a probe
     with no pinning test. Rows were added to `cli/gen.md` and `cli/manifest.md` for `ErrTxDone`, and
     `cli/naming.md`'s reserved-name rows gained it. One finding went to `backlog.md`: a lib/pq
     connect handshake ignores the caller's deadline unless `connect_timeout` is set.
   - **Completion sweep.** With `runtime/database.md` set to `complete`, `req-check` passes.
     Flipped on the user's sign-off (`c1d71c11`), after the report was generated.
   - **Report.** `make conformance` at `fca42310`: 6045 rows, 5932 pass, 0 fail, 0 skip, 0 not
     run, 113 untested. `runtime/database.md` has 135 pass and 2 untested: §18.6's shared commit
     timestamp and MySQL's `COMMIT`-time Galera `1213`.
   - **For the next slice (`runtime/hooks.md`):**
     - Seed `hooks.md` next (35 orphans; §21, §24, §24.2). It is the smallest, and `cache.md` (98)
       and `events.md` (54) both run through the hook chain and `OnCommit`, so their rows will cite
       its behavior. `support.md` (31) last.
     - A mutation backup keyed by basename collides when two packages share a file name
       (`pgx/errors.go` and `stdlib/errors.go`); the restore then overwrote one with the other. Name
       backups by package path.
     - A fix ruling can contradict PRD text it does not name: the timeout fix met §18.4's
       rejected-teardown rule and needed a second ruling. Read the sections next to the one ruled
       before writing code.
     - Test both exits of a teardown fix: the first `ErrTxDone` cut covered `fn` returning an
       error, and a reviewer found the `fn`-returns-nil path still broken.
     - `sqlgen generate` fails at the gqlgen step in several example directories when run from the
       workspace, and then skips writing `manifest_embed_gen.go`. Restore that file, and pass
       `--manifest-timestamp 2026-01-01T00:00:00Z`.

   **Seventeenth slice, `runtime/hooks.md`, recorded 2026-11-03:**
   - **Scope.** `runtime/hooks.md` (`hook`) had 2 rows. As `complete` it failed on 45 things: 9 owned
     sections with no rows (§21.1, §21.2, §21.4, §21.5, §21.7–§21.9, §21.11, §24.2), the empty
     `hooks` matrix and 35 orphan tests.
   - **Rulings (10):**
     - Six PRD amendments with no code change:
       - hooks run in pure registration order, so "global before entity regardless of registration
         order" is dropped (§21.3, §21.6, §21.9);
       - on a matching table, `ForMutation` / `ForQuery` return an error on an `Input` type mismatch
         (nil included) and on a non-nil `Result` mismatch after the operation ran, and a nil result
         reaches `fn` as the zero `Result`; the §21.8 / §21.9 examples are scoped with `OnMutation` /
         `OnQuery`, and `Get` / `Exists` take untyped hooks (§21.2, §21.8);
       - `QueryOp` gains `OpStream` and `OpRefresh` (§21.2);
       - `QueryContext` gains `CallOptions` (§21.2);
       - `RejectMutation`'s error names the op and the table (§21.4).
     - The `hooks` matrix redeclared as `rows=chain:mutation,query` over `order`, `modify`,
       `short-circuit`, `abort`, `panic-recovery` and `panic-handler`. Per-method firing and
       `SkipHooks` stay in `client.md`.
     - §21.9 and §21.11 made `informative`.
     - The flip to `complete`.
   - **Defects: 2, both ruled PRD amendments.**
     - The ordering promise could not be kept: `ForMutation` returns a plain `MutationHook`, so the
       chain cannot tell the two kinds apart. A fail-first test reproduced it, and was dropped with
       the ruling.
     - The PRD's own §21.9 registration made every `Update` and `HardDelete` on `products` fail
       with a type-assertion error. A probe confirmed it, along with the result-mismatch error
       arriving after the terminal ran.
   - **Measurement.** Done in-session, without agents: the package is three files, uses only the
     standard library and needs no Docker. I added 10 top-level tests and about 30 subtests, and
     deleted one duplicate (`TestBuildMutationChain_globalBeforeEntity`). 31 mutants were run, all
     killed, including a `PK int` type change that the reviewer found surviving the first field
     test. No equivalent mutants.
   - **Claim check.** The sqlgen-reviewer found 6 issues, all fixed:
     - the `any` fields of `MutationContext` / `QueryContext` were not type-pinned (now
       `pinAny(...*any)`);
     - the entity-hook rows each claimed both helpers on one helper's test;
     - `OnQuery` and `ForTable` matched only their first listed element;
     - a test still named after the retired ordering rule;
     - a cross-table claim duplicated between §21.4 and §21.7;
     - a "returns fn's result" clause no test could tell apart from `next`'s.

     Its one open question, `OpExists` carrying two input shapes, became a §21.2 sentence.
   - **Rows.** `runtime/hooks.md` 2 → 42 rows (6045 → 6085 in all), all 12 matrix cells filled, no
     `n/a`, 5 unclaimed tests and no gaps. A backlog entry records that `sqlgen lint` does not flag
     an unscoped typed helper. The 2026-10-29 entry on §21.2's missing `OpStream` / `CallOptions`
     is removed, now that the amendment settles it.
   - **Completion sweep.** With `runtime/hooks.md` set to `complete`, `req-check` passes. Flipped on
     the user's sign-off (`e0c14dff`), after the report was generated.
   - **Report.** `make conformance` at `953a3c32`: 6085 rows, 5972 pass, 0 fail, 0 skip, 0 not
     run, 113 untested. `runtime/hooks.md` has 42 pass.
   - **For the next slice (`runtime/cache.md`):**
     - Seed `cache.md` next (98 orphans), then `events.md` (54), then `support.md` (31). The cache
       and event hooks are ordinary chain members, so their rows can cite `hook.*` rows for order
       and pass-through instead of restating them.
     - `MutationContext.Tenant`, `AffectedTenants` and `QueryContext.Tenant` are §29.5 fields that
       hooks.md does not claim; check them against §29.5 there.
     - A positional struct literal pins named-type fields only. An `any` field takes any value, so
       pin it through a `*any`.
     - Ask "what does the PRD's own example do under this code?" A probe of the §21.9 registration
       found the mismatch defect that reading the helper alone did not.

   **Eighteenth slice, `runtime/cache.md`, recorded 2026-11-04:**
   - **Scope.** `runtime/cache.md` (`cache`, `cache/memory`, `cache/redis`, `cache/msgpack`,
     `metrics/otel`) had 48 rows, all citing `cmd/sqlgen/gen` template tests. As `complete` it
     failed on 101 things: §27.10 and §27.12 had no rows, the `cache-keys` matrix had none, and
     98 tests were orphans.
   - **Rulings (14):**
     - Three defects fixed (below), plus one follow-on: a key the mutation hook cannot build is
       reported through metrics and `OnErrorFunc` under `invalidate_many`, with no breaker
       feedback, rather than dropped.
     - PRD amendments with no code change:
       - §21.2 lists `Tenant` on both contexts and `AffectedTenants` on `MutationContext`;
       - §27.3: the Redis backend implements `io.Closer` and closes the client only with
         `WithOwnedClient`;
       - §27.4: `WithCache` runs outermost whatever the option order;
       - §27.9: direct `Invalidate` / `InvalidateMany` error on every tenanted table,
         tenant-in-PK included; `WithEventMetrics` is the adapter caller's to wire, since the
         facade never sees the adapter; a cache commit callback's error never reaches `Commit`;
       - §27.10: the custom-serializer error names the `cache.serializer` key;
       - §27.13: the facade's error op set (`invalidate` is the adapter's alone, and
         `invalidate_tenant_fallback` joins the list) and the breaker gauge's 1/0 convention.
     - The `cache-keys` matrix redeclared as `rows=key-shape:entity,table-pattern,tenant-table-pattern`
       over the five declared columns, with the four pattern × PK-shape cells `n/a`.
     - §27.12 is covered by one row (`cache.warm` rejected by the loader and the schema), and
       §27.10's Size and Speed columns are read as informative.
     - The flip to `complete`.
   - **Defects: 3, all fixed with fail-first tests.**
     - `5afdbf61`: under `CallbackSync`, a failing cache invalidation on `tx.OnCommit` made
       `WithTx` return the backend error for a committed write (§27.9, §29.5). The cache's commit
       callbacks now go through `afterCommit`, which retries once and returns nil; the
       materialized-view refresh had the same path.
     - `63ad679c`: the OTel `circuit_breaker` gauge never cleared the state it left, so after a
       full cycle every state read 1.
     - `eefc07be`: the read-through's in-flight re-check called `Allow` and recorded nothing.
       When it was the call that left `Open`, it held the only `HalfOpen` probe slot forever and
       the cache stayed off. A probe stranded it in 13–26 of 3,000 trips per run. The re-check
       now runs only while the breaker is `Closed`. The regression test is statistical; the
       deterministic gate is the template test banning `Allow` in the re-check.
   - **Measurement.** Done in-session: a read-only agent mapped every §27.3–§27.13 / §29.5
     sentence to tests across the runtime, the templates and the examples, and I classified the
     98 orphans and probed the PRD's examples. The §27.4 wiring ran clean in both option orders;
     the §27.9 direct calls behaved as documented, and the tenanted-table error they hit became an
     amendment. About 50 mutants were run, Docker ones one at a time. The
     one equivalent mutant: counting a `HalfOpen` failure toward the threshold, since the count
     never drops below the threshold while in `HalfOpen`.
   - **Gates.** The two fixes and the first test commit were gated together and committed as
     three commits, so `5afdbf61`, `63ad679c` and `6cf19e7b` were not gated one by one.
   - **Claim check.** An independent pass found 6 high, 10 medium and 8 low issues. All were fixed
     by narrowing wording, dropping or adding citations, or tightening tests, and it surfaced the
     third defect. Among them: `TestTx_EventBeforeCacheOnCommit` could not fail (the setup's
     event came first); the async committed-write case and `TestReadThrough_SingleflightDedup`
     could not back their claims; "each shipped backend implements `StatsReporter`" went to a gap;
     and Redis refs are `integration:`.
   - **Rows.** `runtime/cache.md` 48 → 113 rows (6085 → 6150 in all). All 15 matrix cells are
     filled or `n/a` citing the PRD; there are 4 unclaimed tests and 15 open gaps (13 new). `hooks.md`'s two field
     rows gained the tenant fields. Five findings went to `backlog.md`: a misnamed hydration
     test, out-of-order breaker callbacks, Redis glob syntax in patterns, a panic between
     `Allow` and its record, and four example tests weaker than their names.
   - **Completion sweep.** With `runtime/cache.md` set to `complete`, `req-check` passes.
     Flipped on the user's sign-off (`d01465c3`), after the report was generated.
   - **Report.** `make conformance` at `af3723ba`: 6150 rows, 6037 pass, 0 fail, 0 skip, 0 not
     run, 113 untested. `runtime/cache.md` has 113 pass.
   - **For the next slice (`runtime/events.md`):**
     - Seed `events.md` next (54 orphans), then `support.md` (31). The event hook registers on
       `OnCommit` like the cache does: check its sync-mode error path against §28 the way §27.9's
       was checked here.
     - A generated facade behavior proven only by a template string match is not proven. Cite an
       example test when one exists, and check that it can fail: two of this slice's cited
       examples could not.
     - A breaker, pool or slot that a call acquires must be released on every exit. List each
       acquire and check what releases it; reading for that found the stranded probe.
     - A fix that swallows an error must still report it. Check every error the swallowed path
       could carry, not only the backend's.
     - `TestMain` is not a test function for `req-check`; don't list it as unclaimed.

7. **Skill changes (§6).** `/design`, `/phase` and `/implement` landed with step 2. `/verify` and
   `/close-phase` change during the config pilot (step 3), once there are rows for them to read
   (§9 ruling 14). Done 2026-10-23: `/verify` reads the registry rows for a sub-item's PRD
   sections, and `/close-phase` runs the conformance gate (`make conformance` and its `REPORT.md` since step 4).

## 8. Follow-ons (not part of this design)

- **Variant suites:** tenancy (§29) and cache (§27) suites on the §4 plumbing, each with its own
  schema and harness, registered in `client.md`.
- **The schema fuzzer** (`SCHEMAFUZZ.md`, design-frozen 2026-05-15, never scheduled). Its
  metamorphic invariants (`Count == len(GetMany)`, `Insert → Get` round-trip) run over random
  schemas. The matrices test specific behaviors on known inputs, and the fuzzer tests general
  properties on many. It could reuse the harness.
- **Query counting inside transactions** (§4.3), once a driver-hook or runtime-seam approach is
  chosen.

## 9. Rulings (settled 2026-10-23)

1. **Plumbing: `replace`.** The prototype built, vetted, linted and passed on both dialects under
   `GOWORK=off`, so the `replace` layout (§4.2) is adopted and copy-and-sync is dropped.
2. **Report: committed, regenerated at `/close-phase`.** `docs/conformance/REPORT.md` lives in the
   repo. `/close-phase` and deliberate `make conformance` runs rewrite it. Other test runs don't
   (§5.5).
3. **Context cancellation: backlog, no matrix column.** The suite tests only what the PRD
   promises. The cancellation behaviors the PRD does state are registered under §18.2, §18.6 and
   §20.5. The missing general contract is logged in `docs/tracker/backlog.md` (§3.5).
4. **Hash check: fail.** `make req-check` fails when a PRD section changes under registered
   requirements, until they are reviewed and `make req-rehash` runs in the same commit as the
   PRD edit (§5.3 check 4).
5. **Placement: `cmd/sqlgen/testdata/conformance/`** (§4.2). It is out of `./...` and the
   published tree, and the Makefile loops reuse the example modules' `GOWORK=off` pattern.
6. **Migration: delete in the porting commit.** A per-dialect example test is deleted in the same
   commit whose suite port passes on every dialect. Dialect-specific example tests stay (§7 steps
   3 and 6).

Rulings 7–10 were raised by extending the design from the client suite to every module:

7. **Granularity: per module, split when large.** Four registry files. A file that passes about
   500 rows becomes a directory of per-package files, with the ownership map updated in the same
   commit (§5.1).
8. **Pilot order: config first.** The prototype already proved the suite plumbing, so the main
   untested piece is the registry tool. The config-validation pilot proves it without Docker.
   `HardDeleteMany` follows once `req-check` works (§7 step 3).
9. **`n/a` cells: a PRD citation, or the user's sign-off.** An `n/a` reason that cites a PRD
   section passes on its own. One that doesn't needs `accepted: <date>` from the user, and
   `req-check` enforces this (§5.1 part 3, §5.3 check 6).
10. **Orphan unit tests: an `unclaimed` list with reasons.** In a `complete` module, a test that
    backs no PRD behavior is listed with a one-line reason, not forced into a requirement row
    (§5.1 part 4, §5.3 check 2).

Rulings 11–14 came from the step 2 review:

11. **CI runs `req-check`.** The `repo-checks` job in `.github/workflows/lint.yml` runs
    `make req-check` alongside `refs-check` and `tracker-check`, so a PR that edits the PRD under
    a registered anchor fails CI, not just a local `make check`.
12. **§9.4b belongs to `cli.md`.** Skipping a no-PK table is generation-time behavior, and the
    `cli.md` matrices carry a `no-pk` column. Likewise §26.12 Known Limitations is owned by
    `cli.md`, not `informative`: it states generation errors and accepted values.
13. **A `complete` file has no empty matrix.** Every declared matrix needs at least one row. An
    empty one is a note while the file is seeding and a failure once it is `complete`. Seed it, or
    remove the declaration (§5.4).
14. **`/verify` and `/close-phase` change with the config pilot.** They switch to reading registry
    rows once rows exist to read (§7 steps 3 and 7).

Ruling 15 came after the client pilot:

15. **PRD gaps: a table in each registry file.** The config pilot found 6 and the `HardDeleteMany`
    pilot 1, all first logged in `backlog.md`. They now live in a `PRD gaps` table in the module
    file that shows the behavior, so they are tracked as seeding goes and sit beside the rows an
    `amend-prd` ruling will add. A gap's tests count as claimed. An open gap is a note and never
    fails the check, even in a `complete` file. A ruling, `amend-prd` or `drop-pin` with a date,
    is recorded in the table, and the entry is deleted once it is applied (§5.1 part 5).
