package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// Transactions defer event publishing until commit. With CallbackSync the
// callbacks fire inside Commit, so post-commit assertions are deterministic.

func TestEventTx_CommitPublishes(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	var insideTxID int64
	err := client.WithTx(ctx, "commit-publishes", func(txCtx context.Context) error {
		p, err := client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "TxC", SKU: "EV-TX-COMMIT-1", Price: 1,
		})
		if err != nil {
			return err
		}
		insideTxID = p.ID

		// Inside the transaction, the event has been queued but not yet fired.
		if n := len(got.snapshot()); n != 0 {
			t.Errorf("inside tx before commit: got %d events, want 0", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, insideTxID) })

	events := got.waitFor(t, 1)
	if len(events) != 1 {
		t.Fatalf("after commit: got %d events, want 1", len(events))
	}
	if events[0].PK != insideTxID {
		t.Errorf("PK = %v, want %v", events[0].PK, insideTxID)
	}
}

func TestEventTx_RollbackDiscards(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{})

	sentinel := errors.New("rollback please")
	err := client.WithTx(ctx, "rollback-discards", func(txCtx context.Context) error {
		if _, err := client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "TxR", SKU: "EV-TX-ROLLBACK-1", Price: 1,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx err = %v, want sentinel", err)
	}

	if n := len(got.waitStable()); n != 0 {
		t.Errorf("got %d events after rollback, want 0", n)
	}
}

// Nested tx: inner rollback discards inner events; outer commit publishes outer events.
// Note: once an inner save-point rolls back, existing database semantics allow the
// outer transaction to keep operating. Any mutation attempted before the inner
// rollback is discarded; mutations done in the outer scope after the inner rollback
// still publish on commit.
func TestEventTx_NestedInnerRollbackOuterCommit(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	var outerID int64
	err := client.WithTx(ctx, "sp_outer", func(outerCtx context.Context) error {
		outer, err := client.Products().Create(outerCtx, &models.CreateProductInput{
			Name: "Outer", SKU: "EV-TX-NESTED-OUTER", Price: 1,
		})
		if err != nil {
			return err
		}
		outerID = outer.ID

		// Inner savepoint: create a row, then roll the savepoint back.
		sentinel := errors.New("discard inner")
		innerErr := client.WithTx(outerCtx, "sp_inner", func(innerCtx context.Context) error {
			if _, err := client.Products().Create(innerCtx, &models.CreateProductInput{
				Name: "Inner", SKU: "EV-TX-NESTED-INNER", Price: 1,
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(innerErr, sentinel) {
			t.Errorf("inner WithTx err = %v, want sentinel", innerErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, outerID) })

	events := got.waitFor(t, 1)
	if len(events) != 1 {
		t.Fatalf("got %d events after nested tx, want 1 (outer only)", len(events))
	}
	if events[0].PK != outerID {
		t.Errorf("published PK = %v, want outerID=%v (inner should have been discarded)", events[0].PK, outerID)
	}
}

// With WithCallbackMode(CallbackSync), OnCommit callbacks — including the
// event publish — fire synchronously inside Commit in the caller's goroutine.
// Tests should see all expected events immediately after WithTx returns,
// without polling.
func TestEventTx_SyncCallbackModePublishesOnCommit(t *testing.T) {
	ctx := context.Background()

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	client := models.New(
		dbstdlib.New(testDB),
		models.WithCallbackMode(database.CallbackSync),
		models.WithEventPublisher(bus),
	)

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	var createdID int64
	err := client.WithTx(ctx, "sync-commit", func(txCtx context.Context) error {
		p, err := client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "TxSync", SKU: "EV-TX-SYNC-1", Price: 1,
		})
		if err != nil {
			return err
		}
		createdID = p.ID

		// Inside the transaction, the event is queued but not fired yet —
		// sync mode affects when callbacks run (at commit), not whether
		// they are deferred through the transaction boundary.
		if n := len(got.snapshot()); n != 0 {
			t.Errorf("inside tx before commit: got %d events, want 0", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, createdID) })

	// No waitFor — sync mode fires callbacks in Commit's goroutine before
	// Commit returns, so snapshot() immediately reflects the result.
	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("after sync commit: got %d events, want 1 (sync callback mode should publish immediately)", len(events))
	}
	if events[0].PK != createdID {
		t.Errorf("PK = %v, want %v", events[0].PK, createdID)
	}
}
