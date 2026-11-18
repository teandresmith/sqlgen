package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestTagSoftDelete_Boolean(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	tag, err := client.Tags().Create(ctx, &models.CreateTagInput{
		Name: "sd-bool-tag",
	})
	if err != nil {
		t.Fatalf("Create tag: %v", err)
	}
	if tag.IsDeleted {
		t.Fatal("newly created tag should have IsDeleted=false")
	}

	// SoftDelete sets is_deleted to true
	deleted, err := client.Tags().SoftDelete(ctx, tag.ID)
	if err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if !deleted.IsDeleted {
		t.Error("SoftDelete should set IsDeleted=true")
	}

	// GetMany excludes soft-deleted rows by default
	idEq := tag.ID
	results, err := client.Tags().GetMany(ctx, &models.GetTagsInput{
		Filter: &models.TagFilter{
			ID: &comparator.Number[int64]{Eq: &idEq},
		},
	})
	if err != nil {
		t.Fatalf("GetMany after soft delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("GetMany should exclude soft-deleted tag, got %d results", len(results))
	}

	// Restore sets is_deleted to false
	restored, err := client.Tags().Restore(ctx, tag.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.IsDeleted {
		t.Error("Restore should set IsDeleted=false")
	}

	// After restore, GetMany returns the tag again
	results, err = client.Tags().GetMany(ctx, &models.GetTagsInput{
		Filter: &models.TagFilter{
			ID: &comparator.Number[int64]{Eq: &idEq},
		},
	})
	if err != nil {
		t.Fatalf("GetMany after restore: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany after restore = %d results, want 1", len(results))
	}

	// Cleanup
	if err := client.Tags().HardDelete(ctx, tag.ID); err != nil {
		t.Fatalf("HardDelete cleanup: %v", err)
	}
}

func TestTagRestoreWhere(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	t1, err := client.Tags().Create(ctx, &models.CreateTagInput{Name: "rw-tag-1"})
	if err != nil {
		t.Fatalf("Create t1: %v", err)
	}
	t2, err := client.Tags().Create(ctx, &models.CreateTagInput{Name: "rw-tag-2"})
	if err != nil {
		t.Fatalf("Create t2: %v", err)
	}
	t3, err := client.Tags().Create(ctx, &models.CreateTagInput{Name: "rw-other"})
	if err != nil {
		t.Fatalf("Create t3: %v", err)
	}

	// Soft delete all three
	for _, id := range []int64{t1.ID, t2.ID, t3.ID} {
		if _, err := client.Tags().SoftDelete(ctx, id); err != nil {
			t.Fatalf("SoftDelete %d: %v", id, err)
		}
	}

	// RestoreWhere — only tags matching "rw-tag-%" pattern
	rwPrefix := "rw-tag-%"
	restored, err := client.Tags().RestoreWhere(ctx, &models.TagFilter{
		Name: &comparator.String{Like: &rwPrefix},
	})
	if err != nil {
		t.Fatalf("RestoreWhere: %v", err)
	}
	if len(restored) != 2 {
		t.Errorf("RestoreWhere = %d results, want 2", len(restored))
	}

	// "rw-other" should still be soft-deleted
	otherName := "rw-other"
	results, err := client.Tags().GetMany(ctx, &models.GetTagsInput{
		Filter: &models.TagFilter{
			Name: &comparator.String{Eq: &otherName},
		},
	})
	if err != nil {
		t.Fatalf("GetMany rw-other: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("GetMany rw-other = %d results, want 0 (still soft-deleted)", len(results))
	}

	// Cleanup
	for _, id := range []int64{t1.ID, t2.ID, t3.ID} {
		_ = client.Tags().HardDelete(ctx, id)
	}
}
