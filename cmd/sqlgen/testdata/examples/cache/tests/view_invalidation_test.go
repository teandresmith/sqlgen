package tests

// View cache invalidation coverage (PRD §27.11, §29.5).
//
// Views (PRD §16) can be cached but have no mutations of their own. Their
// caches are invalidated when one of their `invalidate_on:` source tables
// is mutated. The cache hook consults `patternsForSourceTable(table)` and
// fires `Backend.InvalidatePattern` for each matching view.
//
// **Scope notes documented inline:**
//
//  1. View read-through is NOT implemented in the landed codegen. Views go
//     straight to the DB — only the *invalidation* path for view caches is
//     wired (the cache hook consults `patternsForSourceTable` after a
//     mutation on a source table). The §27.11 invariant we can verify
//     end-to-end is: "mutations on a source table fire
//     `invalidate_pattern` for every view that lists it in
//     `invalidate_on`". Hot-cache hits on views themselves would require
//     a `readThrough{View}` codegen path, which is out of Phase-14 scope
//     (test-only rule). The pattern call IS observable via the spy
//     backend.
//
//  2. **No per-tenant view invalidation.** A view can be tenanted (PRD
//     §29.2.5 detects the tenant column on a view, or
//     `views.<name>.tenancy.enabled` asserts it), but that scopes its
//     reads only. A cached view has no read-through path, so its keys
//     carry no `:tenant:` segment and view invalidation fires
//     `BuildTablePattern` (tenant-fanout), not `BuildTenantTablePattern`.
//     Worst case is over-invalidation — every tenant's view entries get
//     evicted when one tenant mutates the source. That's a perf concern,
//     not a correctness leak, and with no read-through there's no
//     read-side cross-tenant exposure either. Should a view read-through
//     path land, §29.5's tenant-segment invariant applies to it, and
//     `BuildTenantTablePattern` (already unit-tested in
//     `cache/key_test.go` + `tenancy/tests/cache_test.go`) drops in.

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// viewPatternFor returns the recorded invalidate_pattern call whose pattern
// targets the given view name, or nil if none. Centralizes the substring
// check so the assertion is consistent across mutation kinds.
func viewPatternFor(env *testEnv, viewSegment string) *spyCall {
	for _, c := range env.backend.filterCalls("invalidate_pattern") {
		if strings.Contains(c.pattern, ":"+viewSegment+":") || strings.HasPrefix(c.pattern, "sqlgen:"+viewSegment+":") {
			c := c
			return &c
		}
	}
	return nil
}

