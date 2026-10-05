package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// seedProductForView creates a category, product, order and order_items for view tests.
// Returns the product ID and a cleanup function.
func seedProductForView(t *testing.T, ctx context.Context, client *models.Client, suffix string, price float64, orderItemCount int) (int64, func()) {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ViewCat" + suffix})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	prod, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      "ViewProduct" + suffix,
		Price:      price,
		SKU:        "VIEW-" + suffix,
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}

	var orderItemIDs []int64
	if orderItemCount > 0 {
		user, err := client.Users().Create(ctx, &models.CreateUserInput{
			Email:   fmt.Sprintf("viewuser%s@test.com", suffix),
			Name:    "ViewUser" + suffix,
			Balance: 0,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
			UserID: user.ID,
			Total:  0,
		})
		if err != nil {
			t.Fatalf("create order: %v", err)
		}

		for i := range orderItemCount {
			oi, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
				OrderID:   order.ID,
				ProductID: prod.ID,
				UnitPrice: price,
				Quantity:  int32(i + 1),
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
		t.Fatalf("ProductSummary.Get(%d): %v", prodID, err)
	}
	if ps.ID != prodID {
		t.Errorf("ID = %d, want %d", ps.ID, prodID)
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
			ID: &comparator.Number[int64]{In: []int64{id1, id2}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("GetMany returned %d rows, want 2", len(results))
	}

	byID := make(map[int64]*models.ProductSummary, len(results))
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
		ID: &comparator.Number[int64]{In: []int64{id1, id2}},
	})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

func TestViewPaginate(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var ids []int64
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
			ID: &comparator.Number[int64]{In: ids},
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
}

func TestViewConnection(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var ids []int64
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
			ID: &comparator.Number[int64]{In: ids},
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

	prodID, cleanup := seedProductForView(t, ctx, client, "NullAnn", 15.0, 0)
	t.Cleanup(cleanup)

	ps, err := client.ProductSummary().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Title is forced nullable by @nullable directive
	// The actual value is NOT NULL in the database, but the Go type is *string
	var _ *string = ps.Title
	if ps.Title == nil {
		t.Error("Title should not be nil (has value in DB)")
	}
	if ps.Title != nil && *ps.Title != "ViewProductNullAnn" {
		t.Errorf("Title = %q, want %q", *ps.Title, "ViewProductNullAnn")
	}
}

// --- category_stats view tests (no @pk, comprehensive aggregates) ---

func TestCategoryStatsNoPK(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "StatsCatNoPK"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	var productIDs []int64
	for i, price := range []float64{10.0, 20.0, 30.0} {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID,
			Title:      fmt.Sprintf("StatsProd_%d", i),
			Price:      price,
			SKU:        fmt.Sprintf("STAT-%d", i),
			Attributes: types.JSON([]byte(`{}`)),
		})
		if err != nil {
			t.Fatalf("create product %d: %v", i, err)
		}
		productIDs = append(productIDs, p.ID)
	}
	t.Cleanup(func() {
		for _, id := range productIDs {
			_ = client.Products().HardDelete(ctx, id)
		}
		_ = client.Categories().HardDelete(ctx, cat.ID)
	})

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

	// SUM → *float64
	if cs.TotalPrice == nil || *cs.TotalPrice != 60.0 {
		t.Errorf("TotalPrice = %v, want *60.0", cs.TotalPrice)
	}

	// AVG → *float64
	if cs.AvgPrice == nil || *cs.AvgPrice != 20.0 {
		t.Errorf("AvgPrice = %v, want *20.0", cs.AvgPrice)
	}

	// MIN → *float64
	if cs.MinPrice == nil || *cs.MinPrice != 10.0 {
		t.Errorf("MinPrice = %v, want *10.0", cs.MinPrice)
	}

	// MAX → *float64
	if cs.MaxPrice == nil || *cs.MaxPrice != 30.0 {
		t.Errorf("MaxPrice = %v, want *30.0", cs.MaxPrice)
	}

	// GROUP_CONCAT → *string
	if cs.ProductNames == nil {
		t.Error("ProductNames should not be nil")
	}

	// JSON_ARRAYAGG → json.RawMessage
	if cs.ProductNamesJSON == nil {
		t.Error("ProductNamesJSON should not be nil")
	}
}

func TestCategoryStatsEmptyCategory(t *testing.T) {
	ctx := context.Background()
	client := newClient()

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
	if cs.ProductCount != 0 {
		t.Errorf("ProductCount = %d, want 0", cs.ProductCount)
	}
	if cs.TotalPrice != nil {
		t.Errorf("TotalPrice = %v, want nil", cs.TotalPrice)
	}
	if cs.AvgPrice != nil {
		t.Errorf("AvgPrice = %v, want nil", cs.AvgPrice)
	}
}
