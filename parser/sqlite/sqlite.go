// Package sqlite implements the parser.Parser interface for SQLite DDL
// using rqlite/sql (pure Go, no CGO).
package sqlite

import (
	"fmt"
	"slices"
	"strings"

	rsql "github.com/rqlite/sql"

	"github.com/teandresmith/sqlgen/parser"
)

// Parser implements the parser.Parser interface for SQLite DDL.
// It uses rqlite/sql (pure Go) to parse SQLite SQL statements.
type Parser struct {
	schema     *parser.Schema
	tableIndex map[string]int // foldASCII(table name) → index in schema.Tables
}

// New creates a new SQLite parser.
func New() *Parser {
	return &Parser{
		schema:     &parser.Schema{},
		tableIndex: make(map[string]int),
	}
}

// Seed pre-populates the parser with an existing schema. This enables "both"
// mode where introspected tables are loaded before file-based DDL is applied.
func (p *Parser) Seed(base *parser.Schema) {
	p.schema = base
	p.tableIndex = make(map[string]int, len(base.Tables))
	for i, t := range base.Tables {
		p.tableIndex[foldASCII(t.Name)] = i
	}
}

// Parse parses SQLite DDL from sql. It can be called multiple times with
// different files; results accumulate.
func (p *Parser) Parse(filename string, sql []byte) error {
	rp := rsql.NewParser(strings.NewReader(string(sql)))
	stmts, err := rp.ParseStatements()
	if err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	// rqlite/sql positions count runes.
	src := []rune(string(sql))
	for _, stmt := range stmts {
		if err := p.processStmt(stmt, src); err != nil {
			return fmt.Errorf("parsing %s: %w", filename, err)
		}
	}

	return nil
}

// Schema returns the accumulated schema model.
func (p *Parser) Schema() *parser.Schema {
	return p.schema
}

func (p *Parser) processStmt(stmt rsql.Statement, src []rune) error {
	switch s := stmt.(type) {
	case *rsql.CreateTableStatement:
		return p.parseCreateTable(s, src)
	case *rsql.AlterTableStatement:
		return p.parseAlterTable(s)
	case *rsql.DropTableStatement:
		return p.parseDropTable(s)
	case *rsql.CreateIndexStatement:
		return p.parseCreateIndex(s)
	case *rsql.CreateViewStatement:
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE VIEW %s skipped in DDL file — use introspection or define the view in input.views",
				rsql.IdentName(s.Name)))
	case *rsql.CreateVirtualTableStatement:
		p.schema.Warnings = append(p.schema.Warnings, VirtualTableWarning(rsql.IdentName(s.Name)))
	case *rsql.DropViewStatement:
		// DROP VIEW is skipped gracefully — views are not tracked in DDL parsing.
	}
	return nil
}

// VirtualTableWarning is the schema warning for a skipped virtual table (FTS5,
// R*Tree, …). The introspector skips virtual tables with the same warning.
func VirtualTableWarning(table string) string {
	return fmt.Sprintf("CREATE VIRTUAL TABLE %s skipped — virtual tables are not generated", table)
}

// parseCreateIndex records a standalone CREATE [UNIQUE] INDEX as a Constraint
// on the target table. SQLite indexes are always btree, but partial indexes
// (CREATE INDEX ... WHERE expr) need to surface their predicate via the
// Constraint.Where field for the manifest's index surface (PRD §30.7). An
// index with an expression part is skipped, as the introspector skips it.
func (p *Parser) parseCreateIndex(stmt *rsql.CreateIndexStatement) error {
	if stmt == nil || stmt.Table == nil {
		return nil
	}
	table, ok := p.lookupTable(rsql.IdentName(stmt.Table))
	if !ok {
		return nil
	}
	cols, ok := indexedColumnNames(stmt.Columns)
	if !ok {
		return nil
	}

	cType := parser.Index
	if stmt.Unique.IsValid() {
		cType = parser.Unique
	}

	c := parser.Constraint{
		Name:    rsql.IdentName(stmt.Name),
		Type:    cType,
		Columns: cols,
		Method:  "btree",
	}
	where, err := indexWhere(stmt)
	if err != nil {
		return fmt.Errorf("index %s: %w", c.Name, err)
	}
	c.Where = where
	table.Constraints = append(table.Constraints, c)
	parser.CanonicalizeConstraints(table, "sqlite")
	return nil
}

