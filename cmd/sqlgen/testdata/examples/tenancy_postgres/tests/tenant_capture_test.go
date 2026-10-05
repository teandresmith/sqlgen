package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

func mkArticle(t *testing.T, c *models.Client, title string) *models.Article {
	t.Helper()
	ctx := context.Background()
	a, err := c.Articles().Create(ctx, &models.CreateArticleInput{Title: title})
	if err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	if _, err := c.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("warm %s: %v", title, err)
	}
	return a
}

// The pgx leg's raison d'être: a SkipTenancy UpdateMany goes through
// SendBatch with per-item `RETURNING workspace_id` scanned via br.Query(),
// and each row's own tenant-scoped key is evicted precisely.
func TestPgxCapture_UpdateManyBatchReturningEvictsPrecisely(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "r-original")
	s := mkArticle(t, env.clientB, "s-original")
	keep := mkArticle(t, env.clientB, "keep-original")

	if _, err := env.clientA.Articles().UpdateMany(ctx, []models.UpdateArticleItem{
		{ID: r.ID, Input: &models.UpdateArticleInput{Title: omittable.Set("r-updated")}},
		{ID: s.ID, Input: &models.UpdateArticleInput{Title: omittable.Set("s-updated")}},
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}

	gotR, err := env.clientA.Articles().Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("re-Get r: %v", err)
	}
	gotS, err := env.clientB.Articles().Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("re-Get s: %v", err)
	}
	if gotR.Title != "r-updated" || gotS.Title != "s-updated" {
		t.Errorf("titles after batch = (%q, %q), want (r-updated, s-updated) — stale entries survived the pgx batch RETURNING capture", gotR.Title, gotS.Title)
	}
	assertHit(t, env.metrics, "Get keep after pgx batch", func() {
		if _, err := env.clientB.Articles().Get(ctx, keep.ID); err != nil {
			t.Fatalf("Get keep: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}

// Composite PK with the tenant outside the PK on the pgx batch path.
func TestPgxCapture_CompositeTenantOutsidePKBatchPrecise(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	mk := func(c *models.Client, order, product int64) models.LineItemPK {
		t.Helper()
		if _, err := c.LineItems().Create(ctx, &models.CreateLineItemInput{
			OrderID: order, ProductID: product, Quantity: 1,
		}); err != nil {
			t.Fatalf("create line item %d/%d: %v", order, product, err)
		}
		pk := models.LineItemPK{OrderID: order, ProductID: product}
		if _, err := c.LineItems().Get(ctx, pk); err != nil {
			t.Fatalf("warm line item %d/%d: %v", order, product, err)
		}
		return pk
	}
	pkA := mk(env.clientA, 1, 1)
	pkB := mk(env.clientB, 2, 1)
	pkKeep := mk(env.clientB, 3, 1)

	if _, err := env.clientA.LineItems().UpdateMany(ctx, []models.UpdateLineItemItem{
		{PK: pkA, Input: &models.UpdateLineItemInput{Quantity: omittable.Set(int64(7))}},
		{PK: pkB, Input: &models.UpdateLineItemInput{Quantity: omittable.Set(int64(8))}},
	}, func(o *models.CallOptions[models.LineItemFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany (composite): %v", err)
	}

	gotA, err := env.clientA.LineItems().Get(ctx, pkA)
	if err != nil {
		t.Fatalf("re-Get pkA: %v", err)
	}
	gotB, err := env.clientB.LineItems().Get(ctx, pkB)
	if err != nil {
		t.Fatalf("re-Get pkB: %v", err)
	}
	if gotA.Quantity != 7 || gotB.Quantity != 8 {
		t.Errorf("quantities = (%d, %d), want (7, 8) — stale composite entries survived", gotA.Quantity, gotB.Quantity)
	}
	assertHit(t, env.metrics, "Get pkKeep after composite batch", func() {
		if _, err := env.clientB.LineItems().Get(ctx, pkKeep); err != nil {
			t.Fatalf("Get pkKeep: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}

// Single-row soft delete under SkipTenancy uses the widened single-statement
// RETURNING on pgx (non-batch path) — exact eviction, sibling survives.
func TestPgxCapture_SoftDeleteReturningEvicts(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "sd")
	s := mkArticle(t, env.clientB, "sd-s")

	if _, err := env.clientB.Articles().SoftDelete(
		ctx, r.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy soft delete: %v", err)
	}
	if _, err := env.clientA.Articles().Get(ctx, r.ID); err == nil {
		t.Errorf("Get soft-deleted row: err = nil — stale cache entry served a deleted row")
	}
	assertHit(t, env.metrics, "Get s after soft delete", func() {
		if _, err := env.clientB.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("Get s: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}
