package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL database/sql driver

	"github.com/teandresmith/sqlgen/parser"
)

// PostgresIntrospector discovers schema elements from a live PostgreSQL database.
type PostgresIntrospector struct {
	db *sql.DB
}

// NewPostgresIntrospector creates a new PostgreSQL introspector.
func NewPostgresIntrospector() *PostgresIntrospector {
	return &PostgresIntrospector{}
}

// Name returns "postgres".
func (pi *PostgresIntrospector) Name() string { return "postgres" }

// Close releases the database connection.
func (pi *PostgresIntrospector) Close() error {
	if pi.db == nil {
		return nil
	}
	if err := pi.db.Close(); err != nil {
		return fmt.Errorf("closing postgres introspector: %w", err)
	}
	return nil
}

// Introspect connects to the PostgreSQL database and populates schema with
// discovered tables, columns, constraints, enums, composite types, domain
// types, and comments.
func (pi *PostgresIntrospector) Introspect(ctx context.Context, connString string, schema *parser.Schema, opts IntrospectionOptions) error {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return fmt.Errorf("introspect postgres: opening connection: %w", err)
	}
	pi.db = db

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("introspect postgres: ping: %w", err)
	}

	if err := pi.introspectEnums(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectCompositeTypes(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectDomainTypes(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectTables(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectViews(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectMaterializedViews(ctx, schema, opts); err != nil {
		return err
	}

	return nil
}

func (pi *PostgresIntrospector) introspectTables(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_type = 'BASE TABLE'
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name`)
	if err != nil {
		return fmt.Errorf("introspect postgres tables: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type tableRef struct {
		schema string
		name   string
	}
	var tables []tableRef

	for rows.Next() {
		var ref tableRef
		if err := rows.Scan(&ref.schema, &ref.name); err != nil {
			return fmt.Errorf("introspect postgres tables: scanning: %w", err)
		}
		if !schemaAllowed(ref.schema, opts) || !tableAllowed(ref.name, opts) {
			continue
		}
		tables = append(tables, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres tables: iterating: %w", err)
	}

	for _, ref := range tables {
		table := parser.Table{
			Schema: ref.schema,
			Name:   ref.name,
		}

		if err := pi.introspectColumns(ctx, &table); err != nil {
			return err
		}
		if err := pi.introspectConstraints(ctx, &table); err != nil {
			return err
		}
		if err := pi.introspectTableComment(ctx, &table); err != nil {
			return err
		}
		if err := pi.introspectColumnComments(ctx, &table); err != nil {
			return err
		}

		schema.Tables = append(schema.Tables, table)
	}

	return nil
}

func (pi *PostgresIntrospector) introspectViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT table_schema, table_name, view_definition
		FROM information_schema.views
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name`)
	if err != nil {
		return fmt.Errorf("introspect postgres views: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type viewRef struct {
		schema     string
		name       string
		definition string
	}
	var views []viewRef

	for rows.Next() {
		var ref viewRef
		var defn sql.NullString
		if err := rows.Scan(&ref.schema, &ref.name, &defn); err != nil {
			return fmt.Errorf("introspect postgres views: scanning: %w", err)
		}
		if !schemaAllowed(ref.schema, opts) || !tableAllowed(ref.name, opts) {
			continue
		}
		if defn.Valid {
			ref.definition = defn.String
		}
		views = append(views, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres views: iterating: %w", err)
	}

	for _, ref := range views {
		view := parser.View{
			Schema: ref.schema,
			Name:   ref.name,
			SQL:    ref.definition,
		}

		if err := pi.introspectViewColumns(ctx, &view); err != nil {
			return err
		}

		schema.Views = append(schema.Views, view)
	}

	return nil
}

// introspectMaterializedViews discovers PostgreSQL materialized views. They
// are invisible to information_schema.views (the introspectViews pass) and
// live in pg_matviews instead. Column metadata is read with the shared
// introspectViewColumns query, which already matches relkind = 'm'. The
// pg_matviews.definition populates View.SQL for documentation purposes only —
// it is never re-injected at query time (same contract as regular views).
func (pi *PostgresIntrospector) introspectMaterializedViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT schemaname, matviewname, definition
		FROM pg_catalog.pg_matviews
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, matviewname`)
	if err != nil {
		return fmt.Errorf("introspect postgres materialized views: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type viewRef struct {
		schema     string
		name       string
		definition string
	}
	var matviews []viewRef

	for rows.Next() {
		var ref viewRef
		var defn sql.NullString
		if err := rows.Scan(&ref.schema, &ref.name, &defn); err != nil {
			return fmt.Errorf("introspect postgres materialized views: scanning: %w", err)
		}
		if !schemaAllowed(ref.schema, opts) || !tableAllowed(ref.name, opts) {
			continue
		}
		if defn.Valid {
			ref.definition = defn.String
		}
		matviews = append(matviews, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres materialized views: iterating: %w", err)
	}

	for _, ref := range matviews {
		view := parser.View{
			Schema:       ref.schema,
			Name:         ref.name,
			SQL:          ref.definition,
			Materialized: true,
		}

		if err := pi.introspectViewColumns(ctx, &view); err != nil {
			return err
		}
		if err := pi.introspectMatviewUniqueIndexes(ctx, &view); err != nil {
			return err
		}

		schema.Views = append(schema.Views, view)
	}

	return nil
}

// matviewUniqueIndex is one qualifying unique index discovered on a
// materialized view: full-row (no partial WHERE) and plain-column (no
// expressions), the shape REFRESH MATERIALIZED VIEW CONCURRENTLY requires.
type matviewUniqueIndex struct {
	name    string
	columns []string
}

// introspectMatviewUniqueIndexes discovers the matview's qualifying unique
// indexes. Any qualifying index makes the matview concurrently refreshable;
// the deterministically chosen one (chooseMatviewPK) marks its columns as the
// primary key so the generator emits Get.
func (pi *PostgresIntrospector) introspectMatviewUniqueIndexes(ctx context.Context, view *parser.View) error {
	// indpred IS NULL excludes partial indexes (not unique across all rows);
	// indexprs IS NULL excludes expression indexes (no plain columns to act
	// as a key). k.ord <= indnkeyatts drops INCLUDE-only payload columns.
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			ic.relname,
			COALESCE(
				(SELECT array_agg(att.attname ORDER BY k.ord)
				 FROM unnest(ix.indkey::smallint[]) WITH ORDINALITY AS k(attnum, ord)
				 JOIN pg_catalog.pg_attribute att
				   ON att.attrelid = ix.indrelid AND att.attnum = k.attnum
				 WHERE k.ord <= ix.indnkeyatts),
				'{}'
			)
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class ic ON ic.oid = ix.indexrelid
		JOIN pg_catalog.pg_class m ON m.oid = ix.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = m.relnamespace
		WHERE m.relkind = 'm'
		  AND n.nspname = $1 AND m.relname = $2
		  AND ix.indisunique
		  AND ix.indisvalid AND ix.indisready
		  AND ix.indpred IS NULL
		  AND ix.indexprs IS NULL
		ORDER BY ic.relname`, view.Schema, view.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres matview indexes %s.%s: %w", view.Schema, view.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var indexes []matviewUniqueIndex
	for rows.Next() {
		var (
			name    string
			colsStr string
		)
		if err := rows.Scan(&name, &colsStr); err != nil {
			return fmt.Errorf("introspect postgres matview indexes %s.%s: scanning: %w", view.Schema, view.Name, err)
		}
		cols := parsePostgresArray(colsStr)
		if len(cols) == 0 {
			continue
		}
		indexes = append(indexes, matviewUniqueIndex{name: name, columns: cols})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres matview indexes %s.%s: iterating: %w", view.Schema, view.Name, err)
	}

	if len(indexes) == 0 {
		return nil
	}
	view.ConcurrentlyRefreshable = true

	pk := chooseMatviewPK(indexes)
	for i := range view.Columns {
		if slices.Contains(pk, view.Columns[i].Name) {
			view.Columns[i].PrimaryKey = true
			view.Columns[i].Nullable = false
		}
	}
	return nil
}

// chooseMatviewPK deterministically selects the unique index whose columns act
// as the materialized view's primary key: fewest columns first, ties broken by
// index name ascending.
func chooseMatviewPK(indexes []matviewUniqueIndex) []string {
	best := indexes[0]
	for _, idx := range indexes[1:] {
		fewerColumns := len(idx.columns) < len(best.columns)
		tieByName := len(idx.columns) == len(best.columns) && idx.name < best.name
		if fewerColumns || tieByName {
			best = idx
		}
	}
	return best.columns
}

func (pi *PostgresIntrospector) introspectViewColumns(ctx context.Context, view *parser.View) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum`, view.Schema, view.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres view columns %s.%s: %w", view.Schema, view.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			col        parser.Column
			formatType string
			nullable   bool
		)
		if err := rows.Scan(&col.Name, &formatType, &nullable); err != nil {
			return fmt.Errorf("introspect postgres view columns %s.%s: scanning: %w", view.Schema, view.Name, err)
		}

		col.Type = normalizePostgresType(formatType)
		col.Nullable = nullable

		view.Columns = append(view.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres view columns %s.%s: iterating: %w", view.Schema, view.Name, err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectColumns(ctx context.Context, table *parser.Table) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			pg_get_expr(ad.adbin, ad.adrelid),
			a.attidentity
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		LEFT JOIN pg_catalog.pg_attrdef ad ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum`, table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres columns %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			col        parser.Column
			formatType string
			nullable   bool
			dflt       sql.NullString
			identity   string
		)
		if err := rows.Scan(&col.Name, &formatType, &nullable, &dflt, &identity); err != nil {
			return fmt.Errorf("introspect postgres columns %s.%s: scanning: %w", table.Schema, table.Name, err)
		}

		col.Type = normalizePostgresType(formatType)
		col.Nullable = nullable

		if dflt.Valid {
			col.Default = dflt.String
			if strings.Contains(strings.ToLower(col.Default), "nextval(") {
				col.AutoIncrement = true
			}
		}

		if identity != "" {
			col.AutoIncrement = true
		}

		table.Columns = append(table.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres columns %s.%s: iterating: %w", table.Schema, table.Name, err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectConstraints(ctx context.Context, table *parser.Table) error {
	// Track the OIDs of indexes already covered by a pg_constraint so the
	// non-constraint pg_index sweep below can skip them.
	conIndIDs, err := pi.introspectConstraintsViaPgConstraint(ctx, table)
	if err != nil {
		return err
	}
	if err := pi.introspectStandaloneIndexes(ctx, table, conIndIDs); err != nil {
		return err
	}
	parser.ApplyTableConstraints(table)
	return nil
}

func (pi *PostgresIntrospector) introspectConstraintsViaPgConstraint(ctx context.Context, table *parser.Table) (map[int64]struct{}, error) {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			con.conname,
			con.contype,
			array_agg(att.attname ORDER BY u.ord) AS columns,
			COALESCE(refns.nspname, ''),
			COALESCE(refc.relname, ''),
			COALESCE(
				(SELECT array_agg(ratt.attname ORDER BY ru.ord)
				 FROM unnest(con.confkey) WITH ORDINALITY AS ru(attnum, ord)
				 JOIN pg_catalog.pg_attribute ratt ON ratt.attrelid = con.confrelid AND ratt.attnum = ru.attnum),
				'{}'
			),
			COALESCE(pg_get_constraintdef(con.oid), ''),
			COALESCE(
				(SELECT am.amname
				 FROM pg_catalog.pg_class ic
				 JOIN pg_catalog.pg_am am ON am.oid = ic.relam
				 WHERE ic.oid = con.conindid),
				''
			) AS amname,
			COALESCE(
				(SELECT pg_get_expr(pi.indpred, pi.indrelid)
				 FROM pg_catalog.pg_index pi
				 WHERE pi.indexrelid = con.conindid),
				''
			) AS indpred,
			COALESCE(con.conindid, 0)
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON con.conrelid = c.oid
		JOIN pg_catalog.pg_namespace ns ON c.relnamespace = ns.oid
		JOIN unnest(con.conkey) WITH ORDINALITY AS u(attnum, ord) ON true
		JOIN pg_catalog.pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = u.attnum
		LEFT JOIN pg_catalog.pg_class refc ON con.confrelid = refc.oid
		LEFT JOIN pg_catalog.pg_namespace refns ON refc.relnamespace = refns.oid
		WHERE ns.nspname = $1 AND c.relname = $2
		  AND con.contype IN ('p', 'f', 'u', 'c')
		GROUP BY con.oid, con.conname, con.contype, con.confrelid, con.confkey,
		         refns.nspname, refc.relname, con.conindid
		ORDER BY con.conname`, table.Schema, table.Name)
	if err != nil {
		return nil, fmt.Errorf("introspect postgres constraints %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	conIndIDs := make(map[int64]struct{})

	for rows.Next() {
		var (
			name       string
			contype    string
			colsStr    string // PostgreSQL array literal
			refSchema  string
			refTable   string
			refColsStr string
			definition string
			amName     string
			where      string
			indID      int64
		)
		if err := rows.Scan(&name, &contype, &colsStr, &refSchema, &refTable, &refColsStr, &definition, &amName, &where, &indID); err != nil {
			return nil, fmt.Errorf("introspect postgres constraints %s.%s: scanning: %w", table.Schema, table.Name, err)
		}

		cols := parsePostgresArray(colsStr)
		refCols := parsePostgresArray(refColsStr)

		constraint := parser.Constraint{
			Name:    name,
			Columns: cols,
		}

		switch contype {
		case "p":
			constraint.Type = parser.PrimaryKey
			// Method / Where intentionally left unset on PRIMARY KEY — the
			// DDL parser path leaves Method empty for inline PRIMARY KEY
			// (no DDL surface to declare a method on a PK), and the manifest
			// builder only consumes Method from indexes[] (PRD §30.7), not
			// from the PK shape. Writing pg_am.amname here would create a
			// path-dependent value (introspect = "btree" vs. DDL = "") with
			// no consumer that cares.
		case "u":
			constraint.Type = parser.Unique
			constraint.Method = amName
			constraint.Where = where
		case "f":
			constraint.Type = parser.ForeignKey
			constraint.ReferenceSchema = refSchema
			constraint.ReferenceTable = refTable
			constraint.ReferenceColumns = refCols
		case "c":
			constraint.Type = parser.Check
			constraint.CheckExpression = extractCheckExpression(definition)
		}

		table.Constraints = append(table.Constraints, constraint)
		if indID != 0 {
			conIndIDs[indID] = struct{}{}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect postgres constraints %s.%s: iterating: %w", table.Schema, table.Name, err)
	}

	return conIndIDs, nil
}

// introspectStandaloneIndexes emits parser.Index (or parser.Unique when partial
// or otherwise not backed by a pg_constraint UNIQUE) entries for every
// pg_index row on the table that isn't already covered by a pg_constraint
// entry. This is the path that surfaces gin/gist/hash indexes (PRD §30.7) and
// partial unique indexes created via CREATE UNIQUE INDEX ... WHERE ...
// (which pg_constraint never represents).
func (pi *PostgresIntrospector) introspectStandaloneIndexes(ctx context.Context, table *parser.Table, conIndIDs map[int64]struct{}) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			pi.indexrelid,
			ic.relname,
			COALESCE(am.amname, ''),
			COALESCE(pg_get_expr(pi.indpred, pi.indrelid), ''),
			pi.indisunique,
			COALESCE(
				(SELECT array_agg(att.attname ORDER BY k.ord)
				 FROM unnest(pi.indkey::smallint[]) WITH ORDINALITY AS k(attnum, ord)
				 JOIN pg_catalog.pg_attribute att
				   ON att.attrelid = pi.indrelid AND att.attnum = k.attnum
				 WHERE k.attnum <> 0),
				'{}'
			)
		FROM pg_catalog.pg_index pi
		JOIN pg_catalog.pg_class ic ON ic.oid = pi.indexrelid
		JOIN pg_catalog.pg_class tc ON tc.oid = pi.indrelid
		JOIN pg_catalog.pg_namespace tns ON tns.oid = tc.relnamespace
		LEFT JOIN pg_catalog.pg_am am ON am.oid = ic.relam
		WHERE tns.nspname = $1 AND tc.relname = $2
		  AND pi.indisvalid AND pi.indisready
		ORDER BY ic.relname`, table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres indexes %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			indexrelid int64
			indexName  string
			amName     string
			where      string
			isUnique   bool
			colsStr    string
		)
		if err := rows.Scan(&indexrelid, &indexName, &amName, &where, &isUnique, &colsStr); err != nil {
			return fmt.Errorf("introspect postgres indexes %s.%s: scanning: %w", table.Schema, table.Name, err)
		}
		if _, covered := conIndIDs[indexrelid]; covered {
			continue
		}
		cols := parsePostgresArray(colsStr)
		// Expression-only indexes leave indkey entries as 0 — they have no
		// projectable columns. Skip them to match the DDL parser's behavior
		// (indexParamColumns skips expression-only entries).
		if len(cols) == 0 {
			continue
		}

		cType := parser.Index
		if isUnique {
			cType = parser.Unique
		}

		constraint := parser.Constraint{
			Name:    indexName,
			Type:    cType,
			Columns: cols,
			Method:  amName,
			Where:   where,
		}
		table.Constraints = append(table.Constraints, constraint)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres indexes %s.%s: iterating: %w", table.Schema, table.Name, err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectTableComment(ctx context.Context, table *parser.Table) error {
	var comment sql.NullString
	err := pi.db.QueryRowContext(ctx, `
		SELECT d.description
		FROM pg_catalog.pg_description d
		JOIN pg_catalog.pg_class c ON d.objoid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		WHERE n.nspname = $1 AND c.relname = $2 AND d.objsubid = 0`,
		table.Schema, table.Name).Scan(&comment)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("introspect postgres table comment %s.%s: %w", table.Schema, table.Name, err)
	}
	if comment.Valid {
		table.Comment = comment.String
	}
	return nil
}

func (pi *PostgresIntrospector) introspectColumnComments(ctx context.Context, table *parser.Table) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT a.attname, d.description
		FROM pg_catalog.pg_description d
		JOIN pg_catalog.pg_class c ON d.objoid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.objsubid
		WHERE n.nspname = $1 AND c.relname = $2 AND d.objsubid > 0`,
		table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres column comments %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var colName, comment string
		if err := rows.Scan(&colName, &comment); err != nil {
			return fmt.Errorf("introspect postgres column comments %s.%s: scanning: %w", table.Schema, table.Name, err)
		}
		if col := parser.ColumnByName(table, colName); col != nil {
			col.Comment = comment
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres column comments %s.%s: iterating: %w", table.Schema, table.Name, err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectEnums(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, t.typname,
		       array_agg(e.enumlabel ORDER BY e.enumsortorder)
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		JOIN pg_catalog.pg_enum e ON t.oid = e.enumtypid
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		GROUP BY n.nspname, t.typname
		ORDER BY n.nspname, t.typname`)
	if err != nil {
		return fmt.Errorf("introspect postgres enums: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			nspname   string
			typname   string
			valuesStr string
		)
		if err := rows.Scan(&nspname, &typname, &valuesStr); err != nil {
			return fmt.Errorf("introspect postgres enums: scanning: %w", err)
		}
		if !schemaAllowed(nspname, opts) {
			continue
		}

		schema.Enums = append(schema.Enums, parser.Enum{
			Name:   typname,
			Schema: nspname,
			Values: parsePostgresArray(valuesStr),
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres enums: iterating: %w", err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectCompositeTypes(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, t.typname, c.oid
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		JOIN pg_catalog.pg_class c ON t.typrelid = c.oid
		WHERE t.typtype = 'c'
		  AND c.relkind = 'c'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY n.nspname, t.typname`)
	if err != nil {
		return fmt.Errorf("introspect postgres composite types: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type ctRef struct {
		schema string
		name   string
		oid    int64
	}
	var refs []ctRef

	for rows.Next() {
		var ref ctRef
		if err := rows.Scan(&ref.schema, &ref.name, &ref.oid); err != nil {
			return fmt.Errorf("introspect postgres composite types: scanning: %w", err)
		}
		if !schemaAllowed(ref.schema, opts) {
			continue
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres composite types: iterating: %w", err)
	}

	for _, ref := range refs {
		ct := parser.CompositeType{
			Name:   ref.name,
			Schema: ref.schema,
		}

		attrRows, err := pi.db.QueryContext(ctx, `
			SELECT a.attname, format_type(a.atttypid, a.atttypmod)
			FROM pg_catalog.pg_attribute a
			WHERE a.attrelid = $1
			  AND a.attnum > 0
			  AND NOT a.attisdropped
			ORDER BY a.attnum`, ref.oid)
		if err != nil {
			return fmt.Errorf("introspect postgres composite type attributes %s.%s: %w", ref.schema, ref.name, err)
		}

		if err := scanCompositeAttributes(attrRows, &ct); err != nil {
			return fmt.Errorf("introspect postgres composite type attributes %s.%s: %w", ref.schema, ref.name, err)
		}

		schema.CompositeTypes = append(schema.CompositeTypes, ct)
	}

	return nil
}

func (pi *PostgresIntrospector) introspectDomainTypes(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, t.typname,
		       format_type(t.typbasetype, t.typtypmod),
		       t.oid
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		WHERE t.typtype = 'd'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY n.nspname, t.typname`)
	if err != nil {
		return fmt.Errorf("introspect postgres domain types: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type dtRef struct {
		schema   string
		name     string
		baseType string
		oid      int64
	}
	var refs []dtRef

	for rows.Next() {
		var ref dtRef
		if err := rows.Scan(&ref.schema, &ref.name, &ref.baseType, &ref.oid); err != nil {
			return fmt.Errorf("introspect postgres domain types: scanning: %w", err)
		}
		if !schemaAllowed(ref.schema, opts) {
			continue
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres domain types: iterating: %w", err)
	}

	for _, ref := range refs {
		dt := parser.DomainType{
			Name:     ref.name,
			Schema:   ref.schema,
			BaseType: normalizePostgresType(ref.baseType),
		}

		conRows, err := pi.db.QueryContext(ctx, `
			SELECT conname, pg_get_constraintdef(oid)
			FROM pg_catalog.pg_constraint
			WHERE contypid = $1`, ref.oid)
		if err != nil {
			return fmt.Errorf("introspect postgres domain constraints %s.%s: %w", ref.schema, ref.name, err)
		}

		if err := scanDomainConstraints(conRows, &dt); err != nil {
			return fmt.Errorf("introspect postgres domain constraints %s.%s: %w", ref.schema, ref.name, err)
		}

		schema.DomainTypes = append(schema.DomainTypes, dt)
	}

	return nil
}

// normalizePostgresType converts format_type() output to the short-form SQL
// type names that the parser produces (e.g., "character varying(255)" → "varchar(255)").
func normalizePostgresType(ft string) string {
	// Handle array suffix.
	suffix := ""
	if base, ok := strings.CutSuffix(ft, "[]"); ok {
		ft = base
		suffix = "[]"
	}

	// Normalize character types.
	if rest, ok := strings.CutPrefix(ft, "character varying"); ok {
		ft = "varchar" + rest
	} else if rest, ok := strings.CutPrefix(ft, "character"); ok {
		ft = "char" + rest
	}

	// Normalize timestamp/time types.
	if strings.Contains(ft, " without time zone") {
		ft = strings.Replace(ft, " without time zone", "", 1)
	} else if strings.Contains(ft, " with time zone") {
		if strings.HasPrefix(ft, "timestamp") {
			ft = strings.Replace(ft, "timestamp", "timestamptz", 1)
			ft = strings.Replace(ft, " with time zone", "", 1)
		} else if strings.HasPrefix(ft, "time") {
			ft = strings.Replace(ft, "time", "timetz", 1)
			ft = strings.Replace(ft, " with time zone", "", 1)
		}
	}

	return ft + suffix
}

// parsePostgresArray parses a PostgreSQL text-format array literal like
// {foo,bar,baz} into a string slice.
func parsePostgresArray(s string) []string {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, ch := range s {
		switch {
		case escaped:
			current.WriteRune(ch)
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			inQuote = !inQuote
		case ch == ',' && !inQuote:
			result = append(result, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// extractCheckExpression extracts the boolean expression from a CHECK
// constraint definition like "CHECK ((age > 0))".
func extractCheckExpression(def string) string {
	def = strings.TrimSpace(def)
	if !strings.HasPrefix(def, "CHECK") {
		return def
	}
	def = strings.TrimPrefix(def, "CHECK")
	def = strings.TrimSpace(def)
	// Remove one layer of outer parentheses.
	if strings.HasPrefix(def, "(") && strings.HasSuffix(def, ")") {
		def = def[1 : len(def)-1]
	}
	return strings.TrimSpace(def)
}

// scanCompositeAttributes reads composite type attributes from rows and
// appends them to ct. It closes the rows when done.
func scanCompositeAttributes(rows *sql.Rows, ct *parser.CompositeType) error {
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var attr parser.Attribute
		var formatType string
		if err := rows.Scan(&attr.Name, &formatType); err != nil {
			return fmt.Errorf("scanning: %w", err)
		}
		attr.Type = normalizePostgresType(formatType)
		ct.Attributes = append(ct.Attributes, attr)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating: %w", err)
	}
	return nil
}

// scanDomainConstraints reads domain constraints from rows and appends them
// to dt. It closes the rows when done.
func scanDomainConstraints(rows *sql.Rows, dt *parser.DomainType) error {
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var conName, definition string
		if err := rows.Scan(&conName, &definition); err != nil {
			return fmt.Errorf("scanning: %w", err)
		}
		dt.Constraints = append(dt.Constraints, parser.Constraint{
			Name:            conName,
			Type:            parser.Check,
			CheckExpression: extractCheckExpression(definition),
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating: %w", err)
	}
	return nil
}
