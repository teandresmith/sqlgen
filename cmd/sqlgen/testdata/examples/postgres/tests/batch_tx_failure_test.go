package tests

// Batch partial-failure × transaction atomicity (PostgreSQL).
//
// Exercises PRD §9.5 "Batch Failure Semantics":
//   - Outside a transaction, sub-batches are independent. When sub-batch N
//     fails, sub-batches 1..N-1 are already persisted. CreateMany returns the
//     error — documented non-atomic semantics.
//   - Inside a transaction, the sub-batch failure bubbles up through
//     WithTransaction, which rolls back the outer tx. Every row inserted by
//     every sub-batch is discarded — atomic at the DB level via the
//     enclosing transaction.
//
// Setup: the generator's default batchSize is 200 (PRD §4.6). To exercise the
// multi-sub-batch path we CreateMany with 201 inputs: sub-batch 1 inserts 200
// rows successfully, sub-batch 2 has 1 row whose `name` collides with a
// pre-seeded category, triggering the UNIQUE(name) constraint violation. No
// sqlgen.yml change is needed — staying test-only avoids regenerating goldens
// just for a batch-size override.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// Each test uses a distinct prefix for its category names so parallel test
// runs do not collide on the shared schema. Category names are globally
// unique (schema.sql:48).
const (
	batchNoTxPrefix   = "batch-notx-"
	batchInsidePrefix = "batch-intx-"
	batchCollideName  = "BATCH-COLLIDE-SENTINEL"
)

// seedCollisionRow inserts the row whose name every CreateMany attempt will
// trip against on its final input. Returns the ID for cleanup.
func seedCollisionRow(t *testing.T, ctx context.Context, client *models.Client) uuid.UUID {
	t.Helper()
	c, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: batchCollideName})
	if err != nil {
		t.Fatalf("seed collision row: %v", err)
	}
	return c.ID
}

// buildCreateManyCollisionInputs builds 201 inputs where inputs[200] uses
// the collision name. With batchSize=200 the split is [0:200] (200 inputs) +
// [200:201] (1 input) — the second sub-batch fails on UNIQUE(name).
func buildCreateManyCollisionInputs(prefix string) []*models.CreateCategoryInput {
	inputs := make([]*models.CreateCategoryInput, 201)
	for i := 0; i < 200; i++ {
		inputs[i] = &models.CreateCategoryInput{Name: fmt.Sprintf("%s%03d", prefix, i)}
	}
	inputs[200] = &models.CreateCategoryInput{Name: batchCollideName}
	return inputs
}

// nameLike builds a *comparator.String matching `name LIKE pattern`. Used
// for cleanup and for the post-test Count assertion.
func nameLike(pattern string) *comparator.String {
	p := pattern
	return &comparator.String{Like: &p}
}

// cleanupByPrefix removes every category whose name matches prefix + "%".
// Runs as a t.Cleanup — errors are logged but not fatal.
func cleanupByPrefix(t *testing.T, ctx context.Context, client *models.Client, prefix string) {
	t.Helper()
	if err := client.Categories().HardDeleteWhere(ctx, &models.CategoryFilter{
		Name: nameLike(prefix + "%"),
	}); err != nil {
		t.Logf("cleanup prefix %q: %v (non-fatal)", prefix, err)
	}
}

// TestBatch_OutsideTx_SubBatchFailurePersistsFirstBatch documents §9.5's
// non-atomic semantics: sub-batch 1 (200 rows) commits, sub-batch 2 fails on
// the unique-name violation, first-batch rows remain in the database.
func TestBatch_OutsideTx_SubBatchFailurePersistsFirstBatch(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	collideID := seedCollisionRow(t, ctx, client)
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, collideID) })
	t.Cleanup(func() { cleanupByPrefix(t, ctx, client, batchNoTxPrefix) })

	inputs := buildCreateManyCollisionInputs(batchNoTxPrefix)

	_, err := client.Categories().CreateMany(ctx, inputs)
	if err == nil {
		t.Fatal("CreateMany: want error from second sub-batch, got nil")
	}
	// The error is a unique-constraint violation surfaced through pgx. Not
	// required to be any specific sentinel, but must be non-nil and must NOT
	// be ErrNotFound (regression guard in case error mapping drifts).
	if errors.Is(err, database.ErrNotFound) {
		t.Errorf("CreateMany: unexpected ErrNotFound, want unique-violation; err = %v", err)
	}

	// §9.5 contract: first sub-batch (200 rows) persisted despite the
	// second sub-batch failing.
	count, err := client.Categories().Count(ctx, &models.CategoryFilter{
		Name: nameLike(batchNoTxPrefix + "%"),
	})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 200 {
		t.Errorf("post-failure Count = %d, want 200 (first sub-batch should persist per §9.5)", count)
	}
}

// TestBatch_InsideTx_SubBatchFailureRollsBackEverything documents the §9.5 +
// §18 composition: when CreateMany fails inside WithTransaction, the returned
// error propagates through fn, WithTransaction rolls back the outer tx, and
// every already-inserted sub-batch row is discarded at the DB level.
//
// This is the atomicity guarantee consumers get by wrapping batch mutations
// in a Tx — the sqlgen-level semantics are "best-effort"; DB-level atomicity
// comes from the enclosing transaction.
func TestBatch_InsideTx_SubBatchFailureRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	collideID := seedCollisionRow(t, ctx, client)
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, collideID) })
	t.Cleanup(func() { cleanupByPrefix(t, ctx, client, batchInsidePrefix) })

	inputs := buildCreateManyCollisionInputs(batchInsidePrefix)

	txErr := client.WithTx(ctx, "batch-atomicity", func(txCtx context.Context) error {
		_, err := client.Categories().CreateMany(txCtx, inputs)
		return err
	})
	if txErr == nil {
		t.Fatal("WithTx: want error from inner CreateMany, got nil")
	}

	// Every insert must be rolled back: neither the first-sub-batch rows nor
	// the collision row attempt should remain from the tx attempt.
	count, err := client.Categories().Count(ctx, &models.CategoryFilter{
		Name: nameLike(batchInsidePrefix + "%"),
	})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("post-tx-rollback Count = %d, want 0 (every sub-batch should be rolled back inside Tx)", count)
	}

	// The pre-seeded collision row is outside the tx — it must still exist.
	ok, err := client.Categories().Exists(ctx, collideID)
	if err != nil {
		t.Fatalf("Exists(collideID): %v", err)
	}
	if !ok {
		t.Error("pre-seed collision row vanished — tx rollback affected rows outside its scope")
	}
}
