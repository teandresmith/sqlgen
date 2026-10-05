package tests

// Scope notes:
//   - ILike is not a first-class String operator in comparator.String (PRD §11.2 —
//     String exposes Eq/Neq/Contains/StartsWith/EndsWith/Like/NLike/In/Nin/Gt/Gte/
//     Lt/Lte/Custom). Case-insensitive PostgreSQL matching is exercised via the
//     Custom field with a raw sql.Raw("col ILIKE $", ...) condition — the
//     documented escape hatch per §11.2 and §11.4.
//   - JSONB column filters in the generated filter struct are represented as
//     comparator.NullableString (product.metadata, warehouses.metadata etc.) —
//     that is the landed codegen behavior. @> / containment filters are therefore
//     exercised via the Custom field (raw SQL with explicit ::jsonb cast) rather
//     than through a dedicated JSONB comparator field. This matches the §11.4
//     escape-hatch contract and is consistent with how the tenancy example tests
//     jsonb probes.
//   - SQL round-trip byte-equality is asserted by wrapping the underlying
//     database.Querier with a capturingQuerier, invoking GetMany twice with the
//     same filter-value set, and comparing the captured SELECT strings byte-wise.
//     If the generator's placeholder numbering or clause ordering ever becomes
//     non-deterministic this test will surface it as a byte diff (PRD §11.1
//     ToConditions deterministic contract).

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- Filtering ---

func TestFilterComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	u1, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter1@example.com", Name: "Filter1", Role: omittable.Set(models.UserRoleAdmin),
	})
	u2, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter2@example.com", Name: "Filter2", Role: omittable.Set(models.UserRoleViewer),
	})
	u3, _ := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "filter3@example.com", Name: "AnotherFilter", Role: omittable.Set(models.UserRoleEditor),
	})
	t.Cleanup(func() {
		_ = client.PublicUsers().HardDelete(ctx, u1.ID)
		_ = client.PublicUsers().HardDelete(ctx, u2.ID)
		_ = client.PublicUsers().HardDelete(ctx, u3.ID)
	})

	// String equality
	name := "Filter1"
	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Name: &comparator.String{Eq: &name},
		},
	})
	if err != nil {
		t.Fatalf("Filter by name: %v", err)
	}
	if len(users) != 1 || users[0].ID != u1.ID {
		t.Errorf("Filter name=Filter1: got %d users", len(users))
	}

	// Enum filter
	adminRole := models.UserRoleAdmin
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Role: &comparator.Enum[models.UserRole]{Eq: &adminRole},
		},
	})
	if err != nil {
		t.Fatalf("Filter by role: %v", err)
	}
	found := false
	for _, u := range users {
		if u.ID == u1.ID {
			found = true
		}
	}
	if !found {
		t.Error("Filter role=admin should include u1")
	}

	// String contains (LIKE)
	contains := "Filter"
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Name: &comparator.String{Contains: &contains},
		},
	})
	if err != nil {
		t.Fatalf("Filter by name contains: %v", err)
	}
	if len(users) < 3 {
		t.Errorf("Filter name contains 'Filter': got %d users, want >= 3", len(users))
	}

	// Timestamptz filter (Gte — users created after a past date)
	pastTime := u1.CreatedAt.Add(-time.Minute)
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID:        &comparator.ID{In: []string{u1.ID.String(), u2.ID.String(), u3.ID.String()}},
			CreatedAt: &comparator.Time{Gte: &pastTime},
		},
	})
	if err != nil {
		t.Fatalf("Filter by created_at gte: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("Filter created_at >= past: got %d users, want 3", len(users))
	}

	// Timestamptz filter (Lt — no users created before a past date)
	users, err = client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			ID:        &comparator.ID{In: []string{u1.ID.String(), u2.ID.String(), u3.ID.String()}},
			CreatedAt: &comparator.Time{Lt: &pastTime},
		},
	})
	if err != nil {
		t.Fatalf("Filter by created_at lt: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("Filter created_at < past: got %d users, want 0", len(users))
	}
}

