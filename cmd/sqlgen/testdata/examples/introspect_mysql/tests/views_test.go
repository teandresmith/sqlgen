package tests

import (
	"context"
	"reflect"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/introspect_mysql/models"
)

// project_summaries selects projects.summary, which the database still has as
// nullable. migrations/ makes it NOT NULL with MODIFY, and the view is read
// again against the migrated table, so its field is non-nullable. @pk on the
// view gives it Get.
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
