// Package otel provides an optional OpenTelemetry cache.MetricsRecorder
// implementation. It lives in a separate Go module so the OpenTelemetry
// dependency only lands in consumer projects that opt in via
//
//	<pkg>.WithMetricsRecorder(sqlgenotel.New(nil))
//
// in the generated cache facade.
//
// The instrument table, label semantics, and schema-as-first-class-label
// rule live in PRD §27.13 and CACHE.md §8.
package otel

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/hook"
)

// meterName follows the OpenTelemetry convention of using the library
// import path as the meter name so dashboards and instrumentation scope
// filters can identify the emitter.
const meterName = "github.com/teandresmith/sqlgen/metrics/otel"

// Attribute keys. Kept as package-level constants so test fixtures can
// reference them by name rather than hard-coded strings.
const (
	attrKeySchema = "schema"
	attrKeyTable  = "table"
	attrKeyOp     = "op"
	attrKeyStatus = "status"
	attrKeyState  = "state"
)

// Instrument names. All live in the sqlgen.cache namespace (PRD §27.13).
const (
	instrHits              = "sqlgen.cache.hits"
	instrMisses            = "sqlgen.cache.misses"
	instrSets              = "sqlgen.cache.sets"
	instrInvalidations     = "sqlgen.cache.invalidations"
	instrErrors            = "sqlgen.cache.errors"
	instrHydrations        = "sqlgen.cache.hydrations"
	instrCircuitBreaker    = "sqlgen.cache.circuit_breaker"
	instrGetLatency        = "sqlgen.cache.get.duration"
	instrSetLatency        = "sqlgen.cache.set.duration"
	instrInvalidateLatency = "sqlgen.cache.invalidate.duration"
)

// Status label values emitted on sqlgen.cache.hydrations.
const (
	statusSuccess = "success"
	statusError   = "error"
)

// Compile-time assertion that recorder satisfies the interface.
var _ cache.MetricsRecorder = (*recorder)(nil)

// recorder implements cache.MetricsRecorder by fanning out each call to
// a pre-built OpenTelemetry instrument.
type recorder struct {
	hits          metric.Int64Counter
	misses        metric.Int64Counter
	sets          metric.Int64Counter
	invalidations metric.Int64Counter
	errors        metric.Int64Counter
	hydrations    metric.Int64Counter

	breakerState metric.Int64Gauge

	getLatency        metric.Float64Histogram
	setLatency        metric.Float64Histogram
	invalidateLatency metric.Float64Histogram
}

// New returns a cache.MetricsRecorder backed by the given OpenTelemetry
// MeterProvider. When provider is nil, otel.GetMeterProvider() (the
// global default) is used, which matches OpenTelemetry ecosystem
// convention and keeps wiring terse:
//
//	sqlgenotel.New(nil)
//
// Instrument-construction errors from the MeterProvider are intentionally
// ignored: OpenTelemetry's contract guarantees the returned instrument is
// always usable (falling back to a no-op implementation on error), so
// propagating the error would force every consumer to handle an effectively
// impossible failure mode.
func New(provider metric.MeterProvider) cache.MetricsRecorder {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	meter := provider.Meter(meterName)

	hits, _ := meter.Int64Counter(
		instrHits,
		metric.WithDescription("Cache hits."),
	)
	misses, _ := meter.Int64Counter(
		instrMisses,
		metric.WithDescription("Cache misses."),
	)
	sets, _ := meter.Int64Counter(
		instrSets,
		metric.WithDescription("Cache writes."),
	)
	invalidations, _ := meter.Int64Counter(
		instrInvalidations,
		metric.WithDescription("Cache invalidations."),
	)
	errs, _ := meter.Int64Counter(
		instrErrors,
		metric.WithDescription("Cache backend errors."),
	)
	hydrations, _ := meter.Int64Counter(
		instrHydrations,
		metric.WithDescription("Background hydrations completed, labeled by status."),
	)

	breakerState, _ := meter.Int64Gauge(
		instrCircuitBreaker,
		metric.WithDescription("Current cache circuit breaker state; value is 1 for the current state label."),
	)

	getLatency, _ := meter.Float64Histogram(
		instrGetLatency,
		metric.WithUnit("s"),
		metric.WithDescription("Backend Get latency."),
	)
	setLatency, _ := meter.Float64Histogram(
		instrSetLatency,
		metric.WithUnit("s"),
		metric.WithDescription("Backend Set latency."),
	)
	invalidateLatency, _ := meter.Float64Histogram(
		instrInvalidateLatency,
		metric.WithUnit("s"),
		metric.WithDescription("Backend Invalidate / InvalidateMany / InvalidatePattern latency."),
	)

	return &recorder{
		hits:              hits,
		misses:            misses,
		sets:              sets,
		invalidations:     invalidations,
		errors:            errs,
		hydrations:        hydrations,
		breakerState:      breakerState,
		getLatency:        getLatency,
		setLatency:        setLatency,
		invalidateLatency: invalidateLatency,
	}
}

