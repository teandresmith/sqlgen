package suite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// createWithRelated is the CreateWithRelated contract.
func createWithRelated(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("writes the parent, then creates and connects on every edge", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		createPost(t, h, "theirs", &other.ID)
		createPost(t, h, "left alone", nil)
		orphan := createPost(t, h, "orphan", nil)
		kept := createTag(t, h, "kept")
		named := createTag(t, h, "named")
		linkAuthorTag(t, h, other.ID, kept.ID)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts: &models.AuthorPostsCreateNested{
				Create:  []*models.AuthorPostsCreateInput{{Title: "first"}, {Title: "second"}},
				Connect: []int64{orphan.ID},
			},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags: &models.AuthorTagsCreateNested{
				Create:  []*models.CreateTagInput{{Name: "fresh"}},
				Connect: []int64{named.ID},
			},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		if got == nil || got.ID == other.ID || got.Name != "written" || cwrDeleted(got) {
			t.Fatalf("CreateWithRelated = %+v, want the written live author with a key of its own", got)
		}
		wantPosts := map[string]*int64{"theirs": &other.ID, "left alone": nil, "orphan": &got.ID, "first": &got.ID, "second": &got.ID}
		if diff := cmp.Diff(wantPosts, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if p := postParents(t, h)[orphan.ID]; p == nil || *p != got.ID {
			t.Errorf("connected post %d has parent %v, want %d", orphan.ID, p, got.ID)
		}
		if diff := cmp.Diff(map[string]*int64{"bio": &got.ID}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"other/kept", "written/fresh", "written/named"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 2)
		assertRowCount(t, h, "tags", 3)
	})

	t.Run("has-one connect adopts the named unparented profile", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		createProfile(t, h, "held", &other.ID)
		createProfile(t, h, "unnamed", nil)
		target := createProfile(t, h, "named", nil)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Profile: &models.AuthorProfileCreateNested{Connect: &target.ID},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(connect profile %d) error = %v, want nil", target.ID, err)
		}
		want := map[string]*int64{"held": &other.ID, "unnamed": nil, "named": &got.ID}
		if diff := cmp.Diff(want, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 3)
	})

	t.Run("connect adopts and links every named row across id-list chunks", func(t *testing.T) {
		h.Reset(t)
		createPost(t, h, "p1", nil)
		p2, p3, p4 := createPost(t, h, "p2", nil), createPost(t, h, "p3", nil), createPost(t, h, "p4", nil)
		createTag(t, h, "t1")
		t2, t3, t4 := createTag(t, h, "t2"), createTag(t, h, "t3"), createTag(t, h, "t4")
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{p4.ID, p2.ID, p3.ID}},
			Tags:   &models.AuthorTagsCreateNested{Connect: []int64{t4.ID, t2.ID, t3.ID}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(connect 3 posts, 3 tags) error = %v, want nil", err)
		}
		want := map[string]*int64{"p1": nil, "p2": &got.ID, "p3": &got.ID, "p4": &got.ID}
		if diff := cmp.Diff(want, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/t2", "written/t3", "written/t4"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
	})

	t.Run("creates more posts than the posts batch size and links more tags than the junction batch size", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts: &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{
				{Title: "a"}, {Title: "b"}, {Title: "c"},
			}},
			Tags: &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{
				{Name: "x"}, {Name: "y"}, {Name: "z"},
			}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(create 3 posts, 3 tags) error = %v, want nil", err)
		}
		want := map[string]*int64{"a": &got.ID, "b": &got.ID, "c": &got.ID}
		if diff := cmp.Diff(want, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/x", "written/y", "written/z"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
	})

	t.Run("a target named twice under connect is adopted or linked once", func(t *testing.T) {
		h.Reset(t)
		createPost(t, h, "decoy", nil)
		orphan := createPost(t, h, "orphan", nil)
		createTag(t, h, "decoy")
		tag := createTag(t, h, "named")
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{orphan.ID, orphan.ID}},
			Tags:   &models.AuthorTagsCreateNested{Connect: []int64{tag.ID, tag.ID}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(connect each target twice) error = %v, want nil", err)
		}
		want := map[string]*int64{"decoy": nil, "orphan": &got.ID}
		if diff := cmp.Diff(want, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/named"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
	})

	t.Run("nil input returns ErrNilInput, begins no transaction, writes nothing and fires no hook", func(t *testing.T) {
		h.Reset(t)
		begins := &beginCounter{Querier: h.Base}
		client, mutations, queries := cwrHookRecorder(begins)
		h.Querier.ResetCount()
		got, err := client.Authors().CreateWithRelated(ctx, nil)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("CreateWithRelated(nil) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("CreateWithRelated(nil) = %+v, want nil", got)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("CreateWithRelated(nil) sent %q, want nothing", stmts)
		}
		if n := begins.n.Load(); n != 0 {
			t.Errorf("CreateWithRelated(nil) began %d transactions, want none", n)
		}
		if m, q := mutations(), queries(); len(m) != 0 || len(q) != 0 {
			t.Errorf("CreateWithRelated(nil) fired mutation hooks %v and query hooks %v, want none", m, q)
		}
		assertRowCount(t, h, "authors", 0)
	})

	t.Run("nil verb blocks write the parent alone", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "unparented", nil)
		createTag(t, h, "tag")
		client, mutations, queries := cwrHookRecorder(h.Base)
		got, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(no blocks) error = %v, want nil", err)
		}
		if got == nil || got.Name != "written" {
			t.Fatalf("CreateWithRelated(no blocks) = %+v, want the written author", got)
		}
		cwrAssertParentAlone(t, h, mutations(), queries(), orphan.ID)
	})

	t.Run("verb blocks with no verb set write the parent alone", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "unparented", nil)
		createTag(t, h, "tag")
		client, mutations, queries := cwrHookRecorder(h.Base)
		got, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{}, Connect: []int64{}},
			Profile: &models.AuthorProfileCreateNested{},
			Tags:    &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{}, Connect: []int64{}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(empty blocks) error = %v, want nil", err)
		}
		if got == nil || got.Name != "written" {
			t.Fatalf("CreateWithRelated(empty blocks) = %+v, want the written author", got)
		}
		cwrAssertParentAlone(t, h, mutations(), queries(), orphan.ID)
	})

	t.Run("a nil element in a create list returns ErrNilInput attributed to that edge's create", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "valid"}, nil}},
		})
		cwrAssertNested(t, err, "Posts", "create", nil, models.ErrNilInput)
		assertRowCount(t, h, "authors", 0)
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("connect naming a post that does not exist returns ErrNotFound naming it and writes nothing", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		missing := orphan.ID + 100
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{orphan.ID, missing}},
		})
		cwrAssertNested(t, err, "Posts", "connect", missing, models.ErrNotFound)
		if diff := cmp.Diff(map[string]*int64{"orphan": nil}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 0)
	})

	t.Run("connect naming a soft-deleted post returns ErrNotFound naming it", func(t *testing.T) {
		h.Reset(t)
		createPost(t, h, "live", nil)
		deleted := createPost(t, h, "deleted", nil)
		if _, err := h.Client.Posts().SoftDelete(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDelete post %d: %v", deleted.ID, err)
		}
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{deleted.ID}},
		})
		cwrAssertNested(t, err, "Posts", "connect", deleted.ID, models.ErrNotFound)
		if diff := cmp.Diff(map[string]*int64{"live": nil, "deleted": nil}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 0)
	})

	t.Run("connect naming a profile that does not exist returns ErrNotFound naming it", func(t *testing.T) {
		h.Reset(t)
		unparented := createProfile(t, h, "unparented", nil)
		missing := unparented.ID + 100
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Profile: &models.AuthorProfileCreateNested{Connect: &missing},
		})
		cwrAssertNested(t, err, "Profile", "connect", missing, models.ErrNotFound)
		if diff := cmp.Diff(map[string]*int64{"unparented": nil}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 0)
	})

	t.Run("connect naming a tag that does not exist returns ErrNotFound naming it and links nothing", func(t *testing.T) {
		h.Reset(t)
		tag := createTag(t, h, "present")
		missing := tag.ID + 100
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Tags:   &models.AuthorTagsCreateNested{Connect: []int64{tag.ID, missing}},
		})
		cwrAssertNested(t, err, "Tags", "connect", missing, models.ErrNotFound)
		assertRowCount(t, h, "author_tags", 0)
		assertRowCount(t, h, "authors", 0)
	})

	t.Run("returns the parent the write leaves soft-deleted when no relationship is selected", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "born deleted", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(DeletedAt set) error = %v, want nil", err)
		}
		if got == nil || got.Name != "born deleted" || !cwrDeleted(got) {
			t.Fatalf("CreateWithRelated(DeletedAt set) = %+v, want the written author with DeletedAt set", got)
		}
		if diff := cmp.Diff(map[string]*int64{"post": &got.ID}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
	})

	t.Run("returns the parent the write leaves soft-deleted with a relationship selected", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "born deleted", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, DeletedAt: true, Posts: &models.PostRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(DeletedAt set, Posts selected) error = %v, want nil", err)
		}
		if got == nil || got.Name != "born deleted" || !cwrDeleted(got) {
			t.Fatalf("CreateWithRelated(DeletedAt set, Posts selected) = %+v, want the written author with DeletedAt set", got)
		}
		if diff := cmp.Diff([]string{"post"}, cwrTitles(got.Posts)); diff != "" {
			t.Errorf("returned Posts titles (-want +got):\n%s", diff)
		}
	})

	t.Run("a failure on a later edge rolls back the parent and every earlier edge", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "present")
		missing := tag.ID + 100
		client, mutations, _ := cwrHookRecorder(h.Base)
		_, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts: &models.AuthorPostsCreateNested{
				Create:  []*models.AuthorPostsCreateInput{{Title: "created"}},
				Connect: []int64{orphan.ID},
			},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags: &models.AuthorTagsCreateNested{
				Create:  []*models.CreateTagInput{{Name: "fresh"}},
				Connect: []int64{missing},
			},
		})
		cwrAssertNested(t, err, "Tags", "connect", missing, models.ErrNotFound)
		// The hook records each write after it succeeds, so these wrote before the failure.
		ran := []cwrOp{
			{hook.OpCreate, models.TableAuthors},
			{hook.OpCreateMany, models.TablePosts},
			{hook.OpUpdateWhere, models.TablePosts},
			{hook.OpCreate, models.TableAuthorProfiles},
			{hook.OpCreateMany, models.TableTags},
		}
		if diff := cmp.Diff(ran, cwrMutationOps(mutations())); diff != "" {
			t.Errorf("writes that ran before the failure (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 0)
		if diff := cmp.Diff(map[string]*int64{"orphan": nil}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title after the rollback (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 0)
		assertRowCount(t, h, "tags", 1)
		assertRowCount(t, h, "author_tags", 0)
	})

	t.Run("inside a caller's transaction a failure rolls back to its savepoint only", func(t *testing.T) {
		h.Reset(t)
		tag := createTag(t, h, "present")
		missing := tag.ID + 100
		client, mutations, _ := cwrHookRecorder(h.Base)
		var nestedErr error
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "before"}); err != nil {
				return err
			}
			_, nestedErr = client.Authors().CreateWithRelated(txCtx, &models.CreateAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "nested"},
				Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "nested post"}}},
				Tags:   &models.AuthorTagsCreateNested{Connect: []int64{missing}},
			})
			// The caller's transaction is still usable after the savepoint rolled back.
			_, err := h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "after"})
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		cwrAssertNested(t, nestedErr, "Tags", "connect", missing, models.ErrNotFound)
		ran := []cwrOp{{hook.OpCreate, models.TableAuthors}, {hook.OpCreateMany, models.TablePosts}}
		if diff := cmp.Diff(ran, cwrMutationOps(mutations())); diff != "" {
			t.Errorf("nested writes that ran before the failure (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"after", "before"}, cwrNames(t, h, "authors")); diff != "" {
			t.Errorf("authors after the commit (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("inside a caller's transaction successive calls commit with the caller", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "named")
		var first, second *models.Author
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			var err error
			first, err = h.Client.Authors().CreateWithRelated(txCtx, &models.CreateAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "first"},
				Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "first post"}}},
			})
			if err != nil {
				return err
			}
			second, err = h.Client.Authors().CreateWithRelated(txCtx, &models.CreateAuthorWithRelatedInput{
				Author:  models.CreateAuthorInput{Name: "second"},
				Posts:   &models.AuthorPostsCreateNested{Connect: []int64{orphan.ID}},
				Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
				Tags:    &models.AuthorTagsCreateNested{Connect: []int64{tag.ID}},
			}, cwrSelectAll)
			if err != nil {
				return err
			}
			// The terminal read left the caller's transaction usable.
			_, err = h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "after"})
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"orphan"}, cwrTitles(second.Posts)); diff != "" {
			t.Errorf("second call's returned Posts titles (-want +got):\n%s", diff)
		}
		if second.Profile == nil || second.Profile.Bio != "bio" {
			t.Errorf("second call's returned Profile = %+v, want the bio it created", second.Profile)
		}
		if diff := cmp.Diff([]string{"named"}, cwrTagNames(second.Tags)); diff != "" {
			t.Errorf("second call's returned Tags names (-want +got):\n%s", diff)
		}
		want := map[string]*int64{"first post": &first.ID, "orphan": &second.ID}
		if diff := cmp.Diff(want, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title after the commit (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[string]*int64{"bio": &second.ID}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio after the commit (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"second/named"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links after the commit (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"after", "first", "second"}, cwrNames(t, h, "authors")); diff != "" {
			t.Errorf("authors after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("inside a caller's transaction a success is undone by the caller's rollback", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "named")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Authors().CreateWithRelated(txCtx, &models.CreateAuthorWithRelatedInput{
				Author:  models.CreateAuthorInput{Name: "written"},
				Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
				Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
				Tags:    &models.AuthorTagsCreateNested{Connect: []int64{tag.ID}},
			}); err != nil {
				return err
			}
			// The writes are visible inside the transaction, so the rollback is what undoes them.
			n, err := h.Client.Posts().Count(txCtx, &models.PostFilter{AuthorID: &comparator.NullableNumber[int64]{Null: new(false)}})
			if err != nil {
				return err
			}
			if n != 2 {
				t.Errorf("parented posts inside the transaction = %d, want 2", n)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertRowCount(t, h, "authors", 0)
		if diff := cmp.Diff(map[string]*int64{"orphan": nil}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title after the rollback (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 0)
		assertRowCount(t, h, "author_tags", 0)
	})

	t.Run("fires one mutation hook per inner write, parent first, then each edge in field-name order", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "named")
		client, mutations, _ := cwrHookRecorder(h.Base)
		in := &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts: &models.AuthorPostsCreateNested{
				Create:  []*models.AuthorPostsCreateInput{{Title: "created"}},
				Connect: []int64{orphan.ID},
			},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags: &models.AuthorTagsCreateNested{
				Create:  []*models.CreateTagInput{{Name: "fresh"}},
				Connect: []int64{tag.ID},
			},
		}
		if _, err := client.Authors().CreateWithRelated(ctx, in); err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		want := []cwrOp{
			{hook.OpCreate, models.TableAuthors},
			{hook.OpCreateMany, models.TablePosts},
			{hook.OpUpdateWhere, models.TablePosts},
			{hook.OpCreate, models.TableAuthorProfiles},
			{hook.OpCreateMany, models.TableTags},
			{hook.OpUpsertMany, models.TableAuthorTags},
		}
		seen := mutations()
		if diff := cmp.Diff(want, cwrMutationOps(seen)); diff != "" {
			t.Fatalf("mutation hooks in order (-want +got):\n%s", diff)
		}
		if gotIn, ok := seen[0].Input.(*models.CreateAuthorInput); !ok || gotIn.Name != "written" {
			t.Errorf("parent MutationContext.Input = %#v, want a *CreateAuthorInput carrying the caller's values", seen[0].Input)
		}
	})

	t.Run("fires a query hook for each visibility and occupancy read, none for the verify or terminal read", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "named")
		client, _, queries := cwrHookRecorder(h.Base)
		got, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsCreateNested{Connect: []int64{tag.ID}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			// The one-to-one edge loads with the parent row, so selecting it runs
			// the terminal read and no relationship query of its own.
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Profile: &models.AuthorProfileFieldOptions{ID: true, Bio: true}}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		// The terminal read ran: the selected edge came back populated.
		if got.Profile == nil || got.Profile.Bio != "bio" {
			t.Errorf("returned Profile = %+v, want the bio the call created", got.Profile)
		}
		want := []cwrQuery{
			{hook.OpGetMany, models.TablePosts},
			{hook.OpGetMany, models.TableAuthorProfiles},
			{hook.OpGetMany, models.TableTags},
		}
		if diff := cmp.Diff(want, cwrQueryOps(queries())); diff != "" {
			t.Errorf("query hooks in order (-want +got):\n%s", diff)
		}
	})

	t.Run("the terminal read loads the selected relationships with the caller's query hooks", func(t *testing.T) {
		h.Reset(t)
		client, _, queries := cwrHookRecorder(h.Base)
		got, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Tags:   &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Posts: &models.PostRelationshipOptions{}, Tags: &models.TagRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"created"}, cwrTitles(got.Posts)); diff != "" {
			t.Errorf("returned Posts titles (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"fresh"}, cwrTagNames(got.Tags)); diff != "" {
			t.Errorf("returned Tags names (-want +got):\n%s", diff)
		}
		// The relationship loads run concurrently, so their order is not compared.
		gotQueries := cwrQueryOps(queries())
		slices.SortFunc(gotQueries, func(a, b cwrQuery) int { return strings.Compare(string(a.Table), string(b.Table)) })
		want := []cwrQuery{{hook.OpGetMany, models.TablePosts}, {hook.OpGetMany, models.TableTags}}
		if diff := cmp.Diff(want, gotQueries); diff != "" {
			t.Errorf("query hooks (-want +got):\n%s", diff)
		}
	})

	t.Run("SkipHooks on the caller fires no hook for any inner call", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "named")
		client, mutations, queries := cwrHookRecorder(h.Base)
		got, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}, Connect: []int64{tag.ID}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) { o.SkipHooks = true })
		if err != nil {
			t.Fatalf("CreateWithRelated(SkipHooks) error = %v, want nil", err)
		}
		if m, q := mutations(), queries(); len(m) != 0 || len(q) != 0 {
			t.Errorf("CreateWithRelated(SkipHooks) fired mutation hooks %v and query hooks %v, want none", cwrMutationOps(m), cwrQueryOps(q))
		}
		want := map[string]*int64{"orphan": &got.ID, "created": &got.ID}
		if diff := cmp.Diff(want, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/fresh", "written/named"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 1)
	})

	t.Run("a duplicate author name returns a unique ConstraintError and writes no edge", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "taken")
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "taken"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
			Tags:   &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}},
		})
		cwrAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "authors", 1)
		assertRowCount(t, h, "posts", 0)
		assertRowCount(t, h, "tags", 0)
	})

	t.Run("a duplicate tag name in the many-to-many create returns a unique ConstraintError attributed to Tags create", func(t *testing.T) {
		h.Reset(t)
		createTag(t, h, "taken")
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
			Tags:   &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "taken"}}},
		})
		cwrAssertNested(t, err, "Tags", "create", nil, nil)
		cwrAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "authors", 0)
		assertRowCount(t, h, "posts", 0)
		assertRowCount(t, h, "tags", 1)
	})

	t.Run("an empty post title in the one-to-many create returns a check ConstraintError attributed to Posts create", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "valid"}, {Title: ""}}},
		})
		cwrAssertNested(t, err, "Posts", "create", nil, nil)
		cwrAssertConstraint(t, err, models.ConstraintCheck)
		assertRowCount(t, h, "authors", 0)
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("connect naming a post another author holds returns ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		owner := createAuthor(t, h, "owner")
		orphan := createPost(t, h, "orphan", nil)
		taken := createPost(t, h, "taken", &owner.ID)
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{orphan.ID, taken.ID}},
		})
		cwrAssertNested(t, err, "Posts", "connect", taken.ID, models.ErrAlreadyRelated)
		if diff := cmp.Diff(map[string]*int64{"orphan": nil, "taken": &owner.ID}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 1)
	})

	t.Run("a post another author holds named twice under connect is still ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		owner := createAuthor(t, h, "owner")
		taken := createPost(t, h, "taken", &owner.ID)
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{taken.ID, taken.ID}},
		})
		cwrAssertNested(t, err, "Posts", "connect", taken.ID, models.ErrAlreadyRelated)
		if diff := cmp.Diff(map[string]*int64{"taken": &owner.ID}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
	})

	t.Run("has-one connect naming a profile another author holds returns ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		owner := createAuthor(t, h, "owner")
		createProfile(t, h, "unparented", nil)
		held := createProfile(t, h, "held", &owner.ID)
		_, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Profile: &models.AuthorProfileCreateNested{Connect: &held.ID},
		})
		cwrAssertNested(t, err, "Profile", "connect", held.ID, models.ErrAlreadyRelated)
		if diff := cmp.Diff(map[string]*int64{"unparented": nil, "held": &owner.ID}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 1)
	})

	t.Run("a has-one block setting create and connect returns ErrNestedVerbConflict attributed to connect before any statement on the edge", func(t *testing.T) {
		h.Reset(t)
		createProfile(t, h, "decoy", nil)
		target := createProfile(t, h, "named", nil)
		client, mutations, queries := cwrHookRecorder(h.Base)
		_, err := client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "created"}, Connect: &target.ID},
		})
		cwrAssertNested(t, err, "Profile", "connect", target.ID, models.ErrNestedVerbConflict)
		// The parent and the earlier edge ran; nothing ran on the profile edge.
		ran := []cwrOp{{hook.OpCreate, models.TableAuthors}, {hook.OpCreateMany, models.TablePosts}}
		if diff := cmp.Diff(ran, cwrMutationOps(mutations())); diff != "" {
			t.Errorf("writes that ran before the refusal (-want +got):\n%s", diff)
		}
		if q := cwrQueryOps(queries()); len(q) != 0 {
			t.Errorf("query hooks before the refusal = %v, want none: the occupancy read must not run", q)
		}
		if diff := cmp.Diff(map[string]*int64{"decoy": nil, "named": nil}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "authors", 0)
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("a nil FieldOptions returns every column and leaves every relationship unpopulated", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		stored, err := h.Client.Authors().Get(ctx, got.ID)
		if err != nil {
			t.Fatalf("Get(%d) error = %v", got.ID, err)
		}
		if diff := cmp.Diff(stored, got); diff != "" {
			t.Errorf("CreateWithRelated against the stored row, relationships unpopulated (-stored +returned):\n%s", diff)
		}
		assertRowCount(t, h, "author_tags", 1)
	})

	t.Run("a selected relationship comes back holding the rows the call wrote", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		createPost(t, h, "theirs", &other.ID)
		createProfile(t, h, "their bio", &other.ID)
		orphan := createPost(t, h, "orphan", nil)
		theirTag := createTag(t, h, "theirs")
		named := createTag(t, h, "named")
		linkAuthorTag(t, h, other.ID, theirTag.ID)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts: &models.AuthorPostsCreateNested{
				Create:  []*models.AuthorPostsCreateInput{{Title: "first"}, {Title: "second"}},
				Connect: []int64{orphan.ID},
			},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags: &models.AuthorTagsCreateNested{
				Create:  []*models.CreateTagInput{{Name: "fresh"}},
				Connect: []int64{named.ID},
			},
		}, cwrSelectAll)
		if err != nil {
			t.Fatalf("CreateWithRelated(every relationship selected) error = %v, want nil", err)
		}
		if got.Name != "written" {
			t.Errorf("returned Name = %q, want written", got.Name)
		}
		if diff := cmp.Diff([]string{"first", "orphan", "second"}, cwrTitles(got.Posts)); diff != "" {
			t.Errorf("returned Posts titles (-want +got):\n%s", diff)
		}
		for _, p := range got.Posts {
			if p.AuthorID == nil || *p.AuthorID != got.ID {
				t.Errorf("returned post %d has AuthorID %v, want %d", p.ID, p.AuthorID, got.ID)
			}
		}
		if got.Profile == nil || got.Profile.Bio != "bio" {
			t.Errorf("returned Profile = %+v, want the bio the call created", got.Profile)
		}
		if diff := cmp.Diff([]string{"fresh", "named"}, cwrTagNames(got.Tags)); diff != "" {
			t.Errorf("returned Tags names (-want +got):\n%s", diff)
		}
	})

	t.Run("an unselected relationship stays unpopulated when another is selected", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Tags: &models.TagRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(Tags selected) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"fresh"}, cwrTagNames(got.Tags)); diff != "" {
			t.Errorf("returned Tags names (-want +got):\n%s", diff)
		}
		if got.Posts != nil || got.Profile != nil {
			t.Errorf("returned Posts = %v, Profile = %+v, want both unpopulated", got.Posts, got.Profile)
		}
	})

	t.Run("an empty FieldOptions writes every edge and returns nil", func(t *testing.T) {
		h.Reset(t)
		orphan := createPost(t, h, "orphan", nil)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "written"},
			Posts:   &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "fresh"}}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("CreateWithRelated(empty FieldOptions) = %+v, want nil", got)
		}
		id := cwrAuthorID(t, h, "written")
		if diff := cmp.Diff(map[string]*int64{"orphan": &id, "created": &id}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[string]*int64{"bio": &id}, cwrProfilesByBio(t, h)); diff != "" {
			t.Errorf("profile parents by bio (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/fresh"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		tag := createTag(t, h, "named")
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Tags:   &models.AuthorTagsCreateNested{Connect: []int64{tag.ID}},
		}, withLock[models.AuthorFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("CreateWithRelated(LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.Name != "written" {
			t.Fatalf("CreateWithRelated(LockForUpdate) = %+v, want the written author", got)
		}
		if diff := cmp.Diff(map[string]*int64{"created": &got.ID}, cwrPostsByTitle(t, h)); diff != "" {
			t.Errorf("post parents by title (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"written/named"}, cwrLinks(t, h)); diff != "" {
			t.Errorf("author tag links (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction with a relationship selected", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Authors().CreateWithRelated(ctx, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "written"},
			Posts:  &models.AuthorPostsCreateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.LockMode = sql.LockForUpdate
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Posts: &models.PostRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("CreateWithRelated(LockForUpdate, Posts selected) error = %v, want nil", err)
		}
		if got == nil || got.Name != "written" {
			t.Fatalf("CreateWithRelated(LockForUpdate, Posts selected) = %+v, want the written author", got)
		}
		if diff := cmp.Diff([]string{"created"}, cwrTitles(got.Posts)); diff != "" {
			t.Errorf("returned Posts titles (-want +got):\n%s", diff)
		}
	})
}

// cwrSelectAll selects the parent's columns and all three relationships.
func cwrSelectAll(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{
		ID: true, Name: true,
		Posts:   &models.PostRelationshipOptions{},
		Profile: &models.AuthorProfileFieldOptions{ID: true, AuthorID: true, Bio: true},
		Tags:    &models.TagRelationshipOptions{},
	}
}

// cwrOp is one mutation hook call, by op and table.
type cwrOp struct {
	Op    hook.MutationOp
	Table hook.TableName
}

// cwrQuery is one query hook call, by op and table.
type cwrQuery struct {
	Op    hook.QueryOp
	Table hook.TableName
}

func cwrMutationOps(seen []hook.MutationContext) []cwrOp {
	var out []cwrOp
	for _, m := range seen {
		out = append(out, cwrOp{m.Op, m.Table})
	}
	return out
}

func cwrQueryOps(seen []hook.QueryContext) []cwrQuery {
	var out []cwrQuery
	for _, q := range seen {
		out = append(out, cwrQuery{q.Op, q.Table})
	}
	return out
}

// cwrHookRecorder returns a client on base with a mutation hook that
// records each MutationContext after the mutation succeeds and a query hook
// that records each QueryContext before the query runs, and a func for each
// that returns what it recorded.
func cwrHookRecorder(base database.Querier) (*models.Client, func() []hook.MutationContext, func() []hook.QueryContext) {
	var (
		mu        sync.Mutex
		mutations []hook.MutationContext
		queries   []hook.QueryContext
	)
	recordMutation := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if err == nil {
				mu.Lock()
				mutations = append(mutations, *m)
				mu.Unlock()
			}
			return res, err
		}
	}
	recordQuery := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			mu.Lock()
			queries = append(queries, *q)
			mu.Unlock()
			return next(ctx, q)
		}
	}
	client := models.New(base, models.WithMutationHook(recordMutation), models.WithQueryHook(recordQuery))
	return client,
		func() []hook.MutationContext {
			mu.Lock()
			defer mu.Unlock()
			return append([]hook.MutationContext(nil), mutations...)
		},
		func() []hook.QueryContext {
			mu.Lock()
			defer mu.Unlock()
			return append([]hook.QueryContext(nil), queries...)
		}
}

