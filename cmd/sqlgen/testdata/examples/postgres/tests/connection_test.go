package tests

// Cursor pagination (Connection) e2e coverage expansion.
//
// The Connection contract runs on every dialect in the conformance suite. These
// tests keep what it cannot: opaque cursor round-trip byte-equality, and
// composite-PK cursor encoding over this example's 3-column key.
//
// Composite-PK cursor test uses product_tag_labels (3-column PK: product_id,
// tag_name, label — cursor_keys explicitly set in sqlgen.yml). In the postgres
// example schema order_items has a single-UUID PK — product_tag_labels is the
// 3-column-composite table that exercises the same cursor-keyset behavior.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// seedConnProducts creates a dedicated category with `count` products and
// returns (categoryID, productIDs). Cleanup is registered via t.Cleanup.
func seedConnProducts(t *testing.T, ctx context.Context, client *models.Client, count int, namePrefix string) (uuid.UUID, []uuid.UUID) {
	t.Helper()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: namePrefix + "Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	ids := make([]uuid.UUID, 0, count)
	for i := range count {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:       fmt.Sprintf("%sProduct%03d", namePrefix, i),
			Price:      float64(i) + 1.0,
			CategoryID: cat.ID,
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

// TestConnection_CursorRoundTripByteEquality decodes an opaque cursor,
// re-encodes the decoded map, and asserts the bytes match. This verifies the
// encoded cursor is a stable, deterministic base64(JSON) of the cursor key
// values (PRD §14.2: "cursors are opaque base64-encoded JSON objects").
func TestConnection_CursorRoundTripByteEquality(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	catID, _ := seedConnProducts(t, ctx, client, 3, "Roundtrip")

	first := 1
	conn, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.ID{Eq: new(catID.String())}},
		First:  &first,
	})
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if len(conn.Edges) != 1 {
		t.Fatalf("Edges = %d, want 1", len(conn.Edges))
	}
	cursor := conn.Edges[0].Cursor

	// Decode → verify structure → re-encode → compare.
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
	// The cursor is JSON, so its `id` is the UUID's canonical string form —
	// comparing it against the uuid.UUID directly would compare an any-boxed
	// string to an any-boxed array and never match, however identically the
	// two print.
	if got, want := decoded["id"], conn.Edges[0].Node.ID.String(); got != want {
		t.Errorf("cursor id = %v, want %v", got, want)
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

// TestConnection_CompositePKCursorTraversal exercises a 3-column composite
// cursor (product_tag_labels, cursor_keys=[product_id, tag_name, label]). It
// verifies:
//   - the encoded cursor decodes to a JSON object containing all three cursor keys
//   - forward traversal visits every row exactly once under ties on product_id
//     (two products each share multiple labels — the row-value keyset comparator
//     (a,b,c) > (v1,v2,v3) breaks ties via tag_name then label)
func TestConnection_CompositePKCursorTraversal(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CompositeCursorCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	// Two products; each with multiple (tag_name, label) pairs. This creates
	// "ties" on the first cursor key (product_id) — the keyset comparator must
	// break them with tag_name / label to be deterministic.
	pA, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "CompA", Price: 1.0, CategoryID: cat.ID})
	if err != nil {
		t.Fatalf("Create product A: %v", err)
	}
	pB, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "CompB", Price: 2.0, CategoryID: cat.ID})
	if err != nil {
		t.Fatalf("Create product B: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Products().HardDelete(ctx, pA.ID)
		_ = client.Products().HardDelete(ctx, pB.ID)
	})

	desc := "ok"
	rows := []struct {
		productID      uuid.UUID
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
			t.Fatalf("Create product_tag_label (%s/%s/%s): %v", r.productID, r.tagName, r.label, err)
		}
	}
	t.Cleanup(func() {
		for _, r := range rows {
			_ = client.ProductTagLabels().HardDelete(ctx, models.ProductTagLabelPK{
				ProductID: r.productID, TagName: r.tagName, Label: r.label,
			})
		}
	})

	// Assert cursor encodes all three keys.
	first := 1
	seed, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{
		Filter: &models.ProductTagLabelFilter{
			ProductID: &comparator.ID{In: []string{pA.ID.String(), pB.ID.String()}},
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

	// Forward traversal page-by-page. Every row visited exactly once.
	pageSize := 2
	seen := make(map[string]int)
	var prevEnd *string
	pageCount := 0
	for {
		conn, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{
			Filter: &models.ProductTagLabelFilter{
				ProductID: &comparator.ID{In: []string{pA.ID.String(), pB.ID.String()}},
			},
			First: &pageSize,
			After: prevEnd,
		})
		if err != nil {
			t.Fatalf("Connection page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			key := edge.Node.ProductID.String() + "|" + edge.Node.TagName + "|" + edge.Node.Label
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
