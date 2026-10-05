package tests

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// TestDefaultOnlyRows_ZeroInput pins that a zero input on a table whose
// every writable column has a DEFAULT supplies no column, so the INSERT must
// take MySQL's all-defaults form. MySQL rejects `DEFAULT VALUES`, so the
// builder keeps `INSERT INTO t () VALUES ()` here.
func TestDefaultOnlyRows_ZeroInput(t *testing.T) {
	ctx := context.Background()
	c := newClient().DefaultOnlyRows()

	want := func(t *testing.T, op string, got *models.DefaultOnlyRow) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s returned a nil row", op)
		}
		if got.Label != "unset" || got.Hits != 0 || !got.Active {
			t.Errorf("%s row = {Label:%q Hits:%d Active:%v}, want the column defaults {unset 0 true}",
				op, got.Label, got.Hits, got.Active)
		}
	}

	created, err := c.Create(ctx, &models.CreateDefaultOnlyRowInput{})
	if err != nil {
		t.Fatalf("Create(zero input): %v", err)
	}
	want(t, "Create", created)

	upserted, err := c.Upsert(ctx, &models.CreateDefaultOnlyRowInput{}, models.DefaultOnlyRowConflictPK)
	if err != nil {
		t.Fatalf("Upsert(zero input): %v", err)
	}
	want(t, "Upsert", upserted)
	if upserted.ID == created.ID {
		t.Errorf("Upsert(zero input) returned id %d, the row Create wrote; want a new row", upserted.ID)
	}

	many, err := c.CreateMany(ctx, []*models.CreateDefaultOnlyRowInput{{}, {}})
	if err != nil {
		t.Fatalf("CreateMany(zero inputs): %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("CreateMany(zero inputs) returned %d rows, want 2", len(many))
	}
	for _, row := range many {
		want(t, "CreateMany", row)
	}
}
