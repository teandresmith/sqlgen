package tests

import (
	"context"
	"reflect"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_postgres/models"
)

// project_summaries selects projects.summary, which the database still has as
// nullable. migrations/ sets it NOT NULL, and the view is read again against
// the migrated table, so its field is non-nullable.
func TestViewColumnNotNullAfterMigration(t *testing.T) {
	field, ok := reflect.TypeFor[models.ProjectSummary]().FieldByName("Summary")
	if !ok {
		t.Fatal("ProjectSummary has no Summary field")
	}
	if field.Type.Kind() != reflect.String {
		t.Errorf("ProjectSummary.Summary is %s, want string", field.Type)
	}

	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "summary@example.com"), "summary")
	got, err := client.ProjectSummary().Get(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Summary != project.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, project.Summary)
	}
}

// @pk on the materialized view project_task_counts gives it Get, and its
// unique index lets it refresh concurrently.
func TestMaterializedViewGet(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "counts@example.com"), "counts")
	newTask(t, client, project, &models.CreateTaskInput{Title: "One"})
	newTask(t, client, project, &models.CreateTaskInput{Title: "Two"})

	if err := client.ProjectTaskCount().RefreshConcurrently(ctx); err != nil {
		t.Fatalf("RefreshConcurrently: %v", err)
	}
	got, err := client.ProjectTaskCount().Get(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TaskCount != 2 {
		t.Errorf("TaskCount = %d, want 2", got.TaskCount)
	}
}
