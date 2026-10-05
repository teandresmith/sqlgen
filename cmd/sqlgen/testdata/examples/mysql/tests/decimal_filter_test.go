package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// TestDecimalFilter_OrderedOperatorsAreNumeric is a regression pin
// (PRD §26.4 "Decimal columns", §11.2).
//
// `decimal.Decimal` does not satisfy `comparator.Numeric`, so a decimal
// column filters through `comparator.String` on its canonical string form.
// That is only safe because the comparison happens in the database against a
// NUMERIC column, which coerces the parameter — the ordering is numeric, not
// the string collation the Go type would suggest.
//
// The fixture is chosen so the two orderings DISAGREE: lexicographically
// "1000.00" < "100.50", numerically 1000.00 > 100.50. A string comparison
// would drop the 1000.00 row from a `gt: 100.50` filter and include it in
// `lte: 100.50`. Both directions are asserted.
func TestDecimalFilter_OrderedOperatorsAreNumeric(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prefix := "decimal-order-" + ksuid.New().String()
	seed := []struct {
		name  string
		price string
	}{
		{name: prefix + "-a", price: "9.99"},
		{name: prefix + "-b", price: "100.50"},
		{name: prefix + "-c", price: "100.51"},
		{name: prefix + "-d", price: "1000.00"},
	}
	for _, s := range seed {
		price, err := decimal.NewFromString(s.price)
		if err != nil {
			t.Fatalf("parsing %q: %v", s.price, err)
		}
		if _, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
			ExternalID: ksuid.New(),
			Name:       s.name,
			Price:      price,
		}); err != nil {
			t.Fatalf("seeding warehouse %s: %v", s.name, err)
		}
	}

	tests := []struct {
		name   string
		filter *comparator.String
		want   []string
	}{
		{
			name:   "gt excludes the boundary and keeps the longer numeral",
			filter: &comparator.String{Gt: new("100.50")},
			want:   []string{prefix + "-c", prefix + "-d"},
		},
		{
			name:   "gte includes the boundary",
			filter: &comparator.String{Gte: new("100.50")},
			want:   []string{prefix + "-b", prefix + "-c", prefix + "-d"},
		},
		{
			name:   "lte keeps only the numerically smaller rows",
			filter: &comparator.String{Lte: new("100.50")},
			want:   []string{prefix + "-a", prefix + "-b"},
		},
		{
			name:   "lt excludes the boundary",
			filter: &comparator.String{Lt: new("100.50")},
			want:   []string{prefix + "-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
				Filter: &models.WarehouseFilter{
					Name:  &comparator.String{StartsWith: new(prefix)},
					Price: tt.filter,
				},
				Sorts: []sql.Sort{{Column: "name", Direction: sql.Asc}},
			})
			if err != nil {
				t.Fatalf("GetMany: %v", err)
			}
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
