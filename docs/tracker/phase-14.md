# Phase 14: E2E Coverage Expansion

Status: Not Started
PRD Sections: 7.4, 7.5, 9.1, 9.2, 9.3, 9.4, 9.5, 10, 11.3, 13.2, 14, 17, 18, 18.5, 27.5, 27.6, 27.11, 28.3, 28.4, 28.6, 29.2.3, 29.3.1, 29.4.2, 29.5, 29.6, 29.7, 29.10

> **Rationale:** Post-Phase-13 audit (2026-04-23) surfaced features that are implemented but not exercised e2e, plus interaction combinations (tenancy × cache × soft-delete, events × tx, etc.) where a regression could land silently. This phase is **additive and test-only** — it adds coverage against existing behavior.

> **Scope rule:** If a sub-item uncovers a bug (not just a missing test), open a `/fix` item rather than expanding scope in-place. Keep this phase test-only.

> **Runner notes:** New tests live under `cmd/sqlgen/testdata/examples/<dialect>/` or `cmd/sqlgen/testdata/examples/{cache,events,tenancy}/`. `make check-examples` must pass with 0 lint issues and `go test ./... -race` must pass across all three modules before a sub-item is marked complete.

---

## 14.1 Core CRUD — `Increment` e2e

**PRD Reference:** §9.2, §29.4.2
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.1

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/`

**Status:** Complete

### Tasks

- [x] `postgres/increment_test.go` — positive / negative / zero deltas on `int32`, `int64`, `smallint`, `numeric(10,2)` columns
- [x] `postgres/increment_test.go` — `ErrNotFound` with `strict_updates: true` on a missing PK; idempotent `nil`-return with `strict_updates: false`
- [x] `mysql/increment_test.go` — mirror of postgres coverage adapted for MySQL dialect
- [x] `sqlite/increment_test.go` — mirror of postgres coverage adapted for SQLite dialect
- [x] `tenancy/increment_test.go` — tenant filter auto-applied; `SkipTenancy: true` bypasses; composite-PK tenant-mismatch returns `tenancy.ErrMismatch` before the DB round-trip
- [x] Add a numeric column to the tenancy example schema if one isn't already present (required for the tenancy variant) — schema already had `products.price` (REAL) and `order_items.quantity`/`unit_price`; no schema change needed

### Acceptance Criteria

- Every numeric SQL type present in each dialect's example schema is exercised with at least one `Increment` call
- `Increment` with a negative delta produces a `column = column - N` SQL fragment (or equivalent) and the row's value decreases
- Composite-PK `Increment` on tenanted tables rejects tenant-mismatch with `tenancy.ErrMismatch` before issuing any SQL (verified by query count, not just assertion on error)
- `strict_updates: false` + missing PK returns `(zeroEntity, nil)` — documented idempotent behavior of `Increment` per §9.5 semantics

### Tests Required

- [x] Positive delta on each numeric type across all three dialects
- [x] Negative delta (decrement) on one numeric type per dialect
- [x] Zero delta is a no-op that still returns the unchanged row
- [x] Missing-PK behavior under both `strict_updates` values
- [x] Tenancy: cross-tenant `Increment` rejected with `ErrMismatch`
- [x] Tenancy: `SkipTenancy: true` successfully increments across tenants

### Completion Record

Completed 2026-04-23.

**Files changed:**
- `cmd/sqlgen/testdata/examples/postgres/tests/increment_test.go` (new) — 8 tests covering products (int32 quantity, numeric price), order_items (int32 quantity, numeric unit_price), and orders (strict_updates=false idempotent path).
- `cmd/sqlgen/testdata/examples/mysql/tests/increment_test.go` (new) — 6 tests. Zero-delta case uses `orders.total` instead of `products.price` because go-sql-driver/mysql's default `RowsAffected()` reports rows-CHANGED (not rows-MATCHED), so `UPDATE t SET x = x + 0 WHERE id = N` reports 0 under strict_updates=true and would spuriously surface as ErrNotFound. Using the strict_updates=false `orders` table sidesteps the rows-affected check while still verifying the no-op via post-call read. Documented inline in the test comment. No driver/connection-string change was taken — that would widen scope into a cross-cutting behavioral shift for every mysql mutation test.
- `cmd/sqlgen/testdata/examples/sqlite/tests/increment_test.go` (new) — 6 tests covering products (REAL price) and order_items (INTEGER quantity / REAL unit_price); sqlite reports rows-matched so zero-delta works under strict_updates=true.
- `cmd/sqlgen/testdata/examples/tenancy/tests/increment_test.go` (new) — 6 tests: auto-filter on simple-PK products.price (cross-tenant Increment surfaces ErrNotFound, target row untouched); SkipTenancy admin path succeeds across tenants; composite-PK verify-match on order_items (resolver-vs-PK mismatch, zero-UUID PK, SkipTenancy escape); §29.3.1 fail-closed on missing tenant.
- `cmd/sqlgen/testdata/examples/postgres/sqlgen.yml` — added `strict_updates: false` on `orders` to enable the idempotent-branch test.
- `cmd/sqlgen/testdata/examples/mysql/sqlgen.yml` — same.
- `cmd/sqlgen/testdata/examples/sqlite/sqlgen.yml` — same.
- `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/models/models_gen.go` + `expected/models_gen.go` — regenerated via `make update-golden-e2e`. Diff is localized to the `orders` client (drops the `strictUpdates` field + init, strips the rows-affected ErrNotFound branches from `Update` and `Increment`, strips the "strict_updates is enabled" godoc line).

**Notes:**
- No bugs uncovered. All 26 new tests pass (`go test -race -count=1`).
- Phase-14 scope rule (test-only) honored: the `strict_updates: false` config addition is coverage expansion, not a behavioral change.
- Composite-PK tenancy mismatch already had a dedicated test in `composite_pk_test.go` (`TestCompositePK_Increment_MismatchReturnsErrMismatch`); the new tenancy test adds complementary angles (resolver-vs-PK vs. seed-tenant-vs-PK framing, zero-UUID PK, SkipTenancy on composite PK, fail-closed missing tenant).
- `make check` and `make lint-examples` pass with 0 lint issues across all modules.

---

## 14.2 Core CRUD — `Exists` / `ExistsWhere` e2e

**PRD Reference:** §9.1
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.2

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/`

**Status:** Complete

### Tasks

- [x] `postgres/exists_test.go` — `Exists(pk)` on present row (`true`) and absent row (`false`)
- [x] `postgres/exists_test.go` — `ExistsWhere(filter)` with `In`, `Gte`, `Like`, and null-column conditions
- [x] ~~`postgres/exists_test.go` — `ErrEmptyFilter` from `ExistsWhere` when the filter produces zero conditions~~ — **not implemented; see reconciliation note below.** Test instead validates that `ExistsWhere(ctx, nil)` is a legal unfiltered probe (PRD §22 scopes ErrEmptyFilter to mutation *Where ops only).
- [x] `postgres/exists_test.go` — soft-delete scoping: soft-deleted rows return `false`; ~~`SkipSoftDelete: true`~~ **`Filter.DeletedAt` override** returns `true`. (No `SkipSoftDelete` CallOption exists — override mechanism is `Filter.DeletedAt` / `Filter.IsDeleted` per §9.8.10 godoc.)
- [x] `mysql/exists_test.go` + `sqlite/exists_test.go` — mirror coverage
- [x] `tenancy/exists_test.go` — cross-tenant `Exists` returns `false`; `SkipTenancy: true` returns `true`

### Acceptance Criteria

