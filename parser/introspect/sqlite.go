package introspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // SQLite database/sql driver

	"github.com/teandresmith/sqlgen/parser"
)

// SQLiteIntrospector discovers schema elements from a live SQLite database.
type SQLiteIntrospector struct {
	db *sql.DB
}

// NewSQLiteIntrospector creates a new SQLite introspector.
func NewSQLiteIntrospector() *SQLiteIntrospector {
	return &SQLiteIntrospector{}
}

// Name returns "sqlite".
func (si *SQLiteIntrospector) Name() string { return "sqlite" }

// Close releases the database connection.
func (si *SQLiteIntrospector) Close() error {
	if si.db == nil {
		return nil
	}
	if err := si.db.Close(); err != nil {
		return fmt.Errorf("closing sqlite introspector: %w", err)
	}
	return nil
}

// Introspect connects to the SQLite database and populates schema with
// discovered tables, columns, primary keys, and foreign keys.
func (si *SQLiteIntrospector) Introspect(ctx context.Context, connString string, schema *parser.Schema, opts IntrospectionOptions) error {
	db, err := sql.Open("sqlite", connString)
	if err != nil {
		return fmt.Errorf("introspect sqlite: opening connection: %w", err)
	}
	si.db = db

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("introspect sqlite: ping: %w", err)
	}

	if err := si.introspectTables(ctx, schema, opts); err != nil {
		return err
	}
	if err := si.introspectViews(ctx, schema, opts); err != nil {
		return err
	}

	return nil
}

func (si *SQLiteIntrospector) introspectTables(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := si.db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return fmt.Errorf("introspect sqlite tables: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("introspect sqlite tables: scanning: %w", err)
		}
		if !tableAllowed(name, opts) {
			continue
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite tables: iterating: %w", err)
	}

	for _, name := range tableNames {
		table := parser.Table{
			Name: name,
		}

		if err := si.introspectColumns(ctx, &table); err != nil {
			return err
		}
		if err := si.introspectForeignKeys(ctx, &table); err != nil {
			return err
		}
		if err := si.introspectUniqueConstraints(ctx, &table); err != nil {
			return err
		}
		if err := si.introspectNonUniqueIndexes(ctx, &table); err != nil {
			return err
		}

		schema.Tables = append(schema.Tables, table)
	}

	return nil
}

