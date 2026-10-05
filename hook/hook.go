// Package hook provides the middleware types and operation enums for the
// sqlgen hook system. Hooks wrap database operations with before/after logic
// for cross-cutting concerns like tracing, authorization, and validation.
package hook

import "context"

// TableName is a string type for type-safe table and view name references.
// Constants of this type are generated per table and view by sqlgen.
type TableName string

// MutationOp identifies the type of mutation operation being performed.
type MutationOp string

// Mutation operation constants.
const (
	OpCreate          MutationOp = "create"
	OpCreateMany      MutationOp = "create_many"
	OpUpdate          MutationOp = "update"
	OpUpdateMany      MutationOp = "update_many"
	OpUpdateWhere     MutationOp = "update_where"
	OpUpsert          MutationOp = "upsert"
	OpUpsertMany      MutationOp = "upsert_many"
	OpSoftDelete      MutationOp = "soft_delete"
	OpSoftDeleteMany  MutationOp = "soft_delete_many"
	OpSoftDeleteWhere MutationOp = "soft_delete_where"
	OpRestore         MutationOp = "restore"
	OpRestoreMany     MutationOp = "restore_many"
	OpRestoreWhere    MutationOp = "restore_where"
	OpHardDelete      MutationOp = "hard_delete"
	OpHardDeleteMany  MutationOp = "hard_delete_many"
	OpHardDeleteWhere MutationOp = "hard_delete_where"
	OpIncrement       MutationOp = "increment"
)

// QueryOp identifies the type of query operation being performed.
type QueryOp string

// Query operation constants.
const (
	OpGet        QueryOp = "get"
	OpGetMany    QueryOp = "get_many"
	OpExists     QueryOp = "exists"
	OpCount      QueryOp = "count"
	OpPaginate   QueryOp = "paginate"
	OpConnection QueryOp = "connection"
	OpStream     QueryOp = "stream"
	// OpRefresh identifies a materialized-view REFRESH. Refreshes flow through
	// the query-hook chain like read operations — they take no input and
	// return no rows.
	OpRefresh QueryOp = "refresh"
)

// MutationHook wraps a MutationHandler to produce a new MutationHandler.
type MutationHook func(next MutationHandler) MutationHandler

// MutationHandler executes a mutation and returns the result.
type MutationHandler func(ctx context.Context, m *MutationContext) (any, error)

