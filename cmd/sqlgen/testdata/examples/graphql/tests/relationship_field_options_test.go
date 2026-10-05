package tests

import (
	"testing"
	"uuid"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	"github.com/teandresmith/sqlgen/comparator"
)

// Regression coverage. The O2M loader used to force the child's FK into
// the projection by writing `relFieldOptions.<FK> = true` into the caller's
// FieldOptions from inside its errgroup closure. `assets` is the fixture that
// makes both consequences reachable: four relationships (three O2M, one M2M)
// all target `documents`, so one *DocumentFieldOptions can legally serve all
// four, and the loaders then race on one bool while leaving it set for every
// later read that reuses the pointer. The requirement now rides the child
// fetch's requiredColumns instead, which is where the M2M half already put it.

// seedSharedTargetAsset creates one asset plus one document per discriminator
// the asset's relationships sub-categorize on, and links one of them through
// the M2M junction. Returns the asset's ID.
func seedSharedTargetAsset(t *testing.T) uuid.UUID {
	t.Helper()
	truncateAll(t)

	asset, err := testClient.Assets().Create(ctx(), &models.CreateAssetInput{Name: "shared-target-asset"})
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	mk := func(name string, kind models.DocumentEntityTypeEnum) *models.Document {
		t.Helper()
		d, err := testClient.Documents().Create(ctx(), &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: kind,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("create document %s: %v", name, err)
		}
		return d
	}
	// `photo_a` matches both Attachments and PhotoAttachments (the latter adds
	// a `name LIKE 'photo_%'` predicate), so Attachments returns two rows.
	mk("site_a", "asset.primary")
	mk("photo_a", "asset.attachment")
	mk("inv_a", "asset.invoice")
	linked := mk("link_a", "asset.attachment")
	if _, err := testPool.Exec(ctx(),
		`INSERT INTO asset_document_links (asset_id, document_id) VALUES ($1, $2)`,
		asset.ID, linked.ID); err != nil {
		t.Fatalf("link asset to document: %v", err)
	}
	return asset.ID
}

// TestRelationshipFieldOptions_SharedPointerIsNeitherRacedNorMutated shares one
// *DocumentFieldOptions across every relationship that targets `documents` —
// three O2M edges and one M2M edge, each loaded on its own goroutine. Under
// -race the pre-fix tree reported write/write on the FK bool between the
// Attachments and Invoices closures; it also left EntityID set on the caller's
// value, silently widening every later read through the same pointer.
func TestRelationshipFieldOptions_SharedPointerIsNeitherRacedNorMutated(t *testing.T) {
	assetID := seedSharedTargetAsset(t)

	shared := &models.DocumentFieldOptions{ID: true, Name: true}
	assets, err := testClient.Assets().GetMany(ctx(), &models.GetAssetsInput{
		Filter: &models.AssetFilter{ID: &comparator.ID{Eq: new(assetID.String())}},
	}, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:                true,
			Name:              true,
			Attachments:       &models.DocumentRelationshipOptions{FieldOptions: shared},
			Invoices:          &models.DocumentRelationshipOptions{FieldOptions: shared},
			PhotoAttachments:  &models.DocumentRelationshipOptions{FieldOptions: shared},
			LinkedAttachments: &models.DocumentRelationshipOptions{FieldOptions: shared},
		}
	})
	if err != nil {
		t.Fatalf("GetMany assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("GetMany returned %d assets, want 1", len(assets))
	}
	got := assets[0]

	// The FK still reaches the projection — the bucketing below it depends on
	// the value, so a wrong count here means requiredColumns did not arrive.
	for _, tc := range []struct {
		field string
		got   int
		want  int
	}{
		{"Attachments", len(got.Attachments), 2},
		{"Invoices", len(got.Invoices), 1},
		{"PhotoAttachments", len(got.PhotoAttachments), 1},
		{"LinkedAttachments", len(got.LinkedAttachments), 1},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %d documents, want %d", tc.field, tc.got, tc.want)
		}
	}

	if shared.EntityID {
		t.Errorf("loader wrote EntityID=true back into the caller's FieldOptions — " +
			"the flag persists into every later read that reuses the pointer")
	}
}

// TestRelationshipFieldOptions_AllFalseChildShortCircuitsOnBothPaths pins the
// behavior change the conversion carries, from the Go client rather than over
// GraphQL: an all-false child FieldOptions means "the caller wants no rows"
// (PRD §9.6), and both list loaders must honor it identically. The pre-fix O2M
// path could not — the force-select made HasSelectedColumns() true, so the
// child fetch ran and returned rows carrying nothing but the FK, while the M2M
// sibling on the same call returned nil.
//
// No GraphQL request reaches this: the walker projects a row's identity for any
// selection set that names no column, which
// TestRowIdentity_NestedO2MTypenameOnlyStillReturnsTheChildren guards.
func TestRelationshipFieldOptions_AllFalseChildShortCircuitsOnBothPaths(t *testing.T) {
	assetID := seedSharedTargetAsset(t)

	assets, err := testClient.Assets().GetMany(ctx(), &models.GetAssetsInput{
		Filter: &models.AssetFilter{ID: &comparator.ID{Eq: new(assetID.String())}},
	}, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:                true,
			Attachments:       &models.DocumentRelationshipOptions{FieldOptions: &models.DocumentFieldOptions{}},
			LinkedAttachments: &models.DocumentRelationshipOptions{FieldOptions: &models.DocumentFieldOptions{}},
		}
	})
	if err != nil {
		t.Fatalf("GetMany assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("GetMany returned %d assets, want 1", len(assets))
	}
	got := assets[0]

	if n := len(got.Attachments); n != 0 {
		t.Errorf("O2M Attachments: got %d documents, want 0 — an all-false child "+
			"FieldOptions must skip the child fetch (PRD §9.6)", n)
	}
	if n := len(got.LinkedAttachments); n != 0 {
		t.Errorf("M2M LinkedAttachments: got %d documents, want 0", n)
	}
}
