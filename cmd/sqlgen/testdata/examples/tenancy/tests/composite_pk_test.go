package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// The order_items table has workspace_id as part of its composite primary key
// (PRIMARY KEY (workspace_id, order_id, product_id) — see schema.sql). Per
// PRD §29.7 with the Option-2 verify-match rule, every PK-scoped operation
// (Get / Update / Delete / Exists / Increment) must compare the caller-supplied
// pk.WorkspaceID against the resolved tenant and short-circuit with
// tenancy.ErrMismatch when they diverge — no auto-fill, no silent-zero-rows.

func createOrderItem(t *testing.T, env *testEnv, ctx context.Context, tenantID uuid.UUID, orderID, productID int64, qty int64) *models.OrderItem {
	t.Helper()
	created, err := env.client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantID,
		OrderID:     orderID,
		ProductID:   productID,
		Quantity:    qty,
		UnitPrice:   1.5,
	})
	if err != nil {
		t.Fatalf("create order_item (tenant %v, order %d, product %d): %v", tenantID, orderID, productID, err)
	}
	return created
}

func TestCompositePK_Get_MatchSucceeds(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	got, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if err != nil {
		t.Fatalf("Get with matching pk.WorkspaceID: %v", err)
	}
	if got.WorkspaceID != tenantA || got.OrderID != item.OrderID || got.ProductID != item.ProductID {
		t.Errorf("Get returned wrong row: %+v", got)
	}
}

func TestCompositePK_Get_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantB, // mismatched
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Get with mismatched pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on PK mismatch path: %d, want 0", got)
	}
}

func TestCompositePK_Get_ZeroTenantOnPKReturnsErrMismatch(t *testing.T) {
	// Option-2 rule: caller always supplies the tenant; forgetting to set it
	// leaves the zero-UUID on the PK struct, which doesn't match the resolver.
	// That's a loud, early error — which is what we want.
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	_ = createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		// WorkspaceID: uuid.Nil (implicit)
		OrderID:   1,
		ProductID: 10,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Get with zero-UUID pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on zero-tenant PK: %d, want 0", got)
	}
}

func TestCompositePK_Update_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Update(ctx, models.OrderItemPK{
		WorkspaceID: tenantB, // mismatched
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, &models.UpdateOrderItemInput{
		Quantity: omittable.Set(int64(99)),
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Update with mismatched pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on Update PK mismatch: %d, want 0", got)
	}
}

func TestCompositePK_HardDelete_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	err := envA.client.OrderItems().HardDelete(ctx, models.OrderItemPK{
		WorkspaceID: tenantB, // mismatched
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("HardDelete with mismatched pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on HardDelete PK mismatch: %d, want 0", got)
	}

	// Row must still exist — mismatch must not partially-commit.
	got, err := envA.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if err != nil {
		t.Fatalf("Get after mismatch HardDelete: %v", err)
	}
	if got == nil {
		t.Errorf("row missing after mismatch HardDelete attempt")
	}
}

func TestCompositePK_HardDeleteMany_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item1 := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)
	item2 := createOrderItem(t, envA, ctx, tenantA, 2, 20, 3)

	envA.counter.reset()
	err := envA.client.OrderItems().HardDeleteMany(ctx, []models.OrderItemPK{
		{WorkspaceID: tenantA, OrderID: item1.OrderID, ProductID: item1.ProductID},
		{WorkspaceID: tenantB, OrderID: item2.OrderID, ProductID: item2.ProductID}, // poison
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("HardDeleteMany with mismatched pk in batch: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on HardDeleteMany batch mismatch: %d, want 0", got)
	}

	// Neither row should be deleted — the mismatch must reject the whole batch.
	listA, err := envA.client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{})
	if err != nil {
		t.Fatalf("GetMany after mismatch HardDeleteMany: %v", err)
	}
	if len(listA) != 2 {
		t.Errorf("batch mismatch partially committed: got %d rows, want 2", len(listA))
	}
}

func TestCompositePK_Exists_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	_, err := envA.client.OrderItems().Exists(ctx, models.OrderItemPK{
		WorkspaceID: tenantB, // mismatched
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Exists with mismatched pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on Exists PK mismatch: %d, want 0", got)
	}
}

func TestCompositePK_Increment_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	envA.counter.reset()
	err := envA.client.OrderItems().Increment(ctx, models.OrderItemPK{
		WorkspaceID: tenantB, // mismatched
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 1,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Increment with mismatched pk.WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on Increment PK mismatch: %d, want 0", got)
	}
}

func TestCompositePK_SkipTenancy_AdminCrossTenantGet(t *testing.T) {
	// §29.4.4: SkipTenancy:true bypasses the verify-match — the caller supplies
	// the tenant value explicitly; the resolver is not consulted. This is the
	// only grep-able / lint-able way to read across tenants by PK.
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	ctx := context.Background()

	item := createOrderItem(t, envA, ctx, tenantA, 1, 10, 2)

	// envB (resolver → tenantB) reads A's row explicitly via SkipTenancy.
	got, err := envB.client.OrderItems().Get(ctx, models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("admin Get with SkipTenancy across tenants: %v", err)
	}
	if got.WorkspaceID != tenantA {
		t.Errorf("admin cross-tenant Get: workspace_id = %v, want %v", got.WorkspaceID, tenantA)
	}
}
