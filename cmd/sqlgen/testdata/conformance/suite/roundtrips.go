package suite

import (
	"context"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
)

// RunRoundTrips measures the database round trips of every client method on
// the shapes docs/round-trips.md reports, and checks that report. h.Client
// and h.Base must sit on a pool whose driver reports every statement to rec;
// h.Reset empties the tables. It is not part of Run: a round-trip count is a
// measurement the report publishes, not a PRD requirement.
func RunRoundTrips(t *testing.T, h *Harness, rec *roundtrip.Recorder) {
	roundtrip.Run(t, roundtrip.Config{
		Suite:    roundtrip.SuiteCore,
		Dialect:  h.Dialect,
		Driver:   h.Driver,
		Recorder: rec,
		Reset:    h.Reset,
	}, roundTripCases(h))
}

// op is an operation a round-trip case measures.
type op = func(ctx context.Context) error

// Report groups and growth notes.
const (
	groupReads  = "Reads"
	groupPages  = "Pagination and streaming"
	groupWrites = "Writes"
	groupBatch  = "Batch writes"
	groupSoft   = "Soft delete and restore"
	groupHard   = "Hard delete"
	groupNested = "Nested writes"

	growsEdge     = "+1 statement per O2M and +2 per M2M relationship selected (junction, then targets); relationships load in parallel"
	growsEdgeMany = growsEdge + "; not with the number of parents"
	growsDepth    = "+1 round per nesting level"
)

// batchRows is the large batch size the report measures; tables at the
// default batch size write it in one chunk, products (batch_size 2) in 25.
const batchRows = 50