func (si *SQLiteIntrospector) introspectViews(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	rows, err := si.db.QueryContext(ctx, `
		SELECT name, sql
		FROM sqlite_master
		WHERE type = 'view' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return fmt.Errorf("introspect sqlite views: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type viewRef struct {
		name string
		sql  string
	}
	var views []viewRef

	for rows.Next() {
		var ref viewRef
		var viewSQL sql.NullString
		if err := rows.Scan(&ref.name, &viewSQL); err != nil {
			return fmt.Errorf("introspect sqlite views: scanning: %w", err)
		}
		if !tableAllowed(ref.name, opts) {
			continue
		}
		if viewSQL.Valid {
			ref.sql = viewSQL.String
		}
		views = append(views, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite views: iterating: %w", err)
	}

	for _, ref := range views {
		view := parser.View{
			Name: ref.name,
			SQL:  ref.sql,
		}

		if err := si.introspectViewColumns(ctx, &view); err != nil {
			return err
		}

		schema.Views = append(schema.Views, view)
	}

	return nil
}

func (si *SQLiteIntrospector) introspectViewColumns(ctx context.Context, view *parser.View) error {
	rows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA table_info(%q)", view.Name))
	if err != nil {
		return fmt.Errorf("introspect sqlite view columns %s: %w", view.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	for rows.Next() {
		var (
			cid       int
			name      string
			typeName  string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("introspect sqlite view columns %s: scanning: %w", view.Name, err)
		}

		col := parser.Column{
			Name:     name,
			Type:     normalizeSQLiteType(typeName),
			Nullable: notNull == 0,
		}

		view.Columns = append(view.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite view columns %s: iterating: %w", view.Name, err)
	}
	return nil
}

func (si *SQLiteIntrospector) introspectColumns(ctx context.Context, table *parser.Table) error {
	rows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA table_info(%q)", table.Name))
	if err != nil {
		return fmt.Errorf("introspect sqlite columns %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	// Track PK columns for composite PK detection.
	var pkCols []pkCol

	for rows.Next() {
		var (
			cid       int
			name      string
			typeName  string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("introspect sqlite columns %s: scanning: %w", table.Name, err)
		}

		col := parser.Column{
			Name:     name,
			Type:     normalizeSQLiteType(typeName),
			Nullable: notNull == 0,
		}

		if dfltValue.Valid {
			col.Default = dfltValue.String
		}

		if pk > 0 {
			col.PrimaryKey = true
			col.Nullable = false
			pkCols = append(pkCols, pkCol{name: name, order: pk, colType: col.Type})
		}

		table.Columns = append(table.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite columns %s: iterating: %w", table.Name, err)
	}

	// INTEGER PRIMARY KEY is auto-increment (rowid alias) — single PK column only.
	if len(pkCols) == 1 && pkCols[0].colType == "integer" {
		if col := parser.ColumnByName(table, pkCols[0].name); col != nil {
			col.AutoIncrement = true
		}
	}

	// Add composite PK as a table-level constraint if >1 PK column.
	if len(pkCols) > 1 {
		constraint := parser.Constraint{
			Type: parser.PrimaryKey,
		}
		for _, pc := range pkCols {
			constraint.Columns = append(constraint.Columns, pc.name)
		}
		table.Constraints = append(table.Constraints, constraint)
	}

	return nil
}

type pkCol struct {
	name    string
	order   int
	colType string
}

func (si *SQLiteIntrospector) introspectForeignKeys(ctx context.Context, table *parser.Table) error {
	rows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA foreign_key_list(%q)", table.Name))
	if err != nil {
		return fmt.Errorf("introspect sqlite foreign keys %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	// Group by FK id for composite FKs.
	type fkRow struct {
		id       int
		seq      int
		refTable string
		from     string
		to       string
	}
	var fkRows []fkRow

	for rows.Next() {
		var (
			r         fkRow
			onUpdate  string
			onDelete  string
			matchRule string
		)
		if err := rows.Scan(&r.id, &r.seq, &r.refTable, &r.from, &r.to, &onUpdate, &onDelete, &matchRule); err != nil {
			return fmt.Errorf("introspect sqlite foreign keys %s: scanning: %w", table.Name, err)
		}
		fkRows = append(fkRows, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite foreign keys %s: iterating: %w", table.Name, err)
	}

	// Group rows by FK id.
	fks := make(map[int]*parser.Constraint)
	var order []int

	for _, r := range fkRows {
		c, ok := fks[r.id]
		if !ok {
			c = &parser.Constraint{
				Type:           parser.ForeignKey,
				ReferenceTable: r.refTable,
			}
			fks[r.id] = c
			order = append(order, r.id)
		}
		c.Columns = append(c.Columns, r.from)
		c.ReferenceColumns = append(c.ReferenceColumns, r.to)
	}

	for _, id := range order {
		c := fks[id]
		table.Constraints = append(table.Constraints, *c)
		// Apply single-column FK to column.
		if len(c.Columns) == 1 && len(c.ReferenceColumns) == 1 {
			if col := parser.ColumnByName(table, c.Columns[0]); col != nil && col.FKReference == nil {
				col.FKReference = &parser.FKReference{
					Table:  c.ReferenceTable,
					Column: c.ReferenceColumns[0],
				}
			}
		}
	}

	return nil
}

func (si *SQLiteIntrospector) introspectUniqueConstraints(ctx context.Context, table *parser.Table) error {
	idxRows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA index_list(%q)", table.Name))
	if err != nil {
		return fmt.Errorf("introspect sqlite indexes %s: %w", table.Name, err)
	}
	defer idxRows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type idxRef struct {
		name    string
		unique  bool
		partial bool
	}
	var indexes []idxRef

	for idxRows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return fmt.Errorf("introspect sqlite indexes %s: scanning: %w", table.Name, err)
		}
		if unique == 1 {
			indexes = append(indexes, idxRef{name: name, unique: true, partial: partial == 1})
		}
	}
	if err := idxRows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite indexes %s: iterating: %w", table.Name, err)
	}

	for _, idx := range indexes {
		cols, err := si.introspectIndexColumns(ctx, idx.name)
		if err != nil {
			return err
		}
		var where string
		if idx.partial {
			w, err := si.indexPartialWhere(ctx, idx.name)
			if err != nil {
				return err
			}
			where = w
		}
		constraint := parser.Constraint{
			Name:    idx.name,
			Type:    parser.Unique,
			Columns: cols,
			Method:  "btree", // SQLite indexes are always btree
			Where:   where,
		}
		table.Constraints = append(table.Constraints, constraint)

		// Route through ApplyConstraintToColumns so the partial-UNIQUE gate
		// keeps col.Unique = false for partial unique indexes.
		parser.ApplyConstraintToColumns(table, constraint)
	}

	return nil
}

// introspectNonUniqueIndexes emits parser.Index constraints for every
// non-unique index on the table. The unique branch lives in
// introspectUniqueConstraints; PRAGMA index_list reports non-unique indexes
// with unique == 0 and origin == "c" (created by an explicit CREATE INDEX) —
// the only way a non-unique index enters SQLite. PK / UNIQUE auto-indexes
// (origin "pk" / "u") are always unique and handled elsewhere, so they never
// reach this path. Partial non-unique indexes are legal in SQLite, so the WHERE
// predicate round-trips via indexPartialWhere (PRD §30.7).
func (si *SQLiteIntrospector) introspectNonUniqueIndexes(ctx context.Context, table *parser.Table) error {
	idxRows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA index_list(%q)", table.Name))
	if err != nil {
		return fmt.Errorf("introspect sqlite non-unique indexes %s: %w", table.Name, err)
	}
	defer idxRows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	type idxRef struct {
		name    string
		partial bool
	}
	var indexes []idxRef

	for idxRows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return fmt.Errorf("introspect sqlite non-unique indexes %s: scanning: %w", table.Name, err)
		}
		if unique == 0 && origin == "c" {
			indexes = append(indexes, idxRef{name: name, partial: partial == 1})
		}
	}
	if err := idxRows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite non-unique indexes %s: iterating: %w", table.Name, err)
	}

	for _, idx := range indexes {
		cols, err := si.introspectIndexColumns(ctx, idx.name)
		if err != nil {
			return err
		}
		// Expression-only indexes project no columns — skip them to match the
		// PostgreSQL standalone-index sweep.
		if len(cols) == 0 {
			continue
		}
		var where string
		if idx.partial {
			w, err := si.indexPartialWhere(ctx, idx.name)
			if err != nil {
				return err
			}
			where = w
		}
		table.Constraints = append(table.Constraints, parser.Constraint{
			Name:    idx.name,
			Type:    parser.Index,
			Columns: cols,
			Method:  "btree", // SQLite indexes are always btree
			Where:   where,
		})
	}

	return nil
}

// indexPartialWhere reads the canonical CREATE INDEX statement for a partial
// unique index out of sqlite_master and extracts the predicate text that
// follows the WHERE keyword. SQLite preserves the original DDL verbatim, so
// this round-trips the same text the DDL parser would have captured.
func (si *SQLiteIntrospector) indexPartialWhere(ctx context.Context, indexName string) (string, error) {
	var ddl sql.NullString
	err := si.db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		indexName).Scan(&ddl)
	if err == sql.ErrNoRows || !ddl.Valid {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("introspect sqlite index sql %s: %w", indexName, err)
	}
	return extractSQLiteIndexWhere(ddl.String), nil
}

// extractSQLiteIndexWhere returns the text following the (case-insensitive)
// WHERE keyword in a CREATE INDEX statement. Returns "" when no WHERE clause
// is present. It walks the DDL once with a small state machine that skips SQL
// comments (`--` line, `/* */` block), string literals (`'…'`), and quoted
// identifiers (`"…"`, “ `…` “, `[…]`) so a WHERE keyword appearing inside any
// of those regions — or an identifier merely containing "where" — is not
// mistaken for the real partial-index predicate.
func extractSQLiteIndexWhere(ddl string) string {
	for i := 0; i < len(ddl); i++ {
		if next, ok := skipSQLiteInert(ddl, i); ok {
			i = next
			continue
		}
		if matchesSQLiteWhere(ddl, i) {
			return strings.TrimSpace(ddl[i+len("WHERE"):])
		}
	}
	return ""
}

// skipSQLiteInert reports whether a comment or delimited region begins at pos
// and, if so, returns the index of its final byte so the caller's loop resumes
// just past it. It covers `--` line comments, `/* */` block comments, `'…'`
// string literals, and `"…"` / “ `…` “ / `[…]` quoted identifiers — the
// regions where a WHERE keyword must not be treated as the partial-index
// predicate.
func skipSQLiteInert(ddl string, pos int) (int, bool) {
	c := ddl[pos]
	switch {
	case c == '-' && pos+1 < len(ddl) && ddl[pos+1] == '-':
		return sqliteScanPast(ddl, pos+2, "\n"), true
	case c == '/' && pos+1 < len(ddl) && ddl[pos+1] == '*':
		return sqliteScanPast(ddl, pos+2, "*/"), true
	case c == '\'' || c == '"' || c == '`':
		return sqliteScanPast(ddl, pos+1, string(c)), true
	case c == '[':
		return sqliteScanPast(ddl, pos+1, "]"), true
	}
	return pos, false
}

