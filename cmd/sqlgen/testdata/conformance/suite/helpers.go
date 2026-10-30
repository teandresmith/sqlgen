package suite

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

func createProduct(t *testing.T, h *Harness, name string) *models.Product {
	t.Helper()
	p, err := h.Client.Products().Create(context.Background(), &models.CreateProductInput{Name: name, Price: 100})
	if err != nil {
		t.Fatalf("Create product %q: %v", name, err)
	}
	return p
}

func createOrderItem(t *testing.T, h *Harness, pk models.OrderItemPK) {
	t.Helper()
	in := &models.CreateOrderItemInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Quantity: 1}
	if _, err := h.Client.OrderItems().Create(context.Background(), in); err != nil {
		t.Fatalf("Create order item %+v: %v", pk, err)
	}
}

func createDocument(t *testing.T, h *Harness, title string) *models.Document {
	t.Helper()
	d, err := h.Client.Documents().Create(context.Background(), &models.CreateDocumentInput{Title: title})
	if err != nil {
		t.Fatalf("Create document %q: %v", title, err)
	}
	return d
}

// rowCount counts a table's physical rows, soft-deleted ones included, with a
// statement the counting querier does not record.
func rowCount(t *testing.T, h *Harness, table string) int64 {
	t.Helper()
	var n int64
	if err := h.Querier.Inner.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	return n
}

func assertRowCount(t *testing.T, h *Harness, table string, want int64) {
	t.Helper()
	if got := rowCount(t, h, table); got != want {
		t.Errorf("%s has %d rows, want %d", table, got, want)
	}
}

func assertProductExists(t *testing.T, h *Harness, id int64, want bool) {
	t.Helper()
	got, err := h.Client.Products().Exists(context.Background(), id)
	if err != nil {
		t.Fatalf("Products().Exists(%d) error = %v", id, err)
	}
	if got != want {
		t.Errorf("Products().Exists(%d) = %v, want %v", id, got, want)
	}
}

// statementsWithPrefix returns the recorded statements that start with prefix.
func statementsWithPrefix(h *Harness, prefix string) []string {
	var out []string
	for _, s := range h.Querier.Statements() {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// createDocuments creates n documents and returns their keys.
func createDocuments(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = createDocument(t, h, "doc").ID
	}
	return ids
}

// assertLiveDocuments checks how many documents are not soft-deleted.
func assertLiveDocuments(t *testing.T, h *Harness, want int64) {
	t.Helper()
	got, err := h.Client.Documents().Count(context.Background(), nil)
	if err != nil {
		t.Fatalf("Documents().Count error = %v", err)
	}
	if got != want {
		t.Errorf("Documents().Count = %d, want %d", got, want)
	}
}

// affectedPKsRecorder returns a client on h's querier whose mutation hook
// records each mutation's AffectedPKs after it runs, and a func that returns
// the AffectedPKs of the last mutation.
func affectedPKsRecorder(h *Harness) (*models.Client, func() []any) {
	var last []any
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			last = m.AffectedPKs
			return res, err
		}
	}
	return models.New(h.Base, models.WithMutationHook(record)), func() []any { return last }
}

func createRevision(t *testing.T, h *Harness, pk models.RevisionPK) {
	t.Helper()
	in := &models.CreateRevisionInput{DocumentID: pk.DocumentID, Number: pk.Number, Body: "body"}
	if _, err := h.Client.Revisions().Create(context.Background(), in); err != nil {
		t.Fatalf("Create revision %+v: %v", pk, err)
	}
}

// seedArchivedDocument inserts a soft-deleted 'archived' document, which the
// documents_archived_stays_deleted CHECK forbids restoring, and returns its key.
func seedArchivedDocument(t *testing.T, h *Harness) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := h.Querier.Inner.Exec(ctx, "INSERT INTO documents (title, deleted_at) VALUES ('archived', CURRENT_TIMESTAMP)"); err != nil {
		t.Fatalf("seed archived document: %v", err)
	}
	var id int64
	if err := h.Querier.Inner.QueryRow(ctx, "SELECT id FROM documents WHERE title = 'archived'").Scan(&id); err != nil {
		t.Fatalf("read archived document key: %v", err)
	}
	return id
}

// mutationRecorder returns a client on h's querier whose mutation hook
// records each MutationContext after the mutation runs, and a func that
// returns the recorded contexts.
func mutationRecorder(h *Harness) (*models.Client, func() []hook.MutationContext) {
	var seen []hook.MutationContext
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			seen = append(seen, *m)
			return res, err
		}
	}
	return models.New(h.Base, models.WithMutationHook(record)), func() []hook.MutationContext { return seen }
}

func createLabel(t *testing.T, h *Harness, name string) {
	t.Helper()
	if _, err := h.Client.Labels().Create(context.Background(), &models.CreateLabelInput{Name: name}); err != nil {
		t.Fatalf("Create label %q: %v", name, err)
	}
}

func createPricedProduct(t *testing.T, h *Harness, name string, price int64) *models.Product {
	t.Helper()
	p, err := h.Client.Products().Create(context.Background(), &models.CreateProductInput{Name: name, Price: price})
	if err != nil {
		t.Fatalf("Create product %q: %v", name, err)
	}
	return p
}

// queryRecorder returns a client on h's querier whose query hook records each
// QueryContext before the query runs, so a parent read is recorded before the
// relationship reads it starts, and a func that returns the recorded contexts.
// Relationship reads run in goroutines, so recording takes a lock.
func queryRecorder(h *Harness) (*models.Client, func() []hook.QueryContext) {
	var (
		mu   sync.Mutex
		seen []hook.QueryContext
	)
	record := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			mu.Lock()
			seen = append(seen, *q)
			mu.Unlock()
			return next(ctx, q)
		}
	}
	return models.New(h.Base, models.WithQueryHook(record)), func() []hook.QueryContext {
		mu.Lock()
		defer mu.Unlock()
		return append([]hook.QueryContext(nil), seen...)
	}
}

