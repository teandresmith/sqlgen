package tests

// Filter-operator breadth for SQLite.
//
// Scope notes:
//   - SQLite has no native array type and no JSON_CONTAINS operator (JSON1 is
//     optional); array / JSON containment is therefore not covered here. Like,
//     Null, NotNull, Between, Custom, And/Or composition are the SQLite-native
//     operators per PRD §11.2.
//   - SQLite uses `?` placeholders (same as MySQL). The byte-identical SQL
//     round-trip asserts on those.

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// capturingQuerier records SQL strings passed to the underlying Querier so
// tests can assert ToConditions determinism.
type capturingQuerier struct {
	inner database.Querier
	mu    sync.Mutex
	sqls  []string
}

func newCapturingQuerier(inner database.Querier) *capturingQuerier {
	return &capturingQuerier{inner: inner}
}

func (c *capturingQuerier) record(sqlStr string) {
	c.mu.Lock()
	c.sqls = append(c.sqls, sqlStr)
	c.mu.Unlock()
}

func (c *capturingQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.record(sqlStr)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *capturingQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.record(sqlStr)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *capturingQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.record(sqlStr)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *capturingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *capturingQuerier) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.sqls))
	copy(out, c.sqls)
	return out
}

func (c *capturingQuerier) reset() {
	c.mu.Lock()
	c.sqls = nil
	c.mu.Unlock()
}

func (c *capturingQuerier) lastSelect(t *testing.T) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(sqls[i]), "SELECT") {
			return sqls[i]
		}
	}
	t.Fatalf("no SELECT captured; got %d sqls: %v", len(sqls), sqls)
	return ""
}

func TestFilter_Like_WildcardAndEscape_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "LikePatternCat-sqlite"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	names := []string{
		"100% cotton shirt",
		"USB-A cable",
		"premium_widget",
		"LOUDWIDGET SPECIAL",
	}
	ids := make([]int64, 0, len(names))
	for i, name := range names {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: name, Price: 25.00,
			SKU: "LIKE-SQLITE-" + strings.Repeat("x", i+1),
		})
		if err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
		ids = append(ids, p.ID)
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
	}

	pattern := "%cotton%"
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Title: &comparator.String{Like: &pattern},
		},
	})
	if err != nil {
		t.Fatalf("Like cotton: %v", err)
	}
	if len(products) != 1 || !strings.Contains(products[0].Title, "cotton") {
		t.Errorf("Like '%%cotton%%': got %d, want 1", len(products))
	}

	underscorePattern := "premium_widget"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Title: &comparator.String{Like: &underscorePattern},
		},
	})
	if err != nil {
		t.Fatalf("Like underscore: %v", err)
	}
	// SQLite's LIKE is case-insensitive by default for ASCII, so "premium_widget"
	// matches exactly and the `_` wildcard matches itself too. Exactly one row.
	if len(products) != 1 {
		t.Errorf("Like underscore: got %d, want 1", len(products))
	}

	nlikePattern := "%cotton%"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Title: &comparator.String{NLike: &nlikePattern},
		},
	})
	if err != nil {
		t.Fatalf("NLike: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("NLike: got %d, want 3", len(products))
	}
}

func TestFilter_NullAndNotNull_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-null-sqlite-14.5"
	nullArticle, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "Null body", Author: author,
	})
	if err != nil {
		t.Fatalf("Create null: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, nullArticle.ID) })

	bodyText := "has body"
	withBody, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "With body", Author: author, Body: omittable.Set(&bodyText),
	})
	if err != nil {
		t.Fatalf("Create with-body: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, withBody.ID) })

	isNull := true
	articles, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author: &comparator.String{Eq: &author},
			Body:   &comparator.NullableString{Null: &isNull},
		},
	})
	if err != nil {
		t.Fatalf("IS NULL: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != nullArticle.ID {
		t.Errorf("IS NULL: got %d, want 1", len(articles))
	}

	isNotNull := false
	articles, err = client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author: &comparator.String{Eq: &author},
			Body:   &comparator.NullableString{Null: &isNotNull},
		},
	})
	if err != nil {
		t.Fatalf("IS NOT NULL: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != withBody.ID {
		t.Errorf("IS NOT NULL: got %d, want 1", len(articles))
	}
}