// cwrAssertParentAlone checks that a call wrote the author and nothing on any
// edge: one create hook on authors, no read on any edge, the orphan post still
// unparented, no profile parented and no link.
func cwrAssertParentAlone(t *testing.T, h *Harness, mutations []hook.MutationContext, queries []hook.QueryContext, orphan int64) {
	t.Helper()
	if diff := cmp.Diff([]cwrOp{{hook.OpCreate, models.TableAuthors}}, cwrMutationOps(mutations)); diff != "" {
		t.Errorf("mutation hooks (-want +got):\n%s", diff)
	}
	if q := cwrQueryOps(queries); len(q) != 0 {
		t.Errorf("query hooks = %v, want none", q)
	}
	if p := postParents(t, h)[orphan]; p != nil {
		t.Errorf("post %d has parent %d, want none", orphan, *p)
	}
	if diff := cmp.Diff(map[string]*int64{"unparented": nil}, cwrProfilesByBio(t, h)); diff != "" {
		t.Errorf("profile parents by bio (-want +got):\n%s", diff)
	}
	assertRowCount(t, h, "author_tags", 0)
	assertRowCount(t, h, "authors", 1)
	assertRowCount(t, h, "posts", 1)
	assertRowCount(t, h, "author_profiles", 1)
	assertRowCount(t, h, "tags", 1)
}

