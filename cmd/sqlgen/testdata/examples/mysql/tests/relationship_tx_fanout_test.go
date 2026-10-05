package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// TestLoadRelationships_MultiEdgeInsideTransaction is a multi-edge relationship
// read inside a transaction, over the database/sql adapter.
//
// loadRelationships' errgroup was once bounded to one worker whenever ctx
// carried a transaction. PRD §18.5's connection reservation serializes the
// fan-out at the Tx instead, so the bound is gone (PRD §13.2 step 4) — and this
// is the read that now reaches the reservation. A transaction is pinned to one
// connection and a connection carries one statement at a time, so the four
// selected edges are five statements contending for it: three O2M queries plus
// the M2M edge's junction and target reads.
//
// The `graphql` tree covers this shape on pgx. This covers the stdlib adapter,
// which fails differently and is the reason this test is not redundant with it:
// widening connSem past capacity 1 and re-running gives
//
//	rollback failed: rolling back transaction fanout_read: stdlib rollback:
//	  invalid connection (original error: load asset documents junction:
//	  tx query fanout_read: tx query: driver: bad connection)
//
// with **zero** data races, where pgx reported 113 and `conn busy`. That is the
// distinction worth a second test: database/sql does serialize *access* to a
// Tx's connection, so there is no Go-level race — but it does not stop a second
// statement being issued while a result set is open, and the MySQL wire protocol
// rejects that and poisons the connection, so the rollback cannot run either.
// Serialized access is not the same guarantee as a reserved connection, and only
// the reservation supplies the second one. Run under -race, as
// `make check-examples` does.
func TestLoadRelationships_MultiEdgeInsideTransaction(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	entityAttachment, entityInvoice := models.DocumentsEntityTypeEnumAssetattachment, models.DocumentsEntityTypeEnumAssetinvoice

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Fanout Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name string, entityType models.DocumentsEntityTypeEnum) *models.Document {
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
