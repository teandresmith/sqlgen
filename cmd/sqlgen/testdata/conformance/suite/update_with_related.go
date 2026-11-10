package suite

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// updateWithRelated is the UpdateWithRelated contract.
func updateWithRelated(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("updates the parent's columns and runs create, connect and disconnect on every edge", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		theirs := createPost(t, h, "theirs", &other.ID)
		linked := createPost(t, h, "linked", &a.ID)
		kept := createPost(t, h, "kept", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		theirProfile := createProfile(t, h, "theirs", &other.ID)
		freeProfile := createProfile(t, h, "free", nil)
		theirTag, linkedTag, keptTag, freeTag := createTag(t, h, "theirs"), createTag(t, h, "linked"), createTag(t, h, "kept"), createTag(t, h, "free")
		linkAuthorTag(t, h, other.ID, theirTag.ID)
		linkAuthorTag(t, h, other.ID, freeTag.ID)
		linkAuthorTag(t, h, a.ID, linkedTag.ID)
		linkAuthorTag(t, h, a.ID, keptTag.ID)
		beforePosts := postParents(t, h)

		got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts: &models.AuthorPostsUpdateNested{
				Create:     []*models.AuthorPostsCreateInput{{Title: "fresh"}},
				Connect:    []int64{orphan.ID},
				Disconnect: []int64{linked.ID},
			},
			Profile: &models.AuthorProfileUpdateNested{Connect: &freeProfile.ID},
			Tags: &models.AuthorTagsUpdateNested{
				Create:     []*models.CreateTagInput{{Name: "new"}},
				Connect:    []int64{freeTag.ID},
				Disconnect: []int64{linkedTag.ID},
			},
		})
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, every edge) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || got.Name != "a2" {
			t.Errorf("UpdateWithRelated(a, every edge) = %+v, want a named a2", got)
		}
		if diff := cmp.Diff(map[int64]string{other.ID: "other", a.ID: "a2"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
		fresh := uwrOnlyNewID(t, beforePosts, postParents(t, h))
		if p, err := h.Client.Posts().Get(ctx, fresh); err != nil || p.Title != "fresh" {
			t.Errorf("created post %d = %+v, %v, want title fresh", fresh, p, err)
		}
		wantPosts := map[int64]*int64{
			theirs.ID: new(other.ID), linked.ID: nil, kept.ID: new(a.ID), orphan.ID: new(a.ID), fresh: new(a.ID),
		}
		if diff := cmp.Diff(wantPosts, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		wantProfiles := map[int64]*int64{theirProfile.ID: new(other.ID), freeProfile.ID: new(a.ID)}
		if diff := cmp.Diff(wantProfiles, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
		newTag := uwrTagID(t, h, "new")
		wantPairs := uwrPairs([2]int64{other.ID, theirTag.ID}, [2]int64{other.ID, freeTag.ID},
			[2]int64{a.ID, keptTag.ID}, [2]int64{a.ID, freeTag.ID}, [2]int64{a.ID, newTag})
		if diff := cmp.Diff(wantPairs, authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
		// The disconnect deleted the link, not the tag.
		assertRowCount(t, h, "tags", 5)
	})

	t.Run("clear sets the FK to NULL on every child of the edge and deletes only the parent's links", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		theirs := createPost(t, h, "theirs", &other.ID)
		p1, p2, p3 := createPost(t, h, "p1", &a.ID), createPost(t, h, "p2", &a.ID), createPost(t, h, "p3", &a.ID)
		theirProfile := createProfile(t, h, "theirs", &other.ID)
		mine := createProfile(t, h, "mine", &a.ID)
		shared, t1, t2 := createTag(t, h, "shared"), createTag(t, h, "t1"), createTag(t, h, "t2")
		linkAuthorTag(t, h, other.ID, shared.ID)
		linkAuthorTag(t, h, a.ID, shared.ID)
		linkAuthorTag(t, h, a.ID, t1.ID)
		linkAuthorTag(t, h, a.ID, t2.ID)

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts:   &models.AuthorPostsUpdateNested{Clear: true},
			Profile: &models.AuthorProfileUpdateNested{Clear: true},
			Tags:    &models.AuthorTagsUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, clear every edge) error = %v, want nil", err)
		}
		wantPosts := map[int64]*int64{theirs.ID: new(other.ID), p1.ID: nil, p2.ID: nil, p3.ID: nil}
		if diff := cmp.Diff(wantPosts, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		wantProfiles := map[int64]*int64{theirProfile.ID: new(other.ID), mine.ID: nil}
		if diff := cmp.Diff(wantProfiles, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(uwrPairs([2]int64{other.ID, shared.ID}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "tags", 3)
	})

	t.Run("clear runs before create and connect, so clear with connect leaves exactly the rows the block names", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		p1, p2 := createPost(t, h, "p1", &a.ID), createPost(t, h, "p2", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		t1, t2, t3 := createTag(t, h, "t1"), createTag(t, h, "t2"), createTag(t, h, "t3")
		linkAuthorTag(t, h, a.ID, t1.ID)
		linkAuthorTag(t, h, a.ID, t2.ID)
		beforePosts := postParents(t, h)

		// p2 and t2 are on the edge already: clear unlinks them first, and
		// connect links them again.
		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{
				Clear:   true,
				Create:  []*models.AuthorPostsCreateInput{{Title: "fresh"}},
				Connect: []int64{p2.ID, orphan.ID},
			},
			Tags: &models.AuthorTagsUpdateNested{
				Clear:   true,
				Create:  []*models.CreateTagInput{{Name: "new"}},
				Connect: []int64{t2.ID, t3.ID},
			},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, clear with create and connect) error = %v, want nil", err)
		}
		fresh := uwrOnlyNewID(t, beforePosts, postParents(t, h))
		wantPosts := map[int64]*int64{p1.ID: nil, p2.ID: new(a.ID), orphan.ID: new(a.ID), fresh: new(a.ID)}
		if diff := cmp.Diff(wantPosts, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		wantPairs := uwrPairs([2]int64{a.ID, t2.ID}, [2]int64{a.ID, t3.ID}, [2]int64{a.ID, uwrTagID(t, h, "new")})
		if diff := cmp.Diff(wantPairs, authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
	})

	t.Run("with clear set, disconnect is ignored, so one naming a connect target is no conflict", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		freeProfile := createProfile(t, h, "free", nil)
		tag := createTag(t, h, "t")

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts:   &models.AuthorPostsUpdateNested{Clear: true, Connect: []int64{orphan.ID}, Disconnect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Connect: &freeProfile.ID, Disconnect: &freeProfile.ID},
			Tags:    &models.AuthorTagsUpdateNested{Clear: true, Connect: []int64{tag.ID}, Disconnect: []int64{tag.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, clear with connect and disconnect of one target) error = %v, want nil", err)
		}
		if diff := cmp.Diff(map[int64]*int64{orphan.ID: new(a.ID)}, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]*int64{freeProfile.ID: new(a.ID)}, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(uwrPairs([2]int64{a.ID, tag.ID}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
	})

	t.Run("disconnect sets the FK to NULL on the named children and deletes only the named links", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		p1, p2, p3 := createPost(t, h, "p1", &a.ID), createPost(t, h, "p2", &a.ID), createPost(t, h, "p3", &a.ID)
		profile := createProfile(t, h, "mine", &a.ID)
		t1, t2, t3 := createTag(t, h, "t1"), createTag(t, h, "t2"), createTag(t, h, "t3")
		for _, tag := range []*models.Tag{t1, t2, t3} {
			linkAuthorTag(t, h, a.ID, tag.ID)
		}

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{p3.ID, p1.ID}},
			Profile: &models.AuthorProfileUpdateNested{Disconnect: &profile.ID},
			Tags:    &models.AuthorTagsUpdateNested{Disconnect: []int64{t3.ID, t1.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, disconnect) error = %v, want nil", err)
		}
		wantPosts := map[int64]*int64{p1.ID: nil, p2.ID: new(a.ID), p3.ID: nil}
		if diff := cmp.Diff(wantPosts, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]*int64{profile.ID: nil}, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(uwrPairs([2]int64{a.ID, t2.ID}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "tags", 3)
	})

	t.Run("disconnect is scoped to the parent, so another parent's child and a missing id are left alone", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		theirs := createPost(t, h, "theirs", &other.ID)
		mine := createPost(t, h, "mine", &a.ID)
		theirProfile := createProfile(t, h, "theirs", &other.ID)
		myProfile := createProfile(t, h, "mine", &a.ID)
		theirTag, myTag := createTag(t, h, "theirs"), createTag(t, h, "mine")
		linkAuthorTag(t, h, other.ID, theirTag.ID)
		linkAuthorTag(t, h, a.ID, myTag.ID)
		missing := myTag.ID + mine.ID + myProfile.ID + 1000
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts:   &models.AuthorPostsUpdateNested{Disconnect: []int64{theirs.ID, missing}},
			Profile: &models.AuthorProfileUpdateNested{Disconnect: &theirProfile.ID},
			Tags:    &models.AuthorTagsUpdateNested{Disconnect: []int64{theirTag.ID, missing}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, disconnect another parent's children) error = %v, want nil", err)
		}
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
	})

	t.Run("a junction keyed by a surrogate links through its UNIQUE, and a re-link keeps the row", func(t *testing.T) {
		h.Reset(t)
		crate, err := h.Client.Crates().Create(ctx, &models.CreateCrateInput{Label: "crate"})
		if err != nil {
			t.Fatalf("Create crate error = %v, want nil", err)
		}
		linked, err := h.Client.Seals().Create(ctx, &models.CreateSealInput{Name: "linked"})
		if err != nil {
			t.Fatalf("Create seal error = %v, want nil", err)
		}
		free, err := h.Client.Seals().Create(ctx, &models.CreateSealInput{Name: "free"})
		if err != nil {
			t.Fatalf("Create seal error = %v, want nil", err)
		}

		if _, err := h.Client.Crates().UpdateWithRelated(ctx, crate.ID, &models.UpdateCrateWithRelatedInput{
			Seals: &models.CrateSealsUpdateNested{Connect: []int64{linked.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(first link) error = %v, want nil", err)
		}
		first := uwrSealRows(t, h, crate.ID)
		if len(first) != 1 {
			t.Fatalf("crate seals = %v, want one link", first)
		}

		if _, err := h.Client.Crates().UpdateWithRelated(ctx, crate.ID, &models.UpdateCrateWithRelatedInput{
			Seals: &models.CrateSealsUpdateNested{
				Create:  []*models.CreateSealInput{{Name: "created"}},
				Connect: []int64{linked.ID, free.ID},
			},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(re-link) error = %v, want nil", err)
		}
		rows := uwrSealRows(t, h, crate.ID)
		if len(rows) != 3 {
			t.Fatalf("crate seals = %v, want three links", rows)
		}
		if rows[linked.ID] != first[linked.ID] {
			t.Errorf("re-linked seal's junction id = %d, want the row's own %d", rows[linked.ID], first[linked.ID])
		}

		if _, err := h.Client.Crates().UpdateWithRelated(ctx, crate.ID, &models.UpdateCrateWithRelatedInput{
			Seals: &models.CrateSealsUpdateNested{Clear: true, Connect: []int64{free.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(clear and connect) error = %v, want nil", err)
		}
		if rows := uwrSealRows(t, h, crate.ID); len(rows) != 1 || rows[free.ID] == 0 {
			t.Errorf("crate seals after clear = %v, want only seal %d", rows, free.ID)
		}
	})

	t.Run("connect leaves a post or tag already on the edge in place and adds the others", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		ours := createPost(t, h, "ours", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		linked, free := createTag(t, h, "linked"), createTag(t, h, "free")
		linkAuthorTag(t, h, a.ID, linked.ID)

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{Connect: []int64{ours.ID, orphan.ID}},
			Tags:  &models.AuthorTagsUpdateNested{Connect: []int64{linked.ID, free.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, connect rows already on the edge) error = %v, want nil", err)
		}
		if diff := cmp.Diff(map[int64]*int64{ours.ID: new(a.ID), orphan.ID: new(a.ID)}, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(uwrPairs([2]int64{a.ID, linked.ID}, [2]int64{a.ID, free.ID}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
	})

	t.Run("one target under connect and disconnect is ErrNestedVerbConflict before any statement on that edge", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			block func(post, profile, tag int64) *models.UpdateAuthorWithRelatedInput
			edge  string
			// earlier are the inner writes an earlier edge in the block runs
			// before the refused edge is reached.
			earlier []string
			target  func(post, profile, tag int64) int64
		}{
			{
				name: "Posts",
				block: func(post, _, _ int64) *models.UpdateAuthorWithRelatedInput {
					return &models.UpdateAuthorWithRelatedInput{Posts: &models.AuthorPostsUpdateNested{
						Create:     []*models.AuthorPostsCreateInput{{Title: "fresh"}},
						Connect:    []int64{post},
						Disconnect: []int64{post},
					}}
				},
				edge:   "Posts",
				target: func(post, _, _ int64) int64 { return post },
			},
			{
				name: "Profile",
				block: func(_, profile, _ int64) *models.UpdateAuthorWithRelatedInput {
					return &models.UpdateAuthorWithRelatedInput{
						Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "earlier"}}},
						Profile: &models.AuthorProfileUpdateNested{Connect: &profile, Disconnect: &profile},
					}
				},
				edge:    "Profile",
				earlier: []string{"create_many " + string(models.TablePosts)},
				target:  func(_, profile, _ int64) int64 { return profile },
			},
			{
				name: "Tags",
				block: func(_, _, tag int64) *models.UpdateAuthorWithRelatedInput {
					return &models.UpdateAuthorWithRelatedInput{
						Posts: &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "earlier"}}},
						Tags: &models.AuthorTagsUpdateNested{
							Create:     []*models.CreateTagInput{{Name: "new"}},
							Connect:    []int64{tag},
							Disconnect: []int64{tag},
						},
					}
				},
				edge:    "Tags",
				earlier: []string{"create_many " + string(models.TablePosts)},
				target:  func(_, _, tag int64) int64 { return tag },
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				post := createPost(t, h, "mine", &a.ID)
				// Unparented, and a holds no profile: a conflict check that ran
				// late would let the connect adopt it first.
				profile := createProfile(t, h, "free", nil)
				tag := createTag(t, h, "mine")
				linkAuthorTag(t, h, a.ID, tag.ID)
				posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
				in := tt.block(post.ID, profile.ID, tag.ID)
				in.Author = models.UpdateAuthorInput{Name: omittable.Set("a2")}

				client, seen := mutationRecorder(h)
				_, err := client.Authors().UpdateWithRelated(ctx, a.ID, in)
				uwrWantNested(t, err, models.ErrNestedVerbConflict, tt.edge, "disconnect", tt.target(post.ID, profile.ID, tag.ID))
				want := append([]string{"update " + string(models.TableAuthors)}, tt.earlier...)
				if diff := cmp.Diff(want, uwrOps(seen())); diff != "" {
					t.Errorf("inner writes of the refused call (-want +got):\n%s", diff)
				}
				uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
				if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
					t.Errorf("author names (-want +got):\n%s", diff)
				}
				assertRowCount(t, h, "tags", 1)
			})
		}
	})

	t.Run("create or connect on a has-one already holding a child is ErrAlreadyRelated naming that child", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			block func(current, free int64) *models.AuthorProfileUpdateNested
			verb  string
		}{
			{"create", func(_, _ int64) *models.AuthorProfileUpdateNested {
				return &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "second"}}
			}, "create"},
			{"connect", func(_, free int64) *models.AuthorProfileUpdateNested {
				return &models.AuthorProfileUpdateNested{Connect: &free}
			}, "connect"},
			// disconnect runs after create and connect, so the occupancy read
			// still finds the current child.
			{"disconnect of the current child with create", func(current, _ int64) *models.AuthorProfileUpdateNested {
				return &models.AuthorProfileUpdateNested{Disconnect: &current, Create: &models.AuthorProfileCreateInput{Bio: "second"}}
			}, "create"},
			{"disconnect of the current child with connect", func(current, free int64) *models.AuthorProfileUpdateNested {
				return &models.AuthorProfileUpdateNested{Disconnect: &current, Connect: &free}
			}, "connect"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				other := createAuthor(t, h, "other")
				a := createAuthor(t, h, "a")
				createProfile(t, h, "theirs", &other.ID)
				current := createProfile(t, h, "current", &a.ID)
				free := createProfile(t, h, "free", nil)
				block := tt.block(current.ID, free.ID)
				profiles := profileParents(t, h)
				_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{Profile: block})
				uwrWantNested(t, err, models.ErrAlreadyRelated, "Profile", tt.verb, current.ID)
				if diff := cmp.Diff(profiles, profileParents(t, h)); diff != "" {
					t.Errorf("profile parents after the refused call (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("a has-one block setting create and connect is ErrNestedVerbConflict attributed to connect before any statement on that edge", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createProfile(t, h, "current", &a.ID)
		free := createProfile(t, h, "free", nil)
		profiles := profileParents(t, h)
		client, seen := mutationRecorder(h)
		// The block also sets clear, which would run first and empty the edge,
		// so the refusal shows up only in the inner writes: a check that ran
		// after any statement on the edge would leave that write in the list.
		_, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "second"}, Connect: &free.ID},
		})
		uwrWantNested(t, err, models.ErrNestedVerbConflict, "Profile", "connect", free.ID)
		if diff := cmp.Diff([]string{"update " + string(models.TableAuthors)}, uwrOps(seen())); diff != "" {
			t.Errorf("inner writes of the refused call (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(profiles, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
	})

	t.Run("clear with create or connect replaces the has-one's child", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			connect bool
		}{{"create", false}, {"connect", true}} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				current := createProfile(t, h, "current", &a.ID)
				free := createProfile(t, h, "free", nil)
				block := &models.AuthorProfileUpdateNested{Clear: true}
				if tt.connect {
					block.Connect = &free.ID
				} else {
					block.Create = &models.AuthorProfileCreateInput{Bio: "fresh"}
				}
				before := profileParents(t, h)
				if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{Profile: block}); err != nil {
					t.Fatalf("UpdateWithRelated(a, clear with %s) error = %v, want nil", tt.name, err)
				}
				want := map[int64]*int64{current.ID: nil, free.ID: nil}
				if tt.connect {
					want[free.ID] = new(a.ID)
				} else {
					want[uwrOnlyNewID(t, before, profileParents(t, h))] = new(a.ID)
				}
				if diff := cmp.Diff(want, profileParents(t, h)); diff != "" {
					t.Errorf("profile parents (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("connect naming the has-one's current child changes nothing", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		current := createProfile(t, h, "current", &a.ID)
		createProfile(t, h, "free", nil)
		before := profileParents(t, h)
		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Profile: &models.AuthorProfileUpdateNested{Connect: &current.ID},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, connect the current profile) error = %v, want nil", err)
		}
		if diff := cmp.Diff(before, profileParents(t, h)); diff != "" {
			t.Errorf("profile parents (-want +got):\n%s", diff)
		}
	})

	t.Run("disconnect and clear set the FK to NULL on a soft-deleted child", func(t *testing.T) {
		h.Reset(t)
		a, b := createAuthor(t, h, "a"), createAuthor(t, h, "b")
		live := createPost(t, h, "live", &a.ID)
		gone := uwrDeletedPost(t, h, "gone", a.ID)
		cleared := uwrDeletedPost(t, h, "cleared", b.ID)

		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{Disconnect: []int64{gone.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, disconnect a soft-deleted post) error = %v, want nil", err)
		}
		if _, err := h.Client.Authors().UpdateWithRelated(ctx, b.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(b, clear) error = %v, want nil", err)
		}
		want := map[int64]*int64{live.ID: new(a.ID), gone.ID: nil, cleared.ID: nil}
		if diff := cmp.Diff(want, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
	})

	t.Run("connect and disconnect lists longer than the batch size write every id, one inner call per chunk", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		// Bystanders between the targets, so a range in place of an id list
		// would reach one.
		var linked, orphans []int64
		bystanders := map[int64]*int64{}
		for i := range 3 {
			linked = append(linked, createPost(t, h, fmt.Sprintf("linked%d", i), &a.ID).ID)
			bystanders[createPost(t, h, fmt.Sprintf("theirs%d", i), &other.ID).ID] = new(other.ID)
			orphans = append(orphans, createPost(t, h, fmt.Sprintf("orphan%d", i), nil).ID)
		}
		var linkedTags, freeTags []int64
		for i := range 3 {
			tag := createTag(t, h, fmt.Sprintf("linked%d", i))
			linkAuthorTag(t, h, a.ID, tag.ID)
			linkedTags = append(linkedTags, tag.ID)
			theirs := createTag(t, h, fmt.Sprintf("theirs%d", i))
			linkAuthorTag(t, h, other.ID, theirs.ID)
			freeTags = append(freeTags, createTag(t, h, fmt.Sprintf("free%d", i)).ID)
		}
		theirPairs := uwrPairsOf(t, h, other.ID)

		client, mutations, queries := uwrHookClient(h.Base)
		if _, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{
				Connect:    []int64{orphans[2], orphans[0], orphans[1]},
				Disconnect: []int64{linked[1], linked[2], linked[0]},
			},
			Tags: &models.AuthorTagsUpdateNested{
				Connect:    []int64{freeTags[2], freeTags[0], freeTags[1]},
				Disconnect: []int64{linkedTags[1], linkedTags[2], linkedTags[0]},
			},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, three ids per verb) error = %v, want nil", err)
		}
		// Each chunk of two ids is an inner call of its own.
		wantCalls := map[string]int{
			"update " + string(models.TableAuthors):                             1,
			"get_many " + string(models.TablePosts):                             2,
			"update_where " + string(models.TablePosts) + " set author_id":      2,
			"update_where " + string(models.TablePosts) + " set author_id NULL": 2,
			"get_many " + string(models.TableTags):                              2,
			"upsert_many " + string(models.TableAuthorTags):                     1,
			"hard_delete_many " + string(models.TableAuthorTags):                2,
		}
		if diff := cmp.Diff(wantCalls, uwrCallCounts(mutations(), queries())); diff != "" {
			t.Errorf("inner calls per op (-want +got):\n%s", diff)
		}
		want := bystanders
		for i := range 3 {
			want[linked[i]] = nil
			want[orphans[i]] = new(a.ID)
		}
		if diff := cmp.Diff(want, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		wantPairs := theirPairs
		for _, id := range freeTags {
			wantPairs = append(wantPairs, [2]int64{a.ID, id})
		}
		if diff := cmp.Diff(uwrPairs(wantPairs...), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
	})

	t.Run("a missing parent key is ErrNotFound and writes nothing, with or without parent fields set", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			author models.UpdateAuthorInput
		}{
			{"Name", models.UpdateAuthorInput{Name: omittable.Set("x")}},
			{"no parent field", models.UpdateAuthorInput{}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				orphan := createPost(t, h, "orphan", nil)
				createProfile(t, h, "free", nil)
				posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
				got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID+1000, &models.UpdateAuthorWithRelatedInput{
					Author:  tt.author,
					Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}, Connect: []int64{orphan.ID}},
					Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
					Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}}},
				})
				if !errors.Is(err, models.ErrNotFound) || got != nil {
					t.Errorf("UpdateWithRelated(missing key, %s) = %+v, %v, want nil, ErrNotFound", tt.name, got, err)
				}
				uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
				assertRowCount(t, h, "tags", 0)
				if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
					t.Errorf("author names (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("connect naming an absent or soft-deleted target is ErrNotFound naming it on that edge", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			edge  string
			block func(orphan, bad int64) *models.UpdateAuthorWithRelatedInput
			// bad picks the id the connect cannot see: absent, or the
			// soft-deleted post.
			softDeleted bool
		}{
			{"an absent post", "Posts", func(orphan, bad int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{Posts: &models.AuthorPostsUpdateNested{Connect: []int64{orphan, bad}}}
			}, false},
			{"a soft-deleted post", "Posts", func(orphan, bad int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{Posts: &models.AuthorPostsUpdateNested{Connect: []int64{orphan, bad}}}
			}, true},
			{"an absent profile", "Profile", func(orphan, bad int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{
					Posts:   &models.AuthorPostsUpdateNested{Connect: []int64{orphan}},
					Profile: &models.AuthorProfileUpdateNested{Connect: &bad},
				}
			}, false},
			{"an absent tag", "Tags", func(orphan, bad int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{
					Posts: &models.AuthorPostsUpdateNested{Connect: []int64{orphan}},
					Tags:  &models.AuthorTagsUpdateNested{Connect: []int64{bad}},
				}
			}, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				orphan := createPost(t, h, "orphan", nil)
				deleted := uwrDeletedPost(t, h, "deleted", 0)
				tag := createTag(t, h, "t")
				profile := createProfile(t, h, "free", nil)
				bad := deleted.ID + tag.ID + profile.ID + 1000
				if tt.softDeleted {
					bad = deleted.ID
				}
				posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
				in := tt.block(orphan.ID, bad)
				in.Author = models.UpdateAuthorInput{Name: omittable.Set("a2")}
				_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, in)
				uwrWantNested(t, err, models.ErrNotFound, tt.edge, "connect", bad)
				// The parent write rolls back. On the Profile and Tags cases the
				// Posts edge adopted the orphan before the failing connect, and
				// that adoption rolls back too; on the Posts cases the miss is
				// found before any adoption runs.
				uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
				if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
					t.Errorf("author names (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("returns the parent it soft-deletes, with or without a relationship selected", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			fo   *models.AuthorFieldOptions
		}{
			{"no relationship", nil},
			{"Posts selected", &models.AuthorFieldOptions{ID: true, Name: true, DeletedAt: true, Posts: &models.PostRelationshipOptions{}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
					Author: models.UpdateAuthorInput{DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
					Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}},
				}, uwrSelect(tt.fo))
				if err != nil {
					t.Fatalf("UpdateWithRelated(a, DeletedAt set, %s) error = %v, want nil", tt.name, err)
				}
				if got == nil || got.ID != a.ID || got.Name != "a" || !uwrAuthorDeleted(got) {
					t.Fatalf("UpdateWithRelated(a, DeletedAt set, %s) = %+v, want a with DeletedAt set", tt.name, got)
				}
				if tt.fo != nil && (len(got.Posts) != 1 || got.Posts[0].Title != "fresh") {
					t.Errorf("returned Posts = %+v, want the created post", got.Posts)
				}
				if diff := cmp.Diff([]int64{a.ID}, uwrDeletedAuthorIDs(t, h)); diff != "" {
					t.Errorf("soft-deleted authors (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("returns a soft-deleted parent it updates, with or without a relationship selected", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			fo   *models.AuthorFieldOptions
		}{
			{"no relationship", nil},
			{"Posts selected", &models.AuthorFieldOptions{ID: true, Name: true, DeletedAt: true, Posts: &models.PostRelationshipOptions{}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				if _, err := h.Client.Authors().SoftDelete(ctx, a.ID); err != nil {
					t.Fatalf("SoftDelete(a) error = %v", err)
				}
				got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
					Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
					Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}},
				}, uwrSelect(tt.fo))
				if err != nil {
					t.Fatalf("UpdateWithRelated(soft-deleted a, %s) error = %v, want nil", tt.name, err)
				}
				if got == nil || got.ID != a.ID || got.Name != "a2" || !uwrAuthorDeleted(got) {
					t.Fatalf("UpdateWithRelated(soft-deleted a, %s) = %+v, want a named a2 with DeletedAt set", tt.name, got)
				}
				if tt.fo != nil && (len(got.Posts) != 1 || got.Posts[0].Title != "fresh") {
					t.Errorf("returned Posts = %+v, want the created post", got.Posts)
				}
			})
		}
	})

	t.Run("a failure on a later edge rolls back the parent write and the earlier edge's writes", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		linked := createPost(t, h, "linked", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "t")
		missing := tag.ID + 1000
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
		in := func(tags []int64) *models.UpdateAuthorWithRelatedInput {
			return &models.UpdateAuthorWithRelatedInput{
				Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
				Posts: &models.AuthorPostsUpdateNested{
					Create:     []*models.AuthorPostsCreateInput{{Title: "fresh"}},
					Connect:    []int64{orphan.ID},
					Disconnect: []int64{linked.ID},
				},
				Tags: &models.AuthorTagsUpdateNested{Connect: tags},
			}
		}

		client, seen := mutationRecorder(h)
		_, err := client.Authors().UpdateWithRelated(ctx, a.ID, in([]int64{missing}))
		uwrWantNested(t, err, models.ErrNotFound, "Tags", "connect", missing)
		// The parent and the Posts edge were written before the Tags connect failed.
		wantOps := []string{
			"update " + string(models.TableAuthors),
			"create_many " + string(models.TablePosts),
			"update_where " + string(models.TablePosts),
			"update_where " + string(models.TablePosts),
		}
		if diff := cmp.Diff(wantOps, uwrOps(seen())); diff != "" {
			t.Errorf("inner writes of the failed call (-want +got):\n%s", diff)
		}
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names after the failed call (-want +got):\n%s", diff)
		}

		// The same call with a visible tag writes everything it rolled back.
		if _, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, in([]int64{tag.ID})); err != nil {
			t.Fatalf("UpdateWithRelated(a, visible tag) error = %v, want nil", err)
		}
		fresh := uwrOnlyNewID(t, posts, postParents(t, h))
		wantPosts := map[int64]*int64{linked.ID: nil, orphan.ID: new(a.ID), fresh: new(a.ID)}
		if diff := cmp.Diff(wantPosts, postParents(t, h)); diff != "" {
			t.Errorf("post parents after the successful call (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "a2"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names after the successful call (-want +got):\n%s", diff)
		}
	})

	t.Run("inside the caller's transaction a failure undoes only its own writes, and a later call commits with the transaction", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		linked := createPost(t, h, "linked", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "t")
		var callerID int64
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			caller, err := h.Client.Authors().Create(txCtx, &models.CreateAuthorInput{Name: "caller"})
			if err != nil {
				return err
			}
			callerID = caller.ID
			_, err = h.Client.Authors().UpdateWithRelated(txCtx, a.ID, &models.UpdateAuthorWithRelatedInput{
				Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
				Posts:  &models.AuthorPostsUpdateNested{Clear: true},
				Tags:   &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID + 1000}},
			})
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("UpdateWithRelated inside the transaction error = %v, want ErrNotFound", err)
			}
			// The clear and the parent write are undone inside the transaction.
			p, err := h.Client.Posts().Get(txCtx, linked.ID)
			if err != nil {
				return err
			}
			if p.AuthorID == nil || *p.AuthorID != a.ID {
				t.Errorf("post author inside the transaction = %v, want %d", p.AuthorID, a.ID)
			}
			got, err := h.Client.Authors().Get(txCtx, a.ID)
			if err != nil {
				return err
			}
			if got.Name != "a" {
				t.Errorf("author name inside the transaction = %q, want a", got.Name)
			}
			// The transaction is still usable.
			if _, err := h.Client.Authors().Update(txCtx, a.ID, &models.UpdateAuthorInput{Name: omittable.Set("after")}); err != nil {
				return err
			}
			_, err = h.Client.Authors().UpdateWithRelated(txCtx, a.ID, &models.UpdateAuthorWithRelatedInput{
				Posts: &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
				Tags:  &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID}},
			})
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "after", callerID: "caller"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names after the commit (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]*int64{linked.ID: new(a.ID), orphan.ID: new(a.ID)}, postParents(t, h)); diff != "" {
			t.Errorf("post parents after the commit (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(uwrPairs([2]int64{a.ID, tag.ID}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("inside the caller's transaction its writes are visible to the caller and undone by the caller's rollback", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "t")
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Authors().UpdateWithRelated(txCtx, a.ID, &models.UpdateAuthorWithRelatedInput{
				Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
				Posts:   &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
				Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
				Tags:    &models.AuthorTagsUpdateNested{Connect: []int64{tag.ID}},
			}); err != nil {
				return err
			}
			got, err := h.Client.Authors().Get(txCtx, a.ID, uwrSelect(&models.AuthorFieldOptions{
				Name: true, Posts: &models.PostRelationshipOptions{}, Profile: &models.AuthorProfileFieldOptions{ID: true, Bio: true}, Tags: &models.TagRelationshipOptions{},
			}))
			if err != nil {
				return err
			}
			if got.Name != "a2" || len(got.Posts) != 1 || got.Posts[0].ID != orphan.ID || got.Profile == nil || got.Profile.Bio != "fresh" || len(got.Tags) != 1 || got.Tags[0].ID != tag.ID {
				t.Errorf("author inside the transaction = %+v, want a2 holding the orphan post, the profile it created and the tag", got)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one mutation hook per inner write, the parent first, then each edge in field-name order and each verb in execution order", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		linked := createPost(t, h, "linked", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		createProfile(t, h, "current", &a.ID)
		linkedTag, freeTag := createTag(t, h, "linked"), createTag(t, h, "free")
		linkAuthorTag(t, h, a.ID, linkedTag.ID)

		client, seen := mutationRecorder(h)
		if _, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts: &models.AuthorPostsUpdateNested{
				Create:     []*models.AuthorPostsCreateInput{{Title: "fresh"}},
				Connect:    []int64{orphan.ID},
				Disconnect: []int64{linked.ID},
			},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
			Tags: &models.AuthorTagsUpdateNested{
				Create:     []*models.CreateTagInput{{Name: "new"}},
				Connect:    []int64{freeTag.ID},
				Disconnect: []int64{linkedTag.ID},
			},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, every verb) error = %v, want nil", err)
		}
		want := []string{
			"update " + string(models.TableAuthors),
			"create_many " + string(models.TablePosts),
			"update_where " + string(models.TablePosts) + " set author_id",
			"update_where " + string(models.TablePosts) + " set author_id NULL",
			"update_where " + string(models.TableAuthorProfiles),
			"create " + string(models.TableAuthorProfiles),
			"create_many " + string(models.TableTags),
			"upsert_many " + string(models.TableAuthorTags),
			"hard_delete_many " + string(models.TableAuthorTags),
		}
		got := uwrOps(seen())
		// The two Posts UpdateWhere calls are the connect and the
		// disconnect; their inputs tell them apart.
		for i, m := range seen() {
			if in, ok := m.Input.(*models.UpdatePostInput); ok && i < len(got) {
				if v, _ := in.AuthorID.Get(); v == nil {
					got[i] += " set author_id NULL"
				} else {
					got[i] += " set author_id"
				}
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("inner writes (-want +got):\n%s", diff)
		}
		if m := seen()[0]; m.PK != any(a.ID) {
			t.Errorf("parent MutationContext.PK = %v, want %d", m.PK, a.ID)
		}

		// On an M2M edge clear deletes the links before connect links again.
		client, seen = mutationRecorder(h)
		if _, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("a3")},
			Tags:   &models.AuthorTagsUpdateNested{Clear: true, Connect: []int64{linkedTag.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(a, clear and connect tags) error = %v, want nil", err)
		}
		want = []string{
			"update " + string(models.TableAuthors),
			"hard_delete_where " + string(models.TableAuthorTags),
			"upsert_many " + string(models.TableAuthorTags),
		}
		if diff := cmp.Diff(want, uwrOps(seen())); diff != "" {
			t.Errorf("inner writes of clear and connect (-want +got):\n%s", diff)
		}
	})

	t.Run("SkipHooks on the call reaches every inner call, so no hook fires", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		linked := createPost(t, h, "linked", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "t")
		client, mutations, queries := uwrHookClient(h.Base)
		got, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}, Connect: []int64{orphan.ID}, Disconnect: []int64{linked.ID}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}}, Connect: []int64{tag.ID}},
		}, func(o *models.CallOptions[models.AuthorFieldOptions]) {
			o.SkipHooks = true
			o.FieldOptions = &models.AuthorFieldOptions{ID: true, Posts: &models.PostRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, SkipHooks) error = %v, want nil", err)
		}
		if got == nil {
			t.Fatal("UpdateWithRelated(a, SkipHooks) = nil, want a")
		}
		var titles []string
		for _, p := range got.Posts {
			titles = append(titles, p.Title)
		}
		slices.Sort(titles)
		if diff := cmp.Diff([]string{"fresh", "orphan"}, titles); diff != "" {
			t.Errorf("returned post titles under SkipHooks (-want +got):\n%s", diff)
		}
		if m := mutations(); len(m) != 0 {
			t.Errorf("mutation hooks fired under SkipHooks: %q", uwrOps(m))
		}
		if q := queries(); len(q) != 0 {
			t.Errorf("query hooks fired under SkipHooks: %q", uwrQueryOps(q))
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "a2"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 1)
	})

	t.Run("the terminal parent read and the adoption's verify read fire no query hook", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		tag := createTag(t, h, "t")
		linkAuthorTag(t, h, a.ID, tag.ID)
		client, queries := queryRecorder(h)
		got, err := client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
		}, uwrSelect(&models.AuthorFieldOptions{ID: true, Tags: &models.TagRelationshipOptions{}}))
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, connect, Tags selected) error = %v, want nil", err)
		}
		if got == nil || len(got.Tags) != 1 || got.Tags[0].ID != tag.ID {
			t.Fatalf("UpdateWithRelated(a, connect, Tags selected) = %+v, want a holding the tag", got)
		}
		// The connect's visibility read fires its hook, and so does the Tags
		// load, which takes the caller's hooks; the parent read that carries
		// it and the adoption's verify read fire none.
		want := []string{"get_many " + string(models.TablePosts), "get_many " + string(models.TableTags)}
		if diff := cmp.Diff(want, uwrQueryOps(queries())); diff != "" {
			t.Errorf("query hooks (-want +got):\n%s", diff)
		}
	})

	t.Run("a name taken by another author is a unique ConstraintError and the edges are not written", func(t *testing.T) {
		h.Reset(t)
		createAuthor(t, h, "taken")
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
		_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("taken")},
			Posts:  &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
		})
		uwrAssertConstraint(t, err, models.ConstraintUnique)
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
	})

	t.Run("a nested tag create whose name is taken is a unique ConstraintError attributed to the Tags create", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createTag(t, h, "taken")
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
		_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts:  &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}},
			Tags:   &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}, {Name: "taken"}}},
		})
		uwrWantNested(t, err, models.ErrConstraintViolation, "Tags", "create", nil)
		uwrAssertConstraint(t, err, models.ConstraintUnique)
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
		assertRowCount(t, h, "tags", 1)
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
	})

	t.Run("a nested post create with an empty title is a check ConstraintError attributed to the Posts create", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
		_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
			// The empty title is in the second chunk, so the first is written
			// and rolled back.
			Posts: &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "p1"}, {Title: "p2"}, {Title: ""}}},
		})
		uwrWantNested(t, err, models.ErrConstraintViolation, "Posts", "create", nil)
		uwrAssertConstraint(t, err, models.ConstraintCheck)
		uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
	})

	t.Run("connect naming a row parented to another author is ErrAlreadyRelated naming it", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			edge  string
			block func(post, profile int64) *models.UpdateAuthorWithRelatedInput
			pick  func(post, profile int64) int64
		}{
			{"a post", "Posts", func(post, _ int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{Posts: &models.AuthorPostsUpdateNested{Connect: []int64{post}}}
			}, func(post, _ int64) int64 { return post }},
			{"a profile", "Profile", func(_, profile int64) *models.UpdateAuthorWithRelatedInput {
				return &models.UpdateAuthorWithRelatedInput{Profile: &models.AuthorProfileUpdateNested{Connect: &profile}}
			}, func(_, profile int64) int64 { return profile }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				other := createAuthor(t, h, "other")
				orphan := createPost(t, h, "orphan", nil)
				theirs := createPost(t, h, "theirs", &other.ID)
				theirProfile := createProfile(t, h, "theirs", &other.ID)
				posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
				in := tt.block(theirs.ID, theirProfile.ID)
				if in.Posts == nil {
					in.Posts = &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}}
				} else {
					in.Posts.Connect = append([]int64{orphan.ID}, in.Posts.Connect...)
				}
				_, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, in)
				uwrWantNested(t, err, models.ErrAlreadyRelated, tt.edge, "connect", tt.pick(theirs.ID, theirProfile.ID))
				uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
			})
		}
	})

	t.Run("a nil FieldOptions returns the parent with no relationship populated, on the edges it wrote as well", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}}},
		})
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, nil FieldOptions) error = %v, want nil", err)
		}
		if diff := cmp.Diff(&models.Author{ID: a.ID, Name: "a2"}, got); diff != "" {
			t.Errorf("UpdateWithRelated(a, nil FieldOptions) (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "posts", 1)
		assertRowCount(t, h, "author_profiles", 1)
		assertRowCount(t, h, "author_tags", 1)
	})

	t.Run("a selected relationship is returned holding the rows the call wrote", func(t *testing.T) {
		h.Reset(t)
		other := createAuthor(t, h, "other")
		a := createAuthor(t, h, "a")
		createPost(t, h, "theirs", &other.ID)
		kept := createPost(t, h, "kept", &a.ID)
		dropped := createPost(t, h, "dropped", &a.ID)
		orphan := createPost(t, h, "orphan", nil)
		old := createProfile(t, h, "old", &a.ID)
		keptTag, droppedTag, freeTag := createTag(t, h, "kept"), createTag(t, h, "dropped"), createTag(t, h, "free")
		linkAuthorTag(t, h, a.ID, keptTag.ID)
		linkAuthorTag(t, h, a.ID, droppedTag.ID)
		posts, profiles := postParents(t, h), profileParents(t, h)

		got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{{Title: "fresh"}}, Connect: []int64{orphan.ID}, Disconnect: []int64{dropped.ID}},
			Profile: &models.AuthorProfileUpdateNested{Clear: true, Create: &models.AuthorProfileCreateInput{Bio: "new"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}}, Connect: []int64{freeTag.ID}, Disconnect: []int64{droppedTag.ID}},
		}, uwrSelect(&models.AuthorFieldOptions{
			ID: true, Name: true,
			Posts:   &models.PostRelationshipOptions{},
			Profile: &models.AuthorProfileFieldOptions{ID: true, Bio: true},
			Tags:    &models.TagRelationshipOptions{},
		}))
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, every relationship selected) error = %v, want nil", err)
		}
		if got == nil {
			t.Fatal("UpdateWithRelated(a, every relationship selected) = nil, want a")
		}
		if got.ID != a.ID || got.Name != "a2" {
			t.Errorf("returned author = {ID: %d, Name: %q}, want {%d, a2}", got.ID, got.Name, a.ID)
		}
		fresh := uwrOnlyNewID(t, posts, postParents(t, h))
		var postIDs []int64
		for _, p := range got.Posts {
			postIDs = append(postIDs, p.ID)
		}
		slices.Sort(postIDs)
		if diff := cmp.Diff(uwrSorted(kept.ID, orphan.ID, fresh), postIDs); diff != "" {
			t.Errorf("returned Posts (-want +got):\n%s", diff)
		}
		newProfile := uwrOnlyNewID(t, profiles, profileParents(t, h))
		if got.Profile == nil || got.Profile.ID != newProfile || got.Profile.ID == old.ID || got.Profile.Bio != "new" {
			t.Errorf("returned Profile = %+v, want the created profile %d", got.Profile, newProfile)
		}
		var tagIDs []int64
		for _, tag := range got.Tags {
			tagIDs = append(tagIDs, tag.ID)
		}
		slices.Sort(tagIDs)
		if diff := cmp.Diff(uwrSorted(keptTag.ID, freeTag.ID, uwrTagID(t, h, "new")), tagIDs); diff != "" {
			t.Errorf("returned Tags (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions performs every write and returns nil", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		orphan := createPost(t, h, "orphan", nil)
		got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
			Author:  models.UpdateAuthorInput{Name: omittable.Set("a2")},
			Posts:   &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
			Profile: &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "fresh"}},
			Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "new"}}},
		}, uwrSelect(&models.AuthorFieldOptions{}))
		if err != nil || got != nil {
			t.Fatalf("UpdateWithRelated(a, empty FieldOptions) = %+v, %v, want nil, nil", got, err)
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "a2"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[int64]*int64{orphan.ID: new(a.ID)}, postParents(t, h)); diff != "" {
			t.Errorf("post parents (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "author_profiles", 1)
		if diff := cmp.Diff(uwrPairs([2]int64{a.ID, uwrTagID(t, h, "new")}), authorTagPairs(t, h)); diff != "" {
			t.Errorf("author tags (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction, with or without a relationship selected", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			fo   *models.AuthorFieldOptions
		}{
			{"no relationship", nil},
			{"Posts selected", &models.AuthorFieldOptions{ID: true, Name: true, Posts: &models.PostRelationshipOptions{}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				orphan := createPost(t, h, "orphan", nil)
				got, err := h.Client.Authors().UpdateWithRelated(ctx, a.ID, &models.UpdateAuthorWithRelatedInput{
					Author: models.UpdateAuthorInput{Name: omittable.Set("a2")},
					Posts:  &models.AuthorPostsUpdateNested{Connect: []int64{orphan.ID}},
				}, uwrSelect(tt.fo), withLock[models.AuthorFieldOptions](sql.LockForUpdate))
				if err != nil {
					t.Fatalf("UpdateWithRelated(a, LockForUpdate, %s) error = %v, want nil", tt.name, err)
				}
				if got == nil || got.Name != "a2" {
					t.Errorf("UpdateWithRelated(a, LockForUpdate, %s) = %+v, want a named a2", tt.name, got)
				}
				if tt.fo != nil && (got == nil || len(got.Posts) != 1 || got.Posts[0].ID != orphan.ID) {
					t.Errorf("returned Posts = %+v, want the connected post", got)
				}
				if diff := cmp.Diff(map[int64]*int64{orphan.ID: new(a.ID)}, postParents(t, h)); diff != "" {
					t.Errorf("post parents (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("inside a transaction LockMode takes no row lock on the parent it reads back", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "mine", &a.ID)
		txCtx := beginBoundedTx(t, h)
		// No parent field and no verb, so nothing in the call writes the
		// author row; only the read that returns it could lock it.
		got, err := h.Client.Authors().UpdateWithRelated(txCtx, a.ID, &models.UpdateAuthorWithRelatedInput{},
			uwrSelect(&models.AuthorFieldOptions{ID: true, Posts: &models.PostRelationshipOptions{}}),
			withLock[models.AuthorFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("UpdateWithRelated(a, LockForUpdate, in a transaction) error = %v, want nil", err)
		}
		if got == nil || len(got.Posts) != 1 {
			t.Fatalf("UpdateWithRelated(a, LockForUpdate, in a transaction) = %+v, want a holding its post", got)
		}
		contender := otherTx(t, h)
		if _, err := h.Client.Authors().Get(contender, a.ID, withLock[models.AuthorFieldOptions](sql.LockForUpdateNoWait)); err != nil {
			t.Errorf("Get(a, LockForUpdateNoWait) from another transaction error = %v, want nil: the call locked the author", err)
		}
	})

	t.Run("nil input returns ErrNilInput, begins no transaction, fires no hook and writes nothing", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		begins := &beginCounter{Querier: h.Base}
		client, mutations, queries := uwrHookClient(begins)
		h.Querier.ResetCount()
		got, err := client.Authors().UpdateWithRelated(ctx, a.ID, nil)
		if !errors.Is(err, models.ErrNilInput) || got != nil {
			t.Errorf("UpdateWithRelated(a, nil) = %+v, %v, want nil, ErrNilInput", got, err)
		}
		if m, q := mutations(), queries(); len(m) != 0 || len(q) != 0 {
			t.Errorf("hooks fired for a nil input: mutations %q, queries %q", uwrOps(m), uwrQueryOps(q))
		}
		if n := begins.n.Load(); n != 0 {
			t.Errorf("a nil input began %d transactions, want 0", n)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("a nil input sent %q, want nothing", stmts)
		}
		if diff := cmp.Diff(map[int64]string{a.ID: "a"}, uwrAuthorNames(t, h)); diff != "" {
			t.Errorf("author names (-want +got):\n%s", diff)
		}
	})

	t.Run("a nil block or a block that sets no verb writes nothing on that edge", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			in   models.UpdateAuthorWithRelatedInput
		}{
			{"nil blocks", models.UpdateAuthorWithRelatedInput{}},
			{"blocks with no verb set", models.UpdateAuthorWithRelatedInput{
				Posts:   &models.AuthorPostsUpdateNested{Create: []*models.AuthorPostsCreateInput{}, Connect: []int64{}, Disconnect: []int64{}},
				Profile: &models.AuthorProfileUpdateNested{},
				Tags:    &models.AuthorTagsUpdateNested{Create: []*models.CreateTagInput{}, Connect: []int64{}, Disconnect: []int64{}},
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				a := createAuthor(t, h, "a")
				createPost(t, h, "mine", &a.ID)
				createProfile(t, h, "mine", &a.ID)
				tag := createTag(t, h, "mine")
				linkAuthorTag(t, h, a.ID, tag.ID)
				posts, profiles, pairs := postParents(t, h), profileParents(t, h), authorTagPairs(t, h)
				in := tt.in
				in.Author = models.UpdateAuthorInput{Name: omittable.Set("a2")}
				client, seen := mutationRecorder(h)
				if _, err := client.Authors().UpdateWithRelated(ctx, a.ID, &in); err != nil {
					t.Fatalf("UpdateWithRelated(a, %s) error = %v, want nil", tt.name, err)
				}
				if diff := cmp.Diff([]string{"update " + string(models.TableAuthors)}, uwrOps(seen())); diff != "" {
					t.Errorf("inner writes (-want +got):\n%s", diff)
				}
				uwrAssertEdgesUnchanged(t, h, posts, profiles, pairs)
				assertRowCount(t, h, "tags", 1)
				if diff := cmp.Diff(map[int64]string{a.ID: "a2"}, uwrAuthorNames(t, h)); diff != "" {
					t.Errorf("author names (-want +got):\n%s", diff)
				}
			})
		}
	})

	updateWithRelatedCompositeKey(t, h)
}

// uwrSelect returns a call option that sets FieldOptions.
func uwrSelect(fo *models.AuthorFieldOptions) func(*models.CallOptions[models.AuthorFieldOptions]) {
	return func(o *models.CallOptions[models.AuthorFieldOptions]) { o.FieldOptions = fo }
}

// uwrAuthorNames returns every author's name by id, soft-deleted authors
// included, read with a statement the counting querier does not record.
func uwrAuthorNames(t *testing.T, h *Harness) map[int64]string {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id, name FROM authors")
	if err != nil {
		t.Fatalf("read author names: %v", err)
	}
	defer rows.Close()
	got := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan author name: %v", err)
		}
		got[id] = name
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read author names: %v", err)
	}
	return got
}

// uwrDeletedAuthorIDs returns the keys of the soft-deleted authors.
func uwrDeletedAuthorIDs(t *testing.T, h *Harness) []int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id FROM authors WHERE deleted_at IS NOT NULL ORDER BY id")
	if err != nil {
		t.Fatalf("read soft-deleted authors: %v", err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan soft-deleted author: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read soft-deleted authors: %v", err)
	}
	return got
}

// uwrDeletedPost creates a post under authorID, or unparented when authorID
// is 0, and soft-deletes it.
func uwrDeletedPost(t *testing.T, h *Harness, title string, authorID int64) *models.Post {
	t.Helper()
	var parent *int64
	if authorID != 0 {
		parent = &authorID
	}
	p := createPost(t, h, title, parent)
	if _, err := h.Client.Posts().SoftDelete(context.Background(), p.ID); err != nil {
		t.Fatalf("SoftDelete post %q: %v", title, err)
	}
	return p
}

// uwrTagID returns the key of the tag with the given name.
func uwrTagID(t *testing.T, h *Harness, name string) int64 {
	t.Helper()
	tags, err := h.Client.Tags().GetMany(context.Background(), &models.GetTagsInput{
		Filter: &models.TagFilter{Name: &comparator.String{Eq: new(name)}},
	})
	if err != nil || len(tags) != 1 {
		t.Fatalf("read tag %q: %d rows, %v", name, len(tags), err)
	}
	return tags[0].ID
}

// uwrOnlyNewID returns the one key in after that is not in before.
func uwrOnlyNewID(t *testing.T, before, after map[int64]*int64) int64 {
	t.Helper()
	var added []int64
	for id := range after {
		if _, ok := before[id]; !ok {
			added = append(added, id)
		}
	}
	if len(added) != 1 {
		t.Fatalf("new rows = %v, want exactly one", added)
	}
	return added[0]
}

// uwrPairs sorts author-tag pairs the way authorTagPairs returns them.
func uwrPairs(pairs ...[2]int64) [][2]int64 {
	out := slices.Clone(pairs)
	slices.SortFunc(out, func(a, b [2]int64) int {
		return slices.Compare(a[:], b[:])
	})
	return out
}

// uwrSealRows returns a crate's crate_seals rows as seal id to junction id.
func uwrSealRows(t *testing.T, h *Harness, crateID int64) map[int64]int64 {
	t.Helper()
	rows, err := h.Client.CrateSeals().GetMany(context.Background(), &models.GetCrateSealsInput{
		Filter: &models.CrateSealFilter{CrateID: &comparator.Number[int64]{Eq: new(crateID)}},
	})
	if err != nil {
		t.Fatalf("GetMany crate seals error = %v, want nil", err)
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.SealID] = r.ID
	}
	return out
}

