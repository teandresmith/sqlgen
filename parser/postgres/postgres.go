// Package postgres provides a PostgreSQL dialect parser using pg_query_go.
package postgres

import (
	"cmp"
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
	// skipped holds the qualified names of the relations the parser skips:
	// views, materialized views and partitions. Each still takes its name.
	skipped map[string]bool
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
		skipped:       make(map[string]bool),
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
	p.skipped = make(map[string]bool, len(base.Views))
	for _, v := range base.Views {
		p.skipped[v.Schema+"."+v.Name] = true
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
		p.skipped[p.relationKey(n.ViewStmt.View)] = true
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE VIEW %s skipped in DDL file — use introspection or define the view in input.views",
				n.ViewStmt.View.Relname))
	case *pg_query.Node_CreateTableAsStmt:
		// CREATE MATERIALIZED VIEW parses as CreateTableAsStmt with a matview
		// object type; plain CREATE TABLE AS keeps its pre-existing silent skip.
		if n.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
			p.skipped[p.relationKey(n.CreateTableAsStmt.Into.Rel)] = true
			p.schema.Warnings = append(p.schema.Warnings,
				fmt.Sprintf("CREATE MATERIALIZED VIEW %s skipped in DDL file — use introspection or define the view in input.views",
					n.CreateTableAsStmt.Into.Rel.Relname))
		}
	}
	return nil
}

func (p *Parser) parseCreateTable(stmt *pg_query.CreateStmt) error {
	if stmt.Partbound != nil {
		// A partition is storage for its partitioned table, which is the
		// entity; the introspector skips partitions too.
		var parent string
		if len(stmt.InhRelations) > 0 {
			parent = stmt.InhRelations[0].GetRangeVar().GetRelname()
		}
		p.skipped[p.relationKey(stmt.Relation)] = true
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE TABLE %s PARTITION OF %s skipped in DDL file — the partitioned table is the entity",
				stmt.Relation.Relname, parent))
		return nil
	}

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

	var constraints []parser.Constraint
	for _, elt := range stmt.TableElts {
		switch n := elt.Node.(type) {
		case *pg_query.Node_ColumnDef:
			col, colConstraints := p.parseColumnDef(n.ColumnDef, schema)
			table.Columns = append(table.Columns, col)
			constraints = append(constraints, colConstraints...)
		case *pg_query.Node_Constraint:
			constraints = append(constraints, p.parseConstraintNode(n.Constraint))
		}
	}

	p.tableIndex[key] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)
	p.addConstraints(&p.schema.Tables[len(p.schema.Tables)-1], mergeDuplicateIndexes(constraints))

	return nil
}

// mergeDuplicateIndexes merges the PRIMARY KEY and UNIQUE constraints of one
// CREATE TABLE that cover the same columns, as PostgreSQL does: the primary key
// comes first, then each UNIQUE in declaration order, and a later duplicate
// folds into the earlier one, which takes the later one's name when it has
// none of its own.
func mergeDuplicateIndexes(constraints []parser.Constraint) []parser.Constraint {
	var kept []int
	if pk := slices.IndexFunc(constraints, func(c parser.Constraint) bool { return c.Type == parser.PrimaryKey }); pk >= 0 {
		kept = append(kept, pk)
	}
	dropped := make([]bool, len(constraints))
	for i, c := range constraints {
		if c.Type != parser.Unique {
			continue
		}
		j := slices.IndexFunc(kept, func(k int) bool {
			return slices.Equal(constraints[k].Columns, c.Columns) && constraints[k].Where == c.Where
		})
		if j < 0 {
			kept = append(kept, i)
			continue
		}
		if constraints[kept[j]].Name == "" {
			constraints[kept[j]].Name = c.Name
		}
		dropped[i] = true
	}
	out := make([]parser.Constraint, 0, len(constraints))
	for i, c := range constraints {
		if !dropped[i] {
			out = append(out, c)
		}
	}
	return out
}

