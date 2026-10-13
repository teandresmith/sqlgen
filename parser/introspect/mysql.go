package introspect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql" // MySQL database/sql driver

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
)

// MySQLIntrospector discovers schema elements from a live MySQL database.
type MySQLIntrospector struct {
	db *sql.DB
}

// NewMySQLIntrospector creates a new MySQL introspector.
func NewMySQLIntrospector() *MySQLIntrospector {
	return &MySQLIntrospector{}
}

// Name returns "mysql".
func (mi *MySQLIntrospector) Name() string { return "mysql" }

// Close releases the database connection.
func (mi *MySQLIntrospector) Close() error {
	if mi.db == nil {
		return nil
	}
	if err := mi.db.Close(); err != nil {
		return fmt.Errorf("closing mysql introspector: %w", err)
	}
	return nil
}

// Introspect connects to the MySQL database and populates schema with
// discovered tables, columns, constraints, and comments.
func (mi *MySQLIntrospector) Introspect(ctx context.Context, connString string, schema *parser.Schema, opts IntrospectionOptions) error {
	db, err := sql.Open("mysql", connString)
	if err != nil {
		return fmt.Errorf("introspect mysql: opening connection: %w", err)
	}
	mi.db = db

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("introspect mysql: ping: %w", err)
	}

	if err := mi.introspectTables(ctx, schema, opts); err != nil {
		return err
	}
	if err := mi.introspectViews(ctx, schema, opts); err != nil {
		return err
	}

	return nil
}

