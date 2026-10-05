package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// PRD §28.6 / §28.9 / §32.3 — the UpsertMany event surface.
//
// None of the gaps these three pins cover would fail to compile:
// mapOpToAction, the §32.3 redaction switch and the cache's
// dispatchMutation all carry a permissive default, so an absent OpUpsertMany
// arm is silent (PRD §21.2). Concretely, before the arm existed:
//
//   - mapOpToAction fell through to event.Action(string(op)), minting a new
//     wire action "upsert_many" that no event.Upsert subscriber matched.
//   - The redaction switch fell through too, and its two defaults fail in
//     opposite directions: a non-fail-closed table published mc.Input, giving
//     every fanned-out event the entire batch slice; a fail-closed one
//     published nil.

// TestEventUpsertMany_ActionIsUpsert pins that OpUpsertMany maps to the
// existing event.Upsert action and introduces no new one — a subscriber
// already filtering event.Upsert receives batched upserts without changing its
// filter (PRD §28.9).
func TestEventUpsertMany_ActionIsUpsert(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	// Filtered on the action, not just the table: that is the half that fails
	// when mapOpToAction mints "upsert_many" instead.
	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Upsert},
	})

	inputs := []*models.CreateProductInput{
		{Name: "UM-a", SKU: "EV-UM-ACT-A", Price: 1},
		{Name: "UM-b", SKU: "EV-UM-ACT-B", Price: 2},
	}
	if _, err := client.Products().UpsertMany(ctx, inputs, models.ProductConflictSKU); err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}

	events := got.waitFor(t, len(inputs))
	if len(events) != len(inputs) {
		t.Fatalf("event count = %d, want %d", len(events), len(inputs))
	}
	for i, e := range events {
		if e.Action != event.Upsert {
			t.Errorf("event[%d].Action = %q, want %q (no new wire action)", i, e.Action, event.Upsert)
		}
	}
}

// TestEventUpsertMany_PerRowInput pins the §28.6 shape on a table with no
// redacted column: event i carries input i, not the whole slice.
func TestEventUpsertMany_PerRowInput(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	got := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"products"}})

	inputs := []*models.CreateProductInput{
		{Name: "UM-p0", SKU: "EV-UM-ROW-0", Price: 10},
		{Name: "UM-p1", SKU: "EV-UM-ROW-1", Price: 20},
		{Name: "UM-p2", SKU: "EV-UM-ROW-2", Price: 30},
	}
	if _, err := client.Products().UpsertMany(ctx, inputs, models.ProductConflictSKU); err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}

	events := got.waitFor(t, len(inputs))
	for i, e := range events {
		// A whole-slice payload asserts to []*CreateProductInput, so naming
		// the per-entity type is what distinguishes the two shapes.
		in, ok := e.Input.(*models.CreateProductInput)
		if !ok {
			t.Fatalf("event[%d].Input type = %T, want *models.CreateProductInput (per-row, not the batch slice)", i, e.Input)
		}
		if in.SKU != inputs[i].SKU {
			t.Errorf("event[%d].Input.SKU = %q, want %q — event i must carry input i", i, in.SKU, inputs[i].SKU)
		}
	}
}