// addConstraints adds the constraints one statement creates to table and
// names each unnamed one as PostgreSQL does. PostgreSQL creates the CHECKs
// first, then the primary key, then each UNIQUE, then each FOREIGN KEY, and a
// name clashes with what exists in the table's schema at that moment.
func (p *Parser) addConstraints(table *parser.Table, constraints []parser.Constraint) {
	table.Constraints = append(table.Constraints, constraints...)
	parser.CanonicalizeConstraints(table, "postgres")
	ns := p.namespace(table.Schema)
	for _, kind := range []parser.ConstraintType{parser.Check, parser.PrimaryKey, parser.Unique, parser.ForeignKey} {
		for i := range table.Constraints {
			c := &table.Constraints[i]
			if c.Type == kind && c.Name == "" {
				c.Name = parser.DerivePostgresConstraintName(table.Name, *c, ns.taken(kind))
				ns.add(kind, c.Name)
			}
		}
	}
	// A column's FKReference is its foreign key whose name sorts first, so it
	// is derived again now that every foreign key has its name.
	parser.CanonicalizeConstraints(table, "postgres")
}

// pgNamespace holds the names in use in one schema. An index, or the index
// behind a PRIMARY KEY or UNIQUE, needs a name no table, view, composite type
// or index in the schema has. A constraint needs a name no constraint in the
// schema has, on any table or domain.
type pgNamespace struct {
	relations   map[string]bool
	constraints map[string]bool
}

func (p *Parser) namespace(schema string) pgNamespace {
	ns := pgNamespace{relations: make(map[string]bool), constraints: make(map[string]bool)}
	for _, t := range p.schema.Tables {
		if t.Schema != schema {
			continue
		}
		ns.relations[t.Name] = true
		for _, c := range t.Constraints {
			ns.add(c.Type, c.Name)
		}
		for _, ix := range t.ExpressionIndexes {
			ns.relations[ix.Name] = true
		}
	}
	for key := range p.skipped {
		if s, name, _ := strings.Cut(key, "."); s == schema {
			ns.relations[name] = true
		}
	}
	for _, ct := range p.schema.CompositeTypes {
		if ct.Schema == schema {
			ns.relations[ct.Name] = true
		}
	}
	for _, dt := range p.schema.DomainTypes {
		if dt.Schema != schema {
			continue
		}
		for _, c := range dt.Constraints {
			ns.constraints[c.Name] = true
		}
	}
	return ns
}

func (ns pgNamespace) taken(kind parser.ConstraintType) func(string) bool {
	switch kind {
	case parser.PrimaryKey, parser.Unique:
		return func(name string) bool { return ns.relations[name] || ns.constraints[name] }
	case parser.Index:
		return func(name string) bool { return ns.relations[name] }
	case parser.ForeignKey, parser.Check:
	}
	return func(name string) bool { return ns.constraints[name] }
}

func (ns pgNamespace) add(kind parser.ConstraintType, name string) {
	if name == "" {
		return
	}
	switch kind {
	case parser.PrimaryKey, parser.Unique:
		ns.relations[name] = true
		ns.constraints[name] = true
	case parser.Index:
		ns.relations[name] = true
	case parser.ForeignKey, parser.Check:
		ns.constraints[name] = true
	}
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
				Type: p.typeName(cd.ColumnDef.TypeName, schema),
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
		BaseType: p.typeName(stmt.TypeName, schema),
	}

	// Only CHECKs are domain constraints in the model. An unnamed CHECK is
	// named as PostgreSQL names it, in declaration order, against every
	// constraint name in the schema.
	ns := p.namespace(schema)
	for _, c := range stmt.Constraints {
		cn, ok := c.Node.(*pg_query.Node_Constraint)
		if !ok || cn.Constraint.Contype != pg_query.ConstrType_CONSTR_CHECK {
			continue
		}
		check := p.parseConstraintNode(cn.Constraint)
		if check.Name == "" {
			check.Name = parser.DerivePostgresConstraintName(name, parser.Constraint{Type: parser.Check}, ns.taken(parser.Check))
		}
		ns.add(parser.Check, check.Name)
		dt.Constraints = append(dt.Constraints, check)
	}

	p.schema.DomainTypes = append(p.schema.DomainTypes, dt)
	return nil
}

