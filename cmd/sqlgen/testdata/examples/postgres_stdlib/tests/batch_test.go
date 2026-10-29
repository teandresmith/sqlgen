package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

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

func TestUpdateMany(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdMany1"})
	if err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	c2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdMany2"})
	if err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, c1.ID)
		_ = client.Categories().HardDelete(ctx, c2.ID)
	})

	updated, err := client.Categories().UpdateMany(ctx, []models.UpdateCategoryItem{
		{ID: c1.ID, Input: &models.UpdateCategoryInput{Name: omittable.Set("UpdMany1-Changed")}},
		{ID: c2.ID, Input: &models.UpdateCategoryInput{Name: omittable.Set("UpdMany2-Changed")}},
	})
	if err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("UpdateMany returned %d, want 2", len(updated))
	}

	byID := make(map[uuid.UUID]*models.Category, len(updated))
	for _, u := range updated {
		byID[u.ID] = u
	}
	if byID[c1.ID].Name != "UpdMany1-Changed" {
		t.Errorf("c1 name = %q, want %q", byID[c1.ID].Name, "UpdMany1-Changed")
	}
	if byID[c2.ID].Name != "UpdMany2-Changed" {
		t.Errorf("c2 name = %q, want %q", byID[c2.ID].Name, "UpdMany2-Changed")
	}
}

func TestUpdateWhere(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Use distinct names (UNIQUE constraint on categories.name), filter by ID to target multiple rows
	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdWhere-Match-A"})
	if err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	c2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdWhere-Match-B"})
	if err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	c3, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdWhere-NoMatch"})
	if err != nil {
		t.Fatalf("Create c3: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, c1.ID)
		_ = client.Categories().HardDelete(ctx, c2.ID)
		_ = client.Categories().HardDelete(ctx, c3.ID)
	})

	// Update description (non-unique field) for c1 and c2 via ID filter
	updated, err := client.Categories().UpdateWhere(
		ctx,
		&models.CategoryFilter{ID: &comparator.ID{In: []string{c1.ID.String(), c2.ID.String()}}},
		&models.UpdateCategoryInput{Description: omittable.Set(new(string("updated-desc")))},
	)
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if len(updated) != 2 {
		t.Errorf("UpdateWhere returned %d, want 2", len(updated))
	}
	for _, u := range updated {
		if u.Description == nil || *u.Description != "updated-desc" {
			t.Errorf("UpdateWhere result description = %v, want %q", u.Description, "updated-desc")
		}
	}

	// c3 should be unchanged
	got3, err := client.Categories().Get(ctx, c3.ID)
	if err != nil {
		t.Fatalf("Get c3: %v", err)
	}
	if got3.Description != nil {
		t.Errorf("c3 description = %v, should be nil (unchanged)", got3.Description)
	}
}
