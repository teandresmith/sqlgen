package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- Enum Round-Trip ---

func TestEnumRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	for _, role := range models.AllUserRole {
		user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
			Email: fmt.Sprintf("enum-%s@example.com", role),
			Name:  string(role),
			Role:  omittable.Set(role),
		})
		if err != nil {
			t.Fatalf("Create user with role %v: %v", role, err)
		}
		t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

		got, err := client.PublicUsers().Get(ctx, user.ID)
		if err != nil {
			t.Fatalf("Get user with role %v: %v", role, err)
		}
		if got.Role != role {
			t.Errorf("UserRole round-trip: got %v, want %v", got.Role, role)
		}
	}

	// order_status enum
	orderUser, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "enum-order@example.com",
		Name:  "EnumOrderTest",
	})
	if err != nil {
		t.Fatalf("Create user for order test: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, orderUser.ID) })

	for _, status := range models.AllOrderStatus {
		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
			UserID: orderUser.ID,
			Status: omittable.Set(status),
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
			t.Errorf("OrderStatus round-trip: got %v, want %v", got.Status, status)
		}
	}
}

// --- Array Columns ---

func TestArrayColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// text[] on users.tags
	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "array@example.com",
		Name:  "ArrayUser",
		Tags:  omittable.Set([]string{"tag1", "tag2", "tag3"}),
	})
	if err != nil {
		t.Fatalf("Create user with tags: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	got, err := client.PublicUsers().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if diff := cmp.Diff([]string{"tag1", "tag2", "tag3"}, got.Tags); diff != "" {
		t.Errorf("text[] round-trip mismatch (-want +got):\n%s", diff)
	}

	// integer[] on profiles.scores
	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID: user.ID,
		Scores: omittable.Set([]int32{100, 95, 88}),
	})
	if err != nil {
		t.Fatalf("Create profile with scores: %v", err)
	}
	t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, profile.ID) })

	gotProfile, err := client.Profiles().Get(ctx, profile.ID)
	if err != nil {
		t.Fatalf("Get profile: %v", err)
	}
	if diff := cmp.Diff([]int32{100, 95, 88}, gotProfile.Scores); diff != "" {
		t.Errorf("integer[] round-trip mismatch (-want +got):\n%s", diff)
	}
}

// --- JSONB Columns ---

func TestJSONBColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	metadata := mustJSON(map[string]any{
		"version":  float64(2),
		"features": []any{"auth", "billing"},
		"config":   map[string]any{"debug": true},
	})

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email:    "jsonb@example.com",
		Name:     "JSONBUser",
		Metadata: omittable.Set(metadata),
	})
	if err != nil {
		t.Fatalf("Create user with JSONB: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	got, err := client.PublicUsers().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}

	assertJSONStructEqual(t, "JSONB metadata", metadata, got.Metadata)
}

// --- Domain Type Columns ---

func TestDomainTypeColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// email domain (behaves as text/string)
	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "domain@example.com",
		Name:  "DomainUser",
	})
	if err != nil {
		t.Fatalf("Create user with email domain: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	got, err := client.PublicUsers().Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("Get user: %v", err)
	}
	if got.Email != "domain@example.com" {
		t.Errorf("email domain round-trip = %q, want %q", got.Email, "domain@example.com")
	}

	// positive_int domain (behaves as integer/int32)
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DomainCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "DomainProduct",
		Price:      9.99,
		Quantity:   omittable.Set[int32](42),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product with positive_int: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	gotProduct, err := client.Products().Get(ctx, product.ID)
	if err != nil {
		t.Fatalf("Get product: %v", err)
	}
	if gotProduct.Quantity != 42 {
		t.Errorf("positive_int round-trip = %d, want 42", gotProduct.Quantity)
	}
}

// --- Composite Type Columns ---

func TestCompositeTypeColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "composite@example.com",
		Name:  "CompositeUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	addr := &models.Address{
		Street:  "123 Main St",
		City:    "Springfield",
		State:   "IL",
		Zip:     "62701",
		Country: "US",
	}

	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID:  user.ID,
		Address: omittable.Set(addr),
	})
	if err != nil {
		t.Fatalf("Create profile with address: %v", err)
	}
	t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, profile.ID) })

	gotProfile, err := client.Profiles().Get(ctx, profile.ID)
	if err != nil {
		t.Fatalf("Get profile: %v", err)
	}
	if gotProfile.Address == nil {
		t.Fatal("Address is nil after round-trip")
	}
	if diff := cmp.Diff(addr, gotProfile.Address); diff != "" {
		t.Errorf("Address round-trip mismatch (-want +got):\n%s", diff)
	}
}
