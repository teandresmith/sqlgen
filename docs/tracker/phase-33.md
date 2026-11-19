# Phase 33: Conformance Variant Suite and Core-Suite Ports

Status: In Progress
PRD Sections: 9.9, 11.1, 11.2, 13.7, 16.4, 20.2, 22.3, 27.5, 27.7, 27.9, 28.4, 28.6, 28.11, 29.6, 29.7, 29.9

> **Design.** `docs/design/CONFORMANCE_VARIANTS.md`, approved 2026-11-18, is the source for every
> sub-item below. Its §9 lists sub-items N.0–N.8, which are 33.0–33.8 here. Its §6 behavior
> contract sets the Tests Required, and its §11 records rulings Q1–Q4:
> - Q1: one **Features** suite (tenancy, cache and events in one generated package), four targets.
> - Q2: citations read `conformance: Features/<Method>/<behavior>`; `reqcheck` maps the first path
>   segment to the suite.
> - Q3: a dialect-neutral `all` row may stay proven on one target, with a registry note naming
>   the dialect-free code (`path:line`).
> - Q4: the `client-methods` matrix gains a `view` column; writes and deletes are `n/a` (§16.4).
>
> The phase changes no PRD behavior, config key or generated API. It adds test homes, moves
> registry citations and deletes example copies once their ports pass.
>
> **Ground rules for every sub-item:**
> - **No report regression.** Each sub-item runs 33.1's report-delta check after its `make
>   conformance`: `make report-delta` against the last committed report, and
>   `make report-delta REPORT_BASE=<base>` against the phase's base. The base is `5b24d6ba` (the
>   merge base with `main`) through 33.2. From 33.3 on it is the 33.2 commit
>   (`git log -1 --format=%h --grep='^feat(conformance): a second conformance suite'`), whose
>   `REPORT.md` first lists the Features targets (user ruling, 2026-11-18). No registry row moves
>   from `pass` to another status, `skip` included (user ruling, 2026-11-18), and the totals per
>   status are recorded in the Completion Record.
> - **Fail first.** Each new suite test is seen failing before its registry row moves: against a
>   deliberately broken assertion, or a generated-code mutation where the behavior already holds,
>   as 32.8 did. Record it in the Completion Record.
> - **Every target.** A Features or core-suite test runs on postgres/pgx, postgres/stdlib, mysql
>   and sqlite. A behavior that genuinely differs uses a capability (`CONFORMANCE.md` §4.4), never
>   a branch on `Dialect`.
> - **Ruling 6.** An example copy is deleted in the commit whose port passes on every target, and
>   no sooner. Feature-combination and dialect-specific example tests stay.
> - **Findings** go through `/fix` → Findings triage. A Features test that fails on one target is a
>   finding (likely a real defect, design §3.1). Never gate it off to reach green.

## 33.0 Doc sync and relabel — dialect-neutral rows

**PRD Reference:** Sections 20.2, 22.3, 29.7, 29.9 (rows relabeled); no PRD text changes

**Status:** Complete

### Tasks

- [x] `docs/design/CONFORMANCE.md`: §8 replaces the tenancy / cache follow-on bullet with the
  Features suite and a link to `CONFORMANCE_VARIANTS.md`; §4.2 gains the variant-module paragraph
  (`features_*` modules, the `Features` root segment); §5.3 gains the suite-rooted citation rule
  (design §8).
- [x] `docs/conformance/README.md`: the `conformance: Features/Method/subtest` citation form, and
  the Applies-to rule for a dialect-neutral row (one target, a note naming the code) (design §8).
- [x] `guidelines/TESTING.md`: which test home a new test belongs in (core suite, Features suite,
  example) (design §8).
- [x] Widen the seven `sqlite`-narrowed dialect-neutral `client.md` rows to `all`:
  `tenancy-pk.mismatch`, `tenancy-pk.zero-tenant`, `tenancy-pk.update-input-omits-tenant`,
  `tenancy-hooks.filter-not-a-hook`, `tenancy-hooks.input-unchanged`,
  `error-prefix.tenant-mismatch`, `client.close-cache-publisher`.
- [x] Add a note to each of the 61 dialect-neutral rows (design §3.3) naming the dialect-free code it
  runs through, as `path:line` at HEAD. Re-derive the list from the registry at HEAD rather than
  copying it.
- [x] Add the `view` column to the `client-methods` matrix declaration in `client.md`. Set the
  five read cells to `untested`, ready for 33.7, and the rest to `n/a` citing §16.4.
- [x] `make req-check` is ok; `make conformance` totals unchanged except for the widened rows.

### Acceptance Criteria

- No registry row is narrowed below `all` without a capability or PRD quote (`CONFORMANCE.md`
  §5.1).
- Every dialect-neutral row proven on one target carries a note whose `path:line` exists at HEAD.
- The docs state where a new test goes and how a Features citation is written.

### Tests Required

- [x] `make req-check` passes with the widened rows, the notes and the new matrix column.
- [x] `make conformance`: the seven widened rows still `pass`, and no other row changes status.

### Completion Record

Completed 2026-11-18 on `feat/conformance-variants`. Docs only, no code.

- **Files:** `docs/conformance/{README.md,client.md}`, `docs/conformance/cli/{config-features,gen}.md`,
  `docs/conformance/runtime/{cache,events,support}.md`, `docs/design/CONFORMANCE.md` (§4.2 variant
  modules, §5.3 suite-rooted citations, §8), `docs/design/CONFORMANCE_VARIANTS.md` (§6.1
  `relabel.neutral` Test cell), `guidelines/TESTING.md` (where a new generated-client test goes).
- **Rulings (2026-11-18):** the note is a prose list under each requirements table ("Proven on one
  target …", one `id`: `path:line` bullet per row), because the table format has no note column.
  No `reqcheck` test is added; §6.1 now names `make req-check`.
- **Neutral set re-derived at HEAD:** 117 rows cite only `cache` / `events` / `tenancy*` example
  tests and no `conformance:` test. Removing the 46 §3.2 rows and the 7 §3.4 rows leaves 64:
  config-features 18, gen 7, cache 18, events 4, support 5, client 12. This matched design §3.3.
- **Moved to carrier-dependent (user ruling, 2026-11-18, from the auto-review):**
  `gen.events.empty-affected-publishes-nothing` and `edge.noop-skips-view-invalidation` (their
  guards read `AffectedPKs`, which MySQL fills by pre-read), and
  `gen.tenancy.filter-comparator-follows-type` (a different comparator arm per tenant column
  type, only SQLite tested). Their notes are removed. Design §3.2 is now 49 and §3.3 is 61
  (gen 5, client 11). They are added to 33.4, 33.6 and 33.5.
- **Widened to `all`:** the seven listed `client.md` rows.
- **`view` column:** five `untested` §16.4 rows (`view.get`, `.get-many`, `.count`, `.paginate`,
  `.connection`) and 23 `n/a` cells citing §16.4.
- **Auto-review:** 4 wrong citations and 5 doc nits, all fixed inline (metadata, CachedTenant,
  keyed-load, comparator arm, Update prefix site, two ranges, the `features_<target>` wording,
  a re-wrap).
- **Tests:** `make check` green; `make req-check` ok (6342 requirements).
- **`make conformance`:** 6257 pass, 0 fail, 0 skip, 0 not run, 85 untested. No existing row
  changed status, and the seven widened rows still `pass`. Against the 2026-11-17 report
  (6256 / 80 untested): +5 untested are the new view rows. +1 pass is
  `config.overrides.types.nullable-import-other-package`, a row added after that report.

## 33.1 Regression guard — report delta

**PRD Reference:** none (repository tooling, `CONFORMANCE.md` §5.5)

**Status:** Complete

### Tasks

- [x] A report-delta check that compares two `REPORT.md` files (or two result sets): the totals
  per status, and every row whose status changed, failing when a row leaves `pass`.
- [x] Wire it as a make target the later sub-items run against the report at the phase's base
  commit.

### Acceptance Criteria

- The check passes on today's report against itself, and fails on a report where one row moved
  from `pass` to `untested`.

### Tests Required

- [x] `unit: cmd/sqlgen/internal/reqcheck`: the delta check passes on current code before any
  change (today's report against itself), and reports a row moved from `pass` to `untested`.

### Completion Record

Completed 2026-11-18 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/internal/reqcheck/{delta.go,delta_test.go}` (new),
  `cmd/sqlgen/internal/cmd/reqcheck/main.go` (`-delta`), `Makefile` (`report-delta`),
  `docs/design/CONFORMANCE.md` §5.5, `docs/design/CONFORMANCE_VARIANTS.md` §6.2.