func (mi *MySQLIntrospector) introspectTables(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT table_name, COALESCE(table_comment, '')
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return fmt.Errorf("introspect mysql tables: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type tableRef struct {
		name    string
		comment string
	}
	var tables []tableRef

	for rows.Next() {
		var ref tableRef
		if err := rows.Scan(&ref.name, &ref.comment); err != nil {
			return fmt.Errorf("introspect mysql tables: scanning: %w", err)
		}
		if !tableAllowed(ref.name, opts) {
			continue
		}
		tables = append(tables, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql tables: iterating: %w", err)
	}

	for _, ref := range tables {
		table := parser.Table{
			Name:    ref.name,
			Comment: ref.comment,
		}

		if err := mi.introspectColumns(ctx, &table, schema); err != nil {
			return err
		}
		if err := mi.introspectConstraints(ctx, &table); err != nil {
			return err
		}
		parser.CanonicalizeConstraints(&table, "mysql")

		schema.Tables = append(schema.Tables, table)
	}

	return nil
}

func (mi *MySQLIntrospector) introspectViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT table_name, view_definition
		FROM information_schema.views
		WHERE table_schema = DATABASE()
		ORDER BY table_name`)
	if err != nil {
		return fmt.Errorf("introspect mysql views: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type viewRef struct {
		name       string
		definition string
	}
	var views []viewRef

	for rows.Next() {
		var ref viewRef
		var defn sql.NullString
		if err := rows.Scan(&ref.name, &defn); err != nil {
			return fmt.Errorf("introspect mysql views: scanning: %w", err)
		}
		if !tableAllowed(ref.name, opts) {
			continue
		}
		if defn.Valid {
			ref.definition = defn.String
		}
		views = append(views, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql views: iterating: %w", err)
	}

	for _, ref := range views {
		view := parser.View{
			Name: ref.name,
			SQL:  ref.definition,
		}

		catalog, err := mi.viewCatalogColumns(ctx, view.Name)
		if err != nil {
			return err
		}
		names := make([]string, len(catalog))
		for i, col := range catalog {
			names[i] = col.name
		}
		// MySQL keeps a view's column list out of view_definition, so the
		// statement names the catalog's columns, as a column list would.
		// A catalog ENUM / SET column may add a type to schema, so a catalog
		// column is built only where it is used.
		ddl := "CREATE VIEW " + quoteMySQLIdent(view.Name) + mysqlColumnList(names) + " AS " + ref.definition
		view.Definition, view.Columns, err = viewColumnsFromDefinition(ddl, mysql.ParseViewSQL, schema, names, func(i int) (parser.Column, bool) {
			return viewColumnFromCatalog(view.Name, catalog[i], schema), true
		})
		if err != nil {
			schema.Warnings = append(schema.Warnings, viewFallbackWarning(view.Name, err))
			view.Columns = make([]parser.Column, 0, len(catalog))
			for _, row := range catalog {
				view.Columns = append(view.Columns, viewColumnFromCatalog(view.Name, row, schema))
			}
		}

		schema.Views = append(schema.Views, view)
	}

	reuseViewSourceTypes(schema)
	return nil
}

// mysqlColumnList returns names as a CREATE VIEW column list, or "" when
// there are none.
func mysqlColumnList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteMySQLIdent(name)
	}
	return " (" + strings.Join(quoted, ", ") + ")"
}

// mysqlViewColumn is one row of a view's information_schema.columns.
type mysqlViewColumn struct {
	name, columnType string
	nullable         bool
}

func quoteMySQLIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// viewColumnFromCatalog builds a view column from its catalog row. An ENUM
// or SET column gets a type of the view's own, added to schema, which
// reuseViewSourceTypes may replace later.
func viewColumnFromCatalog(view string, row mysqlViewColumn, schema *parser.Schema) parser.Column {
	col := parser.Column{Name: row.name, Type: normalizeMySQLType(row.columnType), Nullable: row.nullable}
	switch col.Type {
	case "enum":
		col.Type = parser.InlineEnumName(view, col.Name)
		schema.Enums = append(schema.Enums, parser.Enum{Name: col.Type, Values: extractMySQLEnumValues(row.columnType)})
	case "set":
		col.Type = parser.InlineSetName(view, col.Name)
		schema.Sets = append(schema.Sets, parser.Set{Name: col.Type, Values: extractMySQLEnumValues(row.columnType)})
	}
	return col
}

func (mi *MySQLIntrospector) viewCatalogColumns(ctx context.Context, view string) ([]mysqlViewColumn, error) {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT
			column_name,
			column_type,
			is_nullable
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, view)
	if err != nil {
		return nil, fmt.Errorf("introspect mysql view columns %s: %w", view, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var cols []mysqlViewColumn
	for rows.Next() {
		var col mysqlViewColumn
		var isNullable string
		if err := rows.Scan(&col.name, &col.columnType, &isNullable); err != nil {
			return nil, fmt.Errorf("introspect mysql view columns %s: scanning: %w", view, err)
		}
		col.nullable = isNullable == "YES"
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect mysql view columns %s: iterating: %w", view, err)
	}
	return cols, nil
}

// reuseViewSourceTypes gives a view's ENUM or SET column that projects
// another view's column unchanged the type of that column, through any
// chain of views, so one ENUM or SET column has one type (PRD §8). A
// table's column already has its table's type, since the view's definition
// resolves it. It runs once every view is read, so the result does not
// depend on the order views are read in, and it drops the view types no
// column uses any longer.
func reuseViewSourceTypes(schema *parser.Schema) {
	views := make(map[string]*parser.View, len(schema.Views))
	for i := range schema.Views {
		views[schema.Views[i].Name] = &schema.Views[i]
	}
	unused := make(map[string]bool)

	// resolve returns the type of view's column i, after reusing its
	// source's. depth stops a cycle MySQL would refuse anyway.
	var resolve func(view *parser.View, i, depth int) string
	resolve = func(view *parser.View, i, depth int) string {
		col := &view.Columns[i]
		isSet := col.Type == parser.InlineSetName(view.Name, col.Name)
		own := isSet || col.Type == parser.InlineEnumName(view.Name, col.Name)
		if !own || depth > len(views) {
			return col.Type
		}
		source, j, ok := sourceViewColumn(view, i, views)
		if !ok {
			return col.Type
		}
		typ := resolve(source, j, depth+1)
		values, found := inlineTypeValues(schema, typ, isSet)
		ownValues, _ := inlineTypeValues(schema, col.Type, isSet)
		if typ != col.Type && found && slices.Equal(values, ownValues) {
			unused[col.Type] = true
			col.Type = typ
		}
		return col.Type
	}
	for i := range schema.Views {
		for j := range schema.Views[i].Columns {
			resolve(&schema.Views[i], j, 0)
		}
	}

	schema.Enums = slices.DeleteFunc(schema.Enums, func(e parser.Enum) bool { return unused[e.Name] })
	schema.Sets = slices.DeleteFunc(schema.Sets, func(s parser.Set) bool { return unused[s.Name] })
}

// sourceViewColumn returns the view in views, and the index of its column,
// that view's column i projects unchanged, read from view's definition. ok
// is false when the column is an expression, comes from a table or a
// derived table, or view has no definition.
func sourceViewColumn(view *parser.View, i int, views map[string]*parser.View) (source *parser.View, j int, ok bool) {
	if view.Definition == nil || i >= len(view.Definition.Columns) {
		return nil, 0, false
	}
	c := view.Definition.Columns[i]
	if c.SourceColumn == "" || c.Aggregate != "" {
		return nil, 0, false
	}
	source, ok = views[view.Definition.TableAliases[c.SourceTable]]
	if !ok {
		return nil, 0, false
	}
	j = slices.IndexFunc(source.Columns, func(col parser.Column) bool { return col.Name == c.SourceColumn })
	return source, j, j >= 0
}

// inlineTypeValues returns the values of the SET (isSet) or ENUM named name
// in schema.
func inlineTypeValues(schema *parser.Schema, name string, isSet bool) ([]string, bool) {
	if isSet {
		i := slices.IndexFunc(schema.Sets, func(s parser.Set) bool { return s.Name == name })
		if i < 0 {
			return nil, false
		}
		return schema.Sets[i].Values, true
	}
	i := slices.IndexFunc(schema.Enums, func(e parser.Enum) bool { return e.Name == name })
	if i < 0 {
		return nil, false
	}
	return schema.Enums[i].Values, true
}

func (mi *MySQLIntrospector) introspectColumns(ctx context.Context, table *parser.Table, schema *parser.Schema) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT
			column_name,
			column_type,
			is_nullable,
			column_default,
			extra,
			COALESCE(column_comment, ''),
			COALESCE(generation_expression, '')
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, table.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql columns %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			col        parser.Column
			columnType string
			isNullable string
			dflt       sql.NullString
			extra      string
			comment    string
			generation string
		)
		if err := rows.Scan(&col.Name, &columnType, &isNullable, &dflt, &extra, &comment, &generation); err != nil {
			return fmt.Errorf("introspect mysql columns %s: scanning: %w", table.Name, err)
		}

		col.Type = normalizeMySQLType(columnType)
		col.Nullable = isNullable == "YES"
		col.Comment = comment

		col.Default, err = mysqlDefault(dflt, extra, columnType)
		if err != nil {
			return fmt.Errorf("introspect mysql columns %s: column %s: %w", table.Name, col.Name, err)
		}

		lowerExtra := strings.ToLower(extra)
		if strings.Contains(lowerExtra, "auto_increment") {
			col.AutoIncrement = true
		}

		if storage, ok := mysqlGeneratedStorage(lowerExtra); ok {
			expr, err := mysql.NormalizeExpr(unescapeMySQLCatalogText(generation))
			if err != nil {
				return &SchemaError{Err: fmt.Errorf("introspect mysql columns %s: generated column %s: %w", table.Name, col.Name, err)}
			}
			col.GeneratedExpr = expr
			col.GeneratedStorage = storage
		}

		// Inline ENUM/SET columns get the table-scoped synthetic type name the
		// file parser gives them.
		lowerType := strings.ToLower(columnType)
		switch {
		case strings.HasPrefix(lowerType, "enum("):
			values := extractMySQLEnumValues(columnType)
			if len(values) > 0 {
				name := parser.InlineEnumName(table.Name, col.Name)
				col.Type = name
				schema.Enums = append(schema.Enums, parser.Enum{
					Name:   name,
					Values: values,
				})
			}
		case strings.HasPrefix(lowerType, "set("):
			values := extractMySQLEnumValues(columnType)
			if len(values) > 0 {
				name := parser.InlineSetName(table.Name, col.Name)
				col.Type = name
				schema.Sets = append(schema.Sets, parser.Set{
					Name:   name,
					Values: values,
				})
			}
		}

		table.Columns = append(table.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql columns %s: iterating: %w", table.Name, err)
	}
	return nil
}

// introspectConstraints reads the table's PRIMARY KEY, UNIQUE and FOREIGN KEY
// constraints. A UNIQUE index with an expression part, which has a NULL
// column_name, is skipped, as the DDL parser skips it (PRD §30.7).
func (mi *MySQLIntrospector) introspectConstraints(ctx context.Context, table *parser.Table) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT
			tc.constraint_name,
			tc.constraint_type,
			kcu.column_name,
			COALESCE(kcu.referenced_table_name, ''),
			COALESCE(kcu.referenced_column_name, '')
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
			AND tc.table_name = kcu.table_name
		WHERE tc.table_schema = DATABASE()
		  AND tc.table_name = ?
		  AND tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY')
		  AND (tc.constraint_type <> 'UNIQUE' OR NOT EXISTS (
			SELECT 1 FROM information_schema.statistics s
			WHERE s.table_schema = tc.table_schema
			  AND s.table_name = tc.table_name
			  AND s.index_name = tc.constraint_name
			  AND s.column_name IS NULL))
		ORDER BY tc.constraint_name, kcu.ordinal_position`, table.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql constraints %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	// Group constraint columns by constraint name.
	type conRow struct {
		name   string
		ctype  string
		col    string
		refTbl string
		refCol string
	}
	var conRows []conRow

	for rows.Next() {
		var r conRow
		if err := rows.Scan(&r.name, &r.ctype, &r.col, &r.refTbl, &r.refCol); err != nil {
			return fmt.Errorf("introspect mysql constraints %s: scanning: %w", table.Name, err)
		}
		conRows = append(conRows, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql constraints %s: iterating: %w", table.Name, err)
	}

	// Build constraints from grouped rows.
	constraints := make(map[string]*parser.Constraint)
	var order []string

	for _, r := range conRows {
		c, ok := constraints[r.name]
		if !ok {
			c = &parser.Constraint{Name: r.name}
			switch r.ctype {
			case "PRIMARY KEY":
				c.Type = parser.PrimaryKey
			case "UNIQUE":
				c.Type = parser.Unique
			case "FOREIGN KEY":
				c.Type = parser.ForeignKey
				c.ReferenceTable = r.refTbl
			}
			constraints[r.name] = c
			order = append(order, r.name)
		}
		c.Columns = append(c.Columns, r.col)
		if c.Type == parser.ForeignKey && r.refCol != "" {
			c.ReferenceColumns = append(c.ReferenceColumns, r.refCol)
		}
	}

	for _, name := range order {
		table.Constraints = append(table.Constraints, *constraints[name])
	}

	if err := mi.applyIndexMetadata(ctx, table); err != nil {
		return err
	}

	// Check constraints (MySQL 8.0.16+).
	return mi.introspectCheckConstraints(ctx, table)
}

// applyIndexMetadata patches index access methods onto the PRIMARY KEY /
// UNIQUE constraints introspectConstraints produced, then emits parser.Index
// entries for every non-unique index. Both read INFORMATION_SCHEMA.STATISTICS,
// which is the only catalog surfacing plain CREATE INDEX / KEY definitions.
func (mi *MySQLIntrospector) applyIndexMetadata(ctx context.Context, table *parser.Table) error {
	if err := mi.applyIndexMethods(ctx, table); err != nil {
		return err
	}
	if err := mi.introspectNonUniqueIndexes(ctx, table); err != nil {
		return err
	}
	return mi.introspectExpressionIndexes(ctx, table)
}

// introspectExpressionIndexes reads the keys with an expression part (MySQL
// 8.0.13+), which the constraints leave out, unique or not. Each part is its
// column, or "" for an expression part, which has a NULL column_name.
func (mi *MySQLIntrospector) introspectExpressionIndexes(ctx context.Context, table *parser.Table) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT s.index_name, s.column_name
		FROM information_schema.statistics s
		WHERE s.table_schema = DATABASE()
		  AND s.table_name = ?
		  AND EXISTS (
			SELECT 1 FROM information_schema.statistics e
			WHERE e.table_schema = s.table_schema
			  AND e.table_name = s.table_name
			  AND e.index_name = s.index_name
			  AND e.column_name IS NULL)
		ORDER BY s.index_name, s.seq_in_index`, table.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql expression indexes %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			name string
			col  sql.NullString
		)
		if err := rows.Scan(&name, &col); err != nil {
			return fmt.Errorf("introspect mysql expression indexes %s: scanning: %w", table.Name, err)
		}
		n := len(table.ExpressionIndexes)
		if n == 0 || table.ExpressionIndexes[n-1].Name != name {
			table.ExpressionIndexes = append(table.ExpressionIndexes, parser.ExpressionIndex{Name: name})
			n++
		}
		table.ExpressionIndexes[n-1].Columns = append(table.ExpressionIndexes[n-1].Columns, col.String)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql expression indexes %s: iterating: %w", table.Name, err)
	}
	return nil
}

// applyIndexMethods reads INFORMATION_SCHEMA.STATISTICS.INDEX_TYPE for every
// index on the table and populates Constraint.Method on the matching PRIMARY
// KEY / UNIQUE entries that introspectConstraints already produced. MySQL has
// no partial-index syntax, so Where stays empty for that dialect.
func (mi *MySQLIntrospector) applyIndexMethods(ctx context.Context, table *parser.Table) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT DISTINCT index_name, LOWER(COALESCE(index_type, ''))
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ?`, table.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql index methods %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	methods := make(map[string]string)
	for rows.Next() {
		var name, method string
		if err := rows.Scan(&name, &method); err != nil {
			return fmt.Errorf("introspect mysql index methods %s: scanning: %w", table.Name, err)
		}
		methods[name] = method
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql index methods %s: iterating: %w", table.Name, err)
	}

	for i := range table.Constraints {
		c := &table.Constraints[i]
		// Method intentionally left unset on PRIMARY KEY — the DDL parser
		// (parser/mysql/mysql.go::parseIndex) only populates Method on
		// Unique + Index constraints, and the manifest builder only consumes
		// Method from indexes[] (PRD §30.7). Keeping introspect aligned with
		// DDL output here avoids path-dependent manifest drift.
		if c.Type != parser.Unique && c.Type != parser.Index {
			continue
		}
		if m, ok := methods[c.Name]; ok {
			c.Method = m
		}
	}

	return nil
}

