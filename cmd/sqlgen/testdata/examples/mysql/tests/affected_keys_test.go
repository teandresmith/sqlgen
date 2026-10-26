package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// A write records in AffectedPKs only the keys whose rows it wrote: the cache
// evicts and the event hook publishes from that list. MySQL has no RETURNING,
// so a batch write learns its written keys from a pre-read.

// affectedPKsRecorder returns a client whose mutation hook keeps the
// AffectedPKs of the last mutation, and a func that returns them.
func affectedPKsRecorder() (*models.Client, func() []any) {
	var last []any
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			last = m.AffectedPKs
			return res, err
		}
	}
	return models.New(dbstdlib.New(testDB), models.WithMutationHook(record)), func() []any { return last }
}

func TestAffectedKeys_BatchRecordsOnlyWrittenKeys(t *testing.T) {
	ctx := context.Background()
	const missing = int64(987654321)
	cases := []struct {
		name string
		// write writes a batch over existing and missing.
		write func(client *models.Client, existing int64) error
		// softDeleted reports whether the existing row starts soft-deleted.
		softDeleted bool
	}{
		{name: "soft delete many", write: func(client *models.Client, existing int64) error {
			_, err := client.Articles().SoftDeleteMany(ctx, []int64{missing, existing})
			return err
		}},
		{name: "restore many", softDeleted: true, write: func(client *models.Client, existing int64) error {
			_, err := client.Articles().RestoreMany(ctx, []int64{missing, existing})
			return err
		}},
		{name: "hard delete many", write: func(client *models.Client, existing int64) error {
			return client.Articles().HardDeleteMany(ctx, []int64{missing, existing})
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, affected := affectedPKsRecorder()
			a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "AK", Author: "ak"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })
			if tt.softDeleted {
				if _, err := client.Articles().SoftDelete(ctx, a.ID); err != nil {
					t.Fatalf("SoftDelete: %v", err)
				}
			}
			if err := tt.write(client, a.ID); err != nil {
				t.Fatalf("write: %v", err)
			}
			if diff := cmp.Diff([]any{a.ID}, affected()); diff != "" {
				t.Errorf("AffectedPKs (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAffectedKeys_BatchMatchingNoRowRecordsNothing(t *testing.T) {
	ctx := context.Background()
	missing := []int64{987654321, 987654322}
	cases := []struct {
		name  string
		write func(client *models.Client) error
	}{
		{name: "soft delete many", write: func(client *models.Client) error {
			_, err := client.Articles().SoftDeleteMany(ctx, missing)
			return err
		}},
		{name: "restore many", write: func(client *models.Client) error {
			_, err := client.Articles().RestoreMany(ctx, missing)
			return err
		}},
		{name: "hard delete many", write: func(client *models.Client) error {
			return client.Articles().HardDeleteMany(ctx, missing)
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, affected := affectedPKsRecorder()
			if err := tt.write(client); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := affected(); len(got) != 0 {
				t.Errorf("AffectedPKs = %v, want none", got)
			}
		})
	}
}

// product_tag_labels' key columns (product_id, tag_name, label) do not sort
// in key order, so the pre-read must bind each column to its key field by name.
func TestAffectedKeys_CompositeBatchRecordsOnlyWrittenKeys(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "AKCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Title:      "AKProduct",
		Price:      1.00,
		CategoryID: cat.ID,
		SKU:        "ak-composite-sku",
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	written := models.ProductTagLabelPK{ProductID: product.ID, TagName: "color", Label: "red"}
	missing := models.ProductTagLabelPK{ProductID: product.ID, TagName: "color", Label: "blue"}
	if _, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID: written.ProductID, TagName: written.TagName, Label: written.Label,
	}); err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}
	t.Cleanup(func() { _ = client.ProductTagLabels().HardDelete(ctx, written) })

	if err := client.ProductTagLabels().HardDeleteMany(ctx, []models.ProductTagLabelPK{missing, written}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}
	if diff := cmp.Diff([]any{written}, affected()); diff != "" {
		t.Errorf("AffectedPKs (-want +got):\n%s", diff)
	}
}
