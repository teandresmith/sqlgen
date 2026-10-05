package otel_test

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/hook"
	sqlgenotel "github.com/teandresmith/sqlgen/metrics/otel"
)

// newTestRecorder returns a cache.MetricsRecorder wired to an isolated
// MeterProvider backed by a ManualReader. Tests call reader.Collect to
// snapshot emitted instruments without depending on a timed export loop.
func newTestRecorder(t *testing.T) (cache.MetricsRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return sqlgenotel.New(provider), reader
}

// collect snapshots the current state of every instrument registered on
// the reader.
func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() unexpected error: %v", err)
	}
	return rm
}

// findMetric returns the metricdata.Metrics with the given name, or nil
// when the instrument has not yet been recorded against.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for i := range rm.ScopeMetrics {
		for j := range rm.ScopeMetrics[i].Metrics {
			if rm.ScopeMetrics[i].Metrics[j].Name == name {
				return &rm.ScopeMetrics[i].Metrics[j]
			}
		}
	}
	return nil
}

// sumInt64Point returns the int64 sum data point whose attribute set
// matches every want key/value pair. The attribute set must not contain
// additional keys beyond the expected ones — this is how the circuit-
// breaker test asserts that schema and table are NOT labels on the gauge.
func findSumPoint(t *testing.T, m *metricdata.Metrics, want map[string]string) *metricdata.DataPoint[int64] {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("metric %q aggregation = %T, want Sum[int64]", m.Name, m.Data)
	}
	return findPoint(t, m.Name, sum.DataPoints, want)
}

func findGaugePoint(t *testing.T, m *metricdata.Metrics, want map[string]string) *metricdata.DataPoint[int64] {
	t.Helper()
	g, ok := m.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("metric %q aggregation = %T, want Gauge[int64]", m.Name, m.Data)
	}
	return findPoint(t, m.Name, g.DataPoints, want)
}

func findPoint(t *testing.T, name string, points []metricdata.DataPoint[int64], want map[string]string) *metricdata.DataPoint[int64] {
	t.Helper()
	for i := range points {
		if attrsMatch(points[i].Attributes, want) {
			return &points[i]
		}
	}
	t.Fatalf("metric %q has no data point with attributes %v; got %v", name, want, attrsDump(points))
	return nil
}

// findHistogramPoint is the Float64Histogram analogue.
func findHistogramPoint(t *testing.T, m *metricdata.Metrics, want map[string]string) *metricdata.HistogramDataPoint[float64] {
	t.Helper()
	h, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("metric %q aggregation = %T, want Histogram[float64]", m.Name, m.Data)
	}
	for i := range h.DataPoints {
		if attrsMatch(h.DataPoints[i].Attributes, want) {
			return &h.DataPoints[i]
		}
	}
	t.Fatalf("histogram %q has no data point with attributes %v", m.Name, want)
	return nil
}

// attrsMatch reports whether attrs contains exactly the provided key/value
// pairs — every want entry present and equal, and no extra attributes.
func attrsMatch(attrs attribute.Set, want map[string]string) bool {
	if attrs.Len() != len(want) {
		return false
	}
	for k, v := range want {
		got, ok := attrs.Value(attribute.Key(k))
		if !ok || got.AsString() != v {
			return false
		}
	}
	return true
}

func attrsDump(points []metricdata.DataPoint[int64]) []map[string]string {
	out := make([]map[string]string, len(points))
	for i, p := range points {
		m := make(map[string]string, p.Attributes.Len())
		for _, kv := range p.Attributes.ToSlice() {
			m[string(kv.Key)] = kv.Value.AsString()
		}
		out[i] = m
	}
	return out
}

func TestHitDistinguishesSchemas(t *testing.T) {
	// Asserts that same-named tables in different schemas produce
	// distinct metric series via the schema attribute.
	rec, reader := newTestRecorder(t)

	// The facade passes each table's hook.TableName value, which carries
	// the schema on PostgreSQL (PRD §5.5).
	rec.Hit("public", hook.TableName("public.products"))
	rec.Hit("archive", hook.TableName("archive.products"))
	rec.Hit("public", hook.TableName("public.products")) // second hit on public

	m := findMetric(collect(t, reader), "sqlgen.cache.hits")
	if m == nil {
		t.Fatal("sqlgen.cache.hits not recorded")
	}

	public := findSumPoint(t, m, map[string]string{"schema": "public", "table": "products"})
	if public.Value != 2 {
		t.Errorf("sqlgen.cache.hits{schema=public,table=products} = %d, want 2", public.Value)
	}

	archive := findSumPoint(t, m, map[string]string{"schema": "archive", "table": "products"})
	if archive.Value != 1 {
		t.Errorf("sqlgen.cache.hits{schema=archive,table=products} = %d, want 1", archive.Value)
	}
}

