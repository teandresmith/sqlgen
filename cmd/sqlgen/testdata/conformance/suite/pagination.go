package suite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// paginate: Paginate returns one offset page of the matching rows with their total count.
func paginate(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the requested page in the caller's sort with the total count", func(t *testing.T) {
		h.Reset(t)
		// Inserted out of price order; the filter leaves z out.
		pagSeedPrices(t, h, "c", 30, "a", 10, "e", 50, "b", 20, "d", 40, "z", 999)
		sort := []sql.Sort{models.NewProductSorter().Price(sql.Desc)}
		page, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Filter: pagPriceBelow(100), Sort: sort, Limit: 2, Offset: 1,
		})
		if err != nil {
			t.Fatalf("Paginate(offset 1, limit 2) error = %v, want nil", err)
		}
		pagCheckPage(t, page, []string{"d", "c"}, 5, 1, 2, true)

		last, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Filter: pagPriceBelow(100), Sort: sort, Limit: 2, Offset: 3,
		})
		if err != nil {
			t.Fatalf("Paginate(offset 3, limit 2) error = %v, want nil", err)
		}
		pagCheckPage(t, last, []string{"b", "a"}, 5, 3, 2, false)

		// A short last page: Offset+Limit passes the total.
		short, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Filter: pagPriceBelow(100), Sort: sort, Limit: 2, Offset: 4,
		})
		if err != nil {
			t.Fatalf("Paginate(offset 4, limit 2) error = %v, want nil", err)
		}
		pagCheckPage(t, short, []string{"a"}, 5, 4, 2, false)
	})

	t.Run("an offset with no limit returns every row past the offset", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "c", 30, "a", 10, "d", 40, "b", 20)
		page, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Sort: []sql.Sort{models.NewProductSorter().Price(sql.Asc)}, Offset: 2,
		})
		if err != nil {
			t.Fatalf("Paginate(offset 2, no limit) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"c", "d"}, pagNames(page.Items)); diff != "" {
			t.Errorf("Paginate(offset 2, no limit) items (-want +got):\n%s", diff)
		}
		if page.TotalCount != 4 {
			t.Errorf("Paginate(offset 2, no limit) TotalCount = %d, want 4", page.TotalCount)
		}
	})

	t.Run("a zero input returns every row and reports no more pages", func(t *testing.T) {
		h.Reset(t)
		ids := pagSeedPrices(t, h, "c", 30, "a", 10, "b", 20)
		page, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{})
		if err != nil {
			t.Fatalf("Paginate(zero input) error = %v, want nil", err)
		}
		if diff := cmp.Diff(pagSorted(ids), pagSorted(pagIDs(page.Items))); diff != "" {
			t.Errorf("Paginate(zero input) item keys (-want +got):\n%s", diff)
		}
		if page.TotalCount != 3 {
			t.Errorf("Paginate(zero input) TotalCount = %d, want 3", page.TotalCount)
		}
		if page.HasMore {
			t.Errorf("Paginate(zero input) HasMore = true, want false: the page holds every row")
		}
	})

	t.Run("a filter matching no row returns an empty page with a zero total", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10, "b", 20)
		page, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("missing")}}, Limit: 2,
		})
		if err != nil {
			t.Fatalf("Paginate(name = missing) error = %v, want nil", err)
		}
		if len(page.Items) != 0 {
			t.Errorf("Paginate(name = missing) items = %v, want none", pagNames(page.Items))
		}
		if page.TotalCount != 0 || page.HasMore {
			t.Errorf("Paginate(name = missing) TotalCount, HasMore = %d, %v, want 0, false", page.TotalCount, page.HasMore)
		}
	})

	t.Run("soft-deleted rows are left out of the page and the total unless the filter sets the deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		if _, err := h.Client.Documents().SoftDelete(ctx, ids[1]); err != nil {
			t.Fatalf("SoftDelete(%d) error = %v", ids[1], err)
		}
		sort := []sql.Sort{models.NewDocumentSorter().ID(sql.Asc)}
		live, err := h.Client.Documents().Paginate(ctx, models.PaginateInput[models.DocumentFilter]{Sort: sort, Limit: 10})
		if err != nil {
			t.Fatalf("Paginate(documents) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{ids[0], ids[2]}, pagDocIDs(live.Items)); diff != "" {
			t.Errorf("Paginate(documents) item keys (-want +got):\n%s", diff)
		}
		if live.TotalCount != 2 {
			t.Errorf("Paginate(documents) TotalCount = %d, want 2", live.TotalCount)
		}
		all, err := h.Client.Documents().Paginate(ctx, models.PaginateInput[models.DocumentFilter]{
			Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{}}, Sort: sort, Limit: 10,
		})
		if err != nil {
			t.Fatalf("Paginate(documents, deleted-at comparator) error = %v, want nil", err)
		}
		if diff := cmp.Diff(ids, pagDocIDs(all.Items)); diff != "" {
			t.Errorf("Paginate(documents, deleted-at comparator) item keys (-want +got):\n%s", diff)
		}
		if all.TotalCount != 3 {
			t.Errorf("Paginate(documents, deleted-at comparator) TotalCount = %d, want 3", all.TotalCount)
		}
	})

	t.Run("reads inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in-tx", Price: 5}); err != nil {
				return err
			}
			h.Querier.ResetCount()
			page, err := h.Client.Products().Paginate(txCtx, models.PaginateInput[models.ProductFilter]{
				Sort: []sql.Sort{models.NewProductSorter().Price(sql.Asc)}, Limit: 5,
			})
			if err != nil {
				return err
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Paginate inside the transaction sent %q outside it, want nothing", stmts)
			}
			if diff := cmp.Diff([]string{"in-tx", "a"}, pagNames(page.Items)); diff != "" {
				t.Errorf("Paginate inside the transaction items (-want +got):\n%s", diff)
			}
			if page.TotalCount != 2 {
				t.Errorf("Paginate inside the transaction TotalCount = %d, want 2", page.TotalCount)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("fires one paginate query hook carrying the caller's input", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10, "b", 20, "c", 30)
		client, seen := queryRecorder(h)
		input := models.PaginateInput[models.ProductFilter]{
			Filter: pagPriceBelow(100), Sort: []sql.Sort{models.NewProductSorter().Price(sql.Desc)}, Limit: 2, Offset: 1,
		}
		if _, err := client.Products().Paginate(ctx, input); err != nil {
			t.Fatalf("Paginate error = %v, want nil", err)
		}
		got := seen()
		if len(got) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(got), pagOps(got))
		}
		if got[0].Op != hook.OpPaginate || got[0].Table != models.TableProducts {
			t.Errorf("query hook Op, Table = %q, %q, want %q, %q", got[0].Op, got[0].Table, hook.OpPaginate, models.TableProducts)
		}
		gotInput, ok := got[0].Input.(models.PaginateInput[models.ProductFilter])
		if !ok {
			t.Fatalf("query hook Input is %T, want models.PaginateInput[models.ProductFilter]", got[0].Input)
		}
		if diff := cmp.Diff(input, gotInput); diff != "" {
			t.Errorf("query hook Input (-caller +hook):\n%s", diff)
		}
	})

	t.Run("a narrowed field selection fills only the selected columns", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "c", 30, "a", 10, "b", 20)
		page, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{
			Sort: []sql.Sort{models.NewProductSorter().Price(sql.Desc)}, Limit: 2,
		}, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true}
		})
		if err != nil {
			t.Fatalf("Paginate(name only) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"c", "b"}, pagNames(page.Items)); diff != "" {
			t.Errorf("Paginate(name only) names (-want +got):\n%s", diff)
		}
		for _, p := range page.Items {
			if p.ID != 0 || p.Price != 0 || !p.CreatedAt.IsZero() {
				t.Errorf("Paginate(name only) item %q has ID %d, Price %d, CreatedAt %v, want all zero", p.Name, p.ID, p.Price, p.CreatedAt)
			}
		}
		if page.TotalCount != 3 {
			t.Errorf("Paginate(name only) TotalCount = %d, want 3", page.TotalCount)
		}
	})

	t.Run("a lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			_, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{Limit: 2}, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Paginate with %v outside a transaction error = nil, want an error", mode)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Paginate with %v outside a transaction sent %q, want nothing", mode, stmts)
			}
		}
	})

	t.Run("a lock mode inside a transaction locks the page rows", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		ids := pagSeedPrices(t, h, "a", 10, "b", 20, "c", 30, "d", 40)
		txCtx := beginBoundedTx(t, h)
		page, err := h.Client.Products().Paginate(txCtx, models.PaginateInput[models.ProductFilter]{
			Sort: []sql.Sort{models.NewProductSorter().ID(sql.Asc)}, Limit: 2, Offset: 1,
		}, withLock[models.ProductFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("Paginate with LockForUpdate error = %v, want nil", err)
		}
		locked := pagIDs(page.Items)
		if diff := cmp.Diff(ids[1:3], locked); diff != "" {
			t.Fatalf("Paginate with LockForUpdate item keys (-want +got):\n%s", diff)
		}
		if got := pagSkipLocked(t, h, locked); len(got) != 0 {
			t.Errorf("contender with SkipLocked read page rows %v, want none: they should be locked", got)
		}
		// The row after the page was never read, so it stays unlocked. (The row
		// the offset skips may be locked too: the database reads it.)
		if diff := cmp.Diff(ids[3:], pagSkipLocked(t, h, ids[3:])); diff != "" {
			t.Errorf("contender with SkipLocked read of the row after the page (-want +got):\n%s", diff)
		}
	})

	t.Run("a lock mode on a dialect without row locks returns an error before any SQL", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			_, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{Limit: 2}, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Paginate with %v error = nil, want an error", mode)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Paginate with %v sent %q, want nothing", mode, stmts)
			}
		}
	})

	paginateView(t, h)
}

