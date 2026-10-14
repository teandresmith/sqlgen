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
	// views, materialized views, partitions and CREATE TABLE AS / SELECT …
	// INTO tables. Each still takes its name.
	// The value reports whether PostgreSQL can index the relation: false
	// for a view, true for the rest.
	skipped map[string]bool
	// sequences holds the sequences the DDL creates, in creation order.
	sequences []*pgSequence
	// implicitRefs holds the names of the foreign keys the current statement
	// added with no referenced column list; resolveImplicitRefs gives them
	// the referenced table's primary key once the statement is applied.
	implicitRefs []string
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
// a seeded table modifies it in place. The database's views and partitions
// (base.Partitions) are skipped relations, as they are under files.
func (p *Parser) Seed(base *parser.Schema) {
	p.schema = base
	p.tableIndex = make(map[string]int, len(base.Tables))
	for i, t := range base.Tables {
		p.tableIndex[t.Schema+"."+t.Name] = i
	}
	p.skipped = make(map[string]bool, len(base.Views)+len(base.Partitions))
	for _, v := range base.Views {
		p.skipped[v.Schema+"."+v.Name] = v.Materialized
	}
	for _, part := range base.Partitions {
		p.skipped[part.Schema+"."+part.Name] = true
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
	default:
		return p.processAlterStmt(node)
	}
}

// processAlterStmt applies the ALTER DOMAIN, ALTER TYPE … ADD VALUE / RENAME
// VALUE and sequence statements, and records the relations the parser skips.
func (p *Parser) processAlterStmt(node *pg_query.Node) error {
	switch n := node.Node.(type) {
	case *pg_query.Node_AlterEnumStmt:
		return p.parseAlterEnum(n.AlterEnumStmt)
	case *pg_query.Node_AlterDomainStmt:
		return p.parseAlterDomain(n.AlterDomainStmt)
	case *pg_query.Node_CreateSeqStmt:
		return p.parseCreateSequence(n.CreateSeqStmt)
	case *pg_query.Node_AlterSeqStmt:
		return p.parseAlterSequence(n.AlterSeqStmt)
	default:
		p.skipRelation(node)
	}
	return nil
}

// skipRelation records the relation a view, materialized view, CREATE TABLE
// AS or SELECT … INTO creates. The parser does not model it, but it takes its
// name, and only a view cannot be indexed.
func (p *Parser) skipRelation(node *pg_query.Node) {
	switch n := node.Node.(type) {
	case *pg_query.Node_ViewStmt:
		p.skipped[p.relationKey(n.ViewStmt.View)] = false
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE VIEW %s skipped in DDL file — use introspection or define the view in input.views",
				n.ViewStmt.View.Relname))
	case *pg_query.Node_CreateTableAsStmt:
		// CREATE MATERIALIZED VIEW parses as CreateTableAsStmt with a matview
		// object type; plain CREATE TABLE AS is skipped silently.
		p.skipped[p.relationKey(n.CreateTableAsStmt.Into.Rel)] = true
		if n.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
			p.schema.Warnings = append(p.schema.Warnings,
				fmt.Sprintf("CREATE MATERIALIZED VIEW %s skipped in DDL file — use introspection or define the view in input.views",
					n.CreateTableAsStmt.Into.Rel.Relname))
		}
	case *pg_query.Node_SelectStmt:
		// SELECT … INTO creates a table, skipped silently as CREATE TABLE AS
		// is. In a UNION, INTERSECT or EXCEPT the INTO sits on the leftmost
		// SELECT.
		sel := n.SelectStmt
		for sel.Op != pg_query.SetOperation_SETOP_NONE && sel.Larg != nil {
			sel = sel.Larg
		}
		if into := sel.IntoClause; into != nil && into.Rel != nil {
			p.skipped[p.relationKey(into.Rel)] = true
		}
	}
}

func (p *Parser) parseCreateTable(stmt *pg_query.CreateStmt) error {
	if stmt.Partbound != nil {
		// A partition is storage for its partitioned table, which is the
		// entity; the introspector skips partitions too.
		var parent string
		if len(stmt.InhRelations) > 0 {
			parentRel := stmt.InhRelations[0].GetRangeVar()
			parent = parentRel.GetRelname()
			p.schema.Partitions = append(p.schema.Partitions, parser.Partition{
				Schema:       cmp.Or(stmt.Relation.Schemaname, p.defaultSchema),
				Name:         stmt.Relation.Relname,
				ParentSchema: cmp.Or(parentRel.GetSchemaname(), p.defaultSchema),
				ParentName:   parent,
			})
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
			// PostgreSQL refuses a column name the definition list already
			// holds, compared exactly: unquoted names are already folded, so
			// a and A clash while a and "A" do not.
			name := n.ColumnDef.Colname
			if slices.ContainsFunc(table.Columns, func(c parser.Column) bool { return parser.SameColumnName("postgres", c.Name, name) }) {
				return fmt.Errorf("create table %s: column %s specified more than once", key, name)
			}
			col, colConstraints := p.parseColumnDef(n.ColumnDef, schema)
			table.Columns = append(table.Columns, col)
			constraints = append(constraints, colConstraints...)
		case *pg_query.Node_Constraint:
			constraints = append(constraints, p.parseConstraintNode(n.Constraint))
		}
	}

	p.tableIndex[key] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)
	created := &p.schema.Tables[len(p.schema.Tables)-1]
	for _, elt := range stmt.TableElts {
		if cd := elt.GetColumnDef(); cd != nil {
			p.addColumnSequence(created, cd)
		}
	}
	p.addConstraints(created, mergeDuplicateIndexes(constraints))

	return p.resolveImplicitRefs(created)
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
	start := len(table.Constraints)
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
	for _, c := range table.Constraints[start:] {
		if c.Type == parser.ForeignKey && len(c.ReferenceColumns) == 0 {
			p.implicitRefs = append(p.implicitRefs, c.Name)
		}
	}
}

