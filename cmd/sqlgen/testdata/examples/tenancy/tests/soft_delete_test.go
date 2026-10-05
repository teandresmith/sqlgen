package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestSoftDelete_AndTenancyAreANDed verifies §29.8 composition: when both
// soft-delete-exclusion and the tenant auto-filter are active, both apply
// (AND-ed). Tenant A only ever sees A's non-deleted articles. They are
// orthogonal — toggling one doesn't affect the other.
func TestSoftDelete_AndTenancyAreANDed(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	// 4 rows total:
	//   A live, A soft-deleted, B live, B soft-deleted
	mkArticle := func(env *testEnv, title string) *models.Article {
		t.Helper()
		got, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
			Title: title,
		})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return got
	}

	aLive := mkArticle(envA, "A live")
	aDeleted := mkArticle(envA, "A deleted")
	_ = mkArticle(envB, "B live")
	bDeleted := mkArticle(envB, "B deleted")

	if _, err := envA.client.Articles().SoftDelete(ctx, aDeleted.ID); err != nil {
		t.Fatalf("soft-delete A: %v", err)
	}
	if _, err := envB.client.Articles().SoftDelete(ctx, bDeleted.ID); err != nil {
		t.Fatalf("soft-delete B: %v", err)
	}

	// Default GetMany on tenant A: only A live (A's deleted excluded; B's
	// rows excluded entirely).
	listA, err := envA.client.Articles().GetMany(ctx, &models.GetArticlesInput{})
	if err != nil {
		t.Fatalf("default GetMany A: %v", err)
	}
	if len(listA) != 1 || listA[0].ID != aLive.ID {
		ids := make([]int64, len(listA))
		for i, a := range listA {
			ids[i] = a.ID
		}
		t.Errorf("default GetMany A: ids = %v, want [%d]", ids, aLive.ID)
	}
}

// TestSoftDelete_IncludeDeletedScopedToTenant verifies §29.8 orthogonality
// part 1: setting the DeletedAt filter to "include deleted rows too" still
// applies the tenant filter. Tenant A sees A's live + deleted articles, NOT
// any of B's. Soft-delete and tenancy are independent toggles.
func TestSoftDelete_IncludeDeletedScopedToTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	aLive, _ := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A live"})
	aDeleted, _ := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A deleted"})
	if _, err := envA.client.Articles().SoftDelete(ctx, aDeleted.ID); err != nil {
		t.Fatalf("soft-delete A: %v", err)
	}
	_, _ = envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "B live"})

	// Filter that overrides the default soft-delete exclusion (any non-nil
	// DeletedAt comparator suppresses the auto-injected IS NULL filter — see
	// the existing soft-delete machinery in cache/event examples).
	listA, err := envA.client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			DeletedAt: &comparator.NullableTime{},
		},
	})
	if err != nil {
		t.Fatalf("include-deleted GetMany A: %v", err)
	}

	gotIDs := map[int64]bool{}
	for _, a := range listA {
		gotIDs[a.ID] = true
	}
	if !gotIDs[aLive.ID] || !gotIDs[aDeleted.ID] {
		t.Errorf("include-deleted GetMany A: missing IDs, got %v want both %d and %d", gotIDs, aLive.ID, aDeleted.ID)
	}
	if len(gotIDs) != 2 {
		t.Errorf("include-deleted GetMany A: got %d distinct IDs, want 2 (A's live + deleted only — B's row must not appear)", len(gotIDs))
	}
}

// TestSoftDelete_SkipTenancyAndIncludeDeletedAreOrthogonal verifies §29.8
// orthogonality part 2: SkipTenancy:true + include-deleted filter → ALL rows
// across all tenants AND all deletion states. The two flags compose without
// interaction.
func TestSoftDelete_SkipTenancyAndIncludeDeletedAreOrthogonal(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	aLive, _ := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A live"})
	aDeleted, _ := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A deleted"})
	bLive, _ := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "B live"})
	bDeleted, _ := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "B deleted"})

	if _, err := envA.client.Articles().SoftDelete(ctx, aDeleted.ID); err != nil {
		t.Fatalf("soft-delete A: %v", err)
	}
	if _, err := envB.client.Articles().SoftDelete(ctx, bDeleted.ID); err != nil {
		t.Fatalf("soft-delete B: %v", err)
	}

	all, err := envA.client.Articles().GetMany(
		ctx, &models.GetArticlesInput{
			Filter: &models.ArticleFilter{DeletedAt: &comparator.NullableTime{}},
		},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("admin GetMany: %v", err)
	}

	gotIDs := map[int64]bool{}
	for _, a := range all {
		gotIDs[a.ID] = true
	}
	for _, want := range []int64{aLive.ID, aDeleted.ID, bLive.ID, bDeleted.ID} {
		if !gotIDs[want] {
			t.Errorf("admin GetMany missing id %d (gotIDs=%v)", want, gotIDs)
		}
	}
}
