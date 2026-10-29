package suite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// softDeleteWhere: SoftDeleteWhere marks every live row the filter matches as deleted.
func softDeleteWhere(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("soft-deletes only the live rows the filter matches and returns them", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		other := createDocument(t, h, "other")
		// gone matches the filter but is already deleted, so the write must skip it.
		gone := sdwSeedDeleted(t, h, "gone")
		goneBefore := sdwDeletedDocuments(t, h, gone)
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a", "b", "gone"))
		if err != nil {
			t.Fatalf("SoftDeleteWhere(title in [a, b, gone]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a.ID, b.ID}, sdwDocumentIDs(got), sdwByValue); diff != "" {
			t.Errorf("SoftDeleteWhere returned documents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(goneBefore, sdwDeletedDocuments(t, h, gone)); diff != "" {
			t.Errorf("already-deleted document changed (-before +after):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{other.ID}, sdwDocumentIDs(sdwLiveDocuments(t, h)), sdwByValue); diff != "" {
			t.Errorf("live documents after SoftDeleteWhere (-want +got):\n%s", diff)
		}
	})

	t.Run("nil filter returns ErrEmptyFilter and deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, nil)
		if !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("SoftDeleteWhere(nil) error = %v, want ErrEmptyFilter", err)
		}
		if got != nil {
			t.Errorf("SoftDeleteWhere(nil) = %v, want nil", got)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("empty filter returns ErrEmptyFilter and deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, &models.DocumentFilter{})
		if !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("SoftDeleteWhere(&DocumentFilter{}) error = %v, want ErrEmptyFilter", err)
		}
		if got != nil {
			t.Errorf("SoftDeleteWhere(&DocumentFilter{}) = %v, want nil", got)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("a filter matching no row returns an empty result and no error", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("missing"))
		if err != nil {
			t.Fatalf("SoftDeleteWhere(title in [missing]) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("SoftDeleteWhere(title in [missing]) = %v, want an empty result", got)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("soft-deleted rows leave default reads and stay readable with a deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a"))
		if err != nil {
			t.Fatalf("SoftDeleteWhere(title in [a]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdwDocumentIDs(sdwLiveDocuments(t, h))); diff != "" {
			t.Errorf("documents a default read returns (-want +got):\n%s", diff)
		}
		// The returned entity is the soft-deleted row as now stored.
		if diff := cmp.Diff(sdwDeletedDocuments(t, h, a.ID), got); diff != "" {
			t.Errorf("SoftDeleteWhere result against the stored deleted row (-stored +returned):\n%s", diff)
		}
		assertRowCount(t, h, "documents", 2)
	})

	t.Run("soft-deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		b := createDocument(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().SoftDeleteWhere(txCtx, sdwTitleIn("a")); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("SoftDeleteWhere inside the transaction sent %q outside it, want nothing", stmts)
			}
			n, err := h.Client.Documents().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("Count inside the transaction = %d, want 1", n)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdwDocumentIDs(sdwLiveDocuments(t, h))); diff != "" {
			t.Errorf("live documents after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves rows live", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().SoftDeleteWhere(txCtx, sdwTitleIn("a")); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Documents().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("Count inside the transaction = %d, want 0", n)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("fires one soft_delete_where mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		createDocument(t, h, "other")
		sdwSeedDeleted(t, h, "gone")
		client, seen := mutationRecorder(h)
		filter := sdwTitleIn("a", "b", "gone")
		if _, err := client.Documents().SoftDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("SoftDeleteWhere(title in [a, b, gone]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpSoftDeleteWhere || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpSoftDeleteWhere, models.TableDocuments)
		}
		if m.Input != any(filter) {
			t.Errorf("MutationContext.Input = %v, want the caller's filter %p", m.Input, filter)
		}
		if diff := cmp.Diff([]any{a.ID, b.ID}, m.AffectedPKs, sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("soft-deleting a row a CHECK forbids returns a check ConstraintError and deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		createDocument(t, h, "pinned")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a", "pinned"))
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("SoftDeleteWhere(title in [a, pinned]) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		if got != nil {
			t.Errorf("SoftDeleteWhere result on error = %v, want nil", got)
		}
		// The write is one statement, so the matched row beside pinned stays live too.
		assertLiveDocuments(t, h, 2)
	})

	t.Run("a partial field selection returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a", "b"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{ID: true, Title: true}
		})
		if err != nil {
			t.Fatalf("SoftDeleteWhere(selecting id, title) error = %v, want nil", err)
		}
		// A full entity would carry the deleted_at the write just set.
		want := []*models.Document{{ID: a.ID, Title: "a"}, {ID: b.ID, Title: "b"}}
		if diff := cmp.Diff(want, got, sdwDocumentsByID); diff != "" {
			t.Errorf("SoftDeleteWhere(selecting id, title) (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("an empty field selection soft-deletes, returns nil and still records the affected keys", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		createDocument(t, h, "other")
		client, affected := affectedPKsRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a", "b"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil {
			t.Fatalf("SoftDeleteWhere(selecting nothing) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("SoftDeleteWhere(selecting nothing) = %v, want nil", got)
		}
		// The write is the last statement: no terminal read follows it.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "UPDATE") {
			t.Errorf("SoftDeleteWhere(selecting nothing) sent %q, want the UPDATE last", stmts)
		}
		if diff := cmp.Diff([]any{a.ID, b.ID}, affected(), sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs of SoftDeleteWhere(selecting nothing) (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("a lock mode is ignored outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("SoftDeleteWhere with LockForUpdate error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdwDocumentIDs(got)); diff != "" {
			t.Errorf("SoftDeleteWhere with LockForUpdate returned documents (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("composite keys soft-delete and return exactly the matched tuples", func(t *testing.T) {
		h.Reset(t)
		matched := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}}
		// The trashed keys cross the matched ones and match the filter too, but are
		// already deleted: a re-fetch by per-column key values would return them.
		trashed := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		// unmatched shares its number with a matched key but not its document_id.
		unmatched := models.RevisionPK{DocumentID: 3, Number: 1}
		for _, pk := range []models.RevisionPK{matched[0], matched[1], trashed[0], trashed[1], unmatched} {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, trashed); err != nil {
			t.Fatalf("SoftDeleteMany(%+v) error = %v", trashed, err)
		}
		client, affected := affectedPKsRecorder(h)
		got, err := client.Revisions().SoftDeleteWhere(ctx, sdwDocumentIDIn(1, 2))
		if err != nil {
			t.Fatalf("SoftDeleteWhere(document_id in [1, 2]) error = %v, want nil", err)
		}
		if diff := cmp.Diff(matched, sdwRevisionPKs(got), sdwByRevisionPK); diff != "" {
			t.Errorf("SoftDeleteWhere(document_id in [1, 2]) returned revisions (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{matched[0], matched[1]}, affected(), sdwByAnyRevisionPK); diff != "" {
			t.Errorf("AffectedPKs of SoftDeleteWhere(document_id in [1, 2]) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]models.RevisionPK{unmatched}, sdwRevisionPKs(sdwRevisions(t, h, nil))); diff != "" {
			t.Errorf("live revisions after SoftDeleteWhere (-want +got):\n%s", diff)
		}
	})
}

// restoreWhere: RestoreWhere clears the soft-delete marker on every deleted row the filter matches.
func restoreWhere(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("restores only the deleted rows the filter matches and returns them", func(t *testing.T) {
		h.Reset(t)
		a, b, other := sdwSeedDeleted(t, h, "a"), sdwSeedDeleted(t, h, "b"), sdwSeedDeleted(t, h, "other")
		// live matches the filter but is not deleted, so the restore must skip it.
		live := createDocument(t, h, "live")
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("a", "b", "live"))
		if err != nil {
			t.Fatalf("RestoreWhere(title in [a, b, live]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a, b}, sdwDocumentIDs(got), sdwByValue); diff != "" {
			t.Errorf("RestoreWhere returned documents (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a, b, live.ID}, sdwDocumentIDs(sdwLiveDocuments(t, h)), sdwByValue); diff != "" {
			t.Errorf("live documents after RestoreWhere (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{other}, sdwDocumentIDs(sdwDeletedDocuments(t, h))); diff != "" {
			t.Errorf("deleted documents after RestoreWhere (-want +got):\n%s", diff)
		}
	})

	t.Run("nil filter returns ErrEmptyFilter and restores nothing", func(t *testing.T) {
		h.Reset(t)
		sdwSeedDeleted(t, h, "a")
		got, err := h.Client.Documents().RestoreWhere(ctx, nil)
		if !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("RestoreWhere(nil) error = %v, want ErrEmptyFilter", err)
		}
		if got != nil {
			t.Errorf("RestoreWhere(nil) = %v, want nil", got)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("empty filter returns ErrEmptyFilter and restores nothing", func(t *testing.T) {
		h.Reset(t)
		sdwSeedDeleted(t, h, "a")
		got, err := h.Client.Documents().RestoreWhere(ctx, &models.DocumentFilter{})
		if !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("RestoreWhere(&DocumentFilter{}) error = %v, want ErrEmptyFilter", err)
		}
		if got != nil {
			t.Errorf("RestoreWhere(&DocumentFilter{}) = %v, want nil", got)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("a filter matching no row returns an empty result and no error", func(t *testing.T) {
		h.Reset(t)
		sdwSeedDeleted(t, h, "a")
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("missing"))
		if err != nil {
			t.Fatalf("RestoreWhere(title in [missing]) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("RestoreWhere(title in [missing]) = %v, want an empty result", got)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("restored rows return to default reads", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		createDocument(t, h, "b")
		if _, err := h.Client.Documents().SoftDeleteWhere(ctx, sdwTitleIn("a", "b")); err != nil {
			t.Fatalf("SoftDeleteWhere(title in [a, b]) error = %v", err)
		}
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("a"))
		if err != nil {
			t.Fatalf("RestoreWhere(title in [a]) error = %v, want nil", err)
		}
		live := sdwLiveDocuments(t, h)
		if diff := cmp.Diff([]int64{a.ID}, sdwDocumentIDs(live)); diff != "" {
			t.Errorf("documents a default read returns (-want +got):\n%s", diff)
		}
		// The returned entity is the restored row as a default read now sees it.
		if diff := cmp.Diff(live, got); diff != "" {
			t.Errorf("RestoreWhere result against the stored live row (-stored +returned):\n%s", diff)
		}
	})

	t.Run("restores inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a := sdwSeedDeleted(t, h, "a")
		sdwSeedDeleted(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().RestoreWhere(txCtx, sdwTitleIn("a")); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("RestoreWhere inside the transaction sent %q outside it, want nothing", stmts)
			}
			n, err := h.Client.Documents().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("Count inside the transaction = %d, want 1", n)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a}, sdwDocumentIDs(sdwLiveDocuments(t, h))); diff != "" {
			t.Errorf("live documents after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves rows deleted", func(t *testing.T) {
		h.Reset(t)
		sdwSeedDeleted(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().RestoreWhere(txCtx, sdwTitleIn("a")); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Documents().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("Count inside the transaction = %d, want 1", n)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("fires one restore_where mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b := sdwSeedDeleted(t, h, "a"), sdwSeedDeleted(t, h, "b")
		sdwSeedDeleted(t, h, "other")
		createDocument(t, h, "live")
		client, seen := mutationRecorder(h)
		filter := sdwTitleIn("a", "b", "live")
		if _, err := client.Documents().RestoreWhere(ctx, filter); err != nil {
			t.Fatalf("RestoreWhere(title in [a, b, live]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpRestoreWhere || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpRestoreWhere, models.TableDocuments)
		}
		if m.Input != any(filter) {
			t.Errorf("MutationContext.Input = %v, want the caller's filter %p", m.Input, filter)
		}
		if diff := cmp.Diff([]any{a, b}, m.AffectedPKs, sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("restoring a row a CHECK forbids returns a check ConstraintError and restores nothing", func(t *testing.T) {
		h.Reset(t)
		sdwSeedDeleted(t, h, "a")
		sdwSeedDeleted(t, h, "archived")
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("a", "archived"))
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("RestoreWhere(title in [a, archived]) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		if got != nil {
			t.Errorf("RestoreWhere result on error = %v, want nil", got)
		}
		// The write is one statement, so the matched row beside archived stays deleted too.
		assertLiveDocuments(t, h, 0)
	})

	t.Run("a partial field selection returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a, b := sdwSeedDeleted(t, h, "a"), sdwSeedDeleted(t, h, "b")
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("a", "b"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{ID: true}
		})
		if err != nil {
			t.Fatalf("RestoreWhere(selecting id) error = %v, want nil", err)
		}
		// A full entity would carry the title.
		want := []*models.Document{{ID: a}, {ID: b}}
		if diff := cmp.Diff(want, got, sdwDocumentsByID); diff != "" {
			t.Errorf("RestoreWhere(selecting id) (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 2)
	})

	t.Run("an empty field selection restores, returns nil and still records the affected keys", func(t *testing.T) {
		h.Reset(t)
		a, b := sdwSeedDeleted(t, h, "a"), sdwSeedDeleted(t, h, "b")
		sdwSeedDeleted(t, h, "other")
		client, affected := affectedPKsRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Documents().RestoreWhere(ctx, sdwTitleIn("a", "b"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil {
			t.Fatalf("RestoreWhere(selecting nothing) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("RestoreWhere(selecting nothing) = %v, want nil", got)
		}
		// The write is the last statement: no terminal read follows it.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "UPDATE") {
			t.Errorf("RestoreWhere(selecting nothing) sent %q, want the UPDATE last", stmts)
		}
		if diff := cmp.Diff([]any{a, b}, affected(), sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs of RestoreWhere(selecting nothing) (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 2)
	})

	t.Run("a lock mode is ignored outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := sdwSeedDeleted(t, h, "a")
		got, err := h.Client.Documents().RestoreWhere(ctx, sdwTitleIn("a"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("RestoreWhere with LockForUpdate error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a}, sdwDocumentIDs(got)); diff != "" {
			t.Errorf("RestoreWhere with LockForUpdate returned documents (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("composite keys restore and return exactly the matched tuples", func(t *testing.T) {
		h.Reset(t)
		matched := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}}
		// The live keys cross the matched ones and match the filter too, but are
		// not deleted: a re-fetch by per-column key values would return them.
		live := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		// unmatched shares its number with a matched key but not its document_id.
		unmatched := models.RevisionPK{DocumentID: 3, Number: 1}
		for _, pk := range []models.RevisionPK{matched[0], matched[1], live[0], live[1], unmatched} {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, []models.RevisionPK{matched[0], matched[1], unmatched}); err != nil {
			t.Fatalf("SoftDeleteMany error = %v", err)
		}
		client, affected := affectedPKsRecorder(h)
		got, err := client.Revisions().RestoreWhere(ctx, sdwDocumentIDIn(1, 2))
		if err != nil {
			t.Fatalf("RestoreWhere(document_id in [1, 2]) error = %v, want nil", err)
		}
		if diff := cmp.Diff(matched, sdwRevisionPKs(got), sdwByRevisionPK); diff != "" {
			t.Errorf("RestoreWhere(document_id in [1, 2]) returned revisions (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{matched[0], matched[1]}, affected(), sdwByAnyRevisionPK); diff != "" {
			t.Errorf("AffectedPKs of RestoreWhere(document_id in [1, 2]) (-want +got):\n%s", diff)
		}
		deleted := sdwRevisions(t, h, &models.RevisionFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}})
		if diff := cmp.Diff([]models.RevisionPK{unmatched}, sdwRevisionPKs(deleted)); diff != "" {
			t.Errorf("deleted revisions after RestoreWhere (-want +got):\n%s", diff)
		}
	})
}

var (
	sdwByValue         = cmpopts.SortSlices(func(x, y int64) bool { return x < y })
	sdwByAnyInt64      = cmpopts.SortSlices(func(x, y any) bool { return x.(int64) < y.(int64) })
	sdwDocumentsByID   = cmpopts.SortSlices(func(x, y *models.Document) bool { return x.ID < y.ID })
	sdwByRevisionPK    = cmpopts.SortSlices(sdwRevisionPKLess)
	sdwByAnyRevisionPK = cmpopts.SortSlices(func(x, y any) bool {
		return sdwRevisionPKLess(x.(models.RevisionPK), y.(models.RevisionPK))
	})
)

func sdwRevisionPKLess(x, y models.RevisionPK) bool {
	if x.DocumentID != y.DocumentID {
		return x.DocumentID < y.DocumentID
	}
	return x.Number < y.Number
}

// sdwTitleIn is a document filter matching any of the titles.
func sdwTitleIn(titles ...string) *models.DocumentFilter {
	return &models.DocumentFilter{Title: &comparator.String{In: titles}}
}

// sdwDocumentIDIn is a revision filter matching the listed documents' revisions.
func sdwDocumentIDIn(ids ...int64) *models.RevisionFilter {
	return &models.RevisionFilter{DocumentID: &comparator.Number[int64]{In: ids}}
}

// sdwSeedDeleted inserts a document that is already soft-deleted, at a fixed
// past time, and returns its key. title must be a literal no other row uses.
func sdwSeedDeleted(t *testing.T, h *Harness, title string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := h.Querier.Inner.Exec(ctx, "INSERT INTO documents (title, deleted_at) VALUES ('"+title+"', '2000-01-01 00:00:00')"); err != nil {
		t.Fatalf("seed deleted document %q: %v", title, err)
	}
	var id int64
	if err := h.Querier.Inner.QueryRow(ctx, "SELECT id FROM documents WHERE title = '"+title+"'").Scan(&id); err != nil {
		t.Fatalf("read back deleted document %q: %v", title, err)
	}
	return id
}

// sdwLiveDocuments returns the documents a default read sees, by key.
func sdwLiveDocuments(t *testing.T, h *Harness) []*models.Document {
	t.Helper()
	return sdwDocuments(t, h, nil)
}

// sdwDeletedDocuments returns the soft-deleted documents, all of them or only
// the listed keys, by key.
func sdwDeletedDocuments(t *testing.T, h *Harness, ids ...int64) []*models.Document {
	t.Helper()
	f := &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}}
	if len(ids) > 0 {
		f.ID = &comparator.Number[int64]{In: ids}
	}
	return sdwDocuments(t, h, f)
}

func sdwDocuments(t *testing.T, h *Harness, f *models.DocumentFilter) []*models.Document {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: f,
		Sorts:  []sql.Sort{{Column: "id", Direction: sql.Asc}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany error = %v", err)
	}
	return docs
}

func sdwRevisions(t *testing.T, h *Harness, f *models.RevisionFilter) []*models.Revision {
	t.Helper()
	revs, err := h.Client.Revisions().GetMany(context.Background(), &models.GetRevisionsInput{Filter: f})
	if err != nil {
		t.Fatalf("Revisions().GetMany error = %v", err)
	}
	return revs
}

func sdwDocumentIDs(docs []*models.Document) []int64 {
	var ids []int64
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids
}

func sdwRevisionPKs(revs []*models.Revision) []models.RevisionPK {
	var pks []models.RevisionPK
	for _, r := range revs {
		pks = append(pks, models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number})
	}
	return pks
}
