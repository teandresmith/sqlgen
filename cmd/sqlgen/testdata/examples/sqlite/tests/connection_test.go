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

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

const connSeedCount = 50

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

func TestConnection_ForwardTraversal50Rows(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	catID, _ := seedConnProducts(t, ctx, client, connSeedCount, "FwdTrav")

	pageSize := 10
	seen := make(map[int64]int)
	var prevEnd *string
	pageCount := 0

	for {
		conn, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
			Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
			First:  &pageSize,
			After:  prevEnd,
		})
		if err != nil {
			t.Fatalf("Connection forward page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			seen[edge.Node.ID]++
		}
		pageCount++
		if !conn.PageInfo.HasNextPage {
			break
		}
		if conn.PageInfo.EndCursor == nil {
			t.Fatalf("page %d: HasNextPage=true but EndCursor=nil", pageCount-1)
		}
		prevEnd = conn.PageInfo.EndCursor
		if pageCount > connSeedCount {
			t.Fatalf("forward traversal did not terminate after %d pages", pageCount)
		}
	}

	if len(seen) != connSeedCount {
		t.Errorf("forward traversal visited %d distinct rows, want %d", len(seen), connSeedCount)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %d visited %d times, want 1", id, n)
		}
	}
}

func TestConnection_BackwardTraversal50Rows(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	catID, _ := seedConnProducts(t, ctx, client, connSeedCount, "BwdTrav")

	pageSize := 10
	seen := make(map[int64]int)
	var prevStart *string
	pageCount := 0

	for {
		conn, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
			Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
			Last:   &pageSize,
			Before: prevStart,
		})
		if err != nil {
			t.Fatalf("Connection backward page %d: %v", pageCount, err)
		}
		for _, edge := range conn.Edges {
			seen[edge.Node.ID]++
		}
		pageCount++
		if !conn.PageInfo.HasPreviousPage {
			break
		}
		if conn.PageInfo.StartCursor == nil {
			t.Fatalf("page %d: HasPreviousPage=true but StartCursor=nil", pageCount-1)
		}
		prevStart = conn.PageInfo.StartCursor
		if pageCount > connSeedCount {
			t.Fatalf("backward traversal did not terminate after %d pages", pageCount)
		}
	}

	if len(seen) != connSeedCount {
		t.Errorf("backward traversal visited %d distinct rows, want %d", len(seen), connSeedCount)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %d visited %d times, want 1", id, n)
		}
	}
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

func TestConnection_TamperedCursorReturnsErrInvalidCursor(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	notBase64 := "not-a-valid-base64!*cursor"
	notJSON := base64.StdEncoding.EncodeToString([]byte("this is not json at all"))

	first := 5
	last := 5

	cases := []struct {
		name  string
		input models.ConnectionInput[models.ProductFilter]
	}{
		{
			name:  "non-base64 after cursor",
			input: models.ConnectionInput[models.ProductFilter]{First: &first, After: &notBase64},
		},
		{
			name:  "non-json after cursor",
			input: models.ConnectionInput[models.ProductFilter]{First: &first, After: &notJSON},
		},
		{
			name:  "non-base64 before cursor",
			input: models.ConnectionInput[models.ProductFilter]{Last: &last, Before: &notBase64},
		},
		{
			name:  "non-json before cursor",
			input: models.ConnectionInput[models.ProductFilter]{Last: &last, Before: &notJSON},
		},
		{
			name:  "first and last both set",
			input: models.ConnectionInput[models.ProductFilter]{First: &first, Last: &last},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Products().Connection(ctx, tc.input)
			if !errors.Is(err, models.ErrInvalidCursor) {
				t.Errorf("err = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestConnection_PageInfoFirstMiddleLast(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	catID, _ := seedConnProducts(t, ctx, client, 25, "PageInfo")

	pageSize := 10

	page1, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
		First:  &pageSize,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if !page1.PageInfo.HasNextPage {
		t.Error("page 1: HasNextPage = false, want true")
	}
	if page1.PageInfo.HasPreviousPage {
		t.Error("page 1: HasPreviousPage = true, want false")
	}
	if page1.PageInfo.StartCursor == nil || page1.PageInfo.EndCursor == nil {
		t.Errorf("page 1: StartCursor=%v EndCursor=%v, want non-nil", page1.PageInfo.StartCursor, page1.PageInfo.EndCursor)
	}
	if page1.TotalCount != 25 {
		t.Errorf("page 1 TotalCount = %d, want 25", page1.TotalCount)
	}
	if *page1.PageInfo.StartCursor != page1.Edges[0].Cursor {
		t.Errorf("page 1 StartCursor != first edge cursor")
	}
	if *page1.PageInfo.EndCursor != page1.Edges[len(page1.Edges)-1].Cursor {
		t.Errorf("page 1 EndCursor != last edge cursor")
	}

	page2, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
		First:  &pageSize,
		After:  page1.PageInfo.EndCursor,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if !page2.PageInfo.HasNextPage {
		t.Error("page 2: HasNextPage = false, want true")
	}
	if !page2.PageInfo.HasPreviousPage {
		t.Error("page 2: HasPreviousPage = false, want true")
	}
	if len(page2.Edges) != 10 {
		t.Errorf("page 2 edges = %d, want 10", len(page2.Edges))
	}

	page3, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(catID)}},
		First:  &pageSize,
		After:  page2.PageInfo.EndCursor,
	})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if page3.PageInfo.HasNextPage {
		t.Error("page 3: HasNextPage = true, want false (last page)")
	}
	if !page3.PageInfo.HasPreviousPage {
		t.Error("page 3: HasPreviousPage = false, want true")
	}
	if len(page3.Edges) != 5 {
		t.Errorf("page 3 edges = %d, want 5", len(page3.Edges))
	}

	bogusCat := int64(-1)
	empty, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{
		Filter: &models.ProductFilter{CategoryID: &comparator.Number[int64]{Eq: new(bogusCat)}},
		First:  &pageSize,
	})
	if err != nil {
		t.Fatalf("empty-result Connection: %v", err)
	}
	if len(empty.Edges) != 0 {
		t.Errorf("empty Connection edges = %d, want 0", len(empty.Edges))
	}
	if empty.PageInfo.StartCursor != nil || empty.PageInfo.EndCursor != nil {
		t.Errorf("empty Connection cursors = (%v, %v), want (nil, nil)",
			empty.PageInfo.StartCursor, empty.PageInfo.EndCursor)
	}
	if empty.PageInfo.HasNextPage || empty.PageInfo.HasPreviousPage {
		t.Errorf("empty Connection HasNext/Prev = (%v, %v), want (false, false)",
			empty.PageInfo.HasNextPage, empty.PageInfo.HasPreviousPage)
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