// IndexWhere returns the WHERE predicate of one CREATE INDEX statement as the
// parser records it in Constraint.Where, or "" for an index that is not
// partial, so introspection can give the same text as the files source
// (PRD §6.1).
func IndexWhere(ddl string) (string, error) {
	stmts, err := rsql.NewParser(strings.NewReader(ddl)).ParseStatements()
	if err != nil {
		return "", fmt.Errorf("parsing CREATE INDEX: %w", err)
	}
	if len(stmts) != 1 {
		return "", fmt.Errorf("parsing CREATE INDEX: got %d statements, want 1", len(stmts))
	}
	stmt, ok := stmts[0].(*rsql.CreateIndexStatement)
	if !ok {
		return "", fmt.Errorf("parsing CREATE INDEX: got %T", stmts[0])
	}
	return indexWhere(stmt)
}

// indexWhere renders a CREATE INDEX statement's WHERE predicate, or "" when it
// has none. A predicate exprString cannot render is a schema error on both
// sources (PRD §6.3), not a crash.
func indexWhere(stmt *rsql.CreateIndexStatement) (string, error) {
	if stmt.WhereExpr == nil {
		return "", nil
	}
	return exprString(stmt.WhereExpr, "CREATE INDEX predicate")
}

// exprString renders e as rsql.ExprString does; what names e in an error.
// rqlite/sql parses some expressions it then panics rendering (LIKE … ESCAPE:
// "sql.BinaryExpr.String(): invalid op ESCAPE"); the panic is returned as an
// error.
func exprString(e rsql.Expr, what string) (s string, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = "", fmt.Errorf("rendering %s: %v", what, r)
		}
	}()
	return rsql.ExprString(e), nil
}

func (p *Parser) parseDropTable(stmt *rsql.DropTableStatement) error {
	name := rsql.IdentName(stmt.Name)
	idx, ok := p.tableIndex[foldASCII(name)]
	if !ok {
		return nil
	}
	if err := parser.CheckTableDropRefs(p.schema.Tables, "", p.schema.Tables[idx].Name); err != nil {
		return fmt.Errorf("drop table %s: %w", name, err)
	}
	p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
	delete(p.tableIndex, foldASCII(name))
	for k, v := range p.tableIndex {
		if v > idx {
			p.tableIndex[k] = v - 1
		}
	}
	return nil
}

// --- CREATE TABLE ---

func (p *Parser) parseCreateTable(stmt *rsql.CreateTableStatement, src []rune) error {
	tableName := rsql.IdentName(stmt.Name)
	if _, exists := p.tableIndex[foldASCII(tableName)]; exists {
		if stmt.IfNotExists.IsValid() {
			return nil
		}
		return fmt.Errorf("table %s already exists", tableName)
	}

	table := parser.Table{
		Name:         tableName,
		WithoutRowID: stmt.Without.IsValid(),
		Strict:       stmt.Strict.IsValid(),
	}

	// Parse columns.
	var descKey string // the column whose PRIMARY KEY is declared DESC
	coll := indexCollations{columns: make(map[string]string), explicit: make(map[int][]string)}
	starts := columnStarts(stmt, src)
	for i, col := range stmt.Columns {
		if i > 0 && isPrimaryKeySortOrder(stmt.Columns[i-1], col, src) {
			key := &table.Columns[len(table.Columns)-1]
			if strings.EqualFold(col.Name.Name, "DESC") {
				descKey = key.Name
			}
			constraints, err := applyColumnConstraints(key, col.Constraints)
			if err != nil {
				return fmt.Errorf("table %s: %w", tableName, err)
			}
			table.Constraints = append(table.Constraints, constraints...)
			coll.addColumn(key.Name, col.Constraints)
			continue
		}
		if i > 0 && !starts[col.Name.NamePos.Offset] {
			return fmt.Errorf("table %s: %s: %q after column %s is not a column: a quoted type name or a MATCH clause cannot be read",
				tableName, col.Name.NamePos, col.Name.Name, table.Columns[len(table.Columns)-1].Name)
		}
		c, constraints, err := parseColumn(col)
		if err != nil {
			return fmt.Errorf("table %s: %w", tableName, err)
		}
		table.Columns = append(table.Columns, c)
		table.Constraints = append(table.Constraints, constraints...)
		coll.addColumn(c.Name, col.Constraints)
	}

	if err := addTableConstraints(&table, stmt.Constraints, coll); err != nil {
		return fmt.Errorf("table %s: %w", tableName, err)
	}

	// CanonicalizeConstraints only appends, so coll.explicit's positions hold.
	parser.CanonicalizeConstraints(&table, "sqlite")
	alias := rowIDAlias(&table, descKey)
	if col := parser.ColumnByName(&table, alias); col != nil {
		col.AutoIncrement = true
	}
	dropDuplicateUniques(&table, alias, coll)

	p.tableIndex[foldASCII(tableName)] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

	return nil
}