func TestFilter_Between_Numeric_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "BetweenNumCat-sqlite"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	prices := []float64{10.00, 50.00, 100.00, 200.00}
	ids := make([]int64, len(prices))
	for i, price := range prices {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: "between-p", Price: price,
			SKU: "BETW-SQLITE-" + strings.Repeat("x", i+1),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids[i] = p.ID
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
	}

	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Price: &comparator.Number[float64]{Between: &comparator.Range[float64]{Start: 50.00, End: 100.00}},
		},
	})
	if err != nil {
		t.Fatalf("Between: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Between: got %d, want 2", len(products))
	}

	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Price: &comparator.Number[float64]{NBetween: &comparator.Range[float64]{Start: 50.00, End: 100.00}},
		},
	})
	if err != nil {
		t.Fatalf("NBetween: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("NBetween: got %d, want 2", len(products))
	}
}

func TestFilter_Between_Timestamp_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-between-ts-sqlite-14.5"
	a1, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "ts-1", Author: author,
	})
	if err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a1.ID) })

	// SQLite datetime is stored as a string; ±1s window guards against rounding
	// at seed time. The baseline filter matches *exactly* a1.CreatedAt since
	// DATETIME is deterministic down to the second resolution.
	lo := a1.CreatedAt.Add(-time.Second)
	hi := a1.CreatedAt.Add(time.Second)

	articles, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author:    &comparator.String{Eq: &author},
			CreatedAt: &comparator.Time{Between: &comparator.Range[time.Time]{Start: lo, End: hi}},
		},
	})
	if err != nil {
		t.Fatalf("Between ts: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != a1.ID {
		t.Errorf("Between ts: got %d, want 1", len(articles))
	}
}

func TestFilter_Custom_MultiplePlaceholders_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CustomPredCat-sqlite"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "c1", Price: 60.00, SKU: "CUST-SQLITE-1",
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "c2", Price: 20.00, SKU: "CUST-SQLITE-2",
	})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	ids := []int64{p1.ID, p2.ID}

	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: ids},
			Price: &comparator.Number[float64]{
				Custom: []sql.Condition{sql.Raw("price > $ AND title LIKE $", 50.00, "c%")},
			},
		},
	})
	if err != nil {
		t.Fatalf("Custom raw: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1.ID {
		t.Errorf("Custom 2-placeholder: got %d, want 1 (p1)", len(products))
	}
}

func TestFilter_AndOrComposition_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ComposeCat-sqlite"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(title, sku string, price float64, inStock bool) *models.Product {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: title, SKU: sku, Price: price,
			InStock: omittable.Set(inStock),
		})
		if err != nil {
			t.Fatalf("Create %s: %v", title, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p
	}
	p1 := mk("sc-p1", "SC-1", 10.00, true)
	p2 := mk("sc-p2", "SC-2", 55.00, true)
	p3 := mk("sc-p3", "SC-3", 120.00, true)
	p4 := mk("sc-p4", "SC-4", 60.00, false)
	ids := []int64{p1.ID, p2.ID, p3.ID, p4.ID}

	trueVal := true
	fifty := 50.00
	hundred := 100.00
	twenty := 20.00
	titlePattern := "sc-%"

	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:      &comparator.Number[int64]{In: ids},
			InStock: &comparator.Bool{Eq: &trueVal},
			And: []*models.ProductFilter{
				{Price: &comparator.Number[float64]{Gte: &fifty}},
				{Title: &comparator.String{Like: &titlePattern}},
			},
		},
	})
	if err != nil {
		t.Fatalf("3-cond And: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("And: got %d, want 2 (p2 + p3)", len(products))
	}

	// Nested Or: in_stock=true AND (price<20 OR price>100) — one branch per
	// `Or` member, which is the spelling the syntax suggests. Members OR
	// together; each member's own fields AND together (PRD §11.1).
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:      &comparator.Number[int64]{In: ids},
			InStock: &comparator.Bool{Eq: &trueVal},
			Or: []*models.ProductFilter{
				{Price: &comparator.Number[float64]{Lt: &twenty}},
				{Price: &comparator.Number[float64]{Gt: &hundred}},
			},
		},
	})
	if err != nil {
		t.Fatalf("And-with-Or: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("And-with-Or: got %d, want 2 (p1 + p3)", len(products))
	}

	// The other half of the rule: a single member's OWN fields are AND'd, so
	// this asks for price<20 AND price>100 and must match nothing. Under the
	// pre-fix reading it OR'd them and returned p1 + p3, which is what made
	// the union above compile to an intersection.
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: ids},
			Or: []*models.ProductFilter{
				{Price: &comparator.Number[float64]{Lt: &twenty, Gt: &hundred}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Or-member-conjunction: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("Or([{price<20, price>100}]): got %d, want 0 (unsatisfiable conjunction)", len(products))
	}
}

