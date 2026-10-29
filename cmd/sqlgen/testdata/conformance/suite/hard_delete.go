package suite

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// hardDelete: HardDelete permanently removes one row.
func hardDelete(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("deletes only the row with the given key", func(t *testing.T) {
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		if err := h.Client.Products().HardDelete(ctx, a.ID); err != nil {
			t.Fatalf("HardDelete(a) error = %v, want nil", err)
		}
		assertProductExists(t, h, a.ID, false)
		assertProductExists(t, h, b.ID, true)
		assertRowCount(t, h, "products", 1)
	})

	t.Run("a missing key is not an error", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		missing := a.ID + 1000
		if err := h.Client.Products().HardDelete(ctx, missing); err != nil {
			t.Fatalf("HardDelete(missing) error = %v, want nil", err)
		}
		assertProductExists(t, h, a.ID, true)
	})

	t.Run("deletes a soft-deleted row", func(t *testing.T) {
		h.Reset(t)
		trashed := createDocument(t, h, "trashed")
		createDocument(t, h, "kept")
		if _, err := h.Client.Documents().SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete(trashed) error = %v", err)
		}
		if err := h.Client.Documents().HardDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("HardDelete(trashed) error = %v, want nil", err)
		}
		// The physical row is gone, not just hidden: only the live one remains.
		assertRowCount(t, h, "documents", 1)
		assertLiveDocuments(t, h, 1)
	})

	t.Run("deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if err := h.Client.Products().HardDelete(txCtx, a.ID); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("HardDelete inside the transaction sent %q outside it, want nothing", stmts)
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

	t.Run("rolled-back transaction leaves the row", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if err := h.Client.Products().HardDelete(txCtx, a.ID); err != nil {
				return err
			}
			// The delete is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Products().Count(txCtx, nil)
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
		assertProductExists(t, h, a.ID, true)
	})

	t.Run("fires one hard_delete mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		client, seen := mutationRecorder(h)
		if err := client.Products().HardDelete(ctx, a.ID); err != nil {
			t.Fatalf("HardDelete(a) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpHardDelete || m.Table != models.TableProducts || m.PK != any(a.ID) {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %v}",
				m.Op, m.Table, m.PK, hook.OpHardDelete, models.TableProducts, a.ID)
		}
		if diff := cmp.Diff([]any{a.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		assertProductExists(t, h, b.ID, true)
	})

	t.Run("a missing key runs the hooks and records no affected key", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		missing := a.ID + 1000
		client, seen := mutationRecorder(h)
		if err := client.Products().HardDelete(ctx, missing); err != nil {
			t.Fatalf("HardDelete(missing) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		if got := seen()[0].AffectedPKs; len(got) != 0 {
			t.Errorf("AffectedPKs of HardDelete(missing) = %v, want none", got)
		}
	})

	t.Run("deleting a referenced row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p.ID})
		err := h.Client.Products().HardDelete(ctx, p.ID)
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("HardDelete(referenced) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintForeignKey {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintForeignKey)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		assertProductExists(t, h, p.ID, true)
	})

	t.Run("a composite key deletes only that tuple", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		target := models.OrderItemPK{OrderID: 1, ProductID: p.ID}
		// Each other key shares one column value with the target.
		others := []models.OrderItemPK{{OrderID: 1, ProductID: q.ID}, {OrderID: 2, ProductID: p.ID}}
		for _, pk := range append([]models.OrderItemPK{target}, others...) {
			createOrderItem(t, h, pk)
		}
		if err := h.Client.OrderItems().HardDelete(ctx, target); err != nil {
			t.Fatalf("HardDelete(%+v) error = %v, want nil", target, err)
		}
		for _, pk := range append([]models.OrderItemPK{target}, others...) {
			got, err := h.Client.OrderItems().Exists(ctx, pk)
			if err != nil {
				t.Fatalf("OrderItems().Exists(%+v) error = %v", pk, err)
			}
			if want := pk != target; got != want {
				t.Errorf("OrderItems().Exists(%+v) = %v, want %v", pk, got, want)
			}
		}
	})
}

