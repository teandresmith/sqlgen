# SQLGen Introspection Examples: `source: database` and `both` End to End

> **Status: ACCEPTED (2026-10-11).** This doc is the design for E2E coverage of the two
> database-backed schema sources, `input.source: database` and `input.source: both`
> (PRD §6.1). Phase 30 (`docs/design/INTROSPECTION.md`) built the sources. This work proves
> that code generated from a live database compiles, behaves correctly at runtime, and matches
> what the same schema generates from files. The §2 probe ran on 2026-10-11. The user ruled
> on all four §8 questions on 2026-10-11, each matching its recommendation, so no question is
> open. This doc is the input to `/phase`.
>
> **Phase:** 31 (`docs/tracker/phase-31.md`, broken down 2026-10-11). It is meant to be short:
> one fix, one harness extension, and three small example modules. The §9 preview's N.0–N.4
> are 31.0–31.4.
>
> **Hard dependency:** Phase 30 closed, with its carried-forward FIX queue drained (0 open as
> of `5b1a3573`). File references are to HEAD `5b1a3573`.

---

## 1. Motivation

Every E2E example under `cmd/sqlgen/testdata/examples/` generates from schema files. Coverage
of `database` and `both` today:

| Layer | What it proves | What it leaves out |
|---|---|---|
| `parser/introspect` parity tests (`assertSchemaParity`, 95 tests) | Each introspector builds the same schema model as the file parser on probe-sized DDL. | Generated code. Real-world schema size. |
| `cmd/sqlgen/cli/introspect_integration_test.go` | `generate` under `database` matches `files` byte for byte on one small fixture per dialect, and the output builds (`TestGenerate_DatabaseSourceCompiles`). `both` overlays an `ADD COLUMN` and a `CREATE TABLE`. | Running the generated code. Any schema larger than the fixture. Most `both` merge shapes. |
| Example modules (`make check-examples`) | Generated code runs against a real database. | Every example comes from `source: files`. |

**No test runs code generated from a live database against that database.** FIX-298 is the
kind of bug that gap hides: the file parser and the database disagreed, and only running the
code showed it. The PRD promises that `database` and `files` give "the same generated code and
manifest" (§6.1). It also says `both` "lets the generator produce code that reflects the
*future* schema". At example scale, nothing measures either claim.

## 2. Probe (2026-10-11, at `5b1a3573`)

**Method:**
1. Build `sqlgen` from HEAD.
2. For each of the 12 examples, apply its `schema.sql` and its `views/*.sql` to a fresh database:
   - PostgreSQL 16 in a container;
   - MySQL 8.0 in a container;
   - SQLite in a file, through the `sqlite3` CLI.
   The PostgreSQL example also gets its `order_totals` unique index, which its `TestMain` creates.
3. Copy the example to a sibling directory and rewrite `sqlgen.yml` to `source: database` with a
   `connection.url`, dropping `paths`.
4. Run `sqlgen generate --manifest-timestamp 2026-01-01T00:00:00Z` with `GOWORK=off`.
5. Run `diff -r` between the example's `expected/` and the new output.

**Result:** every example generates from its database (exit 0). The differences:

| Example | Dialect | Go code | Manifest |
|---|---|---|---|
| `postgres` | pg | **2 files differ:** `enums_gen.go`, `types_gen.go` (P1) | View `source` only |
| `postgres_stdlib` | pg | **2 files differ:** `enums_gen.go`, `types_gen.go` (P1) | Identical |
| `graphql` | pg | Identical¹ | View `source` only (split layout) |
| `graphql_null_wrappers` | pg | Identical | Identical |
| `graphql_top_level` | pg | Identical² | Identical |
| `tenancy_postgres` | pg | Identical | View `source` only (split layout) |
| `mysql` | mysql | Identical | View `source`, plus 5 boolean defaults (`true` → `1`, `false` → `0`) |
| `tenancy_mysql` | mysql | Identical | Identical |
| `sqlite` | sqlite | Identical | View `source` only |
| `cache`, `events`, `tenancy` | sqlite | Identical | Identical |

¹ The resolver files first differed (`uuid1 "uuid"` against `"uuid"`). That was a probe
artifact: deleting `models/` also deleted the hand-maintained resolvers. A `files` run over a
fresh copy gives the same resolvers as the `database` run (`diff -r` exit 0).
² Its `graph/` is a sibling of `output.dir`. Diffed on its own, it is identical.

