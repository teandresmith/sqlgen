package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// PRD §28.6 / §32.3 — event payload redaction. accounts classifies
// password_hash as internal and recovery_code as write_only, so every
// published Event.Input must carry those fields cleared (required → zero,
// omittable → unset) while public fields and the concrete input type are
// preserved. The un-redacted input continues to the DB write — asserted by
// reading the row back through the core client.

func assertRedactedCreate(t *testing.T, in *models.CreateAccountInput, wantEmail string) {
	t.Helper()
	if in == nil {
		t.Fatal("Event.Input CreateAccountInput is nil")
	}
	if in.Email != wantEmail {
		t.Errorf("Event.Input.Email = %q, want %q (public field must survive redaction)", in.Email, wantEmail)
	}
	if in.PasswordHash != "" {
		t.Errorf("Event.Input.PasswordHash = %q, want cleared (internal → zero value)", in.PasswordHash)
	}
	if in.RecoveryCode.IsSet() {
		t.Errorf("Event.Input.RecoveryCode is set, want unset (write_only → omittable unset)")
	}
}

func TestEventRedaction_Create(t *testing.T) {
	client, bus := newClientWithBus(t)
	c := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"accounts"}})
	ctx := context.Background()

	callerInput := &models.CreateAccountInput{
		Email:        "redact-create@example.com",
		PasswordHash: "hash-secret-1",
		RecoveryCode: omittable.Set(new("rc-secret-1")),
		Note:         omittable.Set(new("public note")),
	}
	created, err := client.Accounts().Create(ctx, callerInput)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := c.waitFor(t, 1)
	in, ok := events[0].Input.(*models.CreateAccountInput)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.CreateAccountInput (subscriber type-switch must be unaffected)", events[0].Input)
	}
	assertRedactedCreate(t, in, "redact-create@example.com")
	if v, ok := in.Note.Get(); !ok || v == nil || *v != "public note" {
		t.Errorf("Event.Input.Note = %v (set=%v), want %q preserved", v, ok, "public note")
	}
	if in == callerInput {
		t.Error("Event.Input is the caller's pointer — redacted tables must publish a clone")
	}

	// The caller's input must be untouched, and the DB write un-redacted.
	if callerInput.PasswordHash != "hash-secret-1" || !callerInput.RecoveryCode.IsSet() {
		t.Errorf("caller's input was mutated by redaction: %+v", callerInput)
	}
	if events[0].PK == nil {
		t.Error("Event.PK is nil — PK must never be redacted")
	}
	got, err := client.Accounts().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PasswordHash != "hash-secret-1" {
		t.Errorf("DB PasswordHash = %q, want %q (redaction must not reach the write path)", got.PasswordHash, "hash-secret-1")
	}
	if got.RecoveryCode == nil || *got.RecoveryCode != "rc-secret-1" {
		t.Errorf("DB RecoveryCode = %v, want %q", got.RecoveryCode, "rc-secret-1")
	}
}

func TestEventRedaction_Update_And_UpdateWhere(t *testing.T) {
	client, bus := newClientWithBus(t)
	ctx := context.Background()

	created, err := client.Accounts().Create(ctx, &models.CreateAccountInput{
		Email: "redact-update@example.com", PasswordHash: "h0",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"accounts"}, Actions: []event.Action{event.Update}})

	if _, err := client.Accounts().Update(ctx, created.ID, &models.UpdateAccountInput{
		PasswordHash: omittable.Set("h1-secret"),
		Note:         omittable.Set(new("updated note")),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := c.waitFor(t, 1)
	in, ok := events[0].Input.(*models.UpdateAccountInput)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.UpdateAccountInput", events[0].Input)
	}
	if in.PasswordHash.IsSet() {
		t.Errorf("Update Event.Input.PasswordHash is set, want unset (internal → omittable unset)")
	}
	if v, ok := in.Note.Get(); !ok || v == nil || *v != "updated note" {
		t.Errorf("Update Event.Input.Note = %v (set=%v), want preserved", v, ok)
	}

	// UpdateWhere shares one input across the fanout — same redaction shape.
	uwInput := &models.UpdateAccountInput{PasswordHash: omittable.Set("h2-secret")}
	uwFilter := &models.AccountFilter{ID: &comparator.Number[int64]{In: []int64{created.ID}}}
	if _, err := client.Accounts().UpdateWhere(ctx, uwFilter, uwInput); err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	events = c.waitFor(t, 2)
	uwIn, ok := events[1].Input.(*models.UpdateAccountInput)
	if !ok {
		t.Fatalf("UpdateWhere Event.Input type = %T, want *models.UpdateAccountInput", events[1].Input)
	}
	if uwIn.PasswordHash.IsSet() {
		t.Errorf("UpdateWhere Event.Input.PasswordHash is set, want unset")
	}
	if uwIn == uwInput {
		t.Error("UpdateWhere Event.Input is the caller's pointer — must be a redacted clone")
	}
	if !uwInput.PasswordHash.IsSet() {
		t.Error("caller's UpdateWhere input was mutated by redaction")
	}
}

