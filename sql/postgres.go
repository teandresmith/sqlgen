package sql

import (
	"fmt"
	"strings"
)

// DriverMode specifies the Go database driver used with a dialect.
type DriverMode int

const (
	// DriverPgx selects the pgx driver, which supports native array parameters.
	DriverPgx DriverMode = iota
	// DriverStdlib selects the database/sql stdlib driver.
	DriverStdlib
)

// PostgresDialect implements the Dialect interface for PostgreSQL.
// The driver mode controls [PostgresDialect.SupportsArrayParams]: with pgx the
// comparators' In/Nin use native array parameters (= ANY($1)), while with
// stdlib they expand to IN ($1, $2, ...). The builder's own IN expansion is
// the same on both drivers.
type PostgresDialect struct {
	driver DriverMode
}

// Compile-time interface assertion.
var _ Dialect = PostgresDialect{}

// NewPostgresDialect returns a PostgreSQL dialect configured for the pgx driver.
func NewPostgresDialect() PostgresDialect {
	return PostgresDialect{driver: DriverPgx}
}

// NewPostgresStdlibDialect returns a PostgreSQL dialect configured for the
// database/sql stdlib driver.
func NewPostgresStdlibDialect() PostgresDialect {
	return PostgresDialect{driver: DriverStdlib}
}

// Name returns "postgres".
func (d PostgresDialect) Name() string { return "postgres" }

// Placeholder returns a positional placeholder ($1, $2, etc.).
func (d PostgresDialect) Placeholder(position int) string {
	return fmt.Sprintf("$%d", position)
}

// PlaceholderList returns a comma-separated list of positional placeholders.
func (d PostgresDialect) PlaceholderList(start, count int) string {
	if count == 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range count {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}

// QuoteIdentifier quotes an identifier with double quotes. An embedded double
// quote is doubled, which is how PostgreSQL escapes it inside a quoted
// identifier; without it the quote would end the identifier early.
func (d PostgresDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// FormatTable formats a table reference. When schema is set, produces
// "schema"."table"; otherwise just "table".
func (d PostgresDialect) FormatTable(table Table) string {
	if table.Schema != "" {
		return d.QuoteIdentifier(table.Schema) + "." + d.QuoteIdentifier(table.Name)
	}
	return d.QuoteIdentifier(table.Name)
}

// SupportsReturning returns true — PostgreSQL supports RETURNING.
func (d PostgresDialect) SupportsReturning() bool { return true }

// ReturningClause generates a RETURNING clause with quoted column names.
func (d PostgresDialect) ReturningClause(columns []string) string {
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
//
// [UpsertClauseOptions.ResolvePKColumn] is ignored: PostgreSQL cannot preserve
// a PK through the clause the way MySQL does with LAST_INSERT_ID(). The
// DO NOTHING branch below returns no row on conflict, so a caller that needs
// the PK back resolves it with a follow-up SELECT on the conflict columns —
// see the generated insertUpsertAndResolveID.
func (d PostgresDialect) UpsertClause(opts UpsertClauseOptions) string {
	quotedKeys := make([]string, len(opts.ConflictKeys))
	for i, k := range opts.ConflictKeys {
		quotedKeys[i] = d.QuoteIdentifier(k)
	}

	// No columns left to set — every inserted column is part of the conflict
	// target (a pure link table whose columns are exactly its composite key)
	// or is otherwise excluded. `DO UPDATE SET` with an empty list is a
	// syntax error, and there is nothing to update anyway: the conflicting
	// row is already identical, so the insert is a no-op.
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

// SupportsArrayParams reports whether this dialect instance uses native array
// parameters for IN/NOT IN conditions (true for pgx, false for stdlib).
func (d PostgresDialect) SupportsArrayParams() bool {
	return d.driver == DriverPgx
}

// SupportsTupleIN reports whether this dialect supports tuple IN syntax
// for composite PK batch lookups: WHERE (col1, col2) IN (($1, $2), ...).
func (d PostgresDialect) SupportsTupleIN() bool { return true }

// BackslashEscapes returns false — a backslash inside a standard '...'
// literal is literal text (standard_conforming_strings).
func (d PostgresDialect) BackslashEscapes() bool { return false }

// DefaultValuesClause returns "DEFAULT VALUES" — PostgreSQL rejects an empty
// column list.
func (d PostgresDialect) DefaultValuesClause() string { return "DEFAULT VALUES" }

// LockClause returns the row-level lock SQL fragment for the given mode.
// Returns the empty string for [LockNone] or any unrecognised mode.
func (d PostgresDialect) LockClause(mode LockMode) string {
	switch mode {
	case LockNone:
		return ""
	case LockForUpdate:
		return "FOR UPDATE"
	case LockForShare:
		return "FOR SHARE"
	case LockForUpdateNoWait:
		return "FOR UPDATE NOWAIT"
	case LockForUpdateSkipLocked:
		return "FOR UPDATE SKIP LOCKED"
	}
	return ""
}
