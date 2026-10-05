package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- Relationship Loading ---

func TestRelationshipO2O(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "o2o@example.com",
		Name:  "O2OUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	bio := "Developer"
	profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID: user.ID,
		Bio:    omittable.Set(&bio),
	})
	if err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, profile.ID) })

	// Load user with profile (O2O via LEFT JOIN)
	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID: &comparator.ID{Eq: new(user.ID.String())},
		},
	}, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
		o.FieldOptions = &models.PublicUserFieldOptions{
			ID:   true,
			Name: true,
			Profile: &models.ProfileFieldOptions{
				ID:  true,
				Bio: true,
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
		t.Errorf("Profile.ID = %q, want %q", users[0].Profile.ID, profile.ID)
	}
}

func TestRelationshipO2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "O2MCategory",
	})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Product1",
		Price:      10.99,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Product2",
		Price:      20.50,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	// Load category with products (O2M)
	categories, err := client.Categories().GetMany(ctx, &models.GetCategoriesInput{
		Filter: &models.CategoryFilter{
			ID: &comparator.ID{Eq: new(cat.ID.String())},
		},
	}, func(o *models.CallOptions[models.CategoryFieldOptions]) {
		o.FieldOptions = &models.CategoryFieldOptions{
			ID:   true,
			Name: true,
			Products: &models.ProductRelationshipOptions{
				FieldOptions: &models.ProductFieldOptions{
					ID:    true,
					Name:  true,
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

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "o2m-orders@example.com",
		Name:  "O2MOrdersUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	o1, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Status: omittable.Set(models.OrderStatusPending),
		Total:  omittable.Set(50.00),
	})
	if err != nil {
		t.Fatalf("Create order 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, o1.ID) })

	o2, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Status: omittable.Set(models.OrderStatusShipped),
		Total:  omittable.Set(75.00),
	})
	if err != nil {
		t.Fatalf("Create order 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, o2.ID) })

	// Load user with orders (O2M)
	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID: &comparator.ID{Eq: new(user.ID.String())},
		},
	}, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
		o.FieldOptions = &models.PublicUserFieldOptions{
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

func TestRelationshipO2M_OrdersOrderItems(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "o2m-items@example.com",
		Name:  "O2MItemsUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "O2MItemsCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "O2MItemsProduct",
		Price:      20.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID,
		Total:  omittable.Set(60.00),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	item1, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID:   order.ID,
		ProductID: product.ID,
		Quantity:  omittable.Set[int32](2),
		UnitPrice: 20.00,
	})
	if err != nil {
		t.Fatalf("Create order item 1: %v", err)
	}
	t.Cleanup(func() { _ = client.OrderItems().HardDelete(ctx, item1.ID) })

	item2, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID:   order.ID,
		ProductID: product.ID,
		Quantity:  omittable.Set[int32](1),
		UnitPrice: 20.00,
	})
	if err != nil {
		t.Fatalf("Create order item 2: %v", err)
	}
	t.Cleanup(func() { _ = client.OrderItems().HardDelete(ctx, item2.ID) })

	// Load order with order_items (O2M)
	orders, err := client.Orders().GetMany(ctx, &models.GetOrdersInput{
		Filter: &models.OrderFilter{
			ID: &comparator.ID{Eq: new(order.ID.String())},
		},
	}, func(o *models.CallOptions[models.OrderFieldOptions]) {
		o.FieldOptions = &models.OrderFieldOptions{
			ID:    true,
			Total: true,
			OrderItems: &models.OrderItemRelationshipOptions{
				FieldOptions: &models.OrderItemFieldOptions{
					ID:        true,
					Quantity:  true,
					UnitPrice: true,
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with O2M orders→order_items: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("GetMany returned %d orders, want 1", len(orders))
	}
	if len(orders[0].OrderItems) != 2 {
		t.Errorf("Order.OrderItems count = %d, want 2", len(orders[0].OrderItems))
	}
}

func TestRelationshipM2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "m2m@example.com",
		Name:  "M2MUser",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

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

	// Create junction entries
	_, err = client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID:     user.ID,
		CategoryID: cat1.ID,
	})
	if err != nil {
		t.Fatalf("Create user_category 1: %v", err)
	}
	_, err = client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID:     user.ID,
		CategoryID: cat2.ID,
	})
	if err != nil {
		t.Fatalf("Create user_category 2: %v", err)
	}

	// Load user with categories (M2M)
	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{ID: &comparator.ID{Eq: new(user.ID.String())}},
	}, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
		o.FieldOptions = &models.PublicUserFieldOptions{
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
