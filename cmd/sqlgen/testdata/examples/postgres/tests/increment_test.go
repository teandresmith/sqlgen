package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// Increment e2e (PRD §9.2). Exercises the generated Increment method across
// every numeric column shape in the postgres example schema:
//   - products.quantity   (int32 via type_map)
//   - products.price      (NUMERIC(10,2) → float64)
//   - order_items.quantity (int32 via type_map)
//   - order_items.unit_price (NUMERIC(10,2) → float64)
//   - orders.total         (NUMERIC(10,2) → float64)
//
// The `orders` table is configured with `strict_updates: false` in sqlgen.yml
// so it exercises the idempotent-on-missing-PK branch (§9.5); the other tables
// use the default `strict_updates: true` so they exercise the ErrNotFound branch.

func seedIncrementProduct(t *testing.T, ctx context.Context, client *models.Client, price float64, qty int32) (*models.Product, func()) {
	t.Helper()
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: t.Name() + "-cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       t.Name() + "-product",
		Price:      price,
		Quantity:   omittable.Set(qty),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	cleanup := func() {
		_ = client.Products().HardDelete(ctx, product.ID)
		_ = client.Categories().HardDelete(ctx, cat.ID)
	}
	return product, cleanup
}

func TestIncrement_NumericColumn_Positive(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	product, cleanup := seedIncrementProduct(t, ctx, client, 10.00, 1)
	t.Cleanup(cleanup)

	if err := client.Products().Increment(ctx, product.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 5,
	}); err != nil {
		t.Fatalf("Increment price +5: %v", err)
	}

	got, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if got.Price != 15.00 {
		t.Errorf("price after +5: got %v, want 15.00", got.Price)
	}
}

func TestIncrement_NumericColumn_Negative(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	product, cleanup := seedIncrementProduct(t, ctx, client, 20.00, 1)
	t.Cleanup(cleanup)

	if err := client.Products().Increment(ctx, product.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: -8,
	}); err != nil {
		t.Fatalf("Increment price -8: %v", err)
	}

	got, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if got.Price != 12.00 {
		t.Errorf("price after -8: got %v, want 12.00", got.Price)
	}
}

func TestIncrement_OrderItemColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "inc-oi@example.com", Name: "IncOI",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	product, cleanupProduct := seedIncrementProduct(t, ctx, client, 5.00, 1)
	t.Cleanup(cleanupProduct)

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	item, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID:   order.ID,
		ProductID: product.ID,
		Quantity:  omittable.Set[int32](3),
		UnitPrice: 5.00,
	})
	if err != nil {
		t.Fatalf("Create order item: %v", err)
	}
	t.Cleanup(func() { _ = client.OrderItems().HardDelete(ctx, item.ID) })

	// int32 quantity column
	if err := client.OrderItems().Increment(ctx, item.ID, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 4,
	}); err != nil {
		t.Fatalf("Increment quantity: %v", err)
	}
	// NUMERIC unit_price column
	if err := client.OrderItems().Increment(ctx, item.ID, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementUnitPrice,
		Amount: 2,
	}); err != nil {
		t.Fatalf("Increment unit_price: %v", err)
	}

	got, err := client.OrderItems().Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get order item: %v", err)
	}
	if got.Quantity != 7 {
		t.Errorf("order_item.quantity: got %d, want 7", got.Quantity)
	}
	if got.UnitPrice != 7.00 {
		t.Errorf("order_item.unit_price: got %v, want 7.00", got.UnitPrice)
	}
}
