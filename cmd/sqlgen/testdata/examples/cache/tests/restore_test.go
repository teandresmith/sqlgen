package tests

// Restore-after-soft-delete cache coverage (PRD §27.7).
//
// SoftDeleteWhere and RestoreWhere both fire BuildTablePattern invalidation
// (fingerprint-agnostic). The relevant cache invariants:
//
//  - The cache only stores full entities (PRD §27.6); it does NOT cache
//    "absence" entries. A Get on a soft-deleted row returns ErrNotFound,
//    leaves the cache untouched, and produces no cache entry. The phase
//    task wording "caches absence" is loose — what's actually being
//    asserted is that no stale "live" entity survives the soft-delete and
//    that RestoreWhere primes a clean lookup path.
//
//  - SoftDeleteWhere → invalidate_pattern clears any cached "live" entry
//    for the row.
//  - RestoreWhere → invalidate_pattern clears any cached state, so the
//    follow-up Get round-trips to the DB and caches the restored entity
//    fresh.
//
// The end-to-end shape this covers: Cache(live) → SoftDeleteWhere →
// Get(ErrNotFound, no cache) → RestoreWhere → Get(restored, cache fresh).

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestRestore_AfterSoftDeleteWhere_GetServesFreshRow walks the full lifecycle:
// warm cache → SoftDeleteWhere → Get returns ErrNotFound (and writes nothing
// to cache) → RestoreWhere → Get round-trips DB and serves the live row.
func TestRestore_AfterSoftDeleteWhere_GetServesFreshRow(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Live",
		Author: "restore-test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Articles().HardDelete(ctx, a.ID) })

	// Warm the cache with the live entity.
	if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("warm Get: %v", err)
	}
	hitsBefore := env.metrics.hitCount()
	if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("warm Get sanity: %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Fatalf("warm sanity: cache not hot — test setup invalid")
	}

	// SoftDeleteWhere over the matching predicate. Evicts by key (one
	// invalidate_many over the matched PKs) — the live cache entry
	// is gone.
	env.backend.reset()
	if _, err := env.client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: new("restore-test")},
	}); err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}
	if got := len(env.backend.filterCalls("invalidate_many")); got != 1 {
		t.Errorf("SoftDeleteWhere: want 1 invalidate_many, got %d", got)
	}
	if got := len(env.backend.filterCalls("invalidate_pattern")); got != 0 {
		t.Errorf("SoftDeleteWhere: want 0 invalidate_pattern, got %d", got)
	}

	// Post-soft-delete Get returns ErrNotFound (the SQL builder excludes
	// soft-deleted rows by default per §17). Crucially, the cache hook
	// does not store ErrNotFound results — the cache invariant only
	// permits full entities. Verify no Set call landed.
	env.backend.reset()
	missesBefore := env.metrics.missCount()
	if _, err := env.client.Articles().Get(ctx, a.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("Get post-soft-delete: err = %v, want ErrNotFound", err)
	}
	if env.metrics.missCount()-missesBefore != 1 {
		t.Errorf("post-soft-delete Get: misses delta = %d, want 1", env.metrics.missCount()-missesBefore)
	}
	if got := len(env.backend.filterCalls("set")); got != 0 {
		t.Errorf("post-soft-delete Get: backend Set calls = %d, want 0 (absence must NOT be cached per §27.6)", got)
	}

	// RestoreWhere — the predicate must include `deleted_at IS NOT NULL`
	// (the *Where ops auto-scope to non-deleted rows otherwise; restore is
	// the inverse of soft delete and must explicitly target deleted rows).
	env.backend.reset()
	if _, err := env.client.Articles().RestoreWhere(ctx, &models.ArticleFilter{
		Author:    &comparator.String{Eq: new("restore-test")},
		DeletedAt: &comparator.NullableTime{Null: new(false)},
	}); err != nil {
		t.Fatalf("RestoreWhere: %v", err)
	}
	if got := len(env.backend.filterCalls("invalidate_many")); got != 1 {
		t.Errorf("RestoreWhere: want 1 invalidate_many, got %d", got)
	}
	if got := len(env.backend.filterCalls("invalidate_pattern")); got != 0 {
		t.Errorf("RestoreWhere: want 0 invalidate_pattern, got %d", got)
	}

	// Post-restore Get round-trips to DB and serves the restored row. The
	// new entry caches fresh (a follow-up Get hits).
	env.backend.reset()
	env.counter.reset()
	missesBefore = env.metrics.missCount()
	got, err := env.client.Articles().Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get post-restore: %v", err)
	}
	if got.Title != "Live" {
		t.Errorf("Title = %q, want %q (cache must serve fresh row, not stale data)", got.Title, "Live")
	}
	if env.metrics.missCount()-missesBefore != 1 {
		t.Errorf("post-restore Get: misses delta = %d, want 1", env.metrics.missCount()-missesBefore)
	}
	if env.counter.selectQueries() == 0 {
		t.Error("post-restore Get: expected DB query, got 0")
	}

	// Follow-up Get hits the freshly-cached entity.
	hitsBefore = env.metrics.hitCount()
	if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("Get post-restore (warm): %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Error("post-restore second Get: expected cache hit, got 0")
	}
}

// TestRestore_AfterSoftDelete_SinglePKPath mirrors the *Where path for the
// single-PK SoftDelete / Restore mutation pair, which fire invalidate_many
// instead of invalidate_pattern. The end-to-end behaviour is the same: no
// stale entity survives and the post-restore Get serves a fresh row.
func TestRestore_AfterSoftDelete_SinglePKPath(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Mutate",
		Author: "single-pk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Articles().HardDelete(ctx, a.ID) })

	if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("warm: %v", err)
	}

	// SoftDelete by PK.
	env.backend.reset()
	if _, err := env.client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if got := len(env.backend.filterCalls("invalidate_many")); got != 1 {
		t.Errorf("SoftDelete: want 1 invalidate_many, got %d", got)
	}

	// Get → ErrNotFound → no cache write.
	env.backend.reset()
	if _, err := env.client.Articles().Get(ctx, a.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("Get post-soft-delete: err = %v, want ErrNotFound", err)
	}
	if got := len(env.backend.filterCalls("set")); got != 0 {
		t.Errorf("Get post-soft-delete: Set calls = %d, want 0", got)
	}

	// Restore by PK → invalidate_many.
	env.backend.reset()
	if _, err := env.client.Articles().Restore(ctx, a.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := len(env.backend.filterCalls("invalidate_many")); got != 1 {
		t.Errorf("Restore: want 1 invalidate_many, got %d", got)
	}

	// Get → DB round-trip, fresh row, cached for next time.
	env.backend.reset()
	env.counter.reset()
	got, err := env.client.Articles().Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get post-restore: %v", err)
	}
	if got.Title != "Mutate" || got.Author != "single-pk" {
		t.Errorf("Get post-restore: %+v, want fresh restored entity", got)
	}
	if env.counter.selectQueries() == 0 {
		t.Error("Get post-restore: expected DB query, got 0")
	}

	hitsBefore := env.metrics.hitCount()
	if _, err := env.client.Articles().Get(ctx, a.ID); err != nil {
		t.Fatalf("Get post-restore (warm): %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Error("post-restore second Get: expected cache hit, got 0")
	}
}