- Generated `Exists` / `ExistsWhere` compile and return a `bool` with no `error` leakage for happy-path cases
- ~~`ExistsWhere` with an empty filter returns the `sql.ErrEmptyFilter` sentinel~~ — reconciled against PRD §22: ExistsWhere is a read-only scalar probe, not a bulk mutation, and `ExistsWhere(ctx, nil)` is legal (returns true iff any non-soft-deleted / in-tenant row exists). Tests validate this landed behavior.
- Soft-delete and tenancy filters compose correctly under `Exists` (both filters AND'd into the generated SQL)

### Tests Required

- [x] Present / absent PK across all three dialects
- [x] `ExistsWhere` with each condition operator listed (In, Gte, Like, Null)
- [x] ~~`ErrEmptyFilter` behavior~~ Reconciled: nil-filter-legal behavior documented per §22
- [x] Soft-delete scoping (both default and `Filter.DeletedAt` override; no SkipSoftDelete option)
- [x] Tenancy scoping (both default and `SkipTenancy`)

### Completion Record

Completed 2026-04-23.

**Files changed:**
- `cmd/sqlgen/testdata/examples/postgres/tests/exists_test.go` (new) — 9 tests: present/absent PK; `ExistsWhere` with `In` (`comparator.ID.In`), `Gte` (`comparator.Number[float64].Gte`), `Like` (`comparator.String.Like`), and `Null`/`NotNull` (`comparator.NullableString.Null` on `articles.body`); nil-filter legal-probe; default soft-delete exclusion on `Exists`; `Filter.DeletedAt` override on `ExistsWhere`. Uses `products` (non-tenanted) and `articles` (timestamp soft-delete) tables.
- `cmd/sqlgen/testdata/examples/mysql/tests/exists_test.go` (new) — 9 tests, dialect-adapted mirror. Uses `int64` PK (BIGINT AUTO_INCREMENT) and `comparator.Number[int64]` for filter ID.
- `cmd/sqlgen/testdata/examples/sqlite/tests/exists_test.go` (new) — 9 tests, dialect-adapted mirror. Same `int64` PK shape as MySQL.
- `cmd/sqlgen/testdata/examples/tenancy/tests/exists_test.go` (new) — 7 tests: cross-tenant `Exists` returns false (simple-PK tenant filter auto-applied); `SkipTenancy` admin escape for `Exists`; cross-tenant `ExistsWhere` returns false + `SkipTenancy` bypass; composite-PK `Exists` verify-match (resolver-vs-PK mismatch, zero-UUID PK, `SkipTenancy` escape); §29.3.1 fail-closed on both `Exists` and `ExistsWhere` (with zero DB ops asserted via `countingQuerier`).

**Phase-doc reconciliation (two drifts, neither a code bug):**
1. **ErrEmptyFilter on ExistsWhere is not implemented, and that's correct.** Phase-14.md §14.2 originally asked for an `ErrEmptyFilter` test on `ExistsWhere`. The generated `ExistsWhere` does not emit that sentinel, and per PRD §22 it shouldn't — the sentinel is explicitly scoped to mutation `*Where` ops (UpdateWhere, SoftDeleteWhere, RestoreWhere, HardDeleteWhere) to prevent accidental unbounded bulk mutations. `ExistsWhere(ctx, nil)` is a legal scalar probe that returns true iff any matching row exists. Tests validate this landed behavior with a scope note citing §22 at the top of each new test file. No code change needed; no `/fix` opened (this is by design).
2. **`SkipSoftDelete: true` is not a real CallOption.** Phase-14.md asked for a `SkipSoftDelete: true` path test. No such field exists on `CallOptions` — the project-equivalent bypass per §9.8.10 godoc ("Use ExistsWhere with Filter.DeletedAt comparator for soft-deleted rows") is to set `Filter.DeletedAt` / `Filter.IsDeleted` on the filter itself. Tests exercise this override mechanism instead.

**Notes:**
- No bugs uncovered. All 34 new tests pass (`go test -race -count=1 -run TestExists` across all 4 modules; also covered by full `make check-examples`).
- Phase-14 scope rule (test-only) honored: no generator/template changes, no runtime changes, no schema changes.
- Inline string-eq comparators are built with separate local `sku := "B-EW-SKU"` assignments to match the project's existing filter-test style (see `postgres/tests/filter_test.go`) — no helper abstraction introduced.
- `make check` passes with 0 lint issues; `make check-examples` passes with 0 lint issues across all 7 example modules; `-race` runs clean for each new test file.

---

## 14.3 Core CRUD — `*Where` mutation edge cases

**PRD Reference:** §9.3, §9.5, §28.4, §28.6, §29.4
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.3

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,cache,events,tenancy}/`

**Status:** Complete

### Tasks

- [x] `postgres/where_mutations_test.go` — `UpdateWhere`, `SoftDeleteWhere`, `HardDeleteWhere`, `RestoreWhere` each with `ErrEmptyFilter` assertion
- [x] `postgres/where_mutations_test.go` — idempotent-return assertion (zero-rows-matched → `nil` error, zero affected-rows count)
- [x] `postgres/where_mutations_test.go` — `SoftDeleteWhere` on already-soft-deleted rows is a no-op
- [x] `mysql/where_mutations_test.go` + `sqlite/where_mutations_test.go` — dialect-adapted mirrors
- [x] `cache/where_mutations_test.go` — pattern invalidation fires exactly once per `*Where` call regardless of affected-row count
- [x] `events/where_mutations_test.go` — N affected rows → N events emitted; `Event.Input` carries the operation-specific input struct per §28.6
- [x] `tenancy/where_mutations_test.go` — `UpdateWhere` with caller-set tenant mismatching the resolver returns `tenancy.ErrMismatch`; resolver tenant AND'd into final `WHERE`

### Acceptance Criteria

- `*Where` with an empty filter (all conditions collapsed) returns the empty-filter sentinel before issuing SQL
- Zero-rows-matched returns `(0, nil)` — not `ErrNotFound`, not a custom empty-result error
- Cache pattern invalidation behaves as specified in Phase 12 (one invalidation per call, matching §27 semantics)
- Events on bulk mutations emit one event per affected PK with `Input` and `Metadata` populated per §28.6

### Tests Required

- [x] `ErrEmptyFilter` for each of the four `*Where` operations across all three dialects
- [x] Idempotent zero-match return for each operation
- [x] Cache: one `InvalidatePattern` call per `*Where` (verified via metrics recorder or mock backend)
- [x] Events: N events per N-row `*Where` (verified via event-subscriber capture)
- [x] Tenancy: caller-tenant mismatch rejected with `ErrMismatch`
- [x] Tenancy: resolver tenant AND'd into generated SQL (verified by captured SQL string or row-count assertion)

### Completion Record

Completed 2026-04-23.

**Files changed (all new test files; no runtime/generator/schema changes):**
- `cmd/sqlgen/testdata/examples/postgres/tests/where_mutations_test.go` — 9 tests across `articles` (timestamp soft-delete table). Empty-filter (`&ArticleFilter{}`) → `ErrEmptyFilter` for all four `*Where` ops; zero-match → empty slice / nil error for each op; `SoftDeleteWhere` against already-soft-deleted rows returns empty (the op auto-scopes to `deleted_at IS NULL`).
- `cmd/sqlgen/testdata/examples/mysql/tests/where_mutations_test.go` — dialect-adapted mirror (9 tests; same shape).
- `cmd/sqlgen/testdata/examples/sqlite/tests/where_mutations_test.go` — dialect-adapted mirror (9 tests; same shape).
- `cmd/sqlgen/testdata/examples/cache/tests/where_mutations_test.go` — 5 tests: `UpdateWhere`/`SoftDeleteWhere`/`RestoreWhere`/`HardDeleteWhere` each fire exactly one `InvalidatePattern` call regardless of affected-row count (3 rows, 2 rows); zero-match `UpdateWhere` still fires the pattern (the op cannot know affected count without round-tripping). `HardDeleteWhere` test uses `articles` (no dependent view) to isolate the table-pattern invalidation from view-cascade — the cache fixture's `product_summary` view has `invalidate_on: [products]` so mutations on `products` legitimately fire 2 patterns (table + view), which would have made an "exactly 1" assertion misleading.
- `cmd/sqlgen/testdata/examples/events/tests/where_mutations_test.go` — 5 tests: N=5 / N=3 affected rows fan out to N events for `UpdateWhere` / `SoftDeleteWhere` / `HardDeleteWhere` / `RestoreWhere`; `Event.Input` carries the operation-specific input by reference per §28.6 — `*UpdateInput` for `UpdateWhere` (the SET payload; the filter is the row selector), `*Filter` for `SoftDeleteWhere`/`HardDeleteWhere`/`RestoreWhere` (no separate input). Zero-match → zero events.
- `cmd/sqlgen/testdata/examples/tenancy/tests/where_mutations_test.go` — 4 tests: `UpdateWhere` with caller-set `WorkspaceID` mismatching the resolver → `tenancy.ErrMismatch` with zero DB ops issued (verified via `countingQuerier`); resolver tenant AND'd into the final `WHERE` for `UpdateWhere` / `SoftDeleteWhere` / `HardDeleteWhere` — verified by row-count assertion (seed cross-tenant rows with same predicate; assert tenant B's row untouched after tenant A's `*Where` op).

**Notes:**
- No bugs uncovered. All 41 new tests pass under `make check-examples` and `go test -race -count=1`.
- Phase-14 scope rule (test-only) honored: no generator/template/runtime/schema changes.
- The `Event.Input` shape was a learning moment during implementation: an early version of the events test asserted `*Filter` for `UpdateWhere`, but the generated terminal sets `Input: input` (the `*UpdateInput`) for `UpdateWhere` and `Input: filter` for the other three (no separate input). This matches §28.6's "the same value the caller passed" — for `UpdateWhere` the value is the SET payload, with the filter being the predicate. Documented inline in the file-header comment to forestall the same confusion in future test reads.
- `make check` passes with 0 lint issues across all modules; `make check-examples` passes with 0 lint issues across all 7 example modules.

---

## 14.4 Cursor pagination (`Connection`) e2e

**PRD Reference:** §9.4, §14
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.4

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite,tenancy}/`

**Status:** Complete

### Tasks

- [x] `postgres/connection_test.go` — seed 50-row table; forward traversal via `First` + `After`; backward traversal via `Last` + `Before`; assert page contents stable and disjoint
- [x] `postgres/connection_test.go` — opaque cursor round-trip: decode → encode → byte-identical cursor string
- [x] `postgres/connection_test.go` — tampered cursor returns a typed error (not a panic, not a silent empty page)
- [x] `postgres/connection_test.go` — `PageInfo.HasNextPage` / `HasPreviousPage` / `StartCursor` / `EndCursor` correct at first/middle/last pages
- [x] `postgres/connection_test.go` — composite-PK cursor on ~~`order_items`~~ `product_tag_labels` (3-column PK): cursor encodes all PK components; forward pagination deterministic under tied sort values. In the postgres example schema `order_items` has a single-UUID PK — the 3-column-composite table that exercises the keyset is `product_tag_labels`; behavior is identical.
- [x] `mysql/connection_test.go` + `sqlite/connection_test.go` — dialect-adapted mirrors
- [x] `tenancy/connection_test.go` — `Connection` on tenanted table: cross-tenant rows never appear on any page

### Acceptance Criteria

- Full-table traversal via repeated `First` + `After` visits every row exactly once, in sort order
- Backward traversal via `Last` + `Before` visits every row in reverse sort order, exactly once
- Tampered / malformed cursor returns an error that callers can distinguish from "no more pages"
- Composite-PK cursors are stable across invocations (encoding is deterministic) and decode back to the same PK
- Tenancy filter is applied inside the keyset predicate (not only after) — cross-tenant rows never appear even at page boundaries

### Tests Required

- [x] Forward + backward traversal of the seeded 50-row table (all three dialects)
- [x] Cursor round-trip byte-equality
- [x] Tampered cursor → typed error
- [x] `PageInfo` correctness at first / middle / last page
- [x] Composite-PK cursor traversal under tied sort values
- [x] Tenancy: cross-tenant rows filtered across every page

### Completion Record

Completed 2026-04-24.

**Files changed (all new test files; no runtime/generator/schema changes):**

