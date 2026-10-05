package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// *Where mutation event emission (PRD §28.4 table, §28.6 Input).
//
// Existing event_test.go already covers the basic per-action mapping for each
// *Where op (UpdateWhere → Update, SoftDeleteWhere → Delete, HardDeleteWhere →
// Delete, RestoreWhere → Update) and the "N affected rows → N events" property
// for two-row sets. These tests strengthen coverage on two axes:
//
//   1. **N>2 affected rows** — verify the per-PK fan-out scales beyond pair.
//      The §28.4 contract says "1 event per affected row" with no batching at
//      the event layer; a regression that, say, deduplicated PKs would slip
//      past the 2-row sets in event_test.go.
//
//   2. **Event.Input carries the operation-specific input** (§28.6) —
//        UpdateWhere    → *UpdateInput  (the SET payload; filter is the row selector)
//        SoftDeleteWhere → *Filter      (no separate input)
//        HardDeleteWhere → *Filter      (no separate input)
//        RestoreWhere    → *Filter      (no separate input)
//      Per PRD §28.6 "the same value the caller passed to the mutation —
//      *CreateInput, *UpdateInput, filter, etc." The pointer is propagated by
//      reference (MutationContext.Input is the caller's pointer, not a copy).

// TestWhereMutation_UpdateWhere_NRowsNEvents verifies UpdateWhere fans out one
// event per affected row, and Event.Input carries the caller's *UpdateInput
// (the SET payload — the filter is the row selector, not the input). See
// §28.6 and the per-op input mapping in the file-header comment.
func TestWhereMutation_UpdateWhere_NRowsNEvents(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 5
	ids := make([]int64, 0, n)
	for i := range n {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name: "UWN", SKU: t.Name() + "-" + string(rune('A'+i)), Price: float64(i + 1),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}
	t.Cleanup(func() { _ = client.Products().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Update},
	})

	updInput := &models.UpdateProductInput{Price: omittable.Set(99.99)}
	if _, err := client.Products().UpdateWhere(
		ctx,
		&models.ProductFilter{ID: &comparator.Number[int64]{In: ids}},
		updInput,
	); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("UpdateWhere(%d rows): got %d events, want %d", n, len(events), n)
	}

	// Per-event invariants: action, table, PK in expected set, Input is the
	// caller's *UpdateInput pointer (the SET payload).
	wantPKs := make(map[int64]bool, n)
	for _, id := range ids {
		wantPKs[id] = true
	}
	for i, e := range events {
		if e.Action != event.Update {
			t.Errorf("event[%d].Action = %q, want Update", i, e.Action)
		}
		if e.Table != "products" {
			t.Errorf("event[%d].Table = %q, want products", i, e.Table)
		}
		pk, ok := e.PK.(int64)
		if !ok {
			t.Errorf("event[%d].PK type = %T, want int64", i, e.PK)
			continue
		}
		if !wantPKs[pk] {
			t.Errorf("event[%d].PK = %d, not in seeded set", i, pk)
		}
		gotInput, ok := e.Input.(*models.UpdateProductInput)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *UpdateProductInput", i, e.Input)
			continue
		}
		if gotInput != updInput {
			t.Errorf("event[%d].Input pointer = %p, want %p (Input must propagate by reference)", i, gotInput, updInput)
		}
	}
}

// TestWhereMutation_SoftDeleteWhere_InputIsFilter verifies SoftDeleteWhere
// emits one Delete event per affected row with Event.Input = the filter
// pointer.
func TestWhereMutation_SoftDeleteWhere_InputIsFilter(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 3
	ids := make([]int64, 0, n)
	for i := range n {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title: "SDWN", Author: t.Name(),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, a.ID)
	}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"articles"},
		Actions: []event.Action{event.Delete},
	})

	author := t.Name()
	filter := &models.ArticleFilter{Author: &comparator.String{Eq: &author}}
	if _, err := client.Articles().SoftDeleteWhere(ctx, filter); err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("SoftDeleteWhere(%d rows): got %d events, want %d", n, len(events), n)
	}
	for i, e := range events {
		if e.Action != event.Delete {
			t.Errorf("event[%d].Action = %q, want Delete", i, e.Action)
		}
		gotInput, ok := e.Input.(*models.ArticleFilter)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *ArticleFilter", i, e.Input)
			continue
		}
		if gotInput != filter {
			t.Errorf("event[%d].Input pointer = %p, want %p", i, gotInput, filter)
		}
	}
}

