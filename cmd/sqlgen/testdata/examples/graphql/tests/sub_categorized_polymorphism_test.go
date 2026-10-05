package tests

import (
	"context"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// subCategorizedDoc is the projection shape every mutation / query in
// this test reads back. Defined at file scope so it can flow through
// both the seed helper's response envelope and the curated-asset query.
// EntityType is included so the test exercises the enum round-trip: the
// `entityType` GraphQL field is bound to `models.DocumentEntityTypeEnum`
// via the gqlgen `models:` merge (added 2026-05-12 alongside §13.7's
// example fixtures), so reading it on the response is what proves the
// binding is wired and no `documentResolver.EntityType` panic stub is
// generated.
type subCategorizedDoc struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
}

// TestCuratedSurface_SubCategorizedPolymorphism exercises PRD §13.7 over
// the GraphQL curated surface. The single Asset type exposes three
// nested document fields (`primaryDocument`, `attachments`, `invoices`),
// each driven by a different raw-SQL filter on the documents table's
// enum-typed discriminator. Seeding four documents (one per declared
// discriminator plus a stranded `spv`) and selecting all three nested
// fields in a single query asserts (a) per-relationship subset emission
// and (b) the stranded row is silently excluded from every subset.
func TestCuratedSurface_SubCategorizedPolymorphism(t *testing.T) {
	truncateAll(t)

	var assetOut struct {
		CreateAsset struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"createAsset"`
	}
	// declaredCategories exercises the enum-slice path through the GraphQL
	// surface end-to-end. The input wire format is `[ASSETPRIMARY, …]`
	// (SCREAMING_SNAKE_CASE identifiers); UnmarshalGQL bridges to the SQL
	// literal on insert; the column's `default '{}'` would have covered an
	// empty-slice case if the schema allowed it, but the field is
	// `[DocumentEntityTypeEnum!]!` (required) so we always pass a value.
	gqlExecData(t, `
		mutation Seed($name: String!, $categories: [DocumentEntityTypeEnum!]!, $createdAt: Time!) {
			createAsset(input: { name: $name, declaredCategories: $categories, createdAt: $createdAt }) { id name }
		}
	`, map[string]any{
		"name":       "Photovoltaic Plant",
		"categories": []string{"ASSETPRIMARY", "ASSETATTACHMENT"},
		"createdAt":  fixedTimestamp,
	}, &assetOut)
	if assetOut.CreateAsset.ID == "" {
		t.Fatalf("seedAsset: empty ID")
	}
	assetID := assetOut.CreateAsset.ID

	// SeedDoc passes the discriminator as a GraphQL enum identifier (e.g.
	// `ASSETPRIMARY`) — the wire format gqlgen accepts after the
	// `DocumentEntityTypeEnum` model binding lands. The generated
	// MarshalGQL / UnmarshalGQL methods on the enum bridge it back to the
	// SQL literal (`asset.primary`) so the database column receives the
	// expected value. Reading `entityType` on the response also proves the
	// outbound marshal works.
	seedDoc := func(name, gqlEnum, wantWire string) subCategorizedDoc {
		t.Helper()
		var out struct {
			CreateDocument subCategorizedDoc `json:"createDocument"`
		}
		gqlExecData(t, `
			mutation SeedDoc($entityID: UUID!, $entityType: DocumentEntityTypeEnum!, $name: String!, $createdAt: Time!) {
				createDocument(input: {
					entityID: $entityID,
					entityType: $entityType,
					name: $name,
					createdAt: $createdAt,
				}) { id name entityType }
			}
		`, map[string]any{
			"entityID":   assetID,
			"entityType": gqlEnum,
			"name":       name,
			"createdAt":  fixedTimestamp,
		}, &out)
		if out.CreateDocument.ID == "" {
			t.Fatalf("seedDoc %q: empty ID", name)
		}
		if out.CreateDocument.EntityType != wantWire {
			t.Fatalf("seedDoc %q: entityType round-trip got %q, want %q", name, out.CreateDocument.EntityType, wantWire)
		}
		return out.CreateDocument
	}

	primary := seedDoc("site_plan.pdf", "ASSETPRIMARY", "ASSETPRIMARY")
	att1 := seedDoc("photo1.jpg", "ASSETATTACHMENT", "ASSETATTACHMENT")
	att2 := seedDoc("photo2.jpg", "ASSETATTACHMENT", "ASSETATTACHMENT")
	inv := seedDoc("invoice_2026Q1.pdf", "ASSETINVOICE", "ASSETINVOICE")
	stranded := seedDoc("orphan.txt", "SPV", "SPV")

	var query struct {
		Asset struct {
			ID                 string              `json:"id"`
			DeclaredCategories []string            `json:"declaredCategories"`
			PrimaryDocument    *subCategorizedDoc  `json:"primaryDocument"`
			Attachments        []subCategorizedDoc `json:"attachments"`
			Invoices           []subCategorizedDoc `json:"invoices"`
		} `json:"asset"`
	}
	gqlExecData(t, `
		query SubCategorized($id: UUID!) {
			asset(id: $id) {
				id
				declaredCategories
				primaryDocument { id name entityType }
				attachments     { id name entityType }
				invoices        { id name entityType }
			}
		}
	`, map[string]any{"id": assetID}, &query)
	got := query.Asset

	// Slice-of-enum round-trip: declaredCategories was seeded with
	// ["ASSETPRIMARY", "ASSETATTACHMENT"]; MarshalGQL on
	// DocumentEntityTypeEnumSlice walks each element and emits the GraphQL
	// identifier form via the per-element MarshalGQL. A failure here
	// signals the enum-slice path regressed (or the slice's MarshalGQL is
	// missing).
	wantCategories := []string{"ASSETPRIMARY", "ASSETATTACHMENT"}
	if diff := cmp.Diff(wantCategories, got.DeclaredCategories); diff != "" {
		t.Errorf("declaredCategories mismatch (-want +got):\n%s", diff)
	}

	if got.PrimaryDocument == nil {
		t.Fatal("primaryDocument is nil; expected the asset.primary row to surface through the o2o sub-categorized field")
	}
	if got.PrimaryDocument.ID != primary.ID {
		t.Errorf("primaryDocument.ID = %q, want %q", got.PrimaryDocument.ID, primary.ID)
	}
	// Marshal-out check: every nested document carries its discriminator
	// identifier per the relationship that surfaced it. A failure here
	// signals the MarshalGQL emission regressed.
	if got.PrimaryDocument.EntityType != "ASSETPRIMARY" {
		t.Errorf("primaryDocument.entityType = %q, want %q", got.PrimaryDocument.EntityType, "ASSETPRIMARY")
	}
	for _, d := range got.Attachments {
		if d.EntityType != "ASSETATTACHMENT" {
			t.Errorf("attachments[*].entityType = %q, want %q", d.EntityType, "ASSETATTACHMENT")
		}
	}
	for _, d := range got.Invoices {
		if d.EntityType != "ASSETINVOICE" {
			t.Errorf("invoices[*].entityType = %q, want %q", d.EntityType, "ASSETINVOICE")
		}
	}

	wantAttachments := []string{att1.ID, att2.ID}
	gotAttachments := docPayloadIDs(got.Attachments)
	sort.Strings(wantAttachments)
	sort.Strings(gotAttachments)
	if diff := cmp.Diff(wantAttachments, gotAttachments); diff != "" {
		t.Errorf("attachments mismatch (-want +got):\n%s", diff)
	}

	wantInvoices := []string{inv.ID}
	if diff := cmp.Diff(wantInvoices, docPayloadIDs(got.Invoices)); diff != "" {
		t.Errorf("invoices mismatch (-want +got):\n%s", diff)
	}

	allReturned := append(append(docPayloadIDs(got.Attachments), docPayloadIDs(got.Invoices)...), got.PrimaryDocument.ID)
	for _, id := range allReturned {
		if id == stranded.ID {
			t.Errorf("stranded `spv` document %q surfaced through a sub-categorized GraphQL field", stranded.ID)
		}
	}
}

func docPayloadIDs(docs []subCategorizedDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

// TestCuratedSurface_SubCategorizedPolymorphism_MultiColumnFilter covers
// multi-column `filter:` predicates over the GraphQL curated surface: they
// flow through codegen-time qualification on the o2o JOIN ON path, and on the
// target-query WHERE path for o2m / m2m.
func TestCuratedSurface_SubCategorizedPolymorphism_MultiColumnFilter(t *testing.T) {
	truncateAll(t)

	var assetOut struct {
		CreateAsset struct {
			ID string `json:"id"`
		} `json:"createAsset"`
	}
	gqlExecData(t, `
		mutation Seed($name: String!, $categories: [DocumentEntityTypeEnum!]!, $createdAt: Time!) {
			createAsset(input: { name: $name, declaredCategories: $categories, createdAt: $createdAt }) { id }
		}
	`, map[string]any{
		"name":       "Multi-Filter Plant",
		"categories": []string{"ASSETPRIMARY", "ASSETATTACHMENT"},
		"createdAt":  fixedTimestamp,
	}, &assetOut)
	if assetOut.CreateAsset.ID == "" {
		t.Fatalf("seedAsset: empty ID")
	}
	assetID := assetOut.CreateAsset.ID

	seedDoc := func(name, gqlEnum string) subCategorizedDoc {
		t.Helper()
		var out struct {
			CreateDocument subCategorizedDoc `json:"createDocument"`
		}
		gqlExecData(t, `
			mutation SeedDoc($entityID: UUID!, $entityType: DocumentEntityTypeEnum!, $name: String!, $createdAt: Time!) {
				createDocument(input: {
					entityID: $entityID,
					entityType: $entityType,
					name: $name,
					createdAt: $createdAt,
				}) { id name entityType }
			}
		`, map[string]any{
			"entityID":   assetID,
			"entityType": gqlEnum,
			"name":       name,
			"createdAt":  fixedTimestamp,
		}, &out)
		if out.CreateDocument.ID == "" {
			t.Fatalf("seedDoc %q: empty ID", name)
		}
		return out.CreateDocument
	}

	primaryMatch := seedDoc("site_plan_main.pdf", "ASSETPRIMARY")
	_ = seedDoc("warranty.pdf", "ASSETPRIMARY")

	photo1 := seedDoc("photo_aerial.jpg", "ASSETATTACHMENT")
	photo2 := seedDoc("photo_close.jpg", "ASSETATTACHMENT")
	_ = seedDoc("manual.pdf", "ASSETATTACHMENT")

	linked1 := seedDoc("link_drawing.png", "ASSETATTACHMENT")
	linked2 := seedDoc("link_schematic.png", "ASSETATTACHMENT")
	linkedNonMatch := seedDoc("standalone.png", "ASSETATTACHMENT")

	// The junction table only exposes Query + delete on the GraphQL surface
	// (no link mutation is generated for FK-only composite PKs), so seed the
	// junction rows directly through the database pool.
	for _, docID := range []string{linked1.ID, linked2.ID, linkedNonMatch.ID} {
		if _, err := testPool.Exec(
			context.Background(),
			"INSERT INTO asset_document_links (asset_id, document_id) VALUES ($1, $2)",
			assetID, docID,
		); err != nil {
			t.Fatalf("insert asset_document_link: %v", err)
		}
	}

	var query struct {
		Asset struct {
			ID                    string              `json:"id"`
			PrimaryActiveDocument *subCategorizedDoc  `json:"primaryActiveDocument"`
			PhotoAttachments      []subCategorizedDoc `json:"photoAttachments"`
			LinkedAttachments     []subCategorizedDoc `json:"linkedAttachments"`
		} `json:"asset"`
	}
	gqlExecData(t, `
		query MultiFilter($id: UUID!) {
			asset(id: $id) {
				id
				primaryActiveDocument { id name entityType }
				photoAttachments      { id name entityType }
				linkedAttachments     { id name entityType }
			}
		}
	`, map[string]any{"id": assetID}, &query)
	got := query.Asset

	t.Run("O2O", func(t *testing.T) {
		if got.PrimaryActiveDocument == nil {
			t.Fatal("primaryActiveDocument is nil; expected the site_* primary doc")
		}
		if got.PrimaryActiveDocument.ID != primaryMatch.ID {
			t.Errorf("primaryActiveDocument.ID = %q, want %q", got.PrimaryActiveDocument.ID, primaryMatch.ID)
		}
	})

	t.Run("O2M", func(t *testing.T) {
		want := []string{photo1.ID, photo2.ID}
		gotIDs := docPayloadIDs(got.PhotoAttachments)
		sort.Strings(want)
		sort.Strings(gotIDs)
		if diff := cmp.Diff(want, gotIDs); diff != "" {
			t.Errorf("photoAttachments mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("M2M", func(t *testing.T) {
		want := []string{linked1.ID, linked2.ID}
		gotIDs := docPayloadIDs(got.LinkedAttachments)
		sort.Strings(want)
		sort.Strings(gotIDs)
		if diff := cmp.Diff(want, gotIDs); diff != "" {
			t.Errorf("linkedAttachments mismatch (-want +got):\n%s", diff)
		}
	})
}
