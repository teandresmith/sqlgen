package tests

import (
	"context"
	"errors"
	"testing"

	sqlgenpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// The postgres leg of the view-tenancy regression pin (PRD §29.2.5 / §29.4.1): a
// tenanted view's five read methods return only the resolver's tenant, and a
// tenanted materialized view refreshes unscoped (§29.2.5) while its reads
// stay scoped.

// seedArticles creates n articles for each tenant through the tenanted table
// clients, which is what article_stats and workspace_line_totals project.
func seedArticles(t *testing.T, env *tenantEnv, countA, countB int) {
	t.Helper()
	ctx := context.Background()

	for i := range countA {
		if _, err := env.clientA.Articles().Create(ctx, &models.CreateArticleInput{Title: "A article"}); err != nil {
			t.Fatalf("create A article %d: %v", i, err)
		}
	}
	for i := range countB {
		if _, err := env.clientB.Articles().Create(ctx, &models.CreateArticleInput{Title: "B article"}); err != nil {
			t.Fatalf("create B article %d: %v", i, err)
		}
	}
}

// TestViewTenancy_AllReadMethodsScoped is the cross-dialect isolation
// assertion on postgres: two tenants' rows are visible through one view and
// every read method returns only the resolver's.
func TestViewTenancy_AllReadMethodsScoped(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedArticles(t, env, 3, 2)

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

	// Get is scoped: A's row is ErrNotFound for B.
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

// TestViewTenancy_SkipTenancyAndExplicitTenant pins §29.4.4 on the postgres
// leg: SkipTenancy widens the read to both tenants, an explicit Tenant scopes
// it to the named one, and explicit wins when both are set.
func TestViewTenancy_SkipTenancyAndExplicitTenant(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedArticles(t, env, 3, 2)

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

	rows, err = client.GetMany(ctx, &models.GetArticleStatsInput{}, func(o *models.CallOptions[models.ArticleStatFieldOptions]) {
		o.SkipTenancy = true
		o.Tenant = new(tenantB)
	})
	if err != nil {
		t.Fatalf("GetMany with both: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("explicit + SkipTenancy: %d rows, want 2 (explicit wins)", len(rows))
	}
}

// TestViewTenancy_RequiredFailsClosed pins §29.3.1 on the view read path: this
// example runs tenancy.required:true, so a client with no resolver fails every
// view read before the query.
func TestViewTenancy_RequiredFailsClosed(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedArticles(t, env, 2, 1)

	ctx := context.Background()
	client := models.New(sqlgenpgx.New(testPool)).ArticleStat()

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

// TestViewTenancy_CursorPaginationStaysScoped walks a multi-page tenanted view
// with the keyset cursor. The tenant predicate is appended before the keyset
// conditions, so a placeholder-numbering regression would surface as a
// repeated row, a skipped row, or a row belonging to the other tenant.
func TestViewTenancy_CursorPaginationStaysScoped(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	seedArticles(t, env, 5, 4)

	ctx := context.Background()
	pageSize := 2

	seen := make(map[int64]int)
	var cursor *string
	for page := range 10 {
		conn, err := env.clientA.ArticleStat().Connection(ctx, models.ConnectionInput[models.ArticleStatFilter]{
			First: &pageSize,
			After: cursor,
		})
		if err != nil {
			t.Fatalf("Connection page %d: %v", page, err)
		}
		if conn.TotalCount != 5 {
			t.Errorf("page %d: total = %d, want 5", page, conn.TotalCount)
		}
		for _, e := range conn.Edges {
			if e.Node.WorkspaceID != tenantA {
				t.Errorf("page %d: row for %v leaked into tenant A's pages", page, e.Node.WorkspaceID)
			}
			seen[e.Node.ID]++
		}
		if !conn.PageInfo.HasNextPage {
			break
		}
		cursor = conn.PageInfo.EndCursor
	}

	if len(seen) != 5 {
		t.Errorf("walked %d distinct rows, want 5", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %d returned %d times, want exactly once", id, n)
		}
	}
}

// TestViewTenancy_MatviewRefreshIsUnscoped pins §29.2.5: REFRESH
// MATERIALIZED VIEW recomputes the whole relation regardless of the resolver's
// tenant, while reads of the refreshed matview stay tenant-scoped.
func TestViewTenancy_MatviewRefreshIsUnscoped(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	ctx := context.Background()

	seedLineItems := func(client *models.Client, orderID int64, n int) {
		t.Helper()
		for i := range n {
			if _, err := client.LineItems().Create(ctx, &models.CreateLineItemInput{
				OrderID:   orderID,
				ProductID: int64(i + 1),
				Quantity:  1,
			}); err != nil {
				t.Fatalf("create line item: %v", err)
			}
		}
	}
	seedLineItems(env.clientA, 1, 3)
	seedLineItems(env.clientB, 2, 2)

	// A's client issues the refresh; it must recompute both tenants' rows.
	if err := env.clientA.WorkspaceLineTotal().Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	rowsA, err := env.clientA.WorkspaceLineTotal().GetMany(ctx, &models.GetWorkspaceLineTotalsInput{})
	if err != nil {
		t.Fatalf("GetMany A after refresh: %v", err)
	}
	if len(rowsA) != 1 || rowsA[0].WorkspaceID != tenantA || rowsA[0].LineCount != 3 {
		t.Errorf("GetMany A after refresh = %+v, want one row for tenant A with line_count 3", rowsA)
	}

	// B's row was recomputed by A's unscoped refresh — the proof the refresh
	// itself carried no tenant predicate.
	rowsB, err := env.clientB.WorkspaceLineTotal().GetMany(ctx, &models.GetWorkspaceLineTotalsInput{})
	if err != nil {
		t.Fatalf("GetMany B after refresh: %v", err)
	}
	if len(rowsB) != 1 || rowsB[0].WorkspaceID != tenantB || rowsB[0].LineCount != 2 {
		t.Errorf("GetMany B after refresh = %+v, want one row for tenant B with line_count 2", rowsB)
	}

	// RefreshConcurrently takes the same unscoped path.
	seedLineItems(env.clientB, 3, 1)
	if err := env.clientA.WorkspaceLineTotal().RefreshConcurrently(ctx); err != nil {
		t.Fatalf("RefreshConcurrently: %v", err)
	}
	rowsB, err = env.clientB.WorkspaceLineTotal().GetMany(ctx, &models.GetWorkspaceLineTotalsInput{})
	if err != nil {
		t.Fatalf("GetMany B after concurrent refresh: %v", err)
	}
	if len(rowsB) != 1 || rowsB[0].LineCount != 3 {
		t.Errorf("GetMany B after concurrent refresh = %+v, want line_count 3", rowsB)
	}
}

// TestViewTenancy_MatviewPKTenantColumnIsPlainFiltered covers the shape that
// motivated the §30.4.2 delta: workspace_line_totals carries @pk on the tenant
// column. @pk selects a Get signature, it does not make the column a DDL
// primary key, so the read is plain-filtered — a Get for another tenant's key
// returns ErrNotFound rather than a mismatch error.
func TestViewTenancy_MatviewPKTenantColumnIsPlainFiltered(t *testing.T) {
	resetDB(t)

	env := newTenantEnv(t)
	ctx := context.Background()

	if _, err := env.clientA.LineItems().Create(ctx, &models.CreateLineItemInput{
		OrderID: 1, ProductID: 1, Quantity: 1,
	}); err != nil {
		t.Fatalf("create line item: %v", err)
	}
	if err := env.clientA.WorkspaceLineTotal().Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, err := env.clientA.WorkspaceLineTotal().Get(ctx, tenantA)
	if err != nil {
		t.Fatalf("Get A on its own key: %v", err)
	}
	if got.WorkspaceID != tenantA {
		t.Errorf("Get A: workspace_id = %v, want %v", got.WorkspaceID, tenantA)
	}

	// B asks for A's key. The predicate ANDs with the PK filter, so the row is
	// simply not found — never tenancy.ErrMismatch, which is a mutation rule.
	_, err = env.clientB.WorkspaceLineTotal().Get(ctx, tenantA)
	if !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get B on tenant A's key: err = %v, want ErrNotFound", err)
	}
	if errors.Is(err, tenancy.ErrMismatch) {
		t.Errorf("Get B returned tenancy.ErrMismatch — the §29.7 verify-match has no read-only analogue")
	}
}
