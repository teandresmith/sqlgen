package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// This file is a regression pin (PRD §29.2.5 / §29.4.1): without view tenancy
// emission, a view that projected the tenant column returned every tenant's rows through
// all five read methods. product_stats is scoped by detection alone — its
// sqlgen.yml entry is `product_stats: {}`, no tenancy block — and
// workspace_summaries is the explicit `tenancy.enabled: false` opt-out.

// seedViewRows creates products for both tenants so product_stats projects
// rows for each. Returns the number of rows seeded per tenant.
func seedViewRows(t *testing.T, envA, envB *testEnv, countA, countB int) {
	t.Helper()
	ctx := context.Background()

	for i := range countA {
		if _, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name:  "tenant A product",
			SKU:   "VA-" + string(rune('a'+i)),
			Price: float64(i + 1),
		}); err != nil {
			t.Fatalf("create A product %d: %v", i, err)
		}
	}
	for i := range countB {
		if _, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name:  "tenant B product",
			SKU:   "VB-" + string(rune('a'+i)),
			Price: float64(i + 1),
		}); err != nil {
			t.Fatalf("create B product %d: %v", i, err)
		}
	}
}

// TestViewTenancy_AllReadMethodsScoped is the core isolation assertion: with
// rows from two tenants visible through one view, each of the five read
// methods returns only the resolver's tenant.
func TestViewTenancy_AllReadMethodsScoped(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, envA, envB, 3, 2)

	ctx := context.Background()

	// GetMany
	rowsA, err := envA.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{})
	if err != nil {
		t.Fatalf("GetMany A: %v", err)
	}
	if len(rowsA) != 3 {
		t.Errorf("GetMany A: %d rows, want 3", len(rowsA))
	}
	for _, r := range rowsA {
		if r.WorkspaceID != tenantA {
			t.Errorf("GetMany A returned a row for %v, want only %v", r.WorkspaceID, tenantA)
		}
	}

	rowsB, err := envB.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{})
	if err != nil {
		t.Fatalf("GetMany B: %v", err)
	}
	if len(rowsB) != 2 {
		t.Errorf("GetMany B: %d rows, want 2", len(rowsB))
	}

	// Count
	countA, err := envA.client.ProductStat().Count(ctx, nil)
	if err != nil {
		t.Fatalf("Count A: %v", err)
	}
	if countA != 3 {
		t.Errorf("Count A = %d, want 3", countA)
	}
	countB, err := envB.client.ProductStat().Count(ctx, nil)
	if err != nil {
		t.Fatalf("Count B: %v", err)
	}
	if countB != 2 {
		t.Errorf("Count B = %d, want 2", countB)
	}

	// Get — tenant A's row is invisible to tenant B. The view row exists in
	// the database; the auto-filter scopes it out, so this is ErrNotFound
	// rather than an authorization error (matching the table path).
	target := rowsA[0]
	gotA, err := envA.client.ProductStat().Get(ctx, target.ID)
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	if gotA.ID != target.ID {
		t.Errorf("Get A: id = %d, want %d", gotA.ID, target.ID)
	}
	if _, err := envB.client.ProductStat().Get(ctx, target.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get B on tenant A's row: err = %v, want ErrNotFound", err)
	}

	// Paginate
	pageA, err := envA.client.ProductStat().Paginate(ctx, models.PaginateInput[models.ProductStatFilter]{Limit: 10})
	if err != nil {
		t.Fatalf("Paginate A: %v", err)
	}
	if pageA.TotalCount != 3 || len(pageA.Items) != 3 {
		t.Errorf("Paginate A: total = %d, items = %d, want 3/3", pageA.TotalCount, len(pageA.Items))
	}
	for _, r := range pageA.Items {
		if r.WorkspaceID != tenantA {
			t.Errorf("Paginate A returned a row for %v", r.WorkspaceID)
		}
	}

	// Connection
	first := 10
	connB, err := envB.client.ProductStat().Connection(ctx, models.ConnectionInput[models.ProductStatFilter]{First: &first})
	if err != nil {
		t.Fatalf("Connection B: %v", err)
	}
	if connB.TotalCount != 2 || len(connB.Edges) != 2 {
		t.Errorf("Connection B: total = %d, edges = %d, want 2/2", connB.TotalCount, len(connB.Edges))
	}
	for _, e := range connB.Edges {
		if e.Node.WorkspaceID != tenantB {
			t.Errorf("Connection B returned a row for %v", e.Node.WorkspaceID)
		}
	}
}

