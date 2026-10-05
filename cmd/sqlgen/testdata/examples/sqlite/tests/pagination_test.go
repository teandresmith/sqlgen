package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestOffsetPagination(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "PaginationCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	var productIDs []int64
	for i := range 5 {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: fmt.Sprintf("PagProduct%d", i),
			Price: float64(i+1) * 10.0, SKU: fmt.Sprintf("PAG-%03d", i),
		})
		if err != nil {
			t.Fatalf("Create product %d: %v", i, err)
		}
		productIDs = append(productIDs, p.ID)
	}
	t.Cleanup(func() {
		for _, id := range productIDs {
			_ = client.Products().HardDelete(ctx, id)
		}
	})

	// Page 1: limit=2, offset=0
	result, err := client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: productIDs},
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
	if result.Limit != 2 {
		t.Errorf("Page 1 Limit = %d, want 2", result.Limit)
	}

	// Page 3: limit=2, offset=4
	result, err = client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: productIDs},
		},
		Limit:  2,
		Offset: 4,
	})
	if err != nil {
		t.Fatalf("Paginate page 3: %v", err)
	}
	if len(result.Items) != 1 {
		t.Errorf("Page 3 items = %d, want 1", len(result.Items))
	}
	if result.HasMore {
		t.Error("Page 3 should not have more pages (offset+limit ≥ total)")
	}
	if result.Offset == 0 {
		t.Error("Page 3 should have a non-zero offset (signals previous-page existence)")
	}
}

func TestCursorPagination(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CursorCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	var productIDs []int64
	for i := range 5 {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: fmt.Sprintf("CursorProduct%d", i),
			Price: float64(i+1) * 5.0, SKU: fmt.Sprintf("CUR-%03d", i),
		})
		if err != nil {
			t.Fatalf("Create product %d: %v", i, err)
		}
		productIDs = append(productIDs, p.ID)
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		for _, id := range productIDs {
			_ = client.Products().HardDelete(ctx, id)
		}
	})

	first := 2

	// Forward: first 2
	conn, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: productIDs},
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

	// Forward: next page after cursor
	afterCursor := conn.PageInfo.EndCursor
	conn2, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: productIDs},
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
	connBack, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: productIDs},
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