// sqliteScanPast returns the index of the last byte of the first occurrence of
// term at or after start, or the final index of ddl when term is absent (an
// unterminated region consumes the rest of the string).
func sqliteScanPast(ddl string, start int, term string) int {
	if off := strings.Index(ddl[start:], term); off >= 0 {
		return start + off + len(term) - 1
	}
	return len(ddl) - 1
}

// matchesSQLiteWhere reports whether the WHERE keyword begins at pos as a
// standalone token (bounded by separators or the string ends).
func matchesSQLiteWhere(ddl string, pos int) bool {
	end := pos + len("WHERE")
	if end > len(ddl) || !strings.EqualFold(ddl[pos:end], "WHERE") {
		return false
	}
	prevOK := pos == 0 || isSQLiteSeparator(ddl[pos-1])
	nextOK := end >= len(ddl) || isSQLiteSeparator(ddl[end])
	return prevOK && nextOK
}

func isSQLiteSeparator(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '(', ')':
		return true
	}
	return false
}

func (si *SQLiteIntrospector) introspectIndexColumns(ctx context.Context, indexName string) ([]string, error) {
	rows, err := si.db.QueryContext(ctx,
		fmt.Sprintf("PRAGMA index_info(%q)", indexName))
	if err != nil {
		return nil, fmt.Errorf("introspect sqlite index info %s: %w", indexName, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name sql.NullString
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, fmt.Errorf("introspect sqlite index info %s: scanning: %w", indexName, err)
		}
		// PRAGMA index_info reports a NULL name for expression index parts
		// (e.g. CREATE INDEX ... ON t(lower(a))). Skip them — they have no
		// projectable column — to match the MySQL + PostgreSQL sweeps and to
		// avoid a "converting NULL to string" scan error.
		if name.Valid {
			cols = append(cols, name.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect sqlite index info %s: iterating: %w", indexName, err)
	}
	return cols, nil
}

// normalizeSQLiteType normalizes a SQLite type name to match the parser format.
// The parser lowercases all types and maps unrecognized types to "blob".
func normalizeSQLiteType(typeName string) string {
	if typeName == "" {
		return "blob"
	}
	return strings.ToLower(typeName)
}
