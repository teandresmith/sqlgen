package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Tenancy ErrMissing sweep — PRD §29.3.1 fail-closed contract. Every
// entry-point method on a tenanted entity MUST surface tenancy.ErrMissing
// (errors.Is-checkable) when the resolver returns it, and MUST NOT issue any
// DB round-trip (asserted via countingQuerier). This file exercises each of
// the 18 public entry points on `articles` (simple PK, timestamp soft delete —
// gives us Restore in addition to SoftDelete/HardDelete) plus `products` for
// the Increment variant, which articles does not expose (articles has no
// numeric column).
//
// ---------------------------------------------------------------------------
// Scope note — tenancy.required:false
// ---------------------------------------------------------------------------
// PRD §29.2 specifies: when `tenancy.required: false` and the resolver returns
// a zero value, the runtime skips the tenant filter — effectively
// cross-tenant by explicit project opt-in. This file covers only the
// fail-closed (required:true) ErrMissing sweep; the required:false branch is
// covered by the `TestRequiredFalse_*` tests in required_false_test.go.

// ---------------------------------------------------------------------------
// Helpers — build envs with a resolver that always returns ErrMissing, and
// seed a couple of rows under a happy-path resolver so the ErrMissing attempt
// has real data to *not* touch.
// ---------------------------------------------------------------------------

// seedArticle creates one article under tenantA so methods that require a
// target row (Update, Restore, HardDelete, etc.) have a PK to aim at. The
// seeded envA must be discarded before the ErrMissing assertions run —
// otherwise the counter from envA captures the seed's DB ops.
func seedArticle(t *testing.T) *models.Article {
	t.Helper()
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	a, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "seed", Body: omittable.Set(new("body")),
	})
	if err != nil {
		t.Fatalf("seed article: %v", err)
	}
	return a
}

// seedProduct creates one product for the Increment-ErrMissing test.
func seedProduct(t *testing.T) *models.Product {
	t.Helper()
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	p, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "seed", SKU: "SEED-INC", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	return p
}

// assertMissing checks that err wraps tenancy.ErrMissing AND that the counter
// recorded zero DB ops (fail-closed before the round-trip, §29.3.1).
func assertMissing(t *testing.T, err error, counter *countingQuerier, op string) {
	t.Helper()
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("%s: err = %v, want wraps tenancy.ErrMissing", op, err)
	}
	if got := counter.totalDBOps(); got != 0 {
		t.Errorf("%s: %d DB ops issued on fail-closed path, want 0", op, got)
	}
}

// ---------------------------------------------------------------------------
// The sweep — one test per entry point.
// ---------------------------------------------------------------------------

func TestErrMissing_Get(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().Get(context.Background(), seed.ID)
	assertMissing(t, err, env.counter, "Get")
}

func TestErrMissing_GetMany(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().GetMany(context.Background(), &models.GetArticlesInput{})
	assertMissing(t, err, env.counter, "GetMany")
}

func TestErrMissing_Connection(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	first := 10
	_, err := env.client.Articles().Connection(context.Background(), models.ConnectionInput[models.ArticleFilter]{
		First: &first,
	})
	assertMissing(t, err, env.counter, "Connection")
}

func TestErrMissing_Create(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().Create(context.Background(), &models.CreateArticleInput{
		Title: "noop",
	})
	assertMissing(t, err, env.counter, "Create")
}

func TestErrMissing_CreateMany(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().CreateMany(context.Background(), []*models.CreateArticleInput{
		{Title: "a"}, {Title: "b"},
	})
	assertMissing(t, err, env.counter, "CreateMany")
}

func TestErrMissing_Update(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().Update(context.Background(), seed.ID, &models.UpdateArticleInput{
		Title: omittable.Set("updated"),
	})
	assertMissing(t, err, env.counter, "Update")
}

func TestErrMissing_UpdateMany(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().UpdateMany(context.Background(), []models.UpdateArticleItem{
		{ID: seed.ID, Input: &models.UpdateArticleInput{Title: omittable.Set("x")}},
	})
	assertMissing(t, err, env.counter, "UpdateMany")
}

func TestErrMissing_UpdateWhere(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	title := "noop"
	_, err := env.client.Articles().UpdateWhere(
		context.Background(),
		&models.ArticleFilter{Title: &comparator.String{Eq: &title}},
		&models.UpdateArticleInput{Title: omittable.Set("x")},
	)
	assertMissing(t, err, env.counter, "UpdateWhere")
}

func TestErrMissing_Upsert(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().Upsert(context.Background(), &models.CreateArticleInput{
		Title: "upsert-nope",
	}, models.ArticleConflictPK)
	assertMissing(t, err, env.counter, "Upsert")
}

