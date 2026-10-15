package introspect

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/parser"
)

// viewSQLParser is a dialect's ParseViewSQL.
type viewSQLParser func(sql []byte) (parser.ViewSQL, error)

// columnsFromDefinition sets view's columns from ddl, its CREATE VIEW
// statement (see viewColumnsFromDefinition), or keeps the catalog's and
// records a warning.
func columnsFromDefinition(view *parser.View, ddl string, parse viewSQLParser, schema *parser.Schema, searchPath parser.SearchPath, catalogColumn func(i int) (parser.Column, bool)) {
	def, cols, err := viewColumnsFromDefinition(ddl, parse, schema, searchPath, columnNames(view.Columns), catalogColumn)
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

// viewReads records what the introspected views read before the SQL files
// are applied.
type viewReads struct {
	// columns holds, by viewKey, the table column each column of a view
	// with a Definition reads (parser.ViewColumnSources); a zero columnRef
	// for a column that reads none.
	columns map[string][]columnRef
	// reads holds, by viewKey, the table column each column reference of a
	// view with a Definition reads, and the query whose FROM clause it reads
	// (parser.ViewReadSources); a zero columnRef for one that reads none.
	reads map[string][]readRef
	// from holds, by viewKey, the table each FROM-clause key of a view with
	// a Definition, or of a subquery it reads, names
	// (parser.ViewFromTables), for the keys that name a table.
	from map[string]map[string]tableRef
	// types holds every ENUM and SET type name, bare and schema-qualified.
	types map[string]bool
}

type tableRef struct{ schema, name string }

type columnRef struct {
	table  tableRef
	column string
}

// readRef is a columnRef a view's query reads outside its select list, and
// scope the query whose FROM clause it reads (parser.ViewReadSource).
type readRef struct {
	columnRef
	scope string
}

func (r tableRef) String() string {
	if r.schema == "" {
		return r.name
	}
	return r.schema + "." + r.name
}

func readViewSources(schema *parser.Schema) viewReads {
	reads := viewReads{
		columns: make(map[string][]columnRef),
		reads:   make(map[string][]readRef),
		from:    make(map[string]map[string]tableRef),
		types:   enumAndSetNames(schema),
	}
	for _, view := range schema.Views {
		if view.Definition == nil {
			continue
		}
		from := make(map[string]tableRef)
		for key, t := range parser.ViewFromTables(*view.Definition, schema) {
			if t != nil {
				from[key] = tableRef{t.Schema, t.Name}
			}
		}
		reads.from[viewKey(view)] = from
		for _, src := range parser.ViewColumnSources(*view.Definition, schema) {
			reads.columns[viewKey(view)] = append(reads.columns[viewKey(view)], newColumnRef(src))
		}
		for _, src := range parser.ViewReadSources(*view.Definition, schema) {
			reads.reads[viewKey(view)] = append(reads.reads[viewKey(view)], readRef{newColumnRef(src.ViewColumnSource), src.Scope})
		}
	}
	return reads
}

// newColumnRef returns the table column src reads, by name; a zero
// columnRef when it reads none.
func newColumnRef(src parser.ViewColumnSource) columnRef {
	if src.Table == nil {
		return columnRef{}
	}
	return columnRef{tableRef{src.Table.Schema, src.Table.Name}, src.Column}
}

func enumAndSetNames(schema *parser.Schema) map[string]bool {
	names := make(map[string]bool, 2*(len(schema.Enums)+len(schema.Sets)))
	add := func(typeSchema, name string) {
		names[name] = true
		if typeSchema != "" {
			names[typeSchema+"."+name] = true
		}
	}
	for _, e := range schema.Enums {
		add(e.Schema, e.Name)
	}
	for _, s := range schema.Sets {
		add(s.Schema, s.Name)
	}
	return names
}

func viewKey(view parser.View) string {
	if view.Schema == "" {
		return view.Name
	}
	return view.Schema + "." + view.Name
}

// refreshViewColumns reads the columns of every view with a Definition
// again, against schema as the SQL files left it, so a migration that
// changes a table a view reads changes the view too. A column the
// definition cannot trace keeps the column it has, and a primary key from a
// materialized view's unique index stays.
//
// A view that read a table the SQL files drop or rename, or a column that
// read a table column they drop or rename, is an error, since the
// database's view stands under source: both: PostgreSQL refuses to drop a
// table or column a view reads, SQLite refuses to drop such a column and
// leaves the view invalid when its table is dropped, and MySQL leaves the
// view invalid on any of them. A rename PostgreSQL and SQLite carry into
// the view has already been applied to its Definition by the file parser.
// A table or column read through a subquery, or a column read outside the
// select list, counts the same. A view whose columns come from the catalog
// cannot be traced, so it is an error only when one of its ENUM or SET
// types is gone.
func refreshViewColumns(schema *parser.Schema, searchPath parser.SearchPath, reads viewReads) error {
	types := enumAndSetNames(schema)
	for i := range schema.Views {
		view := &schema.Views[i]
		if view.Definition == nil {
			for _, col := range view.Columns {
				typ := strings.TrimRight(col.Type, "[]")
				if reads.types[typ] && !types[typ] {
					return fmt.Errorf("view %s: column %s has type %s, which the SQL files drop or rename; under source: both the database's view stands, so change it in the database first", viewKey(*view), col.Name, col.Type)
				}
			}
			continue
		}
		if err := checkViewReads(*view, schema, reads); err != nil {
			return err
		}
		built, err := parser.BuildView(*view.Definition, view.SQL, &parser.ViewAnnotations{}, schema, searchPath)
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

// checkViewReads returns an error for a table or table column that view,
// which has a Definition, read before the SQL files were applied and no
// longer finds.
func checkViewReads(view parser.View, schema *parser.Schema, reads viewReads) error {
	const stands = ", which the SQL files drop or rename; under source: both the database's view stands, so change it in the database first"
	from, nowFrom := reads.from[viewKey(view)], parser.ViewFromTables(*view.Definition, schema)
	for _, key := range slices.Sorted(maps.Keys(from)) {
		if nowFrom[key] == nil {
			return fmt.Errorf("view %s reads table %s%s", viewKey(view), from[key], stands)
		}
	}
	// Every table the view read still resolves, so a column that no longer
	// finds its source column lost it.
	now := parser.ViewColumnSources(*view.Definition, schema)
	for j, ref := range reads.columns[viewKey(view)] {
		if ref.table.name != "" && now[j].Table == nil {
			return fmt.Errorf("view %s: column %s reads column %s of table %s%s", viewKey(view), view.Definition.Columns[j].Alias, ref.column, ref.table, stands)
		}
	}
	// So does a column the query reads outside its select list. One that
	// now resolves in the FROM clause of a query further out lost its
	// column too: SQLite binds the unqualified name to that query's column
	// instead, which changes what the view means. Every scope a read
	// resolves in is on the path to its own query, so a shorter path is
	// further out.
	nowReads := parser.ViewReadSources(*view.Definition, schema)
	for j, ref := range reads.reads[viewKey(view)] {
		// A name a select-list alias stands in for binds to the alias
		// instead, unless a rename gave it another name.
		if r := nowReads[j].Ref; r.Alias != "" && parser.FoldASCII(r.Column) == r.Alias {
			continue
		}
		movedOut := len(nowReads[j].Scope) < len(ref.scope)
		if ref.table.name != "" && (nowReads[j].Table == nil || movedOut) {
			return fmt.Errorf("view %s reads column %s of table %s%s", viewKey(view), ref.column, ref.table, stands)
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
func viewColumnsFromDefinition(ddl string, parse viewSQLParser, schema *parser.Schema, searchPath parser.SearchPath, catalogNames []string, catalogColumn func(i int) (parser.Column, bool)) (*parser.ViewSQL, []parser.Column, error) {
	parsed, err := parse([]byte(ddl))
	if err != nil {
		return nil, nil, err
	}
	built, err := parser.BuildView(parsed, ddl, &parser.ViewAnnotations{}, schema, searchPath)
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
// because its definition could not be read as a view file. It gives only the
// reason, not a ViewParseError's advice to a view file's author: the view is
// the database's, and its catalog columns stand. A reason that names the
// view again calls it "it".
func viewFallbackWarning(view string, err error) string {
	reason := err.Error()
	if pe, ok := errors.AsType[*parser.ViewParseError](err); ok {
		reason = strings.TrimSuffix(reason, "; "+pe.Advice)
	}
	if qe, ok := errors.AsType[*parser.ViewQueryError](err); ok {
		if rest, ok := strings.CutPrefix(reason, "view "+qe.Name+" "); ok {
			reason = "it " + rest
		}
	}
	return fmt.Sprintf("view %s: columns come from the catalog, since its definition cannot be read as a view file: %s", view, reason)
}

func columnNames(cols []parser.Column) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}