func TestFilterNumericAndIntegerComparators(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "FilterNumCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Cheap",
		Price:      10.00,
		Quantity:   omittable.Set[int32](5),
		IsActive:   omittable.Set(true),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Medium",
		Price:      50.00,
		Quantity:   omittable.Set[int32](10),
		IsActive:   omittable.Set(true),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	p3, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "Expensive",
		Price:      200.00,
		Quantity:   omittable.Set[int32](2),
		IsActive:   omittable.Set(false),
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product 3: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p3.ID) })

	productIDs := []uuid.UUID{p1.ID, p2.ID, p3.ID}

	// Numeric (float64) — Gte filter on price
	minPrice := 50.00
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(productIDs)},
			Price: &comparator.Number[float64]{Gte: &minPrice},
		},
	})
	if err != nil {
		t.Fatalf("Filter by price gte: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter price >= 50: got %d products, want 2", len(products))
	}

	// Numeric (float64) — Lt filter on price
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(productIDs)},
			Price: &comparator.Number[float64]{Lt: &minPrice},
		},
	})
	if err != nil {
		t.Fatalf("Filter by price lt: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1.ID {
		t.Errorf("Filter price < 50: got %d products, want 1 (Cheap)", len(products))
	}

	// Integer (int32) — Gt filter on quantity
	minQty := int32(4)
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			Quantity: &comparator.Number[int32]{Gt: &minQty},
		},
	})
	if err != nil {
		t.Fatalf("Filter by quantity gt: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter quantity > 4: got %d products, want 2", len(products))
	}

	// Integer (int32) — Eq filter on quantity
	exactQty := int32(10)
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			Quantity: &comparator.Number[int32]{Eq: &exactQty},
		},
	})
	if err != nil {
		t.Fatalf("Filter by quantity eq: %v", err)
	}
	if len(products) != 1 || products[0].ID != p2.ID {
		t.Errorf("Filter quantity == 10: got %d products, want 1 (Medium)", len(products))
	}

	// Boolean — Eq filter on is_active (true)
	active := true
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			IsActive: &comparator.Bool{Eq: &active},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active true: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Filter is_active=true: got %d products, want 2", len(products))
	}

	// Boolean — Eq filter on is_active (false)
	inactive := false
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(productIDs)},
			IsActive: &comparator.Bool{Eq: &inactive},
		},
	})
	if err != nil {
		t.Fatalf("Filter by is_active false: %v", err)
	}
	if len(products) != 1 || products[0].ID != p3.ID {
		t.Errorf("Filter is_active=false: got %d products, want 1 (Expensive)", len(products))
	}
}

// --- Operator-breadth coverage ---

// capturingQuerier wraps a database.Querier and records the SQL text of every
// Query/QueryRow/Exec call. Tests use it to assert that identical filter values
// produce byte-identical SQL across repeated invocations (the ToConditions
// determinism contract per PRD §11.1).
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

// lastSelect returns the last captured SQL string that starts with "SELECT"
// (case-insensitive). Filter-driven operations may trigger auxiliary queries
// (relationship loads, count queries) — the primary SELECT is the one tests
// want to compare.
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

// seedLikePatternProducts creates three products with names exposing LIKE
// wildcards (%, _) plus one with a case-variant for the ILike test.
func seedLikePatternProducts(t *testing.T, ctx context.Context, client *models.Client, category string) (catID uuid.UUID, ids []uuid.UUID) {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: category})
	if err != nil {
		t.Fatalf("Create category %q: %v", category, err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	names := []string{
		"100% cotton shirt",  // contains literal % — LIKE pattern must escape it
		"USB-A cable",        // contains literal _ via hyphen — baseline
		"premium_widget",     // contains literal _
		"LOUDWIDGET SPECIAL", // uppercase for ILIKE test
	}
	ids = make([]uuid.UUID, 0, len(names))
	for _, name := range names {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       name,
			Price:      25.00,
			Quantity:   omittable.Set[int32](1),
			IsActive:   omittable.Set(true),
			CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("Create product %q: %v", name, err)
		}
		ids = append(ids, p.ID)
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
	}
	return cat.ID, ids
}

