package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestLoadRelationships_MultiEdgeInsideTransaction is a multi-edge relationship
// read inside a transaction, over the database/sql adapter.
//
// loadRelationships' errgroup was once bounded to one worker whenever ctx
// carried a transaction. PRD §18.5's connection reservation serializes the
// fan-out at the Tx instead, so the bound is gone (PRD §13.2 step 4) — and this
// is the read that now runs unbounded inside a transaction. A transaction is
// pinned to one connection and a connection carries one statement at a time, so
// the four selected edges are five statements contending for it: three O2M
// queries plus the M2M edge's junction and target reads.
//
// The `graphql` tree covers this shape on pgx and the `mysql` tree over the same
// stdlib adapter. This arm is, honestly, **not** failing-first: widening connSem
// past capacity 1 and re-running still passes here, where pgx reports 113 data
// races and `conn busy` and MySQL poisons its connection with
// `driver: bad connection`. SQLite's driver tolerates a second statement while a
// result set is open, so the reservation is not what is saving this read today.
//
// It is kept as a gate rather than dropped because that tolerance is a property
// of the driver, not of the contract PRD §18.5 states — the generated code is
// identical on all three dialects, and this pins that the bound's removal stays
// correct here if that ever changes. Run under -race, as `make check-examples`
// does.
func TestLoadRelationships_MultiEdgeInsideTransaction(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	entityAttachment, entityInvoice := "asset.attachment", "asset.invoice"

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Fanout Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name string, entityType string) *models.Document {
		t.Helper()
		doc, createErr := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: entityType,
			Name:       name,
		})
		if createErr != nil {
			t.Fatalf("Create document %q: %v", name, createErr)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
		return doc
	}

	// Attachments takes every asset.attachment row, so all three below.
	// PhotoAttachments narrows that to photo_%, LinkedAttachments to link_%
	// reached through the junction, and Invoices is its own entity type — four
	// edges whose row sets all differ, so a mismapped edge cannot pass.
	photo := seed("photo_aerial.jpg", entityAttachment)
	linked := seed("link_drawing.png", entityAttachment)
	plain := seed("manual.pdf", entityAttachment)
	invoice := seed("invoice_2026Q1.pdf", entityInvoice)

	if _, err := client.AssetDocumentLinks().Create(ctx, &models.CreateAssetDocumentLinkInput{
		AssetID:    asset.ID,
		DocumentID: linked.ID,
	}); err != nil {
		t.Fatalf("Create asset_document_link: %v", err)
	}
	t.Cleanup(func() {
		_ = client.AssetDocumentLinks().HardDelete(ctx, models.AssetDocumentLinkPK{
			AssetID:    asset.ID,
			DocumentID: linked.ID,
		})
	})

	// Selecting several edges at once is what makes the loader fan out; one
	// edge alone never reached the contention this covers.
	fourEdges := func(o *models.CallOptions[models.AssetFieldOptions]) {
		docFields := func() *models.DocumentRelationshipOptions {
			return &models.DocumentRelationshipOptions{
				FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
			}
		}
		o.FieldOptions = &models.AssetFieldOptions{
			ID:                true,
			Name:              true,
			Attachments:       docFields(),
			Invoices:          docFields(),
			PhotoAttachments:  docFields(),
			LinkedAttachments: docFields(),
		}
	}

	// Asserting the ids, not just the counts, is what proves each edge's rows
	// landed on the edge that queried for them.
	assertEdges := func(where string, got *models.Asset) {
		t.Helper()
		for _, edge := range []struct {
			name string
			want []int64
			got  []*models.Document
		}{
			{"Attachments", []int64{photo.ID, linked.ID, plain.ID}, got.Attachments},
			{"Invoices", []int64{invoice.ID}, got.Invoices},
			{"PhotoAttachments", []int64{photo.ID}, got.PhotoAttachments},
			{"LinkedAttachments", []int64{linked.ID}, got.LinkedAttachments},
		} {
			if diff := cmp.Diff(edge.want, docIDs(edge.got), cmpopts.SortSlices(int64Less)); diff != "" {
				t.Errorf("%s: %s mismatch (-want +got):\n%s", where, edge.name, diff)
			}
		}
	}

	// Outside a transaction the fan-out is real — each edge draws its own
	// pooled connection — and must keep working.
	outside, err := client.Assets().Get(ctx, asset.ID, fourEdges)
	if err != nil {
		t.Fatalf("Get outside a transaction: %v", err)
	}
	assertEdges("outside a transaction", outside)

	var inside *models.Asset
	if err := client.WithTx(ctx, "fanout_read", func(txCtx context.Context) error {
		var getErr error
		inside, getErr = client.Assets().Get(txCtx, asset.ID, fourEdges)
		return getErr
	}); err != nil {
		t.Fatalf("Get inside a transaction: %v", err)
	}
	assertEdges("inside a transaction", inside)
}
