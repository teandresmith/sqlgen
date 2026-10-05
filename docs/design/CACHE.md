# SQLGen Cache — Implementation Design

> **Scope:** Implementation-level supplement to `docs/PRD.md` §27. This document
> holds what the PRD should NOT: internal helper signatures, circuit-breaker
> state-transition tables, fingerprint algorithms, generator pseudocode,
> per-file testing strategy, design-decision rationale, and the resolved-gaps
> trail.
>
> **Public contract (what consumers read):** see `docs/PRD.md` §27. That is
> the authoritative spec for `Backend`, `Serializer`, `MetricsRecorder`,
> `InvalidationSource`, `Cache` facade methods, YAML config schema, cache
> invariant, operation behavior, and OTel instruments.
>
> This doc assumes familiarity with PRD §27. Sections below cross-reference PRD
> only where the two diverge (implementation internals) or where CACHE.md adds
> detail beyond the spec (decision history, pseudocode, fingerprint algorithm,
> breaker state table).

---

## 1. Overview

See PRD §27.1 for the user-facing overview. Implementation aspects tracked
here but not in the PRD:

- Runtime module is **stdlib + `golang.org/x/sync` only** — `singleflight` for
  read-through stampede protection (§9.3), `errgroup` already used by generated
  relationship loading.
- First-party backends (`cache/memory/`, `cache/redis/`) and the optional
  `cache/msgpack/` serializer ship as **separate Go modules** so the runtime
  stays thin. See §4 for module layout.
- `metrics/otel/` is a separate module for the same reason.

---

## 2. Current State

**In place (wired during earlier phases):**

| Piece | Source | Status |
|---|---|---|
| `CallOptions.SkipCache` | Phase 7 | Generated into `shared_types_gen.go`; `SkipHooks` implies `SkipCache`. |
| `MutationContext.AffectedPKs` | Phase 11.3 | Populated by every terminal mutation — one element per affected entity: the scalar PK for single-PK tables (e.g. `uuid.UUID`), the generated `XXXPK` struct value for composite-PK tables (e.g. `OrderItemPK{OrderID, ProductID}`). Element shape matches §9.2's `Cache.Invalidate*` contract exactly, so the generated mutation hook feeds `AffectedPKs` straight into `InvalidateMany` dispatch without any pack step. Verified in `update_products_gen.go:78` (scalar) and `update_order_items_gen.go:65,161` (struct-value). |
| `QueryContext` | `hook/hook.go` | Carries `Op`, `Table`, `Schema`, `PK`, `Input`, `CallOptions`. Sufficient for read-through + hydration decisions. **No changes needed.** |
| `event/` package | Phase 11 | `cache.FromEventSubscriber` can consume `event.Subscriber` directly. |
| `metrics/otel/` stub | pre-phase-12 | `doc.go` placeholder. Implementation lands in Phase 12. |

**Missing (this document designs these):**

- `cache/` runtime package (does not exist).
- Config: `CacheConfig`, `HydrationConfig`, `CircuitBreakerConfig`, `TableCacheConfig`, `ViewCacheConfig`, `Serializer` enum.
- Config wiring: `RootConfig.Cache`, `TableConfig.Cache`, `ViewConfig.Cache`, `ViewConfig.InvalidateOn`. Current `ViewConfig` is only `{StructName, SQL}`.
- First-party backends: `cache/memory/`, `cache/redis/`, `cache/msgpack/`.
- `metrics/otel/` `MetricsRecorder` implementation.
- Generator: `cache_gen.go` template + context builder.
- `docs/tracker/phase-12.md` (produced by `/phase 12`).

---

## 3. Configuration

YAML schema: see PRD §27.2. This section holds the Go struct shapes
(`cmd/sqlgen/config/config.go`), resolver contracts, and validation rules
that the generator and parser enforce.

### 3.2 Config Types (Go)

Location: `cmd/sqlgen/config/config.go`.

```go
// Root-level additions
type RootConfig struct {
    // ... existing fields ...
    Cache *CacheConfig `yaml:"cache"`
}

type CacheConfig struct {
    Enabled        bool                   `yaml:"enabled"`
    Version        int                    `yaml:"version"`          // optional kill switch — feeds every table's fingerprint (§10.2)
    TTL            string                 `yaml:"ttl"`              // Go duration (e.g., "1h")
    Serializer     Serializer             `yaml:"serializer"`       // json | msgpack | custom
    KeyPrefix      string                 `yaml:"key_prefix"`
    Hydration      *HydrationConfig       `yaml:"hydration"`
    CircuitBreaker *CircuitBreakerConfig  `yaml:"circuit_breaker"`
}

type HydrationConfig struct {
    Enabled bool   `yaml:"enabled"`
    Timeout string `yaml:"timeout"`  // Go duration
}

type CircuitBreakerConfig struct {
    Enabled           bool   `yaml:"enabled"`
    FailureThreshold  int    `yaml:"failure_threshold"`
    ProbeInterval     string `yaml:"probe_interval"`
    HalfOpenMaxProbes int    `yaml:"half_open_max_probes"`
}

type Serializer string

const (
    SerializerJSON    Serializer = "json"
    SerializerMsgpack Serializer = "msgpack"
    SerializerCustom  Serializer = "custom"
)

// Table-level (follows the Events pattern — tri-state pointers for overrides)
type TableConfig struct {
    // ... existing fields ...
    Cache *TableCacheConfig `yaml:"cache"`
}

type TableCacheConfig struct {
    Enabled    *bool       `yaml:"enabled"`    // nil = inherit global
    TTL        *string     `yaml:"ttl"`        // nil = inherit global
    Serializer *Serializer `yaml:"serializer"` // nil = inherit global
}

// View-level — currently ViewConfig is {StructName, SQL}, gains two fields
type ViewConfig struct {
    StructName   string           `yaml:"struct_name"`
    SQL          string           `yaml:"sql"`
    Cache        *ViewCacheConfig `yaml:"cache"`
    InvalidateOn []string         `yaml:"invalidate_on"`
}

type ViewCacheConfig struct {
    Enabled    *bool       `yaml:"enabled"`
    TTL        *string     `yaml:"ttl"`
    Serializer *Serializer `yaml:"serializer"`
}
```

### 3.3 Resolution Rules

Resolver helpers mirror the `ResolveTableEventsEnabled` pattern:

```go
func ResolveTableCacheEnabled(table TableConfig, global *CacheConfig) bool
func ResolveTableCacheTTL(table TableConfig, global *CacheConfig) time.Duration
func ResolveTableCacheSerializer(table TableConfig, global *CacheConfig) Serializer
func ResolveViewCacheEnabled(view ViewConfig, global *CacheConfig) bool
func ResolveViewCacheTTL(view ViewConfig, global *CacheConfig) time.Duration
func ResolveViewCacheSerializer(view ViewConfig, global *CacheConfig) Serializer
```

**Table precedence:** per-table override → global → built-in default. A per-table
`enabled: true` overrides global `enabled: false` (opt-in per table even when globally off).

**View precedence (different — opt-in only):** view caching is never inherited from
the global `cache.enabled` setting. `ResolveViewCacheEnabled` returns `true` only
when `view.cache.enabled` is explicitly set to `true`. Global `ttl`, `serializer`,
`hydration`, and `circuit_breaker` still apply to enabled views.

*Rationale:* tables have automatic mutation-hook invalidation, so inheriting
`enabled: true` from the global setting is safe. Views have no mutations — their
only invalidation is `invalidate_on`. Inheriting the global flag would silently
create TTL-only view caches when a user forgets to set `invalidate_on`, producing
stale data bounded only by TTL. Requiring explicit opt-in eliminates the footgun.

### 3.4 Validation

At YAML parse time (before the schema is loaded):

- `cache.ttl` parses as `time.Duration` if set. Empty defaults to `1h`.
- `cache.serializer` ∈ {`json`, `msgpack`, `custom`, ``}. Empty defaults to `json`.
- `cache.key_prefix` is optional. Empty or unset defaults to `"sqlgen"` — no hard error, no leading-colon edge case. The default is baked into the generated facade at codegen time, so the grammar in §10 always holds. Users who explicitly want a non-default prefix (e.g. multi-tenant Redis namespaces) set a non-empty string; the value is used verbatim without additional validation beyond "must be a string."

  When the default is applied, `sqlgen generate` emits a stderr warning: `sqlgen: cache.key_prefix unset — using default "sqlgen". Set cache.key_prefix explicitly to silence this warning.` The message surfaces the choice so multi-tenant deployments and Redis-namespace-sensitive setups don't silently inherit a default that collides across environments. An explicit empty string (`key_prefix: ""`) triggers the same warning, since it resolves to the same default. Setting any non-empty string suppresses the warning.
- `cache.hydration.timeout` parses as `time.Duration`. Empty defaults to `30s`.
- `cache.circuit_breaker.failure_threshold` ≥ 1, `probe_interval` > 0, `half_open_max_probes` ≥ 1.
- `views.*.cache.enabled: true` **requires** `views.*.invalidate_on` non-empty. This is a hard validation error, not a warning. TTL-only view caches are not supported in v1 — views must have an explicit invalidation source.
- `views.*.cache` unset or `enabled: false` → view is not cached, regardless of the global `cache.enabled` setting.

At post-parse validation time (after the schema has been loaded and tables
discovered):

- `views.*.invalidate_on` table names must resolve to tables in the **parsed schema**, not the `tables:` YAML block. The `tables:` block is an override map — auto-discovered tables are legitimate targets. A name that does not resolve to any parsed table is a hard error naming the view and the unresolved table.
- `views.*.invalidate_on` table names must refer to tables that are **included in generation** (not excluded via `exclude_columns`/filter). Referencing an excluded table is a hard error — no mutation hook would fire for it, so the view cache would silently never invalidate from that source.

At `NewCache` construction time:

- `cache.serializer == custom` and no `WithSerializer(...)` → error naming the config line. (No library to auto-inject, so the user must provide one.)
- (Pattern invalidation is mandatory on `Cache` — no capability gating required.)

Note: `serializer: msgpack` does **not** require `WithSerializer(...)`. The
generator emits the `cache/msgpack` import and defaults the serializer to
`msgpack.New()` at codegen time. See §7 and §19.2 for the import-emission
details. `WithSerializer(...)` remains available as an override for users who
want a preconfigured encoder pool.

---

## 4. Module Layout

Mirrors existing patterns (`event/` in-module, `event/natsbus/` separate). External deps always get their own `go.mod` so the runtime module stays dependency-free.

```
cache/                    part of runtime module — stdlib + golang.org/x/sync only
├── backend.go            Backend interface + optional StatsReporter
├── serializer.go         Serializer + JSONSerializer
├── invalidation.go       InvalidationSource / Handler / Subscription
├── event_adapter.go      FromEventSubscriber
├── metrics.go            MetricsRecorder, CircuitState
├── key.go                BuildKey / BuildCompositeKey / BuildTablePattern
├── breaker.go            circuit breaker state machine (consumed by generated facade)
├── error.go              OnErrorFunc + DefaultOnCacheError — cache error policy (§5.8)
├── typed.go              GetAs / SetAs / GetOrSet / KeysFromAny — generic user helpers
└── noop.go               NoopBackend — tests and runtime toggles

cache/memory/             separate go.mod — maypok86/otter/v2 (impl detail)
cache/redis/              separate go.mod — redis/go-redis/v9
cache/msgpack/            separate go.mod — vmihailenco/msgpack/v5

metrics/otel/             separate go.mod — go.opentelemetry.io/otel
```

Each submodule registers in `go.work` + `Makefile`'s `MODULES` list. `.golangci.yml` already permits local `replace` directives (wired during Phase 11.5).

---

## 5. Runtime Package (`cache/`)

### 5.1 Core Interfaces (`cache/backend.go`)

`Backend` + `StatsReporter` semantics and the miss / absent-key contracts are
in PRD §27.3. Implementation-level notes for this package:

- **Absent-key-is-success.** `Invalidate` / `InvalidateMany` / `InvalidatePattern` for a key that does not exist is a success, not an error. First-party backends honor this; custom backends must too. This keeps the `FromEventSubscriber` idempotent-redelivery guarantee intact (§5.4).
- **No capability gating on `InvalidatePattern`.** The generated hook calls `c.backend.InvalidatePattern(ctx, pattern)` unconditionally — no `ErrPatternDeleteUnsupported`, no silent fallback. A stub-error backend routes through the cache error policy (§5.8) and the mutation caller's error path stays clean.
- **`io.Closer` is stdlib, not a custom `Closeable`.** Same signature, standard vocabulary — backends already implementing `Close() error` satisfy it for free.

### 5.2 Serializer (`cache/serializer.go`)

```go
type Serializer interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
}

// JSONSerializer uses encoding/json. Default when no WithSerializer is supplied.
type JSONSerializer struct{}
```

### 5.3 Invalidation (`cache/invalidation.go`)

```go
type InvalidationSource interface {
    Subscribe(handler InvalidationHandler) (InvalidationSubscription, error)
    Close() error
}

// Empty pks slice signals full-table invalidation (backend uses InvalidatePattern).
// Custom InvalidationSource implementations that speak in raw strings (Kafka CDC
// topic names, webhook payloads) cast their string at the boundary, in the
// TableXxx constant's form — schema-qualified on PostgreSQL (PRD §5.5):
// handler(ctx, hook.TableName(schema+"."+rawTable), pks).
type InvalidationHandler func(ctx context.Context, table hook.TableName, pks []any) error

type InvalidationSubscription interface {
    Unsubscribe() error
}
```

### 5.4 Event Adapter (`cache/event_adapter.go`)

Semantics (synchronous-per-event, idempotent, error-routing) are documented
in PRD §27.9 under "FromEventSubscriber Adapter." Signature:

```go
func FromEventSubscriber(sub event.Subscriber, opts ...EventAdapterOption) InvalidationSource

type EventAdapterOption func(*eventAdapter)

func WithEventMetrics(m MetricsRecorder) EventAdapterOption
```

The zero-option form `FromEventSubscriber(sub)` is the user-facing
constructor; the variadic `WithEventMetrics` option exists so the
generated `*Cache` facade can route the handler-error record through
`MetricsRecorder.Error(ev.Schema, table, "invalidate", err)` at
construction time. `ev.Schema` is only available inside the adapter's
shim — the `InvalidationHandler` signature is `(ctx, table, pks)` by
design (to keep Layer 2 implementers free of observability concerns),
so the adapter is the only party that can emit the metric with the
correct `schema` label (G13).

`cache/` may import `event/` — both are stdlib-adjacent runtime packages.

**Why no batching window?** A time-based coalescer adds observable
invalidation latency (reads between mutation commit and the flush return
stale data for the window duration) and a goroutine with its own lifecycle,
for no win over the transport's own batching. When a terminal mutation
produces N `AffectedPKs`, the generated event hook already calls
`Publisher.PublishBatch` with N events — transports either fan those out in
a single network frame (NATS publish loop, Redis pipeline) or serialize them
internally. The adapter receiving side stays boring: one event in, one
invalidation out. This is recorded as §22 decision #20 (non-binding resolution
of G1).

### 5.5 Metrics (`cache/metrics.go`)

`MetricsRecorder` interface methods, `CircuitState` enum, and the
`StatsReporter` complementarity all live in PRD §27.13. Implementation notes:

- **Cycle-free hook import.** `cache/` imports `hook` for the `TableName` type. `hook` is stdlib-only and does not depend on `cache` — no cycle risk. Re-exporting via an alias would add a second name for the same type without benefit, so the canonical one is used directly.
- **Zero-cost when omitted.** The generated hook checks for a nil `MetricsRecorder` once at construction and stores a typed-nil. Per-call guards (`if c.metrics != nil { ... }`) compile to a single nil check; the latency path uses `start := time.Now(); defer c.metrics.GetLatency(schema, table, time.Since(start))` only when metrics are wired.

### 5.6 Key Helpers (`cache/key.go`)