func roundTripCases(h *Harness) []roundtrip.Case {
	c := func(group, method, shape, grows string, setup func(t *testing.T) op) roundtrip.Case {
		return roundtrip.Case{Group: group, Op: method, Shape: shape, Grows: grows, Setup: setup}
	}
	authors := func(t *testing.T) []int64 { return rtSeedAuthors(t, h, 10) }
	return []roundtrip.Case{
		// Reads.
		c(groupReads, "Get", "plain table", "", func(t *testing.T) op {
			p := createProduct(t, h, "p")
			return func(ctx context.Context) error { _, err := h.Client.Products().Get(ctx, p.ID); return err }
		}),
		c(groupReads, "Get", "1 O2M (Posts)", growsEdge, rtGetAuthor(h, rtPosts)),
		c(groupReads, "Get", "2 O2M (Posts, Drafts)", growsEdge, rtGetAuthor(h, rtPostsDrafts)),
		c(groupReads, "Get", "M2M (Tags)", growsEdge, rtGetAuthor(h, rtTags)),
		c(groupReads, "Get", "O2O has-one (Profile)", "", rtGetAuthor(h, rtProfile)),
		c(groupReads, "Get", "O2O belongs-to (Post.Author)", "", func(t *testing.T) op {
			a := rtSeedAuthors(t, h, 1)[0]
			post := createPost(t, h, "solo", new(a))
			return func(ctx context.Context) error {
				_, err := h.Client.Posts().Get(ctx, post.ID, func(o *models.CallOptions[models.PostFieldOptions]) {
					o.FieldOptions = &models.PostFieldOptions{ID: true, Title: true, Author: &models.AuthorFieldOptions{ID: true, Name: true}}
				})
				return err
			}
		}),
		c(groupReads, "Get", "2 O2M, M2M and O2O", growsEdge, rtGetAuthor(h, rtEveryEdge)),
		c(groupReads, "Get", "nested depth 2 (Product.OrderItems.ItemNotes)", growsDepth, func(t *testing.T) op {
			ids := rtSeedProducts(t, h, 1)
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Get(ctx, ids[0], rtItemNotes)
				return err
			}
		}),
		c(groupReads, "Get", "2 O2M, inside the caller's transaction", "+1 round per relationship: a transaction runs its statements one at a time", func(t *testing.T) op {
			a := rtSeedAuthors(t, h, 1)[0]
			return func(ctx context.Context) error {
				return database.WithTransaction(ctx, h.Base, "roundtrip", func(txCtx context.Context) error {
					_, err := h.Client.Authors().Get(txCtx, a, rtPostsDrafts)
					return err
				})
			}
		}),
		c(groupReads, "GetMany", "plain table, 10 rows", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			return func(ctx context.Context) error { _, err := h.Client.Products().GetMany(ctx, nil); return err }
		}),
		c(groupReads, "GetMany", "10 parents, 1 O2M (Posts)", growsEdgeMany, rtGetAuthors(h, authors, rtPosts)),
		c(groupReads, "GetMany", "10 parents, 2 O2M (Posts, Drafts)", growsEdgeMany, rtGetAuthors(h, authors, rtPostsDrafts)),
		c(groupReads, "GetMany", "10 parents, M2M (Tags)", growsEdgeMany, rtGetAuthors(h, authors, rtTags)),
		c(groupReads, "GetMany", "10 parents, O2O has-one (Profile)", "", rtGetAuthors(h, authors, rtProfile)),
		c(groupReads, "GetMany", "10 parents, nested depth 2", growsDepth, func(t *testing.T) op {
			rtSeedProducts(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Products().GetMany(ctx, nil, rtItemNotes)
				return err
			}
		}),
		c(groupReads, "Count", "plain table", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			return func(ctx context.Context) error { _, err := h.Client.Products().Count(ctx, nil); return err }
		}),
		c(groupReads, "Count", "filter through an O2M (Posts)", "", func(t *testing.T) op {
			rtSeedAuthors(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Authors().Count(ctx, &models.AuthorFilter{Posts: &models.PostFilter{Title: &comparator.String{Eq: new("post-0")}}})
				return err
			}
		}),
		c(groupReads, "Exists", "plain table", "", func(t *testing.T) op {
			p := createProduct(t, h, "p")
			return func(ctx context.Context) error { _, err := h.Client.Products().Exists(ctx, p.ID); return err }
		}),
		c(groupReads, "ExistsWhere", "plain table", "", func(t *testing.T) op {
			createProduct(t, h, "p")
			return func(ctx context.Context) error {
				_, err := h.Client.Products().ExistsWhere(ctx, &models.ProductFilter{Name: &comparator.String{Eq: new("p")}})
				return err
			}
		}),

		// Pagination and streaming.
		c(groupPages, "Paginate", "page of 5", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{Limit: 5})
				return err
			}
		}),
		c(groupPages, "Paginate", "page of 5, 1 O2M (Posts)", growsEdge, func(t *testing.T) op {
			rtSeedAuthors(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Authors().Paginate(ctx, models.PaginateInput[models.AuthorFilter]{Limit: 5}, rtPosts)
				return err
			}
		}),
		c(groupPages, "Connection", "first page of 5", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(5)})
				return err
			}
		}),
		c(groupPages, "Connection", "page after a cursor", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			first, err := h.Client.Products().Connection(context.Background(), models.ConnectionInput[models.ProductFilter]{First: new(5)})
			if err != nil {
				t.Fatalf("Connection(first 5) error = %v", err)
			}
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: new(5), After: first.PageInfo.EndCursor})
				return err
			}
		}),
		c(groupPages, "Connection", "first page of 5, 1 O2M (Posts)", growsEdge, func(t *testing.T) op {
			rtSeedAuthors(t, h, 10)
			return func(ctx context.Context) error {
				_, err := h.Client.Authors().Connection(ctx, models.ConnectionInput[models.AuthorFilter]{First: new(5)}, rtPosts)
				return err
			}
		}),
		c(groupPages, "Stream", "plain table, 10 rows", "", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, 10)
			return func(ctx context.Context) error {
				for _, err := range h.Client.Products().Stream(ctx, nil) {
					if err != nil {
						return err
					}
				}
				return nil
			}
		}),

		// Single-row writes.
		c(groupWrites, "Create", "plain table", "", func(t *testing.T) op {
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1})
				return err
			}
		}),
		c(groupWrites, "Create", "empty field selection (no read-back)", "", func(t *testing.T) op {
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1}, func(o *models.CallOptions[models.ProductFieldOptions]) {
					o.FieldOptions = &models.ProductFieldOptions{}
				})
				return err
			}
		}),
		c(groupWrites, "Create", "selecting 1 O2M (Posts)", growsEdge, func(t *testing.T) op {
			return func(ctx context.Context) error {
				_, err := h.Client.Authors().Create(ctx, &models.CreateAuthorInput{Name: "a"}, rtPosts)
				return err
			}
		}),
		c(groupWrites, "Update", "plain table", "", func(t *testing.T) op {
			p := createProduct(t, h, "p")
			return func(ctx context.Context) error {
				_, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Price: omittable.Set[int64](2)})
				return err
			}
		}),
		c(groupWrites, "UpdateWhere", "1 matching row", "", rtUpdateWhere(h, 1)),
		c(groupWrites, "UpdateWhere", "50 matching rows", "", rtUpdateWhere(h, batchRows)),
		c(groupWrites, "UpdateWhere", "50 matching rows, batch_size 2", "+1 read-back per batch_size chunk of matched rows", func(t *testing.T) op {
			rtSeedPlainProducts(t, h, batchRows)
			return func(ctx context.Context) error {
				_, err := h.Client.Products().UpdateWhere(ctx, &models.ProductFilter{Name: &comparator.String{StartsWith: new("p-")}}, &models.UpdateProductInput{Price: omittable.Set[int64](2)})
				return err
			}
		}),
		c(groupWrites, "Upsert", "insert (no conflict)", "", func(t *testing.T) op {
			return func(ctx context.Context) error {
				_, err := h.Client.Counters().Upsert(ctx, rtCounter("c"), models.CounterConflictName)
				return err
			}
		}),
		c(groupWrites, "Upsert", "update (conflict)", "", func(t *testing.T) op {
			rtSeedCounters(t, h, 1)
			return func(ctx context.Context) error {
				_, err := h.Client.Counters().Upsert(ctx, rtCounter("c-0"), models.CounterConflictName)
				return err
			}
		}),
		c(groupWrites, "Increment", "plain table", "", func(t *testing.T) op {
			id := rtSeedCounters(t, h, 1)[0]
			return func(ctx context.Context) error {
				return h.Client.Counters().Increment(ctx, id, models.IncrementInput[models.CounterIncrementColumn]{Column: models.CounterIncrementHits, Amount: 1})
			}
		}),

		// Batch writes.
		c(groupBatch, "CreateMany", "1 row", "", rtCreateTags(h, 1)),
		c(groupBatch, "CreateMany", "50 rows", "", rtCreateTags(h, batchRows)),
		c(groupBatch, "CreateMany", "50 rows, batch_size 2", "+2 statements per batch_size chunk (insert, read-back); +3 on MySQL (an auto_increment_increment read)", func(t *testing.T) op {
			inputs := make([]*models.CreateProductInput, batchRows)
			for i := range inputs {
				inputs[i] = &models.CreateProductInput{Name: fmt.Sprintf("p-%d", i), Price: 1}
			}
			return func(ctx context.Context) error { _, err := h.Client.Products().CreateMany(ctx, inputs); return err }
		}),
		c(groupBatch, "UpdateMany", "1 row", "", rtUpdateTags(h, 1)),
		c(groupBatch, "UpdateMany", "50 rows", "+1 statement per row; pgx sends a chunk's updates as one batch", rtUpdateTags(h, batchRows)),
		c(groupBatch, "UpdateMany", "50 rows, batch_size 2", "+1 statement per row and +1 read-back per batch_size chunk; pgx sends a chunk's updates as one batch", func(t *testing.T) op {
			ids := rtSeedPlainProducts(t, h, batchRows)
			items := make([]models.UpdateProductItem, len(ids))
			for i, id := range ids {
				items[i] = models.UpdateProductItem{ID: id, Input: &models.UpdateProductInput{Price: omittable.Set[int64](2)}}
			}
			return func(ctx context.Context) error { _, err := h.Client.Products().UpdateMany(ctx, items); return err }
		}),
		c(groupBatch, "UpsertMany", "1 row, insert", "", rtUpsertCounters(h, 1, false)),
		c(groupBatch, "UpsertMany", "50 rows, insert", "", rtUpsertCounters(h, batchRows, false)),
		c(groupBatch, "UpsertMany", "50 rows, update (conflict)", "", rtUpsertCounters(h, batchRows, true)),
		c(groupBatch, "UpsertMany", "50 rows, batch_size 2", "+3 statements per batch_size chunk (write, key resolution, read-back)", func(t *testing.T) op {
			inputs := make([]*models.CreateAuthorInput, batchRows)
			for i := range inputs {
				inputs[i] = &models.CreateAuthorInput{Name: fmt.Sprintf("a-%d", i)}
			}
			return func(ctx context.Context) error {
				_, err := h.Client.Authors().UpsertMany(ctx, inputs, models.AuthorConflictName)
				return err
			}
		}),

		// Soft delete and restore.
		c(groupSoft, "SoftDelete", "one row", "", func(t *testing.T) op {
			names := rtSeedLabels(t, h, 1, false)
			return func(ctx context.Context) error { _, err := h.Client.Labels().SoftDelete(ctx, names[0]); return err }
		}),
		c(groupSoft, "SoftDeleteMany", "1 key", "", rtSoftDeleteLabels(h, 1)),
		c(groupSoft, "SoftDeleteMany", "50 keys", "", rtSoftDeleteLabels(h, batchRows)),
		c(groupSoft, "SoftDeleteMany", "50 keys, batch_size 2", "+2 statements per batch_size chunk (update, read-back); +3 on MySQL (a pre-read)", func(t *testing.T) op {
			ids := createDocuments(t, h, batchRows)
			return func(ctx context.Context) error { _, err := h.Client.Documents().SoftDeleteMany(ctx, ids); return err }
		}),
		c(groupSoft, "SoftDeleteWhere", "50 matching rows", "", func(t *testing.T) op {
			rtSeedLabels(t, h, batchRows, false)
			return func(ctx context.Context) error {
				_, err := h.Client.Labels().SoftDeleteWhere(ctx, &models.LabelFilter{Name: &comparator.ID{Gte: new("l-")}})
				return err
			}
		}),
		c(groupSoft, "Restore", "one row", "", func(t *testing.T) op {
			names := rtSeedLabels(t, h, 1, true)
			return func(ctx context.Context) error { _, err := h.Client.Labels().Restore(ctx, names[0]); return err }
		}),
		c(groupSoft, "RestoreMany", "1 key", "", rtRestoreLabels(h, 1)),
		c(groupSoft, "RestoreMany", "50 keys", "", rtRestoreLabels(h, batchRows)),
		c(groupSoft, "RestoreWhere", "50 matching rows", "", func(t *testing.T) op {
			rtSeedLabels(t, h, batchRows, true)
			return func(ctx context.Context) error {
				_, err := h.Client.Labels().RestoreWhere(ctx, &models.LabelFilter{Name: &comparator.ID{Gte: new("l-")}})
				return err
			}
		}),

		// Hard delete.
		c(groupHard, "HardDelete", "one row", "", func(t *testing.T) op {
			id := createTag(t, h, "t").ID
			return func(ctx context.Context) error { return h.Client.Tags().HardDelete(ctx, id) }
		}),
		c(groupHard, "HardDeleteMany", "1 key", "", rtHardDeleteTags(h, 1)),
		c(groupHard, "HardDeleteMany", "50 keys", "", rtHardDeleteTags(h, batchRows)),
		c(groupHard, "HardDeleteMany", "50 keys, batch_size 2", "+1 statement per batch_size chunk; +2 on MySQL (a pre-read)", func(t *testing.T) op {
			ids := rtSeedPlainProducts(t, h, batchRows)
			return func(ctx context.Context) error { return h.Client.Products().HardDeleteMany(ctx, ids) }
		}),
		c(groupHard, "HardDeleteWhere", "50 matching rows", "", func(t *testing.T) op {
			rtSeedTags(t, h, batchRows)
			return func(ctx context.Context) error {
				return h.Client.Tags().HardDeleteWhere(ctx, &models.TagFilter{Name: &comparator.String{StartsWith: new("t-")}})
			}
		}),

		// Nested writes.
		c(groupNested, "CreateWithRelated", "parent only", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {})),
		c(groupNested, "CreateWithRelated", "O2M create 1 row", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsCreateNested{Create: rtPostInputs(1)}
		})),
		c(groupNested, "CreateWithRelated", "O2M create 5 rows, child batch_size 2", "+1 statement per child batch_size chunk; MySQL adds an auto_increment_increment read per chunk of two or more rows", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsCreateNested{Create: rtPostInputs(5)}
		})),
		c(groupNested, "CreateWithRelated", "O2M connect 2 rows", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsCreateNested{Connect: []int64{createPost(t, h, "x", nil).ID, createPost(t, h, "y", nil).ID}}
		})),
		c(groupNested, "CreateWithRelated", "M2M create 2 rows", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Tags = &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "x"}, {Name: "y"}}}
		})),
		c(groupNested, "CreateWithRelated", "M2M connect 2 rows", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Tags = &models.AuthorTagsCreateNested{Connect: []int64{createTag(t, h, "x").ID, createTag(t, h, "y").ID}}
		})),
		c(groupNested, "CreateWithRelated", "O2O create", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Profile = &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}}
		})),
		c(groupNested, "CreateWithRelated", "O2O connect", "", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Profile = &models.AuthorProfileCreateNested{Connect: new(createProfile(t, h, "bio", nil).ID)}
		})),
		c(groupNested, "CreateWithRelated", "O2M, M2M and O2O create", "each edge written adds its own statements, in sequence", rtCreateWithRelated(h, func(t *testing.T, in *models.CreateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsCreateNested{Create: rtPostInputs(1)}
			in.Tags = &models.AuthorTagsCreateNested{Create: []*models.CreateTagInput{{Name: "x"}}}
			in.Profile = &models.AuthorProfileCreateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}}
		})),
		c(groupNested, "UpdateWithRelated", "parent only", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {})),
		c(groupNested, "UpdateWithRelated", "O2M create 1 row", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsUpdateNested{Create: rtPostInputs(1)}
		})),
		c(groupNested, "UpdateWithRelated", "O2M connect 2 rows", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsUpdateNested{Connect: []int64{createPost(t, h, "x", nil).ID, createPost(t, h, "y", nil).ID}}
		})),
		c(groupNested, "UpdateWithRelated", "O2M disconnect 2 rows", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			in.Posts = &models.AuthorPostsUpdateNested{Disconnect: []int64{createPost(t, h, "x", new(a)).ID, createPost(t, h, "y", new(a)).ID}}
		})),
		c(groupNested, "UpdateWithRelated", "O2M clear", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			createPost(t, h, "x", new(a))
			in.Posts = &models.AuthorPostsUpdateNested{Clear: true}
		})),
		c(groupNested, "UpdateWithRelated", "M2M connect 2 rows", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			in.Tags = &models.AuthorTagsUpdateNested{Connect: []int64{createTag(t, h, "x").ID, createTag(t, h, "y").ID}}
		})),
		c(groupNested, "UpdateWithRelated", "M2M disconnect 2 rows", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			x, y := createTag(t, h, "x").ID, createTag(t, h, "y").ID
			linkAuthorTag(t, h, a, x)
			linkAuthorTag(t, h, a, y)
			in.Tags = &models.AuthorTagsUpdateNested{Disconnect: []int64{x, y}}
		})),
		c(groupNested, "UpdateWithRelated", "O2O create", "", rtUpdateWithRelated(h, func(t *testing.T, a int64, in *models.UpdateAuthorWithRelatedInput) {
			in.Profile = &models.AuthorProfileUpdateNested{Create: &models.AuthorProfileCreateInput{Bio: "bio"}}
		})),
		c(groupNested, "UpsertWithRelated", "insert branch, O2M create 1 row", "", rtUpsertWithRelated(h, false)),
		c(groupNested, "UpsertWithRelated", "update branch, O2M create 1 row", "", rtUpsertWithRelated(h, true)),
	}
}

