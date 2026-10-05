package stdlib

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/teandresmith/sqlgen/database"
)

// mapPqError translates *pq.Error from lib/pq into sqlgen error types.
// Uses the same PostgreSQL SQLSTATE codes as the pgx mapper.
func mapPqError(err error) (bool, error) {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false, err
	}

	code := string(pqErr.Code)

	// Connection exception class (08xxx)
	if strings.HasPrefix(code, "08") {
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
	}

	switch code {
	case "23505": // unique_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: pqErr.Constraint,
			Column:     pqErr.Column,
			Detail:     pqErr.Detail,
			Err:        err,
		}
	case "23503": // foreign_key_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: pqErr.Constraint,
			Column:     pqErr.Column,
			Detail:     pqErr.Detail,
			Err:        err,
		}
	case "23502": // not_null_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintNotNull,
			Constraint: pqErr.Constraint,
			Column:     pqErr.Column,
			Detail:     pqErr.Detail,
			Err:        err,
		}
	case "23514": // check_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: pqErr.Constraint,
			Column:     pqErr.Column,
			Detail:     pqErr.Detail,
			Err:        err,
		}
	case "40P01": // deadlock_detected
		return true, fmt.Errorf("%w: %w", database.ErrDeadlock, err)
	default:
		return false, err
	}
}