// TestViewInvalidation_CreateOnSourceFiresViewPattern is the baseline
// path: a Create on `products` (the only source listed in
// `product_summary.invalidate_on`) must fire one invalidate_pattern call
// for the view's BuildTablePattern. The pattern is fingerprint-and-pk-
// agnostic so every cached view entry across every fingerprint generation
// gets evicted in a single sweep (§27.5 + §27.11).
func TestViewInvalidation_CreateOnSourceFiresViewPattern(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "VInvCreate", SKU: uniqueSku(t, "vinv-create"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	got := viewPatternFor(env, "product_summary")
	if got == nil {
		t.Fatalf("Create on products: no invalidate_pattern targeting product_summary, got %+v",
			env.backend.filterCalls("invalidate_pattern"))
	}
	// The pattern must be fingerprint-agnostic per §27.5 — the view's
	// BuildTablePattern is one trailing wildcard with no fingerprint or pk
	// segments, so it clears every generation in one pass.
	if !strings.HasSuffix(got.pattern, ":*") {
		t.Errorf("view pattern = %q, want trailing :*", got.pattern)
	}
	if strings.Contains(got.pattern, ":fingerprint:") {
		t.Errorf("view pattern = %q, must NOT contain :fingerprint: (PRD §27.5)", got.pattern)
	}
	if strings.Contains(got.pattern, ":pk:") {
		t.Errorf("view pattern = %q, must NOT contain :pk:", got.pattern)
	}
}

// TestViewInvalidation_AllMutationKindsFireViewPattern covers every
// mutation shape on the source table (Create, Update, HardDelete,
// HardDeleteWhere, Upsert) — each one should fire the view pattern. This
// catches a class of regressions where a new mutation op forgets to
// dispatch the view-invalidation arm of dispatchMutation (§27.11).
func TestViewInvalidation_AllMutationKindsFireViewPattern(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	tests := []struct {
		name string
		fn   func(t *testing.T) (cleanup func())
	}{
		{
			name: "Create",
			fn: func(t *testing.T) func() {
				p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
					Name: "C", SKU: uniqueSku(t, "c"), Price: 1.0,
				})
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				return func() { _ = env.client.Products().HardDelete(ctx, p.ID) }
			},
		},
		{
			name: "Update",
			fn: func(t *testing.T) func() {
				p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
					Name: "U", SKU: uniqueSku(t, "u"), Price: 1.0,
				})
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				env.backend.reset()
				if _, err := env.client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
					Name: omittable.Set("U2"),
				}); err != nil {
					t.Fatalf("Update: %v", err)
				}
				return func() { _ = env.client.Products().HardDelete(ctx, p.ID) }
			},
		},
		{
			name: "Upsert",
			fn: func(t *testing.T) func() {
				sku := uniqueSku(t, "ups")
				p, err := env.client.Products().Upsert(
					ctx,
					&models.CreateProductInput{Name: "Up", SKU: sku, Price: 1.0},
					models.ProductConflictSKU,
				)
				if err != nil {
					t.Fatalf("Upsert insert: %v", err)
				}
				env.backend.reset()
				if _, err := env.client.Products().Upsert(
					ctx,
					&models.CreateProductInput{Name: "Up2", SKU: sku, Price: 2.0},
					models.ProductConflictSKU,
				); err != nil {
					t.Fatalf("Upsert update: %v", err)
				}
				return func() { _ = env.client.Products().HardDelete(ctx, p.ID) }
			},
		},
		{
			name: "HardDelete",
			fn: func(t *testing.T) func() {
				p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
					Name: "HD", SKU: uniqueSku(t, "hd"), Price: 1.0,
				})
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				env.backend.reset()
				if err := env.client.Products().HardDelete(ctx, p.ID); err != nil {
					t.Fatalf("HardDelete: %v", err)
				}
				return func() {} // already deleted
			},
		},
		{
			name: "HardDeleteWhere",
			fn: func(t *testing.T) func() {
				sku := uniqueSku(t, "hdw")
				if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
					Name: "HDW", SKU: sku, Price: 1.0,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
				env.backend.reset()
				if err := env.client.Products().HardDeleteWhere(ctx, &models.ProductFilter{
					SKU: &comparator.String{Eq: &sku},
				}); err != nil {
					t.Fatalf("HardDeleteWhere: %v", err)
				}
				return func() {}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.backend.reset()
			cleanup := tt.fn(t)
			t.Cleanup(cleanup)

			if got := viewPatternFor(env, "product_summary"); got == nil {
				t.Errorf("%s on products did not fire invalidate_pattern for product_summary; got patterns: %+v",
					tt.name, env.backend.filterCalls("invalidate_pattern"))
			}
		})
	}
}

// TestViewInvalidation_NonSourceTableMutationDoesNotFireViewPattern is the
// negative-case guard: mutations on `articles` (NOT listed in
// product_summary.invalidate_on) must NOT fire the view pattern. The
// patternsForSourceTable map is the gating function — any drift that
// adds tables to the wrong view bucket would surface here.
func TestViewInvalidation_NonSourceTableMutationDoesNotFireViewPattern(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "Unrelated", Author: "view-neg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Articles().HardDelete(ctx, a.ID) })

	if got := viewPatternFor(env, "product_summary"); got != nil {
		t.Errorf("articles mutation fired view pattern: %q (articles is NOT in product_summary.invalidate_on)", got.pattern)
	}
}
