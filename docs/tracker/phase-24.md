# Phase 24: Event Bus — First-Class NATS Transport

Status: In Progress (11 of 11 complete — 24.1, 24.2, 24.3, 24.4, 24.5, 24.6, 24.7, 24.8, 24.9, 24.10, 24.11 — ready for `/close-phase 24`)
PRD Sections: 28.3, 28.5, 28.7, 28.7.1, 28.8, 29.6
Design doc: PRD §28 (PROPOSED 2026-07-16 — decision record D1–D6, phased tickets EB-0…EB-3)

> **Blessing note.** The PRD §28 deltas (B1–B10) are **already written and blessed** — `json`
> tags on `Event` (§28.3), Handler-error semantics (§28.5), the split NATS-core / NATS-JetStream
> transport table + the natsbus §28.7.1 rewrite, and `event.Config.MetadataFunc` (§28.8). This
> phase **implements** that spec; it does not re-derive it. Where a sub-item lists a PRD-sync task
> it is confirming code matches the already-normative text, not drafting new PRD.
>
> **Dependency order (record):** 24.1 (contract hygiene) is the foundation and unblocks all.
> 24.2 (`MetadataFunc`) is independent of natsbus but **gates 24.5** (traceparent rides
> `MetadataFunc`). 24.3/24.4 follow 24.1; 24.5 follows 24.4 (headers) + 24.2. 24.6 (memorybus) is
> independent. 24.7 (JetStream) follows 24.4; 24.8 → 24.9 chain; 24.10 follows 24.7. 24.11
> (convenience) is independent and may slot anytime. Sub-items 24.1–24.6 ship **observability with
> no durability** (EB Phases 0–1); 24.7–24.10 are the **durability headline** (EB Phase 2); 24.11
> is optional sugar (EB Phase 3). Governing decisions D1–D6 are attributed to the sub-item whose
> behavior they gate.

> **Load-bearing constraint (design §1.2), applies to 24.2 and 24.5.** In the default
> `CallbackAsync` mode the deferred `Tx.OnCommit` publish runs on a fresh `context.Background()`
> (`database/transaction.go`; PRD §18.5, §27.9). Request-scoped data (spans, actor, request-id)
> is therefore **not** readable at publish time and MUST be captured **synchronously at
> mutation-hook entry** — the same discipline §29.6 mandates for the tenant stamp. This is why
> consumer metadata and traceparent are producer-side `MetadataFunc` concerns, never transport
> concerns.

---

## 24.1 Contract hygiene — `json` tags on `event.Event` (EB-0.1)

**PRD Reference:** Section 28.3 (Wire format — stable snake_case keys, `Metadata` omitted when empty). Design §6 (Phase 0), §7 EB-0.1/EB-0.2/EB-0.3.

**Status:** Complete (2026-07-31)

**Governing decisions:** none new — pure wire hygiene, pre-release (no back-compat gate per standing project guidance).

### Tasks

- [x] Confirm `event.Event` in `event/event.go` carries the blessed `json` tags exactly as §28.3 (`id`, `table`, `schema`, `action`, `pk`, `input`, `timestamp`, `metadata,omitempty`) — the struct still marshaled with Go field names (no tags); **added** the tags.
- [x] Regenerate / refresh any golden fixtures that pin serialized event JSON — **none exist**: natsbus marshals and unmarshals through the same `event.Event` struct (round-trip is tag-agnostic — `TestJSONRoundTrip` passes unchanged), and no example asserts event wire bytes. Confirmed via repo-wide grep; zero golden churn.
- [x] Verify memorybus is unaffected — dispatches concrete `event.Event` values in-process, never serializes; `make check` green, no golden churn.
- [x] Confirm §28.5 Handler-error doc (EB-0.2, PRD.md:12005) and the §28.7 JetStream PRD prep (EB-0.3, §28.7/§28.7.1) are present and normative in `docs/PRD.md` — verified present (blessed last session); honored by 24.3/24.6/24.8.

### Acceptance Criteria

- `event.Event` serializes to stable snake_case keys independent of Go field names (§28.3 Wire format); the form survives generator upgrades.
- `Metadata` is omitted from the wire form when empty (`omitempty`), not emitted as `null`.
- memorybus behavior and output are byte-identical (in-process, no JSON).
- `make check` + `make check-examples` clean; the only diff is the natsbus/serialized-JSON golden refresh.

### Tests Required

- [x] Wire-form contract test asserts snake_case keys and that an empty-`Metadata` event omits the key — `event/event_test.go` `TestEventJSONWireFormat` (marshal → key-set assertion via `cmp.Diff` on sorted keys; `omitempty` sub-test). Placed in the `event` package itself (source of the tags, stdlib-only, no NATS server needed); the natsbus `TestJSONRoundTrip` round-trip stays green as the transport-level companion.
- [x] Golden diff confined to serialized-event fixtures — in fact **zero** golden/generated-source changes; only `event/event.go` (tags) + `event/event_test.go` (test).

### Completion Record

- **Date:** 2026-07-31
- **Files changed:** `event/event.go` (added `json` snake_case tags to all 8 `Event` fields per §28.3; `metadata,omitempty`), `event/event_test.go` (new `TestEventJSONWireFormat` — snake_case key-set + empty-`Metadata`-omitted; added `encoding/json`/`maps`/`slices` imports).
- **Tests:** `go vet ./event/...` clean; `make check` **green** across all modules (event, memorybus, natsbus round-trip, parser, CLI, cache, metrics) — fmt + lint + vet + unit all pass. `make check-examples` **green** across all 12 example modules (cache, events, graphql, graphql_top_level, mysql, postgres, postgres_stdlib, sqlite, tenancy, tenancy_mysql, tenancy_postgres) with **zero golden/source drift** — nothing regenerated under `cmd/sqlgen/testdata/examples/`, confirming the analytical prediction.
- **Notes:** No golden or generated-source churn — the marshal round-trip flows through the `Event` struct on both ends, so adding tags changes only the on-wire key names, which nothing pinned; the generator never emits the `Event` struct (examples import it), the field set/types are unchanged, and example tests use memorybus (no serialization). No security-review trigger (paths not under `manifest/` embed or `cmd/sqlgen/cli/**`).

---

## 24.2 `event.Config.MetadataFunc` + generator hook-entry merge (EB-1.7, D6)

**PRD Reference:** Section 28.8 (`MetadataFunc` field + "timing and merge" — invoked once at hook entry, per-event shallow clone, `"tenant"` applied last/unoverridable, nil ⇒ byte-identical), 28.3 (Metadata provenance), 29.6 (system tenant stamp interaction). Design §5.5, §2-D6, §9 (blast-radius).

**Status:** Complete (2026-07-31)

**Governing decisions:** D6 (consumer-injected metadata via `event.Config.MetadataFunc func(ctx) map[string]string`, merged producer-side at hook entry; only `"tenant"` hard-reserved). This is the **highest-blast-radius** sub-item — it touches the shared stdlib-only `event` package **and** the generator (the single new injection point), and both memorybus and natsbus exercise it.

### Tasks

- [x] Add the additive `MetadataFunc func(ctx context.Context) map[string]string` field to `event.Config` (`event/event.go`) — no interface method change; `nil` default. Already flows through `WithEventPublisher`'s `func(*event.Config)` configurator (same path as `OnError`), so no wiring change was needed.
- [x] Generator: in the per-table event hook (`event_hooks.go.tmpl`), call `metaFunc(ctx)` **once at hook entry** (after the skip/no-PK early returns, request ctx live, before any `Tx.OnCommit` registration), guarded on non-nil → `baseMeta`. Threaded as a new param on `newXXXEventHook`, resolved from `cfg.MetadataFunc` in `buildEventHooks`.
- [x] Merge into each fanned-out event via a generated `mergeEventMetadata(base, system)` helper: `make`s a fresh map **per event** (per-event isolation), `maps.Copy(merged, base)` then `maps.Copy(merged, system)` so the system stamp is applied **last** and wins. Non-tenanted hooks pass `system = nil`; tenanted pass the existing per-row `tenantMeta`.
- [x] Preserve the nil-`MetadataFunc` path byte-identical: `mergeEventMetadata` returns `nil` when both maps are empty, so a nil `MetadataFunc` on a non-tenanted table yields a nil `Metadata` (not an empty map) — pinned by `TestMetadataFunc_Nil_NoMetadata` + the confined golden diff.
- [x] Confirm the single injection point coexists with the existing §29.6 per-row tenant stamp (23.3) — `tenantMeta` is built unchanged inside the fanout loop and passed as the `system` arg, so it is applied last and remains the authoritative writer of `"tenant"` (`TestMetadataFunc_TenantReservedKeyWins`).
- [x] Regenerate goldens; diff confined to `event_hooks_gen.go` across the 6 event-enabled examples (events, cache, graphql, tenancy, tenancy_mysql, tenancy_postgres); non-event examples (postgres/mysql/sqlite/postgres_stdlib/graphql_top_level) byte-identical.

### Acceptance Criteria

