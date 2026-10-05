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

	for _, stmt := range stmts {
		if err := p.processStmt(stmt); err != nil {
			return fmt.Errorf("parsing %s: %w", filename, err)
		}
	}

	return nil
}

// Schema returns the accumulated schema model.
func (p *Parser) Schema() *parser.Schema {
	return p.schema
}

func (p *Parser) processStmt(stmt rsql.Statement) error {
	switch s := stmt.(type) {
	case *rsql.CreateTableStatement:
		return p.parseCreateTable(s)
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
	parser.ApplyConstraintToColumns(table, c)
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

func (p *Parser) parseCreateTable(stmt *rsql.CreateTableStatement) error {
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
	for _, col := range stmt.Columns {
		c := parseColumn(col, withoutRowID)
		table.Columns = append(table.Columns, c)
	}

	// Parse table-level constraints.
	for _, constraint := range stmt.Constraints {
		c := parseTableConstraint(constraint)
		if c.Type != 0 {
			table.Constraints = append(table.Constraints, c)
		}
	}

	parser.ApplyTableConstraints(&table)

	p.tableIndex[tableName] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

	return nil
}

func parseColumn(col *rsql.ColumnDefinition, withoutRowID bool) parser.Column {
	c := parser.Column{
		Name:     rsql.IdentName(col.Name),
		Type:     buildTypeString(col.Type),
		Nullable: true,
	}

	for _, constraint := range col.Constraints {
		applyColumnConstraint(&c, constraint)
	}

	// INTEGER PRIMARY KEY is auto-increment (rowid alias) unless WITHOUT ROWID.
	if c.PrimaryKey && c.Type == "integer" && !withoutRowID {
		c.AutoIncrement = true
	}

	return c
}

func applyColumnConstraint(c *parser.Column, constraint rsql.Constraint) {
	switch ct := constraint.(type) {
	case *rsql.PrimaryKeyConstraint:
		c.PrimaryKey = true
		c.Nullable = false
		if ct.Autoincrement.IsValid() {
			c.AutoIncrement = true
		}
	case *rsql.NotNullConstraint:
		c.Nullable = false
	case *rsql.UniqueConstraint:
		c.Unique = true
		c.InlineUnique = true
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
		}
	case *rsql.GeneratedConstraint:
		c.GeneratedExpr = rsql.ExprString(ct.Expr)
		if ct.Stored.IsValid() {
			c.GeneratedStorage = "STORED"
		} else {
			c.GeneratedStorage = "VIRTUAL"
		}
	case *rsql.CheckConstraint, *rsql.CollateConstraint:
		// CHECK is captured at table level; COLLATE is not stored.
	}
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
		c := parseColumn(stmt.ColumnDef, table.WithoutRowID)
		table.Columns = append(table.Columns, c)

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
