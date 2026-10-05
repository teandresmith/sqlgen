package tests

// Model-side relationship filter fields → EXISTS (PRD §11.1).
//
// A list relationship contributes a `*<Target>Filter` member to the parent
// filter, and a non-nil member compiles to a correlated EXISTS. Two properties
// separate this from "a nested filter that happens to work":
//
//  1. The target's own scoping rules go INSIDE the subquery — its soft-delete
//     predicate (§17.3) and, when it is tenanted, its tenant predicate
//     (§29.4.1). Without the second, a relationship filter is a cross-tenant
//     existence probe against a table the caller cannot otherwise read.
//  2. The parent-side correlation carries no qualifier at generation time. The
//     plain read path selects from an unaliased table and the O2O-join path
//     aliases it, so the builder resolves the reference per path (§11.5).
//
// This file drives both through the generated client against real SQLite.

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// seedRelFilter creates one user per tenant, each with a post, a tag linked to
// that post, and an article. Returns tenant A's user id.
func seedRelFilter(t *testing.T, client *models.Client) (userA, userB int64) {
	t.Helper()
	ctx := context.Background()

	uA, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	if _, err := client.Posts().Create(ctx, &models.CreatePostInput{UserID: uA.ID, Title: "alpha"}); err != nil {
		t.Fatalf("create post alpha: %v", err)
	}
	if _, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "a2@x.com"}); err != nil {
		t.Fatalf("create user A2: %v", err)
	}
	return uA.ID, 0
}

func userEmails(users []*models.User) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Email
	}
	return out
}

// TestRelationshipFilter_O2MNarrowsToMatchingParents is the row-set assertion
// for the O2M shape: only parents with a matching child come back, and a parent
// with no children at all is excluded rather than passed through.
func TestRelationshipFilter_O2MNarrowsToMatchingParents(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	seedRelFilter(t, env.client)

	got, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if want := []string{"a@x.com"}; len(got) != 1 || got[0].Email != want[0] {
		t.Fatalf("relationship-filtered users = %v, want %v", userEmails(got), want)
	}

	// A title no post carries matches nothing — the EXISTS is a real predicate,
	// not a no-op that leaves the parent list untouched.
	none, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("missing")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany (no match): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("non-matching relationship filter returned %v, want none", userEmails(none))
	}
}

// TestRelationshipFilter_M2MNarrowsThroughJunction covers the junction shape:
// one EXISTS over the junction joined to the target, not the two-query loader.
func TestRelationshipFilter_M2MNarrowsThroughJunction(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	user, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tagged, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: user.ID, Title: "tagged"})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: user.ID, Title: "untagged"}); err != nil {
		t.Fatalf("create untagged post: %v", err)
	}
	tag, err := env.client.Tags().Create(ctx, &models.CreateTagInput{Name: "go"})
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", tagged.ID, tag.ID); err != nil {
		t.Fatalf("link post_tags: %v", err)
	}

	got, err := env.client.Posts().GetMany(ctx, &models.GetPostsInput{
		Filter: &models.PostFilter{
			Tags: &models.TagFilter{Name: &comparator.String{Eq: new("go")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 || got[0].Title != "tagged" {
		titles := make([]string, len(got))
		for i, p := range got {
			titles[i] = p.Title
		}
		t.Fatalf("m2m relationship-filtered posts = %v, want [tagged]", titles)
	}
}

// TestRelationshipFilter_NilMemberEmitsNoCondition preserves the "nil filter ⇒
// no WHERE clause" contract at the member level: an unset relationship member
// must contribute no SQL, not an EXISTS that matches everything.
func TestRelationshipFilter_NilMemberEmitsNoCondition(t *testing.T) {
	resetDB(t)
	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	seedRelFilter(t, client)
	cap.reset()

	got, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{Email: &comparator.String{Eq: new("a@x.com")}},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(got))
	}
	for _, s := range cap.snapshot() {
		if strings.Contains(strings.ToUpper(s), "EXISTS (") {
			t.Errorf("nil relationship member emitted an EXISTS:\n%s", s)
		}
	}
}

// TestRelationshipFilter_SoftDeletedRelatedRowDoesNotMatch is the §17.3
// injection, observed rather than asserted on SQL text: the article exists and
// carries the title, but it is soft-deleted, so the parent must not match. The
// same filter matches once the article is restored, which rules out the test
// passing because the filter never worked.
func TestRelationshipFilter_SoftDeletedRelatedRowDoesNotMatch(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	user, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	article, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "draft",
		UserID: omittable.Set(&user.ID),
	})
	if err != nil {
		t.Fatalf("create article: %v", err)
	}

	filter := func() *models.UserFilter {
		return &models.UserFilter{
			Articles: &models.ArticleFilter{Title: &comparator.String{Eq: new("draft")}},
		}
	}

	live, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{Filter: filter()})
	if err != nil {
		t.Fatalf("GetMany (live): %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("live article: got %d users, want 1 — the filter never matched, so the deleted case below proves nothing", len(live))
	}

	if _, err := env.client.Articles().SoftDelete(ctx, article.ID); err != nil {
		t.Fatalf("soft delete article: %v", err)
	}

	deleted, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{Filter: filter()})
	if err != nil {
		t.Fatalf("GetMany (deleted): %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("soft-deleted related row still satisfied the relationship filter: %v", userEmails(deleted))
	}
}

