package suite

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// count: Count returns the number of rows the filter matches.
func count(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the number of rows the filter matches", func(t *testing.T) {
		h.Reset(t)
		for _, p := range []struct {
			name  string
			price int64
		}{{"a", 100}, {"b", 200}, {"c", 300}, {"d", 400}, {"e", 500}} {
			createPricedProduct(t, h, p.name, p.price)
		}
		tests := []struct {
			name   string
			filter *models.ProductFilter
			want   int64
		}{
			// c, d and e cost at least 300.
			{"price at least 300", &models.ProductFilter{Price: &comparator.Number[int64]{Gte: new(int64(300))}}, 3},
			// a and d; missing names no row.
			{"name in a, d, missing", cntNameIn("a", "d", "missing"), 2},
			// Only d is both at least 300 and named a or d.
			{"both", &models.ProductFilter{
				Name:  &comparator.String{In: []string{"a", "d"}},
				Price: &comparator.Number[int64]{Gte: new(int64(300))},
			}, 1},
		}
		for _, tt := range tests {
			got, err := h.Client.Products().Count(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Count(%s) error = %v, want nil", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("Count(%s) = %d, want %d", tt.name, got, tt.want)
			}
		}
	})

	t.Run("nil or empty filter counts every row", func(t *testing.T) {
		h.Reset(t)
		for _, name := range []string{"a", "b", "c"} {
			createProduct(t, h, name)
		}
		for _, tt := range []struct {
			name   string
			filter *models.ProductFilter
		}{{"nil", nil}, {"&ProductFilter{}", &models.ProductFilter{}}} {
			got, err := h.Client.Products().Count(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Count(%s) error = %v, want nil", tt.name, err)
			}
			if got != 3 {
				t.Errorf("Count(%s) = %d, want 3", tt.name, got)
			}
		}
	})

	t.Run("a filter matching no row returns zero and no error", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		createProduct(t, h, "b")
		got, err := h.Client.Products().Count(ctx, cntNameIn("missing"))
		if err != nil {
			t.Fatalf("Count(name in [missing]) error = %v, want nil", err)
		}
		if got != 0 {
			t.Errorf("Count(name in [missing]) = %d, want 0", got)
		}
	})

	t.Run("soft-deleted rows are not counted unless the filter sets the deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		// Live: a, b, c. Soft-deleted: gone1, gone2.
		for _, title := range []string{"a", "b", "c"} {
			createDocument(t, h, title)
		}
		createDeletedDocument(t, h, "gone1")
		createDeletedDocument(t, h, "gone2")
		tests := []struct {
			name   string
			filter *models.DocumentFilter
			want   int64
		}{
			{"nil", nil, 3},
			{"title in a, gone1", cntTitleIn("a", "gone1"), 1},
			{"deleted only", &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}}, 2},
			{"live only, set explicitly", &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(true)}}, 3},
			{"title in a, gone1, deleted", &models.DocumentFilter{
				Title:     &comparator.String{In: []string{"a", "gone1"}},
				DeletedAt: &comparator.NullableTime{Null: new(false)},
			}, 1},
		}
		for _, tt := range tests {
			got, err := h.Client.Documents().Count(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Count(%s) error = %v, want nil", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("Count(%s) = %d, want %d", tt.name, got, tt.want)
			}
		}
	})

	t.Run("counts a row written earlier in the same transaction", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "b", Price: 1}); err != nil {
				return err
			}
			h.Querier.ResetCount()
			got, err := h.Client.Products().Count(txCtx, cntNameIn("a", "b"))
			if err != nil {
				return err
			}
			if got != 2 {
				t.Errorf("Count inside the transaction = %d, want 2", got)
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Count inside the transaction sent %q outside it, want nothing", stmts)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("fires one count query hook carrying the filter", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		client, seen := queryRecorder(h)
		filter := cntTitleIn("a")
		if _, err := client.Documents().Count(ctx, filter); err != nil {
			t.Fatalf("Count(title in [a]) error = %v, want nil", err)
		}
		cntAssertOneQueryHook(t, seen(), hook.OpCount, filter)
	})

	countView(t, h)
}

