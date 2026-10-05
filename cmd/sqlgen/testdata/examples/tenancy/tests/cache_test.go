package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/segmentio/ksuid"

	"github.com/teandresmith/sqlgen/cache"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestCache_PerTenantIsolation verifies §29.5: tenant A's cached entry for a
// PK does not satisfy tenant B's read of the same PK. Without per-tenant cache
// scoping, a tenant could see another tenant's data after a cache hit — the
// nightmare scenario for tenancy + cache.
//
// We assert via the spy MetricsRecorder: tenant B's first Get is a MISS
// (not a HIT), proving the keys are distinct. The DB returns no row for B
// (tenancy filter), so we expect ErrNotFound — the success case is a clean
// miss + clean DB-level isolation.
func TestCache_PerTenantIsolation(t *testing.T) {
	resetDB(t)

	// We need ONE shared cache backend across two tenants — that's the
	// whole point of the test (verify the keys are distinct on the same
	// backend). The default newEnv gives each test its own backend, so set
	// up two clients sharing the same Cache facade manually.
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())

	// Build envB but reuse envA's cache.
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB = rewireCache(t, envB, envA.cache)

	ctx := context.Background()

	// Two products with the same SKU? No — sku is unique per (workspace, sku).
	// Use different SKUs but force their PKs to collide is impossible
	// (autoincrement). Instead, the realistic test is "tenant A populates
	// the cache for some PK, tenant B Gets the same PK". That collision
	// won't happen via natural creates (different IDs), so we synthesize:
	// admin-write a row for B at A's exact ID using SkipTenancy + an
	// explicit row insert.
	aProduct, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "A product", SKU: "A-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}

	// Tenant A's Get warms the cache.
	if _, err := envA.client.Products().Get(ctx, aProduct.ID); err != nil {
		t.Fatalf("Get A (warm cache): %v", err)
	}

	// Tenant B Gets the same PK. Two outcomes proving the key is distinct:
	//   1. The cache miss count goes up by 1 (we did a fresh lookup, not a
	//      cross-tenant hit).
	//   2. The DB returned no row for tenant B (tenancy filter at the SQL
	//      level), so the read errors with ErrNotFound — we never serve
	//      tenant A's row to tenant B even via cache.
	missesBefore := envA.metrics.missCount()
	hitsBefore := envA.metrics.hitCount()
	_, err = envB.client.Products().Get(ctx, aProduct.ID)
	if err == nil {
		t.Fatalf("Get B for A's PK: err = nil, want ErrNotFound (cross-tenant cache leak!)")
	}

	if got := envA.metrics.hitCount() - hitsBefore; got != 0 {
		t.Errorf("Get B: hits delta = %d, want 0 (cross-tenant cache hit indicates key collision)", got)
	}
	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("Get B: misses delta = %d, want 1", got)
	}

	// Sanity: Tenant A's second Get IS a hit (proves caching works at all,
	// so the previous "0 hits for B" isn't trivially zero because caching
	// is broken for everybody).
	hitsBeforeA := envA.metrics.hitCount()
	if _, err := envA.client.Products().Get(ctx, aProduct.ID); err != nil {
		t.Fatalf("Get A again: %v", err)
	}
	if got := envA.metrics.hitCount() - hitsBeforeA; got != 1 {
		t.Errorf("Get A second time: hits delta = %d, want 1 (cache should be warm)", got)
	}
}

// TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant verifies §29.5:
// BuildTenantTablePattern + InvalidatePattern on the backend evicts ONLY one
// tenant's entries from a table. This is the tenant-offboarding workflow —
// when a tenant leaves, their cache entries should evaporate without touching
// any other tenant's hot data.
func TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB = rewireCache(t, envB, envA.cache)

	ctx := context.Background()

	// Two rows, one per tenant. Cache them both.
	aProduct, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "A", SKU: "P-A", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	bProduct, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "B", SKU: "P-B", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	if _, err := envA.client.Products().Get(ctx, aProduct.ID); err != nil {
		t.Fatalf("warm A: %v", err)
	}
	if _, err := envB.client.Products().Get(ctx, bProduct.ID); err != nil {
		t.Fatalf("warm B: %v", err)
	}

	// Sanity: both reads from cache now.
	hitsBefore := envA.metrics.hitCount()
	if _, err := envA.client.Products().Get(ctx, aProduct.ID); err != nil {
		t.Fatalf("Get A warm: %v", err)
	}
	if _, err := envB.client.Products().Get(ctx, bProduct.ID); err != nil {
		t.Fatalf("Get B warm: %v", err)
	}
	if got := envA.metrics.hitCount() - hitsBefore; got != 2 {
		t.Errorf("hits after both warm-reads: %d, want 2", got)
	}

	// Per-tenant pattern eviction for tenant A.
	pattern := cache.BuildTenantTablePattern("sqlgen", "", "products", tenantA)
	if err := envA.backend.InvalidatePattern(ctx, pattern); err != nil {
		t.Fatalf("InvalidatePattern: %v", err)
	}

	// After eviction: A's read MISSES, B's read still HITS.
	missesBefore := envA.metrics.missCount()
	hitsBefore = envA.metrics.hitCount()

	if _, err := envA.client.Products().Get(ctx, aProduct.ID); err != nil {
		t.Fatalf("Get A post-evict: %v", err)
	}
	if _, err := envB.client.Products().Get(ctx, bProduct.ID); err != nil {
		t.Fatalf("Get B post-evict: %v", err)
	}

	deltaMisses := envA.metrics.missCount() - missesBefore
	deltaHits := envA.metrics.hitCount() - hitsBefore
	if deltaMisses != 1 {
		t.Errorf("post-evict misses: %d, want 1 (tenant A's entry should be gone)", deltaMisses)
	}
	if deltaHits != 1 {
		t.Errorf("post-evict hits: %d, want 1 (tenant B's entry should survive)", deltaHits)
	}
}