// Field selections the read cases use.
func rtPosts(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Posts: &models.PostRelationshipOptions{}}
}

func rtPostsDrafts(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Posts: &models.PostRelationshipOptions{}, Drafts: &models.PostRelationshipOptions{}}
}

func rtTags(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Tags: &models.TagRelationshipOptions{}}
}

func rtProfile(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Profile: &models.AuthorProfileFieldOptions{ID: true, Bio: true}}
}

func rtEveryEdge(o *models.CallOptions[models.AuthorFieldOptions]) {
	o.FieldOptions = &models.AuthorFieldOptions{
		ID: true, Name: true,
		Posts: &models.PostRelationshipOptions{}, Drafts: &models.PostRelationshipOptions{},
		Tags: &models.TagRelationshipOptions{}, Profile: &models.AuthorProfileFieldOptions{ID: true, Bio: true},
	}
}

func rtItemNotes(o *models.CallOptions[models.ProductFieldOptions]) {
	o.FieldOptions = &models.ProductFieldOptions{ID: true, Name: true, OrderItems: &models.OrderItemRelationshipOptions{
		FieldOptions: &models.OrderItemFieldOptions{OrderID: true, ProductID: true, ItemNotes: &models.ItemNoteRelationshipOptions{}},
	}}
}

func rtGetAuthor(h *Harness, sel func(*models.CallOptions[models.AuthorFieldOptions])) func(t *testing.T) op {
	return func(t *testing.T) op {
		a := rtSeedAuthors(t, h, 1)[0]
		return func(ctx context.Context) error { _, err := h.Client.Authors().Get(ctx, a, sel); return err }
	}
}

