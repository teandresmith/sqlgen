package suite

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// hardDeleteMany: HardDeleteMany permanently removes every row whose key is listed.
func hardDeleteMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("deletes only the listed keys", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c")
		if err := h.Client.Products().HardDeleteMany(ctx, []int64{a.ID, c.ID}); err != nil {
			t.Fatalf("HardDeleteMany([a, c]) error = %v, want nil", err)
		}
		assertProductExists(t, h, a.ID, false)
		assertProductExists(t, h, b.ID, true)
		assertProductExists(t, h, c.ID, false)
	})

	t.Run("nil key list deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		if err := h.Client.Products().HardDeleteMany(ctx, nil); err != nil {
			t.Fatalf("HardDeleteMany(nil) error = %v, want nil", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("empty key list deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		if err := h.Client.Products().HardDeleteMany(ctx, []int64{}); err != nil {
			t.Fatalf("HardDeleteMany([]) error = %v, want nil", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("missing keys are not an error", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		missing := a.ID + 1000
		if err := h.Client.Products().HardDeleteMany(ctx, []int64{a.ID, missing}); err != nil {
			t.Fatalf("HardDeleteMany([a, missing]) error = %v, want nil", err)
		}
		assertProductExists(t, h, a.ID, false)
		if err := h.Client.Products().HardDeleteMany(ctx, []int64{missing}); err != nil {
			t.Fatalf("HardDeleteMany([missing]) error = %v, want nil", err)
		}
	})

	t.Run("deletes listed rows whether or not they are soft-deleted", func(t *testing.T) {
		h.Reset(t)
		live, trashed := createDocument(t, h, "live"), createDocument(t, h, "trashed")
		createDocument(t, h, "kept")
		if _, err := h.Client.Documents().SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete(trashed) error = %v", err)
		}
		if err := h.Client.Documents().HardDeleteMany(ctx, []int64{live.ID, trashed.ID}); err != nil {
			t.Fatalf("HardDeleteMany([live, trashed]) error = %v, want nil", err)
		}
		assertRowCount(t, h, "documents", 1)
	})

	t.Run("deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if err := h.Client.Products().HardDeleteMany(txCtx, []int64{a.ID}); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("HardDeleteMany inside the transaction sent %q outside it, want nothing", stmts)
			}
			n, err := h.Client.Products().Count(txCtx, nil)
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
		assertProductExists(t, h, a.ID, false)
		assertProductExists(t, h, b.ID, true)
	})

	t.Run("rolled-back transaction leaves rows", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if err := h.Client.Products().HardDeleteMany(txCtx, []int64{a.ID}); err != nil {
				return err
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertProductExists(t, h, a.ID, true)
	})

	t.Run("fires one hard_delete_many mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c")
		var seen []hook.MutationContext
		record := func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, m *hook.MutationContext) (any, error) {
				res, err := next(ctx, m)
				seen = append(seen, *m)
				return res, err
			}
		}
		client := models.New(h.Querier, models.WithMutationHook(record))
		if err := client.Products().HardDeleteMany(ctx, []int64{a.ID, c.ID}); err != nil {
			t.Fatalf("HardDeleteMany([a, c]) error = %v, want nil", err)
		}
		if len(seen) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen))
		}
		m := seen[0]
		if m.Op != hook.OpHardDeleteMany || m.Table != models.TableProducts || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpHardDeleteMany, models.TableProducts)
		}
		bySet := cmpopts.SortSlices(func(x, y any) bool { return x.(int64) < y.(int64) })
		if diff := cmp.Diff([]any{a.ID, c.ID}, m.AffectedPKs, bySet); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		assertProductExists(t, h, b.ID, true)
	})

	t.Run("a hook that aborts deletes nothing", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		errAbort := errors.New("abort")
		abort := func(hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) { return nil, errAbort }
		}
		client := models.New(h.Querier, models.WithMutationHook(abort))
		if err := client.Products().HardDeleteMany(ctx, []int64{a.ID}); !errors.Is(err, errAbort) {
			t.Fatalf("HardDeleteMany under an aborting hook error = %v, want the hook's error", err)
		}
		assertProductExists(t, h, a.ID, true)
	})

	t.Run("deleting a referenced row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p.ID})
		err := h.Client.Products().HardDeleteMany(ctx, []int64{p.ID})
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("HardDeleteMany(referenced) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintForeignKey {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintForeignKey)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		assertProductExists(t, h, p.ID, true)
	})

	t.Run("composite keys delete only the listed tuples", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		listed := models.OrderItemPK{OrderID: 1, ProductID: p.ID}
		// Each unlisted key shares one column value with the listed one.
		unlisted := []models.OrderItemPK{{OrderID: 1, ProductID: q.ID}, {OrderID: 2, ProductID: p.ID}}
		for _, pk := range append([]models.OrderItemPK{listed}, unlisted...) {
			createOrderItem(t, h, pk)
		}
		if err := h.Client.OrderItems().HardDeleteMany(ctx, []models.OrderItemPK{listed}); err != nil {
			t.Fatalf("HardDeleteMany(%+v) error = %v, want nil", listed, err)
		}
		for _, pk := range append([]models.OrderItemPK{listed}, unlisted...) {
			got, err := h.Client.OrderItems().Exists(ctx, pk)
			if err != nil {
				t.Fatalf("OrderItems().Exists(%+v) error = %v", pk, err)
			}
			if want := pk != listed; got != want {
				t.Errorf("OrderItems().Exists(%+v) = %v, want %v", pk, got, want)
			}
		}
	})
}
