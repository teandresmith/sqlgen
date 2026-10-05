# Phase 21: PostgreSQL Materialized Views

Status: Complete (closed 2026-07-13)
PRD Sections: 16 (Views — extended by proposed §16.5), 16.4 (Generated Artifacts capability table), 4.9 (ViewConfig), 27 (Caching — refresh invalidation), 30/31 (Manifest/MCP surface)

> **Normative spec:** PRD §16.5 — authoritative for this phase (all six design questions resolved: Q1 Refresh + RefreshConcurrently always generated; Q2 introspection + annotation-file discovery; Q3 unique-index PK discovery + `@pk` override; Q4 refresh always invalidates own cache; Q5 `RefreshConcurrently` hard-errors in a transaction; Q6 manifest advertises the capability, no freeze coordination). PRD §16.5 is drafted **from** this doc by 21.6.

> **Rationale:** Postgres materialized views are invisible to today's introspection (`information_schema.views` excludes them — they live in `pg_matviews`), so they currently get no generated client. This phase discovers them and generates a read-only client **identical** to the regular-view surface, plus the one verb a matview adds — `Refresh` / `RefreshConcurrently`. Reads query the matview by name (already optimal — scans precomputed rows), so the read path reuses the entire existing view pipeline unchanged.

> **Scope rule:** PostgreSQL-only. Reuses `parser.View` (+ two flags), `ViewConfig`, and `templates/view/*`. No new schema-object type; no runtime / parser-DDL churn for MySQL/SQLite. Out of scope: `CREATE MATERIALIZED VIEW` DDL emission, index management, auto-refresh scheduling, incremental refresh, staleness detection.

> **Depends on:** Phase 16 (view surface, stable).

---

## 21.1 Data model + Postgres introspection (`pg_matviews` + unique-index PK)

**PRD Reference:** §16.2 (introspected views — extended to matviews)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] Add `Materialized bool` and `ConcurrentlyRefreshable bool` to `parser.View` (`parser/schema.go`), with doc comments per design §2. Preserve the invariant `!Materialized ⇒ !ConcurrentlyRefreshable`.
- [x] Add `introspectMaterializedViews` to `parser/introspect/postgres.go`: query `pg_matviews` (excluding `pg_catalog` / `information_schema`), apply `schemaAllowed` / `tableAllowed`, append to `schema.Views` with `Materialized: true`; populate `View.SQL` from `pg_matviews.definition` (documentation only — never re-injected at query time).
- [x] Reuse the existing `introspectViewColumns` query for matview columns (it joins `pg_attribute`/`pg_class` and already matches `relkind = 'm'`); reuse `normalizePostgresType` verbatim.
- [x] Add unique-index discovery: query `pg_index` for the matview (`indisunique`, excluding partial `indpred IS NULL` and expression `indexprs IS NULL` indexes); select the PK index deterministically (fewest columns, then index name ascending); set `Column.PrimaryKey = true` + `Nullable = false` on its columns; set `ConcurrentlyRefreshable = true` when ≥1 qualifying unique index exists.
- [x] Wire `introspectMaterializedViews` into the Postgres introspection driver alongside `introspectViews`.

### Acceptance Criteria

- A Postgres materialized view is discovered and appears in `schema.Views` with `Materialized == true` and accurate column names/types; a regular view still appears with `Materialized == false`.
- A matview with a single-column unique index has that column marked `PrimaryKey` (→ `HasPK`) and `ConcurrentlyRefreshable == true`; a matview with no qualifying unique index has no PK column and `ConcurrentlyRefreshable == false`.
- PK-index selection is deterministic: given multiple unique indexes, the one with the fewest columns (ties broken by index name) is chosen, reproducibly.
- Partial and expression unique indexes are ignored for PK selection.
- MySQL/SQLite introspection is byte-unchanged (they cannot surface matviews).

### Tests Required