- `event.Config.MetadataFunc(ctx)` is invoked **once per mutation-hook entry**, never from the deferred `OnCommit` callback (which sees `context.Background()`) — verified by a test that a value only present in the request ctx reaches `Event.Metadata` under the default async-commit path.
- Returned keys (e.g. `actor`, `request_id`, `traceparent`) appear in every fanned-out event's `Metadata`; the map is **shallow-cloned per event** so per-row system stamps do not bleed across a batch.
- The system key `"tenant"` is applied **after** the user map and **cannot** be overridden by `MetadataFunc` (reserved-key precedence, §28.8 / §29.6).
- When `MetadataFunc` is `nil`, generated output and runtime wire form are **byte-identical** to pre-phase (§28.8 nil clause).
- Transport-agnostic: the merge writes to `Event.Metadata`, so memorybus, natsbus, and any Redis/Kafka adapter all observe the injected keys with no transport change.

### Tests Required

- [x] Metadata-merge + isolation — `events/tests/metadata_func_test.go` `TestMetadataFunc_MergedIntoEveryEvent`: `{actor, request_id}` present on every event across a 3-row `CreateMany`; mutating `events[0].Metadata` does not bleed into siblings (per-event clone).
- [x] Reserved-key precedence — `tenancy/tests/metadata_func_test.go` `TestMetadataFunc_TenantReservedKeyWins`: on a tenanted table a `MetadataFunc` returning `{"tenant":"HACKED","actor":"bob"}` yields `Metadata["tenant"]` = the resolved row tenant (system wins, §28.8) while `actor=bob` is carried; the non-tenanted "no system tenant stamp" side is `events/tests` `TestMetadataFunc_NonTenanted_NoTenantStamp`. (Note: the system reserves `"tenant"` where it stamps it — tenanted tables; on non-tenanted tables the system adds none, matching the PRD's "system wins where it applies" rather than adding un-specced stripping.)
- [x] Multi-tenant batch — `tenancy/tests/metadata_func_test.go` `TestMetadataFunc_MultiTenantBatch_PerRowTenantSharedKeys`: a `SkipTenancy` `CreateMany` across tenants A and B stamps each event with its own row's tenant (2 distinct stamps, no bleed) alongside the one shared `MetadataFunc` payload.
- [x] Async-commit timing — `events/tests/metadata_func_test.go` `TestMetadataFunc_CapturedAtHookEntry_UnderTx`: an in-transaction Create (default async callback mode) carries an actor present only in the request ctx — proving `MetadataFunc` ran at hook entry, not from the `Background()` OnCommit callback. Render-level order also pinned in `gen/event_hooks_template_test.go` `TestEventHooks_metadataFunc_mergedAtHookEntry` (resolve strictly before the publish closure).
- [x] Nil-`MetadataFunc` byte-identical — `events/tests` `TestMetadataFunc_Nil_NoMetadata` (nil metaFunc, non-tenanted → `Metadata` nil) + the golden diff confined to `event_hooks_gen.go` on event-enabled examples only.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/event.go` — additive `Config.MetadataFunc func(ctx context.Context) map[string]string` field (D6), documented against §28.8/§29.6/§18.5/§27.9; no interface change (blessed blast-radius: the only `event`-pkg change).
  - `cmd/sqlgen/gen/templates/event_hooks.go.tmpl` — new generated `mergeEventMetadata(base, system)` helper (per-event `make` + double `maps.Copy`, system last, nil when empty); `newXXXEventHook` gains a `metaFunc` param; `baseMeta := metaFunc(ctx)` resolved once at hook entry; every `Event.Metadata` now set to `mergeEventMetadata(baseMeta, tenantMeta|nil)`; `buildEventHooks` threads `cfg.MetadataFunc`.
  - `cmd/sqlgen/gen/context_event.go` — add `"maps"` to the generated event-file imports (helper always uses `maps.Copy`).
  - `cmd/sqlgen/gen/event_hooks_template_test.go` — updated 3 render pins to the merge form; new `TestEventHooks_metadataFunc_mergedAtHookEntry` (param + hook-entry resolve + helper + threading + `maps` import + resolve-before-publish ordering); tightened the shared-table pin (non-tenanted merges with a nil system map, no `tenantMeta`/`mc.AffectedTenants`).
  - Regenerated `event_hooks_gen.go` (models/ + expected/) across the 6 event-enabled examples.
  - `cmd/sqlgen/testdata/examples/events/tests/metadata_func_test.go` (new, 4 tests) + `cmd/sqlgen/testdata/examples/tenancy/tests/metadata_func_test.go` (new, 2 tests) + `tenancy/tests/main_test.go` (`withMetadataFunc` envOpt).
- **Tests:** `make check` green (all runtime/parser/CLI/gen modules); `make check-examples` green across all 12 example modules; the 6 new example tests + the new render test pass; `go vet` clean.
- **Notes:** Merge helper deliberately lives in **generated code**, not the `event` package — the design's blessed blast-radius (§9) is "shared `event` pkg: `Config.MetadataFunc` field only; generator: one injection point." The reserved-key guarantee is "system wins where it stamps" (tenanted tables, §29.6); no un-specced stripping of a consumer `"tenant"` on non-tenanted tables was added. traceparent (24.5) will ride this same `MetadataFunc` path with no further generator change. No security-review trigger (no `manifest/` embed or `cmd/sqlgen/cli/**` change).

---

## 24.3 natsbus subscribe error routing + retry (EB-1.1, EB-1.2)

**PRD Reference:** Section 28.5 (Handler-error semantics — at-most-once transports route the error to their configured error handler), 28.7.1 (`WithSubscribeErrorHandler`, `WithRetry`). Design §2-D2/D3, §7 EB-1.1/EB-1.2.

**Status:** Complete (2026-07-31)

**Governing decisions:** D2 (Handler error = "redeliver me", honored where the transport can; core NATS cannot, so the error is surfaced not swallowed), D3 (subscriber error handling is a **natsbus-local** Option, NOT on `event.Config` which is publisher-side, NOT on `event.SubscribeOptions`).

### Tasks

- [x] Add `natsbus.WithSubscribeErrorHandler(func(ctx context.Context, e event.Event, err error))` Option; store on `*Bus`. Added; stored on a new `Bus.subErrHandler` field (set once at construction, read lock-free by callback goroutines like `prefix`).
- [x] Replace `_ = handler(context.Background(), e)` in `msgHandler` with: invoke handler, and on non-nil error route to the configured error handler (if set); default remains discard-with-no-panic to preserve zero-config behavior, but the error is now **observable** when a handler is registered. `msgHandler` now calls `b.deliver(ctx, e, handler)` and routes the final non-nil error to `b.subErrHandler` when set.
- [x] Add `natsbus.WithRetry(n int, backoff)` — bounded in-process retry of the handler on core (at-most-once) subscriptions before the error handler fires; retry is a core-mode concept (JetStream redelivery in 24.8 supersedes it). `backoff` is a fixed `time.Duration` slept between attempts (least-mechanism; no speculative exponential). Semantics: **`n` = total handler invocations** (attempt up to `n` times, short-circuit on first `nil`); matches "retries up to `n` times" and the two acceptance tests. `n < 1` ⇒ single attempt.
- [x] Keep the change additive: `New(conn)` with no options behaves exactly as today (error discarded, no retry) — no regression for existing core consumers. `deliver` defaults to `max(retryAttempts,1)` = 1 attempt; `subErrHandler` nil ⇒ error discarded (no panic).

### Acceptance Criteria

- A core-NATS handler returning an error with `WithSubscribeErrorHandler` set invokes the error handler with the event and error; without it, behavior is unchanged (no panic, no redelivery — core cannot redeliver).
- `WithRetry(n, backoff)` retries the handler up to `n` times on core mode before routing to the error handler; a handler that eventually returns nil is not routed to the error handler.
- Errors are never silently discarded when an error handler is configured (§28.5 "always observable").
- Zero-config `New(conn)` is byte-for-byte behaviorally identical to pre-phase.

### Tests Required

- [x] Failing-first: a core handler that returns an error routes to `WithSubscribeErrorHandler` (asserts event + error identity); pre-change this was `_ =` and unobservable. `TestSubscribeErrorHandlerRoutesError` — `errors.Is` on the sentinel + `cmp.Diff` on the event; asserts exactly one routing call (no redelivery).
- [x] `WithRetry`: handler fails `n-1` times then succeeds → no error-handler call; fails `n` times → exactly one error-handler call after the retries. `TestRetry` (table-driven, `n=3`): `failCount=2` → 3 invocations, 0 error-handler calls; `failCount=3` → 3 invocations, 1 error-handler call.
- [x] Default (no error handler): a failing handler does not panic and does not redeliver. `TestDefaultFailingHandlerNoPanicNoRedeliver` — single invocation, no panic.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — added `time` import; three new `Bus` fields (`subErrHandler`, `retryAttempts`, `retryBackoff`, documented as set-once/read-lock-free); `WithSubscribeErrorHandler` + `WithRetry(n, backoff time.Duration)` Options; `msgHandler` now routes the final handler error to `subErrHandler`; new `deliver` helper (bounded `max(n,1)`-attempt loop, short-circuit on nil, fixed `backoff` sleep between attempts). `New(conn)` output-identical (no options ⇒ 1 attempt, nil error handler ⇒ discard).
  - `event/natsbus/natsbus_test.go` — added `errors` import; `errHandlerBoom` sentinel; `TestSubscribeErrorHandlerRoutesError`, `TestRetry` (table-driven), `TestDefaultFailingHandlerNoPanicNoRedeliver`.
- **Tests:** `go test ./...` in natsbus green; `make check` **green** across all modules (runtime incl. event/memorybus/natsbus, parser, CLI, cache, metrics) — fmt + lint + vet + unit all pass; `gofumpt -l` clean.
- **Notes:** Purely natsbus-local and additive — **no** `event`-package, generator, or golden change (natsbus is a separate module examples never import; examples use memorybus). No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path). No security-review trigger (not `manifest/` embed nor `cmd/sqlgen/cli/**`). `backoff` is a fixed `time.Duration` (least-mechanism; exponential deferred until a real need). Retry is core-mode; JetStream redelivery (24.8) supersedes it.

---

## 24.4 natsbus header plumbing + envelope version (EB-1.3)

**PRD Reference:** Section 28.7.1 (Wire headers — `Nats-Msg-Id`, `traceparent`, `Sqlgen-Envelope-Version` carried as NATS headers out of band from the JSON envelope; unknown headers ignored by older consumers). Design §6, §7 EB-1.3.

**Status:** Complete (2026-07-31)

**Governing decisions:** none new — foundational plumbing that 24.5 (traceparent header) and 24.7 (`Nats-Msg-Id`) build on.

### Tasks

- [x] Switch core publish from `conn.Publish(subject, data)` to `conn.PublishMsg(&nats.Msg{Subject, Data, Header})` so headers can be attached. `Publish` now builds a `*nats.Msg` with `Header: nats.Header{headerEnvelopeVersion: {envelopeVersion}}` and calls `conn.PublishMsg`.
- [x] Define the `Sqlgen-Envelope-Version` header constant and stamp it on every published message (current scheme = version 1); document that any subject-scheme or body-encoding change bumps it. `headerEnvelopeVersion = "Sqlgen-Envelope-Version"` + `envelopeVersion = "1"`, both documented with the bump rule.
- [x] Establish the header-name constants (`Nats-Msg-Id`, `traceparent`, `Sqlgen-Envelope-Version`) in one place for reuse by 24.5/24.7. Single `const` block near `DefaultPrefix`: `headerEnvelopeVersion`, `headerMsgID` (`Nats-Msg-Id`, for 24.7), `headerTraceparent` (`traceparent`, for 24.5). Unexported — `unused` linter is not enabled, so the two forward-declared names compile clean; 24.5/24.7 wire them up.
- [x] Confirm subscribe-side tolerance: unknown/absent headers are ignored (older publishers send none); decoding still works when headers are missing. `msgHandler` decodes only `msg.Data` and never reads headers — already tolerant; pinned by `TestSubscribeHeaderTolerance` (unknown-extra + absent cases).
- [x] Keep JSON envelope body unchanged — headers are purely additive/out-of-band, so a plain-core consumer reading only `msg.Data` is unaffected. Body is the same `json.Marshal(e)` bytes as 24.1; pinned by the body-decode assertion in `TestPublishStampsEnvelopeVersion`.

### Acceptance Criteria

- Every published message carries `Sqlgen-Envelope-Version`; the JSON body is unchanged from 24.1's snake_case form.
- A subscriber built pre-phase (no header awareness) still decodes messages that now carry headers — headers are ignored, not required.
- Header names are single-sourced constants reused by 24.5 and 24.7.

### Tests Required

- [x] Publish attaches `Sqlgen-Envelope-Version`; a round-trip asserts the header is present and the body decodes unchanged. `TestPublishStampsEnvelopeVersion` — raw `ChanSubscribe` asserts `msg.Header.Get("Sqlgen-Envelope-Version") == "1"` (literal wire strings, independent of the unexported const) + `cmp.Diff` on the decoded body.
- [x] A message with unknown extra headers decodes correctly (forward-compat). `TestSubscribeHeaderTolerance/unknown_extra_headers_ignored` — raw `PublishMsg` with `X-Future-Header` still decodes to the event.
- [x] Absent-header path (simulated old publisher) still decodes. `TestSubscribeHeaderTolerance/absent_headers_(old_publisher)` — raw `conn.Publish` (no headers) still decodes.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — new header-name `const` block (`headerEnvelopeVersion`, `headerMsgID`, `headerTraceparent`) + `envelopeVersion = "1"` near `DefaultPrefix`, documented with the bump rule; `Publish` switched from `conn.Publish(subject, data)` to `conn.PublishMsg(&nats.Msg{Subject, Data, Header})` stamping `headerEnvelopeVersion`. Subscribe path unchanged (already header-agnostic).
  - `event/natsbus/natsbus_test.go` — added `encoding/json` import; `TestPublishStampsEnvelopeVersion` (publish-side header + body-unchanged pin) and `TestSubscribeHeaderTolerance` (table-driven: unknown-extra-headers forward-compat + absent-headers backward-compat).
- **Tests:** `go test ./...` in natsbus green; `golangci-lint run ./...` = 0 issues (the two forward-declared header consts pass — `unused` linter not enabled); `make check` **green** across all modules; `gofumpt -l` clean.
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, or golden change; body bytes identical to 24.1. No E2E trigger (changed paths `event/natsbus/**` match no glob) and no security-review trigger. `headerMsgID`/`headerTraceparent` are forward-declared here (the task's "single place for reuse") and wired up by 24.7/24.5 respectively. Header transport works on the in-process test server (NATS 2.2+ core headers).

---

## 24.5 natsbus trace-context propagation (EB-1.5, D4)

**PRD Reference:** Section 28.7.1 (Trace-context propagation — `Metadata["traceparent"]` lifted to a NATS header on publish; `WithContextPropagation` rebuilds a span-continuing ctx on subscribe; adapter is a natsbus-local Inject/Extract interface, no hard OTel dep). Design §5 (Option A), §2-D4, §9.

**Status:** Complete (2026-07-31)

**Depends on:** 24.4 (header plumbing), 24.2 (`MetadataFunc` carries `traceparent` — there is **no** traceparent-specific producer code; it rides D6).

**Governing decisions:** D4 (traceparent is an ordinary `MetadataFunc` key; only natsbus gives it wire treatment; the adapter interface keeps OTel out of the natsbus `go.mod`; `event/` never imports a propagator).

### Tasks

- [x] Define a small natsbus-local propagator adapter interface (Inject/Extract over `nats.Header`) — natsbus takes **no** hard `go.opentelemetry.io` dependency; the consumer supplies an adapter wrapping their propagator. Added exported `Propagator interface { Inject(ctx, nats.Header); Extract(parent, nats.Header) context.Context }`, documented per §28.7.1.
- [x] Publish side: lift `Event.Metadata["traceparent"]` (set by the consumer's `MetadataFunc` at hook entry) onto the `traceparent` NATS header (reusing 24.4's plumbing). `Publish` now sets `header.Set(headerTraceparent, tp)` when `e.Metadata["traceparent"]` is non-empty (unconditional — publisher and subscriber are different processes, so the header travels regardless of the local propagator config).
- [x] Subscribe side: add `natsbus.WithContextPropagation(adapter)`; when set, read the `traceparent` header and build the ctx handed to `Handler` so it **continues** the producing span instead of `context.Background()`. New `WithContextPropagation(p Propagator) Option` stores `b.propagator`; `msgHandler` does `ctx = b.propagator.Extract(context.Background(), msg.Header)` when set.
- [x] No generator change: traceparent is already produced by 24.2's merge; this sub-item is entirely natsbus-local (steps 2–3 of design §5). Confirmed — zero generator/golden churn.
- [x] Confirm `"traceparent"` is treated as an ordinary consumer key (not reserved) — only `"tenant"` is hard-reserved. natsbus reads `Metadata["traceparent"]` with no reservation logic; only 24.2's tenant stamp is system-owned.

### Acceptance Criteria

- With a consumer `MetadataFunc` emitting `traceparent` and `WithContextPropagation` set, a subscribed `Handler` receives a ctx that continues the producing request's span (verified as a single connected trace across the async hop, under the default async-commit path).
- Without `WithContextPropagation`, `traceparent` still travels in the envelope/header but the handler ctx is the default — no crash, no OTel requirement.
- The natsbus module builds with **no** OpenTelemetry dependency in its `go.mod`; the adapter is supplied by the consumer.
- `event/` imports no propagator (stdlib-only invariant §28.2 preserved).

### Tests Required

- [x] Two-service trace-continuity integration test: producer sets `traceparent` via `MetadataFunc`; subscriber with `WithContextPropagation` continues the span (one trace, parent/child linkage asserted via a fake propagator adapter). `TestContextPropagationContinuesSpan` — a `fakePropagator` round-trips the `traceparent` header through a ctx value; the handler ctx carries the exact `traceparent` the producer emitted (rebuilt across the async hop). Companion `TestContextPropagationHeaderOnWire` pins the publish-side header lift via raw `ChanSubscribe`.
- [x] No-adapter path: `traceparent` header present but `WithContextPropagation` unset → handler runs on the default ctx, no panic. `TestNoPropagatorDefaultCtx` — header on the wire, no propagator, handler's `ctx.Value(traceCtxKey{})` is nil, event still delivered.
- [x] Build/import test (or `go list`) asserts natsbus has no OTel import; `event/` has no propagator import. `TestNoTracingDependency` — `go list -deps` over both `event/natsbus` and `event` production closures; fails on any `opentelemetry`/`go.opentelemetry.io` dep (D4 invariant).

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — new exported `Propagator` interface (`Inject`/`Extract` over `nats.Header`), documented as the consumer-supplied OTel seam; `WithContextPropagation(p Propagator) Option` + `Bus.propagator` field (set-once/read-lock-free like `subErrHandler`); `Publish` lifts `Event.Metadata["traceparent"]` onto the `headerTraceparent` (24.4's reserved const) header when present; `msgHandler` rebuilds ctx via `propagator.Extract(context.Background(), msg.Header)` when a propagator is set, else the default `context.Background()`.
  - `event/natsbus/natsbus_test.go` — added `os/exec`/`strings` imports; `fakePropagator` (ctx-value round-trip stand-in for an OTel adapter, no OTel dep); `TestContextPropagationContinuesSpan`, `TestContextPropagationHeaderOnWire`, `TestNoPropagatorDefaultCtx`, `TestNoTracingDependency` (documented `//nolint:gosec` on the fixed-literal `go list` exec).
- **Tests:** `go test ./...` in natsbus green; `make check` **green** across all modules; `golangci-lint` 0 issues; `gofumpt -l` clean.
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, or golden change (traceparent rides 24.2's `MetadataFunc` merge per D4/D6; no traceparent-specific codegen). No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path) and no security-review trigger. **Publish lift is unconditional** (header travels whenever `Metadata["traceparent"]` is set, regardless of the local propagator) because publisher and subscriber are separate processes — the acceptance criterion "without `WithContextPropagation`, `traceparent` still travels in the header" requires this. The `Propagator` interface carries `Inject` per the §28.7.1 "Inject/Extract" adapter shape and consumer-side symmetry, but **natsbus itself never calls `Inject`**: under the default async-commit path the publish ctx is `context.Background()` (§1.2), so there is no live span to inject — traceparent instead rides `Metadata` and is lifted directly. natsbus calls only `Extract` (subscribe side). This is documented on the interface.

---

## 24.6 memorybus optional local error handler (EB-1.6, D5 parity)

**PRD Reference:** Section 28.7.1 (in-memory bus — synchronous dispatch, at-most-once, not for distributed use). Design §2-D5, §7 EB-1.6, §9.

**Status:** Complete (2026-07-31)

**Depends on:** none (independent; stdlib-only runtime package).

**Governing decisions:** D5 (memorybus stays a faithful at-most-once reference impl and does **NOT** emulate durability; it may optionally forward the Handler error to a local error handler for D3 parity — the reference bus must not lie about its guarantees).

### Tasks

- [x] Add an optional local error sink to `memorybus` (e.g. a `New` option or settable field) so a `Handler` error is observable in tests/single-process apps instead of `_ = s.handler(ctx, e)` (`memorybus.go:54`). Added an `Option` pattern (`New(opts ...Option)`, additive — existing `New()` calls unchanged) with `WithSubscribeErrorHandler(func(ctx, event.Event, err))`, mirroring natsbus's option name for D3/D5 parity; stored on a new `Bus.subErrHandler` field (set once at construction, read lock-free in `Publish` like natsbus's).
- [x] Keep dispatch synchronous and at-most-once — **no** retry, **no** redelivery, **no** durability emulation (D5). The error sink is purely observational. `Publish` still calls each handler exactly once; the only change is `_ = s.handler(ctx, e)` → route the non-nil error to `subErrHandler` when set. No retry/nak/redeliver logic added.
- [x] Default (no sink) behavior is unchanged: errors discarded, no allocation, byte-identical to today. `subErrHandler` nil ⇒ error dropped exactly as before (the `!= nil` guard short-circuits before any call); no new allocation on the default path.

### Acceptance Criteria

- With an error sink configured, a memorybus handler error is delivered to the sink; without one, it is discarded exactly as today.
- No redelivery/retry/durability semantics are introduced — memorybus remains a faithful at-most-once reference (D5).
- Default construction (`New()`) is behaviorally identical to pre-phase.

### Tests Required

- [x] Handler error with sink set → sink observes the event + error; no redelivery. `memorybus_test.go` `TestSubscribeErrorHandlerRoutesError` — `errors.Is` on the `errHandlerBoom` sentinel + `cmp.Diff` on the event; asserts exactly one sink call (synchronous dispatch, no redelivery).
- [x] Default `New()` with a failing handler → no panic, error discarded (parity with prior behavior). `TestDefaultFailingHandlerNoPanicNoRedeliver` — no sink, failing handler; single invocation, no panic, `Publish` returns nil.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/memorybus/memorybus.go` — new `Option` type + `WithSubscribeErrorHandler(func(ctx, event.Event, err))` Option (documented against §28.7.1 / design D5, noting memorybus never redelivers and mirrors natsbus's option name); `New()` → `New(opts ...Option)` (additive); new set-once/read-lock-free `Bus.subErrHandler` field; `Publish` routes a non-nil handler error to `subErrHandler` when set (was `_ = s.handler(ctx, e)`), else discards unchanged.
  - `event/memorybus/memorybus_test.go` — added `errors` import + `errHandlerBoom` sentinel; `TestSubscribeErrorHandlerRoutesError` (event + error identity, exactly one call) and `TestDefaultFailingHandlerNoPanicNoRedeliver` (default no-sink no-panic single-invocation).
- **Tests:** `go test ./event/memorybus/` green; `make check` **green** across all modules; `gofumpt -l` clean; `go vet` clean.
- **Notes:** Purely runtime-package-local and additive — **no** `event`-pkg interface, generator, or golden change; no durability/retry/redelivery emulation (D5 — the reference bus must not lie about its at-most-once guarantee). No E2E trigger (changed paths `event/memorybus/**` match no glob — the runtime-package globs list `database/event/**`, a different path; `event/**` is not listed) and no security-review trigger (not `manifest/` embed nor `cmd/sqlgen/cli/**`). Independent of all other 24.x sub-items.

---

## 24.7 JetStream: `StreamConfig` + `WithJetStream` + dedup (EB-2.1, D1)

**PRD Reference:** Section 28.7.1 (JetStream mode — `WithJetStream(StreamConfig{Name, MaxAge, DedupWindow})`; publish to persisted stream; `Nats-Msg-Id` = `Event.ID` for broker-side dedup), 28.7 (transport table — NATS JetStream row, opt-in). Design §2-D1, §3, §7 EB-2.1.

**Status:** Complete (2026-07-31)

**Depends on:** 24.4 (header plumbing for `Nats-Msg-Id`), 24.1 (stable wire).

**Governing decisions:** D1 (JetStream is **opt-in**; `New(conn)` stays core NATS, byte-for-byte identical; JetStream provisions a stream at construct time only when the option is passed). The concrete `*Bus` carries the JetStream context; `event.Publisher` is unchanged (a wider concrete method than the interface).

### Tasks

- [x] Define `natsbus.StreamConfig{Name string, MaxAge time.Duration, DedupWindow time.Duration}` and `natsbus.WithJetStream(StreamConfig) Option`. Added both; `WithJetStream` stores `b.streamCfg` (set-once, read-lock-free like the other config fields). `DedupWindow` maps to JetStream `StreamConfig.Duplicates`.
- [x] On construction with `WithJetStream`, obtain the JetStream context and ensure the stream exists (create/update with the given `MaxAge`/`DedupWindow`); surface provisioning errors at construction, not silently. `New` calls a new `provisionStream` helper once (after options apply) using the modern `nats.go/jetstream` package: `jetstream.New(conn)` + `js.CreateOrUpdateStream(ctx, {Name, Subjects: ["{prefix}.>"], MaxAge, Duplicates})`. **Constructor can't return an error** (the PRD blesses single-return `bus := natsbus.New(nc, WithJetStream(...))`, D1 requires the core path byte-identical), so a provisioning failure is captured on `b.jsErr` and **surfaced from the first `Publish`/`Subscribe`** (returned, not swallowed) — provisioning is *attempted* at construction, *reported* at first use.
- [x] Publish path: when JetStream is enabled, publish to the persisted stream and set the `Nats-Msg-Id` header to `Event.ID` (reusing 24.4's header constants) so the broker dedup window collapses duplicate publishes with no consumer-side bookkeeping. `Publish` now: `if b.js != nil { msg.Header.Set(headerMsgID, e.ID); b.js.PublishMsg(ctx, msg) }`. The `_ context.Context` param is now named `ctx` and threaded into the JS publish (background ctx under the async-commit path, §1.2 — no deadline concern).
- [x] Keep the core path intact: without `WithJetStream`, publish is exactly today's core `PublishMsg` (24.4) — no stream, no dedup. The `b.js == nil` branch is byte-identical to 24.5's `conn.PublishMsg(msg)`; **no** `Nats-Msg-Id` header is set in core mode. Pinned by `TestJetStreamCoreModeCreatesNoStream` (core bus on a JS-enabled server provisions no stream — `jetstream.ErrStreamNotFound`).
- [x] Do **not** add a client-side dedup store for core NATS (explicitly rejected, design §8) — dedup is a JetStream-only feature. Confirmed — dedup is entirely broker-side via the `Nats-Msg-Id` header on the JS publish; no client-side store, no core-mode dedup.

### Acceptance Criteria

- `natsbus.New(conn)` (no JetStream) is byte-for-byte behaviorally identical to core mode; no stream is created.
- `WithJetStream` provisions/ensures the stream and publishes to it; each message's `Nats-Msg-Id` equals `Event.ID`.
- Two publishes of the same `Event.ID` within `DedupWindow` result in one stored message (broker dedup).
- Stream provisioning failure is returned/observable at construction, not swallowed.

### Tests Required

- [x] (Integration, `testing.Short()`-skippable) Publishing to a JetStream stream stores the message; `Nats-Msg-Id` = `Event.ID`. `TestJetStreamPublishStoresMessage` — publishes one event, then via a JS client fetches `stream.Info` (`State.Msgs == 1`) and `stream.GetMsg(LastSeq)`, asserting the stored `Nats-Msg-Id` header equals `Event.ID` and the decoded body round-trips (`cmp.Diff`).
- [x] Dedup window: republishing the same `Event.ID` inside `DedupWindow` yields a single stored message. `TestJetStreamDedupWindow` — publishes the same `Event.ID` twice within a 1-minute `DedupWindow`; asserts `State.Msgs == 1`.
- [x] Core mode (no option) creates no stream and is unchanged. `TestJetStreamCoreModeCreatesNoStream` — core `New(conn)` on a JS-enabled server; a JS `Stream()` lookup returns `jetstream.ErrStreamNotFound`, and a core publish still succeeds.
- [x] (Added) Provisioning-failure observability — `TestJetStreamProvisioningFailureSurfaced`: `WithJetStream` against a JS-**disabled** server; the deferred `jsErr` is surfaced (non-nil error) from **both** the first `Publish` and the first `Subscribe`.

> **Test-harness note.** Tests use the module's established **in-process embedded NATS server** (`nats-io/nats-server/v2/test`, the harness 24.3–24.5 already use) with `opts.JetStream = true` + a per-test `t.TempDir()` store — a **real JetStream broker**, in-process. This deviates from the tracker's "testcontainers" wording: testcontainers would add a heavy new dep (`testcontainers-go` + Docker) to the natsbus `go.mod` for no real-broker gain over the embedded server, which already exercises genuine persistence/dedup semantics. All four are `testing.Short()`-skippable per the requirement (verified: `go test -short` skips all four).

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — new import `github.com/nats-io/nats.go/jetstream` (subpackage of the already-required `nats.go` — **no `go.mod`/`go.sum` change**); new exported `StreamConfig{Name, MaxAge, DedupWindow}` + `WithJetStream(cfg) Option` (documented per §28.7.1/D1); three new set-once/read-lock-free `Bus` fields (`streamCfg`, `js jetstream.JetStream`, `jsErr error`); `New` provisions the stream once via the new `provisionStream` helper (`jetstream.New` + `CreateOrUpdateStream` with `Subjects: ["{prefix}.>"]`, `MaxAge`, `Duplicates=DedupWindow`) capturing `jsErr`; `Publish` gains a `jsErr` guard + a JetStream branch (stamps `headerMsgID`=`Event.ID`, `js.PublishMsg(ctx, msg)`) with the core `conn.PublishMsg` path byte-identical; `Subscribe` gains a `jsErr` guard; package doc updated to note JetStream mode. `New(conn)` (no option) output-identical to 24.5.
  - `event/natsbus/natsbus_test.go` — new import `nats.go/jetstream`; `startJetStreamServer(t)` helper (embedded server, `JetStream=true`, `t.TempDir()` store); 4 `testing.Short()`-skippable tests (store+`Nats-Msg-Id`, dedup window, core-mode-no-stream, provisioning-failure-surfaced).
- **Tests:** `go test ./...` in natsbus green (JetStream tests run in ~0.5s in-process); `go test -short` skips all four JetStream tests; `make check` **green** across all modules; `gofumpt -l` clean; `go vet` clean.
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, golden, or `go.mod` change; core mode byte-identical to 24.5. No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path; `go.mod` unchanged) and no security-review trigger. **Design decisions:** (1) `New` stays single-return per the blessed PRD example — provisioning error rides `jsErr`, surfaced at first use, not swallowed; (2) used the modern `nats.go/jetstream` package (non-deprecated; 24.8–24.10 build on it) over the legacy `JetStreamContext`; (3) `Subscribe` gets the `jsErr` guard now but stays core-subscribe — durable JetStream consumers are 24.8 (D2). `Nats-Msg-Id` set only on the JS branch (core mode unchanged, no client-side dedup per §8).

---

## 24.8 JetStream durable consumers + explicit ack (EB-2.2, D2)

**PRD Reference:** Section 28.7.1 (At-least-once delivery — `nil` acks, non-nil error naks for redelivery with backoff), 28.5 (Handler-error contract, honored here). Design §2-D2, §7 EB-2.2.

**Status:** Complete (2026-07-31)

**Depends on:** 24.7.

**Governing decisions:** D2 **takes effect here** — this is where "Handler error = redeliver" becomes a real redelivery primitive. Durable-consumer knobs are natsbus-local (D3). **Spec amendment (2026-07-31):** the design/PRD text put the knobs on a *variadic added to `Subscribe`*; that is impossible in Go — a variadic parameter changes the method signature, so `*Bus` would stop satisfying `event.Subscriber` (verified empirically: `have Subscribe(…, ...Opt) / want Subscribe(…)`), which would break the shipped `cache.FromEventSubscriber(sub event.Subscriber)` wiring and this sub-item's own interface-narrowing test. **Resolution (approved by user):** the knobs ride a **separate concrete method** `*Bus.SubscribeWith(opts, handler, ...SubscribeOption)`; `Subscribe(opts, handler)` stays frozen and interface-satisfying. PRD §28.7.1 amended to match.

### Tasks

- [x] Add natsbus-local subscribe options: `WithDurable(name)` + `WithBackoff(d)` (the "ack-policy plumbing" needed for "Nak with backoff") as a new `SubscribeOption func(*subscribeConfig)` type, on the concrete `*Bus.SubscribeWith(opts, handler, ...SubscribeOption)` (**not** a variadic on `Subscribe` — see the spec amendment above). `subscribeConfig` is the extension point 24.9/24.10 build on.
- [x] JetStream subscribe path: `subscribeJetStream` creates/binds a durable consumer (`js.CreateOrUpdateConsumer`, `AckExplicitPolicy`, `FilterSubject` = the same subject a core `Subscribe` would use) and drives it via `cons.Consume(jsMsgHandler)`; on handler `nil` → `msg.Ack()`; on non-nil error → `msg.NakWithDelay(backoff)` (or `msg.Nak()` when no backoff) for redelivery. Undecodable body → `msg.Term()`; filtered-out (action/multi-table) → `msg.Ack()`. Error also surfaced to `WithSubscribeErrorHandler` when set (per-attempt observability).
- [x] Ensure `event.SubscribeOptions` (Tables/Actions/Group) still map correctly: single-table narrows the consumer `FilterSubject` broker-side; multi-table/action filters ride the per-message `matches`; `Group` maps to the durable name when `WithDurable` is absent (load-balanced consumer). No JetStream vocabulary added to the shared type (D3).
- [x] Confirm a caller holding the value as `event.Subscriber` still gets core-filter semantics: `Subscribe` keeps its exact interface signature; pinned by compile-time `var _ event.Subscriber = (*Bus)(nil)` (+ `event.Publisher`) and the runtime `TestSubscribeInterfaceNarrowing`. `SubscribeWith` in core mode (no `WithJetStream`) degrades to the core tap — the durable knobs are inert.

### Acceptance Criteria

- A durable JetStream consumer redelivers a message whose handler returned an error (at-least-once); a handler returning nil acks and is not redelivered. ✓ `TestJetStreamRedeliversOnError` / `TestJetStreamAcksOnSuccess`.
- Redelivery uses the configured backoff. ✓ `WithBackoff(d)` → `msg.NakWithDelay(d)`.
- `event.SubscribeOptions` is unchanged; durable/ack knobs live only on natsbus's concrete `SubscribeWith` method (D3) — code depending on `event.Subscriber` compiles and gets core behavior. ✓ (spec amended from "variadic on Subscribe" to a separate method — the original was not compilable).
- Handlers are documented/expected to be idempotent under at-least-once (§28.5). ✓ documented on `SubscribeWith`.

### Tests Required

- [x] (Integration) Handler returns error → message is redelivered; a later success acks and stops redelivery. `TestJetStreamRedeliversOnError` — handler fails 2× (each naks), succeeds on the 3rd (acks); asserts exactly `failCount+1` invocations and no further redelivery after the settle window. `testing.Short()`-skippable.
- [x] `nil` return acks immediately (no redelivery). `TestJetStreamAcksOnSuccess` — durable consumer, handler returns nil; asserts exactly one delivery after the settle window. `testing.Short()`-skippable.
- [x] Interface-narrowing: a `var s event.Subscriber = bus` call gets core-filter semantics; `*Bus` gets the durable knobs (compile + behavior). `TestSubscribeInterfaceNarrowing` (core mode) — `var narrowed event.Subscriber = bus` compiles and delivers via `Subscribe`; `bus.SubscribeWith(..., WithDurable(...))` compiles and delivers (knobs inert in core). Backed by the package-level compile assertions.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — new `SubscribeOption func(*subscribeConfig)` type + `subscribeConfig{durable, backoff}` + `WithDurable(name)` / `WithBackoff(d)` Options; package-level `var _ event.Publisher/Subscriber = (*Bus)(nil)` assertions; `subscription` gains a `jsConsume jetstream.ConsumeContext` field (exactly one of `natsSub`/`jsConsume` non-nil); `Unsubscribe`/`Close` stop the consume context for JS subscriptions (durable server-side state intentionally preserved); `Subscribe` refactored to delegate to a shared `subscribeCore` (via `subscribeGuard` + `trackSub`); new concrete `SubscribeWith(opts, handler, ...SubscribeOption)` → core fallback in core mode, else `subscribeJetStream` (durable consumer, `AckExplicitPolicy`, `FilterSubject`) + `jsMsgHandler` (decode → filter → propagator ctx → `Ack`/`Nak(WithDelay)`/`Term`, error routed to `subErrHandler`). Package doc updated.
  - `event/natsbus/natsbus_test.go` — `TestJetStreamRedeliversOnError`, `TestJetStreamAcksOnSuccess` (both `testing.Short()`-skippable, reuse `startJetStreamServer`), `TestSubscribeInterfaceNarrowing` (core-mode compile + behavior).
  - `docs/PRD.md` §28.7.1 + PRD §28 — amended "variadic on `Subscribe`" → separate `SubscribeWith` method, with the Go-semantics rationale.
- **Tests:** `go test -race ./...` in natsbus green; the 3 new tests pass; `go test -short` skips all 6 JetStream integration tests (24.7's 4 + 24.8's 2); `make check` green all modules; `gofumpt -l` clean; `golangci-lint` 0 issues.
- **Notes:** Purely natsbus-local + doc amendment — **no** `event`-pkg (interface frozen), generator, golden, or `go.mod` change. No E2E trigger (changed code paths `event/natsbus/**` match no glob) and no security-review trigger. **Deviation from blessed spec (approved):** durable knobs on `SubscribeWith`, not a variadic `Subscribe` — the literal spec was not compilable in Go; PRD + design amended. `WithBackoff` added as the minimal "ack-policy plumbing" the acceptance criterion "redelivery uses the configured backoff" requires (not enumerated in the design's option list, but within the task's "and any needed ack-policy plumbing"). In JetStream mode the plain `Subscribe` stays a core real-time tap (at-most-once), matching the interface contract; durable at-least-once is opt-in via `SubscribeWith`. Retry is not the core `WithRetry` in JS mode — broker redelivery supersedes it (D2). `WithMaxDeliver`/`WithDeadLetter` (24.9) and delivery-start (24.10) extend `subscribeConfig`. **Deferred coverage (auto-review):** the `jsMsgHandler` `Term()` (undecodable body) and filtered-out `Ack()` branches have no direct test here — carried to 24.9 as a coverage backfill (both live on the durable path 24.9 extends). **FIX-131 (auto-review, resolved inline):** the pre-existing `Close`-outside-lock race on the subscription handles (predates 24.8; widened by the new `jsConsume` field) was fixed here — `Close` now detaches handles under `b.mu` before the off-lock `Stop`/`Unsubscribe`, with `TestCloseUnsubscribeConcurrentNoRace` as the `-race` guard.

---

## 24.9 JetStream `WithMaxDeliver` + `WithDeadLetter` → DLQ (EB-2.3)

**PRD Reference:** Section 28.7.1 (After the consumer's maximum delivery attempts the message routes to a dead-letter subject). Design §7 EB-2.3.

**Status:** Complete (2026-07-31)

**Depends on:** 24.8.

**Governing decisions:** natsbus-local subscribe options (D3).

### Tasks

- [x] Add `natsbus.WithMaxDeliver(n)` and `natsbus.WithDeadLetter(subject)` subscribe options. Both added as `SubscribeOption` funcs setting new `subscribeConfig.maxDeliver`/`deadLetter` fields; documented as no-ops in core mode. `n <= 0` leaves delivery unbounded; `WithDeadLetter` requires a positive `WithMaxDeliver` to define "maximum deliveries".
- [x] Wire max-deliver into the durable consumer config; after `n` failed deliveries route the message to the dead-letter subject. `subscribeJetStream` sets `ConsumerConfig.MaxDeliver = cfg.maxDeliver` when `> 0` (broker caps redelivery — satisfies the "no DLQ, no crash" default). DLQ routing is **explicit republish** (design's blessed idiomatic pattern): a new `nakOrDeadLetter` helper checks `isFinalDelivery` (via `msg.Metadata().NumDelivered >= maxDeliver`) and, on the final failed delivery, republishes to the dead-letter subject then `Term()`s the stream message; otherwise it `Nak`s (with `WithBackoff` delay) for another attempt. If the DLQ publish fails, the message is `Nak`'d instead so it is not lost.
- [x] Confirm the DLQ message preserves the envelope (and headers, so `Event.ID`/`traceparent` survive) for downstream inspection. `publishDeadLetter` republishes `msg.Data()` + `msg.Headers()` verbatim over **core** NATS (so the DLQ subject need not fall under the stream's captured subjects); pinned by `TestJetStreamDeadLetterAfterMaxDeliver` asserting the `Nats-Msg-Id` (= `Event.ID`), `traceparent` header, and `cmp.Diff` on the decoded body.
- [x] **(Carried from 24.8 review — coverage backfill.)** Added regression tests for the two `jsMsgHandler` terminal-ack branches 24.8 left untested: `TestJetStreamUndecodableBodyTerminated` (`Term()`) and `TestJetStreamFilteredOutAcked` (`Ack()`).

### Acceptance Criteria

- A message whose handler fails is redelivered up to `WithMaxDeliver(n)` times, then routed to the `WithDeadLetter` subject. ✓ `TestJetStreamDeadLetterAfterMaxDeliver`.
- The DLQ message retains the original envelope + headers. ✓ same test — `Nats-Msg-Id`/`traceparent` headers + body `cmp.Diff`.
- Without `WithDeadLetter`, exceeding max-deliver follows JetStream's default terminal behavior (no crash). ✓ `ConsumerConfig.MaxDeliver` caps deliveries broker-side with no DLQ routing (no client-side Term); `nakOrDeadLetter` naks when `deadLetter == ""`. Pinned by `TestJetStreamMaxDeliverWithoutDeadLetter`.
- (Backfill) An undecodable body is terminated and a filtered-out message is acked — neither is redelivered in a loop. ✓ `TestJetStreamUndecodableBodyTerminated` / `TestJetStreamFilteredOutAcked`.

### Tests Required

- [x] (Integration) Handler always errors → exactly `n` deliveries then a message on the DLQ subject. `TestJetStreamDeadLetterAfterMaxDeliver` — handler always fails; asserts exactly `maxDeliver` (3) invocations, exactly one message on the core-subscribed `sqlgen.dlq` subject, and no extra DLQ messages after the settle window. `testing.Short()`-skippable.
- [x] DLQ payload/headers match the original event. Same `TestJetStreamDeadLetterAfterMaxDeliver` — the routed DLQ `*nats.Msg` carries `Nats-Msg-Id == Event.ID` and the original `traceparent` header, and its body `cmp.Diff`s the published event (the two Tests-Required bullets are asserted in one test to share the durable-consumer setup, per the 24.7 precedent).
- [x] **(Backfill from 24.8)** Undecodable-body path: `TestJetStreamUndecodableBodyTerminated` — a malformed JSON body published to a stream-captured subject is `Term()`'d (handler never invoked for it), while a valid event published afterward is still delivered exactly once. The terminal action is pinned **semantically** (not just line-covered) via `assertConsumerSettled` — a `Nak` loop would leave `NumAckPending`/`NumRedelivered` non-zero; `Term` leaves both at zero. `testing.Short()`-skippable.
- [x] **(Backfill from 24.8)** Filtered-out path: `TestJetStreamFilteredOutAcked` — a durable subscription with an `Actions: [Create]` filter (wildcard `FilterSubject`, so both events reach the consumer) `Ack()`s a non-matching `Update` (handler not called, not redelivered) while delivering the matching `Create`. Same `assertConsumerSettled` probe distinguishes the `Ack` from a `Nak` loop. `testing.Short()`-skippable.
- [x] **(Added — criterion #3)** No-DLQ exhaustion: `TestJetStreamMaxDeliverWithoutDeadLetter` — `WithMaxDeliver(2)` with **no** `WithDeadLetter` and an always-failing handler delivers exactly 2 times then stops (broker cap), with no DLQ routing, no crash, no unbounded redelivery. `testing.Short()`-skippable.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — `subscribeConfig` gains `maxDeliver int` + `deadLetter string`; new `WithMaxDeliver(n)` / `WithDeadLetter(subject)` `SubscribeOption`s (documented as core-mode no-ops, `WithDeadLetter` requiring a positive max); `subscribeJetStream` sets `ConsumerConfig.MaxDeliver` when `> 0`; `jsMsgHandler`'s error branch now surfaces to `subErrHandler` **then** delegates the terminal action to the new `nakOrDeadLetter` helper (DLQ republish + `Term()` on the final permitted delivery, else `Nak`/`NakWithDelay`); new `isFinalDelivery` (`NumDelivered >= maxDeliver`, guarded `maxDeliver <= 0 → false`, missing metadata → not final; reasoned `//nolint:gosec` on the guarded `int→uint64`) and `publishDeadLetter` (core republish of `Data()` + `Headers()`). Package/handler docs updated.
  - `event/natsbus/natsbus_test.go` — `TestJetStreamDeadLetterAfterMaxDeliver` (DLQ routing + payload/header preservation), `TestJetStreamUndecodableBodyTerminated` (24.8 `Term()` backfill), `TestJetStreamFilteredOutAcked` (24.8 `Ack()` backfill), `TestJetStreamMaxDeliverWithoutDeadLetter` (criterion-#3 bounded exhaustion), and a shared `assertConsumerSettled` helper that probes `NumAckPending`/`NumRedelivered` to pin `Term`/`Ack` over a `Nak` loop; all `testing.Short()`-skippable, reusing `startJetStreamServer`.
- **Tests:** `go test ./...` in natsbus green; `go test -short` skips all JetStream integration tests (24.7's 4 + 24.8's 2 + 24.9's 4); `make check` **green** across all modules (fmt + lint + vet + unit); `golangci-lint` 0 issues; `gofumpt -l` clean.
- **Auto-review:** `sqlgen-reviewer` returned 0 blocking issues (PRD 3/4 PASS + 1 PARTIAL, guard + `//nolint:gosec` + interface invariants all confirmed correct). The two PARTIALs — backfill tests under-asserting the terminal action, and the untested no-DLQ exhaustion criterion — were **closed in this same sub-item** (the `assertConsumerSettled` probe + `TestJetStreamMaxDeliverWithoutDeadLetter`), not deferred to a FIX. Two low-severity edge notes acknowledged and left as-is: (1) `WithDeadLetter` without a positive `WithMaxDeliver` is a documented no-op (unbounded redelivery) — not a new hazard, it is identical to 24.8's default no-max behavior, so no enforcement added; (2) if the DLQ publish or metadata read fails on the final delivery the message is nak'd but the broker cap prevents redelivery, so it is stranded-in-stream (recoverable via replay), not lost — a failure-path corner acceptable for a fire-and-forget core republish.
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, golden, or `go.mod` change; core mode and JetStream `Subscribe`/`SubscribeWith` (no DLQ opts) byte-identical to 24.8. No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path) and no security-review trigger. **Design decision:** DLQ is client-side explicit republish over **core** NATS (envelope + headers verbatim), not a JetStream advisory subscription — the design's "explicit republish, per the transport's idiomatic pattern" and it keeps the DLQ subject free of any stream-coverage requirement. The consumer's `MaxDeliver` is still set as the broker-side ceiling so the "no `WithDeadLetter` → default terminal, no crash" criterion holds without client bookkeeping. On the final delivery both fire consistently: the client `Term()`s (routing to DLQ) exactly as the broker's `MaxDeliver` cap would otherwise strand the message.

---

## 24.10 JetStream replay / backfill (EB-2.4)

**PRD Reference:** Section 28.7.1 (Replay / backfill — a newly added durable consumer rebuilds its projection from stream history via `DeliverAll`, a start time, or a start sequence). Design §7 EB-2.4.

**Status:** Complete (2026-07-31)

**Depends on:** 24.7 (a persisted stream must exist).

**Governing decisions:** natsbus-local subscribe options (D3).

### Tasks

- [x] Add delivery-start subscribe options: `WithDeliverAll()` plus start-time and start-sequence variants. Added three `SubscribeOption`s — `WithDeliverAll()`, `WithDeliverFromTime(t time.Time)`, `WithDeliverFromSequence(seq uint64)` — setting new `subscribeConfig.deliverPolicy`/`optStartTime`/`optStartSeq` fields. Named per design D3's `DeliverFrom` family token, split into `...Time`/`...Sequence` for the two start-point kinds. Documented as core-mode no-ops.
- [x] Wire them into the durable-consumer creation so a fresh consumer replays stream history from the chosen start point. `subscribeJetStream`'s `ConsumerConfig` now carries `DeliverPolicy`/`OptStartTime`/`OptStartSeq` from `cfg`. The zero value of `jetstream.DeliverPolicy` **is** `DeliverAllPolicy`, so an un-set config (every pre-24.10 `SubscribeWith` call) is **byte-identical** to the prior consumer — `WithDeliverAll()` makes that default explicit; the two `WithDeliverFrom…` options switch to `DeliverByStartTimePolicy`/`DeliverByStartSequencePolicy` with the matching `OptStart…` field.
- [x] Confirm replay interoperates with 24.8's ack/redelivery (replayed messages ack/nak normally) and 24.7's dedup (replay is distinct from duplicate publish). Replay only sets the consumer's start cursor; the `jsMsgHandler` ack/nak/Term/DLQ path is unchanged, so replayed messages ack/nak exactly like live ones (the three tests all ack their replayed deliveries). Dedup is a **publish-time** `Nats-Msg-Id` concern (24.7) orthogonal to a consumer's start policy — replay redelivers already-stored, already-deduped messages; no code interaction.

### Acceptance Criteria

- A newly created durable consumer with `WithDeliverAll` receives all retained stream history (bounded by `MaxAge`); start-time / start-sequence variants begin at the specified point. ✓ `TestJetStreamDeliverAllReplaysHistory` (5 pre-published events, fresh consumer gets all 5), `TestJetStreamDeliverFromSequence` (starts at seq 3 of 4), `TestJetStreamDeliverFromTime` (starts at a cutoff between two batches).
- Replayed messages participate in normal ack/redelivery. ✓ all three tests ack their replayed deliveries through the unchanged `jsMsgHandler` path; no separate replay ack path exists.

### Tests Required

- [x] (Integration) Publish N events, then attach a fresh `WithDeliverAll` consumer → it receives all N (replay onto a fresh durable consumer). `TestJetStreamDeliverAllReplaysHistory` — publishes 5 events **before** subscribing, then a fresh `WithDurable("replayer")` + `WithDeliverAll()` consumer receives all 5 (sorted `cmp.Diff`). `testing.Short()`-skippable.
- [x] Start-sequence / start-time variant begins at the expected offset. `TestJetStreamDeliverFromSequence` — 4 events → stream seqs 1..4; `WithDeliverFromSequence(3)` replays exactly `{3,4}`. `TestJetStreamDeliverFromTime` — old batch, a recorded `cutoff` between 100ms gaps, new batch; `WithDeliverFromTime(cutoff)` replays exactly the new batch. Both `testing.Short()`-skippable; shared `collectIDs` helper.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — `subscribeConfig` gains `deliverPolicy jetstream.DeliverPolicy` + `optStartTime *time.Time` + `optStartSeq uint64` (zero value = `DeliverAllPolicy`, so additive/byte-identical default); new `WithDeliverAll()` / `WithDeliverFromTime(t)` / `WithDeliverFromSequence(seq)` `SubscribeOption`s (documented as core-mode no-ops); `subscribeJetStream`'s `ConsumerConfig` now sets `DeliverPolicy`/`OptStartTime`/`OptStartSeq` from `cfg`. No change to the `jsMsgHandler` ack/nak/DLQ path — replay is purely a consumer start-cursor concern.
  - `event/natsbus/natsbus_test.go` — `TestJetStreamDeliverAllReplaysHistory`, `TestJetStreamDeliverFromSequence`, `TestJetStreamDeliverFromTime` + shared `collectIDs` helper; added `sort` import; all three `testing.Short()`-skippable, reusing `startJetStreamServer`.
- **Tests:** `go test ./...` in natsbus green; `go test -short` skips all JetStream integration tests (now 24.7's 4 + 24.8's 2 + 24.9's 5 + 24.10's 3); `make check` **green** across all modules; `golangci-lint` 0 issues; `gofumpt -l` clean.
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, golden, or `go.mod` change; the un-set delivery-start config is byte-identical to 24.8/24.9 (`DeliverAllPolicy` is the `jetstream.DeliverPolicy` zero value). No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path) and no security-review trigger. **Design decision:** the D3 `DeliverFrom` family is split into `WithDeliverFromTime`/`WithDeliverFromSequence` for self-documenting call sites (vs one overloaded option), matching the PRD's "a start time, or a start sequence" wording. Replay/dedup are orthogonal (consumer start cursor vs publish-time `Nats-Msg-Id`); replay/ack interop needs no new code because the unchanged `jsMsgHandler` handles replayed and live messages identically. This completes the EB Phase 2 durability headline (24.7–24.10).

---

## 24.11 Convenience & niceties — Connect / ActionSubjects / Codec (EB-3.1, EB-3.2, EB-3.3)

**PRD Reference:** Section 28.7.1 (`natsbus.Connect(url, ...ConnectOption)` blessed prod defaults; `WithActionSubjects()` — `{prefix}.{schema}.{table}.{action}`, bumps envelope version; `WithCodec(codec)` — non-JSON body). Design §3, §7 EB-3.1/3.2/3.3. PRD §28.7 JetStream subsection already normative (EB-2.5 blessed).

**Status:** Complete (2026-07-31)

**Depends on:** none (independent; slot anytime). `WithActionSubjects` bumps the 24.4 envelope version.

**Governing decisions:** low-priority sugar (design §3). `WithCodec` ships **only if** a concrete proto/msgpack need surfaces — otherwise dropped, not built speculatively.

### Tasks

- [x] `natsbus.Connect(url, ...ConnectOption)` — blessed constructor dialing with production-sane defaults (infinite reconnect, reconnect buffer so publishes during a blip are not lost, drain-on-close, disconnect/reconnect callbacks); returns a bus ready to pair with `WithJetStream`. `New(conn)` remains for callers who own their `*nats.Conn`. Implemented: `Connect(url, ...ConnectOption) (*Bus, error)` dials with `MaxReconnects(-1)` + `ReconnectBufSize(8 MiB)` + `DrainTimeout(30s)` + disconnect/reconnect `log.Printf` callbacks, then `New(conn, opts...)`. **`type ConnectOption = Option`** (alias) — the only way the blessed "ready to pair with `WithJetStream`" holds is for the variadic to accept `WithJetStream` (an `Option`); a distinct type that couldn't carry it would contradict the sentence introducing it. Connection resilience defaults are baked in (not customizable via `Connect`) — a caller needing custom dial options uses `New` with a self-dialed conn, exactly the case the PRD preserves `New(conn)` for. `Close`'s existing `conn.Drain()` handles drain-on-close for the Connect-owned conn.
- [x] `natsbus.WithActionSubjects()` — publish to `{prefix}.{schema}.{table}.{action}` for broker-side action filtering; this changes the subject scheme, so it is opt-in and **bumps `Sqlgen-Envelope-Version`** (24.4). Added `WithActionSubjects() Option` + `Bus.actionSubjects`; `publishSubject` appends `.{action}`; `envelopeVersion()` returns the new `envelopeVersionActionSubjects = "2"` when on; `subscribeSubject` derives the matching scheme (single table + single action → `{prefix}.*.{table}.{action}` broker-side filter; single table other → `{prefix}.*.{table}.*`; multi-table → `{prefix}.>`). JSON body unchanged.
- [x] `natsbus.WithCodec(codec)` — pluggable body encoding (proto/msgpack). Implement **only** if a real need is confirmed; otherwise record as dropped per design §8/§3. If built, JSON stays the default. **Dropped (recorded)** per the governing decision — no concrete proto/msgpack need has surfaced, so it is not built speculatively. JSON remains the sole (and default) body encoding. Revisit only when a real non-JSON requirement appears.
- [x] Confirm PRD §28.7 JetStream subsection is normative and matches the shipped surface (EB-2.5) — checkpoint, already blessed. Confirmed: §28.7.1 documents core/JetStream modes, `SubscribeWith` + durable knobs, trace propagation, wire headers, and the "Additional opt-in options" (`Connect`, `WithSubscribeErrorHandler`/`WithRetry`, `WithActionSubjects`, `WithCodec`) — all matching the shipped surface (with `WithCodec` documented as the "where a non-JSON wire format is required" opt-in, consistent with dropping it until needed).

### Acceptance Criteria

- `Connect(url, ...)` returns a working bus with the documented resilience defaults; `New(conn)` is unchanged.
- `WithActionSubjects()` publishes to the action-qualified subject and bumps the envelope version; a consumer can filter by action at the broker.
- `WithCodec` (if shipped) round-trips a non-JSON body with JSON remaining the default; if dropped, the decision is recorded.

### Tests Required

- [x] `Connect` applies the resilience defaults (reconnect/buffer/drain assertions where testable; at minimum a construction + publish smoke test). `TestConnectPublishSubscribe` — Connect-built bus publishes and delivers an event end to end and `Close` (draining the owned conn) succeeds; `TestConnectDialFailure` — an unreachable URL returns a dial error (not swallowed); `TestConnectPairsWithJetStream` (`testing.Short()`-skippable) — `Connect(url, WithJetStream(...))` provisions the stream and persists a published event, pinning the "ready to pair with `WithJetStream`" claim. (Reconnect/buffer/drain values are set via `nats.Option`s that the embedded server can't force-exercise without a flaky disconnect/restart; the smoke + JetStream-pairing tests cover the observable contract.)
- [x] `WithActionSubjects()` publishes to `{prefix}.{schema}.{table}.{action}` and stamps a bumped envelope version; broker-side action filter delivers only matching actions. `TestActionSubjectsPublish` — raw `ChanSubscribe` on `sqlgen.events.public.tasks.create` asserts the subject, the literal `Sqlgen-Envelope-Version: 2` header, and a `cmp.Diff` on the unchanged JSON body; `TestActionSubjectsBrokerFilter` — a single-table + single-action subscription (subject narrows to `...tasks.create`) delivers only the `create`, never the `update` published to `...tasks.update`.
- [x] `WithCodec` round-trip (only if implemented). **N/A** — `WithCodec` dropped (recorded above); no test needed.

### Completion Record

- **Date:** 2026-07-31
- **Files changed:**
  - `event/natsbus/natsbus.go` — added `"log"` import; connection-resilience default consts (`defaultReconnectBufSize = 8 MiB`, `defaultDrainTimeout = 30s`); a second envelope-version const (`envelopeVersionActionSubjects = "2"`); `type ConnectOption = Option` + `Connect(url, ...ConnectOption) (*Bus, error)` + `connectDialOptions()` (infinite reconnect, reconnect buffer, drain timeout, disconnect/reconnect `log.Printf` callbacks); `WithActionSubjects() Option` + `Bus.actionSubjects` field; new `publishSubject`/`envelopeVersion()` helpers; `Publish` now uses them; `subscribeSubject` made action-aware and its signature changed from `(tables []string)` to `(opts event.SubscribeOptions)` (both call sites updated). **No** `go.mod` change (`nats.go`/`jetstream` already required; `log` is stdlib).
  - `event/natsbus/natsbus_test.go` — `startServerURL`/`startJetStreamServerURL` helpers; `TestConnectPublishSubscribe`, `TestConnectDialFailure`, `TestConnectPairsWithJetStream` (short-skippable), `TestActionSubjectsPublish`, `TestActionSubjectsBrokerFilter`.
- **Tests:** `go test ./...` in natsbus green (5.1s; short-skippable JS pairing test runs in-process); `gofumpt -l` clean; `make check` **green** across all modules (fmt + lint + vet + `-short -race` unit — cmd/sqlgen/cli, gen, parser, event/natsbus, cache, metrics all `ok`).
- **Notes:** Purely natsbus-local and additive — **no** `event`-pkg, generator, golden, or `go.mod` change; `New(conn)` and the default (non-action) publish/subscribe paths are byte-identical to 24.10 (default `envelopeVersion()` = "1", `publishSubject` = the old `subject`, `subscribeSubject(opts)` returns the old strings for the non-action path). No E2E trigger (changed paths `event/natsbus/**` match no glob — the runtime-package globs list `database/event/**`, a different path) and no security-review trigger (not `manifest/` embed nor `cmd/sqlgen/cli/**`). **Design decisions:** (1) `ConnectOption` is a type alias for `Option` — faithful to the blessed name while making `Connect(url, WithJetStream(cfg))` compile, which is the only reading under which "ready to pair with `WithJetStream`" holds; connection-dial customization is deliberately not exposed (use `New` with a self-dialed conn, per the PRD's own `New(conn)` carve-out) rather than inventing `WithConnectOptions`/`WithBusOptions` surface. (2) The disconnect/reconnect callbacks log via stdlib `log.Printf`, matching `event.DefaultOnError`'s established default sink, rather than no-op callbacks (meaningless) or an imposed structured logger. (3) `WithActionSubjects` bumps the envelope version to "2" only for buses with the option on; the schema-empty single-table subscribe edge inherits the pre-existing "single-table subscribe assumes a schema token" behavior (generated events always carry a schema). (4) `WithCodec` dropped per the governing decision — no proto/msgpack need surfaced; JSON stays the sole body encoding. This completes EB Phase 3 and Phase 24.
