package introspect

import (
	"context"
	"fmt"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/sqlite"
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
// conflicting types exist within any table. The parsers already refuse an
// ALTER TABLE ADD COLUMN of a name the table has, as the databases do, so
// this is a backstop for any other statement that leaves one.
//
// A migration that breaks a database view is an error, decided by the
// database where it can say (PRD §6.1): the PostgreSQL file parser refuses
// to drop or retype what a view depends on (base.ViewDependencies), SQLite
// runs the files against a copy of the database's schema
// (base.SQLiteSchema), and on MySQL a table a view reads
// (base.ViewDependencies) must stay and the views' columns are traced
// through their definitions.
func Merge(base *parser.Schema, p Seedable, files []string) error {
	_, replay := p.(*sqlite.Parser)
	_, traced := p.(*mysql.Parser)
	reads := readViewSources(base, traced)
	var tables []parser.ViewDependency
	if traced {
		tables = viewTables(base)
	}

	// Seed the parser with the introspected schema.
	p.Seed(base)

	// Parse files — CREATE TABLE on existing introspected table will error.
	// ALTER TABLE on introspected table will modify it.
	if err := parser.ParseFiles(p, files); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}

	// Validate column conflicts: duplicate column names with different types
	// indicate that a file-based column collided with an introspected one.
	// Explicit DROP + ADD or ALTER COLUMN TYPE are fine because they remove
	// the original before redefining.
	if err := validateColumnConflicts(p.Schema()); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}
	if replay {
		if err := mergeSQLiteViews(context.Background(), p.Schema(), files); err != nil {
			return fmt.Errorf("merge both mode: %w", err)
		}
		return nil
	}
	if err := checkViewTables(p.Schema(), tables); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}
	var searchPath parser.SearchPath
	if sp, ok := p.(interface{ SearchPath() parser.SearchPath }); ok {
		searchPath = sp.SearchPath()
	}
	if err := refreshViewColumns(p.Schema(), searchPath, reads); err != nil {
		return fmt.Errorf("merge both mode: %w", err)
	}

	return nil
}

// viewTables returns the dependencies of s's views on a table s has.
func viewTables(s *parser.Schema) []parser.ViewDependency {
	var deps []parser.ViewDependency
	for _, dep := range s.ViewDependencies {
		if dep.Object.Kind == parser.TableDependency && hasTable(s, dep.Object) {
			deps = append(deps, dep)
		}
	}
	return deps
}

// checkViewTables returns an error for a table one of deps names that the
// SQL files dropped or renamed: MySQL leaves a view that reads it invalid,
// and the database's view stands under source: both. A table created again
// under the name satisfies it, as the view then reads it.
func checkViewTables(s *parser.Schema, deps []parser.ViewDependency) error {
	for _, dep := range deps {
		if !hasTable(s, dep.Object) {
			return fmt.Errorf("view %s reads table %s, which the SQL files drop or rename; under source: both the database's view stands, so change it in the database first",
				viewKey(parser.View{Schema: dep.ViewSchema, Name: dep.View}), tableRef{dep.Object.Schema, dep.Object.Name})
		}
	}
	return nil
}

func hasTable(s *parser.Schema, obj parser.DependencyObject) bool {
	for _, t := range s.Tables {
		if t.Schema == obj.Schema && parser.SameTableName("mysql", t.Name, obj.Name) {
			return true
		}
	}
	return false
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
