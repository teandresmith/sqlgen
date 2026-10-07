package introspect

import (
	"fmt"
	"slices"

	"github.com/teandresmith/sqlgen/parser"
)

// viewSQLParser is a dialect's ParseViewSQL.
type viewSQLParser func(sql []byte) (parser.ViewSQL, error)

// columnsFromDefinition sets view's columns from ddl, its CREATE VIEW
// statement (see viewColumnsFromDefinition), or keeps the catalog's and
// records a warning.
func columnsFromDefinition(view *parser.View, ddl string, parse viewSQLParser, schema *parser.Schema, catalogColumn func(i int) (parser.Column, bool)) {
	def, cols, err := viewColumnsFromDefinition(ddl, parse, schema, columnNames(view.Columns), catalogColumn)
	if err != nil {
		name := view.Name
		if view.Schema != "" {
			name = view.Schema + "." + view.Name
		}
		schema.Warnings = append(schema.Warnings, viewFallbackWarning(name, err))
		return
	}
	view.Columns, view.Definition = cols, def
}

// refreshViewColumns reads the columns of every view with a Definition
// again, against schema as the SQL files left it, so a migration that
// changes a table a view reads changes the view too. A column the
// definition cannot trace keeps the column it has, and a primary key from a
// materialized view's unique index stays.
func refreshViewColumns(schema *parser.Schema) error {
	for i := range schema.Views {
		view := &schema.Views[i]
		if view.Definition == nil {
			continue
		}
		built, err := parser.BuildView(*view.Definition, view.SQL, &parser.ViewAnnotations{}, schema)
		if err != nil {
			return fmt.Errorf("view %s: %w", view.Name, err)
		}
		for j, col := range built.Columns {
			if col.GoTypeLiteral == "any" {
				continue
			}
			if view.Columns[j].PrimaryKey {
				col.PrimaryKey, col.Nullable = true, false
			}
			view.Columns[j] = col
		}
	}
	return nil
}

// viewColumnsFromDefinition reads a view's columns from ddl, its CREATE VIEW
// statement, with the file view parser, so both sources describe a view the
// same way (PRD §16.2). The catalog cannot do that: PostgreSQL and SQLite
// report every view column nullable. A column the parser leaves `any` (an
// expression, or one read from a subquery or another view) takes the
// catalog's column instead, when catalogColumn has a type for it. It fails
// when the parser cannot read ddl or reads other column names than
// catalogNames, the catalog's.
func viewColumnsFromDefinition(ddl string, parse viewSQLParser, schema *parser.Schema, catalogNames []string, catalogColumn func(i int) (parser.Column, bool)) (*parser.ViewSQL, []parser.Column, error) {
	parsed, err := parse([]byte(ddl))
	if err != nil {
		return nil, nil, err
	}
	built, err := parser.BuildView(parsed, ddl, &parser.ViewAnnotations{}, schema)
	if err != nil {
		return nil, nil, fmt.Errorf("building view: %w", err)
	}
	names := columnNames(built.Columns)
	if !slices.Equal(names, catalogNames) {
		return nil, nil, fmt.Errorf("it reads columns %v where the catalog lists %v", names, catalogNames)
	}
	cols := built.Columns
	for i := range cols {
		if cols[i].GoTypeLiteral != "any" {
			continue
		}
		if col, ok := catalogColumn(i); ok {
			cols[i] = col
		}
	}
	return &parsed, cols, nil
}

// viewFallbackWarning names a view whose columns come from the catalog
// because its definition could not be read as a view file.
func viewFallbackWarning(view string, err error) string {
	return fmt.Sprintf("view %s: columns come from the catalog, since its definition cannot be read as a view file: %v", view, err)
}

func columnNames(cols []parser.Column) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}
