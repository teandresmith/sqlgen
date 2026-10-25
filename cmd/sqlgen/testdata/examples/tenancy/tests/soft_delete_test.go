package tests

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// articleFixture is one live and one soft-deleted article per tenant, all
// sharing a title so a *Where filter on it matches every row.
type articleFixture struct {
	envA, envB        *testEnv
	aLive, aDeleted   *models.Article
	bLive, bDeleted   *models.Article
	sharedTitle       string
	sharedTitleFilter *models.ArticleFilter
}

func newArticleFixture(t *testing.T) articleFixture {
	t.Helper()
	resetDB(t)
	ctx := context.Background()
	f := articleFixture{
		envA:        newEnv(t, withResolver(staticResolver(tenantA))),
		envB:        newEnv(t, withResolver(staticResolver(tenantB))),
		sharedTitle: "shared",
	}
	f.sharedTitleFilter = &models.ArticleFilter{Title: &comparator.String{Eq: &f.sharedTitle}}
	mk := func(env *testEnv, deleted bool) *models.Article {
		t.Helper()
		a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: f.sharedTitle})
		if err != nil {
			t.Fatalf("create article: %v", err)
		}
		if deleted {
			if _, err := env.client.Articles().SoftDelete(ctx, a.ID); err != nil {
				t.Fatalf("soft-delete article: %v", err)
			}
		}
		return a
	}
	f.aLive, f.aDeleted = mk(f.envA, false), mk(f.envA, true)
	f.bLive, f.bDeleted = mk(f.envB, false), mk(f.envB, true)
	return f
}

// isLive reports whether env's tenant still reads id as a live row.
func isLive(t *testing.T, env *testEnv, id int64) bool {
	t.Helper()
	_, err := env.client.Articles().Get(context.Background(), id)
	switch {
	case err == nil:
		return true
	case errors.Is(err, models.ErrNotFound):
		return false
	default:
		t.Fatalf("Get(%d): %v", id, err)
		return false
	}
}

// noRowReached reports whether a by-key SoftDelete or Restore result shows
// that no row matched: both are idempotent and return nil, nil.
func noRowReached(got *models.Article, err error) bool {
	return got == nil && err == nil
}

func articleIDs(as []*models.Article) []int64 {
	ids := make([]int64, 0, len(as))
	for _, a := range as {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	return ids
}

// TestSoftDelete_TenantedGetANDsTenantAndSoftDeleteFilters pins the by-key
// read on a tenanted soft-delete table: a row is visible only when it belongs
// to the resolved tenant and is not soft-deleted.
func TestSoftDelete_TenantedGetANDsTenantAndSoftDeleteFilters(t *testing.T) {
	f := newArticleFixture(t)
	ctx := context.Background()

	t.Run("own live row is returned", func(t *testing.T) {
		got, err := f.envA.client.Articles().Get(ctx, f.aLive.ID)
		if err != nil {
			t.Fatalf("Get own live row: %v", err)
		}
		if got.ID != f.aLive.ID {
			t.Errorf("Get own live row: id = %d, want %d", got.ID, f.aLive.ID)
		}
	})
	t.Run("own soft-deleted row is not found", func(t *testing.T) {
		if _, err := f.envA.client.Articles().Get(ctx, f.aDeleted.ID); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get own soft-deleted row: err = %v, want models.ErrNotFound", err)
		}
	})
	t.Run("another tenant's live row is not found", func(t *testing.T) {
		if _, err := f.envA.client.Articles().Get(ctx, f.bLive.ID); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get other tenant's live row: err = %v, want models.ErrNotFound", err)
		}
	})
}

// TestSoftDelete_SoftDeleteAndRestoreAreTenantScoped pins that every
// SoftDelete* and Restore* method takes the tenant filter: a key or filter
// naming another tenant's row changes nothing, and the *Where forms AND the
// tenant filter with their own soft-delete scoping. None of them takes a
// tenant argument; the calls below pass only keys or a filter.
func TestSoftDelete_SoftDeleteAndRestoreAreTenantScoped(t *testing.T) {
	ctx := context.Background()

	t.Run("SoftDelete of another tenant's row changes nothing", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envB.client.Articles().SoftDelete(ctx, f.aLive.ID)
		if !noRowReached(got, err) {
			t.Errorf("SoftDelete as B of A's row = (%v, %v), want no row", got, err)
		}
		if !isLive(t, f.envA, f.aLive.ID) {
			t.Error("A's row was soft-deleted by tenant B")
		}
	})
	t.Run("SoftDeleteMany of another tenant's rows changes nothing", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envB.client.Articles().SoftDeleteMany(ctx, []int64{f.aLive.ID})
		if err != nil || len(got) != 0 {
			t.Errorf("SoftDeleteMany as B of A's row = (%v, %v), want no rows", articleIDs(got), err)
		}
		if !isLive(t, f.envA, f.aLive.ID) {
			t.Error("A's row was soft-deleted by tenant B")
		}
	})
	t.Run("SoftDeleteWhere soft-deletes only the tenant's own live rows", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envA.client.Articles().SoftDeleteWhere(ctx, f.sharedTitleFilter)
		if err != nil {
			t.Fatalf("SoftDeleteWhere as A: %v", err)
		}
		if diff := cmp.Diff([]int64{f.aLive.ID}, articleIDs(got)); diff != "" {
			t.Errorf("SoftDeleteWhere as A affected rows (-want +got):\n%s", diff)
		}
		if !isLive(t, f.envB, f.bLive.ID) {
			t.Error("B's live row was soft-deleted by tenant A")
		}
	})
	t.Run("Restore of another tenant's row changes nothing", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envB.client.Articles().Restore(ctx, f.aDeleted.ID)
		if !noRowReached(got, err) {
			t.Errorf("Restore as B of A's row = (%v, %v), want no row", got, err)
		}
		if isLive(t, f.envA, f.aDeleted.ID) {
			t.Error("A's soft-deleted row was restored by tenant B")
		}
	})
	t.Run("RestoreMany of another tenant's rows changes nothing", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envB.client.Articles().RestoreMany(ctx, []int64{f.aDeleted.ID})
		if err != nil || len(got) != 0 {
			t.Errorf("RestoreMany as B of A's row = (%v, %v), want no rows", articleIDs(got), err)
		}
		if isLive(t, f.envA, f.aDeleted.ID) {
			t.Error("A's soft-deleted row was restored by tenant B")
		}
	})
	t.Run("RestoreWhere restores only the tenant's own soft-deleted rows", func(t *testing.T) {
		f := newArticleFixture(t)
		got, err := f.envA.client.Articles().RestoreWhere(ctx, f.sharedTitleFilter)
		if err != nil {
			t.Fatalf("RestoreWhere as A: %v", err)
		}
		if diff := cmp.Diff([]int64{f.aDeleted.ID}, articleIDs(got)); diff != "" {
			t.Errorf("RestoreWhere as A affected rows (-want +got):\n%s", diff)
		}
		if isLive(t, f.envB, f.bDeleted.ID) {
			t.Error("B's soft-deleted row was restored by tenant A")
		}
	})
}
