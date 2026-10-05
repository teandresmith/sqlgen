// Package sql provides dialect-aware SQL statement builders and core types
// used by generated SQLGen code.
package sql

// Table identifies a database table by its optional schema and name.
type Table struct {
	Schema string // "public", "billing", "" (MySQL/SQLite)
	Name   string // "users", "products"
}

// Dialect abstracts SQL syntax differences across PostgreSQL, MySQL, and SQLite.
// Every SQL builder function receives a Dialect and calls its methods to produce
// correct SQL for the target database.
type Dialect interface {
	// Name returns the dialect identifier ("postgres", "mysql", "sqlite").
	Name() string

	// Placeholder returns a positional placeholder for the given 1-indexed position.
	// PostgreSQL: "$1", "$2". MySQL/SQLite: "?".
	Placeholder(position int) string

	// PlaceholderList returns a comma-separated list of placeholders starting at
	// the given position for count parameters. Example: "$1, $2, $3" or "?, ?, ?".
	PlaceholderList(start, count int) string

	// QuoteIdentifier quotes a SQL identifier (column name, alias) using the
	// dialect's quoting convention. PostgreSQL/SQLite: "name". MySQL: `name`.
	QuoteIdentifier(name string) string

	// FormatTable formats a table reference for use in FROM, INSERT INTO, UPDATE,
	// DELETE FROM, and JOIN clauses. PostgreSQL: "schema"."table". MySQL: `table`.
	FormatTable(table Table) string

	// SupportsReturning reports whether the dialect supports RETURNING clauses.
	SupportsReturning() bool

	// ReturningClause generates a RETURNING clause for the given columns.
	// Returns an empty string if the dialect does not support RETURNING.
	ReturningClause(columns []string) string

	// UpsertClause generates dialect-specific upsert SQL.
	// PostgreSQL/SQLite: ON CONFLICT (...) DO UPDATE SET ...
	// MySQL: ON DUPLICATE KEY UPDATE ...
	UpsertClause(opts UpsertClauseOptions) string

	// SupportsArrayParams reports whether this dialect uses native array
	// parameters for IN/NOT IN conditions (e.g., pgx passes Go slices as
	// PostgreSQL arrays). When true, comparators produce = ANY($) with a
	// single array arg instead of expanded IN ($, $, ...). The builder's own
	// "col IN $" expansion ([ConditionBuilder.In]) does not consult it and
	// always expands: comparator.Enum relies on that, because pgx has no
	// encode plan for a slice of a named string type against an enum-array
	// OID.
	SupportsArrayParams() bool

	// SupportsTupleIN reports whether this dialect supports tuple IN syntax
	// for composite PK batch lookups: WHERE (col1, col2) IN (($1,$2), ...).
	// MySQL does not support this and uses expanded OR instead.
	SupportsTupleIN() bool

	// BackslashEscapes reports whether a backslash escapes the character after
	// it inside a '...' or "..." string literal, as MySQL's default sql_mode
	// reads it. PostgreSQL and SQLite read a backslash there literally. The
	// builder's "$" token scan consults it to find where a literal ends.
	BackslashEscapes() bool

	// DefaultValuesClause returns the fragment that follows the table in a
	// single-row INSERT that supplies no column, so that every column takes
	// its default. PostgreSQL/SQLite: "DEFAULT VALUES". MySQL: "() VALUES ()".
	// The two are not interchangeable: PostgreSQL and SQLite reject an empty
	// column list, and MySQL rejects DEFAULT VALUES.
	DefaultValuesClause() string

	// LockClause returns the row-level lock SQL fragment for the given mode,
	// without a leading space. Returns an empty string for [LockNone].
	//
	// PostgreSQL: "FOR UPDATE", "FOR SHARE", "FOR UPDATE NOWAIT",
	// "FOR UPDATE SKIP LOCKED".
	// MySQL: "FOR UPDATE", "LOCK IN SHARE MODE", "FOR UPDATE NOWAIT" (8.0+),
	// "FOR UPDATE SKIP LOCKED" (8.0+).
	// SQLite: empty string regardless of mode — SQLite has no per-row locking;
	// the rejection of non-[LockNone] modes happens at the runtime guard in
	// generated read methods (see PRD §9.6a) before this is reached.
	LockClause(mode LockMode) string
}

// UpsertClauseOptions configures [Dialect.UpsertClause].
type UpsertClauseOptions struct {
	// ConflictKeys names the conflict target columns. PostgreSQL and SQLite
	// render them as the ON CONFLICT target; MySQL has no explicit target and
	// reads them only to name a column for the degenerate no-op form.
	ConflictKeys []string

	// UpdateColumns names the columns assigned on the conflict path. The
	// caller has already excluded the primary key, the conflict target, and
	// the tenant column, so this is empty whenever every inserted column
	// belongs to the conflict target.
	UpdateColumns []string

	// ResolvePKColumn names the single-column primary key the caller must read
	// back from the statement, or "" when the caller already knows the PK
	// (composite, app-generated, or caller-supplied) and needs nothing
	// preserved.
	//
	// It exists because a conflict that changes nothing still has to yield the
	// conflicting row's PK. Each dialect loses that differently: MySQL reports
	// insert_id = 0 for an ON DUPLICATE KEY UPDATE that modifies no row, and
	// the RETURNING dialects return no row at all from DO NOTHING. Only MySQL
	// can repair it inside the clause — see [MySQLDialect.UpsertClause]; on
	// PostgreSQL and SQLite the field is ignored and the generated caller
	// resolves the PK with a follow-up SELECT.
	ResolvePKColumn string
}

// Condition represents a single SQL condition with a clause containing "$"
// placeholder tokens and an associated value. Builders replace "$" with
// dialect-specific placeholders at the correct position.
type Condition struct {
	Clause string // SQL fragment, e.g. "col = $" or "col IS NULL"
	Value  any    // parameter value(s): scalar, Range, And, Or, []any, etc.

	// Column names the column token inside Clause that alias qualification
	// rewrites. Set it whenever the column is known at construction time;
	// [PrefixConditions] then qualifies that token in place instead of
	// prepending the alias to the whole clause.
	//
	// It is load-bearing only where the column is not the clause's leading
	// token — `JSON_CONTAINS(col, $)` on MySQL is the case that forced it,
	// since blind prepending produced `u.JSON_CONTAINS(col, ?)`, which MySQL
	// reads as a stored routine in schema `u`. Leaving it empty
	// keeps the historical prepend, which is what [Raw] and any hand-built
	// condition rely on.
	Column string

	// Raw marks Clause as an opaque predicate expression whose internal
	// structure the builder cannot see — set by [Raw], and settable on a
	// hand-built condition carrying the same shape.
	//
	// Builders join conditions with AND, which binds tighter than OR, so an
	// opaque clause carrying a top-level OR would otherwise break out of its
	// own term and drop every sibling predicate on the AND side out of the
	// disjunction — the correlation, the soft-delete default and the tenant
	// filter among them. A marked condition is parenthesised at
	// render time, which is what makes it a single AND-able term.
	//
	// The flag is read at render time rather than baked into Clause at
	// construction, so [PrefixConditions] still sees the unwrapped clause and
	// qualifies it exactly as it does any other.
	Raw bool
}

// SortDirection specifies ascending or descending order.
type SortDirection string

const (
	// Asc sorts in ascending order.
	Asc SortDirection = "ASC"
	// Desc sorts in descending order.
	Desc SortDirection = "DESC"
)

// Sort specifies a column ordering for ORDER BY clauses.
type Sort struct {
	Column    string        // column name
	Direction SortDirection // Asc or Desc
}