// uwrPairsOf returns the author-tag pairs of one author.
func uwrPairsOf(t *testing.T, h *Harness, authorID int64) [][2]int64 {
	t.Helper()
	var out [][2]int64
	for _, p := range authorTagPairs(t, h) {
		if p[0] == authorID {
			out = append(out, p)
		}
	}
	return out
}

func uwrSorted(ids ...int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// uwrAssertEdgesUnchanged checks that the posts' and profiles' parents and
// the author-tag pairs are as they were.
func uwrAssertEdgesUnchanged(t *testing.T, h *Harness, posts, profiles map[int64]*int64, pairs [][2]int64) {
	t.Helper()
	if diff := cmp.Diff(posts, postParents(t, h)); diff != "" {
		t.Errorf("post parents (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(profiles, profileParents(t, h)); diff != "" {
		t.Errorf("profile parents (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(pairs, authorTagPairs(t, h)); diff != "" {
		t.Errorf("author tags (-want +got):\n%s", diff)
	}
}

// uwrWantNested checks that err is a *NestedMutationError with the given
// edge, verb and id that matches sentinel.
func uwrWantNested(t *testing.T, err, sentinel error, edge, verb string, id any) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	nested, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("error = %v, want a *NestedMutationError", err)
	}
	if nested.Edge != edge || nested.Verb != verb || nested.ID != id {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q, ID: %v}, want {%q, %q, %v}", nested.Edge, nested.Verb, nested.ID, edge, verb, id)
	}
}

