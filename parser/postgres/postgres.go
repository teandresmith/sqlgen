// Package postgres provides a PostgreSQL dialect parser using pg_query_go.
package postgres

import (
	"fmt"
	"slices"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// Parser implements the parser.Parser interface for PostgreSQL DDL.
// It uses pg_query_go (which wraps libpg_query via CGO) to parse SQL
// using PostgreSQL's own parser.
type Parser struct {
	schema        *parser.Schema
	defaultSchema string
	tableIndex    map[string]int // qualified "schema.name" → index in schema.Tables
}

// New creates a new PostgreSQL parser. If defaultSchema is empty, "public" is used.
func New(defaultSchema string) *Parser {
	if defaultSchema == "" {
		defaultSchema = "public"
	}
	return &Parser{
		schema:        &parser.Schema{},
		defaultSchema: defaultSchema,
		tableIndex:    make(map[string]int),
	}
}

// Seed pre-populates the parser with an existing schema. This enables "both"
// mode where introspected tables are loaded before file-based DDL is applied.
// CREATE TABLE on a seeded table produces a duplicate error; ALTER TABLE on
// a seeded table modifies it in place.
func (p *Parser) Seed(base *parser.Schema) {
	p.schema = base
	p.tableIndex = make(map[string]int, len(base.Tables))
	for i, t := range base.Tables {
		p.tableIndex[t.Schema+"."+t.Name] = i
	}
}

// Parse parses PostgreSQL DDL from sql. It applies the statements one at a
// time, in file order, as PostgreSQL does: a statement sees the schema the
// statements before it left, so a table moved or renamed mid-file no longer
// answers to its old name, and the old name is free for a later CREATE.
// It can be called multiple times with different files; results accumulate.
func (p *Parser) Parse(filename string, sql []byte) error {
	result, err := pg_query.Parse(string(sql))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	for _, rawStmt := range result.Stmts {
		if err := p.processStmt(rawStmt.Stmt); err != nil {
			return fmt.Errorf("parsing %s: %w", filename, err)
		}
	}

	return nil
}

// Schema returns the accumulated schema model.
func (p *Parser) Schema() *parser.Schema {
	return p.schema
}

func (p *Parser) processStmt(node *pg_query.Node) error {
	switch n := node.Node.(type) {
	case *pg_query.Node_CommentStmt:
		return p.parseComment(n.CommentStmt)
	case *pg_query.Node_AlterTableStmt:
		return p.parseAlterTable(n.AlterTableStmt)
	case *pg_query.Node_DropStmt:
		return p.parseDrop(n.DropStmt)
	case *pg_query.Node_RenameStmt:
		return p.parseRename(n.RenameStmt)
	case *pg_query.Node_AlterObjectSchemaStmt:
		return p.parseAlterObjectSchema(n.AlterObjectSchemaStmt)
	case *pg_query.Node_IndexStmt:
		return p.parseCreateIndex(n.IndexStmt)
	case *pg_query.Node_CreateStmt:
		return p.parseCreateTable(n.CreateStmt)
	case *pg_query.Node_CreateEnumStmt:
		return p.parseCreateEnum(n.CreateEnumStmt)
	case *pg_query.Node_CompositeTypeStmt:
		return p.parseCompositeType(n.CompositeTypeStmt)
	case *pg_query.Node_CreateDomainStmt:
		return p.parseCreateDomain(n.CreateDomainStmt)
	case *pg_query.Node_ViewStmt:
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE VIEW %s skipped in DDL file — use introspection or define the view in input.views",
				n.ViewStmt.View.Relname))
	case *pg_query.Node_CreateTableAsStmt:
		// CREATE MATERIALIZED VIEW parses as CreateTableAsStmt with a matview
		// object type; plain CREATE TABLE AS keeps its pre-existing silent skip.
		if n.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
			p.schema.Warnings = append(p.schema.Warnings,
				fmt.Sprintf("CREATE MATERIALIZED VIEW %s skipped in DDL file — use introspection or define the view in input.views",
					n.CreateTableAsStmt.Into.Rel.Relname))
		}
	}
	return nil
}