// TestFilter_Like_WildcardAndEscape_Postgres exercises comparator.String.Like
// with both % and _ wildcards and confirms the SQL LIKE predicate is threaded
// through unchanged (no wrapper wildcards added by the comparator — those are
// Contains/StartsWith/EndsWith's job).
func TestFilter_Like_WildcardAndEscape_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	_, ids := seedLikePatternProducts(t, ctx, client, "LikePatternCat")

	// Literal "% cotton" match — Like uses caller-provided wildcards verbatim.
	pattern := "%cotton%"
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Name: &comparator.String{Like: &pattern},
		},
	})
	if err != nil {
		t.Fatalf("Like cotton: %v", err)
	}
	if len(products) != 1 || !strings.Contains(products[0].Name, "cotton") {
		t.Errorf("Like '%%cotton%%': got %d products, want 1 (cotton shirt)", len(products))
	}

	// Single-char wildcard "_" — matches one character position.
	// "premium_widget" matches "premium_widget"; "USB-A cable" does not.
	underscorePattern := "premium_widget"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Name: &comparator.String{Like: &underscorePattern},
		},
	})
	if err != nil {
		t.Fatalf("Like underscore: %v", err)
	}
	if len(products) != 1 || products[0].Name != "premium_widget" {
		t.Errorf("Like 'premium_widget': got %d products, want 1", len(products))
	}

	// NLike — negation.
	nlikePattern := "%cotton%"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Name: &comparator.String{NLike: &nlikePattern},
		},
	})
	if err != nil {
		t.Fatalf("NLike cotton: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("NLike '%%cotton%%': got %d products, want 3", len(products))
	}
}

// TestFilter_ILike_Custom_Postgres exercises the PostgreSQL-only ILIKE operator
// via the Custom escape hatch (PRD §11.4). Verifies case-insensitive matching.
func TestFilter_ILike_Custom_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	_, ids := seedLikePatternProducts(t, ctx, client, "ILikePatternCat")

	// ILIKE '%widget%' matches both "premium_widget" and "LOUDWIDGET SPECIAL".
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
			Name: &comparator.String{
				Custom: []sql.Condition{sql.Raw("name ILIKE $", "%widget%")},
			},
		},
	})
	if err != nil {
		t.Fatalf("ILike custom: %v", err)
	}
	if len(products) != 2 {
		names := make([]string, len(products))
		for i, p := range products {
			names[i] = p.Name
		}
		t.Errorf("ILike '%%widget%%': got %d products, want 2 (premium_widget + LOUDWIDGET SPECIAL); names=%v", len(products), names)
	}
}

// TestFilter_NullAndNotNull_Postgres exercises the Null operator on a nullable
// column (articles.body) — both Null=true (IS NULL) and Null=false (IS NOT NULL).
func TestFilter_NullAndNotNull_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-null-14.5"
	nullArticle, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Null body article",
		Author: author,
	})
	if err != nil {
		t.Fatalf("Create null article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, nullArticle.ID) })

	bodyText := "has body"
	withBodyArticle, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "With-body article",
		Author: author,
		Body:   omittable.Set(&bodyText),
	})
	if err != nil {
		t.Fatalf("Create body article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, withBodyArticle.ID) })

	// IS NULL
	isNull := true
	articles, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author: &comparator.String{Eq: &author},
			Body:   &comparator.NullableString{Null: &isNull},
		},
	})
	if err != nil {
		t.Fatalf("Filter body IS NULL: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != nullArticle.ID {
		t.Errorf("Filter body IS NULL: got %d articles, want 1 (null article)", len(articles))
	}

	// IS NOT NULL
	isNotNull := false
	articles, err = client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author: &comparator.String{Eq: &author},
			Body:   &comparator.NullableString{Null: &isNotNull},
		},
	})
	if err != nil {
		t.Fatalf("Filter body IS NOT NULL: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != withBodyArticle.ID {
		t.Errorf("Filter body IS NOT NULL: got %d articles, want 1 (with-body article)", len(articles))
	}
}

// TestFilter_Between_Numeric_Postgres exercises Number[T].Between on products.price
// (NUMERIC(10,2)) — inclusive lower + inclusive upper bound.
func TestFilter_Between_Numeric_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "BetweenNumCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	prices := []float64{10.00, 50.00, 100.00, 200.00}
	ids := make([]uuid.UUID, len(prices))
	for i, price := range prices {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       "between-p",
			Price:      price,
			Quantity:   omittable.Set[int32](1),
			IsActive:   omittable.Set(true),
			CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("Create product %d: %v", i, err)
		}
		ids[i] = p.ID
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
	}

	// Between [50, 100] — both endpoints inclusive. Matches price=50.00 and 100.00.
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(ids)},
			Price: &comparator.Number[float64]{Between: &comparator.Range[float64]{Start: 50.00, End: 100.00}},
		},
	})
	if err != nil {
		t.Fatalf("Between 50..100: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Between [50, 100]: got %d, want 2", len(products))
	}

	// NBetween (50, 100) exclusive-of-range → matches 10 and 200.
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:    &comparator.ID{In: idStrings(ids)},
			Price: &comparator.Number[float64]{NBetween: &comparator.Range[float64]{Start: 50.00, End: 100.00}},
		},
	})
	if err != nil {
		t.Fatalf("NBetween 50..100: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("NBetween [50, 100]: got %d, want 2 (10 + 200)", len(products))
	}
}