func rtGetAuthors(h *Harness, seed func(t *testing.T) []int64, sel func(*models.CallOptions[models.AuthorFieldOptions])) func(t *testing.T) op {
	return func(t *testing.T) op {
		seed(t)
		return func(ctx context.Context) error { _, err := h.Client.Authors().GetMany(ctx, nil, sel); return err }
	}
}

// rtSeedAuthors creates n authors, each with two posts, two drafts, two tags
// and a profile, and returns their keys.
func rtSeedAuthors(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		a := createAuthor(t, h, fmt.Sprintf("author-%d", i))
		ids[i] = a.ID
		for j := range 2 {
			createPost(t, h, fmt.Sprintf("post-%d", j), new(a.ID))
			draft := &models.CreatePostInput{Title: fmt.Sprintf("draft-%d", j), AuthorID: omittable.Set(new(a.ID)), Kind: omittable.Set(new("draft"))}
			if _, err := h.Client.Posts().Create(context.Background(), draft); err != nil {
				t.Fatalf("Create draft: %v", err)
			}
			linkAuthorTag(t, h, a.ID, createTag(t, h, fmt.Sprintf("tag-%d-%d", i, j)).ID)
		}
		createProfile(t, h, "bio", new(a.ID))
	}
	return ids
}

// rtSeedProducts creates n products, each with two order items that each
// have two notes, and returns their keys.
func rtSeedProducts(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		p := createPricedProduct(t, h, fmt.Sprintf("p-%02d", i), int64(100+i))
		ids[i] = p.ID
		for order := range int64(2) {
			pk := models.OrderItemPK{OrderID: order + 1, ProductID: p.ID}
			createOrderItem(t, h, pk)
			for k := range 2 {
				in := &models.CreateItemNoteInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Body: fmt.Sprintf("note-%d", k)}
				if _, err := h.Client.ItemNotes().Create(context.Background(), in); err != nil {
					t.Fatalf("Create item note: %v", err)
				}
			}
		}
	}
	return ids
}