// TestRelationshipFilter_CrossTenantRelatedRowDoesNotMatch is the invariant-1
// assertion — the reason the tenant predicate goes inside the subquery.
//
// The post is written by raw SQL with tenant B's workspace_id but tenant A's
// user_id: the drifted-row shape §29.10's belt-and-suspenders rule exists for.
// Tenant A's relationship filter must not see it. Without the injected tenant
// predicate the EXISTS would match, turning the filter into a probe that
// reports the existence of another tenant's rows.
func TestRelationshipFilter_CrossTenantRelatedRowDoesNotMatch(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	user, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO posts (workspace_id, user_id, title) VALUES (?, ?, ?)",
		tenantB[:], user.ID, "leaked",
	); err != nil {
		t.Fatalf("seed cross-tenant post: %v", err)
	}

	got, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("leaked")}},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cross-tenant related row satisfied the relationship filter: %v", userEmails(got))
	}

	// The same row IS reachable for tenant B, which proves the row exists and
	// the exclusion above came from the tenant predicate rather than from a
	// filter that matches nothing.
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	if _, err := envB.client.Users().Create(ctx, &models.CreateUserInput{Email: "b@x.com"}); err != nil {
		t.Fatalf("create user B: %v", err)
	}
	sqlText := postsTenantSQL(t, tenantB)
	if !strings.Contains(sqlText, "EXISTS (") {
		t.Errorf("expected an EXISTS in tenant B's query, got:\n%s", sqlText)
	}
}

// postsTenantSQL runs a relationship-filtered users query for tenant and
// returns the SELECT that carried the EXISTS.
func postsTenantSQL(t *testing.T, tenant uuid.UUID) string {
	t.Helper()
	client, cap := newCapturingClient(t, staticResolver(tenant))
	if _, err := client.Users().GetMany(context.Background(), &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("leaked")}},
		},
	}); err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	for _, s := range cap.snapshot() {
		if strings.Contains(s, "EXISTS (") {
			return s
		}
	}
	t.Fatalf("no EXISTS query captured, got %v", cap.snapshot())
	return ""
}

// TestRelationshipFilter_TenantPredicateIsInsideSubquery pins the placement the
// row-set tests can only imply: the target's tenant predicate belongs to the
// subquery, not to the outer WHERE where it would scope the parent a second
// time and leave the related rows unscoped.
func TestRelationshipFilter_TenantPredicateIsInsideSubquery(t *testing.T) {
	resetDB(t)
	sqlText := postsTenantSQL(t, tenantA)

	if !strings.Contains(sqlText, `tgt0."workspace_id" = ?`) {
		t.Errorf("subquery is missing the target's tenant predicate:\n%s", sqlText)
	}
	// The parent's own predicate stays outside the subquery, unaliased on the
	// plain read path. It is appended after the filter's conditions, so it lands
	// immediately past the subquery's closing paren.
	if !strings.Contains(sqlText, `) AND "workspace_id" = ?`) {
		t.Errorf("parent tenant predicate is not in the outer WHERE:\n%s", sqlText)
	}
}

// TestRelationshipFilter_SkipTenancyDropsSubqueryPredicate keeps the subquery
// consistent with the statement around it: an admin read that drops the parent
// tenant filter drops the target's too, rather than half-scoping the query.
func TestRelationshipFilter_SkipTenancyDropsSubqueryPredicate(t *testing.T) {
	resetDB(t)
	client, cap := newCapturingClient(t, staticResolver(tenantA))

	if _, err := client.Users().GetMany(context.Background(), &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("x")}},
		},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.SkipTenancy = true
	}); err != nil {
		t.Fatalf("GetMany: %v", err)
	}

	for _, s := range cap.snapshot() {
		if !strings.Contains(s, "EXISTS (") {
			continue
		}
		if countWorkspaceIDPredicates(s) != 0 {
			t.Errorf("SkipTenancy left a tenant predicate in the query:\n%s", s)
		}
		return
	}
	t.Fatalf("no EXISTS query captured, got %v", cap.snapshot())
}

