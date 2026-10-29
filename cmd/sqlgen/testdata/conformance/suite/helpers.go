package suite

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"

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
	return models.New(h.Querier, models.WithMutationHook(record)), func() []any { return last }
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
	return models.New(h.Querier, models.WithMutationHook(record)), func() []hook.MutationContext { return seen }
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
	return models.New(h.Querier, models.WithQueryHook(record)), func() []hook.QueryContext {
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
