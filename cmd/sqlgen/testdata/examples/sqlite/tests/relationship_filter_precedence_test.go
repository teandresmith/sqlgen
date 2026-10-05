package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// A relationship `filter:` whose top-level operator is OR must not
// lose the FK correlation.
//
// Every `filter:` is AND-ed onto the predicate that scopes the load to one
// parent, and AND binds tighter than OR, so an unparenthesised splice reads as
// `(fk = parent AND left) OR right` — the right arm then matches rows belonging
// to any parent. The failure is silent: the loader hands back rows the caller
// did not ask for, with no error, and on a tenanted schema those rows can
// belong to another tenant.
//
// Both edges below collect two sub-categories under one relationship, which is
// what PRD §13.7.1 suggests for otherwise-stranded rows and the only filter
// shape where operator precedence is load-bearing.
//
// Every asset seeded here owns a row matching each edge, so the assertions turn
// on *which* rows come back rather than on whether any do. That keeps the pin
// off the o2o LEFT JOIN miss path, which TestO2OJoinMiss_ReadsAsNil covers on
// its own.

// TestRelationshipFilterPrecedence_O2MStaysCorrelated asserts the O2M edge
// returns only the requested asset's documents, with the OR's right arm
// (`entity_type = 'asset.invoice'`) satisfied by a document owned by a
// *different* asset.
//
// Unlike the O2O case this does **not** fail on the unparenthesised predicate,
// and the distinction is worth stating rather than discovering later. The
// loader over-fetches — the SQL really does return the other asset's invoice —
// but it then re-keys every child on its FK (`byFK[fmt.Sprint(asset.ID)]`), and
// a row belonging to an asset outside the result set is dropped there. For any
// row whose FK *is* in the set, `fk IN P AND (L OR R)` and
// `(fk IN P AND L) OR R` agree, so the two spellings are indistinguishable
// through this field. The re-keying is therefore load-bearing defence in depth,
// and that is what this test pins: remove it and the leak becomes observable.
//
// The predicate itself is pinned where it is actually decided, in
// sql.TestBuildSelect_RawConditionParenthesised. The difference becomes visible
// end-to-end only once the target carries a soft-delete or tenant predicate, so
// that a row satisfying the right arm can be one the other AND terms excluded;
// `documents` carries neither.
func TestRelationshipFilterPrecedence_O2MStaysCorrelated(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	mine, _ := seedPrecedenceFixture(t, client)

	loaded := loadPrecedenceAsset(t, ctx, client, mine)

	var got []string
	for _, d := range loaded.PrimaryOrInvoiceDocuments {
		got = append(got, d.Name)
	}
	want := []string{"mine_primary.pdf"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PrimaryOrInvoiceDocuments mismatch (-want +got):\n%s", diff)
	}
}

// TestRelationshipFilterPrecedence_O2OStaysCorrelated is the JOIN ON half. The
// o2o predicate lives in the ON clause rather than in a WHERE, so an unwrapped
// OR does not merely widen the row set — the LEFT JOIN also matches the other
// asset's archived document, which fans the parent row out. loadPrecedenceAsset
// asserts the row count, so this test fails on both symptoms.
func TestRelationshipFilterPrecedence_O2OStaysCorrelated(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	mine, other := seedPrecedenceFixture(t, client)

	for _, tt := range []struct {
		name  string
		asset *models.Asset
		want  string
	}{
		{name: "mine", asset: mine, want: "mine_archived.pdf"},
		{name: "other", asset: other, want: "other_archived.pdf"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loaded := loadPrecedenceAsset(t, ctx, client, tt.asset)
			if loaded.SpvOrArchivedDocument == nil {
				t.Fatalf("SpvOrArchivedDocument = nil, want %q", tt.want)
			}
			if got := loaded.SpvOrArchivedDocument.Name; got != tt.want {
				t.Errorf("SpvOrArchivedDocument.Name = %q, want %q — the document belongs to another asset", got, tt.want)
			}
		})
	}
}

// seedPrecedenceFixture builds two assets that each own a row matching each
// OR-filtered edge. Every row is therefore a candidate leak into the other
// asset, and none of the edges resolves to a JOIN miss.
func seedPrecedenceFixture(t *testing.T, client *models.Client) (*models.Asset, *models.Asset) {
	t.Helper()
	ctx := context.Background()

	newAsset := func(name string) *models.Asset {
		t.Helper()
		a, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: name})
		if err != nil {
			t.Fatalf("Create asset %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, a.ID) })
		return a
	}

	newDoc := func(owner *models.Asset, entityType, name string) {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   owner.ID,
			EntityType: entityType,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
	}

	mine := newAsset("Precedence Mine")
	other := newAsset("Precedence Other")

	newDoc(mine, "asset.primary", "mine_primary.pdf")
	newDoc(mine, "asset.archived", "mine_archived.pdf")
	// Each matches an OR arm on its own, so an unparenthesised predicate pulls
	// it under `mine` regardless of who owns it.
	newDoc(other, "asset.invoice", "other_invoice.pdf")
	newDoc(other, "asset.archived", "other_archived.pdf")

	return mine, other
}

// loadPrecedenceAsset reads one asset with both OR-filtered edges selected, and
// fails when the read returns anything other than exactly one row — a fanned-out
// LEFT JOIN is itself one of the symptoms under test.
func loadPrecedenceAsset(t *testing.T, ctx context.Context, client *models.Client, want *models.Asset) *models.Asset {
	t.Helper()

	id := want.ID
	assets, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{
		Filter: &models.AssetFilter{ID: &comparator.Number[int64]{Eq: &id}},
	}, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:                    true,
			Name:                  true,
			SpvOrArchivedDocument: &models.DocumentFieldOptions{ID: true, Name: true},
			PrimaryOrInvoiceDocuments: &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("GetMany returned %d assets for one primary key, want 1 — the o2o JOIN fanned out", len(assets))
	}
	return assets[0]
}
