package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// mkArticle creates and cache-warms one article for the given tenant client.
func mkArticle(t *testing.T, c *models.Client, slug, title string) *models.Article {
	t.Helper()
	ctx := context.Background()
	a, err := c.Articles().Create(ctx, &models.CreateArticleInput{Slug: slug, Title: title})
	if err != nil {
		t.Fatalf("create %s: %v", slug, err)
	}
	if _, err := c.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("warm %s: %v", slug, err)
	}
	return a
}

// MySQL leg — a SkipTenancy UpdateMany across two tenants runs exactly
// one batched pre-read and evicts each row's own tenant-scoped
// key precisely; an untouched third entry survives and the defensive
// fallback never fires.
func TestMySQLCapture_UpdateManyBatchedPreReadEvictsPrecisely(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "r", "r-original")
	s := mkArticle(t, env.clientB, "s", "s-original")
	keep := mkArticle(t, env.clientB, "keep", "keep-original")

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
		t.Errorf("titles after *Many = (%q, %q), want (r-updated, s-updated) — stale entries survived the batched pre-read capture", gotR.Title, gotS.Title)
	}
	assertHit(t, env.metrics, "Get keep after *Many", func() {
		if _, err := env.clientB.Articles().Get(ctx, keep.ID); err != nil {
			t.Fatalf("Get keep: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}

// *Where under SkipTenancy uses the widened collectAffectedIDs pre-SELECT —
// precise per-affected-PK eviction, untouched rows survive.
func TestMySQLCapture_UpdateWhereWidenedCollectEvictsPrecisely(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "w7-r", "w7-r")
	s := mkArticle(t, env.clientB, "w7-s", "w7-s")
	keep := mkArticle(t, env.clientB, "keep", "keep")

	prefix := "w7-%"
	if _, err := env.clientA.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Title: &comparator.String{Like: &prefix}},
		&models.UpdateArticleInput{Title: omittable.Set("w7-updated")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy UpdateWhere: %v", err)
	}

	for _, tt := range []struct {
		c  *models.Client
		id int64
	}{{env.clientA, r.ID}, {env.clientB, s.ID}} {
		got, err := tt.c.Articles().Get(ctx, tt.id)
		if err != nil {
			t.Fatalf("re-Get %d: %v", tt.id, err)
		}
		if got.Title != "w7-updated" {
			t.Errorf("re-Get %d title = %q, want w7-updated (stale entry survived *Where)", tt.id, got.Title)
		}
	}
	assertHit(t, env.metrics, "Get keep after *Where", func() {
		if _, err := env.clientB.Articles().Get(ctx, keep.ID); err != nil {
			t.Fatalf("Get keep: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}

// Soft delete under SkipTenancy exercises the single-row pre-read (and
// regression-pins the err-redeclaration codegen bug this module surfaced:
// the pre-read declares err, the delete Exec must assign, not redeclare).
func TestMySQLCapture_SoftDeletePreReadEvicts(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "sd", "sd")
	s := mkArticle(t, env.clientB, "sd-s", "sd-s")

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

// Composite PK with the tenant OUTSIDE the PK: the batched pre-read keys its
// capture map by the PK struct.
func TestMySQLCapture_CompositeTenantOutsidePKPrecise(t *testing.T) {
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
	assertHit(t, env.metrics, "Get pkKeep after composite *Many", func() {
		if _, err := env.clientB.LineItems().Get(ctx, pkKeep); err != nil {
			t.Fatalf("Get pkKeep: %v", err)
		}
	})
	assertNoFallback(t, env.metrics)
}

// String-typed PK with the tenant outside the PK: single-row pre-read and
// eviction keyed by the string PK.
func TestMySQLCapture_StringPKPrecise(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	if _, err := env.clientA.Documents().Create(ctx, &models.CreateDocumentInput{
		DocKey: "doc-a", Label: "a-original",
	}); err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if _, err := env.clientA.Documents().Get(ctx, "doc-a"); err != nil {
		t.Fatalf("warm doc: %v", err)
	}

	if _, err := env.clientB.Documents().Update(
		ctx, "doc-a",
		&models.UpdateDocumentInput{Label: omittable.Set("a-updated")},
		func(o *models.CallOptions[models.DocumentFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy update (string PK): %v", err)
	}
	got, err := env.clientA.Documents().Get(ctx, "doc-a")
	if err != nil {
		t.Fatalf("re-Get doc: %v", err)
	}
	if got.Label != "a-updated" {
		t.Errorf("label = %q, want a-updated (stale string-PK entry survived)", got.Label)
	}
	assertNoFallback(t, env.metrics)
}

// MySQL leg — the batched *Many pre-read feeds per-row tenant stamps
// into the published events.
func TestMySQLCapture_EventsCarryPerRowTenants(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	client := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithEventPublisher(bus),
	)

	mk := func(tenant, slug string) int64 {
		t.Helper()
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Slug: slug, Title: slug,
			WorkspaceID: omittable.Set(tenant),
		}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true })
		if err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
		return a.ID
	}
	rID := mk(tenantA, "ev-r")
	sID := mk(tenantB, "ev-s")

	events := make([]event.Event, 0, 2)
	var evMu sync.Mutex
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{"articles"}}, func(_ context.Context, e event.Event) error {
		evMu.Lock()
		events = append(events, e)
		evMu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	if _, err := client.Articles().UpdateMany(ctx, []models.UpdateArticleItem{
		{ID: rID, Input: &models.UpdateArticleInput{Title: omittable.Set("ev-r2")}},
		{ID: sID, Input: &models.UpdateArticleInput{Title: omittable.Set("ev-s2")}},
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}

	evMu.Lock()
	got := append([]event.Event(nil), events...)
	evMu.Unlock()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	want := map[int64]string{rID: tenantA, sID: tenantB}
	for _, e := range got {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Fatalf("event PK type %T, want int64", e.PK)
		}
		if e.Metadata == nil || e.Metadata["tenant"] != want[pk] {
			t.Errorf("event for pk %d: Metadata[tenant] = %v, want %q (per-row stamp from the batched pre-read)", pk, e.Metadata, want[pk])
		}
	}
}