// parseCreateIndex records a standalone CREATE [UNIQUE] INDEX as a Constraint
// on the target table. CREATE INDEX defaults to btree when no USING clause is
// given (PRD §30.7 expects "btree" / "gin" / "gist" / "hash"); WhereClause
// holds the partial-index predicate text. An index with an expression part is
// not an index over columns, so it is skipped, as the introspector skips it;
// its name stays taken.
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

	cols, plain := indexParamColumns(stmt.IndexParams)
	c := parser.Constraint{
		Name:    stmt.Idxname,
		Type:    cType,
		Columns: cols,
		Method:  method,
		Where:   deparseExpr(stmt.WhereClause),
	}
	if c.Name == "" {
		named := parser.Constraint{
			Type:    parser.Index,
			Columns: parser.PostgresIndexColumnNames(indexElemNames(slices.Concat(stmt.IndexParams, stmt.IndexIncludingParams))),
		}
		c.Name = parser.DerivePostgresConstraintName(table.Name, named, p.namespace(table.Schema).taken(parser.Index))
	}
	if !plain {
		table.ExpressionIndexes = append(table.ExpressionIndexes, parser.ExpressionIndex{
			Name:    c.Name,
			Columns: indexReads(table, stmt),
		})
		return nil
	}
	table.Constraints = append(table.Constraints, c)
	parser.CanonicalizeConstraints(table, "postgres")
	return nil
}

// indexReads returns the columns of table that the index stmt reads, in table
// order: its key and INCLUDE columns and those its expressions and WHERE
// predicate reference.
func indexReads(table *parser.Table, stmt *pg_query.IndexStmt) []string {
	read := make(map[string]bool)
	exprs := []string{deparseExpr(stmt.WhereClause)}
	for _, n := range slices.Concat(stmt.IndexParams, stmt.IndexIncludingParams) {
		e := n.GetIndexElem()
		if e == nil {
			continue
		}
		if col := indexElemColumn(e); col != "" {
			read[col] = true
			continue
		}
		exprs = append(exprs, deparseExpr(e.Expr))
	}
	for _, col := range parser.CheckColumns(strings.Join(exprs, ", "), table.Columns, "postgres") {
		read[col] = true
	}
	var out []string
	for _, col := range table.Columns {
		if read[col.Name] {
			out = append(out, col.Name)
		}
	}
	return out
}

// indexElemNames returns the name PostgreSQL gives each index column when it
// names the index: the column's own name, the name an expression would take as
// a select-list column (lower for lower(x)), or "expr".
func indexElemNames(params []*pg_query.Node) []string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		e, ok := p.Node.(*pg_query.Node_IndexElem)
		if !ok || e.IndexElem == nil {
			continue
		}
		name := e.IndexElem.Name
		if name == "" {
			name, _ = figureColname(e.IndexElem.Expr)
		}
		if name == "" {
			name = "expr"
		}
		names = append(names, name)
	}
	return names
}

// figureColname is PostgreSQL's FigureColnameInternal for the expressions an
// index can hold: it returns the name an expression takes as a select-list
// column, and how strongly it holds (2 for a real name, 1 for a fallback such
// as a cast's type name, 0 for none).
func figureColname(node *pg_query.Node) (string, int) {
	if node == nil {
		return "", 0
	}
	switch n := node.Node.(type) {
	case *pg_query.Node_ColumnRef:
		if fields := extractStringList(n.ColumnRef.Fields); len(fields) > 0 {
			return fields[len(fields)-1], 2
		}
	case *pg_query.Node_FuncCall:
		if names := extractStringList(n.FuncCall.Funcname); len(names) > 0 {
			return names[len(names)-1], 2
		}
	case *pg_query.Node_TypeCast:
		name, strength := figureColname(n.TypeCast.Arg)
		if strength <= 1 && n.TypeCast.TypeName != nil {
			if names := extractStringList(n.TypeCast.TypeName.Names); len(names) > 0 {
				return names[len(names)-1], 1
			}
		}
		return name, strength
	case *pg_query.Node_CollateClause:
		return figureColname(n.CollateClause.Arg)
	case *pg_query.Node_CaseExpr:
		name, strength := figureColname(n.CaseExpr.Defresult)
		if strength <= 1 {
			return "case", 1
		}
		return name, strength
	}
	if name := fixedColname(node); name != "" {
		return name, 2
	}
	return "", 0
}

