package tests

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

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

	var ids []int64
	for i, c := range created {
		ids = append(ids, c.ID)
		if c.ID == 0 {
			t.Errorf("CreateMany[%d]: expected non-zero ID", i)
		}
		if c.Name != inputs[i].Name {
			t.Errorf("CreateMany[%d].Name = %q, want %q", i, c.Name, inputs[i].Name)
		}
	}

	for _, id := range ids {
		exists, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%d): %v", id, err)
		}
		if !exists {
			t.Errorf("Category %d should exist after CreateMany", id)
		}
	}

	t.Cleanup(func() { _ = client.Categories().HardDeleteMany(ctx, ids) })
}
