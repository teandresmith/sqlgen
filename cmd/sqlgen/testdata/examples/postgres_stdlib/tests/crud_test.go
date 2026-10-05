package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- CRUD: Users ---

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "alice@example.com",
		Name:  "Alice",
		Role:  omittable.Set(models.UserRoleAdmin),
		Tags:  omittable.Set([]string{"go", "sql"}),
		Metadata: omittable.Set(mustJSON(map[string]any{
			"level": float64(5),
			"plan":  "pro",
		})),
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	if user.ID == (uuid.UUID{}) {
		t.Fatal("Create user: expected non-empty ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Create user email = %q, want %q", user.Email, "alice@example.com")
	}
	if user.Role != models.UserRoleAdmin {
		t.Errorf("Create user role = %v, want %v", user.Role, models.UserRoleAdmin)
	}

	// Get
	got, err := client.PublicUsers().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("Get user name = %q, want %q", got.Name, "Alice")
	}
	if diff := cmp.Diff([]string{"go", "sql"}, got.Tags); diff != "" {
		t.Errorf("Get user tags mismatch (-want +got):\n%s", diff)
	}

	// Update
	updated, err := client.PublicUsers().Update(ctx, user.ID, &models.UpdatePublicUserInput{
		Name: omittable.Set("Alice Updated"),
	})
	if err != nil {
		t.Fatalf("Update user: %v", err)
	}
	if updated.Name != "Alice Updated" {
		t.Errorf("Update user name = %q, want %q", updated.Name, "Alice Updated")
	}

	// HardDelete
	if err := client.PublicUsers().HardDelete(ctx, user.ID); err != nil {
		t.Fatalf("HardDelete user: %v", err)
	}
	exists, err := client.PublicUsers().Exists(ctx, user.ID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("user should not exist after HardDelete")
	}
}

// --- CRUD: Categories ---

func TestCategoryCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "Electronics",
	})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	got, err := client.Categories().Get(ctx, cat.ID)
	if err != nil {
		t.Fatalf("Get category: %v", err)
	}
	if got.Name != "Electronics" {
		t.Errorf("Get category name = %q, want %q", got.Name, "Electronics")
	}
}

// --- CRUD: Profiles ---

func TestProfileCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "profile-crud@example.com",
		Name:  "ProfileCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	bio := "Original bio"
	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID: user.ID,
		Bio:    omittable.Set(&bio),
		Scores: omittable.Set([]int32{90, 85}),
	})
	if err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	if profile.ID == (uuid.UUID{}) {
		t.Fatal("Create profile: expected non-empty ID")
	}
	if profile.Bio == nil || *profile.Bio != "Original bio" {
		t.Errorf("Create profile bio = %v, want %q", profile.Bio, "Original bio")
	}

	// Get
	got, err := client.Profiles().Get(ctx, profile.ID)
	if err != nil {
		t.Fatalf("Get profile: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("Get profile user_id = %q, want %q", got.UserID, user.ID)
	}

	// Update
	newBio := "Updated bio"
	updated, err := client.Profiles().Update(ctx, profile.ID, &models.UpdateProfileInput{
		Bio: omittable.Set(&newBio),
	})
	if err != nil {
		t.Fatalf("Update profile: %v", err)
	}
	if updated.Bio == nil || *updated.Bio != "Updated bio" {
		t.Errorf("Update profile bio = %v, want %q", updated.Bio, "Updated bio")
	}

	// HardDelete
	if err := client.Profiles().HardDelete(ctx, profile.ID); err != nil {
		t.Fatalf("HardDelete profile: %v", err)
	}
	exists, err := client.Profiles().Exists(ctx, profile.ID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("profile should not exist after HardDelete")
	}
}

// --- CRUD: Products ---

func TestProductCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ProductCRUDCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "TestProduct",
		Price:      29.99,
		Quantity:   omittable.Set[int32](10),
		CategoryID: cat.ID,
		Tags:       omittable.Set([]string{"new", "sale"}),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	if product.ID == (uuid.UUID{}) {
		t.Fatal("Create product: expected non-empty ID")
	}
	if product.Name != "TestProduct" {
		t.Errorf("Create product name = %q, want %q", product.Name, "TestProduct")
	}
	if product.Price != 29.99 {
		t.Errorf("Create product price = %v, want 29.99", product.Price)
	}

	// Get
	got, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if got.Quantity != 10 {
		t.Errorf("Get product quantity = %d, want 10", got.Quantity)
	}

	// Update
	updated, err := client.Products().Update(ctx, product.ID, &models.UpdateProductInput{
		Name:  omittable.Set("UpdatedProduct"),
		Price: omittable.Set(39.99),
	})
	if err != nil {
		t.Fatalf("Update product: %v", err)
	}
	if updated.Name != "UpdatedProduct" {
		t.Errorf("Update product name = %q, want %q", updated.Name, "UpdatedProduct")
	}
	if updated.Price != 39.99 {
		t.Errorf("Update product price = %v, want 39.99", updated.Price)
	}

	// HardDelete
	if err := client.Products().HardDelete(ctx, product.ID); err != nil {
		t.Fatalf("HardDelete product: %v", err)
	}
	exists, err := client.Products().Exists(ctx, product.ID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("product should not exist after HardDelete")
	}
}