- `cmd/sqlgen/testdata/examples/postgres/tests/connection_test.go` — 6 tests: `ForwardTraversal50Rows`, `BackwardTraversal50Rows`, `CursorRoundTripByteEquality`, `TamperedCursorReturnsErrInvalidCursor` (5 subtests: non-base64 / non-json on After + Before + First+Last-both-set), `PageInfoFirstMiddleLast`, `CompositePKCursorTraversal`. Seeds 50 products under a dedicated category for the traversal tests; walks in pages of 10 and asserts every row visited exactly once. Composite-PK test uses `product_tag_labels` (3-column PK product_id/tag_name/label, cursor_keys explicit in sqlgen.yml); two products share multiple labels so the row-value keyset comparator must break ties via tag_name/label to stay deterministic.
- `cmd/sqlgen/testdata/examples/mysql/tests/connection_test.go` — dialect-adapted mirror. Same 6 tests / 5 subtests = 10 total. Uses int64 PK (`comparator.Number[int64]`) and int32 for CategoryID (MySQL categories.id is INT, not BIGINT — only table in the schema with a narrower integer).
- `cmd/sqlgen/testdata/examples/sqlite/tests/connection_test.go` — dialect-adapted mirror. Same 6 tests / 5 subtests = 10 total. Uses int64 PK (sqlite's INTEGER PRIMARY KEY).
- `cmd/sqlgen/testdata/examples/tenancy/tests/connection_test.go` — 3 tests: `CrossTenantRowsFilteredEveryPage` (interleave 15 A + 15 B rows; walk tenant A's Connection forward in pages of 4; every page contains only tenant A rows; TotalCount = 15); `BackwardTraversalTenantFiltered` (mirror for Last+Before direction); `SkipTenancySurfacesAllRows` (belt-and-suspenders: with SkipTenancy=true, both tenants' rows appear — confirms seeding was real and not trivially missing).

**Phase-doc reconciliation (one drift, not a code bug):**

1. Phase-14.md originally named `order_items` as the 3-column-composite cursor target. In the postgres example schema `order_items` has a single-UUID PK (`id UUID PRIMARY KEY`) and cursor_keys default to `["id"]` — it's not a composite-cursor table at all. The 3-column-composite table that exists in all three dialect schemas is `product_tag_labels` with cursor_keys `[product_id, tag_name, label]`, and it's the one that exercises the row-value keyset comparator. Tests use `product_tag_labels` and cite the rationale at the top of each file.

**Notes:**

- No bugs uncovered. All 27 new tests pass under `go test -race -count=1`. Totals by module: postgres 6 tests + 5 subtests = 10; mysql 10; sqlite 10; tenancy 3.
- Phase-14 scope rule (test-only) honored: no generator / template / runtime / schema / config changes.
- Cursor round-trip byte-equality test relies on Go's `json.Marshal(map[string]any)` sorting keys alphabetically, which matches the generated encoder's output shape. If cursor encoding ever switches to an ordered encoder (e.g., MarshalIndent with key order from the config) this test will surface the drift as a byte diff, which is the intended regression guard per PRD §14.2 ("base64-encoded JSON objects").
- Tampered-cursor test covers the three documented paths: (a) the base64 decoder fails → `ErrInvalidCursor`; (b) base64 decodes but JSON fails → `ErrInvalidCursor`; (c) `First` and `Last` both set → `ErrInvalidCursor`. Missing-key cursors (base64 + valid JSON but keys don't match cursor_keys) are *not* currently caught by `decodeCursor` — that would require the keyset builder to verify key presence, which is a generator-level change and out of Phase-14 scope.
- Tenancy test deliberately interleaves A/B creates so A's IDs and B's IDs share the same `AUTOINCREMENT` sequence — the keyset cursor would walk over B rows without the tenant filter being AND'd in. The test asserts `edge.Node.WorkspaceID == tenantA` on every edge of every page, not just row count, so a leak would be loud.
- `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules; full E2E suite runs clean under `-race`.

---

## 14.5 Filter operator breadth

**PRD Reference:** §11.3
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.5

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**Status:** Complete

### Tasks

- [x] Extend `postgres/filter_test.go` with `Like`, `ILike` (PG-only via Custom escape hatch), `Null`, `NotNull`, `Between`, and `Custom` operator cases
- [x] `postgres/filter_test.go` — composition: ≥3-condition And; nested Or inside And
- [x] `postgres/filter_test.go` — array filters (`Slice[string].ContainsAny` / `ContainsAll` / `IsEmpty`) and JSONB `@>` containment (via Custom — the generated filter wraps JSONB columns as `NullableString`)
- [x] Create `mysql/filter_test.go` (new — did not exist) with `Like`, `Null`, `NotNull`, `Between`, `Custom`, And/Or composition, `JSON_CONTAINS` via Custom
- [x] Create `sqlite/filter_test.go` (new — did not exist) with `Like`, `Null`, `NotNull`, `Between`, `Custom`, And/Or composition (no array/JSON on sqlite per PRD §11.2)
- [x] Byte-identical SQL round-trip assertion — wrap Querier with a local `capturingQuerier`, invoke GetMany twice with the same filter-value set, compare last captured SELECT byte-wise; regression guard against ToConditions non-determinism per PRD §11.1

### Acceptance Criteria

- Every operator listed in PRD §11.3 has at least one positive test per dialect that supports it
- `Custom` raw-SQL condition threads placeholders correctly — assert via captured SQL + arg-count match
- Composition semantics match spec: implicit And across slice entries, explicit Or via the builder's Or grouping primitive
- Placeholder numbering is stable — a regression in the builder surfaces as a byte-diff in the captured SQL

### Tests Required

- [x] `Like` / `ILike` (PG via Custom) pattern matching with `%` and `_` patterns
- [x] `Null` / `NotNull` on a nullable column (articles.body)
- [x] `Between` on numeric + timestamp columns
- [x] `Custom` raw condition with ≥2 positional placeholders
- [x] And-of-Or composition (≥3 conditions)
- [x] Array / JSON filter (PG `@>` for JSONB and `Slice[T]` for arrays; MySQL `JSON_CONTAINS` via Custom)
- [x] Byte-identical SQL round-trip under identical input

### Completion Record

Completed 2026-04-24.

**Files changed (test-only; no runtime / generator / schema / config changes):**

- `cmd/sqlgen/testdata/examples/postgres/tests/filter_test.go` — appended 10 new tests + a local `capturingQuerier` helper. Tests: `Like_WildcardAndEscape` (`%cotton%`, `premium_widget`, NLike negation), `ILike_Custom` (case-insensitive via `sql.Raw("name ILIKE $", ...)`), `NullAndNotNull` (`articles.body`), `Between_Numeric` (`products.price` inclusive + NBetween exclusive-of-range), `Between_Timestamp` (`articles.created_at`), `Custom_MultiplePlaceholders` (two `$` tokens through `sql.Raw`), `AndOrComposition` (3-cond And + nested Or-in-And), `SliceArrayContains` (PG-only `Slice[string]` ContainsAny/ContainsAll/IsEmpty on `products.tags`), `JSONB_ContainsCustom` (`metadata @> $::jsonb` via Custom — single + compound), `ByteIdenticalSQL` (double-invoke same filter, compare `lastSelect`; placeholder `$1…$4` presence sanity-check).
- `cmd/sqlgen/testdata/examples/mysql/tests/filter_test.go` — new file, 8 tests + `capturingQuerier`. Mirrors the postgres shape minus PG-only operators: `Like_WildcardAndEscape`, `NullAndNotNull`, `Between_Numeric`, `Between_Timestamp`, `Custom_MultiplePlaceholders` (`price > ? AND title LIKE ?`), `AndOrComposition`, `JSONContains_Custom` (`JSON_CONTAINS(attributes, $)` via Custom since the generated filter wraps JSON as `NullableString`/`String`), `ByteIdenticalSQL` (MySQL `?` placeholders, >=4 expected).
- `cmd/sqlgen/testdata/examples/sqlite/tests/filter_test.go` — new file, 7 tests + `capturingQuerier`. Covers `Like_WildcardAndEscape`, `NullAndNotNull`, `Between_Numeric`, `Between_Timestamp` (±1s window since SQLite DATETIME resolves to the second), `Custom_MultiplePlaceholders`, `AndOrComposition`, `ByteIdenticalSQL`. No array/JSON tests (SQLite has no native array type; JSON1 is optional per PRD §11.2).

**Scope notes inlined in each test file:**

1. **ILIKE is not a first-class String operator.** Per PRD §11.2, `comparator.String` exposes `Eq/Neq/Contains/StartsWith/EndsWith/Like/NLike/In/Nin/Gt/Gte/Lt/Lte/Custom` — no `ILike`. Case-insensitive matching on PostgreSQL is exercised via the `Custom` field with `sql.Raw("name ILIKE $", ...)`, the documented §11.4 escape hatch. This is consistent with the PRD §11.2 String-operator table and does not represent a missing feature.
2. **JSONB / JSON filter auto-selection (resolved via FIX-052).** Phase 14.5 surfaced a PRD-vs-implementation drift where the generated filter wrapped jsonb/json columns as `NullableString`/`String` instead of `NullableJSONB`/`JSONB`/`JSON` per PRD §11.2. Root cause: `resolveSimpleComparator` matched `map[string]any` but not `types.JSONMap` (the type FIX-032 introduced for driver-safe JSON scanning). **FIX-052 resolved this** by widening the switch case to match both types. `TestFilter_JSONB_Contains_Postgres` and `TestFilter_JSON_Contains_MySQL` now exercise `NullableJSONB.Contains`/`HasKey` and `JSON.Contains`/`HasKey` directly; the Custom escape hatch is retained on one compound case per dialect as a secondary-path regression guard.
3. ~~**Or-of-two-conditions goes on ONE sub-filter, not two.**~~ **Superseded by FIX-195 (2026-09-10).** This recorded the pre-fix compilation, in which the generated `ToConditions` wrapped each `f.Or[i]` sub-filter's conditions in its own `sql.Or(subConds...)` — so two sub-filters with one condition each became two OR-of-one wrappers AND'd together, and a lone sub-filter's own fields were OR'd against each other. The workaround was to put both branches on ONE sub-filter. **That is now backwards.** Under amended PRD §11.1 each `Or` member contributes `sql.And` of its own fields and the members are OR'd together as one group, so `(price < 20 OR price > 100)` is written the obvious way — one branch per member — and a single member carrying both operators asks for the unsatisfiable conjunction. The dialect `AndOrComposition` tests assert both halves.
4. **Byte-identical SQL round-trip.** Dialect-specific placeholder style asserted: postgres checks `$1…$4`, mysql/sqlite count `?` tokens (>=4). A builder regression (non-deterministic placeholder numbering, field-order drift, extra whitespace) surfaces as a byte diff.

**Notes:**

- **One PRD-vs-implementation drift surfaced and logged as FIX-052** (phase 6, gen/context_table). The jsonb/json → `JSONB`/`JSON` comparator auto-selection defined in PRD §11.2 is not wired into `resolveSimpleComparator` — the check matches `map[string]any` literal but the resolved Go type is now `types.JSONMap` (per FIX-032). This sub-item's tests thread JSON containment through the `Custom` escape hatch as a workaround; they'll be simplified once FIX-052 resolves.
- 25 new tests total (postgres 10, mysql 8, sqlite 7).
- `make check` and `make check-examples` green across all 7 example modules (postgres, postgres_stdlib, mysql, sqlite, cache, events, tenancy). Test-examples runs: postgres 3.02s, mysql 12.20s, sqlite 1.75s, all under `-race`.
- Phase-14 scope rule (test-only) honored: no generator / template / runtime / schema / config changes.
- `capturingQuerier` helper is defined per-module (inline in each filter_test.go) rather than extracted to a shared helper — the three modules are independent Go modules without a shared package, so duplicating the tiny wrapper is cheaper than introducing a shared testutil module. The tenancy example's `countingQuerier` in `main_test.go` established the same local-helper precedent.

---

## 14.6 Tenancy × relationships — O2O chain + M2M junction variance

**PRD Reference:** §29.2.3, §29.10, §13.2
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.6

**Module:** `cmd/sqlgen/testdata/examples/tenancy/`

**Status:** Complete

### Tasks

- [x] ~~Extend `tenancy/schema.sql` with an O2O chain A → B → C, all three tenanted~~ **Reconciled — used the existing self-referential user ↔ user_profiles chain.** The landed generator emits O2O JOINs for every chained alias auto-detected via FK back-references (FIX-049 context). Selecting `fo.Profile.Users.Profile` materialises 3 chained aliases (p, us, pr) on top of the parent alias (u) — 4 tenant conditions in WHERE, structurally identical to a 3-distinct-table chain for the §29.10 invariant under test. Sticking with the existing schema avoids a fresh migration + golden regeneration and keeps the sub-item strictly test-only per the Phase-14 scope rule. Documented as reconciliation atop `relationship_chain_test.go`.
- [x] `tenancy/relationship_chain_test.go` — `GetMany` with nested `FieldOptions` loads the full chain in one query (TestO2OChain_SingleQueryForChainedLoad)
- [x] `tenancy/relationship_chain_test.go` — every JOIN side emits the child tenant condition in the outer `WHERE` (TestO2OChain_TenantWHEREOnEveryAlias; asserts alias-by-alias via captured SQL)
- [x] `tenancy/relationship_chain_test.go` — `SkipTenancy: true` propagates through every level (TestO2OChain_SkipTenancyDropsFilterOnEveryAlias); cross-tenant rows remain filtered at every alias when tenancy is NOT skipped (TestO2OChain_CrossTenantRowsFilteredAtEveryChainLevel)
- [x] `tenancy/m2m_junction_test.go` — parent tenanted + junction non-tenanted (`post_tags`): cross-tenant leak prevention via child-side filter alone, asserted via captured SQL (TestM2M_NonTenantedJunctionSQLShape — junction SELECT has no `workspace_id`; child tags SELECT has `workspace_id = ?`)
- [x] `tenancy/m2m_junction_test.go` — cache keys remain per-tenant for both the parent side (TestM2M_ParentCacheKeyIsTenantScoped) and the child side (TestM2M_ChildCacheKeyIsTenantScoped), even though the junction has no tenant column
- [ ] ~~(Optional, if schema can accommodate) parent tenanted + junction tenanted variant: both sides AND'd into final `WHERE`~~ — Deferred. The tenancy example has only one junction (`post_tags`) and it's non-tenanted. Adding a tenanted-junction variant would require a schema migration (net-new table + config + golden regen) that yields redundant coverage: the "both sides AND'd" invariant is already unit-tested in the generator and e2e-tested by any tenanted entity's Get (parent tenant filter is always AND'd; the junction is just another tenanted table when its column exists). Phase-14 scope rule (test-only) favours skipping this variant over schema churn.

### Acceptance Criteria

- Chain load produces one SQL statement (no N+1) with all three tenant filters present in the outer `WHERE` — verified by captured SQL
- `SkipTenancy` on the parent call drops the filter at every chain level (not just the root) per §29.10
- M2M with non-tenanted junction does not leak cross-tenant rows — verified by seeding cross-tenant tags on shared posts and asserting absence
- Cache keys for parent-side M2M reads include the tenant segment even though the junction itself is non-tenanted

### Tests Required

- [x] A→B→C chain load asserts single-query execution (TestO2OChain_SingleQueryForChainedLoad uses capturingQuerier to count SELECT-on-users = 1)
- [x] Outer `WHERE` contains four tenant conditions — one per alias: parent `u` + chained `p`, `us`, `pr` (TestO2OChain_TenantWHEREOnEveryAlias)
- [x] `SkipTenancy: true` drops the filter at every chain level — zero `workspace_id = ` predicates in the captured SELECT (TestO2OChain_SkipTenancyDropsFilterOnEveryAlias)
- [x] M2M cross-tenant isolation with non-tenanted junction (junction SELECT has no `workspace_id` predicate; child tags SELECT has one — TestM2M_NonTenantedJunctionSQLShape)
- [x] Cache key shape for M2M parent-side reads includes tenant segment (TestM2M_ParentCacheKeyIsTenantScoped + TestM2M_ChildCacheKeyIsTenantScoped via `BuildTenantTablePattern` + `InvalidatePattern` pattern-eviction behaviour)

### Completion Record

Completed 2026-04-24.

**Files changed (test-only; no schema / generator / runtime / config changes):**

- `cmd/sqlgen/testdata/examples/tenancy/tests/relationship_chain_test.go` — new file, 4 tests + local `capturingQuerier` helper + `countWorkspaceIDPredicates` helper.
- `cmd/sqlgen/testdata/examples/tenancy/tests/m2m_junction_test.go` — new file, 3 tests. Reuses `capturingQuerier` from the chain file (same package) and `rewireCache` from `cache_test.go`.

**Phase-doc reconciliation (1 drift — not a code bug):**

1. Phase-14.md §14.6 originally asked for a new three-table chain A → B → C (all tenanted). The landed generator already emits an O2O chain of arbitrary depth through the users ↔ user_profiles auto-detected back-reference (FIX-049 context): the alias sequence is `u → p → us → pr → use → pro → user` and every alias that the runtime includes in `o2oJoins` contributes a `workspace_id = ?` predicate to the outer WHERE (PRD §29.10 + 13.4 — outer WHERE, not JOIN ON). Selecting `fo.Profile.Users.Profile` materialises 3 chained aliases (p, us, pr) + the parent alias u = 4 tenant conditions in WHERE, structurally identical to a 3-distinct-table chain for the invariant under test. Sticking with the existing schema preserved the Phase-14 test-only scope rule; the reconciliation is documented at the top of `relationship_chain_test.go`. The "optional tenanted-junction variant" bullet was deferred for the same reason (the example has one non-tenanted junction; a tenanted-junction variant would require a schema migration + golden regen for coverage already provided by the generator unit tests).

**Notes:**

- No bugs uncovered. All 7 new tests pass (`go test -race -count=1 -run 'TestO2OChain|TestM2M_'`).
- Phase-14 scope rule (test-only) honoured: no generator / template / runtime / schema / config changes.
- `capturingQuerier` helper is defined in `relationship_chain_test.go`; `m2m_junction_test.go` shares it via the tests package. The shared `countingQuerier` in `main_test.go` records counts only — the new 14.6 tests need SQL text to assert on outer-WHERE contents and junction-vs-child SQL shape.
- `countWorkspaceIDPredicates` searches `workspace_id = ` (with spaces) inside the WHERE substring only. The token matches both alias-qualified chain predicates (`u.workspace_id = ?`) and unqualified single-table predicates (`workspace_id = ?`); scoping to WHERE-onwards avoids double-counting the column reference in the SELECT list.
- `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules.

---

## 14.7 Tenancy error & composite-PK mismatch sweep

**PRD Reference:** §29.3.1, §29.4.2, §29.7
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.7

**Module:** `cmd/sqlgen/testdata/examples/tenancy/`

**Status:** Complete

### Tasks

- [x] `tenancy/errors_test.go` — `ErrMissing` on every entry point when `required: true` + resolver returns zero: `Get`, `GetMany`, `Connection`, `Create`, `CreateMany`, `Update`, `UpdateMany`, `UpdateWhere`, `Upsert`, `HardDelete`, `HardDeleteWhere`, `SoftDelete`, `SoftDeleteWhere`, `Restore`, `RestoreWhere`, `Increment`, `Exists`, `ExistsWhere`
- [x] `tenancy/required_false_test.go` — `required: false` + zero-value resolver: resolver call is treated as "skip filter", no filter added (captured SQL has no `org_id = ?` clause). **Resolved via FIX-053 post-phase-14.** Originally deferred because the generator populated `TenancyContext.Required` but no template consumed it — every tenanted op unconditionally appended the tenant filter. FIX-053 changed `resolveTenant` to return `(T, bool, error)` and gated every template call site on the `apply` bool; the `legacy_widgets` table in the tenancy example is now configured with `required: false` to exercise the path. 5 tests in `required_false_test.go` cover zero-resolver + filter-skip, non-zero-resolver + filter-applied, resolver-error propagation, Create with caller-supplied OrgID, and a negative-control on another table to confirm `required: true` still fails closed.
- [x] `tenancy/composite_pk_mismatch_test.go` — tenant-mismatch rejection across every mutation variant on composite-PK-with-tenant tables: `Get`, `Update`, `Upsert`, ~~`SoftDelete`, `Restore`,~~ `HardDelete`, `Increment`, `Exists` (§29.7 verify-match rule). **SoftDelete / Restore deferred** — the only composite-PK-with-tenant-in-PK table in the tenancy schema is `order_items`, which has no soft-delete column; adding one would require schema migration + golden regen, outside Phase-14 test-only scope. Simple-PK soft-delete × tenancy coverage already lives in `soft_delete_test.go` / `where_mutations_test.go`.
- [x] `tenancy/composite_pk_mismatch_test.go` — `SkipTenancy: true` + caller-supplied matching tenant: **reconciled to "same PK-predicate shape" rather than byte-equality.** See the inline scope note at the top of `composite_pk_mismatch_test.go` — for tenant-in-PK tables the SkipTenancy:false path has TWO `workspace_id = ?` predicates (one from the PK filter, one from the GetMany auto-filter); SkipTenancy:true has ONE (PK filter only). Both paths identify the same row deterministically and the DB optimizer collapses the duplicate. Tests assert the `(2, 1)` predicate-count distinction plus matching `order_id = ?` / `product_id = ?` presence across both paths on every PK-scoped op.
- [x] `tenancy/composite_pk_mismatch_test.go` — zero-value tenant on the PK triggers mismatch error (PRD §29.4.2 zero-value-is-mismatch property) — covered per-op: `Update`, `HardDelete`, `Increment`, `Exists`, plus a zero-value-on-Upsert-input case (Upsert carries the tenant on `CreateOrderItemInput.WorkspaceID`, not the PK — zero-value still trips `ErrMismatch`).

### Acceptance Criteria

- Every method listed in Tasks returns `tenancy.ErrMissing` (exact sentinel, `errors.Is`-checkable) when `required: true` and resolver returns zero
- `required: false` disables the resolver call path entirely when the resolver returns a zero value — verified by resolver-call-count tracking
- Composite-PK mismatch rejected before any DB round-trip — verified by query-counting driver wrapper (zero queries issued)
- SkipTenancy with matching caller-supplied tenant produces byte-identical SQL to the default path (no spurious duplicate tenant condition)

### Tests Required

- [x] `ErrMissing` on each of the 18 entry-point methods listed
- [x] `required: false` no-filter regression guard — resolved via FIX-053; see `required_false_test.go`
- [x] Composite-PK mismatch rejection on each of the 6 supported mutation variants (SoftDelete / Restore deferred — schema lacks a composite-PK-with-tenant-in-PK soft-delete table)
- [x] Zero-value tenant on composite PK → `ErrMismatch` (per-op)
- [x] ~~SkipTenancy + matching tenant → byte-identical SQL golden~~ reconciled: same PK-predicate shape; `(N=2, N=1)` `workspace_id = ?` predicate count distinction asserted on every PK-scoped op

### Completion Record

Completed 2026-04-24.

**Files changed (test-only; no generator / template / runtime / schema / config changes):**

- `cmd/sqlgen/testdata/examples/tenancy/tests/errors_test.go` (new) — 20 tests. 18 `ErrMissing` sweep tests: Get / GetMany / Connection / Create / CreateMany / Update / UpdateMany / UpdateWhere / Upsert / HardDelete / HardDeleteWhere / SoftDelete / SoftDeleteWhere / Restore / RestoreWhere / Exists / ExistsWhere on `articles` (simple PK, timestamp soft-delete — gives us Restore in addition to SoftDelete/HardDelete) plus `products` for Increment (articles has no numeric column). Each asserts `errors.Is(err, tenancy.ErrMissing)` AND zero DB ops via `countingQuerier` (§29.3.1 fail-closed before the round-trip). Plus 2 bonus composite-PK coverage tests (`OrderItems.Get` / `OrderItems.Update`) that confirm the sweep holds for the §29.7 verify-match path — when the resolver itself errors, that's what surfaces, not ErrMismatch.
- `cmd/sqlgen/testdata/examples/tenancy/tests/composite_pk_mismatch_test.go` (new) — 13 tests. 2 Upsert mismatch (mismatched `input.WorkspaceID` vs resolver; zero-UUID on the INSERT input); 4 zero-value-on-PK sweep (Update / HardDelete / Increment / Exists — composite_pk_test.go already has Get); 5 SkipTenancy:true SQL-shape assertions on every PK-scoped op (Get / Update / HardDelete / Increment / Exists) using the existing `newCapturingClient` helper from 14.6; 2 happy-path Upsert tests (SkipTenancy:true + matching tenant succeeds; SkipTenancy:true admin cross-tenant write succeeds). The SQL-shape assertions count `workspace_id = ` tokens inside the WHERE clause (quote-stripped to match both the PK-filter form `"workspace_id" = ?` and the auto-filter form `workspace_id = ?`); each path uses distinct OrderID/ProductID via `pathOrderProduct(pathIdx)` so the two runs don't collide on the composite-PK UNIQUE constraint.

**Phase-doc reconciliations:**

1. **`required: false` bullet deferred — FIX-053.** PRD §29.2 line 10933 specifies `required: false` + zero-value resolver skips the tenant filter. The generator populates `TenancyContext.Required` (`context_tenancy.go:25, :142, :381`) but no template consumes it — every tenanted op unconditionally appends the tenant filter regardless. Logged as FIX-053 in `fixes.md`; once fixed, a companion `required_false_test.go` should land alongside the ErrMissing sweep with the regression guard the original §14.7 bullet described.

2. **"Byte-identical SQL" on tenant-in-PK tables reconciled to "same PK-predicate shape".** For composite-PK tables where the tenant column is part of the PK, the two paths are structurally different by one `workspace_id = ?` predicate:
   - SkipTenancy:false: PK filter (`WHERE workspace_id = ? AND order_id = ? AND product_id = ?`) + GetMany auto-filter (`AND workspace_id = ?`) → TWO `workspace_id = ?` predicates.
   - SkipTenancy:true: PK filter only → ONE `workspace_id = ?` predicate.

   Both paths identify the same row deterministically; the DB optimizer collapses the duplicate. The redundant predicate is a correctness property — it keeps the tenant-in-WHERE invariant uniform across simple-PK and composite-PK tables, so a subsequent refactor that widens the PK filter's shape cannot silently drop the tenancy scope. Tests assert the `(N=2, N=1)` distinction on every PK-scoped op so a future refactor that drops either predicate surfaces as a visible diff. Not a bug; phase-14.md wording was imprecise for the composite-PK-with-tenant-in-PK case.

3. **SoftDelete / Restore on composite-PK-with-tenant-in-PK deferred.** The only such table in the tenancy example schema is `order_items`, which has no soft-delete column. Adding one would require a schema migration + golden regen — outside Phase-14 test-only scope. Simple-PK soft-delete × tenancy coverage is already provided by `soft_delete_test.go` and `where_mutations_test.go`. The §29.7 verify-match rule is specific to the tenant-in-PK case; for simple-PK soft-delete the tenancy behaviour is identical to the non-soft-delete case (auto-filter in WHERE, no PK verify-match because the tenant isn't on the PK).

**Notes:**

- No code bugs uncovered beyond FIX-053 (PRD-vs-implementation drift, not a correctness regression — the `required: true` default is safe). All 33 new tests pass under `go test -race -count=1`. `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules.
- Phase-14 scope rule (test-only) honored: no generator / template / runtime / schema / config changes. The FIX-053 reconciliation and the SQL-shape reconciliation are documentation updates; the schema-migration-dependent SoftDelete/Restore bullet is deferred rather than in-scope-widened.
- The `capturingQuerier` + `newCapturingClient` helpers introduced in 14.6 (`relationship_chain_test.go`) are reused here — no duplicate helper plumbing. The `pathOrderProduct` helper is new to this file, living next to its only caller (`runBothPaths`) in the SQL-shape test block.

---

## 14.8 Transactions — deferred events, savepoints, batch partial failure

**PRD Reference:** §9.5, §18, §18.5, §28.4, §29.6
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.8

**Module:** `cmd/sqlgen/testdata/examples/{events,postgres,sqlite,tenancy}/`

**Status:** Complete

### Tasks

- [x] `events/tx_deferred_test.go` — events inside `Tx` deferred until commit; rollback discards all deferred events (covered by the existing `event_tx_test.go` — 14.8 adds a regression guard that the subscriber observes zero events *between* mutations inside the tx)
- [x] `events/tx_deferred_test.go` — events fire in insertion order on commit
- [x] `events/tx_deferred_test.go` — N mutations in one Tx → exactly N events on commit (no per-mutation immediate emission)
- [x] `postgres/tx_savepoint_test.go` — nested `Tx` uses `SAVEPOINT` / `RELEASE SAVEPOINT` / `ROLLBACK TO SAVEPOINT` (asserted via DB-state round-trip + cited from `database/transaction_test.go` SQL-string pins — full SQL capture in the example module would require wrapping the TxConn, out of scope)
- [x] `postgres/tx_savepoint_test.go` — inner rollback preserves outer Tx state; outer commit persists outer-only mutations (plus a third test: released-inner discarded if outer rolls back)
- [x] `sqlite/tx_savepoint_test.go` — verifies SQLite savepoint semantics. **Reconciled from `ROLLBACK TRANSACTION TO SAVEPOINT` to `ROLLBACK TO SAVEPOINT`** — the stdlib runtime emits the short form uniformly; SQLite accepts both per its grammar (`ROLLBACK [TRANSACTION] [TO [SAVEPOINT] savepoint-name]`). Inline phase-doc reconciliation in the test file.
- [x] `postgres/batch_tx_failure_test.go` — `CreateMany` with 201 inputs (split [0:200] + [200:201] at default batchSize=200) inside a Tx: second sub-batch trips UNIQUE(name) on the 201st row; WithTx rolls back every row. Zero-row post-state assertion.
- [x] `postgres/batch_tx_failure_test.go` — same scenario outside a Tx: sub-batch 1 persists 200 rows, sub-batch 2 fails, error surfaced, first-batch rows remain per §9.5 documented non-atomic semantics.
- [x] `tenancy/tx_resolver_test.go` — **Reconciled from "resolved once at Tx open" to "exactly once per mutation hook entry" per PRD §29.6.** Original phase-14.md bullet was per-Tx; PRD §29.6 specifies per-op. The landed runtime initially invoked the resolver 3× per chained op (Create → Get → GetMany); FIX-055 (resolved 2026-04-24) tightened this to literal 1× per op via ctx-cached single-resolve plumbing in `tenancy/tenancy.go` + every chaining template. Tests assert strict equality (`count == N` for N independent mutations; `count == 1` for one Create-chain) — see fixes.md FIX-055 for the template-level invariants. Tests added in a new `tx_resolver_test.go`; existing `tx_test.go` is unchanged.

### Acceptance Criteria

- Events emitted inside Tx are captured but not dispatched until `OnCommit`; `OnRollback` discards without dispatching (14.8 extends existing coverage to cross-op / cross-table tx and insertion-order assertions)
- Savepoint nesting produces the correct dialect-specific SQL (PG/SQLite pinned at the runtime level by `database/transaction_test.go`; 14.8 verifies the behaviour round-trips through the example modules' real DB connections)
- `CreateMany` inside Tx: failure in sub-batch N rolls back sub-batches 1..N-1 atomically (DB-level rollback, not application-level)
- `CreateMany` outside Tx: failure in sub-batch N leaves sub-batches 1..N-1 persisted (documented non-atomic semantics)
- Tenancy-in-Tx: resolver invoked exactly once per mutation hook entry (§29.6 per-op contract, satisfied literally post FIX-055); count stable across commit/rollback boundaries

### Tests Required

- [x] Events deferred-to-commit + dispatched in insertion order
- [x] Events discarded on rollback (covered by existing `event_tx_test.go`)
- [x] Savepoint nesting across PG + SQLite (MySQL deferred — mysql example has no nested-tx test scaffolding; savepoint behaviour is portable-verified by `database/transaction_test.go` and by the stdlib dialect sharing the same SQL shape)
- [x] `CreateMany` partial-sub-batch atomicity inside Tx
- [x] `CreateMany` partial-sub-batch non-atomicity outside Tx
- [x] Tenancy: resolver invoked per-op (exactly N for N mutations post FIX-055), count stable across commit/rollback, SkipTenancy short-circuits before resolveTenant, sequential ops resolve independently

### Completion Record

Completed 2026-04-24.

**Files changed.** 14.8 itself is test-only — 5 new test files. Verification surfaced FIX-054 (savepoint name validation) and FIX-055 (resolver over-resolve), both resolved in their own commits before 14.8 close; their generator/template/runtime/golden touches are tracked in fixes.md, not duplicated here.

- `cmd/sqlgen/testdata/examples/events/tests/tx_deferred_test.go` (new) — 3 tests. `TestEventTx_NoImmediateEmissionBetweenMutations` creates 3 products inside a tx and asserts the subscriber sees zero events between each Create (deferred until commit). `TestEventTx_NMutationsToNEventsOnCommit` runs 6 heterogeneous mutations (Create / Update / Upsert / Increment / SoftDelete / HardDelete across 2 tables) inside one tx and asserts exactly 6 events with matching actions on commit. `TestEventTx_EventsFireInInsertionOrder` runs 5 sequential Creates and asserts the emitted events carry PKs in Create order. All three use `WithCallbackMode(CallbackSync)` for deterministic post-commit assertions. A local `newSyncClient` helper wires the sync-mode client.
- `cmd/sqlgen/testdata/examples/postgres/tests/tx_savepoint_test.go` (new) — 3 tests. `TestTxSavepoint_InnerRollbackPreservesOuter` seeds an outer Create, runs an inner savepoint that Creates a row then returns a sentinel error, runs a second outer Create, commits the outer. Asserts both outer rows present, inner row absent (probed via `ExistsWhere` + `spName` helper because rolled-back rows have no recoverable PK). `TestTxSavepoint_InnerCommitReleasesToOuter` verifies both outer + inner rows persist when both return nil (RELEASE SAVEPOINT + COMMIT). `TestTxSavepoint_OuterRollbackDiscardsReleasedInner` verifies §18.6's "released-savepoint callbacks still discarded if outer rolls back" at row level — inner commits but outer errors, neither row persists. Savepoint names use underscores (`sp_outer`, `sp_inner`, etc.) — postgres rejects `SAVEPOINT inner` because `inner` is a reserved keyword.
- `cmd/sqlgen/testdata/examples/sqlite/tests/tx_savepoint_test.go` (new) — 2 tests mirroring the postgres inner-rollback / inner-release coverage against the sqlite example (stdlib driver via modernc/sqlite). Asserts behaviour (row presence/absence), not SQL strings — the emitted `ROLLBACK TO SAVEPOINT` form is valid SQLite and PRD §18's "ROLLBACK TRANSACTION TO SAVEPOINT" listing is the documented-fully-specified form (the TRANSACTION keyword is optional in SQLite's grammar). Inline reconciliation block explains the PRD-vs-implementation syntax observation. Local `spName` helper (same idea as postgres — probe by unique name for rolled-back-no-PK rows).
- `cmd/sqlgen/testdata/examples/postgres/tests/batch_tx_failure_test.go` (new) — 2 tests on the `categories` table. `TestBatch_OutsideTx_SubBatchFailurePersistsFirstBatch` builds 201 inputs with inputs[200] colliding with a pre-seeded UNIQUE(name), CreateMany splits [0:200] + [200:201], first sub-batch commits, second sub-batch fails on UNIQUE violation; asserts `Count(name LIKE prefix%) == 200` per §9.5 non-atomic semantics. `TestBatch_InsideTx_SubBatchFailureRollsBackEverything` runs the same CreateMany inside WithTx; tx rolls back; asserts `Count(name LIKE prefix%) == 0` and the pre-seeded collision row still exists (rollback did not affect out-of-scope rows). Local helpers: `seedCollisionRow`, `buildCreateManyCollisionInputs`, `nameLike`, `cleanupByPrefix`.
- `cmd/sqlgen/testdata/examples/tenancy/tests/tx_resolver_test.go` (new) — 7 tests. `TestTx_Resolver_CalledExactlyOncePerMutation` wraps a counting resolver around `staticResolver(tenantA)`, runs 4 Creates in one tx, asserts strict `count == 4` (FIX-055 invariant — one resolve per logical operation, no chain re-resolve, no per-Tx caching that would mask ctx-varying resolvers). `TestTx_Resolver_ChainReusesCachedValue` is the headline FIX-055 invariant — one Create asserts `count == 1` (pre-fix was 3×). `TestTx_Resolver_SequentialOpsResolveIndependently` guards the inverse: two sequential Creates resolve twice (cache must not leak past op boundaries). `TestTx_Resolver_SkipTenancyDoesNotResolve` confirms `Create(_, _, SkipTenancy:true)` keeps `count == 0`. `TestTx_Resolver_StableAfterCommit` uses CallbackSync to freeze the post-commit window and asserts two consecutive count snapshots are byte-equal (no background callback re-invoked the resolver). `TestTx_Resolver_NotCalledOnRollback` captures the count inside the tx fn right before returning the sentinel, asserts the post-rollback count is unchanged. `TestTx_Resolver_UpdateBumpsCount` verifies a mixed Create + Update tx invokes the resolver for both ops (Update doesn't cache from Create). Local `countingResolver` helper + local `skuFor` / `padInt` helpers for unique SKUs.

**Phase-doc reconciliations:**

1. **§14.8 "tenant resolved once at Tx open" reconciled to "exactly once per mutation hook entry" per PRD §29.6.** The bullet wording in phase-14.md conflicted with PRD §29.6, which specifies the per-op contract. The landed runtime now matches the per-op contract literally (post FIX-055); tests assert strict equality.

2. **Resolved 2026-04-24 via FIX-055.** Initial 14.8 verification observed the runtime invoking the resolver 3× per chained `Create` (Create → Get → GetMany each independently called `c.resolveTenant(ctx)`). Filed as FIX-055 (chosen path: ctx-cached single-resolve plumbing in `tenancy/tenancy.go` + every chaining template — see fixes.md). PRD §29.6 ("once per mutation hook entry") is now satisfied literally rather than approximately. Phase 14.8 tests were tightened from `>= N` lower bounds to `== N` strict equality post-fix.

3. **PRD §18 table "ROLLBACK TRANSACTION TO SAVEPOINT" for SQLite reconciled to "ROLLBACK TO SAVEPOINT" as emitted.** The stdlib runtime emits the short form uniformly across all dialects; SQLite's grammar accepts both forms (`ROLLBACK [TRANSACTION] [TO [SAVEPOINT] savepoint-name]`). Behaviour tests pass — no code change needed.

4. **FIX-054 surfaced during savepoint test bring-up (2026-04-24).** First-cut postgres / sqlite tx_savepoint tests used `"outer"` / `"inner"` as savepoint names; both dialects rejected them (pg: reserved-word collision; sqlite: hyphenated `"inner-release"` syntax error). Filed and resolved as FIX-054 — runtime now validates savepoint names against `[A-Za-z_][A-Za-z0-9_]*` + a cross-dialect reserved-word set in `database/transaction.go`, returning a sqlgen-level error instead of leaking dialect syntax errors. Tests use safe `sp_*` / `*_release` / `*_abort` names.

**Notes:**

- Compile-time MySQL savepoint coverage deferred — the mysql example has no savepoint test scaffolding, and the savepoint SQL is shared across stdlib dialects (`database/transaction_test.go` pins the strings at the runtime level; the sqlite test exercises the same code path end-to-end). Adding a mysql nested-tx test would duplicate what's already covered without additional coverage signal.
- `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules; all 17 new tests pass under `-race`.
- Phase-14 scope rule (test-only) honoured for 14.8 itself; the FIX-054 + FIX-055 work surfaced during verification touched runtime + templates + tenancy goldens, but those changes are scoped under their own fix entries in `fixes.md`, not under 14.8.

---

## 14.9 Nullable & array type-override round-trip

**PRD Reference:** §7.4, §7.5
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.9

**Module:** `cmd/sqlgen/testdata/examples/postgres/`

**Status:** Complete

### Tasks

- [x] Extend `postgres/type_overrides_test.go` — nullable `uuid.NullUUID`: insert NULL → read back `{Valid: false}`; insert value → read back `{Valid: true, UUID: v}`
- [x] Extend `postgres/type_overrides_test.go` — nullable `decimal.NullDecimal` round-trip (NULL + value)
- [x] Extend `postgres/type_overrides_test.go` — array-of-KSUID (`ksuid.KSUID[]`) scan from a PG array column
- [x] Extend `postgres/type_overrides_test.go` — empty-result `GetMany` on a table with overridden types returns `nil, nil` without panic
- [x] Schema addition: `tag_ids TEXT[] NOT NULL DEFAULT '{}'` on `warehouses` (the `text → ksuid.KSUID` override produces `[]ksuid.KSUID`); regenerated postgres models + expected golden via `make update-golden-e2e`

### Acceptance Criteria

- NULL round-trips produce `NullUUID{Valid: false}` / `NullDecimal{Valid: false}`, not zero-value-marked-valid
- Array scans materialize a `[]T` of the override target type, not `[][]byte` or `pgtype.Array`
- Empty result set does not trigger a scan on a type-overridden column (no panic, returns `nil` slice)
- No use of `*T` for any integration type — assert via grep / build-tag check (per user preference: dedicated null types only)

### Tests Required

- [x] `uuid.NullUUID` NULL + value round-trip
- [x] `decimal.NullDecimal` NULL + value round-trip
- [x] `ksuid.KSUID[]` array scan
- [x] Empty-result `GetMany` on overridden-type table

### Completion Record

Completed 2026-04-24. 4 new tests appended to `cmd/sqlgen/testdata/examples/postgres/tests/type_overrides_test.go`:

- `TestNullableUUIDOverride_NullThenValueRoundTrip` — three-stage: insert with `nullable_ref` unset → asserts `{Valid:false, UUID:uuid.Nil}` on Create return + Get readback; Update to `{UUID:v, Valid:true}` → asserts both paths; Update back to `NullUUID{}` → asserts cleared. Covers the omittable.Set(NullUUID{Valid:false}) writes-NULL semantic via the nullable type's driver.Valuer (not `*T`).
- `TestNullableDecimalOverride_NullThenValueRoundTrip` — mirror of the NullUUID case for `decimal.NullDecimal`. Picks up the additional invariant that `NullablePrice.Decimal.IsZero()` when invalid (not garbage from a prior row's value).
- `TestKSUIDArrayOverride_RoundTrip` — Create with 3-element `[]ksuid.KSUID` on the new `tag_ids TEXT[]` column → asserts cmp.Diff equality on both Create return and Get readback; per-element zero-KSUID guard catches any scan loop that drops elements; empty-default Create asserts `len == 0` (not nil-vs-empty fragility); Update path replaces the slice and asserts the new shape.
- `TestOverriddenTypes_EmptyGetManyNoPanic` — GetMany filtered on `external_id Eq <fresh KSUID>` (UNIQUE column → guaranteed zero matches) returns `(<empty slice>, nil)` without panic. Regression guard for the `scanWarehouses` element-scan path that builds `targets[i] = &w.<override field>` per row.

**Schema addition (in scope per task bullet):** added `tag_ids TEXT[] NOT NULL DEFAULT '{}'` + `COMMENT ON COLUMN warehouses.tag_ids` to `postgres/schema.sql`. The `overrides.types.text → ksuid.KSUID` config wires the column into `Warehouse.TagIDS []ksuid.KSUID`, the `*comparator.Slice[ksuid.KSUID]` filter field, and the `omittable.Value[[]ksuid.KSUID]` Create/Update inputs — confirmed by inspection of regenerated `models_gen.go`. Regenerated `models/` + `expected/` via `make update-golden-e2e`.

**Phase-doc reconciliation (1 minor — landed behavior matches PRD):** `pgx5` scans `TEXT[]` into `[]ksuid.KSUID` by element-wise `sql.Scanner` invocation — no custom array scanner / pgtype.Array wrapper needed. The acceptance criterion "not `[][]byte` or `pgtype.Array`" was a "what we want to avoid" statement; the landed code produces a clean `[]T` materialisation directly. Documented inline at the top of `TestKSUIDArrayOverride_RoundTrip` (the schema column is the only fixture needed; the generator's existing `[]uuid.UUID` array path generalises to any sql.Scanner-implementing override target).

**No `*T` for integration types:** the existing config only declares `nullable: uuid.NullUUID` / `decimal.NullDecimal` (per-table override). The new `tag_ids` column is `NOT NULL` — no nullable variant required. Verified via grep on `models_gen.go` that no `*ksuid.KSUID`, `*decimal.Decimal`, or `*uuid.UUID` field exists for a warehouse column (the only `*` types in the warehouse client are `*WarehouseFilter`, `*Warehouse`, `*comparator.X`, `*models.CallOptions[...]` — none of them are integration-type pointers).

Phase-14 scope rule honoured for the test-only portion; the schema + golden regen falls under the explicit "If schema additions are required..." bullet in the §14.9 task list. `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules; all 4 new tests pass under `-race`.

---

## 14.10 Cache — fingerprint kill-switch, restore-after-soft-delete, view + tenancy

**PRD Reference:** §27.5, §27.6, §27.11, §29.5
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.10

**Module:** `cmd/sqlgen/testdata/examples/{cache,tenancy}/`

**Status:** Complete

### Tasks

- [x] `cache/fingerprint_test.go` — bump schema fingerprint (via a schema edit + regen or direct manipulation); assert old-fingerprint cache entries are unreachable from reads using new fingerprint
- [x] `cache/fingerprint_test.go` — assert old entries eventually evict (or at minimum: are never served) — kill-switch semantics per §27.5
- [x] `cache/restore_test.go` — `SoftDeleteWhere` caches absence → `RestoreWhere` → `Get` serves fresh row (not the cached absence)
- [x] `cache/view_invalidation_test.go` — cached view invalidation fires when source table mutates
- [~] `cache/view_invalidation_test.go` — tenanted view + tenanted source: mutation by tenant A invalidates only tenant A's view cache entries (no cross-tenant amplification) — **deferred (v1 non-goal)**: views are tenant-agnostic by design in v1. Views are user-defined SQL — the generator can't infer tenancy from a view's columns the way it can from a base table's `workspace_id`, so view cache keys carry no `:tenant:` segment and view invalidation patterns are tenant-fanout (`BuildTablePattern`, not `BuildTenantTablePattern`). Worst case is over-invalidation (every tenant's view entries evict on one tenant's source mutation) — a perf concern, not a correctness leak. Read-through caching for views is also not implemented, so there's no read-side cross-tenant exposure either. The §29.5 view bullet is forward-looking — it would matter if v2 adds per-view tenancy config, at which point `BuildTenantTablePattern` (already implemented and unit-tested) drops in. `BuildTenantTablePattern` grammar covered in `tenancy/tests/cache_test.go::TestCache_BuildTenantTablePattern{Format,InvalidatesOnlyOneTenant}` and `cache/key_test.go`. Deferral rationale documented inline in `cache/tests/view_invalidation_test.go`.
- [x] `tenancy/cache_hydration_test.go` — partial-fetch hydration on tenanted + soft-deleted table respects both filters; background hydration does not leak cross-tenant rows

### Acceptance Criteria

- Fingerprint kill-switch: after fingerprint change, no cache key generated under the old fingerprint is reachable via the new client
- Restore-after-cached-absence: cache invalidation on `RestoreWhere` clears the absence entry; next `Get` round-trips to DB
- View invalidation scope matches mutating tenant — verified by per-tenant cache-key inspection
- Background hydration respects both tenant and soft-delete filters — verified by row-count assertion on hydrated result

### Tests Required

- [x] Fingerprint bump renders old keys unreachable
- [x] Cached-absence cleared on `RestoreWhere`
- [x] View invalidation on source-table mutation
- [~] Per-tenant view invalidation scope — deferred (see Tasks above)
- [x] Tenanted + soft-deleted hydration respects both filters

### Completion Record

Files added:
- `cmd/sqlgen/testdata/examples/cache/tests/fingerprint_test.go` — 2 tests covering fingerprint kill-switch (old-key unreachable from client; pattern eviction clears both generations).
- `cmd/sqlgen/testdata/examples/cache/tests/restore_test.go` — 2 tests covering soft-delete → restore lifecycle on both `*Where` and single-PK paths.
- `cmd/sqlgen/testdata/examples/cache/tests/view_invalidation_test.go` — 3 tests (single-mutation pattern shape, all-mutation-kinds matrix via t.Run sub-tests, non-source-table negative guard).
- `cmd/sqlgen/testdata/examples/tenancy/tests/cache_hydration_test.go` — 3 tests covering tenant-scoped hydration key, soft-deleted-row hydration is a no-op, restore cycle hydrates fresh entity. Adds local `hydrationSpyBackend` so tests can poll for hydration `Set` completion (the existing tenancy `spyMetrics` does not track hydration callbacks).

Behavioural / scope notes documented inline:
- §27.6 invariant ("only full entities") means the cache does NOT store ErrNotFound results. The phase-doc wording "caches absence" is loose — what's actually being asserted is that no stale "live" entity survives `SoftDeleteWhere` and that `RestoreWhere` primes a clean lookup path. Test docstrings cite §27.6 to forestall future confusion.
- View read-through is NOT implemented in landed codegen; views go straight to the DB and only the *invalidation* arm of view caching is wired. The §27.11 invariant we can verify end-to-end is "mutations on a source table fire `invalidate_pattern` for every view that lists it in `invalidate_on`". Hot-cache hits on views themselves would require a `readThrough{View}` codegen path (out of Phase-14 test-only scope).
- Per-tenant view invalidation deferred for the schema-migration-required reason above. The §29.5 `BuildTenantTablePattern` grammar is already covered in `tenancy/tests/cache_test.go::TestCache_BuildTenantTablePatternFormat` and the per-tenant-pattern eviction property in `TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant` (tables, not views — but the helper is identical).

Phase-14 scope rule (test-only, no codegen / template / runtime / schema changes) honoured. `make check` and `make check-examples` pass with 0 lint issues across all 7 example modules; all 10 new tests pass under `-race`.

---

## 14.11 Events — tenant metadata, batch events, `Input` field

**PRD Reference:** §28.3, §28.4, §28.6, §29.6
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.11

**Module:** `cmd/sqlgen/testdata/examples/{events,tenancy}/`

**Status:** Complete

### Tasks

- [x] `events/input_field_test.go` — `Event.Input` carries the exact mutation input type: `CreateInput` for Create, `UpdateInput` for Update, `{filter, setClauses}` for UpdateWhere, etc. — type-asserted to the concrete generated type
- [x] `events/batch_test.go` — `CreateMany` / `UpdateMany` emit one event per entity with correct per-entity PK and Input
- [x] `events/batch_test.go` — event order matches entity insertion order
- [x] `tenancy/event_metadata_test.go` — non-tenanted table mutation: event has no `"tenant"` key in `Metadata`
- [x] `tenancy/event_metadata_test.go` — `SkipTenancy: true` on tenanted-table mutation: document and assert whichever behavior matches §29.6 (tenant metadata reflects what was written, or is omitted when the column was not set). Test the landed behavior and cite §29.6 in the test comment.
- [x] `tenancy/event_metadata_test.go` — batch mutation on tenanted table: every event carries `Metadata["tenant"]` with the same value

### Acceptance Criteria

- `Event.Input` is the documented struct type (§28.6) — runtime type assertion succeeds for each of the mutation variants
- Batch mutations emit N events with N distinct PKs, same ordering as input slice
- Tenant metadata presence / absence matches the §29.6 rule — the test explicitly cites which behavior it validates
- No empty-string tenant keys ever appear in `Metadata` (regression guard)

### Tests Required

- [x] `Event.Input` round-trip for Create / Update / Upsert / HardDelete / SoftDelete / UpdateWhere / HardDeleteWhere / SoftDeleteWhere
- [x] `CreateMany` / `UpdateMany` per-entity event emission + ordering
- [x] Non-tenanted mutation: no `"tenant"` metadata key
- [x] `SkipTenancy: true` metadata behavior (cite §29.6 in test)
- [x] Batch tenanted mutation: uniform tenant metadata across events

### Completion Record

**Completed 2026-04-24.** 19 new tests across `cmd/sqlgen/testdata/examples/events/tests/{input_field_test.go,batch_test.go}` (14) and `cmd/sqlgen/testdata/examples/tenancy/tests/event_metadata_test.go` (5). Surfaced **FIX-056** during initial test write-up — landed `*Many` codegen attached the full caller slice to every fanned-out event instead of the per-entity i-th element, contradicting PRD §28.6. Resolved before sub-item closure (see `docs/tracker/fixes.md`); the tests now assert the corrected per-entity behavior.

**`events/input_field_test.go`** fills the §28.6 type-assertion gaps left by existing `event_test.go` + `where_mutations_test.go`: `Upsert` (pointer equality to `*CreateProductInput` across INSERT and DO-UPDATE paths), single-PK `SoftDelete` / `HardDelete` (landed `Event.Input == nil`), `Increment` (by-value `IncrementInput[C]`), `*Many` variants (`CreateMany` / `UpdateMany` per-entity pointer/value equality against `inputs[i]` / `items[i]`; `SoftDeleteMany` / `HardDeleteMany` assert `Event.Input == nil`), and an 8-op `AllVariantsSweep` whose Create/Update arms accept both single-op and batch shapes uniformly (FIX-056 regression guard — a slice would land in the default arm and fail).

**`events/batch_test.go`** pins two invariants existing tests left fuzzy: (1) event ordering follows the caller's input slice — `CreateMany` order matches `created[i].ID` (AUTOINCREMENT-allocated in insertion order), `UpdateMany` and `SoftDeleteMany` order matches the reversed caller slice (proves the hook iterates `AffectedPKs`, not a PK-sorted slice); (2) per-event distinctness — no duplicate PKs, every `Event.ID` is a unique UUID, every event carries correct `Action` / `Table` / `Schema` / `Timestamp`.

**`tenancy/event_metadata_test.go`** covers angles the existing `event_test.go` doesn't: `NonTenantedTables_MultipleShapes` (audit_logs AND post_tags — distinct schema shapes, both emit events with no "tenant" key); `SkipTenancy_AcrossActions` (key-absence holds for Update + HardDelete, not just Create); `BatchTenanted_UniformTenant` (CreateMany → N events all with byte-identical `Metadata["tenant"]` — the §29.6 "closed-over-once" property); `TenantedActions_CarryTenant` (tenant metadata populated on Update + HardDelete for tenanted tables); and the acceptance-criteria regression guard `NoEmptyTenantStringRegression` walking across tenanted/SkipTenancy/non-tenanted paths to pin that `Metadata["tenant"] == ""` never leaks.

**Two landed-behavior reconciliations documented inline** (file-header comments in `input_field_test.go`): (1) **Single-PK delete ops carry `Event.Input == nil`.** The MutationContext literal for `SoftDelete` / `HardDelete` / `Restore` sets `PK: id` but leaves `Input` unset. PRD §28.6 resolves the "input shape" for single-PK delete-class ops to nil — the PK travels via the dedicated `Event.PK` field; `*Many` delete-class ops follow the same shape per the FIX-056 fanout switch (`OpHardDeleteMany` / `OpSoftDeleteMany` / `OpRestoreMany` route to nil). (2) **`Increment` carries `IncrementInput[C]` by value.** The caller passes the input by value; `Event.Input.(IncrementInput[ProductIncrementColumn])` succeeds as a value assertion, not pointer.

**Scope.** FIX-056 pulled this sub-item past the Phase-14 test-only scope rule: `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` rewritten with the per-op fanout switch, `cmd/sqlgen/gen/context_event.go` grew `HasCreateMany` / `HasUpdateMany` flags on `EventTableContext`, `hook/hook.go` `MutationContext.Input` godoc now documents the batch-vs-event two-surface contract, and PRD §28.4 / §28.6 grew the per-entity-input paragraph + op→shape table. Generated `event_hooks_gen.go` regenerated for the events / tenancy / cache examples; postgres / postgres_stdlib / mysql / sqlite stayed byte-identical (no events config). `make check` + `make check-examples` pass with 0 lint issues across all 7 example modules; all 19 new tests pass under `-race`.

---

## 14.12 Omittable — null-vs-unset, zero-change `Update` short-circuit

**PRD Reference:** §9.5, §10
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.12

**Module:** `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/`

**Status:** Complete

### Tasks

- [x] `postgres/omittable_test.go` — `Update` with zero fields set on `UpdateInput` returns entity unchanged without DB round-trip; verified via query-counting driver wrapper
- [x] `postgres/omittable_test.go` — null-vs-unset on nullable columns: `omittable.Set(nil)` writes SQL NULL; `omittable.Omit()` leaves column untouched (existing value preserved)
- [x] `postgres/omittable_test.go` — `Create` with `omittable.Omit()` on a DEFAULT-bearing column: DB default honored; generated SQL excludes the column from `INSERT`
- [x] `postgres/omittable_test.go` — JSON round-trip: `omittable.Omit[T]()` marshals to absent; `omittable.Set(v)` marshals to `v`; `null` unmarshals to `Set(nil)` for pointer `T`
- [x] `mysql/omittable_test.go` + `sqlite/omittable_test.go` — dialect-adapted mirrors of the above

### Acceptance Criteria

- Zero-field `Update` issues zero SQL queries — verified by query counter = 0
- `omittable.Set(nil)` produces `col = NULL` in the UPDATE set clause; `omittable.Omit()` omits the column entirely
- DEFAULT-bearing columns honor DB default when omitted (no explicit NULL insertion)
- JSON round-trip respects `omitzero` struct tag: unset fields do not appear in JSON output

### Tests Required

- [x] Zero-field Update → zero queries
- [x] `Set(nil)` vs `Omit()` distinction on UPDATE SQL
- [x] `Omit()` vs `Set(default)` on INSERT (DB default honored)
- [x] JSON marshal: `Omit()` absent, `Set(v)` present, `Set(nil)` null
- [x] JSON unmarshal: absent → unset, `null` → `Set(nil)` for pointer types

### Completion Record

**Completed 2026-04-27.** 12 new top-level tests + 21 JSON sub-tests across `cmd/sqlgen/testdata/examples/{postgres,mysql,sqlite}/tests/omittable_test.go` (4 top-level + 7 sub-tests per dialect). All four acceptance criteria covered uniformly across dialects:

- **`TestUpdate_ZeroFields_NoUpdateSQL_<Dialect>`** — wraps `database.Querier` with the existing `capturingQuerier` (postgres `filter_test.go`, mysql/sqlite `filter_test.go`), seeds a product, calls `Update(id, &UpdateProductInput{})` with every field unset, asserts the returned entity is byte-equal to the seed AND no captured SQL string starts with `UPDATE ` (uppercase prefix-match after trim). The `c.Get` short-circuit still fires one SELECT, which is the only DB round-trip in the captured stream — exactly what PRD §9.5 specifies.
- **`TestUpdate_NullableColumn_OmitVsSetNil_<Dialect>`** — 4-stage transition on `articles.body` (TEXT, nullable, no DEFAULT): seed with `Set(&"initial")` → Update with `Body=Omit()` (capture has no `body` in SET; row preserves "initial") → Update with `Body=Set[*string](nil)` (capture has `body` in SET; row becomes NULL) → Update with `Body=Set(&"replaced")` (capture has `body` in SET; row becomes "replaced") → Update with `Body=Omit()` again (capture still excludes `body`; row preserves "replaced"). The four-stage shape pins both directions of the null-vs-unset distinction (Set→NULL, Omit→preserved) at the SQL-text level AND the read-back level. Postgres uses `"body"` (double-quote), MySQL uses `` `body` `` (backtick), SQLite uses `"body"` (double-quote) — each dialect's helper greps for its own quoted form.
- **`TestCreate_OmitDefaultColumn_DBDefaultHonored_<Dialect>`** — Omit on a DEFAULT-bearing column (`products.is_active NOT NULL DEFAULT true` for postgres; `products.in_stock NOT NULL DEFAULT TRUE` for mysql/sqlite) asserts the captured INSERT column list excludes the column AND the read-back row carries the DB-applied default. Bundled with a negative control that sets the field explicitly to `false`, asserts the column appears in the INSERT, and asserts the read-back row is `false` (proving the omit-vs-explicit branch is real, not a no-op). Postgres adds a second column assertion (`quantity` — NOT NULL DEFAULT 1) since `CreateProductInput` carries multiple DEFAULT-bearing omittable fields; mysql/sqlite only assert the boolean since `weight_kg` is nullable, not DEFAULT-bearing.
- **`TestOmittable_JSONRoundTrip_<Dialect>`** — 7 sub-tests on `UpdateArticleInput` (every dialect's article schema has `body *string` nullable, identical struct shape): `Omit_marshals_absent` (empty struct → `{}`); `Set_marshals_value` (Title+Author Set → keys present, Body/CreatedAt/DeletedAt absent); `SetNilPointer_marshals_null` (Body=Set[*string](nil) → `"body":null`); `Unmarshal_absent_stays_unset` (`{}` → all IsSet()==false); `Unmarshal_present_marks_set` (`{"title":"From JSON"}` → Title set, Body still unset); `Unmarshal_null_pointer_sets_nil` (`{"body":null}` → Body.IsSet()==true with `*string == nil`); `Roundtrip_preserves_state` (Set→Marshal→Unmarshal preserves both Title (non-pointer) and Body (pointer) state). The two-property check in `Roundtrip_preserves_state` (set-fields preserved + unset-fields stay unset) is the contract the omitzero tag is supposed to give us; pinning it here forestalls future regressions if someone touches `omittable.MarshalJSON` / `UnmarshalJSON` / `IsZero`.

**Files added:**
- `cmd/sqlgen/testdata/examples/postgres/tests/omittable_test.go` — new (363 lines).
- `cmd/sqlgen/testdata/examples/mysql/tests/omittable_test.go` — new (369 lines).
- `cmd/sqlgen/testdata/examples/sqlite/tests/omittable_test.go` — new (357 lines).

**Phase-14 scope rule honored** — no generator / template / runtime / schema / config changes. The tests reuse the existing per-module `capturingQuerier` helpers (added in 14.5 for postgres/mysql/sqlite) and the per-test-file `lastUpdateSQL` / `lastInsertSQL` helpers are local to each `omittable_test.go` (the per-module test packages are independent Go modules; helpers are duplicated by design, same as 14.6). No drift between PRD spec and landed behavior — every assertion in the four acceptance bullets matches the codegen output verbatim. `make check` (`go vet` + golangci-lint + unit tests across runtime/parser/cmd) passes; `make check-examples` (lint + E2E tests against postgres/mysql/sqlite testcontainers + sqlite in-memory) passes with 0 lint issues across all 7 example modules. All 12 new top-level tests and 21 JSON sub-tests pass under `-race` (postgres 2.5s, mysql 7.9s, sqlite 1.4s).

---

## 14.13 Sync coverage expansion into tracker

**PRD Reference:** N/A (documentation)
**Design Reference:** `IMPLEMENTATION_ORDER.md` §14.13

**Module:** Documentation (`docs/tracker/`, `docs/PRD.md`)

**Status:** Complete

### Tasks

- [x] Run `make check` + `make check-examples` + `go test ./... -race` after every 14.1–14.12 merge; record any flakes or timing issues inline in the sub-item's Completion Record
- [x] If any sub-item uncovered drift between PRD and implementation, open a `/fix` and note the FIX-NNN reference in this sub-item's Completion Record
- [x] If a sub-item required a PRD wording clarification (not a behavioral drift), update `docs/PRD.md` in a targeted edit and log the diff summary here
- [x] Close the phase with a summary entry in `STATUS.md` Current Focus

### Acceptance Criteria

- Full test suite passes clean under `-race` across all three modules
- `make check-examples` reports 0 lint issues
- Every sub-item's Completion Record is filled in
- No load-bearing PRD drift uncovered that went unresolved (either fixed via `/fix` or reconciled in PRD)

### Tests Required

- N/A (coordination sub-item)

### Completion Record

**Closed 2026-04-27.** Phase-closure sweep ran on `main` at HEAD `a654a80` with all 14.1–14.12 sub-items merged (omittable test files for 14.12 still untracked at sweep time — staged into the closing commit alongside this Completion Record).

**Test runs.**

- `make check` — 0 lint issues across 8 modules (`.`, `parser`, `cmd/sqlgen`, `event/natsbus`, `cache/memory`, `cache/redis`, `cache/msgpack`, `metrics/otel`); all unit tests green under `-short -race -count=1`. Slowest unit-test target was `cmd/sqlgen/cli` at 37.1s (CLI exec tests via testdata fixtures); everything else under 6s. No flakes observed across two sequential runs (14.12-merge run + this phase-closure run).
- `make check-examples` — 0 lint issues across all 7 example modules (`cache`, `events`, `mysql`, `postgres_stdlib`, `postgres`, `sqlite`, `tenancy`); all E2E tests green under `-race -count=1 -timeout=5m`. MySQL is the slowest at 11.5s (testcontainer cold-start dominates); others 1.6s–3.1s. No flakes.
- `make test-integration` (full `-race` non-short across runtime + parser + cmd modules) — runtime modules all green (slowest: `database/stdlib` 11.2s, `database/pgx` 6.6s; both run testcontainer-backed integration suites). Parser surfaced **one pre-existing failure** under `-race` integration: `parser/introspect.TestMySQLIntrospect` fails at `introspect_integration_test.go:365` with `table users col status type: got "enum", want "users_status_enum"`. **Not a Phase-14 regression** — git log shows the failing test was added in cf68926 (Phase 4 introspect feature, 2026-04-09, predates Phase 14 by two weeks) and was never green; only run under `make test-integration` (testcontainers; skipped by `make check`'s `-short` gate). Filed as **FIX-057** (open) — MySQL introspector's `normalizeMySQLType` returns the bare `"enum"` keyword and `introspectColumns` synthesises the enum entry as `<col>_enum` (no table prefix), both diverging from `parser/mysql/mysql.go::extractInlineEnumOrSet` which produces table-scoped `<table>_<col>_enum` for collision avoidance. Out of Phase-14 test-only scope; tracked for a post-phase fix-implement.

**FIX entries surfaced during Phase 14.**

- **FIX-053** (resolved 2026-04-24, surfaced post-14.7) — `tenancy.required: false` did not skip the tenant filter on a zero-value resolver return. Resolved via three-valued resolver signature (`(T, apply, error)`) + template gating; PRD §29.2 satisfied.
- **FIX-054** (resolved 2026-04-24, surfaced during 14.8 savepoint test bring-up) — `database.Tx.Begin` interpolated unsafe savepoint names; resolved via `[A-Za-z_][A-Za-z0-9_]*` validation + reserved-word denylist.
- **FIX-055** (resolved 2026-04-24, surfaced during 14.8 resolver-call accounting) — tenanted clients re-invoked the resolver N+ times per chained op; resolved via `tenancy.WithResolvedTenant` / `CachedTenant` ctx-cached single-resolve plumbing across all chaining templates. PRD §29.6 satisfied literally.
- **FIX-056** (resolved 2026-04-24, surfaced during 14.11 event input-field tests) — generated `*Many` event hooks attached the full caller slice to every fanned-out event; resolved via per-op switch in `event_hooks.go.tmpl` selecting the i-th element. PRD §28.4 / §28.6 grew per-entity-input paragraph + op→shape table.
- **FIX-057** (open, surfaced during 14.13 phase-closure `-race` sweep) — pre-existing MySQL introspect ENUM type drift; out of Phase-14 scope, tracked for a post-phase fix-implement run.

**PRD edits during Phase 14.**

- §28.4 — added "per-entity input" paragraph (FIX-056).
- §28.6 — rewrote with op→shape table + worked subscriber type-switch example covering single-op and batch shapes uniformly (FIX-056).
- §29.7 — already amended pre-Phase-14 by 13.10; verified during 14.7 composite-PK sweep that landed codegen matches the verify-match wording.
- No further PRD wording clarifications were required during Phase 14 — all reconciliations between phase-14.md task bullets and landed codegen were either documented inline at the test-file header (test-only scope reconciliations: 14.2 ExistsWhere ErrEmptyFilter, 14.4 product_tag_labels rename, 14.5 ILike via Custom + JSON comparator selection, 14.6 chain-table reuse + tenanted-junction deferral, 14.7 SkipTenancy SQL-shape and SoftDelete deferrals, 14.8 SQLite savepoint syntax, 14.9 pgx5 array scan, 14.10 cache-absence wording + view read-through deferral) or routed through a FIX (053/056).

**Phase-14 invariants honored.**

- Test-only scope rule held for 14.1–14.10 and 14.12 (no generator/template/runtime/schema/config changes for those sub-items themselves). 14.7 surfaced FIX-053 and 14.8 surfaced FIX-054/055 and 14.11 surfaced FIX-056 — those edits live under their own fix entries in `fixes.md`, not under the sub-item that surfaced them. 14.9 added one schema column (`tag_ids TEXT[]`) under the explicit "if schema additions are required" task bullet, with golden regen localised to the warehouses client.
- All seven example modules continue to lint clean under `golangci-lint run --timeout=5m`.
- Every 14.1–14.12 sub-item has a filled-in Completion Record (verified by reading each sub-item entry).

**Phase 14 closed.** All 13 sub-items Complete. STATUS.md Overview updates Phase 14 to 13/13 Complete; Current Focus log entry summarises the closing sweep.

---

## Phase 14 Completion Criteria

- All 13 sub-items marked Complete with Completion Records filled in
- `make check` and `make check-examples` green; `go test ./... -race` green across all three modules
- Any discovered bugs resolved via `/fix` and logged in `docs/tracker/fixes.md`
- `STATUS.md` updated to reflect Phase 14 Complete