- **Shape:** `ParseReport` reads a committed `REPORT.md` back to one status per requirement row:
  the most severe cell, raised by the row's line in "Failing and not-run rows" (a row whose
  conformance tests apply to no target shows only `—` cells). Each file's derived totals must
  equal its Modules line, so a rendering change fails loudly instead of misreading rows.
  `DiffReports` lists rows that changed, appeared or disappeared; a row that leaves `pass`,
  including one that disappears, is a regression. PRD-gap pinning tests are not compared.
- **Make target:** `make report-delta [REPORT_BASE=<rev>]` pipes `git show
  <rev>:docs/conformance/REPORT.md` into `reqcheck -delta -` and compares it with the working-tree
  report. `REPORT_BASE` defaults to `HEAD`. A bad rev fails (empty report, no Requirements section).
- **Contract runs:** `TestParseReportRoundTrip`, `TestReportDeltaCommittedReport`,
  `TestParseReportErrors` and the `unchanged` case of `TestDiffReports` passed with `DiffReports`
  stubbed to return totals only. `TestReportDeltaPassToUntested` and the six change cases of
  `TestDiffReports` failed there (no change reported), then passed with the implementation.
- **Live runs:** `make report-delta` (HEAD): 6257 pass, 85 untested, 0 changed, ok.
  `make report-delta REPORT_BASE=5b24d6ba`: 6256 → 6257 pass, 80 → 85 untested, 6 rows added
  (`config.overrides.types.nullable-import-other-package` and the five `view.*` rows), none left
  `pass`; this matches 33.0's record. A scratch report with one `cli/access-control.md` row moved
  to `untested` fails with that row marked.
- **Auto-review:** fail-first confirmed independently; parser audited against the writer with no
  misread found. Fixed inline: a missing space in `.PHONY` (`report-deltatest`) and a ground-rule
  re-wrap.
- **Rulings (user, 2026-11-18):** `pass` → `skip` stays a regression, even for a capability skip.
  The phase base moves to the post-33.2 report rather than `ParseReport` reading two layouts.
  Deleted or renamed rows have no allowlist; they fail and need a base move.
- **Verify:** independent reviewer confirmed fail-first and found no issues. Three doc wordings
  were fixed inline, and a `pass to skip` case now pins the strict ruling.
- **Tests:** `make check` green; `make check-examples` green (the Makefile change triggers it).

## 33.2 `reqcheck`, Makefile and CI — a second suite

**PRD Reference:** none (repository tooling, `CONFORMANCE.md` §4.2, §5.3, §5.5)

**Status:** Complete

### Tasks

- [x] `reqcheck`: suites become a table, the core suite (`conformance/suite`) and
  `Features` (`conformance/features_suite`). The citation and orphan checks walk both trees
  (`check.go:436,608-659`).
- [x] `readTarget` records which suite package a target's root test imports. `joinRow` sends a
  citation whose first segment names a suite to that suite's targets only, and any other
  `conformance:` citation to the core targets.
- [x] Makefile: `CONFORMANCE_DIALECT_DIRS` and the conformance loops skip `features_suite` as they
  skip `suite`.
- [x] CI (`.github/workflows/examples.yml:44-45`): the target discovery drops `features_suite`.
- [x] Before the layout change lands, run `make report-delta REPORT_BASE=5b24d6ba` one last time.
  A writer change lands in one commit with the matching `ParseReport` change and a regenerated
  `REPORT.md` (`make conformance`, Docker), or `make check` goes red on the committed report.
  Record that commit in the ground rules as the phase's report-delta base for 33.3 on.
