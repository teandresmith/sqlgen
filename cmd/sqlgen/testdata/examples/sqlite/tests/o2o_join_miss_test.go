package tests

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestO2OJoinMiss_ReadsAsNil pins the O2O LEFT JOIN miss.
//
// The joined read selects the target's columns alongside the parent's and tells
// a real row from a miss afterwards, by testing the target's PK against its zero
// value. On a miss the driver returns NULL for every one of the target's
// columns, and a NOT NULL column resolves to a Go type that refuses NULL — so
// the scan failed before the detection could run, and *any* O2O edge errored for
// a parent without a matching row. Measured on both drivers before the fix:
// "converting NULL to int64 is unsupported" on database/sql, and "cannot scan
// NULL into *[16]byte" on pgx. The destinations are wrapped in
// database.NullScan, which defers the decision to the miss detection.
//
// Both edges are covered because they differ in shape, not just in name:
// PrimaryActiveDocument is a plain `filter:` edge and PrimaryDocument a
// `discriminator:` one, and a parent may miss on one while hitting the other.
func TestO2OJoinMiss_ReadsAsNil(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// An asset with no documents at all: every O2O edge is a miss.
	bare, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "O2O Miss Bare"})
	if err != nil {
		t.Fatalf("Create bare asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, bare.ID) })

	// An asset matching one edge but not the other, so the assertion below is
	// not satisfied by a scan that simply nils everything out.
	partial, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "O2O Miss Partial"})
	if err != nil {
		t.Fatalf("Create partial asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, partial.ID) })

	doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
		EntityID:   partial.ID,
		EntityType: "asset.primary",
		Name:       "site_plan.pdf",
	})
	if err != nil {
		t.Fatalf("Create document: %v", err)
	}
	t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })

	load := func(t *testing.T, id int64) *models.Asset {
		t.Helper()
		got, err := client.Assets().Get(ctx, id, func(o *models.CallOptions[models.AssetFieldOptions]) {
			o.FieldOptions = &models.AssetFieldOptions{
				ID:                    true,
				Name:                  true,
				PrimaryActiveDocument: &models.DocumentFieldOptions{ID: true, Name: true},
				PrimaryDocument:       &models.DocumentFieldOptions{ID: true, Name: true},
			}
		})
		if err != nil {
			t.Fatalf("Get asset with an o2o miss: %v", err)
		}
		return got
	}

	t.Run("every edge misses", func(t *testing.T) {
		got := load(t, bare.ID)
		if got.PrimaryActiveDocument != nil {
			t.Errorf("PrimaryActiveDocument = %+v, want nil", got.PrimaryActiveDocument)
		}
		if got.PrimaryDocument != nil {
			t.Errorf("PrimaryDocument = %+v, want nil", got.PrimaryDocument)
		}
	})

	t.Run("one edge hits", func(t *testing.T) {
		got := load(t, partial.ID)
		// `site_plan.pdf` is entity_type 'asset.primary', which the
		// discriminator edge matches and the `name LIKE 'site_%'` filter edge
		// matches too.
		if got.PrimaryDocument == nil {
			t.Fatal("PrimaryDocument = nil, want the seeded document")
		}
		if got.PrimaryDocument.Name != "site_plan.pdf" {
			t.Errorf("PrimaryDocument.Name = %q, want %q", got.PrimaryDocument.Name, "site_plan.pdf")
		}
	})
}