// TestCache_BuildTenantTablePatternFormat is a low-cost guard against pattern
// drift. The grammar is documented in PRD §29.5; if the pattern shape changes
// (e.g. wildcard moves, segment order flips), backend-side InvalidatePattern
// implementations break silently. This test asserts the externally-visible
// invariants without coupling to byte-exact format.
func TestCache_BuildTenantTablePatternFormat(t *testing.T) {
	got := cache.BuildTenantTablePattern("sqlgen", "", "products", tenantA)

	// Per §29.5: prefix → table head → tenant segment → trailing wildcard.
	// fingerprint and pk segments must NOT be in the pattern.
	if !strings.HasPrefix(got, "sqlgen:products:tenant:") {
		t.Errorf("pattern = %q, want prefix sqlgen:products:tenant:", got)
	}
	if !strings.HasSuffix(got, ":*") {
		t.Errorf("pattern = %q, want trailing :*", got)
	}
	if strings.Contains(got, "fingerprint:") {
		t.Errorf("pattern = %q, must not contain :fingerprint: (PRD §29.5)", got)
	}
	if strings.Contains(got, ":pk:") {
		t.Errorf("pattern = %q, must not contain :pk: (PRD §29.5)", got)
	}
	if strings.Count(got, "*") != 1 {
		t.Errorf("pattern = %q, must contain exactly one wildcard (Redis SCAN MATCH compat)", got)
	}
	if !strings.Contains(got, tenantA.String()) {
		t.Errorf("pattern = %q, must contain tenant value %s", got, tenantA)
	}
}

// TestCache_KSUIDTenantStringification verifies the §29.5 cache-key
// stringification path for "custom comparable wrapper types beyond uuid.UUID".
// KSUID is the canonical example — String() returns a base62-encoded form.
// The cache key infrastructure formats tenant via %v, which calls String(),
// so KSUID values produce stable, distinct keys.
//
// This is the smoke for the "TenantResolver[ksuid.KSUID] compiles + cache key
// segments stringify correctly" requirement from §29.2.4 + §29.5.
func TestCache_KSUIDTenantStringification(t *testing.T) {
	a := ksuid.New()
	b := ksuid.New()

	keyA := cache.BuildTenantKey("sqlgen", "", "items", "v1", a, int64(42))
	keyB := cache.BuildTenantKey("sqlgen", "", "items", "v1", b, int64(42))

	if keyA == keyB {
		t.Errorf("KSUID tenants produce same key: %q (collision)", keyA)
	}
	if !strings.Contains(keyA, a.String()) {
		t.Errorf("keyA %q missing tenant string %q", keyA, a.String())
	}
	if !strings.Contains(keyB, b.String()) {
		t.Errorf("keyB %q missing tenant string %q", keyB, b.String())
	}

	patternA := cache.BuildTenantTablePattern("sqlgen", "", "items", a)
	if !strings.Contains(patternA, a.String()) {
		t.Errorf("KSUID pattern %q missing tenant string %q", patternA, a.String())
	}
}

// TestCache_InvalidateManyOnTenantedTableErrors verifies the §29.5 contract
// that the public Cache.InvalidateMany on a tenanted table returns an actionable
// error rather than silently succeeding (which would leave per-tenant entries
// orphaned). Callers must use the auto-invalidation hook or
// BuildTenantTablePattern.
func TestCache_InvalidateManyOnTenantedTableErrors(t *testing.T) {
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	ctx := context.Background()

	err := envA.cache.InvalidateMany(ctx, "products", []any{int64(1), int64(2)})
	if err == nil {
		t.Fatalf("InvalidateMany on tenanted table: err = nil, want descriptive error per §29.5")
	}
	if !strings.Contains(err.Error(), "tenanted") {
		t.Errorf("error %q does not mention tenancy — must direct callers to the right escape hatch", err.Error())
	}
}

// rewireCache returns env with a Client that shares the given cache. The
// helper is here (rather than in main_test.go) because only cache-isolation
// tests need it; making the rest of the suite know about it would be noise.
func rewireCache(t *testing.T, env *testEnv, c *models.Cache) *testEnv {
	t.Helper()

	clientOpts := []models.ClientOption{
		models.WithTenantResolver(env.resolver),
		models.WithCache(c),
	}
	env.client = models.New(env.counter, clientOpts...)
	env.cache = c
	return env
}
