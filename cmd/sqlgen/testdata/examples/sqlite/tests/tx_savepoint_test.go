package tests

// Savepoint nesting (SQLite).
//
// Exercises PRD §18.3 + §18.6 against SQLite via the stdlib driver:
//   - Nested WithTransaction calls transparently create savepoints.
//   - Inner rollback preserves the outer transaction and every outer-scope
//     mutation (before or after the inner block).
//   - Inner release + outer commit persists every mutation at both depths.
//
// Savepoint names use the [A-Za-z_][A-Za-z0-9_]* + non-reserved-word grammar
// validated by database.Tx.Begin; the underscored "sp_*" /
// "*_sp" forms below dodge the cross-dialect reserved-word set.
//
// ---------------------------------------------------------------------------
// PRD reconciliation (1 drift — not a code bug):
// ---------------------------------------------------------------------------
//
// PRD §18 table (line ~7428) documents SQLite's savepoint-rollback syntax as
// `ROLLBACK TRANSACTION TO SAVEPOINT`. The landed runtime emits
// `ROLLBACK TO SAVEPOINT` uniformly across all stdlib dialects (see
// database/transaction.go and database/stdlib/stdlib.go:102 comment). Both
// forms are valid SQLite — SQLite's grammar (https://sqlite.org/lang_savepoint.html)
// is `ROLLBACK [TRANSACTION] [TO [SAVEPOINT] savepoint-name]`, with the
// TRANSACTION keyword optional. The behavioural tests below pass on SQLite
// via modernc/sqlite, which proves the emitted SQL is accepted. No code change
// needed; the PRD table lists SQLite's fully-specified documented syntax,
// while the runtime emits the common shorter form that every dialect accepts.
// Cross-dialect unit tests in database/transaction_test.go pin the emitted
// SQL string to `SAVEPOINT`/`RELEASE SAVEPOINT`/`ROLLBACK TO SAVEPOINT`.

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// spName builds a CategoryFilter matching by the unique `name` column. Used
// for probing row presence when the PK of a rolled-back-savepoint row is not
// recoverable.
func spName(name string) *models.CategoryFilter {
	return &models.CategoryFilter{Name: &comparator.String{Eq: &name}}
}

// TestTxSavepoint_SQLite_InnerRollbackPreservesOuter mirrors the postgres
// variant. Exercises the `ROLLBACK TO SAVEPOINT` code path against SQLite.
func TestTxSavepoint_SQLite_InnerRollbackPreservesOuter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var outerAID, outerBID int64
	sentinel := errors.New("discard inner")

	err := client.WithTx(ctx, "sp_outer", func(outerCtx context.Context) error {
		a, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-sqlite-outer-A",
		})
		if err != nil {
			return err
		}
		outerAID = a.ID

		innerErr := client.WithTx(outerCtx, "sp_inner", func(innerCtx context.Context) error {
			if _, err := client.Categories().Create(innerCtx, &models.CreateCategoryInput{
				Name: "sp-sqlite-inner",
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(innerErr, sentinel) {
			t.Errorf("inner WithTx: err = %v, want sentinel", innerErr)
		}

		b, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-sqlite-outer-B",
		})
		if err != nil {
			return err
		}
		outerBID = b.ID
		return nil
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, outerAID)
		_ = client.Categories().HardDelete(ctx, outerBID)
	})

	for _, id := range []int64{outerAID, outerBID} {
		ok, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%d): %v", id, err)
		}
		if !ok {
			t.Errorf("outer row %d missing after savepoint rollback — outer tx lost on SQLite", id)
		}
	}

	exists, err := client.Categories().ExistsWhere(ctx, spName("sp-sqlite-inner"))
	if err != nil {
		t.Fatalf("ExistsWhere inner: %v", err)
	}
	if exists {
		t.Error("inner row persisted after savepoint rollback on SQLite — ROLLBACK TO SAVEPOINT failed")
	}
}

// TestTxSavepoint_SQLite_InnerCommitReleasesToOuter covers the `RELEASE
// SAVEPOINT` code path. Both rows must persist on outer commit.
func TestTxSavepoint_SQLite_InnerCommitReleasesToOuter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var outerID, innerID int64

	err := client.WithTx(ctx, "outer_release", func(outerCtx context.Context) error {
		o, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-sqlite-release-outer",
		})
		if err != nil {
			return err
		}
		outerID = o.ID

		return client.WithTx(outerCtx, "inner_release", func(innerCtx context.Context) error {
			i, err := client.Categories().Create(innerCtx, &models.CreateCategoryInput{
				Name: "sp-sqlite-release-inner",
			})
			if err != nil {
				return err
			}
			innerID = i.ID
			return nil
		})
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, outerID)
		_ = client.Categories().HardDelete(ctx, innerID)
	})

	for _, id := range []int64{outerID, innerID} {
		ok, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%d): %v", id, err)
		}
		if !ok {
			t.Errorf("row %d missing after nested commit on SQLite — RELEASE SAVEPOINT + COMMIT failed", id)
		}
	}
}
