package cache

import (
	"fmt"
	"strings"
)

// BuildKey returns a cache key using the labeled-segment grammar:
//
//	{prefix}:{schema}.{table}:fingerprint:v{fingerprint}:pk:{pk}
//
// Every segment carries a label (fingerprint:, pk:) so keys are
// self-describing in a cache dump — no positional decoding required.
//
// schema is included when non-empty (typically "public" for PostgreSQL);
// when schema == "" (MySQL / SQLite) the {schema}. prefix is omitted.
// fingerprint is a short per-table schema version baked in at codegen
// time; passing "" drops the fingerprint:v{fp}: segment entirely so the
// remaining key is still well-formed.
//
// The pk: label is ALWAYS the terminal labeled segment — nothing follows
// it. PKs are formatted with %v, which is correct for scalars like
// uuid.UUID (stringer), int64, and string. Pointer types are not
// supported: their %v rendering is the address, not the value.
func BuildKey(prefix, schema, table, fingerprint string, pk any) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	writeFingerprint(&b, fingerprint)
	b.WriteString(":pk:")
	fmt.Fprintf(&b, "%v", pk)
	return b.String()
}

// BuildCompositeKey joins composite PK values with ":" under a single
// terminal pk: label. The components MUST be provided in DDL
// PRIMARY KEY (...) column order — the helper does not inspect column
// names. Passing values out of order produces a different cache key for
// the same logical entity and causes cache misses and orphaned writes.
// The generator is the only code path that calls BuildCompositeKey
// directly and always passes pks in canonical order; user code should
// use the generated per-table keyFor{Table} helpers.
func BuildCompositeKey(prefix, schema, table, fingerprint string, pks []any) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	writeFingerprint(&b, fingerprint)
	b.WriteString(":pk:")
	for i, pk := range pks {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%v", pk)
	}
	return b.String()
}

// BuildTablePattern returns the prefix + trailing-"*" pattern for a table.
// It deliberately omits BOTH the fingerprint: and pk: segments so pattern
// invalidations clear every fingerprint generation AND every PK in a
// single pass. InvalidateTable therefore cleans up current-generation and
// orphaned-prior-generation entries together.
//
// This is not the mutation path. Every mutation op — the *Where family
// included — evicts by key from the affected rows' PKs (PRD 27.7), so
// sweeping orphans is a deliberate InvalidateTable call rather than a side
// effect of write traffic.
//
// For tenanted tables the pattern still matches across every tenant, which
// is the reach InvalidateTable and the section 29.5 capture-gap fallback
// need. Use BuildTenantTablePattern when you want per-tenant scope.
func BuildTablePattern(prefix, schema, table string) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	b.WriteString(":*")
	return b.String()
}

// BuildTenantKey is the tenant-aware variant of BuildKey for tenanted
// tables (PRD §29.5). The tenant: segment lands BEFORE fingerprint:, so
// "invalidate everything for tenant X on table Y" is a single-wildcard
// pattern (BuildTenantTablePattern). The terminal pk: invariant from
// section 27.5 is preserved — pk: is still the last labeled segment.
//
// Grammar:
//
//	{prefix}:{schema}.{table}:tenant:{tenant}:fingerprint:v{fingerprint}:pk:{pk}
//
// Tenant is formatted with %v — generated callers pass a concrete
// `comparable` Go type (PRD §29.3.1) so pointer-stringify-as-address
// hazards do not apply.
func BuildTenantKey(prefix, schema, table, fingerprint string, tenant, pk any) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	writeTenant(&b, tenant)
	writeFingerprint(&b, fingerprint)
	b.WriteString(":pk:")
	fmt.Fprintf(&b, "%v", pk)
	return b.String()
}

// BuildCompositeTenantKey is the tenant-aware variant of
// BuildCompositeKey. Components MUST be in DDL PRIMARY KEY column order;
// callers always go through the generated keyFor{Table} helpers.
func BuildCompositeTenantKey(prefix, schema, table, fingerprint string, tenant any, pks []any) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	writeTenant(&b, tenant)
	writeFingerprint(&b, fingerprint)
	b.WriteString(":pk:")
	for i, pk := range pks {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%v", pk)
	}
	return b.String()
}

// BuildTenantTablePattern returns a single-wildcard pattern that matches
// every cache entry for one tenant on one table — across every
// fingerprint generation and every PK (PRD §29.5). Useful for
// tenant-offboarding workflows that need to evict only the leaving
// tenant's entries.
//
// Grammar:
//
//	{prefix}:{schema}.{table}:tenant:{tenant}:*
//
// One trailing "*" — works on Redis KEYS / SCAN MATCH, Memcached pattern
// engines, and prefix-matching in-memory backends without needing an
// intermediate wildcard.
func BuildTenantTablePattern(prefix, schema, table string, tenant any) string {
	var b strings.Builder
	writeHead(&b, prefix, schema, table)
	writeTenant(&b, tenant)
	b.WriteString(":*")
	return b.String()
}

// writeHead writes "{prefix}:{schema}.{table}" or "{prefix}:{table}" when
// schema is empty. The trailing ":" and segments are appended by callers.
func writeHead(b *strings.Builder, prefix, schema, table string) {
	b.WriteString(prefix)
	b.WriteByte(':')
	if schema != "" {
		b.WriteString(schema)
		b.WriteByte('.')
	}
	b.WriteString(table)
}

// writeFingerprint writes ":fingerprint:v{fp}" when fingerprint is
// non-empty; otherwise writes nothing. The surrounding caller already
// emitted the "{prefix}:{schema}.{table}" head.
func writeFingerprint(b *strings.Builder, fingerprint string) {
	if fingerprint == "" {
		return
	}
	b.WriteString(":fingerprint:v")
	b.WriteString(fingerprint)
}

// writeTenant writes ":tenant:{tenant}" using %v formatting. Always
// before fingerprint so the per-tenant trailing-wildcard pattern works
// on every backend (PRD §29.5 rationale).
func writeTenant(b *strings.Builder, tenant any) {
	b.WriteString(":tenant:")
	fmt.Fprintf(b, "%v", tenant)
}
