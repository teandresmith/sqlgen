package tests

// Events × Transactions — deferred emission, insertion order, N-for-N.
//
// Exercises PRD §18.6 "Deferred Side Effects":
//   - During a transaction, mutation hooks collect events via Tx.OnCommit
//     rather than firing them immediately. Commit flushes in registration
//     (insertion) order; rollback discards every callback.
//
// The existing event_tx_test.go covers commit-publishes, rollback-discards,
// nested-inner-rollback, and sync-callback mode on single-mutation txs. This
// file fills three specific gaps:
//   1. No per-mutation immediate emission — between mutations inside the tx,
//      the subscriber sees zero events.
//   2. N mutations → exactly N events on commit (one event per affected row).
//   3. Events fire in registration (insertion) order per §18.6 "Deferred
//      callbacks fire in registration order".
//
// All three tests use CallbackSync so post-commit assertions are deterministic
// (no waitFor polling); the async path is already covered by the existing
// TestEventTx_CommitPublishes.

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// newSyncClient builds a client wired to CallbackSync so OnCommit callbacks
// fire inside Commit in the caller's goroutine. Tests below rely on this for
// deterministic post-commit assertions without waitFor.
func newSyncClient(t *testing.T) (*models.Client, *memorybus.Bus) {
	t.Helper()
	bus := memorybus.New()
	client := models.New(
		dbstdlib.New(testDB),
		models.WithCallbackMode(database.CallbackSync),
		models.WithEventPublisher(bus),
	)
	t.Cleanup(func() { _ = bus.Close() })
	return client, bus
}

// TestEventTx_NoImmediateEmissionBetweenMutations verifies that each mutation
// inside a transaction does NOT fire an event immediately — the hook defers
// via Tx.OnCommit and the subscriber sees nothing until commit.
//
// This is the load-bearing invariant of PRD §18.6: subscribers must not react
// to uncommitted data. If a future refactor flipped a mutation path back to
// immediate emission, this test catches it as a non-zero count mid-tx.
func TestEventTx_NoImmediateEmissionBetweenMutations(t *testing.T) {
	ctx := context.Background()
	client, bus := newSyncClient(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	ids := make([]int64, 0, 3)
	err := client.WithTx(ctx, "no-immediate-emission", func(txCtx context.Context) error {
		for i, sku := range []string{"EV-NIE-1", "EV-NIE-2", "EV-NIE-3"} {
			p, err := client.Products().Create(txCtx, &models.CreateProductInput{
				Name: "NoImmediate", SKU: sku, Price: float64(i + 1),
			})
			if err != nil {
				return err
			}
			ids = append(ids, p.ID)

			// Between every mutation, the subscriber must see zero events —
			// the hook stashed the event in Tx.OnCommit and the transaction
			// is still open. An immediate publish here would mean a
			// subscriber could react to a row that may still be rolled back.
			if n := len(got.snapshot()); n != 0 {
				t.Errorf("after Create %d (sku=%s): got %d events, want 0 (deferred until commit)", i+1, sku, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDeleteMany(ctx, ids) })

	// Sync callback mode: all 3 events are already published by the time
	// Commit returns — no polling needed.
	events := got.snapshot()
	if len(events) != 3 {
		t.Fatalf("after commit: got %d events, want 3 (one per Create)", len(events))
	}
}

// TestEventTx_NMutationsToNEventsOnCommit verifies the "one event per affected
// row" rule under a heterogeneous tx: Create + Update + Upsert + SoftDelete +
// HardDelete + Increment across two tables, all inside one transaction. Every
// committed mutation must produce exactly one event (no drops, no duplicates).
//
// Also a regression guard for §28.4's action-type table — each event carries
// the action that matches the mutation that produced it.
func TestEventTx_NMutationsToNEventsOnCommit(t *testing.T) {
	ctx := context.Background()
	client, bus := newSyncClient(t)

	// Seed an article outside the tx so SoftDelete has a target. Subscribe
	// AFTER the seed so the pre-tx Create does not pollute the count.
	seedArticle, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "NM Seed", Author: "nm",
	})
	if err != nil {
		t.Fatalf("Create seed article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, seedArticle.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{})

	var created, upserted int64
	err = client.WithTx(ctx, "n-for-n", func(txCtx context.Context) error {
		p1, err := client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "NM-1", SKU: "EV-NM-1", Price: 1,
		})
		if err != nil {
			return err
		}
		created = p1.ID

		if _, err := client.Products().Update(txCtx, p1.ID, &models.UpdateProductInput{
			Name: omittable.Set("NM-1-upd"),
		}); err != nil {
			return err
		}

		up, err := client.Products().Upsert(txCtx, &models.CreateProductInput{
			Name: "NM-Upsert", SKU: "EV-NM-UPSERT", Price: 2,
		}, models.ProductConflictSKU)
		if err != nil {
			return err
		}
		upserted = up.ID

		if err := client.Products().Increment(txCtx, p1.ID, models.IncrementInput[models.ProductIncrementColumn]{
			Column: models.ProductIncrementStock, Amount: 5,
		}); err != nil {
			return err
		}

		if _, err := client.Articles().SoftDelete(txCtx, seedArticle.ID); err != nil {
			return err
		}

		return client.Products().HardDelete(txCtx, up.ID)
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Products().HardDelete(ctx, created)
	})
	_ = upserted // HardDelete inside tx already removes it

	// 6 mutations → 6 events: Create, Update, Upsert, Increment(→Update),
	// SoftDelete(→Delete), HardDelete(→Delete).
	events := got.snapshot()
	if len(events) != 6 {
		t.Fatalf("got %d events, want 6 (one per mutation)", len(events))
	}

	wantActions := []event.Action{
		event.Create, event.Update, event.Upsert, event.Update, event.Delete, event.Delete,
	}
	for i, e := range events {
		if e.Action != wantActions[i] {
			t.Errorf("events[%d].Action = %q, want %q", i, e.Action, wantActions[i])
		}
	}
}

// TestEventTx_EventsFireInInsertionOrder verifies §18.6 "Deferred callbacks
// fire in registration order (the order mutations occurred within the
// transaction)". Five sequential Creates inside one tx must produce five
// events whose PKs match the Create order, not re-ordered by the DB.
func TestEventTx_EventsFireInInsertionOrder(t *testing.T) {
	ctx := context.Background()
	client, bus := newSyncClient(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	wantPKs := make([]int64, 0, 5)
	err := client.WithTx(ctx, "insertion-order", func(txCtx context.Context) error {
		for i, sku := range []string{"EV-ORD-A", "EV-ORD-B", "EV-ORD-C", "EV-ORD-D", "EV-ORD-E"} {
			p, err := client.Products().Create(txCtx, &models.CreateProductInput{
				Name: "Ord", SKU: sku, Price: float64(i + 1),
			})
			if err != nil {
				return err
			}
			wantPKs = append(wantPKs, p.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDeleteMany(ctx, wantPKs) })

	events := got.snapshot()
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	for i, e := range events {
		gotPK, ok := e.PK.(int64)
		if !ok {
			t.Fatalf("events[%d].PK type = %T, want int64", i, e.PK)
		}
		if gotPK != wantPKs[i] {
			t.Errorf("events[%d].PK = %d, want %d (insertion order broken)", i, gotPK, wantPKs[i])
		}
	}
}
