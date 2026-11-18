package tests

// Cursor pagination (Connection) e2e, SQLite dialect.
// See postgres/tests/connection_test.go for the design rationale; this file
// mirrors the same shape adapted for SQLite's int64 autoincrement PKs.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func seedConnProducts(t *testing.T, ctx context.Context, client *models.Client, count int, namePrefix string) (int64, []int64) {
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
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
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

// A cursor that decodes cleanly but lacks a cursor key, or holds null for one,
// has no value to compare against, so Connection rejects it before querying.
func TestConnection_CursorMissingKeyReturnsErrInvalidCursor(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	encode := func(raw string) *string {
		return new(base64.StdEncoding.EncodeToString([]byte(raw)))
	}
	first := 5
	last := 5

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "table after cursor without keys",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first, After: encode(`{}`)})
				return err
			},
		},
		{
			name: "table after cursor with null key",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first, After: encode(`{"id":null}`)})
				return err
			},
		},
		{
			name: "table before cursor without keys",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{Last: &last, Before: encode(`{}`)})
				return err
			},
		},
		{
			name: "table before cursor with null key",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{Last: &last, Before: encode(`{"id":null}`)})
				return err
			},
		},
		{
			name: "composite cursor missing trailing keys",
			call: func() error {
				_, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{First: &first, After: encode(`{"product_id":1}`)})
				return err
			},
		},
		{
			name: "view after cursor without keys",
			call: func() error {
				_, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{First: &first, After: encode(`{}`)})
				return err
			},
		},
		{
			name: "view before cursor with null key",
			call: func() error {
				_, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{Last: &last, Before: encode(`{"id":null}`)})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, models.ErrInvalidCursor) {
				t.Errorf("err = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

// A cursor value is decoded into its key column's Go type, so a value of the
// wrong JSON shape or type is ErrInvalidCursor.
func TestConnection_CursorWrongShapeReturnsErrInvalidCursor(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	encode := func(raw string) *string {
		return new(base64.StdEncoding.EncodeToString([]byte(raw)))
	}
	first := 5
	last := 5

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "table after cursor with empty array",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first, After: encode(`{"id":[]}`)})
				return err
			},
		},
		{
			name: "table after cursor with array",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first, After: encode(`{"id":[1,2]}`)})
				return err
			},
		},
		{
			name: "table before cursor with object",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{Last: &last, Before: encode(`{"id":{"a":1}}`)})
				return err
			},
		},
		{
			name: "table after cursor with string for integer key",
			call: func() error {
				_, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{First: &first, After: encode(`{"id":"1"}`)})
				return err
			},
		},
		{
			name: "composite cursor with array in a later key",
			call: func() error {
				_, err := client.ProductTagLabels().Connection(ctx, models.ConnectionInput[models.ProductTagLabelFilter]{First: &first, After: encode(`{"product_id":1,"tag_name":[1,2],"label":"a"}`)})
				return err
			},
		},
		{
			name: "view after cursor with object",
			call: func() error {
				_, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{First: &first, After: encode(`{"id":{"a":1}}`)})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, models.ErrInvalidCursor) {
				t.Errorf("err = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

// A []byte cursor key round-trips as []byte, so the keyset compares BLOB to
// BLOB and each page advances.
func TestConnection_BinaryKeyCursorAdvances(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	for _, id := range []string{"conn-bin-a", "conn-bin-b", "conn-bin-c"} {
		if _, err := client.BinaryKeys().Create(ctx, &models.CreateBinaryKeyInput{ID: []byte(id), Name: id}); err != nil {
			t.Fatalf("create binary key %q: %v", id, err)
		}
	}
	filter := &models.BinaryKeyFilter{Name: &comparator.String{StartsWith: new("conn-bin-")}}
	one := 1

	var got []string
	var after *string
	for range 4 {
		page, err := client.BinaryKeys().Connection(ctx, models.ConnectionInput[models.BinaryKeyFilter]{Filter: filter, First: &one, After: after})
		if err != nil {
			t.Fatalf("Connection: %v", err)
		}
		for _, edge := range page.Edges {
			got = append(got, string(edge.Node.ID))
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		after = page.PageInfo.EndCursor
	}

	want := []string{"conn-bin-a", "conn-bin-b", "conn-bin-c"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("paged ids mismatch (-want +got):\n%s", diff)
	}
}

func TestConnection_CompositePKCursorTraversal(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "CompositeCursorCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	pA, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "CompA", Price: 1.0, SKU: "CompA-SKU",
	})
	if err != nil {
		t.Fatalf("Create product A: %v", err)
	}
	pB, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "CompB", Price: 2.0, SKU: "CompB-SKU",
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
