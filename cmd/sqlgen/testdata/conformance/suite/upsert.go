package suite

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// upsert is the Upsert contract.
func upsert(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the row it inserts when the input soft-deletes it", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			UpdatedAt: omittable.Set(models.Now()),
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(new c, DeletedAt set) error = %v, want nil", err)
		}
		if got == nil || got.Name != "c" || !usCounterDeleted(got) {
			t.Fatalf("Upsert(new c, DeletedAt set) = %+v, want c with DeletedAt set", got)
		}
		if diff := cmp.Diff([]string{"c"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after Upsert (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("returns a soft-deleted row its conflict updates", func(t *testing.T) {
		h.Reset(t)
		usSeedDeletedCounter(t, h, "c")
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			Hits:      omittable.Set(int64(3)),
			UpdatedAt: omittable.Set(models.Now()),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(soft-deleted c, Hits 3) error = %v, want nil", err)
		}
		if got == nil || got.Name != "c" || got.Hits != 3 || !usCounterDeleted(got) {
			t.Fatalf("Upsert(soft-deleted c, Hits 3) = %+v, want c with Hits 3 and DeletedAt set", got)
		}
		if diff := cmp.Diff([]string{"c"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after Upsert (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("inserts a row when nothing conflicts", func(t *testing.T) {
		h.Reset(t)
		other := usCreateCounter(t, h, "other", 1)
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 4), models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(new c) error = %v, want nil", err)
		}
		if got == nil || got.Name != "c" || got.Hits != 4 || got.ID == other.ID || usCounterDeleted(got) {
			t.Fatalf("Upsert(new c) = %+v, want a new live row c with Hits 4", got)
		}
		want := map[string]usCounterRow{"other": {ID: other.ID, Hits: 1}, "c": {ID: got.ID, Hits: 4}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert(new c) (-want +got):\n%s", diff)
		}
	})

	t.Run("a conflict updates the existing row in place and keeps its key", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		other := usCreateCounter(t, h, "other", 1)
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 5), models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(existing c, Hits 5) error = %v, want nil", err)
		}
		if got == nil || got.ID != c.ID || got.Name != "c" || got.Hits != 5 {
			t.Fatalf("Upsert(existing c, Hits 5) = %+v, want c's row (ID %d) with Hits 5", got, c.ID)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 5}, "other": {ID: other.ID, Hits: 1}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert(existing c) (-want +got):\n%s", diff)
		}
	})

	t.Run("a conflict leaves a column the input does not set unchanged", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 7)
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			UpdatedAt: omittable.Set(models.Now()),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(existing c, Hits unset) error = %v, want nil", err)
		}
		if got == nil || got.ID != c.ID || got.Hits != 7 {
			t.Fatalf("Upsert(existing c, Hits unset) = %+v, want c's row with Hits still 7", got)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 7}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert(existing c, Hits unset) (-want +got):\n%s", diff)
		}
	})

	t.Run("a conflict that changes nothing returns the existing row", func(t *testing.T) {
		h.Reset(t)
		at := models.Now()
		c, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: "c", Hits: omittable.Set(int64(2)), UpdatedAt: omittable.Set(at)})
		if err != nil {
			t.Fatalf("Create counter c: %v", err)
		}
		usCreateCounter(t, h, "other", 1)
		// Every column the input sets already holds that value.
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			Hits:      omittable.Set(int64(2)),
			UpdatedAt: omittable.Set(at),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(c unchanged) error = %v, want nil", err)
		}
		if got == nil || got.ID != c.ID || got.Hits != 2 {
			t.Fatalf("Upsert(c unchanged) = %+v, want c's row (ID %d)", got, c.ID)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("a conflict on a target that covers every column it writes leaves the row as it is and returns it", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		createLabel(t, h, "blue")
		if _, err := h.Client.Labels().SoftDelete(ctx, "red"); err != nil {
			t.Fatalf("SoftDelete label red: %v", err)
		}
		// The input sets only the key, so there is no column to update.
		got, err := h.Client.Labels().Upsert(ctx, &models.CreateLabelInput{Name: "red"}, models.LabelConflictPK)
		if err != nil {
			t.Fatalf("Upsert(red, key only) error = %v, want nil", err)
		}
		if got == nil || got.Name != "red" || cmp.Equal(got.DeletedAt, models.Label{}.DeletedAt) {
			t.Fatalf("Upsert(red, key only) = %+v, want the soft-deleted red row", got)
		}
		if n := usLiveLabelCount(t, h); n != 1 {
			t.Errorf("live labels after Upsert(red, key only) = %d, want 1 (red still deleted)", n)
		}
		assertRowCount(t, h, "labels", 2)
	})

	t.Run("a primary-key target on a database-generated key is a plain insert", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("d", 2), models.CounterConflictPK)
		if err != nil {
			t.Fatalf("Upsert(d, ConflictPK) error = %v, want nil", err)
		}
		if got == nil || got.Name != "d" || got.Hits != 2 || got.ID == c.ID {
			t.Fatalf("Upsert(d, ConflictPK) = %+v, want a new row d", got)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 1}, "d": {ID: got.ID, Hits: 2}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert(d, ConflictPK) (-want +got):\n%s", diff)
		}
	})

	t.Run("nil input returns ErrNilInput, fires no hook and writes nothing", func(t *testing.T) {
		h.Reset(t)
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Counters().Upsert(ctx, nil, models.CounterConflictName)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("Upsert(nil) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("Upsert(nil) = %+v, want nil", got)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("mutation hook fired %d times for Upsert(nil), want 0", n)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("Upsert(nil) sent %q, want nothing", stmts)
		}
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("upserts inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Counters().Upsert(txCtx, usCounterInput("c", 5), models.CounterConflictName); err != nil {
				return err
			}
			if _, err := h.Client.Counters().Upsert(txCtx, usCounterInput("d", 2), models.CounterConflictName); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Upsert inside the transaction sent %q outside it, want nothing", stmts)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		rows := usCounterRows(t, h)
		if got := rows["c"]; got != (usCounterRow{ID: c.ID, Hits: 5}) {
			t.Errorf("counter c after the commit = %+v, want ID %d with Hits 5", got, c.ID)
		}
		if got, ok := rows["d"]; !ok || got.Hits != 2 {
			t.Errorf("counter d after the commit = %+v (present %v), want Hits 2", got, ok)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("rolled-back transaction undoes both the update and the insert", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Counters().Upsert(txCtx, usCounterInput("c", 5), models.CounterConflictName); err != nil {
				return err
			}
			if _, err := h.Client.Counters().Upsert(txCtx, usCounterInput("d", 2), models.CounterConflictName); err != nil {
				return err
			}
			// The writes are visible inside the transaction, so the rollback is what undoes them.
			rows, err := usCounterRowsIn(txCtx, h)
			if err != nil {
				return err
			}
			if rows["c"].Hits != 5 || rows["d"].Hits != 2 || len(rows) != 2 {
				t.Errorf("counters inside the transaction = %+v, want c with Hits 5 and d with Hits 2", rows)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 1}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one upsert mutation hook per call, with the written row's key affected on insert and on conflict", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		usCreateCounter(t, h, "other", 1)
		client, seen := mutationRecorder(h)
		update := usCounterInput("c", 5)
		if _, err := client.Counters().Upsert(ctx, update, models.CounterConflictName); err != nil {
			t.Fatalf("Upsert(existing c) error = %v, want nil", err)
		}
		insert := usCounterInput("d", 2)
		d, err := client.Counters().Upsert(ctx, insert, models.CounterConflictName)
		if err != nil || d == nil {
			t.Fatalf("Upsert(new d) = %+v, %v, want the row", d, err)
		}
		if len(seen()) != 2 {
			t.Fatalf("mutation hook fired %d times, want 2", len(seen()))
		}
		for i, tt := range []struct {
			input *models.CreateCounterInput
			pk    int64
		}{{update, c.ID}, {insert, d.ID}} {
			m := seen()[i]
			if m.Op != hook.OpUpsert || m.Table != models.TableCounters || m.PK != nil {
				t.Errorf("call %d: MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
					i, m.Op, m.Table, m.PK, hook.OpUpsert, models.TableCounters)
			}
			if in, ok := m.Input.(*models.CreateCounterInput); !ok || in != tt.input {
				t.Errorf("call %d: MutationContext.Input = %#v, want the caller's input", i, m.Input)
			}
			if diff := cmp.Diff([]any{tt.pk}, m.AffectedPKs); diff != "" {
				t.Errorf("call %d: AffectedPKs after the mutation (-want +got):\n%s", i, diff)
			}
		}
	})

	t.Run("referencing a missing row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		_, err := h.Client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{OrderID: 1, ProductID: p.ID + 1000, Quantity: 1}, models.OrderItemConflictPK)
		usAssertConstraint(t, err, models.ConstraintForeignKey)
		assertRowCount(t, h, "order_items", 0)
	})

	t.Run("a conflict that breaks a CHECK returns a check ConstraintError and leaves the row unchanged", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 3)
		_, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", -1), models.CounterConflictName)
		usAssertConstraint(t, err, models.ConstraintCheck)
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 3}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the failed Upsert (-want +got):\n%s", diff)
		}
	})

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{Name: "c"}, models.CounterConflictName)
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("a primary-key target colliding on another unique column returns a unique ConstraintError, not an update", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		// The input cannot write the database-generated key, so the target
		// names no column the INSERT writes and nothing may take the update branch.
		_, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 9), models.CounterConflictPK)
		usAssertConstraint(t, err, models.ConstraintUnique)
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 1}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the failed Upsert (-want +got):\n%s", diff)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 5), models.CounterConflictName, func(o *models.CallOptions[models.CounterFieldOptions]) {
			o.FieldOptions = &models.CounterFieldOptions{ID: true, Hits: true}
		})
		if err != nil {
			t.Fatalf("Upsert(existing c, ID and Hits only) error = %v, want nil", err)
		}
		if diff := cmp.Diff(&models.Counter{ID: c.ID, Hits: 5}, got); diff != "" {
			t.Errorf("Upsert(existing c, ID and Hits only) (-want +got):\n%s", diff)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 5}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty FieldOptions upserts and returns nil", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		h.Querier.ResetCount()
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 5), models.CounterConflictName, func(o *models.CallOptions[models.CounterFieldOptions]) {
			o.FieldOptions = &models.CounterFieldOptions{}
		})
		if err != nil {
			t.Fatalf("Upsert(existing c, empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("Upsert(existing c, empty FieldOptions) = %+v, want nil", got)
		}
		// The write is the call's last statement: no follow-up read.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "INSERT") {
			t.Errorf("Upsert(existing c, empty FieldOptions) sent %q, want the INSERT last", stmts)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 5}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after Upsert (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		got, err := h.Client.Counters().Upsert(ctx, usCounterInput("c", 5), models.CounterConflictName, withLock[models.CounterFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("Upsert(existing c, LockForUpdate) error = %v, want nil", err)
		}
		if got == nil || got.ID != c.ID || got.Hits != 5 {
			t.Errorf("Upsert(existing c, LockForUpdate) = %+v, want c's row with Hits 5", got)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("composite key updates the conflicting tuple, inserts a new one and leaves the crossing tuples alone", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		conflict := models.OrderItemPK{OrderID: 10, ProductID: p.ID}
		inserted := models.OrderItemPK{OrderID: 20, ProductID: q.ID}
		// These cross the written tuples: each column value is written, only the pairing is not.
		crossing := []models.OrderItemPK{{OrderID: 10, ProductID: q.ID}, {OrderID: 20, ProductID: p.ID}}
		for _, pk := range append([]models.OrderItemPK{conflict}, crossing...) {
			createOrderItem(t, h, pk)
		}
		got, err := h.Client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{OrderID: conflict.OrderID, ProductID: conflict.ProductID, Quantity: 5}, models.OrderItemConflictPK)
		if err != nil {
			t.Fatalf("Upsert(%+v, Quantity 5) error = %v, want nil", conflict, err)
		}
		if diff := cmp.Diff(&models.OrderItem{OrderID: 10, ProductID: p.ID, Quantity: 5}, got); diff != "" {
			t.Errorf("Upsert(%+v, Quantity 5) (-want +got):\n%s", conflict, diff)
		}
		got, err = h.Client.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{OrderID: inserted.OrderID, ProductID: inserted.ProductID, Quantity: 3}, models.OrderItemConflictPK)
		if err != nil {
			t.Fatalf("Upsert(%+v, Quantity 3) error = %v, want nil", inserted, err)
		}
		if diff := cmp.Diff(&models.OrderItem{OrderID: 20, ProductID: q.ID, Quantity: 3}, got); diff != "" {
			t.Errorf("Upsert(%+v, Quantity 3) (-want +got):\n%s", inserted, diff)
		}
		want := map[models.OrderItemPK]int64{conflict: 5, inserted: 3, crossing[0]: 1, crossing[1]: 1}
		if diff := cmp.Diff(want, usOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("order items after the upserts (-want +got):\n%s", diff)
		}
	})

	t.Run("composite key returns a soft-deleted tuple its conflict updates", func(t *testing.T) {
		h.Reset(t)
		deleted := models.RevisionPK{DocumentID: 1, Number: 2}
		inserted := models.RevisionPK{DocumentID: 3, Number: 1}
		// Each of these shares one column value with a written tuple, and is
		// created first, so a read-back keyed on one column finds it first;
		// (2, 1) is also the mirror of the soft-deleted tuple.
		others := []models.RevisionPK{
			{DocumentID: 1, Number: 1},
			{DocumentID: 2, Number: 2},
			{DocumentID: 3, Number: 2},
			{DocumentID: 2, Number: 1},
		}
		for _, pk := range append(others, deleted) {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDelete(ctx, deleted); err != nil {
			t.Fatalf("SoftDelete revision %+v: %v", deleted, err)
		}
		got, err := h.Client.Revisions().Upsert(ctx, &models.CreateRevisionInput{DocumentID: 1, Number: 2, Body: "new"}, models.RevisionConflictPK)
		if err != nil {
			t.Fatalf("Upsert(soft-deleted %+v) error = %v, want nil", deleted, err)
		}
		if got == nil || got.DocumentID != 1 || got.Number != 2 || got.Body != "new" || !usRevisionDeleted(got) {
			t.Fatalf("Upsert(soft-deleted %+v) = %+v, want the tuple with Body new and DeletedAt set", deleted, got)
		}
		got, err = h.Client.Revisions().Upsert(ctx, &models.CreateRevisionInput{DocumentID: 3, Number: 1, Body: "inserted"}, models.RevisionConflictPK)
		if err != nil {
			t.Fatalf("Upsert(new %+v) error = %v, want nil", inserted, err)
		}
		if got == nil || got.DocumentID != 3 || got.Number != 1 || got.Body != "inserted" || usRevisionDeleted(got) {
			t.Fatalf("Upsert(new %+v) = %+v, want the new live tuple with Body inserted", inserted, got)
		}
		want := map[models.RevisionPK]string{deleted: "new", inserted: "inserted"}
		for _, pk := range others {
			want[pk] = "body"
		}
		if diff := cmp.Diff(want, usRevisionBodies(t, h)); diff != "" {
			t.Errorf("revision bodies after the upserts (-want +got):\n%s", diff)
		}
	})
}

