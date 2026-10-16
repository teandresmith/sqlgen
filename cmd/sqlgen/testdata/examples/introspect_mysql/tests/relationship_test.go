package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_mysql/models"
)

// migrations/ adds the foreign key tasks.assignee_id -> accounts.id with
// ADD CONSTRAINT. The relationship inferred from it loads.
func TestRelationshipFromAddedForeignKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	account := newAccount(t, client, "assignee@example.com")
	project := newProject(t, client, account, "assigned")
	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Assigned", AssigneeID: omittable.Set(&account.ID)})

	got, err := client.Accounts().Get(ctx, account.ID, func(o *models.CallOptions[models.AccountFieldOptions]) {
		o.FieldOptions = &models.AccountFieldOptions{
			ID:    true,
			Tasks: &models.TaskRelationshipOptions{FieldOptions: &models.TaskFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].ID != task.ID {
		t.Errorf("Account.Tasks = %v, want the one assigned task %d", got.Tasks, task.ID)
	}
}

// task_labels, created by migrations/, has a foreign key into the
// introspected tasks table. The relationship it infers loads.
func TestRelationshipIntoNewTable(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "labelled@example.com"), "labelled")
	task := newTask(t, client, project, &models.CreateTaskInput{Title: "Labelled"})
	label, err := client.TaskLabels().Create(ctx, &models.CreateTaskLabelInput{TaskID: task.ID, Label: "docs"})
	if err != nil {
		t.Fatalf("Create label: %v", err)
	}
	t.Cleanup(func() { _ = client.TaskLabels().HardDelete(ctx, label.ID) })

	got, err := client.Tasks().Get(ctx, task.ID, func(o *models.CallOptions[models.TaskFieldOptions]) {
		o.FieldOptions = &models.TaskFieldOptions{
			ID:         true,
			TaskLabels: &models.TaskLabelRelationshipOptions{FieldOptions: &models.TaskLabelFieldOptions{ID: true, Label: true}},
		}
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.TaskLabels) != 1 || got.TaskLabels[0].Label != "docs" {
		t.Errorf("Task.TaskLabels = %v, want the one docs label", got.TaskLabels)
	}
}