// resolveImplicitRefs gives each foreign key this statement added to table
// with no referenced column list (`REFERENCES t`) t's primary key as the
// statement leaves it, as PostgreSQL does. It is an error when t is not
// defined, or is a modelled table with no primary key.
func (p *Parser) resolveImplicitRefs(table *parser.Table) error {
	names := p.implicitRefs
	p.implicitRefs = nil
	if len(names) == 0 {
		return nil
	}
	isPK := func(k parser.Constraint) bool { return k.Type == parser.PrimaryKey }
	for _, name := range names {
		i := slices.IndexFunc(table.Constraints, func(k parser.Constraint) bool {
			return k.Type == parser.ForeignKey && k.Name == name
		})
		if i < 0 {
			continue // dropped later in the same statement
		}
		c := &table.Constraints[i]
		target := p.findTableByNames([]string{c.ReferenceSchema, c.ReferenceTable})
		if _, skipped := p.skipped[c.ReferenceSchema+"."+c.ReferenceTable]; target == nil && skipped {
			// A partition has its partitioned table's primary key. The model
			// keeps no keys for any other skipped relation, or for a partition
			// of a table with none, so such a reference stays as written.
			target = p.partitionedTable(c.ReferenceSchema, c.ReferenceTable)
			if target == nil || !slices.ContainsFunc(target.Constraints, isPK) {
				continue
			}
		}
		if target == nil {
			return fmt.Errorf("foreign key %s on %s.%s: referenced table %s.%s does not exist",
				c.Name, table.Schema, table.Name, c.ReferenceSchema, c.ReferenceTable)
		}
		pk := slices.IndexFunc(target.Constraints, isPK)
		if pk < 0 {
			return fmt.Errorf("foreign key %s on %s.%s: there is no primary key for referenced table %s.%s",
				c.Name, table.Schema, table.Name, c.ReferenceSchema, c.ReferenceTable)
		}
		c.ReferenceColumns = slices.Clone(target.Constraints[pk].Columns)
	}
	// Sets FKReference from the keys just resolved.
	parser.CanonicalizeConstraints(table, "postgres")
	return nil
}

// partitionedTable returns the modelled table that schema.name is a partition
// of, at any depth, or nil when it is no partition of one.
func (p *Parser) partitionedTable(schema, name string) *parser.Table {
	for {
		i := slices.IndexFunc(p.schema.Partitions, func(part parser.Partition) bool {
			return part.Schema == schema && part.Name == name
		})
		if i < 0 {
			return nil
		}
		schema, name = p.schema.Partitions[i].ParentSchema, p.schema.Partitions[i].ParentName
		if t := p.findTableByNames([]string{schema, name}); t != nil {
			return t
		}
	}
}

