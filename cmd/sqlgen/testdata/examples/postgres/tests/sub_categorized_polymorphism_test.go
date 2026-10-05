package tests

import (
	"context"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestSubCategorizedPolymorphism exercises PRD §13.7. A single parent
// (assets) declares three relationships into a shared child table
// (documents), each filtered by a discriminator-enum value. The test
// seeds four documents — one per declared discriminator plus a stranded
// `spv` row — and asserts every relationship returns its own subset and
// the stranded row is silently excluded from every parent-side load.
func TestSubCategorizedPolymorphism(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{
		Name: "Photovoltaic Plant",
	})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name string, et models.DocumentEntityTypeEnum) *models.Document {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: et,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
		return doc
	}

	primary := seed("site_plan.pdf", models.DocumentEntityTypeEnumAssetprimary)
	att1 := seed("photo1.jpg", models.DocumentEntityTypeEnumAssetattachment)
	att2 := seed("photo2.jpg", models.DocumentEntityTypeEnumAssetattachment)
	inv := seed("invoice_2026Q1.pdf", models.DocumentEntityTypeEnumAssetinvoice)
	// Stranded row: discriminator value matches no declared relationship.
	stranded := seed("orphan.txt", models.DocumentEntityTypeEnumSpv)

	assets, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{
		Filter: &models.AssetFilter{ID: &comparator.ID{Eq: new(asset.ID.String())}},
	}, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:   true,
			Name: true,
			PrimaryDocument: &models.DocumentFieldOptions{
				ID:   true,
				Name: true,
			},
			Attachments: &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			},
			Invoices: &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("GetMany asset: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("GetMany returned %d assets, want 1", len(assets))
	}
	got := assets[0]

	if got.PrimaryDocument == nil {
		t.Fatal("PrimaryDocument is nil; expected sub-categorized o2o load to return the asset.primary row")
	}
	if got.PrimaryDocument.ID != primary.ID {
		t.Errorf("PrimaryDocument.ID = %q, want %q", got.PrimaryDocument.ID, primary.ID)
	}

	wantAttachments := []uuid.UUID{att1.ID, att2.ID}
	gotAttachments := docIDs(got.Attachments)
	if diff := cmp.Diff(wantAttachments, gotAttachments, cmpopts.SortSlices(uuidLess)); diff != "" {
		t.Errorf("Attachments mismatch (-want +got):\n%s", diff)
	}

	wantInvoices := []uuid.UUID{inv.ID}
	if diff := cmp.Diff(wantInvoices, docIDs(got.Invoices)); diff != "" {
		t.Errorf("Invoices mismatch (-want +got):\n%s", diff)
	}

	// Stranded row never appears in any relationship subset.
	allReturned := append(append(docIDs(got.Attachments), docIDs(got.Invoices)...), got.PrimaryDocument.ID)
	for _, id := range allReturned {
		if id == stranded.ID {
			t.Errorf("stranded `spv` document %q surfaced through a sub-categorized relationship", stranded.ID)
		}
	}
}

