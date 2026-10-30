package suite

// Helpers in this file take the prefix up.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// update is the Update contract.
func update(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the row it soft-deletes", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		got, err := h.Client.Documents().Update(ctx, d.ID, &models.UpdateDocumentInput{
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		})
		if err != nil {
			t.Fatalf("Update(d, DeletedAt set) error = %v, want nil", err)
		}
		if got == nil || got.ID != d.ID || !upDocumentDeleted(got) {
			t.Errorf("Update(d, DeletedAt set) = %+v, want d with DeletedAt set", got)
		}
		if diff := cmp.Diff([]int64{d.ID}, upDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after Update (-want +got):\n%s", diff)
		}
	})

	t.Run("returns a soft-deleted row it updates", func(t *testing.T) {
		h.Reset(t)
		d := createDeletedDocument(t, h, "before")
		got, err := h.Client.Documents().Update(ctx, d.ID, &models.UpdateDocumentInput{Title: omittable.Set("after")})
		if err != nil {
			t.Fatalf("Update(soft-deleted d, Title) error = %v, want nil", err)
		}
		if got == nil || got.ID != d.ID || got.Title != "after" || !upDocumentDeleted(got) {
			t.Errorf("Update(soft-deleted d, Title) = %+v, want d titled after with DeletedAt set", got)
		}
		if diff := cmp.Diff([]int64{d.ID}, upDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after Update (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty update returns a soft-deleted row unchanged", func(t *testing.T) {
		h.Reset(t)
		d := createDeletedDocument(t, h, "d")
		got, err := h.Client.Documents().Update(ctx, d.ID, &models.UpdateDocumentInput{})
		if err != nil {
			t.Fatalf("Update(soft-deleted d, empty input) error = %v, want nil", err)
		}
		if got == nil || got.ID != d.ID || got.Title != "d" || !upDocumentDeleted(got) {
			t.Errorf("Update(soft-deleted d, empty input) = %+v, want d with DeletedAt set", got)
		}
	})

	t.Run("without strict updates a soft-deleted row is returned, not dropped as missing", func(t *testing.T) {
		h.Reset(t)
		cn := upCreateDeletedCounter(t, h, "c")
		got, err := h.Client.Counters().Update(ctx, cn.ID, &models.UpdateCounterInput{Hits: omittable.Set(int64(5))})
		if err != nil {
			t.Fatalf("Update(soft-deleted counter, Hits) error = %v, want nil", err)
		}
		if got == nil || got.ID != cn.ID || got.Hits != 5 || cmp.Equal(got.DeletedAt, models.Counter{}.DeletedAt) {
			t.Errorf("Update(soft-deleted counter, Hits) = %+v, want the counter with Hits 5 and DeletedAt set", got)
		}
		got, err = h.Client.Counters().Update(ctx, cn.ID, &models.UpdateCounterInput{})
		if err != nil {
			t.Fatalf("Update(soft-deleted counter, empty input) error = %v, want nil", err)
		}
		if got == nil || got.ID != cn.ID {
			t.Errorf("Update(soft-deleted counter, empty input) = %+v, want the counter", got)
		}
		// A key with no row still returns no entity and no error.
		for _, tt := range []struct {
			name string
			in   *models.UpdateCounterInput
		}{
			{"Hits", &models.UpdateCounterInput{Hits: omittable.Set(int64(1))}},
			{"empty input", &models.UpdateCounterInput{}},
		} {
			got, err := h.Client.Counters().Update(ctx, cn.ID+1000, tt.in)
			if err != nil || got != nil {
				t.Errorf("Update(missing key, %s) = %+v, %v, want nil, nil", tt.name, got, err)
			}
		}
	})
}

// updateMany is the UpdateMany contract.
func updateMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("through a querier that cannot send a batch, a failed sub-batch rolls back its earlier items and keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 4)
		// h.Querier has no SendBatch, so on pgx this client runs each
		// sub-batch's statements one by one rather than as a pipelined batch.
		client := models.New(h.Querier)
		_, err := client.Documents().UpdateMany(ctx, upFailingItems(ids))
		upAssertCheckError(t, err)
		want := map[int64]string{ids[0]: "updated", ids[1]: "updated", ids[2]: "doc", ids[3]: "doc"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the failed UpdateMany (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 4)
	})

	t.Run("inside the caller's transaction, a failed sub-batch rolls back to its savepoint and the transaction continues and commits", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 4)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			_, err := h.Client.Documents().UpdateMany(txCtx, upFailingItems(ids))
			if cerr := upCheckError(err); cerr != nil {
				return cerr
			}
			// The failed sub-batch is undone inside the transaction already.
			titles, err := upDocumentTitlesIn(txCtx, h)
			if err != nil {
				return err
			}
			want := map[int64]string{ids[0]: "updated", ids[1]: "updated", ids[2]: "doc", ids[3]: "doc"}
			if diff := cmp.Diff(want, titles); diff != "" {
				t.Errorf("document titles inside the transaction after the failed UpdateMany (-want +got):\n%s", diff)
			}
			// The transaction is still usable: a later write in it succeeds.
			if _, err := h.Client.Documents().Update(txCtx, ids[2], &models.UpdateDocumentInput{Title: omittable.Set("after")}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		want := map[int64]string{ids[0]: "updated", ids[1]: "updated", ids[2]: "after", ids[3]: "doc"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the commit (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 4)
	})

	t.Run("a failed sub-batch rolls back its earlier items and keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 4)
		_, err := h.Client.Documents().UpdateMany(ctx, upFailingItems(ids))
		upAssertCheckError(t, err)
		want := map[int64]string{ids[0]: "updated", ids[1]: "updated", ids[2]: "doc", ids[3]: "doc"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the failed UpdateMany (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 4)
	})

	t.Run("returns every row it updates, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		live := createDocument(t, h, "live")
		deleted := createDeletedDocument(t, h, "deleted")
		got, err := h.Client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
			{ID: live.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("live2")}},
			{ID: deleted.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("deleted2")}},
		})
		if err != nil {
			t.Fatalf("UpdateMany([live, deleted]) error = %v, want nil", err)
		}
		titles := make([]string, 0, len(got))
		for _, d := range got {
			titles = append(titles, d.Title)
		}
		slices.Sort(titles)
		if diff := cmp.Diff([]string{"deleted2", "live2"}, titles); diff != "" {
			t.Errorf("UpdateMany([live, deleted]) returned titles (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{deleted.ID}, upDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after UpdateMany (-want +got):\n%s", diff)
		}
	})
}

