package cache_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/hook"
)

const testTable hook.TableName = "products"

func TestDefaultOnCacheError_WithSchema(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})

	cache.DefaultOnCacheError(context.Background(), "get", "public", testTable, errors.New("boom"))

	got := strings.TrimRight(buf.String(), "\n")
	want := "cache get public.products: boom"
	if got != want {
		t.Errorf("DefaultOnCacheError() log = %q, want %q", got, want)
	}
}

// TestDefaultOnCacheError_QualifiedTable pins the input the generated facade
// passes on PostgreSQL: the table's hook.TableName value, which already
// carries the schema (PRD §5.5). The schema must not be printed twice.
func TestDefaultOnCacheError_QualifiedTable(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		table  hook.TableName
		want   string
	}{
		{name: "qualified value", schema: "public", table: "public.products", want: "cache get public.products: boom"},
		{name: "second schema", schema: "audit", table: "audit.products", want: "cache get audit.products: boom"},
		{name: "schema named like the table", schema: "products", table: "products.products", want: "cache get products.products: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := log.Writer()
			prevFlags := log.Flags()
			log.SetOutput(&buf)
			log.SetFlags(0)
			t.Cleanup(func() {
				log.SetOutput(prev)
				log.SetFlags(prevFlags)
			})

			cache.DefaultOnCacheError(context.Background(), "get", tt.schema, tt.table, errors.New("boom"))

			if got := strings.TrimRight(buf.String(), "\n"); got != tt.want {
				t.Errorf("DefaultOnCacheError() log = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultOnCacheError_EmptySchema(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})

	cache.DefaultOnCacheError(context.Background(), "invalidate_many", "", testTable, errors.New("boom"))

	got := strings.TrimRight(buf.String(), "\n")
	want := "cache invalidate_many products: boom"
	if got != want {
		t.Errorf("DefaultOnCacheError() log = %q, want %q", got, want)
	}
}

type recordingMetrics struct {
	mu      sync.Mutex
	order   *[]string
	records []struct {
		schema string
		table  hook.TableName
		op     string
		err    error
	}
}

func (m *recordingMetrics) note(event string) {
	m.mu.Lock()
	*m.order = append(*m.order, event)
	m.mu.Unlock()
}

func (m *recordingMetrics) Hit(schema string, table hook.TableName)        {}
func (m *recordingMetrics) Miss(schema string, table hook.TableName)       {}
func (m *recordingMetrics) Set(schema string, table hook.TableName)        {}
func (m *recordingMetrics) Invalidate(schema string, table hook.TableName) {}
func (m *recordingMetrics) Error(schema string, table hook.TableName, op string, err error) {
	m.mu.Lock()
	m.records = append(m.records, struct {
		schema string
		table  hook.TableName
		op     string
		err    error
	}{schema, table, op, err})
	m.mu.Unlock()
	m.note("metrics")
}
func (m *recordingMetrics) HydrationStart(schema string, table hook.TableName)               {}
func (m *recordingMetrics) HydrationComplete(schema string, table hook.TableName, err error) {}
func (m *recordingMetrics) CircuitBreakerStateChange(from, to cache.CircuitState)            {}
func (m *recordingMetrics) GetLatency(schema string, table hook.TableName, d time.Duration) {
}

func (m *recordingMetrics) SetLatency(schema string, table hook.TableName, d time.Duration) {
}

func (m *recordingMetrics) InvalidateLatency(schema string, table hook.TableName, d time.Duration) {
}

func TestRouteError_ThreeChannelOrdering(t *testing.T) {
	var order []string
	m := &recordingMetrics{order: &order}
	onErr := func(ctx context.Context, op, schema string, table hook.TableName, err error) {
		order = append(order, "on_err")
	}

	var breakerFired bool
	br := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     time.Hour,
		HalfOpenMaxProbes: 1,
		OnStateChange: func(from, to cache.CircuitState) {
			if to == cache.StateOpen {
				breakerFired = true
			}
		},
	})

	boom := errors.New("backend down")
	cache.RouteError(context.Background(), m, onErr, br, "get", "public", testTable, boom)

	want := []string{"metrics", "on_err"}
	if len(order) < len(want) {
		t.Fatalf("RouteError order = %v, want prefix %v", order, want)
	}
	for i, step := range want {
		if order[i] != step {
			t.Errorf("RouteError order[%d] = %q, want %q", i, order[i], step)
		}
	}
	if !breakerFired {
		t.Errorf("RouteError() did not call breaker.RecordFailure — breaker remained Closed")
	}
	if len(m.records) != 1 || m.records[0].op != "get" || m.records[0].schema != "public" ||
		m.records[0].table != testTable || !errors.Is(m.records[0].err, boom) {
		t.Errorf("metrics record = %+v, want {public, products, get, boom}", m.records)
	}
}

func TestRouteError_PanickingOnErrorDoesNotSkipBreaker(t *testing.T) {
	var order []string
	m := &recordingMetrics{order: &order}
	onErr := func(ctx context.Context, op, schema string, table hook.TableName, err error) {
		panic("logger is broken")
	}

	var brFired bool
	br := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     time.Hour,
		HalfOpenMaxProbes: 1,
		OnStateChange: func(from, to cache.CircuitState) {
			if to == cache.StateOpen {
				brFired = true
			}
		},
	})

	// Must not panic.
	cache.RouteError(context.Background(), m, onErr, br, "set", "public", testTable, errors.New("boom"))

	if !brFired {
		t.Errorf("RouteError() with panicking OnErrorFunc did not fire breaker")
	}
}

func TestRouteError_NilMetricsIsZeroCost(t *testing.T) {
	var called bool
	onErr := func(ctx context.Context, op, schema string, table hook.TableName, err error) {
		called = true
	}
	br := cache.NewBreaker(cache.BreakerConfig{
		FailureThreshold:  1,
		ProbeInterval:     time.Hour,
		HalfOpenMaxProbes: 1,
	})

	// Must not panic and must still call onErr and breaker.
	cache.RouteError(context.Background(), nil, onErr, br, "get", "", testTable, errors.New("boom"))

	if !called {
		t.Errorf("RouteError(nil metrics) did not call onErr")
	}
	if br.State() != cache.StateOpen {
		t.Errorf("RouteError(nil metrics) did not trip breaker: state = %v", br.State())
	}
}
