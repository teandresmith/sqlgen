package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

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

	byID := make(map[int64]*models.Category, len(updated))
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

	updated, err := client.Categories().UpdateWhere(
		ctx,
		&models.CategoryFilter{ID: &comparator.Number[int64]{In: []int64{c1.ID, c2.ID}}},
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

func TestHardDeleteMany(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	c1, _ := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelMany1"})
	c2, _ := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelMany2"})
	c3, _ := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelMany3"})
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, c3.ID) })

	if err := client.Categories().HardDeleteMany(ctx, []int64{c1.ID, c2.ID}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}

	for _, id := range []int64{c1.ID, c2.ID} {
		exists, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%d): %v", id, err)
		}
		if exists {
			t.Errorf("Category %d should not exist after HardDeleteMany", id)
		}
	}

	exists, err := client.Categories().Exists(ctx, c3.ID)
	if err != nil {
		t.Fatalf("Exists(%d): %v", c3.ID, err)
	}
	if !exists {
		t.Error("c3 should still exist after HardDeleteMany of c1,c2")
	}
}

func TestHardDeleteWhere(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelWhere-Target-A"})
	if err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	c2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelWhere-Target-B"})
	if err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	c3, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "DelWhere-Keep"})
	if err != nil {
		t.Fatalf("Create c3: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, c3.ID) })

	if err := client.Categories().HardDeleteWhere(ctx, &models.CategoryFilter{
		ID: &comparator.Number[int64]{In: []int64{c1.ID, c2.ID}},
	}); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}

	for _, id := range []int64{c1.ID, c2.ID} {
		exists, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%d): %v", id, err)
		}
		if exists {
			t.Errorf("Category %d should not exist after HardDeleteWhere", id)
		}
	}

	exists, err := client.Categories().Exists(ctx, c3.ID)
	if err != nil {
		t.Fatalf("Exists(%d): %v", c3.ID, err)
	}
	if !exists {
		t.Error("c3 should still exist after HardDeleteWhere")
	}
}

func TestExistsWhere(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ExistsWhere-Yes"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, c1.ID) })

	matchName := "ExistsWhere-Yes"
	exists, err := client.Categories().ExistsWhere(ctx, &models.CategoryFilter{
		Name: &comparator.String{Eq: &matchName},
	})
	if err != nil {
		t.Fatalf("ExistsWhere matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere should return true for matching filter")
	}

	noMatch := "ExistsWhere-Nope"
	exists, err = client.Categories().ExistsWhere(ctx, &models.CategoryFilter{
		Name: &comparator.String{Eq: &noMatch},
	})
	if err != nil {
		t.Fatalf("ExistsWhere non-matching: %v", err)
	}
	if exists {
		t.Error("ExistsWhere should return false for non-matching filter")
	}
}

func TestCount(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	c1, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "Count-Target-A"})
	if err != nil {
		t.Fatalf("Create c1: %v", err)
	}
	c2, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "Count-Target-B"})
	if err != nil {
		t.Fatalf("Create c2: %v", err)
	}
	c3, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "Count-Other"})
	if err != nil {
		t.Fatalf("Create c3: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, c1.ID)
		_ = client.Categories().HardDelete(ctx, c2.ID)
		_ = client.Categories().HardDelete(ctx, c3.ID)
	})

	count, err := client.Categories().Count(ctx, &models.CategoryFilter{
		ID: &comparator.Number[int64]{In: []int64{c1.ID, c2.ID}},
	})
	if err != nil {
		t.Fatalf("Count matching: %v", err)
	}
	if count != 2 {
		t.Errorf("Count matching = %d, want 2", count)
	}

	noMatch := "Count-ZZZ-Nonexistent"
	count, err = client.Categories().Count(ctx, &models.CategoryFilter{
		Name: &comparator.String{Eq: &noMatch},
	})
	if err != nil {
		t.Fatalf("Count non-matching: %v", err)
	}
	if count != 0 {
		t.Errorf("Count non-matching = %d, want 0", count)
	}
}
