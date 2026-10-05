package tests

// A Connection whose cursor key only parses quoted. The first page
// needs no keyset; every page after it resumes from the cursor with a keyset
// WHERE that names each cursor key, so a key written raw fails there with
// `unrecognized token: "2024_quota"`. digit_leading_columns pages on
// ["2024_quota", "id"] (sqlgen.yml), and the quotas are seeded out of ID order
// so the rows come back in keyset order, not insertion order.

import (
	"cmp"
	"context"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestDigitLeadingColumns_ConnectionPagesOnQuotedCursorKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	type key struct{ quota, id int64 }
	var ids []int64
	var want []key
	for _, quota := range []int64{2, 0, 1, 0, 1} {
		row, err := client.DigitLeadingColumns().Create(ctx, &models.CreateDigitLeadingColumnInput{Col2024Quota: omittable.Set(quota)})
		if err != nil {
			t.Fatalf("Create digit_leading_columns: %v", err)
		}
		t.Cleanup(func() { _ = client.DigitLeadingColumns().HardDelete(ctx, row.ID) })
		ids = append(ids, row.ID)
		want = append(want, key{quota, row.ID})
	}
	slices.SortFunc(want, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.quota, b.quota), cmp.Compare(a.id, b.id))
	})
	// Scoped to this test's rows by ID: other tests leave rows behind.
	filter := &models.DigitLeadingColumnFilter{ID: &comparator.Number[int64]{In: ids}}

	keysOf := func(edges []models.Edge[models.DigitLeadingColumn]) []key {
		out := make([]key, len(edges))
		for i, e := range edges {
			out[i] = key{e.Node.Col2024Quota, e.Node.ID}
		}
		return out
	}

	t.Run("forward", func(t *testing.T) {
		var got []key
		var after *string
		for page := 1; page <= len(want); page++ {
			first := 2
			conn, err := client.DigitLeadingColumns().Connection(ctx, models.ConnectionInput[models.DigitLeadingColumnFilter]{
				Filter: filter, First: &first, After: after,
			})
			if err != nil {
				t.Fatalf("Connection page %d: %v", page, err)
			}
			got = append(got, keysOf(conn.Edges)...)
			if !conn.PageInfo.HasNextPage {
				break
			}
			after = conn.PageInfo.EndCursor
		}
		if !slices.Equal(got, want) {
			t.Errorf("forward pages = %v, want %v", got, want)
		}
	})

	t.Run("backward", func(t *testing.T) {
		var got []key
		var before *string
		for page := 1; page <= len(want); page++ {
			last := 2
			conn, err := client.DigitLeadingColumns().Connection(ctx, models.ConnectionInput[models.DigitLeadingColumnFilter]{
				Filter: filter, Last: &last, Before: before,
			})
			if err != nil {
				t.Fatalf("Connection page %d: %v", page, err)
			}
			got = append(keysOf(conn.Edges), got...)
			if !conn.PageInfo.HasPreviousPage {
				break
			}
			before = conn.PageInfo.StartCursor
		}
		if !slices.Equal(got, want) {
			t.Errorf("backward pages = %v, want %v", got, want)
		}
	})
}
