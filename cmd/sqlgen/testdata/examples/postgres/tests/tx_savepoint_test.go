package tests

// Savepoint nesting (PostgreSQL).
//
// Exercises PRD §18.3 + §18.6:
//   - Nested WithTransaction calls transparently create savepoints on pgx.
//   - Inner rollback preserves the outer transaction and every mutation
//     performed in the outer scope before/after the inner block.
//   - Inner commit (RELEASE SAVEPOINT) promotes inner mutations into the
//     outer transaction; they persist iff the outer also commits.
//
// Savepoint names use the [A-Za-z_][A-Za-z0-9_]* + non-reserved-word grammar
// validated by database.Tx.Begin; the "sp_outer" / "sp_inner" /
// "*_release" / "*_abort" forms below dodge the cross-dialect reserved-word
// set ("inner", "outer", etc.) that triggered the original cryptic syntax
// errors on pg ("syntax error at or near \"inner\"").
//
// Behaviour is asserted via post-commit row state, not SQL string capture —
// pgx issues savepoint SQL internally through its native Tx.Begin API
// (see database/pgx/pgx.go and the savepoint support in database/transaction.go),
// so intercepting the exact SQL string would require wrapping the TxConn. The
// row-state assertions are load-bearing for the invariant under test and
// survive both a pgx-internal refactor and an SQL-shape change.

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// spName builds a CategoryFilter that matches by exact name. Used by the
// savepoint tests to probe for row presence without knowing the generated PK,
// since rows created inside a rolled-back savepoint have no recoverable ID.
func spName(name string) *models.CategoryFilter {
	return &models.CategoryFilter{Name: &comparator.String{Eq: &name}}
}

// TestTxSavepoint_InnerRollbackPreservesOuter seeds a category in the outer
// tx, creates a second category inside a nested tx, rolls the inner back,
// then creates a third category in the outer scope after the inner returned
// its error. Outer commit must persist categories 1 and 3 but not 2.
//
// This mirrors the PRD §18.3 example where an inner "add_items" savepoint
// can fail without invalidating the outer "checkout" transaction.
func TestTxSavepoint_InnerRollbackPreservesOuter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var outerAID, outerBID uuid.UUID

	sentinel := errors.New("discard inner")
	err := client.WithTx(ctx, "sp_outer", func(outerCtx context.Context) error {
		// Outer mutation #1 — must survive inner rollback.
		a, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-outer-A",
		})
		if err != nil {
			return err
		}
		outerAID = a.ID

		// Inner savepoint — commit a row inside, then fail to trigger
		// ROLLBACK TO SAVEPOINT. The inner row must disappear; outerAID must
		// remain.
		innerErr := client.WithTx(outerCtx, "sp_inner", func(innerCtx context.Context) error {
			if _, err := client.Categories().Create(innerCtx, &models.CreateCategoryInput{
				Name: "sp-inner",
			}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(innerErr, sentinel) {
			t.Errorf("inner WithTx: err = %v, want sentinel", innerErr)
		}

		// Outer mutation #2 — runs after the inner rollback. Must also survive.
		b, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-outer-B",
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

	// Outer rows present.
	for _, id := range []uuid.UUID{outerAID, outerBID} {
		ok, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%s): %v", id, err)
		}
		if !ok {
			t.Errorf("outer row %s missing after savepoint rollback — outer tx lost", id)
		}
	}

	// Inner row absent — the name is unique, so an Exists-by-name probe via
	// ExistsWhere is the only way to confirm the savepoint actually rolled
	// back instead of committing.
	innerName := "sp-inner"
	exists, err := client.Categories().ExistsWhere(ctx, spName(innerName))
	if err != nil {
		t.Fatalf("ExistsWhere inner: %v", err)
	}
	if exists {
		t.Error("inner row persisted after savepoint rollback — ROLLBACK TO SAVEPOINT failed")
	}
}

// TestTxSavepoint_InnerCommitReleasesToOuter covers the release path. When
// both the inner and outer return nil, inner mutations are promoted (via
// RELEASE SAVEPOINT) into the outer transaction and persist on outer COMMIT.
func TestTxSavepoint_InnerCommitReleasesToOuter(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var outerID, innerID uuid.UUID

	err := client.WithTx(ctx, "outer_release", func(outerCtx context.Context) error {
		o, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: "sp-release-outer",
		})
		if err != nil {
			return err
		}
		outerID = o.ID

		innerErr := client.WithTx(outerCtx, "inner_release", func(innerCtx context.Context) error {
			i, err := client.Categories().Create(innerCtx, &models.CreateCategoryInput{
				Name: "sp-release-inner",
			})
			if err != nil {
				return err
			}
			innerID = i.ID
			return nil
		})
		return innerErr
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Categories().HardDelete(ctx, outerID)
		_ = client.Categories().HardDelete(ctx, innerID)
	})

	for _, id := range []uuid.UUID{outerID, innerID} {
		ok, err := client.Categories().Exists(ctx, id)
		if err != nil {
			t.Fatalf("Exists(%s): %v", id, err)
		}
		if !ok {
			t.Errorf("row %s missing after nested commit — RELEASE SAVEPOINT + COMMIT failed", id)
		}
	}
}

// TestTxSavepoint_OuterRollbackDiscardsReleasedInner verifies §18.6's
// "released-savepoint callbacks are still discarded if the outer rolls back"
// semantic at the row level: inner commits (RELEASE SAVEPOINT), but the outer
// then errors. Neither the outer row nor the released-inner row must persist.
//
// This is the edge case that distinguishes "promote callbacks on release" from
// "fire callbacks on release" — the runtime does the former, and the same
// promotion applies to the DB rows because pgx only materialises them on
// outer COMMIT.
func TestTxSavepoint_OuterRollbackDiscardsReleasedInner(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	sentinel := errors.New("outer abort")
	var innerID uuid.UUID
	innerName := "sp-discard-inner"
	outerName := "sp-discard-outer"

	err := client.WithTx(ctx, "outer_abort", func(outerCtx context.Context) error {
		if _, err := client.Categories().Create(outerCtx, &models.CreateCategoryInput{
			Name: outerName,
		}); err != nil {
			return err
		}

		innerErr := client.WithTx(outerCtx, "inner_released", func(innerCtx context.Context) error {
			i, err := client.Categories().Create(innerCtx, &models.CreateCategoryInput{
				Name: innerName,
			})
			if err != nil {
				return err
			}
			innerID = i.ID
			return nil // RELEASE SAVEPOINT
		})
		if innerErr != nil {
			return innerErr
		}

		return sentinel // outer ROLLBACK
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("outer WithTx: err = %v, want sentinel", err)
	}
	_ = innerID // no cleanup — the row never persisted

	// Both rows must be absent — ExistsWhere probes by the unique name.
	for _, name := range []string{outerName, innerName} {
		got, err := client.Categories().ExistsWhere(ctx, spName(name))
		if err != nil {
			t.Fatalf("ExistsWhere %q: %v", name, err)
		}
		if got {
			t.Errorf("row %q persisted after outer rollback — savepoint release leaked past outer ROLLBACK", name)
		}
	}
}
