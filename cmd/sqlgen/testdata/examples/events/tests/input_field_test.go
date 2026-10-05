package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// Event.Input — PRD §28.6: each fanned-out event carries the **per-entity**
// input. For single-op mutations (Create / Update / Upsert / Increment),
// Event.Input is the same pointer the caller passed. For batch mutations
// (CreateMany / UpdateMany), Event.Input is the i-th element of the caller's
// input slice — typed as the same per-entity shape the single-op variant
// uses, so subscribers can run one type-switch across both shapes. For
// HardDelete / SoftDelete / Restore (single + *Many), Event.Input is nil
// because the PK already travels via Event.PK. For *Where ops, Event.Input
// is the operation's shared payload (filter for delete-class, *UpdateInput
// for UpdateWhere). Existing event_test.go + where_mutations_test.go cover
// Create, Update, UpdateWhere, SoftDeleteWhere, HardDeleteWhere, and
// RestoreWhere; this file fills the gaps (Upsert, single-PK
// SoftDelete/HardDelete, *Many per-entity assertions, Increment).
//
// Landed-behavior reconciliations documented inline:
//
//  1. **Single-PK SoftDelete / HardDelete / Restore carry `Event.Input == nil`.**
//     The MutationContext literal for these ops sets `PK: id` but leaves
//     `Input` unset. PRD §28.6 says single-PK delete-class ops have nil
//     Event.Input — the PK travels through the dedicated `Event.PK` field.
//     Same shape applies to the *Many variants (HardDeleteMany,
//     SoftDeleteMany, RestoreMany) — Event.Input is nil per-event because a
//     redundant []int64 payload would only repeat what Event.PK already
//     carries.
//
//  2. **`Increment` carries `IncrementInput[C]` by value.** The caller passes
//     the input by value; the MutationContext literal stores it as
//     `Input: input`. So `Event.Input.(IncrementInput[ProductIncrementColumn])`
//     succeeds (value assertion), not `.(*IncrementInput[...])`. The value
//     round-trips equality byte-for-byte (Column + Amount).

// TestEventInput_Upsert verifies Upsert carries the caller's *CreateInput
// pointer by reference — both the INSERT path and the DO-UPDATE path emit an
// Upsert event with the same Input type and identity.
func TestEventInput_Upsert(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Upsert}})

	insertInput := &models.CreateProductInput{Name: "UpsertInsert", SKU: "EV-IN-UPS-1", Price: 10.00}
	inserted, err := client.Products().Upsert(ctx, insertInput, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, inserted.ID) })

	updateInput := &models.CreateProductInput{Name: "UpsertUpdate", SKU: "EV-IN-UPS-1", Price: 20.00}
	if _, err := client.Products().Upsert(ctx, updateInput, models.ProductConflictSKU); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	// Per §28.6 Upsert carries *CreateInput (both paths — INSERT and UPDATE
	// share the same input shape at the caller's surface).
	wantPtrs := []*models.CreateProductInput{insertInput, updateInput}
	for i, e := range events {
		gotInput, ok := e.Input.(*models.CreateProductInput)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *CreateProductInput", i, e.Input)
			continue
		}
		if gotInput != wantPtrs[i] {
			t.Errorf("event[%d].Input pointer = %p, want %p", i, gotInput, wantPtrs[i])
		}
	}
}

// TestEventInput_SoftDelete pins the landed behavior: single-PK SoftDelete
// carries `Event.Input == nil`. See file header reconciliation (1). The PK
// travels via `Event.PK`, which remains a first-class field.
func TestEventInput_SoftDelete(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SD-In", Author: "t"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})
	if _, err := client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Input != nil {
		t.Errorf("SoftDelete Event.Input = %v (type %T), want nil (single-PK ops carry PK, not Input; §28.6)",
			events[0].Input, events[0].Input)
	}
	if events[0].PK != a.ID {
		t.Errorf("Event.PK = %v, want %v", events[0].PK, a.ID)
	}
}

