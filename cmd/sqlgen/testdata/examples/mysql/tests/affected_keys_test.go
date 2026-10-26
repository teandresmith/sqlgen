package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
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

// orders is non-strict (strict_updates: false), so an Update whose key
// matches no row returns no error; it must record no affected key either.
func TestAffectedKeys_UpdateRecordsOnlyWrittenKeys(t *testing.T) {
	ctx := context.Background()
	const missing = int64(987654321)
	client, affected := affectedPKsRecorder()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Name: "AKUpd", Email: "ak-upd@example.com"})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID, Total: 1})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	// An empty FieldOptions skips the re-read, which would report the missing row.
	skipFetch := func(o *models.CallOptions[models.OrderFieldOptions]) { o.FieldOptions = &models.OrderFieldOptions{} }

	t.Run("non-strict update of a missing key", func(t *testing.T) {
		if _, err := client.Orders().Update(ctx, missing, &models.UpdateOrderInput{Total: omittable.Set(2.0)}, skipFetch); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs = %v, want none", got)
		}
	})
	t.Run("non-strict update of an existing key", func(t *testing.T) {
		if _, err := client.Orders().Update(ctx, order.ID, &models.UpdateOrderInput{Total: omittable.Set(3.0)}, skipFetch); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if diff := cmp.Diff([]any{order.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs (-want +got):\n%s", diff)
		}
	})
	t.Run("update many over a missing key, a no-op item and an existing key", func(t *testing.T) {
		if _, err := client.Orders().UpdateMany(ctx, []models.UpdateOrderItem{
			{ID: missing, Input: &models.UpdateOrderInput{Total: omittable.Set(4.0)}},
			{ID: order.ID, Input: &models.UpdateOrderInput{}},
			{ID: order.ID, Input: &models.UpdateOrderInput{Total: omittable.Set(5.0)}},
		}); err != nil {
			t.Fatalf("UpdateMany: %v", err)
		}
		if diff := cmp.Diff([]any{order.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs (-want +got):\n%s", diff)
		}
	})
}

// A filter-based delete pre-reads the composite keys it will remove; each
// recorded key carries its own column values, whatever order the pre-read
// selects the key columns in.
func TestAffectedKeys_WhereDeleteRecordsCompositeKeysByColumn(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "AKWhereCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Title:      "AKWhereProduct",
		Price:      1.00,
		CategoryID: cat.ID,
		SKU:        "ak-where-sku",
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	written := models.ProductTagLabelPK{ProductID: product.ID, TagName: "size", Label: "large"}
	if _, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID: written.ProductID, TagName: written.TagName, Label: written.Label,
	}); err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}
	t.Cleanup(func() { _ = client.ProductTagLabels().HardDelete(ctx, written) })

	filter := &models.ProductTagLabelFilter{ProductID: &comparator.Number[int64]{Eq: &product.ID}}
	if err := client.ProductTagLabels().HardDeleteWhere(ctx, filter); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}
	if diff := cmp.Diff([]any{written}, affected()); diff != "" {
		t.Errorf("AffectedPKs (-want +got):\n%s", diff)
	}
}

// Key columns that scan into each other's types without error would, bound by
// position against a reordered pre-read, record each key with its values
// swapped.
func TestAffectedKeys_UpdateWhereRecordsCompositeKeysByColumn(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Name: "AKWhereUpd", Email: "ak-where-upd@example.com"})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
	// A key whose two columns hold the same value reads alike either way
	// round, so the category id must differ from the user id.
	var cat *models.Category
	for _, name := range []string{"AKWhereUpdCat1", "AKWhereUpdCat2"} {
		c, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: name})
		if err != nil {
			t.Fatalf("Create category: %v", err)
		}
		t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, c.ID) })
		if int64(c.ID) != user.ID {
			cat = c
			break
		}
	}

	written := models.UserCategoryPK{UserID: user.ID, CategoryID: cat.ID}
	if _, err := client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID: written.UserID, CategoryID: written.CategoryID,
	}); err != nil {
		t.Fatalf("Create user_category: %v", err)
	}
	t.Cleanup(func() { _ = client.UserCategories().HardDelete(ctx, written) })

	filter := &models.UserCategoryFilter{UserID: &comparator.Number[int64]{Eq: &user.ID}}
	if _, err := client.UserCategories().UpdateWhere(ctx, filter, &models.UpdateUserCategoryInput{
		Slot: omittable.Set(new(int32(7))),
	}); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if diff := cmp.Diff([]any{written}, affected()); diff != "" {
		t.Errorf("AffectedPKs (-want +got):\n%s", diff)
	}
}

