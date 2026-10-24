package suite

import (
	"context"
	"testing"

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
