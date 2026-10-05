package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// seedProductForView creates a category, product, order and order_items for view tests.
// Returns the product ID and a cleanup function.
func seedProductForView(t *testing.T, ctx context.Context, client *models.Client, suffix string, price float64, orderItemCount int) (uuid.UUID, func()) {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ViewCat" + suffix})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	prod, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "ViewProduct" + suffix,
		Price:      price,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	var orderItemIDs []uuid.UUID
	if orderItemCount > 0 {
		user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
			Email: fmt.Sprintf("viewuser%s@test.com", suffix),
			Name:  "ViewUser" + suffix,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
			UserID: user.ID,
		})
		if err != nil {
			t.Fatalf("create order: %v", err)
		}

		for i := range orderItemCount {
			oi, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
				OrderID:   order.ID,
				ProductID: prod.ID,
				UnitPrice: price,
				Quantity:  omittable.Set(int32(i + 1)),
			})
			if err != nil {
				t.Fatalf("create order item %d: %v", i, err)
			}
			orderItemIDs = append(orderItemIDs, oi.ID)
		}
	}

	cleanup := func() {
		for _, id := range orderItemIDs {
			_ = client.OrderItems().HardDelete(ctx, id)
		}
		_ = client.Products().HardDelete(ctx, prod.ID)
		_ = client.Categories().HardDelete(ctx, cat.ID)
	}

	return prod.ID, cleanup
}

func TestViewGetByPK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prodID, cleanup := seedProductForView(t, ctx, client, "GetPK", 29.99, 3)
	t.Cleanup(cleanup)

	ps, err := client.ProductSummary().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("ProductSummary.Get(%s): %v", prodID, err)
	}
	if ps.ID != prodID {
		t.Errorf("ID = %q, want %q", ps.ID, prodID)
	}
	if ps.Name != "ViewProductGetPK" {
		t.Errorf("Name = %q, want %q", ps.Name, "ViewProductGetPK")
	}
	if ps.Price != 29.99 {
		t.Errorf("Price = %v, want 29.99", ps.Price)
	}
	if ps.CategoryName != "ViewCatGetPK" {
		t.Errorf("CategoryName = %q, want %q", ps.CategoryName, "ViewCatGetPK")
	}
	if ps.OrderCount != 3 {
		t.Errorf("OrderCount = %d, want 3", ps.OrderCount)
	}
}

