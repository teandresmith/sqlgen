package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
)

// --- Soft Delete: Articles (timestamp) ---

func TestArticleSoftDelete_Timestamp(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Soft Delete Article",
		Author: "tester",
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	if article.DeletedAt != nil {
		t.Fatal("newly created article should have nil DeletedAt")
	}

	// SoftDelete sets deleted_at to current time
	deleted, err := client.Articles().SoftDelete(ctx, article.ID)
	if err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if deleted.DeletedAt == nil {
		t.Fatal("SoftDelete should set DeletedAt")
	}
	if time.Since(*deleted.DeletedAt) > 10*time.Second {
		t.Errorf("DeletedAt should be recent, got %v", *deleted.DeletedAt)
	}

	// GetMany excludes soft-deleted rows by default
	results, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			ID: &comparator.ID{Eq: new(article.ID.String())},
		},
	})
	if err != nil {
		t.Fatalf("GetMany after soft delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("GetMany should exclude soft-deleted article, got %d results", len(results))
	}

	// Restore clears deleted_at to NULL
	restored, err := client.Articles().Restore(ctx, article.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Errorf("Restore should clear DeletedAt, got %v", restored.DeletedAt)
	}

	// After restore, GetMany returns the article again
	results, err = client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			ID: &comparator.ID{Eq: new(article.ID.String())},
		},
	})
	if err != nil {
		t.Fatalf("GetMany after restore: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany after restore = %d results, want 1", len(results))
	}

	// Cleanup
	if err := client.Articles().HardDelete(ctx, article.ID); err != nil {
		t.Fatalf("HardDelete cleanup: %v", err)
	}
}

func TestArticleSoftDelete_FilterOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "Filter Override Article",
		Author: "tester",
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}

	_, err = client.Articles().SoftDelete(ctx, article.ID)
	if err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Setting DeletedAt comparator on the filter overrides default scoping
	results, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			ID:        &comparator.ID{Eq: new(article.ID.String())},
			DeletedAt: &comparator.NullableTime{Null: new(false)},
		},
	})
	if err != nil {
		t.Fatalf("GetMany with DeletedAt override: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany with filter override = %d results, want 1 (soft-deleted row)", len(results))
	}

	// Cleanup
	if err := client.Articles().HardDelete(ctx, article.ID); err != nil {
		t.Fatalf("HardDelete cleanup: %v", err)
	}
}

func TestArticleSoftDeleteWhere(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	a1, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "SDW Article 1", Author: "alice",
	})
	if err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	a2, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "SDW Article 2", Author: "alice",
	})
	if err != nil {
		t.Fatalf("Create a2: %v", err)
	}
	a3, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "SDW Article 3", Author: "bob",
	})
	if err != nil {
		t.Fatalf("Create a3: %v", err)
	}

	// SoftDeleteWhere with filter — only alice's articles
	aliceAuthor := "alice"
	deleted, err := client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &aliceAuthor},
	})
	if err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("SoftDeleteWhere = %d results, want 2", len(deleted))
	}

	// Bob's article should still be visible
	bobAuthor := "bob"
	results, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{
			Author: &comparator.String{Eq: &bobAuthor},
		},
	})
	if err != nil {
		t.Fatalf("GetMany bob: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany bob = %d results, want 1", len(results))
	}

	// Cleanup
	for _, id := range []uuid.UUID{a1.ID, a2.ID, a3.ID} {
		_ = client.Articles().HardDelete(ctx, id)
	}
}

// --- Soft Delete: Tags (boolean) ---

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
	results, err := client.Tags().GetMany(ctx, &models.GetTagsInput{
		Filter: &models.TagFilter{
			ID: &comparator.ID{Eq: new(tag.ID.String())},
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
			ID: &comparator.ID{Eq: new(tag.ID.String())},
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
	for _, id := range []uuid.UUID{t1.ID, t2.ID, t3.ID} {
		if _, err := client.Tags().SoftDelete(ctx, id); err != nil {
			t.Fatalf("SoftDelete %s: %v", id, err)
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

	// "rw-other" should still be soft-deleted (not visible in default GetMany)
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
	for _, id := range []uuid.UUID{t1.ID, t2.ID, t3.ID} {
		_ = client.Tags().HardDelete(ctx, id)
	}
}
