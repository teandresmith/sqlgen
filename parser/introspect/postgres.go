package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL database/sql driver

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
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

	comments, err := pi.typeComments(ctx)
	if err != nil {
		return err
	}
	if err := pi.introspectEnums(ctx, schema, opts, comments); err != nil {
		return err
	}
	if err := pi.introspectCompositeTypes(ctx, schema, opts, comments); err != nil {
		return err
	}
	if err := pi.introspectDomainTypes(ctx, schema, opts, comments); err != nil {
		return err
	}
	if err := pi.introspectTables(ctx, schema, opts); err != nil {
		return err
	}
	if err := pi.introspectPartitions(ctx, schema, opts); err != nil {
		return err
	}
	definitions, err := pi.viewDefinitions(ctx)
	if err != nil {
		return err
	}
	if err := pi.introspectViews(ctx, schema, opts, definitions); err != nil {
		return err
	}
	if err := pi.introspectMaterializedViews(ctx, schema, opts, definitions); err != nil {
		return err
	}

	return nil
}

// introspectPartitions lists the partitions introspectTables leaves out, each
// with the table it is a partition of, as the file parser lists them.
func (pi *PostgresIntrospector) introspectPartitions(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, c.relname, pn.nspname, pc.relname
		FROM pg_catalog.pg_inherits i
		JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_class pc ON pc.oid = i.inhparent
		JOIN pg_catalog.pg_namespace pn ON pn.oid = pc.relnamespace
		WHERE c.relispartition AND c.relkind IN ('r', 'p')
		ORDER BY n.nspname, c.relname`)
	if err != nil {
		return fmt.Errorf("introspect postgres partitions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var part parser.Partition
		if err := rows.Scan(&part.Schema, &part.Name, &part.ParentSchema, &part.ParentName); err != nil {
			return fmt.Errorf("introspect postgres partitions: scanning: %w", err)
		}
		if schemaAllowed(part.Schema, opts) {
			schema.Partitions = append(schema.Partitions, part)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres partitions: iterating: %w", err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectTables(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	// A partition is storage for its partitioned table, which is the entity,
	// so partitions are left out (PRD §6.3).
	rows, err := pi.db.QueryContext(ctx, `
		SELECT t.table_schema, t.table_name
		FROM information_schema.tables t
		JOIN pg_catalog.pg_namespace n ON n.nspname = t.table_schema
		JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = t.table_name
		WHERE t.table_type = 'BASE TABLE'
		  AND t.table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND NOT c.relispartition
		ORDER BY t.table_schema, t.table_name`)
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

		if err := pi.introspectColumns(ctx, &table, schema.DomainTypes); err != nil {
			return err
		}
		if err := pi.introspectConstraints(ctx, &table, postgres.TableImplicitCasts(&table, schema.DomainTypes)); err != nil {
			return err
		}
		if err := pi.introspectTableComment(ctx, &table); err != nil {
			return err
		}
		if err := pi.introspectColumnComments(ctx, &table); err != nil {
			return err
		}
		parser.CanonicalizeConstraints(&table, "postgres")

		schema.Tables = append(schema.Tables, table)
	}

	return nil
}

func (pi *PostgresIntrospector) introspectViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions, definitions map[string]string) error {
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
		pi.columnsFromDefinition(&view, definitions, schema)

		schema.Views = append(schema.Views, view)
	}

	return nil
}

// viewDefinitions returns the definition of every view and materialized view,
// keyed by schema.name. pg_get_viewdef qualifies a table whose schema is not
// on the search_path, so with an empty one every table is qualified and the
// view parser finds it whatever the connection's search_path is.
func (pi *PostgresIntrospector) viewDefinitions(ctx context.Context) (map[string]string, error) {
	tx, err := pi.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("introspect postgres view definitions: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only transaction; nothing to undo

	if _, err := tx.ExecContext(ctx, "SELECT set_config('search_path', '', true)"); err != nil {
		return nil, fmt.Errorf("introspect postgres view definitions: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT n.nspname, c.relname, pg_catalog.pg_get_viewdef(c.oid)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('v', 'm')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	if err != nil {
		return nil, fmt.Errorf("introspect postgres view definitions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	definitions := make(map[string]string)
	for rows.Next() {
		var schemaName, name string
		var definition sql.NullString
		if err := rows.Scan(&schemaName, &name, &definition); err != nil {
			return nil, fmt.Errorf("introspect postgres view definitions: scanning: %w", err)
		}
		definitions[schemaName+"."+name] = definition.String
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect postgres view definitions: iterating: %w", err)
	}
	return definitions, nil
}

// columnsFromDefinition reads view's columns from its definition (PRD §16.2).
func (pi *PostgresIntrospector) columnsFromDefinition(view *parser.View, definitions map[string]string, schema *parser.Schema) {
	ddl := "CREATE VIEW " + quotePostgresIdent(view.Schema) + "." + quotePostgresIdent(view.Name) + " AS " + definitions[view.Schema+"."+view.Name]
	parse := func(sql []byte) (parser.ViewSQL, error) { return postgres.ParseViewSQL(sql, view.Schema) }
	catalog := view.Columns
	columnsFromDefinition(view, ddl, parse, schema, func(i int) (parser.Column, bool) {
		return catalog[i], true
	})
}

func quotePostgresIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// introspectMaterializedViews discovers PostgreSQL materialized views. They
// are invisible to information_schema.views (the introspectViews pass) and
// live in pg_matviews instead. Column metadata is read with the shared
// introspectViewColumns query, which already matches relkind = 'm'. The
// pg_matviews.definition populates View.SQL for documentation purposes only —
// it is never re-injected at query time (same contract as regular views).
func (pi *PostgresIntrospector) introspectMaterializedViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions, definitions map[string]string) error {
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
		pi.columnsFromDefinition(&view, definitions, schema)
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

// userTypeJoin joins, for the type whose OID the query exposes as
// tt.type_oid, its element type when it is an array (et) and that type's
// schema (tn). Both are NULL unless the type is an enum, a domain or a
// standalone composite type, the user types columnType writes relative to the
// column's schema.
const userTypeJoin = `
		LEFT JOIN pg_catalog.pg_type ty ON ty.oid = tt.type_oid
		LEFT JOIN pg_catalog.pg_type et
		  ON et.oid = CASE WHEN ty.typelem <> 0 AND ty.typlen = -1 THEN ty.typelem ELSE ty.oid END
		 AND (et.typtype IN ('e', 'd')
		      OR (et.typtype = 'c' AND EXISTS (
		          SELECT 1 FROM pg_catalog.pg_class tc WHERE tc.oid = et.typrelid AND tc.relkind = 'c')))
		LEFT JOIN pg_catalog.pg_namespace tn ON tn.oid = et.typnamespace`

// columnType writes the type of a column, attribute or domain base type that
// belongs to schema own, as the parser writes it. format_type qualifies a type
// whose schema is not on the connection's search_path; an enum, domain or
// composite type (typSchema set) is instead written bare when it lives in own
// and qualified when it lives in another schema, whatever the search_path.
func columnType(formatType, typSchema, typName, own string) string {
	if typSchema == "" {
		return normalizePostgresType(formatType)
	}
	name := typName
	if typSchema != own {
		name = typSchema + "." + typName
	}
	if strings.HasSuffix(formatType, "[]") {
		name += "[]"
	}
	return name
}

func (pi *PostgresIntrospector) introspectViewColumns(ctx context.Context, view *parser.View) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			COALESCE(tn.nspname, ''),
			COALESCE(et.typname, ''),
			NOT a.attnotnull
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		CROSS JOIN LATERAL (SELECT a.atttypid AS type_oid) tt`+userTypeJoin+`
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
			col                            parser.Column
			formatType, typSchema, typName string
			nullable                       bool
		)
		if err := rows.Scan(&col.Name, &formatType, &typSchema, &typName, &nullable); err != nil {
			return fmt.Errorf("introspect postgres view columns %s.%s: scanning: %w", view.Schema, view.Name, err)
		}

		col.Type = columnType(formatType, typSchema, typName, view.Schema)
		col.Nullable = nullable

		view.Columns = append(view.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres view columns %s.%s: iterating: %w", view.Schema, view.Name, err)
	}
	return nil
}

// columnExpr is the default or generation expression of one column, as the
// catalog prints it.
type columnExpr struct {
	text      string
	generated string // pg_attribute.attgenerated
	serial    bool   // the default draws from a sequence the column owns
}

func (pi *PostgresIntrospector) introspectColumns(ctx context.Context, table *parser.Table, domains []parser.DomainType) error {
	// A serial column owns its sequence (an 'a' dependency); an identity
	// column's sequence is an 'i' dependency and it has no pg_attrdef row.
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			COALESCE(tn.nspname, ''),
			COALESCE(et.typname, ''),
			NOT a.attnotnull,
			pg_get_expr(ad.adbin, ad.adrelid),
			a.attidentity,
			-- attgenerated arrived in PostgreSQL 12; read through to_jsonb so an
			-- older server yields NULL instead of failing on the column name.
			COALESCE(to_jsonb(a) ->> 'attgenerated', ''),
			EXISTS (
				SELECT 1
				FROM pg_catalog.pg_depend dep
				JOIN pg_catalog.pg_class seq ON seq.oid = dep.objid AND seq.relkind = 'S'
				WHERE dep.classid = 'pg_catalog.pg_class'::regclass
				  AND dep.refobjid = a.attrelid AND dep.refobjsubid = a.attnum
				  AND dep.deptype = 'a')
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
		JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		LEFT JOIN pg_catalog.pg_attrdef ad ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
		CROSS JOIN LATERAL (SELECT a.atttypid AS type_oid) tt`+userTypeJoin+`
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum`, table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres columns %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var exprs []columnExpr
	for rows.Next() {
		var (
			col                            parser.Column
			formatType, typSchema, typName string
			nullable                       bool
			dflt                           sql.NullString
			identity                       string
			expr                           columnExpr
		)
		if err := rows.Scan(&col.Name, &formatType, &typSchema, &typName, &nullable, &dflt, &identity, &expr.generated, &expr.serial); err != nil {
			return fmt.Errorf("introspect postgres columns %s.%s: scanning: %w", table.Schema, table.Name, err)
		}

		col.Type = columnType(formatType, typSchema, typName, table.Schema)
		col.Nullable = nullable
		col.AutoIncrement = identity != ""
		expr.text = dflt.String

		table.Columns = append(table.Columns, col)
		exprs = append(exprs, expr)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres columns %s.%s: iterating: %w", table.Schema, table.Name, err)
	}

	// A generation expression may name any column, so every column's type
	// must be known before the expressions are normalized.
	casts := postgres.TableImplicitCasts(table, domains)
	for i, expr := range exprs {
		if err := applyColumnExpr(&table.Columns[i], expr, casts); err != nil {
			return fmt.Errorf("introspect postgres columns %s.%s: column %s: %w", table.Schema, table.Name, table.Columns[i].Name, err)
		}
	}
	return nil
}