func TestErrMissing_HardDelete(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	err := env.client.Articles().HardDelete(context.Background(), seed.ID)
	assertMissing(t, err, env.counter, "HardDelete")
}

func TestErrMissing_HardDeleteWhere(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	title := "seed"
	err := env.client.Articles().HardDeleteWhere(
		context.Background(),
		&models.ArticleFilter{Title: &comparator.String{Eq: &title}},
	)
	assertMissing(t, err, env.counter, "HardDeleteWhere")
}

func TestErrMissing_SoftDelete(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().SoftDelete(context.Background(), seed.ID)
	assertMissing(t, err, env.counter, "SoftDelete")
}

func TestErrMissing_SoftDeleteWhere(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	title := "seed"
	_, err := env.client.Articles().SoftDeleteWhere(
		context.Background(),
		&models.ArticleFilter{Title: &comparator.String{Eq: &title}},
	)
	assertMissing(t, err, env.counter, "SoftDeleteWhere")
}

func TestErrMissing_Restore(t *testing.T) {
	resetDB(t)
	// Restore needs a soft-deleted row to target; we seed one and soft-delete
	// it under envA so the missingResolver-backed env below attempts a restore
	// on a row that genuinely exists-but-deleted. The fail-closed path should
	// trip before any SQL runs regardless of row state, but seeding makes the
	// test's intent (a real restore attempt, not a synthetic one) obvious.
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()
	a, err := envA.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "to-restore"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := envA.client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err = env.client.Articles().Restore(ctx, a.ID)
	assertMissing(t, err, env.counter, "Restore")
}

func TestErrMissing_RestoreWhere(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	title := "seed"
	_, err := env.client.Articles().RestoreWhere(
		context.Background(),
		&models.ArticleFilter{Title: &comparator.String{Eq: &title}},
	)
	assertMissing(t, err, env.counter, "RestoreWhere")
}

// TestErrMissing_Increment targets products (articles has no numeric column;
// see the entity-interface in models_gen.go — Increment is only emitted for
// entities with at least one eligible numeric column per PRD §9.2).
func TestErrMissing_Increment(t *testing.T) {
	resetDB(t)
	seed := seedProduct(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	err := env.client.Products().Increment(context.Background(), seed.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementPrice,
		Amount: 1,
	})
	assertMissing(t, err, env.counter, "Increment")
}

func TestErrMissing_Exists(t *testing.T) {
	resetDB(t)
	seed := seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.Articles().Exists(context.Background(), seed.ID)
	assertMissing(t, err, env.counter, "Exists")
}

func TestErrMissing_ExistsWhere(t *testing.T) {
	resetDB(t)
	_ = seedArticle(t)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	title := "seed"
	_, err := env.client.Articles().ExistsWhere(
		context.Background(),
		&models.ArticleFilter{Title: &comparator.String{Eq: &title}},
	)
	assertMissing(t, err, env.counter, "ExistsWhere")
}

// ---------------------------------------------------------------------------
// Bonus coverage — composite-PK entity (order_items) on a few key methods,
// to confirm the sweep holds for the verify-match-wrapped path too. The
// §29.7 composite-PK mismatch tests already prove ErrMismatch fires before
// resolveTenant (PK-supplied tenant is checked vs resolver result); here we
// verify that when the resolver itself errors, that is what surfaces — not
// ErrMismatch and not a silent success.
// ---------------------------------------------------------------------------

func TestErrMissing_CompositePK_Get(t *testing.T) {
	resetDB(t)
	// Seed an order item so the PK we pass identifies a real row — the
	// fail-closed path should still trip before the DB is touched.
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	item := createOrderItem(t, envA, context.Background(), tenantA, 1, 10, 2)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.OrderItems().Get(context.Background(), models.OrderItemPK{
		// The resolver errors before any PK comparison — pk.WorkspaceID value
		// here is irrelevant to the outcome.
		WorkspaceID: uuid.Nil(),
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	})
	assertMissing(t, err, env.counter, "OrderItems.Get")
}

func TestErrMissing_CompositePK_Update(t *testing.T) {
	resetDB(t)
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	item := createOrderItem(t, envA, context.Background(), tenantA, 1, 10, 2)

	env := newEnv(t, withResolver(missingResolver()))
	env.counter.reset()
	_, err := env.client.OrderItems().Update(context.Background(), models.OrderItemPK{
		WorkspaceID: tenantA,
		OrderID:     item.OrderID,
		ProductID:   item.ProductID,
	}, &models.UpdateOrderItemInput{
		Quantity: omittable.Set(int64(99)),
	})
	assertMissing(t, err, env.counter, "OrderItems.Update")
}
