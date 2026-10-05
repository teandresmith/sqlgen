package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// Increment e2e for MySQL (PRD §9.2). Mirrors postgres coverage adapted for
// MySQL numeric type surface: INT, SMALLINT, TINYINT, MEDIUMINT, BIGINT,
// DECIMAL(10,2) / DECIMAL(12,2), FLOAT, DOUBLE.
//
// The `orders` table is configured with `strict_updates: false` in sqlgen.yml
// to exercise the idempotent-on-missing-PK branch (§9.5).

func seedMySQLIncrementProduct(t *testing.T, ctx context.Context, client *models.Client, price float64) (*models.Product, func()) {
	t.Helper()
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: t.Name() + "-cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      t.Name() + "-product",
		Price:      price,
		SKU:        t.Name() + "-sku",
		Attributes: types.JSON([]byte(`{}`)),
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

func TestIncrement_DecimalColumn_Positive(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	product, cleanup := seedMySQLIncrementProduct(t, ctx, client, 10.00)
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

func TestIncrement_DecimalColumn_Negative(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	product, cleanup := seedMySQLIncrementProduct(t, ctx, client, 20.00)
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

func TestIncrement_DecimalColumn_Zero(t *testing.T) {
	// Uses orders.total (strict_updates=false in sqlgen.yml). On MySQL the
	// go-sql-driver default reports rows-AFFECTED (not rows-MATCHED), so
	// `UPDATE ... SET x = x + 0 WHERE id = N` reports 0 rows affected even
	// when the PK exists — which under strict_updates=true would be
	// indistinguishable from a missing PK and surface as ErrNotFound. Using
	// orders (strict_updates=false) sidesteps the rows-affected check and
	// verifies the no-op behavior via a post-Increment read.
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "inc-zero@example.com", Name: "IncZero", Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Total:  7.50,
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	if err := client.Orders().Increment(ctx, order.ID, models.IncrementInput[models.OrderIncrementColumn]{
		Column: models.OrderIncrementTotal,
		Amount: 0,
	}); err != nil {
		t.Fatalf("Increment total 0: %v", err)
	}

	got, err := client.Orders().Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if got.Total != 7.50 {
		t.Errorf("total after 0-delta: got %v, want 7.50 (unchanged)", got.Total)
	}
}

func TestIncrement_OrderItemColumns_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:   "inc-oi@example.com",
		Name:    "IncOI",
		Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	product, cleanupProduct := seedMySQLIncrementProduct(t, ctx, client, 5.00)
	t.Cleanup(cleanupProduct)

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID, Total: 0})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	item, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID:   order.ID,
		ProductID: product.ID,
		Quantity:  3,
		UnitPrice: 5.00,
	})
	if err != nil {
		t.Fatalf("Create order item: %v", err)
	}
	t.Cleanup(func() { _ = client.OrderItems().HardDelete(ctx, item.ID) })

	// INT quantity column
	if err := client.OrderItems().Increment(ctx, item.ID, models.IncrementInput[models.OrderItemIncrementColumn]{
		Column: models.OrderItemIncrementQuantity,
		Amount: 4,
	}); err != nil {
		t.Fatalf("Increment quantity: %v", err)
	}
	// DECIMAL unit_price column
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

func TestIncrement_MissingPK_StrictUpdates_ReturnsErrNotFound_MySQL(t *testing.T) {
	// products.strict_updates defaults to true → missing PK returns ErrNotFound.
	ctx := context.Background()
	client := newClient()

	err := client.Products().Increment(ctx, int64(999999999), models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 1,
	})
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("Increment missing PK (strict_updates=true): got err=%v, want ErrNotFound", err)
	}
}

func TestIncrement_MissingPK_NonStrict_ReturnsNil_MySQL(t *testing.T) {
	// orders.strict_updates is configured to false in sqlgen.yml → missing PK is
	// an idempotent no-op that returns nil (PRD §9.5).
	ctx := context.Background()
	client := newClient()

	err := client.Orders().Increment(ctx, int64(999999999), models.IncrementInput[models.OrderIncrementColumn]{
		Column: models.OrderIncrementTotal,
		Amount: 10,
	})
	if err != nil {
		t.Fatalf("Increment missing PK (strict_updates=false): got err=%v, want nil", err)
	}
}