// connection: Connection returns a Relay page of the matching rows in cursor-key order.
func connection(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("forward pages visit every row once in cursor-key order", func(t *testing.T) {
		h.Reset(t)
		pagSeedTie(t, h)
		pages, infos, totals := pagWalk(t, h, pagPriceBelow(100), 2, true, nil)
		want := [][]string{{"b", "a"}, {"c", "e"}, {"d", "f"}}
		if diff := cmp.Diff(want, pages); diff != "" {
			t.Fatalf("forward pages (-want +got):\n%s", diff)
		}
		wantFlags := [][2]bool{{true, false}, {true, true}, {false, true}}
		for i, info := range infos {
			if got := [2]bool{info.HasNextPage, info.HasPreviousPage}; got != wantFlags[i] {
				t.Errorf("forward page %d HasNextPage, HasPreviousPage = %v, want %v", i, got, wantFlags[i])
			}
		}
		for i, n := range totals {
			if n != 6 {
				t.Errorf("forward page %d TotalCount = %d, want 6", i, n)
			}
		}
	})

	t.Run("backward pages return edges in ascending cursor-key order", func(t *testing.T) {
		h.Reset(t)
		pagSeedTie(t, h)
		pages, infos, totals := pagWalk(t, h, pagPriceBelow(100), 2, false, nil)
		want := [][]string{{"d", "f"}, {"c", "e"}, {"b", "a"}}
		if diff := cmp.Diff(want, pages); diff != "" {
			t.Fatalf("backward pages (-want +got):\n%s", diff)
		}
		wantFlags := [][2]bool{{false, true}, {true, true}, {true, false}}
		for i, info := range infos {
			if got := [2]bool{info.HasNextPage, info.HasPreviousPage}; got != wantFlags[i] {
				t.Errorf("backward page %d HasNextPage, HasPreviousPage = %v, want %v", i, got, wantFlags[i])
			}
		}
		for i, n := range totals {
			if n != 6 {
				t.Errorf("backward page %d TotalCount = %d, want 6", i, n)
			}
		}
	})

	t.Run("a cursor is base64 JSON holding each cursor key's value", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "b", 20)
		a := createPricedProduct(t, h, "a", 10)
		conn, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(1)})
		if err != nil {
			t.Fatalf("Connection(first 1) error = %v, want nil", err)
		}
		if len(conn.Edges) != 1 {
			t.Fatalf("Connection(first 1) returned %d edges, want 1", len(conn.Edges))
		}
		raw, err := base64.StdEncoding.DecodeString(conn.Edges[0].Cursor)
		if err != nil {
			t.Fatalf("cursor %q is not base64: %v", conn.Edges[0].Cursor, err)
		}
		var got map[string]int64
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("cursor %s is not a JSON object of integers: %v", raw, err)
		}
		if diff := cmp.Diff(map[string]int64{"price": 10, "id": a.ID}, got); diff != "" {
			t.Errorf("decoded cursor (-want +got):\n%s", diff)
		}
	})

	t.Run("a zero input returns the default-size first page from the start", func(t *testing.T) {
		h.Reset(t)
		// order_items' page_size is 2, below the row count.
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		for _, pk := range []models.OrderItemPK{{OrderID: 2, ProductID: p1}, {OrderID: 1, ProductID: p2}, {OrderID: 3, ProductID: p1}, {OrderID: 1, ProductID: p1}} {
			createOrderItem(t, h, pk)
		}
		conn, err := h.Client.OrderItems().Connection(ctx, models.ConnectionInput[models.OrderItemFilter]{})
		if err != nil {
			t.Fatalf("OrderItems().Connection(zero input) error = %v, want nil", err)
		}
		want := []models.OrderItemPK{{OrderID: 1, ProductID: p1}, {OrderID: 1, ProductID: p2}}
		if diff := cmp.Diff(want, pagItemPKs(conn.Edges)); diff != "" {
			t.Errorf("OrderItems().Connection(zero input) edges (-want +got):\n%s", diff)
		}
		if !conn.PageInfo.HasNextPage || conn.PageInfo.HasPreviousPage {
			t.Errorf("OrderItems().Connection(zero input) HasNextPage, HasPreviousPage = %v, %v, want true, false",
				conn.PageInfo.HasNextPage, conn.PageInfo.HasPreviousPage)
		}
		if conn.TotalCount != 4 {
			t.Errorf("OrderItems().Connection(zero input) TotalCount = %d, want 4", conn.TotalCount)
		}
	})

	t.Run("a filter matching no row returns no edges, no cursors and a zero total", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10, "b", 20)
		conn, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
			Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("missing")}}, First: new(2),
		})
		if err != nil {
			t.Fatalf("Connection(name = missing) error = %v, want nil", err)
		}
		if len(conn.Edges) != 0 {
			t.Errorf("Connection(name = missing) edges = %v, want none", pagEdgeNames(conn.Edges))
		}
		if conn.PageInfo.StartCursor != nil || conn.PageInfo.EndCursor != nil {
			t.Errorf("Connection(name = missing) StartCursor, EndCursor = %v, %v, want nil, nil",
				conn.PageInfo.StartCursor, conn.PageInfo.EndCursor)
		}
		if conn.PageInfo.HasNextPage || conn.PageInfo.HasPreviousPage {
			t.Errorf("Connection(name = missing) HasNextPage, HasPreviousPage = %v, %v, want false, false",
				conn.PageInfo.HasNextPage, conn.PageInfo.HasPreviousPage)
		}
		if conn.TotalCount != 0 {
			t.Errorf("Connection(name = missing) TotalCount = %d, want 0", conn.TotalCount)
		}
	})

	t.Run("a malformed cursor or both first and last returns ErrInvalidCursor", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10, "b", 20)
		enc := func(s string) *string { return new(base64.StdEncoding.EncodeToString([]byte(s))) }
		cases := []struct {
			name  string
			input models.ConnectionInput[models.ProductFilter]
		}{
			{"after not base64", models.ConnectionInput[models.ProductFilter]{First: new(2), After: new("not*base64!")}},
			{"before not base64", models.ConnectionInput[models.ProductFilter]{Last: new(2), Before: new("not*base64!")}},
			{"after not JSON", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc("not json")}},
			{"before not JSON", models.ConnectionInput[models.ProductFilter]{Last: new(2), Before: enc("not json")}},
			{"after with no keys", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc(`{}`)}},
			{"after missing the trailing key", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc(`{"price":10}`)}},
			{"before missing the leading key", models.ConnectionInput[models.ProductFilter]{Last: new(2), Before: enc(`{"id":1}`)}},
			{"after with a null key", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc(`{"price":10,"id":null}`)}},
			{"before with a null key", models.ConnectionInput[models.ProductFilter]{Last: new(2), Before: enc(`{"price":null,"id":1}`)}},
			{"after with a string for an integer key", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc(`{"price":"10","id":1}`)}},
			{"after with an array for a key", models.ConnectionInput[models.ProductFilter]{First: new(2), After: enc(`{"price":10,"id":[1]}`)}},
			{"before with an object for a key", models.ConnectionInput[models.ProductFilter]{Last: new(2), Before: enc(`{"price":{"a":1},"id":1}`)}},
			{"first and last both set", models.ConnectionInput[models.ProductFilter]{First: new(2), Last: new(2)}},
		}
		for _, tc := range cases {
			conn, err := h.Client.Products().Connection(ctx, tc.input)
			if !errors.Is(err, models.ErrInvalidCursor) {
				t.Errorf("Connection(%s) error = %v, want ErrInvalidCursor", tc.name, err)
			}
			if conn != nil {
				t.Errorf("Connection(%s) = %+v, want nil", tc.name, conn)
			}
		}
		// A composite cursor missing its later key.
		_, err := h.Client.OrderItems().Connection(ctx, models.ConnectionInput[models.OrderItemFilter]{First: new(2), After: enc(`{"order_id":1}`)})
		if !errors.Is(err, models.ErrInvalidCursor) {
			t.Errorf("OrderItems().Connection(after missing product_id) error = %v, want ErrInvalidCursor", err)
		}
	})

	t.Run("soft-deleted rows are left out of the edges and the total unless the filter sets the deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		if _, err := h.Client.Documents().SoftDelete(ctx, ids[1]); err != nil {
			t.Fatalf("SoftDelete(%d) error = %v", ids[1], err)
		}
		live, err := h.Client.Documents().Connection(ctx, models.ConnectionInput[models.DocumentFilter]{First: new(10)})
		if err != nil {
			t.Fatalf("Connection(documents) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{ids[0], ids[2]}, pagDocEdgeIDs(live.Edges)); diff != "" {
			t.Errorf("Connection(documents) edge keys (-want +got):\n%s", diff)
		}
		if live.TotalCount != 2 {
			t.Errorf("Connection(documents) TotalCount = %d, want 2", live.TotalCount)
		}
		all, err := h.Client.Documents().Connection(ctx, models.ConnectionInput[models.DocumentFilter]{
			Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{}}, First: new(10),
		})
		if err != nil {
			t.Fatalf("Connection(documents, deleted-at comparator) error = %v, want nil", err)
		}
		if diff := cmp.Diff(ids, pagDocEdgeIDs(all.Edges)); diff != "" {
			t.Errorf("Connection(documents, deleted-at comparator) edge keys (-want +got):\n%s", diff)
		}
		if all.TotalCount != 3 {
			t.Errorf("Connection(documents, deleted-at comparator) TotalCount = %d, want 3", all.TotalCount)
		}
	})

	t.Run("reads inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in-tx", Price: 5}); err != nil {
				return err
			}
			h.Querier.ResetCount()
			conn, err := h.Client.Products().Connection(txCtx, models.ConnectionInput[models.ProductFilter]{First: new(5)})
			if err != nil {
				return err
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Connection inside the transaction sent %q outside it, want nothing", stmts)
			}
			if diff := cmp.Diff([]string{"in-tx", "a"}, pagEdgeNames(conn.Edges)); diff != "" {
				t.Errorf("Connection inside the transaction edges (-want +got):\n%s", diff)
			}
			if conn.TotalCount != 2 {
				t.Errorf("Connection inside the transaction TotalCount = %d, want 2", conn.TotalCount)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("fires one connection query hook carrying the caller's input", func(t *testing.T) {
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10, "b", 20, "c", 30)
		first, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(1)})
		if err != nil {
			t.Fatalf("Connection(first 1) error = %v, want nil", err)
		}
		client, seen := queryRecorder(h)
		input := models.ConnectionInput[models.ProductFilter]{
			Filter: pagPriceBelow(100), First: new(1), After: first.PageInfo.EndCursor,
		}
		if _, err := client.Products().Connection(ctx, input); err != nil {
			t.Fatalf("Connection error = %v, want nil", err)
		}
		got := seen()
		if len(got) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(got), pagOps(got))
		}
		if got[0].Op != hook.OpConnection || got[0].Table != models.TableProducts {
			t.Errorf("query hook Op, Table = %q, %q, want %q, %q", got[0].Op, got[0].Table, hook.OpConnection, models.TableProducts)
		}
		gotInput, ok := got[0].Input.(models.ConnectionInput[models.ProductFilter])
		if !ok {
			t.Fatalf("query hook Input is %T, want models.ConnectionInput[models.ProductFilter]", got[0].Input)
		}
		if diff := cmp.Diff(input, gotInput); diff != "" {
			t.Errorf("query hook Input (-caller +hook):\n%s", diff)
		}
	})

	t.Run("a narrowed field selection still advances the cursor and leaves unselected columns zero", func(t *testing.T) {
		h.Reset(t)
		pagSeedTie(t, h)
		names := func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true}
		}
		pages, _, _ := pagWalk(t, h, pagPriceBelow(100), 2, true, names)
		if diff := cmp.Diff([][]string{{"b", "a"}, {"c", "e"}, {"d", "f"}}, pages); diff != "" {
			t.Errorf("forward pages with a name-only selection (-want +got):\n%s", diff)
		}
		conn, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(2)}, names)
		if err != nil {
			t.Fatalf("Connection(name only) error = %v, want nil", err)
		}
		for _, e := range conn.Edges {
			if !e.Node.CreatedAt.IsZero() {
				t.Errorf("Connection(name only) node %q CreatedAt = %v, want zero", e.Node.Name, e.Node.CreatedAt)
			}
		}
	})

	t.Run("a lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			_, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(2)}, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Connection with %v outside a transaction error = nil, want an error", mode)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Connection with %v outside a transaction sent %q, want nothing", mode, stmts)
			}
		}
	})

	t.Run("a lock mode inside a transaction locks the page rows", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		pagSeedPrices(t, h, "c", 30, "a", 10, "d", 40, "b", 20)
		txCtx := beginBoundedTx(t, h)
		conn, err := h.Client.Products().Connection(txCtx, models.ConnectionInput[models.ProductFilter]{First: new(2)}, withLock[models.ProductFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("Connection with LockForUpdate error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"a", "b"}, pagEdgeNames(conn.Edges)); diff != "" {
			t.Fatalf("Connection with LockForUpdate edges (-want +got):\n%s", diff)
		}
		var locked []int64
		for _, e := range conn.Edges {
			locked = append(locked, e.Node.ID)
		}
		if got := pagSkipLocked(t, h, locked); len(got) != 0 {
			t.Errorf("contender with SkipLocked read page rows %v, want none: they should be locked", got)
		}
	})

	t.Run("a lock mode on a dialect without row locks returns an error before any SQL", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		pagSeedPrices(t, h, "a", 10)
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			_, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(2)}, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Connection with %v error = nil, want an error", mode)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Connection with %v sent %q, want nothing", mode, stmts)
			}
		}
	})

	t.Run("composite keys page every tuple once in key order in both directions", func(t *testing.T) {
		h.Reset(t)
		p1, p2, p3 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID, createProduct(t, h, "p3").ID
		// Inserted out of key order. Order 1 holds three products, so a page
		// boundary falls inside it in both directions; p1 and p2 each sit in
		// two orders.
		for _, pk := range []models.OrderItemPK{{OrderID: 2, ProductID: p1}, {OrderID: 1, ProductID: p3}, {OrderID: 3, ProductID: p2}, {OrderID: 1, ProductID: p1}, {OrderID: 1, ProductID: p2}} {
			createOrderItem(t, h, pk)
		}
		forward, forwardFlags := pagWalkItems(t, h, true)
		wantForward := [][]models.OrderItemPK{
			{{OrderID: 1, ProductID: p1}, {OrderID: 1, ProductID: p2}},
			{{OrderID: 1, ProductID: p3}, {OrderID: 2, ProductID: p1}},
			{{OrderID: 3, ProductID: p2}},
		}
		if diff := cmp.Diff(wantForward, forward); diff != "" {
			t.Errorf("composite forward pages (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([][2]bool{{true, false}, {true, true}, {false, true}}, forwardFlags); diff != "" {
			t.Errorf("composite forward HasNextPage, HasPreviousPage (-want +got):\n%s", diff)
		}
		backward, backwardFlags := pagWalkItems(t, h, false)
		wantBackward := [][]models.OrderItemPK{
			{{OrderID: 2, ProductID: p1}, {OrderID: 3, ProductID: p2}},
			{{OrderID: 1, ProductID: p2}, {OrderID: 1, ProductID: p3}},
			{{OrderID: 1, ProductID: p1}},
		}
		if diff := cmp.Diff(wantBackward, backward); diff != "" {
			t.Errorf("composite backward pages (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([][2]bool{{false, true}, {true, true}, {true, false}}, backwardFlags); diff != "" {
			t.Errorf("composite backward HasNextPage, HasPreviousPage (-want +got):\n%s", diff)
		}
	})

	connectionView(t, h)
}

// pagSeedTie seeds the products the cursor-order subtests walk. cursor_keys
// are (price, id), and a, c and e tie on price. a is created at another price
// and then updated into the tie, so where the database stores rows (a
// PostgreSQL update writes a new row version) differs from id order inside
// the tie: only the id tiebreaker gives the tie its order. Page boundaries at
// size 2 fall inside the tie in both directions. z is above the price filter
// the subtests use.
func pagSeedTie(t *testing.T, h *Harness) {
	t.Helper()
	ids := pagSeedPrices(t, h, "a", 31, "b", 10, "c", 30, "e", 30, "d", 40, "f", 45, "z", 999)
	if _, err := h.Client.Products().Update(context.Background(), ids[0], &models.UpdateProductInput{Price: omittable.Set(int64(30))}); err != nil {
		t.Fatalf("Update product a: %v", err)
	}
}

// pagWalkItems pages order_items with Connection at size 2 from one end to the
// other and returns each page's keys and (HasNextPage, HasPreviousPage).
func pagWalkItems(t *testing.T, h *Harness, forward bool) ([][]models.OrderItemPK, [][2]bool) {
	t.Helper()
	var (
		pages  [][]models.OrderItemPK
		flags  [][2]bool
		cursor *string
	)
	for range 10 {
		input := models.ConnectionInput[models.OrderItemFilter]{}
		if forward {
			input.First, input.After = new(2), cursor
		} else {
			input.Last, input.Before = new(2), cursor
		}
		conn, err := h.Client.OrderItems().Connection(context.Background(), input)
		if err != nil {
			t.Fatalf("OrderItems().Connection(page %d) error = %v, want nil", len(pages), err)
		}
		pages = append(pages, pagItemPKs(conn.Edges))
		flags = append(flags, [2]bool{conn.PageInfo.HasNextPage, conn.PageInfo.HasPreviousPage})
		if forward {
			if !conn.PageInfo.HasNextPage {
				return pages, flags
			}
			cursor = conn.PageInfo.EndCursor
		} else {
			if !conn.PageInfo.HasPreviousPage {
				return pages, flags
			}
			cursor = conn.PageInfo.StartCursor
		}
	}
	t.Fatalf("OrderItems().Connection paging did not end after 10 pages: %v", pages)
	return nil, nil
}

func pagItemPKs(edges []models.Edge[models.OrderItem]) []models.OrderItemPK {
	out := make([]models.OrderItemPK, len(edges))
	for i, e := range edges {
		out[i] = models.OrderItemPK{OrderID: e.Node.OrderID, ProductID: e.Node.ProductID}
	}
	return out
}

// pagSeedPrices creates one product per (name, price) pair, in argument order,
// and returns their keys.
func pagSeedPrices(t *testing.T, h *Harness, pairs ...any) []int64 {
	t.Helper()
	var ids []int64
	for i := 0; i < len(pairs); i += 2 {
		ids = append(ids, createPricedProduct(t, h, pairs[i].(string), int64(pairs[i+1].(int))).ID)
	}
	return ids
}

func pagPriceBelow(n int64) *models.ProductFilter {
	return &models.ProductFilter{Price: &comparator.Number[int64]{Lt: new(n)}}
}

func pagNames(items []*models.Product) []string {
	out := make([]string, len(items))
	for i, p := range items {
		out[i] = p.Name
	}
	return out
}

func pagIDs(items []*models.Product) []int64 {
	out := make([]int64, len(items))
	for i, p := range items {
		out[i] = p.ID
	}
	return out
}

func pagDocIDs(items []*models.Document) []int64 {
	out := make([]int64, len(items))
	for i, d := range items {
		out[i] = d.ID
	}
	return out
}

func pagEdgeNames(edges []models.Edge[models.Product]) []string {
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = e.Node.Name
	}
	return out
}

