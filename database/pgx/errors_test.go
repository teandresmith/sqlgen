package pgx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

func TestMapErrorConstraintCodes(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		constraintName string
		columnName     string
		detail         string
		wantType       database.ConstraintType
	}{
		{
			name:           "23505 unique violation",
			code:           "23505",
			constraintName: "users_email_key",
			columnName:     "email",
			detail:         "Key (email)=(test@test.com) already exists.",
			wantType:       database.ConstraintUnique,
		},
		{
			name:           "23503 foreign key violation",
			code:           "23503",
			constraintName: "orders_user_id_fkey",
			columnName:     "user_id",
			detail:         "Key (user_id)=(999) is not present in table \"users\".",
			wantType:       database.ConstraintForeignKey,
		},
		{
			name:           "23502 not null violation",
			code:           "23502",
			constraintName: "users_name_not_null",
			columnName:     "name",
			detail:         "Failing row contains (1, null, test@test.com).",
			wantType:       database.ConstraintNotNull,
		},
		{
			name:           "23514 check violation",
			code:           "23514",
			constraintName: "users_age_check",
			columnName:     "age",
			detail:         "Failing row contains (1, alice, -1).",
			wantType:       database.ConstraintCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{
				Code:           tt.code,
				ConstraintName: tt.constraintName,
				ColumnName:     tt.columnName,
				Detail:         tt.detail,
			}

			ok, got := pgxadapter.MapError(pgErr)
			if !ok {
				t.Fatalf("MapError(%s) returned ok=false, want true", tt.code)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("MapError(%s) did not return ConstraintError, got %T: %v", tt.code, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if ce.Constraint != tt.constraintName {
				t.Errorf("ConstraintError.Constraint = %q, want %q", ce.Constraint, tt.constraintName)
			}
			if ce.Column != tt.columnName {
				t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, tt.columnName)
			}
			if ce.Detail != tt.detail {
				t.Errorf("ConstraintError.Detail = %q, want %q", ce.Detail, tt.detail)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

func TestMapErrorDeadlock(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "40P01"}

	ok, got := pgxadapter.MapError(pgErr)

	if !ok {
		t.Fatal("MapError(40P01) returned ok=false, want true")
	}
	if !errors.Is(got, database.ErrDeadlock) {
		t.Errorf("MapError(40P01) is not ErrDeadlock: %v", got)
	}
}

func TestMapErrorConnectionFailed(t *testing.T) {
	codes := []string{"08000", "08003", "08006", "08001", "08P01"}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: code}

			ok, got := pgxadapter.MapError(pgErr)

			if !ok {
				t.Fatalf("MapError(%s) returned ok=false, want true", code)
			}
			if !errors.Is(got, database.ErrConnectionFailed) {
				t.Errorf("MapError(%s) is not ErrConnectionFailed: %v", code, got)
			}
		})
	}
}

func TestMapErrorUnrecognized(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42P01"} // undefined_table

	ok, _ := pgxadapter.MapError(pgErr)

	if ok {
		t.Error("MapError(42P01) returned ok=true, want false for unrecognized code")
	}
}

func TestMapErrorNil(t *testing.T) {
	ok, got := pgxadapter.MapError(nil)
	if ok || got != nil {
		t.Errorf("MapError(nil) = (%v, %v), want (false, nil)", ok, got)
	}
}

func TestMapErrorNonPgError(t *testing.T) {
	ok, _ := pgxadapter.MapError(errors.New("some other error"))

	if ok {
		t.Error("MapError(non-pgx error) returned ok=true, want false")
	}
}

func TestMapErrorWrappedPgError(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "users_email_key",
		ColumnName:     "email",
		Detail:         "Key (email)=(dup@test.com) already exists.",
	}
	wrapped := fmt.Errorf("some context: %w", pgErr)

	ok, got := pgxadapter.MapError(wrapped)
	if !ok {
		t.Fatal("MapError(wrapped PgError) returned ok=false, want true")
	}

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError(wrapped PgError) did not return ConstraintError, got %T: %v", got, got)
	}
	if ce.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
	}
	if ce.Err == nil {
		t.Error("ConstraintError.Err = nil, want non-nil")
	}
}

func TestMapErrorPreservesOriginalInErr(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "pk_test",
	}

	_, got := pgxadapter.MapError(pgErr)

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError did not return ConstraintError")
	}
	if !errors.Is(ce.Err, pgErr) {
		t.Errorf("ConstraintError.Err does not wrap original PgError")
	}
}
