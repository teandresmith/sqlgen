package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestSubCategorizedPolymorphism exercises PRD §13.7 over SQLite. SQLite
// has no native enum type so the discriminator column is plain TEXT —
// codegen lands it as `string` and the relationship loader's raw-SQL
// filter dispatches against literal values. All three relationships
// return only their declared subset; the stranded `spv` row appears in
// none.
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

	seed := func(name, entityType string) *models.Document {
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
		return doc
	}

	primary := seed("site_plan.pdf", "asset.primary")
	att1 := seed("photo1.jpg", "asset.attachment")
	att2 := seed("photo2.jpg", "asset.attachment")
	inv := seed("invoice_2026Q1.pdf", "asset.invoice")
	stranded := seed("orphan.txt", "spv")

	id := asset.ID
	assets, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{
		Filter: &models.AssetFilter{ID: &comparator.Number[int64]{Eq: &id}},
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
		t.Errorf("PrimaryDocument.ID = %d, want %d", got.PrimaryDocument.ID, primary.ID)
	}

	wantAttachments := []int64{att1.ID, att2.ID}
	gotAttachments := docIDs(got.Attachments)
	if diff := cmp.Diff(wantAttachments, gotAttachments, cmpopts.SortSlices(int64Less)); diff != "" {
		t.Errorf("Attachments mismatch (-want +got):\n%s", diff)
	}

	wantInvoices := []int64{inv.ID}
	if diff := cmp.Diff(wantInvoices, docIDs(got.Invoices)); diff != "" {
		t.Errorf("Invoices mismatch (-want +got):\n%s", diff)
	}

	allReturned := append(append(docIDs(got.Attachments), docIDs(got.Invoices)...), got.PrimaryDocument.ID)
	for _, gotID := range allReturned {
		if gotID == stranded.ID {
			t.Errorf("stranded `spv` document %d surfaced through a sub-categorized relationship", stranded.ID)
		}
	}
}

func docIDs(docs []*models.Document) []int64 {
	out := make([]int64, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

func int64Less(a, b int64) bool { return a < b }

// TestSubCategorizedPolymorphism_MultiColumnFilter covers multi-column filters
// over SQLite (rqlite/sql qualifier path).
func TestSubCategorizedPolymorphism_MultiColumnFilter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Multi-Filter Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name, entityType string) *models.Document {
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
		return doc
	}

	primaryMatch := seed("site_plan_main.pdf", "asset.primary")
	_ = seed("warranty.pdf", "asset.primary")

	photo1 := seed("photo_aerial.jpg", "asset.attachment")
	photo2 := seed("photo_close.jpg", "asset.attachment")
	_ = seed("manual.pdf", "asset.attachment")

	linked1 := seed("link_drawing.png", "asset.attachment")
	linked2 := seed("link_schematic.png", "asset.attachment")
	linkedNonMatch := seed("standalone.png", "asset.attachment")

	for _, docID := range []int64{linked1.ID, linked2.ID, linkedNonMatch.ID} {
		if _, err := client.AssetDocumentLinks().Create(ctx, &models.CreateAssetDocumentLinkInput{
			AssetID:    asset.ID,
			DocumentID: docID,
		}); err != nil {
			t.Fatalf("Create asset_document_link: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, docID := range []int64{linked1.ID, linked2.ID, linkedNonMatch.ID} {
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
			t.Errorf("PrimaryActiveDocument.ID = %d, want %d", got.PrimaryActiveDocument.ID, primaryMatch.ID)
		}
	})

	t.Run("O2M", func(t *testing.T) {
		want := []int64{photo1.ID, photo2.ID}
		gotIDs := docIDs(got.PhotoAttachments)
		if diff := cmp.Diff(want, gotIDs, cmpopts.SortSlices(int64Less)); diff != "" {
			t.Errorf("PhotoAttachments mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("M2M", func(t *testing.T) {
		want := []int64{linked1.ID, linked2.ID}
		gotIDs := docIDs(got.LinkedAttachments)
		if diff := cmp.Diff(want, gotIDs, cmpopts.SortSlices(int64Less)); diff != "" {
			t.Errorf("LinkedAttachments mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestDiscriminatorRelationshipFilter exercises the third read path a
// `discriminator:` edge compiles into: the correlated EXISTS subquery behind a
// relationship filter (PRD §11.1), where the discriminator value is bound into
// the subquery's own argument list rather than spliced into its SQL.
//
// Running it on every dialect is the point, not incidental coverage. A bug of
// exactly this shape once hit the `filter:` form, and it was **SQLite-only** — the codegen qualifier's output spelling differs per dialect,
// so a predicate that is correct on postgres can name a non-existent alias here.
// The golden tests cannot see it: they compare generated text against a recorded
// copy of the same generated text, so broken SQL is recorded as faithfully as
// working SQL. Only executing the query distinguishes them.
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

	seed := func(owner int64, name, entityType string) {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID: owner, EntityType: entityType, Name: name,
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
	seed(asset.ID, "invoice_2026Q1.pdf", "asset.invoice")
	seed(other.ID, "photo1.jpg", "asset.attachment")

	byRelationship := func(t *testing.T, f *models.AssetFilter) []int64 {
		t.Helper()
		rows, err := client.Assets().GetMany(ctx, &models.GetAssetsInput{Filter: f})
		if err != nil {
			t.Fatalf("GetMany with a relationship filter: %v", err)
		}
		ids := make([]int64, 0, len(rows))
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
		want := []int64{other.ID}
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b int64) bool { return a < b })); diff != "" {
			t.Errorf("Attachments relationship filter mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the discriminator narrows the subquery", func(t *testing.T) {
		// The invoice row matches the FK correlation and the name predicate;
		// only the discriminator keeps it out of an Attachments filter.
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
		want := []int64{asset.ID}
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b int64) bool { return a < b })); diff != "" {
			t.Errorf("Invoices relationship filter mismatch (-want +got):\n%s", diff)
		}
	})
}
