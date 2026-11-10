package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// A key past int32 is what a narrower FK column cannot hold.
const wideKey int64 = 4294967301

func seedKeyWidths(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM width_scoped_children`,
		`DELETE FROM width_links`,
		`DELETE FROM width_narrow_children`,
		`DELETE FROM width_wide_children`,
		`DELETE FROM width_bigs`,
		`DELETE FROM width_smalls`,
		`INSERT INTO width_smalls (id, scope) VALUES (1, 1), (2, 4294967301)`,
		`INSERT INTO width_bigs (id) VALUES (5), (4294967301)`,
		`INSERT INTO width_wide_children (small_id) VALUES (1), (1), (NULL)`,
		`INSERT INTO width_narrow_children (big_id) VALUES (5)`,
		`INSERT INTO width_links (small_id, big_id) VALUES (1, 5)`,
		`INSERT INTO width_scoped_children (scope, small_id) VALUES (1, 1)`,
	} {
		if _, err := testDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

func TestKeyWidth_loadsAcrossWidths(t *testing.T) {
	seedKeyWidths(t)
	ctx := context.Background()
	client := newClient()

	smalls, err := client.WidthSmalls().GetMany(ctx, &models.GetWidthSmallsInput{}, func(o *models.CallOptions[models.WidthSmallFieldOptions]) {
		o.FieldOptions = &models.WidthSmallFieldOptions{
			ID: true, Scope: true,
			WidthWideChildren:   &models.WidthWideChildRelationshipOptions{FieldOptions: &models.WidthWideChildFieldOptions{ID: true}},
			WidthBigs:           &models.WidthBigRelationshipOptions{FieldOptions: &models.WidthBigFieldOptions{ID: true}},
			WidthScopedChildren: &models.WidthScopedChildRelationshipOptions{FieldOptions: &models.WidthScopedChildFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("load smalls: %v", err)
	}
	type counts struct{ Wide, Bigs, Scoped int }
	gotSmalls := map[int32]counts{}
	for _, s := range smalls {
		gotSmalls[s.ID] = counts{len(s.WidthWideChildren), len(s.WidthBigs), len(s.WidthScopedChildren)}
	}
	wantSmalls := map[int32]counts{1: {2, 1, 1}, 2: {}}
	if diff := cmp.Diff(wantSmalls, gotSmalls); diff != "" {
		t.Errorf("smalls (-want +got):\n%s", diff)
	}

	bigs, err := client.WidthBigs().GetMany(ctx, &models.GetWidthBigsInput{}, func(o *models.CallOptions[models.WidthBigFieldOptions]) {
		o.FieldOptions = &models.WidthBigFieldOptions{
			ID:                  true,
			WidthNarrowChildren: &models.WidthNarrowChildRelationshipOptions{FieldOptions: &models.WidthNarrowChildFieldOptions{ID: true}},
			WidthSmalls:         &models.WidthSmallRelationshipOptions{FieldOptions: &models.WidthSmallFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("load bigs: %v", err)
	}
	type bigCounts struct{ Narrow, Smalls int }
	gotBigs := map[int64]bigCounts{}
	for _, b := range bigs {
		gotBigs[b.ID] = bigCounts{len(b.WidthNarrowChildren), len(b.WidthSmalls)}
	}
	wantBigs := map[int64]bigCounts{5: {1, 1}, wideKey: {}}
	if diff := cmp.Diff(wantBigs, gotBigs); diff != "" {
		t.Errorf("bigs (-want +got):\n%s", diff)
	}
}

func TestKeyWidth_relationshipFilters(t *testing.T) {
	seedKeyWidths(t)
	ctx := context.Background()
	client := newClient()

	for _, tt := range []struct {
		name   string
		filter *models.WidthSmallFilter
		want   int
	}{
		{"wider fk", &models.WidthSmallFilter{WidthWideChildren: &models.WidthWideChildFilter{ID: &comparator.Number[int64]{Gt: new(int64(0))}}}, 1},
		{"m2m", &models.WidthSmallFilter{WidthBigs: &models.WidthBigFilter{ID: &comparator.Number[int64]{Eq: new(int64(5))}}}, 1},
		{"composite", &models.WidthSmallFilter{WidthScopedChildren: &models.WidthScopedChildFilter{Scope: &comparator.Number[int32]{Eq: new(int32(1))}}}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.WidthSmalls().GetMany(ctx, &models.GetWidthSmallsInput{Filter: tt.filter})
			if err != nil {
				t.Fatalf("GetMany: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d rows, want %d", len(got), tt.want)
			}
		})
	}

	narrower, err := client.WidthBigs().GetMany(ctx, &models.GetWidthBigsInput{Filter: &models.WidthBigFilter{
		WidthNarrowChildren: &models.WidthNarrowChildFilter{BigID: &comparator.Number[int32]{Eq: new(int32(5))}},
	}})
	if err != nil {
		t.Fatalf("narrower fk: %v", err)
	}
	if len(narrower) != 1 || narrower[0].ID != 5 {
		t.Errorf("narrower fk: got %+v, want the parent 5", narrower)
	}
}