// TestEventInput_HardDelete — same invariant as SoftDelete (landed nil).
func TestEventInput_HardDelete(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "HD-In", SKU: "EV-IN-HD-1", Price: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})
	if err := client.Products().HardDelete(ctx, p.ID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Input != nil {
		t.Errorf("HardDelete Event.Input = %v (type %T), want nil", events[0].Input, events[0].Input)
	}
	if events[0].PK != p.ID {
		t.Errorf("Event.PK = %v, want %v", events[0].PK, p.ID)
	}
}

// TestEventInput_Increment verifies Increment carries IncrementInput by
// value. See file header reconciliation (2).
func TestEventInput_Increment(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "INC-In", SKU: "EV-IN-INC-1", Price: 1.00, Stock: omittable.Set[int64](0),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	incInput := models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementStock, Amount: 7,
	}
	if err := client.Products().Increment(ctx, p.ID, incInput); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	// By-value assertion — caller never took a pointer (models_gen.go line
	// 3833). A value assertion preserves equality of Column + Amount.
	gotInput, ok := events[0].Input.(models.IncrementInput[models.ProductIncrementColumn])
	if !ok {
		t.Fatalf("Event.Input type = %T, want IncrementInput[ProductIncrementColumn] (by value, not pointer)", events[0].Input)
	}
	if gotInput.Column != incInput.Column || gotInput.Amount != incInput.Amount {
		t.Errorf("Event.Input = %+v, want %+v", gotInput, incInput)
	}
}

// TestEventInput_SoftDeleteMany pins the landed shape (PRD §28.6): per-event
// Input is nil — the PK already travels via Event.PK, so a redundant []int64
// payload would only repeat what Event.PK already carries. Mirrors the
// single-PK SoftDelete invariant.
func TestEventInput_SoftDeleteMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a1, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDM-In-1", Author: "t"})
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDM-In-2", Author: "t"})
	ids := []int64{a1.ID, a2.ID}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})
	if _, err := client.Articles().SoftDeleteMany(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, e := range events {
		if e.Input != nil {
			t.Errorf("event[%d].Input = %v (type %T), want nil (*Many delete carries PK on Event.PK; §28.6)",
				i, e.Input, e.Input)
		}
		if e.PK != ids[i] {
			t.Errorf("event[%d].PK = %v, want %v", i, e.PK, ids[i])
		}
	}
}

// TestEventInput_HardDeleteMany — same nil-Input shape as SoftDeleteMany.
func TestEventInput_HardDeleteMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDM-In-1", SKU: "EV-IN-HDM-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDM-In-2", SKU: "EV-IN-HDM-2", Price: 2})
	ids := []int64{p1.ID, p2.ID}

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})
	if err := client.Products().HardDeleteMany(ctx, ids); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, e := range events {
		if e.Input != nil {
			t.Errorf("event[%d].Input = %v (type %T), want nil", i, e.Input, e.Input)
		}
		if e.PK != ids[i] {
			t.Errorf("event[%d].PK = %v, want %v", i, e.PK, ids[i])
		}
	}
}

// TestEventInput_CreateMany pins the per-entity Input contract (PRD §28.6):
// each fanned-out event carries the i-th *CreateInput pointer the caller
// passed, NOT the full slice. Pointer equality verifies the i-th element
// arrives intact (the same value subscribers would type-switch on as
// *CreateProductInput regardless of single-op vs. batch).
func TestEventInput_CreateMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	inputs := []*models.CreateProductInput{
		{Name: "CM-In-1", SKU: "EV-IN-CM-1", Price: 1},
		{Name: "CM-In-2", SKU: "EV-IN-CM-2", Price: 2},
		{Name: "CM-In-3", SKU: "EV-IN-CM-3", Price: 3},
	}

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})
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
	if len(events) != len(inputs) {
		t.Fatalf("got %d events, want %d", len(events), len(inputs))
	}
	for i, e := range events {
		gotInput, ok := e.Input.(*models.CreateProductInput)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want *CreateProductInput (per-entity, not slice)", i, e.Input)
			continue
		}
		if gotInput != inputs[i] {
			t.Errorf("event[%d].Input pointer = %p, want %p (i-th element of caller's slice)", i, gotInput, inputs[i])
		}
	}
}

