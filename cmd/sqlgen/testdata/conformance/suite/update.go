package suite

// Helpers in this file take the prefix up.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// update is the Update contract.
func update(t *testing.T, h *Harness) {
	_, _ = t, h
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
		// Only that an error came back is uniform: pgx's SendBatch path
		// returns the driver's error unmapped.
		if err == nil {
			t.Fatal("UpdateMany error = nil, want the CHECK violation")
		}
		want := map[int64]string{ids[0]: "updated", ids[1]: "updated", ids[2]: "doc", ids[3]: "doc"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the failed UpdateMany (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 4)
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
