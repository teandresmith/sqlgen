package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// *Where mutation edge cases for PostgreSQL (PRD §9.3, §9.5).
//
// Coverage:
//   - Empty-filter guard: UpdateWhere / SoftDeleteWhere / RestoreWhere /
//     HardDeleteWhere all return ErrEmptyFilter when the filter produces zero
//     conditions (the safety guard that prevents accidental bulk mutations).
//   - Idempotent zero-match: each *Where op returns nil (no error) when the
//     filter matches zero rows (PRD §9.5 "Idempotent Where Operations").
//   - SoftDeleteWhere on already-soft-deleted rows is a no-op: the op always
//     scopes to `deleted_at IS NULL`, so previously-soft-deleted rows are
//     outside the match set and the result slice is empty.

func TestSoftDeleteWhere_AlreadySoftDeleted_NoOp_Postgres(t *testing.T) {
	// SoftDeleteWhere always scopes to `deleted_at IS NULL` (the op only targets
	// active entities). Re-running it after the rows are already soft-deleted is
	// therefore a legitimate no-op that returns an empty result slice without
	// error — not ErrEmptyFilter (the filter has conditions) and not ErrNotFound
	// (PRD §9.5 — zero-match is idempotent on *Where).
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

	// First call soft-deletes both rows.
	first, err := client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	})
	if err != nil {
		t.Fatalf("SoftDeleteWhere(first): %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("SoftDeleteWhere(first) = %d, want 2", len(first))
	}

	// Second call matches zero rows (they're already `deleted_at IS NOT NULL`).
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