// TestWhereMutation_HardDeleteWhere_InputIsFilter verifies HardDeleteWhere
// emits one Delete event per affected row with Event.Input = the filter.
func TestWhereMutation_HardDeleteWhere_InputIsFilter(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 3
	ids := make([]int64, 0, n)
	for i := range n {
		p, err := client.Products().Create(ctx, &models.CreateProductInput{
			Name: "HDWN", SKU: t.Name() + "-" + string(rune('A'+i)), Price: float64(i + 1),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Delete},
	})

	filter := &models.ProductFilter{ID: &comparator.Number[int64]{In: ids}}
	if err := client.Products().HardDeleteWhere(ctx, filter); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("HardDeleteWhere(%d rows): got %d events, want %d", n, len(events), n)
	}
	for i, e := range events {
		if e.Action != event.Delete {
			t.Errorf("event[%d].Action = %q, want Delete", i, e.Action)
		}
		gotInput, ok := e.Input.(*models.ProductFilter)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *ProductFilter", i, e.Input)
			continue
		}
		if gotInput != filter {
			t.Errorf("event[%d].Input pointer = %p, want %p", i, gotInput, filter)
		}
	}
}

// TestWhereMutation_RestoreWhere_InputIsFilter verifies RestoreWhere emits one
// Update event per restored row with Event.Input = the filter (Restore* maps
// to Update per §28.4).
func TestWhereMutation_RestoreWhere_InputIsFilter(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	const n = 3
	ids := make([]int64, 0, n)
	for i := range n {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title: "RWN", Author: t.Name(),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, a.ID)
	}
	if _, err := client.Articles().SoftDeleteMany(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteMany seed: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"articles"},
		Actions: []event.Action{event.Update},
	})

	author := t.Name()
	filter := &models.ArticleFilter{Author: &comparator.String{Eq: &author}}
	if _, err := client.Articles().RestoreWhere(ctx, filter); err != nil {
		t.Fatalf("RestoreWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != n {
		t.Fatalf("RestoreWhere(%d rows): got %d events, want %d", n, len(events), n)
	}
	for i, e := range events {
		if e.Action != event.Update {
			t.Errorf("event[%d].Action = %q, want Update", i, e.Action)
		}
		gotInput, ok := e.Input.(*models.ArticleFilter)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *ArticleFilter", i, e.Input)
			continue
		}
		if gotInput != filter {
			t.Errorf("event[%d].Input pointer = %p, want %p", i, gotInput, filter)
		}
	}
}

// TestWhereMutation_ZeroMatch_NoEvents verifies that when a *Where predicate
// matches zero rows, no events are emitted. Events are gated on AffectedPKs
// (§28.4 batch path); zero affected rows → zero events.
func TestWhereMutation_ZeroMatch_NoEvents(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables: []string{"articles"},
	})

	noMatch := "where-zero-match-events-" + t.Name()
	if _, err := client.Articles().UpdateWhere(
		ctx,
		&models.ArticleFilter{Author: &comparator.String{Eq: &noMatch}},
		&models.UpdateArticleInput{Title: omittable.Set("ignored")},
	); err != nil {
		t.Fatalf("UpdateWhere(zero match): %v", err)
	}

	events := got.snapshot()
	if len(events) != 0 {
		t.Errorf("UpdateWhere(zero match): got %d events, want 0", len(events))
	}
}