// TestFilter_OrUnion_SQLite drives the consumer-reported shape end to end: a
// two-member `or` over two DIFFERENT columns, which is the case the pre-fix
// compilation turned into an intersection and so returned nothing.
//
// TestFilter_AndOrComposition_SQLite's Or branches both target `price`, which
// is why that test passed while the union was broken — a lone member holding
// two operators on ONE comparator produced the right SQL by a different route.
// The predicates here live on different columns, so they cannot collapse onto
// a single comparator and the composition has to carry them.
func TestFilter_OrUnion_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UnionCat-sqlite"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(title, sku string, price float64) *models.Product {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: title, SKU: sku, Price: price,
			InStock: omittable.Set(true),
		})
		if err != nil {
			t.Fatalf("Create %s: %v", title, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p
	}
	// alpha matches neither predicate, bravo matches the title one, charlie
	// the price one. alpha is what makes this a union rather than "everything".
	alpha := mk("un-alpha", "UN-A", 500.00)
	bravo := mk("un-bravo", "UN-B", 500.00)
	charlie := mk("un-charlie", "UN-C", 1.00)
	ids := []int64{alpha.ID, bravo.ID, charlie.ID}

	bravoTitle := "un-bravo"
	cheap := 10.00

	titlesOf := func(ps []*models.Product) []string {
		out := make([]string, 0, len(ps))
		for _, p := range ps {
			out = append(out, p.Title)
		}
		slices.Sort(out)
		return out
	}

	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: ids},
			Or: []*models.ProductFilter{
				{Title: &comparator.String{Eq: &bravoTitle}},
				{Price: &comparator.Number[float64]{Lt: &cheap}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Or union: %v", err)
	}
	if diff := cmp.Diff([]string{"un-bravo", "un-charlie"}, titlesOf(products)); diff != "" {
		t.Errorf("Or([title=bravo, price<10]) (-want +got):\n%s", diff)
	}

	// And nested over Or: (title=bravo OR price<10) AND price<10 ⇒ charlie
	// alone. Recursion has to carry the disjunction down a level intact; if
	// the inner Or collapsed to a conjunction the result would be empty.
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.Number[int64]{In: ids},
			And: []*models.ProductFilter{
				{Or: []*models.ProductFilter{
					{Title: &comparator.String{Eq: &bravoTitle}},
					{Price: &comparator.Number[float64]{Lt: &cheap}},
				}},
				{Price: &comparator.Number[float64]{Lt: &cheap}},
			},
		},
	})
	if err != nil {
		t.Fatalf("And-over-Or: %v", err)
	}
	if diff := cmp.Diff([]string{"un-charlie"}, titlesOf(products)); diff != "" {
		t.Errorf("And([Or([title=bravo, price<10]), price<10]) (-want +got):\n%s", diff)
	}
}

func TestFilter_ByteIdenticalSQL_SQLite(t *testing.T) {
	ctx := context.Background()
	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ByteIdentCat-sqlite-14.5"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	buildFilter := func() *models.ProductFilter {
		catID := cat.ID
		min := 10.00
		max := 100.00
		inStock := true
		title := "%test%"
		return &models.ProductFilter{
			CategoryID: &comparator.Number[int64]{Eq: &catID},
			InStock:    &comparator.Bool{Eq: &inStock},
			Title:      &comparator.String{Like: &title},
			Price: &comparator.Number[float64]{
				Between: &comparator.Range[float64]{Start: min, End: max},
			},
		}
	}

	cap.reset()
	if _, err := client.Products().GetMany(ctx, &models.GetProductsInput{Filter: buildFilter()}); err != nil {
		t.Fatalf("GetMany #1: %v", err)
	}
	first := cap.lastSelect(t)

	cap.reset()
	if _, err := client.Products().GetMany(ctx, &models.GetProductsInput{Filter: buildFilter()}); err != nil {
		t.Fatalf("GetMany #2: %v", err)
	}
	second := cap.lastSelect(t)

	if first != second {
		t.Errorf("byte-identical SQL regression:\n first: %q\nsecond: %q", first, second)
	}

	if got := strings.Count(first, "?"); got < 4 {
		t.Errorf("captured SQL has %d `?` placeholders; want >= 4: %q", got, first)
	}
}
