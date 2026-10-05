package tests

// An empty batch must read nothing, not everything.
//
// The write half of every `*Many` mutation was always safe: an empty id set
// compiles to `WHERE 1 = 0`, so the UPDATE / DELETE matches no row. The bug was
// in the trailing entity re-fetch, which filters on the keys the call touched —
// and every generated key filter guards with `len(...) > 0` (comparator/id.go),
// so an empty set contributes *no condition at all*. Combined with the
// `Limit: new(0)` these calls pass, the re-fetch degraded to an unfiltered
// SELECT and the method returned the entire table as though it had just
// created / updated / restored those rows.
//
// Measured before the fix, on this example:
//
//	CreateMany(ctx, nil)     -> 1 entity,  SELECT "id", "is_deleted", "name" FROM "public"."tags" WHERE is_deleted = $1
//	UpdateMany(ctx, nil)     -> 1 entity,  (same SELECT)
//	RestoreMany(ctx, nil)    -> 1 entity,  UPDATE ... WHERE 1 = 0, then the same SELECT
//	SoftDeleteMany(ctx, nil) -> 0 entities only because no row was soft-deleted
//	HardDeleteMany(ctx, nil) -> safe; it has no re-fetch
//
// The guard is on len(), not on nil: a non-nil empty slice reaches the same
// path and is covered below. UpsertMany carries the same guard for the same
// reason.

import (
	"context"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
)

func TestEmptyBatch_ReadsNothingAndReturnsNothing(t *testing.T) {
	ctx := context.Background()

	// A row has to exist for the regression to be visible at all: the bug
	// returned whatever the table held, so an empty table would pass either way.
	plain := models.New(dbpgx.New(testPool))
	seed, err := plain.Tags().Create(ctx, &models.CreateTagInput{Name: "EmptyBatchSentinel"})
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	t.Cleanup(func() { _ = plain.Tags().HardDelete(ctx, seed.ID) })

	tests := []struct {
		name string
		call func(*models.Client) ([]*models.Tag, error)
	}{
		{"CreateMany nil", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().CreateMany(ctx, nil)
		}},
		{"CreateMany empty slice", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().CreateMany(ctx, []*models.CreateTagInput{})
		}},
		{"UpdateMany nil", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().UpdateMany(ctx, nil)
		}},
		{"UpdateMany empty slice", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().UpdateMany(ctx, []models.UpdateTagItem{})
		}},
		{"SoftDeleteMany nil", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().SoftDeleteMany(ctx, nil)
		}},
		{"RestoreMany nil", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().RestoreMany(ctx, nil)
		}},
		{"UpsertMany nil", func(c *models.Client) ([]*models.Tag, error) {
			return c.Tags().UpsertMany(ctx, nil, models.TagConflictName)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap := newCapturingQuerier(dbpgx.New(testPool))
			client := models.New(cap)

			cap.reset()
			got, err := tt.call(client)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if len(got) != 0 {
				t.Errorf("%s returned %d entities, want 0 — an empty batch affected no row, so it must report none", tt.name, len(got))
			}
			if sqls := cap.snapshot(); len(sqls) != 0 {
				t.Errorf("%s issued %d statements, want 0: %v", tt.name, len(sqls), sqls)
			}
		})
	}
}

// HardDeleteMany returns only an error, so it has no re-fetch to guard. Pinned
// so the asymmetry stays deliberate rather than looking like an omission.
func TestEmptyBatch_HardDeleteManyUnaffected(t *testing.T) {
	ctx := context.Background()
	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	cap.reset()
	if err := client.Tags().HardDeleteMany(ctx, nil); err != nil {
		t.Fatalf("HardDeleteMany(nil): %v", err)
	}
	// It still issues its DELETE ... WHERE 1 = 0, which matches no row. That is
	// pre-existing and harmless; what matters is that it reads nothing back.
	for _, sqlStr := range cap.snapshot() {
		if len(sqlStr) >= 6 && sqlStr[:6] == "SELECT" {
			t.Errorf("HardDeleteMany(nil) issued a SELECT: %q", sqlStr)
		}
	}
}