// addTableConstraints appends the table-level constraints to table, recording
// each UNIQUE's explicit collations in coll.
func addTableConstraints(table *parser.Table, constraints []rsql.Constraint, coll indexCollations) error {
	for _, constraint := range constraints {
		c, err := parseTableConstraint(constraint)
		if err != nil {
			return err
		}
		if c.Type == 0 {
			continue
		}
		if u, ok := constraint.(*rsql.UniqueConstraint); ok {
			coll.explicit[len(table.Constraints)] = indexedColumnCollations(u.Columns)
		}
		table.Constraints = append(table.Constraints, c)
	}
	return nil
}

// isPrimaryKeySortOrder reports whether col is the ASC or DESC of the column
// PRIMARY KEY that ends prev. rqlite/sql reads that keyword as the name of a
// new column, along with any constraints after it. Only blanks and comments
// can separate it from KEY; after a comma it is a real column named asc or
// desc.
func isPrimaryKeySortOrder(prev, col *rsql.ColumnDefinition, src []rune) bool {
	if col.Name == nil || col.Name.Quoted || col.Type != nil ||
		(!strings.EqualFold(col.Name.Name, "ASC") && !strings.EqualFold(col.Name.Name, "DESC")) {
		return false
	}
	if len(prev.Constraints) == 0 {
		return false
	}
	pk, ok := prev.Constraints[len(prev.Constraints)-1].(*rsql.PrimaryKeyConstraint)
	if !ok || pk.Autoincrement.IsValid() {
		return false
	}
	from, to := pk.Key.Offset+len("KEY"), col.Name.NamePos.Offset
	if from > to || to > len(src) {
		return false
	}
	return onlyBlanksAndComments(string(src[from:to]))
}

// columnStarts returns the offsets in src of the tokens that follow the
// opening paren or a comma of stmt's column list. rqlite/sql does not require
// the comma between column definitions, so a token it cannot read as part of
// one column, such as a quoted type name or MATCH, starts another; a column
// whose name is not at one of these offsets is such a token.
func columnStarts(stmt *rsql.CreateTableStatement, src []rune) map[int]bool {
	starts := make(map[int]bool)
	from, to := stmt.Lparen.Offset, stmt.Rparen.Offset
	if from > to || to > len(src) {
		return starts
	}
	s := rsql.NewScanner(strings.NewReader(string(src[from:to])))
	afterDelim := false
	for {
		pos, tok, _ := s.Scan()
		if tok == rsql.EOF {
			return starts
		}
		if tok == rsql.COMMENT {
			continue
		}
		if afterDelim {
			starts[from+pos.Offset] = true
		}
		afterDelim = tok == rsql.LP || tok == rsql.COMMA
	}
}

func onlyBlanksAndComments(s string) bool {
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		switch {
		case strings.HasPrefix(s, "--"):
			_, s, _ = strings.Cut(s, "\n")
		case strings.HasPrefix(s, "/*"):
			_, s, _ = strings.Cut(s[2:], "*/")
		default:
			return false
		}
	}
	return true
}

// indexCollations holds the collations SQLite compares when it decides two
// indexes over the same columns are one: each column's declared COLLATE, and
// the per-column COLLATE of each table-level UNIQUE, keyed by its position in
// table.Constraints ("" where none is written).
type indexCollations struct {
	columns  map[string]string
	explicit map[int][]string
}

// addColumn records the COLLATE among column's constraints, if any.
func (ic indexCollations) addColumn(column string, constraints []rsql.Constraint) {
	for _, c := range constraints {
		if cc, ok := c.(*rsql.CollateConstraint); ok && cc.Collation != nil {
			ic.columns[strings.ToLower(column)] = strings.ToUpper(cc.Collation.Name)
		}
	}
}

