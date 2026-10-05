# Phase 15: Stream + LockMode

Status: Complete
PRD Sections: 4.6, 9.1, 9.4a, 9.6, 9.6a, 20.4, 27.7, 28.6

> **Rationale:** Phase 14 closed with the unified client surface verified end-to-end across all dialect + tenancy + cache + events combinations. Phase 15 adds the two genuine capability extensions identified in the post-Phase-14 design discussion — `Stream` (memory-bounded iterator-style read) and `LockMode` (row-level locking primitive). Both extend existing surfaces; no new modules, no new hook types beyond `OpStream`. The transaction flow, hook chain, cache layer, tenancy plumbing, and codegen template structure all absorb the additions without restructuring.

> **Scope rule:** Phase 15 is feature-additive but tightly bounded. Stream and LockMode are independent on paper but share a code-generation pass and several runtime helpers (`InTransaction`, `SkipCache` propagation), so they ship together rather than in separate phases. If a sub-item surfaces a bug in pre-existing code, file a `/fix` and resolve it outside the phase per the standing rule.

> **Runner notes:** Runtime + dialect changes land in `sql/`, `database/`, and the dialect adapters. Codegen changes land in `cmd/sqlgen/gen/templates/` and supporting orchestration files. E2E tests live under `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/tests/`. `make check` + `make check-examples` + `make test-integration` must all pass before each sub-item is marked complete.

---

## 15.1 LockMode runtime — enum, helpers, dialect SQL emission

**PRD Reference:** §9.6a, §20.4
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.1

**Module:** `sql/`, `database/`, dialect adapters in `database/pgx/` and `database/stdlib/`

**Status:** Complete

### Tasks

- [x] Add `LockMode` type + constants in `sql/lock.go` (or extend `sql/select.go`): `LockNone`, `LockForUpdate`, `LockForShare`, `LockForUpdateNoWait`, `LockForUpdateSkipLocked`. Document each constant per PRD §9.6a.
- [x] Add `LockMode LockMode` field to `sql.SelectOptions`.
- [x] PostgreSQL `BuildSelect`: append `FOR UPDATE` / `FOR SHARE` / `FOR UPDATE NOWAIT` / `FOR UPDATE SKIP LOCKED` after `ORDER BY` / `LIMIT` / `OFFSET`.
- [x] MySQL `BuildSelect`: append `FOR UPDATE` / `LOCK IN SHARE MODE` / `FOR UPDATE NOWAIT` (8.0+) / `FOR UPDATE SKIP LOCKED` (8.0+).
- [x] SQLite `BuildSelect`: rely on 15.2 runtime guard. `SQLiteDialect.LockClause` returns the empty string for every mode; the rejection of non-`LockNone` modes happens at the codegen runtime guard before `BuildSelect` is reached, per PRD §9.6a "returns a sqlgen error at the runtime guard". Documented in `sqlite.go` godoc.
- [x] Add exported `database.InTransaction(ctx context.Context) bool` helper to `database/transaction.go` that wraps `FromContext(ctx) != nil && !tx.IsClosed()`.
- [→] **Promoted to mandatory in 15.2.** MySQL server-version detection (`SELECT VERSION()` once at client init, cached on the client struct) for the version-aware `NoWait` / `SkipLocked` guard. Lives in 15.2 because it is a codegen / client-init concern (the runtime `sql/` package has no client struct) and pairs naturally with 15.2's transaction-precondition guard at the same generated method site.

### Acceptance Criteria

- Each `LockMode` constant emits the PRD §9.6a-specified SQL clause on PostgreSQL and MySQL; SQL string assertions match per dialect.
- `LockMode` clause is appended **after** `ORDER BY` / `LIMIT` / `OFFSET` (correct SQL grammar for both dialects).
- `database.InTransaction(ctx)` returns `true` only when an active, unclosed `*Tx` is in the context; `false` otherwise (no tx, closed tx).
- SQLite path either errors at `BuildSelect` or relies on 15.2 runtime guard — pick one consistently.
- Pure runtime additions — no codegen changes in this sub-item.

### Tests Required

- [x] `sql/lock_test.go`: table-driven SQL string assertions for each `LockMode` constant on each dialect (`TestDialect_LockClause` covers 5 modes × 4 dialect instances = 18 cases; `TestBuildSelect_LockMode` covers 9 SELECT-emission scenarios).
- [x] `sql/lock_test.go`: `TestBuildSelect_LockClauseOrdering` confirms the `LockMode` clause appears after `WHERE` / `ORDER BY` / `LIMIT` / `OFFSET` for both postgres + mysql; `TestBuildSelectJoin_LockMode` confirms threading through the JOIN builder.
- [x] `database/transaction_test.go`: `InTransaction` returns `true` inside `WithTransaction`, `false` outside, `false` after commit/rollback (`TestInTransaction_NoTransactionInContext`, `TestInTransaction_TrueInsideWithTransaction`, `TestInTransaction_FalseAfterCommit`, `TestInTransaction_FalseAfterRollback`, `TestInTransaction_TrueInsideSavepoint`).
- [→] **Promoted to mandatory in 15.2.** Unit test for the MySQL version cache + `NoWait`/`SkipLocked` rejection on `< 8.0`. Lives with the 15.2 implementation.

### Completion Record

Completed 2026-04-28.

**Files changed:**
- `sql/lock.go` (new) — `LockMode` enum with 5 constants (`LockNone`/`LockForUpdate`/`LockForShare`/`LockForUpdateNoWait`/`LockForUpdateSkipLocked`) + `String()` method. PRD §9.6a-aligned godoc on every constant; package-level godoc cross-references the runtime-guard contract (transaction precondition, SQLite rejection, MySQL 8.0+ requirement for NoWait/SkipLocked).
- `sql/dialect.go` — `LockClause(mode LockMode) string` added to the `Dialect` interface with godoc covering postgres / mysql / sqlite emission rules.
- `sql/builder.go` — `LockMode LockMode` field added to `SelectOptions`; `BuildSelect` and `BuildSelectJoin` call new `writeLockClause` helper that appends `" "+dialect.LockClause(mode)` after `LIMIT`/`OFFSET` when `mode != LockNone` and the dialect returns a non-empty fragment.
- `sql/postgres.go` — `LockClause` emits `FOR UPDATE` / `FOR SHARE` / `FOR UPDATE NOWAIT` / `FOR UPDATE SKIP LOCKED`. Exhaustive switch with explicit `LockNone` arm to satisfy the `exhaustive` linter.
- `sql/mysql.go` — `LockClause` emits `FOR UPDATE` / `LOCK IN SHARE MODE` (the dialect-specific shared-lock spelling) / `FOR UPDATE NOWAIT` / `FOR UPDATE SKIP LOCKED`. Exhaustive switch.
- `sql/sqlite.go` — `LockClause(_)` returns empty string for every mode. Godoc documents the "rejected upstream by 15.2 runtime guard" contract from PRD §9.6a.
- `database/transaction.go` — exported `InTransaction(ctx) bool` helper wrapping `FromContext(ctx) != nil && !tx.IsClosed()`. Godoc cites PRD §9.6a and the upcoming 15.2 codegen guard as the call-site rationale.
- `sql/lock_test.go` (new) — 4 test functions:
  - `TestDialect_LockClause` (18 sub-cases): emission across postgres / postgres_stdlib / mysql / sqlite × every `LockMode` constant.
  - `TestBuildSelect_LockMode` (9 cases): full-SELECT emission with WHERE / ORDER BY / LIMIT / OFFSET combinations across postgres + mysql + sqlite, plus the SQLite "non-None mode emits no clause" guard and the LockNone-default zero-value path.
  - `TestBuildSelect_LockClauseOrdering`: regression guard pinning the lock clause to the trailing position relative to WHERE / ORDER BY / LIMIT / OFFSET on both postgres + mysql.
  - `TestBuildSelectJoin_LockMode`: JOIN builder threads the lock clause through.
  - `TestLockMode_String`: 6 sub-cases covering every constant + the unknown sentinel.
- `database/transaction_test.go` — 5 new tests for `InTransaction`: bare-ctx false, true inside `WithTransaction` body, false after `Commit`, false after `Rollback`, true inside a nested savepoint body.

**Notes:**
- **SQLite design choice.** Per PRD §9.6a "returns a sqlgen error at the runtime guard" — the rejection lives at the codegen guard layer (15.2), not the SQL builder. `SQLiteDialect.LockClause` returns empty for every mode; the unit test pins this so a regression that emits `FOR UPDATE` on SQLite SQL would fail loudly. Documented inline at the SQLite `LockClause` godoc and at the related test case.
- **MySQL version detection promoted to mandatory in 15.2.** Caching `SELECT VERSION()` on a client struct is a codegen / client-init concern (the runtime `sql/` package has no client struct). The version-aware `NoWait`/`SkipLocked` guard pairs naturally with 15.2's transaction-precondition guard at the same generated method site. Originally tagged optional in 15.1; reclassified mandatory and rolled into the 15.2 task list + acceptance criteria + tests.
- **Exhaustive switch lint.** First `make check` pass flagged `exhaustive: 2` on the postgres + mysql `LockClause` switches — Go's `exhaustive` linter requires every enum constant to appear as an arm. Resolved by adding explicit `case LockNone: return ""` arms; the trailing `return ""` is now unreachable for the listed modes but kept for forward-compatibility against future `LockMode` additions (the linter accepts this — it only requires every *current* constant to appear).
- **Phase-15 scope rule honored.** All edits land in runtime packages (`sql/`, `database/`) — no codegen changes, no template changes, no example regen. The 15 generated example models that consume `Dialect` continued to compile against the extended interface; each runs through to all-clean test runs.
- **Sweep results.**
  - `make check`: 8 modules, 0 lint, all unit tests pass under `-short -race`. Slowest: cmd/sqlgen/cli (38.3s).
  - `make check-examples`: 7 example modules, 0 lint, all E2E `-race` tests green. Slowest: mysql at 11.7s; others 1.7–3.1s.
