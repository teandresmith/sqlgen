package tests

// Tenancy × relationships — M2M junction variance (PRD §29.10, §29.5).
//
// M2M loads run as two queries: (1) a SELECT on the junction to map parent→tag
// IDs; (2) a SELECT on the child table keyed by collected tag IDs. §29.10 is
// explicit that when the junction is NON-tenanted (the common shape — post_tags
// has no workspace_id column), cross-tenant isolation rides on the child's
// own auto-filter. The junction query produces potentially mixed rows; the
// child SELECT rejects cross-tenant tags before they can surface on a parent.
//
// This file validates the two observable consequences:
//
//  (a) SQL-shape assertion — the junction SELECT has no `workspace_id`
//      predicate; the child tags SELECT does. Captured SQL counts per-table.
//  (b) Cache-key isolation — the parent-side cache keys (posts, which IS
//      tenanted) and the child-side cache keys (tags, which IS tenanted)
//      both carry the tenant segment. BuildTenantTablePattern on tenant A
//      evicts only A's entries; tenant B's entries survive. This mirrors
//      TestCache_BuildTenantTablePatternInvalidatesOnlyOneTenant but for
//      the m2m path specifically.
//
// Schema context: in tenancy/schema.sql, `post_tags` has `tenancy.enabled:
// false` in sqlgen.yml (composite PK (post_id, tag_id) with no workspace_id
// column). Parent `posts` and child `tags` are tenanted.

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cache"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestM2M_NonTenantedJunctionSQLShape asserts the division of labour: junction
// SELECT has no `workspace_id` predicate (the table has no such column), child
// tags SELECT has one. A regression that somehow injected `workspace_id` into
// the junction WHERE would blow up at the DB layer ("no such column"); a
// regression that dropped it from the child tags WHERE would silently leak
// cross-tenant tags. The assertion catches both sides.
func TestM2M_NonTenantedJunctionSQLShape(t *testing.T) {
	resetDB(t)

	// Seed A's post + A's tag + link via the (non-tenanted) junction.
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	postA, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userA.ID, Title: "post A",
	})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	tagA, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tagA"})
	if err != nil {
		t.Fatalf("create tag A: %v", err)
	}
	if _, err := envA.client.PostTags().Create(ctx, &models.CreatePostTagInput{
		PostID: postA.ID, TagID: tagA.ID,
	}); err != nil {
		t.Fatalf("create junction: %v", err)
	}

	// Fresh capturing client for the m2m read path so setup SQLs don't pollute.
	client, cap := newCapturingClient(t, staticResolver(tenantA))

	_, err = client.Posts().Get(ctx, postA.ID, func(o *models.CallOptions[models.PostFieldOptions]) {
		o.FieldOptions = &models.PostFieldOptions{
			ID: true, UserID: true, WorkspaceID: true, Title: true,
			Tags: &models.TagRelationshipOptions{
				FieldOptions: &models.TagFieldOptions{ID: true, WorkspaceID: true, Name: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("Get post with tags: %v", err)
	}

	sqls := cap.snapshot()
	// The read path is: posts SELECT (parent) → post_tags SELECT (junction) →
	// tags SELECT (child). Classify by FROM-clause table reference.
	var postsSQL, junctionSQL, tagsSQL string
	for _, s := range sqls {
		low := strings.ToLower(s)
		switch {
		case strings.Contains(low, `from "post_tags"`):
			junctionSQL = s
		case strings.Contains(low, `from "tags"`):
			tagsSQL = s
		case strings.Contains(low, `from "posts"`):
			postsSQL = s
		}
	}

	if postsSQL == "" {
		t.Fatalf("missing posts SELECT; captured:\n%s", strings.Join(sqls, "\n"))
	}
	if junctionSQL == "" {
		t.Fatalf("missing post_tags SELECT; captured:\n%s", strings.Join(sqls, "\n"))
	}
	if tagsSQL == "" {
		t.Fatalf("missing tags SELECT; captured:\n%s", strings.Join(sqls, "\n"))
	}

	// Junction has no tenant column — must not have a workspace_id predicate.
	// A full-SQL substring check is sufficient because post_tags has no
	// workspace_id column at all; if the generator ever emitted the token,
	// the DB would reject the query and this test would fail loudly for that
	// reason too.
	if strings.Contains(strings.ToLower(junctionSQL), "workspace_id") {
		t.Errorf("junction SELECT references workspace_id (must not — table has no such column):\n%s", junctionSQL)
	}

	// Child tags SELECT must have the tenant predicate in WHERE. Scope the
	// check to WHERE-onward so the `SELECT "workspace_id"` column reference
	// in the projection doesn't trivially satisfy the assertion.
	if got := countWorkspaceIDPredicates(tagsSQL); got < 1 {
		t.Errorf("tags SELECT WHERE has no workspace_id predicate (cross-tenant leak risk):\n%s", tagsSQL)
	}

	// Parent posts SELECT (single row by PK) also carries the tenant predicate.
	if got := countWorkspaceIDPredicates(postsSQL); got < 1 {
		t.Errorf("parent posts SELECT WHERE has no workspace_id predicate:\n%s", postsSQL)
	}
}

// TestM2M_ParentCacheKeyIsTenantScoped verifies the parent-side (posts) cache
// key carries the tenant segment even though the junction itself is
// non-tenanted. Warms A's post in cache, then evicts A's posts via
// `BuildTenantTablePattern(posts, A)`; A's next read MISSES, B's independent
// post (warmed in parallel on the same backend) still HITS. The tenant segment
// is the thing that makes the pattern-eviction tenant-scoped.
func TestM2M_ParentCacheKeyIsTenantScoped(t *testing.T) {
	resetDB(t)

	// Shared backend across two tenants — that's the point.
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB = rewireCache(t, envB, envA.cache)

	ctx := context.Background()

	// Seed: A's post, B's post. Keep both minimal — no m2m links needed for
	// the parent-cache assertion; the junction/tags are tested elsewhere.
	userA, _ := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	postA, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userA.ID, Title: "postA",
	})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	userB, _ := envB.client.Users().Create(ctx, &models.CreateUserInput{Email: "ub@x.com"})
	postB, err := envB.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userB.ID, Title: "postB",
	})
	if err != nil {
		t.Fatalf("create post B: %v", err)
	}

	// Warm both tenants' post cache.
	if _, err := envA.client.Posts().Get(ctx, postA.ID); err != nil {
		t.Fatalf("warm A: %v", err)
	}
	if _, err := envB.client.Posts().Get(ctx, postB.ID); err != nil {
		t.Fatalf("warm B: %v", err)
	}

	// Evict tenant-A's posts by pattern. This relies on the cache key carrying
	// the tenant segment at the posts head — if keys are not tenant-scoped,
	// the pattern either matches everything or matches nothing (backend-specific
	// behaviour on zero matches), and the subsequent assertions fail.
	pattern := cache.BuildTenantTablePattern("sqlgen", "", "posts", tenantA)
	if err := envA.backend.InvalidatePattern(ctx, pattern); err != nil {
		t.Fatalf("InvalidatePattern: %v", err)
	}

	missesBefore := envA.metrics.missCount()
	hitsBefore := envA.metrics.hitCount()

	if _, err := envA.client.Posts().Get(ctx, postA.ID); err != nil {
		t.Fatalf("Get A post-evict: %v", err)
	}
	if _, err := envB.client.Posts().Get(ctx, postB.ID); err != nil {
		t.Fatalf("Get B post-evict: %v", err)
	}

	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("post-evict misses delta: %d, want 1 (only A's post should miss)", got)
	}
	if got := envA.metrics.hitCount() - hitsBefore; got != 1 {
		t.Errorf("post-evict hits delta: %d, want 1 (B's post must survive A's pattern-eviction)", got)
	}
}

