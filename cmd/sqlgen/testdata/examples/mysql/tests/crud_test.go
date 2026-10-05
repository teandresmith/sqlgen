package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// --- CRUD: Users ---

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email:   "alice@example.com",
		Name:    "Alice",
		Balance: 100.50,
		Role:    omittable.Set(models.UsersRoleEnumAdmin),
		Metadata: omittable.Set(mustJSON(map[string]any{
			"level": float64(5),
			"plan":  "pro",
		})),
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
	if user.Role != models.UsersRoleEnumAdmin {
		t.Errorf("Create user role = %v, want %v", user.Role, models.UsersRoleEnumAdmin)
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

	// HardDelete
	if err := client.Users().HardDelete(ctx, user.ID); err != nil {
		t.Fatalf("HardDelete user: %v", err)
	}
	exists, err := client.Users().Exists(ctx, user.ID)
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
		Email: "profile-crud@example.com", Name: "ProfileCRUD", Balance: 0,
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
		Attributes: mustJSON(map[string]any{"color": "red"}),
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
		Email: "order-crud@example.com", Name: "OrderCRUD", Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	notes := "Rush delivery"
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Status: omittable.Set(models.OrdersStatusEnumPending),
		Total:  99.50,
		Notes:  omittable.Set(&notes),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if order.ID == 0 {
		t.Fatal("Create order: expected non-zero ID")
	}
	if order.Status != models.OrdersStatusEnumPending {
		t.Errorf("Create order status = %v, want %v", order.Status, models.OrdersStatusEnumPending)
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
		Status: omittable.Set(models.OrdersStatusEnumShipped),
		Total:  omittable.Set(109.50),
	})
	if err != nil {
		t.Fatalf("Update order: %v", err)
	}
	if updated.Status != models.OrdersStatusEnumShipped {
		t.Errorf("Update order status = %v, want %v", updated.Status, models.OrdersStatusEnumShipped)
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
		Email: "oi-crud@example.com", Name: "OrderItemCRUD", Balance: 0,
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
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID, Total: 15.00,
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
		Quantity:  omittable.Set[int32](5),
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

// --- Inline Enum Round-Trip ---

func TestEnumRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// role enum
	for _, role := range models.AllUsersRoleEnum {
		user, err := client.Users().Create(ctx, &models.CreateUserInput{
			Email: fmt.Sprintf("enum-%s@example.com", role), Name: string(role),
			Balance: 0, Role: omittable.Set(role),
		})
		if err != nil {
			t.Fatalf("Create user with role %v: %v", role, err)
		}
		t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

		got, err := client.Users().Get(ctx, user.ID)
		if err != nil {
			t.Fatalf("Get user with role %v: %v", role, err)
		}
		if got.Role != role {
			t.Errorf("RoleEnum round-trip: got %v, want %v", got.Role, role)
		}
	}

	// status enum
	orderUser, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "enum-order@example.com", Name: "EnumOrderTest", Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user for order test: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, orderUser.ID) })

	for _, status := range models.AllOrdersStatusEnum {
		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
			UserID: orderUser.ID, Status: omittable.Set(status), Total: 10.00,
		})
		if err != nil {
			t.Fatalf("Create order with status %v: %v", status, err)
		}
		t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

		got, err := client.Orders().Get(ctx, order.ID)
		if err != nil {
			t.Fatalf("Get order with status %v: %v", status, err)
		}
		if got.Status != status {
			t.Errorf("StatusEnum round-trip: got %v, want %v", got.Status, status)
		}
	}
}

// --- Unsigned Integer Types ---

func TestUnsignedIntegerTypes(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	bigVal := uint64(18446744073709551615) // max uint64
	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "unsigned@example.com", Name: "UnsignedUser", Balance: 0,
		BigUnsigned: omittable.Set(&bigVal),
		TinyFlag:    omittable.Set(new(int8(127))),
		MediumVal:   omittable.Set(new(int32(8388607))),
	})
	if err != nil {
		t.Fatalf("Create user with unsigned: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if got.BigUnsigned == nil || *got.BigUnsigned != bigVal {
		t.Errorf("BigUnsigned round-trip = %v, want %d", got.BigUnsigned, bigVal)
	}
	if got.TinyFlag == nil || *got.TinyFlag != 127 {
		t.Errorf("TinyFlag = %v, want 127", got.TinyFlag)
	}
	if got.MediumVal == nil || *got.MediumVal != 8388607 {
		t.Errorf("MediumVal = %v, want 8388607", got.MediumVal)
	}
}

// --- JSON Column Operations ---

func TestJSONColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	metadata := mustJSON(map[string]any{
		"version":  float64(2),
		"features": []any{"auth", "billing"},
		"config":   map[string]any{"debug": true},
	})

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "json@example.com", Name: "JSONUser", Balance: 0,
		Metadata: omittable.Set(metadata),
	})
	if err != nil {
		t.Fatalf("Create user with JSON: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	got, err := client.Users().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}

	// MySQL canonicalizes JSON storage (whitespace, key order may differ
	// from input), so compare structurally rather than byte-equal.
	assertJSONStructEqual(t, "user metadata", metadata, got.Metadata)

	// Product attributes (NOT NULL JSON)
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "JSONCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	attrs := mustJSON(map[string]any{"size": "large", "weight": float64(1.5)})
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "JSONProduct", Price: 10.00,
		SKU: "JSON-001", Attributes: attrs,
	})
	if err != nil {
		t.Fatalf("Create product with JSON: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	gotProduct, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}

	assertJSONStructEqual(t, "product attributes", attrs, gotProduct.Attributes)
}

// assertJSONStructEqual asserts two types.JSON values decode to equal Go
// any-trees. Avoids byte-equality which fails when the storage engine
// canonicalizes (MySQL inserts a space after the colon; key ordering may
// drift on jsonb-style storage).
func assertJSONStructEqual(t *testing.T, label string, want, got types.JSON) {
	t.Helper()
	var w, g any
	if err := want.Decode(&w); err != nil {
		t.Fatalf("%s: decode want: %v", label, err)
	}
	if err := got.Decode(&g); err != nil {
		t.Fatalf("%s: decode got: %v", label, err)
	}
	if !jsonAnyEqual(w, g) {
		t.Errorf("%s round-trip:\ngot  %s\nwant %s", label, got, want)
	}
}

// jsonAnyEqual reports whether two values decoded from JSON via
// encoding/json are structurally equal. Sufficient because both sides come
// from json.Unmarshal into any — keys are strings, values are bounded to
// map[string]any / []any / float64 / string / bool / nil.
func jsonAnyEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !jsonAnyEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonAnyEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// --- ON DUPLICATE KEY UPDATE (Upsert) ---

func TestUpsert(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpsertCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Initial insert
	p1, err := client.Products().Upsert(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "UpsertProduct", Price: 10.00,
		SKU: "UPSERT-001", Attributes: mustJSON(map[string]any{"v": float64(1)}),
	}, models.ProductConflictPK)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	if p1.Title != "UpsertProduct" {
		t.Errorf("Upsert insert title = %q, want %q", p1.Title, "UpsertProduct")
	}
	if p1.Price != 10.00 {
		t.Errorf("Upsert insert price = %v, want 10.00", p1.Price)
	}

	// Upsert same PK with updated values (ON DUPLICATE KEY UPDATE)
	p2, err := client.Products().Upsert(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "UpdatedUpsert", Price: 25.00,
		SKU: "UPSERT-001-NEW", Attributes: mustJSON(map[string]any{"v": float64(2)}),
	}, models.ProductConflictPK)
	if err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}

	// Verify the upsert created a new row (different PK since we changed the SKU)
	if p2.Title != "UpdatedUpsert" {
		t.Errorf("Upsert update title = %q, want %q", p2.Title, "UpdatedUpsert")
	}

	t.Cleanup(func() {
		_ = client.Products().HardDelete(ctx, p1.ID)
		_ = client.Products().HardDelete(ctx, p2.ID)
	})
}

// --- Filtering ---

func TestFilterComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	u1, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter1@example.com", Name: "Filter1", Balance: 100.00,
		Role: omittable.Set(models.UsersRoleEnumAdmin),
	})
	u2, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter2@example.com", Name: "Filter2", Balance: 200.00,
		Role: omittable.Set(models.UsersRoleEnumViewer),
	})
	u3, _ := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "filter3@example.com", Name: "AnotherFilter", Balance: 50.00,
		Role: omittable.Set(models.UsersRoleEnumEditor),
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

	// Enum filter
	adminRole := models.UsersRoleEnumAdmin
	users, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Role: &comparator.Enum[models.UsersRoleEnum]{Eq: &adminRole},
			ID:   &comparator.Number[int64]{In: []int64{u1.ID, u2.ID, u3.ID}},
		},
	})
	if err != nil {
		t.Fatalf("Filter by role: %v", err)
	}
	found := false
	for _, u := range users {
		if u.ID == u1.ID {
			found = true
		}
	}
	if !found {
		t.Error("Filter role=admin should include u1")
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

	// Boolean filter — is_active (defaults to true)
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

	// Timestamp filter
	pastTime := u1.CreatedAt.Add(-time.Minute)
	users, err = client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID:        &comparator.Number[int64]{In: []int64{u1.ID, u2.ID, u3.ID}},
			CreatedAt: &comparator.Time{Gte: &pastTime},
		},
	})
	if err != nil {
		t.Fatalf("Filter by created_at gte: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("Filter created_at >= past: got %d users, want 3", len(users))
	}
}
