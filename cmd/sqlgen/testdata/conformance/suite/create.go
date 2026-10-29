package suite

// Helpers in this file take the prefix cr.

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// create is the Create contract.
func create(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("leaving out a NOT NULL column with no default returns a not-null ConstraintError", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().Create(ctx, &models.CreateCounterInput{Name: "visits"})
		crAssertNotNull(t, err, "updated_at")
		assertRowCount(t, h, "counters", 0)
	})
}

// createMany is the CreateMany contract.
func createMany(t *testing.T, h *Harness) {
	_, _ = t, h
}

// crAssertNotNull checks that err is a not-null ConstraintError naming column.
func crAssertNotNull(t *testing.T, err error, column string) {
	t.Helper()
	if !errors.Is(err, models.ErrConstraintViolation) {
		t.Fatalf("error = %v, want ErrConstraintViolation", err)
	}
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok {
		t.Fatalf("error = %T, want *models.ConstraintError", err)
	}
	if ce.Type != models.ConstraintNotNull {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, models.ConstraintNotNull)
	}
	if ce.Column != column {
		t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, column)
	}
}
