package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestDigitLeadingColumns_FilterOnQuotedColumn pins that a filter on a
// column that needs quoting reaches the WHERE clause quoted. The generated
// ToConditions used to hand the comparator the raw name, so every
// filter-accepting method failed with `unrecognized token: "2024_quota"` — a
// digit-leading identifier is not legal SQL unquoted on SQLite. The table is
// the sqlite example's only one whose columns need quoting.
//
// The read paths share ToConditions, so each case drives GetMany, Count and
// ExistsWhere; the where-mutations close the test and double as cleanup.
func TestDigitLeadingColumns_FilterOnQuotedColumn(t *testing.T) {
	ctx := context.Background()
	client := newClient().DigitLeadingColumns()

	seed := []*models.CreateDigitLeadingColumnInput{
		{Col2024Quota: omittable.Set(int64(241001)), Col1stPlace: omittable.Set("quotedcol-gold")},
		{Col2024Quota: omittable.Set(int64(241002)), Col1stPlace: omittable.Set("quotedcol-silver")},
		{Col2024Quota: omittable.Set(int64(241003)), Col1stPlace: omittable.Set("quotedcol-bronze")},
	}
	rows, err := client.CreateMany(ctx, seed)
	if err != nil {
		t.Fatalf("CreateMany digit_leading_columns: %v", err)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	seeded := &models.DigitLeadingColumnFilter{ID: &comparator.Number[int64]{In: ids}}

	tests := []struct {
		name   string
		filter *models.DigitLeadingColumnFilter
		want   []int64
	}{
		{
			name:   "eq on digit-leading number",
			filter: &models.DigitLeadingColumnFilter{Col2024Quota: &comparator.Number[int64]{Eq: new(int64(241002))}},
			want:   []int64{ids[1]},
		},
		{
			name:   "in and between on digit-leading number",
			filter: &models.DigitLeadingColumnFilter{Col2024Quota: &comparator.Number[int64]{In: []int64{241001, 241003}, Between: &comparator.Range[int64]{Start: 241000, End: 241002}}},
			want:   []int64{ids[0]},
		},
		{
			name:   "contains on digit-leading string",
			filter: &models.DigitLeadingColumnFilter{Col1stPlace: &comparator.String{Contains: new("col-b")}},
			want:   []int64{ids[2]},
		},
		{
			name: "or across two digit-leading columns",
			filter: &models.DigitLeadingColumnFilter{Or: []*models.DigitLeadingColumnFilter{
				{Col2024Quota: &comparator.Number[int64]{Eq: new(int64(241001))}},
				{Col1stPlace: &comparator.String{Eq: new("quotedcol-silver")}},
			}},
			want: []int64{ids[0], ids[1]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.GetMany(ctx, &models.GetDigitLeadingColumnsInput{Filter: tt.filter})
			if err != nil {
				t.Fatalf("GetMany: %v", err)
			}
			gotIDs := make([]int64, len(got))
			for i, r := range got {
				gotIDs[i] = r.ID
			}
			slices.Sort(gotIDs)
			if diff := cmp.Diff(tt.want, gotIDs); diff != "" {
				t.Errorf("GetMany IDs mismatch (-want +got):\n%s", diff)
			}

			n, err := client.Count(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if n != int64(len(tt.want)) {
				t.Errorf("Count = %d, want %d", n, len(tt.want))
			}

			ok, err := client.ExistsWhere(ctx, tt.filter)
			if err != nil {
				t.Fatalf("ExistsWhere: %v", err)
			}
			if !ok {
				t.Error("ExistsWhere = false, want true")
			}
		})
	}

	updated, err := client.UpdateWhere(ctx,
		&models.DigitLeadingColumnFilter{And: []*models.DigitLeadingColumnFilter{seeded, {Col2024Quota: &comparator.Number[int64]{Gte: new(int64(241002))}}}},
		&models.UpdateDigitLeadingColumnInput{Col1stPlace: omittable.Set("quotedcol-podium")})
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if len(updated) != 2 {
		t.Errorf("UpdateWhere updated %d rows, want 2", len(updated))
	}

	if err := client.HardDeleteWhere(ctx, &models.DigitLeadingColumnFilter{Col2024Quota: &comparator.Number[int64]{In: []int64{241001, 241002, 241003}}}); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}
	n, err := client.Count(ctx, seeded)
	if err != nil {
		t.Fatalf("Count after HardDeleteWhere: %v", err)
	}
	if n != 0 {
		t.Errorf("Count after HardDeleteWhere = %d, want 0", n)
	}
}