- [x] A stub `features_suite` with one subtest and one stub target, enough to fail first; 33.3
  replaces it.

### Acceptance Criteria

- Core citations resolve and join exactly as before (the existing `reqcheck` tests are unchanged
  and pass).
- A `conformance: Features/…` citation is checked against the Features tree and joined only with
  Features targets.
- The report lists Features targets beside the core targets, each with its database version.

### Tests Required

- [x] Regression guards still pass: `unit: cmd/sqlgen/internal/reqcheck: TestCheck`,
  `TestBuildReportJoin`, `TestBuildReportTargets`.
- [x] `unit: cmd/sqlgen/internal/reqcheck: TestBuildReportJoin/a suite-rooted citation joins only
  that suite's targets` fails before the change, passes after.
- [x] `unit: cmd/sqlgen/internal/reqcheck: TestCheck/an unclaimed Features subtest is an orphan`
  fails before the change, passes after.
- [x] `unit: cmd/sqlgen/internal/reqcheck: TestBuildReportTargets/variant modules are targets of
  their suite` fails before the change, passes after.
- [x] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

Completed 2026-11-18 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/internal/reqcheck/{testtree.go,check.go,report.go}` and their tests
  (`check_test.go`, `report_test.go`); `Makefile` (`CONFORMANCE_DIALECT_DIRS`);
  `.github/workflows/examples.yml` (dialect discovery); new modules
  `cmd/sqlgen/testdata/conformance/features_suite/` and `features_sqlite/` (stub);
  `docs/design/CONFORMANCE.md` §5.5; `docs/conformance/REPORT.md` (regenerated).
- **Shape:** `reqcheck.Suites` lists the core suite and `{Name: "Features", Dir: features_suite}`.
  `ParseTestRef` sends a `conformance:` path whose first segment names a variant suite to that
  suite's directory, so the citation and orphan checks resolve it in that tree. In a variant
  suite a behavior is two levels under the root segment (`Features/<area>/<behavior>`), and every
  `Run` subtest must be the root segment: one beside it would be cited without it and routed to
  the core suite. `readTarget` sets `Target.Suite` from the suite package the module's test
  imports, and fails the report when it imports none (user ruling, 2026-11-18). `joinRow` joins a
  citation only with its suite's targets. Variant targets sort after the core ones and are
  labeled `Features sqlite/stdlib`.
- **Stub:** `features_suite` runs one subtest, `Features/harness` (a `Count` on a one-table
  schema), which is not a behavior and needs no claim. `features_sqlite` is its one target, with a
  committed `models/` (`…/conformance/features/models`). 33.3 replaces both.
- **Contract runs:** with only the `Target.Suite` field added, the existing `TestCheck`,
  `TestBuildReportJoin` and `TestBuildReportTargets` cases passed. The six new cases failed
  because `Features/…` resolved against the core tree (`Run has no subtest "Features"`): four
  `TestCheck` cases (resolves, orphan, looked up in the Features tree, subtest outside the root
  segment), `TestBuildReportJoin/a suite-rooted citation joins only that suite's targets` and
  `TestBuildReportTargets/variant modules are targets of their suite`. All pass after.
- **Report delta:** `make report-delta REPORT_BASE=5b24d6ba` before the change: ok, 6 rows added,
  none left `pass`. `ParseReport` needed no change: it takes the most severe of whatever cells a
  row has, so the extra target column parses in both layouts, and the old-base delta still passes
  on the new report.
- **`make conformance`:** 6342 rows, 6257 pass, 0 fail, 0 skip, 0 not run, 85 untested.
  `Features sqlite/stdlib` is listed with SQLite 3.51.3. `make report-delta` against HEAD: 0 rows
  changed.
- **Tests:** `make check` green; `make check-examples` green, with `features_sqlite` linted,
  fresh and passing (the Makefile and CI changes trigger it). Lint asked for two test-helper
  changes: `sectionHash` lost its always-`"9.3"` key (now `deleteSectionHash`), and `reportRepo`
  now builds from `reportFiles` so `featuresRepo` can extend the same file map.
- **Auto-review:** no issues; fail-first confirmed independently (dropping the join's suite
  filter fails the join subtest both ways). One line range fixed inline (`check.go:436,608-659`).
  Its open question, whether a module importing no suite counts as core, was ruled an error
  (user, 2026-11-18): `TestBuildReportTargets/a module that imports no suite is an error` failed
  first (no error), then passed. The `dialectTest` fixture now imports the core suite, and a
  rejoin of the last `make conformance` results over the real modules gives the same totals.
