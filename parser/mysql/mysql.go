package mysql

import (
	"fmt"
	"slices"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"

	"github.com/teandresmith/sqlgen/parser"
)

// Parser implements the parser.Parser interface for MySQL DDL.
// It uses vitess/sqlparser (pure Go) to parse MySQL SQL statements.
type Parser struct {
	schema     *parser.Schema
	tableIndex map[string]int // table name → index in schema.Tables
	vparser    *sqlparser.Parser
}

// New creates a new MySQL parser.
func New() (*Parser, error) {
	vp, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return nil, fmt.Errorf("creating mysql parser: %w", err)
	}
	return &Parser{
		schema:     &parser.Schema{},
		tableIndex: make(map[string]int),
		vparser:    vp,
	}, nil
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

// Parse parses MySQL DDL from sql. It can be called multiple times with
// different files; results accumulate.
func (p *Parser) Parse(filename string, sql []byte) error {
	pieces, err := p.vparser.SplitStatementToPieces(string(sql))
	if err != nil {
		return fmt.Errorf("splitting %s: %w", filename, err)
	}

	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		stmt, err := p.vparser.ParseStrictDDL(piece)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", filename, err)
		}
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

func (p *Parser) processStmt(stmt sqlparser.Statement) error {
	switch s := stmt.(type) {
	case *sqlparser.CreateTable:
		return p.parseCreateTable(s)
	case *sqlparser.AlterTable:
		return p.parseAlterTable(s)
	case *sqlparser.DropTable:
		return p.parseDropTable(s)
	case *sqlparser.RenameTable:
		return p.parseRenameTable(s)
	case *sqlparser.CreateView:
		p.schema.Warnings = append(p.schema.Warnings,
			fmt.Sprintf("CREATE VIEW %s skipped in DDL file — use introspection or define the view in input.views",
				s.ViewName.Name.String()))
	case *sqlparser.DropView:
		// DROP VIEW is skipped gracefully — views are not tracked in DDL parsing.
	}
	return nil
}