// TestFilter_Between_Timestamp_Postgres exercises Time.Between on a timestamptz
// column (articles.created_at).
func TestFilter_Between_Timestamp_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := "filter-between-ts-14.5"
	a1, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "between-ts-1", Author: author,
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
		t.Fatalf("Between created_at: %v", err)
	}
	if len(articles) != 1 || articles[0].ID != a1.ID {
		t.Errorf("Between created_at: got %d, want 1", len(articles))
	}
}

// TestFilter_Custom_MultiplePlaceholders_Postgres exercises the Custom
// escape hatch with two positional placeholders threaded through sql.Raw.
// Confirms placeholder numbering is not disrupted by the raw clause.
func TestFilter_Custom_MultiplePlaceholders_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CustomPredCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "c1", Price: 60.00, Quantity: omittable.Set[int32](10),
		IsActive: omittable.Set(true), CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "c2", Price: 20.00, Quantity: omittable.Set[int32](2),
		IsActive: omittable.Set(true), CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	ids := []uuid.UUID{p1.ID, p2.ID}

	// price > 50 AND quantity < 20 — two placeholders, both arguments consumed.
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
			Price: &comparator.Number[float64]{
				Custom: []sql.Condition{sql.Raw("price > $ AND quantity < $", 50.00, int32(20))},
			},
		},
	})
	if err != nil {
		t.Fatalf("Custom raw: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1.ID {
		t.Errorf("Custom price>$50 AND quantity<$20: got %d, want 1 (p1)", len(products))
	}
}

// TestFilter_AndOrComposition_Postgres exercises 3-condition And and nested
// Or-inside-And composition per PRD §11.1 And/Or recursion rules.
func TestFilter_AndOrComposition_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ComposeCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Products:
	//   p1: active=true, price=10,  quantity=1
	//   p2: active=true, price=55,  quantity=8
	//   p3: active=true, price=120, quantity=2
	//   p4: active=false, price=60, quantity=10  (inactive, excluded from outer And)
	mk := func(name string, price float64, qty int32, active bool) *models.Product {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name: name, Price: price, Quantity: omittable.Set(qty),
			IsActive: omittable.Set(active), CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p
	}
	p1 := mk("ac-p1", 10.00, 1, true)
	p2 := mk("ac-p2", 55.00, 8, true)
	p3 := mk("ac-p3", 120.00, 2, true)
	p4 := mk("ac-p4", 60.00, 10, false)
	ids := []uuid.UUID{p1.ID, p2.ID, p3.ID, p4.ID}

	// 3-condition And: is_active=true AND price>=50 AND quantity>=5 → p2 only.
	trueVal := true
	fifty := 50.00
	five := int32(5)
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(ids)},
			IsActive: &comparator.Bool{Eq: &trueVal},
			And: []*models.ProductFilter{
				{Price: &comparator.Number[float64]{Gte: &fifty}},
				{Quantity: &comparator.Number[int32]{Gte: &five}},
			},
		},
	})
	if err != nil {
		t.Fatalf("3-cond And: %v", err)
	}
	if len(products) != 1 || products[0].ID != p2.ID {
		t.Errorf("And(active, price>=50, qty>=5): got %d, want 1 (p2)", len(products))
	}

	// Nested Or inside And: is_active=true AND (price<20 OR price>100) → p1 + p3.
	// One branch per `Or` member, which is the spelling the syntax suggests:
	// members OR together, and each member's own fields AND together
	// (PRD §11.1).
	twenty := 20.00
	hundred := 100.00
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:       &comparator.ID{In: idStrings(ids)},
			IsActive: &comparator.Bool{Eq: &trueVal},
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
		t.Errorf("And(active, Or(price<20, price>100)): got %d, want 2 (p1 + p3)", len(products))
	}
	got := map[uuid.UUID]bool{products[0].ID: true}
	if len(products) > 1 {
		got[products[1].ID] = true
	}
	if !got[p1.ID] || !got[p3.ID] {
		t.Errorf("And-with-Or: missing p1 or p3; got IDs %v", got)
	}

	// The other half of the rule: a single member's OWN fields are AND'd, so
	// this asks for price<20 AND price>100 and must match nothing. Under the
	// pre-fix reading it OR'd them and returned p1 + p3, which is what made
	// the union above compile to an intersection.
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
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