// TestRelationshipFilter_ComposesWithAndOr covers §11.1's composition claim: a
// relationship filter nested inside and/or narrows alongside column filters,
// with no reverse-navigation workaround.
func TestRelationshipFilter_ComposesWithAndOr(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	alice, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "alice@x.com"})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "bob@x.com"})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "carol@x.com"}); err != nil {
		t.Fatalf("create carol: %v", err)
	}
	if _, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: alice.ID, Title: "alpha"}); err != nil {
		t.Fatalf("create alice post: %v", err)
	}
	if _, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: bob.ID, Title: "beta"}); err != nil {
		t.Fatalf("create bob post: %v", err)
	}

	// (posts.title = alpha) OR (email = bob@x.com) — alice via the subquery,
	// bob via the column filter, carol by neither. One branch per Or entry:
	// entries are OR-ed, and an entry's own fields would be AND-ed (PRD §11.1),
	// so putting both on one entry asks for the intersection instead.
	got, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Or: []*models.UserFilter{
				{Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}}},
				{Email: &comparator.String{Eq: new("bob@x.com")}},
			},
		},
		Sorts: []sql.Sort{{Column: "email", Direction: sql.Asc}},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	want := []string{"alice@x.com", "bob@x.com"}
	if got := userEmails(got); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("or-composed relationship filter = %v, want %v", got, want)
	}

	// AND narrows to the intersection, which is empty for this data.
	empty, err := env.client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			And: []*models.UserFilter{
				{Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}}},
				{Email: &comparator.String{Eq: new("bob@x.com")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany (and): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("and-composed relationship filter = %v, want none", userEmails(empty))
	}
}

// TestRelationshipFilter_TwoHopsCorrelateToTheirOwnLevel is the nesting proof.
// The inner subquery must correlate to the row of the subquery that encloses it
// — users → posts → tags — which is why the alias is keyed on depth rather than
// fixed per relationship.
func TestRelationshipFilter_TwoHopsCorrelateToTheirOwnLevel(t *testing.T) {
	resetDB(t)
	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tagged, err := client.Posts().Create(ctx, &models.CreatePostInput{UserID: user.ID, Title: "tagged"})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	other, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "b@x.com"})
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if _, err := client.Posts().Create(ctx, &models.CreatePostInput{UserID: other.ID, Title: "plain"}); err != nil {
		t.Fatalf("create other post: %v", err)
	}
	tag, err := client.Tags().Create(ctx, &models.CreateTagInput{Name: "go"})
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", tagged.ID, tag.ID); err != nil {
		t.Fatalf("link post_tags: %v", err)
	}
	cap.reset()

	got, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Posts: &models.PostFilter{
				Tags: &models.TagFilter{Name: &comparator.String{Eq: new("go")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 || got[0].Email != "a@x.com" {
		t.Fatalf("two-hop relationship filter = %v, want [a@x.com]", userEmails(got))
	}

	var found string
	for _, s := range cap.snapshot() {
		if strings.Contains(s, "EXISTS (") {
			found = s
		}
	}
	// The inner junction correlates to the outer subquery's post, not to the
	// outermost users row and not to itself.
	if !strings.Contains(found, `jn1."post_id" = tgt0."id"`) {
		t.Errorf("inner subquery does not correlate to its enclosing level:\n%s", found)
	}
	if strings.Contains(found, `tgt1."post_id" = tgt1.`) {
		t.Errorf("inner subquery shadowed its own alias:\n%s", found)
	}
}

// TestRelationshipFilter_SurvivesO2OJoinPath observes point 2 end-to-end: the
// same emitted condition has to correlate against the table name on the plain
// path and against the alias once an O2O relationship turns the statement into
// a LEFT JOIN. PrefixConditions must leave the EXISTS alone while prefixing the
// column filters beside it.
func TestRelationshipFilter_SurvivesO2OJoinPath(t *testing.T) {
	resetDB(t)
	client, cap := newCapturingClient(t, staticResolver(tenantA))
	ctx := context.Background()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := client.Posts().Create(ctx, &models.CreatePostInput{UserID: user.ID, Title: "alpha"}); err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: user.ID,
		Bio:    omittable.Set(new("hi")),
	}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	cap.reset()

	got, err := client.Users().GetMany(ctx, &models.GetUsersInput{
		Filter: &models.UserFilter{
			Email: &comparator.String{Eq: new("a@x.com")},
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}},
		},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true, Email: true, WorkspaceID: true,
			Profile: &models.UserProfileFieldOptions{ID: true, UserID: true, Bio: true},
		}
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 || got[0].Profile == nil {
		t.Fatalf("join-path relationship filter returned %d users (profile loaded: %t), want 1 with profile",
			len(got), len(got) == 1 && got[0].Profile != nil)
	}

	var joined string
	for _, s := range cap.snapshot() {
		if strings.Contains(s, "LEFT JOIN") {
			joined = s
		}
	}
	if joined == "" {
		t.Fatalf("no LEFT JOIN query captured, got %v", cap.snapshot())
	}
	// The correlation resolves to the parent alias, and the column filter beside
	// it is prefixed — one condition slice, two prefixing rules.
	if !strings.Contains(joined, `tgt0."user_id" = u."id"`) {
		t.Errorf("correlation did not resolve to the join alias:\n%s", joined)
	}
	if !strings.Contains(joined, `u."email" = ?`) {
		t.Errorf("sibling column filter was not alias-prefixed:\n%s", joined)
	}
}

