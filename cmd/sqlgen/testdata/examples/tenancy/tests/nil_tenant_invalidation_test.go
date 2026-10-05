package tests

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Cache invalidation without a resolved tenant (PRD §27.9/§29.5).
// A mutation whose tenant was never resolved
// (CallOptions.SkipTenancy, or tenancy.required:false with a zero resolver)
// used to leave m.Tenant nil and either hard-error, silently skip, or
// over-evict. Invalidation now rebuilds every tenant-scoped key from the
// mutated row's own captured tenant, so it is precise on every shape and can
// never fail a committed write; the cross-tenant table pattern survives only
// as a defensive net that must not fire in normal operation.

// sharedCacheEnvs returns two tenant-scoped envs sharing one cache backend +
// metrics — the shape every precision assertion needs (evicting tenant A's
// entry must not touch tenant B's on the same backend).
func sharedCacheEnvs(t *testing.T) (*testEnv, *testEnv) {
	t.Helper()
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB = rewireCache(t, envB, envA.cache)
	return envA, envB
}

// assertNoFallbackSignal pins the converse of the forced-gap test: the
// defensive table-pattern fallback must be dead across the normal shapes.
func assertNoFallbackSignal(t *testing.T, m *spyMetrics) {
	t.Helper()
	if n := m.errorOpCount("invalidate_tenant_fallback"); n != 0 {
		t.Errorf("defensive fallback signal fired %d times, want 0 (row-derived eviction must stay precise in normal operation)", n)
	}
}

// assertCacheHit runs fn and asserts it produced exactly one cache hit and no
// miss — proving the entry it touched was still cached (untouched by a
// preceding eviction).
func assertCacheHit(t *testing.T, m *spyMetrics, what string, fn func()) {
	t.Helper()
	hitsBefore, missesBefore := m.hitCount(), m.missCount()
	fn()
	if got := m.hitCount() - hitsBefore; got != 1 {
		t.Errorf("%s: cache hits delta = %d, want 1 (entry should have survived)", what, got)
	}
	if got := m.missCount() - missesBefore; got != 0 {
		t.Errorf("%s: cache misses delta = %d, want 0 (entry should have survived)", what, got)
	}
}

// A SkipTenancy update on a tenant-∉-PK table evicts exactly the mutated
// row's tenant-scoped entry. The update runs through tenant B's client so the
// resolver value (B) actively disagrees with the row's tenant (A): only the
// row-derived key can be right.
func TestNilTenant_SkipTenancyUpdateEvictsExactEntry(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	r, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "r-original"})
	if err != nil {
		t.Fatalf("create R: %v", err)
	}
	s, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "s-original"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	// Creates write through; explicit Gets pin both entries as warm.
	if _, err := envA.client.Articles().Get(ctx, r.ID); err != nil {
		t.Fatalf("warm R: %v", err)
	}
	if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm S: %v", err)
	}

	if _, err := envB.client.Articles().Update(
		ctx, r.ID,
		&models.UpdateArticleInput{Title: omittable.Set("r-updated")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy update of R: %v", err)
	}

	// R's exact entry is gone: a re-Get must observe the new value (a stale
	// surviving entry would serve "r-original" from cache).
	got, err := envA.client.Articles().Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("re-Get R: %v", err)
	}
	if got.Title != "r-updated" {
		t.Errorf("re-Get R title = %q, want %q (stale cache entry survived the SkipTenancy update)", got.Title, "r-updated")
	}

	// S is untouched — precise eviction, not the table pattern.
	assertCacheHit(t, envA.metrics, "Get S after SkipTenancy update of R", func() {
		if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("Get S: %v", err)
		}
	})
	assertNoFallbackSignal(t, envA.metrics)
}

