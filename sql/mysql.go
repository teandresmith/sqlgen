package sql

import (
	"strings"
)

// MySQLDialect implements the Dialect interface for MySQL.
type MySQLDialect struct{}

// Compile-time interface assertion.
var _ Dialect = MySQLDialect{}

// NewMySQLDialect returns a MySQL dialect.
func NewMySQLDialect() MySQLDialect {
	return MySQLDialect{}
}

// Name returns "mysql".
func (d MySQLDialect) Name() string { return "mysql" }

// Placeholder returns "?" — MySQL uses positional-by-order placeholders.
func (d MySQLDialect) Placeholder(_ int) string { return "?" }

// PlaceholderList returns a comma-separated list of "?" placeholders.
func (d MySQLDialect) PlaceholderList(_ int, count int) string {
	if count == 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range count {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// QuoteIdentifier quotes an identifier with backticks. An embedded backtick is
// doubled, which is how MySQL escapes it inside a quoted identifier.
func (d MySQLDialect) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// FormatTable formats a table reference with backticks. Schema is ignored —
// MySQL uses databases, not schemas.
func (d MySQLDialect) FormatTable(table Table) string {
	return d.QuoteIdentifier(table.Name)
}

// SupportsReturning returns false — MySQL does not support RETURNING.
func (d MySQLDialect) SupportsReturning() bool { return false }

// ReturningClause returns an empty string — MySQL does not support RETURNING.
func (d MySQLDialect) ReturningClause(_ []string) string { return "" }

// UpsertClause generates ON DUPLICATE KEY UPDATE col = VALUES(col).
// MySQL does not support explicit conflict targets, so ConflictKeys does not
// appear in the output — it is read only to name a column for the no-op form
// below.
//
// When [UpsertClauseOptions.ResolvePKColumn] is set, the set list opens with
// `pk` = LAST_INSERT_ID(`pk`). MySQL's OK packet carries insert_id = 0 for an
// ON DUPLICATE KEY UPDATE that modifies no row, so an idempotent upsert — same
// conflict key, same values — would otherwise report LastInsertId() = 0 and
// send the caller looking for a row with a zero PK. Assigning the PK to itself
// through LAST_INSERT_ID() republishes it into the OK packet without touching
// the row: the statement still reports RowsAffected = 0, and the value survives
// on the insert branch too, where the clause is never evaluated.
//
// The caller excludes PK columns from UpdateColumns, so the PK is assigned
// once. Were it assigned twice, MySQL's left-to-right evaluation would let the
// later `pk` = VALUES(`pk`) overwrite the preserved id.
func (d MySQLDialect) UpsertClause(opts UpsertClauseOptions) string {
	var setClauses []string
	if opts.ResolvePKColumn != "" {
		pk := d.QuoteIdentifier(opts.ResolvePKColumn)
		setClauses = append(setClauses, pk+" = LAST_INSERT_ID("+pk+")")
	}
	for _, col := range opts.UpdateColumns {
		q := d.QuoteIdentifier(col)
		setClauses = append(setClauses, q+" = VALUES("+q+")")
	}

	// Nothing to set — every inserted column is part of the conflict key (a
	// pure link table whose columns are exactly its composite key) or is
	// otherwise excluded, and the caller needs no PK back. MySQL has no
	// `DO NOTHING`, and an empty `ON DUPLICATE KEY UPDATE` is a syntax error,
	// so assign a key column to itself: the statement stays valid and the
	// conflicting row is untouched.
	if len(setClauses) == 0 {
		if len(opts.ConflictKeys) == 0 {
			return ""
		}
		k := d.QuoteIdentifier(opts.ConflictKeys[0])
		return "ON DUPLICATE KEY UPDATE " + k + " = " + k
	}

	return "ON DUPLICATE KEY UPDATE " + strings.Join(setClauses, ", ")
}

// SupportsArrayParams returns false — MySQL does not support native array parameters.
func (d MySQLDialect) SupportsArrayParams() bool { return false }

// SupportsTupleIN returns false — MySQL uses expanded OR for composite PK batch lookups.
func (d MySQLDialect) SupportsTupleIN() bool { return false }

// BackslashEscapes returns true — MySQL's default sql_mode reads a backslash
// inside a '...' or "..." literal as escaping the character after it.
func (d MySQLDialect) BackslashEscapes() bool { return true }

// DefaultValuesClause returns "() VALUES ()" — MySQL has no DEFAULT VALUES
// form, and accepts an empty column list with an empty value row instead.
func (d MySQLDialect) DefaultValuesClause() string { return "() VALUES ()" }

// LockClause returns the row-level lock SQL fragment for the given mode.
// MySQL uses "LOCK IN SHARE MODE" for [LockForShare]. NoWait and SkipLocked
// require MySQL 8.0+; the version guard lives in the codegen runtime layer.
// Returns the empty string for [LockNone] or any unrecognised mode.
func (d MySQLDialect) LockClause(mode LockMode) string {
	switch mode {
	case LockNone:
		return ""
	case LockForUpdate:
		return "FOR UPDATE"
	case LockForShare:
		return "LOCK IN SHARE MODE"
	case LockForUpdateNoWait:
		return "FOR UPDATE NOWAIT"
	case LockForUpdateSkipLocked:
		return "FOR UPDATE SKIP LOCKED"
	}
	return ""
}
