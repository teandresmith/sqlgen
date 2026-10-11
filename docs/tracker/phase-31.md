# Phase 31: Introspection Examples — `source: database` and `both` End to End

Status: In Progress
PRD Sections: 4.1.1, 4.2, 4.3, 6.1, 6.3, 8.1.7, 16.2, 30.4.2

> **Design.** `docs/design/INTROSPECTION_EXAMPLES.md` (ACCEPTED 2026-10-11) is the source for every
> sub-item below. Its §2 records the 2026-10-11 probe: all 12 examples regenerated from a live
> database, with P1 (PostgreSQL type comments) the only defect. Its §8 records the four rulings:
> - Q1: twin all 12 examples.
> - Q2: three `both` modules.
> - Q3: a shared `internal/livedb` package.
> - Q4: findings triage for divergences.
>
> **Ground rules for every sub-item:**
> - P1 and each parity exception start with a test that fails against HEAD. Record the failure
>   in the Completion Record.
> - When a twin or a `both` example trips a by-source divergence:
>   - a measured failure is a candidate FIX for the user;
>   - anything else goes to `docs/tracker/backlog.md` (`/fix` → Findings triage);
>   - steering an example schema around a case needs a backlog line naming it (ruling Q4);
>   - a new parity exception needs a PRD §6.1 sentence that already allows the difference;
>   - never `t.Skip` a dialect to reach green.
> - Integration tests skip under `testing.Short()` (`guidelines/TESTING.md` §2).

## 31.0 PRD and guideline sync — database twin and `both` examples

**PRD Reference:** Section 8.1.7

**Status:** Complete

### Tasks

- [x] PRD §8.1.7, "End-to-End Integration Tests (Example Projects)": add one paragraph.
  - Every `files` example is also generated from a live database built from its own setup
    files, and must match its goldens apart from the differences §6.1 names.
  - The `introspect_*` examples generate under `source: both`. Their output must equal the
    output of `database` on the migrated database and of `files` on schema plus migrations.
- [x] PRD §8.1.7 example tree: add `introspect_postgres/` with `migrations/` and `after_views.sql`,
  and one line each for `introspect_mysql/` and `introspect_sqlite/`.
- [x] `guidelines/TESTING.md` §10 Layout: add the three `introspect_*` modules and
  `after_views.sql`.
- [x] `guidelines/TESTING.md` §10: add a "Database setup" paragraph (design §4.2) giving the
  setup order: `schema.sql`, then `views/*.sql` in lexical order, then `after_views.sql`.
- [x] `guidelines/TESTING.md` §10 Running table:
  - `TestE2EDatabaseTwin` (needs Docker);
  - `TestE2EGoldenFiles` needs Docker for `both` examples outside `-short`;
  - `make update-golden-e2e` needs Docker.
- [x] `guidelines/TESTING.md` §10 "When to Add a New Example": cite the `introspect_*` modules
  under "a new feature spans the whole pipeline".
- [x] Cross-check every edit against design doc §7.

### Acceptance Criteria

- PRD §8.1.7 states the twin invariant and the three-way `both` invariant, with no behavior
  change anywhere else in the PRD.
- `guidelines/TESTING.md` §10 documents:
  - the setup convention;
  - the new modules;
  - the new test;
  - the Docker requirement for `both` goldens.
- `make check` is green (`refs-check` included).

### Tests Required

- [x] None (docs only). `make check` green.

### Completion Record

- **Completed:** 2026-10-11.
- **Files changed:** `docs/PRD.md` (§8.1.7 example tree gains `introspect_postgres/` with
  `after_views.sql` and `migrations/`, plus `introspect_mysql/` and `introspect_sqlite/`; one
  paragraph after "Golden file tests" states the twin and the three-way `both` invariant),
  `guidelines/TESTING.md` (§10 Layout, new "Database Setup" subsection, Running table rows and
  paragraph, "When to Add a New Example").
  Also `docs/design/INTROSPECTION_EXAMPLES.md` (§4.2, §5.4, §10) and
  `docs/tracker/IMPLEMENTATION_ORDER.md` (31.4 summary), from the review pass below.