// rtSeedPlainProducts creates n products with nothing under them and returns
// their keys.
func rtSeedPlainProducts(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = createPricedProduct(t, h, fmt.Sprintf("p-%02d", i), int64(100+i)).ID
	}
	return ids
}

func rtSeedTags(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = createTag(t, h, fmt.Sprintf("t-%02d", i)).ID
	}
	return ids
}

func rtCounter(name string) *models.CreateCounterInput {
	return &models.CreateCounterInput{Name: name, Hits: omittable.Set[int64](1), UpdatedAt: omittable.Set(models.Now())}
}

func rtSeedCounters(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		c, err := h.Client.Counters().Create(context.Background(), rtCounter(fmt.Sprintf("c-%d", i)))
		if err != nil {
			t.Fatalf("Create counter: %v", err)
		}
		ids[i] = c.ID
	}
	return ids
}

// rtSeedLabels creates n labels, soft-deleted when deleted is set, and
// returns their keys.
func rtSeedLabels(t *testing.T, h *Harness, n int, deleted bool) []string {
	t.Helper()
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("l-%02d", i)
		in := &models.CreateLabelInput{Name: names[i]}
		if deleted {
			in.DeletedAt = omittable.Set(models.DeletedAt(models.Now()))
		}
		if _, err := h.Client.Labels().Create(context.Background(), in); err != nil {
			t.Fatalf("Create label: %v", err)
		}
	}
	return names
}