// key identifies the index behind constraint i, c: its columns, each with the
// collation it is indexed under (the explicit one, else the column's, else
// BINARY). Sort order is not part of it.
func (ic indexCollations) key(i int, c parser.Constraint) string {
	var b strings.Builder
	explicit := ic.explicit[i]
	for k, col := range c.Columns {
		collation := ""
		if k < len(explicit) {
			collation = explicit[k]
		}
		if collation == "" {
			collation = ic.columns[strings.ToLower(col)]
		}
		if collation == "" {
			collation = "BINARY"
		}
		b.WriteString(strings.ToLower(col) + "\x00" + collation + "\x00")
	}
	return b.String()
}

// dropDuplicateUniques drops each UNIQUE whose index SQLite never builds:
// one over the same columns, in the same order and under the same
// collations, as the primary key's index or an earlier UNIQUE. A primary key
// on alias, the rowid-alias column (see rowIDAlias), has no index, so a
// UNIQUE on it stays.
func dropDuplicateUniques(table *parser.Table, alias string, coll indexCollations) {
	indexed := make(map[string]bool)
	for i, c := range table.Constraints {
		if c.Type == parser.PrimaryKey && !slices.Equal(c.Columns, []string{alias}) {
			indexed[coll.key(i, c)] = true
		}
	}
	kept := table.Constraints[:0]
	for i, c := range table.Constraints {
		if c.Type == parser.Unique {
			k := coll.key(i, c)
			if indexed[k] {
				continue
			}
			indexed[k] = true
		}
		kept = append(kept, c)
	}
	table.Constraints = kept
	for i := range table.Columns {
		parser.RecomputeColumnUnique(table, table.Columns[i].Name)
	}
}

// rowIDAlias returns the column that aliases the table's rowid, or "" when
// none does. It is the one column of an INTEGER primary key in a table with a
// rowid, unless the column declares its key DESC (descKey). Only that key gets
// an automatic value, and SQLite builds no index for it.
func rowIDAlias(table *parser.Table, descKey string) string {
	if table.WithoutRowID {
		return ""
	}
	for _, c := range table.Constraints {
		if c.Type != parser.PrimaryKey || len(c.Columns) != 1 || c.Columns[0] == descKey {
			continue
		}
		if col := parser.ColumnByName(table, c.Columns[0]); col != nil && col.Type == "integer" {
			return col.Name
		}
	}
	return ""
}

// parseColumn returns the column col defines and the constraints declared
// inline on it, as table constraints over the column.
func parseColumn(col *rsql.ColumnDefinition) (parser.Column, []parser.Constraint, error) {
	c := parser.Column{
		Name:     rsql.IdentName(col.Name),
		Type:     buildTypeString(col.Type),
		Nullable: true,
	}
	constraints, err := applyColumnConstraints(&c, col.Constraints)
	if err != nil {
		return parser.Column{}, nil, err
	}
	return c, constraints, nil
}

// applyColumnConstraints applies each inline column constraint to c and
// returns those that are also table constraints over the column.
func applyColumnConstraints(c *parser.Column, constraints []rsql.Constraint) ([]parser.Constraint, error) {
	var tcs []parser.Constraint
	for _, constraint := range constraints {
		tc, ok, err := applyColumnConstraint(c, constraint)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", c.Name, err)
		}
		if ok {
			tcs = append(tcs, tc)
		}
	}
	return tcs, nil
}