func (p *Parser) parseCreateTable(stmt *pg_query.CreateStmt) error {
	schema := stmt.Relation.Schemaname
	if schema == "" {
		schema = p.defaultSchema
	}

	key := schema + "." + stmt.Relation.Relname
	if _, exists := p.tableIndex[key]; exists {
		if stmt.IfNotExists {
			return nil
		}
		return fmt.Errorf("table %s already exists", key)
	}

	table := parser.Table{
		Name:   stmt.Relation.Relname,
		Schema: schema,
	}

	for _, elt := range stmt.TableElts {
		switch n := elt.Node.(type) {
		case *pg_query.Node_ColumnDef:
			col := p.parseColumnDef(n.ColumnDef)
			table.Columns = append(table.Columns, col)
		case *pg_query.Node_Constraint:
			constraint := p.parseConstraintNode(n.Constraint)
			table.Constraints = append(table.Constraints, constraint)
		}
	}

	parser.ApplyTableConstraints(&table)

	p.tableIndex[key] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

	return nil
}

func (p *Parser) parseCreateEnum(stmt *pg_query.CreateEnumStmt) error {
	names := extractStringList(stmt.TypeName)
	schema, name := p.splitQualifiedName(names)

	for _, e := range p.schema.Enums {
		if e.Schema == schema && e.Name == name {
			return fmt.Errorf("type %s.%s already exists", schema, name)
		}
	}

	values := make([]string, 0, len(stmt.Vals))
	for _, v := range stmt.Vals {
		if s := v.GetString_(); s != nil {
			values = append(values, s.Sval)
		}
	}

	p.schema.Enums = append(p.schema.Enums, parser.Enum{
		Name:   name,
		Schema: schema,
		Values: values,
	})
	return nil
}

func (p *Parser) parseCompositeType(stmt *pg_query.CompositeTypeStmt) error {
	rv := stmt.Typevar
	schema := rv.Schemaname
	if schema == "" {
		schema = p.defaultSchema
	}

	for _, ct := range p.schema.CompositeTypes {
		if ct.Schema == schema && ct.Name == rv.Relname {
			return fmt.Errorf("type %s.%s already exists", schema, rv.Relname)
		}
	}

	ct := parser.CompositeType{
		Name:   rv.Relname,
		Schema: schema,
	}

	for _, col := range stmt.Coldeflist {
		if cd, ok := col.Node.(*pg_query.Node_ColumnDef); ok {
			ct.Attributes = append(ct.Attributes, parser.Attribute{
				Name: cd.ColumnDef.Colname,
				Type: typeNameToString(cd.ColumnDef.TypeName),
			})
		}
	}

	p.schema.CompositeTypes = append(p.schema.CompositeTypes, ct)
	return nil
}

func (p *Parser) parseCreateDomain(stmt *pg_query.CreateDomainStmt) error {
	names := extractStringList(stmt.Domainname)
	schema, name := p.splitQualifiedName(names)

	for _, dt := range p.schema.DomainTypes {
		if dt.Schema == schema && dt.Name == name {
			return fmt.Errorf("domain %s.%s already exists", schema, name)
		}
	}

	dt := parser.DomainType{
		Name:     name,
		Schema:   schema,
		BaseType: typeNameToString(stmt.TypeName),
	}

	for _, c := range stmt.Constraints {
		if cn, ok := c.Node.(*pg_query.Node_Constraint); ok {
			dt.Constraints = append(dt.Constraints, p.parseConstraintNode(cn.Constraint))
		}
	}

	p.schema.DomainTypes = append(p.schema.DomainTypes, dt)
	return nil
}

// parseCreateIndex records a standalone CREATE [UNIQUE] INDEX as a Constraint
// on the target table. CREATE INDEX defaults to btree when no USING clause is
// given (PRD §30.7 expects "btree" / "gin" / "gist" / "hash"); WhereClause
// holds the partial-index predicate text.
func (p *Parser) parseCreateIndex(stmt *pg_query.IndexStmt) error {
	if stmt == nil || stmt.Relation == nil {
		return nil
	}
	table, err := p.lookupTable(stmt.Relation)
	if err != nil {
		return nil //nolint:nilerr // CREATE INDEX on unknown tables is skipped silently to match DROP TABLE handling
	}

	method := stmt.AccessMethod
	if method == "" {
		method = "btree"
	}

	cType := parser.Index
	if stmt.Unique {
		cType = parser.Unique
	}

	c := parser.Constraint{
		Name:    stmt.Idxname,
		Type:    cType,
		Columns: indexParamColumns(stmt.IndexParams),
		Method:  method,
		Where:   deparseExpr(stmt.WhereClause),
	}
	table.Constraints = append(table.Constraints, c)
	parser.ApplyConstraintToColumns(table, c)
	return nil
}