```go
// BuildKey returns "{prefix}:{schema}.{table}:fingerprint:v{fingerprint}:pk:{pk}".
// Every segment carries a label (fingerprint:, pk:) so keys are self-describing
// in a cache dump — no positional decoding required.
//
// fingerprint is a short per-table schema version baked in at codegen time
// (see §10.1). Pass "" to skip the fingerprint segment — only direct user
// callers with their own versioning scheme should do so.
// Schema is included for PostgreSQL (defaulting to "public" when the caller
// passes ""), and omitted for MySQL / SQLite where schemas do not apply.
//
// The pk: label is ALWAYS terminal — nothing follows it. See §10 for the
// grammar's full invariants, including why this matters for extensibility.
func BuildKey(prefix, schema, table, fingerprint string, pk any) string

// BuildCompositeKey joins composite PK values with ":" under the single
// terminal pk: label.
// Example: "sqlgen:public.order_items:fingerprint:v4e5f6g:pk:order123:product456"
//
// INVARIANT: pks MUST be in the table's schema PK column order — the same
// order PRIMARY KEY (a, b, c) was declared in DDL. The helper concatenates
// without inspecting column names; passing values in a different order
// produces a different cache key for the same logical entity, causing
// cache misses and orphaned writes. The generator always calls this helper
// with pks in canonical order (see §9.3); user-direct calls should use the
// typed per-table helpers emitted for composite-PK tables (§9.2) rather
// than calling BuildCompositeKey manually.
func BuildCompositeKey(prefix, schema, table, fingerprint string, pks []any) string

// BuildTablePattern returns the prefix + trailing-"*" pattern for a table.
// Deliberately does NOT include fingerprint: or pk: segments — the pattern
// matches every fingerprint generation for the table, so InvalidateTable
// cleans up BOTH current and orphaned-from-prior-deploy entries in a
// single pass. Not the mutation path: every op, *Where included, evicts by
// key (§9.3).
// Example: "sqlgen:public.products:*" (matches fingerprint:v1a2b3c AND
// fingerprint:v4e5f6g entries across all PKs)
func BuildTablePattern(prefix, schema, table string) string
```

Schema normalization is the **generator's** responsibility, not the key helper's.
The generator resolves each table's schema at render time and passes the literal
to `BuildKey`:

- **PostgreSQL:** empty `input.schema` config is normalized to `"public"` before template rendering. Every PostgreSQL key therefore has a schema segment.
- **MySQL / SQLite:** schema is always `""`; `BuildKey` omits the `{schema}.` prefix entirely (key looks like `sqlgen:products:fingerprint:v1a2b3c:pk:42`).

The key helpers themselves treat `schema == ""` as "omit the segment" so they
stay dialect-agnostic and safe to call from user code (direct `Invalidate*` calls
from handler code pass the same schema string the generator baked into the table
constants). Callers that explicitly want the PostgreSQL default must pass
`"public"` — the helpers do not guess.

PKs are formatted with `%v` — good enough for UUIDs, ints, strings, and the KSUID
stringer path. Composite keys use `:` as a separator; the generator already knows
PK count per table.

### 5.7 Circuit Breaker (`cache/breaker.go`)

A reusable state machine for the generated hook. Lives in the runtime package so
all tables and views share the same implementation.

```go
type Breaker struct {
    // internal fields
}

type BreakerConfig struct {
    FailureThreshold  int
    ProbeInterval     time.Duration
    HalfOpenMaxProbes int
    OnStateChange     func(from, to CircuitState)
}

func NewBreaker(cfg BreakerConfig) *Breaker

// Allow returns true if the call should proceed. When Open, returns false
// until ProbeInterval has elapsed (transitions to HalfOpen).
func (b *Breaker) Allow() bool

// RecordSuccess / RecordFailure drive state transitions.
func (b *Breaker) RecordSuccess()
func (b *Breaker) RecordFailure()

func (b *Breaker) State() CircuitState
```

State machine:

```
Closed  --N consecutive failures-->  Open
Open    --probe_interval elapsed-->  HalfOpen
HalfOpen --success-->                Closed
HalfOpen --failure-->                Open
```

Full transition table (authoritative reference for `breaker_test.go`). `N =
FailureThreshold`. `counter` is the internal consecutive-failure counter.

| From | Event | To | `counter` side-effect | Notes |
|---|---|---|---|---|
| Closed | `RecordSuccess()` | Closed | reset to 0 | Success breaks any ongoing failure streak. |
| Closed | `RecordFailure()` (counter + 1 < N) | Closed | +1 | Still under threshold. |
| Closed | `RecordFailure()` (counter + 1 == N) | **Open** | stays at N; `OpenedAt = now()` | Nth consecutive failure trips the breaker. `OnStateChange(Closed, Open)` fires. |
| Open | `Allow()` called, elapsed < ProbeInterval | Open | no change | Returns `false`; call bypasses cache. |
| Open | `Allow()` called, elapsed ≥ ProbeInterval | **HalfOpen** | no counter change; `probesInFlight = 0` | `OnStateChange(Open, HalfOpen)` fires. Caller's `Allow()` returns `true` only if `probesInFlight < HalfOpenMaxProbes`. |
| Open | `RecordSuccess()` / `RecordFailure()` | Open | no change | Records received while the breaker is open (e.g. from in-flight calls started before the trip) are ignored. Open state transitions are driven by `Allow()` timer, not `RecordX`. |
| HalfOpen | `Allow()` called, `probesInFlight < HalfOpenMaxProbes` | HalfOpen | `probesInFlight += 1` | Returns `true`. Caller MUST eventually call `RecordSuccess` or `RecordFailure` to release the probe slot. |
| HalfOpen | `Allow()` called, `probesInFlight == HalfOpenMaxProbes` | HalfOpen | no change | Returns `false`. Additional callers bypass while the current probe(s) are in flight. |
| HalfOpen | `RecordSuccess()` | **Closed** | reset to 0; `probesInFlight = 0` | First probe success closes the breaker. `OnStateChange(HalfOpen, Closed)` fires. |
| HalfOpen | `RecordFailure()` | **Open** | reset counter to N; `OpenedAt = now()`; `probesInFlight = 0` | One probe failure is enough to re-open (default `HalfOpenMaxProbes = 1`). `OnStateChange(HalfOpen, Open)` fires. The next `Closed` entry (after a successful half-open probe later) starts counter fresh at 0. |

Invariants and edge cases:

- **`counter` is only meaningful in `Closed`.** When the breaker trips to `Open`, `counter` is frozen at N; when it returns to `Closed` via a successful `HalfOpen` probe, `counter` resets to 0. No arithmetic happens in `Open` or `HalfOpen` states.
- **`Allow()` is the state-transition clock for the `Open → HalfOpen` edge.** No background goroutine. The first caller to hit `Allow()` after `ProbeInterval` elapses observes the transition.
- **Concurrent `Allow()` in `HalfOpen`.** `probesInFlight` is incremented atomically; only the first `HalfOpenMaxProbes` concurrent callers get `true`. Followers fall through to "cache miss" (reads) / "skip invalidation" (writes).
- **Concurrent `RecordSuccess()` in `HalfOpen`.** Only one close-transition fires `OnStateChange`. Subsequent `RecordSuccess` calls after the state is already `Closed` are treated as ordinary closed-state successes (counter reset — but already 0).
- **Concurrent `RecordFailure()` in `HalfOpen`.** Only one open-transition fires `OnStateChange`. Subsequent `RecordFailure` calls while the breaker is already `Open` are ignored (Open-state behavior above).
- **`Allow()` never modifies `counter`.** Counter is a failure tally, not a call tally.
- **Records received during Open are ignored, not queued.** A success `RecordSuccess` that arrives while `Open` does not "count toward" reopening — only `HalfOpen` probes drive the recovery path.

When `Allow()` returns false, the generated hook short-circuits to "cache miss"
(for reads) and "skip invalidation" (for writes). The DB operation executes
normally — the cache is non-authoritative. TTL remains the safety net.

### 5.8 Error Policy (`cache/error.go`)

Cache operations MUST NOT break user-facing calls. A failing `Get` surfaces as
a cache miss; a failing `Set` / `Invalidate*` is dropped from the caller's
error path. This is a hard contract: the DB operation always wins.

The facade routes every cache backend error through a three-pronged policy,
unified across `Get` / `Set` / `Invalidate` / `InvalidateMany` /
`InvalidatePattern`:

1. **Structured telemetry.** `MetricsRecorder.Error(schema, table, op, err)` with `op ∈ {"get", "set", "invalidate", "invalidate_many", "invalidate_pattern"}`. Zero-cost when no recorder is wired (§5.5 nil-check gating). Schema carries the key-segment value so errors from same-name tables in different schemas remain distinguishable.
2. **Textual log fallback.** `OnErrorFunc` callback, defaulting to `DefaultOnCacheError` which logs to stderr via `log.Printf` — same shape as `event.DefaultOnError` (§5.4 precedent). User can swap via `WithOnError(fn)` (§9.1).
3. **Breaker feedback.** Every routed error calls `breaker.RecordFailure()` — sustained cache errors open the circuit per §17, which short-circuits subsequent cache calls without the backend even being consulted. Counters reset per §5.7 on the next closed-state success.

```go
// cache/error.go
type OnErrorFunc func(ctx context.Context, op, schema string, table hook.TableName, err error)

// DefaultOnCacheError logs cache backend failures to stderr.
// Mirrors event.DefaultOnError for textual consistency. table is the
// hook.TableName value, which already carries the schema on PostgreSQL
// ("public.products", PRD §5.5), so the schema is prefixed only to a value
// that lacks it; MySQL / SQLite logs stay tidy.
func DefaultOnCacheError(ctx context.Context, op, schema string, table hook.TableName, err error) {
    name := string(table)
    if schema != "" && !strings.HasPrefix(name, schema+".") {
        name = schema + "." + name
    }
    log.Printf("cache %s %s: %v", op, name, err)
}
```

The three channels are complementary, not alternatives:

- **Metrics** is the alerting surface — Prometheus / OTel dashboards pick up the `sqlgen.cache.errors` counter per table and op.
- **`OnErrorFunc`** is the textual breadcrumb for developers eyeballing a running process. Swappable to route through `slog`, `zap`, or a structured logger.
- **Breaker** is the self-protection layer — it doesn't care about log lines or metrics wiring; it counts and trips.

**All three fire in order**: metrics first (fast), then `OnErrorFunc` (typically synchronous stderr), then `breaker.RecordFailure()`. An `OnErrorFunc` panic does not stop breaker accounting — the generated helper wraps the callback in a `defer recover()` so a misconfigured logger can't mask a cache outage.

**Marshal/unmarshal errors are cache errors too.** `Set` failures due to JSON/msgpack encoding and `Get` failures due to corrupt stored bytes both route through the same policy. Unmarshal errors additionally treat the call as a cache miss (fall through to DB) — the entry is not automatically deleted, since TTL + the fingerprint segment (§10.1) already handle stale-payload scenarios.

### 5.9 Developer Utilities (`cache/typed.go`, `cache/noop.go`)

Small helpers that sit on top of the byte-level `Backend` interface. Not used by
the generated hook — that path has the entity type baked in and inlines the
serialize/deserialize calls. These helpers are for user code that wants typed
access outside the generated layer (ad-hoc caching, tests, admin tools).

```go
// GetAs reads and decodes a cached entity.
// Returns (zero, false, nil) on cache miss, letting callers distinguish miss
// from "decoded zero value" without pointer-returning variants.
func GetAs[T any](ctx context.Context, b Backend, s Serializer, key string) (T, bool, error)

// SetAs encodes and stores an entity with the given TTL. Marshal errors
// propagate without touching the backend.
func SetAs[T any](ctx context.Context, b Backend, s Serializer, key string, v T, ttl time.Duration) error

// GetOrSet is the typed read-through pattern: return cached; on miss, call load,
// store result, return it. Errors from load propagate without a Set attempt.
// Useful for ad-hoc caching of computed values in handler code — the generated
// hook already does this for entities internally.
func GetOrSet[T any](ctx context.Context, b Backend, s Serializer, key string, ttl time.Duration, load func(ctx context.Context) (T, error)) (T, error)

// KeysFromAny is used by the generated Invalidate/InvalidateMany dispatch to
// project a []any of PK values into []string cache keys via a typed keyer
// function. Type-asserts each element to T, returning a descriptive error on
// mismatch that names the element index, the expected type, and the actual
// type. The generator calls this once per table case in the dispatch switch
// (§9.3) — exported so user-written Cache alternatives and tests can use
// the same path.
//
// PK type constraint for T: T MUST be a value type (scalar like uuid.UUID,
// int64, string; or a composite-PK struct value). Pointer types produce
// address-stringified keys via %v and are never valid here — see §10's
// "PK types are values, not pointers" rule. Same constraint applies to the
// T used with GetAs / SetAs / GetOrSet when the key is built via any of the
// key helpers (§5.6).
func KeysFromAny[T any](pks []any, key func(T) string) ([]string, error)
```

Why generic functions instead of a generic `Backend[T]` interface: Go does not
support generic methods on interfaces. Making `Backend` generic would force one
adapter per entity type over the same underlying client. The free-function
approach keeps the backend interface type-erased (one backend serves all tables)
and gives users typed access at the call site when they want it.

```go
// NoopBackend implements Backend with no-op semantics: Get always misses, Set
// and Invalidate* always succeed without work. Safe for tests, disabled-cache
// runtime toggles, and smoke-testing facade wiring without a real backend.
type NoopBackend struct{}

var _ Backend = NoopBackend{}

// Optional StatsReporter returning zero values, so NoopBackend slots in
// cleanly anywhere a real backend is expected.
func (NoopBackend) Stats() Stats { return Stats{} }
```

---

## 6. First-Party Backends

### 6.1 `cache/memory/` — in-memory

Backed by `github.com/maypok86/otter/v2` internally. Package name is `memory`
so the underlying library is not part of the public surface — we can swap
implementations without a breaking change.

```go
package memory

type Options struct {
    MaxSize      int           // required — the underlying cache mandates a bound
    DefaultTTL   time.Duration // used when caller passes ttl == 0
    StatsEnabled bool          // wire up native stats recording
}

// New returns a concrete *Backend that satisfies cache.Backend.
func New(opts Options) (*Backend, error)
```

Implements: `cache.Backend` (all methods including `InvalidatePattern` via
entry iteration + prefix match), `cache.StatsReporter` (native stats passthrough),
`io.Closer`.

Behaviors:

- Returns `(nil, nil)` for missing keys.
- `Set` with `ttl == 0` uses `Options.DefaultTTL`; if that is also zero, entries never expire.
- `InvalidatePattern` verifies pattern ends with `*` and walks matching entries.
- Safe for concurrent use (lock-sharded under the hood).

#### Per-table TTL enforcement

Per-table TTL is enforced at the **facade layer** (the generated `Cache`
struct), not the backend. The backend interface
(`cache.Backend.Set(ctx, key, value, ttl)`) takes TTL per-call. The flow:

```
YAML (cache.tables.products.cache.ttl: 30m)
    ↓  resolved at codegen time via ResolveTableCacheTTL
Cache.ttlForProduct() → 30 * time.Minute   (precomputed constant in facade)
    ↓  mutation / hydration hook calls
backend.Set(ctx, key, value, 30*time.Minute)
    ↓
otter stores the entry with per-entry TTL attached
```

Otter v2 supports per-entry TTL natively — different entries in the same
`*otter.Cache` carry different expirations. The backend does not need to know
about tables; each entry carries its own TTL. Same mechanism works for Redis
(`EXPIRE` per key) and any future backend that honors per-call TTL.

`Options.DefaultTTL` is only a fallback for callers that pass `ttl == 0`. The
generated hook always passes a resolved per-table TTL, so `DefaultTTL` is
effectively only used by direct user code (`cache.SetAs` with a zero-value TTL).

#### What `cache/memory` does NOT enforce

- **Per-table memory budgets.** A single `*memory.Backend` holds entries for all tables and evicts them via otter's unified TinyLFU + LRU policy. You cannot say "products gets 50% of MaxSize, audit_logs gets 10%". Hot tables crowd out cold ones naturally; cold tables' entries get evicted first. If strict table isolation is needed, instantiate separate backends per table — but `NewCache` takes one backend and routes all tables through it. Per-table backend support is a deferred feature.
- **Per-table serializer at the backend.** Entries are opaque `[]byte` from the backend's perspective. The generated hook picks the right serializer per table before calling `Set` (via the generated `serializerForProduct()` helpers). The backend never touches a type-aware path.

### 6.2 `cache/redis/` — distributed

```go
package redis

// New wraps a caller-owned client and returns a concrete *Backend that
// satisfies cache.Backend. Does not own the connection lifecycle by default.
func New(client redis.UniversalClient, opts ...Option) *Backend

// Option knobs:
func WithOwnedClient() Option      // Close() will also close the client
func WithScanCount(n int) Option   // SCAN page size (default 500)
func WithLocalStats(b bool) Option // track hits/misses locally (default true)
```