- [x] Unit: `pg_matviews` row → `View{Materialized:true}` with columns resolved via the shared column query. _(Covered against a real Postgres in `TestPostgresIntrospectMaterializedViews` per TESTING.md §1 "test against real databases"; the pure selection logic is unit-tested in `postgres_test.go`.)_
- [x] Unit: unique-index discovery → correct PK columns + `ConcurrentlyRefreshable`; deterministic selection across multiple candidate indexes (fewest cols, then name); partial/expression indexes excluded. _(`TestChooseMatviewPK` table-driven unit test + integration assertions.)_
- [x] Unit: no unique index ⇒ no PK, `ConcurrentlyRefreshable == false`. _(`mv_plain` leg of the integration test — its only unique indexes are partial/expression, both excluded.)_
- [x] Integration (Testcontainers Postgres): create base tables + `CREATE MATERIALIZED VIEW` (+ unique index) → introspect → assert `View` flags, columns, and discovered PK.

### Completion Record

**Completed 2026-07-12.** Files changed:
- `parser/schema.go` — `View` gained `Materialized` + `ConcurrentlyRefreshable` bool fields with design-§2 doc comments; struct doc widened to "regular or materialized".
- `parser/introspect/postgres.go` — new `introspectMaterializedViews` (queries `pg_catalog.pg_matviews`, applies `schemaAllowed`/`tableAllowed`, populates `View.SQL` from `definition`, reuses `introspectViewColumns` + `normalizePostgresType`); new `introspectMatviewUniqueIndexes` (`pg_index` with `indisunique AND indisvalid AND indisready AND indpred IS NULL AND indexprs IS NULL`, key columns only via `k.ord <= indnkeyatts`); pure `chooseMatviewPK` (fewest columns, then name ascending); wired into `Introspect` after `introspectViews`.
- `parser/introspect/postgres_test.go` — new: table-driven `TestChooseMatviewPK` (5 cases incl. tie-break + input-order independence).
- `parser/introspect/introspect_integration_test.go` — new `TestPostgresIntrospectMaterializedViews`: 2 matviews + 1 regular view; 3 competing unique indexes pin deterministic PK selection; partial + expression unique indexes pinned as non-qualifying; PK column `Nullable == false` asserted; regular-view flags asserted false.

Notes: MySQL/SQLite introspection byte-untouched. `INCLUDE` payload columns are excluded from the PK via `indnkeyatts` (design doc implies key columns; documented in a code comment). Integration test expectation for `sum(numeric(10,2))` is unconstrained `numeric` (Postgres aggregate widening). `make check` green (all 8 modules); `make check-examples` green (parser path trigger); targeted matview integration test green.

---

## 21.2 Annotation-file parser (`CREATE MATERIALIZED VIEW`)

**PRD Reference:** §16.1 (annotation files), §16.3 (type-resolution chain)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] Extend the Postgres view-SQL parser (`parser/postgres/view_sql.go`, invoked via the `viewSQLParser` in `parser/cli` / `cmd/sqlgen/cli/pipeline.go`) to accept `CREATE MATERIALIZED VIEW [schema.]name AS SELECT …`, setting `View.Materialized = true`.
- [x] Reject `CREATE OR REPLACE MATERIALIZED VIEW` as a parse error (not valid PostgreSQL).
- [x] Hard-error on `CREATE MATERIALIZED VIEW` under a non-Postgres `input.dialect`.
- [x] Keep all existing view directives working unchanged (`@pk`, `@type`, `@nullable`) and the §16.3 resolution chain; for annotation-file matviews, `@pk` present ⇒ PK columns **and** `ConcurrentlyRefreshable = true`; `@pk` absent ⇒ no `Get`, `ConcurrentlyRefreshable = false`.
- [x] Ensure a `CREATE MATERIALIZED VIEW` encountered in `input.paths` (DDL/migration files) is skipped gracefully with a warning (same rule as regular views).

### Acceptance Criteria

- A `CREATE MATERIALIZED VIEW` annotation file in `input.views` under `dialect: postgres` parses to a `View{Materialized:true}` with columns and directives resolved identically to a regular view annotation file.
- `@pk` on an annotation-file matview marks the PK columns (→ `Get`) and sets `ConcurrentlyRefreshable = true`; without `@pk`, only `GetMany`/`Count`/`Paginate`/`Connection` are generated and `RefreshConcurrently` is not.
- `CREATE OR REPLACE MATERIALIZED VIEW` is a parse error; `CREATE MATERIALIZED VIEW` under `dialect: mysql`/`sqlite` is a hard error; the same statement in `input.paths` is skipped with a warning (no error, no view entry).

