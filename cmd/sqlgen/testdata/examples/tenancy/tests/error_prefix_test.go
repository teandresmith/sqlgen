package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// A tenant mismatch is an error like any other a client method returns: it
// starts with the method's prefix exactly once and still matches
// tenancy.ErrMismatch.
func TestErrMismatch_CarriesTheMethodPrefix(t *testing.T) {
	ctx := context.Background()
	assertPrefix := func(t *testing.T, err error, prefix string) {
		t.Helper()
		if !errors.Is(err, tenancy.ErrMismatch) {
			t.Fatalf("err = %v, want tenancy.ErrMismatch", err)
		}
		if !strings.HasPrefix(err.Error(), prefix) || strings.Count(err.Error(), prefix) != 1 {
			t.Errorf("err = %q, want it to start with %q exactly once", err, prefix)
		}
	}
	mismatched := models.OrderItemPK{WorkspaceID: tenantB, OrderID: 1, ProductID: 10}

	t.Run("Get", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		_, err := env.client.OrderItems().Get(ctx, mismatched)
		assertPrefix(t, err, "get order_item: ")
	})
	t.Run("Update", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		_, err := env.client.OrderItems().Update(ctx, mismatched, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(1))})
		assertPrefix(t, err, "update order_item: ")
	})
	t.Run("Create", func(t *testing.T) {
		resetDB(t)
		env := newEnv(t, withResolver(staticResolver(tenantA)))
		_, err := env.client.Products().Create(ctx, &models.CreateProductInput{WorkspaceID: omittable.Set(tenantB), Name: "p", SKU: "P-1", Price: 1})
		assertPrefix(t, err, "create product: ")
	})
}