// upFailingItems retitles the four documents ids name. With batch_size: 2,
// ids[0] and ids[1] are the first sub-batch, which succeeds; in the second,
// ids[2] is retitled and ids[3] then breaks documents_pinned_stays_live by
// being both pinned and soft-deleted.
func upFailingItems(ids []int64) []models.UpdateDocumentItem {
	return []models.UpdateDocumentItem{
		{ID: ids[0], Input: &models.UpdateDocumentInput{Title: omittable.Set("updated")}},
		{ID: ids[1], Input: &models.UpdateDocumentInput{Title: omittable.Set("updated")}},
		{ID: ids[2], Input: &models.UpdateDocumentInput{Title: omittable.Set("updated")}},
		{ID: ids[3], Input: &models.UpdateDocumentInput{
			Title:     omittable.Set("pinned"),
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		}},
	}
}

// upAssertCheckError fails t unless err maps to a check ConstraintError.
func upAssertCheckError(t *testing.T, err error) {
	t.Helper()
	if cerr := upCheckError(err); cerr != nil {
		t.Fatal(cerr)
	}
}

// upCheckError describes how err differs from a check ConstraintError, or
// returns nil. Inside a transaction a subtest returns it rather than calling
// t.Fatal, so the transaction is rolled back before the subtest ends.
func upCheckError(err error) error {
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		return fmt.Errorf("UpdateMany error = %v, want a *ConstraintError", err)
	}
	if ce.Type != models.ConstraintCheck {
		return fmt.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
	}
	return nil
}

// upDocumentTitles reads every document's title, soft-deleted ones included,
// with a statement the counting querier does not record.
func upDocumentTitles(t *testing.T, h *Harness) map[int64]string {
	t.Helper()
	titles, err := upDocumentTitlesIn(context.Background(), h)
	if err != nil {
		t.Fatalf("read document titles: %v", err)
	}
	return titles
}

// upDocumentTitlesIn reads every document's title on ctx's transaction when
// it carries one, and outside any transaction otherwise.
func upDocumentTitlesIn(ctx context.Context, h *Harness) (map[int64]string, error) {
	rows, err := database.Conn(ctx, h.Querier.Inner).Query(ctx, "SELECT id, title FROM documents")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := make(map[int64]string)
	for rows.Next() {
		var (
			id    int64
			title string
		)
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}
	return titles, rows.Err()
}

func upDocumentDeleted(d *models.Document) bool {
	return !cmp.Equal(d.DeletedAt, models.Document{}.DeletedAt)
}

// upDeletedDocumentIDs returns the keys of the soft-deleted documents, sorted.
func upDeletedDocumentIDs(t *testing.T, h *Harness) []int64 {
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

// upCreateDeletedCounter creates a counter and soft-deletes it, returning the
// soft-deleted entity.
func upCreateDeletedCounter(t *testing.T, h *Harness, name string) *models.Counter {
	t.Helper()
	ctx := context.Background()
	cn, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())})
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	deleted, err := h.Client.Counters().SoftDelete(ctx, cn.ID)
	if err != nil || deleted == nil {
		t.Fatalf("SoftDelete counter %q = %+v, %v", name, deleted, err)
	}
	return deleted
}
