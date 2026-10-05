package tests

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestKeyOnlyRows_NoWritableColumn pins a table with no writable column:
// key_only_rows has no column a caller can write, so every input is a zero
// input and no INSERT can name a writable column. CreateMany names the generated key with the dialect's
// default sentinel instead, since `() VALUES (), ()` is a syntax error on
// PostgreSQL and SQLite. Create and Upsert take the single-row all-defaults
// form, and Upsert, with no column to conflict on, is a plain insert.
func TestKeyOnlyRows_NoWritableColumn(t *testing.T) {
	ctx := context.Background()
	c := newClient().KeyOnlyRows()

	before, err := c.Count(ctx, nil)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}

	created, err := c.Create(ctx, &models.CreateKeyOnlyRowInput{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatalf("Create returned %+v, want a row with a generated id", created)
	}

	many, err := c.CreateMany(ctx, []*models.CreateKeyOnlyRowInput{{}, {}, {}})
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	seen := map[int64]bool{created.ID: true}
	if len(many) != 3 {
		t.Fatalf("CreateMany returned %d rows, want 3", len(many))
	}
	for _, row := range many {
		if seen[row.ID] {
			t.Errorf("CreateMany returned id %d twice or reused Create's id", row.ID)
		}
		seen[row.ID] = true
	}

	upserted, err := c.Upsert(ctx, &models.CreateKeyOnlyRowInput{}, models.KeyOnlyRowConflictPK)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted == nil || upserted.ID == 0 || seen[upserted.ID] {
		t.Fatalf("Upsert returned %+v, want a new row", upserted)
	}

	n, err := c.Count(ctx, nil)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n-before != 5 {
		t.Errorf("Count grew by %d, want 5 (Create, CreateMany x3, Upsert)", n-before)
	}
}
