package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Get by 2-column composite PK (user_categories) ---

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
