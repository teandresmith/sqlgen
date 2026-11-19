# Conformance Variant Suite and Core-Suite Ports

> **Status:** draft (2026-11-18), rulings Q1–Q4 settled 2026-11-18 (§11). Verified against source at `5b24d6ba`. About 120 requirement
> rows for tenancy (§29), cache (§27) and events (§28) are tested only in example modules, each on
> one dialect, mostly SQLite. The report shows them as `pass`, and ten of them are narrowed to
> `sqlite` with no PRD basis. This note adds one variant suite (tenancy, cache and events
> together) on all four conformance targets. It runs the rows whose generated code differs by
> dialect, relabels the rows whose code does not, and ports views, relationship filters and
> comparator semantics into the core suite. It leaves the GraphQL, REST and gRPC rows and the
> dialect-specific example tests where they are. Sibling to `docs/design/CONFORMANCE.md` §8
> (follow-ons) and PRD §27, §28, §29, §11 and §16.

## 1. Motivation

### 1.1 Symptom

Measured on 2026-11-18 at `5b24d6ba`, by joining the registry (`docs/conformance/**/*.md`) with
each example's `input.dialect`:

- 117 registry rows cite only tests in the `tenancy`, `cache`, `events`, `tenancy_postgres` or
  `tenancy_mysql` examples, and each row's tests run on one or two dialects. The `cache`,
  `events` and `tenancy` examples are all SQLite (`sqlgen.yml` `input.dialect`).
- `docs/conformance/REPORT.md` lists every one of them as `pass`. A row's status is its most
  severe target, and "one pass is enough" (`REPORT.md` line 14), so a row tested on SQLite
  alone passes.
- Ten `client.md` rows are narrowed to `sqlite` because the only test is in the SQLite `tenancy`
  example: `tenancy-pk.match-proceeds`, `tenancy-pk.mismatch`, `tenancy-pk.zero-tenant`,
  `tenancy-pk.skip-tenancy`, `tenancy-pk.update-input-omits-tenant` (§29.7),
  `tenancy-hooks.filter-not-a-hook`, `tenancy-hooks.input-unchanged`,
  `tenancy-hooks.affected-pks-tenant` (§29.9), `error-prefix.tenant-mismatch` (§22.3) and
  `client.close-cache-publisher` (§20.2). `CONFORMANCE.md` §5.1 requires a narrowing to name a
  capability or quote the PRD. None of these does, and `tenancy_postgres` and `tenancy_mysql`
  both have tenant-in-PK tables (`schema.sql:44,73` and `:83,101`).

A separate core-suite gap:

- 52 test functions are copied across the `postgres`, `postgres_stdlib`, `mysql` and `sqlite`
  examples, and no registry row claims them. They are the per-dialect copies `CONFORMANCE.md`
  §1.1 set out to replace. They cover CRUD, views (`TestViewGetMany`, `TestViewCount`,
  `TestViewPaginate`, `TestViewConnection`, `TestViewGetByPK`), relationship loads and filters
  (`TestRelationshipO2M` / `M2M` / `O2O`, `TestRelationshipFilter_*`,
  `TestSubCategorizedPolymorphism*`) and comparators (`TestFilterComparators`,
  `TestDecimalFilter_OrderedOperatorsAreNumeric`).
- The conformance schema has no view (`cmd/sqlgen/testdata/conformance/postgres/schema.sql`), so
  §16 view reads have no runtime test home. The eight `client.md` §16 rows cite unit tests only.
- The 171 `runtime/sql.md` §11 comparator rows cite unit tests that check rendered SQL text. No
  registry row runs a comparator against a database, although the `comparators` matrix declares a
  column per target.

### 1.2 Root cause

`CONFORMANCE.md` §4.1 keeps API-changing variants out of the core schema, and §8 defers a tenancy
and a cache suite as follow-ons that were never scheduled. Events is not listed there. The rows
were seeded against the tests that existed, which were the examples. The registry tool supports
exactly one suite:

- The `conformance:` home is hard-wired to one directory and one root function:
  `ConformanceSuiteDir` and `conformanceRoot = "Run"`
  (`cmd/sqlgen/internal/reqcheck/testtree.go:18,32`; used at `check.go:436,608-617`).