// TestViewTenancy_SkipTenancy pins the §29.4.4 admin escape hatch on a view:
// SkipTenancy: true removes the predicate from every read.
func TestViewTenancy_SkipTenancy(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, envA, envB, 3, 2)

	ctx := context.Background()
	skip := func(o *models.CallOptions[models.ProductStatFieldOptions]) { o.SkipTenancy = true }

	rows, err := envA.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{}, skip)
	if err != nil {
		t.Fatalf("GetMany with SkipTenancy: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("GetMany with SkipTenancy: %d rows, want 5 (both tenants)", len(rows))
	}

	count, err := envA.client.ProductStat().Count(ctx, nil, skip)
	if err != nil {
		t.Fatalf("Count with SkipTenancy: %v", err)
	}
	if count != 5 {
		t.Errorf("Count with SkipTenancy = %d, want 5", count)
	}

	page, err := envA.client.ProductStat().Paginate(ctx, models.PaginateInput[models.ProductStatFilter]{Limit: 10}, skip)
	if err != nil {
		t.Fatalf("Paginate with SkipTenancy: %v", err)
	}
	if page.TotalCount != 5 || len(page.Items) != 5 {
		t.Errorf("Paginate with SkipTenancy: total = %d, items = %d, want 5/5", page.TotalCount, len(page.Items))
	}
}

// TestViewTenancy_ExplicitTenant pins the §29.4.4 explicit mode on a view: an
// explicit CallOptions.Tenant replaces the resolver, and wins over
// SkipTenancy when both are set (staying scoped is the safer resolution).
func TestViewTenancy_ExplicitTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, envA, envB, 3, 2)

	ctx := context.Background()

	// A's client, explicitly scoped to B.
	explicitB := func(o *models.CallOptions[models.ProductStatFieldOptions]) { o.Tenant = new(tenantB) }
	rows, err := envA.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{}, explicitB)
	if err != nil {
		t.Fatalf("GetMany with explicit tenant: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("GetMany with explicit tenant B: %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.WorkspaceID != tenantB {
			t.Errorf("explicit tenant B returned a row for %v", r.WorkspaceID)
		}
	}

	count, err := envA.client.ProductStat().Count(ctx, nil, explicitB)
	if err != nil {
		t.Fatalf("Count with explicit tenant: %v", err)
	}
	if count != 2 {
		t.Errorf("Count with explicit tenant B = %d, want 2", count)
	}

	// Explicit wins over SkipTenancy (§29.4.4).
	both := func(o *models.CallOptions[models.ProductStatFieldOptions]) {
		o.SkipTenancy = true
		o.Tenant = new(tenantB)
	}
	rows, err = envA.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{}, both)
	if err != nil {
		t.Fatalf("GetMany with explicit tenant + SkipTenancy: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("explicit tenant + SkipTenancy: %d rows, want 2 (explicit wins)", len(rows))
	}

	// An explicit tenant also satisfies a client with no resolver at all.
	envNil := newEnv(t, withResolver(nil))
	rows, err = envNil.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{}, explicitB)
	if err != nil {
		t.Fatalf("GetMany with explicit tenant and no resolver: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("no resolver + explicit tenant B: %d rows, want 2", len(rows))
	}
}

// TestViewTenancy_RequiredFailsClosed pins §29.3.1 on the view read path:
// under tenancy.required:true (the global setting for this example) an absent
// resolver fails every read before the DB round-trip.
func TestViewTenancy_RequiredFailsClosed(t *testing.T) {
	resetDB(t)

	seedEnvA := newEnv(t, withResolver(staticResolver(tenantA)))
	seedEnvB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, seedEnvA, seedEnvB, 2, 1)

	ctx := context.Background()

	for _, tt := range []struct {
		name string
		env  *testEnv
	}{
		{"nil resolver", newEnv(t, withResolver(nil))},
		{"resolver returns ErrMissing", newEnv(t, withResolver(missingResolver()))},
		{"resolver returns the zero tenant", newEnv(t, withResolver(func(context.Context) (uuid.UUID, error) {
			return uuid.Nil(), nil
		}))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.env.client.ProductStat()

			if _, err := client.GetMany(ctx, &models.GetProductStatsInput{}); !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("GetMany: err = %v, want tenancy.ErrMissing", err)
			}
			if _, err := client.Get(ctx, 1); !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("Get: err = %v, want tenancy.ErrMissing", err)
			}
			if _, err := client.Count(ctx, nil); !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("Count: err = %v, want tenancy.ErrMissing", err)
			}
			if _, err := client.Paginate(ctx, models.PaginateInput[models.ProductStatFilter]{Limit: 10}); !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("Paginate: err = %v, want tenancy.ErrMissing", err)
			}
			first := 10
			if _, err := client.Connection(ctx, models.ConnectionInput[models.ProductStatFilter]{First: &first}); !errors.Is(err, tenancy.ErrMissing) {
				t.Errorf("Connection: err = %v, want tenancy.ErrMissing", err)
			}
		})
	}
}

