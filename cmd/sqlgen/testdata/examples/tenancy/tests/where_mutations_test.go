package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// *Where mutation edge cases under tenancy (PRD §9.3, §9.5, §29.4.2).
//
// Two contracts to validate:
//  1. Caller-supplied `WorkspaceID` on UpdateInput that mismatches the resolved
//     tenant returns `tenancy.ErrMismatch` BEFORE the DB round-trip — same
//     verify-match rule as `Update` (§29.4.2). UpdateWhere is interesting
//     because the caller is also supplying a *filter*; the mismatch guard
//     applies to the input only (the row-set selector is independent).
//  2. The resolver tenant is AND'd into the final WHERE — cross-tenant rows
//     are not affected even when the user-composed predicate is broad enough
//     to match them. Verified by row-count assertion: seed cross-tenant rows
//     matching the predicate, run UpdateWhere as tenant A, assert tenant B's
//     rows are untouched (per phase doc §14.3 acceptance criteria — captured
//     SQL OR row-count is acceptable).

// TestUpdateWhere_TenantMismatch_ReturnsErrMismatch verifies the §29.4.2
// verify-match rule on UpdateWhere: caller sets WorkspaceID on the
// UpdateInput, mismatch with the resolver → ErrMismatch with no DB round-trip.
func TestUpdateWhere_TenantMismatch_ReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	created, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "to-rename",
	})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	envA.counter.reset()
	_, err = envA.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{ID: &comparator.Number[int64]{Eq: &created.ID}},
		&models.UpdateArticleInput{
			WorkspaceID: omittable.Set(tenantB), // mismatched
			Title:       omittable.Set("renamed"),
		},
	)
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("UpdateWhere with mismatched WorkspaceID: err = %v, want tenancy.ErrMismatch", err)
	}
	// PRD §29.4.2: ErrMismatch is returned BEFORE the DB round-trip — no
	// UPDATE hits the wire.
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during UpdateWhere mismatch path: %d, want 0", got)
	}

	// Belt-and-suspenders: row title still original.
	got, err := envA.client.Articles().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get post-mismatch: %v", err)
	}
	if got.Title != "to-rename" {
		t.Errorf("post-mismatch row title = %q, want %q (mismatch must not partially-write)", got.Title, "to-rename")
	}
}

// TestUpdateWhere_ResolverTenantANDdIntoWhere verifies the auto-applied tenant
// filter: rows owned by another tenant are not affected by UpdateWhere even
// when the user-composed predicate would match them.
func TestUpdateWhere_ResolverTenantANDdIntoWhere(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	// Seed two articles with the same title — one per tenant. The predicate
	// `title = "shared"` matches both; the tenant filter must scope the
	// UpdateWhere to tenant A's row only.
	aArticle, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "shared"})
	if err != nil {
		t.Fatalf("Create A's article: %v", err)
	}
	bArticle, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "shared"})
	if err != nil {
		t.Fatalf("Create B's article: %v", err)
	}

	sharedTitle := "shared"
	updated, err := envA.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Title: &comparator.String{Eq: &sharedTitle}},
		&models.UpdateArticleInput{Title: omittable.Set("renamed-by-A")},
	)
	if err != nil {
		t.Fatalf("UpdateWhere as A: %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("UpdateWhere as A: %d rows updated, want 1 (cross-tenant row must be excluded)", len(updated))
	}
	if updated[0].ID != aArticle.ID {
		t.Errorf("UpdateWhere as A: updated row ID = %d, want %d (tenant B's row leaked into result)", updated[0].ID, aArticle.ID)
	}

	// B's row is untouched.
	bGot, err := envB.client.Articles().Get(ctx, bArticle.ID)
	if err != nil {
		t.Fatalf("Get B's article: %v", err)
	}
	if bGot.Title != "shared" {
		t.Errorf("B's article title = %q, want %q (cross-tenant write detected)", bGot.Title, "shared")
	}
}

// TestSoftDeleteWhere_ResolverTenantANDdIntoWhere mirrors the UpdateWhere row-
// count assertion for SoftDeleteWhere — same auto-tenant-filter shape per
// §29.4.2 ("SoftDelete* / HardDelete* / Restore* — same WHERE augmentation").
func TestSoftDeleteWhere_ResolverTenantANDdIntoWhere(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	if _, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "sd-shared"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	bArticle, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "sd-shared"})
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	title := "sd-shared"
	deleted, err := envA.client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Title: &comparator.String{Eq: &title},
	})
	if err != nil {
		t.Fatalf("SoftDeleteWhere as A: %v", err)
	}
	if len(deleted) != 1 {
		t.Errorf("SoftDeleteWhere as A: %d rows soft-deleted, want 1", len(deleted))
	}

	// B's row is still active (not soft-deleted).
	bGot, err := envB.client.Articles().Get(ctx, bArticle.ID)
	if err != nil {
		t.Fatalf("Get B's article: %v", err)
	}
	if bGot == nil {
		t.Fatal("B's article unexpectedly soft-deleted by A's SoftDeleteWhere")
	}
}

// TestHardDeleteWhere_ResolverTenantANDdIntoWhere mirrors the row-count
// assertion for HardDeleteWhere.
func TestHardDeleteWhere_ResolverTenantANDdIntoWhere(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	if _, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "hd-shared"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	bArticle, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "hd-shared"})
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	title := "hd-shared"
	if err := envA.client.Articles().HardDeleteWhere(ctx, &models.ArticleFilter{
		Title: &comparator.String{Eq: &title},
	}); err != nil {
		t.Fatalf("HardDeleteWhere as A: %v", err)
	}

	// B's row still exists.
	bGot, err := envB.client.Articles().Get(ctx, bArticle.ID)
	if err != nil {
		t.Fatalf("Get B's article: %v", err)
	}
	if bGot == nil {
		t.Fatal("B's article unexpectedly hard-deleted by A's HardDeleteWhere")
	}
}