// otherTx begins a transaction on a connection of its own, for a subtest that
// needs a second transaction to contend with, and rolls it back at cleanup.
// Row-lock contention needs CapRowLocks: on SQLite's shared-cache database a
// conflicting statement fails with a table lock at once rather than waiting.
func otherTx(t *testing.T, h *Harness) context.Context {
	t.Helper()
	txCtx, err := database.NewTransaction(context.Background(), h.Querier, "contender")
	if err != nil {
		t.Fatalf("begin the contending transaction: %v", err)
	}
	t.Cleanup(func() { _ = database.Rollback(txCtx) })
	return txCtx
}

// lockModes are the lock modes that ask for a row lock.
var lockModes = []sql.LockMode{
	sql.LockForUpdate,
	sql.LockForShare,
	sql.LockForUpdateNoWait,
	sql.LockForUpdateSkipLocked,
}

// lockWait bounds every transaction a lock subtest opens, so a lock that
// blocks where the subtest expects none fails the subtest instead of hanging it.
const lockWait = 10 * time.Second

// withLock returns a call option that sets LockMode.
func withLock[FO any](mode sql.LockMode) func(*models.CallOptions[FO]) {
	return func(o *models.CallOptions[FO]) { o.LockMode = mode }
}

// beginBoundedTx begins a transaction bounded by lockWait and rolls it back
// at cleanup if the subtest has not ended it.
func beginBoundedTx(t *testing.T, h *Harness) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	txCtx, err := database.NewTransaction(ctx, h.Querier, "conformance")
	if err != nil {
		cancel()
		t.Fatalf("begin a transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Rollback(txCtx)
		cancel()
	})
	return txCtx
}

// createDeletedDocument creates a document, soft-deletes it, and returns the
// soft-deleted entity.
func createDeletedDocument(t *testing.T, h *Harness, title string) *models.Document {
	t.Helper()
	d := createDocument(t, h, title)
	deleted, err := h.Client.Documents().SoftDelete(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("SoftDelete document %q: %v", title, err)
	}
	return deleted
}

func createAuthor(t *testing.T, h *Harness, name string) *models.Author {
	t.Helper()
	a, err := h.Client.Authors().Create(context.Background(), &models.CreateAuthorInput{Name: name})
	if err != nil {
		t.Fatalf("Create author %q: %v", name, err)
	}
	return a
}

// createPost creates a post under authorID, or unparented when authorID is nil.
func createPost(t *testing.T, h *Harness, title string, authorID *int64) *models.Post {
	t.Helper()
	p, err := h.Client.Posts().Create(context.Background(), &models.CreatePostInput{Title: title, AuthorID: omittable.Set(authorID)})
	if err != nil {
		t.Fatalf("Create post %q: %v", title, err)
	}
	return p
}

// createProfile creates an author profile under authorID, or unparented when
// authorID is nil.
func createProfile(t *testing.T, h *Harness, bio string, authorID *int64) *models.AuthorProfile {
	t.Helper()
	p, err := h.Client.AuthorProfiles().Create(context.Background(), &models.CreateAuthorProfileInput{Bio: bio, AuthorID: omittable.Set(authorID)})
	if err != nil {
		t.Fatalf("Create author profile %q: %v", bio, err)
	}
	return p
}

func createTag(t *testing.T, h *Harness, name string) *models.Tag {
	t.Helper()
	tag, err := h.Client.Tags().Create(context.Background(), &models.CreateTagInput{Name: name})
	if err != nil {
		t.Fatalf("Create tag %q: %v", name, err)
	}
	return tag
}

func linkAuthorTag(t *testing.T, h *Harness, authorID, tagID int64) {
	t.Helper()
	if _, err := h.Client.AuthorTags().Create(context.Background(), &models.CreateAuthorTagInput{AuthorID: authorID, TagID: tagID}); err != nil {
		t.Fatalf("Create author tag (%d, %d): %v", authorID, tagID, err)
	}
}

// postParents returns every post's author_id by post id, soft-deleted posts
// included, read with a statement the counting querier does not record. A
// nil value is an unparented post.
func postParents(t *testing.T, h *Harness) map[int64]*int64 {
	t.Helper()
	return parentsOf(t, h, "posts")
}

// profileParents is postParents for author_profiles.
func profileParents(t *testing.T, h *Harness) map[int64]*int64 {
	t.Helper()
	return parentsOf(t, h, "author_profiles")
}

func parentsOf(t *testing.T, h *Harness, table string) map[int64]*int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT id, author_id FROM "+table)
	if err != nil {
		t.Fatalf("read %s parents: %v", table, err)
	}
	defer rows.Close()
	got := map[int64]*int64{}
	for rows.Next() {
		var id int64
		var parent *int64
		if err := rows.Scan(&id, &parent); err != nil {
			t.Fatalf("scan %s parent: %v", table, err)
		}
		got[id] = parent
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s parents: %v", table, err)
	}
	return got
}

// authorTagPairs returns every author_tags row as an (author, tag) pair,
// sorted, read with a statement the counting querier does not record.
func authorTagPairs(t *testing.T, h *Harness) [][2]int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT author_id, tag_id FROM author_tags ORDER BY author_id, tag_id")
	if err != nil {
		t.Fatalf("read author tags: %v", err)
	}
	defer rows.Close()
	var got [][2]int64
	for rows.Next() {
		var p [2]int64
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			t.Fatalf("scan author tag: %v", err)
		}
		got = append(got, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read author tags: %v", err)
	}
	return got
}
