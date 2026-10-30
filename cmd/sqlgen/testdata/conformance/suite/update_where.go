package suite

// Helpers in this file take the prefix uw.

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// updateWhere is the UpdateWhere contract.
func updateWhere(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns every row the filter matches, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		live := createDocument(t, h, "live")
		deleted := createDeletedDocument(t, h, "deleted")
		untouched := createDocument(t, h, "untouched")
		got, err := h.Client.Documents().UpdateWhere(ctx,
			&models.DocumentFilter{ID: &comparator.Number[int64]{In: []int64{live.ID, deleted.ID}}},
			&models.UpdateDocumentInput{Title: omittable.Set("renamed")})
		if err != nil {
			t.Fatalf("UpdateWhere(ID in [live, deleted]) error = %v, want nil", err)
		}
		ids := make([]int64, 0, len(got))
		for _, d := range got {
			ids = append(ids, d.ID)
			if d.Title != "renamed" {
				t.Errorf("UpdateWhere returned %+v, want Title renamed", d)
			}
		}
		slices.Sort(ids)
		if diff := cmp.Diff([]int64{live.ID, deleted.ID}, ids); diff != "" {
			t.Errorf("UpdateWhere(ID in [live, deleted]) returned keys (-want +got):\n%s", diff)
		}
		// The soft-deleted row was written and stays soft-deleted.
		if diff := cmp.Diff([]string{"renamed"}, uwDeletedDocumentTitles(t, h)); diff != "" {
			t.Errorf("soft-deleted document titles after UpdateWhere (-want +got):\n%s", diff)
		}
		if _, err := h.Client.Documents().Get(ctx, untouched.ID); err != nil {
			t.Errorf("Get(untouched) error = %v, want nil", err)
		}
	})

	t.Run("an empty update returns every row the filter matches, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		live := createDocument(t, h, "live")
		deleted := createDeletedDocument(t, h, "deleted")
		createDocument(t, h, "untouched")
		got, err := h.Client.Documents().UpdateWhere(ctx,
			&models.DocumentFilter{ID: &comparator.Number[int64]{In: []int64{live.ID, deleted.ID}}},
			&models.UpdateDocumentInput{})
		if err != nil {
			t.Fatalf("UpdateWhere(ID in [live, deleted], empty input) error = %v, want nil", err)
		}
		ids := make([]int64, 0, len(got))
		for _, d := range got {
			ids = append(ids, d.ID)
		}
		slices.Sort(ids)
		if diff := cmp.Diff([]int64{live.ID, deleted.ID}, ids); diff != "" {
			t.Errorf("UpdateWhere(ID in [live, deleted], empty input) returned keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"deleted"}, uwDeletedDocumentTitles(t, h)); diff != "" {
			t.Errorf("soft-deleted document titles after UpdateWhere (-want +got):\n%s", diff)
		}
	})
}

// increment is the Increment contract.
func increment(t *testing.T, h *Harness) {
	_, _ = t, h
}

// uwDeletedDocumentTitles returns the titles of the soft-deleted documents, sorted.
func uwDeletedDocumentTitles(t *testing.T, h *Harness) []string {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany(DeletedAt IS NOT NULL) error = %v", err)
	}
	titles := make([]string, 0, len(docs))
	for _, d := range docs {
		titles = append(titles, d.Title)
	}
	slices.Sort(titles)
	return titles
}
