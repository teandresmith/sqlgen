package sqlgenresolver

import (
	"testing"

	"github.com/99designs/gqlgen/graphql"
)

// TestRowIdentity_EvaluatedOnceOverTheAliasUnion pins PRD §26.5.2: "Row
// identity is evaluated once against the unioned result, so an alias that
// names no column cannot add identity columns to a sibling alias that does."
// It drives the generated walker, the code a request runs, over two aliases of
// the `orders` relationship. Identity is projected only when the union of
// every alias names no column.
func TestRowIdentity_EvaluatedOnceOverTheAliasUnion(t *testing.T) {
	tests := []struct {
		name      string
		in        []graphql.CollectedField
		wantID    bool
		wantTotal bool
	}{
		{
			name:      "typename-only alias before a column alias adds no identity",
			in:        []graphql.CollectedField{field("orders", "a", "__typename"), field("orders", "b", "total")},
			wantID:    false,
			wantTotal: true,
		},
		{
			name:      "column alias before a typename-only alias adds no identity",
			in:        []graphql.CollectedField{field("orders", "a", "total"), field("orders", "b", "__typename")},
			wantID:    false,
			wantTotal: true,
		},
		{
			name:      "every alias naming no column projects identity",
			in:        []graphql.CollectedField{field("orders", "a", "__typename"), field("orders", "b", "__typename")},
			wantID:    true,
			wantTotal: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fo := userFieldOptionsFromCollected(&graphql.OperationContext{}, tt.in)
			if fo.Orders == nil || fo.Orders.FieldOptions == nil {
				t.Fatalf("Orders = %+v, want the relationship selected", fo.Orders)
			}
			got := fo.Orders.FieldOptions
			if got.ID != tt.wantID {
				t.Errorf("Orders.ID = %v, want %v", got.ID, tt.wantID)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("Orders.Total = %v, want %v", got.Total, tt.wantTotal)
			}
		})
	}
}
