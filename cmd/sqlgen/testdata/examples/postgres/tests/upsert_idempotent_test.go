package tests

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
	"github.com/teandresmith/sqlgen/omittable"
)

// An idempotent Upsert must return the conflicting row on a RETURNING
// dialect too.
//
// The MySQL manifestation (insert_id = 0) has no analogue here, but the second
// trigger does: when the conflict target covers every inserted column there is
// nothing left to assign, so the dialect emits DO NOTHING — and DO NOTHING
// returns no row, leaving RETURNING with no PK to scan. Upserting a category by
// name with no description supplies only `name`, which is the whole target.
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
		t.Errorf("Upsert (idempotent) ID = %v, want %v", second.ID, first.ID)
	}
	if second.Name != "EmptyUpdateCat" {
		t.Errorf("Upsert (idempotent) name = %q, want %q", second.Name, "EmptyUpdateCat")
	}
}

// Control for the case above: supplying a description leaves one column outside
// the conflict target, so the dialect emits DO UPDATE SET and RETURNING yields
// the row. This branch resolved the PK correctly before the fix — it pins the
// boundary between the two branches.
func TestUpsertIdempotent_NonEmptyUpdateColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	input := func() *models.CreateCategoryInput {
		return &models.CreateCategoryInput{
			Name:        "NonEmptyUpdateCat",
			Description: omittable.Set(new("same every time")),
		}
	}

	first, err := client.Categories().Upsert(ctx, input(), models.CategoryConflictName)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, first.ID) })

	second, err := client.Categories().Upsert(ctx, input(), models.CategoryConflictName)
	if err != nil {
		t.Fatalf("Upsert (idempotent) = %v, want nil", err)
	}
	if second.ID != first.ID {
		t.Errorf("Upsert (idempotent) ID = %v, want %v", second.ID, first.ID)
	}
}
