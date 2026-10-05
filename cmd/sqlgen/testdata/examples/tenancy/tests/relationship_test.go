package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestRelationship_O2O_DirectChildGetIsTenantScoped verifies the parent-side
// half of §29.10 o2o: each table's own Get/GetMany applies its tenant
// auto-filter, so reading the o2o child directly (via UserProfileClient) from
// the wrong tenant returns ErrNotFound.
func TestRelationship_O2O_DirectChildGetIsTenantScoped(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bioA := "tenant A bio"
	profileA, err := envA.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: userA.ID,
		Bio:    omittable.Set(&bioA),
	})
	if err != nil {
		t.Fatalf("create profile A: %v", err)
	}

	// Sanity: A reads its own profile.
	gotA, err := envA.client.UserProfiles().Get(ctx, profileA.ID)
	if err != nil {
		t.Fatalf("Get profile from A: %v", err)
	}
	if gotA.WorkspaceID != tenantA {
		t.Errorf("profile workspace_id = %v, want %v", gotA.WorkspaceID, tenantA)
	}

	// Cross-tenant Get of A's profile from B → ErrNotFound (auto-filter).
	if _, err := envB.client.UserProfiles().Get(ctx, profileA.ID); err == nil {
		t.Errorf("Get A's profile from tenant B: err = nil, want ErrNotFound")
	}
}

// TestRelationship_O2O_TenantPropagatesToChild exercises the
// parent-load-with-FieldOptions.Profile path: loading a User with Profile
// selected must JOIN and apply the child tenant filter on the o2o alias.
// Emitting a WHERE for every chained o2o alias unconditionally would produce
// "no such column" SQL errors, so the runtime intersects `o2oJoins` with the
// static tenantedAliases map.
func TestRelationship_O2O_TenantPropagatesToChild(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()

	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bioA := "tenant A bio"
	if _, err := envA.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: userA.ID,
		Bio:    omittable.Set(&bioA),
	}); err != nil {
		t.Fatalf("create profile A: %v", err)
	}

	// Load the user with Profile selected. Depth-1 only: Profile is in the
	// JOIN, Profile.Users / Profile.Users.Profile / etc. are NOT — the
	// runtime alias-set intersection must keep only the JOINed aliases.
	loaded, err := envA.client.Users().Get(ctx, userA.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true, Email: true, WorkspaceID: true,
			Profile: &models.UserProfileFieldOptions{
				ID: true, UserID: true, WorkspaceID: true, Bio: true,
			},
		}
	})
	if err != nil {
		t.Fatalf("Get user with profile: %v", err)
	}
	if loaded.Profile == nil {
		t.Fatalf("loaded.Profile = nil, want non-nil")
	}
	if loaded.Profile.WorkspaceID != tenantA {
		t.Errorf("loaded.Profile.WorkspaceID = %v, want %v", loaded.Profile.WorkspaceID, tenantA)
	}
}

// TestRelationship_O2O_PartialChainSelection is the regression guardrail for
// partial o2o chain selection: select only the first depth of a chained o2o
// tree (Profile but NOT Profile.Users...). A template that emitted a WHERE for
// every chained alias the parser collected (e.g. `p2.workspace_id`,
// `p3.workspace_id`, …) regardless of which aliases the runtime actually
// JOINed would produce "no such column" SQL errors. This test proves the
// runtime loop over o2oJoins drops chained aliases cleanly.
func TestRelationship_O2O_PartialChainSelection(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()

	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bioA := "tenant A bio"
	if _, err := envA.client.UserProfiles().Create(ctx, &models.CreateUserProfileInput{
		UserID: userA.ID,
		Bio:    omittable.Set(&bioA),
	}); err != nil {
		t.Fatalf("create profile A: %v", err)
	}

	// Select Profile only. Profile.Users (the back-reference) and deeper
	// chained aliases must NOT appear in the generated WHERE, even though
	// annotateO2OChildTenancy collects them in TenantedO2OChildren.
	loaded, err := envA.client.Users().Get(ctx, userA.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true, Email: true,
			Profile: &models.UserProfileFieldOptions{
				ID: true, UserID: true, WorkspaceID: true, Bio: true,
			},
		}
	})
	if err != nil {
		t.Fatalf("Get user with partial chain: %v", err)
	}
	if loaded == nil || loaded.Profile == nil {
		t.Fatalf("loaded/loaded.Profile = nil, want non-nil")
	}
	if loaded.Profile.WorkspaceID != tenantA {
		t.Errorf("Profile.WorkspaceID = %v, want %v", loaded.Profile.WorkspaceID, tenantA)
	}
}