// hardDeleteWhere: HardDeleteWhere permanently removes every row the filter matches.
func hardDeleteWhere(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("deletes only the matching rows", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c")
		filter := &models.ProductFilter{Name: &comparator.String{In: []string{"a", "c"}}}
		if err := h.Client.Products().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(name in [a, c]) error = %v, want nil", err)
		}
		assertProductExists(t, h, a.ID, false)
		assertProductExists(t, h, b.ID, true)
		assertProductExists(t, h, c.ID, false)
	})

	t.Run("nil filter returns ErrEmptyFilter and deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		if err := h.Client.Products().HardDeleteWhere(ctx, nil); !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("HardDeleteWhere(nil) error = %v, want ErrEmptyFilter", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("empty filter returns ErrEmptyFilter and deletes nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		if err := h.Client.Products().HardDeleteWhere(ctx, &models.ProductFilter{}); !errors.Is(err, models.ErrEmptyFilter) {
			t.Fatalf("HardDeleteWhere(&ProductFilter{}) error = %v, want ErrEmptyFilter", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("a filter matching no row is not an error", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		filter := &models.ProductFilter{Name: &comparator.String{Eq: new("missing")}}
		if err := h.Client.Products().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(name = missing) error = %v, want nil", err)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("a filter on the soft-delete column deletes soft-deleted rows", func(t *testing.T) {
		h.Reset(t)
		trashed := createDocument(t, h, "trashed")
		live := createDocument(t, h, "live")
		if _, err := h.Client.Documents().SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete(trashed) error = %v", err)
		}
		filter := &models.DocumentFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}}
		if err := h.Client.Documents().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(deleted_at is not null) error = %v, want nil", err)
		}
		// The soft-deleted row is physically gone and the live one is untouched.
		assertRowCount(t, h, "documents", 1)
		got, err := h.Client.Documents().Exists(ctx, live.ID)
		if err != nil {
			t.Fatalf("Documents().Exists(live) error = %v", err)
		}
		if !got {
			t.Errorf("Documents().Exists(live) = false, want true")
		}
	})

	t.Run("a filter that leaves the soft-delete column unset deletes soft-deleted rows too", func(t *testing.T) {
		h.Reset(t)
		trashed := createDocument(t, h, "doc")
		createDocument(t, h, "doc")
		kept := createDocument(t, h, "kept")
		if _, err := h.Client.Documents().SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete(trashed) error = %v", err)
		}
		filter := &models.DocumentFilter{Title: &comparator.String{Eq: new("doc")}}
		if err := h.Client.Documents().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(title = doc) error = %v, want nil", err)
		}
		// Both "doc" rows are physically gone, the soft-deleted one included.
		assertRowCount(t, h, "documents", 1)
		got, err := h.Client.Documents().Exists(ctx, kept.ID)
		if err != nil {
			t.Fatalf("Documents().Exists(kept) error = %v", err)
		}
		if !got {
			t.Errorf("Documents().Exists(kept) = false, want true")
		}
	})

	t.Run("deletes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		filter := &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}}
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if err := h.Client.Products().HardDeleteWhere(txCtx, filter); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("HardDeleteWhere inside the transaction sent %q outside it, want nothing", stmts)
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
		filter := &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}}
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if err := h.Client.Products().HardDeleteWhere(txCtx, filter); err != nil {
				return err
			}
			// The delete is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Products().Count(txCtx, nil)
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
		assertProductExists(t, h, a.ID, true)
	})

	t.Run("fires one hard_delete_where mutation hook", func(t *testing.T) {
		h.Reset(t)
		a, b, c := createProduct(t, h, "a"), createProduct(t, h, "b"), createProduct(t, h, "c")
		client, seen := mutationRecorder(h)
		filter := &models.ProductFilter{Name: &comparator.String{In: []string{"a", "c"}}}
		if err := client.Products().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(name in [a, c]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpHardDeleteWhere || m.Table != models.TableProducts || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpHardDeleteWhere, models.TableProducts)
		}
		if m.Input != any(filter) {
			t.Errorf("MutationContext.Input = %v, want the filter passed in", m.Input)
		}
		bySet := cmpopts.SortSlices(func(x, y any) bool { return x.(int64) < y.(int64) })
		if diff := cmp.Diff([]any{a.ID, c.ID}, m.AffectedPKs, bySet); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
		assertProductExists(t, h, b.ID, true)
	})

	t.Run("deleting a referenced row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p.ID})
		filter := &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(p.ID)}}
		err := h.Client.Products().HardDeleteWhere(ctx, filter)
		ce, ok := errors.AsType[*models.ConstraintError](err)
		if !ok {
			t.Fatalf("HardDeleteWhere(referenced) error = %v, want a *ConstraintError", err)
		}
		if ce.Type != models.ConstraintForeignKey {
			t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintForeignKey)
		}
		if !errors.Is(err, models.ErrConstraintViolation) {
			t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
		}
		assertProductExists(t, h, p.ID, true)
	})

	t.Run("a composite-key table deletes exactly the matched tuples and records their keys", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		matched := []models.OrderItemPK{{OrderID: 1, ProductID: p.ID}, {OrderID: 1, ProductID: q.ID}}
		// The unmatched tuple shares its product_id with a matched one.
		unmatched := models.OrderItemPK{OrderID: 2, ProductID: p.ID}
		for _, pk := range append(matched, unmatched) {
			createOrderItem(t, h, pk)
		}
		client, affected := affectedPKsRecorder(h)
		filter := &models.OrderItemFilter{OrderID: &comparator.Number[int64]{Eq: new(int64(1))}}
		if err := client.OrderItems().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere(order_id = 1) error = %v, want nil", err)
		}
		for _, pk := range append(matched, unmatched) {
			got, err := h.Client.OrderItems().Exists(ctx, pk)
			if err != nil {
				t.Fatalf("OrderItems().Exists(%+v) error = %v", pk, err)
			}
			if want := pk == unmatched; got != want {
				t.Errorf("OrderItems().Exists(%+v) = %v, want %v", pk, got, want)
			}
		}
		bySet := cmpopts.SortSlices(func(x, y any) bool {
			a, b := x.(models.OrderItemPK), y.(models.OrderItemPK)
			return a.OrderID < b.OrderID || a.OrderID == b.OrderID && a.ProductID < b.ProductID
		})
		if diff := cmp.Diff([]any{matched[0], matched[1]}, affected(), bySet); diff != "" {
			t.Errorf("AffectedPKs of HardDeleteWhere(order_id = 1) (-want +got):\n%s", diff)
		}
	})
}
