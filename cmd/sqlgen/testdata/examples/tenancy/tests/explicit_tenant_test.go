package tests

import (
	"context"
	"errors"
	"testing"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Explicit tenant (PRD §29.4.4 third resolution mode).
// CallOptions gains a concretely-typed `Tenant *uuid.UUID`: setting it
// resolves tenancy to that constant — SQL filter, auto-set, and verify-match all apply exactly as if
// the ctx resolver had returned it — without consulting (or even having) a
// resolver. Explicit wins over SkipTenancy; disagreement with an
// input-carried tenant column raises tenancy.ErrMismatch.

// A client with NO ctx resolver creates and reads scoped to the
// explicit tenant, and the mutation evicts the exact tenant-scoped cache
// entry (the explicit value feeds the same tenant capture; no new
// invalidation logic).
func TestExplicitTenant_NoResolverScopedAndEvictsExactKey(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	// Shared cache: envB (resolver B) warms entries; the explicit-tenant
	// client carries no resolver at all.
	envA, envB := sharedCacheEnvs(t)
	explicit := models.New(
		dbstdlib.New(testDB),
		models.WithCache(envA.cache),
	)

	// Create as tenant B via the explicit option only.
	created, err := explicit.Articles().Create(
		ctx, &models.CreateArticleInput{Title: "explicit-b"},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantB) },
	)
	if err != nil {
		t.Fatalf("explicit-tenant create (no resolver): %v", err)
	}

	// The row was auto-set to tenant B: envB sees it, envA does not.
	if _, err := envB.client.Articles().Get(ctx, created.ID); err != nil {
		t.Fatalf("Get as tenant B: %v (auto-set to the explicit tenant failed)", err)
	}
	if _, err := envA.client.Articles().Get(ctx, created.ID); err == nil {
		t.Errorf("Get as tenant A: err = nil, want not-found (row must be scoped to explicit tenant B)")
	}

	// A read through the explicit option is scoped the same way.
	if _, err := explicit.Articles().Get(
		ctx, created.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantB) },
	); err != nil {
		t.Fatalf("explicit-tenant Get: %v", err)
	}

	// Warm S (a second B row) and update the first via the explicit client:
	// the exact tenant-B key is evicted, S survives.
	s, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "explicit-s"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm S: %v", err)
	}
	if _, err := explicit.Articles().Update(
		ctx, created.ID,
		&models.UpdateArticleInput{Title: omittable.Set("explicit-b2")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantB) },
	); err != nil {
		t.Fatalf("explicit-tenant update: %v", err)
	}
	got, err := envB.client.Articles().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("re-Get: %v", err)
	}
	if got.Title != "explicit-b2" {
		t.Errorf("re-Get title = %q, want %q (stale entry survived the explicit-tenant update)", got.Title, "explicit-b2")
	}
	assertCacheHit(t, envA.metrics, "Get S after explicit-tenant eviction", func() {
		if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("Get S: %v", err)
		}
	})
	assertNoFallbackSignal(t, envA.metrics)
}

// An explicit tenant that disagrees with the input's tenant column raises
// tenancy.ErrMismatch and mutates nothing (the same verify-match guard normal
// resolution applies).
func TestExplicitTenant_InputDisagreementIsErrMismatch(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	client := models.New(dbstdlib.New(testDB))

	// Create: explicit X, input column Y.
	_, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:       "mismatch",
		WorkspaceID: omittable.Set(tenantB),
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantA) })
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("create with disagreeing explicit tenant: err = %v, want tenancy.ErrMismatch", err)
	}
	count, err := client.Articles().Count(
		ctx, nil,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("row count after ErrMismatch create = %d, want 0 (must not mutate)", count)
	}

	// Update: seed a row as A, then update with explicit A but input column B.
	created, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "mismatch-seed"},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantA) })
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	_, err = client.Articles().Update(ctx, created.ID, &models.UpdateArticleInput{
		Title:       omittable.Set("changed"),
		WorkspaceID: omittable.Set(tenantB),
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantA) })
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("update with disagreeing input tenant: err = %v, want tenancy.ErrMismatch", err)
	}
	got, err := client.Articles().Get(ctx, created.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantA) })
	if err != nil {
		t.Fatalf("re-Get: %v", err)
	}
	if got.Title != "mismatch-seed" {
		t.Errorf("title after ErrMismatch update = %q, want %q (must not mutate)", got.Title, "mismatch-seed")
	}
}

// Explicit tenant wins over SkipTenancy: with both set the
// call stays scoped to the explicit tenant, without error. Scoping is proven
// negatively: updating another tenant's row with explicit X + SkipTenancy
// returns ErrNotFound (strict updates) — if SkipTenancy had won, the
// cross-tenant update would have succeeded.
func TestExplicitTenant_ExplicitWinsOverSkipTenancy(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	client := models.New(dbstdlib.New(testDB))

	rowA, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "wins-a"},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantA) })
	if err != nil {
		t.Fatalf("create A row: %v", err)
	}
	rowB, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "wins-b"},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantB) })
	if err != nil {
		t.Fatalf("create B row: %v", err)
	}

	// Both set, targeting the explicit tenant's own row: scoped, no error.
	updated, err := client.Articles().Update(
		ctx, rowA.ID,
		&models.UpdateArticleInput{Title: omittable.Set("wins-a2")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) {
			o.Tenant = new(tenantA)
			o.SkipTenancy = true
		},
	)
	if err != nil {
		t.Fatalf("explicit+skip update of own row: %v (explicit must win, scoped, no error)", err)
	}
	if updated.Title != "wins-a2" {
		t.Errorf("updated title = %q, want %q", updated.Title, "wins-a2")
	}

	// Both set, targeting the OTHER tenant's row: the explicit tenant's
	// filter applies, so strict updates report not-found. A SkipTenancy win
	// would have updated cross-tenant.
	_, err = client.Articles().Update(
		ctx, rowB.ID,
		&models.UpdateArticleInput{Title: omittable.Set("cross-tenant")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) {
			o.Tenant = new(tenantA)
			o.SkipTenancy = true
		},
	)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("explicit+skip update of other tenant's row: err = %v, want ErrNotFound (explicit scope must win over SkipTenancy)", err)
	}
	got, err := client.Articles().Get(ctx, rowB.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.Tenant = new(tenantB) })
	if err != nil {
		t.Fatalf("re-Get B row: %v", err)
	}
	if got.Title != "wins-b" {
		t.Errorf("B row title = %q, want %q (cross-tenant update must not have happened)", got.Title, "wins-b")
	}
}