// indexParamColumns extracts column names from a CREATE INDEX parameter list.
// Expression-only entries (e.g. CREATE INDEX ... ON t (lower(x))) have an
// empty IndexElem.Name and are skipped — the manifest's index surface is
// column-oriented.
func indexParamColumns(params []*pg_query.Node) []string {
	cols := make([]string, 0, len(params))
	for _, p := range params {
		e, ok := p.Node.(*pg_query.Node_IndexElem)
		if !ok || e.IndexElem == nil {
			continue
		}
		if name := e.IndexElem.Name; name != "" {
			cols = append(cols, name)
		}
	}
	return cols
}

func (p *Parser) parseRename(stmt *pg_query.RenameStmt) error {
	switch stmt.RenameType { //nolint:exhaustive // only TABLE and COLUMN renames are relevant
	case pg_query.ObjectType_OBJECT_TABLE:
		if stmt.Relation == nil {
			return nil
		}
		table, err := p.lookupTable(stmt.Relation)
		if err != nil {
			if stmt.MissingOk {
				return nil
			}
			return err
		}
		oldKey := table.Schema + "." + table.Name
		// PostgreSQL keys a foreign key on the referenced table's OID, so a
		// rename carries every reference along with it.
		parser.RenameTableReferences(p.schema.Tables, table.Schema, table.Name, stmt.Newname)
		table.Name = stmt.Newname
		newKey := table.Schema + "." + stmt.Newname
		idx := p.tableIndex[oldKey]
		delete(p.tableIndex, oldKey)
		p.tableIndex[newKey] = idx

	case pg_query.ObjectType_OBJECT_COLUMN:
		if stmt.Relation == nil {
			return nil
		}
		table, err := p.lookupTable(stmt.Relation)
		if err != nil {
			if stmt.MissingOk {
				return nil
			}
			return err
		}
		if err := parser.RenameColumn(table, stmt.Subname, stmt.Newname); err != nil {
			return fmt.Errorf("rename column %s.%s: %w", table.Schema+"."+table.Name, stmt.Subname, err)
		}
		// Constraints and foreign keys name the column by its attribute
		// number, so they follow the rename.
		parser.RenameColumnReferences(p.schema.Tables, table.Schema, table.Name, stmt.Subname, stmt.Newname)
		return nil
	}
	return nil
}

// parseAlterObjectSchema applies ALTER TABLE … SET SCHEMA, which pg_query
// parses as an AlterObjectSchemaStmt, not an AlterTableStmt. Only tables are
// modeled; moving any other object is ignored.
func (p *Parser) parseAlterObjectSchema(stmt *pg_query.AlterObjectSchemaStmt) error {
	if stmt.ObjectType != pg_query.ObjectType_OBJECT_TABLE || stmt.Relation == nil {
		return nil
	}
	table, err := p.lookupTable(stmt.Relation)
	if err != nil {
		if stmt.MissingOk {
			return nil
		}
		return err
	}
	if table.Schema == stmt.Newschema {
		return nil // PostgreSQL accepts a move into the current schema as a no-op.
	}
	oldKey := table.Schema + "." + table.Name
	newKey := stmt.Newschema + "." + table.Name
	if _, exists := p.tableIndex[newKey]; exists {
		return fmt.Errorf("table %s already exists", newKey)
	}
	// PostgreSQL keys a foreign key on the referenced table's OID, so a move
	// carries every reference along with it.
	parser.MoveTableReferences(p.schema.Tables, table.Name, table.Schema, stmt.Newschema)
	table.Schema = stmt.Newschema
	idx := p.tableIndex[oldKey]
	delete(p.tableIndex, oldKey)
	p.tableIndex[newKey] = idx
	return nil
}