// upsertMany is the UpsertMany contract.
func upsertMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("keys the database matches as one fail the call rather than return short", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		client, affected := affectedPKsRecorder(h)
		got, err := client.Labels().UpsertMany(ctx, []*models.CreateLabelInput{{Name: "RED"}, {Name: "Red"}, {Name: "blue"}}, models.LabelConflictPK)
		if err == nil {
			t.Errorf("UpsertMany([RED, Red, blue]) over stored red = %d entities and no error (AffectedPKs %v), want an error: the database matches RED and Red as one key", len(got), affected())
		}
	})

	t.Run("conflict values the database matches as one return one entity per deduped input or fail loudly", func(t *testing.T) {
		h.Reset(t)
		now := models.Now()
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "visits", UpdatedAt: omittable.Set(now)},
			{Name: "VISITS", UpdatedAt: omittable.Set(now)},
		}, models.CounterConflictName)
		if err == nil && len(got) != 2 {
			t.Errorf("UpsertMany([visits, VISITS]) by name = %d entities and no error, want 2 entities or an error", len(got))
		}
	})

	t.Run("returns every row it writes, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "live", UpdatedAt: omittable.Set(models.Now())},
			{Name: "deleted", UpdatedAt: omittable.Set(models.Now()), DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([live, deleted]) error = %v, want nil", err)
		}
		names := make([]string, 0, len(got))
		for _, c := range got {
			names = append(names, c.Name)
		}
		slices.Sort(names)
		if diff := cmp.Diff([]string{"deleted", "live"}, names); diff != "" {
			t.Errorf("UpsertMany([live, deleted]) returned names (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"deleted"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after UpsertMany (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("inserts the rows that do not conflict and updates the ones that do, keeping their keys", func(t *testing.T) {
		h.Reset(t)
		a := usCreateCounter(t, h, "a", 1)
		b := usCreateCounter(t, h, "b", 1)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			usCounterInput("a", 5),
			usCounterInput("c", 3),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([a, c]) error = %v, want nil", err)
		}
		rows := usCounterRows(t, h)
		c, ok := rows["c"]
		if !ok {
			t.Fatalf("counters after UpsertMany([a, c]) = %+v, want c inserted", rows)
		}
		want := map[string]usCounterRow{"a": {ID: a.ID, Hits: 5}, "b": {ID: b.ID, Hits: 1}, "c": {ID: c.ID, Hits: 3}}
		if diff := cmp.Diff(want, rows); diff != "" {
			t.Errorf("counters after UpsertMany([a, c]) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(map[string]usCounterRow{"a": want["a"], "c": want["c"]}, usCounterRowsOf(got)); diff != "" {
			t.Errorf("UpsertMany([a, c]) returned (-want +got):\n%s", diff)
		}
	})

	t.Run("duplicate inputs are deduped and the last occurrence wins", func(t *testing.T) {
		h.Reset(t)
		a := usCreateCounter(t, h, "a", 1)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			usCounterInput("a", 2),
			usCounterInput("b", 3),
			usCounterInput("a", 9),
			usCounterInput("b", 4),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([a, b, a, b]) error = %v, want nil", err)
		}
		rows := usCounterRows(t, h)
		want := map[string]usCounterRow{"a": {ID: a.ID, Hits: 9}, "b": {ID: rows["b"].ID, Hits: 4}}
		if diff := cmp.Diff(want, rows); diff != "" {
			t.Errorf("counters after UpsertMany([a, b, a, b]) (-want +got):\n%s", diff)
		}
		// One entity per deduped input.
		if diff := cmp.Diff(want, usCounterRowsOf(got)); diff != "" || len(got) != 2 {
			t.Errorf("UpsertMany([a, b, a, b]) returned %d entities, want 2 (-want +got):\n%s", len(got), diff)
		}
	})

	t.Run("dedupes across the whole call before splitting it into sub-batches", func(t *testing.T) {
		h.Reset(t)
		pks := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 1, Number: 2}, {DocumentID: 2, Number: 1}, {DocumentID: 2, Number: 2}}
		in := func(pk models.RevisionPK, body string) *models.CreateRevisionInput {
			return &models.CreateRevisionInput{DocumentID: pk.DocumentID, Number: pk.Number, Body: body}
		}
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		// Five inputs, four distinct keys: with batch_size: 2 the deduped call is
		// ceil(4 / 2) = 2 statements, where deduping each sub-batch would be 3.
		got, err := client.Revisions().UpsertMany(ctx, []*models.CreateRevisionInput{
			in(pks[0], "first"), in(pks[1], "b"), in(pks[2], "c"), in(pks[3], "d"), in(pks[0], "last"),
		}, models.RevisionConflictPK)
		if err != nil {
			t.Fatalf("UpsertMany(5 inputs, 4 keys) error = %v, want nil", err)
		}
		if inserts := statementsWithPrefix(h, "INSERT"); len(inserts) != 2 {
			t.Errorf("UpsertMany(5 inputs, 4 keys) sent %d INSERT statements, want 2: %q", len(inserts), inserts)
		}
		want := map[models.RevisionPK]string{pks[0]: "last", pks[1]: "b", pks[2]: "c", pks[3]: "d"}
		if diff := cmp.Diff(want, usRevisionBodies(t, h)); diff != "" {
			t.Errorf("revision bodies after UpsertMany (-want +got):\n%s", diff)
		}
		if len(got) != 4 {
			t.Errorf("UpsertMany(5 inputs, 4 keys) returned %d entities, want 4", len(got))
		}
		// The keys follow first appearance; the first key carries the last occurrence's input.
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		if diff := cmp.Diff([]any{pks[0], pks[1], pks[2], pks[3]}, seen()[0].AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("a target covering a caller-known key sends one statement per sub-batch and no key read-back", func(t *testing.T) {
		h.Reset(t)
		createRevision(t, h, models.RevisionPK{DocumentID: 1, Number: 1})
		h.Querier.ResetCount()
		// With batch_size: 2, three inputs are two sub-batches; the reads are the
		// terminal re-fetch, chunked at the same batch size.
		got, err := h.Client.Revisions().UpsertMany(ctx, []*models.CreateRevisionInput{
			{DocumentID: 1, Number: 1, Body: "updated"},
			{DocumentID: 1, Number: 2, Body: "b"},
			{DocumentID: 2, Number: 1, Body: "c"},
		}, models.RevisionConflictPK)
		if err != nil {
			t.Fatalf("UpsertMany(3 revisions) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"INSERT", "INSERT", "SELECT", "SELECT"}, usStatementVerbs(h)); diff != "" {
			t.Errorf("UpsertMany(3 revisions) statements (-want +got):\n%s", diff)
		}
		if len(got) != 3 {
			t.Errorf("UpsertMany(3 revisions) returned %d entities, want 3", len(got))
		}
	})

	t.Run("a database-generated key is read back by the conflict target after each sub-batch", func(t *testing.T) {
		h.Reset(t)
		usCreateCounter(t, h, "c000", 1)
		inputs := make([]*models.CreateCounterInput, 0, 201)
		for i := range 201 {
			inputs = append(inputs, usCounterInput(fmt.Sprintf("c%03d", i), 2))
		}
		h.Querier.ResetCount()
		// counters keeps the default batch size of 200: two sub-batches, each
		// followed by its key read-back, then the terminal re-fetch in two
		// chunks of at most 200 keys.
		got, err := h.Client.Counters().UpsertMany(ctx, inputs, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany(201 counters) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"INSERT", "SELECT", "INSERT", "SELECT", "SELECT", "SELECT"}, usStatementVerbs(h)); diff != "" {
			t.Errorf("UpsertMany(201 counters) statements (-want +got):\n%s", diff)
		}
		if len(got) != 201 {
			t.Errorf("UpsertMany(201 counters) returned %d entities, want 201", len(got))
		}
		assertRowCount(t, h, "counters", 201)
	})

	t.Run("re-upserting identical rows returns one entity per input", func(t *testing.T) {
		h.Reset(t)
		at := models.Now()
		inputs := func() []*models.CreateCounterInput {
			return []*models.CreateCounterInput{
				{Name: "a", Hits: omittable.Set(int64(1)), UpdatedAt: omittable.Set(at)},
				{Name: "b", Hits: omittable.Set(int64(2)), UpdatedAt: omittable.Set(at)},
			}
		}
		first, err := h.Client.Counters().UpsertMany(ctx, inputs(), models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([a, b]) error = %v, want nil", err)
		}
		again, err := h.Client.Counters().UpsertMany(ctx, inputs(), models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([a, b]) again error = %v, want nil", err)
		}
		if diff := cmp.Diff(usCounterRowsOf(first), usCounterRowsOf(again)); diff != "" || len(again) != 2 {
			t.Errorf("UpsertMany([a, b]) again returned %d entities, want the same 2 rows (-first +again):\n%s", len(again), diff)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("a conflict leaves a column the row leaves unset as the conflicting row has it", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 7)
		d := usCreateCounter(t, h, "d", 7)
		begins := &beginCounter{Querier: h.Base}
		h.Querier.ResetCount()
		got, err := models.New(begins).Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "c", UpdatedAt: omittable.Set(models.Now())},
			usCounterInput("d", 9),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([c, Hits unset]) error = %v, want nil", err)
		}
		// One sub-batch whose two rows set different update columns: its two
		// INSERTs run in one transaction, which the counting querier does not
		// record, then come the key read-back and the terminal re-fetch.
		if diff := cmp.Diff([]string{"SELECT", "SELECT"}, usStatementVerbs(h)); diff != "" {
			t.Errorf("UpsertMany([c, Hits unset]) statements outside a transaction (-want +got):\n%s", diff)
		}
		if n := begins.n.Load(); n != 1 {
			t.Errorf("UpsertMany([c, Hits unset]) began %d transactions, want 1", n)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 7}, "d": {ID: d.ID, Hits: 9}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after UpsertMany([c, Hits unset]) (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(want, usCounterRowsOf(got)); diff != "" {
			t.Errorf("UpsertMany([c, Hits unset]) returned (-want +got):\n%s", diff)
		}
	})

	t.Run("a conflict restores a soft-deleted row only when the row sets the soft-delete column", func(t *testing.T) {
		h.Reset(t)
		usSeedDeletedCounter(t, h, "restored")
		usSeedDeletedCounter(t, h, "kept")
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "restored", Hits: omittable.Set(int64(1)), UpdatedAt: omittable.Set(models.Now()), DeletedAt: omittable.Set(usZeroOf(models.DeletedAt(models.Now())))},
			usCounterInput("kept", 2),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([restored, kept]) error = %v, want nil", err)
		}
		if diff := cmp.Diff([]string{"kept"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after UpsertMany (-want +got):\n%s", diff)
		}
		deleted := map[string]bool{}
		for _, c := range got {
			deleted[c.Name] = usCounterDeleted(c)
		}
		if diff := cmp.Diff(map[string]bool{"restored": false, "kept": true}, deleted); diff != "" {
			t.Errorf("UpsertMany([restored, kept]) returned soft-delete state by name (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("a primary-key target on a database-generated key is refused before anything is written", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		h.Querier.ResetCount()
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{usCounterInput("c", 5), usCounterInput("d", 2)}, models.CounterConflictPK)
		if err == nil || got != nil {
			t.Fatalf("UpsertMany(counters, ConflictPK) = %+v, %v, want nil and an error", got, err)
		}
		if _, err := h.Client.Products().UpsertMany(ctx, []*models.CreateProductInput{{Name: "p", Price: 1}}, models.ProductConflictPK); err == nil {
			t.Errorf("UpsertMany(products, ConflictPK) error = nil, want an error")
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("the refused UpsertMany calls sent %q, want nothing", stmts)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 1}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the refused UpsertMany (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "products", 0)
	})

	t.Run("nil input list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		usCreateCounter(t, h, "c", 1)
		h.Querier.ResetCount()
		got, err := h.Client.Counters().UpsertMany(ctx, nil, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany(nil) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpsertMany(nil) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("UpsertMany(nil) returned %d counters, want none", len(got))
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("empty input list sends no statement and returns nothing", func(t *testing.T) {
		h.Reset(t)
		usCreateCounter(t, h, "c", 1)
		h.Querier.ResetCount()
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([]) error = %v, want nil", err)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpsertMany([]) sent %q, want nothing", stmts)
		}
		if len(got) != 0 {
			t.Errorf("UpsertMany([]) returned %d counters, want none", len(got))
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("a nil element returns ErrNilInput, fires no hook and writes nothing", func(t *testing.T) {
		h.Reset(t)
		client, seen := mutationRecorder(h)
		h.Querier.ResetCount()
		got, err := client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{usCounterInput("c", 1), nil}, models.CounterConflictName)
		if !errors.Is(err, models.ErrNilInput) {
			t.Fatalf("UpsertMany([c, nil]) error = %v, want ErrNilInput", err)
		}
		if got != nil {
			t.Errorf("UpsertMany([c, nil]) = %+v, want nil", got)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("mutation hook fired %d times for UpsertMany([c, nil]), want 0", n)
		}
		if stmts := h.Querier.Statements(); len(stmts) != 0 {
			t.Errorf("UpsertMany([c, nil]) sent %q, want nothing", stmts)
		}
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("upserts inside the caller's transaction", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			h.Querier.ResetCount()
			if _, err := h.Client.Counters().UpsertMany(txCtx, []*models.CreateCounterInput{usCounterInput("c", 5), usCounterInput("d", 2)}, models.CounterConflictName); err != nil {
				return err
			}
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("UpsertMany inside the transaction sent %q outside it, want nothing", stmts)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		rows := usCounterRows(t, h)
		if got := rows["c"]; got != (usCounterRow{ID: c.ID, Hits: 5}) {
			t.Errorf("counter c after the commit = %+v, want ID %d with Hits 5", got, c.ID)
		}
		if got, ok := rows["d"]; !ok || got.Hits != 2 {
			t.Errorf("counter d after the commit = %+v (present %v), want Hits 2", got, ok)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("rolled-back transaction undoes both the updates and the inserts", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Counters().UpsertMany(txCtx, []*models.CreateCounterInput{usCounterInput("c", 5), usCounterInput("d", 2)}, models.CounterConflictName); err != nil {
				return err
			}
			// The writes are visible inside the transaction, so the rollback is what undoes them.
			rows, err := usCounterRowsIn(txCtx, h)
			if err != nil {
				return err
			}
			if rows["c"].Hits != 5 || rows["d"].Hits != 2 || len(rows) != 2 {
				t.Errorf("counters inside the transaction = %+v, want c with Hits 5 and d with Hits 2", rows)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 1}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the rollback (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one upsert_many mutation hook with the deduped inputs and every written key affected", func(t *testing.T) {
		h.Reset(t)
		b := usCreateCounter(t, h, "b", 1)
		usCreateCounter(t, h, "other", 1)
		client, seen := mutationRecorder(h)
		inA, inB, inA2 := usCounterInput("a", 1), usCounterInput("b", 5), usCounterInput("a", 2)
		got, err := client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{inA, inB, inA2}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([a, b, a]) error = %v, want nil", err)
		}
		a, ok := usCounterRowsOf(got)["a"]
		if !ok {
			t.Fatalf("UpsertMany([a, b, a]) returned %+v, want a among them", got)
		}
		if len(seen()) != 1 {
			t.Fatalf("mutation hook fired %d times, want 1", len(seen()))
		}
		m := seen()[0]
		if m.Op != hook.OpUpsertMany || m.Table != models.TableCounters || m.PK != nil {
			t.Errorf("MutationContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				m.Op, m.Table, m.PK, hook.OpUpsertMany, models.TableCounters)
		}
		// The published input is the deduped slice: a's last occurrence in a's first slot.
		if in, ok := m.Input.([]*models.CreateCounterInput); !ok || len(in) != 2 || in[0] != inA2 || in[1] != inB {
			t.Errorf("MutationContext.Input = %#v, want the deduped inputs [a (last), b]", m.Input)
		}
		// The inserted a and the updated b, index-aligned with the deduped inputs.
		if diff := cmp.Diff([]any{a.ID, b.ID}, m.AffectedPKs); diff != "" {
			t.Errorf("AffectedPKs after the mutation (-want +got):\n%s", diff)
		}
	})

	t.Run("referencing a missing row returns a foreign key ConstraintError", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		_, err := h.Client.OrderItems().UpsertMany(ctx, []*models.CreateOrderItemInput{
			{OrderID: 1, ProductID: p.ID, Quantity: 1},
			{OrderID: 2, ProductID: p.ID + 1000, Quantity: 1},
		}, models.OrderItemConflictPK)
		usAssertConstraint(t, err, models.ConstraintForeignKey)
		assertRowCount(t, h, "order_items", 0)
	})

	t.Run("a conflict that breaks a CHECK returns a check ConstraintError and leaves the row unchanged", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 3)
		_, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{usCounterInput("c", -1)}, models.CounterConflictName)
		usAssertConstraint(t, err, models.ConstraintCheck)
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 3}}
		if diff := cmp.Diff(want, usCounterRows(t, h)); diff != "" {
			t.Errorf("counters after the failed UpsertMany (-want +got):\n%s", diff)
		}
	})

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{{Name: "c"}}, models.CounterConflictName)
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})

	t.Run("a failed sub-batch keeps the sub-batches before it", func(t *testing.T) {
		h.Reset(t)
		// counters keeps the default batch size of 200, so the 201st input is
		// the second sub-batch, which breaks the CHECK.
		inputs := make([]*models.CreateCounterInput, 0, 201)
		for i := range 200 {
			inputs = append(inputs, usCounterInput(fmt.Sprintf("c%03d", i), 1))
		}
		inputs = append(inputs, usCounterInput("bad", -1))
		_, err := h.Client.Counters().UpsertMany(ctx, inputs, models.CounterConflictName)
		usAssertConstraint(t, err, models.ConstraintCheck)
		rows := usCounterRows(t, h)
		if _, ok := rows["bad"]; ok || len(rows) != 200 {
			t.Errorf("counters after the failed UpsertMany: %d rows (bad present: %v), want the first sub-batch's 200", len(rows), ok)
		}
	})

	t.Run("a failed group rolls back the groups of its sub-batch written before it", func(t *testing.T) {
		h.Reset(t)
		usCreateCounter(t, h, "kept", 1)
		// One sub-batch, two groups: fresh leaves hits unset, bad sets it and
		// breaks the CHECK.
		_, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "fresh", UpdatedAt: omittable.Set(models.Now())},
			usCounterInput("bad", -1),
		}, models.CounterConflictName)
		usAssertConstraint(t, err, models.ConstraintCheck)
		if got := slices.Sorted(maps.Keys(usCounterRows(t, h))); !slices.Equal(got, []string{"kept"}) {
			t.Errorf("counters after the failed UpsertMany = %q, want only [kept]", got)
		}
	})

	t.Run("inside the caller's transaction a failed group rolls back to its savepoint", func(t *testing.T) {
		h.Reset(t)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := h.Client.Counters().Create(txCtx, usCounterInput("before", 1)); err != nil {
				return err
			}
			_, err := h.Client.Counters().UpsertMany(txCtx, []*models.CreateCounterInput{
				{Name: "fresh", UpdatedAt: omittable.Set(models.Now())},
				usCounterInput("bad", -1),
			}, models.CounterConflictName)
			usAssertConstraint(t, err, models.ConstraintCheck)
			// The caller's transaction is still usable after the savepoint
			// rolled back.
			_, err = h.Client.Counters().Create(txCtx, usCounterInput("after", 1))
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
		if got := slices.Sorted(maps.Keys(usCounterRows(t, h))); !slices.Equal(got, []string{"after", "before"}) {
			t.Errorf("counters after the transaction = %q, want [after before]", got)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		usCreateCounter(t, h, "a", 1)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{usCounterInput("a", 5), usCounterInput("b", 2)}, models.CounterConflictName, func(o *models.CallOptions[models.CounterFieldOptions]) {
			o.FieldOptions = &models.CounterFieldOptions{Name: true, Hits: true}
		})
		if err != nil {
			t.Fatalf("UpsertMany([a, b], Name and Hits only) error = %v, want nil", err)
		}
		want := []*models.Counter{{Name: "a", Hits: 5}, {Name: "b", Hits: 2}}
		byName := cmpopts.SortSlices(func(x, y *models.Counter) bool { return x.Name < y.Name })
		if diff := cmp.Diff(want, got, byName); diff != "" {
			t.Errorf("UpsertMany([a, b], Name and Hits only) (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 2)
	})

	t.Run("an empty FieldOptions upserts and returns nil", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p.ID})
		h.Querier.ResetCount()
		got, err := h.Client.OrderItems().UpsertMany(ctx, []*models.CreateOrderItemInput{
			{OrderID: 1, ProductID: p.ID, Quantity: 5},
			{OrderID: 2, ProductID: p.ID, Quantity: 3},
		}, models.OrderItemConflictPK, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{}
		})
		if err != nil {
			t.Fatalf("UpsertMany(empty FieldOptions) error = %v, want nil", err)
		}
		if got != nil {
			t.Errorf("UpsertMany(empty FieldOptions) = %+v, want nil", got)
		}
		// The write is the call's last statement: no follow-up read.
		if stmts := h.Querier.Statements(); len(stmts) == 0 || !strings.HasPrefix(stmts[len(stmts)-1], "INSERT") {
			t.Errorf("UpsertMany(empty FieldOptions) sent %q, want the INSERT last", stmts)
		}
		want := map[models.OrderItemPK]int64{{OrderID: 1, ProductID: p.ID}: 5, {OrderID: 2, ProductID: p.ID}: 3}
		if diff := cmp.Diff(want, usOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("order items after UpsertMany (-want +got):\n%s", diff)
		}
	})

	t.Run("ignores LockMode outside a transaction", func(t *testing.T) {
		h.Reset(t)
		c := usCreateCounter(t, h, "c", 1)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{usCounterInput("c", 5)}, models.CounterConflictName, withLock[models.CounterFieldOptions](sql.LockForUpdate))
		if err != nil {
			t.Fatalf("UpsertMany([c], LockForUpdate) error = %v, want nil", err)
		}
		want := map[string]usCounterRow{"c": {ID: c.ID, Hits: 5}}
		if diff := cmp.Diff(want, usCounterRowsOf(got)); diff != "" {
			t.Errorf("UpsertMany([c], LockForUpdate) returned (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("composite keys update the conflicting tuple, insert a new one and return only those", func(t *testing.T) {
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		conflict := models.OrderItemPK{OrderID: 10, ProductID: p.ID}
		inserted := models.OrderItemPK{OrderID: 20, ProductID: q.ID}
		// These cross the written tuples: each column value is written, only the pairing is not.
		crossing := []models.OrderItemPK{{OrderID: 10, ProductID: q.ID}, {OrderID: 20, ProductID: p.ID}}
		for _, pk := range append([]models.OrderItemPK{conflict}, crossing...) {
			createOrderItem(t, h, pk)
		}
		got, err := h.Client.OrderItems().UpsertMany(ctx, []*models.CreateOrderItemInput{
			{OrderID: conflict.OrderID, ProductID: conflict.ProductID, Quantity: 5},
			{OrderID: inserted.OrderID, ProductID: inserted.ProductID, Quantity: 3},
		}, models.OrderItemConflictPK)
		if err != nil {
			t.Fatalf("UpsertMany([%+v, %+v]) error = %v, want nil", conflict, inserted, err)
		}
		// A per-column read-back would also return the crossing tuples.
		wantGot := []*models.OrderItem{
			{OrderID: 10, ProductID: p.ID, Quantity: 5},
			{OrderID: 20, ProductID: q.ID, Quantity: 3},
		}
		byKey := cmpopts.SortSlices(func(x, y *models.OrderItem) bool { return x.OrderID < y.OrderID })
		if diff := cmp.Diff(wantGot, got, byKey); diff != "" {
			t.Errorf("UpsertMany([%+v, %+v]) returned (-want +got):\n%s", conflict, inserted, diff)
		}
		want := map[models.OrderItemPK]int64{conflict: 5, inserted: 3, crossing[0]: 1, crossing[1]: 1}
		if diff := cmp.Diff(want, usOrderItemQuantities(t, h)); diff != "" {
			t.Errorf("order items after UpsertMany (-want +got):\n%s", diff)
		}
	})

	t.Run("composite keys return a soft-deleted tuple the batch writes", func(t *testing.T) {
		h.Reset(t)
		deleted := models.RevisionPK{DocumentID: 1, Number: 2}
		inserted := models.RevisionPK{DocumentID: 3, Number: 1}
		// (1, 1) and (3, 2) cross the written tuples; (2, 1) mirrors one of them.
		crossing := []models.RevisionPK{{DocumentID: 1, Number: 1}, {DocumentID: 3, Number: 2}, {DocumentID: 2, Number: 1}}
		for _, pk := range append([]models.RevisionPK{deleted}, crossing...) {
			createRevision(t, h, pk)
		}
		if _, err := h.Client.Revisions().SoftDelete(ctx, deleted); err != nil {
			t.Fatalf("SoftDelete revision %+v: %v", deleted, err)
		}
		got, err := h.Client.Revisions().UpsertMany(ctx, []*models.CreateRevisionInput{
			{DocumentID: 1, Number: 2, Body: "new", DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
			{DocumentID: 3, Number: 1, Body: "inserted"},
		}, models.RevisionConflictPK)
		if err != nil {
			t.Fatalf("UpsertMany([soft-deleted %+v, %+v]) error = %v, want nil", deleted, inserted, err)
		}
		gotState := map[models.RevisionPK]string{}
		for _, r := range got {
			state := r.Body
			if usRevisionDeleted(r) {
				state += " (deleted)"
			}
			gotState[models.RevisionPK{DocumentID: r.DocumentID, Number: r.Number}] = state
		}
		if diff := cmp.Diff(map[models.RevisionPK]string{deleted: "new (deleted)", inserted: "inserted"}, gotState); diff != "" {
			t.Errorf("UpsertMany returned (-want +got):\n%s", diff)
		}
		want := map[models.RevisionPK]string{deleted: "new", inserted: "inserted", crossing[0]: "body", crossing[1]: "body", crossing[2]: "body"}
		if diff := cmp.Diff(want, usRevisionBodies(t, h)); diff != "" {
			t.Errorf("revision bodies after UpsertMany (-want +got):\n%s", diff)
		}
	})
}

func usCounterDeleted(c *models.Counter) bool {
	return !cmp.Equal(c.DeletedAt, models.Counter{}.DeletedAt)
}

func usRevisionDeleted(r *models.Revision) bool {
	return !cmp.Equal(r.DeletedAt, models.Revision{}.DeletedAt)
}

// usDeletedCounterNames returns the names of the soft-deleted counters, sorted.
func usDeletedCounterNames(t *testing.T, h *Harness) []string {
	t.Helper()
	counters, err := h.Client.Counters().GetMany(context.Background(), &models.GetCountersInput{
		Filter: &models.CounterFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
	})
	if err != nil {
		t.Fatalf("Counters().GetMany(DeletedAt IS NOT NULL) error = %v", err)
	}
	names := make([]string, 0, len(counters))
	for _, c := range counters {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	return names
}

// usSeedDeletedCounter creates a counter and soft-deletes it.
func usSeedDeletedCounter(t *testing.T, h *Harness, name string) {
	t.Helper()
	ctx := context.Background()
	cn, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())})
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	if _, err := h.Client.Counters().SoftDelete(ctx, cn.ID); err != nil {
		t.Fatalf("SoftDelete counter %q: %v", name, err)
	}
}