// fixedColname returns the name PostgreSQL gives an expression whose name does
// not depend on its arguments, such as coalesce(…), or "".
func fixedColname(node *pg_query.Node) string {
	switch n := node.Node.(type) {
	case *pg_query.Node_AExpr:
		if n.AExpr.Kind == pg_query.A_Expr_Kind_AEXPR_NULLIF {
			return "nullif"
		}
	case *pg_query.Node_AArrayExpr:
		return "array"
	case *pg_query.Node_RowExpr:
		return "row"
	case *pg_query.Node_CoalesceExpr:
		return "coalesce"
	case *pg_query.Node_MinMaxExpr:
		if n.MinMaxExpr.Op == pg_query.MinMaxOp_IS_LEAST {
			return "least"
		}
		return "greatest"
	}
	return ""
}

// indexParamColumns extracts column names from a CREATE INDEX parameter list.
// It reports false when a part is an expression (lower(x), (a + b)).
func indexParamColumns(params []*pg_query.Node) ([]string, bool) {
	cols := make([]string, 0, len(params))
	for _, p := range params {
		e, ok := p.Node.(*pg_query.Node_IndexElem)
		if !ok || e.IndexElem == nil {
			continue
		}
		name := indexElemColumn(e.IndexElem)
		if name == "" {
			return nil, false
		}
		cols = append(cols, name)
	}
	return cols, true
}

// indexElemColumn returns the column an index part names, or "" for an
// expression. A column written as an expression, (a) or (a COLLATE "C"), is
// stored by PostgreSQL as a plain column.
func indexElemColumn(e *pg_query.IndexElem) string {
	if e.Name != "" {
		return e.Name
	}
	expr := e.Expr
	for expr.GetCollateClause() != nil {
		expr = expr.GetCollateClause().Arg
	}
	fields := expr.GetColumnRef().GetFields()
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1].GetString_().GetSval()
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
		newKey := table.Schema + "." + stmt.Newname
		// PostgreSQL refuses a name a relation in the schema already has,
		// the table's own included.
		if _, exists := p.tableIndex[newKey]; exists {
			return fmt.Errorf("table %s already exists", newKey)
		}
		// PostgreSQL keys a foreign key on the referenced table's OID, so a
		// rename carries every reference along with it.
		parser.RenameTableReferences(p.schema.Tables, "postgres", table.Schema, table.Name, stmt.Newname)
		table.Name = stmt.Newname
		p.moveTableKey(oldKey, newKey)

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
		if err := parser.RenameColumn(table, "postgres", stmt.Subname, stmt.Newname); err != nil {
			return fmt.Errorf("rename column %s.%s: %w", table.Schema+"."+table.Name, stmt.Subname, err)
		}
		// Constraints, indexes and foreign keys name the column by its
		// attribute number, so they follow the rename.
		parser.RenameColumnReferences(p.schema.Tables, "postgres", table.Schema, table.Name, stmt.Subname, stmt.Newname)
		for _, ix := range table.ExpressionIndexes {
			for i, col := range ix.Columns {
				if col == stmt.Subname {
					ix.Columns[i] = stmt.Newname
				}
			}
		}
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
	for i := range table.Columns {
		table.Columns[i].Type = p.retargetTypeName(table.Columns[i].Type, table.Schema, stmt.Newschema)
	}
	table.Schema = stmt.Newschema
	p.moveTableKey(oldKey, newKey)
	return nil
}

// moveTableKey rekeys a renamed or moved table.
func (p *Parser) moveTableKey(oldKey, newKey string) {
	idx := p.tableIndex[oldKey]
	delete(p.tableIndex, oldKey)
	p.tableIndex[newKey] = idx
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
		case pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW:
			// A view is not modeled; only the relation name it takes is.
			schema, name := p.splitQualifiedName(names)
			delete(p.skipped, schema+"."+name)
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
		delete(p.skipped, key) // a partition
		return nil
	}
	if err := parser.CheckTableDropRefs(p.schema.Tables, "postgres", schema, name); err != nil {
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
		p.alterColumnType(table, cmd)
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
	col, constraints := p.parseColumnDef(cd.ColumnDef, table.Schema)
	table.Columns = append(table.Columns, col)
	p.addConstraints(table, constraints)
	return nil
}

