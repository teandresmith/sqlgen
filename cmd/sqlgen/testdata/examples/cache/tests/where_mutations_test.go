package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// *Where mutation cache invalidation (PRD §27.7).
//
// Each *Where mutation evicts by key, exactly like its PK-keyed siblings: one
// InvalidateMany carrying the keys of the rows the predicate actually matched,
// and no table-wide pattern wipe. The affected PK set IS knowable — every
// *Where template captures it on MutationContext.AffectedPKs (via RETURNING on
// PostgreSQL/SQLite, via a pre-SELECT on MySQL) before the method returns, the
// same capture the event fan-out reads.
//
// The pattern wipe these tests used to assert swept prior-fingerprint orphans
// as a side effect. That is now left to TTL and to a deliberate
// Cache.InvalidateTable sweep: the wipe cost a full keyspace scan on every
// call and evicted every cached row of the table to invalidate one, and it was
// never a sweep anything could rely on (absent for tenanted tables, absent
// when update_where is disabled per-table, absent for any table that never
// sees *Where traffic).

// wantInvalidateManyKeys asserts that exactly one invalidate_many landed and
// that it carried one key per expected PK — matched on the terminal ":pk:<id>"
// segment so the assertion does not depend on the table's schema fingerprint.
func wantInvalidateManyKeys(t *testing.T, env *testEnv, label string, pks ...int64) {
	t.Helper()

	if got := len(env.backend.filterCalls("invalidate_pattern")); got != 0 {
		t.Errorf("%s: want 0 invalidate_pattern, got %d", label, got)
	}

	calls := env.backend.filterCalls("invalidate_many")
	if len(calls) != 1 {
		t.Fatalf("%s: want 1 invalidate_many, got %d", label, len(calls))
	}
	keys := calls[0].keys
	if len(keys) != len(pks) {
		t.Fatalf("%s: invalidate_many carried %d keys, want %d (%v)", label, len(keys), len(pks), keys)
	}
	for _, pk := range pks {
		suffix := fmt.Sprintf(":pk:%d", pk)
		if !hasKeyWithSuffix(keys, suffix) {
			t.Errorf("%s: no invalidated key ends in %q; got %v", label, suffix, keys)
		}
	}
}

func hasKeyWithSuffix(keys []string, suffix string) bool {
	for _, k := range keys {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// TestWhereMutation_UpdateWhere_InvalidatesAffectedRows verifies that
// UpdateWhere evicts one key per matched row and never pattern-wipes.
func TestWhereMutation_UpdateWhere_InvalidatesAffectedRows(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "U1", Author: "uw-pat"})
	a2, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "U2", Author: "uw-pat"})
	a3, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "U3", Author: "uw-pat"})
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID, a3.ID}) })

	author := "uw-pat"
	env.backend.reset()
	if _, err := env.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: &author}},
		&models.UpdateArticleInput{Title: omittable.Set("renamed")},
	); err != nil {
		t.Fatalf("UpdateWhere(3 rows): %v", err)
	}

	wantInvalidateManyKeys(t, env, "UpdateWhere(3 rows)", a1.ID, a2.ID, a3.ID)
}

// TestWhereMutation_HardDeleteWhere_InvalidatesAffectedRows verifies that
// HardDeleteWhere evicts one key per deleted row. Uses `articles` (no
// dependent view) so the assertion isolates the table-level invalidation from
// view-cascade patterns (the cache fixture's product_summary view has
// invalidate_on: [products], so mutations on products also emit a view
// pattern).
func TestWhereMutation_HardDeleteWhere_InvalidatesAffectedRows(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "HDW1", Author: "hdw-pat"})
	a2, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "HDW2", Author: "hdw-pat"})

	author := "hdw-pat"
	env.backend.reset()
	if err := env.client.Articles().HardDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	}); err != nil {
		t.Fatalf("HardDeleteWhere(2 rows): %v", err)
	}

	wantInvalidateManyKeys(t, env, "HardDeleteWhere(2 rows)", a1.ID, a2.ID)
}