// --- CRUD: Orders ---

func TestOrderCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "order-crud@example.com",
		Name:  "OrderCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	notes := "Rush delivery"
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Status: omittable.Set(models.OrderStatusPending),
		Total:  omittable.Set(99.50),
		Notes:  omittable.Set(&notes),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if order.ID == (uuid.UUID{}) {
		t.Fatal("Create order: expected non-empty ID")
	}
	if order.Status != models.OrderStatusPending {
		t.Errorf("Create order status = %v, want %v", order.Status, models.OrderStatusPending)
	}
	if order.Total != 99.50 {
		t.Errorf("Create order total = %v, want 99.50", order.Total)
	}

	// Get
	got, err := client.Orders().Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if got.Notes == nil || *got.Notes != "Rush delivery" {
		t.Errorf("Get order notes = %v, want %q", got.Notes, "Rush delivery")
	}

	// Update
	updated, err := client.Orders().Update(ctx, order.ID, &models.UpdateOrderInput{
		Status: omittable.Set(models.OrderStatusShipped),
		Total:  omittable.Set(109.50),
	})
	if err != nil {
		t.Fatalf("Update order: %v", err)
	}
	if updated.Status != models.OrderStatusShipped {
		t.Errorf("Update order status = %v, want %v", updated.Status, models.OrderStatusShipped)
	}
	if updated.Total != 109.50 {
		t.Errorf("Update order total = %v, want 109.50", updated.Total)
	}

	// HardDelete
	if err := client.Orders().HardDelete(ctx, order.ID); err != nil {
		t.Fatalf("HardDelete order: %v", err)
	}
	exists, err := client.Orders().Exists(ctx, order.ID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("order should not exist after HardDelete")
	}
}

// --- CRUD: Order Items ---

func TestOrderItemCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "oi-crud@example.com",
		Name:  "OrderItemCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "OICRUDCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "OIProduct",
		Price:      15.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	item, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID:   order.ID,
		ProductID: product.ID,
		Quantity:  omittable.Set[int32](3),
		UnitPrice: 15.00,
	})
	if err != nil {
		t.Fatalf("Create order item: %v", err)
	}
	if item.ID == (uuid.UUID{}) {
		t.Fatal("Create order item: expected non-empty ID")
	}
	if item.Quantity != 3 {
		t.Errorf("Create order item quantity = %d, want 3", item.Quantity)
	}
	if item.UnitPrice != 15.00 {
		t.Errorf("Create order item unit_price = %v, want 15.00", item.UnitPrice)
	}

	// Get
	got, err := client.OrderItems().Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get order item: %v", err)
	}
	if got.OrderID != order.ID {
		t.Errorf("Get order item order_id = %q, want %q", got.OrderID, order.ID)
	}
	if got.ProductID != product.ID {
		t.Errorf("Get order item product_id = %q, want %q", got.ProductID, product.ID)
	}

	// Update
	updated, err := client.OrderItems().Update(ctx, item.ID, &models.UpdateOrderItemInput{
		Quantity:  omittable.Set[int32](5),
		UnitPrice: omittable.Set(12.50),
	})
	if err != nil {
		t.Fatalf("Update order item: %v", err)
	}
	if updated.Quantity != 5 {
		t.Errorf("Update order item quantity = %d, want 5", updated.Quantity)
	}
	if updated.UnitPrice != 12.50 {
		t.Errorf("Update order item unit_price = %v, want 12.50", updated.UnitPrice)
	}

	// HardDelete
	if err := client.OrderItems().HardDelete(ctx, item.ID); err != nil {
		t.Fatalf("HardDelete order item: %v", err)
	}
	exists, err := client.OrderItems().Exists(ctx, item.ID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("order item should not exist after HardDelete")
	}
}
