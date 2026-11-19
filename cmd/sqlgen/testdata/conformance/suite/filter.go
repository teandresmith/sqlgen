package suite

import (
	"context"
	"slices"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// filterAuthors returns the names of the authors filter matches, sorted.
func filterAuthors(t *testing.T, h *Harness, filter *models.AuthorFilter) []string {
	t.Helper()
	got, err := h.Client.Authors().GetMany(context.Background(), &models.GetAuthorsInput{Filter: filter})
	if err != nil {
		t.Fatalf("GetMany authors error = %v, want nil", err)
	}
	names := make([]string, len(got))
	for i, a := range got {
		names[i] = a.Name
	}
	slices.Sort(names)
	return names
}

func filterKindPost(t *testing.T, h *Harness, title string, authorID int64, kind *string) {
	t.Helper()
	in := &models.CreatePostInput{Title: title, AuthorID: omittable.Set(&authorID), Kind: omittable.Set(kind)}
	if _, err := h.Client.Posts().Create(context.Background(), in); err != nil {
		t.Fatalf("Create post %q: %v", title, err)
	}
}

// getManyRelationshipFilters: a relationship filter member selects parents by
// their children.
func getManyRelationshipFilters(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("an O2M relationship filter returns exactly the parents with a matching child", func(t *testing.T) {
		h.Reset(t)
		ann, bob := createAuthor(t, h, "ann"), createAuthor(t, h, "bob")
		createAuthor(t, h, "cal")
		createPost(t, h, "alpha", &ann.ID)
		createPost(t, h, "gamma", &ann.ID)
		createPost(t, h, "gamma", &ann.ID)
		createPost(t, h, "beta", &bob.ID)
		// An unparented post matches no author.
		createPost(t, h, "beta", nil)
		for _, tc := range []struct {
			title string
			want  []string
		}{
			{"alpha", []string{"ann"}},
			{"beta", []string{"bob"}},
			// Two matching children still return their parent once.
			{"gamma", []string{"ann"}},
			{"delta", []string{}},
		} {
			got := filterAuthors(t, h, &models.AuthorFilter{Posts: &models.PostFilter{Title: &comparator.String{Eq: new(tc.title)}}})
			if diff := gocmp.Diff(tc.want, got); diff != "" {
				t.Errorf("authors with a %q post (-want +got):\n%s", tc.title, diff)
			}
		}
	})

	t.Run("an M2M relationship filter returns exactly the parents with a matching child", func(t *testing.T) {
		h.Reset(t)
		ann, bob := createAuthor(t, h, "ann"), createAuthor(t, h, "bob")
		createAuthor(t, h, "cal")
		golang, rust := createTag(t, h, "go"), createTag(t, h, "rust")
		createTag(t, h, "zig")
		linkAuthorTag(t, h, ann.ID, golang.ID)
		linkAuthorTag(t, h, ann.ID, rust.ID)
		linkAuthorTag(t, h, bob.ID, rust.ID)
		for _, tc := range []struct {
			tag  string
			want []string
		}{
			{"go", []string{"ann"}},
			{"rust", []string{"ann", "bob"}},
			// A tag no author holds matches no author.
			{"zig", []string{}},
		} {
			got := filterAuthors(t, h, &models.AuthorFilter{Tags: &models.TagFilter{Name: &comparator.String{Eq: new(tc.tag)}}})
			if diff := gocmp.Diff(tc.want, got); diff != "" {
				t.Errorf("authors tagged %q (-want +got):\n%s", tc.tag, diff)
			}
		}
		// The inverse edge: the tags of an author.
		tags, err := h.Client.Tags().GetMany(ctx, &models.GetTagsInput{
			Filter: &models.TagFilter{Authors: &models.AuthorFilter{Name: &comparator.String{Eq: new("ann")}}},
		})
		if err != nil {
			t.Fatalf("GetMany tags of ann error = %v, want nil", err)
		}
		names := make([]string, len(tags))
		for i, tag := range tags {
			names[i] = tag.Name
		}
		slices.Sort(names)
		if diff := gocmp.Diff([]string{"go", "rust"}, names); diff != "" {
			t.Errorf("tags of ann (-want +got):\n%s", diff)
		}
	})

	t.Run("a relationship filter composes with column filters and with and / or", func(t *testing.T) {
		h.Reset(t)
		ann, bob, cal := createAuthor(t, h, "ann"), createAuthor(t, h, "bob"), createAuthor(t, h, "cal")
		createAuthor(t, h, "dee")
		createPost(t, h, "alpha", &ann.ID)
		createPost(t, h, "alpha", &bob.ID)
		createPost(t, h, "beta", &cal.ID)
		rust := createTag(t, h, "rust")
		linkAuthorTag(t, h, cal.ID, rust.ID)
		alpha := &models.PostFilter{Title: &comparator.String{Eq: new("alpha")}}
		for _, tc := range []struct {
			name   string
			filter *models.AuthorFilter
			want   []string
		}{
			{"a column filter beside the member", &models.AuthorFilter{Name: &comparator.String{In: []string{"ann", "cal"}}, Posts: alpha}, []string{"ann"}},
			{"two members", &models.AuthorFilter{Posts: &models.PostFilter{Title: &comparator.String{Eq: new("beta")}}, Tags: &models.TagFilter{Name: &comparator.String{Eq: new("rust")}}}, []string{"cal"}},
			{"members inside or", &models.AuthorFilter{Or: []*models.AuthorFilter{{Posts: alpha}, {Tags: &models.TagFilter{}}}}, []string{"ann", "bob", "cal"}},
			{"a member and a column inside and", &models.AuthorFilter{And: []*models.AuthorFilter{{Posts: alpha}, {Name: &comparator.String{Neq: new("ann")}}}}, []string{"bob"}},
		} {
			if diff := gocmp.Diff(tc.want, filterAuthors(t, h, tc.filter)); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tc.name, diff)
			}
		}
	})

	t.Run("a discriminator relationship filter matches only the children carrying its value", func(t *testing.T) {
		h.Reset(t)
		ann, bob, cal := createAuthor(t, h, "ann"), createAuthor(t, h, "bob"), createAuthor(t, h, "cal")
		filterKindPost(t, h, "one", ann.ID, new("draft"))
		filterKindPost(t, h, "two", bob.ID, nil)
		filterKindPost(t, h, "three", cal.ID, new("published"))
		filterKindPost(t, h, "four", cal.ID, new("draft"))
		for _, tc := range []struct {
			name   string
			filter *models.AuthorFilter
			want   []string
		}{
			{"any draft", &models.AuthorFilter{Drafts: &models.PostFilter{}}, []string{"ann", "cal"}},
			{"a draft by title", &models.AuthorFilter{Drafts: &models.PostFilter{Title: &comparator.String{Eq: new("one")}}}, []string{"ann"}},
			// bob's post is not a draft, so the Drafts edge does not reach it.
			{"a non-draft title", &models.AuthorFilter{Drafts: &models.PostFilter{Title: &comparator.String{Eq: new("two")}}}, []string{}},
			{"the same title on the edge without a discriminator", &models.AuthorFilter{Posts: &models.PostFilter{Title: &comparator.String{Eq: new("two")}}}, []string{"bob"}},
		} {
			if diff := gocmp.Diff(tc.want, filterAuthors(t, h, tc.filter)); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tc.name, diff)
			}
		}
	})
}