// applyColumnExpr sets col's default or generation expression in the
// parser's form. A serial column carries no default, only AutoIncrement, as
// when parsed.
func applyColumnExpr(col *parser.Column, expr columnExpr, casts map[string][]string) error {
	if expr.text == "" {
		return nil
	}
	isSequence := strings.Contains(strings.ToLower(expr.text), "nextval(")
	if expr.generated == "" && expr.serial && isSequence {
		col.AutoIncrement = true
		return nil
	}
	text, err := catalogExpr(expr.text, casts)
	if err != nil {
		return err
	}
	if expr.generated != "" {
		// pg_attrdef holds a generated column's expression, not a default.
		col.GeneratedExpr = text
		col.GeneratedStorage = postgresGeneratedStorage(expr.generated)
		return nil
	}
	col.Default = text
	if isSequence {
		col.AutoIncrement = true
	}
	return nil
}

// catalogExpr is postgres.CatalogExpr for text that may be empty.
func catalogExpr(text string, casts map[string][]string) (string, error) {
	if text == "" {
		return "", nil
	}
	out, err := postgres.CatalogExpr(text, casts)
	if err != nil {
		return "", &SchemaError{Err: fmt.Errorf("normalizing catalog text: %w", err)}
	}
	return out, nil
}

// postgresGeneratedStorage maps pg_attribute.attgenerated to GeneratedStorage:
// 's' is STORED (PostgreSQL 12+) and 'v' is VIRTUAL (PostgreSQL 18+).
func postgresGeneratedStorage(attgenerated string) string {
	if attgenerated == "v" {
		return "VIRTUAL"
	}
	return "STORED"
}

