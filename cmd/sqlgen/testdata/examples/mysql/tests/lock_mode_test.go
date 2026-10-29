package tests

// LockMode E2E (MySQL).
//
// Pins the driver error a contended FOR UPDATE NOWAIT surfaces against a real
// mysql:8.0 testcontainer (ER_LOCK_NOWAIT, 3572). The lock contract itself
// runs on every dialect in the conformance suite.
//
// Cache-bypass coverage: mysql/ has no cache configured (sqlgen.yml does not
// enable it). The SkipCache force is asserted at the codegen layer
// (lock_mode_codegen_test.go) and at the runtime cache layer in
// cache/tests/skip_test.go. Same rationale as the postgres test header.

import (
	"context"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// MySQL ER_LOCK_NOWAIT — surfaces from `FOR UPDATE NOWAIT` when the targeted
// row is locked by another transaction. Pinned by error number, not message,
// so a server-version locale change doesn't break the test.
const errLockNoWait = 3572

// TestLockMode_NoWaitReturnsLockNoWait_MySQL — second tx attempts FOR UPDATE
// NOWAIT against a row already locked by the first tx; must surface
// ER_LOCK_NOWAIT (3572). Asserted via errors.As against *mysql.MySQLError so
// a message-format change doesn't break the pin.
func TestLockMode_NoWaitReturnsLockNoWait_MySQL(t *testing.T) {
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

	contenderErr := client.WithTx(ctx, "lock_nowait_contender", func(txCtx context.Context) error {
		_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		return err
	})
	if contenderErr == nil {
		t.Fatal("LockForUpdateNoWait against held row: err = nil, want ER_LOCK_NOWAIT")
	}

	var myErr *mysql.MySQLError
	if !errors.As(contenderErr, &myErr) {
		t.Fatalf("LockForUpdateNoWait error not a *mysql.MySQLError: %T: %v", contenderErr, contenderErr)
	}
	if myErr.Number != errLockNoWait {
		t.Errorf("LockForUpdateNoWait MySQL error number = %d, want %d (ER_LOCK_NOWAIT)", myErr.Number, errLockNoWait)
	}
}

func strPtrMy(s string) *string { return &s }
