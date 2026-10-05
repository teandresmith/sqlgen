// Package introspect provides live database schema introspection capabilities.
// It discovers tables, columns, constraints, types, and comments from running
// database instances and populates the parser.Schema model.
package introspect

import (
	"context"
	"path/filepath"

	"github.com/teandresmith/sqlgen/parser"
)

// Introspector discovers schema elements from a live database connection.
type Introspector interface {
	// Name returns the dialect name (e.g., "postgres", "mysql", "sqlite").
	Name() string

	// Introspect connects to the database and populates schema with discovered
	// tables, columns, constraints, types, and comments. The opts parameter
	// controls which schemas and tables are included or excluded.
	Introspect(ctx context.Context, connString string, schema *parser.Schema, opts IntrospectionOptions) error

	// Close releases database resources held by the introspector.
	Close() error
}

// IntrospectionOptions controls which schemas and tables are discovered.
type IntrospectionOptions struct {
	// Schemas to include. If empty, all non-system schemas are included.
	Schemas []string

	// Schemas to exclude. Applied after include filter.
	ExcludeSchemas []string

	// Tables to include. Supports * (any sequence) and ? (any single char) wildcards.
	// If empty, all tables in included schemas are returned.
	Tables []string

	// Tables to exclude. Supports * and ? wildcards. Applied after include filter.
	ExcludeTables []string
}

// matchesFilter checks whether name passes the include/exclude filter lists.
// Include lists use exact matching; exclude lists support wildcards.
// An empty include list means "include all".
func matchesFilter(name string, include, exclude []string) bool {
	if len(include) > 0 && !matchesAny(name, include) {
		return false
	}
	if len(exclude) > 0 && matchesAny(name, exclude) {
		return false
	}
	return true
}

// matchesAny returns true if name matches any pattern in the list.
// Patterns support * (any sequence) and ? (any single char) wildcards
// via filepath.Match semantics.
func matchesAny(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// schemaAllowed checks whether a schema name passes the schema filters.
func schemaAllowed(schema string, opts IntrospectionOptions) bool {
	return matchesFilter(schema, opts.Schemas, opts.ExcludeSchemas)
}

// tableAllowed checks whether a table name passes the table filters.
func tableAllowed(table string, opts IntrospectionOptions) bool {
	return matchesFilter(table, opts.Tables, opts.ExcludeTables)
}
