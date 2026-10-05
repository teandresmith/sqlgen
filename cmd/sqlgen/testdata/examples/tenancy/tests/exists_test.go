package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Exists / ExistsWhere e2e for tenancy (PRD §9.1, §29.4.1, §29.4.4, §29.7,
// §29.3.1). The composite-PK verify-match case lives in composite_pk_test.go
// (TestCompositePK_Exists_*); this file covers the simple-PK path
// (products.id) where the tenant condition is AND'd into the WHERE, plus the
// resolver-missing fail-closed path for both methods.
//
// Scope note: ExistsWhere does NOT return ErrEmptyFilter (§22 limits that
// sentinel to mutation *Where ops); an unfiltered ExistsWhere still has the
// tenant condition injected by the runtime and is therefore safe in tenanted
// projects even with a nil filter.

// TestExists_CrossTenant_ReturnsFalse verifies that tenant A's client cannot
// see tenant B's product by PK — the tenant condition is AND'd into the
// EXISTS subquery's WHERE, so the row is not found and the scalar returns
// false (no ErrNotFound for Exists — it's a bool probe).
func TestExists_CrossTenant_ReturnsFalse(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	productB, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "b-exists", SKU: "B-EXISTS-SKU", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create product in tenant B: %v", err)
	}

	// B sees its own row.
	exists, err := envB.client.Products().Exists(ctx, productB.ID)
	if err != nil {
		t.Fatalf("Exists in tenant B: %v", err)
	}
	if !exists {
		t.Error("Exists(own row, tenant B) = false, want true")
	}

	// A does not.
	exists, err = envA.client.Products().Exists(ctx, productB.ID)
	if err != nil {
		t.Fatalf("Exists cross-tenant: %v", err)
	}
	if exists {
		t.Error("Exists(tenant B row, tenant A client) = true, want false")
	}
}

// TestExists_SkipTenancy_BypassesFilter verifies the §29.4.4 admin escape:
// SkipTenancy drops the tenant condition from the EXISTS subquery, so the
// probe sees rows regardless of tenancy.
func TestExists_SkipTenancy_BypassesFilter(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	productB, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "b-exists-skip", SKU: "B-EXISTS-SKIP-SKU", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create product in tenant B: %v", err)
	}

	exists, err := envA.client.Products().Exists(ctx, productB.ID,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("Exists with SkipTenancy: %v", err)
	}
	if !exists {
		t.Error("Exists(tenant B row, tenant A client, SkipTenancy) = false, want true")
	}
}

// TestExistsWhere_CrossTenant_ReturnsFalse mirrors the Exists test for the
// filter-based variant — even with a filter that matches tenant B's row on
// a non-tenant column, tenant A's client returns false.
func TestExistsWhere_CrossTenant_ReturnsFalse(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	_, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "b-ew", SKU: "B-EW-SKU", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create product in tenant B: %v", err)
	}

	skuB := "B-EW-SKU"

	// A probes by SKU — B's row matches on SKU but is filtered out by tenant.
	exists, err := envA.client.Products().ExistsWhere(ctx, &models.ProductFilter{
		SKU: &comparator.String{Eq: &skuB},
	})
	if err != nil {
		t.Fatalf("ExistsWhere cross-tenant: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(sku=B, tenant A client) = true, want false")
	}

	// SkipTenancy exposes it.
	exists, err = envA.client.Products().ExistsWhere(ctx, &models.ProductFilter{
		SKU: &comparator.String{Eq: &skuB},
	}, func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("ExistsWhere SkipTenancy: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(sku=B, tenant A client, SkipTenancy) = false, want true")
	}
}

// TestExists_CompositePK_TenantMismatch_ShortCircuits verifies §29.7
// verify-match on the Exists entry point: a PK whose WorkspaceID does not
// match the resolved tenant returns tenancy.ErrMismatch BEFORE any DB op.
func TestExists_CompositePK_TenantMismatch_ShortCircuits(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envB.counter.reset()
	_, err := envB.client.OrderItems().Exists(ctx, models.OrderItemPK{
		WorkspaceID: tenantA, // mismatched against envB's resolver
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Exists with resolver-mismatched PK: got err=%v, want tenancy.ErrMismatch", err)
	}
	if got := envB.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on mismatch path: %d, want 0", got)
	}
}

// TestExists_CompositePK_ZeroTenantOnPK_ShortCircuits verifies §29.4.2
// zero-value-is-mismatch: forgetting to set pk.WorkspaceID leaves uuid.Nil(),
// which can't match the resolver — fail-fast before any SQL.
func TestExists_CompositePK_ZeroTenantOnPK_ShortCircuits(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Exists(ctx, models.OrderItemPK{
		// WorkspaceID: uuid.Nil (implicit)
		OrderID:   item.OrderID,
		ProductID: item.ProductID,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Exists with zero-UUID pk.WorkspaceID: got err=%v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant PK: %d, want 0", got)
	}
}

// TestExists_CompositePK_SkipTenancy_Succeeds verifies that SkipTenancy drops
// the verify-match on composite-PK tables too.
func TestExists_CompositePK_SkipTenancy_Succeeds(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	exists, err := envB.client.OrderItems().Exists(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("Exists with SkipTenancy + PK tenantA: %v", err)
	}
	if !exists {
		t.Error("Exists(tenant A PK, tenant B client, SkipTenancy) = false, want true")
	}
}

// TestExists_TenantMissing_FailsClosed verifies the §29.3.1 fail-closed
// contract on Exists: a required resolver that returns ErrMissing propagates
// out as tenancy.ErrMissing, no SQL round-trip.
func TestExists_TenantMissing_FailsClosed(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	ctx := context.Background()

	env.counter.reset()
	_, err := env.client.Products().Exists(ctx, int64(1))
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("Exists with missing tenant: got err=%v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on fail-closed path: %d, want 0", got)
	}
}

// TestExistsWhere_TenantMissing_FailsClosed mirrors the fail-closed check for
// the filter-based variant.
func TestExistsWhere_TenantMissing_FailsClosed(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	ctx := context.Background()

	env.counter.reset()
	_, err := env.client.Products().ExistsWhere(ctx, nil)
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("ExistsWhere(nil) with missing tenant: got err=%v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on fail-closed path: %d, want 0", got)
	}
}

// Guard: tenant type is uuid.UUID (uuid.Nil is the zero-value used by
// verify-match).
var _ uuid.UUID = tenantA
