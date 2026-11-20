package suite

import (
	"context"
	"errors"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// viewSeed creates products priced 100, 200, 300 and 400, named a to d in key
// order. premium_products selects price >= 200, so a is in the table only.
func viewSeed(t *testing.T, h *Harness) (a, b, c, d *models.Product) {
	t.Helper()
	return createPricedProduct(t, h, "a", 100), createPricedProduct(t, h, "b", 200),
		createPricedProduct(t, h, "c", 300), createPricedProduct(t, h, "d", 400)
}

// viewRow is the premium_products row of a product the view selects.
func viewRow(p *models.Product) *models.PremiumProduct {
	return &models.PremiumProduct{ID: p.ID, Name: p.Name, Price: p.Price}
}

func viewRows(ps ...*models.Product) []*models.PremiumProduct {
	rows := make([]*models.PremiumProduct, len(ps))
	for i, p := range ps {
		rows[i] = viewRow(p)
	}
	return rows
}

func viewByPrice() []sql.Sort { return []sql.Sort{{Column: "price", Direction: sql.Asc}} }

// getView: a view with a declared key has Get.
func getView(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a view with a declared key returns the row its query selects", func(t *testing.T) {
		h.Reset(t)
		a, b, _, _ := viewSeed(t, h)
		got, err := h.Client.PremiumProduct().Get(ctx, b.ID)
		if err != nil {
			t.Fatalf("Get(b) error = %v, want nil", err)
		}
		if diff := gocmp.Diff(viewRow(b), got); diff != "" {
			t.Errorf("Get(b) (-want +got):\n%s", diff)
		}
		// a is a products row the view's WHERE leaves out.
		if got, err := h.Client.PremiumProduct().Get(ctx, a.ID); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get(a) = %+v, %v, want ErrNotFound", got, err)
		}
	})
}

// getManyView: a view's GetMany reads the rows of its query.
func getManyView(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a view returns the rows its query selects", func(t *testing.T) {
		h.Reset(t)
		_, b, c, d := viewSeed(t, h)
		all, err := h.Client.PremiumProduct().GetMany(ctx, &models.GetPremiumProductsInput{Sorts: viewByPrice()})
		if err != nil {
			t.Fatalf("GetMany() error = %v, want nil", err)
		}
		if diff := gocmp.Diff(viewRows(b, c, d), all); diff != "" {
			t.Errorf("GetMany() (-want +got):\n%s", diff)
		}
		filtered, err := h.Client.PremiumProduct().GetMany(ctx, &models.GetPremiumProductsInput{
			Filter: &models.PremiumProductFilter{Price: &comparator.Number[int64]{Lte: new(int64(300))}},
			Sorts:  viewByPrice(),
		})
		if err != nil {
			t.Fatalf("GetMany(price <= 300) error = %v, want nil", err)
		}
		if diff := gocmp.Diff(viewRows(b, c), filtered); diff != "" {
			t.Errorf("GetMany(price <= 300) (-want +got):\n%s", diff)
		}
	})
}

// countView: a view's Count counts the rows of its query.
func countView(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a view counts the rows its query selects", func(t *testing.T) {
		h.Reset(t)
		viewSeed(t, h)
		if n, err := h.Client.PremiumProduct().Count(ctx, nil); err != nil || n != 3 {
			t.Errorf("Count(nil) = %d, %v, want 3, nil", n, err)
		}
		filter := &models.PremiumProductFilter{Price: &comparator.Number[int64]{Lte: new(int64(300))}}
		if n, err := h.Client.PremiumProduct().Count(ctx, filter); err != nil || n != 2 {
			t.Errorf("Count(price <= 300) = %d, %v, want 2, nil", n, err)
		}
	})
}

// paginateView: a view's Paginate pages through the rows of its query.
func paginateView(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a view pages through the rows its query selects", func(t *testing.T) {
		h.Reset(t)
		_, b, c, d := viewSeed(t, h)
		first, err := h.Client.PremiumProduct().Paginate(ctx, models.PaginateInput[models.PremiumProductFilter]{Sort: viewByPrice(), Limit: 2})
		if err != nil {
			t.Fatalf("Paginate(limit 2) error = %v, want nil", err)
		}
		want := &models.PaginateResult[models.PremiumProduct]{Items: viewRows(b, c), TotalCount: 3, Limit: 2, HasMore: true}
		if diff := gocmp.Diff(want, first); diff != "" {
			t.Errorf("Paginate(limit 2) (-want +got):\n%s", diff)
		}
		second, err := h.Client.PremiumProduct().Paginate(ctx, models.PaginateInput[models.PremiumProductFilter]{Sort: viewByPrice(), Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("Paginate(limit 2, offset 2) error = %v, want nil", err)
		}
		want = &models.PaginateResult[models.PremiumProduct]{Items: viewRows(d), TotalCount: 3, Offset: 2, Limit: 2}
		if diff := gocmp.Diff(want, second); diff != "" {
			t.Errorf("Paginate(limit 2, offset 2) (-want +got):\n%s", diff)
		}
	})
}

// connectionView: a view's Connection pages through the rows of its query.
func connectionView(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a view pages through the rows its query selects", func(t *testing.T) {
		h.Reset(t)
		_, b, c, d := viewSeed(t, h)
		// The view declares no cursor keys, so pages follow its key, which
		// orders b, c, d.
		var pages [][]*models.PremiumProduct
		var after *string
		for range 3 {
			conn, err := h.Client.PremiumProduct().Connection(ctx, models.ConnectionInput[models.PremiumProductFilter]{First: new(2), After: after})
			if err != nil {
				t.Fatalf("Connection(first 2, after %v) error = %v, want nil", after, err)
			}
			if conn.TotalCount != 3 {
				t.Errorf("Connection page %d TotalCount = %d, want 3", len(pages), conn.TotalCount)
			}
			page := make([]*models.PremiumProduct, len(conn.Edges))
			for i, e := range conn.Edges {
				page[i] = e.Node
			}
			pages = append(pages, page)
			if !conn.PageInfo.HasNextPage {
				break
			}
			after = conn.PageInfo.EndCursor
		}
		if diff := gocmp.Diff([][]*models.PremiumProduct{viewRows(b, c), viewRows(d)}, pages); diff != "" {
			t.Errorf("Connection pages (-want +got):\n%s", diff)
		}
	})
}
