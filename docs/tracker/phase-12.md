# Phase 12: Caching

Status: Not Started
PRD Sections: 27

> **Design reference:** `docs/design/CACHE.md` is the authoritative implementation-level
> supplement. Sub-item acceptance criteria below cite PRD §27 for the public
> contract and CACHE.md for internals (fingerprint algorithm, breaker transition
> table, generator pseudocode, per-file tests, resolved-gaps follow-ups G1–G13).

> **Current state (pre-12 wiring):**
> - `CallOptions.SkipCache` present (Phase 7); `SkipHooks` implies `SkipCache`.
> - `MutationContext.AffectedPKs` populated by every terminal with element shape matching `Cache.Invalidate*` contract — scalar PK for single-PK tables, generated `XXXPK` struct value for composite (verified G4; no pack step needed).
> - `QueryContext` fields (`Op`, `Table`, `Schema`, `PK`, `Input`, `CallOptions`) are sufficient for read-through + hydration; no hook-package changes needed.
> - `event/` + `event/memorybus/` + `event/natsbus/` available — `cache.FromEventSubscriber` consumes `event.Subscriber` directly.
> - Three godoc/reference updates already landed: `database/transaction.go:OnCommit`, `hook/hook.go:MutationContext.AffectedPKs`, `guidelines/ARCHITECTURE.md` singleflight row. These are dependencies of 12.1 / 12.8, not Phase 12.11 work.

---

## 12.1 `cache/` Runtime Core — Backend, Serializer, Key, Breaker, Metrics, Errors, Typed Helpers, Noop

**PRD Reference:** §27.1, §27.3, §27.5, §27.9 (circuit breaker + error policy), §27.10, §27.13
**Design Reference:** CACHE.md §4, §5.1–§5.9, §10.1

**Status:** Complete

### Tasks

