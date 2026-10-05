package tests

// Filter-operator breadth for MySQL.
//
// Scope notes (mirror postgres/tests/filter_test.go):
//   - ILIKE / array / PostgreSQL JSONB operators do not exist on MySQL; JSON
//     containment is exercised via the Custom escape hatch with JSON_CONTAINS
//     (PRD §11.2 JSON comparator table).
//   - SQL round-trip byte-equality wraps the database.Querier with a local
//     capturingQuerier and compares the lastSelect captured across two GetMany
//     invocations with identical filter values. MySQL placeholders are `?`
//     (not $1, $2, ...) per PRD §2 dialect-portability contract.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// capturingQuerier wraps a database.Querier and records the SQL text of every
// Query/QueryRow/Exec call.
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

// --- Filter-operator breadth tests ---

func TestFilter_Like_WildcardAndEscape_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "LikePatternCat-mysql"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
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
			CategoryID: cat.ID,
			Title:      name,
			Price:      25.00,
			SKU:        "LIKE-MYSQL-" + strings.Repeat("x", i+1),
			Attributes: mustJSON(map[string]any{"i": i}),
		})
		if err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
		ids = append(ids, p.ID)
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
	}

	// LIKE %cotton% — one match.
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
		t.Errorf("Like '%%cotton%%': got %d products, want 1", len(products))
	}

	// LIKE premium_widget — single-char wildcard matches literal underscore in the row.
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
	if len(products) != 1 || products[0].Title != "premium_widget" {
		t.Errorf("Like 'premium_widget': got %d products, want 1", len(products))
	}

	// NLike
	nlikePattern := "%cotton%"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.Number[int64]{In: ids},
			Title: &comparator.String{NLike: &nlikePattern},
		},
	})
	if err != nil {
		t.Fatalf("NLike cotton: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("NLike: got %d, want 3", len(products))
	}
}

func TestFilter_NullAndNotNull_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-null-mysql-14.5"
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

func TestFilter_Between_Numeric_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "BetweenNumCat-mysql"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	prices := []float64{10.00, 50.00, 100.00, 200.00}
	ids := make([]int64, len(prices))
	for i, price := range prices {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: "between-p", Price: price,
			SKU: "BETW-MYSQL-" + strings.Repeat("x", i+1), Attributes: types.JSON([]byte(`{}`)),
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

func TestFilter_Between_Timestamp_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-between-ts-mysql-14.5"
	a1, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "ts-1", Author: author,
	})
	if err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a1.ID) })

	lo := a1.CreatedAt.Add(-time.Minute)
	hi := a1.CreatedAt.Add(time.Minute)

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

func TestFilter_Custom_MultiplePlaceholders_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CustomPredCat-mysql"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "c1", Price: 60.00,
		SKU: "CUST-MYSQL-1", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "c2", Price: 20.00,
		SKU: "CUST-MYSQL-2", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	ids := []int64{p1.ID, p2.ID}

	// price > ? AND title LIKE ? — two placeholders threaded through sql.Raw.
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

func TestFilter_AndOrComposition_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ComposeCat-mysql"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(title, sku string, price float64, inStock bool) *models.Product {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: title, SKU: sku, Price: price,
			InStock: omittable.Set(inStock), Attributes: types.JSON([]byte(`{}`)),
		})
		if err != nil {
			t.Fatalf("Create %s: %v", title, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p
	}
	p1 := mk("mc-p1", "MC-1", 10.00, true)
	p2 := mk("mc-p2", "MC-2", 55.00, true)
	p3 := mk("mc-p3", "MC-3", 120.00, true)
	p4 := mk("mc-p4", "MC-4", 60.00, false)
	ids := []int64{p1.ID, p2.ID, p3.ID, p4.ID}

	trueVal := true
	fifty := 50.00
	hundred := 100.00
	twenty := 20.00

	// 3-condition And: in_stock=true AND price>=50 AND title LIKE 'mc-%'.
	titlePattern := "mc-%"
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
	// p2 (55, in_stock) + p3 (120, in_stock) match; p4 excluded (out of stock); p1 excluded (price<50).
	if len(products) != 2 {
		t.Errorf("And: got %d, want 2", len(products))
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

// TestFilter_JSON_Contains_MySQL exercises MySQL's JSON_CONTAINS through the
// dedicated JSON comparator (PRD §11.2 "JSON Comparator"). Contains emits
// `JSON_CONTAINS(attributes, $)` on MySQL / `attributes::jsonb @> $` on
// postgres (the cast is needed because `@>` is jsonb-only). Auto-selection
// makes `products.attributes` (JSON NOT NULL) render as `*comparator.JSON`
// in the generated filter.
func TestFilter_JSON_Contains_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "JSONCat-mysql"})
	if err != nil {
		t.Fatalf("Create cat: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(title, sku string, attrs types.JSON) int64 {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID, Title: title, SKU: sku, Price: 1.00,
			Attributes: attrs,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", title, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p.ID
	}
	p1 := mk("jm-p1", "JM-1", mustJSON(map[string]any{"tier": "gold", "region": "us"}))
	p2 := mk("jm-p2", "JM-2", mustJSON(map[string]any{"tier": "silver", "region": "us"}))
	p3 := mk("jm-p3", "JM-3", mustJSON(map[string]any{"tier": "gold", "region": "eu"}))
	ids := []int64{p1, p2, p3}

	// JSON_CONTAINS(attributes, '{"tier":"gold"}') → p1 + p3.
	goldVal := any(`{"tier":"gold"}`)
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:         &comparator.Number[int64]{In: ids},
			Attributes: &comparator.JSON{Contains: &goldVal},
		},
	})
	if err != nil {
		t.Fatalf("JSON Contains: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Contains tier=gold: got %d, want 2", len(products))
	}

	// HasKey 'region' → JSON_CONTAINS_PATH(attributes, 'one', '$.region').
	// All three rows have "region", so we compose with ID filter to keep the
	// assertion meaningful.
	regionKey := "$.region"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:         &comparator.Number[int64]{In: ids},
			Attributes: &comparator.JSON{HasKey: &regionKey},
		},
	})
	if err != nil {
		t.Fatalf("JSON HasKey: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("HasKey region: got %d, want 3", len(products))
	}
}

// TestFilter_ByteIdenticalSQL_MySQL asserts ToConditions determinism — the same
// filter values produce byte-identical SQL across two invocations. MySQL uses
// `?` placeholders, not `$N`, so the marker check looks for those.
func TestFilter_ByteIdenticalSQL_MySQL(t *testing.T) {
	ctx := context.Background()
	cap := newCapturingQuerier(dbstdlib.New(testDB))
	client := models.New(cap)

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ByteIdentCat-mysql-14.5"})
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
			CategoryID: &comparator.Number[int32]{Eq: &catID},
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

	// MySQL placeholders are `?` — expect at least four of them for the four comparator fields.
	if got := strings.Count(first, "?"); got < 4 {
		t.Errorf("captured SQL has %d `?` placeholders; want >= 4: %q", got, first)
	}
}
