package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Increment e2e for tenancy (PRD §29). The composite-PK verify-match case for
// order_items is covered in composite_pk_test.go (TestCompositePK_Increment_*);
// this file focuses on the simple-PK path (products.price) where the tenant
// filter is injected into the WHERE clause rather than verified against a PK
// field.

// TestIncrement_TenantFilter_AutoApplied verifies that Increment on a
// simple-PK tenanted table refuses to cross tenants: tenant A's client cannot
// mutate tenant B's row — the tenant condition is AND'd into the UPDATE's
// WHERE, so the row is simply not found and (with strict_updates=true)
// ErrNotFound is returned.
func TestIncrement_TenantFilter_AutoApplied(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	// Seed one product in tenant B.
	productB, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "b-product", SKU: "B-SKU", Price: 10.0,
	})
	if err != nil {
		t.Fatalf("create product in tenant B: %v", err)
	}

	// Tenant A's client tries to increment tenant B's row by its PK —
	// the tenant filter in the UPDATE WHERE makes this a miss; with
	// strict_updates=true on products that surfaces as ErrNotFound.
	err = envA.client.Products().Increment(ctx, productB.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 99,
	})
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("cross-tenant Increment: got err=%v, want ErrNotFound", err)
	}

	// Verify the row in tenant B is untouched.
	got, err := envB.client.Products().Get(ctx, productB.ID)
	if err != nil {
		t.Fatalf("Get product in tenant B: %v", err)
	}
	if got.Price != 10.0 {
		t.Errorf("tenant B price leaked to cross-tenant Increment: got %v, want 10.0", got.Price)
	}
}

// TestIncrement_SkipTenancy_CrossTenantSucceeds verifies the §29.4.4 admin
// escape hatch: SkipTenancy:true drops the tenant filter from the WHERE, so a
// caller with an explicit tenant override can mutate across tenants. The only
// grep-able path to cross tenants on a mutation.
func TestIncrement_SkipTenancy_CrossTenantSucceeds(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	productB, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "b-product", SKU: "B-SKU", Price: 10.0,
	})
	if err != nil {
		t.Fatalf("create product in tenant B: %v", err)
	}

	// envA (resolver → tenantA) increments B's row via SkipTenancy.
	err = envA.client.Products().Increment(ctx, productB.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 5,
	}, func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("SkipTenancy Increment across tenants: %v", err)
	}

	got, err := envB.client.Products().Get(ctx, productB.ID)
	if err != nil {
		t.Fatalf("Get product in tenant B: %v", err)
	}
	if got.Price != 15.0 {
		t.Errorf("price after SkipTenancy Increment: got %v, want 15.0", got.Price)
	}
}

// TestIncrement_CompositePK_TenantMismatch_ShortCircuits verifies that on a
// composite-PK-with-tenant table (§29.7 verify-match), an Increment with a
// mismatched pk.WorkspaceID returns tenancy.ErrMismatch BEFORE issuing any
// SQL. This is a slightly different angle than composite_pk_test.go: we seed
// the row in tenant A, then probe it from tenant B's client with a PK that
// CORRECTLY names tenantA (so if the check were gated on resolver-match
// alone, it would pass and mutate) — the resolver-vs-PK verify-match is
// what stops it.
func TestIncrement_CompositePK_TenantMismatch_ShortCircuits(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	// envB's resolver returns tenantB; PK names tenantA → mismatch.
	envB.counter.reset()
	err := envB.client.OrderItems().Increment(ctx, models.OrderItemPK{
		WorkspaceID: tenantA, // does not match envB's resolver
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 1,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Increment with resolver-mismatched PK: got err=%v, want tenancy.ErrMismatch", err)
	}
	if got := envB.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on resolver-mismatch path: %d, want 0", got)
	}

	// Tenant A's row is untouched.
	got, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if err != nil {
		t.Fatalf("Get item in tenant A: %v", err)
	}
	if got.Quantity != 2 {
		t.Errorf("quantity leaked to cross-tenant Increment: got %d, want 2", got.Quantity)
	}
}

// TestIncrement_CompositePK_ZeroTenantOnPK_ShortCircuits verifies the §29.4.2
// zero-value-is-mismatch property: a caller who forgets to set pk.WorkspaceID
// triggers the verify-match guard (uuid.Nil != resolver → ErrMismatch) before
// any DB round-trip.
func TestIncrement_CompositePK_ZeroTenantOnPK_ShortCircuits(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	err := envA.client.OrderItems().Increment(ctx, models.OrderItemPK{
		// WorkspaceID: uuid.Nil (implicit)
		OrderID:   item.OrderID,
		ProductID: item.ProductID,
	}, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 1,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Increment with zero-UUID pk.WorkspaceID: got err=%v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant PK: %d, want 0", got)
	}
}

// TestIncrement_CompositePK_SkipTenancy_Succeeds verifies that SkipTenancy
// drops the verify-match check on composite-PK tables too; the caller owns
// the tenant value on the PK, and it is used verbatim in the WHERE.
func TestIncrement_CompositePK_SkipTenancy_Succeeds(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	// envB increments tenant A's row via SkipTenancy + explicit PK.
	err := envB.client.OrderItems().Increment(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 5,
	}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("SkipTenancy Increment on composite PK: %v", err)
	}

	got, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if err != nil {
		t.Fatalf("Get item in tenant A: %v", err)
	}
	if got.Quantity != 7 {
		t.Errorf("quantity after SkipTenancy Increment: got %d, want 7", got.Quantity)
	}
}

// Ensures the tenancy-resolver-missing path fires on Increment too (§29.3.1
// fail-closed): required resolver returns ErrMissing → Increment propagates.
func TestIncrement_TenantMissing_FailsClosed(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	ctx := context.Background()

	err := env.client.Products().Increment(ctx, int64(1), models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 1,
	})
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("Increment with missing tenant: got err=%v, want tenancy.ErrMissing", err)
	}
}

// Guard against accidental removal: the tenant type is uuid.UUID, so
// uuid.Nil is the zero value used by verify-match.
var _ uuid.UUID = tenantA