- **Inline doc fixes:** the §10 Layout tree listed a nonexistent `sqlite_stdlib/` and omitted
  `postgres_stdlib/`, `graphql_null_wrappers/`, `graphql_top_level/`, `tenancy_mysql/` and
  `tenancy_postgres/`; it now matches `cmd/sqlgen/testdata/examples/`. "When to Add" cited
  `sqlite_stdlib` for the `database/sql` adapter; it now cites `postgres_stdlib`.
- **Cross-check against design §7:** PRD change is §8.1.7 only, no behavior change; §6.3 untouched;
  `docs/configuration.md` untouched.
- **Tests:** `make check` green (exit 0, `refs-check` included). Docs only, so no
  `check-examples` trigger.
- **Auto-review:** 0 high-confidence issues. Applied its six doc corrections: TESTING §10 Running
  rows (`TestE2EGoldenFiles` skips under `-short`; the twin "generates", it never regenerates),
  the parity-exception wording, `migrations/` in lexical order (PRD §5.6); PRD §8.1.7 step 1
  ("its inputs", not `schema.sql`); design §4.2 names all three `TestMain`s that create a
  view index inline (`postgres`, `graphql`, `tenancy_postgres`), which 31.3 moves to
  `after_views.sql`. The 31.4 `-short` `files` row rested on a false premise
  (`TestE2EGoldenFiles` skips entirely under `-short`); the user ruled 2026-10-11 to drop it, so
  design §5.4 / §10 and the 31.4 tasks now keep the whole-test `-short` skip.

## 31.1 PostgreSQL type comments — introspect `COMMENT ON TYPE` / `COMMENT ON DOMAIN` (P1)

**PRD Reference:** Sections 6.1, 6.3

**Status:** Complete

### Tasks

- [x] Fail first. Add type comments to `pgParityFixture` (`parser/introspect/parity_fixtures_test.go`):
  - `COMMENT ON TYPE probe_app.status_enum`;
  - a composite type `probe_app.address` with `COMMENT ON TYPE`;
  - `COMMENT ON DOMAIN probe_app.positive_int`.

  Confirm that `TestPostgresIntrospect_NoSearchPath` (the `assertSchemaParity` test on this
  fixture) fails on `Enums[…].Comment`,
  `CompositeTypes[…].Comment` and `DomainTypes[…].Comment`. Quote the failure.
- [x] `parser/introspect/postgres.go`: read comments for every type in one query per run:
  - the query is `pg_description` joined on `pg_type` (`classoid = 'pg_catalog.pg_type'::regclass`,
    `objsubid = 0`), keyed by schema and type name;
  - set `Comment` in `introspectEnums`, `introspectCompositeTypes` and `introspectDomainTypes`;
  - a domain's comment lives on its `pg_type` row, so the same map covers all three kinds.
- [x] Wrap errors in the file's style (`introspect postgres type comments: %w`).
- [x] Add a type comment to the CLI suite's PG fixture `ddl` (`cmd/sqlgen/cli/introspect_integration_test.go`,
  `dialectFixtures`), so `TestGenerate_DatabaseSourceMatchesFiles` pins the generated Go doc comment.
- [x] Remove the 2026-10-11 PostgreSQL type-comment line from `docs/tracker/backlog.md`. Done
  at breakdown (2026-10-11): the phase owns the defect now.
- [x] Probe again: regenerate the `postgres` and `postgres_stdlib` examples from a live
  database. `enums_gen.go` and `types_gen.go` must be identical to `expected/`. 31.3 makes this
  a standing test.

### Acceptance Criteria

- Under `source: database` and `both`, every PostgreSQL enum, composite and domain carries its
  `COMMENT ON` text. Its generated Go doc comment is the same as under `files`
  (`// OrderStatus — Lifecycle state of a customer order`), as PRD §6.1 requires.
- A type with no comment still gets the default doc comment on both sources.
- `parityExceptions["postgres"]` stays empty.
- No golden churns. `files` output is unchanged.