// MySQL compares string keys under the column's collation, which is
// case-insensitive by default, so a listed key can delete a row whose key
// differs from it under Go ==. The batch must still record the key it was
// given: the row is gone, and its cache entry and event must follow.
func TestAffectedKeys_BatchRecordsKeysTheDatabaseMatched(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()
	product := seedAffectedKeysProduct(t, ctx, client, "ak-collation-sku")

	stored := models.ProductTagLabelPK{ProductID: product.ID, TagName: "color", Label: "red"}
	if _, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
		ProductID: stored.ProductID, TagName: stored.TagName, Label: stored.Label,
	}); err != nil {
		t.Fatalf("Create product_tag_label: %v", err)
	}
	t.Cleanup(func() { _ = client.ProductTagLabels().HardDelete(ctx, stored) })

	listed := models.ProductTagLabelPK{ProductID: product.ID, TagName: "COLOR", Label: "Red"}
	if err := client.ProductTagLabels().HardDeleteMany(ctx, []models.ProductTagLabelPK{listed}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}
	if _, err := client.ProductTagLabels().Get(ctx, stored); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("Get after HardDeleteMany: err = %v, want ErrNotFound", err)
	}
	if diff := cmp.Diff([]any{listed}, affected()); diff != "" {
		t.Errorf("AffectedPKs (-want +got):\n%s", diff)
	}
}

// The pre-read returns its keys as a set; the recorded keys follow the order
// the caller listed them in.
func TestAffectedKeys_BatchRecordsWrittenKeysInListOrder(t *testing.T) {
	ctx := context.Background()
	const missing = int64(987654321)
	client, affected := affectedPKsRecorder()

	var ids []int64
	for _, title := range []string{"AKOrder1", "AKOrder2"} {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: title, Author: "ak"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })
		ids = append(ids, a.ID)
	}

	if err := client.Articles().HardDeleteMany(ctx, []int64{ids[1], missing, ids[0]}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}
	if diff := cmp.Diff([]any{ids[1], ids[0]}, affected()); diff != "" {
		t.Errorf("AffectedPKs (-want +got):\n%s", diff)
	}
}

// MySQL's affected-row count counts the rows a statement changed, not the
// rows it matched, so a write that leaves its row as it was reports none. A
// strict write must still find the row, and every write records the key of
// the row it matched, as it does on PostgreSQL and SQLite.
func TestAffectedKeys_UnchangedRowStillMatches(t *testing.T) {
	ctx := context.Background()
	client, affected := affectedPKsRecorder()
	product := seedAffectedKeysProduct(t, ctx, client, "ak-unchanged-sku")

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Name: "AKUnchanged", Email: "ak-unchanged@example.com"})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID, Total: 1})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "AKLive", Author: "ak"})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, article.ID) })

	tag, err := client.Tags().Create(ctx, &models.CreateTagInput{Name: "ak-unchanged-tag"})
	if err != nil {
		t.Fatalf("Create tag: %v", err)
	}
	t.Cleanup(func() { _ = client.Tags().HardDelete(ctx, tag.ID) })

	cases := []struct {
		name  string
		write func() error
		want  []any
	}{
		{name: "strict update to the current value", want: []any{product.ID}, write: func() error {
			_, err := client.Products().Update(ctx, product.ID, &models.UpdateProductInput{Title: omittable.Set(product.Title)})
			return err
		}},
		{name: "strict increment by zero", want: []any{product.ID}, write: func() error {
			return client.Products().Increment(ctx, product.ID, models.IncrementInput[models.ProductIncrementColumn]{
				Column: models.ProductIncrementPrice,
				Amount: 0,
			})
		}},
		{name: "non-strict update to the current value", want: []any{order.ID}, write: func() error {
			_, err := client.Orders().Update(ctx, order.ID, &models.UpdateOrderInput{Total: omittable.Set(order.Total)})
			return err
		}},
		{name: "update many item set to the current value", want: []any{order.ID}, write: func() error {
			_, err := client.Orders().UpdateMany(ctx, []models.UpdateOrderItem{
				{ID: order.ID, Input: &models.UpdateOrderInput{Total: omittable.Set(order.Total)}},
			})
			return err
		}},
		{name: "non-strict increment by zero", want: []any{order.ID}, write: func() error {
			return client.Orders().Increment(ctx, order.ID, models.IncrementInput[models.OrderIncrementColumn]{
				Column: models.OrderIncrementTotal,
				Amount: 0,
			})
		}},
		{name: "restore of a live row", want: []any{article.ID}, write: func() error {
			_, err := client.Articles().Restore(ctx, article.ID)
			return err
		}},
		{name: "soft delete of a soft-deleted row", want: []any{tag.ID}, write: func() error {
			if _, err := client.Tags().SoftDelete(ctx, tag.ID); err != nil {
				return err
			}
			_, err := client.Tags().SoftDelete(ctx, tag.ID)
			return err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.write(); err != nil {
				t.Fatalf("write: %v", err)
			}
			if diff := cmp.Diff(tt.want, affected()); diff != "" {
				t.Errorf("AffectedPKs (-want +got):\n%s", diff)
			}
		})
	}
}

// seedAffectedKeysProduct creates a product, and the category it needs, for
// the life of the test.
func seedAffectedKeysProduct(t *testing.T, ctx context.Context, client *models.Client, sku string) *models.Product {
	t.Helper()
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: sku + "-cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Title:      sku + "-product",
		Price:      1.00,
		CategoryID: cat.ID,
		SKU:        sku,
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })
	return product
}
