package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// Selecting two or more relationships inside a transaction.
//
// `loadRelationships` fans its per-edge reads out across an errgroup. Inside a
// transaction every one of those reads lands on the *same* driver connection —
// `database.Conn` hands the body the `*database.Tx` — and a connection carries
// one statement at a time. Unbounded, two selected edges therefore raced each
// other on one pgx conn: a data race in the driver's statement cache, then
// `conn busy`, and because the rollback could not run on a busy connection
// either, every statement the transaction had already issued was lost with it:
//
//	rollback failed: rolling back transaction <name>: pgx rollback: conn closed
//	  (original error: load user events: get events: tx query <name>: tx query: conn busy)
//
// The errgroup bound that first fixed this is gone. PRD §18.5's connection
// reservation serializes the fan-out at the Tx instead, so this test now
// exercises the reservation rather than an errgroup limit — it passes either
// way, which is exactly what makes it the regression gate for both the
// original defect and the bound's removal (PRD §13.2 step 4).
//
// `users` is the parent with three edges — `Events` (O2M, nullable FK),
// `Orders` (O2M, NOT NULL FK) and `Categories` (M2M, which is two statements of
// its own). Run under `-race`: the failure this guards against was a data race
// first and an error second.

// allThreeEdges selects every relationship on `users`, which is what makes the
// loader fan out. One edge alone never reproduced the defect.
func allThreeEdges(co *models.CallOptions[models.UserFieldOptions]) {
	co.FieldOptions = &models.UserFieldOptions{
		ID:         true,
		Email:      true,
		Name:       true,
		Events:     &models.EventRelationshipOptions{},
		Orders:     &models.OrderRelationshipOptions{},
		Categories: &models.CategoryRelationshipOptions{},
	}
}

// assertEdgesLoaded checks the three edges came back with the row counts the
// caller seeded, so a test cannot pass by loading nothing at all.
func assertEdgesLoaded(t *testing.T, u *models.User, wantEvents, wantOrders, wantCategories int) {
	t.Helper()
	if got := len(u.Events); got != wantEvents {
		t.Errorf("Events = %d rows, want %d", got, wantEvents)
	}
	if got := len(u.Orders); got != wantOrders {
		t.Errorf("Orders = %d rows, want %d", got, wantOrders)
	}
	if got := len(u.Categories); got != wantCategories {
		t.Errorf("Categories = %d rows, want %d", got, wantCategories)
	}
}

// TestLoadRelationships_MultiEdgeInsideTransaction is the plain read path — a
// caller selecting several relationships inside their own WithTx block. This is
// the probe that establishes the scope: the defect is in relationship loading,
// not in nested mutations.
func TestLoadRelationships_MultiEdgeInsideTransaction(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "fanout-read@example.com")
	seedParentedEvent(t, user, "fanout-read-event")
	cat := seedCategoryRow(t, "fanout-read-topic")
	if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		Categories: &models.UserCategoriesUpdateNested{Connect: []int64{cat}},
		Orders:     &models.UserOrdersUpdateNested{Create: []*models.UserOrdersCreateInput{{Notes: omittable.Set(new("fanout-read-order"))}}},
	}); err != nil {
		t.Fatalf("seeding the edges: %v", err)
	}

	// Outside a transaction first: the fan-out is real there — each edge draws
	// its own pooled connection — and must keep working.
	outside, err := testClient.Users().Get(ctx, user, allThreeEdges)
	if err != nil {
		t.Fatalf("Get outside a transaction: %v", err)
	}
	assertEdgesLoaded(t, outside, 1, 1, 1)

	var inside *models.User
	if err := testClient.WithTx(ctx, "fanout_read", func(txCtx context.Context) error {
		var getErr error
		inside, getErr = testClient.Users().Get(txCtx, user, allThreeEdges)
		return getErr
	}); err != nil {
		t.Fatalf("Get inside a transaction: %v", err)
	}
	assertEdgesLoaded(t, inside, 1, 1, 1)
}