// TestFilter_SliceArrayContains_Postgres exercises comparator.Slice[T] operators
// on products.tags (TEXT[]) — the PostgreSQL-only array comparator per PRD §11.2.
func TestFilter_SliceArrayContains_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "SliceArrayCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Tags via Create aren't exposed through CreateProductInput; seed directly
	// via raw SQL so we control the array contents exactly.
	mk := func(name string, tags string) uuid.UUID {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name: name, Price: 1.00, Quantity: omittable.Set[int32](1),
			IsActive: omittable.Set(true), CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := testPool.Exec(ctx, "UPDATE products SET tags = $1::text[] WHERE id = $2", tags, p.ID); err != nil {
			t.Fatalf("Update tags for %s: %v", name, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p.ID
	}
	p1 := mk("slice-p1", "{red,new,sale}")
	p2 := mk("slice-p2", "{blue,new}")
	p3 := mk("slice-p3", "{green}")
	p4 := mk("slice-p4", "{}")
	ids := []uuid.UUID{p1, p2, p3, p4}

	// ContainsAny — array && array → p1 + p2 carry "new".
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Tags: &comparator.NullableSlice[string]{Slice: comparator.Slice[string]{ContainsAny: []string{"new"}}},
		},
	})
	if err != nil {
		t.Fatalf("ContainsAny: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("Tags && {new}: got %d, want 2", len(products))
	}

	// ContainsAll — array @> array → p1 carries both "red" and "sale".
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Tags: &comparator.NullableSlice[string]{Slice: comparator.Slice[string]{ContainsAll: []string{"red", "sale"}}},
		},
	})
	if err != nil {
		t.Fatalf("ContainsAll: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1 {
		t.Errorf("Tags @> {red,sale}: got %d, want 1 (p1)", len(products))
	}

	// IsEmpty=true → only p4.
	isEmpty := true
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID:   &comparator.ID{In: idStrings(ids)},
			Tags: &comparator.NullableSlice[string]{Slice: comparator.Slice[string]{IsEmpty: &isEmpty}},
		},
	})
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if len(products) != 1 || products[0].ID != p4 {
		t.Errorf("Tags = '{}': got %d, want 1 (p4)", len(products))
	}
}