// cwrAssertNested checks that err is a *NestedMutationError on edge and verb.
// A non-nil id must equal its ID, and a non-nil sentinel must be what it wraps.
func cwrAssertNested(t *testing.T, err error, edge, verb string, id any, sentinel error) {
	t.Helper()
	nm, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("error = %v, want a *NestedMutationError", err)
	}
	if nm.Edge != edge || nm.Verb != verb {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q}, want {Edge: %q, Verb: %q}", nm.Edge, nm.Verb, edge, verb)
	}
	if id != nil && nm.ID != id {
		t.Errorf("NestedMutationError.ID = %#v, want %#v", nm.ID, id)
	}
	if sentinel != nil && !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap %v", err, sentinel)
	}
}

// cwrAssertConstraint checks that err is a ConstraintError of type want that
// unwraps to ErrConstraintViolation.
func cwrAssertConstraint(t *testing.T, err error, want models.ConstraintType) {
	t.Helper()
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v, want a *models.ConstraintError", err)
	}
	if ce.Type != want {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, want)
	}
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
	}
}

// cwrNames returns a table's name column, sorted, read with a statement the
// counting querier does not record.
func cwrNames(t *testing.T, h *Harness, table string) []string {
	t.Helper()
	var out []string
	for _, n := range cwrColumn(t, h, table, "name") {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// cwrAuthorID returns the key of the author named name.
func cwrAuthorID(t *testing.T, h *Harness, name string) int64 {
	t.Helper()
	for id, n := range cwrColumn(t, h, "authors", "name") {
		if n == name {
			return id
		}
	}
	t.Fatalf("no author named %q", name)
	return 0
}

// cwrPostsByTitle returns every post's author_id by title, soft-deleted posts
// included. A nil value is an unparented post.
func cwrPostsByTitle(t *testing.T, h *Harness) map[string]*int64 {
	t.Helper()
	return cwrParentsBy(t, h, "posts", "title", postParents(t, h))
}

// cwrProfilesByBio is cwrPostsByTitle for author_profiles.
func cwrProfilesByBio(t *testing.T, h *Harness) map[string]*int64 {
	t.Helper()
	return cwrParentsBy(t, h, "author_profiles", "bio", profileParents(t, h))
}

func cwrParentsBy(t *testing.T, h *Harness, table, column string, parents map[int64]*int64) map[string]*int64 {
	t.Helper()
	labels := cwrColumn(t, h, table, column)
	out := make(map[string]*int64, len(labels))
	for id, label := range labels {
		if _, dup := out[label]; dup {
			t.Fatalf("%s.%s %q labels two rows", table, column, label)
		}
		out[label] = parents[id]
	}
	return out
}

// cwrLinks returns every author_tags row as "author name/tag name", sorted.
func cwrLinks(t *testing.T, h *Harness) []string {
	t.Helper()
	authors := cwrColumn(t, h, "authors", "name")
	tags := cwrColumn(t, h, "tags", "name")
	var out []string
	for _, p := range authorTagPairs(t, h) {
		out = append(out, authors[p[0]]+"/"+tags[p[1]])
	}
	slices.Sort(out)
	return out
}

// cwrColumn returns a table's string column by id, read with a statement the
// counting querier does not record.
func cwrColumn(t *testing.T, h *Harness, table, column string) map[int64]string {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id, "+column+" FROM "+table)
	if err != nil {
		t.Fatalf("read %s.%s: %v", table, column, err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatalf("scan %s.%s: %v", table, column, err)
		}
		out[id] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s.%s: %v", table, column, err)
	}
	return out
}

func cwrTitles(posts []*models.Post) []string {
	var out []string
	for _, p := range posts {
		out = append(out, p.Title)
	}
	slices.Sort(out)
	return out
}

func cwrTagNames(tags []*models.Tag) []string {
	var out []string
	for _, tag := range tags {
		out = append(out, tag.Name)
	}
	slices.Sort(out)
	return out
}

// cwrDeleted reports whether a carries a soft-delete value.
func cwrDeleted(a *models.Author) bool {
	return !cmp.Equal(a.DeletedAt, models.Author{}.DeletedAt)
}
