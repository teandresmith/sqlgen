package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// --- CRUD: Users ---

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:   "alice@example.com",
		Name:    "Alice",
		Balance: omittable.Set(100.50),
		Role:    omittable.Set("admin"),
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("Create user: expected non-zero ID")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Create user email = %q, want %q", user.Email, "alice@example.com")
	}
	if user.Role != "admin" {
		t.Errorf("Create user role = %q, want %q", user.Role, "admin")
	}

	// Get
	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("Get user name = %q, want %q", got.Name, "Alice")
	}
	if got.Balance != 100.50 {
		t.Errorf("Get user balance = %v, want 100.50", got.Balance)
	}

	// Update
	updated, err := client.Users().Update(ctx, user.ID, &models.UpdateUserInput{
		Name:    omittable.Set("Alice Updated"),
		Balance: omittable.Set(200.75),
	})
	if err != nil {
		t.Fatalf("Update user: %v", err)
	}
	if updated.Name != "Alice Updated" {
		t.Errorf("Update user name = %q, want %q", updated.Name, "Alice Updated")
	}
	if updated.Balance != 200.75 {
		t.Errorf("Update user balance = %v, want 200.75", updated.Balance)
	}

	// Exists
	exists, err := client.Users().Exists(ctx, user.ID)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("user should exist before HardDelete")
	}

	// HardDelete
	if err := client.Users().HardDelete(ctx, user.ID); err != nil {
		t.Fatalf("HardDelete user: %v", err)
	}
	exists, err = client.Users().Exists(ctx, user.ID)
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

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "profile-crud@example.com", Name: "ProfileCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	website := "https://example.com"
	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID:  user.ID,
		Website: omittable.Set(&website),
	})
	if err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	if profile.ID == 0 {
		t.Fatal("Create profile: expected non-zero ID")
	}
	if profile.Website == nil || *profile.Website != "https://example.com" {
		t.Errorf("Create profile website = %v, want %q", profile.Website, "https://example.com")
	}

	// Get
	got, err := client.Profiles().Get(ctx, profile.ID)
	if err != nil {
		t.Fatalf("Get profile: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("Get profile user_id = %d, want %d", got.UserID, user.ID)
	}

	// Update
	newWebsite := "https://updated.com"
	updated, err := client.Profiles().Update(ctx, profile.ID, &models.UpdateProfileInput{
		Website: omittable.Set(&newWebsite),
	})
	if err != nil {
		t.Fatalf("Update profile: %v", err)
	}
	if updated.Website == nil || *updated.Website != "https://updated.com" {
		t.Errorf("Update profile website = %v, want %q", updated.Website, "https://updated.com")
	}

	// HardDelete
	if err := client.Profiles().HardDelete(ctx, profile.ID); err != nil {
		t.Fatalf("HardDelete profile: %v", err)
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
		CategoryID: cat.ID,
		Title:      "TestProduct",
		Price:      29.99,
		SKU:        "TEST-001",
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	if product.ID == 0 {
		t.Fatal("Create product: expected non-zero ID")
	}
	if product.Title != "TestProduct" {
		t.Errorf("Create product title = %q, want %q", product.Title, "TestProduct")
	}
	if product.Price != 29.99 {
		t.Errorf("Create product price = %v, want 29.99", product.Price)
	}

	// Get
	got, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if got.SKU != "TEST-001" {
		t.Errorf("Get product sku = %q, want %q", got.SKU, "TEST-001")
	}

	// Update
	updated, err := client.Products().Update(ctx, product.ID, &models.UpdateProductInput{
		Title: omittable.Set("UpdatedProduct"),
		Price: omittable.Set(39.99),
	})
	if err != nil {
		t.Fatalf("Update product: %v", err)
	}
	if updated.Title != "UpdatedProduct" {
		t.Errorf("Update product title = %q, want %q", updated.Title, "UpdatedProduct")
	}

	// HardDelete
	if err := client.Products().HardDelete(ctx, product.ID); err != nil {
		t.Fatalf("HardDelete product: %v", err)
	}
}

// --- CRUD: Orders ---

func TestOrderCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "order-crud@example.com", Name: "OrderCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	notes := "Rush delivery"
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Status: omittable.Set("pending"),
		Total:  omittable.Set(99.50),
		Notes:  omittable.Set(&notes),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if order.ID == 0 {
		t.Fatal("Create order: expected non-zero ID")
	}
	if order.Status != "pending" {
		t.Errorf("Create order status = %q, want %q", order.Status, "pending")
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
		Status: omittable.Set("shipped"),
		Total:  omittable.Set(109.50),
	})
	if err != nil {
		t.Fatalf("Update order: %v", err)
	}
	if updated.Status != "shipped" {
		t.Errorf("Update order status = %q, want %q", updated.Status, "shipped")
	}

	// HardDelete
	if err := client.Orders().HardDelete(ctx, order.ID); err != nil {
		t.Fatalf("HardDelete order: %v", err)
	}
}