// TestEventInput_UpdateMany pins the per-entity Input contract for UpdateMany:
// each event carries the i-th UpdateProductItem (by value — items are not
// pointer-typed in the slice). The PK + per-row UpdateInput pair lets a
// subscriber correlate without re-indexing into the caller's slice.
func TestEventInput_UpdateMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UM-In-1", SKU: "EV-IN-UM-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UM-In-2", SKU: "EV-IN-UM-2", Price: 2})
	t.Cleanup(func() { _ = client.Products().HardDeleteMany(ctx, []int64{p1.ID, p2.ID}) })

	items := []models.UpdateProductItem{
		{ID: p1.ID, Input: &models.UpdateProductInput{Price: omittable.Set(10.0)}},
		{ID: p2.ID, Input: &models.UpdateProductInput{Price: omittable.Set(20.0)}},
	}

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})
	if _, err := client.Products().UpdateMany(ctx, items); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, e := range events {
		gotItem, ok := e.Input.(models.UpdateProductItem)
		if !ok {
			t.Errorf("event[%d].Input type = %T, want UpdateProductItem (per-entity value, not slice)", i, e.Input)
			continue
		}
		if gotItem.ID != items[i].ID {
			t.Errorf("event[%d].Input.ID = %d, want %d", i, gotItem.ID, items[i].ID)
		}
		if gotItem.Input != items[i].Input {
			t.Errorf("event[%d].Input.Input pointer = %p, want %p", i, gotItem.Input, items[i].Input)
		}
	}
}