// existsWhere: ExistsWhere reports whether any row matches the filter.
func existsWhere(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("reports whether any row matches the filter", func(t *testing.T) {
		h.Reset(t)
		createPricedProduct(t, h, "a", 100)
		createPricedProduct(t, h, "b", 200)
		tests := []struct {
			name   string
			filter *models.ProductFilter
			want   bool
		}{
			{"name in b, missing", cntNameIn("b", "missing"), true},
			{"price at least 200", &models.ProductFilter{Price: &comparator.Number[int64]{Gte: new(int64(200))}}, true},
			// a exists and so does a row costing 200, but not both in one row.
			{"name a and price at least 200", &models.ProductFilter{
				Name:  &comparator.String{In: []string{"a"}},
				Price: &comparator.Number[int64]{Gte: new(int64(200))},
			}, false},
			{"price at least 300", &models.ProductFilter{Price: &comparator.Number[int64]{Gte: new(int64(300))}}, false},
		}
		for _, tt := range tests {
			got, err := h.Client.Products().ExistsWhere(ctx, tt.filter)
			if err != nil {
				t.Fatalf("ExistsWhere(%s) error = %v, want nil", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("ExistsWhere(%s) = %v, want %v", tt.name, got, tt.want)
			}
		}
	})

	t.Run("nil or empty filter tests for any row", func(t *testing.T) {
		h.Reset(t)
		filters := []struct {
			name   string
			filter *models.ProductFilter
		}{{"nil", nil}, {"&ProductFilter{}", &models.ProductFilter{}}}
		check := func(want bool) {
			t.Helper()
			for _, f := range filters {
				got, err := h.Client.Products().ExistsWhere(ctx, f.filter)
				if err != nil {
					t.Fatalf("ExistsWhere(%s) error = %v, want nil", f.name, err)
				}
				if got != want {
					t.Errorf("ExistsWhere(%s) with %d products = %v, want %v", f.name, rowCount(t, h, "products"), got, want)
				}
			}
		}
		check(false)
		createProduct(t, h, "a")
		check(true)
	})

	t.Run("a filter matching no row returns false and no error", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		got, err := h.Client.Products().ExistsWhere(ctx, cntNameIn("missing"))
		if err != nil {
			t.Fatalf("ExistsWhere(name in [missing]) error = %v, want nil", err)
		}
		if got {
			t.Errorf("ExistsWhere(name in [missing]) = true, want false")
		}
	})

	t.Run("soft-deleted rows match only when the filter sets the deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		deleted := &comparator.NullableTime{Null: new(false)}
		check := func(name string, filter *models.DocumentFilter, want bool) {
			t.Helper()
			got, err := h.Client.Documents().ExistsWhere(ctx, filter)
			if err != nil {
				t.Fatalf("ExistsWhere(%s) error = %v, want nil", name, err)
			}
			if got != want {
				t.Errorf("ExistsWhere(%s) = %v, want %v", name, got, want)
			}
		}
		// While the only document is soft-deleted, a default read finds none.
		createDeletedDocument(t, h, "gone")
		check("nil, only a deleted document", nil, false)
		// A live document shows the default still matches live rows.
		createDocument(t, h, "live")
		tests := []struct {
			name   string
			filter *models.DocumentFilter
			want   bool
		}{
			{"title in live", cntTitleIn("live"), true},
			{"title in gone", cntTitleIn("gone"), false},
			{"deleted only", &models.DocumentFilter{DeletedAt: deleted}, true},
			{"title in gone, deleted", &models.DocumentFilter{Title: &comparator.String{In: []string{"gone"}}, DeletedAt: deleted}, true},
			{"title in live, deleted", &models.DocumentFilter{Title: &comparator.String{In: []string{"live"}}, DeletedAt: deleted}, false},
		}
		for _, tt := range tests {
			check(tt.name, tt.filter, tt.want)
		}
	})

	t.Run("finds a row written earlier in the same transaction", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "b", Price: 1}); err != nil {
				return err
			}
			h.Querier.ResetCount()
			got, err := h.Client.Products().ExistsWhere(txCtx, cntNameIn("b"))
			if err != nil {
				return err
			}
			if !got {
				t.Errorf("ExistsWhere(name in [b]) inside the transaction = false, want true")
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("ExistsWhere inside the transaction sent %q outside it, want nothing", stmts)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		got, err := h.Client.Products().ExistsWhere(ctx, cntNameIn("b"))
		if err != nil {
			t.Fatalf("ExistsWhere(name in [b]) after the rollback error = %v, want nil", err)
		}
		if got {
			t.Errorf("ExistsWhere(name in [b]) after the rollback = true, want false")
		}
	})

	t.Run("fires one exists query hook carrying the filter and no key", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		client, seen := queryRecorder(h)
		filter := cntTitleIn("a")
		if _, err := client.Documents().ExistsWhere(ctx, filter); err != nil {
			t.Fatalf("ExistsWhere(title in [a]) error = %v, want nil", err)
		}
		cntAssertOneQueryHook(t, seen(), hook.OpExists, filter)
	})
}

// cntAssertOneQueryHook checks that exactly one query hook fired, on the
// documents table with op, no key, and the caller's filter pointer as Input.
func cntAssertOneQueryHook(t *testing.T, seen []hook.QueryContext, op hook.QueryOp, filter *models.DocumentFilter) {
	t.Helper()
	if len(seen) != 1 {
		t.Fatalf("query hook fired %d times, want 1", len(seen))
	}
	q := seen[0]
	if q.Op != op || q.Table != models.TableDocuments || q.PK != nil {
		t.Errorf("QueryContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
			q.Op, q.Table, q.PK, op, models.TableDocuments)
	}
	if q.Input != any(filter) {
		t.Errorf("QueryContext.Input = %v, want the caller's filter %p", q.Input, filter)
	}
}

func cntNameIn(names ...string) *models.ProductFilter {
	return &models.ProductFilter{Name: &comparator.String{In: names}}
}

func cntTitleIn(titles ...string) *models.DocumentFilter {
	return &models.DocumentFilter{Title: &comparator.String{In: titles}}
}
