package suite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

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

	t.Run("updates only the rows the filter matches and returns them", func(t *testing.T) {
		h.Reset(t)
		// other comes first, so a write or read-back that loosens the filter reaches it.
		other := createDocument(t, h, "other")
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("a", "b"),
			&models.UpdateDocumentInput{Title: omittable.Set("renamed")})
		if err != nil {
			t.Fatalf("UpdateWhere(title in [a, b]) error = %v, want nil", err)
		}
		want := []*models.Document{{ID: a.ID, Title: "renamed"}, {ID: b.ID, Title: "renamed"}}
		if diff := cmp.Diff(want, got, sdwDocumentsByID); diff != "" {
			t.Errorf("UpdateWhere(title in [a, b]) returned (-want +got):\n%s", diff)
		}
		wantStored := map[int64]string{other.ID: "other", a.ID: "renamed", b.ID: "renamed"}
		if diff := cmp.Diff(wantStored, uwDocumentTitles(t, h)); diff != "" {
			t.Errorf("stored titles after UpdateWhere (-want +got):\n%s", diff)
		}
	})

	t.Run("sets the update-tracking column when the input omits it and keeps an explicit value", func(t *testing.T) {
		h.Reset(t)
		uwSeedCounterAt(t, h, "auto", "2000-01-01 00:00:00")
		uwSeedCounterAt(t, h, "explicit", "2000-01-01 00:00:00")
		// source only supplies a Timestamp value, distinct from the seeded one,
		// in this dialect's Go type.
		uwSeedCounterAt(t, h, "source", "2010-06-01 00:00:00")
		explicit := uwCounterByName(t, h, "source").UpdatedAt
		before := time.Now().Truncate(time.Second).Add(-time.Second)
		got, err := h.Client.Counters().UpdateWhere(ctx, uwCounterNameIn("auto"),
			&models.UpdateCounterInput{Hits: omittable.Set(int64(1))})
		after := time.Now().Add(time.Second)
		if err != nil {
			t.Fatalf("UpdateWhere(name in [auto], Hits) error = %v, want nil", err)
		}
		if len(got) != 1 || got[0].Hits != 1 {
			t.Fatalf("UpdateWhere(name in [auto], Hits) = %+v, want one counter with hits 1", got)
		}
		if at := time.Unix(got[0].UpdatedAt.Unix(), 0); at.Before(before) || at.After(after) {
			t.Errorf("auto-set updated_at = %v, want within [%v, %v]", at, before, after)
		}
		got, err = h.Client.Counters().UpdateWhere(ctx, uwCounterNameIn("explicit"),
			&models.UpdateCounterInput{Hits: omittable.Set(int64(1)), UpdatedAt: omittable.Set(explicit)})
		if err != nil {
			t.Fatalf("UpdateWhere(name in [explicit], Hits, UpdatedAt) error = %v, want nil", err)
		}
		if len(got) != 1 || got[0].Hits != 1 || !cmp.Equal(got[0].UpdatedAt, explicit) {
			t.Errorf("UpdateWhere(name in [explicit], Hits, UpdatedAt) = %+v, want hits 1 and updated_at %v", got, explicit)
		}
		if stored := uwCounterByName(t, h, "explicit").UpdatedAt; !cmp.Equal(stored, explicit) {
			t.Errorf("stored updated_at of explicit = %v, want %v", stored, explicit)
		}
	})

	t.Run("nil input returns ErrNilInput before the filter is checked and fires no hook", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		for _, tt := range []struct {
			name   string
			filter *models.DocumentFilter
		}{
			{"nil filter", nil},
			{"empty filter", &models.DocumentFilter{}},
			{"matching filter", sdwTitleIn("a")},
		} {
			got, err := client.Documents().UpdateWhere(ctx, tt.filter, nil)
			if !errors.Is(err, models.ErrNilInput) {
				t.Errorf("UpdateWhere(%s, nil input) error = %v, want ErrNilInput", tt.name, err)
			}
			if got != nil {
				t.Errorf("UpdateWhere(%s, nil input) = %v, want nil", tt.name, got)
			}
		}
		if n := len(seen()); n != 0 {
			t.Errorf("mutation hook fired %d times for a nil input, want 0", n)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpdateWhere with a nil input sent %q, want nothing", stmts)
		}
		if diff := cmp.Diff([]string{"a"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("nil filter returns ErrEmptyFilter and updates nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		for _, tt := range []struct {
			name  string
			input *models.UpdateDocumentInput
		}{
			{"a field set", &models.UpdateDocumentInput{Title: omittable.Set("renamed")}},
			{"empty input", &models.UpdateDocumentInput{}},
		} {
			got, err := h.Client.Documents().UpdateWhere(ctx, nil, tt.input)
			if !errors.Is(err, models.ErrEmptyFilter) {
				t.Errorf("UpdateWhere(nil filter, %s) error = %v, want ErrEmptyFilter", tt.name, err)
			}
			if got != nil {
				t.Errorf("UpdateWhere(nil filter, %s) = %v, want nil", tt.name, got)
			}
		}
		if diff := cmp.Diff([]string{"a"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("empty filter returns ErrEmptyFilter and updates nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		got, err := h.Client.Documents().UpdateWhere(ctx, &models.DocumentFilter{}, &models.UpdateDocumentInput{Title: omittable.Set("renamed")})
		if !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("UpdateWhere(&DocumentFilter{}) error = %v, want ErrEmptyFilter", err)
		}
		if got != nil {
			t.Errorf("UpdateWhere(&DocumentFilter{}) = %v, want nil", got)
		}
		if diff := cmp.Diff([]string{"a"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("a filter matching no row returns an empty result and no error", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		client, affected := affectedPKsRecorder(h)
		got, err := client.Documents().UpdateWhere(ctx, sdwTitleIn("missing"), &models.UpdateDocumentInput{Title: omittable.Set("renamed")})
		if err != nil {
			t.Fatalf("UpdateWhere(title in [missing]) error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("UpdateWhere(title in [missing]) = %v, want an empty result", got)
		}
		if a := affected(); len(a) != 0 {
			t.Errorf("AffectedPKs of UpdateWhere(title in [missing]) = %v, want none", a)
		}
		if diff := cmp.Diff([]string{"a"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("updates inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		createDocument(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().UpdateWhere(txCtx, sdwTitleIn("a"), &models.UpdateDocumentInput{Title: omittable.Set("renamed")}); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("UpdateWhere inside the transaction sent %q outside it, want nothing", stmts)
			}
			d, err := h.Client.Documents().Get(txCtx, a.ID)
			if err != nil {
				return err
			}
			if d.Title != "renamed" {
				t.Errorf("Get inside the transaction = %+v, want title renamed", d)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"b", "renamed"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves rows unchanged", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().UpdateWhere(txCtx, sdwTitleIn("a"), &models.UpdateDocumentInput{Title: omittable.Set("renamed")}); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			d, err := h.Client.Documents().Get(txCtx, a.ID)
			if err != nil {
				return err
			}
			if d.Title != "renamed" {
				t.Errorf("Get inside the transaction = %+v, want title renamed", d)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		if diff := cmp.Diff([]string{"a"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one update_where mutation hook", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "other")
		a := createDocument(t, h, "a")
		gone := sdwSeedDeleted(t, h, "gone")
		client, seen := mutationRecorder(h)
		input := &models.UpdateDocumentInput{Title: omittable.Set("renamed")}
		if _, err := client.Documents().UpdateWhere(ctx, sdwTitleIn("a", "gone"), input); err != nil {
			t.Fatalf("UpdateWhere(title in [a, gone]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpUpdateWhere || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpUpdateWhere, models.TableDocuments)
		}
		if m.Input != any(input) {
			t.Errorf("MutationContext.Input = %v, want the caller's input %p", m.Input, input)
		}
		if diff := cmp.Diff([]any{a.ID, gone}, m.AffectedPKs, sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("a write a CHECK forbids returns a check ConstraintError and updates nothing", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		createDocument(t, h, "pinned")
		got, err := h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("a", "pinned"),
			&models.UpdateDocumentInput{DeletedAt: omittable.Set(models.DeletedAt(models.Now()))})
		uwAssertConstraint(t, err, models.ConstraintCheck)
		if got != nil {
			t.Errorf("UpdateWhere result on error = %v, want nil", got)
		}
		// The write is one statement, so the matched row beside pinned stays live too.
		assertLiveDocuments(t, h, 2)
	})

	t.Run("a write that duplicates a unique value returns a unique ConstraintError and updates nothing", func(t *testing.T) {
		h.Reset(t)
		uwCreateCounter(t, h, "a", 0)
		uwCreateCounter(t, h, "b", 0)
		got, err := h.Client.Counters().UpdateWhere(ctx, uwCounterNameIn("a"),
			&models.UpdateCounterInput{Name: omittable.Set("b")})
		uwAssertConstraint(t, err, models.ConstraintUnique)
		if got != nil {
			t.Errorf("UpdateWhere result on error = %v, want nil", got)
		}
		if diff := cmp.Diff([]string{"a", "b"}, uwCounterNames(t, h)); diff != "" {
			t.Errorf("stored counter names (-want +got):\n%s", diff)
		}
	})

	t.Run("a partial field selection returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "other")
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		got, err := h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("a", "b"),
			&models.UpdateDocumentInput{Title: omittable.Set("renamed")},
			func(o *models.CallOptions[models.DocumentFieldOptions]) {
				o.FieldOptions = &models.DocumentFieldOptions{ID: true}
			})
		if err != nil {
			t.Fatalf("UpdateWhere(selecting id) error = %v, want nil", err)
		}
		// A full entity would carry the title the write just set.
		want := []*models.Document{{ID: a.ID}, {ID: b.ID}}
		if diff := cmp.Diff(want, got, sdwDocumentsByID); diff != "" {
			t.Errorf("UpdateWhere(selecting id) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"other", "renamed", "renamed"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty field selection updates, returns nil and still records the affected keys", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "other")
		a, b := createDocument(t, h, "a"), createDocument(t, h, "b")
		client, affected := affectedPKsRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Documents().UpdateWhere(ctx, sdwTitleIn("a", "b"),
			&models.UpdateDocumentInput{Title: omittable.Set("renamed")},
			func(o *models.CallOptions[models.DocumentFieldOptions]) {
				o.FieldOptions = &models.DocumentFieldOptions{}
			})
		if err != nil {
			t.Fatalf("UpdateWhere(selecting nothing) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("UpdateWhere(selecting nothing) = %v, want nil", got)
		}
		// The write is the last statement: no terminal read follows it.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "UPDATE") {
			t.Errorf("UpdateWhere(selecting nothing) sent %q, want the UPDATE last", stmts)
		}
		if diff := cmp.Diff([]any{a.ID, b.ID}, affected(), sdwByAnyInt64); diff != "" {
			t.Errorf("AffectedPKs of UpdateWhere(selecting nothing) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"other", "renamed", "renamed"}, uwTitles(t, h)); diff != "" {
			t.Errorf("stored titles (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty field selection with an empty input returns nil and sends no statement", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		h.Querier.ResetCount()
		got, err := h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("a"), &models.UpdateDocumentInput{},
			func(o *models.CallOptions[models.DocumentFieldOptions]) {
				o.FieldOptions = &models.DocumentFieldOptions{}
			})
		if err != nil {
			t.Fatalf("UpdateWhere(empty input, selecting nothing) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("UpdateWhere(empty input, selecting nothing) = %v, want nil", got)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpdateWhere(empty input, selecting nothing) sent %q, want nothing", stmts)
		}
	})

	t.Run("a lock mode is ignored outside a transaction", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		lock := func(o *models.CallOptions[models.DocumentFieldOptions]) { o.LockMode = sql.LockForUpdate }
		got, err := h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("a"), &models.UpdateDocumentInput{Title: omittable.Set("renamed")}, lock)
		if err != nil {
			t.Fatalf("UpdateWhere with LockForUpdate error = %v, want nil", err)
		}
		if diff := cmp.Diff([]*models.Document{{ID: a.ID, Title: "renamed"}}, got); diff != "" {
			t.Errorf("UpdateWhere with LockForUpdate returned (-want +got):\n%s", diff)
		}
		// The empty-update path reads the matched rows back too.
		got, err = h.Client.Documents().UpdateWhere(ctx, sdwTitleIn("renamed"), &models.UpdateDocumentInput{}, lock)
		if err != nil {
			t.Fatalf("UpdateWhere(empty input) with LockForUpdate error = %v, want nil", err)
		}
		if diff := cmp.Diff([]*models.Document{{ID: a.ID, Title: "renamed"}}, got); diff != "" {
			t.Errorf("UpdateWhere(empty input) with LockForUpdate returned (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys update and return exactly the matched tuples", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		matched := []models.OrderItemPK{{OrderID: 101, ProductID: p1}, {OrderID: 102, ProductID: p2}}
		// crossed shares every per-column key value with matched, but the filter's
		// quantity condition leaves it out: a re-fetch or key record by
		// per-column values would take it in.
		crossed := []models.OrderItemPK{{OrderID: 101, ProductID: p2}, {OrderID: 102, ProductID: p1}}
		for _, pk := range matched {
			uwCreateOrderItem(t, h, pk, 1)
		}
		for _, pk := range crossed {
			uwCreateOrderItem(t, h, pk, 5)
		}
		client, affected := affectedPKsRecorder(h)
		filter := &models.OrderItemFilter{
			OrderID:  &comparator.Number[int64]{In: []int64{101, 102}},
			Quantity: &comparator.Number[int64]{Eq: new(int64(1))},
		}
		got, err := client.OrderItems().UpdateWhere(ctx, filter, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(9))})
		if err != nil {
			t.Fatalf("UpdateWhere(order_id in [101, 102], quantity 1) error = %v, want nil", err)
		}
		want := []*models.OrderItem{
			{OrderID: matched[0].OrderID, ProductID: matched[0].ProductID, Quantity: 9},
			{OrderID: matched[1].OrderID, ProductID: matched[1].ProductID, Quantity: 9},
		}
		if diff := cmp.Diff(want, got, uwOrderItemsByPK); diff != "" {
			t.Errorf("UpdateWhere(order_id in [101, 102], quantity 1) returned (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{matched[0], matched[1]}, affected(), uwByAnyOrderItemPK); diff != "" {
			t.Errorf("AffectedPKs of UpdateWhere (-want +got):\n%s", diff)
		}
		wantStored := map[models.OrderItemPK]int64{matched[0]: 9, matched[1]: 9, crossed[0]: 5, crossed[1]: 5}
		if diff := cmp.Diff(wantStored, uwOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("stored quantities after UpdateWhere (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys return a soft-deleted tuple the filter matches", func(t *testing.T) {
		h.Reset(t)
		live := models.RevisionPK{DocumentID: 1, Number: 10}
		trashed := models.RevisionPK{DocumentID: 2, Number: 20}
		// crossed shares every per-column key value with the matched tuples, but
		// the filter's body condition leaves it out.
		crossed := []models.RevisionPK{{DocumentID: 1, Number: 20}, {DocumentID: 2, Number: 10}}
		for _, pk := range []models.RevisionPK{live, trashed} {
			uwCreateRevision(t, h, pk, "draft")
		}
		for _, pk := range crossed {
			uwCreateRevision(t, h, pk, "final")
		}
		if _, err := h.Client.Revisions().SoftDelete(ctx, trashed); err != nil {
			t.Fatalf("SoftDelete(%+v) error = %v", trashed, err)
		}
		client, affected := affectedPKsRecorder(h)
		filter := &models.RevisionFilter{
			DocumentID: &comparator.Number[int64]{In: []int64{1, 2}},
			Body:       &comparator.String{Eq: new("draft")},
		}
		got, err := client.Revisions().UpdateWhere(ctx, filter, &models.UpdateRevisionInput{Body: omittable.Set("edited")})
		if err != nil {
			t.Fatalf("UpdateWhere(document_id in [1, 2], body draft) error = %v, want nil", err)
		}
		gotKeys := make(map[models.RevisionPK]bool, len(got))
		for _, r := range got {
			pk := models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number}
			gotKeys[pk] = uwRevisionDeleted(r)
			if r.Body != "edited" {
				t.Errorf("UpdateWhere returned %+v, want body edited", r)
			}
		}
		// Each returned key maps to whether the returned row is soft-deleted.
		if diff := cmp.Diff(map[models.RevisionPK]bool{live: false, trashed: true}, gotKeys); diff != "" {
			t.Errorf("UpdateWhere returned keys and their soft-deleted state (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{live, trashed}, affected(), sdwByAnyRevisionPK); diff != "" {
			t.Errorf("AffectedPKs of UpdateWhere (-want +got):\n%s", diff)
		}
		wantStored := map[models.RevisionPK]string{live: "edited", trashed: "edited", crossed[0]: "final", crossed[1]: "final"}
		if diff := cmp.Diff(wantStored, uwRevisionBodies(t, h)); diff != "" {
			t.Errorf("stored bodies after UpdateWhere (-want +got):\n%s", diff)
		}
	})
}

// increment is the Increment contract.
func increment(t *testing.T, h *Harness) {
	ctx := context.Background()
	price := func(n int) models.IncrementInput[models.ProductIncrementColumn] {
		return models.IncrementInput[models.ProductIncrementColumn]{Column: models.ProductIncrementPrice, Amount: n}
	}
	hits := func(n int) models.IncrementInput[models.CounterIncrementColumn] {
		return models.IncrementInput[models.CounterIncrementColumn]{Column: models.CounterIncrementHits, Amount: n}
	}

	t.Run("adds a positive amount and subtracts a negative one on the given row only", func(t *testing.T) {
		h.Reset(t)
		// other comes first, so a write that loosens the key reaches it.
		other := createPricedProduct(t, h, "other", 100)
		p := createPricedProduct(t, h, "p", 100)
		h.Querier.ResetCount()
		if err := h.Client.Products().Increment(ctx, p.ID, price(7)); err != nil {
			t.Fatalf("Increment(p, +7) error = %v, want nil", err)
		}
		// Atomic: one UPDATE computes the value from the column itself, with no read.
		stmts := h.Querier.Statements()
		if len(stmts) != 1 || !strings.HasPrefix(stmts[0], "UPDATE") {
			t.Errorf("Increment(p, +7) sent %q, want exactly one UPDATE", stmts)
		} else if unquoted := strings.NewReplacer(`"`, "", "`", "").Replace(stmts[0]); !strings.Contains(unquoted, "price = price +") {
			t.Errorf("Increment(p, +7) sent %q, want SET price = price + ...", stmts[0])
		}
		if got := uwPrice(t, h, p.ID); got != 107 {
			t.Errorf("price after Increment(p, +7) = %d, want 107", got)
		}
		if err := h.Client.Products().Increment(ctx, p.ID, price(-5)); err != nil {
			t.Fatalf("Increment(p, -5) error = %v, want nil", err)
		}
		if got := uwPrice(t, h, p.ID); got != 102 {
			t.Errorf("price after Increment(p, -5) = %d, want 102", got)
		}
		if got := uwPrice(t, h, other.ID); got != 100 {
			t.Errorf("price of other = %d, want 100", got)
		}
	})

	t.Run("a missing key on a strict table returns ErrNotFound and writes nothing", func(t *testing.T) {
		h.Reset(t)
		p := createPricedProduct(t, h, "p", 100)
		client, affected := affectedPKsRecorder(h)
		if err := client.Products().Increment(ctx, p.ID+1000, price(7)); !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("Increment(missing) error = %v, want ErrNotFound", err)
		}
		if a := affected(); len(a) != 0 {
			t.Errorf("AffectedPKs of Increment(missing) = %v, want none", a)
		}
		if got := uwPrice(t, h, p.ID); got != 100 {
			t.Errorf("price of p = %d, want 100", got)
		}
	})

	t.Run("a missing key on a non-strict table returns nil and writes nothing", func(t *testing.T) {
		h.Reset(t)
		c := uwCreateCounter(t, h, "c", 2)
		client, affected := affectedPKsRecorder(h)
		if err := client.Counters().Increment(ctx, c.ID+1000, hits(3)); err != nil {
			t.Fatalf("Increment(missing) error = %v, want nil", err)
		}
		if a := affected(); len(a) != 0 {
			t.Errorf("AffectedPKs of Increment(missing) = %v, want none", a)
		}
		if got := uwCounterByName(t, h, "c").Hits; got != 2 {
			t.Errorf("hits of c = %d, want 2", got)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("an increment by zero on an existing row succeeds and records its key", func(t *testing.T) {
		h.Reset(t)
		p := createPricedProduct(t, h, "p", 100)
		// pad makes c's key differ from p's, so a stale record of p's key cannot pass for c's.
		uwCreateCounter(t, h, "pad", 0)
		c := uwCreateCounter(t, h, "c", 2)
		if c.ID == p.ID {
			t.Fatalf("counter key %d equals product key %d, want them distinct", c.ID, p.ID)
		}
		strict, strictAffected := affectedPKsRecorder(h)
		if err := strict.Products().Increment(ctx, p.ID, price(0)); err != nil {
			t.Errorf("strict Increment(p, 0) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{p.ID}, strictAffected()); diff != "" {
			t.Errorf("AffectedPKs of strict Increment(p, 0) (-want +got):\n%s", diff)
		}
		nonStrict, nonStrictAffected := affectedPKsRecorder(h)
		if err := nonStrict.Counters().Increment(ctx, c.ID, hits(0)); err != nil {
			t.Errorf("non-strict Increment(c, 0) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{c.ID}, nonStrictAffected()); diff != "" {
			t.Errorf("AffectedPKs of non-strict Increment(c, 0) (-want +got):\n%s", diff)
		}
		if got := uwPrice(t, h, p.ID); got != 100 {
			t.Errorf("price of p = %d, want 100", got)
		}
		if got := uwCounterByName(t, h, "c").Hits; got != 2 {
			t.Errorf("hits of c = %d, want 2", got)
		}
	})

	t.Run("increments a soft-deleted row and returns nil", func(t *testing.T) {
		h.Reset(t)
		c := uwCreateCounter(t, h, "c", 2)
		if _, err := h.Client.Counters().SoftDelete(ctx, c.ID); err != nil {
			t.Fatalf("SoftDelete(c) error = %v", err)
		}
		client, affected := affectedPKsRecorder(h)
		if err := client.Counters().Increment(ctx, c.ID, hits(3)); err != nil {
			t.Fatalf("Increment(soft-deleted c, +3) error = %v, want nil", err)
		}
		got := uwCounterByName(t, h, "c")
		if got.Hits != 5 || !uwCounterDeleted(got) {
			t.Errorf("c after Increment = %+v, want hits 5 and still soft-deleted", got)
		}
		if diff := cmp.Diff([]any{c.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Increment(soft-deleted c) (-want +got):\n%s", diff)
		}
	})

	t.Run("increments inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		p := createPricedProduct(t, h, "p", 100)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if err := h.Client.Products().Increment(txCtx, p.ID, price(7)); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Increment inside the transaction sent %q outside it, want nothing", stmts)
			}
			got, err := h.Client.Products().Get(txCtx, p.ID)
			if err != nil {
				return err
			}
			if got.Price != 107 {
				t.Errorf("price inside the transaction = %d, want 107", got.Price)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if got := uwPrice(t, h, p.ID); got != 107 {
			t.Errorf("price after the commit = %d, want 107", got)
		}
	})

	t.Run("rolled-back transaction leaves the value unchanged", func(t *testing.T) {
		h.Reset(t)
		p := createPricedProduct(t, h, "p", 100)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if err := h.Client.Products().Increment(txCtx, p.ID, price(7)); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			got, err := h.Client.Products().Get(txCtx, p.ID)
			if err != nil {
				return err
			}
			if got.Price != 107 {
				t.Errorf("price inside the transaction = %d, want 107", got.Price)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		if got := uwPrice(t, h, p.ID); got != 100 {
			t.Errorf("price after the rollback = %d, want 100", got)
		}
	})

	t.Run("fires one increment mutation hook", func(t *testing.T) {
		h.Reset(t)
		createPricedProduct(t, h, "other", 100)
		p := createPricedProduct(t, h, "p", 100)
		client, seen := mutationRecorder(h)
		input := price(7)
		if err := client.Products().Increment(ctx, p.ID, input); err != nil {
			t.Fatalf("Increment(p, +7) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpIncrement || m.Table != models.TableProducts || m.PK != any(p.ID) {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %d}",
				m.Op, m.Table, m.PK, hook.OpIncrement, models.TableProducts, p.ID)
		}
		if m.Input != any(input) {
			t.Errorf("MutationContext.Input = %v, want the caller's input %v", m.Input, input)
		}
		if diff := cmp.Diff([]any{p.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("decrementing below a CHECK returns a check ConstraintError and writes nothing", func(t *testing.T) {
		h.Reset(t)
		c := uwCreateCounter(t, h, "c", 2)
		err := h.Client.Counters().Increment(ctx, c.ID, hits(-3))
		uwAssertConstraint(t, err, models.ConstraintCheck)
		if got := uwCounterByName(t, h, "c").Hits; got != 2 {
			t.Errorf("hits of c = %d, want 2", got)
		}
	})

	t.Run("a lock mode is ignored outside a transaction", func(t *testing.T) {
		h.Reset(t)
		p := createPricedProduct(t, h, "p", 100)
		err := h.Client.Products().Increment(ctx, p.ID, price(7), func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("Increment with LockForUpdate error = %v, want nil", err)
		}
		if got := uwPrice(t, h, p.ID); got != 107 {
			t.Errorf("price after Increment with LockForUpdate = %d, want 107", got)
		}
	})

	t.Run("a composite key increments only its own tuple", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		target := models.OrderItemPK{OrderID: 102, ProductID: p2}
		// The other tuples each share one key column with target, or none. Order
		// ids differ from product ids, so a key with its columns swapped is another key.
		others := []models.OrderItemPK{{OrderID: 101, ProductID: p1}, {OrderID: 101, ProductID: p2}, {OrderID: 102, ProductID: p1}}
		for _, pk := range append([]models.OrderItemPK{target}, others...) {
			uwCreateOrderItem(t, h, pk, 1)
		}
		client, affected := affectedPKsRecorder(h)
		in := models.IncrementInput[models.OrderItemIncrementColumn]{Column: models.OrderItemIncrementQuantity, Amount: 4}
		if err := client.OrderItems().Increment(ctx, target, in); err != nil {
			t.Fatalf("Increment(%+v, +4) error = %v, want nil", target, err)
		}
		if diff := cmp.Diff([]any{target}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Increment (-want +got):\n%s", diff)
		}
		want := map[models.OrderItemPK]int64{target: 5, others[0]: 1, others[1]: 1, others[2]: 1}
		if diff := cmp.Diff(want, uwOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("stored quantities after Increment (-want +got):\n%s", diff)
		}
	})
}

var (
	uwOrderItemsByPK = cmpopts.SortSlices(func(x, y *models.OrderItem) bool {
		return uwOrderItemPKLess(models.OrderItemPK{OrderID: x.OrderID, ProductID: x.ProductID},
			models.OrderItemPK{OrderID: y.OrderID, ProductID: y.ProductID})
	})
	uwByAnyOrderItemPK = cmpopts.SortSlices(func(x, y any) bool {
		return uwOrderItemPKLess(x.(models.OrderItemPK), y.(models.OrderItemPK))
	})
)

func uwOrderItemPKLess(x, y models.OrderItemPK) bool {
	if x.OrderID != y.OrderID {
		return x.OrderID < y.OrderID
	}
	return x.ProductID < y.ProductID
}

// uwAssertConstraint checks that err is a ConstraintError of type want that
// unwraps to ErrConstraintViolation.
func uwAssertConstraint(t *testing.T, err error, want models.ConstraintType) {
	t.Helper()
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v, want a *ConstraintError", err)
	}
	if ce.Type != want {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, want)
	}
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
	}
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

// uwDocumentTitles returns every document's title by key, soft-deleted ones included.
func uwDocumentTitles(t *testing.T, h *Harness) map[int64]string {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany error = %v", err)
	}
	titles := make(map[int64]string, len(docs))
	for _, d := range docs {
		titles[d.ID] = d.Title
	}
	return titles
}

// uwTitles returns every document's title, soft-deleted ones included, sorted.
func uwTitles(t *testing.T, h *Harness) []string {
	t.Helper()
	var titles []string
	for _, title := range uwDocumentTitles(t, h) {
		titles = append(titles, title)
	}
	slices.Sort(titles)
	return titles
}

// uwPrice returns the stored price of a product.
func uwPrice(t *testing.T, h *Harness, id int64) int64 {
	t.Helper()
	p, err := h.Client.Products().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Products().Get(%d) error = %v", id, err)
	}
	return p.Price
}

// uwCreateCounter creates a live counter with the given hits.
func uwCreateCounter(t *testing.T, h *Harness, name string, hits int64) *models.Counter {
	t.Helper()
	c, err := h.Client.Counters().Create(context.Background(), &models.CreateCounterInput{
		Name: name, Hits: omittable.Set(hits), UpdatedAt: omittable.Set(models.Now()),
	})
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	return c
}

// uwSeedCounterAt inserts a counter whose updated_at is the literal at. name
// must be a literal no other row uses.
func uwSeedCounterAt(t *testing.T, h *Harness, name, at string) {
	t.Helper()
	if _, err := h.Querier.Inner.Exec(context.Background(),
		"INSERT INTO counters (name, hits, updated_at) VALUES ('"+name+"', 0, '"+at+"')"); err != nil {
		t.Fatalf("seed counter %q: %v", name, err)
	}
}

// uwCounterNameIn is a counter filter matching any of the names.
func uwCounterNameIn(names ...string) *models.CounterFilter {
	return &models.CounterFilter{Name: &comparator.String{In: names}}
}

// uwCounters returns every counter, soft-deleted ones included.
func uwCounters(t *testing.T, h *Harness) []*models.Counter {
	t.Helper()
	cs, err := h.Client.Counters().GetMany(context.Background(), &models.GetCountersInput{
		Filter: &models.CounterFilter{DeletedAt: &comparator.NullableTime{}},
	})
	if err != nil {
		t.Fatalf("Counters().GetMany error = %v", err)
	}
	return cs
}

// uwCounterByName returns the counter with the given name, soft-deleted or not.
func uwCounterByName(t *testing.T, h *Harness, name string) *models.Counter {
	t.Helper()
	for _, c := range uwCounters(t, h) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no counter named %q", name)
	return nil
}

// uwCounterNames returns every counter's name, sorted.
func uwCounterNames(t *testing.T, h *Harness) []string {
	t.Helper()
	var names []string
	for _, c := range uwCounters(t, h) {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	return names
}

func uwCounterDeleted(c *models.Counter) bool {
	return !cmp.Equal(c.DeletedAt, models.Counter{}.DeletedAt)
}

func uwRevisionDeleted(r *models.Revision) bool {
	return !cmp.Equal(r.DeletedAt, models.Revision{}.DeletedAt)
}

func uwCreateOrderItem(t *testing.T, h *Harness, pk models.OrderItemPK, quantity int64) {
	t.Helper()
	in := &models.CreateOrderItemInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Quantity: quantity}
	if _, err := h.Client.OrderItems().Create(context.Background(), in); err != nil {
		t.Fatalf("Create order item %+v: %v", pk, err)
	}
}

// uwOrderItemQuantities returns every order item's quantity by key.
func uwOrderItemQuantities(t *testing.T, h *Harness) map[models.OrderItemPK]int64 {
	t.Helper()
	items, err := h.Client.OrderItems().GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("OrderItems().GetMany error = %v", err)
	}
	out := make(map[models.OrderItemPK]int64, len(items))
	for _, it := range items {
		out[models.OrderItemPK{OrderID: it.OrderID, ProductID: it.ProductID}] = it.Quantity
	}
	return out
}

func uwCreateRevision(t *testing.T, h *Harness, pk models.RevisionPK, body string) {
	t.Helper()
	in := &models.CreateRevisionInput{DocumentID: pk.DocumentID, Number: pk.Number, Body: body}
	if _, err := h.Client.Revisions().Create(context.Background(), in); err != nil {
		t.Fatalf("Create revision %+v: %v", pk, err)
	}
}

// uwRevisionBodies returns every revision's body by key, soft-deleted ones included.
func uwRevisionBodies(t *testing.T, h *Harness) map[models.RevisionPK]string {
	t.Helper()
	revs, err := h.Client.Revisions().GetMany(context.Background(), &models.GetRevisionsInput{
		Filter: &models.RevisionFilter{DeletedAt: &comparator.NullableTime{}},
	})
	if err != nil {
		t.Fatalf("Revisions().GetMany error = %v", err)
	}
	out := make(map[models.RevisionPK]string, len(revs))
	for _, r := range revs {
		out[models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number}] = r.Body
	}
	return out
}