// alterDropColumn drops the column, and with it every index and constraint of
// the table that reads it, a partial index's predicate included, as
// PostgreSQL does.
func alterDropColumn(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.MissingOk && parser.ColumnByName(table, cmd.Name) == nil {
		return // DROP COLUMN IF EXISTS — column doesn't exist, skip
	}
	for _, c := range slices.Clone(table.Constraints) {
		if slices.Contains(c.Columns, cmd.Name) ||
			slices.Contains(parser.CheckColumns(c.Where, table.Columns, "postgres"), cmd.Name) {
			removeConstraintFromColumns(table, c.Name)
		}
	}
	var kept []parser.ExpressionIndex
	for _, ix := range table.ExpressionIndexes {
		if !slices.Contains(ix.Columns, cmd.Name) {
			kept = append(kept, ix)
		}
	}
	table.ExpressionIndexes = kept
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
	p.addConstraints(table, []parser.Constraint{p.parseConstraintNode(cn.Constraint)})
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

func (p *Parser) alterColumnType(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	if cmd.Def == nil {
		return
	}
	cd, ok := cmd.Def.Node.(*pg_query.Node_ColumnDef)
	if !ok {
		return
	}
	if col := parser.ColumnByName(table, cmd.Name); col != nil {
		col.Type = p.typeName(cd.ColumnDef.TypeName, table.Schema)
	}
}

// --- Column parsing ---

// parseColumnDef returns the column cd defines in a table of schema, and the
// constraints declared inline on it.
func (p *Parser) parseColumnDef(cd *pg_query.ColumnDef, schema string) (parser.Column, []parser.Constraint) {
	col := parser.Column{
		Name:     cd.Colname,
		Nullable: true,
	}

	col.Type = p.typeName(cd.TypeName, schema)

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

	var constraints []parser.Constraint
	for _, c := range cd.Constraints {
		cn, ok := c.Node.(*pg_query.Node_Constraint)
		if !ok {
			continue
		}
		if constraint, ok := p.applyColumnConstraint(&col, cn.Constraint); ok {
			constraints = append(constraints, constraint)
		}
	}

	return col, constraints
}

// applyColumnConstraint applies an inline column constraint to col. A PRIMARY
// KEY, UNIQUE, REFERENCES or CHECK is also returned as a table constraint over
// the column.
func (p *Parser) applyColumnConstraint(col *parser.Column, c *pg_query.Constraint) (parser.Constraint, bool) {
	switch c.Contype {
	case pg_query.ConstrType_CONSTR_NOTNULL:
		col.Nullable = false

	case pg_query.ConstrType_CONSTR_NULL:
		col.Nullable = true

	case pg_query.ConstrType_CONSTR_PRIMARY, pg_query.ConstrType_CONSTR_UNIQUE, pg_query.ConstrType_CONSTR_CHECK:
		constraint := p.parseConstraintNode(c)
		if constraint.Type != parser.Check {
			constraint.Columns = []string{col.Name}
		}
		return constraint, true

	case pg_query.ConstrType_CONSTR_DEFAULT:
		col.Default = deparseExpr(c.RawExpr)
		if strings.Contains(strings.ToLower(col.Default), "nextval(") {
			col.AutoIncrement = true
		}

	case pg_query.ConstrType_CONSTR_IDENTITY:
		col.AutoIncrement = true
		col.Nullable = false

	case pg_query.ConstrType_CONSTR_GENERATED:
		// pg_query parses the PostgreSQL 17 grammar, which accepts only STORED.
		col.GeneratedExpr = deparseExpr(c.RawExpr)
		col.GeneratedStorage = "STORED"

	case pg_query.ConstrType_CONSTR_FOREIGN:
		if c.Pktable != nil {
			constraint := p.parseConstraintNode(c)
			constraint.Columns = []string{col.Name}
			return constraint, true
		}

	default:
		// EXCLUDE and the DEFERRABLE attributes carry nothing the model stores.
	}
	return parser.Constraint{}, false
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
// post-drop state of the table.
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
		// Another foreign key on the column may remain to give it one.
		parser.CanonicalizeConstraints(table, "postgres")

	case parser.Check, parser.Index:
		// CHECK and INDEX constraints don't affect column flags.
	}
}

