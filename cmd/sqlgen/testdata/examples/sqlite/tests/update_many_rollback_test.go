package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// A stdlib UpdateMany runs each batch inside one transaction: when a later
// item in the batch fails, the earlier items' UPDATEs roll back with it.
func TestUpdateMany_FailingItemRollsBackBatch(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	first, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdManyRollback-First"})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdManyRollback-Second"})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	taken, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "UpdManyRollback-Taken"})
	if err != nil {
		t.Fatalf("Create taken: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDeleteMany(ctx, []int64{first.ID, second.ID, taken.ID})
	})

	// categories.name is UNIQUE, so the second item collides with taken.Name
	// after the first item's UPDATE has already executed.
	_, err = client.Categories().UpdateMany(ctx, []models.UpdateCategoryItem{
		{ID: first.ID, Input: &models.UpdateCategoryInput{Name: omittable.Set("UpdManyRollback-First-Changed")}},
		{ID: second.ID, Input: &models.UpdateCategoryInput{Name: omittable.Set(taken.Name)}},
	})
	if err == nil {
		t.Fatal("UpdateMany: want a UNIQUE violation from the second item, got nil")
	}

	got, err := client.Categories().Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if got.Name != first.Name {
		t.Errorf("first.Name after failed UpdateMany = %q, want %q (the batch should have rolled back)", got.Name, first.Name)
	}
}
