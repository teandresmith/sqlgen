package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// TestO2OTenantJoin_NullForeignKeyKeepsParent pins the pgx leg of where a
// tenanted o2o child's tenant filter goes: into the LEFT JOIN's ON (§29.10),
// so a task whose nullable cycle_id is NULL still loads, with Cycle nil
// (§13.2).
func TestO2OTenantJoin_NullForeignKeyKeepsParent(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	c := plainTenantClient(tenantA)

	cy, err := c.Cycles().Create(ctx, &models.CreateCycleInput{Name: "c1"})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	if _, err := c.Tasks().Create(ctx, &models.CreateTaskInput{CycleID: omittable.Set(&cy.ID), Title: "linked"}); err != nil {
		t.Fatalf("create linked task: %v", err)
	}
	free, err := c.Tasks().Create(ctx, &models.CreateTaskInput{Title: "free"})
	if err != nil {
		t.Fatalf("create free task: %v", err)
	}
	withCycle := func(o *models.CallOptions[models.TaskFieldOptions]) {
		o.FieldOptions = &models.TaskFieldOptions{ID: true, Title: true, Cycle: &models.CycleFieldOptions{ID: true}}
	}

	rows, err := c.Tasks().GetMany(ctx, &models.GetTasksInput{}, withCycle)
	if err != nil {
		t.Fatalf("GetMany tasks with Cycle: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Title] = r.Cycle != nil
	}
	if diff := cmp.Diff(map[string]bool{"linked": true, "free": false}, got); diff != "" {
		t.Errorf("tasks by title, cycle loaded (-want +got):\n%s", diff)
	}

	one, err := c.Tasks().Get(ctx, free.ID, withCycle)
	if errors.Is(err, models.ErrNotFound) {
		t.Fatalf("Get free task with Cycle: ErrNotFound, want the task with Cycle nil")
	}
	if err != nil {
		t.Fatalf("Get free task with Cycle: %v", err)
	}
	if one.Cycle != nil {
		t.Errorf("Cycle = %+v, want nil", one.Cycle)
	}
}
