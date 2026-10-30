package suite

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// upsertWithRelated is the UpsertWithRelated contract.
func upsertWithRelated(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("on a fresh insert create and connect write on every edge and clear unlinks no other parent's rows", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		createPost(t, h, "other-post", &other.ID)
		createProfile(t, h, "other-bio", &other.ID)
		otherTag := createTag(t, h, "other-tag")
		linkAuthorTag(t, h, other.ID, otherTag.ID)
		shared := createTag(t, h, "shared")
		linkAuthorTag(t, h, other.ID, shared.ID)
		orphan := createPost(t, h, "orphan", nil)
		orphanProfile := createProfile(t, h, "orphan-bio", nil)

		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "new"},
			Posts:   &models.AuthorPostsUpdateNested{Clear: true, Create: []*models.AuthorPostsCreateInput{{Title: "new-1"}, {Title: "new-2"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Connect: new(orphanProfile.ID)},
			Tags:    &models.AuthorTagsUpdateNested{Clear: true, Create: []*models.CreateTagInput{{Name: "new-tag"}}, Connect: []int64{shared.ID}},
		}, models.AuthorConflictName)
		if err != nil {
			t.Fatalf("UpsertWithRelated(new, clear+create+connect) error = %v, want nil", err)
		}
		if got == nil || got.Name != "new" || got.ID == other.ID {
			t.Fatalf("UpsertWithRelated(new) = %+v, want a new author named new", got)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"other": {ID: other.ID}, "new": {ID: got.ID}})
		swrAssertPosts(t, h, map[string]*int64{"other-post": &other.ID, "orphan": &got.ID, "new-1": &got.ID, "new-2": &got.ID})
		swrAssertProfiles(t, h, map[string]*int64{"other-bio": &other.ID, "orphan-bio": &got.ID})
		tags := swrTagIDs(t, h)
		swrAssertPairs(t, h, [2]int64{other.ID, otherTag.ID}, [2]int64{other.ID, shared.ID}, [2]int64{got.ID, shared.ID}, [2]int64{got.ID, tags["new-tag"]})
	})

	t.Run("on a fresh insert disconnect naming another parent's rows unlinks nothing and the creates still land", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		p1 := createPost(t, h, "other-1", &other.ID)
		p2 := createPost(t, h, "other-2", &other.ID)
		pr := createProfile(t, h, "other-bio", &other.ID)
		tag := createTag(t, h, "other-tag")
		linkAuthorTag(t, h, other.ID, tag.ID)

		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "new"},
			Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{p1.ID, p2.ID}, Create: []*models.AuthorPostsCreateInput{{Title: "new-post"}}},
			Profile: &models.AuthorProfileUpdateNested{Disconnect: new(pr.ID), Create: &models.AuthorProfileCreateInput{Bio: "new-bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Disconnect: []int64{tag.ID}, Create: []*models.CreateTagInput{{Name: "new-tag"}}},
		}, models.AuthorConflictName)
		if err != nil {
			t.Fatalf("UpsertWithRelated(new, disconnect other's rows) error = %v, want nil", err)
		}
		if got == nil || got.Name != "new" || got.ID == other.ID {
			t.Fatalf("UpsertWithRelated(new) = %+v, want a new author named new", got)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"other": {ID: other.ID}, "new": {ID: got.ID}})
		swrAssertPosts(t, h, map[string]*int64{"other-1": &other.ID, "other-2": &other.ID, "new-post": &got.ID})
		swrAssertProfiles(t, h, map[string]*int64{"other-bio": &other.ID, "new-bio": &got.ID})
		swrAssertPairs(t, h, [2]int64{other.ID, tag.ID}, [2]int64{got.ID, swrTagIDs(t, h)["new-tag"]})
	})

	t.Run("on a conflict the block runs against the existing parent, whose key is kept", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		a1 := createPost(t, h, "a-1", &a.ID)
		createPost(t, h, "a-2", &a.ID)
		createPost(t, h, "other-post", &other.ID)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "a-bio", &a.ID)
		createProfile(t, h, "other-bio", &other.ID)
		t1, t2, connected := createTag(t, h, "t1"), createTag(t, h, "t2"), createTag(t, h, "connected")
		linkAuthorTag(t, h, a.ID, t1.ID)
		linkAuthorTag(t, h, a.ID, t2.ID)
		linkAuthorTag(t, h, other.ID, t1.ID)

		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{a1.ID}, Create: []*models.AuthorPostsCreateInput{{Title: "a-new"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "a-new-bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Disconnect: []int64{t1.ID}, Create: []*models.CreateTagInput{{Name: "a-new-tag"}}, Connect: []int64{connected.ID}},
		}, models.AuthorConflictName)
		if err != nil {
			t.Fatalf("UpsertWithRelated(existing a) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || got.Name != "a" {
			t.Fatalf("UpsertWithRelated(existing a) = %+v, want a's row (ID %d)", got, a.ID)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"other": {ID: other.ID}, "a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"a-1": nil, "a-2": &a.ID, "a-new": &a.ID, "orphan": &a.ID, "other-post": &other.ID})
		swrAssertProfiles(t, h, map[string]*int64{"a-bio": nil, "a-new-bio": &a.ID, "other-bio": &other.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, t2.ID}, [2]int64{a.ID, connected.ID}, [2]int64{a.ID, swrTagIDs(t, h)["a-new-tag"]}, [2]int64{other.ID, t1.ID})
	})

	t.Run("the same clear-and-connect block inserts the parent, then on a conflict replaces what it gained since", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "other")
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "tag")
		payload := func() *models.UpsertAuthorWithRelatedInput {
			return &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a"},
				Posts:  &models.AuthorPostsUpdateNested{Clear: true, Connect: []int64{orphan.ID}},
				Tags:   &models.AuthorTagsUpdateNested{Clear: true, Connect: []int64{tag.ID}},
			}
		}
		first, err := h.Client.Authors().UpsertWithRelated(ctx, payload(), models.AuthorConflictName)
		if err != nil || first == nil {
			t.Fatalf("UpsertWithRelated(a, first call) = %+v, %v, want the inserted author", first, err)
		}
		swrAssertPosts(t, h, map[string]*int64{"orphan": &first.ID})
		swrAssertPairs(t, h, [2]int64{first.ID, tag.ID})

		// Rows the parent gains between the calls, for the second call's clear to remove.
		createPost(t, h, "extra", &first.ID)
		extraTag := createTag(t, h, "extra-tag")
		linkAuthorTag(t, h, first.ID, extraTag.ID)

		second, err := h.Client.Authors().UpsertWithRelated(ctx, payload(), models.AuthorConflictName)
		if err != nil || second == nil {
			t.Fatalf("UpsertWithRelated(a, second call) = %+v, %v, want the existing author", second, err)
		}
		if second.ID != first.ID {
			t.Errorf("second UpsertWithRelated(a) returned ID %d, want the first call's %d", second.ID, first.ID)
		}
		assertRowCount(t, h, "authors", 2)
		swrAssertPosts(t, h, map[string]*int64{"orphan": &first.ID, "extra": nil})
		swrAssertPairs(t, h, [2]int64{first.ID, tag.ID})
	})

	t.Run("a repeated upsert carrying create inserts another child and another linked tag", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "other")
		payload := func(tag string) *models.UpsertAuthorWithRelatedInput {
			return &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a"},
				Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
				Tags:   &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: tag}}},
			}
		}
		first, err := h.Client.Authors().UpsertWithRelated(ctx, payload("first"), models.AuthorConflictName)
		if err != nil || first == nil {
			t.Fatalf("UpsertWithRelated(a, first call) = %+v, %v, want the inserted author", first, err)
		}
		second, err := h.Client.Authors().UpsertWithRelated(ctx, payload("second"), models.AuthorConflictName)
		if err != nil || second == nil {
			t.Fatalf("UpsertWithRelated(a, second call) = %+v, %v, want the existing author", second, err)
		}
		if second.ID != first.ID {
			t.Errorf("second UpsertWithRelated(a) returned ID %d, want the first call's %d", second.ID, first.ID)
		}
		assertRowCount(t, h, "authors", 2)
		var under []int64
		for id, parent := range postParents(t, h) {
			if parent == nil || *parent != first.ID {
				t.Errorf("post %d has parent %v, want %d", id, parent, first.ID)
			}
			under = append(under, id)
		}
		if len(under) != 2 {
			t.Errorf("posts after two upserts carrying one create = %v, want 2 rows", under)
		}
		tags := swrTagIDs(t, h)
		if len(tags) != 2 {
			t.Errorf("tags after the two upserts = %v, want first and second", tags)
		}
		swrAssertPairs(t, h, [2]int64{first.ID, tags["first"]}, [2]int64{first.ID, tags["second"]})
	})

	t.Run("a repeated upsert carrying a has-one create is ErrAlreadyRelated naming the child the edge holds", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		// Another author's profile, which an occupancy read not scoped to the parent would see.
		otherProfile := createProfile(t, h, "other-bio", &other.ID)
		payload := func() *models.UpsertAuthorWithRelatedInput {
			return &models.UpsertAuthorWithRelatedInput{
				Author:  models.CreateAuthorInput{Name: "a"},
				Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			}
		}
		first, err := h.Client.Authors().UpsertWithRelated(ctx, payload(), models.AuthorConflictName)
		if err != nil || first == nil {
			t.Fatalf("UpsertWithRelated(a, first call) = %+v, %v, want the inserted author", first, err)
		}
		profiles := profileParents(t, h)
		if len(profiles) != 2 {
			t.Fatalf("profiles after the first call = %v, want other's and a's", profiles)
		}
		var held int64
		for id, parent := range profiles {
			if parent != nil && *parent == first.ID {
				held = id
			}
		}
		_, err = h.Client.Authors().UpsertWithRelated(ctx, payload(), models.AuthorConflictName)
		swrAssertNested(t, err, "Profile", "create", held, models.ErrAlreadyRelated)
		assertRowCount(t, h, "authors", 2)
		if diff := gocmp.Diff(map[int64]*int64{held: &first.ID, otherProfile.ID: &other.ID}, profileParents(t, h)); diff != "" {
			t.Errorf("profiles after the second call (-want +got):\n%s", diff)
		}
	})

	t.Run("nil input returns ErrNilInput before any statement or hook", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "a")
		begins := &beginCounter{Querier: h.Base}
		client, log := swrHookLog(begins)
		h.Querier.ResetCount()
		got, err := client.Authors().UpsertWithRelated(ctx, nil, models.AuthorConflictName)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("UpsertWithRelated(nil) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("UpsertWithRelated(nil) = %+v, want nil", got)
		}
		if l := log(); len(l) != 0 {
			t.Errorf("UpsertWithRelated(nil) fired hooks %q, want none", l)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpsertWithRelated(nil) sent %q, want nothing", stmts)
		}
		if n := begins.n.Load(); n != 0 {
			t.Errorf("UpsertWithRelated(nil) began %d transactions, want none", n)
		}
		assertRowCount(t, h, "authors", 1)
	})

	t.Run("a nil block and a block with no verb set write nothing on their edges", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "a-post", &a.ID)
		createPost(t, h, "orphan", nil)
		createProfile(t, h, "a-bio", &a.ID)
		tag := createTag(t, h, "tag")
		linkAuthorTag(t, h, a.ID, tag.ID)
		client, log := swrHookLog(h.Base)
		got, err := client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Profile: &models.AuthorProfileUpdateNested{},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{}, Connect: []int64{}, Disconnect: []int64{}},
		}, models.AuthorConflictName)
		if err != nil || got == nil || got.ID != a.ID {
			t.Fatalf("UpsertWithRelated(a, empty blocks) = %+v, %v, want a's row (ID %d)", got, err, a.ID)
		}
		if diff := gocmp.Diff([]string{swrMutation(hook.OpUpsert, models.TableAuthors)}, log()); diff != "" {
			t.Errorf("hooks fired by UpsertWithRelated(a, empty blocks) (-want +got):\n%s", diff)
		}
		swrAssertPosts(t, h, map[string]*int64{"a-post": &a.ID, "orphan": nil})
		swrAssertProfiles(t, h, map[string]*int64{"a-bio": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID})
		assertRowCount(t, h, "tags", 1)
	})

	t.Run("a connect naming an absent post is ErrNotFound naming it, and nothing is written", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "other")
		orphan := createPost(t, h, "orphan", nil)
		missing := orphan.ID + 1000
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a"},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID, missing}},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Posts", "connect", missing, models.ErrNotFound)
		assertRowCount(t, h, "authors", 1)
		swrAssertPosts(t, h, map[string]*int64{"orphan": nil})
	})

	t.Run("a connect naming a soft-deleted post is ErrNotFound naming it, and the conflict's writes roll back", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		deleted := createPost(t, h, "deleted", nil)
		if _, err := h.Client.Posts().SoftDelete(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDelete post deleted: %v", err)
		}
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID, deleted.ID}},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Posts", "connect", deleted.ID, models.ErrNotFound)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"orphan": nil, "deleted": nil})
	})

	t.Run("a connect naming an absent tag is ErrNotFound naming it, and nothing is linked", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		tag := createTag(t, h, "tag")
		missing := tag.ID + 1000
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Tags:   &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID, missing}},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Tags", "connect", missing, models.ErrNotFound)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertPairs(t, h)
	})

	t.Run("a has-one connect naming an absent profile is ErrNotFound naming it", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createProfile(t, h, "orphan-bio", nil)
		missing := orphan.ID + 1000
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Profile: &models.AuthorProfileUpdateNested{Connect: new(missing)},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Profile", "connect", missing, models.ErrNotFound)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertProfiles(t, h, map[string]*int64{"orphan-bio": nil})
	})

	softDeleteCases := []struct {
		name     string
		existing bool
		opts     []func(*models.CallOptions[models.AuthorFieldOptions])
	}{
		{name: "returns the parent its insert leaves soft-deleted, with no relationship selected"},
		{name: "returns the parent its insert leaves soft-deleted, with a relationship selected", opts: swrSelectPosts()},
		{name: "returns the parent its conflict leaves soft-deleted, with no relationship selected", existing: true},
		{name: "returns the parent its conflict leaves soft-deleted, with a relationship selected", existing: true, opts: swrSelectPosts()},
	}
	for _, tt := range softDeleteCases {
		t.Run(tt.name, func(t *testing.T) {
			h.Reset(t)
			createAuthor(t, h, "other")
			var existing *models.Author
			if tt.existing {
				existing = createAuthor(t, h, "a")
			}
			got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
				Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "post"}}},
			}, models.AuthorConflictName, tt.opts...)
			if err != nil {
				t.Fatalf("UpsertWithRelated(a, DeletedAt set) error = %v, want nil", err)
			}
			if got == nil || got.Name != "a" || !swrAuthorDeleted(got) {
				t.Fatalf("UpsertWithRelated(a, DeletedAt set) = %+v, want a with DeletedAt set", got)
			}
			rows := swrAuthors(t, h)
			if row := rows["a"]; row.ID != got.ID || !row.Deleted {
				t.Errorf("stored author a = %+v, want ID %d soft-deleted", row, got.ID)
			}
			if existing != nil && got.ID != existing.ID {
				t.Errorf("UpsertWithRelated(existing a) returned ID %d, want %d", got.ID, existing.ID)
			}
			if len(rows) != 2 {
				t.Errorf("authors after the upsert = %+v, want other and a", rows)
			}
			if tt.opts != nil && (len(got.Posts) != 1 || got.Posts[0].Title != "post") {
				t.Errorf("UpsertWithRelated(a, Posts selected).Posts = %+v, want the created post", got.Posts)
			}
		})
	}

	t.Run("a failure on a later edge rolls back the parent write and the earlier edges", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		a1 := createPost(t, h, "a-1", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "tag")
		client, log := swrHookLog(h.Base)
		_, err := client.Authors().UpsertWithRelated(ctx, swrTxInput(a1.ID, orphan.ID, tag.ID+1000), models.AuthorConflictName)
		swrAssertNested(t, err, "Tags", "connect", tag.ID+1000, models.ErrNotFound)
		// The parent write and the earlier edges ran in this call before Tags failed.
		want := []string{
			swrMutation(hook.OpUpsert, models.TableAuthors),
			swrMutation(hook.OpCreateMany, models.TablePosts),
			swrQuery(hook.OpGetMany, models.TablePosts),
			swrMutation(hook.OpUpdateWhere, models.TablePosts),
			swrMutation(hook.OpUpdateWhere, models.TablePosts),
			swrQuery(hook.OpGetMany, models.TableAuthorProfiles),
			swrMutation(hook.OpCreate, models.TableAuthorProfiles),
			swrQuery(hook.OpGetMany, models.TableTags),
		}
		if diff := gocmp.Diff(want, log()); diff != "" {
			t.Errorf("hooks fired by the failing call (-want +got):\n%s", diff)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"a-1": &a.ID, "orphan": nil})
		swrAssertProfiles(t, h, map[string]*int64{})
		swrAssertPairs(t, h)
	})

	t.Run("the same block with a visible tag commits the parent write and every edge", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		a1 := createPost(t, h, "a-1", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "tag")
		if _, err := h.Client.Authors().UpsertWithRelated(ctx, swrTxInput(a1.ID, orphan.ID, tag.ID), models.AuthorConflictName); err != nil {
			t.Fatalf("UpsertWithRelated(a, visible tag) error = %v, want nil", err)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID, Deleted: true}})
		swrAssertPosts(t, h, map[string]*int64{"a-1": nil, "orphan": &a.ID, "created": &a.ID})
		swrAssertProfiles(t, h, map[string]*int64{"bio": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID})
	})

	t.Run("inside a caller's transaction a failure rolls back to its savepoint and the caller's transaction commits", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "a-1", &a.ID)
		createPost(t, h, "a-2", &a.ID)
		tag := createTag(t, h, "tag")
		var upsertErr error
		client, log := swrHookLog(h.Base)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "before"}); err != nil {
				return err
			}
			// Posts clears a's posts, then Tags fails.
			_, upsertErr = client.Authors().UpsertWithRelated(txCtx, &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
				Posts:  &models.AuthorPostsUpdateNested{Clear: true},
				Tags:   &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID + 1000}},
			}, models.AuthorConflictName)
			if upsertErr == nil {
				return errors.New("the nested upsert succeeded inside the transaction")
			}
			_, err := h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "after"})
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil: the caller's transaction must survive the nested failure", err)
		}
		swrAssertNested(t, upsertErr, "Tags", "connect", tag.ID+1000, models.ErrNotFound)
		// The parent write and the Posts clear ran in the failing call before Tags failed.
		want := []string{
			swrMutation(hook.OpUpsert, models.TableAuthors),
			swrMutation(hook.OpUpdateWhere, models.TablePosts),
			swrQuery(hook.OpGetMany, models.TableTags),
		}
		if diff := gocmp.Diff(want, log()); diff != "" {
			t.Errorf("hooks fired by the failing call (-want +got):\n%s", diff)
		}
		rows := swrAuthors(t, h)
		if row := rows["a"]; row.ID != a.ID || row.Deleted {
			t.Errorf("author a after the commit = %+v, want ID %d live", row, a.ID)
		}
		names := slices.Sorted(maps.Keys(rows))
		if diff := gocmp.Diff([]string{"a", "after", "before"}, names); diff != "" {
			t.Errorf("author names after the commit (-want +got):\n%s", diff)
		}
		swrAssertPosts(t, h, map[string]*int64{"a-1": &a.ID, "a-2": &a.ID})
	})

	t.Run("inside a caller's transaction a success commits with the caller", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "a-1", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		old, tag := createTag(t, h, "old"), createTag(t, h, "tag")
		linkAuthorTag(t, h, a.ID, old.ID)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			got, err := h.Client.Authors().UpsertWithRelated(txCtx, &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a"},
				Posts:  &models.AuthorPostsUpdateNested{Clear: true, Connect: []int64{orphan.ID}},
				Tags:   &models.AuthorTagsUpdateNested{Clear: true, Connect: []int64{tag.ID}},
			}, models.AuthorConflictName)
			if err != nil {
				return err
			}
			if got == nil || got.ID != a.ID {
				t.Errorf("UpsertWithRelated(a) inside the transaction = %+v, want a's row (ID %d)", got, a.ID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"a-1": nil, "orphan": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID})
	})

	t.Run("inside a caller's transaction a success is undone by the caller's rollback", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		tag := createTag(t, h, "tag")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			got, err := h.Client.Authors().UpsertWithRelated(txCtx, &models.UpsertAuthorWithRelatedInput{
				Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
				Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
				Tags:   &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID}},
			}, models.AuthorConflictName)
			if err != nil {
				return err
			}
			if got == nil || got.ID != a.ID || !swrAuthorDeleted(got) {
				t.Errorf("UpsertWithRelated(a) inside the transaction = %+v, want a's row soft-deleted", got)
			}
			n, err := h.Client.Posts().Count(txCtx, &models.PostFilter{AuthorID: &comparator.NullableNumber[int64]{Number: comparator.Number[int64]{Eq: new(a.ID)}}})
			if err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("a's posts inside the transaction = %d, want the created one", n)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		assertRowCount(t, h, "posts", 0)
		swrAssertPairs(t, h)
	})

	t.Run("the mutation hook sees one call per inner write in execution order, and neither the verify read nor the terminal read fires a query hook", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "a-1", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "a-bio", &a.ID)
		t1, t2 := createTag(t, h, "t1"), createTag(t, h, "t2")
		linkAuthorTag(t, h, a.ID, t1.ID)
		client, log := swrHookLog(h.Base)
		got, err := client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Posts:   &models.AuthorPostsUpdateNested{Clear: true, Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "created"}}, Connect: []int64{t2.ID}, Disconnect: []int64{t1.ID}},
		}, models.AuthorConflictName, swrSelectPosts()...)
		if err != nil || got == nil || got.ID != a.ID {
			t.Fatalf("UpsertWithRelated(a, every verb) = %+v, %v, want a's row", got, err)
		}
		want := []string{
			swrMutation(hook.OpUpsert, models.TableAuthors),
			swrMutation(hook.OpUpdateWhere, models.TablePosts),
			swrMutation(hook.OpCreateMany, models.TablePosts),
			swrQuery(hook.OpGetMany, models.TablePosts),
			swrMutation(hook.OpUpdateWhere, models.TablePosts),
			swrMutation(hook.OpUpdateWhere, models.TableAuthorProfiles),
			swrQuery(hook.OpGetMany, models.TableAuthorProfiles),
			swrMutation(hook.OpCreate, models.TableAuthorProfiles),
			swrMutation(hook.OpCreateMany, models.TableTags),
			swrQuery(hook.OpGetMany, models.TableTags),
			swrMutation(hook.OpUpsertMany, models.TableAuthorTags),
			swrMutation(hook.OpHardDeleteMany, models.TableAuthorTags),
			// The terminal read fires none; its Posts load fires the caller's.
			swrQuery(hook.OpGetMany, models.TablePosts),
		}
		if diff := gocmp.Diff(want, log()); diff != "" {
			t.Errorf("hooks fired by UpsertWithRelated(a, every verb) (-want +got):\n%s", diff)
		}
		var titles []string
		for _, p := range got.Posts {
			titles = append(titles, p.Title)
		}
		slices.Sort(titles)
		if diff := gocmp.Diff([]string{"created", "orphan"}, titles); diff != "" {
			t.Errorf("UpsertWithRelated(a, Posts selected).Posts titles (-want +got):\n%s", diff)
		}
	})

	t.Run("SkipHooks on the call fires no hook on any inner call", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "tag")
		client, log := swrHookLog(h.Base)
		_, err := client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Posts:   &models.AuthorPostsUpdateNested{Clear: true, Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "created"}}, Connect: []int64{tag.ID}},
		}, models.AuthorConflictName, func(o *models.CallOptions[models.AuthorFieldOptions]) { o.SkipHooks = true })
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, SkipHooks) error = %v, want nil", err)
		}
		if l := log(); len(l) != 0 {
			t.Errorf("UpsertWithRelated(a, SkipHooks) fired hooks %q, want none", l)
		}
		swrAssertPosts(t, h, map[string]*int64{"orphan": &a.ID, "created": &a.ID})
		swrAssertProfiles(t, h, map[string]*int64{"bio": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID}, [2]int64{a.ID, swrTagIDs(t, h)["created"]})
	})

	t.Run("a primary-key target whose name is taken is a unique ConstraintError and writes nothing", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
		}, models.AuthorConflictPK)
		usAssertConstraint(t, err, models.ConstraintUnique)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("a nested tag create on a taken name is a unique ConstraintError attributed to Tags create", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createTag(t, h, "taken")
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Tags:   &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "fresh"}, {Name: "taken"}}},
		}, models.AuthorConflictName)
		swrAssertNestedConstraint(t, err, "Tags", "create", models.ConstraintUnique)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		assertRowCount(t, h, "posts", 0)
		assertRowCount(t, h, "tags", 1)
	})

	t.Run("a nested post create breaking the title CHECK is a check ConstraintError attributed to Posts create", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "ok"}, {Title: ""}}},
		}, models.AuthorConflictName)
		swrAssertNestedConstraint(t, err, "Posts", "create", models.ConstraintCheck)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		assertRowCount(t, h, "posts", 0)
	})

	t.Run("a connect naming another parent's post is ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		taken := createPost(t, h, "taken", &other.ID)
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a"},
			Posts:  &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID, taken.ID}},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Posts", "connect", taken.ID, models.ErrAlreadyRelated)
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"other": {ID: other.ID}, "a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"orphan": nil, "taken": &other.ID})
	})

	t.Run("a has-one connect on a conflict whose parent holds another child is ErrAlreadyRelated naming that child", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		held := createProfile(t, h, "held", &a.ID)
		orphan := createProfile(t, h, "orphan", nil)
		_, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Profile: &models.AuthorProfileUpdateNested{Connect: new(orphan.ID)},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Profile", "connect", held.ID, models.ErrAlreadyRelated)
		swrAssertProfiles(t, h, map[string]*int64{"held": &a.ID, "orphan": nil})
	})

	t.Run("one post under connect and disconnect is ErrNestedVerbConflict attributed to disconnect, before any statement on the edge", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		client, log := swrHookLog(h.Base)
		_, err := client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}, Disconnect: []int64{orphan.ID}},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Posts", "disconnect", orphan.ID, models.ErrNestedVerbConflict)
		if diff := gocmp.Diff([]string{swrMutation(hook.OpUpsert, models.TableAuthors)}, log()); diff != "" {
			t.Errorf("hooks fired before the refusal (-want +got):\n%s", diff)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		swrAssertPosts(t, h, map[string]*int64{"orphan": nil})
	})

	t.Run("a has-one block setting create and connect is ErrNestedVerbConflict attributed to connect, before any statement on the edge", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createProfile(t, h, "orphan", nil)
		client, log := swrHookLog(h.Base)
		_, err := client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}, Connect: new(orphan.ID)},
		}, models.AuthorConflictName)
		swrAssertNested(t, err, "Profile", "connect", orphan.ID, models.ErrNestedVerbConflict)
		if diff := gocmp.Diff([]string{swrMutation(hook.OpUpsert, models.TableAuthors), swrMutation(hook.OpCreateMany, models.TablePosts)}, log()); diff != "" {
			t.Errorf("hooks fired before the refusal (-want +got):\n%s", diff)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID}})
		assertRowCount(t, h, "posts", 0)
		swrAssertProfiles(t, h, map[string]*int64{"orphan": nil})
	})

	t.Run("nil FieldOptions returns the parent with its relationship members unpopulated", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		tag := createTag(t, h, "tag")
		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID}},
		}, models.AuthorConflictName)
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, nil FieldOptions) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || got.Name != "a" {
			t.Fatalf("UpsertWithRelated(a, nil FieldOptions) = %+v, want a's row", got)
		}
		if got.Posts != nil || got.Profile != nil || got.Tags != nil {
			t.Errorf("UpsertWithRelated(a, nil FieldOptions) relationships = Posts %+v, Profile %+v, Tags %+v, want all unpopulated", got.Posts, got.Profile, got.Tags)
		}
		swrAssertPosts(t, h, map[string]*int64{"created": &a.ID})
		swrAssertProfiles(t, h, map[string]*int64{"bio": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID})
	})

	t.Run("a selected relationship is returned holding the rows the call wrote", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		before := createPost(t, h, "before", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "old-bio", &a.ID)
		oldTag, connected := createTag(t, h, "old"), createTag(t, h, "connected")
		linkAuthorTag(t, h, a.ID, oldTag.ID)
		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author:  models.CreateAuthorInput{Name: "a"},
			Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{before.ID}, Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "new-bio"}},
			Tags:    &models.AuthorTagsUpdateNested{Disconnect: []int64{oldTag.ID}, Create: []*models.CreateTagInput{{Name: "created"}}, Connect: []int64{connected.ID}},
		}, models.AuthorConflictName, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{
				ID: true, Name: true,
				Posts:   &models.PostRelationshipOptions{},
				Profile: &models.AuthorProfileFieldOptions{ID: true, AuthorID: true, Bio: true},
				Tags:    &models.TagRelationshipOptions{},
			}
		})
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, every relationship selected) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID {
			t.Fatalf("UpsertWithRelated(a, every relationship selected) = %+v, want a's row", got)
		}
		var titles, names []string
		for _, p := range got.Posts {
			titles = append(titles, p.Title)
		}
		for _, tag := range got.Tags {
			names = append(names, tag.Name)
		}
		slices.Sort(titles)
		slices.Sort(names)
		if diff := gocmp.Diff([]string{"created", "orphan"}, titles); diff != "" {
			t.Errorf("returned Posts titles (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff([]string{"connected", "created"}, names); diff != "" {
			t.Errorf("returned Tags names (-want +got):\n%s", diff)
		}
		if got.Profile == nil || got.Profile.Bio != "new-bio" {
			t.Errorf("returned Profile = %+v, want new-bio", got.Profile)
		}
	})

	t.Run("an empty FieldOptions writes, nested rows included, and returns nil", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		tag := createTag(t, h, "tag")
		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
			Tags:   &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID}},
		}, models.AuthorConflictName, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{}
		})
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("UpsertWithRelated(a, empty FieldOptions) = %+v, want nil", got)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID, Deleted: true}})
		swrAssertPosts(t, h, map[string]*int64{"created": &a.ID})
		swrAssertPairs(t, h, [2]int64{a.ID, tag.ID})
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
		}, models.AuthorConflictName, withLock[models.AuthorFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || !swrAuthorDeleted(got) {
			t.Errorf("UpsertWithRelated(a, LockForUpdate) = %+v, want a's row soft-deleted", got)
		}
		swrAssertAuthors(t, h, map[string]swrAuthorRow{"a": {ID: a.ID, Deleted: true}})
		swrAssertPosts(t, h, map[string]*int64{"created": &a.ID})
	})

	t.Run("ignores LockMode outside a transaction with a relationship selected", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		got, err := h.Client.Authors().UpsertWithRelated(ctx, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a"},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "created"}}},
		}, models.AuthorConflictName, append(swrSelectPosts(), withLock[models.AuthorFieldOptions](sql.LockForUpdate))...)
		if err != nil {
			t.Fatalf("UpsertWithRelated(a, Posts selected, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || len(got.Posts) != 1 || got.Posts[0].Title != "created" {
			t.Errorf("UpsertWithRelated(a, Posts selected, LockForUpdate) = %+v, want a's row holding the created post", got)
		}
		swrAssertPosts(t, h, map[string]*int64{"created": &a.ID})
	})
}

