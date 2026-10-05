package tests

// Stream E2E (tenancy).
//
// Exercises PRD §9.4a + §29 (tenancy) end-to-end against the SQLite-backed
// tenancy example:
//   - default Stream narrows to the resolved tenant — rows from other tenants
//     never appear in the iterator
//   - SkipTenancy: true on Stream bypasses the tenant filter (admin path)
//   - missing-resolver fail-closed: when the resolver returns ErrMissing,
//     Stream emits a (nil, err) yield BEFORE any SQL roundtrip
//
// Per PRD §9.4a, Stream applies tenancy in-method via the same composition
// path as GetMany / Connection (mirrors the inline filter add — see
// templates/table/stream.go.tmpl). The filter is not delegated to a hook
// alone; it lands directly in the conditions list before BuildSelect runs.
// This means SkipTenancy is honored even when SkipHooks is also set
// (consistent with §29.4.4 "the only supported way to bypass the tenant
// filter is SkipTenancy").

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// seedTenantProducts inserts n products owned by the given tenant via a client
// whose resolver returns that tenant. Returns the SKUs in insertion order.
func seedTenantProducts(t *testing.T, ctx context.Context, tenant uuid.UUID, prefix string, n int) []string {
	t.Helper()
	env := newEnv(t, withResolver(staticResolver(tenant)))
	skus := make([]string, 0, n)
	for i := range n {
		sku := fmt.Sprintf("%s-%s-%05d", prefix, tenant.String()[0:8], i)
		if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
			Name:  fmt.Sprintf("%s-%d", prefix, i),
			SKU:   sku,
			Price: 1.0,
		}); err != nil {
			t.Fatalf("seed Product %s: %v", sku, err)
		}
		skus = append(skus, sku)
	}
	return skus
}

// TestStream_TenantFilterApplied — products owned by another tenant must not
// appear in a Stream call from a tenant-A-bound client.
func TestStream_TenantFilterApplied(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	skusA := seedTenantProducts(t, ctx, tenantA, "A", 5)
	skusB := seedTenantProducts(t, ctx, tenantB, "B", 3)

	// Sanity: distinct sku sets.
	if dup := overlap(skusA, skusB); len(dup) > 0 {
		t.Fatalf("seed leak: skus shared between tenants: %v", dup)
	}

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	skuPrefix := "A-"
	got := make([]string, 0, len(skusA))
	for p, err := range envA.client.Products().Stream(ctx, &models.StreamProductsInput{
		Filter: &models.ProductFilter{
			SKU: &comparator.String{Like: new("%-%")},
		},
	}) {
		if err != nil {
			t.Fatalf("Stream tenant A: %v", err)
		}
		got = append(got, p.SKU)
		if p.WorkspaceID != tenantA {
			t.Errorf("Stream tenant A yielded row owned by %v, want %v", p.WorkspaceID, tenantA)
		}
	}
	if len(got) != len(skusA) {
		t.Errorf("Stream tenant A: yielded %d rows, want %d", len(got), len(skusA))
	}
	for _, sku := range got {
		if !strings.HasPrefix(sku, skuPrefix) {
			t.Errorf("Stream tenant A yielded sku %q, want prefix %q", sku, skuPrefix)
		}
	}
}

// TestStream_SkipTenancyBypass — SkipTenancy:true on Stream returns rows
// across every tenant. Mirrors the §29.4.4 admin escape-hatch contract that
// other read methods (GetMany, Connection) honor.
func TestStream_SkipTenancyBypass(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	_ = seedTenantProducts(t, ctx, tenantA, "A", 5)
	_ = seedTenantProducts(t, ctx, tenantB, "B", 3)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	all := make([]models.Product, 0, 8)
	for p, err := range envA.client.Products().Stream(
		ctx, &models.StreamProductsInput{},
		func(o *models.CallOptions[models.StreamProductFieldOptions]) {
			o.SkipTenancy = true
		},
	) {
		if err != nil {
			t.Fatalf("Stream SkipTenancy: %v", err)
		}
		all = append(all, *p)
	}
	if len(all) != 8 {
		t.Errorf("Stream SkipTenancy yielded %d rows, want 8 (5+3 across tenants)", len(all))
	}
	seen := map[uuid.UUID]int{}
	for _, p := range all {
		seen[p.WorkspaceID]++
	}
	if seen[tenantA] != 5 || seen[tenantB] != 3 {
		t.Errorf("Stream SkipTenancy tenant counts = %+v, want {A:5, B:3}", seen)
	}
}

// TestStream_MissingResolverFailsClosed — when the resolver returns
// tenancy.ErrMissing, Stream emits a (nil, err) yield and terminates. The
// in-method tenant resolution runs BEFORE BuildSelect / conn.Query, so no
// SQL is issued.
func TestStream_MissingResolverFailsClosed(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	// Seed a row under tenant A so the table is non-empty (guards against the
	// "fail closed" assertion accidentally passing because the table is
	// empty and Stream hits clean EOF first).
	_ = seedTenantProducts(t, ctx, tenantA, "fail", 3)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()

	yielded := 0
	var streamErr error
	for p, err := range env.client.Products().Stream(ctx, &models.StreamProductsInput{}) {
		if err != nil {
			streamErr = err
			continue
		}
		_ = p
		yielded++
	}

	if streamErr == nil {
		t.Fatal("Stream with missing resolver: streamErr = nil, want tenancy.ErrMissing")
	}
	if !errors.Is(streamErr, tenancy.ErrMissing) {
		t.Errorf("Stream with missing resolver: err = %v, want errors.Is(tenancy.ErrMissing)", streamErr)
	}
	if yielded != 0 {
		t.Errorf("Stream with missing resolver: yielded %d rows, want 0", yielded)
	}
	// Tenancy resolution runs in-method before any conn.Query — the counter
	// must record 0 SELECT-class ops attributable to the failed Stream.
	// (Begin / commits from the seed step ran via a separate env+counter so
	// don't tick this counter; only the failing Stream call exercised it.)
	if got := env.counter.queries.Load(); got != 0 {
		t.Errorf("Stream with missing resolver: Query ops = %d, want 0 (fail-closed must short-circuit before SQL)", got)
	}
}

// overlap returns the intersection of two slices.
func overlap(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var out []string
	for _, s := range b {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}
