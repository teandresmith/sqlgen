package tests

import (
	"context"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestTimeKeyedRoundTrip drives a time primary key and foreign key through the
// paths that bind them: the read-back behind Create, Get, the one-to-many
// loader and HardDelete. The key is outside UTC on purpose.
func TestTimeKeyedRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	at := time.Date(2026, 1, 1, 20, 4, 5, 123456000, time.FixedZone("MST", -7*3600))
	key := types.DateTime{Time: at}

	if _, err := client.TimeKeyeds().Create(ctx, &models.CreateTimeKeyedInput{At: key, Label: "a"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := client.TimeKeyeds().Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Label != "a" {
		t.Errorf("Get label = %q, want %q", got.Label, "a")
	}
	if _, err := client.TimeKeyeds().Update(ctx, key, &models.UpdateTimeKeyedInput{Label: omittable.Set("b")}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	note, err := client.TimeKeyedNotes().Create(ctx, &models.CreateTimeKeyedNoteInput{KeyedAt: omittable.Set(types.NullDateTime{Time: at, Valid: true}), Note: "n"})
	if err != nil {
		t.Fatalf("Create note: %v", err)
	}
	rows, err := client.TimeKeyeds().GetMany(ctx, &models.GetTimeKeyedsInput{}, func(o *models.CallOptions[models.TimeKeyedFieldOptions]) {
		o.FieldOptions = &models.TimeKeyedFieldOptions{At: true, TimeKeyedNotes: &models.TimeKeyedNoteRelationshipOptions{}}
	})
	if err != nil {
		t.Fatalf("GetMany with notes: %v", err)
	}
	if len(rows) != 1 || len(rows[0].TimeKeyedNotes) != 1 {
		t.Fatalf("loader: got %d rows, want 1 row with 1 note", len(rows))
	}

	if err := client.TimeKeyedNotes().HardDelete(ctx, note.ID); err != nil {
		t.Fatalf("HardDelete note: %v", err)
	}
	if err := client.TimeKeyeds().HardDelete(ctx, key); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if ok, err := client.TimeKeyeds().Exists(ctx, key); err != nil || ok {
		t.Fatalf("Exists after delete = %v, %v", ok, err)
	}
}