### Tests Required

- [x] Unit: annotation-file `CREATE MATERIALIZED VIEW` (schema-qualified and bare) → `View{Materialized:true}` with correct columns. _(`TestParseViewSQL_Materialized` table-driven; `TestParseViewSQL_RegularViewNotMaterialized` + `TestParseViewSQL_CreateTableAsNotAView` pin the negatives.)_
- [x] Unit: `@pk` on a matview annotation file → PK columns + `ConcurrentlyRefreshable == true`. _(`TestBuildView_Materialized` both branches; `TestBuildView_RegularViewNeverConcurrentlyRefreshable` pins the invariant.)_
- [x] Unit: `CREATE OR REPLACE MATERIALIZED VIEW` → parse error. _(`TestParseViewSQL_OrReplaceMaterializedRejected` — pg_query grammar rejects it natively.)_
- [x] Unit: `CREATE MATERIALIZED VIEW` under mysql/sqlite dialect → hard error. _(`TestParseViewSQL_MaterializedRejected` in both packages, incl. leading-annotation-comment and lowercase cases.)_
- [x] Unit: `CREATE MATERIALIZED VIEW` in `input.paths` → skipped with warning. _(`TestCreateMaterializedViewSkippedGracefully`.)_

### Completion Record

**Completed 2026-07-12.** Files changed:
- `parser/view.go` — `ViewSQL` gained `Materialized bool`; `BuildView` threads it into `View.Materialized` and derives `ConcurrentlyRefreshable = Materialized && @pk present` (design §3.3); `mergeView` now implements the §3.2 override rule: a non-empty annotation `@pk` **clears** discovered PK columns before applying (explicit user intent replaces, not unions), and on a matview flips `ConcurrentlyRefreshable = true`; overlays without `@pk` preserve discovered PK/capability.
- `parser/postgres/view_sql.go` — `ParseViewSQL` accepts `CreateTableAsStmt` with `OBJECT_MATVIEW` (how pg_query represents `CREATE MATERIALIZED VIEW`) via new `parseMatviewStmt`, reusing `extractResTargets`/`extractFromAliases`; plain `CREATE TABLE AS` still falls through to "no CREATE VIEW statement found"; `CREATE OR REPLACE MATERIALIZED VIEW` fails naturally at the pg_query grammar (pinned by test).
- `parser/postgres/postgres.go` — DDL pass1 warns-and-skips `CREATE MATERIALIZED VIEW` in `input.paths` (mirrors the regular-view rule verbatim); plain `CREATE TABLE AS` keeps its pre-existing silent skip.
- `parser/mysql/view_sql.go`, `parser/sqlite/view_sql.go` — clear dialect hard-error ("not supported for dialect X: materialized views are PostgreSQL-only") via a small `createsMaterializedView` pre-check that skips leading annotation comments, instead of an opaque vitess/rqlite syntax error.
- Tests: `parser/postgres/postgres_test.go` (5 new), `parser/mysql/mysql_test.go` + `parser/sqlite/sqlite_test.go` (1 table-driven each), `parser/view_test.go` (4 new incl. merge-overlay override + invariant).

Notes: the §3.2 `@pk`-overrides-discovered-PK rule required a real behavior change in `mergeView` (previously union semantics — harmless for regular views which never carry a discovered PK, wrong for matviews); implemented clear-then-apply + the concurrent-refresh assertion, both unit-pinned. `make check` green (8 modules); `make check-examples` green (parser path trigger).

---

## 21.3 SQL builder + refresh template + transaction guard