// --- Type name helpers ---

// typeName writes the type of a column, composite attribute or domain that
// belongs to schema. An enum, domain or composite type the DDL declared is
// written bare when it lives in schema and schema-qualified when it lives in
// another, as the introspector writes it; an unqualified name means the type
// in the default schema. Every other type is written as typeNameToString
// writes it.
func (p *Parser) typeName(tn *pg_query.TypeName, schema string) string {
	written := typeNameToString(tn)
	if tn == nil {
		return written
	}
	var typSchema, typName string
	switch names := extractStringList(tn.Names); len(names) {
	case 1:
		typSchema, typName = p.defaultSchema, names[0]
	case 2:
		typSchema, typName = names[0], names[1]
	default:
		return written
	}
	if !p.isUserType(typSchema, typName) {
		return written
	}
	name := relativeTypeName(typSchema, typName, schema)
	if len(tn.ArrayBounds) > 0 {
		name += "[]"
	}
	return name
}

// retargetTypeName rewrites a type typeName wrote for schema from as it is
// written for schema to.
func (p *Parser) retargetTypeName(typ, from, to string) string {
	base, isArray := strings.CutSuffix(typ, "[]")
	typSchema, typName, qualified := strings.Cut(base, ".")
	if !qualified {
		typSchema, typName = from, base
	}
	if !p.isUserType(typSchema, typName) {
		return typ
	}
	name := relativeTypeName(typSchema, typName, to)
	if isArray {
		name += "[]"
	}
	return name
}

// relativeTypeName names the type typSchema.typName as seen from schema.
func relativeTypeName(typSchema, typName, schema string) string {
	if typSchema == schema {
		return typName
	}
	return typSchema + "." + typName
}

// isUserType reports whether the DDL declared an enum, domain or composite
// type named schema.name.
func (p *Parser) isUserType(schema, name string) bool {
	return p.findEnumByName(schema, name) != nil ||
		p.findDomainByName(schema, name) != nil ||
		p.findCompositeTypeByName(schema, name) != nil
}

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

// formatTypmods formats type modifier values as the catalog writes them, with
// no space after a comma: (10,2) for numeric(10,2).
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
	return "(" + strings.Join(parts, ",") + ")"
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

// NormalizeExpr rewrites the SQL expression expr in the form the parser writes
// expressions, so text read from the catalog compares equal to parsed DDL.
// Redundant parentheses are dropped: "(qty * 2)" becomes "qty * 2".
func NormalizeExpr(expr string) (string, error) {
	node, err := parseExpr(expr)
	if err != nil {
		return "", err
	}
	return deparseExpr(node), nil
}

// parseExpr parses the SQL expression expr into its parse tree.
func parseExpr(expr string) (*pg_query.Node, error) {
	tree, err := pg_query.Parse("SELECT " + expr)
	if err != nil {
		return nil, fmt.Errorf("parsing expression %q: %w", expr, err)
	}
	if len(tree.Stmts) != 1 {
		return nil, fmt.Errorf("parsing expression %q: not a single expression", expr)
	}
	sel := tree.Stmts[0].Stmt.GetSelectStmt()
	if sel == nil || len(sel.TargetList) != 1 || sel.FromClause != nil || sel.WhereClause != nil {
		return nil, fmt.Errorf("parsing expression %q: not a single expression", expr)
	}
	return sel.TargetList[0].GetResTarget().GetVal(), nil
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

// relationKey returns the qualified "schema.name" key of rv.
func (p *Parser) relationKey(rv *pg_query.RangeVar) string {
	return cmp.Or(rv.GetSchemaname(), p.defaultSchema) + "." + rv.GetRelname()
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