// pgNamespace holds the names in use in one schema. An index, or the index
// behind a PRIMARY KEY or UNIQUE, needs a name no table, view, composite
// type, sequence or index in the schema has. A constraint needs a name no
// constraint in the schema has, on any table or domain.
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
	for _, s := range p.sequences {
		if s.schema == schema {
			ns.relations[s.name] = true
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

// parseAlterDomain applies ALTER DOMAIN … ADD CHECK, named as CREATE DOMAIN
// names one, and DROP CONSTRAINT [IF EXISTS]. Defaults, NOT NULL and VALIDATE
// change nothing the model holds.
func (p *Parser) parseAlterDomain(stmt *pg_query.AlterDomainStmt) error {
	schema, name := p.splitQualifiedName(extractStringList(stmt.TypeName))
	dt := p.findDomainByName(schema, name)
	if dt == nil {
		return fmt.Errorf("alter domain: unknown domain %s.%s", schema, name)
	}
	switch stmt.Subtype {
	case "C":
		c := stmt.Def.GetConstraint()
		if c == nil || c.Contype != pg_query.ConstrType_CONSTR_CHECK {
			return nil
		}
		check := p.parseConstraintNode(c)
		if check.Name == "" {
			check.Name = parser.DerivePostgresConstraintName(name, parser.Constraint{Type: parser.Check}, p.namespace(schema).taken(parser.Check))
		}
		dt.Constraints = append(dt.Constraints, check)
	case "X":
		dt.Constraints = slices.DeleteFunc(dt.Constraints, func(c parser.Constraint) bool { return c.Name == stmt.Name })
	}
	return nil
}

// parseCreateIndex records a standalone CREATE [UNIQUE] INDEX as a Constraint
// on the target table. CREATE INDEX defaults to btree when no USING clause is
// given (PRD §30.7 expects "btree" / "gin" / "gist" / "hash"); WhereClause
// holds the partial-index predicate text. An index with an expression part is
// not an index over columns, so it is skipped, as the introspector skips it;
// its name stays taken. An index on a materialized view, a partition or a
// CREATE TABLE AS table is skipped with the relation. An index on any other
// relation no earlier statement created, a view included, is an error, as it
// is in PostgreSQL.
func (p *Parser) parseCreateIndex(stmt *pg_query.IndexStmt) error {
	if stmt == nil || stmt.Relation == nil {
		return nil
	}
	table, err := p.lookupTable(stmt.Relation)
	if err != nil {
		if p.skipped[p.relationKey(stmt.Relation)] {
			return nil
		}
		if stmt.Idxname == "" {
			return fmt.Errorf("create index: %w", err)
		}
		return fmt.Errorf("create index %s: %w", stmt.Idxname, err)
	}

	method := stmt.AccessMethod
	if method == "" {
		method = "btree"
	}

	cType := parser.Index
	if stmt.Unique {
		cType = parser.Unique
	}

	cols, plain := p.indexParamColumns(table, stmt.IndexParams)
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
			Columns: p.indexReads(table, stmt),
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
func (p *Parser) indexReads(table *parser.Table, stmt *pg_query.IndexStmt) []string {
	read := make(map[string]bool)
	exprs := []string{deparseExpr(stmt.WhereClause)}
	for _, n := range slices.Concat(stmt.IndexParams, stmt.IndexIncludingParams) {
		e := n.GetIndexElem()
		if e == nil {
			continue
		}
		if col := p.indexElemColumn(table, e); col != "" {
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

// indexParamColumns extracts the column names from the CREATE INDEX parameter
// list of an index on table. It reports false when a part is an expression
// (lower(x), (a + b)).
func (p *Parser) indexParamColumns(table *parser.Table, params []*pg_query.Node) ([]string, bool) {
	cols := make([]string, 0, len(params))
	for _, n := range params {
		e, ok := n.Node.(*pg_query.Node_IndexElem)
		if !ok || e.IndexElem == nil {
			continue
		}
		name := p.indexElemColumn(table, e.IndexElem)
		if name == "" {
			return nil, false
		}
		cols = append(cols, name)
	}
	return cols, true
}

// indexElemColumn returns the column of table an index part names, or "" for
// an expression. A column written as an expression is stored by PostgreSQL as
// a plain column: (a), (a COLLATE "C"), and (a::text) when a is a text column,
// since PostgreSQL drops a cast to the column's own type and typmod. A cast to
// any other type, a domain's base type included, stays an expression.
func (p *Parser) indexElemColumn(table *parser.Table, e *pg_query.IndexElem) string {
	if e.Name != "" {
		return e.Name
	}
	expr := e.Expr
	var casts []*pg_query.TypeName
	for {
		if c := expr.GetCollateClause(); c != nil {
			expr = c.Arg
			continue
		}
		if c := expr.GetTypeCast(); c != nil {
			casts = append(casts, c.TypeName)
			expr = c.Arg
			continue
		}
		break
	}
	fields := expr.GetColumnRef().GetFields()
	if len(fields) == 0 {
		return ""
	}
	name := fields[len(fields)-1].GetString_().GetSval()
	if len(casts) == 0 {
		return name
	}
	col := parser.ColumnByName(table, name)
	if col == nil {
		return ""
	}
	for _, tn := range casts {
		if p.typeName(tn, table.Schema) != col.Type {
			return ""
		}
	}
	return name
}

func (p *Parser) parseRename(stmt *pg_query.RenameStmt) error {
	switch stmt.RenameType { //nolint:exhaustive // only renames of what the model or the namespace holds are relevant
	case pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW:
		if stmt.Relation != nil {
			p.moveSkipped(p.relationKey(stmt.Relation), cmp.Or(stmt.Relation.Schemaname, p.defaultSchema)+"."+stmt.Newname)
		}
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
		// PostgreSQL keys a foreign key and a view's table on the referenced
		// table's OID, so a rename carries every reference along with it.
		parser.RenameViewTableReferences(p.schema, table, table.Schema, stmt.Newname)
		parser.RenameTableReferences(p.schema.Tables, "postgres", table.Schema, table.Name, stmt.Newname)
		table.Name = stmt.Newname
		p.moveTableKey(oldKey, newKey)
		p.moveSequences(oldKey, newKey, table.Schema)

	case pg_query.ObjectType_OBJECT_COLUMN:
		return p.renameColumn(stmt)

	case pg_query.ObjectType_OBJECT_TABCONSTRAINT:
		return p.renameTableConstraint(stmt)

	case pg_query.ObjectType_OBJECT_INDEX:
		p.renameIndex(stmt.Relation, stmt.Newname)

	case pg_query.ObjectType_OBJECT_DOMCONSTRAINT:
		return p.renameDomainConstraint(stmt)

	case pg_query.ObjectType_OBJECT_SEQUENCE:
		p.renameSequence(stmt.Relation, stmt.Newname)

	default:
		return p.renameTypeObject(stmt)
	}
	return nil
}

// renameColumn applies ALTER TABLE … RENAME COLUMN.
func (p *Parser) renameColumn(stmt *pg_query.RenameStmt) error {
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
	// Views, constraints, indexes and foreign keys name the column by its
	// attribute number, so they follow the rename.
	parser.RenameViewColumnReferences(p.schema, table, "postgres", stmt.Subname, stmt.Newname)
	if err := parser.RenameColumn(table, "postgres", stmt.Subname, stmt.Newname); err != nil {
		return fmt.Errorf("rename column %s.%s: %w", table.Schema+"."+table.Name, stmt.Subname, err)
	}
	parser.RenameColumnReferences(p.schema.Tables, "postgres", table.Schema, table.Name, stmt.Subname, stmt.Newname)
	for _, ix := range table.ExpressionIndexes {
		for i, col := range ix.Columns {
			if col == stmt.Subname {
				ix.Columns[i] = stmt.Newname
			}
		}
	}
	p.renameSequenceColumn(table.Schema+"."+table.Name, stmt.Subname, stmt.Newname)
	return nil
}

// renameTypeObject applies the renames of a type, a domain or a composite
// type's attribute.
func (p *Parser) renameTypeObject(stmt *pg_query.RenameStmt) error {
	switch stmt.RenameType { //nolint:exhaustive // parseRename handles the rest
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		return p.renameType(stmt)
	case pg_query.ObjectType_OBJECT_ATTRIBUTE:
		return p.renameAttribute(stmt)
	}
	return nil
}

// renameDomainConstraint applies ALTER DOMAIN … RENAME CONSTRAINT.
func (p *Parser) renameDomainConstraint(stmt *pg_query.RenameStmt) error {
	schema, name := p.splitQualifiedName(extractObjectNames(stmt.Object))
	dt := p.findDomainByName(schema, name)
	if dt == nil {
		return fmt.Errorf("alter domain: unknown domain %s.%s", schema, name)
	}
	for i := range dt.Constraints {
		if dt.Constraints[i].Name == stmt.Subname {
			dt.Constraints[i].Name = stmt.Newname
		}
	}
	return nil
}

// renameTableConstraint applies ALTER TABLE … RENAME CONSTRAINT. The index
// behind a PRIMARY KEY or UNIQUE takes the new name too, so the model's one
// entry is renamed. A name the model does not hold is skipped.
func (p *Parser) renameTableConstraint(stmt *pg_query.RenameStmt) error {
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
	for i := range table.Constraints {
		if c := &table.Constraints[i]; c.Name == stmt.Subname && c.Type != parser.Index {
			c.Name = stmt.Newname
			parser.CanonicalizeConstraints(table, "postgres")
			return nil
		}
	}
	return nil
}

// renameIndex applies ALTER INDEX … RENAME TO. Renaming the index behind a
// PRIMARY KEY or UNIQUE renames the constraint too.
func (p *Parser) renameIndex(rv *pg_query.RangeVar, newName string) {
	if rv == nil {
		return
	}
	schema := cmp.Or(rv.Schemaname, p.defaultSchema)
	for ti := range p.schema.Tables {
		table := &p.schema.Tables[ti]
		if table.Schema != schema {
			continue
		}
		for i := range table.Constraints {
			c := &table.Constraints[i]
			if c.Name == rv.Relname && (c.Type == parser.Index || c.Type == parser.Unique || c.Type == parser.PrimaryKey) {
				c.Name = newName
				parser.CanonicalizeConstraints(table, "postgres")
				return
			}
		}
		for i := range table.ExpressionIndexes {
			if ix := &table.ExpressionIndexes[i]; ix.Name == rv.Relname {
				ix.Name = newName
				return
			}
		}
	}
}

// parseAlterObjectSchema applies ALTER TABLE … SET SCHEMA, which pg_query
// parses as an AlterObjectSchemaStmt, not an AlterTableStmt. A moved table is
// rekeyed and a moved view or materialized view keeps its name reserved;
// moving any other object is ignored.
func (p *Parser) parseAlterObjectSchema(stmt *pg_query.AlterObjectSchemaStmt) error {
	if stmt.Relation == nil {
		return nil
	}
	if stmt.ObjectType == pg_query.ObjectType_OBJECT_VIEW || stmt.ObjectType == pg_query.ObjectType_OBJECT_MATVIEW {
		p.moveSkipped(p.relationKey(stmt.Relation), stmt.Newschema+"."+stmt.Relation.Relname)
		return nil
	}
	if stmt.ObjectType != pg_query.ObjectType_OBJECT_TABLE {
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
	// PostgreSQL keys a foreign key and a view's table on the referenced
	// table's OID, so a move carries every reference along with it.
	parser.RenameViewTableReferences(p.schema, table, stmt.Newschema, table.Name)
	parser.MoveTableReferences(p.schema.Tables, table.Name, table.Schema, stmt.Newschema)
	for i := range table.Columns {
		table.Columns[i].Type = p.retargetTypeName(table.Columns[i].Type, table.Schema, stmt.Newschema)
	}
	table.Schema = stmt.Newschema
	p.moveTableKey(oldKey, newKey)
	p.moveSequences(oldKey, newKey, stmt.Newschema)
	return nil
}

// moveTableKey rekeys a renamed or moved table.
func (p *Parser) moveTableKey(oldKey, newKey string) {
	idx := p.tableIndex[oldKey]
	delete(p.tableIndex, oldKey)
	p.tableIndex[newKey] = idx
}

func (p *Parser) parseDrop(stmt *pg_query.DropStmt) error {
	// PostgreSQL drops the objects of one DROP TABLE, DROP TYPE or DROP DOMAIN
	// together.
	if stmt.RemoveType == pg_query.ObjectType_OBJECT_TABLE {
		return p.dropTables(stmt)
	}
	if stmt.RemoveType == pg_query.ObjectType_OBJECT_TYPE {
		return p.dropTypes(p.dropTypeRefs(stmt), "type")
	}
	if stmt.RemoveType == pg_query.ObjectType_OBJECT_DOMAIN {
		return p.dropTypes(p.dropTypeRefs(stmt), "domain")
	}
	for _, obj := range stmt.Objects {
		names := extractDropObjectNames(obj)
		switch stmt.RemoveType {
		case pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW:
			// A view is not modeled; only the relation name it takes is.
			schema, name := p.splitQualifiedName(names)
			delete(p.skipped, schema+"."+name)
		case pg_query.ObjectType_OBJECT_INDEX:
			schema, name := p.splitQualifiedName(names)
			p.dropIndex(schema, name)
		case pg_query.ObjectType_OBJECT_SEQUENCE:
			schema, name := p.splitQualifiedName(names)
			p.dropSequence(schema, name, stmt.Behavior == pg_query.DropBehavior_DROP_CASCADE)
		default:
			// Other object types are not handled.
		}
	}
	return nil
}

// dropTypeRefs names the objects of a DROP TYPE or DROP DOMAIN statement.
func (p *Parser) dropTypeRefs(stmt *pg_query.DropStmt) []parser.TypeRef {
	refs := make([]parser.TypeRef, 0, len(stmt.Objects))
	for _, obj := range stmt.Objects {
		schema, name := p.splitQualifiedName(extractDropObjectNames(obj))
		refs = append(refs, parser.TypeRef{Schema: schema, Name: name})
	}
	return refs
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

// dropTables drops the tables of one DROP TABLE statement. PostgreSQL drops
// them together, so a foreign key from one to another does not block the
// drop. A relation the schema does not model is skipped. A foreign key from
// another table blocks the drop, unless the statement says CASCADE, which
// drops that key instead.
func (p *Parser) dropTables(stmt *pg_query.DropStmt) error {
	refs := make([]parser.TableRef, 0, len(stmt.Objects))
	for _, obj := range stmt.Objects {
		schema, name := p.splitQualifiedName(extractDropObjectNames(obj))
		p.dropPartitions(schema, name)
		if _, ok := p.tableIndex[schema+"."+name]; !ok {
			delete(p.skipped, schema+"."+name) // a skipped relation
			continue
		}
		refs = append(refs, parser.TableRef{Schema: schema, Name: name})
	}
	if stmt.Behavior == pg_query.DropBehavior_DROP_CASCADE {
		p.dropReferencingKeys(refs)
	} else if err := parser.CheckTableDropRefs(p.schema.Tables, "postgres", refs); err != nil {
		return fmt.Errorf("drop table: %w", err)
	}
	for _, ref := range refs {
		key := ref.Schema + "." + ref.Name
		if _, ok := p.tableIndex[key]; !ok {
			continue // named twice in one DROP TABLE
		}
		p.removeTable(key)
		p.dropOwnedSequences(key, "")
	}
	return nil
}

// dropReferencingKeys drops every foreign key that references a table in
// drop, as DROP TABLE … CASCADE does. The referencing columns keep their
// unique constraints and indexes, and a column's FKReference falls back to a
// remaining single-column key, if it has one.
func (p *Parser) dropReferencingKeys(drop []parser.TableRef) {
	dropped := func(schema, name string) bool {
		return slices.Contains(drop, parser.TableRef{Schema: schema, Name: name})
	}
	for i := range p.schema.Tables {
		t := &p.schema.Tables[i]
		n := len(t.Constraints)
		t.Constraints = slices.DeleteFunc(t.Constraints, func(c parser.Constraint) bool {
			return c.Type == parser.ForeignKey && dropped(c.ReferenceSchema, c.ReferenceTable)
		})
		if len(t.Constraints) == n {
			continue
		}
		// Canonicalization would restore a key from a reference left in place.
		for j := range t.Columns {
			if ref := t.Columns[j].FKReference; ref != nil && dropped(ref.Schema, ref.Table) {
				t.Columns[j].FKReference = nil
			}
		}
		parser.CanonicalizeConstraints(t, "postgres")
	}
}

// removeTable removes the table key from the model.
func (p *Parser) removeTable(key string) {
	idx := p.tableIndex[key]
	p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
	delete(p.tableIndex, key)
	// Shift indices for tables after the removed one.
	for k, v := range p.tableIndex {
		if v > idx {
			p.tableIndex[k] = v - 1
		}
	}
}

// skippedTable reports whether names is a relation the parser skips that
// PostgreSQL can index: a partition, materialized view or CREATE TABLE AS /
// SELECT … INTO table. ALTER TABLE and COMMENT ON of one are skipped with it,
// as CREATE INDEX on one is.
func (p *Parser) skippedTable(names []string) bool {
	schema, name := p.splitQualifiedName(names)
	return p.skipped[schema+"."+name]
}

// moveSkipped rekeys a renamed or moved view or materialized view, so a later
// CREATE INDEX finds it under its new name.
func (p *Parser) moveSkipped(oldKey, newKey string) {
	if indexable, ok := p.skipped[oldKey]; ok {
		delete(p.skipped, oldKey)
		p.skipped[newKey] = indexable
	}
}

// dropPartitions forgets the partition schema.name, if it is one, and every
// partition of it, as DROP TABLE drops a table's partitions with it.
func (p *Parser) dropPartitions(schema, name string) {
	dropped := map[string]bool{schema + "." + name: true}
	for n := -1; n != len(p.schema.Partitions); {
		n = len(p.schema.Partitions)
		p.schema.Partitions = slices.DeleteFunc(p.schema.Partitions, func(part parser.Partition) bool {
			key := part.Schema + "." + part.Name
			if dropped[key] || dropped[part.ParentSchema+"."+part.ParentName] {
				dropped[key] = true
				delete(p.skipped, key)
				return true
			}
			return false
		})
	}
	if len(p.schema.Partitions) == 0 {
		p.schema.Partitions = nil // as the introspector leaves it
	}
}

// dropIndex applies DROP INDEX schema.name. A CREATE INDEX index leaves its
// table, and the column flags it set are re-derived. An expression index
// releases its name. An index the parser never recorded, such as one on a
// materialized view or a partition, leaves the schema as it is.
func (p *Parser) dropIndex(schema, name string) {
	for i := range p.schema.Tables {
		table := &p.schema.Tables[i]
		if table.Schema != schema {
			continue
		}
		if idx := slices.IndexFunc(table.Constraints, func(c parser.Constraint) bool {
			return (c.Type == parser.Index || c.Type == parser.Unique) && c.Name == name
		}); idx >= 0 {
			c := table.Constraints[idx]
			table.Constraints = slices.Delete(table.Constraints, idx, idx+1)
			if c.Type == parser.Unique && len(c.Columns) == 1 {
				parser.RecomputeColumnUnique(table, c.Columns[0])
			}
			return
		}
		if idx := slices.IndexFunc(table.ExpressionIndexes, func(ix parser.ExpressionIndex) bool {
			return ix.Name == name
		}); idx >= 0 {
			table.ExpressionIndexes = slices.Delete(table.ExpressionIndexes, idx, idx+1)
			return
		}
	}
}

// dropTypes drops the objects of one DROP TYPE (kind "type") or DROP DOMAIN
// (kind "domain") statement. PostgreSQL drops them together, so a reference
// from one to another does not block the drop. DROP TYPE drops a domain too;
// DROP DOMAIN refuses an enum or composite type.
func (p *Parser) dropTypes(refs []parser.TypeRef, kind string) error {
	if kind == "domain" {
		for _, ref := range refs {
			if p.findEnumByName(ref.Schema, ref.Name) != nil || p.findCompositeTypeByName(ref.Schema, ref.Name) != nil {
				return fmt.Errorf("drop domain: %s.%s is not a domain", ref.Schema, ref.Name)
			}
		}
	}
	if err := parser.CheckTypeDropRefs(p.schema, refs); err != nil {
		return fmt.Errorf("drop %s: %w", kind, err)
	}
	for _, ref := range refs {
		p.schema.DomainTypes = slices.DeleteFunc(p.schema.DomainTypes, func(dt parser.DomainType) bool {
			return dt.Schema == ref.Schema && dt.Name == ref.Name
		})
		p.schema.Enums = slices.DeleteFunc(p.schema.Enums, func(e parser.Enum) bool {
			return e.Schema == ref.Schema && e.Name == ref.Name
		})
		p.schema.CompositeTypes = slices.DeleteFunc(p.schema.CompositeTypes, func(ct parser.CompositeType) bool {
			return ct.Schema == ref.Schema && ct.Name == ref.Name
		})
	}
	return nil
}

func (p *Parser) parseComment(stmt *pg_query.CommentStmt) error {
	switch stmt.Objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		names := extractObjectNames(stmt.Object)
		table := p.findTableByNames(names)
		if table == nil {
			if p.skippedTable(names) {
				return nil
			}
			return fmt.Errorf("comment on unknown table %s", strings.Join(names, "."))
		}
		table.Comment = stmt.Comment

	case pg_query.ObjectType_OBJECT_COLUMN:
		return p.commentOnColumn(stmt)

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

// commentOnColumn applies COMMENT ON COLUMN.
func (p *Parser) commentOnColumn(stmt *pg_query.CommentStmt) error {
	names := extractObjectNames(stmt.Object)
	if len(names) < 2 {
		return fmt.Errorf("comment on column: invalid name %s", strings.Join(names, "."))
	}
	colName := names[len(names)-1]
	tableNames := names[:len(names)-1]
	table := p.findTableByNames(tableNames)
	if table == nil {
		if p.skippedTable(tableNames) {
			return nil
		}
		return fmt.Errorf("comment on column of unknown table %s", strings.Join(tableNames, "."))
	}
	for i := range table.Columns {
		if table.Columns[i].Name == colName {
			table.Columns[i].Comment = stmt.Comment
			return nil
		}
	}
	return fmt.Errorf("comment on unknown column %s.%s", strings.Join(tableNames, "."), colName)
}

func (p *Parser) parseAlterTable(stmt *pg_query.AlterTableStmt) error {
	if stmt.Objtype == pg_query.ObjectType_OBJECT_TYPE {
		return p.alterCompositeType(stmt)
	}
	table, err := p.lookupTable(stmt.Relation)
	if err != nil {
		if p.skipped[p.relationKey(stmt.Relation)] {
			p.alterSkippedTable(stmt)
			return nil
		}
		if stmt.MissingOk {
			return nil // ALTER TABLE IF EXISTS — table doesn't exist, skip
		}
		return err
	}

	var drops, rest []*pg_query.AlterTableCmd
	for _, cmd := range stmt.Cmds {
		atCmd, ok := cmd.Node.(*pg_query.Node_AlterTableCmd)
		if !ok {
			continue
		}
		if atCmd.AlterTableCmd.Subtype == pg_query.AlterTableType_AT_DropColumn {
			drops = append(drops, atCmd.AlterTableCmd)
		} else {
			rest = append(rest, atCmd.AlterTableCmd)
		}
	}
	// PostgreSQL drops columns before it adds any, wherever DROP COLUMN sits
	// among the clauses, so `ADD COLUMN c, DROP COLUMN c` replaces c.
	for _, cmd := range slices.Concat(drops, rest) {
		if err := p.applyAlterCmd(table, cmd); err != nil {
			return err
		}
	}

	return p.resolveImplicitRefs(table)
}

// alterSkippedTable applies ALTER TABLE to a relation the parser skips. Only
// ATTACH and DETACH PARTITION change the model, when the skipped relation is
// itself a partitioned partition.
func (p *Parser) alterSkippedTable(stmt *pg_query.AlterTableStmt) {
	parent := &parser.Table{Schema: cmp.Or(stmt.Relation.Schemaname, p.defaultSchema), Name: stmt.Relation.Relname}
	for _, cmd := range stmt.Cmds {
		switch c := cmd.GetAlterTableCmd(); c.GetSubtype() { //nolint:exhaustive // the other subtypes change only the skipped relation
		case pg_query.AlterTableType_AT_AttachPartition:
			p.attachPartition(parent, c.Def.GetPartitionCmd())
		case pg_query.AlterTableType_AT_DetachPartition:
			p.detachPartition(parent, c.Def.GetPartitionCmd())
		}
	}
}

func (p *Parser) applyAlterCmd(table *parser.Table, cmd *pg_query.AlterTableCmd) error {
	switch cmd.Subtype {
	case pg_query.AlterTableType_AT_AddColumn:
		return p.alterAddColumn(table, cmd)
	case pg_query.AlterTableType_AT_DropColumn:
		alterDropColumn(table, cmd)
		p.dropOwnedSequences(table.Schema+"."+table.Name, cmd.Name)
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
		p.alterColumnDefault(table, cmd)
	case pg_query.AlterTableType_AT_AlterColumnType:
		p.alterColumnType(table, cmd)
	default:
		p.applyAlterStateCmd(table, cmd)
	}

	return nil
}

// applyAlterStateCmd applies the identity, generation-expression and
// partition subtypes of ALTER TABLE.
func (p *Parser) applyAlterStateCmd(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	switch cmd.Subtype { //nolint:exhaustive // applyAlterCmd handles the rest
	case pg_query.AlterTableType_AT_AddIdentity:
		p.alterAddIdentity(table, cmd)
	case pg_query.AlterTableType_AT_DropIdentity:
		p.alterDropIdentity(table, cmd)
	case pg_query.AlterTableType_AT_DropExpression:
		// DROP EXPRESSION [IF EXISTS] leaves a plain column holding the values.
		if col := parser.ColumnByName(table, cmd.Name); col != nil {
			col.GeneratedExpr, col.GeneratedStorage = "", ""
		}
	case pg_query.AlterTableType_AT_SetExpression:
		if col := parser.ColumnByName(table, cmd.Name); col != nil {
			col.GeneratedExpr = deparseExpr(cmd.Def)
		}
	case pg_query.AlterTableType_AT_AttachPartition:
		p.attachPartition(table, cmd.Def.GetPartitionCmd())
	case pg_query.AlterTableType_AT_DetachPartition:
		p.detachPartition(table, cmd.Def.GetPartitionCmd())
	default:
		// Other ALTER TABLE subtypes are not supported yet.
	}
}

// alterAddColumn adds the column cmd defines to table. A name the table
// already has is an error, as it is in PostgreSQL, and a no-op under ADD
// COLUMN IF NOT EXISTS.
func (p *Parser) alterAddColumn(table *parser.Table, cmd *pg_query.AlterTableCmd) error {
	if cmd.Def == nil {
		return nil
	}
	cd, ok := cmd.Def.Node.(*pg_query.Node_ColumnDef)
	if !ok {
		return nil
	}
	name := cd.ColumnDef.Colname
	if slices.ContainsFunc(table.Columns, func(c parser.Column) bool { return parser.SameColumnName("postgres", c.Name, name) }) {
		if cmd.MissingOk {
			return nil
		}
		return fmt.Errorf("add column %s to %s.%s: column already exists", name, table.Schema, table.Name)
	}
	col, constraints := p.parseColumnDef(cd.ColumnDef, table.Schema)
	table.Columns = append(table.Columns, col)
	p.addColumnSequence(table, cd.ColumnDef)
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

// attachPartition applies ATTACH PARTITION: the attached table becomes a
// partition of parent, which the model lists as a partition and does not hold
// as a table (PRD §5.2). A foreign key that references it keeps referencing
// it, as in PostgreSQL.
func (p *Parser) attachPartition(parent *parser.Table, cmd *pg_query.PartitionCmd) {
	rv := cmd.GetName()
	if rv == nil {
		return
	}
	key := cmp.Or(rv.Schemaname, p.defaultSchema) + "." + rv.Relname
	if _, ok := p.tableIndex[key]; ok {
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("ALTER TABLE %s ATTACH PARTITION %s: the partition is dropped from the model — the partitioned table is the entity",
				parent.Name, rv.Relname))
		p.removeTable(key)
	} else if !p.skipped[key] {
		return
	}
	p.skipped[key] = true
	p.schema.Partitions = append(p.schema.Partitions, parser.Partition{
		Schema:       cmp.Or(rv.Schemaname, p.defaultSchema),
		Name:         rv.Relname,
		ParentSchema: parent.Schema,
		ParentName:   parent.Name,
	})
}

// detachPartition applies DETACH PARTITION. The detached table keeps the
// partition's indexes and constraints, under names PostgreSQL derives as it
// clones them, which the parser does not reproduce, so it stays a skipped
// relation that is no longer a partition of parent.
func (p *Parser) detachPartition(parent *parser.Table, cmd *pg_query.PartitionCmd) {
	rv := cmd.GetName()
	schema := cmp.Or(rv.GetSchemaname(), p.defaultSchema)
	p.schema.Partitions = slices.DeleteFunc(p.schema.Partitions, func(part parser.Partition) bool {
		return part.Schema == schema && part.Name == rv.GetRelname() &&
			part.ParentSchema == parent.Schema && part.ParentName == parent.Name
	})
	if len(p.schema.Partitions) == 0 {
		p.schema.Partitions = nil // as the introspector leaves it
	}
	p.schema.Warnings = append(p.schema.Warnings,
		fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s skipped in DDL file — the detached table is not modelled under files; use introspection",
			parent.Name, rv.GetRelname()))
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
// writes it, less a default-schema qualifier: the introspector writes such a
// type with format_type, which leaves out the schema of a type on the
// connection's search_path, and that search_path is assumed to resolve the
// default schema, as an unqualified name in the DDL does.
func (p *Parser) typeName(tn *pg_query.TypeName, schema string) string {
	written := typeNameToString(tn)
	if tn == nil {
		return written
	}
	var typSchema, typName string
	qualified := false
	switch names := extractStringList(tn.Names); len(names) {
	case 1:
		typSchema, typName = p.defaultSchema, names[0]
	case 2:
		typSchema, typName, qualified = names[0], names[1], true
	default:
		return written
	}
	if !p.isUserType(typSchema, typName) {
		if qualified && typSchema == p.defaultSchema {
			return strings.TrimPrefix(written, typSchema+".")
		}
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
	ref, isArray := parser.ResolveTypeRef(typ, from)
	if !p.isUserType(ref.Schema, ref.Name) {
		return typ
	}
	name := relativeTypeName(ref.Schema, ref.Name, to)
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
