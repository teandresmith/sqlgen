package tests

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Offset Pagination ---

func TestOffsetPagination(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "PaginationCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	var productIDs []uuid.UUID
	for i := range 5 {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       fmt.Sprintf("PagProduct%d", i),
			Price:      float64(i+1) * 10.0,
			CategoryID: cat.ID,
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
			ID: &comparator.ID{In: idStrings(productIDs)},
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
			ID: &comparator.ID{In: idStrings(productIDs)},
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

// --- Cursor Pagination ---

func TestCursorPagination(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CursorCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	var productIDs []uuid.UUID
	for i := range 5 {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       fmt.Sprintf("CursorProduct%d", i),
			Price:      float64(i+1) * 5.0,
			CategoryID: cat.ID,
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
			ID: &comparator.ID{In: idStrings(productIDs)},
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
			ID: &comparator.ID{In: idStrings(productIDs)},
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
			ID: &comparator.ID{In: idStrings(productIDs)},
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

// TestConnection_TimeCursorKey pages categories, whose cursor_keys are
// (created_at, id), one row at a time in both directions. Every page after
// the first decodes a time.Time from the cursor and binds it into the keyset
// condition; two rows share a created_at, so the id tie-break must hold too.
func TestConnection_TimeCursorKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	ids := []string{
		"00000000-0000-0000-0000-00000000c001",
		"00000000-0000-0000-0000-00000000c002",
		"00000000-0000-0000-0000-00000000c003",
	}
	names := []string{"TimeCursorCat1", "TimeCursorCat2", "TimeCursorCat3"}
	// created_at orders the rows against their ids: the lowest id is the
	// newest, and the other two share a created_at, so id breaks their tie.
	if _, err := testDB.ExecContext(ctx, `INSERT INTO categories (id, name, created_at) VALUES
		($1, $4, '2026-01-02 00:00:00+00'),
		($2, $5, '2026-01-01 00:00:00.123456+00'),
		($3, $6, '2026-01-01 00:00:00.123456+00')`,
		ids[0], ids[1], ids[2], names[0], names[1], names[2]); err != nil {
		t.Fatalf("seeding categories: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(context.Background(), `DELETE FROM categories WHERE id = ANY($1::uuid[])`, "{"+strings.Join(ids, ",")+"}")
	})

	filter := &models.CategoryFilter{Name: &comparator.String{In: names}}
	one := 1

	var forward []string
	var after *string
	for range len(ids) + 1 {
		conn, err := client.Categories().Connection(ctx, models.ConnectionInput[models.CategoryFilter]{
			Filter: filter, First: &one, After: after,
		})
		if err != nil {
			t.Fatalf("Connection(after %v): %v", after, err)
		}
		for _, e := range conn.Edges {
			forward = append(forward, e.Node.ID.String())
		}
		if !conn.PageInfo.HasNextPage {
			break
		}
		after = conn.PageInfo.EndCursor
	}
	if want := []string{ids[1], ids[2], ids[0]}; !slices.Equal(forward, want) {
		t.Errorf("forward pages = %v, want %v", forward, want)
	}

	var backward []string
	var before *string
	for range len(ids) + 1 {
		conn, err := client.Categories().Connection(ctx, models.ConnectionInput[models.CategoryFilter]{
			Filter: filter, Last: &one, Before: before,
		})
		if err != nil {
			t.Fatalf("Connection(before %v): %v", before, err)
		}
		for _, e := range conn.Edges {
			backward = append(backward, e.Node.ID.String())
		}
		if !conn.PageInfo.HasPreviousPage {
			break
		}
		before = conn.PageInfo.StartCursor
	}
	if want := []string{ids[0], ids[2], ids[1]}; !slices.Equal(backward, want) {
		t.Errorf("backward pages = %v, want %v", backward, want)
	}
}
