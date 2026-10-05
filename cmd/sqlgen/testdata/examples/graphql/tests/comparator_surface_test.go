package tests

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// The completed comparator surface, over the real gqlgen server. Two things
// here were once unreachable:
//
//   - `DecimalComparator` with ordered operators. Previously a decimal
//     column advertised `StringComparator`, which has no gt/gte/lt/lte, so
//     `price: { gt: "100.50" }` was inexpressible over GraphQL at all.
//   - `isNull` on the `Nullable<X>Comparator` twin (PRD §26.4 Rule 2).
//     Previously no comparator input exposed the model's `Null *bool`, so
//     IS NULL / IS NOT NULL was unreachable from the API on every nullable
//     column.

// seedDecimalProducts creates four products whose prices are chosen so the
// numeric and lexicographic orderings DISAGREE — "1000.00" sorts below
// "100.50" as text but above it as a number. Every name carries namePrefix so
// the caller can scope its assertions with a startsWith filter.
func seedDecimalProducts(t *testing.T, namePrefix string) {
	t.Helper()
	cat, err := testClient.Categories().Create(ctx(), &models.CreateCategoryInput{
		Name: namePrefix + "-category",
	})
	if err != nil {
		t.Fatalf("seeding category: %v", err)
	}
	for _, p := range []struct {
		name  string
		price string
	}{
		{namePrefix + "-a", "9.99"},
		{namePrefix + "-b", "100.50"},
		{namePrefix + "-c", "100.51"},
		{namePrefix + "-d", "1000.00"},
	} {
		price, err := decimal.NewFromString(p.price)
		if err != nil {
			t.Fatalf("parsing %q: %v", p.price, err)
		}
		if _, err := testClient.Products().Create(ctx(), &models.CreateProductInput{
			Name:       p.name,
			Price:      price,
			CategoryID: cat.ID,
			Stock:      omittable.Set(int32(1)),
		}); err != nil {
			t.Fatalf("seeding product %s: %v", p.name, err)
		}
	}
}

// TestDecimalComparator_OverHTTP pins this end to end: a decimal column is
// filterable with ordered operators over GraphQL, and the comparison is
// numeric because the Decimal-typed operand is compared against a NUMERIC
// column by the database — not collated as text, which is what the model
// side's comparator.String would otherwise imply.
func TestDecimalComparator_OverHTTP(t *testing.T) {
	// `categories.name` is UNIQUE, so seeding a fixed name twice in one
	// process fails during setup rather than at an assertion — which is
	// exactly what `-count=2` does when someone is chasing a flake. Reset
	// first, the way every other clean-slate test in this package does.
	truncateAll(t)

	const prefix = "gql-decimal"
	seedDecimalProducts(t, prefix)

	tests := []struct {
		name      string
		predicate string
		want      []string
	}{
		{
			name:      "gt excludes the boundary and keeps the longer numeral",
			predicate: `price: { gt: "100.50" }`,
			want:      []string{prefix + "-c", prefix + "-d"},
		},
		{
			name:      "lte keeps only the numerically smaller rows",
			predicate: `price: { lte: "100.50" }`,
			want:      []string{prefix + "-a", prefix + "-b"},
		},
		{
			name:      "in matches on the canonical string form",
			predicate: `price: { in: ["9.99", "1000.00"] }`,
			want:      []string{prefix + "-a", prefix + "-d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := `query {
				productList(
					filter: { name: { startsWith: "` + prefix + `" }, ` + tt.predicate + ` }
					sort: [{ field: NAME, direction: ASC }]
				) { items { name price } }
			}`
			got := productListNames(t, q)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNullableComparator_IsNullOverHTTP covers the half that was flatly
// unreachable: `isNull` appears only on the Nullable twin, and both
// polarities reach the model's `Null *bool`.
func TestNullableComparator_IsNullOverHTTP(t *testing.T) {
	truncateAll(t) // see the note in TestDecimalComparator_OverHTTP

	const prefix = "gql-isnull"
	cat, err := testClient.Categories().Create(ctx(), &models.CreateCategoryInput{Name: prefix + "-category"})
	if err != nil {
		t.Fatalf("seeding category: %v", err)
	}
	for _, p := range []struct {
		name        string
		description *string
	}{
		{prefix + "-described", new("has a description")},
		{prefix + "-blank", nil},
	} {
		in := &models.CreateProductInput{
			Name:       p.name,
			Price:      decimal.NewFromInt(1),
			CategoryID: cat.ID,
			Stock:      omittable.Set(int32(1)),
		}
		if p.description != nil {
			in.Description = omittable.Set(p.description)
		}
		if _, err := testClient.Products().Create(ctx(), in); err != nil {
			t.Fatalf("seeding product %s: %v", p.name, err)
		}
	}

	tests := []struct {
		name      string
		predicate string
		want      []string
	}{
		{
			name:      "isNull true selects the rows with no value",
			predicate: `description: { isNull: true }`,
			want:      []string{prefix + "-blank"},
		},
		{
			name:      "isNull false selects the rows that have one",
			predicate: `description: { isNull: false }`,
			want:      []string{prefix + "-described"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := `query {
				productList(
					filter: { name: { startsWith: "` + prefix + `" }, ` + tt.predicate + ` }
					sort: [{ field: NAME, direction: ASC }]
				) { items { name } }
			}`
			got := productListNames(t, q)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered names mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// The NOT NULL half of Rule 2: `isNull` exists only on the nullable
	// twin, so offering it on a NOT NULL column's comparator is a schema
	// validation error — rejected before any resolver runs.
	gqlExpectValidationError(t,
		`query { productList(filter: { name: { isNull: true } }) { items { id } } }`,
		`"isNull" is not defined`)
}

// productListNames runs a productList query and returns the item names.
func productListNames(t *testing.T, query string) []string {
	t.Helper()
	resp := gqlExec(t, query, nil, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("query errored: %+v", resp.Errors)
	}
	var out struct {
		ProductList struct {
			Items []struct {
				Name  string `json:"name"`
				Price string `json:"price"`
			} `json:"items"`
		} `json:"productList"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decoding productList response: %v", err)
	}
	names := make([]string, 0, len(out.ProductList.Items))
	for _, it := range out.ProductList.Items {
		names = append(names, it.Name)
	}
	return names
}
