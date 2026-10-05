package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestRelationshipO2O(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "o2o@example.com", Name: "O2OUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	github := "o2ouser"
	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID:       user.ID,
		GithubHandle: omittable.Set(&github),
	})
	if err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, profile.ID) })

	id := user.ID
	users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID: &comparator.Number[int64]{Eq: &id},
		},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:   true,
			Name: true,
			Profile: &models.ProfileFieldOptions{
				ID:           true,
				GithubHandle: true,
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with O2O: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(users))
	}
	if users[0].Profile == nil {
		t.Fatal("User.Profile is nil, expected profile to be loaded")
	}
	if users[0].Profile.ID != profile.ID {
		t.Errorf("Profile.ID = %d, want %d", users[0].Profile.ID, profile.ID)
	}
}

func TestRelationshipO2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "O2MCategory"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "Product1", Price: 10.99, SKU: "O2M-001",
	})
	if err != nil {
		t.Fatalf("Create product 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "Product2", Price: 20.50, SKU: "O2M-002",
	})
	if err != nil {
		t.Fatalf("Create product 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	catID := cat.ID
	categories, err := client.Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{
			ID: &comparator.Number[int64]{Eq: &catID},
		},
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			ID:   true,
			Name: true,
			Products: &models.ProductRelationshipOptions{
				FieldOptions: &models.ProductFieldOptions{
					ID:    true,
					Title: true,
					Price: true,
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with O2M: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("GetMany returned %d categories, want 1", len(categories))
	}
	if len(categories[0].Products) != 2 {
		t.Errorf("Category.Products count = %d, want 2", len(categories[0].Products))
	}
}

func TestRelationshipO2M_UsersOrders(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "o2m-orders@example.com", Name: "O2MOrdersUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	o1, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID, Status: omittable.Set("pending"), Total: omittable.Set(50.00),
	})
	if err != nil {
		t.Fatalf("Create order 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, o1.ID) })

	o2, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID, Status: omittable.Set("shipped"), Total: omittable.Set(75.00),
	})
	if err != nil {
		t.Fatalf("Create order 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, o2.ID) })

	id := user.ID
	users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			ID: &comparator.Number[int64]{Eq: &id},
		},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:   true,
			Name: true,
			Orders: &models.OrderRelationshipOptions{
				FieldOptions: &models.OrderFieldOptions{
					ID:     true,
					Status: true,
					Total:  true,
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with O2M users→orders: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(users))
	}
	if len(users[0].Orders) != 2 {
		t.Errorf("User.Orders count = %d, want 2", len(users[0].Orders))
	}
}

func TestRelationshipM2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: "m2m@example.com", Name: "M2MUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	cat1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "M2MCat1"})
	if err != nil {
		t.Fatalf("Create cat1: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat1.ID) })

	cat2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "M2MCat2"})
	if err != nil {
		t.Fatalf("Create cat2: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat2.ID) })

	_, err = client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID: user.ID, CategoryID: cat1.ID,
	})
	if err != nil {
		t.Fatalf("Create user_category 1: %v", err)
	}
	_, err = client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID: user.ID, CategoryID: cat2.ID,
	})
	if err != nil {
		t.Fatalf("Create user_category 2: %v", err)
	}

	id := user.ID
	users, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{ID: &comparator.Number[int64]{Eq: &id}},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:   true,
			Name: true,
			Categories: &models.CategoryRelationshipOptions{
				FieldOptions: &models.CategoryFieldOptions{
					ID:   true,
					Name: true,
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with M2M: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(users))
	}
	if len(users[0].Categories) != 2 {
		t.Errorf("User.Categories count = %d, want 2", len(users[0].Categories))
	}
}
