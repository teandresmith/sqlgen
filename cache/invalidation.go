package cache

import (
	"context"

	"github.com/teandresmith/sqlgen/hook"
)

// InvalidationSource feeds cache invalidation signals from any event system.
// The generated *Cache facade subscribes at construction time and unsubscribes
// via Close during shutdown.
//
// Implementations exist for three layers:
//
//   - Layer 1: cache.FromEventSubscriber (built-in adapter over event.Subscriber)
//   - Layer 2: user-provided bridges from external systems (Kafka CDC, Redis
//     Streams, PostgreSQL LISTEN/NOTIFY, webhooks, ...)
//   - Layer 3: none — direct *Cache.Invalidate* calls bypass this interface
//
// Subscribe registers the single handler that the generated cache facade uses
// to dispatch invalidations; an InvalidationSource supports one active
// subscription at a time. The returned InvalidationSubscription's Unsubscribe
// is idempotent.
type InvalidationSource interface {
	Subscribe(handler InvalidationHandler) (InvalidationSubscription, error)
	Close() error
}

// InvalidationSignal carries one invalidation instruction from an
// InvalidationSource to the generated cache facade. It is a struct rather
// than positional handler parameters so future fields (operation,
// fingerprint, ...) extend the extension point without breaking every
// InvalidationSource implementation again (PRD §28.11).
type InvalidationSignal struct {
	// Table is the strongly-typed table name — generated packages expose
	// TableXxx constants of type hook.TableName, and the facade matches on
	// their value alone. That value is "schema.table" for a table with a
	// schema (every PostgreSQL table, e.g. "public.orders") and the bare
	// name for MySQL/SQLite (PRD §8.5), so a Layer 2 implementation that
	// speaks in raw strings sends the same form and casts at the boundary:
	//
	//	handler(ctx, cache.InvalidationSignal{Table: hook.TableName(schema + "." + table), PKs: pks})
	//
	// A bare name for a PostgreSQL table matches no cached table: the
	// facade returns its "unknown or non-cached table" error to the source.
	Table hook.TableName
	// Schema is the SQL schema of the table ("public" for PostgreSQL, ""
	// for MySQL/SQLite). Informational — the facade identifies the table
	// by Table, which already carries the schema, and resolves its own
	// schema per table constant; sources may leave it empty.
	Schema string
	// Tenant is the stringified tenant of the affected rows for tenanted
	// tables (PRD §29.5/§29.6) — the %v form of the row's tenant value,
	// byte-identical to the tenant segment of the cache-key grammar, so the
	// receiving facade rebuilds exact keys without knowing the concrete
	// tenant type. Empty for non-tenanted tables or when the source has no
	// tenant (a tenanted table then degrades to a full-table clear).
	Tenant string
	// PKs holds the primary keys to invalidate, one element per affected
	// entity. Element shape matches cache.Invalidate* on the generated
	// facade: a scalar value for single-PK tables and the generated XXXPK
	// struct value for composite-PK tables. An empty slice signals
	// full-table invalidation — the facade dispatches through
	// Backend.InvalidatePattern using the fingerprint-agnostic pattern
	// produced by BuildTablePattern, clearing every fingerprint generation
	// and every PK for that table in a single pass.
	//
	// PK values MUST be value types, not pointers — pointer PKs
	// %v-stringify to memory addresses and break every cache key.
	// (CACHE.md §10.)
	PKs []any
}

// InvalidationHandler receives invalidation signals from an
// InvalidationSource.
//
// The handler is synchronous: the source waits for it to return before
// proceeding. A returned error propagates to the caller of Subscribe's
// transport so ACK-capable systems can redeliver.
type InvalidationHandler func(ctx context.Context, signal InvalidationSignal) error

// InvalidationSubscription represents an active subscription returned from
// InvalidationSource.Subscribe. Unsubscribe detaches the handler from the
// source and is safe to call more than once.
type InvalidationSubscription interface {
	Unsubscribe() error
}