func pagDocEdgeIDs(edges []models.Edge[models.Document]) []int64 {
	out := make([]int64, len(edges))
	for i, e := range edges {
		out[i] = e.Node.ID
	}
	return out
}

func pagSorted(ids []int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func pagOps(seen []hook.QueryContext) []hook.QueryOp {
	out := make([]hook.QueryOp, len(seen))
	for i, q := range seen {
		out[i] = q.Op
	}
	return out
}

// pagCheckPage checks a Paginate result against the expected item names and
// echoed fields.
func pagCheckPage(t *testing.T, page *models.PaginateResult[models.Product], names []string, total int64, offset, limit int, hasMore bool) {
	t.Helper()
	if diff := cmp.Diff(names, pagNames(page.Items)); diff != "" {
		t.Errorf("Paginate(offset %d, limit %d) items (-want +got):\n%s", offset, limit, diff)
	}
	if page.TotalCount != total || page.Offset != offset || page.Limit != limit || page.HasMore != hasMore {
		t.Errorf("Paginate(offset %d, limit %d) TotalCount, Offset, Limit, HasMore = %d, %d, %d, %v, want %d, %d, %d, %v",
			offset, limit, page.TotalCount, page.Offset, page.Limit, page.HasMore, total, offset, limit, hasMore)
	}
}

// pagWalk pages products with Connection from one end to the other, forward
// with First and After or backward with Last and Before, and returns each
// page's node names, PageInfo and TotalCount in the order fetched. It checks
// that StartCursor and EndCursor are the page's first and last edge cursors.
func pagWalk(t *testing.T, h *Harness, filter *models.ProductFilter, size int, forward bool, opt func(*models.CallOptions[models.ProductFieldOptions])) ([][]string, []models.PageInfo, []int64) {
	t.Helper()
	var opts []func(*models.CallOptions[models.ProductFieldOptions])
	if opt != nil {
		opts = append(opts, opt)
	}
	var (
		pages  [][]string
		infos  []models.PageInfo
		totals []int64
		cursor *string
	)
	for range 10 {
		input := models.ConnectionInput[models.ProductFilter]{Filter: filter}
		if forward {
			input.First, input.After = new(size), cursor
		} else {
			input.Last, input.Before = new(size), cursor
		}
		conn, err := h.Client.Products().Connection(context.Background(), input, opts...)
		if err != nil {
			t.Fatalf("Connection(page %d) error = %v, want nil", len(pages), err)
		}
		pages = append(pages, pagEdgeNames(conn.Edges))
		infos = append(infos, conn.PageInfo)
		totals = append(totals, conn.TotalCount)
		if len(conn.Edges) > 0 {
			if conn.PageInfo.StartCursor == nil || *conn.PageInfo.StartCursor != conn.Edges[0].Cursor {
				t.Errorf("page %d StartCursor = %v, want the first edge's cursor", len(pages)-1, conn.PageInfo.StartCursor)
			}
			if conn.PageInfo.EndCursor == nil || *conn.PageInfo.EndCursor != conn.Edges[len(conn.Edges)-1].Cursor {
				t.Errorf("page %d EndCursor = %v, want the last edge's cursor", len(pages)-1, conn.PageInfo.EndCursor)
			}
		}
		if forward {
			if !conn.PageInfo.HasNextPage {
				return pages, infos, totals
			}
			cursor = conn.PageInfo.EndCursor
		} else {
			if !conn.PageInfo.HasPreviousPage {
				return pages, infos, totals
			}
			cursor = conn.PageInfo.StartCursor
		}
	}
	t.Fatalf("Connection paging did not end after 10 pages: %v", pages)
	return nil, nil, nil
}

// pagSkipLocked reads the listed products from a second transaction with
// LockForUpdateSkipLocked and returns the keys it could read: a key another
// transaction holds locked is skipped.
func pagSkipLocked(t *testing.T, h *Harness, ids []int64) []int64 {
	t.Helper()
	got, err := h.Client.Products().GetMany(otherTx(t, h), &models.GetProductsInput{
		Filter: &models.ProductFilter{ID: &comparator.Number[int64]{In: ids}},
	}, withLock[models.ProductFieldOptions](sql.LockForUpdateSkipLocked))
	if err != nil {
		t.Fatalf("contender GetMany with SkipLocked error = %v, want nil", err)
	}
	return pagIDs(got)
}