// SkipTenancy soft and hard delete evict the exact entry; the sibling
// tenant's entry survives. A stale entry after delete is the worst failure
// shape: the cache would keep serving a row the DB no longer returns.
func TestNilTenant_SkipTenancyDeleteEvictsExactEntry(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	r, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "del-soft"})
	if err != nil {
		t.Fatalf("create R: %v", err)
	}
	r2, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "del-hard"})
	if err != nil {
		t.Fatalf("create R2: %v", err)
	}
	s, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "del-s"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	for _, warm := range []struct {
		env *testEnv
		id  int64
	}{{envA, r.ID}, {envA, r2.ID}, {envB, s.ID}} {
		if _, err := warm.env.client.Articles().Get(ctx, warm.id); err != nil {
			t.Fatalf("warm %d: %v", warm.id, err)
		}
	}

	// Soft delete under SkipTenancy through the disagreeing client.
	if _, err := envB.client.Articles().SoftDelete(
		ctx, r.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy soft delete: %v", err)
	}
	if _, err := envA.client.Articles().Get(ctx, r.ID); err == nil {
		t.Errorf("Get soft-deleted R: err = nil — stale cache entry served a deleted row")
	}

	// Hard delete under SkipTenancy.
	if err := envB.client.Articles().HardDelete(
		ctx, r2.ID,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy hard delete: %v", err)
	}
	if _, err := envA.client.Articles().Get(ctx, r2.ID); err == nil {
		t.Errorf("Get hard-deleted R2: err = nil — stale cache entry served a deleted row")
	}

	assertCacheHit(t, envA.metrics, "Get S after SkipTenancy deletes", func() {
		if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("Get S: %v", err)
		}
	})
	assertNoFallbackSignal(t, envA.metrics)
}

// No invalidation error escapes a committed write in either callback
// mode. Before the rewrite the nil-tenant path hard-errored inside the
// post-commit callback: silently dropped under CallbackAsync, surfaced as a
// spurious WithTx failure under CallbackSync. Both modes must now return nil,
// keep the commit durable, and log nothing.
func TestNilTenant_NoCallbackErrorEscapesCommittedWrite(t *testing.T) {
	resetDB(t)
	envA, _ := sharedCacheEnvs(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		mode database.CallbackMode
	}{
		{"CallbackSync", database.CallbackSync},
		{"CallbackAsync", database.CallbackAsync},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := models.New(
				dbstdlib.New(testDB),
				models.WithTenantResolver(staticResolver(tenantA)),
				models.WithCache(envA.cache),
				models.WithCallbackMode(tt.mode),
			)

			r, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "cb-" + tt.name})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := client.Articles().Get(ctx, r.ID); err != nil {
				t.Fatalf("warm: %v", err)
			}

			wantTitle := "cb-updated-" + tt.name
			err = client.WithTx(ctx, "cb_"+tt.name, func(txCtx context.Context) error {
				_, updateErr := client.Articles().Update(
					txCtx, r.ID,
					&models.UpdateArticleInput{Title: omittable.Set(wantTitle)},
					func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
				)
				return updateErr
			})
			if err != nil {
				t.Fatalf("WithTx (%s) = %v, want nil — a committed write surfaced a cache bookkeeping failure", tt.name, err)
			}

			// (b) the commit is durable — read straight from the DB.
			got, err := client.Articles().Get(
				ctx, r.ID,
				func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipCache = true },
			)
			if err != nil {
				t.Fatalf("post-commit Get (SkipCache): %v", err)
			}
			if got.Title != wantTitle {
				t.Errorf("post-commit DB title = %q, want %q (commit not durable)", got.Title, wantTitle)
			}

			// The stale entry is evicted. Under CallbackAsync the post-commit
			// callback runs on a detached goroutine, so poll until it lands.
			deadline := time.Now().Add(2 * time.Second)
			for {
				cached, err := client.Articles().Get(ctx, r.ID)
				if err != nil {
					t.Fatalf("post-commit Get: %v", err)
				}
				if cached.Title == wantTitle {
					break
				}
				if time.Now().After(deadline) {
					t.Errorf("post-commit cached title = %q, want %q (stale entry not evicted within 2s)", cached.Title, wantTitle)
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}

	// (d) nothing was logged through the cache error channel — the old
	// "tenant type mismatch" hard error is gone, not just rerouted.
	if n := envA.metrics.errorCount(); n != 0 {
		t.Errorf("cache error signals = %d, want 0 (ops: %v)", n, envA.metrics.errorOps)
	}
}

