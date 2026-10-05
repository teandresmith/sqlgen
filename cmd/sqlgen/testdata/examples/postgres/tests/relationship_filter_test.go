package tests

// Relationship filter fields → correlated EXISTS (PRD §11.1), PostgreSQL arm.
//
// The subquery's *structure* is identical on all three dialects — only quoting
// and placeholder style differ — so the mysql/ and sqlite/ examples carry the
// same assertions against their own databases. PostgreSQL is the arm that also
// exercises numbered placeholders: the subquery's args are renumbered into the
// enclosing statement's sequence, and a slip there binds the wrong values
// rather than failing to parse.

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestRelationshipFilter_O2MExactRowSet filters parents by a child predicate:
// only the user whose order carries the status comes back.
func TestRelationshipFilter_O2MExactRowSet(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	matching, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "relfilter-o2m-hit@example.com", Name: "RelFilterO2MHit",
	})
	if err != nil {
		t.Fatalf("Create matching user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, matching.ID) })

	other, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "relfilter-o2m-miss@example.com", Name: "RelFilterO2MMiss",
	})
	if err != nil {
		t.Fatalf("Create other user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, other.ID) })

	hit, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: matching.ID, Notes: omittable.Set(new("relfilter-shipped")),
	})
	if err != nil {
		t.Fatalf("Create matching order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, hit.ID) })

	miss, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: other.ID, Notes: omittable.Set(new("relfilter-pending")),
	})
	if err != nil {
		t.Fatalf("Create other order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, miss.ID) })

	got, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Orders: &models.OrderFilter{
				Notes: &comparator.NullableString{String: comparator.String{Eq: new("relfilter-shipped")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with O2M relationship filter: %v", err)
	}
	if len(got) != 1 || got[0].ID != matching.ID {
		t.Fatalf("O2M relationship filter returned %d users (want exactly the matching one)", len(got))
	}
}

// TestRelationshipFilter_M2MExactRowSet drives the junction shape: one EXISTS
// over user_categories joined to categories, not the two-query M2M loader.
func TestRelationshipFilter_M2MExactRowSet(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	linked, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "relfilter-m2m-hit@example.com", Name: "RelFilterM2MHit",
	})
	if err != nil {
		t.Fatalf("Create linked user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, linked.ID) })

	unlinked, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "relfilter-m2m-miss@example.com", Name: "RelFilterM2MMiss",
	})
	if err != nil {
		t.Fatalf("Create unlinked user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, unlinked.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "RelFilterM2MCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	if _, err := client.UserCategories().Create(ctx, &models.CreateUserCategoryInput{
		UserID: linked.ID, CategoryID: cat.ID,
	}); err != nil {
		t.Fatalf("Create user_category: %v", err)
	}

	got, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{
			Categories: &models.CategoryFilter{
				Name: &comparator.String{Eq: new("RelFilterM2MCat")},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with M2M relationship filter: %v", err)
	}
	if len(got) != 1 || got[0].ID != linked.ID {
		t.Fatalf("M2M relationship filter returned %d users (want exactly the linked one)", len(got))
	}
}

// TestRelationshipFilter_ComposesWithColumnFilter is the composition claim:
// the EXISTS narrows alongside an ordinary column predicate in one statement.
func TestRelationshipFilter_ComposesWithColumnFilter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: "relfilter-compose@example.com", Name: "RelFilterCompose",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	order, err := client.Orders().Create(ctx, &models.CreateOrderInput{
		UserID: user.ID, Notes: omittable.Set(new("relfilter-compose-note")),
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

	filter := func(name string) *models.PublicUserFilter {
		return &models.PublicUserFilter{
			Name: &comparator.String{Eq: &name},
			Orders: &models.OrderFilter{
				Notes: &comparator.NullableString{String: comparator.String{Eq: new("relfilter-compose-note")}},
			},
		}
	}

	got, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{Filter: filter("RelFilterCompose")})
	if err != nil {
		t.Fatalf("GetMany (both match): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("column + relationship filter returned %d users, want 1", len(got))
	}

	// The column half alone must still be able to exclude the row, which rules
	// out the EXISTS silently widening the result.
	none, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{Filter: filter("NoSuchName")})
	if err != nil {
		t.Fatalf("GetMany (column excludes): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("column filter did not narrow alongside the relationship filter: got %d users", len(none))
	}
}
