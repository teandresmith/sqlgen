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
	tableIndex map[string]int // table name → index in schema.Tables
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
		p.tableIndex[t.Name] = i
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
	case *rsql.DropViewStatement:
		// DROP VIEW is skipped gracefully — views are not tracked in DDL parsing.
	}
	return nil
}

// parseCreateIndex records a standalone CREATE [UNIQUE] INDEX as a Constraint
// on the target table. SQLite indexes are always btree, but partial indexes
// (CREATE INDEX ... WHERE expr) need to surface their predicate via the
// Constraint.Where field for the manifest's index surface (PRD §30.7).
func (p *Parser) parseCreateIndex(stmt *rsql.CreateIndexStatement) error {
	if stmt == nil || stmt.Table == nil {
		return nil
	}
	tableName := rsql.IdentName(stmt.Table)
	idx, ok := p.tableIndex[tableName]
	if !ok {
		return nil
	}
	table := &p.schema.Tables[idx]

	cType := parser.Index
	if stmt.Unique.IsValid() {
		cType = parser.Unique
	}

	c := parser.Constraint{
		Name:    rsql.IdentName(stmt.Name),
		Type:    cType,
		Columns: indexedColumnNames(stmt.Columns),
		Method:  "btree",
	}
	if stmt.WhereExpr != nil {
		c.Where = rsql.ExprString(stmt.WhereExpr)
	}
	table.Constraints = append(table.Constraints, c)
	parser.CanonicalizeConstraints(table, "sqlite")
	return nil
}

func (p *Parser) parseDropTable(stmt *rsql.DropTableStatement) error {
	name := rsql.IdentName(stmt.Name)
	idx, ok := p.tableIndex[name]
	if !ok {
		return nil
	}
	if err := parser.CheckTableDropRefs(p.schema.Tables, "", name); err != nil {
		return fmt.Errorf("drop table %s: %w", name, err)
	}
	p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
	delete(p.tableIndex, name)
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
	if _, exists := p.tableIndex[tableName]; exists {
		if stmt.IfNotExists.IsValid() {
			return nil
		}
		return fmt.Errorf("table %s already exists", tableName)
	}

	withoutRowID := stmt.Without.IsValid()
	strict := stmt.Strict.IsValid()

	table := parser.Table{
		Name:         tableName,
		WithoutRowID: withoutRowID,
		Strict:       strict,
	}

	// Parse columns.
	for i, col := range stmt.Columns {
		if i > 0 && isPrimaryKeySortOrder(stmt.Columns[i-1], col, src) {
			key := &table.Columns[len(table.Columns)-1]
			for _, constraint := range col.Constraints {
				if c, ok := applyColumnConstraint(key, constraint); ok {
					table.Constraints = append(table.Constraints, c)
				}
			}
			continue
		}
		c, constraints := parseColumn(col, withoutRowID)
		table.Columns = append(table.Columns, c)
		table.Constraints = append(table.Constraints, constraints...)
	}

	// Parse table-level constraints.
	for _, constraint := range stmt.Constraints {
		c := parseTableConstraint(constraint)
		if c.Type != 0 {
			table.Constraints = append(table.Constraints, c)
		}
	}

	parser.CanonicalizeConstraints(&table, "sqlite")
	dropDuplicateUniques(&table)

	p.tableIndex[tableName] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

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

// dropDuplicateUniques drops each UNIQUE whose index SQLite never builds:
// one over the same columns, in the same order, as the primary key's index or
// an earlier UNIQUE. A rowid-alias INTEGER PRIMARY KEY has no index, so a
// UNIQUE on it stays.
func dropDuplicateUniques(table *parser.Table) {
	var indexed [][]string
	for _, c := range table.Constraints {
		if c.Type == parser.PrimaryKey && !isRowIDAlias(table, c) {
			indexed = append(indexed, c.Columns)
		}
	}
	table.Constraints = slices.DeleteFunc(table.Constraints, func(c parser.Constraint) bool {
		if c.Type != parser.Unique {
			return false
		}
		if slices.ContainsFunc(indexed, func(cols []string) bool { return slices.Equal(cols, c.Columns) }) {
			return true
		}
		indexed = append(indexed, c.Columns)
		return false
	})
	for i := range table.Columns {
		parser.RecomputeColumnUnique(table, table.Columns[i].Name)
	}
}

// isRowIDAlias reports whether the primary key pk makes its column an alias
// for the rowid: a single INTEGER column in a table that has a rowid.
func isRowIDAlias(table *parser.Table, pk parser.Constraint) bool {
	if table.WithoutRowID || len(pk.Columns) != 1 {
		return false
	}
	col := parser.ColumnByName(table, pk.Columns[0])
	return col != nil && col.Type == "integer"
}

// parseColumn returns the column col defines and the constraints declared
// inline on it, as table constraints over the column.
func parseColumn(col *rsql.ColumnDefinition, withoutRowID bool) (parser.Column, []parser.Constraint) {
	c := parser.Column{
		Name:     rsql.IdentName(col.Name),
		Type:     buildTypeString(col.Type),
		Nullable: true,
	}

	var constraints []parser.Constraint
	for _, constraint := range col.Constraints {
		if tc, ok := applyColumnConstraint(&c, constraint); ok {
			constraints = append(constraints, tc)
		}
	}

	// INTEGER PRIMARY KEY is auto-increment (rowid alias) unless WITHOUT ROWID.
	if c.PrimaryKey && c.Type == "integer" && !withoutRowID {
		c.AutoIncrement = true
	}

	return c, constraints
}

// applyColumnConstraint applies an inline column constraint to c. A PRIMARY
// KEY, UNIQUE, REFERENCES or CHECK is also returned as a table constraint over
// the column.
func applyColumnConstraint(c *parser.Column, constraint rsql.Constraint) (parser.Constraint, bool) {
	switch ct := constraint.(type) {
	case *rsql.PrimaryKeyConstraint:
		c.PrimaryKey = true
		c.Nullable = false
		if ct.Autoincrement.IsValid() {
			c.AutoIncrement = true
		}
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.PrimaryKey, Columns: []string{c.Name}}, true
	case *rsql.NotNullConstraint:
		c.Nullable = false
	case *rsql.UniqueConstraint:
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.Unique, Columns: []string{c.Name}}, true
	case *rsql.DefaultConstraint:
		c.Default = rsql.ExprString(ct.Expr)
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
			}, true
		}
	case *rsql.GeneratedConstraint:
		c.GeneratedExpr = rsql.ExprString(ct.Expr)
		if ct.Stored.IsValid() {
			c.GeneratedStorage = "STORED"
		} else {
			c.GeneratedStorage = "VIRTUAL"
		}
	case *rsql.CheckConstraint:
		return parser.Constraint{Name: rsql.IdentName(ct.Name), Type: parser.Check, CheckExpression: rsql.ExprString(ct.Expr)}, true
	case *rsql.CollateConstraint:
		// COLLATE is not stored.
	}
	return parser.Constraint{}, false
}