// swrAuthorRow is a stored author: its key and whether it is soft-deleted.
type swrAuthorRow struct {
	ID      int64
	Deleted bool
}

// swrAuthors returns every author by name, soft-deleted ones included, read
// with a statement the counting querier does not record.
func swrAuthors(t *testing.T, h *Harness) map[string]swrAuthorRow {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id, name, CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END FROM authors")
	if err != nil {
		t.Fatalf("read authors: %v", err)
	}
	defer rows.Close()
	got := map[string]swrAuthorRow{}
	for rows.Next() {
		var (
			name    string
			row     swrAuthorRow
			deleted int64
		)
		if err := rows.Scan(&row.ID, &name, &deleted); err != nil {
			t.Fatalf("scan author: %v", err)
		}
		row.Deleted = deleted != 0
		got[name] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read authors: %v", err)
	}
	return got
}

func swrAssertAuthors(t *testing.T, h *Harness, want map[string]swrAuthorRow) {
	t.Helper()
	if diff := gocmp.Diff(want, swrAuthors(t, h)); diff != "" {
		t.Errorf("authors (-want +got):\n%s", diff)
	}
}

// swrParentsByLabel returns each row's author_id keyed by a label column whose
// values the subtest keeps unique, soft-deleted rows included.
func swrParentsByLabel(t *testing.T, h *Harness, table, label string) map[string]*int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT "+label+", author_id FROM "+table)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer rows.Close()
	got := map[string]*int64{}
	for rows.Next() {
		var key string
		var parent *int64
		if err := rows.Scan(&key, &parent); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		if _, dup := got[key]; dup {
			t.Fatalf("%s holds two rows with %s %q", table, label, key)
		}
		got[key] = parent
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return got
}

