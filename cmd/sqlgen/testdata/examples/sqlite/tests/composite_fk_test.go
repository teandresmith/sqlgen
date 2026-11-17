package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// seedBins fills bins and bin_items. Bins share an aisle or a slot, so a load or
// filter that matched one key column alone would cross bins; i5 and i6 hold a
// NULL member and reference no bin.
func seedBins(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	wipe := func() {
		for _, table := range []string{"bin_items", "bins"} {
			if _, err := testDB.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Fatalf("clear %s: %v", table, err)
			}
		}
	}
	wipe()
	t.Cleanup(wipe)
	for _, b := range []struct {
		aisle string
		slot  int
		label string
	}{{"A", 1, "A1"}, {"A", 2, "A2"}, {"B", 1, "B1"}} {
		if _, err := testDB.ExecContext(ctx, "INSERT INTO bins (aisle, slot, label) VALUES (?, ?, ?)", b.aisle, b.slot, b.label); err != nil {
			t.Fatalf("seed bin %s: %v", b.label, err)
		}
	}
	for _, it := range []struct {
		name  string
		aisle any
		slot  any
	}{{"i1", "A", 1}, {"i2", "A", 1}, {"i3", "A", 2}, {"i4", "B", 1}, {"i5", "A", nil}, {"i6", nil, nil}} {
		if _, err := testDB.ExecContext(ctx, "INSERT INTO bin_items (aisle, slot, name) VALUES (?, ?, ?)", it.aisle, it.slot, it.name); err != nil {
			t.Fatalf("seed bin item %s: %v", it.name, err)
		}
	}
}

// A composite-key O2M load maps each child to the parent matching every key
// column, with neither key column selected.
func TestCompositeFK_LoadMatchesEveryKeyColumn(t *testing.T) {
	seedBins(t)
	rows, err := newClient().Bins().GetMany(context.Background(), &models.GetBinsInput{}, func(o *models.CallOptions[models.BinFieldOptions]) {
		o.FieldOptions = &models.BinFieldOptions{Label: true, BinItems: &models.BinItemRelationshipOptions{
			FieldOptions: &models.BinItemFieldOptions{Name: true},
		}}
	})
	if err != nil {
		t.Fatalf("Bins().GetMany: %v", err)
	}
	got := map[string][]string{}
	for _, b := range rows {
		names := []string{}
		for _, it := range b.BinItems {
			names = append(names, it.Name)
		}
		slices.Sort(names)
		got[b.Label] = names
	}
	want := map[string][]string{"A1": {"i1", "i2"}, "A2": {"i3"}, "B1": {"i4"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("loaded bin items (-want +got):\n%s", diff)
	}
}

// A composite-key relationship filter matches the parent on every key column,
// and a child with a NULL member matches no parent.
func TestCompositeFK_RelationshipFilterMatchesEveryKeyColumn(t *testing.T) {
	seedBins(t)
	for _, tt := range []struct {
		item string
		want []string
	}{
		{"i3", []string{"A2"}},
		{"i4", []string{"B1"}},
		{"i5", []string{}},
	} {
		t.Run(tt.item, func(t *testing.T) {
			rows, err := newClient().Bins().GetMany(context.Background(), &models.GetBinsInput{Filter: &models.BinFilter{
				BinItems: &models.BinItemFilter{Name: &comparator.String{Eq: new(tt.item)}},
			}})
			if err != nil {
				t.Fatalf("Bins().GetMany: %v", err)
			}
			got := []string{}
			for _, b := range rows {
				got = append(got, b.Label)
			}
			slices.Sort(got)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("bins (-want +got):\n%s", diff)
			}
		})
	}
}