func TestViewGetMany(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	id1, cleanup1 := seedProductForView(t, ctx, client, "GM1", 10.0, 2)
	t.Cleanup(cleanup1)
	id2, cleanup2 := seedProductForView(t, ctx, client, "GM2", 20.0, 0)
	t.Cleanup(cleanup2)

	results, err := client.ProductSummary().GetMany(ctx, &models.GetProductSummariesInput{
		Filter: &models.ProductSummaryFilter{
			ID: &comparator.ID{In: idStrings([]uuid.UUID{id1, id2})},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("GetMany returned %d rows, want 2", len(results))
	}

	// Verify filtering: product with 2 order items vs 0
	byID := make(map[uuid.UUID]*models.ProductSummary, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}
	if byID[id1].OrderCount != 2 {
		t.Errorf("id1 OrderCount = %d, want 2", byID[id1].OrderCount)
	}
	if byID[id2].OrderCount != 0 {
		t.Errorf("id2 OrderCount = %d, want 0", byID[id2].OrderCount)
	}
}

func TestViewCount(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	id1, cleanup1 := seedProductForView(t, ctx, client, "Cnt1", 10.0, 0)
	t.Cleanup(cleanup1)
	id2, cleanup2 := seedProductForView(t, ctx, client, "Cnt2", 20.0, 0)
	t.Cleanup(cleanup2)

	count, err := client.ProductSummary().Count(ctx, &models.ProductSummaryFilter{
		ID: &comparator.ID{In: idStrings([]uuid.UUID{id1, id2})},
	})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// Non-matching filter
	count, err = client.ProductSummary().Count(ctx, &models.ProductSummaryFilter{
		Name: &comparator.String{Eq: new("nonexistent_product_xyz")},
	})
	if err != nil {
		t.Fatalf("Count non-matching: %v", err)
	}
	if count != 0 {
		t.Errorf("Count non-matching = %d, want 0", count)
	}
}

func TestViewPaginate(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var ids []uuid.UUID
	var cleanups []func()
	for i := range 5 {
		id, cleanup := seedProductForView(t, ctx, client, fmt.Sprintf("Pag%d", i), float64(i+1)*10.0, 0)
		ids = append(ids, id)
		cleanups = append(cleanups, cleanup)
	}
	t.Cleanup(func() {
		for _, c := range cleanups {
			c()
		}
	})

	result, err := client.ProductSummary().Paginate(ctx, models.PaginateInput[models.ProductSummaryFilter]{
		Filter: &models.ProductSummaryFilter{
			ID: &comparator.ID{In: idStrings(ids)},
		},
		Limit:  2,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("Paginate page 1: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("Page 1 items = %d, want 2", len(result.Items))
	}
	if result.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", result.TotalCount)
	}
	if !result.HasMore {
		t.Error("Page 1 should have more pages")
	}
	if result.Offset != 0 {
		t.Errorf("Page 1 Offset = %d, want 0", result.Offset)
	}
}

func TestViewConnection(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var ids []uuid.UUID
	var cleanups []func()
	for i := range 5 {
		id, cleanup := seedProductForView(t, ctx, client, fmt.Sprintf("Conn%d", i), float64(i+1)*5.0, 0)
		ids = append(ids, id)
		cleanups = append(cleanups, cleanup)
	}
	t.Cleanup(func() {
		for _, c := range cleanups {
			c()
		}
	})

	first := 2
	conn, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{
		Filter: &models.ProductSummaryFilter{
			ID: &comparator.ID{In: idStrings(ids)},
		},
		First: &first,
	})
	if err != nil {
		t.Fatalf("Connection forward: %v", err)
	}
	if len(conn.Edges) != 2 {
		t.Fatalf("Forward edges = %d, want 2", len(conn.Edges))
	}
	if !conn.PageInfo.HasNextPage {
		t.Error("Forward should have next page")
	}
	if conn.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", conn.TotalCount)
	}

	// Next page after cursor
	afterCursor := conn.PageInfo.EndCursor
	conn2, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{
		Filter: &models.ProductSummaryFilter{
			ID: &comparator.ID{In: idStrings(ids)},
		},
		First: &first,
		After: afterCursor,
	})
	if err != nil {
		t.Fatalf("Connection forward page 2: %v", err)
	}
	if len(conn2.Edges) != 2 {
		t.Errorf("Forward page 2 edges = %d, want 2", len(conn2.Edges))
	}
	if !conn2.PageInfo.HasPreviousPage {
		t.Error("Forward page 2 should have previous page")
	}

	// Backward: last 2
	last := 2
	connBack, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{
		Filter: &models.ProductSummaryFilter{
			ID: &comparator.ID{In: idStrings(ids)},
		},
		Last: &last,
	})
	if err != nil {
		t.Fatalf("Connection backward: %v", err)
	}
	if len(connBack.Edges) != 2 {
		t.Errorf("Backward edges = %d, want 2", len(connBack.Edges))
	}
	if !connBack.PageInfo.HasPreviousPage {
		t.Error("Backward should have previous page")
	}
}

func TestViewTypeAnnotation(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prodID, cleanup := seedProductForView(t, ctx, client, "TypeAnn", 50.0, 1)
	t.Cleanup(cleanup)

	ps, err := client.ProductSummary().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// OrderCount should be int32 (from @type annotation override of COUNT's default int64)
	var _ int32 = ps.OrderCount
	if ps.OrderCount != 1 {
		t.Errorf("OrderCount = %d, want 1 (type: int32 from @type annotation)", ps.OrderCount)
	}
}

func TestViewNullableAnnotation(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Product with no description — should be nil due to @nullable directive
	prodID, cleanup := seedProductForView(t, ctx, client, "NullAnn", 15.0, 0)
	t.Cleanup(cleanup)

	ps, err := client.ProductSummary().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Description is forced nullable by @nullable directive — should be nil when unset
	if ps.Description != nil {
		t.Errorf("Description = %q, want nil (forced nullable by @nullable)", *ps.Description)
	}
}