func docIDs(docs []*models.Document) []uuid.UUID {
	out := make([]uuid.UUID, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

// uuidLess orders IDs for cmpopts.SortSlices. Its parameter type must match the
// slice element type: a less func over string never applies to []uuid.UUID,
// which silently made these comparisons order-sensitive.
func uuidLess(a, b uuid.UUID) bool { return a.String() < b.String() }

// TestSubCategorizedPolymorphism_MultiColumnFilter covers multi-column
// `filter:` predicates on all three relationship types. The codegen
// qualifier rewrites bare identifiers to the relationship's target alias so
// every column in the filter binds to the target table.
func TestSubCategorizedPolymorphism_MultiColumnFilter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Multi-Filter Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name string, et models.DocumentEntityTypeEnum) *models.Document {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: et,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
		return doc
	}

	// O2O: two primary docs, one matching the second predicate (name LIKE 'site_%').
	primaryMatch := seed("site_plan_main.pdf", models.DocumentEntityTypeEnumAssetprimary)
	_ = seed("warranty.pdf", models.DocumentEntityTypeEnumAssetprimary) // non-matching primary

	// O2M: attachments with mixed names. Only `photo_*` should surface in PhotoAttachments.
	photo1 := seed("photo_aerial.jpg", models.DocumentEntityTypeEnumAssetattachment)
	photo2 := seed("photo_close.jpg", models.DocumentEntityTypeEnumAssetattachment)
	_ = seed("manual.pdf", models.DocumentEntityTypeEnumAssetattachment) // non-matching attachment

	// M2M: attachments linked via the junction, only some matching `link_*`.
	linked1 := seed("link_drawing.png", models.DocumentEntityTypeEnumAssetattachment)
	linked2 := seed("link_schematic.png", models.DocumentEntityTypeEnumAssetattachment)
	linkedNonMatch := seed("standalone.png", models.DocumentEntityTypeEnumAssetattachment)

	// Link the matching subset AND a non-matching name through the junction.
	for _, docID := range []uuid.UUID{linked1.ID, linked2.ID, linkedNonMatch.ID} {
		if _, err := client.AssetDocumentLinks().Create(ctx, &models.CreateAssetDocumentLinkInput{
			AssetID:    asset.ID,
			DocumentID: docID,
		}); err != nil {
			t.Fatalf("Create asset_document_link: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, docID := range []uuid.UUID{linked1.ID, linked2.ID, linkedNonMatch.ID} {
			_ = client.AssetDocumentLinks().HardDelete(ctx, models.AssetDocumentLinkPK{
				AssetID:    asset.ID,
				DocumentID: docID,
			})
		}
	})

	got, err := client.Assets().Get(ctx, asset.ID, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:                    true,
			Name:                  true,
			PrimaryActiveDocument: &models.DocumentFieldOptions{ID: true, Name: true},
			PhotoAttachments: &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			},
			LinkedAttachments: &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("Get asset: %v", err)
	}

	t.Run("O2O", func(t *testing.T) {
		if got.PrimaryActiveDocument == nil {
			t.Fatal("PrimaryActiveDocument is nil; expected the site_* primary doc")
		}
		if got.PrimaryActiveDocument.ID != primaryMatch.ID {
			t.Errorf("PrimaryActiveDocument.ID = %q, want %q (matching doc)", got.PrimaryActiveDocument.ID, primaryMatch.ID)
		}
	})

	t.Run("O2M", func(t *testing.T) {
		want := []uuid.UUID{photo1.ID, photo2.ID}
		gotIDs := docIDs(got.PhotoAttachments)
		if diff := cmp.Diff(want, gotIDs, cmpopts.SortSlices(uuidLess)); diff != "" {
			t.Errorf("PhotoAttachments mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("M2M", func(t *testing.T) {
		want := []uuid.UUID{linked1.ID, linked2.ID}
		gotIDs := docIDs(got.LinkedAttachments)
		if diff := cmp.Diff(want, gotIDs, cmpopts.SortSlices(uuidLess)); diff != "" {
			t.Errorf("LinkedAttachments mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestDiscriminatorRelationshipFilter exercises the third read path a
// `discriminator:` edge compiles into: the correlated EXISTS subquery behind a
// relationship filter (PRD §11.1), where the discriminator value is bound into
// the subquery's own argument list rather than spliced into its SQL.
//
// It exists because the goldens cannot catch what it catches. A bug of exactly
// this shape once hit the `filter:` form — an alias placeholder that survived
// into the emitted SQL and named no relation — and it went unnoticed because the golden test compares generated text against a recorded
// copy of the same generated text, so a broken predicate is recorded as
// faithfully as a working one. Only executing the query distinguishes them.
func TestDiscriminatorRelationshipFilter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Filtered Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	other, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Unrelated Plant"})
	if err != nil {
		t.Fatalf("Create other asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, other.ID) })

	seed := func(owner uuid.UUID, name string, et models.DocumentEntityTypeEnum) {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID: owner, EntityType: et, Name: name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
	}

	// The asset under test owns an invoice but no attachment; the other asset
	// owns the attachment. A filter on Attachments must therefore select the
	// other asset and not this one — which fails both if the discriminator is
	// dropped from the subquery and if the bound argument lands in the wrong
	// position.
	seed(asset.ID, "invoice_2026Q1.pdf", models.DocumentEntityTypeEnumAssetinvoice)
	seed(other.ID, "photo1.jpg", models.DocumentEntityTypeEnumAssetattachment)

	byRelationship := func(t *testing.T, f *models.AssetFilter) []uuid.UUID {
		t.Helper()
		rows, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{Filter: f})
		if err != nil {
			t.Fatalf("GetMany with a relationship filter: %v", err)
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			if r.ID == asset.ID || r.ID == other.ID {
				ids = append(ids, r.ID)
			}
		}
		return ids
	}

	t.Run("matches the parent whose child carries the discriminator value", func(t *testing.T) {
		got := byRelationship(t, &models.AssetFilter{
			Attachments: &models.DocumentFilter{
				Name: &comparator.String{Eq: new("photo1.jpg")},
			},
		})
		want := []uuid.UUID{other.ID}
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(uuidLess)); diff != "" {
			t.Errorf("Attachments relationship filter mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the discriminator narrows the subquery", func(t *testing.T) {
		// The invoice row matches the FK correlation and the name predicate;
		// only the discriminator keeps it out of an Attachments filter. Without
		// it in the subquery this returns the asset under test.
		got := byRelationship(t, &models.AssetFilter{
			Attachments: &models.DocumentFilter{
				Name: &comparator.String{Eq: new("invoice_2026Q1.pdf")},
			},
		})
		if len(got) != 0 {
			t.Errorf("Attachments filter matched %v, want none — the invoice row is not an attachment", got)
		}
	})

	t.Run("the sibling sub-category sees it", func(t *testing.T) {
		got := byRelationship(t, &models.AssetFilter{
			Invoices: &models.DocumentFilter{
				Name: &comparator.String{Eq: new("invoice_2026Q1.pdf")},
			},
		})
		want := []uuid.UUID{asset.ID}
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(uuidLess)); diff != "" {
			t.Errorf("Invoices relationship filter mismatch (-want +got):\n%s", diff)
		}
	})
}