- Targets are every `cmd/sqlgen/testdata/conformance/*/go.mod` except `suite`
  (`report.go:159-167`), and a citation names no target: `joinRow` checks a `conformance:` path
  on every target admitted by Applies-to (`report.go:239-247`).
- CI finds targets the same way (`.github/workflows/examples.yml:44-45`, `grep -vx suite`).

## 2. Scope

### 2.1 In scope

- One variant suite, **Features**, with tenancy, cache and events enabled in one generated
  package, on postgres/pgx, postgres/stdlib, mysql and sqlite.
- `reqcheck`, Makefile and CI support for a second suite.
- Moving the carrier-dependent rows (§3.2) to the Features suite, and widening each one to `all`.
- Relabeling the dialect-neutral rows (§3.3), the ten unjustified `sqlite` narrowings included.
- Core-suite ports: a view in the core schema and view reads, relationship filters, and
  comparator semantics against each database.
- Deleting each example copy once its port passes on every target (`CONFORMANCE.md` §9 ruling 6).

### 2.2 Non-goals

- **GraphQL, REST and gRPC rows** (about 230): the suite tests the Go client. The API layer sits on
  top of it and is dialect-neutral.
- **Dialect-specific example tests:** materialized views, arrays, `inet`, MySQL `DATETIME`
  binding, SQLite time functions and `MATCH FULL` keep their justified narrowings.
- **Golden, consumer-build, database-twin and `source: both` coverage:** these stay example jobs.
- **Event transports** (`runtime/events.md` `transports` matrix): delivery over time is out of
  scope per `CONFORMANCE.md` §2.
- **No product change:** no PRD behavior, config key or generated API changes.

## 3. Current state (measured from code)

### 3.1 Where generated code differs by dialect

`event_hooks.go.tmpl` and `cache.go.tmpl` contain no dialect or driver token (`grep -ciE
'dialect|supportsReturning|pgx'` returns 0 for both). Events and cache consume the mutation's
carriers, `MutationContext.AffectedPKs`, `AffectedTenants` and `Input`. What differs by dialect
is how each write fills those carriers:

| Write path | PostgreSQL / SQLite | MySQL | Cited |
|---|---|---|---|
| Single-row Update / Delete / Restore / Increment tenant capture | `RETURNING` the tenant | pre-read `captureAffectedTenants`, then `Exec` | `table/update.go.tmpl:121-150,184-204`; `table/delete.go.tmpl:55,73,120`; `table/increment.go.tmpl:57,75,106` |
| Matched-but-unchanged row | n/a | `rowMatches` lookup decides `AffectedPKs` | `table/update.go.tmpl:217-247` |
| `*Many` / `*Where` affected PKs and tenants | `RETURNING` | `collectAffectedPKs` pre-read | `table/update.go.tmpl:1000,1070,1109,1239,1294-1345`; `table/delete.go.tmpl:515,592,638,771` |
| `UpdateMany` execution | pgx `SendBatch`, or `WithTransaction` without it; stdlib per batch | stdlib per batch | `table/update.go.tmpl:286-310,441-590` |
| Create / Upsert PK | `RETURNING` | `LastInsertId`, Upsert tenant read-back | `table/create.go.tmpl:503,523,583-602`; `table/upsert.go.tmpl:258-288,322-443` |
| Cache key schema segment | PostgreSQL `public` or `input.schema` | `""` (SQLite too) | `cmd/sqlgen/gen/context_cache.go:253-261` |

`SupportsReturning()` is true on SQLite and PostgreSQL and false on MySQL (`sql/sqlite.go:48`,
`sql/postgres.go:78`, `sql/mysql.go:49`). So the SQLite examples already run the `RETURNING`
branch. They miss the MySQL pre-read branches, the pgx `SendBatch` path and the PostgreSQL key
segment.

Existing coverage off SQLite (`tenancy_mysql/tests/tenant_capture_test.go`,
`update_many_repeated_key_tenant_test.go`, `tenancy_postgres/tests/tenant_capture_test.go`) checks
precise eviction for `UpdateMany`, `UpdateWhere` and `SoftDelete`, and per-row event tenants on
MySQL. Nothing checks MySQL `Upsert`, `UpsertMany`, `Increment` or `Create` `LastInsertId` events,
the `rowMatches` path, MySQL `HardDeleteWhere`, or any PostgreSQL event.