- **Verify:** independent reviewer confirmed fail-first on the no-suite error and found no issues.
  Fixed inline: the regression-guard test name (`TestConformance` → `TestCheck`, here and in the
  design's §6.2) and three over-long lines. Open question: a module importing two suites takes the
  last one.

## 33.3 Features modules — schema, config, harness

**PRD Reference:** Sections 27, 28, 29 (configuration under test); `CONFORMANCE.md` §4

**Status:** Complete

### Tasks

- [x] `features_suite/`: `Run(t, *Harness)` wrapping `t.Run("Features", …)`; a harness with
  `Client`, `Querier`, `Caps`, `Reset`, a `memorybus`, a memory cache and a counting
  `TenantResolver`, and constructors for a client with or without cache and publisher.
- [x] Features schema in each dialect's DDL, one table per carrier shape (design §5.1):
  - tenant outside the PK on a database-generated key
  - tenant inside a composite PK
  - a tenanted soft delete
  - a tenanted O2M child and a tenant junction
  - an untenanted cached table
  - a cached view
- [x] `sqlgen.yml` per target with `events`, `cache` and `tenancy` enabled. Generate and commit
  `models/` declaring `…/conformance/features/models`. Generation on all four targets is the
  probe design §7 did not run: a failure is a finding.
- [x] `features_postgres`, `features_postgres_stdlib`, `features_mysql`, `features_sqlite`: a
  `TestConformance` per target (containers as the core targets), capabilities set per target.
- [x] Replace 33.2's stub.
- [x] `reqcheck` `readTarget`: a module whose tests import more than one conformance suite fails
  the report, naming the module and the suites, as importing none does (user ruling,
  2026-11-18; today the last import wins).

### Acceptance Criteria

- `make conformance-fresh` covers the Features `models/`, and fails when they are stale.
- The four Features targets build, vet, lint and run an empty Features tree under `-race`.

### Tests Required

- [x] A stale Features `models/` fails `make conformance-fresh` (shown once by editing a
  generated file), and the fresh tree passes.
- [x] `unit: cmd/sqlgen/internal/reqcheck: TestBuildReportTargets/a module that imports two suites
  is an error` fails before the change (the last import wins), passes after.
- [x] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

Completed 2026-11-18 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/testdata/conformance/features_suite/{harness.go,querier.go,go.mod,go.sum}`;
  `features_{postgres,postgres_stdlib,mysql,sqlite}/` (`conformance_test.go`, `go.mod`, `go.sum`,
  `sqlgen.yml`, committed `models/` with a handwritten `conformance.go`; `schema.sql` and
  `views/active_tasks.sql` except in `features_postgres_stdlib`, which reads the pgx module's);
  `cmd/sqlgen/internal/reqcheck/{report.go,report_test.go}`; `docs/conformance/REPORT.md`
  (regenerated); `docs/design/CONFORMANCE_VARIANTS.md` §6.1.
- **Schema:** `notes` (tenant outside the PK, generated key, a unique `(workspace_id, slug)` for
  Upsert's update branch and a `views` counter for `Increment`), `memberships` (tenant inside a
  composite PK, a `seats` counter for `Increment`), `tasks` (tenanted soft delete), `boards` with an O2M child `cards` and a tenant
  junction `board_tags` through `tags` (composite tenant FKs into the tenanted parent), the
  shared parent `templates` with an O2M child `template_notes` and a junction `template_tags`
  (children take `workspace_id` from the untenanted `owner_workspace`, as in `tenancy_postgres`),
  `bulletins` (`tenancy.required: false`), `regions` (untenanted) and the cached, tenanted view
  `active_tasks` (`invalidate_on: [tasks]`). 12 tables, 1 view.
- **Tenant type (user rulings, 2026-11-18):** `uuid.UUID` on PostgreSQL (`UUID`) and SQLite
  (`BLOB` overridden), a `CHAR(36)` string on MySQL, where a `uuid.UUID` tenant type fails the
  PRD §29.2.4 SQL-type check (all 10 tenanted tables and the view refused, measured).
  `models/conformance.go` aliases it per dialect as `models.Tenant` with `NewTenant()`, as the
  core models alias the soft-delete type. The tenant column's filter comparator follows the type
  (`comparator.ID`, or `comparator.String` for MySQL's tenant outside the primary key), so a suite test that
  filters on it directly needs a per-dialect alias too; that is 33.5's
  `gen.tenancy.filter-comparator-follows-type`. The rest of the generated API (`go doc -all`, 3474
  lines) matches across the four targets apart from the PostgreSQL `Table*` values, the aliased
  timestamp and soft-delete types, and SQLite's `TemplateFilter.OwnerWorkspace` comparator
  (backlog).
- **Generation probe:** all four targets generate with events, cache, tenancy and nested
  mutations on. Finding: none for the generator; the MySQL type check above is PRD behavior.
  Adjacent: SQLite gives the overridden `owner_workspace` a `*comparator.String` filter
  (backlog, 2026-11-18).
- **Harness:** `Harness{Dialect, Driver, Database, Querier, Base, Caps, Reset}` with
  `Require`/`Lacks` on `RowLocks` and `CaseInsensitiveText`. `NewEnv(t, WithCache(),
  WithPublisher())` builds a client on `Base` with a fresh `CountingResolver` (the tenant comes
  from `WithTenant(ctx, …)`), a memory-backed `Cache` and a `memorybus`; `Client(t)` is the plain
  one. `CountingQuerier` is a copy of the core suite's: importing the core suite would give each
  Features target two suite imports, which the report rejects. `Run` checks the wiring first
  (every table and the view count zero for one tenant after `Reset`) and then runs an empty
  `Features` tree. The postgres pgx target routes through a batching querier, as the core one
  does. The MySQL target splits its DDL on `;`, so no comment in it may contain one.
- **Dependencies:** each target's `go.mod` is seeded from its core counterpart before `go mod
  tidy`, so indirect versions match the core modules (an unseeded tidy picked newer `genproto`).
- **Two-suite import:** `TestBuildReportTargets/a module that imports two suites is an error`
  failed first (nil error, the last import won), then passed. `readTarget` collects the distinct
  suites a module's tests import and fails on zero or more than one.
- **Fresh gate:** appending a line to `features_mysql/models/client_gen.go` failed `make
  conformance-fresh` (`models/ is stale`); restored, it passes.
- **Runs:** `make lint-conformance` 0 issues on all ten modules. Each Features target passes
  under `-race` (SQLite 3.51.3, PostgreSQL 16.16 twice, MySQL 8.0.46). `make conformance`: 6257
  pass, 0 fail, 0 skip, 0 not run, 85 untested, the four Features targets listed with their
  versions. `make report-delta` against `a46e9e95` and against HEAD: 0 rows changed.
- **Tests:** `make check` green. No path in the diff is on the `check-examples` trigger list;
  `make conformance` ran every example and conformance module anyway, all passing.
- **Auto-review:** fail-first on the two-suite error confirmed. It found four carrier shapes
  33.4–33.5 would need (counter, natural unique key, shared-parent nested tables, a
  `required: false` table), added here by user ruling so `models/` regenerate once. Fixed
  inline: the `Run` comment, this record's FK wording, and the two-suite test now asserts both
  suite directories. `cache.invalidate.mutations-by-key` stays on the `cache` example (ruling;
  design §3.2 and 33.6 updated).
- **Verify:** fail-first on the two-suite error confirmed again. It found `memberships` had no
  counter, so the `Increment` halves of `tenancy-pk.match-proceeds` and `.skip-tenancy` had no
  carrier: `seats` added and `models/` regenerated. Fixed inline: the tenanted-table count, the
  comparator and API-parity wording here, design §3.2's count (48) and §5.1's shape list.

## 33.4 Events — per operation and input

**PRD Reference:** Sections 28.4, 28.6

**Status:** Complete

### Tasks

- [x] `Features/Events/<Method>` trees for the 16 operations §28.4 lists, plus batch, fire-and-forget
  PK and the failed-mutation case.
- [x] Input cases for each §28.6 row.
- [x] Cover the MySQL paths design §3.1 lists as untested: `Create` via `LastInsertId`, `Upsert`
  with its tenant read-back, `Increment`, `HardDeleteWhere`, and the `rowMatches`
  matched-but-unchanged row.
- [x] A no-op write on every target publishes nothing (`gen.events.empty-affected-publishes-nothing`),
  the MySQL pre-read `*Many` / `*Where` paths included.
- [x] Move each row's citation to its Features test and widen it to `all`.

### Acceptance Criteria

- Every §28.4 and §28.6 carrier-dependent row (design §3.2) passes on all four targets.

### Tests Required

- [x] `conformance: Features/Events/<Method>/publishes one event per affected row`, one per §28.4
  operation, on all four targets: fails before the change (a broken assertion or a generated-code
  mutation), passes after.
- [x] `conformance: Features/Events/<Method>/the event carries the documented input`, per §28.6
  row, on all four targets: fails before, passes after.
- [x] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

Completed 2026-11-19 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/testdata/conformance/features_suite/{events.go,recorder.go,harness.go}`;
  `docs/conformance/runtime/events.md`, `docs/conformance/cli/gen.md`,
  `docs/conformance/REPORT.md` (regenerated); `docs/design/CONFORMANCE_VARIANTS.md` §6.1
  (`events.per-op`, `events.input` now `passing`).
- **Harness:** `Recorder`, an `event.Publisher` that records each `PublishBatch` call and each
  `Publish`, and two `NewEnv` options, `WithRecorder()` and `WithMutationHook(h)`.
  `WithRecorder` with `WithPublisher` fails the test.
- **Tree (`Features/Events/…`, 37 leaves):** for each of the 16 §28.4 methods, `publishes one
  event per affected row` and `the event carries the documented input`. The writes run on
  `notes` (database-generated key, unique `(workspace_id, slug)` for Upsert's update branch,
  `views` for Increment) and `tasks` (soft delete). `*Many` / `*Where` writes list or match
  two of three rows, and `UpdateMany` includes a missing key, so its input leaf checks the
  written items 0 and 2. Five more leaves: `Update/an update that leaves the row unchanged
  publishes its event` (MySQL's `rowMatches`), `Create/a fire-and-forget create carries the
  generated key` (MySQL `LastInsertId`), `a batch mutation hands every event to one PublishBatch
  call`, `a failed mutation publishes nothing`, and `a write that affects no row publishes
  nothing` (15 writes: missing keys, zero-match `*Where`, `SoftDeleteWhere` over a soft-deleted
  row and `RestoreWhere` over a live row; no `PublishBatch` call).
- **Test correction:** the first draft expected by-key `SoftDelete` of a soft-deleted row and
  `Restore` of a live row to publish nothing. PRD §9.8 (exclude_deleted exceptions) says by-key
  methods address their keys directly, so the event is correct; those cases became the
  `*Where` ones above.
- **Registry:** 27 rows gain their Features citation ahead of the example citations, which stay
  until 33.8: the 16 `event.op.*`, `event.batch.one-publish-batch`, `event.pk.fire-and-forget`,
  `event.hook.failed-mutation-no-event`, the 7 `event.input.*` and
  `gen.events.empty-affected-publishes-nothing`. All were already `all`. Each now shows `pass`
  on the four Features targets.
- **Fail first (generated-code mutations, restored with `git checkout`):** on `features_sqlite`,
  stringifying each event's `PK` failed the 16 `publishes …` leaves plus fire-and-forget,
  unchanged-update and the `CreateMany` / `UpdateMany` input leaves (PK alignment).
  Stringifying `Input` failed all 16 input leaves. Dropping the empty-`AffectedPKs` guard
  failed only the no-row leaf. One `PublishBatch` per event failed only the batch leaf.
  Publishing after a failed `next` failed only the failed-mutation leaf. On `features_mysql`,
  `rowMatches` returning false failed only the unchanged-update leaf. Emptying the pre-read
  `AffectedPKs` failed the 8 `UpdateWhere` / `SoftDeleteWhere` / `RestoreWhere` /
  `HardDeleteWhere` leaves.
- **Runs:** each Features target passes under `-race` (SQLite 3.51.3, PostgreSQL 16.16 pgx and
  stdlib, MySQL 8.0.46). `make lint-conformance` 0 issues. `make req-check` ok (6342
  requirements). `make conformance`: 6257 pass, 0 fail, 0 skip, 0 not run, 85 untested.
  `make report-delta` against HEAD and against `a46e9e95`: 0 rows changed, none left `pass`.
- **Tests:** `make check` green. No path in the diff is on the `check-examples` trigger list;
  `make conformance` ran every example anyway, all passing.
- **Auto-review:** 0 high-confidence issues. Its own fail-first check (the empty-`AffectedPKs`
  guard removed) confirmed that only the no-row leaf fails. Fixed inline: the write count here
  (15) and the STATUS overview row. The event's tenant stamp on MySQL's Upsert update branch is
  checked in 33.5 (`tenant-event.*`).

## 33.5 Tenancy carriers — stamps, tenant in the PK, nested tenancy

**PRD Reference:** Sections 9.9, 29.6, 29.7, 29.9

**Status:** Complete

### Tasks

- [x] `Features/Tenancy/<Method>` stamp cases for each §29.6 row: row-derived, from the PK,
  `SkipTenancy`, `required: false` with a zero tenant, an unmatched key, `%v` formatting.
- [x] Tenant-in-PK cases: a matching PK tenant proceeds; `SkipTenancy` uses the caller's
  `pk.Tenant`; `MutationContext.AffectedPKs` holds full PK values, tenant included.
- [x] Nested cases on the tenanted O2M child and the tenant junction: unlink with a mismatched
  parent tenant; an M2M create whose target differs on a shared junction column.
- [x] Filtering on the tenant column with the target's tenant Go type compiles and selects the
  caller's rows on every target (`gen.tenancy.filter-comparator-follows-type`).
- [x] Move each row's citation and widen it to `all`.

### Acceptance Criteria

- Every §29.6, §29.7 and §29.9 carrier-dependent row, and the two nested-tenancy §9.9 rows, pass on
  all four targets.

### Tests Required

- [x] `conformance: Features/Tenancy/<Method>/each event is stamped with its row's tenant`, per
  §29.6 row, on all four targets: fails before, passes after.
- [x] `conformance: Features/Tenancy/<Method>/a matching PK tenant proceeds` and its `SkipTenancy`
  and `AffectedPKs` siblings (§29.7, §29.9), on all four targets: fail before, pass after.
- [x] `conformance: Features/Tenancy/UpdateWithRelated/<case>` for the two §9.9 rows, on all four
  targets: fails before, passes after.
- [x] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

Completed 2026-11-19 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/testdata/conformance/features_suite/{tenancy.go,harness.go}`;
  `features_{postgres,postgres_stdlib,mysql,sqlite}/models/conformance.go` (`TenantEq`);
  `docs/conformance/{client.md,runtime/events.md,cli/gen.md}`, `docs/conformance/REPORT.md`
  (regenerated); `docs/design/CONFORMANCE_VARIANTS.md` §6.1 (`events.tenant-stamp`,
  `tenancy.pk-carriers`, `tenancy.nested` now `passing`).
- **Tree (`Features/Tenancy/…`, 45 leaves):** for each of the 16 §28.4 methods on `notes` and
  `tasks`, `each event is stamped with its row's tenant`. It runs the write twice: under the
  resolved tenant, then under `SkipTenancy` over rows of two other tenants, with the resolver still
  answering the first. Each event's `Metadata["tenant"]` must equal its own row's tenant formatted
  with `%v`. On `memberships` (tenant in the PK), `Get`, `Update`, `HardDelete`, `Increment` and
  `Exists` each have `a matching PK tenant proceeds` and `SkipTenancy uses the caller's PK tenant`.
  The latter runs under another tenant's context and requires zero resolver calls. Ten writes
  have `AffectedPKs hold the full PK, tenant included`: `Create`, `CreateMany`, `Update`,
  `UpdateMany`, `UpdateWhere`, `Upsert`, `Increment`, `HardDelete`, `HardDeleteMany` and
  `HardDeleteWhere`. Three more leaves: `a tenant-in-PK event is stamped from its PK` (ten
  `memberships` writes), `a required-false write under the zero tenant stamps its row's tenant`
  (`bulletins`, seven writes) and `a listed key that matched no row publishes no event` (six
  `*Many` writes on notes, tasks and memberships). Nested: `UpdateWithRelated/` holds `an unlink
  under a parent of another tenant is ErrAlreadyRelated` (O2M and M2M disconnect, a disconnect
  naming no row, and clear), `an unlink on the parent's tenant writes` (resolved, `SkipTenancy`,
  explicit tenant), `a disconnect miss on the parent's tenant is a no-op` and `an M2M create
  whose target differs on a shared junction column is ErrAlreadyRelated`. `UpsertWithRelated/`
  holds `an unlink under a parent of another tenant rolls back the insert`. `GetMany/a filter on
  the tenant column selects that tenant's rows` filters through `models.TenantEq`.
- **Seeding:** the nested cases seed through the client, not raw SQL. `templates` is untenanted,
  so a template owned by one tenant takes its note, tag and link from that tenant's resolver.
  This keeps the tests free of per-dialect placeholders.
- **`TenantEq`:** each target's handwritten `models/conformance.go` declares the comparator the
  tenant column outside the PK filters through: `comparator.ID` for the `uuid.UUID` tenant on
  PostgreSQL and SQLite, and `comparator.String` for MySQL's string tenant. The suite assigns it to
  `NoteFilter.WorkspaceID`, so a generator that picked another comparator fails to compile.
  Shown on MySQL by declaring it `*comparator.ID`: `cannot use models.TenantEq(theirs) (value of
  type *comparator.ID) as *comparator.String value`.
- **Registry:** 13 rows gain their Features citations ahead of the example citations, which stay
  until 33.8: the seven `tenant-event.*` §29.6 rows, `tenancy-pk.match-proceeds`,
  `tenancy-pk.skip-tenancy`, `tenancy-hooks.affected-pks-tenant` (these three widened from
  `sqlite` to `all`), `nested.unlink-tenant-mismatch`, `nested.m2m-create-shared-column-mismatch`
  and `gen.tenancy.filter-comparator-follows-type`. Each shows `pass` on the four Features
  targets. No `client-methods × composite-pk` cell is set: the core `*.composite-pk` rows already
  claim them (design §6.1 updated).
- **Fail first (generated-code mutations, restored with `git checkout`):** all on
  `features_sqlite` except the last. Stamping from `mc.Tenant` failed the 16 stamp leaves and the
  required-false and listed-key leaves. Formatting with `%q` failed those plus the PK-stamp leaf.
  Stamping `memberships` from `Member` failed the PK-stamp and listed-key leaves. Counting an
  unmatched `UpdateMany` item as written failed only the listed-key leaf. Inverting the
  `memberships` verify-match failed the five match leaves, and further leaves whose paths call
  those methods. Ignoring `SkipTenancy` in those five methods failed the five `SkipTenancy` leaves
  and the PK-stamp leaf (its writes run under `SkipTenancy`). Zeroing the tenant in `memberships`
  `AffectedPKs` failed the ten `AffectedPKs` leaves and the two PK-stamp readers. Turning off the
  shared-parent unlink comparison failed the two mismatch leaves (`UpdateWithRelated`,
  `UpsertWithRelated`). Making it refuse agreeing tenants failed the `writes` and `no-op` leaves.
  Turning off the created-row comparison failed only the M2M create leaf. Dropping `NoteFilter`'s
  tenant condition failed only the filter leaf. On `features_mysql`, a wrong pre-read tenant in
  `notes` and `tasks` `captureAffectedTenants` failed the ten stamp leaves whose writes use it and
  the listed-key leaf.
- **Findings:** none. Every case passed on all four targets at first run.
- **Runs:** each Features target passes under `-race` (SQLite 3.51.3, PostgreSQL 16.16 pgx and
  stdlib, MySQL 8.0.46). `make lint-conformance` 0 issues. `make req-check` ok (6342
  requirements). `make conformance`: 6257 pass, 0 fail, 0 skip, 0 not run, 85 untested.
  `make report-delta` against HEAD and against `a46e9e95`: 0 rows changed, none left `pass`.
- **Tests:** `make check` green. No path in the diff is on the `check-examples` trigger list;
  `make conformance` ran every example anyway, all passing.
- **Auto-review:** 0 high-confidence issues; the ports keep every check of the example tests
  they replace. Its own fail-first check (both shared-parent unlink comparisons off) failed only
  the two mismatch leaves. Fixed inline: STATUS said four nested cases (five), and
  `tenant-event.stamp-percent-v` now also cites the listed-key leaf, which the `%q` mutation
  fails. The `Increment` leaves now seed 2 seats and expect 5, so a write that set the column
  would fail. Kept: the six unlink-mismatch cases run in one leaf's loop, not as subtests, so the
  registry cites one leaf; the first failing case stops the leaf.

## 33.6 Cache — invalidation and event-driven receive

**PRD Reference:** Sections 27.5, 27.7, 27.9, 28.11, 29.5

**Status:** Complete

### Tasks

- [x] `Features/Cache/<Method>` cases for each §27.5 / §27.7 / §27.9 / §29.5 carrier-dependent
  row: key-based invalidation for single-row, `*Many` and `*Where` writes; write-through on
  `Create` / `CreateMany`; an unbuildable key reported once; per-row tenanted eviction.
- [x] Event-driven receive cases (§28.11): a tenant-stamped event, a tenant-in-PK event with no
  stamp, a tenant-less event's fallback. Assert the key each target builds, PostgreSQL's
  `public` segment included (`context_cache.go:253-261`).
- [x] A no-op write invalidates no view pattern on every target
  (`edge.noop-skips-view-invalidation`), the MySQL pre-read paths included.
- [x] Move each row's citation and widen it to `all`. Fill the `cache-keys` matrix cells the
  design §6.3 names.

### Acceptance Criteria

- Every cache carrier-dependent row (design §3.2) passes on all four targets.
  `cache.invalidate.mutations-by-key` is not among them: it stays on the `cache` example (user
  ruling, 2026-11-18).

### Tests Required

- [x] `conformance: Features/Cache/<Method>/evicts each affected row's key`, per row, on all four
  targets: fails before, passes after.
- [x] `conformance: Features/Cache/event-driven invalidation/<case>`, per §28.11 row, on all four
  targets: fails before, passes after.
- [x] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

Completed 2026-11-19 on `feat/conformance-variants`.

- **Files:** `cmd/sqlgen/testdata/conformance/features_suite/{cache.go,cacherecord.go,harness.go}`;
  `features_{postgres,postgres_stdlib}/conformance_test.go` (`KeySchema: "public"`);
  `docs/conformance/{client.md,runtime/cache.md,runtime/events.md}`, `docs/conformance/REPORT.md`
  (regenerated); `docs/design/CONFORMANCE_VARIANTS.md` §6.1 (`cache.invalidation`, `cache.receive`
  now `passing`) and §6.3.
- **Harness:** `RecordingBackend` records each backend call over the memory backend;
  `CacheErrors` records `OnErrorFunc` reports (installed by `WithCache`); `HandlerResults` wraps an
  `InvalidationSource` and records what the cache's handler returns. New options
  `WithCacheOptions`, `WithEventPublisher` and `WithClientOption`; more than one publisher option
  fails the test. `Capabilities.KeySchema` is the key's schema segment (`public` on PostgreSQL).
- **Tree (`Features/Cache/…`, 23 leaves):** `Create` and `CreateMany` `writes the created row
  through under its own key`. 15 writes (`Update` through `RestoreWhere`, `UpsertMany` included)
  `evicts each affected row's key` on `notes` and `tasks`. Each runs under the resolved tenant and
  under `SkipTenancy` over rows of two other tenants; the evicted keys must equal the rows' own
  tenant keys in one backend `InvalidateMany` per write, nothing written through, no pattern but the `active_tasks` view
  for `tasks`, no `OnErrorFunc` report, and a bystander row still a hit. Every written, read or
  evicted key is checked against the §29.5 grammar with `KeySchema`. Two `an unbuildable key is
  reported once …` leaves (outside a transaction, inside a sync one) on the untenanted `regions`:
  one `invalidate_many` report, no backend invalidation, the write returns nil. `a write that affects no
  row invalidates no key and no view pattern`: 14 `tasks` writes, no backend invalidation.
  `event-driven invalidation/` holds the three §28.11 cases, with a writer that has no cache and a
  receiver fed by `FromEventSubscriber`: the tenant-stamped event (written under `SkipTenancy`)
  evicts that key only; a tenant-in-PK `memberships` event stripped of its stamp evicts that PK's
  key only; a tenant-less `notes` event clears `sqlgen:{schema.}notes:*`, reports one
  `invalidate_tenant_fallback`, and the handler returns nil.
- **Registry:** 10 rows gain Features citations ahead of the example citations, which stay until
  33.8: `cache.create.write-through`, `cache.invalidate.many-and-where-by-key`,
  `cache.mutation.unbuildable-key-reported`, `cache-key.tenant-per-row-invalidation`,
  `edge.noop-skips-view-invalidation`, the three `event.cache-receive.*`, and the cell rows
  `cache-key.entity.tenanted` and `cache-key.table-pattern.tenanted`. All were already `all`; each
  now shows `pass` on the four Features targets. No `cache-keys` cell is set: the unit rows claim
  them. Design §6.3 named `tenant-table-pattern` cells, but no mutation or receive path builds a
  tenant pattern (the fallback uses `BuildTablePattern`); corrected to `table-pattern`.
- **Fail first (generated-code mutations, restored with `git checkout`):** on `features_sqlite`,
  building `notes` keys from the first row's tenant failed the five two-tenant `notes` `*Many` /
  `*Where` eviction leaves; the same on `tasks` failed its four. Dropping the `Create` write-through
  failed only `Create`; one tenant for every `CreateMany` row failed only `CreateMany`. Dropping the
  view no-op guard failed only the no-row leaf. Not reporting the unbuildable key failed only the two
  unbuildable leaves. Routing `notes` mutations to the table-pattern fallback failed the nine
  `notes` eviction leaves. On receive: ignoring the `notes` stamp failed only the stamped leaf;
  ignoring the `memberships` PK tenant only the tenant-in-PK leaf; a fallback that returns an error,
  or a tenant-less signal sent to `InvalidateMany`, only the tenant-less leaf. On
  `features_postgres_stdlib`, a `notes` key without `public` failed the 11 `notes` write leaves and
  the two `notes` receive leaves. On `features_mysql`, a wrong tenant from the by-key pre-read
  failed the six `notes` by-key and `*Many` leaves and the stamped receive leaf; from the `*Where`
  pre-read, only `UpdateWhere` and `HardDeleteWhere`.
- **Findings:** none. Every case passed on all four targets once the tests were right. Test
  corrections during bring-up: a read-through miss reads its key twice (the singleflight
  re-check), and `Upsert`'s two calls make two `InvalidateMany` calls.
- **Runs:** each Features target passes under `-race` (SQLite 3.51.3, PostgreSQL 16 pgx and
  stdlib, MySQL 8.0). `make lint-conformance` 0 issues. `make req-check` ok (6342 requirements).
  `make conformance`: 6257 pass, 0 fail, 0 skip, 0 not run, 85 untested. `make report-delta`
  against HEAD and against `a46e9e95`: 0 rows changed, none left `pass`.
- **Tests:** `make check` green. No path in the diff is on the `check-examples` trigger list;
  `make conformance` ran every example anyway, all passing.
- **Auto-review:** 0 high-confidence issues; the Features leaves keep or tighten every check of
  the example tests whose rows they cite. Its own fail-first check (`notes` keys from the first
  row's tenant) failed only the five two-tenant `notes` `*Many` / `*Where` leaves. Fixed inline:
  the unbuildable-key leaves now require no backend invalidation of any kind (the record claimed
  it); every eviction leaf now requires nothing written through (it was `UpsertMany` only); design
  §6.3 says the `schema` cells keep their unit citations. Kept: the no-row leaf accepts nil or
  `ErrNotFound` from each write, as the events no-row leaf does.

## 33.7 Core-suite ports — views, relationship filters, comparators

**PRD Reference:** Sections 11.1, 11.2, 13.7, 16.4

**Status:** Not Started

### Tasks

- [ ] Add one regular view over `products`, with a `@pk` annotation, to each core schema and
  `sqlgen.yml`; regenerate the four core `models/`.
- [ ] View reads under the existing method trees: `Get` (keyed), `GetMany`, `Count`, `Paginate`,
  `Connection`. Fill the new `view` column cells.
- [ ] Relationship filters under `GetMany` on the `authors` / `posts` / `tags` edges: O2M and M2M
  exact row sets, composition with a column filter, a discriminator.
- [ ] Comparator semantics under `GetMany` on `products` columns, one case per `comparators`
  matrix operator, filling its per-target cells.
- [ ] Register new rows only where the PRD states a behavior no row records yet. A pinned behavior
  the PRD does not state goes in the PRD gaps table.

### Acceptance Criteria

- Adding the view changes no existing core-suite result.
- The §16.4 view reads, the relationship-filter rows and every `comparators` operator pass on all
  four targets.

### Tests Required

- [ ] Regression guard: `make conformance` on the core targets, where every existing row keeps its
  status after the view is added.
- [ ] `conformance: GetMany/a view returns the rows its query selects` and the `Get`, `Count`,
  `Paginate` and `Connection` siblings (§16.4), on all four targets: fail before, pass after.
- [ ] `conformance: GetMany/an O2M relationship filter returns exactly the parents with a matching
  child` and siblings (§11.1, §13.7), on all four targets: fail before, pass after.
- [ ] `conformance: GetMany/<operator> selects the rows its semantics state`, one per operator
  (§11.2), on all four targets: fails before, passes after.
- [ ] 33.1's report-delta check: no row leaves `pass`.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 33.8 Delete ported example copies — ruling 6

**PRD Reference:** none (`CONFORMANCE.md` §9 ruling 6)

**Status:** Not Started

### Tasks

- [ ] For each of the 52 unclaimed test functions copied across the core dialect examples (design
  §1.1), and each variant example test whose rows moved in 33.4–33.6, decide whether it is
  covered by a suite test that passes on every target. Re-derive the list at HEAD.
- [ ] Delete each covered copy in the same commit as its citation move. Keep a test that also
  covers a config combination or a dialect-only feature, and say why in the Completion Record.
- [ ] Regenerate `docs/conformance/REPORT.md` (`make conformance`).

### Acceptance Criteria

- No registry citation points at a deleted test.
- No carrier-dependent row (design §3.2) has fewer than four passing targets.
- No row moved from `pass` to another status across the phase (33.1's check against the phase's
  base report).

### Tests Required

- [ ] `make req-check` passes after the deletions.
- [ ] `make check-examples` passes after the deletions.
- [ ] 33.1's report-delta check against the phase's base report: no row leaves `pass`.

### Completion Record

_Filled in when complete: files changed, commit references, notes._