// applyColumnConstraint applies an inline column constraint to c. A PRIMARY
// KEY, UNIQUE, REFERENCES or CHECK is also returned as a table constraint over
// the column.
func applyColumnConstraint(c *parser.Column, constraint rsql.Constraint) (parser.Constraint, bool, error) {
	switch ct := constraint.(type) {
	case *rsql.PrimaryKeyConstraint:
		c.PrimaryKey = true
		c.Nullable = false
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.PrimaryKey, Columns: []string{c.Name}}, true, nil
	case *rsql.NotNullConstraint:
		c.Nullable = false
	case *rsql.UniqueConstraint:
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.Unique, Columns: []string{c.Name}}, true, nil
	case *rsql.DefaultConstraint:
		def, err := exprString(ct.Expr, "DEFAULT")
		if err != nil {
			return parser.Constraint{}, false, err
		}
		c.Default = def
	case *rsql.ForeignKeyConstraint:
		if ct.ForeignTable != nil {
			var refCol string
			if len(ct.ForeignColumns) > 0 {
				refCol = rsql.IdentName(ct.ForeignColumns[0])
			}
			c.FKReference = &parser.FKReference{
				Table:  rsql.IdentName(ct.ForeignTable),
				Column: refCol,
			}
			return parser.Constraint{
				Name:             rsql.IdentName(ct.Name),
				Type:             parser.ForeignKey,
				Columns:          []string{c.Name},
				ReferenceTable:   rsql.IdentName(ct.ForeignTable),
				ReferenceColumns: identNames(ct.ForeignColumns),
			}, true, nil
		}
	case *rsql.GeneratedConstraint:
		expr, err := exprString(ct.Expr, "generated column expression")
		if err != nil {
			return parser.Constraint{}, false, err
		}
		c.GeneratedExpr = expr
		if ct.Stored.IsValid() {
			c.GeneratedStorage = "STORED"
		} else {
			c.GeneratedStorage = "VIRTUAL"
		}
	case *rsql.CheckConstraint:
		check, err := exprString(ct.Expr, "CHECK")
		if err != nil {
			return parser.Constraint{}, false, err
		}
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.Check, CheckExpression: check}, true, nil
	case *rsql.CollateConstraint:
		// COLLATE is not stored.
	}
	return parser.Constraint{}, false, nil
}

// buildTypeString returns the type string for a rqlite/sql Type, by the rule
// NormalizeType states.
func buildTypeString(typ *rsql.Type) string {
	if typ == nil || typ.Name == nil {
		return "blob"
	}
	return NormalizeType(typ.String())
}

// NormalizeType returns the type the parser records for a column declared
// with type declared: lower case, a type modifier written without spaces, and
// blob for an empty or unrecognized type. SQLite itself gives an unrecognized
// type NUMERIC affinity (BLOB affinity goes only to a declared type that
// contains "BLOB", or to a column with none). The introspector applies it to
// the declared type the catalog reports, so both sources agree.
func NormalizeType(declared string) string {
	base, mods, hasMods := strings.Cut(strings.ToLower(declared), "(")
	base = strings.Join(strings.Fields(base), " ")
	if base == "" || !isRecognizedType(base) {
		return "blob"
	}
	if !hasMods {
		return base
	}
	return base + "(" + strings.Join(strings.Fields(mods), "")
}

// exactTypes are type names recognized by exact match (not substring).
var exactTypes = map[string]bool{
	"bool": true, "boolean": true,
	"numeric": true, "decimal": true,
	"date": true, "datetime": true, "timestamp": true,
}

// isRecognizedType checks if a base type name matches a known SQLite type.
// Uses SQLite type affinity rules for substring matching (e.g., "int" matches
// integer/tinyint/bigint, "char" matches varchar/character).
func isRecognizedType(name string) bool {
	if exactTypes[name] {
		return true
	}
	// SQLite affinity substrings: any type containing these is recognized.
	for _, sub := range []string{
		"int", "char", "clob", "text", "blob", "real",
		"float"[:4], "double"[:4],
	} {
		if strings.Contains(name, sub) {
			return true
		}
	}
	return false
}

func parseTableConstraint(constraint rsql.Constraint) (parser.Constraint, error) {
	switch ct := constraint.(type) {
	case *rsql.PrimaryKeyConstraint:
		return parser.Constraint{
			Name:    rsql.IdentName(ct.Name),
			Type:    parser.PrimaryKey,
			Columns: identNames(ct.Columns),
		}, nil
	case *rsql.UniqueConstraint:
		// SQLite rejects an expression in a UNIQUE constraint, so every
		// part names a column.
		cols, _ := indexedColumnNames(ct.Columns)
		return parser.Constraint{
			Name:    rsql.IdentName(ct.Name),
			Type:    parser.Unique,
			Columns: cols,
		}, nil
	case *rsql.ForeignKeyConstraint:
		return parser.Constraint{
			Name:             rsql.IdentName(ct.Name),
			Type:             parser.ForeignKey,
			Columns:          identNames(ct.Columns),
			ReferenceTable:   rsql.IdentName(ct.ForeignTable),
			ReferenceColumns: identNames(ct.ForeignColumns),
		}, nil
	case *rsql.CheckConstraint:
		check, err := exprString(ct.Expr, "CHECK")
		if err != nil {
			return parser.Constraint{}, err
		}
		return parser.Constraint{
			Name:            rsql.IdentName(ct.Name),
			Type:            parser.Check,
			CheckExpression: check,
		}, nil
	}
	return parser.Constraint{}, nil
}