### Tests Required

- [x] `TestPostgresIntrospect_NoSearchPath` parity passes with type comments on an enum, a
  composite and a domain, and fails with the new query reverted. Quote the failure.
- [x] `TestGenerate_DatabaseSourceMatchesFiles/postgres` passes with a commented type in the
  fixture, and fails with the query reverted.
- [x] The existing `parser/introspect` suite passes without `-short`.

### Completion Record

- **Completed:** 2026-10-11.
- **Files changed:**
  - `parser/introspect/postgres.go`: `typeComments` reads every type comment in one query
    (`pg_description` on `pg_type`, `objsubid = 0`), keyed by schema and type name.
    `introspectEnums`, `introspectCompositeTypes` and `introspectDomainTypes` set `Comment` from it.
  - `parser/introspect/parity_fixtures_test.go`: `pgParityFixture` gains a composite
    `probe_app.address` and comments on `status_enum`, `address` and `positive_int`.
    `probe_other.region` stays uncommented, so the default path is covered too.
  - `cmd/sqlgen/cli/introspect_integration_test.go`: `pgMatchDDL` gains
    `COMMENT ON TYPE user_status` and `COMMENT ON DOMAIN positive_int`.
- **Fail-first (parser), fixture added, query absent:** all four
  `TestPostgresIntrospect_NoSearchPath` subtests fail:
  ```
  Enums: { Name: "status_enum", ...
  -   Comment: "Lifecycle state of a user",
  +   Comment: "",
  CompositeTypes: { Name: "address", ...
  -   Comment: "Postal address",
  +   Comment: "",
  DomainTypes: { ... BaseType: "integer", ...
  -   Comment: "Integer above zero",
  +   Comment: "",
  ```
- **Fail-first (CLI), fixture added, `postgres.go` reverted to HEAD:**
  ```
  --- FAIL: TestGenerate_DatabaseSourceMatchesFiles/postgres
  -   // UserStatus — Lifecycle state of a user
  +   // UserStatus represents the user_status enum type.
  -   // PositiveInt — Integer above zero
  +   // PositiveInt is a domain type alias for int32.
  ```
- **Probe again:** `postgres` and `postgres_stdlib` regenerated from a live PostgreSQL 16 database
  built from `schema.sql` and `views/*.sql` (plus the `order_totals` index for `postgres`).
  `enums_gen.go` and `types_gen.go` are byte-identical to `expected/` in both. The only remaining
  `postgres` difference is the view `source` in the manifest (`manifest_gen.json` and three view
  `.md` files), which 31.2's exception covers. `postgres_stdlib` is identical apart from
  hand-written tests.
- **Tests:** `go test ./introspect/` (parser, without `-short`) green; `go test ./cli/` (without
  `-short`) green; `make check` green; `make check-examples` green. No golden changed.
- **Tracker fix:** the fail-first test is `TestPostgresIntrospect_NoSearchPath`, which applies
  `pgParityFixture`; `TestPostgresIntrospect` has its own inline DDL. Design §6 said "one query
  per schema, keyed by type OID"; it now says one query per run, keyed by schema and type name.
- **Auto-review:** 0 high-confidence issues; fail-first independently confirmed. Applied its
  design §6 wording fix (the CLI test has its own fixture, `pgMatchDDL`, rather than the shared
  probe fixture) and its rename of the map key type to `qualifiedType`, since `sqlite.go` uses
  `typeName` for local variables. Not applied: an uncommented type in `pgMatchDDL`. The parser
  parity test already covers the default path through `probe_other.region`.

## 31.2 Shared live-database test package — `cmd/sqlgen/internal/livedb`

**PRD Reference:** Sections 6.1, 30.4.2

**Status:** Not Started

### Tasks

- [ ] Create `cmd/sqlgen/internal/livedb` (package doc: test support for live-database tests;
  imported only by `_test.go` files). Move into it from `cli/introspect_integration_test.go`,
  exporting each:
  - the lazy container starters (`postgresAddr`, `mysqlServerAddr`), with their
    `sync.Once` state and the `testing.Short()` skip;
  - `postgresURL`, `mysqlDSN`, `execSQL`;
  - `newPostgresDatabase`, `newMySQLDatabase`, `newSQLiteDatabase`.