`diff` also listed "Only in expected: `*_test.go`" for some examples. Those are hand-written
tests, which `compareWithExpected` skips.

**Findings:**

- **P1, a defect.** The PostgreSQL introspector never reads comments on types.
  `introspectTableComment` and `introspectColumnComments` (`parser/introspect/postgres.go:820-860`)
  query `pg_description` only for `pg_class` rows. `Enum.Comment`, `CompositeType.Comment` and
  `DomainType.Comment` (`parser/schema.go`) therefore stay empty under `database` and `both`, so
  each generated type's Go doc comment falls back to its default:
  - `// OrderStatus — Lifecycle state of a customer order` becomes
    `// OrderStatus represents the order_status enum type.`
  - `// Email — RFC 5321 email address` becomes `// Email is a domain type alias for string.`

  This breaks PRD §6.1's "same generated code". The CLI parity fixture has no `COMMENT ON TYPE`,
  so nothing caught it. Fixed in N.1 (§6).
- **Expected differences, named in the PRD.**
  - View `source` (§6.1, §30.4.2). The CLI suite already masks this (`sourceParityExceptions`),
    but only for the single JSON layout. `graphql` and `tenancy_postgres` use the split layout
    (`manifest/entities/*.json`), so the mask has to cover it too.
  - MySQL stores a literal default converted to its column's type: `TRUE` reads back as `1`
    (§6.1). No mask covers this yet, because the CLI fixture has no boolean default.

**Takeaway:** apart from P1, every existing example already generates identically from a
database. Checking all of them is therefore cheap, and it covers `database` across 12
real-world schemas (§4). Part B (§5) adds the runtime coverage of `both`, which nothing has today.

## 3. Scope

**In scope:**
- **Part A:** a database twin of every `files` example (§4).
- **Part B:** three new example modules, one per dialect, under `source: both`, whose runtime
  suites run against the migrated database (§5).
- **The P1 fix** (§6).
- **Shared test infrastructure:** container helpers and parity exceptions shared by the CLI
  suite and the E2E harness.

**Out of scope:**
- New introspection behavior.
- The 28 by-source backlog lines (`docs/tracker/backlog.md`). A twin or example that trips one
  is handled by findings triage, not by widening this phase (§8 Q4).
- `go:generate` wiring for the new examples. Generating needs a live database, so it stays in
  the harness.

## 4. Part A: the database twin

### 4.1 Invariant

For every example whose `sqlgen.yml` uses `source: files`, the following must equal the
example's `expected/` tree, apart from the named parity exceptions (§4.4):
1. build a database from the example's own setup files (§4.2);
2. generate with `source: database`, using the same config apart from the source.

The example's runtime suite already runs the `files` output against a real database. Byte
identity therefore extends that suite to `source: database`, and no new runtime test is needed.

### 4.2 Database setup convention

The twin applies, in order:
1. `schema.sql`;
2. every `views/*.sql` in lexical order;
3. `after_views.sql`, when present.

An example whose `TestMain` applies anything beyond `schema.sql` and its views moves that
statement into `after_views.sql`, and the `TestMain` reads the file too, so the two cannot drift.
Today that is the unique index on each materialized view with `@pk`: `postgres`
(`order_totals`), `graphql` (`category_price_totals`) and `tenancy_postgres`
(`workspace_line_totals`).

The probe applied views in lexical order everywhere except `postgres`, where it used the
`TestMain` order. `/phase` confirms lexical order works there too, or the convention takes an
explicit order.

Statement splitting:
- PostgreSQL runs a file as one simple-protocol `Exec`.
- MySQL opens with `multiStatements=true`.
- SQLite uses one `Exec` through modernc.

### 4.3 Harness: `TestE2EDatabaseTwin` (`cmd/sqlgen/e2e_test.go`)

- **Discovery:** it reuses `discoverExamples` and takes every example whose `input.source` is
  `files` or unset.
- **Short mode:** it skips under `testing.Short()`, so `make check` and the unit CI job are
  unchanged. The integration CI job (`test.yml`, no `-short`) and `make test-integration` run it.
- **Containers:** one PostgreSQL 16 and one MySQL 8.0 container per test binary, with one
  database per example. SQLite uses a temp file. The container helpers move from
  `cli/introspect_integration_test.go` into a shared package (§4.5).