func TestTableLabelIsTheBareSQLName(t *testing.T) {
	// The recorder receives the hook.TableName value, which is
	// "schema.table" whenever the table has a schema (PRD §5.5). The
	// schema has its own label, so the table label carries the bare SQL
	// name on every dialect: {schema="public", table="users"}, never
	// table="public.users". Every per-table method is covered,
	// including the two that add a third label.
	sentinel := errors.New("boom")
	type recordFn func(r cache.MetricsRecorder, schema string, table hook.TableName)
	methods := []struct {
		name       string
		instrument string
		extra      map[string]string
		histogram  bool
		call       recordFn
	}{
		{
			name: "Hit", instrument: "sqlgen.cache.hits",
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.Hit(s, tb) },
		},
		{
			name: "Miss", instrument: "sqlgen.cache.misses",
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.Miss(s, tb) },
		},
		{
			name: "Set", instrument: "sqlgen.cache.sets",
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.Set(s, tb) },
		},
		{
			name: "Invalidate", instrument: "sqlgen.cache.invalidations",
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.Invalidate(s, tb) },
		},
		{
			name: "Error", instrument: "sqlgen.cache.errors", extra: map[string]string{"op": "get"},
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.Error(s, tb, "get", sentinel) },
		},
		{
			name: "HydrationComplete", instrument: "sqlgen.cache.hydrations", extra: map[string]string{"status": "success"},
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.HydrationComplete(s, tb, nil) },
		},
		{
			name: "GetLatency", instrument: "sqlgen.cache.get.duration", histogram: true,
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.GetLatency(s, tb, time.Millisecond) },
		},
		{
			name: "SetLatency", instrument: "sqlgen.cache.set.duration", histogram: true,
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) { r.SetLatency(s, tb, time.Millisecond) },
		},
		{
			name: "InvalidateLatency", instrument: "sqlgen.cache.invalidate.duration", histogram: true,
			call: func(r cache.MetricsRecorder, s string, tb hook.TableName) {
				r.InvalidateLatency(s, tb, time.Millisecond)
			},
		},
	}
	inputs := []struct {
		name      string
		schema    string
		table     hook.TableName
		wantTable string
	}{
		{name: "postgres public", schema: "public", table: "public.users", wantTable: "users"},
		{name: "postgres second schema", schema: "audit", table: "audit.users", wantTable: "users"},
		{name: "schema named like the table", schema: "users", table: "users.users", wantTable: "users"},
		{name: "mysql sqlite no schema", schema: "", table: "users", wantTable: "users"},
		{name: "a dot in a schemaless name is kept", schema: "", table: "a.users", wantTable: "a.users"},
	}

	for _, m := range methods {
		for _, in := range inputs {
			t.Run(m.name+"/"+in.name, func(t *testing.T) {
				rec, reader := newTestRecorder(t)
				m.call(rec, in.schema, in.table)

				got := findMetric(collect(t, reader), m.instrument)
				if got == nil {
					t.Fatalf("%s not recorded", m.instrument)
				}
				want := map[string]string{"schema": in.schema, "table": in.wantTable}
				maps.Copy(want, m.extra)
				// Both finders fail the test unless one data point carries
				// exactly these attributes and no others.
				if m.histogram {
					findHistogramPoint(t, got, want)
				} else {
					findSumPoint(t, got, want)
				}
			})
		}
	}
}

