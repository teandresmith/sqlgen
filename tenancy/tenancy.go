// Package tenancy provides the runtime integration point for sqlgen's tenant
// isolation. It defines the single injection primitive — TenantResolver[T] —
// and the sentinel errors that generated code and user code exchange.
//
// Tenancy is a codegen concern. All filter injection, auto-column assignment,
// mismatch checks, cache-key scoping, and event-metadata population happens in
// generated code. At runtime the library needs exactly one piece of user
// behavior: a function that extracts the active tenant from the request
// context. That function is TenantResolver.
package tenancy

import (
	"context"
	"errors"
)

// TenantResolver extracts the active tenant from a request context.
//
// T is pinned at codegen time to the resolved Go type of the tenant column
// (PRD §29.2.4) so the resolver signature is compile-time-checked against the
// schema. The generated client holds a concrete TenantResolver[T] field; the
// type parameter is not threaded through hooks, CallOptions, or entity
// clients.
//
// T is constrained to comparable because the mismatch check performed inside
// every tenanted mutation (PRD §29.4.2) compares a caller-supplied tenant
// value against the resolver's value with ==. Tenant identifiers are identity
// values — map keys, cache-key segments, stable equality — so comparable is
// the natural constraint.
//
// Returning the zero value for T when tenancy.required: true causes every
// tenanted operation to error with ErrMissing before a database round-trip.
// Resolvers that want a clearer diagnostic can return ErrMissing explicitly.
type TenantResolver[T comparable] func(ctx context.Context) (T, error)

// ErrMissing is returned when tenancy.required is true and the tenant cannot
// be resolved from the request context. Generated code wraps this sentinel so
// callers can branch with errors.Is(err, tenancy.ErrMissing).
var ErrMissing = errors.New("tenancy: tenant missing from context")

// ErrMismatch is returned when a mutation input carries a tenant value that
// does not match the resolved tenant (PRD §29.4.2). The check runs before the
// database round-trip, so no partial write occurs. To perform a deliberate
// cross-tenant write, set CallOptions.SkipTenancy to true; the explicit
// opt-in is grep-able and lintable.
var ErrMismatch = errors.New("tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override")

// resolvedKey is the private ctx key that carries a resolver short-circuit
// across a chained operation. Parameterised by T so applications wiring
// multiple tenanted clients with different tenant types do not collide on a
// single ctx slot.
type resolvedKey[T comparable] struct{}

// cachedTenant is the value stashed under resolvedKey[T]: the tenant the user
// resolver returned for this operation, and nothing else.
//
// It deliberately does NOT carry the generated resolveTenant helper's `apply`
// bool. That bool is the *table's* reading of the resolver's answer — it folds
// in that table's tenancy.required setting (PRD §29.2.2 makes required a
// per-table tri-state) — while this slot is keyed on T alone and is therefore
// shared by every entity client in the package. Caching the reading rather than
// the answer let whichever table resolved first impose its `required` on every
// table downstream: a required:false table that resolved to the zero value
// handed `apply=false` to a required:true table, which then skipped the
// tenancy.ErrMissing it owed and fell open. Storing the raw answer
// keeps the shared slot table-independent, so each resolveTenant re-applies its
// own required branch to it.
type cachedTenant[T comparable] struct {
	value T
}

// WithResolvedTenant caches a resolver result on ctx for reuse within a single
// logical operation — a chain (Create → terminal Get → terminal GetMany) or a
// nested mutation, whose inner writes route through other entity clients. PRD
// §29.6 ("once per mutation hook entry") treats either as one logical hook
// entry; the cache is what makes that literal.
//
// Only a value the *resolver* produced belongs here. An explicit tenant
// (CallOptions.Tenant, PRD §29.4.4) does not: it travels on CallOptions, which
// every internal call already inherits, and resolveTenant honours it ahead of
// this cache — so stashing it would put a value with different semantics (a set
// zero is honoured verbatim, where a resolved zero takes the required branch)
// into a slot that cannot express the difference.
//
// This helper is generator-emitted plumbing — consumers should not call it
// directly. The ctx returned is derived from the input; the original ctx is
// untouched, so two sequential operations on the same caller-supplied ctx
// resolve independently.
func WithResolvedTenant[T comparable](ctx context.Context, value T) context.Context {
	return context.WithValue(ctx, resolvedKey[T]{}, cachedTenant[T]{value: value})
}

// CachedTenant returns a tenant value previously stashed by WithResolvedTenant.
// When ok is false no cache entry is present and the caller falls through to
// the user resolver.
//
// A cached zero value is a real answer — "the resolver ran and returned the zero
// value" — not an absent one, which is why presence is reported separately
// rather than inferred from the value. The caller applies its own
// tenancy.required branch to it; see [cachedTenant]. Generator-emitted plumbing
// — consumers should not call it directly.
func CachedTenant[T comparable](ctx context.Context) (value T, ok bool) {
	r, found := ctx.Value(resolvedKey[T]{}).(cachedTenant[T])
	if !found {
		return value, false
	}
	return r.value, true
}