- **Generation:** it copies the example to a temp directory and rewrites:
  - the `go.mod` `replace` to an absolute path, as `TestGenerate_DatabaseSourceCompiles` does;
  - `sqlgen.yml` to `source: database`, with `connection.url` set and `paths` removed.

  It then generates there. The copy keeps a real module context for the gqlgen subprocess on the
  GraphQL examples, and it never touches the checked-in `models/`.
- **Comparison:** it compares the output (and any sibling graph directory, through the existing
  `siblingGraphDirs`) with `expected/` after normalizing both sides. On failure it prints a
  unified diff per file. It never writes goldens: `-update-e2e` does not apply to this test.

**Cost:** in the probe, a generation took about 1 s on the plain examples and about 8 s on the
GraphQL-chained ones, so roughly 1-2 minutes per integration run, plus container start-up shared
with the rest of the package.

### 4.4 Parity exceptions

The exceptions move out of the CLI test into the shared package. Each one names a PRD
sentence, and each has a test that fails when it is removed, so the list cannot grow silently.

| Exception | PRD | Change in this phase |
|---|---|---|
| View `source` | §6.1, §30.4.2 | Extend `dropViewSource` to the split JSON layout (`manifest/entities/<view>.json`). |
| MySQL literal default stored converted | §6.1 ("`TRUE` as `1`") | **New.** Masks a manifest `default` only where the files value and the database value are the same literal by the §6.1 rule. A blanket mask is not allowed. |

### 4.5 Shared test package

`cmd/sqlgen/internal/livedb` (new, imported only by tests) holds:
- the container starters (`postgresAddr`, `mysqlServerAddr`, and their URL and DSN builders);
- a per-test database creator;
- the setup applier (§4.2);
- the parity exceptions and `NormalizeTree`.

The CLI suite and the E2E harness both import it. `testcontainers-go` is already a requirement
of `cmd/sqlgen/go.mod`. A non-test package importing it does not reach the `sqlgen` binary,
which never imports `internal/livedb`. Sharing rather than duplicating is the Q3 ruling.

## 5. Part B: `source: both` examples

### 5.1 Modules

There are three new modules, one per dialect (Q2 ruling):
- `introspect_postgres` (pgx);
- `introspect_mysql` (stdlib);
- `introspect_sqlite` (stdlib, modernc).

They follow `guidelines/TESTING.md` §10. A new example is warranted because `both` "spans the
whole pipeline", and no existing example can carry `source: both` without changing what it
covers today.

```
introspect_postgres/
├── go.mod, go.sum
├── sqlgen.yml          # source: both, paths: [./migrations/], views: [./views/]
├── schema.sql          # the database's current schema, applied by the harness and TestMain
├── views/              # full view DDL plus annotations (@pk); applied to the database
├── after_views.sql     # optional (§4.2)
├── migrations/         # pending migrations: read by sqlgen; the harness applies them only to the migrated database (§5.2)
├── expected/
├── models/
└── tests/              # TestMain applies schema.sql, views/, after_views.sql, then migrations/
```

`sqlgen.yml` reads its connection from a reference, so the example also exercises §4.1.1:

```yaml
input:
  dialect: postgres
  source: both
  connection:
    url: ${SQLGEN_EXAMPLE_DATABASE_URL}
  paths:
    - ./migrations/
  views:
    - ./views/
```

### 5.2 The three-way invariant

The harness builds two databases for each `both` example:
- **Base:** setup only.
- **Migrated:** setup, then `migrations/` in file order.

It then asserts the following, with every comparison normalized by §4.4:

| Source | Generated against | Must equal |
|---|---|---|
| `both` | Base database, with `migrations/` as the overlay | `expected/`, byte for byte. This is the golden. |
| `database` | Migrated database | `expected/` |
| `files` | `paths: [schema.sql, migrations/]`, with `views/` | `expected/` |

The three rows state PRD §6.1's use case as a test. It also follows that a pending migration in
an example must not be one where the PRD lets the sources legitimately differ. Two examples:
- a pending change to a view, where the database's view stands;
- a column the PRD documents as differing by source.

Those cases stay in the CLI suite.

### 5.3 Overlay shapes

