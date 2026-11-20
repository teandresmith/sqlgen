package featuressuite

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
)

// events: each §28.4 operation publishes one event per affected row, and each
// event carries the §28.6 input. The writes run on notes (a database-generated
// key) and tasks (a soft delete), so MySQL fills the carriers by LastInsertId
// and pre-read where PostgreSQL and SQLite use RETURNING.
func events(t *testing.T, h *Harness) {
	t.Run("Create", func(t *testing.T) { eventsCreate(t, h) })
	t.Run("CreateMany", func(t *testing.T) { eventsCreateMany(t, h) })
	t.Run("Update", func(t *testing.T) { eventsUpdate(t, h) })
	t.Run("UpdateMany", func(t *testing.T) { eventsUpdateMany(t, h) })
	t.Run("UpdateWhere", func(t *testing.T) { eventsUpdateWhere(t, h) })
	t.Run("Upsert", func(t *testing.T) { eventsUpsert(t, h) })
	t.Run("Increment", func(t *testing.T) { eventsIncrement(t, h) })
	t.Run("SoftDelete", func(t *testing.T) { eventsSoftDelete(t, h) })
	t.Run("SoftDeleteMany", func(t *testing.T) { eventsSoftDeleteMany(t, h) })
	t.Run("SoftDeleteWhere", func(t *testing.T) { eventsSoftDeleteWhere(t, h) })
	t.Run("HardDelete", func(t *testing.T) { eventsHardDelete(t, h) })
	t.Run("HardDeleteMany", func(t *testing.T) { eventsHardDeleteMany(t, h) })
	t.Run("HardDeleteWhere", func(t *testing.T) { eventsHardDeleteWhere(t, h) })
	t.Run("Restore", func(t *testing.T) { eventsRestore(t, h) })
	t.Run("RestoreMany", func(t *testing.T) { eventsRestoreMany(t, h) })
	t.Run("RestoreWhere", func(t *testing.T) { eventsRestoreWhere(t, h) })

	t.Run("a batch mutation hands every event to one PublishBatch call", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		notes := env.Client.Notes()
		inputs := []*models.CreateNoteInput{{Slug: "a", Body: "a"}, {Slug: "b", Body: "b"}, {Slug: "c", Body: "c"}}
		created, err := notes.CreateMany(ctx, inputs)
		if err != nil {
			t.Fatalf("CreateMany() error = %v", err)
		}
		ids := noteIDs(created)
		if _, err := notes.UpdateMany(ctx, []models.UpdateNoteItem{
			{ID: ids[0], Input: &models.UpdateNoteInput{Body: omittable.Set("a2")}},
			{ID: ids[1], Input: &models.UpdateNoteInput{Body: omittable.Set("b2")}},
		}); err != nil {
			t.Fatalf("UpdateMany() error = %v", err)
		}
		if _, err := notes.UpdateWhere(ctx, &models.NoteFilter{ID: &comparator.Number[int64]{In: ids}},
			&models.UpdateNoteInput{Body: omittable.Set("all")}); err != nil {
			t.Fatalf("UpdateWhere() error = %v", err)
		}
		if err := notes.HardDeleteMany(ctx, ids); err != nil {
			t.Fatalf("HardDeleteMany() error = %v", err)
		}
		var sizes []int
		for _, b := range env.Recorder.Batches() {
			sizes = append(sizes, len(b))
		}
		// CreateMany, UpdateMany, UpdateWhere, HardDeleteMany: one call each.
		if want := []int{3, 2, 3, 3}; !slices.Equal(sizes, want) {
			t.Errorf("PublishBatch batch sizes = %v, want %v", sizes, want)
		}
		if p := env.Recorder.Published(); len(p) != 0 {
			t.Errorf("Publish called %d times, want 0: every event goes through PublishBatch", len(p))
		}
	})

	t.Run("a write that affects no row publishes nothing", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		notes, tasks := env.Client.Notes(), env.Client.Tasks()
		// A live task, a soft-deleted task and a note, none of which a write
		// below affects.
		live := createTasks(t, ctx, env, "live")[0]
		trashed := createTasks(t, ctx, env, "trashed")[0]
		if _, err := tasks.SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete(trashed) error = %v", err)
		}
		note := createNotes(t, ctx, env, "kept")[0]
		missing := max(live.ID, trashed.ID, note.ID) + 1000
		noTask := &models.TaskFilter{ID: &comparator.Number[int64]{Eq: &missing}}
		noNote := &models.NoteFilter{ID: &comparator.Number[int64]{Eq: &missing}}
		writes := []struct {
			name  string
			write func() error
		}{
			{"Update of a missing key", func() error {
				_, err := notes.Update(ctx, missing, &models.UpdateNoteInput{Body: omittable.Set("x")})
				return err
			}},
			{"UpdateMany of a missing key", func() error {
				_, err := notes.UpdateMany(ctx, []models.UpdateNoteItem{{ID: missing, Input: &models.UpdateNoteInput{Body: omittable.Set("x")}}})
				return err
			}},
			{"UpdateWhere matching no row", func() error {
				_, err := notes.UpdateWhere(ctx, noNote, &models.UpdateNoteInput{Body: omittable.Set("x")})
				return err
			}},
			{"Increment of a missing key", func() error {
				return notes.Increment(ctx, missing, models.IncrementInput[models.NoteIncrementColumn]{Column: models.NoteIncrementViews, Amount: 1})
			}},
			{"SoftDelete of a missing key", func() error { _, err := tasks.SoftDelete(ctx, missing); return err }},
			{"SoftDeleteMany of a missing key", func() error {
				_, err := tasks.SoftDeleteMany(ctx, []int64{missing})
				return err
			}},
			{"SoftDeleteWhere matching no row", func() error { _, err := tasks.SoftDeleteWhere(ctx, noTask); return err }},
			// SoftDeleteWhere only soft-deletes live rows.
			{"SoftDeleteWhere matching only a soft-deleted row", func() error {
				_, err := tasks.SoftDeleteWhere(ctx, &models.TaskFilter{ID: &comparator.Number[int64]{Eq: &trashed.ID}})
				return err
			}},
			{"Restore of a missing key", func() error { _, err := tasks.Restore(ctx, missing); return err }},
			{"RestoreMany of a missing key", func() error {
				_, err := tasks.RestoreMany(ctx, []int64{missing})
				return err
			}},
			{"RestoreWhere matching no row", func() error { _, err := tasks.RestoreWhere(ctx, noTask); return err }},
			// RestoreWhere only restores soft-deleted rows.
			{"RestoreWhere matching only a live row", func() error {
				_, err := tasks.RestoreWhere(ctx, &models.TaskFilter{ID: &comparator.Number[int64]{Eq: &live.ID}})
				return err
			}},
			{"HardDelete of a missing key", func() error { return notes.HardDelete(ctx, missing) }},
			{"HardDeleteMany of a missing key", func() error { return notes.HardDeleteMany(ctx, []int64{missing}) }},
			{"HardDeleteWhere matching no row", func() error { return notes.HardDeleteWhere(ctx, noNote) }},
		}
		for _, w := range writes {
			env.Recorder.Clear()
			// A strict Update and a single-row delete report the missing row;
			// either way the write affected nothing.
			if err := w.write(); err != nil && !errors.Is(err, models.ErrNotFound) {
				t.Errorf("%s: error = %v, want nil or ErrNotFound", w.name, err)
			}
			if b := env.Recorder.Batches(); len(b) != 0 {
				t.Errorf("%s: PublishBatch called with %v, want no call", w.name, b)
			}
			if p := env.Recorder.Published(); len(p) != 0 {
				t.Errorf("%s: Publish called with %v, want no call", w.name, p)
			}
		}
	})

	t.Run("a failed mutation publishes nothing", func(t *testing.T) {
		hookErr := errors.New("rejected after the write")
		failAfterWrite := func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, mc *hook.MutationContext) (any, error) {
				result, err := next(ctx, mc)
				if err == nil && mc.Op == hook.OpUpdateMany {
					return result, hookErr
				}
				return result, err
			}
		}
		env, ctx := eventEnv(t, h, WithMutationHook(failAfterWrite))
		note := createNotes(t, ctx, env, "dup")[0]
		env.Recorder.Clear()

		// (workspace_id, slug) is unique, so a second note with the slug fails.
		if _, err := env.Client.Notes().Create(ctx, &models.CreateNoteInput{Slug: "dup", Body: "again"}); err == nil {
			t.Fatal("Create() of a duplicate slug succeeded, want a constraint error")
		}
		if got := env.Recorder.Events(); len(got) != 0 {
			t.Errorf("a failed Create published %d events, want 0: %+v", len(got), got)
		}
		// The write runs and records its affected row; the inner hook then fails
		// the call.
		if _, err := env.Client.Notes().UpdateMany(ctx, []models.UpdateNoteItem{
			{ID: note.ID, Input: &models.UpdateNoteInput{Body: omittable.Set("changed")}},
		}); !errors.Is(err, hookErr) {
			t.Fatalf("UpdateMany() error = %v, want %v", err, hookErr)
		}
		if got := env.Recorder.Events(); len(got) != 0 {
			t.Errorf("a failed UpdateMany published %d events, want 0: %+v", len(got), got)
		}
	})
}

