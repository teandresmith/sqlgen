package tests

// LockMode E2E (PostgreSQL).
//
// Pins the driver error a contended FOR UPDATE NOWAIT surfaces against a real
// Postgres testcontainer (SQLSTATE 55P03). The lock contract itself runs on
// every dialect in the conformance suite.
//
// Cache-bypass coverage: postgres/ has no cache configured (sqlgen.yml does
// not enable it), so the SkipCache force is asserted at the codegen layer
// (see lock_mode_codegen_test.go) and at the runtime cache layer in
// cache/tests/skip_test.go. PRD §9.6a's SkipCache invariant is pinned by the
// guard's emitted line at the top of every read method body — verifying that
// pinning here would require wiring a cache module the example does not
// generate.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestLockMode_NoWaitReturnsLockNotAvailable verifies that LockForUpdateNoWait
// against a row already locked by another transaction returns the dialect's
// lock-not-available error immediately, surfaced through sqlgen's normal error
// chain. PostgreSQL emits SQLSTATE 55P03 (lock_not_available); we assert via
// errors.As against *pgconn.PgError so a string-format change in the driver
// or in sqlgen's wrapping doesn't break the pin.
func TestLockMode_NoWaitReturnsLockNotAvailable(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seed, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-nowait-seed",
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, seed.ID) })

	holderReady := make(chan struct{})
	holderRelease := make(chan struct{})
	holderDone := make(chan error, 1)

	// Holder takes FOR UPDATE and waits for the test to release it.
	go func() {
		holderDone <- client.WithTx(context.Background(), "lock_nowait_holder", func(txCtx context.Context) error {
			if _, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = sql.LockForUpdate
			}); err != nil {
				return err
			}
			close(holderReady)
			<-holderRelease
			return nil
		})
	}()

	<-holderReady
	defer func() {
		close(holderRelease)
		<-holderDone
	}()

	// Contender attempts NOWAIT — must surface SQLSTATE 55P03 immediately.
	contenderErr := client.WithTx(ctx, "lock_nowait_contender", func(txCtx context.Context) error {
		_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		return err
	})
	if contenderErr == nil {
		t.Fatal("LockForUpdateNoWait against held row: err = nil, want lock_not_available")
	}

	var pgErr *pgconn.PgError
	if !errors.As(contenderErr, &pgErr) {
		t.Fatalf("LockForUpdateNoWait error not a *pgconn.PgError: %T: %v", contenderErr, contenderErr)
	}
	if pgErr.Code != "55P03" {
		t.Errorf("LockForUpdateNoWait SQLSTATE = %q, want %q (lock_not_available)", pgErr.Code, "55P03")
	}
}
