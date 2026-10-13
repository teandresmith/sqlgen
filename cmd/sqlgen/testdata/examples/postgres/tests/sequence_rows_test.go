package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestSequenceRows_NonKeySequenceColumnsOptional covers serial and identity
// columns outside the key: an INSERT that leaves them out draws the next
// sequence value, and a set value is written as given.
func TestSequenceRows_NonKeySequenceColumnsOptional(t *testing.T) {
	ctx := context.Background()
	c := newClient().SequenceRows()

	created, err := c.Create(ctx, &models.CreateSequenceRowInput{Note: "unset"})
	if err != nil {
		t.Fatalf("Create(sequence columns unset): %v", err)
	}
	if created.Seq == 0 || created.Ticket == 0 || created.AuditNo == 0 {
		t.Errorf("Create(sequence columns unset) = {Seq:%d Ticket:%d AuditNo:%d}, want sequence values, not 0",
			created.Seq, created.Ticket, created.AuditNo)
	}

	set, err := c.Create(ctx, &models.CreateSequenceRowInput{
		Note:   "set",
		Seq:    omittable.Set[int32](1000),
		Ticket: omittable.Set[int64](2000),
	})
	if err != nil {
		t.Fatalf("Create(seq and ticket set): %v", err)
	}
	if set.Seq != 1000 || set.Ticket != 2000 {
		t.Errorf("Create(seq and ticket set) = {Seq:%d Ticket:%d}, want {1000 2000}", set.Seq, set.Ticket)
	}

	// Rows that differ in which columns they set send DEFAULT for the unset one.
	many, err := c.CreateMany(ctx, []*models.CreateSequenceRowInput{
		{Note: "many set", Seq: omittable.Set[int32](3000)},
		{Note: "many unset"},
	})
	if err != nil {
		t.Fatalf("CreateMany(mixed): %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("CreateMany(mixed) returned %d rows, want 2", len(many))
	}
	if many[0].Seq != 3000 {
		t.Errorf("CreateMany(mixed) row 0 Seq = %d, want 3000", many[0].Seq)
	}
	if many[1].Seq == 0 || many[1].Seq == 3000 {
		t.Errorf("CreateMany(mixed) row 1 Seq = %d, want a sequence value", many[1].Seq)
	}
}

// TestSequenceRows_UpsertManyKeepsSequenceColumns covers PRD §9.5: UpsertMany
// leaves non-key sequence columns out of its update half. Every row binds
// DEFAULT for an unset one, so setting them there would fail every call on the
// GENERATED ALWAYS column (SQLSTATE 428C9) and redraw the others on a conflict.
func TestSequenceRows_UpsertManyKeepsSequenceColumns(t *testing.T) {
	ctx := context.Background()
	c := newClient().SequenceRows()

	inserted, err := c.UpsertMany(ctx, []*models.CreateSequenceRowInput{
		{Code: omittable.Set(new("upsert-many-a")), Note: "first"},
	}, models.SequenceRowConflictCode)
	if err != nil {
		t.Fatalf("UpsertMany(insert): %v", err)
	}
	if len(inserted) != 1 {
		t.Fatalf("UpsertMany(insert) returned %d rows, want 1", len(inserted))
	}
	before := inserted[0]

	updated, err := c.UpsertMany(ctx, []*models.CreateSequenceRowInput{
		{Code: omittable.Set(new("upsert-many-a")), Note: "second"},
	}, models.SequenceRowConflictCode)
	if err != nil {
		t.Fatalf("UpsertMany(conflict): %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("UpsertMany(conflict) returned %d rows, want 1", len(updated))
	}
	after := updated[0]

	if after.Note != "second" {
		t.Errorf("UpsertMany(conflict) Note = %q, want %q", after.Note, "second")
	}
	if after.ID != before.ID || after.Seq != before.Seq || after.Ticket != before.Ticket || after.AuditNo != before.AuditNo {
		t.Errorf("UpsertMany(conflict) = {ID:%d Seq:%d Ticket:%d AuditNo:%d}, want the row's own {ID:%d Seq:%d Ticket:%d AuditNo:%d}",
			after.ID, after.Seq, after.Ticket, after.AuditNo, before.ID, before.Seq, before.Ticket, before.AuditNo)
	}
}
