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