// getManyComparators: each set operator of the comparators matrix selects, on
// the database, the rows its semantics state.
func getManyComparators(t *testing.T, h *Harness) {
	ctx := context.Background()
	// Whole seconds in UTC, which every dialect's timestamp column stores and
	// reads back exactly.
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Hour) }
	seed := func(t *testing.T) {
		t.Helper()
		h.Reset(t)
		for i, name := range []string{"a", "b", "c", "d"} {
			in := &models.CreateProductInput{Name: name, Price: int64(i+1) * 100, CreatedAt: omittable.Set(models.TimestampAt(at(i)))}
			if _, err := h.Client.Products().Create(ctx, in); err != nil {
				t.Fatalf("Create product %q: %v", name, err)
			}
		}
	}
	products := func(t *testing.T, filter *models.ProductFilter) []string {
		t.Helper()
		got, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{Filter: filter})
		if err != nil {
			t.Fatalf("GetMany products error = %v, want nil", err)
		}
		names := getNames(got)
		slices.Sort(names)
		return names
	}
	labels := func(t *testing.T, filter *models.LabelFilter) []string {
		t.Helper()
		h.Reset(t)
		for _, name := range []string{"x", "y", "z"} {
			createLabel(t, h, name)
		}
		got, err := h.Client.Labels().GetMany(ctx, &models.GetLabelsInput{Filter: filter})
		if err != nil {
			t.Fatalf("GetMany labels error = %v, want nil", err)
		}
		names := make([]string, len(got))
		for i, l := range got {
			names[i] = l.Name
		}
		slices.Sort(names)
		return names
	}
	for _, tc := range []struct {
		name string
		run  func(t *testing.T) []string
		want []string
	}{
		{"Number.In selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{Price: &comparator.Number[int64]{In: []int64{200, 400, 999}}})
		}, []string{"b", "d"}},
		{"Number.Nin selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{Price: &comparator.Number[int64]{Nin: []int64{200, 400, 999}}})
		}, []string{"a", "c"}},
		{"String.In selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{Name: &comparator.String{In: []string{"a", "c", "zz"}}})
		}, []string{"a", "c"}},
		{"String.Nin selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{Name: &comparator.String{Nin: []string{"a", "c", "zz"}}})
		}, []string{"b", "d"}},
		{"Time.In selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{CreatedAt: &comparator.Time{In: []time.Time{at(1), at(3), at(9)}}})
		}, []string{"b", "d"}},
		{"Time.Nin selects the rows its semantics state", func(t *testing.T) []string {
			seed(t)
			return products(t, &models.ProductFilter{CreatedAt: &comparator.Time{Nin: []time.Time{at(1), at(3), at(9)}}})
		}, []string{"a", "c"}},
		{"ID.In selects the rows its semantics state", func(t *testing.T) []string {
			return labels(t, &models.LabelFilter{Name: &comparator.ID{In: []string{"x", "z", "w"}}})
		}, []string{"x", "z"}},
		{"ID.Nin selects the rows its semantics state", func(t *testing.T) []string {
			return labels(t, &models.LabelFilter{Name: &comparator.ID{Nin: []string{"x", "z", "w"}}})
		}, []string{"y"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if diff := gocmp.Diff(tc.want, tc.run(t)); diff != "" {
				t.Errorf("selected (-want +got):\n%s", diff)
			}
		})
	}
}