**PRD Reference:** proposed §16.5 (refresh semantics); §18 (transaction injection — guard interaction)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] Add `sql.BuildRefreshMaterializedView(dialect, table, concurrently bool) string` — emits `REFRESH MATERIALIZED VIEW [CONCURRENTLY] <FormatTable(t)>` with identifiers quoted via `Dialect.FormatTable` (no string interpolation of identifiers).
- [x] Add `hook.OpRefresh` so refreshes flow through the query-hook chain + panic handler, matching read ops.
- [x] Add sentinel `ErrRefreshConcurrentlyInTx`; `RefreshConcurrently` calls `database.InTransaction(ctx)` and returns the sentinel **before** issuing any SQL when a transaction is present. `Refresh` has no such guard (it is transaction-safe).
- [x] Add `templates/view/refresh.go.tmpl` (`define "view/refresh"`), included only when `.Materialized`; emit `Refresh` always and `RefreshConcurrently` only when `.ConcurrentlyRefreshable`. Add both methods to the client interface + implementation; error-wrap as `fmt.Errorf("refresh %s: %w", viewName, err)`.
- [x] Thread `.Materialized` / `.ConcurrentlyRefreshable` through `ViewContext` in `cmd/sqlgen/gen/context_view.go` and include the refresh template in the view render path.
- [x] Method doc comments state the locking/transaction caveats verbatim from design §5.1 (`Refresh` = ACCESS EXCLUSIVE lock, tx-safe; `RefreshConcurrently` = non-blocking, requires unique index, cannot run in a transaction).

### Acceptance Criteria

- A materialized view generates `Refresh(ctx) error`; when `ConcurrentlyRefreshable`, it also generates `RefreshConcurrently(ctx) error`. A regular view generates neither (byte-identical to today's view output).
- Generated SQL is exactly `REFRESH MATERIALIZED VIEW "schema"."name"` and `REFRESH MATERIALIZED VIEW CONCURRENTLY "schema"."name"` (schema omitted correctly when empty), with quoting through `FormatTable`.
- `RefreshConcurrently` invoked with an ambient transaction returns `ErrRefreshConcurrentlyInTx` and issues no SQL; outside a transaction it proceeds.
- Refresh methods run through the hook chain (observable as `OpRefresh`) and are panic-wrapped.

### Tests Required

- [x] Unit (SQL builder): `BuildRefreshMaterializedView` for schema-qualified + bare names, concurrently on/off. _(`TestBuildRefreshMaterializedView`, 6 cases incl. mysql/sqlite quoting determinism.)_
- [x] Golden (codegen): matview client golden — read surface matches the view golden, plus `Refresh` + `RefreshConcurrently`; a second golden where no unique index ⇒ `Refresh` only. _(`view_matview_client_gen.go`, `view_matview_refresh_gen.go`, `view_matview_refresh_plain_gen.go`.)_
- [x] Unit: `RefreshConcurrently` with a transaction in `ctx` → `ErrRefreshConcurrentlyInTx`, no SQL issued (assert via a counting/fake querier). _(Template-shape leg landed here per the Phase 15 LockMode-guard precedent: `TestViewRefreshTemplate_txGuardPrecedesExecution` pins guard-before-executeQuery + sentinel-before-SQL ordering in the emitted code; the behavioral fake-querier/live leg is 21.5's integration item "RefreshConcurrently inside a transaction returns ErrRefreshConcurrentlyInTx", which compiles and runs the generated client.)_
- [x] Golden: regular-view output is byte-unchanged (no accidental refresh emission). _(Pre-existing view goldens pass untouched; `TestViewRefreshTemplate_regularViewEmitsNothing` + `TestViewClientTemplate_matviewInterface` negative branch pin it.)_

### Completion Record

**Completed 2026-07-12.** Files changed:
- `sql/builder.go` — `BuildRefreshMaterializedView(d, t, concurrently) string`; identifier via `FormatTable`, no args (statement carries no values).
- `hook/hook.go` — `OpRefresh QueryOp = "refresh"` with doc comment (query-op family; refreshes flow through the query-hook chain + panic handler).
- `database/errors.go` — sentinel `ErrRefreshConcurrentlyInTx`.
- `cmd/sqlgen/gen/templates/view/refresh.go.tmpl` — new `view/refresh` template, whole body gated on `.Materialized`; `RefreshConcurrently` additionally on `.ConcurrentlyRefreshable`; §5.1 caveats in doc comments; guard checks `database.InTransaction(ctx)` and returns the wrapped sentinel before `executeQuery` (fail-fast, no SQL, hooks not invoked on the guarded path); errors wrap as `refresh <view>: %w`.
- `cmd/sqlgen/gen/templates/view/client.go.tmpl` — interface gains `Refresh`/`RefreshConcurrently` entries under the same gates.
- `cmd/sqlgen/gen/context.go` + `context_view.go` — `ViewContext.Materialized`/`.ConcurrentlyRefreshable` threaded from `parser.View`; design-§2 invariant assertion (panic on `!Materialized && ConcurrentlyRefreshable` — parser upholds it on all discovery paths, so violation = hand-built fixture bug).
- `cmd/sqlgen/gen/orchestrate.go` — `view/refresh` added to `viewTemplateNames` (template self-gates; empty output skipped).
- Tests: `sql/builder_test.go` (+1 table-driven), `gen/context_test.go` (+3: flags threaded, regular view unchanged, invariant panic), `gen/view_test.go` (+9 incl. 3 goldens, guard-ordering, empty-for-regular-view, compile-clean both shapes; shared `compareGolden` helper extracted).

Notes: `Refresh`/`RefreshConcurrently` signatures are exactly `(ctx context.Context) error` per design §5 (no CallOptions — refresh takes no input and returns no rows); the tx-guard runs before the hook chain (design "fail fast before issuing any SQL" reading; hooks observe only refreshes that reach execution). `make check` green; `make check-examples` green (no example output change — no matview in examples until 21.5).

---

## 21.4 Cache invalidation + manifest/MCP surface

**PRD Reference:** §27 (caching — view cache), §27.11 (invalidation), §30 (manifest), §31 (MCP)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] On success, both `Refresh` and `RefreshConcurrently` invalidate this matview's own cache entries (no-op when the matview is not cached). Orthogonal to `invalidate_on` (which is other-tables'-mutation-driven); refresh-invalidation is self-triggered by the refresh method.
- [x] Add a `materialized: true` marker to the matview's manifest entry and advertise the `Refresh` / `RefreshConcurrently` methods (additive schema change; no version-freeze coordination — pre-release).
- [x] Ensure matview methods surface in the MCP read-only tool set consistently with views (they already flow via `schema.Views`); the refresh capability is discoverable through the manifest marker.

