package tests

import (
	"context"
	"errors"
	"strconv"
	"testing"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// The MySQL leg of the view-tenancy regression pin (PRD §29.2.5 / §29.4.1). The tenant
// column here is CHAR(36), so the view predicate is exercised against a
// string-typed tenant rather than uuid — the same uniform-type point the table
// suite in this module makes.

// seedViewArticles creates articles for both tenants so article_stats projects
// rows for each.
func seedViewArticles(t *testing.T, env *tenantEnv, countA, countB int) {
	t.Helper()
	ctx := context.Background()

	for i := range countA {
		if _, err := env.clientA.Articles().Create(ctx, &models.CreateArticleInput{
			Slug:  "a-" + strconv.Itoa(i),
			Title: "A article",
		}); err != nil {
			t.Fatalf("create A article %d: %v", i, err)
		}
	}
	for i := range countB {
		if _, err := env.clientB.Articles().Create(ctx, &models.CreateArticleInput{
			Slug:  "b-" + strconv.Itoa(i),
			Title: "B article",
		}); err != nil {
			t.Fatalf("create B article %d: %v", i, err)
		}
	}
}

// TestViewTenancy_AllReadMethodsScoped is the MySQL isolation assertion across
// all five view read methods.
func TestViewTenancy_AllReadMethodsScoped(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedViewArticles(t, env, 3, 2)

	ctx := context.Background()

	rowsA, err := env.clientA.ArticleStat().GetMany(ctx, &models.GetArticleStatsInput{})
	if err != nil {
		t.Fatalf("GetMany A: %v", err)
	}
	if len(rowsA) != 3 {
		t.Errorf("GetMany A: %d rows, want 3", len(rowsA))
	}
	for _, r := range rowsA {
		if r.WorkspaceID != tenantA {
			t.Errorf("GetMany A returned a row for %v", r.WorkspaceID)
		}
	}

	countB, err := env.clientB.ArticleStat().Count(ctx, nil)
	if err != nil {
		t.Fatalf("Count B: %v", err)
	}
	if countB != 2 {
		t.Errorf("Count B = %d, want 2", countB)
	}

	target := rowsA[0]
	if _, err := env.clientA.ArticleStat().Get(ctx, target.ID); err != nil {
		t.Errorf("Get A on its own row: %v", err)
	}
	if _, err := env.clientB.ArticleStat().Get(ctx, target.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get B on tenant A's row: err = %v, want ErrNotFound", err)
	}

	page, err := env.clientA.ArticleStat().Paginate(ctx, models.PaginateInput[models.ArticleStatFilter]{Limit: 10})
	if err != nil {
		t.Fatalf("Paginate A: %v", err)
	}
	if page.TotalCount != 3 || len(page.Items) != 3 {
		t.Errorf("Paginate A: total = %d, items = %d, want 3/3", page.TotalCount, len(page.Items))
	}

	first := 10
	conn, err := env.clientB.ArticleStat().Connection(ctx, models.ConnectionInput[models.ArticleStatFilter]{First: &first})
	if err != nil {
		t.Fatalf("Connection B: %v", err)
	}
	if conn.TotalCount != 2 || len(conn.Edges) != 2 {
		t.Errorf("Connection B: total = %d, edges = %d, want 2/2", conn.TotalCount, len(conn.Edges))
	}
	for _, e := range conn.Edges {
		if e.Node.WorkspaceID != tenantB {
			t.Errorf("Connection B returned a row for %v", e.Node.WorkspaceID)
		}
	}
}

// TestViewTenancy_SkipTenancyAndExplicitTenant pins §29.4.4 on the MySQL leg.
func TestViewTenancy_SkipTenancyAndExplicitTenant(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedViewArticles(t, env, 3, 2)

	ctx := context.Background()
	client := env.clientA.ArticleStat()

	rows, err := client.GetMany(ctx, &models.GetArticleStatsInput{}, func(o *models.CallOptions[models.ArticleStatFieldOptions]) {
		o.SkipTenancy = true
	})
	if err != nil {
		t.Fatalf("GetMany with SkipTenancy: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("SkipTenancy: %d rows, want 5", len(rows))
	}

	rows, err = client.GetMany(ctx, &models.GetArticleStatsInput{}, func(o *models.CallOptions[models.ArticleStatFieldOptions]) {
		o.Tenant = new(tenantB)
	})
	if err != nil {
		t.Fatalf("GetMany with explicit tenant: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("explicit tenant B: %d rows, want 2", len(rows))
	}
}

// TestViewTenancy_RequiredFailsClosed pins §29.3.1 on the MySQL view read path.
func TestViewTenancy_RequiredFailsClosed(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedViewArticles(t, env, 2, 1)

	ctx := context.Background()
	client := models.New(dbstdlib.New(testDB)).ArticleStat()

	if _, err := client.GetMany(ctx, &models.GetArticleStatsInput{}); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("GetMany: err = %v, want tenancy.ErrMissing", err)
	}
	if _, err := client.Get(ctx, 1); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Get: err = %v, want tenancy.ErrMissing", err)
	}
	if _, err := client.Count(ctx, nil); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Count: err = %v, want tenancy.ErrMissing", err)
	}
	if _, err := client.Paginate(ctx, models.PaginateInput[models.ArticleStatFilter]{Limit: 10}); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Paginate: err = %v, want tenancy.ErrMissing", err)
	}
	first := 10
	if _, err := client.Connection(ctx, models.ConnectionInput[models.ArticleStatFilter]{First: &first}); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Connection: err = %v, want tenancy.ErrMissing", err)
	}
}