// swrAssertPosts checks every post's parent by title.
func swrAssertPosts(t *testing.T, h *Harness, want map[string]*int64) {
	t.Helper()
	if diff := gocmp.Diff(want, swrParentsByLabel(t, h, "posts", "title")); diff != "" {
		t.Errorf("post parents by title (-want +got):\n%s", diff)
	}
}

// swrAssertProfiles checks every author profile's parent by bio.
func swrAssertProfiles(t *testing.T, h *Harness, want map[string]*int64) {
	t.Helper()
	if diff := gocmp.Diff(want, swrParentsByLabel(t, h, "author_profiles", "bio")); diff != "" {
		t.Errorf("profile parents by bio (-want +got):\n%s", diff)
	}
}

// swrTagIDs returns every tag's key by name.
func swrTagIDs(t *testing.T, h *Harness) map[string]int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id, name FROM tags")
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan tag: %v", err)
		}
		got[name] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tags: %v", err)
	}
	return got
}

// swrAssertPairs checks the author_tags rows are exactly want, in any order.
func swrAssertPairs(t *testing.T, h *Harness, want ...[2]int64) {
	t.Helper()
	slices.SortFunc(want, func(x, y [2]int64) int {
		return cmp.Or(cmp.Compare(x[0], y[0]), cmp.Compare(x[1], y[1]))
	})
	if diff := gocmp.Diff(want, authorTagPairs(t, h), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("author tags (-want +got):\n%s", diff)
	}
}

