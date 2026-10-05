package tests

// Cursor pagination (Connection) under tenancy (PRD §14, §29.4.1).
//
// The invariant being tested: on a tenanted table, Connection() never surfaces
// a row whose workspace_id doesn't match the resolver — not on the first page,
// not on a middle page, not on the last page. The keyset predicate and the
// tenant filter are both AND'd into every query the Connection helper issues,
// so a row owned by tenant B cannot leak onto tenant A's pages even when the
// IDs interleave.

import (
	"context"
	"fmt"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestConnection_CrossTenantRowsFilteredEveryPage seeds interleaved rows for
// tenant A and tenant B on `products` (simple PK, cursor_keys default = ["id"])
// and walks tenant A's Connection forward; assert every page contains only
// tenant A's rows and the full traversal visits exactly the A seed set.
func TestConnection_CrossTenantRowsFilteredEveryPage(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	// Interleave creates: A, B, A, B, ... 15 A rows + 15 B rows. Since product.id
	// is AUTOINCREMENT, A's IDs and B's IDs are interleaved in the same integer
	// sequence — the keyset cursor would happily walk right over B rows if the
	// tenant filter weren't AND'd in.
	const perTenant = 15
	var aIDs, bIDs []int64
	for i := range perTenant {
		pA, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("A-prod-%d", i), SKU: fmt.Sprintf("A-SKU-%d", i), Price: 1.0,
		})
		if err != nil {
			t.Fatalf("Create tenant A row %d: %v", i, err)
		}
		aIDs = append(aIDs, pA.ID)

		pB, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("B-prod-%d", i), SKU: fmt.Sprintf("B-SKU-%d", i), Price: 1.0,
		})
		if err != nil {
			t.Fatalf("Create tenant B row %d: %v", i, err)
		}
		bIDs = append(bIDs, pB.ID)
	}

	aSeen := make(map[int64]int)
	pageSize := 4
	var prevEnd *string
	pageCount := 0

	for {
		conn, err := envA.client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
			First: &pageSize,
			After: prevEnd,
		})
		if err != nil {
			t.Fatalf("tenant A Connection page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			if edge.Node.WorkspaceID != tenantA {
				t.Errorf("page %d: leaked row id=%d workspace_id=%v, want tenant A", pageCount, edge.Node.ID, edge.Node.WorkspaceID)
			}
			aSeen[edge.Node.ID]++
		}
		pageCount++
		if !conn.PageInfo.HasNextPage {
			break
		}
		prevEnd = conn.PageInfo.EndCursor
		if pageCount > perTenant+1 {
			t.Fatalf("forward traversal did not terminate after %d pages", pageCount)
		}
	}

	if len(aSeen) != perTenant {
		t.Errorf("tenant A forward traversal visited %d distinct rows, want %d", len(aSeen), perTenant)
	}
	for _, id := range aIDs {
		if aSeen[id] != 1 {
			t.Errorf("tenant A row %d visited %d times, want 1", id, aSeen[id])
		}
	}
	for _, id := range bIDs {
		if _, ok := aSeen[id]; ok {
			t.Errorf("tenant B row %d appeared on tenant A's traversal", id)
		}
	}

	// TotalCount on tenant A must be the tenant A count, not the table total.
	first1 := 1
	head, err := envA.client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first1})
	if err != nil {
		t.Fatalf("tenant A head page: %v", err)
	}
	if head.TotalCount != int64(perTenant) {
		t.Errorf("tenant A Connection.TotalCount = %d, want %d", head.TotalCount, perTenant)
	}
}

// TestConnection_BackwardTraversalTenantFiltered mirrors the forward test for
// Last+Before — backward keyset direction must still AND the tenant filter in.
func TestConnection_BackwardTraversalTenantFiltered(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	const perTenant = 12
	var aIDs []int64
	for i := range perTenant {
		pA, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("A-prod-%d", i), SKU: fmt.Sprintf("A-SKU-%d", i), Price: 1.0,
		})
		if err != nil {
			t.Fatalf("Create tenant A row %d: %v", i, err)
		}
		aIDs = append(aIDs, pA.ID)

		if _, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("B-prod-%d", i), SKU: fmt.Sprintf("B-SKU-%d", i), Price: 1.0,
		}); err != nil {
			t.Fatalf("Create tenant B row %d: %v", i, err)
		}
	}

	aSeen := make(map[int64]int)
	pageSize := 4
	var prevStart *string
	pageCount := 0

	for {
		conn, err := envA.client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
			Last:   &pageSize,
			Before: prevStart,
		})
		if err != nil {
			t.Fatalf("tenant A backward page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			if edge.Node.WorkspaceID != tenantA {
				t.Errorf("page %d: leaked row id=%d workspace_id=%v, want tenant A", pageCount, edge.Node.ID, edge.Node.WorkspaceID)
			}
			aSeen[edge.Node.ID]++
		}
		pageCount++
		if !conn.PageInfo.HasPreviousPage {
			break
		}
		prevStart = conn.PageInfo.StartCursor
		if pageCount > perTenant+1 {
			t.Fatalf("backward traversal did not terminate after %d pages", pageCount)
		}
	}

	if len(aSeen) != perTenant {
		t.Errorf("tenant A backward traversal visited %d distinct rows, want %d", len(aSeen), perTenant)
	}
	for _, id := range aIDs {
		if aSeen[id] != 1 {
			t.Errorf("tenant A row %d visited %d times, want 1", id, aSeen[id])
		}
	}
}

// TestConnection_SkipTenancySurfacesAllRows confirms the escape hatch: an admin
// caller with SkipTenancy=true (PRD §29.4.4) sees rows from every tenant in
// the Connection result. Also doubles as a belt-and-suspenders check that the
// interleaved seed data actually spans both tenants (without SkipTenancy the
// A-only traversal above would pass trivially if seeding were broken).
func TestConnection_SkipTenancySurfacesAllRows(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	const perTenant = 5
	for i := range perTenant {
		if _, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("A-prod-%d", i), SKU: fmt.Sprintf("A-SKU-%d", i), Price: 1.0,
		}); err != nil {
			t.Fatalf("Create tenant A %d: %v", i, err)
		}
		if _, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name: fmt.Sprintf("B-prod-%d", i), SKU: fmt.Sprintf("B-SKU-%d", i), Price: 1.0,
		}); err != nil {
			t.Fatalf("Create tenant B %d: %v", i, err)
		}
	}

	pageSize := 20
	conn, err := envA.client.Products().Connection(
		ctx,
		models.ConnectionInput[models.ProductFilter]{First: &pageSize},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("SkipTenancy Connection: %v", err)
	}
	if len(conn.Edges) != 2*perTenant {
		t.Errorf("SkipTenancy Connection edges = %d, want %d (both tenants)", len(conn.Edges), 2*perTenant)
	}
	if conn.TotalCount != int64(2*perTenant) {
		t.Errorf("SkipTenancy Connection TotalCount = %d, want %d", conn.TotalCount, 2*perTenant)
	}

	// Both tenants appear.
	seenTenants := make(map[[16]byte]int)
	for _, e := range conn.Edges {
		seenTenants[e.Node.WorkspaceID]++
	}
	if seenTenants[tenantA] != perTenant || seenTenants[tenantB] != perTenant {
		t.Errorf("SkipTenancy tenant counts = {A:%d, B:%d}, want {A:%d, B:%d}",
			seenTenants[tenantA], seenTenants[tenantB], perTenant, perTenant)
	}
}