// MutationContext provides metadata and access to the mutation being performed.
type MutationContext struct {
	Op     MutationOp // Create, Update, Delete, Upsert, etc.
	Table  TableName  // table name constant (e.g., TableProducts)
	Schema string     // schema name (e.g., "public", "" for MySQL/SQLite)
	PK     any        // primary key value (for Update/Delete/Increment; nil for Create)
	// Input carries the operation payload — the caller-supplied one, except
	// where a terminal mutation substitutes a narrowed view of it (UpsertMany,
	// below). Shape is op-keyed:
	//   - Single-op (Create / Update / Upsert / Increment): the input pointer
	//     (e.g. *CreateProductInput, *UpdateProductInput).
	//   - UpsertMany: the []*CreateInput slice AFTER the PRD §9.2 dedupe, not
	//     the caller's. Inputs resolving to one conflict target collapse to
	//     their last occurrence before any statement runs, so the caller's
	//     slice describes rows the write never carried; the deduped one is
	//     what stays index-aligned with AffectedPKs, which is what a per-entity
	//     event fanout indexes. A mutation hook running BEFORE the terminal
	//     still sees the caller's slice — the substitution happens inside the
	//     terminal, so only after-hooks and the event hook observe it.
	//   - CreateMany: the full []*CreateInput slice the caller passed —
	//     mc.Input is the BATCH view at this surface, so user-written
	//     mutation hooks running before the event hook see the whole slice.
	//     The event hook (PRD §28.6) fans out to per-entity Input on each
	//     emitted Event.Input — the BATCH view does not propagate to
	//     subscribers.
	//   - UpdateMany: the []UpdateItem slice (same batch-vs-event split).
	//   - HardDeleteMany / SoftDeleteMany / RestoreMany: the []PK slice the
	//     caller passed; Event.Input fans out to nil since the per-entity
	//     PK already lives on Event.PK.
	//   - *Where ops: the *Filter (delete/restore) or *UpdateInput (update),
	//     shared across every fanned-out event.
	// Two distinct contracts at two distinct surfaces — mutation hooks see
	// the batch view; event-hook subscribers see the per-entity view.
	Input       any
	CallOptions any // resolved CallOptions[FO]; hooks can type-assert for table-specific access
	// AffectedPKs is populated by the terminal mutation with one element per
	// affected entity. Consumed by event hooks and cache invalidation.
	//
	// Element shape:
	//   - Single-PK tables: the scalar PK value (uuid.UUID, int64, string, ...).
	//   - Composite-PK tables: the generated XXXPK struct value
	//     (e.g. OrderItemPK{OrderID: ..., ProductID: ...}), not a flat list of
	//     scalars. Field order in the struct matches DDL PRIMARY KEY column
	//     order.
	//
	// The element shape matches the Cache.Invalidate / InvalidateMany contract,
	// so the generated cache hook can pass AffectedPKs directly into
	// InvalidateMany without a pack step.
	AffectedPKs []any
	// AffectedTenants carries the tenant column value of each affected row,
	// index-aligned with AffectedPKs: AffectedTenants[i] is the tenant of the
	// row identified by AffectedPKs[i]. Both slices are filled in lockstep by
	// the same generated capture step (PRD §29.5),
	// so consumers may rely on the alignment invariant.
	//
	// Populated only for tenanted tables whose tenant column is NOT part of
	// the primary key; nil otherwise (non-tenanted tables, tenancy disabled,
	// and tenant-in-PK tables, where the tenant is read from the PK struct in
	// AffectedPKs instead). The tenant is captured structurally from the
	// mutated row itself — widened RETURNING on PostgreSQL/SQLite, a
	// pre-SELECT on MySQL, and for UpsertMany a post-write SELECT on every
	// dialect (a conflict leaves the existing row's tenant in place, and the
	// DO NOTHING branch returns no row for RETURNING to widen — PRD §9.5) —
	// never from the resolver and never from a
	// possibly-partial returned entity, so it is correct even when Tenant is
	// nil (CallOptions.SkipTenancy, or tenancy.required:false with a zero
	// resolver).
	//
	// An individual element is nil when the mutation did not materialize a
	// row for that PK (e.g. an idempotent delete of a nonexistent key, or a
	// no-op batch update item) — nothing changed for that row, so consumers
	// skip it.
	AffectedTenants []any
	// Tenant carries the tenant value resolved at the entity-method boundary
	// for tenanted tables (PRD §29.5). It's set once before the terminal
	// runs and re-read by the cache invalidation hook so the tenant is
	// resolved exactly once per operation. Nil on shared/non-tenanted
	// tables and when CallOptions.SkipTenancy is true.
	Tenant any
}

// QueryHook wraps a QueryHandler to produce a new QueryHandler.
type QueryHook func(next QueryHandler) QueryHandler

// QueryHandler executes a query and returns the result.
type QueryHandler func(ctx context.Context, q *QueryContext) (any, error)

// QueryContext provides metadata and access to the query being performed.
type QueryContext struct {
	Op          QueryOp   // Get, GetMany, Exists, Count, Paginate, Connection
	Table       TableName // table name constant (e.g., TableProducts)
	Schema      string    // schema name (e.g., "public", "" for MySQL/SQLite)
	PK          any       // primary key value (for Get/Exists; nil for GetMany/Count)
	Input       any       // *GetInput, *Filter, etc.
	CallOptions any       // resolved CallOptions[FO]; hooks can type-assert for table-specific access
	// Tenant carries the tenant value resolved at the entity-method boundary
	// for tenanted tables (PRD §29.5). It's populated before executeQuery
	// runs so the cache read-through hook can build a tenant-scoped key
	// without re-invoking the resolver, and the terminal handler reads
	// from it instead of resolving again. Nil on shared/non-tenanted tables
	// and when CallOptions.SkipTenancy is true.
	Tenant any
}
