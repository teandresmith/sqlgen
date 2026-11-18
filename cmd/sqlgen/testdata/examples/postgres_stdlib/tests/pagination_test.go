package tests

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Offset Pagination ---

// --- Cursor Pagination ---

// TestConnection_TimeCursorKey pages categories, whose cursor_keys are
// (created_at, id), one row at a time in both directions. Every page after
// the first decodes a time.Time from the cursor and binds it into the keyset
// condition; two rows share a created_at, so the id tie-break must hold too.
func TestConnection_TimeCursorKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	ids := []string{
		"00000000-0000-0000-0000-00000000c001",
		"00000000-0000-0000-0000-00000000c002",
		"00000000-0000-0000-0000-00000000c003",
	}
	names := []string{"TimeCursorCat1", "TimeCursorCat2", "TimeCursorCat3"}
	// created_at orders the rows against their ids: the lowest id is the
	// newest, and the other two share a created_at, so id breaks their tie.
	if _, err := testDB.ExecContext(ctx, `INSERT INTO categories (id, name, created_at) VALUES
		($1, $4, '2026-01-02 00:00:00+00'),
		($2, $5, '2026-01-01 00:00:00.123456+00'),
		($3, $6, '2026-01-01 00:00:00.123456+00')`,
		ids[0], ids[1], ids[2], names[0], names[1], names[2]); err != nil {
		t.Fatalf("seeding categories: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(context.Background(), `DELETE FROM categories WHERE id = ANY($1::uuid[])`, "{"+strings.Join(ids, ",")+"}")
	})

	filter := &models.CategoryFilter{Name: &comparator.String{In: names}}
	one := 1

	var forward []string
	var after *string
	for range len(ids) + 1 {
		conn, err := client.Categories().Connection(ctx, models.ConnectionInput[models.CategoryFilter]{
			Filter: filter, First: &one, After: after,
		})
		if err != nil {
			t.Fatalf("Connection(after %v): %v", after, err)
		}
		for _, e := range conn.Edges {
			forward = append(forward, e.Node.ID.String())
		}
		if !conn.PageInfo.HasNextPage {
			break
		}
		after = conn.PageInfo.EndCursor
	}
	if want := []string{ids[1], ids[2], ids[0]}; !slices.Equal(forward, want) {
		t.Errorf("forward pages = %v, want %v", forward, want)
	}

	var backward []string
	var before *string
	for range len(ids) + 1 {
		conn, err := client.Categories().Connection(ctx, models.ConnectionInput[models.CategoryFilter]{
			Filter: filter, Last: &one, Before: before,
		})
		if err != nil {
			t.Fatalf("Connection(before %v): %v", before, err)
		}
		for _, e := range conn.Edges {
			backward = append(backward, e.Node.ID.String())
		}
		if !conn.PageInfo.HasPreviousPage {
			break
		}
		before = conn.PageInfo.StartCursor
	}
	if want := []string{ids[0], ids[2], ids[1]}; !slices.Equal(backward, want) {
		t.Errorf("backward pages = %v, want %v", backward, want)
	}
}