- [ ] Add `ApplySetup(t, dialect, conn, dir string)`, the design §4.2 applier:
  - it applies `schema.sql`, then `views/*.sql` in lexical order, then `after_views.sql` when
    present;
  - PostgreSQL runs one `Exec` per file;
  - MySQL opens with `multiStatements=true`;
  - SQLite runs one `Exec` per file.
- [ ] Move `sourceParityException`, `sourceParityExceptions`, `dropViewSource` and
  `normalizeTree` into the package. Export `NormalizeTree` and the exception list.
- [ ] Extend `dropViewSource` to the split manifest layout: `manifest/entities/<entity>.json`
  with `kind: view`. Treat a view with an empty `source` as an error, as the single layout does.
- [ ] Add the MySQL literal-default exception. Its reason quotes PRD §6.1 ("a literal default
  is stored converted to its column's type … `TRUE` as `1`").
  - In the JSON and markdown manifest, it rewrites only a `default` of `true` / `false` on a
    `tinyint(1)` column to `1` / `0`, on both trees.
  - It applies to MySQL only. The dialect comes from the manifest, or from a parameter of
    `NormalizeTree`.
  - No other value is masked.
- [ ] Point `cli/introspect_integration_test.go` at the package, with no behavior change. Its
  `dialectFixtures` stays in the CLI suite.
- [ ] `go mod tidy` in `cmd/sqlgen`, and confirm that `testcontainers-go` was already required.
  Confirm that `go list -deps .` (the `sqlgen` binary) does not include `internal/livedb`.

### Acceptance Criteria

- One exception list serves the CLI suite and the E2E harness (ruling Q3).
- Every exception names its PRD §6.1 sentence, and no exception masks a whole file or field
  across all dialects.
- The `sqlgen` binary's dependency graph is unchanged.
- The CLI live-database suite passes unchanged on all three dialects.

### Tests Required

- [ ] `livedb` unit tests (no container), table-driven over the exceptions:
  - The view-`source` exception removes `source` from a view in both layouts and leaves a
    table's untouched.
  - It errors on a view with no `source`.
  - The MySQL default exception rewrites `true` → `1` on `tinyint(1)`. It leaves `true` on a
    non-`tinyint(1)` column, leaves a PostgreSQL manifest, and leaves `'1.5'` unchanged.
- [ ] Fail-first per exception. Each is removed in turn and a twin case that needs it fails:
  - the CLI suite for view `source`;
  - the 31.3 `mysql` twin for the boolean default, or a CLI MySQL fixture column
    `active BOOLEAN DEFAULT TRUE` added here.

  Quote each failure.
- [ ] `go test -race -count=1 ./cli/... ./internal/...` in `cmd/sqlgen` (Docker) is green.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 31.3 Database twin — every `files` example from a live database

**PRD Reference:** Sections 6.1, 16.2, 8.1.7

**Status:** Not Started

### Tasks

- [ ] `cmd/sqlgen/testdata/examples/postgres/after_views.sql`: move the `order_totals_product_id_idx`
  unique index out of `tests/main_test.go`. Change that `TestMain` to apply the file after the
  views, so the runtime suite and the twin build the same database.
- [ ] Check every other example's `TestMain` for DDL beyond `schema.sql` and its views. Move any
  into that example's `after_views.sql`. Seed data stays where it is.
- [ ] Confirm lexical view order works for every example. The `postgres` `TestMain` order is
  `product_summary`, `category_stats`, `order_totals`. If one needs an explicit order, stop and
  ask before adding one to the convention.
- [ ] `cmd/sqlgen/e2e_test.go`: add `TestE2EDatabaseTwin`.
  - It skips under `testing.Short()`.
  - For each `discoverExamples` entry whose `input.source` is `files` or unset, as a subtest:
    1. create a database on the example's dialect (`livedb`) and `livedb.ApplySetup`;
    2. copy the example to `t.TempDir()`, rewriting the `go.mod` `replace` to the absolute
       repo root, as `TestGenerate_DatabaseSourceCompiles` does;
    3. rewrite the copy's `sqlgen.yml`: `input.source: database`, `input.connection.url` set,
       and `input.paths` removed;
    4. generate in the copy with `--manifest-timestamp goldenManifestTimestamp` and `GOWORK=off`;
    5. collect the output dir plus any `siblingGraphDirs`, skipping `*_test.go` as
       `compareWithExpected` does;
    6. normalize both the output and `expected/` with `livedb.NormalizeTree`, then `cmp.Diff`
       per file.
  - It never writes goldens: it ignores `-update-e2e`.
- [ ] Report a twin failure per example and per file, with the unified diff, so one example's
  divergence does not hide another's.
- [ ] Record each example's twin wall time in the Completion Record. The design estimates
  1–2 minutes in total.

### Acceptance Criteria

- All 12 `files` examples generate from their database, and match `expected/` after the
  shared exceptions:
  - `cache`, `events`;
  - `graphql`, `graphql_null_wrappers`, `graphql_top_level`;
  - `mysql`, `postgres`, `postgres_stdlib`, `sqlite`;
  - `tenancy`, `tenancy_mysql`, `tenancy_postgres`.
- The checked-in `models/` and `expected/` are never written by the twin.
- `make test` (`-short`) does not start a container or run the twin.
- Each example's runtime suite still passes against a database built by `after_views.sql`.

### Tests Required

- [ ] `TestE2EDatabaseTwin` green on all 12 examples (Docker).
- [ ] Fail-first: with 31.1's query reverted, the `postgres` and `postgres_stdlib` subtests fail
  on `enums_gen.go` / `types_gen.go`. Quote the failure.
- [ ] `make check-examples` green, with the `postgres` example's `TestMain` reading `after_views.sql`.
- [ ] `make test-integration` green.

### Completion Record

_Filled in when complete: files changed, commit references, notes._

## 31.4 `source: both` examples — `introspect_postgres`, `introspect_mysql`, `introspect_sqlite`

**PRD Reference:** Sections 4.1.1, 4.2, 4.3, 6.1 (`both` Mode — Merge Strategy), 6.3, 16.2

**Status:** Not Started

### Tasks

- [ ] **Harness first** (`cmd/sqlgen/e2e_test.go`). `TestE2EGoldenFiles` reads each example's
  `input.source`. For `both`:
  - **Default mode:** build the base database (`livedb.ApplySetup`) and set
    `SQLGEN_EXAMPLE_DATABASE_URL`. Generate under `both` and compare with `expected/` byte
    for byte. Then build the migrated database (setup, then `migrations/*.sql` in file order).
    Generate the copy under `source: database` and compare it, normalized. Finally generate
    the copy under `source: files` with `paths: [schema.sql, migrations/]` and compare it,
    normalized (design §5.2).
  - **`-short`:** no change. `TestE2EGoldenFiles` skips under `-short` for every example
    (ruled 2026-10-11 at 31.0).
  - **`-update-e2e`:** build the base database and regenerate `expected/` under `both`. It
    needs Docker.
- [ ] Per module, use the layout in design §5.1:
  - `go.mod` / `go.sum` per `guidelines/TESTING.md` §10;
  - `sqlgen.yml` with `source: both`, `connection.url: ${SQLGEN_EXAMPLE_DATABASE_URL}`,
    `paths: [./migrations/]` and `views: [./views/]`;
  - `schema.sql`, `views/`, optional `after_views.sql`;
  - `migrations/`, `expected/`, `models/`, `tests/`.
- [ ] `introspect_postgres` (driver `pgx`). The base schema has about six tables and covers:
  - identity, a domain, a composite;
  - a cross-schema FK;
  - table, column and type comments (31.1);
  - a generated column;
  - a materialized view with `@pk`.

  The migrations cover every pg ✓ row of design §5.3: `ADD COLUMN` (nullable, and
  `NOT NULL DEFAULT`), `DROP COLUMN`, `ADD CONSTRAINT … FOREIGN KEY`,
  `ADD CONSTRAINT … UNIQUE`, `CREATE TABLE` with an FK into an introspected table, `DROP TABLE`,
  `CREATE TYPE … AS ENUM` used by an added column, and `ALTER COLUMN … SET NOT NULL` on a column
  a view selects.
- [ ] `introspect_mysql` (driver `stdlib`). The base schema covers:
  - `unsigned`, `SET`;
  - a generated column, comments;
  - a `BOOLEAN DEFAULT TRUE` column (the 31.2 exception).

  The migrations cover every mysql ✓ row of §5.3, using an inline `ENUM` for the new enum and
  `MODIFY` for the view-column nullability change.
- [ ] `introspect_sqlite` (driver `stdlib`, modernc). The base schema covers:
  - a rowid alias;
  - a composite PK in non-column order;
  - a generated column, a partial index.

  The migrations cover every sqlite ✓ row of §5.3: `ADD COLUMN` (including
  `ADD COLUMN … REFERENCES` for the FK row), `DROP COLUMN`, `CREATE UNIQUE INDEX`,
  `CREATE TABLE` with an FK into an introspected table, and `DROP TABLE`.
- [ ] Keep every pending migration inside the three-way invariant. A shape where PRD §6.1 lets
  the sources differ stays in the CLI suite:
  - a pending view change;
  - a column whose type the catalog reports differently.

  If a planned shape breaks the invariant, record it and stop to ask.
- [ ] Each module's `tests/main_test.go` starts its container, then applies `schema.sql`,
  `views/`, `after_views.sql` and `migrations/` in order. The runtime suite tests the migrated
  database, which is the schema the generated code describes.
- [ ] Runtime tests per module, split by concern (`guidelines/TESTING.md` §10). Each ✓ row of
  §5.3 gets the check the table names:
  - an added column round-trips through Create, Get and Update;
  - Create succeeds after `DROP COLUMN`;
  - the FK-inferred relationship from `ADD CONSTRAINT` (or `ADD COLUMN … REFERENCES`) loads;
  - the new table's CRUD works, and its relationship into the introspected table loads;
  - the dropped table is absent from the manifest;
  - the new enum value round-trips, and the database rejects an invalid one;
  - the view column is non-nullable after the pending `SET NOT NULL` / `MODIFY`;
  - `@pk` on the view gives `Get`;
  - the generated column is absent from `Create<T>Input` / `Update<T>Input`, and is readable.
- [ ] Generate the goldens with `make update-golden-e2e` (Docker), and commit `expected/` and
  `models/`.
- [ ] Confirm the examples workflow (`.github/workflows/examples.yml`) discovers the three modules
  from their `go.mod`, with no workflow edit.

### Acceptance Criteria

- Each `introspect_*` example generates under `source: both` from its base database plus
  `migrations/`, and the generated code reflects the migrated schema (PRD §6.1, `both` use case).
- That output equals `expected/` byte for byte. `database` on the migrated database and `files`
  on schema plus migrations equal it after the shared exceptions only.
- Each runtime suite passes against the migrated database, and exercises every §5.3 ✓ shape for
  its dialect.
- The connection comes from a `${VAR}` reference (PRD §4.1.1, §4.3). No credentials are committed.

### Tests Required

- [ ] `TestE2EGoldenFiles/introspect_{postgres,mysql,sqlite}` green (Docker).
- [ ] Fail-first for the three-way check: a deliberately wrong golden fails the `both` row, and
  an extra column in the migrated database fails the `database` row. Quote both and revert.
- [ ] The runtime suite of each `introspect_*` module is green under `make test-examples`.
- [ ] `make check`, `make check-examples` and `make test-integration` green.
- [ ] Two consecutive `make update-golden-e2e` runs leave the tree unchanged (idempotency,
  `guidelines/TESTING.md` §10).

### Completion Record

_Filled in when complete: files changed, commit references, notes._
