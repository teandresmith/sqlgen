package tests

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// A PK whose tenant matches the resolver is accepted, and the operation runs
// against that row: each PK-bearing method below changes or reports the row,
// not merely returns no error.
func TestCompositePK_MatchingTenantProceeds(t *testing.T) {
	ctx := context.Background()
	pk := models.OrderItemPK{WorkspaceID: tenantA, OrderID: 1, ProductID: 10}

	readQuantity := func(t *testing.T, env *testEnv) int64 {
		t.Helper()
		got, err := env.client.OrderItems().Get(ctx, pk)
		if err != nil {
			t.Fatalf("Get after the write: %v", err)
		}
		return got.Quantity
	}

	t.Run("Update", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		createOrderItem(t, env, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		if _, err := env.client.OrderItems().Update(ctx, pk, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(7))}); err != nil {
			t.Fatalf("Update with the matching tenant: %v", err)
		}
		if got := readQuantity(t, env); got != 7 {
			t.Errorf("quantity after Update = %d, want 7", got)
		}
	})

	t.Run("Increment", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		createOrderItem(t, env, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		in := models.IncrementInput[models.OrderItemIncrementColumn]{Column: models.OrderItemIncrementQuantity, Amount: 3}
		if err := env.client.OrderItems().Increment(ctx, pk, in); err != nil {
			t.Fatalf("Increment with the matching tenant: %v", err)
		}
		if got := readQuantity(t, env); got != 5 {
			t.Errorf("quantity after Increment = %d, want 5", got)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		createOrderItem(t, env, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		ok, err := env.client.OrderItems().Exists(ctx, pk)
		if err != nil || !ok {
			t.Errorf("Exists with the matching tenant = %v, %v, want true, nil", ok, err)
		}
	})

	t.Run("HardDelete", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		createOrderItem(t, env, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		if err := env.client.OrderItems().HardDelete(ctx, pk); err != nil {
			t.Fatalf("HardDelete with the matching tenant: %v", err)
		}
		if _, err := env.client.OrderItems().Get(ctx, pk); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get after HardDelete error = %v, want ErrNotFound", err)
		}
	})
}

// When the tenant column is part of the primary key, the PK carries the tenant
// and the update input does not: a row's tenant is not a mutable attribute.
func TestCompositePK_UpdateInputOmitsTheTenantColumn(t *testing.T) {
	if _, ok := reflect.TypeFor[models.OrderItemPK]().FieldByName("WorkspaceID"); !ok {
		t.Error("OrderItemPK has no WorkspaceID field, want the tenant column in the PK struct")
	}
	if f, ok := reflect.TypeFor[models.UpdateOrderItemInput]().FieldByName("WorkspaceID"); ok {
		t.Errorf("UpdateOrderItemInput has field %s, want no tenant column", f.Name)
	}
}

// Under SkipTenancy the resolver is not consulted and the PK's own tenant is
// used as-is: tenant B's client writes tenant A's row by its PK.
func TestCompositePK_SkipTenancyWritesAnotherTenantsRowByPK(t *testing.T) {
	ctx := context.Background()
	pk := models.OrderItemPK{WorkspaceID: tenantA, OrderID: 1, ProductID: 10}
	skip := func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true }

	t.Run("Update", func(t *testing.T) {
		resetDB(t)
		envA := newEnv(t, withResolver(staticResolver(tenantA)))
		envB := newEnv(t, withResolver(staticResolver(tenantB)))
		createOrderItem(t, envA, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		if _, err := envB.client.OrderItems().Update(ctx, pk, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(9))}, skip); err != nil {
			t.Fatalf("tenant B Update of tenant A's PK with SkipTenancy: %v", err)
		}
		got, err := envA.client.OrderItems().Get(ctx, pk)
		if err != nil {
			t.Fatalf("tenant A Get: %v", err)
		}
		if got.Quantity != 9 {
			t.Errorf("quantity after tenant B's Update = %d, want 9", got.Quantity)
		}
	})

	t.Run("HardDelete", func(t *testing.T) {
		resetDB(t)
		envA := newEnv(t, withResolver(staticResolver(tenantA)))
		envB := newEnv(t, withResolver(staticResolver(tenantB)))
		createOrderItem(t, envA, ctx, tenantA, pk.OrderID, pk.ProductID, 2)
		if err := envB.client.OrderItems().HardDelete(ctx, pk, skip); err != nil {
			t.Fatalf("tenant B HardDelete of tenant A's PK with SkipTenancy: %v", err)
		}
		if _, err := envA.client.OrderItems().Get(ctx, pk); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("tenant A Get after tenant B's HardDelete error = %v, want ErrNotFound", err)
		}
	})
}