### 3.2 Carrier-dependent rows (need every target): 48

Their outcome depends on the carriers of §3.1:

- **Events per operation** (`runtime/events.md` §28.4): `event.op.create`, `.create-many`,
  `.update`, `.update-many`, `.update-where`, `.upsert`, `.soft-delete`, `.soft-delete-many`,
  `.soft-delete-where`, `.hard-delete`, `.hard-delete-many`, `.hard-delete-where`, `.restore`,
  `.restore-many`, `.restore-where`, `.increment`; `event.batch.one-publish-batch`,
  `event.pk.fire-and-forget`, `event.hook.failed-mutation-no-event`;
  `gen.events.empty-affected-publishes-nothing` (`cli/gen.md` §28.9).
- **Event input** (§28.6): `event.input.single-op-same-pointer`, `.increment-by-value`,
  `.create-many-ith`, `.update-many-ith-written`, `.delete-restore-nil`, `.update-where-shared`,
  `.where-filter-shared`.
- **Tenant stamps** (§29.6): `tenant-event.every-tenanted-event`, `.stamp-row-derived`,
  `.stamp-from-pk`, `.skip-tenancy-stamped`, `.required-false-zero-stamped`,
  `.unmatched-key-no-event`, `.stamp-percent-v`.
- **Cache invalidation** (§27.5, §27.7, §27.9, §29.5): `cache.invalidate.many-and-where-by-key`,
  `cache.create.write-through`, `cache.mutation.unbuildable-key-reported`, `cache-key.tenant-per-row-invalidation`;
  `edge.noop-skips-view-invalidation` (`client.md`). `cache.invalidate.mutations-by-key` stays on
  the `cache` example: it is stated for a package with no tenanted table, which the Features
  package is not (user ruling, 2026-11-18).
- **Event-driven invalidation** (§28.11, PostgreSQL key segment): `event.cache-receive.tenant-stamp-precise`,
  `.tenant-in-pk-precise`, `.tenant-less-fallback`.
- **Tenant in the PK** (§29.7, §29.9): `tenancy-pk.match-proceeds`, `tenancy-pk.skip-tenancy`,
  `tenancy-hooks.affected-pks-tenant`.
- **Nested tenancy** (§9.9): `nested.unlink-tenant-mismatch`,
  `nested.m2m-create-shared-column-mismatch`. Their tests are in `tenancy_postgres` only.
- **Tenant filter comparator** (`cli/gen.md` §29.2): `gen.tenancy.filter-comparator-follows-type`.
  The comparator comes from a different `resolveSimpleComparator` arm per tenant column type: the
  SQL-type `uuid` arm (`cmd/sqlgen/gen/context_table.go:2822`) on PostgreSQL, the tenant-type arm
  (`:2843`) for a SQLite `blob`. Only SQLite is tested.

Moved from §3.3 on 2026-11-18 (user ruling during 33.0): the two no-op guards
(`event_hooks.go.tmpl:103`, `cache.go.tmpl:1011`) have no dialect branch, but they read
`AffectedPKs`, which MySQL fills by pre-read (§3.1). So their outcome depends on the carriers.

### 3.3 Dialect-neutral rows (one target is enough): 61

Their behavior runs before any SQL, or in code with no dialect branch: the tenant verify-match
and `SkipTenancy` check (`table/get.go.tmpl:52-69,188`), the relationship-subquery tenant
predicate (`shared/_filter.tmpl:233-237`), `event_hooks.go.tmpl` (fan-out, `MetadataFunc`,
`PublishBatch`, `tx.OnCommit` deferral at 294-298), `cache.go.tmpl` (read-through,
singleflight, breaker, hydration, `afterCommit` at 1058), and the shared `database/transaction.go`
callbacks at 188-212. They cover these areas:

