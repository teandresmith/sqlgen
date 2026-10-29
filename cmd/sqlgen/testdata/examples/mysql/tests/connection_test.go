package tests

// Cursor pagination (Connection) e2e, MySQL dialect.
// See postgres/tests/connection_test.go for the design rationale; this file
// mirrors the same shape adapted for MySQL's int64 auto-increment PKs.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

func seedConnProducts(t *testing.T, ctx context.Context, client *models.Client, count int, namePrefix string) (int32, []int64) {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: namePrefix + "Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	ids := make([]int64, 0, count)
	for i := range count {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			CategoryID: cat.ID,
			Title:      fmt.Sprintf("%sProduct%03d", namePrefix, i),
			Price:      float64(i) + 1.0,
			SKU:        fmt.Sprintf("%s-%03d", namePrefix, i),
			Attributes: types.JSON([]byte(`{}`)),
		})
		if err != nil {
			t.Fatalf("Create product %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = client.Products().HardDelete(ctx, id)
		}
	})
	return cat.ID, ids
}

func TestConnection_CursorRoundTripByteEquality(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	catID, _ := seedConnProducts(t, ctx, client, 3, "Roundtrip")

	first := 1
	conn, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int32]{Eq: new(catID)}},
		First:  &first,
	})
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if len(conn.Edges) != 1 {
		t.Fatalf("Edges = %d, want 1", len(conn.Edges))
	}
	cursor := conn.Edges[0].Cursor

	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("cursor is not valid base64: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("cursor is not valid JSON: %v", err)
	}
	if _, ok := decoded["id"]; !ok {
		t.Errorf("cursor missing 'id' key; decoded = %v", decoded)
	}

	reMarshalled, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal cursor map: %v", err)
	}
	reEncoded := base64.StdEncoding.EncodeToString(reMarshalled)
	if reEncoded != cursor {
		t.Errorf("cursor not byte-stable under decode→re-encode:\n  orig: %q\n  new:  %q", cursor, reEncoded)
	}
}

// TestConnection_CompositePKCursorTraversal — cursor_keys for product_tag_labels
// is [product_id, tag_name, label]. Two products share multiple labels so the
// row-value keyset comparator must break ties via tag_name / label to stay
// deterministic. See postgres/tests/connection_test.go for the rationale.
func TestConnection_CompositePKCursorTraversal(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CompositeCursorCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	pA, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "CompA", Price: 1.0, SKU: "CompA-SKU", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product A: %v", err)
	}
	pB, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "CompB", Price: 2.0, SKU: "CompB-SKU", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product B: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Products().HardDelete(ctx, pA.ID)
		_ = client.Products().HardDelete(ctx, pB.ID)
	})

	desc := "ok"
	rows := []struct {
		productID      int64
		tagName, label string
	}{
		{pA.ID, "color", "blue"},
		{pA.ID, "color", "red"},
		{pA.ID, "size", "large"},
		{pB.ID, "color", "green"},
		{pB.ID, "size", "small"},
	}
	for _, r := range rows {
		if _, err := client.ProductTagLabels().Create(ctx, &models.CreateProductTagLabelInput{
			ProductID:   r.productID,
			TagName:     r.tagName,
			Label:       r.label,
			Description: omittable.Set(&desc),
		}); err != nil {
			t.Fatalf("Create product_tag_label (%d/%s/%s): %v", r.productID, r.tagName, r.label, err)
		}
	}
	t.Cleanup(func() {
		for _, r := range rows {
			_ = client.ProductTagLabels().HardDelete(ctx, models.ProductTagLabelPK{
				ProductID: r.productID, TagName: r.tagName, Label: r.label,
			})
		}
	})

	first := 1
	seed, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{
		Filter: &models.ProductTagLabelFilter{
			ProductID: &comparator.Number[int64]{In: []int64{pA.ID, pB.ID}},
		},
		First: &first,
	})
	if err != nil {
		t.Fatalf("seed Connection: %v", err)
	}
	if len(seed.Edges) != 1 {
		t.Fatalf("seed edges = %d, want 1", len(seed.Edges))
	}
	raw, err := base64.StdEncoding.DecodeString(seed.Edges[0].Cursor)
	if err != nil {
		t.Fatalf("decode cursor base64: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode cursor json: %v", err)
	}
	for _, key := range []string{"product_id", "tag_name", "label"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("composite cursor missing key %q; decoded = %v", key, decoded)
		}
	}

	pageSize := 2
	seen := make(map[string]int)
	var prevEnd *string
	pageCount := 0
	for {
		conn, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{
			Filter: &models.ProductTagLabelFilter{
				ProductID: &comparator.Number[int64]{In: []int64{pA.ID, pB.ID}},
			},
			First: &pageSize,
			After: prevEnd,
		})
		if err != nil {
			t.Fatalf("Connection page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			key := fmt.Sprintf("%d|%s|%s", edge.Node.ProductID, edge.Node.TagName, edge.Node.Label)
			seen[key]++
		}
		pageCount++
		if !conn.PageInfo.HasNextPage {
			break
		}
		prevEnd = conn.PageInfo.EndCursor
		if pageCount > len(rows) {
			t.Fatalf("composite forward traversal did not terminate after %d pages", pageCount)
		}
	}

	if len(seen) != len(rows) {
		t.Errorf("composite traversal visited %d distinct rows, want %d", len(seen), len(rows))
	}
	for key, n := range seen {
		if n != 1 {
			t.Errorf("row %s visited %d times, want 1", key, n)
		}
	}
}
