package introspect

import (
	"fmt"

	"github.com/teandresmith/sqlgen/parser"
)

// Seedable is a parser that can be pre-populated with an existing schema.
// This enables "both" mode where introspected tables are loaded before
// file-based DDL is applied.
type Seedable interface {
	parser.Parser
	Seed(base *parser.Schema)
}

// Merge implements "both" mode: the introspected schema is seeded into the
// parser, then file-based DDL is applied on top. The parser's existing
// duplicate detection handles CREATE TABLE on existing tables (validation
// error). ALTER TABLE on introspected tables modifies them in place.
//
// After parsing, Merge validates that no duplicate column names with
// conflicting types exist within any table. This catches the case where
// ALTER TABLE ADD COLUMN names a column that already exists from the
// introspected schema with a different type.
func Merge(base *parser.Schema, p Seedable, files []string) error {
	// Seed the parser with the introspected schema.
	p.Seed(base)

	// Parse files — CREATE TABLE on existing introspected table will error.
	// ALTER TABLE on introspected table will modify it.
	if err := parser.ParseFiles(p, files); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}

	// Validate column conflicts: duplicate column names with different types
	// indicate that a file-based ADD COLUMN collided with an introspected column.
	// Explicit DROP + ADD or ALTER COLUMN TYPE are fine because they remove
	// the original before redefining.
	if err := validateColumnConflicts(p.Schema()); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}

	return nil
}

// validateColumnConflicts checks for duplicate column names within any table.
// A duplicate with a different type means a file-based column silently
// conflicts with an existing column.
func validateColumnConflicts(merged *parser.Schema) error {
	for _, t := range merged.Tables {
		seen := make(map[string]string, len(t.Columns))
		for _, c := range t.Columns {
			if prevType, exists := seen[c.Name]; exists && prevType != c.Type {
				tbl := t.Name
				if t.Schema != "" {
					tbl = t.Schema + "." + t.Name
				}
				return fmt.Errorf(
					"column conflict: %s has duplicate column %q with types %q and %q — use ALTER TABLE ... ALTER COLUMN to change",
					tbl, c.Name, prevType, c.Type,
				)
			}
			seen[c.Name] = c.Type
		}
	}
	return nil
}