// rtUpdateWhere updates n matching counters, which write at the default
// batch size.
func rtUpdateWhere(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		rtSeedCounters(t, h, n)
		return func(ctx context.Context) error {
			_, err := h.Client.Counters().UpdateWhere(ctx, &models.CounterFilter{Name: &comparator.String{StartsWith: new("c-")}}, &models.UpdateCounterInput{Hits: omittable.Set[int64](2)})
			return err
		}
	}
}

func rtCreateTags(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		inputs := make([]*models.CreateTagInput, n)
		for i := range inputs {
			inputs[i] = &models.CreateTagInput{Name: fmt.Sprintf("t-%02d", i)}
		}
		return func(ctx context.Context) error { _, err := h.Client.Tags().CreateMany(ctx, inputs); return err }
	}
}

func rtUpdateTags(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		ids := rtSeedTags(t, h, n)
		items := make([]models.UpdateTagItem, n)
		for i, id := range ids {
			items[i] = models.UpdateTagItem{ID: id, Input: &models.UpdateTagInput{Name: omittable.Set(fmt.Sprintf("u-%02d", i))}}
		}
		return func(ctx context.Context) error { _, err := h.Client.Tags().UpdateMany(ctx, items); return err }
	}
}

func rtUpsertCounters(h *Harness, n int, conflict bool) func(t *testing.T) op {
	return func(t *testing.T) op {
		if conflict {
			rtSeedCounters(t, h, n)
		}
		inputs := make([]*models.CreateCounterInput, n)
		for i := range inputs {
			inputs[i] = rtCounter(fmt.Sprintf("c-%d", i))
		}
		return func(ctx context.Context) error {
			_, err := h.Client.Counters().UpsertMany(ctx, inputs, models.CounterConflictName)
			return err
		}
	}
}