func eventsCreate(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n, err := env.Client.Notes().Create(ctx, &models.CreateNoteInput{Slug: "a", Body: "a"})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Create, n.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		input := &models.CreateNoteInput{Slug: "a", Body: "a"}
		if _, err := env.Client.Notes().Create(ctx, input); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		assertInputs(t, env.Recorder, input)
	})

	t.Run("a fire-and-forget create carries the generated key", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		got, err := env.Client.Notes().Create(ctx, &models.CreateNoteInput{Slug: "ff", Body: "ff"},
			func(o *models.CallOptions[models.NoteFieldOptions]) { o.FieldOptions = &models.NoteFieldOptions{} })
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if got != nil {
			t.Fatalf("fire-and-forget Create() = %+v, want nil", got)
		}
		rows, err := env.Client.Notes().GetMany(ctx, nil)
		if err != nil || len(rows) != 1 {
			t.Fatalf("GetMany() = %v, %v, want one row", rows, err)
		}
		assertEvents(t, env.Recorder, "notes", event.Create, rows[0].ID)
	})
}

func eventsCreateMany(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created, err := env.Client.Notes().CreateMany(ctx, []*models.CreateNoteInput{
			{Slug: "a", Body: "a"}, {Slug: "b", Body: "b"}, {Slug: "c", Body: "c"},
		})
		if err != nil {
			t.Fatalf("CreateMany() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Create, noteIDs(created)...)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		inputs := []*models.CreateNoteInput{{Slug: "a", Body: "a"}, {Slug: "b", Body: "b"}, {Slug: "c", Body: "c"}}
		created, err := env.Client.Notes().CreateMany(ctx, inputs)
		if err != nil {
			t.Fatalf("CreateMany() error = %v", err)
		}
		got := env.Recorder.Events()
		if len(got) != len(inputs) {
			t.Fatalf("got %d events, want %d", len(got), len(inputs))
		}
		for i, e := range got {
			if e.Input != any(inputs[i]) {
				t.Errorf("event %d: Input = %#v, want the caller's input %d", i, e.Input, i)
			}
			if e.PK != any(created[i].ID) {
				t.Errorf("event %d: PK = %v, want %d, the key created from input %d", i, e.PK, created[i].ID, i)
			}
		}
	})
}