// --- ALTER TABLE ---

func (p *Parser) parseAlterTable(stmt *rsql.AlterTableStatement) error {
	tableName := rsql.IdentName(stmt.Name)

	switch {
	case stmt.ColumnDef != nil:
		// ADD COLUMN
		table, ok := p.lookupTable(tableName)
		if !ok {
			return fmt.Errorf("table %s not found", tableName)
		}
		c, constraints, err := parseColumn(stmt.ColumnDef)
		if err != nil {
			return fmt.Errorf("table %s: %w", tableName, err)
		}
		table.Columns = append(table.Columns, c)
		table.Constraints = append(table.Constraints, constraints...)
		parser.CanonicalizeConstraints(table, "sqlite")

	case stmt.DropColumnName != nil:
		// DROP COLUMN (SQLite 3.35+)
		table, ok := p.lookupTable(tableName)
		if !ok {
			return fmt.Errorf("table %s not found", tableName)
		}
		colName := rsql.IdentName(stmt.DropColumnName)
		table.Columns = slices.DeleteFunc(table.Columns, func(c parser.Column) bool {
			return c.Name == colName
		})

	case stmt.ColumnName != nil && stmt.NewColumnName != nil:
		// RENAME COLUMN
		table, ok := p.lookupTable(tableName)
		if !ok {
			return fmt.Errorf("table %s not found", tableName)
		}
		oldName, newName := rsql.IdentName(stmt.ColumnName), rsql.IdentName(stmt.NewColumnName)
		if err := parser.RenameColumn(table, oldName, newName); err != nil {
			return fmt.Errorf("rename column %s: %w", tableName, err)
		}
		parser.RenameColumnReferences(p.schema.Tables, table.Schema, table.Name, oldName, newName)
		if err := renameColumnInExpressions(table, oldName, newName); err != nil {
			return fmt.Errorf("rename column %s: %w", tableName, err)
		}
		return nil

	case stmt.Rename.IsValid() && stmt.NewName != nil:
		// RENAME TABLE
		return p.renameTable(tableName, rsql.IdentName(stmt.NewName))

	default:
		return fmt.Errorf("unsupported ALTER TABLE operation on %s", tableName)
	}

	return nil
}

// renameColumnInExpressions rewrites column oldName to newName in the table's
// CHECK, generated-column and partial-index expressions. SQLite rewrites the
// stored DDL the same way on RENAME COLUMN, and introspection reads that DDL,
// so left on the old name the text differs by source (PRD §6.1).
func renameColumnInExpressions(table *parser.Table, oldName, newName string) error {
	for i := range table.Columns {
		c := &table.Columns[i]
		expr, err := renameColumnInExpr(c.GeneratedExpr, oldName, newName)
		if err != nil {
			return fmt.Errorf("generated column %s: %w", c.Name, err)
		}
		c.GeneratedExpr = expr
	}
	for i := range table.Constraints {
		c := &table.Constraints[i]
		check, err := renameColumnInExpr(c.CheckExpression, oldName, newName)
		if err != nil {
			return fmt.Errorf("check constraint: %w", err)
		}
		where, err := renameColumnInExpr(c.Where, oldName, newName)
		if err != nil {
			return fmt.Errorf("index %s: %w", c.Name, err)
		}
		c.CheckExpression, c.Where = check, where
	}
	return nil
}

// renameColumnInExpr returns expr, as the parser renders it, with every
// reference to column oldName renamed to newName. Names match without regard
// to ASCII case, as SQLite matches them. Function, type, collation and table
// names are left alone, and expr comes back unchanged when it does not name
// the column.
func renameColumnInExpr(expr, oldName, newName string) (string, error) {
	if expr == "" {
		return "", nil
	}
	x, err := rsql.ParseExprString(expr)
	if err != nil {
		return "", fmt.Errorf("parsing %q: %w", expr, err)
	}
	r := &columnRenamer{oldName: oldName, newName: newName, skip: make(map[*rsql.Ident]bool)}
	if _, err := rsql.Walk(r, x); err != nil {
		return "", fmt.Errorf("renaming in %q: %w", expr, err)
	}
	if !r.renamed {
		return expr, nil
	}
	return exprString(x, "renamed expression")
}