Implements: `cache.Backend` (all methods including `InvalidatePattern` via
SCAN + DEL in batches — never KEYS), `cache.StatsReporter` (local counters for
hits/misses/sets/invalidations; `Entries` from INFO when available),
`io.Closer` (no-op unless `WithOwnedClient`).

Behaviors:

- Uses `redis.Nil` to detect misses and returns `(nil, nil)`.
- `InvalidatePattern` uses `SCAN MATCH pattern COUNT n` + `DEL` in batches. Never `KEYS`.
- Local stats use atomic counters. `WithLocalStats(false)` disables them for zero-overhead mode.
- Per-table TTL / serializer enforcement works identically to §6.1 — the generated hook passes the resolved per-table TTL on every `Set`, Redis honors it via `EXPIRE`. No Redis-side concept of tables.

---

## 7. Optional Serializer: `cache/msgpack/`

```go
package msgpack

// New returns a Serializer backed by github.com/vmihailenco/msgpack/v5.
func New() cache.Serializer

// NewWith lets callers supply a preconfigured Encoder/Decoder pool.
func NewWith(opts Options) cache.Serializer
```

**Auto-injected when `serializer: msgpack`.** The generator handles the wiring
at codegen time — same pattern as the driver packages (pgx vs stdlib):

- When `cache.serializer == "msgpack"`, the generated `cache_gen.go` emits `import sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"` and defaults the serializer to `sqlgenmsgpack.New()`.
- The consumer runs `go mod tidy` once; the dependency only lands in projects that opted in.
- `WithSerializer(...)` still wins when passed — use it to inject a preconfigured encoder pool, custom tags, or a test double.
- `NewCache` does NOT validate msgpack availability at runtime; the import is baked in at codegen.

`serializer: custom` is the only case that errors at construction when
`WithSerializer(...)` is absent — there is no library for the generator to
auto-inject.

---

## 8. `metrics/otel/`

Full instrument table + schema- and table-label semantics live in PRD §27.13.
The `table` label is the bare SQL name: `tableAttrs` strips the `schema.`
prefix the `hook.TableName` value carries on PostgreSQL. Public surface:

```go
package otel

// New returns a MetricsRecorder backed by an OTel MeterProvider. If provider
// is nil, uses otel.GetMeterProvider() (global default).
func New(provider metric.MeterProvider) cache.MetricsRecorder
```

---

## 9. Generated `Cache` facade

Factory surface (`NewCache`, `CacheOption` list), public method signatures
(`QueryHook` / `MutationHook` / `Invalidate` / `InvalidateMany` / `InvalidateTable`
/ `Close`), PK element-type contract, and call-site examples all live in PRD
§27.4 and §27.9. The name `Cache` (vs. `CacheHooks`) was chosen to follow the
`http.Client` / `http.RoundTripper` precedent — runtime package owns the
interface (`cache.Backend`), user-facing code owns the facade struct.

**Design parity with `WithEventPublisher`.** `NewCache(backend, ...opts)`
mirrors `WithEventPublisher(publisher, ...configFns)`: config (per-table TTL,
serializer, key prefix, hydration, breaker defaults) is a generator concern
baked in at codegen time, not a runtime argument. Duplicating YAML state at
call sites would create drift between `sqlgen.yml` and deployed behavior. The
`CacheConfig` Go type (§3.2) models the YAML for parsing / validation /
resolution — it is not part of the `NewCache` surface.

**No per-table typed invalidation methods are generated.** The `XXXPK` struct
is already the typed guardrail: field order is fixed by the declaration, and
field-named literals (`OrderItemPK{OrderID: ..., ProductID: ...}`) are
self-documenting. A positional typed method like
`InvalidateOrderItem(ctx, orderID, productID)` would silently accept swapped
arguments; the struct-literal form does not. Static analysis (§19A) closes the
build-time gap for users of generated `<pkg>.TableXxx` constants without
multiplying the public method count.

### 9.3 Generated Internals

Per-table generated helpers (not exported):

```go
// Per-table schema fingerprint constants (§10.1). Baked at codegen time from
// the table's column names, Go types, struct tags, PK order, and serializer
// identity. Any struct-shape change regenerates these; old-fingerprint keys
// naturally orphan.
const fingerprintProducts   = "1a2b3c4d"
const fingerprintOrderItems = "4e5f6g7h"

// Per-table key builders, one per cached table. Parameter type is the CONCRETE
// PK type (scalar for single-PK, the generated XXXPK struct for composite).
// The typed signature lets the generic dispatch use cache.KeysFromAny[T] without
// per-case type-assertion boilerplate AND turns wrong-type PK arguments into
// loud errors instead of silently-garbage %v-stringified keys.
func (c *Cache) keyForProduct(pk uuid.UUID) string
    // internally: cache.BuildKey(c.prefix, "public", "products", fingerprintProducts, pk)

func (c *Cache) keyForOrderItem(pk OrderItemPK) string
    // Composite variant — takes the generated PK struct. Canonical ordering
    // is enforced by the struct declaration: the generator always builds the
    // []any slice in DDL PRIMARY KEY column order inside this one function,
    // never at a call site.
    // internally:
    //   cache.BuildCompositeKey(c.prefix, "public", "order_items",
    //       fingerprintOrderItems, []any{pk.OrderID, pk.ProductID})

// Per-table TTL resolver (precomputed at construction).
func (c *Cache) ttlForProduct() time.Duration

// Per-table serializer (only when per-table override present).
func (c *Cache) serializerForProduct() cache.Serializer

// Per-view pattern helpers. Takes hook.TableName so the generated switch can
// use table constants directly rather than magic strings.
func (c *Cache) patternsForSourceTable(table hook.TableName) []string
// e.g., patternsForSourceTable(TableProducts) returns ["sqlgen:public.product_summary:*"]
// built from views that list "products" in invalidate_on.

// Per-table "full parent, no relationships" FieldOptions factory. Used by the
// hydration path to guarantee parent-only results (see §13). One per cached
// table; emitted with every column flag pre-set true, every relationship flag
// pre-set false. Never depends on the default semantics of nil FieldOptions.
func fullParentFieldOptionsProduct() *ProductFieldOptions

// Per-table relationship detector. Returns true if the given FieldOptions has
// any relationship-typed flag set (see §11 bypass rule). Returns false when fo
// is nil or when only column flags are set.
func hasAnyRelationshipProduct(fo any) bool
```

**Key-order invariant enforcement.** The generator is the only code path that
calls `BuildCompositeKey` directly — inside each `keyForX` function, once per
cached composite-PK table. The `[]any{...}` slice is constructed by reading the
generated PK struct fields in their declared order (which matches DDL PK column
order by construction). `MutationContext.AffectedPKs` is populated in canonical
order by terminals for the same reason — iterating PK columns in declared
order, never a map (maps are non-deterministic per the template guideline in
`guidelines/TEMPLATES.md`).

**Schema migration caveat.** Reordering PRIMARY KEY columns in a migration
changes every cache key for that table. No stale-data risk (all old keys miss,
cache warms from cold), but the cache effectively flushes on deploy — operators
should plan capacity for the cold-start load. Added to the sync-back list as a
PRD note.

**Generic `Invalidate` / `InvalidateMany` dispatch** (as generated — one switch
case per cached table, sourced from `BuildCacheContext.CachedTables`):

```go
func (c *Cache) InvalidateMany(ctx context.Context, table hook.TableName, pks []any) error {
    var (
        keys []string
        err  error
    )
    switch table {
    case TableProducts:
        keys, err = cache.KeysFromAny(pks, c.keyForProduct)
    case TableOrderItems:
        keys, err = cache.KeysFromAny(pks, c.keyForOrderItem)
    // ... one line per additional cached table
    default:
        return fmt.Errorf("cache: unknown or non-cached table %q", table)
    }
    if err != nil {
        return fmt.Errorf("cache: InvalidateMany(%q): %w", table, err)
    }
    return c.invalidateKeys(ctx, table, keys)
}

func (c *Cache) Invalidate(ctx context.Context, table hook.TableName, pk any) error {
    return c.InvalidateMany(ctx, table, []any{pk}) // single is a 1-element batch
}
```

Each cached table contributes **one line** to the switch. `cache.KeysFromAny[T]`
(§5.9) performs the per-element type assertion with uniform error messaging,
and the wrapping `fmt.Errorf` naming the table runs once for the whole batch.
Both single and batch paths converge on `c.invalidateKeys(ctx, table, keys)`
for metrics, circuit-breaker treatment, and the final `backend.InvalidateMany`
call.

**`InvalidateTable` dispatch.** Full-table invalidation routes through a
separate internal helper — no PK switch, no `KeysFromAny` — but still gated on
"is this table actually in the cached-tables set" so a typo doesn't silently
succeed. The generated implementation reuses the same `switch table` skeleton
to resolve the per-table `schema` constant, then hands off to
`c.invalidatePattern` (the internal sibling of `invalidateKeys` — same metrics
+ breaker treatment, different backend method).

```go
func (c *Cache) InvalidateTable(ctx context.Context, table hook.TableName) error {
    var schema string
    switch table {
    case TableProducts:
        schema = "public"
    case TableOrderItems:
        schema = "public"
    // ... one line per additional cached table
    default:
        return fmt.Errorf("cache: unknown or non-cached table %q", table)
    }
    pattern := cache.BuildTablePattern(c.prefix, schema, string(table))
    return c.invalidatePattern(ctx, table, pattern)
}
```

`BuildTablePattern` is fingerprint-agnostic (§10.1), so this one call clears
both current-generation and orphaned-from-prior-deploy entries — handy right
after a migration if the operator wants instant cleanup instead of waiting
for backend LRU/TTL.

**Internal helper symmetry.** `invalidateKeys` and `invalidatePattern` are
sibling internals on `*Cache` — both centralize the metrics emission
(`metrics.Invalidate(schema, table)` on success, `metrics.Error(schema, table, op, err)` on
failure), circuit-breaker `RecordSuccess` / `RecordFailure`, and
`InvalidateLatency` timing. The only difference is which backend method they
dispatch to (`InvalidateMany` vs. `InvalidatePattern`). Neither is public —
all external callers go through `Invalidate` / `InvalidateMany` /
`InvalidateTable`. This is why §9.2 lists only three invalidation methods: the
fourth-seeming "InvalidatePattern" is internal and is not part of the public
surface.

**Correctness upgrade over the prior `(pk any)` design.** Typed
`keyForProduct(pk uuid.UUID)` makes wrong-type PK arguments fail loudly. A
call like `c.Invalidate(ctx, TableProducts, 42)` on a UUID-PK table errors
with `element 0: expected uuid.UUID, got int` — the old `keyForProduct(pk any)`
would silently build `sqlgen:public.products:42` via `fmt.Sprintf("%v", pk)`
and no-op-delete a key that doesn't exist.

Per-operation dispatch (the core of `QueryHook()`/`MutationHook()`):

```go
// QueryHook — only hooks Get; all other query ops pass through.
func (c *Cache) queryHook() hook.QueryHook {
    return func(next hook.QueryHandler) hook.QueryHandler {
        return func(ctx context.Context, q *hook.QueryContext) (any, error) {
            if q.Op != hook.OpGet { return next(ctx, q) }
            if skipCache(q.CallOptions) { return next(ctx, q) }
            if !c.tableEnabled(q.Table) { return next(ctx, q) }
            if hasRelationshipLoad(q.CallOptions) {
                return next(ctx, q)  // §11: relationship-loaded Gets bypass cache
            }
            if !c.breaker.Allow() {
                c.metrics.Miss(q.Schema, q.Table)
                return next(ctx, q)  // circuit open — skip cache
            }
            if isPartial(q) && !c.hydrationEnabled {
                return next(ctx, q)  // partial + no hydration → bypass
            }
            // read-through, stampede-deduped via singleflight (see below)
            // ...
        }
    }
}
```

**Cache stampede dedup (`singleflight.Group`).** The read-through path is
wrapped in a `golang.org/x/sync/singleflight.Group` owned by the `Cache`
facade, keyed by the cache key. Concurrent cache misses for the same
`(table, pk)` collapse into a single backend `Get` → DB query → backend
`Set`; siblings await the in-flight result and share it. Without this, a
cold key or just-invalidated hot key triggers N duplicate DB round-trips
under load.

Dependency posture: `golang.org/x/sync` is already an accepted runtime
dependency — `guidelines/ARCHITECTURE.md`'s "Key Runtime Dependencies"
table lists `golang.org/x/sync/errgroup` for generated relationship
loading. `singleflight` sits in the same subrepo, maintained by the Go
team alongside the stdlib. Adding the second subpackage does not expand
the module's dependency surface meaningfully.

```go
import "golang.org/x/sync/singleflight"

// Field on the generated *Cache facade.
type Cache struct {
    // ... existing fields ...
    sf singleflight.Group
}
```

```go
// Sketch — lives inside the generated per-table read-through helper,
// which knows the concrete entity type.
//
// Outer probe (warm-cache fast path): a hit returns immediately and
// never enters singleflight bookkeeping.
if entity, hit, _ := backend.Get(key); hit { return entity, nil }

// Miss path: collapse concurrent callers into a single in-flight call.
ch := c.sf.DoChan(key, func() (any, error) {
    // 0. backend.Get re-probe — closes the gap between the outer probe
    //    and flight registration. A sibling flight may have populated
    //    the cache while this caller traversed that gap; if so, return
    //    the populated entity without calling next.
    // 1. DB via next(ctx, q)
    // 2. backend.Set (post-DB-success only)
    return entity, err
})
```

Scope and semantics:

