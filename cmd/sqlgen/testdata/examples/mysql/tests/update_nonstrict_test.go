package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// orders sets strict_updates: false in sqlgen.yml, so an Update whose key
// matches no row is an idempotent no-op: no entity and no error, rather than
// the ErrNotFound the read-back Get would report.
func TestUpdate_MissingPK_NonStrict_ReturnsNil_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	got, err := client.Orders().Update(ctx, int64(999999999), &models.UpdateOrderInput{Total: omittable.Set(10.0)})
	if err != nil {
		t.Fatalf("Update missing PK (strict_updates=false): got err=%v, want nil", err)
	}
	if got != nil {
		t.Errorf("Update missing PK (strict_updates=false): got %+v, want nil entity", got)
	}
}

// UpdateWithRelated composes Update for the parent. Under strict_updates:
// false a missing parent key leaves no parent to nest under, so the call
// writes nothing and returns no entity and no error. A nested create that ran
// anyway would fail the order_items foreign key.
func TestUpdateWithRelated_MissingPK_NonStrict_ReturnsNil_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	got, err := client.Orders().UpdateWithRelated(ctx, int64(999999999), &models.UpdateOrderWithRelatedInput{
		Order: models.UpdateOrderInput{Total: omittable.Set(10.0)},
		OrderItems: &models.OrderOrderItemsUpdateNested{
			Create: []*models.OrderOrderItemsCreateInput{{ProductID: 1, Quantity: 1, UnitPrice: 1}},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWithRelated missing PK (strict_updates=false): got err=%v, want nil", err)
	}
	if got != nil {
		t.Errorf("UpdateWithRelated missing PK (strict_updates=false): got %+v, want nil entity", got)
	}
}

// An Update with no field set issues no UPDATE and reads the row back. Under
// strict_updates: false a missing key is the same idempotent no-op as above,
// not the read-back's ErrNotFound, while an existing key still returns the row.
func TestUpdate_EmptyInput_NonStrict_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	t.Run("missing key returns nil", func(t *testing.T) {
		got, err := client.Orders().Update(ctx, int64(999999999), &models.UpdateOrderInput{})
		if err != nil {
			t.Fatalf("empty Update missing PK (strict_updates=false): got err=%v, want nil", err)
		}
		if got != nil {
			t.Errorf("empty Update missing PK (strict_updates=false): got %+v, want nil entity", got)
		}
	})

	t.Run("existing key returns the row", func(t *testing.T) {
		user, err := client.Users().Create(ctx, &models.CreateUserInput{Name: "NonStrictEmpty", Email: "nonstrict-empty@example.com"})
		if err != nil {
			t.Fatalf("Create user: %v", err)
		}
		t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID, Total: 1})
		if err != nil {
			t.Fatalf("Create order: %v", err)
		}
		t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

		got, err := client.Orders().Update(ctx, order.ID, &models.UpdateOrderInput{})
		if err != nil {
			t.Fatalf("empty Update existing PK (strict_updates=false): %v", err)
		}
		if got == nil || got.ID != order.ID {
			t.Errorf("empty Update existing PK (strict_updates=false): got %+v, want order %v", got, order.ID)
		}
	})
}

// The missing-parent guard is not over-broad: under strict_updates: false an
// UpdateWithRelated on an existing parent still writes its nested rows.
func TestUpdateWithRelated_ExistingPK_NonStrict_Writes_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "NonStrictNestedCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "NonStrictNestedProduct", Price: 5, SKU: "NSN-001",
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })
	user, err := client.Users().Create(ctx, &models.CreateUserInput{Name: "NonStrictNested", Email: "nonstrict-nested@example.com"})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID, Total: 1})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })
	t.Cleanup(func() { _, _ = testDB.ExecContext(ctx, "DELETE FROM order_items WHERE order_id = ?", order.ID) })

	got, err := client.Orders().UpdateWithRelated(ctx, order.ID, &models.UpdateOrderWithRelatedInput{
		Order: models.UpdateOrderInput{Total: omittable.Set(10.0)},
		OrderItems: &models.OrderOrderItemsUpdateNested{
			Create: []*models.OrderOrderItemsCreateInput{{ProductID: product.ID, Quantity: 2, UnitPrice: 5}},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWithRelated existing PK (strict_updates=false): %v", err)
	}
	if got == nil || got.ID != order.ID || got.Total != 10.0 {
		t.Errorf("UpdateWithRelated existing PK (strict_updates=false): got %+v, want order %d with total 10", got, order.ID)
	}
	var items int
	if err := testDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM order_items WHERE order_id = ?", order.ID).Scan(&items); err != nil {
		t.Fatalf("count order items: %v", err)
	}
	if items != 1 {
		t.Errorf("order_items for the order = %d, want 1 (nested create written)", items)
	}
}