// TestRelationship_O2M_TenantPropagatesToChildren verifies §29.10 for o2m: a
// User loaded by tenant A with the Posts relationship selected gets only A's
// posts. Cross-tenant Post rows that point to the same UserID by FK are
// filtered out by the child's tenant auto-filter.
func TestRelationship_O2M_TenantPropagatesToChildren(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	user, err := envA.client.Users().Create(ctx, &models.CreateUserInput{
		Email: "user-a@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Two of A's posts.
	for _, title := range []string{"A post 1", "A post 2"} {
		if _, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
			UserID: user.ID,
			Title:  title,
		}); err != nil {
			t.Fatalf("create post %s: %v", title, err)
		}
	}

	// Poisoned cross-tenant post pointing at A's user.
	if _, err := envA.client.Posts().Create(
		ctx, &models.CreatePostInput{
			UserID:      user.ID,
			WorkspaceID: omittable.Set(tenantB),
			Title:       "B's post on A's user",
		},
		func(o *models.CallOptions[models.PostFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("create poisoned post: %v", err)
	}

	loaded, err := envA.client.Users().Get(ctx, user.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID: true, Email: true,
			Posts: &models.PostRelationshipOptions{
				FieldOptions: &models.PostFieldOptions{ID: true, UserID: true, WorkspaceID: true, Title: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("Get user with posts: %v", err)
	}
	// We can't reference loaded.Posts directly without checking the field
	// name on User struct. Use the User.Posts field name from the codegen.
	posts := loaded.Posts
	if len(posts) != 2 {
		t.Errorf("loaded posts: got %d, want 2 (cross-tenant post must be filtered)", len(posts))
	}
	for _, p := range posts {
		if p.WorkspaceID != tenantA {
			t.Errorf("loaded post workspace_id = %v, want %v", p.WorkspaceID, tenantA)
		}
	}
}

// TestRelationship_O2M_SkipTenancyPropagatesToChildren verifies §29.10
// SkipTenancy propagation: parent.GetMany with SkipTenancy:true drops the
// auto-filter on BOTH the parent AND every child relationship loaded for it.
// Half-applied SkipTenancy would be more confusing than no SkipTenancy at all.
func TestRelationship_O2M_SkipTenancyPropagatesToChildren(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	userA, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	userB, err := envB.client.Users().Create(ctx, &models.CreateUserInput{Email: "ub@x.com"})
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	if _, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userA.ID, Title: "A's post",
	}); err != nil {
		t.Fatalf("create A post: %v", err)
	}
	if _, err := envB.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userB.ID, Title: "B's post",
	}); err != nil {
		t.Fatalf("create B post: %v", err)
	}

	// Admin GetMany with SkipTenancy:true.
	users, err := envA.client.Users().GetMany(
		ctx, &models.GetUsersInput{},
		func(o *models.CallOptions[models.UserFieldOptions]) {
			o.SkipTenancy = true
			o.FieldOptions = &models.UserFieldOptions{
				ID: true, Email: true, WorkspaceID: true,
				Posts: &models.PostRelationshipOptions{
					FieldOptions: &models.PostFieldOptions{ID: true, UserID: true, WorkspaceID: true, Title: true},
				},
			}
		},
	)
	if err != nil {
		t.Fatalf("admin GetMany: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("admin GetMany: %d users, want 2", len(users))
	}
	totalPosts := 0
	for _, u := range users {
		totalPosts += len(u.Posts)
	}
	if totalPosts != 2 {
		t.Errorf("total posts across both users: %d, want 2 (SkipTenancy must propagate to children)", totalPosts)
	}
}

// TestRelationship_M2M_TenantPropagatesToChildren verifies §29.10 for m2m: a
// Post loaded with Tags returns only the tags that belong to the same tenant
// as the post. The junction table (post_tags) is non-tenanted so it doesn't
// filter, but the child tags table does — and that filter is what protects
// the cross-tenant boundary.
func TestRelationship_M2M_TenantPropagatesToChildren(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	userA, _ := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "ua@x.com"})
	postA, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: userA.ID, Title: "post A",
	})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}

	tagAGood, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tagA"})
	if err != nil {
		t.Fatalf("create tag A: %v", err)
	}

	// Force tenant B onto the same posts row by creating a B tag directly,
	// then linking via the junction. Even though both ends point through
	// non-tenanted post_tags, the m2m loader applies the child tenant
	// filter on tags — so the B tag is dropped.
	_, _ = envB.client.Users().Create(ctx, &models.CreateUserInput{Email: "ub@x.com"})
	tagBPoison, err := envB.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tagB"})
	if err != nil {
		t.Fatalf("create tag B: %v", err)
	}

	// Junction rows from A's post to BOTH tags. post_tags is non-tenanted so
	// either env can write any junction row (the table has no tenant column
	// at all).
	for _, tagID := range []int64{tagAGood.ID, tagBPoison.ID} {
		if _, err := envA.client.PostTags().Create(ctx, &models.CreatePostTagInput{
			PostID: postA.ID, TagID: tagID,
		}); err != nil {
			t.Fatalf("create junction (post=%d tag=%d): %v", postA.ID, tagID, err)
		}
	}

	loaded, err := envA.client.Posts().Get(ctx, postA.ID, func(o *models.CallOptions[models.PostFieldOptions]) {
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

	if len(loaded.Tags) != 1 {
		var names []string
		for _, tg := range loaded.Tags {
			names = append(names, tg.Name)
		}
		t.Errorf("loaded m2m tags: got %v, want exactly [tagA] — B tag must be filtered by child tenant scope", names)
	}
	for _, tg := range loaded.Tags {
		if tg.WorkspaceID != tenantA {
			t.Errorf("loaded m2m tag workspace_id = %v, want %v", tg.WorkspaceID, tenantA)
		}
	}
}