// swrAssertNested checks err is a *NestedMutationError on edge and verb naming
// id, carrying sentinel.
func swrAssertNested(t *testing.T, err error, edge, verb string, id any, sentinel error) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
	}
	ne, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("error = %v, want a *NestedMutationError", err)
	}
	if ne.Edge != edge || ne.Verb != verb || ne.ID != id {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q, ID: %v}, want {Edge: %q, Verb: %q, ID: %v}", ne.Edge, ne.Verb, ne.ID, edge, verb, id)
	}
}

// swrAssertNestedConstraint checks err is a *NestedMutationError on edge and
// verb with no id, wrapping a *ConstraintError of type want.
func swrAssertNestedConstraint(t *testing.T, err error, edge, verb string, want models.ConstraintType) {
	t.Helper()
	usAssertConstraint(t, err, want)
	ne, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("error = %v, want a *NestedMutationError", err)
	}
	if ne.Edge != edge || ne.Verb != verb || ne.ID != nil {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q, ID: %v}, want {Edge: %q, Verb: %q, ID: <nil>}", ne.Edge, ne.Verb, ne.ID, edge, verb)
	}
}

// swrHookLog returns a client on base whose mutation and query hooks
// append "mutation <op> <table>" and "query <op> <table>" to one log before
// each call runs, and a func that returns the log.
func swrHookLog(base database.Querier) (*models.Client, func() []string) {
	var (
		mu  sync.Mutex
		log []string
	)
	add := func(s string) {
		mu.Lock()
		log = append(log, s)
		mu.Unlock()
	}
	mutation := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			add(swrMutation(m.Op, m.Table))
			return next(ctx, m)
		}
	}
	query := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			add(swrQuery(q.Op, q.Table))
			return next(ctx, q)
		}
	}
	return models.New(base, models.WithMutationHook(mutation), models.WithQueryHook(query)), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), log...)
	}
}