The pending migrations cover the PRD §6.1 merge table on each dialect, as far as the dialect's
`ALTER` allows. `/phase` fixes the exact DDL.

| Shape | pg | mysql | sqlite | What the runtime suite checks |
|---|:-:|:-:|:-:|---|
| `ALTER TABLE … ADD COLUMN` (nullable; `NOT NULL DEFAULT`) | ✓ | ✓ | ✓ | The field round-trips through Create, Get and Update. |
| `ALTER TABLE … DROP COLUMN` | ✓ | ✓ | ✓ | Create succeeds against the migrated table. A compile-time absence check is not needed. |
| `ALTER TABLE … ADD CONSTRAINT … FOREIGN KEY` | ✓ | ✓ | — (no `ADD CONSTRAINT`; covered by `ADD COLUMN … REFERENCES`) | The FK-inferred relationship loads. |
| `ALTER TABLE … ADD CONSTRAINT … UNIQUE` | ✓ | ✓ | — (`CREATE UNIQUE INDEX`) | The manifest lists the index under the database's name. Upsert on it, if eligible. |
| `CREATE TABLE` with an FK into an introspected table | ✓ | ✓ | ✓ | CRUD works, and the relationship into the database-only table loads (the FIX-243 ordering). |
| `DROP TABLE` | ✓ | ✓ | ✓ | No entity is generated. Assert it in the manifest. |
| A new enum used by an added column (`CREATE TYPE`; MySQL inline `ENUM`) | ✓ | ✓ | — | The value round-trips, and an invalid value is rejected by the database. |
| `ALTER COLUMN … SET NOT NULL` / `MODIFY` on a column a view selects | ✓ | ✓ | — | The view's field becomes non-nullable. Only valid where the database allows the change under a view. |

The base schema also carries one representative of each introspection-only fact for its
dialect, so the example covers them in a runtime suite:
- **PostgreSQL:** identity, domain, composite, a cross-schema FK, comments on tables, columns
  and types (P1), a generated column, and a materialized view with `@pk`.
- **MySQL:** `unsigned`, `SET`, a generated column, comments, and a boolean literal default
  (the §4.4 exception).
- **SQLite:** a rowid alias, a composite PK in non-column order, a generated column, and a
  partial index.

Each base schema stays at about six tables. The goal is source coverage, not feature coverage;
feature coverage is what the dialect examples are for.

### 5.4 Golden harness changes for `both` examples

`TestE2EGoldenFiles` learns the source of each example:

| Mode | A `both` example |
|---|---|
| Default (integration) | Builds the base database (§5.2), generates under `both` and compares with `expected/` byte for byte, then runs the other two rows of §5.2. |
| `-short` | Skips, as every example does: `TestE2EGoldenFiles` skips under `-short`, so golden drift is caught in the integration job, not the unit job (ruled 2026-10-11). |
| `-update-e2e` | Needs Docker, since it builds the base database to regenerate the golden. `make update-golden-e2e` therefore needs Docker whenever a `both` example exists, as `check-examples` already does. |

`discoverExamples` needs no change. The examples CI workflow (`examples.yml`) discovers the new
modules from their `go.mod`.

## 6. The P1 fix: PostgreSQL type comments

- **Introspector:** read `pg_description` for `pg_type` objects (`classoid = 'pg_type'::regclass`,
  `objsubid = 0`), and set `Comment` on each `Enum`, `CompositeType` and `DomainType` it builds.
  - One query per run, keyed by schema and type name. This avoids a query per type.
  - A domain's comment is stored on its `pg_type` row too, so one query covers all three kinds.
- **Parity fixture:** add `COMMENT ON TYPE` for an enum and a composite, and `COMMENT ON DOMAIN`,
  to the shared probe fixture, which `assertSchemaParity` then pins. The CLI's
  `TestGenerate_DatabaseSourceMatchesFiles` fixture gets its own type comments.
- **Fail-first:** revert the query, and the parity test fails on `Comment`.
- **Other dialects:** MySQL and SQLite have no named types (PRD §6.3), so the fix is
  PostgreSQL only.

Part A makes `postgres` and `postgres_stdlib` byte-identical once this lands. A twin red on P1
is therefore the regression test at example scale.

## 7. PRD and guideline impact