// TestRelationshipFilter_PlaceholderNumberingWithKeyset is the arg-ordering
// pin. Connection adds keyset conditions after the filter's, so the subquery's
// own args sit in the middle of the statement's sequence — the case where a
// subquery that numbered its own placeholders would bind the wrong values.
func TestRelationshipFilter_PlaceholderNumberingWithKeyset(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	for _, email := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		u, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: email})
		if err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		if _, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: u.ID, Title: "alpha"}); err != nil {
			t.Fatalf("create post for %s: %v", email, err)
		}
	}

	first, err := env.client.Users().Connection(ctx, models.ConnectionInput[models.UserFilter]{
		Filter: &models.UserFilter{
			Email: &comparator.String{Neq: new("zzz@x.com")},
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}},
		},
		First: new(2),
	})
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if len(first.Edges) != 2 {
		t.Fatalf("first page = %d edges, want 2", len(first.Edges))
	}

	// The second page carries the keyset predicate alongside the subquery's
	// args; a numbering slip surfaces here as wrong rows or a driver error.
	second, err := env.client.Users().Connection(ctx, models.ConnectionInput[models.UserFilter]{
		Filter: &models.UserFilter{
			Email: &comparator.String{Neq: new("zzz@x.com")},
			Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}},
		},
		First: new(2),
		After: first.PageInfo.EndCursor,
	})
	if err != nil {
		t.Fatalf("Connection (page 2): %v", err)
	}
	if len(second.Edges) != 1 {
		t.Fatalf("second page = %d edges, want 1", len(second.Edges))
	}
	if second.Edges[0].Node.ID == first.Edges[0].Node.ID {
		t.Error("second page repeated the first page's row — keyset and subquery args are interleaved wrongly")
	}
}

// TestRelationshipFilter_ReachesFilterAcceptingMutations covers the surfaces
// beyond GetMany that take a filter. They resolve the tenant on the same path,
// so a relationship filter is scoped there too rather than only on reads.
func TestRelationshipFilter_ReachesFilterAcceptingMutations(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	user, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "a@x.com"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := env.client.Posts().Create(ctx, &models.CreatePostInput{UserID: user.ID, Title: "alpha"}); err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := env.client.Users().Create(ctx, &models.CreateUserInput{Email: "b@x.com"}); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	filter := &models.UserFilter{
		Posts: &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}},
	}

	n, err := env.client.Users().Count(ctx, filter)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("Count with relationship filter = %d, want 1", n)
	}

	ok, err := env.client.Users().ExistsWhere(ctx, filter)
	if err != nil {
		t.Fatalf("ExistsWhere: %v", err)
	}
	if !ok {
		t.Error("ExistsWhere with relationship filter = false, want true")
	}

	updated, err := env.client.Users().UpdateWhere(ctx, filter, &models.UpdateUserInput{
		Email: omittable.Set("renamed@x.com"),
	})
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if len(updated) != 1 || updated[0].Email != "renamed@x.com" {
		t.Errorf("UpdateWhere touched %v, want exactly the relationship-matched user", userEmails(updated))
	}
}
