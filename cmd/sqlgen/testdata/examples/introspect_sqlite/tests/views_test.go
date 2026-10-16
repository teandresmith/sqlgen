package tests

import (
	"context"
	"testing"
)

// @pk on the view project_summaries gives it Get.
func TestViewGet(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	project := newProject(t, client, newAccount(t, client, "summary@example.com"), "summary")
	got, err := client.ProjectSummary().Get(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "summary" {
		t.Errorf("Slug = %q, want summary", got.Slug)
	}
}