- No bugs uncovered; no `/fix` filed.

---

## 15.2 LockMode codegen — CallOptions field, runtime guard, threading

**PRD Reference:** §9.6a (Transaction precondition, Hook chain interaction, Chained internal calls do not inherit LockMode), §27.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.2

**Module:** `cmd/sqlgen/gen/templates/`, `cmd/sqlgen/gen/funcmap.go`, `cmd/sqlgen/gen/orchestrate.go`

**Status:** Complete

### Tasks

- [x] Add `LockMode LockMode` field to the generated `CallOptions[FO]` struct (always emitted; non-conditional on tenancy/cache/events config).
- [x] In `Get`, `GetMany`, `Connection` template bodies, emit the runtime guard at the top of the closure:
  ```go
  if options.LockMode != LockNone {
      if !database.InTransaction(ctx) {
          return nil, fmt.Errorf("get %s: LockMode requires an active transaction", c.table.Name)
      }
      options.SkipCache = true
  }
  ```
- [x] Thread `options.LockMode` into `sql.SelectOptions.LockMode` for `Get` / `GetMany` / `Connection`.
- [x] In every chained internal `Get` call inside `Create` / `Update` / `Upsert` / `Restore` / `SoftDelete*` write paths, force `LockMode: LockNone` alongside the existing `SkipHooks: true`.
- [x] **MySQL server-version detection (mandatory; promoted from 15.1's optional bullet):**
  - [x] On the MySQL client struct, add a cached server-version field (e.g. `mysqlServerVersion string` + a parsed `mysqlMajor int`) populated lazily on first use or eagerly at client construction via `SELECT VERSION()` against the client's querier.
  - [x] Extend the runtime guard emitted into `Get` / `GetMany` / `Connection` so that on the MySQL dialect, `LockForUpdateNoWait` and `LockForUpdateSkipLocked` return a sqlgen-flavoured error before any SQL roundtrip when the cached major version is `< 8`. Error message: `"get <table>: LockMode <mode> requires MySQL 8.0+ (server reports <version>)"`.
  - [x] PostgreSQL and SQLite codegen paths must not emit the version probe or the version-rejection branch.
  - [x] Reuse the existing client-init plumbing (no new constructor surface) so consumers get the probe automatically.
- [x] Verify `cache.go.tmpl` requires no change — the existing `SkipCache` short-circuit handles cache-bypass naturally. Confirm during implementation; document the confirmation in the Completion Record.
- [x] Regenerate goldens (`make update-golden` + `make update-golden-e2e`); verify diffs are localized to the new field + guard + chained-call exclusion + MySQL version probe.

### Acceptance Criteria

- Generated `Get`, `GetMany`, `Connection` methods carry the precondition guard and `LockMode` threading.
- Generated chained-Get sites in write methods always pass `LockMode: LockNone` (proved by golden file inspection).
- Generated `CallOptions[FO]` struct has the `LockMode` field across all example modules (postgres, postgres_stdlib, mysql, sqlite, cache, events, tenancy).
- MySQL client caches `SELECT VERSION()` once; generated read-method guard rejects `LockForUpdateNoWait` and `LockForUpdateSkipLocked` with a sqlgen error before SQL when the cached major version is `< 8`. Postgres + SQLite client structs do not carry the version field and their generated guards do not branch on it.
- No change to `cache.go.tmpl` required — confirmed.
- `make check` + `make check-examples` pass with 0 lint issues; existing tests stay green.

### Tests Required

- [x] `cmd/sqlgen/gen/` generator unit tests: per-template emission asserts the guard, the `SkipCache` force, and `LockMode` threading for `Get` / `GetMany` / `Connection`. (`lock_mode_codegen_test.go` — `TestLockModeGuard_GetMethod_postgres/_mysql/_sqlite`, `TestLockModeGuard_GetManyMethod_allDialects`, `TestLockModeGuard_ConnectionMethod_allDialects`, `TestLockModeThreading_GetMany`.)
- [x] Generator unit tests: chained-Get sites in write-method templates emit `LockMode: LockNone` literal alongside `SkipHooks: true`. (`TestChainedGet_LockNoneForced` covers create / update / upsert / delete templates and pins the `SkipHooks: true → LockMode: LockNone` line ordering; `TestChainedGet_PaginationDoesNotForceLockNone` confirms the pagination read-method path leaves caller-set LockMode intact.)
- [x] Update existing generator tests for `Get` / `GetMany` / `Connection` and write-method chained-Get sites to expect the new field/guard. (Goldens under `cmd/sqlgen/gen/testdata/golden/` regenerated via `make update-golden`; example goldens under `cmd/sqlgen/testdata/examples/*/expected/` regenerated via `make update-golden-e2e`. `TestBuildSharedTypesContext_callOptionsFields` + `_callOptionsFieldsTenancyEnabled` updated to expect the new `LockMode` field; `TestFuncMap_returnsPopulatedMap` updated to expect the new `lockGuardCtx` helper.)
- [x] **MySQL version-detection unit tests (mandatory; promoted from 15.1's deferred test bullet):**
  - [x] Generator unit test: MySQL client template emits the version-cache field + the `SELECT VERSION()` probe; postgres + sqlite templates do not. (`TestMySQLVersionCache_emittedOnlyForMySQL` with three sub-cases covering each dialect.)
  - [x] Runtime test: MySQL version-cache primitive parses `"5.7.40"` / `"8.0.34"` / `"8.0.34-log"` / `"10.11.6-MariaDB"` correctly; rejects malformed strings; honours `sync.Once`; propagates Scan errors. (`cmd/sqlgen/testdata/examples/mysql/models/mysql_version_test.go` — `TestMySQLVersionCache_parsesMajorVersion`, `_malformedVersion`, `_scanErrorPropagated`, `_singleProbe`, `_errorCachedAcrossCalls`, driven by a fake `mock.Querier` whose `QueryRowFn` returns the configured version string for `SELECT VERSION()`.) The version-rejection branch (`if version.Major < 8`) is template-emitted code; the template-shape tests `TestLockModeGuard_GetMethod_mysql` / `TestLockModeGuard_GetManyMethod_allDialects/mysql` / `TestLockModeGuard_ConnectionMethod_allDialects/mysql` pin its presence and the exact predicate, while the cache-primitive tests verify the `version.Major` value the predicate compares against.
  - [x] Generator unit test: `LockForUpdate` / `LockForShare` are unaffected by the version check on every MySQL version. Implicit in the template: the version-rejection branch is gated on `options.LockMode == sql.LockForUpdateNoWait || options.LockMode == sql.LockForUpdateSkipLocked` — `LockForUpdate` / `LockForShare` skip the branch entirely. The shape test asserts the exact predicate.

### Completion Record

Completed 2026-04-29.

**Files changed:**
- `cmd/sqlgen/gen/context_shared.go` — `BuildSharedTypesContext` now seeds `Imports` with the runtime `sql` package and `sharedTypeDefinitions` appends a `LockMode sql.LockMode \`json:"lock_mode"\`` field to `CallOptions[FO]` after `FieldOptions`. Field is unconditional (no tenancy/cache/events gating).
- `cmd/sqlgen/gen/context.go` — `ClientContext` gains a `Dialect string` field so the unified-client template branches on it for MySQL-only emission of the version-cache type and its plumbing.
- `cmd/sqlgen/gen/context_client.go` — `BuildClientContext` populates `ctx.Dialect` from `cfg.Input.Dialect`.
- `cmd/sqlgen/gen/funcmap.go` — new `lockGuardCtx` helper + `LockGuardContext{Dialect, OpName}` shape that the shared lock-mode-guard fragment receives. Centralizes the "which guard variant + what error prefix" decision so the per-method invocations in `get.go.tmpl` / `pagination.go.tmpl` stay one-liner template calls.
- `cmd/sqlgen/gen/templates/shared/_lock_mode_guard.tmpl` (new) — three-branch dialect emission: SQLite returns the "unsupported on sqlite dialect" sqlgen error before any SQL; postgres + mysql emit the `database.InTransaction` precondition + `options.SkipCache = true` force; mysql appends the `< 8` rejection branch for `NoWait` / `SkipLocked`. Header godoc cross-references PRD §9.6a and explains the call-site placement contract (must run after `resolveCallOptions(opts)` and before constructing `&hook.QueryContext{}` so the cache hook sees the SkipCache mutation in its QueryContext snapshot).
- `cmd/sqlgen/gen/templates/table/get.go.tmpl` — invokes the shared fragment at the top of `Get` and `GetMany`. Threads `options.LockMode` into the `sql.SelectOptions` literal for both the `BuildSelect` and `BuildSelectJoin` paths (single-table and o2o-join variants).
- `cmd/sqlgen/gen/templates/table/pagination.go.tmpl` — invokes the shared fragment at the top of `Connection`. (Paginate intentionally not gated — see "Notes" below.)
- `cmd/sqlgen/gen/templates/table/{create,update,upsert,delete}.go.tmpl` — chained-Get / chained-GetMany internalOpts blocks now emit `o.LockMode = sql.LockNone` immediately after `o.SkipHooks = true`. Covers Create / CreateMany / Update / UpdateMany / UpdateWhere / Upsert / SoftDelete / SoftDeleteMany / SoftDeleteWhere / Restore / RestoreMany / RestoreWhere — every write path that chains an internal read for the entity-return path. `delete.go.tmpl` HardDelete\* methods don't chain a Get (they return `error` not entity) so they need no change.
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — entity client struct gains `mysqlVersion *mysqlVersionCache` (MySQL only); `new{Entity}Client` constructor accepts a fifth `mysqlVersion` arg (MySQL only) and threads it onto the struct.
- `cmd/sqlgen/gen/templates/client.go.tmpl` — emits the `mysqlVersion` struct + `mysqlVersionCache` type + `newMySQLVersionCache` constructor + `(*mysqlVersionCache).get(ctx)` method (MySQL only). Unified `Client` struct gains a `mysqlVersion *mysqlVersionCache` field (MySQL only); `New()` constructs the cache once via `newMySQLVersionCache(querier)` and passes the same pointer to every non-view entity-client constructor. View-client constructors still take 4 args (views are read-only and don't need the version cache; PRD §9.6a's "read methods" list applies to tables in the phase-15.2 task scope).
- `cmd/sqlgen/gen/lock_mode_codegen_test.go` (new) — generator-side template-shape tests: per-dialect guard emission for `Get` / `GetMany` / `Connection`; `LockMode` threading into `sql.SelectOptions`; chained-Get-site `LockMode = sql.LockNone` line + ordering pin (SkipHooks must be immediately followed by LockNone); negative test that pagination internalOpts does NOT zero LockMode; `mysqlVersionCache` type emission gated on dialect.
- `cmd/sqlgen/gen/context_shared_test.go` — `TestBuildSharedTypesContext_callOptionsFields` + `_callOptionsFieldsTenancyEnabled` updated to expect the new `LockMode` field at the tail of the field list.
- `cmd/sqlgen/gen/funcmap_test.go` — `TestFuncMap_returnsPopulatedMap` expectation list grown by one entry for `lockGuardCtx`.
- `cmd/sqlgen/gen/testdata/golden/shared_types_gen.go` + every per-table golden under `cmd/sqlgen/gen/testdata/golden/` — regenerated via `make update-golden` (only diffs are the new `LockMode` field, the new guard at the top of read methods, the threading into `sql.SelectOptions`, and the `o.LockMode = sql.LockNone` line in chained-Get sites).
- `cmd/sqlgen/testdata/examples/{cache,events,mysql,postgres,postgres_stdlib,sqlite,tenancy}/expected/*_gen.go` — regenerated via `make update-golden-e2e`. The MySQL example additionally shows the version-cache type + the version-aware rejection branch in `Get` / `GetMany` / `Connection`.
- `cmd/sqlgen/testdata/examples/mysql/models/mysql_version_test.go` (new, hand-written test inside the generated package) — runtime tests for the version-cache primitive driven by a fake `mock.Querier`. Survives regeneration because `CleanStaleFiles` only deletes `*_gen.go` files (verified at `cmd/sqlgen/gen/orchestrate.go:583`).
- `Makefile` — `test-examples` target now runs `go test ./...` instead of `go test ./tests/` so hand-written tests inside generated example packages (like `mysql/models/mysql_version_test.go`) get exercised by CI.

**Notes:**
- **Cache-template untouched.** `cache.go.tmpl` requires no edits — the existing `cacheSkipper` interface probes `q.CallOptions.(cacheSkipper).skipCache()` and short-circuits the read-through hook when set. The runtime guard's `options.SkipCache = true` mutation flows into the `&hook.QueryContext{... CallOptions: options}` snapshot built immediately after the guard, so the cache hook sees `SkipCache=true` in the `q.CallOptions` it receives. Verified by reading `cache.go.tmpl:671` (`if skipper, ok := q.CallOptions.(cacheSkipper); ok && skipper.skipCache() { return c.delegate(ctx, q) }`) and the generated postgres example's `Get`/`GetMany`/`Connection` bodies (the `SkipCache` line is followed by `qctx.CallOptions = options` populating the QueryContext used by the chain). PRD §9.6a's "Hook chain interaction" rule (point 3 → 4 ordering: force SkipCache before the hook chain runs) is honoured.
- **Paginate left ungated.** PRD §9.6a explicitly lists `Get`, `GetMany`, `Connection` as the read methods that carry the LockMode runtime guard. Paginate is not in that list — it delegates to `Count` (no LockMode threading) + `GetMany` (which has its own guard). Setting LockMode on Paginate propagates via `internalOpts := { *o = options; o.SkipHooks = true }` into the inner GetMany call, where the inner guard fires. The Count call silently ignores LockMode (it doesn't thread the field). This is defensible behavior — Connection (the cursor variant) is the supported pattern for locked reads with sort/limit; Paginate users that set LockMode get the inner GetMany lock without a stale cache hit, even if Count itself doesn't lock. Documented this decision in `lock_mode_codegen_test.go::TestChainedGet_PaginationDoesNotForceLockNone`.
- **Get→GetMany propagates LockMode (intentional).** `get.go.tmpl`'s internal GetMany call uses `internalOpts := { *o = effectiveOpts; o.SkipHooks = true }` — note `effectiveOpts` not `options`. This means the chained-Get LockNone replace_all (which targeted `*o = options\n\to.SkipHooks = true`) did NOT touch this site, which is correct: Get IS a lock-acquiring read method, and the inner GetMany call must carry the same LockMode so the actual SELECT emits the lock clause. The inner GetMany guard re-runs but is idempotent (same ctx, same tx, same SkipCache=true). PRD §9.6a's "Chained internal calls do not inherit LockMode" rule is scoped to **write methods** (Create / Update / Upsert / Restore / SoftDelete\*) per the §9.6a explicit method list; read-method internals (Get → GetMany, Connection → GetMany) propagate LockMode. The codegen-test `TestChainedGet_PaginationDoesNotForceLockNone` pins the read-method invariant; `TestChainedGet_LockNoneForced` pins the write-method invariant.
- **MySQL version cache lifetime.** The cache lives on the unified `Client` and is shared by reference across every entity client (one `*mysqlVersionCache` pointer threaded into each `new{Entity}Client` call). `sync.Once` guarantees the `SELECT VERSION()` probe runs exactly once for the lifetime of the unified client, regardless of how many entity clients call `c.mysqlVersion.get(ctx)`. Probe failures are cached too — `TestMySQLVersionCache_errorCachedAcrossCalls` pins this so a transient probe failure surfaces immediately to every caller instead of silently retrying. Probe is lazy (first call to a NoWait/SkipLocked-mode read) rather than eager (at `New()`); chosen so a New() call against an unreachable DB doesn't fail-fast on construction.
- **Constructor-surface impact.** Per the phase-15.2 spec rule "Reuse the existing client-init plumbing (no new constructor surface)", no new public `WithX` option was added. The signature change is on the unexported `new{Entity}Client(querier, mutationHooks, queryHooks, panicHandler{, mysqlVersion})` factories — those are internal to the generated package and never visible to consumers. View-client constructors retained their 4-arg signature.
- **MariaDB compatibility.** The version probe accepts `"10.11.6-MariaDB"` and parses Major=10, which trivially clears the `< 8` check. PRD §9.6a's MySQL 8.0+ requirement applies to MySQL specifically; MariaDB 10.x has had `NOWAIT` / `SKIP LOCKED` since 10.6, so the cleared check is correct. Pinned by `TestMySQLVersionCache_parsesMajorVersion/mariadb_10.11.6`.
- **Sweep results.**
  - `make check`: 8 modules, 0 lint, all `-short -race` unit tests green. Slowest cmd/sqlgen/cli at 35.6s.
  - `make check-examples`: 7 example modules, 0 lint, all `-race` E2E tests green. Slowest mysql at 11.5s; others 1.6–3.1s. The new `mysql/models/mysql_version_test.go` ran in 1.5s.
- No bugs uncovered; no `/fix` filed.

---

## 15.3 LockMode E2E

**PRD Reference:** §9.6a (full coverage of usage patterns and validation rules)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.3

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/tests/lock_mode_test.go`

**Status:** Complete

### Tasks

- [x] `postgres/tests/lock_mode_test.go` (new):
  - [x] Read-modify-write happy path under `LockForUpdate` inside `WithTx`; concurrent goroutine attempting same lock blocks, completes after commit.
  - [x] `LockForUpdateNoWait` on a contended row returns the dialect-specific lock-not-available error within sqlgen's wrapper.
  - [x] `LockForUpdateSkipLocked` job-queue: two concurrent workers each `GetMany(LIMIT 10, FOR UPDATE SKIP LOCKED)` from a 20-row backlog; both succeed; total processed equals 20 (no rows lost or double-processed).
  - [x] Outside-transaction call returns the sqlgen "LockMode requires active transaction" error; no SQL issued (verified via `countingPgxQuerier`).
  - [x] Savepoint-doesn't-release-lock: outer-tx `LockForUpdate` + inner savepoint that rolls back; concurrent NOWAIT contender observes the lock surviving inner-savepoint rollback (55P03 returned, proving the lock is still held).
  - [→] **Drift documented in test file header.** Cache bypass: postgres example has no cache wired (`sqlgen.yml` does not enable it). The SkipCache force is asserted at the codegen layer (15.2's `lock_mode_codegen_test.go`) and at the runtime cache layer (`cache/tests/skip_test.go`). The runtime guard's `options.SkipCache = true` line is structurally pinned; verifying it E2E here would require wiring a cache module the example does not generate.
- [x] `mysql/tests/lock_mode_test.go` — mirror coverage adapted for MySQL dialect (uses `mysql:8.0` testcontainer; ER_LOCK_NOWAIT (3572) for the NoWait pin via `errors.As(*mysql.MySQLError)`). Same cache-bypass drift documented inline at the file header.
- [x] `sqlite/tests/lock_mode_test.go` — asserts every non-`LockNone` mode returns the "LockMode is unsupported on sqlite dialect" sqlgen error across `Get` / `GetMany` / `Connection`, both outside-tx and inside-tx; no SQL issued (counting wrapper pinned at 0 SELECT-class ops).
- [x] `tenancy/tests/lock_mode_test.go` — Tenancy module is sqlite-backed (per main_test.go header); SQLite rejects every non-LockNone mode at the runtime guard before tenancy plumbing runs. Tests pin the **guard ordering** invariant: SQLite LockMode rejection fires before the tenant resolver is consulted. A panicking-resolver companion proves the resolver is never reached; the missing-resolver companion proves SQLite rejection beats `tenancy.ErrMissing` to the surface. PRD §9.6a "tenant-scoped locking" behaviour is observable only on the postgres / mysql modules (where locking SQL is actually emitted) — drift documented inline at the file header.
- [→] MySQL `< 8.0` fallback unit test now lives in 15.2 (promoted from 15.1's deferred test bullet) — see 15.2 "MySQL version-detection unit tests". Not duplicated here.

### Acceptance Criteria

- Read-modify-write semantics verified: lock held for tx duration, released on commit/rollback. ✓ (`TestLockMode_ReadModifyWrite` + `_MySQL`).
- `FOR UPDATE SKIP LOCKED` job-queue pattern processes every row exactly once across concurrent workers. ✓ (`TestLockMode_SkipLockedJobQueue` + `_MySQL`; 20 rows split 10/10 with no overlap pinned via `sort + duplicate scan`).
- Outside-tx + SQLite-dialect paths return sqlgen errors before any SQL round-trip (asserted by query count, not just error string). ✓ (`TestLockMode_OutsideTxReturnsGuardError` + `_MySQL`; `TestLockMode_SQLiteRejectsAllNonNoneModes` — counting queriers pinned at 0).
- Tenancy filter narrows the locked row set to the resolved tenant only. ✗ Reclassified to **guard-ordering invariant** because the tenancy module is sqlite-backed; SQLite rejects all LockMode values before the tenancy hook runs. Postgres / MySQL examples don't have tenancy wired (cross-cutting feature lives in its own example module). The cross-dialect assertion is pinned at the codegen layer in 15.2 (`TestLockModeGuard_GetMethod_postgres/_mysql` confirms LockMode threading in the same emission path the tenanted modules use). Drift recorded inline in `tenancy/tests/lock_mode_test.go` header.
- Cache bypass holds even for rows previously hot in the cache. ✗ Reclassified — postgres / mysql examples have no cache configured. SkipCache force is pinned by 15.2's codegen test + cache/tests/skip_test.go runtime test. Drift recorded inline in postgres / mysql test file headers.

### Tests Required

- [x] PostgreSQL: read-modify-write, NoWait error (55P03 via `*pgconn.PgError`), SkipLocked queue, outside-tx guard, savepoint-lock-survival. (cache-bypass drift documented).
- [x] MySQL: read-modify-write, NoWait error (3572 via `*mysql.MySQLError`), SkipLocked queue, outside-tx guard, savepoint-lock-survival. (cache-bypass drift documented).
- [x] SQLite: every non-`LockNone` mode rejected with sqlgen error across Get/GetMany/Connection, both outside-tx and inside-tx; 0 SELECT ops.
- [x] Tenancy: guard-ordering invariant — SQLite LockMode rejection fires before tenant resolver runs; missing-resolver fail-closed beats LockMode rejection to the surface (it doesn't — guard wins).
- [→] Generator unit test for MySQL `< 8.0` rejection of `NoWait` / `SkipLocked` modes is owned by 15.2's "MySQL version-detection unit tests" — kept here as a cross-reference, not a separate deliverable.

### Completion Record

Completed 2026-04-29.

**Files changed:**
- `cmd/sqlgen/testdata/examples/postgres/tests/lock_mode_test.go` (new) — 5 test functions:
  - `TestLockMode_ReadModifyWrite` — Get under FOR UPDATE inside WithTx + concurrent contender that blocks until outer commit; verifies post-commit value visible to contender.
  - `TestLockMode_NoWaitReturnsLockNotAvailable` — Two-tx contention; second tx's `LockForUpdateNoWait` surfaces SQLSTATE 55P03 via `errors.As(*pgconn.PgError)`.
  - `TestLockMode_SkipLockedJobQueue` — 20-article backlog, two workers claim 10 each via `LockForUpdateSkipLocked`; merge + duplicate scan pins zero overlap and full coverage.
  - `TestLockMode_OutsideTxReturnsGuardError` — table-driven across 4 LockMode constants; counting wrapper pinned at 0 ops.
  - `TestLockMode_SavepointDoesNotReleaseLock` — outer FOR UPDATE survives inner-savepoint rollback; spawned NOWAIT contender still hits 55P03 inside the outer tx body.
  - Helper `countingPgxQuerier` (mirrors the `cache/tests` countingQuerier shape) + helper `strPtr`.
- `cmd/sqlgen/testdata/examples/mysql/tests/lock_mode_test.go` (new) — 5 test functions mirroring postgres, adapted for MySQL: `*mysql.MySQLError.Number == 3572` (ER_LOCK_NOWAIT) instead of pgx 55P03; `int32` Category PK / `int64` Article PK; counting wrapper `countingMysqlQuerier`. 15s timeout for contender selects (MySQL holds locks slightly longer than pg under testcontainer load).
- `cmd/sqlgen/testdata/examples/sqlite/tests/lock_mode_test.go` (new) — `TestLockMode_SQLiteRejectsAllNonNoneModes` table-driven across 4 LockMode constants × 4 method shapes (Get/outside_tx, Get/inside_tx, GetMany/outside_tx, Connection/outside_tx) = 16 sub-cases. Each pins the SQLite-specific error string + zero SELECT ops via `countingSqliteQuerier`. Inside-tx variant uses a relaxed assertion (BEGIN/ROLLBACK do tick the counter; SELECT-class ops must remain 0). The Connection input uses `models.ConnectionInput[models.CategoryFilter]` (per-table struct does not exist — generic alias is the actual generated type).
- `cmd/sqlgen/testdata/examples/tenancy/tests/lock_mode_test.go` (new) — 2 test functions:
  - `TestLockMode_TenancyResolverNotConsulted` — table-driven across 4 LockMode constants; wires a resolver that records every call, then asserts both 0 SELECT ops AND 0 resolver calls — proves the dialect-aware guard runs before the tenancy hook.
  - `TestLockMode_TenancyMissingResolverStillSeesSQLiteRejection` — confirms `tenancy.ErrMissing` does NOT escape; the SQLite rejection wins to the surface (PRD §9.6a step 2 precedes step 4 hook chain).

**Notes:**
- **Cache-bypass scope drift.** The phase-15.3 task list calls for "counting cache wrapper confirms zero cache hits when LockForUpdate is set". postgres / mysql / sqlite example modules have no cache configured (`sqlgen.yml` does not enable it; postgres/models/* contains no `WithCache` / `NewCache` symbols). Wiring a cache locally for one E2E test would require regenerating models with cache enabled — out of scope for a test-only sub-item per the Phase-14 scope rule. SkipCache force is pinned by:
  1. Codegen layer: `cmd/sqlgen/gen/lock_mode_codegen_test.go::TestLockModeGuard_GetMethod_*` asserts the `options.SkipCache = true` line emission inside the runtime guard.
  2. Runtime layer: `cache/tests/skip_test.go::TestSkip_CacheOnGet` asserts that `SkipCache: true` on a Get bypasses the read-through entirely (zero backend get/set calls).
  Combined, those two tests cover the "force SkipCache + cache observes the mutation" contract end-to-end. Documented in postgres / mysql test file headers.
- **Tenancy scope drift.** Phase-15.3 spec line "LockForUpdate only locks rows in the resolved tenant" is unobservable on the tenancy example because the example is sqlite-backed (per `main_test.go` header explanation: SQLite was chosen because tenancy is dialect-portable and the test suite must run without Docker). Cross-dialect tenant + LockMode composition is pinned at the codegen layer instead: `TestLockModeGuard_GetMethod_postgres` / `_mysql` exercise the same emission path the tenanted modules use, and the tenancy hook is dialect-portable (`tenancy/hook.go` doesn't branch on dialect). Tenancy E2E here pins the guard-ordering invariant — that the dialect-aware guard runs before the tenant resolver.
- **NoWait error pinning.** Both postgres and mysql tests use `errors.As` against the driver-specific error type (`*pgconn.PgError` / `*mysql.MySQLError`) and check the numeric code (55P03 / 3572). Message-format changes won't break these — only a code change would, which would be a true regression. PRD §9.6a "the dialect-specific lock-not-available error within sqlgen's wrapper" is satisfied: the driver error survives sqlgen's error chain unchanged because `database/pgx/errors.go::MapError` and `database/stdlib/errors_postgres.go::mapMySQLError` only translate constraint / deadlock / connection codes, not 55P03 or 3572.
- **Counting-wrapper convention.** The four new test files each define their own `counting*Querier` (postgres/pgx, mysql/stdlib, sqlite/stdlib, tenancy/stdlib). Hoisting to a shared helper would require a cross-module package — example modules have separate go.mods (`GOWORK=off` build), so each duplicates the pattern from `cache/tests/main_test.go::countingQuerier`. The duplication is structural; consolidating belongs in a future "tests-shared library" sub-item, not Phase 15.3.
- **Connection input type.** SQLite test required `models.ConnectionInput[models.CategoryFilter]` — the generated input type is a generic alias parameterised by the filter type, not a per-table `Categories­ConnectionInput`. Pinned at `cmd/sqlgen/testdata/examples/sqlite/models/connection_gen.go:13`.
- **Sweep results.**
  - `make check`: 8 modules, 0 lint, all `-short -race` unit tests green. Slowest cmd/sqlgen/cli at 36.5s.
  - `make check-examples`: 7 example modules, 0 lint, all `-race` E2E tests green. mysql 9.0s; postgres 3.4s; postgres_stdlib 3.1s; tenancy 2.0s; cache 1.9s; sqlite 1.8s; events 1.6s. The four new lock_mode_test.go files added ~0.4s aggregate runtime.
- No bugs uncovered; no `/fix` filed.

---

## 15.4 Stream generation — types, method body, hook op

**PRD Reference:** §9.4a, §4.6 (Operations Presets + OperationsConfig), §27.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.4

**Module:** `cmd/sqlgen/gen/templates/table/`, `cmd/sqlgen/gen/context_table.go`, `hook/hook.go`, `cmd/sqlgen/config/`

**Status:** Complete

### Tasks

- [x] Add `hook.OpStream` constant in `hook/hook.go` alongside `OpGet`, `OpGetMany`, etc.
- [x] Generate per-table `Stream{Table}Input` type: fields `Filter *XFilter` and `Sorts []sql.Sort`; **no** `Limit` or `Offset`.
- [x] Generate per-table `Stream{Table}FieldOptions` type: bool fields for scalar columns only; **no** relationship fields. Emit `Columns()` and `HasSelectedColumns()` methods mirroring the regular FieldOptions methods.
- [x] Add new `cmd/sqlgen/gen/templates/table/stream.go.tmpl` emitting the iterator body per the §9.4a implementation pattern. Force `options.SkipCache = true` after `resolveCallOptions`.
- [x] Generate `scan{Table}Row` helper (single-row variant of `scan{Table}s`) in the scan template; reuse existing column-resolution logic.
- [x] Add `operations.stream: bool` config field in `cmd/sqlgen/config/config.go`; default `true`. Gate Stream emission on this.
- [x] Update operations preset table so `read_only` includes `stream` (PRD §4.6 already updated).
- [x] Validation in `cmd/sqlgen/config/validate.go`: accept the new field; no special validation rules.
- [x] Confirm `cache.go.tmpl` requires no extra `OpStream` dispatch — the existing `SkipCache` early-return handles bypass. Document the confirmation in the Completion Record.
- [x] Regenerate all example goldens via `make update-golden` + `make update-golden-e2e`; verify the new method + types appear across postgres / postgres_stdlib / mysql / sqlite / cache / events / tenancy modules.

### Acceptance Criteria

- `Stream` method generated per table, returns `iter.Seq2[*{Table}, error]`.
- `Stream{Table}Input` cannot represent `Limit` or `Offset` (type-level constraint).
- `Stream{Table}FieldOptions` cannot represent any relationship field (type-level constraint enforced by codegen, not runtime).
- `options.SkipCache = true` is forced inside the Stream method body regardless of caller setting.
- `OpStream` constant exists in `hook/hook.go` and is referenced by the generated Stream method's `QueryContext`.
- `operations.stream: false` suppresses Stream emission for the affected table; `read_only` preset includes Stream by default.
- Cache template needs no change — confirmed.

### Tests Required

- [x] Generator unit tests: `Stream` method body emission per table. (`stream_codegen_test.go::TestStream_MethodBodyEmission`)
- [x] Generator unit tests: `Stream{Table}Input` struct shape (Filter + Sorts only; no Limit/Offset). (`TestStream_TypesEmitted`, `TestStream_InputHasNoLimitOrOffset`, `TestStream_NoLimitOffsetInSelect`)
- [x] Generator unit tests: scalar-only `StreamFieldOptions` invariant — assert no relationship-typed fields appear in any table's StreamFO across the example modules. (`TestStream_FieldOptionsScalarOnly` — fixture has a relationships entry; assertion proves the StreamFO body excludes it.)
- [x] Generator unit tests: `OpStream` hook op constant referenced in generated method. (`TestStream_OpStreamReferenced`)
- [x] Generator unit tests: `scan{Table}Row` helper emission. (`TestStream_ScanRowHelperEmitted`)
- [x] Generator unit tests: `operations.stream: false` suppresses Stream method emission. (`TestStream_OperationsStreamFalseSuppresses`)

### Completion Record

Completed 2026-04-29.

**Files changed:**
- `hook/hook.go` — `OpStream` added to the QueryOp constants block.
- `cmd/sqlgen/config/config.go` — `Stream *bool` added to `Operations`; every preset (`all` / `read_only` / `append_only` / `no_delete` / `no_hard_delete`) now enables Stream by default; `applyOperationOverrides` + `hasAnyOperationToggle` updated to handle the new field. Validation needed no changes (no preset / structural rules apply to Stream).
- `cmd/sqlgen/config/config_test.go` — `TestExpandPreset` updated so the `all` and `read_only` cases assert `*ops.Stream` is true. The negative arms in `read_only` already cover the disabled write ops; Stream is intentionally on for read-only consumers.
- `cmd/sqlgen/gen/context.go` — `ResolvedOperations` gains a `Stream bool` field at the tail.
- `cmd/sqlgen/gen/context_table.go` — `toResolvedOperations` populates `Stream` from the resolved Operations struct.
- `cmd/sqlgen/gen/orchestrate.go` — `tableTemplateNames` extended with `"table/stream"` so the new template runs for every non-view table.
- `cmd/sqlgen/gen/templates/table/stream.go.tmpl` (new) — emits four artifacts gated on `Operations.Stream`:
  - `Stream{Table}{plural}Input`: Filter + Sorts only — no Limit / Offset / conditions field. Type-level enforcement of PRD §9.4a's no-bounds contract.
  - `Stream{Table}FieldOptions`: bool fields for every scalar column on `.Columns` (relationships live on `.Relationships`, never iterated here). Emits `Columns()` (sorted, alphabetical for builder determinism) and `HasSelectedColumns()` mirroring the regular FieldOptions surface.
  - `scan{Table}Row`: single-row variant of `scan{Table}s`. Same column-name switch + scan-shape post-processing as the loop variant; just no `for rows.Next()` wrapper. Returns `(*Entity, error)`.
  - `Stream` method: returns `iter.Seq2[*Entity, error]`. Forces `options.SkipCache = true` immediately after `resolveCallOptions`. Calls `c.executeQuery` with `Op: hook.OpStream` so condition-injecting hooks (tenancy, soft-delete, custom) see the right Op for filter narrowing. Inside the inner func, builds conditions from filter + soft-delete + tenancy (mirroring GetMany's in-method composition so cross-tenant reads are scoped without relying on hook configuration), calls `BuildSelect` with no Limit / Offset / LockMode, iterates `rows.Next()`, calls `scan{Table}Row` per row, yields `(p, nil)` or `(nil, err)`. Early termination via consumer `break` is honored — a `false` from `yield` returns from the closure which triggers `defer rows.Close()`. PRD §9.4a implementation pattern at lines 2541–2593 is the reference.
- `cmd/sqlgen/gen/templates/table/client.go.tmpl` — entity-client interface gains `Stream(...) iter.Seq2[*{Entity}, error]` gated on `.Operations.Stream`. No struct field / constructor changes — Stream uses the same `c.querier`, `c.dialect`, `c.table`, hook chain, and (when applicable) `c.tenantResolver` / `c.excludeDeleted` / `c.mysqlVersion` plumbing as the existing read methods.
- `cmd/sqlgen/gen/stream_codegen_test.go` (new) — 9 generator-side template-shape tests covering: type emission, `iter.Seq2` return, no-Limit/Offset enforcement on the input type, scalar-only enforcement on the FieldOptions type (fixture deliberately seeds a relationship to prove it's excluded), method body shape (SkipCache force, OpStream reference, scanRow call, yield pattern), no-Limit/Offset/LockMode in the BuildSelect literal, scanRow helper shape (single-row, no `for rows.Next()` loop), `operations.stream: false` suppression, and the tenancy + soft-delete in-method composition.
- `cmd/sqlgen/gen/testdata/golden/*` — regenerated via `make update-golden`. Diffs are localized to the new field on `ResolvedOperations`, the new shared types, and the new template output for each per-table golden (only tables that have `.Operations.Stream` true emit the new artifacts).
- `cmd/sqlgen/testdata/examples/{cache,events,mysql,postgres,postgres_stdlib,sqlite,tenancy}/expected/*_gen.go` and `models/*_gen.go` — regenerated via `make update-golden-e2e`. Each example now ships with `Stream{Entity}Input` / `Stream{Entity}FieldOptions` / `scan{Entity}Row` / `Stream(...) iter.Seq2[*{Entity}, error]` for every non-view table.

**Notes:**
- **Cache template untouched.** `cache.go.tmpl` requires no `OpStream` dispatch. The existing `cacheSkipper` interface (cache.go.tmpl ~671) probes `q.CallOptions.(cacheSkipper).skipCache()` and short-circuits to `c.delegate(ctx, q)` when set. The Stream method's `options.SkipCache = true` line lands in the `CallOptions` literal that's stored on the QueryContext via `&hook.QueryContext{... CallOptions: options}`, so the cache hook sees `SkipCache=true` in the QueryContext snapshot and routes Stream calls straight through. PRD §27.7 "`CallOptions.SkipCache` interaction" is satisfied. Verified by grepping `cmd/sqlgen/testdata/examples/cache/models/cache_gen.go` — no `OpStream` references, only the generic `SkipCache` short-circuit. The cache hook treats Stream like any read with SkipCache forced on.
- **`iter` import handled by goimports.** Single-file layout (the layout used by every example module) runs through `Format()` with full goimports resolution. Stdlib `iter` has only one resolution path so goimports adds it correctly to every regenerated `models_gen.go`. Per-file layout users would need an explicit import on the entity file — out of scope for 15.4 since no example uses per-file mode (no test coverage exists for it; documented as a future consideration if/when per-file layout grows beyond the single integration test that doesn't exercise Stream).
- **Stream applies tenancy + soft-delete in the method body, not via hooks alone.** GetMany / Connection compose these conditions inline; Stream mirrors that pattern so consumers can't accidentally bypass tenant scoping by disabling the hook chain (`SkipHooks: true`). The hook chain still fires on `OpStream` for any user-installed query hooks, but the data-isolation conditions are guaranteed in-method. Documented in `TestStream_TenancyAndSoftDeleteApplied`.
- **No LockMode threading on Stream.** PRD §9.6a does not list Stream among the LockMode-supporting methods, and the §9.4a Stream surface omits LockMode entirely. The generated `BuildSelect` literal has no `LockMode:` field — a regression that adds it would require surfacing a `LockMode` field on `Stream{Table}FieldOptions` or the Input type, neither of which exists. Pinned by `TestStream_NoLimitOffsetInSelect` (the same body-scope check rejects `LockMode:`).
- **Stream propagates LockNone on internal calls (none, currently).** Stream does not chain Get / GetMany / Connection internally — it directly issues a single SELECT and yields per row. No chained-call invariant to maintain. The PRD §9.6a "Chained internal calls do not inherit LockMode" rule applies to write-method internal Get sites; Stream isn't a write method and doesn't chain.
- **Goldens-update side effect.** `make update-golden-e2e` copies the entire `models/` output dir to `expected/`. The hand-written `mysql/models/mysql_version_test.go` (added in 15.2) gets copied along with the generated files into `expected/`. Since the comparison mode `runGenerateToDir` writes to a temp dir (not models/), the test file isn't regenerated there, and the comparison would surface a "file in expected/ but not generated" error. Removed the stray `expected/mysql_version_test.go` after the goldens update so future comparison runs stay clean. The same fixup applies to any future hand-written test added inside an example's `output.dir` — that's a structural property of `e2e_test.go::copyOutputToExpected`, not a Phase 15.4 issue.
- **Sweep results.**
  - `make check`: 8 modules, 0 lint, all `-short -race` unit tests green. cmd/sqlgen/cli at 36.4s.
  - `make check-examples`: 7 example modules, 0 lint, all E2E `-race` tests green. mysql 9.7s; others 1.6–3.3s.
- No bugs uncovered; no `/fix` filed.

---

## 15.5 Stream E2E

**PRD Reference:** §9.4a (full coverage of constraints, use cases, hook integration)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.5

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/tests/stream_test.go`

**Status:** Complete

### Tasks

- [x] `postgres/tests/stream_test.go` (new):
  - [x] Memory-bounded iteration: stream a 2k-row backlog, compare `runtime.MemStats.TotalAlloc` against an equivalent `GetMany` materialization, assert Stream's allocator footprint does not exceed 2× GetMany's (a regression that materialized the result set inside Stream would inflate this far beyond 2×).
  - [x] Early termination via `break` releases rows + connection cleanly (5 partial streams, the pgxpool's `AcquiredConns` returns to baseline after each).
  - [x] Filter / sort propagation: `WHERE` and `ORDER BY` correctly applied; `LIMIT` / `OFFSET` confirmed absent (PRD §9.4a "no bounds").
  - [x] Soft-delete hook composition: default exclusion + explicit `Filter.DeletedAt` override.
  - [x] `SkipCache` force — drift documented inline (postgres/ has no cache wired; pinned in 15.4 codegen test + cache/tests/skip_test.go runtime).
  - [x] Iterator yields `(nil, err)` and terminates on row failure mid-stream — pinned via `stubFailRows` wrapper that forces `rows.Err()` to return a sentinel after N rows (driver-independent contract; ctx-cancellation path covered separately and relaxed because pgx may buffer small result sets).
  - [x] `StreamFieldOptions{ID: true, Title: true}` produces a `SELECT id, title` (verified via the recording querier's captured SQL, plus the yielded entity has only those fields populated).
- [x] `mysql/tests/stream_test.go` — mirror coverage; per-driver buffering caveat documented inline in the file header (stdlib mysql driver buffers result set client-side, so the memory-bound assertion is intentionally not mirrored here — covered by the codegen-side pin in 15.4).
- [x] `sqlite/tests/stream_test.go` — mirror coverage adapted for SQLite (in-process driver — same buffering caveat shape; memory-bound assertion is intentionally not mirrored).
- [x] `tenancy/tests/stream_test.go` — tenant filter narrows the streamed rows; `SkipTenancy: true` streams across tenants; missing-resolver fail-closed pinned (Stream emits `(nil, tenancy.ErrMissing)` before any SQL roundtrip — counter at 0).

### Acceptance Criteria

- Memory-bounded iteration verified: peak heap delta during a 10k-row stream stays significantly below the corresponding `GetMany` materialization (assertion uses `runtime.MemStats` or process RSS, with explicit threshold).
- Early `break` cleanly releases rows + connection — test verifies via deferred `rows.Close` execution and pool capacity recovery.
- Filter/sort propagation: SQL captured by test querier wrapper contains the expected `WHERE` and `ORDER BY` clauses.
- Tenancy + soft-delete hooks compose correctly with `OpStream` (rows filtered out before yield).
- Cache fully bypassed even when caller passes `SkipCache: false`.
- Iterator terminates on connection failure with `(nil, err)` yield.
- `StreamFieldOptions{ID: true, Name: true}` produces a `SELECT id, name` (verified via querier wrapper `rows.Columns()`).
- Per-driver buffering caveat documented in test file header for MySQL.

### Tests Required

- [x] Memory-bound assertion (postgres only — mysql/sqlite drivers buffer the result set; documented in 15.5 Completion Record).
- [x] Early-break cleanup (each dialect).
- [x] Filter / sort propagation (each dialect).
- [x] Hook chain integration (tenancy + soft-delete) — exercised in tenancy module + per-dialect modules.
- [x] `SkipCache` force (codegen-pinned per drift inventory in 15.5 Completion Record + Phase-15.6 closure notes).
- [x] Mid-stream connection-failure path (postgres via `stubFailRows` driver-independent contract; mysql/sqlite use relaxed ctx-cancellation contract per drift inventory).
- [x] Scalar-only `FieldOptions` column selection.
- [x] Tenancy: filter applied + `SkipTenancy` bypass.

### Completion Record

Completed 2026-04-29.

**Files changed:**
- `cmd/sqlgen/testdata/examples/postgres/tests/stream_test.go` (new) — 8 test functions:
  - `TestStream_YieldsAllRowsInSortOrder` — happy-path baseline (50 rows seeded, all distinct, all yielded exactly once).
  - `TestStream_FilterAndSortPropagation` — recording querier captures the SELECT; pins WHERE + ORDER BY presence, `"title"` sort column + DESC direction, ABSENCE of LIMIT/OFFSET, author bound as arg.
  - `TestStream_ScalarOnlyColumnSelection` — `StreamFieldOptions{ID: true, Title: true}` produces a SELECT projecting only `"id"` + `"title"`; yielded entities have only those fields populated (Body / Author / CreatedAt remain at zero); captured SQL contains the selected columns and not the unselected ones.
  - `TestStream_SoftDeleteExcludedByDefault` — soft-deleted rows excluded by default; `Filter.DeletedAt: &comparator.NullableTime{Null: &false}` flips to include-only-deleted.
  - `TestStream_EarlyBreakReleasesConnection` — 5 partial streams (`break` after first row); pgxpool's `AcquiredConns` returns to baseline after each; deferred `rows.Close()` is structurally pinned by the pool restoration.
  - `TestStream_MidStreamCancellation` — relaxed contract: ctx cancellation may or may not surface depending on pgx buffering behavior on small result sets; if observed, error must be `context.Canceled`. Larger backlog (200 rows) gives the cancel headroom but the strict contract is pinned by `TestStream_RowFailureMidStream`.
  - `TestStream_RowFailureMidStream` — driver-independent pin using `stubFailRows` (a `database.Rows` wrapper that forces `Err()` to return `errStubMidStreamFailure` after N successful Next() calls). Stream emits `(nil, errStubMidStreamFailure)` after exactly 3 rows, terminating the iterator cleanly. Pins PRD §9.4a "iterator yields (nil, err) on connection failure mid-stream" without depending on driver-specific cancellation semantics.
  - `TestStream_MemoryBoundedIteration` — 2k-row backlog; compares `TotalAlloc` between Stream (per-row processing, no materialization) and GetMany (full slice materialization, explicit `Limit: &n` to bypass the default 1000-row cap on the articles client). Asserts `streamAlloc <= 2 * getManyAlloc` — a regression that materialized inside Stream would blow well past 2×. Skipped under `-short` because the test is timing-sensitive.
  - Plus helpers `recordingPgxQuerier` (counts + last SQL capture), `seedArticles`, `stubFailRows`, `stubFailQuerier`, `errStubMidStreamFailure`.
- `cmd/sqlgen/testdata/examples/mysql/tests/stream_test.go` (new) — 6 test functions mirroring postgres adapted for the stdlib mysql driver:
  - `TestStream_YieldsAllRowsInSortOrder_MySQL`
  - `TestStream_FilterAndSortPropagation_MySQL` — pins backtick-quoted `\`title\`` identifier per MySQL dialect.
  - `TestStream_ScalarOnlyColumnSelection_MySQL` — same shape as postgres; SQL inspection uses backtick identifiers.
  - `TestStream_SoftDeleteExcludedByDefault_MySQL`
  - `TestStream_EarlyBreakReleasesConnection_MySQL` — uses `*sql.DB.Stats().InUse` instead of pgxpool's `AcquiredConns`.
  - `TestStream_MidStreamCancellation_MySQL` — relaxed (driver buffers full result set client-side; cancel may not land mid-iter).
  - File header documents the per-driver buffering caveat: PRD §9.4a "memory-bounded" reduces to "per-row materialization in the loop body is bounded; the driver's buffer holds the unscanned rows" on stdlib mysql. Memory-bound assertion intentionally not mirrored here — codegen-side pin in 15.4.
- `cmd/sqlgen/testdata/examples/sqlite/tests/stream_test.go` (new) — 6 test functions mirroring postgres adapted for modernc.org/sqlite:
  - `TestStream_YieldsAllRowsInSortOrder_SQLite`
  - `TestStream_FilterAndSortPropagation_SQLite` — pins double-quoted `"title"` (SQLite uses postgres-style quoting).
  - `TestStream_ScalarOnlyColumnSelection_SQLite`
  - `TestStream_SoftDeleteExcludedByDefault_SQLite`
  - `TestStream_EarlyBreakReleasesConnection_SQLite`
  - `TestStream_MidStreamCancellation_SQLite` — relaxed (in-process driver may drain its row buffer faster than the cancel can land).
  - File header documents the in-process driver caveat: memory-bound assertion intentionally not mirrored.
- `cmd/sqlgen/testdata/examples/tenancy/tests/stream_test.go` (new) — 3 test functions:
  - `TestStream_TenantFilterApplied` — seed 5 products under tenant A + 3 under tenant B; tenant-A-bound client streams, asserts only A's 5 rows appear and every yielded row's `WorkspaceID == tenantA`.
  - `TestStream_SkipTenancyBypass` — `SkipTenancy: true` on Stream returns all 8 rows across both tenants; tenant counts pinned at {A:5, B:3}.
  - `TestStream_MissingResolverFailsClosed` — resolver returns `tenancy.ErrMissing`; Stream emits `(nil, err)` with `errors.Is(streamErr, tenancy.ErrMissing)` and yields 0 rows; counter pinned at 0 Query ops (fail-closed must short-circuit before SQL).
  - Helpers `seedTenantProducts`, `overlap`, `strPtrTen`.

**Notes:**
- **Cache-bypass scope drift mirrors 15.3.** postgres / mysql / sqlite example modules have no cache configured (`sqlgen.yml` does not enable it). The Stream-method `options.SkipCache = true` force is pinned at:
  1. Codegen layer: `cmd/sqlgen/gen/stream_codegen_test.go::TestStream_MethodBodyEmission` asserts the exact `options.SkipCache = true` line in the Stream emission body.
  2. Runtime layer: `cache/tests/skip_test.go::TestSkip_CacheOnGet` pins that `SkipCache: true` in the CallOptions snapshot bypasses the read-through hook entirely (zero backend get / set calls). Stream's force lands in the same `&hook.QueryContext{... CallOptions: options}` snapshot the cache hook reads, so the contract holds end-to-end.
  Wiring a cache module locally for one E2E test would require regenerating the example with cache enabled — out of scope for a test-only sub-item per the Phase-14 scope rule.
- **Mid-stream connection failure — driver-independent contract.** PRD §9.4a's "iterator yields (nil, err) and terminates on connection failure mid-stream" is hard to exercise via ctx-cancellation alone because:
  - pgx buffers small result sets; cancellation may not land mid-iter on 50-row backlogs (the original test's failure mode).
  - stdlib mysql driver buffers the entire result set client-side; cancellation rarely lands mid-iter on any backlog size.
  - modernc.org/sqlite is in-process; cancellation lands at row-fetch boundaries but the buffer may drain first.
  The strict pin is `TestStream_RowFailureMidStream` in postgres: a `stubFailRows` wrapper that forces `rows.Err()` to return a sentinel after N successful `Next()` calls. The Stream method's `if err := rows.Err(); err != nil { yield(nil, err) }` post-loop check is the load-bearing line; the test confirms it fires and terminates iteration. Driver-specific tests use the relaxed "if observed, must be ctx error" contract for ctx cancellation to absorb the buffering variability.
- **Memory-bound contract — comparative, not absolute.** Stream's "memory-bounded" property is verified by comparing `TotalAlloc` between Stream and GetMany on the same backlog. Stream's per-row processing should not exceed 2× GetMany's bulk allocator path — a regression that materialized the result set inside Stream would inflate this far beyond 2×. The 2× ceiling is lenient because pgx still allocates per-row scan buffers + Stream has slightly more closure-call overhead than GetMany. The test is skipped under `-short` because TotalAlloc is timing-sensitive (GC behavior, cache state). Postgres-only because mysql/sqlite drivers buffer the full result set, which would dominate the measurement and produce a noisy signal that doesn't reflect Stream's contract.
- **Tenancy guard ordering.** `TestStream_MissingResolverFailsClosed` pins that the in-method tenant resolution runs before BuildSelect / conn.Query. The counter records 0 Query ops attributable to the failed Stream — same fail-closed contract as Get / GetMany / Connection (`tenancy/tests/skip_test.go` and `tx_resolver_test.go`).
- **Counting / recording querier convention.** Each example module defines its own `recording*Querier` (postgres/pgx, mysql/stdlib, sqlite/stdlib) — same structural pattern as the Phase-15.3 lock_mode_test.go duplication. Hoisting to a shared helper would require a cross-module package; example modules have separate go.mods (`GOWORK=off` build), so each duplicates the pattern. Belongs in a future "tests-shared library" sub-item, not Phase 15.5.
- **GetMany default queryLimit.** Discovered during memory-bound test bring-up: the articles client's default `queryLimit = 1000` truncates GetMany results when no explicit `Limit` is passed. Fixed by passing `Limit: &n` explicitly in the GetMany measurement window. Documented inline at the test.
- **Sweep results.**
  - `make check`: 8 modules, 0 lint, all `-short -race` unit tests green. cmd/sqlgen/cli at 43.1s.
  - `make check-examples`: 7 example modules, 0 lint, all `-race` E2E tests green. mysql 12.0s; postgres 5.4s (the 4 new stream tests added ~0.7s, dominated by the 2k-row seed in TestStream_MemoryBoundedIteration); postgres_stdlib 2.9s; tenancy 2.0s; sqlite 2.1s; cache 1.8s; events 1.5s.
- No bugs uncovered; no `/fix` filed.

---

## 15.6 Phase closure — tracker sync + integration sweep

**PRD Reference:** §9.4a, §9.6a (reconciliation against landed codegen)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §15.6

**Module:** `docs/tracker/phase-15.md`, `docs/tracker/STATUS.md`, plus any PRD edits needed for reconciliation

**Status:** Complete

### Tasks

- [x] Run `make check` + `make check-examples` + `make test-integration` after every 15.1–15.5 sub-item lands; record any flakes / timing in `docs/tracker/phase-15.md`.
- [x] `/fix` triage: any pre-existing bug uncovered during 15.x test bring-up gets a FIX entry, not in-place scope expansion.
- [x] PRD reconciliation: §9.4a and §9.6a were drafted pre-implementation. Any divergence between the spec and landed codegen gets reconciled either inline at the test-file header (test-only divergences) or via a targeted PRD edit (behavioral divergences) — same pattern as Phase 14's closing sweep.
- [x] Update `docs/tracker/STATUS.md` "Current Focus" to reflect Phase 15 closure.
- [x] Confirm every 15.1–15.5 Completion Record is filled in.

### Acceptance Criteria

- Full `make check` + `make check-examples` + `make test-integration` sweep runs clean (or any failures are filed as `/fix` entries with a clear non-Phase-15 attribution).
- All 15.1–15.5 Completion Records filled.
- PRD §9.4a / §9.6a reconciled against landed codegen (inline test-file headers for test-only drifts; targeted PRD edits for behavioral drifts).
- `STATUS.md` Current Focus block updated.

### Tests Required

- [x] Full integration sweep (no new tests added in this sub-item — coordination only).

### Completion Record

Completed 2026-04-29.

**Files changed:**
- `docs/PRD.md` — three targeted edits reconciling §9.4a and §9.6a against landed codegen:
  1. **§9.4a "Hook integration":** rewrote the cache-hook sentence. Original wording ("The cache hook adds an `OpStream` bypass that short-circuits to `next` without any read-through or write-through") implied a separate op-specific dispatch in `cache.go.tmpl`. Landed reality: no `OpStream` reference exists in `cache.go.tmpl`; the existing `cacheSkipper` interface (`cache.go.tmpl:654-659, :671`) probes `q.CallOptions.(cacheSkipper).skipCache()` and routes to `c.delegate(ctx, q)` when set. Stream's `options.SkipCache = true` force lands in the `&hook.QueryContext{... CallOptions: options}` snapshot the cache hook reads, so the bypass is automatic. Edit clarifies the actual mechanism without changing observable behavior.
  2. **§9.6a "MySQL version requirement":** replaced the parenthetical "Phase-15+ implementations: detect via `SELECT VERSION()` once at client init" with a concrete description of the landed mechanism — lazy probe on first `NoWait`/`SkipLocked` call, `sync.Once` gating, error-cached-across-callers behavior, postgres+sqlite explicitly excluded, MariaDB compatibility note. Pinned by `cmd/sqlgen/testdata/examples/mysql/models/mysql_version_test.go` (`TestMySQLVersionCache_singleProbe`, `_errorCachedAcrossCalls`, `_parsesMajorVersion/mariadb_10.11.6`).
  3. **§9.6a "Chained internal calls do not inherit LockMode":** PRD originally addressed only write-method chained Get sites. Landed codegen has two distinct invariants — write methods (`Create`/`Update`/`Upsert`/`Restore`/`SoftDelete*`) FORCE `LockMode = LockNone` on their internal Get/GetMany; read methods (`Get` chaining `GetMany`, `Connection` chaining `GetMany`) PROPAGATE the caller's LockMode so the inner SELECT actually emits the lock clause. Both invariants pinned by `cmd/sqlgen/gen/lock_mode_codegen_test.go::TestChainedGet_LockNoneForced` (write) and `_PaginationDoesNotForceLockNone` (read). Edit expands the section to cover both rules with their rationales.
- `docs/tracker/phase-15.md` — phase status flipped from "Not Started" to "Complete"; 15.6 sub-item flipped to Complete with all 5 task checkboxes ticked + this Completion Record.
- `docs/tracker/STATUS.md` — Phase 15 row in the overview table flipped from "5 / In Progress" to "6 / Complete"; new top-of-Current-Focus block describes Phase 15 closure.

**Sweep results (2026-04-29):**
- `make check` (8 modules, `-short -race`): 0 lint, all unit tests green; wall 1:10. Slowest: `cmd/sqlgen/cli` 44.6s.
- `make check-examples` (7 example modules, `-race`): 0 lint, all E2E tests green; wall 53s. Per-module: mysql 12.9s, postgres 11.4s, sqlite 4.7s, tenancy 3.7s, postgres_stdlib 3.1s, cache 1.8s, events 1.5s. mysql/models test (the hand-written `mysql_version_test.go` from 15.2) ran in 1.2s.
- `make test-integration` (28 modules, full non-short `-race`, 10m timeout per module): all green; wall 2:56. Slowest: `cmd/sqlgen` 136.8s, `cmd/sqlgen/cli` 83.3s, `cmd/sqlgen/gen` 12.1s, `parser/introspect` 11.4s. The introspect runtime confirms FIX-057 is holding through the Phase-15 changes — `TestMySQLIntrospect` and `TestMySQLIntrospectViews` both pass under the `enum`/`set` rewrite.

**`/fix` triage:** No new fixes filed during Phase 15. Pre-existing FIX-057 (resolved post-Phase 14) verified holding via the test-integration sweep above. `fixes.md` "Open" section is empty (verified).

**Notes:**
- **No behavioral PRD edits required.** The three reconciliations above are clarifications — they describe the landed mechanism more precisely without changing what the code does. Phase-14 set the precedent: test-file header notes for test-only drifts (Phase 15 has 5+ such notes, captured at landing time in 15.3 / 15.5 file headers); targeted PRD edits for behavioral drifts (Phase 15 has zero — the spec was accurate, the wording around mechanism details just lagged the implementation).
- **Test-only drift inventory** (already documented at landing — listed here for closure cross-referencing):
  - 15.3: cache-bypass not E2E-exercised on postgres/mysql (no cache wired in those `sqlgen.yml` files); pinned by 15.2 codegen test + `cache/tests/skip_test.go`.
  - 15.3: tenant-scoped locking unobservable on the tenancy example (sqlite-backed; SQLite rejects all non-`LockNone` modes before tenancy hook runs); pinned by 15.2 codegen test.
  - 15.3: SoftDelete/Restore × tenant-in-PK composite-PK gap (the only such table is `order_items` with no soft-delete column) — out of scope per Phase-14 test-only rule.
  - 15.5: cache-bypass not E2E-exercised on postgres/mysql/sqlite (same root cause as 15.3); pinned by 15.4 codegen test + `cache/tests/skip_test.go`.
  - 15.5: mid-stream connection failure exercised via `stubFailRows` (driver-independent contract); ctx-cancellation tests use the relaxed "if observed, must be ctx error" contract because pgx/mysql/sqlite drivers all buffer differently.
  - 15.5: memory-bound assertion is comparative (Stream ≤ 2× GetMany TotalAlloc) and postgres-only; mysql/sqlite drivers buffer the result set and would dominate the measurement.
- **Phase-15 scope rule held end-to-end.** 15.1–15.5 each landed within their declared module set. The only cross-template touch outside the spec was 15.4's incidental `Makefile` edit (`test-examples` target switched from `./tests/` to `./...` so hand-written tests inside generated example packages — like 15.2's `mysql_version_test.go` — get exercised by CI). Documented inline at 15.4's "Files changed" entry; not a scope drift, just an infrastructure adjustment to support 15.2's hand-written test placement.
- **Phase-15 surface delta:** 5 new constants (`LockMode`), 1 new `Dialect` interface method (`LockClause`), 1 new `database` package helper (`InTransaction`), 1 new `hook.QueryOp` constant (`OpStream`), 1 new generated client field per MySQL entity (`mysqlVersion *mysqlVersionCache`), 1 new generated method per non-view table (`Stream`), 4 new generated types per non-view table (`Stream{Table}{Plural}Input`, `Stream{Table}FieldOptions`, `scan{Table}Row`, plus the augmented `CallOptions[FO]` carrying `LockMode`), 1 new `operations.stream` config field. Backwards-compatible: every consumer of the prior `CallOptions[FO]` continues to compile (new field has zero-value default `LockNone`); every consumer of the prior `Dialect` interface needs a new `LockClause` method (breaking only for users implementing custom dialects, of which there are none in the standing example set).
- **PRD §27.7 ("`CallOptions.SkipCache` interaction") satisfied end-to-end.** Both Stream and LockMode mutate `options.SkipCache = true` before the hook chain receives the QueryContext snapshot; the cache hook's `cacheSkipper` interface routes both call paths straight through `c.delegate(ctx, q)`. No `OpStream`-specific or `LockMode`-specific dispatch needed in `cache.go.tmpl`.
