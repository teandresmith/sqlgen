package tests

import (
	"context"
	"testing"
	"uuid"

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
// reservation serializes the fan-out at the Tx instead, so these tests now
// exercise the reservation rather than an errgroup limit — they pass either
// way, which is exactly what makes them the regression gate for both the
// original defect and the bound's removal (PRD §13.2 step 4).
//
// They cover both surfaces that reach the loader: the plain read path, and
// §9.9.6 step 3's terminal re-read, which runs inside the nested method's own
// transaction and so had no call-site spelling that avoided the defect.
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

// TestLoadRelationships_MultiEdgeInsideNestedTransaction covers the surface
// that could not opt out. §9.9.6 step 3 places the terminal re-read inside the
// method's own transaction and §9.9.5 makes selecting a relationship the
// documented way to read back what the call just wrote — so a parent with two
// or more selected edges failed on the feature's own contract, with no way to
// spell the call that avoided it. Both write verbs are covered: the re-read is
// the same code on either side, but only an assertion proves it.
func TestLoadRelationships_MultiEdgeInsideNestedTransaction(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	existingCat := seedCategoryRow(t, "nested-fanout-topic")

	t.Run("CreateWithRelated", func(t *testing.T) {
		created, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User: models.CreateUserInput{Email: "nested-fanout-create@example.com", Name: "Fanout"},
			Events: &models.UserEventsCreateNested{
				Create: []*models.UserEventsCreateInput{{Action: "nested-fanout", OccurredAt: nestedOccurredAt()}},
			},
			Orders: &models.UserOrdersCreateNested{
				Create: []*models.UserOrdersCreateInput{{Notes: omittable.Set(new("nested-fanout-order"))}},
			},
			Categories: &models.UserCategoriesCreateNested{Connect: []int64{existingCat}},
		}, allThreeEdges)
		if err != nil {
			t.Fatalf("CreateWithRelated with three edges selected: %v", err)
		}
		// The rows come back off the terminal re-read, which is the statement
		// that used to fail — asserting the counts is what proves it ran.
		assertEdgesLoaded(t, created, 1, 1, 1)
	})

	t.Run("UpdateWithRelated", func(t *testing.T) {
		user := seedUserRow(t, "nested-fanout-update@example.com")
		updated, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			User: models.UpdateUserInput{Name: omittable.Set("Fanout Updated")},
			Events: &models.UserEventsUpdateNested{
				Create: []*models.UserEventsCreateInput{{Action: "nested-fanout-update", OccurredAt: nestedOccurredAt()}},
			},
			Orders: &models.UserOrdersUpdateNested{
				Create: []*models.UserOrdersCreateInput{{Notes: omittable.Set(new("nested-fanout-update-order"))}},
			},
			Categories: &models.UserCategoriesUpdateNested{Connect: []int64{existingCat}},
		}, allThreeEdges)
		if err != nil {
			t.Fatalf("UpdateWithRelated with three edges selected: %v", err)
		}
		assertEdgesLoaded(t, updated, 1, 1, 1)
	})
}

// TestLoadRelationships_MultiEdgeInsideCallerTransaction nests the two:
// a nested mutation running inside a caller's own transaction, so §18.3's
// savepoint path is what the re-read fans out under. The outer transaction must
// still be usable afterwards — the original failure poisoned the connection, so
// a later statement on the same tx is the assertion that it no longer does.
func TestLoadRelationships_MultiEdgeInsideCallerTransaction(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	cat := seedCategoryRow(t, "outer-fanout-topic")

	var created *models.User
	var afterID uuid.UUID
	if err := testClient.WithTx(ctx, "outer_fanout", func(txCtx context.Context) error {
		var createErr error
		created, createErr = testClient.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
			User: models.CreateUserInput{Email: "outer-fanout@example.com", Name: "Outer"},
			Events: &models.UserEventsCreateNested{
				Create: []*models.UserEventsCreateInput{{Action: "outer-fanout", OccurredAt: nestedOccurredAt()}},
			},
			Orders: &models.UserOrdersCreateNested{
				Create: []*models.UserOrdersCreateInput{{Notes: omittable.Set(new("outer-fanout-order"))}},
			},
			Categories: &models.UserCategoriesCreateNested{Connect: []int64{cat}},
		}, allThreeEdges)
		if createErr != nil {
			return createErr
		}
		// The connection survived the re-read, so the outer transaction can
		// still issue statements.
		after, createErr := testClient.Users().Create(txCtx, &models.CreateUserInput{
			Email: "outer-fanout-after@example.com", Name: "After",
		})
		if createErr != nil {
			return createErr
		}
		afterID = after.ID
		return nil
	}); err != nil {
		t.Fatalf("nested mutation inside a caller transaction: %v", err)
	}
	assertEdgesLoaded(t, created, 1, 1, 1)

	// Committed, not rolled back — the original failure lost the whole
	// transaction, so the row written after the re-read is the proof.
	if _, err := testClient.Users().Get(ctx, afterID); err != nil {
		t.Errorf("row written after the terminal re-read is absent: %v", err)
	}
}
