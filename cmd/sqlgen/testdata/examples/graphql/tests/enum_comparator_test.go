package tests

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Enum comparators over the real gqlgen server.
//
// `documents.entityType` once advertised `StringComparator`. That input has no
// translator entry behind an enum column, so the model filter received nil: the
// server answered every `{entityType: {eq: …}}` query with the UNFILTERED set
// while the client believed it had filtered. These tests pin both halves of the
// fix — the filter now narrows, and an unknown member is rejected by the
// GraphQL parser rather than reaching a resolver.

// seedEnumDocuments creates one asset and four documents, one per member of
// document_entity_type_enum, all named with namePrefix so assertions scope
// themselves with a startsWith filter.
func seedEnumDocuments(t *testing.T, namePrefix string) string {
	t.Helper()

	var assetOut struct {
		CreateAsset struct {
			ID string `json:"id"`
		} `json:"createAsset"`
	}
	gqlExecData(t, `
		mutation SeedAsset($name: String!, $createdAt: Time!) {
			createAsset(input: { name: $name, declaredCategories: [ASSETPRIMARY], createdAt: $createdAt }) { id }
		}
	`, map[string]any{"name": namePrefix + "-asset", "createdAt": fixedTimestamp}, &assetOut)
	if assetOut.CreateAsset.ID == "" {
		t.Fatal("seeding asset: empty ID")
	}

	for _, d := range []struct{ suffix, enum string }{
		{"-primary", "ASSETPRIMARY"},
		{"-attachment", "ASSETATTACHMENT"},
		{"-invoice", "ASSETINVOICE"},
		{"-spv", "SPV"},
	} {
		var out struct {
			CreateDocument struct {
				ID string `json:"id"`
			} `json:"createDocument"`
		}
		gqlExecData(t, `
			mutation SeedDoc($entityID: UUID!, $entityType: DocumentEntityTypeEnum!, $name: String!, $createdAt: Time!) {
				createDocument(input: { entityID: $entityID, entityType: $entityType, name: $name, createdAt: $createdAt }) { id }
			}
		`, map[string]any{
			"entityID":   assetOut.CreateAsset.ID,
			"entityType": d.enum,
			"name":       namePrefix + d.suffix,
			"createdAt":  fixedTimestamp,
		}, &out)
		if out.CreateDocument.ID == "" {
			t.Fatalf("seeding document %s: empty ID", d.suffix)
		}
	}
	return assetOut.CreateAsset.ID
}

