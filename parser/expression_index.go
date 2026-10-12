package parser

// ExpressionIndex is an index with an expression part, such as lower(email).
// It is not an index over columns, so it is not one of the table's constraints
// (PRD §30.7), but its name is taken, and an unnamed index created after it is
// named around it. Both the file parser and the introspector record it, so a
// parser seeded with an introspected schema names later indexes as the
// database does.
type ExpressionIndex struct {
	Name string
	// Columns is dialect-specific. On PostgreSQL it holds every column the
	// index reads, in table order: key columns, expression, INCLUDE and WHERE
	// columns alike, since dropping any of them drops the index. On MySQL it
	// holds the key parts in order, "" for an expression part, since one whose
	// leading columns are a foreign key's serves as that key's index.
	Columns []string
}