- **Event config** (`cli/config-features.md` §28.8): 10 rows.
- **Tenancy config and constraints** (§29.2, §29.11): 8 rows.
- **Event hook generation** (`cli/gen.md` §28.9, §29.x): 5 rows.
- **Cache facade, read-through, breaker and transactions** (§27.3–§27.9, §29.5 fallback): 18 rows.
- **Event transactions and `SkipEvents`** (§28.4): 4 rows.
- **Tenancy wiring and types** (`runtime/support.md` §29.3): 5 rows.
- **Read-side and pre-SQL tenancy** (`client.md`): `tenancy-pk.mismatch`, `.zero-tenant`,
  `.update-input-omits-tenant`, `tenancy-hooks.filter-not-a-hook`, `.input-unchanged`,
  `error-prefix.tenant-mismatch`, `client.close-cache-publisher`,
  `tenancy-soft-delete.anded`, `.comparator-keeps-tenant`, `.orthogonal`,
  `tenant-rel.composite-load-in-tenant`: 11 rows.

The exact list is the "NEUTRAL" set of the §7 probe, less the three rows moved to §3.2. These rows already apply to `all`, apart
from the seven `sqlite` ones listed above.

### 3.4 Rows that stay narrowed (justified): 7

`gotype.sqlite-time-bind.*` (4), `gotype.uuid.stdlib.database-sql-bytes`,
`cache-key.tenant-capture-mysql-many-per-sub-batch` and
`nested.composite-match-full-disconnect-nulls-every-member` each name the dialect or capability
in their requirement.

### 3.5 The suite plumbing a variant reuses

`cmd/sqlgen/testdata/conformance/` has five modules. `suite/` is shared. Each dialect module
replaces `…/conformance/models` with its own committed `./models` and `…/conformance/suite` with
`../suite`, then calls `suite.Run(t, &suite.Harness{…})` from one `TestConformance`
(`suite/harness.go:21-46,100-130`; `postgres/conformance_test.go:20-75`). `make conformance-fresh`
runs `sqlgen diff --quiet` in each `*/` module (`Makefile:46-51`). The Makefile
(`CONFORMANCE_DIRS = wildcard */go.mod`, `Makefile:6-7`) and CI pick up any new top-level module.
Capabilities are fields on `Harness.Caps`, gated by `h.Require` / `h.Lacks`
(`suite/harness.go:49-95`).

The `tenancy` example shows that one generated package can carry all three features. Its
`sqlgen.yml` enables `events`, `cache` and `tenancy`, and `tests/main_test.go:338-374` builds
clients with and without them through `WithTenantResolver`, `WithCache` (memory backend) and
`WithEventPublisher` (`memorybus`). None of those depends on the database.

## 4. Options

### 4.1 How many variant suites

- **A. One Features suite (recommended).** Tenancy, cache and events in one schema and one
  generated package, as the `tenancy` example already does. Four modules. Each test builds the
  client it needs (cache or not, publisher or not), so the features stay separable. The rows
  that combine features (`tenant-event.*`, `event.cache-receive.*`, `cache.tx.event-before-cache`,
  `cache-key.tenant-per-row-invalidation`) need the combination anyway. Loses: a change in one
  feature's codegen regenerates models used by all three.
- **B. Three suites (tenancy, cache, events).** Twelve modules, three schemas, and the combined
  rows still need a fourth combination. More CI time and container starts for no extra coverage.
- **C. Enable the features in the core schema.** Rejected by `CONFORMANCE.md` §4.1: it changes the
  API under the 30 existing method trees.

### 4.2 How a citation names the suite

- **A. A root segment (recommended).** The Features `Run` wraps its tree in `t.Run("Features", …)`,
  and citations read `conformance: Features/<Method>/<behavior>`. `reqcheck` parses both suite
  directories and sends a `Features/…` path only to Features targets. The README format is
  unchanged.
- **B. A new home prefix** (`conformance-features: Method/behavior`). Explicit, but every
  consumer of the home list (README, `ParseTestRef`, the report) changes, and each later variant
  adds another prefix.

### 4.3 What a dialect-neutral row needs

- **A. One target is enough, recorded (recommended).** Keep each neutral row's test where it is
  (one dialect). Add a registry note that names the dialect-free code it runs through (§3.3
  citations), so the single-target proof is deliberate and reviewable. Widen the seven `sqlite`
  narrowings to `all`.
- **B. Every `all` row on every target.** Port all 61 neutral rows to the Features suite too.
  This roughly doubles the work for no branch that can differ today, and the next template change
  that adds a branch is not caught by the rule either.