// TestWhereMutation_SoftDeleteWhere_InvalidatesAffectedRows verifies that
// SoftDeleteWhere evicts one key per matched row.
func TestWhereMutation_SoftDeleteWhere_InvalidatesAffectedRows(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "S1", Author: "sdw-pat"})
	a2, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "S2", Author: "sdw-pat"})
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID}) })

	author := "sdw-pat"
	env.backend.reset()
	if _, err := env.client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	}); err != nil {
		t.Fatalf("SoftDeleteWhere(2 rows): %v", err)
	}

	wantInvalidateManyKeys(t, env, "SoftDeleteWhere(2 rows)", a1.ID, a2.ID)
}

// TestWhereMutation_RestoreWhere_InvalidatesAffectedRows verifies that
// RestoreWhere evicts one key per restored row.
func TestWhereMutation_RestoreWhere_InvalidatesAffectedRows(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	a1, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "R1", Author: "rw-pat"})
	a2, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "R2", Author: "rw-pat"})
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID}) })

	if _, err := env.client.Articles().SoftDeleteMany(ctx, []int64{a1.ID, a2.ID}); err != nil {
		t.Fatalf("SoftDeleteMany seed: %v", err)
	}

	author := "rw-pat"
	env.backend.reset()
	if _, err := env.client.Articles().RestoreWhere(ctx, &models.ArticleFilter{
		Author:    &comparator.String{Eq: &author},
		DeletedAt: &comparator.NullableTime{Null: new(false)},
	}); err != nil {
		t.Fatalf("RestoreWhere(2 rows): %v", err)
	}

	wantInvalidateManyKeys(t, env, "RestoreWhere(2 rows)", a1.ID, a2.ID)
}

// TestWhereMutation_BlastRadius_LeavesUntouchedRowCached is the blast-radius
// regression pin: a *Where mutation must evict the rows it touched and leave
// an unrelated cached row of the same table intact. Asserted through observed
// cache behavior rather than key strings — the touched row's next Get misses,
// the untouched row's next Get still hits.
func TestWhereMutation_BlastRadius_LeavesUntouchedRowCached(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	target, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "target", Author: "blast-hit"})
	bystander, _ := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "bystander", Author: "blast-miss"})
	t.Cleanup(func() { _ = env.client.Articles().HardDeleteMany(ctx, []int64{target.ID, bystander.ID}) })

	// Warm both rows, then confirm both are genuinely hot.
	for _, id := range []int64{target.ID, bystander.ID} {
		if _, err := env.client.Articles().Get(ctx, id); err != nil {
			t.Fatalf("warm Get(%d): %v", id, err)
		}
	}
	hitsBefore := env.metrics.hitCount()
	for _, id := range []int64{target.ID, bystander.ID} {
		if _, err := env.client.Articles().Get(ctx, id); err != nil {
			t.Fatalf("warm sanity Get(%d): %v", id, err)
		}
	}
	if env.metrics.hitCount()-hitsBefore != 2 {
		t.Fatalf("warm sanity: both rows must be hot — test setup invalid")
	}

	// Update only the target row.
	author := "blast-hit"
	if _, err := env.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: &author}},
		&models.UpdateArticleInput{Title: omittable.Set("target-renamed")},
	); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}

	// The touched row is evicted: its Get misses and serves the new value.
	missesBefore := env.metrics.missCount()
	got, err := env.client.Articles().Get(ctx, target.ID)
	if err != nil {
		t.Fatalf("Get(target): %v", err)
	}
	if env.metrics.missCount()-missesBefore != 1 {
		t.Errorf("Get(target) after UpdateWhere: want a cache miss, got a hit")
	}
	if got.Title != "target-renamed" {
		t.Errorf("Get(target).Title = %q, want %q", got.Title, "target-renamed")
	}

	// The untouched row survives: its Get still hits.
	hitsBefore = env.metrics.hitCount()
	if _, err := env.client.Articles().Get(ctx, bystander.ID); err != nil {
		t.Fatalf("Get(bystander): %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Errorf("Get(bystander) after an unrelated UpdateWhere: want a cache hit, got a miss — the mutation over-evicted")
	}
}