// introspectNonUniqueIndexes emits parser.Index constraints for every
// non-unique index on the table. Plain CREATE INDEX / KEY definitions surface
// only in INFORMATION_SCHEMA.STATISTICS — TABLE_CONSTRAINTS covers only
// PRIMARY KEY / UNIQUE / FOREIGN KEY, and applyIndexMethods only patches Method
// onto existing rows. Without this sweep an introspection-sourced manifest
// drops every non-unique index the DDL parser emits (PRD §30.7). MySQL has no
// partial-index syntax, so Where stays empty; index_type feeds Method to match
// parser/mysql/mysql.go::parseIndex.
func (mi *MySQLIntrospector) introspectNonUniqueIndexes(ctx context.Context, table *parser.Table) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT index_name, column_name, LOWER(COALESCE(index_type, ''))
		FROM information_schema.statistics
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND non_unique = 1
		ORDER BY index_name, seq_in_index`, table.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql non-unique indexes %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	indexes := make(map[string]*parser.Constraint)
	exprIndexes := make(map[string]bool)
	var order []string
	for rows.Next() {
		var (
			name   string
			col    sql.NullString
			method string
		)
		if err := rows.Scan(&name, &col, &method); err != nil {
			return fmt.Errorf("introspect mysql non-unique indexes %s: scanning: %w", table.Name, err)
		}
		// An expression part (MySQL 8.0.13+) has a NULL column_name. Its
		// index is skipped, as the DDL parser skips it (PRD §30.7).
		if !col.Valid {
			exprIndexes[name] = true
			continue
		}
		c, ok := indexes[name]
		if !ok {
			c = &parser.Constraint{Name: name, Type: parser.Index, Method: method}
			indexes[name] = c
			order = append(order, name)
		}
		c.Columns = append(c.Columns, col.String)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql non-unique indexes %s: iterating: %w", table.Name, err)
	}

	for _, name := range order {
		if !exprIndexes[name] {
			table.Constraints = append(table.Constraints, *indexes[name])
		}
	}
	return nil
}

func (mi *MySQLIntrospector) introspectCheckConstraints(ctx context.Context, table *parser.Table) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT cc.constraint_name, cc.check_clause
		FROM information_schema.check_constraints cc
		JOIN information_schema.table_constraints tc
			ON cc.constraint_name = tc.constraint_name
			AND cc.constraint_schema = tc.constraint_schema
		WHERE tc.table_schema = DATABASE()
		  AND tc.table_name = ?
		  AND tc.constraint_type = 'CHECK'`, table.Name)
	if err != nil {
		return checkQueryError(table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var name, clause string
		if err := rows.Scan(&name, &clause); err != nil {
			return fmt.Errorf("introspect mysql check constraints %s: scanning: %w", table.Name, err)
		}
		expr, err := mysql.NormalizeExpr(unescapeMySQLCatalogText(clause))
		if err != nil {
			return &SchemaError{Err: fmt.Errorf("introspect mysql check constraints %s: %s: %w", table.Name, name, err)}
		}
		table.Constraints = append(table.Constraints, parser.Constraint{
			Name:            name,
			Type:            parser.Check,
			CheckExpression: expr,
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql check constraints %s: iterating: %w", table.Name, err)
	}
	return nil
}

// mysqlErrUnknownTable is ER_UNKNOWN_TABLE. A server older than MySQL 8.0.16
// has no information_schema.check_constraints and answers the CHECK query
// with it.
const mysqlErrUnknownTable = 1109

// checkQueryError returns the error to report for a failed CHECK query: none
// when the server has no CHECK catalog, so it has no CHECKs to read, and err
// wrapped otherwise.
func checkQueryError(table string, err error) error {
	if myErr, ok := errors.AsType[*mysqldriver.MySQLError](err); ok && myErr.Number == mysqlErrUnknownTable {
		return nil
	}
	return fmt.Errorf("introspect mysql check constraints %s: %w", table, err)
}

// normalizeMySQLType normalizes a MySQL COLUMN_TYPE to match the parser format.
// Enum and set types become "enum" and "set".
func normalizeMySQLType(columnType string) string {
	lower := strings.ToLower(columnType)
	if strings.HasPrefix(lower, "enum(") {
		return "enum"
	}
	if strings.HasPrefix(lower, "set(") {
		return "set"
	}
	return mysql.NormalizeType(lower)
}

// mysqlStringTypes are the column types whose literal default
// information_schema.columns.column_default stores as the bare string value.
var mysqlStringTypes = map[string]bool{
	"char": true, "varchar": true, "tinytext": true, "text": true, "mediumtext": true, "longtext": true,
	"enum": true, "set": true, "date": true, "datetime": true, "timestamp": true, "time": true,
}

// mysqlCurrentTimestamp matches a CURRENT_TIMESTAMP default as COLUMN_DEFAULT
// stores it, with or without a fractional-seconds precision. MySQL stores NOW()
// and LOCALTIMESTAMP as CURRENT_TIMESTAMP too.
var mysqlCurrentTimestamp = regexp.MustCompile(`(?i)^current_timestamp(\(\d\))?$`)

// mysqlDefault returns a column's default in the parser's form, from its
// information_schema.columns COLUMN_DEFAULT, EXTRA and COLUMN_TYPE.
//   - An expression default (EXTRA has DEFAULT_GENERATED) is escaped like
//     GENERATION_EXPRESSION, and is normalized like it.
//   - A datetime or timestamp column's CURRENT_TIMESTAMP default is an
//     expression too. MySQL before 8.0.13 does not mark it DEFAULT_GENERATED,
//     and no such column can hold the literal text CURRENT_TIMESTAMP.
//   - A string or temporal column's literal default is the bare value, which
//     is quoted again. An empty string is a default too.
//   - Any other literal (a number, b'101', a binary column's 0x6162) is SQL
//     text already.
func mysqlDefault(dflt sql.NullString, extra, columnType string) (string, error) {
	if !dflt.Valid {
		return "", nil
	}
	text := dflt.String
	if strings.Contains(strings.ToLower(extra), "default_generated") {
		text = unescapeMySQLCatalogText(text)
	} else {
		base := strings.ToLower(columnType)
		if i := strings.IndexAny(base, "( "); i >= 0 {
			base = base[:i]
		}
		temporalNow := (base == "datetime" || base == "timestamp") && mysqlCurrentTimestamp.MatchString(text)
		if mysqlStringTypes[base] && !temporalNow {
			return mysql.QuoteLiteral(text), nil
		}
	}
	expr, err := mysql.NormalizeExpr(text)
	if err != nil {
		return "", &SchemaError{Err: fmt.Errorf("normalizing default: %w", err)}
	}
	return expr, nil
}

// extractMySQLEnumValues extracts enum or set values from a MySQL COLUMN_TYPE
// like "enum('active','in,active')". COLUMN_TYPE writes each value as a string
// literal, so a value may hold a comma, a doubled quote or a backslash escape,
// and may be empty.
func extractMySQLEnumValues(columnType string) []string {
	start := strings.Index(columnType, "(")
	end := strings.LastIndex(columnType, ")")
	if start < 0 || end <= start {
		return nil
	}
	inner := columnType[start+1 : end]

	var values []string
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\'' {
			continue // the comma between two literals
		}
		value, next := readMySQLStringLiteral(inner, i)
		values = append(values, value)
		i = next
	}
	return values
}

// readMySQLStringLiteral decodes the single-quoted literal that opens at
// s[open] and returns its value and the index of its closing quote.
func readMySQLStringLiteral(s string, open int) (string, int) {
	var b strings.Builder
	i := open + 1
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			b.WriteByte('\'')
			i++
		case c == '\'':
			return b.String(), i
		case c == '\\' && i+1 < len(s):
			i++
			b.WriteString(unescapeMySQL(s[i]))
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), i
}

// unescapeMySQL returns what the escape sequence \c stands for in a MySQL
// string literal. \% and \_ keep their backslash, as MySQL does.
func unescapeMySQL(c byte) string {
	switch c {
	case '0':
		return "\x00"
	case 'b':
		return "\b"
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	case 'Z':
		return "\x1a"
	case '%', '_':
		return "\\" + string(c)
	default:
		return string(c)
	}
}

// mysqlGeneratedStorage reports a generated column's storage from its
// lowercased information_schema.columns.extra, which reads "virtual
// generated" or "stored generated".
func mysqlGeneratedStorage(extra string) (string, bool) {
	switch {
	case strings.Contains(extra, "stored generated"):
		return "STORED", true
	case strings.Contains(extra, "virtual generated"):
		return "VIRTUAL", true
	}
	return "", false
}

// unescapeMySQLCatalogText undoes the backslash escaping information_schema
// applies to expression text: a quote inside an expression is stored as \'
// and a backslash as \\.
func unescapeMySQLCatalogText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