func rtSoftDeleteLabels(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		names := rtSeedLabels(t, h, n, false)
		return func(ctx context.Context) error { _, err := h.Client.Labels().SoftDeleteMany(ctx, names); return err }
	}
}

func rtRestoreLabels(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		names := rtSeedLabels(t, h, n, true)
		return func(ctx context.Context) error { _, err := h.Client.Labels().RestoreMany(ctx, names); return err }
	}
}

func rtHardDeleteTags(h *Harness, n int) func(t *testing.T) op {
	return func(t *testing.T) op {
		ids := rtSeedTags(t, h, n)
		return func(ctx context.Context) error { return h.Client.Tags().HardDeleteMany(ctx, ids) }
	}
}

func rtPostInputs(n int) []*models.AuthorPostsCreateInput {
	in := make([]*models.AuthorPostsCreateInput, n)
	for i := range in {
		in[i] = &models.AuthorPostsCreateInput{Title: fmt.Sprintf("nested-%d", i)}
	}
	return in
}

func rtCreateWithRelated(h *Harness, edges func(t *testing.T, in *models.CreateAuthorWithRelatedInput)) func(t *testing.T) op {
	return func(t *testing.T) op {
		in := &models.CreateAuthorWithRelatedInput{Author: models.CreateAuthorInput{Name: "nested"}}
		edges(t, in)
		return func(ctx context.Context) error { _, err := h.Client.Authors().CreateWithRelated(ctx, in); return err }
	}
}

func rtUpdateWithRelated(h *Harness, edges func(t *testing.T, author int64, in *models.UpdateAuthorWithRelatedInput)) func(t *testing.T) op {
	return func(t *testing.T) op {
		a := createAuthor(t, h, "nested").ID
		in := &models.UpdateAuthorWithRelatedInput{Author: models.UpdateAuthorInput{Name: omittable.Set("renamed")}}
		edges(t, a, in)
		return func(ctx context.Context) error {
			_, err := h.Client.Authors().UpdateWithRelated(ctx, a, in)
			return err
		}
	}
}

// rtUpsertWithRelated upserts an author with one new post, onto an existing
// author when conflict is set.
func rtUpsertWithRelated(h *Harness, conflict bool) func(t *testing.T) op {
	return func(t *testing.T) op {
		if conflict {
			createAuthor(t, h, "nested")
		}
		in := &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "nested"},
			Posts:  &models.AuthorPostsUpdateNested{Create: rtPostInputs(1)},
		}
		return func(ctx context.Context) error {
			_, err := h.Client.Authors().UpsertWithRelated(ctx, in, models.AuthorConflictName)
			return err
		}
	}
}
