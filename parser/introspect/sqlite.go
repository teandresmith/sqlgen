package introspect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	_ "modernc.org/sqlite" // SQLite database/sql driver

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// SQLiteIntrospector discovers schema elements from a live SQLite database.
//
// The PRAGMAs give the catalog facts: columns, types, nullability, defaults,
// primary-key membership and order, foreign keys, indexes and table flags.
// SQLite keeps no constraint names, CHECK expressions or generated-column
// expressions apart from each table's stored CREATE TABLE, so those come
// from reading that text with the file parser (PRD §6.3). A table the file
// parser cannot read is an error, as the same DDL is under source: files.
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

// sqliteTableRef is one row of PRAGMA table_list, with the table's stored
// CREATE TABLE.
type sqliteTableRef struct {
	name         string
	virtual      bool
	withoutRowID bool
	strict       bool
	ddl          string
}

func (si *SQLiteIntrospector) introspectTables(ctx context.Context, schema *parser.Schema, opts IntrospectionOptions) error {
	// A virtual table's shadow tables (type 'shadow') are its storage, not
	// part of the schema.
	rows, err := si.db.QueryContext(ctx, `
		SELECT tl.name, tl.type = 'virtual', tl.wr, tl.strict, COALESCE(m.sql, '')
		FROM pragma_table_list AS tl
		LEFT JOIN sqlite_master AS m ON m.type = 'table' AND m.name = tl.name
		WHERE tl.schema = 'main' AND tl.type IN ('table', 'virtual') AND tl.name NOT LIKE 'sqlite_%'
		ORDER BY tl.name`)
	if err != nil {
		return fmt.Errorf("introspect sqlite tables: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var tables []sqliteTableRef
	for rows.Next() {
		var ref sqliteTableRef
		if err := rows.Scan(&ref.name, &ref.virtual, &ref.withoutRowID, &ref.strict, &ref.ddl); err != nil {
			return fmt.Errorf("introspect sqlite tables: scanning: %w", err)
		}
		if !tableAllowed(ref.name, opts) {
			continue
		}
		tables = append(tables, ref)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite tables: iterating: %w", err)
	}

	for _, ref := range tables {
		if ref.virtual {
			schema.Warnings = append(schema.Warnings, sqlite.VirtualTableWarning(ref.name))
			continue
		}
		table, err := si.introspectTable(ctx, ref)
		if err != nil {
			return err
		}
		schema.Tables = append(schema.Tables, table)
	}

	return nil
}

func (si *SQLiteIntrospector) introspectTable(ctx context.Context, ref sqliteTableRef) (parser.Table, error) {
	declared, err := parseStoredTable(ref.ddl)
	if err != nil {
		return parser.Table{}, fmt.Errorf("introspect sqlite table %s: %w", ref.name, err)
	}
	table := parser.Table{
		Name:         ref.name,
		WithoutRowID: ref.withoutRowID,
		Strict:       ref.strict,
	}
	indexes, err := si.indexList(ctx, table.Name)
	if err != nil {
		return parser.Table{}, err
	}
	if err := si.introspectColumns(ctx, &table, declared, indexes); err != nil {
		return parser.Table{}, err
	}
	if err := si.introspectForeignKeys(ctx, &table); err != nil {
		return parser.Table{}, err
	}
	if err := si.introspectIndexes(ctx, &table, indexes); err != nil {
		return parser.Table{}, err
	}
	for _, c := range declared.Constraints {
		if c.Type == parser.Check {
			table.Constraints = append(table.Constraints, c)
		}
	}
	if err := nameDeclaredConstraints(&table, declared); err != nil {
		return parser.Table{}, fmt.Errorf("introspect sqlite table %s: %w", ref.name, err)
	}
	parser.CanonicalizeConstraints(&table, "sqlite")
	return table, nil
}

// parseStoredTable reads a table's stored CREATE TABLE with the file parser.
func parseStoredTable(ddl string) (*parser.Table, error) {
	p := sqlite.New()
	if err := p.Parse("sqlite_master", []byte(ddl)); err != nil {
		return nil, &SchemaError{Err: fmt.Errorf("parsing stored CREATE TABLE: %w", err)}
	}
	tables := p.Schema().Tables
	if len(tables) != 1 {
		return nil, &SchemaError{Err: fmt.Errorf("stored CREATE TABLE yields %d tables, want 1", len(tables))}
	}
	return &tables[0], nil
}

// nameDeclaredConstraints gives each unnamed primary key, foreign key and
// UNIQUE constraint read from the PRAGMAs the name its CREATE TABLE declares
// for it, if any. A foreign key matches on its parent table and columns as
// well, since one table can hold several over the same columns. Every such
// constraint must be in the CREATE TABLE; one that is not means the parser
// and the catalog disagree.
func nameDeclaredConstraints(table, declared *parser.Table) error {
	used := make([]bool, len(declared.Constraints))
	for i := range table.Constraints {
		c := &table.Constraints[i]
		if c.Name != "" || c.Type == parser.Check || c.Type == parser.Index {
			continue
		}
		j := -1
		for k, d := range declared.Constraints {
			if !used[k] && d.Type == c.Type && sameColumns(d.Columns, c.Columns) &&
				strings.EqualFold(d.ReferenceTable, c.ReferenceTable) && sameColumns(d.ReferenceColumns, c.ReferenceColumns) {
				j = k
				break
			}
		}
		if j < 0 {
			return &SchemaError{Err: fmt.Errorf("stored CREATE TABLE declares no %s over (%s)", c.Type, strings.Join(c.Columns, ", "))}
		}
		used[j] = true
		c.Name = declared.Constraints[j].Name
	}
	return nil
}

// sameColumns reports whether a and b name the same columns in the same
// order. SQLite column names are case-insensitive.
func sameColumns(a, b []string) bool {
	return slices.EqualFunc(a, b, strings.EqualFold)
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

		declared, err := si.introspectViewColumns(ctx, &view)
		if err != nil {
			return err
		}
		// SQLite reports no type for an expression column; that one stays any.
		catalog := view.Columns
		columnsFromDefinition(&view, ref.sql, sqlite.ParseViewSQL, schema, func(i int) (parser.Column, bool) {
			return catalog[i], declared[i] != ""
		})

		schema.Views = append(schema.Views, view)
	}

	return nil
}

// introspectViewColumns reads view's columns from PRAGMA table_info and
// returns each one's declared type, which is empty for an expression.
func (si *SQLiteIntrospector) introspectViewColumns(ctx context.Context, view *parser.View) ([]string, error) {
	rows, err := si.db.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdent(view.Name)+")")
	if err != nil {
		return nil, fmt.Errorf("introspect sqlite view columns %s: %w", view.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var declared []string
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
			return nil, fmt.Errorf("introspect sqlite view columns %s: scanning: %w", view.Name, err)
		}

		col := parser.Column{
			Name:     name,
			Type:     sqlite.NormalizeType(typeName),
			Nullable: notNull == 0,
		}

		view.Columns = append(view.Columns, col)
		declared = append(declared, typeName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect sqlite view columns %s: iterating: %w", view.Name, err)
	}
	return declared, nil
}

// introspectColumns reads table's columns from PRAGMA table_xinfo, which,
// unlike table_info, lists generated columns. declared is the table's stored
// CREATE TABLE, the only place SQLite keeps a generated column's expression.
func (si *SQLiteIntrospector) introspectColumns(ctx context.Context, table *parser.Table, declared *parser.Table, indexes []sqliteIndex) error {
	rows, err := si.db.QueryContext(ctx, "PRAGMA table_xinfo("+quoteSQLiteIdent(table.Name)+")")
	if err != nil {
		return fmt.Errorf("introspect sqlite columns %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var pkCols []pkCol
	for rows.Next() {
		var (
			cid       int
			name      string
			typeName  string
			notNull   int
			dfltValue sql.NullString
			pk        int
			hidden    int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk, &hidden); err != nil {
			return fmt.Errorf("introspect sqlite columns %s: scanning: %w", table.Name, err)
		}

		col := parser.Column{
			Name:     name,
			Type:     sqlite.NormalizeType(typeName),
			Nullable: notNull == 0,
		}

		storage, ok := sqliteGeneratedStorage(hidden)
		if !ok {
			continue
		}
		col.GeneratedStorage = storage

		if dfltValue.Valid {
			col.Default = dfltValue.String
		}

		if pk > 0 {
			col.PrimaryKey = true
			col.Nullable = false
			pkCols = append(pkCols, pkCol{name: name, order: pk})
		}

		table.Columns = append(table.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("introspect sqlite columns %s: iterating: %w", table.Name, err)
	}

	if err := sqliteGeneratedExprs(table, declared); err != nil {
		return fmt.Errorf("introspect sqlite columns %s: %w", table.Name, err)
	}

	if len(pkCols) == 0 {
		return nil
	}
	// pk is the column's position in the key, which can differ from its
	// position in the table: PRIMARY KEY (b, a).
	slices.SortFunc(pkCols, func(a, b pkCol) int { return a.order - b.order })
	constraint := parser.Constraint{Type: parser.PrimaryKey}
	for _, pc := range pkCols {
		constraint.Columns = append(constraint.Columns, pc.name)
	}
	table.Constraints = append(table.Constraints, constraint)

	// A single-column primary key with no index of its own is the rowid
	// alias, the only key SQLite fills in. INT, DESC and WITHOUT ROWID keys
	// all get an index.
	hasKeyIndex := slices.ContainsFunc(indexes, func(idx sqliteIndex) bool { return idx.origin == "pk" })
	if len(pkCols) == 1 && !hasKeyIndex {
		parser.ColumnByName(table, pkCols[0].name).AutoIncrement = true
	}
	return nil
}

// sqliteGeneratedStorage maps PRAGMA table_xinfo's hidden value to a
// column's GeneratedStorage: 0 is an ordinary column, 2 a VIRTUAL and 3 a
// STORED generated column. ok is false for 1, a virtual table's hidden
// column, which is not part of the model.
func sqliteGeneratedStorage(hidden int) (storage string, ok bool) {
	switch hidden {
	case 1:
		return "", false
	case 2:
		return "VIRTUAL", true
	case 3:
		return "STORED", true
	}
	return "", true
}

// sqliteGeneratedExprs sets GeneratedExpr on table's generated columns from
// declared, its stored CREATE TABLE as the file parser reads it, so the text
// matches the files path.
func sqliteGeneratedExprs(table, declared *parser.Table) error {
	for i := range table.Columns {
		col := &table.Columns[i]
		if col.GeneratedStorage == "" {
			continue
		}
		dc := parser.ColumnByName(declared, col.Name)
		if dc == nil || dc.GeneratedExpr == "" {
			return &SchemaError{Err: fmt.Errorf("stored CREATE TABLE has no expression for generated column %s", col.Name)}
		}
		col.GeneratedExpr = dc.GeneratedExpr
	}
	return nil
}

type pkCol struct {
	name  string
	order int
}

// sqliteFKRow is one row of PRAGMA foreign_key_list: column seq of foreign
// key id. to is NULL for REFERENCES t with no column list, which reading the
// stored CREATE TABLE rejects before foreign keys are read (PRD §6.3).
type sqliteFKRow struct {
	id       int
	seq      int
	refTable string
	from     string
	to       sql.NullString
}

func (si *SQLiteIntrospector) introspectForeignKeys(ctx context.Context, table *parser.Table) error {
	rows, err := si.db.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdent(table.Name)+")")
	if err != nil {
		return fmt.Errorf("introspect sqlite foreign keys %s: %w", table.Name, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var fkRows []sqliteFKRow
	for rows.Next() {
		var (
			r         sqliteFKRow
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

	// Group rows by FK id for composite FKs.
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
		c.ReferenceColumns = append(c.ReferenceColumns, r.to.String)
	}

	// PRAGMA foreign_key_list numbers foreign keys in reverse declaration
	// order: id 0 is the last declared. Walking from the highest id keeps the
	// constraints, and the FK a column's FKReference takes when it has
	// several, in declaration order, as the file parser does.
	slices.Sort(order)
	for _, id := range slices.Backward(order) {
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

// sqliteIndex is one row of PRAGMA index_list. origin is "pk" for the
// primary key's index, "u" for a UNIQUE constraint's and "c" for CREATE INDEX.
type sqliteIndex struct {
	name    string
	unique  bool
	origin  string
	partial bool
}

func (si *SQLiteIntrospector) indexList(ctx context.Context, table string) ([]sqliteIndex, error) {
	rows, err := si.db.QueryContext(ctx, "PRAGMA index_list("+quoteSQLiteIdent(table)+")")
	if err != nil {
		return nil, fmt.Errorf("introspect sqlite indexes %s: %w", table, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Close error is not actionable after reading

	var indexes []sqliteIndex
	for rows.Next() {
		var (
			idx     sqliteIndex
			seq     int
			unique  int
			partial int
		)
		if err := rows.Scan(&seq, &idx.name, &unique, &idx.origin, &partial); err != nil {
			return nil, fmt.Errorf("introspect sqlite indexes %s: scanning: %w", table, err)
		}
		idx.unique, idx.partial = unique == 1, partial == 1
		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect sqlite indexes %s: iterating: %w", table, err)
	}
	return indexes, nil
}

// introspectIndexes adds a constraint for each index on table except the
// primary key's. A UNIQUE constraint's index carries an auto-index name, so
// it is left unnamed here for nameDeclaredConstraints; a CREATE [UNIQUE]
// INDEX keeps its own name. SQLite indexes are always btree, which the parser
// records for CREATE INDEX only. A non-unique expression-only index projects
// no columns and is skipped, matching the PostgreSQL sweep. Partial indexes
// keep their WHERE predicate (PRD §30.7).
func (si *SQLiteIntrospector) introspectIndexes(ctx context.Context, table *parser.Table, indexes []sqliteIndex) error {
	for _, idx := range indexes {
		if idx.origin == "pk" {
			continue
		}
		cols, err := si.introspectIndexColumns(ctx, idx.name)
		if err != nil {
			return err
		}
		c := parser.Constraint{Type: parser.Unique, Columns: cols}
		switch {
		case idx.origin == "u":
		case idx.unique:
			c.Name, c.Method = idx.name, "btree"
		case len(cols) == 0:
			continue
		default:
			c.Type, c.Name, c.Method = parser.Index, idx.name, "btree"
		}
		if idx.partial {
			if c.Where, err = si.indexPartialWhere(ctx, idx.name); err != nil {
				return err
			}
		}
		table.Constraints = append(table.Constraints, c)
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
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("introspect sqlite index sql %s: %w", indexName, err)
	}
	if !ddl.Valid {
		return "", nil
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
	rows, err := si.db.QueryContext(ctx, "PRAGMA index_info("+quoteSQLiteIdent(indexName)+")")
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

// quoteSQLiteIdent quotes name as a SQL identifier, doubling any embedded
// double quote.
func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