## 5. Proposed design

### 5.1 Layout

New top-level modules beside the core ones, so the existing one-level globs keep working:

| Module | Contents |
|---|---|
| `features_suite/` | `package featuressuite`: `Run(t, *Harness)` wrapping `t.Run("Features", …)`, the per-feature trees, and a harness with `Client`, `Querier`, `Caps`, `Reset`, a `memorybus`, a memory cache and a counting `TenantResolver` |
| `features_postgres/`, `features_postgres_stdlib/`, `features_mysql/`, `features_sqlite/` | `TestConformance`, `schema.sql`, `sqlgen.yml` (events, cache and tenancy on) and a committed `models/` declaring `…/conformance/features/models` |

The Features schema has one table per carrier shape:

- tenant outside the PK, database-generated key (`LastInsertId` on MySQL), with a natural unique
  key (Upsert's update branch) and a counter (`Increment`)
- tenant inside a composite PK, with a counter (`Increment`)
- tenanted soft delete
- a tenanted O2M child and a tenant junction (nested tenancy rows)
- a shared untenanted parent whose O2M child and junction take the tenant from it (nested
  tenant-mismatch rows)
- a tenanted table with `tenancy.required: false`
- an untenanted cached table
- one cached view (`event.cache-receive` view fallbacks)

The tenant is `uuid.UUID` on PostgreSQL and SQLite and a `CHAR(36)` string on MySQL, where a
`uuid.UUID` tenant type fails the PRD §29.2.4 SQL-type check. Each `models/conformance.go`
aliases it as `models.Tenant` (ruled 2026-11-18).

### 5.2 `reqcheck`

- Suites are a table: `{Name: "", Dir: "conformance/suite"}` and
  `{Name: "Features", Dir: "conformance/features_suite"}`. The orphan check and the citation check
  walk both trees (`check.go:436,605-636`).
- A target belongs to the suite its module imports. `readTarget` already reads the module's root
  test (`report.go:181-217`). It also records which suite package that test imports, and the
  target belongs to that suite.
- `joinRow` sends a citation whose first segment is a suite name to that suite's targets, and
  every other `conformance:` citation to the core targets.
- Results file names stay `conformance-<basename>.json`. The basenames are distinct.

### 5.3 Makefile and CI

`CONFORMANCE_DIRS` already includes the new modules. `CONFORMANCE_DIALECT_DIRS` and the CI
`find … | grep -vx suite` filter must also drop `features_suite`. No new make target is needed.

### 5.4 Registry moves

- Each §3.2 row gains its `conformance: Features/…` test, and is widened to `all` where it was
  narrowed. Its example test stays cited until the example copy is deleted.
- Each §3.3 row gains a note (§4.3 A), and the seven `sqlite` rows are widened to `all`.
- The core ports add `conformance:` tests to the §16, §11 and relationship-filter rows, and new
  rows only where the PRD states a behavior no row records yet.

### 5.5 Core-suite ports

- **Views:** one regular view in each dialect's core schema over `products`, with a
  `@pk` annotation. View reads (`Get`, `GetMany`, `Count`, `Paginate`, `Connection`) go under the
  existing method trees (`GetMany/a view …`).
- **Relationship filters:** O2M and M2M exact row sets, composition with a column filter, and the
  discriminator filter, under `GetMany`, on the existing `authors` / `posts` / `tags` edges.
- **Comparators:** each `comparators` matrix operator run against the database under `GetMany`,
  on `products` columns. Each fills that operator's per-target cells, which today cite unit tests
  only.

## 6. Behavior contract

The design adds no product behavior. §6.1 records two kinds of row: the registry requirements
that gain a test on every target (the PRD anchors are the rows' own), and the tool behaviors of
`reqcheck`, the Makefile and CI. The tool rows are anchored to `CONFORMANCE.md` rather than the
PRD, since the PRD does not specify repository tooling.

### 6.1 New and changed behavior

| ID | Requirement | PRD | Applies to | Test | Matrix cell | Status |
|---|---|---|---|---|---|---|
| tool.suite-citation | A `conformance: Features/…` citation resolves against the Features suite tree and is joined only with Features targets; a core citation never reaches a Features target | CONFORMANCE.md §5.3 (proposed in §8) | all | `unit: cmd/sqlgen/internal/reqcheck: TestBuildReportJoin/a suite-rooted citation joins only that suite's targets` | — | passing |
| tool.suite-orphans | An unclaimed leaf in the Features tree fails `req-check` as one in the core tree does | CONFORMANCE.md §5.3 | all | `unit: cmd/sqlgen/internal/reqcheck: TestCheck/an unclaimed Features subtest is an orphan` | — | passing |
| tool.suite-targets | The report lists the four Features targets beside the four core ones, each with its database version | CONFORMANCE.md §5.5 | all | `unit: cmd/sqlgen/internal/reqcheck: TestBuildReportTargets/variant modules are targets of their suite` | — | passing |
| tool.features-fresh | A stale Features `models/` fails `make conformance-fresh` | CONFORMANCE.md §4.2 | all | `golden: conformance features_*` (the `sqlgen diff --quiet` gate) | — | passing |
| events.per-op | Each §28.4 operation publishes the events its row states on every target | §28.4 | all | `conformance: Features/Events/<Method>/publishes one event per affected row` | — | passing |
| events.input | Each §28.6 input row holds on every target | §28.6 | all | `conformance: Features/Events/<Method>/the event carries the documented input` | — | passing |
| events.tenant-stamp | Each §29.6 stamp row holds on every target | §29.6 | all | `conformance: Features/Tenancy/<Method>/each event is stamped with its row's tenant` | — | planned |
| cache.invalidation | Each §27.5 / §27.7 / §27.9 / §29.5 invalidation row in §3.2 holds on every target | §27.5, §27.7, §27.9, §29.5 | all | `conformance: Features/Cache/<Method>/evicts each affected row's key` | `cache-keys: entity × tenanted` | planned |
| cache.receive | Each §28.11 receive row holds on every target, PostgreSQL's `public` key segment included | §28.11 | all | `conformance: Features/Cache/event-driven invalidation/<case>` | `cache-keys: tenant-table-pattern × schema` | planned |
| tenancy.pk-carriers | `tenancy-pk.match-proceeds`, `tenancy-pk.skip-tenancy` and `tenancy-hooks.affected-pks-tenant` hold on every target | §29.7, §29.9 | all | `conformance: Features/Tenancy/<Method>/a matching PK tenant proceeds` and siblings | `client-methods: <Method> × composite-pk` | planned |
| tenancy.nested | `nested.unlink-tenant-mismatch` and `nested.m2m-create-shared-column-mismatch` hold on every target | §9.9 | all | `conformance: Features/Tenancy/UpdateWithRelated/<case>` | — | planned |
| relabel.neutral | The seven §3.3 `sqlite` rows apply to `all`, and every §3.3 row carries a note naming its dialect-free code | CONFORMANCE.md §5.1 (proposed in §8) | all | `make req-check` over the widened rows and the notes under each table (ruled 2026-11-18: the note is prose, so no `reqcheck` test) | — | planned |
| views.reads | A view's `Get` (with a declared key), `GetMany`, `Count`, `Paginate` and `Connection` return the rows its query selects, on every target | §16.4 | all | `conformance: GetMany/a view returns the rows its query selects` and siblings | `client-methods: GetMany × view` | planned |
| relfilter.exact | O2M and M2M relationship filters return exactly the parents with a matching child, compose with a column filter, and honor a discriminator | §11.1, §13.7 | all | `conformance: GetMany/an O2M relationship filter returns exactly the parents with a matching child` and siblings | `relationships: o2m × filter`, `relationships: m2m × filter` | planned |
| comparators.db | Each operator of the `comparators` matrix returns the rows its PRD semantics select, on every target | §11.2 | all | `conformance: GetMany/<operator> selects the rows its semantics state` | `comparators: <operator> × <target>` | planned |
| ports.deleted | Each example copy whose port passes on every target is deleted in the porting commit | CONFORMANCE.md §9 ruling 6 | all | `make req-check` (no citation points at a deleted test) | — | planned |

### 6.2 Regression guards

| Behavior that must not change | PRD | Guarded by | Status |
|---|---|---|---|
| Core citations resolve and join exactly as today | CONFORMANCE.md §5.3 | `cmd/sqlgen/internal/reqcheck: TestCheck`, `TestBuildReportJoin`, `TestBuildReportTargets` | guarded |
| The 30 core method trees, schema and harness are unchanged by the Features suite | CONFORMANCE.md §4 | `make conformance` on the core targets (6,256 pass at `84626ff5`) | guarded |
| The report never moves a row from `pass` to another status (`skip` included) or drops a passing row | CONFORMANCE.md §5.5 | `make report-delta` after each sub-item's `make conformance`; `cmd/sqlgen/internal/reqcheck: TestDiffReports`, `TestReportDeltaPassToUntested` | guarded |
| Example tests that are not ported keep passing, and their citations stay valid | CONFORMANCE.md §9 ruling 6 | `make check-examples`, `make req-check` | guarded |
| The examples' feature combinations (`tenancy`, `tenancy_postgres`, `tenancy_mysql`, `cache`, `events`) keep their config-variant tests | CONFORMANCE.md §2 | `make check-examples` | guarded |
| Adding a view to the core schema changes no existing core-suite result | §16.4 | `make conformance` on the core targets | guarded |

The report-delta guard is `make report-delta` (§9 N.1): it compares the working-tree `REPORT.md`
with the one committed at `REPORT_BASE` and fails when a row leaves `pass`.

### 6.3 Module matrix cells

#### client-methods (generated client)

| Method | view (new column, ruled §11 Q4) |
|---|---|
| `Get`, `GetMany`, `Count`, `Paginate`, `Connection` | views.reads |
| `Exists`, `ExistsWhere`, `Stream` | n/a: §16.4 lists no `Exists`, `ExistsWhere` or `Stream` for a view |
| every write and delete method | n/a: §16 views are read-only |

#### comparators (runtime/sql.md)

| Operator | postgres-pgx | postgres-stdlib | mysql | sqlite |
|---|---|---|---|---|
| every declared operator | comparators.db | comparators.db | comparators.db | comparators.db |

#### cache-keys (runtime/cache.md)

| Key shape | tenanted | schema |
|---|---|---|
| `entity` | cache.invalidation | cache.receive |
| `tenant-table-pattern` | cache.receive | cache.receive |

#### relationships (cli/gen.md)

| Type | filter |
|---|---|
| `o2m` | relfilter.exact |
| `m2m` | relfilter.exact |

### 6.4 Dialect differences

- **RETURNING (capability `SupportsReturning`, PRD Appendix B):** PostgreSQL and SQLite fill the
  carriers from `RETURNING`; MySQL fills them by pre-read. The Features suite asserts the same
  events, stamps and evictions on both sides. That is the point of the suite.
- **pgx batch:** `UpdateMany` uses `SendBatch` on pgx and per-batch statements elsewhere. The
  same assertions apply.
- **Cache key schema segment:** `public` (or `input.schema`) on PostgreSQL, empty on MySQL and
  SQLite (`context_cache.go:253-261`). `cache.receive` asserts the key each target builds.

## 7. Probe record

- **2026-11-18 at `5b24d6ba`: registry join.** I parsed every registry table row (6,337 rows),
  took rows citing `example:` and no `conformance:` test, and mapped each example to its
  `input.dialect`. Result: 117 variant rows on one or two dialects, 64 neutral, 46
  carrier-dependent, 7 justified narrowings; and 52 unclaimed test functions copied across at
  least two of the core dialect examples. The script and its output were kept in the session
  scratchpad, not the repo.
- **2026-11-18 at `5b24d6ba`: template scan.** `grep -ciE
  'dialect|isMySQL|isPostgres|isSQLite|supportsReturning|\.Driver|pgx'` returns 0 on
  `event_hooks.go.tmpl` and `cache.go.tmpl`; `table/update.go.tmpl` branches on
  `supportsReturning` at 121, 141, 158, 184 and 217. `SupportsReturning()` returns true at
  `sql/sqlite.go:48` and `sql/postgres.go:78` and false at `sql/mysql.go:49`.
- **Not probed:** whether the Features `sqlgen.yml` generates cleanly on all four targets. N.3
  generates it first, and a failure there is a finding, not a gate-off.

## 8. PRD and doc impact

- **PRD:** none. No behavior changes.
- **`docs/design/CONFORMANCE.md`:**
  - §8: replace the tenancy and cache follow-on bullet with "Variant suite: Features (tenancy,
    cache, events), `docs/design/CONFORMANCE_VARIANTS.md`."
  - §4.2: add a paragraph on variant modules (`features_*`, the `Features` root segment).
  - §5.3: add "A `conformance:` citation whose first segment names a suite (`Features`) is
    checked against that suite's tree and joined only with its targets."
- **`docs/conformance/README.md`:** the citation form `conformance: Features/Method/subtest`. Under
  Applies to, add: "A row whose code has no dialect branch may be proven on one target; its note
  names that code (`path:line`). Any narrowing below `all` still needs a PRD basis."
- **`guidelines/TESTING.md`:** state which test home a new test goes to:
  - a cross-dialect client behavior goes to the core suite
  - a behavior that needs tenancy, cache or events goes to the Features suite
  - a config combination or a dialect-only feature goes to an example
- **Registry files:** the §5.4 moves, in the sub-items that land each test.

## 9. Plan

| # | Sub-item | Contract rows | Notes |
|---|---|---|---|
| N.0 | Doc sync (§8) and relabel the dialect-neutral rows | relabel.neutral | No code. Widens the seven `sqlite` rows and adds the notes |
| N.1 | Regression guards | the report-delta guard | A `reqcheck` report diff (totals per status and per-row changes) run before and after each sub-item; passes on today's report |
| N.2 | `reqcheck`, Makefile and CI support a second suite | tool.suite-citation, tool.suite-orphans, tool.suite-targets | Fail-first against a stub `features_suite` with one subtest |
| N.3 | Features modules: schema, `sqlgen.yml`, harness, committed `models/` | tool.features-fresh | Generation on four targets is the probe §7 did not run |
| N.4 | Events per operation and input | events.per-op, events.input | Includes the MySQL `LastInsertId`, `Upsert` and `Increment` paths §3.1 lists as untested |
| N.5 | Tenancy carriers: stamps, tenant in PK, nested | events.tenant-stamp, tenancy.pk-carriers, tenancy.nested | |
| N.6 | Cache invalidation and event-driven receive | cache.invalidation, cache.receive | |
| N.7 | Core ports: views, relationship filters, comparators | views.reads, relfilter.exact, comparators.db | Adds the view to the four core schemas |
| N.8 | Delete the ported example copies, regenerate the report | ports.deleted | Ruling 6, per copy |

## 10. Definition of done

The work closes only when every item below holds:

1. Every §6.1 row has a test that was seen failing before its implementation and now passes on
   every dialect in its Applies-to column.
2. Every §6.2 row is guarded, and every guard still passes.
3. Every §6.3 cell is an ID or a stated `n/a`.
4. Every §8 edit has landed, and every §11 question is ruled.
5. `make check` and `make check-examples` are green, and `make test-integration` too when the
   change reaches a runtime or driver package.
6. `make conformance` shows no §3.2 row with fewer than four passing targets, and no row that
   moved from `pass` to another status.

## 11. Rulings (settled 2026-11-18)

| # | Question | Ruling | Evidence |
|---|---|---|---|
| Q1 | One Features suite, or a suite per feature? | One Features suite (§4.1 A) | The `tenancy` example generates all three in one package (`tests/main_test.go:338-374`); the combined rows need the combination anyway |
| Q2 | How does a citation name the suite? | A `Features/` root segment (§4.2 A) | The README format stays; `reqcheck` maps the first segment to a suite |
| Q3 | May a dialect-neutral `all` row be proven on one target? | Yes, with a registry note naming the dialect-free code (§4.3 A); the seven `sqlite` narrowings are widened to `all` | §3.3 citations; no dialect token in `event_hooks.go.tmpl` or `cache.go.tmpl` |
| Q4 | Add a `view` column to the `client-methods` matrix? | Yes, with `n/a` for every write and delete method | PRD §16: views are read-only |

## 12. Rejected alternatives

- **Port the tenancy tests into `tenancy_postgres` and `tenancy_mysql`** (the first plan, ruled out
  by the user on 2026-11-18): the Features suite would replace those copies, and ruling 6 would then
  delete them.
- **Enable the features in the core schema:** `CONFORMANCE.md` §4.1.
