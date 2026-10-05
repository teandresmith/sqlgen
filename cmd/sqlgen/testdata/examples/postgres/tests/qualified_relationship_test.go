package tests

// Declared relationships whose `table:` is schema-qualified, into
// public.notes / audit.notes — a pair whose bare name is ambiguous (PRD §5.5).
// Each edge must read the table its field's struct names. The two tables
// differ in their columns and key types, and the tests seed rows in the other
// schema, so a lookup that dropped the schema and took the first `notes` (or
// `users`) it found returns the wrong rows or fails, rather than passing.

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// countRows runs a `SELECT count(*)` query with one argument, bypassing the
// generated clients so a write is checked against the table it landed in.
func countRows(t *testing.T, query string, arg any) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), query, arg).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func createQualifiedRelUser(t *testing.T, client *models.Client, email string) *models.PublicUser {
	t.Helper()
	ctx := context.Background()
	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{Email: email, Name: "QualRel " + email})
	if err != nil {
		t.Fatalf("Create public user %s: %v", email, err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })
	return user
}

func createAuditNote(t *testing.T, client *models.Client, in *models.CreateAuditNoteInput) *models.AuditNote {
	t.Helper()
	ctx := context.Background()
	note, err := client.AuditNotes().Create(ctx, in)
	if err != nil {
		t.Fatalf("Create audit note: %v", err)
	}
	t.Cleanup(func() { _ = client.AuditNotes().HardDelete(ctx, note.ID) })
	return note
}

func createPublicNote(t *testing.T, client *models.Client, in *models.CreatePublicNoteInput) *models.PublicNote {
	t.Helper()
	ctx := context.Background()
	note, err := client.PublicNotes().Create(ctx, in)
	if err != nil {
		t.Fatalf("Create public note: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicNotes().HardDelete(ctx, note.ID) })
	return note
}

// TestQualifiedRelationship_O2M covers `Notes table: public.notes` and
// `AuditNotes table: audit.notes` on one parent: each loader and each
// relationship filter reads its own schema's table.
func TestQualifiedRelationship_O2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	user := createQualifiedRelUser(t, client, "qualrel-o2m@example.com")
	other := createQualifiedRelUser(t, client, "qualrel-o2m-other@example.com")

	pub := createPublicNote(t, client, &models.CreatePublicNoteInput{UserID: user.ID, Body: "qualrel-public-body"})
	aud := createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: user.ID, Severity: "qualrel-o2m-high"})
	// The other user's notes are in the opposite schemas, so a filter that
	// reads the wrong table matches the wrong user.
	createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: other.ID, Severity: "qualrel-public-body"})
	createPublicNote(t, client, &models.CreatePublicNoteInput{UserID: other.ID, Body: "qualrel-o2m-high"})

	// The writes land in the table each struct names.
	if got := countRows(t, "SELECT count(*) FROM public.notes WHERE user_id = $1", user.ID); got != 1 {
		t.Errorf("public.notes rows for user = %d, want 1", got)
	}
	if got := countRows(t, "SELECT count(*) FROM audit.notes WHERE user_id = $1", user.ID); got != 1 {
		t.Errorf("audit.notes rows for user = %d, want 1", got)
	}

	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{ID: &comparator.ID{Eq: new(user.ID.String())}},
	}, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
		o.FieldOptions = &models.PublicUserFieldOptions{
			ID:         true,
			Notes:      &models.PublicNoteRelationshipOptions{FieldOptions: &models.PublicNoteFieldOptions{ID: true, Body: true}},
			AuditNotes: &models.AuditNoteRelationshipOptions{FieldOptions: &models.AuditNoteFieldOptions{ID: true, Severity: true}},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with Notes and AuditNotes: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(users))
	}
	if n := users[0].Notes; len(n) != 1 || n[0].ID != pub.ID || n[0].Body != "qualrel-public-body" {
		t.Errorf("PublicUser.Notes = %+v, want the one public.notes row %v", n, pub.ID)
	}
	if n := users[0].AuditNotes; len(n) != 1 || n[0].ID != aud.ID || n[0].Severity != "qualrel-o2m-high" {
		t.Errorf("PublicUser.AuditNotes = %+v, want the one audit.notes row %d", n, aud.ID)
	}

	tests := []struct {
		name   string
		filter *models.PublicUserFilter
	}{
		{"AuditNotes", &models.PublicUserFilter{AuditNotes: &models.AuditNoteFilter{Severity: &comparator.String{Eq: new("qualrel-o2m-high")}}}},
		{"Notes", &models.PublicUserFilter{Notes: &models.PublicNoteFilter{Body: &comparator.String{Eq: new("qualrel-public-body")}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name+" filter", func(t *testing.T) {
			got, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{Filter: tt.filter})
			if err != nil {
				t.Fatalf("GetMany with %s relationship filter: %v", tt.name, err)
			}
			if len(got) != 1 || got[0].ID != user.ID {
				t.Errorf("%s relationship filter returned %d users, want exactly %v", tt.name, len(got), user.ID)
			}
		})
	}
}

