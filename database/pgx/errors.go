package pgx

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/teandresmith/sqlgen/database"
)

// MapError translates pgx driver errors into sqlgen error types.
// It maps *pgconn.PgError to *database.ConstraintError for constraint
// violations, or to database.ErrDeadlock / database.ErrConnectionFailed
// for the corresponding PostgreSQL error classes.
// Returns (true, mapped error) when the error was recognized, or
// (false, original error) when it was not.
func MapError(err error) (bool, error) {
	if err == nil {
		return false, nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false, err
	}

	// Connection exception class (08xxx)
	if strings.HasPrefix(pgErr.Code, "08") {
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
	}

	switch pgErr.Code {
	case "23505": // unique_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23503": // foreign_key_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23502": // not_null_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintNotNull,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23514": // check_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "40P01": // deadlock_detected
		return true, fmt.Errorf("%w: %w", database.ErrDeadlock, err)
	default:
		return false, err
	}
}
