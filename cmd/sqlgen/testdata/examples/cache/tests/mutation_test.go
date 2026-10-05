package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestMutation_CreateSetsCache verifies Create populates the cache with the
// full entity (PRD §27.7 Create row).
func TestMutation_CreateSetsCache(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "CacheMe", SKU: uniqueSku(t, "create-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	sets := env.backend.filterCalls("set")
	if len(sets) != 1 {
		t.Fatalf("Create: want 1 backend Set, got %d", len(sets))
	}
}

// TestMutation_CreateManySetsCache verifies CreateMany populates the cache
// once per entity.
func TestMutation_CreateManySetsCache(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	inputs := []*models.CreateProductInput{
		{Name: "A", SKU: uniqueSku(t, "cm-1"), Price: 1.0},
		{Name: "B", SKU: uniqueSku(t, "cm-2"), Price: 2.0},
		{Name: "C", SKU: uniqueSku(t, "cm-3"), Price: 3.0},
	}
	created, err := env.client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	ids := make([]int64, len(created))
	for i, p := range created {
		ids[i] = p.ID
	}
	t.Cleanup(func() { _ = env.client.Products().HardDeleteMany(ctx, ids) })

	sets := env.backend.filterCalls("set")
	if len(sets) != len(inputs) {
		t.Errorf("CreateMany: want %d Set calls, got %d", len(inputs), len(sets))
	}
}

// TestMutation_UpdateInvalidatesKey verifies Update dispatches
// InvalidateMany([key]) (PRD §27.7).
func TestMutation_UpdateInvalidatesKey(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Before", SKU: uniqueSku(t, "upd-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	env.backend.reset()
	if _, err := env.client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
		Name: omittable.Set("After"),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 {
		t.Fatalf("Update: want 1 invalidate_many, got %d", len(calls))
	}
	if len(calls[0].keys) != 1 {
		t.Errorf("Update invalidate: want 1 key, got %d", len(calls[0].keys))
	}
}

// TestMutation_UpdateManyInvalidatesBatch verifies UpdateMany produces a
// single InvalidateMany batch.
func TestMutation_UpdateManyInvalidatesBatch(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p1, err := env.client.Products().Create(ctx, &models.CreateProductInput{Name: "M1", SKU: uniqueSku(t, "um-1"), Price: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	p2, err := env.client.Products().Create(ctx, &models.CreateProductInput{Name: "M2", SKU: uniqueSku(t, "um-2"), Price: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDeleteMany(ctx, []int64{p1.ID, p2.ID}) })

	env.backend.reset()
	if _, err := env.client.Products().UpdateMany(ctx, []models.UpdateProductItem{
		{ID: p1.ID, Input: &models.UpdateProductInput{Name: omittable.Set("M1x")}},
		{ID: p2.ID, Input: &models.UpdateProductInput{Name: omittable.Set("M2x")}},
	}); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 {
		t.Fatalf("UpdateMany: want 1 invalidate_many, got %d", len(calls))
	}
	if len(calls[0].keys) != 2 {
		t.Errorf("UpdateMany invalidate keys = %d, want 2", len(calls[0].keys))
	}
}

// TestMutation_UpsertInvalidatesKey verifies Upsert invalidates via
// InvalidateMany (single key case).
func TestMutation_UpsertInvalidatesKey(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	sku := uniqueSku(t, "ups-1")
	p, err := env.client.Products().Upsert(ctx, &models.CreateProductInput{
		Name: "Upserted", SKU: sku, Price: 4.2,
	}, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	env.backend.reset()
	_, err = env.client.Products().Upsert(ctx, &models.CreateProductInput{
		Name: "UpsertedAgain", SKU: sku, Price: 5.0,
	}, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) == 0 {
		t.Fatal("Upsert: expected invalidate_many, got none")
	}
}

// TestMutation_IncrementInvalidates verifies Increment fires Invalidate.
func TestMutation_IncrementInvalidates(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Inc", SKU: uniqueSku(t, "inc-1"), Price: 1.0, Stock: omittable.Set[int64](10),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	env.backend.reset()
	if err := env.client.Products().Increment(ctx, p.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementStock, Amount: 5,
	}); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 || len(calls[0].keys) != 1 {
		t.Errorf("Increment: want 1 invalidate_many of 1 key, got %+v", calls)
	}
}

// TestMutation_SoftDeleteInvalidates verifies SoftDelete + SoftDeleteMany +
// Restore + RestoreMany all trigger the documented invalidation shape.
func TestMutation_SoftDeleteInvalidates(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "A1", Author: "x",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a2, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "A2", Author: "x",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID}) })

	// SoftDelete
	env.backend.reset()
	if _, err := env.client.Articles().SoftDelete(ctx, a1.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("SoftDelete: want 1 invalidate_many, got %d", len(c))
	}

	// SoftDeleteMany
	env.backend.reset()
	if _, err := env.client.Articles().SoftDeleteMany(ctx, []int64{a2.ID}); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("SoftDeleteMany: want 1 invalidate_many, got %d", len(c))
	}

	// Restore
	env.backend.reset()
	if _, err := env.client.Articles().Restore(ctx, a1.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("Restore: want 1 invalidate_many, got %d", len(c))
	}

	// RestoreMany
	env.backend.reset()
	if _, err := env.client.Articles().RestoreMany(ctx, []int64{a2.ID}); err != nil {
		t.Fatalf("RestoreMany: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("RestoreMany: want 1 invalidate_many, got %d", len(c))
	}
}