// TestWhereMutation_ZeroMatch_InvalidatesNothing verifies that a *Where
// predicate matching zero rows dispatches no invalidation at all. Nothing was
// written, so nothing cached can have gone stale; the old behavior wiped the
// table's whole namespace for a statement that changed no row.
func TestWhereMutation_ZeroMatch_InvalidatesNothing(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	noMatch := "where-zero-match-cache-" + t.Name()
	env.backend.reset()
	if _, err := env.client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: &noMatch}},
		&models.UpdateArticleInput{Title: omittable.Set("ignored")},
	); err != nil {
		t.Fatalf("UpdateWhere(zero match): %v", err)
	}

	if got := len(env.backend.filterCalls("invalidate_pattern")); got != 0 {
		t.Errorf("UpdateWhere(zero match): want 0 invalidate_pattern, got %d", got)
	}
	if got := len(env.backend.filterCalls("invalidate_many")); got != 0 {
		t.Errorf("UpdateWhere(zero match): want 0 invalidate_many, got %d", got)
	}
}

// TestWhereMutation_NonCachedTable_InvalidatesNothing verifies that a *Where
// mutation on a table the config opted out of caching
// (audit_logs — cache.enabled: false) dispatches no invalidation and, in
// particular, no pattern scan. PRD §27.7 offers that opt-out as the remedy for
// a junction paying invalidation it never reads back; the opt-out has to
// actually stop the work.
func TestWhereMutation_NonCachedTable_InvalidatesNothing(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	l, err := env.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{Message: "noncached-where"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.AuditLogs().HardDelete(ctx, l.ID) })

	msg := "noncached-where"
	env.backend.reset()
	if _, err := env.client.AuditLogs().UpdateWhere(
		ctx,
		&models.AuditLogFilter{Message: &comparator.String{Eq: &msg}},
		&models.UpdateAuditLogInput{Message: omittable.Set("noncached-where-2")},
	); err != nil {
		t.Fatalf("UpdateWhere(non-cached table): %v", err)
	}

	for _, op := range []string{"invalidate_pattern", "invalidate_many", "invalidate"} {
		if got := len(env.backend.filterCalls(op)); got != 0 {
			t.Errorf("UpdateWhere(non-cached table): want 0 %s, got %d", op, got)
		}
	}
}

// TestWhereMutation_NonCachedTable_SyncCommitSucceeds pins PRD §27.9: a
// committed write is never failed by cache bookkeeping. Under
// CallbackMode=CallbackSync the OnCommit callbacks run in-thread and their
// error is returned from Commit, so an invalidation arm that reports
// "unknown or non-cached table" for a table the config opted out of would
// surface as a WithTx error for a transaction that already committed.
func TestWhereMutation_NonCachedTable_SyncCommitSucceeds(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	l, err := env.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{Message: "sync-commit"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.AuditLogs().HardDelete(ctx, l.ID) })

	syncClient := models.New(
		env.counter,
		models.WithEventPublisher(env.bus),
		models.WithCache(env.cache),
		models.WithCallbackMode(database.CallbackSync),
	)

	// The *Where arm.
	msg := "sync-commit"
	if err := syncClient.WithTx(ctx, "where-sync", func(ctx context.Context) error {
		_, err := syncClient.AuditLogs().UpdateWhere(
			ctx,
			&models.AuditLogFilter{Message: &comparator.String{Eq: &msg}},
			&models.UpdateAuditLogInput{Message: omittable.Set("sync-commit-2")},
		)
		return err
	}); err != nil {
		t.Errorf("WithTx(UpdateWhere on non-cached table, CallbackSync) = %v, want nil", err)
	}

	// The *Many arm, which shares the same guard.
	if err := syncClient.WithTx(ctx, "many-sync", func(ctx context.Context) error {
		_, err := syncClient.AuditLogs().UpdateMany(ctx, []models.UpdateAuditLogItem{
			{ID: l.ID, Input: &models.UpdateAuditLogInput{Message: omittable.Set("sync-commit-3")}},
		})
		return err
	}); err != nil {
		t.Errorf("WithTx(UpdateMany on non-cached table, CallbackSync) = %v, want nil", err)
	}
}