// The second nil-tenant path: tenancy.required:false
// with a zero resolver leaves m.Tenant nil with no SkipTenancy anywhere. The
// row-derived key must still evict the exact entry.
func TestNilTenant_RequiredFalseZeroTenantEvictsExactEntry(t *testing.T) {
	resetDB(t)
	envReal := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	envZero := newEnv(t, withResolver(staticResolver(uuid.UUID{})))
	envZero = rewireCache(t, envZero, envReal.cache)
	ctx := context.Background()

	w, err := envReal.client.LegacyWidgets().Create(ctx, &models.CreateLegacyWidgetInput{Label: "w-original"})
	if err != nil {
		t.Fatalf("create widget: %v", err)
	}
	w2, err := envReal.client.LegacyWidgets().Create(ctx, &models.CreateLegacyWidgetInput{Label: "w2"})
	if err != nil {
		t.Fatalf("create widget 2: %v", err)
	}
	if _, err := envReal.client.LegacyWidgets().Get(ctx, w.ID); err != nil {
		t.Fatalf("warm w: %v", err)
	}
	if _, err := envReal.client.LegacyWidgets().Get(ctx, w2.ID); err != nil {
		t.Fatalf("warm w2: %v", err)
	}

	// legacy_widgets has tenancy.required: false — the zero resolver opts
	// this call out of tenancy (apply=false, m.Tenant nil), no SkipTenancy.
	if _, err := envZero.client.LegacyWidgets().Update(
		ctx, w.ID,
		&models.UpdateLegacyWidgetInput{Label: omittable.Set("w-updated")},
	); err != nil {
		t.Fatalf("zero-resolver update: %v", err)
	}

	got, err := envReal.client.LegacyWidgets().Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("re-Get w: %v", err)
	}
	if got.Label != "w-updated" {
		t.Errorf("re-Get w label = %q, want %q (stale entry survived the required:false zero-tenant update)", got.Label, "w-updated")
	}
	assertCacheHit(t, envReal.metrics, "Get w2 after zero-tenant update of w", func() {
		if _, err := envReal.client.LegacyWidgets().Get(ctx, w2.ID); err != nil {
			t.Fatalf("Get w2: %v", err)
		}
	})
	assertNoFallbackSignal(t, envReal.metrics)
}