// TestMutation_HardDeleteInvalidates verifies HardDelete + HardDeleteMany fire
// the expected invalidate_many.
func TestMutation_HardDeleteInvalidates(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Hard", SKU: uniqueSku(t, "hd-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	env.backend.reset()
	if err := env.client.Products().HardDelete(ctx, p.ID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("HardDelete: want 1 invalidate_many, got %d", len(c))
	}

	// HardDeleteMany
	p2, _ := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Hard2", SKU: uniqueSku(t, "hd-2"), Price: 1.0,
	})
	env.backend.reset()
	if err := env.client.Products().HardDeleteMany(ctx, []int64{p2.ID}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("HardDeleteMany: want 1 invalidate_many, got %d", len(c))
	}
}

// TestMutation_WhereMutationsInvalidateByKey verifies UpdateWhere /
// SoftDeleteWhere / HardDeleteWhere / RestoreWhere dispatch
// InvalidateMany over the PKs the predicate matched, not a table-wide pattern
// wipe (PRD §27.7). Entity-table invalidation only — the view cascade
// still uses patterns, which is why HardDeleteWhere below targets products and
// asserts on invalidate_many rather than on a pattern count.
func TestMutation_WhereMutationsInvalidateByKey(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "W1", Author: "z"})
	a2, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "W2", Author: "z"})
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID}) })

	// UpdateWhere
	env.backend.reset()
	if _, err := env.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: new("z")}},
		&models.UpdateArticleInput{Title: omittable.Set("W1x")},
	); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) == 0 {
		t.Error("UpdateWhere: want invalidate_many, got none")
	}
	if c := env.backend.filterCalls("invalidate_pattern"); len(c) != 0 {
		t.Errorf("UpdateWhere: want 0 invalidate_pattern, got %d", len(c))
	}

	// SoftDeleteWhere
	env.backend.reset()
	if _, err := env.client.Articles().SoftDeleteWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: new("z")}},
	); err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) == 0 {
		t.Error("SoftDeleteWhere: want invalidate_many, got none")
	}
	if c := env.backend.filterCalls("invalidate_pattern"); len(c) != 0 {
		t.Errorf("SoftDeleteWhere: want 0 invalidate_pattern, got %d", len(c))
	}

	// RestoreWhere
	env.backend.reset()
	if _, err := env.client.Articles().RestoreWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: new("z")}, DeletedAt: &comparator.NullableTime{Null: new(false)}},
	); err != nil {
		t.Fatalf("RestoreWhere: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) == 0 {
		t.Error("RestoreWhere: want invalidate_many, got none")
	}
	if c := env.backend.filterCalls("invalidate_pattern"); len(c) != 0 {
		t.Errorf("RestoreWhere: want 0 invalidate_pattern, got %d", len(c))
	}

	// HardDeleteWhere. The seed Create also cascades to the product_summary
	// view, so the reset lands between the two mutations to keep the
	// assertion on HardDeleteWhere alone.
	if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "HDW", SKU: uniqueSku(t, "hdw-1"), Price: 1.0,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	env.backend.reset()
	if err := env.client.Products().HardDeleteWhere(
		ctx,
		&models.ProductFilter{SKU: &comparator.String{Eq: new(uniqueSku(t, "hdw-1"))}},
	); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) == 0 {
		t.Error("HardDeleteWhere: want invalidate_many, got none")
	}
	// products is the source table of the product_summary view
	// (invalidate_on: [products]), so exactly one pattern remains — the view
	// cascade (§27.11). The entity arm contributes none.
	if c := env.backend.filterCalls("invalidate_pattern"); len(c) != 1 {
		t.Errorf("HardDeleteWhere: want 1 invalidate_pattern (view cascade only), got %d", len(c))
	}
}

// TestMutation_DirectInvalidateAPIs verifies Invalidate / InvalidateMany /
// InvalidateTable public Cache methods (PRD §27.9).
func TestMutation_DirectInvalidateAPIs(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()

	if err := env.cache.Invalidate(ctx, models.TableProducts, int64(42)); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_many"); len(c) != 1 {
		t.Errorf("Invalidate routes to InvalidateMany: want 1 call, got %d", len(c))
	}

	env.backend.reset()
	if err := env.cache.InvalidateMany(ctx, models.TableProducts, []any{int64(1), int64(2), int64(3)}); err != nil {
		t.Fatalf("InvalidateMany: %v", err)
	}
	batchCalls := env.backend.filterCalls("invalidate_many")
	if len(batchCalls) != 1 {
		t.Errorf("InvalidateMany: want 1 call, got %d", len(batchCalls))
	}
	if len(batchCalls) > 0 && len(batchCalls[0].keys) != 3 {
		t.Errorf("InvalidateMany: want 3 keys, got %d", len(batchCalls[0].keys))
	}

	env.backend.reset()
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}
	if c := env.backend.filterCalls("invalidate_pattern"); len(c) != 1 {
		t.Errorf("InvalidateTable: want 1 invalidate_pattern, got %d", len(c))
	}
}