func eventsUpdate(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a", "b")[0]
		env.Recorder.Clear()
		if _, err := env.Client.Notes().Update(ctx, n.ID, &models.UpdateNoteInput{Body: omittable.Set("changed")}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Update, n.ID)
	})

	t.Run("an update that leaves the row unchanged publishes its event", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a")[0]
		env.Recorder.Clear()
		// The row already holds this body: the key matched a row, which the
		// write leaves as it was.
		if _, err := env.Client.Notes().Update(ctx, n.ID, &models.UpdateNoteInput{Body: omittable.Set(n.Body)}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Update, n.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a")[0]
		env.Recorder.Clear()
		input := &models.UpdateNoteInput{Body: omittable.Set("changed")}
		if _, err := env.Client.Notes().Update(ctx, n.ID, input); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertInputs(t, env.Recorder, input)
	})
}

func eventsUpdateMany(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		missing := created[2].ID + 1000
		if _, err := env.Client.Notes().UpdateMany(ctx, []models.UpdateNoteItem{
			{ID: created[0].ID, Input: &models.UpdateNoteInput{Body: omittable.Set("a2")}},
			{ID: missing, Input: &models.UpdateNoteInput{Body: omittable.Set("gone")}},
			{ID: created[2].ID, Input: &models.UpdateNoteInput{Body: omittable.Set("c2")}},
		}); err != nil {
			t.Fatalf("UpdateMany() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Update, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		missing := created[1].ID + 1000
		items := []models.UpdateNoteItem{
			{ID: created[0].ID, Input: &models.UpdateNoteInput{Body: omittable.Set("a2")}},
			{ID: missing, Input: &models.UpdateNoteInput{Body: omittable.Set("gone")}},
			{ID: created[1].ID, Input: &models.UpdateNoteInput{Body: omittable.Set("b2")}},
		}
		if _, err := env.Client.Notes().UpdateMany(ctx, items); err != nil {
			t.Fatalf("UpdateMany() error = %v", err)
		}
		// The missing key wrote nothing, so the written items are 0 and 2.
		assertInputs(t, env.Recorder, items[0], items[2])
		for i, e := range env.Recorder.Events() {
			if item, ok := e.Input.(models.UpdateNoteItem); ok && e.PK != any(item.ID) {
				t.Errorf("event %d: PK = %v, but its item's key is %d", i, e.PK, item.ID)
			}
		}
	})
}

