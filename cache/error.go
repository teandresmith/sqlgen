package cache

import (
	"context"
	"log"
	"strings"

	"github.com/teandresmith/sqlgen/hook"
)

// OnErrorFunc is the textual-log callback invoked when a cache backend
// operation fails. The generated facade routes every backend error
// through MetricsRecorder.Error, OnErrorFunc, and breaker.RecordFailure
// in that order; callers swap OnErrorFunc to route through slog, zap,
// or any structured logger.
//
// table is the generated TableXxx constant's value, which already carries
// the schema on PostgreSQL ("public.products", PRD §5.5); schema is passed
// alongside it and is empty for MySQL / SQLite. op ∈ {"get", "set",
// "invalidate", "invalidate_many", "invalidate_pattern",
// "invalidate_tenant_fallback"} — the last fires when
// tenanted invalidation degrades to the cross-tenant table pattern because a
// mutation's captured tenant was unexpectedly missing (PRD §27.9; should
// never occur in normal operation).
type OnErrorFunc func(ctx context.Context, op, schema string, table hook.TableName, err error)

// DefaultOnCacheError logs cache backend failures to stderr via log.Printf.
// Mirrors event.DefaultOnError for textual consistency. The generated facade
// passes the table's hook.TableName value, which already carries the schema
// on PostgreSQL ("public.products", PRD §5.5), so the schema is prefixed only
// to a value that lacks it — it is never printed twice.
func DefaultOnCacheError(ctx context.Context, op, schema string, table hook.TableName, err error) {
	name := string(table)
	if schema != "" && !strings.HasPrefix(name, schema+".") {
		name = schema + "." + name
	}
	log.Printf("cache %s %s: %v", op, name, err)
}

// RouteError dispatches a cache backend error through the three-channel
// policy: structured telemetry (metrics), textual log fallback (onErr),
// and breaker feedback — in that order.
//
// A nil metrics or breaker short-circuits that channel with no allocation.
// onErr is wrapped in a defer / recover so a panicking logger cannot skip
// breaker accounting — a misconfigured logger must not mask a cache
// outage. Callers pass the same (op, schema, table, err) values that
// were observed at the call site.
func RouteError(ctx context.Context, metrics MetricsRecorder, onErr OnErrorFunc, breaker *Breaker, op, schema string, table hook.TableName, err error) {
	if metrics != nil {
		metrics.Error(schema, table, op, err)
	}
	if onErr != nil {
		func() {
			defer func() { _ = recover() }()
			onErr(ctx, op, schema, table, err)
		}()
	}
	if breaker != nil {
		breaker.RecordFailure()
	}
}