func TestHitEmptySchemaIsValidDimension(t *testing.T) {
	// MySQL and SQLite emit schema="" — OTel treats empty strings as
	// first-class label values, so the data point must be queryable as
	// its own series.
	rec, reader := newTestRecorder(t)
	rec.Hit("", hook.TableName("products"))

	m := findMetric(collect(t, reader), "sqlgen.cache.hits")
	if m == nil {
		t.Fatal("sqlgen.cache.hits not recorded")
	}
	pt := findSumPoint(t, m, map[string]string{"schema": "", "table": "products"})
	if pt.Value != 1 {
		t.Errorf("sqlgen.cache.hits{schema=\"\",table=products} = %d, want 1", pt.Value)
	}
}

func TestCounterMethods(t *testing.T) {
	// Every counter-style method emits against the right instrument with
	// the schema+table attribute pair. Table-driven to catch a drift
	// between any method and its named instrument.
	const (
		schema = "public"
		table  = hook.TableName("products")
	)

	tests := []struct {
		name       string
		instrument string
		call       func(r cache.MetricsRecorder)
	}{
		{
			name:       "Miss",
			instrument: "sqlgen.cache.misses",
			call:       func(r cache.MetricsRecorder) { r.Miss(schema, table) },
		},
		{
			name:       "Set",
			instrument: "sqlgen.cache.sets",
			call:       func(r cache.MetricsRecorder) { r.Set(schema, table) },
		},
		{
			name:       "Invalidate",
			instrument: "sqlgen.cache.invalidations",
			call:       func(r cache.MetricsRecorder) { r.Invalidate(schema, table) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reader := newTestRecorder(t)
			tt.call(rec)

			m := findMetric(collect(t, reader), tt.instrument)
			if m == nil {
				t.Fatalf("%s not recorded", tt.instrument)
			}
			pt := findSumPoint(t, m, map[string]string{"schema": schema, "table": string(table)})
			if pt.Value != 1 {
				t.Errorf("%s{schema=%s,table=%s} = %d, want 1", tt.instrument, schema, table, pt.Value)
			}
		})
	}
}

func TestErrorAttachesOpLabel(t *testing.T) {
	// sqlgen.cache.errors must carry schema, table, AND op. The op label
	// is the recipe consumers use to filter "which cache call faulted".
	rec, reader := newTestRecorder(t)
	rec.Error("public", hook.TableName("products"), "invalidate_many", errors.New("boom"))

	m := findMetric(collect(t, reader), "sqlgen.cache.errors")
	if m == nil {
		t.Fatal("sqlgen.cache.errors not recorded")
	}
	pt := findSumPoint(t, m, map[string]string{
		"schema": "public",
		"table":  "products",
		"op":     "invalidate_many",
	})
	if pt.Value != 1 {
		t.Errorf("sqlgen.cache.errors = %d, want 1", pt.Value)
	}
}

func TestHydrationCompleteStatusLabel(t *testing.T) {
	// nil err → status="success"; non-nil err → status="error".
	tests := []struct {
		name   string
		err    error
		status string
	}{
		{"success", nil, "success"},
		{"error", errors.New("hydration failed"), "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reader := newTestRecorder(t)
			rec.HydrationComplete("public", hook.TableName("products"), tt.err)

			m := findMetric(collect(t, reader), "sqlgen.cache.hydrations")
			if m == nil {
				t.Fatalf("sqlgen.cache.hydrations not recorded")
			}
			pt := findSumPoint(t, m, map[string]string{
				"schema": "public",
				"table":  "products",
				"status": tt.status,
			})
			if pt.Value != 1 {
				t.Errorf("hydrations{status=%s} = %d, want 1", tt.status, pt.Value)
			}
		})
	}
}

func TestHydrationStartIsNoop(t *testing.T) {
	// HydrationStart intentionally does not emit against the hydrations
	// counter — the instrument's status label is constrained to
	// success/error (PRD §27.13). This test locks in the no-op so a
	// future change that wires HydrationStart in has to update the
	// documented label set first.
	rec, reader := newTestRecorder(t)
	rec.HydrationStart("public", hook.TableName("products"))

	if m := findMetric(collect(t, reader), "sqlgen.cache.hydrations"); m != nil {
		t.Errorf("HydrationStart unexpectedly recorded on sqlgen.cache.hydrations: %+v", m)
	}
}