- One `singleflight.Group` per `*Cache` instance. Key namespace is the full cache key (already unique per `(prefix, schema, table, pk)`), so cross-table collisions are impossible.
- Applies only to the `Get` read-through path. Mutations (`Set` after `Create`, `Invalidate*`) do not go through singleflight — they are already per-operation atomic at the backend.
- Runs **inside** the circuit-breaker gate — when the breaker is open, reads short-circuit to `next(ctx, q)` and never reach `sf.Do`. `singleflight` never holds work across breaker state changes.
- Hydration retains its own `sync.Map` dedup (§13). The two mechanisms cover different paths: singleflight collapses concurrent full-entity read-through misses; hydration dedup prevents parallel background refills for the same partial-read key. Both can be active simultaneously without interfering.
- The leader's `context.Context` is the one `singleflight` passes into `fn`. Followers that cancel their own context return `ctx.Err()` locally — the implementation selects on `<-ch` and `<-ctx.Done()` around `sf.DoChan(...)`, not the blocking `sf.Do(...)`. The leader's result still completes and populates the cache for subsequent callers.
- Errors are shared: if the DB query fails for the leader, every awaiting caller receives the same error. This matches the `next(ctx, q)`-direct behavior they would have had without caching, just without duplicating the load.
- No explicit `Forget` call is needed on the `Get` path — `singleflight` releases the in-flight slot as soon as `fn` returns, whether it succeeded or failed. A subsequent caller arriving after the error re-enters `fn` and gets a fresh attempt.
- **In-flight re-check (FIX-070).** The flight callback re-probes `backend.Get` before calling `next`. The outer probe handles the warm-cache fast path; the inner re-check handles the stampede edge where a sibling flight populated the cache between this caller's outer miss and `sf.DoChan` registration. Without the re-check, a flight registered after a sibling's flight has already completed would still run a duplicate DB query (the flight's slot is released as soon as `fn` returns, so late stragglers find no in-flight call to follow). Metrics are recorded only on the outer probe — the inner re-check is dedup machinery, not an observable cache event, so it emits no `Hit` / `Miss` / `GetLatency` and skips `RouteError` on inner failure (the outer probe already routed any backend error). Breaker gating still applies: an open breaker skips the inner probe and falls through to `next` just like the outer probe.

**`hasRelationshipLoad`** inspects the resolved `CallOptions.FieldOptions`. The
generator emits `{Table}FieldOptions` with relationship-typed fields mixed
alongside column fields (PRD glossary: "Generated struct controlling which
columns appear in SELECT and which relationships to load"). The hook type-asserts
to the specific `FieldOptions` type and calls a generated per-table
`hasAnyRelationship(fo any) bool` helper — one per cached table — that
checks only the fields the generator knows to be relationship-typed (ignoring
column flags). Returns `false` when `FieldOptions` is nil or when only column
flags are set.

`MutationHook()` fans out on `m.Op`:

| Op | Behavior |
|---|---|
| `OpCreate`, `OpCreateMany` | After success, `Set` each result entity. |
| `OpUpdate`, `OpUpdateMany` | `InvalidateMany(keys(AffectedPKs))`. |
| `OpUpsert` | Same as Update — `InvalidateMany`. |
| `OpUpsertMany` | `InvalidateMany(keys(AffectedPKs))` — same arm as the `*Many` ops, **not** `OpCreateMany`'s write-through arm (PRD §27.7, 27.6). |
| `OpIncrement` | `Invalidate` single key. |
| `OpSoftDelete`, `OpHardDelete`, `OpRestore` | `Invalidate` single key. |
| `OpSoftDeleteMany`, `OpHardDeleteMany`, `OpRestoreMany` | `InvalidateMany`. |
| `OpUpdateWhere`, `OpSoftDeleteWhere`, `OpHardDeleteWhere`, `OpRestoreWhere` | `InvalidateMany(keys(AffectedPKs))` — same arm as the `*Many` ops (FIX-208). |

After every mutation, also pattern-invalidate views listed in `invalidate_on` for
that source table (§15).

### 9.4 Integration with Client

Generated client options (`cache_gen.go` + additions to `client.go.tmpl`):

```go
// WithCache wires a *Cache instance into the client. Shorthand for:
//   WithQueryHook(c.QueryHook()), WithMutationHook(c.MutationHook())
func WithCache(c *Cache) Option
```

Typical user wiring:

```go
c, err := <pkg>.NewCache(
    memory.New(memory.Options{MaxSize: 100_000, DefaultTTL: time.Hour}),
    <pkg>.WithInvalidationSource(cache.FromEventSubscriber(eventSub)),
    <pkg>.WithMetricsRecorder(otel.New(nil)),
)

client := <pkg>.New(db,
    <pkg>.WithEventPublisher(publisher),
    <pkg>.WithCache(c),
)
defer c.Close()
```

### 9.5 Hook Chain Ordering

The generated `New()` constructs the hook chain from outermost to innermost:

```
outermost:  WithCache (query + mutation)
            ...
            WithEventPublisher (mutation only)
            ...
user hooks (WithQueryHook, WithMutationHook in registration order)
            ...
innermost:  terminal (actual DB call)
```

Reasoning:

- **Cache is outermost** so a cache hit short-circuits *before* any user hook or event hook runs. User hooks and events should not observe reads that never hit the DB.
- **Events are outside user hooks** (established in Phase 11.3) so users can mutate `Input` and the event still sees the final shape.
- On mutations, the order cache → events → user → terminal means:
  - User hooks run with the pre-invalidation cache state (can still read cached values during the mutation path).
  - Events fire after the mutation succeeds.
  - Cache invalidation is queued on the outermost layer — it registers `tx.OnCommit` callbacks so invalidation happens on commit, after events.

**Post-commit firing order is FIFO, not emergent.** When mutations run inside a
transaction, both events (Phase 11.3) and cache invalidation (§14.3) register
`tx.OnCommit` callbacks. `database.Tx.OnCommit` guarantees FIFO execution within
a depth — see §14.3 for the full contract and the backing test
(`database/transaction_test.go:TestOnCommitCallbackOrdering`). Because the cache
hook is outermost, its `post` block runs *after* the event hook's, so its
`OnCommit` registration is appended *after* the event hook's. On commit, events
fire first, then cache invalidation. Reordering the hook chain or the `OnCommit`
behavior would invert this, so both are tracked contracts (the hook chain here,
the `OnCommit` FIFO contract cross-referenced from §14.3).

---

## 10. Cache Key Grammar

Grammar, examples, composite-PK ordering invariant, fingerprint segment
rationale, `BuildTablePattern` fingerprint-agnostic behavior, the terminal
`pk:` rule, and the PK-values-not-pointers rule all live in PRD §27.5. This
section holds the fingerprint computation algorithm (§10.1) and the
`cache.version` kill-switch semantics (§10.2) — both are implementation-level,
not spec-level.

**Labeled-segment grammar.** Keys are self-describing:
`{prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}`. Each labeled slot
(`fingerprint:`, `pk:`) means a reader can decode a key without consulting the
spec, and adding new labeled segments later (tenant, partition, region) doesn't
disturb existing positions. `pk:` is ALWAYS terminal — nothing comes after it,
which keeps composite-PK encoding unambiguous under a label-based parser.

### 10.1 Schema Fingerprinting

The `fingerprint:v{fp}` segment is a short hex string baked into each cached
table's and view's key helpers at codegen time. Its purpose: prevent
serving stale-decoded entries after a schema migration regenerates the Go
struct shape.

**Computed input (sha256 → first 8 hex chars):**

```
sha256(
    table_name || 0x00 ||
    schema || 0x00 ||
    json_bytes(columns) ||      // [{Name, GoType, StructTag} ...] in DDL column order
    json_bytes(pk_columns) ||   // PK names in DDL PRIMARY KEY order (order matters)
    serializer_id ||            // "json" | "msgpack" | "custom:<sha of WithSerializer type name>"
    cache_version               // optional user-controlled kill switch — see §10.2
)[:8]
```

8 hex chars = 32 bits of entropy — collisions are astronomically unlikely
within a single deployment (one-table namespace), and collision risk has no
correctness impact even if it happened (a stale decode attempt would fail
decoding and the hook treats it as a miss — see below).

**Why these inputs:**

- **Column name, Go type, struct tag** — a rename or type change produces
  different serialized bytes; the fingerprint must change.
- **Column order** — some serializers (msgpack with array-mode encoding, CBOR)
  are position-sensitive; include order to be safe.
- **PK column order** — composite-PK key grammar depends on declared order
  (§5.6 invariant). Reordering is technically a shape change.
- **Serializer identity** — json-encoded bytes and msgpack-encoded bytes are
  never interchangeable. Changing the serializer must invalidate.
- **Optional user-controlled `cache.version`** — see §10.2.

**What the fingerprint does NOT cover:**

- SQL DDL details that do not affect the Go struct shape (index changes,
  constraint tweaks, comments) — these do not regenerate the struct, so
  cached bytes remain valid.
- Go comments or formatting — does not change wire format.
- Generator template refactors that produce identical struct bytes — same
  reason.

Put another way: the fingerprint tracks *serialized wire format*, not *DDL*
or *generator version*. Rolling out a new generator that produces
byte-identical structs keeps the cache warm.

**Generated artifact:**

```go
// cache_gen.go — one constant per cached table/view.
const fingerprintProducts   = "1a2b3c4d"
const fingerprintOrderItems = "4e5f6g7h"

// Per-table key builder (emitted per §9.3, with fingerprint passed in).
func (c *Cache) keyForProduct(pk uuid.UUID) string {
    return cache.BuildKey(c.prefix, "public", "products", fingerprintProducts, pk)
}
```

**Cache miss semantics on fingerprint mismatch.** The fingerprint lives in
the key's `fingerprint:` segment, not the payload. An old-fingerprint key and
a new-fingerprint key are literally different keys — a `Get` with the new
fingerprint simply does not find the old entry and falls through to the DB,
then `Set`s under
the new fingerprint. No "is this byte stream decodable" check is needed —
the separation is structural, not behavioral.

**Orphan cleanup:**

- In-memory backend: otter's TinyLFU/LRU reclaims old-fingerprint entries as
  bounded space pressure evicts them. Bounded `MaxSize` (§6.1) means this is
  automatic and fast.
- Redis backend: old entries sit until TTL expires or LRU evicts under
  `maxmemory` pressure. Operators who want immediate cleanup can call
  `Cache.InvalidateTable(ctx, TableProducts)` after deploy — the pattern is
  fingerprint-agnostic and clears both generations in one pass.
- No housekeeping goroutine, no background cleaner. TTL + backend eviction
  + optional explicit `InvalidateTable` covers all cases.

**Rolling-deploy semantics:**

During a rolling deploy with mixed old/new pods: old pods read/write
old-fingerprint keys; new pods read/write new-fingerprint keys. Both
namespaces coexist temporarily. No data is served under the wrong shape —
each pod only sees keys its generator produced. Brief DB load bump (new
pods hit cold cache) is the cost; correctness never suffers. This is the
same behavior as a hard flush, just staggered over the deploy window.

### 10.2 Optional Manual Kill Switch: `cache.version`

For operators who want an explicit deploy-day invalidation (independent of
struct shape changes), `CacheConfig.Version` feeds into every fingerprint:

```yaml
cache:
  enabled: true
  version: 2            # bump to force-invalidate every cached table + view
  ttl: "1h"
  # ...
```

- Default `0` (or unset) — fingerprint is purely schema-derived.
- Bumping it regenerates every fingerprint on next `sqlgen generate`, turning
  the deploy into a full cold-start for the cache.
- Use cases: emergency response to a data-correctness bug, forcing a cache
  flush after a non-struct-affecting semantic change (e.g. a custom
  serializer now emits slightly different bytes for the same Go struct).
- Per-table `cache.version` override is deferred — demand-driven addition.

---

## 11. Cache Invariant

See PRD §27.6. Summary: cache only contains full entities; relationship-loading
calls bypass the cache entirely (any relationship-typed flag on `FieldOptions`
→ direct DB JOIN for o2o, separate `GetMany` for o2m / m2m; no cache read, no
cache write). Rationale for bypass: preserves the one-key-one-table invariant
without hiding complexity. See §22 decision #18 for the full rationale
(including rejected alternatives: caching JOIN result + caching parent-only
with re-JOIN on hit).

---

## 12. Cache Behavior by Operation

See PRD §27.7. Implementation notes: `UpdateMany` / batch delete variants feed
`InvalidateMany(keys(AffectedPKs))` — `AffectedPKs` element shape matches the
`Cache.Invalidate*` contract (scalar for single-PK, `XXXPK` struct value for
composite) so no pack step is needed (see `hook/hook.go` godoc on
`MutationContext.AffectedPKs`). `*Where` operations share that arm rather than
pattern-wiping: each captures `AffectedPKs` before returning (`RETURNING` on
PostgreSQL/SQLite, a pre-`SELECT` on MySQL), so the matched row set is known by
the time invalidation runs. The arm also returns early for a table the config
left uncached — its `cacheSchemaFor` miss must not become a commit error under
`CallbackSync` (PRD §27.9).

**No-PK tables.** Tables with no primary key (and no `tables.<name>.primary_key.columns`
override — see PRD §8.6) are skipped from generation entirely by `BuildTableContexts`
(PRD §9.4b) — `BuildCacheContext` never sees them. No client method exists to
mutate the table, so no cache code path is reachable for it. Users who want the
table generated must declare a PK via `primary_key.columns`, after which the
standard cache rules above apply.

---

## 13. Hydration

When `hydration.enabled: true` and `Get` is called with non-nil `FieldOptions`
that contains ONLY column flags (no relationship flags — relationship-loaded
Gets bypass the cache entirely per §11):

1. Cache miss path: DB query with the partial projection runs → result returned to caller immediately.
2. A goroutine is launched with `context.WithTimeout(ctx, hydration.timeout)`.
3. Goroutine queries the DB for the full parent entity using an **explicit** `FieldOptions`: every column flag set to `true`, every relationship flag set to `false`. `SkipHooks: true` to avoid recursion.
4. Serializes full parent entity → `cache.Set(key, full, ttl)`.
5. Failures are logged via `MetricsRecorder.HydrationComplete(table, err)` and never propagate.

**Why explicit `FieldOptions` instead of nil.** A nil `FieldOptions` in the
generator's query path means "default" — and the default could include
relationship loading depending on the generator's current behavior or a future
change. Caching a nil-FieldOptions result risks bundling relationship data
into the parent entry, which violates the one-key-one-table invariant (§11).
The hydration path explicitly builds a per-table `{Table}FieldOptions{ColA:
true, ColB: true, ..., Rel1: false, ...}` struct — generated once per cached
table — so the hydration query is guaranteed to return parent columns only,
regardless of what "nil `FieldOptions`" means elsewhere in the codebase now or
later.

Generated helper, one per cached table:

```go
// fullParentFieldOptionsProduct returns a FieldOptions with every column flag
// set to true and every relationship flag set to false. Used by the hydration
// path to guarantee parent-only results without depending on the default
// semantics of nil FieldOptions.
func fullParentFieldOptionsProduct() *ProductFieldOptions
```

Goroutine lifecycle:

- Launched via a generated `hydrate` helper, not inline in the hook.
- Does **not** inherit the caller's `context.Context` cancellation (the caller's goroutine may return before hydration finishes). It inherits deadlines via a fresh timeout-only context.
- Runs at most one hydration per `(table, pk)` at a time — de-duplicated via a `sync.Map` keyed by cache key.

**Dedup-map lifecycle.** The `sync.Map` is a sentinel tracker, not a result
cache. Entries are added at hydration launch and removed when the hydration
goroutine exits — success, failure, or timeout — via a `defer`. Sketch:

```go
func (c *Cache) hydrate(key string, ...) {
    if _, loaded := c.hydrating.LoadOrStore(key, struct{}{}); loaded {
        return // another goroutine is already hydrating this key
    }
    go func() {
        defer c.hydrating.Delete(key)
        ctx, cancel := context.WithTimeout(context.Background(), c.hydrationTimeout)
        defer cancel()
        // ... query full entity, Set on cache ...
    }()
}
```

- **Always-delete on exit.** Failed hydrations release the slot the same way successful ones do. The next caller's cache miss relaunches, identical to a cold-miss path — this preserves the "if you see a cache miss, hydration will run" contract.
- **No cooldown between failures.** A persistently failing DB query (timeout, auth error) means every miss for that key relaunches hydration. This is intentional for v1 — the circuit breaker (§17) catches sustained cache/DB failures through the metrics path (`HydrationComplete(table, err)` increments the error counter), and TTL keeps the partial-read path usable while hydration is broken. If real-world usage shows retry-storm pain, add a per-key failure-backoff in a follow-up.
- **Memory bound is "in-flight hydrations."** Because entries exist only for the duration of a running goroutine, the map cannot grow unboundedly over process lifetime — its size is bounded by the number of concurrent hydrations in flight. At steady state with a healthy DB, hydration completes in tens of ms, so the map is nearly empty.
- **No `Close` plumbing.** `Cache.Close()` does not wait on in-flight hydrations; the generated facade cancels the hydration context(s) implicitly via process teardown (or explicitly via a facade-owned `context.CancelFunc` wired through `Close` — decided in 12.8 based on how the existing terminal goroutines handle shutdown). Either way, the map is released with the `*Cache` instance on GC.

When `hydration.enabled: false` + partial fetch: bypass cache entirely. Subsequent
full fetches populate the cache normally.

---

## 14. Invalidation

TTL + three-layer source-driven invalidation surface (Layer 1 `FromEventSubscriber`,
Layer 2 `InvalidationSource`, Layer 3 direct `Cache.Invalidate*` calls) all live
in PRD §27.9. This section retains the transaction-safety implementation sketch
(§14.3) that drives the generated mutation hook.

### 14.3 Transaction-Safe Invalidation

When a mutation hook detects it is running inside a transaction (via
`database.FromContext(ctx)`), it registers the cache operation on `tx.OnCommit`
rather than firing immediately. Rollback discards the registration.

```go
if tx := database.FromContext(ctx); tx != nil && !tx.IsClosed() {
    tx.OnCommit(func(ctx context.Context) error {
        return c.invalidate(ctx, table, pks)
    })
} else {
    c.invalidate(ctx, table, pks)
}
```

**`tx.OnCommit` ordering contract (relied on by cache).** Cache invalidation's
relative timing vs. event publishing is not an accident — it depends on three
documented guarantees of `database.Tx` (Phase 11):

1. **FIFO within a depth.** `OnCommit` appends to `tx.callbacks[depth]`; `Commit` iterates the slice in order. Two callbacks registered in sequence fire in the same sequence. Codified by `database/transaction_test.go:TestOnCommitCallbackOrdering`.
2. **Order-preserving savepoint promotion.** When a savepoint releases, its callbacks are appended to the parent's slice (`append(parent, current...)`) — parent-registered callbacks still fire first, then the promoted ones.
3. **Rollback discards at-depth callbacks only.** Callbacks registered inside a rolled-back savepoint never fire; siblings and ancestors are untouched.

These guarantees make the §9.5 hook-chain reasoning work out:

- Cache is the outermost mutation hook, so its `post` block runs LAST — meaning its `tx.OnCommit(...)` registration happens AFTER the event hook's (the event hook is inner, finishes earlier).
- On commit, FIFO means events fire first, cache invalidation fires second. Cached reads during event handlers can still observe the pre-invalidation state, which is the semantic users expect from "events describe what happened" while "cache invalidation clears stale data."

**Callback context caveat.** `CallbackMode` defaults to `CallbackAsync`; the async path executes callbacks on a fresh `context.Background()` — not the original request context. Cache invalidation logic **must not read request-scoped metadata** (tracing spans, auth identities, request deadlines) from the callback's `ctx`. Use the key + table data already closed over in the callback; that's all the backend call needs. Setting `CallbackMode: CallbackSync` propagates the original ctx at the cost of blocking the commit return until every callback completes — usually not what cache users want.

**Retry behavior.** Both sync and async paths retry a failed callback once before giving up (sync returns the error, async logs it). Cache invalidation is idempotent (§5.4 / §22 decision #1), so a retry-on-transient-error is safe. A persistently failing invalidation means the backend is unhealthy — the circuit breaker (§17) will notice via `MetricsRecorder.Error` accumulation and short-circuit future reads.

**Tracked contract.** The three ordering guarantees above are cross-referenced
in the sync-back list so `database/transaction.go`'s `OnCommit` godoc can be
expanded to state them explicitly. Until that godoc change lands, the test
(`TestOnCommitCallbackOrdering`) is the live regression gate.

`Set` operations after `Create` follow the same rule. This mirrors the pattern
Phase 11.3 used for events and is required by PRD §18.6.

`FromEventSubscriber` and custom `InvalidationSource` inputs are **already
post-commit** (events only fire after commit), so invalidation from those layers
is fired immediately — no nested `OnCommit` needed.

---

## 15. View Cache Invalidation

See PRD §27.11 for opt-in rule, rationale, YAML config, and validation
semantics. Implementation sketch — generated into `cache_gen.go`:

```go
func (c *Cache) patternsForSourceTable(table hook.TableName) []string {
    switch table {
    case TableProducts: return []string{"sqlgen:public.product_summary:*"}
    case TableReviews:  return []string{"sqlgen:public.product_summary:*"}
    }
    return nil
}
```

The mutation hook calls this after processing entity invalidation:

```go
for _, pattern := range c.patternsForSourceTable(m.Table) {
    c.backend.InvalidatePattern(ctx, pattern)
}
```

Patterns are fingerprint-agnostic (§10.1): one call clears every fingerprint
generation of the view. No capability check — `InvalidatePattern` is mandatory
on `Backend` (§5.1).

---

## 16. `SkipCache` Semantics

See PRD §27.7 "`CallOptions.SkipCache` interaction" and §9.6. The cache hook
additionally suppresses hydration when `SkipCache` is set — no background
goroutine is launched. `SkipHooks` implies `SkipCache` (wired in Phase 7).

---

## 17. Circuit Breaker Design

The authoritative transition table lives at §5.7. Config-driven defaults
(`failure_threshold: 5`, `probe_interval: 30s`, `half_open_max_probes: 1`)
appear in PRD §27.2. One breaker per `*Cache` instance; per-table breakers
are a deferred feature pending demand.

---

## 18. Per-Dialect Considerations

See PRD §27.5 for the schema-segment rules. Nothing dialect-specific lives in
the cache layer itself — the hook sits above the SQL layer. The generator
normalizes `input.schema == ""` to `"public"` for PostgreSQL at render time
and emits `""` for MySQL / SQLite; key helpers treat `schema == ""` as "omit
the segment."

---

## 19. Code Generation

### 19.1 Generator Context Additions

`cmd/sqlgen/gen/context.go` `ClientContext` gets:

```go
type ClientContext struct {
    // ... existing ...
    CacheEnabled   bool            // from resolved global config
    CacheConfig    CacheRenderCtx  // bake into generated defaults
    CachedTables   []CachedTable   // tables with caching enabled
    CachedViews    []CachedView    // views with caching enabled
    ViewInvalidateMap map[string][]string  // source_table -> view_pattern list
}

type CachedTable struct {
    Name        string           // table name ("products")
    Struct      string           // Go struct name ("Product")
    Schema      string           // PostgreSQL: "public" (or user value); MySQL/SQLite: ""
    PKGoType    string           // "uuid.UUID" | "int64" | ...
    PKColumns   []string         // composite support — always non-empty for CachedTable entries
    TTL         time.Duration
    Serializer  Serializer
    Fingerprint string           // 8-char hex, computed per §10.1
}

// BuildCacheContext never sees no-PK tables: BuildTableContexts skips any table
// without a primary key (after override resolution) before context build —
// see PRD §9.4b. Cached tables therefore always have a non-empty PKColumns slice.

type CachedView struct {
    Name         string
    Struct       string
    Schema       string
    TTL          time.Duration
    Serializer   Serializer
    InvalidateOn []string
    Fingerprint  string          // 8-char hex, computed per §10.1
}
```

Builder: `cmd/sqlgen/gen/context_cache.go` with `BuildCacheContext(root config.RootConfig)` — mirrors `context_event.go`.

### 19.2 New Templates

| Template | Output file | Purpose |
|---|---|---|
| `cache.go.tmpl` | `cache_gen.go` | `Cache` struct, factory (`NewCache`), options, public methods, internal helpers, per-table key builders, view invalidation map. |

**Conditional imports in `cache.go.tmpl`:**

| Config | Emitted import | Default serializer expression |
|---|---|---|
| `serializer: json` (or unset) | none beyond `cache/` | `cache.JSONSerializer{}` |
| `serializer: msgpack` | `import sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"` | `sqlgenmsgpack.New()` |
| `serializer: custom` | none | `nil` — `NewCache` errors if `WithSerializer` is absent |

The template chooses the expression via a `{{ if eq .CacheConfig.Serializer "msgpack" }}` branch. The same pattern is already used in `client.go.tmpl` for driver selection.

### 19.3 Modified Templates

| Template | Change |
|---|---|
| `client.go.tmpl` | Add `WithCache(c *Cache) Option` and wire it into `New()`. Hook chain ordering per §9.5. |
| `shared_types.go.tmpl` | Already carries `SkipCache`. No change. |

Per-operation templates (`table/create.go.tmpl`, etc.) require **no changes** —
cache logic is entirely hook-based, and terminals already populate `AffectedPKs`.

### 19.4 Conditional Emission

`cache_gen.go` is emitted only when `cache.enabled: true`. When disabled:

- No cache files generated.
- `WithCache` option is not emitted.
- `shared_types_gen.go` still has `SkipCache` (harmless — no-op without a cache hook).

This matches the Phase 11.3 pattern for `event_hooks_gen.go`.

### 19.5 Generated File Artifacts

For an events-off, cache-on config:

```
<output_dir>/
├── client_gen.go
├── shared_types_gen.go
├── cache_gen.go    ← new
├── table_gen.go per table
└── ...
```

For a cache-on, events-on config:

```
<output_dir>/
├── client_gen.go
├── shared_types_gen.go
├── event_hooks_gen.go
├── cache_gen.go
└── ...
```

---

## 19A. CLI Linter — Cache Call Sites

The `sqlgen lint` subcommand **already exists** (`cmd/sqlgen/cli/lint.go`) and
scans consumer Go code for mistakes in `hook.ForMutation` / `hook.ForQuery`
registrations. Cache call-site checks land as **additional rules** on top of
that infrastructure — not as a new subcommand.

Scope decision: type-aware analysis via `golang.org/x/tools/go/packages` (same
deps powering staticcheck, gopls, goimports). AST-only lint was considered and
rejected — it would catch only literal-arg mistakes, and real consumer code
almost never passes PK literals. PKs come from request params, struct fields,
database results, or function returns. Without type resolution, the linter
would report `info` ("cannot verify") on 80%+ of real call sites and catch
nothing useful. For a DX-focused library, that's not worth shipping.

### What already exists (reuse as-is)

- `sqlgen lint` Cobra subcommand with `--paths` (defaults to `cfg.Output.Dir`) and `--fail-on error|warning|info`.
- Three severity levels: `severityInfo` / `severityWarning` / `severityError`.
- `lintIssue` struct + `formatLintOutput`, `sortIssues`, `hasIssuesAtOrAbove`.
- `typeRegistry` + `tableInfo` built from the parsed schema/config — already maps `TableProducts` → `{StructName, Ops, HasSoftDelete, HasIncrement, ...}`.
- AST analysis using `go/parser`, `go/ast`, `go/token` for the existing hook-registration rules (which only need name-level matching — no type resolution).
- `_gen.go` and `_test.go` skipped automatically.

The existing hook-registration rules stay AST-based — they inspect literal
type-parameter names (e.g., `hook.ForMutation[*CreateProductInput]`) which is
textual and doesn't need type info. Cache rules layer type-aware analysis
alongside them.

### What cache lint adds

1. **Extend `tableInfo`** with PK metadata populated inside `buildTypeRegistry`:
   ```go
   type tableInfo struct {
       // ... existing fields ...
       PKGoType     string   // single-PK: "uuid.UUID" / "int64" / "string" / ...
       PKImportPath string   // single-PK: "github.com/google/uuid" (if named type)
       PKComposite  bool
       PKStructName string   // composite only: "OrderItemPK"
       PKFields     []PKField // composite only; DDL column order + types
   }

   type PKField struct {
       Name     string // "OrderID"
       GoType   string // "uuid.UUID"
       ImportPath string
   }
   ```

2. **New type-aware pass** (`lintCacheCallsTyped` in a new file `cmd/sqlgen/cli/lint_cache.go`). Uses `packages.Load` with `NeedTypes | NeedTypesInfo | NeedSyntax | NeedImports` to resolve every expression's static type:
   ```go
   cfg := &packages.Config{
       Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
             packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
       Dir:  scanDir,
   }
   pkgs, err := packages.Load(cfg, "./...")
   ```

3. **Call-site scanner** walks each loaded package's syntax tree looking for `*<facade>.Invalidate` / `.InvalidateMany` / `.InvalidateTable` method calls. Uses `types.Info.Selections` / `types.Info.Types` to confirm the receiver is the generated `*<pkg>.Cache` type and to get the static type of every argument — including variables, field accesses, and function-call results.

4. **`validateCacheCall`** compares resolved types to `tableInfo` expectations. Emits `lintIssue` records through the existing pipeline.

### Diagnostics

| Pattern | Severity | Message |
|---|---|---|
| Single-PK table + PK arg's static type differs from `PKGoType` | error | `cache.Invalidate(TableProducts): PK arg is {T}, expected uuid.UUID` |
| Composite-PK table + non-matching struct type | error | `cache.Invalidate(TableOrderItems): expected OrderItemPK, got UserPK` |
| Composite-PK table + scalar (non-struct) PK arg | error | `cache.Invalidate(TableOrderItems): expected OrderItemPK struct, got {T}` |
| `InvalidateMany` with `[]any` elements whose resolved types don't match | error | `cache.InvalidateMany(TableOrderItems): element at {pos} is {T}, expected OrderItemPK` |
| `InvalidateMany` with a homogeneous typed slice (e.g. `[]uuid.UUID`) passed as `[]any` | error if element type wrong; info if the call pattern prevents element access | `slice element type {T} does not match expected PK type` |
| First arg is `hook.TableName(expr)` or a variable whose value can't be resolved | info | `dynamic table name — cache arg types not statically checked` |
| Receiver is a `*<pkg>.Cache` returned from a factory whose output type can't be traced | info | `cache receiver type unresolved — skipping` |

Severities match the existing lint philosophy: **error** for provable
mismatches; **warning** reserved for existing semantic checks; **info** for
genuinely unknowable cases (dynamic-table callers).

### Additional opportunities type-aware analysis unlocks (future rules)

Once `x/tools/go/packages` is wired in, several other high-value rules become
possible at low incremental cost:

- Detect `c.Invalidate(ctx, TableProducts, &productID)` (pointer passed where value expected — common mistake when migrating from ORMs).
- Detect PK-argument type confusions across **related** tables (e.g., passing `UserID` to `TableOrders` when the schema has `orders.user_id` — correct column, wrong table).
- Detect `hook.ForMutation` registrations whose callback body accesses a field that doesn't exist on the typed input (currently AST-only can't see through the type parameter).

These aren't part of the cache lint MVP but the infrastructure supports them
when we want to add them.

### New dependencies (CLI module only — runtime stays stdlib)

- `golang.org/x/tools/go/packages`

Same dep used by staticcheck, gopls, goimports, revive. Not exotic.

### Why a stretch goal, not blocking

- Runtime dispatch (§9.3) already surfaces mismatches with descriptive errors at call time — this is a build-time UX upgrade, not a safety requirement.
- Generated `XXXPK` structs already give composite-PK callers field-named, order-fixed constructors; the Go compiler already rejects many PK-type confusions at build time.
- The dynamic-table case (Layer 2 `InvalidationSource`) remains inherently non-lintable — the analyzer cannot close that gap even with full type info.

### Proposed phase positioning

Track as **12.12** with dependency on 12.8 (generator emits the `hook.TableName`
constants and PK type metadata the registry consumes). Can slip to a later
phase without blocking caching itself.

---

## 20. Testing Strategy

### 20.1 Unit Tests (in `cache/`)

- `cache_test.go`: interface shape tests (compile-time assertions on core + optional interfaces).
- `serializer_test.go`: JSON round-trip for each generated struct shape.
- `key_test.go`: key grammar for simple/composite/empty-schema cases.
- `invalidation_test.go`: `FromEventSubscriber` translation, batching.
- `breaker_test.go`: state transitions per the §5.7 full transition table. Minimum case coverage: (a) `Closed` + `RecordSuccess` resets counter, (b) `Closed` counter advances to N-1 without tripping, (c) Nth consecutive `RecordFailure` trips to `Open` and fires `OnStateChange(Closed, Open)`, (d) `Allow()` in `Open` returns false until `ProbeInterval` elapses, (e) `Allow()` post-interval transitions to `HalfOpen` and fires `OnStateChange(Open, HalfOpen)`, (f) `HalfOpen` probe success returns to `Closed` with counter reset, (g) `HalfOpen` probe failure re-opens immediately (default `HalfOpenMaxProbes=1`), (h) `probesInFlight` saturation bypasses followers, (i) `RecordSuccess` / `RecordFailure` received while `Open` are ignored, (j) concurrent `Allow()` + `RecordX` correctness (race-free transitions).
- `error_test.go`: `DefaultOnCacheError` log format; unified policy fires all three channels in order (metrics → `OnErrorFunc` → `breaker.RecordFailure`); a panicking `OnErrorFunc` does not skip breaker accounting; nil `MetricsRecorder` is zero-cost.
- `singleflight_test.go`: exercises the read-through dedup wrapper in the generated facade. Concurrent `Get(pk)` calls for the same key collapse to a single backend + DB round-trip; distinct keys run in parallel; leader DB errors propagate to every waiter; follower context cancellation returns `ctx.Err()` without canceling the leader's in-flight `fn`; a subsequent caller after a failed leader re-enters `fn` (no permanent slot leak); **and a flight registered after a sibling flight completed re-probes `backend.Get` inside the callback and returns the populated value without entering `next`** (FIX-070 late-straggler regression guard; the in-flight re-check itself emits no metrics — outer probe already accounted for `Miss` / `breaker.RecordSuccess` / `GetLatency`).
- `typed_test.go`: `GetAs` / `SetAs` / `GetOrSet` round-trips, miss semantics (zero + false), `load` error propagation without stray `Set`, cache error propagation. `KeysFromAny[T]` happy path, type-mismatch error format (names index, expected, actual), empty-slice case.
- `noop_test.go`: `NoopBackend` satisfies `Backend` + `StatsReporter`; `Get` always misses; `Invalidate*` all succeed as no-ops.

### 20.2 Unit Tests (in backends)

- `cache/memory/memory_test.go`: Get/Set/Invalidate/InvalidateMany/InvalidatePattern/Stats/Close; TTL expiry; iteration-based pattern invalidation.
- `cache/redis/redis_test.go`: testcontainers-backed (`testcontainers-go/modules/redis`) — real SCAN pagination + DEL batching + never-KEYS, `redis.Nil` → miss, `WithScanCount` arg flow, stats counters, owned-client Close.
- `cache/msgpack/msgpack_test.go`: round-trip, struct-tag compatibility.

### 20.3 E2E Tests

New example: `cmd/sqlgen/testdata/examples/cache/` (SQLite-based, mirrors the `events/` example).

- `sqlgen.yml` with `cache.enabled: true`, a per-table override disabling cache, a view with `invalidate_on`.
- Tests using `cache/memory/` (no external infra):
  - Read-through: cache miss → DB → hit on second call.
  - Full-entity invariant: partial fetch with hydration on → cache populated; hydration off → not populated.
  - All mutation ops trigger correct invalidation.
  - Key-based invalidation on `*Where` ops — one `InvalidateMany` over the
    matched PKs, no table pattern wipe, nothing at all on a zero-match
    predicate or an uncached table (FIX-208).
  - View invalidation on source-table mutation.
  - `SkipCache` bypasses both paths.
  - Per-table `cache.enabled: false` suppresses all cache activity for that table.
  - Direct `Cache.Invalidate*` methods.
  - Circuit breaker — inject a failing backend, assert state transitions and metric emission.
  - Transaction safety: commit triggers invalidation, rollback does not.

- Golden file comparison for `cache_gen.go`.
- Regression guard: existing examples (no cache) must not emit `cache_gen.go`.

Cross-dialect coverage is implicit — the cache hook sits above the SQL layer. Existing
`postgres/`, `mysql/`, `sqlite/` examples act as conditional-emission regression guards.

### 20.4 Redis Integration

Runs as part of the normal test suite — no env-var gate:

- testcontainers-based Redis (`github.com/testcontainers/testcontainers-go`).
- `testing.Short()` skip guard for fast-feedback loops (`go test -short`) — matches the project convention (CLAUDE.md: "Testcontainers for integration tests, not mocks. Skip with `testing.Short()`").
- Same assertions as the in-memory suite, plus SCAN-vs-KEYS behavior and DEL batching.
- CI runs the full suite (no `-short`). Local developers can choose between fast iteration (`go test -short ./cache/...`) and full coverage (`go test ./cache/...`).
- Lives in `cache/redis/redis_integration_test.go` alongside the unit tests. No separate test package or job needed.

---

## 21. Phase 12 Task Breakdown (proposed)

| # | Task | Depends on |
|---|---|---|
| 12.1 | `cache/` core — `Backend` interface, key helpers, metrics, serializer, breaker, typed helpers (`GetAs`/`SetAs`/`GetOrSet`/`KeysFromAny`), `NoopBackend` | — |
| 12.2 | `cache/` invalidation — `InvalidationSource`, `FromEventSubscriber` | 12.1 |
| 12.3 | Config — `CacheConfig`, `TableCacheConfig`, `ViewCacheConfig`, `HydrationConfig`, `CircuitBreakerConfig`, `Serializer` enum, resolvers, validation | — |
| 12.4 | `cache/memory/` backend | 12.1 |
| 12.5 | `cache/redis/` backend | 12.1 |
| 12.6 | `cache/msgpack/` serializer | 12.1 |
| 12.7 | `metrics/otel/` `MetricsRecorder` implementation | 12.1 |
| 12.8 | Generator — `BuildCacheContext`, `cache.go.tmpl`, `client.go.tmpl` additions, conditional emission | 12.1, 12.2, 12.3 |
| 12.9 | E2E example + tests (in-memory backend, SQLite) | 12.4, 12.8 |
| 12.10 | Redis integration test suite (testcontainers, `testing.Short()` skip guard, runs in the default test suite) | 12.5, 12.8 |
| 12.11 | Sync design back into `docs/PRD.md` §4.9 + §27 | all |
| 12.12 | **Stretch:** extend existing `sqlgen lint` with type-aware cache call-site rules (§19A). Adds `golang.org/x/tools/go/packages` to the CLI module (runtime stays stdlib). Extends `tableInfo` with PK metadata, adds `lint_cache.go` with type-aware call-site scanner, wires `validateCacheCall` into the existing diagnostic pipeline. | 12.8 |

`/phase 12` should generate `docs/tracker/phase-12.md` with these sub-items and acceptance criteria derived from this document.

---

## 22. Design Decisions

1. **Pattern invalidation is mandatory; stats/close remain optional.** `Cache` carries `InvalidatePattern` as a required method because view cache invalidation, `InvalidateTable` and the tenant capture-gap fallback are core features — a backend that cannot do pattern invalidation silently breaks them. (Mutation invalidation itself is key-based on every op since FIX-208, `*Where` included.) `StatsReporter` is opt-in (not every wrapper can report stats honestly). Graceful shutdown uses stdlib `io.Closer` rather than a custom `Closeable` interface — same signature, standard vocabulary, and backends that already implement `Close() error` for other reasons satisfy it for free. `Invalidate` / `InvalidateMany` / `InvalidatePattern` use invalidation terminology throughout to match the domain vocabulary.
2. **`JSONSerializer` default in core, msgpack as a submodule, generator-injected.** Zero-config works on stdlib alone. When `serializer: msgpack` is configured, the generator emits the `cache/msgpack` import and the default serializer expression — same pattern as driver selection. Consumers never have to write `WithSerializer(msgpack.New())` boilerplate just to match config. `WithSerializer(...)` remains available for preconfigured pools and custom serializers.
3. **Direct invalidation uses `Backend.Invalidate` / `InvalidateMany` / `InvalidatePattern`.** Same methods the mutation hook calls. The generated `*Cache.Invalidate*` methods are thin wrappers over the backend — they add key building, metrics, and circuit-breaker treatment.
4. **First-party backends: `cache/memory/` + `cache/redis/`.** In-memory + distributed coverage, each isolated in its own `go.mod`. The in-memory backend is named by capability (not by library) so the `maypok86/otter/v2` dependency remains an implementation detail.
5. **Connection lifecycle ownership is opt-in.** Redis wrapper does not close caller-owned clients unless `WithOwnedClient()` is passed.
6. **Cache hook is the outermost hook.** Cache hits short-circuit before user hooks and events; no false user-hook / event fires on a cached read.
7. **`NewCache(backend, ...opts)` — no config struct, named handle retained.** Events collapse to `WithEventPublisher(publisher)` because the event system has no user-facing runtime API post-construction. Cache keeps a named handle because users need it for Layer 3 direct invalidation (`c.Invalidate` / `InvalidateMany` / `InvalidateTable`) and for explicit `Close()` of the invalidation subscription + backend. The factory does NOT take a `CacheConfig` argument — config (per-table TTL, serializer, key prefix, hydration, circuit-breaker defaults) is baked into the generated factory at codegen time so runtime calls cannot drift from the YAML. Functional options are runtime-only overrides of codegen defaults, matching the `WithEventPublisher(publisher, ...configFns)` pattern.
8. **Circuit breaker in runtime package, not generated.** Shared implementation; all tables use the same breaker instance. Per-table breakers deferred until demand.
9. **Pattern grammar: prefix + trailing `*`.** Lowest common denominator across backends. Backends may support more for direct user calls.
10. **`cache/memory` `MaxSize` required.** No silent default. Users must size their cache explicitly (the underlying library mandates a bound).
11. **Redis `StatsReporter` uses local counters by default.** More useful than server-wide `INFO` numbers. Toggle via `WithLocalStats(false)` for zero-overhead mode.
12. **`QueryContext` unchanged.** Existing fields (`Op`, `Table`, `Schema`, `PK`, `Input`, `CallOptions`) are sufficient for read-through + hydration decisions. Verified against `hook/hook.go`.
13. **No per-operation template changes.** Cache lives entirely in the hook layer. Terminal mutations already populate `AffectedPKs` (Phase 11.3).
14. **View caching is opt-in; tables inherit from global.** Tables inherit `cache.enabled` from the global setting because the mutation hook invalidates them automatically. Views do not — they have no mutations of their own, so a missing `invalidate_on` means silent TTL-bounded staleness. Views require explicit per-view `cache.enabled: true` plus a non-empty `invalidate_on`. TTL-only view caches are not supported in v1.
15. **Typed user helpers are free functions, not a generic `Backend[T]`.** Go does not support generic methods on interfaces, so a generic `Backend[T]` would force one adapter per entity type over the same backing client. Instead, `cache/typed.go` exposes `GetAs[T]` / `SetAs[T]` / `GetOrSet[T]` / `KeysFromAny[T]` as generic free functions over the type-erased `Backend` interface. Users get typed ergonomics at call sites; the backend interface stays single-instance-per-client. The generated facade never calls these helpers — it inlines the marshal/unmarshal path directly since the entity type is known at codegen time.
16. **Composite key ordering is enforced by the generated `XXXPK` struct; per-table keyers are typed.** `BuildCompositeKey` concatenates without inspecting columns, so ordering is the caller's contract. The generator is the only code path that invokes it — per-table `keyForX(pk T) string` helpers (with concrete `T`, not `any`) read struct fields in declared order inside a single function per composite table. `MutationContext.AffectedPKs` is populated in canonical order by terminals. For user-direct invalidation of composite-PK tables, callers pass the generated `XXXPK` struct literal (e.g. `OrderItemPK{OrderID: ..., ProductID: ...}`) — field names are mandatory, field order is fixed by the struct declaration, so the call site cannot silently reorder. No per-table typed invalidation methods are generated; the struct IS the typed guardrail, and a generic `cache.KeysFromAny[T]` helper collapses the dispatch switch to one line per table. Typed keyers also turn wrong-type PK arguments into loud errors — the prior `pk any` design silently stringified `42` on a UUID-PK table. Reordering PK columns in a migration bumps every cache key for that table — cold-start, not stale-data.
17. **Cache lint rules extend `sqlgen lint` with type-aware analysis.** The generic `Invalidate` / `InvalidateMany` methods enforce type correctness at runtime via the per-table dispatch switch. For build-time checking, the existing `cmd/sqlgen/cli/lint.go` — which already lints `hook.ForMutation` / `hook.ForQuery` registrations with AST-only analysis — gains cache rules using `golang.org/x/tools/go/packages`. Existing rules stay AST-based (they only need literal type-parameter name matching); cache rules need full type resolution because real consumer code passes PK variables, struct fields, and function results, not literals. AST-only cache lint would miss 80%+ of real call sites. The one-new-dep cost is the same library used by staticcheck / gopls / goimports. Dynamic-table callers (Layer 2 `InvalidationSource`) remain inherently non-lintable and covered by the runtime path. Tracked as Phase 12.12 stretch; not MVP but part of the v1 DX story.
18. **Relationship-loading calls bypass the cache.** Relationships are expressed as boolean fields on the per-table `{Table}FieldOptions` struct (mixed with column flags, not a separate `WithXxx()` layer). o2o uses JOIN inline in the parent `Get`; o2m/m2m use separate `GetMany` calls (already uncached). Bundling the JOIN result into the parent's cache entry would violate the one-key-one-table invariant and create cross-entity invalidation dependencies (a child mutation chasing up to invalidate every parent that joined to it). Caching parent-only and re-joining on hit would silently drop relationship data or require an extra query per hit — wiping out the JOIN performance win. Bypass is the only approach that preserves the invariant without hiding complexity: cache-through when `FieldOptions` has only column flags, direct DB JOIN when any relationship flag is set. The JOIN is still a single query — no regression relative to today. Users who need cached relationship data compose it from cached parent + child `Get` calls client-side. Composition from multiple caches is a future optimization path that can land without changing the invariant.
19. **Hydration uses an explicit parent-only `FieldOptions`, never nil.** A nil `FieldOptions` means "default" in the generator's query path — and the default could include relationship loading now or in a future release. The hydration goroutine cannot depend on ambient default semantics; it must positively state "all column flags true, all relationship flags false." A generated `fullParentFieldOptions{Table}()` factory produces the struct per cached table so the hydration query is guaranteed to populate the cache with parent-only rows regardless of how nil-FieldOptions defaults evolve elsewhere.
20A. **Automatic per-table schema fingerprint in the cache key.** Cached entries are scoped by a short per-table fingerprint (§10.1) computed at codegen time from the table's serialized-wire-format inputs: column name/type/struct-tag, PK column order, serializer identity, plus the optional `cache.version` kill switch. Keys look like `sqlgen:public.products:fingerprint:v1a2b3c:pk:42` rather than `sqlgen:public.products:42`. Any migration that regenerates the Go struct produces new fingerprints → new key namespace; old entries orphan and get cleaned up by backend LRU/TTL, or by a one-shot `Cache.InvalidateTable(ctx, ...)` since pattern invalidation is fingerprint-agnostic (§10, `BuildTablePattern` matches every generation). Rejected alternatives: (a) TTL-as-rollover exposes a staleness window during migrations; (b) in-payload magic-byte/version prefix bloats every value and couples serializer logic to versioning; (c) global cache version requires operator discipline on every deploy. The fingerprint is the only option that keeps correctness automatic without compromising stable-key workloads — byte-identical struct regenerations keep the same fingerprint so the cache stays warm. Key-length cost is ~25 bytes per entry with the labeled grammar — still negligible against the value payload.

20. **Read-through stampede protection uses `golang.org/x/sync/singleflight` with an in-flight cache re-check.** The `Cache` facade holds a `singleflight.Group` keyed by the full cache key. Concurrent read-through misses for the same `(table, pk)` collapse to one backend-`Get` → DB → backend-`Set` round-trip; followers share the result. Reusing `x/sync/singleflight` rather than rolling our own deduper: the subrepo is already a runtime dep (`guidelines/ARCHITECTURE.md` lists `golang.org/x/sync/errgroup` for generated relationship loading), maintained by the Go team, and stable. Purpose-built equivalents add surface area without buying anything. Applies only to the `Get` path — mutations and invalidations are per-operation atomic at the backend, and hydration retains its own `sync.Map` dedup (§13) for a different path (parallel background refills of a partial-read key). Followers honor their own context via `sf.DoChan` + select, so a cancelled follower returns `ctx.Err()` without aborting the leader. The flight callback re-probes `backend.Get` before calling `next` so a flight registered after a sibling's flight completed still returns the populated value without a duplicate DB round-trip — `singleflight` releases each in-flight slot the moment `fn` returns, so without the re-check stragglers that observed the outer miss before the sibling populated would slip past dedup (FIX-070; see §9.3).

---

## 23. Open Questions (all tentatively resolved — confirm before `/phase 12`)

### Q1. Pattern grammar contract

**Proposal (adopted in §5.1 / §10):** prefix + trailing `*`. Backends may support more for direct user calls but the contract is minimal.

### Q2. `cache/memory` `MaxSize` default

**Proposal (adopted in §6.1):** required. `New` fails when `MaxSize <= 0`.

### Q3. Redis `Stats` semantics

**Proposal (adopted in §6.2):** local counters for hits/misses/sets/invalidations; `Entries` from INFO when available. Controlled by `WithLocalStats` (default on).

### Q4. Factory option shape

**Revised proposal (adopted in §9.1):** `NewCache(backend, ...opts)` — no `CacheConfig` argument. Config is baked into the generated factory at codegen time; options are runtime-only overrides of the generator-baked defaults. Parallels `WithEventPublisher(publisher, ...configFns)`. The named handle is retained because cache exposes a runtime API (`Invalidate*`, `Close`) that events does not.

### Q5. Circuit breaker configurability

**Proposal (adopted in §3.1 / §5.7):** `CacheConfig.CircuitBreaker` with `failure_threshold: 5`, `probe_interval: 30s`, `half_open_max_probes: 1`. Per-table overrides deferred.

### Q6. `QueryContext` shape

**Resolved:** existing fields are sufficient (§2). No changes needed to `hook/hook.go`.

### Q7. Msgpack → serializer binding failure mode

**Revised proposal (adopted in §3.4 / §7 / §19.2):** msgpack is auto-injected by the generator. When `cache.serializer == "msgpack"`, `cache.go.tmpl` emits `import sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"` and defaults the serializer to `sqlgenmsgpack.New()`. No construction-time error; the consumer runs `go mod tidy` once and the dependency only lands in projects that opted in. Same mechanism used for conditional driver selection. Only `serializer: custom` still errors at construction when `WithSerializer(...)` is absent — there is no library for the generator to inject.

### Q8. Hydration goroutine lifetime

**Proposal (adopted in §13):** fresh context with `hydration.timeout`, independent of caller context. De-dup via `sync.Map` keyed by cache key — at most one hydration in flight per `(table, pk)`.

---

## 24. Unresolved Gaps (to be closed before `/phase 12`)

Distinct from §23 (tentatively-resolved open questions). These are specification
holes surfaced during a pre-implementation review — none have a proposed
resolution baked into the design yet. Resolve top-to-bottom; items 1–5 shape
the design itself, 6–10 shape sub-task acceptance criteria, 11–13 are doc
tweaks that land in the sync-back pass.

Each entry names the affecting sections, the questions to answer, and (where
useful) a non-binding proposal to react to.

### Must resolve before `/phase 12` (design-defining)

#### G1. `FromEventSubscriber` batching & error semantics

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal.
- **Affects:** §5.4 (updated), §20.1 `invalidation_test.go`
- **Resolution:**
  1. **Synchronous per event.** Each `event.Event` produces exactly one `InvalidationHandler` call with `pks = []any{ev.PK}`. No coalescing window. Publisher-side batching (`PublishBatch`) is preserved transparently — transports fan out one `Handler` call per event, the adapter forwards 1:1.
  2. **Idempotent by backend contract.** `Backend.Invalidate` / `InvalidateMany` for an absent key is a success. At-least-once transports (NATS redelivery, Kafka replay) are safe without `event.ID` dedup — the adapter keeps no per-event state.
  3. **Error path:** record via `MetricsRecorder.Error(table, "invalidate", err)` AND return the error from the inner `event.Handler` so ACK-capable transports redeliver. Fire-and-forget transports drop it after their own error step. Nothing is silently swallowed.
- **Follow-ups (carry into phase-12 tasks):**
  - `20.1 invalidation_test.go` must assert: (a) 1 event → 1 `InvalidationHandler` call, (b) handler error is both recorded on the `MetricsRecorder` and returned from the event `Handler`, (c) redelivered events (same `event.ID`) produce a second no-op invalidation without error.
  - Document the "absent key is success" rule in `Backend` godoc so custom backends honor it.

#### G2. Cache stampede / singleflight policy

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal: use `golang.org/x/sync/singleflight` directly. **Refined 2026-05-06 (FIX-070)** to add an in-flight `backend.Get` re-check at the top of the flight callback, closing the gap between the outer probe and flight registration.
- **Affects:** §4 (updated), §9.3 (updated), §20.1 (updated), §22 (decision #20 updated), `guidelines/ARCHITECTURE.md` (sync-back: add `singleflight` entry alongside `errgroup`)
- **Resolution:**
  1. **`singleflight.Group` in the facade, keyed by cache key, with an in-flight `backend.Get` re-check.** Concurrent read-through misses for the same `(table, pk)` collapse to a single backend `Get` → DB query → backend `Set`. The flight callback re-probes `backend.Get` before invoking `next` so a flight that registers after a sibling's flight completed still returns the populated entity instead of running a duplicate DB query — `singleflight` releases each in-flight slot the moment `fn` returns, so the slot-relinquish window is what late stragglers (outer-miss-observed-but-not-yet-`DoChan`-registered callers) would otherwise slip through.
  2. **Dep is already blessed.** `golang.org/x/sync/errgroup` is an accepted runtime dep per `guidelines/ARCHITECTURE.md`. Reusing the same subrepo for `singleflight` does not meaningfully expand the dependency surface.
  3. **Scope is Get-only.** Mutations and invalidations do not participate. Runs inside the circuit-breaker gate — open breaker short-circuits before reaching `sf.Do`. The in-flight re-check is also breaker-gated, so an open breaker skips both probes and falls through to `next`.
  4. **Context-aware followers.** Implementation uses `sf.DoChan` + `select` on `<-ch` / `<-ctx.Done()` so a cancelled follower returns `ctx.Err()` without aborting the leader. Leader error propagates to all waiters; `singleflight` releases the slot post-return so the next caller retries.
  5. **Coexists with hydration dedup.** Hydration keeps its own `sync.Map` (§13); both mechanisms cover different paths (full-entity read-through vs. background parallel refill of partial-read keys).
  6. **Re-check is silent (FIX-070).** The inner re-check emits no metrics — the outer probe already recorded `Miss` / `breaker.RecordSuccess` / `GetLatency` for this caller; the inner re-check is dedup machinery, not an observable cache event. An inner-Get error is silently dropped (the outer probe already routed any backend error through `RouteError`); the callback falls through to `next` exactly as if the inner Get had returned a clean miss.
- **Follow-ups (carry into phase-12 tasks):**
  - `20.1 singleflight_test.go` must assert: concurrent same-key `Get` calls collapse to one backend+DB round-trip, distinct keys run in parallel, leader errors propagate to all waiters, follower `ctx.Done` returns `ctx.Err()` without canceling the leader, a follow-up call after a failed leader re-enters `fn`, **and a flight registered AFTER a sibling flight completed re-probes `backend.Get` and returns the populated value without entering `next`** (the FIX-070 late-straggler regression guard — see `cmd/sqlgen/testdata/examples/cache/tests/read_through_test.go::TestReadThrough_SingleflightLateStraggler` for a deterministic implementation that forces the ordering via a `firstMissBackend` wrapper).
  - Sync-back to `guidelines/ARCHITECTURE.md`: add a `golang.org/x/sync/singleflight` row to the "Key Runtime Dependencies" table, Used By: `cache/`, Purpose: Read-through stampede protection.
  - Design decision #20 in §22 captures the chosen policy.

#### G3. `tx.OnCommit` ordering vs. outer-most cache hook

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal. Verified against the existing implementation; no runtime changes needed.
- **Affects:** §9.5 (updated), §14.3 (expanded), §25 (new godoc sync-back row)
- **Verification (against `database/transaction.go` as of this writing):**
  - `OnCommit` appends at `tx.callbacks[tx.depth]` (`transaction.go:104`).
  - Savepoint release promotes via `append(parent, current...)` preserving order (`transaction.go:245`).
  - `fireCallbacks` iterates the slice in order (`transaction.go:401`).
  - `TestOnCommitCallbackOrdering` (`transaction_test.go:251-278`) asserts `[1, 2, 3]` order — live regression gate.
- **Resolution:**
  1. **FIFO within depth is a documented `database.Tx` contract.** §14.3 now spells out the three guarantees cache relies on: (1) FIFO within depth, (2) order-preserving savepoint promotion, (3) rollback discards at-depth callbacks only.
  2. **Cache depends on FIFO for the cache-fires-after-events property.** §9.5 now calls this out explicitly rather than treating the ordering as emergent from "outermost layer registers last."
  3. **Async callback context is `context.Background()`, not the request ctx.** §14.3 documents that cache invalidation must not read request-scoped metadata (tracing spans, deadlines) from the callback's ctx — closure-captured key + table is all that's needed. `CallbackMode: CallbackSync` is the escape hatch if the original ctx is required, at the cost of blocking the commit return.
  4. **Retry-once is compatible with idempotent invalidation.** Both sync and async paths retry a failed callback once before giving up. Cache `Invalidate*` is idempotent by contract (§5.4), so the retry is safe; persistent failures show up as `MetricsRecorder.Error` accumulation and drive the circuit breaker (§17).
- **Follow-ups (carry into phase-12 tasks):**
  - Expand `database/transaction.go:OnCommit` godoc from its current 3-line description to cover the three guarantees explicitly. Added as a new row in §25 (godoc sync-backs outside `docs/PRD.md`).
  - Phase-12 integration tests should include one case asserting event-then-cache-invalidation commit ordering, so any future `OnCommit` refactor that breaks FIFO trips a cache test, not just the `database` test.

#### G4. `MutationContext.AffectedPKs` shape for composite-PK tables

- **Status:** **Resolved** (2026-04-20) — existing Phase 11.3 terminals already satisfy the `Cache.Invalidate*` contract. No runtime or generator changes needed.
- **Affects:** §2 (updated with verification), §25 (new `hook/hook.go` godoc sync-back row)
- **Verification (read against current golden generated files):**
  - Single-PK update: `update_products_gen.go:78` → `m.AffectedPKs = []any{id}` where `id` is the scalar PK (`uuid.UUID`).
  - Single-PK batch: `update_products_gen.go:186` → `m.AffectedPKs = toAnySlice(allIDs)` over `[]uuid.UUID`.
  - Composite-PK update: `update_order_items_gen.go:65` → `m.AffectedPKs = []any{pk}` where `pk` is `OrderItemPK` struct value.
  - Composite-PK batch: `update_order_items_gen.go:161` → `m.AffectedPKs = toAnySlice(allPKs)` over `[]OrderItemPK`.
  - `hook.MutationContext.AffectedPKs` godoc (`hook/hook.go:62`) says "used by event hooks" — functionally correct but doesn't yet document the element-shape contract.
- **Resolution:**
  1. **Element shape matches §9.2's contract exactly.** For single-PK tables the element is the scalar PK value; for composite-PK tables it's the generated `XXXPK` struct value. This is true across every terminal (update, update-many, delete, delete-many, upsert, create, increment).
  2. **No pack step needed in the generated cache hook.** `c.InvalidateMany(ctx, m.Table, m.AffectedPKs)` works directly — `cache.KeysFromAny[T]` per-element type-asserts to the concrete single-PK scalar or composite `XXXPK` struct.
  3. **Lock in the invariant with a godoc expansion.** `hook/hook.go:62` `AffectedPKs` comment is expanded to state the element-shape contract explicitly, so a future terminal refactor that breaks it trips godoc review. Tracked in §25's "Godoc sync-backs outside PRD" table.
- **Follow-ups (carry into phase-12 tasks):**
  - Update `hook/hook.go:62` godoc per §25.
  - E2E test in the cache example must cover a composite-PK table round-trip: mutation → `tx.OnCommit` → `InvalidateMany(table, AffectedPKs)` → cache miss on next `Get(pk)`. If a future terminal regression drops the struct-value shape (e.g. emits flat scalars), the composite case's `KeysFromAny[OrderItemPK]` assertion will fail and the test will catch it.

#### G5. Serializer schema evolution

- **Status:** **Resolved** (2026-04-20) — rejected original TTL-as-rollover proposal; adopted automatic per-table schema fingerprinting instead.
- **Affects:** §3.1 (yaml `version` knob), §3.2 (`CacheConfig.Version` field), §5.6 (`BuildKey` / `BuildCompositeKey` signatures gain `fingerprint` parameter), §9.3 (per-table `fingerprint{Table}` constants), §10 (key grammar updated, new §10.1 / §10.2), §19.1 (`CachedTable.Fingerprint` / `CachedView.Fingerprint` fields), §22 (new decision #20A), §25 (PRD §27.5 sync-back expanded)
- **Rationale for rejecting TTL-as-rollover:** Users explicitly flagged that waiting out TTL during migration windows is a bad user experience — staleness during the window is a real correctness risk, not just a warmup cost. Manual `InvalidateTable` calls on every migration are easy to forget. The design needs automatic handling.
- **Rationale for rejecting in-payload magic byte:** Bloats every stored value, forces all backends + serializers to understand the wrapper, and couples versioning to the serializer layer. Per-key fingerprinting is cleaner and uses the existing key grammar.
- **Resolution (fingerprint approach):**
  1. **Codegen-time per-table fingerprint.** 8-char sha256-truncation over column name/type/struct-tag (in DDL order), PK column order, serializer identity, and the optional `cache.version` knob. Baked as a `const fingerprint{Table}` in `cache_gen.go`.
  2. **Injected into the key as a dedicated labeled segment.** Grammar becomes `{prefix}:{schema}.{table}:fingerprint:v{fp}:pk:{pk}` — self-describing labels, with `pk:` as the terminal segment (PRD §27.5). `BuildKey` / `BuildCompositeKey` gain a `fingerprint` parameter.
  3. **Pattern invalidation is fingerprint-agnostic.** `BuildTablePattern` keeps the old form (`{prefix}:{schema}.{table}:*`), so `InvalidateTable` clears both current and orphaned generations in one pass. (`*Where` invalidations shared that path until FIX-208 moved every mutation op onto key-based eviction; orphan sweeping is now item 5's job alone.)
  4. **Cache miss (not decode failure) on fingerprint mismatch.** Structural separation — an old-fingerprint key is literally a different key, so a `Get` with the new fingerprint misses without touching the stored bytes. No "is this payload decodable" check needed.
  5. **Orphan cleanup is automatic.** Backend LRU/TTL handles it; operators who want instant cleanup can call `Cache.InvalidateTable(ctx, ...)` post-deploy.
  6. **Rolling-deploy-safe.** Old and new pods use different fingerprints and coexist without serving wrong-shape data. Brief DB load bump during deploy; correctness preserved.
  7. **Manual kill switch.** `cache.version: N` (yaml) or `CacheConfig.Version` (Go) feeds every fingerprint. Bump it to force a global cache cold-start on next `sqlgen generate`.
- **Follow-ups (carry into phase-12 tasks):**
  - `12.3` — add `Version int` to `CacheConfig` parsing + validation.
  - `12.1` — `cache/key.go`: update `BuildKey` / `BuildCompositeKey` signatures to take `fingerprint`; `BuildTablePattern` unchanged. `key_test.go` adds fingerprint-presence + composite-fingerprint cases.
  - `12.8` — `BuildCacheContext` computes fingerprints at codegen time; `cache.go.tmpl` emits `const fingerprint{Table}` per cached table/view.
  - `12.9` — E2E test: generate with `cache.version: 1`, populate cache, regenerate with `cache.version: 2` (same schema), assert `Get` misses and repopulates under new fingerprint. Separate E2E test: add a column to a cached table's DDL, regenerate, assert cache miss on re-deploy.
  - Document that schema-changing migrations should coincide with an `sqlgen generate` step (usually true today) so the fingerprint moves with the DDL.
  - Sync-back to PRD §27.5 (see §25) covers the user-facing documentation.

### Should resolve before implementation starts (acceptance-criteria level)

#### G6. `Cache.InvalidateTable` implementation path

- **Status:** **Resolved** (2026-04-20) — `InvalidateTable` routes through a generated per-table schema-resolving switch into the internal `c.invalidatePattern` helper, which calls `backend.InvalidatePattern` with the same metrics + circuit-breaker treatment as `c.invalidateKeys`.
- **Affects:** §9.3 (new `InvalidateTable` dispatch block + internal-helper-symmetry note), §14.2 (misleading comment corrected)
- **Resolution:**
  1. **Same switch skeleton as `InvalidateMany`, different body.** Each cached table contributes one line that resolves the table's `schema` constant; then the helper builds the pattern via `cache.BuildTablePattern(c.prefix, schema, string(table))` and hands off to `c.invalidatePattern(ctx, table, pattern)`.
  2. **Unknown-table error is identical to `InvalidateMany`'s.** `fmt.Errorf("cache: unknown or non-cached table %q", table)` — a typo or a non-cached table fails loudly instead of silently succeeding.
  3. **Fingerprint-agnostic pattern (G5 integration).** `BuildTablePattern` does not include the fingerprint segment (§10.1), so one call clears both current-generation and orphaned entries. Useful right after a schema-migration deploy.
  4. **Internal sibling of `invalidateKeys`.** `invalidatePattern` centralizes metrics (`metrics.Invalidate(schema, table)`, `metrics.Error(schema, table, "invalidate_pattern", err)`), breaker `RecordSuccess`/`RecordFailure`, and `InvalidateLatency` timing. Differs only in which backend method it dispatches to. Neither internal is public — the §9.2 method list stays three-public (`Invalidate`, `InvalidateMany`, `InvalidateTable`).
- **Follow-ups (carry into phase-12 tasks):**
  - `12.8` — `cache.go.tmpl` emits the `InvalidateTable` switch alongside the `InvalidateMany` switch. One `{{ range .CachedTables }}` block can source both.
  - `20.1` / `20.3` — test cases for: (a) `InvalidateTable(TableProducts)` clears every key in the table including orphans from a bumped-fingerprint generation, (b) `InvalidateTable(hook.TableName("unknown"))` returns the canonical unknown-table error, (c) `invalidatePattern` failure routes to `MetricsRecorder.Error` + breaker `RecordFailure`.

#### G7. Hydration `sync.Map` lifecycle

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal.
- **Affects:** §13 (expanded with a "Dedup-map lifecycle" subsection)
- **Resolution:**
  1. **`defer c.hydrating.Delete(key)` at goroutine top.** Entry is released on every exit path — success, failure, or timeout. No separate success-vs-failure branching.
  2. **Failed hydrations release the slot.** Next miss relaunches, same as a cold-miss path. Preserves the "if you see a cache miss, hydration will run" contract.
  3. **No cooldown between failures in v1.** Circuit breaker (§17) + `MetricsRecorder.HydrationComplete(table, err)` error-counter already provide the safety net against sustained failures. A per-key backoff is demand-driven — revisit if retry-storm pain surfaces.
  4. **Memory bound is "in-flight hydrations."** Map size is bounded by the number of concurrent running hydration goroutines — cannot grow unboundedly over process lifetime. At steady state with a healthy DB, near-empty.
  5. **`Close()` lifecycle deferred to 12.8.** Whether `Cache.Close()` actively waits on in-flight hydrations or just cancels them via a facade-owned `context.CancelFunc` will match whatever pattern the existing terminal goroutines use (Phase 11 precedent).
- **Follow-ups (carry into phase-12 tasks):**
  - `12.1` (or 12.8, depending on where hydration helpers land): implement with the `LoadOrStore` + `defer Delete` pattern shown in §13.
  - Test case: launch 50 concurrent `Get(pk)` miss-hydrations for the same key; assert exactly one DB full-fetch runs, map returns to empty after completion. Second case: force the hydration DB query to error; assert map returns to empty, and a subsequent miss relaunches a fresh hydration.

#### G8. Cache-error swallow policy in mutation path

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal in full. Unified three-pronged policy applies to every cache backend error (`Get` / `Set` / `Invalidate` / `InvalidateMany` / `InvalidatePattern`).
- **Affects:** §4 (added `error.go` to layout), §5.1 (updated the `InvalidatePattern`-error paragraph to cite §5.8), §5.8 (new "Error Policy" subsection), §5.9 (renumbered from old §5.8), §9.1 (added `WithOnError` option)
- **Resolution:**
  1. **Metrics.** `MetricsRecorder.Error(table, op, err)` — structured, zero-cost when no recorder wired.
  2. **`OnErrorFunc` callback.** Textual log breadcrumb; default `DefaultOnCacheError` logs to stderr via `log.Printf` (mirrors `event.DefaultOnError`). Swappable via `WithOnError` option — users can route through `slog`, `zap`, etc.
  3. **Breaker accounting.** Every routed error calls `breaker.RecordFailure()`; sustained errors open the circuit per §17.
  4. **Three channels fire in order** (metrics → OnErrorFunc → breaker) so a misconfigured logger cannot mask a cache outage. `OnErrorFunc` is wrapped in `defer recover()` at the call site.
  5. **Marshal / unmarshal errors route through the same policy.** Unmarshal failures additionally treat the call as a cache miss; TTL + fingerprint segment (§10.1) handle stale-payload cleanup, so no automatic entry deletion.
  6. **Caller's error path is always clean.** The mutation / query caller never sees cache backend errors — the DB operation's success/failure is what propagates.
- **Follow-ups (carry into phase-12 tasks):**
  - `12.1` — implement `cache/error.go` with `OnErrorFunc`, `DefaultOnCacheError`, and the internal `routeError(ctx, op, table, err)` helper called from `invalidateKeys` / `invalidatePattern` / read-through / write-through paths.
  - `12.1` `error_test.go` — all-three-channels-fire, OnErrorFunc-panic-safe, nil-MetricsRecorder-zero-cost.
  - `12.8` — generator bakes `DefaultOnCacheError` into the generated facade unless `WithOnError` is passed. Same zero-cost pattern as `MetricsRecorder`.

#### G9. Circuit breaker counter reset rules

- **Status:** **Resolved** (2026-04-20) — adopted non-binding proposal. Full transition table now lives in §5.7 as the authoritative reference.
- **Affects:** §5.7 (expanded with full transition table + invariants), §20.1 `breaker_test.go` (acceptance cases enumerated)
- **Resolution:**
  1. **`Closed` + `RecordSuccess` resets counter to 0.** Any success breaks the consecutive-failure streak.
  2. **Nth consecutive `RecordFailure` in `Closed` trips to `Open`.** Counter stays at N; `OpenedAt = now()`; `OnStateChange(Closed, Open)` fires.
  3. **`Open` → `HalfOpen` driven by `Allow()` timer check.** No background goroutine. First caller post-`ProbeInterval` observes the transition.
  4. **`HalfOpen` success → `Closed`, counter resets to 0.** First probe success closes the breaker.
  5. **`HalfOpen` failure → `Open` immediately.** One probe failure is enough (default `HalfOpenMaxProbes = 1`); counter resets to N, `OpenedAt = now()`, `OnStateChange(HalfOpen, Open)` fires.
  6. **`probesInFlight` gates concurrent `HalfOpen` callers.** Atomic increment on `Allow() = true`; followers get `false` until the probe resolves. `RecordSuccess`/`RecordFailure` release the slot.
  7. **`RecordX` received while `Open` is ignored.** Only `HalfOpen` probes drive recovery; in-flight calls that started before the trip don't contribute to reopening.
  8. **No counter arithmetic in `Open` / `HalfOpen`.** Counter is meaningful only in `Closed` state.
- **Follow-ups (carry into phase-12 tasks):**
  - `12.1` — implement `cache/breaker.go` per the §5.7 transition table. Use atomic operations for `counter`, `probesInFlight`, and a `sync.Mutex` (or `atomic.Pointer[breakerSnapshot]`) for the compound `state + OpenedAt` transitions.
  - `12.1` `breaker_test.go` — cover cases (a) through (j) enumerated in §20.1.
  - Race test: run `go test -race` explicitly in CI for the breaker — concurrent `Allow()` + `RecordX` must not drop or double-count state transitions.

#### G10. `key_prefix: ""` validation

- **Status:** **Resolved** (2026-04-20) — `key_prefix` is optional; empty or unset defaults to `"sqlgen"` at parse time, with a stderr warning from `sqlgen generate` when the default is applied. No hard error, no leading-colon edge case.
- **Affects:** §3.1 (YAML comment annotated), §3.4 (new validation rule + warning requirement)
- **Resolution:**
  1. **Empty or unset → default `"sqlgen"`.** Applied during YAML parse, before any resolver or generator touches the config. Same shape as `cache.ttl` / `cache.serializer` / `cache.hydration.timeout` defaults.
  2. **Default baked in at codegen time.** The generated facade stores the resolved prefix as a constant (or field initialized at `NewCache`), so the `BuildKey` / `BuildCompositeKey` contract always holds — no runtime "empty prefix → drop leading segment" special case in the helpers.
  3. **Stderr warning when the default applies.** `sqlgen generate` emits: `sqlgen: cache.key_prefix unset — using default "sqlgen". Set cache.key_prefix explicitly to silence this warning.` Surfaces the silent-collision risk for multi-tenant / shared-Redis deployments. Suppressed by setting any non-empty string. Both unset and explicit `""` trigger the warning (same resolved value).
  4. **Non-default values used verbatim.** No further content validation beyond "must be a string." Users setting a multi-tenant prefix (e.g. `tenant42:sqlgen`) get exactly what they typed, no warning.
- **Follow-ups (carry into phase-12 tasks):**
  - `12.3` — add `key_prefix` default handling next to `ttl` / `serializer` / `hydration.timeout` defaults. Emit the stderr warning from the same code path; reuse whatever warning-emission helper the CLI already has (check `cmd/sqlgen/cli/` for the existing pattern — likely a `fmt.Fprintln(os.Stderr, ...)` or a leveled logger).
  - `12.3` config tests: (a) unset `key_prefix` resolves to `"sqlgen"` AND the warning is emitted, (b) explicit `""` resolves to `"sqlgen"` AND the warning is emitted, (c) explicit `"custom"` resolves to `"custom"` AND no warning is emitted. Capture stderr in the test to assert the warning text.

### Minor / doc tweaks (batch into sync-back)

#### G11. §14.2 Layer 3 example uses a raw string for `table`

- **Status:** **Resolved** (2026-04-20) — §14.2 Layer 3 examples now use the generated `<pkg>.TableProducts` constant (typed `hook.TableName`) instead of raw strings. The dynamic-table case shows `hook.TableName(rawTableStr)` as the explicit cast pattern for Layer 2 handlers.
- **Affects:** §14.2 (Layer 3 example block updated)
- **Resolution:**
  1. **Prefer generated `<pkg>.TableXxx` constants.** Consistent with the §9.2 call-site examples. Typos fail at compile time instead of silently dispatching through the unknown-table switch (which would surface as `"cache: unknown or non-cached table %q"` at runtime).
  2. **Keep `hook.TableName(str)` cast as the dynamic-table form.** Explicit at the boundary where the string enters the cache API (CDC payloads, webhook table fields, etc.) — not sprinkled through Layer 3 examples where the table is statically known.
- **Follow-ups:** None — doc-only change.

#### G12. Pointer-typed PKs in `cache.SetAs[T]`

- **Status:** **Resolved** (2026-04-20) — "PK types are values, not pointers" documented in §10 as a hard rule, and §5.9's `KeysFromAny` godoc extends the constraint to `GetAs` / `SetAs` / `GetOrSet` when keys are built via the helpers in §5.6.
- **Affects:** §10 (new "PK types are values, not pointers" paragraph), §5.9 (`KeysFromAny` godoc extended to cite the rule)
- **Resolution:**
  1. **Value-only PK rule in §10.** Pointer types %v-stringify to memory addresses; silently breaks every call. Explicitly forbidden for `BuildKey` / `BuildCompositeKey` / `KeysFromAny` / `GetAs` / `SetAs` / `GetOrSet`.
  2. **Generated path is already safe.** Per-table `keyForX(pk T)` helpers use concrete value `T`, so `c.Invalidate` / `InvalidateMany` / `InvalidateTable` cannot hit this mistake. The rule exists for user-direct callers (`cache.SetAs`, custom `InvalidationSource`, admin tools, tests).
  3. **Boundary conversion pattern.** Documentation tells users to dereference at the boundary (`*p`) before calling cache helpers.
- **Follow-ups:** None — doc-only change.

#### G13. OTel metric labels omit `schema`

- **Status:** **Resolved** (2026-04-20) — `schema` is now a first-class label on every per-table OTel instrument and a leading parameter on every per-table `MetricsRecorder` method. Same-name tables in different schemas stay distinct.
- **Affects:** §5.5 (interface updated), §5.8 (`OnErrorFunc` signature + `DefaultOnCacheError`), §8 (instrument table updated + label-value note added), §9.3 (call-site sketch updated — `c.metrics.Miss(q.Schema, q.Table)`, `invalidatePattern` metrics calls), §24 G6 (internal-helper metrics-call sketch), §25 (PRD §27.13 sync-back entry expanded)
- **Resolution:**
  1. **`schema string` added as the leading parameter to every per-table `MetricsRecorder` method.** Applies to `Hit` / `Miss` / `Set` / `Invalidate` / `Error` / `HydrationStart` / `HydrationComplete` + the three latency histograms.
  2. **`schema` label added to every per-table OTel instrument.** `sqlgen.cache.{hits,misses,sets,invalidations,errors,hydrations,get.duration,set.duration,invalidate.duration}`. The `sqlgen.cache.circuit_breaker` gauge stays un-schema-labeled because the breaker is `*Cache`-scoped, not per-table.
  3. **Empty schema is a valid label value.** MySQL / SQLite emit `schema=""`. OTel and Prometheus both treat empty-label values as first-class dimensions, so mixed-dialect deployments stay queryable without recording-rule gymnastics.
  4. **`OnErrorFunc` signature consistency.** Cache error policy (§5.8) routes schema + table together, and `DefaultOnCacheError` omits the schema segment in log output when the value is empty so MySQL / SQLite logs stay tidy.
  5. **Generated call sites always have schema in hand.** The generated `*Cache` facade resolves per-table schema at codegen time (§9.3 `switch table → schema` in `InvalidateTable`, and the per-operation hooks already read `q.Schema` / `m.Schema` from `QueryContext` / `MutationContext`). Nothing new needs to be plumbed through.
- **Follow-ups (carry into phase-12 tasks):**
  - `12.1` — `cache/metrics.go` `MetricsRecorder` interface carries the schema parameter.
  - `12.7` `metrics/otel/` — instruments are created with both `schema` and `table` attributes.
  - `12.7` test: record a `Hit` with `schema="public"` and a second with `schema="archive"` against the same table name; assert OTel collector sees two distinct series.

---

## 25. Sync-Back Targets in `docs/PRD.md`

**Status: Verified in sync (2026-04-22, Phase 12.11).** All PRD and godoc
sync-backs below have been applied and re-verified row-by-row against the
current `docs/PRD.md` and adjacent source files — no drift detected since
the original 2026-04-20 landing. This table is preserved as a provenance
trail — reviewers can trace any particular piece of design rationale here
back to the exact PRD location it landed in (`git log -- docs/PRD.md`
scoped to `12.x` commit messages gives the SHA trail per row).

| PRD § | Change |
|---|---|
| §4.8 TableConfig | Add `cache` field pointing to §27. |
| §4.9 ViewConfig | Add `cache` and `invalidate_on` (already documented there — ensure `invalidate_on` validation aligns with §3.4 here). Document that view caching is opt-in (does not inherit from global `cache.enabled`). |
| §9.6 CallOptions | Confirm `SkipCache` row matches §16 here (already correct). |
| §27.2 Configuration | Add `circuit_breaker` block to the YAML schema. |
| §27.5 Cache Key Generation | Document the composite-key ordering invariant (PKs joined in DDL PRIMARY KEY column order) and the schema-migration caveat (reordering PK columns invalidates every cache entry for that table — cold-start event, not a staleness risk). Also document the per-table schema fingerprint segment (§10.1) and the fingerprint-agnostic pattern behavior (`BuildTablePattern` matches every fingerprint generation). Include the `cache.version` kill-switch knob (§10.2). |
| §27.6 Cache Invariant + §27.7 Cache Behavior by Operation | Add the relationship-bypass rule: any `Get` call with a relationship-typed flag set on `FieldOptions` (o2o JOIN, o2m/m2m separate loads) bypasses the cache entirely — no read, no write. Preserves the one-key-one-table invariant without hiding complexity. Add a corresponding row to §27.7's operation table. |
| §27.3 Cache Interface | Match §5.1 here — rename the backend interface from `Cache` to `Backend` (the user-facing facade in the generated package takes the `Cache` name). `Backend` includes `Invalidate` / `InvalidateMany` / `InvalidatePattern` as mandatory methods (replaces `Delete` / `DeleteMany` and folds in the previously-optional `PatternDeleter`). `StatsReporter` remains optional; graceful shutdown uses stdlib `io.Closer` instead of a custom `Closeable` interface. |
| §27.4 Injection via Hooks | Reference the `WithCache` shorthand from §9.4. |
| §27.9 Invalidation Strategies | Reference the breaker wiring from §5.7 + §17. |
| §27.10 Serialization | Reference the `WithSerializer` option from §7 and the validation rule from §3.4. |
| §27.13 Cache Metrics | Rename `CacheMetrics` → `MetricsRecorder` in the interface definition; add histogram methods (`GetLatency` / `SetLatency` / `InvalidateLatency`) and the corresponding OTel histogram instruments (`sqlgen.cache.get.duration` etc.). Reference the `metrics/otel/` package from §8. Add `schema` as a leading parameter to every per-table method and as a label on every per-table OTel instrument, so same-name tables in different schemas do not collapse into a single metric series (§8 / G13). `schema` is `""` for MySQL/SQLite. |

Godoc sync-backs outside `docs/PRD.md`:

| Target | Change |
|---|---|
| `database/transaction.go:OnCommit` godoc | Expand from the current 3-line description to state three guarantees cache + events rely on: (1) FIFO execution within a depth, (2) order-preserving savepoint promotion (parent callbacks first, then promoted ones), (3) rollback discards at-depth callbacks only. Reference `TestOnCommitCallbackOrdering` as the regression gate. |
| `hook/hook.go:MutationContext.AffectedPKs` godoc | Current comment reads "used by event hooks" — expand to "used by event hooks and cache invalidation" and document the element shape: scalar PK for single-PK tables, generated `XXXPK` struct value for composite-PK tables. The shape is already stable across every terminal (verified in G4); the godoc change just locks it in. |
| `guidelines/ARCHITECTURE.md` "Key Runtime Dependencies" | Add a `golang.org/x/sync/singleflight` row (Used By: `cache/`, Purpose: Read-through stampede protection). Alongside the existing `errgroup` entry. |
