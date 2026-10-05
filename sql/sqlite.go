package sql

import (
	"strings"
)

// SQLiteDialect implements the Dialect interface for SQLite.
type SQLiteDialect struct{}

// Compile-time interface assertion.
var _ Dialect = SQLiteDialect{}

// NewSQLiteDialect returns a SQLite dialect.
func NewSQLiteDialect() SQLiteDialect {
	return SQLiteDialect{}
}

// Name returns "sqlite".
func (d SQLiteDialect) Name() string { return "sqlite" }

// Placeholder returns "?" — SQLite uses positional-by-order placeholders.
func (d SQLiteDialect) Placeholder(_ int) string { return "?" }

// PlaceholderList returns a comma-separated list of "?" placeholders.
func (d SQLiteDialect) PlaceholderList(_ int, count int) string {
	if count == 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range count {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

// QuoteIdentifier quotes an identifier with double quotes. An embedded double
// quote is doubled, which is how SQLite escapes it inside a quoted identifier.
func (d SQLiteDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// FormatTable formats a table reference with double quotes. Schema is ignored.
func (d SQLiteDialect) FormatTable(table Table) string {
	return d.QuoteIdentifier(table.Name)
}

// SupportsReturning returns true — SQLite 3.35+ supports RETURNING.
func (d SQLiteDialect) SupportsReturning() bool { return true }

// ReturningClause generates a RETURNING clause with quoted column names.
func (d SQLiteDialect) ReturningClause(columns []string) string {
	if len(columns) == 0 {
		return ""
	}
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = d.QuoteIdentifier(col)
	}
	return "RETURNING " + strings.Join(quoted, ", ")
}

// UpsertClause generates ON CONFLICT (...) DO UPDATE SET col = excluded.col.
// SQLite uses the same upsert syntax as PostgreSQL.
//
// [UpsertClauseOptions.ResolvePKColumn] is ignored for the same reason it is on
// PostgreSQL — see [PostgresDialect.UpsertClause].
func (d SQLiteDialect) UpsertClause(opts UpsertClauseOptions) string {
	quotedKeys := make([]string, len(opts.ConflictKeys))
	for i, k := range opts.ConflictKeys {
		quotedKeys[i] = d.QuoteIdentifier(k)
	}

	// No columns left to set — see PostgresDialect.UpsertClause. An empty
	// `DO UPDATE SET` is a syntax error; the conflicting row is already
	// identical, so DO NOTHING is the correct no-op.
	if len(opts.UpdateColumns) == 0 {
		return "ON CONFLICT (" + strings.Join(quotedKeys, ", ") + ") DO NOTHING"
	}

	setClauses := make([]string, len(opts.UpdateColumns))
	for i, col := range opts.UpdateColumns {
		q := d.QuoteIdentifier(col)
		setClauses[i] = q + " = excluded." + q
	}

	return "ON CONFLICT (" + strings.Join(quotedKeys, ", ") +
		") DO UPDATE SET " + strings.Join(setClauses, ", ")
}

// SupportsArrayParams returns false — SQLite does not support native array parameters.
func (d SQLiteDialect) SupportsArrayParams() bool { return false }

// SupportsTupleIN reports whether this dialect supports tuple IN syntax
// for composite PK batch lookups: WHERE (col1, col2) IN (($1, $2), ...).
func (d SQLiteDialect) SupportsTupleIN() bool { return true }

// BackslashEscapes returns false — SQLite reads a backslash inside a literal
// as literal text.
func (d SQLiteDialect) BackslashEscapes() bool { return false }

// DefaultValuesClause returns "DEFAULT VALUES" — SQLite rejects an empty
// column list. SQLite also accepts no upsert clause after DEFAULT VALUES,
// which is why [BuildInsert] omits the conflict clause when no column is
// supplied.
func (d SQLiteDialect) DefaultValuesClause() string { return "DEFAULT VALUES" }

// LockClause returns the empty string for every mode. SQLite has no per-row
// locking — it relies on database-level locks via BEGIN IMMEDIATE / BEGIN
// EXCLUSIVE. Generated read methods reject any non-[LockNone] mode at the
// runtime guard before reaching the SQL builder (see PRD §9.6a). This method
// returning empty preserves the [Dialect] contract for [LockNone] callers and
// is unreachable in well-formed generated code for other modes.
func (d SQLiteDialect) LockClause(_ LockMode) string { return "" }