func (p *Parser) parseDrop(stmt *pg_query.DropStmt) error {
	for _, obj := range stmt.Objects {
		names := extractDropObjectNames(obj)
		switch stmt.RemoveType {
		case pg_query.ObjectType_OBJECT_TABLE:
			schema, name := p.splitQualifiedName(names)
			if err := p.dropTable(schema, name); err != nil {
				return err
			}
		case pg_query.ObjectType_OBJECT_TYPE:
			schema, name := p.splitQualifiedName(names)
			if err := p.dropType(schema, name); err != nil {
				return err
			}
		case pg_query.ObjectType_OBJECT_DOMAIN:
			schema, name := p.splitQualifiedName(names)
			if err := p.dropDomain(schema, name); err != nil {
				return err
			}
		case pg_query.ObjectType_OBJECT_VIEW:
			// DROP VIEW is skipped gracefully — views are not tracked in DDL parsing.
		default:
			// Other object types (indexes, etc.) are not handled.
		}
	}
	return nil
}

// extractDropObjectNames extracts name parts from a DROP statement's object node.
// DROP TABLE uses Node_List, while DROP TYPE/DOMAIN uses Node_TypeName.
func extractDropObjectNames(node *pg_query.Node) []string {
	if node == nil {
		return nil
	}
	// DROP TABLE objects are lists of string names.
	if list, ok := node.Node.(*pg_query.Node_List); ok {
		return extractStringList(list.List.Items)
	}
	// DROP TYPE / DROP DOMAIN objects are TypeName nodes.
	if tn, ok := node.Node.(*pg_query.Node_TypeName); ok {
		return extractStringList(tn.TypeName.Names)
	}
	if s := node.GetString_(); s != nil {
		return []string{s.Sval}
	}
	return nil
}

func (p *Parser) dropTable(schema, name string) error {
	key := schema + "." + name
	idx, ok := p.tableIndex[key]
	if !ok {
		return nil
	}
	if err := parser.CheckTableDropRefs(p.schema.Tables, schema, name); err != nil {
		return fmt.Errorf("drop table %s.%s: %w", schema, name, err)
	}
	p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
	delete(p.tableIndex, key)
	// Shift indices for tables after the removed one.
	for k, v := range p.tableIndex {
		if v > idx {
			p.tableIndex[k] = v - 1
		}
	}
	return nil
}

func (p *Parser) dropType(schema, name string) error {
	if err := parser.CheckTypeDropRefs(p.schema.Tables, schema, name); err != nil {
		return fmt.Errorf("drop type %s.%s: %w", schema, name, err)
	}
	p.schema.Enums = slices.DeleteFunc(p.schema.Enums, func(e parser.Enum) bool {
		return e.Schema == schema && e.Name == name
	})
	p.schema.CompositeTypes = slices.DeleteFunc(p.schema.CompositeTypes, func(ct parser.CompositeType) bool {
		return ct.Schema == schema && ct.Name == name
	})
	return nil
}

func (p *Parser) dropDomain(schema, name string) error {
	if err := parser.CheckTypeDropRefs(p.schema.Tables, schema, name); err != nil {
		return fmt.Errorf("drop domain %s.%s: %w", schema, name, err)
	}
	p.schema.DomainTypes = slices.DeleteFunc(p.schema.DomainTypes, func(dt parser.DomainType) bool {
		return dt.Schema == schema && dt.Name == name
	})
	return nil
}

func (p *Parser) parseComment(stmt *pg_query.CommentStmt) error {
	switch stmt.Objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		names := extractObjectNames(stmt.Object)
		table := p.findTableByNames(names)
		if table == nil {
			return fmt.Errorf("comment on unknown table %s", strings.Join(names, "."))
		}
		table.Comment = stmt.Comment

	case pg_query.ObjectType_OBJECT_COLUMN:
		names := extractObjectNames(stmt.Object)
		if len(names) < 2 {
			return fmt.Errorf("comment on column: invalid name %s", strings.Join(names, "."))
		}
		colName := names[len(names)-1]
		tableNames := names[:len(names)-1]
		table := p.findTableByNames(tableNames)
		if table == nil {
			return fmt.Errorf("comment on column of unknown table %s", strings.Join(tableNames, "."))
		}
		for i := range table.Columns {
			if table.Columns[i].Name == colName {
				table.Columns[i].Comment = stmt.Comment
				return nil
			}
		}
		return fmt.Errorf("comment on unknown column %s.%s", strings.Join(tableNames, "."), colName)

	case pg_query.ObjectType_OBJECT_TYPE:
		names := extractTypeObjectNames(stmt.Object)
		schema, name := p.splitQualifiedName(names)
		if e := p.findEnumByName(schema, name); e != nil {
			e.Comment = stmt.Comment
			return nil
		}
		if ct := p.findCompositeTypeByName(schema, name); ct != nil {
			ct.Comment = stmt.Comment
			return nil
		}
		return fmt.Errorf("comment on unknown type %s.%s", schema, name)

	case pg_query.ObjectType_OBJECT_DOMAIN:
		names := extractTypeObjectNames(stmt.Object)
		schema, name := p.splitQualifiedName(names)
		if dt := p.findDomainByName(schema, name); dt != nil {
			dt.Comment = stmt.Comment
			return nil
		}
		return fmt.Errorf("comment on unknown domain %s.%s", schema, name)

	default:
		// Other object types (views, indexes, etc.) are not supported yet.
	}
	return nil
}

