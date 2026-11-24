package suite

import (
	"context"
	"errors"
	"slices"
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

// softDeleteMany: SoftDeleteMany marks every listed row as deleted.
func softDeleteMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a key listed twice across a sub-batch boundary is read back once", func(t *testing.T) {
		h.Reset(t)
		// revisions has batch_size: 2, so the repeated key opens the second chunk.
		sdmSeedRevisions(t, h, 2)
		pks := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 1, Number: 2}, {DocumentID: 1, Number: 1}}
		got, err := h.Client.Revisions().SoftDeleteMany(ctx, pks)
		if err != nil {
			t.Fatalf("SoftDeleteMany(keys 1, 2, 1) error = %v, want nil", err)
		}
		numbers := make([]int64, len(got))
		for i, r := range got {
			numbers[i] = r.Number
		}
		slices.Sort(numbers)
		if diff := cmp.Diff([]int64{1, 2}, numbers); diff != "" {
			t.Errorf("SoftDeleteMany(keys 1, 2, 1) returned revisions (-want +got):\n%s", diff)
		}
	})

	t.Run("reads back the soft-deleted rows in chunks of the table's batch size", func(t *testing.T) {
		h.Reset(t)
		// revisions has batch_size: 2, so three keys are two UPDATEs followed by
		// two reads. A dialect without RETURNING reads each sub-batch's keys
		// before its UPDATE, so only the statements after the last UPDATE are
		// the read-back.
		pks := sdmSeedRevisions(t, h, 3)
		h.Querier.ResetCount()
		got, err := h.Client.Revisions().SoftDeleteMany(ctx, pks)
		if err != nil {
			t.Fatalf("SoftDeleteMany(3 revisions) error = %v, want nil", err)
		}
		verbs := usStatementVerbs(h)
		last := -1
		for i, v := range verbs {
			if v == "UPDATE" {
				last = i
			}
		}
		if sdmCount(verbs, "UPDATE") != 2 {
			t.Fatalf("SoftDeleteMany(3 revisions) statements = %v, want two UPDATEs", verbs)
		}
		if diff := cmp.Diff([]string{"SELECT", "SELECT"}, verbs[last+1:]); diff != "" {
			t.Errorf("SoftDeleteMany(3 revisions) read-back statements (-want +got):\n%s", diff)
		}
		if len(got) != 3 {
			t.Errorf("SoftDeleteMany(3 revisions) returned %d rows, want 3", len(got))
		}
	})

	t.Run("composite keys past SQLite's expression depth limit read back every written row", func(t *testing.T) {
		h.Reset(t)
		// Each read-back names the written keys. One statement naming 1000
		// composite keys renders an OR of 1000 ANDs, past SQLite's depth limit
		// ("Expression tree is too large"), after every sub-batch is written.
		const n = 1000
		pks := sdmSeedRevisions(t, h, n)
		all := &models.RevisionFilter{DocumentID: &comparator.Number[int64]{Eq: new(int64(1))}}
		steps := []struct {
			name string
			call func() ([]*models.Revision, error)
		}{
			{"SoftDeleteMany", func() ([]*models.Revision, error) { return h.Client.Revisions().SoftDeleteMany(ctx, pks) }},
			{"RestoreMany", func() ([]*models.Revision, error) { return h.Client.Revisions().RestoreMany(ctx, pks) }},
			{"SoftDeleteWhere", func() ([]*models.Revision, error) { return h.Client.Revisions().SoftDeleteWhere(ctx, all) }},
			{"RestoreWhere", func() ([]*models.Revision, error) { return h.Client.Revisions().RestoreWhere(ctx, all) }},
		}
		for _, step := range steps {
			got, err := step.call()
			if err != nil {
				t.Errorf("%s(%d revisions) error = %v, want nil", step.name, n, err)
				continue
			}
			if len(got) != n {
				t.Errorf("%s(%d revisions) returned %d rows, want %d", step.name, n, len(got), n)
			}
		}
	})

	t.Run("soft-deletes only the listed keys and returns them", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createDocument(t, h, "a"), createDocument(t, h, "b"), createDocument(t, h, "c")
		// An unlisted row deleted earlier must not come back as one this call wrote.
		earlier := createDocument(t, h, "earlier")
		sdmSoftDeleteDocuments(t, h, earlier.ID)
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID, c.ID})
		if err != nil {
			t.Fatalf("SoftDeleteMany([a, c]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a.ID, c.ID}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("SoftDeleteMany([a, c]) returned keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after SoftDeleteMany([a, c]) (-want +got):\n%s", diff)
		}
	})

	t.Run("nil key list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().SoftDeleteMany(ctx, nil)
		if err != nil {
			t.Fatalf("SoftDeleteMany(nil) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("SoftDeleteMany(nil) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("SoftDeleteMany(nil) returned %d documents, want none", len(got))
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("empty key list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{})
		if err != nil {
			t.Fatalf("SoftDeleteMany([]) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("SoftDeleteMany([]) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("SoftDeleteMany([]) returned %d documents, want none", len(got))
		}
		assertLiveDocuments(t, h, 1)
	})

	t.Run("missing keys are not an error and are not returned", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		missing := d.ID + 1000
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{missing, d.ID})
		if err != nil {
			t.Fatalf("SoftDeleteMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{d.ID}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("SoftDeleteMany([missing, d]) returned keys (-want +got):\n%s", diff)
		}
		got, err = h.Client.Documents().SoftDeleteMany(ctx, []int64{missing})
		if err != nil {
			t.Fatalf("SoftDeleteMany([missing]) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("SoftDeleteMany([missing]) returned %d documents, want none", len(got))
		}
	})

	t.Run("soft-deleted rows leave default reads and stay readable through the DeletedAt comparator", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID})
		if err != nil {
			t.Fatalf("SoftDeleteMany([a]) error = %v, want nil", err)
		}
		// The returned entity is the soft-deleted row: its marker is set.
		if len(got) != 1 || got[0].ID != a.ID || !sdmDocumentDeleted(got[0]) {
			t.Errorf("SoftDeleteMany([a]) returned %+v, want only a with DeletedAt set", got)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("GetMany with no DeletedAt comparator (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("GetMany with DeletedAt IS NOT NULL (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "documents", 2)
	})

	t.Run("soft-deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().SoftDeleteMany(txCtx, []int64{a.ID}); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("SoftDeleteMany inside the transaction sent %q outside it, want nothing", stmts)
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
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves rows live", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().SoftDeleteMany(txCtx, []int64{a.ID}); err != nil {
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
		if diff := cmp.Diff([]int64{a.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one soft_delete_many mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createDocument(t, h, "a"), createDocument(t, h, "b"), createDocument(t, h, "c")
		client, seen := mutationRecorder(h)
		// Listed out of key order: the affected keys follow the list.
		if _, err := client.Documents().SoftDeleteMany(ctx, []int64{c.ID, a.ID}); err != nil {
			t.Fatalf("SoftDeleteMany([c, a]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpSoftDeleteMany || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpSoftDeleteMany, models.TableDocuments)
		}
		if diff := cmp.Diff([]int64{c.ID, a.ID}, m.Input); diff != "" {
			t.Errorf("MutationContext.Input (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{c.ID, a.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after SoftDeleteMany([c, a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("records only the keys it wrote as affected", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		missing := d.ID + 1000
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Documents().SoftDeleteMany(ctx, []int64{missing, d.ID}); err != nil {
			t.Fatalf("SoftDeleteMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{d.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of SoftDeleteMany([missing, d]) (-want +got):\n%s", diff)
		}
		if _, err := client.Documents().SoftDeleteMany(ctx, []int64{missing}); err != nil {
			t.Fatalf("SoftDeleteMany([missing]) error = %v, want nil", err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs of a SoftDeleteMany matching no row = %v, want none", got)
		}
	})

	t.Run("records a key the database matched case-insensitively as affected", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Labels().SoftDeleteMany(ctx, []string{"RED"}); err != nil {
			t.Fatalf("SoftDeleteMany([RED]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{"RED"}, affected()); diff != "" {
			t.Errorf("AffectedPKs of SoftDeleteMany([RED]) over stored red (-want +got):\n%s", diff)
		}
		if n := sdmLiveLabelCount(t, h); n != 0 {
			t.Errorf("live labels after SoftDeleteMany([RED]) = %d, want 0", n)
		}
		assertRowCount(t, h, "labels", 1)
	})

	t.Run("soft-deleting a pinned document returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		pinned := createDocument(t, h, "pinned")
		_, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{pinned.ID})
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("SoftDeleteMany([pinned]) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		if diff := cmp.Diff([]int64{pinned.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the failed SoftDeleteMany (-want +got):\n%s", diff)
		}
	})

	t.Run("a failed sub-batch keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		a, b, pinned := createDocument(t, h, "a"), createDocument(t, h, "b"), createDocument(t, h, "pinned")
		// With batch_size: 2, {a, b} is the first sub-batch and {pinned} the second, which fails.
		_, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID, b.ID, pinned.ID})
		if _, ok := errors.AsType[*models.ConstraintError](err); !ok {
			t.Fatalf("SoftDeleteMany([a, b, pinned]) error = %v, want a *ConstraintError", err)
		}
		if diff := cmp.Diff([]int64{a.ID, b.ID}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after the failed SoftDeleteMany (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{pinned.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the failed SoftDeleteMany (-want +got):\n%s", diff)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID, b.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{Title: true}
		})
		if err != nil {
			t.Fatalf("SoftDeleteMany([a, b], Title only) error = %v, want nil", err)
		}
		want := []*models.Document{{Title: "a"}, {Title: "b"}}
		byTitle := cmpopts.SortSlices(func(x, y *models.Document) bool { return x.Title < y.Title })
		if diff := cmp.Diff(want, got, byTitle); diff != "" {
			t.Errorf("SoftDeleteMany([a, b], Title only) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a.ID, b.ID}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after SoftDeleteMany([a, b], Title only) (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions soft-deletes and returns nil", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil {
			t.Fatalf("SoftDeleteMany([a], empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("SoftDeleteMany([a], empty FieldOptions) = %+v, want nil", got)
		}
		// The write is the call's last statement: no follow-up read.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "UPDATE") {
			t.Errorf("SoftDeleteMany([a], empty FieldOptions) sent %q, want the UPDATE last", stmts)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after SoftDeleteMany([a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().SoftDeleteMany(ctx, []int64{a.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("SoftDeleteMany([a], LockForUpdate) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("SoftDeleteMany([a], LockForUpdate) returned keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after SoftDeleteMany([a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys soft-delete only the listed tuples", func(t *testing.T) {
		h.Reset(t)
		listed := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}}
		// The unlisted keys cross the listed ones: each column value is listed,
		// only the pairing is not.
		unlisted := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		for _, pk := range append(append([]models.RevisionPK{}, listed...), unlisted...) {
			createRevision(t, h, pk)
		}
		// An unlisted tuple deleted earlier must not come back as one this call wrote.
		earlier := models.RevisionPK{DocumentID: 3, Number: 1}
		createRevision(t, h, earlier)
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, []models.RevisionPK{earlier}); err != nil {
			t.Fatalf("SoftDeleteMany(%+v) setup error = %v", earlier, err)
		}
		got, err := h.Client.Revisions().SoftDeleteMany(ctx, listed)
		if err != nil {
			t.Fatalf("SoftDeleteMany(%+v) error = %v, want nil", listed, err)
		}
		if diff := cmp.Diff(listed, sdmRevisionPKs(got)); diff != "" {
			t.Errorf("SoftDeleteMany(%+v) returned keys (-want +got):\n%s", listed, diff)
		}
		// A per-column match would have soft-deleted the crossing tuples too.
		if diff := cmp.Diff(unlisted, sdmLiveRevisionPKs(t, h)); diff != "" {
			t.Errorf("live revisions after SoftDeleteMany(%+v) (-want +got):\n%s", listed, diff)
		}
		assertRowCount(t, h, "revisions", 5)
	})

	t.Run("splits the keys into sub-batches of the table's batch size", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 5)
		h.Querier.ResetCount()
		if _, err := h.Client.Documents().SoftDeleteMany(ctx, ids); err != nil {
			t.Fatalf("SoftDeleteMany(5 keys) error = %v, want nil", err)
		}
		// The documents table sets batch_size: 2, so 5 keys are ceil(5 / 2) = 3 sub-batches.
		if updates := statementsWithPrefix(h, "UPDATE"); len(updates) != 3 {
			t.Errorf("SoftDeleteMany(5 keys) sent %d UPDATE statements, want 3: %q", len(updates), updates)
		}
		assertLiveDocuments(t, h, 0)
		assertRowCount(t, h, "documents", 5)
	})
}

// restoreMany: RestoreMany clears the soft-delete marker on every listed row.
func restoreMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a key listed twice across a sub-batch boundary is read back once", func(t *testing.T) {
		h.Reset(t)
		// revisions has batch_size: 2, so the repeated key opens the second chunk.
		sdmSeedRevisions(t, h, 2)
		pks := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 1, Number: 2}, {DocumentID: 1, Number: 1}}
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, pks[:2]); err != nil {
			t.Fatalf("SoftDeleteMany(keys 1, 2) error = %v", err)
		}
		got, err := h.Client.Revisions().RestoreMany(ctx, pks)
		if err != nil {
			t.Fatalf("RestoreMany(keys 1, 2, 1) error = %v, want nil", err)
		}
		numbers := make([]int64, len(got))
		for i, r := range got {
			numbers[i] = r.Number
		}
		slices.Sort(numbers)
		if diff := cmp.Diff([]int64{1, 2}, numbers); diff != "" {
			t.Errorf("RestoreMany(keys 1, 2, 1) returned revisions (-want +got):\n%s", diff)
		}
	})

	t.Run("restores only the listed keys and returns them", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		a, b, c := ids[0], ids[1], ids[2]
		sdmSoftDeleteDocuments(t, h, ids...)
		// An unlisted live row must not come back as one this call restored.
		live := createDocument(t, h, "live")
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{a, c})
		if err != nil {
			t.Fatalf("RestoreMany([a, c]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a, c}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("RestoreMany([a, c]) returned keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after RestoreMany([a, c]) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a, c, live.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after RestoreMany([a, c]) (-want +got):\n%s", diff)
		}
	})

	t.Run("nil key list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		sdmSoftDeleteDocuments(t, h, a.ID)
		h.Querier.ResetCount()
		got, err := h.Client.Documents().RestoreMany(ctx, nil)
		if err != nil {
			t.Fatalf("RestoreMany(nil) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("RestoreMany(nil) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("RestoreMany(nil) returned %d documents, want none", len(got))
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("empty key list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		sdmSoftDeleteDocuments(t, h, a.ID)
		h.Querier.ResetCount()
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{})
		if err != nil {
			t.Fatalf("RestoreMany([]) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("RestoreMany([]) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("RestoreMany([]) returned %d documents, want none", len(got))
		}
		assertLiveDocuments(t, h, 0)
	})

	t.Run("missing keys are not an error and are not returned", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		sdmSoftDeleteDocuments(t, h, d.ID)
		missing := d.ID + 1000
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{missing, d.ID})
		if err != nil {
			t.Fatalf("RestoreMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{d.ID}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("RestoreMany([missing, d]) returned keys (-want +got):\n%s", diff)
		}
		got, err = h.Client.Documents().RestoreMany(ctx, []int64{missing})
		if err != nil {
			t.Fatalf("RestoreMany([missing]) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("RestoreMany([missing]) returned %d documents, want none", len(got))
		}
	})

	t.Run("restored rows return to default reads", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID})
		if err != nil {
			t.Fatalf("RestoreMany([a]) error = %v, want nil", err)
		}
		// The returned entity is the restored row: its marker is cleared.
		if len(got) != 1 || got[0].ID != a.ID || sdmDocumentDeleted(got[0]) {
			t.Errorf("RestoreMany([a]) returned %+v, want only a with DeletedAt cleared", got)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("GetMany with no DeletedAt comparator (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b.ID}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("GetMany with DeletedAt IS NOT NULL (-want +got):\n%s", diff)
		}
	})

	t.Run("restores inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().RestoreMany(txCtx, []int64{a.ID}); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("RestoreMany inside the transaction sent %q outside it, want nothing", stmts)
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
		if diff := cmp.Diff([]int64{a.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves rows deleted", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		sdmSoftDeleteDocuments(t, h, a.ID)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().RestoreMany(txCtx, []int64{a.ID}); err != nil {
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
		if diff := cmp.Diff([]int64{a.ID}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one restore_many mutation hook", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		a, b, c := ids[0], ids[1], ids[2]
		sdmSoftDeleteDocuments(t, h, ids...)
		client, seen := mutationRecorder(h)
		// Listed out of key order: the affected keys follow the list.
		if _, err := client.Documents().RestoreMany(ctx, []int64{c, a}); err != nil {
			t.Fatalf("RestoreMany([c, a]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpRestoreMany || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpRestoreMany, models.TableDocuments)
		}
		if diff := cmp.Diff([]int64{c, a}, m.Input); diff != "" {
			t.Errorf("MutationContext.Input (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{c, a}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{b}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after RestoreMany([c, a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("records only the keys it wrote as affected", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		if _, err := h.Client.Documents().SoftDelete(ctx, d.ID); err != nil {
			t.Fatalf("SoftDelete(d) error = %v", err)
		}
		missing := d.ID + 1000
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Documents().RestoreMany(ctx, []int64{missing, d.ID}); err != nil {
			t.Fatalf("RestoreMany([missing, d]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{d.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of RestoreMany([missing, d]) (-want +got):\n%s", diff)
		}
		if _, err := client.Documents().RestoreMany(ctx, []int64{missing}); err != nil {
			t.Fatalf("RestoreMany([missing]) error = %v, want nil", err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs of a RestoreMany matching no row = %v, want none", got)
		}
	})

	t.Run("records a key the database matched case-insensitively as affected", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		if _, err := h.Client.Labels().SoftDelete(ctx, "red"); err != nil {
			t.Fatalf("SoftDelete(red) error = %v", err)
		}
		client, affected := affectedPKsRecorder(h)
		if _, err := client.Labels().RestoreMany(ctx, []string{"RED"}); err != nil {
			t.Fatalf("RestoreMany([RED]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{"RED"}, affected()); diff != "" {
			t.Errorf("AffectedPKs of RestoreMany([RED]) over stored red (-want +got):\n%s", diff)
		}
		if n := sdmLiveLabelCount(t, h); n != 1 {
			t.Errorf("live labels after RestoreMany([RED]) = %d, want 1", n)
		}
	})

	t.Run("restoring an archived document returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		archived := seedArchivedDocument(t, h)
		// With batch_size: 2, {a, b} is the first sub-batch and {archived} the second, which fails.
		_, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID, b.ID, archived})
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("RestoreMany([a, b, archived]) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintCheck {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintCheck)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		if !slices.Contains(sdmDeletedDocumentIDs(t, h), archived) {
			t.Errorf("archived document %d is live after the failed RestoreMany, want it still deleted", archived)
		}
	})

	t.Run("a failed sub-batch keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		archived := seedArchivedDocument(t, h)
		// With batch_size: 2, {a, b} is the first sub-batch and {archived} the second, which fails.
		_, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID, b.ID, archived})
		if _, ok := errors.AsType[*models.ConstraintError](err); !ok {
			t.Fatalf("RestoreMany([a, b, archived]) error = %v, want a *ConstraintError", err)
		}
		if diff := cmp.Diff([]int64{a.ID, b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after the failed RestoreMany (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{archived}, sdmDeletedDocumentIDs(t, h)); diff != "" {
			t.Errorf("soft-deleted documents after the failed RestoreMany (-want +got):\n%s", diff)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID, b.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{Title: true}
		})
		if err != nil {
			t.Fatalf("RestoreMany([a, b], Title only) error = %v, want nil", err)
		}
		want := []*models.Document{{Title: "a"}, {Title: "b"}}
		byTitle := cmpopts.SortSlices(func(x, y *models.Document) bool { return x.Title < y.Title })
		if diff := cmp.Diff(want, got, byTitle); diff != "" {
			t.Errorf("RestoreMany([a, b], Title only) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a.ID, b.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after RestoreMany([a, b], Title only) (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions restores and returns nil", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		h.Querier.ResetCount()
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil {
			t.Fatalf("RestoreMany([a], empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("RestoreMany([a], empty FieldOptions) = %+v, want nil", got)
		}
		// The write is the call's last statement: no follow-up read.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "UPDATE") {
			t.Errorf("RestoreMany([a], empty FieldOptions) sent %q, want the UPDATE last", stmts)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after RestoreMany([a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		sdmSoftDeleteDocuments(t, h, a.ID, b.ID)
		got, err := h.Client.Documents().RestoreMany(ctx, []int64{a.ID}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("RestoreMany([a], LockForUpdate) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmDocumentIDs(got)); diff != "" {
			t.Errorf("RestoreMany([a], LockForUpdate) returned keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]int64{a.ID}, sdmLiveDocumentIDs(t, h)); diff != "" {
			t.Errorf("live documents after RestoreMany([a]) (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys restore only the listed tuples", func(t *testing.T) {
		h.Reset(t)
		listed := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}}
		// The unlisted keys cross the listed ones: each column value is listed,
		// only the pairing is not.
		unlisted := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		all := append(append([]models.RevisionPK{}, listed...), unlisted...)
		for _, pk := range all {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDeleteMany(ctx, all); err != nil {
			t.Fatalf("SoftDeleteMany(all revisions) error = %v", err)
		}
		// An unlisted live tuple must not come back as one this call restored.
		live := models.RevisionPK{DocumentID: 3, Number: 1}
		createRevision(t, h, live)
		got, err := h.Client.Revisions().RestoreMany(ctx, listed)
		if err != nil {
			t.Fatalf("RestoreMany(%+v) error = %v, want nil", listed, err)
		}
		if diff := cmp.Diff(listed, sdmRevisionPKs(got)); diff != "" {
			t.Errorf("RestoreMany(%+v) returned keys (-want +got):\n%s", listed, diff)
		}
		// A per-column match would have restored the crossing tuples too.
		want := []models.RevisionPK{listed[0], listed[1], live}
		if diff := cmp.Diff(want, sdmLiveRevisionPKs(t, h)); diff != "" {
			t.Errorf("live revisions after RestoreMany(%+v) (-want +got):\n%s", listed, diff)
		}
		assertRowCount(t, h, "revisions", 5)
	})

	t.Run("splits the keys into sub-batches of the table's batch size", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 5)
		if _, err := h.Client.Documents().SoftDeleteMany(ctx, ids); err != nil {
			t.Fatalf("SoftDeleteMany(5 keys) error = %v", err)
		}
		h.Querier.ResetCount()
		if _, err := h.Client.Documents().RestoreMany(ctx, ids); err != nil {
			t.Fatalf("RestoreMany(5 keys) error = %v, want nil", err)
		}
		// The documents table sets batch_size: 2, so 5 keys are ceil(5 / 2) = 3 sub-batches.
		if updates := statementsWithPrefix(h, "UPDATE"); len(updates) != 3 {
			t.Errorf("RestoreMany(5 keys) sent %d UPDATE statements, want 3: %q", len(updates), updates)
		}
		assertLiveDocuments(t, h, 5)
	})
}

// sdmSoftDeleteDocuments soft-deletes the documents ids name.
func sdmSoftDeleteDocuments(t *testing.T, h *Harness, ids ...int64) {
	t.Helper()
	if _, err := h.Client.Documents().SoftDeleteMany(context.Background(), ids); err != nil {
		t.Fatalf("SoftDeleteMany(%v) setup error = %v", ids, err)
	}
}

// sdmDocumentDeleted reports whether d's soft-delete marker is set.
func sdmDocumentDeleted(d *models.Document) bool {
	return !cmp.Equal(d.DeletedAt, models.Document{}.DeletedAt)
}

// sdmDocumentIDs returns the keys of docs, sorted.
func sdmDocumentIDs(docs []*models.Document) []int64 {
	ids := make([]int64, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	slices.Sort(ids)
	return ids
}

// sdmLiveDocumentIDs returns the sorted keys GetMany reads with no DeletedAt
// comparator, so the default not-deleted condition applies.
func sdmLiveDocumentIDs(t *testing.T, h *Harness) []int64 {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("Documents().GetMany(nil) error = %v", err)
	}
	return sdmDocumentIDs(docs)
}

// sdmDeletedDocumentIDs returns the sorted keys of the soft-deleted documents.
func sdmDeletedDocumentIDs(t *testing.T, h *Harness) []int64 {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany(DeletedAt IS NOT NULL) error = %v", err)
	}
	return sdmDocumentIDs(docs)
}

// sdmRevisionPKs returns the keys of revs, sorted.
func sdmRevisionPKs(revs []*models.Revision) []models.RevisionPK {
	pks := make([]models.RevisionPK, 0, len(revs))
	for _, r := range revs {
		pks = append(pks, models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number})
	}
	slices.SortFunc(pks, func(x, y models.RevisionPK) int {
		if x.DocumentID != y.DocumentID {
			return int(x.DocumentID - y.DocumentID)
		}
		return int(x.Number - y.Number)
	})
	return pks
}

// sdmLiveRevisionPKs returns the sorted keys of the revisions that are not
// soft-deleted.
func sdmLiveRevisionPKs(t *testing.T, h *Harness) []models.RevisionPK {
	t.Helper()
	revs, err := h.Client.Revisions().GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("Revisions().GetMany(nil) error = %v", err)
	}
	return sdmRevisionPKs(revs)
}

// sdmLiveLabelCount counts the labels that are not soft-deleted.
func sdmLiveLabelCount(t *testing.T, h *Harness) int {
	t.Helper()
	labels, err := h.Client.Labels().GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("Labels().GetMany(nil) error = %v", err)
	}
	return len(labels)
}

// sdmSeedRevisions writes n revisions of document 1 and returns their keys.
func sdmSeedRevisions(t *testing.T, h *Harness, n int) []models.RevisionPK {
	t.Helper()
	inputs := make([]*models.CreateRevisionInput, n)
	pks := make([]models.RevisionPK, n)
	for i := range inputs {
		inputs[i] = &models.CreateRevisionInput{DocumentID: 1, Number: int64(i + 1), Body: "body"}
		pks[i] = models.RevisionPK{DocumentID: 1, Number: int64(i + 1)}
	}
	if _, err := h.Client.Revisions().CreateMany(context.Background(), inputs); err != nil {
		t.Fatalf("CreateMany(%d revisions): %v", n, err)
	}
	return pks
}

// sdmCount reports how many of verbs equal verb.
func sdmCount(verbs []string, verb string) int {
	n := 0
	for _, v := range verbs {
		if v == verb {
			n++
		}
	}
	return n
}