func TestEventRedaction_BatchShapes(t *testing.T) {
	client, bus := newClientWithBus(t)
	c := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"accounts"}})
	ctx := context.Background()

	createdMany, err := client.Accounts().CreateMany(ctx, []*models.CreateAccountInput{
		{Email: "redact-batch-1@example.com", PasswordHash: "bh1"},
		{Email: "redact-batch-2@example.com", PasswordHash: "bh2"},
	})
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	events := c.waitFor(t, 2)
	for i, e := range events {
		in, ok := e.Input.(*models.CreateAccountInput)
		if !ok {
			t.Fatalf("CreateMany event %d Input type = %T, want *models.CreateAccountInput", i, e.Input)
		}
		assertRedactedCreate(t, in, in.Email) // email varies; core check is redaction
		if in.Email == "" {
			t.Errorf("CreateMany event %d lost the public Email field", i)
		}
	}

	items := []models.UpdateAccountItem{
		{ID: createdMany[0].ID, Input: &models.UpdateAccountInput{PasswordHash: omittable.Set("bh1-new"), Email: omittable.Set("redact-batch-1b@example.com")}},
		{ID: createdMany[1].ID, Input: &models.UpdateAccountInput{PasswordHash: omittable.Set("bh2-new")}},
	}
	if _, err := client.Accounts().UpdateMany(ctx, items); err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}
	events = c.waitFor(t, 4)
	for _, e := range events[2:] {
		item, ok := e.Input.(models.UpdateAccountItem)
		if !ok {
			t.Fatalf("UpdateMany event Input type = %T, want models.UpdateAccountItem", e.Input)
		}
		if item.Input == nil {
			t.Fatal("UpdateMany event item.Input is nil")
		}
		if item.Input.PasswordHash.IsSet() {
			t.Errorf("UpdateMany event PasswordHash is set, want unset")
		}
		if item.ID == 0 {
			t.Error("UpdateMany event item lost its PK")
		}
	}
	// The caller's item inputs must be untouched (the clone is per-event).
	if !items[0].Input.PasswordHash.IsSet() || !items[1].Input.PasswordHash.IsSet() {
		t.Error("caller's UpdateMany item inputs were mutated by redaction")
	}

	// Upsert routes through the create-shape redaction.
	if _, err := client.Accounts().Upsert(ctx, &models.CreateAccountInput{
		Email: "redact-batch-1@example.com", PasswordHash: "bh1-upserted",
	}, models.AccountConflictEmail); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	events = c.waitFor(t, 5)
	upIn, ok := events[4].Input.(*models.CreateAccountInput)
	if !ok {
		t.Fatalf("Upsert event Input type = %T, want *models.CreateAccountInput", events[4].Input)
	}
	if upIn.PasswordHash != "" {
		t.Errorf("Upsert event PasswordHash = %q, want cleared", upIn.PasswordHash)
	}
}

// PRD §28.6 increment leg: incrementing an access-redacted numeric column
// publishes the IncrementInput with the delta blanked — subscribers see
// which column changed, never the amount. The DB write applies the real
// delta.
func TestEventRedaction_Increment(t *testing.T) {
	client, bus := newClientWithBus(t)
	ctx := context.Background()

	created, err := client.Accounts().Create(ctx, &models.CreateAccountInput{
		Email: "redact-inc@example.com", PasswordHash: "hi0",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"accounts"}, Actions: []event.Action{event.Update}})

	if err := client.Accounts().Increment(ctx, created.ID, models.IncrementInput[models.AccountIncrementColumn]{
		Column: models.AccountIncrementInternalScore,
		Amount: 7,
	}); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	events := c.waitFor(t, 1)
	in, ok := events[0].Input.(models.IncrementInput[models.AccountIncrementColumn])
	if !ok {
		t.Fatalf("Increment Event.Input type = %T, want models.IncrementInput[models.AccountIncrementColumn]", events[0].Input)
	}
	if in.Column != models.AccountIncrementInternalScore {
		t.Errorf("Increment Event.Input.Column = %q, want %q", in.Column, models.AccountIncrementInternalScore)
	}
	if in.Amount != 0 {
		t.Errorf("Increment Event.Input.Amount = %d, want 0 (internal column delta must be blanked)", in.Amount)
	}

	got, err := client.Accounts().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.InternalScore != 7 {
		t.Errorf("DB InternalScore = %d, want 7 (redaction must not reach the write path)", got.InternalScore)
	}
}

// PRD §28.6 same-pointer guarantee: a table with NO redacted column keeps
// publishing the caller's exact input pointer — no clone, no allocation.
func TestEventRedaction_UnredactedTableKeepsSamePointer(t *testing.T) {
	client, bus := newClientWithBus(t)
	c := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"products"}})
	ctx := context.Background()

	callerInput := &models.CreateProductInput{Name: "SamePtr", SKU: "EV-REDACT-PTR-1", Price: 1.0}
	if _, err := client.Products().Create(ctx, callerInput); err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := c.waitFor(t, 1)
	in, ok := events[0].Input.(*models.CreateProductInput)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.CreateProductInput", events[0].Input)
	}
	if in != callerInput {
		t.Error("unredacted table published a different pointer — §28.6 same-pointer guarantee regressed")
	}
}