// documentListNames runs a documentList query and returns the item names.
func documentListNames(t *testing.T, query string) []string {
	t.Helper()
	resp := gqlExec(t, query, nil, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("query errored: %+v", resp.Errors)
	}
	var out struct {
		DocumentList struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		} `json:"documentList"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decoding documentList response: %v", err)
	}
	names := make([]string, 0, len(out.DocumentList.Items))
	for _, it := range out.DocumentList.Items {
		names = append(names, it.Name)
	}
	return names
}

// TestEnumComparator_NarrowsOverHTTP is the unfiltered-answer regression pin.
// Every case selects a strict subset of the four seeded rows, so the old
// behaviour (the whole set, unfiltered) fails each one.
func TestEnumComparator_NarrowsOverHTTP(t *testing.T) {
	truncateAll(t)

	const prefix = "gql-enum"
	seedEnumDocuments(t, prefix)

	tests := []struct {
		name      string
		predicate string
		want      []string
	}{
		{
			name:      "eq selects one member",
			predicate: `entityType: { eq: SPV }`,
			want:      []string{prefix + "-spv"},
		},
		{
			name:      "neq excludes one member",
			predicate: `entityType: { neq: SPV }`,
			want:      []string{prefix + "-attachment", prefix + "-invoice", prefix + "-primary"},
		},
		{
			name:      "in selects a set",
			predicate: `entityType: { in: [ASSETPRIMARY, SPV] }`,
			want:      []string{prefix + "-primary", prefix + "-spv"},
		},
		{
			name:      "nin excludes a set",
			predicate: `entityType: { nin: [ASSETPRIMARY, SPV] }`,
			want:      []string{prefix + "-attachment", prefix + "-invoice"},
		},
		// The recursion path: an enum comparator nested inside `and` / `or`
		// reaches the same translator, since the per-table filter translator
		// recurses into itself for sub-filters (PRD §26.5.3).
		{
			name: "and nesting",
			predicate: `and: [
				{ entityType: { neq: SPV } },
				{ entityType: { neq: ASSETPRIMARY } }
			]`,
			want: []string{prefix + "-attachment", prefix + "-invoice"},
		},
		{
			// One branch per `or` entry: entries are OR'd together, and an
			// entry's own fields are AND'd (PRD §11.1). Putting both branches
			// on a single entry would ask for the intersection instead, which
			// is empty here.
			name: "or nesting",
			predicate: `or: [
				{ entityType: { eq: SPV } },
				{ name: { endsWith: "-invoice" } }
			]`,
			want: []string{prefix + "-invoice", prefix + "-spv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := `query {
				documentList(
					filter: { name: { startsWith: "` + prefix + `" }, ` + tt.predicate + ` }
					sort: [{ field: NAME, direction: ASC }]
				) { items { name } }
			}`
			got := documentListNames(t, q)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnumComparator_RejectsUnknownMember is the schema-level-validation
// benefit that motivated enum-typed comparators over runtime parsing: with
// enum-typed operands, a bad member never reaches a resolver. Under the old
// StringComparator fallback both queries below parsed cleanly and returned 200
// with the unfiltered set.
func TestEnumComparator_RejectsUnknownMember(t *testing.T) {
	gqlExpectValidationError(t,
		`query { documentList(filter: { entityType: { eq: BANANA } }) { items { id } } }`,
		`BANANA`)

	// The SQL literal as a string is rejected too — the operand's type is the
	// enum, not String.
	gqlExpectValidationError(t,
		`query { documentList(filter: { entityType: { eq: "asset.primary" } }) { items { id } } }`,
		`DocumentEntityTypeEnum`)

	// `isNull` belongs to the Nullable twin (PRD §26.4 Rule 2), and
	// documents.entity_type is NOT NULL.
	gqlExpectValidationError(t,
		`query { documentList(filter: { entityType: { isNull: true } }) { items { id } } }`,
		`"isNull" is not defined`)
}

// TestNullableEnumComparator_OverHTTP covers Rule 2's twin on an enum column:
// `assets.primary_category` is nullable, so its comparator carries `isNull`
// and both polarities reach the model's `Null *bool`.
func TestNullableEnumComparator_OverHTTP(t *testing.T) {
	truncateAll(t)

	const prefix = "gql-nullenum"
	for _, a := range []struct {
		suffix   string
		category any
	}{
		{"-set", "ASSETINVOICE"},
		{"-unset", nil},
	} {
		var out struct {
			CreateAsset struct {
				ID string `json:"id"`
			} `json:"createAsset"`
		}
		gqlExecData(t, `
			mutation SeedAsset($name: String!, $category: DocumentEntityTypeEnum, $createdAt: Time!) {
				createAsset(input: {
					name: $name,
					declaredCategories: [],
					primaryCategory: $category,
					createdAt: $createdAt,
				}) { id }
			}
		`, map[string]any{"name": prefix + a.suffix, "category": a.category, "createdAt": fixedTimestamp}, &out)
		if out.CreateAsset.ID == "" {
			t.Fatalf("seeding asset %s: empty ID", a.suffix)
		}
	}

	tests := []struct {
		name      string
		predicate string
		want      []string
	}{
		{
			name:      "isNull true selects the unset row",
			predicate: `primaryCategory: { isNull: true }`,
			want:      []string{prefix + "-unset"},
		},
		{
			name:      "isNull false selects the row that has a value",
			predicate: `primaryCategory: { isNull: false }`,
			want:      []string{prefix + "-set"},
		},
		{
			name:      "eq narrows on the nullable twin too",
			predicate: `primaryCategory: { eq: ASSETINVOICE }`,
			want:      []string{prefix + "-set"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := `query {
				assetList(
					filter: { name: { startsWith: "` + prefix + `" }, ` + tt.predicate + ` }
					sort: [{ field: NAME, direction: ASC }]
				) { items { name } }
			}`
			resp := gqlExec(t, q, nil, nil)
			if len(resp.Errors) != 0 {
				t.Fatalf("query errored: %+v", resp.Errors)
			}
			var out struct {
				AssetList struct {
					Items []struct {
						Name string `json:"name"`
					} `json:"items"`
				} `json:"assetList"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				t.Fatalf("decoding assetList response: %v", err)
			}
			got := make([]string, 0, len(out.AssetList.Items))
			for _, it := range out.AssetList.Items {
				got = append(got, it.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