func TestViewNoMutationMethods(t *testing.T) {
	// Compile-time assertion: ProductSummaryClient is read-only.
	// The interface only exposes Get, GetMany, Count, Paginate, Connection.
	// If Create/Update/Delete methods were generated, this would need
	// to change — but per PRD 16.4, views are read-only.
	var _ models.ProductSummaryClient = nil

	// CategoryStatClient has no @pk → no Get method. The view's column set
	// does not include the inherited default cursor_keys (`id`), so Connection
	// is also omitted per PRD §4.13. Only GetMany/Count/Paginate are generated.
	var _ models.CategoryStatClient = nil
}

// --- category_stats view tests (no @pk, comprehensive aggregates) ---

// seedCategoryForStats creates a category with products for category_stats view tests.
func seedCategoryForStats(t *testing.T, ctx context.Context, client *models.Client, suffix string, prices []float64) func() {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "StatsCat" + suffix})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	var productIDs []uuid.UUID
	for i, price := range prices {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       fmt.Sprintf("StatsProd%s_%d", suffix, i),
			Price:      price,
			CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("create product %d: %v", i, err)
		}
		productIDs = append(productIDs, p.ID)
	}

	return func() {
		for _, id := range productIDs {
			_ = client.Products().HardDelete(ctx, id)
		}
		_ = client.Categories().HardDelete(ctx, cat.ID)
	}
}

