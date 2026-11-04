package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/hook"
)

// pin takes its type argument explicitly, so the call fails to compile when
// the value's type differs from the pinned one.
func pin[T any](T) {}

// Each method-set test pins an interface two ways: the method expressions fail
// to compile if a method is removed or its signature changes, and the
// assignment of a type that has exactly the listed methods fails if the
// interface gains one.

type exactBackend struct{}

func (exactBackend) Get(context.Context, string) ([]byte, error)              { return nil, nil }
func (exactBackend) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (exactBackend) Invalidate(context.Context, string) error                 { return nil }
func (exactBackend) InvalidateMany(context.Context, []string) error           { return nil }
func (exactBackend) InvalidatePattern(context.Context, string) error          { return nil }

func TestBackendMethodSet(t *testing.T) {
	pin[func(cache.Backend, context.Context, string) ([]byte, error)](cache.Backend.Get)
	pin[func(cache.Backend, context.Context, string, []byte, time.Duration) error](cache.Backend.Set)
	pin[func(cache.Backend, context.Context, string) error](cache.Backend.Invalidate)
	pin[func(cache.Backend, context.Context, []string) error](cache.Backend.InvalidateMany)
	pin[func(cache.Backend, context.Context, string) error](cache.Backend.InvalidatePattern)
	var _ cache.Backend = exactBackend{}
}

type exactStatsReporter struct{}

func (exactStatsReporter) Stats() cache.Stats { return cache.Stats{} }

func TestStatsReporterAndStatsFields(t *testing.T) {
	pin[func(cache.StatsReporter) cache.Stats](cache.StatsReporter.Stats)
	var _ cache.StatsReporter = exactStatsReporter{}

	// The positional literal fails to compile if a field is added, removed or
	// changes type; the keyed reads fail if one is renamed.
	s := cache.Stats{1, 2, 3, 4, 5, 6}
	got := [6]uint64{s.Hits, s.Misses, s.Sets, s.Invalidations, s.Evictions, s.Entries}
	if got != [6]uint64{1, 2, 3, 4, 5, 6} {
		t.Errorf("Stats fields = %v, want Hits, Misses, Sets, Invalidations, Evictions, Entries in order", got)
	}
}

type exactSerializer struct{}

func (exactSerializer) Marshal(any) ([]byte, error) { return nil, nil }
func (exactSerializer) Unmarshal([]byte, any) error { return nil }

func TestSerializerMethodSet(t *testing.T) {
	pin[func(cache.Serializer, any) ([]byte, error)](cache.Serializer.Marshal)
	pin[func(cache.Serializer, []byte, any) error](cache.Serializer.Unmarshal)
	var _ cache.Serializer = exactSerializer{}
}

type exactRecorder struct{}

func (exactRecorder) Hit(string, hook.TableName)                                       {}
func (exactRecorder) Miss(string, hook.TableName)                                      {}
func (exactRecorder) Set(string, hook.TableName)                                       {}
func (exactRecorder) Invalidate(string, hook.TableName)                                {}
func (exactRecorder) Error(string, hook.TableName, string, error)                      {}
func (exactRecorder) HydrationStart(string, hook.TableName)                            {}
func (exactRecorder) HydrationComplete(string, hook.TableName, error)                  {}
func (exactRecorder) CircuitBreakerStateChange(cache.CircuitState, cache.CircuitState) {}
func (exactRecorder) GetLatency(string, hook.TableName, time.Duration)                 {}
func (exactRecorder) SetLatency(string, hook.TableName, time.Duration)                 {}
func (exactRecorder) InvalidateLatency(string, hook.TableName, time.Duration)          {}

func TestMetricsRecorderMethodSet(t *testing.T) {
	type rec = cache.MetricsRecorder
	pin[func(rec, string, hook.TableName)](rec.Hit)
	pin[func(rec, string, hook.TableName)](rec.Miss)
	pin[func(rec, string, hook.TableName)](rec.Set)
	pin[func(rec, string, hook.TableName)](rec.Invalidate)
	pin[func(rec, string, hook.TableName, string, error)](rec.Error)
	pin[func(rec, string, hook.TableName)](rec.HydrationStart)
	pin[func(rec, string, hook.TableName, error)](rec.HydrationComplete)
	pin[func(rec, cache.CircuitState, cache.CircuitState)](rec.CircuitBreakerStateChange)
	pin[func(rec, string, hook.TableName, time.Duration)](rec.GetLatency)
	pin[func(rec, string, hook.TableName, time.Duration)](rec.SetLatency)
	pin[func(rec, string, hook.TableName, time.Duration)](rec.InvalidateLatency)
	var _ cache.MetricsRecorder = exactRecorder{}
}

type exactSource struct{}

func (exactSource) Subscribe(cache.InvalidationHandler) (cache.InvalidationSubscription, error) {
	return nil, nil
}
func (exactSource) Close() error { return nil }

type exactSubscription struct{}

func (exactSubscription) Unsubscribe() error { return nil }

func TestInvalidationTypes(t *testing.T) {
	pin[func(cache.InvalidationSource, cache.InvalidationHandler) (cache.InvalidationSubscription, error)](cache.InvalidationSource.Subscribe)
	pin[func(cache.InvalidationSource) error](cache.InvalidationSource.Close)
	pin[func(cache.InvalidationSubscription) error](cache.InvalidationSubscription.Unsubscribe)
	var _ cache.InvalidationSource = exactSource{}
	var _ cache.InvalidationSubscription = exactSubscription{}

	var h cache.InvalidationHandler = func(context.Context, cache.InvalidationSignal) error { return nil }
	_ = h

	// Positional literal: field count, order and types. The keyed reads pin
	// the names.
	sig := cache.InvalidationSignal{hook.TableName("public.products"), "public", "acme", []any{7}}
	type fields struct {
		Table  hook.TableName
		Schema string
		Tenant string
		PKs    []any
	}
	got := fields{sig.Table, sig.Schema, sig.Tenant, sig.PKs}
	if got.Table != "public.products" || got.Schema != "public" || got.Tenant != "acme" || len(got.PKs) != 1 || got.PKs[0] != 7 {
		t.Errorf("InvalidationSignal fields = %+v", got)
	}
}

func TestEventAdapterAndErrorPolicySignatures(t *testing.T) {
	pin[func(event.Subscriber, ...cache.EventAdapterOption) cache.InvalidationSource](cache.FromEventSubscriber)
	pin[func(cache.MetricsRecorder) cache.EventAdapterOption](cache.WithEventMetrics)
	pin[cache.OnErrorFunc](cache.DefaultOnCacheError)
	pin[func(context.Context, string, string, hook.TableName, error)](cache.OnErrorFunc(nil))
	pin[func(*cache.Breaker)]((*cache.Breaker).RecordFailure)
}

func TestPatternBuilderSignatures(t *testing.T) {
	pin[func(prefix, schema, table string) string](cache.BuildTablePattern)
	pin[func(prefix, schema, table string, tenant any) string](cache.BuildTenantTablePattern)
}
