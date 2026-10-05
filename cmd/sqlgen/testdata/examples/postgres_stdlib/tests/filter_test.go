package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Filtering ---

func TestFilterComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	u1, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter1@example.com", Name: "Filter1", Role: omittable.Set(models.UserRoleAdmin),
	})
	u2, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter2@example.com", Name: "Filter2", Role: omittable.Set(models.UserRoleViewer),
	})
	u3, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter3@example.com", Name: "AnotherFilter", Role: omittable.Set(models.UserRoleEditor),
	})
	t.Cleanup(func() {
		_ = client.PublicUsers().HardDelete(ctx, u1.ID)
		_ = client.PublicUsers().HardDelete(ctx, u2.ID)
		_ = client.PublicUsers().HardDelete(ctx, u3.ID)
	})

	// String equality
	name := "Filter1"
	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Name: &comparator.String{Eq: &name},
		},
	})
	if err != nil {
		t.Fatalf("Filter by name: %v", err)
	}
	if len(users) != 1 || users[0].ID != u1.ID {
		t.Errorf("Filter name=Filter1: got %d users", len(users))
	}

	// Enum filter
	adminRole := models.UserRoleAdmin
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Role: &comparator.Enum[models.UserRole]{Eq: &adminRole},
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
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Name: &comparator.String{Contains: &contains},
		},
	})
	if err != nil {
		t.Fatalf("Filter by name contains: %v", err)
	}
	if len(users) < 3 {
		t.Errorf("Filter name contains 'Filter': got %d users, want >= 3", len(users))
	}

	// Timestamptz filter (Gte — users created after a past date)
	pastTime := u1.CreatedAt.Add(-time.Minute)
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID:        &comparator.ID{In: []string{u1.ID.String(), u2.ID.String(), u3.ID.String()}},
			CreatedAt: &comparator.Time{Gte: &pastTime},
		},
	})
	if err != nil {
		t.Fatalf("Filter by created_at gte: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("Filter created_at >= past: got %d users, want 3", len(users))
	}

	// Timestamptz filter (Lt — no users created before a past date)
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID:        &comparator.ID{In: []string{u1.ID.String(), u2.ID.String(), u3.ID.String()}},
			CreatedAt: &comparator.Time{Lt: &pastTime},
		},
	})
	if err != nil {
		t.Fatalf("Filter by created_at lt: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("Filter created_at < past: got %d users, want 0", len(users))
	}
}

func TestFilterNumericAndIntegerComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "FilterNumCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Cheap",
		Price:      10.00,
		Quantity:   omittable.Set[int32](5),
		IsActive:   omittable.Set(true),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Medium",
		Price:      50.00,
		Quantity:   omittable.Set[int32](10),
		IsActive:   omittable.Set(true),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	p3, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Expensive",
		Price:      200.00,
		Quantity:   omittable.Set[int32](2),
		IsActive:   omittable.Set(false),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 3: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p3.ID) })

	productIDs := []uuid.UUID{p1.ID, p2.ID, p3.ID}

	// Numeric (float64) — Gte filter on price
	minPrice := 50.00
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(productIDs)},
			Price: &comparator.Number[float64]{Gte: &minPrice},
		},
	})
	if err != nil {
		t.Fatalf("Filter by price gte: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter price >= 50: got %d products, want 2", len(products))
	}

	// Numeric (float64) — Lt filter on price
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(productIDs)},
			Price: &comparator.Number[float64]{Lt: &minPrice},
		},
	})
	if err != nil {
		t.Fatalf("Filter by price lt: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1.ID {
		t.Errorf("Filter price < 50: got %d products, want 1 (Cheap)", len(products))
	}

	// Integer (int32) — Gt filter on quantity
	minQty := int32(4)
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			Quantity: &comparator.Number[int32]{Gt: &minQty},
		},
	})
	if err != nil {
		t.Fatalf("Filter by quantity gt: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter quantity > 4: got %d products, want 2", len(products))
	}

	// Integer (int32) — Eq filter on quantity
	exactQty := int32(10)
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			Quantity: &comparator.Number[int32]{Eq: &exactQty},
		},
	})
	if err != nil {
		t.Fatalf("Filter by quantity eq: %v", err)
	}
	if len(products) != 1 || products[0].ID != p2.ID {
		t.Errorf("Filter quantity == 10: got %d products, want 1 (Medium)", len(products))
	}

	// Boolean — Eq filter on is_active (true)
	active := true
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			IsActive: &comparator.Bool{Eq: &active},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active true: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter is_active=true: got %d products, want 2", len(products))
	}

	// Boolean — Eq filter on is_active (false)
	inactive := false
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			IsActive: &comparator.Bool{Eq: &inactive},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active false: %v", err)
	}
	if len(products) != 1 || products[0].ID != p3.ID {
		t.Errorf("Filter is_active=false: got %d products, want 1 (Expensive)", len(products))
	}
}
