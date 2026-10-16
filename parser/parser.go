package parser

// Parser parses SQL DDL statements into a Schema model.
// Implementations are dialect-specific (PostgreSQL, MySQL, SQLite).
// A Parser is re-entrant: Parse can be called multiple times with different
// files, and the schema is accumulated across calls.
type Parser interface {
	// Parse parses the SQL from the given file into the internal schema model.
	// It can be called multiple times with different files; results accumulate.
	Parse(filename string, sql []byte) error

	// Schema returns the accumulated schema model.
	Schema() *Schema
}

// UnreadColumns ends the error for a CREATE TABLE that takes its columns from
// another table, a type or a query, such as LIKE, INHERITS, OF or AS SELECT,
// after the form's name. The file parsers do not read those columns.
const UnreadColumns = "is not supported in SQL files, since sqlgen does not read the columns it gives the table; write the columns out, or create the table in the database and read it from there (input.source: database or both)"
