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
