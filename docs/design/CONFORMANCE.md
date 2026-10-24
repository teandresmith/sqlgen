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
| `parser.md` | `parser/` and its dialect and `introspect` packages | §5, §6 |
| `cli.md` | `cmd/sqlgen/` (`config`, `gotype`, `gen`, `manifest`, `mcp`, `wrapper`, `cli`) | §4, §7, §8, §12, §13, §16, §23, §26, §30, §31, §32 |
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

### 3.3 Parser (`parser.md`)

| Matrix | Rows | Columns | Test home | PRD |
|---|---|---|---|---|
| Schema model | each §5.1 element (tables, columns, constraints, enums, sets, composite and domain types, comments, multi-schema) | dialect × source (`files`, `database`, `both`) | unit (files) + integration (database, both) | §5.1, §6.1, §6.3 |
| Relationship detection | FK shapes (single, composite, self, junction, redundant) | dialect × source | unit + integration | §5.3 |
| Soft-delete detection | column name × type | dialect | unit | §5.4 |
| Qualification and ordering | qualified / unqualified names, multi-file order, default schema | dialect | unit | §5.5, §5.6 |
| Determinism | each parse entry point | shuffled input order → identical model | unit | §5.7 |
| Introspection filtering | include / exclude rules | dialect | integration | §6.4 |

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
| Field selection and access control | column access role | surface ({struct, input, filter, sort, GraphQL, REST}) | unit + golden | §12, §32 |
| Commands | each command (`generate`, `validate`, `init`, `diff`, `lint`, `completion`, `manifest …`) | outcome ({success, config error, schema error, generation error}) × {exit code, output} | unit | §23 |
| API generation | GraphQL / REST / gRPC surface element | table kind | golden | §26 |
| Manifest and MCP | manifest field / MCP tool | {present, shape, error code} | unit + golden | §30, §31 |

### 3.5 Generated client (`client.md`)

Rows: the 29 generated methods.

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
| `documents` | Soft delete (timestamp) |
| `flags` | Soft delete (bool) |
| `audit_rows` | No PK: no PK methods generated (§9.4b) |
| `counters` | `Increment` |
| `authors` / `posts` / `post_tags` / `tags` | o2m, m2o, m2m, nested mutations |

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
	Querier *CountingQuerier // the querier Client was built on

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
   orphan check of §5.3 check 2 scans them), and its matrix declarations:

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
   its file doesn't declare (rows are an open set), or when a cell declared `n/a` is also
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