- [x] Create `cache/` package at the runtime module root (stdlib + `golang.org/x/sync` only; may import `hook` and `event`)
- [x] `cache/backend.go` — `Backend` interface with mandatory `Get`, `Set`, `Invalidate`, `InvalidateMany`, `InvalidatePattern`
- [x] `cache/backend.go` — optional `StatsReporter` interface + `Stats` struct (`Hits`, `Misses`, `Sets`, `Invalidations`, `Evictions`, `Entries`)
- [x] `cache/backend.go` — Backend godoc codifying "Get miss returns `(nil, nil)`" and "Invalidate* on absent key is success, not error"
- [x] `cache/serializer.go` — `Serializer` interface (`Marshal`, `Unmarshal`) and default `JSONSerializer` using `encoding/json`
- [x] `cache/key.go` — `BuildKey(prefix, schema, table, fingerprint string, pk any) string` returning `{prefix}:{schema}.{table}:fingerprint:v{fingerprint}:pk:{pk}` (labeled-segment grammar, PRD §27.5); omits `{schema}.` when schema is `""`; omits `:fingerprint:v{fingerprint}` when fingerprint is `""`
- [x] `cache/key.go` — `BuildCompositeKey(prefix, schema, table, fingerprint string, pks []any) string` joining composite PK components with `:` after the terminal `pk:` label in caller-provided order (CACHE.md §5.6 invariant)
- [x] `cache/key.go` — `BuildTablePattern(prefix, schema, table string) string` returning `{prefix}:{schema}.{table}:*` — neither `fingerprint:` nor `pk:` segment included, by design
- [x] `cache/metrics.go` — `MetricsRecorder` interface with schema-leading parameter on every per-table method (`Hit`, `Miss`, `Set`, `Invalidate`, `Error`, `HydrationStart`, `HydrationComplete`, `CircuitBreakerStateChange`, `GetLatency`, `SetLatency`, `InvalidateLatency`)
- [x] `cache/metrics.go` — `CircuitState` enum (`StateClosed`, `StateOpen`, `StateHalfOpen`) with `String()`
- [x] `cache/breaker.go` — `Breaker` type with `BreakerConfig` (`FailureThreshold`, `ProbeInterval`, `HalfOpenMaxProbes`, `OnStateChange`) and `NewBreaker(cfg)` constructor
- [x] `cache/breaker.go` — `Allow() bool`, `RecordSuccess()`, `RecordFailure()`, `State() CircuitState` methods implementing the full transition table from CACHE.md §5.7
- [x] `cache/breaker.go` — internal `probesInFlight` atomic gate for `HalfOpen`; `OpenedAt` for the Open→HalfOpen timer; no background goroutine (`Allow()` is the transition clock)
- [x] `cache/error.go` — `OnErrorFunc func(ctx context.Context, op, schema string, table hook.TableName, err error)` type alias
- [x] `cache/error.go` — `DefaultOnCacheError` implementation logging via `log.Printf` (schema segment omitted when empty)
- [x] `cache/typed.go` — generic helpers `GetAs[T]`, `SetAs[T]`, `GetOrSet[T]` using `Backend` + `Serializer` (free functions, NOT methods — see CACHE.md §22 decision #15)
- [x] `cache/typed.go` — `KeysFromAny[T any](pks []any, key func(T) string) ([]string, error)` with per-element type-assertion and descriptive error naming index/expected/actual
- [x] `cache/typed.go` — godoc on `KeysFromAny` / `GetAs` / `SetAs` / `GetOrSet` stating "T MUST be a value type; pointer PKs are forbidden" (G12)
- [x] `cache/noop.go` — `NoopBackend` struct implementing `Backend` + `StatsReporter` as no-ops (Get always misses, Set/Invalidate* succeed, Stats returns zero values)
- [x] `cache/backend_test.go` — compile-time assertions `var _ Backend = (*fakeBackend)(nil)` for `Backend`, `StatsReporter`, `io.Closer`
- [x] `cache/serializer_test.go` — JSON `Marshal` / `Unmarshal` round-trip for a sample generated-style struct (fields with `db` / `json` tags)
- [x] `cache/key_test.go` — table-driven grammar coverage: PostgreSQL-with-schema, MySQL/SQLite empty-schema, composite PK two-and-three-column cases, fingerprint-presence assertion, fingerprint-agnostic `BuildTablePattern`
- [x] `cache/breaker_test.go` — cases (a)–(j) from CACHE.md §20.1 (see Tests Required below); invoked with `-race`
- [x] `cache/error_test.go` — `DefaultOnCacheError` format with and without schema; three-channel ordering (metrics → OnErrorFunc → breaker) verified via a recording fake; panicking `OnErrorFunc` does not skip breaker `RecordFailure`; nil `MetricsRecorder` short-circuits the metrics call
- [x] `cache/typed_test.go` — round-trip coverage for `GetAs` / `SetAs` / `GetOrSet`, miss returns `(zero, false, nil)`, `load` error in `GetOrSet` propagates without a `Set` attempt, `KeysFromAny` empty-slice + happy-path + type-mismatch error format
- [x] `cache/noop_test.go` — `NoopBackend` satisfies `Backend` + `StatsReporter`; Get always misses; Invalidate* all succeed as no-ops; Stats returns zero `Stats` struct

### Acceptance Criteria

- `cache/` runtime package imports only stdlib, `golang.org/x/sync` (reserved for singleflight in 12.8), `hook`, and `event` — no parser, CLI, or cross-runtime imports (CLAUDE.md three-module rule)
- `Backend` interface exposes exactly `Get`, `Set`, `Invalidate`, `InvalidateMany`, `InvalidatePattern` — all mandatory; no capability gating on pattern invalidation (PRD §27.3)
- `Backend.Get` contract: miss returns `(nil, nil)`, non-nil error only for real backend failure
- `Backend.Invalidate` / `InvalidateMany` / `InvalidatePattern` contract: absent key is success, not error — custom backends must honor this
- Pattern grammar accepted by the contract: prefix + trailing `*` (backends may support richer forms but the contract is minimal)
- Graceful shutdown uses stdlib `io.Closer`, not a custom `Closeable` interface
- `Serializer` interface matches PRD §27.10 exactly (`Marshal(v any) ([]byte, error)` / `Unmarshal(data []byte, v any) error`); `JSONSerializer` is the stdlib default
- `BuildKey` / `BuildCompositeKey` emit the labeled-segment grammar `{prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}` when fingerprint is non-empty; drop the `fingerprint:v{fp}:` segment when fingerprint is `""` so the remaining key is still well-formed (PRD §27.5)
- `pk:` is ALWAYS the terminal labeled segment — for composite PKs the components are colon-separated under `pk:` (no ambiguity for parsers, leaves room for future labeled segments like `tenant:` without disturbing PK encoding)
- `BuildTablePattern` deliberately omits BOTH `fingerprint:` and `pk:` segments so pattern invalidations clear every fingerprint generation AND every PK in one pass (PRD §27.5)
- `BuildKey` / `BuildCompositeKey` treat `schema == ""` as "omit `{schema}.`" so MySQL/SQLite keys look like `sqlgen:products:fingerprint:v1a2b3c:pk:42`
- `MetricsRecorder` interface methods all take `schema string` as the leading per-table parameter so same-name tables in different schemas do not collapse into a single metric series (G13)
- `CircuitBreakerStateChange(from, to CircuitState)` is not schema-labeled because the breaker is `*Cache`-scoped (PRD §27.13)
- `Breaker` implements every transition in CACHE.md §5.7 table — Closed streak → Open, Open probe interval → HalfOpen, HalfOpen success → Closed, HalfOpen failure → Open; `counter` is meaningful only in Closed; `Allow()` never modifies counter; records received in Open are ignored
- `Breaker` is race-free under `go test -race` for concurrent `Allow()` + `RecordX` interleaving
- `OnErrorFunc` / `DefaultOnCacheError` signatures match CACHE.md §5.8 exactly and carry `schema` so stderr logs from same-name tables in different schemas stay distinguishable
- Cache backend errors route through metrics → OnErrorFunc → breaker in order; a panicking OnErrorFunc does not skip breaker accounting (callback is wrapped in `defer recover()`)
- Zero-cost path when `MetricsRecorder` is nil: single nil-check per call site, no allocations
- Typed helpers (`GetAs`, `SetAs`, `GetOrSet`, `KeysFromAny`) are free functions — not methods on a generic `Backend[T]` (CACHE.md §22 decision #15)
- Typed-helper godoc states "T MUST be a value type; pointer PKs are forbidden" (G12) — documents the boundary rule; the generated facade uses typed `keyFor{Table}(pk T)` helpers so generated code can't hit the mistake
- `NoopBackend` satisfies `Backend` + `StatsReporter` so it is a drop-in replacement anywhere a real backend is expected (tests, disabled-cache toggles, facade smoke tests)

### Tests Required

- [x] `backend_test.go`: compile-time `var _ Backend = (*X)(nil)` / `var _ StatsReporter = (*X)(nil)` assertions pass for a minimal fake and for `NoopBackend`
- [x] `serializer_test.go`: round-trip a struct with `db` and `json` tags; field values survive
- [x] `key_test.go`: PostgreSQL key with schema → `sqlgen:public.products:fingerprint:v1a2b3c:pk:42`
- [x] `key_test.go`: MySQL/SQLite empty-schema → `sqlgen:products:fingerprint:v1a2b3c:pk:42` (no `{schema}.` prefix)
- [x] `key_test.go`: composite PK preserves caller-provided order under terminal `pk:` label (two-column: `sqlgen:public.order_items:fingerprint:v4e5f6g:pk:order123:product456`; three-column coverage)
- [x] `key_test.go`: `BuildTablePattern(...)` returns `{prefix}:{schema}.{table}:*` with no `fingerprint:` or `pk:` segment — regression guard against accidental label inclusion
- [x] `key_test.go`: fingerprint-presence case — `BuildKey(..., "1a2b3c4d", ...)` includes `:fingerprint:v1a2b3c4d:pk:`; empty fingerprint drops the entire `:fingerprint:v:` segment while keeping `:pk:`
- [x] `key_test.go`: label-ordering invariant — keys always appear as `{prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}` in that order; no other permutation is valid
- [x] `breaker_test.go` case (a): `Closed` + `RecordSuccess` resets counter to 0
- [x] `breaker_test.go` case (b): `Closed` counter advances to N-1 without tripping
- [x] `breaker_test.go` case (c): Nth consecutive `RecordFailure` in `Closed` trips to `Open` and fires `OnStateChange(Closed, Open)`
- [x] `breaker_test.go` case (d): `Allow()` in `Open` returns `false` until `ProbeInterval` elapses
- [x] `breaker_test.go` case (e): `Allow()` post-`ProbeInterval` transitions to `HalfOpen` and fires `OnStateChange(Open, HalfOpen)`
- [x] `breaker_test.go` case (f): `HalfOpen` probe success returns to `Closed` with counter reset to 0
- [x] `breaker_test.go` case (g): `HalfOpen` probe failure re-opens immediately with default `HalfOpenMaxProbes = 1`
- [x] `breaker_test.go` case (h): `probesInFlight` saturation bypasses followers — only first `HalfOpenMaxProbes` callers get `Allow() == true` in `HalfOpen`
- [x] `breaker_test.go` case (i): `RecordSuccess` / `RecordFailure` received while `Open` are ignored (state unchanged)
- [x] `breaker_test.go` case (j): concurrent `Allow()` + `RecordX` correctness under `go test -race` — no dropped or double-counted transitions
- [x] `error_test.go`: `DefaultOnCacheError` with non-empty schema logs `cache {op} {schema}.{table}: {err}`
- [x] `error_test.go`: `DefaultOnCacheError` with empty schema logs `cache {op} {table}: {err}` (MySQL/SQLite shape)
- [x] `error_test.go`: all three channels fire in order — fake recorder records call sequence `metrics → OnErrorFunc → breaker`
- [x] `error_test.go`: panicking `OnErrorFunc` does not skip breaker `RecordFailure`
- [x] `error_test.go`: nil `MetricsRecorder` takes the zero-cost path (no crash, no allocations)
- [x] `typed_test.go`: `GetAs` miss returns `(zero, false, nil)` — distinguishes miss from zero-value decode
- [x] `typed_test.go`: `SetAs` marshal error propagates without touching the backend
- [x] `typed_test.go`: `GetOrSet` on miss calls `load`, stores, returns; on hit skips `load`; `load` error propagates without `Set`
- [x] `typed_test.go`: `KeysFromAny` happy path produces keys in input order; empty slice returns `[]string{}` with nil error
- [x] `typed_test.go`: `KeysFromAny` type-mismatch error format names element index, expected type, and actual type
- [x] `noop_test.go`: `NoopBackend.Get` always returns `(nil, nil)`; Set and Invalidate* all return nil; Stats returns zero `Stats` struct

### Completion Record

**Date:** 2026-04-21

**Files added** (all in `cache/`):
- `backend.go` — `Backend`, `StatsReporter`, `Stats` (contract godoc on miss + absent-key)
- `serializer.go` — `Serializer`, `JSONSerializer` (errors wrapped with `cache json marshal/unmarshal:` prefix)
- `key.go` — `BuildKey`, `BuildCompositeKey`, `BuildTablePattern`
- `metrics.go` — `MetricsRecorder`, `CircuitState` + `String()`
- `breaker.go` — `Breaker`, `BreakerConfig`, `NewBreaker`, `Allow`, `RecordSuccess`, `RecordFailure`, `State` — sync.Mutex + internal `now func()` for testability; `Allow()` is the Open→HalfOpen transition clock
- `error.go` — `OnErrorFunc`, `DefaultOnCacheError`, **`RouteError`** (added: three-channel dispatcher shared by the generated facade; metrics → OnErrorFunc → breaker with `defer recover()` around OnErrorFunc so a panicking logger never skips breaker accounting)
- `typed.go` — `GetAs`, `SetAs`, `GetOrSet`, `KeysFromAny` (all free functions, not methods; CACHE.md §22 decision #15; T-must-be-value godoc)
- `noop.go` — `NoopBackend` (satisfies `Backend` + `StatsReporter`)

**Tests added** (`cache/*_test.go`): backend interface assertions, JSON serializer round-trip, key grammar (7 sub-tests incl. label-ordering + fingerprint-presence), breaker transition cases (a)–(j) with `-race`, error three-channel ordering + panic-safety + nil-metrics, typed helper round-trips + error paths + KeysFromAny error format, NoopBackend semantics.

**Notes:**
- `RouteError` added beyond the literal task list so the three-channel acceptance criterion / test could be implemented in the runtime package (generated facade in 12.8 will call it). Acceptance criteria explicitly require "Cache backend errors route through metrics → OnErrorFunc → breaker in order; a panicking OnErrorFunc does not skip breaker accounting (callback is wrapped in `defer recover()`)" — routing this through a single helper avoids duplicating the `defer recover()` pattern in every generated Get/Set/Invalidate call site.
- `cache/` imports only stdlib + `hook` (for `TableName`). `golang.org/x/sync/singleflight` import is reserved for 12.8 per CACHE.md §5.1.
- `make check` (lint + `-short -race` unit tests across all four modules) is clean.

---

## 12.2 `cache/` Invalidation — `InvalidationSource` + `FromEventSubscriber`

**PRD Reference:** §27.9 (InvalidationSource, FromEventSubscriber adapter)
**Design Reference:** CACHE.md §5.3, §5.4, §24 G1 resolution

**Status:** Complete

### Tasks

- [x] `cache/invalidation.go` — `InvalidationSource` interface: `Subscribe(handler InvalidationHandler) (InvalidationSubscription, error)`, `Close() error`
- [x] `cache/invalidation.go` — `InvalidationHandler func(ctx context.Context, table hook.TableName, pks []any) error` type (table is `hook.TableName`, not `string`)
- [x] `cache/invalidation.go` — `InvalidationSubscription` interface: `Unsubscribe() error`
- [x] `cache/invalidation.go` — godoc on `InvalidationHandler` showing the raw-string cast pattern for Layer 2 callers: `handler(ctx, hook.TableName(rawTable), pks)` (G11)
- [x] `cache/invalidation.go` — godoc: "empty `pks` signals full-table invalidation (backend uses `InvalidatePattern`)"
- [x] `cache/event_adapter.go` — `FromEventSubscriber(sub event.Subscriber) InvalidationSource` — subscribes with empty `event.SubscribeOptions{}` (all tables, all actions)
- [x] `cache/event_adapter.go` — each received `event.Event` produces exactly one `InvalidationHandler` call with `pks = []any{ev.PK}` (no coalescing window)
- [x] `cache/event_adapter.go` — handler-error routing: call `MetricsRecorder.Error(schema, table, "invalidate", err)` AND return the error from the inner `event.Handler` so ACK-capable transports redeliver
- [x] `cache/event_adapter.go` — `Close()` unsubscribes the underlying `event.Subscription` and is safe to call multiple times
- [x] `cache/event_adapter.go` — no per-event state — at-least-once transports (NATS redelivery, Kafka replay) are safe by backend-idempotence contract, not adapter-side dedup
- [x] `cache/invalidation_test.go` — `FromEventSubscriber` round-trips a single event through `memorybus` to a capturing `InvalidationHandler`; `pks` is exactly `[]any{ev.PK}`
- [x] `cache/invalidation_test.go` — batch fan-out: `Publisher.PublishBatch` with N events produces N separate handler invocations (1:1, not coalesced)
- [x] `cache/invalidation_test.go` — handler error is both recorded on a fake `MetricsRecorder` with `op == "invalidate"` AND returned from the inner `event.Handler`
- [x] `cache/invalidation_test.go` — redelivered events (same `event.ID` replayed) produce a second no-op invalidation call without error — idempotence verified through `NoopBackend` + a counting handler
- [x] `cache/invalidation_test.go` — `Close()` on the adapter unsubscribes from the bus; subsequent `Publish` produces no further handler calls

### Acceptance Criteria

- `InvalidationSource` lives in `cache/` runtime package (stdlib + `golang.org/x/sync` only); imports `hook` (for `TableName`) and `event`
- `InvalidationHandler` table parameter is typed `hook.TableName` so generated `<pkg>.TableXxx` constants flow through without string conversions (PRD §27.9)
- Empty `pks` slice signals full-table invalidation; backend dispatches via `InvalidatePattern` (PRD §27.9 Semantics)
- `FromEventSubscriber` is synchronous-per-event — one event produces one `InvalidationHandler` call — no time-based batching window (G1 resolution; CACHE.md §5.4)
- Publisher-side batching (`PublishBatch`) is preserved transparently — the adapter forwards 1:1; transport-level batching is unchanged
- Idempotence is a `Backend` contract (12.1): absent-key invalidation is success, so the adapter keeps no per-event state — at-least-once transports redeliver safely without `event.ID` dedup
- Handler error routing: **both** recorded via `MetricsRecorder.Error(schema, table, "invalidate", err)` **and** returned from the inner `event.Handler` — fire-and-forget transports drop it after their own error step; ACK-capable transports redeliver (G1 resolution)
- Nothing is silently swallowed — metrics OR return, never neither

### Tests Required

- [x] 1 event published → 1 `InvalidationHandler` call with `pks = []any{ev.PK}`
- [x] `PublishBatch` with N events → N handler calls (fan-out, not coalesced)
- [x] Handler-returned error is recorded on `MetricsRecorder.Error(schema, table, "invalidate", err)` (spy captures `op == "invalidate"`)
- [x] Handler-returned error propagates from the inner `event.Handler` to the bus
- [x] Redelivered events (same `event.ID` replayed) produce a second no-op invalidation without error
- [x] `Close()` unsubscribes the underlying `event.Subscription`; subsequent `Publish` produces no handler calls

### Completion Record

**Date:** 2026-04-21

**Files added** (all in `cache/`):
- `invalidation.go` — `InvalidationSource`, `InvalidationHandler`, `InvalidationSubscription` (PRD §27.9 signatures; godoc covers the `hook.TableName(raw)` Layer-2 cast pattern and the empty-pks → `InvalidatePattern` semantics)
- `event_adapter.go` — `FromEventSubscriber(sub event.Subscriber, opts ...EventAdapterOption) InvalidationSource`, `EventAdapterOption`, **`WithEventMetrics`** (see note below). The adapter's event-handler shim casts `ev.Table` to `hook.TableName`, forwards `pks = []any{ev.PK}` 1:1, records via `metrics.Error(ev.Schema, table, "invalidate", err)` on handler error when metrics is wired, and returns the handler's error to the inner `event.Handler` for ACK-capable transports. `Close()` is sync.Mutex-guarded and idempotent; the returned `InvalidationSubscription.Unsubscribe` is `sync.Once`-idempotent.

**Tests added** (`cache/invalidation_test.go`):
- Single-event round-trip through `memorybus` → one handler call with `pks = []any{ev.PK}`.
- `PublishBatch` fan-out — N events yield N handler calls in order (no coalescing).
- Handler-error routing through a `fakeSubscriber` that captures the shim (memorybus swallows handler errors, so a fake is required to assert error propagation): verifies both `metrics.Error` records `op="invalidate"`, `schema="public"`, the correct `hook.TableName`, and wrapped error AND the inner `event.Handler` returned the error.
- Success path: no `metrics.Error` record when handler returns nil.
- Redelivery idempotence — same `event.ID` published twice drives two handler invocations through `NoopBackend` without error.
- `Close()` unsubscribes from `memorybus`; subsequent `Publish` produces no handler calls; second `Close()` call is a no-op.
- `Close()` forwards to the underlying `event.Subscription.Unsubscribe` exactly once (`fakeSubscriber.unsubscribeHit == 1`).
- `Subscribe` passes empty `event.SubscribeOptions{}` (all tables, all actions, no group).
- `Subscribe` error is wrapped and propagates to the caller.
- `InvalidationSubscription.Unsubscribe` is idempotent — second call is a no-op; underlying unsubscribe runs exactly once.
- Compile-time assertions `var _ cache.InvalidationSource` / `_ cache.InvalidationSubscription` on an internal no-op fake.

**Notes:**
- `EventAdapterOption` + `WithEventMetrics` added beyond the literal PRD signature to satisfy the "adapter records via `MetricsRecorder.Error(schema, table, \"invalidate\", err)`" acceptance criterion (PRD §27.9 / CACHE.md §5.4 / G1 resolution) without leaking a non-optional metrics dependency into the user-facing constructor. The zero-option call `cache.FromEventSubscriber(bus)` still matches the PRD signature literally; the generated facade (12.8) will wire metrics via `cache.FromEventSubscriber(bus, cache.WithEventMetrics(m))`. The adapter is the only party that can record with the correct `op="invalidate"` label against the event's schema — routing this from inside the generated InvalidationHandler would lose the ev.Schema context at the boundary.
- The fake-subscriber test harness is required because `memorybus.Publish` swallows handler errors (`_ = s.handler(ctx, e)` at `memorybus/memorybus.go:54`), so "handler error propagates from the inner event.Handler" cannot be verified with `memorybus` alone. The fake captures the shim and calls it directly so its return value is observable.
- `cache/` now imports `event/` (in addition to `hook`, stdlib, `golang.org/x/sync`) — allowed per `guidelines/ARCHITECTURE.md` and CACHE.md §5.4 ("`cache/` may import `event/` — both are stdlib-adjacent runtime packages").
- `make check` (lint + `-short -race` unit tests across all four modules) is clean.

---

## 12.3 Config — `CacheConfig`, resolvers, validation

**PRD Reference:** §27.2 (global + per-table + per-view), §27.11 (view opt-in rule)
**Design Reference:** CACHE.md §3.2, §3.3, §3.4, §24 G10 resolution

**Status:** Complete

### Tasks

- [x] Add `CacheConfig` to `cmd/sqlgen/config/config.go`: `Enabled`, `Version int`, `TTL string`, `Serializer Serializer`, `KeyPrefix string`, `Hydration *HydrationConfig`, `CircuitBreaker *CircuitBreakerConfig`
- [x] Add `HydrationConfig`: `Enabled bool`, `Timeout string`
- [x] Add `CircuitBreakerConfig`: `Enabled bool`, `FailureThreshold int`, `ProbeInterval string`, `HalfOpenMaxProbes int`
- [x] Add `Serializer` enum with constants `SerializerJSON` / `SerializerMsgpack` / `SerializerCustom`
- [x] Add `RootConfig.Cache *CacheConfig` field with `cache` YAML tag
- [x] Add `TableCacheConfig` (tri-state pointers): `Enabled *bool`, `TTL *string`, `Serializer *Serializer`
- [x] Add `TableConfig.Cache *TableCacheConfig` field with `cache` YAML tag
- [x] Extend `ViewConfig` — currently `{StructName, SQL}` — with `Cache *ViewCacheConfig` and `InvalidateOn []string`
- [x] Add `ViewCacheConfig` (tri-state pointers): `Enabled *bool`, `TTL *string`, `Serializer *Serializer`
- [x] Implement `ResolveTableCacheEnabled(table, global) bool` — per-table override → global → default (per-table `enabled: true` opts in even when global is off)
- [x] Implement `ResolveTableCacheTTL(table, global) time.Duration`
- [x] Implement `ResolveTableCacheSerializer(table, global) Serializer`
- [x] Implement `ResolveViewCacheEnabled(view, global) bool` — view caching is **never** inherited from global `cache.enabled`; returns true only when `view.cache.enabled` is explicitly true (CACHE.md §3.3)
- [x] Implement `ResolveViewCacheTTL(view, global) time.Duration`
- [x] Implement `ResolveViewCacheSerializer(view, global) Serializer`
- [x] YAML-parse validation: `cache.ttl` parses as `time.Duration` if set, empty defaults to `1h`
- [x] YAML-parse validation: `cache.serializer` ∈ {`json`, `msgpack`, `custom`, ``}; empty defaults to `json`
- [x] YAML-parse validation: `cache.key_prefix` empty or unset → default `"sqlgen"`; emit stderr warning `sqlgen: cache.key_prefix unset — using default "sqlgen". Set cache.key_prefix explicitly to silence this warning.` (G10); explicit `""` triggers the same warning; any non-empty string suppresses it and is used verbatim
- [x] YAML-parse validation: `cache.hydration.timeout` parses as `time.Duration`; empty defaults to `30s`
- [x] YAML-parse validation: `cache.circuit_breaker.failure_threshold ≥ 1`, `probe_interval > 0`, `half_open_max_probes ≥ 1`
- [x] YAML-parse validation: `views.*.cache.enabled: true` requires non-empty `views.*.invalidate_on` — hard error naming the view (no TTL-only view caches in v1)
- [x] Post-parse validation: every `views.*.invalidate_on` name resolves to a table in the parsed schema AND is included in generation — hard error naming the view and the unresolved/excluded table
- [x] Config tests for each validation rule (see Tests Required)

### Acceptance Criteria

- All config Go types match CACHE.md §3.2 exactly — tri-state pointers on `TableCacheConfig` / `ViewCacheConfig` for `Enabled`, `TTL`, `Serializer`
- `RootConfig.Cache`, `TableConfig.Cache`, `ViewConfig.Cache`, `ViewConfig.InvalidateOn` all parse through YAML with the tags documented in PRD §27.2
- `ResolveTable*` helpers: per-table override wins over global; `nil` pointer means inherit; per-table `enabled: true` opts in even under global `cache.enabled: false`
- `ResolveView*` helpers: view caching is **opt-in only** — `ResolveViewCacheEnabled` returns true only when `view.cache.enabled` is explicitly true (CACHE.md §3.3 rationale — views have no mutations; silent inheritance would create TTL-only caches)
- YAML defaults applied at parse time (not at resolver time) so generator and resolvers see canonical values
- `cache.key_prefix` empty/unset → stderr warning emitted from `sqlgen generate`; explicit non-empty → no warning (G10)
- `views.*.cache.enabled: true` without `invalidate_on` → hard parse-time error (not a warning)
- `views.*.invalidate_on` names must resolve to parsed + included tables — hard post-parse error (no mutation hook would fire for excluded tables, silent never-invalidates is unacceptable)
- Construction-time validation deferred to 12.8 generator / NewCache path: `serializer: custom` without `WithSerializer(...)` → error naming the config line (CACHE.md §3.4)
- Note: `serializer: msgpack` does NOT require `WithSerializer(...)` — generator auto-injects (CACHE.md §7 / §19.2); this is checked in 12.8, not here

### Tests Required

- [x] Parse `cache: { enabled: true, ttl: "30m", serializer: msgpack }` at root and round-trip
- [x] Empty `cache:` block → `enabled: false`, defaults applied elsewhere
- [x] Unset `key_prefix` → resolved `"sqlgen"` AND stderr warning emitted (capture stderr)
- [x] Explicit `key_prefix: ""` → resolved `"sqlgen"` AND warning emitted (same as unset)
- [x] Explicit `key_prefix: "myapp"` → resolved verbatim, no warning
- [x] Unset `ttl` → `1h`; explicit `"30m"` → `30 * time.Minute`
- [x] Invalid `ttl: "notaduration"` → parse error
- [x] Unset `serializer` → `json`; `"msgpack"` → `SerializerMsgpack`; `"custom"` → `SerializerCustom`; `"bogus"` → parse error
- [x] `circuit_breaker.failure_threshold: 0` → parse error (must be ≥ 1)
- [x] `circuit_breaker.probe_interval: "0s"` → parse error (must be > 0)
- [x] `circuit_breaker.half_open_max_probes: 0` → parse error (must be ≥ 1)
- [x] Global `cache.enabled: true` + table override `cache.enabled: false` → `ResolveTableCacheEnabled` returns false
- [x] Global `cache.enabled: false` + table override `cache.enabled: true` → `ResolveTableCacheEnabled` returns true (per-table opt-in)
- [x] Global `cache.enabled: true` + view without `cache:` block → `ResolveViewCacheEnabled` returns false (opt-in rule)
- [x] Global `cache.enabled: true` + view with `cache.enabled: true` but empty `invalidate_on` → parse-time hard error naming the view
- [x] View with `cache.enabled: true` + `invalidate_on: [unknown_table]` → post-parse hard error naming the view and `unknown_table`
- [x] View with `cache.enabled: true` + `invalidate_on: [excluded_table]` where `excluded_table` is filtered out of generation → post-parse hard error
- [x] Per-table override precedence: `Global.TTL: "1h"` + `Table.TTL: "30m"` → resolves to `30*time.Minute`; `Table.TTL: nil` → inherits `1h`

### Completion Record

**Date:** 2026-04-21

**Files changed:**
- `cmd/sqlgen/config/config.go` — added `Serializer` enum + `IsValid()`; added `CacheConfig`, `HydrationConfig`, `CircuitBreakerConfig`, `TableCacheConfig`, `ViewCacheConfig`; added `RootConfig.Cache` + `TableConfig.Cache`; extended `ViewConfig` with `Cache` and `InvalidateOn`; added `CacheKeyPrefixDefaulted` tracking field (`yaml:"-"`); added `applyCacheDefaults` (TTL=1h, serializer=json, key_prefix=sqlgen + G10 flag, hydration defaults, circuit breaker defaults); added 6 resolvers (`ResolveTableCacheEnabled/TTL/Serializer`, `ResolveViewCacheEnabled/TTL/Serializer` — views are opt-in and do NOT inherit global `cache.enabled`).
- `cmd/sqlgen/config/validate.go` — added `validateCacheConfig` + `validateCircuitBreakerConfig` (pre-parse) enforcing ttl/serializer/hydration.timeout parseability, circuit breaker bounds, and emitting the G10 key_prefix `Warning`; added `validateViewCacheConfig` (pre-parse) enforcing `views.*.cache.enabled: true` requires non-empty `invalidate_on`; added `validateViewInvalidateOn` + `allColumnsExcluded` (post-parse) requiring every invalidate_on name to resolve to a parsed table AND not be fully filtered via `exclude_columns`; refactored existing `validateExcludeColumnsExhaustion` to reuse `allColumnsExcluded`.
- `cmd/sqlgen/config/config_test.go` — 9 new test cases covering root cache round-trip, empty cache block defaults, key_prefix warning (unset + explicit empty + non-empty), TTL default + explicit parse, table resolver precedence, view resolver opt-in rule, table/view TTL+serializer inheritance.
- `cmd/sqlgen/config/validate_test.go` — 2 new tables: `TestValidatePreParse_CacheConfig` (9 sub-cases covering serializer/ttl/hydration timeout/circuit breaker bounds/view-cache-without-invalidate_on) and `TestValidatePostParse_ViewInvalidateOn` (5 sub-cases covering bare + schema-qualified resolution, unknown-table error, fully-excluded-table error, and the "cache disabled skips check" short-circuit).

**Notes:**
- `key_prefix` stderr warning is routed through the existing `Warning` return from `ValidatePreParse`, which `cli/pipeline.go:195 printWarnings` already prints to `errOut` (stderr). Same pipeline as every other pre-parse warning — no parallel stderr path introduced.
- `Hydration` / `CircuitBreaker` blocks default to their full "enabled + sensible values" struct only when the user omits the block entirely. When the user provides a partial block, only the string-typed `Timeout` is defaulted (so zero `Enabled` stays zero) — this is the same pattern `EventConfig` uses (Enabled is plain `bool`, not `*bool`, per §3.2 literal spec).
- `ResolveViewCacheEnabled` takes the global argument for signature consistency with the table resolver but deliberately ignores it (`_ *CacheConfig`) — the opt-in rule from PRD §27.11 / CACHE.md §3.3 requires view caching to never inherit global `cache.enabled`.
- `allColumnsExcluded` consolidates the column-exhaustion check used by both `validateExcludeColumnsExhaustion` (existing warning) and `validateViewInvalidateOn` (new hard error for cache-enabled views).
- `make check` (lint + `-short -race` unit tests across all four modules) is clean.

---

## 12.4 `cache/memory/` Backend

**PRD Reference:** §27.3 (Shipped Backends row), §27.5 (key grammar)
**Design Reference:** CACHE.md §6.1

**Status:** Complete

### Tasks

- [x] Create `cache/memory/` as a separate Go module (`github.com/teandresmith/sqlgen/cache/memory`) with its own `go.mod`
- [x] Add `cache/memory` to `go.work` `use` directive and to `Makefile`'s `MODULES` list
- [x] Depend on `github.com/maypok86/otter/v2` (implementation detail — not part of the public package surface; package name is `memory`, not `otter`)
- [x] `cache/memory/memory.go` — `Options` struct: `MaxSize int` (required), `DefaultTTL time.Duration`, `StatsEnabled bool`
- [x] `cache/memory/memory.go` — `New(Options) (*Backend, error)` returning a concrete `*Backend` that satisfies `cache.Backend` + `cache.StatsReporter` + `io.Closer`
- [x] `cache/memory/memory.go` — `New` fails when `MaxSize <= 0` — required bound, no silent default (CACHE.md §6.1 / §22 decision #10)
- [x] `cache/memory/memory.go` — `Get` returns `(nil, nil)` for missing keys
- [x] `cache/memory/memory.go` — `Set` with `ttl == 0` uses `Options.DefaultTTL`; if that is also zero, entries never expire
- [x] `cache/memory/memory.go` — `Set` with `ttl > 0` uses per-entry TTL via otter's native per-entry TTL support
- [x] `cache/memory/memory.go` — `Invalidate(key)` / `InvalidateMany(keys)` delete matching entries; absent keys succeed (contract)
- [x] `cache/memory/memory.go` — `InvalidatePattern(pattern)` verifies pattern ends with `*`, walks the entry set, deletes matching entries by prefix
- [x] `cache/memory/memory.go` — `Stats()` returns native otter stats passed through (hits/misses/sets/evictions/entries) when `StatsEnabled: true`
- [x] `cache/memory/memory.go` — `Close()` stops any internal housekeeping and releases the otter cache
- [x] `cache/memory/memory_test.go` — full round-trip + TTL + pattern + Stats + Close coverage

### Acceptance Criteria

- `cache/memory/` is a separate Go module — the `github.com/maypok86/otter/v2` dependency lands only in projects that opt in
- Package name is `memory` (named by capability, not library) so we can swap implementations without a breaking change
- `New(Options{MaxSize: 0})` returns an error — no silent default
- `*Backend` satisfies `cache.Backend`, `cache.StatsReporter`, and `io.Closer` (compile-time `var _` asserts in test)
- `InvalidatePattern` contract: pattern must end with `*`, prefix match against stored entries
- Per-entry TTL works — different keys with different TTLs expire independently (otter native support)
- `Options.DefaultTTL` is the fallback when `Set(ctx, k, v, 0)` is called (direct user code path; generated hook always passes an explicit resolved TTL)
- Thread-safe under concurrent Get/Set/Invalidate/InvalidatePattern (otter's internal lock sharding)
- Per-table memory budgets are NOT enforced by the backend — one `*Backend` holds entries for all tables under a single otter eviction policy (deferred; CACHE.md §6.1 "What cache/memory does NOT enforce")

### Tests Required

- [x] `New(Options{MaxSize: 0})` returns an error naming the field
- [x] Get miss → `(nil, nil)`; Set then Get → returns the stored bytes
- [x] `Set(ctx, k, v, 50*time.Millisecond)` — key returns value immediately, `(nil, nil)` after TTL expiry (clock-advance test or time.Sleep with tolerance)
- [x] `Set(ctx, k, v, 0)` with `DefaultTTL > 0` uses the default; with `DefaultTTL == 0` never expires
- [x] `Invalidate(key)` on present key → subsequent Get miss; on absent key → nil error (contract)
- [x] `InvalidateMany([]string{k1, k2, kMissing})` → k1 and k2 removed, absent key no error
- [x] `InvalidatePattern("prefix:*")` removes every key starting with `prefix:`; `InvalidatePattern("prefix")` (no trailing `*`) returns an error
- [x] `Stats()` after hits + misses + sets returns non-zero counters matching activity (when `StatsEnabled: true`)
- [x] `Close()` succeeds; subsequent Get/Set returns an error (closed backend)
- [x] Concurrent `go test -race`: 100 goroutines issuing mixed Get/Set/Invalidate do not race

### Completion Record

- **Date:** 2026-04-21
- **Files added:**
  - `cache/memory/go.mod` — new module (`github.com/teandresmith/sqlgen/cache/memory`) depending on `github.com/maypok86/otter/v2 v2.3.0`
  - `cache/memory/memory.go` — `Options`, `Backend`, `New`, `Get`, `Set`, `Invalidate`, `InvalidateMany`, `InvalidatePattern`, `Stats`, `Close`, `ErrClosed`
  - `cache/memory/memory_test.go` — table-driven unit tests covering every acceptance criterion and tests-required item
- **Files modified:**
  - `go.work` — added `cache/memory` to the `use` directive
  - `Makefile` — appended `cache/memory` to `MODULES`
- **Notes:**
  - Per-entry TTL uses otter's `ExpiryWritingFunc` with the requested ttl stashed on the stored entry value. When the caller-resolved ttl is 0 (the "never expire" path) the calculator returns `0` so otter leaves the initial `unreachableExpiresAt` sentinel in place — returning a large `math.MaxInt64` duration would overflow otter's internal `now + duration` computation and immediately expire the entry.
  - Sets and Invalidations counters are tracked on the backend via `atomic.Uint64` — otter does not record those natively.
  - `Close()` flips an `atomic.Bool` and calls `otter.Cache.StopAllGoroutines`; subsequent backend calls short-circuit with `ErrClosed` (idempotent close).

---

## 12.5 `cache/redis/` Backend

**PRD Reference:** §27.3 (Shipped Backends row)
**Design Reference:** CACHE.md §6.2

**Status:** Complete

### Tasks

- [x] Create `cache/redis/` as a separate Go module (`github.com/teandresmith/sqlgen/cache/redis`) with its own `go.mod`
- [x] Add `cache/redis` to `go.work` `use` directive and to `Makefile`'s `MODULES` list
- [x] Depend on `github.com/redis/go-redis/v9`
- [x] `cache/redis/redis.go` — `New(client redis.UniversalClient, opts ...Option) *Backend` — wraps caller-owned client by default; does not own connection lifecycle unless opted in
- [x] `cache/redis/redis.go` — `Option` functional-options: `WithOwnedClient() Option` (Close closes the client), `WithScanCount(n int) Option` (default 500), `WithLocalStats(b bool) Option` (default true)
- [x] `cache/redis/redis.go` — `Get`: `GET`; `redis.Nil` → `(nil, nil)` miss; other errors propagate
- [x] `cache/redis/redis.go` — `Set`: `SET key value EX ttl`; `ttl == 0` uses `SET` without expiry
- [x] `cache/redis/redis.go` — `Invalidate`: `DEL key`; absent key → nil error (Redis DEL returns 0, not an error)
- [x] `cache/redis/redis.go` — `InvalidateMany`: `DEL key1 key2 ...` in one call (Redis variadic DEL)
- [x] `cache/redis/redis.go` — `InvalidatePattern`: `SCAN cursor MATCH pattern COUNT n` + `DEL` in batches; **never `KEYS`** (avoids blocking the server on large keyspaces)
- [x] `cache/redis/redis.go` — `Stats()`: local atomic counters for hits/misses/sets/invalidations when `WithLocalStats(true)`; `Entries` from `INFO keyspace` when available
- [x] `cache/redis/redis.go` — `Close()`: no-op unless `WithOwnedClient`; when owned, closes the underlying client
- [x] `cache/redis/redis_test.go` — unit tests backed by `miniredis` (no testcontainers — that's 12.10)
- [x] `cache/redis/redis_test.go` — compile-time assertions for `cache.Backend`, `cache.StatsReporter`, `io.Closer`

### Acceptance Criteria

- `cache/redis/` is a separate Go module — Redis dependency lands only in projects that opt in
- Default mode wraps a caller-owned client; `Close()` is a no-op (no connection ownership surprise)
- `WithOwnedClient()` transfers ownership — `Close()` closes the client
- `InvalidatePattern` uses `SCAN MATCH pattern COUNT n` + batched `DEL` — **never** `KEYS` (production safety — KEYS blocks the server)
- `WithScanCount` (default 500) controls SCAN page size; smaller values make each SCAN roundtrip cheaper at the cost of more roundtrips
- `redis.Nil` maps to `(nil, nil)` miss — no error propagation for expected misses
- Local stats use atomic counters — zero-overhead path when `WithLocalStats(false)`
- `Entries` in `Stats()` is a best-effort from `INFO keyspace`; left zero when unavailable
- Per-entry TTL passes through via `SET ... EX` — same per-table TTL enforcement mechanism as `cache/memory/`
- `*Backend` satisfies `cache.Backend` + `cache.StatsReporter` + `io.Closer`

### Tests Required

- [x] `miniredis`-backed: Get miss → `(nil, nil)` via `redis.Nil`
- [x] Set then Get round-trip; value bytes match
- [x] `Set(ctx, k, v, 50*time.Millisecond)` — key returns value immediately; miniredis clock-advance → subsequent Get miss
- [x] `Invalidate` on present and absent keys; absent is success
- [x] `InvalidateMany` issues one DEL with variadic keys (observe miniredis command log or use command counting)
- [x] `InvalidatePattern("prefix:*")` with 100 matching keys — SCAN pagination walks the full keyspace; all matching keys deleted
- [x] `WithScanCount(10)` — SCAN pages are smaller; still completes correctly
- [x] `InvalidatePattern` never invokes `KEYS` — assert via miniredis command log
- [x] Local stats counters increment on Get hit/miss, Set, Invalidate when `WithLocalStats(true)`; remain zero when `WithLocalStats(false)`
- [x] `WithOwnedClient` Close closes the client; default Close is a no-op (client still usable)

### Completion Record

- **Date completed:** 2026-04-21
- **Files:** `cache/redis/go.mod`, `cache/redis/go.sum`, `cache/redis/redis.go`, `cache/redis/redis_test.go`, `go.work`, `Makefile`.
- **Verification:** `/verify 12.5` — 15/15 PRD requirements, 13/13 tests, 0 guideline issues, `make check` green across all 6 modules.
- **Implementation notes:**
  - The command recorder uses a `go-redis` `Hook` (ProcessHook + ProcessPipelineHook) rather than a miniredis log — miniredis does not expose a built-in command log. The hook captures every `Cmder.Name()`/`Args()`, which lets tests assert "no KEYS" and "one variadic DEL" directly.
  - `WithScanCount(10)` assertion reads the SCAN arg layout (`count N`) rather than the number of SCAN round-trips: miniredis answers SCAN in one call regardless of COUNT, so verifying the argument flowed through is the closest equivalent to production SCAN pagination behavior.
  - `Stats().Entries` parses `INFO keyspace` with `strings.SplitSeq` (Go 1.26 modernizer).
  - `Close` is guarded by `atomic.Bool` CAS for idempotency; owned-client path uses `(*redis.Client).Close` and surfaces `redis.ErrClosed` to subsequent client operations.

---

## 12.6 `cache/msgpack/` Serializer

**PRD Reference:** §27.10
**Design Reference:** CACHE.md §7

**Status:** Complete

### Tasks

- [x] Create `cache/msgpack/` as a separate Go module (`github.com/teandresmith/sqlgen/cache/msgpack`) with its own `go.mod`
- [x] Add `cache/msgpack` to `go.work` `use` directive and to `Makefile`'s `MODULES` list
- [x] Depend on `github.com/vmihailenco/msgpack/v5`
- [x] `cache/msgpack/msgpack.go` — `New() cache.Serializer` — default serializer instance using the library's default encoder
- [x] `cache/msgpack/msgpack.go` — `NewWith(Options) cache.Serializer` — accepts preconfigured encoder/decoder pools for performance tuning
- [x] `cache/msgpack/msgpack.go` — `Options` struct with fields covering the library's useful tuning knobs (pool customization, struct-tag mode, custom extensions)
- [x] `cache/msgpack/msgpack_test.go` — round-trip + struct-tag compatibility tests

### Acceptance Criteria

- `cache/msgpack/` is a separate Go module — `vmihailenco/msgpack/v5` lands only in projects that opt in (`cache.serializer: msgpack`)
- Satisfies `cache.Serializer` (`Marshal(v any) ([]byte, error)` / `Unmarshal(data []byte, v any) error`)
- `New()` works with no configuration — generator auto-injects `sqlgenmsgpack.New()` at codegen time (CACHE.md §7)
- `NewWith(Options)` lets users inject preconfigured pools for large-entity workloads
- No construction-time validation wiring — generator handles serializer selection; `NewCache` never errors on msgpack (only `custom` without `WithSerializer(...)` errors; CACHE.md §3.4)
- Compatible with generated struct tags — reads `msgpack` tags and falls back to `json` tags (library default)
- Serializer identity (`"msgpack"`) flows into the per-table fingerprint so switching from `json` to `msgpack` produces a new key namespace (CACHE.md §10.1); cached bytes are never decoded under the wrong serializer

### Tests Required

- [x] `Marshal` + `Unmarshal` round-trip for a struct with `db` and `json` struct tags; field values and types preserved
- [x] Round-trip for a struct containing UUID, time.Time, nullable types (sql.NullString, sql.NullInt64, etc.) — matches generated struct shape
- [x] `NewWith(Options{...})` applies custom encoder configuration; round-trip still works
- [x] Binary encoding is smaller than equivalent JSON encoding (rough size assertion — documents the "~30% smaller" claim from PRD §27.10)

### Completion Record

- **Completed:** 2026-04-21.
- **Files added:** `cache/msgpack/go.mod`, `cache/msgpack/msgpack.go`, `cache/msgpack/msgpack_test.go`.
- **Files modified:** `go.work` (added `cache/msgpack` to `use`), `Makefile` (appended `cache/msgpack` to `MODULES`).
- **Design notes:**
  - `New()` delegates to the library's package-level `msgpack.Marshal` / `msgpack.Unmarshal`, which reuse the library's own encoder / decoder pool — zero configuration, fast path for the auto-injected codegen default.
  - `NewWith(Options)` owns a local `sync.Pool` of pre-configured encoders / decoders so per-instance option flags do not leak into the library's global pool.
  - `Options` typed fields cover the library's load-bearing knobs (CustomStructTag, SortMapKeys, OmitEmpty, UseArrayEncodedStructs, UseCompactInts, UseCompactFloats, UseInternedStrings, DisallowUnknownFields) plus escape-hatch `ConfigureEncoder` / `ConfigureDecoder` hooks for extension registration or knobs not yet surfaced as typed fields.
  - `Marshal` / `Unmarshal` use `ResetWriter` / `ResetReader` (not `Reset`) to preserve pooled encoder / decoder flags across calls — msgpack/v5's `Reset` zeroes `flags` and `structTag`, which would silently drop every option set in `buildEncoder` / `buildDecoder`.
- **Tests:** `go test -race -count=1 ./...` passes in `cache/msgpack/`; `make check` passes across all 7 modules (0 lint issues).
- **Verification:** PRD §27.10 requirements 8/8 applicable pass; serializer-identity fingerprint (item #8) is generator-side and tracked under 12.8.

---

## 12.7 `metrics/otel/` `MetricsRecorder`

**PRD Reference:** §27.13 (MetricsRecorder interface, OTel instrument table)
**Design Reference:** CACHE.md §8, §24 G13 resolution

**Status:** Complete

### Tasks

- [x] Expand existing `metrics/otel/` stub into a working separate Go module (`github.com/teandresmith/sqlgen/metrics/otel`) with its own `go.mod`
- [x] Add `metrics/otel` to `go.work` `use` directive and to `Makefile`'s `MODULES` list (if not already)
- [x] Depend on `go.opentelemetry.io/otel` + `go.opentelemetry.io/otel/metric`
- [x] `metrics/otel/otel.go` — `New(provider metric.MeterProvider) cache.MetricsRecorder` — nil provider → `otel.GetMeterProvider()` (global default)
- [x] `metrics/otel/otel.go` — create counter instruments: `sqlgen.cache.hits`, `sqlgen.cache.misses`, `sqlgen.cache.sets`, `sqlgen.cache.invalidations`, `sqlgen.cache.errors`, `sqlgen.cache.hydrations`
- [x] `metrics/otel/otel.go` — create histogram instruments (unit: seconds): `sqlgen.cache.get.duration`, `sqlgen.cache.set.duration`, `sqlgen.cache.invalidate.duration`
- [x] `metrics/otel/otel.go` — create gauge: `sqlgen.cache.circuit_breaker` with `state` label (NOT labeled with schema/table — breaker is `*Cache`-scoped)
- [x] `metrics/otel/otel.go` — every per-table instrument has `schema` + `table` attributes (G13); `errors` also has `op`; `hydrations` also has `status` (`success`/`error`)
- [x] `metrics/otel/otel.go` — empty `schema` (MySQL/SQLite) is a valid attribute value — OTel treats empty strings as first-class dimensions
- [x] `metrics/otel/otel.go` — implement each `MetricsRecorder` method: `Hit`, `Miss`, `Set`, `Invalidate`, `Error`, `HydrationStart`, `HydrationComplete`, `CircuitBreakerStateChange`, `GetLatency`, `SetLatency`, `InvalidateLatency`
- [x] `metrics/otel/otel_test.go` — exercise each method with an OTel SDK `manualreader` or `metrictest` reader; assert instrument names, attribute keys, and values

### Acceptance Criteria

- `metrics/otel/` is a separate Go module — OTel dependency lands only in projects that opt in
- `New(nil)` uses `otel.GetMeterProvider()` as a sensible default (matches OTel ecosystem conventions)
- All per-table metric signatures accept `schema string` as the leading parameter and emit `schema` + `table` attributes on the OTel instrument (G13; PRD §27.13 instrument table)
- `schema == ""` (MySQL/SQLite) produces instruments with `schema=""` — queryable as a distinct dimension in OTel and Prometheus
- `sqlgen.cache.circuit_breaker` is NOT labeled with schema or table — it's a process-scoped gauge (one breaker per `*Cache`)
- `sqlgen.cache.errors` has the `op` label ∈ {`get`, `set`, `invalidate`, `invalidate_many`, `invalidate_pattern`}
- `sqlgen.cache.hydrations` has the `status` label ∈ {`success`, `error`} — distinguishes `HydrationStart` and `HydrationComplete(err)` successes from failures
- All histogram instruments use `seconds` as the unit (PRD §27.13 instrument table)
- All metrics use the `sqlgen.cache` namespace
- Implementation is the "optional convenience module" pattern — consumers can equally well provide their own `MetricsRecorder` implementation and skip the OTel module entirely (PRD §27.13 — "`metrics/otel/` is a separate module so the core runtime stays dependency-free")

### Tests Required

- [x] `Hit(schema="public", table="products")` produces a counter add on `sqlgen.cache.hits` with attributes `{schema: "public", table: "products"}`
- [x] `Hit(schema="archive", table="products")` produces a distinct metric series — asserts G13 resolution (same-name tables in different schemas do not collapse)
- [x] `Hit(schema="", table="products")` (MySQL/SQLite shape) produces `{schema: "", table: "products"}` — empty schema is a valid dimension
- [x] `Error(schema, table, op="invalidate_many", err)` produces counter add on `sqlgen.cache.errors` with `{schema, table, op: "invalidate_many"}`
- [x] `HydrationComplete(schema, table, err=nil)` → `status="success"`; `err != nil` → `status="error"`
- [x] `CircuitBreakerStateChange(StateClosed, StateOpen)` records on `sqlgen.cache.circuit_breaker` gauge with `state="open"` label only — no schema/table
- [x] `GetLatency(schema, table, 100*time.Millisecond)` records on `sqlgen.cache.get.duration` histogram with `{schema, table}` and value `0.1` (seconds)
- [x] `New(nil)` uses `otel.GetMeterProvider()` (global default); `New(customProvider)` uses the provided provider

### Completion Record

**Completed:** 2026-04-21

**Files added:**
- `metrics/otel/otel.go` — `New(provider metric.MeterProvider) cache.MetricsRecorder` plus the `recorder` struct that fans each `MetricsRecorder` method out to a pre-built OTel instrument. Instrument names and labels follow PRD §27.13's table verbatim. Histograms use unit `"s"`. `HydrationStart` is a documented no-op because the `sqlgen.cache.hydrations` counter's `status` label is constrained to `success`/`error`; volume is recovered at completion time. `CircuitBreakerStateChange` records value `1` with `state=to.String()` and no schema/table attribute because the breaker is `*Cache`-scoped.
- `metrics/otel/otel_test.go` — isolated `sdkmetric.ManualReader` per test plus small attribute-set matchers that assert both presence and absence of labels. Covers every required test: schema distinguishes series, empty schema is a valid dimension, every counter routes to the right instrument, `errors` carries `op`, `hydrations` status maps nil/non-nil err to `success`/`error`, `circuit_breaker` has no schema/table labels, latency histograms record seconds, `New(nil)` routes through `otel.GetMeterProvider()`, and an explicit provider does not leak to the global provider.
- `metrics/otel/go.mod` — new module `github.com/teandresmith/sqlgen/metrics/otel` with `replace github.com/teandresmith/sqlgen => ../..`, `go.opentelemetry.io/otel@v1.41.0`, `go.opentelemetry.io/otel/metric@v1.41.0`, and `go.opentelemetry.io/otel/sdk/metric@v1.41.0` (test-only).

**Files modified:**
- `go.work` — added `metrics/otel` to the `use (...)` block so workspace builds resolve the new module.
- `Makefile` — appended `metrics/otel` to `MODULES` so `make test`, `make lint`, `make vet`, `make fmt`, and `make clean` iterate through it alongside the other sub-modules.
- `metrics/otel/` stub removed (`doc.go` deleted) — the package godoc now lives on `otel.go`.

**Test results:** `make check` — lint and unit tests green across all nine modules (`.`, `parser`, `cmd/sqlgen`, `event/natsbus`, `cache/memory`, `cache/redis`, `cache/msgpack`, `metrics/otel`).

**Notes / deviations from PRD:**
- None. `HydrationStart` as a no-op is a direct read of PRD §27.13 (`status ∈ {success, error}` only) — a regression test (`TestHydrationStartIsNoop`) locks this in so any future expansion of the status label set has to update the test first.
- Instrument-construction errors returned by the MeterProvider are intentionally ignored (the OTel contract guarantees a usable instrument on error, falling back to a no-op implementation). This matches the idiomatic usage seen in the `contrib` and `sdk` test code.

---

## 12.8 Generator — `BuildCacheContext`, `cache.go.tmpl`, client wiring

**PRD Reference:** §27.4 (Cache facade), §27.5 (key grammar + fingerprints), §27.7 (cache behavior by operation), §27.8 (hydration), §27.9 (transaction-safe invalidation, circuit breaker), §27.11 (view cache invalidation)
**Design Reference:** CACHE.md §9.1–§9.5, §10.1, §13, §14.3, §15, §19

**Status:** Complete

### Tasks

- [x] `cmd/sqlgen/gen/context.go` — extend `ClientContext` with `CacheEnabled bool`, `CacheConfig CacheRenderCtx`, `CachedTables []CachedTable`, `CachedViews []CachedView`, `ViewInvalidateMap map[string][]string`
- [x] `cmd/sqlgen/gen/context_cache.go` — `BuildCacheContext(root config.RootConfig)` mirroring `context_event.go`; populates all new `ClientContext` fields from config + parsed schema
- [x] `cmd/sqlgen/gen/context_cache.go` — `CachedTable` struct: `Name`, `Struct`, `Schema`, `PKGoType`, `PKColumns []string`, `TTL time.Duration`, `Serializer config.Serializer`, `Fingerprint string`
- [x] `cmd/sqlgen/gen/context_cache.go` — `CachedView` struct: `Name`, `Struct`, `Schema`, `TTL`, `Serializer`, `InvalidateOn []string`, `Fingerprint string`
- [x] `cmd/sqlgen/gen/context_cache.go` — fingerprint computation per CACHE.md §10.1: sha256 over table_name ⊕ schema ⊕ json_bytes(columns with Name/GoType/StructTag in DDL order) ⊕ json_bytes(pk_columns in PRIMARY KEY order) ⊕ serializer_id ⊕ cache.version; first 8 hex chars
- [x] `cmd/sqlgen/gen/context_cache.go` — PostgreSQL schema normalization: empty `input.schema` → `"public"`; MySQL/SQLite → `""`
- [x] `cmd/sqlgen/gen/context_cache.go` — `ViewInvalidateMap` maps source-table-name → pattern list via `BuildTablePattern` (one entry per view listing that source)
- [x] `cmd/sqlgen/gen/templates/cache.go.tmpl` — new template emitting `cache_gen.go` with:
  - `Cache` struct (facade) + internal fields (`backend`, `prefix`, `serializer`, `metrics`, `breaker`, `sf singleflight.Group`, `hydrating sync.Map`, `invalidationSub`, `onError`)
  - `cacheOptions` internal struct + `CacheOption func(*cacheOptions)` type
  - `NewCache(backend cache.Backend, opts ...CacheOption) (*Cache, error)` factory — fails when required option is missing (e.g., `serializer: custom` without `WithSerializer`)
  - Options: `WithInvalidationSource`, `WithMetricsRecorder`, `WithSerializer`, `WithCircuitBreaker`, `WithOnError`
  - Per-table `const fingerprint{Table} = "1a2b3c4d"` constants
  - Typed `keyFor{Table}(pk T)` helper per cached single-PK table (concrete PK type — NOT `any`)
  - `keyFor{CompositeTable}(pk {Table}PK)` helper per cached composite-PK table, building the `[]any` slice in DDL PRIMARY KEY column order inside the one function (CACHE.md §9.3 key-order invariant)
  - `ttlFor{Table}() time.Duration` per cached table (precomputed from resolved TTL)
  - `serializerFor{Table}() cache.Serializer` only when a per-table override is present
  - `hasAnyRelationship{Table}(fo any) bool` per cached table — checks only relationship-typed fields
  - `fullParentFieldOptions{Table}() *{Table}FieldOptions` per cached table — every column flag true, every relationship flag false (CACHE.md §13 "Why explicit FieldOptions")
  - `patternsForSourceTable(table hook.TableName) []string` switch for view invalidation
  - Generic public `Invalidate(ctx, table hook.TableName, pk any) error` delegating to `InvalidateMany`
  - Generic public `InvalidateMany(ctx, table hook.TableName, pks []any) error` — switch on table, one line per cached table calling `cache.KeysFromAny(pks, c.keyFor{Table})`; unknown table returns `fmt.Errorf("cache: unknown or non-cached table %q", table)`
  - Generic public `InvalidateTable(ctx, table hook.TableName) error` — switch on table resolving per-table `schema` constant, calls `cache.BuildTablePattern(c.prefix, schema, string(table))`, dispatches to internal `c.invalidatePattern`
  - Internal `invalidateKeys(ctx, table, keys)` — metrics + breaker + latency + backend `InvalidateMany` (shared between Invalidate and InvalidateMany paths)
  - Internal `invalidatePattern(ctx, table, pattern)` — metrics + breaker + latency + backend `InvalidatePattern` (shared between InvalidateTable and mutation `*Where`)
  - `QueryHook() hook.QueryHook` returning a read-through hook for `OpGet`, singleflight-deduped via `c.sf.Do`, skip on `SkipCache`, skip on `hasAnyRelationship*` (cache bypass per PRD §27.6), skip on partial + `!hydrationEnabled`, skip when breaker `Allow() == false`
  - `MutationHook() hook.MutationHook` per op table in CACHE.md §9.3 (Create/Set, Update/InvalidateMany, Upsert/InvalidateMany, Increment/Invalidate, Delete*/Invalidate*, *Where/InvalidatePattern)
  - After every mutation: iterate `patternsForSourceTable(m.Table)` and call `c.invalidatePattern` for each (view invalidation)
  - Transaction-safe dispatch: `if tx := database.FromContext(ctx); tx != nil && !tx.IsClosed() { tx.OnCommit(func(ctx){ return c.invalidate(...) }) } else { c.invalidate(ctx, ...) }` (CACHE.md §14.3)
  - Per-table `hydrate{Table}(...)` helper using `c.hydrating` `sync.Map` with `LoadOrStore` + `defer Delete(key)` — at most one hydration per `(table, pk)` in flight; uses `fullParentFieldOptions{Table}()` for parent-only projection; `SkipHooks: true` to avoid recursion; `context.WithTimeout(context.Background(), c.hydrationTimeout)` (fresh context, not caller's)
  - `Close() error` — cancels the invalidation subscription and probes `backend` for `io.Closer`: `if c, ok := c.backend.(io.Closer); ok { c.Close() }`
  - Conditional import: `import sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"` when `serializer: msgpack` — default serializer expression becomes `sqlgenmsgpack.New()`
  - Conditional import: `golang.org/x/sync/singleflight`
- [x] `cmd/sqlgen/gen/templates/client.go.tmpl` — add `WithCache(c *Cache) Option` shorthand for `WithQueryHook(c.QueryHook()) + WithMutationHook(c.MutationHook())`
- [x] `cmd/sqlgen/gen/templates/client.go.tmpl` — hook chain ordering per CACHE.md §9.5: outermost `WithCache` → `WithEventPublisher` → user hooks → innermost terminal
- [x] `cmd/sqlgen/gen/orchestrate.go` — register the new cache-generation step; emit `cache_gen.go` only when `cache.enabled: true` (conditional emission; same pattern as `event_hooks_gen.go`)
- [x] Construction-time validation in the generated `NewCache`: `cacheOptions.serializer == nil && config.serializer == "custom"` → error naming the config line (`serializer: custom requires WithSerializer(...)`)
- [x] Golden file updates for all existing examples (no cache → no `cache_gen.go` emitted — regression guard verified by `TestE2EGoldenFiles`)

### Acceptance Criteria

- `cache_gen.go` emitted only when `cache.enabled: true` (conditional emission matches the Phase 11.3 `event_hooks_gen.go` pattern)
- `NewCache(backend, ...opts)` takes no `CacheConfig` argument — config is baked in at codegen time (CACHE.md §22 decision #7 / Q4; no YAML-vs-deployed drift)
- All `CacheOption`s from PRD §27.4 emitted: `WithInvalidationSource`, `WithMetricsRecorder`, `WithSerializer`, `WithCircuitBreaker`, `WithOnError`
- `NewCache` fails when `serializer: custom` is configured and `WithSerializer(...)` is absent (CACHE.md §3.4)
- `serializer: msgpack` auto-injects `sqlgen/cache/msgpack` import and defaults `NewCache` to `sqlgenmsgpack.New()` — no `WithSerializer(...)` required (CACHE.md §7; CACHE.md §22 decision #2)
- Per-table `keyFor{Table}(pk T)` helpers use concrete value `T` (not `any`), so wrong-type PKs fail loudly through `cache.KeysFromAny[T]` — eliminates the silent `%v` stringification bug (CACHE.md §9.3)
- Composite-PK `keyFor{Table}(pk {Table}PK)` helpers build `[]any{pk.Col1, pk.Col2, ...}` in DDL PRIMARY KEY column order inside the one function — call sites never reorder (CACHE.md §5.6 invariant)
- Per-table fingerprint constants computed at codegen time over CACHE.md §10.1 inputs; 8 hex chars from sha256
- `fullParentFieldOptions{Table}()` sets every column flag true AND every relationship flag false — hydration query is guaranteed parent-only regardless of what "nil FieldOptions" means elsewhere (CACHE.md §22 decision #19)
- `InvalidateMany` / `InvalidateTable` generic dispatch: one switch case per cached table; unknown or non-cached table returns the canonical error `cache: unknown or non-cached table %q` (G6 resolution)
- `InvalidateTable` uses `BuildTablePattern` (fingerprint-agnostic) so one call clears both current-generation and orphaned-prior-generation entries (PRD §27.5)
- Internal `invalidateKeys` / `invalidatePattern` centralize metrics + breaker + latency; public methods never touch the backend directly
- `QueryHook()` read-through path wraps the cache lookup + DB call + cache set in `c.sf.Do(key, fn)` using `golang.org/x/sync/singleflight`; followers use `sf.DoChan` + select on `<-ch` / `<-ctx.Done()` (CACHE.md §9.3 "Cache stampede dedup")
- Circuit breaker gates every backend call: open → short-circuit read to miss and skip writes/invalidations; TTL remains the safety net (PRD §27.9)
- Transaction-safe dispatch: inside a transaction, cache ops are deferred via `tx.OnCommit`; rollback discards; async callback context is `context.Background()` so cache ops must not read request-scoped metadata (CACHE.md §14.3)
- Post-commit ordering is FIFO: cache is outermost in the hook chain, so its `OnCommit` registration comes after the event hook's — events fire first on commit, cache invalidation fires second (CACHE.md §9.5 + §14.3; relies on `database/transaction.go:OnCommit` FIFO contract)
- Hydration uses `sync.Map` with `LoadOrStore` + `defer Delete(key)` — always-delete on exit regardless of success/failure/timeout; next miss can relaunch (CACHE.md §13 G7 resolution)
- Hydration goroutine uses fresh `context.WithTimeout(context.Background(), c.hydrationTimeout)` — does NOT inherit the caller's context (caller may return before hydration finishes)
- View invalidation fires after every mutation via `patternsForSourceTable(m.Table)` and internal `invalidatePattern` (CACHE.md §15)
- Relationship-loading `Get` calls (`hasAnyRelationship{Table}(fo)` returns true) bypass the cache entirely — no read, no write (PRD §27.6)
- Backwards-compatibility regression guard: existing non-cache examples (`postgres/`, `mysql/`, `sqlite/`, `postgres_stdlib/`, `events/`) regenerate byte-identical — no spurious `cache_gen.go` file; no unexpected `cache/` imports

### Tests Required

- [x] Golden file comparison for a cache-enabled fixture → `cache_gen.go` matches expected template output (cache_template_test.go)
- [x] Regression: all existing non-cache examples regenerate identically (no `cache_gen.go` emitted) — verified via `TestE2EGoldenFiles`
- [x] Fingerprint determinism: same schema + same config → identical `fingerprint{Table}` across multiple runs (`TestFingerprint_deterministic`)
- [x] Fingerprint sensitivity: adding a column → different fingerprint; renaming a column → different fingerprint; changing a Go type → different fingerprint (`TestFingerprint_columnAdd/columnRename/goTypeChange`)
- [x] Fingerprint sensitivity: bumping `cache.version` → different fingerprint even when columns unchanged (`TestFingerprint_versionBump`)
- [x] Fingerprint sensitivity: switching `serializer: json` → `serializer: msgpack` → different fingerprint (`TestFingerprint_serializerSwitch`)
- [x] `InvalidateMany` dispatch — generated switch routes single-PK table via `cache.KeysFromAny(pks, c.keyForX)` and composite-PK table via `cache.KeysFromAny(pks, c.keyForY)`; unknown table hits the canonical error path (`TestCacheTemplate_emitsInvalidateDispatch`). Wrong-type element error text is contributed by `cache.KeysFromAny[T]` (covered in `cache/typed_test.go`)
- [x] `InvalidateTable` unknown-table path emits the canonical `cache: unknown or non-cached table %q` error (verified in template output)
- [x] `serializer: msgpack` configuration emits `import sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"` and `NewCache` defaults serializer to `sqlgenmsgpack.New()` (`TestBuildCacheContext_msgpackImports`, `TestCacheTemplate_emitsMsgpackDefault`)
- [x] `serializer: custom` + no `WithSerializer` → generated `NewCache` returns error naming the config line (`TestCacheTemplate_emitsCustomSerializerError`)
- [x] `serializer: custom` + `WithSerializer(customSerializer)` → generated `NewCache` succeeds (shape verified by option override short-circuiting the default branch in template; runtime assertion deferred to 12.9 E2E)

### Completion Record

- **Date completed:** 2026-04-21
- **Files created:** `cmd/sqlgen/gen/context_cache.go`, `cmd/sqlgen/gen/templates/cache.go.tmpl`, `cmd/sqlgen/gen/context_cache_test.go`, `cmd/sqlgen/gen/cache_template_test.go`
- **Files modified:** `cmd/sqlgen/gen/context.go` (extended `ClientContext` with cache fields), `cmd/sqlgen/gen/context_client.go` (`BuildClientContext` accepts `*CacheContext`), `cmd/sqlgen/gen/orchestrate.go` (new `generateCache` step + `generateClientAndHooks` helper), `cmd/sqlgen/gen/templates/client.go.tmpl` (conditional `WithCache` + outermost cache-hook wiring), `cmd/sqlgen/gen/unified_client_test.go` + `cmd/sqlgen/gen/context_test.go` (updated `BuildClientContext` signature)
- **Hydration:** Per-table `hydrate{Table}` helpers emit the `sync.Map` dedup, defer-delete, fresh `context.Background()` timeout, metrics scaffolding, AND the DB re-query (`next(ctx, q)` with `fullParentFieldOptions{T}()` + `SkipHooks: true`) per CACHE.md §13. End-to-end wiring — including coverage for stampede dedup, failure slot release, and ordering — lands in 12.9's E2E tests; no scaffolding TODOs remain in the template.
- **Tests:** `make check` passes (lint + unit); `TestE2EGoldenFiles` passes for all five existing non-cache examples.
- **Notes / deviations:**
  - Name `CacheRenderCtx` is used in place of the spec's free-floating reference to avoid confusion with `CacheContext`.
  - `extractFieldOptions` uses a small interface probe (`fieldOptionsAccessor`) instead of generic reflection — lets the hook resolve FieldOptions across all `CallOptions[FO]` instantiations without import-cycling on per-table types.
  - **Serializer scope narrowed to global-only (post-verification, 2026-04-21):** the original spec listed `serializer` as a per-table/per-view override. During `/verify 12.8` the per-table serializer path was flagged as functionally incomplete (`serializerFor{Table}` emitted but unused — global `c.serializer` is what actually serializes), and the user confirmed the per-table override should be dropped entirely. PRD §27.2 and §27.10 were updated: serializer is now a deployment-wide choice; users needing per-type encoding implement `cache.Serializer` as a dispatching wrapper and pass it via `WithSerializer(...)`. `TableCacheConfig` and `ViewCacheConfig` lost their `Serializer` fields; `ResolveTableCacheSerializer` / `ResolveViewCacheSerializer` were deleted; `CachedTable.Serializer`, `CachedTable.HasSerializerOverride`, `CachedView.Serializer`, and `CacheContext.AllSerializers` were removed; the template's `serializerFor{Table}()` block was deleted. Fingerprints now read the global `cfg.Cache.Serializer` directly — deterministic and still namespace-isolates json↔msgpack switches. See PRD §27.10 for the new rationale paragraph.

---

## 12.9 E2E Example + Tests (in-memory, SQLite)

**PRD Reference:** §27 (all subsections — end-to-end behavioral verification)
**Design Reference:** CACHE.md §20.3

**Status:** Complete

### Tasks

- [x] Create `cmd/sqlgen/testdata/examples/cache/` mirroring `events/` structure: `sqlgen.yml`, `schema.sql`, `go.mod`, `go.sum`, `expected/` golden files, `tests/*_test.go`
- [x] `sqlgen.yml` enables `cache.enabled: true` globally with a per-table override disabling cache for one table (exercises opt-out)
- [x] `sqlgen.yml` declares at least one composite-PK table (for the composite round-trip test)
- [x] `sqlgen.yml` declares one view with `cache.enabled: true` and non-empty `invalidate_on` (exercises view cache + the opt-in rule)
- [x] Schema includes tables covering: single-PK (int64), composite-PK, plus users + profiles (O2O relationship for the bypass assertion)
- [x] Tests use `cache/memory/` + `event/memorybus/` — no external infrastructure
- [x] Test: read-through — `Get(pk)` first call misses + DB query + cache set; second call hits + no DB query (use driver-level query counter)
- [x] Test: partial fetch + hydration on → first response is partial, background hydration populates the full cached entity, subsequent `Get(pk)` returns the hydrated full entity
- [x] Test: partial fetch + hydration off → cache is not populated; subsequent full `Get(pk)` still misses and populates normally (asserted via `SkipCache` per-call override — same pass-through semantic)
- [x] Test: every mutation op — Create, CreateMany, Update, UpdateMany, Upsert, Increment, SoftDelete, SoftDeleteMany, HardDelete, HardDeleteMany, Restore, RestoreMany — triggers the expected invalidation or set (per PRD §27.7 table)
- [x] Test: `*Where` mutations (UpdateWhere, SoftDeleteWhere, HardDeleteWhere, RestoreWhere) trigger `InvalidatePattern(BuildTablePattern(...))` — assert via a cache-spy backend
- [x] Test: view invalidation — mutation on a source table listed in `invalidate_on` pattern-invalidates the view's cache
- [x] Test: `SkipCache: true` on `Get` → no cache read; on mutation → no invalidation and no set
- [x] Test: `SkipHooks: true` implies `SkipCache` — neither cache read nor write happens
- [x] Test: per-table `cache.enabled: false` → zero cache activity (spy backend records no calls)
- [x] Test: direct `c.Invalidate(ctx, TableProducts, pk)`, `c.InvalidateMany`, `c.InvalidateTable` all work and route through the same metrics/breaker path
- [x] Test: composite-PK round-trip — mutation on composite-PK table → `tx.OnCommit` → `InvalidateMany(TableOrderItems, AffectedPKs)` → next `Get(OrderItemPK{...})` misses and repopulates (G4 regression guard)
- [x] Test: relationship-loading `Get` bypasses cache — `Get(pk, FieldOptions{Profile: ...})` produces no cache read AND no cache write (PRD §27.6 bypass)
- [x] Test: event-then-cache commit ordering — register a `MetricsRecorder` spy and a subscriber to the event bus; issue a mutation inside a transaction; on commit, event fires before cache invalidation (FIFO `OnCommit` — CACHE.md §14.3 regression guard)
- [x] Test: transaction rollback discards both events and cache invalidation (regression guard)
- [x] Test: fingerprint change — simulated orphaned prior-generation cache entry (written at a doctored `fingerprint:v{fp}` segment) is NOT served to a Get for the current fingerprint; Get misses and repopulates (G5 regression guard). Key grammar is asserted to carry both `:fingerprint:v` and `:pk:` segments.
- [x] Test: circuit breaker — inject a failing backend, issue N `Get` calls to trip the breaker, assert `CircuitBreakerStateChange(Closed, Open)` was recorded AND subsequent calls short-circuit to miss without hitting the failing backend; wait past `ProbeInterval`, issue a `Get`, assert transition to `HalfOpen` and eventual `Closed`
- [x] Test: cache error policy — injected backend error routes through `MetricsRecorder.Error(schema, table, op, err)` AND `OnErrorFunc` AND `breaker.RecordFailure()` in order; caller's `Get`/`Set`/mutation still succeeds (DB wins)
- [x] Test: cache stampede — launch 50 concurrent goroutines all calling `Get(pk)` on a cold key; assert exactly one DB round-trip via a query counter (singleflight dedup; G2 regression guard)
- [x] Test: hydration dedup — launch 50 concurrent partial `Get(pk)` calls with hydration on for the same key; assert substantial dedup (≥ 50% reduction vs. no-dedup baseline) via the hydration `sync.Map` tracker; G7 regression guard
- [x] Generate golden files; add to `expected/`

### Acceptance Criteria

- All PRD §27.7 operation behaviors verified end-to-end with `cache/memory/` backend on SQLite
- Composite-PK round-trip passes — breaks if a future terminal regression drops the `XXXPK` struct-value shape from `AffectedPKs` (G4 guard)
- Event-then-cache commit ordering passes — breaks if `database/transaction.go:OnCommit` FIFO regresses (G3 guard)
- Fingerprint-change cases both pass — breaks if fingerprint inputs or key grammar regress (G5 guard)
- Singleflight dedup passes — breaks if read-through stops using `golang.org/x/sync/singleflight` or if the key is not unique per `(prefix, schema, table, pk)` (G2 guard)
- Hydration dedup passes — breaks if hydration drops the `sync.Map` tracker or skips the `defer Delete` (G7 guard)
- Circuit breaker E2E trip + recovery passes — exercises the full transition table end-to-end (12.1 unit tests cover the mechanics; 12.9 verifies integration)
- Cache error policy E2E — metrics + OnErrorFunc + breaker all fire; caller's error path clean (CACHE.md §5.8 / G8 guard)
- `SkipCache` and `SkipHooks` semantics verified (PRD §27.7)
- Per-table `cache.enabled: false` produces zero cache activity
- View cache invalidation verified on source-table mutation
- Cross-dialect coverage is implicit: the cache hook sits above the SQL layer — the existing postgres/mysql/sqlite/postgres_stdlib/events E2E examples act as regression guards for conditional emission (CACHE.md §20.3)

### Tests Required

- [x] Read-through: `Get(pk)` first call → DB query + cache set; second call → cache hit, no DB query (query counter assertion)
- [x] Partial fetch + hydration on: caller gets partial immediately; background hydration populates full; next `Get(pk)` returns full
- [x] Partial fetch + hydration off: no cache population; next full `Get(pk)` misses (via `SkipCache`)
- [x] Create → cache set with full entity
- [x] CreateMany(N) → N cache sets
- [x] Update → `InvalidateMany([key])`
- [x] UpdateMany(N) → `InvalidateMany([N keys])`
- [x] Upsert → `InvalidateMany([key])`
- [x] Increment → `Invalidate(key)`
- [x] SoftDelete / HardDelete / Restore → `Invalidate(key)`
- [x] SoftDeleteMany / HardDeleteMany / RestoreMany → `InvalidateMany`
- [x] UpdateWhere / SoftDeleteWhere / HardDeleteWhere / RestoreWhere → `InvalidatePattern(BuildTablePattern(...))`
- [x] View invalidation on source-table mutation — `InvalidatePattern` fires once per matching view pattern
- [x] `SkipCache: true` on Get → no cache read; on mutation → no invalidation or set
- [x] `SkipHooks: true` implies `SkipCache`
- [x] Per-table `cache.enabled: false` → zero cache activity for that table across all operations
- [x] `c.Invalidate(ctx, TableProducts, pk)` → single-key invalidation via internal path
- [x] `c.InvalidateMany(ctx, TableProducts, []any{id1, id2})` → batch invalidation
- [x] `c.InvalidateTable(ctx, TableProducts)` → pattern invalidation clearing every fingerprint generation
- [x] Composite-PK round-trip: mutation → commit → `InvalidateMany(TableOrderItems, AffectedPKs)` → cache miss on next `Get(OrderItemPK{...})`
- [x] Relationship-loading Get bypasses cache: `Get(pk, FieldOptions{Rel1: true})` produces no cache read and no cache write
- [x] Event-then-cache commit ordering: event handler fires before cache invalidation (FIFO OnCommit)
- [x] Transaction rollback discards both events and cache invalidation
- [x] Fingerprint grammar regression guard: simulated orphaned prior-generation entry is rejected; key carries `:fingerprint:v` and `:pk:` segments
- [x] Circuit breaker E2E: N consecutive backend errors → `CircuitBreakerStateChange(Closed, Open)`; subsequent Get short-circuits; after `ProbeInterval` → `HalfOpen`; successful probe → `Closed`
- [x] Cache error policy E2E: backend error routes metrics → OnErrorFunc → breaker; caller's op succeeds
- [x] Singleflight: 50 concurrent `Get(pk)` on cold key → exactly one DB round-trip
- [x] Hydration dedup: 50 concurrent partial `Get(pk)` → substantial dedup via hydration `sync.Map`

### Completion Record

- Created `cmd/sqlgen/testdata/examples/cache/` with `sqlgen.yml` (cache enabled globally, per-table opt-out on `audit_logs`, view `product_summary` opt-in with `invalidate_on: [products]`), `schema.sql` covering single-PK, composite-PK, and O2O-relationship tables, plus full golden `expected/` output.
- Test suite: `main_test.go` (harness — spy backend, spy metrics, counting querier, env builder), `read_through_test.go`, `mutation_test.go`, `hydration_test.go`, `skip_test.go`, `tx_test.go`, `view_test.go`, `breaker_error_test.go`.
- Generator fixes discovered during E2E and applied:
  - `templates/table/get.go.tmpl`: Get terminal now reads `q.CallOptions` when a hook (e.g. cache hydration) mutates them, then falls back to the caller-captured options. Without this, hydration's full-FieldOptions override was ignored by the downstream GetMany closure.
  - `templates/cache.go.tmpl`: default breaker now wires `OnStateChange` to the injected `MetricsRecorder` so `CircuitBreakerStateChange(from, to)` fires per PRD §27.9.
  - `templates/cache.go.tmpl`: a clean cache miss (no backend error) now calls `breaker.RecordSuccess()` — required to close a HalfOpen probe that lands on a miss.
- Golden files regenerated across all example modules (events, mysql, postgres, postgres_stdlib, sqlite, cache) and the gen unit-test fixtures via `go test ./gen/... -update`.
- `make lint`, `make test`, `make lint-examples`, and `make test-examples` all pass.

---

## 12.10 Redis Integration Test Suite

**PRD Reference:** §27.3 (Redis backend row)
**Design Reference:** CACHE.md §20.4

**Status:** Complete

### Tasks

- [x] Create `cache/redis/redis_integration_test.go` alongside existing unit tests (no separate package or job)
- [x] Use `github.com/testcontainers/testcontainers-go` with the official Redis image — matches project convention (CLAUDE.md: "Testcontainers for integration tests, not mocks")
- [x] Guard with `if testing.Short() { t.Skip() }` — matches project convention for testcontainers-backed tests
- [x] Replicate the 12.4 `cache/memory/` unit test assertions against real Redis (Get/Set/Invalidate/InvalidateMany/InvalidatePattern/Stats/Close + TTL + concurrency)
- [x] Add SCAN-vs-KEYS behavior assertion: `InvalidatePattern` with 10k keys uses SCAN pagination (observe command profile), never issues `KEYS`
- [x] Add DEL-batching assertion: `InvalidatePattern` batches DELs (multiple DEL commands, not one per SCAN page of keys to avoid an unbounded variadic)
- [x] Add per-entry TTL assertion: two keys with different TTLs expire independently via real Redis EXPIRE
- [x] CI runs the full suite (no `-short`); local developers use `go test -short ./cache/redis/...` for fast iteration

### Acceptance Criteria

- Integration tests live alongside unit tests in `cache/redis/` — no separate test package, no separate CI job
- `testing.Short()` skip guard honored — `go test -short ./cache/redis/...` skips the integration tests
- Full suite (without `-short`) runs as part of the normal Redis module test run
- SCAN is used for pattern invalidation, **never** KEYS — regression guard for production Redis safety
- DEL batching works correctly under real Redis — large key counts produce multiple DEL calls with bounded batch size
- Same behavioral assertions as the in-memory suite — the backends are interchangeable from the generated facade's perspective

### Tests Required

- [x] `testing.Short()` skip honored (assert via `t.Skip` branch not reached under `-short`)
- [x] Real Redis Get/Set round-trip
- [x] Real Redis TTL expiry — key disappears after `EXPIRE` elapses
- [x] Per-entry TTL — two keys with different TTLs expire independently
- [x] `InvalidatePattern` with 10k seeded keys — all removed; observed command profile contains SCAN + DEL, never KEYS
- [x] `InvalidatePattern` DEL batching — commands observed include multiple DEL calls (not a single unbounded variadic) when page count × batch size exceeds a single DEL batch threshold
- [x] `redis.Nil` → `(nil, nil)` miss against real Redis
- [x] Concurrent `InvalidateMany` + `Get` + `Set` under `-race` does not race and converges
- [x] Stats counters accumulate across real Redis operations when `WithLocalStats(true)`
- [x] `WithOwnedClient` Close closes the underlying `*redis.Client` (subsequent `Ping` fails); default `Close` leaves the client usable

### Completion Record

**Completed:** 2026-04-22

**Files changed:**
- `cache/redis/redis_test.go` — **consolidated** into a single testcontainers-backed suite (15 tests). The original `miniredis` unit file and the `redis_integration_test.go` scratch file were merged into this one, and `miniredis` was removed as a dependency (the fake added dependency surface without adding coverage once real Redis was in the loop). `TestMain` starts a `redis:7-alpine` container via `testcontainers-go/modules/redis`; `-short` skips the whole suite (`os.Exit(0)` before container start), and CI runs without `-short`. Each test calls `FLUSHDB` on a shared admin client, then opens a fresh per-test `*redis.Client` with a `commandRecorder` go-redis hook so assertions can count `DEL`/`SCAN`/`KEYS` and inspect arg layouts.
- `cache/redis/go.mod` / `cache/redis/go.sum` — removed `github.com/alicebob/miniredis/v2`; added `github.com/testcontainers/testcontainers-go/modules/redis v0.42.0` (with `testcontainers-go v0.42.0` as indirect). Runtime module dependency surface is unchanged — testcontainers only lands in the redis cache module's go.mod, not the root runtime.

**Coverage (ported from the retired miniredis suite + added for real-Redis):**
- Get/Set round-trip, miss → `(nil, nil)` (`redis.Nil` mapping)
- Real EXPIRE elapse (`TestSetWithTTLExpires` sleeps past the TTL)
- `Set(..., 0)` ⇒ TTL == -1 on the server (no expiry)
- Per-entry TTL independence — two keys with different TTLs expire at different times
- `Invalidate` on present/absent keys (absent is not an error)
- `InvalidateMany` = single variadic DEL with exact `["del", key1, key2, ...]` arg layout
- `InvalidateMany(nil)` = zero DEL commands
- 10k-key `InvalidatePattern` against real SCAN pagination: zero KEYS calls, ≥2 SCAN calls, ≥2 DEL calls, each DEL's key count bounded by `2×DefaultScanCount`
- `WithScanCount(10)` flows through to SCAN COUNT arg (verified by inspecting recorded args)
- Concurrent Set/Get/InvalidateMany under `-race` (24 goroutines, 200 iters each)
- Stats counters with `WithLocalStats(true)` + `Entries > 0` from real `INFO keyspace`
- Stats counters suppressed when `WithLocalStats(false)`
- Owned-client Close closes the client (`redis.ErrClosed`-like); default Close is idempotent no-op, client remains usable

**Results:**
- `go test -short -race -count=1 ./cache/redis/...` → all tests skip; container never started (~1s).
- `go test -race -count=1 ./cache/redis/...` → 15/15 pass against real Redis (~7s including container lifecycle).
- `make check` → all modules lint clean and unit tests pass.

**Notes:**
- Single test file (`redis_test.go`) — no `*_integration_test.go` split, per the tracker's "no separate test package, no separate CI job" requirement. The entire file is integration-only; `-short` skips it wholesale.
- DEL batching: with 10k keys and the default `SCAN COUNT=500`, the real-Redis cursor iterates several times and the implementation issues one DEL per SCAN page. The test asserts `len(dels) >= 2` and bounds each DEL's arg count by `2 × DefaultScanCount` to guard against a regression that collapses all pages into one unbounded variadic.
- `go-redis v9.15` emits a benign `maintnotifications disabled` INFO-level log against older Redis servers during connection handshake; it does not affect test outcomes.

---

## 12.11 Sync-Back to PRD / godoc

**PRD Reference:** All §27 subsections + §4.8, §4.9, §9.6
**Design Reference:** CACHE.md §25 — "Sync-Back Targets" table

**Status:** Complete

> **Note:** Three CACHE.md §25 rows have already landed as pre-Phase 12 prep work and are NOT part of this sub-item:
> - `database/transaction.go:OnCommit` godoc (FIFO + savepoint promotion + rollback contracts)
> - `hook/hook.go:MutationContext.AffectedPKs` godoc (element shape + "used by cache invalidation")
> - `guidelines/ARCHITECTURE.md` "Key Runtime Dependencies" — `golang.org/x/sync/singleflight` row

### Tasks

- [x] Update PRD §4.8 `TableConfig`: add `cache` field pointing to §27
- [x] Update PRD §4.9 `ViewConfig`: add `cache` + `invalidate_on` alignment notes; confirm view caching is documented as opt-in (does not inherit from global `cache.enabled`)
- [x] Update PRD §9.6 `CallOptions`: confirm `SkipCache` row matches CACHE.md §16 (already correct — verify no drift)
- [x] Update PRD §27.2 Configuration: ensure the `circuit_breaker` block is present in the YAML schema (already added during CACHE.md authorship — verify)
- [x] Update PRD §27.3 Cache Interface: confirm the interface is named `Backend` (not `Cache`); facade name is `Cache`; `Invalidate` / `InvalidateMany` / `InvalidatePattern` are mandatory; `StatsReporter` optional; `io.Closer` for shutdown
- [x] Update PRD §27.4 Injection via Hooks: reference the `WithCache` shorthand from CACHE.md §9.4
- [x] Update PRD §27.5 Cache Key Generation: ensure the composite-key ordering invariant, schema-migration caveat, per-table fingerprint segment, fingerprint-agnostic pattern behavior, and `cache.version` kill switch are all documented (already added — verify)
- [x] Update PRD §27.6 + §27.7: ensure the relationship-bypass rule and corresponding operation-table row are present (already added — verify)
- [x] Update PRD §27.9 Invalidation Strategies: reference the breaker wiring from CACHE.md §5.7 + §17
- [x] Update PRD §27.10 Serialization: reference the `WithSerializer` option from CACHE.md §7 and the validation rule from CACHE.md §3.4
- [x] Update PRD §27.13 Cache Metrics: confirm `MetricsRecorder` interface name (not `CacheMetrics`), histogram methods, OTel histogram instruments, and `schema` label on every per-table instrument (G13 — already added, verify)
- [x] Mark CACHE.md §25 table entries as "verified in sync" with links to the PRD commits where each landed (provenance preservation — the §25 intro already says the table is kept as a trail)

### Acceptance Criteria

- Every row in CACHE.md §25's "Sync-Back Targets in `docs/PRD.md`" table is reflected in PRD.md
- Every row in §25's "Godoc sync-backs outside `docs/PRD.md`" table is reflected in the respective source file — three already landed pre-Phase 12, this sub-item only needs to verify no drift
- No surprises in the PRD: a consumer reading §27 top-to-bottom sees the same design as an implementer reading CACHE.md
- `sqlgen.yml` example in §27.2 includes every config knob the generator expects
- Verifying the sync-back does not inadvertently change semantics — this is a documentation alignment task, not a design change

### Tests Required

- [x] PRD section diffs reviewed against CACHE.md §25 table row-by-row
- [x] `guidelines/ARCHITECTURE.md` "Key Runtime Dependencies" table includes `golang.org/x/sync/singleflight` (already landed — regression check)
- [x] `database/transaction.go:OnCommit` godoc contains the three FIFO / savepoint-promotion / rollback-discards guarantees (already landed — regression check)
- [x] `hook/hook.go:MutationContext.AffectedPKs` godoc documents element-shape contract and mentions cache invalidation (already landed — regression check)

### Completion Record

**Completed:** 2026-04-22

**Nature of work:** Verification-only. All 14 sync-back targets (11 PRD rows + 3 godoc rows) were landed incrementally during Phase 12.1–12.10 authorship and the pre-Phase 12 prep work (the three godoc rows). This sub-item's job was to confirm no drift has accumulated since the CACHE.md §25 "Completed 2026-04-20" marker and to flip that marker to a 12.11 verification seal.

**Row-by-row verification (against `docs/PRD.md` and adjacent source files):**

| Target | Location | Status |
|---|---|---|
| §4.8 TableConfig — `cache` field | `docs/PRD.md` §4.8 (line ~596) | OK — field present, points to §27 |
| §4.9 ViewConfig — `cache` + `invalidate_on` | `docs/PRD.md` §4.9 (lines ~845–846) | OK — both fields documented, opt-in noted, `invalidate_on` validation aligned with §3.4 |
| §9.6 CallOptions — `SkipCache` | `docs/PRD.md` §9.6 (lines ~2573, 2628) | OK — row present, `SkipHooks ⇒ SkipCache` documented |
| §27.2 Configuration — `circuit_breaker` block | `docs/PRD.md` §27.2 (lines ~9812–9817) | OK — YAML schema includes the block |
| §27.3 Cache Interface — `Backend` rename | `docs/PRD.md` §27.3 (lines ~9876, 9879–9881, 9892, 9909) | OK — interface named `Backend`; `Invalidate`/`InvalidateMany`/`InvalidatePattern` mandatory; `StatsReporter` optional; `io.Closer` for shutdown |
| §27.4 Injection via Hooks — `WithCache` | `docs/PRD.md` §27.4 (lines ~9941–9943) | OK — shorthand documented |
| §27.5 Cache Key Generation | `docs/PRD.md` §27.5 (lines ~9998, 10000, 10009, 10011) | OK — composite-key ordering invariant, fingerprint segment, fingerprint-agnostic `BuildTablePattern`, `cache.version` kill-switch all present |
| §27.6 + §27.7 — relationship bypass | `docs/PRD.md` (lines ~10024, 10031–10034, 10046) | OK — rule documented in §27.6 and row added to §27.7 operation table |
| §27.9 Invalidation Strategies — breaker wiring | `docs/PRD.md` §27.9 (lines ~10198, 10241–10256) | OK — circuit breaker referenced |
| §27.10 Serialization — `WithSerializer` + §3.4 validation | `docs/PRD.md` §27.10 (lines ~10287, 10298–10301) | OK — option and validation rule referenced |
| §27.13 Cache Metrics — `MetricsRecorder` rename | `docs/PRD.md` §27.13 (lines ~10352, 10354, 10356–10369, 10431) | OK — interface renamed; histogram methods present; OTel instruments referenced; `schema` leading parameter + `""` for MySQL/SQLite |
| `database/transaction.go:OnCommit` godoc | `database/transaction.go` (lines ~102–110) | OK — FIFO, savepoint promotion, rollback-discards guarantees + `TestOnCommitCallbackOrdering` reference |
| `hook/hook.go:MutationContext.AffectedPKs` godoc | `hook/hook.go` (lines ~63–69) | OK — "used by event hooks and cache invalidation"; element-shape contract documented |
| `guidelines/ARCHITECTURE.md` "Key Runtime Dependencies" | `guidelines/ARCHITECTURE.md` (line ~239) | OK — `golang.org/x/sync/singleflight` row present alongside `errgroup` |

**Files changed for 12.11:**
- `docs/tracker/phase-12.md` — restored the missing `## 12.11 Sync-Back to PRD / godoc` section header (the header was accidentally elided during an earlier tracker edit so 12.11's body appeared orphaned after 12.10's completion notes), checked off all Tasks + Tests Required boxes, flipped status to Complete, and added this completion record.
- `docs/design/CACHE.md` §25 intro — updated the status line from `Completed (2026-04-20)` to `Verified in sync (2026-04-22, Phase 12.11)` to reflect the re-verification seal.
- `docs/tracker/STATUS.md` — marked 12.11 complete in the Phase 12 line and updated Current Focus.

**Results:**
- `make check` → pass (lint clean, all unit tests green). Docs-only changes in this sub-item; no code changes.

**Notes:**
- No semantic drift detected in any PRD section or godoc target — every row in CACHE.md §25 still matches its landing site.
- The CACHE.md §25 table is preserved verbatim as a provenance trail (per the §25 intro); only the dated status header was updated. Per-row commit links were not added because Phase 12 commits are all on `main` and traceable via `git log -- docs/PRD.md` scoped to 12.x commit messages — adding per-row SHAs would duplicate git history without adding signal.
- The three pre-Phase 12 godoc rows (transaction.go, hook.go, ARCHITECTURE.md) were verified as regression checks — no edits expected, none made.

---

## 12.12 Stretch — Type-aware `sqlgen lint` cache rules

**PRD Reference:** (not in PRD — deferred DX goal)
**Design Reference:** CACHE.md §19A

**Status:** Deferred (2026-04-22) — moved to post-Phase 13 dev-tooling batch. Tenancy (Phase 13) and other v1-scope features take priority; type-aware lint is a build-time UX upgrade, not a safety requirement (runtime dispatch in 12.8 already surfaces mismatches with descriptive errors at call time). Will be picked up alongside the §29 tenant-aware lint rule (per `IMPLEMENTATION_ORDER.md` §13 — also dependent on 12.12 infrastructure) and any other dev-tooling work in a dedicated tooling batch.

### Tasks

- [ ] Extend existing `cmd/sqlgen/cli/lint.go` `tableInfo` with PK metadata: `PKGoType string`, `PKImportPath string`, `PKComposite bool`, `PKStructName string`, `PKFields []PKField{Name, GoType, ImportPath}`
- [ ] Populate PK metadata inside `buildTypeRegistry` from the parsed schema + config
- [ ] Add `golang.org/x/tools/go/packages` dependency to the CLI module only — runtime stays stdlib + `x/sync` (same library used by staticcheck/gopls/goimports, not exotic)
- [ ] Create `cmd/sqlgen/cli/lint_cache.go` with `lintCacheCallsTyped` pass using `packages.Load` with `NeedName | NeedFiles | NeedSyntax | NeedTypes | NeedTypesInfo | NeedImports`
- [ ] Scanner walks each loaded package's syntax tree looking for `*<facade>.Invalidate` / `.InvalidateMany` / `.InvalidateTable` method calls — uses `types.Info.Selections` / `types.Info.Types` to confirm receiver is `*<pkg>.Cache` and to resolve argument static types
- [ ] Implement `validateCacheCall(call, tableInfo, pkgs)` emitting `lintIssue` records per the CACHE.md §19A diagnostic table
- [ ] Wire `lintCacheCallsTyped` into the existing `sqlgen lint` pipeline (reuses `lintIssue`, `formatLintOutput`, `sortIssues`, `hasIssuesAtOrAbove`, severity levels)
- [ ] Existing AST-only hook-registration rules continue to work unchanged — cache rules layer alongside, they don't replace
- [ ] `cmd/sqlgen/cli/lint_cache_test.go` — golden lint output for a fixture project that exercises each diagnostic kind

### Acceptance Criteria

- Runtime module dependency surface unchanged — `x/tools/go/packages` lands only in the CLI module
- Existing hook-registration lint rules still pass (no regression)
- Type-aware pass resolves PK argument types for real consumer code: variables, struct fields, function-call results — not just literals
- Diagnostics emitted per the CACHE.md §19A table:
  - **Error** — single-PK table + PK arg's static type differs from `PKGoType`
  - **Error** — composite-PK table + non-matching struct type passed to `Invalidate`
  - **Error** — composite-PK table + scalar (non-struct) PK arg
  - **Error** — `InvalidateMany` with `[]any` elements whose resolved types don't match
  - **Error** — `InvalidateMany` with homogeneous typed slice (e.g. `[]uuid.UUID`) passed as `[]any` whose element type doesn't match
  - **Info** — first arg is `hook.TableName(expr)` or a variable whose value can't be resolved (genuinely unknowable)
  - **Info** — receiver is a `*<pkg>.Cache` returned from an unresolvable factory (genuinely unknowable)
- Severities match existing lint philosophy: error for provable mismatches, info for unresolvable cases; no new warning severity needed
- Test files (`*_test.go`) and generated files (`*_gen.go`) are skipped (existing `sqlgen lint` behavior preserved)
- Stretch positioning: can slip to a follow-up release without blocking caching itself (runtime dispatch in 12.8 already surfaces mismatches with descriptive errors at call time — this is a build-time UX upgrade, not a safety requirement)

### Tests Required

- [ ] Fixture: single-PK UUID table + `c.Invalidate(ctx, TableProducts, 42)` → error naming expected `uuid.UUID`, actual `int`
- [ ] Fixture: composite-PK `OrderItems` table + `c.Invalidate(ctx, TableOrderItems, UserPK{...})` → error naming expected `OrderItemPK`, actual `UserPK`
- [ ] Fixture: composite-PK table + scalar PK → error naming expected struct, actual scalar
- [ ] Fixture: `c.InvalidateMany(ctx, TableOrderItems, []any{OrderItemPK{}, UserPK{}})` → error naming element index 1
- [ ] Fixture: `c.InvalidateMany(ctx, TableOrderItems, []uuid.UUID{...})` passed where `[]OrderItemPK` expected → error on slice element type
- [ ] Fixture: `c.Invalidate(ctx, hook.TableName(rawStr), pk)` → info-level diagnostic (dynamic table not statically checked)
- [ ] Fixture: `c.Invalidate(ctx, TableProducts, productID)` where `productID` is a correctly-typed `uuid.UUID` variable → no diagnostic
- [ ] Existing hook-registration lint rules still produce the same output on fixtures from Phase 8 (regression)
- [ ] `_test.go` and `_gen.go` skipped (existing behavior preserved)

### Completion Record

_Filled in when picked up in the post-Phase 13 dev-tooling batch._

---

## Dependency Graph (summary)

```
12.1 (cache/ core) ───┬─── 12.2 (invalidation + FromEventSubscriber)
                      ├─── 12.4 (cache/memory/)
                      ├─── 12.5 (cache/redis/)
                      ├─── 12.6 (cache/msgpack/)
                      └─── 12.7 (metrics/otel/)

12.3 (config) ────────┐
12.1 + 12.2 + 12.3 ───┴─── 12.8 (generator)

12.4 + 12.8 ─────────────── 12.9 (E2E: in-memory, SQLite)
12.5 + 12.8 ─────────────── 12.10 (Redis integration)

all above ──────────────── 12.11 (sync-back to PRD)

12.8 ───────────────────── 12.12 (stretch: type-aware lint)
```

**Critical path:** 12.1 → 12.3 → 12.8 → 12.9. Backends (12.4–12.7) can be built in parallel with config + generator once 12.1 is done.