func eventsUpdateWhere(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		if _, err := env.Client.Notes().UpdateWhere(ctx,
			&models.NoteFilter{ID: &comparator.Number[int64]{In: []int64{created[0].ID, created[2].ID}}},
			&models.UpdateNoteInput{Body: omittable.Set("changed")}); err != nil {
			t.Fatalf("UpdateWhere() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Update, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		input := &models.UpdateNoteInput{Body: omittable.Set("changed")}
		if _, err := env.Client.Notes().UpdateWhere(ctx,
			&models.NoteFilter{ID: &comparator.Number[int64]{In: noteIDs(created)}}, input); err != nil {
			t.Fatalf("UpdateWhere() error = %v", err)
		}
		assertInputs(t, env.Recorder, input, input)
	})
}

func eventsUpsert(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		notes := env.Client.Notes()
		inserted, err := notes.Upsert(ctx, &models.CreateNoteInput{Slug: "a", Body: "first"}, models.NoteConflictWorkspaceIDSlug)
		if err != nil {
			t.Fatalf("Upsert() inserting error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Upsert, inserted.ID)

		env.Recorder.Clear()
		updated, err := notes.Upsert(ctx, &models.CreateNoteInput{Slug: "a", Body: "second"}, models.NoteConflictWorkspaceIDSlug)
		if err != nil {
			t.Fatalf("Upsert() updating error = %v", err)
		}
		if updated.ID != inserted.ID {
			t.Fatalf("Upsert() updating returned key %d, want the existing %d", updated.ID, inserted.ID)
		}
		assertEvents(t, env.Recorder, "notes", event.Upsert, inserted.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		insert := &models.CreateNoteInput{Slug: "a", Body: "first"}
		if _, err := env.Client.Notes().Upsert(ctx, insert, models.NoteConflictWorkspaceIDSlug); err != nil {
			t.Fatalf("Upsert() inserting error = %v", err)
		}
		assertInputs(t, env.Recorder, insert)

		env.Recorder.Clear()
		update := &models.CreateNoteInput{Slug: "a", Body: "second"}
		if _, err := env.Client.Notes().Upsert(ctx, update, models.NoteConflictWorkspaceIDSlug); err != nil {
			t.Fatalf("Upsert() updating error = %v", err)
		}
		assertInputs(t, env.Recorder, update)
	})
}

func eventsIncrement(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a", "b")[0]
		env.Recorder.Clear()
		if err := env.Client.Notes().Increment(ctx, n.ID,
			models.IncrementInput[models.NoteIncrementColumn]{Column: models.NoteIncrementViews, Amount: 2}); err != nil {
			t.Fatalf("Increment() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Update, n.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a")[0]
		env.Recorder.Clear()
		input := models.IncrementInput[models.NoteIncrementColumn]{Column: models.NoteIncrementViews, Amount: 2}
		if err := env.Client.Notes().Increment(ctx, n.ID, input); err != nil {
			t.Fatalf("Increment() error = %v", err)
		}
		assertInputs(t, env.Recorder, input)
	})
}

