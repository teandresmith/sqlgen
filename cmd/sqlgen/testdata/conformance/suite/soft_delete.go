package suite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// softDelete: SoftDelete marks one row as deleted and returns it.
func softDelete(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("marks only the given row deleted and returns it", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDelete(ctx, a.ID)
		if err != nil {
			t.Fatalf("SoftDelete(a) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || got.Title != "a" {
			t.Fatalf("SoftDelete(a) = %+v, want document %d titled %q", got, a.ID, "a")
		}
		if sdlIsZero(got.DeletedAt) {
			t.Errorf("SoftDelete(a) returned DeletedAt unset, want the soft-delete column set")
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
		sdlAssertDocumentState(t, h, b.ID, sdlLive)
	})

	t.Run("missing key returns nil and no error", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		missing := a.ID + 1000
		got, err := h.Client.Documents().SoftDelete(ctx, missing)
		if err != nil || got != nil {
			t.Fatalf("SoftDelete(missing) = %+v, %v, want nil, nil", got, err)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("soft-deleted row leaves default reads and is found by a deleted-at filter", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDelete(ctx, a.ID)
		if err != nil || got == nil {
			t.Fatalf("SoftDelete(a) = %+v, %v, want the document", got, err)
		}
		assertLiveDocuments(t, h, 1)
		live, err := h.Client.Documents().GetMany(ctx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}},
		})
		if err != nil {
			t.Fatalf("GetMany(a) error = %v", err)
		}
		if len(live) != 0 {
			t.Errorf("GetMany(a) with default scoping = %d rows, want 0", len(live))
		}
		deleted, err := h.Client.Documents().GetMany(ctx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
		})
		if err != nil {
			t.Fatalf("GetMany(deleted) error = %v", err)
		}
		if len(deleted) != 1 {
			t.Fatalf("GetMany(deleted) = %d rows, want 1", len(deleted))
		}
		if diff := cmp.Diff(deleted[0], got); diff != "" {
			t.Errorf("SoftDelete(a) result against the soft-deleted row (-row +returned):\n%s", diff)
		}
	})

	t.Run("soft-deleting a soft-deleted row returns it and records the key", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		if _, err := h.Client.Documents().SoftDelete(ctx, a.ID); err != nil {
			t.Fatalf("first SoftDelete(a) error = %v", err)
		}
		client, affected := affectedPKsRecorder(h)
		got, err := client.Documents().SoftDelete(ctx, a.ID)
		if err != nil {
			t.Fatalf("second SoftDelete(a) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID {
			t.Fatalf("second SoftDelete(a) = %+v, want document %d", got, a.ID)
		}
		if diff := cmp.Diff([]any{a.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of a second SoftDelete(a) (-want +got):\n%s", diff)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("soft-deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().SoftDelete(txCtx, a.ID); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("SoftDelete inside the transaction sent %q outside it, want nothing", stmts)
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
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
		sdlAssertDocumentState(t, h, b.ID, sdlLive)
	})

	t.Run("rolled-back transaction leaves the row live", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().SoftDelete(txCtx, a.ID); err != nil {
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
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("fires one soft_delete mutation hook", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		client, seen := mutationRecorder(h)
		if _, err := client.Documents().SoftDelete(ctx, a.ID); err != nil {
			t.Fatalf("SoftDelete(a) error = %v, want nil", err)
		}
		ms := seen()
		if len(ms) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(ms))
		}
		m := ms[0]
		if m.Op != hook.OpSoftDelete || m.Table != models.TableDocuments || m.PK != any(a.ID) {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %d}",
				m.Op, m.Table, m.PK, hook.OpSoftDelete, models.TableDocuments, a.ID)
		}
		if diff := cmp.Diff([]any{a.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("a key matching no row runs the hook and records no affected key", func(t *testing.T) {
		h.Reset(t)
		missing := createDocument(t, h, "a").ID + 1000
		client, seen := mutationRecorder(h)
		if _, err := client.Documents().SoftDelete(ctx, missing); err != nil {
			t.Fatalf("SoftDelete(missing) error = %v, want nil", err)
		}
		ms := seen()
		if len(ms) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(ms))
		}
		if ms[0].PK != any(missing) {
			t.Errorf("MutationContext.PK = %v, want %d", ms[0].PK, missing)
		}
		if got := ms[0].AffectedPKs; len(got) != 0 {
			t.Errorf("AffectedPKs of a SoftDelete matching no row = %v, want none", got)
		}
	})

	t.Run("soft-deleting a row a CHECK forbids returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		pinned := createDocument(t, h, "pinned")
		got, err := h.Client.Documents().SoftDelete(ctx, pinned.ID)
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("SoftDelete(pinned) = %+v, %v, want a *ConstraintError", got, err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		sdlAssertDocumentState(t, h, pinned.ID, sdlLive)
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDelete(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{ID: true, DeletedAt: true}
		})
		if err != nil || got == nil {
			t.Fatalf("SoftDelete(a, {ID, DeletedAt}) = %+v, %v, want the document", got, err)
		}
		if got.ID != a.ID {
			t.Errorf("selected ID = %d, want %d", got.ID, a.ID)
		}
		if sdlIsZero(got.DeletedAt) {
			t.Errorf("selected DeletedAt is unset, want the soft-delete column set")
		}
		if got.Title != "" {
			t.Errorf("unselected Title = %q, want the zero value", got.Title)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("empty field options soft-deletes and returns nil", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().SoftDelete(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil || got != nil {
			t.Fatalf("SoftDelete(a, {}) = %+v, %v, want nil, nil", got, err)
		}
		if reads := statementsWithPrefix(h, "SELECT"); len(reads) != 0 {
			t.Errorf("SoftDelete(a, {}) sent %q, want no follow-up read", reads)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		got, err := h.Client.Documents().SoftDelete(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("SoftDelete(a) under LockForUpdate error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID {
			t.Errorf("SoftDelete(a) under LockForUpdate = %+v, want document %d", got, a.ID)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("composite key soft-deletes only the listed tuple", func(t *testing.T) {
		h.Reset(t)
		listed := models.RevisionPK{DocumentID: 1, Number: 1}
		// Each unlisted key shares one column value with the listed one.
		unlisted := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		for _, pk := range append([]models.RevisionPK{listed}, unlisted...) {
			createRevision(t, h, pk)
		}
		got, err := h.Client.Revisions().SoftDelete(ctx, listed)
		if err != nil {
			t.Fatalf("SoftDelete(%+v) error = %v, want nil", listed, err)
		}
		if got == nil || got.DocumentID != listed.DocumentID || got.Number != listed.Number {
			t.Errorf("SoftDelete(%+v) = %+v, want that revision", listed, got)
		}
		sdlAssertRevisionState(t, h, listed, sdlDeleted)
		for _, pk := range unlisted {
			sdlAssertRevisionState(t, h, pk, sdlLive)
		}
	})
}

// restore: Restore clears the soft-delete marker on one row and returns it.
func restore(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("restores only the given row and returns it", func(t *testing.T) {
		h.Reset(t)
		a, b := createDeletedDocument(t, h, "a"), createDeletedDocument(t, h, "b")
		got, err := h.Client.Documents().Restore(ctx, a.ID)
		if err != nil {
			t.Fatalf("Restore(a) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID || got.Title != "a" {
			t.Fatalf("Restore(a) = %+v, want document %d titled %q", got, a.ID, "a")
		}
		if !sdlIsZero(got.DeletedAt) {
			t.Errorf("Restore(a) returned DeletedAt = %v, want the soft-delete column cleared", got.DeletedAt)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
		sdlAssertDocumentState(t, h, b.ID, sdlDeleted)
	})

	t.Run("missing key returns nil and no error", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		missing := a.ID + 1000
		got, err := h.Client.Documents().Restore(ctx, missing)
		if err != nil || got != nil {
			t.Fatalf("Restore(missing) = %+v, %v, want nil, nil", got, err)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("restored row returns to default reads", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		createDocument(t, h, "b")
		assertLiveDocuments(t, h, 1)
		got, err := h.Client.Documents().Restore(ctx, a.ID)
		if err != nil || got == nil {
			t.Fatalf("Restore(a) = %+v, %v, want the document", got, err)
		}
		assertLiveDocuments(t, h, 2)
		live, err := h.Client.Documents().GetMany(ctx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}},
		})
		if err != nil {
			t.Fatalf("GetMany(a) error = %v", err)
		}
		if len(live) != 1 {
			t.Fatalf("GetMany(a) with default scoping = %d rows, want 1", len(live))
		}
		if diff := cmp.Diff(live[0], got); diff != "" {
			t.Errorf("Restore(a) result against the restored row (-row +returned):\n%s", diff)
		}
	})

	t.Run("restoring a live row returns it and records the key", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		client, affected := affectedPKsRecorder(h)
		got, err := client.Documents().Restore(ctx, a.ID)
		if err != nil {
			t.Fatalf("Restore(live a) error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID {
			t.Fatalf("Restore(live a) = %+v, want document %d", got, a.ID)
		}
		if diff := cmp.Diff([]any{a.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Restore(live a) (-want +got):\n%s", diff)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("restores inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDeletedDocument(t, h, "a"), createDeletedDocument(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().Restore(txCtx, a.ID); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Restore inside the transaction sent %q outside it, want nothing", stmts)
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
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
		sdlAssertDocumentState(t, h, b.ID, sdlDeleted)
	})

	t.Run("rolled-back transaction leaves the row deleted", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().Restore(txCtx, a.ID); err != nil {
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
		sdlAssertDocumentState(t, h, a.ID, sdlDeleted)
	})

	t.Run("fires one restore mutation hook", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		client, seen := mutationRecorder(h)
		if _, err := client.Documents().Restore(ctx, a.ID); err != nil {
			t.Fatalf("Restore(a) error = %v, want nil", err)
		}
		ms := seen()
		if len(ms) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(ms))
		}
		m := ms[0]
		if m.Op != hook.OpRestore || m.Table != models.TableDocuments || m.PK != any(a.ID) {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %d}",
				m.Op, m.Table, m.PK, hook.OpRestore, models.TableDocuments, a.ID)
		}
		if diff := cmp.Diff([]any{a.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("a key matching no row runs the hook and records no affected key", func(t *testing.T) {
		h.Reset(t)
		missing := createDeletedDocument(t, h, "a").ID + 1000
		client, seen := mutationRecorder(h)
		if _, err := client.Documents().Restore(ctx, missing); err != nil {
			t.Fatalf("Restore(missing) error = %v, want nil", err)
		}
		ms := seen()
		if len(ms) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(ms))
		}
		if ms[0].PK != any(missing) {
			t.Errorf("MutationContext.PK = %v, want %d", ms[0].PK, missing)
		}
		if got := ms[0].AffectedPKs; len(got) != 0 {
			t.Errorf("AffectedPKs of a Restore matching no row = %v, want none", got)
		}
	})

	t.Run("restoring a row a CHECK forbids returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		archived := seedArchivedDocument(t, h)
		got, err := h.Client.Documents().Restore(ctx, archived)
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("Restore(archived) = %+v, %v, want a *ConstraintError", got, err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		sdlAssertDocumentState(t, h, archived, sdlDeleted)
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		got, err := h.Client.Documents().Restore(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{Title: true}
		})
		if err != nil || got == nil {
			t.Fatalf("Restore(a, {Title}) = %+v, %v, want the document", got, err)
		}
		if got.Title != "a" {
			t.Errorf("selected Title = %q, want %q", got.Title, "a")
		}
		if got.ID != 0 {
			t.Errorf("unselected ID = %d, want the zero value", got.ID)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("empty field options restores and returns nil", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().Restore(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil || got != nil {
			t.Fatalf("Restore(a, {}) = %+v, %v, want nil, nil", got, err)
		}
		if reads := statementsWithPrefix(h, "SELECT"); len(reads) != 0 {
			t.Errorf("Restore(a, {}) sent %q, want no follow-up read", reads)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := createDeletedDocument(t, h, "a")
		got, err := h.Client.Documents().Restore(ctx, a.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("Restore(a) under LockForUpdate error = %v, want nil", err)
		}
		if got == nil || got.ID != a.ID {
			t.Errorf("Restore(a) under LockForUpdate = %+v, want document %d", got, a.ID)
		}
		sdlAssertDocumentState(t, h, a.ID, sdlLive)
	})

	t.Run("composite key restores only the listed tuple", func(t *testing.T) {
		h.Reset(t)
		listed := models.RevisionPK{DocumentID: 1, Number: 1}
		// Each unlisted key shares one column value with the listed one.
		unlisted := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		all := append([]models.RevisionPK{listed}, unlisted...)
		for _, pk := range all {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, all); err != nil {
			t.Fatalf("SoftDeleteMany(revisions) error = %v", err)
		}
		got, err := h.Client.Revisions().Restore(ctx, listed)
		if err != nil {
			t.Fatalf("Restore(%+v) error = %v, want nil", listed, err)
		}
		if got == nil || got.DocumentID != listed.DocumentID || got.Number != listed.Number {
			t.Errorf("Restore(%+v) = %+v, want that revision", listed, got)
		}
		sdlAssertRevisionState(t, h, listed, sdlLive)
		for _, pk := range unlisted {
			sdlAssertRevisionState(t, h, pk, sdlDeleted)
		}
	})
}

// sdlState is a row's soft-delete state.
type sdlState string

const (
	sdlLive    sdlState = "live"
	sdlDeleted sdlState = "soft-deleted"
	sdlAbsent  sdlState = "absent"
)

// sdlIsZero reports whether v is its type's zero value, so a soft-delete
// column is checked without naming the dialect's timestamp type.
func sdlIsZero(v any) bool {
	return reflect.ValueOf(v).IsZero()
}

// sdlStateOf turns the row counts matched by an IS NULL and an IS NOT NULL
// deleted-at filter into a state.
func sdlStateOf(t *testing.T, live, deleted int64) sdlState {
	t.Helper()
	switch {
	case live == 1 && deleted == 0:
		return sdlLive
	case live == 0 && deleted == 1:
		return sdlDeleted
	case live == 0 && deleted == 0:
		return sdlAbsent
	}
	t.Fatalf("row matched %d live and %d soft-deleted rows", live, deleted)
	return ""
}

// sdlAssertDocumentState checks document id's soft-delete state with explicit
// deleted-at comparators, independent of the default scoping.
func sdlAssertDocumentState(t *testing.T, h *Harness, id int64, want sdlState) {
	t.Helper()
	count := func(null bool) int64 {
		n, err := h.Client.Documents().Count(context.Background(), &models.DocumentFilter{
			ID:        &comparator.Number[int64]{Eq: new(id)},
			DeletedAt: &comparator.NullableTime{Null: new(null)},
		})
		if err != nil {
			t.Fatalf("Documents().Count(%d) error = %v", id, err)
		}
		return n
	}
	if got := sdlStateOf(t, count(true), count(false)); got != want {
		t.Errorf("document %d is %s, want %s", id, got, want)
	}
}

// sdlAssertRevisionState checks revision pk's soft-delete state.
func sdlAssertRevisionState(t *testing.T, h *Harness, pk models.RevisionPK, want sdlState) {
	t.Helper()
	count := func(null bool) int64 {
		n, err := h.Client.Revisions().Count(context.Background(), &models.RevisionFilter{
			DocumentID: &comparator.Number[int64]{Eq: new(pk.DocumentID)},
			Number:     &comparator.Number[int64]{Eq: new(pk.Number)},
			DeletedAt:  &comparator.NullableTime{Null: new(null)},
		})
		if err != nil {
			t.Fatalf("Revisions().Count(%+v) error = %v", pk, err)
		}
		return n
	}
	if got := sdlStateOf(t, count(true), count(false)); got != want {
		t.Errorf("revision %+v is %s, want %s", pk, got, want)
	}
}
