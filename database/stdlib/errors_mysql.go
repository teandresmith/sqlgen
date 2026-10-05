package stdlib

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/teandresmith/sqlgen/database"
)

// mapMySQLError translates *mysql.MySQLError into sqlgen error types.
func mapMySQLError(err error) (bool, error) {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false, err
	}

	switch mysqlErr.Number {
	case 1062: // ER_DUP_ENTRY → unique violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: parseMySQLConstraint(mysqlErr.Message),
			Detail:     mysqlErr.Message,
			Err:        err,
		}
	case 1451, 1452: // ER_ROW_IS_REFERENCED_2 / ER_NO_REFERENCED_ROW_2 → FK violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: parseMySQLConstraint(mysqlErr.Message),
			Detail:     mysqlErr.Message,
			Err:        err,
		}
	case 1048: // ER_BAD_NULL_ERROR → not null violation
		return true, &database.ConstraintError{
			Type:   database.ConstraintNotNull,
			Column: parseMySQLColumn(mysqlErr.Message),
			Detail: mysqlErr.Message,
			Err:    err,
		}
	case 3819: // ER_CHECK_CONSTRAINT_VIOLATED → check violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: parseMySQLConstraint(mysqlErr.Message),
			Detail:     mysqlErr.Message,
			Err:        err,
		}
	case 1213: // ER_LOCK_DEADLOCK
		return true, fmt.Errorf("%w: %w", database.ErrDeadlock, err)
	case 2002, 2006, 1040: // connection errors
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
	default:
		return false, err
	}
}

// parseMySQLConstraint extracts the constraint name from MySQL error messages.
// Supports duplicate entry (1062), foreign key (1451/1452), and check constraint (3819) formats.
func parseMySQLConstraint(msg string) string {
	// 1062: "Duplicate entry 'VALUE' for key 'TABLE.CONSTRAINT'" or "... for key 'CONSTRAINT'"
	if idx := strings.LastIndex(msg, "for key '"); idx != -1 {
		rest := msg[idx+len("for key '"):]
		if key, _, found := strings.Cut(rest, "'"); found {
			// MySQL 8+ includes table prefix: "table.constraint"
			if dot := strings.LastIndex(key, "."); dot != -1 {
				return key[dot+1:]
			}
			return key
		}
	}

	// 1451/1452: "... CONSTRAINT `constraint_name` ..."
	if _, after, found := strings.Cut(msg, "CONSTRAINT `"); found {
		if name, _, ok := strings.Cut(after, "`"); ok {
			return name
		}
	}

	// 3819: "Check constraint 'constraint_name' is violated."
	if after, found := strings.CutPrefix(msg, "Check constraint '"); found {
		if name, _, ok := strings.Cut(after, "'"); ok {
			return name
		}
	}

	return ""
}

// parseMySQLColumn extracts the column name from MySQL error messages.
// Supports not-null violation (1048) format: "Column 'COLUMN' cannot be null"
func parseMySQLColumn(msg string) string {
	after, found := strings.CutPrefix(msg, "Column '")
	if !found {
		return ""
	}
	if col, _, ok := strings.Cut(after, "'"); ok {
		return col
	}
	return ""
}