// tableAttrs returns the schema / table attribute pair that labels every
// per-table instrument, followed by any extra attributes. Empty schema
// (MySQL / SQLite) is a valid value — OpenTelemetry and Prometheus both
// treat empty-string label values as first-class dimensions, so
// mixed-dialect deployments stay queryable.
func tableAttrs(schema string, table hook.TableName, extra ...attribute.KeyValue) []attribute.KeyValue {
	return append([]attribute.KeyValue{
		attribute.String(attrKeySchema, schema),
		attribute.String(attrKeyTable, tableLabel(schema, table)),
	}, extra...)
}

// tableLabel returns the bare SQL table name for the table label. The
// hook.TableName value is schema-qualified whenever the table has a schema
// ("public.products" on PostgreSQL, PRD §5.5), and the schema already has
// its own label, so the "schema." prefix is stripped: the series reads
// {schema="public", table="products"}, not table="public.products". With an
// empty schema (MySQL / SQLite) the value is already the bare name.
func tableLabel(schema string, table hook.TableName) string {
	if schema == "" {
		return string(table)
	}
	return strings.TrimPrefix(string(table), schema+".")
}

// Hit records a cache hit for the given schema / table pair.
func (r *recorder) Hit(schema string, table hook.TableName) {
	r.hits.Add(context.Background(), 1, metric.WithAttributes(tableAttrs(schema, table)...))
}

// Miss records a cache miss for the given schema / table pair.
func (r *recorder) Miss(schema string, table hook.TableName) {
	r.misses.Add(context.Background(), 1, metric.WithAttributes(tableAttrs(schema, table)...))
}

// Set records a cache write for the given schema / table pair.
func (r *recorder) Set(schema string, table hook.TableName) {
	r.sets.Add(context.Background(), 1, metric.WithAttributes(tableAttrs(schema, table)...))
}

// Invalidate records a cache invalidation for the given schema / table pair.
func (r *recorder) Invalidate(schema string, table hook.TableName) {
	r.invalidations.Add(context.Background(), 1, metric.WithAttributes(tableAttrs(schema, table)...))
}

// Error records a backend error against the sqlgen.cache.errors counter.
// The op label identifies which cache operation faulted (one of "get",
// "set", "invalidate", "invalidate_many", "invalidate_pattern"). The err
// value itself is not attached — OpenTelemetry counters carry only
// numeric magnitudes, and error string cardinality is unbounded.
func (r *recorder) Error(schema string, table hook.TableName, op string, _ error) {
	r.errors.Add(context.Background(), 1, metric.WithAttributes(
		tableAttrs(schema, table, attribute.String(attrKeyOp, op))...,
	))
}

// HydrationStart is a no-op for the OpenTelemetry recorder. The
// sqlgen.cache.hydrations counter is defined with status ∈
// {success, error} (PRD §27.13), so hydration volume is recorded at
// completion by HydrationComplete. Other MetricsRecorder implementations
// are free to count starts independently.
func (r *recorder) HydrationStart(schema string, table hook.TableName) {}

// HydrationComplete records a completed background hydration on the
// sqlgen.cache.hydrations counter. A nil err sets status="success";
// a non-nil err sets status="error".
func (r *recorder) HydrationComplete(schema string, table hook.TableName, err error) {
	status := statusSuccess
	if err != nil {
		status = statusError
	}
	r.hydrations.Add(context.Background(), 1, metric.WithAttributes(
		tableAttrs(schema, table, attribute.String(attrKeyStatus, status))...,
	))
}

// CircuitBreakerStateChange records a single data point on the
// sqlgen.cache.circuit_breaker gauge with value 1 and a state label
// naming the new state. No schema / table attributes — the breaker is
// *Cache-scoped, not per-table.
func (r *recorder) CircuitBreakerStateChange(_, to cache.CircuitState) {
	r.breakerState.Record(context.Background(), 1, metric.WithAttributes(
		attribute.String(attrKeyState, to.String()),
	))
}

// GetLatency records a backend Get duration on the
// sqlgen.cache.get.duration histogram. The value is converted from
// time.Duration to seconds so the histogram unit (s) matches the reading.
func (r *recorder) GetLatency(schema string, table hook.TableName, d time.Duration) {
	r.getLatency.Record(context.Background(), d.Seconds(), metric.WithAttributes(tableAttrs(schema, table)...))
}

// SetLatency records a backend Set duration on the
// sqlgen.cache.set.duration histogram.
func (r *recorder) SetLatency(schema string, table hook.TableName, d time.Duration) {
	r.setLatency.Record(context.Background(), d.Seconds(), metric.WithAttributes(tableAttrs(schema, table)...))
}

// InvalidateLatency records a backend Invalidate / InvalidateMany /
// InvalidatePattern duration on the sqlgen.cache.invalidate.duration
// histogram.
func (r *recorder) InvalidateLatency(schema string, table hook.TableName, d time.Duration) {
	r.invalidateLatency.Record(context.Background(), d.Seconds(), metric.WithAttributes(tableAttrs(schema, table)...))
}