// TestViewTenancy_RequiredFailsBeforeQuery asserts the fail-closed path
// short-circuits before any statement reaches the database — the same
// guarantee the table path carries (§29.3.1).
func TestViewTenancy_RequiredFailsBeforeQuery(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()

	if _, err := env.client.ProductStat().GetMany(context.Background(), &models.GetProductStatsInput{}); !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("GetMany: err = %v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("issued %d queries, want 0 — the read must fail before the DB round-trip", got)
	}
}

// TestViewTenancy_OptedOutViewIsCrossTenant pins the §29.2.5 opt-out:
// workspace_summaries carries `tenancy.enabled: false`, so it is a deliberate
// cross-tenant aggregate and its reads are never scoped.
func TestViewTenancy_OptedOutViewIsCrossTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()
	for _, id := range []uuid.UUID{tenantA, tenantB} {
		if _, err := envA.client.Workspaces().Create(ctx, &models.CreateWorkspaceInput{
			ID:   id,
			Name: "workspace " + id.String(),
		}); err != nil {
			t.Fatalf("create workspace %v: %v", id, err)
		}
	}
	seedViewRows(t, envA, envB, 2, 1)

	rows, err := envA.client.WorkspaceSummary().GetMany(ctx, &models.GetWorkspaceSummariesInput{})
	if err != nil {
		t.Fatalf("GetMany on opted-out view: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("opted-out view returned %d rows, want 2 (both workspaces)", len(rows))
	}

	// The same read from an absent-resolver client also succeeds: an opted-out
	// view never resolves a tenant, so required:true cannot fail it.
	envNil := newEnv(t, withResolver(nil))
	if _, err := envNil.client.WorkspaceSummary().Count(ctx, nil); err != nil {
		t.Errorf("Count on opted-out view with no resolver: %v", err)
	}
}

// TestViewTenancy_CursorPaginationStaysScoped walks a tenanted view a page at
// a time. The tenant predicate is appended before the keyset conditions, so
// placeholder numbering stays stable across pages — a corrupted binding would
// surface as a repeated row, a skipped row, or a row from the other tenant.
func TestViewTenancy_CursorPaginationStaysScoped(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, envA, envB, 5, 4)

	ctx := context.Background()
	pageSize := 2

	seen := make(map[int64]int)
	var cursor *string
	for page := 0; page < 10; page++ {
		conn, err := envA.client.ProductStat().Connection(ctx, models.ConnectionInput[models.ProductStatFilter]{
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

// TestViewTenancy_FilterComposesWithPredicate asserts the tenant predicate
// ANDs with a caller-supplied filter rather than replacing it.
func TestViewTenancy_FilterComposesWithPredicate(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	seedViewRows(t, envA, envB, 3, 2)

	ctx := context.Background()

	// Tenant A's products are priced 1, 2, 3; tenant B's are 1, 2.
	rows, err := envA.client.ProductStat().GetMany(ctx, &models.GetProductStatsInput{
		Filter: &models.ProductStatFilter{
			Price: &comparator.Number[float64]{Gte: new(2.0)},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with filter: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("filtered GetMany: %d rows, want 2 (A's price >= 2)", len(rows))
	}
	for _, r := range rows {
		if r.WorkspaceID != tenantA {
			t.Errorf("filtered GetMany returned a row for %v", r.WorkspaceID)
		}
		if r.Price < 2 {
			t.Errorf("filtered GetMany returned price %v, want >= 2", r.Price)
		}
	}
}