// Tenant-in-PK tables are precise on every shape: the tenant rides in
// each AffectedPK struct, so single-row, *Many, and *Where mutations under
// SkipTenancy evict exactly the affected rows' keys.
func TestNilTenant_TenantInPKPreciseAllShapes(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	mk := func(env *testEnv, ws uuid.UUID, order, product int64) models.OrderItemPK {
		t.Helper()
		if _, err := env.client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
			WorkspaceID: ws, OrderID: order, ProductID: product, Quantity: 1, UnitPrice: 2.5,
		}); err != nil {
			t.Fatalf("create order item (%s/%d/%d): %v", ws, order, product, err)
		}
		pk := models.OrderItemPK{WorkspaceID: ws, OrderID: order, ProductID: product}
		if _, err := env.client.OrderItems().Get(ctx, pk); err != nil {
			t.Fatalf("warm order item (%s/%d/%d): %v", ws, order, product, err)
		}
		return pk
	}
	pkA := mk(envA, tenantA, 1, 1)
	pkB := mk(envB, tenantB, 1, 1)
	pkB2 := mk(envB, tenantB, 2, 1)

	// Single-row update of tenant B's row through tenant A's client under
	// SkipTenancy — the tenant for the key comes from the PK struct.
	if _, err := envA.client.OrderItems().Update(
		ctx, pkB,
		&models.UpdateOrderItemInput{Quantity: omittable.Set(int64(42))},
		func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy update of pkB: %v", err)
	}
	got, err := envB.client.OrderItems().Get(ctx, pkB)
	if err != nil {
		t.Fatalf("re-Get pkB: %v", err)
	}
	if got.Quantity != 42 {
		t.Errorf("re-Get pkB quantity = %d, want 42 (stale entry survived)", got.Quantity)
	}
	assertCacheHit(t, envA.metrics, "Get pkA after single-row eviction", func() {
		if _, err := envA.client.OrderItems().Get(ctx, pkA); err != nil {
			t.Fatalf("Get pkA: %v", err)
		}
	})

	// *Many across both tenants under SkipTenancy — each row's key evicted
	// from its own PK-carried tenant; pkB2 untouched.
	if _, err := envA.client.OrderItems().UpdateMany(ctx, []models.UpdateOrderItemItem{
		{PK: pkA, Input: &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(7))}},
		{PK: pkB, Input: &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(8))}},
	}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}
	gotA, err := envA.client.OrderItems().Get(ctx, pkA)
	if err != nil {
		t.Fatalf("re-Get pkA: %v", err)
	}
	gotB, err := envB.client.OrderItems().Get(ctx, pkB)
	if err != nil {
		t.Fatalf("re-Get pkB: %v", err)
	}
	if gotA.Quantity != 7 || gotB.Quantity != 8 {
		t.Errorf("post-UpdateMany quantities = (%d, %d), want (7, 8) (stale entries survived)", gotA.Quantity, gotB.Quantity)
	}
	assertCacheHit(t, envA.metrics, "Get pkB2 after *Many eviction", func() {
		if _, err := envB.client.OrderItems().Get(ctx, pkB2); err != nil {
			t.Fatalf("Get pkB2: %v", err)
		}
	})

	// *Where matching order 1 across tenants under SkipTenancy — precise
	// per-affected-PK eviction (the PK structs come from the widened
	// capture); pkB2 (order 2) untouched.
	orderOne := int64(1)
	if _, err := envA.client.OrderItems().UpdateWhere(
		ctx,
		&models.OrderItemFilter{OrderID: &comparator.Number[int64]{Eq: &orderOne}},
		&models.UpdateOrderItemInput{UnitPrice: omittable.Set(9.99)},
		func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy UpdateWhere: %v", err)
	}
	gotB, err = envB.client.OrderItems().Get(ctx, pkB)
	if err != nil {
		t.Fatalf("re-Get pkB after *Where: %v", err)
	}
	if gotB.UnitPrice != 9.99 {
		t.Errorf("post-UpdateWhere pkB unit price = %v, want 9.99 (stale entry survived)", gotB.UnitPrice)
	}
	assertCacheHit(t, envA.metrics, "Get pkB2 after *Where eviction", func() {
		if _, err := envB.client.OrderItems().Get(ctx, pkB2); err != nil {
			t.Fatalf("Get pkB2: %v", err)
		}
	})

	// Hard delete under SkipTenancy — entry gone, not stale.
	if err := envA.client.OrderItems().HardDelete(
		ctx, pkB,
		func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy hard delete pkB: %v", err)
	}
	if _, err := envB.client.OrderItems().Get(ctx, pkB); err == nil {
		t.Errorf("Get hard-deleted pkB: err = nil — stale cache entry served a deleted row")
	}
	assertNoFallbackSignal(t, envA.metrics)
}