func (p *Parser) parseAlterTable(stmt *pg_query.AlterTableStmt) error {
	table, err := p.lookupTable(stmt.Relation)
	if err != nil {
		if stmt.MissingOk {
			return nil // ALTER TABLE IF EXISTS — table doesn't exist, skip
		}
		return err
	}

	for _, cmd := range stmt.Cmds {
		atCmd, ok := cmd.Node.(*pg_query.Node_AlterTableCmd)
		if !ok {
			continue
		}
		if err := p.applyAlterCmd(table, atCmd.AlterTableCmd); err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) applyAlterCmd(table *parser.Table, cmd *pg_query.AlterTableCmd) error {
	switch cmd.Subtype {
	case pg_query.AlterTableType_AT_AddColumn:
		return p.alterAddColumn(table, cmd)
	case pg_query.AlterTableType_AT_DropColumn:
		alterDropColumn(table, cmd)
	case pg_query.AlterTableType_AT_AddConstraint:
		p.alterAddConstraint(table, cmd)
	case pg_query.AlterTableType_AT_DropConstraint:
		alterDropConstraint(table, cmd)
	case pg_query.AlterTableType_AT_SetNotNull:
		if col := parser.ColumnByName(table, cmd.Name); col != nil {
			col.Nullable = false
		}
	case pg_query.AlterTableType_AT_DropNotNull:
		if col := parser.ColumnByName(table, cmd.Name); col != nil {
			col.Nullable = true
		}
	case pg_query.AlterTableType_AT_ColumnDefault:
		alterColumnDefault(table, cmd)
	case pg_query.AlterTableType_AT_AlterColumnType:
		alterColumnType(table, cmd)
	default:
		// Other ALTER TABLE subtypes are not supported yet.
	}

	return nil
}

func (p *Parser) alterAddColumn(table *parser.Table, cmd *pg_query.AlterTableCmd) error {
	if cmd.Def == nil {
		return nil
	}
	cd, ok := cmd.Def.Node.(*pg_query.Node_ColumnDef)
	if !ok {
		return nil
	}
	if cmd.MissingOk && parser.ColumnByName(table, cd.ColumnDef.Colname) != nil {
		return nil // ADD COLUMN IF NOT EXISTS — column already exists, skip
	}
	col := p.parseColumnDef(cd.ColumnDef)
	table.Columns = append(table.Columns, col)
	return nil
}

func alterDropColumn(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.MissingOk && parser.ColumnByName(table, cmd.Name) == nil {
		return // DROP COLUMN IF EXISTS — column doesn't exist, skip
	}
	table.Columns = slices.DeleteFunc(table.Columns, func(c parser.Column) bool {
		return c.Name == cmd.Name
	})
}

func (p *Parser) alterAddConstraint(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.Def == nil {
		return
	}
	cn, ok := cmd.Def.Node.(*pg_query.Node_Constraint)
	if !ok {
		return
	}
	constraint := p.parseConstraintNode(cn.Constraint)
	table.Constraints = append(table.Constraints, constraint)
	parser.ApplyConstraintToColumns(table, constraint)
}

func alterDropConstraint(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.MissingOk && !hasConstraint(table, cmd.Name) {
		return // DROP CONSTRAINT IF EXISTS — constraint doesn't exist, skip
	}
	removeConstraintFromColumns(table, cmd.Name)
}

func alterColumnDefault(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if col := parser.ColumnByName(table, cmd.Name); col != nil {
		if cmd.Def != nil {
			col.Default = deparseExpr(cmd.Def)
		} else {
			col.Default = "" // DROP DEFAULT
		}
	}
}

func alterColumnType(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.Def == nil {
		return
	}
	cd, ok := cmd.Def.Node.(*pg_query.Node_ColumnDef)
	if !ok {
		return
	}
	if col := parser.ColumnByName(table, cmd.Name); col != nil {
		col.Type = typeNameToString(cd.ColumnDef.TypeName)
	}
}

// --- Column parsing ---

func (p *Parser) parseColumnDef(cd *pg_query.ColumnDef) parser.Column {
	col := parser.Column{
		Name:     cd.Colname,
		Nullable: true,
	}

	col.Type = typeNameToString(cd.TypeName)

	// Detect serial types (preserved as-is in the raw parse tree).
	switch strings.ToLower(col.Type) {
	case "serial", "serial4":
		col.Type = "integer"
		col.AutoIncrement = true
		col.Nullable = false
	case "bigserial", "serial8":
		col.Type = "bigint"
		col.AutoIncrement = true
		col.Nullable = false
	case "smallserial", "serial2":
		col.Type = "smallint"
		col.AutoIncrement = true
		col.Nullable = false
	}

	if cd.IsNotNull {
		col.Nullable = false
	}

	for _, c := range cd.Constraints {
		cn, ok := c.Node.(*pg_query.Node_Constraint)
		if !ok {
			continue
		}
		p.applyColumnConstraint(&col, cn.Constraint)
	}

	return col
}

func (p *Parser) applyColumnConstraint(col *parser.Column, c *pg_query.Constraint) {
	switch c.Contype {
	case pg_query.ConstrType_CONSTR_NOTNULL:
		col.Nullable = false

	case pg_query.ConstrType_CONSTR_NULL:
		col.Nullable = true

	case pg_query.ConstrType_CONSTR_PRIMARY:
		col.PrimaryKey = true
		col.Nullable = false

	case pg_query.ConstrType_CONSTR_UNIQUE:
		col.Unique = true
		col.InlineUnique = true

	case pg_query.ConstrType_CONSTR_DEFAULT:
		col.Default = deparseExpr(c.RawExpr)
		if strings.Contains(strings.ToLower(col.Default), "nextval(") {
			col.AutoIncrement = true
		}

	case pg_query.ConstrType_CONSTR_IDENTITY:
		col.AutoIncrement = true
		col.Nullable = false

	case pg_query.ConstrType_CONSTR_FOREIGN:
		if c.Pktable != nil {
			fkSchema := p.referenceSchema(c.Pktable)
			var fkCol string
			if len(c.PkAttrs) > 0 {
				if s := c.PkAttrs[0].GetString_(); s != nil {
					fkCol = s.Sval
				}
			}
			col.FKReference = &parser.FKReference{
				Table:  c.Pktable.Relname,
				Schema: fkSchema,
				Column: fkCol,
			}
		}

	default:
		// Other constraint types (IDENTITY, GENERATED, etc.) are handled elsewhere or ignored.
	}
}

// --- Constraint parsing ---

func (p *Parser) parseConstraintNode(c *pg_query.Constraint) parser.Constraint {
	constraint := parser.Constraint{
		Name: c.Conname,
	}

	switch c.Contype {
	case pg_query.ConstrType_CONSTR_PRIMARY:
		constraint.Type = parser.PrimaryKey
		constraint.Columns = extractStringList(c.Keys)

	case pg_query.ConstrType_CONSTR_UNIQUE:
		constraint.Type = parser.Unique
		constraint.Columns = extractStringList(c.Keys)

	case pg_query.ConstrType_CONSTR_CHECK:
		constraint.Type = parser.Check
		constraint.CheckExpression = deparseExpr(c.RawExpr)

	case pg_query.ConstrType_CONSTR_FOREIGN:
		constraint.Type = parser.ForeignKey
		constraint.Columns = extractStringList(c.FkAttrs)
		if c.Pktable != nil {
			constraint.ReferenceTable = c.Pktable.Relname
			constraint.ReferenceSchema = p.referenceSchema(c.Pktable)
		}
		constraint.ReferenceColumns = extractStringList(c.PkAttrs)

	default:
		// Other constraint types not relevant at table level.
	}

	return constraint
}

// referenceSchema resolves the schema of a REFERENCES target. An unqualified
// target resolves the way every other unqualified name in the file does — to
// input.schema (default "public") — not to the schema of the table declaring
// the key. PostgreSQL resolves it through the search path, so
// `CREATE TABLE audit.labels (… REFERENCES documents(id))` references
// public.documents, not audit.documents.
func (p *Parser) referenceSchema(rv *pg_query.RangeVar) string {
	if rv.Schemaname != "" {
		return rv.Schemaname
	}
	return p.defaultSchema
}

// hasConstraint reports whether the table has a constraint with the given name.
func hasConstraint(table *parser.Table, name string) bool {
	for _, c := range table.Constraints {
		if c.Name == name {
			return true
		}
	}
	return false
}

// removeConstraintFromColumns removes the named constraint from
// table.Constraints and reverses its column flag effects. The slice
// deletion happens first so that RecomputeColumnUnique sees the
// post-drop state of the table when re-deriving col.Unique from the
// remaining constraints + the column's InlineUnique flag.
func removeConstraintFromColumns(table *parser.Table, constraintName string) {
	idx := slices.IndexFunc(table.Constraints, func(c parser.Constraint) bool {
		return c.Name == constraintName
	})
	if idx < 0 {
		return
	}
	c := table.Constraints[idx]
	table.Constraints = slices.Delete(table.Constraints, idx, idx+1)
	switch c.Type {
	case parser.PrimaryKey:
		for _, colName := range c.Columns {
			if col := parser.ColumnByName(table, colName); col != nil {
				col.PrimaryKey = false
			}
		}
	case parser.Unique:
		if len(c.Columns) == 1 {
			parser.RecomputeColumnUnique(table, c.Columns[0])
		}
	case parser.ForeignKey:
		if len(c.Columns) == 1 {
			if col := parser.ColumnByName(table, c.Columns[0]); col != nil {
				col.FKReference = nil
			}
		}

	case parser.Check, parser.Index:
		// CHECK and INDEX constraints don't affect column flags.
	}
}

// --- Type name helpers ---

// typeNameToString converts a pg_query TypeName to a SQL type string.
func typeNameToString(tn *pg_query.TypeName) string {
	if tn == nil {
		return ""
	}

	var names []string
	for _, n := range tn.Names {
		if s := n.GetString_(); s != nil {
			names = append(names, s.Sval)
		}
	}
	if len(names) == 0 {
		return ""
	}

	var typeName string
	if len(names) >= 2 && names[0] == "pg_catalog" {
		typeName = pgCatalogToSQL(names[len(names)-1])
	} else {
		// Normalize bare short-form aliases (e.g. "bool" → "boolean")
		// that bypass pg_catalog.
		typeName = pgCatalogToSQL(strings.Join(names, "."))
	}

	typeName += formatTypmods(tn.Typmods)

	if len(tn.ArrayBounds) > 0 {
		typeName += "[]"
	}

	return typeName
}

// pgCatalogToSQL maps PostgreSQL internal type names to SQL-friendly names.
func pgCatalogToSQL(internal string) string {
	switch internal {
	case "int2":
		return "smallint"
	case "int4":
		return "integer"
	case "int8":
		return "bigint"
	case "float4":
		return "real"
	case "float8":
		return "double precision"
	case "bool":
		return "boolean"
	case "varchar":
		return "varchar"
	case "bpchar":
		return "char"
	case "timestamptz":
		return "timestamptz"
	case "timetz":
		return "timetz"
	default:
		return internal
	}
}

// formatTypmods formats type modifier values like (10, 2) for numeric(10, 2).
func formatTypmods(typmods []*pg_query.Node) string {
	if len(typmods) == 0 {
		return ""
	}
	parts := make([]string, 0, len(typmods))
	for _, m := range typmods {
		if ac := m.GetAConst(); ac != nil {
			switch v := ac.Val.(type) {
			case *pg_query.A_Const_Ival:
				parts = append(parts, fmt.Sprintf("%d", v.Ival.Ival))
			case *pg_query.A_Const_Fval:
				parts = append(parts, v.Fval.Fval)
			case *pg_query.A_Const_Sval:
				parts = append(parts, v.Sval.Sval)
			}
		} else if ic := m.GetInteger(); ic != nil {
			parts = append(parts, fmt.Sprintf("%d", ic.Ival))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// --- Utility functions ---

// deparseExpr converts a pg_query expression node back to SQL text.
func deparseExpr(node *pg_query.Node) string {
	if node == nil {
		return ""
	}
	tree := &pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{
			{
				Stmt: &pg_query.Node{
					Node: &pg_query.Node_SelectStmt{
						SelectStmt: &pg_query.SelectStmt{
							TargetList: []*pg_query.Node{
								{
									Node: &pg_query.Node_ResTarget{
										ResTarget: &pg_query.ResTarget{
											Val: node,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	sql, err := pg_query.Deparse(tree)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(sql, "SELECT ")
}

// extractStringList extracts string values from a []*Node list.
func extractStringList(nodes []*pg_query.Node) []string {
	result := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if s := n.GetString_(); s != nil {
			result = append(result, s.Sval)
		}
	}
	return result
}

// extractObjectNames extracts names from a Node containing a List of Strings.
func extractObjectNames(node *pg_query.Node) []string {
	if node == nil {
		return nil
	}
	if list, ok := node.Node.(*pg_query.Node_List); ok {
		return extractStringList(list.List.Items)
	}
	if s := node.GetString_(); s != nil {
		return []string{s.Sval}
	}
	return nil
}

// extractTypeObjectNames extracts name parts from a COMMENT ON TYPE/DOMAIN object node.
// These are represented as TypeName nodes in pg_query, not plain lists.
func extractTypeObjectNames(node *pg_query.Node) []string {
	if node == nil {
		return nil
	}
	if tn, ok := node.Node.(*pg_query.Node_TypeName); ok {
		return extractStringList(tn.TypeName.Names)
	}
	if list, ok := node.Node.(*pg_query.Node_List); ok {
		return extractStringList(list.List.Items)
	}
	return nil
}

// findEnumByName returns a pointer to the enum with the given schema and name, or nil.
func (p *Parser) findEnumByName(schema, name string) *parser.Enum {
	for i := range p.schema.Enums {
		if p.schema.Enums[i].Schema == schema && p.schema.Enums[i].Name == name {
			return &p.schema.Enums[i]
		}
	}
	return nil
}

// findCompositeTypeByName returns a pointer to the composite type with the given schema and name, or nil.
func (p *Parser) findCompositeTypeByName(schema, name string) *parser.CompositeType {
	for i := range p.schema.CompositeTypes {
		if p.schema.CompositeTypes[i].Schema == schema && p.schema.CompositeTypes[i].Name == name {
			return &p.schema.CompositeTypes[i]
		}
	}
	return nil
}

// findDomainByName returns a pointer to the domain type with the given schema and name, or nil.
func (p *Parser) findDomainByName(schema, name string) *parser.DomainType {
	for i := range p.schema.DomainTypes {
		if p.schema.DomainTypes[i].Schema == schema && p.schema.DomainTypes[i].Name == name {
			return &p.schema.DomainTypes[i]
		}
	}
	return nil
}

// splitQualifiedName splits a list of name parts into (schema, name).
func (p *Parser) splitQualifiedName(names []string) (schema, name string) {
	switch len(names) {
	case 0:
		return p.defaultSchema, ""
	case 1:
		return p.defaultSchema, names[0]
	default:
		return names[0], names[1]
	}
}

// lookupTable finds a table by its RangeVar reference.
func (p *Parser) lookupTable(rv *pg_query.RangeVar) (*parser.Table, error) {
	schema := rv.Schemaname
	if schema == "" {
		schema = p.defaultSchema
	}
	key := schema + "." + rv.Relname
	idx, ok := p.tableIndex[key]
	if !ok {
		return nil, fmt.Errorf("table %s not found", key)
	}
	return &p.schema.Tables[idx], nil
}

// findTableByNames finds a table from a list of name parts ([schema, table]).
func (p *Parser) findTableByNames(names []string) *parser.Table {
	var schema, tableName string
	switch len(names) {
	case 1:
		schema = p.defaultSchema
		tableName = names[0]
	case 2:
		schema = names[0]
		tableName = names[1]
	default:
		return nil
	}
	key := schema + "." + tableName
	idx, ok := p.tableIndex[key]
	if !ok {
		return nil
	}
	return &p.schema.Tables[idx]
}
