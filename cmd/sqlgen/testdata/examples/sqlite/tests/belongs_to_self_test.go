package tests

// A self-referential belongs-to holds its key on the row itself, so the
// LEFT JOIN reads target.id = this.previous_step_id and loads the previous
// step. Read the other way round, the first step loads its successor and the
// second loads nothing.

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestBelongsTo_SelfReferentialLoadsTheParent(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	first, err := client.Steps().Create(ctx, &models.CreateStepInput{Name: "first"})
	if err != nil {
		t.Fatalf("Create first step: %v", err)
	}
	t.Cleanup(func() { _ = client.Steps().HardDelete(ctx, first.ID) })
	second, err := client.Steps().Create(ctx, &models.CreateStepInput{Name: "second", PreviousStepID: omittable.Set(&first.ID)})
	if err != nil {
		t.Fatalf("Create second step: %v", err)
	}
	// The second step first: its key holds the first while it exists.
	t.Cleanup(func() { _ = client.Steps().HardDelete(ctx, second.ID) })

	get := func(id int64) *models.Step {
		t.Helper()
		got, err := client.Steps().Get(ctx, id, func(o *models.CallOptions[models.StepFieldOptions]) {
			o.FieldOptions = &models.StepFieldOptions{
				ID: true, Name: true, PreviousStepID: true,
				PreviousStep: &models.StepFieldOptions{ID: true, Name: true},
			}
		})
		if err != nil {
			t.Fatalf("Get step %d with its previous step: %v", id, err)
		}
		return got
	}

	if got := get(second.ID).PreviousStep; got == nil || got.ID != first.ID {
		t.Errorf("second step's previous step = %+v, want step %d (%q)", got, first.ID, "first")
	}
	if got := get(first.ID).PreviousStep; got != nil {
		t.Errorf("first step's previous step = %+v, want none", got)
	}
}