// buildTypeString returns the lowercased SQL type string from a rqlite/sql Type.
// Unrecognized type names default to "blob"; SQLite itself gives such a
// column NUMERIC affinity (BLOB affinity goes only to a declared type that
// contains "BLOB", or to a column with none).
func buildTypeString(typ *rsql.Type) string {
	if typ == nil || typ.Name == nil {
		return "blob"
	}

	baseName := strings.ToLower(typ.Name.Name)
	if !isRecognizedType(baseName) {
		return "blob"
	}

	return strings.ToLower(typ.String())
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

func parseTableConstraint(constraint rsql.Constraint) parser.Constraint {
	switch ct := constraint.(type) {
	case *rsql.PrimaryKeyConstraint:
		return parser.Constraint{
			Name:    rsql.IdentName(ct.Name),
			Type:    parser.PrimaryKey,
			Columns: identNames(ct.Columns),
		}
	case *rsql.UniqueConstraint:
		return parser.Constraint{
			Name:    rsql.IdentName(ct.Name),
			Type:    parser.Unique,
			Columns: indexedColumnNames(ct.Columns),
		}
	case *rsql.ForeignKeyConstraint:
		return parser.Constraint{
			Name:             rsql.IdentName(ct.Name),
			Type:             parser.ForeignKey,
			Columns:          identNames(ct.Columns),
			ReferenceTable:   rsql.IdentName(ct.ForeignTable),
			ReferenceColumns: identNames(ct.ForeignColumns),
		}
	case *rsql.CheckConstraint:
		return parser.Constraint{
			Name:            rsql.IdentName(ct.Name),
			Type:            parser.Check,
			CheckExpression: rsql.ExprString(ct.Expr),
		}
	}
	return parser.Constraint{}
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
		c, constraints := parseColumn(stmt.ColumnDef, table.WithoutRowID)
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
		return nil

	case stmt.Rename.IsValid() && stmt.NewName != nil:
		// RENAME TABLE
		return p.renameTable(tableName, rsql.IdentName(stmt.NewName))

	default:
		return fmt.Errorf("unsupported ALTER TABLE operation on %s", tableName)
	}

	return nil
}

func (p *Parser) renameTable(oldName, newName string) error {
	idx, ok := p.tableIndex[oldName]
	if !ok {
		return fmt.Errorf("table %s not found", oldName)
	}
	// Keys referencing the table follow it to its new name, as they do in the
	// database; left on the old name, relationship detection drops the edge.
	parser.RenameTableReferences(p.schema.Tables, p.schema.Tables[idx].Schema, oldName, newName)
	p.schema.Tables[idx].Name = newName
	delete(p.tableIndex, oldName)
	p.tableIndex[newName] = idx
	return nil
}

// --- Helpers ---

func (p *Parser) lookupTable(name string) (*parser.Table, bool) {
	idx, ok := p.tableIndex[name]
	if !ok {
		return nil, false
	}
	return &p.schema.Tables[idx], true
}

func identNames(idents []*rsql.Ident) []string {
	names := make([]string, len(idents))
	for i, ident := range idents {
		names[i] = rsql.IdentName(ident)
	}
	return names
}

func indexedColumnNames(cols []*rsql.IndexedColumn) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		if ident, ok := col.X.(*rsql.Ident); ok {
			names[i] = ident.Name
		} else {
			names[i] = rsql.ExprString(col.X)
		}
	}
	return names
}
