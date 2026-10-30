package suite

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// create is the Create contract.
func create(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("writes the row and returns it read back with its generated key and defaults", func(t *testing.T) {
		h.Reset(t)
		// An earlier row, so a read-back that ignored the key would return it.
		earlier := createPricedProduct(t, h, "earlier", 100)
		got, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "written", Price: 250})
		if err != nil {
			t.Fatalf("Create(written) error = %v, want nil", err)
		}
		if got == nil || got.ID == earlier.ID || got.Name != "written" || got.Price != 250 {
			t.Fatalf("Create(written) = %+v, want the written row with a key of its own", got)
		}
		// created_at comes from the column's DEFAULT, so only a read-back carries it.
		if cmp.Equal(got.CreatedAt, models.Product{}.CreatedAt) {
			t.Errorf("Create(written).CreatedAt is zero, want the database default")
		}
		stored, err := h.Client.Products().Get(ctx, got.ID)
		if err != nil {
			t.Fatalf("Get(%d) error = %v", got.ID, err)
		}
		if diff := cmp.Diff(stored, got); diff != "" {
			t.Errorf("Create(written) against the stored row (-stored +returned):\n%s", diff)
		}
		assertRowCount(t, h, "products", 2)
	})

	t.Run("the read-back fires no query hook", func(t *testing.T) {
		h.Reset(t)
		client, seen := queryRecorder(h)
		if _, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1}); err != nil {
			t.Fatalf("Create(p) error = %v, want nil", err)
		}
		if got := seen(); len(got) != 0 {
			t.Errorf("Create fired %d query hooks, want none: %+v", len(got), got)
		}
	})

	t.Run("nil input returns ErrNilInput, sends no statement and fires no hook", func(t *testing.T) {
		h.Reset(t)
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Products().Create(ctx, nil)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("Create(nil) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("Create(nil) = %+v, want nil", got)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("Create(nil) sent %q, want nothing", stmts)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("Create(nil) fired %d mutation hooks, want none", n)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("a zero input writes a row of each required field's zero value", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Products().Create(ctx, &models.CreateProductInput{})
		if err != nil {
			t.Fatalf("Create(zero input) error = %v, want nil", err)
		}
		if got == nil || got.Name != "" || got.Price != 0 {
			t.Fatalf("Create(zero input) = %+v, want a row with an empty name and price 0", got)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("writes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		var id int64
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			p, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in tx", Price: 1})
			if err != nil {
				return err
			}
			id = p.ID
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Create inside the transaction sent %q outside it, want nothing", stmts)
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
		assertProductExists(t, h, id, true)
	})

	t.Run("rolled-back transaction leaves no row", func(t *testing.T) {
		h.Reset(t)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in tx", Price: 1}); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Products().Count(txCtx, nil)
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
		assertRowCount(t, h, "products", 0)
	})

	t.Run("fires one create mutation hook", func(t *testing.T) {
		h.Reset(t)
		client, seen := mutationRecorder(h)
		in := &models.CreateProductInput{Name: "p", Price: 1}
		got, err := client.Products().Create(ctx, in)
		if err != nil {
			t.Fatalf("Create(p) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpCreate || m.Table != models.TableProducts || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpCreate, models.TableProducts)
		}
		if gotIn, ok := m.Input.(*models.CreateProductInput); !ok || gotIn != in {
			t.Errorf("MutationContext.Input = %#v, want the caller's input pointer", m.Input)
		}
		if diff := cmp.Diff([]any{got.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("a hook that aborts writes nothing", func(t *testing.T) {
		h.Reset(t)
		errAbort := errors.New("abort")
		abort := func(hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) { return nil, errAbort }
		}
		client := models.New(h.Base, models.WithMutationHook(abort))
		if _, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1}); !errors.Is(err, errAbort) {
			t.Fatalf("Create under an aborting hook error = %v, want the hook's error", err)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("a duplicate unique value returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		crCreateCounter(t, h, "visits")
		_, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: "visits", UpdatedAt: omittable.Set(models.Now())})
		crAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("a key that differs only in case from a stored one returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		_, err := h.Client.Labels().Create(ctx, &models.CreateLabelInput{Name: "RED"})
		crAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "labels", 1)
	})

	t.Run("a reference to a missing row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		_, err := h.Client.OrderItems().Create(ctx, &models.CreateOrderItemInput{OrderID: 1, ProductID: p.ID + 1000, Quantity: 1})
		crAssertConstraint(t, err, models.ConstraintForeignKey)
		assertRowCount(t, h, "order_items", 0)
	})

	t.Run("a row a CHECK forbids returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Documents().Create(ctx, &models.CreateDocumentInput{
			Title:     "pinned",
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		})
		crAssertConstraint(t, err, models.ConstraintCheck)
		assertRowCount(t, h, "documents", 0)
	})

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: "visits"})
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 250}, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true}
		})
		if err != nil {
			t.Fatalf("Create(p, Name only) error = %v, want nil", err)
		}
		if diff := cmp.Diff(&models.Product{Name: "p"}, got); diff != "" {
			t.Errorf("Create(p, Name only) (-want +got):\n%s", diff)
		}
		ps := crProducts(t, h)
		if len(ps) != 1 || ps[0].Price != 250 {
			t.Errorf("stored products after Create(p, Name only) = %+v, want one with price 250", ps)
		}
	})

	t.Run("an empty FieldOptions writes and returns nil", func(t *testing.T) {
		h.Reset(t)
		h.Querier.ResetCount()
		got, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1}, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{}
		})
		if err != nil {
			t.Fatalf("Create(p, empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("Create(p, empty FieldOptions) = %+v, want nil", got)
		}
		if reads := crReadsOf(h, "products"); len(reads) != 0 {
			t.Errorf("Create(p, empty FieldOptions) read the table back: %q", reads)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Products().Create(ctx, &models.CreateProductInput{Name: "p", Price: 1}, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("Create(p, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.Name != "p" {
			t.Errorf("Create(p, LockForUpdate) = %+v, want the written row", got)
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("a composite key reads back only the written tuple", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1"), createProduct(t, h, "p2")
		// The existing tuples share each column value with the written one.
		createOrderItem(t, h, models.OrderItemPK{OrderID: 2, ProductID: p1.ID})
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p2.ID})
		got, err := h.Client.OrderItems().Create(ctx, &models.CreateOrderItemInput{OrderID: 2, ProductID: p2.ID, Quantity: 7})
		if err != nil {
			t.Fatalf("Create(2, p2) error = %v, want nil", err)
		}
		if diff := cmp.Diff(&models.OrderItem{OrderID: 2, ProductID: p2.ID, Quantity: 7}, got); diff != "" {
			t.Errorf("Create(2, p2) (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "order_items", 3)
	})

	t.Run("a composite key reads back the tuple it writes soft-deleted", func(t *testing.T) {
		h.Reset(t)
		createRevision(t, h, models.RevisionPK{DocumentID: 2, Number: 1})
		createRevision(t, h, models.RevisionPK{DocumentID: 1, Number: 2})
		got, err := h.Client.Revisions().Create(ctx, &models.CreateRevisionInput{
			DocumentID: 2, Number: 2, Body: "written",
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		})
		if err != nil {
			t.Fatalf("Create(2, 2, deleted) error = %v, want nil", err)
		}
		if got == nil || got.DocumentID != 2 || got.Number != 2 || got.Body != "written" || !crRevisionDeleted(got) {
			t.Errorf("Create(2, 2, deleted) = %+v, want revision (2, 2) with DeletedAt set", got)
		}
		assertRowCount(t, h, "revisions", 3)
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

	t.Run("writes every input across sub-batches and returns the rows it wrote", func(t *testing.T) {
		h.Reset(t)
		// An earlier row, so a read-back by the wrong keys would return it.
		earlier := createPricedProduct(t, h, "earlier", 100)
		inputs := crProductInputs("a", "b", "c", "d", "e")
		h.Querier.ResetCount()
		got, err := h.Client.Products().CreateMany(ctx, inputs)
		if err != nil {
			t.Fatalf("CreateMany(5 inputs) error = %v, want nil", err)
		}
		// The products table sets batch_size: 2, so 5 inputs are ceil(5 / 2) = 3 sub-batches.
		if inserts := statementsWithPrefix(h, "INSERT"); len(inserts) != 3 {
			t.Errorf("CreateMany(5 inputs) sent %d INSERT statements, want 3: %q", len(inserts), inserts)
		}
		stored := crProducts(t, h)
		stored = slices.DeleteFunc(stored, func(p *models.Product) bool { return p.ID == earlier.ID })
		if len(stored) != 5 {
			t.Fatalf("stored products besides the earlier one = %d, want 5", len(stored))
		}
		// Each returned entity is the stored row under its key, with the
		// columns that input wrote.
		if diff := cmp.Diff(stored, crSortProducts(got)); diff != "" {
			t.Errorf("CreateMany(5 inputs) against the stored rows (-stored +returned):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"a", "b", "c", "d", "e"}, crProductNames(got)); diff != "" {
			t.Errorf("CreateMany(5 inputs) returned names (-want +got):\n%s", diff)
		}
		for _, p := range got {
			if p.Price != crPriceOf(p.Name) || cmp.Equal(p.CreatedAt, models.Product{}.CreatedAt) {
				t.Errorf("CreateMany returned %+v, want price %d and the default created_at", p, crPriceOf(p.Name))
			}
		}
	})

	t.Run("the read-back fires no query hook", func(t *testing.T) {
		h.Reset(t)
		client, seen := queryRecorder(h)
		if _, err := client.Products().CreateMany(ctx, crProductInputs("a", "b", "c")); err != nil {
			t.Fatalf("CreateMany(3 inputs) error = %v, want nil", err)
		}
		if got := seen(); len(got) != 0 {
			t.Errorf("CreateMany fired %d query hooks, want none: %+v", len(got), got)
		}
	})

	t.Run("nil slice sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		// A row the call did not write, which an unguarded read-back would return.
		createProduct(t, h, "earlier")
		h.Querier.ResetCount()
		got, err := h.Client.Products().CreateMany(ctx, nil)
		if err != nil {
			t.Fatalf("CreateMany(nil) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("CreateMany(nil) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("CreateMany(nil) returned %d products, want none", len(got))
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("empty slice sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "earlier")
		h.Querier.ResetCount()
		got, err := h.Client.Products().CreateMany(ctx, []*models.CreateProductInput{})
		if err != nil {
			t.Fatalf("CreateMany([]) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("CreateMany([]) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("CreateMany([]) returned %d products, want none", len(got))
		}
		assertRowCount(t, h, "products", 1)
	})

	t.Run("a nil element returns ErrNilInput, sends no statement and fires no hook", func(t *testing.T) {
		h.Reset(t)
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		// The nil element is in the last sub-batch: the earlier ones are not written either.
		inputs := append(crProductInputs("a", "b"), nil)
		got, err := client.Products().CreateMany(ctx, inputs)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("CreateMany([a, b, nil]) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("CreateMany([a, b, nil]) = %+v, want nil", got)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("CreateMany([a, b, nil]) sent %q, want nothing", stmts)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("CreateMany([a, b, nil]) fired %d mutation hooks, want none", n)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("writes inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		var got []*models.Product
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			var err error
			if got, err = h.Client.Products().CreateMany(txCtx, crProductInputs("a", "b", "c")); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("CreateMany inside the transaction sent %q outside it, want nothing", stmts)
			}
			n, err := h.Client.Products().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 3 {
				t.Errorf("Count inside the transaction = %d, want 3", n)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if diff := cmp.Diff(crProducts(t, h), crSortProducts(got)); diff != "" {
			t.Errorf("CreateMany inside the transaction against the committed rows (-stored +returned):\n%s", diff)
		}
	})

	t.Run("rolled-back transaction leaves no row from any sub-batch", func(t *testing.T) {
		h.Reset(t)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Products().CreateMany(txCtx, crProductInputs("a", "b", "c", "d", "e")); err != nil {
				return err
			}
			// The write is visible inside the transaction, so the rollback is what undoes it.
			n, err := h.Client.Products().Count(txCtx, nil)
			if err != nil {
				return err
			}
			if n != 5 {
				t.Errorf("Count inside the transaction = %d, want 5", n)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("inside a transaction a failed sub-batch rolls back the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		// A row written before the transaction, which the rollback must not touch.
		before := createDocument(t, h, "before")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			_, err := h.Client.Documents().CreateMany(txCtx, crDocumentInputsFailingThird())
			return err
		})
		if _, ok := errors.AsType[*models.ConstraintError](err); !ok {
			t.Fatalf("WithTransaction error = %v, want the CreateMany *ConstraintError", err)
		}
		if diff := cmp.Diff([]string{before.Title}, crDocumentTitles(t, h)); diff != "" {
			t.Errorf("documents after the rolled-back CreateMany (-want +got):\n%s", diff)
		}
	})

	t.Run("a failed sub-batch keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Documents().CreateMany(ctx, crDocumentInputsFailingThird())
		if _, ok := errors.AsType[*models.ConstraintError](err); !ok {
			t.Fatalf("CreateMany error = %v, want a *ConstraintError", err)
		}
		if got != nil {
			t.Errorf("CreateMany = %+v, want nil with the error", got)
		}
		// The first sub-batch stays; the failed one and the one after it are not written.
		if diff := cmp.Diff([]string{"a", "b"}, crDocumentTitles(t, h)); diff != "" {
			t.Errorf("documents after the failed CreateMany (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one create_many mutation hook with keys in input order", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "earlier")
		client, seen := mutationRecorder(h)
		inputs := crProductInputs("c", "a", "b")
		got, err := client.Products().CreateMany(ctx, inputs)
		if err != nil {
			t.Fatalf("CreateMany([c, a, b]) error = %v, want nil", err)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpCreateMany || m.Table != models.TableProducts || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpCreateMany, models.TableProducts)
		}
		if gotIn, ok := m.Input.([]*models.CreateProductInput); !ok || len(gotIn) != len(inputs) || &gotIn[0] != &inputs[0] {
			t.Errorf("MutationContext.Input = %#v, want the caller's input slice", m.Input)
		}
		// The i-th affected key is the row the i-th input wrote.
		if len(m.AffectedPKs) != len(inputs) {
			t.Fatalf("AffectedPKs = %v, want %d keys", m.AffectedPKs, len(inputs))
		}
		var keys []int64
		for i, pk := range m.AffectedPKs {
			id, ok := pk.(int64)
			if !ok {
				t.Fatalf("AffectedPKs[%d] = %#v, want an int64 key", i, pk)
			}
			keys = append(keys, id)
			p, err := h.Client.Products().Get(ctx, id)
			if err != nil {
				t.Fatalf("Get(AffectedPKs[%d] = %d) error = %v", i, id, err)
			}
			if p.Name != inputs[i].Name {
				t.Errorf("AffectedPKs[%d] names %q, want the row input %d wrote, %q", i, p.Name, i, inputs[i].Name)
			}
		}
		slices.Sort(keys)
		if diff := cmp.Diff(keys, crProductIDs(got)); diff != "" {
			t.Errorf("returned keys against AffectedPKs (-affected +returned):\n%s", diff)
		}
	})

	t.Run("a hook that aborts writes nothing", func(t *testing.T) {
		h.Reset(t)
		errAbort := errors.New("abort")
		abort := func(hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) { return nil, errAbort }
		}
		client := models.New(h.Base, models.WithMutationHook(abort))
		if _, err := client.Products().CreateMany(ctx, crProductInputs("a", "b", "c")); !errors.Is(err, errAbort) {
			t.Fatalf("CreateMany under an aborting hook error = %v, want the hook's error", err)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("a duplicate unique value returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		crCreateCounter(t, h, "visits")
		_, err := h.Client.Counters().CreateMany(ctx, []*models.CreateCounterInput{
			{Name: "other", UpdatedAt: omittable.Set(models.Now())},
			{Name: "visits", UpdatedAt: omittable.Set(models.Now())},
		})
		crAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("a key that differs only in case from a stored one returns a unique ConstraintError", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		_, err := h.Client.Labels().CreateMany(ctx, []*models.CreateLabelInput{{Name: "blue"}, {Name: "RED"}})
		crAssertConstraint(t, err, models.ConstraintUnique)
		assertRowCount(t, h, "labels", 1)
	})

	t.Run("a reference to a missing row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		_, err := h.Client.OrderItems().CreateMany(ctx, []*models.CreateOrderItemInput{
			{OrderID: 1, ProductID: p.ID, Quantity: 1},
			{OrderID: 2, ProductID: p.ID + 1000, Quantity: 1},
		})
		crAssertConstraint(t, err, models.ConstraintForeignKey)
		assertRowCount(t, h, "order_items", 0)
	})

	t.Run("a row a CHECK forbids returns a check ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Documents().CreateMany(ctx, []*models.CreateDocumentInput{
			{Title: "live"},
			{Title: "pinned", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		})
		crAssertConstraint(t, err, models.ConstraintCheck)
		assertRowCount(t, h, "documents", 0)
	})

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().CreateMany(ctx, []*models.CreateCounterInput{
			{Name: "set", UpdatedAt: omittable.Set(models.Now())},
			{Name: "unset"},
		})
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Products().CreateMany(ctx, crProductInputs("a", "b", "c"), func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true}
		})
		if err != nil {
			t.Fatalf("CreateMany(3 inputs, Name only) error = %v, want nil", err)
		}
		slices.SortFunc(got, func(x, y *models.Product) int { return strings.Compare(x.Name, y.Name) })
		want := []*models.Product{{Name: "a"}, {Name: "b"}, {Name: "c"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("CreateMany(3 inputs, Name only) (-want +got):\n%s", diff)
		}
		for _, p := range crProducts(t, h) {
			if p.Price != crPriceOf(p.Name) {
				t.Errorf("stored product %q has price %d, want %d", p.Name, p.Price, crPriceOf(p.Name))
			}
		}
		assertRowCount(t, h, "products", 3)
	})

	t.Run("an empty FieldOptions writes and returns nil", func(t *testing.T) {
		h.Reset(t)
		h.Querier.ResetCount()
		got, err := h.Client.Products().CreateMany(ctx, crProductInputs("a", "b", "c"), func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{}
		})
		if err != nil {
			t.Fatalf("CreateMany(3 inputs, empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("CreateMany(3 inputs, empty FieldOptions) = %+v, want nil", got)
		}
		if reads := crReadsOf(h, "products"); len(reads) != 0 {
			t.Errorf("CreateMany(3 inputs, empty FieldOptions) read the table back: %q", reads)
		}
		assertRowCount(t, h, "products", 3)
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Products().CreateMany(ctx, crProductInputs("a", "b", "c"), func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			t.Fatalf("CreateMany(3 inputs, LockForUpdate) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"a", "b", "c"}, crProductNames(got)); diff != "" {
			t.Errorf("CreateMany(3 inputs, LockForUpdate) returned names (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "products", 3)
	})

	t.Run("composite keys read back only the written tuples", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1"), createProduct(t, h, "p2")
		// The existing tuples cross the written ones: each column value is
		// written, only the pairing is not.
		for _, pk := range []models.OrderItemPK{{OrderID: 1, ProductID: p2.ID}, {OrderID: 2, ProductID: p1.ID}} {
			createOrderItem(t, h, pk)
		}
		got, err := h.Client.OrderItems().CreateMany(ctx, []*models.CreateOrderItemInput{
			{OrderID: 1, ProductID: p1.ID, Quantity: 5},
			{OrderID: 2, ProductID: p2.ID, Quantity: 6},
		})
		if err != nil {
			t.Fatalf("CreateMany([(1, p1), (2, p2)]) error = %v, want nil", err)
		}
		slices.SortFunc(got, func(x, y *models.OrderItem) int { return int(x.OrderID - y.OrderID) })
		want := []*models.OrderItem{
			{OrderID: 1, ProductID: p1.ID, Quantity: 5},
			{OrderID: 2, ProductID: p2.ID, Quantity: 6},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("CreateMany([(1, p1), (2, p2)]) (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "order_items", 4)
	})

	t.Run("composite keys read back the written tuples, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		// The existing tuples cross the written ones; one is soft-deleted, so a
		// read-back that keeps deleted rows would return it under a per-column match.
		createRevision(t, h, models.RevisionPK{DocumentID: 1, Number: 2})
		createRevision(t, h, models.RevisionPK{DocumentID: 2, Number: 1})
		if _, err := h.Client.Revisions().SoftDelete(ctx, models.RevisionPK{DocumentID: 2, Number: 1}); err != nil {
			t.Fatalf("SoftDelete(2, 1) setup error = %v", err)
		}
		got, err := h.Client.Revisions().CreateMany(ctx, []*models.CreateRevisionInput{
			{DocumentID: 1, Number: 1, Body: "live"},
			{DocumentID: 2, Number: 2, Body: "deleted", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		})
		if err != nil {
			t.Fatalf("CreateMany([(1, 1), (2, 2) deleted]) error = %v, want nil", err)
		}
		want := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 2, Number: 2}}
		if diff := cmp.Diff(want, sdmRevisionPKs(got)); diff != "" {
			t.Fatalf("CreateMany([(1, 1), (2, 2) deleted]) returned keys (-want +got):\n%s", diff)
		}
		for _, r := range got {
			wantBody, wantDeleted := "live", false
			if r.Number == 2 {
				wantBody, wantDeleted = "deleted", true
			}
			if r.Body != wantBody || crRevisionDeleted(r) != wantDeleted {
				t.Errorf("CreateMany returned %+v, want body %q and deleted %v", r, wantBody, wantDeleted)
			}
		}
		assertRowCount(t, h, "revisions", 4)
	})

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

// crPriceOf is the price crProductInputs gives the product named name.
func crPriceOf(name string) int64 {
	return int64(len(name))*1000 + int64(name[0])
}

// crProductInputs returns one product input per name, each with a price of its own.
func crProductInputs(names ...string) []*models.CreateProductInput {
	inputs := make([]*models.CreateProductInput, len(names))
	for i, n := range names {
		inputs[i] = &models.CreateProductInput{Name: n, Price: crPriceOf(n)}
	}
	return inputs
}

// crDocumentInputsFailingThird returns five document inputs whose second
// sub-batch of two violates the documents CHECK: with batch_size 2 the
// sub-batches are {a, b}, {c, pinned} and {e}.
func crDocumentInputsFailingThird() []*models.CreateDocumentInput {
	return []*models.CreateDocumentInput{
		{Title: "a"},
		{Title: "b"},
		{Title: "c"},
		{Title: "pinned", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		{Title: "e"},
	}
}

// crDocumentTitles returns the titles of every document, soft-deleted ones included, sorted.
func crDocumentTitles(t *testing.T, h *Harness) []string {
	t.Helper()
	docs, err := h.Client.Documents().GetMany(context.Background(), &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{DeletedAt: &comparator.NullableTime{}},
	})
	if err != nil {
		t.Fatalf("Documents().GetMany(any DeletedAt) error = %v", err)
	}
	titles := make([]string, 0, len(docs))
	for _, d := range docs {
		titles = append(titles, d.Title)
	}
	slices.Sort(titles)
	return titles
}

// crProducts returns every stored product, sorted by key.
func crProducts(t *testing.T, h *Harness) []*models.Product {
	t.Helper()
	ps, err := h.Client.Products().GetMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("Products().GetMany(nil) error = %v", err)
	}
	return crSortProducts(ps)
}

// crSortProducts sorts ps by key in place and returns it.
func crSortProducts(ps []*models.Product) []*models.Product {
	slices.SortFunc(ps, func(x, y *models.Product) int {
		if x.ID < y.ID {
			return -1
		}
		if x.ID > y.ID {
			return 1
		}
		return 0
	})
	return ps
}

// crProductIDs returns the keys of ps, sorted.
func crProductIDs(ps []*models.Product) []int64 {
	ids := make([]int64, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	slices.Sort(ids)
	return ids
}

// crProductNames returns the names of ps, sorted.
func crProductNames(ps []*models.Product) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	slices.Sort(names)
	return names
}

// crRevisionDeleted reports whether r's soft-delete marker is set.
func crRevisionDeleted(r *models.Revision) bool {
	return !cmp.Equal(r.DeletedAt, models.Revision{}.DeletedAt)
}

// crCreateCounter creates a counter named name.
func crCreateCounter(t *testing.T, h *Harness, name string) {
	t.Helper()
	in := &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())}
	if _, err := h.Client.Counters().Create(context.Background(), in); err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
}

// crReadsOf returns the recorded SELECT statements that read table.
func crReadsOf(h *Harness, table string) []string {
	var out []string
	for _, s := range statementsWithPrefix(h, "SELECT") {
		if strings.Contains(s, table) {
			out = append(out, s)
		}
	}
	return out
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

// crAssertConstraint checks that err is a ConstraintError of type want that
// unwraps to ErrConstraintViolation.
func crAssertConstraint(t *testing.T, err error, want models.ConstraintType) {
	t.Helper()
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %v, want a *models.ConstraintError", err)
	}
	if ce.Type != want {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, want)
	}
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Errorf("errors.Is(err, ErrConstraintViolation) = false for %v", err)
	}
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