// A SkipTenancy create writes through under the row's actual tenant key
// (previously it silently skipped because m.Tenant was nil); a partial
// FieldOptions create must not populate the cache at all — the tenant is
// captured structurally, never read off a possibly-partial entity.
func TestNilTenant_CreateWriteThroughUnderSkipTenancy(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	setsBefore := envA.metrics.setCount()
	created, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:       "wt-full",
		WorkspaceID: omittable.Set(tenantB),
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("SkipTenancy create: %v", err)
	}
	if got := envA.metrics.setCount() - setsBefore; got != 1 {
		t.Errorf("write-through sets delta = %d, want 1 (SkipTenancy create no longer skips)", got)
	}
	// The entry must live under tenant B's key: tenant B's first Get is a HIT.
	assertCacheHit(t, envA.metrics, "tenant B Get of SkipTenancy-created row", func() {
		if _, err := envB.client.Articles().Get(ctx, created.ID); err != nil {
			t.Fatalf("Get created: %v", err)
		}
	})

	// Partial-FieldOptions variant: nothing may be cached — a
	// partial entity must never feed the write-through key or value.
	if _, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:       "wt-partial",
		WorkspaceID: omittable.Set(tenantB),
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) {
		o.SkipTenancy = true
		o.FieldOptions = &models.ArticleFieldOptions{Title: true}
	}); err != nil {
		t.Fatalf("partial SkipTenancy create: %v", err)
	}
	// The partial result carries no ID (not selected) — recover the row's PK
	// through tenant B's client.
	title := "wt-partial"
	rows, err := envB.client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{Title: &comparator.String{Eq: &title}},
	})
	if err != nil || len(rows) != 1 {
		t.Fatalf("locate partial-created row: rows=%d err=%v", len(rows), err)
	}
	missesBefore := envA.metrics.missCount()
	if _, err := envB.client.Articles().Get(ctx, rows[0].ID); err != nil {
		t.Fatalf("Get partial-created: %v", err)
	}
	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("Get after partial create: misses delta = %d, want 1 (partial create must not populate the cache)", got)
	}
	assertNoFallbackSignal(t, envA.metrics)
}

// *Where and *Many on a tenant-∉-PK table evict precisely under
// SkipTenancy: every affected row's own tenant key goes, unaffected rows'
// entries survive. (The MySQL leg of (b) — the batched pre-read — runs on the
// tenancy_mysql integration example; SQLite covers the RETURNING shape here.)
func TestNilTenant_WhereAndManyEvictPrecisely(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	mk := func(env *testEnv, title string) *models.Article {
		t.Helper()
		a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: title})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
			t.Fatalf("warm %s: %v", title, err)
		}
		return a
	}
	r := mk(envA, "many-r")
	s := mk(envB, "many-s")
	keep := mk(envB, "keep-many")

	// (a) *Where across both tenants under SkipTenancy — matches r and s
	// (title prefix), leaves keep untouched.
	prefix := "many-%"
	body := "where-updated"
	if _, err := envA.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Title: &comparator.String{Like: &prefix}},
		&models.UpdateArticleInput{Body: omittable.Set(new(body))},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy UpdateWhere: %v", err)
	}
	for _, tt := range []struct {
		env *testEnv
		id  int64
	}{{envA, r.ID}, {envB, s.ID}} {
		got, err := tt.env.client.Articles().Get(ctx, tt.id)
		if err != nil {
			t.Fatalf("re-Get %d: %v", tt.id, err)
		}
		if got.Body == nil || *got.Body != body {
			t.Errorf("re-Get %d body = %v, want %q (stale entry survived *Where)", tt.id, got.Body, body)
		}
	}
	assertCacheHit(t, envA.metrics, "Get keep after *Where", func() {
		if _, err := envB.client.Articles().Get(ctx, keep.ID); err != nil {
			t.Fatalf("Get keep: %v", err)
		}
	})

	// (b) *Many across both tenants under SkipTenancy.
	manyBody := "many-updated"
	if _, err := envA.client.Articles().UpdateMany(ctx, []models.UpdateArticleItem{
		{ID: r.ID, Input: &models.UpdateArticleInput{Body: omittable.Set(new(manyBody))}},
		{ID: s.ID, Input: &models.UpdateArticleInput{Body: omittable.Set(new(manyBody))}},
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}
	for _, tt := range []struct {
		env *testEnv
		id  int64
	}{{envA, r.ID}, {envB, s.ID}} {
		got, err := tt.env.client.Articles().Get(ctx, tt.id)
		if err != nil {
			t.Fatalf("re-Get %d: %v", tt.id, err)
		}
		if got.Body == nil || *got.Body != manyBody {
			t.Errorf("re-Get %d body = %v, want %q (stale entry survived *Many)", tt.id, got.Body, manyBody)
		}
	}
	assertCacheHit(t, envA.metrics, "Get keep after *Many", func() {
		if _, err := envB.client.Articles().Get(ctx, keep.ID); err != nil {
			t.Fatalf("Get keep: %v", err)
		}
	})
	assertNoFallbackSignal(t, envA.metrics)
}