// TestFilter_JSONB_Contains_Postgres exercises PostgreSQL JSONB operators
// through the dedicated NullableJSONB comparator (PRD §11.2 "PostgreSQL-Only
// Comparators"). Contains emits `metadata @> $`, HasKey emits `metadata ? $`.
// Auto-selection makes `products.metadata` (JSONB NULL) render as
// `*comparator.NullableJSONB` in the generated filter.
func TestFilter_JSONB_Contains_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "JSONBCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(name, meta string) uuid.UUID {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name: name, Price: 1.00, Quantity: omittable.Set[int32](1),
			IsActive: omittable.Set(true), CategoryID: cat.ID,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		// Keep the explicit ::jsonb cast — seed `meta` is a plain string and
		// postgres does not auto-coerce text → jsonb on assignment.
		if _, err := testPool.Exec(ctx, "UPDATE products SET metadata = $1::jsonb WHERE id = $2", meta, p.ID); err != nil {
			t.Fatalf("Update metadata for %s: %v", name, err)
		}
		pid := p.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, pid) })
		return p.ID
	}
	p1 := mk("jb-p1", `{"tier":"gold","region":"us"}`)
	p2 := mk("jb-p2", `{"tier":"silver","region":"us"}`)
	p3 := mk("jb-p3", `{"tier":"gold","region":"eu"}`)
	ids := []uuid.UUID{p1, p2, p3}

	// metadata @> '{"tier":"gold"}' → p1 + p3.
	goldVal := any(`{"tier":"gold"}`)
	products, err := client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
			Metadata: &comparator.NullableJSONB{
				JSONB: comparator.JSONB{Contains: &goldVal},
			},
		},
	})
	if err != nil {
		t.Fatalf("JSONB Contains: %v", err)
	}
	if len(products) != 2 {
		t.Errorf("metadata @> gold: got %d, want 2", len(products))
	}

	// HasKey 'tier' → all three, but region alone narrows to the two with region=us.
	// Compose via AND on Contains + Contains to pin down p1.
	usVal := any(`{"region":"us"}`)
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
			Metadata: &comparator.NullableJSONB{
				JSONB: comparator.JSONB{
					// JSONB exposes Contains/HasKey/HasAllKeys/ContainedBy/PathExists
					// as individual fields — all are AND'd inside Parse. Setting
					// Contains on the caller side covers the @> predicate, and we
					// layer a second @> via Custom to exercise the compound path.
					Contains: &goldVal,
					Custom:   []sql.Condition{sql.Raw(`metadata @> $`, usVal)},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("JSONB compound: %v", err)
	}
	if len(products) != 1 || products[0].ID != p1 {
		t.Errorf("metadata @> gold + @> us: got %d, want 1 (p1)", len(products))
	}

	// HasKey: every row has "tier" — returns all three.
	tierKey := "tier"
	products, err = client.Products().GetMany(ctx, &models.GetProductsInput{
		Filter: &models.ProductFilter{
			ID: &comparator.ID{In: idStrings(ids)},
			Metadata: &comparator.NullableJSONB{
				JSONB: comparator.JSONB{HasKey: &tierKey},
			},
		},
	})
	if err != nil {
		t.Fatalf("JSONB HasKey: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("metadata ? 'tier': got %d, want 3", len(products))
	}
}

// TestFilter_ByteIdenticalSQL_Postgres asserts that two GetMany invocations
// with the same filter-value set produce byte-identical SELECT SQL. Regression
// guard against non-determinism in ToConditions or placeholder numbering
// (PRD §11.1).
func TestFilter_ByteIdenticalSQL_Postgres(t *testing.T) {
	ctx := context.Background()
	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	// Seed a minimal row so the query has something to scan (the SQL is what's
	// being compared, not the result set).
	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ByteIdentCat-14.5"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Identical filter values across two invocations.
	buildFilter := func() *models.ProductFilter {
		catID := cat.ID
		min := 10.00
		max := 100.00
		active := true
		name := "%test%"
		return &models.ProductFilter{
			CategoryID: &comparator.ID{Eq: new(catID.String())},
			IsActive:   &comparator.Bool{Eq: &active},
			Name:       &comparator.String{Like: &name},
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

	// Sanity-check that the SQL actually contains placeholders in ascending
	// order — a classic non-determinism failure mode (e.g., $1 $2 $3 vs $1 $3 $2).
	for i := 1; i <= 4; i++ {
		marker := "$" + strconv.Itoa(i)
		if !strings.Contains(first, marker) {
			t.Errorf("captured SQL missing placeholder %s: %q", marker, first)
		}
	}
}

// TestFilter_EnumSetOperators_Postgres pins the Go-client half of the pgx
// enum-array encode fix. The set operators USED to take the
// array-parameter path on pgx (`role = ANY($1)` / `role != ALL($1)`), and
// handing pgx a slice of the NAMED enum type failed the query outright:
//
//	unable to encode []models.UserRole{…} into text format
//	for unknown type (OID …): cannot find encode plan
//
// `Eq` was unaffected — a scalar named string goes through pgx's string-kind
// fallback — which is why every existing enum filter test passed while In and
// Nin were unreachable. `comparator.Enum` now expands to `IN ($, $, …)` on
// every dialect, asking the driver for nothing beyond what `Eq` already
// proved it can do.
//
// Reached from the GraphQL surface too (`<Enum>Comparator` projects
// `in` / `nin`), but the defect is the Go client's: this test uses no GraphQL.
func TestFilter_EnumSetOperators_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seeded := map[string]uuid.UUID{}
	for _, u := range []struct {
		name string
		role models.UserRole
	}{
		{"EnumSetAdmin", models.UserRoleAdmin},
		{"EnumSetEditor", models.UserRoleEditor},
		{"EnumSetViewer", models.UserRoleViewer},
	} {
		created, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
			Email: strings.ToLower(u.name) + "@example.com",
			Name:  u.name,
			Role:  omittable.Set(u.role),
		})
		if err != nil {
			t.Fatalf("seeding %s: %v", u.name, err)
		}
		seeded[u.name] = created.ID
		t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, created.ID) })
	}

	names := func(t *testing.T, filter *models.PublicUserFilter) []string {
		t.Helper()
		filter.Name = &comparator.String{StartsWith: new("EnumSet")}
		users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{Filter: filter})
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		out := make([]string, 0, len(users))
		for _, u := range users {
			out = append(out, u.Name)
		}
		slices.Sort(out)
		return out
	}

	t.Run("In", func(t *testing.T) {
		got := names(t, &models.PublicUserFilter{
			Role: &comparator.Enum[models.UserRole]{
				In: []models.UserRole{models.UserRoleAdmin, models.UserRoleViewer},
			},
		})
		want := []string{"EnumSetAdmin", "EnumSetViewer"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("In mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Nin", func(t *testing.T) {
		got := names(t, &models.PublicUserFilter{
			Role: &comparator.Enum[models.UserRole]{
				Nin: []models.UserRole{models.UserRoleAdmin, models.UserRoleViewer},
			},
		})
		want := []string{"EnumSetEditor"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Nin mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestFilter_SliceArrayLiteralQuoting_Postgres pins array-literal quoting
// against a real PostgreSQL array column. `comparator.Slice` builds the array
// literal by hand instead of handing the driver a typed slice — deliberate,
// since it is what lets an ENUM array travel without OID registration — so it
// owns PostgreSQL's array-literal grammar.
//
// Before the fix, three of these five values were broken and they failed in
// two different ways:
//
//	"with,comma"  -> split into two elements server-side; 0 rows, NO error
//	`with"quote`  -> ERROR: malformed array literal: "{with"quote}"
//	"with{brace}" -> ERROR: malformed array literal: "{with{brace}}"
//
// The comma case is the dangerous one: a filter that silently matches a
// different set is worse than one that errors. Enum members are identifiers
// and never needed quoting, which is why the defect survived — but this
// column is `text[]`, and the GraphQL surface exposes exactly that, where
// the values are whatever a client sends.
func TestFilter_SliceArrayLiteralQuoting_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "SliceQuotingCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "SliceQuotingProduct", Price: 1.00, Quantity: omittable.Set[int32](1),
		IsActive: omittable.Set(true), CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	// Tags aren't exposed through CreateProductInput; seed via the driver so
	// the stored values are exactly these bytes.
	tags := []string{"plain", "with space", "with,comma", `with"quote`, "with{brace}", ""}
	if _, err := testPool.Exec(ctx, "UPDATE products SET tags = $1 WHERE id = $2", tags, p.ID); err != nil {
		t.Fatalf("seed tags: %v", err)
	}

	for _, tag := range tags {
		t.Run("ContainsAny "+strconv.Quote(tag), func(t *testing.T) {
			got, err := client.Products().GetMany(ctx, &models.GetProductsInput{
				Filter: &models.ProductFilter{
					ID:   &comparator.ID{Eq: new(p.ID.String())},
					Tags: &comparator.NullableSlice[string]{Slice: comparator.Slice[string]{ContainsAny: []string{tag}}},
				},
			})
			if err != nil {
				t.Fatalf("GetMany: %v", err)
			}
			if len(got) != 1 {
				t.Errorf("tag %q matched %d rows, want 1 — the stored array contains it", tag, len(got))
			}
		})
	}

	// A value NOT in the array must still not match: quoting has to preserve
	// the predicate, not merely stop it erroring.
	t.Run("absent value does not match", func(t *testing.T) {
		got, err := client.Products().GetMany(ctx, &models.GetProductsInput{
			Filter: &models.ProductFilter{
				ID:   &comparator.ID{Eq: new(p.ID.String())},
				Tags: &comparator.NullableSlice[string]{Slice: comparator.Slice[string]{ContainsAny: []string{"absent,value"}}},
			},
		})
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("absent value matched %d rows, want 0", len(got))
		}
	})
}
