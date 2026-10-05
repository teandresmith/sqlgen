package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestRelationshipStaticSort pins that a relationship's static `sort:`
// (PRD §4.8) orders the O2M and M2M loaders when the caller passes no Sorts,
// and caller Sorts replace it (PRD §12.1). sqlgen.yml sorts PhotoAttachments
// (O2M) and LinkedAttachments (M2M) by name descending. Before the fix the
// client's <rel>DefaultSort fields were declared and never assigned, so the
// configured order was silently dropped.
func TestRelationshipStaticSort(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().Create(ctx, &models.CreateAssetInput{Name: "Static Sort Plant"})
	if err != nil {
		t.Fatalf("Create asset: %v", err)
	}
	t.Cleanup(func() { _ = client.Assets().HardDelete(ctx, asset.ID) })

	seed := func(name string) *models.Document {
		t.Helper()
		doc, err := client.Documents().Create(ctx, &models.CreateDocumentInput{
			EntityID:   asset.ID,
			EntityType: models.DocumentEntityTypeEnumAssetattachment,
			Name:       name,
		})
		if err != nil {
			t.Fatalf("Create document %q: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Documents().HardDelete(ctx, doc.ID) })
		return doc
	}

	// Inserted out of name order, so neither insertion nor key order can
	// pass for the sorted one.
	for _, name := range []string{"photo_b.jpg", "photo_c.jpg", "photo_a.jpg"} {
		seed(name)
	}
	for _, name := range []string{"link_b.png", "link_c.png", "link_a.png"} {
		doc := seed(name)
		if _, err := client.AssetDocumentLinks().Create(ctx, &models.CreateAssetDocumentLinkInput{
			AssetID:    asset.ID,
			DocumentID: doc.ID,
		}); err != nil {
			t.Fatalf("Create asset_document_link: %v", err)
		}
		t.Cleanup(func() {
			_ = client.AssetDocumentLinks().HardDelete(ctx, models.AssetDocumentLinkPK{AssetID: asset.ID, DocumentID: doc.ID})
		})
	}

	load := func(t *testing.T, sorts []sql.Sort) *models.Asset {
		t.Helper()
		got, err := client.Assets().Get(ctx, asset.ID, func(o *models.CallOptions[models.AssetFieldOptions]) {
			o.FieldOptions = &models.AssetFieldOptions{
				ID: true,
				PhotoAttachments: &models.DocumentRelationshipOptions{
					FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
					Sorts:        sorts,
				},
				LinkedAttachments: &models.DocumentRelationshipOptions{
					FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true},
					Sorts:        sorts,
				},
			}
		})
		if err != nil {
			t.Fatalf("Get asset: %v", err)
		}
		return got
	}
	names := func(docs []*models.Document) []string {
		out := make([]string, len(docs))
		for i, d := range docs {
			out[i] = d.Name
		}
		return out
	}

	tests := []struct {
		name       string
		sorts      []sql.Sort
		wantPhotos []string
		wantLinks  []string
	}{
		{
			name:       "static sort applies",
			wantPhotos: []string{"photo_c.jpg", "photo_b.jpg", "photo_a.jpg"},
			wantLinks:  []string{"link_c.png", "link_b.png", "link_a.png"},
		},
		{
			name:       "caller sorts replace it",
			sorts:      []sql.Sort{{Column: "name", Direction: sql.Asc}},
			wantPhotos: []string{"photo_a.jpg", "photo_b.jpg", "photo_c.jpg"},
			wantLinks:  []string{"link_a.png", "link_b.png", "link_c.png"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := load(t, tt.sorts)
			if diff := cmp.Diff(tt.wantPhotos, names(got.PhotoAttachments)); diff != "" {
				t.Errorf("PhotoAttachments (O2M) order mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantLinks, names(got.LinkedAttachments)); diff != "" {
				t.Errorf("LinkedAttachments (M2M) order mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