// Forcing a capture gap (a synthetic hook nils both m.Tenant and the
// AffectedTenants carrier, simulating an unwired future mutation shape) makes
// invalidation degrade to the cross-tenant table pattern, emit exactly one
// observability signal, and return no error. The converse — the signal never
// fires in normal operation — is asserted at the end of every other test here.
func TestNilTenant_ForcedCaptureGapFallsBackSafely(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	r, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "gap-r"})
	if err != nil {
		t.Fatalf("create R: %v", err)
	}
	s, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "gap-s"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	if _, err := envA.client.Articles().Get(ctx, r.ID); err != nil {
		t.Fatalf("warm R: %v", err)
	}
	if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm S: %v", err)
	}

	// suppressCapture runs inside the cache hook (user hooks are inner), so
	// nil-ing the carriers after the terminal returns is exactly what an
	// unwired mutation shape would hand the invalidation path.
	suppressCapture := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if err == nil && m.Table == models.TableArticles {
				m.Tenant = nil
				m.AffectedTenants = nil
			}
			return res, err
		}
	}
	suppressed := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithCache(envA.cache),
		models.WithMutationHook(suppressCapture),
	)

	if _, err := suppressed.Articles().Update(
		ctx, r.ID,
		&models.UpdateArticleInput{Title: omittable.Set("gap-r-updated")},
	); err != nil {
		t.Fatalf("update with suppressed capture = %v, want nil (the fallback must not fail the committed write)", err)
	}

	// Exactly one anomaly signal.
	if n := envA.metrics.errorOpCount("invalidate_tenant_fallback"); n != 1 {
		t.Errorf("fallback signals = %d, want 1", n)
	}

	// Over-eviction proves the table pattern fired: tenant B's unrelated
	// entry is gone too (safe-but-loud, never stale).
	missesBefore := envA.metrics.missCount()
	if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("Get S: %v", err)
	}
	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("Get S after forced fallback: misses delta = %d, want 1 (table pattern should have evicted every tenant's entries)", got)
	}

	// And the mutated row itself is fresh, not stale.
	got, err := envA.client.Articles().Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("re-Get R: %v", err)
	}
	if got.Title != "gap-r-updated" {
		t.Errorf("re-Get R title = %q, want %q", got.Title, "gap-r-updated")
	}
}

// A *Many batch containing a nonexistent PK exercises the per-element
// nil-tenant skip inside invalidateAffectedTenanted: the missing row
// materializes nothing (nil AffectedTenants element — hook contract), so it
// is skipped without touching the defensive fallback, while the real rows'
// entries are still evicted precisely.
func TestNilTenant_ManyWithMissingPKSkipsNilElementWithoutFallback(t *testing.T) {
	resetDB(t)
	envA, envB := sharedCacheEnvs(t)
	ctx := context.Background()

	r, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "skip-r"})
	if err != nil {
		t.Fatalf("create R: %v", err)
	}
	s, err := envB.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "skip-s"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	if _, err := envA.client.Articles().Get(ctx, r.ID); err != nil {
		t.Fatalf("warm R: %v", err)
	}
	if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm S: %v", err)
	}

	// Soft-delete R plus a PK that does not exist, under SkipTenancy. The
	// missing PK yields a nil AffectedTenants element (its row was never
	// materialized); the op must succeed, evict R precisely, and leave both
	// the fallback signal and S untouched.
	const missingID int64 = 999_999
	if _, err := envA.client.Articles().SoftDeleteMany(
		ctx, []int64{r.ID, missingID},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SoftDeleteMany with missing PK: %v", err)
	}

	if _, err := envA.client.Articles().Get(ctx, r.ID); err == nil {
		t.Errorf("Get soft-deleted R: err = nil — stale cache entry served a deleted row")
	}
	assertCacheHit(t, envA.metrics, "Get S after batch with missing PK", func() {
		if _, err := envB.client.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("Get S: %v", err)
		}
	})
	assertNoFallbackSignal(t, envA.metrics)
}
