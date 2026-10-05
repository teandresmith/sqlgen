package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// Batch event emission — PRD §28.4: "Batch operations produce multiple
// events. The generated terminal sets MutationContext.AffectedPKs [...] The
// event hook iterates AffectedPKs to build one event per entity."
//
// Existing event_test.go covers the cardinality (N inputs → N events) for
// CreateMany / UpdateMany / SoftDeleteMany / HardDeleteMany / RestoreMany.
// This file strengthens coverage on two axes the existing tests do NOT pin:
//
//  1. **Ordering.** Events fire in insertion order. Existing TestEvent_CreateMany
//     asserts set membership (map keyed by PK) which would pass even if the
//     event hook shuffled outputs — a regression that reordered events
//     (e.g., switched to a concurrent map during iteration) would slip
//     through. The task bullet "event order matches entity insertion order"
//     is explicit in §14.11.
//
//  2. **Per-entity PK uniqueness.** A batch must not emit duplicate PKs or
//     skip any input. The §28.4 invariant "1 event per entity in the batch"
//     is loudly violated by dedup / skip bugs. We assert each event's PK
//     maps 1:1 with the input at the same index.

// TestEventBatch_CreateMany_OrderMatchesInsertion verifies that the N events
// fanned out from a CreateMany arrive in the same order as the input slice.
// For an AUTOINCREMENT table, inserted IDs are allocated in insertion order
// (SQLite guarantees this for serial inserts); so event[i].PK must equal
// created[i].ID.
func TestEventBatch_CreateMany_OrderMatchesInsertion(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 5
	inputs := make([]*models.CreateProductInput, n)
	for i := range n {
		inputs[i] = &models.CreateProductInput{
			Name:  "BatchOrd",
			SKU:   "EV-BATCH-ORD-" + string(rune('A'+i)),
			Price: float64(i + 1),
		}
	}

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	created, err := client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]int64, len(created))
		for i, p := range created {
			ids[i] = p.ID
		}
		_ = client.Products().HardDeleteMany(ctx, ids)
	})

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}

	// Strict ordering: event[i].PK must equal the i-th created row's PK.
	for i, e := range events {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Errorf("event[%d].PK type = %T, want int64", i, e.PK)
			continue
		}
		if pk != created[i].ID {
			t.Errorf("event[%d].PK = %d, want %d (order must match input slice)", i, pk, created[i].ID)
		}
	}
}

// TestEventBatch_UpdateMany_OrderMatchesInsertion verifies UpdateMany emits
// events in the same order as the items slice. Unlike CreateMany (where
// order correlates with the DB's ID allocation), UpdateMany operates on
// pre-existing PKs passed by the caller — the assertion is purely about the
// event hook's iteration order, not the underlying DB.
func TestEventBatch_UpdateMany_OrderMatchesInsertion(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	// Seed 5 products. Use distinct SKUs to avoid Upsert-style collisions.
	const n = 5
	ids := make([]int64, 0, n)
	for i := range n {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name:  "BatchUMOrd",
			SKU:   "EV-BATCH-UM-" + string(rune('A'+i)),
			Price: float64(i + 1),
		})
		if err != nil {
			t.Fatalf("seed Create %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}
	t.Cleanup(func() { _ = client.Products().HardDeleteMany(ctx, ids) })

	// Shuffle the UpdateMany input order vs. insertion order by reversing
	// ids. If the event hook accidentally iterates a PK-sorted slice instead
	// of AffectedPKs, the assertion will catch it — events should match
	// items, not a resort.
	items := make([]models.UpdateProductItem, n)
	for i := range n {
		revID := ids[n-1-i]
		items[i] = models.UpdateProductItem{
			ID:    revID,
			Input: &models.UpdateProductInput{Price: omittable.Set(float64(100 + i))},
		}
	}

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Update},
	})

	if _, err := client.Products().UpdateMany(ctx, items); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}
	for i, e := range events {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Errorf("event[%d].PK type = %T, want int64", i, e.PK)
			continue
		}
		if pk != items[i].ID {
			t.Errorf("event[%d].PK = %d, want %d (events must follow items order, not sorted)",
				i, pk, items[i].ID)
		}
	}
}

