package suite

import (
	"context"
	"errors"
	"fmt"
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

	t.Run("writes only the fields the input sets and returns the updated row", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c")
		h.Querier.ResetCount()
		got, err := h.Client.Products().Update(ctx, b.ID, &models.UpdateProductInput{Name: omittable.Set("b2")})
		if err != nil {
			t.Fatalf("Update(b, Name) error = %v, want nil", err)
		}
		// The UPDATE runs, then the row is read back.
		if diff := cmp.Diff([]string{"UPDATE", "SELECT"}, upStatementVerbs(h)); diff != "" {
			t.Errorf("statements of Update(b, Name) (-want +got):\n%s", diff)
		}
		want := *b
		want.Name = "b2"
		if diff := cmp.Diff(&want, got); diff != "" {
			t.Errorf("Update(b, Name) (-want +got):\n%s", diff)
		}
		// Price and CreatedAt were not set, so they keep their values, and
		// the rows on either side of b are untouched.
		for _, p := range []*models.Product{a, &want, c} {
			if diff := cmp.Diff(p, upProduct(t, h, p.ID)); diff != "" {
				t.Errorf("product %d after Update(b, Name) (-want +got):\n%s", p.ID, diff)
			}
		}
	})

	t.Run("nil input returns ErrNilInput and fires no hook", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		client, seen := mutationRecorder(h)
		got, err := client.Products().Update(ctx, p.ID, nil)
		if !errors.Is(err, models.ErrNilInput) || got != nil {
			t.Errorf("Update(p, nil) = %+v, %v, want nil, ErrNilInput", got, err)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("mutation hook fired %d times for a nil input, want 0", n)
		}
		if diff := cmp.Diff(p, upProduct(t, h, p.ID)); diff != "" {
			t.Errorf("product after Update(p, nil) (-want +got):\n%s", diff)
		}
	})

	t.Run("an input that sets no field sends no UPDATE and returns the row unchanged", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		p := createProduct(t, h, "p")
		h.Querier.ResetCount()
		got, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{})
		if err != nil {
			t.Fatalf("Update(p, empty input) error = %v, want nil", err)
		}
		if diff := cmp.Diff(p, got); diff != "" {
			t.Errorf("Update(p, empty input) (-want +got):\n%s", diff)
		}
		if updates := statementsWithPrefix(h, "UPDATE"); len(updates) != 0 {
			t.Errorf("Update(p, empty input) sent %q, want no UPDATE", updates)
		}
	})

	t.Run("a missing key returns ErrNotFound on a strict table, with or without fields set", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		missing := p.ID + 1000
		for _, tt := range []struct {
			name string
			in   *models.UpdateProductInput
		}{
			{"Name", &models.UpdateProductInput{Name: omittable.Set("x")}},
			{"empty input", &models.UpdateProductInput{}},
		} {
			got, err := h.Client.Products().Update(ctx, missing, tt.in)
			if !errors.Is(err, models.ErrNotFound) || got != nil {
				t.Errorf("Update(missing key, %s) = %+v, %v, want nil, ErrNotFound", tt.name, got, err)
			}
		}
		if diff := cmp.Diff(p, upProduct(t, h, p.ID)); diff != "" {
			t.Errorf("product after Update(missing key) (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("an update that leaves the row as it was still matches it", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		cn := upCreateCounter(t, h, "c")
		client, affected := affectedPKsRecorder(h)
		// Strict: the row matched, so it is returned rather than ErrNotFound.
		got, err := client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("p")})
		if err != nil {
			t.Fatalf("Update(p, Name unchanged) error = %v, want nil", err)
		}
		if diff := cmp.Diff(p, got); diff != "" {
			t.Errorf("Update(p, Name unchanged) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{p.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Update(p, Name unchanged) (-want +got):\n%s", diff)
		}
		// Not strict: the row matched, so it is returned and recorded.
		gotCn, err := client.Counters().Update(ctx, cn.ID, &models.UpdateCounterInput{
			Hits:      omittable.Set(cn.Hits),
			UpdatedAt: omittable.Set(cn.UpdatedAt),
		})
		if err != nil {
			t.Fatalf("Update(counter, unchanged) error = %v, want nil", err)
		}
		if diff := cmp.Diff(cn, gotCn); diff != "" {
			t.Errorf("Update(counter, unchanged) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{cn.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Update(counter, unchanged) (-want +got):\n%s", diff)
		}
	})

	t.Run("without strict updates only a key that matches a row is recorded as affected", func(t *testing.T) {
		h.Reset(t)
		cn := upCreateCounter(t, h, "c")
		client, affected := affectedPKsRecorder(h)
		got, err := client.Counters().Update(ctx, cn.ID+1000, &models.UpdateCounterInput{Hits: omittable.Set(int64(1))})
		if err != nil || got != nil {
			t.Fatalf("Update(missing key, Hits) = %+v, %v, want nil, nil", got, err)
		}
		if got := affected(); len(got) != 0 {
			t.Errorf("AffectedPKs of Update(missing key) = %v, want none", got)
		}
		if _, err := client.Counters().Update(ctx, cn.ID, &models.UpdateCounterInput{Hits: omittable.Set(int64(2))}); err != nil {
			t.Fatalf("Update(counter, Hits) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]any{cn.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of Update(counter, Hits) (-want +got):\n%s", diff)
		}
	})

	t.Run("updates inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Products().Update(txCtx, p.ID, &models.UpdateProductInput{Name: omittable.Set("tx")}); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Update inside the transaction sent %q outside it, want nothing", stmts)
			}
			got, err := h.Client.Products().Get(txCtx, p.ID)
			if err != nil {
				return err
			}
			if got.Name != "tx" {
				t.Errorf("Get inside the transaction: Name = %q, want tx", got.Name)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if got := upProduct(t, h, p.ID); got.Name != "tx" {
			t.Errorf("product Name after the commit = %q, want tx", got.Name)
		}
	})

	t.Run("rolled-back transaction leaves the row unchanged", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Update(txCtx, p.ID, &models.UpdateProductInput{Name: omittable.Set("tx")}); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			got, err := h.Client.Products().Get(txCtx, p.ID)
			if err != nil {
				return err
			}
			if got.Name != "tx" {
				t.Errorf("Get inside the transaction: Name = %q, want tx", got.Name)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		if diff := cmp.Diff(p, upProduct(t, h, p.ID)); diff != "" {
			t.Errorf("product after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one update mutation hook", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		p := createProduct(t, h, "p")
		client, seen := mutationRecorder(h)
		in := &models.UpdateProductInput{Name: omittable.Set("p2")}
		if _, err := client.Products().Update(ctx, p.ID, in); err != nil {
			t.Fatalf("Update(p, Name) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpUpdate || m.Table != models.TableProducts || m.PK != any(p.ID) {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %d}",
				m.Op, m.Table, m.PK, hook.OpUpdate, models.TableProducts, p.ID)
		}
		if gotIn, ok := m.Input.(*models.UpdateProductInput); !ok || gotIn != in {
			t.Errorf("MutationContext.Input = %#v, want the caller's input", m.Input)
		}
		if diff := cmp.Diff([]any{p.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("soft-deleting a pinned document returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		pinned := createDocument(t, h, "pinned")
		_, err := h.Client.Documents().Update(ctx, pinned.ID, &models.UpdateDocumentInput{
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		})
		upAssertConstraint(t, err, models.ConstraintCheck)
		assertLiveDocuments(t, h, 1)
	})

	t.Run("renaming a counter to a taken name returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		upCreateCounter(t, h, "a")
		b := upCreateCounter(t, h, "b")
		_, err := h.Client.Counters().Update(ctx, b.ID, &models.UpdateCounterInput{Name: omittable.Set("a")})
		upAssertConstraint(t, err, models.ConstraintUnique)
		if diff := cmp.Diff(b, upCounter(t, h, b.ID)); diff != "" {
			t.Errorf("counter b after the failed Update (-want +got):\n%s", diff)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		h.Querier.ResetCount()
		got, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("p2")},
			func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.FieldOptions = &models.ProductFieldOptions{Name: true}
			})
		if err != nil {
			t.Fatalf("Update(p, Name only) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"UPDATE", "SELECT"}, upStatementVerbs(h)); diff != "" {
			t.Errorf("statements of Update(p, Name only) (-want +got):\n%s", diff)
		}
		// The selected column carries the value the write just made.
		if diff := cmp.Diff(&models.Product{Name: "p2"}, got); diff != "" {
			t.Errorf("Update(p, Name only) (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions updates and returns nil without reading the row back", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		h.Querier.ResetCount()
		got, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("p2")},
			func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.FieldOptions = &models.ProductFieldOptions{}
			})
		if err != nil || got != nil {
			t.Fatalf("Update(p, empty FieldOptions) = %+v, %v, want nil, nil", got, err)
		}
		if diff := cmp.Diff([]string{"UPDATE"}, upStatementVerbs(h)); diff != "" {
			t.Errorf("statements of Update(p, empty FieldOptions) (-want +got):\n%s", diff)
		}
		if got := upProduct(t, h, p.ID); got.Name != "p2" {
			t.Errorf("product Name after Update(p, empty FieldOptions) = %q, want p2", got.Name)
		}
	})

	t.Run("an empty FieldOptions with an input that sets no field sends no statement and returns nil", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		h.Querier.ResetCount()
		got, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{},
			func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.FieldOptions = &models.ProductFieldOptions{}
			})
		if err != nil || got != nil {
			t.Fatalf("Update(p, empty input, empty FieldOptions) = %+v, %v, want nil, nil", got, err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("Update(p, empty input, empty FieldOptions) sent %q, want nothing", stmts)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		lock := withLock[models.ProductFieldOptions](sql.LockForUpdate)
		got, err := h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{Name: omittable.Set("p2")}, lock)
		if err != nil {
			t.Fatalf("Update(p, Name, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.Name != "p2" {
			t.Errorf("Update(p, Name, LockForUpdate) = %+v, want p named p2", got)
		}
		// The empty-update path reads the row as well, and ignores LockMode too.
		got, err = h.Client.Products().Update(ctx, p.ID, &models.UpdateProductInput{}, lock)
		if err != nil {
			t.Fatalf("Update(p, empty input, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.Name != "p2" {
			t.Errorf("Update(p, empty input, LockForUpdate) = %+v, want p named p2", got)
		}
	})

	t.Run("composite key updates only the given tuple", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		target := models.OrderItemPK{OrderID: 1, ProductID: q.ID}
		// Each of these shares one key column with the target, and each comes
		// before it in insertion and key order, so a match on one column
		// would reach one of them first.
		others := []models.OrderItemPK{{OrderID: 1, ProductID: p.ID}, {OrderID: 2, ProductID: q.ID}}
		for _, pk := range append(append([]models.OrderItemPK{}, others...), target) {
			createOrderItem(t, h, pk)
		}
		got, err := h.Client.OrderItems().Update(ctx, target, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(5))})
		if err != nil {
			t.Fatalf("Update(%+v, Quantity) error = %v, want nil", target, err)
		}
		if diff := cmp.Diff(&models.OrderItem{OrderID: 1, ProductID: q.ID, Quantity: 5}, got); diff != "" {
			t.Errorf("Update(%+v, Quantity) (-want +got):\n%s", target, diff)
		}
		want := map[models.OrderItemPK]int64{target: 5, others[0]: 1, others[1]: 1}
		if diff := cmp.Diff(want, upOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("order item quantities after Update(%+v) (-want +got):\n%s", target, diff)
		}
	})

	t.Run("an update that leaves out the update column sets it", func(t *testing.T) {
		h.Reset(t)
		old := upSeedStaleCounter(t, h, "c", "2000-01-01 00:00:00")
		before := time.Now()
		got, err := h.Client.Counters().Update(ctx, old.ID, &models.UpdateCounterInput{Hits: omittable.Set(int64(1))})
		if err != nil {
			t.Fatalf("Update(counter, Hits) error = %v, want nil", err)
		}
		after := time.Now()
		if got == nil {
			t.Fatal("Update(counter, Hits) = nil, want the counter")
		}
		upAssertNow(t, "returned UpdatedAt", got.UpdatedAt, before, after)
		upAssertNow(t, "stored UpdatedAt", upCounter(t, h, old.ID).UpdatedAt, before, after)
	})

	t.Run("an update that sets the update column writes the given value", func(t *testing.T) {
		h.Reset(t)
		cn := upSeedStaleCounter(t, h, "c", "2000-01-01 00:00:00")
		given := upSeedStaleCounter(t, h, "given", "2001-01-01 00:00:00").UpdatedAt
		got, err := h.Client.Counters().Update(ctx, cn.ID, &models.UpdateCounterInput{
			Hits:      omittable.Set(int64(1)),
			UpdatedAt: omittable.Set(given),
		})
		if err != nil {
			t.Fatalf("Update(counter, Hits, UpdatedAt) error = %v, want nil", err)
		}
		if got == nil || !cmp.Equal(got.UpdatedAt, given) {
			t.Errorf("Update(counter, Hits, UpdatedAt) = %+v, want UpdatedAt %v", got, given)
		}
		if diff := cmp.Diff(given, upCounter(t, h, cn.ID).UpdatedAt); diff != "" {
			t.Errorf("stored UpdatedAt (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty update leaves the update column unchanged", func(t *testing.T) {
		h.Reset(t)
		old := upSeedStaleCounter(t, h, "c", "2000-01-01 00:00:00")
		got, err := h.Client.Counters().Update(ctx, old.ID, &models.UpdateCounterInput{})
		if err != nil {
			t.Fatalf("Update(counter, empty input) error = %v, want nil", err)
		}
		if diff := cmp.Diff(old, got); diff != "" {
			t.Errorf("Update(counter, empty input) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(old, upCounter(t, h, old.ID)); diff != "" {
			t.Errorf("counter after Update(counter, empty input) (-want +got):\n%s", diff)
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

	t.Run("writes each item's own fields across sub-batches and returns the updated rows", func(t *testing.T) {
		h.Reset(t)
		a, b, c, d := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c"), createProduct(t, h, "d")
		// With batch_size: 2, {b, c} is the first sub-batch and {d} the second.
		got, err := h.Client.Products().UpdateMany(ctx, []models.UpdateProductItem{
			{ID: b.ID, Input: &models.UpdateProductInput{Name: omittable.Set("b2")}},
			{ID: c.ID, Input: &models.UpdateProductInput{Price: omittable.Set(int64(300))}},
			{ID: d.ID, Input: &models.UpdateProductInput{Name: omittable.Set("d2"), Price: omittable.Set(int64(400))}},
		})
		if err != nil {
			t.Fatalf("UpdateMany([b, c, d]) error = %v, want nil", err)
		}
		wantB, wantC, wantD := *b, *c, *d
		wantB.Name = "b2"
		wantC.Price = 300
		wantD.Name, wantD.Price = "d2", 400
		want := []*models.Product{&wantB, &wantC, &wantD}
		byID := cmpopts.SortSlices(func(x, y *models.Product) bool { return x.ID < y.ID })
		if diff := cmp.Diff(want, got, byID); diff != "" {
			t.Errorf("UpdateMany([b, c, d]) (-want +got):\n%s", diff)
		}
		// Fields an item did not set keep their values; the unlisted row is untouched.
		for _, p := range append(want, a) {
			if diff := cmp.Diff(p, upProduct(t, h, p.ID)); diff != "" {
				t.Errorf("product %d after UpdateMany (-want +got):\n%s", p.ID, diff)
			}
		}
	})

	t.Run("an item with a nil input returns ErrNilInput, writes nothing and fires no hook", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		client, seen := mutationRecorder(h)
		// The nil input is in the second sub-batch: the first is not written either.
		got, err := client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
			{ID: ids[0], Input: &models.UpdateDocumentInput{Title: omittable.Set("updated")}},
			{ID: ids[1], Input: &models.UpdateDocumentInput{Title: omittable.Set("updated")}},
			{ID: ids[2]},
		})
		if !errors.Is(err, models.ErrNilInput) || got != nil {
			t.Errorf("UpdateMany(nil item input) = %+v, %v, want nil, ErrNilInput", got, err)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("mutation hook fired %d times for a nil item input, want 0", n)
		}
		want := map[int64]string{ids[0]: "doc", ids[1]: "doc", ids[2]: "doc"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after UpdateMany(nil item input) (-want +got):\n%s", diff)
		}
	})

	t.Run("nil item list sends no statement and returns nothing", func(t *testing.T) {
		upAssertNoStatement(t, h, nil)
	})

	t.Run("empty item list sends no statement and returns nothing", func(t *testing.T) {
		upAssertNoStatement(t, h, []models.UpdateDocumentItem{})
	})

	t.Run("on a strict table a key matching no row is not an error and is not returned", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "d")
		missing := d.ID + 1000
		// The second client's querier cannot send a batch, so on pgx it runs
		// the statements one by one.
		for i, client := range []*models.Client{h.Client, models.New(h.Querier)} {
			title := fmt.Sprintf("d%d", i)
			got, err := client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
				{ID: missing, Input: &models.UpdateDocumentInput{Title: omittable.Set("x")}},
				{ID: d.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set(title)}},
			})
			if err != nil {
				t.Fatalf("client %d: UpdateMany([missing, d]) error = %v, want nil", i, err)
			}
			if len(got) != 1 || got[0].ID != d.ID || got[0].Title != title {
				t.Errorf("client %d: UpdateMany([missing, d]) = %+v, want only d titled %s", i, got, title)
			}
			got, err = client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
				{ID: missing, Input: &models.UpdateDocumentInput{Title: omittable.Set("x")}},
			})
			if err != nil {
				t.Fatalf("client %d: UpdateMany([missing]) error = %v, want nil", i, err)
			}
			if len(got) != 0 {
				t.Errorf("client %d: UpdateMany([missing]) returned %+v, want none", i, got)
			}
		}
		assertRowCount(t, h, "documents", 1)
	})

	t.Run("without strict updates a key matching no row is not an error and is not returned", func(t *testing.T) {
		h.Reset(t)
		cn := upCreateCounter(t, h, "c")
		missing := cn.ID + 1000
		recorder, affected := affectedPKsRecorder(h)
		// The second client's querier cannot send a batch, so on pgx it runs
		// the statements one by one.
		for i, client := range []*models.Client{recorder, models.New(h.Querier)} {
			hits := int64(5 + i)
			got, err := client.Counters().UpdateMany(ctx, []models.UpdateCounterItem{
				{ID: missing, Input: &models.UpdateCounterInput{Hits: omittable.Set(int64(9))}},
				{ID: cn.ID, Input: &models.UpdateCounterInput{Hits: omittable.Set(hits)}},
			})
			if err != nil {
				t.Fatalf("client %d: UpdateMany([missing, c]) error = %v, want nil", i, err)
			}
			if len(got) != 1 || got[0].ID != cn.ID || got[0].Hits != hits {
				t.Errorf("client %d: UpdateMany([missing, c]) = %+v, want only c with Hits %d", i, got, hits)
			}
			if i == 0 {
				if diff := cmp.Diff([]any{cn.ID}, affected()); diff != "" {
					t.Errorf("AffectedPKs of UpdateMany([missing, c]) (-want +got):\n%s", diff)
				}
			}
			got, err = client.Counters().UpdateMany(ctx, []models.UpdateCounterItem{
				{ID: missing, Input: &models.UpdateCounterInput{Hits: omittable.Set(int64(9))}},
			})
			if err != nil {
				t.Fatalf("client %d: UpdateMany([missing]) error = %v, want nil", i, err)
			}
			if len(got) != 0 {
				t.Errorf("client %d: UpdateMany([missing]) returned %+v, want none", i, got)
			}
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("updates inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Documents().UpdateMany(txCtx, upRetitleItems(ids, "tx")); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("UpdateMany inside the transaction sent %q outside it, want nothing", stmts)
			}
			titles, err := upDocumentTitlesIn(txCtx, h)
			if err != nil {
				return err
			}
			if diff := cmp.Diff(upTitles(ids, "tx"), titles); diff != "" {
				t.Errorf("document titles inside the transaction (-want +got):\n%s", diff)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff(upTitles(ids, "tx"), upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the commit (-want +got):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves the rows unchanged", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Documents().UpdateMany(txCtx, upRetitleItems(ids, "tx")); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			titles, err := upDocumentTitlesIn(txCtx, h)
			if err != nil {
				return err
			}
			if diff := cmp.Diff(upTitles(ids, "tx"), titles); diff != "" {
				t.Errorf("document titles inside the transaction (-want +got):\n%s", diff)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		if diff := cmp.Diff(upTitles(ids, "doc"), upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one update_many mutation hook that records the written items in list order", func(t *testing.T) {
		h.Reset(t)
		a, b, d := createDocument(t, h, "a"), createDocument(t, h, "b"), createDocument(t, h, "d")
		missing := d.ID + 1000
		// Listed out of key order and across two sub-batches, {d, missing} and
		// {b, a}; the missing key writes nothing.
		items := []models.UpdateDocumentItem{
			{ID: d.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("d2")}},
			{ID: missing, Input: &models.UpdateDocumentInput{Title: omittable.Set("x")}},
			{ID: b.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("b2")}},
			{ID: a.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("a2")}},
		}
		client, seen := mutationRecorder(h)
		if _, err := client.Documents().UpdateMany(ctx, items); err != nil {
			t.Fatalf("UpdateMany([d, missing, b, a]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpUpdateMany || m.Table != models.TableDocuments || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpUpdateMany, models.TableDocuments)
		}
		if diff := cmp.Diff([]any{d.ID, b.ID, a.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		// After the mutation Input is narrowed to the written items, index-aligned with AffectedPKs.
		written, ok := m.Input.([]models.UpdateDocumentItem)
		if !ok {
			t.Fatalf("MutationContext.Input = %T, want []UpdateDocumentItem", m.Input)
		}
		wantItems := []models.UpdateDocumentItem{items[0], items[2], items[3]}
		if len(written) != len(wantItems) {
			t.Fatalf("MutationContext.Input has %d items, want %d", len(written), len(wantItems))
		}
		for i := range wantItems {
			if written[i].ID != wantItems[i].ID || written[i].Input != wantItems[i].Input {
				t.Errorf("MutationContext.Input[%d] = {ID: %d, Input: %p}, want {ID: %d, Input: %p}",
					i, written[i].ID, written[i].Input, wantItems[i].ID, wantItems[i].Input)
			}
		}
	})

	t.Run("an item that leaves its row as it was is still recorded as affected", func(t *testing.T) {
		h.Reset(t)
		changed, unchanged := createDocument(t, h, "a"), createDocument(t, h, "b")
		client, affected := affectedPKsRecorder(h)
		// Both items are in one sub-batch: the first changes its row, the
		// second leaves its row as it was.
		got, err := client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
			{ID: changed.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("a2")}},
			{ID: unchanged.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("b")}},
		})
		if err != nil {
			t.Fatalf("UpdateMany([changed, unchanged]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"a2", "b"}, upSortedTitles(got)); diff != "" {
			t.Errorf("UpdateMany([changed, unchanged]) returned titles (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]any{changed.ID, unchanged.ID}, affected()); diff != "" {
			t.Errorf("AffectedPKs of UpdateMany([changed, unchanged]) (-want +got):\n%s", diff)
		}
	})

	t.Run("soft-deleting a pinned document returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		valid, pinned := createDocument(t, h, "valid"), createDocument(t, h, "pinned")
		// One sub-batch: a valid item, then the one the CHECK rejects.
		_, err := h.Client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
			{ID: valid.ID, Input: &models.UpdateDocumentInput{Title: omittable.Set("valid2")}},
			{ID: pinned.ID, Input: &models.UpdateDocumentInput{DeletedAt: omittable.Set(models.DeletedAt(models.Now()))}},
		})
		upAssertConstraint(t, err, models.ConstraintCheck)
		// The failing item rolls back its sub-batch, the valid item with it.
		want := map[int64]string{valid.ID: "valid", pinned.ID: "pinned"}
		if diff := cmp.Diff(want, upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after the failed UpdateMany (-want +got):\n%s", diff)
		}
		assertLiveDocuments(t, h, 2)
	})

	t.Run("renaming a counter to a taken name returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		upCreateCounter(t, h, "a")
		b, c := upCreateCounter(t, h, "b"), upCreateCounter(t, h, "c")
		// One sub-batch: a valid item, then the one the UNIQUE rejects.
		_, err := h.Client.Counters().UpdateMany(ctx, []models.UpdateCounterItem{
			{ID: b.ID, Input: &models.UpdateCounterInput{Hits: omittable.Set(int64(1))}},
			{ID: c.ID, Input: &models.UpdateCounterInput{Name: omittable.Set("a")}},
		})
		upAssertConstraint(t, err, models.ConstraintUnique)
		// The failing item rolls back its sub-batch, the valid item with it.
		for _, cn := range []*models.Counter{b, c} {
			if diff := cmp.Diff(cn, upCounter(t, h, cn.ID)); diff != "" {
				t.Errorf("counter %s after the failed UpdateMany (-want +got):\n%s", cn.Name, diff)
			}
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		got, err := h.Client.Documents().UpdateMany(ctx, []models.UpdateDocumentItem{
			{ID: ids[0], Input: &models.UpdateDocumentInput{Title: omittable.Set("a")}},
			{ID: ids[1], Input: &models.UpdateDocumentInput{Title: omittable.Set("b")}},
			{ID: ids[2], Input: &models.UpdateDocumentInput{Title: omittable.Set("c")}},
		}, func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{Title: true}
		})
		if err != nil {
			t.Fatalf("UpdateMany(3 items, Title only) error = %v, want nil", err)
		}
		// The selected column carries the values the write just made.
		want := []*models.Document{{Title: "a"}, {Title: "b"}, {Title: "c"}}
		byTitle := cmpopts.SortSlices(func(x, y *models.Document) bool { return x.Title < y.Title })
		if diff := cmp.Diff(want, got, byTitle); diff != "" {
			t.Errorf("UpdateMany(3 items, Title only) (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions updates and returns nil without reading the rows back", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		h.Querier.ResetCount()
		got, err := h.Client.Documents().UpdateMany(ctx, upRetitleItems(ids, "updated"), func(o *models.CallOptions[models.DocumentFieldOptions]) {
			o.FieldOptions = &models.DocumentFieldOptions{}
		})
		if err != nil || got != nil {
			t.Fatalf("UpdateMany(3 items, empty FieldOptions) = %+v, %v, want nil, nil", got, err)
		}
		if selects := statementsWithPrefix(h, "SELECT"); len(selects) != 0 {
			t.Errorf("UpdateMany(3 items, empty FieldOptions) sent %q, want no read", selects)
		}
		if diff := cmp.Diff(upTitles(ids, "updated"), upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after UpdateMany (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		ids := createDocuments(t, h, 3)
		got, err := h.Client.Documents().UpdateMany(ctx, upRetitleItems(ids, "updated"),
			withLock[models.DocumentFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("UpdateMany(3 items, LockForUpdate) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"updated", "updated", "updated"}, upSortedTitles(got)); diff != "" {
			t.Errorf("UpdateMany(3 items, LockForUpdate) returned titles (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(upTitles(ids, "updated"), upDocumentTitles(t, h)); diff != "" {
			t.Errorf("document titles after UpdateMany (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys update only the listed tuples and return soft-deleted ones", func(t *testing.T) {
		h.Reset(t)
		listed := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}, {DocumentID: 3, Number: 1}}
		// The unlisted keys cross the first two listed ones: each column value
		// is listed, only the pairing is not.
		unlisted := []models.RevisionPK{{DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}}
		for _, pk := range append(append([]models.RevisionPK{}, listed...), unlisted...) {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDelete(ctx, listed[2]); err != nil {
			t.Fatalf("SoftDelete(%+v) setup error = %v", listed[2], err)
		}
		// With batch_size: 2, the third listed tuple is a sub-batch of its own.
		items := make([]models.UpdateRevisionItem, len(listed))
		for i, pk := range listed {
			items[i] = models.UpdateRevisionItem{PK: pk, Input: &models.UpdateRevisionInput{Body: omittable.Set("new")}}
		}
		got, err := h.Client.Revisions().UpdateMany(ctx, items)
		if err != nil {
			t.Fatalf("UpdateMany(%+v) error = %v, want nil", listed, err)
		}
		// A per-column read-back would also return the crossing tuples.
		var gotPKs []models.RevisionPK
		for _, r := range got {
			gotPKs = append(gotPKs, models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number})
			if r.Body != "new" {
				t.Errorf("UpdateMany returned %+v with Body %q, want new", r, r.Body)
			}
			if deleted := !cmp.Equal(r.DeletedAt, models.Revision{}.DeletedAt); deleted != (r.DocumentID == 3) {
				t.Errorf("UpdateMany returned %+v: soft-deleted = %v, want %v", r, deleted, r.DocumentID == 3)
			}
		}
		slices.SortFunc(gotPKs, upCompareRevisionPKs)
		if diff := cmp.Diff(listed, gotPKs); diff != "" {
			t.Errorf("UpdateMany(%+v) returned keys (-want +got):\n%s", listed, diff)
		}
		want := map[models.RevisionPK]string{listed[0]: "new", listed[1]: "new", listed[2]: "new", unlisted[0]: "body", unlisted[1]: "body"}
		if diff := cmp.Diff(want, upRevisionBodies(t, h)); diff != "" {
			t.Errorf("revision bodies after UpdateMany (-want +got):\n%s", diff)
		}
	})

	t.Run("sets each item's omitted update column and writes a given one as is", func(t *testing.T) {
		h.Reset(t)
		omitted := upSeedStaleCounter(t, h, "omitted", "2000-01-01 00:00:00")
		given := upSeedStaleCounter(t, h, "given", "2000-01-01 00:00:00")
		empty := upSeedStaleCounter(t, h, "empty", "2000-01-01 00:00:00")
		value := upSeedStaleCounter(t, h, "value", "2001-01-01 00:00:00").UpdatedAt
		before := time.Now()
		got, err := h.Client.Counters().UpdateMany(ctx, []models.UpdateCounterItem{
			{ID: omitted.ID, Input: &models.UpdateCounterInput{Hits: omittable.Set(int64(1))}},
			{ID: given.ID, Input: &models.UpdateCounterInput{Hits: omittable.Set(int64(1)), UpdatedAt: omittable.Set(value)}},
			{ID: empty.ID, Input: &models.UpdateCounterInput{}},
		})
		if err != nil {
			t.Fatalf("UpdateMany(counters) error = %v, want nil", err)
		}
		after := time.Now()
		var returned *models.Counter
		for _, cn := range got {
			if cn.ID == omitted.ID {
				returned = cn
			}
		}
		if returned == nil {
			t.Fatalf("UpdateMany(counters) = %+v, want the counter that left UpdatedAt out among them", got)
		}
		upAssertNow(t, "returned UpdatedAt of the item that left it out", returned.UpdatedAt, before, after)
		upAssertNow(t, "stored UpdatedAt of the item that left it out", upCounter(t, h, omitted.ID).UpdatedAt, before, after)
		if diff := cmp.Diff(value, upCounter(t, h, given.ID).UpdatedAt); diff != "" {
			t.Errorf("UpdatedAt of the item that set it (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(empty, upCounter(t, h, empty.ID)); diff != "" {
			t.Errorf("counter of the item that set no field (-want +got):\n%s", diff)
		}
	})
}

// upAssertNoStatement checks that UpdateMany(items), with no item, sends no
// statement and returns nothing while the table holds a row.
func upAssertNoStatement(t *testing.T, h *Harness, items []models.UpdateDocumentItem) {
	t.Helper()
	h.Reset(t)
	createDocument(t, h, "a")
	h.Querier.ResetCount()
	got, err := h.Client.Documents().UpdateMany(context.Background(), items)
	if err != nil {
		t.Fatalf("UpdateMany(%#v) error = %v, want nil", items, err)
	}
	if stmts := h.Querier.Statements(); len(stmts) != 0 {
		t.Errorf("UpdateMany(%#v) sent %q, want nothing", items, stmts)
	}
	if len(got) != 0 {
		t.Errorf("UpdateMany(%#v) returned %d documents, want none", items, len(got))
	}
}

// upRetitleItems retitles each document in ids to title.
func upRetitleItems(ids []int64, title string) []models.UpdateDocumentItem {
	items := make([]models.UpdateDocumentItem, len(ids))
	for i, id := range ids {
		items[i] = models.UpdateDocumentItem{ID: id, Input: &models.UpdateDocumentInput{Title: omittable.Set(title)}}
	}
	return items
}

// upTitles maps each of ids to title.
func upTitles(ids []int64, title string) map[int64]string {
	m := make(map[int64]string, len(ids))
	for _, id := range ids {
		m[id] = title
	}
	return m
}

func upSortedTitles(docs []*models.Document) []string {
	titles := make([]string, 0, len(docs))
	for _, d := range docs {
		titles = append(titles, d.Title)
	}
	slices.Sort(titles)
	return titles
}

// upAssertNow checks that ts, a time the database stored and returned, falls
// within a second of the window [before, after] the write ran in.
func upAssertNow(t *testing.T, what string, ts interface{ UnixNano() int64 }, before, after time.Time) {
	t.Helper()
	got := time.Unix(0, ts.UnixNano())
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("%s = %v, want within a second of [%v, %v]", what, got.UTC(), before.UTC(), after.UTC())
	}
}

// upAssertConstraint fails t unless err is a ConstraintError of type want that
// unwraps to ErrConstraintViolation.
func upAssertConstraint(t *testing.T, err error, want database.ConstraintType) {
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

// upStatementVerbs returns the first word of each recorded statement.
func upStatementVerbs(h *Harness) []string {
	var verbs []string
	for _, s := range h.Querier.Statements() {
		verb, _, _ := strings.Cut(strings.TrimSpace(s), " ")
		verbs = append(verbs, verb)
	}
	return verbs
}

func upProduct(t *testing.T, h *Harness, id int64) *models.Product {
	t.Helper()
	p, err := h.Client.Products().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Products().Get(%d) error = %v", id, err)
	}
	return p
}

func upCounter(t *testing.T, h *Harness, id int64) *models.Counter {
	t.Helper()
	cn, err := h.Client.Counters().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Counters().Get(%d) error = %v", id, err)
	}
	return cn
}

func upCreateCounter(t *testing.T, h *Harness, name string) *models.Counter {
	t.Helper()
	cn, err := h.Client.Counters().Create(context.Background(), &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())})
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	return cn
}

// upSeedStaleCounter inserts a counter whose updated_at is the literal
// updatedAt, long past, so a write that sets the column to now moves it.
func upSeedStaleCounter(t *testing.T, h *Harness, name, updatedAt string) *models.Counter {
	t.Helper()
	ctx := context.Background()
	if _, err := h.Querier.Inner.Exec(ctx, "INSERT INTO counters (name, updated_at) VALUES ('"+name+"', '"+updatedAt+"')"); err != nil {
		t.Fatalf("seed counter %q: %v", name, err)
	}
	var id int64
	if err := h.Querier.Inner.QueryRow(ctx, "SELECT id FROM counters WHERE name = '"+name+"'").Scan(&id); err != nil {
		t.Fatalf("read counter %q key: %v", name, err)
	}
	return upCounter(t, h, id)
}

// upOrderItemQuantities reads every order item's quantity with a statement
// the counting querier does not record.
func upOrderItemQuantities(t *testing.T, h *Harness) map[models.OrderItemPK]int64 {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT order_id, product_id, quantity FROM order_items")
	if err != nil {
		t.Fatalf("read order items: %v", err)
	}
	defer rows.Close()
	out := make(map[models.OrderItemPK]int64)
	for rows.Next() {
		var (
			pk models.OrderItemPK
			q  int64
		)
		if err := rows.Scan(&pk.OrderID, &pk.ProductID, &q); err != nil {
			t.Fatalf("scan order item: %v", err)
		}
		out[pk] = q
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read order items: %v", err)
	}
	return out
}

// upRevisionBodies reads every revision's body, soft-deleted ones included,
// with a statement the counting querier does not record.
func upRevisionBodies(t *testing.T, h *Harness) map[models.RevisionPK]string {
	t.Helper()
	rows, err := h.Querier.Inner.Query(context.Background(), "SELECT document_id, number, body FROM revisions")
	if err != nil {
		t.Fatalf("read revisions: %v", err)
	}
	defer rows.Close()
	out := make(map[models.RevisionPK]string)
	for rows.Next() {
		var (
			pk   models.RevisionPK
			body string
		)
		if err := rows.Scan(&pk.DocumentID, &pk.Number, &body); err != nil {
			t.Fatalf("scan revision: %v", err)
		}
		out[pk] = body
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read revisions: %v", err)
	}
	return out
}

func upCompareRevisionPKs(a, b models.RevisionPK) int {
	switch {
	case a.DocumentID != b.DocumentID:
		return int(a.DocumentID - b.DocumentID)
	default:
		return int(a.Number - b.Number)
	}
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
	cn := upCreateCounter(t, h, name)
	deleted, err := h.Client.Counters().SoftDelete(context.Background(), cn.ID)
	if err != nil || deleted == nil {
		t.Fatalf("SoftDelete counter %q = %+v, %v", name, deleted, err)
	}
	return deleted
}