- **PRD:** no behavior change. PRD §6.3's "Comments: `pg_description`" row already covers type
  comments. P1 is the code falling short of it, not a spec gap. §8.1.7, under "E2E example
  projects", gains one sentence: every example is also generated from a live database and must
  match its goldens, and the `both` examples check the three-way invariant (§5.2).
- **`guidelines/TESTING.md` §10:**
  - add the `introspect_*` modules to the Layout tree;
  - document the setup convention (§4.2);
  - Running table: `TestE2EGoldenFiles` needs Docker for `both` examples outside `-short`, and
    `make update-golden-e2e` needs Docker;
  - add `TestE2EDatabaseTwin`.
- **`docs/configuration.md`:** no change.

## 8. Questions

All four were ruled on 2026-10-11, each matching its recommendation. No question is open.

| # | Question | Options and consequences | Ruling |
|---|---|---|---|
| Q1 | How wide is the Part A twin? | **(a) All 12 `files` examples.** About 1-2 minutes per integration run; every schema in the repo becomes `database` coverage. **(b) Only `postgres`, `mysql` and `sqlite`.** About 20 seconds, but it misses the split manifest layout, tenancy views, and GraphQL-chained generation. **(c) None:** rely on Part B alone. | **(a), all 12.** The probe shows it is nearly free, and P1 shows it finds real defects. |
| Q2 | How many `both` example modules? | **(a) Three, one per dialect.** Each dialect's file parser runs the overlay merge against its own introspected seed, and the `ALTER` support differs by dialect (§5.3). **(b) PostgreSQL only.** One module; MySQL and SQLite keep `both` covered only by the CLI fixtures. | **(a), three modules.** It follows the `tenancy_*` precedent. Each module is small (§5.3). |
| Q3 | Share the container helpers and parity exceptions? | **(a) A new `cmd/sqlgen/internal/livedb` package**, used by the CLI suite and the E2E harness. **(b) Duplicate them into `e2e_test.go`.** Two exception lists can then drift, and the twin's masks stop matching the CLI's. | **(a), shared `internal/livedb`.** |
| Q4 | What happens when the twin or a `both` example trips a backlog divergence? | **(a) Findings triage.** A measured failure becomes a candidate FIX; while a FIX is open, a schema change that avoids the case is allowed only with a backlog line naming it. **(b) Fix every divergence the phase touches inside the phase.** | **(a), findings triage.** It keeps the phase short and follows the anti-waterfall rule. The probe found only P1, so the risk is low. |

## 9. Sub-item preview

This list is provisional; `/phase` produces the authoritative breakdown. Closure is
`/close-phase`, with no sub-item of its own.

**Dependencies:**
- N.0 blocks everything.
- N.1 and N.2 run in parallel.
- N.3 depends on N.1 and N.2.
- N.4 depends on N.2, and its three modules run in parallel.

### N.0 PRD and guideline sync
- The §7 edits. No code.

### N.1 PostgreSQL type comments (P1)
- §6: the introspector query, the parity fixture rows, and the CLI fixture rows.

### N.2 Shared live-database test package
- §4.5: move the container helpers, the setup applier and the parity exceptions into
  `cmd/sqlgen/internal/livedb`, and point the CLI suite at it with no behavior change.
- Extend the view-`source` mask to the split layout, and add the MySQL literal-default exception
  with its own fail-first test (§4.4).

### N.3 Database twin
- §4.3: `TestE2EDatabaseTwin` over every `files` example.
- The `after_views.sql` convention, and the `postgres` example's `TestMain` change (§4.2).
- Green on all 12 examples.

### N.4 `both` examples
- §5: `introspect_postgres`, `introspect_mysql` and `introspect_sqlite`.
- Base schema, migrations, views and runtime suites.
- The `TestE2EGoldenFiles` source awareness (§5.4) and the three-way invariant (§5.2).

## 10. Definition of done

1. `TestE2EDatabaseTwin` passes for every `files` example, with only the §4.4 exceptions applied.
2. Each `introspect_*` example's `both` output equals its `expected/` byte for byte, and its
   `database` and `files` twins equal it after normalization.
3. Each `introspect_*` runtime suite passes against the migrated database, and covers every
   shape in the §5.3 table that is ✓ for its dialect.
4. P1 is fixed, with a parity test that failed first.
5. Every parity exception has a test that fails without it.
6. `make check`, `make check-examples` and `make test-integration` are green.