// parseRenameTable applies a standalone RENAME TABLE a TO b [, c TO d ...].
// MySQL renames the pairs left to right, so a swap through a temporary name
// works, and each pair goes through the same path as ALTER TABLE ... RENAME.
func (p *Parser) parseRenameTable(stmt *sqlparser.RenameTable) error {
	for _, pair := range stmt.TablePairs {
		if err := p.renameTable(pair.FromTable.Name.String(), pair.ToTable.Name.String()); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) parseDropTable(stmt *sqlparser.DropTable) error {
	for _, tbl := range stmt.FromTables {
		name := tbl.Name.String()
		idx, ok := p.tableIndex[name]
		if !ok {
			continue
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
	}
	return nil
}

// --- CREATE TABLE ---

func (p *Parser) parseCreateTable(stmt *sqlparser.CreateTable) error {
	if stmt.TableSpec == nil {
		return nil
	}

	tableName := stmt.Table.Name.String()
	if _, exists := p.tableIndex[tableName]; exists {
		if stmt.IfNotExists {
			return nil
		}
		return fmt.Errorf("table %s already exists", tableName)
	}

	table := parser.Table{
		Name: tableName,
	}

	// Parse table comment from options.
	for _, opt := range stmt.TableSpec.Options {
		if strings.EqualFold(opt.Name, "COMMENT") && opt.Value != nil {
			table.Comment = opt.Value.Val
		}
	}

	// Parse columns.
	for _, col := range stmt.TableSpec.Columns {
		c := p.parseColumn(tableName, col)
		table.Columns = append(table.Columns, c)
	}

	// Parse indexes (PRIMARY KEY, UNIQUE, INDEX declared at table level).
	for _, idx := range stmt.TableSpec.Indexes {
		constraint := p.parseIndex(idx)
		table.Constraints = append(table.Constraints, constraint)
	}

	// Parse constraints (FOREIGN KEY, CHECK declared at table level).
	for _, cd := range stmt.TableSpec.Constraints {
		constraint := p.parseConstraintDef(cd)
		table.Constraints = append(table.Constraints, constraint)
	}

	synthesizeForeignKeyIndexes(&table)

	parser.ApplyTableConstraints(&table)

	p.tableIndex[tableName] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

	return nil
}

func (p *Parser) parseColumn(tableName string, col *sqlparser.ColumnDefinition) parser.Column {
	c := parser.Column{
		Name:     col.Name.String(),
		Nullable: true,
		Type:     buildTypeString(col.Type),
	}

	opts := col.Type.Options
	if opts == nil {
		return c
	}

	// Nullable: *bool — nil means unspecified (default nullable), false means NOT NULL.
	if opts.Null != nil && !*opts.Null {
		c.Nullable = false
	}

	// Auto-increment.
	if opts.Autoincrement {
		c.AutoIncrement = true
		c.Nullable = false
	}

	// Default value.
	if opts.Default != nil {
		c.Default = sqlparser.String(opts.Default)
	}

	// Column comment.
	if opts.Comment != nil {
		c.Comment = opts.Comment.Val
	}

	// Column-level key options.
	switch opts.KeyOpt {
	case sqlparser.ColKeyPrimary:
		c.PrimaryKey = true
		c.Nullable = false
	case sqlparser.ColKeyUnique, sqlparser.ColKeyUniqueKey:
		c.Unique = true
		c.InlineUnique = true
	case sqlparser.ColKeyNone, sqlparser.ColKeySpatialKey, sqlparser.ColKeyFulltextKey, sqlparser.ColKey:
		// No column flags to set for these key options.
	}

	// Inline FK reference.
	if opts.Reference != nil {
		ref := opts.Reference
		var refCol string
		if len(ref.ReferencedColumns) > 0 {
			refCol = ref.ReferencedColumns[0].String()
		}
		c.FKReference = &parser.FKReference{
			Table:  ref.ReferencedTable.Name.String(),
			Column: refCol,
		}
	}

	// Inline ENUM/SET — extract values and create schema entries.
	p.extractInlineEnumOrSet(tableName, &c, col.Type)

	return c
}

// extractInlineEnumOrSet handles MySQL inline ENUM and SET column types.
// It creates schema-level entries and updates the column type to the derived name.
// Names are scoped to the table (e.g., "users_role_enum") to prevent collisions
// when multiple tables have columns with the same name but different values.
func (p *Parser) extractInlineEnumOrSet(tableName string, c *parser.Column, ct *sqlparser.ColumnType) {
	if len(ct.EnumValues) == 0 {
		return
	}
	values := make([]string, len(ct.EnumValues))
	for i, v := range ct.EnumValues {
		values[i] = strings.Trim(v, "'")
	}
	typ := strings.ToLower(ct.Type)
	switch typ {
	case "enum":
		name := tableName + "_" + c.Name + "_enum"
		p.schema.Enums = append(p.schema.Enums, parser.Enum{Name: name, Values: values})
		c.Type = name
	case "set":
		name := tableName + "_" + c.Name + "_set"
		p.schema.Sets = append(p.schema.Sets, parser.Set{Name: name, Values: values})
		c.Type = name
	}
}

// dropInlineEnumOrSet removes every inline ENUM or SET entry named typeName,
// if there is one. MODIFY and CHANGE COLUMN call it with the old column's type
// before parsing the new definition, which registers its own entry when it is
// still an ENUM or SET: the new definition replaces the old value list rather
// than adding a second entry under the same name, and a column redefined to
// another type, or renamed by CHANGE, leaves no entry behind.
//
// A MySQL ENUM or SET exists only inline, so the entry normally belongs to the
// redefined column alone, which still carries typeName when this runs. RENAME
// COLUMN and RENAME TABLE keep the name derived from the old names, though, so
// a later column can register the same name a second time. The entries are
// then left alone: deleting by name would drop the other column's entry too
// and turn the duplicate-type generation error into a column that silently
// resolves to another column's values or to string.
func (p *Parser) dropInlineEnumOrSet(typeName string) {
	users := 0
	for _, t := range p.schema.Tables {
		for _, c := range t.Columns {
			if c.Type == typeName {
				users++
			}
		}
	}
	if users > 1 {
		return
	}
	p.schema.Enums = slices.DeleteFunc(p.schema.Enums, func(e parser.Enum) bool { return e.Name == typeName })
	p.schema.Sets = slices.DeleteFunc(p.schema.Sets, func(s parser.Set) bool { return s.Name == typeName })
}

// buildTypeString builds the SQL type string from a ColumnType.
func buildTypeString(ct *sqlparser.ColumnType) string {
	typ := strings.ToLower(ct.Type)

	// For ENUM/SET columns, the type is just the keyword.
	if typ == "enum" || typ == "set" {
		return typ
	}

	var sb strings.Builder
	sb.WriteString(typ)

	// Append length/precision.
	if ct.Length != nil {
		if ct.Scale != nil {
			fmt.Fprintf(&sb, "(%d,%d)", *ct.Length, *ct.Scale)
		} else {
			fmt.Fprintf(&sb, "(%d)", *ct.Length)
		}
	}

	// Append unsigned.
	if ct.Unsigned {
		sb.WriteString(" unsigned")
	}

	return sb.String()
}

func (p *Parser) parseIndex(idx *sqlparser.IndexDefinition) parser.Constraint {
	cols := indexColumns(idx)
	name := idx.Info.Name.String()

	switch idx.Info.Type {
	case sqlparser.IndexTypePrimary:
		return parser.Constraint{
			Name:    name,
			Type:    parser.PrimaryKey,
			Columns: cols,
		}
	case sqlparser.IndexTypeUnique:
		return parser.Constraint{
			Name:    name,
			Type:    parser.Unique,
			Columns: cols,
			Method:  indexUsingMethod(idx),
		}
	default:
		return parser.Constraint{
			Name:    name,
			Type:    parser.Index,
			Columns: cols,
			Method:  indexUsingMethod(idx),
		}
	}
}

// indexUsingMethod returns the access method declared by a MySQL
// `INDEX ... USING <method>` clause, lowercased. Returns "" when no USING
// clause is present so the manifest builder can apply the engine default.
// MySQL stores USING as an IndexOption with Name "using" (the literal
// keyword) and the method name in String.
func indexUsingMethod(idx *sqlparser.IndexDefinition) string {
	if idx == nil {
		return ""
	}
	for _, opt := range idx.Options {
		if opt == nil {
			continue
		}
		if strings.EqualFold(opt.Name, "using") {
			return strings.ToLower(opt.String)
		}
	}
	return ""
}

func (p *Parser) parseConstraintDef(cd *sqlparser.ConstraintDefinition) parser.Constraint {
	name := cd.Name.String()

	switch d := cd.Details.(type) {
	case *sqlparser.ForeignKeyDefinition:
		src := make([]string, len(d.Source))
		for i, col := range d.Source {
			src[i] = col.String()
		}
		var refTable string
		var refCols []string
		if d.ReferenceDefinition != nil {
			refTable = d.ReferenceDefinition.ReferencedTable.Name.String()
			refCols = make([]string, len(d.ReferenceDefinition.ReferencedColumns))
			for i, col := range d.ReferenceDefinition.ReferencedColumns {
				refCols[i] = col.String()
			}
		}
		return parser.Constraint{
			Name:             name,
			Type:             parser.ForeignKey,
			Columns:          src,
			ReferenceTable:   refTable,
			ReferenceColumns: refCols,
		}

	case *sqlparser.CheckConstraintDefinition:
		return parser.Constraint{
			Name:            name,
			Type:            parser.Check,
			CheckExpression: sqlparser.String(d.Expr),
		}

	default:
		return parser.Constraint{Name: name}
	}
}

// synthesizeForeignKeyIndexes mirrors MySQL's auto-creation of a backing index
// for every FOREIGN KEY whose referencing columns are not already the leftmost
// prefix of an existing PRIMARY KEY, UNIQUE, or plain index. MySQL materializes
// such an index at FK-creation time (naming it after the FK constraint, or the
// first referencing column for an unnamed FK) and reports it via introspection;
// without this a DDL parse of a bare `FOREIGN KEY (x) REFERENCES ...` omits the
// index, producing the introspect-vs-DDL manifest drift.
// Call after all of a table's constraints are parsed and before
// ApplyTableConstraints. It is idempotent: a re-run finds each FK already
// covered by the index synthesized for it, matching MySQL's own dedup of a
// single backing index across FKs that share columns.
func synthesizeForeignKeyIndexes(table *parser.Table) {
	// Snapshot the FK (name, columns) pairs before mutating table.Constraints:
	// the loop appends Index constraints, and a later FK sharing a column must
	// observe the index synthesized for an earlier one.
	type fkRef struct {
		name    string
		columns []string
	}
	var fks []fkRef
	for _, c := range table.Constraints {
		if c.Type == parser.ForeignKey && len(c.Columns) > 0 {
			fks = append(fks, fkRef{name: c.Name, columns: c.Columns})
		}
	}

	for _, fk := range fks {
		if foreignKeyIndexCovers(table, fk.columns) {
			continue
		}
		table.Constraints = append(table.Constraints, parser.Constraint{
			Name:    foreignKeyIndexName(fk.name, fk.columns),
			Type:    parser.Index,
			Columns: slices.Clone(fk.columns),
		})
	}
}

// foreignKeyIndexCovers reports whether fkCols already form the leftmost prefix
// of some PRIMARY KEY, UNIQUE, or plain index on the table — or, for a
// single-column FK, of a column-level PRIMARY KEY / UNIQUE flag. This is
// MySQL's condition for not materializing a backing index.
func foreignKeyIndexCovers(table *parser.Table, fkCols []string) bool {
	if len(fkCols) == 1 {
		if col := parser.ColumnByName(table, fkCols[0]); col != nil && (col.PrimaryKey || col.Unique) {
			return true
		}
	}
	for _, c := range table.Constraints {
		switch c.Type {
		case parser.PrimaryKey, parser.Unique, parser.Index:
			if columnsPrefixMatch(c.Columns, fkCols) {
				return true
			}
		case parser.ForeignKey, parser.Check:
			// Neither backs an index that can cover a foreign key.
		}
	}
	return false
}

// columnsPrefixMatch reports whether prefix equals the leading columns of
// indexCols in order.
func columnsPrefixMatch(indexCols, prefix []string) bool {
	if len(prefix) == 0 || len(prefix) > len(indexCols) {
		return false
	}
	for i, col := range prefix {
		if indexCols[i] != col {
			return false
		}
	}
	return true
}

// foreignKeyIndexName reproduces MySQL's naming of an FK backing index: the FK
// constraint name when the FK is named, otherwise the first referencing column.
func foreignKeyIndexName(fkName string, cols []string) string {
	if fkName != "" {
		return fkName
	}
	return cols[0]
}

// --- ALTER TABLE ---

func (p *Parser) parseAlterTable(stmt *sqlparser.AlterTable) error {
	tableName := stmt.Table.Name.String()
	table, ok := p.lookupTable(tableName)
	if !ok {
		return fmt.Errorf("table %s not found", tableName)
	}

	// MySQL drops the old primary key before it adds a new one, wherever DROP
	// PRIMARY KEY sits among the clauses, and the drop clears the key flag on
	// every column (applyDropKey). Applied in clause order, `ADD PRIMARY KEY
	// (code), DROP PRIMARY KEY` would lose the key it just added.
	for _, opt := range stmt.AlterOptions {
		if o, ok := dropPrimaryKey(opt); ok {
			applyDropKey(table, o)
		}
	}
	for _, opt := range stmt.AlterOptions {
		if _, ok := dropPrimaryKey(opt); ok {
			continue
		}
		if err := p.applyAlterOption(table, opt); err != nil {
			return err
		}
	}

	return nil
}

// dropPrimaryKey returns opt as a DropKey, and true, when it is a DROP PRIMARY
// KEY clause.
func dropPrimaryKey(opt sqlparser.AlterOption) (*sqlparser.DropKey, bool) {
	o, ok := opt.(*sqlparser.DropKey)
	if !ok || o.Type != sqlparser.PrimaryKeyType {
		return nil, false
	}
	return o, true
}

func (p *Parser) applyAlterOption(table *parser.Table, opt sqlparser.AlterOption) error {
	switch o := opt.(type) {
	case *sqlparser.AddColumns:
		for _, col := range o.Columns {
			c := p.parseColumn(table.Name, col)
			table.Columns = append(table.Columns, c)
		}
	case *sqlparser.DropColumn:
		colName := o.Name.Name.String()
		table.Columns = slices.DeleteFunc(table.Columns, func(c parser.Column) bool {
			return c.Name == colName
		})
	case *sqlparser.ModifyColumn:
		return p.alterModifyColumn(table, o)
	case *sqlparser.AddIndexDefinition:
		constraint := p.parseIndex(o.IndexDefinition)
		table.Constraints = append(table.Constraints, constraint)
		parser.ApplyConstraintToColumns(table, constraint)
	case *sqlparser.DropKey:
		applyDropKey(table, o)
	case *sqlparser.AddConstraintDefinition:
		constraint := p.parseConstraintDef(o.ConstraintDefinition)
		table.Constraints = append(table.Constraints, constraint)
		parser.ApplyConstraintToColumns(table, constraint)
		if constraint.Type == parser.ForeignKey {
			// ALTER TABLE ... ADD FOREIGN KEY makes MySQL materialize a backing
			// index too; mirror it so the DDL and introspect paths agree.
			synthesizeForeignKeyIndexes(table)
		}
	case *sqlparser.RenameColumn:
		oldName, newName := o.OldName.Name.String(), o.NewName.Name.String()
		if err := parser.RenameColumn(table, oldName, newName); err != nil {
			return fmt.Errorf("rename column %s: %w", table.Name, err)
		}
		parser.RenameColumnReferences(p.schema.Tables, table.Schema, table.Name, oldName, newName)
		return nil
	case *sqlparser.RenameTableName:
		return p.renameTable(table.Name, o.Table.Name.String())
	case *sqlparser.ChangeColumn:
		return p.alterChangeColumn(table, o)
	}

	return nil
}

func (p *Parser) alterModifyColumn(table *parser.Table, o *sqlparser.ModifyColumn) error {
	if o.NewColDefinition == nil {
		return nil
	}
	name := o.NewColDefinition.Name.String()
	for i := range table.Columns {
		if table.Columns[i].Name == name {
			p.dropInlineEnumOrSet(table.Columns[i].Type)
			table.Columns[i] = redefineColumn(table.Columns[i], p.parseColumn(table.Name, o.NewColDefinition))
			return nil
		}
	}
	// Column not found — add it.
	table.Columns = append(table.Columns, p.parseColumn(table.Name, o.NewColDefinition))
	return nil
}

func (p *Parser) alterChangeColumn(table *parser.Table, o *sqlparser.ChangeColumn) error {
	if o.NewColDefinition == nil {
		return nil
	}
	oldName := o.OldColumn.Name.String()
	for i := range table.Columns {
		if table.Columns[i].Name == oldName {
			p.dropInlineEnumOrSet(table.Columns[i].Type)
			newCol := p.parseColumn(table.Name, o.NewColDefinition)
			table.Columns[i] = redefineColumn(table.Columns[i], newCol)
			// CHANGE COLUMN can rename too; references follow, as on RENAME COLUMN.
			parser.RenameColumnReferences(p.schema.Tables, table.Schema, table.Name, oldName, newCol.Name)
			return nil
		}
	}
	return fmt.Errorf("column %s not found in table %s", oldName, table.Name)
}

// redefineColumn returns the column that MODIFY or CHANGE COLUMN leaves in
// place of old. The new definition replaces the type, nullability, default,
// AUTO_INCREMENT and comment, but the table's keys outlive it: MySQL keeps the
// PRIMARY KEY and every UNIQUE index on the column, declared inline or at table
// level, and every foreign key on it (measured on MySQL 8.0, SHOW CREATE TABLE
// after the ALTER).
// The definition alone cannot see them, so the flags they set on old carry
// over, and a primary-key column stays NOT NULL as MySQL makes it. Without
// this the table loses its primary key and drops out of generation.
func redefineColumn(old, newCol parser.Column) parser.Column {
	if old.PrimaryKey {
		newCol.PrimaryKey = true
		newCol.Nullable = false
	}
	newCol.Unique = newCol.Unique || old.Unique
	newCol.InlineUnique = newCol.InlineUnique || old.InlineUnique
	if newCol.FKReference == nil {
		newCol.FKReference = old.FKReference
	}
	return newCol
}

func applyDropKey(table *parser.Table, o *sqlparser.DropKey) {
	name := o.Name.String()
	switch o.Type {
	case sqlparser.PrimaryKeyType:
		// A table has one primary key, and an inline PRIMARY KEY exists only
		// as the column flag (parseColumn never records it in
		// table.Constraints), so the flag comes off every column, not only
		// the columns of a table-level constraint. Nullability
		// stays: MySQL leaves the former key columns NOT NULL.
		removeConstraintFromColumns(table, parser.PrimaryKey, name)
		for i := range table.Columns {
			table.Columns[i].PrimaryKey = false
		}
	case sqlparser.ForeignKeyType:
		removeConstraintFromColumns(table, parser.ForeignKey, name)
	case sqlparser.NormalKeyType:
		// MySQL DROP INDEX drops both UNIQUE and non-UNIQUE indexes by
		// name. parseIndex tags UNIQUE indexes as parser.Unique, so
		// reverse flags via the helper for whichever shape matches.
		removeConstraintFromColumns(table, parser.Unique, name)
		removeConstraintFromColumns(table, parser.Index, name)
	case sqlparser.CheckKeyType:
		removeConstraintFromColumns(table, parser.Check, name)
	}
}

// --- Helpers ---

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

func (p *Parser) lookupTable(name string) (*parser.Table, bool) {
	idx, ok := p.tableIndex[name]
	if !ok {
		return nil, false
	}
	return &p.schema.Tables[idx], true
}

func indexColumns(idx *sqlparser.IndexDefinition) []string {
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = c.Column.String()
	}
	return cols
}

// removeConstraintFromColumns removes the first constraint matching
// (cType, name) from table.Constraints and reverses its column flag
// effects. Name may be empty for PrimaryKey (MySQL DROP PRIMARY KEY has
// no name). The slice deletion happens first so that RecomputeColumnUnique
// sees the post-drop state of the table when re-deriving col.Unique
// from the remaining constraints + the column's InlineUnique flag.
func removeConstraintFromColumns(table *parser.Table, cType parser.ConstraintType, name string) {
	idx := slices.IndexFunc(table.Constraints, func(c parser.Constraint) bool {
		if c.Type != cType {
			return false
		}
		return name == "" || c.Name == name
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
		// No column flags to reverse.
	}
}
