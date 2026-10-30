package suite

// Helpers in this file take the prefix cr.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// create is the Create contract.
func create(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: "visits"})
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("returns the row it wrote when the input soft-deletes it", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Documents().Create(ctx, &models.CreateDocumentInput{
			Title:     "born deleted",
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		})
		if err != nil {
			t.Fatalf("Create(DeletedAt set) error = %v, want nil", err)
		}
		if got == nil || got.Title != "born deleted" || cmp.Equal(got.DeletedAt, models.Document{}.DeletedAt) {
			t.Fatalf("Create(DeletedAt set) = %+v, want the written row with DeletedAt set", got)
		}
		if diff := cmp.Diff([]int64{got.ID}, crDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after Create (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 0)
	})
}

// createMany is the CreateMany contract.
func createMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns every row it wrote, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Documents().CreateMany(ctx, []*models.CreateDocumentInput{
			{Title: "live"},
			{Title: "born deleted", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		})
		if err != nil {
			t.Fatalf("CreateMany([live, deleted]) error = %v, want nil", err)
		}
		titles := make([]string, 0, len(got))
		var deletedIDs []int64
		for _, d := range got {
			titles = append(titles, d.Title)
			if !cmp.Equal(d.DeletedAt, models.Document{}.DeletedAt) {
				deletedIDs = append(deletedIDs, d.ID)
			}
		}
		slices.Sort(titles)
		if diff := cmp.Diff([]string{"born deleted", "live"}, titles); diff != "" {
			t.Errorf("CreateMany([live, deleted]) returned titles (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(crDeletedDocumentIDs(t, h), deletedIDs); diff != "" || len(deletedIDs) != 1 {
			t.Errorf("CreateMany returned soft-deleted keys %v, want the one soft-deleted row (-stored +returned):\n%s", deletedIDs, diff)
		}
		assertRowCount(t, h, "documents", 2)
		assertLiveDocuments(t, h, 1)
	})
}

// crDeletedDocumentIDs returns the keys of the soft-deleted documents, sorted.
func crDeletedDocumentIDs(t *testing.T, h *Harness) []int64 {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany(DeletedAt IS NOT NULL) error = %v", err)
	}
	ids := make([]int64, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	slices.Sort(ids)
	return ids
}

// crAssertNotNull checks that err is a not-null ConstraintError naming column.
func crAssertNotNull(t *testing.T, err error, column string) {
	t.Helper()
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Fatalf("error = %v, want ErrConstraintViolation", err)
	}
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %T, want *models.ConstraintError", err)
	}
	if ce.Type != models.ConstraintNotNull {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintNotNull)
	}
	if ce.Column != column {
		t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, column)
	}
}
