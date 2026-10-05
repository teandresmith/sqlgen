package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// The order_totals materialized view groups order_items by product_id:
// line_count = COUNT(oi.id), total_quantity = SUM(oi.quantity).
// seedProductForView creates order items with quantities 1..N, so a product
// seeded with N items shows line_count == N and total_quantity == N*(N+1)/2.

// TestMatviewRefreshObservesBaseTableChanges proves the matview semantics:
// rows seeded after the last refresh are invisible (stale snapshot) until
// Refresh recomputes the stored rows.
func TestMatviewRefreshObservesBaseTableChanges(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prodID, cleanup := seedProductForView(t, ctx, client, "MvStale", 10.0, 3)
	t.Cleanup(cleanup)

	// Stale snapshot: the matview was last refreshed (or created) before this
	// product existed, so Get must miss.
	if _, err := client.OrderTotal().Get(ctx, prodID); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("pre-refresh Get error = %v, want ErrNotFound (stale snapshot must not see new rows)", err)
	}

	if err := client.OrderTotal().Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, err := client.OrderTotal().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("post-refresh Get: %v", err)
	}
	if got.LineCount != 3 {
		t.Errorf("LineCount = %d, want 3", got.LineCount)
	}
	if got.TotalQuantity != 6 {
		t.Errorf("TotalQuantity = %d, want 6 (1+2+3)", got.TotalQuantity)
	}
}

func TestMatviewReadSurface(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	id1, cleanup1 := seedProductForView(t, ctx, client, "MvRead1", 10.0, 2)
	t.Cleanup(cleanup1)
	id2, cleanup2 := seedProductForView(t, ctx, client, "MvRead2", 20.0, 4)
	t.Cleanup(cleanup2)

	if err := client.OrderTotal().Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	filter := &models.OrderTotalFilter{ProductID: &comparator.ID{In: idStrings([]uuid.UUID{id1, id2})}}

	// GetMany with filter + sort (total_quantity: id1 = 3, id2 = 10).
	rows, err := client.OrderTotal().GetMany(ctx, &models.GetOrderTotalsInput{
		Filter: filter,
		Sorts:  []sql.Sort{{Column: "total_quantity", Direction: sql.Desc}},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("GetMany returned %d rows, want 2", len(rows))
	}
	if rows[0].ProductID != id2 || rows[1].ProductID != id1 {
		t.Errorf("sort order = [%s %s], want [%s %s] (total_quantity desc)",
			rows[0].ProductID, rows[1].ProductID, id2, id1)
	}
	if rows[0].TotalQuantity != 10 {
		t.Errorf("id2 TotalQuantity = %d, want 10 (1+2+3+4)", rows[0].TotalQuantity)
	}

	// Count.
	count, err := client.OrderTotal().Count(ctx, filter)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}

	// Paginate.
	page, err := client.OrderTotal().Paginate(ctx, models.PaginateInput[models.OrderTotalFilter]{
		Filter: filter,
		Limit:  1,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("Paginate items = %d, want 1", len(page.Items))
	}
	if page.TotalCount != 2 {
		t.Errorf("Paginate TotalCount = %d, want 2", page.TotalCount)
	}

	// Connection (cursor_keys: product_id via the view config).
	first := 1
	seen := map[uuid.UUID]bool{}
	var after *string
	for {
		conn, err := client.OrderTotal().Connection(ctx, models.ConnectionInput[models.OrderTotalFilter]{
			Filter: filter,
			First:  &first,
			After:  after,
		})
		if err != nil {
			t.Fatalf("Connection: %v", err)
		}
		for _, edge := range conn.Edges {
			seen[edge.Node.ProductID] = true
		}
		if !conn.PageInfo.HasNextPage {
			break
		}
		after = conn.PageInfo.EndCursor
	}
	if !seen[id1] || !seen[id2] {
		t.Errorf("Connection pages missed rows: seen = %v", seen)
	}
}

func TestMatviewRefreshConcurrently(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prodID, cleanup := seedProductForView(t, ctx, client, "MvConc", 15.0, 2)
	t.Cleanup(cleanup)

	// Succeeds against the unique-indexed, populated matview and picks up the
	// base-table change without blocking readers.
	if err := client.OrderTotal().RefreshConcurrently(ctx); err != nil {
		t.Fatalf("RefreshConcurrently: %v", err)
	}

	got, err := client.OrderTotal().Get(ctx, prodID)
	if err != nil {
		t.Fatalf("post-refresh Get: %v", err)
	}
	if got.LineCount != 2 {
		t.Errorf("LineCount = %d, want 2", got.LineCount)
	}
}

func TestMatviewRefreshConcurrentlyInTransaction(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	querier := dbpgx.New(testPool)
	err := database.WithTransaction(ctx, querier, "mv_refresh_tx", func(txCtx context.Context) error {
		return client.OrderTotal().RefreshConcurrently(txCtx)
	})
	if !errors.Is(err, database.ErrRefreshConcurrentlyInTx) {
		t.Fatalf("in-tx RefreshConcurrently error = %v, want ErrRefreshConcurrentlyInTx", err)
	}

	// Plain Refresh is transaction-safe.
	err = database.WithTransaction(ctx, querier, "mv_plain_refresh_tx", func(txCtx context.Context) error {
		return client.OrderTotal().Refresh(txCtx)
	})
	if err != nil {
		t.Fatalf("in-tx Refresh error = %v, want nil (Refresh may run inside a transaction)", err)
	}
}