// introspectConstraints reads the table's constraints and indexes. casts
// holds the implicit casts of the table's columns, for normalizing CHECK
// expressions and partial-index predicates (see postgres.CatalogExpr).
func (pi *PostgresIntrospector) introspectConstraints(ctx context.Context, table *parser.Table, casts map[string][]string) error {
	// Track the OIDs of indexes already covered by a pg_constraint so the
	// non-constraint pg_index sweep below can skip them.
	conIndIDs, err := pi.introspectConstraintsViaPgConstraint(ctx, table, casts)
	if err != nil {
		return err
	}
	if err := pi.introspectStandaloneIndexes(ctx, table, conIndIDs, casts); err != nil {
		return err
	}
	return pi.introspectExpressionIndexes(ctx, table)
}

// introspectExpressionIndexes reads the indexes with an expression part, which
// the constraints leave out, with the columns each reads: pg_depend records
// one entry per column referenced anywhere in the index, and dropping any of
// them drops the index.
func (pi *PostgresIntrospector) introspectExpressionIndexes(ctx context.Context, table *parser.Table) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			ic.relname,
			COALESCE(
				(SELECT array_agg(att.attname ORDER BY att.attnum)
				 FROM pg_catalog.pg_attribute att
				 WHERE att.attrelid = pi.indrelid
				   AND att.attnum IN (
					SELECT dep.refobjsubid
					FROM pg_catalog.pg_depend dep
					WHERE dep.classid = 'pg_catalog.pg_class'::regclass
					  AND dep.objid = pi.indexrelid
					  AND dep.refclassid = 'pg_catalog.pg_class'::regclass
					  AND dep.refobjid = pi.indrelid
					  AND dep.refobjsubid > 0)),
				'{}'
			)
		FROM pg_catalog.pg_index pi
		JOIN pg_catalog.pg_class ic ON ic.oid = pi.indexrelid
		JOIN pg_catalog.pg_class tc ON tc.oid = pi.indrelid
		JOIN pg_catalog.pg_namespace tns ON tns.oid = tc.relnamespace
		WHERE tns.nspname = $1 AND tc.relname = $2
		  AND pi.indexprs IS NOT NULL
		ORDER BY ic.relname`, table.Schema, table.Name)
	if err != nil {
		return fmt.Errorf("introspect postgres expression indexes %s.%s: %w", table.Schema, table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var name, cols string
		if err := rows.Scan(&name, &cols); err != nil {
			return fmt.Errorf("introspect postgres expression indexes %s.%s: scanning: %w", table.Schema, table.Name, err)
		}
		table.ExpressionIndexes = append(table.ExpressionIndexes, parser.ExpressionIndex{Name: name, Columns: parsePostgresArray(cols)})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres expression indexes %s.%s: iterating: %w", table.Schema, table.Name, err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectConstraintsViaPgConstraint(ctx context.Context, table *parser.Table, casts map[string][]string) (map[int64]struct{}, error) {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT
			con.conname,
			con.contype,
			COALESCE(array_agg(att.attname ORDER BY u.ord) FILTER (WHERE att.attname IS NOT NULL), '{}') AS columns,
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
		LEFT JOIN unnest(con.conkey) WITH ORDINALITY AS u(attnum, ord) ON true
		LEFT JOIN pg_catalog.pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = u.attnum
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
		var row pgConstraintRow
		if err := rows.Scan(&row.name, &row.contype, &row.cols, &row.refSchema, &row.refTable, &row.refCols, &row.definition, &row.amName, &row.where, &row.indID); err != nil {
			return nil, fmt.Errorf("introspect postgres constraints %s.%s: scanning: %w", table.Schema, table.Name, err)
		}
		constraint, err := row.constraint(casts)
		if err != nil {
			return nil, fmt.Errorf("introspect postgres constraints %s.%s: constraint %s: %w", table.Schema, table.Name, row.name, err)
		}

		table.Constraints = append(table.Constraints, constraint)
		if row.indID != 0 {
			conIndIDs[row.indID] = struct{}{}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect postgres constraints %s.%s: iterating: %w", table.Schema, table.Name, err)
	}

	return conIndIDs, nil
}

// pgConstraintRow is one row of the pg_constraint query. cols and refCols are
// PostgreSQL array literals.
type pgConstraintRow struct {
	name, contype, cols          string
	refSchema, refTable, refCols string
	definition, amName, where    string
	indID                        int64
}

// constraint builds the row's constraint, with CHECK and predicate text in
// the parser's form; casts holds the implicit casts of the table's columns.
func (r pgConstraintRow) constraint(casts map[string][]string) (parser.Constraint, error) {
	c := parser.Constraint{Name: r.name, Columns: parsePostgresArray(r.cols)}
	var err error
	switch r.contype {
	case "p":
		c.Type = parser.PrimaryKey
		// Method / Where intentionally left unset on PRIMARY KEY — the
		// DDL parser path leaves Method empty for inline PRIMARY KEY
		// (no DDL surface to declare a method on a PK), and the manifest
		// builder only consumes Method from indexes[] (PRD §30.7), not
		// from the PK shape. Writing pg_am.amname here would create a
		// path-dependent value (introspect = "btree" vs. DDL = "") with
		// no consumer that cares.
	case "u":
		c.Type = parser.Unique
		c.Method = r.amName
		c.Where, err = catalogExpr(r.where, casts)
	case "f":
		c.Type = parser.ForeignKey
		c.ReferenceSchema = r.refSchema
		c.ReferenceTable = r.refTable
		c.ReferenceColumns = parsePostgresArray(r.refCols)
	case "c":
		c.Type = parser.Check
		c.CheckExpression, err = catalogExpr(extractCheckExpression(r.definition), casts)
	}
	return c, err
}

// introspectStandaloneIndexes emits parser.Index (or parser.Unique when partial
// or otherwise not backed by a pg_constraint UNIQUE) entries for every
// pg_index row on the table that isn't already covered by a pg_constraint
// entry. This is the path that surfaces gin/gist/hash indexes (PRD §30.7) and
// partial unique indexes created via CREATE UNIQUE INDEX ... WHERE ...
// (which pg_constraint never represents). indexprs IS NULL skips an index with
// an expression part, as the DDL parser does: it is not an index over columns.
// k.ord <= indnkeyatts drops INCLUDE payload columns.
func (pi *PostgresIntrospector) introspectStandaloneIndexes(ctx context.Context, table *parser.Table, conIndIDs map[int64]struct{}, casts map[string][]string) error {
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
				 WHERE k.ord <= pi.indnkeyatts),
				'{}'
			)
		FROM pg_catalog.pg_index pi
		JOIN pg_catalog.pg_class ic ON ic.oid = pi.indexrelid
		JOIN pg_catalog.pg_class tc ON tc.oid = pi.indrelid
		JOIN pg_catalog.pg_namespace tns ON tns.oid = tc.relnamespace
		LEFT JOIN pg_catalog.pg_am am ON am.oid = ic.relam
		WHERE tns.nspname = $1 AND tc.relname = $2
		  AND pi.indisvalid AND pi.indisready
		  AND pi.indexprs IS NULL
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

		cType := parser.Index
		if isUnique {
			cType = parser.Unique
		}
		where, err := catalogExpr(where, casts)
		if err != nil {
			return fmt.Errorf("introspect postgres indexes %s.%s: index %s: %w", table.Schema, table.Name, indexName, err)
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

type qualifiedType struct {
	schema string
	name   string
}

// typeComments returns the COMMENT ON TYPE or COMMENT ON DOMAIN text of every
// user type. A domain's comment is stored on its pg_type row too.
func (pi *PostgresIntrospector) typeComments(ctx context.Context) (map[qualifiedType]string, error) {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, t.typname, d.description
		FROM pg_catalog.pg_description d
		JOIN pg_catalog.pg_type t ON d.objoid = t.oid
		JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		WHERE d.classoid = 'pg_catalog.pg_type'::regclass
		  AND d.objsubid = 0
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	if err != nil {
		return nil, fmt.Errorf("introspect postgres type comments: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	comments := make(map[qualifiedType]string)
	for rows.Next() {
		var key qualifiedType
		var comment string
		if err := rows.Scan(&key.schema, &key.name, &comment); err != nil {
			return nil, fmt.Errorf("introspect postgres type comments: scanning: %w", err)
		}
		comments[key] = comment
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect postgres type comments: iterating: %w", err)
	}
	return comments, nil
}

func (pi *PostgresIntrospector) introspectEnums(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions, comments map[qualifiedType]string) error {
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
			Name:    typname,
			Schema:  nspname,
			Values:  parsePostgresArray(valuesStr),
			Comment: comments[qualifiedType{nspname, typname}],
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres enums: iterating: %w", err)
	}
	return nil
}

func (pi *PostgresIntrospector) introspectCompositeTypes(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions, comments map[qualifiedType]string) error {
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
			Name:    ref.name,
			Schema:  ref.schema,
			Comment: comments[qualifiedType{ref.schema, ref.name}],
		}

		attrRows, err := pi.db.QueryContext(ctx, `
			SELECT a.attname, format_type(a.atttypid, a.atttypmod), COALESCE(tn.nspname, ''), COALESCE(et.typname, '')
			FROM pg_catalog.pg_attribute a
			CROSS JOIN LATERAL (SELECT a.atttypid AS type_oid) tt`+userTypeJoin+`
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

