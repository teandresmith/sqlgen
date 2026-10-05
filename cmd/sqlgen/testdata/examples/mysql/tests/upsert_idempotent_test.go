package tests

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// An idempotent Upsert — same conflict key, same column values — must
// return the conflicting row, not ErrNotFound.
//
// MySQL's OK packet carries insert_id = 0 for an ON DUPLICATE KEY UPDATE that
// modifies no row, so resolving the PK from LastInsertId() used to yield 0 and
// send the follow-up Get looking for a row with a zero PK. The dialect now
// self-assigns the PK through LAST_INSERT_ID() so the statement republishes it.
//
// TestUpsert cannot catch this: it targets ProductConflictPK without supplying
// an id, so the AUTO_INCREMENT value is always fresh and neither call takes a
// conflict branch.
func TestUpsertIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "IdempotentCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	input := func() *models.CreateProductInput {
		return &models.CreateProductInput{
			CategoryID: cat.ID, Title: "IdempotentProduct", Price: 10.00,
			SKU: "IDEMPOTENT-001", Attributes: mustJSON(map[string]any{"v": float64(1)}),
		}
	}

	first, err := client.Products().Upsert(ctx, input(), models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, first.ID) })

	// The conflict branch that changes nothing — the regression.
	second, err := client.Products().Upsert(ctx, input(), models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert (idempotent) = %v, want nil (ErrNotFound=%v)", err, errors.Is(err, models.ErrNotFound))
	}
	if second.ID != first.ID {
		t.Errorf("Upsert (idempotent) ID = %d, want %d", second.ID, first.ID)
	}
	if second.Title != "IdempotentProduct" || second.Price != 10.00 {
		t.Errorf("Upsert (idempotent) = {%q, %v}, want {%q, %v}", second.Title, second.Price, "IdempotentProduct", 10.00)
	}

	// Control: the conflict branch that does change something still resolves
	// to the same row. This one passed before the fix.
	changed := input()
	changed.Price = 25.00
	third, err := client.Products().Upsert(ctx, changed, models.ProductConflictSKU)
	if err != nil {
		t.Fatalf("Upsert (changed): %v", err)
	}
	if third.ID != first.ID {
		t.Errorf("Upsert (changed) ID = %d, want %d", third.ID, first.ID)
	}
	if third.Price != 25.00 {
		t.Errorf("Upsert (changed) price = %v, want 25.00", third.Price)
	}
}

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
