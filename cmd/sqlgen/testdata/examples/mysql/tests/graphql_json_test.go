package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// The JSON comparator family over the real gqlgen server, against
// a real MySQL.
//
// This is the dialect half of the family. `comparator.JSON.Parse` switches on
// the dialect, so one `JSONComparator` input compiles to
// `attributes::jsonb @> $` on PostgreSQL and `JSON_CONTAINS(attributes, ?)` /
// `JSON_CONTAINS_PATH(attributes, 'one', ?)` here — and only a MySQL server
// proves the second branch runs. The PostgreSQL branch is pinned by the
// graphql example's TestJSONComparator_NarrowsOverHTTP.
//
// It is also the only place the `NOT NULL` form of the family earns a golden:
// `products.attributes` is `JSON NOT NULL`, while every JSON-ish column in the
// PostgreSQL example is nullable.
//
// `hasKey`'s operand is where the two dialects genuinely diverge and the shared
// input does not paper over it: the cases below pass `$.region` because MySQL
// wants a JSON path, where PostgreSQL's `?` wants the bare key (PRD §11.2).
//
// The document reaching the driver as TEXT rather than as raw bytes is
// load-bearing here in a way it is not on PostgreSQL. `types.JSON` is a
// `[]byte`, and a Go []byte parameter is sent as a BINARY value — MySQL then
// rejects `JSON_CONTAINS(col, _binary'…')` with "Cannot create a JSON value
// from a string with CHARACTER SET 'binary'", so every `contains` filter would
// fail at request time. These cases are what pin the conversion.

// seedJSONProducts creates one category and three products whose SKUs share a
// prefix, so every filter below can scope itself without a truncate — this
// module shares one MySQL container across suites.
func seedJSONProducts(t *testing.T) (gold1, gold2, silver int64) {
	t.Helper()
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "GQLJSONCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	mk := func(sku string, attrs map[string]any) int64 {
		var out struct {
			CreateProduct struct {
				ID int64 `json:"id"`
			} `json:"createProduct"`
		}
		gqlExecData(t, `
			mutation Seed($categoryID: Int!, $sku: String!, $attributes: JSON!) {
				createProduct(input: {
					categoryID: $categoryID, title: "gql-json probe", price: 1.0,
					inStock: true, sku: $sku, attributes: $attributes,
					createdAt: "2026-01-01T00:00:00Z"
				}) { id }
			}
		`, map[string]any{"categoryID": cat.ID, "sku": sku, "attributes": attrs}, &out)
		if out.CreateProduct.ID == 0 {
			t.Fatalf("seeding %s: empty ID", sku)
		}
		id := out.CreateProduct.ID
		t.Cleanup(func() { _ = client.Products().HardDelete(ctx, id) })
		return id
	}

	gold1 = mk("GQLJSON-1", map[string]any{"tier": "gold", "region": "us"})
	gold2 = mk("GQLJSON-2", map[string]any{"tier": "gold", "region": "eu"})
	silver = mk("GQLJSON-3", map[string]any{"tier": "silver"})
	return gold1, gold2, silver
}

// jsonProductIDs runs productList scoped to the seeded SKU prefix and returns
// the matched ids, sorted.
func jsonProductIDs(t *testing.T, varDefs, jsonFilter string, vars map[string]any) []int64 {
	t.Helper()
	var out struct {
		ProductList struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		} `json:"productList"`
	}
	gqlExecData(t, `
		query Probe`+varDefs+` {
			productList(filter: {sku: {startsWith: "GQLJSON-"}, attributes: `+jsonFilter+`}) {
				items { id }
			}
		}
	`, vars, &out)
	ids := make([]int64, 0, len(out.ProductList.Items))
	for _, it := range out.ProductList.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	return ids
}

func sortedIDs(ids ...int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// TestGraphQLJSONComparator_NarrowsOverHTTP exercises both operators the JSON
// family projects. Without the JSON family, `attributes` advertised `StringComparator` with
// no translator behind it, so each of these queries returned all three rows.
func TestGraphQLJSONComparator_NarrowsOverHTTP(t *testing.T) {
	gold1, gold2, silver := seedJSONProducts(t)

	// The unfiltered baseline every case below must differ from — it is
	// exactly what a missing translator returns.
	if got, want := jsonProductIDs(t, "", `{}`, nil), sortedIDs(gold1, gold2, silver); !cmp.Equal(got, want) {
		t.Fatalf("unfiltered baseline mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}

	tests := []struct {
		name    string
		varDefs string
		filter  string
		vars    map[string]any
		want    []int64
	}{
		{
			// JSON_CONTAINS(attributes, '{"tier":"gold"}') — containment, so
			// the two gold rows match regardless of their other keys.
			name:    "contains",
			varDefs: `($v: JSON)`,
			filter:  `{contains: $v}`,
			vars:    map[string]any{"v": map[string]any{"tier": "gold"}},
			want:    sortedIDs(gold1, gold2),
		},
		{
			// JSON_CONTAINS_PATH(attributes, 'one', '$.region'). MySQL's
			// operand is a JSON PATH where PostgreSQL's `?` takes a bare key
			// — that difference lives in comparator.JSON.Parse, and the one
			// GraphQL input serves both.
			name:   "hasKey",
			filter: `{hasKey: "$.region"}`,
			want:   sortedIDs(gold1, gold2),
		},
		{
			// A path no row carries, so the answer is empty rather than a
			// subset — the case a translator that silently drops the operand
			// cannot produce.
			name:   "hasKey with no matches",
			filter: `{hasKey: "$.absent"}`,
			want:   []int64{},
		},
		{
			// Both operators in one comparator, which is the AND the model
			// filter builds from a single struct.
			name:    "contains and hasKey together",
			varDefs: `($v: JSON)`,
			filter:  `{contains: $v, hasKey: "$.region"}`,
			vars:    map[string]any{"v": map[string]any{"region": "us"}},
			want:    sortedIDs(gold1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonProductIDs(t, tt.varDefs, tt.filter, tt.vars)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tt.filter, diff)
			}
		})
	}
}

// TestGraphQLJSONComparator_SchemaShape pins the two schema-level facts a
// MySQL project depends on: the PostgreSQL-only JSONB family is absent, and
// the NOT NULL form of the JSON family carries no `isNull` (PRD §26.4 Rule 2 —
// `products.attributes` is `JSON NOT NULL`).
func TestGraphQLJSONComparator_SchemaShape(t *testing.T) {
	resp := gqlExec(t, `
		query { productList(filter: {attributes: {isNull: true}}) { items { id } } }
	`, nil)
	if len(resp.Errors) == 0 {
		t.Error("isNull was accepted on a NOT NULL JSON column")
	}

	// The JSONB operators do not exist on this dialect at all: `hasAnyKey`
	// belongs to JSONBComparator, which no MySQL schema declares.
	resp = gqlExec(t, `
		query { productList(filter: {attributes: {hasAnyKey: ["tier"]}}) { items { id } } }
	`, nil)
	if len(resp.Errors) == 0 {
		t.Error("a JSONB operator was accepted on MySQL")
	}
}
