package tests

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// A nested write whose parent write leaves the parent soft-deleted returns
// that parent whether or not the call selects a relationship: the terminal
// read reads it back by key, as the flat write does, with no "is not deleted"
// default.
func TestNested_SoftDeletedParentIsReturnedWithOrWithoutRelationships(t *testing.T) {
	selections := []struct {
		name string
		opts []func(*models.CallOptions[models.ProductFieldOptions])
	}{
		{name: "no relationship selected"},
		{name: "a relationship selected", opts: []func(*models.CallOptions[models.ProductFieldOptions]){
			func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.FieldOptions = &models.ProductFieldOptions{Name: true, DeletedAt: true, OrderItems: &models.OrderItemRelationshipOptions{}}
			},
		}},
	}
	deletedAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	for _, sel := range selections {
		t.Run("CreateWithRelated, "+sel.name, func(t *testing.T) {
			truncateAll(t)
			in := nestedSoftDeletedProduct(t, "created", deletedAt)
			got, err := testClient.Products().CreateWithRelated(nestedCtx(), &models.CreateProductWithRelatedInput{Product: in}, sel.opts...)
			assertSoftDeletedProduct(t, got, err, "created")
		})
		t.Run("UpdateWithRelated, "+sel.name, func(t *testing.T) {
			truncateAll(t)
			in := nestedSoftDeletedProduct(t, "live", time.Time{})
			p, err := testClient.Products().Create(nestedCtx(), &in)
			if err != nil {
				t.Fatalf("seeding product: %v", err)
			}
			got, err := testClient.Products().UpdateWithRelated(nestedCtx(), p.ID, &models.UpdateProductWithRelatedInput{
				Product: models.UpdateProductInput{Name: omittable.Set("updated"), DeletedAt: omittable.Set(&deletedAt)},
			}, sel.opts...)
			assertSoftDeletedProduct(t, got, err, "updated")
		})
		t.Run("UpsertWithRelated, "+sel.name, func(t *testing.T) {
			truncateAll(t)
			in := nestedSoftDeletedProduct(t, "upserted", deletedAt)
			got, err := testClient.Products().UpsertWithRelated(nestedCtx(), &models.UpsertProductWithRelatedInput{Product: in}, models.ProductConflictPK, sel.opts...)
			assertSoftDeletedProduct(t, got, err, "upserted")
		})
	}
}

// nestedSoftDeletedProduct returns a product input under a fresh category,
// soft-deleted at deletedAt unless it is zero.
func nestedSoftDeletedProduct(t *testing.T, name string, deletedAt time.Time) models.CreateProductInput {
	t.Helper()
	in := models.CreateProductInput{
		Name:       name,
		Price:      decimal.NewFromInt(1),
		CategoryID: seedCategoryRow(t, "cat-"+name),
	}
	if !deletedAt.IsZero() {
		in.DeletedAt = omittable.Set(&deletedAt)
	}
	return in
}

func assertSoftDeletedProduct(t *testing.T, got *models.Product, err error, name string) {
	t.Helper()
	if errors.Is(err, models.ErrNotFound) {
		t.Fatalf("error = %v: the write committed a soft-deleted parent, so it must be returned", err)
	}
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got == nil || got.Name != name || got.DeletedAt == nil {
		t.Fatalf("returned %+v, want the soft-deleted %q product", got, name)
	}
}