### Acceptance Criteria

- A cached matview served a read, then refreshed, does not return the pre-refresh cached rows (cache entries dropped on refresh success).
- An uncached matview's refresh performs no cache operations (pure no-op) and still succeeds.
- The manifest entry for a matview carries `materialized: true` and lists the refresh methods; a regular view's manifest entry is unchanged.

### Tests Required

- [x] Unit/E2E: read (populates cache) → `Refresh` → subsequent read misses the stale cache (fresh rows). Uncached matview: `Refresh` issues no cache calls. _(Unit legs here: `TestCacheTemplate_emitsRefreshInvalidation` pins the OpRefresh interception, invalidate-after-success ordering, and tx-deferral emission; `TestCacheTemplate_refreshNoCachedViewsIsNoOp` pins the unconditional-false `isCachedView` (pure no-op). The live read→refresh→fresh-rows E2E leg rides 21.5's Postgres example/integration work where a generated matview client exists.)_
- [x] Unit: manifest builder emits `materialized: true` + refresh methods for a matview; regular-view manifest entry byte-unchanged. _(`TestBuild_MaterializedViewEntity` — both-methods, refresh-only, and regular-view cases with exact REFRESH SQL bodies + `ErrRefreshConcurrentlyInTx` error advertisement; `TestBuild_MaterializedMarkerJSONShape` pins `"materialized":true` present / key absent via omitempty.)_
- [x] Manifest/MCP doc-consistency check: matview surface reflected in the manifest and MCP tool output. _(`TestGetEntity_MaterializedViewMarker` — marker on full + compact output, absent on regular views; `TestFindMethod_MatviewRefreshMethods` — refresh methods discoverable exactly + fuzzy.)_

### Completion Record

**Completed 2026-07-13.** Files changed:
- `cmd/sqlgen/gen/templates/cache.go.tmpl` — `QueryHook` intercepts `hook.OpRefresh`: runs the refresh, and on success calls new `dispatchRefresh` which no-ops unless `isCachedView(table)`, defers the clear via `tx.OnCommit` inside a transaction (same repopulation race the mutation path defers for, CACHE.md §14.3), and otherwise fires `InvalidateTable` (pattern-based, clears every entry incl. orphaned fingerprints); invalidation errors route through the standard cache error path and never fail the refresh. `isCachedView` degrades to unconditional `false` with no cached views.
- `cmd/sqlgen/manifest/types.go` + `builder.go` + `sql.go` — `Entity.Materialized bool` (`json:"materialized,omitempty"`); `buildViewEntity` sets it; `buildViewMethods` appends mutation-side `Refresh` (+`RefreshConcurrently` with `Errors: ["ErrRefreshConcurrentlyInTx"]`) with canonical SQL bodies via new `sqlRefreshView` (runtime `sql.BuildRefreshMaterializedView`). Placement rationale: refresh mutates the stored rows, so it advertises on the mutation list even though the runtime routes it through the query-hook chain.
- `cmd/sqlgen/manifest/schema/v1.json` — entity gains the documented `materialized` boolean property (additive; pre-release schema moves freely per design Q6).
- `manifest/types.go` (runtime module) — mirrored `Materialized` field so the MCP store's unmarshal does not drop the marker. **Security-review note:** this runtime change is one additive JSON-tagged bool with no parsing logic; the path-based security review was deferred to `/verify 21.4` — flagged there explicitly.
- Regenerated goldens: `cache`/`graphql`/`tenancy` example modules' `cache_gen.go` (expected/ + models/) picked up the refresh-invalidation emission.
- Tests: `gen/cache_template_test.go` (+2), `cmd/sqlgen/manifest/builder_test.go` (+2), `cmd/sqlgen/mcp/tools_entity_test.go` (+2 with a matview-extended fixture store).

Notes: refresh-invalidation is deliberately hook-side (cache is wired as a `QueryHook`), so the 21.3 refresh template needed zero changes; a matview client with no cache wiring runs the same code path and no-ops. `make check` green; `make update-golden-e2e` + `make update-golden` + `make check-examples` green. **Verify-pass findings resolved inline pre-`/done` (0 FIXes logged):** manifest method `Notes` gained the "(no-op when the view is not cached)" caveat (both refresh methods); the §27.11 `invalidate_on`-still-required observation was routed to 21.6's task list as an explicit PRD-wording input (design-intentional behavior, needs stating in §16.5/§27.11).

---

## 21.5 Integration (Testcontainers Postgres) + example module

**PRD Reference:** §16 (end-to-end view behavior), §18 (transactions)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] Postgres integration test (Testcontainers, gated by `testing.Short()`): create base tables → `CREATE MATERIALIZED VIEW` (+ unique index) → introspect → generate → compile → exercise the full surface. _(Realized compositionally per the established view-test architecture: the **introspect** leg is 21.1's `TestPostgresIntrospectMaterializedViews` (Testcontainers; asserts the exact `parser.View` shape codegen consumes); the **generate → compile → exercise** legs are the `postgres` example module (annotation path — produces the identical `parser.View` shape) whose `tests/` leg runs the compiled client against a Testcontainers Postgres. No introspect-driven generate-compile harness exists for any schema object (introspection is not yet CLI-wired); matviews follow the same pattern as regular views (16.x)._
- [x] Cover reads: `GetMany` with filter + sort, `Get` by the discovered PK, `Count`, `Paginate`, `Connection`. _(`TestMatviewReadSurface` — all five methods incl. keyset Connection paging via view-config `cursor_keys`.)_
- [x] Cover refresh: mutate base tables, assert the matview still returns pre-refresh rows, then `Refresh` and assert rows update; `RefreshConcurrently` succeeds with the unique index; `RefreshConcurrently` inside a transaction returns `ErrRefreshConcurrentlyInTx`. _(`TestMatviewRefreshObservesBaseTableChanges` — `ErrNotFound` on the stale snapshot, fresh totals post-refresh; `TestMatviewRefreshConcurrently`; `TestMatviewRefreshConcurrentlyInTransaction` — sentinel via `errors.Is` inside `database.WithTransaction`, plus the plain-`Refresh`-is-tx-safe positive path. This closes the behavioral tx-guard leg deferred from 21.3 and the live refresh-semantics leg deferred from 21.4.)_
- [x] Extend the `postgres` example module (`cmd/sqlgen/testdata/examples/postgres/`) with a materialized view (schema + `sqlgen.yml` + regenerated `expected/` goldens + a `tests/` leg) so `make check-examples` exercises the generated matview client end to end.

### Acceptance Criteria

- The generated matview client compiles and all read methods return correct results against a real Postgres matview.
- `Refresh` demonstrably updates the matview's rows after a base-table change; a read before refresh returns the stale snapshot (proving matview semantics), a read after refresh returns the new data.
- `RefreshConcurrently` succeeds against the unique-indexed matview and hard-errors (`ErrRefreshConcurrentlyInTx`) inside a transaction.
- `make check-examples` passes with the new `postgres`-example matview; goldens are byte-clean.

### Tests Required

- [x] Integration: full read surface (`Get`/`GetMany` filter+sort/`Count`/`Paginate`/`Connection`) against a Postgres matview.
- [x] Integration: `Refresh` row-change observation (stale-before / fresh-after).
- [x] Integration: `RefreshConcurrently` success (unique index) + in-transaction hard-error path.
- [x] Example: `postgres` module regenerates byte-clean and its matview `tests/` leg passes under `make check-examples`.

### Completion Record

**Completed 2026-07-13.** Files changed (all under `cmd/sqlgen/testdata/examples/postgres/`):
- `views/order_totals.sql` — new annotation-file matview (`-- @pk: product_id`, `-- @type total_quantity: int64`; groups `order_items` by product). `@pk` ⇒ `Get` + `RefreshConcurrently` per design §3.3.
- `sqlgen.yml` — new `views:` block: `order_totals.cursor_keys: [product_id]` (enables `Connection`; exercises ViewConfig-applies-to-matviews, design §6).
- `tests/main_test.go` — container setup executes the annotation file directly (it is valid SQL) + `CREATE UNIQUE INDEX order_totals_product_id_idx` (the `@pk` assertion's backing index, the `CONCURRENTLY` precondition).
- `tests/matview_test.go` — new: 4 tests covering stale-before/fresh-after refresh semantics, the full read surface, concurrent refresh success, and both tx paths (sentinel + tx-safe plain refresh); reuses `seedProductForView`.
- Regenerated `models/` + `expected/`: `views_gen.go` (OrderTotal client with `Refresh`/`RefreshConcurrently`), `client_gen.go`, `tablenames_gen.go`, `manifest/manifest_gen.json` (+`order_total.md`, `_index.md`) carrying `materialized: true` + refresh methods, `AGENTS.md`/`CLAUDE.md`.

Notes: the manifest/MCP surface delta flowed through the example's existing `manifest_test.go`/`mcp_test.go` legs untouched and green. All 4 new tests passed first run (`GOWORK=off go test -race`, ~5s once the container is warm). `make check` + `make check-examples` green. This sub-item closes the two deferred behavioral legs (21.3 tx-guard, 21.4 live refresh-invalidated reads — note the example module has no cache wiring, so the cache-invalidation behavior remains covered by 21.4's template tests plus the refresh-semantics observation here).

---

## 21.6 PRD sync + phase closure

**PRD Reference:** §16 (new §16.5), §16.4 (capability table), §4.9 (ViewConfig notes)
**Design Reference:** PRD §16.5

**Status:** Complete

### Tasks

- [x] Draft PRD §16.5 (Materialized Views) from PRD §16.5: discovery (introspection + annotation), read-surface reuse, refresh semantics + transaction guard, PK/unique-index rule, dialect scope. _(§16.5.1–§16.5.4: dialect scope; discovery incl. deterministic PK rule, overlay-override, and the invariant; refresh methods with exact SQL, sentinel, and the fail-fast contract; cache/manifest/config interactions + out-of-scope list.)_
- [x] §16.5 + §27.11 note (carried from 21.4 verify observation): a cached matview still requires `invalidate_on` non-empty (the §27.11 hard rule applies unchanged — intentional per design §6 "ViewCacheConfig works unchanged"); refresh-driven self-invalidation is additive, not a substitute for source-table invalidation. State this explicitly so the config rule doesn't read as a dead-end for refresh-only matviews. _(§16.5.4 + a §27.11 matview paragraph spelling out the two complementary invalidation vectors and why the uniform requirement is harmless-not-incorrect.)_
- [x] Extend the §16.4 capability table with the Materialized Views column (matching design §4.1). _(Also added the missing Count row so the table matches design §4.1's full surface.)_
- [x] Note in §4.9 that `ViewConfig` applies to matviews unchanged (and that `Materialized` is a discovered/parsed schema property, not a config toggle).
- [x] Flip PRD §16.5 status to SYNCED (superseded by PRD §16.5), mirroring the CACHE/TENANCY/MCP doc-lifecycle pattern.
- [x] Run `/close-phase 21`: verify all sub-items complete, triage any open FIXes, run the full test sweep (`make check` `-race`, `make check-examples`, `make test-integration`), update STATUS.md. _(Landed 2026-07-13: all three legs clean, 0 open FIXes, STATUS flipped.)_

Additional consistency edits (same PRD-wide sweep discipline as 19.7): §16 intro skip rule now names `CREATE MATERIALIZED VIEW`; §16.2's PostgreSQL introspection row names `pg_matviews` + unique-index PK discovery (closing the wording deferral noted at 21.1 verify); §30.4.2's view-entries paragraph documents the `materialized: true` marker + mutation-side refresh methods + `ErrRefreshConcurrentlyInTx`.

### Acceptance Criteria

- PRD §16.5 exists and normatively describes the matview feature; §16.4 and §4.9 reflect it; no contradiction remains between the PRD and the design doc.
- PRD §16.5 is marked SYNCED.
- `/close-phase 21` sweep is green across all modules and example modules; STATUS.md shows Phase 21 Complete.

### Tests Required

- [x] Doc-consistency: PRD §16.4/§16.5 capability + surface match the generated matview client (methods present/absent as specified). _(Two independent reviewer passes: 12/12 normative §16.5 claims verified against the shipped code, plus a systematic design-doc §2–§8 vs PRD contradiction sweep — clean.)_
- [x] Full test sweep green (`make check` `-race`, `make check-examples`, `make test-integration`); no flakes. _(Closure sweep 2026-07-13: check ~5m29s, check-examples ~1m30s, test-integration ~6m59s — all exit 0, first pass, no flakes.)_

### Completion Record

**Completed 2026-07-13 (doc portion; closure sweep follows immediately).** Files changed:
- `docs/PRD.md` — new **§16.5 Materialized Views (PostgreSQL)** (16.5.1 dialect scope; 16.5.2 discovery incl. deterministic unique-index PK rule, annotation `@pk` semantics, overlay clear-then-apply override, and the `!Materialized ⇒ !ConcurrentlyRefreshable` invariant; 16.5.3 refresh methods with exact SQL, `refresh` hook op, `ErrRefreshConcurrentlyInTx` fail-fast contract, and the no-config-toggle rule; 16.5.4 cache/manifest/config interactions + out-of-scope list). §16.4 capability table gained the Materialized Views column and a previously-missing Count row. §4.9 ViewConfig matview note. §16 intro skip rule names `CREATE MATERIALIZED VIEW`. §16.2 Postgres introspection row names `pg_matviews` (closes the 21.1 wording deferral). §27.11 matview paragraph — the two complementary invalidation vectors, `invalidate_on` still required (closes the 21.4 carried observation). §30.4.2 view-entries paragraph — `materialized: true` marker + mutation-side refresh methods. Plus a doc-wide anchor repair: 4× broken `#27-caching-phase-2` → `#27-caching` (pre-existing; surfaced by the 21.6 review).
- PRD §16.5 — header flipped to **SYNCED — superseded by PRD §16.5 (2026-07-13)** (MCP.md lifecycle pattern; doc retained as design supplement: §9 decisions, §11 contrast); §10 stale "proposed §16.5" / "will formalize" wording retired (verify-pass finding, fixed inline).

Notes: doc-only diff — code tree byte-identical to the green 21.5 sweeps; no golden or example impact. Verify findings resolved inline pre-`/done`, 0 FIXes logged. Full sweep + STATUS flip land via `/close-phase 21`.