func uwrAssertConstraint(t *testing.T, err error, want database.ConstraintType) {
	t.Helper()
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v, want a *ConstraintError", err)
	}
	if ce.Type != want {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, want)
	}
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
	}
}

// uwrOps renders each mutation as "op table".
func uwrOps(seen []hook.MutationContext) []string {
	var out []string
	for _, m := range seen {
		out = append(out, string(m.Op)+" "+string(m.Table))
	}
	return out
}

// uwrQueryOps renders each query as "op table".
func uwrQueryOps(seen []hook.QueryContext) []string {
	var out []string
	for _, q := range seen {
		out = append(out, string(q.Op)+" "+string(q.Table))
	}
	return out
}

// uwrHookClient returns a client on base that records every mutation and
// every query its hooks see.
func uwrHookClient(base database.Querier) (*models.Client, func() []hook.MutationContext, func() []hook.QueryContext) {
	var (
		mu        sync.Mutex
		mutations []hook.MutationContext
		queries   []hook.QueryContext
	)
	mutation := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			mu.Lock()
			mutations = append(mutations, *m)
			mu.Unlock()
			return next(ctx, m)
		}
	}
	query := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			mu.Lock()
			queries = append(queries, *q)
			mu.Unlock()
			return next(ctx, q)
		}
	}
	client := models.New(base, models.WithMutationHook(mutation), models.WithQueryHook(query))
	return client, func() []hook.MutationContext {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(mutations)
		}, func() []hook.QueryContext {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(queries)
		}
}

// uwrAuthorDeleted reports whether a's DeletedAt is set.
func uwrAuthorDeleted(a *models.Author) bool {
	return !cmp.Equal(a.DeletedAt, models.Author{}.DeletedAt)
}

// uwrCallCounts counts the hooked inner calls by "op table". A posts
// UpdateWhere is told apart by whether it sets author_id or NULLs it.
func uwrCallCounts(mutations []hook.MutationContext, queries []hook.QueryContext) map[string]int {
	got := map[string]int{}
	for _, m := range mutations {
		key := string(m.Op) + " " + string(m.Table)
		if in, ok := m.Input.(*models.UpdatePostInput); ok {
			if v, _ := in.AuthorID.Get(); v == nil {
				key += " set author_id NULL"
			} else {
				key += " set author_id"
			}
		}
		got[key]++
	}
	for _, q := range queries {
		got[string(q.Op)+" "+string(q.Table)]++
	}
	return got
}