func swrMutation(op hook.MutationOp, table hook.TableName) string {
	return fmt.Sprintf("mutation %s %s", op, table)
}

func swrQuery(op hook.QueryOp, table hook.TableName) string {
	return fmt.Sprintf("query %s %s", op, table)
}

// swrTxInput is the transaction subtests' block on the conflict branch: the
// parent write soft-deletes a, Posts disconnects a1, creates "created" and
// connects orphan, Profile creates "bio", and Tags connects tag last.
func swrTxInput(a1, orphan, tag int64) *models.UpsertAuthorWithRelatedInput {
	return &models.UpsertAuthorWithRelatedInput{
		Author:  models.CreateAuthorInput{Name: "a", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{a1}, Create: []*models.AuthorPostsCreateInput{{Title: "created"}}, Connect: []int64{orphan}},
		Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}},
		Tags:    &models.AuthorTagsUpdateNested{Connect: []int64{tag}},
	}
}

// swrSelectPosts selects the parent's columns and its Posts.
func swrSelectPosts() []func(*models.CallOptions[models.AuthorFieldOptions]) {
	return []func(*models.CallOptions[models.AuthorFieldOptions]){
		func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, DeletedAt: true, Posts: &models.PostRelationshipOptions{}}
		},
	}
}

func swrAuthorDeleted(a *models.Author) bool {
	return !gocmp.Equal(a.DeletedAt, models.Author{}.DeletedAt)
}