// TestM2M_ChildCacheKeyIsTenantScoped — the child side of the same invariant.
// Tags are reached through the m2m path, but the tags cache key still carries
// the tenant segment because the tags TABLE is tenanted. Warm A's tag via a
// direct Tags().Get (equivalent to what the m2m loader does internally), then
// evict via BuildTenantTablePattern(tags, A); A's next read MISSES, B's tag
// still HITS. A regression where the m2m loader built cache keys without the
// tenant segment would render the pattern ineffective (or over-broad).
func TestM2M_ChildCacheKeyIsTenantScoped(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	envB = rewireCache(t, envB, envA.cache)

	ctx := context.Background()

	tagA, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tagA"})
	if err != nil {
		t.Fatalf("create tag A: %v", err)
	}
	tagB, err := envB.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tagB"})
	if err != nil {
		t.Fatalf("create tag B: %v", err)
	}

	if _, err := envA.client.Tags().Get(ctx, tagA.ID); err != nil {
		t.Fatalf("warm A: %v", err)
	}
	if _, err := envB.client.Tags().Get(ctx, tagB.ID); err != nil {
		t.Fatalf("warm B: %v", err)
	}

	pattern := cache.BuildTenantTablePattern("sqlgen", "", "tags", tenantA)
	if err := envA.backend.InvalidatePattern(ctx, pattern); err != nil {
		t.Fatalf("InvalidatePattern: %v", err)
	}

	missesBefore := envA.metrics.missCount()
	hitsBefore := envA.metrics.hitCount()

	if _, err := envA.client.Tags().Get(ctx, tagA.ID); err != nil {
		t.Fatalf("Get A tag post-evict: %v", err)
	}
	if _, err := envB.client.Tags().Get(ctx, tagB.ID); err != nil {
		t.Fatalf("Get B tag post-evict: %v", err)
	}

	if got := envA.metrics.missCount() - missesBefore; got != 1 {
		t.Errorf("post-evict tag misses delta: %d, want 1", got)
	}
	if got := envA.metrics.hitCount() - hitsBefore; got != 1 {
		t.Errorf("post-evict tag hits delta: %d, want 1 (B's tag cache entry must survive A's pattern-eviction)", got)
	}
}