func TestCategoryStatsNoPK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cleanup := seedCategoryForStats(t, ctx, client, "NoPK", []float64{10.0, 20.0, 30.0})
	t.Cleanup(cleanup)

	// No Get method — only GetMany available (no @pk annotation)
	results, err := client.CategoryStat().GetMany(ctx, &models.GetCategoryStatsInput{
		Filter: &models.CategoryStatFilter{
			CategoryName: &comparator.NullableString{String: comparator.String{Eq: new("StatsCatNoPK")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany returned %d rows, want 1", len(results))
	}

	cs := results[0]

	// @nullable: category_name forced to *string
	var _ *string = cs.CategoryName
	if cs.CategoryName == nil || *cs.CategoryName != "StatsCatNoPK" {
		t.Errorf("CategoryName = %v, want *StatsCatNoPK", cs.CategoryName)
	}

	// @type product_count: int32 (overrides COUNT's default int64)
	var _ int32 = cs.ProductCount
	if cs.ProductCount != 3 {
		t.Errorf("ProductCount = %d, want 3", cs.ProductCount)
	}

	// SUM(p.price) → SQL type passthrough → *float64 (nullable aggregate)
	if cs.TotalPrice == nil || *cs.TotalPrice != 60.0 {
		t.Errorf("TotalPrice = %v, want *60.0", cs.TotalPrice)
	}

	// AVG(p.price) → *float64 (always nullable)
	if cs.AvgPrice == nil || *cs.AvgPrice != 20.0 {
		t.Errorf("AvgPrice = %v, want *20.0", cs.AvgPrice)
	}

	// MIN(p.price) → SQL type passthrough → *float64 (nullable)
	if cs.MinPrice == nil || *cs.MinPrice != 10.0 {
		t.Errorf("MinPrice = %v, want *10.0", cs.MinPrice)
	}

	// MAX(p.price) → SQL type passthrough → *float64 (nullable)
	if cs.MaxPrice == nil || *cs.MaxPrice != 30.0 {
		t.Errorf("MaxPrice = %v, want *30.0", cs.MaxPrice)
	}

	// STRING_AGG(p.name, ', ') → *string
	if cs.ProductNames == nil {
		t.Error("ProductNames should not be nil")
	}

	// BOOL_OR(p.is_active) → *bool (default is_active=true)
	if cs.HasActiveProduct == nil || !*cs.HasActiveProduct {
		t.Errorf("HasActiveProduct = %v, want *true", cs.HasActiveProduct)
	}

	// JSON_AGG(p.name) → json.RawMessage
	if cs.ProductNamesJSON == nil {
		t.Error("ProductNamesJSON should not be nil")
	}

	// ARRAY_AGG(p.name) → []string, resolved from the source column's type and
	// scanned through pq.Array. A frozen []any literal fails here with
	// "unsupported Scan" on the stdlib driver.
	var _ []string = cs.ProductNameList
	if len(cs.ProductNameList) != 3 {
		t.Errorf("ProductNameList = %v, want 3 elements", cs.ProductNameList)
	}
}

func TestCategoryStatsEmptyCategory(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Category with no products — aggregate columns should be nil
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "StatsCatEmpty"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	results, err := client.CategoryStat().GetMany(ctx, &models.GetCategoryStatsInput{
		Filter: &models.CategoryStatFilter{
			CategoryName: &comparator.NullableString{String: comparator.String{Eq: new("StatsCatEmpty")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany returned %d rows, want 1", len(results))
	}

	cs := results[0]

	// COUNT is never null — should be 0 for empty category
	if cs.ProductCount != 0 {
		t.Errorf("ProductCount = %d, want 0", cs.ProductCount)
	}

	// Nullable aggregates should be nil for empty category
	if cs.TotalPrice != nil {
		t.Errorf("TotalPrice = %v, want nil", cs.TotalPrice)
	}
	if cs.AvgPrice != nil {
		t.Errorf("AvgPrice = %v, want nil", cs.AvgPrice)
	}
	if cs.MinPrice != nil {
		t.Errorf("MinPrice = %v, want nil", cs.MinPrice)
	}
	if cs.MaxPrice != nil {
		t.Errorf("MaxPrice = %v, want nil", cs.MaxPrice)
	}

	// ARRAY_AGG ... FILTER over a group with no matching rows yields SQL NULL
	// rather than {NULL}, so it scans as a nil slice instead of failing.
	if cs.ProductNameList != nil {
		t.Errorf("ProductNameList = %v, want nil", cs.ProductNameList)
	}
}

func TestCategoryStatsCount(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cleanup1 := seedCategoryForStats(t, ctx, client, "CntA", []float64{5.0})
	t.Cleanup(cleanup1)
	cleanup2 := seedCategoryForStats(t, ctx, client, "CntB", []float64{15.0})
	t.Cleanup(cleanup2)

	count, err := client.CategoryStat().Count(ctx, &models.CategoryStatFilter{
		ProductCount: &comparator.Number[int32]{Gte: new(int32(1))},
	})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 2 {
		t.Errorf("Count = %d, want >= 2", count)
	}
}

func TestCategoryStatsPaginate(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var cleanups []func()
	for i := range 4 {
		c := seedCategoryForStats(t, ctx, client, fmt.Sprintf("Pg%d", i), []float64{float64(i + 1)})
		cleanups = append(cleanups, c)
	}
	t.Cleanup(func() {
		for _, c := range cleanups {
			c()
		}
	})

	result, err := client.CategoryStat().Paginate(ctx, models.PaginateInput[models.CategoryStatFilter]{
		Filter: &models.CategoryStatFilter{
			CategoryName: &comparator.NullableString{String: comparator.String{Like: new("StatsCatPg%")}},
		},
		Limit:  2,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("Page 1 items = %d, want 2", len(result.Items))
	}
	if result.TotalCount != 4 {
		t.Errorf("TotalCount = %d, want 4", result.TotalCount)
	}
	if !result.HasMore {
		t.Error("Page 1 should have more pages")
	}
}

// TestWarehouseRolesEnumSliceColumn reads a view column whose type is a
// PostgreSQL enum array. The generated column is the named slice UserRoleSlice,
// which supplies its own Scan/Value — it must not be wrapped in pq.Array, and
// its filter must be keyed on the element type. Before the view context carried
// SliceElemType, this shape produced comparator.Slice[UserRoleSlice], which does
// not satisfy comparable, so the package did not compile.
func TestWarehouseRolesEnumSliceColumn(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wh, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "RoleSliceWarehouse",
		Price:        decimal.NewFromFloat(1.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleViewer}),
	})
	if err != nil {
		t.Fatalf("create warehouse: %v", err)
	}
	t.Cleanup(func() { _ = client.Warehouses().HardDelete(ctx, wh.ID) })

	results, err := client.WarehouseRole().GetMany(ctx, &models.GetWarehouseRolesInput{
		Filter: &models.WarehouseRoleFilter{
			WarehouseName: &comparator.String{Eq: new("RoleSliceWarehouse")},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany returned %d rows, want 1", len(results))
	}

	// The filter field is keyed on the element type, not the slice type. This
	// declaration is the compile-time half of the assertion.
	var _ *comparator.Slice[models.UserRole] = (&models.WarehouseRoleFilter{}).AllowedRoles

	if got := results[0].AllowedRoles; len(got) != 2 {
		t.Errorf("AllowedRoles = %v, want 2 elements", got)
	}
}