func TestCircuitBreakerStateChangeLabels(t *testing.T) {
	// Asserts the gauge carries ONLY the state label — no schema, no
	// table — because the breaker is *Cache-scoped. The value recorded
	// for the new state is 1.
	rec, reader := newTestRecorder(t)
	rec.CircuitBreakerStateChange(cache.StateClosed, cache.StateOpen)

	m := findMetric(collect(t, reader), "sqlgen.cache.circuit_breaker")
	if m == nil {
		t.Fatal("sqlgen.cache.circuit_breaker not recorded")
	}
	pt := findGaugePoint(t, m, map[string]string{"state": "open"})
	if pt.Value != 1 {
		t.Errorf("circuit_breaker{state=open} = %d, want 1", pt.Value)
	}
}

func TestGetLatencyRecordsSeconds(t *testing.T) {
	// Histogram unit is seconds — a 100ms duration records as 0.1.
	rec, reader := newTestRecorder(t)
	rec.GetLatency("public", hook.TableName("products"), 100*time.Millisecond)

	m := findMetric(collect(t, reader), "sqlgen.cache.get.duration")
	if m == nil {
		t.Fatal("sqlgen.cache.get.duration not recorded")
	}
	if m.Unit != "s" {
		t.Errorf("unit = %q, want %q", m.Unit, "s")
	}
	pt := findHistogramPoint(t, m, map[string]string{"schema": "public", "table": "products"})
	if pt.Count != 1 {
		t.Errorf("count = %d, want 1", pt.Count)
	}
	if pt.Sum != 0.1 {
		t.Errorf("sum = %v seconds, want 0.1", pt.Sum)
	}
}

func TestLatencyInstruments(t *testing.T) {
	// Covers the set/invalidate histogram counterparts — confirms both
	// record to the right instrument and use seconds.
	tests := []struct {
		name       string
		instrument string
		call       func(r cache.MetricsRecorder, d time.Duration)
	}{
		{
			name:       "SetLatency",
			instrument: "sqlgen.cache.set.duration",
			call: func(r cache.MetricsRecorder, d time.Duration) {
				r.SetLatency("public", hook.TableName("products"), d)
			},
		},
		{
			name:       "InvalidateLatency",
			instrument: "sqlgen.cache.invalidate.duration",
			call: func(r cache.MetricsRecorder, d time.Duration) {
				r.InvalidateLatency("public", hook.TableName("products"), d)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reader := newTestRecorder(t)
			tt.call(rec, 250*time.Millisecond)

			m := findMetric(collect(t, reader), tt.instrument)
			if m == nil {
				t.Fatalf("%s not recorded", tt.instrument)
			}
			if m.Unit != "s" {
				t.Errorf("unit = %q, want %q", m.Unit, "s")
			}
			pt := findHistogramPoint(t, m, map[string]string{"schema": "public", "table": "products"})
			if pt.Sum != 0.25 {
				t.Errorf("sum = %v, want 0.25", pt.Sum)
			}
		})
	}
}

func TestNewNilProviderUsesGlobal(t *testing.T) {
	// Installing a test provider via otel.SetMeterProvider and passing
	// nil to New must route recorded metrics to the same reader.
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	rec := sqlgenotel.New(nil)
	rec.Hit("public", hook.TableName("products"))

	if m := findMetric(collect(t, reader), "sqlgen.cache.hits"); m == nil {
		t.Fatal("New(nil) did not route to the global MeterProvider")
	}
}

func TestNewExplicitProvider(t *testing.T) {
	// The explicit-provider branch is the happy path used in godoc.
	// Provider passed in → metrics flow to that provider's reader;
	// installing a different provider as the global does not leak.
	explicitReader := sdkmetric.NewManualReader()
	explicit := sdkmetric.NewMeterProvider(sdkmetric.WithReader(explicitReader))

	globalReader := sdkmetric.NewManualReader()
	global := sdkmetric.NewMeterProvider(sdkmetric.WithReader(globalReader))

	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(global)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	rec := sqlgenotel.New(explicit)
	rec.Hit("public", hook.TableName("products"))

	if m := findMetric(collect(t, explicitReader), "sqlgen.cache.hits"); m == nil {
		t.Error("explicit provider did not receive the recorded metric")
	}
	if m := findMetric(collect(t, globalReader), "sqlgen.cache.hits"); m != nil {
		t.Error("global provider unexpectedly received the metric — explicit provider leaked")
	}
}