// --- CRUD: Order Items ---

func TestOrderItemCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "oi-crud@example.com", Name: "OrderItemCRUD",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "OICRUDCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "OIProduct", Price: 15.00, SKU: "OI-001",
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID, Total: omittable.Set(15.00),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	item, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID: order.ID, ProductID: product.ID, Quantity: 3, UnitPrice: 15.00,
	})
	if err != nil {
		t.Fatalf("Create order item: %v", err)
	}
	if item.ID == 0 {
		t.Fatal("Create order item: expected non-zero ID")
	}
	if item.Quantity != 3 {
		t.Errorf("Create order item quantity = %d, want 3", item.Quantity)
	}

	// Get
	got, err := client.OrderItems().Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get order item: %v", err)
	}
	if got.OrderID != order.ID {
		t.Errorf("Get order item order_id = %d, want %d", got.OrderID, order.ID)
	}

	// Update
	updated, err := client.OrderItems().Update(ctx, item.ID, &models.UpdateOrderItemInput{
		Quantity:  omittable.Set[int64](5),
		UnitPrice: omittable.Set(12.50),
	})
	if err != nil {
		t.Fatalf("Update order item: %v", err)
	}
	if updated.Quantity != 5 {
		t.Errorf("Update order item quantity = %d, want 5", updated.Quantity)
	}

	// HardDelete
	if err := client.OrderItems().HardDelete(ctx, item.ID); err != nil {
		t.Fatalf("HardDelete order item: %v", err)
	}
}

// --- BOOLEAN stored as INTEGER round-trip ---

func TestBooleanIntegerRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Create with is_active=true (stored as INTEGER 1)
	userTrue, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "bool-true@example.com", Name: "BoolTrue",
		IsActive: omittable.Set(true),
	})
	if err != nil {
		t.Fatalf("Create user (true): %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userTrue.ID) })

	got, err := client.Users().Get(ctx, userTrue.ID)
	if err != nil {
		t.Fatalf("Get user (true): %v", err)
	}
	if !got.IsActive {
		t.Error("IsActive should be true after storing 1")
	}

	// Create with is_active=false (stored as INTEGER 0)
	userFalse, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "bool-false@example.com", Name: "BoolFalse",
		IsActive: omittable.Set(false),
	})
	if err != nil {
		t.Fatalf("Create user (false): %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, userFalse.ID) })

	got, err = client.Users().Get(ctx, userFalse.ID)
	if err != nil {
		t.Fatalf("Get user (false): %v", err)
	}
	if got.IsActive {
		t.Error("IsActive should be false after storing 0")
	}

	// Verify boolean filter works
	active := true
	trueID := userTrue.ID
	falseID := userFalse.ID
	users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID:       &comparator.Number[int64]{In: []int64{trueID, falseID}},
			IsActive: &comparator.Bool{Eq: &active},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("Filter is_active=true: got %d users, want 1", len(users))
	}
	if len(users) == 1 && users[0].ID != trueID {
		t.Errorf("Filter is_active=true: got user ID %d, want %d", users[0].ID, trueID)
	}

	// Also verify products.in_stock (another BOOLEAN column)
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "BoolCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "InStockProduct", Price: 10.00, SKU: "BOOL-001",
		InStock: omittable.Set(false),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	gotProduct, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if gotProduct.InStock {
		t.Error("InStock should be false")
	}
}

// --- RETURNING clause behavior ---

func TestReturningClause(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Create: RETURNING should provide the auto-generated ID and default values
	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "returning@example.com", Name: "ReturningUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	if user.ID == 0 {
		t.Error("RETURNING should provide auto-generated ID")
	}
	if user.Role != "viewer" {
		t.Errorf("RETURNING should provide default role = %q, want %q", user.Role, "viewer")
	}
	if user.CreatedAt.IsZero() {
		t.Error("RETURNING should provide default created_at")
	}

	// Update: RETURNING should provide updated values
	updated, err := client.Users().Update(ctx, user.ID, &models.UpdateUserInput{
		Name: omittable.Set("ReturningUpdated"),
	})
	if err != nil {
		t.Fatalf("Update user: %v", err)
	}
	if updated.Name != "ReturningUpdated" {
		t.Errorf("RETURNING after update: name = %q, want %q", updated.Name, "ReturningUpdated")
	}
	if updated.ID != user.ID {
		t.Errorf("RETURNING after update: ID = %d, want %d", updated.ID, user.ID)
	}

	// UpdateWhere: RETURNING should provide affected row IDs for re-fetch
	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "Returning-Cat1"})
	if err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	c2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "Returning-Cat2"})
	if err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, c1.ID)
		_ = client.Categories().HardDelete(ctx, c2.ID)
	})

	updatedCats, err := client.Categories().UpdateWhere(
		ctx,
		&models.CategoryFilter{ID: &comparator.Number[int64]{In: []int64{c1.ID, c2.ID}}},
		&models.UpdateCategoryInput{Description: omittable.Set(new(string("batch-updated")))},
	)
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if len(updatedCats) != 2 {
		t.Errorf("UpdateWhere RETURNING: got %d results, want 2", len(updatedCats))
	}
}

