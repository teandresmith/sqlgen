package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Get by 2-column composite PK (user_categories) ---

func TestGetBy2ColumnCompositePK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "cpk2-get@example.com",
		Name:  "CPK2Get",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPK2GetCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	created, err := client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID:     user.ID,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create user_category: %v", err)
	}
	t.Cleanup(func() {
		_ = client.UserCategories().HardDelete(ctx, models.UserCategoryPK{
			UserID: user.ID, CategoryID: cat.ID,
		})
	})

	got, err := client.UserCategories().Get(ctx, models.UserCategoryPK{
		UserID:     created.UserID,
		CategoryID: created.CategoryID,
	})
	if err != nil {
		t.Fatalf("Get by 2-column composite PK: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("Get user_id = %q, want %q", got.UserID, user.ID)
	}
	if got.CategoryID != cat.ID {
		t.Errorf("Get category_id = %q, want %q", got.CategoryID, cat.ID)
	}
}

// --- Get by 3-column composite PK (product_tag_labels) ---

func TestGetBy3ColumnCompositePK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPK3GetCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "CPK3Product",
		Price:      10.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	desc := "test description"
	created, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID:   product.ID,
		TagName:     "color",
		Label:       "red",
		Description: omittable.Set(&desc),
	})
	if err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}
	t.Cleanup(func() {
		_ = client.ProductTagLabels().HardDelete(ctx, models.ProductTagLabelPK{
			ProductID: product.ID, TagName: "color", Label: "red",
		})
	})

	got, err := client.ProductTagLabels().Get(ctx, models.ProductTagLabelPK{
		ProductID: created.ProductID,
		TagName:   created.TagName,
		Label:     created.Label,
	})
	if err != nil {
		t.Fatalf("Get by 3-column composite PK: %v", err)
	}
	if got.ProductID != product.ID {
		t.Errorf("Get product_id = %q, want %q", got.ProductID, product.ID)
	}
	if got.TagName != "color" {
		t.Errorf("Get tag_name = %q, want %q", got.TagName, "color")
	}
	if got.Label != "red" {
		t.Errorf("Get label = %q, want %q", got.Label, "red")
	}
	if got.Description == nil || *got.Description != "test description" {
		t.Errorf("Get description = %v, want %q", got.Description, "test description")
	}
}

// --- Update by composite PK ---

func TestUpdateByCompositePK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPKUpdateCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "CPKUpdateProduct",
		Price:      20.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	desc := "original"
	_, err = client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID:   product.ID,
		TagName:     "size",
		Label:       "large",
		Description: omittable.Set(&desc),
	})
	if err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}
	pk := models.ProductTagLabelPK{
		ProductID: product.ID, TagName: "size", Label: "large",
	}
	t.Cleanup(func() { _ = client.ProductTagLabels().HardDelete(ctx, pk) })

	newDesc := "updated description"
	updated, err := client.ProductTagLabels().Update(ctx, pk, &models.UpdateProductTagLabelInput{
		Description: omittable.Set(&newDesc),
	})
	if err != nil {
		t.Fatalf("Update by composite PK: %v", err)
	}
	if updated.Description == nil || *updated.Description != "updated description" {
		t.Errorf("Update description = %v, want %q", updated.Description, "updated description")
	}
	// PK fields unchanged
	if updated.ProductID != product.ID {
		t.Errorf("Update product_id = %q, want %q", updated.ProductID, product.ID)
	}
	if updated.TagName != "size" {
		t.Errorf("Update tag_name = %q, want %q", updated.TagName, "size")
	}
	if updated.Label != "large" {
		t.Errorf("Update label = %q, want %q", updated.Label, "large")
	}
}

// --- HardDelete by composite PK ---