// TestEventBatch_CreateMany_DistinctPKs verifies each event in a CreateMany
// fan-out has a distinct PK. A dedup bug (e.g., map-keyed-by-PK-in-hook)
// would emit fewer than N events or repeat one.
func TestEventBatch_CreateMany_DistinctPKs(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 7
	inputs := make([]*models.CreateProductInput, n)
	for i := range n {
		inputs[i] = &models.CreateProductInput{
			Name:  "BatchDist",
			SKU:   "EV-BATCH-DIST-" + string(rune('A'+i)),
			Price: 1,
		}
	}

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	created, err := client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]int64, len(created))
		for i, p := range created {
			ids[i] = p.ID
		}
		_ = client.Products().HardDeleteMany(ctx, ids)
	})

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}

	seen := make(map[int64]bool, n)
	for i, e := range events {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Errorf("event[%d].PK type = %T, want int64", i, e.PK)
			continue
		}
		if seen[pk] {
			t.Errorf("event[%d].PK = %d already seen — duplicate PK across batch events", i, pk)
		}
		seen[pk] = true
	}
}

// TestEventBatch_SoftDeleteMany_OrderMatchesInsertion verifies delete-batch
// event ordering follows the caller-supplied ids slice.
func TestEventBatch_SoftDeleteMany_OrderMatchesInsertion(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 4
	ids := make([]int64, 0, n)
	for i := range n {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title: "BatchSDMOrd", Author: "sdm",
		})
		if err != nil {
			t.Fatalf("seed Create %d: %v", i, err)
		}
		ids = append(ids, a.ID)
	}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	// Reverse the ids slice for the delete call — events should follow this
	// reversed order, not the original ascending insertion order.
	reversed := make([]int64, n)
	for i := range n {
		reversed[i] = ids[n-1-i]
	}

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"articles"},
		Actions: []event.Action{event.Delete},
	})
	if _, err := client.Articles().SoftDeleteMany(ctx, reversed); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}
	for i, e := range events {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Errorf("event[%d].PK type = %T, want int64", i, e.PK)
			continue
		}
		if pk != reversed[i] {
			t.Errorf("event[%d].PK = %d, want %d (events must follow caller's ids order)", i, pk, reversed[i])
		}
	}
}

// TestEventBatch_CreateMany_PerEventActionAndTable verifies every event in
// the batch is shaped correctly — Action, Table, Schema — not just the
// aggregate count. A regression that emitted the wrong Action for the tail
// of a batch (e.g., copied the first event's shape N times) would be caught
// here even though per-event PKs are correct.
func TestEventBatch_CreateMany_PerEventActionAndTable(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 3
	inputs := make([]*models.CreateProductInput, n)
	for i := range n {
		inputs[i] = &models.CreateProductInput{
			Name: "BatchShape", SKU: "EV-BATCH-SH-" + string(rune('A'+i)), Price: 1,
		}
	}

	got := subscribe(t, bus, event.SubscribeOptions{})
	created, err := client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]int64, len(created))
		for i, p := range created {
			ids[i] = p.ID
		}
		_ = client.Products().HardDeleteMany(ctx, ids)
	})

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}
	for i, e := range events {
		if e.Action != event.Create {
			t.Errorf("event[%d].Action = %q, want Create", i, e.Action)
		}
		if e.Table != "products" {
			t.Errorf("event[%d].Table = %q, want products", i, e.Table)
		}
		if e.Schema != "" {
			t.Errorf("event[%d].Schema = %q, want empty (sqlite)", i, e.Schema)
		}
		if e.ID == "" {
			t.Errorf("event[%d].ID is empty; every event must have a unique ID", i)
		}
		if e.Timestamp.IsZero() {
			t.Errorf("event[%d].Timestamp is zero", i)
		}
	}

	// Every event's ID must be unique — the event hook allocates a fresh
	// UUID per event (event_hooks_gen.go line 61).
	seenIDs := make(map[string]bool, n)
	for i, e := range events {
		if seenIDs[e.ID] {
			t.Errorf("event[%d].ID = %q already seen — every event must have a unique ID", i, e.ID)
		}
		seenIDs[e.ID] = true
	}
}