// TestQualifiedRelationship_O2OHasOne covers `audit.events.Note table:
// audit.notes fk: event_id` and `SourceNote table: public.notes fk:
// source_event_id`. Each FK is on its target and exists only in that schema's
// `notes`, so an edge that read the other table would join the other way
// round.
func TestQualifiedRelationship_O2OHasOne(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	user := createQualifiedRelUser(t, client, "qualrel-has-one@example.com")

	event, err := client.Events().Create(ctx, &models.CreateEventInput{Name: "qualrel-has-one"})
	if err != nil {
		t.Fatalf("Create event: %v", err)
	}
	t.Cleanup(func() { _ = client.Events().HardDelete(ctx, event.ID) })

	// Notes on no event: a join run the wrong way round cannot pick them.
	createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: user.ID, Severity: "qualrel-has-one-unlinked"})
	createPublicNote(t, client, &models.CreatePublicNoteInput{UserID: user.ID, Body: "qualrel-has-one-unlinked"})
	aud := createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: user.ID, EventID: omittable.Set(&event.ID), Severity: "qualrel-has-one"})
	pub := createPublicNote(t, client, &models.CreatePublicNoteInput{UserID: user.ID, SourceEventID: omittable.Set(&event.ID), Body: "qualrel-has-one"})

	events, err := client.Events().GetMany(ctx, &models.GetEventsInput{
		Filter: &models.EventFilter{ID: &comparator.ID{Eq: new(event.ID.String())}},
	}, func(o *models.CallOptions[models.EventFieldOptions]) {
		o.FieldOptions = &models.EventFieldOptions{
			ID:         true,
			Note:       &models.AuditNoteFieldOptions{ID: true, Severity: true},
			SourceNote: &models.PublicNoteFieldOptions{ID: true, Body: true},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with Note and SourceNote: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("GetMany returned %d events, want 1", len(events))
	}
	if n := events[0].Note; n == nil || n.ID != aud.ID || n.Severity != "qualrel-has-one" {
		t.Errorf("Event.Note = %+v, want audit.notes row %d", n, aud.ID)
	}
	if n := events[0].SourceNote; n == nil || n.ID != pub.ID || n.Body != "qualrel-has-one" {
		t.Errorf("Event.SourceNote = %+v, want public.notes row %v", n, pub.ID)
	}
}

// TestQualifiedRelationship_O2OBelongsTo covers `audit.notes.Author table:
// public.users fk: user_id`: the FK is on the source, and an audit.users row
// shares the author's id.
func TestQualifiedRelationship_O2OBelongsTo(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	author := createQualifiedRelUser(t, client, "qualrel-belongs-to@example.com")

	decoy, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{ID: omittable.Set(author.ID), Action: "qualrel-decoy"})
	if err != nil {
		t.Fatalf("Create audit.users decoy: %v", err)
	}
	t.Cleanup(func() { _ = client.AuditUsers().HardDelete(ctx, decoy.ID) })

	aud := createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: author.ID, Severity: "qualrel-belongs-to"})

	notes, err := client.AuditNotes().GetMany(ctx, &models.GetAuditNotesInput{
		Filter: &models.AuditNoteFilter{ID: &comparator.Number[int64]{Eq: new(aud.ID)}},
	}, func(o *models.CallOptions[models.AuditNoteFieldOptions]) {
		o.FieldOptions = &models.AuditNoteFieldOptions{
			ID:     true,
			Author: &models.PublicUserFieldOptions{ID: true, Name: true, Email: true},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with Author: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("GetMany returned %d audit notes, want 1", len(notes))
	}
	a := notes[0].Author
	if a == nil || a.ID != author.ID || a.Name != author.Name || a.Email != author.Email {
		t.Errorf("AuditNote.Author = %+v, want public.users row %v (%q)", a, author.ID, author.Name)
	}
}

// TestQualifiedRelationship_M2M covers `WatchedAuditNotes table: audit.notes
// junction: note_watchers`: the loader and the relationship filter both
// reach audit.notes, whose bigint key the junction carries. The junction is
// bare and only `audit` declares it, so both must read it as
// audit.note_watchers rather than through the search path.
func TestQualifiedRelationship_M2M(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	watcher := createQualifiedRelUser(t, client, "qualrel-m2m@example.com")
	bystander := createQualifiedRelUser(t, client, "qualrel-m2m-bystander@example.com")

	watched := createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: bystander.ID, Severity: "qualrel-m2m-watched"})
	createAuditNote(t, client, &models.CreateAuditNoteInput{UserID: bystander.ID, Severity: "qualrel-m2m-unwatched"})
	createPublicNote(t, client, &models.CreatePublicNoteInput{UserID: watcher.ID, Body: "qualrel-m2m-watched"})

	link, err := client.NoteWatchers().Create(ctx, &models.CreateNoteWatcherInput{UserID: watcher.ID, NoteID: watched.ID})
	if err != nil {
		t.Fatalf("Create note watcher: %v", err)
	}
	t.Cleanup(func() {
		_ = client.NoteWatchers().HardDelete(ctx, models.NoteWatcherPK{UserID: link.UserID, NoteID: link.NoteID})
	})
	if got := countRows(t, "SELECT count(*) FROM audit.note_watchers WHERE user_id = $1", watcher.ID); got != 1 {
		t.Errorf("audit.note_watchers rows for watcher = %d, want 1", got)
	}

	users, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{ID: &comparator.ID{Eq: new(watcher.ID.String())}},
	}, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
		o.FieldOptions = &models.PublicUserFieldOptions{
			ID:                true,
			WatchedAuditNotes: &models.AuditNoteRelationshipOptions{FieldOptions: &models.AuditNoteFieldOptions{ID: true, Severity: true}},
		}
	})
	if err != nil {
		t.Fatalf("GetMany with WatchedAuditNotes: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetMany returned %d users, want 1", len(users))
	}
	if n := users[0].WatchedAuditNotes; len(n) != 1 || n[0].ID != watched.ID || n[0].Severity != "qualrel-m2m-watched" {
		t.Errorf("PublicUser.WatchedAuditNotes = %+v, want the one watched audit.notes row %d", n, watched.ID)
	}

	got, err := client.PublicUsers().GetMany(ctx, &models.GetPublicUsersInput{
		Filter: &models.PublicUserFilter{WatchedAuditNotes: &models.AuditNoteFilter{Severity: &comparator.String{Eq: new("qualrel-m2m-watched")}}},
	})
	if err != nil {
		t.Fatalf("GetMany with WatchedAuditNotes relationship filter: %v", err)
	}
	if len(got) != 1 || got[0].ID != watcher.ID {
		t.Errorf("WatchedAuditNotes relationship filter returned %d users, want exactly %v", len(got), watcher.ID)
	}
}
