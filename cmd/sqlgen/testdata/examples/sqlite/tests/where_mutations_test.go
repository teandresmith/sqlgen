package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// *Where mutation edge cases for SQLite (PRD §9.3, §9.5).
// Mirrors the postgres suite, dialect-adapted (int64 PKs).

func TestSoftDeleteWhere_AlreadySoftDeleted_NoOp_SQLite(t *testing.T) {
	// SoftDeleteWhere always scopes to `deleted_at IS NULL` (the op only targets
	// active entities). Re-running it after the rows are already soft-deleted is
	// a legitimate no-op that returns an empty result slice without error.
	ctx := context.Background()
	client := newClient()

	author := "sdw-already-sd-" + t.Name()
	a1, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A1", Author: author})
	if err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	a2, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "A2", Author: author})
	if err != nil {
		t.Fatalf("Create a2: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Articles().HardDelete(ctx, a1.ID)
		_ = client.Articles().HardDelete(ctx, a2.ID)
	})

	first, err := client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	})
	if err != nil {
		t.Fatalf("SoftDeleteWhere(first): %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("SoftDeleteWhere(first) = %d, want 2", len(first))
	}

	second, err := client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	})
	if err != nil {
		t.Fatalf("SoftDeleteWhere(second, already soft-deleted): %v", err)
	}
	if len(second) != 0 {
		t.Errorf("SoftDeleteWhere(second) = %d, want 0 (already soft-deleted is a no-op)", len(second))
	}
}
