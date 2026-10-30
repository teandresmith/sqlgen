package suite

// Helpers in this file take the prefix us.

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// upsert is the Upsert contract.
func upsert(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the row it inserts when the input soft-deletes it", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			UpdatedAt: omittable.Set(models.Now()),
			DeletedAt: omittable.Set(models.DeletedAt(models.Now())),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(new c, DeletedAt set) error = %v, want nil", err)
		}
		if got == nil || got.Name != "c" || !usCounterDeleted(got) {
			t.Fatalf("Upsert(new c, DeletedAt set) = %+v, want c with DeletedAt set", got)
		}
		if diff := cmp.Diff([]string{"c"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after Upsert (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 1)
	})

	t.Run("returns a soft-deleted row its conflict updates", func(t *testing.T) {
		h.Reset(t)
		usSeedDeletedCounter(t, h, "c")
		got, err := h.Client.Counters().Upsert(ctx, &models.CreateCounterInput{
			Name:      "c",
			Hits:      omittable.Set(int64(3)),
			UpdatedAt: omittable.Set(models.Now()),
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("Upsert(soft-deleted c, Hits 3) error = %v, want nil", err)
		}
		if got == nil || got.Name != "c" || got.Hits != 3 || !usCounterDeleted(got) {
			t.Fatalf("Upsert(soft-deleted c, Hits 3) = %+v, want c with Hits 3 and DeletedAt set", got)
		}
		if diff := cmp.Diff([]string{"c"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after Upsert (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 1)
	})
}

func usCounterDeleted(c *models.Counter) bool {
	return !cmp.Equal(c.DeletedAt, models.Counter{}.DeletedAt)
}

// usDeletedCounterNames returns the names of the soft-deleted counters, sorted.
func usDeletedCounterNames(t *testing.T, h *Harness) []string {
	t.Helper()
	counters, err := h.Client.Counters().GetMany(context.Background(), &models.GetCountersInput{
		Filter: &models.CounterFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}},
	})
	if err != nil {
		t.Fatalf("Counters().GetMany(DeletedAt IS NOT NULL) error = %v", err)
	}
	names := make([]string, 0, len(counters))
	for _, c := range counters {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	return names
}

// usSeedDeletedCounter creates a counter and soft-deletes it.
func usSeedDeletedCounter(t *testing.T, h *Harness, name string) {
	t.Helper()
	ctx := context.Background()
	cn, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())})
	if err != nil {
		t.Fatalf("Create counter %q: %v", name, err)
	}
	if _, err := h.Client.Counters().SoftDelete(ctx, cn.ID); err != nil {
		t.Fatalf("SoftDelete counter %q: %v", name, err)
	}
}

// upsertMany is the UpsertMany contract.
func upsertMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("keys the database matches as one fail the call rather than return short", func(t *testing.T) {
		h.Reset(t)
		createLabel(t, h, "red")
		client, affected := affectedPKsRecorder(h)
		got, err := client.Labels().UpsertMany(ctx, []*models.CreateLabelInput{{Name: "RED"}, {Name: "Red"}, {Name: "blue"}}, models.LabelConflictPK)
		if err == nil {
			t.Errorf("UpsertMany([RED, Red, blue]) over stored red = %d entities and no error (AffectedPKs %v), want an error: the database matches RED and Red as one key", len(got), affected())
		}
	})

	t.Run("conflict values the database matches as one return one entity per deduped input or fail loudly", func(t *testing.T) {
		h.Reset(t)
		now := models.Now()
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "visits", UpdatedAt: omittable.Set(now)},
			{Name: "VISITS", UpdatedAt: omittable.Set(now)},
		}, models.CounterConflictName)
		if err == nil && len(got) != 2 {
			t.Errorf("UpsertMany([visits, VISITS]) by name = %d entities and no error, want 2 entities or an error", len(got))
		}
	})

	t.Run("returns every row it writes, soft-deleted ones included", func(t *testing.T) {
		h.Reset(t)
		got, err := h.Client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
			{Name: "live", UpdatedAt: omittable.Set(models.Now())},
			{Name: "deleted", UpdatedAt: omittable.Set(models.Now()), DeletedAt: omittable.Set(models.DeletedAt(models.Now()))},
		}, models.CounterConflictName)
		if err != nil {
			t.Fatalf("UpsertMany([live, deleted]) error = %v, want nil", err)
		}
		names := make([]string, 0, len(got))
		for _, c := range got {
			names = append(names, c.Name)
		}
		slices.Sort(names)
		if diff := cmp.Diff([]string{"deleted", "live"}, names); diff != "" {
			t.Errorf("UpsertMany([live, deleted]) returned names (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"deleted"}, usDeletedCounterNames(t, h)); diff != "" {
			t.Errorf("soft-deleted counters after UpsertMany (-want +got):\n%s", diff)
		}
		assertRowCount(t, h, "counters", 2)
	})
}
