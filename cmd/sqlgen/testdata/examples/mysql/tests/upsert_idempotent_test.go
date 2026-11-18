package tests

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// The same defect reached through the other trigger — a conflict
// target that covers every inserted column, so updateColumns is empty and the
// dialect emits the degenerate no-op set list. Upserting a category by name
// with no description supplies only `name`, which is the whole conflict target.
func TestUpsertIdempotent_EmptyUpdateColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	first, err := client.Categories().Upsert(ctx, &models.CreateCategoryInput{Name: "EmptyUpdateCat"}, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, first.ID) })

	second, err := client.Categories().Upsert(ctx, &models.CreateCategoryInput{Name: "EmptyUpdateCat"}, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("Upsert (idempotent) = %v, want nil (ErrNotFound=%v)", err, errors.Is(err, models.ErrNotFound))
	}
	if second.ID != first.ID {
		t.Errorf("Upsert (idempotent) ID = %d, want %d", second.ID, first.ID)
	}
	if second.Name != "EmptyUpdateCat" {
		t.Errorf("Upsert (idempotent) name = %q, want %q", second.Name, "EmptyUpdateCat")
	}
}
