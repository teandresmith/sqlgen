package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Batch & Filter CRUD Operations ---

func TestCreateMany(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	inputs := []*models.CreateCategoryInput{
		{Name: "CreateMany1"},
		{Name: "CreateMany2"},
		{Name: "CreateMany3"},
	}
	created, err := client.Categories().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("CreateMany returned %d categories, want 3", len(created))
	}

	var ids []uuid.UUID
	for i, c := range created {
		ids = append(ids, c.ID)
		if c.ID == (uuid.UUID{}) {
			t.Errorf("CreateMany[%d]: expected non-empty ID", i)
		}
		if c.Name != inputs[i].Name {
			t.Errorf("CreateMany[%d].Name = %q, want %q", i, c.Name, inputs[i].Name)
		}
	}

	// Verify all exist
	for _, id := range ids {
		exists, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%s): %v", id, err)
		}
		if !exists {
			t.Errorf("Category %s should exist after CreateMany", id)
		}
	}

	t.Cleanup(func() { _ = client.Categories().HardDeleteMany(ctx, ids) })
}
