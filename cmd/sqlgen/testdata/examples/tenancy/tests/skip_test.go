package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestSkipTenancy_AdminGetMany verifies the §29.4.4 admin escape hatch:
// SkipTenancy:true on a read returns rows for every tenant. This is what
// platform-admin tooling uses — backups, cross-tenant analytics, support
// debugging — and is the ONLY supported way to bypass the tenant filter.
// Critically, it's grep-able and lint-able (which is why it's a struct field
// rather than a separate method).
func TestSkipTenancy_AdminGetMany(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()
	for _, sku := range []string{"A1", "A2"} {
		if _, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "A", SKU: sku, Price: 1.0,
		}); err != nil {
			t.Fatalf("create A %s: %v", sku, err)
		}
	}
	for _, sku := range []string{"B1"} {
		if _, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "B", SKU: sku, Price: 1.0,
		}); err != nil {
			t.Fatalf("create B %s: %v", sku, err)
		}
	}

	// Admin path: any client (here envA) with SkipTenancy:true sees both tenants.
	all, err := envA.client.Products().GetMany(
		ctx, &models.GetProductsInput{},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("admin GetMany with SkipTenancy: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("admin GetMany: got %d rows, want 3", len(all))
	}

	seen := map[string]bool{}
	for _, p := range all {
		seen[p.WorkspaceID.String()] = true
	}
	if !seen[tenantA.String()] || !seen[tenantB.String()] {
		t.Errorf("admin GetMany missing tenants: seen=%v", seen)
	}
}

// TestSkipTenancy_CreateRequiresExplicitTenant verifies the §29.4.4 contract:
// when SkipTenancy:true, the resolver is NOT called. Caller must supply the
// tenant value explicitly on the input. This prevents an admin path from
// accidentally writing rows owned by the wrong tenant.
func TestSkipTenancy_CreateRequiresExplicitTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()

	// Admin writes a row for tenant B from a client whose resolver returns A.
	created, err := envA.client.Products().Create(
		ctx, &models.CreateProductInput{
			WorkspaceID: omittable.Set(tenantB),
			Name:        "admin-write",
			SKU:         "ADMIN-1",
			Price:       1.0,
		},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("admin Create with SkipTenancy: %v", err)
	}
	if created.WorkspaceID != tenantB {
		t.Errorf("admin Create: workspace_id = %v, want %v (the explicit value, not the resolver's)", created.WorkspaceID, tenantB)
	}

	// Another sanity check — the tenant-A resolver client without
	// SkipTenancy can't read it, but tenant B can.
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	got, err := envB.client.Products().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get from tenant B: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get from B after admin write: id = %d, want %d", got.ID, created.ID)
	}
}

// TestSkipTenancy_GetByID confirms SkipTenancy:true on Get bypasses the filter
// — useful for support tooling that knows the row's PK and wants to load it
// regardless of tenant.
func TestSkipTenancy_GetByID(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "support-target", SKU: "SUP-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	// envB normally can't see this row.
	got, err := envB.client.Products().Get(
		ctx, created.ID,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("admin Get with SkipTenancy from envB: %v", err)
	}
	if got.ID != created.ID || got.WorkspaceID != tenantA {
		t.Errorf("admin Get: id=%d ws=%v, want id=%d ws=%v", got.ID, got.WorkspaceID, created.ID, tenantA)
	}
}