func (pi *PostgresIntrospector) introspectDomainTypes(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions, comments map[qualifiedType]string) error {
	rows, err := pi.db.QueryContext(ctx, `
		SELECT n.nspname, t.typname,
		       format_type(t.typbasetype, t.typtypmod),
		       COALESCE(tn.nspname, ''), COALESCE(et.typname, ''),
		       t.oid
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		CROSS JOIN LATERAL (SELECT t.typbasetype AS type_oid) tt`+userTypeJoin+`
		WHERE t.typtype = 'd'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY n.nspname, t.typname`)
	if err != nil {
		return fmt.Errorf("introspect postgres domain types: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var (
		domains []parser.DomainType
		oids    []int64
	)
	for rows.Next() {
		var (
			dt                             parser.DomainType
			formatType, typSchema, typName string
			oid                            int64
		)
		if err := rows.Scan(&dt.Schema, &dt.Name, &formatType, &typSchema, &typName, &oid); err != nil {
			return fmt.Errorf("introspect postgres domain types: scanning: %w", err)
		}
		if !schemaAllowed(dt.Schema, opts) {
			continue
		}
		dt.BaseType = columnType(formatType, typSchema, typName, dt.Schema)
		dt.Comment = comments[qualifiedType{dt.Schema, dt.Name}]
		domains = append(domains, dt)
		oids = append(oids, oid)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect postgres domain types: iterating: %w", err)
	}

	// Every base type is read first: a constraint's implicit casts follow a
	// domain over another domain to its base. Only CHECKs are domain
	// constraints in the model; PostgreSQL 18 also lists a domain's NOT NULL
	// here (contype 'n'), and 16 keeps it on the type.
	for i := range domains {
		dt := &domains[i]
		conRows, err := pi.db.QueryContext(ctx, `
			SELECT conname, pg_get_constraintdef(oid)
			FROM pg_catalog.pg_constraint
			WHERE contypid = $1 AND contype = 'c'`, oids[i])
		if err != nil {
			return fmt.Errorf("introspect postgres domain constraints %s.%s: %w", dt.Schema, dt.Name, err)
		}

		if err := scanDomainConstraints(conRows, dt, postgres.DomainImplicitCasts(dt, domains)); err != nil {
			return fmt.Errorf("introspect postgres domain constraints %s.%s: %w", dt.Schema, dt.Name, err)
		}
	}

	schema.DomainTypes = append(schema.DomainTypes, domains...)
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

	// Normalize character and bit-string types.
	if rest, ok := strings.CutPrefix(ft, "character varying"); ok {
		ft = "varchar" + rest
	} else if rest, ok := strings.CutPrefix(ft, "character"); ok {
		ft = "char" + rest
	} else if rest, ok := strings.CutPrefix(ft, "bit varying"); ok {
		ft = "varbit" + rest
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
	// A non-empty body always holds one more element than it has separators,
	// even when that element is an empty quoted string.
	result = append(result, current.String())

	return result
}

// extractCheckExpression extracts the boolean expression from a
// pg_get_constraintdef CHECK definition like "CHECK ((age > 0))". The
// expression is the text inside the parentheses that follow CHECK; a trailing
// NOT VALID or NO INHERIT is not part of it.
func extractCheckExpression(def string) string {
	def = strings.TrimSpace(def)
	rest, ok := strings.CutPrefix(def, "CHECK")
	if !ok {
		return def
	}
	rest = strings.TrimSpace(rest)
	if end := closingParen(rest); end > 0 {
		return strings.TrimSpace(rest[1:end])
	}
	return rest
}

// closingParen returns the index of the parenthesis that closes the one at
// s[0], skipping string literals and quoted identifiers, or -1 when s does not
// start with a balanced group. pg_get_constraintdef doubles a quote inside
// either kind, which toggles the quoted state twice and leaves it unchanged.
func closingParen(s string) int {
	if !strings.HasPrefix(s, "(") {
		return -1
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// scanCompositeAttributes reads composite type attributes from rows and
// appends them to ct. It closes the rows when done.
func scanCompositeAttributes(rows *sql.Rows, ct *parser.CompositeType) error {
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			attr                           parser.Attribute
			formatType, typSchema, typName string
		)
		if err := rows.Scan(&attr.Name, &formatType, &typSchema, &typName); err != nil {
			return fmt.Errorf("scanning: %w", err)
		}
		attr.Type = columnType(formatType, typSchema, typName, ct.Schema)
		ct.Attributes = append(ct.Attributes, attr)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating: %w", err)
	}
	return nil
}

// scanDomainConstraints reads domain CHECK constraints from rows and appends
// them to dt, with their expressions in the parser's form. casts holds VALUE's
// implicit casts. It closes the rows when done.
func scanDomainConstraints(rows *sql.Rows, dt *parser.DomainType, casts map[string][]string) error {
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var conName, definition string
		if err := rows.Scan(&conName, &definition); err != nil {
			return fmt.Errorf("scanning: %w", err)
		}
		expr, err := catalogExpr(extractCheckExpression(definition), casts)
		if err != nil {
			return fmt.Errorf("constraint %s: %w", conName, err)
		}
		dt.Constraints = append(dt.Constraints, parser.Constraint{
			Name:            conName,
			Type:            parser.Check,
			CheckExpression: expr,
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating: %w", err)
	}
	return nil
}