// columnRenamer renames the column references in an expression. The walk
// reaches every identifier, so the ones that name something other than a
// column are marked to skip when their parent is visited.
type columnRenamer struct {
	oldName, newName string
	skip             map[*rsql.Ident]bool
	renamed          bool
}

// Visit renames n when it is a reference to the column, and marks the
// identifiers below n that name something else.
func (r *columnRenamer) Visit(n rsql.Node) (rsql.Visitor, rsql.Node, error) {
	switch n := n.(type) {
	case *rsql.Call:
		r.skip[n.Name] = true
		if n.Over != nil {
			r.skip[n.Over.Name] = true
		}
	case *rsql.Type:
		r.skip[n.Name] = true
	case *rsql.CollateExpr:
		if n.Collation != nil {
			r.skip[n.Collation.Name] = true
		}
	case *rsql.QualifiedRef:
		r.skip[n.Schema] = true
		r.skip[n.Table] = true
	case *rsql.Null:
		// rqlite/sql's walk does not descend into IS NULL / NOT NULL.
		if _, err := rsql.Walk(r, n.X); err != nil {
			return nil, nil, fmt.Errorf("walking %s: %w", n, err)
		}
	case *rsql.Ident:
		if !r.skip[n] && foldASCII(n.Name) == foldASCII(r.oldName) {
			n.Name = r.newName
			r.renamed = true
		}
	}
	return r, n, nil
}

// VisitEnd returns n unchanged.
func (r *columnRenamer) VisitEnd(n rsql.Node) (rsql.Node, error) { return n, nil }

func (p *Parser) renameTable(oldName, newName string) error {
	idx, ok := p.tableIndex[foldASCII(oldName)]
	if !ok {
		return fmt.Errorf("table %s not found", oldName)
	}
	// Keys referencing the table follow it to its new name, as they do in the
	// database; left on the old name, relationship detection drops the edge.
	table := &p.schema.Tables[idx]
	parser.RenameTableReferences(p.schema.Tables, table.Schema, table.Name, newName)
	table.Name = newName
	delete(p.tableIndex, foldASCII(oldName))
	p.tableIndex[foldASCII(newName)] = idx
	return nil
}

// --- Helpers ---

func (p *Parser) lookupTable(name string) (*parser.Table, bool) {
	idx, ok := p.tableIndex[foldASCII(name)]
	if !ok {
		return nil, false
	}
	return &p.schema.Tables[idx], true
}

// foldASCII lowers the ASCII letters in an identifier, as SQLite does when it
// matches names: Users and users are one table, while Äb and äb are two.
func foldASCII(name string) string {
	b := []byte(name)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func identNames(idents []*rsql.Ident) []string {
	names := make([]string, len(idents))
	for i, ident := range idents {
		names[i] = rsql.IdentName(ident)
	}
	return names
}

// indexedColumnCollations returns each indexed column's explicit COLLATE, upper
// case, or "" where none is written. The outermost COLLATE counts, inside
// parentheses or not.
func indexedColumnCollations(cols []*rsql.IndexedColumn) []string {
	collations := make([]string, len(cols))
	for i, col := range cols {
		x := col.X
		for paren, ok := x.(*rsql.ParenExpr); ok; paren, ok = x.(*rsql.ParenExpr) {
			x = paren.X
		}
		if collate, ok := x.(*rsql.CollateExpr); ok && collate.Collation != nil && collate.Collation.Name != nil {
			collations[i] = strings.ToUpper(collate.Collation.Name.Name)
		}
	}
	return collations
}

// indexedColumnNames returns the column each indexed column names, and false
// when a part is an expression, which names no column. A collation and
// parentheses are not part of the name: SQLite indexes (a) as column a.
func indexedColumnNames(cols []*rsql.IndexedColumn) ([]string, bool) {
	names := make([]string, len(cols))
	for i, col := range cols {
		x := col.X
		for unwrapped := false; !unwrapped; {
			switch e := x.(type) {
			case *rsql.CollateExpr:
				x = e.X
			case *rsql.ParenExpr:
				x = e.X
			default:
				unwrapped = true
			}
		}
		ident, ok := x.(*rsql.Ident)
		if !ok {
			return nil, false
		}
		names[i] = ident.Name
	}
	return names, true
}
