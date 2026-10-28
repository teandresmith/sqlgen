package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// orders sets strict_updates: false in sqlgen.yml, so an Update whose key
// matches no row is an idempotent no-op: no entity and no error, rather than
// the ErrNotFound the read-back Get would report.
func TestUpdate_MissingPK_NonStrict_ReturnsNil_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	got, err := client.Orders().Update(ctx, int64(999999999), &models.UpdateOrderInput{Total: omittable.Set(10.0)})
	if err != nil {
		t.Fatalf("Update missing PK (strict_updates=false): got err=%v, want nil", err)
	}
	if got != nil {
		t.Errorf("Update missing PK (strict_updates=false): got %+v, want nil entity", got)
	}
}

// An Update with no field set issues no UPDATE and reads the row back. Under
// strict_updates: false a missing key is the same idempotent no-op as above,
// not the read-back's ErrNotFound, while an existing key still returns the row.
func TestUpdate_EmptyInput_NonStrict_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	t.Run("missing key returns nil", func(t *testing.T) {
		got, err := client.Orders().Update(ctx, int64(999999999), &models.UpdateOrderInput{})
		if err != nil {
			t.Fatalf("empty Update missing PK (strict_updates=false): got err=%v, want nil", err)
		}
		if got != nil {
			t.Errorf("empty Update missing PK (strict_updates=false): got %+v, want nil entity", got)
		}
	})

	t.Run("existing key returns the row", func(t *testing.T) {
		user, err := client.Users().Create(ctx, &models.CreateUserInput{Email: "nonstrict-empty@example.com", Name: "NonStrictEmpty"})
		if err != nil {
			t.Fatalf("Create user: %v", err)
		}
		t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
		order, err := client.Orders().Create(ctx, &models.CreateOrderInput{UserID: user.ID})
		if err != nil {
			t.Fatalf("Create order: %v", err)
		}
		t.Cleanup(func() { _ = client.Orders().HardDelete(ctx, order.ID) })

		got, err := client.Orders().Update(ctx, order.ID, &models.UpdateOrderInput{})
		if err != nil {
			t.Fatalf("empty Update existing PK (strict_updates=false): %v", err)
		}
		if got == nil || got.ID != order.ID {
			t.Errorf("empty Update existing PK (strict_updates=false): got %+v, want order %v", got, order.ID)
		}
	})
}