// TestEventInput_AllVariantsSweep is a compact sanity sweep over every event
// variant. Strict type-assert against the
// generated input types (or nil for PK-only ops). A refactor that changes the
// per-event Input shape for any variant surfaces here.
//
// Includes CreateMany and UpdateMany so a regression of per-entity Input
// fanout (PRD §28.6) — e.g. silently re-attaching the full caller slice
// — surfaces here as the Create/Update arms reject the slice type.
func TestEventInput_AllVariantsSweep(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{})

	// Create
	createInput := &models.CreateProductInput{Name: "Sweep", SKU: "EV-IN-SW-1", Price: 1}
	p, err := client.Products().Create(ctx, createInput)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	// CreateMany (per-entity Input — each event carries inputs[i],
	// not the full []*CreateProductInput slice).
	cmInputs := []*models.CreateProductInput{
		{Name: "SweepCM-1", SKU: "EV-IN-SW-CM-1", Price: 1},
		{Name: "SweepCM-2", SKU: "EV-IN-SW-CM-2", Price: 2},
	}
	cmCreated, err := client.Products().CreateMany(ctx, cmInputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]int64, len(cmCreated))
		for i, p := range cmCreated {
			ids[i] = p.ID
		}
		_ = client.Products().HardDeleteMany(ctx, ids)
	})

	// Update
	updateInput := &models.UpdateProductInput{Name: omittable.Set("SweepUpd")}
	if _, err := client.Products().Update(ctx, p.ID, updateInput); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// UpdateMany (per-entity Input — each event carries items[i] as
	// an UpdateProductItem value, not the full []UpdateProductItem slice).
	umItems := []models.UpdateProductItem{
		{ID: cmCreated[0].ID, Input: &models.UpdateProductInput{Price: omittable.Set(11.0)}},
		{ID: cmCreated[1].ID, Input: &models.UpdateProductInput{Price: omittable.Set(22.0)}},
	}
	if _, err := client.Products().UpdateMany(ctx, umItems); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	// Upsert (will DO-UPDATE on sku conflict — p was created above)
	upsertInput := &models.CreateProductInput{Name: "SweepUpsert", SKU: "EV-IN-SW-1", Price: 99}
	if _, err := client.Products().Upsert(ctx, upsertInput, models.ProductConflictSKU); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// UpdateWhere
	uwInput := &models.UpdateProductInput{Name: omittable.Set("SweepUW")}
	if _, err := client.Products().UpdateWhere(
		ctx,
		&models.ProductFilter{ID: &comparator.Number[int64]{Eq: &p.ID}},
		uwInput,
	); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}

	// SoftDeleteWhere — use articles because products has no soft-delete
	a, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SwA", Author: "sw"})
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })
	author := "sw"
	sdwFilter := &models.ArticleFilter{Author: &comparator.String{Eq: &author}}
	if _, err := client.Articles().SoftDeleteWhere(ctx, sdwFilter); err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}

	// HardDeleteWhere — target a fresh row so p survives for cleanup.
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "SweepHDW", SKU: "EV-IN-SW-HDW", Price: 1})
	hdwFilter := &models.ProductFilter{ID: &comparator.Number[int64]{Eq: &p2.ID}}
	if err := client.Products().HardDeleteWhere(ctx, hdwFilter); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}

	// SoftDelete (single-PK — Input will be nil)
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SwA2", Author: "sd"})
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a2.ID) })
	if _, err := client.Articles().SoftDelete(ctx, a2.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// HardDelete (single-PK — Input will be nil). Use a fresh row.
	p3, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "SweepHD", SKU: "EV-IN-SW-HD", Price: 1})
	if err := client.Products().HardDelete(ctx, p3.ID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}

	events := got.snapshot()
	// Expected count: Create(p) + CreateMany(2) + Update + UpdateMany(2) +
	// Upsert + UpdateWhere + Create(a) + Create(p2) + SoftDeleteWhere(1) +
	// HardDeleteWhere(1) + Create(a2) + SoftDelete + Create(p3) + HardDelete
	// = 16.
	if len(events) != 16 {
		t.Fatalf("sweep: got %d events, want 16 (%+v)", len(events), actionCounts(events))
	}

	// Per-action type-assertion table. Walk events and for each action shape,
	// assert the expected concrete type. The Create arm covers single-op
	// AND CreateMany (per-entity Input); the Update arm covers single-op,
	// UpdateWhere, AND UpdateMany.
	for i, e := range events {
		switch e.Action {
		case event.Create:
			// Single-op Create AND CreateMany both carry *CreateXxxInput
			// at the per-entity Input slot (PRD §28.6). A regression
			// that re-attached []*CreateXxxInput would land in the default arm.
			switch e.Input.(type) {
			case *models.CreateProductInput, *models.CreateArticleInput:
				// ok
			default:
				t.Errorf("event[%d] Create Input type = %T, want *CreateProductInput or *CreateArticleInput", i, e.Input)
			}
		case event.Update:
			// Update / UpdateWhere → *UpdateXxxInput.
			// UpdateMany             → UpdateXxxItem (value).
			switch e.Input.(type) {
			case *models.UpdateProductInput, *models.UpdateArticleInput:
				// single-op + UpdateWhere
			case models.UpdateProductItem, models.UpdateArticleItem:
				// UpdateMany — per-entity item value
			default:
				t.Errorf("event[%d] Update Input type = %T, want *UpdateXxxInput or UpdateXxxItem", i, e.Input)
			}
		case event.Upsert:
			if _, ok := e.Input.(*models.CreateProductInput); !ok {
				t.Errorf("event[%d] Upsert Input type = %T, want *CreateProductInput", i, e.Input)
			}
		case event.Delete:
			// SoftDeleteWhere / HardDeleteWhere carry *Filter; single-PK
			// SoftDelete / HardDelete (and *Many delete-class) carry nil.
			switch e.Input.(type) {
			case *models.ArticleFilter, *models.ProductFilter:
				// *Where shape
			case nil:
				// single-PK + *Many delete-class shape
			default:
				t.Errorf("event[%d] Delete Input type = %T, want *Filter or nil", i, e.Input)
			}
		}
	}
}

// actionCounts is a tiny debug helper for the sweep test failure message.
func actionCounts(events []event.Event) map[event.Action]int {
	counts := make(map[event.Action]int)
	for _, e := range events {
		counts[e.Action]++
	}
	return counts
}
