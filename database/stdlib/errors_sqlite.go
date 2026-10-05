package stdlib

import (
	"errors"
	"fmt"
	"strings"

	"github.com/teandresmith/sqlgen/database"
)

// sqliteError matches modernc.org/sqlite's error type via interface detection.
// This avoids importing the specific package while supporting any error type
// that exposes an extended SQLite error code.
type sqliteError interface {
	error
	Code() int
}

// mapModerncSQLiteError translates modernc.org/sqlite errors into sqlgen error types.
// Detection is interface-based: any error with a Code() int method is matched.
func mapModerncSQLiteError(err error) (bool, error) {
	var se sqliteError
	if !errors.As(err, &se) {
		return false, err
	}
	return mapSQLiteCode(se.Code(), se.Error(), err)
}

// mapSQLiteCode maps a SQLite extended error code and message to a sqlgen error.
// modernc.org/sqlite appends the extended code to its message, as in
// "constraint failed: UNIQUE constraint failed: users.email (2067)", so the
// suffix is stripped before the constraint and column are parsed. Detail keeps
// the whole message.
func mapSQLiteCode(code int, msg string, original error) (bool, error) {
	// Check primary code for connection-level errors.
	// Primary code is the lower 8 bits of the extended code.
	primary := code & 0xff
	switch primary {
	case 5: // SQLITE_BUSY
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, original)
	case 14: // SQLITE_CANTOPEN
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, original)
	}

	body := strings.TrimSuffix(msg, fmt.Sprintf(" (%d)", code))
	switch code {
	case 2067: // SQLITE_CONSTRAINT_UNIQUE
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: parseSQLiteConstraint(body),
			Column:     parseSQLiteColumn(body),
			Detail:     msg,
			Err:        original,
		}
	case 1555: // SQLITE_CONSTRAINT_PRIMARYKEY
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: parseSQLiteConstraint(body),
			Column:     parseSQLiteColumn(body),
			Detail:     msg,
			Err:        original,
		}
	case 787: // SQLITE_CONSTRAINT_FOREIGNKEY
		return true, &database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: parseSQLiteConstraint(body),
			Column:     parseSQLiteColumn(body),
			Detail:     msg,
			Err:        original,
		}
	case 1299: // SQLITE_CONSTRAINT_NOTNULL
		return true, &database.ConstraintError{
			Type:       database.ConstraintNotNull,
			Constraint: parseSQLiteConstraint(body),
			Column:     parseSQLiteColumn(body),
			Detail:     msg,
			Err:        original,
		}
	case 275: // SQLITE_CONSTRAINT_CHECK
		return true, &database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: parseSQLiteConstraint(body),
			Column:     parseSQLiteColumn(body),
			Detail:     msg,
			Err:        original,
		}
	default:
		return false, original
	}
}

// parseSQLiteConstraint extracts the constraint identifier from SQLite error messages.
// SQLite's format is "TYPE constraint failed: identifier", and modernc.org/sqlite
// prefixes the result code's own text: "constraint failed: TYPE constraint
// failed: identifier". The identifier follows the last "constraint failed", so a
// message whose last one carries no identifier ("FOREIGN KEY constraint failed")
// yields "".
func parseSQLiteConstraint(msg string) string {
	idx := strings.LastIndex(msg, "constraint failed")
	if idx == -1 {
		return ""
	}
	after, found := strings.CutPrefix(msg[idx+len("constraint failed"):], ": ")
	if !found {
		return ""
	}
	return strings.TrimSpace(after)
}

// parseSQLiteColumn extracts the column name from SQLite constraint error messages.
// For messages like "UNIQUE constraint failed: table.column", returns "column".
func parseSQLiteColumn(msg string) string {
	constraint := parseSQLiteConstraint(msg)
	if constraint == "" {
		return ""
	}
	if dot := strings.LastIndex(constraint, "."); dot != -1 {
		return constraint[dot+1:]
	}
	return ""
}