// usZeroOf returns the zero value of its argument's type: a soft-delete
// column's not-deleted state, whatever Go type the dialect gives it.
func usZeroOf[T any](T) T {
	var zero T
	return zero
}

// usCounterInput is a counter input that sets name, hits and updated_at.
func usCounterInput(name string, hits int64) *models.CreateCounterInput {
	return &models.CreateCounterInput{Name: name, Hits: omittable.Set(hits), UpdatedAt: omittable.Set(models.Now())}
}

func usCreateCounter(t *testing.T, h *Harness, name string, hits int64) *models.Counter {
	t.Helper()
	c, err := h.Client.Counters().Create(context.Background(), usCounterInput(name, hits))
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	return c
}

// usCounterRow is a stored counter's key and hits.
type usCounterRow struct {
	ID   int64
	Hits int64
}

// usCounterRowsOf keys returned counters by name.
func usCounterRowsOf(counters []*models.Counter) map[string]usCounterRow {
	rows := make(map[string]usCounterRow, len(counters))
	for _, c := range counters {
		rows[c.Name] = usCounterRow{ID: c.ID, Hits: c.Hits}
	}
	return rows
}

// usCounterRows reads every counter, soft-deleted ones included, with a
// statement the counting querier does not record.
func usCounterRows(t *testing.T, h *Harness) map[string]usCounterRow {
	t.Helper()
	rows, err := usCounterRowsIn(context.Background(), h)
	if err != nil {
		t.Fatalf("read counters: %v", err)
	}
	return rows
}

