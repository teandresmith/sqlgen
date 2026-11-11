package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestBoolKeyedRoundTrip drives a boolean primary key and foreign key through
// the paths that bind them: the read-back behind Create, Get, Update, the
// one-to-many loader and HardDelete. Both values run, since MySQL and SQLite
// read the text "false" differently from "true".
func TestBoolKeyedRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	for _, key := range []bool{true, false} {
		if _, err := client.BoolKeyeds().Create(ctx, &models.CreateBoolKeyedInput{K: key, Label: "a"}); err != nil {
			t.Fatalf("Create(%v): %v", key, err)
		}
		got, err := client.BoolKeyeds().Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%v): %v", key, err)
		}
		if got.Label != "a" {
			t.Errorf("Get(%v) label = %q, want %q", key, got.Label, "a")
		}
		if _, err := client.BoolKeyeds().Update(ctx, key, &models.UpdateBoolKeyedInput{Label: omittable.Set("b")}); err != nil {
			t.Fatalf("Update(%v): %v", key, err)
		}

		note, err := client.BoolKeyedNotes().Create(ctx, &models.CreateBoolKeyedNoteInput{KeyedK: omittable.Set(&key), Note: "n"})
		if err != nil {
			t.Fatalf("Create note(%v): %v", key, err)
		}
		rows, err := client.BoolKeyeds().GetMany(ctx, &models.GetBoolKeyedsInput{Filter: &models.BoolKeyedFilter{K: &comparator.Bool{Eq: new(key)}}}, func(o *models.CallOptions[models.BoolKeyedFieldOptions]) {
			o.FieldOptions = &models.BoolKeyedFieldOptions{K: true, BoolKeyedNotes: &models.BoolKeyedNoteRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany(%v) with notes: %v", key, err)
		}
		if len(rows) != 1 || len(rows[0].BoolKeyedNotes) != 1 {
			t.Fatalf("loader(%v): got %d rows, want 1 row with 1 note", key, len(rows))
		}
		if err := client.BoolKeyedNotes().HardDelete(ctx, note.ID); err != nil {
			t.Fatalf("HardDelete note(%v): %v", key, err)
		}
		if err := client.BoolKeyeds().HardDelete(ctx, key); err != nil {
			t.Fatalf("HardDelete(%v): %v", key, err)
		}
		if ok, err := client.BoolKeyeds().Exists(ctx, key); err != nil || ok {
			t.Fatalf("Exists(%v) after delete = %v, %v", key, ok, err)
		}
	}
}