// TestMutation_CompositePKRoundTrip is a regression guard: mutation on a
// composite-PK table → InvalidateMany(TableOrderItems, AffectedPKs) → next
// Get(OrderItemPK{...}) misses and repopulates.
func TestMutation_CompositePKRoundTrip(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	// Seed an order_items row.
	oi, err := env.client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		OrderID: 100, ProductID: 200, Quantity: 1, UnitPrice: 9.99,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.OrderItems().HardDelete(ctx, models.OrderItemPK{OrderID: 100, ProductID: 200}) })

	// Warm the cache via Get.
	pk := models.OrderItemPK{OrderID: oi.OrderID, ProductID: oi.ProductID}
	if _, err := env.client.OrderItems().Get(ctx, pk); err != nil {
		t.Fatalf("Get warm: %v", err)
	}

	// Mutate (Update) — should emit InvalidateMany with the composite PK struct.
	env.backend.reset()
	if _, err := env.client.OrderItems().Update(ctx, pk, &models.UpdateOrderItemInput{
		Quantity: omittable.Set[int64](5),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 {
		t.Fatalf("composite Update: want 1 invalidate_many, got %d", len(calls))
	}
	// The generated keyForOrderItem joins order_id:product_id after the pk: label.
	if len(calls[0].keys) == 0 || calls[0].keys[0] == "" {
		t.Fatal("composite Update: invalidated key is empty")
	}

	// Next Get should miss and repopulate. Count the DB queries before and
	// after; the Get must go to DB.
	env.counter.reset()
	env.backend.reset()
	got, err := env.client.OrderItems().Get(ctx, pk)
	if err != nil {
		t.Fatalf("Get (post-invalidate): %v", err)
	}
	if got.Quantity != 5 {
		t.Errorf("Quantity = %d, want 5 (cache should have missed)", got.Quantity)
	}
	if env.counter.selectQueries() == 0 {
		t.Error("composite Get after invalidate: expected DB query, got 0")
	}
	if env.metrics.missCount() == 0 {
		t.Error("composite Get after invalidate: expected metrics.Miss, got none")
	}
}

// TestMutation_UpsertManyInvalidatesBatch verifies UpsertMany dispatches one
// InvalidateMany carrying a key per written row (PRD §27.7's UpsertMany row:
// "Invalidate many: InvalidateMany(keys(AffectedPKs))").
//
// Without the OpUpsertMany arm, the op would fall out of dispatchMutation's
// switch to a bare `return nil` and nothing would be invalidated at all. The switch has a permissive default, so the omission
// never failed to compile — this pin is the only thing that catches it.
func TestMutation_UpsertManyInvalidatesBatch(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	sku1, sku2 := uniqueSku(t, "upm-1"), uniqueSku(t, "upm-2")
	seeded, err := env.client.Products().UpsertMany(ctx, []*models.CreateProductInput{
		{Name: "UM1", SKU: sku1, Price: 1},
		{Name: "UM2", SKU: sku2, Price: 2},
	}, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("UpsertMany seed: %v", err)
	}
	ids := make([]int64, len(seeded))
	for i, p := range seeded {
		ids[i] = p.ID
	}
	t.Cleanup(func() { _ = env.client.Products().HardDeleteMany(ctx, ids) })

	// Re-upsert the same two rows: both take the conflict branch, so this also
	// pins that a row which was updated rather than inserted is still evicted.
	env.backend.reset()
	if _, err := env.client.Products().UpsertMany(ctx, []*models.CreateProductInput{
		{Name: "UM1x", SKU: sku1, Price: 11},
		{Name: "UM2x", SKU: sku2, Price: 22},
	}, models.ProductConflictSKU); err != nil {
		t.Fatalf("UpsertMany conflict: %v", err)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 {
		t.Fatalf("UpsertMany: want 1 invalidate_many, got %d", len(calls))
	}
	if len(calls[0].keys) != 2 {
		t.Errorf("UpsertMany invalidate keys = %d, want 2", len(calls[0].keys))
	}
	// §27.7 gives UpsertMany the "Invalidate many" row, not Create's "Set"
	// row — it must evict, never write the returned entities through.
	if sets := env.backend.filterCalls("set"); len(sets) != 0 {
		t.Errorf("UpsertMany: want 0 backend Set calls (invalidate, not write-through), got %d", len(sets))
	}
}
