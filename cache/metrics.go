package cache

import (
	"time"

	"github.com/teandresmith/sqlgen/hook"
)

// MetricsRecorder receives cache operation signals. Consumers implement this
// to bridge to their metrics system (Prometheus, OTel, Datadog, StatsD,
// etc.). The generated cache facade treats a nil MetricsRecorder as a
// zero-cost path — every call site is guarded by a single nil check.
//
// schema is the resolved schema string — typically "public" (or a
// user-configured schema) for PostgreSQL, and "" for MySQL / SQLite. It
// matches the schema segment baked into the cache key so same-name tables
// in different schemas do not collapse into a single metric series.
//
// table is the generated TableXxx constant's value, which carries the schema
// whenever the table has one ("public.products" on PostgreSQL, "products" on
// MySQL / SQLite — PRD §5.5). A recorder that labels schema and table
// separately strips the schema + "." prefix from table, as metrics/otel does,
// so the table label is the bare SQL name on every dialect.
//
// CircuitBreakerStateChange intentionally does NOT carry schema or table
// attributes: the breaker is scoped to a single *Cache facade, not to an
// individual table.
type MetricsRecorder interface {
	Hit(schema string, table hook.TableName)
	Miss(schema string, table hook.TableName)
	Set(schema string, table hook.TableName)
	Invalidate(schema string, table hook.TableName)
	Error(schema string, table hook.TableName, op string, err error)
	HydrationStart(schema string, table hook.TableName)
	HydrationComplete(schema string, table hook.TableName, err error)
	CircuitBreakerStateChange(from, to CircuitState)

	GetLatency(schema string, table hook.TableName, d time.Duration)
	SetLatency(schema string, table hook.TableName, d time.Duration)
	InvalidateLatency(schema string, table hook.TableName, d time.Duration)
}