func eventsSoftDelete(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		task := createTasks(t, ctx, env, "a", "b")[0]
		env.Recorder.Clear()
		if _, err := env.Client.Tasks().SoftDelete(ctx, task.ID); err != nil {
			t.Fatalf("SoftDelete() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Delete, task.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		task := createTasks(t, ctx, env, "a")[0]
		env.Recorder.Clear()
		if _, err := env.Client.Tasks().SoftDelete(ctx, task.ID); err != nil {
			t.Fatalf("SoftDelete() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil)
	})
}

func eventsSoftDeleteMany(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTasks(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		if _, err := env.Client.Tasks().SoftDeleteMany(ctx, []int64{created[0].ID, created[2].ID}); err != nil {
			t.Fatalf("SoftDeleteMany() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Delete, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTasks(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		if _, err := env.Client.Tasks().SoftDeleteMany(ctx, taskIDs(created)); err != nil {
			t.Fatalf("SoftDeleteMany() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil, nil)
	})
}

func eventsSoftDeleteWhere(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTasks(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		if _, err := env.Client.Tasks().SoftDeleteWhere(ctx,
			&models.TaskFilter{ID: &comparator.Number[int64]{In: []int64{created[0].ID, created[2].ID}}}); err != nil {
			t.Fatalf("SoftDeleteWhere() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Delete, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTasks(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		filter := &models.TaskFilter{ID: &comparator.Number[int64]{In: taskIDs(created)}}
		if _, err := env.Client.Tasks().SoftDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("SoftDeleteWhere() error = %v", err)
		}
		assertInputs(t, env.Recorder, filter, filter)
	})
}

func eventsHardDelete(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a", "b")[0]
		env.Recorder.Clear()
		if err := env.Client.Notes().HardDelete(ctx, n.ID); err != nil {
			t.Fatalf("HardDelete() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Delete, n.ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		n := createNotes(t, ctx, env, "a")[0]
		env.Recorder.Clear()
		if err := env.Client.Notes().HardDelete(ctx, n.ID); err != nil {
			t.Fatalf("HardDelete() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil)
	})
}

func eventsHardDeleteMany(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		if err := env.Client.Notes().HardDeleteMany(ctx, []int64{created[0].ID, created[2].ID}); err != nil {
			t.Fatalf("HardDeleteMany() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Delete, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		if err := env.Client.Notes().HardDeleteMany(ctx, noteIDs(created)); err != nil {
			t.Fatalf("HardDeleteMany() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil, nil)
	})
}

func eventsHardDeleteWhere(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b", "c")
		env.Recorder.Clear()
		if err := env.Client.Notes().HardDeleteWhere(ctx,
			&models.NoteFilter{ID: &comparator.Number[int64]{In: []int64{created[0].ID, created[2].ID}}}); err != nil {
			t.Fatalf("HardDeleteWhere() error = %v", err)
		}
		assertEvents(t, env.Recorder, "notes", event.Delete, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createNotes(t, ctx, env, "a", "b")
		env.Recorder.Clear()
		filter := &models.NoteFilter{ID: &comparator.Number[int64]{In: noteIDs(created)}}
		if err := env.Client.Notes().HardDeleteWhere(ctx, filter); err != nil {
			t.Fatalf("HardDeleteWhere() error = %v", err)
		}
		assertInputs(t, env.Recorder, filter, filter)
	})
}

func eventsRestore(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a", "b")
		if _, err := env.Client.Tasks().Restore(ctx, created[0].ID); err != nil {
			t.Fatalf("Restore() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Update, created[0].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a")
		if _, err := env.Client.Tasks().Restore(ctx, created[0].ID); err != nil {
			t.Fatalf("Restore() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil)
	})
}

func eventsRestoreMany(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a", "b", "c")
		if _, err := env.Client.Tasks().RestoreMany(ctx, []int64{created[0].ID, created[2].ID}); err != nil {
			t.Fatalf("RestoreMany() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Update, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a", "b")
		if _, err := env.Client.Tasks().RestoreMany(ctx, taskIDs(created)); err != nil {
			t.Fatalf("RestoreMany() error = %v", err)
		}
		assertInputs(t, env.Recorder, nil, nil)
	})
}

func eventsRestoreWhere(t *testing.T, h *Harness) {
	t.Run("publishes one event per affected row", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a", "b", "c")
		if _, err := env.Client.Tasks().RestoreWhere(ctx,
			&models.TaskFilter{ID: &comparator.Number[int64]{In: []int64{created[0].ID, created[2].ID}}}); err != nil {
			t.Fatalf("RestoreWhere() error = %v", err)
		}
		assertEvents(t, env.Recorder, "tasks", event.Update, created[0].ID, created[2].ID)
	})

	t.Run("the event carries the documented input", func(t *testing.T) {
		env, ctx := eventEnv(t, h)
		created := createTrashedTasks(t, ctx, env, "a", "b")
		filter := &models.TaskFilter{ID: &comparator.Number[int64]{In: taskIDs(created)}}
		if _, err := env.Client.Tasks().RestoreWhere(ctx, filter); err != nil {
			t.Fatalf("RestoreWhere() error = %v", err)
		}
		assertInputs(t, env.Recorder, filter, filter)
	})
}

// eventEnv empties the tables and returns a client publishing to a Recorder,
// with a context carrying a fresh tenant.
func eventEnv(t *testing.T, h *Harness, opts ...Option) (*Env, context.Context) {
	t.Helper()
	h.Reset(t)
	env := h.NewEnv(t, append([]Option{WithRecorder()}, opts...)...)
	return env, WithTenant(context.Background(), models.NewTenant())
}

// createNotes creates one note per slug, then forgets the Create events.
func createNotes(t *testing.T, ctx context.Context, env *Env, slugs ...string) []*models.Note {
	t.Helper()
	inputs := make([]*models.CreateNoteInput, len(slugs))
	for i, s := range slugs {
		inputs[i] = &models.CreateNoteInput{Slug: s, Body: s}
	}
	created, err := env.Client.Notes().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany(notes %v) error = %v", slugs, err)
	}
	env.Recorder.Clear()
	return created
}

// createTasks creates one task per title, then forgets the Create events.
func createTasks(t *testing.T, ctx context.Context, env *Env, titles ...string) []*models.Task {
	t.Helper()
	inputs := make([]*models.CreateTaskInput, len(titles))
	for i, s := range titles {
		inputs[i] = &models.CreateTaskInput{Title: s}
	}
	created, err := env.Client.Tasks().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany(tasks %v) error = %v", titles, err)
	}
	env.Recorder.Clear()
	return created
}

// createTrashedTasks creates one soft-deleted task per title, then forgets the
// events.
func createTrashedTasks(t *testing.T, ctx context.Context, env *Env, titles ...string) []*models.Task {
	t.Helper()
	created := createTasks(t, ctx, env, titles...)
	if _, err := env.Client.Tasks().SoftDeleteMany(ctx, taskIDs(created)); err != nil {
		t.Fatalf("SoftDeleteMany(tasks %v) error = %v", titles, err)
	}
	env.Recorder.Clear()
	return created
}

func noteIDs(notes []*models.Note) []int64 {
	ids := make([]int64, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	return ids
}

func taskIDs(tasks []*models.Task) []int64 {
	ids := make([]int64, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}

// assertEvents checks that rec holds exactly one event per key in wantPKs, in
// any order, each with table and action, an ID and a timestamp.
func assertEvents(t *testing.T, rec *Recorder, table string, action event.Action, wantPKs ...int64) {
	t.Helper()
	got := rec.Events()
	var pks []string
	for i, e := range got {
		if e.Table != table || e.Action != action {
			t.Errorf("event %d: Table, Action = %q, %q, want %q, %q", i, e.Table, e.Action, table, action)
		}
		if e.ID == "" || e.Timestamp.IsZero() {
			t.Errorf("event %d: ID = %q, Timestamp = %v, want both set", i, e.ID, e.Timestamp)
		}
		pks = append(pks, fmt.Sprintf("%T(%v)", e.PK, e.PK))
	}
	var want []string
	for _, pk := range wantPKs {
		want = append(want, fmt.Sprintf("%T(%v)", pk, pk))
	}
	slices.Sort(pks)
	slices.Sort(want)
	if !slices.Equal(pks, want) {
		t.Errorf("event keys = %v, want %v (one event per affected row)", pks, want)
	}
}

// assertInputs checks that rec holds one event per entry of want, in order,
// each carrying that input: the same pointer, or an equal value.
func assertInputs(t *testing.T, rec *Recorder, want ...any) {
	t.Helper()
	got := rec.Events()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.Input != want[i] {
			t.Errorf("event %d: Input = %T %#v, want %T %#v", i, e.Input, e.Input, want[i], want[i])
		}
	}
}
