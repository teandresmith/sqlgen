package tests

// SQLite arm: a relationship filter over an edge that declares
// `filter:` must reach the database with the subquery's real alias, not the
// codegen placeholder.
//
// The gap this closes is specific. The example tests exercised relationship
// *loading* on every dialect — `AssetFieldOptions{PhotoAttachments: ...}` —
// but relationship *filters* only on postgres, and the two paths are different
// code: the loader applies a `filter:` verbatim to a single-table query and
// never qualifies, while the filter path qualifies it into a correlated EXISTS.
// Only the second was broken, so everything compiled and every existing test
// passed while the shipped SQLite client could not run this query at all:
//
//	get assets: query: SQL logic error: no such column: sqlgenrel.entity_type (1)
//
// Golden tests could not catch it either — they compare generated text against
// a recorded copy of the same generated text, so a wrong alias is recorded as
// faithfully as a right one. Only a real query does.

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestRelationshipFilter_StaticFilterEdge filters assets by a predicate on a
// child reached through a `filter:` edge — the shape that failed outright.
//
// PhotoAttachments carries `filter: "entity_type = 'asset.attachment' AND name
// LIKE 'photo_%'"`, so the EXISTS must AND that predicate in alongside the
// caller's. Both halves are asserted: the sibling sub-category and the
// same-sub-category non-match are each excluded, which is what makes this a
// test of the static predicate rather than only of the correlation.
func TestRelationshipFilter_StaticFilterEdge(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	newAsset := func(name string) *models.Asset {
		t.Helper()
		a, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: name})
		if err != nil {
			t.Fatalf("Create asset %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, a.ID) })
		return a
	}

	addDoc := func(assetID int64, name, entityType string) {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   assetID,
			EntityType: entityType,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
	}

	// Matches: the document is an attachment, its name is photo_*, and it
	// carries the caller's predicate.
	hit := newAsset("StaticFilter Static Hit")
	addDoc(hit.ID, "photo_turbine.jpg", "asset.attachment")

	// Excluded by the edge's `filter:` on entity_type — the caller's predicate
	// matches, but the document belongs to a sibling sub-category.
	wrongType := newAsset("StaticFilter Wrong Subcategory")
	addDoc(wrongType.ID, "photo_turbine.jpg", "asset.primary")

	// Excluded by the edge's `filter:` on name — right sub-category, and it
	// satisfies the caller's predicate below, so only the edge's own
	// `name LIKE 'photo_%'` can rule it out. Without that half of the static
	// filter reaching the subquery, this asset comes back too.
	wrongName := newAsset("StaticFilter Wrong Prefix")
	addDoc(wrongName.ID, "link_turbine.jpg", "asset.attachment")

	// Contains, not Eq: the caller's predicate has to match both photo_ and
	// link_ documents, or it would exclude wrongName on its own and the case
	// would prove nothing about the edge's filter.
	got, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{
		Filter: &models.AssetFilter{
			PhotoAttachments: &models.DocumentFilter{
				Name: &comparator.String{Contains: new("turbine")},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with a relationship filter on a `filter:` edge: %v", err)
	}

	want := []int64{hit.ID}
	gotIDs := make([]int64, 0, len(got))
	for _, a := range got {
		// Other tests share this table, so narrow to the rows this one seeded.
		switch a.ID {
		case hit.ID, wrongType.ID, wrongName.ID:
			gotIDs = append(gotIDs, a.ID)
		}
	}
	if diff := cmp.Diff(want, gotIDs, cmpopts.SortSlices(int64Less)); diff != "" {
		t.Errorf("relationship filter on a `filter:` edge mismatch (-want +got):\n%s", diff)
	}
}

// TestRelationshipFilter_StaticFilterEdgeM2M is the junction shape of the same
// thing. LinkedAttachments reaches documents through asset_document_links, so
// the subquery has the junction in scope as well as the target — which is why
// the static predicate is qualified at all, and therefore why it had a
// placeholder to get wrong.
func TestRelationshipFilter_StaticFilterEdgeM2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "StaticFilter M2M Static"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	link := func(name, entityType string) {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: entityType,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })

		if _, err := client.AssetDocumentLinks().Create(ctx, &models.CreateAssetDocumentLinkInput{
			AssetID:    asset.ID,
			DocumentID: doc.ID,
		}); err != nil {
			t.Fatalf("Create asset_document_link for %q: %v", name, err)
		}
		t.Cleanup(func() {
			_ = client.AssetDocumentLinks().HardDelete(ctx, models.AssetDocumentLinkPK{
				AssetID:    asset.ID,
				DocumentID: doc.ID,
			})
		})
	}

	link("link_wiring.pdf", "asset.attachment")

	got, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{
		Filter: &models.AssetFilter{
			LinkedAttachments: &models.DocumentFilter{
				Name: &comparator.String{Eq: new("link_wiring.pdf")},
			},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with an M2M relationship filter on a `filter:` edge: %v", err)
	}

	var found bool
	for _, a := range got {
		if a.ID == asset.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("M2M relationship filter on a `filter:` edge returned %d assets, none of them the linked one", len(got))
	}
}