// TestEventUpsertMany_PerRowInputAfterDedupe pins the alignment that makes the
// index-based fanout sound: UpsertMany publishes the *deduped* slice, not the
// caller's, because that is the one index-aligned with AffectedPKs (PRD §28.9).
//
// Inputs colliding on the conflict target collapse to one row, last occurrence
// winning (PRD §9.2), so the caller's slice is longer than the event count —
// the case where indexing the caller's slice by i would hand event 1 the wrong
// row's input.
func TestEventUpsertMany_PerRowInputAfterDedupe(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	got := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"products"}})

	// Three inputs, two distinct conflict keys → two written rows.
	inputs := []*models.CreateProductInput{
		{Name: "UM-d0", SKU: "EV-UM-DUP-0", Price: 1},
		{Name: "UM-d1", SKU: "EV-UM-DUP-1", Price: 2},
		{Name: "UM-d0-again", SKU: "EV-UM-DUP-0", Price: 99},
	}
	written, err := client.Products().UpsertMany(ctx, inputs, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written rows = %d, want 2 (dedupe by conflict target)", len(written))
	}

	events := got.waitFor(t, 2)
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2 (one per deduped row, not per caller input)", len(events))
	}

	// First-appearance order preserved, last occurrence's values winning.
	want := []struct {
		sku   string
		price float64
	}{
		{"EV-UM-DUP-0", 99},
		{"EV-UM-DUP-1", 2},
	}
	for i, e := range events {
		in, ok := e.Input.(*models.CreateProductInput)
		if !ok {
			t.Fatalf("event[%d].Input type = %T, want *models.CreateProductInput", i, e.Input)
		}
		if in.SKU != want[i].sku || in.Price != want[i].price {
			t.Errorf("event[%d].Input = {SKU:%q Price:%v}, want {SKU:%q Price:%v} — the published slice is the deduped one",
				i, in.SKU, in.Price, want[i].sku, want[i].price)
		}
	}
}

// TestEventUpsertMany_RedactedPerRowInput pins the §32.3 half on a fail-closed
// table: accounts redacts password_hash (internal) and recovery_code
// (write_only), so each event must carry its own row's input as a redacted
// clone — not nil, which is what the fail-closed default published before the
// OpUpsertMany arm existed.
func TestEventUpsertMany_RedactedPerRowInput(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	got := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"accounts"}})

	// The batch dedupes — inputs 0 and 2 share an email — so the redaction arm
	// must index the *deduped* slice too, not the caller's. Indexing the
	// caller's would hand event 1 input 1's row while the PK is row 1's, and
	// on a redacting table that mismatch is invisible in the payload's shape.
	const wantEvents = 2
	inputs := []*models.CreateAccountInput{
		{
			Email:        "um-redact-0@example.com",
			PasswordHash: "hash-secret-0",
			RecoveryCode: omittable.Set(new("rc-secret-0")),
		},
		{
			Email:        "um-redact-1@example.com",
			PasswordHash: "hash-secret-1",
			RecoveryCode: omittable.Set(new("rc-secret-1")),
			Note:         omittable.Set(new("public note 1")),
		},
		{
			Email:        "um-redact-0@example.com",
			PasswordHash: "hash-secret-0-again",
			RecoveryCode: omittable.Set(new("rc-secret-0-again")),
			Note:         omittable.Set(new("public note 0")),
		},
	}
	written, err := client.Accounts().UpsertMany(ctx, inputs, models.AccountConflictEmail)
	if err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}
	if len(written) != wantEvents {
		t.Fatalf("written rows = %d, want %d (dedupe by conflict target)", len(written), wantEvents)
	}

	// First-appearance order, last occurrence's values winning (PRD §9.2).
	wantEmail := []string{"um-redact-0@example.com", "um-redact-1@example.com"}
	wantNote := []string{"public note 0", "public note 1"}

	events := got.waitFor(t, wantEvents)
	if len(events) != wantEvents {
		t.Fatalf("event count = %d, want %d (one per deduped row)", len(events), wantEvents)
	}
	for i, e := range events {
		in, ok := e.Input.(*models.CreateAccountInput)
		if !ok {
			t.Fatalf("event[%d].Input type = %T, want *models.CreateAccountInput (fail-closed default must not publish nil)", i, e.Input)
		}
		// Per-row against the deduped slice, with the secrets cleared.
		assertRedactedCreate(t, in, wantEmail[i])
		if v, ok := in.Note.Get(); !ok || v == nil || *v != wantNote[i] {
			t.Errorf("event[%d].Input.Note = %v (set=%v), want %q — the published slice is the deduped one", i, v, ok, wantNote[i])
		}
	}

	// The callers' inputs are untouched: the un-redacted values went to the DB.
	for i, in := range inputs {
		if in.PasswordHash == "" || !in.RecoveryCode.IsSet() {
			t.Errorf("caller input %d was mutated by redaction: %+v", i, in)
		}
	}
}
