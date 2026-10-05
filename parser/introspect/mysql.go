package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql" // MySQL database/sql driver

	"github.com/teandresmith/sqlgen/parser"
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

		if err := mi.introspectViewColumns(ctx, &view); err != nil {
			return err
		}

		schema.Views = append(schema.Views, view)
	}

	return nil
}

func (mi *MySQLIntrospector) introspectViewColumns(ctx context.Context, view *parser.View) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT
			column_name,
			column_type,
			is_nullable
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, view.Name)
	if err != nil {
		return fmt.Errorf("introspect mysql view columns %s: %w", view.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			col        parser.Column
			columnType string
			isNullable string
		)
		if err := rows.Scan(&col.Name, &columnType, &isNullable); err != nil {
			return fmt.Errorf("introspect mysql view columns %s: scanning: %w", view.Name, err)
		}

		col.Type = normalizeMySQLType(columnType)
		col.Nullable = isNullable == "YES"

		view.Columns = append(view.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql view columns %s: iterating: %w", view.Name, err)
	}
	return nil
}

func (mi *MySQLIntrospector) introspectColumns(ctx context.Context, table *parser.Table, schema *parser.Schema) error {
	rows, err := mi.db.QueryContext(ctx, `
		SELECT
			column_name,
			column_type,
			is_nullable,
			column_default,
			extra,
			COALESCE(column_comment, '')
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
		)
		if err := rows.Scan(&col.Name, &columnType, &isNullable, &dflt, &extra, &comment); err != nil {
			return fmt.Errorf("introspect mysql columns %s: scanning: %w", table.Name, err)
		}

		col.Type = normalizeMySQLType(columnType)
		col.Nullable = isNullable == "YES"
		col.Comment = comment

		if dflt.Valid {
			col.Default = dflt.String
		}

		if strings.Contains(strings.ToLower(extra), "auto_increment") {
			col.AutoIncrement = true
		}

		// Inline ENUM/SET columns get a table-scoped synthetic type name to match
		// parser/mysql/mysql.go::extractInlineEnumOrSet and prevent cross-table collisions.
		lowerType := strings.ToLower(columnType)
		switch {
		case strings.HasPrefix(lowerType, "enum("):
			values := extractMySQLEnumValues(columnType)
			if len(values) > 0 {
				name := table.Name + "_" + col.Name + "_enum"
				col.Type = name
				schema.Enums = append(schema.Enums, parser.Enum{
					Name:   name,
					Values: values,
				})
			}
		case strings.HasPrefix(lowerType, "set("):
			values := extractMySQLEnumValues(columnType)
			if len(values) > 0 {
				name := table.Name + "_" + col.Name + "_set"
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

func (mi *MySQLIntrospector) introspectConstraints(ctx context.Context, table *parser.Table) error {
	// Primary key and unique constraints.
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
	if err := mi.introspectCheckConstraints(ctx, table); err != nil {
		return err
	}

	parser.ApplyTableConstraints(table)
	return nil
}

// applyIndexMetadata patches index access methods onto the PRIMARY KEY /
// UNIQUE constraints introspectConstraints produced, then emits parser.Index
// entries for every non-unique index. Both read INFORMATION_SCHEMA.STATISTICS,
// which is the only catalog surfacing plain CREATE INDEX / KEY definitions.
func (mi *MySQLIntrospector) applyIndexMetadata(ctx context.Context, table *parser.Table) error {
	if err := mi.applyIndexMethods(ctx, table); err != nil {
		return err
	}
	return mi.introspectNonUniqueIndexes(ctx, table)
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
		// Expression index parts (MySQL 8.0.13+) report a NULL column_name and
		// have no projectable column — skip them to match the DDL parser and
		// the PostgreSQL standalone-index sweep.
		if !col.Valid {
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
		table.Constraints = append(table.Constraints, *indexes[name])
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
		// Older MySQL versions may not have check_constraints table.
		return nil //nolint:nilerr // gracefully degrade on older MySQL
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var name, clause string
		if err := rows.Scan(&name, &clause); err != nil {
			return fmt.Errorf("introspect mysql check constraints %s: scanning: %w", table.Name, err)
		}
		table.Constraints = append(table.Constraints, parser.Constraint{
			Name:            name,
			Type:            parser.Check,
			CheckExpression: clause,
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect mysql check constraints %s: iterating: %w", table.Name, err)
	}
	return nil
}

// normalizeMySQLType normalizes a MySQL COLUMN_TYPE to match the parser format.
// Enum types become "enum"; everything else is lowercased as-is.
func normalizeMySQLType(columnType string) string {
	lower := strings.ToLower(columnType)
	if strings.HasPrefix(lower, "enum(") {
		return "enum"
	}
	if strings.HasPrefix(lower, "set(") {
		return "set"
	}
	return lower
}

// extractMySQLEnumValues extracts enum values from a MySQL COLUMN_TYPE like
// "enum('active','inactive','pending')".
func extractMySQLEnumValues(columnType string) []string {
	// Find the content between first ( and last ).
	start := strings.Index(columnType, "(")
	end := strings.LastIndex(columnType, ")")
	if start < 0 || end <= start {
		return nil
	}
	inner := columnType[start+1 : end]

	var values []string
	for part := range strings.SplitSeq(inner, ",") {
		v := strings.TrimSpace(part)
		v = strings.Trim(v, "'")
		if v != "" {
			values = append(values, v)
		}
	}
	return values
}