// --- ON CONFLICT Upsert ---

func TestUpsert(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpsertCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Initial insert via upsert (ON CONFLICT on SKU)
	p1, err := client.Products().Upsert(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "UpsertProduct", Price: 10.00,
		SKU: "UPSERT-001",
	}, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	if p1.Title != "UpsertProduct" {
		t.Errorf("Upsert insert title = %q, want %q", p1.Title, "UpsertProduct")
	}
	if p1.Price != 10.00 {
		t.Errorf("Upsert insert price = %v, want 10.00", p1.Price)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	// Upsert same SKU with updated values (ON CONFLICT DO UPDATE)
	p2, err := client.Products().Upsert(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "UpdatedUpsert", Price: 25.00,
		SKU: "UPSERT-001",
	}, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}
	if p2.Title != "UpdatedUpsert" {
		t.Errorf("Upsert update title = %q, want %q", p2.Title, "UpdatedUpsert")
	}
	if p2.Price != 25.00 {
		t.Errorf("Upsert update price = %v, want 25.00", p2.Price)
	}
	// Should be same row (same ID) since SKU conflict triggered update
	if p2.ID != p1.ID {
		t.Errorf("Upsert should update existing row: p2.ID=%d, p1.ID=%d", p2.ID, p1.ID)
	}

	// Verify only one product with that SKU
	sku := "UPSERT-001"
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{SKU: &comparator.String{Eq: &sku}},
	})
	if err != nil {
		t.Fatalf("GetMany by sku: %v", err)
	}
	if len(products) != 1 {
		t.Errorf("Expected 1 product with sku %q, got %d", sku, len(products))
	}
}

// --- Filtering ---

func TestFilterComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	u1, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter1@example.com", Name: "Filter1", Balance: omittable.Set(100.00),
		Role: omittable.Set("admin"),
	})
	u2, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter2@example.com", Name: "Filter2", Balance: omittable.Set(200.00),
		Role: omittable.Set("viewer"),
	})
	u3, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter3@example.com", Name: "AnotherFilter", Balance: omittable.Set(50.00),
		Role: omittable.Set("editor"),
	})
	t.Cleanup(func() {
		_ = client.Users().HardDelete(ctx, u1.ID)
		_ = client.Users().HardDelete(ctx, u2.ID)
		_ = client.Users().HardDelete(ctx, u3.ID)
	})

	// String equality
	name := "Filter1"
	users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{Name: &comparator.String{Eq: &name}},
	})
	if err != nil {
		t.Fatalf("Filter by name: %v", err)
	}
	if len(users) != 1 || users[0].ID != u1.ID {
		t.Errorf("Filter name=Filter1: got %d users", len(users))
	}

	// String contains (LIKE)
	contains := "Filter"
	users, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Name: &comparator.String{Contains: &contains},
			ID:   &comparator.Number[int64]{In: []int64{u1.ID, u2.ID, u3.ID}},
		},
	})
	if err != nil {
		t.Fatalf("Filter by name contains: %v", err)
	}
	if len(users) < 3 {
		t.Errorf("Filter name contains 'Filter': got %d users, want >= 3", len(users))
	}

	// Numeric filter — balance >= 100
	minBalance := 100.00
	users, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID:      &comparator.Number[int64]{In: []int64{u1.ID, u2.ID, u3.ID}},
			Balance: &comparator.Number[float64]{Gte: &minBalance},
		},
	})
	if err != nil {
		t.Fatalf("Filter by balance gte: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("Filter balance >= 100: got %d users, want 2", len(users))
	}

	// Boolean filter — is_active (defaults to true/1)
	active := true
	users, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID:       &comparator.Number[int64]{In: []int64{u1.ID, u2.ID, u3.ID}},
			IsActive: &comparator.Bool{Eq: &active},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("Filter is_active=true: got %d users, want 3", len(users))
	}
}

// --- BLOB column round-trip ---

func TestBlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	avatarData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:  "blob@example.com",
		Name:   "BlobUser",
		Avatar: omittable.Set(avatarData),
	})
	if err != nil {
		t.Fatalf("Create user with blob: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if diff := cmp.Diff(avatarData, got.Avatar); diff != "" {
		t.Errorf("Avatar round-trip mismatch (-want +got):\n%s", diff)
	}
}