// usCounterRowsIn reads every counter on ctx's transaction when it carries
// one, and outside any transaction otherwise.
func usCounterRowsIn(ctx context.Context, h *Harness) (map[string]usCounterRow, error) {
	rows, err := database.Conn(ctx, h.Querier.Inner).Query(ctx, "SELECT name, id, hits FROM counters")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]usCounterRow)
	for rows.Next() {
		var (
			name string
			row  usCounterRow
		)
		if err := rows.Scan(&name, &row.ID, &row.Hits); err != nil {
			return nil, err
		}
		out[name] = row
	}
	return out, rows.Err()
}

// usOrderItemQuantities reads every order item's quantity by key.
func usOrderItemQuantities(t *testing.T, h *Harness) map[models.OrderItemPK]int64 {
	t.Helper()
	ctx := context.Background()
	rows, err := h.Querier.Inner.Query(ctx, "SELECT order_id, product_id, quantity FROM order_items")
	if err != nil {
		t.Fatalf("read order items: %v", err)
	}
	defer rows.Close()
	out := make(map[models.OrderItemPK]int64)
	for rows.Next() {
		var (
			pk  models.OrderItemPK
			qty int64
		)
		if err := rows.Scan(&pk.OrderID, &pk.ProductID, &qty); err != nil {
			t.Fatalf("scan order item: %v", err)
		}
		out[pk] = qty
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read order items: %v", err)
	}
	return out
}

// usRevisionBodies reads every revision's body by key, soft-deleted ones included.
func usRevisionBodies(t *testing.T, h *Harness) map[models.RevisionPK]string {
	t.Helper()
	ctx := context.Background()
	rows, err := h.Querier.Inner.Query(ctx, "SELECT document_id, number, body FROM revisions")
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

// usStatementVerbs returns the first word of each recorded statement.
func usStatementVerbs(h *Harness) []string {
	var verbs []string
	for _, s := range h.Querier.Statements() {
		verb, _, _ := strings.Cut(strings.TrimSpace(s), " ")
		verbs = append(verbs, strings.ToUpper(verb))
	}
	return verbs
}

func usLiveLabelCount(t *testing.T, h *Harness) int64 {
	t.Helper()
	n, err := h.Client.Labels().Count(context.Background(), nil)
	if err != nil {
		t.Fatalf("Labels().Count error = %v", err)
	}
	return n
}

// usAssertConstraint checks that err is a ConstraintError of type want that
// unwraps to ErrConstraintViolation.
func usAssertConstraint(t *testing.T, err error, want models.ConstraintType) {
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