func TestHardDeleteByCompositePK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPKDeleteCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "CPKDeleteProduct",
		Price:      30.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	_, err = client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID: product.ID,
		TagName:   "material",
		Label:     "cotton",
	})
	if err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}

	pk := models.ProductTagLabelPK{
		ProductID: product.ID, TagName: "material", Label: "cotton",
	}

	// Verify it exists
	exists, err := client.ProductTagLabels().Exists(ctx, pk)
	if err != nil {
		t.Fatalf("Exists before delete: %v", err)
	}
	if !exists {
		t.Fatal("product_tag_label should exist before HardDelete")
	}

	// Delete
	if err := client.ProductTagLabels().HardDelete(ctx, pk); err != nil {
		t.Fatalf("HardDelete by composite PK: %v", err)
	}

	// Verify deleted
	exists, err = client.ProductTagLabels().Exists(ctx, pk)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("product_tag_label should not exist after HardDelete")
	}
}

// --- Upsert with composite PK conflict target ---

func TestUpsertCompositePK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPKUpsertCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "CPKUpsertProduct",
		Price:      40.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	pk := models.ProductTagLabelPK{
		ProductID: product.ID, TagName: "weight", Label: "heavy",
	}
	t.Cleanup(func() { _ = client.ProductTagLabels().HardDelete(ctx, pk) })

	// Insert via upsert
	desc := "initial"
	inserted, err := client.ProductTagLabels().Upsert(ctx, &models.CreateProductTagLabelInput{
		ProductID:   product.ID,
		TagName:     "weight",
		Label:       "heavy",
		Description: omittable.Set(&desc),
	}, models.ProductTagLabelConflictPK)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	if inserted.Description == nil || *inserted.Description != "initial" {
		t.Errorf("Upsert insert description = %v, want %q", inserted.Description, "initial")
	}

	// Update via upsert (same PK, different description)
	updatedDesc := "upserted"
	upserted, err := client.ProductTagLabels().Upsert(ctx, &models.CreateProductTagLabelInput{
		ProductID:   product.ID,
		TagName:     "weight",
		Label:       "heavy",
		Description: omittable.Set(&updatedDesc),
	}, models.ProductTagLabelConflictPK)
	if err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}
	if upserted.Description == nil || *upserted.Description != "upserted" {
		t.Errorf("Upsert update description = %v, want %q", upserted.Description, "upserted")
	}

	// Verify only one row exists
	count, err := client.ProductTagLabels().Count(ctx, &models.ProductTagLabelFilter{
		ProductID: &comparator.ID{Eq: new(product.ID.String())},
		TagName:   &comparator.ID{Eq: new("weight")},
	})
	if err != nil {
		t.Fatalf("Count after upsert: %v", err)
	}
	if count != 1 {
		t.Errorf("Count after upsert = %d, want 1", count)
	}
}

// --- GetMany with filter on composite PK table ---

func TestGetManyCompositePKFilter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CPKFilterCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "CPKFilterProduct",
		Price:      50.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	// Create multiple entries
	for _, pair := range []struct{ tag, label string }{
		{"color", "blue"},
		{"color", "green"},
		{"size", "medium"},
	} {
		_, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
			ProductID: product.ID,
			TagName:   pair.tag,
			Label:     pair.label,
		})
		if err != nil {
			t.Fatalf("Create product_tag_label (%s/%s): %v", pair.tag, pair.label, err)
		}
	}
	t.Cleanup(func() {
		for _, pair := range []struct{ tag, label string }{
			{"color", "blue"},
			{"color", "green"},
			{"size", "medium"},
		} {
			_ = client.ProductTagLabels().HardDelete(ctx, models.ProductTagLabelPK{
				ProductID: product.ID, TagName: pair.tag, Label: pair.label,
			})
		}
	})

	// Filter by tag_name = "color"
	results, err := client.ProductTagLabels().GetMany(ctx, &models.GetProductTagLabelsInput{
		Filter: &models.ProductTagLabelFilter{
			TagName: &comparator.ID{Eq: new("color")},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with filter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany with tag_name filter returned %d results, want 2", len(results))
	}

	// Filter by product_id
	all, err := client.ProductTagLabels().GetMany(ctx, &models.GetProductTagLabelsInput{
		Filter: &models.ProductTagLabelFilter{
			ProductID: &comparator.ID{Eq: new(product.ID.String())},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with product_id filter: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("GetMany with product_id filter returned %d results, want 3", len(all))
	}
}
